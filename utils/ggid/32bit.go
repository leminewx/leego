package ggid

import (
	"sync"
	"time"
)

// ShortID32 32位极短ID
type ShortID32 uint32

// 32位ID配置
const (
	tinyEpochStr  = "2024-01-01T00:00:00Z"
	tinySeqMax    = 0x3FF // 10位序列号
	tinyTimeBits  = 20    // 时间戳位数 (20位 ≈ 12天)
	tinyNodeBits  = 6     // 节点ID位数
	tinyRandomBit = 6     // 随机位
)

var (
	tinyEpoch    time.Time
	tinySequence uint16
	tinyLastTime int64
	tinyMutex    sync.Mutex
	tinyNodeID   byte
)

// NewTinyID 生成32位极短ID
func NewTinyID() ShortID32 {
	tinyMutex.Lock()
	defer tinyMutex.Unlock()

	currentTime := time.Since(tinyEpoch).Seconds() // 使用秒级时间戳

	if currentTime < float64(lastTimestamp) {
		currentTime = float64(lastTimestamp)
	}

	if int64(currentTime) == tinyLastTime {
		tinySequence = (tinySequence + 1) & tinySeqMax
		if tinySequence == 0 {
			currentTime = float64(tinyLastTime + 1)
		}
	} else {
		tinySequence = 0
	}

	tinyLastTime = int64(currentTime)

	// 32位结构: [时间戳20位][节点ID6位][序列号6位]
	id := uint32(currentTime) << (tinyNodeBits + 6)
	id |= uint32(tinyNodeID) << 6
	id |= uint32(tinySequence)

	return ShortID32(id)
}

// String Base62编码，约6字符
func (s ShortID32) String() string {
	return base62Encode(uint64(s))
}
