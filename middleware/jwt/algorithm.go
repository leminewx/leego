package jwt

// Algorithm 定义 JWT 签名算法的数据类型
type Algorithm string

const (
	HS256 Algorithm = "HS256" // HMAC-SHA256
	HS384 Algorithm = "HS384" // HMAC-SHA384
	HS512 Algorithm = "HS512" // HMAC-SHA512
)
