package uuid

import "testing"

func TestUUID_V4(t *testing.T) {
	for range 10 {
		t.Log(NewV4().String())
		t.Log(NewV4().FastString())
	}
}

func BenchmarkNewV4(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = NewV4()
	}
}
