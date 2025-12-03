package main

import (
	"bytes"
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
