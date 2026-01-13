package breaker

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestCircuitBreaker(t *testing.T) {
	// 创建熔断器：失败3次后熔断，5秒后尝试恢复，需要2次成功才能完全恢复
	breaker := NewCircuitBreaker(3, 2, 5*time.Second)

	// 模拟服务调用
	serviceCall := func(fail bool) func() error {
		return func() error {
			if fail {
				return errors.New("服务调用失败")
			}

			return nil
		}
	}

	fmt.Println("=== 测试失败触发熔断 ===")
	// 连续失败3次，触发熔断
	for i := 0; i < 4; i++ {
		err := breaker.Execute(serviceCall(true))
		fmt.Printf("第%d次调用: %v, 状态: %s\n", i+1, err, breaker.GetState())
	}

	fmt.Println("\n=== 测试熔断状态下的调用 ===")
	// 熔断状态下的调用应该被拒绝
	for i := 0; i < 3; i++ {
		err := breaker.Execute(serviceCall(false))
		fmt.Printf("第%d次调用: %v, 状态: %s\n", i+1, err, breaker.GetState())
		time.Sleep(1 * time.Second)
	}

	fmt.Println("\n=== 等待熔断超时 ===")
	// 等待熔断超时，进入半开状态
	time.Sleep(3 * time.Second) // 前面已经等待了2秒，再等3秒总共5秒

	fmt.Println("\n=== 测试半开状态 ===")
	// 第一次成功调用
	err := breaker.Execute(serviceCall(false))
	fmt.Printf("半开状态第1次成功调用: %v, 状态: %s\n", err, breaker.GetState())

	// 第二次成功调用，应该恢复闭合状态
	err = breaker.Execute(serviceCall(false))
	fmt.Printf("半开状态第2次成功调用: %v, 状态: %s\n", err, breaker.GetState())

	fmt.Println("\n=== 测试恢复正常后 ===")
	// 恢复正常后的调用
	err = breaker.Execute(serviceCall(false))
	fmt.Printf("正常调用: %v, 状态: %s\n", err, breaker.GetState())
}

// cpu: QEMU Virtual CPU version 2.5+		80    	33131412	        36.19 ns/op	       0 B/op	       0 allocs/op
func BenchmarkCircuitBreaker(b *testing.B) {
	// 创建熔断器：失败3次后熔断，5秒后尝试恢复，需要2次成功才能完全恢复
	breaker := NewCircuitBreaker(3, 2, 5*time.Second)

	// 模拟服务调用
	serviceCall := func(fail bool) func() error {
		return func() error {
			if fail {
				return errors.New("服务调用失败")
			}

			return nil
		}
	}

	for range b.N {
		_ = breaker.Execute(serviceCall(false))
	}
}
