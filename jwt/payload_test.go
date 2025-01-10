package jwt

import (
	"errors"
	"log"
	"testing"
	"time"
)

func TestPayload(t *testing.T) {
	payload := NewPayload().
		WithAud("service-user-001").
		WithExp(time.Now().Unix() + 20).
		WithNbf(time.Now().Unix() + 10).
		WithSub("lemine")

	data, err := payload.Encode()
	if err != nil {
		t.Fatal(err)
	}
	t.Log(string(data))

	for i := 0; i < 30; i++ {
		time.Sleep(1 * time.Second)
		if err := payload.Validate(); err != nil {
			if errors.Is(err, errTokenNotEffective) {
				log.Println("ineffective token")
				continue
			}
			t.Error(err)
			break
		}
		log.Println("valid token")
	}
	log.Println("invalid token")
}

// goos: windows
// goarch: amd64
// cpu: Intel(R) Core(TM) i5-14600KF
// BenchmarkJwt_Encode-20	      996553	      1100 ns/op	     992 B/op	      18 allocs/op
func BenchmarkJwt_Encode(b *testing.B) {
	payload := NewPayload().
		WithAud("service-user-001").
		WithExp(time.Now().Unix() + 60*60*24).
		WithNbf(time.Now().Unix() + 60).
		WithSub("lemine")

	for i := 0; i < b.N; i++ {
		if _, err := payload.Encode(); err != nil {
			b.Fatal(err)
		}
	}
}
