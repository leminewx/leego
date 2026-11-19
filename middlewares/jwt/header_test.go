package jwt

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

// TestNewHeaderEncodesTypeAndAlgorithm 验证头部同时包含 typ 与 alg。
func TestNewHeaderEncodesTypeAndAlgorithm(t *testing.T) {
	for _, algorithm := range []Algorithm{HS256, HS384, HS512} {
		head := NewHeader(algorithm)
		if head.GetAlgorithm() != algorithm {
			t.Fatalf("GetAlgorithm() = %q, want %q", head.GetAlgorithm(), algorithm)
		}

		raw, err := base64.RawURLEncoding.DecodeString(head.GetEncodedString())
		if err != nil {
			t.Fatalf("%s: 头部不是合法 Base64URL: %v", algorithm, err)
		}

		var decoded struct {
			Type      string    `json:"typ"`
			Algorithm Algorithm `json:"alg"`
		}
		if err = json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("%s: 头部不是合法 JSON: %v", algorithm, err)
		}

		if decoded.Type != "JWT" {
			t.Fatalf("%s: typ = %q, want JWT", algorithm, decoded.Type)
		}
		if decoded.Algorithm != algorithm {
			t.Fatalf("%s: alg = %q", algorithm, decoded.Algorithm)
		}
	}
}

// TestNewHeaderProducesStableEncoding 验证同一算法生成的头部字符串稳定，
// 这是签名结果可复现、可跨进程比对的前提。
func TestNewHeaderProducesStableEncoding(t *testing.T) {
	first := NewHeader(HS256).GetEncodedString()
	for i := 0; i < 100; i++ {
		if next := NewHeader(HS256).GetEncodedString(); next != first {
			t.Fatalf("第 %d 次生成的头部不一致: %q != %q", i, next, first)
		}
	}
}

// TestNewHeaderUsesRawURLEncoding 验证头部使用无填充的 Base64URL 编码，
// 避免出现 JWT 规范不允许的 "=" 填充字符。
func TestNewHeaderUsesRawURLEncoding(t *testing.T) {
	encoded := NewHeader(HS512).GetEncodedString()
	if encoded == "" {
		t.Fatal("头部编码结果为空")
	}

	for _, char := range encoded {
		if char == '=' || char == '+' || char == '/' {
			t.Fatalf("头部包含非 RawURL 字符 %q: %s", char, encoded)
		}
	}
}

// TestNewHeaderAcceptsEmptyAlgorithm 验证空算法会被原样编码，
// 由解析器在比对阶段返回 ErrInvalidAlgorithm，而不是在创建阶段 panic。
func TestNewHeaderAcceptsEmptyAlgorithm(t *testing.T) {
	head := NewHeader("")
	if head.GetAlgorithm() != "" {
		t.Fatalf("GetAlgorithm() = %q, want empty", head.GetAlgorithm())
	}

	raw, err := base64.RawURLEncoding.DecodeString(head.GetEncodedString())
	if err != nil {
		t.Fatalf("头部不是合法 Base64URL: %v", err)
	}

	var decoded struct {
		Algorithm Algorithm `json:"alg"`
	}
	if err = json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("头部不是合法 JSON: %v", err)
	}
	if decoded.Algorithm != "" {
		t.Fatalf("alg = %q, want empty", decoded.Algorithm)
	}
}

// BenchmarkNewHeader 测量头部创建的耗时与内存分配。
func BenchmarkNewHeader(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = NewHeader(HS256)
	}
}
