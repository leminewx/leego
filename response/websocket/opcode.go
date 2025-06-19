package websocket

// OpCode 定义消息类型
type OpCode byte

const (
	OP_CODE_CONTINUE OpCode = 0x00 // 持续数据
	OP_CODE_TEXT     OpCode = 0x01 // 文本数据
	OP_CODE_BINARY   OpCode = 0x02 // 二进制数据
	OP_CODE_CLOSE    OpCode = 0x08 // 连接关闭
	OP_CODE_PING     OpCode = 0x09 // ping
	OP_CODE_PONG     OpCode = 0x0A // pong
	// 	1000	正常关闭
	// 1001	端点离开（如服务器重启）
	// 1002	协议错误
	// 1003	接收到不支持的数据类型
	// 1007	数据格式不一致（非UTF-8）
	// 1011	服务器内部错误
)
