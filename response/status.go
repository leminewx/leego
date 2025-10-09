package response

const (
	/****************************************************** 4XX ******************************************************/

	STATUS_400_BAD_REQUEST        = "400 Bad Request"        // 请求错误
	STATUS_401_UNAUTHORIZED       = "401 Unauthorized"       // 请求未认证
	STATUS_403_FORBIDDEN          = "403 Forbidden"          // 禁止请求（通常权限不足或黑名单）
	STATUS_404_NOT_FOUND          = "404 Not Found"          // 未找到请求资源
	STATUS_405_METHOD_NOT_ALLOWED = "405 Method Not Allowed" // 请求方法不被允许
	STATUS_423_LOCKED             = "423 Locked"             // 表示目标资源被锁定，无法访问或修改
	STATUS_429_TOO_MANY_REQUESTS  = "429 Too Many Requests"  // 请求太频繁

	/****************************************************** 5XX ******************************************************/

	STATUS_500_INTERNAL_SERVER_ERROR = "500 Internal Server Error" // 服务内部错误
	STATUS_502_BAD_GATEWAY           = "502 Bad Gateway"           // 网关或代理的上游服务异常
	STATUS_503_UNAVAILABLE           = "503 Service Unavailable"   // 服务不可用（正在进行维护或过载）
)
