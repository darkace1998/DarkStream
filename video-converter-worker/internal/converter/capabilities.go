package converter

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/darkace1998/video-converter-common/constants"
	"github.com/darkace1998/video-converter-common/models"
)

const (
	// probeTimeout bounds a single backend probe. A hardware encoder that is
	// going to work opens in well under this; one that hangs is unusable anyway.
	probeTimeout = 30 * time.Second

	// probeSize is deliberately small but 16-aligned, since several hardware
	// encoders require aligned dimensions.
	probeSize = "128x128"
)

// probeBackends is the set of backends worth probing, cheapest first.
var probeBackends = []string{HWAccelNone, HWAccelVulkan}

// probeCodecs are the video codecs the Web UI offers. Each is probed per backend
// rather than assuming that support for one implies the rest: an FFmpeg build
// can carry libx264 without libx265, and the Vulkan encoder covers only h264 and
// h265, so a single h264 probe would have misrepresented every other choice.
var probeCodecs = []string{
	constants.CodecH264,
	constants.CodecH265,
	constants.CodecVP9,
	constants.CodecAV1,
}

// encoderForProbe returns the encoder implementing codec on backend, and whether
// the combination exists at all. Unlike the conversion path it does not fall
// back to h264, since reporting a substituted encoder as "working" would be a
// lie about the codec that was asked for.
func encoderForProbe(codec, backend string) (string, bool) {
	switch backend {
	case HWAccelVulkan:
		switch codec {
		case constants.CodecH264:
			return encoderH264Vulkan, true
		case constants.CodecH265:
			return "hevc_vulkan", true
		default:
			return "", false // Vulkan encodes only h264 and h265
		}
	default:
		switch codec {
		case constants.CodecH264:
			return encoderLibx264, true
		case constants.CodecH265:
			return "libx265", true
		case constants.CodecVP9:
			return "libvpx-vp9", true
		case constants.CodecAV1:
			return "libaom-av1", true
		default:
			return "", false
		}
	}
}

// DetectCapabilities determines which encoding backends this host can actually
// use. Rather than trusting driver-advertised extensions — which lie: a GPU can
// expose the Vulkan video-encode extensions and still fail to open the encoder —
// each backend is verified by running a real conversion through the same command
// builder production jobs use. If a probe succeeds, a real job will too.
func (fc *FFmpegConverter) DetectCapabilities() *models.WorkerCapabilities {
	caps := &models.WorkerCapabilities{
		ProbedAt:      time.Now(),
		FFmpegVersion: fc.ffmpegVersion(),
	}

	if fc.vulkanDetector != nil {
		if vc, err := fc.vulkanDetector.DetectVulkanCapabilities(); err == nil && vc.Supported {
			caps.GPU = vc.Device.Name
		}
	}

	sourcePath, cleanup, err := fc.makeProbeSource()
	if err != nil {
		// Without a sample clip nothing can be verified. Report every backend as
		// unknown-but-not-working rather than guessing.
		slog.Warn("Could not create capability probe source", "error", err)
		for _, backend := range probeBackends {
			for _, codec := range probeCodecs {
				encoder, ok := encoderForProbe(codec, backend)
				if !ok {
					continue
				}
				caps.Backends = append(caps.Backends, models.BackendCapability{
					Name:    backend,
					Codec:   codec,
					Encoder: encoder,
					Detail:  "probe source could not be created: " + err.Error(),
				})
			}
		}
		return caps
	}
	defer cleanup()

	for _, backend := range probeBackends {
		for _, codec := range probeCodecs {
			encoder, ok := encoderForProbe(codec, backend)
			if !ok {
				continue // this backend has no encoder for that codec
			}
			caps.Backends = append(caps.Backends, fc.probeBackend(backend, codec, encoder, sourcePath))
		}
	}

	slog.Info("Encoder capabilities detected",
		"working", caps.WorkingBackends(),
		"gpu", caps.GPU)

	return caps
}

