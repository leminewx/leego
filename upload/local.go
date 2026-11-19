package upload

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

const (
	// _ID_BYTES 为上传会话 ID 的随机字节数；hex 编码后为 32 个字符。
	_ID_BYTES = 16
	// _DIR_MODE 为目录权限。
	_DIR_MODE = 0o700
	// _FILE_MODE 为文件权限。
	_FILE_MODE = 0o600
	// _SESSIONS_DIR 为会话元信息目录。
	_SESSIONS_DIR = ".sessions"
	// _PARTS_DIR 为分片目录。
	_PARTS_DIR = ".parts"
	// _FILES_DIR 为最终文件目录。
	_FILES_DIR = "files"
	// _META_DIR 为索引目录。
	_META_DIR = ".meta"
	// _INDEX_FILE 为秒传索引的追加日志文件名。
	_INDEX_FILE = "index.jsonl"
	// _MAX_PARTS 为单个文件的切片数量上限，避免一次分配过大位图。
	_MAX_PARTS = 100000
	// _MAX_EXTENSION 为保留的文件扩展名最大长度。
	_MAX_EXTENSION = 16
	// _TEMP_SUFFIX 为临时文件后缀。
	_TEMP_SUFFIX = ".tmp"
)

// ErrInvalidRoot 表示存储根目录非法。
var ErrInvalidRoot = errors.New("upload: storage root must not be empty")

// LocalStorage 把分片与最终文件落在本地目录。
//
// 目录布局：
//
//	<root>/files/            拼接后的文件
//	<root>/.parts/<id>/      分片临时文件
//	<root>/.sessions/<id>.json 上传会话元信息
//	<root>/.meta/index.jsonl  SHA-256 → 相对路径，用于秒传（追加写）
type LocalStorage struct {
	// root 为存储根目录。
	root string
	// sessionsDir 为会话元信息目录。
	sessionsDir string
	// partsDir 为分片目录。
	partsDir string
	// filesDir 为最终文件目录。
	filesDir string
	// indexPath 为秒传索引文件路径。
	indexPath string
	// mu 保护 index 与 loaded。
	mu sync.Mutex
	// index 为内存索引。
	index map[string]string
	// loaded 标记索引是否已从磁盘加载。
	loaded bool
	// locks 保存每个会话的互斥锁，串行化同一会话的写操作。
	locks sync.Map
	// locksMu 保护会话锁的引用计数与回收，避免条目随会话数无限增长。
	locksMu sync.Mutex
}

// NewLocalStorage 创建本地分片存储，并按需创建目录。
func NewLocalStorage(root string) (*LocalStorage, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, ErrInvalidRoot
	}

	own := &LocalStorage{
		root:        filepath.Clean(root),
		sessionsDir: filepath.Join(root, _SESSIONS_DIR),
		partsDir:    filepath.Join(root, _PARTS_DIR),
		filesDir:    filepath.Join(root, _FILES_DIR),
		indexPath:   filepath.Join(root, _META_DIR, _INDEX_FILE),
		index:       make(map[string]string),
	}

	for _, directory := range []string{own.sessionsDir, own.partsDir, own.filesDir, filepath.Dir(own.indexPath)} {
		if err := os.MkdirAll(directory, _DIR_MODE); err != nil {
			return nil, err
		}
	}

	return own, nil
}

// GetRoot 返回存储根目录的绝对路径。
func (own *LocalStorage) GetRoot() string {
	absolute, err := filepath.Abs(own.root)
	if err != nil {
		return own.root
	}

	return absolute
}

// Init 创建上传会话。
func (own *LocalStorage) Init(ctx context.Context, request InitRequest) (Session, error) {
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}

	if request.Size <= 0 || request.PartSize <= 0 {
		return Session{}, ErrInvalidSession
	}

	count := request.Size / request.PartSize
	if request.Size%request.PartSize != 0 {
		count++
	}

	if count <= 0 || count > _MAX_PARTS {
		return Session{}, ErrInvalidSession
	}

	id, err := newSessionID()
	if err != nil {
		return Session{}, err
	}

	session := Session{
		ID:       id,
		Name:     sanitizeName(request.Name),
		Size:     request.Size,
		PartSize: request.PartSize,
		Parts:    make([]bool, int(count)),
	}

	if err = own.saveSession(session); err != nil {
		return Session{}, err
	}

	return session, nil
}

