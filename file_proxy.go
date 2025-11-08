package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
)

func fileProxyHandler(w http.ResponseWriter, r *http.Request) {
	// Detect the prefix pattern (e.g., /file1/, /file2/, /file3/)
	pathParts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)
	if len(pathParts) < 2 {
		sendError(w, http.StatusBadRequest, "Invalid file proxy path", nil)
		return
	}

	prefix := "/" + pathParts[0] + "/"
	path := pathParts[1]

	// URL‑decode the path to handle encoded filenames
	decodedPath, err := url.PathUnescape(path)
	if err != nil {
		// If decoding fails, use the original path
		decodedPath = path
	}

	if decodedPath == "" {
		sendError(w, http.StatusBadRequest, "Invalid file proxy path", nil)
		return
	}

	// Get host parameter (required)
	host := r.URL.Query().Get("host")
	if host == "" {
		sendError(w, http.StatusBadRequest, "host parameter is required", nil)
		return
	}

	targetURL := fmt.Sprintf("%s/%s", strings.TrimSuffix(host, "/"), decodedPath)

	parsedHeaders := make(map[string]string)
	headersParam := r.URL.Query().Get("headers")
	if headersParam != "" {
		decodedHeaders, err := url.QueryUnescape(headersParam)
		if err == nil {
			if err2 := json.Unmarshal([]byte(decodedHeaders), &parsedHeaders); err2 != nil {
				log.Printf("Failed to parse headers JSON: %v", err2)
			}
		}
	}

	requestHeaders := generateRequestHeaders(targetURL, parsedHeaders)

	isM3U8 := strings.HasSuffix(strings.ToLower(decodedPath), ".m3u8")

	if isM3U8 {
		handleFileM3U8Proxy(w, targetURL, host, decodedPath, prefix, requestHeaders)
	} else {
		// Handle regular file (TS segments, etc.)
		handleFileSegmentProxy(w, targetURL, requestHeaders)
	}
}

func handleFileM3U8Proxy(w http.ResponseWriter, targetURL, host, originalPath, prefix string, headers map[string]string) {
	// Fetch the M3U8 content
	resp, err := makeRequest(targetURL, headers, nil)
	if err != nil {
		sendError(w, http.StatusBadGateway, "Failed to fetch m3u8 content", err.Error())
		return
	}
	defer resp.Body.Close()

	body, err := readResponseBody(resp)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "Failed to read m3u8 content", err.Error())
		return
	}

	m3u8Content := string(body)
	lines := strings.Split(m3u8Content, "\n")
	newLines := make([]string, 0, len(lines))

	// Encode headers for URL (cache to avoid redundant encoding)
	encodedHeaders, _ := json.Marshal(headers)
	headersParam := url.QueryEscape(string(encodedHeaders))
	encodedHost := url.QueryEscape(host)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(trimmed, "#") {
			// Handle key URIs in #EXT‑X‑KEY lines
			if strings.Contains(trimmed, "URI=") {
				newLines = append(newLines, processFileKeyURI(trimmed, targetURL, encodedHost, prefix, headersParam))
			} else {
				newLines = append(newLines, trimmed)
			}
		} else {
			// Handle segment URLs - resolve against base URL
			newLines = append(newLines, processFileSegmentURL(trimmed, targetURL, encodedHost, prefix, headersParam))
		}
	}

	w.Header().Set("Content‑Type", "application/vnd.apple.mpegurl")
	w.Write([]byte(strings.Join(newLines, "\n")))
}

func handleFileSegmentProxy(w http.ResponseWriter, targetURL string, headers map[string]string) {
	// Fetch the content
	resp, err := makeRequest(targetURL, headers, nil)
	if err != nil {
		sendError(w, http.StatusBadGateway, "Failed to proxy segment", err.Error())
		return
	}
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content‑Type")
	if contentType == "" {
		contentType = detectContentType(targetURL)
	}

	w.Header().Set("Content‑Type", contentType)
	w.WriteHeader(resp.StatusCode)

	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Printf("Error streaming file response: %v", err)
	}
}

func processFileKeyURI(line, baseURL, encodedHost, prefix, headersParam string) string {
	start := strings.Index(line, `URI="`)
	if start == -1 {
		return line
	}
	start += len(`URI="`)
	end := strings.Index(line[start:], `"`)
	if end == -1 {
		return line
	}

	originalURI := line[start : start+end]

	// Resolve relative URL against base URL
	resolvedURL := resolveURL(baseURL, originalURI)
	parsed, err := url.Parse(resolvedURL)
	if err != nil {
		return line
	}

	newURI := fmt.Sprintf("%s%s%s?host=%s&headers=%s",
		webServerURL,
		prefix,
		strings.TrimPrefix(parsed.Path, "/"),
		encodedHost,
		headersParam)

	return strings.Replace(line, originalURI, newURI, 1)
}

func processFileSegmentURL(line, baseURL, encodedHost, prefix, headersParam string) string {
	resolvedURL := resolveURL(baseURL, line)
	parsed, err := url.Parse(resolvedURL)
	if err != nil {
		return line
	}

	return fmt.Sprintf("%s%s%s?host=%s&headers=%s",
		webServerURL,
		prefix,
		strings.TrimPrefix(parsed.Path, "/"),
		encodedHost,
		headersParam)
}
