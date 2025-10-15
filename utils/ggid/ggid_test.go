package ggid

import (
	"fmt"
	"testing"
)

// 示例使用
func TestGGID(t *testing.T) {
	for range 100 {
		// 单个生成
		gid1 := NewGID()
		fmt.Printf("GGID: %s\n", gid1.String())
		fmt.Printf("Short GGID: %s\n", gid1.ShortString())
		fmt.Printf("GGID Timestamp: %s\n", gid1.Timestamp())
		fmt.Printf("GGID Machine ID: %d\n\n\n", gid1.MachineID())
	}
}

// 测试唯一性
func BenchmarkGGID(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = NewGID().String()
	}
}
