package server

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimiter manages per-IP rate limiting
type RateLimiter struct {
	limiters sync.Map // map[string]*rateLimiterEntry
	rate     int      // requests per minute
	burst    int      // burst capacity
}

type rateLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(requestsPerMinute, burst int) *RateLimiter {
	rl := &RateLimiter{
		rate:  requestsPerMinute,
		burst: burst,
	}

	// Start cleanup routine
	go rl.cleanupRoutine()

	return rl
}

// GetLimiter returns the rate limiter for a specific IP
func (rl *RateLimiter) GetLimiter(ip string) *rate.Limiter {
	// Try to get existing limiter
	if entry, exists := rl.limiters.Load(ip); exists {
		if e, ok := entry.(*rateLimiterEntry); ok {
			e.lastSeen = time.Now()
			return e.limiter
		}
	}

	// Create new limiter for this IP
	limiter := rate.NewLimiter(rate.Limit(float64(rl.rate)/60.0), rl.burst)
	entry := &rateLimiterEntry{
		limiter:  limiter,
		lastSeen: time.Now(),
	}
	rl.limiters.Store(ip, entry)

	return limiter
}

// Middleware returns an HTTP middleware that enforces rate limits
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := extractIP(r)
		limiter := rl.GetLimiter(ip)

		if !limiter.Allow() {
			// Calculate retry after (roughly)
			retryAfter := int(60 / rl.rate)

			w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", rl.rate))
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", time.Now().Add(time.Duration(retryAfter)*time.Second).Unix()))

			http.Error(w, "Rate limit exceeded. Please try again later.", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// cleanupRoutine removes old entries every 30 minutes
func (rl *RateLimiter) cleanupRoutine() {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		rl.limiters.Range(func(key, value interface{}) bool {
			if entry, ok := value.(*rateLimiterEntry); ok {
				// Remove entries not seen in last hour
				if now.Sub(entry.lastSeen) > time.Hour {
					rl.limiters.Delete(key)
				}
			}
			return true
		})
	}
}

// extractIP extracts the real IP address from the request
func extractIP(r *http.Request) string {
	// Check X-Real-IP header first
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return cleanIP(ip)
	}

	// Check X-Forwarded-For header
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		// Take the first IP in the chain
		ips := strings.Split(forwarded, ",")
		if len(ips) > 0 {
			return cleanIP(ips[0])
		}
	}

	// Fallback to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// cleanIP cleans and validates an IP address
func cleanIP(ip string) string {
	ip = strings.TrimSpace(ip)

	// Remove port if present
	if host, _, err := net.SplitHostPort(ip); err == nil {
		return host
	}

	return ip
}
