package headers

const (
	// ******************************************************************************************************************
	// * 通用头：适用于 HTTP 请求和响应，提供基础的传输控制信息
	// ******************************************************************************************************************

	/*
		Date

		作用：
		  - 表示消息发送的时间戳，格式遵循 RFC 7231 定义的 HTTP 日期格式
		应用：
		  - 服务器可用于判断请求的时效性，客户端可用于计算响应延迟
		示例：
		  - Date: Wed, 21 Oct 2015 07:28:00 GMT
	*/
	DATE = "Date"

	/*
		Cache-Control

		作用：
		  - 控制缓存的行为，是缓存策略的核心头字段，优先级高于其他缓存相关头（如 Expires）
		取值：
		  - public：允许代理服务器缓存资源（默认私有资源仅客户端可缓存）
		  - private：仅客户端可缓存，代理服务器禁止缓存
		  - no-cache：强制验证缓存（需向服务器确认资源是否更新），并非不缓存
		  - no-store：禁止缓存资源（客户端和代理服务器均不能存储）
		  - max-age=3600：资源的新鲜度为 3600 秒（客户端在该时间内可直接使用缓存）
		示例：
		  - Cache-Control: public, max-age=86400
	*/
	CACHE_CONTROL = "Cache-Control"

	/*
		Connection

		作用：
		  - 控制连接的状态，如是否保持长连接
		取值：
		  - close：请求 / 响应完成后关闭连接
		  - keep-alive：保持 TCP 连接不关闭，用于复用连接（HTTP/1.1 默认开启）
		  - upgrade：表示客户端希望将连接升级到另一种协议（必须与 Upgrade 头配合使用）
		示例：
		  - Connection: keep-alive
	*/
	CONNECTION = "Connection"

	/*
		Upgrade

		作用：
		  - 提供一种简单的机制，将当前连接从HTTP/1.1切换到其他应用层协议
		取值：
		  - WebSocket：从HTTP升级到WebSocket协议
		  - HTTP/2 (h2c)：通过HTTP/1.1升级到HTTP/2（不加密的通道）
	*/
	UPGRADE = "Upgrade"

	/*
		Allow

		作用：
		  - 用于 405 Method Not Allowed 响应，告知客户端该资源支持哪些 HTTP 方法
		  - 也可用于 204/304 等场景声明资源的可用方法集合
		示例：
		  - Allow: GET, HEAD, POST
	*/
	ALLOW = "Allow"

	/*
		Via

		作用：
		  - 记录请求 / 响应经过的代理服务器或网关，用于追踪传输路径
		示例：
		  - Via: 1.1 proxy-server-1 (Apache/2.4.18), 1.0 proxy-server-2 (Nginx/1.21.0)
	*/
	VIA = "Via"

	/*
		Content-Type

		作用：
			- 指定消息体的媒体类型（MIME 类型）和字符编码，是最常用的实体头
			- 用于明确请求体或响应体的格式和编码，服务端 / 客户端据此解析数据，常用类型与 Accept 对应
		取值：
		  - text/html：优先级最高的类型之一，用于接收 HTML 网页（浏览器默认核心接收类型）
		  - text/plain：接收纯文本内容（如 txt 文件、简单字符串响应）
		  - text/xml：消息体为 XML 格式数据
		  - text/css：接收 CSS 样式文件（客户端加载样式资源时使用）
		  - application/json：消息体为 JSON 格式数据，前后端 API 交互的主流类型
		  - application/xml：消息体为 XML 格式数据
		  - application/x-www-form-urlencoded：表单数据的默认编码格式，数据以 key1=value1&key2=value2 形式拼接
		  - application/octet-stream：二进制流数据（通用文件类型，服务器无法识别具体格式时使用，客户端通常会触发下载）
		  - application/zip：ZIP 压缩文件
		  - application/pdf：PDF 文件
		  - multipart/form-data：表单数据的混合编码格式，支持同时传输文本和文件（如图片、文档）
		  - image/jpeg：JPG 图片
		  - image/jpeg：JPG/JPEG 图片
		  - image/png：PNG 图片
		  - image/gif：GIF 图片
		  - image/webp：WebP 图片（压缩率更高，支持透明和动画）
		  - audio/mpeg：MP3 音频
		  - video/mp4：MP4 视频
		  - ......
		示例：
			- Content-Type: application/json; charset=utf-8
	*/
	CONTENT_TYPE = "Content-Type"

	/*
		Content-Length

		作用：
		  - 指定消息体的字节长度，服务器可据此判断请求 / 响应是否完整
		示例：
		  - Content-Length: 1024
		注意：
		  - 若消息体采用分块传输（Transfer-Encoding: chunked），则该头会被忽略
	*/
	CONTENT_LENGTH = "Content-Length"

	/*
		Content-Range

		作用：
		  - 在部分响应（206 Partial Content）中，指定返回的资源片段在完整资源中的位置
		示例：
		  - Content-Range: bytes 0-499/2000（返回前 500 字节，完整资源共 2000 字节）
	*/
	CONTENT_RANGE = "Content-Range"

	/*
		Content-Disposition

		作用：
		  - 作为响应头，指示回复的内容该以何种形式展示，是以内联（inline）的方式（即在浏览器中直接显示），还是以附件（attachment）的形式下载并保存到本地
		  - 作为请求头，附带上传文件字段信息
		格式：
		  - Content-Disposition: inline | attachment; filename="filename.txt"; filename*=UTF-8''%E6%8A%A5%E5%91%8A.txt
		  - Content-Disposition: form-data; name="fieldName"; filename="filename.txt"
		取值：
		  - inline：默认值，表示回复的内容会显示在网页内，或者作为网页的一部分。当然，如果浏览器无法显示（比如没有合适的插件），则会下载到本地
		  - attachment：表示回复的内容应该被下载到本地；大多数情况下会弹出“保存为”对话框
		  - form-data：用于 multipart/form-data 类型的请求体中，用于每个字段的信息
		  - filename：可选参数，指定保存的文件名。如果省略，浏览器通常会使用请求的 URL 的最后一部分作为文件名
		  - filename*：早期浏览器支持的一种非标准的参数，并采用 RFC 5987 中规定的编码方式，格式是：<字符集>'<语言>'<实际编码后的文件名>
		示例：
		  - Content-Disposition: inline; filename="filename.jpg" --- 表示回复的内容会以内联方式显示，并指定文件名为 filename.jpg
		  - Content-Disposition: attachment; filename="filename.jpg" --- 表示回复的内容应该被下载到本地，并指定文件名为 filename.jpg
		  - Content-Disposition: form-data; name="fieldName"; filename="filename.jpg" --- 表示上传文件的字段信息，并指定文件名为 filename.jpg
		注意：
		  - 现在更常见的做法是使用 filename* 和 filename 两个参数，以便兼容新旧浏览器
	*/
	CONTENT_DISPOSITION = "Content-Disposition"

	/*
		Content-Encoding

		作用：
		  - 告知对端服务器对消息体数据使用的压缩编码方式，如 gzip、deflate 等
		取值：
		  - br：用 Brotli 压缩算法，是一种新的压缩算法，压缩比更高（新兴标准）
		  - gzip：使用 LZ77 编码和32位CRC的格式。该编码方式在客户端和服务器中广泛支持，效率较高
		  - deflate：使用 zlib 结构（在 RFC 1950 中定义）和 deflate 压缩算法（在 RFC 1951 中定义）
		  - compress：压缩传输，采用 Lempel-Ziv-Welch (LZW) 算法对数据进行压缩，并使用 Huffman 码进行编码。由于专利问题（已过期）和性能问题，现在很少使用
		  - identity：指示不进行编码。这是默认值，但除非明确指定，否则不会在头部中出现。如果客户端在请求中发送了包含 identity 的 TE 头部，表示客户端可以接受不编码的响应
		示例：
		  - Content-Encoding: gzip
		  - Content-Encoding: gzip, br（组合压缩：原始内容 → br 压缩 → gzip 压缩 → 传输 → gzip 解压 -> br 解压 → 原始内容）
	*/
	CONTENT_ENCODING = "Content-Encoding"

	/*
		Transfer-Encoding

		作用：
		  - 指定了消息体在传输过程中使用的编码方式，主要用于在发送方和接收方之间安全地传输消息体
		取值：
		  - chunked：分块传输，每个块包含一个十六进制的块大小，后跟块数据，最后以一个大小为0的块结束。该编码允许发送方在不知道内容总长度的情况下开始传输，特别适用于动态生成的内容或流式数据
		  - gzip：使用 LZ77 编码和32位CRC的格式。该编码方式在客户端和服务器中广泛支持，效率较高
		  - deflate：使用 zlib 结构（在 RFC 1950 中定义）和 deflate 压缩算法（在 RFC 1951 中定义）
		  - compress：压缩传输，采用 Lempel-Ziv-Welch (LZW) 算法对数据进行压缩，并使用 Huffman 码进行编码。由于专利问题（已过期）和性能问题，现在很少使用
		  - identity：指示不进行编码。这是默认值，但除非明确指定，否则不会在头部中出现。如果客户端在请求中发送了包含 identity 的 TE 头部，表示客户端可以接受不编码的响应
		示例：
		  - Transfer-Encoding: gzip
		注意：
		  - Transfer-Encoding 是逐跳（hop-by-hop）头部，即它只适用于单个传输连接，而不是整个端到端路径，这意味着代理可能会修改它。
			通常，Transfer-Encoding 取值主要是 chunked，而压缩通常通过 Content-Encoding 头部指定
	*/
	TRANSFER_ENCODING = "Transfer-Encoding"
)
