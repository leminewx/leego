package upgrader

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// 帧处理相关常量。
const (
	// _MAX_CONTROL_PAYLOAD_LENGTH 为控制帧载荷上限（RFC 6455 规定控制帧不得超过 125 字节）。
	_MAX_CONTROL_PAYLOAD_LENGTH = 125
	// _DEFAULT_MAX_READ_LIMIT 为默认的单条消息上限（16 MiB）。
	//
	// 没有上限时，对端只需声明一个超大的 payload 长度就能让服务端预分配巨量内存，
	// 因此必须默认开启限制。
	_DEFAULT_MAX_READ_LIMIT = 16 << 20
	// _CLOSE_CODE_INVALID_PAYLOAD 为“载荷格式错误”的关闭码别名，便于阅读。
	_CLOSE_CODE_INVALID_PAYLOAD = CLOSE_CODE_INVALID_FRAME_PAYLOAD_DATA
)

// 连接错误定义。
var (
	// ErrConnectionIsClosed 表示连接已关闭。
	ErrConnectionIsClosed = errors.New("leego: websocket: connection closed")
	// ErrWriteIsClosed 表示关闭帧已写出，不能再写入业务数据。
	ErrWriteIsClosed = errors.New("leego: websocket: write closed")
)

// 编译期断言：Conn 必须可用于服务端与客户端两种角色。
var _ = (*Conn)(nil)

/*
Conn 表示一条已完成握手的 WebSocket 连接。

并发模型：

  - 读操作只允许在单个协程中串行调用（Read 不是并发安全的）；
  - 写操作由 writeLock 串行化，因此任意协程都可以安全调用 Write / WriteControl；
  - isClosed 与 isWriteClosed 使用原子类型，避免读写协程之间的数据竞争。
*/
type Conn struct {
	// conn 为底层 TCP/TLS 连接。
	conn net.Conn
	// reader 为带缓冲的读取器。
	reader *bufio.Reader
	// writer 为带缓冲的写入器。
	writer *bufio.Writer
	// writeLock 串行化所有写操作（业务帧与控制帧共用同一写入器）。
	writeLock sync.Mutex

	// isServer 表示是否为服务端连接（决定掩码校验方向）。
	isServer bool
	// maxReadLimit 为单条消息的最大字节数。
	maxReadLimit atomic.Int64
	// readTimeout 为单次读取的超时时间，小于等于 0 表示不设置。
	readTimeout time.Duration
	// writeTimeout 为单次写入的超时时间，小于等于 0 表示不设置。
	writeTimeout time.Duration
	// isClosed 表示连接是否已关闭。
	isClosed atomic.Bool
	// isWriteClosed 表示关闭帧是否已写出。
	isWriteClosed atomic.Bool
}

// SetMaxReadLimit 设置单条消息的最大字节数。
//
// limit 小于等于 0 时使用默认值 16 MiB；超过上限的帧会被拒绝并返回错误，
// 服务端还会向对端发送 1009（消息过大）关闭帧。
func (own *Conn) SetMaxReadLimit(limit int64) {
	if limit <= 0 {
		limit = _DEFAULT_MAX_READ_LIMIT
	}

	own.maxReadLimit.Store(limit)
}

// GetMaxReadLimit 返回单条消息的最大字节数。
func (own *Conn) GetMaxReadLimit() int64 {
	limit := own.maxReadLimit.Load()
	if limit <= 0 {
		return _DEFAULT_MAX_READ_LIMIT
	}

	return limit
}

// SetReadTimeout 设置读取的绝对截止时间。
func (own *Conn) SetReadTimeout(deadline time.Time) error {
	return own.conn.SetReadDeadline(deadline)
}

// SetWriteTimeout 设置写入的绝对截止时间。
func (own *Conn) SetWriteTimeout(deadline time.Time) error {
	return own.conn.SetWriteDeadline(deadline)
}

