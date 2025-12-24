package main

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestCLIHelp(t *testing.T) {
	// Test that CLI shows help when no command is provided
	cmd := exec.Command("./video-converter-cli")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Error("Expected error when no command provided")
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "Video Converter CLI") {
		t.Errorf("Expected help text, got: %s", outputStr)
	}
	if !strings.Contains(outputStr, "master") {
		t.Error("Expected 'master' command in help")
	}
	if !strings.Contains(outputStr, "worker") {
		t.Error("Expected 'worker' command in help")
	}
	if !strings.Contains(outputStr, "status") {
		t.Error("Expected 'status' command in help")
	}
}

// TestMain builds the CLI binary before running tests and cleans up afterwards.
func TestMain(m *testing.M) {
	// Build the CLI binary used by the integration-style tests.
	buildCmd := exec.Command("go", "build", "-o", "video-converter-cli", ".")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to build CLI binary: %s: %v\n", string(out), err)
		os.Exit(2)
	}

	code := m.Run()

	// Clean up the built binary
	_ = os.Remove("video-converter-cli")
	os.Exit(code)
}

func TestDetectCommand(t *testing.T) {
	// Test the detect command
	cmd := exec.Command("./video-converter-cli", "detect")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("Detect command failed: %v", err)
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "GPU / Vulkan Detection") {
		t.Errorf("Expected detection header, got: %s", outputStr)
	}
	if !strings.Contains(outputStr, "Environment:") {
		t.Errorf("Expected environment section, got: %s", outputStr)
	}
}

func TestStatusCommandNoServer(t *testing.T) {
	// Test status command when server is not running
	cmd := exec.Command("./video-converter-cli", "status", "--master-url", "http://localhost:19999")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Error("Expected error when server is not running")
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "Error connecting") {
		t.Errorf("Expected connection error, got: %s", outputStr)
	}
}

func TestMasterCommandNoConfig(t *testing.T) {
	// Test master command without config file
	cmd := exec.Command("./video-converter-cli", "master")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Error("Expected error when no config provided")
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "config file path is required") {
		t.Errorf("Expected config error, got: %s", outputStr)
	}
}

func TestWorkerCommandNoConfig(t *testing.T) {
	// Test worker command without config file
	cmd := exec.Command("./video-converter-cli", "worker")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Error("Expected error when no config provided")
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "config file path is required") {
		t.Errorf("Expected config error, got: %s", outputStr)
	}
}

func TestEndToEndWithMaster(t *testing.T) {
	// This test requires the master and worker binaries to be built
	// Skip if they don't exist
	if _, err := os.Stat("../video-converter-master/video-converter-master"); os.IsNotExist(err) {
		t.Skip("Master binary not found, skipping end-to-end test")
	}

	// Create test directories
	testDir := "/tmp/cli-integration-test"
	_ = os.RemoveAll(testDir)
	if err := os.MkdirAll(testDir+"/videos", 0o750); err != nil {
		t.Fatalf("Failed to create test directories: %v", err)
	}
	if err := os.MkdirAll(testDir+"/converted", 0o750); err != nil {
		t.Fatalf("Failed to create test directories: %v", err)
	}
	defer func() {
		_ = os.RemoveAll(testDir)
	}()

	// Create test config
	config := `server:
  port: 19876
  host: 127.0.0.1
scanner:
  root_path: ` + testDir + `/videos
  video_extensions:
    - .mp4
  output_base: ` + testDir + `/converted
  scan_interval: 0s
database:
  path: ` + testDir + `/jobs.db
conversion:
  target_resolution: 640x360
  codec: h264
  bitrate: 1M
  preset: ultrafast
logging:
  level: info
  format: text
`
	configPath := testDir + "/master-config.yaml"
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Start master server
	masterCmd := exec.Command("./video-converter-cli", "master", configPath)
	var masterOut bytes.Buffer
	masterCmd.Stdout = &masterOut
	masterCmd.Stderr = &masterOut
	if err := masterCmd.Start(); err != nil {
		t.Fatalf("Failed to start master: %v", err)
	}
	defer func() {
		if err := masterCmd.Process.Kill(); err != nil {
			t.Logf("Failed to kill master process: %v", err)
		}
	}()

	// Wait for server to start
	time.Sleep(2 * time.Second)

	// Test status command
	statusCmd := exec.Command("./video-converter-cli", "status", "--master-url", "http://127.0.0.1:19876")
	output, err := statusCmd.CombinedOutput()
	if err != nil {
		t.Errorf("Status command failed: %v\nOutput: %s\nMaster output: %s", err, string(output), masterOut.String())
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "Conversion Progress") {
		t.Errorf("Expected progress output, got: %s", outputStr)
	}

	// Test stats command
	statsCmd := exec.Command("./video-converter-cli", "stats", "--master-url", "http://127.0.0.1:19876")
	output, err = statsCmd.CombinedOutput()
	if err != nil {
		t.Errorf("Stats command failed: %v\nOutput: %s", err, string(output))
	}

	outputStr = string(output)
	if !strings.Contains(outputStr, "Detailed Statistics") {
		t.Errorf("Expected stats output, got: %s", outputStr)
	}

	slog.Info("✅ All integration tests passed")
}

