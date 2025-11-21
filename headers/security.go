package headers

const (
	// ******************************************************************************************************************
	// * 安全头：适用于 HTTP 响应头，通过控制浏览器行为，增强 Web 应用安全性，
	// * 常见 Web 安全威胁：防御跨站脚本（XSS）、跨站请求伪造（CSRF）、点击劫持、中间人攻击、拒绝服务攻击等
	// ******************************************************************************************************************

	/*
		Referrer-Policy

		作用：
		  - 控制 Referrer 头的发送，防止信息泄露。策略决定了在从源站 A 跳转到目标站 B 的场景下，发送多少信息给 B
		  - 防止信息泄露： 默认情况下，浏览器在发起请求时会带上 Referer 头，其中包含了用户来自哪个页面的完整 URL
		  - 平衡功能与安全： 某些情况下，服务器需要 Referer 信息来进行分析、防盗链或实现业务逻辑
		取值：
		  - origin：在任何情况下都只发送源（协议+主机名+端口），不发送路径等具体信息
		  - same-origin：同源时发送完整 URL，跨域时不发送
		  - no-referrer：不发送任何信息（最严格）
		  - no-referrer-when-downgrade：从 HTTPS 到 HTTP（安全性降级）时不发送，其他情况发送完整 URL（默认策略）
		  - unsafe-url：在任何情况下都发送完整 URL，即使从 HTTPS 到 HTTP（最宽松，不推荐）
		  - origin-when-cross-origin：同源时发送完整 URL，跨域时只发送源（很常用）
		  - strict-origin-when-cross-origin：同源时发送完整 URL；跨域到安全目标（HTTPS）时发送源；降级到 HTTP时不发送源（现代浏览器默认推荐）
		示例：
		  - Referrer-Policy: strict-origin-when-cross-origin, origin-when-cross-origin, origin
		注意：
		  - 最佳实践是主动在你的 Web 服务器或应用框架中设置一个全局的、安全的策略，不要依赖浏览器的默认行为（因为默认值可能变化，且不一致）
		  - 推荐使用 strict-origin-when-cross-origin
		  - 对敏感操作使用更严格的策略：对于包含敏感信息（如密码、银行卡号）的表单页面或管理后台，可以考虑使用 same-origin 或 no-referrer
		  - 谨慎使用 unsafe-url，除非有非常特殊的业务需求（例如，一个需要将完整 URL 传递给第三方服务的支付回调），否则应避免使用
	*/
	REFERRER_POLICY = "Referrer-Policy"

	/*
		Permissions-Policy（之前叫 Feature-Policy）

		作用：
		  - 安全性：防止恶意代码滥用浏览器功能。例如，一个被植入的第三方脚本无法在你的网站上偷偷打开摄像头或麦克风
		  - 隐私保护：限制那些可能侵犯用户隐私的 API（如摄像头、麦克风、地理位置等），除非你明确需要它们
		  - 最佳实践与性能：通过禁用不必要的功能（如同步脚本、某些过时的 API），可以改善网站的性能和稳定性，并强制执行最佳实践
		  - 简单来说，它让网站开发者从"默认允许"转变为"按需授权"，对网站上的资源（包括自己的代码和所有第三方脚本）拥有更强的控制力
		格式：
		  - Permissions-Policy: <feature> <allowlist>, <feature> <allowlist>, ...
		  	* <feature>：要控制的浏览器功能或 API 的名称，支持 camera、geolocation、microphone、usb、bluetooth 等
		  	* <allowlist>：指定允许的源列表，支持域名、IP 地址、协议、端口号、*（表示任意源）等
		<feature>：
		  - usb：控制 WebUSB API 的访问
		  - camera：控制摄像头访问
		  - microphone：控制麦克风访问
		  - bluetooth：控制 Web Bluetooth API 的访问
		  - fullscreen：控制是否允许元素进入全屏模式
		  - geolocation：控制地理位置 API
		  - autoplay：控制媒体（音频/视频）的自动播放
		  - payment：控制 Payment Request API 的使用
		  - display-capture：控制屏幕、窗口或标签页的捕获（getDisplayMedia）
		  - sync-xhr：控制是否允许同步 XMLHttpRequest
		  - encrypted-media：控制 Encrypted Media Extensions (EME) API 的使用（用于 DRM 保护内容，如 Netflix）
		  - picture-in-picture：控制画中画功能
		  - web-share：控制 Web Share API 的使用
		<allowlist>：
		  - *：允许在任何上下文中使用该功能，包括当前页面和所有嵌入的帧（iframe）
		  - src：（仅适用于 iframe）允许在加载到该 iframe 中的内容中使用，但仅当该内容来自与 src 属性相同的源时
		  - self：允许在当前页面的同源上下文中使用，但跨域的 iframe 中不允许
		  - 'none'：完全禁用该功能，在所有上下文中都不可用
		  - 'origin'：允许在指定的源中使用（例如 https://example.com），可以指定一个或多个源
		示例：
		  - Permissions-Policy: camera='none' --- 完全禁止网站使用摄像头
		  - Permissions-Policy: microphone=(), sync-xhr=(), geolocation=(self) --- 禁用麦克风和同步XHR，地理位置仅限同源
		  - Permissions-Policy: autoplay=(self "https://cdn.example.com"), fullscreen=() --- 允许自己和特定CDN自动播放，禁用全屏
		注意：
		  - 采用"最小权限"原则：默认情况下禁用所有不必要的功能，然后根据需要逐个启用
		  - 从严格的配置开始：一个良好的起点是禁用一些高风险和不常用的功能
		  - 一个安全的基准策略：
			Permissions-Policy: camera=(), microphone=(), geolocation=(), payment=(), usb=(), sync-xhr=()
		  - 审查你的依赖：了解你的网站使用了哪些第三方脚本（分析、广告、小部件等），并确保你的策略不会破坏它们的功能
		  - 利用报告机制：Permissions-Policy 可以与 Reporting API 结合使用，当策略被违反时，浏览器可以向一个指定的端点发送报告：
		  	Permissions-Policy: geolocation=(); report-to="default"
		  - 在 iframe 上善用 allow 属性。如果你嵌入了来自不同源的第三方内容（如 YouTube 视频、地图），请使用 allow 属性精确授予它们所需的权限
	*/
	PERMISSIONS_POLICY = "Permissions-Policy"

	/*
		Content-Security-Policy（CSP - 内容安全策略）

		作用：
		  - 限制页面可加载的资源来源（如脚本、样式、图片、API 等），从根源防御 XSS 攻击和数据注入
		指令：
		  - default-src 'self'：默认限制所有资源仅从当前域名加载（优先级最低，未指定指令时生效）
		  - script-src 'self' https://trusted-cdn.com：限制脚本仅从当前域名和可信 CDN 加载
		  - style-src 'self' 'unsafe-inline'：允许内联样式（unsafe-inline 需谨慎使用，尽量避免）
		  - img-src 'self' data: https://*.example.com：允许图片从当前域名、Data URI 及 example.com 子域名加载
		  - connect-src 'self' https://api.example.com：限制 AJAX/fetch 请求的目标域名
		  - frame-src 'none'：禁止页面被嵌入 iframe（防御点击劫持）
		  - report-uri /csp-report-endpoint：指定 CSP 违规报告的提交地址（现代浏览器推荐用 report-to）
		示例：
		  - Content-Security-Policy: default-src 'self'; script-src 'self' https://cdn.jsdelivr.net; img-src 'self' data:; frame-src 'none'; report-to /csp-report
		注意：
		  - 避免使用 unsafe-inline（允许内联脚本 / 样式）和 unsafe-eval（允许动态执行代码），仅在 legacy 系统兼容时临时使用
		  - 可先使用 Content-Security-Policy-Report-Only 头进行测试，仅记录违规行为不阻断资源加载，避免影响业务
	*/
	CONTENT_SECURITY_POLICY = "Content-Security-Policy"

	/*
		X-XSS-Protection（XSS - 防护）

		作用：
		  - 启用浏览器内置的 XSS 过滤机制，当检测到潜在 XSS 攻击时，阻止页面加载或过滤恶意代码。
		取值：
		  - 0：禁用 XSS 过滤
		  - 1：启用过滤（默认行为，检测到攻击时过滤恶意代码）
		  - 1; mode=block：启用过滤，检测到攻击时直接阻断页面加载（更严格，推荐使用）
		示例：
		  - X-XSS-Protection: 1; mode=block
		注意：
		  - 该头已被现代浏览器逐步淘汰（如 Chrome 83+ 已移除支持），更可靠的方案是依赖 CSP 和代码层面的 XSS 防护（如输入过滤、输出编码）
		  - 仅作为辅助防护手段，不可单独依赖
	*/
	X_XSS_PROTECTION = "X-XSS-Protection"

	/*
		X-Frame-Options （ClickJacking - 点击劫持防御）

		作用：
		  - 控制页面是否允许被嵌入到 iframe 中，防止恶意网站通过 iframe 嵌套目标页面实施点击劫持攻击
		取值：
		  - DENY：完全禁止被嵌入（最安全）
		  - SAMEORIGIN：仅允许当前域名下的 iframe 嵌入
		  - ALLOW-FROM uri： 仅允许指定域名的 iframe 嵌入（部分浏览器不支持，推荐用 CSP 的 frame-src 替代）
		示例：
		  - X-Frame-Options: ALLOW-FROM https://trusted-domain.com
		注意：
		 - 与 CSP 的 frame-src 指令功能重叠，现代应用建议优先使用 CSP，若需兼容旧浏览器可同时配置
	*/
	X_FRAME_OPTIONS = "X-Frame-Options"

	/*
		X-Content-Type-Options（MIME 类型嗅探防御）

		作用：
		  - 禁用浏览器的 MIME 类型嗅探行为，强制浏览器严格按照 Content-Type 头指定的类型解析资源，防止恶意文件伪装成合法资源（如将脚本文件伪装成图片）
		取值：
		  - nosniff：禁止嗅探（唯一有效值）
		注意：
		  - 必须确保服务器返回的 Content-Type 头与资源实际类型一致，否则可能导致资源加载失败
	*/
	X_CONTENT_TYPE_OPTIONS = "X-Content-Type-Options"

	/*
		Strict-Transport-Security（HSTS - 强制 HTTPS）

		作用：
		  - 告知浏览器后续访问该域名时，必须使用 HTTPS 协议，禁止通过 HTTP 访问，防御中间人攻击和 HTTP 降级攻击
		取值：
		  - max-age=31536000：HSTS 策略的有效期（秒），此处为 1 年
		  - includeSubDomains：将策略应用到所有子域名（可选，推荐启用）
		  - preload：申请将域名加入浏览器的 HSTS 预加载列表（可选，需在浏览器官方平台提交申请）
		示例：
		  - Strict-Transport-Security: max-age=31536000; includeSubDomains; preload
		注意：
		  - 配置前需确保域名已全面支持 HTTPS（包括所有子域名），否则会导致用户无法访问
		  - preload 一旦提交，移除周期较长，需谨慎操作
		  - 首次访问时浏览器未收到 HSTS 头，仍可能通过 HTTP 访问，需配合 HTTP 跳转 HTTPS 实现全链路加密
	*/
	STRICT_TRANSPORT_SECURITY = "Strict-Transport-Security"

	/*
		Access-Control-Allow-Origin

		作用：
		  - 解决跨域资源共享（CORS）问题，指定允许访问当前资源的跨域请求来源。
		取值：
		  - *：允许所有来源的跨域请求（不建议在生产环境使用，且不支持携带 Cookie）
		  - 具体域名：表示仅允许该域名跨域访问，如 https://example.com
		示例：
		  - Access-Control-Allow-Origin: * --- 允许所有来源的跨域请求
		  - Access-Control-Allow-Origin: https://example.com --- 仅允许 example.com 的跨域请求
	*/
	ACCESS_CONTROL_ALLOW_ORIGIN = "Access-Control-Allow-Origin"

	ACCESS_CONTROL_ALLOW_METHODS = "Access-Control-Allow-Methods"

	ACCESS_CONTROL_ALLOW_HEADERS = "Access-Control-Allow-Headers"

	ACCESS_CONTROL_EXPOSE_HEADERS = "Access-Control-Expose-Headers"

	ACCESS_CONTROL_REQUEST_METHOD = "Access-Control-Request-Method" // 实际请求方法

	ACCESS_CONTROL_REQUEST_HEADERS = "Access-Control-Request-Headers" // 实际请求携带的自定义字段

	/*
		Access-Control-Allow-Credentials

		作用：
		  - 允许携带 Cookie，解决跨域 Cookie 问题。
		取值：
		  - true：允许携带 Cookie
		  - false：不允许携带 Cookie
		示例：
		  - Access-Control-Allow-Credentials: true --- 允许携带 Cookie
		注意：
		  - 当该头取值为 true 时， Access-Control-Allow-Origin 必须指定具体的源，不能设置为 *
	*/
	ACCESS_CONTROL_ALLOW_CREDENTIALS = "Access-Control-Allow-Credentials"

	/*
		Access-Control-Max-Age

		作用：
		  - 缓存跨域头的过期时间，减少对源服务器的请求。
		取值：
		  - 缓存时间（秒），如 86400 表示 1 天
		示例：
		  - Access-Control-Max-Age: 86400 --- 缓存 1 天
	*/
	ACCESS_CONTROL_MAX_AGE = "Access-Control-Max-Age"
)
