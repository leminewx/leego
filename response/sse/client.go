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

// 客户端默认参数。
const (
	// _DEFAULT_MESSAGE_CACHE_NUMBER 为消息通道的默认缓冲长度。
	_DEFAULT_MESSAGE_CACHE_NUMBER = 8
	// _DEFAULT_WRITE_TIMEOUT 为向消息通道投递消息的默认超时时间。
	_DEFAULT_WRITE_TIMEOUT = 300 * time.Millisecond
	// _DEFAULT_HEARTBEAT_PERIOD 为默认心跳周期。
	_DEFAULT_HEARTBEAT_PERIOD = 30 * time.Second
)

// 客户端错误定义。
var (
	// ErrClientIsClosed 表示客户端已经关闭。
	ErrClientIsClosed = errors.New("sse: client is closed")
	// ErrMessageChannelIsFull 表示消息通道已满，投递超时。
	ErrMessageChannelIsFull = errors.New("sse: client message channel is full")
)

/*
Client 表示一个 SSE 连接。

并发模型（重要）：

  - 写响应只可能发生在 serve 所在的协程，属于“单写者”模型，
    因此 http.ResponseWriter 不需要额外加锁；
  - 其它协程只能通过 WriteMessage 把消息投递到带缓冲的 messageChan，
    绝不直接写响应；
  - Close 只关闭 closeChan，永不关闭 messageChan，
    从而彻底避免“向已关闭通道发送”导致的 panic。
*/
type Client struct {
	// Id 为客户端标识，用于 SendTo / Unregister 等定向操作。
	Id string
	// messageChan 为内部消息队列，只由 serve 协程消费。
	messageChan chan *Message
	// closeChan 在客户端关闭时被关闭，用于唤醒所有阻塞中的投递者。
	closeChan chan struct{}
	// closeOnce 保证 closeChan 只被关闭一次。
	closeOnce sync.Once
	// configMu 保护下面两个可热更新的时间参数。
	configMu sync.RWMutex
	// writeTimeout 为投递消息到 messageChan 的超时时间。
	writeTimeout time.Duration
	// heartbeatPeriod 为心跳发送周期。
	heartbeatPeriod time.Duration
	// isClosed 表示客户端是否已关闭。
	isClosed atomic.Bool
	// isWrittenHeader 表示响应头是否已写入。
	isWrittenHeader atomic.Bool
}

// NewClient 创建 SSE 客户端。
//
// 参数：
//   - id：客户端标识符，可为空字符串（内部会自动生成占位标识）；
//   - messageCacheNumber：内部消息队列的缓冲长度，小于 1 时使用默认值 8。
//
// 返回的客户端需要由 serve（或 Manager.Serve）驱动才会真正写入响应。
func NewClient(id string, messageCacheNumber int) *Client {
	if messageCacheNumber < 1 {
		messageCacheNumber = _DEFAULT_MESSAGE_CACHE_NUMBER
	}

	return &Client{
		Id:              id,
		messageChan:     make(chan *Message, messageCacheNumber),
		closeChan:       make(chan struct{}),
		writeTimeout:    _DEFAULT_WRITE_TIMEOUT,
		heartbeatPeriod: _DEFAULT_HEARTBEAT_PERIOD,
	}
}

// SetWriteTimeout 设置投递消息的超时时间，非正数将被忽略。
func (own *Client) SetWriteTimeout(timeout time.Duration) {
	if timeout <= 0 {
		return
	}

	own.configMu.Lock()
	own.writeTimeout = timeout
	own.configMu.Unlock()
}

// GetWriteTimeout 返回投递消息的超时时间。
func (own *Client) GetWriteTimeout() time.Duration {
	own.configMu.RLock()
	timeout := own.writeTimeout
	own.configMu.RUnlock()
	return timeout
}

// SetHeartbeatPeriod 设置心跳周期，非正数将被忽略。
func (own *Client) SetHeartbeatPeriod(period time.Duration) {
	if period <= 0 {
		return
	}

	own.configMu.Lock()
	own.heartbeatPeriod = period
	own.configMu.Unlock()
}

// GetHeartbeatPeriod 返回心跳周期；未设置时返回默认值 30 秒。
func (own *Client) GetHeartbeatPeriod() time.Duration {
	own.configMu.RLock()
	period := own.heartbeatPeriod
	own.configMu.RUnlock()

	if period <= 0 {
		return _DEFAULT_HEARTBEAT_PERIOD
	}

	return period
}

// IsClosed 判断客户端是否已关闭。
func (own *Client) IsClosed() bool {
	return own.isClosed.Load()
}

// Close 关闭客户端。
//
// 该方法是幂等的，可被任意协程重复调用。
// 实现刻意不关闭 messageChan：投递方可能在 Close 之后仍持有 messageChan 的引用，
// 关闭它会导致发送方 panic；关闭 closeChan 已足以唤醒并结束 serve 协程。
func (own *Client) Close() error {
	own.closeOnce.Do(func() {
		own.isClosed.Store(true)
		close(own.closeChan)
	})

	return nil
}

