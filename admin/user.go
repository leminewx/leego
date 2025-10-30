package admin

import (
	"encoding/json"
	"net/http"

	"gitee.com/leminewx/gokit/cryptox/rsax"
	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/headers"
)

type (
	Administrator struct {
		Emails   []string `json:"emails"`
		Username string   `json:"username"`
		Password string   `json:"password"`
	}

	LoginUser struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
)

func (own *Admin) HandleAdministrator() {
	// 创建认证组
	group := own.admin.Group("/auth")
	publicKeyJSONBytes, _ := json.Marshal(&leego.JSON{
		"code": 0,
		"desc": "success",
		"data": own.publicKeyString,
	})

	// 获取公钥
	group.POST("/pkey", func(ctx *leego.Context) {
		ctx.Response.Header().Set(headers.CONTENT_TYPE, "application/json")
		ctx.Response.WriteHeader(http.StatusOK)
		_, _ = ctx.Response.Write(publicKeyJSONBytes)
	})

	// 登录账户
	group.POST("/login", func(ctx *leego.Context) {
		// 读取用户信息
		var user LoginUser
		if err := ctx.BindJSON(&user); err != nil {
			_ = ctx.ResponseJSON(http.StatusBadRequest, leego.JSON{
				"code": 400,
				"desc": "bad request",
			})
			return
		}

		// 解析密码
		password, err := rsax.DecryptWithPrivateKey(own.privateKey, []byte(user.Password))
		if err != nil {
			_ = ctx.ResponseJSON(http.StatusBadRequest, leego.JSON{
				"code": 400,
				"desc": "bad request",
			})
			return
		}

		// 对比密码
		if string(password) != user.Password {
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
	})

	// 更新令牌
	//group.PUT("/", func(ctx *leego.Context) {
	//})

	// 退出账户
	group.DELETE("/logout", func(ctx *leego.Context) {
		own.lock.Lock()
		own.oldTokenBytes = nil
		own.nowTokenBytes = nil
		own.lock.Unlock()

		_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{
			"code": 0,
			"desc": "success",
		})
	})
}
