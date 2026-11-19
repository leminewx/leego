package upload

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gitee.com/leminewx/leego"
)

// newUploadEngine 构造挂载了上传处理器的引擎。
func newUploadEngine(t *testing.T, options ...Option) (*leego.Engine, *LocalStorage, string) {
	t.Helper()

	root := t.TempDir()
	storage, err := NewLocalStorage(root)
	if err != nil {
		t.Fatalf("NewLocalStorage() error = %v", err)
	}

	handler := NewHandler(storage, options...)
	engine := leego.NewEngine(leego.DisableStartupLog())
	engine.POST("/upload", handler, "upload")
	engine.PUT("/upload", handler, "upload-part")
	engine.GET("/upload", handler, "upload-status")
	engine.DELETE("/upload", handler, "upload-abort")

	return engine, storage, root
}

// doJSON 发送 JSON 请求。
func doJSON(t *testing.T, engine *leego.Engine, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()

	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	return doRaw(engine, method, target, payload)
}

// doRaw 发送原始字节请求。
func doRaw(engine *leego.Engine, method, target string, body []byte) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, bytes.NewReader(body))
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	return recorder
}

// initSession 创建上传会话并解码返回值。
func initSession(t *testing.T, engine *leego.Engine, name string, size int64, partSize int64) Session {
	t.Helper()

	recorder := doJSON(t, engine, http.MethodPost, "/upload?action=init", map[string]any{
		"name":      name,
		"size":      size,
		"part_size": partSize,
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("init 状态码 = %d: %s", recorder.Code, recorder.Body.String())
	}

	var session Session
	if err := json.Unmarshal(recorder.Body.Bytes(), &session); err != nil {
		t.Fatalf("解析会话失败: %v", err)
	}

	return session
}

// uploadParts 按乱序上传全部分片。
func uploadParts(t *testing.T, engine *leego.Engine, session Session, content []byte) {
	t.Helper()

	for index := len(session.Parts) - 1; index >= 0; index-- {
		start := int64(index) * session.PartSize
		end := start + expectedPartSize(session, index)

		target := "/upload?action=part&id=" + session.ID + "&index=" + itoa(index)
		recorder := doRaw(engine, http.MethodPut, target, content[start:end])
		if recorder.Code != http.StatusOK {
			t.Fatalf("分片 %d 状态码 = %d: %s", index, recorder.Code, recorder.Body.String())
		}
	}
}

// itoa 避免在测试里重复引入 strconv。
func itoa(value int) string {
	if value == 0 {
		return "0"
	}

	digits := make([]byte, 0, 4)
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}

	return string(digits)
}

func TestUploadFlow(t *testing.T) {
	engine, _, root := newUploadEngine(t)

	content := []byte("hello leego upload world")
	session := initSession(t, engine, "demo.txt", int64(len(content)), 10)
	if len(session.Parts) != 3 {
		t.Fatalf("分片数量 = %d, want 3", len(session.Parts))
	}

	uploadParts(t, engine, session, content)

	status := doRaw(engine, http.MethodGet, "/upload?action=status&id="+session.ID, nil)
	var current Session
	if err := json.Unmarshal(status.Body.Bytes(), &current); err != nil {
		t.Fatalf("解析状态失败: %v", err)
	}

	for index, done := range current.Parts {
		if !done {
			t.Fatalf("分片 %d 未标记完成", index)
		}
	}

	completed := doRaw(engine, http.MethodPost, "/upload?action=complete&id="+session.ID, nil)
	if completed.Code != http.StatusOK {
		t.Fatalf("complete 状态码 = %d: %s", completed.Code, completed.Body.String())
	}

	var result struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(completed.Body.Bytes(), &result); err != nil {
		t.Fatalf("解析结果失败: %v", err)
	}

	sum := sha256.Sum256(content)
	want := "files/" + hex.EncodeToString(sum[:]) + ".txt"
	if result.Path != want {
		t.Fatalf("文件路径 = %q, want %q", result.Path, want)
	}

	stored, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(result.Path)))
	if err != nil {
		t.Fatalf("读取合并文件失败: %v", err)
	}

	if !bytes.Equal(stored, content) {
		t.Fatalf("合并内容不一致: %q", stored)
	}

	if _, statErr := os.Stat(filepath.Join(root, _PARTS_DIR, session.ID)); !os.IsNotExist(statErr) {
		t.Fatalf("分片目录未清理: %v", statErr)
	}

	// 相同内容再次上传应命中秒传，复用同一份文件。
	again := initSession(t, engine, "copy.txt", int64(len(content)), 10)
	uploadParts(t, engine, again, content)

	replay := doRaw(engine, http.MethodPost, "/upload?action=complete&id="+again.ID, nil)
	if replay.Code != http.StatusOK {
		t.Fatalf("秒传状态码 = %d: %s", replay.Code, replay.Body.String())
	}

	var replayResult struct {
		Path string `json:"path"`
	}
	if err = json.Unmarshal(replay.Body.Bytes(), &replayResult); err != nil {
		t.Fatalf("解析秒传结果失败: %v", err)
	}

	if replayResult.Path != want {
		t.Fatalf("秒传路径 = %q, want %q", replayResult.Path, want)
	}

	entries, err := os.ReadDir(filepath.Join(root, _FILES_DIR))
	if err != nil {
		t.Fatalf("读取文件目录失败: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("文件目录条目 = %d, want 1", len(entries))
	}
}

