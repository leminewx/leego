package jwt

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Manager JWT管理器
type Manager struct {
	// salt 用于签名算法的盐
	slat any
	// algorithm 签名算法
	algorithm Algorithm
	// expiration 令牌的有效时间
	expiration time.Duration
	// blacklist 黑名单
	// blacklist *TokenBlacklist
}

// NewManager 创建JWT管理器
func NewManager(algorithm Algorithm, salt any, expiration time.Duration) *Manager {
	return &Manager{
		slat:       salt,
		algorithm:  algorithm,
		expiration: expiration,
	}
}

// Generate 生成 JWT 令牌
func (own *Manager) Generate(claims Claims) (string, error) {
	// 设置签发时间
	if token, ok := claims.(*StandardClaims); ok {
		if token.IssuedAt == 0 {
			token.IssuedAt = time.Now().Unix()
		}

		if token.ExpiredAt == 0 {
			token.ExpiredAt = time.Now().Add(own.expiration).Unix()
		}

		claims = token
	}

	// 构造 header
	header := map[string]any{
		"typ": "JWT",
		"alg": own.algorithm.GetAlgorithmHash().String(),
	}

	// 构造 payload
	headerBytes, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encodedHeader := base64.RawURLEncoding.EncodeToString(headerBytes)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	signingInput := encodedHeader + "." + encodedPayload

	// 构造 signature
	signature, err := own.algorithm.Encrypt([]byte(signingInput), own.slat)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s.%s", signingInput, base64.RawURLEncoding.EncodeToString(signature)), nil
}

// Validate 验证并解析JWT令牌
func (own *Manager) Validate(token string) (Claims, error) {
	// 检查黑名单
	// if m.blacklist.IsRevoked(token) {
	// 	return nil, ErrInvalidToken
	// }

	// 拆分令牌
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errInvalidToken
	}

	// 验证签名
	sign := parts[0] + "." + parts[1]
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, errInvalidSignature
	}
	if err := own.algorithm.Decrypt([]byte(sign), signature, own.slat); err != nil {
		return nil, err
	}

	// 解析Payload
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errInvalidToken
	}

	var claims StandardClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, errInvalidToken
	}

	// 验证Claims
	if err := claims.Validate(); err != nil {
		return nil, err
	}

	return &claims, nil
}

// // RefreshToken 自动刷新令牌
// func (m *JWTManager) RefreshToken(tokenString string) (string, error) {
// 	claims, err := m.Verify(tokenString)
// 	if err != nil {
// 		return "", err
// 	}

// 	// 将旧令牌加入黑名单
// 	m.blacklist.Revoke(tokenString, time.Unix(claims.ExpiresAt(), 0))

// 	// 生成新令牌
// 	newClaims := StandardClaims{
// 		ExpiresAt: time.Now().Add(m.expiration).Unix(),
// 		IssuedAt:  time.Now().Unix(),
// 		JWTID:     GenerateJWTID(),
// 	}

// 	return m.Generate(newClaims)
// }
