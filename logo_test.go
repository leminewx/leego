package leego

import (
	"io"
	"os"
	"strings"
	"testing"
)

// TestShowLogo 验证启动 Logo 能被完整写出。
//
// ShowLogo 直接写 os.Stdout，因此这里临时替换标准输出以捕获内容。
// 管道缓冲区远大于 Logo 体积，不存在写阻塞风险。
func TestShowLogo(t *testing.T) {
	original := os.Stdout

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("创建管道失败: %v", err)
	}

	os.Stdout = writer
	defer func() { os.Stdout = original }()

	ShowLogo()

	if err = writer.Close(); err != nil {
		t.Fatalf("关闭管道写入端失败: %v", err)
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("读取 Logo 输出失败: %v", err)
	}

	if err = reader.Close(); err != nil {
		t.Fatalf("关闭管道读取端失败: %v", err)
	}

	output := string(data)
	if !strings.Contains(output, "Leego") {
		t.Fatalf("Logo 输出缺少应用名: %q", output)
	}

	if !strings.Contains(output, version) {
		t.Fatalf("Logo 输出缺少版本号 %q", version)
	}

	if !strings.Contains(output, release) {
		t.Fatalf("Logo 输出缺少发布标识 %q", release)
	}
}

// captureLogo 捕获一次 ShowLogo 的完整输出。
//
// ShowLogo 直接写 os.Stdout，因此替换标准输出后再恢复；
// 管道缓冲区远大于 Logo 体积，不存在写阻塞风险。
func captureLogo(t *testing.T) string {
	t.Helper()

	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("创建管道失败: %v", err)
	}

	os.Stdout = writer
	ShowLogo()
	os.Stdout = original

	if err = writer.Close(); err != nil {
		t.Fatalf("关闭管道写入端失败: %v", err)
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("读取 Logo 输出失败: %v", err)
	}

	if err = reader.Close(); err != nil {
		t.Fatalf("关闭管道读取端失败: %v", err)
	}

	return string(data)
}

// TestShowLogoStyles 验证每种样式都包含应用名、版本与发布标识。
func TestShowLogoStyles(t *testing.T) {
	original := GetLogoStyle()
	defer SetLogoStyle(original)

	styles := map[string]LogoStyle{
		"classic": LOGO_STYLE_CLASSIC,
		"pixel":   LOGO_STYLE_PIXEL,
		"shadow":  LOGO_STYLE_SHADOW,
		"ascii":   LOGO_STYLE_ASCII,
	}

	for name, style := range styles {
		t.Run(name, func(t *testing.T) {
			SetLogoStyle(style)

			output := captureLogo(t)
			for _, want := range []string{"Leego", version, release} {
				if !strings.Contains(output, want) {
					t.Fatalf("%s 样式输出缺少 %q:\n%s", name, want, output)
				}
			}
		})
	}
}

// TestSetLogoStyleRejectsOutOfRange 验证越界样式被忽略且不影响当前设置。
func TestSetLogoStyleRejectsOutOfRange(t *testing.T) {
	original := GetLogoStyle()
	defer SetLogoStyle(original)

	SetLogoStyle(LOGO_STYLE_ASCII)
	SetLogoStyle(LOGO_STYLE_ASCII + 1)
	SetLogoStyle(-1)

	if got := GetLogoStyle(); got != LOGO_STYLE_ASCII {
		t.Fatalf("越界样式被接受: %v", got)
	}
}
