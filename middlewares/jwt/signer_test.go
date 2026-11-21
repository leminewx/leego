package jwt

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// testHMACKey 是满足 HS512 最小密钥长度（64 字节）的测试密钥。
const testHMACKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// createTestClaims 构造一份时间字段均合法的测试声明。
func createTestClaims() *StandardClaims {
	now := time.Now().Unix()
	return &StandardClaims{
		Id:        "jti-1",
		Issuer:    "issuer",
		Subject:   "subject",
		Audiences: []string{"audience"},
		IssuedAt:  now,
		NotBefore: now - 60,
		ExpiresAt: now + 3600,
	}
}

// TestNewHMACSignerRejectsEmptyKey 验证空密钥被拒绝：
// 空密钥会让 HMAC 丧失认证能力，任意载荷都可被伪造。
func TestNewHMACSignerRejectsEmptyKey(t *testing.T) {
	if _, err := NewHMACSigner(HS256, nil); err == nil {
		t.Fatal("nil 密钥必须返回错误")
	}

	if _, err := NewHMACSigner(HS256, []byte{}); err == nil {
		t.Fatal("空密钥必须返回错误")
	}
}

// TestNewHMACSignerRejectsShortKey 验证低于 RFC 7518 最小长度的密钥被拒绝。
func TestNewHMACSignerRejectsShortKey(t *testing.T) {
	cases := []struct {
		algorithm Algorithm
		key       string
	}{
		{algorithm: HS256, key: strings.Repeat("a", 31)},
		{algorithm: HS384, key: strings.Repeat("a", 47)},
		{algorithm: HS512, key: strings.Repeat("a", 63)},
	}

	for _, testCase := range cases {
		t.Run(string(testCase.algorithm), func(t *testing.T) {
			if _, err := NewHMACSigner(testCase.algorithm, []byte(testCase.key)); err == nil {
				t.Fatalf("%s 短密钥必须返回错误", testCase.algorithm)
			}
		})
	}
}

// TestNewHMACSignerUsesExpectedAlgorithm 验证三种受支持算法生成的头部算法。
func TestNewHMACSignerUsesExpectedAlgorithm(t *testing.T) {
	cases := []struct {
		name      string
		algorithm Algorithm
	}{
		{name: "HS256", algorithm: HS256},
		{name: "HS384", algorithm: HS384},
		{name: "HS512", algorithm: HS512},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			signer, err := NewHMACSigner(testCase.algorithm, []byte(testHMACKey))
			if err != nil {
				t.Fatalf("创建签名器失败: %v", err)
			}

			if got := signer.GetHeader().GetAlgorithm(); got != testCase.algorithm {
				t.Fatalf("算法 = %q, want %q", got, testCase.algorithm)
			}
		})
	}
}

// TestNewHMACSignerRejectsUnsupportedAlgorithm 验证未知算法被拒绝。
//
// 静默回退到 HS256 会隐藏配置错误，也可能造成签发方与校验方算法不一致，
// 因此这里采用 fail-fast 策略。
func TestNewHMACSignerRejectsUnsupportedAlgorithm(t *testing.T) {
	cases := []struct {
		name      string
		algorithm Algorithm
	}{
		{name: "非对称算法", algorithm: Algorithm("RS256")},
		{name: "空算法", algorithm: Algorithm("")},
		{name: "大小写不匹配", algorithm: Algorithm("hs256")},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			signer, err := NewHMACSigner(testCase.algorithm, []byte(testHMACKey))
			if !errors.Is(err, ErrUnsupportedMethod) {
				t.Fatalf("err = %v, want ErrUnsupportedMethod", err)
			}
			if signer != nil {
				t.Fatal("不支持算法时必须返回 nil 签名器")
			}
		})
	}
}

// TestHMACSignerRejectsNilClaims 验证签发 nil 声明返回 ErrNilClaims。
func TestHMACSignerRejectsNilClaims(t *testing.T) {
	signer, err := NewHMACSigner(HS256, []byte(testHMACKey))
	if err != nil {
		t.Fatalf("创建签名器失败: %v", err)
	}

	if _, err = signer.Sign(nil); !errors.Is(err, ErrNilClaims) {
		t.Fatalf("err = %v, want ErrNilClaims", err)
	}
}

