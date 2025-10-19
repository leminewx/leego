package authenticator

import (
	"strconv"
	"testing"

	"gitee.com/leminewx/leego/middleware/authenticator/jwt"
)

func TestAuthenticator_GenerateToken(t *testing.T) {
	parser := jwt.NewParser()
	signer := jwt.NewHMACSigner(jwt.HS256, []byte("secret"))
	authenticator := NewAuthenticator(NewDefaultConfig("JWT 认证管理器"), signer, parser)

	for i := range 10 {
		t.Log(*authenticator.GenerateToken(&jwt.StandardClaims{Id: strconv.Itoa(i)}).GetStandardClaims())
	}
}

// cpu: Intel(R) Core(TM) i5-14600KF		20    	19991403	        60.73 ns/op	     192 B/op	       3 allocs/op
func BenchmarkAuthenticator_GenerateToken(b *testing.B) {
	parser := jwt.NewParser()
	signer := jwt.NewHMACSigner(jwt.HS256, []byte("secret"))
	authenticator := NewAuthenticator(NewDefaultConfig("JWT 认证管理器"), signer, parser)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_ = authenticator.GenerateToken(&jwt.StandardClaims{Id: "0123456789"})
	}
}

func TestAuthenticator_Sign(t *testing.T) {
	parser := jwt.NewParser()
	signer := jwt.NewHMACSigner(jwt.HS256, []byte("secret"))
	authenticator := NewAuthenticator(NewDefaultConfig("JWT 认证管理器"), signer, parser)

	for i := range 10 {
		t.Log(authenticator.GenerateToken(&jwt.StandardClaims{Id: strconv.Itoa(i)}).Sign(signer))
	}
}

// cpu: Intel(R) Core(TM) i5-14600KF		20    	1596573	       748.2 ns/op	     961 B/op	      12 allocs/op
func BenchmarkAuthenticator_Sign(b *testing.B) {
	parser := jwt.NewParser()
	signer := jwt.NewHMACSigner(jwt.HS256, []byte("secret"))
	authenticator := NewAuthenticator(NewDefaultConfig("JWT 认证管理器"), signer, parser)

	token := authenticator.GenerateToken(&jwt.StandardClaims{Id: "0123456789"})

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		if _, err := authenticator.Sign(token); err != nil {
			b.Fatal(err)
		}
	}
}

func TestAuthenticator_Verify(t *testing.T) {
	parser := jwt.NewParser()
	signer := jwt.NewHMACSigner(jwt.HS256, []byte("secret"))
	authenticator := NewAuthenticator(NewDefaultConfig("JWT 认证管理器"), signer, parser)

	signature, err := authenticator.GenerateToken(&jwt.StandardClaims{Id: "0123456789"}).Sign(signer)
	if err != nil {
		t.Fatal(err)
	}

	token, err := authenticator.Verify(signature)
	if err != nil {
		t.Fatal(err)
	}

	t.Log(*token.GetStandardClaims())
}

// cpu: Intel(R) Core(TM) i5-14600KF		20    	524584	      2128 ns/op	    1545 B/op	      40 allocs/op
func BenchmarkAuthenticator_Verify(b *testing.B) {
	parser := jwt.NewParser()
	signer := jwt.NewHMACSigner(jwt.HS256, []byte("secret"))
	authenticator := NewAuthenticator(NewDefaultConfig("JWT 认证管理器"), signer, parser)

	signature, err := authenticator.GenerateToken(&jwt.StandardClaims{Id: "0123456789"}).Sign(signer)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		if _, err := authenticator.Verify(signature); err != nil {
			b.Fatal(err)
		}
	}
}
