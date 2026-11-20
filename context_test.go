package leego

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"html/template"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitee.com/leminewx/leego/response/sse"
)

// createTestContext 构造一个不绑定引擎的上下文。
//
// 返回的 recorder 用于断言状态码、响应头与响应体。
func createTestContext(req *http.Request) (*Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	return NewContext(recorder, req), recorder
}

// TestNewContextBasics 验证独立上下文的基础字段。
func TestNewContextBasics(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/users/42?q=go", nil)
	req.RemoteAddr = "192.0.2.10:54321"

	ctx, _ := createTestContext(req)

	if ctx.GetEngine() != nil {
		t.Fatal("NewContext 构造的上下文不应绑定引擎")
	}

	if ctx.GetServiceName() != "" {
		t.Fatalf("未绑定引擎时服务名 = %q, want 空字符串", ctx.GetServiceName())
	}

	if ctx.GetStartTime().IsZero() {
		t.Fatal("GetStartTime 不能为零值")
	}

	if ctx.GetURL() != req.URL {
		t.Fatal("GetURL 必须返回请求 URL")
	}

	if got := ctx.GetRemoteIP(); got != "192.0.2.10" {
		t.Fatalf("GetRemoteIP() = %q, want 192.0.2.10", got)
	}

	if got := ctx.GetClientIP(); got != "192.0.2.10" {
		t.Fatalf("未配置可信代理时 GetClientIP() = %q, want 192.0.2.10", got)
	}

	if ctx.GetStatusCode() != 0 {
		t.Fatalf("尚未写入响应时状态码 = %d, want 0", ctx.GetStatusCode())
	}
}

// TestContextPathAndQueryValues 验证路由参数与查询参数读取。
func TestContextPathAndQueryValues(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/users/42?q=go&q=lang&empty=", nil)
	ctx, _ := createTestContext(req)

	ctx.setPathValues(map[string]string{"id": "42"})

	if got := ctx.GetPathValue("id"); got != "42" {
		t.Fatalf("GetPathValue(id) = %q, want 42", got)
	}

	if got := ctx.GetPathValue("missing"); got != "" {
		t.Fatalf("不存在的参数 = %q, want 空字符串", got)
	}

	if got := ctx.GetPathValues()["id"]; got != "42" {
		t.Fatalf("GetPathValues()[id] = %q, want 42", got)
	}

	if got := ctx.GetQueryValue("q"); got != "go" {
		t.Fatalf("GetQueryValue(q) = %q, want go", got)
	}

	if got := ctx.GetQueryValue("empty"); got != "" {
		t.Fatalf("空查询参数 = %q", got)
	}

	// 静态路由命中时路由参数为 nil。
	ctx.setPathValues(nil)
	if ctx.GetPathValues() != nil {
		t.Fatal("静态路由的 GetPathValues 必须返回 nil")
	}
}

// TestContextHeaderOperations 验证请求头与响应头读写。
func TestContextHeaderOperations(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	ctx, _ := createTestContext(req)

	if got := ctx.GetContentType(); got != "application/json" {
		t.Fatalf("GetContentType() = %q, want application/json", got)
	}

	ctx.SetRequestHeader("X-Token", "abc")
	if got := ctx.GetRequestHeader("X-Token"); got != "abc" {
		t.Fatalf("请求头 = %q, want abc", got)
	}

	// 空值表示删除，而不是写入空字符串。
	ctx.SetRequestHeader("X-Token", "")
	if got := ctx.GetRequestHeader("X-Token"); got != "" {
		t.Fatalf("删除后的请求头 = %q, want 空字符串", got)
	}

	ctx.SetResponseHeader("X-Trace", "trace-1")
	if got := ctx.GetResponseHeader("X-Trace"); got != "trace-1" {
		t.Fatalf("响应头 = %q, want trace-1", got)
	}

	ctx.SetResponseHeader("X-Trace", "")
	if got := ctx.GetResponseHeader("X-Trace"); got != "" {
		t.Fatalf("删除后的响应头 = %q, want 空字符串", got)
	}

	ctx.SetResponseHeader("X-A", "1")
	ctx.SetResponseHeader("X-B", "2")
	ctx.DeleteResponseHeader("X-A", "X-B")
	if ctx.GetResponseHeader("X-A") != "" || ctx.GetResponseHeader("X-B") != "" {
		t.Fatal("DeleteResponseHeader 必须删除全部指定头")
	}

	req.Header.Set("X-Del", "1")
	ctx.DeleteRequestHeader("X-Del")
	if ctx.GetRequestHeader("X-Del") != "" {
		t.Fatal("DeleteRequestHeader 必须删除指定请求头")
	}
}

// TestContextReadBodyCaches 验证请求体只读取一次并缓存。
func TestContextReadBodyCaches(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("hello body"))
	ctx, _ := createTestContext(req)

	first, err := ctx.ReadBody()
	if err != nil {
		t.Fatalf("ReadBody() error = %v", err)
	}

	second, err := ctx.ReadBody()
	if err != nil {
		t.Fatalf("第二次 ReadBody() error = %v", err)
	}

	if string(first) != "hello body" || !bytes.Equal(first, second) {
		t.Fatalf("ReadBody 多次读取结果不一致: %q / %q", first, second)
	}

	// GetRequestBody 与 ReadBody 语义一致。
	third, err := ctx.GetRequestBody()
	if err != nil || !bytes.Equal(first, third) {
		t.Fatalf("GetRequestBody = (%q, %v), want %q", third, err, first)
	}
}