// TestHMACSignerProducesThreeSegments 验证令牌是标准的三段式结构。
func TestHMACSignerProducesThreeSegments(t *testing.T) {
	signer, err := NewHMACSigner(HS256, []byte(testHMACKey))
	if err != nil {
		t.Fatalf("创建签名器失败: %v", err)
	}

	token, err := signer.Sign(createTestClaims())
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}

	if segments := strings.Split(string(token), "."); len(segments) != 3 {
		t.Fatalf("令牌段数 = %d, want 3: %s", len(segments), token)
	}
}

// TestHMACSignerVerifyRoundTrip 验证同密钥签发的令牌可通过验签。
func TestHMACSignerVerifyRoundTrip(t *testing.T) {
	for _, algorithm := range []Algorithm{HS256, HS384, HS512} {
		t.Run(string(algorithm), func(t *testing.T) {
			signer, err := NewHMACSigner(algorithm, []byte(testHMACKey))
			if err != nil {
				t.Fatalf("创建签名器失败: %v", err)
			}

			token, err := signer.Sign(createTestClaims())
			if err != nil {
				t.Fatalf("签发失败: %v", err)
			}

			segments := strings.Split(string(token), ".")
			signature := decodeBase64URL(t, segments[2])

			if !signer.Verify([]byte(segments[0]+"."+segments[1]), signature) {
				t.Fatal("同密钥验签应通过")
			}
		})
	}
}

// TestHMACSignerVerifyRejectsTamperedData 验证载荷被篡改后验签失败。
func TestHMACSignerVerifyRejectsTamperedData(t *testing.T) {
	signer, err := NewHMACSigner(HS256, []byte(testHMACKey))
	if err != nil {
		t.Fatalf("创建签名器失败: %v", err)
	}

	token, err := signer.Sign(createTestClaims())
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}

	segments := strings.Split(string(token), ".")
	signature := decodeBase64URL(t, segments[2])

	if signer.Verify([]byte(segments[0]+"."+segments[1]+"tampered"), signature) {
		t.Fatal("篡改数据后验签必须失败")
	}

	// 篡改签名本身同样必须失败。
	tampered := append([]byte(nil), signature...)
	tampered[0] ^= 0xFF
	if signer.Verify([]byte(segments[0]+"."+segments[1]), tampered) {
		t.Fatal("篡改签名后验签必须失败")
	}
}

// TestHMACSignerVerifyRejectsDifferentKey 验证不同密钥签发的令牌无法互相验签。
func TestHMACSignerVerifyRejectsDifferentKey(t *testing.T) {
	signerA, err := NewHMACSigner(HS256, []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	if err != nil {
		t.Fatalf("创建签名器失败: %v", err)
	}
	signerB, err := NewHMACSigner(HS256, []byte("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"))
	if err != nil {
		t.Fatalf("创建签名器失败: %v", err)
	}

	token, err := signerA.Sign(createTestClaims())
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}

	segments := strings.Split(string(token), ".")
	signature := decodeBase64URL(t, segments[2])

	if signerB.Verify([]byte(segments[0]+"."+segments[1]), signature) {
		t.Fatal("不同密钥验签必须失败")
	}
}

// TestHMACSignerConcurrentUse 验证签名器通过 sync.Pool 复用哈希对象时并发安全，
// 且并发签发结果完全一致（HMAC 是确定性算法）。
func TestHMACSignerConcurrentUse(t *testing.T) {
	signer, err := NewHMACSigner(HS512, []byte(testHMACKey))
	if err != nil {
		t.Fatalf("创建签名器失败: %v", err)
	}

	claims := createTestClaims()
	reference, err := signer.Sign(claims)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}

	const goroutines, iterations = 16, 50
	var waitGroup sync.WaitGroup
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for j := 0; j < iterations; j++ {
				token, err := signer.Sign(claims)
				if err != nil {
					errs <- err
					return
				}
				if string(token) != string(reference) {
					errs <- errors.New("并发签发结果不一致")
					return
				}
			}
		}()
	}

	waitGroup.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("并发签发失败: %v", err)
	}
}

// BenchmarkHMACSignerSign 测量签发吞吐与内存分配。
func BenchmarkHMACSignerSign(b *testing.B) {
	signer, err := NewHMACSigner(HS256, []byte(testHMACKey))
	if err != nil {
		b.Fatalf("创建签名器失败: %v", err)
	}

	claims := createTestClaims()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if _, err = signer.Sign(claims); err != nil {
			b.Fatalf("签发失败: %v", err)
		}
	}
}
