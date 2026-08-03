package upgrader

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"time"
	"unicode/utf8"
)

// Conn 定义 WebSocket 连接的数据结构
type Conn struct {
	// 连接
	conn   net.Conn
	reader *bufio.Reader
	writer *bufio.Writer
	lock   sync.Mutex

	// 状态
	closed      bool
	isServer    bool
	writeClosed bool

	// 配置
	readTimeout  time.Duration
	writeTimeout time.Duration
}

// SetMaxReadLimit 设置最大读取大小
func (own *Conn) SetMaxReadLimit(limit int64) {
	// 在这个简单实现中，我们依赖调用方控制消息大小
}

// SetReadTimeout 设置读取超时时间
func (own *Conn) SetReadTimeout(timeout time.Time) error {
	return own.conn.SetReadDeadline(timeout)
}

// SetWriteTimeout 设置写入超时时间
func (own *Conn) SetWriteTimeout(timeout time.Time) error {
	return own.conn.SetWriteDeadline(timeout)
}

// Read 读取 WebSocket 消息
func (own *Conn) Read() (opCode OpCode, data []byte, err error) {
	for {
		// 设置读取超时
		if own.readTimeout > 0 {
			if err = own.conn.SetReadDeadline(time.Now().Add(own.readTimeout)); err != nil {
				return 0, nil, err
			}
		}

		// 读取帧头
		frame, err := own.readFrame()
		if err != nil {
			return 0, nil, err
		}

		// 处理控制帧
		switch frame.opCode {
		// 处理关闭帧消息
		case OP_CODE_CLOSE:
			// 未收到关闭状态码
			if len(frame.payload) < 2 {
				own.writeCloseFrameSafe(CLOSE_CODE_NO_CODE_RECEIVED, "")
				return 0, nil, &CloseError{Code: CLOSE_CODE_NO_CODE_RECEIVED}
			}

			// 读取关闭状态码
			closeCode := CloseCode(binary.BigEndian.Uint16(frame.payload))

			// 读取关闭消息
			var closeMessage string
			if len(frame.payload) > 2 {
				closeMessage = string(frame.payload[2:])
				if !utf8.ValidString(closeMessage) {
					return 0, nil, errors.New("leego: websocket: invalid UTF-8 in close message")
				}
			}

			// 发送关闭帧
			own.writeCloseFrameSafe(closeCode, closeMessage)
			return 0, nil, &CloseError{Code: closeCode, Text: closeMessage}

		// 处理 Ping 消息
		case OP_CODE_PING:
			own.writeControl(OP_CODE_PONG, frame.payload)
			continue

		// 处理 Pong 消息
		case OP_CODE_PONG:
			continue

		// 处理文本消息
		case OP_CODE_TEXT:
			if !utf8.Valid(frame.payload) {
				own.writeCloseFrameSafe(CLOSE_CODE_INVALID_FRAME_PAYLOAD_DATA, "")
				return 0, nil, errors.New("leego: websocket: invalid UTF-8 in text message")
			}
			fallthrough

		// 处理二进制消息
		case OP_CODE_BINARY:
			return frame.opCode, frame.payload, nil

		// 处理未知类型消息
		default:
			own.writeCloseFrameSafe(CLOSE_CODE_PROTOCOL_ERROR, "")
			return 0, nil, errors.New("leego: websocket: unknown opcode")
		}
	}
}

// Write 写入 WebSocket 消息
func (own *Conn) Write(opCode OpCode, data []byte) error {
	own.lock.Lock()
	defer own.lock.Unlock()

	// 检查是否已关闭
	if own.closed {
		return errors.New("leego: websocket: connection closed")
	}

	// 检查是否已写入关闭帧
	if own.writeClosed {
		return errors.New("leego: websocket: write closed")
	}

	// 验证文本消息的 UTF-8 编码
	if opCode == OP_CODE_TEXT && !utf8.Valid(data) {
		return errors.New("leego: websocket: invalid UTF-8 in text message")
	}

	return own.writeFrame(opCode, data)
}

// writeControl 在持写锁的前提下发送控制帧（由读循环调用，避免与 Write 竞争 writer）
func (own *Conn) writeControl(opcode OpCode, data []byte) {
	own.lock.Lock()
	defer own.lock.Unlock()
	if own.closed || own.writeClosed {
		return
	}
	_ = own.writeFrame(opcode, data)
}

// writeCloseFrameSafe 线程安全地发送关闭帧（由读循环调用）
func (own *Conn) writeCloseFrameSafe(code CloseCode, text string) {
	own.lock.Lock()
	defer own.lock.Unlock()
	if own.closed || own.writeClosed {
		return
	}
	_ = own.writeCloseFrame(code, text)
}

