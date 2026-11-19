// Package problem 实现 RFC 9457（application/problem+json）错误响应。
//
// 使用场景：对外 API 需要稳定的错误结构，且希望把校验失败、业务错误码与
// HTTP 状态码解耦。包本身只依赖标准库、leego 根包与 validate。
package problem

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/validate"
)

const (
	// TYPE_ABOUT_BLANK 为未指定类型时的默认值。
	TYPE_ABOUT_BLANK = "about:blank"
	// TYPE_VALIDATION 表示请求参数校验失败。
	TYPE_VALIDATION = "https://leego.dev/problems/validation-error"
	// TYPE_INTERNAL 表示服务端不可分类的错误。
	TYPE_INTERNAL = "https://leego.dev/problems/internal-error"

	// CODE_VALIDATION_FAILED 为校验失败的稳定业务码。
	CODE_VALIDATION_FAILED = "validation_failed"
	// CODE_INTERNAL_ERROR 为内部错误的稳定业务码。
	CODE_INTERNAL_ERROR = "internal_error"
)

// Problem 是 RFC 9457 定义的问题详情对象。
//
// 字段语义与规范一致：Type 为问题类型 URI，Title 为稳定的短描述，
// Detail 面向本次请求，Instance 指向具体实例（通常是请求路径）。
// Code 与 Errors 是面向业务的扩展字段，不属于规范但允许附加。
type Problem struct {
	// Type 为问题类型 URI；为空时序列化为 about:blank。
	Type string `json:"type,omitempty"`
	// Title 为问题类型的简短描述，同一 Type 应保持稳定。
	Title string `json:"title"`
	// Status 为 HTTP 状态码，必须与响应状态行一致。
	Status int `json:"status"`
	// Detail 为本次问题的具体说明。
	Detail string `json:"detail,omitempty"`
	// Instance 为问题实例标识，通常写入请求路径。
	Instance string `json:"instance,omitempty"`
	// Code 为供客户端分支处理的稳定业务码。
	Code string `json:"code,omitempty"`
	// Errors 为字段级错误明细，仅校验类问题使用。
	Errors []validate.FieldError `json:"errors,omitempty"`
}

// New 创建问题详情。
//
// 参数：
//   - status：HTTP 状态码；
//   - code：稳定业务码，可为空；
//   - detail：面向本次请求的说明，可为空。
//
// Title 自动取 http.StatusText(status)，Type 使用 about:blank；
// 需要更精确的类型时直接修改返回值的字段。
func New(status int, code, detail string) *Problem {
	if status < 100 || status > 599 {
		status = http.StatusInternalServerError
	}

	title := http.StatusText(status)
	if title == "" {
		title = "Unknown Error"
	}

	return &Problem{
		Type:   TYPE_ABOUT_BLANK,
		Title:  title,
		Status: status,
		Detail: detail,
		Code:   code,
	}
}

// NewFromValidation 将 validate.Errors 转换为 422 问题详情。
//
// 传入 nil 或非校验错误时返回 nil，调用方应回退到 NewFromError。
func NewFromValidation(err error) *Problem {
	if err == nil {
		return nil
	}

	var failures validate.Errors
	if !errors.As(err, &failures) {
		return nil
	}

	result := New(http.StatusUnprocessableEntity, CODE_VALIDATION_FAILED, "请求参数校验失败")
	result.Type = TYPE_VALIDATION
	result.Errors = failures
	return result
}

// NewFromError 将任意错误映射为问题详情。
//
// 映射优先级：
//  1. 校验错误 → 422；
//  2. leego.HTTPError → 使用其状态码、业务码与消息；
//  3. 其他错误 → 500，且**不回显原始错误文本**，避免泄漏内部细节。
func NewFromError(err error) *Problem {
	if err == nil {
		return nil
	}

	if validation := NewFromValidation(err); validation != nil {
		return validation
	}

	var httpErr *leego.HTTPError
	if errors.As(err, &httpErr) {
		result := New(httpErr.Status, httpErr.Code, httpErr.Message)
		return result
	}

	result := New(http.StatusInternalServerError, CODE_INTERNAL_ERROR, http.StatusText(http.StatusInternalServerError))
	result.Type = TYPE_INTERNAL
	return result
}

// Error 实现 error 接口，便于把 Problem 继续向上抛出。
func (own *Problem) Error() string {
	if own == nil {
		return ""
	}

	if own.Detail != "" {
		return fmt.Sprintf("%d %s: %s", own.Status, own.Title, own.Detail)
	}

	return fmt.Sprintf("%d %s", own.Status, own.Title)
}

// Write 将问题详情写入响应。
//
// 若响应已经开始写入（状态码非 0），则不再覆盖，保持“首个响应生效”的语义。
// Content-Type 固定为 application/problem+json; charset=utf-8。
func (own *Problem) Write(ctx *leego.Context) {
	if own == nil || ctx == nil || ctx.GetStatusCode() != 0 {
		return
	}

	if own.Instance == "" && ctx.GetURL() != nil {
		own.Instance = ctx.GetURL().Path
	}

	payload, err := own.MarshalJSON()
	if err != nil {
		ctx.ResponseError(http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))
		return
	}

	ctx.SetResponseHeader("Content-Type", "application/problem+json; charset=utf-8")
	if err = ctx.ResponseBytes(own.Status, payload); err != nil {
		ctx.AddError(err)
	}
}

// WriteTo 将问题详情写入标准库 ResponseWriter。
func (own *Problem) WriteTo(resp http.ResponseWriter) {
	if own == nil || resp == nil {
		return
	}

	payload, err := own.MarshalJSON()
	if err != nil {
		http.Error(resp, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	resp.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	resp.WriteHeader(own.Status)
	_, _ = resp.Write(payload)
}

// MarshalJSON 保证 Type 为空时输出 about:blank，语义符合 RFC 9457 第 3.1 节。
func (own *Problem) MarshalJSON() ([]byte, error) {
	type alias Problem
	copied := alias(*own)
	if copied.Type == "" {
		copied.Type = TYPE_ABOUT_BLANK
	}

	return json.Marshal(copied)
}
