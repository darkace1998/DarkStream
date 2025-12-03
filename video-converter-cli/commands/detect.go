// Package commands provides CLI commands for the video converter.
package commands

import (
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// Detect displays GPU and Vulkan capabilities available on the system.
func Detect(_ []string) {
	slog.Info("🖥️  GPU / Vulkan Detection")
	slog.Info("")

	// Check FFmpeg availability
	detectFFmpeg()
	slog.Info("")

	// Check Vulkan support
	detectVulkan()
	slog.Info("")

	// System information
	detectSystem()
}

func detectFFmpeg() {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		slog.Info("FFmpeg Status: ✗ Not Found")
		slog.Info("  Please install FFmpeg to use video conversion")
		return
	}

	slog.Info("FFmpeg Status: ✓ Available")
	slog.Info("  ├─ Path: " + ffmpegPath)

	// Get FFmpeg version
	cmd := exec.Command("ffmpeg", "-version")
	output, err := cmd.Output()
	if err == nil {
		lines := strings.Split(string(output), "\n")
		if len(lines) > 0 {
			version := strings.TrimSpace(lines[0])
			slog.Info("  └─ Version: " + version)
		}
	}

	// Check for hardware acceleration support
	cmd = exec.Command("ffmpeg", "-hwaccels")
	output, err = cmd.Output()
	if err == nil {
		hwaccels := strings.Split(string(output), "\n")
		slog.Info("  └─ Hardware Acceleration:")
		// Filter out empty lines and header
		var filtered []string
		for _, hwaccel := range hwaccels {
			hwaccel = strings.TrimSpace(hwaccel)
			if hwaccel != "" && hwaccel != "Hardware acceleration methods:" {
				filtered = append(filtered, hwaccel)
			}
		}
		for i, hwaccel := range filtered {
			prefix := "├─"
			if i == len(filtered)-1 {
				prefix = "└─"
			}
			if strings.Contains(hwaccel, "vulkan") {
				slog.Info("     " + prefix + " " + hwaccel + " ✓")
			} else {
				slog.Info("     " + prefix + " " + hwaccel)
			}
		}
	}
}

func detectVulkan() {
	// Try to detect Vulkan using vulkaninfo
	vulkanPath, err := exec.LookPath("vulkaninfo")
	if err != nil {
		slog.Info("Vulkan Status: ⚠ Cannot detect (vulkaninfo not found)")
		slog.Info("  Install vulkan-tools to check Vulkan capabilities")
		return
	}

	slog.Info("Vulkan Status: ✓ Tools Available")
	slog.Info("  └─ Path: " + vulkanPath)

	// Try to get basic Vulkan info
	cmd := exec.Command("vulkaninfo", "--summary")
	output, err := cmd.Output()
	if err != nil {
		slog.Info("  └─ Device Detection: Failed (no Vulkan devices found)")
		return
	}

	// Parse vulkaninfo output for device information
	lines := strings.Split(string(output), "\n")
	deviceFound := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "deviceName") || strings.Contains(line, "GPU") {
			if !deviceFound {
				slog.Info("  └─ Devices:")
				deviceFound = true
			}
			slog.Info("     ├─ " + line)
		}
	}

	if !deviceFound {
		slog.Info("  └─ No Vulkan-capable devices detected")
	}
}

func detectSystem() {
	slog.Info("Environment:")
	slog.Info("├─ OS: " + runtime.GOOS)
	slog.Info("├─ Architecture: " + runtime.GOARCH)
	slog.Info("├─ CPUs: " + strconv.Itoa(runtime.NumCPU()))

	// Get hostname
	hostname, err := os.Hostname()
	if err == nil {
		slog.Info("└─ Hostname: " + hostname)
	}
}
