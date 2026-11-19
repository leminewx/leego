package upgrader

import (
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Config 定义 WebSocket 升级器配置。
//
// Config 在创建后被视为不可变：多个连接可以共享同一个 Upgrader，
// 因此不建议在运行期修改字段，否则会产生数据竞争。
type Config struct {
	// ReadTimeout 为单次读取的超时时间；小于等于 0 表示不设置超时。
	//
	// 不设置超时的连接可能被对端长期挂起，生产环境建议显式配置。
	ReadTimeout time.Duration
	// WriteTimeout 为单次写入的超时时间；小于等于 0 表示不设置超时。
	WriteTimeout time.Duration
	// MaxReadLimit 为单条消息的最大字节数；小于等于 0 时使用默认值 16 MiB。
	MaxReadLimit int64
	// OriginCheckFunc 为来源校验函数；为 nil 时允许任意来源。
	//
	// 默认实现只允许同源请求（Origin 缺失时放行，用于非浏览器客户端）。
	// 不做校验时，任意站点都能发起跨站 WebSocket 连接（CSWSH）；
	// 如需支持多域名，请显式替换本函数。
	OriginCheckFunc func(req *http.Request) bool
	// Subprotocols 为允许协商的子协议列表；为空表示不协商子协议。
	Subprotocols []string
}

// NewDefaultConfig 返回默认配置。
//
// 默认值：读超时 60 秒、写超时 10 秒、单条消息上限 16 MiB、只允许同源。
func NewDefaultConfig() *Config {
	return &Config{
		ReadTimeout:     60 * time.Second,
		WriteTimeout:    10 * time.Second,
		MaxReadLimit:    _DEFAULT_MAX_READ_LIMIT,
		OriginCheckFunc: checkSameOrigin,
	}
}

// checkSameOrigin 只允许 Origin 与请求 Host 一致的跨站请求。
//
// Origin 缺失时放行：非浏览器客户端（CLI、服务间调用）不会携带该头，
// 也就不存在浏览器自动附带凭据的跨站攻击面。
func checkSameOrigin(req *http.Request) bool {
	origin := strings.TrimSpace(req.Header.Get("Origin"))
	if origin == "" {
		return true
	}

	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}

	return strings.EqualFold(parsed.Host, req.Host)
}
