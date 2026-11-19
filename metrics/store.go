package metrics

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gitee.com/leminewx/leego/middlewares/collector"
)

const (
	// _DEFAULT_MAX_BYTES 为默认单文件上限（64 MiB）。
	_DEFAULT_MAX_BYTES = 64 << 20
	// _DEFAULT_MAX_FILES 为默认保留的轮转文件数量。
	_DEFAULT_MAX_FILES = 5
	// _FILE_MODE 为指标文件权限；可能包含路由名等内部信息。
	_FILE_MODE = 0o600
	// _DIR_MODE 为指标目录权限。
	_DIR_MODE = 0o700
)

// ErrStoreClosed 表示存储已关闭。
var ErrStoreClosed = errors.New("metrics: store is closed")

// Store 定义指标存储。
type Store interface {
	// Append 记录一次采集快照。
	Append(snapshot collector.Snapshot) error
	// AppendPoint 记录一个已经聚合好的时间片，用于回放。
	AppendPoint(point Point) error
	// Query 按 step 聚合 [from, to] 区间内的指标。
	Query(from, to time.Time, step time.Duration) ([]Point, error)
	// Close 刷新缓冲并关闭存储。
	Close() error
}

// MemoryStore 是只存在于内存中的指标存储。
type MemoryStore struct {
	// series 为时间序列。
	series *TimeSeries
}

// NewMemoryStore 创建内存存储。
func NewMemoryStore(window time.Duration, buckets int) *MemoryStore {
	return &MemoryStore{series: NewTimeSeries(window, buckets)}
}

// Append 记录一次采集快照。
func (own *MemoryStore) Append(snapshot collector.Snapshot) error {
	own.series.Append(time.Now(), snapshot)
	return nil
}

// AppendPoint 记录一个聚合点。
func (own *MemoryStore) AppendPoint(point Point) error {
	own.series.AppendPoint(point)
	return nil
}

// Query 按 step 聚合查询结果。
func (own *MemoryStore) Query(from, to time.Time, step time.Duration) ([]Point, error) {
	return own.series.Query(from, to, step), nil
}

// Close 为空实现，保持接口一致。
func (own *MemoryStore) Close() error {
	return nil
}

// FileOptions 定义文件存储的轮转参数。
type FileOptions struct {
	// MaxBytes 为单文件上限，非正数使用默认值 64 MiB。
	MaxBytes int64
	// MaxFiles 为保留的轮转文件数量，非正数使用默认值 5。
	MaxFiles int
}

// normalize 修正非法配置。
func (own FileOptions) normalize() FileOptions {
	if own.MaxBytes <= 0 {
		own.MaxBytes = _DEFAULT_MAX_BYTES
	}

	if own.MaxFiles <= 0 {
		own.MaxFiles = _DEFAULT_MAX_FILES
	}

	return own
}

// FileStore 以 JSONL 形式把指标追加到文件。
//
// 崩溃一致性策略：
//   - 每条记录写完后立即 Flush，宁可降低吞吐也不丢最近的数据；
//   - 启动回放时，不完整的尾行或被破坏的尾行会被截断；
//   - 达到大小上限后把当前文件重命名为带时间戳的历史文件，再新建文件。
type FileStore struct {
	// mu 保护文件句柄、大小与关闭状态。
	mu sync.Mutex
	// path 为当前写入文件路径。
	path string
	// options 为轮转参数。
	options FileOptions
	// series 为内存中的时间序列。
	series *TimeSeries
	// file 为当前文件句柄。
	file *os.File
	// writer 为带缓冲的写入器。
	writer *bufio.Writer
	// size 为当前文件字节数。
	size int64
	// closed 标记是否已关闭。
	closed bool
}

// NewFileStore 创建文件存储并回放已有数据。
//
// path 为当前写入文件；父目录不存在时自动创建。
func NewFileStore(path string, window time.Duration, buckets int, options ...FileOptions) (*FileStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("metrics: file store path must not be empty")
	}

	config := FileOptions{}
	if len(options) > 0 {
		config = options[0]
	}
	config = config.normalize()

	directory := filepath.Dir(path)
	if directory != "" && directory != "." {
		if err := os.MkdirAll(directory, _DIR_MODE); err != nil {
			return nil, fmt.Errorf("metrics: create directory: %w", err)
		}
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, _FILE_MODE)
	if err != nil {
		return nil, fmt.Errorf("metrics: open file store: %w", err)
	}

	own := &FileStore{
		path:    path,
		options: config,
		series:  NewTimeSeries(window, buckets),
		file:    file,
		writer:  bufio.NewWriter(file),
	}

	if err := own.replay(); err != nil {
		_ = file.Close()
		return nil, err
	}

	return own, nil
}

// replay 回放已有记录并截断损坏的尾行。
func (own *FileStore) replay() error {
	if _, err := own.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("metrics: seek store: %w", err)
	}

	reader := bufio.NewReader(own.file)
	var offset int64

	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			if strings.HasSuffix(line, "\n") {
				trimmed := strings.TrimSpace(line)
				if trimmed != "" {
					var point Point
					if jsonErr := json.Unmarshal([]byte(trimmed), &point); jsonErr == nil {
						own.series.AppendPoint(point)
					}
				}
			} else {
				// 半行必然来自进程被强杀：截断它，避免后续追加把坏数据串起来。
				if truncateErr := own.file.Truncate(offset); truncateErr != nil {
					return fmt.Errorf("metrics: truncate broken tail: %w", truncateErr)
				}

				own.size = offset
				_, _ = own.file.Seek(0, io.SeekEnd)
				return nil
			}
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			return fmt.Errorf("metrics: read store: %w", err)
		}

		offset += int64(len(line))
	}

	info, err := own.file.Stat()
	if err != nil {
		return fmt.Errorf("metrics: stat store: %w", err)
	}

	own.size = info.Size()
	_, _ = own.file.Seek(0, io.SeekEnd)
	return nil
}

