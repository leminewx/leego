package upgrader

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// fakeAddr 是一个占位的网络地址实现，仅用于满足 net.Conn 接口。
type fakeAddr struct{}

// Network 返回固定的网络名。
func (own fakeAddr) Network() string { return "fake" }

// String 返回固定的地址字符串。
func (own fakeAddr) String() string { return "fake" }

/*
fakeConn 是 net.Conn 的内存实现。

写入的数据累积到 written，读取的数据来自 input；
它让帧编解码测试完全脱离真实网络，从而做到确定、无阻塞。
*/
type fakeConn struct {
	// written 保存所有已写入的字节。
	written bytes.Buffer
	// input 为预置的读取内容，可为 nil（此时读到 EOF）。
	input *bytes.Reader
	// isClosed 表示连接是否已关闭。
	isClosed bool
}

// Read 从预置内容中读取数据。
func (own *fakeConn) Read(buffer []byte) (int, error) {
	if own.input == nil {
		return 0, io.EOF
	}

	return own.input.Read(buffer)
}

// Write 把数据追加到写入缓冲区。
func (own *fakeConn) Write(data []byte) (int, error) {
	return own.written.Write(data)
}

// Close 标记连接已关闭。
func (own *fakeConn) Close() error {
	own.isClosed = true
	return nil
}

// LocalAddr 返回占位地址。
func (own *fakeConn) LocalAddr() net.Addr { return fakeAddr{} }

// RemoteAddr 返回占位地址。
func (own *fakeConn) RemoteAddr() net.Addr { return fakeAddr{} }

// SetDeadline 忽略截止时间设置。
func (own *fakeConn) SetDeadline(time.Time) error { return nil }

// SetReadDeadline 忽略读截止时间设置。
func (own *fakeConn) SetReadDeadline(time.Time) error { return nil }

// SetWriteDeadline 忽略写截止时间设置。
func (own *fakeConn) SetWriteDeadline(time.Time) error { return nil }

// frameSpec 描述一个待编码的帧，便于构造合法与非法帧。
type frameSpec struct {
	// fin 表示是否为消息最后一帧。
	fin bool
	// rsv 为保留位（正常必须为 0）。
	rsv byte
	// masked 表示是否携带掩码。
	masked bool
	// opCode 为操作码。
	opCode OpCode
	// payload 为负载数据。
	payload []byte
}

// testMaskKey 是测试用固定掩码密钥，便于断言与复现。
var testMaskKey = [4]byte{0x11, 0x22, 0x33, 0x44}

// encodeFrame 按 RFC 6455 把 frameSpec 编码为字节序列。
func encodeFrame(spec frameSpec) []byte {
	first := byte(spec.opCode) | spec.rsv
	if spec.fin {
		first |= 0x80
	}

	encoded := []byte{first}
	length := len(spec.payload)

	lengthByte := byte(0)
	if spec.masked {
		lengthByte = 0x80
	}

	switch {
	case length <= 125:
		encoded = append(encoded, lengthByte|byte(length))

	case length <= 65535:
		encoded = append(encoded, lengthByte|126)
		encoded = binary.BigEndian.AppendUint16(encoded, uint16(length))

	default:
		encoded = append(encoded, lengthByte|127)
		encoded = binary.BigEndian.AppendUint64(encoded, uint64(length))
	}

	if !spec.masked {
		return append(encoded, spec.payload...)
	}

	encoded = append(encoded, testMaskKey[:]...)
	for index, value := range spec.payload {
		encoded = append(encoded, value^testMaskKey[index%4])
	}

	return encoded
}

// createFrameConn 构造一个只读的 Conn，用于直接测试 readFrame。
func createFrameConn(data []byte) *Conn {
	return &Conn{
		reader:   bufio.NewReader(bytes.NewReader(data)),
		isServer: true,
	}
}

// createReadConn 构造一个服务端 Conn，预置待读取的帧序列。
func createReadConn(frames ...[]byte) (*Conn, *fakeConn) {
	raw := &fakeConn{}
	input := bytes.Join(frames, nil)

	return &Conn{
		conn:     raw,
		reader:   bufio.NewReader(bytes.NewReader(input)),
		writer:   bufio.NewWriter(raw),
		isServer: true,
	}, raw
}

