package jwt

import (
	"testing"
	"time"
)

func TestXxx(t *testing.T) {
	claims := &StandardClaims{
		Id:        "id",
		Issuer:    "issuer",
		Subject:   "subject",
		Audiences: []string{"audience"},
		IssuedAt:  time.Now().Unix(),
		NotBefore: time.Now().Add(+10 * time.Second).Unix(),
		ExpiresAt: time.Now().Add(+20 * time.Second).Unix(),
	}

	for range 25 {
		time.Sleep(1 * time.Second)
		if err := claims.Validate(); err != nil {
			t.Log(err)
		} else {
			t.Log("jwt: validation success")
		}
	}
}
