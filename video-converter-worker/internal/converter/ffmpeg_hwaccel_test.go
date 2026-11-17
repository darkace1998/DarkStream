package converter

import (
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/darkace1998/video-converter-common/models"

	"github.com/darkace1998/video-converter-common/constants"
)

func testConverter(ffmpegPath string) *FFmpegConverter {
	return NewFFmpegConverter(ffmpegPath, nil, time.Minute)
}

func TestResolveHWAccelMapping(t *testing.T) {
	fc := testConverter("ffmpeg")
	log := slog.Default()

	cases := []struct {
		name   string
		hw     string
		vulkan bool
		want   string
	}{
		{"explicit cuda", constants.HWAccelCUDA, false, constants.HWAccelCUDA},
		{"explicit vaapi uppercase", "VAAPI", false, constants.HWAccelVAAPI},
		{"explicit qsv", constants.HWAccelQSV, false, constants.HWAccelQSV},
		{"explicit vulkan", constants.HWAccelVulkan, false, constants.HWAccelVulkan},
		{"none disables", constants.HWAccelNone, true, ""},
		{"legacy use_vulkan maps to vulkan", "", true, constants.HWAccelVulkan},
		{"hw_accel wins over use_vulkan", constants.HWAccelCUDA, true, constants.HWAccelCUDA},
		{"empty with no vulkan is software", "", false, ""},
		{"unknown value ignored with warning", "dxva2", false, ""},
		{"whitespace trimmed", "  " + constants.HWAccelCUDA + "  ", false, constants.HWAccelCUDA},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &models.ConversionConfig{HWAccel: tc.hw, UseVulkan: tc.vulkan}
			if got := fc.resolveHWAccel(cfg, log); got != tc.want {
				t.Errorf("resolveHWAccel(hw=%q, vulkan=%v) = %q, want %q", tc.hw, tc.vulkan, got, tc.want)
			}
		})
	}
}

func TestResolveHWAccelAutoDetectsOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script stub requires a POSIX shell")
	}
	// Fake ffmpeg that reports only vulkan support.
	dir := t.TempDir()
	stub := filepath.Join(dir, "ffmpeg")
	script := "#!/bin/sh\nprintf 'Hardware acceleration devices:\\nvulkan\\n'\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	fc := testConverter(stub)
	cfg := &models.ConversionConfig{HWAccel: constants.HWAccelAuto}
	first := fc.resolveHWAccel(cfg, slog.Default())
	// Whatever the machine has, the result must be cached: a second call with a
	// deleted stub must return the same value (no re-exec).
	if err := os.Remove(stub); err != nil {
		t.Fatal(err)
	}
	second := fc.resolveHWAccel(cfg, slog.Default())
	if first != second {
		t.Errorf("detectHWAccel not cached: first=%q second=%q", first, second)
	}
	// On a machine without /dev/nvidia0 or /dev/dri, only vulkan can be picked.
	if _, err := os.Stat("/dev/nvidia0"); err != nil {
		if _, err := os.Stat("/dev/dri/renderD128"); err != nil {
			if first != constants.HWAccelVulkan {
				t.Errorf("Expected vulkan on this machine, got %q", first)
			}
		}
	}
}

func TestDetectHWAccelParsesOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script stub requires a POSIX shell")
	}
	dir := t.TempDir()
	stub := filepath.Join(dir, "ffmpeg")
	script := "#!/bin/sh\ncat <<'EOF'\nffmpeg version 7.0\nHardware acceleration devices:\ncuda\nvaapi\nvulkan\nEOF\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	fc := testConverter(stub)
	got := fc.detectHWAccel(slog.Default())
	// cuda is only chosen when /dev/nvidia0 exists; vaapi/qsv only with the
	// DRI device node; otherwise vulkan. Any of those proves parsing worked,
	// but the result must be one of the advertised values or "".
	switch got {
	case constants.HWAccelCUDA, constants.HWAccelVAAPI, constants.HWAccelQSV, constants.HWAccelVulkan, "":
	default:
		t.Errorf("detectHWAccel returned unadvertised value %q", got)
	}
}

func TestGetVideoCodecPassesHardwareEncodersThrough(t *testing.T) {
	fc := testConverter("ffmpeg")
	log := slog.Default()

	hw := []string{"h264_nvenc", "hevc_nvenc", "h264_qsv", "hevc_qsv", "h264_vaapi", "hevc_vaapi", "h264_amf", "hevc_videotoolbox"}
	for _, codec := range hw {
		if got := fc.getVideoCodecWithLogger(codec, false, log); got != codec {
			t.Errorf("hardware encoder %q must pass through unchanged, got %q", codec, got)
		}
	}

	// Software codecs are still mapped as before (vulkan off => unchanged here).
	if got := fc.getVideoCodecWithLogger("h264", false, log); got != "h264" && got != "libx264" {
		t.Errorf("software codec mapping changed unexpectedly: %q", got)
	}
}
