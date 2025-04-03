package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"gitee.com/leminewx/leego"
	_ "gitee.com/leminewx/leego/logo"
	"gitee.com/leminewx/leego/response"
)

func main() {
	// engine := leego.NewEngine("test")
	// engine.WithMiddlewares(
	// 	leego.Cors(cors.NewCORS()),
	// 	leego.AuthFromHubuByHTTP(http.MethodPost, "http://10.254.249.225:12345/api/auth"),
	// 	leego.Log(),
	// )

	engine := leego.NewDefaultEngine("test")
	engine.WithInterceptors(func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
			response := response.NewResponseWriter(resp)
			handler.ServeHTTP(response, req)

			if response.GetStatusCode() == http.StatusNotFound {
				fmt.Println("Oh my god, not found")
			}
		})
	})

	{
		group1 := engine.Group("/v1").WithMiddlewares(
			func(handle leego.HandleFunc) leego.HandleFunc {
				return func(ctx *leego.Context) {
					fmt.Println("/v1 1 start")
					handle(ctx)
					fmt.Println("/v1 1 end")
				}
			},
			func(handle leego.HandleFunc) leego.HandleFunc {
				return func(ctx *leego.Context) {
					fmt.Println("/v1 2 start")
					handle(ctx)
					fmt.Println("/v1 2 end")
				}
			})

		group1.GET("/a", func(ctx *leego.Context) {
			fmt.Println("/v1/a ok")
			ctx.ResponseJSON(http.StatusOK, leego.Json{"code": "00000", "desc": "ok"})
		}, "通用API1")

		group1.GET("/ws", func(ctx *leego.Context) {
			ctx.ResponseUpgrade(func(conn net.Conn, reader *bufio.ReadWriter) {
				// 简单消息处理循环
				for {
					// 读取客户端消息（简化版，仅处理小文本消息）
					frame, err := readFrame(reader)
					if err != nil {
						log.Println("Read error:", err)
						return
					}

					// 处理消息（示例：原样返回）
					log.Printf("Received: %s", frame.payload)
					sendTextFrame(conn, frame.payload)
				}
			})
		})

		group1.WithStatics("/files", "D:\\Projects\\Golang\\leego\\test", "下载文件1")
	}

	{
		group2 := engine.Group("/v2").WithMiddlewares(
			func(handle leego.HandleFunc) leego.HandleFunc {
				return func(ctx *leego.Context) {
					fmt.Println("/v2 1 start")
					handle(ctx)
					fmt.Println("/v2 1 end")
				}
			},
			func(handle leego.HandleFunc) leego.HandleFunc {
				return func(ctx *leego.Context) {
					fmt.Println("/v2 2 start")
					handle(ctx)
					fmt.Println("/v2 2 end")
				}
			})

		group2.GET("/b", func(ctx *leego.Context) {
			fmt.Println("*********ready response b*********")
			fmt.Println("/v2/b ok")
			ctx.ResponseJSON(http.StatusOK, leego.Json{"code": "00000", "desc": "ok"})
		}, "通用API2")

		{
			group3 := group2.Group("/login").WithMiddlewares(
				func(handle leego.HandleFunc) leego.HandleFunc {
					return func(ctx *leego.Context) {
						fmt.Println("/v2/login 1 start")
						handle(ctx)
						fmt.Println("/v2/login 1 end")
					}
				},
			)

			group3.POST("/{name}", func(ctx *leego.Context) {
				fmt.Println("*********ready response login*********")
				fmt.Printf("/v2/login/%s ok\n", ctx.GetPathValue("name"))
				ctx.ResponseJSON(http.StatusOK, leego.Json{"code": "00000", "desc": "ok"})
			}, "登录系统", "用户管理")

			group3.GET("/sse", func(ctx *leego.Context) {
				fmt.Println("*********ready response sse*********")
				ctx.ResponseSSE(func() <-chan []byte {
					data := make(chan []byte)
					go func() {
						for i := 0; i < 10; i++ {
							data <- []byte(fmt.Sprintf("hello, now is %v", time.Now()))
							time.Sleep(1 * time.Second)
						}

						close(data)
					}()

					return data
				})
			}, "获取系统信息", "用户管理")

			// group3.WithStatics("/files", "D:\\Projects\\Golang\\leego\\logo", "下载文件2")

			group3.GET("/files/download/*", func(ctx *leego.Context) {
				fmt.Println("*********ready download file*********")
				if err := ctx.ResponseFile("APIS.csv"); err != nil {
					ctx.ResponseFail(http.StatusInternalServerError, err.Error())
				}
			}, "下载文件", "文件管理")

			group3.GET("/files/show/*", func(ctx *leego.Context) {
				fmt.Println("*********ready response file content*********")
				if err := ctx.ResponseFileContent("test.go"); err != nil {
					ctx.ResponseFail(http.StatusInternalServerError, err.Error())
				}
			}, "显示文件", "文件管理")
		}
	}

	engine.ListenAndServe(&leego.Options{
		Addr: ":10000",
	})
}

// WebSocket 帧结构（简化版）
type wsFrame struct {
	fin     bool
	opcode  byte
	payload []byte
}

func readFrame(rw *bufio.ReadWriter) (*wsFrame, error) {
	// 读取前两个字节
	head, err := rw.ReadByte()
	if err != nil {
		return nil, err
	}
	second, err := rw.ReadByte()
	if err != nil {
		return nil, err
	}

	frame := &wsFrame{
		fin:    (head & 0x80) != 0,
		opcode: head & 0x0F,
	}

	masked := (second & 0x80) != 0
	length := int(second & 0x7F)

	// 处理扩展长度
	switch {
	case length == 126:
		b1, _ := rw.ReadByte()
		b2, _ := rw.ReadByte()
		length = int(b1)<<8 | int(b2)
	case length == 127:
		// 简化处理，不处理超大长度
		return nil, nil
	}

	// 读取掩码密钥
	var mask []byte
	if masked {
		mask = make([]byte, 4)
		rw.Read(mask)
	}

	// 读取数据
	payload := make([]byte, length)
	rw.Read(payload)

	// 应用掩码
	if masked {
		for i := 0; i < len(payload); i++ {
			payload[i] ^= mask[i%4]
		}
	}

	frame.payload = payload
	return frame, nil
}

func sendTextFrame(conn net.Conn, data []byte) {
	// 构建简单帧头
	header := []byte{0x81} // FIN + Text Frame
	length := len(data)
	if length <= 125 {
		header = append(header, byte(length))
	} else {
		// 简化处理，不支持长消息
		return
	}

	// 发送帧
	fullFrame := append(header, data...)
	conn.Write(fullFrame)
}
