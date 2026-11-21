// Package upgrader 实现 RFC 6455 的 WebSocket 握手与帧编解码。
//
// 支持范围：
//   - 握手校验（方法、Upgrade/Connection/版本/Key/来源/子协议）；
//   - 文本帧与二进制帧、PING/PONG/CLOSE 控制帧；
//   - 单条消息大小限制、读写超时、掩码方向校验；
//
// 不支持范围（收到相关帧会以协议错误关闭连接）：
//   - 分片消息（FIN=0）、扩展协商（RSV 位）。
//
// 这些限制是有意为之：分片会显著增加状态机复杂度与内存占用，
// 而绝大多数业务场景使用单帧消息即可满足。
package upgrader

import (
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"gitee.com/leminewx/leego/response"
)

// Upgrader 将普通 HTTP 连接升级为 WebSocket 连接。
//
// Upgrader 在创建后不可变，可被多个请求并发复用。
type Upgrader struct {
	Config
}

/*
New 创建升级器。

config 为 nil 时使用 NewDefaultConfig 的默认配置；
传入的配置会被复制一份，后续修改原对象不会影响升级器。
*/
func New(config *Config) *Upgrader {
	if config == nil {
		config = NewDefaultConfig()
	}

	instance := &Upgrader{Config: *config}
	if instance.Config.MaxReadLimit <= 0 {
		instance.Config.MaxReadLimit = _DEFAULT_MAX_READ_LIMIT
	}

	return instance
}

/*
Upgrade 执行握手并把连接升级为 WebSocket。

参数：
  - req：原始 HTTP 请求；
  - resp：响应写入器，必须支持 Hijack（HTTP/1.x 直连或支持劫持的中间件）；
  - respHeader：需要附加到 101 响应中的额外响应头，可为 nil。

返回的 *Conn 由调用方负责关闭。
*/
func (own *Upgrader) Upgrade(req *http.Request, resp response.Writer, respHeader http.Header) (*Conn, error) {
	if req == nil || resp == nil {
		return nil, errors.New("leego: websocket: nil request or response")
	}

	if err := own.validateRequest(req); err != nil {
		return nil, err
	}

	subprotocol := own.negotiateSubprotocol(req)

	conn, readWriter, err := resp.Hijack()
	if err != nil {
		return nil, fmt.Errorf("leego: websocket: failed to hijack connection: %w", err)
	}

	handshake := buildHandshakeResponse(req, respHeader, subprotocol)
	if _, err := readWriter.WriteString(handshake); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("leego: websocket: failed to write handshake response: %w", err)
	}

	if err := readWriter.Flush(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("leego: websocket: failed to flush handshake response: %w", err)
	}

	// 复用 Hijack 返回的缓冲读写器：客户端可能在收到 101 后立刻发送数据，
	// 若改用新的 bufio.Reader，这部分已缓冲的数据会被丢弃。
	websocketConn := &Conn{
		conn:         conn,
		reader:       readWriter.Reader,
		writer:       readWriter.Writer,
		isServer:     true,
		readTimeout:  own.ReadTimeout,
		writeTimeout: own.WriteTimeout,
	}
	websocketConn.SetMaxReadLimit(own.MaxReadLimit)

	return websocketConn, nil
}

// validateRequest 校验握手请求是否合法。
func (own *Upgrader) validateRequest(req *http.Request) error {
	if req.Method != http.MethodGet {
		return errors.New("leego: websocket: request method must be GET")
	}

	header := req.Header

	if !strings.EqualFold(header.Get("Upgrade"), "websocket") {
		return errors.New("leego: websocket: missing or invalid header 'Upgrade'")
	}

	if !strings.Contains(strings.ToLower(header.Get("Connection")), "upgrade") {
		return errors.New("leego: websocket: missing or invalid header 'Connection'")
	}

	if version := header.Get("Sec-WebSocket-Version"); version != "13" {
		return fmt.Errorf("leego: websocket: unsupported version %q, want 13", version)
	}

	// Sec-WebSocket-Key 必须是 16 字节随机数的 Base64 编码。
	key := header.Get("Sec-WebSocket-Key")
	if key == "" {
		return errors.New("leego: websocket: missing header 'Sec-WebSocket-Key'")
	}

	decodedKey, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(decodedKey) != 16 {
		return errors.New("leego: websocket: invalid header 'Sec-WebSocket-Key'")
	}

	if own.OriginCheckFunc != nil && !own.OriginCheckFunc(req) {
		return errors.New("leego: websocket: origin not allowed")
	}

	return nil
}

