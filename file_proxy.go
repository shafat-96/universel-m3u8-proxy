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
	
	if path == "" {
		sendError(w, http.StatusBadRequest, "Invalid file proxy path", nil)
		return
	}

	// Get host parameter (required)
	host := r.URL.Query().Get("host")
	if host == "" {
		sendError(w, http.StatusBadRequest, "host parameter is required", nil)
		return
	}

	targetURL := fmt.Sprintf("%s/%s", strings.TrimSuffix(host, "/"), path)

	parsedHeaders := make(map[string]string)
	headersParam := r.URL.Query().Get("headers")
	if headersParam != "" {
		decodedHeaders, err := url.QueryUnescape(headersParam)
		if err == nil {
			json.Unmarshal([]byte(decodedHeaders), &parsedHeaders)
		}
	}

	requestHeaders := generateRequestHeaders(targetURL, parsedHeaders)

	isM3U8 := strings.HasSuffix(strings.ToLower(path), ".m3u8")

	if isM3U8 {
		handleFileM3U8Proxy(w, targetURL, host, path, prefix, requestHeaders)
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "Failed to read m3u8 content", err.Error())
		return
	}

	// Process M3U8 content
	m3u8Content := string(body)
	lines := strings.Split(m3u8Content, "\n")
	newLines := make([]string, 0, len(lines))

	// Extract base path from original path (everything before the filename)
	basePath := ""
	if lastSlash := strings.LastIndex(originalPath, "/"); lastSlash != -1 {
		basePath = originalPath[:lastSlash+1]
	}

	// Encode headers for URL
	encodedHeaders, _ := json.Marshal(headers)
	headersParam := url.QueryEscape(string(encodedHeaders))

	for _, line := range lines {
		if strings.HasPrefix(line, "#") {
			// Handle key URIs in #EXT-X-KEY lines
			if strings.Contains(line, "URI=") {
				newLines = append(newLines, processFileKeyURI(line, host, basePath, prefix, headersParam))
			} else {
				newLines = append(newLines, line)
			}
		} else if strings.TrimSpace(line) != "" {
			// Handle segment URLs
			newLines = append(newLines, processFileSegmentURL(line, host, basePath, prefix, headersParam))
		} else {
			newLines = append(newLines, line)
		}
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Write([]byte(strings.Join(newLines, "\n")))
}

// handleFileSegmentProxy streams file segments (TS, keys, etc.)
func handleFileSegmentProxy(w http.ResponseWriter, targetURL string, headers map[string]string) {
	// Fetch the content
	resp, err := makeRequest(targetURL, headers, nil)
	if err != nil {
		sendError(w, http.StatusBadGateway, "Failed to proxy segment", err.Error())
		return
	}
	defer resp.Body.Close()

	// Determine content type
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = detectContentType(targetURL)
	}

	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(resp.StatusCode)

	// Stream the response
	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Printf("Error streaming file response: %v", err)
	}
}

// processFileKeyURI processes encryption key URIs in M3U8 playlists with dynamic prefix
func processFileKeyURI(line, host, basePath, prefix, headersParam string) string {
	// Extract URI from the line
	start := strings.Index(line, `URI="`)
	if start == -1 {
		return line
	}
	start += 5 // len(`URI="`)

	end := strings.Index(line[start:], `"`)
	if end == -1 {
		return line
	}

	originalURI := line[start : start+end]

	// Build the new proxy URI
	var newURI string
	if strings.HasPrefix(originalURI, "http://") || strings.HasPrefix(originalURI, "https://") {
		// Absolute URL - extract path
		if parsed, err := url.Parse(originalURI); err == nil {
			newURI = fmt.Sprintf("%s%s%s?host=%s&headers=%s",
				webServerURL,
				prefix,
				parsed.Path,
				url.QueryEscape(host),
				headersParam)
		} else {
			return line
		}
	} else {
		// Relative URL
		newURI = fmt.Sprintf("%s%s%s%s?host=%s&headers=%s",
			webServerURL,
			prefix,
			basePath,
			originalURI,
			url.QueryEscape(host),
			headersParam)
	}

	return strings.Replace(line, originalURI, newURI, 1)
}

// processFileSegmentURL processes segment URLs in M3U8 playlists with dynamic prefix
func processFileSegmentURL(line, host, basePath, prefix, headersParam string) string {
	trimmedLine := strings.TrimSpace(line)

	// Check if it's an absolute URL
	if strings.HasPrefix(trimmedLine, "http://") || strings.HasPrefix(trimmedLine, "https://") {
		// Extract path from absolute URL
		if parsed, err := url.Parse(trimmedLine); err == nil {
			return fmt.Sprintf("%s%s%s?host=%s&headers=%s",
				webServerURL,
				prefix,
				parsed.Path,
				url.QueryEscape(host),
				headersParam)
		}
		return line
	}

	// Relative URL - combine with base path
	return fmt.Sprintf("%s%s%s%s?host=%s&headers=%s",
		webServerURL,
		prefix,
		basePath,
		trimmedLine,
		url.QueryEscape(host),
		headersParam)
}
