// Package converter provides video conversion functionality using FFmpeg.
package converter

import (
	"bufio"
	"context"
	"errors"
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

	"sync"
)

const (
	// MinOutputFileSize is the minimum acceptable size for output files. It only
	// guards against empty/header-only output; a higher bar (it used to be 1MB)
	// rejects perfectly valid conversions of short or low-bitrate clips.
	MinOutputFileSize = 1024

	// stderrTailLines is how many trailing FFmpeg log lines are kept to explain a failure.
	stderrTailLines = 12
)

// FFmpeg encoder and muxer names. These are FFmpeg's own identifiers and are
// distinct from the codec/format identifiers in the shared constants package,
// which name the codec rather than the specific FFmpeg implementation.
const (
	encoderH264Vulkan = "h264_vulkan"
	encoderLibx264    = "libx264"
	codecHEVC         = "hevc" // alias accepted alongside constants.CodecH265
	encoderAAC        = "aac"
	muxerMP4          = "mp4"
)

var (
	// ffmpegTimePattern matches FFmpeg's out_time_ms field (in microseconds)
	ffmpegTimePattern = regexp.MustCompile(`out_time_ms=(\d+)`)
	// ffmpegFPSPattern matches FFmpeg's fps field
	ffmpegFPSPattern = regexp.MustCompile(`^fps=([\d.]+)`)
	// ffmpegProgressLinePattern matches the key=value lines of a -progress block,
	// as opposed to FFmpeg's ordinary log output.
	ffmpegProgressLinePattern = regexp.MustCompile(`^(frame|fps|stream_\d+_\d+_\w+|bitrate|total_size|out_time_us|out_time_ms|out_time|dup_frames|drop_frames|speed|progress)=`)
)

// ErrConversionTimeout is returned (wrapped) when FFmpeg exceeds the configured timeout.
var ErrConversionTimeout = errors.New("ffmpeg conversion timed out")

// ProgressCallback is a function type for progress updates during conversion
type ProgressCallback func(progress float64, fps float64)

// FFmpegConverter handles video conversion using FFmpeg
type FFmpegConverter struct {
	ffmpegPath     string
	vulkanDetector *VulkanDetector
	timeout        time.Duration
	ctx            context.Context //nolint:containedctx // optional parent; cancels running conversions on shutdown

	hwAccelMu     sync.Mutex
	hwAccelCached string
	hwAccelDone   bool
}

// SetContext wires a parent context whose cancellation stops in-flight FFmpeg
// processes, so a shutting-down worker does not leave orphaned encoders behind.
func (fc *FFmpegConverter) SetContext(ctx context.Context) {
	fc.ctx = ctx
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
	return fc.ConvertVideoContext(fc.ctx, job, cfg, progressCallback, log)
}

// ConvertVideoContext performs the video conversion and stops FFmpeg as soon as
// ctx is cancelled (for example when the job is cancelled on the master). A nil
// ctx means no external cancellation.
//
//nolint:contextcheck // a nil ctx intentionally falls back to context.Background()
func (fc *FFmpegConverter) ConvertVideoContext(
	ctx context.Context,
	job *models.Job,
	cfg *models.ConversionConfig,
	progressCallback ProgressCallback,
	log *slog.Logger,
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

	// Create context with timeout
	parent := ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, fc.timeout)
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
	var stderrTail []string
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		stderrTail = fc.trackProgress(stderr, duration, progressCallback, log)
		// Keep draining even if line scanning stopped early (e.g. an over-long
		// line), otherwise FFmpeg blocks on a full pipe until the timeout.
		_, _ = io.Copy(io.Discard, stderr)
	}()
	<-readDone

	err = cmd.Wait()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("%w after %v: %w", ErrConversionTimeout, fc.timeout, err)
		}
		if ctx.Err() != nil {
			return fmt.Errorf("ffmpeg conversion cancelled: %w", ctx.Err())
		}
		// "exit status 1" alone says nothing; include FFmpeg's own explanation.
		if len(stderrTail) > 0 {
			return fmt.Errorf("ffmpeg conversion failed: %w: %s", err, strings.Join(stderrTail, " | "))
		}
		return fmt.Errorf("ffmpeg conversion failed: %w", err)
	}

	log.Info("Conversion completed", "job_id", job.ID)
	return nil
}

