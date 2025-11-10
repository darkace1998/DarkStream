module github.com/darkace1998/video-converter-master

go 1.24

require (
	github.com/darkace1998/video-converter-common v0.1.0
	github.com/mattn/go-sqlite3 v1.14.32
	gopkg.in/yaml.v3 v3.0.1
)

replace github.com/darkace1998/video-converter-common => ../video-converter-common
