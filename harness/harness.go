// Package harness 汇总 leego 的开发者体验资产。
//
// 目录结构：
//
//	harness/testutil     基于标准库的 HTTP 测试客户端与断言辅助；
//	harness/stresstest   并发压测入口与延迟统计；
//	harness/examples     可独立运行的功能示例；
//	harness.Service      把框架扩展能力装配成可直接体验的实体服务。
//
// Service 的目标是“一条命令快速体验框架”：
//
//	go run ./harness/cmd/harness
//
// 启动后访问 http://127.0.0.1:8080 即可逐项体验会话、缓存、限流、
// 分片上传、SSE、反向代理、定时任务、链路追踪、指标与 OpenAPI 文档。
package harness

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/dashboard"
	"gitee.com/leminewx/leego/gateway"
	"gitee.com/leminewx/leego/job"
	"gitee.com/leminewx/leego/metrics"
	"gitee.com/leminewx/leego/middlewares"
	"gitee.com/leminewx/leego/middlewares/cache"
	"gitee.com/leminewx/leego/middlewares/compress"
	"gitee.com/leminewx/leego/middlewares/cors"
	"gitee.com/leminewx/leego/middlewares/limiter"
	"gitee.com/leminewx/leego/middlewares/session"
	middletrace "gitee.com/leminewx/leego/middlewares/trace"
	"gitee.com/leminewx/leego/response/sse"
	"gitee.com/leminewx/leego/upload"
)

//go:embed statics/index.html
var indexPage []byte

// 演示服务使用的默认常量。
const (
	// _DEFAULT_ADDR 为默认监听地址。
	_DEFAULT_ADDR = "127.0.0.1:8080"
	// _DEFAULT_DATA_DIR_NAME 为默认数据目录名，位于系统临时目录下。
	_DEFAULT_DATA_DIR_NAME = "leego-harness"
	// _DEFAULT_SESSION_TTL 为会话默认有效期。
	_DEFAULT_SESSION_TTL = 30 * time.Minute
	// _DEFAULT_CACHE_TTL 为缓存默认新鲜期。
	_DEFAULT_CACHE_TTL = 5 * time.Second
	// _DEFAULT_CACHE_STALE 为缓存默认陈旧期。
	_DEFAULT_CACHE_STALE = 15 * time.Second
	// _DEFAULT_RATE_LIMIT 为默认每秒令牌数。
	_DEFAULT_RATE_LIMIT = 5
	// _DEFAULT_RATE_BURST 为默认突发容量。
	_DEFAULT_RATE_BURST = 10
	// _DEFAULT_METRICS_INTERVAL 为默认指标采集间隔。
	_DEFAULT_METRICS_INTERVAL = 2 * time.Second
	// _DEFAULT_CHUNK_SIZE 为默认分片大小。
	_DEFAULT_CHUNK_SIZE = 256 << 10
	// _MAX_UPLOAD_SIZE 为演示服务允许的单文件上限。
	_MAX_UPLOAD_SIZE = 512 << 20
	// _MAX_UPLOAD_PARTS 为演示服务允许的分片数量上限。
	_MAX_UPLOAD_PARTS = 8192
	// _CANARY_TAG 为演示服务识别灰度流量的标签值。
	_CANARY_TAG = "v2"
	// _TRACE_LIMIT 为内存中保留的链路条数。
	_TRACE_LIMIT = 256
)

// defaultSessionSecret 为演示服务的会话签名密钥。
//
// 生产环境必须由配置中心或环境变量注入，且各实例保持一致；
// 这里的固定值只用于让开发者在本地重启后会话仍然有效。
var defaultSessionSecret = []byte("leego-harness-demo-secret-please-override")