// TestContextReadBodyRespectsMaxBytes 验证请求体上限。
func TestContextReadBodyRespectsMaxBytes(t *testing.T) {
	engine := NewEngine(WithMaxBodyBytes(4), DisableStartupLog())

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("too large"))
	ctx, _ := createTestContext(req)
	ctx.engine = engine

	if _, err := ctx.ReadBody(); err == nil {
		t.Fatal("超过 MaxBodyBytes 的请求体必须返回错误")
	}

	// 空请求体不是错误。
	empty, _ := createTestContext(httptest.NewRequest(http.MethodGet, "/", nil))
	data, err := empty.ReadBody()
	if err != nil || len(data) != 0 {
		t.Fatalf("空请求体 ReadBody = (%v, %v), want (空, nil)", data, err)
	}
}

// TestContextBindJSON 验证 JSON 绑定与非法输入处理。
func TestContextBindJSON(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"leego","age":3}`))
	req.Header.Set("Content-Type", "application/json")
	ctx, _ := createTestContext(req)

	var bound payload
	if err := ctx.Bind(&bound); err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	if bound.Name != "leego" || bound.Age != 3 {
		t.Fatalf("绑定结果 = %+v", bound)
	}

	invalid := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{invalid"))
	invalid.Header.Set("Content-Type", "application/json")
	invalidCtx, _ := createTestContext(invalid)

	if err := invalidCtx.BindJSON(&bound); err == nil {
		t.Fatal("非法 JSON 必须返回错误")
	}

	if err := invalidCtx.Bind(nil); err == nil {
		t.Fatal("nil 目标必须返回错误")
	}

	unsupported := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
	unsupported.Header.Set("Content-Type", "text/csv")
	unsupportedCtx, _ := createTestContext(unsupported)

	if err := unsupportedCtx.Bind(&bound); !errors.Is(err, ErrUnsupportedMediaType) {
		t.Fatalf("不支持的类型错误 = %v, want ErrUnsupportedMediaType", err)
	}
}

// TestContextResponseWriters 验证各类响应输出格式。
func TestContextResponseWriters(t *testing.T) {
	ctx, recorder := createTestContext(httptest.NewRequest(http.MethodGet, "/", nil))

	if err := ctx.ResponseJSON(http.StatusCreated, JSON{"name": "leego"}); err != nil {
		t.Fatalf("ResponseJSON() error = %v", err)
	}

	if recorder.Code != http.StatusCreated {
		t.Fatalf("ResponseJSON 状态码 = %d, want 201", recorder.Code)
	}

	if got := recorder.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("ResponseJSON Content-Type = %q", got)
	}

	if !strings.Contains(recorder.Body.String(), `"name":"leego"`) {
		t.Fatalf("ResponseJSON 响应体 = %q", recorder.Body.String())
	}

	// XML 响应。
	xmlCtx, xmlRecorder := createTestContext(httptest.NewRequest(http.MethodGet, "/", nil))
	type item struct {
		XMLName xml.Name `xml:"item"`
		Name    string   `xml:"name"`
	}
	if err := xmlCtx.ResponseXML(http.StatusOK, item{Name: "leego"}); err != nil {
		t.Fatalf("ResponseXML() error = %v", err)
	}

	if !strings.Contains(xmlRecorder.Body.String(), "<item>") {
		t.Fatalf("ResponseXML 响应体 = %q", xmlRecorder.Body.String())
	}

	// 纯文本。
	plainCtx, plainRecorder := createTestContext(httptest.NewRequest(http.MethodGet, "/", nil))
	if err := plainCtx.ResponseText(http.StatusAccepted, "plain"); err != nil {
		t.Fatalf("ResponseText() error = %v", err)
	}

	if plainRecorder.Code != http.StatusAccepted || plainRecorder.Body.String() != "plain" {
		t.Fatalf("ResponseText = (%d, %q)", plainRecorder.Code, plainRecorder.Body.String())
	}

	if got := plainRecorder.Header().Get("Content-Type"); !strings.Contains(got, "text/plain") {
		t.Fatalf("ResponseText Content-Type = %q", got)
	}

	// HTML。
	htmlCtx, htmlRecorder := createTestContext(httptest.NewRequest(http.MethodGet, "/", nil))
	if err := htmlCtx.ResponseHTML(http.StatusOK, "<h1>leego</h1>"); err != nil {
		t.Fatalf("ResponseHTML() error = %v", err)
	}

	if !strings.Contains(htmlRecorder.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("ResponseHTML Content-Type = %q", htmlRecorder.Header().Get("Content-Type"))
	}

	// 原始字节：不设置 Content-Type，由调用方自行控制。
	bytesCtx, bytesRecorder := createTestContext(httptest.NewRequest(http.MethodGet, "/", nil))
	if err := bytesCtx.ResponseBytes(http.StatusOK, []byte{0x01, 0x02}); err != nil {
		t.Fatalf("ResponseBytes() error = %v", err)
	}

	if !bytes.Equal(bytesRecorder.Body.Bytes(), []byte{0x01, 0x02}) {
		t.Fatalf("ResponseBytes 响应体 = %v", bytesRecorder.Body.Bytes())
	}
}

// TestContextWriteError 验证错误响应的幂等性。
func TestContextWriteError(t *testing.T) {
	ctx, recorder := createTestContext(httptest.NewRequest(http.MethodGet, "/", nil))

	ctx.ResponseError(http.StatusNotFound, "missing route")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("状态码 = %d, want 404", recorder.Code)
	}

	if !strings.Contains(recorder.Body.String(), "missing route") {
		t.Fatalf("响应体 = %q", recorder.Body.String())
	}

	if !strings.Contains(recorder.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("Content-Type = %q", recorder.Header().Get("Content-Type"))
	}

	// 响应已写入后再次 ResponseError 只能记录错误，不能改变响应。
	body := recorder.Body.String()
	ctx.ResponseError(http.StatusInternalServerError, "second")

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("重复 ResponseError 后状态码 = %d, want 404", recorder.Code)
	}

	if recorder.Body.String() != body {
		t.Fatalf("重复 ResponseError 改变了响应体: %q", recorder.Body.String())
	}

	if ctx.GetLastError() == nil {
		t.Fatal("ResponseError 必须记录错误链")
	}

	// 空消息回退为标准状态文本。
	emptyCtx, emptyRecorder := createTestContext(httptest.NewRequest(http.MethodGet, "/", nil))
	emptyCtx.ResponseError(http.StatusBadRequest, "")
	if !strings.Contains(emptyRecorder.Body.String(), http.StatusText(http.StatusBadRequest)) {
		t.Fatalf("空消息响应体 = %q", emptyRecorder.Body.String())
	}
}

// TestContextRedirectTo 验证重定向与非法状态码。
func TestContextRedirectTo(t *testing.T) {
	ctx, recorder := createTestContext(httptest.NewRequest(http.MethodGet, "/old", nil))
	ctx.ResponseRedirect(http.StatusMovedPermanently, "/new")

	if recorder.Code != http.StatusMovedPermanently {
		t.Fatalf("状态码 = %d, want 301", recorder.Code)
	}

	if got := recorder.Header().Get("Location"); got != "/new" {
		t.Fatalf("Location = %q, want /new", got)
	}

	assertPanics(t, "非 3xx 重定向", func() {
		ctx.ResponseRedirect(http.StatusOK, "/bad")
	})
}

// TestContextCookies 验证 Cookie 写入与读取。
func TestContextCookies(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "abc"})

	ctx, recorder := createTestContext(req)

	value, err := ctx.GetCookieValue("session")
	if err != nil || value != "abc" {
		t.Fatalf("GetCookieValue = (%q, %v), want (abc, nil)", value, err)
	}

	if _, err = ctx.GetCookieValue("missing"); err == nil {
		t.Fatal("不存在的 Cookie 必须返回错误")
	}

	ctx.SetSameSite(http.SameSiteLaxMode)
	ctx.SetCookie(&http.Cookie{Name: "plain", Value: "1"})
	ctx.SetCookieData("data", "2", 60, "/", "example.com", true, true)

	cookies := recorder.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("写入 Cookie 数量 = %d, want 2", len(cookies))
	}

	byName := make(map[string]*http.Cookie, len(cookies))
	for _, cookie := range cookies {
		byName[cookie.Name] = cookie
	}

	if byName["plain"] == nil || byName["plain"].Value != "1" {
		t.Fatalf("plain Cookie = %+v", byName["plain"])
	}

	data := byName["data"]
	if data == nil || data.Value != "2" || data.MaxAge != 60 || !data.Secure || !data.HttpOnly {
		t.Fatalf("data Cookie = %+v", data)
	}

	if data.SameSite != http.SameSiteLaxMode {
		t.Fatalf("SameSite = %v, want Lax", data.SameSite)
	}
}

// TestContextErrorChain 验证错误链与默认错误响应。
func TestContextErrorChain(t *testing.T) {
	ctx, recorder := createTestContext(httptest.NewRequest(http.MethodGet, "/", nil))

	first := errors.New("first")
	ctx.AddError(nil)
	ctx.AddError(first)

	if len(ctx.GetErrors()) != 1 {
		t.Fatalf("错误数量 = %d, want 1", len(ctx.GetErrors()))
	}

	if !errors.Is(ctx.GetLastError(), first) {
		t.Fatalf("GetLastError() = %v, want first", ctx.GetLastError())
	}

	ctx.Fail(first)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("Fail 状态码 = %d, want 500", recorder.Code)
	}

	var body map[string]map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("错误响应不是合法 JSON: %v", err)
	}

	if body["error"]["code"] != "internal_error" || body["error"]["message"] != "first" {
		t.Fatalf("错误响应 = %+v", body)
	}

	// HTTPError 决定状态码、业务码与消息。
	httpCtx, httpRecorder := createTestContext(httptest.NewRequest(http.MethodGet, "/", nil))
	httpCtx.Fail(NewHTTPError(http.StatusNotFound, "not_found", "route not found", nil))

	if httpRecorder.Code != http.StatusNotFound {
		t.Fatalf("HTTPError 状态码 = %d, want 404", httpRecorder.Code)
	}

	if !strings.Contains(httpRecorder.Body.String(), `"code":"not_found"`) {
		t.Fatalf("HTTPError 响应体 = %q", httpRecorder.Body.String())
	}
}

// TestContextFailUsesErrorHandler 验证自定义错误处理器优先。
func TestContextFailUsesErrorHandler(t *testing.T) {
	engine := NewEngine(DisableStartupLog())
	engine.SetErrorHandler(func(ctx *Context, err error) {
		_ = ctx.ResponseText(http.StatusTeapot, err.Error())
	})

	ctx, recorder := createTestContext(httptest.NewRequest(http.MethodGet, "/", nil))
	ctx.engine = engine
	ctx.Fail(errors.New("custom"))

	if recorder.Code != http.StatusTeapot || recorder.Body.String() != "custom" {
		t.Fatalf("自定义错误处理器输出 = (%d, %q)", recorder.Code, recorder.Body.String())
	}
}

// TestContextDeriveContext 验证派生上下文的隔离性。
func TestContextDeriveContext(t *testing.T) {
	type contextKey struct{}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	parent, _ := createTestContext(req)
	parent.setPathValues(map[string]string{"id": "1"})
	parent.AddError(errors.New("boom"))

	derived := parent.DeriveContext(context.WithValue(context.Background(), contextKey{}, "value"))

	if value, ok := derived.GetContextValue(contextKey{}); !ok || value != "value" {
		t.Fatalf("派生上下文未继承标准库上下文: (%v, %v)", value, ok)
	}

	if derived.GetPathValue("id") != "1" {
		t.Fatal("派生上下文必须复制路由参数")
	}

	if len(derived.GetErrors()) != 1 {
		t.Fatalf("派生上下文错误数量 = %d, want 1", len(derived.GetErrors()))
	}

	// 修改派生上下文不得影响原上下文。
	derived.GetPathValues()["id"] = "changed"
	derived.AddError(errors.New("derived"))

	if parent.GetPathValue("id") != "1" {
		t.Fatal("派生上下文的参数修改泄漏到了原上下文")
	}

	if len(parent.GetErrors()) != 1 {
		t.Fatal("派生上下文的错误链修改泄漏到了原上下文")
	}

	if derived.GetEngine() != parent.GetEngine() {
		t.Fatal("派生上下文必须共享引擎")
	}

	// nil 上下文按 Background 处理。
	if derivedNil := parent.DeriveContext(nil); derivedNil.Request.Context() == nil {
		t.Fatal("DeriveContext(nil) 必须回退为 Background")
	}
}

// TestContextJWTClaims 验证 JWT 声明的存取。
func TestContextJWTClaims(t *testing.T) {
	ctx, _ := createTestContext(httptest.NewRequest(http.MethodGet, "/", nil))

	if claims, ok := ctx.GetJWTClaims(); ok || claims != nil {
		t.Fatalf("初始 JWT 声明 = (%v, %v), want (nil, false)", claims, ok)
	}

	ctx.SetJWTClaims("claims")

	claims, ok := ctx.GetJWTClaims()
	if !ok || claims != "claims" {
		t.Fatalf("JWT 声明 = (%v, %v), want (claims, true)", claims, ok)
	}
}

// TestContextIsWebsocket 验证 WebSocket 升级请求识别。
func TestContextIsWebsocket(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")

	ctx, _ := createTestContext(req)
	if !ctx.IsWebsocket() {
		t.Fatal("必须识别 WebSocket 升级请求")
	}

	missingUpgrade, _ := createTestContext(httptest.NewRequest(http.MethodGet, "/", nil))
	if missingUpgrade.IsWebsocket() {
		t.Fatal("普通请求不应被识别为 WebSocket")
	}
}

// TestContextFormValues 验证表单字段读取。
func TestContextFormValues(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/?source=query", strings.NewReader("name=leego&age=3"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	ctx, _ := createTestContext(req)

	if got := ctx.GetPostFormValue("name"); got != "leego" {
		t.Fatalf("GetPostFormValue(name) = %q, want leego", got)
	}

	if got := ctx.GetFormValue("source"); got != "query" {
		t.Fatalf("GetFormValue(source) = %q, want query", got)
	}

	if got := ctx.GetPostFormValue("source"); got != "" {
		t.Fatalf("GetPostFormValue 不应返回查询参数: %q", got)
	}
}

// TestContextMultipartUpload 验证 multipart 字段与文件保存。
func TestContextMultipartUpload(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	if err := writer.WriteField("name", "leego"); err != nil {
		t.Fatalf("写入表单字段失败: %v", err)
	}

	part, err := writer.CreateFormFile("file", "upload.txt")
	if err != nil {
		t.Fatalf("创建文件字段失败: %v", err)
	}

	if _, err = part.Write([]byte("uploaded content")); err != nil {
		t.Fatalf("写入文件内容失败: %v", err)
	}

	if err = writer.Close(); err != nil {
		t.Fatalf("关闭 multipart writer 失败: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	ctx, _ := createTestContext(req)

	if got := ctx.GetMultipartFormValue("name"); got != "leego" {
		t.Fatalf("GetMultipartFormValue(name) = %q, want leego", got)
	}

	source, header, err := ctx.GetFormFile("file")
	if err != nil {
		t.Fatalf("GetFormFile() error = %v", err)
	}
	defer func() { _ = source.Close() }()

	if header.Filename != "upload.txt" {
		t.Fatalf("上传文件名 = %q", header.Filename)
	}

	target := filepath.Join(t.TempDir(), "saved", "upload.txt")
	if err = ctx.SaveUploadedFile(header, target); err != nil {
		t.Fatalf("SaveUploadedFile() error = %v", err)
	}

	saved, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("读取保存文件失败: %v", err)
	}

	if string(saved) != "uploaded content" {
		t.Fatalf("保存内容 = %q", saved)
	}

	if err = ctx.SaveUploadedFile(nil, target); err == nil {
		t.Fatal("nil 文件头必须返回错误")
	}
}

// TestContextRenderHTML 验证模板渲染与错误分支。
func TestContextRenderHTML(t *testing.T) {
	directory := t.TempDir()
	filename := filepath.Join(directory, "index.html")
	if err := os.WriteFile(filename, []byte("<h1>{{.Name}}</h1>"), 0o600); err != nil {
		t.Fatalf("写入模板失败: %v", err)
	}

	engine := NewEngine(DisableStartupLog())
	if err := engine.LoadHTMLFiles(filename); err != nil {
		t.Fatalf("LoadHTMLFiles() error = %v", err)
	}

	ctx, recorder := createTestContext(httptest.NewRequest(http.MethodGet, "/", nil))
	ctx.engine = engine

	if err := ctx.ResponseTemplate("index.html", map[string]string{"Name": "leego"}); err != nil {
		t.Fatalf("ResponseTemplate() error = %v", err)
	}

	if !strings.Contains(recorder.Body.String(), "<h1>leego</h1>") {
		t.Fatalf("渲染结果 = %q", recorder.Body.String())
	}

	// 未绑定引擎时返回明确错误。
	detached, _ := createTestContext(httptest.NewRequest(http.MethodGet, "/", nil))
	if err := detached.ResponseTemplate("index.html", nil); err == nil {
		t.Fatal("未绑定引擎时必须返回错误")
	}

	// 已绑定引擎但未加载模板时同样返回错误。
	emptyEngine := NewEngine(DisableStartupLog())
	empty, _ := createTestContext(httptest.NewRequest(http.MethodGet, "/", nil))
	empty.engine = emptyEngine
	if err := empty.ResponseTemplate("index.html", nil); err == nil {
		t.Fatal("未加载模板时必须返回错误")
	}
}

// TestContextAttachmentHeader 验证下载文件名编码。
func TestContextAttachmentHeader(t *testing.T) {
	ctx, recorder := createTestContext(httptest.NewRequest(http.MethodGet, "/", nil))

	ctx.setAttachmentHeader("/files/report.pdf")
	if got := recorder.Header().Get("Content-Disposition"); got != `attachment; filename="report.pdf"` {
		t.Fatalf("ASCII 文件名 = %q", got)
	}

	ctx.setAttachmentHeader("/files/报告.pdf")
	got := recorder.Header().Get("Content-Disposition")
	if !strings.HasPrefix(got, "attachment; filename*=UTF-8''") {
		t.Fatalf("非 ASCII 文件名 = %q", got)
	}

	// 引号与反斜杠必须转义，避免破坏头部结构。
	if escaped := escapeQuotes(`a"b\c`); escaped != `a\"b\\c` {
		t.Fatalf("escapeQuotes = %q", escaped)
	}

	if isASCII("报告") || !isASCII("report.pdf") {
		t.Fatal("isASCII 判断错误")
	}
}

