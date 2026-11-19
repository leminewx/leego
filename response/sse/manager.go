package sse

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"gitee.com/leminewx/leego/response"
)

// 管理器默认参数。
const (
	// _DEFAULT_ERROR_BUFFER_SIZE 为错误通道的缓冲长度。
	_DEFAULT_ERROR_BUFFER_SIZE = 16
	// _DEFAULT_FLUSH_WAIT 为关闭前等待关闭消息刷出连接的时间。
	_DEFAULT_FLUSH_WAIT = 50 * time.Millisecond
	// _DEFAULT_BROADCAST_CONCURRENCY 限制一次广播同时存在的投递协程数。
	//
	// 直接为每个客户端起一个协程，在万级连接时瞬间放大成万个协程；
	// 限制并发数后吞吐仍能饱和，而内存与调度开销有了上界。
	_DEFAULT_BROADCAST_CONCURRENCY = 64
)

// 管理器错误定义。
var (
	// ErrManagerIsClosed 表示管理器已关闭，无法再接受新连接或消息。
	ErrManagerIsClosed = errors.New("sse: manager is closed")
	// ErrClientAlreadyExists 表示同一标识的客户端已存在。
	ErrClientAlreadyExists = errors.New("sse: client already exists")
)

/*
Manager 管理一组 SSE 客户端连接。

并发模型：

  - Register 使用 sync.Map.LoadOrStore 保证同一 ID 的原子注册；
  - 每个连接的响应写入都由独立的 serve 协程独占（单写者），
    其它协程通过 SendTo / Broadcast 投递到各客户端自己的队列；
  - Close 是可重入的：由 closeOnce 保证只执行一次真正的清理逻辑。
*/
type Manager struct {
	// configMu 保护下面三个可热更新的参数。
	configMu sync.RWMutex
	// messageChanNum 为新建客户端的消息队列长度。
	messageChanNum int
	// writeTimeout 为新建客户端的投递超时。
	writeTimeout time.Duration
	// heartbeatPeriod 为新建客户端的心跳周期。
	heartbeatPeriod time.Duration

	// clients 保存全部在线客户端，键为客户端 ID。
	clients sync.Map

	// errorChan 汇总连接异常，供监控协程消费。
	errorChan chan error
	// errorMu 保证“检查关闭标志 + 发送”是原子的，避免向已关闭通道发送。
	errorMu sync.Mutex
	// isErrorChanClosed 表示 errorChan 已关闭。
	isErrorChanClosed bool

	// closeChan 在管理器关闭时被关闭，用于唤醒等待者。
	closeChan chan struct{}
	// closeOnce 保证清理逻辑只执行一次。
	closeOnce sync.Once
	// isClosed 表示管理器是否已关闭。
	isClosed atomic.Bool
}

// NewManager 创建 SSE 连接管理器。
//
// 默认消息队列长度 16、投递超时 300 毫秒、心跳周期 30 秒、错误通道缓冲 16。
func NewManager() *Manager {
	return &Manager{
		messageChanNum:  16,
		writeTimeout:    _DEFAULT_WRITE_TIMEOUT,
		heartbeatPeriod: _DEFAULT_HEARTBEAT_PERIOD,
		errorChan:       make(chan error, _DEFAULT_ERROR_BUFFER_SIZE),
		closeChan:       make(chan struct{}),
	}
}

// SetMessageCacheNumber 设置新建客户端的消息队列长度，非正数将被忽略。
func (own *Manager) SetMessageCacheNumber(number int) {
	if number <= 0 {
		return
	}

	own.configMu.Lock()
	own.messageChanNum = number
	own.configMu.Unlock()
}

// SetWriteTimeout 设置新建客户端的投递超时，非正数将被忽略。
func (own *Manager) SetWriteTimeout(timeout time.Duration) {
	if timeout <= 0 {
		return
	}

	own.configMu.Lock()
	own.writeTimeout = timeout
	own.configMu.Unlock()
}

// SetHeartbeatPeriod 设置新建客户端的心跳周期，非正数将被忽略。
func (own *Manager) SetHeartbeatPeriod(period time.Duration) {
	if period <= 0 {
		return
	}

	own.configMu.Lock()
	own.heartbeatPeriod = period
	own.configMu.Unlock()
}

// getMessageCacheNumber 返回当前的消息队列长度配置。
func (own *Manager) getMessageCacheNumber() int {
	own.configMu.RLock()
	number := own.messageChanNum
	own.configMu.RUnlock()

	if number <= 0 {
		return _DEFAULT_MESSAGE_CACHE_NUMBER
	}

	return number
}

