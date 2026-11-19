package websocket

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestNewWelcomeMessage 验证欢迎消息的类型与载荷。
func TestNewWelcomeMessage(t *testing.T) {
	before := time.Now().Unix()
	message := NewWelcomeMessage("client-1")
	after := time.Now().Unix()

	if message == nil {
		t.Fatal("欢迎消息不应为 nil")
	}

	if message.Type != MSG_TYPE_CONNECTED {
		t.Fatalf("Type = %q, want %q", message.Type, MSG_TYPE_CONNECTED)
	}

	if message.Timestamp < before || message.Timestamp > after {
		t.Fatalf("Timestamp = %d, 不在 [%d, %d] 区间", message.Timestamp, before, after)
	}

	data, isMap := message.Data.(map[string]any)
	if !isMap {
		t.Fatalf("Data 类型 = %T, want map[string]any", message.Data)
	}

	if data["client_id"] != "client-1" {
		t.Fatalf("Data[client_id] = %v, want client-1", data["client_id"])
	}

	if text, _ := data["message"].(string); text == "" {
		t.Fatal("Data[message] 不应为空")
	}
}

// TestNewPingAndPongMessages 验证心跳消息的类型与载荷。
func TestNewPingAndPongMessages(t *testing.T) {
	ping := NewPingMessage()
	if ping.Type != MSG_TYPE_PING || ping.Data != MSG_TYPE_PING {
		t.Fatalf("PING 消息 = %+v", ping)
	}

	if ping.Timestamp == 0 {
		t.Fatal("PING 消息应填充时间戳")
	}

	pong := NewPongMessage()
	if pong.Type != MSG_TYPE_PONG || pong.Data != MSG_TYPE_PONG {
		t.Fatalf("PONG 消息 = %+v", pong)
	}

	if pong.Timestamp == 0 {
		t.Fatal("PONG 消息应填充时间戳")
	}
}

// TestNewSimpleMessage 验证自定义消息透传类型与载荷。
func TestNewSimpleMessage(t *testing.T) {
	message := NewSimpleMessage("custom_type", map[string]any{"key": "value"})

	if message.Type != MessageType("custom_type") {
		t.Fatalf("Type = %q, want custom_type", message.Type)
	}

	data, isMap := message.Data.(map[string]any)
	if !isMap || data["key"] != "value" {
		t.Fatalf("Data = %v, want map{key: value}", message.Data)
	}

	if message.Timestamp == 0 {
		t.Fatal("消息应填充时间戳")
	}
}

// TestMessageJSONContract 验证 JSON 字段名与序列化契约。
//
// 前端依赖固定的字段名（type/timestamp/data），一旦改动属于破坏性变更，
// 因此这里把契约锁定下来。
func TestMessageJSONContract(t *testing.T) {
	message := &Message{
		Type:      MSG_TYPE_BROADCAST,
		Timestamp: 1700000000,
		From:      "client-1",
		To:        "client-2",
		Data:      "hello",
	}

	payload, err := json.Marshal(message)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	text := string(payload)
	for _, expected := range []string{
		`"type":"broadcast"`,
		`"timestamp":1700000000`,
		`"from":"client-1"`,
		`"to":"client-2"`,
		`"data":"hello"`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("JSON 缺少片段 %s: %s", expected, text)
		}
	}

	var decoded Message
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if decoded.Type != message.Type || decoded.Timestamp != message.Timestamp {
		t.Fatalf("往返后头部字段不一致: %+v", decoded)
	}

	if decoded.From != message.From || decoded.To != message.To {
		t.Fatalf("往返后收发方不一致: %+v", decoded)
	}

	if decoded.Data != "hello" {
		t.Fatalf("往返后 Data = %v, want hello", decoded.Data)
	}
}

// TestMessageJSONOmitsEmptyFromTo 验证空收发方不会出现在 JSON 中。
func TestMessageJSONOmitsEmptyFromTo(t *testing.T) {
	payload, err := json.Marshal(NewPingMessage())
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	text := string(payload)
	if strings.Contains(text, `"from"`) || strings.Contains(text, `"to"`) {
		t.Fatalf("空的 from/to 不应出现在 JSON 中: %s", text)
	}
}

// TestMessageTypeConstants 锁定内置消息类型的字符串取值。
//
// 这些取值同时是前后端协议的一部分，修改会导致旧客户端无法识别消息。
func TestMessageTypeConstants(t *testing.T) {
	expected := map[MessageType]string{
		MSG_TYPE_PING:      "ping",
		MSG_TYPE_PONG:      "pong",
		MSG_TYPE_BROADCAST: "broadcast",
		MSG_TYPE_PRIVATE:   "private",
		MSG_TYPE_IDENTIFY:  "identify",
		MSG_TYPE_CONNECTED: "connected",
	}

	for messageType, want := range expected {
		if string(messageType) != want {
			t.Fatalf("消息类型 = %q, want %q", messageType, want)
		}
	}

	if len(expected) != 6 {
		t.Fatalf("内置消息类型数量 = %d, want 6", len(expected))
	}
}
