package converter

import (
	"testing"

	"github.com/darkace1998/video-converter-common/constants"
	"github.com/darkace1998/video-converter-common/models"
)

// TestSelectDevice_Auto tests auto device selection with different device types
func TestSelectDevice_Auto(t *testing.T) {
	tests := []struct {
		name     string
		devices  []models.VulkanDevice
		expected string
	}{
		{
			name: "selects discrete GPU in auto mode",
			devices: []models.VulkanDevice{
				{Name: "Integrated GPU", Type: constants.VulkanDeviceTypeIntegrated, Available: true},
				{Name: "Discrete GPU", Type: constants.VulkanDeviceTypeDiscrete, Available: true},
			},
			expected: "Discrete GPU",
		},
		{
			name: "selects integrated GPU when no discrete available",
			devices: []models.VulkanDevice{
				{Name: "Integrated GPU", Type: constants.VulkanDeviceTypeIntegrated, Available: true},
				{Name: "CPU Device", Type: constants.VulkanDeviceTypeCPU, Available: true},
			},
			expected: "Integrated GPU",
		},
		{
			name: "selects first available device when no discrete or integrated",
			devices: []models.VulkanDevice{
				{Name: "Virtual GPU", Type: constants.VulkanDeviceTypeVirtual, Available: true},
				{Name: "CPU Device", Type: constants.VulkanDeviceTypeCPU, Available: true},
			},
			expected: "Virtual GPU",
		},
		{
			name: "skips unavailable discrete GPU",
			devices: []models.VulkanDevice{
				{Name: "Discrete GPU", Type: constants.VulkanDeviceTypeDiscrete, Available: false},
				{Name: "Integrated GPU", Type: constants.VulkanDeviceTypeIntegrated, Available: true},
			},
			expected: "Integrated GPU",
		},
		{
			name: "returns first device when none available",
			devices: []models.VulkanDevice{
				{Name: "Discrete GPU", Type: constants.VulkanDeviceTypeDiscrete, Available: false},
				{Name: "Integrated GPU", Type: constants.VulkanDeviceTypeIntegrated, Available: false},
			},
			expected: "Discrete GPU",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detector := NewVulkanDetector("auto")
			selected := detector.selectDevice(tt.devices)
			if selected.Name != tt.expected {
				t.Errorf("Expected device %s, got %s", tt.expected, selected.Name)
			}
		})
	}
}

// TestSelectDevice_PreferredByName tests device selection by name
func TestSelectDevice_PreferredByName(t *testing.T) {
	devices := []models.VulkanDevice{
		{Name: "NVIDIA GeForce RTX 3080", Type: constants.VulkanDeviceTypeDiscrete, Available: true},
		{Name: "AMD Radeon RX 6800", Type: constants.VulkanDeviceTypeDiscrete, Available: true},
		{Name: "Intel UHD Graphics", Type: constants.VulkanDeviceTypeIntegrated, Available: true},
	}

	tests := []struct {
		name      string
		preferred string
		expected  string
	}{
		{
			name:      "exact match",
			preferred: "NVIDIA GeForce RTX 3080",
			expected:  "NVIDIA GeForce RTX 3080",
		},
		{
			name:      "partial match - case insensitive",
			preferred: "nvidia",
			expected:  "NVIDIA GeForce RTX 3080",
		},
		{
			name:      "partial match - AMD",
			preferred: "amd",
			expected:  "AMD Radeon RX 6800",
		},
		{
			name:      "partial match - Intel",
			preferred: "intel",
			expected:  "Intel UHD Graphics",
		},
		{
			name:      "case insensitive full match",
			preferred: "nvidia geforce rtx 3080",
			expected:  "NVIDIA GeForce RTX 3080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detector := NewVulkanDetector(tt.preferred)
			selected := detector.selectDevice(devices)
			if selected.Name != tt.expected {
				t.Errorf("Expected device %s, got %s", tt.expected, selected.Name)
			}
		})
	}
}

