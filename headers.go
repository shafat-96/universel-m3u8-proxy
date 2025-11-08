package main

import (
	"net/url"
	"strings"
)

// HeaderConfig holds configuration for request headers
type HeaderConfig struct {
	Referer string
	Origin  string
}

// getHeaderConfig returns header configuration based on the target URL
func getHeaderConfig(targetURL string) HeaderConfig {
	u, err := url.Parse(targetURL)
	if err != nil {
		return HeaderConfig{
			Referer: "https://videostr.net/",
			Origin:  "https://videostr.net",
		}
	}

	domain := strings.ToLower(u.Hostname())

	// Check for specific domains that need videostr.net headers
	videostrDomains := []string{
		"1hd.su",
		"rainflare",
		"lightbeam",
		"videostr",
	}

	for _, d := range videostrDomains {
		if strings.Contains(domain, d) {
			return HeaderConfig{
				Referer: "https://videostr.net/",
				Origin:  "https://videostr.net",
			}
		}
	}

	// Default: use the target domain as referer/origin
	return HeaderConfig{
		Referer: u.Scheme + "://" + u.Host + "/",
		Origin:  u.Scheme + "://" + u.Host,
	}
}

// getDefaultHeaders returns default HTTP headers
func getDefaultHeaders() map[string]string {
	return map[string]string{
		"User-Agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Accept":          "*/*",
		"Accept-Language": "en-US,en;q=0.9",
		"Accept-Encoding": "gzip, deflate, br",
		"Connection":      "keep-alive",
		"Sec-Fetch-Dest":  "empty",
		"Sec-Fetch-Mode":  "cors",
		"Sec-Fetch-Site":  "cross-site",
	}
}

// generateRequestHeaders creates headers for the request
func generateRequestHeaders(targetURL string, additionalHeaders map[string]string) map[string]string {
	requestHeaders := getDefaultHeaders()
	
	// Get header config for the target URL
	config := getHeaderConfig(targetURL)
	requestHeaders["Referer"] = config.Referer
	requestHeaders["Origin"] = config.Origin

	// Merge additional headers (these override defaults)
	for k, v := range additionalHeaders {
		if v != "" {
			requestHeaders[k] = v
		}
	}

	return requestHeaders
}

// generateBasicHeaders creates minimal headers without domain-specific Referer/Origin
func generateBasicHeaders(additionalHeaders map[string]string) map[string]string {
	requestHeaders := getDefaultHeaders()

	// Merge additional headers (these override defaults)
	for k, v := range additionalHeaders {
		if v != "" {
			requestHeaders[k] = v
		}
	}

	return requestHeaders
}

// extractDomainFromPath extracts domain:port from path format like /f3.megacdn.co:2228/path
func extractDomainFromPath(path string) (string, string, bool) {
	// Remove leading slash
	path = strings.TrimPrefix(path, "/")
	
	// Split by first slash to get potential domain part
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 {
		return "", "", false
	}
	
	domainPart := parts[0]
	remainingPath := "/" + parts[1]
	
	// Check if it looks like domain:port or just domain
	if strings.Contains(domainPart, ":") || strings.Contains(domainPart, ".") {
		// Validate it has at least one dot (domain)
		if strings.Contains(domainPart, ".") {
			return domainPart, remainingPath, true
		}
	}
	
	return "", "", false
}