// Close 关闭 WebSocket 连接
func (own *Conn) Close() error {
	return own.CloseWithCode(CLOSE_CODE_NORMAL_CLOSURE, "")
}

// CloseWithCode 使用指定状态码关闭连接
func (own *Conn) CloseWithCode(code CloseCode, text string) error {
	own.lock.Lock()
	defer own.lock.Unlock()

	// 检查是否已关闭
	if own.closed {
		return nil
	}

	// 标记为已关闭
	own.closed = true

	// 发送关闭帧
	err := own.writeCloseFrame(code, text)

	// 关闭底层连接
	if closeErr := own.conn.Close(); err == nil {
		err = closeErr
	}

	return err
}

// readFrame 读取 WebSocket 帧
func (own *Conn) readFrame() (*frame, error) {
	// 读取第一个字节
	b1, err := own.reader.ReadByte()
	if err != nil {
		return nil, err
	}

	fin := b1&0x80 != 0
	rsv := b1 & 0x70
	opCode := OpCode(b1 & 0x0F)

	// 验证 RSV 位
	if rsv != 0 {
		return nil, errors.New("leego: websocket: reserved bits set")
	}

	// 读取第二个字节
	b2, err := own.reader.ReadByte()
	if err != nil {
		return nil, err
	}

	mask := b2&0x80 != 0
	payloadLen := uint64(b2 & 0x7F)

	// 处理扩展载荷长度
	switch payloadLen {
	case 126:
		var len16 uint16
		if err := binary.Read(own.reader, binary.BigEndian, &len16); err != nil {
			return nil, err
		}
		payloadLen = uint64(len16)
	case 127:
		var len64 uint64
		if err := binary.Read(own.reader, binary.BigEndian, &len64); err != nil {
			return nil, err
		}
		payloadLen = len64
	}

	// 读取掩码密钥
	var maskKey [4]byte
	if mask {
		if _, err := io.ReadFull(own.reader, maskKey[:]); err != nil {
			return nil, err
		}
	}

	// 读取载荷数据
	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(own.reader, payload); err != nil {
		return nil, err
	}

	// 解掩码数据
	if mask {
		for i := uint64(0); i < payloadLen; i++ {
			payload[i] ^= maskKey[i%4]
		}
	}

	return &frame{
		fin:     fin,
		opCode:  opCode,
		masked:  mask,
		payload: payload,
	}, nil
}

// writeFrame 写入 WebSocket 帧
func (own *Conn) writeFrame(opcode OpCode, data []byte) error {
	// 设置写入超时
	if own.writeTimeout > 0 {
		if err := own.conn.SetWriteDeadline(time.Now().Add(own.writeTimeout)); err != nil {
			return err
		}
	}

	// 构建帧头
	header := make([]byte, 2, 14)
	header[0] = 0x80 | byte(opcode) // FIN=1, RSV=0, Opcode

	payloadLen := len(data)
	switch {
	case payloadLen <= 125:
		header[1] = byte(payloadLen)
	case payloadLen <= 65535:
		header[1] = 126
		header = append(header, byte(payloadLen>>8), byte(payloadLen))
	default:
		header[1] = 127
		header = append(header, byte(payloadLen>>56), byte(payloadLen>>48), byte(payloadLen>>40), byte(payloadLen>>32),
			byte(payloadLen>>24), byte(payloadLen>>16), byte(payloadLen>>8), byte(payloadLen))
	}

	// 写入帧头
	if _, err := own.writer.Write(header); err != nil {
		return err
	}

	// 写入载荷数据
	if _, err := own.writer.Write(data); err != nil {
		return err
	}

	return own.writer.Flush()
}

// writeCloseFrame 写入关闭帧（调用方必须持有 own.lock）
func (own *Conn) writeCloseFrame(code CloseCode, text string) error {
	// 检查是否已写入关闭帧
	if own.writeClosed {
		return nil
	}

	// 标记已写入关闭帧
	own.writeClosed = true

	// 创建关闭帧
	payload := make([]byte, 2+len(text))
	binary.BigEndian.PutUint16(payload, uint16(code))
	copy(payload[2:], text)

	// 设置写入关闭帧
	return own.writeFrame(OP_CODE_CLOSE, payload)
}

// GenerateMaskKey 生成 4 字节的掩码密钥（用于客户端）
func GenerateMaskKey() [4]byte {
	var key [4]byte
	_, _ = rand.Read(key[:])
	return key
}
