package authorizator

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/middleware/authorizator/jwt"
)

// Authorizator 定义 JWT 管理器的数据结构
type Authorizator struct {
	id    int
	group string // 作用路由组

	// 管理器组件
	signer jwt.Signer  // 签名器
	parser jwt.Parser  // 解析器
	header *jwt.Header // JWT头部

	// 基本配置
	skipPaths       []string         // 跳过的路径
	tokenExtractors []TokenExtractor // 令牌提取器

	// 标准声明的校验参数
	expectedIssuer         string        // 签名的签发方
	exceptedAudience       string        // 签名的接收方
	notBeforeDuration      time.Duration // 签名在什么时间之后开始生效
	expiresAfterDuration   time.Duration // 签名在生效多久之后过期
	validateStandardClaims bool          // 是否验证标准声明

	// 错误处理
	ErrorHandler func(*leego.Context, error)
}

// New 创建一个 JWT 管理器（默认使用 Bearer 令牌提取器）
func NewAuthorizator(signer jwt.Signer, parser jwt.Parser) *Authorizator {
	return &Authorizator{
		signer: signer,
		parser: parser,
		header: &jwt.Header{
			Type:      "JWT",
			Algorithm: signer.GetAlgorithm(),
		},
		skipPaths:       []string{},
		tokenExtractors: []TokenExtractor{NewBearerExtractor()},
	}
}

// WithGroup 指定中间件作用的路由组
func (own *Authorizator) WithGroup(group string) *Authorizator {
	own.group = group
	return own
}

// WithValidateStandardClaims 设置是否验证标准声明
func (own *Authorizator) WithValidateStandardClaims(validation bool) *Authorizator {
	own.validateStandardClaims = validation
	return own
}

// WithSkipPaths 设置跳过路径（白名单）
func (own *Authorizator) WithSkipPaths(paths ...string) *Authorizator {
	for _, path := range paths {
		if path = strings.TrimSpace(path); path != "" {
			own.skipPaths = append(own.skipPaths, path)
		}
	}

	return own
}

// WithTokenExtractors 设置令牌提取器（当所有令牌提取器都无效时，默认使用 Bearer 令牌提取器）
func (own *Authorizator) WithTokenExtractors(extractors ...TokenExtractor) *Authorizator {
	own.tokenExtractors = make([]TokenExtractor, 0, len(extractors))
	for _, extractor := range own.tokenExtractors {
		if extractor != nil {
			own.tokenExtractors = append(own.tokenExtractors, extractor)
		}
	}

	if len(own.tokenExtractors) == 0 {
		own.tokenExtractors = append(own.tokenExtractors, NewBearerExtractor())
	}

	return own
}

// WithExceptedIssuer 设置期望的签发者
func (own *Authorizator) WithExceptedIssuer(issuer string) *Authorizator {
	own.expectedIssuer = strings.TrimSpace(issuer)
	return own
}

// WithExceptedIssuer 设置期望的签发者
func (own *Authorizator) WithExceptedAudience(audience string) *Authorizator {
	own.exceptedAudience = strings.TrimSpace(audience)
	return own
}

// WithNotBeforeDuration 设置令牌在什么时间之后开始生效（签发时间之后）
func (own *Authorizator) WithNotBeforeDuration(duration time.Duration) *Authorizator {
	if duration > 0 {
		own.notBeforeDuration = duration
	}

	return own
}

// WithExpiresAfterDuration 设置令牌在生效多久之后过期
func (own *Authorizator) WithExpiresAfterDuration(duration time.Duration) *Authorizator {
	if duration > 0 {
		own.expiresAfterDuration = duration
	}

	return own
}

// GenerateToken 生成 JWT 令牌
func (own *Authorizator) GenerateToken(claims *jwt.StandardClaims) *jwt.Token {
	if claims == nil {
		if own.expectedIssuer == "" && own.exceptedAudience == "" && own.notBeforeDuration <= 0 && own.expiresAfterDuration <= 0 {
			return jwt.NewTokenWithHeader(nil, own.header)
		}

		claims = &jwt.StandardClaims{}
	}

	if claims.Issuer == "" && own.expectedIssuer != "" {
		claims.Issuer = own.expectedIssuer
	}

	if len(claims.Audiences) == 0 {
		claims.Audiences = []string{own.exceptedAudience}
	}

	now := time.Now()
	claims.IssuedAt = now.Unix()

	if claims.NotBefore == 0 && own.notBeforeDuration > 0 {
		claims.NotBefore = now.Add(own.notBeforeDuration).Unix()
	}

	if claims.ExpiresAt == 0 && own.expiresAfterDuration > 0 {
		claims.ExpiresAt = now.Add(own.notBeforeDuration + own.expiresAfterDuration).Unix()
	}

	return jwt.NewTokenWithHeader(claims, own.header)
}

// ShouldSkip 检查是否应该跳过当前路径
func (own *Authorizator) ShouldSkip(path string) bool {
	// 如果没有指定跳过路径，则不跳过
	if len(own.skipPaths) == 0 {
		return false
	}

	for _, skipPath := range own.skipPaths {
		// 完全匹配，直接跳过
		if path == skipPath {
			return true
		}

		// 支持通配符匹配
		if strings.HasSuffix(skipPath, "/*") {
			prefix := strings.TrimSuffix(skipPath, "/*")
			if strings.HasPrefix(path, prefix) {
				return true
			}
		}
	}

	return false
}

// ShouldValidateStandardClaims 检查是否应该验证标准声明
func (own *Authorizator) ShouldValidateStandardClaims() bool {
	return own.validateStandardClaims
}

// ExtractToken 提取取令牌
func (own *Authorizator) ExtractToken(req *http.Request) (string, error) {
	for _, extractor := range own.tokenExtractors {
		if extractor == nil {
			continue
		}

		if token, err := extractor.Extract(req); err == nil && token != "" {
			return token, nil
		}
	}

	return "", errTokenNotFoundInAnyExtrator
}

// Sign 生成签名
func (own *Authorizator) Sign(token *jwt.Token) (string, error) {
	if token == nil {
		return "", fmt.Errorf("leego: jwt: token is nil")
	}

	return token.Sign(own.signer)
}

// Verify 验证签名
func (own *Authorizator) Verify(token string) (*jwt.Token, error) {
	if len(token) == 0 {
		return nil, fmt.Errorf("leego: jwt: token is empty")
	}

	return own.parser.Parse(own.signer, token)
}

// VerifyWithStandardClaims 验证签名并验证标准声明（签发时间允许1分钟时间偏差）
func (own *Authorizator) VerifyWithStandardClaims(token string) (*jwt.Token, error) {
	if len(token) == 0 {
		return nil, fmt.Errorf("leego: jwt: token is empty")
	}

	return own.parser.ParseAndValidateStandardClaims(own.signer, token, own.expectedIssuer, own.exceptedAudience)
}
