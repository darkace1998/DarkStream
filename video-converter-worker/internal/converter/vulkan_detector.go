//go:build vulkan

package converter

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"

	vk "github.com/darkace1998/golang-vulkan-api"
	"github.com/darkace1998/video-converter-common/constants"
	"github.com/darkace1998/video-converter-common/models"
)

var (
	vulkanInitOnce sync.Once
	vulkanInitErr  error
)

const (
	// VulkanValidationLayer is the standard Khronos validation layer
	VulkanValidationLayer = "VK_LAYER_KHRONOS_validation"
)

// VulkanDetector handles detection and selection of Vulkan-capable GPUs
type VulkanDetector struct {
	preferredDevice     string
	enableValidation    bool
	validationLayersSet bool
}

// initVulkan initializes the Vulkan library once
func initVulkan() error {
	vulkanInitOnce.Do(func() {
		// The Golang-Vulkan-api doesn't require explicit initialization
		// as it handles CGO binding automatically. Return nil for success.
		vulkanInitErr = nil
	})
	return vulkanInitErr
}

// createVulkanInstance creates a Vulkan instance for device enumeration
func (vd *VulkanDetector) createVulkanInstance() (vk.Instance, error) {
	appInfo := &vk.ApplicationInfo{
		ApplicationName:    "DarkStream Video Converter",
		ApplicationVersion: vk.MakeVersion(1, 0, 0),
		EngineName:         "No Engine",
		EngineVersion:      vk.MakeVersion(1, 0, 0),
		APIVersion:         vk.Version13,
	}

	instanceCreateInfo := &vk.InstanceCreateInfo{
		ApplicationInfo: appInfo,
	}

	// Add validation layers if enabled
	if vd.enableValidation {
		validationLayers := []string{VulkanValidationLayer}

		// Check if validation layers are available
		availableLayers, err := vk.EnumerateInstanceLayerProperties()
		if err == nil {
			layerFound := false
			for _, layer := range availableLayers {
				if layer.LayerName == VulkanValidationLayer {
					layerFound = true
					break
				}
			}

			if layerFound {
				instanceCreateInfo.EnabledLayerNames = validationLayers
				vd.validationLayersSet = true
				slog.Info("Vulkan validation layers enabled")
			} else {
				slog.Warn("Vulkan validation layers requested but not available")
			}
		} else {
			slog.Warn("Failed to enumerate Vulkan layers", "error", err)
		}
	}

	instance, err := vk.CreateInstance(instanceCreateInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to create Vulkan instance: %w", err)
	}

	return instance, nil
}

// NewVulkanDetector creates a new VulkanDetector instance
func NewVulkanDetector(preferredDevice string) *VulkanDetector {
	return &VulkanDetector{
		preferredDevice:     preferredDevice,
		enableValidation:    false,
		validationLayersSet: false,
	}
}

// NewVulkanDetectorWithValidation creates a new VulkanDetector with validation layers
func NewVulkanDetectorWithValidation(preferredDevice string, enableValidation bool) *VulkanDetector {
	return &VulkanDetector{
		preferredDevice:     preferredDevice,
		enableValidation:    enableValidation,
		validationLayersSet: false,
	}
}

// VulkanCapabilities contains detected Vulkan GPU capabilities
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

