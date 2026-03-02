package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"fmt"
	"io"

	"crypto/sha256"
	"golang.org/x/crypto/hkdf"
)

const (
	// ChunkSize is the size of each encrypted chunk (64KB)
	ChunkSize = 64 * 1024

	// KeySize is the size of AES-256 keys (32 bytes)
	KeySize = 32

	// NonceSize is the size of AES-GCM nonces (12 bytes)
	NonceSize = 12

	// NoncePrefixSize is the size of the nonce prefix from HKDF (4 bytes)
	NoncePrefixSize = 4
)

// DerivedKeys contains keys derived from the master key
type DerivedKeys struct {
	FileKey     []byte // AES-256 key for file encryption
	MetadataKey []byte // AES-256 key for metadata encryption
	FileNonce   []byte // 4-byte prefix for file chunk nonces
	MetaNonce   []byte // 4-byte prefix for metadata nonce
}

// DeriveKeys derives encryption keys from a master key using HKDF
func DeriveKeys(masterKey []byte) (*DerivedKeys, error) {
	if len(masterKey) != KeySize {
		return nil, fmt.Errorf("master key must be %d bytes", KeySize)
	}

	keys := &DerivedKeys{}

	// Derive file encryption key
	fileKeyReader := hkdf.New(sha256.New, masterKey, nil, []byte("xfer-file"))
	keys.FileKey = make([]byte, KeySize)
	if _, err := io.ReadFull(fileKeyReader, keys.FileKey); err != nil {
		return nil, fmt.Errorf("failed to derive file key: %w", err)
	}

	// Derive metadata encryption key
	metaKeyReader := hkdf.New(sha256.New, masterKey, nil, []byte("xfer-metadata"))
	keys.MetadataKey = make([]byte, KeySize)
	if _, err := io.ReadFull(metaKeyReader, keys.MetadataKey); err != nil {
		return nil, fmt.Errorf("failed to derive metadata key: %w", err)
	}

	// Derive file nonce prefix
	fileNonceReader := hkdf.New(sha256.New, masterKey, nil, []byte("xfer-file-nonce"))
	keys.FileNonce = make([]byte, NoncePrefixSize)
	if _, err := io.ReadFull(fileNonceReader, keys.FileNonce); err != nil {
		return nil, fmt.Errorf("failed to derive file nonce prefix: %w", err)
	}

	// Derive metadata nonce prefix
	metaNonceReader := hkdf.New(sha256.New, masterKey, nil, []byte("xfer-meta-nonce"))
	keys.MetaNonce = make([]byte, NoncePrefixSize)
	if _, err := io.ReadFull(metaNonceReader, keys.MetaNonce); err != nil {
		return nil, fmt.Errorf("failed to derive metadata nonce prefix: %w", err)
	}

	return keys, nil
}

// makeNonce creates a 12-byte nonce from a 4-byte prefix and 8-byte counter
func makeNonce(prefix []byte, counter uint64) []byte {
	nonce := make([]byte, NonceSize)
	copy(nonce[:NoncePrefixSize], prefix)
	binary.BigEndian.PutUint64(nonce[NoncePrefixSize:], counter)
	return nonce
}

// EncryptChunk encrypts a single chunk of data
func EncryptChunk(data []byte, key []byte, noncePrefix []byte, chunkID uint64) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := makeNonce(noncePrefix, chunkID)
	ciphertext := aesGCM.Seal(nil, nonce, data, nil)
	return ciphertext, nil
}

// DecryptChunk decrypts a single chunk of data
func DecryptChunk(ciphertext []byte, key []byte, noncePrefix []byte, chunkID uint64) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := makeNonce(noncePrefix, chunkID)
	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed: %w", err)
	}

	return plaintext, nil
}

// EncryptMetadata encrypts file metadata (filename, size, checksum)
func EncryptMetadata(data []byte, key []byte, noncePrefix []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := makeNonce(noncePrefix, 0) // Metadata always uses counter 0
	ciphertext := aesGCM.Seal(nil, nonce, data, nil)
	return ciphertext, nil
}

// DecryptMetadata decrypts file metadata
func DecryptMetadata(ciphertext []byte, key []byte, noncePrefix []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := makeNonce(noncePrefix, 0) // Metadata always uses counter 0
	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("metadata decryption failed: %w", err)
	}

	return plaintext, nil
}
