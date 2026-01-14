package jwt

import (
	"crypto"
	"crypto/hmac"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash"
	"sync"
)

// Signer 定义签名器的接口类型
type Signer interface {
	// Sign 签名
	Sign(claims Claims) ([]byte, error)
	// Verify 验证
	Verify(src, dst []byte) bool

	// GetAlgorithm 获取签名算法
	GetAlgorithm() Algorithm
	// GetHeaderEncodedString 获取 Header 编码后的字符串
	GetHeaderEncodedString() string
}

const (
	HS256 Algorithm = "HS256" // HMAC-SHA256
	HS384 Algorithm = "HS384" // HMAC-SHA384
	HS512 Algorithm = "HS512" // HMAC-SHA512
)

// hmacSigner 定义 HMAC 签名器的数据结构
type hmacSigner struct {
	pool   *sync.Pool
	header *Header

	// 保存 header 编码后的字符串，提高性能
	headerEncodedString string
}

/*
NewHMACSigner 创建 HMAC 签名器
  - alg：签名算法（仅支持：HS256, HS384, HS512），若算法不支持，则默认使用 HS256
*/
func NewHMACSigner(header Header, key []byte) (Signer, error) {
	signer := &hmacSigner{header: &header}
	switch header.Algorithm {
	case HS256:
		signer.pool = &sync.Pool{New: func() any { return hmac.New(crypto.SHA256.New, key) }}
	case HS384:
		signer.pool = &sync.Pool{New: func() any { return hmac.New(crypto.SHA384.New, key) }}
	case HS512:
		signer.pool = &sync.Pool{New: func() any { return hmac.New(crypto.SHA512.New, key) }}
	default:
		return nil, fmt.Errorf("jwt: unsupported algorithm: %s", header.Algorithm)
	}

	// 序列化
	headerBytes, err := json.Marshal(&header)
	if err != nil {
		return nil, fmt.Errorf("jwt: failed to marshal header: %v", err)
	}

	// Base64 编码
	signer.headerEncodedString = base64.RawURLEncoding.EncodeToString(headerBytes)
	return signer, err
}

// GetAlgorithm 获取算法类型
func (own *hmacSigner) GetAlgorithm() Algorithm {
	return own.header.Algorithm
}

// GetHeaderEncodedString 获取 Header 的编码字符串
func (own *hmacSigner) GetHeaderEncodedString() string {
	return own.headerEncodedString
}

// Sign 对数据进行签名
func (own *hmacSigner) Sign(claims Claims) ([]byte, error) {
	if claims == nil {
		return nil, ErrInvalidClaims
	}

	// 序列化
	claimsBytes, err := json.Marshal(claims)
	if err != nil {
		return nil, fmt.Errorf("jwt: failed to marshal claims: %v", err)
	}

	// Base64 编码
	claimsBase64String := base64.RawURLEncoding.EncodeToString(claimsBytes)

	// 拼接前缀
	signingInput := own.headerEncodedString + "." + claimsBase64String

	// 生成签名
	signature, err := own.sign([]byte(signingInput))
	if err != nil {
		return nil, err
	}

	// 编码签名，并返回完整的 JWT 字符串
	return []byte(signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)), nil

}

// Verify 验证签名
func (own *hmacSigner) Verify(src, dst []byte) bool {
	if expected, err := own.sign(src); err == nil {
		return hmac.Equal(dst, expected)
	}

	return false
}

// sign 签名函数
func (own *hmacSigner) sign(data []byte) ([]byte, error) {
	// 获取签名函数
	hasher := own.pool.Get().(hash.Hash)

	// 生成签名
	var signature []byte
	if _, err := hasher.Write(data); err == nil {
		signature = hasher.Sum(nil)
	} else {
		err = fmt.Errorf("jwt: failed to sign token: %w", err)
	}

	// 释放签名函数
	hasher.Reset()
	own.pool.Put(hasher)

	return signature, nil
}