// Append 记录一次采集快照。
func (own *FileStore) Append(snapshot collector.Snapshot) error {
	return own.AppendPoint(Point{
		At:       time.Now(),
		Total:    snapshot.Total,
		Failed:   snapshot.Failed,
		AvgMicro: snapshot.GetAverageCostMicro(),
	})
}

// AppendPoint 追加一个聚合点。
func (own *FileStore) AppendPoint(point Point) error {
	payload, err := json.Marshal(point)
	if err != nil {
		return fmt.Errorf("metrics: encode point: %w", err)
	}

	payload = append(payload, '\n')

	own.mu.Lock()
	if own.closed {
		own.mu.Unlock()
		return ErrStoreClosed
	}

	// 上一次轮转重开文件失败时 writer 为空：这里重试，避免一次磁盘抖动
	// 让指标永久停写（旧实现直接置 closed=true）。
	if own.writer == nil {
		if err = own.reopenLocked(); err != nil {
			own.mu.Unlock()
			return fmt.Errorf("metrics: reopen store: %w", err)
		}
	}

	written, err := own.writer.Write(payload)
	if err != nil {
		own.mu.Unlock()
		return fmt.Errorf("metrics: write point: %w", err)
	}

	if err = own.writer.Flush(); err != nil {
		own.mu.Unlock()
		return fmt.Errorf("metrics: flush point: %w", err)
	}

	own.size += int64(written)
	own.series.AppendPoint(point)

	needRotate := own.size >= own.options.MaxBytes
	own.mu.Unlock()

	if needRotate {
		// 轮转失败不影响本次写入：数据已经落盘，下一次还会再尝试。
		_ = own.rotate()
	}

	return nil
}

// rotate 把当前文件转为历史文件并新建写入文件。
func (own *FileStore) rotate() error {
	own.mu.Lock()
	if own.closed {
		own.mu.Unlock()
		return ErrStoreClosed
	}

	if own.size < own.options.MaxBytes {
		own.mu.Unlock()
		return nil
	}

	if own.writer == nil {
		if err := own.reopenLocked(); err != nil {
			own.mu.Unlock()
			return err
		}
	}

	if err := own.writer.Flush(); err != nil {
		own.mu.Unlock()
		return err
	}

	// Windows 上必须先关闭句柄才能重命名。
	if err := own.file.Close(); err != nil {
		own.mu.Unlock()
		return err
	}

	rotated := rotateName(own.path, time.Now())
	if err := os.Rename(own.path, rotated); err != nil {
		// 重命名失败时把文件重新打开，保证后续写入仍能恢复；
		// 即使重开失败也不关闭存储，下一次写入会再试。
		own.reopenLocked()
		own.mu.Unlock()
		return err
	}

	own.size = 0
	if err := own.reopenLocked(); err != nil {
		own.mu.Unlock()
		return err
	}

	own.mu.Unlock()

	own.prune()
	return nil
}

// reopenLocked 重新打开写入文件。
//
// 失败时清空句柄并返回错误，由调用方决定是否重试；
// 调用方必须已持有 mu。
func (own *FileStore) reopenLocked() error {
	file, err := os.OpenFile(own.path, os.O_CREATE|os.O_RDWR|os.O_APPEND, _FILE_MODE)
	if err != nil {
		own.file = nil
		own.writer = nil
		return err
	}

	own.file = file
	own.writer = bufio.NewWriter(file)
	return nil
}

// prune 删除超过保留数量的历史文件。
func (own *FileStore) prune() {
	extension := filepath.Ext(own.path)
	prefix := strings.TrimSuffix(filepath.Base(own.path), extension) + "-"

	entries, err := os.ReadDir(filepath.Dir(own.path))
	if err != nil {
		return
	}

	history := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, prefix) || filepath.Ext(name) != extension {
			continue
		}

		history = append(history, name)
	}

	if len(history) <= own.options.MaxFiles {
		return
	}

	sort.Strings(history)
	for _, name := range history[:len(history)-own.options.MaxFiles] {
		_ = os.Remove(filepath.Join(filepath.Dir(own.path), name))
	}
}

// Query 按 step 聚合查询结果。
func (own *FileStore) Query(from, to time.Time, step time.Duration) ([]Point, error) {
	own.mu.Lock()
	closed := own.closed
	own.mu.Unlock()

	if closed {
		return nil, ErrStoreClosed
	}

	return own.series.Query(from, to, step), nil
}

// Close 刷新缓冲并关闭文件。
func (own *FileStore) Close() error {
	own.mu.Lock()
	if own.closed {
		own.mu.Unlock()
		return nil
	}

	own.closed = true

	var flushErr, closeErr error
	if own.writer != nil {
		flushErr = own.writer.Flush()
	}

	if own.file != nil {
		closeErr = own.file.Close()
	}

	own.mu.Unlock()

	return errors.Join(flushErr, closeErr)
}

// rotateName 生成历史文件名：metrics-<unix>.jsonl。
func rotateName(path string, at time.Time) string {
	extension := filepath.Ext(path)
	base := strings.TrimSuffix(filepath.Base(path), extension)
	return filepath.Join(filepath.Dir(path), fmt.Sprintf("%s-%d%s", base, at.Unix(), extension))
}
