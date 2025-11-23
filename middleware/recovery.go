package middleware

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
)

// Recover 当请求触发 panic 时的处理
func Recover() leego.MiddlewareFunc {
	return RecoverWithConfig(&RecoveryConfig{
		Output:          os.Stdout,
		DumpRequest:     true,
		DumpRequestBody: false,
	})
}

// RecoverWithConfig 当请求触发 panic 时，通过指定配置进行处理
func RecoverWithConfig(config *RecoveryConfig) leego.MiddlewareFunc {
	var logger *log.Logger
	if config.Output != nil {
		logger = log.New(config.Output, "\n\n\x1b[31m", log.LstdFlags)
	}

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
					var req []byte
					if config.DumpRequestBody {
						req, _ = httputil.DumpRequest(ctx.Request, true)
					} else {
						req, _ = httputil.DumpRequest(ctx.Request, false)
					}

					logger.Printf("[Recovery] panic recovered:\n%s\n%s\n%s\033[0m", req, err, trace(skip))
				} else {
					logger.Printf("[Recovery] panic recovered:\n%s\n%s\033[0m", err, trace(skip))
				}

				// 自定义处理
				if config.HandleFunc != nil {
					config.HandleFunc(ctx, err)
				}

				// 若该请求还未处理响应，则响应错误
				if ctx.GetStatusCode() == 0 {
					ctx.ResponseError(http.StatusInternalServerError, "500 "+http.StatusText(http.StatusInternalServerError))
				}
			}()

			next(ctx)
		}
	}
}

type RecoveryConfig struct {
	Output          io.Writer                         // 输出接口，若未指定，则输出到 os.Stdout
	DumpRequest     bool                              // 转储请求信息（不包含请求体）
	DumpRequestBody bool                              // 转储请求体
	HandleFunc      func(ctx *leego.Context, err any) // 自定义处理函数
}

// trace 追踪调用信息
func trace(skip int) []byte {
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

// getFunction 获取调用函数信息
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

// getSourceLine 获取源代码所在行信息
func getSource(lines [][]byte, n int) []byte {
	n--
	if n < 0 || n >= len(lines) {
		return []byte("???")
	}

	return bytes.TrimSpace(lines[n])
}
