package utils

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestValidatePathWithinBase(t *testing.T) {
	// Use a temporary base path for testing
	basePath := "/mnt/storage/videos"

	tests := []struct {
		name        string
		basePath    string
		targetPath  string
		shouldError bool
		errContains string
	}{
		{
			name:        "valid relative path",
			basePath:    basePath,
			targetPath:  "subdir/video.mp4",
			shouldError: false,
		},
		{
			name:        "valid absolute path within base",
			basePath:    basePath,
			targetPath:  "/mnt/storage/videos/subdir/video.mp4",
			shouldError: false,
		},
		{
			name:        "path traversal with ../",
			basePath:    basePath,
			targetPath:  "../../../etc/passwd",
			shouldError: true,
			errContains: "suspicious traversal pattern",
		},
		{
			name:        "path traversal with absolute path",
			basePath:    basePath,
			targetPath:  "/etc/passwd",
			shouldError: true,
			errContains: "path traversal detected",
		},
		{
			name:        "path with .. in the middle",
			basePath:    basePath,
			targetPath:  "subdir/../../../etc/passwd",
			shouldError: true,
			errContains: "suspicious traversal pattern",
		},
		{
			name:        "valid path with similar name",
			basePath:    basePath,
			targetPath:  "video..mp4",
			shouldError: false,
		},
		{
			name:        "path with null byte",
			basePath:    basePath,
			targetPath:  "video\x00.mp4",
			shouldError: true,
			errContains: "null byte",
		},
		{
			name:        "empty target path",
			basePath:    basePath,
			targetPath:  "",
			shouldError: false, // Empty path resolves to base path
		},
		{
			name:        "path exactly at base",
			basePath:    basePath,
			targetPath:  basePath,
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ValidatePathWithinBase(tt.basePath, tt.targetPath)

			if tt.shouldError {
				if err == nil {
					t.Errorf("expected error but got none, result: %s", result)
				} else if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing %q, got: %v", tt.errContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				// Verify result is absolute and clean
				if !filepath.IsAbs(result) {
					t.Errorf("expected absolute path, got: %s", result)
				}
			}
		})
	}
}

func TestValidatePathInAllowedDirs(t *testing.T) {
	sourceDir := "/mnt/storage/videos"
	outputDir := "/mnt/storage/converted"

	tests := []struct {
		name        string
		allowedDirs []string
		targetPath  string
		shouldError bool
		errContains string
	}{
		{
			name:        "valid path in source dir",
			allowedDirs: []string{sourceDir, outputDir},
			targetPath:  "/mnt/storage/videos/video.mp4",
			shouldError: false,
		},
		{
			name:        "valid path in output dir",
			allowedDirs: []string{sourceDir, outputDir},
			targetPath:  "/mnt/storage/converted/video.mp4",
			shouldError: false,
		},
		{
			name:        "invalid path outside allowed dirs",
			allowedDirs: []string{sourceDir, outputDir},
			targetPath:  "/etc/passwd",
			shouldError: true,
			errContains: "not in any allowed directory",
		},
		{
			name:        "path traversal attempt",
			allowedDirs: []string{sourceDir, outputDir},
			targetPath:  "/mnt/storage/videos/../../../etc/passwd",
			shouldError: true,
			errContains: "suspicious traversal pattern",
		},
		{
			name:        "empty allowed dirs",
			allowedDirs: []string{},
			targetPath:  "/mnt/storage/videos/video.mp4",
			shouldError: true,
			errContains: "no allowed directories",
		},
		{
			name:        "relative path in source dir",
			allowedDirs: []string{sourceDir, outputDir},
			targetPath:  "video.mp4",
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ValidatePathInAllowedDirs(tt.allowedDirs, tt.targetPath)

			if tt.shouldError {
				if err == nil {
					t.Errorf("expected error but got none, result: %s", result)
				} else if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing %q, got: %v", tt.errContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				// Verify result is absolute
				if !filepath.IsAbs(result) {
					t.Errorf("expected absolute path, got: %s", result)
				}
			}
		})
	}
}

func TestValidatePathWithinBase_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		basePath    string
		targetPath  string
		shouldError bool
		description string
	}{
		{
			name:        "double dot in filename",
			basePath:    "/tmp/test",
			targetPath:  "file..txt",
			shouldError: false,
			description: "Double dots in filename should be allowed",
		},
		{
			name:        "hidden file",
			basePath:    "/tmp/test",
			targetPath:  ".hidden",
			shouldError: false,
			description: "Hidden files should be allowed",
		},
		{
			name:        "hidden directory",
			basePath:    "/tmp/test",
			targetPath:  ".hidden/file.txt",
			shouldError: false,
			description: "Hidden directories should be allowed",
		},
		{
			name:        "multiple slashes",
			basePath:    "/tmp/test",
			targetPath:  "dir//file.txt",
			shouldError: false,
			description: "Multiple slashes should be normalized",
		},
		{
			name:        "trailing slash",
			basePath:    "/tmp/test",
			targetPath:  "dir/",
			shouldError: false,
			description: "Trailing slash should be handled",
		},
		{
			name:        "symbolic link name (not actual symlink)",
			basePath:    "/tmp/test",
			targetPath:  "symlink.txt",
			shouldError: false,
			description: "File named symlink should be allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ValidatePathWithinBase(tt.basePath, tt.targetPath)

			if tt.shouldError {
				if err == nil {
					t.Errorf("%s: expected error but got none, result: %s", tt.description, result)
				}
			} else {
				if err != nil {
					t.Errorf("%s: unexpected error: %v", tt.description, err)
				}
			}
		})
	}
}