// IsClosed 判断连接是否已关闭。
func (own *Conn) IsClosed() bool {
	return own.isClosed.Load()
}

/*
Read 读取一条完整的数据消息。

行为说明：
  - 收到 PING 会自动回复 PONG 并继续等待下一条消息；
  - 收到 CLOSE 会回写关闭帧并返回 *CloseError；
  - 不支持分片消息：收到 FIN=0 的帧会以 1003 关闭并按协议错误返回；
  - 读超时只在进入 Read 时设置一次，处理控制帧不会延长超时，
    避免对端用大量心跳包拖住读取从而绕过超时保护。
*/
func (own *Conn) Read() (OpCode, []byte, error) {
	if own.readTimeout > 0 {
		if err := own.conn.SetReadDeadline(time.Now().Add(own.readTimeout)); err != nil {
			return 0, nil, err
		}
	}

	for {
		messageFrame, err := own.readFrame()
		if err != nil {
			return 0, nil, err
		}

		switch messageFrame.opCode {
		case OP_CODE_CLOSE:
			return 0, nil, own.handleCloseFrame(messageFrame.payload)

		case OP_CODE_PING:
			if writeErr := own.WriteControl(OP_CODE_PONG, messageFrame.payload); writeErr != nil {
				return 0, nil, writeErr
			}

			continue

		case OP_CODE_PONG:
			continue

		case OP_CODE_CONTINUE:
			text := "fragmented messages are not supported"
			own.closeWithCodeQuietly(CLOSE_CODE_UNSUPPORTED_DATA, text)
			return 0, nil, newProtocolError("%s", text)

		case OP_CODE_TEXT:
			if !messageFrame.fin {
				text := "fragmented messages are not supported"
				own.closeWithCodeQuietly(CLOSE_CODE_UNSUPPORTED_DATA, text)
				return 0, nil, newProtocolError("%s", text)
			}

			if !utf8.Valid(messageFrame.payload) {
				text := "invalid UTF-8 in text message"
				own.closeWithCodeQuietly(_CLOSE_CODE_INVALID_PAYLOAD, text)
				return 0, nil, newProtocolError("%s", text)
			}

			return messageFrame.opCode, messageFrame.payload, nil

		case OP_CODE_BINARY:
			if !messageFrame.fin {
				text := "fragmented messages are not supported"
				own.closeWithCodeQuietly(CLOSE_CODE_UNSUPPORTED_DATA, text)
				return 0, nil, newProtocolError("%s", text)
			}

			return messageFrame.opCode, messageFrame.payload, nil

		default:
			text := fmt.Sprintf("unknown opcode %#x", byte(messageFrame.opCode))
			own.closeWithCodeQuietly(CLOSE_CODE_PROTOCOL_ERROR, text)
			return 0, nil, newProtocolError("%s", text)
		}
	}
}

// handleCloseFrame 处理对端关闭帧。
func (own *Conn) handleCloseFrame(payload []byte) error {
	if len(payload) < 2 {
		own.closeWithCodeQuietly(CLOSE_CODE_NORMAL_CLOSURE, "")
		return &CloseError{Code: CLOSE_CODE_NO_CODE_RECEIVED}
	}

	closeCode := CloseCode(binary.BigEndian.Uint16(payload))

	var text string
	if len(payload) > 2 {
		text = string(payload[2:])
		if !utf8.ValidString(text) {
			own.closeWithCodeQuietly(_CLOSE_CODE_INVALID_PAYLOAD, "")
			return newProtocolError("invalid UTF-8 in close message")
		}
	}

	own.closeWithCodeQuietly(closeCode, text)

	return &CloseError{Code: closeCode, Text: text}
}

