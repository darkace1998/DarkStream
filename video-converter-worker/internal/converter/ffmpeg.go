// Package converter provides video conversion functionality using FFmpeg.
package converter

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
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
	// MinOutputFileSize is a floor used only to catch empty or truncated output.
	// It is deliberately tiny: a real validity check is done with ffprobe, since
	// a legitimately short or low-bitrate clip can be far smaller than any size
	// threshold worth setting.
	MinOutputFileSize = 1024

	// outputDurationTolerance is how far the output duration may differ from the
	// source before the conversion is considered truncated. Container rounding
	// and trailing partial frames account for well under a second.
	outputDurationTolerance = 2 * time.Second

	// outputDurationToleranceRatio allows longer files a proportional margin,
	// which matters for variable-frame-rate sources.
	outputDurationToleranceRatio = 0.02
)

// FFmpeg encoder and muxer names. These are FFmpeg's own identifiers and are
// distinct from the codec/format identifiers in the shared constants package,
// which name the codec rather than the specific FFmpeg implementation.
const (
	encoderH264Vulkan = "h264_vulkan"
	encoderLibx264    = "libx264"
	encoderAAC        = "aac"
	muxerMP4          = "mp4"
)

// Hardware-acceleration backends selectable via ConversionConfig.HWAccel.
// DarkStream deliberately supports only Vulkan and CPU encoding: Vulkan is an
// open, vendor-neutral API, so there is no vendor-specific path to maintain.
const (
	HWAccelNone   = "none"
	HWAccelVulkan = "vulkan"
)

// resolveHWAccel returns the effective backend, honouring the legacy UseVulkan
// flag only when HWAccel is unset.
func resolveHWAccel(cfg *models.ConversionConfig) string {
	switch strings.ToLower(strings.TrimSpace(cfg.HWAccel)) {
	case HWAccelVulkan:
		return HWAccelVulkan
	case HWAccelNone:
		return HWAccelNone
	}
	if cfg.UseVulkan {
		return HWAccelVulkan
	}
	return HWAccelNone
}

var (
	// ffmpegTimePattern matches FFmpeg's out_time_ms field (in microseconds)
	ffmpegTimePattern = regexp.MustCompile(`out_time_ms=(\d+)`)
	// ffmpegFPSPattern matches FFmpeg's fps field
	ffmpegFPSPattern = regexp.MustCompile(`fps=([\d.]+)`)
)

// ProgressCallback is a function type for progress updates during conversion
type ProgressCallback func(progress float64, fps float64)

// LineSink receives FFmpeg's output lines as they are produced, so callers can
// record them against the job. FFmpeg writes both progress data and its
// diagnostics to stderr; the sink only receives the diagnostics, since progress
// lines are high-frequency machine output with no diagnostic value.
type LineSink func(line string)

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
	return fc.ConvertVideoWithProgressLogger(job, cfg, nil, slog.Default())
}

// ConvertVideoWithProgress performs the video conversion with progress tracking
func (fc *FFmpegConverter) ConvertVideoWithProgress(
	job *models.Job,
	cfg *models.ConversionConfig,
	progressCallback ProgressCallback,
) error {
	return fc.ConvertVideoWithProgressLogger(job, cfg, progressCallback, slog.Default())
}

// ConvertVideoWithProgressLogger performs the video conversion with progress tracking using the provided logger.
func (fc *FFmpegConverter) ConvertVideoWithProgressLogger(
	job *models.Job,
	cfg *models.ConversionConfig,
	progressCallback ProgressCallback,
	log *slog.Logger,
) error {
	return fc.ConvertVideoWithSink(job, cfg, progressCallback, log, nil)
}

// ConvertVideoWithSink performs the conversion and additionally streams FFmpeg's
// diagnostic output to sink, so the caller can attach it to the job's log.
func (fc *FFmpegConverter) ConvertVideoWithSink(
	job *models.Job,
	cfg *models.ConversionConfig,
	progressCallback ProgressCallback,
	log *slog.Logger,
	sink LineSink,
) error {
	if log == nil {
		log = slog.Default()
	}

	log.Info("Starting conversion",
		"job_id", job.ID,
		"source", job.SourcePath,
		"output", job.OutputPath,
	)

	// Ensure output directory exists
	outputDir := filepath.Dir(job.OutputPath)
	err := os.MkdirAll(outputDir, 0o750)
	if err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Get video duration for progress calculation
	duration, err := fc.getVideoDuration(job.SourcePath)
	if err != nil {
		log.Warn("Failed to get video duration, progress tracking may be inaccurate", "error", err)
		duration = 0
	}

	// Build FFmpeg command
	args := fc.buildFFmpegCommand(job, cfg, log)

	if sink != nil {
		sink(fc.ffmpegPath + " " + strings.Join(args, " "))
	}

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

	log.Debug("Executing FFmpeg command", "args", args)

	err = cmd.Start()
	if err != nil {
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	// Always consume stderr so FFmpeg never blocks when the pipe buffer fills.
	// All reads MUST complete before cmd.Wait() is called, otherwise Wait may
	// close the pipe while the reader is still running (see os/exec docs).
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		if progressCallback != nil && duration > 0 {
			fc.trackProgress(stderr, duration, progressCallback, log, sink)
		} else {
			fc.drainStderr(stderr, sink)
		}
	}()
	<-readDone

	err = cmd.Wait()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("ffmpeg conversion timed out after %v: %w", fc.timeout, err)
		}
		return fmt.Errorf("ffmpeg conversion failed: %w", err)
	}

	log.Info("Conversion completed", "job_id", job.ID)
	return nil
}

