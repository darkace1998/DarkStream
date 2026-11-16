// Package utils provides common utility functions for the video converter system.
package utils

import (
	"crypto/sha1" // #nosec G505 - SHA-1 is opt-in for fast integrity checks and dedup, not a security primitive
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"log/slog"
	"os"
	"strings"
)

// Supported hash algorithms for file checksums.
const (
	HashAlgoSHA256 = "sha256"
	HashAlgoSHA1   = "sha1"
)

// hashReadBufferSize is the chunk size used when streaming a file into a hash.
// 1 MiB keeps sequential reads efficient without large allocations per file.
const hashReadBufferSize = 1 << 20

// NewHash returns a hash.Hash for the given algorithm. Empty and "sha256"
// return SHA-256 (the default); "sha1" returns SHA-1, which is faster and less
// CPU-intensive while still fine for integrity/duplicate detection.
func NewHash(algo string) (hash.Hash, error) {
	switch strings.ToLower(algo) {
	case "", HashAlgoSHA256:
		return sha256.New(), nil
	case HashAlgoSHA1:
		// #nosec G401 - SHA-1 is opt-in for fast integrity checks and dedup, not a security primitive
		return sha1.New(), nil
	default:
		return nil, fmt.Errorf("unsupported hash algorithm %q (want %q or %q)", algo, HashAlgoSHA256, HashAlgoSHA1)
	}
}

// CalculateFileHash calculates the checksum of a file using the given algorithm
// ("sha256" default, or "sha1").
func CalculateFileHash(filePath, algo string) (string, error) {
	// #nosec G304 - filePath is derived from job metadata, not untrusted network input
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer func() {
		cerr := file.Close()
		if cerr != nil {
			slog.Warn("Failed to close file during checksum calculation", "error", cerr)
		}
	}()

	h, err := NewHash(algo)
	if err != nil {
		return "", err
	}
	buf := make([]byte, hashReadBufferSize)
	if _, err := io.CopyBuffer(h, file, buf); err != nil {
		return "", fmt.Errorf("failed to calculate checksum: %w", err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// CalculateFileSHA256 calculates the SHA256 checksum of a file
func CalculateFileSHA256(filePath string) (string, error) {
	return CalculateFileHash(filePath, HashAlgoSHA256)
}

// VerifyFileHash verifies that a file's checksum matches the expected value,
// using the given algorithm ("sha256" default, or "sha1").
func VerifyFileHash(filePath, algo, expectedChecksum string) (bool, error) {
	actualChecksum, err := CalculateFileHash(filePath, algo)
	if err != nil {
		return false, err
	}

	// Compare case-insensitively: hex.EncodeToString emits lowercase, but an
	// expected checksum supplied by an external tool may be upper- or mixed-case.
	return strings.EqualFold(actualChecksum, expectedChecksum), nil
}

// VerifyFileSHA256 verifies that a file's SHA256 checksum matches the expected value
func VerifyFileSHA256(filePath, expectedChecksum string) (bool, error) {
	return VerifyFileHash(filePath, HashAlgoSHA256, expectedChecksum)
}

// AlgoForChecksum infers the hash algorithm from a hex checksum's length
// (SHA-256 = 64 chars, SHA-1 = 40 chars). Anything else defaults to SHA-256.
// This lets workers verify downloads without the master persisting the algo.
func AlgoForChecksum(hexChecksum string) string {
	switch len(strings.TrimSpace(hexChecksum)) {
	case 40:
		return HashAlgoSHA1
	default:
		return HashAlgoSHA256
	}
}

// VerifyFileHashAuto verifies a file against an expected hex checksum,
// inferring the algorithm from the checksum length (see AlgoForChecksum).
func VerifyFileHashAuto(filePath, expectedChecksum string) (bool, error) {
	return VerifyFileHash(filePath, AlgoForChecksum(expectedChecksum), expectedChecksum)
}