// Write 写入一条数据消息。
//
// 该方法并发安全；关闭帧请使用 CloseWithCode 发送，以避免状态不一致。
func (own *Conn) Write(opCode OpCode, data []byte) error {
	if opCode == OP_CODE_CLOSE {
		return errors.New("leego: websocket: use CloseWithCode to send close frame")
	}

	if opCode&0x08 != 0 && len(data) > _MAX_CONTROL_PAYLOAD_LENGTH {
		return newProtocolError("control frame payload exceeds %d bytes", _MAX_CONTROL_PAYLOAD_LENGTH)
	}

	if opCode == OP_CODE_TEXT && !utf8.Valid(data) {
		return errors.New("leego: websocket: invalid UTF-8 in text message")
	}

	own.writeLock.Lock()
	defer own.writeLock.Unlock()

	if own.isClosed.Load() {
		return ErrConnectionIsClosed
	}

	if own.isWriteClosed.Load() {
		return ErrWriteIsClosed
	}

	return own.writeFrame(opCode, data)
}

// WriteControl 写入一个控制帧（PING / PONG）。
//
// 该方法并发安全，供读循环回复 PONG 以及应用层主动探活使用。
func (own *Conn) WriteControl(opCode OpCode, data []byte) error {
	if opCode != OP_CODE_PING && opCode != OP_CODE_PONG {
		return errors.New("leego: websocket: not a control frame")
	}

	if len(data) > _MAX_CONTROL_PAYLOAD_LENGTH {
		return newProtocolError("control frame payload exceeds %d bytes", _MAX_CONTROL_PAYLOAD_LENGTH)
	}

	own.writeLock.Lock()
	defer own.writeLock.Unlock()

	if own.isClosed.Load() || own.isWriteClosed.Load() {
		return ErrConnectionIsClosed
	}

	return own.writeFrame(opCode, data)
}

// Close 以正常关闭码关闭连接。
func (own *Conn) Close() error {
	return own.CloseWithCode(CLOSE_CODE_NORMAL_CLOSURE, "")
}

// CloseWithCode 使用指定关闭码关闭连接。
//
// 该方法是幂等的：重复调用不会重复写关闭帧，也不会重复关闭底层连接。
func (own *Conn) CloseWithCode(code CloseCode, text string) error {
	own.writeLock.Lock()
	defer own.writeLock.Unlock()

	if own.isClosed.Swap(true) {
		return nil
	}

	var closeErr error
	if !own.isWriteClosed.Load() {
		closeErr = own.writeCloseFrame(code, text)
	}

	if err := own.conn.Close(); closeErr == nil {
		closeErr = err
	}

	return closeErr
}

// closeWithCodeQuietly 尽力发送关闭帧（忽略写入错误），供协议错误路径使用。
func (own *Conn) closeWithCodeQuietly(code CloseCode, text string) {
	_ = own.CloseWithCode(code, text)
}

