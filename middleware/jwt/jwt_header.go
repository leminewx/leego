package jwt

import (
	"crypto"
	"crypto/hmac"
	"encoding/base64"
	"fmt"
	"hash"
	"sync"
)

type Algorithm string

const (
	ALG_HS256 Algorithm = "HS256"
	ALG_HS384 Algorithm = "HS384"
	ALG_HS512 Algorithm = "HS512"
)

// Header 定义 JWT 的 Header 结构
type Header struct {
	pool      *sync.Pool
	algorithm Algorithm
	signature []byte
}

// NewHeader 新建一个 JWT Header
func NewHeader(alg Algorithm, salt []byte) (*Header, error) {
	if len(salt) == 0 {
		return nil, fmt.Errorf("leego: jwt: failed to create jwt header, empty salt")
	}

	sign, _ := encode(&map[string]any{"typ": "JWT", "alg": alg})
	header := &Header{algorithm: alg, signature: sign}

	switch alg {
	case ALG_HS512:
		header.pool = &sync.Pool{New: func() any { return hmac.New(crypto.SHA512.New, salt) }}
	case ALG_HS384:
		header.pool = &sync.Pool{New: func() any { return hmac.New(crypto.SHA384.New, salt) }}
	case ALG_HS256:
		header.pool = &sync.Pool{New: func() any { return hmac.New(crypto.SHA256.New, salt) }}
	default:
		return nil, fmt.Errorf("leego: jwt: failed to create jwt header, unsupported algorithm: %v", alg)
	}

	return header, nil
}

// Encode 编码
func (own *Header) Encode(claims Claims) string {
	payload, _ := encode(&claims)
	sign := make([]byte, 0, len(own.signature)+len(payload)+1)
	sign = append(append(append(sign, own.signature...), '.'), payload...)

	hasher := own.pool.Get().(hash.Hash)
	hasher.Write(sign)
	signature := hasher.Sum(nil)
	hasher.Reset()
	own.pool.Put(hasher)

	return string(sign) + "." + base64.RawURLEncoding.EncodeToString(signature)
}

// Validate 验证
func (own *Header) Validate(payload, signature []byte) error {
	hasher := own.pool.Get().(hash.Hash)
	sign := make([]byte, 0, len(own.signature)+len(payload)+1)
	hasher.Write(append(append(append(sign, own.signature...), '.'), payload...))
	if !hmac.Equal([]byte(base64.RawURLEncoding.EncodeToString(hasher.Sum(nil))), signature) {
		hasher.Reset()
		own.pool.Put(hasher)
		return errInvalidSignature
	}

	hasher.Reset()
	own.pool.Put(hasher)
	return nil
}