// WriteResponseHeader 写入 SSE 响应头。
//
// 该方法是幂等的：只有第一次调用会写入响应头并立刻刷新，
// 后续调用不会重复写状态码（避免 "superfluous WriteHeader" 告警）。
func (own *Client) WriteResponseHeader(resp response.Writer) {
	if !own.isWrittenHeader.CompareAndSwap(false, true) {
		return
	}

	header := resp.Header()
	header.Set(headers.CONTENT_TYPE, "text/event-stream; charset=utf-8")
	header.Set(headers.CACHE_CONTROL, "no-cache")
	header.Set(headers.CONNECTION, "keep-alive")
	// 关闭 Nginx 等反向代理的响应缓冲，否则事件会被攒到缓冲满才下发。
	header.Set("X-Accel-Buffering", "no")

	resp.WriteHeader(http.StatusOK)
	resp.Flush()
}

// WriteMessage 投递一条消息到客户端内部队列。
//
// 该方法并发安全，返回 nil 表示消息已入队（或消息本身无效被忽略）。
// 队列已满且超过写超时、或客户端已关闭时返回错误，
// 调用方通常据此判定客户端失联并调用 Close。
func (own *Client) WriteMessage(msg *Message) error {
	if msg == nil || !msg.format() {
		return nil
	}

	if own.IsClosed() {
		return fmt.Errorf("%w: %s", ErrClientIsClosed, own.Id)
	}

	timer := time.NewTimer(own.GetWriteTimeout())
	defer timer.Stop()

	select {
	case own.messageChan <- msg:
		return nil
	case <-timer.C:
		return fmt.Errorf("%w: %s", ErrMessageChannelIsFull, own.Id)
	case <-own.closeChan:
		return fmt.Errorf("%w: %s", ErrClientIsClosed, own.Id)
	}
}

// serve 是客户端唯一的写协程。
//
// events 为可选的业务事件通道，为 nil 时该分支永久阻塞（即被 select 忽略）。
// 返回值语义：
//   - nil：连接被正常关闭（客户端关闭、事件通道关闭、请求结束）；
//   - 非 nil：写入失败或请求上下文被取消。
func (own *Client) serve(resp response.Writer, req *http.Request, events <-chan *Message) error {
	own.WriteResponseHeader(resp)

	requestContext := req.Context()

	if err := NewConnectedMessage(own.Id).Write(resp); err != nil {
		return fmt.Errorf("sse: failed to write connected message: %w", err)
	}

	heartbeat := time.NewTicker(own.GetHeartbeatPeriod())
	defer heartbeat.Stop()

	for {
		select {
		// 客户端断开连接
		case <-requestContext.Done():
			return requestContext.Err()

		// 客户端被主动关闭
		case <-own.closeChan:
			_ = NewClosedMessage().Write(resp)
			return nil

		// 内部队列消息
		case msg := <-own.messageChan:
			if err := writeMessage(resp, msg); err != nil {
				return err
			}

		// 业务事件消息（events 为 nil 时本分支永久阻塞）
		case msg, isOpen := <-events:
			if !isOpen {
				_ = NewClosedMessage().Write(resp)
				return nil
			}

			if err := writeMessage(resp, msg); err != nil {
				return err
			}

		// 心跳
		case <-heartbeat.C:
			if err := NewHeartbeatMessage().Write(resp); err != nil {
				return fmt.Errorf("sse: failed to write heartbeat: %w", err)
			}
		}
	}
}

// writeMessage 忽略无效消息后写入响应，并在写入后立即刷新。
func writeMessage(resp response.Writer, msg *Message) error {
	if msg == nil {
		return nil
	}

	return msg.Write(resp)
}

/*
ServeEventStream 直接以“事件流函数”的方式提供 SSE，一次性完成建连与推送。

参数：
  - resp：响应写入器；
  - req：当前请求，用于感知客户端断开与服务关闭；
  - event：事件生产函数，返回只读消息通道；返回的通道由生产方负责关闭；
  - messageCacheNumber：内部队列长度，小于 1 时使用默认值。

该函数是阻塞的：会一直占用调用方协程直到连接结束，
因此必须直接在 HTTP 处理函数中调用，不要放到后台协程里。
*/
func ServeEventStream(resp response.Writer, req *http.Request, event func(ctx context.Context) <-chan *Message, messageCacheNumber int) error {
	if req == nil {
		return errors.New("sse: nil request")
	}

	client := NewClient("", messageCacheNumber)

	var events <-chan *Message
	if event != nil {
		events = event(req.Context())
	}

	err := client.serve(resp, req, events)
	_ = client.Close()

	return err
}
