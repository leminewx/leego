package main

import (
	"image"
	"image/png"
	"os"
	"syscall"
	"time"
	"unsafe"
)

// 引用 Windows API 函数（user32.dll、gdi32.dll）
var (
	user32               = syscall.NewLazyDLL("user32.dll")
	procGetDC            = user32.NewProc("GetDC")
	procReleaseDC        = user32.NewProc("ReleaseDC")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")

	gdi32                      = syscall.NewLazyDLL("gdi32.dll")
	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject           = gdi32.NewProc("SelectObject")
	procBitBlt                 = gdi32.NewProc("BitBlt")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
	procDeleteObject           = gdi32.NewProc("DeleteObject")
	procGetDIBits              = gdi32.NewProc("GetDIBits") // 显式定义GetDIBits，避免重复创建
)

// GetScreenSize 获取屏幕尺寸（宽度、高度）
func GetScreenSize() (int, int) {
	width, _, _ := procGetSystemMetrics.Call(0)  // SM_CXSCREEN = 0（屏幕宽度）
	height, _, _ := procGetSystemMetrics.Call(1) // SM_CYSCREEN = 1（屏幕高度）
	return int(width), int(height)
}
func CaptureScreen() (*image.RGBA, error) {
	width, height := GetScreenSize()

	// 1. 获取桌面 DC
	desktopDC, _, _ := procGetDC.Call(0)
	if desktopDC == 0 {
		return nil, syscall.GetLastError()
	}
	defer procReleaseDC.Call(0, desktopDC)

	// 2. 创建兼容 DC
	compatibleDC, _, _ := procCreateCompatibleDC.Call(desktopDC)
	if compatibleDC == 0 {
		return nil, syscall.GetLastError()
	}
	defer procDeleteDC.Call(compatibleDC)

	// 3. 创建兼容位图并选入 DC（同时保存原对象）
	bitmap, _, _ := procCreateCompatibleBitmap.Call(desktopDC, uintptr(width), uintptr(height))
	if bitmap == 0 {
		return nil, syscall.GetLastError()
	}
	defer procDeleteObject.Call(bitmap)

	originalObj, _, _ := procSelectObject.Call(compatibleDC, bitmap)
	if originalObj == 0 {
		return nil, syscall.GetLastError()
	}
	defer procSelectObject.Call(compatibleDC, originalObj) // 恢复原对象

	// 4. 执行 BitBlt 截图
	success, _, err := procBitBlt.Call(
		compatibleDC, 0, 0, uintptr(width), uintptr(height),
		desktopDC, 0, 0, 0x00CC0020, // SRCCOPY
	)
	if success == 0 {
		return nil, err
	}

	// 5. 准备 RGBA 缓冲区
	buf := make([]byte, width*height*4)
	h := &image.RGBA{
		Pix:    buf,
		Stride: 4 * width,
		Rect:   image.Rect(0, 0, width, height),
	}

	// 6. 完善位图信息结构体
	bitmapInfo := &struct {
		Size          uint32
		Width         int32
		Height        int32
		Planes        uint16
		BitCount      uint16
		Compression   uint32
		SizeImage     uint32
		XPelsPerMeter int32
		YPelsPerMeter int32
		ClrUsed       uint32
		ClrImportant  uint32
	}{
		Size:        40,
		Width:       int32(width),
		Height:      -int32(height),
		Planes:      1,
		BitCount:    32,
		Compression: 0, // BI_RGB
		SizeImage:   0,
	}

	// 7. 正确调用 GetDIBits（参数数量为 7）
	hdc := syscall.Handle(compatibleDC)
	_, _, err = syscall.SyscallN(
		procGetDIBits.Addr(),
		7, // 实际参数数量是 7 个
		uintptr(hdc),
		bitmap,
		0,
		uintptr(height),
		uintptr(unsafe.Pointer(&h.Pix[0])),
		uintptr(unsafe.Pointer(bitmapInfo)),
		0,
	)
	if err != nil && err != syscall.Errno(0) {
		return nil, err
	}

	return h, nil
}

// SaveImage 保存图像到指定路径
func SaveImage(img *image.RGBA, savePath string) error {
	if err := os.MkdirAll(savePath, 0755); err != nil {
		return err
	}
	now := time.Now().Format("20060102_150405")
	fullPath := savePath + "\\" + "Screenshot_" + now + ".png"

	file, err := os.Create(fullPath)
	if err != nil {
		return err
	}
	defer file.Close()

	return png.Encode(file, img)
}

func main() {
	interval := 15 * time.Second
	savePath := "D:\\Screenshots"

	println("Go 自动化截图工具已启动")
	println("截图间隔：", interval)
	println("保存路径：", savePath)
	println("按 Ctrl+C 停止程序...\n")

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// 首次截图
	if img, err := CaptureScreen(); err != nil {
		println("截图失败：", err.Error())
	} else {
		if err := SaveImage(img, savePath); err != nil {
			println("保存失败：", err.Error())
		}
	}

	// 定时截图
	for range ticker.C {
		if img, err := CaptureScreen(); err != nil {
			println("截图失败：", err.Error())
		} else {
			if err := SaveImage(img, savePath); err != nil {
				println("保存失败：", err.Error())
			}
		}
	}
}
