package jwt

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

type Generator interface {
	// WithHeader 设置令牌的头部
	WithHeader(header *Header) Generator
	// WithDelay 设置令牌生效的延迟时间
	WithDelay(delay int64) Generator
	// WithTimeout 设置令牌的超时时间
	WithTimeout(timeout int64) Generator
	// WithIss 设置令牌的颁发者，用来表示谁签发了该令牌
	/*
		当验证令牌时，服务端可以检查 iss 声明来确认令牌的发行者是否是预期的发行者。这对于防止未经授权的实体签发令牌非常重要。
		如果 iss 不匹配，则应当拒绝访问，因为这意味着令牌可能不是由预期的发行者签发的。
		此外，在多发行者环境中，iss 声明还可以帮助区分不同来源的令牌，允许系统根据不同的发行者配置不同的验证规则或权限策略。
		总之，iss 提供了一种方法来跟踪和验证令牌的来源，确保只有受信任的发行者签发的令牌才能被接受。这增加了系统的安全性和可靠性。
	*/
	WithIss(iss string) Generator
	// WithJtiGenerator 设置生成令牌的全局唯一标识符的生成器
	/*
	   1.防止重放攻击：通过记录已经使用过的 jti，接收方可以检测到重复提交的同一个令牌，并拒绝接受这些重复的令牌，这样就有效地防止了重放攻击。
	   2.追踪和调试：jti 可以帮助在日志或监控系统中追踪特定的令牌，便于问题排查和审计。
	   3.一次性令牌：当令牌作为一次性令牌（例如密码重置链接中的令牌）时，jti 确保了该令牌只能被使用一次。
	   4.撤销机制：虽然令牌通常是无状态的，但如果有需要，可以通过 jti 来实现基于令牌的撤销机制，即维护一个黑名单来存储已经被撤销的 jti 列表。

	   在验证令牌时，服务端可以检查 jti 是否已经被使用过或者是否存在于黑名单中。如果 jti 已经存在，则应当拒绝访问，因为这可能意味着令牌正在被重放攻击利用，或者是已经被撤销的令牌。
	   总之，jti 提供了一种方法来增强令牌的安全性和管理能力，尤其是在需要保证令牌唯一性的情况下。
	*/
	WithJtiGenerator(jtiGenerator func() string) Generator
	// Encode 编码
	Encode(payload Payload) (string, error)
	// Decode 解码
	Decode(token string) (Payload, error)
}

const (
	DEFAULT_EXPIRED_DURATION = 60 * 60 * 24 * 7 // 默认有效期1周
)

var (
	errIllegalFormat  = errors.New("jwt: illegal format")
	errInvalidPayload = errors.New("jwt: invalid payload")
	errInvalidToken   = errors.New("jwt: invalid token")
	errInvalidIssuer  = errors.New("jwt: invalid issuer")
	errInvalidJwtId   = errors.New("jwt: invalid jwt id")
)

type generator struct {
	delay        int64
	timeout      int64
	issuer       string
	allowIssuers map[string]struct{}
	header       *Header
	salt         []byte
	jtis         *sync.Map
	jtiGenerator func() string
	// jtiBlacklist *sync.Map
}