// negotiateSubprotocol 在允许列表中协商子协议，未配置或未命中时返回空字符串。
func (own *Upgrader) negotiateSubprotocol(req *http.Request) string {
	if len(own.Subprotocols) == 0 {
		return ""
	}

	allowed := make(map[string]struct{}, len(own.Subprotocols))
	for _, subprotocol := range own.Subprotocols {
		allowed[subprotocol] = struct{}{}
	}

	for _, candidate := range strings.Split(req.Header.Get("Sec-WebSocket-Protocol"), ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}

		if _, isAllowed := allowed[candidate]; isAllowed {
			return candidate
		}
	}

	return ""
}

// buildHandshakeResponse 构建 101 Switching Protocols 响应报文。
func buildHandshakeResponse(req *http.Request, respHeader http.Header, subprotocol string) string {
	var builder strings.Builder
	builder.Grow(256)

	builder.WriteString("HTTP/1.1 101 Switching Protocols\r\n")
	builder.WriteString("Upgrade: websocket\r\n")
	builder.WriteString("Connection: Upgrade\r\n")
	builder.WriteString("Sec-WebSocket-Accept: ")
	builder.WriteString(computeAcceptKey(req.Header.Get("Sec-WebSocket-Key")))
	builder.WriteString("\r\n")

	if subprotocol != "" {
		builder.WriteString("Sec-WebSocket-Protocol: ")
		builder.WriteString(subprotocol)
		builder.WriteString("\r\n")
	}

	reserved := map[string]struct{}{
		"upgrade":                {},
		"connection":             {},
		"sec-websocket-accept":   {},
		"sec-websocket-protocol": {},
	}

	for key, values := range respHeader {
		if _, isReserved := reserved[strings.ToLower(key)]; isReserved {
			continue
		}

		for _, value := range values {
			// 手写字节拼接必须逐项校验：标准库 Header 写入会拒绝含 CR/LF 的
			// 非法头，而这里绕过了该保护，不校验即形成响应头注入面。
			if !isValidHeaderField(key, value) {
				continue
			}

			builder.WriteString(key)
			builder.WriteString(": ")
			builder.WriteString(value)
			builder.WriteString("\r\n")
		}
	}

	builder.WriteString("\r\n")

	return builder.String()
}

// isValidHeaderField 判断响应头名与值是否可安全写入裸报文。
//
// 头部名称必须是 RFC 7230 的 token，值不得包含 CR、LF 或 NUL，
// 避免注入额外的响应行或头。
func isValidHeaderField(name, value string) bool {
	if name == "" {
		return false
	}

	for index := 0; index < len(name); index++ {
		if !isTokenChar(name[index]) {
			return false
		}
	}

	for index := 0; index < len(value); index++ {
		switch value[index] {
		case '\r', '\n', 0:
			return false
		}
	}

	return true
}

// isTokenChar 判断字节是否为 HTTP token 允许的字符。
func isTokenChar(char byte) bool {
	if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' {
		return true
	}

	switch char {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	}

	return false
}

// keySalt 为 RFC 6455 定义的固定 GUID。
var keySalt = []byte("258EAFA5-E914-47DA-95CA-C5AB0DC85B11")

// computeAcceptKey 计算 Sec-WebSocket-Accept 值。
func computeAcceptKey(challengeKey string) string {
	hash := sha1.New()
	hash.Write([]byte(challengeKey))
	hash.Write(keySalt)

	return base64.StdEncoding.EncodeToString(hash.Sum(nil))
}
