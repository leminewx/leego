package websocket

import (
	"sync"
	"time"

	"gitee.com/leminewx/leego/response/websocket/upgrader"
)

// _DEFAULT_SEND_BUFFER_SIZE 为客户端发送队列的默认长度。
const _DEFAULT_SEND_BUFFER_SIZE = 256

// 客户端错误定义。
var (
	// ErrClientIsClosed 表示客户端已关闭，无法继续投递消息。
	ErrClientIsClosed = errClientSendChanClosed
	// ErrSendBufferIsFull 表示发送队列已满，消息被丢弃。
	ErrSendBufferIsFull = errClientSendChanFull
)

/*
Client 表示一个已建立的 WebSocket 客户端连接。

并发约定（重要）：

  - SendChan 只由写协程消费，其它协程一律通过 WriteMessage 投递，
    绝不直接 close(SendChan) —— 关闭发送队列会让正在发送的协程 panic；
  - CloseChan 在关闭时被关闭，用于广播退出信号；
  - UserId / Metadata 属于可变状态，统一由 stateMu 保护，
    外部请通过 SetUserId / GetUserId / SetMetadata / GetMetadata 访问。
*/
type Client struct {
	// Id 为客户端标识。
	Id string
	// SendChan 为待发送的数据队列，只由写协程消费。
	SendChan chan []byte
	// CloseChan 在客户端关闭时被关闭。
	CloseChan chan struct{}
	// Connection 为底层 WebSocket 连接。
	Connection *upgrader.Conn
	// ConnectedAt 为连接建立时间（只读）。
	ConnectedAt time.Time

	// closeOnce 保证关闭逻辑只执行一次。
	closeOnce sync.Once
	// stateMu 保护 UserId 与 Metadata。
	stateMu sync.RWMutex
	// UserId 为业务用户标识，建议通过 GetUserId 读取。
	UserId string
	// Metadata 为业务自定义元数据，建议通过 GetMetadata 读取。
	Metadata map[string]any
}

// ClientSnapshot 为客户端状态的只读快照，可安全地跨协程传递或序列化为 JSON。
type ClientSnapshot struct {
	// Id 为客户端标识。
	Id string `json:"id"`
	// UserId 为业务用户标识。
	UserId string `json:"user_id"`
	// ConnectedAt 为连接建立时间。
	ConnectedAt time.Time `json:"connected_at"`
	// Metadata 为元数据副本。
	Metadata map[string]any `json:"metadata"`
}

// NewClient 创建 WebSocket 客户端。
func NewClient(clientId, userId string, conn *upgrader.Conn) *Client {
	return &Client{
		Id:          clientId,
		UserId:      userId,
		Metadata:    make(map[string]any),
		SendChan:    make(chan []byte, _DEFAULT_SEND_BUFFER_SIZE),
		CloseChan:   make(chan struct{}),
		Connection:  conn,
		ConnectedAt: time.Now(),
	}
}

// Close 关闭客户端连接。
//
// 关闭顺序为“先广播关闭信号，再关闭底层连接”：
// 写协程会因 CloseChan 关闭而退出，从而避免在连接关闭后继续写数据。
// 该方法是幂等的。
func (own *Client) Close() error {
	var closeErr error

	own.closeOnce.Do(func() {
		close(own.CloseChan)

		if own.Connection != nil {
			closeErr = own.Connection.Close()
		}
	})

	return closeErr
}

// IsClosed 判断客户端是否已关闭。
func (own *Client) IsClosed() bool {
	select {
	case <-own.CloseChan:
		return true
	default:
		return false
	}
}

/*
WriteMessage 非阻塞地把一条文本消息放入发送队列。

队列已满时立即返回 ErrSendBufferIsFull，由调用方决定是丢弃还是断开连接；
这种“快速失败”策略可以防止慢客户端拖垮整个广播流程。
*/
func (own *Client) WriteMessage(payload []byte) error {
	if payload == nil {
		return nil
	}

	select {
	case <-own.CloseChan:
		return ErrClientIsClosed
	default:
	}

	select {
	case own.SendChan <- payload:
		return nil
	case <-own.CloseChan:
		return ErrClientIsClosed
	default:
		return ErrSendBufferIsFull
	}
}

// SetUserId 设置业务用户标识。
func (own *Client) SetUserId(userId string) {
	own.stateMu.Lock()
	own.UserId = userId
	own.stateMu.Unlock()
}

// GetUserId 返回业务用户标识。
func (own *Client) GetUserId() string {
	own.stateMu.RLock()
	userId := own.UserId
	own.stateMu.RUnlock()

	return userId
}

// SetMetadata 写入一个元数据键值。
func (own *Client) SetMetadata(key string, value any) {
	if key == "" {
		return
	}

	own.stateMu.Lock()
	if own.Metadata == nil {
		own.Metadata = make(map[string]any)
	}
	own.Metadata[key] = value
	own.stateMu.Unlock()
}

// GetMetadata 返回元数据的副本。
func (own *Client) GetMetadata() map[string]any {
	own.stateMu.RLock()
	defer own.stateMu.RUnlock()

	metadata := make(map[string]any, len(own.Metadata))
	for key, value := range own.Metadata {
		metadata[key] = value
	}

	return metadata
}

// GetSnapshot 返回客户端状态快照。
func (own *Client) GetSnapshot() ClientSnapshot {
	snapshot := ClientSnapshot{
		Id:          own.Id,
		ConnectedAt: own.ConnectedAt,
	}

	own.stateMu.RLock()
	snapshot.UserId = own.UserId
	snapshot.Metadata = make(map[string]any, len(own.Metadata))
	for key, value := range own.Metadata {
		snapshot.Metadata[key] = value
	}
	own.stateMu.RUnlock()

	return snapshot
}