// GetStatus 返回上传会话状态。
func (own *LocalStorage) GetStatus(ctx context.Context, id string) (Session, error) {
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}

	return own.readSession(id)
}

// PutPart 写入一个分片。
//
// 写入先落到临时文件，校验长度通过后再原子重命名，
// 因此中断的上传不会留下半截分片。
func (own *LocalStorage) PutPart(ctx context.Context, id string, index int, reader io.Reader, size int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if reader == nil {
		return ErrInvalidSession
	}

	unlock := own.lockSession(id)
	session, err := own.readSession(id)
	if err != nil {
		unlock()
		return err
	}

	if index < 0 || index >= len(session.Parts) {
		unlock()
		return ErrPartIndex
	}

	expected := expectedPartSize(session, index)
	if size >= 0 && size != expected {
		unlock()
		return ErrPartSize
	}

	directory := filepath.Join(own.partsDir, id)
	if err = os.MkdirAll(directory, _DIR_MODE); err != nil {
		unlock()
		return err
	}

	temp, err := os.CreateTemp(directory, "part-*"+_TEMP_SUFFIX)
	if err != nil {
		unlock()
		return err
	}

	written, copyErr := io.Copy(temp, io.LimitReader(reader, expected+1))
	if copyErr == nil {
		copyErr = temp.Sync()
	}

	if closeErr := temp.Close(); closeErr != nil && copyErr == nil {
		copyErr = closeErr
	}

	if copyErr != nil {
		_ = os.Remove(temp.Name())
		unlock()
		return copyErr
	}

	if written != expected {
		_ = os.Remove(temp.Name())
		unlock()
		return ErrPartSize
	}

	if err = os.Rename(temp.Name(), own.partPath(id, index)); err != nil {
		_ = os.Remove(temp.Name())
		unlock()
		return err
	}

	session.Parts[index] = true
	saveErr := own.saveSession(session)
	unlock()

	return saveErr
}

// Complete 按序拼接全部分片，返回相对存储根目录的文件路径。
//
// 拼接结果按 SHA-256 去重：内容相同的文件只保留一份。
func (own *LocalStorage) Complete(ctx context.Context, id string, parts []Part) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	unlock := own.lockSession(id)
	session, err := own.readSession(id)
	if err != nil {
		unlock()
		return "", err
	}

	for index := range session.Parts {
		if !session.Parts[index] {
			unlock()
			return "", ErrIncomplete
		}

		info, statErr := os.Stat(own.partPath(id, index))
		if statErr != nil {
			unlock()
			return "", ErrIncomplete
		}

		if info.Size() != expectedPartSize(session, index) {
			unlock()
			return "", ErrPartSize
		}
	}

	for _, part := range parts {
		if part.Size <= 0 {
			continue
		}

		if part.Index < 0 || part.Index >= len(session.Parts) {
			unlock()
			return "", ErrPartIndex
		}

		if part.Size != expectedPartSize(session, part.Index) {
			unlock()
			return "", ErrPartSize
		}
	}

	temp, err := os.CreateTemp(own.filesDir, "merge-*"+_TEMP_SUFFIX)
	if err != nil {
		unlock()
		return "", err
	}

	hasher := sha256.New()
	writer := io.MultiWriter(temp, hasher)
	var total int64

	for index := range session.Parts {
		file, openErr := os.Open(own.partPath(id, index))
		if openErr != nil {
			own.discardTemp(temp)
			unlock()
			return "", openErr
		}

		copied, copyErr := io.Copy(writer, file)
		closeErr := file.Close()
		if copyErr != nil {
			own.discardTemp(temp)
			unlock()
			return "", copyErr
		}

		if closeErr != nil {
			own.discardTemp(temp)
			unlock()
			return "", closeErr
		}

		total += copied
	}

	if syncErr := temp.Sync(); syncErr != nil {
		own.discardTemp(temp)
		unlock()
		return "", syncErr
	}

	if closeErr := temp.Close(); closeErr != nil {
		_ = os.Remove(temp.Name())
		unlock()
		return "", closeErr
	}

	if total != session.Size {
		_ = os.Remove(temp.Name())
		unlock()
		return "", ErrMergedSize
	}

	sum := hex.EncodeToString(hasher.Sum(nil))

	// 秒传：内容已存在时直接复用，避免重复写入。
	if existing, ok := own.lookupIndex(sum); ok {
		if _, statErr := os.Stat(filepath.Join(own.root, filepath.FromSlash(existing))); statErr == nil {
			_ = os.Remove(temp.Name())
			own.removeSession(session.ID)
			unlock()
			return existing, nil
		}
	}

	relative := finalPath(sum, session.Name)
	if err = os.Rename(temp.Name(), filepath.Join(own.root, filepath.FromSlash(relative))); err != nil {
		_ = os.Remove(temp.Name())
		unlock()
		return "", err
	}

	if err = own.rememberIndex(sum, relative); err != nil {
		unlock()
		return "", err
	}

	own.removeSession(session.ID)
	unlock()

	return relative, nil
}

