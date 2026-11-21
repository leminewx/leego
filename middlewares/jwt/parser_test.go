package jwt

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// createTestSigner 创建 HS256 测试签名器，创建失败时直接终止测试。
func createTestSigner(t *testing.T) Signer {
	t.Helper()

	signer, err := NewHMACSigner(HS256, []byte(testHMACKey))
	if err != nil {
		t.Fatalf("创建签名器失败: %v", err)
	}

	return signer
}

// createSignedToken 用指定签名器签发测试声明并返回令牌字符串。
func createSignedToken(t *testing.T, signer Signer, claims Claims) string {
	t.Helper()

	token, err := signer.Sign(claims)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}

	return string(token)
}

// createTokenWithHeader 用指定的头部 JSON 构造令牌，
// 用于覆盖算法混淆、非法 typ 等解析分支。
func createTokenWithHeader(t *testing.T, signer Signer, headerJSON string, claimsJSON []byte) string {
	t.Helper()

	hmac, ok := signer.(*hmacSigner)
	if !ok {
		t.Fatal("测试辅助仅支持 HMAC 签名器")
	}

	encodedHeader := base64.RawURLEncoding.EncodeToString([]byte(headerJSON))
	encodedPayload := base64.RawURLEncoding.EncodeToString(claimsJSON)
	signingInput := encodedHeader + "." + encodedPayload

	signature, err := hmac.sign([]byte(signingInput))
	if err != nil {
		t.Fatalf("计算签名失败: %v", err)
	}

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}

// TestNewParserUsesDefaultClockSkew 验证新建解析器的默认时钟偏差为 1 分钟。
func TestNewParserUsesDefaultClockSkew(t *testing.T) {
	parser := NewParser()
	if got := parser.GetClockSkew(); got != _DEFAULT_CLOCK_SKEW {
		t.Fatalf("GetClockSkew() = %v, want %v", got, _DEFAULT_CLOCK_SKEW)
	}
	if _DEFAULT_CLOCK_SKEW != time.Minute {
		t.Fatalf("_DEFAULT_CLOCK_SKEW = %v, want 1m", _DEFAULT_CLOCK_SKEW)
	}
}

// TestSetClockSkewNormalizesNegative 验证负偏差被修正为 0。
func TestSetClockSkewNormalizesNegative(t *testing.T) {
	parser := NewParser()

	parser.SetClockSkew(-time.Hour)
	if got := parser.GetClockSkew(); got != 0 {
		t.Fatalf("负偏差应归零，得到 %v", got)
	}

	parser.SetClockSkew(30 * time.Second)
	if got := parser.GetClockSkew(); got != 30*time.Second {
		t.Fatalf("GetClockSkew() = %v, want 30s", got)
	}
}

// TestParserRejectsNilArguments 验证 nil 签名器与 nil 声明被拒绝。
func TestParserRejectsNilArguments(t *testing.T) {
	parser := NewParser()
	signer := createTestSigner(t)
	token := createSignedToken(t, signer, createTestClaims())

	if err := parser.Parse(nil, token, createTestClaims()); err == nil {
		t.Fatal("nil 签名器必须返回错误")
	}

	if err := parser.Parse(signer, token, nil); !errors.Is(err, ErrNilClaims) {
		t.Fatalf("err = %v, want ErrNilClaims", err)
	}
}

