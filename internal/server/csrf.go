package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"sync"
)

// CSRFManager manages CSRF tokens for sessions
type CSRFManager struct {
	tokens sync.Map // map[sessionToken]csrfToken
}

// NewCSRFManager creates a new CSRF manager
func NewCSRFManager() *CSRFManager {
	return &CSRFManager{}
}

// GenerateToken generates a new CSRF token for a session
func (c *CSRFManager) GenerateToken(sessionToken string) (string, error) {
	// Generate 32-byte random token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", err
	}

	token := base64.URLEncoding.EncodeToString(tokenBytes)

	// Store token associated with session (Option B: per-session tokens)
	c.tokens.Store(sessionToken, token)

	return token, nil
}

// ValidateToken validates a CSRF token for a session
func (c *CSRFManager) ValidateToken(sessionToken, csrfToken string) bool {
	stored, exists := c.tokens.Load(sessionToken)
	if !exists {
		return false
	}

	storedToken, ok := stored.(string)
	if !ok {
		return false
	}

	// Use constant-time comparison to prevent timing attacks
	return subtle.ConstantTimeCompare([]byte(storedToken), []byte(csrfToken)) == 1
}

// DeleteToken removes a CSRF token when session expires
func (c *CSRFManager) DeleteToken(sessionToken string) {
	c.tokens.Delete(sessionToken)
}

// SetCSRFCookie sets the CSRF token cookie
func SetCSRFCookie(w http.ResponseWriter, r *http.Request, token string) {
	isHTTPS := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"

	cookie := &http.Cookie{
		Name:     "xfer_csrf",
		Value:    token,
		Path:     "/",
		HttpOnly: false, // JavaScript needs access for double-submit
		Secure:   isHTTPS,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   300, // 5 minutes to match session TTL
	}

	http.SetCookie(w, cookie)
}

// GetCSRFToken gets CSRF token from request (cookie or header)
func GetCSRFToken(r *http.Request) string {
	// Check header first (for AJAX requests)
	if token := r.Header.Get("X-CSRF-Token"); token != "" {
		return token
	}

	// Check form value
	if token := r.FormValue("csrf_token"); token != "" {
		return token
	}

	// Check cookie
	if cookie, err := r.Cookie("xfer_csrf"); err == nil {
		return cookie.Value
	}

	return ""
}

// CSRFMiddleware validates CSRF tokens on state-changing requests
func (s *Server) CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only validate on state-changing methods
		if r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodDelete {
			next.ServeHTTP(w, r)
			return
		}

		// Skip CSRF for certain endpoints (like health check)
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}

		// For session creation, we don't have a token yet
		if r.URL.Path == "/api/sessions" && r.Method == http.MethodPost {
			// Generate new CSRF token after session creation
			next.ServeHTTP(w, r)
			return
		}

		// Get session token from the request path or header
		sessionToken := extractSessionToken(r)
		if sessionToken == "" {
			http.Error(w, "Session token required", http.StatusBadRequest)
			return
		}

		// Get CSRF token from request
		csrfToken := GetCSRFToken(r)
		if csrfToken == "" {
			http.Error(w, "CSRF token required", http.StatusForbidden)
			return
		}

		// Validate CSRF token
		if !s.csrfManager.ValidateToken(sessionToken, csrfToken) {
			http.Error(w, "Invalid CSRF token", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// extractSessionToken extracts session token from request
func extractSessionToken(r *http.Request) string {
	// Try to extract from URL path
	// e.g., /api/sessions/{token}/something
	if len(r.URL.Path) > len("/api/sessions/") {
		path := r.URL.Path[len("/api/sessions/"):]
		if idx := strings.IndexByte(path, '/'); idx > 0 {
			return path[:idx]
		}
		return path
	}

	// Try header
	if token := r.Header.Get("X-Session-Token"); token != "" {
		return token
	}

	return ""
}
