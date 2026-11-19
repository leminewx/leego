package leego

import (
	"os"
	"sync/atomic"
	"text/template"

	"gitee.com/leminewx/leego/logging"
)

// LogoStyle 指定 ShowLogo 绘制哪一种启动标识。
type LogoStyle int32

const (
	// LOGO_STYLE_CLASSIC 为原始宽幅艺术字，也是默认样式。
	LOGO_STYLE_CLASSIC LogoStyle = iota
	// LOGO_STYLE_PIXEL 为实心像素块，29 列，视觉冲击最强。
	LOGO_STYLE_PIXEL
	// LOGO_STYLE_SHADOW 为双线阴影，品牌感最强，依赖终端 Unicode 制表符。
	LOGO_STYLE_SHADOW
	// LOGO_STYLE_ASCII 为纯 ASCII 斜线体，任何编码与字体下都不会错位。
	LOGO_STYLE_ASCII
)

// currentLogoStyle 保存当前标识样式。
//
// 用原子整数而不是普通变量：样式可能在启动协程中设置，
// 而 ShowLogo 可能被并发调用（多入口进程或并行测试）。
var currentLogoStyle atomic.Int32

// SetLogoStyle 设置 ShowLogo 使用的标识样式，越界值被忽略。
func SetLogoStyle(style LogoStyle) {
	if style < LOGO_STYLE_CLASSIC || style > LOGO_STYLE_ASCII {
		return
	}

	currentLogoStyle.Store(int32(style))
}

// GetLogoStyle 返回当前标识样式。
func GetLogoStyle() LogoStyle {
	return LogoStyle(currentLogoStyle.Load())
}

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

// logoTemplatePixel 为实心像素块风格，29 列。
var logoTemplatePixel = `
█     █████ █████ █████ █████
█     █     █     █     █   █
█     ████  ████  █  ██ █   █
█     █     █     █   █ █   █
█████ █████ █████ █████ █████

   Copyright (c) {{.Year}} Leminewx. All rights reserved.
---------------------------------------------------------------
   AppName: Leego
   Version: {{.Version}}
   Release: {{.Release}}
---------------------------------------------------------------
`

// logoTemplateShadow 为双线阴影风格，需要终端支持 Unicode 制表符。
var logoTemplateShadow = `
██╗     ███████╗███████╗ ██████╗  ██████╗
██║     ██╔════╝██╔════╝██╔════╝ ██╔═══██╗
██║     █████╗  █████╗  ██║  ███╗██║   ██║
██║     ██╔══╝  ██╔══╝  ██║   ██║██║   ██║
███████╗███████╗███████╗╚██████╔╝╚██████╔╝
╚══════╝╚══════╝╚══════╝ ╚═════╝  ╚═════╝

   Copyright (c) {{.Year}} Leminewx. All rights reserved.
---------------------------------------------------------------
   AppName: Leego
   Version: {{.Version}}
   Release: {{.Release}}
---------------------------------------------------------------
`

// logoTemplateASCII 为纯 ASCII 斜线体，不依赖任何扩展字符。
var logoTemplateASCII = `
 _     ___   ___   ___   ___
| |   | __| | __| / __| / _ \
| |   | _|  | _|  | (_| | | |
| |   | |__ | |__ \__ \ | |_|
|___| |___| |___| |___/ \___/

   Copyright (c) {{.Year}} Leminewx. All rights reserved.
---------------------------------------------------------------
   AppName: Leego
   Version: {{.Version}}
   Release: {{.Release}}
---------------------------------------------------------------
`

// getLogoTemplate 返回样式对应的模板，未知样式回落到默认。
func getLogoTemplate(style LogoStyle) string {
	switch style {
	case LOGO_STYLE_PIXEL:
		return logoTemplatePixel
	case LOGO_STYLE_SHADOW:
		return logoTemplateShadow
	case LOGO_STYLE_ASCII:
		return logoTemplateASCII
	default:
		return logoTemplate
	}
}

// ShowLogo 显示 logo
func ShowLogo() {
	show()
}

func show() {
	// 解析模板
	tmpl, err := template.New("logo").Parse(getLogoTemplate(GetLogoStyle()))
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