// TestContextClientIPWithTrustedProxy 验证可信代理链解析。
func TestContextClientIPWithTrustedProxy(t *testing.T) {
	engine := NewEngine(DisableStartupLog())
	if err := engine.SetTrustedProxies("10.0.0.0/8"); err != nil {
		t.Fatalf("SetTrustedProxies() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.5:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.8")

	ctx, _ := createTestContext(req)
	ctx.engine = engine

	if got := ctx.GetClientIP(); got != "203.0.113.9" {
		t.Fatalf("GetClientIP() = %q, want 203.0.113.9", got)
	}

	// 全部地址可信时回退为直连对端 IP。
	allTrusted := httptest.NewRequest(http.MethodGet, "/", nil)
	allTrusted.RemoteAddr = "10.0.0.5:1234"
	allTrusted.Header.Set("X-Forwarded-For", "10.0.0.7")

	trustedCtx, _ := createTestContext(allTrusted)
	trustedCtx.engine = engine

	if got := trustedCtx.GetClientIP(); got != "10.0.0.5" {
		t.Fatalf("全部可信时 GetClientIP() = %q, want 10.0.0.5", got)
	}
}

// TestContextClientIPIgnoresUntrustedPeerHeaders 验证直连对端不可信时忽略伪造的代理头。
func TestContextClientIPIgnoresUntrustedPeerHeaders(t *testing.T) {
	engine := NewEngine(DisableStartupLog())
	if err := engine.SetTrustedProxies("10.0.0.0/8"); err != nil {
		t.Fatalf("SetTrustedProxies() error = %v", err)
	}

	// 直连对端是公网地址，属于不可信链路：任何代理头都可能由客户端自带。
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.50:4321"
	req.Header.Set("X-Forwarded-For", "10.0.0.9, 1.2.3.4")
	req.Header.Set("X-Real-IP", "1.2.3.4")

	ctx, _ := createTestContext(req)
	ctx.engine = engine

	if got := ctx.GetClientIP(); got != "203.0.113.50" {
		t.Fatalf("GetClientIP() = %q, want 203.0.113.50（不得采信不可信对端携带的代理头）", got)
	}
}

// TestNormalizeIP 验证 IP 文本规范化。
func TestNormalizeIP(t *testing.T) {
	tests := map[string]string{
		"":                  "",
		"  ":                "",
		"192.0.2.1:8080":    "192.0.2.1",
		"[2001:db8::1]:443": "2001:db8::1",
		"[2001:db8::1]":     "2001:db8::1",
		"192.0.2.1":         "192.0.2.1",
	}

	for input, want := range tests {
		if got := normalizeIP(input); got != want {
			t.Errorf("normalizeIP(%q) = %q, want %q", input, got, want)
		}
	}
}

// formPayload 为表单绑定测试结构体。
type formPayload struct {
	// Name 通过 form 标签映射。
	Name string `form:"name"`
	// Age 验证有符号整数解析。
	Age int `form:"age"`
	// Count 验证无符号整数解析。
	Count uint `form:"count"`
	// Score 验证浮点数解析。
	Score float64 `form:"score"`
	// Active 验证布尔值解析。
	Active bool `form:"active"`
	// Tags 验证切片聚合。
	Tags []string `form:"tag"`
	// Plain 未声明标签时使用字段名匹配。
	Plain string
	// Skipped 使用 "-" 显式跳过。
	Skipped string `form:"-"`
	// private 未导出字段不应被绑定。
	private string
}

// formUnsupported 包含不支持的表单字段类型。
type formUnsupported struct {
	// Extra 为结构体类型，表单绑定不支持。
	Extra struct{} `form:"extra"`
}

// TestContextBindXML 验证 XML 请求体绑定。
func TestContextBindXML(t *testing.T) {
	type xmlUser struct {
		XMLName xml.Name `xml:"user"`
		Name    string   `xml:"name"`
		Age     int      `xml:"age"`
	}

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`<user><name>leego</name><age>3</age></user>`))
	req.Header.Set("Content-Type", "application/xml")

	ctx, _ := createTestContext(req)

	var user xmlUser
	if err := ctx.BindXML(&user); err != nil {
		t.Fatalf("BindXML() error = %v", err)
	}

	if user.Name != "leego" || user.Age != 3 {
		t.Fatalf("绑定结果 = %+v", user)
	}

	// 非法 XML 必须返回错误。
	badRequest := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`<user>`))
	badContext, _ := createTestContext(badRequest)
	if err := badContext.BindXML(&user); err == nil {
		t.Fatal("非法 XML 必须返回错误")
	}
}

