// Package utils provides common utility functions for the video converter system.
package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
)

// CalculateFileSHA256 calculates the SHA256 checksum of a file
func CalculateFileSHA256(filePath string) (string, error) {
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

	hash := sha256.New()
	_, err = io.Copy(hash, file)
	if err != nil {
		return "", fmt.Errorf("failed to calculate checksum: %w", err)
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// VerifyFileSHA256 verifies that a file's SHA256 checksum matches the expected value
func VerifyFileSHA256(filePath, expectedChecksum string) (bool, error) {
	actualChecksum, err := CalculateFileSHA256(filePath)
	if err != nil {
		return false, err
	}

	return actualChecksum == expectedChecksum, nil
}
