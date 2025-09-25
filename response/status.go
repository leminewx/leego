package response

const (
	/****************************************************** 4XX ******************************************************/

	STATUS_BAD_REQUEST        = "400 Bad Request"        // 请求错误
	STATUS_UNAUTHORIZED       = "401 Unauthorized"       // 请求未认证
	STATUS_FORBIDDEN          = "403 Forbidden"          // 禁止请求（通常权限不足）
	STATUS_NOT_FOUND          = "404 Not Found"          // 未找到请求资源
	STATUS_METHOD_NOT_ALLOWED = "405 Method Not Allowed" // 请求方法不被允许
	STATUS_TOO_MANY_REQUESTS  = "429 Too Many Requests"  // 请求太频繁

	/****************************************************** 5XX ******************************************************/

	STATUS_INTERNAL_SERVER_ERROR = "500 Internal Server Error" // 服务内部错误
	STATUS_BAD_GATEWAY           = "502 Bad Gateway"           // 网关或代理的上游服务异常
	STATUS_UNAVAILABLE           = "503 Service Unavailable"   // 服务不可用（正在进行维护或过载）
)
