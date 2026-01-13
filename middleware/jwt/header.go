package jwt

// Algorithm 定义 JWT 签名算法的数据类型
type Algorithm string

const (
	HS256 Algorithm = "HS256" // HMAC-SHA256
	HS384 Algorithm = "HS384" // HMAC-SHA384
	HS512 Algorithm = "HS512" // HMAC-SHA512
)

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
