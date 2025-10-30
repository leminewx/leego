package controller

import (
	"crypto/rsa"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"

	"gitee.com/leminewx/gokit/cryptox/rsax"
	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/headers"
)

var (
	Administrator     *administrator
	administratorOnce sync.Once
)

func init() {
	administratorOnce.Do(func() {
		Administrator = &administrator{
			username: "admin",
			password: "admin.123",
			emails:   make(map[string]struct{}),
		}
	})
}

// GetPublicKey 获取公钥
func GetPublicKey(publicKey *rsa.PublicKey) leego.HandleFunc {
	publicKeyString, _ := rsax.ReadPublicKeyToPEMString(publicKey)
	publicKeyJSONBytes, _ := json.Marshal(&leego.JSON{
		"code": 0,
		"desc": "success",
		"data": publicKeyString,
	})

	return func(ctx *leego.Context) {

		ctx.Response.Header().Set(headers.CONTENT_TYPE, "application/json; charset=utf-8")
		ctx.Response.WriteHeader(http.StatusOK)
		_, _ = ctx.Response.Write(publicKeyJSONBytes)
	}
}

// LoginAdministrator 登录管理员账户
func LoginAdministrator(privateKey *rsa.PrivateKey) leego.HandleFunc {
	return func(ctx *leego.Context) {
		// 读取请求体
		data, err := io.ReadAll(ctx.Request.Body)
		if err != nil {
			_ = ctx.ResponseJSON(http.StatusBadRequest, leego.JSON{
				"code": 400,
				"desc": "bad request",
				"info": err.Error(),
			})
			return
		}

		// 解析密码
		body, err := rsax.DecryptWithPrivateKey(privateKey, data)
		if err != nil {
			_ = ctx.ResponseJSON(http.StatusBadRequest, leego.JSON{
				"code": 400,
				"desc": "bad request",
				"info": err.Error(),
			})
			return
		}

		// 读取用户信息
		var user struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.Unmarshal(body, &user); err != nil {
			_ = ctx.ResponseJSON(http.StatusBadRequest, leego.JSON{
				"code": 400,
				"desc": "bad request",
				"info": err.Error(),
			})
			return
		}

		// 校验账户信息
		if !Administrator.Verify(user.Username, user.Password) {
			_ = ctx.ResponseJSON(http.StatusUnauthorized, leego.JSON{
				"code": 401,
				"desc": "unauthorized",
			})
			return
		}

		// 认证通过，生成令牌
		jwtBytes, err := own.authenticator.Sign(own.authenticator.GenerateToken(nil))
		if err != nil {
			_ = ctx.ResponseJSON(http.StatusBadRequest, leego.JSON{
				"code": 400,
				"desc": "bad request",
			})
			return
		}

		// 更新本地缓存
		own.lock.Lock()
		own.oldTokenBytes = own.nowTokenBytes
		own.nowTokenBytes = jwtBytes
		own.lock.Unlock()

		// 响应
		_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{
			"code": 0,
			"desc": "success",
			"data": string(jwtBytes),
		})
	}
}
