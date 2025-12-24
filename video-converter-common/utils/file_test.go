package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileExists(t *testing.T) {
	// Create a temp file
	tmpFile, err := os.CreateTemp(t.TempDir(), "test")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	_ = tmpFile.Close()

	if !FileExists(tmpFile.Name()) {
		t.Errorf("FileExists(%q) = false, want true", tmpFile.Name())
	}

	if FileExists("/nonexistent/file") {
		t.Error("FileExists(/nonexistent/file) = true, want false")
	}

	// Test that directory returns false
	tmpDir := t.TempDir()
	if FileExists(tmpDir) {
		t.Errorf("FileExists(%q) = true for directory, want false", tmpDir)
	}
}

func TestDirExists(t *testing.T) {
	tmpDir := t.TempDir()

	if !DirExists(tmpDir) {
		t.Errorf("DirExists(%q) = false, want true", tmpDir)
	}

	if DirExists("/nonexistent/dir") {
		t.Error("DirExists(/nonexistent/dir) = true, want false")
	}

	// Test that file returns false
	tmpFile, err := os.CreateTemp(t.TempDir(), "test")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	_ = tmpFile.Close()

	if DirExists(tmpFile.Name()) {
		t.Errorf("DirExists(%q) = true for file, want false", tmpFile.Name())
	}
}

func TestEnsureDir(t *testing.T) {
	tmpDir := t.TempDir()
	newDir := filepath.Join(tmpDir, "new", "nested", "dir")

	if err := EnsureDir(newDir); err != nil {
		t.Fatalf("EnsureDir() error = %v", err)
	}

	if !DirExists(newDir) {
		t.Error("EnsureDir() did not create directory")
	}

	// Should not error if already exists
	if err := EnsureDir(newDir); err != nil {
		t.Errorf("EnsureDir() on existing dir error = %v", err)
	}
}

func TestCopyFile(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "source.txt")
	dstPath := filepath.Join(tmpDir, "dest.txt")

	content := []byte("test content")
	if err := os.WriteFile(srcPath, content, 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	if err := CopyFile(srcPath, dstPath); err != nil {
		t.Fatalf("CopyFile() error = %v", err)
	}

	dstContent, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("failed to read destination file: %v", err)
	}

	if string(dstContent) != string(content) {
		t.Errorf("CopyFile() content mismatch: got %q, want %q", dstContent, content)
	}
}

func TestMoveFile(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "source.txt")
	dstPath := filepath.Join(tmpDir, "dest.txt")

	content := []byte("test content")
	if err := os.WriteFile(srcPath, content, 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	if err := MoveFile(srcPath, dstPath); err != nil {
		t.Fatalf("MoveFile() error = %v", err)
	}

	if FileExists(srcPath) {
		t.Error("MoveFile() did not remove source file")
	}

	if !FileExists(dstPath) {
		t.Error("MoveFile() did not create destination file")
	}

	dstContent, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("failed to read destination file: %v", err)
	}

	if string(dstContent) != string(content) {
		t.Errorf("MoveFile() content mismatch: got %q, want %q", dstContent, content)
	}
}

func TestGetFileSize(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.txt")
	content := []byte("test content")

	if err := os.WriteFile(filePath, content, 0o600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	size, err := GetFileSize(filePath)
	if err != nil {
		t.Fatalf("GetFileSize() error = %v", err)
	}

	if size != int64(len(content)) {
		t.Errorf("GetFileSize() = %d, want %d", size, len(content))
	}
}

func TestGetBasenameWithoutExt(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/path/to/file.txt", "file"},
		{"file.txt", "file"},
		{"/path/to/file.tar.gz", "file.tar"},
		{"noextension", "noextension"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := GetBaseNameWithoutExt(tt.path)
			if got != tt.want {
				t.Errorf("GetBaseNameWithoutExt(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestIsHiddenFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{".hidden", true},
		{"/path/to/.hidden", true},
		{"visible", false},
		{"/path/to/visible", false},
		{".hidden/file", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := IsHiddenFile(tt.path)
			if got != tt.want {
				t.Errorf("IsHiddenFile(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestListFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create files and directories
	files := []string{"file1.txt", "file2.txt"}
	dirs := []string{"dir1", "dir2"}

	for _, f := range files {
		if err := os.WriteFile(filepath.Join(tmpDir, f), []byte("test"), 0o600); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}
	}
	for _, d := range dirs {
		if err := os.Mkdir(filepath.Join(tmpDir, d), 0o750); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
	}

	listed, err := ListFiles(tmpDir)
	if err != nil {
		t.Fatalf("ListFiles() error = %v", err)
	}

	if len(listed) != len(files) {
		t.Errorf("ListFiles() returned %d files, want %d", len(listed), len(files))
	}
}

func TestListDirs(t *testing.T) {
	tmpDir := t.TempDir()

	// Create files and directories
	files := []string{"file1.txt", "file2.txt"}
	dirs := []string{"dir1", "dir2"}

	for _, f := range files {
		if err := os.WriteFile(filepath.Join(tmpDir, f), []byte("test"), 0o600); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}
	}
	for _, d := range dirs {
		if err := os.Mkdir(filepath.Join(tmpDir, d), 0o750); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
	}

	listed, err := ListDirs(tmpDir)
	if err != nil {
		t.Fatalf("ListDirs() error = %v", err)
	}

	if len(listed) != len(dirs) {
		t.Errorf("ListDirs() returned %d dirs, want %d", len(listed), len(dirs))
	}
}

func TestReadWriteFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.txt")
	content := []byte("test content")

	if err := WriteFile(filePath, content); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	readContent, err := ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if string(readContent) != string(content) {
		t.Errorf("ReadFile() content mismatch: got %q, want %q", readContent, content)
	}
}

func TestSafeRemove(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.txt")

	// Should not error on non-existent file
	if err := SafeRemove(filePath); err != nil {
		t.Errorf("SafeRemove() on non-existent file error = %v", err)
	}

	// Create and remove file
	if err := os.WriteFile(filePath, []byte("test"), 0o600); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	if err := SafeRemove(filePath); err != nil {
		t.Fatalf("SafeRemove() error = %v", err)
	}

	if FileExists(filePath) {
		t.Error("SafeRemove() did not remove file")
	}
}

func TestTempDir(t *testing.T) {
	parentDir := t.TempDir()
	tmpDir, err := TempDir(parentDir, "test")
	if err != nil {
		t.Fatalf("TempDir() error = %v", err)
	}

	if !DirExists(tmpDir) {
		t.Error("TempDir() did not create directory")
	}
}

func TestTempFile(t *testing.T) {
	tmpDir := t.TempDir()
	f, err := TempFile(tmpDir, "test")
	if err != nil {
		t.Fatalf("TempFile() error = %v", err)
	}
	defer func() { _ = f.Close() }()

	if !FileExists(f.Name()) {
		t.Error("TempFile() did not create file")
	}
}
