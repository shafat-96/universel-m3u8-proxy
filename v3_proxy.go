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

	// Check response status
	if resp.StatusCode != http.StatusOK {
		sendError(w, http.StatusBadGateway, "Upstream returned non-200 status", fmt.Sprintf("Status: %d", resp.StatusCode))
		return
	}

	body, err := readResponseBody(resp)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "Failed to read m3u8 content", err.Error())
		return
	}

	// Validate M3U8 content
	m3u8Content := string(body)
	if !strings.HasPrefix(strings.TrimSpace(m3u8Content), "#EXTM3U") {
		sendError(w, http.StatusBadGateway, "Invalid M3U8 content", "Content does not start with #EXTM3U")
		return
	}

	// Detect if this is a master playlist
	isMaster := strings.Contains(m3u8Content, "#EXT-X-STREAM-INF:")

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

	// Track current tag for master playlist processing
	var currentTag string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		
		// Skip empty lines
		if trimmed == "" {
			newLines = append(newLines, line)
			continue
		}

		var inlineURL string

		if strings.HasPrefix(trimmed, "#") {
			// Track tag type for master playlists
			if isMaster {
				tagParts := strings.SplitN(trimmed, ":", 2)
				if len(tagParts) > 0 {
					currentTag = strings.ToUpper(strings.TrimPrefix(tagParts[0], "#"))
				}
			}

			// Check for inline URIs in tags (e.g., #EXT-X-KEY)
			if strings.Contains(trimmed, `URI="`) {
				// Extract URI from quotes
				if start := strings.Index(trimmed, `URI="`); start != -1 {
					start += 5
					if end := strings.Index(trimmed[start:], `"`); end != -1 {
						inlineURL = trimmed[start : start+end]
					}
				}
			}

			// If no inline URL found, just add the line as-is
			if inlineURL == "" {
				newLines = append(newLines, line)
				continue
			}
		}

		// Determine the URL to process
		urlToProcess := inlineURL
		if urlToProcess == "" {
			urlToProcess = trimmed
		}

		// For master playlists, only rewrite specific tags
		if isMaster && currentTag != "EXT-X-STREAM-INF" && currentTag != "EXT-X-MEDIA" && currentTag != "EXT-X-I-FRAME-STREAM-INF" {
			newLines = append(newLines, line)
			continue
		}

		// Resolve and rewrite the URL
		resolvedURL := resolveUniversalURL(urlToProcess, targetURL, host, basePath, prefix)
		proxyURL := fmt.Sprintf("%s%s%s?host=%s&headers=%s",
			webServerURL,
			prefix,
			resolvedURL,
			url.QueryEscape(host),
			headersParam)

		// Replace the URL in the line
		if inlineURL != "" {
			newLines = append(newLines, strings.Replace(line, inlineURL, proxyURL, 1))
		} else {
			newLines = append(newLines, proxyURL)
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

// resolveUniversalURL resolves a URL (absolute or relative) and returns the path portion for proxying
func resolveUniversalURL(urlStr, targetURL, host, basePath, prefix string) string {
	// Check if it's an absolute URL
	if strings.HasPrefix(urlStr, "http://") || strings.HasPrefix(urlStr, "https://") {
		// Parse and extract path
		if parsed, err := url.Parse(urlStr); err == nil {
			// Return path without the prefix (will be added back by caller)
			return strings.TrimPrefix(parsed.Path, prefix)
		}
		return urlStr
	}

	// Relative URL - resolve against target URL
	if baseURL, err := url.Parse(targetURL); err == nil {
		// Create a URL relative to the base
		baseURL.Path = strings.TrimSuffix(baseURL.Path, strings.TrimPrefix(strings.TrimPrefix(targetURL, baseURL.Scheme+"://"+baseURL.Host), "/"))
		if relURL, err := url.Parse(basePath + urlStr); err == nil {
			resolvedPath := baseURL.ResolveReference(relURL).Path
			// Return path without the prefix
			return strings.TrimPrefix(resolvedPath, prefix)
		}
	}

	// Fallback: combine base path with URL
	return basePath + urlStr
}

