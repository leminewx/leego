package ggid

import (
	"encoding/binary"
	"time"
)

// ShortID96 96位增强短ID
type ShortID96 struct {
	timestamp uint64 // 48位: 时间戳(约8925年)
	nodeID    uint16 // 16位: 节点ID
	sequence  uint32 // 32位: 序列号+随机数
}

// NewShortID96 生成96位增强ID
func NewShortID96() ShortID96 {
	shortMutex.Lock()
	defer shortMutex.Unlock()

	currentTime := time.Since(shortEpoch).Milliseconds()

	if currentTime < shortLastTime {
		currentTime = shortLastTime
	}

	if currentTime == shortLastTime {
		shortSequence = (shortSequence + 1) & 0xFFFFFF
		if shortSequence == 0 {
			for currentTime <= shortLastTime {
				time.Sleep(100 * time.Microsecond)
				currentTime = time.Since(shortEpoch).Milliseconds()
			}
		}
	} else {
		shortSequence = 0
	}

	shortLastTime = currentTime

	// 生成随机部分
	randomPart := generateRandom24Bits()

	return ShortID96{
		timestamp: uint64(currentTime),
		nodeID:    nodeID,
		sequence:  (shortSequence << 8) | randomPart,
	}
}

// String Base62编码，约16字符
func (s ShortID96) String() string {
	// 将96位数据编码为Base62
	data := make([]byte, 14)
	binary.BigEndian.PutUint64(data[0:8], s.timestamp)
	binary.BigEndian.PutUint16(data[8:10], s.nodeID)
	binary.BigEndian.PutUint32(data[10:14], s.sequence)

	return base62EncodeBytes(data[:12])
}
