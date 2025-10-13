package jwt

import (
	"testing"
	"time"
)

func TestToken_Sign(t *testing.T) {
	now := time.Now()
	token := NewToken(&StandardClaims{
		Id:        "test",
		Issuer:    "test",
		Subject:   "test",
		Audiences: []string{"test"},
		IssuedAt:  now.Unix(),
		NotBefore: now.Add(-time.Hour).Unix(),
		ExpiresAt: now.Add(+time.Hour).Unix(),
	})

	signer, err := NewHMACSigner(HS256, []byte("test"))
	if err != nil {
		t.Error(err)
	}

	data, err := token.Sign(signer)
	if err != nil {
		t.Error(err)
	}
	t.Log(data)
}

func TestToken_ValidateStandardClaims(t *testing.T) {
	now := time.Now()
	token := NewToken(&StandardClaims{
		Id:        "test",
		Issuer:    "test",
		Subject:   "test",
		Audiences: []string{"test"},
		IssuedAt:  now.Unix(),
		NotBefore: now.Add(-time.Hour).Unix(),
		ExpiresAt: now.Add(+time.Hour).Unix(),
	})

	if err := token.ValidateStandardClaims("test", "leego"); err == nil {
		t.Error("audience expect 'test', get 'leego'")
	}
	if err := token.ValidateStandardClaims("leego", "test"); err == nil {
		t.Error("issuer expect 'test', get 'leego'")
	}
	if err := token.ValidateStandardClaims("test", "test"); err != nil {
		t.Error(err)
	}
}

// cpu: Intel(R) Core(TM) i5-14600KF					20		standard claims	    1413406	      839.1 ns/op	    1266 B/op	      12 allocs/op
// cpu: Intel(R) Core(TM) i5-14600KF					20		mix claims			 633018	      1857 ns/op	    2083 B/op	      36 allocs/op

// cpu: Intel(R) Core(TM) i5-8250U CPU @ 1.60GHz		8		standard claims		 737779	      1561 ns/op	    1265 B/op	      12 allocs/op
// cpu: Intel(R) Core(TM) i5-8250U CPU @ 1.60GHz		8		mix claims			 346509	      3498 ns/op	    2081 B/op	      36 allocs/op
func BenchmarkToken_Sign(b *testing.B) {
	now := time.Now()
	token := NewToken(&StandardClaims{
		Id:        "test",
		Issuer:    "test",
		Subject:   "test",
		Audiences: []string{"test"},
		IssuedAt:  now.Unix(),
		NotBefore: now.Add(-time.Hour).Unix(),
		ExpiresAt: now.Add(+time.Hour).Unix(),
	})

	signer, err := NewHMACSigner(HS256, []byte("test"))
	if err != nil {
		b.Error(err)
	}

	b.Run("使用标准声明", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for range b.N {
			if _, err := token.Sign(signer); err != nil {
				b.Error(err)
			}
		}
	})

	b.Run("使用混合声明", func(b *testing.B) {
		token.WithClaim("uid", "TestUser")

		b.ReportAllocs()
		b.ResetTimer()

		for range b.N {
			if _, err := token.Sign(signer); err != nil {
				b.Error(err)
			}
		}
	})
}

// cpu: Intel(R) Core(TM) i5-14600KF					20		165040785	        7.317 ns/op	       0 B/op	       0 allocs/op
// cpu: Intel(R) Core(TM) i5-8250U CPU @ 1.60GHz		8		100000000	        10.29 ns/op	       0 B/op	       0 allocs/op
func BenchmarkToken_ValidateStandardClaims(b *testing.B) {
	now := time.Now()
	token := NewToken(&StandardClaims{
		Id:        "test",
		Issuer:    "test",
		Subject:   "test",
		Audiences: []string{"test"},
		IssuedAt:  now.Unix(),
		NotBefore: now.Add(-time.Hour).Unix(),
		ExpiresAt: now.Add(+time.Hour).Unix(),
	})

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		if err := token.ValidateStandardClaims("test", "test"); err != nil {
			b.Error(err)
		}
	}
}
