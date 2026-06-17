package scanner

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatcher(t *testing.T) {
	// Create temporary directory for testing
	tempDir := t.TempDir()

	// Initialize scanner
	scn := New(tempDir, []string{".mp4", ".mkv"}, tempDir)
	scn.SetOptions(ScanOptions{
		MaxDepth:        -1,
		SkipHiddenFiles: true,
		SkipHiddenDirs:  true,
	})

	// Initialize watcher
	watcher, err := NewWatcher(scn)
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}
	defer watcher.Close()

	if err := watcher.Start(); err != nil {
		t.Fatalf("Failed to start watcher: %v", err)
	}

	// Wait for watcher to initialize
	time.Sleep(100 * time.Millisecond)

	// Test 1: Create a valid video file
	validFile := filepath.Join(tempDir, "test1.mp4")
	if err := os.WriteFile(validFile, []byte("dummy video content"), 0600); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Expect job on channel. The debounce timer is 2s, so we should wait up to 3s
	select {
	case job := <-watcher.Jobs:
		if job.SourcePath != validFile {
			t.Errorf("Expected job for %s, got %s", validFile, job.SourcePath)
		}
	case <-time.After(3 * time.Second):
		t.Error("Timed out waiting for watcher event (valid file)")
	}

	// Test 2: Create a file with invalid extension
	invalidExtFile := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(invalidExtFile, []byte("text content"), 0600); err != nil {
		t.Fatalf("Failed to create invalid file: %v", err)
	}

	// Expect NO job on channel
	select {
	case job := <-watcher.Jobs:
		t.Errorf("Received unexpected job for invalid file: %v", job.SourcePath)
	case <-time.After(3 * time.Second):
		// Expected behavior
	}

	// Test 3: Create a subdirectory and a valid video file inside it
	subDir := filepath.Join(tempDir, "subdir")
	if err := os.Mkdir(subDir, 0700); err != nil {
		t.Fatalf("Failed to create subdirectory: %v", err)
	}

	// Wait for watcher to add directory (increase wait time as some OS/FS take longer)
	time.Sleep(500 * time.Millisecond)

	validSubFile := filepath.Join(subDir, "test2.mkv")
	if err := os.WriteFile(validSubFile, []byte("dummy video content"), 0600); err != nil {
		t.Fatalf("Failed to create test file in subdir: %v", err)
	}

	// Expect job on channel
	select {
	case job := <-watcher.Jobs:
		if job.SourcePath != validSubFile {
			t.Errorf("Expected job for %s, got %s", validSubFile, job.SourcePath)
		}
	case <-time.After(5 * time.Second):
		t.Error("Timed out waiting for watcher event (file in subdir)")
	}

	// Test 4: Hidden files should be ignored
	hiddenFile := filepath.Join(tempDir, ".hidden.mp4")
	if err := os.WriteFile(hiddenFile, []byte("dummy video content"), 0600); err != nil {
		t.Fatalf("Failed to create hidden file: %v", err)
	}

	// Expect NO job on channel
	select {
	case job := <-watcher.Jobs:
		t.Errorf("Received unexpected job for hidden file: %v", job.SourcePath)
	case <-time.After(3 * time.Second):
		// Expected behavior
	}
}
