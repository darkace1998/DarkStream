package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileExists(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. File exists
	filePath := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(filePath, []byte("test"), 0644)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	if !FileExists(filePath) {
		t.Errorf("FileExists() returned false for an existing file")
	}

	// 2. File doesn't exist
	nonExistentPath := filepath.Join(tmpDir, "non_existent.txt")
	if FileExists(nonExistentPath) {
		t.Errorf("FileExists() returned true for a non-existent file")
	}

	// 3. Path is a directory
	if FileExists(tmpDir) {
		t.Errorf("FileExists() returned true for a directory")
	}
}

func TestDirExists(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Directory exists
	if !DirExists(tmpDir) {
		t.Errorf("DirExists() returned false for an existing directory")
	}

	// 2. Directory doesn't exist
	nonExistentPath := filepath.Join(tmpDir, "non_existent_dir")
	if DirExists(nonExistentPath) {
		t.Errorf("DirExists() returned true for a non-existent directory")
	}

	// 3. Path is a file
	filePath := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(filePath, []byte("test"), 0644)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}
	if DirExists(filePath) {
		t.Errorf("DirExists() returned true for a file")
	}
}

func TestEnsureDir(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Create a new directory
	newDir := filepath.Join(tmpDir, "new_dir")
	err := EnsureDir(newDir)
	if err != nil {
		t.Errorf("EnsureDir() failed to create new directory: %v", err)
	}
	if !DirExists(newDir) {
		t.Errorf("EnsureDir() did not actually create the directory")
	}

	// 2. Directory already exists
	err = EnsureDir(newDir)
	if err != nil {
		t.Errorf("EnsureDir() failed when directory already existed: %v", err)
	}

	// 3. Path is a file (should fail)
	filePath := filepath.Join(tmpDir, "test.txt")
	err = os.WriteFile(filePath, []byte("test"), 0644)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}
	err = EnsureDir(filePath)
	if err == nil {
		t.Errorf("EnsureDir() did not fail when path is an existing file")
	}
}

func TestGetFileSize(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. File with content
	filePath := filepath.Join(tmpDir, "test.txt")
	content := []byte("test content")
	err := os.WriteFile(filePath, content, 0644)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	size, err := GetFileSize(filePath)
	if err != nil {
		t.Errorf("GetFileSize() failed: %v", err)
	}
	if size != int64(len(content)) {
		t.Errorf("GetFileSize() returned incorrect size: expected %d, got %d", len(content), size)
	}

	// 2. Empty file
	emptyFilePath := filepath.Join(tmpDir, "empty.txt")
	err = os.WriteFile(emptyFilePath, []byte(""), 0644)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	size, err = GetFileSize(emptyFilePath)
	if err != nil {
		t.Errorf("GetFileSize() failed for empty file: %v", err)
	}
	if size != 0 {
		t.Errorf("GetFileSize() returned non-zero size for empty file: got %d", size)
	}

	// 3. Non-existent file
	nonExistentPath := filepath.Join(tmpDir, "non_existent.txt")
	_, err = GetFileSize(nonExistentPath)
	if err == nil {
		t.Errorf("GetFileSize() did not fail for non-existent file")
	}
}

func TestGetRelativePath(t *testing.T) {
	// Simple paths without checking the file system
	base := "/base/dir"
	target := "/base/dir/subdir/file.txt"

	relPath, err := GetRelativePath(base, target)
	if err != nil {
		t.Errorf("GetRelativePath() failed: %v", err)
	}
	expected := "subdir/file.txt"
	// Check if relPath matches either Unix or Windows separators
	if relPath != expected && relPath != filepath.FromSlash(expected) {
		t.Errorf("GetRelativePath() returned incorrect path: expected %s, got %s", expected, relPath)
	}
}
