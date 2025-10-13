package jwt

import (
	"testing"
	"time"
)

func TestParser_Parse(t *testing.T) {
	now := time.Now()
	tokenSrc := NewToken(&StandardClaims{
		Id:        "test",
		Issuer:    "test",
		Subject:   "test",
		Audiences: []string{"test"},
		IssuedAt:  now.Unix(),
		NotBefore: now.Add(-time.Hour).Unix(),
		ExpiresAt: now.Add(+time.Hour).Unix(),
	})

	// 签名
	signer, err := NewHMACSigner(HS256, []byte("test"))
	if err != nil {
		t.Error(err)
	}

	tokenString, err := tokenSrc.Sign(signer)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(tokenString)

	// 验签
	parser := NewParser(HS256)

	tokenDst, err := parser.Parse(signer, tokenString)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(tokenDst.GetClaim("uid"))
	t.Log(tokenDst.GetStandardClaims())

	//	自定义声明
	tokenSrc.WithClaim("uid", "test")

	// 重新签名
	if tokenString, err = tokenSrc.Sign(signer); err != nil {
		t.Fatal(err)
	}
	t.Log(tokenString)

	//	重新验签
	if tokenDst, err = parser.Parse(signer, tokenString); err != nil {
		t.Fatal(err)
	}
	t.Log(tokenDst.GetClaim("uid"))
	t.Log(tokenDst.GetStandardClaims())
}

func TestParser_ParseWithClaims(t *testing.T) {
	now := time.Now()
	tokenSrc := NewToken(&StandardClaims{
		Id:        "test",
		Issuer:    "test",
		Subject:   "test",
		Audiences: []string{"test"},
		IssuedAt:  now.Unix(),
		NotBefore: now.Add(-time.Hour).Unix(),
		ExpiresAt: now.Add(+time.Hour).Unix(),
	})

	// 签名
	signer, err := NewHMACSigner(HS256, []byte("test"))
	if err != nil {
		t.Error(err)
	}

	tokenString, err := tokenSrc.Sign(signer)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(tokenString)

	// 验签
	parser := NewParser(HS256)

	tokenDst, err := parser.ParseWithClaims(signer, tokenString, "test", "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Log(tokenDst.GetClaim("uid"))
	t.Log(tokenDst.GetStandardClaims())

	//	自定义声明
	tokenSrc.WithClaim("uid", "test")

	// 重新签名
	if tokenString, err = tokenSrc.Sign(signer); err != nil {
		t.Fatal(err)
	}
	t.Log(tokenString)

	//	重新验签
	if tokenDst, err = parser.ParseWithClaims(signer, tokenString, "test", "test"); err != nil {
		t.Fatal(err)
	}
	t.Log(tokenDst.GetClaim("uid"))
	t.Log(tokenDst.GetStandardClaims())
}

// cpu: Intel(R) Core(TM) i5-8250U CPU @ 1.60GHz		20		419414	      2801 ns/op	    1769 B/op	      54 allocs/op
func BenchmarkParser_Parse(b *testing.B) {
	now := time.Now()
	tokenSrc := NewToken(&StandardClaims{
		Id:        "test",
		Issuer:    "test",
		Subject:   "test",
		Audiences: []string{"test"},
		IssuedAt:  now.Unix(),
		NotBefore: now.Add(-time.Hour).Unix(),
		ExpiresAt: now.Add(+time.Hour).Unix(),
	})

	// 签名
	signer, err := NewHMACSigner(HS256, []byte("test"))
	if err != nil {
		b.Error(err)
	}

	tokenString, err := tokenSrc.Sign(signer)
	if err != nil {
		b.Fatal(err)
	}

	// 验签
	parser := NewParser(HS256)

	b.ReportAllocs()
	b.ResetTimer()

	// 压测
	for range b.N {
		if _, err := parser.Parse(signer, tokenString); err != nil {
			b.Fatal(err)
		}
	}
}

// cpu: Intel(R) Core(TM) i5-8250U CPU @ 1.60GHz		20		380750	      2893 ns/op	    1769 B/op	      54 allocs/op
func BenchmarkParser_ParseWithClaims(b *testing.B) {
	now := time.Now()
	tokenSrc := NewToken(&StandardClaims{
		Id:        "test",
		Issuer:    "test",
		Subject:   "test",
		Audiences: []string{"test"},
		IssuedAt:  now.Unix(),
		NotBefore: now.Add(-time.Hour).Unix(),
		ExpiresAt: now.Add(+time.Hour).Unix(),
	})

	// 签名
	signer, err := NewHMACSigner(HS256, []byte("test"))
	if err != nil {
		b.Error(err)
	}

	tokenString, err := tokenSrc.Sign(signer)
	if err != nil {
		b.Fatal(err)
	}

	// 验签
	parser := NewParser(HS256)

	b.ReportAllocs()
	b.ResetTimer()

	// 压测
	for range b.N {
		if _, err := parser.ParseWithClaims(signer, tokenString, "test", "test"); err != nil {
			b.Fatal(err)
		}
	}
}
