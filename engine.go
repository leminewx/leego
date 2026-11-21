package leego

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"gitee.com/leminewx/leego/dashboard"
	"gitee.com/leminewx/leego/logging"
	"gitee.com/leminewx/leego/middlewares/collector"
	"gitee.com/leminewx/leego/response"
	"gitee.com/leminewx/leego/tree"
)

const (
	// _DEFAULT_MULTIPART_MEMORY 定义解析 multipart 表单时默认使用的内存上限（32 MiB）。
	_DEFAULT_MULTIPART_MEMORY int64 = 32 << 20
	// _DEFAULT_MAX_BODY_BYTES 定义单个请求体默认允许的最大字节数（8 MiB）。
	//
	// 该上限用于防御“声明超大 Content-Length 后慢速发送”的内存耗尽攻击，
	// 业务方可通过 WithMaxBodyBytes 调整，或在具体路由中自行覆盖。
	_DEFAULT_MAX_BODY_BYTES int64 = 8 << 20
	// _DEFAULT_READ_HEADER_TIMEOUT 定义读取请求头的默认超时时间。
	//
	// 未设置该超时会让服务暴露在 Slowloris 慢速攻击下，因此默认启用。
	_DEFAULT_READ_HEADER_TIMEOUT = 10 * time.Second
	// _DEFAULT_READ_TIMEOUT 定义读取完整请求（含请求体）的默认超时时间。
	_DEFAULT_READ_TIMEOUT = 60 * time.Second
	// _DEFAULT_WRITE_TIMEOUT 定义写入响应的默认超时时间。
	_DEFAULT_WRITE_TIMEOUT = 60 * time.Second
	// _DEFAULT_IDLE_TIMEOUT 定义 keep-alive 空闲连接的默认超时时间。
	_DEFAULT_IDLE_TIMEOUT = 120 * time.Second
	// _DEFAULT_MAX_HEADER_BYTES 定义请求头的默认最大字节数（1 MiB）。
	_DEFAULT_MAX_HEADER_BYTES = 1 << 20
)

// ErrorHandlerFunc 定义统一错误处理函数的类型。
//
// 引擎在 Context.Fail 被调用时依次尝试：自定义错误处理器 → 默认错误格式。
type ErrorHandlerFunc func(ctx *Context, err error)

// EngineConfig 定义引擎的完整配置。
//
// 所有字段都有安全的默认值；通过 WithXxx 选项函数按需覆盖，
// 避免“零值即不安全”的静默默认。
type EngineConfig struct {
	// Name 为服务名称，出现在日志、指标后台与错误响应中。
	Name string
	// MaxMultipartMemory 为解析 multipart 表单时可用的最大内存（字节）。
	MaxMultipartMemory int64
	// MaxBodyBytes 为单个请求体的最大字节数；小于等于 0 表示不限制。
	MaxBodyBytes int64
	// StrictJSON 为 true 时，JSON 绑定会拒绝未知字段。
	StrictJSON bool
	// H2C 为 true 时允许明文 HTTP/2 与 HTTP/1.1 共用同一端口。
	//
	// 由 http.Server.Protocols 控制，不依赖处理器包装；客户端必须直接发送
	// HTTP/2 连接序言（前导知识），用 Upgrade: h2c 握手的请求按 HTTP/1.1 处理。
	H2C bool
	// DisableStartupLog 为 true 时不打印路由注册信息。
	DisableStartupLog bool
	// ReadHeaderTimeout 为读取请求头的超时时间。
	ReadHeaderTimeout time.Duration
	// ReadTimeout 为读取完整请求的超时时间。
	ReadTimeout time.Duration
	// WriteTimeout 为写入响应的超时时间。
	//
	// 注意：SSE / WebSocket 等长连接必须设为 0（不限制），
	// 否则连接会在默认 60 秒后被切断；可用 WithWriteTimeout 单独调整。
	WriteTimeout time.Duration
	// IdleTimeout 为空闲连接的超时时间。
	IdleTimeout time.Duration
	// MaxHeaderBytes 为请求头的最大字节数。
	MaxHeaderBytes int
	// TrustedProxies 为可信代理的 CIDR 列表，用于安全解析 X-Forwarded-For。
	TrustedProxies []string
}

// NewDefaultEngineConfig 返回一份带安全默认值的引擎配置。
func NewDefaultEngineConfig() EngineConfig {
	return EngineConfig{
		Name:               "leego",
		MaxMultipartMemory: _DEFAULT_MULTIPART_MEMORY,
		MaxBodyBytes:       _DEFAULT_MAX_BODY_BYTES,
		StrictJSON:         false,
		H2C:                false,
		DisableStartupLog:  false,
		ReadHeaderTimeout:  _DEFAULT_READ_HEADER_TIMEOUT,
		ReadTimeout:        _DEFAULT_READ_TIMEOUT,
		WriteTimeout:       _DEFAULT_WRITE_TIMEOUT,
		IdleTimeout:        _DEFAULT_IDLE_TIMEOUT,
		MaxHeaderBytes:     _DEFAULT_MAX_HEADER_BYTES,
	}
}

