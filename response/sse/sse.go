package sse

import (
	"net/http"

	"gitee.com/leminewx/leego/response"
)

// SetResponseHeader 设置 SSE 响应头
func SetResponseHeader(resp response.Writer) {
	// 设置响应头
	header := resp.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no") // 禁用代理缓冲

	// 设置响应状态码
	resp.WriteHeader(http.StatusOK)
	resp.Flush()
}
