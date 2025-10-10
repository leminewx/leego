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

const (
	_ERR_FORMAT_NOT_FOUND_CLIENT = "leego: websocket: client not found '%s'"
)

var (
	errClientSendChanFull       = errors.New("leego: websocket: client send channel is full")
	errManagerClosed            = errors.New("leego: websocket: manager is closed")
	errManagerShuttingDown      = errors.New("leego: websocket: manager is shutting down")
	errManagerBroadcastChanFull = errors.New("leego: websocket: manager broadcast channel is full")
)

// Manager 定义 WebSocket 连接管理器的数据结构
type Manager struct {
	clients    *sync.Map          // 连接管理
	upgrader   *upgrader.Upgrader // WebSocket 升级器
	register   chan *Client       // 注册通道
	unregister chan *Client       // 注销通道
	broadcast  chan []byte        // 广播通道
	closeChan  chan struct{}      // 关闭通道
	isClosed   int32              // 管理器标志位
	once       sync.Once          // 确保管理器只关闭一次
}

// NewManager 新建一个 WebSocket 连接管理器
func NewManager(config *upgrader.Config) *Manager {
	return &Manager{
		clients:    &sync.Map{},
		upgrader:   upgrader.NewUpgrader(config),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan []byte, 256),
		closeChan:  make(chan struct{}),
	}
}

// HandleWebSocket 处理 WebSocket 连接请求
func (own *Manager) HandleWebSocket(resp response.Writer, req *http.Request) error {
	// 检测是否已关闭
	if atomic.LoadInt32(&own.isClosed) == 1 {
		return errManagerClosed
	}

	// 升级为 WebSocket 连接
	conn, err := own.upgrader.Upgrade(req, resp, nil)
	if err != nil {
		return err
	}

	// 创建 WebSocket 客户端，并注册到管理器
	client := NewClient(generateClientId(), "", conn)
	select {
	// 成功注册
	case own.register <- client:

	// 管理器已关闭，拒绝新连接
	case <-own.closeChan:
		_ = client.Close()
		return errManagerShuttingDown
	}

	// 启动读写协程
	ctx, cancel := context.WithCancel(context.Background())

	// 使用 WaitGroup 确保读写循环都退出后才清理资源
	var wg sync.WaitGroup

	// 异步读
	wg.Go(func() {
		own.readPump(ctx, client)
		cancel() // 读循环退出时取消上下文
	})

	// 异步写
	wg.Go(func() {
		own.writePump(ctx, client)
	})

	// 在后台等待两个循环都退出后执行清理
	go func(wg *sync.WaitGroup) {
		// 等待读写循环都退出
		wg.Wait()

		select {
		// 确保客户端被注销
		case own.unregister <- client:

		// 管理器已关闭，直接清理
		case <-own.closeChan:
			_ = client.Close()
		}
	}(&wg)

	// 构建欢迎消息
	msg := NewWelcomeMessage(client.Id)
	bytes, _ := json.Marshal(&msg)

	select {
	// 发送欢迎消息
	case client.SendChan <- bytes:
	// 上下文已取消，不发送欢迎消息
	case <-ctx.Done():
	}

	return nil
}

// readPump 从客户端读取消息
func (own *Manager) readPump(ctx context.Context, client *Client) {
	defer func() {
		if err := recover(); err != nil {
			// TODO 待处理
			log.Printf("leego: websocket: recovered from readPump panic: %v", err)
		}
	}()

	for {
		select {
		// 上下文取消
		case <-ctx.Done():
			return

		// 客户端关闭
		case <-client.CloseChan:
			return

		default:
			// 阻塞读取数据
			opCode, data, err := client.Connection.Read()
			if err != nil {
				if !upgrader.IsCloseError(err, upgrader.CLOSE_CODE_NORMAL_CLOSURE, upgrader.CLOSE_CODE_GOING_AWAY) {
					// TODO 待处理
					log.Printf("leego: websocket: read error: %v", err)
				}
				return
			}

			// 处理消息
			switch opCode {
			// 处理文本消息
			case upgrader.OP_CODE_TEXT:
				own.handleMessage(client, data)
			}
		}
	}
}

