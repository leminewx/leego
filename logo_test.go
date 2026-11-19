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
