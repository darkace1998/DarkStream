//go:build !vulkan

package converter

import (
	"log/slog"

	"github.com/darkace1998/video-converter-common/constants"
	"github.com/darkace1998/video-converter-common/models"
)

// VulkanCapabilities contains detected Vulkan GPU capabilities.
// This is a stub implementation when Vulkan support is not compiled in.
type VulkanCapabilities struct {
	Supported           bool
	Device              models.VulkanDevice
	APIVersion          string
	SupportedExtensions []string
	CanEncode           bool
	CanDecode           bool
	MaxWidth            uint32
	MaxHeight           uint32
	PreferredFormat     string
}

// VulkanDetector handles detection and selection of Vulkan-capable GPUs.
// This is a stub implementation when Vulkan support is not compiled in.
type VulkanDetector struct {
	preferredDevice  string
	enableValidation bool
}

// NewVulkanDetector creates a new VulkanDetector instance.
// This is a stub implementation when Vulkan support is not compiled in.
func NewVulkanDetector(preferredDevice string) *VulkanDetector {
	slog.Info("Vulkan support not compiled in, using stub implementation")
	return &VulkanDetector{
		preferredDevice:  preferredDevice,
		enableValidation: false,
	}
}

// SetValidation enables or disables Vulkan validation layers.
// This is a stub implementation when Vulkan support is not compiled in.
func (vd *VulkanDetector) SetValidation(enabled bool) {
	vd.enableValidation = enabled
}

// DetectGPUs detects available Vulkan-capable GPUs.
// This is a stub implementation that returns no devices.
func (vd *VulkanDetector) DetectGPUs() ([]models.VulkanDevice, error) {
	slog.Info("Vulkan not available - returning empty device list")
	return nil, nil
}

// SelectDevice selects a GPU for video conversion.
// This is a stub implementation that returns an empty string.
func (vd *VulkanDetector) SelectDevice(_ []models.VulkanDevice) string {
	return ""
}

// IsVulkanAvailable returns whether Vulkan is available.
// This is a stub implementation that always returns false.
func (vd *VulkanDetector) IsVulkanAvailable() bool {
	return false
}

// DetectVulkanCapabilities detects and returns Vulkan GPU capabilities.
// This is a stub implementation that returns unsupported capabilities.
func (vd *VulkanDetector) DetectVulkanCapabilities() (*VulkanCapabilities, error) {
	slog.Info("Vulkan not available - returning unsupported capabilities")
	return &VulkanCapabilities{
		Supported:       false,
		CanEncode:       false,
		CanDecode:       false,
		MaxWidth:        0,
		MaxHeight:       0,
		PreferredFormat: constants.CodecH264,
	}, nil
}
