package models

type VulkanCapabilities struct {
	Supported           bool           `json:"supported"`
	Device              VulkanDevice   `json:"device"`
	ApiVersion          string         `json:"api_version"`
	SupportedExtensions []string       `json:"supported_extensions"`
	CanEncode           bool           `json:"can_encode"`
	CanDecode           bool           `json:"can_decode"`
	MaxWidth            uint32         `json:"max_width"`
	MaxHeight           uint32         `json:"max_height"`
	PreferredFormat     string         `json:"preferred_format"`
}

type VulkanDeviceList struct {
	Devices       []VulkanDevice `json:"devices"`
	DefaultDevice string         `json:"default_device"`
}