// writePump 向客户端写入消息
func (own *Manager) writePump(ctx context.Context, client *Client) {
	defer func() {
		if err := recover(); err != nil {
			// TODO 待处理
			log.Printf("leego: websocket: recovered from writePump panic: %v", err)
		}
	}()

	// 心跳计时器
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		// 接收消息
		case message, ok := <-client.SendChan:
			if !ok {
				// 通道关闭，发送关闭帧
				_ = client.Connection.Write(upgrader.OP_CODE_CLOSE, []byte{})
				return
			}

			if err := client.Connection.Write(upgrader.OP_CODE_TEXT, message); err != nil {
				log.Printf("leego: websocket: write error: %v", err)
				return
			}

		// 发送心跳
		case <-ticker.C:
			// 构建心跳消息
			msg := NewPingMessage()
			bytes, _ := json.Marshal(&msg)

			// 使用非阻塞方式发送心跳，避免阻塞正常消息
			select {
			// 心跳发送成功
			case client.SendChan <- bytes:

			// 上下文取消
			case <-ctx.Done():
				return

			// 客户端已关闭
			case <-client.CloseChan:
				return

			// 心跳发送失败，通道已满，记录但不中断连接
			default:
				log.Printf("leego: websocket: heartbeat skipped due to full channel for client %s", client.Id)
			}

		// 上下文取消
		case <-ctx.Done():
			return

		// 客户端已关闭
		case <-client.CloseChan:
			return
		}
	}
}

// handleMessage 处理客户端消息
func (own *Manager) handleMessage(client *Client, data []byte) {
	// 反序列化消息
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		// TODO 待处理
		log.Printf("leego: websocket: invalid message format from client %s: %v", client.Id, err)

		// 构建错误消息
		msg := NewSimpleMessage("invalid_message_format", map[string]any{
			"message": "Invalid message format",
		})
		bytes, _ := json.Marshal(&msg)

		// 发送错误消息
		select {
		case client.SendChan <- bytes:
		default:
			// TODO 待处理
			// 通道已满，无法发送错误消息
		}

		return
	}

	// 处理消息
	switch msg.Type {
	// 响应心跳
	case MSG_TYPE_PING:
		// 构建心跳响应消息
		msg := NewPongMessage()
		bytes, _ := json.Marshal(&msg)

		// 发送心跳响应消息
		select {
		case client.SendChan <- bytes:
		default:
			// 通道已满，无法发送 pong
		}

	// 广播消息
	case MSG_TYPE_BROADCAST:
		if err := own.Broadcast(MSG_TYPE_BROADCAST, msg.Data); err != nil {
			// TODO 待处理
			log.Printf("leego: websocket: broadcast failed: %v", err)
		}

	// 私聊消息
	case MSG_TYPE_PRIVATE:
		if msg.To != "" {
			if err := own.SendToClient(msg.To, MSG_TYPE_PRIVATE, map[string]any{
				"from":    client.Id,
				"message": msg.Data,
			}); err != nil {
				// TODO 待处理
				log.Printf("leego: websocket: private message failed to client %s: %v", msg.To, err)
			}
		}

	// 客户端身份识别
	case MSG_TYPE_IDENTIFY:
		// 读取用户信息
		switch data := msg.Data.(type) {
		case string:
			client.UserId = data
		case map[string]any:
			// 获取用户ID
			userIdKey, userId := getUserId(data)
			if userIdKey == "" || userId == "" {
				return
			}

			// 保存用户信息
			for key, val := range data {
				if key != userIdKey {
					client.Metadata[key] = val
				}
			}
		default:
			return
		}

		client.Metadata["user_id"] = client.UserId
		client.Metadata["identified_at"] = time.Now()

		// 构建确认消息
		ackMsg := NewSimpleMessage("identification_success", "Identification successful")
		ackBytes, _ := json.Marshal(&ackMsg)

		// 发送确认消息
		select {
		case client.SendChan <- ackBytes:
		default:
			// TODO 待处理
			// 通道已满，无法发送确认消息
		}

	default:
		// TODO 待处理
		log.Printf("leego: websocket: unknown message type from client %s: %s", client.Id, msg.Type)

		// 构建未知类型的错误消息
		msg := NewSimpleMessage("unknown_message_type", map[string]any{
			"message": fmt.Sprintf("Unknown message type: %s", msg.Type),
		})
		bytes, _ := json.Marshal(&msg)

		// 发送错误消息
		select {
		case client.SendChan <- bytes:
		default:
			// TODO 待处理
			// 通道已满，无法发送确认消息
		}
	}
}

