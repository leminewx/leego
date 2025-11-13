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
	signer, err := NewHMACSigner(NewHeader(HS256), []byte("test"))
	if err != nil {
		t.Fatal(err)
	}

	signature, err := signer.Sign(tokenSrc)
	if err != nil {
		t.Fatal(err)
	}
	signatureString := string(signature)
	t.Log(signatureString)

	// 验签
	parser := NewParser()
	tokenDst, err := parser.Parse(signer, signatureString)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(tokenDst.GetClaim("uid"))
	t.Log(tokenDst.GetStandardClaims())

	//	自定义声明
	tokenSrc.WithClaim("uid", "test")

	// 重新签名
	if signature, err = signer.Sign(tokenSrc); err != nil {
		t.Fatal(err)
	}
	signatureString = string(signature)
	t.Log(signatureString)

	//	重新验签
	if tokenDst, err = parser.Parse(signer, signatureString); err != nil {
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
	signer, err := NewHMACSigner(NewHeader(HS256), []byte("test"))
	if err != nil {
		t.Fatal(err)
	}
	signature, err := signer.Sign(tokenSrc)
	if err != nil {
		t.Fatal(err)
	}
	signatureString := string(signature)
	t.Log(signatureString)

	// 验签
	parser := NewParser()
	tokenDst, err := parser.ParseAndValidateStandardClaims(signer, signatureString, "test", "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Log(tokenDst.GetClaim("uid"))
	t.Log(tokenDst.GetStandardClaims())

	//	自定义声明
	tokenSrc.WithClaim("uid", "test")

	// 重新签名
	if signature, err = signer.Sign(tokenSrc); err != nil {
		t.Fatal(err)
	}
	signatureString = string(signature)
	t.Log(signatureString)

	//	重新验签
	if tokenDst, err = parser.ParseAndValidateStandardClaims(signer, signatureString, "test", "test"); err != nil {
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
	signer, err := NewHMACSigner(NewHeader(HS256), []byte("test"))
	if err != nil {
		b.Fatal(err)
	}
	tokenString, err := signer.Sign(tokenSrc)
	if err != nil {
		b.Fatal(err)
	}

	// 验签
	parser := NewParser()

	b.ReportAllocs()
	b.ResetTimer()

	// 压测
	for range b.N {
		if _, err := parser.Parse(signer, string(tokenString)); err != nil {
			b.Fatal(err)
		}
	}
}

// cpu: Intel(R) Core(TM) i5-8250U CPU @ 1.60GHz		20		413990	      2923 ns/op	    1937 B/op	      55 allocs/op
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
	signer, err := NewHMACSigner(NewHeader(HS256), []byte("test"))
	if err != nil {
		b.Fatal(err)
	}
	tokenString, err := signer.Sign(tokenSrc)
	if err != nil {
		b.Fatal(err)
	}

	// 验签
	parser := NewParser()

	b.ReportAllocs()
	b.ResetTimer()

	// 压测
	for range b.N {
		if _, err := parser.ParseAndValidateStandardClaims(signer, string(tokenString), "test", "test"); err != nil {
			b.Fatal(err)
		}
	}
}
