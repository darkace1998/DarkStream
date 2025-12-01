// Package converter provides video conversion functionality using FFmpeg.
package converter

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/darkace1998/video-converter-common/models"
)

const (
	// MinOutputFileSize is the minimum acceptable size for output files (1MB)
	MinOutputFileSize = 1024 * 1024
)

// FFmpegConverter handles video conversion using FFmpeg
type FFmpegConverter struct {
	ffmpegPath     string
	vulkanDetector *VulkanDetector
	timeout        time.Duration
}

// NewFFmpegConverter creates a new FFmpegConverter instance
func NewFFmpegConverter(
	ffmpegPath string,
	vulkanDetector *VulkanDetector,
	timeout time.Duration,
) *FFmpegConverter {
	return &FFmpegConverter{
		ffmpegPath:     ffmpegPath,
		vulkanDetector: vulkanDetector,
		timeout:        timeout,
	}
}

// ConvertVideo performs the video conversion using FFmpeg
func (fc *FFmpegConverter) ConvertVideo(
	job *models.Job,
	cfg *models.ConversionConfig,
) error {

	slog.Info("Starting conversion",
		"job_id", job.ID,
		"source", job.SourcePath,
		"output", job.OutputPath,
	)

	// Ensure output directory exists
	outputDir := filepath.Dir(job.OutputPath)
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Build FFmpeg command
	args := fc.buildFFmpegCommand(job, cfg)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), fc.timeout)
	defer cancel()

	// #nosec G204 - fc.ffmpegPath is validated/controlled, not user input from network
	cmd := exec.CommandContext(ctx, fc.ffmpegPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	slog.Debug("Executing FFmpeg command", "args", args)

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("ffmpeg conversion timed out after %v: %w", fc.timeout, err)
		}
		return fmt.Errorf("ffmpeg conversion failed: %w", err)
	}

	slog.Info("Conversion completed", "job_id", job.ID)
	return nil
}

// buildFFmpegCommand constructs the FFmpeg command arguments
func (fc *FFmpegConverter) buildFFmpegCommand(
	job *models.Job,
	cfg *models.ConversionConfig,
) []string {

	args := []string{
		"-i", job.SourcePath,
	}

	if cfg.UseVulkan {
		// Use Vulkan for hardware decoding
		args = append(args,
			"-hwaccel", "vulkan",
			"-hwaccel_device", "0", // Device index
		)
	}

	// Video filtering and encoding
	args = append(args,
		"-vf", fmt.Sprintf("scale=%s", cfg.TargetResolution),
	)

	// Use Vulkan for encoding
	if cfg.UseVulkan {
		var vulkanCodec string
		switch cfg.Codec {
		case "h264", "avc":
			vulkanCodec = "h264_vulkan"
		case "hevc", "h265":
			vulkanCodec = "hevc_vulkan"
		default:
			// Fallback to h264_vulkan if unknown
			slog.Warn("Unknown codec for Vulkan encoding, falling back to h264_vulkan", "codec", cfg.Codec)
			vulkanCodec = "h264_vulkan"
		}
		args = append(args,
			"-c:v", vulkanCodec,
			"-preset", cfg.Preset,
			"-b:v", cfg.Bitrate,
		)
	} else {
		// Fallback to libx264
		args = append(args,
			"-c:v", "libx264",
			"-preset", cfg.Preset,
			"-b:v", cfg.Bitrate,
		)
	}

	// Audio encoding
	args = append(args,
		"-c:a", cfg.AudioCodec,
		"-b:a", cfg.AudioBitrate,
		"-y", job.OutputPath,
	)

	return args
}

// ValidateOutput validates the converted output file
func (fc *FFmpegConverter) ValidateOutput(outputPath string) error {
	// Check if file exists
	info, err := os.Stat(outputPath)
	if err != nil {
		return fmt.Errorf("output file not found: %w", err)
	}

	// Check minimum file size (1MB)
	if info.Size() < MinOutputFileSize {
		return fmt.Errorf("output file too small: %d bytes", info.Size())
	}

	slog.Info("Output validated", "path", outputPath, "size", info.Size())
	return nil
}
