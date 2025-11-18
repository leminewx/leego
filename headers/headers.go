package headers

import (
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// GetContentType 获取内容的类型
func GetContentType(header http.Header) string {
	if header == nil {
		return ""
	}

	content := header.Get(CONTENT_TYPE)
	for i, char := range content {
		if char == ';' || char == ' ' {
			return content[:i]
		}
	}

	return content
}

// DetectContentType 检测文件内容类型
func DetectContentType(file *os.File) string {
	// 优先使用文件扩展名检测
	if ext := strings.ToLower(filepath.Ext(file.Name())); ext != "" {
		if mimeType := mime.TypeByExtension(ext); mimeType != "" {
			return mimeType
		}
	}

	// 系统检测
	buffer := make([]byte, 512)
	if n, err := file.Read(buffer); err == nil {
		return http.DetectContentType(buffer[:n])
	}

	// 直接返回
	return "application/octet-stream"
}