// Config 定义体验服务的运行参数。
type Config struct {
	// Addr 为监听地址，空值时使用 127.0.0.1:8080。
	Addr string
	// DataDir 为数据目录，存放上传分片与录像；空值时使用系统临时目录。
	DataDir string
	// SessionTTL 为会话有效期，非正数时使用 30 分钟。
	SessionTTL time.Duration
	// SessionSecret 为会话 Cookie 的 HMAC 密钥。
	SessionSecret []byte
	// CacheTTL 为缓存新鲜期，非正数时使用 5 秒。
	CacheTTL time.Duration
	// CacheStale 为缓存陈旧期，0 表示使用默认值 15 秒。
	CacheStale time.Duration
	// RateLimit 为令牌桶每秒补充的令牌数，非正数时使用 5。
	RateLimit int
	// RateBurst 为令牌桶突发容量，非正数时使用 10。
	RateBurst int
	// MetricsInterval 为指标采集间隔，非正数时使用 2 秒。
	MetricsInterval time.Duration
	// ChunkSize 为默认分片大小，非正数时使用 256 KiB。
	ChunkSize int64
	// EnableLogging 表示是否启用访问日志；测试时通常关闭以减少噪声。
	EnableLogging bool
}

// NewDefaultConfig 返回推荐配置。
func NewDefaultConfig() Config {
	return Config{
		Addr:            _DEFAULT_ADDR,
		DataDir:         filepath.Join(os.TempDir(), _DEFAULT_DATA_DIR_NAME),
		SessionTTL:      _DEFAULT_SESSION_TTL,
		SessionSecret:   append([]byte(nil), defaultSessionSecret...),
		CacheTTL:        _DEFAULT_CACHE_TTL,
		CacheStale:      _DEFAULT_CACHE_STALE,
		RateLimit:       _DEFAULT_RATE_LIMIT,
		RateBurst:       _DEFAULT_RATE_BURST,
		MetricsInterval: _DEFAULT_METRICS_INTERVAL,
		ChunkSize:       _DEFAULT_CHUNK_SIZE,
		EnableLogging:   true,
	}
}

// normalize 修正非法配置并填充默认值。
func (own Config) normalize() Config {
	defaults := NewDefaultConfig()

	own.Addr = strings.TrimSpace(own.Addr)
	if own.Addr == "" {
		own.Addr = defaults.Addr
	}

	own.DataDir = strings.TrimSpace(own.DataDir)
	if own.DataDir == "" {
		own.DataDir = defaults.DataDir
	}

	if own.SessionTTL <= 0 {
		own.SessionTTL = defaults.SessionTTL
	}

	if len(own.SessionSecret) == 0 {
		own.SessionSecret = append([]byte(nil), defaults.SessionSecret...)
	}

	if own.CacheTTL <= 0 {
		own.CacheTTL = defaults.CacheTTL
	}

	if own.CacheStale < 0 {
		own.CacheStale = 0
	} else if own.CacheStale == 0 {
		own.CacheStale = defaults.CacheStale
	}

	if own.RateLimit <= 0 {
		own.RateLimit = defaults.RateLimit
	}

	if own.RateBurst <= 0 {
		own.RateBurst = defaults.RateBurst
	}

	if own.MetricsInterval <= 0 {
		own.MetricsInterval = defaults.MetricsInterval
	}

	if own.ChunkSize <= 0 {
		own.ChunkSize = defaults.ChunkSize
	}

	return own
}

// clone 返回配置副本，切片字段做深拷贝。
func (own Config) clone() Config {
	copied := own
	copied.SessionSecret = append([]byte(nil), own.SessionSecret...)
	return copied
}

// Option 修改 Config。
type Option func(*Config)

// WithAddr 设置监听地址。
func WithAddr(addr string) Option {
	return func(config *Config) {
		config.Addr = addr
	}
}

// WithDataDir 设置数据目录。
func WithDataDir(dir string) Option {
	return func(config *Config) {
		config.DataDir = dir
	}
}

// WithSessionTTL 设置会话有效期。
func WithSessionTTL(ttl time.Duration) Option {
	return func(config *Config) {
		config.SessionTTL = ttl
	}
}

// WithSessionSecret 设置会话签名密钥；传 nil 时保留默认值。
func WithSessionSecret(secret []byte) Option {
	return func(config *Config) {
		config.SessionSecret = append([]byte(nil), secret...)
	}
}

// WithCacheTTL 设置缓存新鲜期。
func WithCacheTTL(ttl time.Duration) Option {
	return func(config *Config) {
		config.CacheTTL = ttl
	}
}

// WithRateLimit 设置令牌桶速率与突发容量。
func WithRateLimit(limit, burst int) Option {
	return func(config *Config) {
		config.RateLimit = limit
		config.RateBurst = burst
	}
}

