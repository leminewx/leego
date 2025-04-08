package jwt

import (
	"crypto"
	"crypto/hmac"
	"crypto/rsa"
	"fmt"
)

type Algorithm int8

const (
	ALG_HS256 Algorithm = iota
	ALG_HS384
	ALG_HS512
)

// GetAlgorithmHash 获取算法的 Hash 类型
func (own Algorithm) GetAlgorithmHash() crypto.Hash {
	switch own {
	case ALG_HS512:
		return crypto.SHA512
	case ALG_HS384:
		return crypto.SHA384
	default:
		return crypto.SHA256
	}
}

// Encrypt 加签
func (own Algorithm) Encrypt(data []byte, salt any) ([]byte, error) {
	hash := own.GetAlgorithmHash()
	switch key := salt.(type) {
	case []byte:
		hasher := hmac.New(hash.New, key)
		hasher.Write(data)
		return hasher.Sum(nil), nil
	case *rsa.PrivateKey:
		return rsa.SignPKCS1v15(nil, key, hash, data)
	default:
		return nil, fmt.Errorf("leego: jwt: unsupported algorithm: %v", own)
	}
}

// Decrypt 验签
func (own Algorithm) Decrypt(data, sign []byte, salt any) error {
	hash := own.GetAlgorithmHash()
	switch key := salt.(type) {
	case []byte:
		hasher := hmac.New(hash.New, key)
		hasher.Write(data)

		if !hmac.Equal(sign, hasher.Sum(nil)) {
			return errInvalidSignature
		}

		return nil
	case *rsa.PublicKey:
		return rsa.VerifyPKCS1v15(key, hash, data, sign)
	default:
		return fmt.Errorf("leego: jwt: unsupported algorithm: %v", own)
	}
}
