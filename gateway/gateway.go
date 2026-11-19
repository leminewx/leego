// Package gateway 提供反向代理与加权灰度分流。
//
// 设计要点：
//   - 负载均衡采用平滑加权轮询（smooth weighted round-robin），
//     权重为 3:1 时每 4 次请求严格出现 3+1 次，不会出现连续偏斜；
//   - 灰度按请求头命中上游标签：命中则只在同标签上游之间轮询，
//     未命中则回落到全量加权轮询；
//   - 只依赖标准库 httputil.ReverseProxy 与根包，不引入第三方代理库。
package gateway

import (
	"errors"
	"net/url"
	"sync"
)

// 分组错误。
var (
	// ErrNoUpstream 表示没有可用的上游。
	ErrNoUpstream = errors.New("gateway: at least one upstream is required")
	// ErrInvalidUpstream 表示上游配置非法。
	ErrInvalidUpstream = errors.New("gateway: invalid upstream")
)

// Upstream 描述一个上游服务。
type Upstream struct {
	// URL 为上游地址，必须包含 scheme 与 host。
	URL *url.URL
	// Weight 为权重，必须为正数；权重越大分到的流量越多。
	Weight int
	// Tag 为灰度标签，空字符串表示普通上游。
	Tag string
}

// node 是负载均衡的内部状态。
type node struct {
	// url 为上游地址。
	url *url.URL
	// weight 为静态权重。
	weight int
	// current 为平滑加权轮询的动态权重。
	current int
	// tag 为灰度标签。
	tag string
}

// Balancer 是并发安全的平滑加权轮询负载均衡器。
type Balancer struct {
	// mu 保护 nodes。
	mu sync.Mutex
	// nodes 为全部上游。
	nodes []*node
	// tags 记录出现过的灰度标签，便于快速判断命中。
	tags map[string]struct{}
}

// NewBalancer 创建负载均衡器。
func NewBalancer(upstreams ...Upstream) (*Balancer, error) {
	if len(upstreams) == 0 {
		return nil, ErrNoUpstream
	}

	own := &Balancer{tags: make(map[string]struct{}, 2)}
	for _, item := range upstreams {
		if item.URL == nil || item.URL.Scheme == "" || item.URL.Host == "" {
			return nil, ErrInvalidUpstream
		}

		if item.Weight <= 0 {
			// 权重为 0 的上游被跳过；负数属于配置错误。
			if item.Weight < 0 {
				return nil, ErrInvalidUpstream
			}

			continue
		}

		target := *item.URL
		own.nodes = append(own.nodes, &node{
			url:    &target,
			weight: item.Weight,
			tag:    item.Tag,
		})

		if item.Tag != "" {
			own.tags[item.Tag] = struct{}{}
		}
	}

	if len(own.nodes) == 0 {
		return nil, ErrNoUpstream
	}

	return own, nil
}

// GetUpstreams 返回上游快照。
func (own *Balancer) GetUpstreams() []Upstream {
	own.mu.Lock()
	upstreams := make([]Upstream, 0, len(own.nodes))
	for _, item := range own.nodes {
		target := *item.url
		upstreams = append(upstreams, Upstream{URL: &target, Weight: item.weight, Tag: item.tag})
	}
	own.mu.Unlock()

	return upstreams
}

// CountUpstreams 返回上游数量。
func (own *Balancer) CountUpstreams() int {
	own.mu.Lock()
	length := len(own.nodes)
	own.mu.Unlock()

	return length
}

// nodesSnapshot 返回内部节点的浅拷贝。
//
// 节点元素的指针在均衡器生命周期内保持不变，因此可以安全地用作
// 反向代理缓存的键，避免每个请求都调用 url.URL.String() 拼接字符串。
func (own *Balancer) nodesSnapshot() []*node {
	own.mu.Lock()
	nodes := append([]*node(nil), own.nodes...)
	own.mu.Unlock()

	return nodes
}

// PickNext 按权重选择一个上游。
func (own *Balancer) PickNext() *url.URL {
	return own.pick("")
}

// PickNextFor 优先在同标签上游中选择，没有匹配标签时回落到全量选择。
func (own *Balancer) PickNextFor(tag string) *url.URL {
	if tag == "" {
		return own.PickNext()
	}

	own.mu.Lock()
	_, exists := own.tags[tag]
	own.mu.Unlock()

	if !exists {
		return own.PickNext()
	}

	return own.pick(tag)
}

// pick 执行平滑加权轮询；tag 为空表示不限制标签。
func (own *Balancer) pick(tag string) *url.URL {
	own.mu.Lock()
	defer own.mu.Unlock()

	candidates := own.nodes
	if tag != "" {
		filtered := make([]*node, 0, len(own.nodes))
		for _, item := range own.nodes {
			if item.tag == tag {
				filtered = append(filtered, item)
			}
		}

		candidates = filtered
	}

	var best *node
	total := 0
	for _, item := range candidates {
		item.current += item.weight
		total += item.weight

		if best == nil || item.current > best.current {
			best = item
		}
	}

	if best == nil || total == 0 {
		return nil
	}

	best.current -= total
	return best.url
}
