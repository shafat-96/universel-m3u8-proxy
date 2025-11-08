package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
)

var webServerURL string

func main() {
	// Load .env file if it exists
	_ = godotenv.Load()

	// Get server configuration
	host := getEnv("HOST", "localhost")
	port := getEnv("PORT", "3000")
	publicURL := os.Getenv("PUBLIC_URL")

	if publicURL != "" {
		webServerURL = publicURL
	} else {
		webServerURL = fmt.Sprintf("http://%s:%s", host, port)
	}

	// Create router
	r := mux.NewRouter()

	// Register routes
	r.HandleFunc("/proxy", m3u8ProxyHandler).Methods("GET")
	r.HandleFunc("/ts-proxy", tsProxyHandler).Methods("GET")
	r.HandleFunc("/mp4-proxy", mp4ProxyHandler).Methods("GET", "OPTIONS")

	// Health check
	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}).Methods("GET")

	// Catch-all route for path-based format (must be last)
	// Handles URLs like: /domain:port/path/to/file.m3u8
	r.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle OPTIONS for CORS preflight
		if r.Method == "OPTIONS" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Range")
			w.WriteHeader(http.StatusOK)
			return
		}
		
		// Check if path contains a domain-like pattern
		if strings.Contains(r.URL.Path, ".") {
			// Determine handler based on file extension
			if strings.Contains(r.URL.Path, ".m3u8") {
				m3u8PathProxyHandler(w, r)
			} else {
				tsPathProxyHandler(w, r)
			}
		} else {
			http.NotFound(w, r)
		}
	}).Methods("GET", "OPTIONS")

	// Start server
	addr := fmt.Sprintf("%s:%s", host, port)
	log.Printf("Starting proxy server on %s", addr)
	log.Printf("Public URL: %s", webServerURL)

	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatal(err)
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
