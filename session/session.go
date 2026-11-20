// Package session 提供服务端会话中间件。
//
// 设计要点：
//   - 服务端持有数据：Cookie 里只保存会话 ID 与其 HMAC-SHA256 签名，
//     客户端无法伪造或读取业务数据；
//   - 会话 ID 由 crypto/rand 生成，登录等敏感操作可通过 Regenerate 轮换，
//     防止会话固定攻击；
//   - 请求结束时统一回写数据；Cookie 在响应头发送前同步，
//     避免 net/http 在 WriteHeader 之后丢弃响应头修改；
//   - 只依赖标准库与根包，Store 为接口，可替换为任意外部存储实现。
package session

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/response"
)

const (
	// _DEFAULT_NAME 为默认 Cookie 名称。
	_DEFAULT_NAME = "leego_session"
	// _DEFAULT_TTL 为默认会话有效期。
	_DEFAULT_TTL = 24 * time.Hour
	// _DEFAULT_PATH 为默认 Cookie 生效路径。
	_DEFAULT_PATH = "/"
	// _ID_BYTES 为会话 ID 的随机字节数；base64url 编码后为 43 个字符。
	_ID_BYTES = 32
	// _SECRET_BYTES 为未配置密钥时自动生成的随机密钥长度。
	_SECRET_BYTES = 32
)

// ErrNilStore 表示创建中间件时未提供存储。
var ErrNilStore = errors.New("session: store must not be nil")

// ErrRandomSource 表示系统随机源不可用。
//
// 此时既不能生成会话 ID，也不能生成签名密钥；
// 继续运行会退化为“空密钥 + 可预测 ID”，因此创建中间件时直接失败。
var ErrRandomSource = errors.New("session: crypto/rand is unavailable")

// Values 是会话键值对。
type Values map[string]any

// Store 定义服务端会话存储。
//
// 实现必须并发安全；Get 在会话不存在或已过期时返回 ErrNotFound。
type Store interface {
	// Get 读取会话数据。
	Get(ctx context.Context, id string) (Values, error)
	// Save 写入会话数据并刷新过期时间。
	Save(ctx context.Context, id string, values Values, ttl time.Duration) error
	// Destroy 销毁会话数据。
	Destroy(ctx context.Context, id string) error
}

// Options 定义会话行为。
type Options struct {
	// Name 为 Cookie 名称，空值时使用 leego_session。
	Name string
	// TTL 为会话有效期，非正数时使用 24h。
	TTL time.Duration
	// Path 为 Cookie 生效路径，空值时使用 /。
	Path string
	// Domain 为 Cookie 生效域名，空值表示仅当前域。
	Domain string
	// Secure 为 true 时 Cookie 只在 HTTPS 下发送。
	Secure bool
	// HTTPOnly 为 true 时禁止脚本读取 Cookie；默认开启。
	HTTPOnly bool
	// SameSite 为 Cookie 的 SameSite 策略，零值使用 Lax。
	SameSite http.SameSite
	// Secret 为 Cookie 签名密钥；为空时自动生成随机密钥，
	// 此时进程重启后已有 Cookie 全部失效。
	Secret []byte
}

