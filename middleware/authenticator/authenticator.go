package authenticator

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/middleware/authenticator/jwt"
)

// Authenticator 定义 JWT 认证管理器的数据结构
type Authenticator struct {
	// 配置
	*Config

	// 管理器组件
	signer     jwt.Signer  // 签名器
	parser     jwt.Parser  // 解析器
	header     *jwt.Header // JWT头部
	extractors []Extractor // 令牌提取器

	// 错误处理
	ErrorHandler func(*leego.Context, error)
}

// NewAuthenticator 创建一个 JWT 管理器（默认使用 Bearer 令牌提取器）
func NewAuthenticator(config *Config, signer jwt.Signer, parser jwt.Parser) *Authenticator {
	return &Authenticator{
		Config: config,
		signer: signer,
		parser: parser,
		header: &jwt.Header{
			Type:      "JWT",
			Algorithm: signer.GetAlgorithm(),
		},
		extractors: []Extractor{NewBearerExtractor()},
	}
}

// WithExtractors 设置令牌提取器（当所有令牌提取器都无效时，默认使用 Bearer 令牌提取器）
func (own *Authenticator) WithExtractors(extractors ...Extractor) *Authenticator {
	own.extractors = make([]Extractor, 0, len(extractors))
	for _, extractor := range own.extractors {
		if extractor != nil {
			own.extractors = append(own.extractors, extractor)
		}
	}

	if len(own.extractors) == 0 {
		own.extractors = append(own.extractors, NewBearerExtractor())
	}

	return own
}

// GenerateToken 生成 JWT 令牌
func (own *Authenticator) GenerateToken(claims *jwt.StandardClaims) *jwt.Token {
	own.lock.RLock()
	defer own.lock.RUnlock()

	conf := own.JWTStandardClaimsConfig
	if conf == nil || (conf.ExceptedIssuer == "" && conf.ExceptedAudience == "" && conf.NotBeforeSeconds <= 0 && conf.ExpiresAfterSeconds <= 0) {
		return jwt.NewTokenWithHeader(claims, own.header)
	}

	if claims == nil {
		claims = &jwt.StandardClaims{}
	}

	if claims.Issuer == "" && conf.ExceptedIssuer != "" {
		claims.Issuer = conf.ExceptedIssuer
	}

	if len(claims.Audiences) == 0 {
		claims.Audiences = []string{conf.ExceptedAudience}
	}

	now := time.Now()
	claims.IssuedAt = now.Unix()

	if claims.NotBefore == 0 && conf.NotBeforeSeconds > 0 {
		claims.NotBefore = now.Add(time.Duration(conf.NotBeforeSeconds) * time.Second).Unix()
	}

	if claims.ExpiresAt == 0 && conf.ExpiresAfterSeconds > 0 {
		claims.ExpiresAt = now.Add(time.Duration(conf.NotBeforeSeconds+conf.ExpiresAfterSeconds) * time.Second).Unix()
	}

	return jwt.NewTokenWithHeader(claims, own.header)
}

// ShouldSkip 检查是否应该跳过当前路径
func (own *Authenticator) ShouldSkip(path string) bool {
	own.lock.RLock()
	defer own.lock.RUnlock()

	// 检查绝对路径
	if len(own.SkipAbsolutePathMap) > 0 {
		if _, ok := own.SkipAbsolutePathMap[path]; ok {
			return true
		}
	}

	// 检查通配路径
	for _, skipPath := range own.SkipWildcardPathSlice {
		if strings.HasPrefix(path, skipPath) {
			return true
		}
	}

	return false
}

// ShouldValidateStandardClaims 检查是否应该验证标准声明
func (own *Authenticator) ShouldValidateStandardClaims() bool {
	return atomic.LoadInt32(&own.Validation) == 1
}

// ExtractToken 提取取令牌
func (own *Authenticator) ExtractToken(req *http.Request) (string, error) {
	for _, extractor := range own.extractors {
		if extractor == nil {
			continue
		}

		if token, err := extractor.Extract(req); err == nil && token != "" {
			return token, nil
		}
	}

	return "", errTokenNotFoundInAnyExtractor
}

// Sign 生成签名
func (own *Authenticator) Sign(token *jwt.Token) (string, error) {
	if token == nil {
		return "", fmt.Errorf("leego: jwt: token is nil")
	}

	return token.Sign(own.signer)
}

// Verify 验证签名
func (own *Authenticator) Verify(token string) (*jwt.Token, error) {
	if len(token) == 0 {
		return nil, fmt.Errorf("leego: jwt: token is empty")
	}

	return own.parser.Parse(own.signer, token)
}

// VerifyWithStandardClaims 验证签名并验证标准声明（签发时间允许1分钟时间偏差）
func (own *Authenticator) VerifyWithStandardClaims(token string) (*jwt.Token, error) {
	if len(token) == 0 {
		return nil, fmt.Errorf("leego: jwt: token is empty")
	}

	if own.JWTStandardClaimsConfig == nil {
		return nil, fmt.Errorf("leego: jwt: standard claims of token is nil")
	}

	own.lock.RLock()
	exceptedIssuer := own.JWTStandardClaimsConfig.ExceptedIssuer
	exceptedAudience := own.JWTStandardClaimsConfig.ExceptedAudience
	own.lock.RUnlock()

	return own.parser.ParseAndValidateStandardClaims(own.signer, token, exceptedIssuer, exceptedAudience)
}
