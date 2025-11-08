package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

var (
	webServerURL   string
	allowedOrigins []string
)

func main() {
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using default values")
	}

	// Get configuration from environment
	host := getEnv("HOST", "localhost")
	port := getEnv("PORT", "3000")
	publicURL := getEnv("PUBLIC_URL", fmt.Sprintf("http://%s:%s", host, port))
	webServerURL = publicURL

	// Parse allowed origins
	originsEnv := os.Getenv("ALLOWED_ORIGINS")
	if originsEnv != "" {
		allowedOrigins = strings.Split(originsEnv, ",")
		for i := range allowedOrigins {
			allowedOrigins[i] = strings.TrimSpace(allowedOrigins[i])
		}
	}

	// Setup routes with smart router
	http.HandleFunc("/", smartRouter)

	// Start server
	addr := fmt.Sprintf("%s:%s", host, port)
	log.Printf("M3U8 Proxy Server running at http://%s", addr)
	if len(allowedOrigins) > 0 {
		log.Printf("Allowed origins: %s", strings.Join(allowedOrigins, ", "))
	} else {
		log.Println("Allowed origins: All (*)")
	}

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

// smartRouter intelligently routes requests based on path and query parameters
func smartRouter(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	query := r.URL.Query()

	// Apply CORS middleware
	origin := r.Header.Get("Origin")
	if len(allowedOrigins) == 0 {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	} else if origin != "" && contains(allowedOrigins, origin) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
	}
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Range")
	w.Header().Set("Access-Control-Allow-Credentials", "true")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Route based on path and parameters
	if path == "/" {
		homeHandler(w, r)
		return
	}

	// Check for specific endpoints first
	if path == "/proxy" {
		m3u8ProxyHandler(w, r)
		return
	}
	if path == "/ts-proxy" {
		tsProxyHandler(w, r)
		return
	}
	if path == "/mp4-proxy" {
		mp4ProxyHandler(w, r)
		return
	}
	if path == "/fetch" {
		fetchHandler(w, r)
		return
	}

	// Universal HLS proxy - any path with 'host' parameter (handles /file1/, /file2/, etc.)
	if query.Get("host") != "" && len(path) > 1 {
		universalHLSProxyHandler(w, r)
		return
	}

	// Default 404
	sendError(w, http.StatusNotFound, "Endpoint not found", nil)
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	allowedOriginsStr := "All (*)"
	if len(allowedOrigins) > 0 {
		allowedOriginsStr = strings.Join(allowedOrigins, ", ")
	}

	response := fmt.Sprintf(`{
  "message": "M3U8 Cross-Origin Proxy Server - Universal HLS Proxy",
  "endpoints": {
    "m3u8": "/proxy?url={m3u8_url}&headers={optional_headers}",
    "ts": "/ts-proxy?url={ts_segment_url}&headers={optional_headers}",
    "fetch": "/fetch?url={any_url}&ref={optional_referer}",
    "mp4": "/mp4-proxy?url={mp4_url}&headers={optional_headers}",
    "universal-hls": "ANY_PATH?host={host_url}&headers={optional_headers}",
    "note": "Universal HLS proxy works with ANY path pattern when 'host' parameter is present (including /file1/, /file2/, /hls-playback/, etc.)"
  },
  "examples": [
    "/hls-playback/path/file.m3u8?host=https://example.com",
    "/v3-hls-playback/path/file.m3u8?host=https://example.com",
    "/file1/path/video.m3u8?host=https://example.com",
    "/stream/01/03/hash/uwu.m3u8?host=https://example.com",
    "/any/custom/path/video.m3u8?host=https://example.com"
  ],
  "allowedOrigins": "%s"
}`, allowedOriginsStr)

	w.Write([]byte(response))
}

func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// If no allowed origins are specified, allow all (*)
		if len(allowedOrigins) == 0 {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else if origin != "" && contains(allowedOrigins, origin) {
			// If allowed origins are specified, check if the request origin is in the list
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Range")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next(w, r)
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
