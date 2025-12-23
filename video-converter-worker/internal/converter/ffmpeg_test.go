package converter

import (
	"testing"

	"github.com/darkace1998/video-converter-common/constants"
)

// TestGetVideoCodec tests video codec selection
func TestGetVideoCodec(t *testing.T) {
	fc := &FFmpegConverter{
		ffmpegPath: "/usr/bin/ffmpeg",
	}

	tests := []struct {
		name       string
		codec      string
		useVulkan  bool
		expected   string
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
