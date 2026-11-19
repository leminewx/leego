// Package jwt 提供最小可用、语义清晰的 JWT 签发与校验能力。
//
// 设计边界：
//   - 只内置 HMAC（HS256/HS384/HS512）签名，避免非对称算法带来的密钥管理复杂度；
//   - 头部算法必须与签名器一致，杜绝 alg 混淆与 "none" 攻击；
//   - 声明反序列化前强制校验目标为非 nil 指针，避免静默退化为 map；
//   - 签名比较使用 hmac.Equal，具备常数时间特性。
package jwt

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"time"
)

// _DEFAULT_CLOCK_SKEW 定义默认允许的时钟偏差。
//
// 分布式部署中各节点时钟存在小幅漂移，完全严格的时间校验会造成
// “刚签发即过期”的误判，因此默认容忍 1 分钟偏差。
const _DEFAULT_CLOCK_SKEW = time.Minute

// Parser 定义 JWT 解析与校验接口。
type Parser interface {
	// Parse 解析并验证签名，但不校验声明中的时间字段与签发者。
	Parse(signer Signer, tokenString string, claims Claims) error
	// ParseAndValidateStandardClaims 解析并验证时间字段（容忍时钟偏差）。
	ParseAndValidateStandardClaims(signer Signer, tokenString string, claims Claims) error
	// ParseAndValidateStandardClaimsWithIssuerAndAudiences 解析并验证时间字段、签发者与受众。
	ParseAndValidateStandardClaimsWithIssuerAndAudiences(signer Signer, tokenString, expectedIssuer, expectedAudience string, claims Claims) error
	// SetClockSkew 设置允许的时钟偏差。
	SetClockSkew(skew time.Duration)
	// GetClockSkew 返回当前允许的时钟偏差。
	GetClockSkew() time.Duration
}

// parser 是 Parser 的默认实现。
//
// parser 不含共享可变状态：时钟偏差由 atomic.Int64 保存，
// 因此 GetClockSkew / SetClockSkew 与解析调用可以并发执行。
type parser struct {
	// clockSkew 为允许的时钟偏差（纳秒），使用原子读写以避免配置变更与解析竞争。
	clockSkew atomic.Int64
}

// 编译期断言：*parser 必须实现 Parser。
var _ Parser = (*parser)(nil)

// rawHeader 定义 JWT 头部的反序列化结构。
//
// 使用具体结构体而非接口，避免 json.Unmarshal 到 nil 接口时必然失败。
type rawHeader struct {
	// Algorithm 为签名算法。
	Algorithm Algorithm `json:"alg"`
	// Type 为令牌类型，通常为 "JWT"。
	Type string `json:"typ"`
}

// NewParser 创建 JWT 解析器。
func NewParser() Parser {
	instance := &parser{}
	instance.clockSkew.Store(int64(_DEFAULT_CLOCK_SKEW))
	return instance
}

// SetClockSkew 设置允许的时钟偏差；负值会被修正为 0。
func (own *parser) SetClockSkew(skew time.Duration) {
	if skew < 0 {
		skew = 0
	}

	own.clockSkew.Store(int64(skew))
}

// GetClockSkew 返回当前允许的时钟偏差。
func (own *parser) GetClockSkew() time.Duration {
	return time.Duration(own.clockSkew.Load())
}

