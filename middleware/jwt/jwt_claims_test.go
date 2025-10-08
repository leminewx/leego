package jwt

// func TestClaims(t *testing.T) {
// 	claims := NewClaims().
// 		WithSubject("test").
// 		WithAudience("leego").
// 		WithExpiration(time.Now().Add(7 * 24 * time.Hour).Unix()).
// 		WithIssuedAt(time.Now().Unix()).
// 		WithNotBefore(time.Now().Add(1 * time.Hour).Unix()).
// 		WithIssuer("lemine")

// 	t.Logf("claims: %v", claims)
// 	t.Logf("encode claims: %s", claims.Encode())

// 	data, err := base64.RawURLEncoding.DecodeString(claims.Encode())
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	c := NewClaims()
// 	if err := json.Unmarshal(data, &c); err != nil {
// 		t.Fatal(err)
// 	}
// 	t.Logf("claims: %v", claims)
// }

// // cpu: Intel(R) Core(TM) i5-14600KF		20		1342328	       900.0 ns/op	     784 B/op	      16 allocs/op
// func BenchmarkClaims(b *testing.B) {
// 	claims := NewClaims().
// 		WithSubject("test").
// 		WithAudience("leego").
// 		WithExpiration(time.Now().Add(7 * 24 * time.Hour).Unix()).
// 		WithIssuedAt(time.Now().Unix()).
// 		WithNotBefore(time.Now().Add(1 * time.Hour).Unix()).
// 		WithIssuer("lemine")

// }
