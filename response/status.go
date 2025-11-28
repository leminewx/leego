package response

import (
	"net/http"
	"strconv"
	"strings"
)

// GetStatusText 获取状态码的文本描述
func GetStatusText(code int) string {
	if text := http.StatusText(code); text != "" {
		return strconv.Itoa(code) + " " + text
	}

	return ""
}

// GetStatusLowerText 获取状态码的小写文本描述
func GetStatusLowerText(code int) string {
	if text := http.StatusText(code); text != "" {
		return strconv.Itoa(code) + " " + strings.ToLower(text)
	}

	return ""
}
