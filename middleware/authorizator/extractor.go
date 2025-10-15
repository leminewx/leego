package authorizator

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"gitee.com/leminewx/leego/headers"
)

var (
	errAuthorizationHeaderRequired = errors.New("leego: jwt: authorization header is required")
	errAuthorizationHeaderFormat   = errors.New("leego: jwt: authorization header format must be 'Bearer {token}'")
	errTokenQueryParameterRequired = errors.New("leego: jwt: token query parameter is required")
	errTokenNotFoundInAnyExtrator  = errors.New("leego: jwt: not found token in any extractor")
)

// TokenExtractor 定义 JWT 令牌提取器的接口类型
type TokenExtractor interface {
	// Extract 从请求中提取令牌
	Extract(req *http.Request) (string, error)
}

/************************************************* Bearer Extractor ***************************************************/

// bearerExtractor 定义从 Authorization 头提取 Bearer 令牌的提取器数据结构
type bearerExtractor struct{}

// NewBearerExtractor 创建一个 Bearer 令牌提取器
func NewBearerExtractor() TokenExtractor {
	return &bearerExtractor{}
}

const _BEARER = "bearer"

func (own *bearerExtractor) Extract(req *http.Request) (string, error) {
	// 从 Authorization 头中提取令牌
	auth := req.Header.Get(headers.AUTHORIZATION)
	if auth == "" {
		return "", errAuthorizationHeaderRequired
	}

	// 检查令牌格式
	parts := strings.Split(auth, " ")
	if len(parts) != 2 || strings.ToLower(parts[0]) != _BEARER {
		return "", errAuthorizationHeaderFormat
	}

	return parts[1], nil
}

/************************************************** Query Extractor ***************************************************/

// queryExtractor 定义从 URL 查询参数提取令牌的提取器数据结构
type queryExtractor struct {
	name string // 参数名，默认为 "token"
}

// NewQueryExtractor 创建一个从 URL 查询参数提取令牌的提取器
func NewQueryExtractor(name string) TokenExtractor {
	return &queryExtractor{name: name}
}

const _NAME = "token"

func (own *queryExtractor) Extract(req *http.Request) (string, error) {
	// 检查令牌参数名
	name := own.name
	if name == "" {
		name = _NAME
	}

	// 从 URL 查询参数中提取令牌
	token := req.URL.Query().Get(name)
	if token == "" {
		return "", errTokenQueryParameterRequired
	}

	return token, nil
}

/************************************************* Cookie Extractor ***************************************************/

// cookieExtractor 定义从 Cookie 提取令牌
type cookieExtractor struct {
	name string // Cookie 名，默认为 "token"
}

// NewCookieExtractor 创建一个从 Cookie 提取令牌的提取器
func NewCookieExtractor(name string) TokenExtractor {
	return &cookieExtractor{name: name}
}

func (own *cookieExtractor) Extract(req *http.Request) (string, error) {
	// 检查令牌参数名
	name := own.name
	if name == "" {
		name = _NAME
	}

	// 从 URL 查询参数中提取令牌
	cookie, err := req.Cookie(name)
	if err != nil {
		return "", fmt.Errorf("leego: jwt: token cookie is required: %v", err)
	}

	return cookie.Value, nil
}

/************************************************** Multi Extractor ***************************************************/

// multiExtractor 组合多个提取器
type multiExtractor struct {
	Extractors []TokenExtractor
}

// NewMultiExtractor 创建一个组合多个提取器的提取器
func NewMultiExtractor(extractors ...TokenExtractor) TokenExtractor {
	return &multiExtractor{Extractors: extractors}
}

func (own *multiExtractor) Extract(req *http.Request) (string, error) {
	// 遍历所有提取器
	for _, extractor := range own.Extractors {
		if extractor == nil {
			continue
		}

		// 尝试从当前提取器中提取令牌
		if token, err := extractor.Extract(req); err == nil && token != "" {
			return token, nil
		}
	}

	return "", errTokenNotFoundInAnyExtrator
}
