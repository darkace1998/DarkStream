package worker

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Enforcing the size limit must never evict the files of a running job.
func TestCleanupSkipsProtectedJobDirectories(t *testing.T) {
	cache := t.TempDir()
	active := filepath.Join(cache, "job_active")
	stale := filepath.Join(cache, "job_stale")
	for _, dir := range []string{active, stale} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}

	activeFile := filepath.Join(active, "source.mkv")
	staleFile := filepath.Join(stale, "source.mkv")
	for _, f := range []string{activeFile, staleFile} {
		if err := os.WriteFile(f, make([]byte, 1024), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// The active job's file is the oldest, i.e. the first eviction candidate.
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(activeFile, old, old); err != nil {
		t.Fatal(err)
	}

	cm := NewCacheManager(cache, 100, time.Hour) // limits that everything exceeds
	cm.Protect(active)

	if err := cm.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(activeFile); err != nil {
		t.Errorf("file of a running job was evicted: %v", err)
	}
	if _, err := os.Stat(staleFile); err == nil {
		t.Error("unprotected file should have been evicted")
	}

	cm.Unprotect(active)
	if err := cm.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(activeFile); err == nil {
		t.Error("file should be evictable again once the job is finished")
	}
}
