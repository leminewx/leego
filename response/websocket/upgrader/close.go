package upgrader

import (
	"errors"
	"fmt"
)

// CloseCode 定义关闭状态码的数据类型
type CloseCode uint16

const (
	CLOSE_CODE_NORMAL_CLOSURE             CloseCode = 1000 // 正常关闭
	CLOSE_CODE_GOING_AWAY                 CloseCode = 1001 // 端点离开（如服务器重启）
	CLOSE_CODE_PROTOCOL_ERROR             CloseCode = 1002 // 协议错误
	CLOSE_CODE_UNSUPPORTED_DATA           CloseCode = 1003 // 接收到不支持的数据类型
	CLOSE_CODE_NO_CODE_RECEIVED           CloseCode = 1005 // 未收到状态码
	CLOSE_CODE_ABNORMAL_CLOSURE           CloseCode = 1006 // 异常关闭
	CLOSE_CODE_INVALID_FRAME_PAYLOAD_DATA CloseCode = 1007 // 数据格式不一致（非UTF-8）
	CLOSE_CODE_POLICY_VIOLATION           CloseCode = 1008 // 策略违规
	CLOSE_CODE_MESSAGE_TOO_BIG            CloseCode = 1009 // 消息过大
	CLOSE_CODE_MANDATORY_EXTENSION        CloseCode = 1010 // 缺少必要扩展
	CLOSE_CODE_INTERNAL_SERVER_ERROR      CloseCode = 1011 // 服务器内部错误
	CLOSE_CODE_SERVICE_RESTART            CloseCode = 1012 // 服务器重启
	CLOSE_CODE_TRY_AGAIN_LATER            CloseCode = 1013 // 请稍后再试
	CLOSE_CODE_TLS_HANDSHAKE              CloseCode = 1015 // TLS 握手失败
)

// CloseError 表示 WebSocket 关闭错误
type CloseError struct {
	Code CloseCode
	Text string
}

// Error 实现 error 接口
func (own *CloseError) Error() string {
	return fmt.Sprintf("leego: websocket: close %d %s", own.Code, own.Text)
}

// IsCloseError 检查错误是否为关闭错误
func IsCloseError(err error, codes ...CloseCode) bool {
	if err == nil {
		return false
	}

	var ce *CloseError
	if ok := errors.As(err, &ce); !ok {
		return false
	}

	if len(codes) == 0 {
		return true
	}

	for _, code := range codes {
		if ce.Code == code {
			return true
		}
	}

	return false
}
