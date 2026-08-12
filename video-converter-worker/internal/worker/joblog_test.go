package worker

import (
	"strings"
	"sync"
	"testing"

	"github.com/darkace1998/video-converter-common/models"
)

func TestJobLogRecordsEntries(t *testing.T) {
	l := NewJobLog()
	l.Info(models.JobStageDownload, "downloaded")
	l.Warn(models.JobStageConvert, "odd but fine")
	l.Error(models.JobStageComplete, "boom")

	entries := l.Entries()
	if len(entries) != 3 {
		t.Fatalf("Entries() = %d, want 3", len(entries))
	}
	if entries[0].Stage != models.JobStageDownload || entries[0].Level != models.JobLogInfo {
		t.Errorf("first entry = %+v, want download/info", entries[0])
	}
	if entries[2].Level != models.JobLogError {
		t.Errorf("last entry level = %q, want error", entries[2].Level)
	}
}

// Blank lines are noise in FFmpeg output and must not become log entries.
func TestJobLogSkipsEmptyLines(t *testing.T) {
	l := NewJobLog()
	l.Info(models.JobStageConvert, "")
	l.Info(models.JobStageConvert, "   \n")

	if got := len(l.Entries()); got != 0 {
		t.Errorf("Entries() = %d, want 0 for blank input", got)
	}
}

func TestJobLogTruncatesLongLines(t *testing.T) {
	l := NewJobLog()
	l.Info(models.JobStageConvert, strings.Repeat("x", maxJobLogMessage*2))

	entries := l.Entries()
	if len(entries) != 1 {
		t.Fatalf("Entries() = %d, want 1", len(entries))
	}
	if len(entries[0].Message) > maxJobLogMessage+4 {
		t.Errorf("message length = %d, want it truncated near %d",
			len(entries[0].Message), maxJobLogMessage)
	}
}

// The failure is almost always at the end, and the setup at the start, so both
// ends must survive when a job produces more lines than the cap.
func TestJobLogKeepsFirstAndLastLinesWhenOverflowing(t *testing.T) {
	l := NewJobLog()

	total := maxJobLogEntries * 3
	l.Info(models.JobStageConvert, "FIRST")
	for i := 0; i < total; i++ {
		l.Info(models.JobStageConvert, "filler")
	}
	l.Error(models.JobStageComplete, "LAST")

	entries := l.Entries()
	if len(entries) > maxJobLogEntries+1 {
		t.Fatalf("Entries() = %d, want at most %d (plus the omission marker)",
			len(entries), maxJobLogEntries)
	}

	joined := make([]string, 0, len(entries))
	for _, e := range entries {
		joined = append(joined, e.Message)
	}
	all := strings.Join(joined, "\n")

	if !strings.Contains(all, "FIRST") {
		t.Error("the first line was dropped; job setup context is lost")
	}
	if !strings.Contains(all, "LAST") {
		t.Error("the last line was dropped; the actual failure is lost")
	}
	if !strings.Contains(all, "omitted") {
		t.Error("no marker indicates that lines were dropped")
	}
}

func TestDetectLevel(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{"[vost#0:0/libx264] Could not open encoder before EOF", models.JobLogError},
		{"Error while opening encoder", models.JobLogError},
		{"Conversion failed!", models.JobLogError},
		{"Invalid argument", models.JobLogError},
		{"deprecated pixel format used", models.JobLogWarn},
		{"Stream #0:0: Video: h264, yuv420p, 1920x1080", models.JobLogInfo},
		{"  libavcodec 63. 1.101", models.JobLogInfo},
	}

	for _, tc := range tests {
		if got := detectLevel(tc.line); got != tc.want {
			t.Errorf("detectLevel(%q) = %q, want %q", tc.line, got, tc.want)
		}
	}
}

// FFmpeg output is drained on its own goroutine while the job goroutine records
// stage transitions, so the collector must tolerate concurrent writers.
func TestJobLogIsConcurrencySafe(t *testing.T) {
	l := NewJobLog()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				l.Info(models.JobStageConvert, "line")
			}
		}()
	}
	wg.Wait()

	if got := len(l.Entries()); got == 0 {
		t.Error("Entries() is empty after concurrent writes")
	}
}