func TestUploadRejectsInvalidRequests(t *testing.T) {
	engine, _, _ := newUploadEngine(t, UseMaxSize(16))

	if recorder := doJSON(t, engine, http.MethodPost, "/upload?action=init", map[string]any{"size": 0}); recorder.Code != http.StatusBadRequest {
		t.Fatalf("零长度状态码 = %d, want 400", recorder.Code)
	}

	if recorder := doJSON(t, engine, http.MethodPost, "/upload?action=init", map[string]any{"size": 64}); recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("超限状态码 = %d, want 413", recorder.Code)
	}

	if recorder := doJSON(t, engine, http.MethodPost, "/upload?action=init", "not-an-object"); recorder.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 状态码 = %d, want 400", recorder.Code)
	}

	if recorder := doRaw(engine, http.MethodGet, "/upload?action=unknown", nil); recorder.Code != http.StatusBadRequest {
		t.Fatalf("未知动作状态码 = %d, want 400", recorder.Code)
	}

	session := initSession(t, engine, "demo.bin", 12, 4)

	if recorder := doRaw(engine, http.MethodPut, "/upload?action=part&id="+session.ID+"&index=9", make([]byte, 4)); recorder.Code != http.StatusBadRequest {
		t.Fatalf("越界分片状态码 = %d, want 400", recorder.Code)
	}

	if recorder := doRaw(engine, http.MethodPut, "/upload?action=part&id="+session.ID+"&index=0", make([]byte, 3)); recorder.Code != http.StatusBadRequest {
		t.Fatalf("分片长度不符状态码 = %d, want 400", recorder.Code)
	}

	if recorder := doRaw(engine, http.MethodPut, "/upload?action=part&id="+session.ID+"&index=0", make([]byte, 5)); recorder.Code != http.StatusBadRequest {
		t.Fatalf("分片超长状态码 = %d, want 400", recorder.Code)
	}

	empty := initSession(t, engine, "empty.bin", 4, 4)
	if recorder := doRaw(engine, http.MethodPost, "/upload?action=complete&id="+empty.ID, nil); recorder.Code != http.StatusConflict {
		t.Fatalf("未完成状态码 = %d, want 409", recorder.Code)
	}

	if recorder := doRaw(engine, http.MethodGet, "/upload?action=status&id=../../etc/passwd", nil); recorder.Code != http.StatusNotFound {
		t.Fatalf("非法 ID 状态码 = %d, want 404", recorder.Code)
	}

	if recorder := doRaw(engine, http.MethodPut, "/upload?action=part&id=abc&index=0", []byte("x")); recorder.Code != http.StatusNotFound {
		t.Fatalf("未知会话状态码 = %d, want 404", recorder.Code)
	}
}

