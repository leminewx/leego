package trace

import "testing"

// TestParseTraceparentRejectsMalformed 验证严格按 W3C 规范拒绝非法取值。
func TestParseTraceparentRejectsMalformed(t *testing.T) {
	cases := map[string]string{
		"空串":           "",
		"段数不足":         "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7",
		"版本不符":         "01-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		"TraceID 全零":   "00-00000000000000000000000000000000-00f067aa0ba902b7-01",
		"SpanID 全零":    "00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000000-01",
		"大写十六进制":       "00-4BF92F3577B34DA6A3CE929D0E0E4736-00f067aa0ba902b7-01",
		"TraceID 长度错误": "00-4bf92f3577b34da6a3ce929d0e0e47-00f067aa0ba902b7-01",
		"标志位非十六进制":     "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-zz",
		"多余段":          "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01-extra",
	}

	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, _, ok := ParseTraceparent(value); ok {
				t.Fatalf("ParseTraceparent(%q) 应返回 ok=false", value)
			}
		})
	}
}

// TestParseTraceparentReadsIdentityAndFlags 验证 ID 与采样标志位解析。
func TestParseTraceparentReadsIdentityAndFlags(t *testing.T) {
	traceID, spanID, sampled, ok := ParseTraceparent("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00")
	if !ok || traceID != "4bf92f3577b34da6a3ce929d0e0e4736" || spanID != "00f067aa0ba902b7" {
		t.Fatalf("解析结果异常: %q %q %v", traceID, spanID, ok)
	}

	if sampled {
		t.Fatal("标志位 00 应为未采样")
	}
}

// TestFormatTraceparentRejectsInvalidIDs 验证非法 ID 不会被拼进请求头。
func TestFormatTraceparentRejectsInvalidIDs(t *testing.T) {
	invalid := [][2]string{
		{"", "00f067aa0ba902b7"},
		{"4bf92f3577b34da6a3ce929d0e0e473", "00f067aa0ba902b7"},
		{"4bf92f3577b34da6a3ce929d0e0e4736", "00f067aa0ba902b"},
		{"4BF92F3577B34DA6A3CE929D0E0E4736", "00f067aa0ba902b7"},
		{"zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz", "00f067aa0ba902b7"},
	}

	for _, item := range invalid {
		if got := FormatTraceparent(item[0], item[1], true); got != "" {
			t.Fatalf("FormatTraceparent(%q, %q) = %q, want 空串", item[0], item[1], got)
		}
	}
}

// TestHexHelpers 验证十六进制判定工具。
func TestHexHelpers(t *testing.T) {
	if !isLowerHex("0123456789abcdef", 16) {
		t.Fatal("合法小写十六进制被拒绝")
	}

	for _, value := range []string{"0123456789ABCDEF", "0123456789abcdeg", "0123456789abcde"} {
		if isLowerHex(value, 16) {
			t.Fatalf("isLowerHex(%q) 应返回 false", value)
		}
	}

	if !isZeroHex("0000") {
		t.Fatal("全零串应被识别")
	}

	if isZeroHex("0001") {
		t.Fatal("含非零字符的串不应被识别为全零")
	}
}
