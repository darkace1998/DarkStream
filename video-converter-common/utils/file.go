// Package utils provides utility functions for the video converter application.
package utils

import (
	"fmt"
	"io"
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

// CopyFile copies a file from src to dst.
// If dst already exists, it will be overwritten.
func CopyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer func() { _ = srcFile.Close() }()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat source file: %w", err)
	}

	if err := EnsureDir(filepath.Dir(dst)); err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer func() { _ = dstFile.Close() }()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("failed to copy file contents: %w", err)
	}

	if err := dstFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync destination file: %w", err)
	}
	return nil
}

// MoveFile moves a file from src to dst.
// It first attempts a rename, and if that fails (e.g., cross-device),
// it falls back to copy and delete.
func MoveFile(src, dst string) error {
	if err := EnsureDir(filepath.Dir(dst)); err != nil {
		return err
	}

	if err := os.Rename(src, dst); err == nil {
		return nil
	}

	if err := CopyFile(src, dst); err != nil {
		return fmt.Errorf("failed to copy file during move: %w", err)
	}

	if err := os.Remove(src); err != nil {
		return fmt.Errorf("failed to remove source file after copy: %w", err)
	}

	return nil
}

// SafeRemove removes a file only if it exists.
// It returns nil if the file doesn't exist.
func SafeRemove(path string) error {
	if !FileExists(path) && !DirExists(path) {
		return nil
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("failed to remove %s: %w", path, err)
	}
	return nil
}

// SafeRemoveAll removes a path and any children it contains.
// It returns nil if the path doesn't exist.
func SafeRemoveAll(path string) error {
	if !FileExists(path) && !DirExists(path) {
		return nil
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("failed to remove %s: %w", path, err)
	}
	return nil
}

// GetAbsolutePath returns the absolute path for a given path.
func GetAbsolutePath(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}
	return absPath, nil
}

// CleanPath cleans and normalizes a path.
func CleanPath(path string) string {
	return filepath.Clean(path)
}

// GetExtension returns the file extension including the dot.
func GetExtension(path string) string {
	return filepath.Ext(path)
}

// GetBaseName returns the file name without the directory.
func GetBaseName(path string) string {
	return filepath.Base(path)
}

// GetDir returns the directory containing the file.
func GetDir(path string) string {
	return filepath.Dir(path)
}

// GetBaseNameWithoutExt returns the file name without extension.
func GetBaseNameWithoutExt(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext)
}

// JoinPath joins path elements with the file separator.
func JoinPath(elem ...string) string {
	return filepath.Join(elem...)
}

// ReadFile reads the entire file contents.
func ReadFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	return data, nil
}

// WriteFile writes data to a file, creating it if necessary.
// If the file exists, it will be overwritten.
func WriteFile(path string, data []byte) error {
	if err := EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	return nil
}

// WriteFileWithPerm writes data to a file with specified permissions.
func WriteFileWithPerm(path string, data []byte, perm os.FileMode) error {
	if err := EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, perm); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	return nil
}

// IsHiddenFile checks if a file or directory is hidden (starts with '.')
func IsHiddenFile(path string) bool {
	base := filepath.Base(path)
	return len(base) > 0 && base[0] == '.'
}

// ListFiles lists all files in a directory (non-recursive).
func ListFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var files []string
	for _, entry := range entries {
		if !entry.IsDir() {
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}
	return files, nil
}

// ListDirs lists all directories in a directory (non-recursive).
func ListDirs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var dirs []string
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, filepath.Join(dir, entry.Name()))
		}
	}
	return dirs, nil
}

// WalkDir walks a directory tree, calling walkFn for each file or directory.
func WalkDir(root string, walkFn filepath.WalkFunc) error {
	if err := filepath.Walk(root, walkFn); err != nil {
		return fmt.Errorf("failed to walk directory: %w", err)
	}
	return nil
}

// GetFileModTime returns the modification time of a file.
func GetFileModTime(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("failed to get file info: %w", err)
	}
	return info.ModTime().Unix(), nil
}

// TempDir creates a new temporary directory.
func TempDir(dir, prefix string) (string, error) {
	tmpDir, err := os.MkdirTemp(dir, prefix)
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}
	return tmpDir, nil
}

// TempFile creates a new temporary file.
func TempFile(dir, pattern string) (*os.File, error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	return f, nil
}
