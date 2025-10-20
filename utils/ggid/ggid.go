package ggid // Go's Global ID

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"sync"
	"time"
)

// GID 全局唯一ID结构
type GID [16]byte

// 配置参数
const (
	epochStr    = "2020-01-01T00:00:00Z" // 自定义纪元，延长使用时间
	machineID   = 0xFFFF                 // 机器ID掩码
	processID   = 0xFF                   // 进程ID掩码
	sequenceMax = 0x3FFF                 // 序列号最大值 (14位)
)

var (
	epoch         time.Time
	sequence      uint32
	lastTimestamp int64
	mutex         sync.Mutex
	machineIDVal  uint16
	processIDVal  uint8
)

func init() {
	// 解析纪元时间
	var err error
	epoch, err = time.Parse(time.RFC3339, epochStr)
	if err != nil {
		panic(fmt.Sprintf("Failed to parse epoch: %v", err))
	}

	// 自动生成机器标识
	initMachineID()
	initProcessID()
}

// 初始化机器标识
func initMachineID() {
	// 方法1: 使用IP地址后16位
	if interfaces, err := net.Interfaces(); err == nil {
		for _, iface := range interfaces {
			if addrs, err := iface.Addrs(); err == nil && len(addrs) > 0 {
				for _, addr := range addrs {
					if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
						if ipnet.IP.To4() != nil {
							// 使用IP后两字节
							ip := ipnet.IP.To4()
							machineIDVal = binary.BigEndian.Uint16(ip[2:4])
							return
						}
					}
				}
			}
		}
	}

	// 方法2: 使用随机数作为后备
	buf := make([]byte, 2)
	rand.Read(buf)
	machineIDVal = binary.BigEndian.Uint16(buf)
}

// 初始化进程标识
func initProcessID() {
	// 在实际应用中可以从环境变量或配置文件获取
	// 这里使用随机数
	buf := make([]byte, 1)
	rand.Read(buf)
	processIDVal = buf[0]
}

// NewGID 生成全局唯一ID
func NewGID() GID {
	mutex.Lock()
	defer mutex.Unlock()

	var gid GID
	currentTime := timeSinceEpoch()

	// 处理时钟回拨
	if currentTime < lastTimestamp {
		currentTime = handleClockDrift(currentTime, lastTimestamp)
	}

	// 同一毫秒内的序列号递增
	if currentTime == lastTimestamp {
		sequence = (sequence + 1) & sequenceMax
		if sequence == 0 {
			// 序列号耗尽，等待下一毫秒
			currentTime = waitNextMillis(lastTimestamp)
		}
	} else {
		sequence = 0
	}

	lastTimestamp = currentTime

	// 构建GID
	// 字节布局: [时间戳(6B)][机器ID(2B)][进程ID(1B)][序列号(2B)][随机数(5B)]

	// 时间戳 (48位, 6字节) - 可支持约8925年
	timestamp := uint64(currentTime)
	binary.BigEndian.PutUint64(gid[0:8], timestamp<<16) // 只使用前6字节

	// 机器ID (16位, 2字节)
	binary.BigEndian.PutUint16(gid[6:8], machineIDVal)

	// 进程ID (8位, 1字节)
	gid[8] = processIDVal

	// 序列号 (14位, 2字节中的14位)
	seqWithFlags := uint16(sequence) & sequenceMax
	binary.BigEndian.PutUint16(gid[9:11], seqWithFlags)

	// 随机数 (40位, 5字节) - 增加随机性，防止猜测
	rand.Read(gid[11:16])

	return gid
}

// 从纪元开始的时间戳（毫秒）
func timeSinceEpoch() int64 {
	return time.Since(epoch).Milliseconds()
}

// 等待下一毫秒
func waitNextMillis(lastTimestamp int64) int64 {
	timestamp := timeSinceEpoch()
	for timestamp <= lastTimestamp {
		time.Sleep(time.Microsecond * 100) // 100微秒
		timestamp = timeSinceEpoch()
	}
	return timestamp
}