// createWriteConn 构造一个服务端 Conn，写入内容会累积到返回的 fakeConn。
func createWriteConn() (*Conn, *fakeConn) {
	raw := &fakeConn{}

	return &Conn{
		conn:     raw,
		reader:   bufio.NewReader(raw),
		writer:   bufio.NewWriter(raw),
		isServer: true,
	}, raw
}

// parseServerFrames 解析服务端写出的（不带掩码的）帧序列。
func parseServerFrames(t *testing.T, data []byte) []*frame {
	t.Helper()

	reader := bufio.NewReader(bytes.NewReader(data))
	frames := make([]*frame, 0, 4)

	for {
		first, err := reader.ReadByte()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			t.Fatalf("读取首字节失败: %v", err)
		}

		second, err := reader.ReadByte()
		if err != nil {
			t.Fatalf("读取次字节失败: %v", err)
		}

		length := int(second & 0x7F)
		switch length {
		case 126:
			var length16 uint16
			if err := binary.Read(reader, binary.BigEndian, &length16); err != nil {
				t.Fatalf("读取 16 位长度失败: %v", err)
			}

			length = int(length16)

		case 127:
			var length64 uint64
			if err := binary.Read(reader, binary.BigEndian, &length64); err != nil {
				t.Fatalf("读取 64 位长度失败: %v", err)
			}

			length = int(length64)
		}

		if second&0x80 != 0 {
			t.Fatal("服务端帧不应携带掩码")
		}

		payload := make([]byte, length)
		if _, err := io.ReadFull(reader, payload); err != nil {
			t.Fatalf("读取负载失败: %v", err)
		}

		frames = append(frames, &frame{
			fin:     first&0x80 != 0,
			opCode:  OpCode(first & 0x0F),
			masked:  false,
			payload: payload,
		})
	}

	return frames
}

// parseClosePayload 解析服务端写出的关闭帧，返回关闭码与原因。
func parseClosePayload(t *testing.T, data []byte) (CloseCode, string) {
	t.Helper()

	frames := parseServerFrames(t, data)
	if len(frames) != 1 {
		t.Fatalf("关闭帧数量 = %d, want 1", len(frames))
	}

	if frames[0].opCode != OP_CODE_CLOSE {
		t.Fatalf("操作码 = %#x, want OP_CODE_CLOSE", byte(frames[0].opCode))
	}

	if len(frames[0].payload) < 2 {
		t.Fatalf("关闭帧负载长度 = %d, want >= 2", len(frames[0].payload))
	}

	return CloseCode(binary.BigEndian.Uint16(frames[0].payload)), string(frames[0].payload[2:])
}

// TestConnReadTextAndBinary 验证文本帧与二进制帧的读取。
func TestConnReadTextAndBinary(t *testing.T) {
	instance, _ := createReadConn(
		encodeFrame(frameSpec{fin: true, masked: true, opCode: OP_CODE_TEXT, payload: []byte("hello")}),
		encodeFrame(frameSpec{fin: true, masked: true, opCode: OP_CODE_BINARY, payload: []byte{0x00, 0xFF}}),
	)

	opCode, payload, err := instance.Read()
	if err != nil {
		t.Fatalf("读取文本帧失败: %v", err)
	}

	if opCode != OP_CODE_TEXT || string(payload) != "hello" {
		t.Fatalf("文本帧 = (%#x, %q), want (OP_CODE_TEXT, hello)", byte(opCode), payload)
	}

	opCode, payload, err = instance.Read()
	if err != nil {
		t.Fatalf("读取二进制帧失败: %v", err)
	}

	if opCode != OP_CODE_BINARY || !bytes.Equal(payload, []byte{0x00, 0xFF}) {
		t.Fatalf("二进制帧 = (%#x, %v)", byte(opCode), payload)
	}
}

// TestConnReadIgnoresEmptyFrame 验证空负载文本帧可以正常读取。
func TestConnReadIgnoresEmptyFrame(t *testing.T) {
	instance, _ := createReadConn(
		encodeFrame(frameSpec{fin: true, masked: true, opCode: OP_CODE_TEXT}),
	)

	opCode, payload, err := instance.Read()
	if err != nil {
		t.Fatalf("读取空帧失败: %v", err)
	}

	if opCode != OP_CODE_TEXT || len(payload) != 0 {
		t.Fatalf("空帧 = (%#x, %q)", byte(opCode), payload)
	}
}

