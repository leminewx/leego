package main

import (
	"net/http"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/middleware"
)

func main() {
	engine := leego.NewEngine()
	engine.UseMiddlewareFuncs(
		middleware.Log(),
		middleware.Recover(),
	)

	engine.GET("/apis/files/show/*filepath", func(ctx *leego.Context) {
		filepath := ctx.GetPathValue("filepath")
		ctx.ResponseFileContent(filepath)
	})
	engine.GET("/apis/files/download/*filepath", func(ctx *leego.Context) {
		filepath := ctx.GetPathValue("filepath")
		ctx.ResponseFileAttachment(filepath)
	})

	fs := http.Dir(".")
	engine.GET("/apis/files/show/fs/*filepath", func(ctx *leego.Context) {
		filepath := ctx.GetPathValue("filepath")
		ctx.ResponseFileContentFromFS(filepath, fs)
	})
	engine.GET("/apis/files/download/fs/*filepath", func(ctx *leego.Context) {
		filepath := ctx.GetPathValue("filepath")
		ctx.ResponseFileAttachmentFromFS(filepath, fs)
	})

	_ = engine.Run(":8080")
}
