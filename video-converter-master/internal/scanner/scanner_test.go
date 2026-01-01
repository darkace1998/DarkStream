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

func TestScannerWithDepthLimit(t *testing.T) {
	tmpDir := t.TempDir()
	videosDir := filepath.Join(tmpDir, "videos")
	outputDir := filepath.Join(tmpDir, "output")

	// Create nested directory structure
	// videos/
	//   video1.mp4 (depth 0)
	//   subdir1/
	//     video2.mp4 (depth 1)
	//     subdir2/
	//       video3.mp4 (depth 2)
	if err := os.MkdirAll(filepath.Join(videosDir, "subdir1", "subdir2"), 0o750); err != nil {
		t.Fatalf("Failed to create nested directories: %v", err)
	}

	testFiles := map[string]string{
		"video1.mp4":                 "root level",
		"subdir1/video2.mp4":         "depth 1",
		"subdir1/subdir2/video3.mp4": "depth 2",
	}

	for relPath, content := range testFiles {
		fullPath := filepath.Join(videosDir, relPath)
		if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
			t.Fatalf("Failed to create test file %s: %v", relPath, err)
		}
	}

	// Test with depth limit 0 (root only)
	scanner := New(videosDir, []string{".mp4"}, outputDir)
	scanner.SetOptions(ScanOptions{
		MaxDepth: 0,
	})

	jobs, err := scanner.ScanDirectory()
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	if len(jobs) != 1 {
		t.Errorf("Expected 1 job at depth 0, got %d", len(jobs))
	}

	// Test with depth limit 1
	scanner.SetOptions(ScanOptions{
		MaxDepth: 1,
	})

	jobs, err = scanner.ScanDirectory()
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	if len(jobs) != 2 {
		t.Errorf("Expected 2 jobs at depth 1, got %d", len(jobs))
	}

	// Test with unlimited depth
	scanner.SetOptions(ScanOptions{
		MaxDepth: -1,
	})

	jobs, err = scanner.ScanDirectory()
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	if len(jobs) != 3 {
		t.Errorf("Expected 3 jobs with unlimited depth, got %d", len(jobs))
	}
}

func TestScannerWithSizeFiltering(t *testing.T) {
	tmpDir := t.TempDir()
	videosDir := filepath.Join(tmpDir, "videos")
	outputDir := filepath.Join(tmpDir, "output")

	if err := os.MkdirAll(videosDir, 0o750); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// Create files with different sizes
	files := map[string]int{
		"small.mp4":  100,   // 100 bytes
		"medium.mp4": 5000,  // 5KB
		"large.mp4":  10000, // 10KB
	}

	for filename, size := range files {
		path := filepath.Join(videosDir, filename)
		data := make([]byte, size)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
	}

	// Test with minimum size filter (skip files smaller than 1KB)
	scanner := New(videosDir, []string{".mp4"}, outputDir)
	scanner.SetOptions(ScanOptions{
		MinFileSize: 1000, // 1KB minimum
	})

	jobs, err := scanner.ScanDirectory()
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	if len(jobs) != 2 {
		t.Errorf("Expected 2 jobs with min size filter, got %d", len(jobs))
	}

	// Test with maximum size filter (skip files larger than 6KB)
	scanner.SetOptions(ScanOptions{
		MaxFileSize: 6000, // 6KB maximum
	})

	jobs, err = scanner.ScanDirectory()
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	if len(jobs) != 2 {
		t.Errorf("Expected 2 jobs with max size filter, got %d", len(jobs))
	}

	// Test with both min and max size filters
	scanner.SetOptions(ScanOptions{
		MinFileSize: 1000,  // 1KB minimum
		MaxFileSize: 6000,  // 6KB maximum
	})

	jobs, err = scanner.ScanDirectory()
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	if len(jobs) != 1 {
		t.Errorf("Expected 1 job with min/max size filter, got %d", len(jobs))
	}
}

