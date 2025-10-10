package admin

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"path/filepath"
	"runtime"
	"time"

	"gitee.com/leminewx/leego"
)

//go:embed views
var adminFS embed.FS

// Admin 管理系统结构体
type Admin struct {
	addr   string
	start  time.Time
	engine *leego.Engine
}

// New 创建一个新的管理系统实例
func New(engine *leego.Engine, addr string) *Admin {
	return &Admin{
		addr:   addr,
		start:  time.Now(),
		engine: engine,
	}
}

// Register 注册管理系统路由
func (own *Admin) Register() error {
	// 获取嵌入的静态文件
	staticFS, err := fs.Sub(adminFS, "views")
	if err != nil {
		return fmt.Errorf("leego: admin: failed to get admin static files: %v", err)
	}

	// 注册静态文件服务
	group := own.engine.Group("/views")
	if err := group.SetStatics("/", "", false); err != nil {
		return fmt.Errorf("leego: admin: failed to set admin static files: %v", err)
	}

	// 提供静态文件
	fileServer := http.FileServer(http.FS(staticFS))
	group.GET("/", func(ctx *leego.Context) {
		fileServer.ServeHTTP(ctx.Response, ctx.Request)
	})

	// 注册API接口
	apiGroup := a.engine.Group(filepath.Join(a.mountPath, "api"))
	apiGroup.GET("/service", a.handleServiceInfo)
	apiGroup.GET("/metrics", a.handleMetrics)
	apiGroup.GET("/apis", a.handleApis)
	apiGroup.GET("/requests", a.handleRecentRequests)
	apiGroup.GET("/system", a.handleSystemInfo)

	return nil
}

// handleServiceInfo 处理服务信息请求
func (a *Admin) handleServiceInfo(ctx *leego.Context) {
	ctx.JSON(http.StatusOK, leego.H{
		"serviceName": a.engine.GetServiceName(),
		"startTime":   a.startTime.Format("2006-01-02 15:04:05"),
		"listenAddr":  a.listenAddr,
		"status":      "running",
	})
}

// handleMetrics 处理指标数据请求
func (a *Admin) handleMetrics(ctx *leego.Context) {
	// 实际实现中应该从collector获取真实数据
	ctx.JSON(http.StatusOK, leego.H{
		"totalApis":     len(a.engine.GetCollectors()),
		"todayRequests": 1528,
		"avgResponse":   "45ms",
		"errorRate":     "1.2%",
	})
}

// handleApis 处理接口列表请求
func (a *Admin) handleApis(ctx *leego.Context) {
	// 实际实现中应该从engine获取真实接口数据
	collectors := a.engine.GetCollectors()
	apis := make([]leego.H, 0, len(collectors))

	// 这里简化处理，实际应遍历collectors获取真实数据
	for name := range collectors {
		apis = append(apis, leego.H{
			"name":  name,
			"calls": 100 + len(apis)*50,
			"avg":   fmt.Sprintf("%dms", 20+len(apis)*5),
			"error": fmt.Sprintf("%.1f%%", 0.5+float64(len(apis))*0.2),
		})
	}

	ctx.JSON(http.StatusOK, apis)
}

// handleRecentRequests 处理最近请求数据
func (a *Admin) handleRecentRequests(ctx *leego.Context) {
	// 实际实现中应该从日志或记录器获取真实请求数据
	ctx.JSON(http.StatusOK, []leego.H{
		{
			"time":     time.Now().Add(-5 * time.Minute).Format("15:04:05"),
			"method":   "GET",
			"path":     "/api/users",
			"status":   200,
			"duration": "32ms",
			"ip":       "192.168.1.100",
		},
		{
			"time":     time.Now().Add(-6 * time.Minute).Format("15:04:05"),
			"method":   "POST",
			"path":     "/api/login",
			"status":   200,
			"duration": "45ms",
			"ip":       "192.168.1.101",
		},
	})
}

// handleSystemInfo 处理系统信息请求
func (a *Admin) handleSystemInfo(ctx *leego.Context) {
	// 获取系统信息
	memStats := &runtime.MemStats{}
	runtime.ReadMemStats(memStats)
	totalMem := memStats.Sys / 1024 / 1024
	usedMem := memStats.Alloc / 1024 / 1024

	// 获取操作系统信息
	osInfo := fmt.Sprintf("%s %s", runtime.GOOS, runtime.GOARCH)

	ctx.JSON(http.StatusOK, leego.H{
		"os":           osInfo,
		"cpu":          runtime.NumCPU(),
		"memory":       fmt.Sprintf("%dMB / %dMB", usedMem, totalMem),
		"goVersion":    runtime.Version(),
		"numGoroutine": runtime.NumGoroutine(),
	})
}
