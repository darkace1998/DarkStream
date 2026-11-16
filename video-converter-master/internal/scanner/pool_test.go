package scanner

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/darkace1998/video-converter-common/utils"
)

// memFileStore is an in-memory FileStore for tests.
type memFileStore struct {
	mu   sync.Mutex
	data map[string]struct {
		size    int64
		mtimeNs int64
		hash    string
		algo    string
	}
}

func newMemFileStore() *memFileStore {
	return &memFileStore{data: map[string]struct {
		size    int64
		mtimeNs int64
		hash    string
		algo    string
	}{}}
}

func (m *memFileStore) GetFileScanState(path string) (int64, int64, string, string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.data[path]
	if !ok {
		return 0, 0, "", "", false, nil
	}
	return rec.size, rec.mtimeNs, rec.hash, rec.algo, true, nil
}

func (m *memFileStore) UpsertFileScanState(path string, size int64, mtimeNs int64, hash, algo string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[path] = struct {
		size    int64
		mtimeNs int64
		hash    string
		algo    string
	}{size, mtimeNs, hash, algo}
	return nil
}

// makeVideoTree creates a directory tree with n video files and some non-video files.
func makeVideoTree(t *testing.T, root string, n int) []string {
	t.Helper()
	paths := make([]string, 0, n)
	for i := range n {
		sub := filepath.Join(root, fmt.Sprintf("sub%d", i%3))
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(sub, fmt.Sprintf("video%d.mp4", i))
		if err := os.WriteFile(p, fmt.Appendf(nil, "video content %d", i), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	// Non-video files that must be skipped by the extension pre-filter.
	for i := range 5 {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("notes%d.txt", i)), []byte("text"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return paths
}

func newTestPoolScanner(root, output string, opts ScanOptions) *Scanner {
	s := New(root, []string{".mp4", ".mkv"}, output)
	opts.MaxDepth = -1
	s.SetOptions(opts)
	return s
}

func TestScanDirectoryParallel_FindsAllVideos(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(t.TempDir(), "out")
	paths := makeVideoTree(t, root, 25)

	s := newTestPoolScanner(root, output, ScanOptions{})
	jobs, stats, err := s.ScanDirectoryParallel(8)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(jobs) != len(paths) {
		t.Errorf("jobs = %d, want %d", len(jobs), len(paths))
	}
	if stats.Hashed != len(paths) {
		t.Errorf("hashed = %d, want %d", stats.Hashed, len(paths))
	}
	if stats.Files != 5 {
		t.Errorf("non-video files counted = %d, want 5", stats.Files)
	}
	if stats.Videos != len(paths) {
		t.Errorf("videos = %d, want %d", stats.Videos, len(paths))
	}
	if stats.Jobs != len(jobs) {
		t.Errorf("stats.Jobs = %d, want %d", stats.Jobs, len(jobs))
	}
	for _, j := range jobs {
		if len(j.SourceChecksum) != 64 {
			t.Errorf("default checksum length = %d, want 64 (sha256)", len(j.SourceChecksum))
		}
	}
}

func TestScanDirectoryParallel_SecondScanSkipsUnchanged(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(t.TempDir(), "out")
	paths := makeVideoTree(t, root, 10)
	store := newMemFileStore()

	s := newTestPoolScanner(root, output, ScanOptions{FileStore: store, HashAlgo: utils.HashAlgoSHA1})

	jobs1, stats1, err := s.ScanDirectoryParallel(4)
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if stats1.Hashed != len(paths) {
		t.Fatalf("first scan hashed = %d, want %d", stats1.Hashed, len(paths))
	}
	if len(jobs1) != len(paths) {
		t.Fatalf("first scan jobs = %d", len(jobs1))
	}
	for _, j := range jobs1 {
		if len(j.SourceChecksum) != 40 {
			t.Fatalf("sha1 checksum length = %d, want 40", len(j.SourceChecksum))
		}
		if j.SourceChecksumAlgo != utils.HashAlgoSHA1 {
			t.Fatalf("algo = %s, want sha1", j.SourceChecksumAlgo)
		}
	}

	// Second scan: all files unchanged -> no re-hashing, but jobs are still
	// produced (the DB dedups them via CreateJob).
	jobs2, stats2, err := s.ScanDirectoryParallel(4)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if stats2.Hashed != 0 {
		t.Errorf("second scan hashed = %d, want 0 (unchanged files must not be re-read)", stats2.Hashed)
	}
	if stats2.SkippedUnchanged != len(paths) {
		t.Errorf("second scan skipped_unchanged = %d, want %d", stats2.SkippedUnchanged, len(paths))
	}
	if len(jobs2) != len(paths) {
		t.Errorf("second scan jobs = %d, want %d (stored hash reused)", len(jobs2), len(paths))
	}
	for _, j := range jobs2 {
		if len(j.SourceChecksum) != 40 {
			t.Errorf("reused checksum length = %d, want 40", len(j.SourceChecksum))
		}
	}

	// Touch one file -> only that one is re-hashed.
	changed := paths[0]
	if err := os.WriteFile(changed, []byte("modified content"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, stats3, err := s.ScanDirectoryParallel(4)
	if err != nil {
		t.Fatalf("third scan: %v", err)
	}
	if stats3.Hashed != 1 {
		t.Errorf("third scan hashed = %d, want 1", stats3.Hashed)
	}
	if stats3.SkippedUnchanged != len(paths)-1 {
		t.Errorf("third scan skipped_unchanged = %d, want %d", stats3.SkippedUnchanged, len(paths)-1)
	}
}

func TestScanDirectoryParallel_DuplicateDetection(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(t.TempDir(), "out")
	content := []byte("identical content")
	p1 := filepath.Join(root, "a.mp4")
	p2 := filepath.Join(root, "b.mp4")
	for _, p := range []string{p1, p2} {
		if err := os.WriteFile(p, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	s := newTestPoolScanner(root, output, ScanOptions{DetectDuplicates: true})
	jobs, stats, err := s.ScanDirectoryParallel(4)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(jobs) != 1 {
		t.Errorf("jobs = %d, want 1 (duplicate skipped)", len(jobs))
	}
	if stats.Duplicates != 1 {
		t.Errorf("duplicates = %d, want 1", stats.Duplicates)
	}
}
