package converter

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// ffmpegForTest returns a converter bound to a real ffmpeg, or skips.
func ffmpegForTest(t *testing.T) *FFmpegConverter {
	t.Helper()

	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		if _, statErr := os.Stat("/opt/ffmpeg9/bin/ffmpeg"); statErr == nil {
			path = "/opt/ffmpeg9/bin/ffmpeg"
		} else {
			t.Skip("ffmpeg not available")
		}
	}
	return NewFFmpegConverter(path, nil, 0)
}

// renderClip produces a short test clip and returns its path.
func renderClip(t *testing.T, fc *FFmpegConverter, dir, name, duration, size string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	// #nosec G204 - test-controlled arguments
	cmd := exec.Command(fc.ffmpegPath,
		"-v", "error",
		"-f", "lavfi", "-i", "testsrc=size="+size+":rate=10:duration="+duration,
		"-c:v", encoderLibx264, "-preset", "ultrafast",
		"-y", path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to render test clip: %v: %s", err, out)
	}
	return path
}

// The bug this replaces: a valid but small output was rejected outright, which
// permanently failed the job. Short clips, trailers and low-bitrate content all
// land well under any size threshold worth setting.
func TestValidateOutputAcceptsSmallButValidFile(t *testing.T) {
	fc := ffmpegForTest(t)
	dir := t.TempDir()

	// A couple of seconds at a small size lands far below the old 1 MB floor.
	clip := renderClip(t, fc, dir, "small.mp4", "2", "160x120")

	info, err := os.Stat(clip)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Size() >= 1024*1024 {
		t.Skipf("test clip is %d bytes, too large to exercise the old threshold", info.Size())
	}

	if err := fc.ValidateOutputAgainstSource(clip, 2, nil); err != nil {
		t.Errorf("ValidateOutputAgainstSource() rejected a valid %d byte file: %v", info.Size(), err)
	}
}

func TestValidateOutputRejectsEmptyFile(t *testing.T) {
	fc := ffmpegForTest(t)
	path := filepath.Join(t.TempDir(), "empty.mp4")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := fc.ValidateOutputAgainstSource(path, 0, nil); err == nil {
		t.Error("ValidateOutputAgainstSource() accepted an empty file")
	}
}

func TestValidateOutputRejectsMissingFile(t *testing.T) {
	fc := ffmpegForTest(t)

	if err := fc.ValidateOutputAgainstSource(filepath.Join(t.TempDir(), "nope.mp4"), 0, nil); err == nil {
		t.Error("ValidateOutputAgainstSource() accepted a missing file")
	}
}

// Garbage that happens to be large enough must still be rejected — this is what
// a size-only check could not do.
func TestValidateOutputRejectsNonVideoData(t *testing.T) {
	fc := ffmpegForTest(t)
	path := filepath.Join(t.TempDir(), "junk.mp4")
	if err := os.WriteFile(path, make([]byte, 64*1024), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := fc.ValidateOutputAgainstSource(path, 0, nil); err == nil {
		t.Error("ValidateOutputAgainstSource() accepted a file with no video stream")
	}
}

// A conversion that stopped early is the failure the old size check was really
// aiming at, and a duration comparison detects it regardless of file size.
func TestValidateOutputRejectsTruncatedConversion(t *testing.T) {
	fc := ffmpegForTest(t)
	dir := t.TempDir()

	clip := renderClip(t, fc, dir, "short.mp4", "2", "320x240")

	// Claim the source was much longer than what was produced.
	if err := fc.ValidateOutputAgainstSource(clip, 60, nil); err == nil {
		t.Error("ValidateOutputAgainstSource() accepted a 2s output for a 60s source")
	}
}

// Small timing differences from container rounding must not fail a job.
func TestValidateOutputToleratesMinorDurationDrift(t *testing.T) {
	fc := ffmpegForTest(t)
	dir := t.TempDir()

	clip := renderClip(t, fc, dir, "ten.mp4", "10", "320x240")

	for _, sourceDuration := range []float64{10, 10.4, 9.6} {
		if err := fc.ValidateOutputAgainstSource(clip, sourceDuration, nil); err != nil {
			t.Errorf("ValidateOutputAgainstSource(source=%.1fs) rejected a valid output: %v",
				sourceDuration, err)
		}
	}
}

// With no known source duration the check must still pass a valid file.
func TestValidateOutputWithUnknownSourceDuration(t *testing.T) {
	fc := ffmpegForTest(t)
	dir := t.TempDir()

	clip := renderClip(t, fc, dir, "unknown.mp4", "3", "320x240")

	if err := fc.ValidateOutputAgainstSource(clip, 0, nil); err != nil {
		t.Errorf("ValidateOutputAgainstSource() rejected a valid file: %v", err)
	}
}
