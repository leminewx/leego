package upload

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/logging"
)

const (
	// _DEFAULT_ACTION_PARAM 为动作查询参数名。
	_DEFAULT_ACTION_PARAM = "action"
	// _DEFAULT_ID_PARAM 为会话查询参数名。
	_DEFAULT_ID_PARAM = "id"
	// _DEFAULT_INDEX_PARAM 为分片序号查询参数名。
	_DEFAULT_INDEX_PARAM = "index"
	// _DEFAULT_MAX_SIZE 为单文件上限（5 GiB）。
	_DEFAULT_MAX_SIZE = 5 << 30
	// _DEFAULT_PART_SIZE 为默认分片大小（4 MiB）。
	_DEFAULT_PART_SIZE = 4 << 20
	// _DEFAULT_MAX_PARTS 为默认分片数量上限。
	_DEFAULT_MAX_PARTS = 10000

	// _ACTION_INIT 创建上传会话。
	_ACTION_INIT = "init"
	// _ACTION_PART 上传一个分片。
	_ACTION_PART = "part"
	// _ACTION_COMPLETE 拼接分片。
	_ACTION_COMPLETE = "complete"
	// _ACTION_ABORT 放弃上传。
	_ACTION_ABORT = "abort"
	// _ACTION_STATUS 查询上传状态。
	_ACTION_STATUS = "status"
)

// Options 定义 HTTP 处理器行为。
type Options struct {
	// ActionParam 为动作参数名，空值时使用 action。
	ActionParam string
	// IDParam 为会话参数名，空值时使用 id。
	IDParam string
	// IndexParam 为分片序号参数名，空值时使用 index。
	IndexParam string
	// MaxSize 为单文件上限，非正数时使用 5 GiB。
	MaxSize int64
	// DefaultPartSize 为客户端未指定分片大小时的默认值，非正数时使用 4 MiB。
	DefaultPartSize int64
	// MaxParts 为分片数量上限，非正数时使用 10000。
	MaxParts int
}

// NewDefaultOptions 返回推荐配置。
func NewDefaultOptions() Options {
	return Options{
		ActionParam:     _DEFAULT_ACTION_PARAM,
		IDParam:         _DEFAULT_ID_PARAM,
		IndexParam:      _DEFAULT_INDEX_PARAM,
		MaxSize:         _DEFAULT_MAX_SIZE,
		DefaultPartSize: _DEFAULT_PART_SIZE,
		MaxParts:        _DEFAULT_MAX_PARTS,
	}
}

// normalize 修正非法配置。
func (own Options) normalize() Options {
	defaults := NewDefaultOptions()

	if strings.TrimSpace(own.ActionParam) == "" {
		own.ActionParam = defaults.ActionParam
	}

	if strings.TrimSpace(own.IDParam) == "" {
		own.IDParam = defaults.IDParam
	}

	if strings.TrimSpace(own.IndexParam) == "" {
		own.IndexParam = defaults.IndexParam
	}

	if own.MaxSize <= 0 {
		own.MaxSize = defaults.MaxSize
	}

	if own.DefaultPartSize <= 0 {
		own.DefaultPartSize = defaults.DefaultPartSize
	}

	if own.MaxParts <= 0 {
		own.MaxParts = defaults.MaxParts
	}

	return own
}

// Option 修改 Options。
type Option func(*Options)

// UseActionParam 设置动作参数名。
func UseActionParam(name string) Option {
	return func(options *Options) {
		options.ActionParam = name
	}
}

// UseIDParam 设置会话参数名。
func UseIDParam(name string) Option {
	return func(options *Options) {
		options.IDParam = name
	}
}

// UseIndexParam 设置分片序号参数名。
func UseIndexParam(name string) Option {
	return func(options *Options) {
		options.IndexParam = name
	}
}

// UseMaxSize 设置单文件上限。
func UseMaxSize(size int64) Option {
	return func(options *Options) {
		options.MaxSize = size
	}
}

// UseDefaultPartSize 设置默认分片大小。
func UseDefaultPartSize(size int64) Option {
	return func(options *Options) {
		options.DefaultPartSize = size
	}
}

// UseMaxParts 设置分片数量上限。
func UseMaxParts(count int) Option {
	return func(options *Options) {
		options.MaxParts = count
	}
}

// handler 保存上传处理器的运行期状态。
type handler struct {
	// storage 为分片存储。
	storage Storage
	// options 为规范化后的配置。
	options Options
}

// NewHandler 创建分片上传处理器。
//
// 同一个处理器按查询参数 dispatch：
//
//	POST   ?action=init                 创建会话，JSON 入参 {name,size,part_size}
//	PUT    ?action=part&id=..&index=..  上传一个分片，请求体为原始字节
//	POST   ?action=complete&id=..       拼接分片，返回 {path}
//	DELETE ?action=abort&id=..          放弃上传
//	GET    ?action=status&id=..         查询会话状态
//
// 分片不经过 multipart 解析，直接读取请求体，避免额外拷贝。
func NewHandler(storage Storage, options ...Option) leego.HandleFunc {
	if storage == nil {
		panic("upload: storage must not be nil")
	}

	settings := NewDefaultOptions()
	for _, option := range options {
		if option != nil {
			option(&settings)
		}
	}

	own := &handler{storage: storage, options: settings.normalize()}
	return own.serve
}