func TestScannerWithHiddenFiles(t *testing.T) {
	tmpDir := t.TempDir()
	videosDir := filepath.Join(tmpDir, "videos")
	outputDir := filepath.Join(tmpDir, "output")

	if err := os.MkdirAll(videosDir, 0o750); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// Create hidden directory
	hiddenDir := filepath.Join(videosDir, ".hidden")
	if err := os.MkdirAll(hiddenDir, 0o750); err != nil {
		t.Fatalf("Failed to create hidden directory: %v", err)
	}

	// Create test files explicitly
	// 1. Visible file in root
	if err := os.WriteFile(filepath.Join(videosDir, "visible.mp4"), []byte("test"), 0o600); err != nil {
		t.Fatalf("Failed to create visible.mp4: %v", err)
	}
	
	// 2. Hidden file in root
	if err := os.WriteFile(filepath.Join(videosDir, ".hidden_file.mp4"), []byte("test"), 0o600); err != nil {
		t.Fatalf("Failed to create .hidden_file.mp4: %v", err)
	}
	
	// 3. File in hidden directory
	if err := os.WriteFile(filepath.Join(hiddenDir, "video.mp4"), []byte("test"), 0o600); err != nil {
		t.Fatalf("Failed to create .hidden/video.mp4: %v", err)
	}

	// Test with hidden files/dirs skipped (default)
	scanner := New(videosDir, []string{".mp4"}, outputDir)
	scanner.SetOptions(ScanOptions{
		MaxDepth:        -1, // Unlimited depth
		SkipHiddenFiles: true,
		SkipHiddenDirs:  true,
	})

	jobs, err := scanner.ScanDirectory()
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	if len(jobs) != 1 {
		t.Errorf("Expected 1 job (only visible.mp4), got %d", len(jobs))
	}

	// Test with hidden files included but hidden dirs skipped
	scanner.SetOptions(ScanOptions{
		MaxDepth:        -1, // Unlimited depth
		SkipHiddenFiles: false,
		SkipHiddenDirs:  true,
	})

	jobs, err = scanner.ScanDirectory()
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	if len(jobs) != 2 {
		t.Errorf("Expected 2 jobs (visible + hidden file in root), got %d", len(jobs))
	}

	// Test with all hidden content included
	scanner.SetOptions(ScanOptions{
		MaxDepth:        -1, // Unlimited depth
		SkipHiddenFiles: false,
		SkipHiddenDirs:  false,
	})

	jobs, err = scanner.ScanDirectory()
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	if len(jobs) != 3 {
		t.Errorf("Expected 3 jobs (all files including file in hidden dir), got %d", len(jobs))
		for i, job := range jobs {
			t.Logf("Job %d: %s", i, job.SourcePath)
		}
	}
}

func TestScannerWithDuplicateDetection(t *testing.T) {
	tmpDir := t.TempDir()
	videosDir := filepath.Join(tmpDir, "videos")
	outputDir := filepath.Join(tmpDir, "output")

	if err := os.MkdirAll(videosDir, 0o750); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// Create files with identical content
	content := []byte("identical video content")
	files := []string{
		"video1.mp4",
		"video2.mp4", // Duplicate content
		"video3.mp4", // Different content
	}

	// Write identical content to first two files
	for i, filename := range files[:2] {
		path := filepath.Join(videosDir, filename)
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatalf("Failed to create test file %d: %v", i, err)
		}
	}

	// Write different content to third file
	if err := os.WriteFile(filepath.Join(videosDir, files[2]), []byte("different content"), 0o600); err != nil {
		t.Fatalf("Failed to create test file 3: %v", err)
	}

	// Test without duplicate detection
	scanner := New(videosDir, []string{".mp4"}, outputDir)
	scanner.SetOptions(ScanOptions{
		DetectDuplicates: false,
	})

	jobs, err := scanner.ScanDirectory()
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	if len(jobs) != 3 {
		t.Errorf("Expected 3 jobs without duplicate detection, got %d", len(jobs))
	}

	// Test with duplicate detection
	scanner.SetOptions(ScanOptions{
		DetectDuplicates: true,
	})

	jobs, err = scanner.ScanDirectory()
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	if len(jobs) != 2 {
		t.Errorf("Expected 2 jobs with duplicate detection (one duplicate removed), got %d", len(jobs))
	}
}

func TestScannerWithReplaceSource(t *testing.T) {
	tmpDir := t.TempDir()
	videosDir := filepath.Join(tmpDir, "videos")
	outputDir := filepath.Join(tmpDir, "output")

	if err := os.MkdirAll(videosDir, 0o750); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// Create test file
	testFile := filepath.Join(videosDir, "video.mp4")
	if err := os.WriteFile(testFile, []byte("test"), 0o600); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test with ReplaceSource = false (default)
	scanner := New(videosDir, []string{".mp4"}, outputDir)
	scanner.SetOptions(ScanOptions{
		ReplaceSource: false,
	})

	jobs, err := scanner.ScanDirectory()
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	if len(jobs) != 1 {
		t.Fatalf("Expected 1 job, got %d", len(jobs))
	}

	// Output path should be in output directory
	if jobs[0].OutputPath == jobs[0].SourcePath {
		t.Error("Output path should differ from source path when ReplaceSource is false")
	}

	// Test with ReplaceSource = true
	scanner.SetOptions(ScanOptions{
		ReplaceSource: true,
	})

	jobs, err = scanner.ScanDirectory()
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	if len(jobs) != 1 {
		t.Fatalf("Expected 1 job, got %d", len(jobs))
	}

	// Output path should be same as source path
	if jobs[0].OutputPath != jobs[0].SourcePath {
		t.Errorf("Output path should equal source path when ReplaceSource is true. Got source=%s, output=%s",
			jobs[0].SourcePath, jobs[0].OutputPath)
	}
}
