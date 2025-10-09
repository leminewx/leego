package upgrader

// OpCode 定义 websocket 操作码的数据类型
type OpCode byte

const (
	OP_CODE_CONTINUE OpCode = 0x0 // 持续数据
	OP_CODE_TEXT     OpCode = 0x1 // 文本数据
	OP_CODE_BINARY   OpCode = 0x2 // 二进制数据
	OP_CODE_CLOSE    OpCode = 0x8 // 连接关闭
	OP_CODE_PING     OpCode = 0x9 // ping
	OP_CODE_PONG     OpCode = 0xA // pong
)
