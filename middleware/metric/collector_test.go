package collector

import (
	"encoding/json"
	"math/rand"
	"testing"
	"time"
)

func TestCollector(t *testing.T) {
	c := New()
	for range 100 {
		c.Increase()
		time.Sleep(time.Duration(rand.Intn(30)) * time.Millisecond)
		c.Decrease([]int{200, 400, 401, 403, 404, 405, 429, 500, 502, 503}[rand.Intn(10)], time.Duration(rand.Intn(100))*time.Microsecond)
	}

	data, err := json.MarshalIndent(&c, "", "    ")
	if err != nil {
		t.Error(err)
	}

	t.Log(string(data))
}

// cpu: Intel(R) Core(TM) i5-14600KF 						20    	32023226	        37.58 ns/op	       0 B/op	       0 allocs/op
// cpu: 11th Gen Intel(R) Core(TM) i5-1135G7 @ 2.40GHz		8		25593556	        46.14 ns/op	       0 B/op	       0 allocs/op
func BenchmarkCollector(b *testing.B) {
	c := New()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		c.Increase()
		c.Decrease(200, 100*time.Microsecond)
	}
}
