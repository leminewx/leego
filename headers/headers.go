package headers

import (
	"net/http"
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
