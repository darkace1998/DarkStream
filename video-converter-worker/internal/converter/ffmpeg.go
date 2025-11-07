package converter

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/darkace1998/video-converter-common/models"
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
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Build FFmpeg command
	args := fc.buildFFmpegCommand(job, cfg)

	cmd := exec.Command(fc.ffmpegPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	slog.Debug("Executing FFmpeg command", "args", args)

	if err := cmd.Run(); err != nil {
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
		args = append(args,
			"-c:v", "h264_vulkan", // Vulkan codec
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
	)

	// Output file (overwrite if exists)
	args = append(args, "-y", job.OutputPath)

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
	if info.Size() < 1024*1024 {
		return fmt.Errorf("output file too small: %d bytes", info.Size())
	}

	slog.Info("Output validated", "path", outputPath, "size", info.Size())
	return nil
}
