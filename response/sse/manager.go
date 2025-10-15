package sse

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"gitee.com/leminewx/leego/response"
)

// Manager 定义 SSE 客户端连接管理器的数据结构
type Manager struct {
	clients         sync.Map // map[string]*Client
	register        chan *Client
	unregister      chan *Client
	broadcast       chan *Message
	quit            chan struct{}
	writeTimeout    int64
	heartbeatPeriod int64
	closed          int32
	lock            sync.RWMutex
}

// NewManager 新建一个的 SSE 连接管理器
func NewManager() *Manager {
	manager := &Manager{
		clients:         sync.Map{},
		broadcast:       make(chan *Message, 128),
		register:        make(chan *Client),
		unregister:      make(chan *Client),
		quit:            make(chan struct{}),
		writeTimeout:    int64(300 * time.Millisecond),
		heartbeatPeriod: int64(30 * time.Second),
	}

	go manager.run()
	return manager
}

// run 运行管理器的主循环
func (own *Manager) run() {
	for {
		select {
		// 注册
		case client, ok := <-own.register:
			own.clients.Store(client.Id, client)
			if !ok {

			}

		// 注销
		case client, ok := <-own.unregister:
			if c, ok := own.clients.Load(client.Id); ok {
				close(c.(*Client).Message)
				own.clients.Delete(client.Id)
			}

			if !ok {

			}

		// 广播
		case message, ok := <-own.broadcast:
			own.clients.Range(func(key, value any) bool {
				_ = value.(*Client).WriteMessage(message)
				return true
			})

			if !ok {

			}

		// 退出
		case <-own.quit:
			return
		}
	}
}

// WithWriteTimeout 设置写超时时间
func (own *Manager) WithWriteTimeout(timeout time.Duration) *Manager {
	if timeout > 0 {
		atomic.StoreInt64(&own.writeTimeout, int64(timeout))
	}

	return own
}

// WithHeartbeatPeriod 设置连接发送心跳的周期（默认30s）
func (own *Manager) WithHeartbeatPeriod(period time.Duration) *Manager {
	if period > 0 {
		atomic.StoreInt64(&own.heartbeatPeriod, int64(period))
	}

	return own
}

// Register 注册新的SSE客户端
func (own *Manager) Register(resp response.Writer, req *http.Request, clientId string) error {
	own.lock.RLock()
	defer own.lock.RUnlock()

	// 管理器已关闭
	if atomic.CompareAndSwapInt32(&own.closed, 1, 1) {
		return errors.New("leego: sse: manager is closed")
	}

	// 创建并注册客户端
	client := NewClient(clientId).
		WithWriteTimeout(time.Duration(own.writeTimeout)).
		WithHeartbeatPeriod(time.Duration(own.heartbeatPeriod))
	own.register <- client

	// 处理请求响应
	err := client.ServeHTTP(resp, req)
	own.Unregister(clientId)
	return err
}

// Unregister 注销 SSE 客户端
func (own *Manager) Unregister(clientId string) {
	if client, ok := own.clients.Load(clientId); ok {
		close(client.(*Client).Close)
		own.unregister <- client.(*Client)
	}
}

// SendTo 向指定客户端发送消息
func (own *Manager) SendTo(clientId string, message *Message) error {
	if client, ok := own.clients.Load(clientId); ok {
		return client.(*Client).WriteMessage(message)
	}

	return fmt.Errorf("leego: sse: not found client: %s", clientId)
}

// Broadcast 向所有客户端广播消息
func (own *Manager) Broadcast(message *Message) error {
	if atomic.CompareAndSwapInt32(&own.closed, 0, 0) {
		select {
		case own.broadcast <- message:
			return nil
		case <-own.quit:
			return errors.New("leego: sse: server is shutting down")
		}
	}
}

// BroadcastTo 向指定客户端组广播消息
func (own *Manager) BroadcastTo(clientIds []string, message *Message) {
	for _, clientId := range clientIds {
		if client, ok := own.clients.Load(clientId); ok {
			_ = client.(*Client).WriteMessage(message)
		}
	}
}

// CountClients 获取当前连接的客户端数量
func (own *Manager) CountClients() int {
	count := 0
	own.clients.Range(func(key, value any) bool {
		count++
		return true
	})

	return count
}

// IsClientConnected 检查客户端是否连接
func (own *Manager) IsClientConnected(clientId string) bool {
	_, ok := own.clients.Load(clientId)
	return ok
}

// GetAllClientIds 获取所有连接的客户端Id
func (own *Manager) GetAllClientIds() []string {
	var clients []string
	own.clients.Range(func(key, value any) bool {
		clients = append(clients, key.(string))
		return true
	})

	return clients
}

// Close 关闭 SSE 管理器
func (own *Manager) Close() error {
	own.lock.Lock()
	defer own.lock.Unlock()

	// 已经关闭，直接返回
	if atomic.CompareAndSwapInt32(&own.closed, 0, 0) {
		return nil
	}

	// 向所有客户端发送关闭消息
	err := own.Broadcast(&Message{
		Event: "shutdown",
		Data:  []byte(fmt.Sprintf(_MSG_SERVER_SHUTDOWN, time.Now().Format(time.DateTime))),
	})

	// 关闭所有客户端连接
	own.clients.Range(func(key, value any) bool {
		close(value.(*Client).close)
		own.clients.Delete(key)
		return true
	})

	// 发送退出信号
	close(own.quit)

	// 给一点时间让消息发送完成
	time.Sleep(300 * time.Millisecond)

	// 关闭通道
	close(own.register)
	close(own.unregister)
	close(own.broadcast)

	return err
}