// Clone 返回配置的深拷贝。
//
// TrustedProxies 会被复制，避免调用方修改返回值时影响引擎内部状态。
func (own EngineConfig) Clone() EngineConfig {
	clone := own
	if len(own.TrustedProxies) > 0 {
		clone.TrustedProxies = append([]string(nil), own.TrustedProxies...)
	}

	return clone
}

// EngineOption 定义引擎配置选项的函数类型。
type EngineOption func(config *EngineConfig)

// WithEngineName 设置服务名称。
func WithEngineName(name string) EngineOption {
	return func(config *EngineConfig) {
		if name = strings.TrimSpace(name); name != "" {
			config.Name = name
		}
	}
}

// WithMultipartMemory 设置解析 multipart 表单时允许使用的最大内存。
func WithMultipartMemory(bytes int64) EngineOption {
	return func(config *EngineConfig) {
		if bytes > 0 {
			config.MaxMultipartMemory = bytes
		}
	}
}

// WithMaxBodyBytes 设置单个请求体的最大字节数；传入小于等于 0 的值表示不限制。
func WithMaxBodyBytes(bytes int64) EngineOption {
	return func(config *EngineConfig) {
		config.MaxBodyBytes = bytes
	}
}

// WithStrictJSON 设置 JSON 绑定是否拒绝未知字段。
func WithStrictJSON(strict bool) EngineOption {
	return func(config *EngineConfig) {
		config.StrictJSON = strict
	}
}

// WithH2C 启用明文 HTTP/2（h2c）。
//
// 该能力由标准库 http.Server.Protocols 提供（Go 1.24+），不再依赖
// golang.org/x/net/http2/h2c：同一端口既接受 HTTP/1.1，也接受直接发送
// HTTP/2 连接序言的连接。依赖 Upgrade: h2c 握手升级的客户端只会走 HTTP/1.1，
// 因为标准库只实现前导知识（prior knowledge）一种方式。
//
// Run / RunTLS / RunUnix / RunFd / RunListener 会自动把协议集写入服务器；
// 自行创建 http.Server 时，需要把它的 Protocols 设为 GetHTTPProtocols()。
func WithH2C() EngineOption {
	return func(config *EngineConfig) {
		config.H2C = true
	}
}

// WithServerTimeouts 一次性设置服务端四个超时时间。
//
// 任一参数小于等于 0 时保留该配置项的默认值，避免误把超时设为 0 而退化为无限等待。
func WithServerTimeouts(readHeader, read, write, idle time.Duration) EngineOption {
	return func(config *EngineConfig) {
		if readHeader > 0 {
			config.ReadHeaderTimeout = readHeader
		}
		if read > 0 {
			config.ReadTimeout = read
		}
		if write > 0 {
			config.WriteTimeout = write
		}
		if idle > 0 {
			config.IdleTimeout = idle
		}
	}
}

// WithWriteTimeout 单独设置响应写入超时。
//
// 与 WithServerTimeouts 不同，这里允许传入 0：SSE / WebSocket 等长连接
// 依赖该值关闭全局写入超时，否则默认 60 秒会直接切断连接。
// 负数视为 0。
func WithWriteTimeout(timeout time.Duration) EngineOption {
	return func(config *EngineConfig) {
		if timeout < 0 {
			timeout = 0
		}

		config.WriteTimeout = timeout
	}
}

// WithTrustedProxies 设置可信代理的 CIDR 列表。
//
// 只有来自这些网段的请求才会被信任 X-Forwarded-For / X-Real-IP；
// 传入无效 CIDR 时会被忽略，并在错误返回值中汇总（见 Engine.SetTrustedProxies）。
func WithTrustedProxies(cidrs ...string) EngineOption {
	return func(config *EngineConfig) {
		config.TrustedProxies = append([]string(nil), cidrs...)
	}
}

// DisableStartupLog 关闭启动时的路由注册信息输出。
func DisableStartupLog() EngineOption {
	return func(config *EngineConfig) {
		config.DisableStartupLog = true
	}
}