// drainStderr consumes FFmpeg's output when progress tracking is not in use,
// forwarding diagnostics to the sink. The pipe must always be drained or FFmpeg
// blocks once the buffer fills.
func (fc *FFmpegConverter) drainStderr(stderr io.ReadCloser, sink LineSink) {
	if sink == nil {
		_, _ = io.Copy(io.Discard, stderr)
		return
	}

	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		if line := scanner.Text(); !isProgressLine(line) {
			sink(line)
		}
	}
}

// progressFields are the keys FFmpeg emits for "-progress"; they are machine
// output repeated many times per second and would drown the job log.
var progressFields = []string{
	"frame=", "fps=", "stream_", "bitrate=", "total_size=", "out_time",
	"dup_frames=", "drop_frames=", "speed=", "progress=",
}

// isProgressLine reports whether a line is FFmpeg progress data rather than a
// diagnostic worth logging.
func isProgressLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	for _, field := range progressFields {
		if strings.HasPrefix(trimmed, field) {
			return true
		}
	}
	return false
}

// ValidateOutput validates the converted output file
func (fc *FFmpegConverter) ValidateOutput(outputPath string) error {
	return fc.ValidateOutputWithLogger(outputPath, slog.Default())
}

// ValidateOutputWithLogger validates the converted output file using the provided logger.
func (fc *FFmpegConverter) ValidateOutputWithLogger(outputPath string, log *slog.Logger) error {
	return fc.ValidateOutputAgainstSource(outputPath, 0, log)
}

// ValidateOutputAgainstSource checks that a conversion actually produced usable
// video. sourceDuration is the source length in seconds; pass 0 when it is
// unknown, in which case the duration comparison is skipped.
//
// This deliberately does not judge the output by file size. A size threshold
// rejects legitimately small results — short clips, trailers, quiet or
// low-bitrate content — and rejecting them is permanent, because the job fails
// rather than being retried. Instead the output is probed: it must be readable,
// contain a video stream, and (when the source length is known) be about as long
// as the source, which is what actually distinguishes a good conversion from a
// truncated one.
func (fc *FFmpegConverter) ValidateOutputAgainstSource(
	outputPath string,
	sourceDuration float64,
	log *slog.Logger,
) error {
	if log == nil {
		log = slog.Default()
	}

	info, err := os.Stat(outputPath)
	if err != nil {
		return fmt.Errorf("output file not found: %w", err)
	}

	// Catch the degenerate cases (empty or a bare container header) before
	// spending an ffprobe call on them.
	if info.Size() < MinOutputFileSize {
		return fmt.Errorf("output file is empty or truncated: %d bytes", info.Size())
	}

	probe, err := fc.probeOutput(outputPath)
	if err != nil {
		return fmt.Errorf("output file is not readable: %w", err)
	}
	if !probe.hasVideo {
		return fmt.Errorf("output file contains no video stream")
	}
	if probe.duration <= 0 {
		return fmt.Errorf("output file has no playable duration")
	}

	if sourceDuration > 0 {
		tolerance := outputDurationTolerance.Seconds()
		if scaled := sourceDuration * outputDurationToleranceRatio; scaled > tolerance {
			tolerance = scaled
		}
		if diff := math.Abs(sourceDuration - probe.duration); diff > tolerance {
			return fmt.Errorf(
				"output duration %.2fs does not match source %.2fs (differs by %.2fs, tolerance %.2fs)",
				probe.duration, sourceDuration, diff, tolerance)
		}
	}

	log.Info("Output validated",
		"path", outputPath,
		"size", info.Size(),
		"duration", probe.duration,
		"codec", probe.videoCodec)
	return nil
}

