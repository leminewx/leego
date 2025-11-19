package response

import "net/http"

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

// StatusText returns a text for the HTTP status code. It returns the empty
// string if the code is unknown.
func StatusText(code int) string {
	switch code {
	case http.StatusContinue:
		return "Continue"
	case http.StatusSwitchingProtocols:
		return "Switching Protocols"
	case http.StatusProcessing:
		return "Processing"
	case http.StatusEarlyHints:
		return "Early Hints"
	case http.StatusOK:
		return "OK"
	case http.StatusCreated:
		return "Created"
	case http.StatusAccepted:
		return "Accepted"
	case http.StatusNonAuthoritativeInfo:
		return "Non-Authoritative Information"
	case http.StatusNoContent:
		return "No Content"
	case http.StatusResetContent:
		return "Reset Content"
	case http.StatusPartialContent:
		return "Partial Content"
	case http.StatusMultiStatus:
		return "Multi-Status"
	case http.StatusAlreadyReported:
		return "Already Reported"
	case http.StatusIMUsed:
		return "IM Used"
	case http.StatusMultipleChoices:
		return "Multiple Choices"
	case http.StatusMovedPermanently:
		return "Moved Permanently"
	case http.StatusFound:
		return "Found"
	case http.StatusSeeOther:
		return "See Other"
	case http.StatusNotModified:
		return "Not Modified"
	case http.StatusUseProxy:
		return "Use Proxy"
	case http.StatusTemporaryRedirect:
		return "Temporary Redirect"
	case http.StatusPermanentRedirect:
		return "Permanent Redirect"
	case http.StatusBadRequest:
		return "Bad Request"
	case http.StatusUnauthorized:
		return "Unauthorized"
	case http.StatusPaymentRequired:
		return "Payment Required"
	case http.StatusForbidden:
		return "Forbidden"
	case http.StatusNotFound:
		return "Not Found"
	case http.StatusMethodNotAllowed:
		return "Method Not Allowed"
	case http.StatusNotAcceptable:
		return "Not Acceptable"
	case http.StatusProxyAuthRequired:
		return "Proxy Authentication Required"
	case http.StatusRequestTimeout:
		return "Request Timeout"
	case http.StatusConflict:
		return "Conflict"
	case http.StatusGone:
		return "Gone"
	case http.StatusLengthRequired:
		return "Length Required"
	case http.StatusPreconditionFailed:
		return "Precondition Failed"
	case http.StatusRequestEntityTooLarge:
		return "Request Entity Too Large"
	case http.StatusRequestURITooLong:
		return "Request URI Too Long"
	case http.StatusUnsupportedMediaType:
		return "Unsupported Media Type"
	case http.StatusRequestedRangeNotSatisfiable:
		return "Requested Range Not Satisfiable"
	case http.StatusExpectationFailed:
		return "Expectation Failed"
	case http.StatusTeapot:
		return "I'm a teapot"
	case http.StatusMisdirectedRequest:
		return "Misdirected Request"
	case http.StatusUnprocessableEntity:
		return "Unprocessable Entity"
	case http.StatusLocked:
		return "Locked"
	case http.StatusFailedDependency:
		return "Failed Dependency"
	case http.StatusTooEarly:
		return "Too Early"
	case http.StatusUpgradeRequired:
		return "Upgrade Required"
	case http.StatusPreconditionRequired:
		return "Precondition Required"
	case http.StatusTooManyRequests:
		return "Too Many Requests"
	case http.StatusRequestHeaderFieldsTooLarge:
		return "Request Header Fields Too Large"
	case http.StatusUnavailableForLegalReasons:
		return "Unavailable For Legal Reasons"
	case http.StatusInternalServerError:
		return "Internal Server Error"
	case http.StatusNotImplemented:
		return "Not Implemented"
	case http.StatusBadGateway:
		return "Bad Gateway"
	case http.StatusServiceUnavailable:
		return "Service Unavailable"
	case http.StatusGatewayTimeout:
		return "Gateway Timeout"
	case http.StatusHTTPVersionNotSupported:
		return "HTTP Version Not Supported"
	case http.StatusVariantAlsoNegotiates:
		return "Variant Also Negotiates"
	case http.StatusInsufficientStorage:
		return "Insufficient Storage"
	case http.StatusLoopDetected:
		return "Loop Detected"
	case http.StatusNotExtended:
		return "Not Extended"
	case http.StatusNetworkAuthenticationRequired:
		return "Network Authentication Required"
	default:
		return ""
	}
}
