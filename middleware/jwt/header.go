package jwt

import (
	"encoding/base64"
	"encoding/json"
)

// Algorithm 定义 JWT 签名算法的数据类型
type Algorithm string

type Header interface {
	// GetAlgorithm 获取算法
	GetAlgorithm() Algorithm
	// GetEncodedString 获取编码字符串
	GetEncodedString() string
}

// Header 定义 JWT 头部的数据结构
type header struct {
	algorithm     Algorithm
	encodedString string
}

// NewHeader 新建一个签名头
func NewHeader(algorithm Algorithm) Header {
	h := &header{algorithm: algorithm}
	// 序列化
	headerBytes, _ := json.Marshal(map[string]any{"typ": "JWT", "alg": algorithm})
	// Base64 编码
	h.encodedString = base64.RawURLEncoding.EncodeToString(headerBytes)
	return h
}

// GetAlgorithm 获取算法
func (own *header) GetAlgorithm() Algorithm {
	return own.algorithm
}

// GetEncodedString 获取编码字符串
func (own *header) GetEncodedString() string {
	return own.encodedString
}
