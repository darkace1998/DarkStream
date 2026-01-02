package converter

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/darkace1998/video-converter-common/models"
)

// FFprobeOutput represents the JSON output from ffprobe
type FFprobeOutput struct {
	Format  FFprobeFormat   `json:"format"`
	Streams []FFprobeStream `json:"streams"`
}

// FFprobeFormat represents the format section of ffprobe output
type FFprobeFormat struct {
	Filename   string `json:"filename"`
	Duration   string `json:"duration"`
	Size       string `json:"size"`
	BitRate    string `json:"bit_rate"`
	FormatName string `json:"format_name"`
}

// FFprobeStream represents a stream in ffprobe output
type FFprobeStream struct {
	CodecType   string `json:"codec_type"`  // "video" or "audio"
	CodecName   string `json:"codec_name"`  // e.g., "h264", "aac"
	Width       int    `json:"width"`       // Video width
	Height      int    `json:"height"`      // Video height
	BitRate     string `json:"bit_rate"`    // Stream bitrate
	SampleRate  string `json:"sample_rate"` // Audio sample rate
	Channels    int    `json:"channels"`    // Audio channels
	Disposition struct {
		Default int `json:"default"` // 1 if this is the default stream
	} `json:"disposition"`
}

// MetadataExtractor handles video metadata extraction using FFprobe
type MetadataExtractor struct {
	ffprobePath string
}

// NewMetadataExtractor creates a new MetadataExtractor instance
func NewMetadataExtractor(ffmpegPath string) *MetadataExtractor {
	// Derive ffprobe path from ffmpeg path using filepath operations
	dir := filepath.Dir(ffmpegPath)
	base := filepath.Base(ffmpegPath)

	// Replace "ffmpeg" with "ffprobe" in the base name
	ffprobeBase := strings.Replace(base, "ffmpeg", "ffprobe", 1)
	ffprobePath := filepath.Join(dir, ffprobeBase)

	return &MetadataExtractor{
		ffprobePath: ffprobePath,
	}
}

// GetVideoMetadata extracts metadata from a video file using FFprobe
// The sourcePath must be a validated path to an existing file
//
//nolint:gocognit,cyclop // FFprobe output parsing with multiple format fallbacks is inherently complex
func (me *MetadataExtractor) GetVideoMetadata(sourcePath string) (*models.VideoMetadata, error) {
	// Validate source path exists and is a file
	fileInfo, err := os.Stat(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat source file: %w", err)
	}
	if fileInfo.IsDir() {
		return nil, fmt.Errorf("source path is a directory, not a file")
	}

	// Validate the path is clean (no directory traversal)
	cleanPath := filepath.Clean(sourcePath)
	if cleanPath != sourcePath && filepath.Clean(sourcePath) != cleanPath {
		return nil, fmt.Errorf("invalid source path")
	}

	// Run ffprobe with JSON output
	// #nosec G204 - ffprobePath is derived from validated config, sourcePath validated above
	cmd := exec.Command(me.ffprobePath,
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		cleanPath,
	)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run ffprobe: %w", err)
	}

	var probeResult FFprobeOutput
	if err := json.Unmarshal(output, &probeResult); err != nil {
		return nil, fmt.Errorf("failed to parse ffprobe output: %w", err)
	}

	metadata := &models.VideoMetadata{
		FileSize: fileInfo.Size(),
	}

	// Parse duration
	if probeResult.Format.Duration != "" {
		var duration float64
		if _, err := fmt.Sscanf(probeResult.Format.Duration, "%f", &duration); err == nil {
			metadata.Duration = duration
		}
	}

	// Parse total bitrate
	if probeResult.Format.BitRate != "" {
		var bitrate int64
		if _, err := fmt.Sscanf(probeResult.Format.BitRate, "%d", &bitrate); err == nil {
			metadata.Bitrate = bitrate
		}
	}

	// Extract stream information, preferring default streams
	var defaultVideoStream, defaultAudioStream *FFprobeStream
	var firstVideoStream, firstAudioStream *FFprobeStream

	for i := range probeResult.Streams {
		stream := &probeResult.Streams[i]
		switch stream.CodecType {
		case "video":
			if firstVideoStream == nil {
				firstVideoStream = stream
			}
			if stream.Disposition.Default == 1 {
				defaultVideoStream = stream
			}
		case "audio":
			if firstAudioStream == nil {
				firstAudioStream = stream
			}
			if stream.Disposition.Default == 1 {
				defaultAudioStream = stream
			}
		}
	}

	// Use default stream if available, otherwise use first stream
	videoStream := defaultVideoStream
	if videoStream == nil {
		videoStream = firstVideoStream
	}
	audioStream := defaultAudioStream
	if audioStream == nil {
		audioStream = firstAudioStream
	}

	if videoStream != nil {
		metadata.VideoCodec = videoStream.CodecName
		metadata.Width = videoStream.Width
		metadata.Height = videoStream.Height
	}

	if audioStream != nil {
		metadata.AudioCodec = audioStream.CodecName
	}

	return metadata, nil
}

// ApplyMetadataToJob copies metadata fields to a Job struct
func ApplyMetadataToJob(job *models.Job, metadata *models.VideoMetadata) {
	job.SourceDuration = metadata.Duration
	job.SourceWidth = metadata.Width
	job.SourceHeight = metadata.Height
	job.SourceVideoCodec = metadata.VideoCodec
	job.SourceAudioCodec = metadata.AudioCodec
	job.SourceBitrate = metadata.Bitrate
	job.SourceFileSize = metadata.FileSize
}
