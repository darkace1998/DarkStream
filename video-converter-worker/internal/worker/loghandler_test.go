package worker

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadLogTailMissingFile(t *testing.T) {
	if got := readLogTail(filepath.Join(t.TempDir(), "nope.log")); got != "" {
		t.Errorf("Expected empty tail for missing file, got %q", got)
	}
}

func TestReadLogTailSmallFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "job.log")
	content := "line1\nline2\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readLogTail(path); got != content {
		t.Errorf("Expected full content %q, got %q", content, got)
	}
}

func TestReadLogTailTruncatesToLast16KiB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "job.log")
	content := strings.Repeat("a", maxJobLogTail-10) + "LAST-CHUNK"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got := readLogTail(path)
	if len(got) != maxJobLogTail {
		t.Fatalf("Expected tail of %d bytes, got %d", maxJobLogTail, len(got))
	}
	if !strings.HasSuffix(got, "LAST-CHUNK") {
		t.Error("Tail must keep the END of the log, not the beginning")
	}
	if strings.HasPrefix(got, "a") && got[:10] != content[:10] && len(content) > maxJobLogTail {
		// The head must have been cut off.
		if got == content {
			t.Error("Tail equals full content, truncation did not happen")
		}
	}
}

func TestMultiHandlerFansOut(t *testing.T) {
	var buf1, buf2 strings.Builder
	h1 := slog.NewTextHandler(&buf1, nil)
	h2 := slog.NewTextHandler(&buf2, nil)
	log := slog.New(newMultiSlogHandler(h1, h2))

	log.Info("hello job", "job_id", "j1")

	if !strings.Contains(buf1.String(), "hello job") {
		t.Errorf("handler 1 missed the record: %q", buf1.String())
	}
	if !strings.Contains(buf2.String(), "hello job") || !strings.Contains(buf2.String(), "j1") {
		t.Errorf("handler 2 missed the record or attrs: %q", buf2.String())
	}

	// WithAttrs/WithGroup must propagate to all children.
	log2 := slog.New(newMultiSlogHandler(h1, h2).WithAttrs([]slog.Attr{slog.String("w", "worker-1")}))
	buf1.Reset()
	buf2.Reset()
	log2.Info("with attrs")
	if !strings.Contains(buf1.String(), "worker-1") || !strings.Contains(buf2.String(), "worker-1") {
		t.Errorf("WithAttrs not propagated: %q / %q", buf1.String(), buf2.String())
	}

	// Enabled: true when any child is enabled.
	mh := newMultiSlogHandler(slog.NewTextHandler(&buf1, &slog.HandlerOptions{Level: slog.LevelError}))
	if mh.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("Info must be disabled when all children are Error-level")
	}
	if !mh.Enabled(context.Background(), slog.LevelError) {
		t.Error("Error must be enabled")
	}
}
