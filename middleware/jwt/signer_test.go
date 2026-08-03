package jwt

import (
	"testing"
)

// eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJqdGkiOiJpZCIsImlzcyI6Imlzc3VlciIsInN1YiI6InN1YmplY3QiLCJhdWQiOlsiYXVkaWVuY2UiXSwiaWF0IjoxNzY4NDY5MTkwLCJuYmYiOjE3Njg0NjU1OTAsImV4cCI6MTc2ODQ3Mjc5MH0.880PHGkJyHSTxfvZxuCPyvjcXX40jV5esrJntsGI6N4
// eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJqdGkiOiJpZCIsImlzcyI6Imlzc3VlciIsInN1YiI6InN1YmplY3QiLCJhdWQiOlsiYXVkaWVuY2UiXSwiaWF0IjoxNzY4NDY5MTkwLCJuYmYiOjE3Njg0NjkxOTAsImV4cCI6MTc2ODU1NTU5MCwidXNlcm5hbWUiOiJ1c2VybmFtZSIsInRlYW0iOiJ0ZWFtIiwicm9sZSI6InJvbGUifQ.SrN7oluYq7KA4ASWKbGP5zE1F2n-Zx_VLCJS8cFA1Uw
func TestSigner(t *testing.T) {
	signer, err := NewHMACSigner(HS256, []byte("123456789."))
	if err != nil {
		t.Fatal(err)
	}
	token, err := signer.Sign(testStandardClaims)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(string(token))

	token, err = signer.Sign(testCustomizedClaims)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(string(token))

}

// cpu: QEMU Virtual CPU version 2.5+	standard		2   	  239024	      4628 ns/op	    1200 B/op	       9 allocs/op
// cpu: QEMU Virtual CPU version 2.5+	customized		2   	  203030	      5565 ns/op	    1584 B/op	       9 allocs/op
func BenchmarkSigner_Sign(b *testing.B) {
	signer, err := NewHMACSigner(HS256, []byte("123456789."))
	if err != nil {
		b.Fatal(err)
	}

	b.Run("标准声明", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for range b.N {
			if _, err := signer.Sign(testStandardClaims); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("自定义声明", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for range b.N {
			if _, err := signer.Sign(testCustomizedClaims); err != nil {
				b.Fatal(err)
			}
		}
	})
}
