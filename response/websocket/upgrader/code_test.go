package upgrader

import "testing"

// TestOpCodeValues 验证操作码取值与 RFC 6455 一致。
//
// 这些数值直接决定线上帧的解析结果，一旦被改动会造成协议不兼容，
// 因此用显式断言锁定。
func TestOpCodeValues(t *testing.T) {
	cases := []struct {
		name   string
		opCode OpCode
		want   byte
	}{
		{name: "连续帧", opCode: OP_CODE_CONTINUE, want: 0x0},
		{name: "文本帧", opCode: OP_CODE_TEXT, want: 0x1},
		{name: "二进制帧", opCode: OP_CODE_BINARY, want: 0x2},
		{name: "关闭帧", opCode: OP_CODE_CLOSE, want: 0x8},
		{name: "PING", opCode: OP_CODE_PING, want: 0x9},
		{name: "PONG", opCode: OP_CODE_PONG, want: 0xA},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if byte(testCase.opCode) != testCase.want {
				t.Fatalf("opCode = %#x, want %#x", byte(testCase.opCode), testCase.want)
			}
		})
	}
}

// TestControlOpCodes 验证哪些操作码属于控制帧。
func TestControlOpCodes(t *testing.T) {
	cases := []struct {
		opCode    OpCode
		isControl bool
	}{
		{opCode: OP_CODE_CONTINUE, isControl: false},
		{opCode: OP_CODE_TEXT, isControl: false},
		{opCode: OP_CODE_BINARY, isControl: false},
		{opCode: OP_CODE_CLOSE, isControl: true},
		{opCode: OP_CODE_PING, isControl: true},
		{opCode: OP_CODE_PONG, isControl: true},
	}

	for _, testCase := range cases {
		got := testCase.opCode&0x08 != 0
		if got != testCase.isControl {
			t.Fatalf("%#x 控制帧判定 = %v, want %v", byte(testCase.opCode), got, testCase.isControl)
		}
	}
}
