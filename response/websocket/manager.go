// Package websocket 提供基于 upgrader 的连接管理能力。
//
// 典型用法：
//
//	manager := websocket.NewManager(nil)
//	defer manager.Close()
//
//	engine.GET("/ws", func(ctx *leego.Context) {
//		_ = manager.Serve(ctx.Response, ctx.Request)
//	})
//
//	manager.Broadcast(websocket.MSG_TYPE_BROADCAST, "hello")
//
// 并发模型：
//   - 每个连接独占一个读协程（Serve 所在的 HTTP 处理协程）与一个写协程；
//   - 连接表使用 sync.Map，广播与定向发送都只做非阻塞投递；
//   - 关闭只关闭信号通道，不关闭发送队列，从根本上避免“向已关闭通道发送”的 panic。
package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"gitee.com/leminewx/leego/response"
	"gitee.com/leminewx/leego/response/websocket/upgrader"
)

// 管理器默认参数。
const (
	// _DEFAULT_PING_INTERVAL 为服务端主动发送 PING 控制帧的间隔。
	_DEFAULT_PING_INTERVAL = 30 * time.Second
	// _DEFAULT_FLUSH_WAIT 为关闭前等待关闭通知刷出连接的时间。
	_DEFAULT_FLUSH_WAIT = 50 * time.Millisecond
	// _ERR_FORMAT_NOT_FOUND_CLIENT 为“客户端不存在”的错误模板。
	_ERR_FORMAT_NOT_FOUND_CLIENT = "leego: websocket: client not found '%s'"
)

// 管理器错误定义。
var (
	// errClientSendChanFull 表示客户端发送队列已满。
	errClientSendChanFull = errors.New("leego: websocket: client send channel is full")
	// errClientSendChanClosed 表示客户端已关闭。
	errClientSendChanClosed = errors.New("leego: websocket: client is closed")
	// ErrManagerIsClosed 表示管理器已关闭。
	ErrManagerIsClosed = errors.New("leego: websocket: manager is closed")
	// ErrManagerShuttingDown 表示管理器正在关闭。
	ErrManagerShuttingDown = errors.New("leego: websocket: manager is shutting down")
)

/*
Manager 管理全部 WebSocket 连接。

Manager 是并发安全的，可被多个 HTTP 处理协程同时使用；
创建后即可直接 Serve，无需额外的 Start。
*/
type Manager struct {
	// clients 保存全部在线连接，键为客户端 ID。
	clients sync.Map
	// upgrader 为共享的升级器（不可变，可并发复用）。
	upgrader *upgrader.Upgrader

	// pingInterval 为心跳间隔。
	pingInterval time.Duration

	// closeChan 在管理器关闭时被关闭。
	closeChan chan struct{}
	// closeOnce 保证关闭逻辑只执行一次。
	closeOnce sync.Once
	// isClosed 表示管理器是否已关闭。
	isClosed atomic.Bool
}

/*
NewManager 创建连接管理器。

config 为 nil 时使用 upgrader 的默认配置；
传入配置会被复制，后续修改原对象不会影响管理器。
*/
func NewManager(config *upgrader.Config) *Manager {
	return &Manager{
		upgrader:     upgrader.New(config),
		pingInterval: _DEFAULT_PING_INTERVAL,
		closeChan:    make(chan struct{}),
	}
}

// SetPingInterval 设置心跳间隔，非正数将被忽略。
func (own *Manager) SetPingInterval(interval time.Duration) {
	if interval <= 0 {
		return
	}

	own.pingInterval = interval
}

// GetPingInterval 返回心跳间隔。
func (own *Manager) GetPingInterval() time.Duration {
	return own.pingInterval
}

