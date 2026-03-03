package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"xfer/internal/server"
)

func main() {
	// Parse command-line flags
	port := flag.String("port", getEnv("XFER_PORT", ":8080"), "Server port")
	baseURL := flag.String("base-url", getEnv("XFER_BASE_URL", "http://localhost:8080"), "Public base URL")
	sessionTTL := flag.Duration("session-ttl", parseDuration(getEnv("XFER_SESSION_TTL", "5m")), "Session TTL")
	maxSize := flag.Int64("max-size", parseInt64(getEnv("XFER_MAX_SIZE", "209715200")), "Max file size (bytes)")
	flag.Parse()

	// Create server config
	config := &server.Config{
		Port:       *port,
		BaseURL:    *baseURL,
		SessionTTL: *sessionTTL,
		MaxSize:    *maxSize,
	}

	// Create and start server
	srv := server.New(config)

	// Start server in goroutine
	go func() {
		if err := srv.Start(); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server shutdown error: %v", err)
	}

	log.Println("Server stopped")
}

// getEnv gets environment variable with fallback
func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// parseDuration parses duration string
func parseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 5 * time.Minute
	}
	return d
}

// parseInt64 parses int64 string
func parseInt64(s string) int64 {
	var result int64
	_, err := fmt.Sscanf(s, "%d", &result)
	if err != nil {
		return 209715200 // 200MB default
	}
	return result
}
