package main

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

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

// generateRequestHeaders generates headers for the request with default values
func generateRequestHeaders(targetURL string, additionalHeaders map[string]string) map[string]string {
	requestHeaders := make(map[string]string)

	// Set default headers
	requestHeaders["User-Agent"] = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:137.0) Gecko/20100101 Firefox/137.0"
	requestHeaders["Accept"] = "*/*"
	requestHeaders["Accept-Language"] = "en-US,en;q=0.5"
	requestHeaders["Accept-Encoding"] = "gzip, deflate"
	requestHeaders["Connection"] = "keep-alive"

	// Merge additional headers (they override default headers)
	for k, v := range additionalHeaders {
		if v != "" {
			requestHeaders[k] = v
		}
	}

	return requestHeaders
}

// makeRequest makes an HTTP request with the given headers
func makeRequest(targetURL string, headers map[string]string, rangeHeader *string) (*http.Response, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Allow up to 5 redirects
			if len(via) >= 5 {
				return fmt.Errorf("stopped after 5 redirects")
			}
			return nil
		},
	}

	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		return nil, err
	}

	// Set headers
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	// Set range header if provided
	if rangeHeader != nil && *rangeHeader != "" {
		req.Header.Set("Range", *rangeHeader)
	}

	return client.Do(req)
}

// readResponseBody reads and decompresses response body if needed
func readResponseBody(resp *http.Response) ([]byte, error) {
	var reader io.ReadCloser = resp.Body
	
	// Check if response is gzip-compressed
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Encoding")), "gzip") {
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		reader = gz
	}
	
	return io.ReadAll(reader)
}

// resolveURL resolves a relative URL against a base URL
func resolveURL(baseURL, relativeURL string) string {
	base, err := url.Parse(baseURL)
	if err != nil {
		return relativeURL
	}

	rel, err := url.Parse(relativeURL)
	if err != nil {
		return relativeURL
	}

	resolved := base.ResolveReference(rel)
	return resolved.String()
}

// normalizeHeaders normalizes header keys to standard format
func normalizeHeaders(headers map[string]interface{}) map[string]string {
	normalized := make(map[string]string)
	for k, v := range headers {
		if str, ok := v.(string); ok {
			normalized[strings.ToLower(k)] = str
		}
	}
	return normalized
}
