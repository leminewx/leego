package sse

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"gitee.com/leminewx/leego/headers"
	"gitee.com/leminewx/leego/response"
)

// Handle 处理 sse 响应
func Handle(resp response.Writer, req *http.Request, event func(ctx context.Context) <-chan *Message, messageCacheNumber int) error {
	// 创建 SSE 客户端
	client := NewClient("*", messageCacheNumber)
	// 处理 SSE 请求
	return client.serve(resp, req, event(req.Context()))
}

// Client 定义 SSE 客户端连接的数据结构
type Client struct {
	Id              string
	messageChan     chan *Message // 消息通道
	closeChan       chan struct{} // 关闭通道
	writeTimeout    time.Duration // 写超时时间
	heartbeatPeriod time.Duration // 心跳发送周期
	isClosed        int32         // 连接是否已关闭
	isWrittenHeader int32         // 是否已写入响应头
	lock            sync.RWMutex
}

/*
NewClient 创建 SSE 客户端（默认写超时300ms、心跳周期30s）
  - id: 客户端标识符
  - messageCacheNumber: 消息缓存数量
*/
func NewClient(id string, messageCacheNumber int) *Client {
	if messageCacheNumber < 1 {
		messageCacheNumber = 8
	}

	return &Client{
		Id:              id,
		messageChan:     make(chan *Message, messageCacheNumber),
		closeChan:       make(chan struct{}),
		writeTimeout:    300 * time.Millisecond,
		heartbeatPeriod: 30 * time.Second,
	}
}

// SetWriteTimeout 设置写超时时间（默认300ms）
func (own *Client) SetWriteTimeout(timeout time.Duration) {
	if timeout > 0 {
		own.lock.Lock()
		own.writeTimeout = timeout
		own.lock.Unlock()
	}
}

// SetHeartbeatPeriod 设置连接发送心跳的周期（默认30s）
func (own *Client) SetHeartbeatPeriod(period time.Duration) {
	if period > 0 {
		own.lock.Lock()
		own.heartbeatPeriod = period
		own.lock.Unlock()
	}
}

// IsClosed 判断连接是否已关闭
func (own *Client) IsClosed() bool {
	return atomic.LoadInt32(&own.isClosed) == 1
}

// WriteResponseHeader 写入 SSE 响应头
func (own *Client) WriteResponseHeader(resp response.Writer) {
	// 响应头未写入
	if atomic.CompareAndSwapInt32(&own.isWrittenHeader, 0, 1) {
		// 设置响应头
		header := resp.Header()
		header.Set(headers.CONTENT_TYPE, "text/event-stream")
		header.Set(headers.CACHE_CONTROL, "no-cache")
		header.Set(headers.CONNECTION, "keep-alive")
		header.Set("X-Accel-Buffering", "no") // 禁用 Ngix 代理缓冲

		// 设置响应状态码
		resp.WriteHeader(http.StatusOK)
		resp.Flush()
	}
}

// WriteMessage 写入事件消息
func (own *Client) WriteMessage(msg *Message) error {
	// 若消息无效，则直接返回
	if msg == nil || !msg.format() {
		return nil
	}

	// 已经关闭，直接返回
	if own.IsClosed() {
		return fmt.Errorf("sse: client is closed: %s", own.Id)
	}

	own.lock.RLock()
	writeTimeout := own.writeTimeout
	own.lock.RUnlock()

	// 发送消息给消息通道
	select {
	case own.messageChan <- msg:
		return nil
	case <-time.After(writeTimeout):
		return fmt.Errorf("sse: client message channel is full: %s", own.Id)
	case <-own.closeChan:
		return fmt.Errorf("sse: client is closing: %s", own.Id)
	}
}

// Close 主动关闭连接
func (own *Client) Close() error {
	own.lock.Lock()
	defer own.lock.Unlock()

	if atomic.CompareAndSwapInt32(&own.isClosed, 0, 1) {
		// 安全关闭通道
		select {
		case <-own.closeChan:
		default:
			close(own.closeChan)
		}

		// 给一点时间让消息发送完成
		time.Sleep(100 * time.Millisecond)

		// 关闭消息通道
		close(own.messageChan)
	}

	return nil
}

func (own *Client) serve(resp response.Writer, req *http.Request, msgChan <-chan *Message) error {
	// 写入 SSE 响应头
	own.WriteResponseHeader(resp)

	// 发送连接消息
	if err := NewConnectedMessage(own.Id).Write(resp); err != nil {
		return err
	}

	// 创建自定义的、可控制的 Context
	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()

	// 启动心跳机制
	heartbeat := time.NewTicker(own.heartbeatPeriod)
	defer func() {
		heartbeat.Stop() // 停止心跳
		_ = own.Close()  // 如果本身是因为调用 Client.Close() 主动关闭，重复调用也不影响
	}()

	for {
		select {
		// 接收外部通道消息（未过滤无效消息）
		case msg, ok := <-msgChan:
			// 消息通道关闭
			if !ok {
				return NewClosedMessage().Write(resp)
			}

			// 跳过无效数据
			if msg == nil || len(msg.Data) == 0 {
				continue
			}

			// 发送数据
			if err := msg.Write(resp); err != nil {
				return err
			}

		// 接收内部通道消息（已过滤无效消息）
		case msg, ok := <-own.messageChan:
			// 消息通道关闭
			if !ok {
				return NewClosedMessage().Write(resp)
			}

			// 发送数据
			if err := msg.Write(resp); err != nil {
				return err
			}

		// 发起心跳
		case <-heartbeat.C:
			// 发送心跳消息
			if err := NewHeartbeatMessage().Write(resp); err != nil {
				return err
			}

		// 客户端断开连接
		case <-req.Context().Done():
			return req.Context().Err()

		// 上下文被取消
		case <-ctx.Done():
			return context.Cause(ctx)

		// 管理器主动关闭
		case <-own.closeChan:
			return errors.New("sse: manager is closed")
		}
	}
}
