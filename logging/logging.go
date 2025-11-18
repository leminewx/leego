package logging

import (
	"fmt"
	"io"
	"os"
	"strings"
)

var DefaultWriter io.Writer = os.Stdout

const (
	_LOG_IFNO = "[LEEGO] [INFO] "
	_LOG_WARN = "[LEEGO] [WARN] "
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
