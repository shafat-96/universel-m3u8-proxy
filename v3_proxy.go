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

// universalHLSProxyHandler handles any HLS playback proxy requests with dynamic prefix detection
// Works with: /hls-playback/, /v3-hls-playback/, /v2-hls-playback/, etc.
// Example: /hls-playback/encoded_path/playlist.m3u8?host=https://example.com&headers={...}
func universalHLSProxyHandler(w http.ResponseWriter, r *http.Request) {
	// Detect the prefix pattern (e.g., /hls-playback/, /v3-hls-playback/)
	pathParts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)
	if len(pathParts) < 2 {
		sendError(w, http.StatusBadRequest, "Invalid HLS proxy path", nil)
		return
	}
	
	prefix := "/" + pathParts[0] + "/"
	path := pathParts[1]
	
	if path == "" {
		sendError(w, http.StatusBadRequest, "Invalid HLS proxy path", nil)
		return
	}

	// Get host parameter (required)
	host := r.URL.Query().Get("host")
	if host == "" {
		sendError(w, http.StatusBadRequest, "host parameter is required", nil)
		return
	}

	// Construct the full target URL with the same prefix
	targetURL := fmt.Sprintf("%s%s%s", strings.TrimSuffix(host, "/"), prefix, path)

	// Parse additional headers from query parameter
	parsedHeaders := make(map[string]string)
	headersParam := r.URL.Query().Get("headers")
	if headersParam != "" {
		decodedHeaders, err := url.QueryUnescape(headersParam)
		if err == nil {
			json.Unmarshal([]byte(decodedHeaders), &parsedHeaders)
		}
	}

	// Generate request headers
	requestHeaders := generateRequestHeaders(targetURL, parsedHeaders)

	// Check if this is an M3U8 file
	isM3U8 := strings.HasSuffix(strings.ToLower(path), ".m3u8")

	if isM3U8 {
		// Handle M3U8 playlist
		handleUniversalM3U8Proxy(w, targetURL, host, path, prefix, requestHeaders)
	} else {
		// Handle regular file (TS segments, etc.)
		handleUniversalSegmentProxy(w, targetURL, requestHeaders)
	}
}

// handleUniversalM3U8Proxy processes M3U8 playlists and rewrites URLs with dynamic prefix
func handleUniversalM3U8Proxy(w http.ResponseWriter, targetURL, host, originalPath, prefix string, headers map[string]string) {
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
				newLines = append(newLines, processUniversalKeyURI(line, host, basePath, prefix, headersParam))
			} else {
				newLines = append(newLines, line)
			}
		} else if strings.TrimSpace(line) != "" {
			// Handle segment URLs
			newLines = append(newLines, processUniversalSegmentURL(line, host, basePath, prefix, headersParam))
		} else {
			newLines = append(newLines, line)
		}
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Write([]byte(strings.Join(newLines, "\n")))
}

// handleUniversalSegmentProxy streams file segments (TS, keys, etc.)
func handleUniversalSegmentProxy(w http.ResponseWriter, targetURL string, headers map[string]string) {
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
		log.Printf("Error streaming HLS response: %v", err)
	}
}

// processUniversalKeyURI processes encryption key URIs in M3U8 playlists with dynamic prefix
func processUniversalKeyURI(line, host, basePath, prefix, headersParam string) string {
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
		// Absolute URL - extract path after prefix
		if parsed, err := url.Parse(originalURI); err == nil {
			pathPart := strings.TrimPrefix(parsed.Path, prefix)
			newURI = fmt.Sprintf("%s%s%s?host=%s&headers=%s",
				webServerURL,
				prefix,
				pathPart,
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

// processUniversalSegmentURL processes segment URLs in M3U8 playlists with dynamic prefix
func processUniversalSegmentURL(line, host, basePath, prefix, headersParam string) string {
	trimmedLine := strings.TrimSpace(line)

	// Check if it's an absolute URL
	if strings.HasPrefix(trimmedLine, "http://") || strings.HasPrefix(trimmedLine, "https://") {
		// Extract path from absolute URL
		if parsed, err := url.Parse(trimmedLine); err == nil {
			pathPart := strings.TrimPrefix(parsed.Path, prefix)
			return fmt.Sprintf("%s%s%s?host=%s&headers=%s",
				webServerURL,
				prefix,
				pathPart,
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
