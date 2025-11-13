package jwt

import (
	"fmt"
	"strconv"
	"testing"
)

func TestAuthenticator_GenerateToken(t *testing.T) {
	signer, err := NewHMACSigner(NewHeader(HS256), []byte("test"))
	if err != nil {
		t.Fatal(err)
	}

	authenticator := NewAuthenticator(NewDefaultConfig(), signer, NewParser())

	for i := range 10 {
		t.Log(*authenticator.GenerateToken(&StandardClaims{Id: strconv.Itoa(i)}).GetStandardClaims())
	}
}

// cpu: Intel(R) Core(TM) i5-14600KF		20    	22280605	        52.82 ns/op	     136 B/op	       3 allocs/op
func BenchmarkAuthenticator_GenerateToken(b *testing.B) {
	signer, err := NewHMACSigner(NewHeader(HS256), []byte("test"))
	if err != nil {
		b.Fatal(err)
	}

	authenticator := NewAuthenticator(NewDefaultConfig(), signer, NewParser())

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_ = authenticator.GenerateToken(&StandardClaims{Id: "0123456789"})
	}
}

func TestAuthenticator_Sign(t *testing.T) {
	signer, err := NewHMACSigner(NewHeader(HS256), []byte("test"))
	if err != nil {
		t.Fatal(err)
	}

	authenticator := NewAuthenticator(NewDefaultConfig(), signer, NewParser())

	for i := range 10 {
		token := authenticator.GenerateToken(&StandardClaims{Id: strconv.Itoa(i)})
		signature, _ := authenticator.Sign(token)
		fmt.Println(string(signature))
	}
}

// cpu: Intel(R) Core(TM) i5-14600KF		20    	1990297	       598.1 ns/op	     833 B/op	       9 allocs/op
func BenchmarkAuthenticator_Sign(b *testing.B) {
	signer, err := NewHMACSigner(NewHeader(HS256), []byte("test"))
	if err != nil {
		b.Fatal(err)
	}

	authenticator := NewAuthenticator(NewDefaultConfig(), signer, NewParser())

	token := authenticator.GenerateToken(&StandardClaims{Id: "0123456789"})

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		if _, err := authenticator.Sign(token); err != nil {
			b.Fatal(err)
		}
	}
}

func TestAuthenticator_Verify(t *testing.T) {
	signer, err := NewHMACSigner(NewHeader(HS256), []byte("test"))
	if err != nil {
		t.Fatal(err)
	}

	authenticator := NewAuthenticator(NewDefaultConfig(), signer, NewParser())

	tokenSrc := authenticator.GenerateToken(&StandardClaims{Id: "0123456789"})
	signature, err := authenticator.Sign(tokenSrc)
	if err != nil {
		t.Fatal(err)
	}

	tokenDst, err := authenticator.Verify(string(signature))
	if err != nil {
		t.Fatal(err)
	}

	t.Log(*tokenDst.GetStandardClaims())
}

// cpu: Intel(R) Core(TM) i5-14600KF		20    	531808	      2137 ns/op	    1489 B/op	      40 allocs/op
func BenchmarkAuthenticator_Verify(b *testing.B) {
	signer, err := NewHMACSigner(NewHeader(HS256), []byte("test"))
	if err != nil {
		b.Fatal(err)
	}

	authenticator := NewAuthenticator(NewDefaultConfig(), signer, NewParser())

	token := authenticator.GenerateToken(&StandardClaims{Id: "0123456789"})
	signature, err := authenticator.Sign(token)
	if err != nil {
		b.Fatal(err)
	}
	tokenString := string(signature)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		if _, err := authenticator.Verify(tokenString); err != nil {
			b.Fatal(err)
		}
	}
}
