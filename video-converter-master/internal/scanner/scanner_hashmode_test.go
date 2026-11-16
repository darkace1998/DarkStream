package scanner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darkace1998/video-converter-common/models"
)

// writeHashModeFixture creates a videos dir with one small mp4 and returns
// the paths plus the file content.
func writeHashModeFixture(t *testing.T) (videosDir, outputDir string) {
	t.Helper()
	tmpDir := t.TempDir()
	videosDir = filepath.Join(tmpDir, "videos")
	outputDir = filepath.Join(tmpDir, "output")
	if err := os.MkdirAll(videosDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := strings.Repeat("video-bytes-", 100)
	if err := os.WriteFile(filepath.Join(videosDir, "clip.mp4"), []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return videosDir, outputDir
}

func TestScanHashModeSkipNeverReadsFile(t *testing.T) {
	videosDir, outputDir := writeHashModeFixture(t)

	scanner := New(videosDir, []string{extMP4}, outputDir)
	scanner.SetOptions(ScanOptions{HashMode: models.HashModeSkip})

	jobs, err := scanner.ScanDirectory()
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	job := jobs[0]
	if job.SourceChecksum != "" {
		t.Errorf("hash_mode=skip must not compute a checksum, got %q", job.SourceChecksum)
	}
	if job.SourceChecksumAlgo != models.HashModeSkip {
		t.Errorf("expected algo 'skip', got %q", job.SourceChecksumAlgo)
	}

	stats := scanner.Stats()
	if stats.Hashed != 0 {
		t.Errorf("hash_mode=skip must not read file contents, Hashed=%d", stats.Hashed)
	}
	if stats.Videos != 1 {
		t.Errorf("expected 1 video considered, got %d", stats.Videos)
	}
}

func TestScanHashModeFullComputesSHA256(t *testing.T) {
	videosDir, outputDir := writeHashModeFixture(t)

	scanner := New(videosDir, []string{extMP4}, outputDir)
	scanner.SetOptions(ScanOptions{HashMode: models.HashModeFull})

	jobs, err := scanner.ScanDirectory()
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	job := jobs[0]
	if len(job.SourceChecksum) != 64 {
		t.Errorf("expected 64-char sha256 hex, got %q", job.SourceChecksum)
	}
	if job.SourceChecksumAlgo != "sha256" {
		t.Errorf("expected algo sha256, got %q", job.SourceChecksumAlgo)
	}

	stats := scanner.Stats()
	if stats.Hashed != 1 {
		t.Errorf("expected Hashed=1 in full mode, got %d", stats.Hashed)
	}
}

func TestScanHashAlgoSHA1(t *testing.T) {
	videosDir, outputDir := writeHashModeFixture(t)

	scanner := New(videosDir, []string{extMP4}, outputDir)
	scanner.SetOptions(ScanOptions{HashAlgo: "sha1"})

	jobs, err := scanner.ScanDirectory()
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if len(jobs[0].SourceChecksum) != 40 {
		t.Errorf("expected 40-char sha1 hex, got %q", jobs[0].SourceChecksum)
	}
	if jobs[0].SourceChecksumAlgo != "sha1" {
		t.Errorf("expected algo sha1, got %q", jobs[0].SourceChecksumAlgo)
	}
}