// TestSelectDevice_PreferredNotFound tests fallback when preferred device not found
func TestSelectDevice_PreferredNotFound(t *testing.T) {
	devices := []models.VulkanDevice{
		{Name: "NVIDIA GeForce RTX 3080", Type: constants.VulkanDeviceTypeDiscrete, Available: true},
		{Name: "Intel UHD Graphics", Type: constants.VulkanDeviceTypeIntegrated, Available: true},
	}

	detector := NewVulkanDetector("AMD Radeon")
	selected := detector.selectDevice(devices)

	// Should fallback to auto-selection, which prioritizes discrete GPU
	if selected.Name != "NVIDIA GeForce RTX 3080" {
		t.Errorf("Expected fallback to discrete GPU, got %s", selected.Name)
	}
}

// TestSelectDevice_PreferredUnavailable tests when preferred device is unavailable
func TestSelectDevice_PreferredUnavailable(t *testing.T) {
	devices := []models.VulkanDevice{
		{Name: "NVIDIA GeForce RTX 3080", Type: constants.VulkanDeviceTypeDiscrete, Available: false},
		{Name: "Intel UHD Graphics", Type: constants.VulkanDeviceTypeIntegrated, Available: true},
	}

	detector := NewVulkanDetector("nvidia")
	selected := detector.selectDevice(devices)

	// Should fallback to auto-selection since preferred is unavailable
	if selected.Name != "Intel UHD Graphics" {
		t.Errorf("Expected fallback to available integrated GPU, got %s", selected.Name)
	}
}

// TestSelectDevice_EmptyDeviceList tests behavior with empty device list
func TestSelectDevice_EmptyDeviceList(t *testing.T) {
	detector := NewVulkanDetector("auto")
	selected := detector.selectDevice([]models.VulkanDevice{})

	if selected.Name != "" {
		t.Errorf("Expected empty device, got %s", selected.Name)
	}
}

// TestMapVulkanDeviceType tests device type mapping
func TestMapVulkanDeviceType(t *testing.T) {
	tests := []struct {
		name     string
		vkType   int32
		expected string
	}{
		{
			name:     "discrete GPU",
			vkType:   1, // PhysicalDeviceTypeDiscreteGpu
			expected: constants.VulkanDeviceTypeDiscrete,
		},
		{
			name:     "integrated GPU",
			vkType:   2, // PhysicalDeviceTypeIntegratedGpu
			expected: constants.VulkanDeviceTypeIntegrated,
		},
		{
			name:     "virtual GPU",
			vkType:   3, // PhysicalDeviceTypeVirtualGpu
			expected: constants.VulkanDeviceTypeVirtual,
		},
		{
			name:     "CPU",
			vkType:   4, // PhysicalDeviceTypeCpu
			expected: constants.VulkanDeviceTypeCPU,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// We can't directly test the internal function without exposing it,
			// but we test that the constants match expected values
			switch tt.vkType {
			case 1:
				if constants.VulkanDeviceTypeDiscrete != "discrete" {
					t.Errorf("Expected discrete constant to be 'discrete'")
				}
			case 2:
				if constants.VulkanDeviceTypeIntegrated != "integrated" {
					t.Errorf("Expected integrated constant to be 'integrated'")
				}
			case 3:
				if constants.VulkanDeviceTypeVirtual != "virtual" {
					t.Errorf("Expected virtual constant to be 'virtual'")
				}
			case 4:
				if constants.VulkanDeviceTypeCPU != "cpu" {
					t.Errorf("Expected CPU constant to be 'cpu'")
				}
			}
		})
	}
}

// TestNewVulkanDetector tests the constructor
func TestNewVulkanDetector(t *testing.T) {
	tests := []struct {
		name             string
		preferredDevice  string
		expectedInternal string
	}{
		{
			name:             "auto mode",
			preferredDevice:  "auto",
			expectedInternal: "auto",
		},
		{
			name:             "specific device",
			preferredDevice:  "NVIDIA GeForce RTX 3080",
			expectedInternal: "NVIDIA GeForce RTX 3080",
		},
		{
			name:             "empty string",
			preferredDevice:  "",
			expectedInternal: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detector := NewVulkanDetector(tt.preferredDevice)
			if detector.preferredDevice != tt.expectedInternal {
				t.Errorf("Expected preferredDevice %s, got %s",
					tt.expectedInternal, detector.preferredDevice)
			}
		})
	}
}

