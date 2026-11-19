package openapi

import (
	"embed"
	"net/http"
)

// assetsFS 内嵌 UI 静态资源，保证离线可用。
//
//go:embed assets/index.html
var assetsFS embed.FS

// _UI_PAGE 为内嵌页面路径。
const _UI_PAGE = "assets/index.html"

// NewHandler 创建 openapi.json 处理器。
//
// spec 为 nil 时输出只含版本号的最小文档，便于在未接线时快速排查。
// 文档在创建处理器时一次性序列化：运行期只读，可直接复用字节，
// 避免每个请求都重新编码整份文档。
func NewHandler(spec *Spec) http.Handler {
	payload := []byte("{\"openapi\":\"" + _OPENAPI_VERSION + "\"}")
	var marshalErr error

	if spec != nil {
		data, err := spec.MarshalJSON()
		if err != nil {
			marshalErr = err
		} else {
			payload = data
		}
	}

	return http.HandlerFunc(func(resp http.ResponseWriter, _ *http.Request) {
		if marshalErr != nil {
			http.Error(resp, "openapi: failed to marshal document", http.StatusInternalServerError)
			return
		}

		resp.Header().Set("Content-Type", "application/json; charset=utf-8")
		resp.Header().Set("Cache-Control", "no-store")
		resp.WriteHeader(http.StatusOK)
		_, _ = resp.Write(payload)
	})
}

// NewUIHandler 创建文档浏览页面处理器。
//
// 页面通过相对路径推导 {prefix}/openapi.json，因此可以挂在任意子路径下；
// 静态资源全部内嵌，不依赖 CDN。页面在创建处理器时读取一次并缓存。
func NewUIHandler(spec *Spec) http.Handler {
	_ = spec

	page, readErr := assetsFS.ReadFile(_UI_PAGE)

	return http.HandlerFunc(func(resp http.ResponseWriter, _ *http.Request) {
		if readErr != nil {
			http.Error(resp, "openapi: ui asset unavailable", http.StatusInternalServerError)
			return
		}

		resp.Header().Set("Content-Type", "text/html; charset=utf-8")
		resp.Header().Set("Cache-Control", "no-store")
		resp.WriteHeader(http.StatusOK)
		_, _ = resp.Write(page)
	})
}