// outputProbe is the subset of ffprobe output used for validation.
type outputProbe struct {
	hasVideo   bool
	videoCodec string
	duration   float64
}

// probeOutput inspects a converted file with ffprobe.
func (fc *FFmpegConverter) probeOutput(path string) (*outputProbe, error) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	// #nosec G204 - ffprobe path is derived from the operator-configured ffmpeg
	cmd := exec.CommandContext(ctx, fc.ffprobePath(),
		"-v", "error",
		"-show_entries", "format=duration:stream=codec_type,codec_name",
		"-of", "json",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed: %w", err)
	}

	var parsed struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
			CodecName string `json:"codec_name"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("could not parse ffprobe output: %w", err)
	}

	result := &outputProbe{}
	for _, st := range parsed.Streams {
		if st.CodecType == "video" {
			result.hasVideo = true
			result.videoCodec = st.CodecName
			break
		}
	}
	if parsed.Format.Duration != "" {
		result.duration, _ = strconv.ParseFloat(parsed.Format.Duration, 64)
	}

	return result, nil
}

// ffprobePath derives the ffprobe binary sitting alongside the configured
// ffmpeg, so a custom build is probed with its own matching tool.
func (fc *FFmpegConverter) ffprobePath() string {
	dir := filepath.Dir(fc.ffmpegPath)
	base := strings.Replace(filepath.Base(fc.ffmpegPath), "ffmpeg", "ffprobe", 1)
	return filepath.Join(dir, base)
}

// buildFFmpegCommand constructs the FFmpeg command arguments
func (fc *FFmpegConverter) buildFFmpegCommand(
	job *models.Job,
	cfg *models.ConversionConfig,
	log *slog.Logger,
) []string {
	// Hardware-acceleration flags are per-input options and MUST precede the
	// "-i" they apply to, otherwise ffmpeg rejects them as output options.
	args := []string{"-progress", "pipe:2"} // Output progress to stderr

	backend := resolveHWAccel(cfg)
	switch backend {
	case HWAccelVulkan:
		// Frame-threaded software decoding deadlocks against the Vulkan upload
		// path: with an HEVC source, ffmpeg hangs within seconds and stops
		// responding even to SIGTERM, with every thread parked in futex_do_wait
		// and no GPU activity. Slice threading avoids the deadlock while keeping
		// decoding multithreaded (single-threaded decode also works but is much
		// slower). This must precede -i to apply to the input decoder.
		args = append(args, "-thread_type", "slice")

		// The Vulkan encoder takes frames from a Vulkan device, and the hwupload
		// filter added below needs that device referenced explicitly — with only
		// "-hwaccel vulkan" it fails with "A hardware device reference is
		// required to upload frames to". Decoding deliberately stays in software
		// so the scale filter still operates on ordinary frames and only the
		// final upload crosses to the GPU.
		args = append(args,
			"-init_hw_device", "vulkan=vk:0",
			"-filter_hw_device", "vk",
		)
	}

	args = append(args, "-i", job.SourcePath)

	// Video filtering and encoding. The Vulkan encoder consumes frames in a
	// Vulkan hardware format, so scaling in software requires uploading the
	// result back to the device before it reaches the encoder.
	videoFilter := fmt.Sprintf("scale=%s", cfg.TargetResolution)
	if backend == HWAccelVulkan {
		videoFilter += ",format=nv12,hwupload"
	}
	args = append(args, "-vf", videoFilter)

	// Video codec selection for the chosen backend
	videoCodec := fc.getVideoCodecForBackend(cfg.Codec, backend, log)
	args = append(args, "-c:v", videoCodec)

	// Add preset and bitrate for video
	args = append(args,
		"-preset", fc.getPresetForBackend(cfg.Preset, backend, log),
		"-b:v", cfg.Bitrate,
	)

	// Audio codec selection
	audioCodec := fc.getAudioCodecWithLogger(cfg.AudioCodec, log)
	args = append(args,
		"-c:a", audioCodec,
		"-b:a", cfg.AudioBitrate,
	)

	// Add output format if specified
	if cfg.OutputFormat != "" {
		args = append(args, "-f", fc.getOutputFormatWithLogger(cfg.OutputFormat, log))
	}

	// Output file with format
	args = append(args, "-y", job.OutputPath)

	return args
}

// getVideoCodec returns the appropriate video codec based on configuration
func (fc *FFmpegConverter) getVideoCodec(codec string, useVulkan bool) string {
	return fc.getVideoCodecWithLogger(codec, useVulkan, slog.Default())
}

func (fc *FFmpegConverter) getVideoCodecWithLogger(codec string, useVulkan bool, log *slog.Logger) string {
	backend := HWAccelNone
	if useVulkan {
		backend = HWAccelVulkan
	}
	return fc.getVideoCodecForBackend(codec, backend, log)
}

// getPresetForBackend returns the preset to pass to the encoder. libx264 and the
// Vulkan encoder both accept the configured names as-is.
func (fc *FFmpegConverter) getPresetForBackend(preset, _ string, _ *slog.Logger) string {
	return preset
}

func (fc *FFmpegConverter) getVideoCodecForBackend(codec, backend string, log *slog.Logger) string {
	if log == nil {
		log = slog.Default()
	}

	switch backend {
	case HWAccelVulkan:
		switch codec {
		case constants.CodecH264, "avc":
			return encoderH264Vulkan
		case constants.CodecH265, "hevc":
			return "hevc_vulkan"
		default:
			// Fallback to h264_vulkan for unknown codecs
			log.Warn("Unknown codec for Vulkan encoding, falling back to h264_vulkan", "codec", codec)
			return encoderH264Vulkan
		}
	}

	// CPU encoding fallback
	switch codec {
	case constants.CodecH264, "avc":
		return encoderLibx264
	case constants.CodecH265, "hevc":
		return "libx265"
	case constants.CodecVP9:
		return "libvpx-vp9"
	case constants.CodecAV1:
		return "libaom-av1"
	default:
		// Fallback to libx264
		log.Warn("Unknown codec, falling back to libx264", "codec", codec)
		return encoderLibx264
	}
}

// getAudioCodec returns the appropriate audio codec
func (fc *FFmpegConverter) getAudioCodec(codec string) string {
	return fc.getAudioCodecWithLogger(codec, slog.Default())
}

func (fc *FFmpegConverter) getAudioCodecWithLogger(codec string, log *slog.Logger) string {
	if log == nil {
		log = slog.Default()
	}

	switch codec {
	case constants.AudioCodecAAC:
		return encoderAAC
	case constants.AudioCodecMP3:
		return "libmp3lame"
	case constants.AudioCodecOPUS:
		return "libopus"
	case constants.AudioCodecVorbis:
		return "libvorbis"
	default:
		// Fallback to aac
		log.Warn("Unknown audio codec, falling back to aac", "codec", codec)
		return encoderAAC
	}
}

// getOutputFormat returns the appropriate output format
func (fc *FFmpegConverter) getOutputFormat(format string) string {
	return fc.getOutputFormatWithLogger(format, slog.Default())
}

func (fc *FFmpegConverter) getOutputFormatWithLogger(format string, log *slog.Logger) string {
	if log == nil {
		log = slog.Default()
	}

	switch format {
	case constants.FormatMP4:
		return muxerMP4
	case constants.FormatMKV:
		return "matroska"
	case constants.FormatWebM:
		return "webm"
	case constants.FormatAVI:
		return "avi"
	default:
		// Fallback to mp4
		log.Warn("Unknown output format, falling back to mp4", "format", format)
		return muxerMP4
	}
}

// getVideoDuration extracts the duration of a video file using ffprobe
func (fc *FFmpegConverter) getVideoDuration(sourcePath string) (float64, error) {
	// Determine ffprobe path from ffmpeg path by rewriting only the final path
	// component, so a parent directory containing "ffmpeg" is not corrupted
	// (e.g. /opt/ffmpeg/bin/ffmpeg -> /opt/ffmpeg/bin/ffprobe).
	dir := filepath.Dir(fc.ffmpegPath)
	base := strings.Replace(filepath.Base(fc.ffmpegPath), "ffmpeg", "ffprobe", 1)
	ffprobePath := filepath.Join(dir, base)

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
	log *slog.Logger,
	sink LineSink,
) {
	if log == nil {
		log = slog.Default()
	}

	scanner := bufio.NewScanner(stderr)

	var currentTime float64
	var fps float64

	for scanner.Scan() {
		line := scanner.Text()

		if sink != nil && !isProgressLine(line) {
			sink(line)
		}

		// Extract current time from FFmpeg's out_time_ms field (in microseconds)
		if matches := ffmpegTimePattern.FindStringSubmatch(line); len(matches) > 1 {
			timeUs, err := strconv.ParseInt(matches[1], 10, 64)
			if err == nil {
				currentTime = float64(timeUs) / 1000000.0 // Convert microseconds to seconds
			}
		}

		// Extract FPS
		if matches := ffmpegFPSPattern.FindStringSubmatch(line); len(matches) > 1 {
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

			log.Debug("Conversion progress",
				"progress", fmt.Sprintf("%.1f%%", progress),
				"fps", fmt.Sprintf("%.1f", fps),
			)
		}
	}
}
