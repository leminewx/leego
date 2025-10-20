package ggid

import (
	"sync"
	"time"
)

// ShortID64 64位短ID
type ShortID64 uint64

// 配置
const (
	shortEpochStr = "2023-01-01T00:00:00Z"
	shortSeqMax   = 0x3FF // 10位序列号
	shortNodeBits = 10    // 节点ID位数
	shortTimeBits = 43    // 时间戳位数 (43位 ≈ 278年)
)

var (
	shortEpoch    time.Time
	shortSequence uint32
	shortLastTime int64
	shortMutex    sync.Mutex
	nodeID        uint16
)

func init() {
	var err error
	shortEpoch, err = time.Parse(time.RFC3339, shortEpochStr)
	if err != nil {
		panic(err)
	}

	initNodeID()
}

// NewShortID 生成64位短ID
func NewShortID() ShortID64 {
	shortMutex.Lock()
	defer shortMutex.Unlock()

	currentTime := time.Since(shortEpoch).Milliseconds()

	// 处理时钟回拨
	if currentTime < shortLastTime {
		currentTime = shortLastTime
	}

	// 序列号处理
	if currentTime == shortLastTime {
		shortSequence = (shortSequence + 1) & shortSeqMax
		if shortSequence == 0 {
			// 等待下一毫秒
			for currentTime <= shortLastTime {
				time.Sleep(100 * time.Microsecond)
				currentTime = time.Since(shortEpoch).Milliseconds()
			}
		}
	} else {
		shortSequence = 0
	}

	shortLastTime = currentTime

	// 构建64位ID
	// 结构: [时间戳43位][节点ID10位][序列号10位][随机标志1位]
	id := uint64(currentTime) << (shortNodeBits + 11)
	id |= uint64(nodeID) << 11
	id |= uint64(shortSequence) << 1

	// 添加随机位增加熵
	randomBit := uint64(0)
	if time.Now().Nanosecond()%2 == 0 {
		randomBit = 1
	}
	id |= randomBit

	return ShortID64(id)
}

// String Base62编码，约11字符
func (s ShortID64) String() string {
	return base62Encode(uint64(s))
}

// Timestamp 提取时间
func (s ShortID64) Timestamp() time.Time {
	timestamp := uint64(s) >> (shortNodeBits + 11)
	return shortEpoch.Add(time.Duration(timestamp) * time.Millisecond)
}
