package sse

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"gitee.com/leminewx/leego/response"
)

/*
Message 表示一条 SSE 消息。

字段可读可改，但“格式化”只发生一次：
一旦消息被写入过（或被 WriteMessage 投递过），后续修改字段不会再影响已生成的报文。
因此请把 Message 视为“一次发送的不可变快照”；如需复用同一对象，
请先调用 Reset 清空缓存，且确保此时没有任何协程正在读它。
*/
type Message struct {
	// Id 为事件 ID，客户端重连时会在 Last-Event-ID 请求头中回传。
	Id string
	// Event 为事件类型；为空时客户端按默认的 message 事件处理。
	Event string
	// Retry 为客户端重连前的等待毫秒数；小于等于 0 表示不下发该字段。
	Retry int64
	// Data 为消息载荷，支持多行：每行都会被编码成一条 data: 记录。
	Data []byte

	// formatted 为格式化后的报文缓存，非空后不再改变。
	formatted []byte
	// formattedLock 保护 formatted 的构建过程。
	formattedLock sync.Mutex
}

// bufferPool 复用格式化过程中的临时缓冲区，降低高频广播时的分配压力。
var bufferPool = &sync.Pool{
	New: func() any { return bytes.NewBuffer(make([]byte, 0, 1024)) },
}

// 内置消息模板。
//
// 统一使用 %q 生成带引号的字符串，避免客户端 ID 中的引号破坏 JSON 结构。
const (
	_MSG_CONNECTED       = `{"time": %q, "message": "Welcome to connect SSE server", "client_id": %q}`
	_MSG_HEARTBEAT       = `{"time": %q, "message": "Ping"}`
	_MSG_DISCONNECTED    = `{"time": %q, "message": "SSE connection is closed"}`
	_MSG_SERVER_SHUTDOWN = `{"time": %q, "message": "SSE server is shutting down"}`
)

// NewConnectedMessage 创建“连接成功”提示消息。
func NewConnectedMessage(clientId string) *Message {
	return &Message{
		Event: "connect",
		Data:  []byte(fmt.Sprintf(_MSG_CONNECTED, time.Now().Format(time.DateTime), clientId)),
	}
}

// NewHeartbeatMessage 创建心跳消息。
func NewHeartbeatMessage() *Message {
	return &Message{
		Event: "heartbeat",
		Data:  []byte(fmt.Sprintf(_MSG_HEARTBEAT, time.Now().Format(time.DateTime))),
	}
}

// NewClosedMessage 创建“连接已关闭”消息。
func NewClosedMessage() *Message {
	return &Message{
		Event: "close",
		Data:  []byte(fmt.Sprintf(_MSG_DISCONNECTED, time.Now().Format(time.DateTime))),
	}
}

// NewShutdownMessage 创建“服务器即将关闭”消息。
func NewShutdownMessage() *Message {
	return &Message{
		Event: "close",
		Data:  []byte(fmt.Sprintf(_MSG_SERVER_SHUTDOWN, time.Now().Format(time.DateTime))),
	}
}

// NewMessageWithEvent 创建指定事件类型的消息。
func NewMessageWithEvent(event string, data []byte) *Message {
	return &Message{
		Event: event,
		Data:  data,
	}
}

// Reset 清空格式化缓存，使消息可以重新构建报文。
//
// 调用方必须保证此刻没有其它协程正在读取或写入该消息。
func (own *Message) Reset() {
	own.formattedLock.Lock()
	own.formatted = nil
	own.formattedLock.Unlock()
}

// Write 将消息写入响应并立即刷新。
//
// 无效消息（所有字段均为空）会被静默忽略并返回 nil。
// 写入失败时返回包装后的错误，便于上层区分“客户端断开”和“业务错误”。
func (own *Message) Write(resp response.Writer) error {
	if resp == nil {
		return errors.New("sse: nil response writer")
	}

	if !own.format() {
		return nil
	}

	// 格式化完成后的切片内容不再改变，持锁取一次引用即可安全地在锁外写入。
	own.formattedLock.Lock()
	payload := own.formatted
	own.formattedLock.Unlock()

	if _, err := resp.Write(payload); err != nil {
		return fmt.Errorf("sse: write message to response error: %w", err)
	}

	resp.Flush()

	return nil
}

// format 构建 SSE 报文，返回是否存在可写入内容。
//
// 采用双检查锁定：只有第一个调用者会真正构建报文，
// 其余调用者直接复用已构建结果，因此高频广播时性能稳定。
func (own *Message) format() bool {
	own.formattedLock.Lock()
	defer own.formattedLock.Unlock()

	// 双检查：已格式化则直接返回。
	if len(own.formatted) > 0 {
		return true
	}

	if own.Id == "" && own.Event == "" && own.Retry <= 0 && len(own.Data) == 0 {
		return false
	}

	buffer := bufferPool.Get().(*bytes.Buffer)
	defer func() {
		buffer.Reset()
		bufferPool.Put(buffer)
	}()

	// 事件 ID
	if own.Id != "" {
		buffer.WriteString("id: ")
		buffer.WriteString(own.Id)
		buffer.WriteByte('\n')
	}

	// 事件类型
	if own.Event != "" {
		buffer.WriteString("event: ")
		buffer.WriteString(own.Event)
		buffer.WriteByte('\n')
	}

	// 重连时间
	if own.Retry > 0 {
		buffer.WriteString("retry: ")
		buffer.WriteString(strconv.FormatInt(own.Retry, 10))
		buffer.WriteByte('\n')
	}

	// 数据（多行会被拆成多条 data: 记录）
	for _, line := range bytes.Split(own.Data, []byte{'\n'}) {
		buffer.WriteString("data: ")
		buffer.Write(line)
		buffer.WriteByte('\n')
	}

	// 报文结束：空行
	buffer.WriteByte('\n')

	own.formatted = append([]byte(nil), buffer.Bytes()...)

	return true
}
