package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// pathProxyHandler handles HLS proxying where the URL is in the path
// Example: http://localhost:3000/nightbreeze17.site/file2/.../playlist.m3u8
func pathProxyHandler(w http.ResponseWriter, r *http.Request) {
	// Reconstruct the target URL from the path
	path := r.URL.Path

	// Remove leading slash and add https://
	targetURL := "https://" + strings.TrimPrefix(path, "/")

	// Add back query parameters if any
	if r.URL.RawQuery != "" {
		targetURL = targetURL + "?" + r.URL.RawQuery
	}

	// Get optional headers from query param
	parsedHeaders := map[string]string{
		"Referer":    "https://videostr.net/",
		"User-Agent": "Mozilla/5.0",
	}
	headersParam := r.URL.Query().Get("headers")
	if headersParam != "" {
		decodedHeaders, err := url.QueryUnescape(headersParam)
		if err == nil {
			json.Unmarshal([]byte(decodedHeaders), &parsedHeaders)
		}
	}

	requestHeaders := generateRequestHeaders(targetURL, parsedHeaders)

	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		sendError(w, "Failed to create request", err.Error())
		return
	}

	for k, v := range requestHeaders {
		req.Header.Set(k, v)
	}

	resp, err := sharedClient.Do(req)
	if err != nil {
		sendError(w, "Failed to proxy content", err.Error())
		return
	}
	defer resp.Body.Close()

	// Check if this is an M3U8 playlist (needs URL rewriting)
	contentType := resp.Header.Get("Content-Type")
	isM3U8 := isM3U8URL(targetURL) || strings.Contains(contentType, "mpegurl") || strings.Contains(contentType, "m3u8")

	if isM3U8 {
		// M3U8: Read all, process URLs, then send
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			sendError(w, "Failed to read content", err.Error())
			return
		}
		content := string(body)
		if strings.Contains(content, "#EXTM3U") {
			content = processM3U8Content(content, targetURL, requestHeaders)
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Write([]byte(content))
	} else {
		// Segments: Stream directly for progressive playback
		if contentType == "" {
			if strings.HasSuffix(targetURL, ".ts") {
				contentType = "video/mp2t"
			} else if strings.HasSuffix(targetURL, ".mp4") {
				contentType = "video/mp4"
			} else {
				contentType = "application/octet-stream"
			}
		}
		w.Header().Set("Content-Type", contentType)
		io.Copy(w, resp.Body)
	}
}

// processM3U8Content processes M3U8 content and rewrites URLs
func processM3U8Content(m3u8Content, targetURL string, requestHeaders map[string]string) string {
	// Normalize line endings
	m3u8Content = strings.ReplaceAll(m3u8Content, "\r\n", "\n")
	m3u8Content = strings.ReplaceAll(m3u8Content, "\r", "\n")

	lines := strings.Split(m3u8Content, "\n")
	newLines := make([]string, 0, len(lines))

	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		if strings.HasPrefix(trimmedLine, "#") {
			// Handle URI in tags (e.g., encryption keys)
			if strings.Contains(line, "URI=") {
				if start := strings.Index(line, `URI="`); start != -1 {
					start += 5 // len(`URI="`)
					if end := strings.Index(line[start:], `"`); end != -1 {
						originalURI := line[start : start+end]
						resolvedKeyURL := resolveURL(originalURI, targetURL)
						// Remove https:// or http:// for path-based proxy
						keyProxyPath := strings.TrimPrefix(resolvedKeyURL, "https://")
						keyProxyPath = strings.TrimPrefix(keyProxyPath, "http://")
						newURI := fmt.Sprintf("%s/%s", webServerURL, keyProxyPath)
						line = strings.Replace(line, originalURI, newURI, 1)
					}
				}
			}
			newLines = append(newLines, line)
		} else if trimmedLine != "" {
			resolvedURL := resolveURL(trimmedLine, targetURL)

			// Remove https:// or http:// from the URL for the path format
			proxyPath := strings.TrimPrefix(resolvedURL, "https://")
			proxyPath = strings.TrimPrefix(proxyPath, "http://")

			// Build proxy URL without headers in URL (headers used only in HTTP request)
			newURL := fmt.Sprintf("%s/%s", webServerURL, proxyPath)
			newLines = append(newLines, newURL)
		} else {
			newLines = append(newLines, line)
		}
	}

	return strings.Join(newLines, "\n")
}