// serve 按动作分发请求。
func (own *handler) serve(ctx *leego.Context) {
	action := strings.ToLower(strings.TrimSpace(ctx.GetQueryValue(own.options.ActionParam)))

	switch action {
	case _ACTION_INIT:
		own.init(ctx)
	case _ACTION_PART:
		own.putPart(ctx)
	case _ACTION_COMPLETE:
		own.complete(ctx)
	case _ACTION_ABORT:
		own.abort(ctx)
	case _ACTION_STATUS:
		own.status(ctx)
	default:
		own.fail(ctx, http.StatusBadRequest, ErrInvalidSession)
	}
}

// init 创建上传会话。
func (own *handler) init(ctx *leego.Context) {
	body, err := ctx.ReadBody()
	if err != nil {
		own.fail(ctx, http.StatusBadRequest, err)
		return
	}

	var request InitRequest
	if err = json.Unmarshal(body, &request); err != nil {
		own.fail(ctx, http.StatusBadRequest, ErrInvalidSession)
		return
	}

	if request.PartSize <= 0 {
		request.PartSize = own.options.DefaultPartSize
	}

	if request.Size <= 0 {
		own.fail(ctx, http.StatusBadRequest, ErrInvalidSession)
		return
	}

	if request.Size > own.options.MaxSize {
		own.fail(ctx, http.StatusRequestEntityTooLarge, ErrInvalidSession)
		return
	}

	count := request.Size / request.PartSize
	if request.Size%request.PartSize != 0 {
		count++
	}

	if count > int64(own.options.MaxParts) {
		own.fail(ctx, http.StatusRequestEntityTooLarge, ErrInvalidSession)
		return
	}

	session, err := own.storage.Init(ctx.Request.Context(), request)
	if err != nil {
		own.fail(ctx, http.StatusBadRequest, err)
		return
	}

	_ = ctx.ResponseJSON(http.StatusOK, session)
}

// putPart 写入一个分片。
func (own *handler) putPart(ctx *leego.Context) {
	id := strings.TrimSpace(ctx.GetQueryValue(own.options.IDParam))
	index, err := strconv.Atoi(strings.TrimSpace(ctx.GetQueryValue(own.options.IndexParam)))
	if err != nil || index < 0 {
		own.fail(ctx, http.StatusBadRequest, ErrPartIndex)
		return
	}

	if ctx.Request.Body == nil {
		own.fail(ctx, http.StatusBadRequest, ErrInvalidSession)
		return
	}

	size := ctx.Request.ContentLength
	if err = own.storage.PutPart(ctx.Request.Context(), id, index, ctx.Request.Body, size); err != nil {
		own.fail(ctx, statusForError(err), err)
		return
	}

	_ = ctx.ResponseJSON(http.StatusOK, map[string]any{"index": index, "size": size})
}

// complete 拼接全部分片。
func (own *handler) complete(ctx *leego.Context) {
	id := strings.TrimSpace(ctx.GetQueryValue(own.options.IDParam))

	path, err := own.storage.Complete(ctx.Request.Context(), id, nil)
	if err != nil {
		own.fail(ctx, statusForError(err), err)
		return
	}

	_ = ctx.ResponseJSON(http.StatusOK, map[string]string{"path": path})
}

// abort 放弃上传。
func (own *handler) abort(ctx *leego.Context) {
	id := strings.TrimSpace(ctx.GetQueryValue(own.options.IDParam))
	if err := own.storage.Abort(ctx.Request.Context(), id); err != nil {
		own.fail(ctx, statusForError(err), err)
		return
	}

	_ = ctx.ResponseJSON(http.StatusOK, map[string]bool{"ok": true})
}

// status 查询会话状态。
func (own *handler) status(ctx *leego.Context) {
	id := strings.TrimSpace(ctx.GetQueryValue(own.options.IDParam))

	session, err := own.storage.GetStatus(ctx.Request.Context(), id)
	if err != nil {
		own.fail(ctx, statusForError(err), err)
		return
	}

	_ = ctx.ResponseJSON(http.StatusOK, session)
}

// _GENERIC_ERROR 为未知内部错误的对外文案。
const _GENERIC_ERROR = "upload: internal error"

// fail 输出统一错误响应。
//
// 只有白名单内的业务错误才回显原文；其它错误通常来自 os/json，
// 可能携带服务器绝对路径等实现细节，因此只记日志并返回固定文案。
func (own *handler) fail(ctx *leego.Context, code int, err error) {
	message := publicMessage(err)
	if message == "" {
		logging.LogWarn("upload: request failed: %v", err)
		message = _GENERIC_ERROR
	}

	_ = ctx.ResponseJSON(code, map[string]string{"error": message})
}

// publicMessage 返回可安全回显的错误文案；未知错误返回空字符串。
func publicMessage(err error) string {
	switch {
	case errors.Is(err, ErrSessionNotFound),
		errors.Is(err, ErrIncomplete),
		errors.Is(err, ErrPartSize),
		errors.Is(err, ErrPartIndex),
		errors.Is(err, ErrMergedSize),
		errors.Is(err, ErrInvalidSession),
		errors.Is(err, ErrInvalidRoot),
		errors.Is(err, context.Canceled),
		errors.Is(err, context.DeadlineExceeded):
		return err.Error()
	}

	return ""
}

// statusForError 把存储错误映射为 HTTP 状态码。
func statusForError(err error) int {
	switch {
	case errors.Is(err, ErrSessionNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrIncomplete):
		return http.StatusConflict
	case errors.Is(err, ErrPartSize), errors.Is(err, ErrPartIndex), errors.Is(err, ErrInvalidSession), errors.Is(err, ErrInvalidRoot):
		return http.StatusBadRequest
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return http.StatusRequestTimeout
	default:
		return http.StatusInternalServerError
	}
}
