package leego

import (
	"net/http"
	"path"
	"strings"

	"gitee.com/leminewx/leego/headers"
)

// onlyFileFS 定义
type onlyFileFS struct {
	fs     http.FileSystem
	prefix string
}

func newOnlyFileFS(prefix string, fs http.FileSystem) *onlyFileFS {
	return &onlyFileFS{fs: fs, prefix: prefix}
}

func (own *onlyFileFS) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	// 规范化路径：移除开头的斜杠，防止路径遍历攻击
	requestedPath := req.URL.Path
	requestedPath = strings.TrimPrefix(requestedPath, "/")
	requestedPath = strings.TrimPrefix(requestedPath, "\\")
	requestedPath = path.Clean(requestedPath)

	// 安全检查：防止路径遍历攻击
	if containsPathTraversal(requestedPath) {
		http.Error(resp, "403 "+http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}

	// 检查文件是否存在且可访问
	file, err := own.fs.Open(requestedPath)
	if err != nil {
		http.Error(resp, "404 "+http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}
	defer func() { _ = file.Close() }()

	// 检查文件状态
	stat, err := file.Stat()
	if err != nil || stat.IsDir() {
		http.Error(resp, "404 "+http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	// 设置安全头
	resp.Header().Set(headers.X_CONTENT_TYPE_OPTIONS, "nosniff") // 防止 MIME 类型嗅探攻击
	resp.Header().Set(headers.X_FRAME_OPTIONS, "DENY")           // 防止点击劫持（ClickJacking）攻击

	// 响应文件
	req.URL.Path = requestedPath
	http.ServeContent(resp, req, stat.Name(), stat.ModTime(), file)
}