func TestValidateCommandNoArgs(t *testing.T) {
	// Test validate command without required arguments
	cmd := exec.Command("./video-converter-cli", "validate")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Error("Expected error when no type provided")
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "Config type is required") {
		t.Errorf("Expected type required error, got: %s", outputStr)
	}
}

func TestValidateCommandNoFile(t *testing.T) {
	// Test validate command without file argument
	cmd := exec.Command("./video-converter-cli", "validate", "--type", "master")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Error("Expected error when no file provided")
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "Config file path is required") {
		t.Errorf("Expected file required error, got: %s", outputStr)
	}
}

func TestValidateCommandInvalidType(t *testing.T) {
	// Test validate command with invalid type
	cmd := exec.Command("./video-converter-cli", "validate", "--type", "invalid")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Error("Expected error for invalid type")
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "Invalid config type") {
		t.Errorf("Expected invalid type error, got: %s", outputStr)
	}
}

func TestCancelCommandNoJobID(t *testing.T) {
	// Test cancel command without job ID
	cmd := exec.Command("./video-converter-cli", "cancel")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Error("Expected error when no job ID provided")
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "Job ID is required") {
		t.Errorf("Expected job ID required error, got: %s", outputStr)
	}
}

func TestJobsCommandNoServer(t *testing.T) {
	// Test jobs command when server is not running
	cmd := exec.Command("./video-converter-cli", "jobs", "--master-url", "http://localhost:19999")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Error("Expected error when server is not running")
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "Error connecting") {
		t.Errorf("Expected connection error, got: %s", outputStr)
	}
}

func TestWorkersCommandNoServer(t *testing.T) {
	// Test workers command when server is not running
	cmd := exec.Command("./video-converter-cli", "workers", "--master-url", "http://localhost:19999")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Error("Expected error when server is not running")
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "Error connecting") {
		t.Errorf("Expected connection error, got: %s", outputStr)
	}
}

func TestHelpCommand(t *testing.T) {
	// Test help command shows usage
	cmd := exec.Command("./video-converter-cli", "help")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("Help command should not fail: %v", err)
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "Video Converter CLI") {
		t.Error("Expected help text header")
	}
	if !strings.Contains(outputStr, "validate") {
		t.Error("Expected 'validate' command in help")
	}
	if !strings.Contains(outputStr, "cancel") {
		t.Error("Expected 'cancel' command in help")
	}
	if !strings.Contains(outputStr, "workers") {
		t.Error("Expected 'workers' command in help")
	}
	if !strings.Contains(outputStr, "jobs") {
		t.Error("Expected 'jobs' command in help")
	}
}

func TestLocalValidation(t *testing.T) {
	// Create a temporary config file
	testDir := "/tmp/cli-validate-test"
	_ = os.RemoveAll(testDir)
	if err := os.MkdirAll(testDir, 0o750); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}
	defer func() {
		_ = os.RemoveAll(testDir)
	}()

	// Create a valid master config
	validConfig := `server:
  port: 8080
  host: 0.0.0.0
scanner:
  root_path: /tmp/videos
  video_extensions:
    - .mp4
  output_base: /tmp/output
database:
  path: /tmp/jobs.db
logging:
  level: info
`
	configPath := testDir + "/valid-config.yaml"
	if err := os.WriteFile(configPath, []byte(validConfig), 0o600); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Test valid config
	cmd := exec.Command("./video-converter-cli", "validate", "--type", "master", "--file", configPath, "--local")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("Validate command failed for valid config: %v\nOutput: %s", err, string(output))
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "Configuration is valid") {
		t.Errorf("Expected valid config message, got: %s", outputStr)
	}
}

func TestLocalValidationInvalid(t *testing.T) {
	// Create a temporary config file with missing required sections
	testDir := "/tmp/cli-validate-invalid-test"
	_ = os.RemoveAll(testDir)
	if err := os.MkdirAll(testDir, 0o750); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}
	defer func() {
		_ = os.RemoveAll(testDir)
	}()

	// Create an invalid master config (missing required sections)
	invalidConfig := `logging:
  level: info
`
	configPath := testDir + "/invalid-config.yaml"
	if err := os.WriteFile(configPath, []byte(invalidConfig), 0o600); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Test invalid config
	cmd := exec.Command("./video-converter-cli", "validate", "--type", "master", "--file", configPath, "--local")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Error("Expected error for invalid config")
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "validation failed") {
		t.Errorf("Expected validation failed message, got: %s", outputStr)
	}
}
