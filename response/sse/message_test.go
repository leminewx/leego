package sse

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"gitee.com/leminewx/leego/response"
)

// createTestResponse 创建一个绑定 httptest.ResponseRecorder 的响应写入器。
func createTestResponse() (response.Writer, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	return response.NewWriter(recorder), recorder
}

// TestMessageIgnoresEmptyMessage 验证所有字段均为空的消息被静默忽略：
// 既不写响应，也不返回错误。
func TestMessageIgnoresEmptyMessage(t *testing.T) {
	resp, recorder := createTestResponse()

	if err := (&Message{}).Write(resp); err != nil {
		t.Fatalf("空消息不应报错: %v", err)
	}

	if recorder.Body.Len() != 0 {
		t.Fatalf("空消息不应写入任何内容，实际写入 %q", recorder.Body.String())
	}
}

// TestMessageWritesAllFields 验证 id / event / retry / data 按规范顺序输出。
func TestMessageWritesAllFields(t *testing.T) {
	resp, recorder := createTestResponse()

	msg := &Message{Id: "42", Event: "update", Retry: 3000, Data: []byte(`{"ok":true}`)}
	if err := msg.Write(resp); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	expected := "id: 42\nevent: update\nretry: 3000\ndata: {\"ok\":true}\n\n"
	if recorder.Body.String() != expected {
		t.Fatalf("报文 = %q, want %q", recorder.Body.String(), expected)
	}
}

// TestMessageWritesMultilineData 验证多行载荷被拆分为多条 data: 记录。
func TestMessageWritesMultilineData(t *testing.T) {
	resp, recorder := createTestResponse()

	msg := &Message{Data: []byte("line1\nline2\nline3")}
	if err := msg.Write(resp); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	expected := "data: line1\ndata: line2\ndata: line3\n\n"
	if recorder.Body.String() != expected {
		t.Fatalf("报文 = %q, want %q", recorder.Body.String(), expected)
	}
}

// TestMessageSkipsNonPositiveRetry 验证 retry 小于等于 0 时不下发该字段。
func TestMessageSkipsNonPositiveRetry(t *testing.T) {
	for _, retry := range []int64{0, -1} {
		resp, recorder := createTestResponse()

		msg := &Message{Retry: retry, Data: []byte("payload")}
		if err := msg.Write(resp); err != nil {
			t.Fatalf("写入失败: %v", err)
		}

		if strings.Contains(recorder.Body.String(), "retry:") {
			t.Fatalf("retry=%d 时不应下发 retry 字段: %q", retry, recorder.Body.String())
		}
	}
}

// TestMessageOnlyIdStillWritesDataLine 验证仅设置 id 时仍会输出一条空 data 行，
// 保证报文字段完整、客户端不会误判为无载荷事件。
func TestMessageOnlyIdStillWritesDataLine(t *testing.T) {
	resp, recorder := createTestResponse()

	if err := (&Message{Id: "abc"}).Write(resp); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	expected := "id: abc\ndata: \n\n"
	if recorder.Body.String() != expected {
		t.Fatalf("报文 = %q, want %q", recorder.Body.String(), expected)
	}
}

// TestMessageCachesFormattedPayload 验证报文只构建一次：
// 写入后修改字段不会改变后续写入的内容，符合“一次发送的不可变快照”语义。
func TestMessageCachesFormattedPayload(t *testing.T) {
	resp, recorder := createTestResponse()

	msg := &Message{Data: []byte("first")}
	if err := msg.Write(resp); err != nil {
		t.Fatalf("首次写入失败: %v", err)
	}

	msg.Data = []byte("second")
	if err := msg.Write(resp); err != nil {
		t.Fatalf("二次写入失败: %v", err)
	}

	expected := "data: first\n\ndata: first\n\n"
	if recorder.Body.String() != expected {
		t.Fatalf("报文 = %q, want %q", recorder.Body.String(), expected)
	}
}

// TestMessageResetRebuildsPayload 验证 Reset 后可以重新构建报文。
func TestMessageResetRebuildsPayload(t *testing.T) {
	resp, recorder := createTestResponse()

	msg := &Message{Data: []byte("first")}
	if err := msg.Write(resp); err != nil {
		t.Fatalf("首次写入失败: %v", err)
	}

	msg.Reset()
	msg.Data = []byte("second")
	if err := msg.Write(resp); err != nil {
		t.Fatalf("Reset 后写入失败: %v", err)
	}

	expected := "data: first\n\ndata: second\n\n"
	if recorder.Body.String() != expected {
		t.Fatalf("报文 = %q, want %q", recorder.Body.String(), expected)
	}
}

