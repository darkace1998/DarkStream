package db

import (
	"path/filepath"
	"testing"
)

// fileScanStateKnown reports just the ok flag of GetFileScanState.
func fileScanStateKnown(tracker *Tracker, path string) (bool, error) {
	var size, mtimeNs int64
	var hash, algo string
	var ok bool
	var err error
	size, mtimeNs, hash, algo, ok, err = tracker.GetFileScanState(path)
	_ = size
	_ = mtimeNs
	_ = hash
	_ = algo
	return ok, err
}

func TestFileScanState_Roundtrip(t *testing.T) {
	tracker, err := New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = tracker.Close() }()

	// Unknown file -> ok=false
	ok, err := fileScanStateKnown(tracker, "/videos/a.mp4")
	if err != nil || ok {
		t.Fatalf("unknown file: ok=%v err=%v", ok, err)
	}

	if err := tracker.UpsertFileScanState("/videos/a.mp4", 1234, 999, "abc123", "sha1"); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	size, mtimeNs, hash, algo, ok, err := tracker.GetFileScanState("/videos/a.mp4")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if size != 1234 || mtimeNs != 999 || hash != "abc123" || algo != "sha1" {
		t.Errorf("got size=%d mtime=%d hash=%s algo=%s", size, mtimeNs, hash, algo)
	}

	// Upsert again (update path)
	if err := tracker.UpsertFileScanState("/videos/a.mp4", 2222, 1000, "def456", "sha256"); err != nil {
		t.Fatalf("upsert2: %v", err)
	}
	size, _, hash, algo, ok, err = tracker.GetFileScanState("/videos/a.mp4")
	if err != nil || !ok || size != 2222 || hash != "def456" || algo != "sha256" {
		t.Errorf("after update: size=%d hash=%s algo=%s ok=%v err=%v", size, hash, algo, ok, err)
	}

	if err := tracker.DeleteFileScanState("/videos/a.mp4"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	ok, err = fileScanStateKnown(tracker, "/videos/a.mp4")
	if err != nil || ok {
		t.Errorf("after delete: ok=%v err=%v", ok, err)
	}
}
