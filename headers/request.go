package headers

const (
	// ******************************************************************************************************************
	// * 请求头：适用于 HTTP 请求，包含请求的上下文信息（如客户端身份、期望的响应格式等）
	// ******************************************************************************************************************

	/*
		Host

		作用：
		  - 指定请求的目标服务器域名和端口号，是 HTTP/1.1 必需的请求头（无此头服务器可能返回 400 错误）
		场景：
		  - 支持虚拟主机（一台服务器托管多个域名），服务器通过该头识别目标站点
		示例：
		  - Host: www.example.com 或 Host: api.example.com:8080
	*/
	HOST = "Host"

	/*
		User-Agent（UA）

		作用：
		  - 标识发送请求的客户端（浏览器、爬虫、APP 等）的信息，包括客户端类型、版本、操作系统等
		场景：
		  - 服务器可根据 UA 适配不同的响应内容（如移动端 / PC 端页面），或限制恶意爬虫
		示例：
		  - User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/117.0.0.0 Safari/537.36
	*/
	USER_AGENT = "User-Agent"

	/*
		Accept

		作用：
		  - 告知服务器客户端可接收的资源媒体类型（MIME 类型），优先级由 q 参数指定（默认 q=1，值越高优先级越高）
		取值：
		  - text/html：优先级最高的类型之一，用于接收 HTML 网页（浏览器默认核心接收类型）
		  - text/plain：接收纯文本内容（如 txt 文件、简单字符串响应）
		  - text/css：接收 CSS 样式文件（客户端加载样式资源时使用）
		  - application/json：RESTful API 最常用的类型，接收 JSON 格式数据（前后端数据交互首选）
		  - application/x-www-form-urlencoded：接收表单默认编码的数据（通常用于非文件上传的表单提交请求）
		  - application/pdf：接收 PDF 文件
		  - application/zip：接收 ZIP 压缩文件
		  - application/vnd.ms-excel：接收 Excel 文件（.xls）
		  - application/vnd.openxmlformats-officedocument.spreadsheetml.sheet：接收 Excel 2007+ 文件（.xlsx）
		  - image/*：接收所有图片类型（通配符 * 表示匹配该类别下所有子类型），也可指定具体图片格式
		  - audio/*：接收所有音频类型（通配符 * 表示匹配该类别下所有子类型），也可指定具体图片格式
		  - video/*：视频所有视频类型（通配符 * 表示匹配该类别下所有子类型），也可指定具体图片格式
		  - ......
		示例：
		  - Accept: text/html, application/json; q=0.9, image/*; q=0.8
	*/
	ACCEPT = "Accept"

	/*
		Accept-Encoding

		作用：
		  - 指定客户端支持的内容编码方式（压缩算法），服务器可据此压缩响应数据以减少传输体积
		取值：
		  - 常用值：gzip、deflate、br（Brotli）
		示例：
		  - Accept-Encoding: gzip, deflate, br
	*/
	ACCEPT_ENCODING = "Accept-Encoding"

	/*
		Accept-Language

		作用：
		  - 告知服务器客户端偏好的自然语言（如中文、英文），服务器可返回对应语言的内容
		示例：
		  - Accept-Language: zh-CN, zh; q=0.9, en; q=0.8
	*/
	ACCEPT_LANGUAGE = "Accept-Language"

	/*
		Authorization

		作用：
		  - 传递用户身份验证信息，用于访问受保护的资源
		取值：
		  - Basic 认证：格式为 Basic <base64 编码的用户名:密码>（明文编码，安全性低，仅适用于测试或 HTTPS 场景）
		  - Bearer 认证：格式为 Bearer <token>（常用于 OAuth 2.0 授权，如 JWT 令牌）
		  - 其他认证方式：如 OAuth 2.0、JWT 等
		示例：
		  - Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...
	*/
	AUTHORIZATION = "Authorization"

	/*
		Cookie

		作用：
		  - 客户端向服务器传递之前存储的 Cookie 数据（服务器通过 Set-Cookie 响应头设置）
		场景：
		  - 用于维持会话状态（如登录状态）、用户偏好设置等
		示例：
		  - Cookie: sessionId=abc123; username=examples
	*/
	COOKIE = "Cookie"

	/*
		Referer

		作用：
			- 标识当前请求的来源页面 URL，即用户从哪个页面跳转而来
		场景：
			- 服务器可用于统计流量来源、防盗链等（注意：部分浏览器在隐私模式下可能不发送该头）
		示例：
			- Referer: https://www.google.com/
	*/
	REFERER = "Referer"

	/*
		Range

		作用：
		  - 请求资源的部分内容（断点续传、分块下载），服务器成功返回部分内容时，响应状态码为 206 Partial Content
		格式：
		  - Range: bytes=start-end
		示例：
		  - Range: bytes=0-499（请求前 500 字节）
		  - Range: bytes=1000-（请求从 1000 字节开始到末尾的内容）
	*/
	RANGE = "Range"
)
