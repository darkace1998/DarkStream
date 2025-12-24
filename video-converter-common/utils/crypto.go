package utils

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// GenerateRandomBytes generates cryptographically secure random bytes.
func GenerateRandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return b, nil
}

// GenerateRandomString generates a cryptographically secure random string.
// The string is base64 URL-encoded and safe for use in URLs.
func GenerateRandomString(n int) (string, error) {
	b, err := GenerateRandomBytes(n)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// GenerateRandomHex generates a cryptographically secure random hex string.
func GenerateRandomHex(n int) (string, error) {
	b, err := GenerateRandomBytes(n)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// HashSHA256 computes the SHA-256 hash of the input data.
func HashSHA256(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// HashSHA256String computes the SHA-256 hash of a string.
func HashSHA256String(s string) string {
	return HashSHA256([]byte(s))
}

// HashFileSHA256 computes the SHA-256 hash of a file's contents.
func HashFileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// ConstantTimeCompare compares two strings in constant time.
// This is useful for comparing secrets or hashes to prevent timing attacks.
func ConstantTimeCompare(a, b string) bool {
	if len(a) != len(b) {
		return false
	}

	var result byte
	for i := range len(a) {
		result |= a[i] ^ b[i]
	}
	return result == 0
}

// SecureWipe overwrites a byte slice with zeros.
// This is useful for clearing sensitive data from memory.
func SecureWipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// GenerateID generates a unique ID suitable for use as a job or request ID.
// It returns a 16-character hex string (64 bits of entropy).
func GenerateID() (string, error) {
	return GenerateRandomHex(8)
}

// GenerateSecureToken generates a secure token suitable for authentication.
// It returns a 32-character hex string (128 bits of entropy).
func GenerateSecureToken() (string, error) {
	return GenerateRandomHex(16)
}
