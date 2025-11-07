package logger

import (
	"github.com/darkace1998/video-converter-common/utils"
)

func Init(level, format string) {
	utils.InitLogger(level, format)
}