// WithMetricsInterval 设置指标采集间隔。
func WithMetricsInterval(interval time.Duration) Option {
	return func(config *Config) {
		config.MetricsInterval = interval
	}
}

// WithChunkSize 设置默认分片大小。
func WithChunkSize(size int64) Option {
	return func(config *Config) {
		config.ChunkSize = size
	}
}

// WithLogging 控制是否启用访问日志。
func WithLogging(enabled bool) Option {
	return func(config *Config) {
		config.EnableLogging = enabled
	}
}

// Service 是装配好的体验服务。
//
// 一个 Service 对应一个独立的引擎实例与一套后台资源：
// 会话存储、指标存储、链路导出器、任务调度器、分片存储与两个内部上游。
// 所有后台资源都注册为引擎关闭钩子，Shutdown 之后不会残留协程。
type Service struct {
	// config 为规范化后的配置。
	config Config
	// engine 为框架引擎。
	engine *leego.Engine
	// uploads 为分片上传存储。
	uploads *upload.LocalStorage
	// sessions 为服务端会话存储。
	sessions *session.MemoryStore
	// scheduler 为定时任务调度器。
	scheduler *job.Scheduler
	// tracer 为链路追踪中间件。
	tracer *middletrace.Tracer
	// exporter 保存最近若干条链路，供 /api/traces 查询。
	exporter *middletrace.MemoryExporter
	// metricStore 保存指标历史。
	metricStore metrics.Store
	// bucket 为限流器。
	bucket *limiter.TokenBucketLimiter
	// balancer 为反向代理负载均衡器。
	balancer *gateway.Balancer
	// upstreams 为内部演示上游。
	upstreams []*http.Server
	// startedAt 为服务创建时间。
	startedAt time.Time
	// ticks 记录定时任务的成功执行次数。
	ticks atomic.Int64
	// cancel 用于停止后台任务的上下文。
	cancel context.CancelFunc
	// serverMu 保护 server。
	serverMu sync.Mutex
	// server 为运行期的 HTTP 服务器；未运行时为 nil。
	server *http.Server
}

// NewService 装配体验服务。
//
// 创建过程不启动对外监听，但会启动两个内部上游与后台任务；
// 调用方在退出时必须执行 Shutdown，或直接用 Run 托管生命周期。
func NewService(options ...Option) (*Service, error) {
	config := NewDefaultConfig()
	for _, option := range options {
		if option != nil {
			option(&config)
		}
	}
	config = config.normalize()

	if err := os.MkdirAll(config.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("harness: create data dir: %w", err)
	}

	uploads, err := upload.NewLocalStorage(filepath.Join(config.DataDir, "upload"))
	if err != nil {
		return nil, fmt.Errorf("harness: create upload storage: %w", err)
	}

	own := &Service{
		config:      config,
		uploads:     uploads,
		sessions:    session.NewMemoryStore(),
		scheduler:   job.NewScheduler(job.WithWorkers(2)),
		exporter:    middletrace.NewMemoryExporter(_TRACE_LIMIT),
		metricStore: metrics.NewMemoryStore(5*time.Minute, 300),
		startedAt:   time.Now(),
	}
	own.tracer = middletrace.NewTracer(own.exporter, middletrace.WithFlushInterval(500*time.Millisecond))
	own.bucket = limiter.NewTokenBucketLimiter(config.RateLimit, config.RateBurst, limiter.Config{PollInterval: 10 * time.Millisecond})

	if err = own.startUpstreams(); err != nil {
		own.sessions.Close()
		return nil, err
	}

	own.engine = own.buildEngine()
	if err = own.registerPlatformRoutes(); err != nil {
		own.closeUpstreams()
		own.sessions.Close()
		return nil, err
	}

	own.startBackground()
	return own, nil
}

// GetEngine 返回底层引擎，便于测试或二次扩展。
func (own *Service) GetEngine() *leego.Engine {
	return own.engine
}

// GetConfig 返回配置副本。
func (own *Service) GetConfig() Config {
	return own.config.clone()
}

