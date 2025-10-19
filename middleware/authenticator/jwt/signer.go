package jwt

import (
	"crypto"
	"crypto/hmac"
	"fmt"
	"hash"
	"sync"
)

// Signer 定义签名器的接口类型
type Signer interface {
	// Sign 签名
	Sign(data []byte) ([]byte, error)
	// Verify 验证
	Verify(data, signature []byte) bool
	// GetAlgorithm 获取签名算法
	GetAlgorithm() Algorithm
}

// hmacSigner 定义 HMAC 签名器的数据结构
type hmacSigner struct {
	pool      *sync.Pool
	algorithm Algorithm
}

/*
NewHMACSigner 创建 HMAC 签名器
  - alg：签名算法（仅支持：HS256, HS384, HS512），若算法不支持，则默认使用 HS256
*/
func NewHMACSigner(alg Algorithm, key []byte) Signer {
	signer := &hmacSigner{algorithm: alg}
	switch alg {
	case HS384:
		signer.pool = &sync.Pool{New: func() any { return hmac.New(crypto.SHA384.New, key) }}
	case HS512:
		signer.pool = &sync.Pool{New: func() any { return hmac.New(crypto.SHA512.New, key) }}
	default:
		signer.algorithm = HS256
		signer.pool = &sync.Pool{New: func() any { return hmac.New(crypto.SHA256.New, key) }}
	}

	return signer
}

// Sign 对数据进行签名
func (own *hmacSigner) Sign(data []byte) ([]byte, error) {
	hasher := own.pool.Get().(hash.Hash)
	if _, err := hasher.Write(data); err != nil {
		return nil, fmt.Errorf("leego: jwt: failed to sign data: %w", err)
	}

	signature := hasher.Sum(nil)
	hasher.Reset()
	own.pool.Put(hasher)

	return signature, nil
}

// Verify 验证签名
func (own *hmacSigner) Verify(data, signature []byte) bool {
	if expected, err := own.Sign(data); err == nil {
		return hmac.Equal(signature, expected)
	}

	return false
}

// GetAlgorithm 获取算法类型
func (own *hmacSigner) GetAlgorithm() Algorithm {
	return own.algorithm
}
