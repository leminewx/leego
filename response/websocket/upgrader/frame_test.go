package upgrader

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

// TestReadFrameParsesLengthEncodings 验证 7 位、16 位与 64 位长度编码。
func TestReadFrameParsesLengthEncodings(t *testing.T) {
	cases := []struct {
		name string
		size int
	}{
		{name: "空负载", size: 0},
		{name: "7 位长度上界", size: 125},
		{name: "16 位长度下界", size: 126},
		{name: "16 位长度上界", size: 65535},
		{name: "64 位长度", size: 65536},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			payload := bytes.Repeat([]byte{0x5A}, testCase.size)
			instance := createFrameConn(encodeFrame(frameSpec{
				fin:     true,
				masked:  true,
				opCode:  OP_CODE_BINARY,
				payload: payload,
			}))

			parsed, err := instance.readFrame()
			if err != nil {
				t.Fatalf("解析帧失败: %v", err)
			}

			if !parsed.fin || !parsed.masked || parsed.opCode != OP_CODE_BINARY {
				t.Fatalf("帧头 = (fin=%v, masked=%v, opCode=%#x)", parsed.fin, parsed.masked, byte(parsed.opCode))
			}

			if !bytes.Equal(parsed.payload, payload) {
				t.Fatalf("负载长度 = %d, want %d", len(parsed.payload), len(payload))
			}
		})
	}
}

// TestReadFrameDecodesMask 验证掩码被正确还原。
func TestReadFrameDecodesMask(t *testing.T) {
	instance := createFrameConn(encodeFrame(frameSpec{
		fin:     true,
		masked:  true,
		opCode:  OP_CODE_TEXT,
		payload: []byte("masked payload"),
	}))

	parsed, err := instance.readFrame()
	if err != nil {
		t.Fatalf("解析帧失败: %v", err)
	}

	if string(parsed.payload) != "masked payload" {
		t.Fatalf("payload = %q, want %q", parsed.payload, "masked payload")
	}
}

// TestReadFrameRejectsReservedBits 验证未协商扩展时 RSV 必须为 0。
func TestReadFrameRejectsReservedBits(t *testing.T) {
	instance := createFrameConn(encodeFrame(frameSpec{
		fin:     true,
		rsv:     0x40,
		masked:  true,
		opCode:  OP_CODE_TEXT,
		payload: []byte("x"),
	}))

	_, err := instance.readFrame()
	if err == nil || !strings.Contains(err.Error(), "reserved bits") {
		t.Fatalf("err = %v, want reserved bits 协议错误", err)
	}
}

// TestReadFrameRejectsUnmaskedClientFrame 验证客户端帧必须携带掩码。
func TestReadFrameRejectsUnmaskedClientFrame(t *testing.T) {
	instance := createFrameConn(encodeFrame(frameSpec{
		fin:     true,
		masked:  false,
		opCode:  OP_CODE_TEXT,
		payload: []byte("x"),
	}))

	_, err := instance.readFrame()
	if err == nil || !strings.Contains(err.Error(), "must be masked") {
		t.Fatalf("err = %v, want must be masked", err)
	}
}

// TestReadFrameRejectsMaskedServerFrame 验证客户端角色拒绝带掩码的服务端帧。
func TestReadFrameRejectsMaskedServerFrame(t *testing.T) {
	instance := &Conn{
		reader: bufio.NewReader(bytes.NewReader(encodeFrame(frameSpec{
			fin:     true,
			masked:  true,
			opCode:  OP_CODE_TEXT,
			payload: []byte("x"),
		}))),
		isServer: false,
	}

	_, err := instance.readFrame()
	if err == nil || !strings.Contains(err.Error(), "must not be masked") {
		t.Fatalf("err = %v, want must not be masked", err)
	}
}

// TestReadFrameRejectsInvalid64BitLength 验证 64 位长度的最高位必须为 0。
func TestReadFrameRejectsInvalid64BitLength(t *testing.T) {
	encoded := []byte{0x81, 0x80 | 127}
	encoded = binary.BigEndian.AppendUint64(encoded, 1<<63)

	instance := createFrameConn(encoded)

	_, err := instance.readFrame()
	if err == nil || !strings.Contains(err.Error(), "invalid payload length") {
		t.Fatalf("err = %v, want invalid payload length", err)
	}
}

// TestReadFrameRejectsOversizedControlFrame 验证控制帧负载不得超过 125 字节。
func TestReadFrameRejectsOversizedControlFrame(t *testing.T) {
	instance := createFrameConn(encodeFrame(frameSpec{
		fin:     true,
		masked:  true,
		opCode:  OP_CODE_PING,
		payload: bytes.Repeat([]byte{0x01}, _MAX_CONTROL_PAYLOAD_LENGTH+1),
	}))

	_, err := instance.readFrame()
	if err == nil || !strings.Contains(err.Error(), "control frame payload exceeds") {
		t.Fatalf("err = %v, want control frame payload exceeds", err)
	}
}

// TestReadFrameRejectsFragmentedControlFrame 验证控制帧不允许分片。
func TestReadFrameRejectsFragmentedControlFrame(t *testing.T) {
	instance := createFrameConn(encodeFrame(frameSpec{
		fin:     false,
		masked:  true,
		opCode:  OP_CODE_PING,
		payload: []byte("x"),
	}))

	_, err := instance.readFrame()
	if err == nil || !strings.Contains(err.Error(), "must not be fragmented") {
		t.Fatalf("err = %v, want must not be fragmented", err)
	}
}

// TestReadFrameEnforcesMaxReadLimit 验证超过上限的消息被拒绝。
func TestReadFrameEnforcesMaxReadLimit(t *testing.T) {
	instance := createFrameConn(encodeFrame(frameSpec{
		fin:     true,
		masked:  true,
		opCode:  OP_CODE_BINARY,
		payload: bytes.Repeat([]byte{0x01}, 32),
	}))
	instance.SetMaxReadLimit(16)

	_, err := instance.readFrame()
	if err == nil || !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("err = %v, want exceeds limit", err)
	}
}

// TestReadFrameFallsBackToDefaultLimit 验证未设置上限时使用默认 16 MiB。
func TestReadFrameFallsBackToDefaultLimit(t *testing.T) {
	instance := createFrameConn(encodeFrame(frameSpec{
		fin:     true,
		masked:  true,
		opCode:  OP_CODE_BINARY,
		payload: bytes.Repeat([]byte{0x01}, 1024),
	}))

	parsed, err := instance.readFrame()
	if err != nil {
		t.Fatalf("默认上限下解析失败: %v", err)
	}

	if len(parsed.payload) != 1024 {
		t.Fatalf("负载长度 = %d, want 1024", len(parsed.payload))
	}
}

// TestReadFramePropagatesTruncatedPayload 验证负载被截断时返回错误。
func TestReadFramePropagatesTruncatedPayload(t *testing.T) {
	encoded := encodeFrame(frameSpec{
		fin:     true,
		masked:  true,
		opCode:  OP_CODE_TEXT,
		payload: []byte("hello"),
	})

	instance := createFrameConn(encoded[:len(encoded)-2])

	if _, err := instance.readFrame(); err == nil {
		t.Fatal("负载被截断时必须返回错误")
	}
}

// TestReadFramePropagatesTruncatedHeader 验证首字节缺失时返回错误。
func TestReadFramePropagatesTruncatedHeader(t *testing.T) {
	instance := createFrameConn(nil)

	if _, err := instance.readFrame(); err == nil {
		t.Fatal("空输入必须返回错误")
	}
}
