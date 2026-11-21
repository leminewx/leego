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
//   - key：密钥，不能为空，且长度不得小于所选算法的哈希输出长度
//     （RFC 7518 §3.2：HS256≥32、HS384≥48、HS512≥64 字节）。
//     过短的密钥会降低签名的抗暴力破解强度，因此直接拒绝。
func NewHMACSigner(algorithm Algorithm, key []byte) (Signer, error) {
	if len(key) == 0 {
		return nil, errors.New("jwt: HMAC key must not be empty")
	}

	minimum := minHMACKeyLength(algorithm)
	if minimum == 0 {
		// 未知算法一律拒绝，避免“笔误静默降级为 HS256”带来的安全与互操作问题。
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedMethod, algorithm)
	}

	if len(key) < minimum {
		return nil, fmt.Errorf("jwt: %s requires a key of at least %d bytes, got %d", algorithm, minimum, len(key))
	}

	signer := &hmacSigner{}
	switch algorithm {
	case HS512:
		signer.hashPool = &sync.Pool{New: func() any { return hmac.New(sha512.New, key) }}
	case HS384:
		signer.hashPool = &sync.Pool{New: func() any { return hmac.New(sha512.New384, key) }}
	default:
		signer.hashPool = &sync.Pool{New: func() any { return hmac.New(sha256.New, key) }}
	}

	signer.header = NewHeader(algorithm)
	return signer, nil
}

// minHMACKeyLength 返回算法要求的最小密钥长度（字节）。
//
// 依据 RFC 7518 §3.2，HMAC 密钥长度不得短于哈希输出长度；
// 未知算法返回 0。
func minHMACKeyLength(algorithm Algorithm) int {
	switch algorithm {
	case HS256:
		return 32
	case HS384:
		return 48
	case HS512:
		return 64
	default:
		return 0
	}
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
