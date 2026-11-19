package testutil

import (
	"sync"
	"time"
)

// Clock 抽象时间源，便于在测试中冻结或推进时间。
//
// 框架内部不依赖本接口；它用于业务代码在测试中注入可控时钟，
// 从而让 TTL、过期、重试间隔等逻辑可以在毫秒级完成验证。
type Clock interface {
	// Now 返回当前时间。
	Now() time.Time
}

// FrozenClock 是 Clock 的可控实现，默认返回构造时指定的时间。
//
// 实现内部使用读写锁，可被多个 goroutine 并发读取，也可在测试协程中推进。
type FrozenClock struct {
	// mu 保护 at。
	mu sync.RWMutex
	// at 为当前被冻结的时间点。
	at time.Time
}

// NewFrozenClock 创建冻结时钟；at 为零值时使用 Unix 纪元，避免出现零值语义歧义。
func NewFrozenClock(at time.Time) *FrozenClock {
	if at.IsZero() {
		at = time.Unix(0, 0).UTC()
	}

	return &FrozenClock{at: at}
}

// Now 返回当前冻结时间。
func (own *FrozenClock) Now() time.Time {
	own.mu.RLock()
	now := own.at
	own.mu.RUnlock()

	return now
}

// Advance 按给定增量推进时间；负数表示回退。
func (own *FrozenClock) Advance(delta time.Duration) {
	own.mu.Lock()
	own.at = own.at.Add(delta)
	own.mu.Unlock()
}

// Set 将时钟设置为指定时间；零值会被忽略，保证 Now 永远可用。
func (own *FrozenClock) Set(at time.Time) {
	if at.IsZero() {
		return
	}

	own.mu.Lock()
	own.at = at
	own.mu.Unlock()
}

// 编译期断言：FrozenClock 实现 Clock。
var _ Clock = &FrozenClock{}
