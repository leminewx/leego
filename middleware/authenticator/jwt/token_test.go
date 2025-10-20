package jwt

import (
	"testing"
	"time"
)

func TestToken_Sign(t *testing.T) {
	now := time.Now()
	signer, err := NewHMACSigner(NewHeader(HS256), []byte("test"))
	if err != nil {
		t.Fatal(err)
	}

	token := NewToken(&StandardClaims{
		Id:        "test",
		Issuer:    "test",
		Subject:   "test",
		Audiences: []string{"test"},
		IssuedAt:  now.Unix(),
		NotBefore: now.Add(-time.Hour).Unix(),
		ExpiresAt: now.Add(+time.Hour).Unix(),
	})

	data, err := signer.Sign(token)
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

// cpu: Intel(R) Core(TM) i5-14600KF					20		standard claims	    1666219	       703.8 ns/op	    1138 B/op	       9 allocs/op
// cpu: Intel(R) Core(TM) i5-14600KF					20		mix claims			 704564	        1760 ns/op	    1955 B/op	      33 allocs/op

// cpu: Intel(R) Core(TM) i5-8250U CPU @ 1.60GHz		8		standard claims		 737779	      1561 ns/op	    1265 B/op	      12 allocs/op
// cpu: Intel(R) Core(TM) i5-8250U CPU @ 1.60GHz		8		mix claims			 346509	      3498 ns/op	    2081 B/op	      36 allocs/op
func BenchmarkToken_Sign(b *testing.B) {
	now := time.Now()
	signer, err := NewHMACSigner(NewHeader(HS256), []byte("test"))
	if err != nil {
		b.Fatal(err)
	}

	token := NewToken(&StandardClaims{
		Id:        "test",
		Issuer:    "test",
		Subject:   "test",
		Audiences: []string{"test"},
		IssuedAt:  now.Unix(),
		NotBefore: now.Add(-time.Hour).Unix(),
		ExpiresAt: now.Add(+time.Hour).Unix(),
	})

	b.Run("使用标准声明", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for range b.N {
			if _, err := signer.Sign(token); err != nil {
				b.Error(err)
			}
		}
	})

	b.Run("使用混合声明", func(b *testing.B) {
		token.WithClaim("uid", "TestUser")

		b.ReportAllocs()
		b.ResetTimer()

		for range b.N {
			if _, err := signer.Sign(token); err != nil {
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
