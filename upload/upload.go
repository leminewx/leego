// Package upload 提供分片上传能力。
//
// 设计要点：
//   - 会话元信息（总大小、分片大小、完成位图）以 JSON 落在磁盘，
//     进程重启后仍可续传；
//   - 分片按序号独立落盘，允许乱序与重复上传，最后按序拼接；
//   - 拼接时计算 SHA-256，命中索引则直接复用已有文件（秒传），
//     省去重复写入与存储；
//   - 只依赖标准库与根包，Storage 为接口，可替换为对象存储实现。
package upload

import (
	"context"
	"errors"
	"io"
)

// Part 描述一个已完成的分片。
type Part struct {
	// Index 为分片序号，从 0 开始。
	Index int `json:"index"`
	// Size 为分片字节数。
	Size int64 `json:"size"`
	// Hash 为分片摘要，可选。
	Hash string `json:"hash,omitempty"`
}

// Session 描述一次上传任务。
type Session struct {
	// ID 为上传会话标识。
	ID string `json:"id"`
	// Name 为原始文件名，用于推导最终扩展名。
	Name string `json:"name,omitempty"`
	// Size 为文件总字节数。
	Size int64 `json:"size"`
	// PartSize 为除最后一片外的分片大小。
	PartSize int64 `json:"part_size"`
	// Parts 标记每个分片是否已经上传。
	Parts []bool `json:"parts"`
}

// InitRequest 是创建上传任务的入参。
type InitRequest struct {
	// Name 为原始文件名。
	Name string `json:"name"`
	// Size 为文件总字节数。
	Size int64 `json:"size"`
	// PartSize 为期望的分片大小，非正数时由调用方填充默认值。
	PartSize int64 `json:"part_size"`
}

// Storage 定义分片上传存储。
//
// 实现必须并发安全：同一会话的多个分片可能并行写入。
type Storage interface {
	// Init 创建上传会话。
	Init(ctx context.Context, request InitRequest) (Session, error)
	// GetStatus 返回上传会话状态。
	GetStatus(ctx context.Context, id string) (Session, error)
	// PutPart 写入一个分片；size 为声明长度，负数表示未知。
	PutPart(ctx context.Context, id string, index int, reader io.Reader, size int64) error
	// Complete 按序拼接全部分片，返回最终文件路径。
	Complete(ctx context.Context, id string, parts []Part) (string, error)
	// Abort 丢弃全部分片与会话。
	Abort(ctx context.Context, id string) error
}

// 上传过程中的错误。
var (
	// ErrInvalidSession 表示会话参数非法。
	ErrInvalidSession = errors.New("upload: invalid session")
	// ErrSessionNotFound 表示上传会话不存在。
	ErrSessionNotFound = errors.New("upload: session not found")
	// ErrPartIndex 表示分片序号越界。
	ErrPartIndex = errors.New("upload: part index out of range")
	// ErrPartSize 表示分片长度与约定不符。
	ErrPartSize = errors.New("upload: part size mismatch")
	// ErrIncomplete 表示仍有分片未上传。
	ErrIncomplete = errors.New("upload: parts are incomplete")
	// ErrMergedSize 表示拼接结果与声明总大小不一致。
	ErrMergedSize = errors.New("upload: merged size mismatch")
)