// TestMessageFlushesAfterWrite 验证写入后会立即刷新，保证事件实时下发。
func TestMessageFlushesAfterWrite(t *testing.T) {
	resp, recorder := createTestResponse()

	if err := (&Message{Data: []byte("now")}).Write(resp); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	if !recorder.Flushed {
		t.Fatal("写入消息后必须立即 Flush")
	}
}

// TestMessageRejectsNilResponse 验证 nil 响应写入器返回明确错误。
func TestMessageRejectsNilResponse(t *testing.T) {
	if err := (&Message{Data: []byte("x")}).Write(nil); err == nil {
		t.Fatal("nil 响应写入器必须返回错误")
	}
}

// TestMessageConstructorProducesExpectedEvent 验证内置消息构造函数的事件类型与载荷。
func TestMessageConstructorProducesExpectedEvent(t *testing.T) {
	cases := []struct {
		name            string
		msg             *Message
		expectedEvent   string
		expectedKeyword string
	}{
		{name: "连接成功", msg: NewConnectedMessage("client-1"), expectedEvent: "connect", expectedKeyword: "client-1"},
		{name: "心跳", msg: NewHeartbeatMessage(), expectedEvent: "heartbeat", expectedKeyword: "Ping"},
		{name: "连接关闭", msg: NewClosedMessage(), expectedEvent: "close", expectedKeyword: "closed"},
		{name: "服务关闭", msg: NewShutdownMessage(), expectedEvent: "close", expectedKeyword: "shutting down"},
		{name: "自定义事件", msg: NewMessageWithEvent("custom", []byte("payload")), expectedEvent: "custom", expectedKeyword: "payload"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.msg.Event != testCase.expectedEvent {
				t.Fatalf("Event = %q, want %q", testCase.msg.Event, testCase.expectedEvent)
			}
			if !strings.Contains(string(testCase.msg.Data), testCase.expectedKeyword) {
				t.Fatalf("Data = %q, 缺少关键字 %q", testCase.msg.Data, testCase.expectedKeyword)
			}
		})
	}
}

// TestConnectedMessageProducesValidJSON 验证即使客户端 ID 含引号与反斜杠，
// 连接消息仍是合法 JSON（internal 使用 %q 转义）。
func TestConnectedMessageProducesValidJSON(t *testing.T) {
	clientId := `weird"id\with"quotes`
	msg := NewConnectedMessage(clientId)

	var decoded struct {
		Time     string `json:"time"`
		Message  string `json:"message"`
		ClientId string `json:"client_id"`
	}
	if err := json.Unmarshal(msg.Data, &decoded); err != nil {
		t.Fatalf("连接消息不是合法 JSON: %v (%q)", err, msg.Data)
	}

	if decoded.ClientId != clientId {
		t.Fatalf("client_id = %q, want %q", decoded.ClientId, clientId)
	}
	if decoded.Time == "" || decoded.Message == "" {
		t.Fatalf("连接消息字段不完整: %+v", decoded)
	}
}

// TestMessageConcurrentWriteIsSafe 验证同一消息被多协程并发写入时不会产生数据竞争，
// 且每个协程都能拿到完整报文。
func TestMessageConcurrentWriteIsSafe(t *testing.T) {
	msg := &Message{Id: "1", Event: "concurrent", Data: []byte("payload")}

	const goroutines = 16
	var waitGroup sync.WaitGroup
	results := make([]string, goroutines)

	for i := 0; i < goroutines; i++ {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()

			recorder := httptest.NewRecorder()
			if err := msg.Write(response.NewWriter(recorder)); err != nil {
				t.Errorf("并发写入失败: %v", err)
				return
			}

			results[index] = recorder.Body.String()
		}(i)
	}

	waitGroup.Wait()

	expected := "id: 1\nevent: concurrent\ndata: payload\n\n"
	for i, result := range results {
		if result != expected {
			t.Fatalf("第 %d 个协程的报文 = %q, want %q", i, result, expected)
		}
	}
}

// BenchmarkMessageWrite 测量消息写入（含格式化缓存与响应刷新）的耗时。
func BenchmarkMessageWrite(b *testing.B) {
	msg := &Message{Id: "1", Event: "bench", Data: []byte("payload")}
	if !msg.format() {
		b.Fatal("消息格式化失败")
	}

	recorder := httptest.NewRecorder()
	resp := response.NewWriter(recorder)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := msg.Write(resp); err != nil {
			b.Fatalf("写入失败: %v", err)
		}
	}
}

// BenchmarkMessageFormat 测量首次格式化（含缓冲池复用）的耗时。
func BenchmarkMessageFormat(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		msg := &Message{Id: "1", Event: "bench", Data: []byte("payload")}
		if !msg.format() {
			b.Fatal("消息格式化失败")
		}
	}
}
