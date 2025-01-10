package jwt

import "testing"

func TestError(t *testing.T) {
	t.Log(IsInvalidError(errInvalidExp))
	t.Log(IsInvalidError(errInvalidIssuer))
	t.Log(IsInvalidError(errInvalidJti))
	t.Log(IsInvalidError(errInvalidPayload))
	t.Log(IsInvalidError(errInvalidToken))
	t.Log(IsInvalidError(errTokenNotEffective))
	t.Log(IsInvalidError(errHashWriteLengthNotEqual))
}