// TestParserRejectsMalformedStruct 验证结构错误的令牌返回语义明确的错误，
// 而不是统一报“签名无效”。
func TestParserRejectsMalformedStruct(t *testing.T) {
	parser := NewParser()
	signer := createTestSigner(t)
	valid := createSignedToken(t, signer, createTestClaims())
	segments := strings.Split(valid, ".")

	cases := []struct {
		name      string
		token     string
		expectErr error
	}{
		{name: "空字符串", token: "", expectErr: ErrInvalidToken},
		{name: "仅两段", token: segments[0] + "." + segments[1], expectErr: ErrInvalidToken},
		{name: "四段", token: valid + ".extra", expectErr: ErrInvalidToken},
		{name: "首段为空", token: "." + segments[1] + "." + segments[2], expectErr: ErrMalformedToken},
		{name: "载荷段为空", token: segments[0] + ".." + segments[2], expectErr: ErrMalformedToken},
		{name: "签名段为空", token: segments[0] + "." + segments[1] + ".", expectErr: ErrMalformedToken},
		{name: "头部非 Base64URL", token: "@@@." + segments[1] + "." + segments[2], expectErr: ErrInvalidHeader},
		{name: "载荷非 Base64URL", token: segments[0] + ".@@@." + segments[2], expectErr: ErrInvalidPayload},
		{name: "签名非 Base64URL", token: segments[0] + "." + segments[1] + ".@@@", expectErr: ErrInvalidSignature},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := parser.Parse(signer, testCase.token, &StandardClaims{})
			if !errors.Is(err, testCase.expectErr) {
				t.Fatalf("err = %v, want %v", err, testCase.expectErr)
			}
		})
	}
}

// TestParserRejectsInvalidHeader 验证头部 JSON、算法、类型异常的各类分支。
func TestParserRejectsInvalidHeader(t *testing.T) {
	parser := NewParser()
	signer := createTestSigner(t)
	claimsJSON, err := json.Marshal(createTestClaims())
	if err != nil {
		t.Fatalf("序列化声明失败: %v", err)
	}

	cases := []struct {
		name      string
		header    string
		expectErr error
	}{
		{name: "非 JSON 头部", header: "not-json", expectErr: ErrInvalidHeader},
		{name: "空 alg", header: `{"typ":"JWT","alg":""}`, expectErr: ErrInvalidAlgorithm},
		{name: "alg 不匹配", header: `{"typ":"JWT","alg":"HS512"}`, expectErr: ErrAlgorithmMismatch},
		{name: "非法 typ", header: `{"typ":"JWE","alg":"HS256"}`, expectErr: ErrInvalidType},
		{name: "typ 大小写不敏感", header: `{"typ":"jwt","alg":"HS256"}`},
		{name: "省略 typ", header: `{"alg":"HS256"}`},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			token := createTokenWithHeader(t, signer, testCase.header, claimsJSON)
			err := parser.Parse(signer, token, &StandardClaims{})
			if !errors.Is(err, testCase.expectErr) {
				t.Fatalf("err = %v, want %v", err, testCase.expectErr)
			}
		})
	}
}

// TestParserRejectsAlgorithmConfusion 验证用另一种算法的令牌请求解析时，
// 返回 ErrAlgorithmMismatch，从而杜绝 alg 混淆攻击。
func TestParserRejectsAlgorithmConfusion(t *testing.T) {
	parser := NewParser()

	hs512Signer, err := NewHMACSigner(HS512, []byte(testHMACKey))
	if err != nil {
		t.Fatalf("创建签名器失败: %v", err)
	}
	token := createSignedToken(t, hs512Signer, createTestClaims())

	if err = parser.Parse(createTestSigner(t), token, &StandardClaims{}); !errors.Is(err, ErrAlgorithmMismatch) {
		t.Fatalf("err = %v, want ErrAlgorithmMismatch", err)
	}
}

// TestParserRejectsTamperedSignatureAndPayload 验证篡改载荷或签名均被拒绝。
func TestParserRejectsTamperedSignatureAndPayload(t *testing.T) {
	parser := NewParser()
	signer := createTestSigner(t)
	valid := createSignedToken(t, signer, createTestClaims())
	segments := strings.Split(valid, ".")

	// 篡改载荷（修改一位 Base64 字符），签名保持不变。
	payload := []byte(segments[1])
	payload[0] ^= 0x01
	tamperedPayload := segments[0] + "." + string(payload) + "." + segments[2]
	if err := parser.Parse(signer, tamperedPayload, &StandardClaims{}); !errors.Is(err, ErrInvalidPayload) && !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("篡改载荷 err = %v, want ErrInvalidPayload/ErrInvalidSignature", err)
	}

	// 篡改签名。
	signature := []byte(segments[2])
	signature[0] ^= 0x01
	tamperedSignature := segments[0] + "." + segments[1] + "." + string(signature)
	if err := parser.Parse(signer, tamperedSignature, &StandardClaims{}); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("篡改签名 err = %v, want ErrInvalidSignature", err)
	}
}

