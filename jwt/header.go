package jwt

import (
	"encoding/base64"
	"encoding/json"
)

type Type string

const (
	TYPE_JWT Type = "JWT"
)

type Algorithm string

const (
	ALG_HS256 Algorithm = "HS256"
)

type Header struct {
	typ  Type
	alg  Algorithm
	sign string
}

func NewHeader(typ Type, alg Algorithm) *Header {
	return &Header{
		typ: typ,
		alg: alg,
	}
}

func (own *Header) GetType() Type { return own.typ }

func (own *Header) GetAlgorithm() Algorithm { return own.alg }

func (own *Header) Encode() (string, error) {
	if own.sign != "" {
		return own.sign, nil
	}

	data, err := json.Marshal(map[string]any{"typ": own.typ, "alg": own.alg})
	if err != nil {
		return "", err
	}

	own.sign = base64.RawURLEncoding.EncodeToString(data)
	return own.sign, nil
}