// TestConnReadAutoRepliesPongToPing 验证收到 PING 时自动回复相同负载的 PONG。
func TestConnReadAutoRepliesPongToPing(t *testing.T) {
	instance, raw := createReadConn(
		encodeFrame(frameSpec{fin: true, masked: true, opCode: OP_CODE_PING, payload: []byte("hi")}),
		encodeFrame(frameSpec{fin: true, masked: true, opCode: OP_CODE_TEXT, payload: []byte("after")}),
	)

	opCode, payload, err := instance.Read()
	if err != nil {
		t.Fatalf("PING 后读取文本失败: %v", err)
	}

	if opCode != OP_CODE_TEXT || string(payload) != "after" {
		t.Fatalf("业务帧 = (%#x, %q), want (OP_CODE_TEXT, after)", byte(opCode), payload)
	}

	frames := parseServerFrames(t, raw.written.Bytes())
	if len(frames) != 1 {
		t.Fatalf("PONG 数量 = %d, want 1", len(frames))
	}

	if frames[0].opCode != OP_CODE_PONG || string(frames[0].payload) != "hi" {
		t.Fatalf("PONG = (%#x, %q), want (OP_CODE_PONG, hi)", byte(frames[0].opCode), frames[0].payload)
	}
}

// TestConnReadIgnoresPong 验证主动收到的 PONG 被忽略且不写回。
func TestConnReadIgnoresPong(t *testing.T) {
	instance, raw := createReadConn(
		encodeFrame(frameSpec{fin: true, masked: true, opCode: OP_CODE_PONG, payload: []byte("x")}),
		encodeFrame(frameSpec{fin: true, masked: true, opCode: OP_CODE_TEXT, payload: []byte("ok")}),
	)

	opCode, payload, err := instance.Read()
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}

	if opCode != OP_CODE_TEXT || string(payload) != "ok" {
		t.Fatalf("业务帧 = (%#x, %q)", byte(opCode), payload)
	}

	if raw.written.Len() != 0 {
		t.Fatalf("PONG 不应触发写回，实际写入 %d 字节", raw.written.Len())
	}
}

// TestConnReadCloseHandshake 验证收到关闭帧时回写并返回 CloseError。
func TestConnReadCloseHandshake(t *testing.T) {
	payload := binary.BigEndian.AppendUint16(nil, uint16(CLOSE_CODE_NORMAL_CLOSURE))
	payload = append(payload, []byte("bye")...)

	instance, raw := createReadConn(
		encodeFrame(frameSpec{fin: true, masked: true, opCode: OP_CODE_CLOSE, payload: payload}),
	)

	_, _, err := instance.Read()

	var closeErr *CloseError
	if !errors.As(err, &closeErr) {
		t.Fatalf("err = %v, want *CloseError", err)
	}

	if closeErr.Code != CLOSE_CODE_NORMAL_CLOSURE || closeErr.Text != "bye" {
		t.Fatalf("CloseError = (%d, %q)", closeErr.Code, closeErr.Text)
	}

	code, text := parseClosePayload(t, raw.written.Bytes())
	if code != CLOSE_CODE_NORMAL_CLOSURE || text != "bye" {
		t.Fatalf("回写关闭帧 = (%d, %q), want (1000, bye)", code, text)
	}

	if !instance.IsClosed() {
		t.Fatal("处理关闭帧后连接应标记为已关闭")
	}
}

// TestConnReadCloseWithoutPayload 验证空关闭帧返回 1005。
func TestConnReadCloseWithoutPayload(t *testing.T) {
	instance, raw := createReadConn(
		encodeFrame(frameSpec{fin: true, masked: true, opCode: OP_CODE_CLOSE}),
	)

	_, _, err := instance.Read()

	var closeErr *CloseError
	if !errors.As(err, &closeErr) {
		t.Fatalf("err = %v, want *CloseError", err)
	}

	if closeErr.Code != CLOSE_CODE_NO_CODE_RECEIVED {
		t.Fatalf("CloseError.Code = %d, want %d", closeErr.Code, CLOSE_CODE_NO_CODE_RECEIVED)
	}

	code, _ := parseClosePayload(t, raw.written.Bytes())
	if code != CLOSE_CODE_NORMAL_CLOSURE {
		t.Fatalf("回写关闭码 = %d, want %d", code, CLOSE_CODE_NORMAL_CLOSURE)
	}
}