// GetHandler 返回可直接挂到 HTTP 服务器或测试服务器上的处理器。
func (own *Service) GetHandler() http.Handler {
	return own.engine.GetHTTPHandler()
}

// Run 在配置地址上提供服务，并在 ctx 结束时优雅关闭。
//
// 该方法会阻塞到服务退出；关闭顺序为：先停止接收新请求并等待在途请求，
// 再执行引擎关闭钩子（任务调度器、链路导出、会话存储、内部上游等）。
func (own *Service) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	own.engine.Prepare()

	listener, err := net.Listen("tcp", own.config.Addr)
	if err != nil {
		cancelCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = own.Shutdown(cancelCtx)
		cancel()
		return err
	}

	server := &http.Server{
		Handler:           own.GetHandler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	own.serverMu.Lock()
	own.server = server
	own.serverMu.Unlock()

	log.Printf("leego harness 已启动：http://%s（数据目录 %s）", listener.Addr().String(), own.config.DataDir)

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()

	select {
	case err = <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err = own.Shutdown(shutdownCtx)
		cancel()
	}

	return err
}

// Shutdown 关闭对外服务器并释放后台资源。
func (own *Service) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	own.serverMu.Lock()
	server := own.server
	own.server = nil
	own.serverMu.Unlock()

	var serverErr error
	if server != nil {
		serverErr = server.Shutdown(ctx)
	}

	return errors.Join(serverErr, own.engine.Shutdown(ctx))
}

// buildEngine 创建引擎并注册全部业务路由。
func (own *Service) buildEngine() *leego.Engine {
	leego.SetLogoStyle(leego.LOGO_STYLE_ASCII)
	leego.ShowLogo()
	engine := leego.NewEngine(
		leego.WithEngineName("leego-harness"),
		leego.WithMaxBodyBytes(_MAX_UPLOAD_SIZE),
		leego.WithServerTimeouts(10*time.Second, 0, 0, 120*time.Second),
	)

	chain := make([]leego.MiddlewareFunc, 0, 6)
	chain = append(chain, middlewares.NewRecovery().AdaptToMiddleware())
	if own.config.EnableLogging {
		chain = append(chain, middlewares.NewLogger().AdaptToMiddleware())
	}
	chain = append(
		chain,
		own.tracer.AdaptToMiddleware(),
		cors.New().AdaptToMiddleware(),
		compress.New(compress.NewDefaultConfig()).AdaptToMiddleware(),
	)
	engine.UseMiddlewares(chain...)

	engine.GET("/", own.serveIndexPage, "index", _TAG_OVERVIEW)
	engine.GET("/api/hello", own.serveHello, "hello", _TAG_OVERVIEW)
	engine.GET("/api/healthz", own.serveHealth, "health", _TAG_OVERVIEW)
	engine.GET("/api/echo", own.serveEcho, "echo", _TAG_OVERVIEW)
	engine.POST("/api/echo", own.serveEcho, "echo-post", _TAG_OVERVIEW)

	own.registerSessionRoutes(engine)
	own.registerCacheRoutes(engine)
	own.registerLimiterRoutes(engine)
	own.registerUploadRoutes(engine)
	own.registerGatewayRoutes(engine)

	engine.GET("/api/events", own.serveEvents, "events", _TAG_STREAM)
	engine.GET("/api/jobs", own.serveJobs, "jobs", _TAG_JOBS)
	engine.GET("/api/traces", own.serveTraces, "traces", _TAG_TRACE)

	return engine
}

// registerSessionRoutes 注册会话演示路由。
func (own *Service) registerSessionRoutes(engine *leego.Engine) {
	group := engine.NewGroup("/api/session")
	group.UseMiddlewares(session.New(
		own.sessions,
		session.WithTTL(own.config.SessionTTL),
		session.WithSecret(own.config.SessionSecret),
		session.WithSameSite(http.SameSiteLaxMode),
	).AdaptToMiddleware())

	group.GET("/current", own.serveSessionCurrent, "session-current", _TAG_SESSION)
	group.POST("/login", own.serveSessionLogin, "session-login", _TAG_SESSION)
	group.POST("/logout", own.serveSessionLogout, "session-logout", _TAG_SESSION)
}

