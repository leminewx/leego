package websocket

import (
	"gitee.com/leminewx/leego/response/websocket/v2/upgrader"
	"time"
)

// Client 定义 WebSocket 客户端的数据结构
type Client struct {
	Id          string         // 客户端Id
	UserId      string         // 用户Id
	Metadata    map[string]any // 客户端元数据
	SendChan    chan []byte    // 发送消息通道
	Connection  *upgrader.Conn // WebSocket 连接
	ConnectedAt time.Time      // 连接建立时间
}
