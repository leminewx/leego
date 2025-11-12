package middleware

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"gitee.com/leminewx/leego"
)

var DefaultWriter io.Writer = os.Stdout

// 颜色常量
const (
	_COLOR_RED     = "\033[97;41m" // 红色
	_COLOR_GREEN   = "\033[97;42m" // 绿色
	_COLOR_YELLOW  = "\033[90;43m" // 黄色
	_COLOR_BLUE    = "\033[97;44m" // 蓝色
	_COLOR_MAGENTA = "\033[97;45m" // 品红
	_COLOR_CYAN    = "\033[97;46m" // 青色
	_COLOR_WHITE   = "\033[90;47m" // 白色
	_COLOR_RESET   = "\033[0m"     // 无色

	_FORMAT_DATETIME          = "2006-01-02 15:04:05.000"
	_FORMAT_LOG_WITH_COLOR    = "[LEEGO] %v |%s %3d \033[0m|%s %12v \033[0m| %15s |%s %-7s \033[0m %#v\n"
	_FORMAT_LOG_WITHOUT_COLOR = "[LEEGO] %v | %3d | %12v | %15s | %-7s %#v\n"
)

/*
Log 打印请求日志
  - record：用于记录日志信息，可设为 nil
*/
func Log() leego.MiddlewareFunc {
	return func(handle leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			// 调用下一个处理器
			handle(ctx)

			// 获取响应信息
			start := ctx.GetStartTime()
			status := ctx.GetStatusCode()
			latency := time.Since(start)
			request := ctx.Request

			_, _ = fmt.Fprintf(os.Stdout, _FORMAT_LOG_WITH_COLOR,
				time.Now().Format(_FORMAT_DATETIME),
				getStatusColor(status), status,
				getLatencyColor(latency), latency,
				ctx.GetClientIP(),
				getMethodColor(request.Method), request.Method, request.URL.Path,
			)
		}
	}
}

func LogWithConfig(config *LoggerConfig) leego.MiddlewareFunc {
	return func(handle leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			// 调用下一个处理器
			handle(ctx)

			// 检查是否跳过 404 请求
			status := ctx.GetStatusCode()
			if config.Skip404NotFound && ctx.GetStatusCode() == http.StatusNotFound {
				return
			}

			// 检查是否跳过当前 path
			request := ctx.Request
			if config.shouldSkip(request.URL.Path) {
				return
			}

			// 检查输出
			out := config.Output
			if out == nil {
				out = os.Stdout
			}

			// 获取响应信息
			start := ctx.GetStartTime()
			latency := time.Since(start)

			// 带颜色
			if config.WithColor {
				// 带查询参数
				if config.WithQuery {
					_, _ = fmt.Fprintf(os.Stdout, _FORMAT_LOG_WITH_COLOR,
						time.Now().Format(_FORMAT_DATETIME),
						getStatusColor(status), status,
						getLatencyColor(latency), latency,
						ctx.GetClientIP(),
						getMethodColor(request.Method), request.Method, request.URL.Path,
					)
					return
				}

				// 不带查询参数
				_, _ = fmt.Fprintf(os.Stdout, _FORMAT_LOG_WITH_COLOR,
					time.Now().Format(_FORMAT_DATETIME),
					getStatusColor(status), status,
					getLatencyColor(latency), latency,
					ctx.GetClientIP(),
					getMethodColor(request.Method), request.Method, request.URL.Path+request.URL.RawPath,
				)
				return
			}

			// 不带颜色，带查询参数
			if config.WithQuery {
				_, _ = fmt.Fprintf(os.Stdout, _FORMAT_LOG_WITHOUT_COLOR, time.Now().Format(_FORMAT_DATETIME),
					status, latency, ctx.GetClientIP(), request.Method, request.URL.Path+request.URL.RawQuery,
				)
				return
			}

			// 不带颜色，不带查询参数
			_, _ = fmt.Fprintf(os.Stdout, _FORMAT_LOG_WITHOUT_COLOR, time.Now().Format(_FORMAT_DATETIME),
				status, latency, ctx.GetClientIP(), request.Method, request.URL.Path)
		}
	}
}

// LoggerConfig 定义日志配置的实现类型
type LoggerConfig struct {
	Output            io.Writer           // 输出接口
	WithColor         bool                // 是否携带颜色
	WithQuery         bool                // 是否携带查询参数
	Skip404NotFound   bool                // 跳过 404 请求
	SkipAbsolutePaths []string            // 跳过记录的绝对路径
	SkipWildcardPaths map[string]struct{} // 跳过记录的通配路径
}

// shouldSkip 检查是否应该跳过日志记录
func (own *LoggerConfig) shouldSkip(path string) bool {
	// 先匹配通配路径
	if _, ok := own.SkipWildcardPaths[path]; ok {
		return true
	}

	// 再匹配绝对路径
	for _, skipPath := range own.SkipAbsolutePaths {
		if path == skipPath {
			return true
		}
	}

	return false
}

// getMethodColor 获取方法颜色
func getMethodColor(method string) string {
	switch method {
	case http.MethodGet:
		return _COLOR_BLUE
	case http.MethodPost:
		return _COLOR_GREEN
	case http.MethodPut:
		return _COLOR_YELLOW
	case http.MethodPatch:
		return _COLOR_CYAN
	case http.MethodDelete:
		return _COLOR_RED
	case http.MethodHead:
		return _COLOR_MAGENTA
	default:
		return _COLOR_WHITE
	}
}

// getStatusColor 获取状态码颜色
func getStatusColor(status int) string {
	switch {
	case status >= http.StatusInternalServerError:
		return _COLOR_RED
	case status >= http.StatusBadRequest:
		return _COLOR_YELLOW
	case status >= http.StatusMultipleChoices:
		return _COLOR_BLUE
	case status >= http.StatusOK:
		return _COLOR_GREEN
	default:
		return _COLOR_WHITE
	}
}

// getLatencyColor 获取响应延迟颜色
func getLatencyColor(latency time.Duration) string {
	switch {
	case latency < time.Millisecond*100:
		return _COLOR_WHITE
	case latency < time.Millisecond*200:
		return _COLOR_GREEN
	case latency < time.Millisecond*300:
		return _COLOR_CYAN
	case latency < time.Millisecond*500:
		return _COLOR_BLUE
	case latency < time.Second:
		return _COLOR_YELLOW
	case latency < time.Second*2:
		return _COLOR_MAGENTA
	default:
		return _COLOR_RED
	}
}
