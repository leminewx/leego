// Package main 是一个用于压力测试的基准服务。
//
// 它提供三个端点，配合 harness/stresstest 包使用：
//   - GET /examples：返回固定 JSON，用于测量框架自身的吞吐上限；
//   - GET /metadata：返回全部路由指标，用于观察压测期间的采集开销；
//   - GET /files/*：静态文件服务，用于测量文件传输性能。
package main

import (
	"log"
	"net/http"
	"time"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/harness/assetpath"
	"gitee.com/leminewx/leego/middlewares"
)

func main() {
	engine := leego.NewEngine(
		leego.UseEngineName("leego-stress-base"),
		leego.DisableStartupLog(),
	)
	engine.UseMiddlewares(
		middlewares.NewLogger().AdaptToMiddleware(),
		middlewares.NewRecovery().AdaptToMiddleware(),
	)

	engine.GET("/examples", func(ctx *leego.Context) {
		_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{"code": "00000", "desc": "ok"})
	}, "验证基本接口", "压测", "JSON")

	engine.GET("/metadata", func(ctx *leego.Context) {
		_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{
			"code": "00000",
			"desc": "ok",
			"data": engine.CollectReport(),
		})
	}, "获取所有路由监控数据", "压测", "监控数据")

	// 静态文件：挂载演示数据目录 harness/testdata，供文件传输性能测试。
	engine.SetStatic("/files", assetpath.GetDir(), "静态文件服务接口", "压测", "静态文件")

	go func() {
		if err := engine.Run(":8080"); err != nil {
			log.Printf("HTTP 服务退出: %v", err)
		}
	}()

	if err := engine.WaitSignalAndShutdown(5*time.Second, nil); err != nil {
		log.Printf("优雅关闭失败: %v", err)
	}
}
