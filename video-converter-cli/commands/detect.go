package commands

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Detect displays GPU and Vulkan capabilities available on the system.
func Detect(args []string) {
	fmt.Println("🖥️  GPU / Vulkan Detection")
	fmt.Println()

	// Check FFmpeg availability
	detectFFmpeg()
	fmt.Println()

	// Check Vulkan support
	detectVulkan()
	fmt.Println()

	// System information
	detectSystem()
}

func detectFFmpeg() {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		fmt.Println("FFmpeg Status: ✗ Not Found")
		fmt.Println("  Please install FFmpeg to use video conversion")
		return
	}

	fmt.Println("FFmpeg Status: ✓ Available")
	fmt.Printf("  ├─ Path: %s\n", ffmpegPath)

	// Get FFmpeg version
	cmd := exec.Command("ffmpeg", "-version")
	output, err := cmd.Output()
	if err == nil {
		lines := strings.Split(string(output), "\n")
		if len(lines) > 0 {
			version := strings.TrimSpace(lines[0])
			fmt.Printf("  └─ Version: %s\n", version)
		}
	}

	// Check for hardware acceleration support
	cmd = exec.Command("ffmpeg", "-hwaccels")
	output, err = cmd.Output()
	if err == nil {
		hwaccels := strings.Split(string(output), "\n")
		fmt.Println("  └─ Hardware Acceleration:")
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
				fmt.Printf("     %s %s ✓\n", prefix, hwaccel)
			} else {
				fmt.Printf("     %s %s\n", prefix, hwaccel)
			}
		}
	}
}

func detectVulkan() {
	// Try to detect Vulkan using vulkaninfo
	vulkanPath, err := exec.LookPath("vulkaninfo")
	if err != nil {
		fmt.Println("Vulkan Status: ⚠ Cannot detect (vulkaninfo not found)")
		fmt.Println("  Install vulkan-tools to check Vulkan capabilities")
		return
	}

	fmt.Println("Vulkan Status: ✓ Tools Available")
	fmt.Printf("  └─ Path: %s\n", vulkanPath)

	// Try to get basic Vulkan info
	cmd := exec.Command("vulkaninfo", "--summary")
	output, err := cmd.Output()
	if err != nil {
		fmt.Println("  └─ Device Detection: Failed (no Vulkan devices found)")
		return
	}

	// Parse vulkaninfo output for device information
	lines := strings.Split(string(output), "\n")
	deviceFound := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "deviceName") || strings.Contains(line, "GPU") {
			if !deviceFound {
				fmt.Println("  └─ Devices:")
				deviceFound = true
			}
			fmt.Printf("     ├─ %s\n", line)
		}
	}

	if !deviceFound {
		fmt.Println("  └─ No Vulkan-capable devices detected")
	}
}

func detectSystem() {
	fmt.Println("Environment:")
	fmt.Printf("├─ OS: %s\n", runtime.GOOS)
	fmt.Printf("├─ Architecture: %s\n", runtime.GOARCH)
	fmt.Printf("├─ CPUs: %d\n", runtime.NumCPU())

	// Get hostname
	hostname, err := os.Hostname()
	if err == nil {
		fmt.Printf("└─ Hostname: %s\n", hostname)
	}
}
