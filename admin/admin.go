package admin

import (
	"crypto/rsa"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"sync"
	"time"

	"gitee.com/leminewx/gokit/cryptox/rsax"
	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/middleware/jwt"
)

//go:embed views
var adminFS embed.FS

// Admin 定义管理系统的数据结构
type Admin struct {
	// 服务启动时间
	start time.Time

	// 用户业务服务
	service *leego.Engine

	// 后台管理服务
	admin         *leego.Engine      // 后台管理服务
	config        *Config            // 后台服务配置
	authenticator *jwt.Authenticator // 后台服务的认证管理器
	staticHandler http.Handler       // 静态文件处理器

	// 管理员账户
	user            *Administrator  // 账户
	privateKey      *rsa.PrivateKey // 私钥
	publicKey       *rsa.PublicKey  // 公钥
	publicKeyString string          // 公钥字符串
	nowTokenBytes   []byte          // JWT 新令牌字符串
	oldTokenBytes   []byte          // JWT 旧令牌字符串

	lock sync.RWMutex
}

// New 创建一个新的管理系统实例
func New(config Config) *Admin {

	return &Admin{
		start:  time.Now(),
		admin:  leego.NewEngine("admin"),
		config: &config,
	}
}

func (own *Admin) init() error {
	// 初始化管理员账户
	own.initAdministrator()

	//	初始化密钥信息
	if err := own.initKeys(); err != nil {
		return err
	}
}

// initAdministrator 初始化管理员账户
func (own *Admin) initAdministrator() {
	own.user = &Administrator{
		Emails:   own.config.Administrator.Emails,
		Username: "admin",
		Password: "admin.123",
	}

	if own.config.Administrator.Username != "" {
		own.user.Username = own.config.Administrator.Username
	}

	if own.config.Administrator.Password != "" {
		own.user.Password = own.config.Administrator.Password
	}
}

// initKeys 初始化密钥信息
func (own *Admin) initKeys() (err error) {
	if own.publicKey, own.privateKey, err = rsax.GenerateRSAKeyPair(128); err != nil {
		return
	}

	own.publicKeyString, err = rsax.ReadPublicKeyToPEMString(own.publicKey)
	return
}

func (own *Admin) initAuthenticator() {
	own.authenticator = jwt.NewHS256Authenticator("adminAuthenticator", []byte(DEFAULT_AUTHENTICATOR_KEY)).
		WithSkipPaths("/auth/pkey", "/auth/login"). // 认证跳过这两个接口
		WithExceptedIssuer("leego_admin").          // 设置签发者
		WithExpiresAfterSeconds(3600 * 24).         // JWT 有效期 1 天
		WithValidateStandardClaims(true)            // 校验标准声明
}

func (own *Admin) initStaticHandler() error {
	// 获取嵌入的静态文件
	staticFS, err := fs.Sub(adminFS, "views")
	if err != nil {
		return fmt.Errorf("leego: admin: failed to get admin static files: %v", err)
	}

	// 创建静态文件处理器
	own.staticHandler = http.FileServer(http.FS(staticFS))
	return nil
}

// Register 注册管理系统路由
func (own *Admin) Register(service *leego.Engine) error {
	if service == nil {
		return fmt.Errorf("service is nil")
	}

	// 获取嵌入的静态文件
	staticFS, err := fs.Sub(adminFS, "views")
	if err != nil {
		return fmt.Errorf("leego: admin: failed to get admin static files: %v", err)
	}

	// 注册静态文件服务组
	groupStatic := own.admin.Group("/views")
	{

		fileServer := http.FileServer(http.FS(staticFS))
		groupStatic.GET("/", func(ctx *leego.Context) {
			fileServer.ServeHTTP(ctx.Response, ctx.Request)
		})
	}

	// 注册 API 接口组
	apiGroup := own.service.Group("/apis")
	apiGroup.GET("/apis", own.handleApis)
	apiGroup.GET("/service", own.handleServiceInfo)
	apiGroup.GET("/metrics", own.handleMetrics)
	apiGroup.GET("/requests", own.handleRecentRequests)
	apiGroup.GET("/system", own.handleSystemInfo)

	return nil
}

//}
//
//// handleServiceInfo 处理服务信息请求
//func (a *Admin) handleServiceInfo(ctx *leego.Context) {
//	ctx.JSON(http.StatusOK, leego.H{
//		"serviceName": a.engine.GetServiceName(),
//		"startTime":   a.startTime.Format("2006-01-02 15:04:05"),
//		"listenAddr":  a.listenAddr,
//		"status":      "running",
//	})
//}
//
//// handleMetrics 处理指标数据请求
//func (a *Admin) handleMetrics(ctx *leego.Context) {
//	// 实际实现中应该从collector获取真实数据
//	ctx.JSON(http.StatusOK, leego.H{
//		"totalApis":     len(a.engine.GetCollectors()),
//		"todayRequests": 1528,
//		"avgResponse":   "45ms",
//		"errorRate":     "1.2%",
//	})
//}
//
//// handleApis 处理接口列表请求
//func (a *Admin) handleApis(ctx *leego.Context) {
//	// 实际实现中应该从engine获取真实接口数据
//	collectors := a.engine.GetCollectors()
//	apis := make([]leego.H, 0, len(collectors))
//
//	// 这里简化处理，实际应遍历collectors获取真实数据
//	for name := range collectors {
//		apis = append(apis, leego.H{
//			"name":  name,
//			"calls": 100 + len(apis)*50,
//			"avg":   fmt.Sprintf("%dms", 20+len(apis)*5),
//			"error": fmt.Sprintf("%.1f%%", 0.5+float64(len(apis))*0.2),
//		})
//	}
//
//	ctx.JSON(http.StatusOK, apis)
//}
//
//// handleRecentRequests 处理最近请求数据
//func (a *Admin) handleRecentRequests(ctx *leego.Context) {
//	// 实际实现中应该从日志或记录器获取真实请求数据
//	ctx.JSON(http.StatusOK, []leego.H{
//		{
//			"time":     time.Now().Add(-5 * time.Minute).Format("15:04:05"),
//			"method":   "GET",
//			"path":     "/api/users",
//			"status":   200,
//			"duration": "32ms",
//			"ip":       "192.168.1.100",
//		},
//		{
//			"time":     time.Now().Add(-6 * time.Minute).Format("15:04:05"),
//			"method":   "POST",
//			"path":     "/api/login",
//			"status":   200,
//			"duration": "45ms",
//			"ip":       "192.168.1.101",
//		},
//	})
//}
//
//// handleSystemInfo 处理系统信息请求
//func (a *Admin) handleSystemInfo(ctx *leego.Context) {
//	// 获取系统信息
//	memStats := &runtime.MemStats{}
//	runtime.ReadMemStats(memStats)
//	totalMem := memStats.Sys / 1024 / 1024
//	usedMem := memStats.Alloc / 1024 / 1024
//
//	// 获取操作系统信息
//	osInfo := fmt.Sprintf("%s %s", runtime.GOOS, runtime.GOARCH)
//
//	ctx.JSON(http.StatusOK, leego.H{
//		"os":           osInfo,
//		"cpu":          runtime.NumCPU(),
//		"memory":       fmt.Sprintf("%dMB / %dMB", usedMem, totalMem),
//		"goVersion":    runtime.Version(),
//		"numGoroutine": runtime.NumGoroutine(),
//	})
//}
