package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileExists checks if a file exists and is not a directory
func FileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// DirExists checks if a directory exists
func DirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// EnsureDir creates a directory if it doesn't exist
func EnsureDir(path string) error {
	if !DirExists(path) {
		if err := os.MkdirAll(path, 0o750); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}
	}
	return nil
}

// GetFileSize returns the size of a file in bytes
func GetFileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("failed to get file info: %w", err)
	}
	return info.Size(), nil
}

// GetRelativePath returns the relative path between two paths
func GetRelativePath(basePath, targetPath string) (string, error) {
	relPath, err := filepath.Rel(basePath, targetPath)
	if err != nil {
		return "", fmt.Errorf("failed to get relative path: %w", err)
	}
	return relPath, nil
}

// ValidatePathWithinBase validates that targetPath is within basePath and prevents path traversal.
// It returns an error if:
// - targetPath attempts to escape basePath (e.g., using ../)
// - targetPath contains suspicious patterns
// - Paths cannot be resolved to absolute paths
//
// Parameters:
//   - basePath: The allowed base directory (e.g., /mnt/storage/videos)
//   - targetPath: The path to validate (can be relative or absolute)
//
// Returns:
//   - The cleaned absolute path if valid
//   - An error if the path is invalid or attempts traversal
func ValidatePathWithinBase(basePath, targetPath string) (string, error) {
	// Convert both paths to absolute paths
	absBase, err := filepath.Abs(basePath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute base path: %w", err)
	}

	// Clean the base path to remove any .., ., or duplicate separators
	absBase = filepath.Clean(absBase)

	// If targetPath is relative, join it with basePath first
	var absTarget string
	if filepath.IsAbs(targetPath) {
		absTarget = targetPath
	} else {
		absTarget = filepath.Join(absBase, targetPath)
	}

	// Clean the target path to resolve .., ., and duplicate separators
	absTarget = filepath.Clean(absTarget)

	// Check for suspicious patterns that might indicate traversal attempts
	// Look for ".." as a path component, not just in the string
	// Split by separator and check each component
	pathComponents := strings.Split(filepath.ToSlash(targetPath), "/")
	for _, component := range pathComponents {
		if component == ".." {
			return "", fmt.Errorf("path contains suspicious traversal pattern: %s", targetPath)
		}
	}

	// Ensure the resolved path is within the base directory
	// Use filepath.Rel to check if target is within base
	relPath, err := filepath.Rel(absBase, absTarget)
	if err != nil {
		return "", fmt.Errorf("failed to compute relative path: %w", err)
	}

	// If the relative path starts with "..", it means the target is outside base
	if strings.HasPrefix(relPath, "..") {
		return "", fmt.Errorf("path traversal detected: %s attempts to escape %s", targetPath, basePath)
	}

	// Additional check: ensure the path doesn't contain null bytes (security)
	if strings.Contains(absTarget, "\x00") {
		return "", fmt.Errorf("path contains null byte")
	}

	return absTarget, nil
}

// ValidatePathInAllowedDirs validates that a path is within one of the allowed base directories.
// This is useful when multiple allowed directories exist (e.g., source and output directories).
//
// Parameters:
//   - allowedDirs: List of allowed base directories
//   - targetPath: The path to validate
//
// Returns:
//   - The cleaned absolute path if valid
//   - An error if the path is not within any allowed directory
func ValidatePathInAllowedDirs(allowedDirs []string, targetPath string) (string, error) {
	if len(allowedDirs) == 0 {
		return "", fmt.Errorf("no allowed directories configured")
	}

	// Try to validate against each allowed directory
	var lastErr error
	for _, baseDir := range allowedDirs {
		validPath, err := ValidatePathWithinBase(baseDir, targetPath)
		if err == nil {
			return validPath, nil
		}
		lastErr = err
	}

	// Path is not within any allowed directory
	return "", fmt.Errorf("path not in any allowed directory: %w", lastErr)
}
