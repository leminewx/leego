package websocket

import (
	"bufio"
	"net"
)

// Conn 定义 Websocket 连接的数据结构
type Conn struct {
	// chunkSize 指定数据帧的载荷大小
	chunkSize int64
	// masked 掩码标志位
	masked bool
	// MaskKey 掩码密钥
	maskKey [4]byte
	// conn 原始连接
	conn net.Conn
	// reader 客户端创建连接时可能写入的数据缓存
	reader *bufio.ReadWriter
}

// NewConn 新建一个 Websocket 连接
func NewConn(conn net.Conn) *Conn {
	return &Conn{chunkSize: 1024, conn: conn}
}

func (own *Conn) WithMaskKey(key [4]byte) *Conn {
	own.masked = true
	own.maskKey = key
	return own
}

func (own *Conn) WithChunkSize(size int64) *Conn {
	own.chunkSize = size
	return own
}

func (own *Conn) SendText(text []byte) error {
	return own.Write(OP_CODE_TEXT, text)
}

func (own *Conn) SendBytes(data []byte) error {
	return own.Write(OP_CODE_BINARY, data)
}

func (own *Conn) Ping() error {

}

func (own *Conn) Pong() error {

}

// Close 关闭 Websocket 连接
func (own *Conn) Close() error {
	if err := own.Write(OP_CODE_CLOSE, []byte{}); err != nil {
		return err
	}

	return own.conn.Close()
}

func (own *Conn) Read() (*Frame, error) {

}

func (own *Conn) Write(opcode OpCdoe, msg []byte) (err error) {
	total := int64(len(msg))

	// 数据不超过1帧
	if total < own.chunkSize {
		// 构造数据帧
		frame := NewFrame(opcode, msg).WithLast()
		if own.masked {
			frame.WithMask(own.maskKey)
		}

		// 发送数据
		err = frame.Write(own.conn)
		// 回收数据帧
		frame.Recycle()
		return
	}

	// 发送首帧
	firstChunk := msg[:own.chunkSize]
	firstFrame := NewFrame(opcode, firstChunk)
	if own.masked {
		firstFrame.WithMask(own.maskKey)
	}

	err = firstFrame.Write(own.conn)
	if firstFrame.Recycle(); err != nil {
		return err
	}

	// 发送中间帧
	for i := own.chunkSize; i < total; i += own.chunkSize {
		end := i + own.chunkSize
		if end > total {
			end = total
		}

		continueFrame := NewFrame(OP_CODE_CONTINUE, msg[i:end])
		if end == total {
			continueFrame.WithLast()
		}
		if own.masked {
			firstFrame.WithMask(own.maskKey)
		}

		err = continueFrame.Write(own.conn)
		if firstFrame.Recycle(); err != nil {
			return err
		}
	}

	return nil
}

// // 处理分帧消息示例
// func handleFragmentedMessage(conn net.Conn) {
// 	var message []byte
// 	var opcode byte

// 	for {
// 		frame, err := ReadFrame(conn)
// 		if err != nil {
// 			// 处理错误
// 			break
// 		}

// 		if opcode == 0 { // 第一个帧
// 			opcode = frame.OpCode
// 			if opcode >= 0x8 { // 控制帧不能分帧
// 				handleControlFrame(frame)
// 				continue
// 			}
// 		}

// 		message = append(message, frame.Payload...)

// 		if frame.Fin {
// 			// 处理完整消息
// 			switch opcode {
// 			case 0x1: // 文本消息
// 				handleTextMessage(string(message))
// 			case 0x2: // 二进制消息
// 				handleBinaryMessage(message)
// 			}
// 			// 重置状态
// 			message = nil
// 			opcode = 0
// 		}
// 	}
// }

// // 处理控制帧示例
// func handleControlFrame(frame *Frame) {
// 	switch frame.OpCode {
// 	case OP_CODE_CLOSE:
// 		code := binary.BigEndian.Uint16(frame.Payload[:2])
// 		reason := string(frame.Payload[2:])
// 		log.Printf("收到关闭帧: code=%d, reason=%s", code, reason)
// 	case 0x9: // Ping帧
// 		sendPong(frame.Payload)
// 	case 0xA: // Pong帧
// 		// 通常不需要处理
// 	}
// }
