package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/thecodefreak/xfer/internal/crypto"
	"github.com/thecodefreak/xfer/internal/protocol"
)

// handleCreateSession creates a new transfer session
func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request
	var req protocol.SessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate session type
	sessionType := protocol.SessionType(req.Type)
	if sessionType != protocol.SessionTypeSend && sessionType != protocol.SessionTypeReceive {
		http.Error(w, "Invalid session type", http.StatusBadRequest)
		return
	}

	// Validate encryption is enabled (always required)
	if !req.Encrypted {
		http.Error(w, "Encryption is mandatory", http.StatusBadRequest)
		return
	}

	// Generate session token
	token, err := crypto.GenerateToken()
	if err != nil {
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	// Create session
	session := s.sessionStore.Create(token, sessionType, req.Password)

	// Build URL based on session type
	var url string
	if sessionType == protocol.SessionTypeSend {
		url = fmt.Sprintf("%s/download/%s", s.config.BaseURL, token)
	} else {
		url = fmt.Sprintf("%s/upload/%s", s.config.BaseURL, token)
	}

	// Return response
	resp := protocol.SessionResponse{
		Token:     token,
		URL:       url,
		ExpiresAt: session.ExpiresAt,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// handleWebSocket handles WebSocket connections
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Extract token from path: /api/sessions/{token}/ws
	path := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	token := strings.TrimSuffix(path, "/ws")

	if token == "" {
		http.Error(w, "Token required", http.StatusBadRequest)
		return
	}

	// Get session
	session, exists := s.sessionStore.Get(token)
	if !exists {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	// Check if expired
	if session.IsExpired() {
		s.sessionStore.Delete(token)
		http.Error(w, "Session expired", http.StatusGone)
		return
	}

	// Handle WebSocket connection
	s.handleWebSocketConnection(w, r, session)
}

// handleDownloadPage serves the download page
func (s *Server) handleDownloadPage(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.URL.Path, "/download/")
	if token == "" {
		http.Error(w, "Token required", http.StatusBadRequest)
		return
	}

	// Verify session exists
	session, exists := s.sessionStore.Get(token)
	if !exists {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	// Check if expired
	if session.IsExpired() {
		s.sessionStore.Delete(token)
		http.Error(w, "Session expired", http.StatusGone)
		return
	}

	// Serve download page template
	data := struct{ Token string }{Token: token}
	err := downloadTemplate.Execute(w, data)
	if err != nil {
		http.Error(w, "Failed to render template", http.StatusInternalServerError)
	}
}

// handleUploadPage serves the upload page
func (s *Server) handleUploadPage(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.URL.Path, "/upload/")
	if token == "" {
		http.Error(w, "Token required", http.StatusBadRequest)
		return
	}

	// Verify session exists
	session, exists := s.sessionStore.Get(token)
	if !exists {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	// Check if expired
	if session.IsExpired() {
		s.sessionStore.Delete(token)
		http.Error(w, "Session expired", http.StatusGone)
		return
	}

	// Serve upload page template
	data := struct{ Token string }{Token: token}
	err := uploadTemplate.Execute(w, data)
	if err != nil {
		http.Error(w, "Failed to render template", http.StatusInternalServerError)
	}
}