// Engine 定义 HTTP 引擎。
//
// Engine 同时是根路由组，因此可以直接调用 GET/POST 等方法注册路由。
//
// 生命周期约定：
//   - 注册期：NewEngine 之后到第一次处理请求（或第一次调用 Prepare）之前，
//     允许注册路由、中间件、模板与仪表盘；
//   - 运行期：Prepare 会冻结路由树并折叠中间件链，此时再注册路由会 panic，
//     与标准库 http.ServeMux 的 fail-fast 策略一致；
//   - 关闭期：调用 Shutdown 或 WaitSignalAndShutdown 优雅关闭。
type Engine struct {
	// group 为根路由组，使 Engine 直接具备路由注册能力。
	*group

	// config 保存引擎配置的不可变快照；通过原子指针整体替换，
	// 使运行期读取无需加锁，也不会与 Set* 的并发写入产生数据竞争。
	config atomic.Pointer[EngineConfig]
	// trustedProxies 保存解析后的可信代理网段的不可变快照。
	trustedProxies atomic.Pointer[[]netip.Prefix]

	// router 为路由器。
	router Router
	// serverMu 保护 server 指针。
	serverMu sync.RWMutex
	// server 为当前运行的 http.Server；未运行时为 nil。
	server *http.Server

	// handlersMu 保护 handlers 与 printBuilder。
	handlersMu sync.RWMutex
	// handlers 保存“方法 + 模式 -> 处理器”的索引。
	handlers map[string]Handler
	// printBuilder 缓冲路由注册信息，启动时一次性输出。
	printBuilder *strings.Builder

	// errorHandlerMu 保护 errorHandler。
	errorHandlerMu sync.RWMutex
	// errorHandler 为自定义错误处理器。
	errorHandler ErrorHandlerFunc

	// writerPool 复用响应包装器，消除每请求一次的堆分配。
	writerPool *response.WriterPool

	// templatesMu 保护模板与模板函数。
	templatesMu sync.RWMutex
	// templates 保存预加载的 HTML 模板。
	templates *template.Template
	// templateFuncs 保存模板函数表。
	templateFuncs template.FuncMap

	// prepareOnce 保证 Prepare 只执行一次。
	prepareOnce sync.Once
	// prepared 标记引擎是否已进入运行期。
	prepared atomic.Bool
	// globalHandle 保存折叠后的全局中间件链。
	globalHandle atomic.Pointer[HandleFunc]

	// shutdownMu 保护关闭钩子列表。
	shutdownMu sync.Mutex
	// shutdownHooks 保存按注册顺序排列的关闭钩子。
	shutdownHooks []ShutdownHook
}

// NewEngine 创建并初始化一个引擎。
//
// 参数 options 用于覆盖默认配置。未传入任何选项时，引擎使用
// NewDefaultEngineConfig 提供的安全默认值（含四个服务端超时）。
func NewEngine(options ...EngineOption) *Engine {
	checkGoVersion()

	config := NewDefaultEngineConfig()
	for _, option := range options {
		if option != nil {
			option(&config)
		}
	}

	engine := &Engine{
		handlers:     make(map[string]Handler),
		printBuilder: &strings.Builder{},
		writerPool:   response.NewWriterPool(),
	}
	engine.config.Store(&config)

	// 可信代理属于启动期配置，解析失败时保留合法项并记录警告。
	if err := engine.SetTrustedProxies(config.TrustedProxies...); err != nil {
		logging.LogWarn("%v", err)
	}

	engine.group = &group{engine: engine}
	engine.router = newRouter(engine)

	// 默认全局链：直接分发到路由匹配逻辑。
	handle := HandleFunc(engine.dispatch)
	engine.globalHandle.Store(&handle)

	engine.printBuilder.WriteString("Creating routes:\n")
	return engine
}

// GetConfig 返回当前配置的深拷贝。
func (own *Engine) GetConfig() EngineConfig {
	return own.getConfig().Clone()
}

// getConfig 返回当前配置的只读快照。
//
// 返回值指向不可变对象，调用方不得修改；需要修改时使用 updateConfig。
func (own *Engine) getConfig() *EngineConfig {
	if config := own.config.Load(); config != nil {
		return config
	}

	// 零值引擎（未经过 NewEngine）兜底，避免空指针。
	config := NewDefaultEngineConfig()
	own.config.Store(&config)
	return &config
}

// updateConfig 以写时复制方式更新配置。
//
// 每次更新都基于当前快照克隆一份再整体替换，保证并发读取始终看到
// 一份完整一致的配置，不会读到“改了一半”的中间状态。
func (own *Engine) updateConfig(mutate func(*EngineConfig)) {
	for {
		current := own.getConfig()
		next := current.Clone()
		mutate(&next)
		if own.config.CompareAndSwap(current, &next) {
			return
		}
	}
}

// SetName 设置服务名称。
func (own *Engine) SetName(name string) {
	if name = strings.TrimSpace(name); name != "" {
		own.updateConfig(func(config *EngineConfig) { config.Name = name })
	}
}

// GetName 返回服务名称。
func (own *Engine) GetName() string {
	return own.getConfig().Name
}

// SetMaxMultipartMemory 设置解析 multipart 表单时允许使用的最大内存。
func (own *Engine) SetMaxMultipartMemory(bytes int64) {
	if bytes > 0 {
		own.updateConfig(func(config *EngineConfig) { config.MaxMultipartMemory = bytes })
	}
}

// getMultipartMemory 返回解析 multipart 表单时可用的内存上限。
func (own *Engine) getMultipartMemory() int64 {
	if own == nil {
		return _DEFAULT_MULTIPART_MEMORY
	}

	if config := own.getConfig(); config.MaxMultipartMemory > 0 {
		return config.MaxMultipartMemory
	}

	return _DEFAULT_MULTIPART_MEMORY
}

