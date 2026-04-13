package main

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var cliBinaryPath string

func TestCLIHelp(t *testing.T) {
	// Test that CLI shows help when no command is provided
	cmd := exec.Command(cliBinaryPath)
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
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get working directory: %v\n", err)
		os.Exit(2)
	}

	binaryDir, err := os.MkdirTemp("", "darkstream-cli-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create temp dir for CLI binary: %v\n", err)
		os.Exit(2)
	}
	cliBinaryPath = filepath.Join(binaryDir, "video-converter-cli")

	buildCmd := exec.Command("go", "build", "-o", cliBinaryPath, ".")
	buildCmd.Dir = wd
	out, err := buildCmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to build CLI binary: %s: %v\n", string(out), err)
		os.Exit(2)
	}

	code := m.Run()

	// Clean up the built binary
	_ = os.Remove(cliBinaryPath)
	_ = os.Remove(binaryDir)
	os.Exit(code)
}

func waitForHTTPStatus(t *testing.T, url string, wantStatus int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error

	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == wantStatus {
				return
			}
			lastErr = fmt.Errorf("unexpected status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %s to return %d: %v", url, wantStatus, lastErr)
}

func TestDetectCommand(t *testing.T) {
	// Test the detect command
	cmd := exec.Command(cliBinaryPath, "detect")
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
	cmd := exec.Command(cliBinaryPath, "status", "--master-url", "http://localhost:19999")
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
	cmd := exec.Command(cliBinaryPath, "master")
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
	cmd := exec.Command(cliBinaryPath, "worker")
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
	repoRoot := filepath.Join("..")
	masterBinary := filepath.Join(repoRoot, "video-converter-master", "master")
	buildBinary(t, masterBinary, filepath.Join(repoRoot, "video-converter-master"))

	var err error

	// Create test directories
	testDir := t.TempDir()
	videosDir := filepath.Join(testDir, "videos")
	convertedDir := filepath.Join(testDir, "converted")
	err = os.MkdirAll(videosDir, 0o750)
	if err != nil {
		t.Fatalf("Failed to create test directories: %v", err)
	}
	err = os.MkdirAll(convertedDir, 0o750)
	if err != nil {
		t.Fatalf("Failed to create test directories: %v", err)
	}

	// Create test config
	config := `server:
  port: 19876
  host: 127.0.0.1
scanner:
  root_path: ` + videosDir + `
  video_extensions:
    - .mp4
  output_base: ` + convertedDir + `
  scan_interval: 0s
database:
  path: ` + filepath.Join(testDir, "jobs.db") + `
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
	err = os.WriteFile(configPath, []byte(config), 0o600)
	if err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Start master server
	masterCmd := exec.Command(cliBinaryPath, "master", configPath)
	var masterOut bytes.Buffer
	masterCmd.Stdout = &masterOut
	masterCmd.Stderr = &masterOut
	err = masterCmd.Start()
	if err != nil {
		t.Fatalf("Failed to start master: %v", err)
	}
	defer func() {
		killErr := masterCmd.Process.Kill()
		if killErr != nil {
			t.Logf("Failed to kill master process: %v", killErr)
		}
	}()

	waitForHTTPStatus(t, "http://127.0.0.1:19876/api/status", http.StatusOK, 30*time.Second)

	// Test status command
	statusCmd := exec.Command(cliBinaryPath, "status", "--master-url", "http://127.0.0.1:19876")
	output, err := statusCmd.CombinedOutput()
	if err != nil {
		t.Errorf("Status command failed: %v\nOutput: %s\nMaster output: %s", err, string(output), masterOut.String())
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "Conversion Progress") {
		t.Errorf("Expected progress output, got: %s", outputStr)
	}

	// Test stats command
	statsCmd := exec.Command(cliBinaryPath, "stats", "--master-url", "http://127.0.0.1:19876")
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
	cmd := exec.Command(cliBinaryPath, "validate")
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
	cmd := exec.Command(cliBinaryPath, "validate", "--type", "master")
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
	cmd := exec.Command(cliBinaryPath, "validate", "--type", "invalid")
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
	cmd := exec.Command(cliBinaryPath, "cancel")
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
	cmd := exec.Command(cliBinaryPath, "jobs", "--master-url", "http://localhost:19999")
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
	cmd := exec.Command(cliBinaryPath, "workers", "--master-url", "http://localhost:19999")
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
	cmd := exec.Command(cliBinaryPath, "help")
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
	testDir := t.TempDir()
	err := os.MkdirAll(testDir, 0o750)
	if err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// Create a valid master config
	videosDir := filepath.Join(testDir, "videos")
	outputDir := filepath.Join(testDir, "output")
	dbPath := filepath.Join(testDir, "jobs.db")
	validConfig := `server:
  port: 8080
  host: 0.0.0.0
scanner:
  root_path: ` + videosDir + `
  video_extensions:
    - .mp4
  output_base: ` + outputDir + `
database:
  path: ` + dbPath + `
logging:
  level: info
`
	configPath := testDir + "/valid-config.yaml"
	err = os.WriteFile(configPath, []byte(validConfig), 0o600)
	if err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Test valid config
	cmd := exec.Command(cliBinaryPath, "validate", "--type", "master", "--file", configPath, "--local")
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
	testDir := t.TempDir()
	err := os.MkdirAll(testDir, 0o750)
	if err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// Create an invalid master config (missing required sections)
	invalidConfig := `logging:
  level: info
`
	configPath := testDir + "/invalid-config.yaml"
	err = os.WriteFile(configPath, []byte(invalidConfig), 0o600)
	if err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Test invalid config
	cmd := exec.Command(cliBinaryPath, "validate", "--type", "master", "--file", configPath, "--local")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Error("Expected error for invalid config")
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "validation failed") {
		t.Errorf("Expected validation failed message, got: %s", outputStr)
	}
}

func buildBinary(t *testing.T, binaryPath, sourceDir string) {
	t.Helper()

	buildCmd := exec.Command("go", "build", "-o", filepath.Base(binaryPath), "./main.go")
	buildCmd.Dir = sourceDir
	out, err := buildCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to build binary %s: %v\n%s", binaryPath, err, out)
	}
}
