package jwt

// Algorithm 定义 JWT 签名的算法类型
type Algorithm string

const (
	ALG_HS256 Algorithm = "HS256"
	ALG_HS384 Algorithm = "HS384"
	ALG_HS512 Algorithm = "HS512"
)
