package jwt

import (
	"encoding/json"
	"fmt"
)

// Header 定义 JWT 头部的数据结构
type Header struct {
	Type      string    `json:"typ"`
	Algorithm Algorithm `json:"alg"`
}

// Encode 编码
func (own *Header) Encode() (string, error) {
	bytes, err := json.Marshal(own)
	if err != nil {
		return "", fmt.Errorf("leego: jwt: failed to marshal header: %v", err)
	}

	return EncodeBase64(bytes), nil
}