// Abort 丢弃全部分片与会话，可重复调用。
func (own *LocalStorage) Abort(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	unlock := own.lockSession(id)
	own.removeSession(id)
	unlock()

	return nil
}

// readSession 读取会话元信息。
func (own *LocalStorage) readSession(id string) (Session, error) {
	if !isSafeID(id) {
		return Session{}, ErrSessionNotFound
	}

	data, err := os.ReadFile(filepath.Join(own.sessionsDir, id+".json"))
	if errors.Is(err, fs.ErrNotExist) {
		return Session{}, ErrSessionNotFound
	}

	if err != nil {
		return Session{}, err
	}

	var session Session
	if err = json.Unmarshal(data, &session); err != nil {
		return Session{}, err
	}

	if session.ID != id || session.Size <= 0 || session.PartSize <= 0 || len(session.Parts) == 0 {
		return Session{}, ErrInvalidSession
	}

	return session, nil
}

// saveSession 原子写入会话元信息。
func (own *LocalStorage) saveSession(session Session) error {
	return writeJSON(filepath.Join(own.sessionsDir, session.ID+".json"), session)
}

// removeSession 清理分片目录与会话文件。
func (own *LocalStorage) removeSession(id string) {
	if !isSafeID(id) {
		return
	}

	_ = os.RemoveAll(filepath.Join(own.partsDir, id))
	_ = os.Remove(filepath.Join(own.sessionsDir, id+".json"))
}

// partPath 返回分片文件路径。
func (own *LocalStorage) partPath(id string, index int) string {
	return filepath.Join(own.partsDir, id, strconv.Itoa(index)+".part")
}

// lookupIndex 查询秒传索引。
func (own *LocalStorage) lookupIndex(sum string) (string, bool) {
	own.mu.Lock()
	defer own.mu.Unlock()

	if err := own.loadIndexLocked(); err != nil {
		return "", false
	}

	path, ok := own.index[sum]
	return path, ok
}

// indexEntry 是秒传索引的一行记录。
type indexEntry struct {
	// Sum 为文件内容的 SHA-256（小写十六进制）。
	Sum string `json:"sum"`
	// Path 为相对存储根目录的文件路径。
	Path string `json:"path"`
}

// rememberIndex 追加写入秒传索引。
//
// 采用逐行追加而不是整表重写：文件数增长后，每次全量 Marshal
// 会让单次上传的耗时随历史文件数线性变差。
func (own *LocalStorage) rememberIndex(sum, relative string) error {
	own.mu.Lock()
	defer own.mu.Unlock()

	if err := own.loadIndexLocked(); err != nil {
		return err
	}

	if _, exists := own.index[sum]; exists {
		return nil
	}

	data, err := json.Marshal(indexEntry{Sum: sum, Path: relative})
	if err != nil {
		return err
	}

	file, err := os.OpenFile(own.indexPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, _FILE_MODE)
	if err != nil {
		return err
	}

	if _, err = file.Write(append(data, '\n')); err == nil {
		err = file.Sync()
	}

	if closeErr := file.Close(); closeErr != nil && err == nil {
		err = closeErr
	}

	if err != nil {
		return err
	}

	own.index[sum] = relative
	return nil
}

