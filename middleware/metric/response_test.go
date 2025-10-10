package metric

import (
	"testing"
	"time"
)

// cpu: Intel(R) Core(TM) i5-14600KF						20		49494536	        23.87 ns/op	       0 B/op	       0 allocs/op
// cpu: 11th Gen Intel(R) Core(TM) i5-1135G7 @ 2.40GHz		8		34355898	        35.65 ns/op	       0 B/op	       0 allocs/op
func BenchmarkResponse(b *testing.B) {
	resp := NewResponse(200)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		resp.Decrease(100 * time.Microsecond)
	}
}