// TestConnReadCloseRejectsInvalidUTF8 验证关闭原因非法 UTF-8 时按协议错误处理。
func TestConnReadCloseRejectsInvalidUTF8(t *testing.T) {
	payload := binary.BigEndian.AppendUint16(nil, uint16(CLOSE_CODE_NORMAL_CLOSURE))
	payload = append(payload, 0xFF)

	instance, raw := createReadConn(
		encodeFrame(frameSpec{fin: true, masked: true, opCode: OP_CODE_CLOSE, payload: payload}),
	)

	_, _, err := instance.Read()
	if err == nil {
		t.Fatal("非法 UTF-8 的关闭原因必须返回错误")
	}

	var closeErr *CloseError
	if errors.As(err, &closeErr) {
		t.Fatalf("应返回协议错误而不是 CloseError，实际 = %v", closeErr)
	}

	code, _ := parseClosePayload(t, raw.written.Bytes())
	if code != _CLOSE_CODE_INVALID_PAYLOAD {
		t.Fatalf("回写关闭码 = %d, want %d", code, _CLOSE_CODE_INVALID_PAYLOAD)
	}
}

// TestConnReadRejectsFragmentedMessages 验证分片消息被拒绝。
func TestConnReadRejectsFragmentedMessages(t *testing.T) {
	cases := []struct {
		name   string
		spec   frameSpec
		reason CloseCode
	}{
		{
			name:   "文本分片",
			spec:   frameSpec{fin: false, masked: true, opCode: OP_CODE_TEXT, payload: []byte("part")},
			reason: CLOSE_CODE_UNSUPPORTED_DATA,
		},
		{
			name:   "二进制分片",
			spec:   frameSpec{fin: false, masked: true, opCode: OP_CODE_BINARY, payload: []byte{0x01}},
			reason: CLOSE_CODE_UNSUPPORTED_DATA,
		},
		{
			name:   "连续帧",
			spec:   frameSpec{fin: true, masked: true, opCode: OP_CODE_CONTINUE, payload: []byte("part")},
			reason: CLOSE_CODE_UNSUPPORTED_DATA,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			instance, raw := createReadConn(encodeFrame(testCase.spec))

			_, _, err := instance.Read()
			if err == nil {
				t.Fatal("分片消息必须返回错误")
			}

			code, _ := parseClosePayload(t, raw.written.Bytes())
			if code != testCase.reason {
				t.Fatalf("回写关闭码 = %d, want %d", code, testCase.reason)
			}
		})
	}
}

// TestConnReadRejectsInvalidUTF8Text 验证文本帧必须是合法 UTF-8。
func TestConnReadRejectsInvalidUTF8Text(t *testing.T) {
	instance, raw := createReadConn(
		encodeFrame(frameSpec{fin: true, masked: true, opCode: OP_CODE_TEXT, payload: []byte{0xFF, 0xFE}}),
	)

	_, _, err := instance.Read()
	if err == nil {
		t.Fatal("非法 UTF-8 文本帧必须返回错误")
	}

	code, _ := parseClosePayload(t, raw.written.Bytes())
	if code != _CLOSE_CODE_INVALID_PAYLOAD {
		t.Fatalf("回写关闭码 = %d, want %d", code, _CLOSE_CODE_INVALID_PAYLOAD)
	}
}

// TestConnReadRejectsUnknownOpcode 验证未知操作码按协议错误关闭。
func TestConnReadRejectsUnknownOpcode(t *testing.T) {
	instance, raw := createReadConn(
		encodeFrame(frameSpec{fin: true, masked: true, opCode: OpCode(0x03), payload: []byte("x")}),
	)

	_, _, err := instance.Read()
	if err == nil {
		t.Fatal("未知操作码必须返回错误")
	}

	code, _ := parseClosePayload(t, raw.written.Bytes())
	if code != CLOSE_CODE_PROTOCOL_ERROR {
		t.Fatalf("回写关闭码 = %d, want %d", code, CLOSE_CODE_PROTOCOL_ERROR)
	}
}

// TestConnReadPropagatesFrameError 验证底层帧错误直接返回且不写关闭帧。
func TestConnReadPropagatesFrameError(t *testing.T) {
	instance, raw := createReadConn(
		encodeFrame(frameSpec{fin: true, masked: false, opCode: OP_CODE_TEXT, payload: []byte("x")}),
	)

	_, _, err := instance.Read()
	if err == nil {
		t.Fatal("未掩码的客户端帧必须被拒绝")
	}

	if raw.written.Len() != 0 {
		t.Fatalf("帧解析失败时不应写关闭帧，实际写入 %d 字节", raw.written.Len())
	}
}

