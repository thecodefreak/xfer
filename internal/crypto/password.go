package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"fmt"

	"golang.org/x/crypto/argon2"
)

const (
	// Argon2 parameters (balanced for security and performance)
	Argon2Time      = 3         // Number of iterations
	Argon2Memory    = 64 * 1024 // Memory in KB (64 MB)
	Argon2Threads   = 4         // Parallelism
	Argon2KeyLength = 32        // Output key size (256 bits)
)

// DerivePasswordKey derives a key from a password using Argon2id
func DerivePasswordKey(password string, salt []byte) []byte {
	return argon2.IDKey(
		[]byte(password),
		salt,
		Argon2Time,
		Argon2Memory,
		Argon2Threads,
		Argon2KeyLength,
	)
}

// EncryptMasterKey encrypts the master key with a password-derived key
// Returns the encrypted master key (ciphertext includes auth tag)
func EncryptMasterKey(masterKey []byte, password string, salt []byte) ([]byte, error) {
	if len(masterKey) != KeySize {
		return nil, fmt.Errorf("master key must be %d bytes", KeySize)
	}

	// Derive key from password
	passwordKey := DerivePasswordKey(password, salt)

	// Create AES cipher
	block, err := aes.NewCipher(passwordKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate nonce (GCM standard nonce size)
	nonce := make([]byte, aesGCM.NonceSize())
	// Use deterministic nonce derived from salt for master key encryption
	// This is safe because we only encrypt one message (the master key) per salt
	copy(nonce, salt[:aesGCM.NonceSize()])

	// Encrypt master key
	ciphertext := aesGCM.Seal(nil, nonce, masterKey, nil)
	return ciphertext, nil
}

// DecryptMasterKey decrypts the master key using a password
func DecryptMasterKey(encryptedKey []byte, password string, salt []byte) ([]byte, error) {
	// Derive key from password
	passwordKey := DerivePasswordKey(password, salt)

	// Create AES cipher
	block, err := aes.NewCipher(passwordKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Use same deterministic nonce
	nonce := make([]byte, aesGCM.NonceSize())
	copy(nonce, salt[:aesGCM.NonceSize()])

	// Decrypt master key
	plaintext, err := aesGCM.Open(nil, nonce, encryptedKey, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid password or corrupted data: %w", err)
	}

	if len(plaintext) != KeySize {
		return nil, fmt.Errorf("decrypted key has invalid size")
	}

	return plaintext, nil
}
