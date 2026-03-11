package sse

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"gitee.com/leminewx/leego/response"
)

// mockRequestWithContext 模拟带上下文的 HTTP 请求
func mockRequestWithContext(ctx context.Context) *http.Request {
	req, _ := http.NewRequest("GET", "/sse", nil)
	return req.WithContext(ctx)
}

// TestNewClient 测试客户端创建
func TestNewClient(t *testing.T) {
	client := NewClient("examples-client", 8)

	if client.Id != "examples-client" {
		t.Errorf("Expected client ID 'examples-client', got '%s'", client.Id)
	}

	if cap(client.messageChan) != 8 {
		t.Errorf("Expected message channel capacity 8, got %d", cap(client.messageChan))
	}

	if client.writeTimeout != 300*time.Millisecond {
		t.Errorf("Expected write timeout 300ms, got %v", client.writeTimeout)
	}

	if client.heartbeatPeriod != 30*time.Second {
		t.Errorf("Expected heartbeat period 30s, got %v", client.heartbeatPeriod)
	}
}

// TestWriteResponseHeader 测试响应头写入
func TestWriteResponseHeader(t *testing.T) {
	client := NewClient("examples-client", 8)
	resp := response.NewWriter(httptest.NewRecorder())

	client.WriteResponseHeader(resp)

	// 检查状态码
	if resp.GetStatusCode() != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, resp.GetStatusCode())
	}

	// 检查响应头
	expectedHeaders := map[string]string{
		"Content-Type":      "text/event-stream",
		"Cache-Control":     "no-cache",
		"Connection":        "keep-alive",
		"X-Accel-Buffering": "no",
	}

	for key, expectedValue := range expectedHeaders {
		actualValue := resp.Header().Get(key)
		if actualValue != expectedValue {
			t.Errorf("Expected header %s: %s, got %s", key, expectedValue, actualValue)
		}
	}

	// 测试重复写入响应头（应该只写入一次）
	client.WriteResponseHeader(resp)
	client.WriteResponseHeader(resp)

	// 响应头应该只设置一次
	if resp.GetStatusCode() != http.StatusOK {
		t.Error("Response header should be written only once")
	}
}

// TestWriteMessage 测试消息写入
func TestWriteMessage(t *testing.T) {
	client := NewClient("examples-client", 8)

	// 测试有效消息
	msg := &Message{Data: []byte("examples message")}
	err := client.WriteMessage(msg)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// 测试空消息（应该被忽略）
	err = client.WriteMessage(nil)
	if err != nil {
		t.Errorf("Expected nil message to be ignored, got error: %v", err)
	}

	emptyMsg := &Message{Data: []byte("")}
	err = client.WriteMessage(emptyMsg)
	if err != nil {
		t.Errorf("Expected empty message to be ignored, got error: %v", err)
	}

	// 测试关闭后的客户端
	_ = client.Close()
	err = client.WriteMessage(msg)
	if err == nil {
		t.Error("Expected error when writing to closed client")
	}
}

// TestClientClose 测试客户端关闭
func TestClientClose(t *testing.T) {
	client := NewClient("examples-client", 8)

	// 第一次关闭
	err := client.Close()
	if err != nil {
		t.Errorf("Expected no error on first close, got %v", err)
	}

	// 第二次关闭（应该无操作）
	err = client.Close()
	if err != nil {
		t.Errorf("Expected no error on second close, got %v", err)
	}

	// 检查通道是否关闭
	select {
	case _, ok := <-client.closeChan:
		if ok {
			t.Error("Expected closeChan to be closed")
		}
	default:
		t.Error("closeChan should be closed")
	}

	select {
	case _, ok := <-client.messageChan:
		if ok {
			t.Error("Expected messageChan to be closed")
		}
	default:
		t.Error("messageChan should be closed")
	}
}

// TestClientDisconnect 测试客户端断开连接
func TestClientDisconnect(t *testing.T) {
	client := NewClient("examples-client", 8)
	resp := response.NewWriter(httptest.NewRecorder())

	// 创建可取消的上下文
	ctx, cancel := context.WithCancel(context.Background())
	req := mockRequestWithContext(ctx)

	// 启动服务
	var serveErr error
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		serveErr = client.serve(resp, req, nil)
	}()

	// 模拟客户端断开连接
	cancel()

	// 等待服务结束
	wg.Wait()

	// 应该返回上下文取消错误
	if serveErr == nil {
		t.Error("Expected error when client disconnects")
	}

	if !strings.Contains(serveErr.Error(), context.Canceled.Error()) {
		t.Errorf("Expected client disconnected error, got %v", serveErr)
	}
}

// TestWriteTimeout 测试写超时
func TestWriteTimeout(t *testing.T) {
	client := NewClient("examples-client", 8)
	client.SetWriteTimeout(10 * time.Millisecond)

	// 填充消息通道使其阻塞
	for i := 0; i < 8; i++ {
		client.messageChan <- &Message{Data: []byte(fmt.Sprintf("message %d", i))}
	}

	// 尝试发送消息，应该超时
	err := client.WriteMessage(&Message{Data: []byte("should timeout")})
	if err == nil {
		t.Error("Expected timeout error when channel is full")
	}

	if !strings.Contains(err.Error(), "channel is full") {
		t.Errorf("Expected channel full error, got %v", err)
	}
}

// TestConfigurationMethods 测试配置方法
func TestConfigurationMethods(t *testing.T) {
	client := NewClient("examples-client", 8)

	// 测试写超时配置
	newTimeout := 200 * time.Millisecond
	client.SetWriteTimeout(newTimeout)
	if client.writeTimeout != newTimeout {
		t.Errorf("Expected write timeout %v, got %v", newTimeout, client.writeTimeout)
	}

	// 测试心跳周期配置
	newHeartbeat := 60 * time.Second
	client.SetHeartbeatPeriod(newHeartbeat)
	if client.heartbeatPeriod != newHeartbeat {
		t.Errorf("Expected heartbeat period %v, got %v", newHeartbeat, client.heartbeatPeriod)
	}

	// 测试无效配置（应该保持原值）
	client.SetWriteTimeout(0)
	if client.writeTimeout != newTimeout {
		t.Errorf("Expected write timeout to remain %v with invalid input, got %v", newTimeout, client.writeTimeout)
	}

	client.SetWriteTimeout(0)
	if client.heartbeatPeriod != newHeartbeat {
		t.Errorf("Expected heartbeat period to remain %v with invalid input, got %v", newHeartbeat, client.heartbeatPeriod)
	}
}
