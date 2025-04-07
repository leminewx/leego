package websocket

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"net"
	"sync"
)

const (
	MaxChunkSize int64 = 1 << 20 // 数据分片时的最大载荷：1M
)

var (
	framePool  = &sync.Pool{New: func() any { return &Frame{Payload: make([]byte, 0, MaxChunkSize)} }}
	bufferPool = &sync.Pool{New: func() any { return new(bytes.Buffer) }}
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
	frame.MaskKey = [4]byte{}
	frame.Payload = payload
	return frame
}

func NewEmptyFrame() *Frame {
	frame := framePool.Get().(*Frame)
	frame.Fin = false
	frame.OpCode = OpCdoe(OP_CODE_CONTINUE)
	frame.Masked = false
	frame.MaskKey = [4]byte{}
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

func (own *Frame) Write(writer net.Conn) error {
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
	_, err := writer.Write(buf.Bytes())
	buf.Reset()
	bufferPool.Put(buf)

	return err
}

func (own *Frame) Read(reader *bufio.ReadWriter) error {
	// 读取前两个字节
	head, err := reader.ReadByte()
	if err != nil {
		return err
	}
	second, err := reader.ReadByte()
	if err != nil {
		return err
	}

	// 创建一个接收数据帧
	own.Fin = (head & 0x80) != 0
	own.OpCode = OpCdoe(head & 0x0F)
	own.Masked = (second & 0x80) != 0

	// 处理扩展长度
	length := int(second & 0x7F)
	switch {
	case length == 126:
		b1, _ := reader.ReadByte()
		b2, _ := reader.ReadByte()
		length = int(b1)<<8 | int(b2)
	case length == 127:
		// 简化处理，不处理超大长度
		return nil
	}

	// 读取掩码密钥
	if own.Masked {
		reader.Read(own.MaskKey[:])
	}

	// 读取数据
	own.Payload = make([]byte, length)
	if _, err := reader.Read(own.Payload); err != nil {
		return err
	}

	// 应用掩码
	if own.Masked {
		for i := 0; i < len(own.Payload); i++ {
			own.Payload[i] ^= own.MaskKey[i%4]
		}
	}

	return nil
}

func (own *Frame) Recycle() {
	own.Payload = nil
	framePool.Put(own)
}
