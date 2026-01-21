package limiter

import (
	"context"
	"testing"
	"time"
)

func TestSlidingWindowLimiter(t *testing.T) {
	limiter := NewSlidingWindowLimite(10, 10, 1*time.Second)
	for range 100 {
		time.Sleep(50 * time.Millisecond)
		t.Log(limiter.AllowWithTimeout(context.Background(), 300*time.Millisecond))
	}
}