// TestParserRejectsNonPointerClaims 验证声明目标必须是可写入的非 nil 指针。
func TestParserRejectsNonPointerClaims(t *testing.T) {
	parser := NewParser()
	signer := createTestSigner(t)
	token := createSignedToken(t, signer, createTestClaims())

	// 值类型实现 Claims，但不是指针，无法反序列化。
	if err := parser.Parse(signer, token, StandardClaims{}); !errors.Is(err, ErrNilClaims) {
		t.Fatalf("值类型声明 err = %v, want ErrNilClaims", err)
	}

	// typed nil 指针：接口非 nil，但底层指针为 nil。
	var typedNil *StandardClaims
	if err := parser.Parse(signer, token, typedNil); !errors.Is(err, ErrNilClaims) {
		t.Fatalf("typed nil 声明 err = %v, want ErrNilClaims", err)
	}
}

// TestParserParsesStandardClaims 验证标准声明的端到端解析一致性。
func TestParserParsesStandardClaims(t *testing.T) {
	parser := NewParser()
	signer := createTestSigner(t)

	expected := createTestClaims()
	token := createSignedToken(t, signer, expected)

	actual := &StandardClaims{}
	if err := parser.Parse(signer, token, actual); err != nil {
		t.Fatalf("解析失败: %v", err)
	}

	if actual.Id != expected.Id || actual.Issuer != expected.Issuer || actual.Subject != expected.Subject {
		t.Fatalf("字符串声明不一致: %+v != %+v", actual, expected)
	}
	if len(actual.Audiences) != 1 || actual.Audiences[0] != expected.Audiences[0] {
		t.Fatalf("受众声明不一致: %v != %v", actual.Audiences, expected.Audiences)
	}
	if actual.IssuedAt != expected.IssuedAt || actual.NotBefore != expected.NotBefore || actual.ExpiresAt != expected.ExpiresAt {
		t.Fatalf("时间声明不一致: %+v != %+v", actual, expected)
	}
}

// TestParserParsesCustomizedClaims 验证自定义声明（嵌入标准声明）可正确反序列化。
func TestParserParsesCustomizedClaims(t *testing.T) {
	parser := NewParser()
	signer := createTestSigner(t)

	expected := &customizedClaims{
		StandardClaims: createTestClaims(),
		Username:       "alice",
		Team:           "platform",
		Role:           "admin",
	}
	token := createSignedToken(t, signer, expected)

	actual := &customizedClaims{}
	if err := parser.Parse(signer, token, actual); err != nil {
		t.Fatalf("解析失败: %v", err)
	}

	if actual.StandardClaims == nil {
		t.Fatal("嵌入的标准声明未被分配")
	}
	if actual.Username != "alice" || actual.Team != "platform" || actual.Role != "admin" {
		t.Fatalf("自定义字段不一致: %+v", actual)
	}
	if actual.Id != expected.Id || actual.Issuer != expected.Issuer {
		t.Fatalf("标准字段不一致: %+v", actual.StandardClaims)
	}
}

