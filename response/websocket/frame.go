package websocket

import (
	"bytes"
	"encoding/binary"
	"net"
	"sync"
)

var (
	bufferPool = &sync.Pool{New: func() any { return new(bytes.Buffer) }}
	framePool  = &sync.Pool{New: func() any { return &Frame{Payload: make([]byte, 0, 4096)} }}
)

// Frame 定义 Websocket 数据帧的数据结构
type Frame struct {
	// Fin 消息最后一个分片的标志位
	Fin bool
	// OpCode 消息类型
	OpCode OpCdoe
	// Masked 负载数据掩码标志位
	Masked bool
	// MaskKey 32位掩码密钥
	MaskKey [4]byte
	// Payload 负载数据
	Payload []byte
}

func NewFrame(opcode OpCdoe, payload []byte) *Frame {
	frame := framePool.Get().(*Frame)
	frame.Fin = false
	frame.OpCode = opcode
	frame.Masked = false
	frame.Payload = payload
	return frame
}

func (own *Frame) WithLast() *Frame {
	own.Fin = true
	return own
}

func (own *Frame) WithMask(mask [4]byte) *Frame {
	own.Masked = true
	own.MaskKey = mask
	return own
}

func (own *Frame) Write(conn net.Conn) error {
	buf := bufferPool.Get().(*bytes.Buffer)

	// 构造第1个字节
	var b1 byte
	if own.Fin {
		b1 |= 0x80
	}

	b1 |= byte(own.OpCode) & 0x0F
	buf.WriteByte(b1)

	// 构造第2个字节和载荷长度
	var b2 byte
	if own.Masked {
		b2 |= 0x80
	}

	payloadLen := len(own.Payload)
	switch {
	case payloadLen <= 125:
		b2 |= byte(payloadLen)
		buf.WriteByte(b2)
	case payloadLen <= 65535:
		b2 |= 126
		buf.WriteByte(b2)
		binary.Write(buf, binary.BigEndian, uint16(payloadLen))
	default:
		b2 |= 127
		buf.WriteByte(b2)
		binary.Write(buf, binary.BigEndian, uint64(payloadLen))
	}

	// 处理掩码
	if own.Masked {
		buf.Write(own.MaskKey[:])
		maskedPayload := make([]byte, len(own.Payload))
		for i := 0; i < len(own.Payload); i++ {
			maskedPayload[i] = own.Payload[i] ^ own.MaskKey[i%4]
		}
		buf.Write(maskedPayload)
	} else {
		buf.Write(own.Payload)
	}

	// 发送数据
	_, err := conn.Write(buf.Bytes())
	buf.Reset()
	bufferPool.Put(buf)

	return err
}

func (own *Frame) Recycle() {
	own.Payload = nil
	framePool.Put(own)
}
