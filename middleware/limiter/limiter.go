package limiter

import "context"

// Limiter 定义限流器的接口类型
type Limiter interface {
	// Allow 非阻塞，立即返回是否允许
	Allow() bool
	// Wait 阻塞，直到获取令牌或取消上下文
	Wait(ctx context.Context) error
}
