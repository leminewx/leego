package jwt

// Token 定义一个 JWT 令牌的数据结构
type Token struct {
	Pass      bool           // 是否验证通过
	Source    string         // 原始令牌字符串
	Header    map[string]any // 头部信息
	Claims    map[string]any // 声明信息
	Signature []byte         // 签名
}

// NewToken 创建新的 JWT
func NewToken(claims map[string]any) *Token {
	return &Token{
		Header: map[string]any{
			"typ": "JWT",
			"alg": ALG_HS256, // 默认算法
		},
		Claims: claims,
	}
}

// SetAlgorithm 设置签名算法
func (t *Token) SetAlgorithm(alg Algorithm) {
	t.Header["alg"] = alg
}
