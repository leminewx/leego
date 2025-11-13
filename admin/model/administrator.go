package model

import (
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"

	"gitee.com/leminewx/gokit/cryptox/rsax"
	"gitee.com/leminewx/leego"
	jwt2 "gitee.com/leminewx/leego/middleware/jwt"
	"gitee.com/leminewx/leego/response"
)

type Administrator struct {
	// 账号信息
	username string
	password string
	emails   *sync.Map

	// 钥匙信息
	privateKey      *rsa.PrivateKey
	publicKey       *rsa.PublicKey
	publicKeyString string

	// 认证信息
	maxTokens     int
	Authenticator *jwt2.Authenticator
	tokens        map[string]*jwt2.Token

	lock sync.RWMutex
}

func NewAdministrator() *Administrator {
	// 创建公私钥
	publicKey, privateKey, _ := rsax.GenerateRSAKeyPair(1024)
	// 读取公钥字符串
	publicKeyString, _ := rsax.ReadPublicKeyToPEMString(publicKey)

	//  创建管理员账号
	return &Administrator{
		username:        "admin",
		password:        "admin.123",
		emails:          &sync.Map{},
		privateKey:      privateKey,
		publicKey:       publicKey,
		publicKeyString: publicKeyString,
		tokens:          make(map[string]*jwt2.Token),
		Authenticator: jwt2.NewHS256Authenticator("", []byte("00008e03308a-0b1f-0000-0628ba3cba87")).
			WithSkipPaths("/auth/pubkey", "/auth/login").
			WithExceptedIssuer("leego").
			WithExceptedAudience("leego_admin").
			WithExpiresAfterSeconds(3600 * 24).
			WithValidateStandardClaims(true),
	}
}

// WithPassword 设置密码
func (own *Administrator) WithPassword(password string) *Administrator {
	own.lock.Lock()
	own.password = password
	own.lock.Unlock()
	return own
}

// WithEmails 添加指定邮箱
func (own *Administrator) WithEmails(emails ...string) *Administrator {
	for _, email := range emails {
		email = strings.TrimSpace(email)
		if strings.Contains(email, "@") {
			own.emails.Store(email, struct{}{})
		}
	}

	return own
}

// SetScope 实现 leego.Middleware 接口
func (own *Administrator) SetScope(_ string) {}

// GetEmails 获取邮箱列表
func (own *Administrator) GetEmails() []string {
	emails := make([]string, 0)
	own.emails.Range(func(email, _ any) bool {
		if email != "" {
			emails = append(emails, email.(string))
		}
		return true
	})

	return emails
}

// GetUsername 获取用户名
func (own *Administrator) GetUsername() string {
	own.lock.RLock()
	name := own.username
	own.lock.RUnlock()
	return name
}

// DeleteEmails 删除指定邮箱
func (own *Administrator) DeleteEmails(emails ...string) {
	for _, email := range emails {
		email = strings.TrimSpace(email)
		if strings.Contains(email, "@") {
			own.emails.Delete(email)
		}
	}
}

// Login 用户登录
func (own *Administrator) Login(data []byte) (string, error) {
	// 解密登录数据
	body, err := rsax.DecryptWithPrivateKey(own.privateKey, data)
	if err != nil {
		return "", err
	}

	// 读取用户信息
	var user struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(body, &user); err != nil {
		return "", err
	}

	// 校验账户信息
	own.lock.RLock()
	defer own.lock.RUnlock()

	// token数量超时最大限制
	if len(own.tokens) > own.maxTokens {
		// 清理过期token
		for tokenString, token := range own.tokens {
			if err := token.GetStandardClaims().Validate(); err != nil {
				delete(own.tokens, tokenString)
			}
		}

		// token数量超时最大限制
		if len(own.tokens) > own.maxTokens {
			return "", errors.New("tokens number is too many")
		}
	}

	// 校验账号密码
	if own.username != user.Username {
		return "", errors.New("incorrect username or password")
	}
	if own.password != user.Password {
		return "", errors.New("incorrect username or password")
	}

	// 生成token
	token := own.Authenticator.GenerateToken(&jwt2.StandardClaims{})
	tokenBytes, err := own.Authenticator.Sign(token)
	if err != nil {
		return "", err
	}

	// 存储token信息
	own.tokens[string(tokenBytes)] = token

	return string(tokenBytes), nil
}

// Logout 用户登出
func (own *Administrator) Logout(tokenString string) error {
	own.lock.Lock()
	defer own.lock.Unlock()

	if _, ok := own.tokens[tokenString]; !ok {
		return errors.New("invalid token")
	}

	delete(own.tokens, tokenString)
	return nil
}

// Handle 处理中间件调用
func (own *Administrator) Handle(handle leego.HandleFunc) leego.HandleFunc {
	return func(ctx *leego.Context) {
		// 检查是否跳过当前路径
		if own.Authenticator.ShouldSkip(ctx.Request.URL.Path) {
			handle(ctx)
			return
		}

		// 提取令牌
		tokenString, err := own.Authenticator.ExtractToken(ctx.Request)
		if err != nil {
			ctx.ResponseError(http.StatusUnauthorized, response.STATUS_401_UNAUTHORIZED)
			return
		}

		// 检查令牌
		own.lock.RLock()
		token, ok := own.tokens[tokenString]
		if own.lock.RUnlock(); !ok {
			ctx.ResponseError(http.StatusUnauthorized, "401 Invalid Token")
			return
		}

		// 验证令牌
		if err := token.GetStandardClaims().Validate(); err != nil {
			ctx.ResponseError(http.StatusUnauthorized, "401 Expired Token")
			return
		}

		// 处理请求
		handle(ctx)
	}
}
