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

// WriteResponseHeader 写入 SSE 响应头
func WriteResponseHeader(resp response.Writer) {
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

func ServeHTTP(resp response.Writer, req *http.Request, handle func(ctx context.Context) <-chan *Message) error {
	// 写入 SSE 响应头
	WriteResponseHeader(resp)

	// 创建自定义的、可控制的 Context
	ctx, cancel := context.WithCancelCause(req.Context())
	defer cancel(nil)

	// 发送连接消息
	NewConnectedMessage("-1")

	// 处理 SSE 请求
	msgChan := handle(ctx)
	for {
		select {
		// 正常接收数据
		case message, ok := <-msgChan:
			// 检查数据有效性
			if message == nil || len(message.Data) == 0 {
				return nil
			}

			// 发送数据
			if err := message.Write(resp); err != nil {
				cancel(err)
				return err
			}

			// 通道关闭
			if !ok {
				return NewClosedMessage().Write(resp)
			}

		// 客户端断开连接
		case <-req.Context().Done():
			cancel(context.Cause(req.Context()))
			return req.Context().Err()

		// 上下文被取消
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	}
}

// Client 定义 SSE 客户端连接的数据结构
type Client struct {
	Id              string
	close           chan struct{}
	message         chan *Message
	isClosed        int32
	writeTimeout    int64
	heartbeatPeriod int64

	lock sync.Mutex
}

func NewClient(id string) *Client {
	return &Client{
		Id:              id,
		close:           make(chan struct{}),
		message:         make(chan *Message, 8),
		writeTimeout:    int64(100 * time.Millisecond),
		heartbeatPeriod: int64(30 * time.Second),
	}
}

func (own *Client) ServeHTTP(resp response.Writer, req *http.Request) error {
	// 写入 SSE 响应头
	WriteResponseHeader(resp)

	// 创建自定义的、可控制的 Context
	ctx, cancel := context.WithCancelCause(req.Context())
	defer cancel(nil)

	// 发送连接消息
	NewConnectedMessage(own.Id)

	// 启动心跳机制
	heartbeat := time.NewTicker(time.Duration(own.heartbeatPeriod))
	defer func() {
		heartbeat.Stop()
		_ = own.Close()
	}()

	for {
		select {
		// 心跳
		case <-heartbeat.C:
			if err := NewHeartbeatMessage().Write(resp); err != nil {
				cancel(err)
				return err
			}

		// 正常接收数据
		case message, ok := <-own.message:
			// 检查数据有效性
			if message == nil || len(message.Data) == 0 {
				return nil
			}

			// 发送数据
			if err := message.Write(resp); err != nil {
				cancel(err)
				return err
			}

			// 通道关闭
			if !ok {
				return NewClosedMessage().Write(resp)
			}

		// 客户端断开连接
		case <-req.Context().Done():
			cancel(context.Cause(req.Context()))
			return req.Context().Err()

		// 上下文被取消
		case <-ctx.Done():
			close(own.message)
			return context.Cause(ctx)

		// 管理器主动关闭
		case <-own.close:
			err := errors.New("leego: sse: manager is closed")
			cancel(err)
			return err
		}
	}
}

// WithWriteTimeout 设置写超时时间
func (own *Client) WithWriteTimeout(timeout time.Duration) *Client {
	if timeout > 0 {
		atomic.StoreInt64(&own.writeTimeout, int64(timeout))
	}

	return own
}

// WithHeartbeatPeriod 设置连接发送心跳的周期（默认30s）
func (own *Client) WithHeartbeatPeriod(period time.Duration) *Client {
	if period > 0 {
		atomic.StoreInt64(&own.heartbeatPeriod, int64(period))
	}

	return own
}

// WriteMessage 写入事件消息
func (own *Client) WriteMessage(msg *Message) error {
	// 若消息无效，则直接返回
	if msg == nil || len(msg.Data) == 0 {
		return nil
	}

	// 已经关闭，直接返回
	if atomic.CompareAndSwapInt32(&own.isClosed, 1, 1) {
		return fmt.Errorf("leego: sse: client is closed: %s", own.Id)
	}

	// 发送消息给消息通道
	select {
	case own.message <- msg:
		return nil
	case <-time.After(time.Duration(atomic.LoadInt64(&own.writeTimeout))):
		return fmt.Errorf("leego: sse: client message channel is full: %s", own.Id)
	}
}

func (own *Client) Close() error {
	own.lock.Lock()
	defer own.lock.Unlock()

	if atomic.CompareAndSwapInt32(&own.isClosed, 1, 1) {
		return nil
	}

	close(own.message)
	close(own.close)
	
	return nil
}