// Parse 解析 JWT 并验证签名。
//
// 校验顺序（先结构、后签名、最后声明）：
//  1. 令牌必须由三段组成；
//  2. 头部必须是合法 JSON，且 alg 与签名器一致；
//  3. 载荷必须是合法 Base64URL 与合法 JSON；
//  4. 签名必须匹配；
//  5. 声明必须能反序列化到调用方提供的非 nil 指针。
//
// 该顺序保证结构错误的令牌返回语义明确的错误，而不是统一报“签名无效”。
func (own *parser) Parse(signer Signer, tokenString string, claims Claims) error {
	if signer == nil {
		return errors.New("jwt: nil signer")
	}

	if claims == nil {
		return ErrNilClaims
	}

	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return ErrInvalidToken
	}

	if parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return ErrMalformedToken
	}

	// 1. 解析头部并校验算法。
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return ErrInvalidHeader
	}

	var head rawHeader
	if err = json.Unmarshal(headerBytes, &head); err != nil {
		return ErrInvalidHeader
	}

	if head.Algorithm == "" {
		return ErrInvalidAlgorithm
	}

	if head.Algorithm != signer.GetHeader().GetAlgorithm() {
		return ErrAlgorithmMismatch
	}

	if head.Type != "" && !strings.EqualFold(head.Type, "JWT") {
		return ErrInvalidType
	}

	// 2. 解析载荷，确保结构合法。
	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ErrInvalidPayload
	}

	// 3. 校验签名。
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return ErrInvalidSignature
	}

	if !signer.Verify([]byte(parts[0]+"."+parts[1]), signature) {
		return ErrInvalidSignature
	}

	// 4. 声明必须是非 nil 指针，否则 json.Unmarshal 会返回歧义错误。
	if err = validateClaimsTarget(claims); err != nil {
		return err
	}

	if err = json.Unmarshal(claimsJSON, claims); err != nil {
		return ErrInvalidPayload
	}

	return nil
}

// validateClaimsTarget 校验声明目标必须是非 nil 指针。
func validateClaimsTarget(claims Claims) error {
	if claims == nil {
		return ErrNilClaims
	}

	value := reflect.ValueOf(claims)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return ErrNilClaims
	}

	return nil
}

// ParseAndValidateStandardClaims 解析令牌并校验标准声明中的时间字段。
func (own *parser) ParseAndValidateStandardClaims(signer Signer, tokenString string, claims Claims) error {
	if err := own.Parse(signer, tokenString, claims); err != nil {
		return err
	}

	return validateClaims(claims, own.GetClockSkew())
}

// ParseAndValidateStandardClaimsWithIssuerAndAudiences 解析令牌并校验时间字段、签发者与受众。
func (own *parser) ParseAndValidateStandardClaimsWithIssuerAndAudiences(signer Signer, tokenString, expectedIssuer, expectedAudience string, claims Claims) error {
	if err := own.Parse(signer, tokenString, claims); err != nil {
		return err
	}

	if err := validateClaims(claims, own.GetClockSkew()); err != nil {
		return err
	}

	return validateClaimsIssuerAndAudiences(claims, expectedIssuer, expectedAudience, own.GetClockSkew())
}

// validateClaimsIssuerAndAudiences 校验签发者与受众，并在声明实现支持时携带时钟偏差。
//
// 声明实现（如 StandardClaims）会在同一次调用中重新校验时间字段，因此必须把
// 解析器的时钟偏差一并传入；否则会出现「时间校验已按偏差通过、签发者校验却因
// 过期而失败」的不一致。
func validateClaimsIssuerAndAudiences(claims Claims, expectedIssuer, expectedAudience string, clockSkew time.Duration) error {
	if validator, ok := claims.(interface {
		ValidateWithIssuerAndAudiencesAndLeeway(expectedIssuer, expectedAudience string, leeway time.Duration) error
	}); ok {
		return validator.ValidateWithIssuerAndAudiencesAndLeeway(expectedIssuer, expectedAudience, clockSkew)
	}

	return claims.ValidateWithIssuerAndAudiences(expectedIssuer, expectedAudience)
}

// validateClaims 校验声明的时间字段，并在支持时应用时钟偏差。
func validateClaims(claims Claims, clockSkew time.Duration) error {
	if leeway, ok := claims.(interface{ ValidateWithLeeway(time.Duration) error }); ok {
		return leeway.ValidateWithLeeway(clockSkew)
	}

	return claims.Validate()
}
