package upgrader

import (
	"errors"
	"fmt"
	"testing"
)

// TestCloseCodeValues 验证关闭码取值与 RFC 6455 / IANA 注册表一致。
func TestCloseCodeValues(t *testing.T) {
	cases := []struct {
		name string
		code CloseCode
		want uint16
	}{
		{name: "正常关闭", code: CLOSE_CODE_NORMAL_CLOSURE, want: 1000},
		{name: "端点离开", code: CLOSE_CODE_GOING_AWAY, want: 1001},
		{name: "协议错误", code: CLOSE_CODE_PROTOCOL_ERROR, want: 1002},
		{name: "不支持的数据", code: CLOSE_CODE_UNSUPPORTED_DATA, want: 1003},
		{name: "未收到状态码", code: CLOSE_CODE_NO_CODE_RECEIVED, want: 1005},
		{name: "异常关闭", code: CLOSE_CODE_ABNORMAL_CLOSURE, want: 1006},
		{name: "数据格式不一致", code: CLOSE_CODE_INVALID_FRAME_PAYLOAD_DATA, want: 1007},
		{name: "策略违规", code: CLOSE_CODE_POLICY_VIOLATION, want: 1008},
		{name: "消息过大", code: CLOSE_CODE_MESSAGE_TOO_BIG, want: 1009},
		{name: "缺少必要扩展", code: CLOSE_CODE_MANDATORY_EXTENSION, want: 1010},
		{name: "服务器内部错误", code: CLOSE_CODE_INTERNAL_SERVER_ERROR, want: 1011},
		{name: "服务器重启", code: CLOSE_CODE_SERVICE_RESTART, want: 1012},
		{name: "稍后再试", code: CLOSE_CODE_TRY_AGAIN_LATER, want: 1013},
		{name: "TLS 握手失败", code: CLOSE_CODE_TLS_HANDSHAKE, want: 1015},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if uint16(testCase.code) != testCase.want {
				t.Fatalf("code = %d, want %d", uint16(testCase.code), testCase.want)
			}
		})
	}
}

// TestCloseErrorMessage 验证 CloseError 的错误文本包含关闭码与原因。
func TestCloseErrorMessage(t *testing.T) {
	err := &CloseError{Code: CLOSE_CODE_GOING_AWAY, Text: "server restart"}

	message := err.Error()
	if message != "leego: websocket: close 1001 server restart" {
		t.Fatalf("Error() = %q", message)
	}
}

// TestIsCloseError 验证关闭错误的识别逻辑。
func TestIsCloseError(t *testing.T) {
	closeErr := &CloseError{Code: CLOSE_CODE_POLICY_VIOLATION, Text: "nope"}

	cases := []struct {
		name  string
		err   error
		codes []CloseCode
		want  bool
	}{
		{name: "nil 错误", err: nil, want: false},
		{name: "普通错误", err: errors.New("boom"), want: false},
		{name: "未指定关闭码", err: closeErr, want: true},
		{name: "关闭码匹配", err: closeErr, codes: []CloseCode{CLOSE_CODE_PROTOCOL_ERROR, CLOSE_CODE_POLICY_VIOLATION}, want: true},
		{name: "关闭码不匹配", err: closeErr, codes: []CloseCode{CLOSE_CODE_NORMAL_CLOSURE}, want: false},
		{name: "包装后的关闭错误", err: fmt.Errorf("wrapped: %w", closeErr), codes: []CloseCode{CLOSE_CODE_POLICY_VIOLATION}, want: true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := IsCloseError(testCase.err, testCase.codes...); got != testCase.want {
				t.Fatalf("IsCloseError() = %v, want %v", got, testCase.want)
			}
		})
	}
}

// TestIsCloseErrorMatchesAnyInstance 验证不依赖具体错误实例，只看类型与关闭码。
func TestIsCloseErrorMatchesAnyInstance(t *testing.T) {
	first := &CloseError{Code: CLOSE_CODE_NORMAL_CLOSURE}
	second := &CloseError{Code: CLOSE_CODE_NORMAL_CLOSURE}

	if !IsCloseError(first, CLOSE_CODE_NORMAL_CLOSURE) || !IsCloseError(second, CLOSE_CODE_NORMAL_CLOSURE) {
		t.Fatal("同码不同实例都应被识别")
	}
}