// NewDefaultOptions 返回推荐配置。
func NewDefaultOptions() Options {
	return Options{
		Name:     _DEFAULT_NAME,
		TTL:      _DEFAULT_TTL,
		Path:     _DEFAULT_PATH,
		HTTPOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
}

// normalize 修正非法配置并补齐随机密钥。
//
// 返回错误表示系统随机源不可用：此时无法得到安全的签名密钥，
// 必须由调用方中止启动，而不是用空密钥继续运行。
func (own Options) normalize() (Options, error) {
	defaults := NewDefaultOptions()

	if strings.TrimSpace(own.Name) == "" {
		own.Name = defaults.Name
	}

	if own.TTL <= 0 {
		own.TTL = defaults.TTL
	}

	if strings.TrimSpace(own.Path) == "" {
		own.Path = defaults.Path
	}

	if own.SameSite == http.SameSiteDefaultMode {
		own.SameSite = defaults.SameSite
	}

	if len(own.Secret) == 0 {
		secret := make([]byte, _SECRET_BYTES)
		if _, err := rand.Read(secret); err != nil {
			return own, fmt.Errorf("%w: %v", ErrRandomSource, err)
		}
		own.Secret = secret
	}

	own.Secret = slices.Clone(own.Secret)
	return own, nil
}

// Option 修改 Options。
type Option func(*Options)

// WithName 设置 Cookie 名称。
func WithName(name string) Option {
	return func(options *Options) {
		options.Name = name
	}
}

// WithTTL 设置会话有效期。
func WithTTL(ttl time.Duration) Option {
	return func(options *Options) {
		options.TTL = ttl
	}
}

// WithPath 设置 Cookie 生效路径。
func WithPath(path string) Option {
	return func(options *Options) {
		options.Path = path
	}
}

// WithDomain 设置 Cookie 生效域名。
func WithDomain(domain string) Option {
	return func(options *Options) {
		options.Domain = domain
	}
}

// WithSecure 设置是否只在 HTTPS 下发送 Cookie。
func WithSecure(secure bool) Option {
	return func(options *Options) {
		options.Secure = secure
	}
}

// WithSameSite 设置 SameSite 策略。
func WithSameSite(sameSite http.SameSite) Option {
	return func(options *Options) {
		options.SameSite = sameSite
	}
}

// WithSecret 设置 Cookie 签名密钥。
func WithSecret(secret []byte) Option {
	return func(options *Options) {
		options.Secret = slices.Clone(secret)
	}
}

// sessionKey 是上下文中的会话键类型，避免与其他包冲突。
type sessionKey struct{}

// middleware 保存会话中间件的运行期状态。
type middleware struct {
	// options 为规范化后的配置。
	options Options
}

// New 创建会话中间件。
//
// 中间件把会话写入请求上下文，业务代码用 From 取回；
// store 为 nil 时直接 panic，属于启动期配置错误。
func New(store Store, options ...Option) leego.MiddlewareFunc {
	if store == nil {
		panic(ErrNilStore.Error())
	}

	settings := NewDefaultOptions()
	for _, option := range options {
		if option != nil {
			option(&settings)
		}
	}

	settings, err := settings.normalize()
	if err != nil {
		// 随机源不可用属于启动期致命错误：继续运行会得到空密钥。
		panic(err.Error())
	}

	own := &middleware{options: settings}

	return func(next leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			current := own.load(store, ctx)

			// 包装响应写入器：在响应头真正发送之前同步 Cookie。
			// 数据持久化发生在处理函数之后，此时再写响应头已不会生效。
			original := ctx.Response
			ctx.Response = &cookieWriter{Writer: original, session: current}
			// 处理器 panic 时也要还原写入器，否则恢复中间件拿到的是包装器。
			defer func() {
				ctx.Response = original
			}()

			// 中间件只改写本次请求的上下文副本，池化回收后不会残留。
			requestCtx := context.WithValue(ctx.Request.Context(), sessionKey{}, current)
			ctx.Request = ctx.Request.WithContext(requestCtx)

			next(ctx)

			// 请求已结束，后续处理不能再依赖请求上下文（可能已被取消）。
			current.persist(ctx, context.WithoutCancel(ctx.Request.Context()))
		}
	}
}

// cookieWriter 在响应头发送前同步会话 Cookie。
//
// net/http 在 WriteHeader 之后会忽略响应头修改，而会话 ID 可能在处理函数
// 内部才被轮换，因此必须在第一次写响应时把 Cookie 补进响应头。
type cookieWriter struct {
	// Writer 为被包装的响应写入器。
	response.Writer
	// session 为当前请求的会话。
	session *Session
}

// WriteHeader 同步 Cookie 后写入状态码。
func (own *cookieWriter) WriteHeader(status int) {
	own.session.syncCookie(own.Writer)
	own.Writer.WriteHeader(status)
}

// Write 同步 Cookie 后写入响应体。
func (own *cookieWriter) Write(data []byte) (int, error) {
	own.session.syncCookie(own.Writer)
	return own.Writer.Write(data)
}

// Flush 同步 Cookie 后刷新缓冲。
func (own *cookieWriter) Flush() {
	own.session.syncCookie(own.Writer)
	own.Writer.Flush()
}

