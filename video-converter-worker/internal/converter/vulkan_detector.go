package converter

import (
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/darkace1998/video-converter-common/models"
)

type VulkanDetector struct {
	preferredDevice string
}

func NewVulkanDetector(preferredDevice string) *VulkanDetector {
	return &VulkanDetector{
		preferredDevice: preferredDevice,
	}
}

func (vd *VulkanDetector) DetectVulkanCapabilities() (*models.VulkanCapabilities, error) {
	caps := &models.VulkanCapabilities{
		Supported:       true,
		CanEncode:       true,
		CanDecode:       true,
		MaxWidth:        3840,
		MaxHeight:       2160,
		PreferredFormat: "h264",
	}

	// Detect Vulkan devices
	devices, err := vd.listVulkanDevices()
	if err != nil {
		slog.Error("Failed to list Vulkan devices", "error", err)
		caps.Supported = false
		return caps, err
	}

	if len(devices) == 0 {
		caps.Supported = false
		return caps, fmt.Errorf("no Vulkan devices found")
	}

	// Select device
	device := vd.selectDevice(devices)
	caps.Device = device

	slog.Info("Vulkan device detected",
		"name", device.Name,
		"type", device.Type,
		"driver_version", device.DriverVersion,
	)

	return caps, nil
}

func (vd *VulkanDetector) listVulkanDevices() ([]models.VulkanDevice, error) {
	// Try to detect GPU using vulkaninfo or similar tool
	// For now, we'll use a simple approach with lspci or nvidia-smi

	devices := []models.VulkanDevice{}

	// Try nvidia-smi first for NVIDIA GPUs
	if gpuName := vd.detectNvidiaGPU(); gpuName != "" {
		devices = append(devices, models.VulkanDevice{
			Name:          gpuName,
			Type:          "discrete",
			DeviceID:      0x0000,
			VendorID:      0x10DE, // NVIDIA
			DriverVersion: "unknown",
			Available:     true,
		})
	}

	// Fallback: try lspci for any GPU
	if len(devices) == 0 {
		if gpuName := vd.detectGPUViaPCI(); gpuName != "" {
			devices = append(devices, models.VulkanDevice{
				Name:          gpuName,
				Type:          "discrete",
				DeviceID:      0x0000,
				VendorID:      0x0000,
				DriverVersion: "unknown",
				Available:     true,
			})
		}
	}

	// If still no devices found, create a generic CPU fallback
	if len(devices) == 0 {
		slog.Warn("No GPU detected, will use CPU fallback")
		devices = append(devices, models.VulkanDevice{
			Name:          "CPU (Software Rendering)",
			Type:          "cpu",
			DeviceID:      0x0000,
			VendorID:      0x0000,
			DriverVersion: "N/A",
			Available:     true,
		})
	}

	return devices, nil
}

func (vd *VulkanDetector) detectNvidiaGPU() string {
	cmd := exec.Command("nvidia-smi", "--query-gpu=name", "--format=csv,noheader")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	name := strings.TrimSpace(string(output))
	if name != "" {
		return name
	}

	return ""
}

func (vd *VulkanDetector) detectGPUViaPCI() string {
	cmd := exec.Command("lspci")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "vga") || strings.Contains(lower, "3d") {
			// Extract GPU name from the line
			parts := strings.Split(line, ":")
			if len(parts) >= 3 {
				gpuName := strings.TrimSpace(strings.Join(parts[2:], ":"))
				if gpuName != "" {
					return gpuName
				}
			}
		}
	}

	return ""
}

func (vd *VulkanDetector) selectDevice(devices []models.VulkanDevice) models.VulkanDevice {
	if vd.preferredDevice == "auto" || vd.preferredDevice == "" {
		// Select first available device
		for _, dev := range devices {
			if dev.Available {
				return dev
			}
		}
	}

	// Find preferred device
	for _, dev := range devices {
		if dev.Name == vd.preferredDevice && dev.Available {
			return dev
		}
	}

	// Fallback to first device
	return devices[0]
}
