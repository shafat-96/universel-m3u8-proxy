package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var sharedClient = &http.Client{
	Transport: &http.Transport{
		DisableKeepAlives:   false,
		MaxIdleConns:        2000,
		MaxIdleConnsPerHost: 500,
		IdleConnTimeout:     90 * time.Second,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("stopped after 5 redirects")
		}
		return nil
	},
}

// resolveURL resolves a relative URL against a base URL
func resolveURL(href, base string) string {
	baseURL, err := url.Parse(base)
	if err != nil {
		return href
	}

	relURL, err := url.Parse(href)
	if err != nil {
		return href
	}

	return baseURL.ResolveReference(relURL).String()
}

// validateRequest validates and extracts URL and headers from request
func validateRequest(r *http.Request) (string, map[string]string, error) {
	targetURL := r.URL.Query().Get("url")
	if targetURL == "" {
		return "", nil, fmt.Errorf("URL parameter is required")
	}

	parsedHeaders := make(map[string]string)
	headersParam := r.URL.Query().Get("headers")
	if headersParam != "" {
		decodedHeaders, err := url.QueryUnescape(headersParam)
		if err == nil {
			json.Unmarshal([]byte(decodedHeaders), &parsedHeaders)
		}
	}

	return targetURL, parsedHeaders, nil
}

// sendError sends an error response
func sendError(w http.ResponseWriter, message string, details interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error":   message,
		"details": details,
	})
}

// m3u8ProxyHandler handles M3U8 playlist proxying
func m3u8ProxyHandler(w http.ResponseWriter, r *http.Request) {
	targetURL, parsedHeaders, err := validateRequest(r)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
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
		sendError(w, "Failed to proxy m3u8 content", err.Error())
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		sendError(w, "Failed to read m3u8 content", err.Error())
		return
	}

	m3u8Content := string(body)
	lines := strings.Split(m3u8Content, "\n")
	newLines := make([]string, 0, len(lines))

	// Encode headers for URL parameters
	headersJSON, _ := json.Marshal(requestHeaders)
	encodedHeaders := url.QueryEscape(string(headersJSON))

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
						newURI := fmt.Sprintf("%s/ts-proxy?url=%s&headers=%s",
							webServerURL,
							url.QueryEscape(resolvedKeyURL),
							encodedHeaders)
						line = strings.Replace(line, originalURI, newURI, 1)
					}
				}
			}
			newLines = append(newLines, line)
		} else if trimmedLine != "" {
			// Trim the line to ensure clean URL resolution
			resolvedURL := resolveURL(trimmedLine, targetURL)
			var newURL string
			// Check if the resolved URL ends with .m3u8 (variant playlist)
			if strings.HasSuffix(strings.ToLower(resolvedURL), ".m3u8") {
				newURL = fmt.Sprintf("%s/proxy?url=%s&headers=%s",
					webServerURL,
					url.QueryEscape(resolvedURL),
					encodedHeaders)
			} else {
				// For all other files (segments, keys, etc.), use ts-proxy
				newURL = fmt.Sprintf("%s/ts-proxy?url=%s&headers=%s",
					webServerURL,
					url.QueryEscape(resolvedURL),
					encodedHeaders)
			}
			newLines = append(newLines, newURL)
		} else {
			newLines = append(newLines, line)
		}
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Write([]byte(strings.Join(newLines, "\n")))
}

// tsProxyHandler handles TS segment and general content proxying
func tsProxyHandler(w http.ResponseWriter, r *http.Request) {
	targetURL, parsedHeaders, err := validateRequest(r)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
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
		sendError(w, "Failed to proxy segment", err.Error())
		return
	}
	defer resp.Body.Close()

	// Determine content type
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		if strings.HasSuffix(targetURL, ".ts") {
			contentType = "video/mp2t"
		} else if strings.HasSuffix(targetURL, ".m3u8") {
			contentType = "application/vnd.apple.mpegurl"
		} else if strings.Contains(targetURL, ".jpg") || strings.Contains(targetURL, ".jpeg") ||
			strings.Contains(targetURL, ".png") || strings.Contains(targetURL, ".gif") ||
			strings.Contains(targetURL, ".webp") || strings.Contains(targetURL, ".bmp") ||
			strings.Contains(targetURL, ".svg") {
			contentType = "image/jpeg"
		} else {
			contentType = "application/octet-stream"
		}
	}

	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(resp.StatusCode)

	io.Copy(w, resp.Body)
}

