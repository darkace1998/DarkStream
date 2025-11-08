module github.com/darkace1998/video-converter-worker

go 1.24

toolchain go1.24.9

require (
	github.com/darkace1998/video-converter-common v0.1.0
	github.com/vulkan-go/vulkan v0.0.0-20221209234627-c0a353ae26c8
	gopkg.in/yaml.v3 v3.0.1
)

replace github.com/darkace1998/video-converter-common => ../video-converter-common
