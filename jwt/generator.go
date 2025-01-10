package jwt

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"gitee.com/leminewx/loggo"
)

type Generator interface {
	// WithHeader 设置令牌的头部
	WithHeader(header *Header) Generator
	// WithDelay 设置令牌生效的延迟时间
	WithDelay(delay int64) Generator
	// WithTimeout 设置令牌的超时时间
	WithTimeout(timeout int64) Generator
	// WithIss 设置令牌的签发者
	/*
		当验证令牌时，服务端可以检查 iss 声明来确认令牌的发行者是否是预期的发行者。这对于防止未经授权的实体签发令牌非常重要。
		如果 iss 不匹配，则应当拒绝访问，因为这意味着令牌可能不是由预期的发行者签发的。
		此外，在多发行者环境中，iss 声明还可以帮助区分不同来源的令牌，允许系统根据不同的发行者配置不同的验证规则或权限策略。
		总之，iss 提供了一种方法来跟踪和验证令牌的来源，确保只有受信任的发行者签发的令牌才能被接受。这增加了系统的安全性和可靠性。
	*/
	WithIss(iss string) Generator
	// WithAllowIssuers 设置可接受的令牌签发者，用于多系统条件下的签发来源认证
	WithAllowIssuers(issuers ...string) Generator
	// WithJtiGenerator 设置生成令牌的 ID 的生成器
	/*
	   1.防止重放攻击：通过记录已经使用过的 jti，接收方可以检测到重复提交的同一个令牌，并拒绝接受这些重复的令牌，这样就有效地防止了重放攻击。
	   2.追踪和调试：jti 可以帮助在日志或监控系统中追踪特定的令牌，便于问题排查和审计。
	   3.一次性令牌：当令牌作为一次性令牌（例如密码重置链接中的令牌）时，jti 确保了该令牌只能被使用一次。
	   4.撤销机制：虽然令牌通常是无状态的，但如果有需要，可以通过 jti 来实现基于令牌的撤销机制，即维护一个黑名单来存储已经被撤销的 jti 列表。

	   在验证令牌时，服务端可以检查 jti 是否已经被使用过或者是否存在于黑名单中。如果 jti 已经存在，则应当拒绝访问，因为这可能意味着令牌正在被重放攻击利用，或者是已经被撤销的令牌。
	   总之，jti 提供了一种方法来增强令牌的安全性和管理能力，尤其是在需要保证令牌唯一性的情况下。
	*/
	WithJtiGenerator(jtiGenerator func() string) Generator
	// WithJtiWhitelist 启用白名单
	/*
		注意：默认间隔为60*30s，小于60s取默认值
	*/
	WithJtiWhitelist(interval ...int64) Generator
	// Encode 编码
	Encode(payload Payload) (string, error)
	// Decode 解码
	Decode(token string) (Payload, error)
	// Validate 校验令牌
	Validate(payload Payload) error
	// Close 重置令牌生成器
	Reset()
}

const (
	DEFAULT_EXPIRED_DURATION  = 60 * 60 * 24 * 7 // 默认有效期1周
	DEFAULT_CLEAN_EXPIRED_JTI = 60 * 30          // 默认30分钟清理一次过期令牌
)

type generator struct {
	delay   int64 // 令牌生效的延迟时间（delay 后生效）
	timeout int64 // 令牌有效的时间（delay+timeout 后失效）

	issuer       string              // 令牌颁发者
	allowIssuers map[string]struct{} // 可接受的令牌颁发者（其他颁发者）

	salt   []byte  // 签名算法的 salt
	header *Header // 令牌的标头

	jtiGenerator     func() string      // 令牌 ID 生成器
	jtiCleanInterval time.Duration      // 过期令牌清理间隔
	jtiCleanCancel   context.CancelFunc // 过期令牌清理 goroutine 的取消函数
	jtiWhitelist     *sync.Map          // 白名单
	// jtiBlacklist *sync.Map // 黑名单
}

func NewDefaultGenerator(salt []byte) *generator {
	return &generator{
		timeout:          DEFAULT_EXPIRED_DURATION,
		allowIssuers:     make(map[string]struct{}),
		salt:             salt,
		header:           NewHeader(TYPE_JWT, ALG_HS256),
		jtiCleanInterval: DEFAULT_CLEAN_EXPIRED_JTI * time.Second,
	}
}

func (own *generator) WithHeader(header *Header) Generator {
	if header != nil {
		own.header = header
	}

	return own
}

func (own *generator) WithDelay(delay int64) Generator {
	if delay > -1 {
		own.delay = delay
	}

	return own
}

func (own *generator) WithTimeout(timeout int64) Generator {
	if timeout > 0 {
		own.timeout = timeout
	}

	return own
}

func (own *generator) WithIss(iss string) Generator {
	delete(own.allowIssuers, own.issuer)
	own.allowIssuers[iss] = struct{}{}
	own.issuer = iss
	return own
}

func (own *generator) WithAllowIssuers(issuers ...string) Generator {
	for _, issuer := range issuers {
		own.allowIssuers[issuer] = struct{}{}
	}

	return own
}

