package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"gitee.com/leminewx/leego/response"
	"log"
	"net/http"
	"sync"
	"time"

	"gitee.com/leminewx/leego/response/websocket/v2/upgrader"
)

const (
	_ERR_FORMAT_NOT_FOUND_CLIENT = "leego: websocket: client not found '%s'"
)

// Manager 定义 WebSocket 连接管理器的数据结构
type Manager struct {
	clients    sync.Map           // 连接管理
	register   chan *Client       // 注册通道
	unregister chan *Client       // 注销通道
	broadcast  chan []byte        // 广播通道
	upgrader   *upgrader.Upgrader // WebSocket 升级器
}

// NewManager 新建一个 WebSocket 连接管理器
func NewManager(config *upgrader.Config) *Manager {
	return &Manager{
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan []byte, 256),
		upgrader:   upgrader.NewUpgrader(config),
	}
}

// HandleWebSocket 处理 WebSocket 连接请求
func (own *Manager) HandleWebSocket(resp response.Writer, req *http.Request) error {
	conn, err := own.upgrader.Upgrade(req, resp, nil)
	if err != nil {
		return err
	}

	clientId := generateClientId()
	client := &Client{
		Id:          clientId,
		SendChan:    make(chan []byte, 256),
		Metadata:    make(map[string]any),
		Connection:  conn,
		ConnectedAt: time.Now(),
	}

	own.register <- client

	// 启动读写协程
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go own.readPump(ctx, client)
	go own.writePump(ctx, client)

	// 发送欢迎消息
	msg := NewWelcomeMessage(clientId)
	welcomeBytes, _ := json.Marshal(&msg)
	client.SendChan <- welcomeBytes
	return nil
}

// readPump 从客户端读取消息
func (own *Manager) readPump(ctx context.Context, client *Client) {
	defer func() {
		own.unregister <- client
		_ = client.Connection.Close()
	}()

	for {
		select {
		case <-ctx.Done():
			return

		default:
			// 阻塞读取消息
			opCode, data, err := client.Connection.Read()
			if err != nil {
				if !upgrader.IsCloseError(err, upgrader.CLOSE_CODE_NORMAL_CLOSURE, upgrader.CLOSE_CODE_GOING_AWAY) {
					// TODO 待处理
				}
				return
			}

			// 只处理文本消息
			if opCode == upgrader.OP_CODE_TEXT {
				own.handleMessage(client, data)
			}
		}
	}
}

// writePump 向客户端写入消息
func (own *Manager) writePump(ctx context.Context, client *Client) {
	ticker := time.NewTicker(30 * time.Second) // 心跳
	defer func() {
		ticker.Stop()
		_ = client.Connection.Close()
	}()

	for {
		select {
		// 读取消息
		case message, ok := <-client.SendChan:
			if !ok {
				// 通道关闭
				if err := client.Connection.Write(upgrader.OP_CODE_CLOSE, []byte{}); err != nil {
					// TODO 待处理
				}
				return
			}

			if err := client.Connection.Write(upgrader.OP_CODE_TEXT, message); err != nil {
				// TODO 待处理
				return
			}

		// 发送心跳
		case <-ticker.C:
			bytes := NewPingMessage().Marshal()
			err := client.Connection.Write(upgrader.OP_CODE_TEXT, bytes)
			if err != nil {
				// TODO 待处理
				return
			}

		case <-ctx.Done():
			return
		}
	}
}

// handleMessage 处理客户端消息
func (own *Manager) handleMessage(client *Client, data []byte) {
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		// TODO 待处理
		log.Printf("Invalid message format: %v", err)
		return
	}

	switch msg.Type {
	// 响应心跳
	case MSG_TYPE_PING:
		bytes := NewPongMessage().Marshal()
		client.SendChan <- bytes

	// 广播消息
	case MSG_TYPE_BROADCAST:
		own.Broadcast("broadcast", msg.Data)

	// 私聊消息
	case MSG_TYPE_PRIVATE:
		if msg.To != "" {
			if err := own.SendToClient(msg.To, "private", map[string]any{
				"from":    client.Id,
				"message": msg.Data,
			}); err != nil {
				// TODO 待处理
			}
		}

	// 客户端身份识别
	case MSG_TYPE_IDENTIFY:
		if userId, ok := msg.Data.(string); ok {
			client.UserId = userId
			client.Metadata["user_id"] = userId
		}

	default:
		// TODO 待处理
		log.Printf("Unknown message type: %s", msg.Type)
	}
}

// Start 启动 WebSocket 管理器
func (own *Manager) Start(ctx context.Context) {
	for {
		select {
		case client := <-own.register:
			own.clients.Store(client.ID, client)
			log.Printf("Client connected: %s, total: %d", client.ID, own.ConnectionCount())

		case client := <-own.unregister:
			if _, loaded := own.clients.LoadAndDelete(client.ID); loaded {
				close(client.Send)
				log.Printf("Client disconnected: %s, total: %d", client.ID, own.ConnectionCount())
			}

		case message := <-own.broadcast:
			own.clients.Range(func(key, value any) bool {
				client := value.(*Client)
				select {
				case client.Send <- message:
				default:
					close(client.Send)
					own.clients.Delete(key)
				}
				return true
			})

		case <-ctx.Done():
			// 优雅关闭所有连接
			own.clients.Range(func(key, value any) bool {
				client := value.(*Client)
				client.Conn.Close()
				close(client.Send)
				own.clients.Delete(key)
				return true
			})
			return
		}
	}
}

// Broadcast 广播消息给所有客户端
func (own *Manager) Broadcast(messageType MessageType, data any) {
	msg := &Message{
		Type:      messageType,
		Timestamp: time.Now().Unix(),
		Data:      data,
	}
	own.broadcast <- msg.Marshal()
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
	bytes, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	// 发送消息
	select {
	case value.(*Client).Send <- bytes:
	default:
		return fmt.Errorf("client send channel is full")
	}
	return nil
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
// func (own *Manager) GetClients() []map[string]any {
// 	var clients []map[string]any
// 	own.clients.Range(func(key, value any) bool {
// 		client := value.(*Client)
// 		clients = append(clients, map[string]any{
// 			"id":           client.ID,
// 			"user_id":      client.UserID,
// 			"connected_at": client.ConnectedAt,
// 			"metadata":     client.Metadata,
// 		})
// 		return true
// 	})
// 	return clients
// }

// CloseClient 关闭特定客户端连接
func (own *Manager) CloseClient(clientId string) error {
	if value, ok := own.clients.Load(clientId); ok {
		value.(*Client).Connection.Close()
		return nil
	}

	return fmt.Errorf(_ERR_FORMAT_NOT_FOUND_CLIENT, clientId)
}

// generateClientId 生成客户端 ID
func generateClientId() string {
	return fmt.Sprintf("client_%d", time.Now().UnixNano())
}
