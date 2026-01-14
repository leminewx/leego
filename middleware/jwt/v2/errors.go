package jwt

import "errors"

var (
	// 令牌错误
	ErrInvalidToken      = errors.New("invalid token")
	ErrMalformedToken    = errors.New("malformed token")
	ErrUnsupportedMethod = errors.New("unsupported signing method")

	// 签名错误
	ErrInvalidSignature = errors.New("invalid signature")
	ErrMissingSignature = errors.New("missing signature")

	// 声明错误
	ErrExpiredToken    = errors.New("token has expired")
	ErrNotValidYet     = errors.New("token not valid yet")
	ErrInvalidIssuer   = errors.New("invalid issuer")
	ErrInvalidAudience = errors.New("invalid audience")
	ErrInvalidClaims   = errors.New("invalid claims")

	// 算法错误
	ErrInvalidAlgorithm  = errors.New("invalid algorithm")
	ErrAlgorithmMismatch = errors.New("algorithm mismatch")

	// 校验错误
	ErrValidationFailed = errors.New("validation failed")
)
