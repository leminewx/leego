package ggid

import (
	"crypto/rand"
	"math/big"
	"time"
)

// Base62字符集 (0-9, A-Z, a-z)
const base62Chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// base62Encode 数字转Base62
func base62Encode(num uint64) string {
	if num == 0 {
		return "0"
	}

	var result []byte
	for num > 0 {
		remainder := num % 62
		result = append([]byte{base62Chars[remainder]}, result...)
		num = num / 62
	}

	return string(result)
}

// base62EncodeBytes 字节切片转Base62
func base62EncodeBytes(data []byte) string {
	// 将字节数组视为大端序的大整数
	var num big.Int
	num.SetBytes(data)

	if num.BitLen() == 0 {
		return "0"
	}

	var result []byte
	zero := big.NewInt(0)
	base := big.NewInt(62)
	mod := new(big.Int)

	for num.Cmp(zero) > 0 {
		num.DivMod(&num, base, mod)
		result = append([]byte{base62Chars[mod.Uint64()]}, result...)
	}

	return string(result)
}

// ReadableID 可读性优化的ID
type ReadableID string

// NewReadableID 生成易读的短ID
func NewReadableID() ReadableID {
	timestamp := time.Now().UnixNano() / 1e6 // 毫秒时间戳
	random := generateReadableRandom()

	// 格式: 时间戳(base36) + 分隔符 + 随机串
	timestampStr := base36Encode(uint64(timestamp))

	return ReadableID(timestampStr + "-" + random)
}

// base36编码 (0-9, a-z)
func base36Encode(num uint64) string {
	const chars = "0123456789abcdefghijklmnopqrstuvwxyz"

	if num == 0 {
		return "0"
	}

	var result []byte
	for num > 0 {
		result = append([]byte{chars[num%36]}, result...)
		num /= 36
	}
	return string(result)
}

// 生成易读的随机串 (避免相似字符)
func generateReadableRandom() string {
	const readableChars = "23456789abcdefghjkmnpqrstuvwxyz" // 去掉 0,1,i,l,o
	buf := make([]byte, 6)
	rand.Read(buf)

	result := make([]byte, 6)
	for i := 0; i < 6; i++ {
		result[i] = readableChars[int(buf[i])%len(readableChars)]
	}
	return string(result)
}
