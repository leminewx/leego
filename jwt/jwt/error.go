package jwt

import "errors"

var (
	errInvalidExp        = errors.New("jwt: invalid 'exp'")
	errInvalidJti        = errors.New("jwt: invalid 'jti'")
	errInvalidToken      = errors.New("jwt: invalid token")
	errInvalidIssuer     = errors.New("jwt: invalid issuer")
	errInvalidPayload    = errors.New("jwt: invalid payload")
	errTokenNotEffective = errors.New("jwt: not effective yet")
)

func IsInvalidError(err error) bool {
	if err == errInvalidExp || err == errInvalidJti || err == errInvalidToken || err == errInvalidIssuer || err == errInvalidPayload || err == errTokenNotEffective {
		return true
	}

	return false
}
