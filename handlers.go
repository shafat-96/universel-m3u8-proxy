package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// validateRequest validates and extracts URL and headers from request
func validateRequest(r *http.Request) (string, map[string]string, error) {
	targetURL := r.URL.Query().Get("url")
	if targetURL == "" {
		return "", nil, fmt.Errorf("URL parameter is required")
	}

	headers := make(map[string]string)
	if h := r.URL.Query().Get("headers"); h != "" {
		if decoded, err := url.QueryUnescape(h); err == nil {
			_ = json.Unmarshal([]byte(decoded), &headers)
		}
	}

	return targetURL, headers, nil
}

func sendError(w http.ResponseWriter, message string, statusCode int, details interface{}) {
	log.Printf("%s: %v", message, details)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error":   message,
		"details": details,
	})
}

func newHTTPClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
}

func streamRequest(w http.ResponseWriter, targetURL string, headers map[string]string, cors bool) error {
	client := newHTTPClient()
	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		return err
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if cors {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Range")
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = guessContentType(targetURL)
	}
	w.Header().Set("Content-Type", contentType)

	if cl := resp.Header.Get("Content-Length"); cl != "" {
		w.Header().Set("Content-Length", cl)
	}

	if cr := resp.Header.Get("Content-Range"); cr != "" {
		w.Header().Set("Content-Range", cr)
	}

	if ar := resp.Header.Get("Accept-Ranges"); ar == "" {
		w.Header().Set("Accept-Ranges", "bytes")
	} else {
		w.Header().Set("Accept-Ranges", ar)
	}

	w.WriteHeader(resp.StatusCode)
	_, err = io.Copy(w, bufio.NewReader(resp.Body))
	return err
}

func guessContentType(urlStr string) string {
	lower := strings.ToLower(urlStr)
	switch {
	case strings.HasSuffix(lower, ".ts"):
		return "video/mp2t"
	case strings.HasSuffix(lower, ".m3u8"):
		return "application/vnd.apple.mpegurl"
	case regexp.MustCompile(`\.(jpe?g|png|gif|webp|bmp|svg)(?:\?|#|$)`).MatchString(lower):
		return "image/jpeg"
	default:
		return "application/octet-stream"
	}
}

func resolveURL(href, base string) string {
	if strings.HasPrefix(href, "http") {
		return href
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return href
	}
	if strings.HasPrefix(href, "/") {
		return baseURL.Scheme + "://" + baseURL.Host + href
	}
	rel, err := url.Parse(href)
	if err != nil {
		return href
	}
	return baseURL.ResolveReference(rel).String()
}

func convertURLToPath(fullURL string) string {
	u, err := url.Parse(fullURL)
	if err != nil {
		return fullURL
	}
	return "/" + u.Host + u.Path
}

// --- Handlers ---

func m3u8ProxyHandler(w http.ResponseWriter, r *http.Request) {
	targetURL, parsedHeaders, err := validateRequest(r)
	if err != nil {
		sendError(w, err.Error(), http.StatusBadRequest, nil)
		return
	}

	requestHeaders := generateRequestHeaders(targetURL, parsedHeaders)
	body, err := fetchContent(targetURL, requestHeaders)
	if err != nil {
		sendError(w, "Failed to fetch m3u8 content", http.StatusBadGateway, err.Error())
		return
	}

	lines := strings.Split(string(body), "\n")
	newLines := make([]string, 0, len(lines))
	headersJSON, _ := json.Marshal(requestHeaders)
	encodedHeaders := url.QueryEscape(string(headersJSON))
	uriPattern := regexp.MustCompile(`URI="([^"]+)"`)

	for _, line := range lines {
		if strings.HasPrefix(line, "#") {
			if strings.Contains(line, "URI=") {
				matches := uriPattern.FindStringSubmatch(line)
				if len(matches) > 1 {
					originalURI := matches[1]
					resolvedKeyURL := resolveURL(originalURI, targetURL)
					newURI := fmt.Sprintf("%s/ts-proxy?url=%s&headers=%s",
						webServerURL, url.QueryEscape(resolvedKeyURL), encodedHeaders)
					line = strings.Replace(line, originalURI, newURI, 1)
				}
			}
			newLines = append(newLines, line)
		} else if strings.TrimSpace(line) != "" {
			resolvedURL := resolveURL(line, targetURL)
			newURL := fmt.Sprintf("%s/ts-proxy?url=%s&headers=%s",
				webServerURL, url.QueryEscape(resolvedURL), encodedHeaders)
			newLines = append(newLines, newURL)
		} else {
			newLines = append(newLines, line)
		}
	}

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Range")
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(strings.Join(newLines, "\n")))
}

