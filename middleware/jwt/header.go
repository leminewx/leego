package jwt

// Algorithm 定义 JWT 签名算法的数据类型
type Algorithm string

// Header 定义 JWT 头部的数据结构
type Header struct {
	Type      string    `json:"typ"`
	Algorithm Algorithm `json:"alg"`
}

// NewHeader 新建一个签名头
func NewHeader(algorithm Algorithm) Header {
	return Header{
		Type:      "JWT",
		Algorithm: algorithm,
	}
}
