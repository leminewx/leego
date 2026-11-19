package leego

import (
	"strings"

	"gitee.com/leminewx/leego/openapi"
)

const (
	// _DEFAULT_OPENAPI_PREFIX 为默认文档挂载前缀。
	_DEFAULT_OPENAPI_PREFIX = "/openapi"
	// _OPENAPI_JSON_PATH 为文档 JSON 的相对路径。
	_OPENAPI_JSON_PATH = "/openapi.json"
)

// routeAdapter 把 Engine.GetRoutes 的结果适配为 openapi.RouteSource。
//
// 适配层放在根包，openapi 只认自己的最小结构，二者不会互相导入。
type routeAdapter struct {
	// routes 为引擎导出的路由信息。
	routes []RouteInfo
}

// GetRoutes 转换为 openapi 路由列表。
func (own *routeAdapter) GetRoutes() []openapi.Route {
	routes := make([]openapi.Route, 0, len(own.routes))
	for _, route := range own.routes {
		routes = append(routes, openapi.Route{
			Method:  route.Method,
			Pattern: route.Pattern,
			Name:    route.Name,
			Tags:    route.Tags,
		})
	}

	return routes
}

// RegisterOpenAPI 在指定前缀下注册 OpenAPI 文档。
//
// 注册的路由：
//   - GET {prefix}              文档浏览页面（内嵌 HTML）
//   - GET {prefix}/openapi.json 文档 JSON
//
// 必须在 Prepare 之前调用：文档在注册期一次性构建，运行期只读。
func (own *Engine) RegisterOpenAPI(prefix string, options ...openapi.Option) error {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = _DEFAULT_OPENAPI_PREFIX
	}

	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}

	prefix = strings.TrimSuffix(prefix, "/")

	source := &routeAdapter{routes: own.GetRoutes()}
	spec := openapi.NewSpec(source, options...)

	document := openapi.NewHandler(spec)
	own.GET(prefix+_OPENAPI_JSON_PATH, func(ctx *Context) {
		document.ServeHTTP(ctx.Response, ctx.Request)
	}, "openapi")

	page := openapi.NewUIHandler(spec)
	own.GET(prefix, func(ctx *Context) {
		page.ServeHTTP(ctx.Response, ctx.Request)
	}, "openapi-ui")

	return nil
}
