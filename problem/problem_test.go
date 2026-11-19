package problem_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/harness/testutil"
	"gitee.com/leminewx/leego/problem"
	"gitee.com/leminewx/leego/validate"
)

func TestNewDefaults(t *testing.T) {
	item := problem.New(http.StatusBadRequest, "bad_request", "缺少参数 id")

	if item.Title != http.StatusText(http.StatusBadRequest) {
		t.Fatalf("Title = %q", item.Title)
	}

	if item.Type != problem.TYPE_ABOUT_BLANK {
		t.Fatalf("Type = %q", item.Type)
	}

	if item.Error() != "400 Bad Request: 缺少参数 id" {
		t.Fatalf("Error() = %q", item.Error())
	}

	invalid := problem.New(999, "", "")
	if invalid.Status != http.StatusInternalServerError {
		t.Fatalf("非法状态码应回退到 500, 实际 = %d", invalid.Status)
	}
}

func TestNewFromValidation(t *testing.T) {
	failures := validate.Errors{{Field: "Name", Rule: "required"}}
	item := problem.NewFromValidation(failures)
	if item == nil {
		t.Fatal("期望转换成功")
	}

	if item.Status != http.StatusUnprocessableEntity || item.Code != problem.CODE_VALIDATION_FAILED {
		t.Fatalf("状态码或业务码错误: %+v", item)
	}

	if item.Type != problem.TYPE_VALIDATION {
		t.Fatalf("Type = %q", item.Type)
	}

	if problem.NewFromValidation(errors.New("other")) != nil {
		t.Fatal("非校验错误应返回 nil")
	}

	if problem.NewFromValidation(nil) != nil {
		t.Fatal("nil 应返回 nil")
	}
}

func TestNewFromError(t *testing.T) {
	httpErr := &leego.HTTPError{Status: http.StatusForbidden, Code: "forbidden", Message: "无权访问"}
	item := problem.NewFromError(httpErr)
	if item.Status != http.StatusForbidden || item.Code != "forbidden" || item.Detail != "无权访问" {
		t.Fatalf("HTTPError 映射错误: %+v", item)
	}

	internal := problem.NewFromError(errors.New("database password=123456"))
	if internal.Status != http.StatusInternalServerError {
		t.Fatalf("状态码 = %d", internal.Status)
	}

	if internal.Detail == "database password=123456" {
		t.Fatal("内部错误细节不应回显")
	}
}

func TestProblemWrite(t *testing.T) {
	engine := leego.NewEngine()
	engine.GET("/users/:id", func(ctx *leego.Context) {
		problem.New(http.StatusNotFound, "user_not_found", "用户不存在").Write(ctx)
	})

	client := testutil.NewClient(engine)
	response := client.Get("/users/42")
	response.AssertStatus(t, http.StatusNotFound)
	response.AssertHeader(t, "Content-Type", "application/problem+json; charset=utf-8")

	var payload struct {
		Type     string `json:"type"`
		Title    string `json:"title"`
		Status   int    `json:"status"`
		Detail   string `json:"detail"`
		Instance string `json:"instance"`
		Code     string `json:"code"`
	}

	payloadData := response.JSONMap(t)
	if payloadData["instance"] != "/users/42" {
		t.Fatalf("Instance = %v", payloadData["instance"])
	}

	response.BindJSON(t, &payload)
	if payload.Title != http.StatusText(http.StatusNotFound) || payload.Code != "user_not_found" {
		t.Fatalf("响应内容错误: %+v", payload)
	}
}

func TestProblemWriteDoesNotOverwrite(t *testing.T) {
	engine := leego.NewEngine()
	engine.GET("/done", func(ctx *leego.Context) {
		_ = ctx.ResponseJSON(http.StatusOK, map[string]string{"status": "ok"})
		problem.New(http.StatusBadRequest, "late", "重复写入").Write(ctx)
	})

	client := testutil.NewClient(engine)
	response := client.Get("/done")
	response.AssertStatus(t, http.StatusOK)
	response.AssertContains(t, `"status":"ok"`)
}

func TestProblemWriteToAndMarshal(t *testing.T) {
	item := problem.New(http.StatusConflict, "conflict", "版本冲突")
	item.Type = ""

	payload, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var decoded map[string]any
	if err = json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if decoded["type"] != problem.TYPE_ABOUT_BLANK {
		t.Fatalf("空 Type 应序列化为 about:blank, 实际 = %v", decoded["type"])
	}

	engine := leego.NewEngine()
	engine.GET("/raw", func(ctx *leego.Context) {
		item.WriteTo(ctx.Response)
	})

	response := testutil.NewClient(engine).Get("/raw")
	response.AssertStatus(t, http.StatusConflict)
	response.AssertContains(t, "版本冲突")
}
