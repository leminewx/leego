package upload

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

// TestUploadOptionsNormalizeDefaults 验证零值配置补齐为默认值。
func TestUploadOptionsNormalizeDefaults(t *testing.T) {
	if options := (Options{}).normalize(); options != NewDefaultOptions() {
		t.Fatalf("零值配置未补齐默认值: %+v", options)
	}
}

// TestUploadOptionsApplied 验证全部选项函数都写入对应字段。
func TestUploadOptionsApplied(t *testing.T) {
	options := NewDefaultOptions()
	for _, option := range []Option{
		UseActionParam("op"),
		UseIDParam("sid"),
		UseIndexParam("idx"),
		UseMaxSize(1024),
		UseDefaultPartSize(256),
		UseMaxParts(8),
	} {
		if option == nil {
			t.Fatal("选项函数不应为 nil")
		}

		option(&options)
	}

	if options.ActionParam != "op" || options.IDParam != "sid" || options.IndexParam != "idx" {
		t.Fatalf("参数名选项未生效: %+v", options)
	}

	if options.MaxSize != 1024 || options.DefaultPartSize != 256 || options.MaxParts != 8 {
		t.Fatalf("容量选项未生效: %+v", options)
	}
}

// TestStatusForErrorMapping 验证存储错误到 HTTP 状态码的映射。
func TestStatusForErrorMapping(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{err: ErrSessionNotFound, want: http.StatusNotFound},
		{err: ErrIncomplete, want: http.StatusConflict},
		{err: ErrPartSize, want: http.StatusBadRequest},
		{err: ErrPartIndex, want: http.StatusBadRequest},
		{err: ErrInvalidSession, want: http.StatusBadRequest},
		{err: ErrInvalidRoot, want: http.StatusBadRequest},
		{err: context.Canceled, want: http.StatusRequestTimeout},
		{err: context.DeadlineExceeded, want: http.StatusRequestTimeout},
		{err: errors.New("boom"), want: http.StatusInternalServerError},
	}

	for _, testCase := range cases {
		if got := statusForError(testCase.err); got != testCase.want {
			t.Fatalf("statusForError(%v) = %d, want %d", testCase.err, got, testCase.want)
		}
	}
}
