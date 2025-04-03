package websocket

// OpCode 定义消息类型
type OpCdoe byte

const (
	OP_CODE_CONTINUE OpCdoe = 0x00 // 持续数据
	OP_CODE_TEXT     OpCdoe = 0x01 // 文本数据
	OP_CODE_BINARY   OpCdoe = 0x02 // 二进制数据
	OP_CODE_CLOSE    OpCdoe = 0x08 // 连接关闭
	OP_CODE_PING     OpCdoe = 0x09 // ping
	OP_CODE_PONG     OpCdoe = 0x0A // pong
)
