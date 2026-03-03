package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"
)

// Config holds server configuration
type Config struct {
	Port       string
	BaseURL    string
	SessionTTL time.Duration
	MaxSize    int64 // Maximum file size in bytes
}

// Server represents the relay server
type Server struct {
	config       *Config
	sessionStore *SessionStore
	server       *http.Server
}

// New creates a new server instance
func New(config *Config) *Server {
	return &Server{
		config:       config,
		sessionStore: NewSessionStore(config.SessionTTL),
	}
}

// Start starts the HTTP server
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Register routes
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/sessions", s.handleCreateSession)
	mux.HandleFunc("/api/sessions/", s.handleWebSocket) // WebSocket endpoint
	mux.HandleFunc("/download/", s.handleDownloadPage)
	mux.HandleFunc("/upload/", s.handleUploadPage)

	s.server = &http.Server{
		Addr:         s.config.Port,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start background cleanup routine
	go s.cleanupRoutine()

	log.Printf("Starting server on %s (public URL: %s)", s.config.Port, s.config.BaseURL)
	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	log.Println("Shutting down server...")
	return s.server.Shutdown(ctx)
}

// cleanupRoutine periodically removes expired sessions
func (s *Server) cleanupRoutine() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		count := s.sessionStore.CleanupExpired()
		if count > 0 {
			log.Printf("Cleaned up %d expired sessions", count)
		}
	}
}

// handleHealth returns server health status
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"healthy","sessions":%d}`, s.sessionStore.Count())
}
