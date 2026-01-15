package jwt

import "errors"

var (
	// 令牌错误
	ErrInvalidToken      = errors.New("jwt: invalid token")
	ErrMalformedToken    = errors.New("jwt: malformed token")
	ErrUnsupportedMethod = errors.New("jwt: unsupported signing method")

	// 签名错误
	ErrInvalidSignature = errors.New("jwt: invalid signature")
	ErrMissingSignature = errors.New("jwt: missing signature")

	// 声明错误
	ErrNilClaims           = errors.New("jwt: nil claims")
	ErrInvalidIssuer       = errors.New("jwt: invalid issuer")
	ErrInvalidSubject      = errors.New("jwt: invalid subject")
	ErrInvalidAudience     = errors.New("jwt: invalid audience")
	ErrExpiredToken        = errors.New("jwt: token has expired")
	ErrNotValidYet         = errors.New("jwt: token not valid yet")
	ErrInvalidIssuanceTime = errors.New("jwt: invalid issunace time")

	// 算法错误
	ErrInvalidType       = errors.New("jwt: invalid type")
	ErrInvalidHeader     = errors.New("jwt: invalid header")
	ErrInvalidAlgorithm  = errors.New("jwt: invalid algorithm")
	ErrAlgorithmMismatch = errors.New("jwt: algorithm mismatch")

	// 负载错误
	ErrInvalidPayload = errors.New("jwt: invalid payload")

	// 校验错误
	ErrValidationFailed = errors.New("jwt: validation failed")
)
