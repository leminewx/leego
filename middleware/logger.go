package middleware

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"gitee.com/leminewx/leego"
)

// 颜色与样式常量
const (
	_RESET  = "\033[0m"
	_BOLD   = "\033[1m"
	_DIM    = "\033[2m"
	_RED    = "\033[31m"
	_GREEN  = "\033[32m"
	_YELLOW = "\033[33m"
	_BLUE   = "\033[34m"
	_MAGENTA = "\033[35m"
	_CYAN   = "\033[36m"
	_WHITE  = "\033[37m"

	// 粗体白字 + 彩色底，用作方法 / 状态码徽标
	_BADGE_GET     = "\033[97;44m" // 蓝底
	_BADGE_POST    = "\033[97;42m" // 绿底
	_BADGE_PUT     = "\033[97;43m" // 黄底
	_BADGE_PATCH   = "\033[97;46m" // 青底
	_BADGE_DELETE  = "\033[97;41m" // 红底
	_BADGE_HEAD    = "\033[97;45m" // 品红底
	_BADGE_DEFAULT = "\033[97;47m" // 白底

	// 状态码底色
	_STATUS_OK         = "\033[97;42m" // 2xx 绿
	_STATUS_REDIRECT   = "\033[97;46m" // 3xx 青
	_STATUS_CLIENT_ERR = "\033[30;43m" // 4xx 黄
	_STATUS_SERVER_ERR = "\033[97;41m" // 5xx 红
	_STATUS_OTHER      = "\033[97;47m" // 其他 白

	_FORMAT_DATETIME = "2006-01-02 15:04:05.000"
)

// Log 记录请求响应信息（自动检测终端，启用彩色输出）
func Log() leego.MiddlewareFunc {
	return LogWithConfig(&LoggerConfig{})
}

// LogWithConfig 通过指定配置，记录请求响应信息
func LogWithConfig(config *LoggerConfig) leego.MiddlewareFunc {
	// 允许传入 nil 配置
	if config == nil {
		config = &LoggerConfig{}
	}

	// 拷贝一份配置快照，避免：
	// 1. 直接修改调用方持有的结构体；
	// 2. 请求期读取配置与调用方并发修改配置产生数据竞争
	snapshot := &LoggerConfig{
		Output:            config.Output,
		WithColor:         config.WithColor,
		WithQuery:         config.WithQuery,
		SkipAbsolutePaths: make(map[string]struct{}, len(config.SkipAbsolutePaths)),
		SkipWildcardPaths: append([]string(nil), config.SkipWildcardPaths...),
	}
	for path := range config.SkipAbsolutePaths {
		snapshot.SkipAbsolutePaths[path] = struct{}{}
	}
	config = snapshot

	return func(handle leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			// 调用下一个处理器
			handle(ctx)

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
			status := ctx.GetStatusCode()
			latency := time.Since(ctx.GetStartTime())
			path := request.URL.Path
			if config.WithQuery && request.URL.RawQuery != "" {
				path += "?" + request.URL.RawQuery
			}

			// 是否启用颜色：显式配置优先；未显式设置时按终端能力自动判定
			withColor := config.WithColor || colorAllowed(out, config.WithColor)

			renderLog(out, withColor, status, latency, request.Method, path, ctx.GetClientIP())
		}
	}
}

// renderLog 按统一列布局渲染一行访问日志
//
//	[LEEGO] 2026-08-04 12:00:00.000 │ GET    │ 200 │  12.34ms │ 127.0.0.1 │ /api/v1/users/42
func renderLog(out io.Writer, withColor bool, status int, latency time.Duration, method, path, ip string) {
	const sep = _DIM + " │ " + _RESET

	methodField := badge(method, getMethodColor(method), pad(method, 6), withColor)
	statusField := badge(fmt.Sprintf("%d", status), getStatusColor(status), pad(fmt.Sprintf("%d", status), 3), withColor)
	latencyField := colorize(fmt.Sprintf("%8s", formatLatency(latency)), getLatencyColor(latency), withColor)
	ipField := colorize(ip, _DIM, withColor)
	pathField := colorize(path, _BOLD, withColor)

	line := fmt.Sprintf("[LEEGO] %s%s%s%s%s%s%s%s%s%s\n",
		colorize(time.Now().Format(_FORMAT_DATETIME), _DIM, withColor),
		sep,
		methodField,
		sep,
		statusField,
		sep,
		latencyField,
		sep,
		ipField,
		sep+pathField,
	)

	_, _ = fmt.Fprint(out, line)
}

