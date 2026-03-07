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

	// Rate limiting configuration
	RateLimitSessions  int  // Sessions per minute
	RateLimitWebSocket int  // WebSocket connections per minute
	RateLimitPages     int  // Page requests per minute
	EnableRateLimit    bool // Enable rate limiting
}

// Server represents the relay server
type Server struct {
	config       *Config
	sessionStore *SessionStore
	csrfManager  *CSRFManager
	server       *http.Server
}

// New creates a new server instance
func New(config *Config) *Server {
	return &Server{
		config:       config,
		sessionStore: NewSessionStore(config.SessionTTL),
		csrfManager:  NewCSRFManager(),
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

	// Apply middleware chain
	handler := ApplyMiddleware(mux, s.config)

	s.server = &http.Server{
		Addr:         s.config.Port,
		Handler:      handler,
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

// Handler returns an http.Handler for the server (useful for testing)
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/sessions", s.handleCreateSession)
	mux.HandleFunc("/api/sessions/", s.handleWebSocket)
	mux.HandleFunc("/download/", s.handleDownloadPage)
	mux.HandleFunc("/upload/", s.handleUploadPage)
	return mux
}
