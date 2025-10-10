package websocket

import (
	"time"
)

// MessageType 定义消息类型的数据类型
type MessageType string

const (
	MSG_TYPE_PING      MessageType = "ping"
	MSG_TYPE_PONG      MessageType = "pong"
	MSG_TYPE_BROADCAST MessageType = "broadcast"
	MSG_TYPE_PRIVATE   MessageType = "private"
	MSG_TYPE_IDENTIFY  MessageType = "identify"
	MSG_TYPE_CONNECTED MessageType = "connected"
)

// Message 定义 WebSocket 消息的数据结构
type Message struct {
	Type      MessageType `json:"type"`           // 消息类型
	Timestamp int64       `json:"timestamp"`      // 消息时间戳
	From      string      `json:"from,omitempty"` // 发送方Id
	To        string      `json:"to,omitempty"`   // 接收方Id
	Data      any         `json:"data"`           // 消息数据
}

// NewWelcomeMessage 创建一条欢迎消息
func NewWelcomeMessage(clientId string) *Message {
	return &Message{
		Type:      MSG_TYPE_CONNECTED,
		Timestamp: time.Now().Unix(),
		Data: map[string]any{
			"client_id": clientId,
			"message":   "Connected to WebSocket server",
		},
	}
}

// NewPingMessage 创建一条心跳消息
func NewPingMessage() *Message {
	return &Message{
		Type:      MSG_TYPE_PING,
		Timestamp: time.Now().Unix(),
		Data:      MSG_TYPE_PING,
	}
}

// NewPongMessage 创建一条心跳响应消息
func NewPongMessage() *Message {
	return &Message{
		Type:      MSG_TYPE_PONG,
		Timestamp: time.Now().Unix(),
		Data:      MSG_TYPE_PONG,
	}
}

// NewSimpleMessage 创建一条简单的消息
func NewSimpleMessage(messageType MessageType, message any) *Message {
	return &Message{
		Type:      messageType,
		Timestamp: time.Now().Unix(),
		Data:      message,
	}
}
