package converter

import (
	"log/slog"
	"testing"

	"github.com/darkace1998/video-converter-common/constants"
	"github.com/darkace1998/video-converter-common/models"
)

// TestBuildFFmpegCommandHwaccelOrder ensures Vulkan hardware-acceleration flags
// are emitted as input options (before "-i <source>"). ffmpeg rejects them when
// they appear after the input, so this ordering is load-bearing.
func TestBuildFFmpegCommandHwaccelOrder(t *testing.T) {
	fc := &FFmpegConverter{ffmpegPath: "/usr/bin/ffmpeg"}
	job := &models.Job{SourcePath: "/in.mkv", OutputPath: "/out.mp4"}
	cfg := &models.ConversionConfig{
		UseVulkan:        true,
		TargetResolution: "1920:1080",
		Codec:            constants.CodecH264,
		Preset:           "fast",
		Bitrate:          "5M",
		AudioCodec:       constants.AudioCodecAAC,
		AudioBitrate:     "128k",
	}

	args := fc.buildFFmpegCommand(job, cfg, slog.Default())

	hwaccelIdx, inputIdx := -1, -1
	for i, a := range args {
		if a == "-hwaccel" && hwaccelIdx == -1 {
			hwaccelIdx = i
		}
		if a == "-i" && inputIdx == -1 {
			inputIdx = i
		}
	}

	if hwaccelIdx == -1 {
		t.Fatalf("expected -hwaccel in args, got %v", args)
	}
	if inputIdx == -1 {
		t.Fatalf("expected -i in args, got %v", args)
	}
	if hwaccelIdx > inputIdx {
		t.Errorf("-hwaccel (index %d) must appear before -i (index %d): %v", hwaccelIdx, inputIdx, args)
	}
	if inputIdx+1 >= len(args) || args[inputIdx+1] != job.SourcePath {
		t.Errorf("expected input path %q immediately after -i, got %v", job.SourcePath, args)
	}
}

// TestGetVideoCodec tests video codec selection
func TestGetVideoCodec(t *testing.T) {
	fc := &FFmpegConverter{
		ffmpegPath: "/usr/bin/ffmpeg",
	}

	tests := []struct {
		name      string
		codec     string
		useVulkan bool
		expected  string
	}{
		{
			name:      "h264 with Vulkan",
			codec:     constants.CodecH264,
			useVulkan: true,
			expected:  "h264_vulkan",
		},
		{
			name:      "h265 with Vulkan",
			codec:     constants.CodecH265,
			useVulkan: true,
			expected:  "hevc_vulkan",
		},
		{
			name:      "h264 without Vulkan",
			codec:     constants.CodecH264,
			useVulkan: false,
			expected:  "libx264",
		},
		{
			name:      "h265 without Vulkan",
			codec:     constants.CodecH265,
			useVulkan: false,
			expected:  "libx265",
		},
		{
			name:      "vp9 without Vulkan",
			codec:     constants.CodecVP9,
			useVulkan: false,
			expected:  "libvpx-vp9",
		},
		{
			name:      "av1 without Vulkan",
			codec:     constants.CodecAV1,
			useVulkan: false,
			expected:  "libaom-av1",
		},
		{
			name:      "unknown codec with Vulkan fallback",
			codec:     "unknown",
			useVulkan: true,
			expected:  "h264_vulkan",
		},
		{
			name:      "unknown codec without Vulkan fallback",
			codec:     "unknown",
			useVulkan: false,
			expected:  "libx264",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := fc.getVideoCodec(tt.codec, tt.useVulkan)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

// TestGetAudioCodec tests audio codec selection
func TestGetAudioCodec(t *testing.T) {
	fc := &FFmpegConverter{
		ffmpegPath: "/usr/bin/ffmpeg",
	}

	tests := []struct {
		name     string
		codec    string
		expected string
	}{
		{
			name:     "aac codec",
			codec:    constants.AudioCodecAAC,
			expected: "aac",
		},
		{
			name:     "mp3 codec",
			codec:    constants.AudioCodecMP3,
			expected: "libmp3lame",
		},
		{
			name:     "opus codec",
			codec:    constants.AudioCodecOPUS,
			expected: "libopus",
		},
		{
			name:     "vorbis codec",
			codec:    constants.AudioCodecVorbis,
			expected: "libvorbis",
		},
		{
			name:     "unknown codec fallback",
			codec:    "unknown",
			expected: "aac",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := fc.getAudioCodec(tt.codec)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

// TestGetOutputFormat tests output format selection
func TestGetOutputFormat(t *testing.T) {
	fc := &FFmpegConverter{
		ffmpegPath: "/usr/bin/ffmpeg",
	}

	tests := []struct {
		name     string
		format   string
		expected string
	}{
		{
			name:     "mp4 format",
			format:   constants.FormatMP4,
			expected: "mp4",
		},
		{
			name:     "mkv format",
			format:   constants.FormatMKV,
			expected: "matroska",
		},
		{
			name:     "webm format",
			format:   constants.FormatWebM,
			expected: "webm",
		},
		{
			name:     "avi format",
			format:   constants.FormatAVI,
			expected: "avi",
		},
		{
			name:     "unknown format fallback",
			format:   "unknown",
			expected: "mp4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := fc.getOutputFormat(tt.format)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

// TestNewFFmpegConverter tests the constructor
func TestNewFFmpegConverter(t *testing.T) {
	detector := NewVulkanDetector("auto")
	converter := NewFFmpegConverter("/usr/bin/ffmpeg", detector, 3600)

	if converter == nil {
		t.Fatal("Expected non-nil converter")
	}

	if converter.ffmpegPath != "/usr/bin/ffmpeg" {
		t.Errorf("Expected ffmpegPath /usr/bin/ffmpeg, got %s", converter.ffmpegPath)
	}

	if converter.vulkanDetector == nil {
		t.Error("Expected non-nil vulkanDetector")
	}

	if converter.timeout != 3600 {
		t.Errorf("Expected timeout 3600, got %v", converter.timeout)
	}
}
