# JWT

`middleware/jwt` 提供最小可用的 JWT（JSON Web Token）签发与校验能力，只内置
HMAC 系列算法（HS256 / HS384 / HS512），刻意不引入非对称算法与密钥管理体系。

---

## 设计边界

| 项 | 说明 |
| --- | --- |
| 算法 | 仅 HMAC：`HS256`、`HS384`、`HS512` |
| 头部 | 固定 `typ=JWT`，`alg` 必须与签名器一致 |
| 载荷 | Base64URL（无填充）编码的 JSON |
| 签名比较 | `hmac.Equal`，常数时间 |
| 校验目标 | 必须是非 nil 指针，避免静默反序列化到 map |
| 时钟偏差 | 默认 1 分钟，支持原子读写 |

不支持 `none` 算法，也不支持从令牌头部「自动选择」算法 —— 这是规避 alg 混淆
攻击的关键设计。

---

## 签名

```go
import "gitee.com/leminewx/leego/middleware/jwt"

signer, err := jwt.NewHMACSigner(jwt.HS256, []byte("your-256-bit-secret"))
if err != nil {
	log.Fatal(err)
}
```

`NewHMACSigner` 的校验：

- 算法必须是 `HS256` / `HS384` / `HS512`；
- 密钥不能为空；
- 返回 `Signer` 接口：`GetHeader()`、`Sign(claims)`、`Verify(src, dst)`。

签名流程：

1. `NewHeader(algorithm)` 生成 `{"typ":"JWT","alg":"HS256"}` 并做 Base64URL 编码；
2. 载荷 JSON 序列化后 Base64URL 编码；
3. 对 `header.payload` 做 HMAC；
4. 拼接为 `header.payload.signature`。

`Sign` 内部使用 `sync.Pool` 复用 `hmac.Hash`，高并发签发时避免重复分配。

---

## 声明

### 标准声明

```go
type StandardClaims struct {
	Id        string   `json:"jti,omitempty"`
	Issuer    string   `json:"iss,omitempty"`
	Subject   string   `json:"sub,omitempty"`
	Audiences []string `json:"aud,omitempty"`
	IssuedAt  int64    `json:"iat,omitempty"`
	NotBefore int64    `json:"nbf,omitempty"`
	ExpiresAt int64    `json:"exp,omitempty"`
}
```

时间字段均为 Unix 秒。校验规则（`ValidateWithLeeway`）：

| 字段 | 规则 | 为 0 时 |
| --- | --- | --- |
| `exp` | `exp + leeway < now` → `ErrExpiredToken` | 不限制 |
| `nbf` | `nbf - leeway > now` → `ErrNotValidYet` | 不限制 |
| `iat` | `iat - leeway > now` → `ErrInvalidIssuanceTime` | 不限制 |

> `leeway` 按秒向下取整，因此小于 1 秒的偏差等于无偏差。

### 自定义声明

```go
type userClaims struct {
	*jwt.StandardClaims
	Email string `json:"email"`
	Role  string `json:"role"`
}
```

`Claims` 接口只要求两个方法：

```go
type Claims interface {
	Validate() error
	ValidateWithIssuerAndAudiences(expectedIssuer, expectedAudience string) error
}
```

嵌入 `*jwt.StandardClaims` 即可满足。若需要携带时钟偏差，额外实现
`ValidateWithLeeway(time.Duration) error`，解析器会自动使用。

---

## 解析与校验

```go
parser := jwt.NewParser()
parser.SetClockSkew(30 * time.Second) // 默认 1 分钟；负值归零

claims := &jwt.StandardClaims{}
err := parser.ParseAndValidateStandardClaims(signer, token, claims)
```

三个入口：

| 方法 | 校验内容 |
| --- | --- |
| `Parse` | 结构、头部、算法、签名；**不**校验时间与签发者 |
| `ParseAndValidateStandardClaims` | 上述 + 时间字段（含时钟偏差） |
| `ParseAndValidateStandardClaimsWithIssuerAndAudiences` | 上述 + `iss` / `aud` |

`iss` / `aud` 规则：

- `expectedIssuer` 非空时，`iss` 必须完全一致；
- `expectedAudience` 非空时，`aud` 列表中必须包含该项；
- 任一参数为空表示「不限制该维度」。

### 校验顺序

```
1. signer / claims 非空
2. 令牌必须是三段（header.payload.signature）
3. 三段均非空
4. 头部 Base64URL 解码 + JSON 反序列化     → ErrInvalidHeader
5. alg 非空                                → ErrInvalidAlgorithm
6. alg 与签名器一致                        → ErrAlgorithmMismatch
7. typ 为空或为 "JWT"                      → ErrInvalidType
8. 载荷 Base64URL 解码                     → ErrInvalidPayload
9. 签名 Base64URL 解码                     → ErrInvalidSignature
10. hmac.Equal 比较                        → ErrInvalidSignature
11. 载荷反序列化到声明指针                  → ErrValidationFailed / ErrNilClaims
12. 时间字段（含偏差）                      → ErrExpiredToken / ErrNotValidYet / ErrInvalidIssuanceTime
13. 签发者与受众                            → ErrInvalidIssuer / ErrInvalidAudience
```

先解码载荷再校验签名，是为了尽早区分「格式错误」与「签名伪造」；两者都不应
继续解析业务声明。

