package cache

import (
	"container/list"
	"net/http"
	"sync"
	"time"
)

// entry 是一条不可变的缓存记录。
//
// 写入缓存后字段不再修改：命中路径直接引用 header 与 body，
// 避免每次命中都做一次深拷贝。需要更新时整体替换条目。
type entry struct {
	// key 为缓存键。
	key string
	// status 为缓存的响应状态码。
	status int
	// header 为缓存的响应头快照。
	header http.Header
	// body 为缓存的响应体。
	body []byte
	// expires 为新鲜期截止时间。
	expires time.Time
	// staleUntil 为陈旧期截止时间；超过后条目被彻底清除。
	staleUntil time.Time
}

// store 是基于 LRU 的响应缓存。
//
// 并发模型：所有读写都在同一把互斥锁内完成，条目本身不可变，
// 因此命中路径可以在锁外安全地读 header 与 body。
type store struct {
	// mu 保护 items/order/bytes/refreshing。
	mu sync.Mutex
	// items 保存 key 到链表节点的映射。
	items map[string]*list.Element
	// order 按访问时间排序，头部最新、尾部最旧。
	order *list.List
	// bytes 为当前缓存的响应体总字节数。
	bytes int64
	// maxBytes 为缓存总容量上限。
	maxBytes int64
	// ttl 为新鲜期。
	ttl time.Duration
	// stale 为陈旧期；0 表示过期即删除。
	stale time.Duration
	// refreshing 标记正在后台刷新的 key，避免重复刷新。
	refreshing map[string]struct{}
}

// newStore 创建缓存存储。
func newStore(ttl, stale time.Duration, maxBytes int64) *store {
	return &store{
		items:      make(map[string]*list.Element),
		order:      list.New(),
		maxBytes:   maxBytes,
		ttl:        ttl,
		stale:      stale,
		refreshing: make(map[string]struct{}),
	}
}

// get 查询缓存。
//
// 返回值的含义：
//   - found 为 false 时表示未命中（不存在或已彻底过期）；
//   - fresh 为 true 表示仍在新鲜期内，可直接响应；
//   - fresh 为 false 且 found 为 true 表示处于陈旧期，可先响应旧值再刷新。
func (own *store) get(key string, now time.Time) (*entry, bool, bool) {
	own.mu.Lock()
	element, ok := own.items[key]
	if !ok {
		own.mu.Unlock()
		return nil, false, false
	}

	item := element.Value.(*entry)
	if now.Before(item.expires) {
		// 命中即刷新 LRU 位置，保证热点条目不被淘汰。
		own.order.MoveToFront(element)
		own.mu.Unlock()
		return item, true, true
	}

	if now.Before(item.staleUntil) {
		// 陈旧条目保留在原位：后台刷新会整体替换它。
		own.mu.Unlock()
		return item, false, true
	}

	// 彻底过期：立即回收，避免陈旧数据长期占用容量。
	own.removeElement(element)
	own.mu.Unlock()
	return nil, false, false
}

// put 写入缓存，并按容量淘汰最久未使用的条目。
//
// 单条响应超过总容量时不写入：写入后立刻被自己挤掉没有意义。
func (own *store) put(item *entry) {
	now := time.Now()
	item.expires = now.Add(own.ttl)
	item.staleUntil = item.expires
	if own.stale > 0 {
		item.staleUntil = item.expires.Add(own.stale)
	}

	size := int64(len(item.body))
	if own.maxBytes > 0 && size > own.maxBytes {
		return
	}

	own.mu.Lock()

	if element, ok := own.items[item.key]; ok {
		own.removeElement(element)
	}

	for own.maxBytes > 0 && own.bytes+size > own.maxBytes {
		oldest := own.order.Back()
		if oldest == nil {
			break
		}

		own.removeElement(oldest)
	}

	own.items[item.key] = own.order.PushFront(item)
	own.bytes += size
	own.mu.Unlock()
}

// beginRefresh 标记 key 正在后台刷新，返回 false 表示已有刷新在进行。
func (own *store) beginRefresh(key string) bool {
	own.mu.Lock()

	if _, ok := own.refreshing[key]; ok {
		own.mu.Unlock()
		return false
	}

	own.refreshing[key] = struct{}{}
	own.mu.Unlock()
	return true
}

// endRefresh 清除刷新标记。
func (own *store) endRefresh(key string) {
	own.mu.Lock()
	delete(own.refreshing, key)
	own.mu.Unlock()
}

// len 返回当前缓存条目数，仅用于测试与诊断。
func (own *store) len() int {
	own.mu.Lock()
	count := own.order.Len()
	own.mu.Unlock()

	return count
}

// removeElement 移除节点并同步容量计数；调用方必须已持有锁。
func (own *store) removeElement(element *list.Element) {
	item := element.Value.(*entry)
	own.order.Remove(element)
	delete(own.items, item.key)
	own.bytes -= int64(len(item.body))
}
