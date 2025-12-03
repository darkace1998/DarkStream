package models

// VulkanCapabilities describes the Vulkan capabilities of a GPU device.
type VulkanCapabilities struct {
	Supported           bool         `json:"supported"`
	Device              VulkanDevice `json:"device"`
	APIVersion          string       `json:"api_version"`
	SupportedExtensions []string     `json:"supported_extensions"`
	CanEncode           bool         `json:"can_encode"`
	CanDecode           bool         `json:"can_decode"`
	MaxWidth            uint32       `json:"max_width"`
	MaxHeight           uint32       `json:"max_height"`
	PreferredFormat     string       `json:"preferred_format"`
}

// VulkanDeviceList contains a list of available Vulkan devices and the default device.
type VulkanDeviceList struct {
	Devices       []VulkanDevice `json:"devices"`
	DefaultDevice string         `json:"default_device"`
}