func tsProxyHandler(w http.ResponseWriter, r *http.Request) {
	targetURL, parsedHeaders, err := validateRequest(r)
	if err != nil {
		sendError(w, err.Error(), http.StatusBadRequest, nil)
		return
	}
	err = streamRequest(w, targetURL, generateRequestHeaders(targetURL, parsedHeaders), true)
	if err != nil {
		log.Printf("TS proxy error: %v", err)
	}
}

func mp4ProxyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Range")
		w.WriteHeader(http.StatusOK)
		return
	}

	targetURL, parsedHeaders, err := validateRequest(r)
	if err != nil {
		sendError(w, err.Error(), http.StatusBadRequest, nil)
		return
	}

	if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
		parsedHeaders["Range"] = rangeHeader
	}

	err = streamRequest(w, targetURL, generateRequestHeaders(targetURL, parsedHeaders), true)
	if err != nil {
		log.Printf("MP4 proxy error: %v", err)
	}
}

func m3u8PathProxyHandler(w http.ResponseWriter, r *http.Request) {
	domain, remaining, ok := extractDomainFromPath(r.URL.Path)
	if !ok {
		sendError(w, "Invalid path format", http.StatusBadRequest, nil)
		return
	}

	targetURL := "https://" + domain + remaining
	headers := make(map[string]string)
	if h := r.URL.Query().Get("headers"); h != "" {
		if decoded, err := url.QueryUnescape(h); err == nil {
			_ = json.Unmarshal([]byte(decoded), &headers)
		}
	}

	requestHeaders := generateBasicHeaders(headers)
	body, err := fetchContent(targetURL, requestHeaders)
	if err != nil {
		sendError(w, "Failed to fetch m3u8 content", http.StatusBadGateway, err.Error())
		return
	}

	lines := strings.Split(string(body), "\n")
	newLines := make([]string, 0, len(lines))
	uriPattern := regexp.MustCompile(`URI="([^"]+)"`)

	for _, line := range lines {
		if strings.HasPrefix(line, "#") && strings.Contains(line, "URI=") {
			matches := uriPattern.FindStringSubmatch(line)
			if len(matches) > 1 {
				originalURI := matches[1]
				resolvedKeyURL := resolveURL(originalURI, targetURL)
				line = strings.Replace(line, originalURI, convertURLToPath(resolvedKeyURL), 1)
			}
		} else if strings.TrimSpace(line) != "" {
			resolvedURL := resolveURL(line, targetURL)
			line = convertURLToPath(resolvedURL)
		}
		newLines = append(newLines, line)
	}

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Range")
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(strings.Join(newLines, "\n")))
}

func tsPathProxyHandler(w http.ResponseWriter, r *http.Request) {
	domain, remaining, ok := extractDomainFromPath(r.URL.Path)
	if !ok {
		sendError(w, "Invalid path format", http.StatusBadRequest, nil)
		return
	}
	targetURL := "https://" + domain + remaining
	headers := make(map[string]string)
	if h := r.URL.Query().Get("headers"); h != "" {
		if decoded, err := url.QueryUnescape(h); err == nil {
			_ = json.Unmarshal([]byte(decoded), &headers)
		}
	}

	err := streamRequest(w, targetURL, generateBasicHeaders(headers), true)
	if err != nil {
		log.Printf("TS path proxy error: %v", err)
	}
}

// Fetch content helper for m3u8 handlers
func fetchContent(targetURL string, headers map[string]string) ([]byte, error) {
	client := newHTTPClient()
	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