// ValidateOutput validates the converted output file
func (fc *FFmpegConverter) ValidateOutput(outputPath string) error {
	return fc.ValidateOutputWithLogger(outputPath, slog.Default())
}

// ValidateOutputWithLogger validates the converted output file using the provided logger.
func (fc *FFmpegConverter) ValidateOutputWithLogger(outputPath string, log *slog.Logger) error {
	if log == nil {
		log = slog.Default()
	}

	// Check if file exists
	info, err := os.Stat(outputPath)
	if err != nil {
		return fmt.Errorf("output file not found: %w", err)
	}

	// Check minimum file size (1MB)
	if info.Size() < MinOutputFileSize {
		return fmt.Errorf("output file too small: %d bytes", info.Size())
	}

	log.Info("Output validated", "path", outputPath, "size", info.Size())
	return nil
}

// hwAccelNames are the -hwaccel values this worker understands.
var hwAccelNames = map[string]bool{
	"":                    true,
	constants.HWAccelNone: true, constants.HWAccelAuto: true,
	constants.HWAccelCUDA: true, constants.HWAccelVAAPI: true,
	constants.HWAccelQSV: true, constants.HWAccelVulkan: true,
}

// hwEncoderSuffixes mark codecs that are hardware encoders; they are passed
// through to ffmpeg unchanged instead of being mapped to a software encoder.
var hwEncoderSuffixes = []string{"_nvenc", "_qsv", "_vaapi", "_vulkan", "_amf", "_videotoolbox"}

// SoftwareCodecFor maps a hardware encoder codec (h264_nvenc, hevc_qsv,
// h264_vaapi, ...) to its software counterpart (h264, hevc, ...) so a CPU
// fallback retry does not fail again on the missing hardware encoder.
// Non-hardware codecs are returned unchanged.
func SoftwareCodecFor(codec string) string {
	lower := strings.ToLower(codec)
	for _, suffix := range hwEncoderSuffixes {
		if base, ok := strings.CutSuffix(lower, suffix); ok {
			if base == codecHEVC {
				base = constants.CodecH265
			}
			return base
		}
	}
	return codec
}

// resolveHWAccel picks the effective -hwaccel value for a conversion:
// cfg.HWAccel wins when set ("auto" triggers one-time detection); otherwise the
// legacy UseVulkan flag maps to vulkan. Returns "" for software decoding.
func (fc *FFmpegConverter) resolveHWAccel(cfg *models.ConversionConfig, log *slog.Logger) string {
	if log == nil {
		log = slog.Default()
	}
	hw := strings.ToLower(strings.TrimSpace(cfg.HWAccel))
	if hw == "" && cfg.UseVulkan {
		return constants.HWAccelVulkan
	}
	if hw == constants.HWAccelAuto {
		hw = fc.detectHWAccel(log)
	}
	if hw == constants.HWAccelNone {
		return ""
	}
	if !hwAccelNames[hw] {
		log.Warn("Unknown hw_accel value, ignoring", "hw_accel", cfg.HWAccel)
		return ""
	}
	return hw
}

// detectHWAccel asks ffmpeg which hardware accelerators this build supports and
// picks the best one present on this machine, in preference order
// cuda (NVIDIA) > vaapi (/dev/dri) > qsv > vulkan. The result is cached.
func (fc *FFmpegConverter) detectHWAccel(log *slog.Logger) string {
	fc.hwAccelMu.Lock()
	defer fc.hwAccelMu.Unlock()
	if fc.hwAccelDone {
		return fc.hwAccelCached
	}
	fc.hwAccelDone = true

	available := map[string]bool{}
	// #nosec G204 -- ffmpegPath comes from trusted local worker config; args are fixed literals.
	if out, err := exec.Command(fc.ffmpegPath, "-hide_banner", "-hwaccels").Output(); err == nil {
		for line := range strings.SplitSeq(string(out), "\n") {
			fields := strings.Fields(strings.TrimSpace(line))
			if len(fields) > 0 {
				available[fields[len(fields)-1]] = true
			}
		}
	}

	_, nvidiaDev := os.Stat("/dev/nvidia0")
	_, driDev := os.Stat("/dev/dri/renderD128")
	switch {
	case nvidiaDev == nil && available[constants.HWAccelCUDA]:
		fc.hwAccelCached = constants.HWAccelCUDA
	case driDev == nil && available[constants.HWAccelVAAPI]:
		fc.hwAccelCached = constants.HWAccelVAAPI
	case driDev == nil && available[constants.HWAccelQSV]:
		fc.hwAccelCached = constants.HWAccelQSV
	// Vulkan needs an actual device node too; ffmpeg build support alone
	// would make every job pay a failed hardware attempt.
	case (driDev == nil || nvidiaDev == nil) && available[constants.HWAccelVulkan]:
		fc.hwAccelCached = constants.HWAccelVulkan
	default:
		fc.hwAccelCached = ""
	}
	log.Info("Detected hardware accelerators", "available", available, "selected", fc.hwAccelCached)
	return fc.hwAccelCached
}

