package jwt

import (
	"testing"
)

func TestParser_Parse(t *testing.T) {
	signer, err := NewHMACSigner(HS512, []byte("1234567890."))
	if err != nil {
		t.Fatal(err)
	}
	parser := NewParser()

	// ***************************************************** 标准声明 *****************************************************

	// 签名
	token, err := signer.Sign(testStandardClaims)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("JWT Token: %s", token)

	// 验签
	var claims Claims = &StandardClaims{}
	if err = parser.Parse(signer, string(token), claims); err != nil {
		t.Fatal(err)
	}
	t.Logf("User Info: %v", claims)

	// ***************************************************** 自定义声明 *****************************************************

	// 签名
	token, err = signer.Sign(testCustomizedClaims)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("JWT Token: %s", token)

	// 验签
	claims = &customizedClaims{}
	if err = parser.Parse(signer, string(token), claims); err != nil {
		t.Fatal(err)
	}
	t.Logf("User Info1: %v", claims.(*customizedClaims).StandardClaims)
	t.Logf("User Info2: %v", claims)
}

// cpu: QEMU Virtual CPU version 2.5+	standard		2   	  186280	      6018 ns/op	     848 B/op	      17 allocs/op
// cpu: QEMU Virtual CPU version 2.5+	customized		2   	  140084	      8204 ns/op	    1072 B/op	      22 allocs/op
func BenchmarkParser_Parse(b *testing.B) {
	signer, err := NewHMACSigner(HS384, []byte("1234567890."))
	if err != nil {
		b.Fatal(err)
	}
	parser := NewParser()

	b.Run("标准声明", func(b *testing.B) {
		token, err := signer.Sign(testStandardClaims)
		if err != nil {
			b.Fatal(err)
		}
		tokenString := string(token)

		b.ReportAllocs()
		b.ResetTimer()

		for range b.N {
			claims := &StandardClaims{}
			if err = parser.Parse(signer, tokenString, claims); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("自定义声明", func(b *testing.B) {
		token, err := signer.Sign(testCustomizedClaims)
		if err != nil {
			b.Fatal(err)
		}
		tokenString := string(token)

		b.ReportAllocs()
		b.ResetTimer()

		for range b.N {
			claims := &customizedClaims{}
			if err = parser.Parse(signer, tokenString, claims); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// cpu: QEMU Virtual CPU version 2.5+	standard		2   	  184634	      6052 ns/op	     848 B/op	      17 allocs/op
// cpu: QEMU Virtual CPU version 2.5+	customized		2   	  140335	      8207 ns/op	    1072 B/op	      22 allocs/op
func BenchmarkParser_ParseAndValidateStandardClaims(b *testing.B) {
	signer, err := NewHMACSigner(HS384, []byte("1234567890."))
	if err != nil {
		b.Fatal(err)
	}
	parser := NewParser()

	b.Run("标准声明", func(b *testing.B) {
		token, err := signer.Sign(testStandardClaims)
		if err != nil {
			b.Fatal(err)
		}
		tokenString := string(token)

		b.ReportAllocs()
		b.ResetTimer()

		for range b.N {
			claims := &StandardClaims{}
			if err = parser.ParseAndValidateStandardClaims(signer, tokenString, claims); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("自定义声明", func(b *testing.B) {
		token, err := signer.Sign(testCustomizedClaims)
		if err != nil {
			b.Fatal(err)
		}
		tokenString := string(token)

		b.ReportAllocs()
		b.ResetTimer()

		for range b.N {
			claims := &customizedClaims{}
			if err = parser.ParseAndValidateStandardClaims(signer, tokenString, claims); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// cpu: QEMU Virtual CPU version 2.5+	standard		2   	  181467	      6137 ns/op	     848 B/op	      17 allocs/op
// cpu: QEMU Virtual CPU version 2.5+	customized		2   	  127566	      8269 ns/op	    1072 B/op	      22 allocs/op
func BenchmarkParser_ParseAndValidateStandardClaimsWithIssuerAndAudiences(b *testing.B) {
	signer, err := NewHMACSigner(HS384, []byte("1234567890."))
	if err != nil {
		b.Fatal(err)
	}
	parser := NewParser()

	b.Run("标准声明", func(b *testing.B) {
		token, err := signer.Sign(testStandardClaims)
		if err != nil {
			b.Fatal(err)
		}
		tokenString := string(token)

		b.ReportAllocs()
		b.ResetTimer()

		for range b.N {
			claims := &StandardClaims{}
			if err = parser.ParseAndValidateStandardClaimsWithIssuerAndAudiences(signer, tokenString, "issuer", "audience", claims); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("自定义声明", func(b *testing.B) {
		token, err := signer.Sign(testCustomizedClaims)
		if err != nil {
			b.Fatal(err)
		}
		tokenString := string(token)

		b.ReportAllocs()
		b.ResetTimer()

		for range b.N {
			claims := &customizedClaims{}
			if err = parser.ParseAndValidateStandardClaimsWithIssuerAndAudiences(signer, tokenString, "issuer", "audience", claims); err != nil {
				b.Fatal(err)
			}
		}
	})
}
