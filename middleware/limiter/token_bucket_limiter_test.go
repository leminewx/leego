package limiter

import (
	"testing"
	"time"
)

func TestTokenBucketLimiter(t *testing.T) {
	limiter := NewTokenBucketLimiter(20, 30)
	for range 1000 {
		time.Sleep(10 * time.Millisecond)
		t.Log(limiter.Allow())
	}
}