func TestUploadAbort(t *testing.T) {
	engine, _, root := newUploadEngine(t)

	session := initSession(t, engine, "abort.txt", 8, 4)
	if recorder := doRaw(engine, http.MethodPut, "/upload?action=part&id="+session.ID+"&index=0", []byte("lege")); recorder.Code != http.StatusOK {
		t.Fatalf("分片状态码 = %d", recorder.Code)
	}

	if recorder := doRaw(engine, http.MethodDelete, "/upload?action=abort&id="+session.ID, nil); recorder.Code != http.StatusOK {
		t.Fatalf("abort 状态码 = %d", recorder.Code)
	}

	if recorder := doRaw(engine, http.MethodGet, "/upload?action=status&id="+session.ID, nil); recorder.Code != http.StatusNotFound {
		t.Fatalf("中止后状态码 = %d, want 404", recorder.Code)
	}

	if _, statErr := os.Stat(filepath.Join(root, _PARTS_DIR, session.ID)); !os.IsNotExist(statErr) {
		t.Fatalf("分片目录未清理: %v", statErr)
	}

	// 重复中止应当幂等。
	if recorder := doRaw(engine, http.MethodDelete, "/upload?action=abort&id="+session.ID, nil); recorder.Code != http.StatusOK {
		t.Fatalf("重复中止状态码 = %d, want 200", recorder.Code)
	}
}

func TestUploadOverwritesPart(t *testing.T) {
	engine, _, root := newUploadEngine(t)

	session := initSession(t, engine, "overwrite.bin", 4, 4)
	if recorder := doRaw(engine, http.MethodPut, "/upload?action=part&id="+session.ID+"&index=0", []byte("old!")); recorder.Code != http.StatusOK {
		t.Fatalf("首次分片状态码 = %d", recorder.Code)
	}

	if recorder := doRaw(engine, http.MethodPut, "/upload?action=part&id="+session.ID+"&index=0", []byte("new!")); recorder.Code != http.StatusOK {
		t.Fatalf("二次分片状态码 = %d", recorder.Code)
	}

	completed := doRaw(engine, http.MethodPost, "/upload?action=complete&id="+session.ID, nil)
	if completed.Code != http.StatusOK {
		t.Fatalf("complete 状态码 = %d: %s", completed.Code, completed.Body.String())
	}

	var result struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(completed.Body.Bytes(), &result); err != nil {
		t.Fatalf("解析结果失败: %v", err)
	}

	stored, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(result.Path)))
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}

	if string(stored) != "new!" {
		t.Fatalf("文件内容 = %q, want new!", stored)
	}
}

func TestUploadConcurrentParts(t *testing.T) {
	_, storage, root := newUploadEngine(t)

	content := []byte(strings.Repeat("leego", 1000))
	session, err := storage.Init(t.Context(), InitRequest{Name: "race.txt", Size: int64(len(content)), PartSize: 1024})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	var group sync.WaitGroup
	errs := make(chan error, len(session.Parts))

	for index := range session.Parts {
		start := int64(index) * session.PartSize
		end := start + expectedPartSize(session, index)

		group.Add(1)
		go func(index int) {
			defer group.Done()
			reader := bytes.NewReader(content[start:end])
			if putErr := storage.PutPart(t.Context(), session.ID, index, reader, end-start); putErr != nil {
				errs <- putErr
			}
		}(index)
	}

	group.Wait()
	close(errs)

	for putErr := range errs {
		t.Fatalf("并发写入失败: %v", putErr)
	}

	path, err := storage.Complete(t.Context(), session.ID, nil)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	stored, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}

	if !bytes.Equal(stored, content) {
		t.Fatal("并发拼接结果不一致")
	}
}

func TestNewLocalStorageRejectsEmptyRoot(t *testing.T) {
	if _, err := NewLocalStorage("  "); err == nil {
		t.Fatal("空根目录必须返回错误")
	}
}

func TestHandlerPanicsOnNilStorage(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("nil 存储必须 panic")
		}
	}()

	_ = NewHandler(nil)
}
