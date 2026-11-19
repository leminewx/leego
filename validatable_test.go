package leego

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

// validatingRequest 是实现了 Validatable 的请求对象。
type validatingRequest struct {
	// Name 为必填字段。
	Name string `json:"name"`
	// valid 记录 Validate 是否被调用。
	valid bool
}

// Validate 模拟业务校验。
func (own *validatingRequest) Validate() error {
	own.valid = true
	if strings.TrimSpace(own.Name) == "" {
		return errors.New("name is required")
	}

	return nil
}

// TestValidateBound 验证接口判定与错误透传。
func TestValidateBound(t *testing.T) {
	request := &validatingRequest{}
	if err := validateBound(request); err == nil {
		t.Fatal("空 Name 必须返回校验错误")
	}

	if !request.valid {
		t.Fatal("Validate() 未被调用")
	}

	if err := validateBound(&validatingRequest{Name: "leego"}); err != nil {
		t.Fatalf("validateBound() error = %v", err)
	}

	if err := validateBound("plain"); err != nil {
		t.Fatalf("非 Validatable 对象 error = %v, want nil", err)
	}
}

// TestBindJSONInvokesValidate 验证绑定成功后自动触发校验。
func TestBindJSONInvokesValidate(t *testing.T) {
	engine := createTestEngine()
	engine.POST("/validate", func(ctx *Context) {
		var request validatingRequest
		if err := ctx.BindJSON(&request); err != nil {
			ctx.ResponseError(http.StatusBadRequest, err.Error())
			return
		}

		_ = ctx.ResponseJSON(http.StatusOK, map[string]string{"name": request.Name})
	}, "validate")

	recorder := performRequest(engine, http.MethodPost, "/validate", strings.NewReader(`{"name":""}`))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("空名称状态码 = %d, want 400", recorder.Code)
	}

	recorder = performRequest(engine, http.MethodPost, "/validate", strings.NewReader(`{"name":"leego"}`))
	if recorder.Code != http.StatusOK {
		t.Fatalf("合法请求状态码 = %d, want 200", recorder.Code)
	}
}
