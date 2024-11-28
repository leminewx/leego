package leego

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"

	"gitee.com/leminewx/loggo"
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
		loggo.Fatal("not found leego/logo.go path")
		return
	}

	// 构建目标文件的路径
	versionFilePath := filepath.Join(filepath.Dir(currentFile), "VERSION.txt")

	// 读取版本文件
	file, err := os.Open(versionFilePath)
	if err != nil {
		loggo.Error(err.Error())
		return
	}

	reader := bufio.NewReader(file)
	first, _, err := reader.ReadLine()
	if file.Close(); err != nil {
		loggo.Error(err.Error())
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
		loggo.Fatal(err.Error())
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
		loggo.Fatal(err.Error())
	}
}
