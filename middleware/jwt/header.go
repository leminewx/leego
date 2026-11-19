package jwt

import (
	"encoding/base64"
	"encoding/json"
)

// Algorithm 定义 JWT 签名算法的数据类型。
type Algorithm string

const (
	// HS256 表示 HMAC-SHA256 签名算法。
	HS256 Algorithm = "HS256"
	// HS384 表示 HMAC-SHA384 签名算法。
	HS384 Algorithm = "HS384"
	// HS512 表示 HMAC-SHA512 签名算法。
	HS512 Algorithm = "HS512"
)

// Header 定义 JWT 头部必须提供的能力。
//
// 头部在创建时完成序列化与 Base64URL 编码，运行期只读。
type Header interface {
	// GetAlgorithm 返回签名算法。
	GetAlgorithm() Algorithm
	// GetEncodedString 返回 Base64URL 编码后的头部字符串。
	GetEncodedString() string
}

// header 是 Header 的默认实现。
type header struct {
	// algorithm 为签名算法。
	algorithm Algorithm
	// encodedString 为编码后的头部字符串。
	encodedString string
}

// 编译期断言：*header 必须实现 Header。
var _ Header = (*header)(nil)

// NewHeader 创建指定算法的 JWT 头部。
//
// 头部固定包含 typ=JWT 与 alg=<algorithm>；字段顺序由结构体保证，
// 因此同一算法生成的编码字符串稳定可比较。
func NewHeader(algorithm Algorithm) Header {
	head := &header{algorithm: algorithm}
	encoded, err := json.Marshal(struct {
		Type      string    `json:"typ"`
		Algorithm Algorithm `json:"alg"`
	}{
		Type:      "JWT",
		Algorithm: algorithm,
	})
	if err != nil {
		// Algorithm 为字符串别名，编码不可能失败；保留兜底避免静默产出空头部。
		encoded = []byte(`{"typ":"JWT","alg":""}`)
	}

	head.encodedString = base64.RawURLEncoding.EncodeToString(encoded)
	return head
}

// GetAlgorithm 返回签名算法。
func (own *header) GetAlgorithm() Algorithm {
	return own.algorithm
}

// GetEncodedString 返回 Base64URL 编码后的头部字符串。
func (own *header) GetEncodedString() string {
	return own.encodedString
}