// TestParseAndValidateStandardClaimsChecksTimeFields 验证解析并校验收敛了时间字段校验。
func TestParseAndValidateStandardClaimsChecksTimeFields(t *testing.T) {
	parser := NewParser()
	signer := createTestSigner(t)

	t.Run("有效令牌", func(t *testing.T) {
		token := createSignedToken(t, signer, createTestClaims())
		if err := parser.ParseAndValidateStandardClaims(signer, token, &StandardClaims{}); err != nil {
			t.Fatalf("有效令牌不应报错: %v", err)
		}
	})

	t.Run("过期令牌", func(t *testing.T) {
		claims := createTestClaims()
		claims.ExpiresAt = time.Now().Add(-2 * time.Minute).Unix()
		token := createSignedToken(t, signer, claims)

		err := parser.ParseAndValidateStandardClaims(signer, token, &StandardClaims{})
		if !errors.Is(err, ErrExpiredToken) {
			t.Fatalf("err = %v, want ErrExpiredToken", err)
		}
	})

	t.Run("偏差容忍", func(t *testing.T) {
		claims := createTestClaims()
		claims.ExpiresAt = time.Now().Add(-30 * time.Second).Unix()
		token := createSignedToken(t, signer, claims)

		// 默认偏差 1 分钟：刚好过期的令牌仍被接受。
		if err := parser.ParseAndValidateStandardClaims(signer, token, &StandardClaims{}); err != nil {
			t.Fatalf("偏差容忍范围内不应报错: %v", err)
		}

		parser.SetClockSkew(0)
		if err := parser.ParseAndValidateStandardClaims(signer, token, &StandardClaims{}); !errors.Is(err, ErrExpiredToken) {
			t.Fatalf("无偏差时 err = %v, want ErrExpiredToken", err)
		}
	})
}

// TestParseAndValidateWithIssuerAndAudience 验证签发者与受众校验分支。
func TestParseAndValidateWithIssuerAndAudience(t *testing.T) {
	parser := NewParser()
	signer := createTestSigner(t)

	t.Run("标准声明", func(t *testing.T) {
		token := createSignedToken(t, signer, createTestClaims())

		if err := parser.ParseAndValidateStandardClaimsWithIssuerAndAudiences(signer, token, "issuer", "audience", &StandardClaims{}); err != nil {
			t.Fatalf("匹配时不应报错: %v", err)
		}

		err := parser.ParseAndValidateStandardClaimsWithIssuerAndAudiences(signer, token, "other", "audience", &StandardClaims{})
		if !errors.Is(err, ErrInvalidIssuer) {
			t.Fatalf("签发者不匹配 err = %v, want ErrInvalidIssuer", err)
		}

		err = parser.ParseAndValidateStandardClaimsWithIssuerAndAudiences(signer, token, "issuer", "other", &StandardClaims{})
		if !errors.Is(err, ErrInvalidAudience) {
			t.Fatalf("受众不匹配 err = %v, want ErrInvalidAudience", err)
		}
	})

	t.Run("自定义声明", func(t *testing.T) {
		claims := &customizedClaims{StandardClaims: createTestClaims(), Username: "bob"}
		token := createSignedToken(t, signer, claims)

		if err := parser.ParseAndValidateStandardClaimsWithIssuerAndAudiences(signer, token, "issuer", "audience", &customizedClaims{}); err != nil {
			t.Fatalf("匹配时不应报错: %v", err)
		}

		err := parser.ParseAndValidateStandardClaimsWithIssuerAndAudiences(signer, token, "other", "audience", &customizedClaims{})
		if !errors.Is(err, ErrInvalidIssuer) {
			t.Fatalf("签发者不匹配 err = %v, want ErrInvalidIssuer", err)
		}
	})
}

// TestParseAndValidateIssuerAndAudienceHonorsClockSkew 验证签发者/受众校验沿用时钟偏差。
//
// 回归场景：解析器先按偏差校验时间，再校验签发者与受众；若第二次校验丢弃偏差，
// 则「仅轻微过期」的令牌会被误判为过期，与单独做时间校验的结果不一致。
func TestParseAndValidateIssuerAndAudienceHonorsClockSkew(t *testing.T) {
	parser := NewParser()
	signer := createTestSigner(t)

	claims := createTestClaims()
	claims.ExpiresAt = time.Now().Add(-30 * time.Second).Unix()
	token := createSignedToken(t, signer, claims)

	if err := parser.ParseAndValidateStandardClaimsWithIssuerAndAudiences(signer, token, "issuer", "audience", &StandardClaims{}); err != nil {
		t.Fatalf("偏差容忍范围内不应报错: %v", err)
	}

	parser.SetClockSkew(0)
	err := parser.ParseAndValidateStandardClaimsWithIssuerAndAudiences(signer, token, "issuer", "audience", &StandardClaims{})
	if !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("无偏差时 err = %v, want ErrExpiredToken", err)
	}
}

