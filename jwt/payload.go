package jwt

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"
)

var (
	errNBFDatatype     = errors.New("jwt: 'nbf' datatype is error")
	errNBFNotEffective = errors.New("jwt: 'nbf' is not effective yet")
	errEXPNotFound     = errors.New("jwt: 'exp' is not found")
	errEXPDatatype     = errors.New("jwt: 'exp' datatype is error")
	errEXPInvalid      = errors.New("jwt: 'exp' is invalid")
	errIATDatatype     = errors.New("jwt: 'iat' datatype is error")
)

type Payload map[string]any

func NewPayload() Payload {
	return Payload{"iat": time.Now().Unix()}
}

// WithNbf 设置令牌的生效时间戳，生效之前不可用
func (own Payload) WithNbf(nbf int64) Payload {
	own["nbf"] = nbf
	return own
}

// WithExp 设置令牌的过期时间，过期之后将失效
func (own Payload) WithExp(exp int64) Payload {
	own["exp"] = exp
	return own
}

// WithSub 设置令牌的主体，用来表示该令牌所代表的主要对象，通常是唯一标识符，比如用户或设备的 ID
/*
sub 的值应该在整个系统中是唯一的，以确保正确识别主体。
当验证令牌时，服务端可以检查 sub 声明来确认令牌是为哪个用户或实体签发的。这对于个性化内容、权限控制和跟踪用户活动等场景非常有用。
需要注意的是，尽管 sub 通常用于标识用户，但它的用途并不局限于此。它可以用于任何需要被唯一标识的对象或实体。
此外，在某些情况下，如果应用有多个类型的主体（例如用户和设备），你可能还需要结合其他声明一起使用，如 typ（类型）声明，来更准确地描述令牌的主题。
*/
func (own Payload) WithSub(sub string) Payload {
	own["sub"] = sub
	return own
}

// WithAud 设置令牌的受众，通常是一个服务或应用程序的标识符，比如客户端ID或者API网关的唯一识别码
/*
在使用令牌进行认证和授权时，aud 用来确保令牌只被它意料中的接收者所接受。
如果你有一个系统包含多个子系统（如一个用户管理系统和一个支付处理系统），你可以为每个子系统设置不同的 aud 值。当一个子系统接收到令牌时，它应该检查 aud 声明以验证令牌是否确实是为其准备的。如果不是，则应拒绝该令牌。
如果令牌中包含了多个预期的受众，aud 声明可以是一个字符串数组。在这种情况下，只要令牌的接收者在数组中存在，就可以认为令牌是有效的。
*/
func (own Payload) WithAud(aud string) Payload {
	own["aud"] = aud
	return own
}

// With 添加字段
func (own Payload) With(key string, value any) Payload {
	if key != "iat" {
		own[key] = value
	}

	return own
}

// GetJti 获取令牌的全局唯一标识符
func (own Payload) GetJti() string {
	return own["jti"].(string)
}

// GetIss 获取令牌的颁发者
func (own Payload) GetIss() string {
	return own["iss"].(string)
}

// GetSub 获取令牌的主体信息
func (own Payload) GetSub() string {
	return own["sub"].(string)
}

// GetAud 获取令牌的受众信息
func (own Payload) GetAud() string {
	return own["aud"].(string)
}

// GetIat 获取令牌的生成时间戳
func (own Payload) GetIat() (int64, error) {
	exp, ok := own["iat"]
	if !ok {
		return 0, nil
	}

	switch val := exp.(type) {
	case float64:
		return int64(val), nil
	case int64:
		return val, nil
	default:
		return 0, errIATDatatype
	}
}

// GetNbf 获取令牌的生效时间戳
func (own Payload) GetNbf() (int64, error) {
	nbf, ok := own["nbf"]
	if !ok {
		return 0, nil
	}

	switch val := nbf.(type) {
	case float64:
		return int64(val), nil
	case int64:
		return val, nil
	default:
		return 0, errNBFDatatype
	}
}

// GetExp 获取令牌的过期时间戳
func (own Payload) GetExp() (int64, error) {
	exp, ok := own["exp"]
	if !ok {
		return 0, errEXPNotFound
	}

	switch val := exp.(type) {
	case float64:
		return int64(val), nil
	case int64:
		return val, nil
	default:
		return 0, errEXPDatatype
	}
}

// Validate 验证令牌是否有效
func (own Payload) Validate() error {
	now := time.Now().Unix()

	// 校验 nbf
	nbf, err := own.GetNbf()
	if err != nil {
		return err
	} else if now < nbf {
		return errNBFNotEffective
	}

	// 校验 exp
	exp, err := own.GetExp()
	if err != nil {
		return err
	} else if now > exp {
		return errEXPInvalid
	}

	return nil
}

func (own Payload) Encode() (string, error) {
	data, err := json.Marshal(own)
	if err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(data), nil
}
