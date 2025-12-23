// Package converter provides video conversion functionality using FFmpeg.
package converter

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/darkace1998/video-converter-common/constants"
	"github.com/darkace1998/video-converter-common/models"
)

const (
	// MinOutputFileSize is the minimum acceptable size for output files (1MB)
	MinOutputFileSize = 1024 * 1024
)

// ProgressCallback is a function type for progress updates during conversion
type ProgressCallback func(progress float64, fps float64)

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
	return fc.ConvertVideoWithProgress(job, cfg, nil)
}

// ConvertVideoWithProgress performs the video conversion with progress tracking
func (fc *FFmpegConverter) ConvertVideoWithProgress(
	job *models.Job,
	cfg *models.ConversionConfig,
	progressCallback ProgressCallback,
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

	// Get video duration for progress calculation
	duration, err := fc.getVideoDuration(job.SourcePath)
	if err != nil {
		slog.Warn("Failed to get video duration, progress tracking may be inaccurate", "error", err)
		duration = 0
	}

	// Build FFmpeg command
	args := fc.buildFFmpegCommand(job, cfg)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), fc.timeout)
	defer cancel()

	// #nosec G204 - fc.ffmpegPath is validated/controlled, not user input from network
	cmd := exec.CommandContext(ctx, fc.ffmpegPath, args...)
	
	// Setup stderr pipe for progress tracking
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	slog.Debug("Executing FFmpeg command", "args", args)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	// Track progress if callback provided
	if progressCallback != nil && duration > 0 {
		go fc.trackProgress(stderr, duration, progressCallback)
	}

	if err := cmd.Wait(); err != nil {
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
		"-progress", "pipe:2", // Output progress to stderr
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

	// Video codec selection with Vulkan support
	videoCodec := fc.getVideoCodec(cfg.Codec, cfg.UseVulkan)
	args = append(args, "-c:v", videoCodec)
	
	// Add preset and bitrate for video
	args = append(args,
		"-preset", cfg.Preset,
		"-b:v", cfg.Bitrate,
	)

	// Audio codec selection
	audioCodec := fc.getAudioCodec(cfg.AudioCodec)
	args = append(args,
		"-c:a", audioCodec,
		"-b:a", cfg.AudioBitrate,
	)

	// Add output format if specified
	if cfg.OutputFormat != "" {
		args = append(args, "-f", fc.getOutputFormat(cfg.OutputFormat))
	}

	// Output file with format
	args = append(args, "-y", job.OutputPath)

	return args
}

// getVideoCodec returns the appropriate video codec based on configuration
func (fc *FFmpegConverter) getVideoCodec(codec string, useVulkan bool) string {
	if useVulkan {
		switch codec {
		case constants.CodecH264, "avc":
			return "h264_vulkan"
		case constants.CodecH265, "hevc":
			return "hevc_vulkan"
		default:
			// Fallback to h264_vulkan for unknown codecs
			slog.Warn("Unknown codec for Vulkan encoding, falling back to h264_vulkan", "codec", codec)
			return "h264_vulkan"
		}
	}

	// CPU encoding fallback
	switch codec {
	case constants.CodecH264, "avc":
		return "libx264"
	case constants.CodecH265, "hevc":
		return "libx265"
	case constants.CodecVP9:
		return "libvpx-vp9"
	case constants.CodecAV1:
		return "libaom-av1"
	default:
		// Fallback to libx264
		slog.Warn("Unknown codec, falling back to libx264", "codec", codec)
		return "libx264"
	}
}

// getAudioCodec returns the appropriate audio codec
func (fc *FFmpegConverter) getAudioCodec(codec string) string {
	switch codec {
	case constants.AudioCodecAAC:
		return "aac"
	case constants.AudioCodecMP3:
		return "libmp3lame"
	case constants.AudioCodecOPUS:
		return "libopus"
	case constants.AudioCodecVorbis:
		return "libvorbis"
	default:
		// Fallback to aac
		slog.Warn("Unknown audio codec, falling back to aac", "codec", codec)
		return "aac"
	}
}

// getOutputFormat returns the appropriate output format
func (fc *FFmpegConverter) getOutputFormat(format string) string {
	switch format {
	case constants.FormatMP4:
		return "mp4"
	case constants.FormatMKV:
		return "matroska"
	case constants.FormatWebM:
		return "webm"
	case constants.FormatAVI:
		return "avi"
	default:
		// Fallback to mp4
		slog.Warn("Unknown output format, falling back to mp4", "format", format)
		return "mp4"
	}
}

// getVideoDuration extracts the duration of a video file using ffprobe
func (fc *FFmpegConverter) getVideoDuration(sourcePath string) (float64, error) {
	// Determine ffprobe path from ffmpeg path
	ffprobePath := strings.Replace(fc.ffmpegPath, "ffmpeg", "ffprobe", 1)
	
	// #nosec G204 - ffprobePath is derived from validated fc.ffmpegPath
	cmd := exec.Command(ffprobePath,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		sourcePath,
	)

	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("failed to get duration: %w", err)
	}

	durationStr := strings.TrimSpace(string(output))
	duration, err := strconv.ParseFloat(durationStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse duration: %w", err)
	}

	return duration, nil
}

// trackProgress monitors FFmpeg's progress output
func (fc *FFmpegConverter) trackProgress(
	stderr io.ReadCloser,
	totalDuration float64,
	callback ProgressCallback,
) {
	scanner := bufio.NewScanner(stderr)
	
	// Regex patterns for extracting progress information
	timePattern := regexp.MustCompile(`out_time_ms=(\d+)`)
	fpsPattern := regexp.MustCompile(`fps=([\d.]+)`)

	var currentTime float64
	var fps float64

	for scanner.Scan() {
		line := scanner.Text()

		// Extract current time (out_time_ms is in microseconds)
		if matches := timePattern.FindStringSubmatch(line); len(matches) > 1 {
			timeUs, err := strconv.ParseInt(matches[1], 10, 64)
			if err == nil {
				currentTime = float64(timeUs) / 1000000.0 // Convert microseconds to seconds
			}
		}

		// Extract FPS
		if matches := fpsPattern.FindStringSubmatch(line); len(matches) > 1 {
			parsedFPS, err := strconv.ParseFloat(matches[1], 64)
			if err == nil {
				fps = parsedFPS
			}
		}

		// Calculate and report progress
		if totalDuration > 0 && currentTime > 0 {
			progress := (currentTime / totalDuration) * 100.0
			if progress > 100 {
				progress = 100
			}
			callback(progress, fps)
			
			slog.Debug("Conversion progress",
				"progress", fmt.Sprintf("%.1f%%", progress),
				"fps", fmt.Sprintf("%.1f", fps),
			)
		}
	}
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