/*
Serve 升级并服务一个 WebSocket 连接，直到连接结束。

该方法必须在 HTTP 处理函数中同步调用（内部会阻塞在读循环上）：
返回 nil 表示连接正常结束，返回错误表示升级失败或管理器已关闭。
*/
func (own *Manager) Serve(resp response.Writer, req *http.Request) error {
	if own.IsClosed() {
		return ErrManagerIsClosed
	}

	connection, err := own.upgrader.Upgrade(req, resp, nil)
	if err != nil {
		return err
	}

	client := NewClient(generateClientId(), "", connection)
	own.clients.Store(client.Id, client)

	// 升级期间管理器可能刚好被关闭，这里复核一次，避免连接泄漏。
	if own.IsClosed() {
		own.clients.Delete(client.Id)
		_ = client.Close()
		return ErrManagerIsClosed
	}

	defer func() {
		own.clients.Delete(client.Id)
		_ = client.Close()
	}()

	// 欢迎消息：失败不影响连接，写协程会负责后续退出。
	if payload, marshalErr := json.Marshal(NewWelcomeMessage(client.Id)); marshalErr == nil {
		_ = client.WriteMessage(payload)
	}

	go own.writePump(client)
	own.readPump(client)

	return nil
}

// Start 启动管理器并阻塞直到 ctx 结束或管理器关闭。
//
// 新模型不依赖中央调度协程，因此 Start 是可选的；
// 需要把管理器生命周期绑定到应用上下文时可以使用它。
func (own *Manager) Start(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}

	select {
	case <-ctx.Done():
		_ = own.Close()
	case <-own.closeChan:
	}
}

// readPump 持续读取客户端消息，返回即表示连接结束。
func (own *Manager) readPump(client *Client) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("leego: websocket: recovered from read loop panic: %v", recovered)
		}
	}()

	for {
		opCode, payload, err := client.Connection.Read()
		if err != nil {
			if !upgrader.IsCloseError(err, upgrader.CLOSE_CODE_NORMAL_CLOSURE, upgrader.CLOSE_CODE_GOING_AWAY) {
				log.Printf("leego: websocket: read error from client %s: %v", client.Id, err)
			}

			return
		}

		switch opCode {
		case upgrader.OP_CODE_TEXT:
			own.handleMessage(client, payload)
		case upgrader.OP_CODE_BINARY:
			// 二进制帧暂不参与业务协议，直接忽略但保持连接可用。
		}
	}
}

// writePump 持续向客户端写入消息与心跳，返回即表示写端结束。
func (own *Manager) writePump(client *Client) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("leego: websocket: recovered from write loop panic: %v", recovered)
		}
	}()

	ticker := time.NewTicker(own.pingInterval)
	defer ticker.Stop()

	for {
		if client.IsClosed() {
			return
		}

		select {
		case payload := <-client.SendChan:
			if err := client.Connection.Write(upgrader.OP_CODE_TEXT, payload); err != nil {
				log.Printf("leego: websocket: write error to client %s: %v", client.Id, err)
				return
			}

		case <-ticker.C:
			// 直接发送 PING 控制帧：控制帧不占用发送队列，
			// 也不会被业务消息的背压影响。
			if err := client.Connection.WriteControl(upgrader.OP_CODE_PING, nil); err != nil {
				return
			}

		case <-client.CloseChan:
			return

		case <-own.closeChan:
			return
		}
	}
}

// handleMessage 处理客户端发来的文本消息。
func (own *Manager) handleMessage(client *Client, payload []byte) {
	var message Message
	if err := json.Unmarshal(payload, &message); err != nil {
		own.reply(client, NewSimpleMessage("invalid_message_format", map[string]any{
			"message": "Invalid message format",
		}))

		return
	}

	switch message.Type {
	case MSG_TYPE_PING:
		own.reply(client, NewPongMessage())

	case MSG_TYPE_BROADCAST:
		if err := own.Broadcast(MSG_TYPE_BROADCAST, message.Data); err != nil {
			log.Printf("leego: websocket: broadcast failed: %v", err)
		}

	case MSG_TYPE_PRIVATE:
		if message.To == "" {
			return
		}

		if err := own.SendToClient(message.To, MSG_TYPE_PRIVATE, map[string]any{
			"from":    client.Id,
			"message": message.Data,
		}); err != nil {
			log.Printf("leego: websocket: private message to %s failed: %v", message.To, err)
		}

	case MSG_TYPE_IDENTIFY:
		own.identifyClient(client, message.Data)

	default:
		own.reply(client, NewSimpleMessage("unknown_message_type", map[string]any{
			"message": fmt.Sprintf("Unknown message type: %s", message.Type),
		}))
	}
}

