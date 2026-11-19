// Command harness 启动 leego 的开发者体验服务。
//
// 用法：
//
//	go run ./harness/cmd/harness
//	go run ./harness/cmd/harness -addr 0.0.0.0:9090 -data ./tmp
//
// 启动后访问 http://127.0.0.1:8080 即可逐项体验框架能力；
// 按 Ctrl+C 或发送 SIGTERM 触发优雅关闭。
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"gitee.com/leminewx/leego/harness"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "监听地址")
	dataDir := flag.String("data", "", "数据目录，默认位于系统临时目录")
	flag.Parse()

	options := []harness.Option{harness.UseAddr(*addr)}
	if *dataDir != "" {
		options = append(options, harness.UseDataDir(*dataDir))
	}

	service, err := harness.NewService(options...)
	if err != nil {
		log.Fatalf("初始化服务失败: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err = service.Run(ctx); err != nil {
		log.Fatalf("服务异常退出: %v", err)
	}

	log.Print("leego harness 已退出")
}
