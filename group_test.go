package leego

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestSetStatics(t *testing.T) {
	absDir, _ := filepath.Abs("./test")
	fileServer := http.StripPrefix("/v1/files/", http.FileServer(http.Dir(absDir)))
	http.HandleFunc("/v1/files/{filepath...}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Println(r.URL.Path)
		// 获取请求的文件路径
		reqPath := r.URL.Path[len("/v1/files/"):]
		fullPath := filepath.Join(absDir, filepath.FromSlash(reqPath))

		// 检查文件是否存在
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			http.NotFound(w, r)
			log.Printf("文件未找到: %s", fullPath)
			return
		}

		// 禁用缓存
		w.Header().Set("Cache-Control", "no-store")
		// 服务文件
		fileServer.ServeHTTP(w, r)
	})

	log.Printf("服务目录: %s", absDir)
	log.Fatal(http.ListenAndServe(":10000", nil))
}