// registerCacheRoutes 注册响应缓存演示路由。
func (own *Service) registerCacheRoutes(engine *leego.Engine) {
	group := engine.NewGroup("/api/cache")
	group.UseMiddlewares(cache.New(cache.Config{
		TTL:   own.config.CacheTTL,
		Stale: own.config.CacheStale,
		Vary:  []string{"Accept-Encoding"},
	}).AdaptToMiddleware())

	group.GET("/timestamp", own.serveCacheTimestamp, "cache-timestamp", _TAG_CACHE)
}

// registerLimiterRoutes 注册限流演示路由。
func (own *Service) registerLimiterRoutes(engine *leego.Engine) {
	group := engine.NewGroup("/api/limited")
	// 非阻塞判定：额度不足立即返回 429，前端连点即可观察到效果。
	group.UseMiddlewares(own.bucket.AdaptToMiddleware())

	group.GET("/ping", own.serveLimitedPing, "limited-ping", _TAG_LIMITER)
}

// registerUploadRoutes 注册分片上传与下载路由。
func (own *Service) registerUploadRoutes(engine *leego.Engine) {
	handler := upload.NewHandler(
		own.uploads,
		upload.WithMaxSize(_MAX_UPLOAD_SIZE),
		upload.WithDefaultPartSize(own.config.ChunkSize),
		upload.WithMaxParts(_MAX_UPLOAD_PARTS),
	)

	engine.POST("/api/upload", handler, "upload-init", _TAG_UPLOAD)
	engine.PUT("/api/upload", handler, "upload-part", _TAG_UPLOAD)
	engine.GET("/api/upload", handler, "upload-status", _TAG_UPLOAD)
	engine.DELETE("/api/upload", handler, "upload-abort", _TAG_UPLOAD)
	engine.GET("/api/upload/download/*filepath", own.serveUploadDownload, "upload-download", _TAG_UPLOAD)
}

// registerGatewayRoutes 注册反向代理演示路由。
func (own *Service) registerGatewayRoutes(engine *leego.Engine) {
	handler := gateway.NewProxy(
		own.balancer,
		gateway.WithTimeout(3*time.Second),
		gateway.WithErrorHandler(func(ctx *leego.Context, target *url.URL, err error) {
			log.Printf("harness: gateway error: target=%s err=%v", target.Redacted(), err)
		}),
	)

	engine.GET("/api/gateway/*path", handler, "gateway", _TAG_GATEWAY)
	engine.POST("/api/gateway/*path", handler, "gateway-post", _TAG_GATEWAY)
}

// registerPlatformRoutes 注册指标、分析后台与 OpenAPI 文档。
//
// 这三个能力都会在注册期读取已注册路由，因此必须最后调用。
func (own *Service) registerPlatformRoutes() error {
	if err := own.engine.RegisterMetrics(
		"/metrics",
		own.metricStore,
		leego.WithMetricsInterval(own.config.MetricsInterval),
	); err != nil {
		return fmt.Errorf("harness: register metrics: %w", err)
	}

	if err := own.engine.RegisterDashboard(
		"/dashboard",
		// harness 仅监听本地回环，显式关闭鉴权以便直接查看指标。
		dashboard.WithoutAuthorizer(),
		dashboard.WithTitle("leego harness"),
		dashboard.WithRefreshInterval(2*time.Second),
	); err != nil {
		return fmt.Errorf("harness: register dashboard: %w", err)
	}

	if err := own.registerOpenAPI(); err != nil {
		return err
	}

	return nil
}

