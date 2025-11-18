package middleware

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"runtime"
	"strings"
	"time"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/response"
)

/*
Recover 当请求触发 panic 时恢复正常
  - notify：异步通知，可设为 nil
*/
func Recover() leego.MiddlewareFunc {
	return func(handle leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			// 处理异常
			defer func() {
				if err := recover(); err != nil {
					// 获取错误信息
					msg := trace(fmt.Sprintf("%v", err))

					// 打印错误信息
					_, _ = fmt.Fprintf(os.Stdout, "\033[45m\033[30m%s\033[0m\n\n", msg)

					// 若该请求还未处理，则响应错误
					if ctx.GetStatusCode() == 0 {
						ctx.ResponseError(http.StatusInternalServerError, response.STATUS_500_INTERNAL_SERVER_ERROR)
					}
				}
			}()

			// 调用下一个处理器
			handle(ctx)
		}
	}
}

func trace(message string) string {
	// 获取调用栈信息(跳过前3层调用)
	var pcs [32]uintptr
	n := runtime.Callers(3, pcs[:])

	// 封装调用信息
	var builder bytes.Buffer
	builder.WriteString(message)
	builder.WriteString("\nTraceback:")
	for _, pc := range pcs[:n] {
		fn := runtime.FuncForPC(pc)
		file, line := fn.FileLine(pc)
		builder.WriteString(fmt.Sprintf("\n    %s:%d", file, line))
	}

	return builder.String()
}

// RecoveryWithWriter returns a middleware for a given writer that recovers from any panics and calls the provided handle func to handle it.
func RecoveryWithWriter(writer io.Writer, handle func(ctx *leego.Context, err error)) leego.HandleFunc {
	var logger *log.Logger
	if out != nil {
		logger = log.New(out, "\n\n\x1b[31m", log.LstdFlags)
	}

	return func(ctx *leego.Context) {
		defer func() {
			if err := recover(); err != nil {
				// 检查是否因为连接断开导致的panic
				var brokenPipe bool
				if opError, ok := err.(*net.OpError); ok {
					var sysError *os.SyscallError
					if errors.As(opError, &sysError) {
						seStr := strings.ToLower(sysError.Error())
						if strings.Contains(seStr, "broken pipe") ||
							strings.Contains(seStr, "connection reset by peer") {
							brokenPipe = true
						}
					}
				}

				if logger != nil {
					const stackSkip = 3
					if brokenPipe {
						logger.Printf("%s\n%s%s", err, secureRequestDump(c.Request), reset)
					} else if IsDebugging() {
						logger.Printf("[Recovery] %s panic recovered:\n%s\n%s\n%s%s",
							timeFormat(time.Now()), secureRequestDump(c.Request), err, stack(stackSkip), reset)
					} else {
						logger.Printf("[Recovery] %s panic recovered:\n%s\n%s%s",
							timeFormat(time.Now()), err, stack(stackSkip), reset)
					}
				}
				if brokenPipe {
					// If the connection is dead, we can't write a status to it.
					c.Error(err.(error)) //nolint: errcheck
					c.Abort()
				} else {
					handle(c, err)
				}
			}
		}()
		c.Next()
	}
}

// secureRequestDump returns a sanitized HTTP request dump where the Authorization header,
// if present, is replaced with a masked value ("Authorization: *") to avoid leaking sensitive credentials.
//
// Currently, only the Authorization header is sanitized. All other headers and request data remain unchanged.
func secureRequestDump(r *http.Request) string {
	httpRequest, _ := httputil.DumpRequest(r, false)
	lines := strings.Split(bytesconv.BytesToString(httpRequest), "\r\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "Authorization:") {
			lines[i] = "Authorization: *"
		}
	}
	return strings.Join(lines, "\r\n")
}

func defaultHandleRecovery(c *Context, _ any) {
	c.AbortWithStatus(http.StatusInternalServerError)
}

// stack returns a nicely formatted stack frame, skipping skip frames.
func stack(skip int) []byte {
	buf := new(bytes.Buffer) // the returned data
	// As we loop, we open files and read them. These variables record the currently
	// loaded file.
	var lines [][]byte
	var lastFile string
	for i := skip; ; i++ { // Skip the expected number of frames
		pc, file, line, ok := runtime.Caller(i)
		if !ok {
			break
		}
		// Print this much at least.  If we can't find the source, it won't show.
		fmt.Fprintf(buf, "%s:%d (0x%x)\n", file, line, pc)
		if file != lastFile {
			data, err := os.ReadFile(file)
			if err != nil {
				continue
			}
			lines = bytes.Split(data, []byte{'\n'})
			lastFile = file
		}
		fmt.Fprintf(buf, "\t%s: %s\n", function(pc), source(lines, line))
	}
	return buf.Bytes()
}

// source returns a space-trimmed slice of the n'th line.
func source(lines [][]byte, n int) []byte {
	n-- // in stack trace, lines are 1-indexed but our array is 0-indexed
	if n < 0 || n >= len(lines) {
		return dunnoBytes
	}
	return bytes.TrimSpace(lines[n])
}

// function returns, if possible, the name of the function containing the PC.
func function(pc uintptr) string {
	fn := runtime.FuncForPC(pc)
	if fn == nil {
		return dunno
	}
	name := fn.Name()
	// The name includes the path name to the package, which is unnecessary
	// since the file name is already included.  Plus, it has center dots.
	// That is, we see
	//	runtime/debug.*T·ptrmethod
	// and want
	//	*T.ptrmethod
	// Also the package path might contain dot (e.g. code.google.com/...),
	// so first eliminate the path prefix
	if lastSlash := strings.LastIndexByte(name, '/'); lastSlash >= 0 {
		name = name[lastSlash+1:]
	}
	if period := strings.IndexByte(name, '.'); period >= 0 {
		name = name[period+1:]
	}
	name = strings.ReplaceAll(name, "·", ".")
	return name
}

// timeFormat returns a customized time string for logger.
func timeFormat(t time.Time) string {
	return t.Format("2006/01/02 - 15:04:05")
}
