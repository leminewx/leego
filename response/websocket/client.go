package websocket

import (
	"sync"
	"time"

	"gitee.com/leminewx/leego/response/websocket/upgrader"
)

// Client 定义 WebSocket 客户端的数据结构
type Client struct {
	Id          string         // 客户端Id
	UserId      string         // 用户Id
	Metadata    map[string]any // 客户端元数据
	SendChan    chan []byte    // 发送消息通道
	CloseChan   chan struct{}  // 关闭通道
	Connection  *upgrader.Conn // WebSocket 连接
	ConnectedAt time.Time      // 连接建立时间
	closeOnce   sync.Once      // 确保 Close 只执行一次，避免重复关闭通道引发 panic
}

// NewClient 创建一个新的 WebSocket 客户端
func NewClient(clientId, userId string, conn *upgrader.Conn) *Client {
	return &Client{
		Id:          clientId,
		UserId:      userId,
		Metadata:    make(map[string]any),
		SendChan:    make(chan []byte, 256),
		CloseChan:   make(chan struct{}),
		Connection:  conn,
		ConnectedAt: time.Now(),
	}
}

// Close 关闭 WebSocket 客户端的连接（幂等）
func (own *Client) Close() error {
	var err error
	own.closeOnce.Do(func() {
		err = own.Connection.Close()
		close(own.SendChan)
		close(own.CloseChan)
	})
	return err
}