// identifyClient 处理客户端身份识别消息。
func (own *Manager) identifyClient(client *Client, data any) {
	switch payload := data.(type) {
	case string:
		client.SetUserId(payload)

	case map[string]any:
		userIdKey, userId := getUserId(payload)
		if userIdKey == "" || userId == "" {
			own.reply(client, NewSimpleMessage("identification_failed", map[string]any{
				"message": "Missing user id",
			}))

			return
		}

		client.SetUserId(userId)
		for key, value := range payload {
			if key != userIdKey {
				client.SetMetadata(key, value)
			}
		}

	default:
		return
	}

	client.SetMetadata("user_id", client.GetUserId())
	client.SetMetadata("identified_at", time.Now())

	own.reply(client, NewSimpleMessage("identification_success", "Identification successful"))
}

// reply 把消息序列化后投递到客户端发送队列（失败时静默丢弃）。
func (own *Manager) reply(client *Client, message *Message) {
	if message == nil {
		return
	}

	payload, err := json.Marshal(message)
	if err != nil {
		return
	}

	_ = client.WriteMessage(payload)
}

// Broadcast 向全部在线客户端广播消息。
//
// 广播采用非阻塞投递：队列已满的客户端会丢弃本条消息并记录日志，
// 慢客户端不会阻塞其它客户端。
func (own *Manager) Broadcast(messageType MessageType, data any) error {
	if own.IsClosed() {
		return ErrManagerShuttingDown
	}

	payload, err := json.Marshal(&Message{
		Type:      messageType,
		Timestamp: time.Now().Unix(),
		Data:      data,
	})
	if err != nil {
		return err
	}

	own.clients.Range(func(key, value any) bool {
		client, isClient := value.(*Client)
		if !isClient || client == nil {
			return true
		}

		if writeErr := client.WriteMessage(payload); writeErr != nil {
			if errors.Is(writeErr, ErrClientIsClosed) {
				own.clients.Delete(key)
			} else {
				log.Printf("leego: websocket: broadcast dropped for client %s: %v", client.Id, writeErr)
			}
		}

		return true
	})

	return nil
}

// SendToClient 向指定客户端发送消息。
func (own *Manager) SendToClient(clientId string, messageType MessageType, data any) error {
	if own.IsClosed() {
		return ErrManagerShuttingDown
	}

	value, isLoaded := own.clients.Load(clientId)
	if !isLoaded {
		return fmt.Errorf(_ERR_FORMAT_NOT_FOUND_CLIENT, clientId)
	}

	client, isClient := value.(*Client)
	if !isClient || client == nil {
		return fmt.Errorf(_ERR_FORMAT_NOT_FOUND_CLIENT, clientId)
	}

	payload, err := json.Marshal(&Message{
		Type:      messageType,
		Timestamp: time.Now().Unix(),
		Data:      data,
	})
	if err != nil {
		return err
	}

	if writeErr := client.WriteMessage(payload); writeErr != nil {
		if errors.Is(writeErr, ErrClientIsClosed) {
			own.clients.Delete(clientId)
			return fmt.Errorf(_ERR_FORMAT_NOT_FOUND_CLIENT, clientId)
		}

		return writeErr
	}

	return nil
}