func (own *generator) WithJtiWhitelist(interval ...int64) Generator {
	if len(interval) > 0 && interval[0] > 59 {
		own.jtiCleanInterval = time.Duration(interval[0]) * time.Second
	}

	if own.jtiWhitelist == nil {
		own.jtiWhitelist = &sync.Map{}
		own.cleanExpiredTokens()
	}

	return own
}

func (own *generator) WithJtiGenerator(jtiGenerator func() string) Generator {
	if jtiGenerator != nil {
		own.jtiGenerator = jtiGenerator
	}

	if own.jtiWhitelist == nil {
		own.jtiWhitelist = &sync.Map{}
		own.cleanExpiredTokens()
	}

	return own
}

func (own *generator) cleanExpiredTokens() {
	var ctx context.Context
	ctx, own.jtiCleanCancel = context.WithCancel(context.Background())
	go func() {
		ticker := time.NewTicker(own.jtiCleanInterval)
		for {
			select {
			case <-ticker.C:
				loggo.Info("Cleaning expired tokens from the whitelist...")
				now := time.Now().Unix()

				// 获取过期令牌
				deleteKeys := make([]any, 0)
				own.jtiWhitelist.Range(func(key, value any) bool {
					if value.(int64) > now {
						return true
					}

					deleteKeys = append(deleteKeys, key)
					return true
				})

				// 删除过期令牌
				for _, key := range deleteKeys {
					own.jtiWhitelist.Delete(key)
				}
			case <-ctx.Done():
				loggo.Info("Close the whitelist goroutine.")
				ticker.Stop()
				return
			}

			ticker.Reset(own.jtiCleanInterval)
		}
	}()
}

func (own *generator) Encode(payload Payload) (signature string, err error) {
	headerStr, err := own.header.Encode()
	if err != nil {
		return "", err
	}

	if err := own.format(payload); err != nil {
		return "", err
	}

	payloadStr, err := payload.Encode()
	if err != nil {
		return "", err
	}

	HeaderAndPayload := headerStr + "." + payloadStr
	switch own.header.GetAlgorithm() {
	case ALG_HS256:
		if signature, err = GenerateSHA256Signature(own.salt, []byte(HeaderAndPayload)); err != nil {
			return "", err
		}
	}

	return HeaderAndPayload + "." + signature, nil
}

func (own *generator) format(payload Payload) error {
	// iss
	if own.issuer != "" {
		payload["iss"] = own.issuer
	}

	// nbf/exp
	now := time.Now().Unix()
	if _, ok := payload["exp"]; !ok {
		if own.delay > 0 {
			payload["nbf"] = now + own.delay
			payload["exp"] = now + own.delay + own.timeout
		} else {
			payload["exp"] = now + own.timeout
			delete(payload, "nbf")
		}
	}

	// jti
	if own.jtiWhitelist != nil {
		jti := payload.GetJti()
		if jti == "" {
			if own.jtiGenerator == nil {
				return errInvalidJti
			}

			jti = own.jtiGenerator()
			payload["jti"] = jti
		}

		if _, ok := own.jtiWhitelist.LoadOrStore(jti, payload.GetExp()); ok {
			return fmt.Errorf("jwt: 'jti' is existed: %s", jti)
		}
	}

	return nil
}

func (own *generator) Decode(token string) (Payload, error) {
	// 分割签名
	signatures := strings.Split(token, ".")
	if len(signatures) != 3 {
		return nil, errInvalidToken
	}

	// 校验签名
	switch own.header.GetAlgorithm() {
	case ALG_HS256:
		if confirmSignature, err := GenerateSHA256Signature(own.salt, []byte(signatures[0]+"."+signatures[1])); err != nil || signatures[2] != confirmSignature {
			return nil, errInvalidToken
		}
	}

	// 解码载荷
	playloadBytes, err := base64.RawURLEncoding.DecodeString(signatures[1])
	if err != nil {
		return nil, errInvalidPayload
	}

	var playload Payload
	return playload, json.Unmarshal(playloadBytes, &playload)
}

func (own *generator) Validate(payload Payload) error {
	// 校验令牌是否有效
	if err := payload.Validate(); err != nil {
		return err
	}

	// 校验令牌标识是否有效
	if own.jtiWhitelist != nil {
		if _, ok := own.jtiWhitelist.Load(payload.GetJti()); !ok {
			return errInvalidJti
		}
	}

	// 校验令牌是否接收颁发者
	if len(own.allowIssuers) > 0 {
		if _, ok := own.allowIssuers[payload.GetIss()]; !ok {
			return errInvalidIssuer
		}
	}

	return nil
}

func (own *generator) Reset() {
	if own.jtiCleanCancel != nil {
		own.jtiCleanCancel()
	}

	own.delay = 0
	own.timeout = DEFAULT_EXPIRED_DURATION
	own.issuer = ""
	own.allowIssuers = make(map[string]struct{})
	own.header = NewHeader(TYPE_JWT, ALG_HS256)
	own.jtiGenerator = nil
	own.jtiCleanInterval = DEFAULT_CLEAN_EXPIRED_JTI * time.Second
	own.jtiCleanCancel = nil
	own.jtiWhitelist = nil
}
