package crypto

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

const (
	// TokenSize is the size of session tokens in bytes (256 bits)
	TokenSize = 32
)

// GenerateToken generates a cryptographically secure random token
// Returns a base64url-encoded string
func GenerateToken() (string, error) {
	b := make([]byte, TokenSize)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate random token: %w", err)
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// GenerateMasterKey generates a 256-bit master key for E2E encryption
func GenerateMasterKey() ([]byte, error) {
	key := make([]byte, 32) // 256 bits
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("failed to generate master key: %w", err)
	}
	return key, nil
}

// GenerateSalt generates a random salt for password-based key derivation
func GenerateSalt() ([]byte, error) {
	salt := make([]byte, 16) // 128 bits
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}
	return salt, nil
}
