package logging

import (
	"fmt"
	"io"
	"os"
	"strings"
)

var DefaultWriter io.Writer = os.Stdout

const (
	_LOG_IFNO = "[\033[1mLEEGO\033[0m] [\033[1;32mINFO\033[0m] "
	_LOG_WARN = "[\033[1mLEEGO\033[0m] [\033[1;33mWARN\033[0m] "
)

// INFO 记录 INFO 信息
func INFO(format string, values ...any) {
	log(_LOG_IFNO, format, values...)
}

// WARN	记录 WARN 信息
func WARN(format string, values ...any) {
	log(_LOG_WARN, format, values...)
}

func log(level string, format string, values ...any) {
	if !strings.HasSuffix(format, "\n") {
		format += "\n"
	}

	if DefaultWriter == nil {
		DefaultWriter = os.Stdout
	}

	_, _ = fmt.Fprintf(DefaultWriter, level+format, values...)
}
