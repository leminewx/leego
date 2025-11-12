package render

import "net/http"

// Render 定义渲染响应的接口类型
type Render interface {
	// WriteContentType 写入内容类型到响应头
	WriteContentType(resp http.ResponseWriter)
	// WriteContent 写入内容到响应体
	WriteContent(resp http.ResponseWriter, req *http.Request)
}