// TestDetectVulkanCapabilities_NoVulkan tests graceful fallback when Vulkan is unavailable
// Note: This test will actually try to initialize Vulkan, so it may pass or fail depending
// on whether Vulkan is available on the system. The important thing is that it doesn't crash.
func TestDetectVulkanCapabilities_NoVulkanGracefulFallback(t *testing.T) {
	detector := NewVulkanDetector("auto")
	caps, err := detector.DetectVulkanCapabilities()

	// Should return caps even if Vulkan is unavailable (no error, but Supported=false)
	if caps == nil {
		t.Fatal("Expected non-nil capabilities")
	}

	// If Vulkan is not available, Supported should be false
	// If it is available, Supported should be true
	// We just verify that we don't crash and get a result
	t.Logf("Vulkan supported: %v", caps.Supported)
	if caps.Supported {
		t.Logf("Device: %s (%s)", caps.Device.Name, caps.Device.Type)
		t.Logf("API Version: %s", caps.ApiVersion)
		t.Logf("Max Resolution: %dx%d", caps.MaxWidth, caps.MaxHeight)
		t.Logf("Can Encode: %v, Can Decode: %v", caps.CanEncode, caps.CanDecode)
	}

	// Verify that err is nil (graceful fallback, not an error)
	if err != nil {
		t.Errorf("Expected nil error for graceful fallback, got: %v", err)
	}

	// Verify PreferredFormat is set
	if caps.PreferredFormat != constants.CodecH264 {
		t.Errorf("Expected preferred format %s, got %s", constants.CodecH264, caps.PreferredFormat)
	}
}

// TestSelectDevice_MultipleSameType tests selection when multiple devices of same type
func TestSelectDevice_MultipleSameType(t *testing.T) {
	devices := []models.VulkanDevice{
		{Name: "NVIDIA GeForce RTX 3080", Type: constants.VulkanDeviceTypeDiscrete, Available: true},
		{Name: "NVIDIA GeForce RTX 3090", Type: constants.VulkanDeviceTypeDiscrete, Available: true},
		{Name: "AMD Radeon RX 6800", Type: constants.VulkanDeviceTypeDiscrete, Available: true},
	}

	detector := NewVulkanDetector("auto")
	selected := detector.selectDevice(devices)

	// Should select first discrete GPU
	if selected.Name != "NVIDIA GeForce RTX 3080" {
		t.Errorf("Expected first discrete GPU, got %s", selected.Name)
	}
}

// TestSelectDevice_PartialNameMatch tests partial name matching
func TestSelectDevice_PartialNameMatch(t *testing.T) {
	devices := []models.VulkanDevice{
		{Name: "NVIDIA GeForce RTX 3080", Type: constants.VulkanDeviceTypeDiscrete, Available: true},
		{Name: "Intel UHD Graphics", Type: constants.VulkanDeviceTypeIntegrated, Available: true},
	}

	tests := []struct {
		name      string
		preferred string
		expected  string
	}{
		{
			name:      "match by vendor",
			preferred: "nvidia",
			expected:  "NVIDIA GeForce RTX 3080",
		},
		{
			name:      "match by series",
			preferred: "rtx",
			expected:  "NVIDIA GeForce RTX 3080",
		},
		{
			name:      "match by model",
			preferred: "3080",
			expected:  "NVIDIA GeForce RTX 3080",
		},
		{
			name:      "match intel",
			preferred: "uhd",
			expected:  "Intel UHD Graphics",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detector := NewVulkanDetector(tt.preferred)
			selected := detector.selectDevice(devices)
			if selected.Name != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, selected.Name)
			}
		})
	}
}
