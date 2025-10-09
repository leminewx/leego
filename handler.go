package leego

import (
	"sync"
	"sync/atomic"

	"gitee.com/leminewx/leego/middleware/metric"
	"gitee.com/leminewx/leego/trie"
)

type (
	// HandleFunc 定义处理函数的类型
	HandleFunc func(ctx *Context)

	// MiddlewareFunc 定义中间件函数的类型
	MiddlewareFunc func(HandleFunc) HandleFunc
)

// handler 定义处理器的数据结构
type handler struct {
	group *group

	enable      int32             // 使能标志
	handle      HandleFunc        // 处理函数
	metadata    *metric.Metadata  // 路由元数据
	collector   *metric.Collector // 数据收集器
	paramNames  []string          // 模式中的参数名列表
	middlewares []MiddlewareFunc  // 中间件函数

	initialized      bool // 标记是否已经初始化
	initializedMutex sync.Mutex
}

// newHandler 新建一个处理器
func newHandler(group *group, name, method, pattern string, handle HandleFunc) *handler {
	return &handler{
		group:  group,
		enable: 1,
		handle: handle,
		metadata: &metric.Metadata{
			Name:    name,
			Method:  method,
			Pattern: pattern,
		},
		collector:  metric.NewCollector(method, pattern),
		paramNames: extractSegmentNames(trie.SplitPattern(pattern)),
	}
}

// WithEnable 启用或禁用处理器
func (own *handler) WithEnable(enable bool) *handler {
	if enable {
		atomic.StoreInt32(&own.enable, 1)
		return own
	}

	atomic.StoreInt32(&own.enable, 0)
	return own
}

// IsEnable 检查处理器是否启用
func (own *handler) IsEnable() bool {
	return atomic.LoadInt32(&own.enable) == 1
}

// Handle 请求处理函数
func (own *handler) Handle(ctx *Context) {
	// 延迟初始化
	if !own.initialized {
		own.initializedMutex.Lock()
		own.initialize()
		own.initializedMutex.Unlock()
	}

	// 调用处理函数
	own.handle(ctx)
}

// initialize （在服务前）初始化处理器
func (own *handler) initialize() {
	// 如果已经初始化，则返回
	if own.initialized {
		return
	}

	// 获取所有中间件
	own.middlewares = make([]MiddlewareFunc, 0, 30)
	own.group.getAllMiddlewares(&own.middlewares)

	// 调用中间件
	for i := len(own.middlewares) - 1; i >= 0; i-- {
		own.handle = own.middlewares[i](own.handle)
	}

	own.initialized = true
}
