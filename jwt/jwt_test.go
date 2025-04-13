package jwt

import (
	"testing"
	"time"
)

func TestJwt_encode(t *testing.T) {
	sign, _ := encode(&map[string]any{"typ": "JWT", "alg": ALG_HS256})
	t.Log(sign)

	sign, _ = encode(NewClaims().WithIssuer("lq").WithSubject("test").WithAudience("leego").WithIssuedAt(time.Now().Unix()).WithNotBefore(time.Now().Add(1 * time.Hour).Unix()).WithExpiration(time.Now().Add(7 * 24 * time.Hour).Unix()))
	t.Log(sign)

}
