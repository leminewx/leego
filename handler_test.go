package leego

import (
	"fmt"
	"net/http"
	"testing"
)

func TestHandler(t *testing.T) {
	handler := newHandler(nil, http.MethodGet, func(ctx *Context) {
		fmt.Println("hello world")
	})

	handler.WithMiddlewares(func(handle HandleFunc) HandleFunc {
		return func(ctx *Context) {
			fmt.Println("middleware 1 start")
			handle(ctx)
			fmt.Println("middleware 1 end")
		}
	}, func(handle HandleFunc) HandleFunc {
		return func(ctx *Context) {
			fmt.Println("middleware 2 start")
			handle(ctx)
			fmt.Println("middleware 2 end")
		}
	}).InitBeforeServe()

	t.Log("第1次请求")
	req, err := http.NewRequest(http.MethodGet, "/", nil)
	if err != nil {
		t.Fatal(err)
	}
	handler.ServeHTTP(nil, req)

	t.Log("第2次请求")
	req.Method = http.MethodPost
	handler.ServeHTTP(nil, req)
}
