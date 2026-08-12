package converter

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/darkace1998/video-converter-common/constants"
	"github.com/darkace1998/video-converter-common/models"
)

// A conversion must not outlive the worker that started it. It previously ran
// from context.Background(), so killing a worker left FFmpeg running, reparented
// to init, still holding its GPU memory — and the worker's own shutdown blocked
// waiting for a process nobody was going to stop.
func TestConversionStopsWhenWorkerContextIsCancelled(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		if _, statErr := os.Stat("/opt/ffmpeg9/bin/ffmpeg"); statErr != nil {
			t.Skip("ffmpeg not available")
		}
		ffmpegPath = "/opt/ffmpeg9/bin/ffmpeg"
	}

	dir := t.TempDir()
	source := filepath.Join(dir, "src.mp4")

	// A long clip, so the conversion is still running when we cancel.
	build := exec.Command(ffmpegPath, "-v", "error",
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=25:duration=120",
		"-c:v", encoderLibx264, "-preset", "ultrafast", "-y", source)
	if out, buildErr := build.CombinedOutput(); buildErr != nil {
		t.Fatalf("failed to render source: %v: %s", buildErr, out)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fc := NewFFmpegConverter(ffmpegPath, nil, time.Hour)
	fc.SetContext(ctx)

	job := &models.Job{SourcePath: source, OutputPath: filepath.Join(dir, "out.mp4")}
	cfg := &models.ConversionConfig{
		TargetResolution: "320x240",
		Codec:            constants.CodecH264,
		Bitrate:          "1M",
		Preset:           "ultrafast",
		AudioCodec:       constants.AudioCodecAAC,
		AudioBitrate:     "64k",
		OutputFormat:     muxerMP4,
	}

	done := make(chan error, 1)
	go func() { done <- fc.ConvertVideoWithProgressLogger(job, cfg, nil, nil) }()

	// Let it get going, then cancel as a shutting-down worker would.
	time.Sleep(1500 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Returned promptly: FFmpeg was stopped rather than left running.
	case <-time.After(20 * time.Second):
		t.Fatal("conversion did not stop after its context was cancelled; " +
			"FFmpeg would be left orphaned and shutdown would hang")
	}
}

// With no context wired the converter must still work, so existing callers and
// the capability probe are unaffected.
func TestConversionWithoutContextStillRuns(t *testing.T) {
	fc := NewFFmpegConverter("ffmpeg", nil, time.Hour)

	if fc.parentContext() == nil {
		t.Fatal("parentContext() returned nil")
	}
	if fc.parentContext().Err() != nil {
		t.Error("parentContext() returned an already-cancelled context")
	}
}

func TestSetContextIsUsedAsParent(t *testing.T) {
	fc := NewFFmpegConverter("ffmpeg", nil, time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	fc.SetContext(ctx)

	cancel()

	if fc.parentContext().Err() == nil {
		t.Error("cancelling the worker context did not affect the converter's parent context")
	}
}
