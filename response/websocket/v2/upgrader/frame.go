package upgrader

// frame 定义 WebSocket 消息帧的数据结构
type frame struct {
	fin     bool   // 消息最后一个分片的标志位
	opCode  OpCode // 消息类型
	masked  bool   // 负载数据掩码标志位
	payload []byte // 负载数据
}
