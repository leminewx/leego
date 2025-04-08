package jwt

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
)

// GenerateSHA256Signature 生成 SHA-256 签名
func GenerateSHA256Signature(salt []byte, data []byte) (string, error) {
	hash := hmac.New(sha256.New, salt)
	if _, err := hash.Write(data); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(hash.Sum(nil)), nil
}