// TestConnWriteEncodesPayloadLength 验证三种长度编码边界。
func TestConnWriteEncodesPayloadLength(t *testing.T) {
	cases := []struct {
		name string
		size int
	}{
		{name: "空负载", size: 0},
		{name: "短负载", size: 125},
		{name: "16 位长度下界", size: 126},
		{name: "16 位长度上界", size: 65535},
		{name: "64 位长度", size: 65536},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			instance, raw := createWriteConn()
			payload := bytes.Repeat([]byte{0xAB}, testCase.size)

			if err := instance.Write(OP_CODE_BINARY, payload); err != nil {
				t.Fatalf("写入失败: %v", err)
			}

			frames := parseServerFrames(t, raw.written.Bytes())
			if len(frames) != 1 {
				t.Fatalf("帧数量 = %d, want 1", len(frames))
			}

			if !frames[0].fin || frames[0].opCode != OP_CODE_BINARY {
				t.Fatalf("帧头 = (fin=%v, opCode=%#x)", frames[0].fin, byte(frames[0].opCode))
			}

			if !bytes.Equal(frames[0].payload, payload) {
				t.Fatalf("负载长度 = %d, want %d", len(frames[0].payload), len(payload))
			}
		})
	}
}

// TestConnWriteRejectsCloseOpcode 验证业务写入不能直接发送关闭帧。
func TestConnWriteRejectsCloseOpcode(t *testing.T) {
	instance, _ := createWriteConn()

	if err := instance.Write(OP_CODE_CLOSE, nil); err == nil {
		t.Fatal("Write 不应允许 OP_CODE_CLOSE")
	}
}

// TestConnWriteRejectsInvalidUTF8Text 验证文本帧写入前的 UTF-8 校验。
func TestConnWriteRejectsInvalidUTF8Text(t *testing.T) {
	instance, raw := createWriteConn()

	if err := instance.Write(OP_CODE_TEXT, []byte{0xFF}); err == nil {
		t.Fatal("非法 UTF-8 文本必须被拒绝")
	}

	if raw.written.Len() != 0 {
		t.Fatalf("校验失败时不应写入任何字节，实际 %d", raw.written.Len())
	}
}

// TestConnWriteControlRejectsInvalidOpcode 验证 WriteControl 只接受 PING/PONG。
func TestConnWriteControlRejectsInvalidOpcode(t *testing.T) {
	instance, _ := createWriteConn()

	if err := instance.WriteControl(OP_CODE_TEXT, nil); err == nil {
		t.Fatal("WriteControl 不应接受文本帧")
	}
}

// TestConnWriteControlRejectsOversizedPayload 验证控制帧负载上限 125 字节。
func TestConnWriteControlRejectsOversizedPayload(t *testing.T) {
	instance, raw := createWriteConn()

	if err := instance.WriteControl(OP_CODE_PING, bytes.Repeat([]byte{0x01}, 126)); err == nil {
		t.Fatal("超过 125 字节的控制帧必须被拒绝")
	}

	if raw.written.Len() != 0 {
		t.Fatalf("校验失败时不应写入任何字节，实际 %d", raw.written.Len())
	}
}

// TestConnWriteAfterClose 验证关闭后写入返回 ErrConnectionIsClosed。
func TestConnWriteAfterClose(t *testing.T) {
	instance, _ := createWriteConn()

	if err := instance.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}

	if !instance.IsClosed() {
		t.Fatal("关闭后 IsClosed 应为 true")
	}

	if err := instance.Write(OP_CODE_TEXT, []byte("x")); !errors.Is(err, ErrConnectionIsClosed) {
		t.Fatalf("err = %v, want ErrConnectionIsClosed", err)
	}

	if err := instance.WriteControl(OP_CODE_PING, nil); !errors.Is(err, ErrConnectionIsClosed) {
		t.Fatalf("控制帧 err = %v, want ErrConnectionIsClosed", err)
	}
}

// TestConnCloseIsIdempotent 验证重复关闭不会重复写关闭帧。
func TestConnCloseIsIdempotent(t *testing.T) {
	instance, raw := createWriteConn()

	if err := instance.Close(); err != nil {
		t.Fatalf("首次关闭失败: %v", err)
	}

	written := raw.written.Len()
	if written == 0 {
		t.Fatal("首次关闭应写出关闭帧")
	}

	if err := instance.Close(); err != nil {
		t.Fatalf("重复关闭失败: %v", err)
	}

	if raw.written.Len() != written {
		t.Fatalf("重复关闭不应追加写入，%d -> %d", written, raw.written.Len())
	}
}

