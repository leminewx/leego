package headers

const (
	// ******************************************************************************************************************
	// * 响应头：适用于 HTTP 响应，包含响应的元数据（如状态说明、缓存规则、资源位置等）
	// ******************************************************************************************************************

	/*
		Status

		作用：
			- 明确响应的状态码和原因短语（HTTP/2 中已移除原因短语，仅保留状态码）
		示例：
			- Status: 200 OK
			- Status: 404 Not Found
	*/
	STATUS = "Status"

	/*
		Server

		作用：
		  - 标识服务器的软件名称和版本（部分服务器会隐藏具体版本以提高安全性）
		示例：
		  - Server: Nginx/1.21.0
		  - Server: Apache/2.4.18
	*/
	SERVER = "Server"

	/*
		Set-Cookie

		作用：
		  - 服务器向客户端设置 Cookie，客户端后续请求会携带该 Cookie
		取值：
		  - Expires=Wed, 21 Oct 2026 07:28:00 GMT：Cookie 的过期时间（会话 Cookie 无此属性，关闭浏览器失效）
		  - Max-Age=31536000：Cookie 的有效期（秒），优先级高于 Expires
		  - Path=/：Cookie 生效的路径（仅该路径下的请求会携带）
		  - Domain=example.com：Cookie 生效的域名（子域名可继承）
		  - HttpOnly：禁止 JavaScript 访问 Cookie（防止 XSS 攻击窃取 Cookie）
		  - Secure：仅在 HTTPS 协议下传输 Cookie
		  - SameSite=Strict/Lax：限制跨站请求携带 Cookie（防止 CSRF 攻击）
		示例：
		  - Set-Cookie: sessionId=abc123; Max-Age=86400; HttpOnly; Secure; SameSite=Lax
	*/
	SET_COOKIE = "Set-Cookie"

	/*
		Location

		作用：
		  - 用于重定向场景，指定资源的新 URL
		  - 通常配合 3xx 状态码（如 301 永久重定向、302 临时重定向、307 临时重定向）使用
		示例：
		  - Location: https://new.example.com
	*/
	LOCATION = "Location"

	/*
		Content-Encoding

		作用：
		  - 告知客户端服务器对响应数据使用的压缩编码方式（对应请求头 Accept-Encoding）
		示例：
		  - Content-Encoding: gzip
	*/
	CONTENT_ENCODING = "Content-Encoding"

	/*
		ETag

		作用：
		  - 资源的唯一标识（如基于文件内容的哈希值），用于缓存验证
		场景：
		  - 客户端后续请求可通过 If-None-Match 头携带该值，服务器对比后若资源未变更，返回 304 Not Modified（不返回资源内容，减少传输）
		示例：
		  - ETag: "5f8d72a3-1234"
	*/
	E_TAG = "ETag"

	/*
		Last-Modified

		作用：
		  - 资源最后修改的时间戳，用于缓存验证
		场景：
		  - 客户端后续请求可通过 If-Modified-Since 头携带该时间戳，服务器若判断资源未修改，返回 304 Not Modified
		示例：
		  - Last-Modified: Wed, 21 Oct 2015 07:28:00 GMT
	*/
	LAST_MODIFIED = "Last-Modified"
)
