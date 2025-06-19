package websocket

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
)

/*
websocket protocol:

 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-------+-+-------------+-------------------------------+
|F|R|R|R| opcode|M| Payload len |    Extended payload length    |
|I|S|S|S|  (4)  |A|     (7)     |             (16/64)           |
|N|V|V|V|       |S|             |   (if payload len==126/127)   |
| |1|2|3|       |K|             |                               |
+-+-+-+-+-------+-+-------------+-------------------------------+
|     Extended payload length continued, if payload len == 127  |
+-------------------------------+-------------------------------+
|                               | Masking-key, if MASK set to 1 |
+-------------------------------+-------------------------------+
|    Masking-key (continued)    |         Payload Data          |
+---------------------------------------------------------------+
|                   Payload Data continued ...                  |
+---------------------------------------------------------------+
|                   Payload Data continued ...                  |
+---------------------------------------------------------------+
*/

// Read 从连接中读取并解析 WebSocket 数据帧
func Read(conn net.Conn) (*Frame, error) {
	// 读取前2字节
	head := make([]byte, 2)
	if _, err := io.ReadFull(conn, head); err != nil {
		return nil, err
	}

	// 新建数据帧
	frame := &Frame{
		Fin:    (head[0] & 0x80) != 0,
		OpCode: OpCode(head[0] & 0x0F),
		Masked: (head[1] & 0x80) != 0,
	}

	// 解析载荷数据长度
	payloadLen := int64(head[1] & 0x7F)
	switch {
	case payloadLen == 126:
		lenBuf := make([]byte, 2)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return nil, err
		}
		payloadLen = int64(binary.BigEndian.Uint16(lenBuf))
	case payloadLen == 127:
		lenBuf := make([]byte, 8)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return nil, err
		}
		payloadLen = int64(binary.BigEndian.Uint64(lenBuf))
	}

	// 读取32位掩码密钥
	if frame.Masked {
		maskKey := make([]byte, 4)
		if _, err := io.ReadFull(conn, maskKey); err != nil {
			return nil, err
		}
		copy(frame.MaskKey[:], maskKey)
	}

	// 读取载荷数据
	frame.Payload = make([]byte, payloadLen)
	if _, err := io.ReadFull(conn, frame.Payload); err != nil {
		return nil, err
	}

	// 应用掩码解码
	if frame.Masked {
		for i := int64(0); i < payloadLen; i++ {
			frame.Payload[i] ^= frame.MaskKey[i%4]
		}
	}

	return frame, nil
}

// Write 将 WebSocket 数据帧写入连接
func Write(conn net.Conn, frame *Frame) error {
	buf := bufferPool.Get().(*bytes.Buffer)

	// 构造第1个字节
	var b1 byte
	if frame.Fin {
		b1 |= 0x80
	}

	b1 |= byte(frame.OpCode) & 0x0F
	buf.WriteByte(b1)

	// 构造第2个字节和载荷长度
	var b2 byte
	if frame.Masked {
		b2 |= 0x80
	}

	payloadLen := len(frame.Payload)
	switch {
	case payloadLen <= 125:
		b2 |= byte(payloadLen)
		buf.WriteByte(b2)
	case payloadLen <= 65535:
		b2 |= 126
		buf.WriteByte(b2)
		if err := binary.Write(buf, binary.BigEndian, uint16(payloadLen)); err != nil {
			return err
		}
	default:
		b2 |= 127
		buf.WriteByte(b2)
		if err := binary.Write(buf, binary.BigEndian, uint64(payloadLen)); err != nil {
			return err
		}
	}

	// 处理掩码
	if frame.Masked {
		buf.Write(frame.MaskKey[:])
		maskedPayload := make([]byte, len(frame.Payload))
		for i := 0; i < len(frame.Payload); i++ {
			maskedPayload[i] = frame.Payload[i] ^ frame.MaskKey[i%4]
		}
		buf.Write(maskedPayload)
	} else {
		buf.Write(frame.Payload)
	}

	// 发送数据
	_, err := conn.Write(buf.Bytes())
	buf.Reset()
	bufferPool.Put(buf)

	return err
}
