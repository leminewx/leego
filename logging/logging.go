// Package logging 提供框架内部使用的轻量日志输出能力。
//
// 该包只服务于框架自身的启动、告警与诊断信息，不承担业务日志职责。
// 需要结构化日志、分级、采样或输出到文件的场景，请接入 logging/slog
// 或第三方日志库，并通过 SetOutput 把框架日志重定向到统一出口。
package logging

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

const (
	// _PREFIX_INFO 定义 INFO 级别的前缀（含 ANSI 颜色）。
	_PREFIX_INFO = "[\033[1mLEEGO\033[0m] [\033[1;32mINFO\033[0m] "
	// _PREFIX_WARN 定义 WARN 级别的前缀（含 ANSI 颜色）。
	_PREFIX_WARN = "[\033[1mLEEGO\033[0m] [\033[1;33mWARN\033[0m] "
)

var (
	// outputMu 保护 outputWriter，避免并发替换输出目标时产生数据竞争。
	outputMu sync.RWMutex
	// outputWriter 为当前输出目标，默认标准输出。
	outputWriter io.Writer = os.Stdout
	// writeMu 串行化写入：fmt.Fprintf 对同一 writer 的多次 Write 不保证原子，
	// 并发输出时日志行会互相交织。
	writeMu sync.Mutex
)

// SetOutput 设置日志输出目标。
//
// 传入 nil 表示恢复为 os.Stdout。该函数并发安全，可在运行期随时切换，
// 常见用法是把框架日志接入统一日志文件或结构化日志适配器。
func SetOutput(writer io.Writer) {
	outputMu.Lock()
	if writer == nil {
		outputWriter = os.Stdout
	} else {
		outputWriter = writer
	}
	outputMu.Unlock()
}

// GetOutput 返回当前日志输出目标。
func GetOutput() io.Writer {
	outputMu.RLock()
	writer := outputWriter
	outputMu.RUnlock()
	return writer
}

// LogInfo 输出 INFO 级别日志。
//
// 参数与 fmt.Printf 一致；format 不以换行结尾时会自动补一个换行。
func LogInfo(format string, values ...any) {
	write(_PREFIX_INFO, format, values...)
}

// LogWarn 输出 WARN 级别日志。
//
// 参数与 fmt.Printf 一致；format 不以换行结尾时会自动补一个换行。
func LogWarn(format string, values ...any) {
	write(_PREFIX_WARN, format, values...)
}

// write 将带级别的日志写入当前输出目标。
func write(level, format string, values ...any) {
	if !strings.HasSuffix(format, "\n") {
		format += "\n"
	}

	writer := GetOutput()
	if writer == nil {
		writer = os.Stdout
	}

	// 框架日志频率低，直接串行化以保证每行完整。
	writeMu.Lock()
	_, _ = fmt.Fprintf(writer, level+format, values...)
	writeMu.Unlock()
}