// mp4ProxyHandler handles MP4 video proxying with range support
func mp4ProxyHandler(w http.ResponseWriter, r *http.Request) {
	targetURL, parsedHeaders, err := validateRequest(r)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Forward Range header if provided by the client
	rangeHeader := r.Header.Get("Range")
	if rangeHeader != "" {
		parsedHeaders["Range"] = rangeHeader
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
		sendError(w, "Failed to proxy mp4 content", err.Error())
		return
	}
	defer resp.Body.Close()

	// Set CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Range")

	// Use upstream headers when available
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "video/mp4"
	}
	w.Header().Set("Content-Type", contentType)

	if contentLength := resp.Header.Get("Content-Length"); contentLength != "" {
		w.Header().Set("Content-Length", contentLength)
	}

	if contentRange := resp.Header.Get("Content-Range"); contentRange != "" {
		w.Header().Set("Content-Range", contentRange)
	}

	acceptRanges := resp.Header.Get("Accept-Ranges")
	if acceptRanges == "" {
		acceptRanges = "bytes"
	}
	w.Header().Set("Accept-Ranges", acceptRanges)
	w.Header().Set("Content-Disposition", "inline")

	w.WriteHeader(resp.StatusCode)

	io.Copy(w, resp.Body)
}

// fetchHandler handles generic fetch requests with optional referer and custom headers
func fetchHandler(w http.ResponseWriter, r *http.Request) {
	targetURL := r.URL.Query().Get("url")
	if targetURL == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "URL parameter is required"})
		return
	}

	// Optional referer convenience param
	referer := r.URL.Query().Get("ref")

	// Optional header overrides via `headers` query param (URL-escaped JSON)
	parsedHeaders := make(map[string]string)
	if headersParam := r.URL.Query().Get("headers"); headersParam != "" {
		if decoded, err := url.QueryUnescape(headersParam); err == nil {
			_ = json.Unmarshal([]byte(decoded), &parsedHeaders)
		}
	}
	if referer != "" {
		parsedHeaders["Referer"] = referer
	}
	// Forward Range from client if present and not overridden
	if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
		if _, exists := parsedHeaders["Range"]; !exists {
			parsedHeaders["Range"] = rangeHeader
		}
	}

	// Generate headers tailored to the target domain, allowing overrides
	requestHeaders := generateRequestHeaders(targetURL, parsedHeaders)

	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Request failed",
			"error":   err.Error(),
		})
		return
	}

	for k, v := range requestHeaders {
		if v != "" {
			req.Header.Set(k, v)
		}
	}

	resp, err := sharedClient.Do(req)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Request failed",
			"error":   err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	// Propagate upstream content headers when useful
	if contentType := resp.Header.Get("Content-Type"); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	if contentLength := resp.Header.Get("Content-Length"); contentLength != "" {
		w.Header().Set("Content-Length", contentLength)
	}
	if contentRange := resp.Header.Get("Content-Range"); contentRange != "" {
		w.Header().Set("Content-Range", contentRange)
	}
	if acceptRanges := resp.Header.Get("Accept-Ranges"); acceptRanges != "" {
		w.Header().Set("Accept-Ranges", acceptRanges)
	}

	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// ghostProxyHandler handles requests through a Ghost IP proxy
