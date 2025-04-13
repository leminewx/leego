package jwt

import (
	"testing"
	"time"
)

func TestManager_Generate(t *testing.T) {
	manager, err := NewManager(ALG_HS256, []byte("1234567890qwertyuiopasdfghjklzxcvbnm"), 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	claims := NewClaims().
		WithIssuer("lemine").
		WithSubject("test").
		WithAudience("leego").
		WithIssuedAt(time.Now().Unix()).
		WithNotBefore(time.Now().Add(1 * time.Second).Unix()).
		WithExpiration(time.Now().Add(7 * 24 * time.Hour).Unix())

	signature := manager.Generate(claims)
	t.Logf("token: %s", signature)

	time.Sleep(3 * time.Second)

	c := NewClaims()
	t.Log(manager.Validate(signature, &c))
	t.Logf("claims: %v", c)
}

// cpu: Intel(R) Core(TM) i5-14600KF		20		639658	      1744 ns/op	    1145 B/op	      33 allocs/op
func BenchmarkManager_Generate(b *testing.B) {
	manager, err := NewManager(ALG_HS256, []byte("1234567890qwertyuiopasdfghjklzxcvbnm"), 7*24*time.Hour)
	if err != nil {
		b.Fatal(err)
	}

	claims := NewClaims().
		WithIssuer("lemine").
		WithSubject("test").
		WithAudience("leego").
		WithIssuedAt(time.Now().Unix()).
		WithExpiration(time.Now().Add(7 * 24 * time.Hour).Unix())

	signature := manager.Generate(claims)

	for i := 0; i < b.N; i++ {
		c := NewClaims()
		if err := manager.Validate(signature, &c); err != nil {
			b.Fatal(err)
		}
	}
}
