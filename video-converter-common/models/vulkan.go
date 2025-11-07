package models

type VulkanCapabilities struct {
	Supported           bool
	Device              VulkanDevice
	ApiVersion          string
	SupportedExtensions []string
	CanEncode           bool
	CanDecode           bool
	MaxWidth            uint32
	MaxHeight           uint32
	PreferredFormat     string
}

type VulkanDeviceList struct {
	Devices       []VulkanDevice `json:"devices"`
	DefaultDevice string         `json:"default_device"`
}
