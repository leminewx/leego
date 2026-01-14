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
	// GetHeader 后期签名头信息
	GetHeader() Header
}

const (
	HS256 Algorithm = "HS256" // HMAC-SHA256
	HS384 Algorithm = "HS384" // HMAC-SHA384
	HS512 Algorithm = "HS512" // HMAC-SHA512
)

// hmacSigner 定义 HMAC 签名器的数据结构
type hmacSigner struct {
	header   Header
	hashPool *sync.Pool
}

/*
NewHMACSigner 创建 HMAC 签名器
  - alg：签名算法（仅支持：HS256, HS384, HS512），若算法不支持，则默认使用 HS256
*/
func NewHMACSigner(header Header, key []byte) (Signer, error) {
	signer := &hmacSigner{header: header}
	switch header.GetAlgorithm() {
	case HS256:
		signer.hashPool = &sync.Pool{New: func() any { return hmac.New(crypto.SHA256.New, key) }}
	case HS384:
		signer.hashPool = &sync.Pool{New: func() any { return hmac.New(crypto.SHA384.New, key) }}
	case HS512:
		signer.hashPool = &sync.Pool{New: func() any { return hmac.New(crypto.SHA512.New, key) }}
	default:
		return nil, fmt.Errorf("jwt: HMAC signer unsupported algorithm: %s", header.GetAlgorithm())
	}

	return signer, nil
}

// GetHeader 获取签名头信息
func (own *hmacSigner) GetHeader() Header {
	return own.header
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
	signingInput := own.header.GetEncodedString() + "." + claimsBase64String

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
func (own *hmacSigner) sign(data []byte) (signature []byte, err error) {
	// 获取签名函数
	hasher := own.hashPool.Get().(hash.Hash)

	// 生成签名
	if _, err = hasher.Write(data); err == nil {
		signature = hasher.Sum(nil)
	} else {
		err = fmt.Errorf("jwt: failed to sign token: %w", err)
	}

	// 释放签名函数
	hasher.Reset()
	own.hashPool.Put(hasher)

	return
}
