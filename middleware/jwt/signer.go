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

// Signer 定义 JWT 签名器必须提供的能力。
type Signer interface {
	// Sign 将声明序列化并签发为完整令牌。
	Sign(claims Claims) ([]byte, error)
	// Verify 验证签名；src 为待验签数据，dst 为令牌中的签名。
	Verify(src, dst []byte) bool
	// GetHeader 返回签名器使用的头部。
	GetHeader() Header
}

// hmacSigner 是基于 HMAC 的签名器实现。
//
// 哈希对象通过 sync.Pool 复用，避免每次签发都创建新的 HMAC 实例。
type hmacSigner struct {
	// header 为签名器使用的头部。
	header Header
	// hashPool 复用哈希对象。
	hashPool *sync.Pool
}

// 编译期断言：*hmacSigner 必须实现 Signer。
var _ Signer = (*hmacSigner)(nil)

// NewHMACSigner 创建 HMAC 签名器。
//
// 参数：
//   - algorithm：HS256 / HS384 / HS512，其他值回退为 HS256；
//   - key：密钥，不能为空——空密钥会让 HMAC 失去认证能力，任意载荷都可被伪造。
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
	case HS256:
		signer.hashPool = &sync.Pool{New: func() any { return hmac.New(sha256.New, key) }}
	default:
		// 未知算法一律拒绝，避免“笔误静默降级为 HS256”带来的安全与互操作问题。
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedMethod, algorithm)
	}

	signer.header = NewHeader(algorithm)
	return signer, nil
}

// GetHeader 返回签名器使用的头部。
func (own *hmacSigner) GetHeader() Header {
	return own.header
}

// Sign 将声明序列化并签发为完整令牌。
//
// 令牌格式为 header.payload.signature，三段均为 Base64URL（无填充）。
func (own *hmacSigner) Sign(claims Claims) ([]byte, error) {
	if claims == nil {
		return nil, ErrNilClaims
	}

	payload, err := json.Marshal(claims)
	if err != nil {
		return nil, fmt.Errorf("jwt: failed to marshal claims: %w", err)
	}

	signingInput := own.header.GetEncodedString() + "." + base64.RawURLEncoding.EncodeToString(payload)
	signature, err := own.sign([]byte(signingInput))
	if err != nil {
		return nil, err
	}

	return []byte(signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)), nil
}

// Verify 验证签名。
//
// 使用 hmac.Equal 做常数时间比较，避免通过响应时间侧信道推导签名。
func (own *hmacSigner) Verify(src, dst []byte) bool {
	expected, err := own.sign(src)
	if err != nil {
		return false
	}

	return hmac.Equal(dst, expected)
}

// sign 计算给定数据的 HMAC 值。
func (own *hmacSigner) sign(data []byte) ([]byte, error) {
	hasher, ok := own.hashPool.Get().(hash.Hash)
	if !ok || hasher == nil {
		return nil, errors.New("jwt: failed to acquire hash instance")
	}

	hasher.Reset()
	if _, err := hasher.Write(data); err != nil {
		own.hashPool.Put(hasher)
		return nil, fmt.Errorf("jwt: failed to sign token: %w", err)
	}

	signature := hasher.Sum(nil)
	hasher.Reset()
	own.hashPool.Put(hasher)
	return signature, nil
}