// TestConnCloseWithCodeWritesPayload 验证自定义关闭码与原因被正确编码。
func TestConnCloseWithCodeWritesPayload(t *testing.T) {
	instance, raw := createWriteConn()

	if err := instance.CloseWithCode(CLOSE_CODE_INTERNAL_SERVER_ERROR, "boom"); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}

	code, text := parseClosePayload(t, raw.written.Bytes())
	if code != CLOSE_CODE_INTERNAL_SERVER_ERROR || text != "boom" {
		t.Fatalf("关闭帧 = (%d, %q), want (1011, boom)", code, text)
	}
}

// TestConnConcurrentWrites 验证并发写入的帧不会互相交织。
func TestConnConcurrentWrites(t *testing.T) {
	instance, raw := createWriteConn()

	const writers = 32

	var waitGroup sync.WaitGroup
	for index := 0; index < writers; index++ {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			if err := instance.WriteControl(OP_CODE_PING, []byte{byte(index)}); err != nil {
				t.Errorf("并发写入失败: %v", err)
			}
		}(index)
	}

	waitGroup.Wait()

	frames := parseServerFrames(t, raw.written.Bytes())
	if len(frames) != writers {
		t.Fatalf("帧数量 = %d, want %d", len(frames), writers)
	}

	seen := make(map[byte]struct{}, writers)
	for _, item := range frames {
		if item.opCode != OP_CODE_PING || len(item.payload) != 1 {
			t.Fatalf("帧 = (opCode=%#x, len=%d)", byte(item.opCode), len(item.payload))
		}

		seen[item.payload[0]] = struct{}{}
	}

	if len(seen) != writers {
		t.Fatalf("负载去重后数量 = %d, want %d（说明帧被交织）", len(seen), writers)
	}
}

// TestConnMaxReadLimitFallsBackToDefault 验证非正的读取上限回退为默认值。
func TestConnMaxReadLimitFallsBackToDefault(t *testing.T) {
	instance := &Conn{}

	if got := instance.GetMaxReadLimit(); got != _DEFAULT_MAX_READ_LIMIT {
		t.Fatalf("零值上限 = %d, want %d", got, int64(_DEFAULT_MAX_READ_LIMIT))
	}

	instance.SetMaxReadLimit(0)
	if got := instance.GetMaxReadLimit(); got != _DEFAULT_MAX_READ_LIMIT {
		t.Fatalf("SetMaxReadLimit(0) = %d, want %d", got, int64(_DEFAULT_MAX_READ_LIMIT))
	}

	instance.SetMaxReadLimit(-1)
	if got := instance.GetMaxReadLimit(); got != _DEFAULT_MAX_READ_LIMIT {
		t.Fatalf("SetMaxReadLimit(-1) = %d, want %d", got, int64(_DEFAULT_MAX_READ_LIMIT))
	}

	instance.SetMaxReadLimit(1024)
	if got := instance.GetMaxReadLimit(); got != 1024 {
		t.Fatalf("SetMaxReadLimit(1024) = %d, want 1024", got)
	}
}

// TestConnSetDeadlines 验证截止时间设置被传递到底层连接。
func TestConnSetDeadlines(t *testing.T) {
	instance, raw := createWriteConn()
	deadline := time.Now().Add(time.Minute)

	if err := instance.SetReadTimeout(deadline); err != nil {
		t.Fatalf("SetReadTimeout 失败: %v", err)
	}

	if err := instance.SetWriteTimeout(deadline); err != nil {
		t.Fatalf("SetWriteTimeout 失败: %v", err)
	}

	if raw.isClosed {
		t.Fatal("设置截止时间不应关闭连接")
	}
}

// TestGenerateMaskKey 验证掩码密钥具备随机性。
func TestGenerateMaskKey(t *testing.T) {
	keys := make(map[[4]byte]struct{}, 32)

	for index := 0; index < 32; index++ {
		keys[GenerateMaskKey()] = struct{}{}
	}

	if len(keys) < 2 {
		t.Fatalf("32 次生成的掩码密钥去重后仅 %d 个，随机性异常", len(keys))
	}
}