// buildFFmpegCommand constructs the FFmpeg command arguments
func (fc *FFmpegConverter) buildFFmpegCommand(
	job *models.Job,
	cfg *models.ConversionConfig,
	log *slog.Logger,
) []string {
	// Hardware-acceleration flags are per-input options and MUST precede the
	// "-i" they apply to, otherwise ffmpeg rejects them as output options.
	// -progress writes machine-readable key=value blocks to stderr; -nostats
	// suppresses the human-oriented status line, which is redrawn with bare
	// carriage returns and would otherwise run together into one endless line.
	args := []string{"-nostdin", "-nostats", "-progress", "pipe:2"}
	if hwaccel := fc.resolveHWAccel(cfg, log); hwaccel != "" {
		args = append(args, "-hwaccel", hwaccel)
		switch hwaccel {
		case constants.HWAccelVAAPI, constants.HWAccelQSV:
			args = append(args, "-hwaccel_device", "/dev/dri/renderD128")
		case constants.HWAccelVulkan:
			args = append(args, "-hwaccel_device", "0") // Device index
		case constants.HWAccelCUDA:
			// no -hwaccel_device needed; ffmpeg picks the default GPU
		}
	}

	args = append(args, "-i", job.SourcePath)

	// Video filtering and encoding
	args = append(args,
		"-vf", fmt.Sprintf("scale=%s", cfg.TargetResolution),
	)

	// Video codec selection with Vulkan support
	videoCodec := fc.getVideoCodecWithLogger(cfg.Codec, cfg.UseVulkan, log)
	args = append(args, "-c:v", videoCodec)

	// Add preset and bitrate for video
	args = append(args,
		"-preset", cfg.Preset,
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
	if log == nil {
		log = slog.Default()
	}

	// Hardware encoders (h264_nvenc, hevc_qsv, h264_vaapi, ...) are passed
	// through unchanged; mapping them to a software encoder would silently
	// defeat the whole point of selecting them.
	lower := strings.ToLower(codec)
	for _, suffix := range hwEncoderSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return lower
		}
	}

	if useVulkan {
		switch codec {
		case constants.CodecH264, "avc":
			return encoderH264Vulkan
		case constants.CodecH265, codecHEVC:
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
	case constants.CodecH265, codecHEVC:
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

// trackProgress consumes FFmpeg's stderr. It reports progress through callback
// once per "-progress" block (not once per line) and returns the last few
// ordinary log lines so a failure can be explained.
func (fc *FFmpegConverter) trackProgress(
	stderr io.Reader,
	totalDuration float64,
	callback ProgressCallback,
	log *slog.Logger,
) []string {
	if log == nil {
		log = slog.Default()
	}

	scanner := bufio.NewScanner(stderr)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var currentTime float64
	var fps float64
	var tail []string

	for scanner.Scan() {
		line := scanner.Text()

		if !ffmpegProgressLinePattern.MatchString(line) {
			if trimmed := strings.TrimSpace(line); trimmed != "" {
				tail = append(tail, trimmed)
				if len(tail) > stderrTailLines {
					tail = tail[1:]
				}
				// Route ffmpeg's own output (errors, warnings, stream info)
				// into the job logger so it lands in the per-job log tail.
				log.Debug("ffmpeg", "line", trimmed)
			}
			continue
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

		// Each block ends with a "progress=continue|end" line; report once per
		// block. Reporting on every line multiplied the callback (and the HTTP
		// request behind it) roughly twelvefold.
		if !strings.HasPrefix(line, "progress=") {
			continue
		}
		// Report even before the first frame is out (progress 0): slow encoders
		// can take a while to get there, and the callback is also how the worker
		// notices that the job was cancelled.
		if callback != nil && totalDuration > 0 {
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

	return tail
}
