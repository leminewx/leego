// Package main 演示 leego 的文件服务能力。
//
// 覆盖四类场景：
//   - 直接输出本地文件（在线预览）；
//   - 以附件形式下载本地文件；
//   - 通过 http.FileSystem 输出文件（可对接 embed.FS、内存 FS 等）；
//   - 大文件流式下载（手动控制 Content-Disposition 与分块写入）。
//
// 同时还演示了静态目录挂载与自定义 404 响应。
package main

import (
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/headers"
	"gitee.com/leminewx/leego/middleware"
)

func main() {
	engine := leego.NewEngine(leego.UseEngineName("leego-file-example"))
	engine.UseMiddlewares(
		middleware.Log(),
		middleware.Recover(),
	)

	// 直接读取本地文件：路径相对于进程工作目录。
	engine.GET("/apis/files/show/*filepath", func(ctx *leego.Context) {
		ctx.ResponseFile(normalizeFilePath(ctx.GetPathValue("filepath")))
	}, "在线查看文件", "文件", "查看")

	engine.GET("/apis/files/download/*filepath", func(ctx *leego.Context) {
		ctx.ResponseAttachment(normalizeFilePath(ctx.GetPathValue("filepath")))
	}, "下载文件", "文件", "下载")

	// 通过 http.FileSystem 访问文件：可替换为任意实现。
	fileSystem := http.Dir(".")
	engine.GET("/apis/files/show/fs/*filepath", func(ctx *leego.Context) {
		ctx.ResponseFileFromFS(normalizeFilePath(ctx.GetPathValue("filepath")), fileSystem)
	}, "通过文件系统查看文件", "文件", "FileSystem")

	engine.GET("/apis/files/download/fs/*filepath", func(ctx *leego.Context) {
		ctx.ResponseAttachmentFromFS(normalizeFilePath(ctx.GetPathValue("filepath")), fileSystem)
	}, "通过文件系统下载文件", "文件", "FileSystem")

	// 流式下载大文件：不把文件整体读入内存，适合 GB 级文件。
	engine.GET("/apis/files/download/bigfile", func(ctx *leego.Context) {
		file, err := os.Open("file.txt")
		if err != nil {
			ctx.ResponseError(http.StatusNotFound, "文件不存在")
			return
		}
		defer func() { _ = file.Close() }()

		info, err := file.Stat()
		if err != nil {
			ctx.ResponseError(http.StatusInternalServerError, "读取文件信息失败")
			return
		}

		ctx.SetResponseHeader(headers.CONTENT_TYPE, "application/octet-stream")
		ctx.SetResponseHeader(headers.CONTENT_DISPOSITION, `attachment; filename="file.txt"`)
		ctx.SetResponseHeader(headers.CONTENT_LENGTH, strconv.FormatInt(info.Size(), 10))
		ctx.SetStatusCode(http.StatusOK)

		// response.Writer 实现了 io.ReaderFrom，io.Copy 会走 sendfile 优化。
		if _, copyErr := io.Copy(ctx.Response, file); copyErr != nil {
			log.Printf("流式下载中断: %v", copyErr)
			return
		}

		ctx.Response.Flush()
	}, "流式下载大文件", "文件", "流式")

	// 静态目录：把当前工作目录挂载到 /static。
	engine.SetStatic("/static", ".")

	// 启动服务并等待退出信号。
	go func() {
		if err := engine.Run(":8080"); err != nil {
			log.Printf("HTTP 服务退出: %v", err)
		}
	}()

	if err := engine.WaitSignalAndShutdown(5*time.Second, nil); err != nil {
		log.Printf("优雅关闭失败: %v", err)
	}
}

// normalizeFilePath 去掉通配符带来的前导斜杠，避免被当作绝对路径。
func normalizeFilePath(filePath string) string {
	filePath = strings.TrimPrefix(strings.TrimSpace(filePath), "/")
	if filePath == "" {
		return "."
	}

	return filePath
}
