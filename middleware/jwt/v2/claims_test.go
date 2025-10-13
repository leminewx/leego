package jwt

import (
	"testing"
	"time"
)

func TestStandardClaims(t *testing.T) {
	now := time.Now()
	claims := &StandardClaims{
		Id:        "test",
		Issuer:    "test",
		Subject:   "test",
		Audiences: []string{"test"},
		IssuedAt:  now.Unix(),
		NotBefore: now.Add(-time.Hour).Unix(),
		ExpiresAt: now.Add(+time.Hour).Unix(),
	}
	t.Log(claims.Validate())

	data, err := claims.Encode()
	if err != nil {
		t.Fatal(err)
	}
	t.Log(data)
}

// cpu: Intel(R) Core(TM) i5-8250U CPU @ 1.60GHz		20    	3612774	       324.0 ns/op	     400 B/op	       3 allocs/op
// cpu: Intel(R) Core(TM) i5-8250U CPU @ 1.60GHz		8		1967604	       604.9 ns/op	     400 B/op	       3 allocs/op
func BenchmarkStandard_Encode(b *testing.B) {
	now := time.Now()
	claims := &StandardClaims{
		Id:        "test",
		Issuer:    "test",
		Subject:   "test",
		Audiences: []string{"test"},
		IssuedAt:  now.Unix(),
		NotBefore: now.Add(-time.Hour).Unix(),
		ExpiresAt: now.Add(+time.Hour).Unix(),
	}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		if _, err := claims.Encode(); err != nil {
			b.Fatal(err)
		}
	}
}

// cpu: Intel(R) Core(TM) i5-8250U CPU @ 1.60GHz		20		347010661	         3.457 ns/op	       0 B/op	       0 allocs/op
// cpu: Intel(R) Core(TM) i5-8250U CPU @ 1.60GHz		8		186424736	         6.290 ns/op	       0 B/op	       0 allocs/op
func BenchmarkStandard_Validate(b *testing.B) {
	now := time.Now()
	claims := &StandardClaims{
		Id:        "test",
		Issuer:    "test",
		Subject:   "test",
		Audiences: []string{"test"},
		IssuedAt:  now.Unix(),
		NotBefore: now.Add(-time.Hour).Unix(),
		ExpiresAt: now.Add(+time.Hour).Unix(),
	}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		if err := claims.Validate(); err != nil {
			b.Fatal(err)
		}
	}
}
