package converter

import (
	"log/slog"
	"os"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
}

const sampleFFmpegStderr = `Input #0, mov,mp4, from 'in.mp4':
  Duration: 00:00:10.00, start: 0.000000, bitrate: 1000 kb/s
frame=10
fps=25.0
stream_0_0_q=28.0
bitrate=1000.0kbits/s
total_size=1000
out_time_us=2000000
out_time_ms=2000000
out_time=00:00:02.000000
dup_frames=0
drop_frames=0
speed=1.0x
progress=continue
frame=50
fps=30.5
out_time_ms=10000000
progress=end
[libx264 @ 0x1] kb/s:900.00
Conversion failed!
`

// Progress is reported once per -progress block, not once per line.
func TestTrackProgressReportsOncePerBlock(t *testing.T) {
	fc := NewFFmpegConverter("ffmpeg", nil, 0)

	type report struct{ progress, fps float64 }
	var reports []report
	tail := fc.trackProgress(strings.NewReader(sampleFFmpegStderr), 10, func(progress, fps float64) {
		reports = append(reports, report{progress, fps})
	}, slog.Default())

	if len(reports) != 2 {
		t.Fatalf("expected 2 progress reports (one per block), got %d: %+v", len(reports), reports)
	}
	if reports[0].progress != 20 || reports[0].fps != 25 {
		t.Errorf("unexpected first report: %+v", reports[0])
	}
	if reports[1].progress != 100 || reports[1].fps != 30.5 {
		t.Errorf("unexpected final report: %+v", reports[1])
	}

	joined := strings.Join(tail, "\n")
	if !strings.Contains(joined, "Conversion failed!") {
		t.Errorf("stderr tail should keep FFmpeg's log lines, got %q", joined)
	}
	if strings.Contains(joined, "out_time_ms") || strings.Contains(joined, "progress=") {
		t.Errorf("stderr tail should not contain progress key/value lines: %q", joined)
	}
}

func TestTrackProgressWithoutCallbackStillCollectsTail(t *testing.T) {
	fc := NewFFmpegConverter("ffmpeg", nil, 0)
	tail := fc.trackProgress(strings.NewReader(sampleFFmpegStderr), 0, nil, nil)
	if len(tail) == 0 {
		t.Fatal("expected log lines to be collected without a callback")
	}
}

func TestValidateOutputAcceptsSmallButRealFiles(t *testing.T) {
	fc := NewFFmpegConverter("ffmpeg", nil, 0)
	dir := t.TempDir()

	small := dir + "/small.mp4"
	writeFile(t, small, 200*1024) // a short clip: far below the old 1MB floor
	if err := fc.ValidateOutput(small); err != nil {
		t.Errorf("a 200KB output should be valid: %v", err)
	}

	empty := dir + "/empty.mp4"
	writeFile(t, empty, 0)
	if err := fc.ValidateOutput(empty); err == nil {
		t.Error("an empty output must be rejected")
	}
}
