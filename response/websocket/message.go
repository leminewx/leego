package websocket

import (
	"time"
)

// MessageType 定义业务消息类型。
//
// 消息类型由应用层自定义，本包内置的类型仅覆盖连接管理所需的最小集合。
type MessageType string

const (
	// MSG_TYPE_PING 为心跳请求消息类型。
	MSG_TYPE_PING MessageType = "ping"
	// MSG_TYPE_PONG 为心跳响应消息类型。
	MSG_TYPE_PONG MessageType = "pong"
	// MSG_TYPE_BROADCAST 为广播消息类型。
	MSG_TYPE_BROADCAST MessageType = "broadcast"
	// MSG_TYPE_PRIVATE 为私聊消息类型。
	MSG_TYPE_PRIVATE MessageType = "private"
	// MSG_TYPE_IDENTIFY 为身份识别消息类型。
	MSG_TYPE_IDENTIFY MessageType = "identify"
	// MSG_TYPE_CONNECTED 为连接成功消息类型。
	MSG_TYPE_CONNECTED MessageType = "connected"
)

// Message 定义客户端与服务端之间传输的业务消息。
//
// 消息体固定为 JSON 文本帧：字段名使用小写下划线风格，
// 与常见前端约定一致，同时便于日志与抓包排查。
type Message struct {
	// Type 为消息类型。
	Type MessageType `json:"type"`
	// Timestamp 为消息产生时间（Unix 秒）。
	Timestamp int64 `json:"timestamp"`
	// From 为发送方客户端 ID，仅服务端下发时填充。
	From string `json:"from,omitempty"`
	// To 为接收方客户端 ID，仅私聊消息使用。
	To string `json:"to,omitempty"`
	// Data 为消息载荷，可以是任意可 JSON 序列化的值。
	Data any `json:"data"`
}

// NewWelcomeMessage 创建连接成功消息。
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

// NewPingMessage 创建心跳请求消息。
func NewPingMessage() *Message {
	return &Message{
		Type:      MSG_TYPE_PING,
		Timestamp: time.Now().Unix(),
		Data:      MSG_TYPE_PING,
	}
}

// NewPongMessage 创建心跳响应消息。
func NewPongMessage() *Message {
	return &Message{
		Type:      MSG_TYPE_PONG,
		Timestamp: time.Now().Unix(),
		Data:      MSG_TYPE_PONG,
	}
}

// NewSimpleMessage 创建携带任意载荷的简单消息。
func NewSimpleMessage(messageType MessageType, data any) *Message {
	return &Message{
		Type:      messageType,
		Timestamp: time.Now().Unix(),
		Data:      data,
	}
}