// loadIndexLocked 从磁盘加载索引；调用方必须已持有锁。
//
// 文件是逐行追加的 JSON，损坏行直接跳过，不影响其余记录。
func (own *LocalStorage) loadIndexLocked() error {
	if own.loaded {
		return nil
	}

	data, err := os.ReadFile(own.indexPath)
	if errors.Is(err, fs.ErrNotExist) {
		own.loaded = true
		return nil
	}

	if err != nil {
		return err
	}

	index := make(map[string]string)
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		var entry indexEntry
		if json.Unmarshal(line, &entry) != nil || entry.Sum == "" || entry.Path == "" {
			continue
		}

		index[entry.Sum] = entry.Path
	}

	own.index = index
	own.loaded = true
	return nil
}

// discardTemp 关闭并删除临时文件。
func (own *LocalStorage) discardTemp(temp *os.File) {
	_ = temp.Close()
	_ = os.Remove(temp.Name())
}

// sessionLock 是带引用计数的会话锁。
//
// 只在没有任何持锁者/等待者时才从 locks 中删除，
// 这样既不会随上传会话数无限增长，也不会让新旧锁同时存在而失去互斥。
type sessionLock struct {
	// mu 为真正的互斥锁。
	mu sync.Mutex
	// refs 为当前持锁与等锁的调用方数量，由 locksMu 保护。
	refs int
}

// lockSession 返回会话锁的解锁函数。
func (own *LocalStorage) lockSession(id string) func() {
	own.locksMu.Lock()
	value, _ := own.locks.LoadOrStore(id, &sessionLock{})
	lock := value.(*sessionLock)
	lock.refs++
	own.locksMu.Unlock()

	lock.mu.Lock()

	return func() {
		lock.mu.Unlock()

		own.locksMu.Lock()
		lock.refs--
		if lock.refs <= 0 {
			own.locks.Delete(id)
		}
		own.locksMu.Unlock()
	}
}

// expectedPartSize 返回指定分片的预期字节数。
func expectedPartSize(session Session, index int) int64 {
	if index == len(session.Parts)-1 {
		return session.Size - int64(index)*session.PartSize
	}

	return session.PartSize
}

// finalPath 生成最终文件的相对路径。
func finalPath(sum, name string) string {
	extension := filepath.Ext(name)
	if len(extension) > _MAX_EXTENSION || strings.ContainsAny(extension, `/\`) {
		extension = ""
	}

	return _FILES_DIR + "/" + sum + extension
}

// sanitizeName 去掉文件名中的目录成分，防止路径穿越。
func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}

	name = filepath.Base(name)
	if name == "." || name == string(filepath.Separator) {
		return ""
	}

	return name
}

// isSafeID 校验会话 ID 是否为本包生成的 32 位小写十六进制串。
func isSafeID(id string) bool {
	if len(id) != _ID_BYTES*2 {
		return false
	}

	for index := 0; index < len(id); index++ {
		character := id[index]
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}

	return true
}

// newSessionID 生成随机会话 ID。
func newSessionID() (string, error) {
	buffer := make([]byte, _ID_BYTES)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}

	return hex.EncodeToString(buffer), nil
}

// writeJSON 以"临时文件 + 重命名"方式原子写入 JSON。
func writeJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}

	temp, err := os.CreateTemp(filepath.Dir(path), "*.json"+_TEMP_SUFFIX)
	if err != nil {
		return err
	}

	if err = temp.Chmod(_FILE_MODE); err != nil {
		_ = temp.Close()
		_ = os.Remove(temp.Name())
		return err
	}

	if _, err = temp.Write(data); err != nil {
		_ = temp.Close()
		_ = os.Remove(temp.Name())
		return err
	}

	if err = temp.Sync(); err != nil {
		_ = temp.Close()
		_ = os.Remove(temp.Name())
		return err
	}

	if err = temp.Close(); err != nil {
		_ = os.Remove(temp.Name())
		return err
	}

	if err = os.Rename(temp.Name(), path); err != nil {
		_ = os.Remove(temp.Name())
		return err
	}

	return nil
}
