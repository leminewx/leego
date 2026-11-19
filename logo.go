package leego

import (
	"os"
	"text/template"

	"gitee.com/leminewx/leego/logging"
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
    
   Copyright (c) {{.Year}} Leminewx. All rights reserved.
---------------------------------------------------------------
   AppName: Leego
   Version: {{.Version}}
   Release: {{.Release}}
---------------------------------------------------------------
`

// ShowLogo 显示 logo
func ShowLogo() {
	show()
}

func show() {
	// 解析模板
	tmpl, err := template.New("logo").Parse(logoTemplate)
	if err != nil {
		logging.LogWarn("Failed to load logo template: %v", err)
		return
	}

	// 写入数据
	if err = tmpl.Execute(os.Stdout, &struct {
		Year    string
		Version string
		Release string
	}{
		Year:    year,
		Version: version,
		Release: release,
	}); err != nil {
		logging.LogWarn("Failed to write logo data to template: %v", err)
	}
}
