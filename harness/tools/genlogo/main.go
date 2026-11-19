//go:build ignore

// 该程序用于生成 README 与文档使用的品牌图，不参与框架构建。
//
// 用法（在仓库根目录执行）：
//
//	go run ./harness/tools/genlogo/main.go ./docs/assets/logo.png
//
// 图形与 harness 首页的内联 SVG 同源（48 单位逻辑坐标系），
// 修改样式时两侧要一起改。
package main

import (
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
)

// 画布配置：48 单位逻辑坐标对应 1024 像素渲染，再按 4 倍盒式下采样输出
// 256 像素；逻辑图形与 harness 首页 SVG 同源。
const (
	canvasSize = 1024
	outputSize = 256
	unitScale  = float64(canvasSize) / 48.0
	downscale  = canvasSize / outputSize
	samples    = 3 // 每像素 3x3 超采样，用于抗锯齿
)

type point struct{ x, y float64 }

type rgb struct{ r, g, b float64 }

var (
	navy  = rgb{16.0 / 255, 23.0 / 255, 51.0 / 255}
	blue  = rgb{91.0 / 255, 140.0 / 255, 255.0 / 255}
	teal  = rgb{41.0 / 255, 211.0 / 255, 162.0 / 255}
	lPath []point
	arrow []point
)

// init 构造字形折线：L 由竖笔、下圆角与横笔组成。
func init() {
	lPath = append(lPath, point{18, 13}, point{18, 30})
	for i := 0; i <= 12; i++ {
		theta := math.Pi - float64(i)*math.Pi/24
		lPath = append(lPath, point{23 + 5*math.Cos(theta), 30 + 5*math.Sin(theta)})
	}
	lPath = append(lPath, point{30, 35})

	arrow = []point{{30, 15}, {35, 19}, {30, 23}}
}

// mix 按比例混合两端颜色。
func mix(from, to rgb, t float64) rgb {
	return rgb{from.r + (to.r-from.r)*t, from.g + (to.g-from.g)*t, from.b + (to.b-from.b)*t}
}

// gradient 返回该点在对角渐变中的颜色。
func gradient(p point) rgb {
	t := (p.x + p.y - 12) / 60
	return mix(blue, teal, math.Max(0, math.Min(1, t)))
}

// inRoundRect 判断点是否落在圆角矩形内。
func inRoundRect(p point, x0, y0, x1, y1, radius float64) bool {
	cx := math.Max(x0+radius, math.Min(p.x, x1-radius))
	cy := math.Max(y0+radius, math.Min(p.y, y1-radius))
	return math.Hypot(p.x-cx, p.y-cy) <= radius
}

// segDistance 返回点到线段的距离。
func segDistance(p, a, b point) float64 {
	dx, dy := b.x-a.x, b.y-a.y
	length := dx*dx + dy*dy
	t := 0.0
	if length > 0 {
		t = math.Max(0, math.Min(1, ((p.x-a.x)*dx+(p.y-a.y)*dy)/length))
	}
	return math.Hypot(p.x-(a.x+t*dx), p.y-(a.y+t*dy))
}

// polyDistance 返回点到折线的最短距离。
func polyDistance(p point, path []point) float64 {
	shortest := math.Inf(1)
	for i := 0; i+1 < len(path); i++ {
		if d := segDistance(p, path[i], path[i+1]); d < shortest {
			shortest = d
		}
	}
	return shortest
}

// sample 返回逻辑坐标点的颜色与不透明度。
func sample(p point) (rgb, float64) {
	if !inRoundRect(p, 1.5, 1.5, 46.5, 46.5, 13) {
		return rgb{}, 0
	}

	if inRoundRect(p, 3.5, 3.5, 44.5, 44.5, 11) {
		switch {
		case polyDistance(p, lPath) <= 2.25:
			return gradient(p), 1
		case polyDistance(p, arrow) <= 1.75:
			return teal, 1
		case math.Hypot(p.x-33, p.y-31) <= 2.6:
			return blue, 1
		default:
			return navy, 1
		}
	}

	return gradient(p), 1
}

// render 逐像素超采样生成图像。
func render() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, canvasSize, canvasSize))
	total := float64(samples * samples)

	for y := 0; y < canvasSize; y++ {
		for x := 0; x < canvasSize; x++ {
			var sum rgb
			var alpha float64

			for sy := 0; sy < samples; sy++ {
				for sx := 0; sx < samples; sx++ {
					px := (float64(x) + (float64(sx)+0.5)/samples) / unitScale
					py := (float64(y) + (float64(sy)+0.5)/samples) / unitScale

					c, a := sample(point{px, py})
					sum.r += c.r * a
					sum.g += c.g * a
					sum.b += c.b * a
					alpha += a
				}
			}

			if alpha == 0 {
				continue
			}

			offset := y*img.Stride + x*4
			img.Pix[offset] = toByte(sum.r / alpha)
			img.Pix[offset+1] = toByte(sum.g / alpha)
			img.Pix[offset+2] = toByte(sum.b / alpha)
			img.Pix[offset+3] = toByte(alpha / total)
		}
	}

	return img
}

// toByte 把 0~1 的分量换算为 0~255。
func toByte(v float64) uint8 {
	return uint8(math.Round(math.Max(0, math.Min(1, v)) * 255))
}

// downsample 把渲染结果按 4×4 盒式滤波缩到 outputSize。
//
// 颜色按 alpha 加权后再除总 alpha，避免透明边缘在平均值里被“提亮”；
// 直接在 256 像素画布上采样会损失边缘平滑度，所以先高分辨率渲染再缩小。
func downsample(src *image.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, outputSize, outputSize))
	total := uint64(downscale * downscale)

	for y := 0; y < outputSize; y++ {
		for x := 0; x < outputSize; x++ {
			var r, g, b, a uint64

			for sy := 0; sy < downscale; sy++ {
				for sx := 0; sx < downscale; sx++ {
					offset := (y*downscale+sy)*src.Stride + (x*downscale+sx)*4
					alpha := uint64(src.Pix[offset+3])
					r += uint64(src.Pix[offset]) * alpha
					g += uint64(src.Pix[offset+1]) * alpha
					b += uint64(src.Pix[offset+2]) * alpha
					a += alpha
				}
			}

			if a == 0 {
				continue
			}

			to := y*img.Stride + x*4
			img.Pix[to] = uint8(r / a)
			img.Pix[to+1] = uint8(g / a)
			img.Pix[to+2] = uint8(b / a)
			img.Pix[to+3] = uint8(a / total)
		}
	}

	return img
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: genlogo <output.png>")
		os.Exit(2)
	}

	file, err := os.Create(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "create file: %v\n", err)
		os.Exit(1)
	}

	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err = encoder.Encode(file, downsample(render())); err != nil {
		_ = file.Close()
		fmt.Fprintf(os.Stderr, "encode png: %v\n", err)
		os.Exit(1)
	}

	if err = file.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "close file: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("written:", os.Args[1])
}
