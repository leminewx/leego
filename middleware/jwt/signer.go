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
	Sign(token *Token) ([]byte, error)
	// Verify 验证
	Verify(src, dst []byte) bool

	// GetAlgorithm 获取签名算法
	GetAlgorithm() Algorithm
	// GetHeaderEncodedString 获取 Header 编码后的字符串
	GetHeaderEncodedString() string
}

// hmacSigner 定义 HMAC 签名器的数据结构
type hmacSigner struct {
	pool                *sync.Pool
	header              *Header
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
		return nil, fmt.Errorf("leego: jwt: unsupported algorithm: %s", header.Algorithm)
	}

	var err error
	signer.headerEncodedString, err = header.Encode()

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
func (own *hmacSigner) Sign(token *Token) ([]byte, error) {
	if token == nil {
		return nil, errTokenNil
	}

	// 同步声明
	token.syncClaims()
	// 编码声明
	encodedClaims, err := token.encodeClaims()
	if err != nil {
		return nil, err
	}

	// 拼接前缀
	signingInput := own.headerEncodedString + "." + encodedClaims
	// 生成签名
	signature, err := own.sign([]byte(signingInput))
	if err != nil {
		return nil, err
	}

	// 编码签名，并返回完整的 JWT 字符串
	return []byte(signingInput + "." + EncodeBase64(signature)), nil

}

// sign 签名函数
func (own *hmacSigner) sign(data []byte) ([]byte, error) {
	hasher := own.pool.Get().(hash.Hash)
	if _, err := hasher.Write(data); err != nil {
		return nil, fmt.Errorf("leego: jwt: failed to sign token: %w", err)
	}

	signature := hasher.Sum(nil)
	hasher.Reset()
	own.pool.Put(hasher)

	return signature, nil
}

// Verify 验证签名
func (own *hmacSigner) Verify(src, dst []byte) bool {
	if expected, err := own.sign(src); err == nil {
		return hmac.Equal(dst, expected)
	}

	return false
}
