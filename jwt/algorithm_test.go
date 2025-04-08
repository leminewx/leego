package jwt

import "testing"

func TestAlgorithm_HS256(t *testing.T) {
	var alg Algorithm = ALG_HS256
	data := []byte("hello world, leminewx")
	salt := []byte("0123456789qwertyuiopasdfghjklzxcvbnm")

	// 加签
	sign, err := alg.Encrypt(data, salt)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(sign)

	// 验签
	if err = alg.Decrypt(data, sign, salt); err != nil {
		t.Fatal(err)
	}
}
