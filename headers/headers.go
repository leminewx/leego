package headers

const (
	/****************************************************** 普通头 *****************************************************/

	AUTHORIZATION = "Authorization"

	X_REQUEST_ID = "X-Request-Id"

	/****************************************************** 跨域头 *****************************************************/

	ACCESS_CONTROL_REQUEST_METHOD = "Access-Control-Request-Method" // 实际请求方法

	ACCESS_CONTROL_REQUEST_HEADERS = "Access-Control-Request-Headers" // 实际请求携带的自定义字段

	ACCESS_CONTROL_ALLOW_CREDENTIALS = "Access-Control-Allow-Credentials"

	ACCESS_CONTROL_ALLOW_ORIGIN = "Access-Control-Allow-Origin"

	ACCESS_CONTROL_ALLOW_METHODS = "Access-Control-Allow-Methods"

	ACCESS_CONTROL_ALLOW_HEADERS = "Access-Control-Allow-Headers"

	ACCESS_CONTROL_EXPOSE_HEADERS = "Access-Control-Expose-Headers"

	ACCESS_CONTROL_MAX_AGE = "Access-Control-Max-Age"

	/****************************************************** 安全头 *****************************************************/

	// Content-Security-Policy (CSP)：内容安全策略，用于防止XSS攻击，通过指定允许加载的资源来源来限制资源加载。

	// Strict-Transport-Security (HSTS)：强制使用HTTPS，防止中间人攻击。

	// Referrer-Policy：控制Referrer头的发送，防止信息泄露。

	// Permissions-Policy（之前叫Feature-Policy）：控制浏览器哪些功能可以被使用（如摄像头、地理位置等）。

	// Expect-CT：用于处理证书透明度（Certificate Transparency）。

	// Cache-Control：控制缓存，防止敏感信息被缓存。

	// Set-Cookie属性：如Secure、HttpOnly、SameSite等属性用于增强Cookie安全。

	/*
		X-Frame-Options

		作用：
		  - 防止点击劫持（ClickJacking）攻击，控制页面是否可以被嵌入到 <frame>, <iframe>, <embed>, <object> 中。

		取值：
		  - DENY：完全禁止被嵌入（最安全）
		  - SAMEORIGIN：只允许同源网站嵌入
		  - ALLOW-FROM uri： 只允许特定域名嵌入（浏览器支持度不高）
	*/
	X_FRAME_OPTIONS = "X-Frame-Options"

	/*
		X-XSS-Protection

		作用：
		  - 启用浏览器的XSS过滤功能。
	*/
	X_XSS_PROTECTION = "X-XSS-Protection"

	/*
		X-Content-Type-Options

		作用：
			- 防止 MIME 类型嗅探攻击，强制浏览器严格遵守 Content-Type 头中声明的 MIME 类型。
	*/
	X_CONTENT_TYPE_OPTIONS = "X-Content-Type-Options"
)