// getWriteTimeout 返回当前的投递超时配置。
func (own *Manager) getWriteTimeout() time.Duration {
	own.configMu.RLock()
	timeout := own.writeTimeout
	own.configMu.RUnlock()

	if timeout <= 0 {
		return _DEFAULT_WRITE_TIMEOUT
	}

	return timeout
}

// getHeartbeatPeriod 返回当前的心跳周期配置。
func (own *Manager) getHeartbeatPeriod() time.Duration {
	own.configMu.RLock()
	period := own.heartbeatPeriod
	own.configMu.RUnlock()

	if period <= 0 {
		return _DEFAULT_HEARTBEAT_PERIOD
	}

	return period
}

/*
Serve 注册并阻塞服务一个新连接，直到连接结束。

该方法必须在 HTTP 处理函数中同步调用：
它会在内部创建客户端、登记到管理器、持续推送消息，
并在客户端断开或管理器关闭后自动注销。

返回 nil 表示连接正常结束；返回错误表示请求上下文被取消、写入失败或注册失败。
*/
func (own *Manager) Serve(resp response.Writer, req *http.Request, clientId string) error {
	if resp == nil || req == nil {
		return errors.New("sse: nil response writer or request")
	}

	if own.IsClosed() {
		return ErrManagerIsClosed
	}

	client := NewClient(clientId, own.getMessageCacheNumber())
	client.SetWriteTimeout(own.getWriteTimeout())
	client.SetHeartbeatPeriod(own.getHeartbeatPeriod())

	if _, loaded := own.clients.LoadOrStore(clientId, client); loaded {
		return fmt.Errorf("%w: %s", ErrClientAlreadyExists, clientId)
	}

	// 注册成功后立即复核关闭状态，避免与 Close 竞争导致连接泄漏。
	if own.IsClosed() {
		own.clients.Delete(clientId)
		return ErrManagerIsClosed
	}

	defer own.Unregister(clientId)

	return client.serve(resp, req, nil)
}

// Unregister 注销并关闭指定客户端。
//
// 客户端不存在时返回 nil，因此可以被安全地重复调用。
func (own *Manager) Unregister(clientId string) error {
	value, isLoaded := own.clients.LoadAndDelete(clientId)
	if !isLoaded {
		return nil
	}

	client, isClient := value.(*Client)
	if !isClient || client == nil {
		return nil
	}

	return client.Close()
}

// SendTo 向指定客户端发送消息。
func (own *Manager) SendTo(clientId string, msg *Message) error {
	if own.IsClosed() {
		return ErrManagerIsClosed
	}

	value, isLoaded := own.clients.Load(clientId)
	if !isLoaded {
		return fmt.Errorf("sse: not found client: %s", clientId)
	}

	client, isClient := value.(*Client)
	if !isClient || client == nil {
		return fmt.Errorf("sse: not found client: %s", clientId)
	}

	return client.WriteMessage(msg)
}

// Broadcast 向所有在线客户端广播消息。
//
// 广播是并发投递的：单个客户端阻塞不会拖慢其它客户端；
// 并发度受 _DEFAULT_BROADCAST_CONCURRENCY 限制，避免连接数大时协程爆炸。
// 投递失败的客户端会被自动注销，失败原因可通过 GetErrorChan 获取。
func (own *Manager) Broadcast(msg *Message) error {
	if msg == nil || !msg.format() {
		return nil
	}

	if own.IsClosed() {
		return ErrManagerIsClosed
	}

	own.visitConcurrently(own.snapshotClients(), func(client *Client) {
		own.deliver(client, msg)
	})

	return nil
}

// BroadcastTo 向指定客户端集合广播消息，不存在的客户端会被跳过。
func (own *Manager) BroadcastTo(clientIds []string, msg *Message) error {
	if msg == nil || !msg.format() {
		return nil
	}

	if own.IsClosed() {
		return ErrManagerIsClosed
	}

	clients := make([]*Client, 0, len(clientIds))
	for _, clientId := range clientIds {
		value, isLoaded := own.clients.Load(clientId)
		if !isLoaded {
			continue
		}

		if client, isClient := value.(*Client); isClient && client != nil {
			clients = append(clients, client)
		}
	}

	own.visitConcurrently(clients, func(client *Client) {
		own.deliver(client, msg)
	})

	return nil
}

// snapshotClients 返回当前在线客户端的快照。
func (own *Manager) snapshotClients() []*Client {
	clients := make([]*Client, 0, own.CountClients())
	own.clients.Range(func(key, value any) bool {
		if client, isClient := value.(*Client); isClient && client != nil {
			clients = append(clients, client)
		}

		return true
	})

	return clients
}