// startUpstreams 启动两个内部上游并创建负载均衡器。
//
// 上游监听 127.0.0.1 的随机端口，仅用于演示加权轮询与灰度分流；
// 关闭由 closeUpstreams 负责。
func (own *Service) startUpstreams() error {
	specs := []struct {
		name   string
		weight int
		tag    string
	}{
		{name: "stable", weight: 3, tag: "stable"},
		{name: "canary", weight: 1, tag: _CANARY_TAG},
	}

	upstreams := make([]gateway.Upstream, 0, len(specs))
	for _, spec := range specs {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			own.closeUpstreams()
			return fmt.Errorf("harness: listen upstream %s: %w", spec.name, err)
		}

		server := &http.Server{Handler: newUpstreamHandler(spec.name), ReadHeaderTimeout: 5 * time.Second}
		own.upstreams = append(own.upstreams, server)
		go func() {
			// Serve 只会在 Close/Shutdown 或监听失败时返回；
			// 演示服务没有恢复动作，错误交给引擎关闭流程处理。
			_ = server.Serve(listener)
		}()

		target, err := url.Parse("http://" + listener.Addr().String())
		if err != nil {
			own.closeUpstreams()
			return fmt.Errorf("harness: parse upstream %s: %w", spec.name, err)
		}

		upstreams = append(upstreams, gateway.Upstream{URL: target, Weight: spec.weight, Tag: spec.tag})
	}

	balancer, err := gateway.NewBalancer(upstreams...)
	if err != nil {
		own.closeUpstreams()
		return fmt.Errorf("harness: create balancer: %w", err)
	}

	own.balancer = balancer
	return nil
}

// closeUpstreams 立即关闭所有内部上游。
func (own *Service) closeUpstreams() {
	for _, server := range own.upstreams {
		_ = server.Close()
	}
}

// startBackground 启动定时任务并注册全部关闭钩子。
func (own *Service) startBackground() {
	own.scheduler.ScheduleEvery(3*time.Second, job.JobFunc(func(context.Context) error {
		own.ticks.Add(1)
		return nil
	}))

	runCtx, cancel := context.WithCancel(context.Background())
	own.cancel = cancel
	own.scheduler.Start(runCtx)

	// 钩子按注册顺序的逆序执行：先关上游，再停链路导出与会话，
	// 最后停止调度器并取消后台上下文。
	_ = own.engine.AddShutdownHook(func(_ context.Context) error {
		own.closeUpstreams()
		return nil
	})
	_ = own.engine.AddShutdownHook(func(ctx context.Context) error {
		return own.tracer.Shutdown(ctx)
	})
	_ = own.engine.AddShutdownHook(func(_ context.Context) error {
		return own.sessions.Close()
	})
	_ = own.engine.AddShutdownHook(func(ctx context.Context) error {
		err := own.scheduler.Stop(ctx)
		if own.cancel != nil {
			own.cancel()
		}
		return err
	})
}

// serveIndexPage 输出内嵌的单页前端。
func (own *Service) serveIndexPage(ctx *leego.Context) {
	_ = ctx.ResponseHTML(http.StatusOK, string(indexPage))
}

// serveHello 返回服务概览。
func (own *Service) serveHello(ctx *leego.Context) {
	_ = ctx.ResponseJSON(http.StatusOK, map[string]any{
		"service":    own.engine.GetName(),
		"version":    _HARNESS_VERSION,
		"go":         runtime.Version(),
		"started_at": own.startedAt,
		"uptime":     time.Since(own.startedAt).Round(time.Second).String(),
		"config": map[string]any{
			"addr":        own.config.Addr,
			"data_dir":    own.config.DataDir,
			"cache_ttl":   own.config.CacheTTL.String(),
			"cache_stale": own.config.CacheStale.String(),
			"rate_limit":  own.config.RateLimit,
			"rate_burst":  own.config.RateBurst,
			"chunk_size":  own.config.ChunkSize,
		},
		"links": map[string]string{
			"dashboard":  "/dashboard",
			"openapi":    "/openapi",
			"metrics":    "/metrics",
			"prometheus": "/metrics/prometheus",
		},
	})
}

// serveHealth 返回健康状态。
func (own *Service) serveHealth(ctx *leego.Context) {
	stats := own.scheduler.GetStats()
	_ = ctx.ResponseJSON(http.StatusOK, map[string]any{
		"status": "ok",
		"uptime": time.Since(own.startedAt).Round(time.Second).String(),
		"jobs":   stats,
	})
}