// TestParserConcurrentClockSkewUpdates 验证时钟偏差的读写与解析可并发执行。
func TestParserConcurrentClockSkewUpdates(t *testing.T) {
	parser := NewParser()
	signer := createTestSigner(t)
	token := createSignedToken(t, signer, createTestClaims())

	var waitGroup sync.WaitGroup

	for writer := 0; writer < 4; writer++ {
		waitGroup.Go(func() {
			for index := 0; index < 50; index++ {
				parser.SetClockSkew(time.Duration(index) * time.Millisecond)
			}
		})
	}

	for reader := 0; reader < 8; reader++ {
		waitGroup.Go(func() {
			for index := 0; index < 50; index++ {
				_ = parser.GetClockSkew()
				if err := parser.Parse(signer, token, &StandardClaims{}); err != nil {
					t.Errorf("并发解析失败: %v", err)
					return
				}
			}
		})
	}

	waitGroup.Wait()
}

// TestParseAndValidatePropagatesParseError 验证解析失败时不会继续做声明校验。
//
// 篡改位置选在签名段中部：Base64URL 末尾字符可能只携带不足 6 位有效信息，
// 改动它未必会改变解码后的字节，因此不能用“改最后一个字符”来构造篡改样本。
func TestParseAndValidatePropagatesParseError(t *testing.T) {
	parser := NewParser()
	signer := createTestSigner(t)

	token := createSignedToken(t, signer, createTestClaims())

	segments := strings.Split(token, ".")
	if len(segments) != 3 {
		t.Fatalf("令牌段数 = %d, want 3", len(segments))
	}

	signature := []byte(segments[2])
	replacement := byte('A')
	middle := len(signature) / 2
	if signature[middle] == replacement {
		replacement = 'B'
	}
	signature[middle] = replacement

	tampered := segments[0] + "." + segments[1] + "." + string(signature)
	if tampered == token {
		t.Fatal("篡改后的令牌必须与原令牌不同")
	}

	if err := parser.ParseAndValidateStandardClaims(signer, tampered, &StandardClaims{}); err == nil {
		t.Fatal("篡改令牌必须返回错误")
	}

	err := parser.ParseAndValidateStandardClaimsWithIssuerAndAudiences(signer, tampered, "issuer", "audience", &StandardClaims{})
	if err == nil {
		t.Fatal("篡改令牌必须返回错误")
	}
}

// TestParserConcurrentUse 验证解析器可被多协程并发复用。
func TestParserConcurrentUse(t *testing.T) {
	parser := NewParser()
	signer := createTestSigner(t)
	token := createSignedToken(t, signer, createTestClaims())

	const goroutines, iterations = 16, 50
	results := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			for j := 0; j < iterations; j++ {
				if err := parser.Parse(signer, token, &StandardClaims{}); err != nil {
					results <- err
					return
				}
			}
			results <- nil
		}()
	}

	for i := 0; i < goroutines; i++ {
		if err := <-results; err != nil {
			t.Fatalf("并发解析失败: %v", err)
		}
	}
}

// BenchmarkParserParse 测量解析（含验签）的耗时与内存分配。
func BenchmarkParserParse(b *testing.B) {
	parser := NewParser()
	signer, err := NewHMACSigner(HS256, []byte(testHMACKey))
	if err != nil {
		b.Fatalf("创建签名器失败: %v", err)
	}

	token, err := signer.Sign(&StandardClaims{
		Id:        "jti-1",
		Issuer:    "issuer",
		Subject:   "subject",
		Audiences: []string{"audience"},
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		b.Fatalf("签发失败: %v", err)
	}

	tokenString := string(token)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err = parser.Parse(signer, tokenString, &StandardClaims{}); err != nil {
			b.Fatalf("解析失败: %v", err)
		}
	}
}