// SetErrorHandler 设置自定义错误处理器。
//
// 传入 nil 表示恢复默认的 JSON 错误响应。
func (own *Engine) SetErrorHandler(handler ErrorHandlerFunc) {
	own.errorHandlerMu.Lock()
	own.errorHandler = handler
	own.errorHandlerMu.Unlock()
}

// getErrorHandler 返回当前错误处理器；未配置时返回 nil。
func (own *Engine) getErrorHandler() ErrorHandlerFunc {
	own.errorHandlerMu.RLock()
	handler := own.errorHandler
	own.errorHandlerMu.RUnlock()
	return handler
}

// SetTrustedProxies 设置可信代理网段。
//
// 参数 cidrs 支持单个 IP（如 10.0.0.1）与 CIDR（如 10.0.0.0/8）。
// 函数会跳过无效项并返回聚合错误，合法项仍然生效。
func (own *Engine) SetTrustedProxies(cidrs ...string) error {
	prefixes := make([]netip.Prefix, 0, len(cidrs))
	invalid := make([]string, 0)

	for _, cidr := range cidrs {
		prefix, err := parsePrefix(cidr)
		if err != nil {
			invalid = append(invalid, cidr)
			continue
		}

		prefixes = append(prefixes, prefix)
	}

	own.trustedProxies.Store(&prefixes)
	own.updateConfig(func(config *EngineConfig) {
		config.TrustedProxies = append([]string(nil), cidrs...)
	})
	if len(invalid) > 0 {
		return fmt.Errorf("leego: invalid trusted proxy entries: %s", strings.Join(invalid, ", "))
	}

	return nil
}

// parsePrefix 将 IP 或 CIDR 解析为网段。
func parsePrefix(value string) (netip.Prefix, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return netip.Prefix{}, errors.New("empty proxy entry")
	}

	if prefix, err := netip.ParsePrefix(value); err == nil {
		return prefix.Masked(), nil
	}

	addr, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Prefix{}, err
	}

	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

// GetTrustedProxies 返回可信代理网段的文本形式。
func (own *Engine) GetTrustedProxies() []string {
	return append([]string(nil), own.getConfig().TrustedProxies...)
}

// getTrustedProxies 返回可信代理网段的只读快照；调用方不得修改。
func (own *Engine) getTrustedProxies() []netip.Prefix {
	if prefixes := own.trustedProxies.Load(); prefixes != nil {
		return *prefixes
	}

	return nil
}

// LoadHTMLGlob 按通配模式加载 HTML 模板。
//
// 模板使用 html/template 解析，默认具备上下文转义能力。
func (own *Engine) LoadHTMLGlob(pattern string) error {
	tmpl, err := template.New(resolveTemplateName(pattern)).Funcs(own.getTemplateFuncs()).ParseGlob(pattern)
	if err != nil {
		return fmt.Errorf("leego: failed to load templates by glob %q: %w", pattern, err)
	}

	own.templatesMu.Lock()
	own.templates = tmpl
	own.templatesMu.Unlock()
	return nil
}

// LoadHTMLFiles 加载指定的 HTML 模板文件。
func (own *Engine) LoadHTMLFiles(files ...string) error {
	if len(files) == 0 {
		return errors.New("leego: no template files were provided")
	}

	name := resolveTemplateName(files[0])
	tmpl, err := template.New(name).Funcs(own.getTemplateFuncs()).ParseFiles(files...)
	if err != nil {
		return fmt.Errorf("leego: failed to load template files: %w", err)
	}

	own.templatesMu.Lock()
	own.templates = tmpl
	own.templatesMu.Unlock()
	return nil
}

// SetTemplateFuncs 设置模板函数表。
//
// 应在 LoadHTMLGlob / LoadHTMLFiles 之前调用，否则已加载的模板不会包含新函数。
func (own *Engine) SetTemplateFuncs(funcs template.FuncMap) {
	own.templatesMu.Lock()
	if len(funcs) == 0 {
		own.templateFuncs = nil
	} else {
		own.templateFuncs = funcs
	}
	own.templatesMu.Unlock()
}

// getTemplate 返回已加载的模板；未加载时返回 nil。
func (own *Engine) getTemplate() *template.Template {
	own.templatesMu.RLock()
	tmpl := own.templates
	own.templatesMu.RUnlock()
	return tmpl
}

// getTemplateFuncs 返回模板函数表的副本。
func (own *Engine) getTemplateFuncs() template.FuncMap {
	// 读取一次引用即可复制，无需在整个函数生命周期内持锁。
	own.templatesMu.RLock()
	funcs := own.templateFuncs
	own.templatesMu.RUnlock()

	if len(funcs) == 0 {
		return template.FuncMap{}
	}

	copied := make(template.FuncMap, len(funcs))
	for name, fn := range funcs {
		copied[name] = fn
	}

	return copied
}

