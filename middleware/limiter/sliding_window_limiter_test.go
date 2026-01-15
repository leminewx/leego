package limiter

import (
	"testing"
	"time"
)

func TestSlidingWindowLimiter(t *testing.T) {
	limiter := NewSlidingWindowLimite(20, 10, 1*time.Second)
	for range 1000 {
		time.Sleep(10 * time.Millisecond)
		t.Log(limiter.Allow())
	}
}
