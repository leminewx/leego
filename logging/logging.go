package logging

import (
	"fmt"
	"os"
	"strings"
)

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

	_, _ = fmt.Fprintf(os.Stdout, level+format, values...)
}