// visitConcurrently 以有限并发对客户端集合执行访问。
//
// 信号量在循环内获取，形成天然背压：同时运行的协程数不会超过上限。
func (own *Manager) visitConcurrently(clients []*Client, visit func(*Client)) {
	if len(clients) == 0 {
		return
	}

	slots := make(chan struct{}, _DEFAULT_BROADCAST_CONCURRENCY)
	var waitGroup sync.WaitGroup

	for _, client := range clients {
		slots <- struct{}{}
		waitGroup.Go(func() {
			defer func() { <-slots }()
			visit(client)
		})
	}

	waitGroup.Wait()
}

// deliver 向单个客户端投递消息，失败时记录错误并注销该客户端。
func (own *Manager) deliver(client *Client, msg *Message) {
	if err := client.WriteMessage(msg); err != nil {
		own.sendError(err)

		if unregisterErr := own.Unregister(client.Id); unregisterErr != nil {
			own.sendError(unregisterErr)
		}
	}
}

// CountClients 返回当前在线客户端数量。
func (own *Manager) CountClients() int {
	count := 0
	own.clients.Range(func(key, value any) bool {
		count++
		return true
	})

	return count
}

// IsClientConnected 判断指定客户端是否在线。
func (own *Manager) IsClientConnected(clientId string) bool {
	_, isLoaded := own.clients.Load(clientId)
	return isLoaded
}

// GetAllClientIds 返回全部在线客户端标识，结果按字典序升序排列。
func (own *Manager) GetAllClientIds() []string {
	clientIds := make([]string, 0, own.CountClients())
	own.clients.Range(func(key, value any) bool {
		if clientId, isString := key.(string); isString {
			clientIds = append(clientIds, clientId)
		}

		return true
	})

	sort.Strings(clientIds)

	return clientIds
}

// sendError 向错误通道投递错误，通道已满或已关闭时静默丢弃。
//
// 采用“检查 + 发送”同锁的方式，确保不会向已关闭的通道发送而 panic。
func (own *Manager) sendError(err error) {
	if err == nil {
		return
	}

	own.errorMu.Lock()
	defer own.errorMu.Unlock()

	if own.isErrorChanClosed {
		return
	}

	select {
	case own.errorChan <- err:
	default:
	}
}

// GetErrorChan 返回只读错误通道。
//
// 消费方应在收到关闭信号（通道关闭或 GetDoneChan 被关闭）后退出循环。
func (own *Manager) GetErrorChan() <-chan error {
	return own.errorChan
}

// GetDoneChan 返回在管理器关闭时被关闭的只读通道，便于与 select 组合使用。
func (own *Manager) GetDoneChan() <-chan struct{} {
	return own.closeChan
}

// IsClosed 判断管理器是否已关闭。
func (own *Manager) IsClosed() bool {
	return own.isClosed.Load()
}

/*
Close 关闭管理器并释放全部连接。

关闭顺序：
 1. 置位关闭标志并关闭 Done 通道，阻止新的接入与广播；
 2. 向所有客户端投递“服务器关闭”消息，并等待它们把消息写出去（最多 1 秒）；
 3. 关闭每个客户端；
 4. 关闭错误通道。

该方法幂等，重复调用只会执行一次清理。
*/
func (own *Manager) Close() error {
	var closeErr error

	own.closeOnce.Do(func() {
		own.isClosed.Store(true)
		close(own.closeChan)

		// 1. 通知所有客户端服务器即将关闭。
		// 同样限制并发：关闭时也不能一次拉起上万个协程。
		shutdownMessage := NewShutdownMessage()
		own.visitConcurrently(own.snapshotClients(), func(client *Client) {
			_ = client.WriteMessage(shutdownMessage)
		})

		// 2. 给写协程一点时间把关闭消息真正刷到连接上。
		time.Sleep(_DEFAULT_FLUSH_WAIT)

		// 3. 关闭并摘除所有客户端。
		var (
			errorMu     sync.Mutex
			closeErrors []error
		)
		own.clients.Range(func(key, value any) bool {
			client, isClient := value.(*Client)
			if isClient && client != nil {
				if err := client.Close(); err != nil {
					errorMu.Lock()
					closeErrors = append(closeErrors, err)
					errorMu.Unlock()
				}
			}

			own.clients.Delete(key)
			return true
		})

		// 4. 关闭错误通道。
		own.errorMu.Lock()
		own.isErrorChanClosed = true
		close(own.errorChan)
		own.errorMu.Unlock()

		if len(closeErrors) > 0 {
			closeErr = fmt.Errorf("sse: errors occurred during close: %v", closeErrors)
		}
	})

	return closeErr
}
