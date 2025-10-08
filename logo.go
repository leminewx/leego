package leego

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
	"time"
)

// logo 模板
var logoTemplate = `
  ___       _______   _______   _______   _______
 |\  \     |\   ___\ |\   ___\ |\   ___\ |\   __ \
 \ \  \    \ \  \__|_\ \  \__|_\ \  \__|_\ \  \|\ \
  \ \  \    \ \   ___\\ \   ___\\ \   __ \\ \  \\\ \ 
   \ \  \____\ \  \__|_\ \  \__|_\ \  \_\ \\ \  \\\ \
    \ \______\\ \______\\ \______\\ \______\\ \______\
     \|______| \|______| \|______| \|______| \|______|
    
   Copyright (c) {{.NowYear}} Lemine. All rights reserved.
---------------------------------------------------------------
   AppName: Leego
   Version: {{.Version}}
   Release: {{.Release}}
---------------------------------------------------------------
`

// 初始化 logo
func init() {
	// 获取当前文件的绝对路径
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		fmt.Printf("|\033[42m\033[30m ERROR \033[0m| %s Not found leego/logo/logo.go\n", time.Now().Format(time.DateTime))
		return
	}

	// 构建目标文件的路径
	versionFilePath := filepath.Join(filepath.Dir(filepath.Dir(currentFile)), "VERSION.txt")

	// 打开 VERSION.txt 文件
	versionFile, err := os.Open(versionFilePath)
	if err != nil {
		fmt.Printf("|\033[42m\033[30m ERROR \033[0m| %s Failed to open VERSION.txt: %v\n", time.Now().Format(time.DateTime), err)
		return
	}

	reader := bufio.NewReader(versionFile)
	first, _, err := reader.ReadLine()
	if versionFile.Close(); err != nil {
		fmt.Printf("|\033[42m\033[30m ERROR \033[0m| %s Failed to read VERSION.txt: %v\n", time.Now().Format(time.DateTime), err)
		return
	}

	// 解析第一行（最新版本信息）
	release := strings.TrimSpace(strings.Split(string(first), "-")[0])
	version := strings.TrimSpace(strings.Split(string(first), "-")[1])

	fields := strings.Split(release, "/")
	if len(fields) == 1 {
		fields = strings.Split(version, "-")
	}

	// 写入 logo 模板
	templ, err := template.New("logo").Parse(logoTemplate)
	if err != nil {
		fmt.Printf("|\033[42m\033[30m ERROR \033[0m| %s Failed to parse the logo template: %v\n", time.Now().Format(time.DateTime), err)
	}
	if err = templ.Execute(os.Stdout, &struct {
		NowYear string
		Version string
		Release string
	}{
		NowYear: fields[0],
		Version: version,
		Release: release,
	}); err != nil {
		fmt.Printf("|\033[42m\033[30m ERROR \033[0m| %s Failed to write version info to logo template: %v\n", time.Now().Format(time.DateTime), err)
	}
}
