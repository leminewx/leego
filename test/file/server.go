package main

import (
	"io"
	"net/http"
	"os"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/headers"
	"gitee.com/leminewx/leego/middleware"
)

func main() {
	engine := leego.NewEngine()
	engine.UseMiddlewareFuncs(
		middleware.Log(),
		middleware.Recover(),
	)

	// 访问文件
	engine.GET("/apis/files/show/*filepath", func(ctx *leego.Context) {
		filepath := ctx.GetPathValue("filepath")
		ctx.ResponseFileContent(filepath)
	})
	engine.GET("/apis/files/download/*filepath", func(ctx *leego.Context) {
		filepath := ctx.GetPathValue("filepath")
		ctx.ResponseFileAttachment(filepath)
	})

	// 通过 http.FileSytem 访问文件
	fs := http.Dir(".")
	engine.GET("/apis/files/show/fs/*filepath", func(ctx *leego.Context) {
		filepath := ctx.GetPathValue("filepath")
		ctx.ResponseFileContentFromFS(filepath, fs)
	})
	engine.GET("/apis/files/download/fs/*filepath", func(ctx *leego.Context) {
		filepath := ctx.GetPathValue("filepath")
		ctx.ResponseFileAttachmentFromFS(filepath, fs)
	})

	// 流式下载大文件
	engine.GET("/apis/files/download/bigfile", func(ctx *leego.Context) {
		file, err := os.Open("file.txt")
		if err != nil {
			ctx.ResponseError(http.StatusNotFound, "404 Not Found File")
			return
		}

		ctx.SetResponseHeader(headers.CONTENT_TYPE, "application/octet-stream")
		ctx.SetResponseHeader(headers.CONTENT_DISPOSITION, "attachment; filename=file.txt")
		ctx.SetStatusCode(http.StatusOK)

		buffer := make([]byte, 1024*4)
		for {
			n, err := file.Read(buffer)
			if n > 0 {
				ctx.Response.Write(buffer[:n])
				ctx.Response.Flush()
			}
			if err == io.EOF {
				break
			}
		}
	})

	// 静态文件访问
	engine.SetStatic("/static", ".")
	_ = engine.Run(":8080")
}
