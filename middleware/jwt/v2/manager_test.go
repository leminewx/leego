package jwt

import (
	"math/rand/v2"
	"strconv"
	"testing"
	"time"
)

func TestManager(t *testing.T) {
	opt := &Options{
		Issuer:       "leego",
		Secret:       []byte("secret"),
		Audiences:    []string{"leego"},
		Algorithm:    HS256,
		ValidAfter:   3600,
		ExpiredAfter: 60,
	}

	signer, err := NewHMACSigner(opt.Algorithm, opt.Secret)
	if err != nil {
		t.Fatal(err)
	}

	parser := NewParser()
	manager := NewManager(signer, parser, opt)

	for range 10 {
		time.Sleep(time.Duration(rand.IntN(1000)) * time.Millisecond)

		token := manager.GenerateToken(&StandardClaims{
			Id: strconv.Itoa(rand.Int()),
		}).WithClaim("uid", strconv.Itoa(rand.Int()))

		signature, err := manager.Sign(token)
		if err != nil {
			t.Fatal(err)
		}
		t.Log(signature)

		if token, err = manager.Verify(signature); err != nil {
			t.Fatal(err)
		}
		t.Log(*token.stdClaims, token.claims)

		if token, err = manager.VerifyWithStandardClaims(signature, "test", "leego"); err == nil || token == nil {
			t.Error("leego: jwt: token verification failed")
		} else {
			t.Log(*token.stdClaims, token.claims)
		}
	}
}

// cpu: Intel(R) Core(TM) i5-14600KF					20		standard claims	    1389517	     865.1 ns/op	    1314 B/op	      12 allocs/op
// cpu: Intel(R) Core(TM) i5-14600KF					20		mix claims			 695498	      1740 ns/op	    2003 B/op	      33 allocs/op
func BenchmarkManager_Sign(b *testing.B) {
	opt := &Options{
		Issuer:       "leego",
		Secret:       []byte("secret"),
		Audiences:    []string{"leego"},
		Algorithm:    HS256,
		ValidAfter:   3600,
		ExpiredAfter: 60,
	}

	signer, err := NewHMACSigner(opt.Algorithm, opt.Secret)
	if err != nil {
		b.Fatal(err)
	}

	parser := NewParser()
	manager := NewManager(signer, parser, opt)
	token := manager.GenerateToken(&StandardClaims{
		Id: strconv.Itoa(rand.Int()),
	})

	b.Run("使用标准声明", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for range b.N {
			if _, err := manager.Sign(token); err != nil {
				b.Error(err)
			}
		}
	})

	b.Run("使用混合声明", func(b *testing.B) {
		token.WithClaim("uid", "TestUser")

		b.ReportAllocs()
		b.ResetTimer()

		for range b.N {
			if _, err := manager.Sign(token); err != nil {
				b.Error(err)
			}
		}
	})
}

// cpu: Intel(R) Core(TM) i5-14600KF					20		standard claims	    436077	      2720 ns/op	    1761 B/op	      50 allocs/op
// cpu: Intel(R) Core(TM) i5-14600KF					20		mix claims			413164	      2925 ns/op	    1825 B/op	      54 allocs/op
func BenchmarkManager_Verify(b *testing.B) {
	opt := &Options{
		Issuer:       "leego",
		Secret:       []byte("secret"),
		Audiences:    []string{"leego"},
		Algorithm:    HS256,
		ValidAfter:   3600,
		ExpiredAfter: 60,
	}

	signer, err := NewHMACSigner(opt.Algorithm, opt.Secret)
	if err != nil {
		b.Fatal(err)
	}

	parser := NewParser()
	manager := NewManager(signer, parser, opt)
	token := manager.GenerateToken(&StandardClaims{
		Id: strconv.Itoa(rand.Int()),
	})

	b.Run("使用标准声明", func(b *testing.B) {
		signature, err := manager.Sign(token)
		if err != nil {
			b.Fatal(err)
		}

		b.ReportAllocs()
		b.ResetTimer()

		for range b.N {
			if _, err := manager.Verify(signature); err != nil {
				b.Error(err)
			}
		}
	})

	b.Run("使用混合声明", func(b *testing.B) {
		token.WithClaim("uid", "TestUser")
		signature, err := manager.Sign(token)
		if err != nil {
			b.Fatal(err)
		}

		b.ReportAllocs()
		b.ResetTimer()

		for range b.N {
			if _, err := manager.Verify(signature); err != nil {
				b.Error(err)
			}
		}
	})
}
