package jwt

import (
	"encoding/json"
	"testing"
)

// cpu: Intel(R) Core(TM) i5-8250U CPU @ 1.60GHz		8		1175592	       908.4 ns/op	     512 B/op	       8 allocs/op
func BenchmarkHeader_map(b *testing.B) {
	for range b.N {
		if _, err := json.Marshal(map[string]string{"typ": "jwt", "alg": "HS256"}); err != nil {
			b.Fatal(err)
		}
	}
}

// cpu: Intel(R) Core(TM) i5-8250U CPU @ 1.60GHz		8		4140441	       283.0 ns/op	      64 B/op	       2 allocs/op
func BenchmarkHeader_struct(b *testing.B) {
	for range b.N {
		if _, err := json.Marshal(&Header{"jwt", "HS256"}); err != nil {
			b.Fatal(err)
		}
	}
}
