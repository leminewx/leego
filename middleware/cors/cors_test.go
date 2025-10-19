package cors

import (
	"sync"
	"testing"
)

func TestCors(t *testing.T) {
	cors := New()
	t.Log(cors)
}

func BenchmarkCORS(b *testing.B) {
	lock := &sync.RWMutex{}
	for range b.N {
		lock.Lock()
		lock.Unlock()
	}
}
