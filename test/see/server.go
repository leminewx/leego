package main

import (
	"fmt"
	"log"
	"net"
)

func main() {
	// 解析UDP地址
	addr, err := net.ResolveUDPAddr("udp", ":10001")
	if err != nil {
		log.Fatal(err)
	}

	// 创建UDP连接
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	fmt.Println("UDP服务器正在监听 8080 端口...")

	buffer := make([]byte, 1024)

	for {
		// 读取数据
		n, clientAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			log.Printf("读取错误: %v", err)
			continue
		}

		message := string(buffer[:n])
		fmt.Printf("收到来自 %s 的消息: %s\n", clientAddr, message)

		// 回复客户端
		response := fmt.Sprintf("已收到你的消息: %s", message)
		_, err = conn.WriteToUDP([]byte(response), clientAddr)
		if err != nil {
			log.Printf("回复错误: %v", err)
		}
	}
}