// probeBackend runs one real conversion for a backend and codec pair and reports
// the outcome.
func (fc *FFmpegConverter) probeBackend(backend, codec, encoder, sourcePath string) models.BackendCapability {
	result := models.BackendCapability{Name: backend, Codec: codec, Encoder: encoder}

	available, err := fc.hasEncoder(encoder)
	if err != nil {
		result.Detail = "could not list encoders: " + err.Error()
		return result
	}
	result.Available = available
	if !available {
		result.Detail = fmt.Sprintf("encoder %q is not compiled into this FFmpeg build", encoder)
		return result
	}

	outputPath := filepath.Join(filepath.Dir(sourcePath), "probe-out-"+backend+"-"+codec+".mkv")
	defer func() { _ = os.Remove(outputPath) }()

	// Build the command exactly as a real job would, so the probe exercises the
	// hwaccel flags, filter chain and preset mapping that jobs depend on.
	job := &models.Job{SourcePath: sourcePath, OutputPath: outputPath}
	cfg := &models.ConversionConfig{
		TargetResolution: probeSize,
		Codec:            codec,
		Bitrate:          "1M",
		Preset:           constants.PresetFast,
		AudioCodec:       constants.AudioCodecAAC,
		AudioBitrate:     "64k",
		OutputFormat:     constants.FormatMKV,
		HWAccel:          backend,
	}

	args := fc.buildFFmpegCommand(job, cfg, slog.Default())

	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	// #nosec G204 - ffmpegPath is operator-configured and args are built internally
	cmd := exec.CommandContext(ctx, fc.ffmpegPath, args...)
	output, runErr := cmd.CombinedOutput()

	switch {
	case ctx.Err() != nil:
		result.Detail = "probe timed out after " + probeTimeout.String()
	case runErr != nil:
		result.Detail = summarizeFFmpegError(string(output), runErr)
	default:
		result.Working = true
	}

	if !result.Working {
		slog.Info("Encoder unavailable", "backend", backend, "codec", codec,
			"encoder", encoder, "detail", result.Detail)
	}

	return result
}

// makeProbeSource renders a short clip used as input for the probes. It returns
// the path and a cleanup function.
func (fc *FFmpegConverter) makeProbeSource() (path string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "darkstream-probe-")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create probe directory: %w", err)
	}
	cleanup = func() { _ = os.RemoveAll(dir) }

	source := filepath.Join(dir, "probe-src.mp4")
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	// #nosec G204 - ffmpegPath is operator-configured; all other args are literals
	cmd := exec.CommandContext(ctx, fc.ffmpegPath,
		"-v", "error",
		"-f", "lavfi", "-i", "testsrc=size="+probeSize+":rate=5:duration=1",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-c:v", encoderLibx264, "-preset", "ultrafast",
		"-c:a", encoderAAC, "-shortest",
		"-y", source,
	)
	if out, runErr := cmd.CombinedOutput(); runErr != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to render probe clip: %w: %s", runErr, strings.TrimSpace(string(out)))
	}

	return source, cleanup, nil
}

// hasEncoder reports whether FFmpeg was built with the named encoder.
func (fc *FFmpegConverter) hasEncoder(name string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	// #nosec G204 - ffmpegPath is operator-configured; args are literals
	cmd := exec.CommandContext(ctx, fc.ffmpegPath, "-hide_banner", "-encoders")
	output, err := cmd.Output()
	if err != nil {
		return false, err
	}

	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		// Encoder lines look like: " V....D h264_nvenc  NVIDIA NVENC H.264 encoder"
		if len(fields) >= 2 && fields[1] == name {
			return true, nil
		}
	}
	return false, nil
}

// ffmpegVersion returns the first line of `ffmpeg -version`, which identifies
// the build that produced these results.
func (fc *FFmpegConverter) ffmpegVersion() string {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	// #nosec G204 - ffmpegPath is operator-configured; args are literals
	cmd := exec.CommandContext(ctx, fc.ffmpegPath, "-version")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	lines := strings.SplitN(string(output), "\n", 2)
	return strings.TrimSpace(lines[0])
}

// genericFFmpegErrors are FFmpeg's closing summary lines. They are always last
// and say nothing useful, so the specific error above them is preferred.
var genericFFmpegErrors = []string{
	"conversion failed!",
	"error while filtering",
	"task finished with error code",
	"terminating thread with return code",
}

// summarizeFFmpegError picks the most informative line out of FFmpeg's output so
// the reason a backend is unusable survives into the UI, instead of just an exit
// status.
func summarizeFFmpegError(output string, runErr error) string {
	lines := strings.Split(output, "\n")
	fallback := ""

	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if !strings.Contains(lower, "error") && !strings.Contains(lower, "invalid") &&
			!strings.Contains(lower, "not supported") && !strings.Contains(lower, "cannot") &&
			!strings.Contains(lower, "failed") && !strings.Contains(lower, "unknown") {
			continue
		}

		if isGenericFFmpegError(lower) {
			// Remember it, but keep looking for something more specific.
			if fallback == "" {
				fallback = line
			}
			continue
		}
		return truncate(line, 300)
	}

	if fallback != "" {
		return truncate(fallback, 300)
	}

	if trimmed := strings.TrimSpace(output); trimmed != "" {
		lines := strings.Split(trimmed, "\n")
		return truncate(strings.TrimSpace(lines[len(lines)-1]), 300)
	}

	return runErr.Error()
}

// isGenericFFmpegError reports whether a line is one of FFmpeg's uninformative
// closing summaries.
func isGenericFFmpegError(lowerLine string) bool {
	for _, generic := range genericFFmpegErrors {
		if strings.Contains(lowerLine, generic) {
			return true
		}
	}
	return false
}

// truncate shortens s to at most n characters.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