// String 返回标准格式字符串
func (g GID) String() string {
	var buf [36]byte

	// 格式: TTTTTT-MM-SS-RR-RRRRRR
	// T: 时间戳, M: 机器ID, S: 序列号, R: 随机数
	hex.Encode(buf[0:12], g[0:6]) // 时间戳 (6字节 -> 12字符)
	buf[12] = '-'
	hex.Encode(buf[13:17], g[6:8]) // 机器ID (2字节 -> 4字符)
	buf[17] = '-'
	hex.Encode(buf[18:20], g[8:9])  // 进程ID (1字节 -> 2字符)
	hex.Encode(buf[20:22], g[9:10]) // 序列号高字节 (1字节 -> 2字符)
	buf[22] = '-'
	hex.Encode(buf[23:25], g[10:11]) // 序列号低字节 (1字节 -> 2字符)
	buf[25] = '-'
	hex.Encode(buf[26:36], g[11:16]) // 随机数 (5字节 -> 10字符)

	return string(buf[:])
}

// ShortString 返回短格式（Base62编码）
func (g GID) ShortString() string {
	// Base62编码实现，减少字符串长度
	// 这里简化为十六进制，实际可以使用Base62库
	return fmt.Sprintf("%x", g[:])
}

// Timestamp 提取时间戳
func (g GID) Timestamp() time.Time {
	// 提取时间戳部分
	timestampBytes := make([]byte, 8)
	copy(timestampBytes[2:], g[0:6]) // 大端序，前补零
	timestampMs := binary.BigEndian.Uint64(timestampBytes)
	return epoch.Add(time.Duration(timestampMs) * time.Millisecond)
}

// MachineID 提取机器标识
func (g GID) MachineID() uint16 {
	return binary.BigEndian.Uint16(g[6:8])
}

// 初始化函数
func init() {
	var err error

	// 初始化64位ID的纪元时间
	shortEpoch, err = time.Parse(time.RFC3339, "2023-01-01T00:00:00Z")
	if err != nil {
		panic(err)
	}

	// 初始化32位ID的纪元时间
	tinyEpoch, err = time.Parse(time.RFC3339, "2024-01-01T00:00:00Z")
	if err != nil {
		panic(err)
	}

	// 初始化节点ID
	initNodeID()
	initTinyNodeID()
}

// initNodeID 初始化64位ID的节点ID
func initNodeID() {
	// 尝试使用IP地址后16位
	if interfaces, err := net.Interfaces(); err == nil {
		for _, iface := range interfaces {
			if iface.Flags&net.FlagUp == 0 {
				continue // 接口未启用
			}
			if addrs, err := iface.Addrs(); err == nil {
				for _, addr := range addrs {
					if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
						if ip := ipnet.IP.To4(); ip != nil {
							// 使用IP地址的后16位
							nodeID = binary.BigEndian.Uint16(ip[2:4])
							return
						}
					}
				}
			}
		}
	}

	// 后备方案：使用随机数
	buf := make([]byte, 2)
	rand.Read(buf)
	nodeID = binary.BigEndian.Uint16(buf)
}

// initTinyNodeID 初始化32位ID的节点ID
func initTinyNodeID() {
	// 32位ID使用更短的节点ID（6位）
	buf := make([]byte, 1)
	rand.Read(buf)
	tinyNodeID = buf[0] & 0x3F // 只取6位
}

// generateRandom24Bits 生成24位随机数
func generateRandom24Bits() uint32 {
	buf := make([]byte, 3)
	rand.Read(buf)

	// 将3字节转换为uint32
	return uint32(buf[0])<<16 | uint32(buf[1])<<8 | uint32(buf[2])
}

// 处理时钟回拨的通用函数
func handleClockDrift(currentTime, lastTime int64) int64 {
	if currentTime > lastTime {
		return currentTime
	}

	// 简单的时钟回拨处理：使用最后时间+1
	// 在实际生产环境中可能需要更复杂的策略
	return lastTime + 1
}
