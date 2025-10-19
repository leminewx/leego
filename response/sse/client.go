package sse

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"gitee.com/leminewx/leego/response"
)

// ServeHTTP 创建独立 SSE 响应
func ServeHTTP(resp response.Writer, req *http.Request, handle func(ctx context.Context) <-chan *Message) error {
	// 创建 SSE 客户端
	client := NewClient("*")
	// 处理 SSE 请求
	return client.serve(resp, req, handle(req.Context()))
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

func NewClient(id string) *Client {
	return &Client{
		Id:              id,
		messageChan:     make(chan *Message, 8),
		closeChan:       make(chan struct{}),
		writeTimeout:    100 * time.Millisecond,
		heartbeatPeriod: 30 * time.Second,
	}
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

			// 重置心跳超时时间
			own.lock.RLock()
			heartbeat.Reset(own.heartbeatPeriod)
			own.lock.RUnlock()

		// 客户端断开连接
		case <-req.Context().Done():
			_ = context.Cause(req.Context())
			return req.Context().Err()

		// 上下文被取消
		case <-ctx.Done():
			return context.Cause(ctx)

		// 管理器主动关闭
		case <-own.closeChan:
			err := errors.New("leego: sse: manager is closed")
			return err
		}
	}
}

// WithWriteTimeout 设置写超时时间
func (own *Client) WithWriteTimeout(timeout time.Duration) *Client {
	if timeout > 0 {
		own.lock.Lock()
		own.writeTimeout = timeout
		own.lock.Unlock()
	}

	return own
}

// WithHeartbeatPeriod 设置连接发送心跳的周期（默认30s）
func (own *Client) WithHeartbeatPeriod(period time.Duration) *Client {
	if period > 0 {
		own.lock.Lock()
		own.heartbeatPeriod = period
		own.lock.Unlock()
	}

	return own
}

// WriteResponseHeader 写入 SSE 响应头
func (own *Client) WriteResponseHeader(resp response.Writer) {
	// 响应头未写入
	if atomic.CompareAndSwapInt32(&own.isWrittenHeader, 0, 1) {
		// 设置响应头
		header := resp.Header()
		header.Set("Content-Type", "text/event-stream")
		header.Set("Cache-Control", "no-cache")
		header.Set("Connection", "keep-alive")
		header.Set("X-Accel-Buffering", "no") // 禁用代理缓冲

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
		return fmt.Errorf("leego: sse: client is closed: %s", own.Id)
	}

	// 发送消息给消息通道
	select {
	case own.messageChan <- msg:
		return nil
	case <-time.After(own.writeTimeout):
		return fmt.Errorf("leego: sse: client message channel is full: %s", own.Id)
	case <-own.closeChan:
		return fmt.Errorf("leego: sse: client is closing: %s", own.Id)
	}
}

// IsClosed 判断连接是否已关闭
func (own *Client) IsClosed() bool {
	return atomic.LoadInt32(&own.isClosed) == 1
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
