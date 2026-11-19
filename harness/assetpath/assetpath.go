// Package assetpath 定位 harness/testdata 目录，供示例服务与压测基准服务复用。
//
// 示例通常在仓库根目录通过 `go run ./harness/examples/xxx` 启动，
// 此时工作目录下的 "harness/testdata" 直接命中；若从其它目录启动，
// 则退回按源码位置推断的路径，保证 go run 场景始终能找到演示数据。
package assetpath

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// _FALLBACK_DIR 为所有候选都不可用时的兜底相对路径。
const _FALLBACK_DIR = "harness/testdata"

// _DEFAULT_FILE 为相对路径越界或为空时回落的演示文件。
const _DEFAULT_FILE = "data.txt"

// GetDir 返回演示数据目录。
//
// 查找顺序：源码位置推断 → "harness/testdata" → "../testdata" → "testdata"；
// 全部不可用时返回兜底路径，由调用方在读取文件时报错。
func GetDir() string {
	if _, file, _, ok := runtime.Caller(0); ok {
		guessed := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "testdata"))
		if isDir(guessed) {
			return guessed
		}
	}

	for _, candidate := range []string{_FALLBACK_DIR, filepath.Join("..", "testdata"), "testdata"} {
		if isDir(candidate) {
			return candidate
		}
	}

	return _FALLBACK_DIR
}

// Resolve 把通配符或相对路径映射到演示数据目录内。
//
// 空路径与越界路径（含 ".."）会回落到 data.txt，
// 避免示例把整个仓库暴露给文件下载接口。
func Resolve(relative string) string {
	root := GetDir()
	target := filepath.Join(root, strings.TrimPrefix(strings.TrimSpace(relative), "/"))

	inner, err := filepath.Rel(root, target)
	if err != nil || inner == "." || inner == ".." || strings.HasPrefix(inner, ".."+string(filepath.Separator)) {
		return filepath.Join(root, _DEFAULT_FILE)
	}

	return target
}

// isDir 判断路径是否存在且为目录。
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
