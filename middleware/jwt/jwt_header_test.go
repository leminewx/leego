package jwt

import (
	"testing"
	"time"
)

func TestHeader_Encrypt(t *testing.T) {
	header, err := NewHeader(ALG_HS256, []byte("0123456789qwertyuiopasdfghjklzxcvbnm"))
	if err != nil {
		t.Fatal(err)
	}

	claims := NewClaims().
		WithIssuer("lemine").
		WithSubject("test").
		WithAudience("leego").
		WithIssuedAt(time.Now().Unix()).
		WithNotBefore(time.Now().Add(1 * time.Hour).Unix()).
		WithExpiration(time.Now().Add(7 * 24 * time.Hour).Unix())

	t.Logf("signature: %s", header.Encode(claims))
}
