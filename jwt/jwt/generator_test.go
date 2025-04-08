package jwt

import (
	"errors"
	"fmt"
	"log"
	"strconv"
	"testing"
	"time"
)

func TestGenerator(t *testing.T) {
	var jti int64
	generator := NewDefaultGenerator([]byte("123qwe!@#QWE")).WithIss("test").WithJtiGenerator(func() string {
		jti++
		return strconv.Itoa(int(jti))
	})
	sign, err := generator.Encode(
		NewPayload().
			WithAud("service-user-001").
			WithExp(time.Now().Unix() + 20).
			WithNbf(time.Now().Unix() + 10).
			WithSub("lemine"),
	)
	if err != nil {
		t.Fatal(err)
	}
	log.Println("sign ===>>>", sign)

	payload, err := generator.Decode(sign)
	if err != nil {
		t.Fatal(err)
	}
	log.Println("payload ===>>>", payload)

	for i := 0; i < 30; i++ {
		time.Sleep(1 * time.Second)
		if err := generator.Validate(payload); err != nil {
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
// BenchmarkGenerator_Encode-20	      605311	      2044 ns/op	    1942 B/op	      31 allocs/op
func BenchmarkGenerator_Encode(b *testing.B) {
	var jti int64
	generator := NewDefaultGenerator([]byte("123qwe!@#QWE")).WithIss("test").WithJtiGenerator(func() string {
		jti++
		return strconv.Itoa(int(jti))
	})

	payload := NewPayload().WithSub("lemine")
	for i := 0; i < b.N; i++ {
		if _, err := generator.Encode(payload); err != nil {
			b.Fatal(err)
		}
		delete(payload, "jti")
	}
}

func TestGenerator_WithJtiWhitelist(t *testing.T) {
	gen := NewDefaultGenerator([]byte("123qwe!@#QWE")).
		WithIss("test").
		WithDelay(10).
		WithTimeout(50).
		WithJtiWhitelist(60)
		// WithJtiGenerator(func() string {
		// 	return "0001000"
		// })

	token, err := gen.Encode(NewPayload().With("jti", "1"))
	if err != nil {
		t.Fatal(err)
	}
	log.Println("sign ===>>>", token)

	payload, err := gen.Decode(token)
	if err != nil {
		t.Fatal(err)
	}
	log.Println("payload ===>>>", payload)

	for i := 0; i < 8; i++ {
		if err := gen.Validate(payload); err != nil {
			t.Log((i+1)*10, err)
		} else {
			t.Log((i+1)*10, "valid token")
		}

		t.Log(gen.(*generator).jtiWhitelist.Load(payload.GetJti()))
		time.Sleep(10 * time.Second)
		fmt.Println()
	}

	gen.Reset()
	time.Sleep(1 * time.Second)
}

func TestGenerator_WithJtiGenerator(t *testing.T) {
	gen := NewDefaultGenerator([]byte("123qwe!@#QWE")).
		WithIss("test").
		WithDelay(10).
		WithTimeout(50).
		WithJtiGenerator(func() string {
			return "0001000"
		})

	token, err := gen.Encode(NewPayload())
	if err != nil {
		t.Fatal(err)
	}
	log.Println("sign ===>>>", token)

	payload, err := gen.Decode(token)
	if err != nil {
		t.Fatal(err)
	}
	log.Println("payload ===>>>", payload)

	for i := 0; i < 8; i++ {
		if err := gen.Validate(payload); err != nil {
			t.Log((i+1)*10, err)
		} else {
			t.Log((i+1)*10, "valid token")
		}

		t.Log(gen.(*generator).jtiWhitelist.Load(payload.GetJti()))
		time.Sleep(10 * time.Second)
		fmt.Println()
	}

	gen.Reset()
	time.Sleep(1 * time.Second)
}
