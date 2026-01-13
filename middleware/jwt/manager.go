package jwt

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/response"
)

var (
	errTokenStringEmpty       = errors.New("leego: jwt: token string is empty")
	errTokenStandardClaimsNil = errors.New("leego: jwt: standard claims of token is nil")
)

// Manager 定义 JWT 中间件管理器的实现类型
type Manager struct {
	config           *Config                     // 配置
	signer           Signer                      // 签名器
	parser           Parser                      // 解析器
	header           *Header                     // JWT 头部
	extractors       []Extractor                 // 令牌提取器
	defaultExtractor Extractor                   // 令牌默认提取器
	errorHandler     func(*leego.Context, error) // 错误处理
	lock             sync.RWMutex
}

var _ leego.Middleware = (*Manager)(nil)

/*
NewDefaultManager 创建一个默认配置的 JWT 令牌管理器
  - key: 密钥
  - 默认使用 HS256 签名算法
  - 默认启用 JWT 校验
  - 默认令牌 1 天有效
  - 默认通过 Authorization: Bearer 提取令牌信息
*/
func NewDefaultManager(key []byte) *Manager {
	signer, _ := NewHMACSigner(NewHeader(HS256), key)
	config := NewDefaultConfig()
	config.Algorithm = signer.GetAlgorithm()
	return NewManager(config, signer, NewParser())
}

// NewManager 创建一个 JWT 中间件（默认使用 Bearer 令牌提取器）
func NewManager(config *Config, signer Signer, parser Parser) *Manager {
	config.Algorithm = signer.GetAlgorithm()
	return &Manager{
		config: config,
		signer: signer,
		parser: parser,
		header: &Header{
			Type:      "JWT",
			Algorithm: signer.GetAlgorithm(),
		},
		extractors:       []Extractor{},
		defaultExtractor: NewBearerExtractor(),
	}
}

// SetExtractors 设置令牌提取器（当所有令牌提取器都无效时，默认使用 Bearer 令牌提取器）
func (own *Manager) SetExtractors(extractors ...Extractor) *Manager {
	own.lock.Lock()
	for _, extractor := range extractors {
		if extractor != nil {
			own.extractors = append(own.extractors, extractor)
		}
	}
	own.lock.Unlock()

	return own
}

// SetErrorHandler 设置错误处理函数
func (own *Manager) SetErrorHandler(handler func(ctx *leego.Context, err error)) {
	if handler != nil {
		own.lock.Lock()
		own.errorHandler = handler
		own.lock.Unlock()
	}
}

// GenerateToken 生成 JWT 令牌
func (own *Manager) GenerateToken(claims Claims) (string, error) {
	own.lock.RLock()
	defer own.lock.RUnlock()

	conf := own.config.StandardClaimsConfig
	if conf == nil || (conf.ExceptedIssuer == "" && conf.ExceptedAudience == "" && conf.NotBeforeSeconds <= 0 && conf.ExpiresAfterSeconds <= 0) {
		return NewToken(claims)
	}

	if claims == nil {
		claims = &StandardClaims{}
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

	return NewToken(claims)
}

// GenerateTokenString 生成 JWT 令牌字符串
func (own *Manager) GenerateToken(claims *StandardClaims) (string, error) {
	tokenBytes, err := own.Sign(own.GenerateToken(claims))
	if err != nil {
		return "", err
	}

	return string(tokenBytes), nil
}

// Handle 创建一个处理函数
func (own *Manager) Handle(next leego.HandleFunc) leego.HandleFunc {
	return func(ctx *leego.Context) {
		// 未启用，直接跳过
		if atomic.LoadInt32(&own.config.IsEnabled) == 0 {
			next(ctx)
			return
		}

		// 检查是否跳过当前路径
		if own.ShouldSkip(ctx.Request.URL.Path) {
			next(ctx)
			return
		}

		// 提取令牌
		tokenString, err := own.ExtractToken(ctx.Request)
		if err != nil {
			// 处理错误若存在，则处理错误
			if own.errorHandler != nil {
				own.errorHandler(ctx, err)
			}

			// 未指定状态码，需额外处理响应
			if ctx.GetStatusCode() == 0 {
				ctx.ResponseError(http.StatusUnauthorized, response.GetStatusLowerText(http.StatusUnauthorized))
			}

			return
		}

		// 验证令牌
		var token *Token
		if atomic.LoadInt32(&own.config.Validation) > 0 { // 验证签名和标准声明
			token, err = own.VerifySetStandardClaims(tokenString)
		} else { // 仅验证签名
			token, err = own.Verify(tokenString)
		}

		// 认证失败
		if err != nil {
			// 处理错误若存在，则处理错误
			if own.errorHandler != nil {
				own.errorHandler(ctx, err)
			}

			// 未指定状态码，需额外处理响应
			if ctx.GetStatusCode() == 0 {
				ctx.ResponseError(http.StatusUnauthorized, response.GetStatusLowerText(http.StatusUnauthorized))
			}

			return
		}

		// 将令牌和声明存储在请求中，然后调用下一个处理器
		ctx.SetJWTClaims(token.GetAllClaims())
		next(ctx)
	}
}

// ShouldSkip 检查是否应该跳过当前路径
func (own *Manager) ShouldSkip(path string) bool {
	own.lock.RLock()
	defer own.lock.RUnlock()

	// 检查绝对路径
	if len(own.config.SkipAbsolutePaths) > 0 {
		if _, ok := own.config.SkipAbsolutePaths[path]; ok {
			return true
		}
	}

	// 检查通配路径
	for _, skipPath := range own.config.SkipWildcardPaths {
		if strings.HasPrefix(path, skipPath) {
			return true
		}
	}

	return false
}

// ExtractToken 提取取令牌
func (own *Manager) ExtractToken(req *http.Request) (string, error) {
	// 未指定令牌提取器，则使用默认令牌提取器
	if len(own.extractors) == 0 {
		return own.defaultExtractor.Extract(req)
	}

	// 尝试从当前提取器中提取令牌
	for _, extractor := range own.extractors {
		if token, err := extractor.Extract(req); err == nil && token != "" {
			return token, nil
		}
	}

	return "", errTokenNotFoundInAnyExtractor
}

// Sign 生成令牌签名
func (own *Manager) Sign(token *Token) ([]byte, error) {
	return own.signer.Sign(token)
}

// Verify 验证签名
func (own *Manager) Verify(tokenString string) (*Token, error) {
	if len(tokenString) == 0 {
		return nil, errTokenStringEmpty
	}

	return own.parser.Parse(own.signer, tokenString)
}

// VerifySetStandardClaims 验证签名并验证标准声明（签发时间允许1分钟时间偏差）
func (own *Manager) VerifySetStandardClaims(tokenString string) (*Token, error) {
	if len(tokenString) == 0 {
		return nil, errTokenStringEmpty
	}

	conf := own.config.StandardClaimsConfig
	if conf == nil {
		return nil, errTokenStandardClaimsNil
	}

	own.lock.RLock()
	exceptedIssuer := conf.ExceptedIssuer
	exceptedAudience := conf.ExceptedAudience
	own.lock.RUnlock()

	return own.parser.ParseAndValidateStandardClaims(own.signer, tokenString, exceptedIssuer, exceptedAudience)
}