// badge 生成固定可见宽度的彩色徽标（文字已内补，色码不影响对齐）
func badge(_, color, padded string, withColor bool) string {
	if !withColor {
		return padded
	}
	return color + padded + _RESET
}

// colorize 为文本包裹颜色（withColor 为 false 时原样返回）
func colorize(text, color string, withColor bool) string {
	if !withColor {
		return text
	}
	return color + text + _RESET
}

// pad 将文本按可见宽度补到 w（ASCII 场景按字节长度处理即可）
func pad(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-len(s))
}

// formatLatency 将耗时格式化为人类可读字符串
func formatLatency(d time.Duration) string {
	switch {
	case d < time.Microsecond:
		return fmt.Sprintf("%.3fns", float64(d.Nanoseconds()))
	case d < time.Millisecond:
		return fmt.Sprintf("%.3fµs", float64(d.Microseconds()))
	case d < time.Second:
		return fmt.Sprintf("%.2fms", float64(d.Microseconds())/1000)
	default:
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
}

// colorAllowed 在配置未显式开启颜色时，依据终端与环境变量决定是否可以着色：
//   - 尊重 NO_COLOR（https://no-color.org）约定；
//   - 当 TERM=dumb 时不着色；
//   - 仅当输出目标是标准输出/标准错误等字符设备时着色。
func colorAllowed(out io.Writer, explicit bool) bool {
	if explicit {
		return true
	}
	// 显式禁用
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	return out == os.Stdout || out == os.Stderr
}

// LoggerConfig 定义日志配置的实现类型
type LoggerConfig struct {
	Output            io.Writer           // 输出接口，若未指定，则输出到 os.Stdout
	WithColor         bool                // 是否携带颜色
	WithQuery         bool                // 是否携带查询参数
	SkipWildcardPaths []string            // 跳过通配路径
	SkipAbsolutePaths map[string]struct{} // 跳过绝对路径
}

// shouldSkip 检查是否应该跳过日志记录
func (own *LoggerConfig) shouldSkip(path string) bool {
	// 匹配绝对路径
	if _, ok := own.SkipAbsolutePaths[path]; ok {
		return true
	}

	// 匹配通配路径
	for _, wildcardPath := range own.SkipWildcardPaths {
		if strings.HasPrefix(path, wildcardPath) {
			return true
		}
	}

	return false
}

// getMethodColor 获取方法徽标底色
func getMethodColor(method string) string {
	switch method {
	case http.MethodGet:
		return _BADGE_GET
	case http.MethodPost:
		return _BADGE_POST
	case http.MethodPut:
		return _BADGE_PUT
	case http.MethodPatch:
		return _BADGE_PATCH
	case http.MethodDelete:
		return _BADGE_DELETE
	case http.MethodHead:
		return _BADGE_HEAD
	default:
		return _BADGE_DEFAULT
	}
}

// getStatusColor 获取状态码徽标底色
func getStatusColor(status int) string {
	switch {
	case status >= http.StatusInternalServerError:
		return _STATUS_SERVER_ERR
	case status >= http.StatusBadRequest:
		return _STATUS_CLIENT_ERR
	case status >= http.StatusMultipleChoices:
		return _STATUS_REDIRECT
	case status >= http.StatusOK:
		return _STATUS_OK
	default:
		return _STATUS_OTHER
	}
}

// getLatencyColor 获取响应延迟颜色（仅前景色，避免视觉过重的底色）
func getLatencyColor(latency time.Duration) string {
	switch {
	case latency < time.Millisecond*100:
		return _WHITE
	case latency < time.Millisecond*200:
		return _GREEN
	case latency < time.Millisecond*300:
		return _CYAN
	case latency < time.Millisecond*500:
		return _BLUE
	case latency < time.Second:
		return _YELLOW
	case latency < time.Second*2:
		return _MAGENTA
	default:
		return _RED
	}
}
