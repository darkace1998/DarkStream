package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanDirectory(t *testing.T) {
	// Create temporary test directory
	tmpDir := t.TempDir()
	videosDir := filepath.Join(tmpDir, "videos")
	outputDir := filepath.Join(tmpDir, "output")

	if err := os.MkdirAll(videosDir, 0o750); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// Create test video files
	testFiles := []string{
		"video1.mp4",
		"video2.mkv",
		"video3.avi",
		"document.txt", // Should be ignored
	}

	for _, filename := range testFiles {
		path := filepath.Join(videosDir, filename)
		if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
	}

	// Create scanner
	scanner := New(videosDir, []string{".mp4", ".mkv", ".avi"}, outputDir)

	// Scan directory
	jobs, err := scanner.ScanDirectory()
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	// Verify results
	expectedCount := 3 // Only video files, not .txt
	if len(jobs) != expectedCount {
		t.Errorf("Expected %d jobs, got %d", expectedCount, len(jobs))
	}

	// Verify each job has required fields
	for _, job := range jobs {
		if job.ID == "" {
			t.Error("Job ID should not be empty")
		}
		if job.SourcePath == "" {
			t.Error("Job SourcePath should not be empty")
		}
		if job.OutputPath == "" {
			t.Error("Job OutputPath should not be empty")
		}
		if job.Status != "pending" {
			t.Errorf("Expected status 'pending', got '%s'", job.Status)
		}
		if job.MaxRetries != 3 {
			t.Errorf("Expected MaxRetries 3, got %d", job.MaxRetries)
		}
	}
}