// DetectVulkanCapabilities detects and returns Vulkan GPU capabilities
func (vd *VulkanDetector) DetectVulkanCapabilities() (*VulkanCapabilities, error) {
	caps := &VulkanCapabilities{
		Supported:       false,
		CanEncode:       false,
		CanDecode:       false,
		MaxWidth:        0,
		MaxHeight:       0,
		PreferredFormat: constants.CodecH264,
	}

	// Initialize Vulkan (only happens once)
	initErr := initVulkan()
	if initErr != nil {
		slog.Warn("Failed to initialize Vulkan, falling back to CPU encoding", "error", initErr)
		return caps, nil
	}

	// Create Vulkan instance (reused for both device listing and capability query)
	instance, err := vd.createVulkanInstance()
	if err != nil {
		slog.Warn("Failed to create Vulkan instance, falling back to CPU encoding", "error", err)
		return caps, nil
	}
	defer vk.DestroyInstance(instance)

	// Detect Vulkan devices
	devices, err := vd.listVulkanDevicesWithInstance(instance)
	if err != nil {
		slog.Warn("Failed to list Vulkan devices, falling back to CPU encoding", "error", err)
		// Return capabilities with Supported=false but no error (graceful fallback)
		return caps, nil
	}

	if len(devices) == 0 {
		slog.Warn("No Vulkan devices found, falling back to CPU encoding")
		return caps, nil
	}

	// Select device
	device := vd.selectDevice(devices)
	if !device.Available {
		slog.Warn("Selected Vulkan device is not available, falling back to CPU encoding",
			"device", device.Name,
		)
		return caps, nil
	}

	caps.Device = device
	caps.Supported = true

	// Query detailed capabilities from the selected device (reusing the instance)
	deviceCaps, err := vd.queryDeviceCapabilitiesWithInstance(instance, device)
	if err != nil {
		slog.Warn("Failed to query device capabilities, using defaults",
			"device", device.Name,
			"error", err,
		)
		// Set reasonable defaults
		caps.APIVersion = "1.0"
		caps.CanEncode = true
		caps.CanDecode = true
		caps.MaxWidth = 3840
		caps.MaxHeight = 2160
	} else {
		caps.APIVersion = deviceCaps.APIVersion
		caps.SupportedExtensions = deviceCaps.SupportedExtensions
		caps.CanEncode = deviceCaps.CanEncode
		caps.CanDecode = deviceCaps.CanDecode
		caps.MaxWidth = deviceCaps.MaxWidth
		caps.MaxHeight = deviceCaps.MaxHeight
	}

	slog.Info("Vulkan device detected",
		"name", device.Name,
		"type", device.Type,
		"driver_version", device.DriverVersion,
		"api_version", caps.APIVersion,
		"can_encode", caps.CanEncode,
		"can_decode", caps.CanDecode,
		"max_resolution", fmt.Sprintf("%dx%d", caps.MaxWidth, caps.MaxHeight),
	)

	return caps, nil
}

// queryDeviceCapabilitiesWithInstance queries detailed capabilities from a Vulkan device using a provided instance
func (vd *VulkanDetector) queryDeviceCapabilitiesWithInstance(instance vk.Instance, device models.VulkanDevice) (*VulkanCapabilities, error) {
	caps := &VulkanCapabilities{
		SupportedExtensions: []string{},
		CanEncode:           false,
		CanDecode:           false,
		MaxWidth:            3840,
		MaxHeight:           2160,
	}

	// Enumerate physical devices to find our device
	physicalDevices, err := vk.EnumeratePhysicalDevices(instance)
	if err != nil {
		return nil, fmt.Errorf("failed to enumerate physical devices: %w", err)
	}

	// Find the matching physical device
	var selectedPhysicalDevice vk.PhysicalDevice
	var found bool
	for _, physicalDevice := range physicalDevices {
		props := vk.GetPhysicalDeviceProperties(physicalDevice)

		if props.DeviceID == device.DeviceID && props.VendorID == device.VendorID {
			selectedPhysicalDevice = physicalDevice
			found = true

			// Get API version
			caps.APIVersion = fmt.Sprintf("%d.%d.%d",
				props.APIVersion.Major(),
				props.APIVersion.Minor(),
				props.APIVersion.Patch(),
			)

			// Set conservative video resolution limits based on device type.
			// Note: MaxImageDimension2D is for textures, not video encoding/decoding.
			// True video limits would require querying VK_KHR_video_queue extensions.
			// We use conservative defaults based on typical device capabilities.
			switch device.Type {
			case constants.VulkanDeviceTypeDiscrete:
				// Discrete GPUs typically support up to 8K
				caps.MaxWidth = 7680
				caps.MaxHeight = 4320
			case constants.VulkanDeviceTypeIntegrated:
				// Integrated GPUs typically support up to 4K
				caps.MaxWidth = 3840
				caps.MaxHeight = 2160
			default:
				// Conservative defaults for other device types
				caps.MaxWidth = 1920
				caps.MaxHeight = 1080
			}

			break
		}
	}

	if !found {
		return nil, fmt.Errorf("physical device not found")
	}

	// Query device extensions
	extensions, err := vk.EnumerateDeviceExtensionProperties(selectedPhysicalDevice, "")
	if err == nil && len(extensions) > 0 {
		for _, ext := range extensions {
			extName := ext.ExtensionName
			caps.SupportedExtensions = append(caps.SupportedExtensions, extName)

			// Check for video encoding/decoding extensions using exact match
			if extName == "VK_KHR_video_encode_queue" {
				caps.CanEncode = true
			}
			if extName == "VK_KHR_video_decode_queue" {
				caps.CanDecode = true
			}
		}
	}

	// If no video extensions found, assume basic support for encoding/decoding
	// (FFmpeg Vulkan filters work even without these specific extensions)
	if !caps.CanEncode && !caps.CanDecode {
		caps.CanEncode = true
		caps.CanDecode = true
		slog.Debug("No specific video extensions found, assuming basic Vulkan support",
			"device", device.Name,
		)
	}

	return caps, nil
}

