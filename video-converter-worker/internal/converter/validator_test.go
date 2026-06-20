package converter

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidator_GetFileSize(t *testing.T) {
	v := NewValidator()

	// Setup a temporary directory for our test files
	tempDir, err := os.MkdirTemp("", "validator_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a test file with known size
	testContent := []byte("hello world")
	expectedSize := int64(len(testContent))
	testFile := filepath.Join(tempDir, "test.txt")
	err = os.WriteFile(testFile, testContent, 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	tests := []struct {
		name     string
		path     string
		wantSize int64
		wantErr  bool
	}{
		{
			name:     "Valid file",
			path:     testFile,
			wantSize: expectedSize,
			wantErr:  false,
		},
		{
			name:     "Non-existent file",
			path:     filepath.Join(tempDir, "nonexistent.txt"),
			wantSize: 0,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			size, err := v.GetFileSize(tt.path)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetFileSize() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if size != tt.wantSize {
				t.Errorf("GetFileSize() size = %v, want %v", size, tt.wantSize)
			}
		})
	}
}
