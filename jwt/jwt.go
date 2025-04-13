package jwt

import (
	"encoding/base64"
	"encoding/json"
)

func encode(data any) ([]byte, error) {
	src, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	return []byte(base64.RawURLEncoding.EncodeToString(src)), nil
}
