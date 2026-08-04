package converter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darkace1998/video-converter-common/constants"
	"github.com/darkace1998/video-converter-common/models"
)

// testScenarioSuccess is the TEST_FFPROBE_SCENARIO value that makes the helper
// process emit a well-formed ffprobe response.
const testScenarioSuccess = "success"

// TestHelperProcess isn't a real test. It's used as a helper process
// for TestGetVideoMetadata.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	args := os.Args
	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}
		args = args[1:]
	}

	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "No command\n")
		os.Exit(2)
	}

	cmd, args := args[0], args[1:]

	if !strings.HasSuffix(cmd, "ffprobe") && !strings.HasSuffix(cmd, "ffprobe.exe") {
		fmt.Fprintf(os.Stderr, "Expected ffprobe, got %s\n", cmd)
		os.Exit(1)
	}

	// We expect the arguments to contain the dummy file path at the end.
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "No arguments provided\n")
		os.Exit(1)
	}
	_ = args[len(args)-1] // filePath

	scenario := os.Getenv("TEST_FFPROBE_SCENARIO")
	switch scenario {
	case testScenarioSuccess:
		fmt.Print(`{
			"format": {
				"duration": "120.5",
				"bit_rate": "5000000"
			},
			"streams": [
				{
					"codec_type": "video",
					"codec_name": "h264",
					"width": 1920,
					"height": 1080,
					"disposition": {
						"default": 1
					}
				},
				{
					"codec_type": "audio",
					"codec_name": "aac",
					"disposition": {
						"default": 1
					}
				}
			]
		}`)
	case "invalid_json":
		fmt.Print(`{ invalid json`)
	case "execution_error":
		os.Exit(1)
	default:
		fmt.Fprintf(os.Stderr, "Unknown scenario: %s\n", scenario)
		os.Exit(1)
	}

	os.Exit(0)
}

func TestNewMetadataExtractor(t *testing.T) {
	tests := []struct {
		name       string
		ffmpegPath string
		expected   string
	}{
		{
			name:       "standard linux path",
			ffmpegPath: testFFmpegPath,
			expected:   "/usr/bin/ffprobe",
		},
		{
			name:       "relative path",
			ffmpegPath: "./bin/ffmpeg",
			expected:   "./bin/ffprobe",
		},
		{
			name:       "only binary name",
			ffmpegPath: "ffmpeg",
			expected:   "ffprobe",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extractor := NewMetadataExtractor(tt.ffmpegPath)

			// We build expected from predefined base string but apply correct separator.
			expectedDir := filepath.Dir(tt.ffmpegPath)
			expectedBase := filepath.Base(tt.expected)
			expectedPath := filepath.Join(expectedDir, expectedBase)

			if extractor.ffprobePath != expectedPath {
				t.Errorf("Expected ffprobe path %s, got %s", expectedPath, extractor.ffprobePath)
			}
		})
	}
}

func TestGetVideoMetadata(t *testing.T) {
	// Setup a dummy file
	tmpFile, err := os.CreateTemp(t.TempDir(), "dummy_video_*.mp4")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpFileName := tmpFile.Name()
	_ = tmpFile.Close()

	// Create a dummy directory to test directory error
	tmpDir := t.TempDir()

	extractor := &MetadataExtractor{
		// Use the test binary itself as the ffprobe executable
		ffprobePath: os.Args[0],
	}

	// This trick ensures exec.Command runs this test binary and calls TestHelperProcess
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")

	// Create a script that properly quotes the execution argument and calls the go test binary
	scriptContent := fmt.Sprintf("#!/bin/sh\n\"%s\" -test.run=TestHelperProcess -- ffprobe \"$@\"\n", os.Args[0])
	scriptFile, err := os.CreateTemp(t.TempDir(), "mock_ffprobe_*.sh")
	if err != nil {
		t.Fatalf("Failed to create mock script: %v", err)
	}
	_, _ = scriptFile.WriteString(scriptContent)
	_ = scriptFile.Close()
	_ = os.Chmod(scriptFile.Name(), 0755)

	extractor.ffprobePath = scriptFile.Name()

	tests := []struct {
		name          string
		sourcePath    string
		scenario      string
		expectError   bool
		checkMetadata func(*testing.T, *models.VideoMetadata)
	}{
		{
			name:        "successful extraction",
			sourcePath:  tmpFileName,
			scenario:    testScenarioSuccess,
			expectError: false,
			checkMetadata: func(t *testing.T, m *models.VideoMetadata) {
				t.Helper()
				if m.Duration != 120.5 {
					t.Errorf("Expected duration 120.5, got %v", m.Duration)
				}
				if m.Bitrate != 5000000 {
					t.Errorf("Expected bitrate 5000000, got %v", m.Bitrate)
				}
				if m.VideoCodec != constants.CodecH264 {
					t.Errorf("Expected video codec h264, got %s", m.VideoCodec)
				}
				if m.Width != 1920 {
					t.Errorf("Expected width 1920, got %d", m.Width)
				}
				if m.Height != 1080 {
					t.Errorf("Expected height 1080, got %d", m.Height)
				}
				if m.AudioCodec != constants.AudioCodecAAC {
					t.Errorf("Expected audio codec aac, got %s", m.AudioCodec)
				}
			},
		},
		{
			name:        "file does not exist",
			sourcePath:  "non_existent_file.mp4",
			scenario:    testScenarioSuccess,
			expectError: true,
		},
		{
			name:        "path is a directory",
			sourcePath:  tmpDir,
			scenario:    testScenarioSuccess,
			expectError: true,
		},
		{
			name:        "invalid json from ffprobe",
			sourcePath:  tmpFileName,
			scenario:    "invalid_json",
			expectError: true,
		},
		{
			name:        "ffprobe execution error",
			sourcePath:  tmpFileName,
			scenario:    "execution_error",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TEST_FFPROBE_SCENARIO", tt.scenario)

			metadata, err := extractor.GetVideoMetadata(tt.sourcePath)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
				if tt.checkMetadata != nil {
					tt.checkMetadata(t, metadata)
				}
			}
		})
	}
}

func TestApplyMetadataToJob(t *testing.T) {
	job := &models.Job{}
	metadata := &models.VideoMetadata{
		Duration:   120.5,
		Width:      1920,
		Height:     1080,
		VideoCodec: constants.CodecH264,
		AudioCodec: constants.AudioCodecAAC,
		Bitrate:    5000000,
		FileSize:   1024000,
	}

	ApplyMetadataToJob(job, metadata)

	if job.SourceDuration != 120.5 {
		t.Errorf("Expected SourceDuration 120.5, got %v", job.SourceDuration)
	}
	if job.SourceWidth != 1920 {
		t.Errorf("Expected SourceWidth 1920, got %v", job.SourceWidth)
	}
	if job.SourceHeight != 1080 {
		t.Errorf("Expected SourceHeight 1080, got %v", job.SourceHeight)
	}
	if job.SourceVideoCodec != constants.CodecH264 {
		t.Errorf("Expected SourceVideoCodec h264, got %s", job.SourceVideoCodec)
	}
	if job.SourceAudioCodec != constants.AudioCodecAAC {
		t.Errorf("Expected SourceAudioCodec aac, got %s", job.SourceAudioCodec)
	}
	if job.SourceBitrate != 5000000 {
		t.Errorf("Expected SourceBitrate 5000000, got %v", job.SourceBitrate)
	}
	if job.SourceFileSize != 1024000 {
		t.Errorf("Expected SourceFileSize 1024000, got %v", job.SourceFileSize)
	}
}
