package middleware

import (
	"fmt"
	"testing"

	"gitee.com/leminewx/leego"
)

func TestLogger(t *testing.T) {
	engine := leego.NewEngine()
	engine.UseMiddlewares(Log())

	group := engine.Group("/v1")
	group.UseMiddlewares(func(next leego.HandleFunc) leego.HandleFunc {
		fmt.Println("init middleware 1")
		return func(ctx *leego.Context) {
			fmt.Println("get in group v1")
			next(ctx)
			fmt.Println("get out group v1")
		}
	})
}
