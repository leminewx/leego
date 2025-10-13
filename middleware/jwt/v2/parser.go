package jwt

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Parser 定义用于解析和验证 JWT 的解析器的接口类型
type Parser interface {
	// Parse 解析 JWT
	Parse(signer Signer, tokenString string) (*Token, error)
	// ParseWithClaims 解析 JWT 并验证特定声明
	ParseWithClaims(signer Signer, tokenString string, expectedIssuer, expectedAudience string) (*Token, error)
}

// parser 定义用于解析和验证 JWT 的解析器的数据结构
type parser struct {
	ValidAlgorithms []Algorithm // 可接受的签名算法
}

/*
NewParser 新建一个 JWT 解析器
  - algorithms: 解析器可接受的签名算法
  - 默认支持的签名算法包含：HS256、HS384、HS512
*/
func NewParser(algorithms ...Algorithm) Parser {
	if len(algorithms) == 0 {
		return &parser{ValidAlgorithms: []Algorithm{HS256, HS384, HS512}}
	}

	return &parser{ValidAlgorithms: algorithms}
}

// Parse 解析 JWT 字符串
func (own *parser) Parse(signer Signer, tokenString string) (*Token, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("leego: jwt: token contains an invalid number of segments")
	}

	header := &Header{}

	// 解析头部
	headerJSON, err := DecodeBase64(parts[0])
	if err != nil {
		return nil, fmt.Errorf("leego: jwt: failed to decode header: %v", err)
	}

	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return nil, fmt.Errorf("leego: jwt: failed to unmarshal header: %v", err)
	}

	// 验证算法
	if !slices.Contains(own.ValidAlgorithms, header.Algorithm) {
		return nil, fmt.Errorf("leego: jwt: invalid signing algorithm: %s", header.Algorithm)
	}

	if header.Algorithm != signer.GetAlgorithm() {
		return nil, fmt.Errorf("leego: jwt: signing algorithm mismatch")
	}

	claims := make(map[string]any)

	// 解析声明
	claimsJSON, err := DecodeBase64(parts[1])
	if err != nil {
		return nil, fmt.Errorf("leego: jwt: failed to decode claims: %v", err)
	}

	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		return nil, fmt.Errorf("leego: jwt: failed to unmarshal claims: %v", err)
	}

	// 验证签名
	signature, err := DecodeBase64(parts[2])
	if err != nil {
		return nil, fmt.Errorf("leego: jwt: failed to decode signature: %v", err)
	}

	signingInput := []byte(parts[0] + "." + parts[1])
	if !signer.Verify(signingInput, signature) {
		return nil, fmt.Errorf("leego: jwt: failed to verify signature")
	}

	return &Token{
		source:    tokenString,
		header:    header,
		claims:    claims,
		stdClaims: extractStandardClaims(claims),
		signature: signature,
		isValid:   true,
	}, nil
}

// ParseWithClaims 解析 JWT 并验证特定声明
func (own *parser) ParseWithClaims(signer Signer, tokenString string, expectedIssuer, expectedAudience string) (*Token, error) {
	token, err := own.Parse(signer, tokenString)
	if err != nil {
		return nil, err
	}

	return token, token.ValidateStandardClaims(expectedIssuer, expectedAudience)
}

// extractStandardClaims 从 JWT 中提取标准声明
func extractStandardClaims(claims map[string]any) *StandardClaims {
	stdClaims := &StandardClaims{}
	for key, value := range claims {
		switch key {
		case "iat":
			stdClaims.IssuedAt = convertInt64(value)
		case "exp":
			stdClaims.ExpiresAt = convertInt64(value)
		case "nbf":
			stdClaims.NotBefore = convertInt64(value)
		case "jti":
			if jti, ok := value.(string); ok {
				stdClaims.Id = jti
			}
		case "iss":
			if iss, ok := value.(string); ok {
				stdClaims.Issuer = iss
			}
		case "sub":
			if sub, ok := value.(string); ok {
				stdClaims.Subject = sub
			}
		case "aud":
			switch aud := value.(type) {
			case string:
				stdClaims.Audiences = []string{aud}
			case []string:
				stdClaims.Audiences = aud
			case []any:
				stdClaims.Audiences = make([]string, 0, len(aud))
				for _, a := range aud {
					if v, ok := a.(string); ok {
						stdClaims.Audiences = append(stdClaims.Audiences, v)
					}
				}
			}
		}
	}

	return stdClaims
}

func convertInt64(value any) int64 {
	switch nbf := value.(type) {
	case int64:
		return nbf
	case float64:
		return int64(nbf)
	case json.Number:
		val, _ := nbf.Int64()
		return val
	default:
		return 0
	}
}
