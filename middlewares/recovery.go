package middlewares

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"runtime"
	"strings"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/response"
)

// Recovery 捕获业务处理链中的 panic。
type Recovery struct {
	// config 为构造时固化的配置快照。
	config *RecoveryConfig
	// logger 为输出 panic 信息的日志器。
	logger *log.Logger
}

// NewRecovery 使用默认配置创建 panic 恢复中间件。
//
// 默认为：输出到标准输出、转储请求行与请求头、不转储请求体。
// 生产环境建议使用 NewRecoveryWithConfig 指定统一的日志出口。
func NewRecovery() *Recovery {
	return NewRecoveryWithConfig(&RecoveryConfig{
		Output:          os.Stdout,
		DumpRequest:     true,
		DumpRequestBody: false,
	})
}

// NewRecoveryWithConfig 使用指定配置创建 panic 恢复中间件；config 为 nil 时使用默认配置。
//
// 恢复流程：
//  1. 输出 panic 值与调用栈；
//  2. 调用自定义 HandleFunc（若配置）；
//  3. 若响应尚未写入，返回 500，避免连接悬挂。
func NewRecoveryWithConfig(config *RecoveryConfig) *Recovery {
	// 允许传入 nil 配置，使用默认值兜底
	if config == nil {
		config = &RecoveryConfig{Output: os.Stdout, DumpRequest: true}
	}

	// Output 为空时回退到 os.Stdout，
	// 避免 logger 为 nil 时在 panic 恢复流程中再次 panic（掩盖原始错误）
	output := config.Output
	if output == nil {
		output = os.Stdout
	}

	// 固化配置快照，避免请求期与调用方并发修改产生数据竞争
	snapshot := &RecoveryConfig{
		Output:          output,
		DumpRequest:     config.DumpRequest,
		DumpRequestBody: config.DumpRequestBody,
		HandleFunc:      config.HandleFunc,
	}

	return &Recovery{
		config: snapshot,
		logger: log.New(output, "\n\n\x1b[31m", log.LstdFlags),
	}
}

// AdaptToMiddleware 将 panic 恢复适配为 leego.MiddlewareFunc。
func (own *Recovery) AdaptToMiddleware() leego.MiddlewareFunc {
	config := own.config
	logger := own.logger

	return func(next leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			defer func() {
				// 捕获 panic
				err := recover()
				if err == nil {
					return
				}

				// 输出错误信息
				const skip = 3
				if config.DumpRequest {
					req := dumpRequest(ctx.Request, config.DumpRequestBody)
					logger.Printf("[Recovery] panic recovered:\n%s\n%v\n%s\033[0m", req, err, captureTrace(skip))
				} else {
					logger.Printf("[Recovery] panic recovered:\n%v\n%s\033[0m", err, captureTrace(skip))
				}

				// 自定义处理
				if config.HandleFunc != nil {
					config.HandleFunc(ctx, err)
				}

				// 若该请求还未处理响应，则响应错误
				if ctx.GetStatusCode() == 0 {
					ctx.ResponseError(http.StatusInternalServerError, response.GetStatusLowerText(http.StatusInternalServerError))
				}
			}()

			next(ctx)
		}
	}
}

// RecoveryConfig 定义 panic 恢复中间件的配置。
type RecoveryConfig struct {
	// Output 为日志输出目标；nil 时使用 os.Stdout。
	Output io.Writer
	// DumpRequest 表示是否转储请求行与请求头。
	DumpRequest bool
	// DumpRequestBody 表示是否转储请求体；会消耗请求体，慎用。
	DumpRequestBody bool
	// HandleFunc 为自定义善后函数，可为 nil。
	HandleFunc func(ctx *leego.Context, err any)
}

// sensitiveHeaders 列出转储前需要脱敏的请求头，避免把凭据写入日志。
var sensitiveHeaders = []string{
	"Authorization",
	"Proxy-Authorization",
	"Cookie",
	"Set-Cookie",
	"X-Api-Key",
	"X-Auth-Token",
}

// dumpRequest 转储请求，但先抹除敏感头部。
//
// 直接 DumpRequest 会把 Authorization、Cookie 等凭据原文写进日志，
// 因此这里基于副本脱敏后再转储，不修改原始请求。
func dumpRequest(request *http.Request, withBody bool) []byte {
	if request == nil {
		return nil
	}

	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	for _, name := range sensitiveHeaders {
		if _, ok := clone.Header[name]; ok {
			clone.Header.Set(name, "[REDACTED]")
		}
	}

	if !withBody {
		clone.Body = nil
	}

	data, _ := httputil.DumpRequest(clone, withBody)
	return data
}

// captureTrace 追踪调用信息
func captureTrace(skip int) []byte {
	var lines [][]byte
	var lastFile string
	buf := new(bytes.Buffer)
	for i := skip; ; i++ {
		pc, file, line, ok := runtime.Caller(i)
		if !ok {
			break
		}

		_, _ = fmt.Fprintf(buf, "%s:%d (0x%x)\n", file, line, pc)
		if file != lastFile {
			data, err := os.ReadFile(file)
			if err != nil {
				continue
			}

			lines = bytes.Split(data, []byte{'\n'})
			lastFile = file
		}

		_, _ = fmt.Fprintf(buf, "\t%s: %s\n", getFunction(pc), getSource(lines, line))
	}

	return buf.Bytes()
}

// getFunction 获取调用函数信息。
func getFunction(pc uintptr) string {
	fn := runtime.FuncForPC(pc)
	if fn == nil {
		return "???"
	}

	name := fn.Name()
	if lastSlash := strings.LastIndexByte(name, '/'); lastSlash >= 0 {
		name = name[lastSlash+1:]
	}

	if period := strings.IndexByte(name, '.'); period >= 0 {
		name = name[period+1:]
	}

	name = strings.ReplaceAll(name, "·", ".")
	return name
}

// getSourceLine 获取源代码所在行信息。
func getSource(lines [][]byte, n int) []byte {
	n--
	if n < 0 || n >= len(lines) {
		return []byte("???")
	}

	return bytes.TrimSpace(lines[n])
}
