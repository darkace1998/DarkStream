package commands

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// Detect detects GPU and Vulkan capabilities
func Detect(args []string) {
	fmt.Println("🖥️  GPU / Vulkan Detection")
	fmt.Println("")

	// Check if vulkaninfo is available
	vulkanAvailable := checkVulkan()
	
	if vulkanAvailable {
		fmt.Println("Vulkan Status: ✓ Available")
		displayVulkanInfo()
	} else {
		fmt.Println("Vulkan Status: ✗ Not Available")
		fmt.Println("")
		fmt.Println("To enable Vulkan support:")
		fmt.Println("  - Install Vulkan SDK")
		fmt.Println("  - Install GPU drivers with Vulkan support")
		fmt.Println("  - Install vulkan-tools package")
	}
	
	fmt.Println("")
	fmt.Println("Environment:")
	fmt.Printf("├─ OS: %s\n", runtime.GOOS)
	fmt.Printf("├─ Architecture: %s\n", runtime.GOARCH)
	fmt.Printf("└─ CPUs: %d\n", runtime.NumCPU())
}

// checkVulkan checks if Vulkan is available on the system
func checkVulkan() bool {
	_, err := exec.LookPath("vulkaninfo")
	return err == nil
}

// displayVulkanInfo displays Vulkan device information
func displayVulkanInfo() {
	fmt.Println("")
	
	cmd := exec.Command("vulkaninfo", "--summary")
	output, err := cmd.CombinedOutput()
	
	if err != nil {
		fmt.Println("Could not retrieve detailed Vulkan information")
		fmt.Println("Run 'vulkaninfo' manually for detailed GPU information")
		return
	}
	
	// Parse and display basic info
	lines := strings.Split(string(output), "\n")
	fmt.Println("Devices:")
	
	deviceFound := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		
		// Look for device name
		if strings.Contains(line, "deviceName") || strings.Contains(line, "GPU") {
			if strings.Contains(line, "=") {
				parts := strings.Split(line, "=")
				if len(parts) > 1 {
					deviceName := strings.TrimSpace(parts[1])
					fmt.Printf("├─ %s\n", deviceName)
					deviceFound = true
				}
			}
		}
		
		// Look for device type
		if strings.Contains(line, "deviceType") {
			if strings.Contains(line, "=") {
				parts := strings.Split(line, "=")
				if len(parts) > 1 {
					deviceType := strings.TrimSpace(parts[1])
					fmt.Printf("│  ├─ Type: %s\n", deviceType)
				}
			}
		}
		
		// Look for driver version
		if strings.Contains(line, "driverVersion") {
			if strings.Contains(line, "=") {
				parts := strings.Split(line, "=")
				if len(parts) > 1 {
					driverVersion := strings.TrimSpace(parts[1])
					fmt.Printf("│  └─ Driver Version: %s\n", driverVersion)
				}
			}
		}
	}
	
	if !deviceFound {
		fmt.Println("├─ No GPU devices detected")
		fmt.Println("│  Run 'vulkaninfo' for detailed information")
	}
}