### 时钟偏差

- 默认 1 分钟，用于容忍分布式节点的时钟漂移；
- `SetClockSkew` 与 `GetClockSkew` 通过原子变量实现，可与解析并发调用；
- 偏差只影响时间字段，不影响签发者与受众匹配；
- 带签发者/受众的校验会沿用同一偏差，避免「时间校验通过、签发者校验却因过期
  而失败」的不一致。

---

## 端到端示例

```go
signer, _ := jwt.NewHMACSigner(jwt.HS384, []byte(secret))
parser := jwt.NewParser()

type loginClaims struct {
	*jwt.StandardClaims
	Name string `json:"name"`
}

// 签发
token, err := signer.Sign(&loginClaims{
	StandardClaims: &jwt.StandardClaims{
		Issuer:    "leego",
		Subject:   "user-1",
		Audiences: []string{"leego-api"},
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
		Id:        "token-id-1",
	},
	Name: "alice",
})
if err != nil {
	// 处理
}

// 校验
parsed := &loginClaims{}
if err = parser.ParseAndValidateStandardClaimsWithIssuerAndAudiences(
	signer, string(token), "leego", "leego-api", parsed,
); err != nil {
	// 处理
}
```

### 作为中间件使用

```go
func authenticate(signer jwt.Signer, parser jwt.Parser) leego.MiddlewareFunc {
	return func(next leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			token := strings.TrimPrefix(ctx.GetRequestHeader("Authorization"), "Bearer ")
			if token == "" {
				ctx.ResponseError(http.StatusUnauthorized, "缺少令牌")
				return
			}

			claims := &jwt.StandardClaims{}
			if err := parser.ParseAndValidateStandardClaims(signer, token, claims); err != nil {
				ctx.ResponseError(http.StatusUnauthorized, "令牌无效: "+err.Error())
				return
			}

			ctx.SetJWTClaims(claims) // 供业务处理器读取
			next(ctx)
		}
	}
}

api := engine.NewGroup("/api")
api.UseMiddlewares(authenticate(signer, parser))
```

业务侧：

```go
if claims, ok := ctx.GetJWTClaims().(*jwt.StandardClaims); ok {
	_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{"subject": claims.Subject})
}
```

---

## 错误表

| 哨兵错误 | 触发场景 |
| --- | --- |
| `ErrInvalidToken` | 令牌不是三段结构 |
| `ErrMalformedToken` | 三段中存在空段 |
| `ErrInvalidHeader` | 头部非 Base64URL 或非合法 JSON |
| `ErrInvalidAlgorithm` | 头部 `alg` 为空 |
| `ErrAlgorithmMismatch` | `alg` 与签名器算法不一致 |
| `ErrInvalidType` | `typ` 非空且不是 `JWT` |
| `ErrInvalidPayload` | 载荷非 Base64URL 或 JSON 非法 |
| `ErrInvalidSignature` | 签名段非法、长度不符或校验失败 |
| `ErrMissingSignature` | 签名段为空（`ErrMalformedToken` 的语义细化） |
| `ErrNilClaims` | 声明为 nil 或 typed nil |
| `ErrExpiredToken` | 已过期 |
| `ErrNotValidYet` | 尚未生效 |
| `ErrInvalidIssuanceTime` | 签发时间在未来 |
| `ErrInvalidIssuer` | 签发者不匹配 |
| `ErrInvalidAudience` | 受众不匹配 |
| `ErrValidationFailed` | 载荷反序列化失败（字段类型不符等） |
| `ErrUnsupportedMethod` | 签名算法不受支持 |

统一使用 `errors.Is` 判断，便于在中间件中把错误映射为不同状态码。

---

## 安全注意事项

1. **密钥强度**：HMAC-SHA256 建议至少 32 字节随机密钥；不要把密钥写进代码库，
   使用环境变量或配置中心。
2. **不要信任 `alg`**：框架要求头部算法与签名器一致，禁止客户端选择算法。
3. **校验 `iss` / `aud`**：多租户或多服务场景必须校验，避免令牌跨服务复用。
4. **`exp` 必须设置**：为 0 表示不限制，生产环境不应签发永不过期的令牌。
5. **`jti` 防重放**：需要一次性令牌时，由业务侧维护已使用 `jti` 的集合；
   框架只提供字段，不内置存储。
6. **错误信息脱敏**：对外返回统一文案，避免通过错误差异探测令牌结构。
7. **传输安全**：JWT 只做签名不做加密，必须走 HTTPS；载荷中不要放敏感信息。

---

## 常见问题

**Q：为什么 `iat` 在未来会报错？**
防止客户端伪造「未来签发」的令牌绕过 `nbf` 判断。若确实存在时钟漂移，调大
`SetClockSkew`。

**Q：能否使用 RS256？**
当前版本只内置 HMAC。需要非对称签名时，可实现自己的 `Signer` 接口（`GetHeader`
/ `Sign` / `Verify`），解析器只依赖接口，不依赖具体实现。

**Q：`Validate()` 与 `ValidateWithLeeway()` 有什么区别？**
`Validate()` 等价于 `ValidateWithLeeway(0)`；带签发者/受众的版本也提供
`ValidateWithIssuerAndAudiencesAndLeeway` 用于显式传入偏差。
