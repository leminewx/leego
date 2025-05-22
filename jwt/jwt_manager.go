package jwt

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Manager JWT管理器
type Manager struct {
	// issuer 令牌的签发者
	issuer string
	// header 令牌的头部信息
	header *Header
	// expiration 令牌的有效时间
	expiration time.Duration
}

// NewManager 创建 JWT 令牌管理器
func NewManager(algorithm Algorithm, salt []byte, expiration time.Duration) (*Manager, error) {
	header, err := NewHeader(algorithm, salt)
	if err != nil {
		return nil, err
	}

	return &Manager{
		header:     header,
		expiration: expiration,
	}, nil
}

// WithIssuer 指定令牌的签发者
func (own *Manager) WithIssuer(iss string) *Manager {
	own.issuer = iss
	return own
}

// Generate 生成 JWT 令牌
func (own *Manager) Generate(claims Claims) string {
	now := time.Now()

	// 设置签发时间
	claims.WithIssuedAt(now.Unix())

	// 设置过期时间
	claims.WithExpiration(now.Add(own.expiration).Unix())

	// 设置签发者
	if own.issuer != "" {
		claims.WithIssuer(own.issuer)
	}

	// 编码令牌
	return own.header.Encode(claims)
}

// 定义错误类型
var (
	errInvalidToken     = errors.New("invalid token")
	errExpiredToken     = errors.New("token has expired")
	errIneffectiveToken = errors.New("token not yet valid")
	errInvalidSignature = errors.New("invalid signature")
)

// Validate 验证 JWT 令牌
func (own *Manager) Validate(token string, claims Claims) error {
	// 拆分令牌
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return errInvalidToken
	}

	// 验证签名
	if err := own.header.Validate([]byte(parts[1]), []byte(parts[2])); err != nil {
		return err
	}

	// 解码 Claims
	claimsBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return errInvalidToken
	}

	// 反序列化 Claims
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		return errInvalidToken
	}

	// 验证 Claims
	return claims.Validate()
}

// Refresh 刷新 JWT 令牌
// func (own *Manager) Refresh(token string) (string, error) {}

func encode(data any) ([]byte, error) {
	src, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	return []byte(base64.RawURLEncoding.EncodeToString(src)), nil
}
