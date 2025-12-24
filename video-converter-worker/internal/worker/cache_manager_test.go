package worker

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewCacheManager(t *testing.T) {
	tempDir := t.TempDir()
	
	cm := NewCacheManager(tempDir, 1024*1024, 24*time.Hour)
	
	if cm == nil {
		t.Fatal("Expected non-nil cache manager")
	}
	
	if cm.cachePath != tempDir {
		t.Errorf("Expected cache path %s, got %s", tempDir, cm.cachePath)
	}
	
	if cm.maxSize != 1024*1024 {
		t.Errorf("Expected max size %d, got %d", 1024*1024, cm.maxSize)
	}
	
	if cm.maxAge != 24*time.Hour {
		t.Errorf("Expected max age %v, got %v", 24*time.Hour, cm.maxAge)
	}
}

func TestCacheManager_Cleanup_EmptyDir(t *testing.T) {
	tempDir := t.TempDir()
	
	cm := NewCacheManager(tempDir, 1024*1024, 24*time.Hour)
	
	err := cm.Cleanup()
	if err != nil {
		t.Errorf("Cleanup failed on empty directory: %v", err)
	}
	
	if cm.GetCacheSize() != 0 {
		t.Errorf("Expected cache size 0, got %d", cm.GetCacheSize())
	}
}

func TestCacheManager_Cleanup_SizeLimit(t *testing.T) {
	tempDir := t.TempDir()
	
	// Create test files totaling 3000 bytes
	for i := 0; i < 3; i++ {
		filePath := filepath.Join(tempDir, "test"+string(rune('A'+i))+".tmp")
		data := make([]byte, 1000)
		if err := os.WriteFile(filePath, data, 0o644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
		// Stagger file times so we know which gets removed first
		time.Sleep(10 * time.Millisecond)
	}
	
	// Set max size to 2500 bytes (should remove oldest file)
	cm := NewCacheManager(tempDir, 2500, 0)
	
	err := cm.Cleanup()
	if err != nil {
		t.Errorf("Cleanup failed: %v", err)
	}
	
	// Verify only 2 files remain
	entries, _ := cm.listCacheEntries()
	if len(entries) != 2 {
		t.Errorf("Expected 2 files after cleanup, got %d", len(entries))
	}
	
	// Verify cache size is under limit
	if cm.GetCacheSize() > 2500 {
		t.Errorf("Expected cache size <= 2500, got %d", cm.GetCacheSize())
	}
}

func TestCacheManager_Stop(t *testing.T) {
	tempDir := t.TempDir()
	
	cm := NewCacheManager(tempDir, 1024*1024, 24*time.Hour)
	
	if cm.IsStopped() {
		t.Error("Cache manager should not be stopped initially")
	}
	
	cm.Stop()
	
	if !cm.IsStopped() {
		t.Error("Cache manager should be stopped after Stop()")
	}
	
	// Cleanup should be a no-op when stopped
	err := cm.Cleanup()
	if err != nil {
		t.Errorf("Cleanup on stopped manager should not return error: %v", err)
	}
}

func TestCacheManager_AddRemoveFile(t *testing.T) {
	tempDir := t.TempDir()
	
	cm := NewCacheManager(tempDir, 1024*1024, 24*time.Hour)
	
	initialSize := cm.GetCacheSize()
	
	cm.AddFile(1000)
	if cm.GetCacheSize() != initialSize+1000 {
		t.Errorf("Expected size %d, got %d", initialSize+1000, cm.GetCacheSize())
	}
	
	cm.RemoveFile(500)
	if cm.GetCacheSize() != initialSize+500 {
		t.Errorf("Expected size %d, got %d", initialSize+500, cm.GetCacheSize())
	}
	
	// Test that size doesn't go negative
	cm.RemoveFile(10000)
	if cm.GetCacheSize() != 0 {
		t.Errorf("Expected size 0 (not negative), got %d", cm.GetCacheSize())
	}
}
