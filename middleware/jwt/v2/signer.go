package jwt

import (
	"crypto"
	"crypto/hmac"
	"fmt"
	"hash"
	"sync"
)

// Signer 定义签名器的接口
type Signer interface {
	// Sign 签名
	Sign(data []byte) ([]byte, error)
	// Verify 验证
	Verify(data, signature []byte) bool
	// GetAlgorithm 获取签名算法
	GetAlgorithm() Algorithm
}

// HMACSigner 定义 HMAC 签名器的数据结构
type HMACSigner struct {
	key       []byte
	algorithm Algorithm
	hashPool  *sync.Pool
}

// NewHMACSigner 创建 HMAC 签名器
func NewHMACSigner(alg Algorithm, key []byte) (*HMACSigner, error) {
	signer := &HMACSigner{
		key:       key,
		algorithm: alg,
	}

	switch alg {
	case ALG_HS512:
		signer.hashPool = &sync.Pool{New: func() any { return hmac.New(crypto.SHA512.New, key) }}
	case ALG_HS384:
		signer.hashPool = &sync.Pool{New: func() any { return hmac.New(crypto.SHA384.New, key) }}
	case ALG_HS256:
		signer.hashPool = &sync.Pool{New: func() any { return hmac.New(crypto.SHA256.New, key) }}
	default:
		return nil, fmt.Errorf("leego: jwt: unknown algorithm: %s", alg)
	}

	return signer, nil
}

// Sign 对数据进行签名
func (own *HMACSigner) Sign(data []byte) []byte {
	hasher := own.hashPool.Get().(hash.Hash)
	hasher.Write(data)
	signature := hasher.Sum(nil)
	hasher.Reset()
	own.hashPool.Put(hasher)
	return signature
}

// Verify 验证签名
func (own *HMACSigner) Verify(data, signature []byte) bool {
	expected := own.Sign(data)
	return hmac.Equal(signature, expected)
}

// GetAlgorithm 获取算法类型
func (own *HMACSigner) GetAlgorithm() Algorithm {
	return own.algorithm
}
