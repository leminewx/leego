package jwt

import (
	"fmt"
	"time"
)

type Options struct {
	Issuer       string        `json:"issuer" xml:"issuer"`
	Secret       []byte        `json:"secret" xml:"secret"`
	Audiences    []string      `json:"audiences" xml:"audiences"`
	Algorithm    Algorithm     `json:"algorithm" xml:"algorithm"`
	ValidAfter   time.Duration `json:"valid_after" xml:"valid_after"`
	ExpiredAfter time.Duration `json:"expired_after" xml:"expired_after"`
}

// Validate 校验必要配置参数
func (own *Options) Validate() error {
	if len(own.Secret) == 0 {
		return fmt.Errorf("leego: jwt: 'secret' is empty")
	}

	if own.Algorithm == "" {
		return fmt.Errorf("leego: jwt: 'algorithm' is empty")
	}

	if own.ValidAfter <= 0 {
		return fmt.Errorf("leego: jwt: 'valid_after' is not greater than 0")
	}

	if own.ExpiredAfter <= 0 {
		return fmt.Errorf("leego: jwt: 'expired_after' is not greater than 0")
	}

	return nil
}

// Manager 定义 JWT 管理器的数据结构
type Manager struct {
	signer       Signer   // 签名器
	parser       Parser   // 解析器
	header       *Header  // JWT头部
	config       *Options // 配置文件
	useStdClaims bool     // 使用标准声明
}

// NewManager 新建一个 JWT 管理器
func NewManager(signer Signer, parser Parser, opt *Options) *Manager {
	return &Manager{
		signer: signer,
		parser: parser,
		header: &Header{
			Type:      "JWT",
			Algorithm: signer.GetAlgorithm(),
		},
		config: opt,
	}
}

// GenerateToken 生成 JWT 令牌
func (own *Manager) GenerateToken(claims *StandardClaims) *Token {
	if claims == nil {
		return &Token{header: own.header}
	}

	if claims.Issuer == "" && own.config.Issuer != "" {
		claims.Issuer = own.config.Issuer
	}

	if len(claims.Audiences) == 0 {
		claims.Audiences = own.config.Audiences
	}

	now := time.Now()
	claims.IssuedAt = now.Unix()

	if claims.NotBefore == 0 && own.config.ValidAfter > 0 {
		claims.NotBefore = now.Add(own.config.ValidAfter * time.Second).Unix()
	}

	if claims.ExpiresAt == 0 && own.config.ExpiredAfter > 0 {
		claims.ExpiresAt = now.Add(own.config.ExpiredAfter * time.Second).Unix()
	}

	return &Token{
		header:    own.header,
		stdClaims: claims,
	}
}

func (own *Manager) Sign(token *Token) (string, error) {
	if token == nil {
		return "", fmt.Errorf("leego: jwt: token is nil")
	}

	return token.Sign(own.signer)
}

func (own *Manager) Verify(token string) (*Token, error) {
	if len(token) == 0 {
		return nil, fmt.Errorf("leego: jwt: token is empty")
	}

	return own.parser.Parse(own.signer, token)
}

func (own *Manager) VerifyWithStandardClaims(token, exceptedIssuer, exceptedAudience string) (*Token, error) {
	if len(token) == 0 {
		return nil, fmt.Errorf("leego: jwt: token is empty")
	}

	return own.parser.ParseAndValidateStandardClaims(own.signer, token, exceptedIssuer, exceptedAudience)
}
