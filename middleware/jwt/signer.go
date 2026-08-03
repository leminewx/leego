package jwt

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
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

// hmacSigner 定义 HMAC 签名器的结构类型
type hmacSigner struct {
	header   Header
	hashPool *sync.Pool
}

// NewHMACSigner 创建 HMAC 签名器（签名算法仅支持：HS256, HS384, HS512，默认使用 HS256）
// key 不能为空，否则 HMAC 退化为无密钥运算，任意载荷都可被伪造
func NewHMACSigner(algorithm Algorithm, key []byte) (Signer, error) {
	if len(key) == 0 {
		return nil, errors.New("jwt: HMAC key must not be empty")
	}
	signer := &hmacSigner{}
	switch algorithm {
	case HS512:
		signer.hashPool = &sync.Pool{New: func() any { return hmac.New(sha512.New, key) }}
	case HS384:
		signer.hashPool = &sync.Pool{New: func() any { return hmac.New(sha512.New384, key) }}
	default:
		algorithm = HS256
		signer.hashPool = &sync.Pool{New: func() any { return hmac.New(sha256.New, key) }}
	}

	signer.header = NewHeader(algorithm)
	return signer, nil
}

// GetHeader 获取签名头信息
func (own *hmacSigner) GetHeader() Header {
	return own.header
}

// Sign 对数据进行签名
func (own *hmacSigner) Sign(claims Claims) ([]byte, error) {
	// 检查声明
	if claims == nil {
		return nil, ErrNilClaims
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
		err = fmt.Errorf("jwt: failed to sign token: %v", err)
	}

	// 释放签名函数
	hasher.Reset()
	own.hashPool.Put(hasher)

	return
}