// TestContextBindFormBindsSupportedTypes 验证表单各标量类型与切片绑定。
func TestContextBindFormBindsSupportedTypes(t *testing.T) {
	body := "name=leego&age=42&count=7&score=1.5&active=true&tag=a&tag=b&Plain=raw&Skipped=no"
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	ctx, _ := createTestContext(req)

	var payload formPayload
	if err := ctx.BindForm(&payload); err != nil {
		t.Fatalf("BindForm() error = %v", err)
	}

	if payload.Name != "leego" || payload.Age != 42 || payload.Count != 7 {
		t.Fatalf("标量绑定结果 = %+v", payload)
	}

	if payload.Score != 1.5 || !payload.Active {
		t.Fatalf("浮点/布尔绑定结果 = %+v", payload)
	}

	if len(payload.Tags) != 2 || payload.Tags[0] != "a" || payload.Tags[1] != "b" {
		t.Fatalf("切片绑定结果 = %v", payload.Tags)
	}

	if payload.Plain != "raw" {
		t.Fatalf("无标签字段 = %q, want raw", payload.Plain)
	}

	if payload.Skipped != "" {
		t.Fatalf("form:\"-\" 字段不应被绑定，实际为 %q", payload.Skipped)
	}

	if payload.private != "" {
		t.Fatalf("未导出字段不应被绑定，实际为 %q", payload.private)
	}
}

