package uuid

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"sync"
	"unsafe"
)

var pool = sync.Pool{
	New: func() any {
		buf := make([]byte, 16)
		return &buf
	},
}

// UUID 表示一个 128 位的 UUID
type UUID [16]byte

// NewV4 生成版本 4 UUID（随机）
func NewV4() UUID {
	var uuid UUID
	buf := *(pool.Get().(*[]byte))
	defer pool.Put(&buf)

	_, err := io.ReadFull(rand.Reader, buf)
	if err != nil {
		panic(fmt.Errorf("failed to read random bytes: %v", err))
	}

	copy(uuid[:], buf)

	// 设置版本位 (4) 和变体位 (2)
	uuid[6] = (uuid[6] & 0x0f) | 0x40 // version 4
	uuid[8] = (uuid[8] & 0x3f) | 0x80 // variant RFC 4122

	return uuid
}

// String 返回规范的 UUID 字符串格式
func (u UUID) String() string {
	var buf [36]byte
	hex.Encode(buf[0:8], u[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], u[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], u[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], u[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], u[10:16])
	return string(buf[:])
}

const hextable = "0123456789abcdef"

// FastString 使用非安全转换优化字符串格式化
func (u UUID) FastString() string {
	var buf [36]byte

	// 手动编码十六进制，避免 hex.Encode 的开销
	for i := 0; i < 16; i++ {
		v := u[i]

		// 计算在输出缓冲区中的位置
		var pos int
		switch {
		case i < 4:
			pos = i * 2
		case i < 6:
			pos = i*2 + 1
		case i < 8:
			pos = i*2 + 2
		case i < 10:
			pos = i*2 + 3
		default:
			pos = i*2 + 4
		}

		buf[pos] = hextable[v>>4]
		buf[pos+1] = hextable[v&0x0f]
	}

	// 插入横线
	buf[8] = '-'
	buf[13] = '-'
	buf[18] = '-'
	buf[23] = '-'

	return unsafe.String(&buf[0], 36)
}

// Bytes 返回 UUID 的字节切片
func (u UUID) Bytes() []byte {
	return u[:]
}