// listVulkanDevicesWithInstance enumerates available Vulkan devices using a provided instance
func (vd *VulkanDetector) listVulkanDevicesWithInstance(instance vk.Instance) ([]models.VulkanDevice, error) {
	// Enumerate physical devices
	physicalDevices, err := vk.EnumeratePhysicalDevices(instance)
	if err != nil {
		return nil, fmt.Errorf("failed to enumerate physical devices: %w", err)
	}

	if len(physicalDevices) == 0 {
		return nil, fmt.Errorf("no Vulkan-capable devices found")
	}

	// Extract device information
	devices := make([]models.VulkanDevice, 0, len(physicalDevices))
	for _, physicalDevice := range physicalDevices {
		props := vk.GetPhysicalDeviceProperties(physicalDevice)

		// Convert device name from C string
		deviceName := props.DeviceName

		// Map Vulkan device type to our constants
		deviceType := mapVulkanDeviceType(props.DeviceType)

		// Format driver version.
		// Note: NVIDIA uses a vendor-specific encoding for driver version.
		// Other vendors typically use the standard Vulkan version format.
		var driverVersion string
		if props.VendorID == 0x10DE { // NVIDIA
			// NVIDIA encoding: bits 31-22: major, 21-14: minor, 13-6: patch, 5-0: build
			major := (props.DriverVersion >> 22) & 0x3FF
			minor := (props.DriverVersion >> 14) & 0xFF
			patch := (props.DriverVersion >> 6) & 0xFF
			build := props.DriverVersion & 0x3F
			driverVersion = fmt.Sprintf("%d.%d.%d.%d", major, minor, patch, build)
		} else {
			// Standard Vulkan version format for other vendors
			driverVersion = fmt.Sprintf("%d.%d.%d",
				props.APIVersion.Major(),
				props.APIVersion.Minor(),
				props.APIVersion.Patch(),
			)
		}

		// Check queue family support for compute/graphics
		queueFamilies := vk.GetPhysicalDeviceQueueFamilyProperties(physicalDevice)

		// Check if device supports graphics and compute operations
		hasGraphics := false
		hasCompute := false
		for _, qf := range queueFamilies {
			queueFlags := qf.QueueFlags
			if queueFlags&vk.QueueGraphicsBit != 0 {
				hasGraphics = true
			}
			if queueFlags&vk.QueueComputeBit != 0 {
				hasCompute = true
			}
		}

		// Device is available if it supports both graphics and compute
		available := hasGraphics && hasCompute

		device := models.VulkanDevice{
			Name:          deviceName,
			Type:          deviceType,
			DeviceID:      props.DeviceID,
			VendorID:      props.VendorID,
			DriverVersion: driverVersion,
			Available:     available,
		}

		devices = append(devices, device)

		slog.Debug("Found Vulkan device",
			"name", deviceName,
			"type", deviceType,
			"vendor_id", fmt.Sprintf("0x%04X", props.VendorID),
			"device_id", fmt.Sprintf("0x%04X", props.DeviceID),
			"available", available,
		)
	}

	return devices, nil
}