// readFrame 读取并校验一个帧。
func (own *Conn) readFrame() (*frame, error) {
	firstByte, err := own.reader.ReadByte()
	if err != nil {
		return nil, err
	}

	fin := firstByte&0x80 != 0
	rsv := firstByte & 0x70
	opCode := OpCode(firstByte & 0x0F)

	// 未协商任何扩展，RSV 必须为 0。
	if rsv != 0 {
		return nil, newProtocolError("reserved bits set")
	}

	secondByte, err := own.reader.ReadByte()
	if err != nil {
		return nil, err
	}

	masked := secondByte&0x80 != 0
	payloadLength := uint64(secondByte & 0x7F)

	switch payloadLength {
	case 126:
		var length16 uint16
		if err := binary.Read(own.reader, binary.BigEndian, &length16); err != nil {
			return nil, err
		}

		payloadLength = uint64(length16)

	case 127:
		var length64 uint64
		if err := binary.Read(own.reader, binary.BigEndian, &length64); err != nil {
			return nil, err
		}

		// 64 位长度的最高位必须为 0。
		if length64&(1<<63) != 0 {
			return nil, newProtocolError("invalid payload length")
		}

		payloadLength = length64
	}

	isControl := opCode&0x08 != 0
	if isControl {
		if !fin {
			return nil, newProtocolError("control frame must not be fragmented")
		}

		if payloadLength > _MAX_CONTROL_PAYLOAD_LENGTH {
			return nil, newProtocolError("control frame payload exceeds %d bytes", _MAX_CONTROL_PAYLOAD_LENGTH)
		}
	}

	// 掩码方向校验：客户端发往服务端的帧必须掩码，反之必须不掩码。
	if own.isServer && !masked {
		return nil, newProtocolError("client frame must be masked")
	}

	if !own.isServer && masked {
		return nil, newProtocolError("server frame must not be masked")
	}

	if limit := own.GetMaxReadLimit(); int64(payloadLength) > limit {
		text := fmt.Sprintf("message of %d bytes exceeds limit of %d bytes", payloadLength, limit)
		// 服务端主动告知对端“消息过大”（1009），再以错误结束读取，
		// 使对端不必继续发送剩余数据。仅在连接已完整初始化时写关闭帧，
		// 避免在未绑定底层连接的场景下解引用空指针。
		if own.isServer && own.conn != nil && own.writer != nil {
			own.closeWithCodeQuietly(CLOSE_CODE_MESSAGE_TOO_BIG, text)
		}

		return nil, fmt.Errorf("leego: websocket: %s", text)
	}

	var maskKey [4]byte
	if masked {
		if _, err := io.ReadFull(own.reader, maskKey[:]); err != nil {
			return nil, err
		}
	}

	payload := make([]byte, payloadLength)
	if _, err := io.ReadFull(own.reader, payload); err != nil {
		return nil, err
	}

	if masked {
		for index := range payload {
			payload[index] ^= maskKey[index%4]
		}
	}

	return &frame{
		fin:     fin,
		opCode:  opCode,
		masked:  masked,
		payload: payload,
	}, nil
}

// writeFrame 写入一个完整帧（调用方必须持有 writeLock）。
//
// 服务端发出的帧不带掩码；客户端方向由 GenerateMaskKey 提供，当前实现仅用于服务端。
func (own *Conn) writeFrame(opCode OpCode, data []byte) error {
	if own.writeTimeout > 0 {
		if err := own.conn.SetWriteDeadline(time.Now().Add(own.writeTimeout)); err != nil {
			return err
		}
	}

	header := make([]byte, 2, 10)
	header[0] = 0x80 | byte(opCode) // FIN=1, RSV=0

	payloadLength := len(data)
	switch {
	case payloadLength <= 125:
		header[1] = byte(payloadLength)

	case payloadLength <= 65535:
		header[1] = 126
		header = binary.BigEndian.AppendUint16(header, uint16(payloadLength))

	default:
		header[1] = 127
		header = binary.BigEndian.AppendUint64(header, uint64(payloadLength))
	}

	if _, err := own.writer.Write(header); err != nil {
		return err
	}

	if len(data) > 0 {
		if _, err := own.writer.Write(data); err != nil {
			return err
		}
	}

	return own.writer.Flush()
}

// writeCloseFrame 写入关闭帧（调用方必须持有 writeLock）。
func (own *Conn) writeCloseFrame(code CloseCode, text string) error {
	if own.isWriteClosed.Swap(true) {
		return nil
	}

	payload := make([]byte, 2+len(text))
	binary.BigEndian.PutUint16(payload, uint16(code))
	copy(payload[2:], text)

	return own.writeFrame(OP_CODE_CLOSE, payload)
}

// GenerateMaskKey 生成 4 字节掩码密钥（客户端方向使用）。
func GenerateMaskKey() [4]byte {
	var key [4]byte
	_, _ = rand.Read(key[:])

	return key
}

// newProtocolError 构造带统一前缀的协议错误。
func newProtocolError(format string, args ...any) error {
	return fmt.Errorf("leego: websocket: protocol error: "+format, args...)
}
