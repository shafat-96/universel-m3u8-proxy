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

// m3u8ProxyHandler handles M3U8 playlist proxying
func m3u8ProxyHandler(w http.ResponseWriter, r *http.Request) {
	targetURL, parsedHeaders, err := validateRequest(r)
	if err != nil {
		sendError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}

	// Generate request headers
	requestHeaders := generateRequestHeaders(targetURL, parsedHeaders)

	// Fetch the M3U8 content
	resp, err := makeRequest(targetURL, requestHeaders, nil)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "Failed to fetch m3u8 content", err.Error())
		return
	}
	defer resp.Body.Close()

	body, err := readResponseBody(resp)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "Failed to read m3u8 content", err.Error())
		return
	}

	// Process M3U8 content
	m3u8Content := string(body)
	lines := strings.Split(m3u8Content, "\n")
	newLines := make([]string, 0, len(lines))

	for _, line := range lines {
		if strings.HasPrefix(line, "#") {
			// Handle key URIs
			if strings.Contains(line, "URI=") {
				newLines = append(newLines, processKeyURI(line, targetURL, requestHeaders))
			} else {
				newLines = append(newLines, line)
			}
		} else if strings.TrimSpace(line) != "" {
			// Handle segment URLs
			newLines = append(newLines, processSegmentURL(line, targetURL, requestHeaders))
		} else {
			newLines = append(newLines, line)
		}
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Write([]byte(strings.Join(newLines, "\n")))
}

// tsProxyHandler handles TS segment and general file proxying
func tsProxyHandler(w http.ResponseWriter, r *http.Request) {
	targetURL, parsedHeaders, err := validateRequest(r)
	if err != nil {
		sendError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}

	// Generate request headers
	requestHeaders := generateRequestHeaders(targetURL, parsedHeaders)

	// Fetch the content
	resp, err := makeRequest(targetURL, requestHeaders, nil)
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
		log.Printf("Error streaming response: %v", err)
	}
}

// mp4ProxyHandler handles MP4 video proxying with range support
func mp4ProxyHandler(w http.ResponseWriter, r *http.Request) {
	targetURL, parsedHeaders, err := validateRequest(r)
	if err != nil {
		sendError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}

	// Forward Range header if provided
	rangeHeader := r.Header.Get("Range")

	// Generate request headers
	requestHeaders := generateRequestHeaders(targetURL, parsedHeaders)
	if rangeHeader != "" {
		requestHeaders["Range"] = rangeHeader
	}

	// Fetch the content
	resp, err := makeRequest(targetURL, requestHeaders, nil)
	if err != nil {
		sendError(w, http.StatusBadGateway, "Failed to proxy mp4 content", err.Error())
		return
	}
	defer resp.Body.Close()

	// Set CORS and pass-through headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Range")

	// Copy upstream headers
	if contentType := resp.Header.Get("Content-Type"); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	} else {
		w.Header().Set("Content-Type", "video/mp4")
	}

	if contentLength := resp.Header.Get("Content-Length"); contentLength != "" {
		w.Header().Set("Content-Length", contentLength)
	}

	if contentRange := resp.Header.Get("Content-Range"); contentRange != "" {
		w.Header().Set("Content-Range", contentRange)
	}

	if acceptRanges := resp.Header.Get("Accept-Ranges"); acceptRanges != "" {
		w.Header().Set("Accept-Ranges", acceptRanges)
	} else {
		w.Header().Set("Accept-Ranges", "bytes")
	}

	w.Header().Set("Content-Disposition", "inline")
	w.WriteHeader(resp.StatusCode)

	// Stream the response
	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Printf("Error streaming mp4 response: %v", err)
	}
}

// fetchHandler handles generic URL fetching with optional referer
func fetchHandler(w http.ResponseWriter, r *http.Request) {
	targetURL := r.URL.Query().Get("url")
	if targetURL == "" {
		sendError(w, http.StatusBadRequest, "URL parameter is required", nil)
		return
	}

	referer := r.URL.Query().Get("ref")

	// Generate request headers
	requestHeaders := generateRequestHeaders(targetURL, nil)
	if referer != "" {
		requestHeaders["Referer"] = referer
	}

	// Fetch the content
	resp, err := makeRequest(targetURL, requestHeaders, nil)
	if err != nil {
		sendError(w, http.StatusBadGateway, "Failed to fetch content", err.Error())
		return
	}
	defer resp.Body.Close()

	// Pass through status and content type
	if contentType := resp.Header.Get("Content-Type"); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}

	w.WriteHeader(resp.StatusCode)

	// Stream the response
	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Printf("Error streaming fetch response: %v", err)
	}
}

// processKeyURI processes encryption key URIs in M3U8 playlists
func processKeyURI(line, baseURL string, headers map[string]string) string {
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

	// Resolve relative URIs
	resolvedURL := resolveURL(baseURL, originalURI)

	// Create proxy URL
	encodedHeaders, _ := json.Marshal(headers)
	newURI := fmt.Sprintf("%s/ts-proxy?url=%s&headers=%s",
		webServerURL,
		url.QueryEscape(resolvedURL),
		url.QueryEscape(string(encodedHeaders)))

	return strings.Replace(line, originalURI, newURI, 1)
}

// processSegmentURL processes segment URLs in M3U8 playlists
func processSegmentURL(line, baseURL string, headers map[string]string) string {
	resolvedURL := resolveURL(baseURL, strings.TrimSpace(line))

	encodedHeaders, _ := json.Marshal(headers)

	// Determine endpoint based on file extension
	endpoint := "/ts-proxy"
	if strings.HasSuffix(strings.ToLower(line), ".m3u8") {
		endpoint = "/proxy"
	}

	newURL := fmt.Sprintf("%s%s?url=%s&headers=%s",
		webServerURL,
		endpoint,
		url.QueryEscape(resolvedURL),
		url.QueryEscape(string(encodedHeaders)))

	return newURL
}

// detectContentType determines content type based on URL extension
func detectContentType(targetURL string) string {
	lowerURL := strings.ToLower(targetURL)

	if strings.HasSuffix(lowerURL, ".ts") {
		return "video/mp2t"
	} else if strings.HasSuffix(lowerURL, ".m3u8") {
		return "application/vnd.apple.mpegurl"
	} else if strings.Contains(lowerURL, ".jpg") || strings.Contains(lowerURL, ".jpeg") {
		return "image/jpeg"
	} else if strings.Contains(lowerURL, ".png") {
		return "image/png"
	} else if strings.Contains(lowerURL, ".gif") {
		return "image/gif"
	} else if strings.Contains(lowerURL, ".webp") {
		return "image/webp"
	} else if strings.Contains(lowerURL, ".svg") {
		return "image/svg+xml"
	}

	return "application/octet-stream"
}

// sendError sends a JSON error response
func sendError(w http.ResponseWriter, statusCode int, message string, details interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	errorResponse := map[string]interface{}{
		"error": message,
	}
	if details != nil {
		errorResponse["details"] = details
		log.Printf("%s: %v", message, details)
	} else {
		log.Printf("%s", message)
	}

	json.NewEncoder(w).Encode(errorResponse)
}
