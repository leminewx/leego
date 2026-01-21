package limiter

import (
	"context"
	"testing"
	"time"
)

func TestNewFixedWindowLimiter(t *testing.T) {
	limiter := NewFixedWindowLimiter(10, 1*time.Second)
	for range 100 {
		time.Sleep(50 * time.Millisecond)
		t.Log(limiter.AllowWithTimeout(context.Background(), 100*time.Millisecond))
	}
}
