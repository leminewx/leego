package middleware

import (
	"net/http"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/middleware/authenticator"
	"gitee.com/leminewx/leego/middleware/authenticator/jwt"
	"gitee.com/leminewx/leego/response"
)

// Authorize 创建 JWT 鉴权中间件
func Authorize(auth *authenticator.Authenticator) leego.MiddlewareFunc {
	return func(handle leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			// 检查是否跳过当前路径
			if auth.ShouldSkip(ctx.GetPath()) {
				handle(ctx)
				return
			}

			// 提取令牌
			tokenString, err := auth.ExtractToken(ctx.Request)
			if err != nil {
				// 处理错误若存在，则处理错误
				if auth.ErrorHandler != nil {
					auth.ErrorHandler(ctx, err)
				}

				// 未指定状态码，需额外处理响应
				if ctx.GetStatusCode() == 0 {
					ctx.ResponseError(http.StatusUnauthorized, response.STATUS_401_UNAUTHORIZED)
				}

				return
			}

			// 验证令牌
			var token *jwt.Token
			if auth.ShouldValidateStandardClaims() { // 验证签名和标准声明
				token, err = auth.VerifyWithStandardClaims(tokenString)
			} else { // 仅验证签名
				token, err = auth.Verify(tokenString)
			}

			// 鉴权失败
			if err != nil {
				// 处理错误若存在，则处理错误
				if auth.ErrorHandler != nil {
					auth.ErrorHandler(ctx, err)
				}

				// 未指定状态码，需额外处理响应
				if ctx.GetStatusCode() == 0 {
					ctx.ResponseError(http.StatusUnauthorized, response.STATUS_401_UNAUTHORIZED)
				}

				return
			}

			// 将令牌和声明存储在请求中，然后调用下一个处理器
			ctx.WithJWT(token)
			handle(ctx)
		}
	}
}
