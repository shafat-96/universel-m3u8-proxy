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

// --- Main Proxy Handler ---
func fileProxyHandler(w http.ResponseWriter, r *http.Request) {
	// Detect the prefix pattern (e.g., /file1/, /file2/, /file3/)
	pathParts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)
	if len(pathParts) < 2 {
		sendError(w, http.StatusBadRequest, "Invalid file proxy path", nil)
		return
	}

	prefix := "/" + pathParts[0] + "/"
	path := pathParts[1]

	// URL-decode the path to handle encoded filenames
	decodedPath, err := url.PathUnescape(path)
	if err != nil {
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

	// Construct the full target URL
	// Ensure host doesn't end with slash and path starts with slash
	hostTrimmed := strings.TrimSuffix(host, "/")
	pathWithSlash := decodedPath
	if !strings.HasPrefix(pathWithSlash, "/") {
		pathWithSlash = "/" + pathWithSlash
	}
	
	targetURLString := hostTrimmed + pathWithSlash
	
	// Parse to validate the URL
	targetURL, err := url.Parse(targetURLString)
	if err != nil {
		sendError(w, http.StatusBadRequest, "Failed to construct target URL", err.Error())
		return
	}

	// Parse optional headers
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

	requestHeaders := generateRequestHeaders(targetURL.String(), parsedHeaders)

	isM3U8 := strings.HasSuffix(strings.ToLower(decodedPath), ".m3u8")

	if isM3U8 {
		handleFileM3U8Proxy(w, targetURL, host, decodedPath, prefix, requestHeaders)
	} else {
		handleFileSegmentProxy(w, targetURL.String(), requestHeaders)
	}
}

// --- M3U8 Playlist Handling ---
func handleFileM3U8Proxy(w http.ResponseWriter, targetURL *url.URL, host, originalPath, prefix string, headers map[string]string) {
	resp, err := makeRequest(targetURL.String(), headers, nil)
	if err != nil {
		sendError(w, http.StatusBadGateway, "Failed to fetch m3u8 content", err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		sendError(w, http.StatusBadGateway, fmt.Sprintf("Upstream returned %d", resp.StatusCode), nil)
		return
	}

	body, err := readResponseBody(resp)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "Failed to read m3u8 content", err.Error())
		return
	}

	m3u8Content := string(body)
	lines := strings.Split(m3u8Content, "\n")
	newLines := make([]string, 0, len(lines))

	encodedHeaders, _ := json.Marshal(headers)
	headersParam := url.QueryEscape(string(encodedHeaders))
	encodedHost := url.QueryEscape(host)

	// Determine base URL for relative paths
	baseURL := targetURL
	if lastSlash := strings.LastIndex(originalPath, "/"); lastSlash != -1 {
		basePath := originalPath[:lastSlash+1]
		baseURL, _ = targetURL.Parse(basePath)
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(trimmed, "#") {
			if strings.Contains(trimmed, "URI=") {
				newLines = append(newLines, processFileKeyURI(trimmed, baseURL, encodedHost, prefix, headersParam))
			} else {
				newLines = append(newLines, trimmed)
			}
		} else {
			newLines = append(newLines, processFileSegmentURL(trimmed, baseURL, encodedHost, prefix, headersParam))
		}
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Write([]byte(strings.Join(newLines, "\n")))
}

// --- TS / Segment Handling ---
func handleFileSegmentProxy(w http.ResponseWriter, targetURL string, headers map[string]string) {
	resp, err := makeRequest(targetURL, headers, nil)
	if err != nil {
		sendError(w, http.StatusBadGateway, "Failed to proxy segment", err.Error())
		return
	}
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = detectContentType(targetURL)
	}

	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(resp.StatusCode)

	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Printf("Error streaming file response: %v", err)
	}
}

// --- Rewrite #EXT-X-KEY URIs ---
func processFileKeyURI(line string, baseURL *url.URL, encodedHost, prefix, headersParam string) string {
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
	resolvedURL, err := baseURL.Parse(originalURI)
	if err != nil {
		return line
	}

	newURI := fmt.Sprintf("%s%s%s?host=%s&headers=%s",
		webServerURL,
		prefix,
		resolvedURL.Path,
		encodedHost,
		headersParam,
	)

	return strings.Replace(line, originalURI, newURI, 1)
}

// --- Rewrite Segment URLs ---
func processFileSegmentURL(line string, baseURL *url.URL, encodedHost, prefix, headersParam string) string {
	resolvedURL, err := baseURL.Parse(line)
	if err != nil {
		return line
	}

	return fmt.Sprintf("%s%s%s?host=%s&headers=%s",
		webServerURL,
		prefix,
		resolvedURL.Path,
		encodedHost,
		headersParam,
	)
}