// serveEcho 回显请求信息，便于观察中间件对请求的改写。
func (own *Service) serveEcho(ctx *leego.Context) {
	body, err := ctx.ReadBody()
	if err != nil {
		_ = ctx.ResponseJSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	query := make(map[string]string, len(ctx.GetURL().Query()))
	for key := range ctx.GetURL().Query() {
		query[key] = ctx.GetQueryValue(key)
	}

	headers := make(map[string]string, len(ctx.Request.Header))
	for name := range ctx.Request.Header {
		headers[name] = ctx.GetRequestHeader(name)
	}

	_ = ctx.ResponseJSON(http.StatusOK, map[string]any{
		"method":    ctx.Request.Method,
		"path":      ctx.Request.URL.Path,
		"query":     query,
		"headers":   headers,
		"body":      string(body),
		"client_ip": ctx.GetClientIP(),
		"trace":     traceIDFromRequest(ctx),
	})
}

// serveSessionCurrent 返回当前会话内容。
func (own *Service) serveSessionCurrent(ctx *leego.Context) {
	current, ok := session.GetFromContext(ctx.Request.Context())
	if !ok {
		_ = ctx.ResponseJSON(http.StatusInternalServerError, map[string]string{"error": "session middleware missing"})
		return
	}

	name, _ := current.Get("name")
	loginAt, _ := current.Get("login_at")
	_ = ctx.ResponseJSON(http.StatusOK, map[string]any{
		"id":       current.GetID(),
		"new":      current.IsNew(),
		"name":     name,
		"login_at": loginAt,
		"keys":     current.GetKeys(),
	})
}

// serveSessionLogin 登录并轮换会话 ID，避免会话固定攻击。
func (own *Service) serveSessionLogin(ctx *leego.Context) {
	var request struct {
		Name string `json:"name"`
	}

	body, err := ctx.ReadBody()
	if err != nil {
		_ = ctx.ResponseJSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if err = json.Unmarshal(body, &request); err != nil {
		_ = ctx.ResponseJSON(http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return
	}

	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" {
		_ = ctx.ResponseJSON(http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	current, ok := session.GetFromContext(ctx.Request.Context())
	if !ok {
		_ = ctx.ResponseJSON(http.StatusInternalServerError, map[string]string{"error": "session middleware missing"})
		return
	}

	if err = current.Regenerate(ctx.Request.Context()); err != nil {
		_ = ctx.ResponseJSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	loginAt := time.Now().Format(time.RFC3339)
	current.Set("name", request.Name)
	current.Set("login_at", loginAt)

	_ = ctx.ResponseJSON(http.StatusOK, map[string]any{
		"id":       current.GetID(),
		"name":     request.Name,
		"login_at": loginAt,
	})
}

// serveSessionLogout 销毁会话。
func (own *Service) serveSessionLogout(ctx *leego.Context) {
	current, ok := session.GetFromContext(ctx.Request.Context())
	if !ok {
		_ = ctx.ResponseJSON(http.StatusInternalServerError, map[string]string{"error": "session middleware missing"})
		return
	}

	current.Destroy()
	_ = ctx.ResponseJSON(http.StatusOK, map[string]bool{"ok": true})
}

// serveCacheTimestamp 模拟一次“昂贵”计算，用于观察缓存命中。
func (own *Service) serveCacheTimestamp(ctx *leego.Context) {
	// 随机耗时让 X-Cache 的效果更直观：命中时不会出现这段延迟。
	cost := time.Duration(rand.IntN(30)+10) * time.Millisecond
	time.Sleep(cost)

	ctx.SetResponseHeader("Cache-Control", "public, max-age="+strconv.Itoa(int(own.config.CacheTTL.Seconds())))
	_ = ctx.ResponseJSON(http.StatusOK, map[string]any{
		"value":      time.Now().Format("15:04:05.000"),
		"cost_ms":    cost.Milliseconds(),
		"cache_hint": "连续请求两次，第二次应命中并被 X-Cache 标记",
	})
}

// serveLimitedPing 返回限流放行结果。
func (own *Service) serveLimitedPing(ctx *leego.Context) {
	limit, burst := own.bucket.GetConfig()
	_ = ctx.ResponseJSON(http.StatusOK, map[string]any{
		"ok":    true,
		"limit": limit,
		"burst": burst,
		"at":    time.Now().Format(time.RFC3339Nano),
	})
}

// serveUploadDownload 输出已合并的文件。
func (own *Service) serveUploadDownload(ctx *leego.Context) {
	name := strings.TrimSpace(ctx.GetPathValue("filepath"))
	name = strings.TrimPrefix(name, "/")

	// 只允许访问 files/ 下的最终文件，防止越权读取会话与分片目录。
	if !strings.HasPrefix(name, "files/") || strings.Contains(name, "..") {
		ctx.ResponseError(http.StatusBadRequest, "invalid file path")
		return
	}

	ctx.ResponseAttachmentFromFS("/"+name, http.Dir(own.uploads.GetRoot()))
}

// serveEvents 通过 SSE 持续推送服务运行状态。
func (own *Service) serveEvents(ctx *leego.Context) {
	_ = ctx.ResponseSSE(func(requestCtx context.Context) <-chan *sse.Message {
		messages := make(chan *sse.Message, 8)
		go own.streamEvents(requestCtx, messages)
		return messages
	})
}

// streamEvents 是 SSE 的生产者协程。
func (own *Service) streamEvents(requestCtx context.Context, messages chan<- *sse.Message) {
	ticker := time.NewTicker(time.Second)
	// 冷路径启动的协程需要可靠回收定时器，这里保留 defer。
	defer ticker.Stop()

	messages <- sse.NewMessageWithEvent("hello", own.encodeEventPayload())

	for {
		select {
		case <-requestCtx.Done():
			close(messages)
			return
		case <-ticker.C:
			messages <- sse.NewMessageWithEvent("tick", own.encodeEventPayload())
		}
	}
}

// encodeEventPayload 序列化一次状态快照。
func (own *Service) encodeEventPayload() []byte {
	stats := own.scheduler.GetStats()
	payload, err := json.Marshal(map[string]any{
		"at":        time.Now().Format(time.RFC3339Nano),
		"uptime":    time.Since(own.startedAt).Round(time.Second).String(),
		"ticks":     own.ticks.Load(),
		"job_runs":  stats.Runs,
		"job_queue": stats.Pending,
	})
	if err != nil {
		return []byte(`{"error":"encode failed"}`)
	}

	return payload
}

// serveJobs 返回定时任务统计。
func (own *Service) serveJobs(ctx *leego.Context) {
	stats := own.scheduler.GetStats()
	_ = ctx.ResponseJSON(http.StatusOK, map[string]any{
		"ticks":      own.ticks.Load(),
		"workers":    own.scheduler.GetWorkers(),
		"stats":      stats,
		"uptime":     time.Since(own.startedAt).Round(time.Second).String(),
		"interval":   "3s",
		"registered": 1,
	})
}

// serveTraces 返回最近导出的链路片段。
func (own *Service) serveTraces(ctx *leego.Context) {
	spans := own.exporter.GetSpans()
	total := len(spans)

	limit := 20
	if total < limit {
		limit = total
	}

	items := make([]map[string]any, 0, limit)
	for index := total - 1; index >= total-limit; index-- {
		span := spans[index]
		items = append(items, map[string]any{
			"trace_id":    span.TraceID,
			"span_id":     span.SpanID,
			"name":        span.Name,
			"status":      span.Status,
			"duration_ms": float64(span.GetDuration().Microseconds()) / 1000,
			"at":          span.Start,
			"tags":        span.Attrs,
		})
	}

	_ = ctx.ResponseJSON(http.StatusOK, map[string]any{
		"total": total,
		"items": items,
	})
}

// traceIDFromRequest 从请求上下文中读取链路 ID。
func traceIDFromRequest(ctx *leego.Context) string {
	span, ok := middletrace.TraceFrom(ctx.Request.Context())
	if !ok {
		return ""
	}

	return span.TraceID
}

// newUpstreamHandler 创建内部上游的应答处理器。
func newUpstreamHandler(name string) http.Handler {
	return http.HandlerFunc(func(resp http.ResponseWriter, request *http.Request) {
		resp.Header().Set("Content-Type", "application/json; charset=utf-8")
		resp.Header().Set("X-Upstream", name)
		_ = json.NewEncoder(resp).Encode(map[string]any{
			"upstream": name,
			"method":   request.Method,
			"path":     request.URL.Path,
			"query":    request.URL.RawQuery,
			"host":     request.Host,
			"tag":      request.Header.Get("X-Leego-Canary"),
			"at":       time.Now().Format(time.RFC3339Nano),
		})
	})
}
