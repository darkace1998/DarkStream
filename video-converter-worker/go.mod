module github.com/darkace1998/video-converter-worker

go 1.24

toolchain go1.24.9

require (
	github.com/darkace1998/golang-vulkan-api v1.0.4
	github.com/darkace1998/video-converter-common v0.1.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/google/uuid v1.6.0 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/rogpeppe/go-internal v1.10.0 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
)

replace github.com/darkace1998/video-converter-common => ../video-converter-common
