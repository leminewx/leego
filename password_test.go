package leego

import (
	"bytes"
	"strings"
	"testing"
)

// TestHashPasswordByBcrypt 验证哈希结果的基本性质。
func TestHashPasswordByBcrypt(t *testing.T) {
	password := []byte("leego-secret")

	hashed, err := HashPasswordByBcrypt(password)
	if err != nil {
		t.Fatalf("HashPasswordByBcrypt() error = %v", err)
	}

	if len(hashed) == 0 {
		t.Fatal("哈希结果不能为空")
	}

	if bytes.Equal(hashed, password) {
		t.Fatal("哈希结果不能等于明文密码")
	}

	// bcrypt 的输出以版本与成本因子开头，便于人工排查配置。
	if !strings.HasPrefix(string(hashed), "$2") {
		t.Fatalf("哈希结果前缀异常: %q", string(hashed[:4]))
	}
}

// TestHashPasswordUsesRandomSalt 验证相同明文每次哈希结果不同。
func TestHashPasswordUsesRandomSalt(t *testing.T) {
	password := []byte("same-password")

	first, err := HashPasswordByBcrypt(password)
	if err != nil {
		t.Fatalf("第一次哈希失败: %v", err)
	}

	second, err := HashPasswordByBcrypt(password)
	if err != nil {
		t.Fatalf("第二次哈希失败: %v", err)
	}

	if bytes.Equal(first, second) {
		t.Fatal("bcrypt 必须使用随机盐，两次哈希结果不应相同")
	}

	// 尽管盐不同，两者都必须能校验通过。
	if !VerifyPasswordByBcrypt(first, password) || !VerifyPasswordByBcrypt(second, password) {
		t.Fatal("不同盐生成的哈希都应能校验通过")
	}
}

// TestVerifyPasswordByBcrypt 验证正确与错误密码的校验结果。
func TestVerifyPasswordByBcrypt(t *testing.T) {
	password := []byte("correct-password")
	hashed, err := HashPasswordByBcrypt(password)
	if err != nil {
		t.Fatalf("哈希失败: %v", err)
	}

	if !VerifyPasswordByBcrypt(hashed, password) {
		t.Fatal("正确密码必须校验通过")
	}

	if VerifyPasswordByBcrypt(hashed, []byte("wrong-password")) {
		t.Fatal("错误密码必须校验失败")
	}

	if VerifyPasswordByBcrypt([]byte("not-a-bcrypt-hash"), password) {
		t.Fatal("非法哈希必须校验失败")
	}

	if VerifyPasswordByBcrypt(hashed, nil) {
		t.Fatal("空密码必须校验失败")
	}
}

// TestHashPasswordRejectsTooLongInput 验证超长输入被拒绝。
//
// bcrypt 只处理前 72 字节，超出部分会被静默丢弃；标准库因此直接返回错误，
// 避免“看似更长的密码实际上只有前 72 字节有效”这一安全隐患。
func TestHashPasswordRejectsTooLongInput(t *testing.T) {
	if _, err := HashPasswordByBcrypt(bytes.Repeat([]byte("a"), 73)); err == nil {
		t.Fatal("超过 72 字节的密码必须返回错误")
	}
}