// load 读取并校验现有会话，失败时新建。
func (own *middleware) load(store Store, ctx *leego.Context) *Session {
	cookie, err := ctx.Request.Cookie(own.options.Name)
	if err == nil {
		if id, ok := decodeCookie(own.options.Secret, cookie.Value); ok {
			values, getErr := store.Get(context.WithoutCancel(ctx.Request.Context()), id)
			if getErr == nil && values != nil {
				return &Session{
					id:      id,
					values:  values,
					store:   store,
					options: own.options,
				}
			}
		}
	}

	id, err := newID()
	if err != nil {
		// 随机源不可用时退化为空会话：宁可不持久化，也不生成可预测的 ID。
		return &Session{values: Values{}, store: store, options: own.options}
	}

	return &Session{id: id, values: Values{}, store: store, options: own.options, isNew: true}
}

// GetFromContext 从请求上下文取回会话。
func GetFromContext(ctx context.Context) (*Session, bool) {
	if ctx == nil {
		return nil, false
	}

	current, ok := ctx.Value(sessionKey{}).(*Session)
	return current, ok
}

// GetFromRequest 从 HTTP 请求取回会话，便于不使用 Context 的场景。
func GetFromRequest(request *http.Request) (*Session, bool) {
	if request == nil {
		return nil, false
	}

	return GetFromContext(request.Context())
}

// Session 是单次请求内的会话视图。
//
// 并发约定：同一会话可能被处理函数派生的协程读取，
// 因此所有方法都加锁；写回仍由中间件在请求结束时完成。
type Session struct {
	// mu 保护全部可变字段。
	mu sync.Mutex
	// id 为当前会话 ID；轮换时会被替换。
	id string
	// values 为会话数据。
	values Values
	// store 为服务端存储。
	store Store
	// options 为会话配置。
	options Options
	// modified 标记数据已变化。
	modified bool
	// destroyed 标记会话已被销毁。
	destroyed bool
	// isNew 标记会话尚未写入存储。
	isNew bool
	// lastCookie 记录本次响应中已写入的会话 ID，避免重复 Set-Cookie。
	lastCookie string
	// cookieCleared 标记本次响应已写入清除 Cookie。
	cookieCleared bool
}

// GetID 返回当前会话 ID；空字符串表示会话不可持久化。
func (own *Session) GetID() string {
	own.mu.Lock()
	defer own.mu.Unlock()

	return own.id
}

// IsNew 返回会话是否是本次请求新建的。
func (own *Session) IsNew() bool {
	own.mu.Lock()
	defer own.mu.Unlock()

	return own.isNew
}

// IsDestroyed 返回会话是否已被销毁。
func (own *Session) IsDestroyed() bool {
	own.mu.Lock()
	defer own.mu.Unlock()

	return own.destroyed
}

// Get 读取一个会话值。
func (own *Session) Get(key string) (any, bool) {
	own.mu.Lock()
	defer own.mu.Unlock()

	value, ok := own.values[key]
	return value, ok
}

// Set 写入一个会话值并标记为已修改。
func (own *Session) Set(key string, value any) {
	own.mu.Lock()
	defer own.mu.Unlock()

	if own.destroyed {
		return
	}

	own.values[key] = value
	own.modified = true
}

// Delete 删除一个会话值并标记为已修改。
func (own *Session) Delete(key string) {
	own.mu.Lock()
	defer own.mu.Unlock()

	if own.destroyed {
		return
	}

	if _, ok := own.values[key]; !ok {
		return
	}

	delete(own.values, key)
	own.modified = true
}

// GetKeys 返回全部键名，结果按字典序排列。
func (own *Session) GetKeys() []string {
	own.mu.Lock()
	keys := make([]string, 0, len(own.values))
	for key := range own.values {
		keys = append(keys, key)
	}
	own.mu.Unlock()

	slices.Sort(keys)
	return keys
}

// GetValues 返回会话数据的浅拷贝，调用方修改拷贝不会影响会话。
func (own *Session) GetValues() Values {
	own.mu.Lock()
	defer own.mu.Unlock()

	values := make(Values, len(own.values))
	for key, value := range own.values {
		values[key] = value
	}

	return values
}

// Destroy 标记销毁会话；存储与 Cookie 在请求结束时统一清理。
func (own *Session) Destroy() {
	own.mu.Lock()
	defer own.mu.Unlock()

	own.destroyed = true
	own.modified = true
	own.values = Values{}
}