func NewDefaultGenerator(salt []byte) *generator {
	return &generator{
		timeout:      DEFAULT_EXPIRED_DURATION,
		allowIssuers: make(map[string]struct{}),
		header:       NewHeader(TYPE_JWT, ALG_HS256),
		salt:         salt,
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

func (own *generator) WithJtiGenerator(jtiGenerator func() string) Generator {
	if jtiGenerator != nil {
		own.jtis = &sync.Map{}
		own.jtiGenerator = jtiGenerator
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

var empty = &struct{}{}

func (own *generator) format(payload Payload) error {
	if own.issuer != "" {
		payload["iss"] = own.issuer
	}

	if own.jtiGenerator != nil {
		var jti string
		if _, ok := payload["jti"]; !ok {
			jti = own.jtiGenerator()
			payload["jti"] = jti
		}

		if _, ok := own.jtis.LoadOrStore(jti, empty); ok {
			return fmt.Errorf("jwt: 'jti' is existed: %s", jti)
		}
	}

	now := time.Now().Unix()
	if _, ok := payload["exp"]; !ok {
		if own.delay > 0 {
			payload["nbf"] = now + own.delay
			payload["exp"] = now + own.delay + own.timeout
		} else {
			delete(payload, "nbf")
			payload["exp"] = now + own.timeout
		}
	}

	return nil
}

func (own *generator) Decode(token string) (Payload, error) {
	signatures := strings.Split(token, ".")
	if len(signatures) != 3 {
		return nil, errIllegalFormat
	}

	switch own.header.GetAlgorithm() {
	case ALG_HS256:
		if confirmSignature, err := GenerateSHA256Signature(own.salt, []byte(signatures[0]+"."+signatures[1])); err != nil || signatures[2] != confirmSignature {
			return nil, errInvalidToken
		}
	}

	playloadBytes, err := base64.RawURLEncoding.DecodeString(signatures[1])
	if err != nil {
		return nil, errInvalidPayload
	}

	var playload Payload
	return playload, json.Unmarshal(playloadBytes, &playload)
}

func (own *generator) Validate(token string) error {
	payload, err := own.Decode(token)
	if err != nil {
		return err
	}

	if err := payload.Validate(); err != nil {
		return err
	}

	if len(own.allowIssuers) > 0 {
		if _, ok := own.allowIssuers[payload.GetIss()]; !ok {
			return errInvalidIssuer
		}
	}

	if own.jtiGenerator != nil {
		if _, ok := own.jtis.Load(payload.GetJti()); !ok {
			return errInvalidJwtId
		}
	}

	return nil
}

// func (own *generator) initPlayload(playload map[string]any) error {
// 	if own.iss != "" {
// 		playload["iss"] = own.iss
// 	}
// 	if own.sub != "" {
// 		playload["sub"] = own.sub
// 	}

// 	now := time.Now().Unix()
// 	playload["exp"] = now + own.exp
// 	if own.nbf != 0 {
// 		playload["nbf"] = now + own.nbf
// 	}

// 	jti := utils.GenerateRandomString(20)
// 	playload["jti"] = jti
// 	own.lock.Lock()
// 	own.jtis[playload["jti"]] = struct{}{}
// 	own.lock.Unlock()
// 	return nil
// }

// func (own *generator) Decode(token string) (map[string]any, error) {
// 	jwtParts := strings.Split(token, ".")
// 	if len(jwtParts) != 3 {
// 		return nil, fmt.Errorf("jwt is illegal")
// 	}

// 	encodedPayload := jwtParts[1]
// 	playloadBytes, err := base64.RawURLEncoding.DecodeString(encodedPayload)
// 	if err != nil {
// 		return nil, fmt.Errorf("jwt[playload] is illegal")
// 	}

// 	var playload map[string]any
// 	if err := json.Unmarshal(playloadBytes, &playload); err != nil {
// 		return nil, fmt.Errorf("jwt[playload] is invalid")
// 	} else if err := own.validatePlayload(playload); err != nil {
// 		return nil, err
// 	}

// 	signature := jwtParts[2]
// 	encodedHeader := jwtParts[0]
// 	if confirmSignature, _ := generateSHA256Signature(own.key, []byte(encodedHeader+"."+encodedPayload)); signature != confirmSignature {
// 		return nil, fmt.Errorf("jwt is invalid")
// 	}

// 	return playload, nil
// }

// func (own *generator) validatePlayload(playload map[string]any) error {
// 	now := time.Now().Unix()
// 	if exp, ok := playload["exp"].(float64); !ok {
// 		return fmt.Errorf("jwt[exp] is empty")
// 	} else if now > int64(exp) {
// 		own.lock.Lock()
// 		delete(own.jtis, playload["jti"])
// 		own.lock.Unlock()
// 		return fmt.Errorf("jwt[exp] is expired")
// 	}

// 	if jti, ok := playload["jti"]; !ok {
// 		return fmt.Errorf("jwt[jti] is empty")
// 	} else {
// 		own.lock.RLock()
// 		_, ok = own.jtis[jti]
// 		if own.lock.RUnlock(); !ok {
// 			return fmt.Errorf("jwt[jti] is invalid")
// 		}
// 	}

// 	if own.iss != "" {
// 		if iss, ok := playload["iss"]; !ok {
// 			return fmt.Errorf("jwt[iss] is empty")
// 		} else if iss != own.iss {
// 			return fmt.Errorf("jwt[iss] is invalid")
// 		}
// 	}

// 	if own.nbf > 0 {
// 		if nbf, ok := playload["nbf"].(float64); !ok {
// 			return fmt.Errorf("jwt[nbf] is illegal")
// 		} else if now < int64(nbf) {
// 			return fmt.Errorf("jwt[nbf] cannot be used yet")
// 		}
// 	}

// 	return nil
// }
