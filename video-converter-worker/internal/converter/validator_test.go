package converter

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewValidator(t *testing.T) {
	v := NewValidator()
	if v == nil {
		t.Fatal("Expected NewValidator() to return non-nil *Validator")
	}
}

func TestValidateFile(t *testing.T) {
	v := NewValidator()

	// Setup a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "validator_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a valid file (non-empty)
	validFile := filepath.Join(tempDir, "valid.txt")
	if err := os.WriteFile(validFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to create valid file: %v", err)
	}

	// Create an empty file
	emptyFile := filepath.Join(tempDir, "empty.txt")
	if err := os.WriteFile(emptyFile, []byte(""), 0644); err != nil {
		t.Fatalf("Failed to create empty file: %v", err)
	}

	tests := []struct {
		name      string
		path      string
		expectErr bool
		errString string
	}{
		{
			name:      "Valid non-empty file",
			path:      validFile,
			expectErr: false,
		},
		{
			name:      "Empty file",
			path:      emptyFile,
			expectErr: true,
			errString: "file is empty:",
		},
		{
			name:      "Directory path",
			path:      tempDir,
			expectErr: true,
			errString: "path is a directory, not a file:",
		},
		{
			name:      "Non-existent file",
			path:      filepath.Join(tempDir, "does-not-exist.txt"),
			expectErr: true,
			errString: "file does not exist:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateFile(tt.path)
			if tt.expectErr {
				if err == nil {
					t.Errorf("Expected an error for path %s, but got nil", tt.path)
				} else if tt.errString != "" && !contains(err.Error(), tt.errString) {
					t.Errorf("Expected error to contain %q, but got: %v", tt.errString, err)
				}
			} else {
				if err != nil {
					t.Errorf("Did not expect an error for path %s, but got: %v", tt.path, err)
				}
			}
		})
	}
}

func TestGetFileSize(t *testing.T) {
	v := NewValidator()

	tempDir, err := os.MkdirTemp("", "validator_test_size")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	validFile := filepath.Join(tempDir, "valid.txt")
	content := []byte("1234567890")
	if err := os.WriteFile(validFile, content, 0644); err != nil {
		t.Fatalf("Failed to create valid file: %v", err)
	}

	size, err := v.GetFileSize(validFile)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if size != int64(len(content)) {
		t.Errorf("Expected size %d, got %d", len(content), size)
	}

	_, err = v.GetFileSize(filepath.Join(tempDir, "does-not-exist.txt"))
	if err == nil {
		t.Error("Expected an error for non-existent file, got nil")
	}
}

// contains is a helper to check substring
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
