package crypto

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// CalculateChecksum calculates SHA-256 checksum of a file
func CalculateChecksum(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("failed to calculate checksum: %w", err)
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// CalculateChecksumBytes calculates SHA-256 checksum of byte data
func CalculateChecksumBytes(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// VerifyChecksum verifies a file's checksum matches the expected value
func VerifyChecksum(filePath string, expected string) (bool, error) {
	actual, err := CalculateChecksum(filePath)
	if err != nil {
		return false, err
	}
	return actual == expected, nil
}