// CountConnections 返回当前在线连接数。
func (own *Manager) CountConnections() int {
	count := 0
	own.clients.Range(func(key, value any) bool {
		count++
		return true
	})

	return count
}

// GetClients 返回全部客户端的状态快照。
func (own *Manager) GetClients() []ClientSnapshot {
	clients := make([]ClientSnapshot, 0, own.CountConnections())
	own.clients.Range(func(key, value any) bool {
		client, isClient := value.(*Client)
		if isClient && client != nil {
			clients = append(clients, client.GetSnapshot())
		}

		return true
	})

	return clients
}

// GetClient 返回指定客户端的状态快照。
func (own *Manager) GetClient(clientId string) (ClientSnapshot, bool) {
	value, isLoaded := own.clients.Load(clientId)
	if !isLoaded {
		return ClientSnapshot{}, false
	}

	client, isClient := value.(*Client)
	if !isClient || client == nil {
		return ClientSnapshot{}, false
	}

	return client.GetSnapshot(), true
}

// CloseClient 关闭并移除指定客户端。
func (own *Manager) CloseClient(clientId string) error {
	value, isLoaded := own.clients.LoadAndDelete(clientId)
	if !isLoaded {
		return fmt.Errorf(_ERR_FORMAT_NOT_FOUND_CLIENT, clientId)
	}

	client, isClient := value.(*Client)
	if !isClient || client == nil {
		return fmt.Errorf(_ERR_FORMAT_NOT_FOUND_CLIENT, clientId)
	}

	return client.Close()
}

// IsClosed 判断管理器是否已关闭。
func (own *Manager) IsClosed() bool {
	return own.isClosed.Load()
}

// GetDoneChan 返回在管理器关闭时被关闭的只读通道。
func (own *Manager) GetDoneChan() <-chan struct{} {
	return own.closeChan
}

/*
Close 关闭管理器并断开全部连接。

关闭顺序：置位关闭标志 → 投递关闭通知 → 等待短暂刷出 → 逐个关闭连接。
该方法幂等，重复调用只会执行一次清理。
*/
func (own *Manager) Close() error {
	own.closeOnce.Do(func() {
		own.isClosed.Store(true)
		close(own.closeChan)

		clients := make([]*Client, 0, own.CountConnections())
		own.clients.Range(func(key, value any) bool {
			client, isClient := value.(*Client)
			if isClient && client != nil {
				clients = append(clients, client)
			}

			return true
		})

		// 先投递关闭通知，让客户端有机会感知服务端下线。
		// 没有在线连接时跳过等待，避免空管理器关闭也付出固定延迟。
		notification, err := json.Marshal(NewSimpleMessage("server_shutdown", "Server is shutting down"))
		if err == nil && len(clients) > 0 {
			for _, client := range clients {
				_ = client.WriteMessage(notification)
			}

			time.Sleep(_DEFAULT_FLUSH_WAIT)
		}

		for _, client := range clients {
			own.clients.Delete(client.Id)
			_ = client.Close()
		}
	})

	return nil
}

// 用户 ID 候选字段名，按优先级从高到低排列。
var userIdNames = []string{"user_id", "userid", "userId", "userID", "uid", "id"}

// getUserId 从消息数据中提取用户 ID，返回命中的字段名与取值。
func getUserId(data map[string]any) (key, value string) {
	for _, name := range userIdNames {
		rawValue, isExisted := data[name]
		if !isExisted {
			continue
		}

		if userId, isString := rawValue.(string); isString && userId != "" {
			return name, userId
		}
	}

	return "", ""
}

// clientIdCounter 为客户端 ID 的自增序列，保证同一纳秒内创建的连接也不会重名。
var clientIdCounter atomic.Uint64

// generateClientId 生成全局唯一的客户端 ID。
func generateClientId() string {
	sequence := clientIdCounter.Add(1)

	return fmt.Sprintf("client_%d_%d", time.Now().UnixNano(), sequence)
}
