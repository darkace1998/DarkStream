package scanner

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A file that is still being written must not be scanned. Hashing it mid-write
// records the checksum of a partial file, which the finished file can never
// match — every conversion of it then fails with a checksum mismatch.
func TestProcessFileSkipsFilesStillBeingWritten(t *testing.T) {
	root := t.TempDir()
	out := t.TempDir()

	path := filepath.Join(root, "uploading.mp4")
	if err := os.WriteFile(path, []byte("partial contents"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	s := New(root, []string{".mp4"}, out)
	s.SetOptions(ScanOptions{MinFileAge: time.Hour})

	job, err := s.ProcessFile(path)
	if err != nil {
		t.Fatalf("ProcessFile() error = %v", err)
	}
	if job != nil {
		t.Fatal("ProcessFile() queued a file that was just modified")
	}
}

// Once the writer stops, the file must be picked up normally.
func TestProcessFileAcceptsSettledFiles(t *testing.T) {
	root := t.TempDir()
	out := t.TempDir()

	path := filepath.Join(root, "finished.mp4")
	if err := os.WriteFile(path, []byte("complete contents"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	// Backdate the file so it looks settled without slowing the test down.
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}

	s := New(root, []string{".mp4"}, out)
	s.SetOptions(ScanOptions{MinFileAge: time.Minute})

	job, err := s.ProcessFile(path)
	if err != nil {
		t.Fatalf("ProcessFile() error = %v", err)
	}
	if job == nil {
		t.Fatal("ProcessFile() skipped a settled file")
	}
	if job.SourceChecksum == "" {
		t.Error("settled file was queued without a checksum")
	}
}

// A negative age disables the check, for setups where files appear atomically.
func TestProcessFileMinFileAgeCanBeDisabled(t *testing.T) {
	root := t.TempDir()
	out := t.TempDir()

	path := filepath.Join(root, "fresh.mp4")
	if err := os.WriteFile(path, []byte("just written"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	s := New(root, []string{".mp4"}, out)
	s.SetOptions(ScanOptions{MinFileAge: -1})

	job, err := s.ProcessFile(path)
	if err != nil {
		t.Fatalf("ProcessFile() error = %v", err)
	}
	if job == nil {
		t.Fatal("ProcessFile() skipped a file although the age check was disabled")
	}
}

func TestEffectiveMinFileAge(t *testing.T) {
	tests := []struct {
		name string
		in   time.Duration
		want time.Duration
	}{
		{"zero uses the default", 0, DefaultMinFileAge},
		{"negative disables", -1, 0},
		{"explicit value is kept", 45 * time.Second, 45 * time.Second},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := (ScanOptions{MinFileAge: tc.in}).effectiveMinFileAge(); got != tc.want {
				t.Errorf("effectiveMinFileAge() = %v, want %v", got, tc.want)
			}
		})
	}
}
