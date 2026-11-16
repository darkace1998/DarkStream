package scanner

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// When the output directory lives inside the scanned root, converted files must
// not be discovered as new sources.
func TestScanSkipsNestedOutputDirectory(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "converted")
	if err := os.MkdirAll(filepath.Join(output, "sub"), 0o750); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		filepath.Join(root, "movie.mkv"),
		filepath.Join(output, "unrelated.mp4"),
		filepath.Join(output, "sub", "other.mp4"),
	} {
		if err := os.WriteFile(p, []byte(p), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	s := New(root, []string{extMKV, extMP4}, output)
	jobs, err := s.ScanDirectory()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || filepath.Base(jobs[0].SourcePath) != "movie.mkv" {
		paths := make([]string, 0, len(jobs))
		for _, j := range jobs {
			paths = append(paths, j.SourcePath)
		}
		t.Fatalf("expected only movie.mkv, got %v", paths)
	}

	// The watcher path goes through ProcessFile directly.
	if job, _ := s.ProcessFile(filepath.Join(output, "unrelated.mp4")); job != nil {
		t.Error("ProcessFile created a job for a converted output file")
	}
}

func TestIsOutputPath(t *testing.T) {
	root := t.TempDir()

	nested := New(root, []string{extMP4}, filepath.Join(root, "out"))
	if !nested.IsOutputPath(filepath.Join(root, "out", "a.mp4")) || !nested.IsOutputPath(filepath.Join(root, "out")) {
		t.Error("paths inside a nested output directory should be recognised")
	}
	if nested.IsOutputPath(filepath.Join(root, "outside.mp4")) || nested.IsOutputPath(filepath.Join(root, "output2", "a.mp4")) {
		t.Error("paths outside the output directory were treated as output")
	}

	// Output equal to the root, or outside it, never excludes anything.
	same := New(root, []string{extMP4}, root)
	if same.IsOutputPath(filepath.Join(root, "a.mp4")) {
		t.Error("output == root must not exclude sources")
	}
	separate := New(root, []string{extMP4}, t.TempDir())
	if separate.IsOutputPath(filepath.Join(root, "a.mp4")) {
		t.Error("separate output directory must not exclude sources")
	}

	// Replace-source mode writes output over the source by design.
	replace := New(root, []string{extMP4}, filepath.Join(root, "out"))
	opts := replace.GetOptions()
	opts.ReplaceSource = true
	replace.SetOptions(opts)
	if replace.IsOutputPath(filepath.Join(root, "out", "a.mp4")) {
		t.Error("replace-source mode must not exclude the output directory")
	}
}

// A source whose converted file already exists must not be queued again (for
// example after finished jobs were pruned), unless the source is newer.
func TestScanSkipsAlreadyConvertedSources(t *testing.T) {
	root := t.TempDir()
	output := t.TempDir()
	source := filepath.Join(root, "show", "episode.mkv")
	converted := filepath.Join(output, "show", "episode.webm") // any output container counts
	for _, p := range []string{source, converted} {
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(p), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	s := New(root, []string{extMKV}, output)
	jobs, err := s.ScanDirectory()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatalf("already converted source was queued again: %s", jobs[0].SourcePath)
	}

	// The source changes after the conversion: it needs converting again.
	newer := time.Now().Add(time.Hour)
	if err := os.Chtimes(source, newer, newer); err != nil {
		t.Fatal(err)
	}
	jobs, err = s.ScanDirectory()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected the modified source to be queued, got %d jobs", len(jobs))
	}
}

// Files the system produced itself are recognised by checksum.
func TestScanSkipsKnownOutputs(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "replaced.mp4"), []byte("converted"), 0o600); err != nil {
		t.Fatal(err)
	}

	s := New(root, []string{extMP4}, root)
	opts := s.GetOptions()
	opts.ReplaceSource = true
	opts.KnownOutputChecker = func(string) (bool, error) { return true, nil }
	s.SetOptions(opts)

	jobs, err := s.ScanDirectory()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatal("a known conversion output was queued as a new source")
	}
}
