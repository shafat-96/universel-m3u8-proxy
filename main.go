package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

var (
	webServerURL   string
	allowedOrigins []string
)

func main() {
	// Load .env file
	godotenv.Load()

	// Get configuration from environment
	host := getEnv("HOST", "localhost")
	port := getEnv("PORT", "3000")
	publicURL := getEnv("PUBLIC_URL", fmt.Sprintf("http://%s:%s", host, port))
	webServerURL = publicURL

	// Parse allowed origins
	allowedOriginsStr := os.Getenv("ALLOWED_ORIGINS")
	if allowedOriginsStr != "" {
		allowedOrigins = strings.Split(allowedOriginsStr, ",")
		for i := range allowedOrigins {
			allowedOrigins[i] = strings.TrimSpace(allowedOrigins[i])
		}
	}

	// Configure default transport
	http.DefaultTransport.(*http.Transport).MaxIdleConnsPerHost = 500

	// Setup routes with custom handler
	http.HandleFunc("/", routeHandler)

	// Create server with timeouts
	addr := fmt.Sprintf("%s:%s", host, port)
	server := &http.Server{
		Addr:         addr,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	log.Printf("M3U8 Proxy Server running at http://%s", addr)

	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func routeHandler(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Route to specific handlers based on path
	switch {
	case path == "/":
		homeHandler(w, r)
	case path == "/proxy":
		corsMiddleware(m3u8ProxyHandler)(w, r)
	case path == "/ts-proxy":
		corsMiddleware(tsProxyHandler)(w, r)
	case path == "/mp4-proxy":
		corsMiddleware(mp4ProxyHandler)(w, r)
	case path == "/fetch":
		corsMiddleware(fetchHandler)(w, r)
	case path == "/ghost-proxy":
		corsMiddleware(ghostProxyHandler)(w, r)
	default:
		// Unknown endpoint
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error": "Endpoint not found"}`))
	}
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		allowedOriginsDisplay := "All (*)"
		if len(allowedOrigins) > 0 {
			allowedOriginsDisplay = strings.Join(allowedOrigins, ", ")
		}

		response := fmt.Sprintf(`{
  "message": "M3U8 Cross-Origin Proxy Server",
  "endpoints": {
    "m3u8": "/proxy?url={m3u8_url}&headers={optional_headers}",
    "ts": "/ts-proxy?url={ts_segment_url}&headers={optional_headers}",
    "fetch": "/fetch?url={any_url}&ref={optional_referer}",
    "mp4": "/mp4-proxy?url={mp4_url}&headers={optional_headers}",
    "ghost": "/ghost-proxy?url={target_url}&proxy={proxy_url}&headers={optional_headers}"
  },
  "allowedOrigins": "%s"
}`, allowedOriginsDisplay)

		w.Write([]byte(response))
	})(w, r)
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
