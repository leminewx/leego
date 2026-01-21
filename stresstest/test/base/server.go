package main

import (
	"log"
	"net/http"
	_ "net/http/pprof"
	"time"

	"gitee.com/leminewx/leego"
)

func main() {
	engine := leego.NewEngine()
	engine.GET("/test", func(ctx *leego.Context) {
		_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{"code": "00000", "desc": "ok"})
	}, "验证基本接口", "v1", "JSON")

	engine.GET("/metadata", func(ctx *leego.Context) {
		collectors := engine.GetAllHandlerCollectors()
		_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{"code": "00000", "desc": "ok", "data": collectors})
	}, "获取所有路由监控数据", "v1", "监控数据")

	engine.SetStatic("/files", ".", "静态文件服务接口", "文件", "下载")

	// 启动服务
	go func() {
		server := &http.Server{
			Addr: ":8080",

			Handler: engine.GetHTTPHandler(),
		}

		_ = server.ListenAndServe()
	}()

	if err := engine.WaitSignalAndShutdown(5*time.Second, nil); err != nil {
		log.Printf("Faild to shutdown service: %v", err)
	}
}