// TestContextBindFormRejectsInvalidValue 验证类型转换失败时返回字段级错误。
func TestContextBindFormRejectsInvalidValue(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("age=abc"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	ctx, _ := createTestContext(req)

	var payload formPayload
	err := ctx.BindForm(&payload)
	if err == nil || !strings.Contains(err.Error(), "bind field Age") {
		t.Fatalf("BindForm() = %v, want 字段级错误", err)
	}
}

// TestContextBindFormRejectsUnsupportedType 验证不支持的字段类型返回错误。
func TestContextBindFormRejectsUnsupportedType(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("extra=1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	ctx, _ := createTestContext(req)

	var payload formUnsupported
	err := ctx.BindForm(&payload)
	if err == nil || !strings.Contains(err.Error(), "unsupported field type") {
		t.Fatalf("BindForm() = %v, want 不支持类型错误", err)
	}
}

// TestContextBindDispatchesByContentType 验证 Bind 按 Content-Type 分发。
func TestContextBindDispatchesByContentType(t *testing.T) {
	type xmlUser struct {
		XMLName xml.Name `xml:"user"`
		Name    string   `xml:"name"`
	}

	xmlRequest := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`<user><name>leego</name></user>`))
	xmlRequest.Header.Set("Content-Type", "text/xml")
	xmlContext, _ := createTestContext(xmlRequest)

	var user xmlUser
	if err := xmlContext.Bind(&user); err != nil {
		t.Fatalf("XML 分发失败: %v", err)
	}

	if user.Name != "leego" {
		t.Fatalf("XML 分发结果 = %+v", user)
	}

	formRequest := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("name=leego"))
	formRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	formContext, _ := createTestContext(formRequest)

	var payload formPayload
	if err := formContext.Bind(&payload); err != nil {
		t.Fatalf("表单分发失败: %v", err)
	}

	if payload.Name != "leego" {
		t.Fatalf("表单分发结果 = %+v", payload)
	}

	plainRequest := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("name=leego"))
	plainRequest.Header.Set("Content-Type", "text/plain")
	plainContext, _ := createTestContext(plainRequest)

	if err := plainContext.Bind(&payload); !errors.Is(err, ErrUnsupportedMediaType) {
		t.Fatalf("不支持的类型 = %v, want ErrUnsupportedMediaType", err)
	}
}

