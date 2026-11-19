package session

import (
	"context"
	"errors"
	"sync"
	"time"
)

const (
	// _DEFAULT_SWEEP_INTERVAL 为内存存储的默认清理周期。
	_DEFAULT_SWEEP_INTERVAL = time.Minute
	// _MIN_SWEEP_INTERVAL 为避免过密扫描设置的下限。
	_MIN_SWEEP_INTERVAL = time.Second
)

// ErrNotFound 表示会话不存在或已过期。
var ErrNotFound = errors.New("session: session not found")

// memoryEntry 是内存存储中的一条会话。
type memoryEntry struct {
	// values 为会话数据。
	values Values
	// expires 为过期时间。
	expires time.Time
}

// MemoryStore 是进程内的会话存储，适用于单实例部署与测试。
type MemoryStore struct {
	// mu 保护 items。
	mu sync.RWMutex
	// items 为会话表。
	items map[string]*memoryEntry
	// stop 用于停止后台清理协程。
	stop chan struct{}
	// once 保证 Close 只关闭一次。
	once sync.Once
}

// NewMemoryStore 创建内存存储并启动后台清理协程。
//
// 过期判断是惰性的：Get 命中过期条目时直接删除并返回 ErrNotFound，
// 后台协程只负责回收不再被访问的条目。
func NewMemoryStore() *MemoryStore {
	own := &MemoryStore{
		items: make(map[string]*memoryEntry),
		stop:  make(chan struct{}),
	}

	go own.run()
	return own
}

// Get 读取会话数据。
func (own *MemoryStore) Get(_ context.Context, id string) (Values, error) {
	if id == "" {
		return nil, ErrNotFound
	}

	own.mu.Lock()
	defer own.mu.Unlock()

	item, ok := own.items[id]
	if !ok {
		return nil, ErrNotFound
	}

	if !item.expires.IsZero() && !time.Now().Before(item.expires) {
		delete(own.items, id)
		return nil, ErrNotFound
	}

	values := make(Values, len(item.values))
	for key, value := range item.values {
		values[key] = value
	}

	return values, nil
}

// Save 写入会话数据并刷新过期时间。
func (own *MemoryStore) Save(_ context.Context, id string, values Values, ttl time.Duration) error {
	if id == "" {
		return ErrNotFound
	}

	if ttl <= 0 {
		ttl = _DEFAULT_TTL
	}

	copied := make(Values, len(values))
	for key, value := range values {
		copied[key] = value
	}

	own.mu.Lock()
	own.items[id] = &memoryEntry{values: copied, expires: time.Now().Add(ttl)}
	own.mu.Unlock()

	return nil
}

// Destroy 删除会话数据；会话不存在时视为成功。
func (own *MemoryStore) Destroy(_ context.Context, id string) error {
	own.mu.Lock()
	delete(own.items, id)
	own.mu.Unlock()

	return nil
}

// Len 返回当前会话数量（包含尚未清理的过期条目），供测试与诊断使用。
func (own *MemoryStore) Len() int {
	own.mu.RLock()
	defer own.mu.RUnlock()

	return len(own.items)
}

// Close 停止后台清理协程，可重复调用。
func (own *MemoryStore) Close() error {
	own.once.Do(func() {
		close(own.stop)
	})

	return nil
}

// run 周期性清理过期条目。
func (own *MemoryStore) run() {
	interval := _DEFAULT_SWEEP_INTERVAL
	if interval < _MIN_SWEEP_INTERVAL {
		interval = _MIN_SWEEP_INTERVAL
	}

	ticker := time.NewTicker(interval)
	for {
		select {
		case <-ticker.C:
			own.sweep(time.Now())
		case <-own.stop:
			ticker.Stop()
			return
		}
	}
}

// sweep 删除全部过期条目。
func (own *MemoryStore) sweep(now time.Time) {
	own.mu.Lock()
	for id, item := range own.items {
		if !item.expires.IsZero() && !now.Before(item.expires) {
			delete(own.items, id)
		}
	}
	own.mu.Unlock()
}
