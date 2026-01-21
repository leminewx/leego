package sse

import (
	"bytes"
	"fmt"
	"strconv"
	"sync"
	"time"

	"gitee.com/leminewx/leego/response"
)

// Message 定义 SSE 消息的数据结构（消息发送之后更新就无效了，但可以复用）
type Message struct {
	// Id 表示事件的 ID，用于重连时指定 Last-Event-ID 头
	Id string
	/*
		Event 表示事件类型，可自定义
		  - 如果指定，客户端会触发该事件
		  - 如果不指定，默认为 message 事件
	*/
	Event string
	// Retry 表示客户端在连接断开后重连之前等待的毫秒数
	Retry int64
	// Data 表示消息的数据，可以有多行，且每行都会作为一个数据片段，最终将所有数据用换行符连接起来作为完整数据
	Data []byte

	formatted     []byte // 格式化数据，提高复用性
	formattedLock sync.Mutex
}

// 使用 sync.Pool 复用 buffer
var bufferPool = &sync.Pool{
	New: func() any { return bytes.NewBuffer(make([]byte, 0, 1024)) },
}

const (
	_MSG_CONNECTED       = `{"time": "%s", "message": "Welcome to connect SSE server", "client_id": "%s"}`
	_MSG_HEARTBEAT       = `{"time": "%s", "message": "Ping"}`
	_MSG_DISCONNECTED    = `{"time": "%s", "message": "SSE connection is closed"}`
	_MSG_SERVER_SHUTDOWN = `{"time": "%s", "message": "SSE server is shutting down"}`
)

// NewConnectedMessage 创建一个连接消息
func NewConnectedMessage(clientId string) *Message {
	return NewMessageWithEvent("connect", []byte(fmt.Sprintf(_MSG_CONNECTED, time.Now().Format(time.DateTime), clientId)))
}

// NewHeartbeatMessage 创建一个心跳消息
func NewHeartbeatMessage() *Message {
	return NewMessageWithEvent("heartbeat", []byte(fmt.Sprintf(_MSG_HEARTBEAT, time.Now().Format(time.DateTime))))
}

// NewClosedMessage 创建一个连接关闭消息
func NewClosedMessage() *Message {
	return NewMessageWithEvent("close", []byte(fmt.Sprintf(_MSG_DISCONNECTED, time.Now().Format(time.DateTime))))
}

// NewShutdownMessage 创建一个服务器关闭消息
func NewShutdownMessage() *Message {
	return NewMessageWithEvent("close", []byte(fmt.Sprintf(_MSG_SERVER_SHUTDOWN, time.Now().Format(time.DateTime))))
}

// NewMessageWithEvent 创建一个指定事件类型的消息
func NewMessageWithEvent(event string, data []byte) *Message {
	return &Message{
		Event: event,
		Data:  data,
	}
}

// Send 写入格式化的 SSE 消息到响应
func (own *Message) Write(resp response.Writer) error {
	// 若格式化数据无效，则直接返回
	if !own.format() {
		return nil
	}

	// 发送格式化后的数据
	if _, err := resp.Write(own.formatted); err != nil {
		return fmt.Errorf("sse: write message to response error: %w", err)
	}
	resp.Flush()

	return nil
}

// format 格式化数据
func (own *Message) format() bool {
	own.formattedLock.Lock()
	defer own.formattedLock.Unlock()

	// 双检查锁定模式
	if len(own.formatted) > 0 {
		return true
	}

	if own.Id == "" && own.Event == "" && own.Retry <= 0 && len(own.Data) == 0 {
		return false
	}

	buffer := bufferPool.Get().(*bytes.Buffer)

	// 事件ID
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

	// 数据（支持多行）
	lines := bytes.Split(own.Data, []byte{'\n'})
	for _, line := range lines {
		buffer.WriteString("data: ")
		buffer.Write(line)
		buffer.WriteByte('\n')
	}

	// 消息结束换行
	buffer.WriteByte('\n')

	// 更新格式化数据
	own.formatted = append(own.formatted[:0], buffer.Bytes()...) // 复用已有内存

	buffer.Reset()
	bufferPool.Put(buffer)

	return true
}