// TestContextBindRejectsInvalidTargets 验证绑定目标必须为非 nil 指针。
func TestContextBindRejectsInvalidTargets(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"leego"}`))
	req.Header.Set("Content-Type", "application/json")

	ctx, _ := createTestContext(req)

	if err := ctx.BindJSON(nil); err == nil {
		t.Fatal("BindJSON(nil) 必须返回错误")
	}

	if err := ctx.BindJSON(formPayload{}); err == nil {
		t.Fatal("非指针目标必须返回错误")
	}

	if err := ctx.BindXML("not a pointer"); err == nil {
		t.Fatal("BindXML 非指针目标必须返回错误")
	}

	if err := ctx.BindForm(42); err == nil {
		t.Fatal("BindForm 非指针目标必须返回错误")
	}
}

// TestContextRenderHTMLFile 验证按文件渲染模板与函数表注入。
func TestContextRenderHTMLFile(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "page.html")

	if err := os.WriteFile(filename, []byte("<p>{{ shout .Name }}</p>"), 0o644); err != nil {
		t.Fatalf("写入模板失败: %v", err)
	}

	engine := NewEngine(DisableStartupLog())
	engine.SetTemplateFuncs(template.FuncMap{"shout": strings.ToUpper})

	ctx, recorder := createTestContext(httptest.NewRequest(http.MethodGet, "/", nil))
	ctx.engine = engine

	if err := ctx.ResponseTemplateFile(filename, "", map[string]string{"Name": "leego"}); err != nil {
		t.Fatalf("ResponseTemplateFile() error = %v", err)
	}

	if !strings.Contains(recorder.Body.String(), "<p>LEEGO</p>") {
		t.Fatalf("渲染结果 = %q", recorder.Body.String())
	}

	if got := recorder.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}

	// 指定模板名时走 ExecuteTemplate 分支。
	namedContext, namedRecorder := createTestContext(httptest.NewRequest(http.MethodGet, "/", nil))
	namedContext.engine = engine

	if err := namedContext.ResponseTemplateFile(filename, "page.html", map[string]string{"Name": "go"}); err != nil {
		t.Fatalf("ResponseTemplateFile(指定模板) error = %v", err)
	}

	if !strings.Contains(namedRecorder.Body.String(), "<p>GO</p>") {
		t.Fatalf("指定模板渲染结果 = %q", namedRecorder.Body.String())
	}

	// 文件不存在时返回错误。
	missingContext, _ := createTestContext(httptest.NewRequest(http.MethodGet, "/", nil))
	if err := missingContext.ResponseTemplateFile(filepath.Join(dir, "missing.html"), "", nil); err == nil {
		t.Fatal("模板文件不存在时必须返回错误")
	}
}

// TestContextServeFileContent 验证文件输出、附件下载与自定义文件系统。
func TestContextServeFileContent(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "a.txt")

	if err := os.WriteFile(filename, []byte("hello file"), 0o644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}

	ctx, recorder := createTestContext(httptest.NewRequest(http.MethodGet, "/", nil))
	ctx.ResponseFile(filename)

	if recorder.Body.String() != "hello file" {
		t.Fatalf("ResponseFile 响应体 = %q", recorder.Body.String())
	}

	attachmentContext, attachmentRecorder := createTestContext(httptest.NewRequest(http.MethodGet, "/", nil))
	attachmentContext.ResponseAttachment(filename)

	if got := attachmentRecorder.Header().Get("Content-Disposition"); got != `attachment; filename="a.txt"` {
		t.Fatalf("Content-Disposition = %q", got)
	}

	if attachmentRecorder.Body.String() != "hello file" {
		t.Fatalf("附件响应体 = %q", attachmentRecorder.Body.String())
	}

	// 自定义文件系统：验证内容输出与 URL 路径恢复。
	request := httptest.NewRequest(http.MethodGet, "/origin", nil)
	fsContext, fsRecorder := createTestContext(request)
	fsContext.ResponseFileFromFS("a.txt", http.FS(os.DirFS(dir)))

	if fsRecorder.Body.String() != "hello file" {
		t.Fatalf("ResponseFileFromFS 响应体 = %q", fsRecorder.Body.String())
	}

	if request.URL.Path != "/origin" {
		t.Fatalf("请求路径未恢复，当前为 %q", request.URL.Path)
	}

	attachmentFSContext, attachmentFSRecorder := createTestContext(httptest.NewRequest(http.MethodGet, "/", nil))
	attachmentFSContext.ResponseAttachmentFromFS("a.txt", http.FS(os.DirFS(dir)))

	if got := attachmentFSRecorder.Header().Get("Content-Disposition"); got != `attachment; filename="a.txt"` {
		t.Fatalf("FromFS Content-Disposition = %q", got)
	}

	if attachmentFSRecorder.Body.String() != "hello file" {
		t.Fatalf("FromFS 附件响应体 = %q", attachmentFSRecorder.Body.String())
	}
}

// TestContextStreamSSEWritesEvents 验证 SSE 建连、业务事件与关闭事件。
func TestContextStreamSSEWritesEvents(t *testing.T) {
	ctx, recorder := createTestContext(httptest.NewRequest(http.MethodGet, "/events", nil))

	events := make(chan *sse.Message, 1)
	events <- sse.NewMessageWithEvent("custom", []byte("payload"))
	close(events)

	err := ctx.ResponseSSE(func(inner context.Context) <-chan *sse.Message {
		return events
	})
	if err != nil {
		t.Fatalf("ResponseSSE() error = %v", err)
	}

	body := recorder.Body.String()
	for _, expected := range []string{"event: connect", "event: custom", "payload", "event: close"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("事件流缺少 %q: %s", expected, body)
		}
	}

	if got := recorder.Header().Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("Content-Type = %q", got)
	}
}

// TestContextSmallAccessors 验证小工具方法与错误链行为。
func TestContextSmallAccessors(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.0.2.9"

	ctx, _ := createTestContext(req)

	if got := ctx.GetRemoteIP(); got != "192.0.2.9" {
		t.Fatalf("无端口时 GetRemoteIP() = %q, want 192.0.2.9", got)
	}

	if got := ctx.GetServiceName(); got != "" {
		t.Fatalf("未绑定引擎时 GetServiceName() = %q, want 空", got)
	}

	engine := NewEngine(WithEngineName("orders"), DisableStartupLog())
	ctx.engine = engine

	if got := ctx.GetServiceName(); got != "orders" {
		t.Fatalf("GetServiceName() = %q, want orders", got)
	}

	if value, isFound := ctx.GetContextValue("missing"); isFound || value != nil {
		t.Fatalf("GetContextValue(缺失) = (%v, %v)", value, isFound)
	}

	keyed := req.WithContext(context.WithValue(req.Context(), "trace", "abc"))
	keyedContext, _ := createTestContext(keyed)

	if value, isFound := keyedContext.GetContextValue("trace"); !isFound || value != "abc" {
		t.Fatalf("GetContextValue(trace) = (%v, %v)", value, isFound)
	}

	if err := ctx.GetLastError(); err != nil {
		t.Fatalf("空错误链 GetLastError() = %v, want nil", err)
	}

	ctx.AddError(errors.New("boom"))
	if got := ctx.GetLastError(); got == nil || got.Error() != "boom" {
		t.Fatalf("GetLastError() = %v, want boom", got)
	}

	if got := ctx.GetPathValue("id"); got != "" {
		t.Fatalf("无路由参数时 GetPathValue() = %q, want 空", got)
	}

	base := errors.New("base")
	httpErr := NewHTTPError(http.StatusBadRequest, "bad_request", "bad", base)

	if got := errors.Unwrap(httpErr); got != base {
		t.Fatalf("Unwrap() = %v, want base", got)
	}

	if got := httpErr.Error(); !strings.Contains(got, "bad") || !strings.Contains(got, "base") {
		t.Fatalf("HTTPError.Error() = %q", got)
	}

	if got := NewHTTPError(http.StatusBadRequest, "", "bad", nil).Error(); got != "bad" {
		t.Fatalf("无底层错误时 Error() = %q, want bad", got)
	}
}

// TestContextCookieDetails 验证 Cookie 的默认值与编码细节。
func TestContextCookieDetails(t *testing.T) {
	ctx, recorder := createTestContext(httptest.NewRequest(http.MethodGet, "/", nil))

	// nil Cookie 必须被忽略。
	ctx.SetCookie(nil)
	if got := recorder.Header().Get("Set-Cookie"); got != "" {
		t.Fatalf("nil Cookie 不应写入响应头，实际为 %q", got)
	}

	ctx.SetSameSite(http.SameSiteLaxMode)
	ctx.SetCookieData("token", "a b&c", 60, "", "example.com", true, true)

	header := recorder.Header().Get("Set-Cookie")
	for _, expected := range []string{"token=a%20b&c", "Path=/", "Domain=example.com", "Max-Age=60", "Secure", "HttpOnly", "SameSite=Lax"} {
		if !strings.Contains(header, expected) {
			t.Fatalf("Cookie 缺少 %q: %s", expected, header)
		}
	}
}

// TestGetCookieValueKeepsPlus 验证 Cookie 解码保留字面量加号。
//
// url.QueryUnescape 会把 “+” 解成空格，导致含加号的令牌被静默篡改。
func TestGetCookieValueKeepsPlus(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: "token", Value: "a+b%2Bc"})
	ctx, _ := createTestContext(request)

	value, err := ctx.GetCookieValue("token")
	if err != nil {
		t.Fatalf("读取 Cookie 失败: %v", err)
	}

	if value != "a+b+c" {
		t.Fatalf("GetCookieValue() = %q, want a+b+c", value)
	}
}

// TestCookieValueRoundTrip 验证 SetCookieData 与 GetCookieValue 编解码对称。
func TestCookieValueRoundTrip(t *testing.T) {
	values := []string{"a b&c", "a+b%2Bc", "中文 空格", "semi;comma,quote\""}
	for _, value := range values {
		ctx, recorder := createTestContext(httptest.NewRequest(http.MethodGet, "/", nil))
		ctx.SetCookieData("token", value, 60, "/", "", false, true)

		cookies := recorder.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatalf("Set-Cookie 数量 = %d, want 1", len(cookies))
		}

		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.AddCookie(cookies[0])
		readCtx, _ := createTestContext(request)

		got, err := readCtx.GetCookieValue("token")
		if err != nil {
			t.Fatalf("读取 Cookie 失败: %v", err)
		}

		if got != value {
			t.Fatalf("往返后 Cookie 值 = %q, want %q", got, value)
		}
	}
}

// TestReleaseBodyCacheDropsLargeBuffers 验证大请求体不会长期驻留在对象池。
func TestReleaseBodyCacheDropsLargeBuffers(t *testing.T) {
	ctx, _ := createTestContext(httptest.NewRequest(http.MethodPost, "/", nil))

	ctx.bodyCache = make([]byte, 1, _BODY_CACHE_LIMIT+1)
	ctx.clear()
	if ctx.bodyCache != nil {
		t.Fatalf("超过上限的缓存应被丢弃，cap = %d", cap(ctx.bodyCache))
	}

	ctx.bodyCache = make([]byte, 0, _BODY_CACHE_LIMIT)
	ctx.releaseBodyCache()
	if cap(ctx.bodyCache) != _BODY_CACHE_LIMIT {
		t.Fatalf("未超上限的缓存应保留容量，cap = %d", cap(ctx.bodyCache))
	}
}

// TestContextFormFallbacks 验证表单回退与 multipart 内存上限。
func TestContextFormFallbacks(t *testing.T) {
	ctx, _ := createTestContext(httptest.NewRequest(http.MethodGet, "/?name=leego", nil))

	if got := ctx.GetFormValue("name"); got != "leego" {
		t.Fatalf("仅查询参数时 GetFormValue() = %q, want leego", got)
	}

	if _, _, err := ctx.GetFormFile("missing"); err == nil {
		t.Fatal("缺失文件必须返回错误")
	}

	if got := ctx.getMultipartMemory(); got != _DEFAULT_MULTIPART_MEMORY {
		t.Fatalf("未绑定引擎时内存上限 = %d, want %d", got, _DEFAULT_MULTIPART_MEMORY)
	}

	ctx.engine = NewEngine(WithMultipartMemory(2<<20), DisableStartupLog())
	if got := ctx.getMultipartMemory(); got != 2<<20 {
		t.Fatalf("绑定引擎后内存上限 = %d, want %d", got, int64(2<<20))
	}
}