// Start 启动 WebSocket 管理器
func (own *Manager) Start(ctx context.Context) {
	// 退出时，确保关闭所有通道
	defer func() {

		own.once.Do(func() {
			close(own.closeChan)
			close(own.register)
			close(own.unregister)
			close(own.broadcast)
		})
	}()

	for {
		select {
		// 注册
		case client := <-own.register:
			if client == nil {
				continue
			}

			own.clients.Store(client.Id, client)
			// TODO 待处理
			log.Printf("leego: websocket: client connected: %s, total: %d", client.Id, own.CountConnections())

		// 注销
		case client := <-own.unregister:
			if client == nil {
				continue
			}

			if _, loaded := own.clients.LoadAndDelete(client.Id); loaded {
				client.Close()
				// TODO 待处理
				log.Printf("leego: websocket: client disconnected: %s, total: %d", client.Id, own.CountConnections())
			}

		// 广播
		case message := <-own.broadcast:
			if message == nil {
				continue
			}

			own.clients.Range(func(key, value any) bool {
				client := value.(*Client)
				select {
				// 消息发送成功
				case client.SendChan <- message:
				// 客户端已关闭，从管理器中移除
				case <-client.CloseChan:
					own.clients.Delete(key)
				// 通道已满，记录但继续处理其他客户端
				default:
					// TODO 待处理
					log.Printf("leego: websocket: broadcast message dropped for client %s (channel full)", client.Id)
				}

				return true
			})

		// 退出
		case <-ctx.Done():
			own.shutdown()
			return
		}
	}
}

// shutdown 优雅关闭所有客户端连接
func (own *Manager) shutdown() {
	var wg sync.WaitGroup
	own.clients.Range(func(key, value any) bool {
		client := value.(*Client)
		wg.Go(func() {
			// 构建关闭通知消息
			msg := NewSimpleMessage("server_shutdown", "Server is shutting down")
			bytes, _ := json.Marshal(&msg)

			// 发送关闭通知
			select {
			case client.SendChan <- bytes:
			case <-time.After(100 * time.Millisecond):
			}

			// 给客户端一点时间处理关闭消息，然后直接关闭
			time.Sleep(100 * time.Millisecond)
			client.Close()
		})

		own.clients.Delete(key)
		return true
	})

	// 等待所有客户端关闭完成
	wg.Wait()
	log.Printf("leego: websocket: manager shutdown completed")
}

// Broadcast 广播消息给所有客户端
func (own *Manager) Broadcast(messageType MessageType, data any) error {
	// 构建消息
	msg := &Message{
		Type:      messageType,
		Timestamp: time.Now().Unix(),
		Data:      data,
	}

	// 序列化消息体
	bytes, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	// 广播消息
	select {
	case own.broadcast <- bytes:
		return nil
	case <-own.closeChan:
		return errManagerShuttingDown
	default:
		return errManagerBroadcastChanFull
	}
}

// SendToClient 发送消息给特定客户端
func (own *Manager) SendToClient(clientId string, messageType MessageType, data any) error {
	// 获取客户端
	value, ok := own.clients.Load(clientId)
	if !ok {
		return fmt.Errorf(_ERR_FORMAT_NOT_FOUND_CLIENT, clientId)
	}

	// 构建消息
	msg := Message{
		Type:      messageType,
		Timestamp: time.Now().Unix(),
		Data:      data,
	}

	// 序列化消息体
	bytes, err := json.Marshal(&msg)
	if err != nil {
		return err
	}

	// 发送消息
	client := value.(*Client)
	select {
	case client.SendChan <- bytes:
		return nil
	case <-client.CloseChan:
		own.clients.Delete(clientId)
		return fmt.Errorf(_ERR_FORMAT_NOT_FOUND_CLIENT, clientId)
	default:
		return errClientSendChanFull
	}
}

// CountConnections 统计当前连接数
func (own *Manager) CountConnections() int {
	var count int
	own.clients.Range(func(_, _ any) bool {
		count++
		return true
	})

	return count
}

// GetClients 获取所有客户端信息
func (own *Manager) GetClients() []map[string]any {
	var clients []map[string]any
	own.clients.Range(func(key, value any) bool {
		client := value.(*Client)
		clients = append(clients, map[string]any{
			"id":           client.Id,
			"user_id":      client.UserId,
			"connected_at": client.ConnectedAt,
			"metadata":     client.Metadata,
		})

		return true
	})

	return clients
}

// CloseClient 关闭指定客户端连接
func (own *Manager) CloseClient(clientId string) error {
	if value, ok := own.clients.Load(clientId); ok {
		err := value.(*Client).Close()
		own.clients.Delete(clientId)
		return err
	}

	return fmt.Errorf(_ERR_FORMAT_NOT_FOUND_CLIENT, clientId)
}

var userIdNames = []string{"user_id", "userid", "userId", "userID", "uid", "id"}

// getUserId 获取用户 ID
func getUserId(data map[string]any) (key, val string) {
	for _, name := range userIdNames {
		if userId, ok := data[name]; ok {
			if uid, ok := userId.(string); ok {
				return name, uid
			}
		}
	}

	return "", ""
}

// generateClientId 生成客户端 ID
func generateClientId() string {
	return fmt.Sprintf("client_%d_%d", time.Now().Unix(), time.Now().UnixNano()%10000)
}
