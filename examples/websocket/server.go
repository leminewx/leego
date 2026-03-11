package main

// func main() {
// 	engine := leego.NewEngine("examples").
// 		WithURLBlacklist("/.well-known/appspecific/com.chrome.devtools.json")

// 	engine.WithMiddlewares(
// 		middleware.Recover(),
// 		middleware.Log(),
// 		middleware.Cors(cors.New()),
// 		func(handle leego.HandleFunc) leego.HandleFunc {
// 			return func(ctx *leego.Context) {
// 				handle(ctx)
// 				if ctx.GetStatusCode() == http.StatusNotFound {
// 					fmt.Println("Oh my god, not found")
// 				}
// 			}
// 		},
// 	)

// 	// 创建 WebSocket 配置
// 	config := &upgrader.Config{
// 		ReadTimeout:  60 * time.Second,
// 		WriteTimeout: 10 * time.Second,
// 		OriginCheckFunc: func(req *http.Request) bool {
// 			// 在生产环境中应该验证来源
// 			return true
// 		},
// 	}

// 	// 创建 WebSocket 管理器
// 	wsManager := websocket.NewManager(config)

// 	// 启动管理器
// 	ctx, cancel := context.WithCancel(context.Background())
// 	defer cancel()

// 	go wsManager.Start(ctx)

// 	// 设置路由
// 	http.HandleFunc("/ws", wsManager.HandleWebSocket)
// 	http.HandleFunc("/ws/stats", func(w http.ResponseWriter, r *http.Request) {
// 		// 返回连接统计信息
// 		stats := map[string]interface{}{
// 			"connection_count": wsManager.ConnectionCount(),
// 			"connections":      wsManager.GetClients(),
// 		}
// 		w.Header().Set("Content-Type", "application/json")
// 		json.NewEncoder(w).Encode(stats)
// 	})

// 	// 启动 HTTP 服务器
// 	server := &http.Server{
// 		Addr:         ":8080",
// 		ReadTimeout:  15 * time.Second,
// 		WriteTimeout: 15 * time.Second,
// 	}

// 	// 优雅关闭
// 	go func() {
// 		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
// 			log.Fatalf("Server error: %v", err)
// 		}
// 	}()

// 	log.Println("WebSocket server started on :8080")

// 	// 等待中断信号
// 	quit := make(chan os.Signal, 1)
// 	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
// 	<-quit

// 	log.Println("Shutting down server...")

// 	// 优雅关闭
// 	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
// 	defer shutdownCancel()

// 	if err := server.Shutdown(shutdownCtx); err != nil {
// 		log.Fatalf("Server shutdown error: %v", err)
// 	}

// 	cancel() // 停止 WebSocket 管理器
// 	log.Println("Server stopped")
// }
