package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/google/uuid"
)

// RequestLogger logs minimal request information (privacy-focused)
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Generate request ID
		requestID := uuid.New().String()

		// Add request ID to context
		ctx := context.WithValue(r.Context(), "requestID", requestID)
		r = r.WithContext(ctx)

		// Wrap response writer to capture status
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		// Process request
		next.ServeHTTP(wrapped, r)

		// Log minimal info (Option A: privacy-focused)
		log.Printf("request_id=%s ip=%s method=%s path=%s status=%d duration=%v",
			requestID,
			extractIP(r),
			r.Method,
			r.URL.Path,
			wrapped.statusCode,
			time.Since(start),
		)
	})
}

// RecoverPanic prevents the server from crashing on panics
func RecoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("panic recovered: %v\n%s", err, debug.Stack())
				http.Error(w, "Internal server error", http.StatusInternalServerError)
			}
		}()

		next.ServeHTTP(w, r)
	})
}

// RequestSizeLimit enforces maximum request body size
func RequestSizeLimit(maxSize int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check Content-Length header
			if r.ContentLength > maxSize {
				http.Error(w, fmt.Sprintf("Request body too large. Maximum size is %d bytes", maxSize), http.StatusRequestEntityTooLarge)
				return
			}

			// Limit the reader
			r.Body = http.MaxBytesReader(w, r.Body, maxSize)

			next.ServeHTTP(w, r)
		})
	}
}

// RequestID adds a unique request ID to the context
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Context().Value("requestID")
		if requestID == nil {
			requestID = uuid.New().String()
			ctx := context.WithValue(r.Context(), "requestID", requestID)
			r = r.WithContext(ctx)
		}

		// Add to response header for client correlation
		w.Header().Set("X-Request-ID", requestID.(string))

		next.ServeHTTP(w, r)
	})
}

// ApplyMiddleware applies a chain of middleware to routes based on path patterns
func ApplyMiddleware(mux *http.ServeMux, config *Config) http.Handler {
	// Create rate limiters with different limits per endpoint type
	sessionLimiter := NewRateLimiter(config.RateLimitSessions, config.RateLimitSessions*2)
	wsLimiter := NewRateLimiter(config.RateLimitWebSocket, config.RateLimitWebSocket*2)
	pageLimiter := NewRateLimiter(config.RateLimitPages, config.RateLimitPages*2)

	// Create the main handler with path-specific rate limiting
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Apply appropriate rate limiter based on path
		var rateLimited http.Handler
		rateLimited = http.Handler(mux)

		if config.EnableRateLimit {
			switch {
			case path == "/api/sessions":
				rateLimited = sessionLimiter.Middleware(mux)
			case strings.HasPrefix(path, "/api/sessions/") && strings.HasSuffix(path, "/ws"):
				rateLimited = wsLimiter.Middleware(mux)
			case strings.HasPrefix(path, "/download/") || strings.HasPrefix(path, "/upload/"):
				rateLimited = pageLimiter.Middleware(mux)
			}
		}

		rateLimited.ServeHTTP(w, r)
	})

	// Apply global middleware chain (in reverse order of application)
	var finalHandler http.Handler = handler
	finalHandler = RequestID(finalHandler)
	finalHandler = RequestSizeLimit(config.MaxSize)(finalHandler)
	finalHandler = RequestLogger(finalHandler)
	finalHandler = RecoverPanic(finalHandler)

	return finalHandler
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.written {
		rw.statusCode = code
		rw.ResponseWriter.WriteHeader(code)
		rw.written = true
	}
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}
