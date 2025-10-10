package metric

import (
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestCollector(t *testing.T) {
	c := NewCollector("获取用户信息", http.MethodGet, "/api/v1/users/:id")

	var wg sync.WaitGroup
	wg.Go(func() {
		for range 1000 {
			c.Increase()
			time.Sleep(time.Duration(rand.IntN(30)) * time.Millisecond)
			c.Decrease([]int{200, 400, 401, 500, 502}[rand.IntN(5)], time.Duration(rand.IntN(100))*time.Microsecond)
		}
	})

	wg.Go(func() {
		for range 5 {
			time.Sleep(time.Duration(rand.IntN(6)) * time.Second)
			data, err := json.MarshalIndent(&c, "", "    ")
			if err != nil {
				t.Error(err)
			}

			t.Log(string(data))
		}
	})

	wg.Wait()

	time.Sleep(time.Duration(rand.IntN(6)) * time.Second)
	data, err := json.MarshalIndent(&c, "", "    ")
	if err != nil {
		t.Error(err)
	}

	t.Log(string(data))
}

// cpu: Intel(R) Core(TM) i5-14600KF 						20    	24746859	        49.05 ns/op	       0 B/op	       0 allocs/op
// cpu: 11th Gen Intel(R) Core(TM) i5-1135G7 @ 2.40GHz		8		17951575	        64.34 ns/op	       0 B/op	       0 allocs/op
func BenchmarkCollector(b *testing.B) {
	c := NewCollector("获取用户信息", http.MethodGet, "/api/v1/users/:id")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		c.Increase()
		c.Decrease(200, 100*time.Microsecond)
	}
}
