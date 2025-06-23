//go:build windows

package leego

import (
	"net/http"
	"path/filepath"
	"strings"
)

// createStaticHandler 创建静态文件服务的处理器
func (own *group) createStaticHandler(relativePath string, fs http.FileSystem, cache bool) HandleFunc {
	const FILEPATH = "leegofilepath"
	fileServer := http.StripPrefix(relativePath, http.FileServer(fs))
	return func(ctx *Context) {
		if _, err := fs.Open(strings.ReplaceAll(filepath.Join(ctx.GetPathValue(FILEPATH)), "\\", "/")); err != nil {
			ctx.WriteCode(http.StatusNotFound)
			return
		}

		if !cache {
			ctx.SetHeader("Cache-Control", "no-store")
		}

		fileServer.ServeHTTP(ctx.GetResponse(), ctx.GetRequest())
	}
}
