package middleware

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"gitee.com/leminewx/leego"
)

// 颜色与样式常量
const (
	_RESET   = "\033[0m"
	_BOLD    = "\033[1m"
	_DIM     = "\033[2m"
	_RED     = "\033[31m"
	_GREEN   = "\033[32m"
	_YELLOW  = "\033[33m"
	_BLUE    = "\033[34m"
	_MAGENTA = "\033[35m"
	_CYAN    = "\033[36m"
	_WHITE   = "\033[37m"

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

	// 各列的固定可见宽度：
	//   - 徽标宽度已包含左右各一空格的内边距，该内边距随底色一起着色；
	//   - 耗时列 = 数值 7（999.999）+ 单位 2，超出时自动降低精度以维持定宽。
	_WIDTH_METHOD  = 9
	_WIDTH_STATUS  = 5
	_WIDTH_LATENCY = 9

	// 耗时单位统一为 2 个 ASCII 字符：
	// “µ”属于 East Asian Ambiguous 字符，在部分终端/字体（尤其中文区域）下会被渲染为双宽，
	// 从而破坏列对齐；使用 ASCII 可确保任何等宽终端下单位恒占 2 列。
	// 若确认终端按窄字符渲染 µ，可将 _UNIT_US 改为 "µs"。
	_UNIT_NS = "ns"
	_UNIT_US = "us"
	_UNIT_MS = "ms"
	_UNIT_S  = "s "
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
			withColor := config.WithColor || isColorAllowed(out, config.WithColor)

			renderLog(out, withColor, status, latency, request.Method, path, ctx.GetClientIP())
		}
	}
}

// renderLog 按统一列布局渲染一行访问日志
//
//	[LEEGO] 2026-08-04 12:00:00.000 │ GET     │ 200 │  12.340ms │ 127.0.0.1 │ /api/v1/users/42
//
// 徽标自带左右内边距，因此竖线紧贴徽标（| 与 | 之间无未着色的空白），其余列保留空格
func renderLog(out io.Writer, withColor bool, status int, latency time.Duration, method, path, ip string) {
	bar := colorize("│", _DIM, withColor)

	line := "[LEEGO] " +
		colorize(time.Now().Format(_FORMAT_DATETIME), _DIM, withColor) + " " +
		bar + createBadge(method, getMethodColor(method), _WIDTH_METHOD, withColor) +
		bar + createBadge(strconv.Itoa(status), getStatusColor(status), _WIDTH_STATUS, withColor) +
		bar + " " + colorize(formatLatency(latency), getLatencyColor(latency), withColor) + " " +
		bar + " " + colorize(ip, _DIM, withColor) + " " +
		bar + " " + colorize(path, _BOLD, withColor) + "\n"

	_, _ = fmt.Fprint(out, line)
}

// createBadge 生成固定可见宽度的彩色徽标：左侧留一空格内边距，右侧补足至 width-1（至少再留一空格），
// 内边距与文字一并着色，保证 | 与 | 之间被底色完整填充（色码不影响对齐）
func createBadge(text, color string, width int, withColor bool) string {
	padded := pad(" "+truncate(text, width-2), width-1) + " "
	if !withColor {
		return padded
	}
	return color + padded + _RESET
}

// truncate 按可见宽度截断文本，保证徽标宽度恒定（HTTP 方法 / 状态码均为 ASCII）
func truncate(s string, w int) string {
	if len(s) <= w {
		return s
	}
	return s[:w]
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

// formatLatency 将耗时格式化为固定 _WIDTH_LATENCY 列宽的字符串（数值 + 单位），确保列严格对齐
func formatLatency(d time.Duration) string {
	var value float64
	var unit string
	switch {
	case d < time.Microsecond:
		value, unit = float64(d.Nanoseconds()), _UNIT_NS
	case d < time.Millisecond:
		value, unit = float64(d.Nanoseconds())/float64(time.Microsecond), _UNIT_US
	case d < time.Second:
		value, unit = float64(d.Nanoseconds())/float64(time.Millisecond), _UNIT_MS
	default:
		value, unit = d.Seconds(), _UNIT_S
	}

	return formatFixedFloat(value, _WIDTH_LATENCY-len(unit)) + unit
}

// formatFixedFloat 以固定宽度右对齐输出数值：优先保留 3 位小数，
// 宽度不足时逐级降低精度，最终退化为科学计数，确保返回值宽度恒为 width
func formatFixedFloat(value float64, width int) string {
	for _, precision := range [...]int{3, 2, 1, 0} {
		if text := strconv.FormatFloat(value, 'f', precision, 64); len(text) <= width {
			return padLeft(text, width)
		}
	}

	if text := strconv.FormatFloat(value, 'e', 1, 64); len(text) <= width {
		return padLeft(text, width)
	}

	// 极端兜底：数值已超出可表达宽度，仍保持定宽
	return strings.Repeat(" ", width-1) + "+"
}

// padLeft 按可见宽度在左侧补空格至 w
func padLeft(s string, w int) string {
	if n := len(s); n < w {
		return strings.Repeat(" ", w-n) + s
	}
	return s
}

// isColorAllowed 在配置未显式开启颜色时，依据终端与环境变量决定是否可以着色：
//   - 尊重 NO_COLOR（https://no-color.org）约定；
//   - 当 TERM=dumb 时不着色；
//   - 仅当输出目标是标准输出/标准错误等字符设备时着色。
func isColorAllowed(out io.Writer, explicit bool) bool {
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
