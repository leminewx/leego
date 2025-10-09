package upgrader

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"

	"gitee.com/leminewx/leego/response"
)

// Upgrader 定义升级 HTTP/HTTPS 连接到 WebSocket 的数据结构
type Upgrader struct {
	Config
}

// NewUpgrader 新建一个 Upgrader
func NewUpgrader(config *Config) *Upgrader {
	if config == nil {
		config = NewDefaultConfig()
	}

	return &Upgrader{Config: *config}
}

// Upgrade 升级 HTTP/HTTPS 连接到 WebSocket
func (own *Upgrader) Upgrade(req *http.Request, resp response.Writer, respHeader http.Header) (*Conn, error) {
	// 校验请求
	if err := own.validateRequest(req); err != nil {
		return nil, err
	}

	// 获取 HTTP/HTTPS 底层连接
	conn, buf, err := resp.Hijack()
	if err != nil {
		return nil, err
	}

	// 构建响应头
	headers := make([]string, 0, 8)
	headers = append(headers,
		"HTTP/1.1 101 Switching Protocols",
		"Upgrade: websocket",
		"Connection: Upgrade",
		"Sec-WebSocket-Accept: "+computeAcceptKey(req.Header.Get("Sec-WebSocket-Key")),
	)

	// 添加额外的响应头
	for key, values := range respHeader {
		if key == "Upgrade" || key == "Connection" || key == "Sec-WebSocket-Accept" {
			continue
		}

		for _, value := range values {
			headers = append(headers, key+": "+value)
		}
	}

	// 写入响应
	if _, err = buf.WriteString(strings.Join(headers, "\r\n") + "\r\n\r\n"); err != nil {
		_ = conn.Close()
		return nil, err
	}

	if err = buf.Flush(); err != nil {
		_ = conn.Close()
		return nil, err
	}

	// 返回 WebSocket 连接
	return &Conn{
		conn:         conn,
		reader:       bufio.NewReader(conn),
		writer:       bufio.NewWriter(conn),
		isServer:     true,
		readTimeout:  own.ReadTimeout,
		writeTimeout: own.WriteTimeout,
	}, nil
}

func (own *Upgrader) validateRequest(req *http.Request) error {
	// 验证请求方法
	if req.Method != http.MethodGet {
		return errors.New("leego: websocket: request method not 'GET'")
	}

	// 验证 Upgrade 头
	header := req.Header
	if !strings.EqualFold(header.Get("Upgrade"), "websocket") {
		return errors.New("leego: websocket: missing or invalid header 'Upgrade'")
	}

	// 验证 Connection 头
	if !strings.Contains(strings.ToLower(header.Get("Connection")), "upgrade") {
		return errors.New("leego: websocket: missing or invalid header 'Connection'")
	}

	// 验证 WebSocket 版本
	if header.Get("Sec-WebSocket-Version") != "13" {
		return errors.New("leego: websocket: missing or invalid header 'Sec-WebSocket-Version'")
	}

	// 验证 Origin
	if own.OriginCheckFunc != nil && !own.OriginCheckFunc(req) {
		return errors.New("leego: websocket: origin not allowed")
	}

	// 获取 WebSocket 密钥
	if key := header.Get("Sec-WebSocket-Key"); key == "" {
		return errors.New("leego: websocket: missing header 'Sec-WebSocket-Key'")
	}

	return nil
}

var keySalt = []byte("258EAFA5-E914-47DA-95CA-C5AB0DC85B11")

// computeAcceptKey 计算 WebSocket 接受密钥
func computeAcceptKey(challengeKey string) string {
	hash := sha1.New()
	hash.Write([]byte(challengeKey))
	hash.Write(keySalt)
	return base64.StdEncoding.EncodeToString(hash.Sum(nil))
}