// mapVulkanDeviceType converts Vulkan device type to our constant
func mapVulkanDeviceType(vkType vk.PhysicalDeviceType) string {
	switch vkType {
	case 1: // VK_PHYSICAL_DEVICE_TYPE_DISCRETE_GPU
		return constants.VulkanDeviceTypeDiscrete
	case 2: // VK_PHYSICAL_DEVICE_TYPE_INTEGRATED_GPU
		return constants.VulkanDeviceTypeIntegrated
	case 3: // VK_PHYSICAL_DEVICE_TYPE_VIRTUAL_GPU
		return constants.VulkanDeviceTypeVirtual
	case 4: // VK_PHYSICAL_DEVICE_TYPE_CPU
		return constants.VulkanDeviceTypeCPU
	default:
		return constants.VulkanDeviceTypeIntegrated // fallback
	}
}

// selectDevice selects the appropriate Vulkan device based on preferences
func (vd *VulkanDetector) selectDevice(devices []models.VulkanDevice) models.VulkanDevice {
	return selectDeviceWithPreference(devices, vd.preferredDevice)
}

// selectDeviceWithPreference selects a device based on the given preference
// This is a pure function that doesn't modify state, avoiding race conditions
func selectDeviceWithPreference(devices []models.VulkanDevice, preference string) models.VulkanDevice {
	// Use auto-selection logic for "auto" or empty preference, or as fallback
	autoSelect := preference == "auto" || preference == ""

	if !autoSelect {
		// Find preferred device by name (case-insensitive)
		preferredLower := strings.ToLower(preference)
		for _, dev := range devices {
			if strings.Contains(strings.ToLower(dev.Name), preferredLower) && dev.Available {
				slog.Info("Selected preferred device", "device", dev.Name, "preferred", preference)
				return dev
			}
		}

		// If preferred device not found available, check unavailable devices
		for _, dev := range devices {
			if strings.Contains(strings.ToLower(dev.Name), preferredLower) {
				slog.Warn("Preferred device found but not available",
					"device", dev.Name,
					"preferred", preference,
				)
			}
		}

		// Fallback to auto-selection
		slog.Warn("Preferred Vulkan device not found, falling back to auto-selection",
			"preferred_device", preference,
		)
	}

	// Auto-selection logic: prioritize discrete GPUs, then integrated, then others
	// First, try to find an available discrete GPU
	for _, dev := range devices {
		if dev.Available && dev.Type == constants.VulkanDeviceTypeDiscrete {
			slog.Info("Auto-selected discrete GPU", "device", dev.Name)
			return dev
		}
	}

	// Next, try integrated GPU
	for _, dev := range devices {
		if dev.Available && dev.Type == constants.VulkanDeviceTypeIntegrated {
			slog.Info("Auto-selected integrated GPU", "device", dev.Name)
			return dev
		}
	}

	// Finally, select any available device
	for _, dev := range devices {
		if dev.Available {
			slog.Info("Auto-selected available device", "device", dev.Name, "type", dev.Type)
			return dev
		}
	}

	// If no available devices found, return first device anyway
	if len(devices) > 0 {
		slog.Warn("No available devices found, using first device", "device", devices[0].Name)
		return devices[0]
	}
	// Return empty device if no devices at all (shouldn't happen due to check in DetectVulkanCapabilities)
	return models.VulkanDevice{}
}
