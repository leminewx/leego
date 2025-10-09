package sse

import (
	"bytes"
	"strconv"
	"sync"

	"gitee.com/leminewx/leego/response"
)

// 使用 sync.Pool 复用 buffer
var bufferPool = &sync.Pool{
	New: func() any { return &bytes.Buffer{} },
}

// Message 定义 SSE 消息的数据结构
type Message struct {
	/*
		Event 表示事件类型，可自定义
		  - 如果指定，客户端会触发该事件
		  - 如果不指定，默认为 message 事件
	*/
	Event string
	// Id 表示事件的 ID，用于重连时指定 Last-Event-ID 头
	Id string
	// Retry 表示客户端在连接断开后重连之前等待的毫秒数
	Retry int64
	// Data 表示消息的数据，可以有多行，且每行都会作为一个数据片段，最终将所有数据用换行符连接起来作为完整数据
	Data []byte
}

// NewClosedMessage 创建一个关闭消息
func NewClosedMessage(id string) *Message {
	return &Message{
		Event: "close",
		Id:    id,
		Data:  []byte("[CLOSED]"),
	}
}

// Send 写入格式化的 SSE 消息到响应
func (own *Message) Write(resp response.Writer) error {
	// 获取 bytes.Buffer
	buffer := bufferPool.Get().(*bytes.Buffer)
	defer func() {
		buffer.Reset()
		bufferPool.Put(buffer)
	}()

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
		buffer.WriteString(strconv.Itoa(int(own.Retry)))
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

	// 发送数据
	if _, err := resp.Write(buffer.Bytes()); err != nil {
		return err
	}

	resp.Flush()
	return nil
}
