package limiter

import (
	"context"
	"time"
)

// Limiter 定义限流器的接口类型
type Limiter interface {
	// Allow 非阻塞的允许检查，立即返回是否允许
	Allow() bool
	// AllowWithTimeout 带超时的允许检查
	AllowWithTimeout(ctx context.Context, timeout time.Duration) bool
	// Wait 带上下文的允许检查，直到允许或取消上下文
	Wait(ctx context.Context) error
}
