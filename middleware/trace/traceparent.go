package trace

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	// _VERSION_PREFIX 为当前支持的 traceparent 版本前缀。
	_VERSION_PREFIX = "00"
	// _SAMPLED_FLAG 表示采样的标志位。
	_SAMPLED_FLAG = "01"
	// _UNSAMPLED_FLAG 表示未采样的标志位。
	_UNSAMPLED_FLAG = "00"
)

// fallbackCounter 在 crypto/rand 不可用时提供进程内唯一后缀。
var fallbackCounter atomic.Uint64

// ParseTraceparent 解析 W3C traceparent 头。
//
// 严格按规范校验：版本 2 位、TraceID 32 位、SpanID 16 位、标志位 2 位，
// 全部为小写十六进制且 ID 不得全零；任何不满足都返回 ok=false。
func ParseTraceparent(value string) (traceID, parentID string, sampled bool, ok bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", false, false
	}

	parts := strings.Split(value, "-")
	// 规范要求 4 段；未来版本可能追加字段，但当前版本不接受。
	if len(parts) != 4 {
		return "", "", false, false
	}

	if parts[0] != _VERSION_PREFIX {
		return "", "", false, false
	}

	if !isLowerHex(parts[1], 32) || isZeroHex(parts[1]) {
		return "", "", false, false
	}

	if !isLowerHex(parts[2], 16) || isZeroHex(parts[2]) {
		return "", "", false, false
	}

	if !isLowerHex(parts[3], 2) {
		return "", "", false, false
	}

	flags, err := strconv.ParseUint(parts[3], 16, 8)
	if err != nil {
		return "", "", false, false
	}

	return parts[1], parts[2], flags&1 == 1, true
}

// FormatTraceparent 按 W3C 规范拼接 traceparent 头。
//
// 任何 ID 长度不为 32/16 位时返回空字符串，避免把非法值写进请求头。
func FormatTraceparent(traceID, spanID string, sampled bool) string {
	if len(traceID) != 32 || len(spanID) != 16 {
		return ""
	}

	if !isLowerHex(traceID, 32) || !isLowerHex(spanID, 16) {
		return ""
	}

	flag := _UNSAMPLED_FLAG
	if sampled {
		flag = _SAMPLED_FLAG
	}

	return fmt.Sprintf("%s-%s-%s-%s", _VERSION_PREFIX, traceID, spanID, flag)
}

// NewTraceID 生成新的链路 ID。
//
// 供需要主动开启链路的组件（定时任务、消息消费、网关转发）使用。
func NewTraceID() string {
	return newTraceID()
}

// NewSpanID 生成新的片段 ID。
func NewSpanID() string {
	return newSpanID()
}

// newTraceID 生成 32 位十六进制链路 ID（16 字节）。
func newTraceID() string {
	return randomHex(16)
}

// newSpanID 生成 16 位十六进制片段 ID（8 字节）。
func newSpanID() string {
	return randomHex(8)
}

// randomHex 生成指定字节数的随机十六进制串。
//
// crypto/rand 失败属于系统级异常（例如熵源不可用），此时退化为
// "时间戳 + 计数器"，宁可牺牲不可预测性也不能让链路 ID 为空。
func randomHex(size int) string {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		counter := fallbackCounter.Add(1)
		binary.BigEndian.PutUint64(buffer[0:8], uint64(time.Now().UnixNano())^counter)
		if size > 8 {
			binary.BigEndian.PutUint64(buffer[8:], counter)
		}
	}

	return hex.EncodeToString(buffer)
}

// isLowerHex 判断字符串是否为指定长度的小写十六进制串。
func isLowerHex(value string, length int) bool {
	if len(value) != length {
		return false
	}

	for index := 0; index < len(value); index++ {
		char := value[index]
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}

	return true
}

// isZeroHex 判断十六进制串是否全零。
func isZeroHex(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] != '0' {
			return false
		}
	}

	return true
}
