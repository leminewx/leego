package jwt

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
)

var errHashWriteLengthNotEqual = errors.New("jwt: the length of the data used for generating sha256 signature is not equal to the length of the data")

func GenerateSHA256Signature(salt []byte, data []byte) (string, error) {
	hash := hmac.New(sha256.New, salt)
	if num, err := hash.Write(data); err != nil {
		return "", err
	} else if num != len(data) {
		return "", errHashWriteLengthNotEqual
	}

	return base64.RawURLEncoding.EncodeToString(hash.Sum(nil)), nil
}
