package authenticator

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/middleware/authenticator/jwt"
	"gitee.com/leminewx/leego/response"
)

var (
	errTokenStringEmpty       = errors.New("leego: jwt: token string is empty")
	errTokenStandardClaimsNil = errors.New("leego: jwt: standard claims of token is nil")
)

// Authenticator 定义 JWT 中间件的数据结构
type Authenticator struct {
	config           *Config                     // 配置
	signer           jwt.Signer                  // 签名器
	parser           jwt.Parser                  // 解析器
	header           *jwt.Header                 // JWT 头部
	extractors       []Extractor                 // 令牌提取器
	defaultExtractor Extractor                   // 令牌默认提取器
	errorHandler     func(*leego.Context, error) // 错误处理
	lock             sync.RWMutex
}

// NewHS256Authenticator 创建一个带默认配置的 使用 HS256 签名算法的 JWT 中间件
func NewHS256Authenticator(key []byte) *Authenticator {
	signer, _ := jwt.NewHMACSigner(jwt.NewHeader(jwt.HS256), key)
	config := NewDefaultConfig()
	config.Algorithm = jwt.HS256
	return NewAuthenticator(config, signer, jwt.NewParser())
}

// NewAuthenticator 创建一个 JWT 中间件（默认使用 Bearer 令牌提取器）
func NewAuthenticator(config *Config, signer jwt.Signer, parser jwt.Parser) *Authenticator {
	config.Algorithm = signer.GetAlgorithm()
	return &Authenticator{
		config: config,
		signer: signer,
		parser: parser,
		header: &jwt.Header{
			Type:      "JWT",
			Algorithm: signer.GetAlgorithm(),
		},
		extractors:       []Extractor{},
		defaultExtractor: NewBearerExtractor(),
	}
}

// WithErrorHandler 设置错误处理函数
func (own *Authenticator) WithErrorHandler(handler func(ctx *leego.Context, err error)) *Authenticator {
	if handler != nil {
		own.lock.Lock()
		own.errorHandler = handler
		own.lock.Unlock()
	}

	return own
}

// WithExtractors 设置令牌提取器（当所有令牌提取器都无效时，默认使用 Bearer 令牌提取器）
func (own *Authenticator) WithExtractors(extractors ...Extractor) *Authenticator {
	own.lock.Lock()
	for _, extractor := range extractors {
		if extractor != nil {
			own.extractors = append(own.extractors, extractor)
		}
	}
	own.lock.Unlock()

	return own
}

// GenerateToken 生成 JWT 令牌
func (own *Authenticator) GenerateToken(claims *jwt.StandardClaims) *jwt.Token {
	own.lock.RLock()
	defer own.lock.RUnlock()

	conf := own.config.JWTStandardClaimsConfig
	if conf == nil || (conf.ExceptedIssuer == "" && conf.ExceptedAudience == "" && conf.NotBeforeSeconds <= 0 && conf.ExpiresAfterSeconds <= 0) {
		return jwt.NewToken(claims)
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

	return jwt.NewToken(claims)
}

// GenerateTokenString 生成 JWT 令牌字符串
func (own *Authenticator) GenerateTokenString(claims *jwt.StandardClaims) (string, error) {
	tokenBytes, err := own.Sign(own.GenerateToken(claims))
	if err != nil {
		return "", err
	}

	return string(tokenBytes), nil
}

// Handle 创建一个处理函数
func (own *Authenticator) Handle(handle leego.HandleFunc) leego.HandleFunc {
	return func(ctx *leego.Context) {
		// 检查是否跳过当前路径
		if own.ShouldSkip(ctx.Request.URL.Path) {
			handle(ctx)
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
				ctx.ResponseError(http.StatusUnauthorized, response.STATUS_401_UNAUTHORIZED)
			}

			return
		}

		// 验证令牌
		var token *jwt.Token
		if atomic.LoadInt32(&own.config.Validation) > 0 { // 验证签名和标准声明
			token, err = own.VerifyWithStandardClaims(tokenString)
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
				ctx.ResponseError(http.StatusUnauthorized, response.STATUS_401_UNAUTHORIZED)
			}

			return
		}

		// 将令牌和声明存储在请求中，然后调用下一个处理器
		ctx.WithJWT(token)
		handle(ctx)
	}
}

// ShouldSkip 检查是否应该跳过当前路径
func (own *Authenticator) ShouldSkip(path string) bool {
	own.lock.RLock()
	defer own.lock.RUnlock()

	// 检查绝对路径
	if len(own.config.SkipAbsolutePathMap) > 0 {
		if _, ok := own.config.SkipAbsolutePathMap[path]; ok {
			return true
		}
	}

	// 检查通配路径
	for _, skipPath := range own.config.SkipWildcardPathSlice {
		if strings.HasPrefix(path, skipPath) {
			return true
		}
	}

	return false
}

// ExtractToken 提取取令牌
func (own *Authenticator) ExtractToken(req *http.Request) (string, error) {
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
func (own *Authenticator) Sign(token *jwt.Token) ([]byte, error) {
	return own.signer.Sign(token)
}

// Verify 验证签名
func (own *Authenticator) Verify(tokenString string) (*jwt.Token, error) {
	if len(tokenString) == 0 {
		return nil, errTokenStringEmpty
	}

	return own.parser.Parse(own.signer, tokenString)
}

// VerifyWithStandardClaims 验证签名并验证标准声明（签发时间允许1分钟时间偏差）
func (own *Authenticator) VerifyWithStandardClaims(tokenString string) (*jwt.Token, error) {
	if len(tokenString) == 0 {
		return nil, errTokenStringEmpty
	}

	conf := own.config.JWTStandardClaimsConfig
	if conf == nil {
		return nil, errTokenStandardClaimsNil
	}

	own.lock.RLock()
	exceptedIssuer := conf.ExceptedIssuer
	exceptedAudience := conf.ExceptedAudience
	own.lock.RUnlock()

	return own.parser.ParseAndValidateStandardClaims(own.signer, tokenString, exceptedIssuer, exceptedAudience)
}
