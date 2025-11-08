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

// fileProxyHandler handles requests for files or HLS playlists
func fileProxyHandler(w http.ResponseWriter, r *http.Request) {
	// Extract dynamic prefix and path: /file1/encoded_path
	pathParts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)
	if len(pathParts) < 2 {
		sendError(w, http.StatusBadRequest, "Invalid file proxy path", nil)
		return
	}

	prefix := "/" + pathParts[0] + "/"
	path := pathParts[1]

	// Decode URL path
	decodedPath, err := url.PathUnescape(path)
	if err != nil {
		decodedPath = path
	}

	if decodedPath == "" {
		sendError(w, http.StatusBadRequest, "Invalid file proxy path", nil)
		return
	}

	// Get required host parameter
	host := r.URL.Query().Get("host")
	if host == "" {
		sendError(w, http.StatusBadRequest, "host parameter is required", nil)
		return
	}

	targetURL := fmt.Sprintf("%s/%s", strings.TrimSuffix(host, "/"), decodedPath)

	// Parse optional headers
	parsedHeaders := make(map[string]string)
	headersParam := r.URL.Query().Get("headers")
	if headersParam != "" {
		if decodedHeaders, err := url.QueryUnescape(headersParam); err == nil {
			if err2 := json.Unmarshal([]byte(decodedHeaders), &parsedHeaders); err2 != nil {
				log.Printf("Failed to parse headers JSON: %v", err2)
			}
		}
	}

	requestHeaders := generateRequestHeaders(targetURL, parsedHeaders)

	// Check if this is an M3U8 file
	isM3U8 := strings.HasSuffix(strings.ToLower(decodedPath), ".m3u8")

	if isM3U8 {
		handleFileM3U8Proxy(w, targetURL, host, decodedPath, prefix, requestHeaders)
	} else {
		handleFileSegmentProxy(w, targetURL, requestHeaders)
	}
}

// handleFileM3U8Proxy rewrites M3U8 playlists to go through the proxy
func handleFileM3U8Proxy(w http.ResponseWriter, targetURL, host, originalPath, prefix string, headers map[string]string) {
	resp, err := makeRequest(targetURL, headers, nil)
	if err != nil {
		sendError(w, http.StatusBadGateway, "Failed to fetch m3u8 content", err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		sendError(w, resp.StatusCode, "Upstream server returned error", fmt.Sprintf("Status: %d", resp.StatusCode))
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "Failed to read m3u8 content", err.Error())
		return
	}

	m3u8Content := string(body)
	if !strings.HasPrefix(strings.TrimSpace(m3u8Content), "#EXTM3U") {
		sendError(w, http.StatusBadGateway, "Invalid m3u8 content", "Response does not start with #EXTM3U")
		return
	}

	lines := strings.Split(m3u8Content, "\n")
	newLines := make([]string, 0, len(lines))

	encodedHeaders, _ := json.Marshal(headers)
	headersParam := url.QueryEscape(string(encodedHeaders))

	// Base path for relative segments
	basePath := ""
	if lastSlash := strings.LastIndex(originalPath, "/"); lastSlash != -1 {
		basePath = originalPath[:lastSlash+1]
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			newLines = append(newLines, line)
			continue
		}

		if strings.HasPrefix(line, "#") {
			if strings.Contains(line, "URI=") {
				newLines = append(newLines, processFileKeyURI(line, host, basePath, prefix, headersParam))
			} else {
				newLines = append(newLines, line)
			}
		} else {
			newLines = append(newLines, processFileSegmentURL(line, host, basePath, prefix, headersParam))
		}
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Write([]byte(strings.Join(newLines, "\n")))
}

// handleFileSegmentProxy streams TS or other files
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

// processFileKeyURI rewrites #EXT-X-KEY URI lines
func processFileKeyURI(line, host, basePath, prefix, headersParam string) string {
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

	var resolved string
	if strings.HasPrefix(originalURI, "http://") || strings.HasPrefix(originalURI, "https://") {
		resolved = originalURI
	} else {
		resolved = fmt.Sprintf("%s%s%s", host, basePath, originalURI)
	}

	return fmt.Sprintf(`%s%s%s?host=%s&headers=%s`,
		webServerURL,
		prefix,
		strings.TrimPrefix(resolved, "/"),
		url.QueryEscape(host),
		headersParam)
}

// processFileSegmentURL rewrites segment URLs in M3U8
func processFileSegmentURL(line, host, basePath, prefix, headersParam string) string {
	trimmed := strings.TrimSpace(line)
	resolved := trimmed
	if !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
		resolved = fmt.Sprintf("%s%s", basePath, trimmed)
	}
	return fmt.Sprintf(`%s%s%s?host=%s&headers=%s`,
		webServerURL,
		prefix,
		strings.TrimPrefix(resolved, "/"),
		url.QueryEscape(host),
		headersParam)
}
