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
	errorChan       chan error
	writeTimeout    time.Duration
	heartbeatPeriod time.Duration
	isClosed        int32
	lock            sync.RWMutex
}

// NewManager 新建一个的 SSE 连接管理器
func NewManager() *Manager {
	return &Manager{
		clients:         sync.Map{},
		errorChan:       make(chan error, 8),
		writeTimeout:    300 * time.Millisecond,
		heartbeatPeriod: 30 * time.Second,
	}
}

// WithWriteTimeout 设置写超时时间
func (own *Manager) WithWriteTimeout(timeout time.Duration) *Manager {
	if timeout > 0 {
		own.lock.Lock()
		own.writeTimeout = timeout
		own.lock.Unlock()
	}

	return own
}

// WithHeartbeatPeriod 设置连接发送心跳的周期（默认30s）
func (own *Manager) WithHeartbeatPeriod(period time.Duration) *Manager {
	if period > 0 {
		own.lock.Lock()
		own.heartbeatPeriod = period
		own.lock.Unlock()
	}

	return own
}

// Register 注册新的SSE客户端
func (own *Manager) Register(resp response.Writer, req *http.Request, clientId string) (*Client, error) {
	// 创建客户端
	client := NewClient(clientId).
		WithWriteTimeout(own.writeTimeout).
		WithHeartbeatPeriod(own.heartbeatPeriod)

	own.lock.RLock()
	defer own.lock.RUnlock()

	// 管理器已关闭
	if own.IsClosed() {
		return nil, errors.New("leego: sse: manager is closed")
	}

	// 检查客户端是否已存在
	if _, ok := own.clients.Load(clientId); !ok {
		own.clients.Store(clientId, client)
		go func() {
			defer func() {
				// 确保在发生 panic 时也能注销客户端
				if err := recover(); err != nil {
					own.sendError(errors.New(fmt.Sprintln(err)))
				}

				// 注销客户端
				if err := own.Unregister(clientId); err != nil {
					own.sendError(err)
				}
			}()

			// 异步处理
			if err := client.serve(resp, req, nil); err != nil {
				own.sendError(err)
			}
		}()

		return client, nil
	}

	return nil, fmt.Errorf("leego: sse: client already exists: %s", clientId)
}

// Unregister 注销 SSE 客户端
func (own *Manager) Unregister(clientId string) error {
	if client, ok := own.clients.LoadAndDelete(clientId); ok {
		return client.(*Client).Close()
	}

	return nil
}

// SendTo 向指定客户端发送消息
func (own *Manager) SendTo(clientId string, message *Message) error {
	if own.IsClosed() {
		return errors.New("leego: sse: server is closed")
	}

	if client, ok := own.clients.Load(clientId); ok {
		return client.(*Client).WriteMessage(message)
	}

	return fmt.Errorf("leego: sse: not found client: %s", clientId)
}

// Broadcast 向所有客户端广播消息
func (own *Manager) Broadcast(message *Message) error {
	if own.IsClosed() {
		return errors.New("leego: sse: server is closed")
	}

	var wg sync.WaitGroup
	own.clients.Range(func(key, value any) bool {
		// 异步广播
		wg.Go(func() {
			client := value.(*Client)

			// 如果广播失败，可能是客户端已断开
			if err := client.WriteMessage(message); err != nil {
				own.sendError(err)

				// 尝试注销该客户端
				if err = own.Unregister(client.Id); err != nil {
					own.sendError(err)
				}
			}
		})

		return true
	})
	wg.Wait()

	return nil
}

// BroadcastTo 向指定客户端组广播消息
func (own *Manager) BroadcastTo(clientIds []string, message *Message) error {
	if own.IsClosed() {
		return errors.New("leego: sse: server is closed")
	}

	var wg sync.WaitGroup
	for _, clientId := range clientIds {
		client, ok := own.clients.Load(clientId)
		if !ok {
			continue
		}

		// 异步广播
		wg.Go(func() {
			c := client.(*Client)

			// 如果广播失败，可能是客户端已断开
			if err := c.WriteMessage(message); err != nil {
				own.sendError(err)

				// 尝试注销该客户端
				if err = own.Unregister(c.Id); err != nil {
					own.sendError(err)
				}
			}
		})
	}
	wg.Wait()

	return nil
}

// CountClients 获取当前连接的客户端数量
func (own *Manager) CountClients() (count int) {
	if own.IsClosed() {
		return
	}

	own.clients.Range(func(key, value any) bool {
		count++
		return true
	})

	return count
}

// IsClientConnected 检查客户端是否连接
func (own *Manager) IsClientConnected(clientId string) bool {
	if own.IsClosed() {
		return false
	}

	_, ok := own.clients.Load(clientId)
	return ok
}

// GetAllClientIds 获取所有连接的客户端Id
func (own *Manager) GetAllClientIds() []string {
	if own.IsClosed() {
		return nil
	}

	var clients []string
	own.clients.Range(func(key, value any) bool {
		clients = append(clients, key.(string))
		return true
	})

	return clients
}

// sendError 发送错误消息给管理器
func (own *Manager) sendError(err error) {
	select {
	case own.errorChan <- err:
	default:
	}
}

// GetErrorChan 获取错误通道
func (own *Manager) GetErrorChan() <-chan error {
	return own.errorChan
}

// IsClosed 判断连接是否已关闭
func (own *Manager) IsClosed() bool {
	return atomic.LoadInt32(&own.isClosed) == 1
}

// Close 关闭 SSE 管理器
func (own *Manager) Close() error {
	own.lock.Lock()
	defer own.lock.Unlock()

	// 已经关闭，则直接返回
	if own.IsClosed() {
		return nil
	}

	// 向所有客户端发送关闭消息
	err := own.Broadcast(&Message{
		Event: "shutdown",
		Data:  []byte(fmt.Sprintf(_MSG_SERVER_SHUTDOWN, time.Now().Format(time.DateTime))),
	})

	// 关闭标志位置为1
	atomic.StoreInt32(&own.isClosed, 1)

	// 关闭所有客户端连接
	msg := &Message{
		Event: "shutdown",
		Data:  []byte(fmt.Sprintf(_MSG_SERVER_SHUTDOWN, time.Now().Format(time.DateTime))),
	}

	var wg sync.WaitGroup
	own.clients.Range(func(key, value any) bool {
		wg.Go(func() {
			// 发送关闭消息
			client := value.(*Client)
			if err := client.WriteMessage(msg); err != nil {
				own.sendError(err)
			}

			// 关闭客户端
			if err := client.Close(); err != nil {
				own.sendError(err)
			}

			// 注销客户端
			own.clients.Delete(key)
		})

		return true
	})
	wg.Wait()

	// 给一点时间让消息发送完成
	time.Sleep(300 * time.Millisecond)

	close(own.errorChan)

	return err
}