// resolveTemplateName 返回路径的最后一段，用于给模板命名。
func resolveTemplateName(path string) string {
	path = strings.TrimSpace(path)
	if index := strings.LastIndexAny(path, `/\`); index >= 0 {
		path = path[index+1:]
	}

	if path == "" {
		return "leego"
	}

	return path
}

// GetHandler 返回指定方法与模式对应的处理器。
func (own *Engine) GetHandler(method, pattern string) (Handler, bool) {
	own.handlersMu.RLock()
	handler, ok := own.handlers[method+" "+pattern]
	own.handlersMu.RUnlock()
	return handler, ok
}

// GetAllHandlerSnapshots 返回全部路由的指标快照。
//
// 返回值按“方法 + 模式”排序，便于稳定展示与断言。
func (own *Engine) GetAllHandlerSnapshots() []collector.Snapshot {
	snapshots := own.collectSnapshots()
	sortSnapshots(snapshots)
	return snapshots
}

// CollectReport 汇总全部路由的指标，生成指标后台可直接渲染的报告。
func (own *Engine) CollectReport() collector.Report {
	return collector.NewReport(own.collectSnapshots())
}

// collectSnapshots 采集全部路由的快照（未排序）。
func (own *Engine) collectSnapshots() []collector.Snapshot {
	own.handlersMu.RLock()
	handlers := make([]Handler, 0, len(own.handlers))
	for _, handler := range own.handlers {
		handlers = append(handlers, handler)
	}
	own.handlersMu.RUnlock()

	snapshots := make([]collector.Snapshot, 0, len(handlers))
	for _, handler := range handlers {
		if monitor := handler.GetCollector(); monitor != nil {
			snapshots = append(snapshots, monitor.GetSnapshot())
		}
	}

	return snapshots
}

// sortSnapshots 按方法与模式排序快照。
func sortSnapshots(snapshots []collector.Snapshot) {
	slices.SortFunc(snapshots, func(a, b collector.Snapshot) int {
		if compare := strings.Compare(a.Method, b.Method); compare != 0 {
			return compare
		}

		return strings.Compare(a.Pattern, b.Pattern)
	})
}

// AddURLBlacklist 添加路由黑名单。
//
// 黑名单按“请求路径”精确匹配，命中后直接返回 403，不进入路由匹配。
func (own *Engine) AddURLBlacklist(urls ...string) {
	own.router.AddBlacklist(urls...)
}

// IsBlacklisted 判断给定路径是否在黑名单中。
func (own *Engine) IsBlacklisted(path string) bool {
	return own.router.IsBlacklisted(path)
}

// RegisterDashboard 在指定前缀下注册指标管理后台。
//
// 该函数会自动注册两个路由：
//   - GET  {prefix}          指标页面（HTML）
//   - GET  {prefix}/*path   页面静态资源与 JSON API
//
// 必须在服务启动（Prepare）之前调用；返回值非 nil 表示注册失败。
func (own *Engine) RegisterDashboard(prefix string, options ...dashboard.Option) error {
	options = append([]dashboard.Option{dashboard.WithProvider(own.CollectReport)}, options...)
	monitor, err := dashboard.New(options...)
	if err != nil {
		return err
	}

	prefix = normalizePrefix(prefix)

	// 直接在包内做适配，避免根包反向依赖 middleware 包造成循环依赖。
	handle := func(ctx *Context) {
		monitor.ServeHTTP(ctx.Response, ctx.Request)
	}

	own.GET(prefix, handle)
	own.GET(joinPath(prefix, "/*"+tree.WILDCARD_DEFAULT_NAME), handle)
	own.HEAD(prefix, handle)
	own.HEAD(joinPath(prefix, "/*"+tree.WILDCARD_DEFAULT_NAME), handle)
	return nil
}

// normalizePrefix 规范化仪表盘前缀。
func normalizePrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return "/metrics"
	}

	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}

	return strings.TrimRight(prefix, "/")
}

// joinPath 以安全的方式拼接两个 URL 路径片段。
func joinPath(left, right string) string {
	return strings.TrimRight(left, "/") + "/" + strings.TrimLeft(right, "/")
}

// Prepare 冻结路由与中间件，使引擎进入运行期。
//
// 该函数是幂等的，可安全地被 ServeHTTP、Run 等方法并发调用。
// 冻结之后：
//   - 路由树只读，查询无需加锁；
//   - 每条路由的中间件链只折叠一次；
//   - 全局中间件链只构建一次。
func (own *Engine) Prepare() {
	own.prepareOnce.Do(func() {
		// 1. 冻结路由树：此后并发查询无需加锁。
		own.router.Freeze()

		// 2. 冻结每条路由的中间件链，避免首次请求时在数据竞争下折叠。
		own.handlersMu.RLock()
		handlers := make([]Handler, 0, len(own.handlers))
		for _, handler := range own.handlers {
			handlers = append(handlers, handler)
		}
		own.handlersMu.RUnlock()

		for _, handler := range handlers {
			handler.Prepare()
		}

		// 3. 构建全局中间件链。
		chain := own.buildGlobalChain()
		own.globalHandle.Store(&chain)

		// 4. 标记运行期。
		own.prepared.Store(true)
	})
}

// IsPrepared 返回引擎是否已进入运行期。
func (own *Engine) IsPrepared() bool {
	return own.prepared.Load()
}

// buildGlobalChain 折叠根路由组上的中间件。
//
// 全局链作用于所有请求（含 404/405），因此 CORS、日志、恢复、
// 请求 ID 等中间件即使面对未匹配路由也能正常工作。
func (own *Engine) buildGlobalChain() HandleFunc {
	handle := HandleFunc(own.dispatch)

	middlewares := own.group.middlewares
	for index := len(middlewares) - 1; index >= 0; index-- {
		handle = middlewares[index](handle)
	}

	return handle
}

// serveHTTP 执行全局中间件链并处理单个请求。
func (own *Engine) serveHTTP(resp http.ResponseWriter, req *http.Request) {
	own.Prepare()

	ctx := own.createRequestContext(resp, req)

	// 池化对象必须在任何出口归还：panic 时不归还会让每次崩溃
	// 永久丢失一个上下文与写入器。
	defer own.destroyRequestContext(ctx)

	// 最后一道防线：用户没挂恢复中间件时，也不能让 panic 直接穿透到
	// net/http（连接被断开、客户端看不到任何响应）。
	defer func() {
		if recovered := recover(); recovered != nil {
			own.handlePanic(ctx, recovered)
		}
	}()

	handle := own.globalHandle.Load()
	if handle == nil {
		http.Error(resp, response.GetStatusLowerText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}

	(*handle)(ctx)
}

// handlePanic 记录 panic 堆栈，并在响应尚未开始时返回 500。
//
// http.ErrAbortHandler 保持标准库语义：上抛给 net/http 静默中止请求。
func (own *Engine) handlePanic(ctx *Context, recovered any) {
	if recovered == http.ErrAbortHandler {
		panic(recovered)
	}

	logging.LogWarn("leego: panic recovered: %v\n%s", recovered, debug.Stack())

	if ctx != nil && ctx.GetStatusCode() == 0 {
		ctx.ResponseError(http.StatusInternalServerError, response.GetStatusLowerText(http.StatusInternalServerError))
	}
}

// ServeHTTP 实现 http.Handler 接口。
func (own *Engine) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	own.serveHTTP(resp, req)
}

// createRequestContext 从对象池取出上下文并绑定请求与响应。
func (own *Engine) createRequestContext(resp http.ResponseWriter, req *http.Request) *Context {
	writer := own.writerPool.Acquire(resp)
	ctx := acquireContext()
	ctx.reset(own, writer, req)
	return ctx
}

// destroyRequestContext 归还上下文与响应包装器。
func (own *Engine) destroyRequestContext(ctx *Context) {
	if ctx == nil {
		return
	}

	if ctx.Response != nil {
		own.writerPool.Release(ctx.Response)
	}

	releaseContext(ctx)
}

// dispatch 完成黑名单检查、路由匹配与响应。
func (own *Engine) dispatch(ctx *Context) {
	path := ctx.Request.URL.Path

	// 1. 黑名单检查。
	if own.router.IsBlacklisted(path) {
		ctx.ResponseError(http.StatusForbidden, response.GetStatusLowerText(http.StatusForbidden))
		return
	}

	// 2. 路由匹配。
	handler, params, ok := own.router.Search(ctx.Request.Method, path)
	if !ok {
		own.handleUnmatched(ctx)
		return
	}

	// 3. 写入路由参数并执行处理器。
	ctx.setPathValues(params)
	own.serveMatched(ctx, handler)
}

// serveMatched 执行命中路由的处理器，并采集指标。
func (own *Engine) serveMatched(ctx *Context, handler Handler) {
	monitor := handler.GetCollector()
	if monitor != nil {
		monitor.Increase()
		start := time.Now()

		// 必须用 defer：处理器 panic 时由上层 Recovery 接管，
		// 若正常返回路径才回收，在途计数会永久泄漏。
		defer func() {
			status := ctx.GetStatusCode()
			if status == 0 {
				status = http.StatusInternalServerError
			}
			monitor.Decrease(status, time.Since(start))
		}()
	}

	if handler.IsEnabled() {
		handler.ServeHTTP(ctx)
	} else {
		ctx.ResponseError(http.StatusLocked, response.GetStatusLowerText(http.StatusLocked))
	}
}

// handleUnmatched 处理未命中路由的请求。
//
// 处理顺序：
//   - 若路径在其他方法下存在路由，返回 405 并携带 Allow 头；
//   - 若请求方法为 OPTIONS 且存在可用方法，返回 204 并携带 Allow 头；
//   - 否则返回 404。
func (own *Engine) handleUnmatched(ctx *Context) {
	allowed := own.router.CollectAllowedMethods(ctx.Request.URL.Path)
	if len(allowed) == 0 {
		ctx.ResponseError(http.StatusNotFound, response.GetStatusLowerText(http.StatusNotFound))
		return
	}

	ctx.SetResponseHeader("Allow", strings.Join(allowed, ", "))
	if ctx.Request.Method == http.MethodOptions {
		ctx.SetStatusCode(http.StatusNoContent)
		return
	}

	ctx.ResponseError(http.StatusMethodNotAllowed, response.GetStatusLowerText(http.StatusMethodNotAllowed))
}

// GetRoutes 返回已注册路由的只读描述。
//
// 返回值用于诊断、启动日志与文档生成，不包含业务处理器。
func (own *Engine) GetRoutes() []RouteInfo {
	own.handlersMu.RLock()
	routes := make([]RouteInfo, 0, len(own.handlers))
	for _, handler := range own.handlers {
		if handler == nil {
			continue
		}

		monitor := handler.GetCollector()
		if monitor == nil {
			continue
		}

		routes = append(routes, RouteInfo{
			Method:  monitor.GetMethod(),
			Pattern: monitor.GetPattern(),
			Name:    monitor.GetName(),
			Tags:    monitor.GetTags(),
			Enabled: handler.IsEnabled(),
		})
	}
	own.handlersMu.RUnlock()

	sortRoutes(routes)
	return routes
}

// RouteInfo 描述一条已注册路由的元信息。
type RouteInfo struct {
	// Method 为 HTTP 请求方法。
	Method string `json:"method"`
	// Pattern 为路由模式。
	Pattern string `json:"pattern"`
	// Name 为路由名称。
	Name string `json:"name,omitempty"`
	// Tags 为业务标签。
	Tags []string `json:"tags,omitempty"`
	// Enabled 表示路由是否启用。
	Enabled bool `json:"enabled"`
}

// sortRoutes 按方法与模式排序路由信息。
func sortRoutes(routes []RouteInfo) {
	slices.SortFunc(routes, func(a, b RouteInfo) int {
		if compare := strings.Compare(a.Method, b.Method); compare != 0 {
			return compare
		}

		return strings.Compare(a.Pattern, b.Pattern)
	})
}

// Run 在指定地址上监听并提供 HTTP 服务。
//
// 与标准库不同，优雅关闭导致的 http.ErrServerClosed 会被转换为 nil，
// 因此调用方可以写：
//
//	if err := engine.Run(":8080"); err != nil { ... }
//
// 而不会在正常关闭时误判为错误。
func (own *Engine) Run(addr string) error {
	return own.runServer(addr)
}

// RunTLS 在指定地址上监听并提供 HTTPS 服务。
func (own *Engine) RunTLS(addr, certFile, keyFile string) error {
	if certFile == "" || keyFile == "" {
		return errors.New("leego: certificate and key files are required")
	}

	own.Prepare()
	server := own.newServer(addr)
	own.setServer(server)
	own.logStartup(server.Addr, "https")

	err := server.ListenAndServeTLS(certFile, keyFile)
	return normalizeServeError(err)
}

// runServer 创建并启动明文 HTTP 服务。
func (own *Engine) runServer(addr string) error {
	own.Prepare()
	server := own.newServer(addr)
	own.setServer(server)
	own.logStartup(server.Addr, "http")

	err := server.ListenAndServe()
	return normalizeServeError(err)
}

// normalizeServeError 将优雅关闭产生的错误转换为 nil。
func normalizeServeError(err error) error {
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}

	return err
}

// newServer 基于当前配置创建 http.Server。
func (own *Engine) newServer(addr string) *http.Server {
	config := own.GetConfig()
	return &http.Server{
		Addr:              addr,
		Handler:           own.GetHTTPHandler(),
		Protocols:         own.GetHTTPProtocols(),
		ReadHeaderTimeout: config.ReadHeaderTimeout,
		ReadTimeout:       config.ReadTimeout,
		WriteTimeout:      config.WriteTimeout,
		IdleTimeout:       config.IdleTimeout,
		MaxHeaderBytes:    config.MaxHeaderBytes,
	}
}

// setServer 保存当前运行的服务器指针。
func (own *Engine) setServer(server *http.Server) {
	own.serverMu.Lock()
	own.server = server
	own.serverMu.Unlock()
}

// GetServer 返回当前运行的服务器；未运行时返回 nil。
func (own *Engine) GetServer() *http.Server {
	own.serverMu.RLock()
	server := own.server
	own.serverMu.RUnlock()
	return server
}

// logStartup 输出路由注册信息与监听地址。
func (own *Engine) logStartup(addr, scheme string) {
	config := own.getConfig()
	if config.DisableStartupLog {
		return
	}

	own.handlersMu.Lock()
	routes := own.printBuilder.String()
	own.printBuilder.Reset()
	own.handlersMu.Unlock()

	if routes != "" {
		logging.LogInfo("%s", routes)
	}

	// 长连接提示：写超时到点会直接断开 SSE/WebSocket 连接，
	// 而症状（连接使用一段时间后静默断开）很难让人联想到超时配置。
	if config.WriteTimeout > 0 {
		logging.LogInfo("Hint: WriteTimeout=%s will close long-lived responses; use leego.WithWriteTimeout(0) for SSE/WebSocket", config.WriteTimeout)
	}

	logging.LogInfo("Listening and serving %s on %s ...", strings.ToUpper(scheme), addr)
}

// RunUnix 使用 Unix 套接字提供 HTTP 服务。
func (own *Engine) RunUnix(file string) error {
	listener, err := net.Listen("unix", file)
	if err != nil {
		return err
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(file)
	}()

	return own.RunListener(listener)
}

// RunFd 使用指定文件描述符提供 HTTP 服务。
func (own *Engine) RunFd(fd int) error {
	file := os.NewFile(uintptr(fd), fmt.Sprintf("fd@%d", fd))
	if file == nil {
		return fmt.Errorf("leego: invalid file descriptor %d", fd)
	}
	defer func() { _ = file.Close() }()

	listener, err := net.FileListener(file)
	if err != nil {
		return err
	}
	defer func() { _ = listener.Close() }()

	return own.RunListener(listener)
}

// RunListener 使用给定监听器提供 HTTP 服务。
//
// 该函数同样会记录服务端指针，因此 WaitSignalAndShutdown 与 Shutdown
// 在 Unix 套接字、fd、自定义监听器场景下均能正常工作。
func (own *Engine) RunListener(listener net.Listener) error {
	if listener == nil {
		return errors.New("leego: nil listener")
	}

	own.Prepare()
	server := own.newServer(listener.Addr().String())
	own.setServer(server)
	own.logStartup(listener.Addr().String(), "http")

	err := server.Serve(listener)
	return normalizeServeError(err)
}

// GetHTTPHandler 返回可挂载到任意 http.Server 的处理器。
//
// h2c 由 http.Server.Protocols 控制，处理器本身无需包装；自行创建
// http.Server 时把 Protocols 设为 GetHTTPProtocols() 即可获得同样的协议支持。
func (own *Engine) GetHTTPHandler() http.Handler {
	return own
}

// GetHTTPProtocols 返回与当前配置对应的协议集合。
//
// 未启用 h2c 时返回 nil，表示沿用标准库默认（HTTP/1.1，TLS 上额外协商 HTTP/2）；
// 启用后返回同时包含 HTTP/1.1、HTTP/2 与明文 HTTP/2 的集合——保留 HTTP/2 是为了
// RunTLS 场景下 ALPN 仍能协商到 h2，而不是只提供 h2c。
func (own *Engine) GetHTTPProtocols() *http.Protocols {
	if !own.getConfig().H2C {
		return nil
	}

	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetHTTP2(true)
	protocols.SetUnencryptedHTTP2(true)
	return protocols
}

// Shutdown 优雅关闭服务器。
//
// 执行顺序：先停止接收新请求并等待在途请求结束，再逆序执行关闭钩子。
// 服务器尚未启动时仍然会执行钩子，因此定时任务、指标落盘等资源不会泄漏。
func (own *Engine) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	var serverErr error
	if server := own.GetServer(); server != nil {
		serverErr = server.Shutdown(ctx)
	}

	return errors.Join(serverErr, own.RunShutdownHooks(ctx))
}

// WaitSignalAndShutdown 等待 SIGINT/SIGTERM 后优雅关闭服务器。
//
// 参数：
//   - timeout：关闭超时时间；小于等于 0 时使用 10 秒；
//   - doBeforeShutdown：关闭前执行的回调，可为 nil。
func (own *Engine) WaitSignalAndShutdown(timeout time.Duration, doBeforeShutdown func() error) error {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)

	<-quit

	if doBeforeShutdown != nil {
		if err := doBeforeShutdown(); err != nil {
			return err
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := own.Shutdown(ctx); err != nil {
		logging.LogWarn("Failed to shutdown server: %v", err)
		return err
	}

	logging.LogInfo("Shutdown server successfully")
	return nil
}

// checkGoVersion 检查当前 Go 版本是否满足最低要求。
func checkGoVersion() {
	version := runtime.Version()
	if after, ok := strings.CutPrefix(version, "go"); ok {
		version = after
	}

	major, minor, _, err := getGoVersionParts(version)
	if err != nil {
		return
	}

	// 仅当主版本为 1 且次版本小于 25 时告警；Go 2.x 视为满足要求。
	if major == 1 && minor < 25 {
		logging.LogWarn("Leego requires Go 1.25+, but now Go version is %s.", version)
	}
}

// getGoVersionParts 解析 Go 版本号的主、次、修订版本。
func getGoVersionParts(version string) (major, minor, revised uint64, err error) {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		err = errors.New("invalid version")
		return
	}

	if major, err = strconv.ParseUint(parts[0], 10, 64); err != nil {
		return
	}

	// 次版本可能带有预发布后缀（如 21rc1），截断非数字部分。
	minorText := parts[1]
	for index, char := range minorText {
		if char < '0' || char > '9' {
			minorText = minorText[:index]
			break
		}
	}

	if minor, err = strconv.ParseUint(minorText, 10, 64); err != nil {
		return
	}

	if len(parts) > 2 {
		revised, err = strconv.ParseUint(strings.TrimFunc(parts[2], func(r rune) bool {
			return r < '0' || r > '9'
		}), 10, 64)
	}

	return
}