// Regenerate 轮换会话 ID，用于登录成功后防止会话固定。
//
// 数据会被保留并写入新 ID；旧 ID 立即从存储中删除。
func (own *Session) Regenerate(ctx context.Context) error {
	id, err := newID()
	if err != nil {
		return err
	}

	own.mu.Lock()
	if own.destroyed {
		own.mu.Unlock()
		return nil
	}

	oldID := own.id
	own.id = id
	own.isNew = true
	own.modified = true
	own.mu.Unlock()

	if oldID == "" {
		return nil
	}

	return own.store.Destroy(ctx, oldID)
}

// persist 在请求结束时写回存储并同步 Cookie。
func (own *Session) persist(ctx *leego.Context, requestCtx context.Context) {
	own.mu.Lock()
	if own.destroyed {
		id := own.id
		own.mu.Unlock()

		if id != "" {
			if err := own.store.Destroy(requestCtx, id); err != nil {
				ctx.AddError(err)
			}
		}

		// 响应可能已写出，syncCookie 会自行判断是否还有机会写头。
		own.syncCookie(ctx.Response)
		return
	}

	if !own.modified && !own.isNew {
		own.mu.Unlock()
		return
	}

	id := own.id
	values := make(Values, len(own.values))
	for key, value := range own.values {
		values[key] = value
	}
	own.mu.Unlock()

	// 随机源不可用时 id 为空：放弃持久化，避免写入不可定位的会话。
	if id == "" {
		return
	}

	if err := own.store.Save(requestCtx, id, values, own.options.TTL); err != nil {
		ctx.AddError(err)
		return
	}

	// 处理函数未写响应时，响应头尚未发送，这里补一次同步。
	own.syncCookie(ctx.Response)
}

// syncCookie 把会话状态同步到响应头。
//
// 只有“新建/轮换”的会话需要下发 Cookie；数据修改不改变 ID，
// 沿用浏览器已有 Cookie 即可。销毁会话时写入过期 Cookie。
func (own *Session) syncCookie(resp response.Writer) {
	if resp == nil {
		return
	}

	own.mu.Lock()
	if own.destroyed {
		if own.cookieCleared {
			own.mu.Unlock()
			return
		}

		own.cookieCleared = true
		own.mu.Unlock()
		replaceCookie(resp, own.buildCookie("", -1))
		return
	}

	id := own.id
	if id == "" || !own.isNew || own.lastCookie == id {
		own.mu.Unlock()
		return
	}

	own.lastCookie = id
	own.mu.Unlock()

	replaceCookie(resp, own.buildCookie(id, int(own.options.TTL/time.Second)))
}

// buildCookie 构造会话 Cookie；id 为空时返回过期 Cookie。
func (own *Session) buildCookie(id string, maxAge int) *http.Cookie {
	cookie := &http.Cookie{
		Name:     own.options.Name,
		Path:     own.options.Path,
		Domain:   own.options.Domain,
		MaxAge:   maxAge,
		HttpOnly: own.options.HTTPOnly,
		Secure:   own.options.Secure,
		SameSite: own.options.SameSite,
	}

	if id == "" {
		cookie.Expires = time.Unix(0, 0)
		return cookie
	}

	cookie.Value = encodeCookie(own.options.Secret, id)
	cookie.Expires = time.Now().Add(own.options.TTL)
	return cookie
}

// replaceCookie 用新 Cookie 覆盖同名响应头，避免重复 Set-Cookie。
func replaceCookie(resp response.Writer, cookie *http.Cookie) {
	if cookie == nil {
		return
	}

	header := resp.Header()
	prefix := cookie.Name + "="
	values := header.Values("Set-Cookie")
	kept := make([]string, 0, len(values)+1)

	for _, value := range values {
		if !strings.HasPrefix(value, prefix) {
			kept = append(kept, value)
		}
	}

	kept = append(kept, cookie.String())
	header.Del("Set-Cookie")
	for _, value := range kept {
		header.Add("Set-Cookie", value)
	}
}

// newID 生成随机会话 ID。
func newID() (string, error) {
	buffer := make([]byte, _ID_BYTES)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(buffer), nil
}