// URL format: /ghost-proxy?url={target_url}&proxy={proxy_url}&headers={optional_headers}
func ghostProxyHandler(w http.ResponseWriter, r *http.Request) {
	targetURL := r.URL.Query().Get("url")
	if targetURL == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "URL parameter is required"})
		return
	}

	// Get proxy URL (default to the Ghost IP from the example)
	proxyURL := r.URL.Query().Get("proxy")
	if proxyURL == "" {
		proxyURL = "http://5.231.61.237:8080"
	}

	// Parse proxy URL
	parsedProxyURL, err := url.Parse(proxyURL)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "Invalid proxy URL",
			"details": err.Error(),
		})
		return
	}

	// Optional header overrides via `headers` query param (URL-escaped JSON)
	parsedHeaders := make(map[string]string)
	if headersParam := r.URL.Query().Get("headers"); headersParam != "" {
		if decoded, err := url.QueryUnescape(headersParam); err == nil {
			_ = json.Unmarshal([]byte(decoded), &parsedHeaders)
		}
	}

	// Generate headers tailored to the target domain, allowing overrides
	requestHeaders := generateRequestHeaders(targetURL, parsedHeaders)

	// Create a client with proxy
	proxyClient := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(parsedProxyURL),
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("stopped after 5 redirects")
			}
			return nil
		},
	}

	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Request failed",
			"error":   err.Error(),
		})
		return
	}

	// Set headers
	for k, v := range requestHeaders {
		if v != "" {
			req.Header.Set(k, v)
		}
	}

	// Forward Range from client if present and not overridden
	if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
		if _, exists := requestHeaders["Range"]; !exists {
			req.Header.Set("Range", rangeHeader)
		}
	}

	resp, err := proxyClient.Do(req)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Request through proxy failed",
			"error":   err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	// Check if it's an M3U8 file
	contentType := resp.Header.Get("Content-Type")
	isM3U8 := strings.Contains(contentType, "mpegurl") ||
		strings.HasSuffix(strings.ToLower(targetURL), ".m3u8")

	if isM3U8 {
		// Read and process M3U8 content
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		m3u8Content := string(body)
		lines := strings.Split(m3u8Content, "\n")
		newLines := make([]string, 0, len(lines))

		// Encode headers and proxy for URL parameters
		headersJSON, _ := json.Marshal(requestHeaders)
		encodedHeaders := url.QueryEscape(string(headersJSON))
		encodedProxy := url.QueryEscape(proxyURL)

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
							newURI := fmt.Sprintf("%s/ghost-proxy?url=%s&proxy=%s&headers=%s",
								webServerURL,
								url.QueryEscape(resolvedKeyURL),
								encodedProxy,
								encodedHeaders)
							line = strings.Replace(line, originalURI, newURI, 1)
						}
					}
				}
				newLines = append(newLines, line)
			} else if trimmedLine != "" {
				// Resolve and proxy segment URLs
				resolvedURL := resolveURL(trimmedLine, targetURL)
				var newURL string
				// Check if the resolved URL ends with .m3u8 (variant playlist)
				if strings.HasSuffix(strings.ToLower(resolvedURL), ".m3u8") {
					newURL = fmt.Sprintf("%s/ghost-proxy?url=%s&proxy=%s&headers=%s",
						webServerURL,
						url.QueryEscape(resolvedURL),
						encodedProxy,
						encodedHeaders)
				} else {
					// For all other files (segments, keys, etc.)
					newURL = fmt.Sprintf("%s/ghost-proxy?url=%s&proxy=%s&headers=%s",
						webServerURL,
						url.QueryEscape(resolvedURL),
						encodedProxy,
						encodedHeaders)
				}
				newLines = append(newLines, newURL)
			} else {
				newLines = append(newLines, line)
			}
		}

		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Write([]byte(strings.Join(newLines, "\n")))
	} else {
		// Stream non-M3U8 content directly
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		if contentLength := resp.Header.Get("Content-Length"); contentLength != "" {
			w.Header().Set("Content-Length", contentLength)
		}
		if contentRange := resp.Header.Get("Content-Range"); contentRange != "" {
			w.Header().Set("Content-Range", contentRange)
		}
		if acceptRanges := resp.Header.Get("Accept-Ranges"); acceptRanges != "" {
			w.Header().Set("Accept-Ranges", acceptRanges)
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}
}
