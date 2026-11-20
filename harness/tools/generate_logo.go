//go:build ignore

// 该程序用于生成 README 与文档使用的品牌图，不参与框架构建。
//
// 用法（在仓库根目录执行）：
//
//	go run ./harness/tools/genlogo/main.go ./docs/assets/candidates
//
// 每个候选方案输出一张 256 像素 PNG，选定后用 `genlogo <file.png>` 单独生成。
// 图形与 harness 首页的内联 SVG 同源（48 单位逻辑坐标系），修改样式时两侧要一起改。
package main

import (
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// 画布配置：48 单位逻辑坐标对应 1024 像素渲染，再按 4 倍盒式下采样输出
// 256 像素。
const (
	canvasSize = 1024
	outputSize = 256
	unitScale  = float64(canvasSize) / 48.0
	downscale  = canvasSize / outputSize
	samples    = 3 // 每像素 3x3 超采样，用于抗锯齿
)

type point struct{ x, y float64 }

type rgb struct{ r, g, b float64 }

// stroke 描述一条圆头笔画，solid 为 false 时使用对角渐变。
type stroke struct {
	path  []point
	width float64
	ink   rgb
	solid bool
}

// disk 描述一个实心圆。
type disk struct {
	center point
	radius float64
	ink    rgb
	alpha  float64
}

// ring 描述一段圆环，角度按数学方向（0 度朝右、90 度朝上）取 [from, to]。
type ring struct {
	center point
	radius float64
	width  float64
	from   float64
	to     float64
}

// bar 描述一条细线，用于窗口栏与下划线等装饰。
//
// ink 为零值时取渐变，alpha 为零值时按不透明处理。
type bar struct {
	path  []point
	width float64
	alpha float64
	ink   rgb
}

// style 描述一个候选方案。
//
// bg/from/to 为零值时使用 navy、goCyan、teal。
type style struct {
	name     string
	inverted bool // 负空间：方块填充渐变，字母挖空
	circle   bool // 圆形徽章：外层为圆环而不是圆角方
	bg       rgb
	from     rgb
	to       rgb
	strokes  []stroke
	rings    []ring
	disks    []disk
	bars     []bar
	window   []disk
}

var (
	navy   = rgb{15.0 / 255, 27.0 / 255, 46.0 / 255}
	goCyan = rgb{0.0, 173.0 / 255, 216.0 / 255}
	teal   = rgb{41.0 / 255, 211.0 / 255, 162.0 / 255}
)

// palette 返回方案使用的底色与渐变端点。
func (st style) palette() (bg, from, to rgb) {
	bg, from, to = navy, goCyan, teal
	if st.bg != (rgb{}) {
		bg = st.bg
	}
	if st.from != (rgb{}) {
		from = st.from
	}
	if st.to != (rgb{}) {
		to = st.to
	}
	return bg, from, to
}

// mix 按比例混合两端颜色。
func mix(from, to rgb, t float64) rgb {
	return rgb{from.r + (to.r-from.r)*t, from.g + (to.g-from.g)*t, from.b + (to.b-from.b)*t}
}

// tone 返回该点在对角渐变中的颜色。
func (st style) tone(p point) rgb {
	_, from, to := st.palette()
	t := (p.x + p.y - 14) / 56
	return mix(from, to, math.Max(0, math.Min(1, t)))
}

// polar 返回圆上指定角度的点，角度按数学方向、屏幕 y 轴向下。
func polar(c point, radius, deg float64) point {
	rad := deg * math.Pi / 180
	return point{c.x + radius*math.Cos(rad), c.y - radius*math.Sin(rad)}
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

// onRing 判断点是否落在圆环的实心弧段上，两端为圆头。
func onRing(p point, r ring) bool {
	if d := math.Hypot(p.x-r.center.x, p.y-r.center.y); math.Abs(d-r.radius) > r.width/2 {
		return false
	}

	deg := math.Atan2(r.center.y-p.y, p.x-r.center.x) * 180 / math.Pi
	if deg < 0 {
		deg += 360
	}

	if deg >= r.from && deg <= r.to {
		return true
	}

	// 圆头端点。
	for _, end := range []float64{r.from, r.to} {
		head := polar(r.center, r.radius, end)
		if math.Hypot(p.x-head.x, p.y-head.y) <= r.width/2 {
			return true
		}
	}

	return false
}

// hit 判断点是否落在字母笔画内。
func (st style) hit(p point) bool {
	for _, item := range st.strokes {
		if polyDistance(p, item.path) <= item.width/2 {
			return true
		}
	}

	for _, item := range st.rings {
		if onRing(p, item) {
			return true
		}
	}

	for _, item := range st.disks {
		if math.Hypot(p.x-item.center.x, p.y-item.center.y) <= item.radius {
			return true
		}
	}

	return false
}

// sample 返回逻辑坐标点的颜色与不透明度。
func sample(st style, p point) (rgb, float64) {
	bg, _, _ := st.palette()

	if st.circle {
		switch d := math.Hypot(p.x-24, p.y-24); {
		case d > 23.5:
			return rgb{}, 0
		case d > 21.5:
			return st.tone(p), 1
		}
	} else {
		if !inRoundRect(p, 1.5, 1.5, 46.5, 46.5, 13) {
			return rgb{}, 0
		}

		if !inRoundRect(p, 3.5, 3.5, 44.5, 44.5, 11) {
			return st.tone(p), 1
		}
	}

	if st.inverted {
		if st.hit(p) {
			return bg, 1
		}
		return st.tone(p), 1
	}

	for _, item := range st.bars {
		if polyDistance(p, item.path) <= item.width/2 {
			ink, alpha := item.ink, item.alpha
			if ink == (rgb{}) {
				ink = st.tone(p)
			}
			if alpha == 0 {
				alpha = 1
			}
			return ink, alpha
		}
	}

	for _, item := range st.window {
		if math.Hypot(p.x-item.center.x, p.y-item.center.y) <= item.radius {
			return item.ink, item.alpha
		}
	}

	for _, item := range st.strokes {
		if polyDistance(p, item.path) <= item.width/2 {
			if item.solid {
				return item.ink, 1
			}
			return st.tone(p), 1
		}
	}

	for _, item := range st.rings {
		if onRing(p, item) {
			return st.tone(p), 1
		}
	}

	for _, item := range st.disks {
		if math.Hypot(p.x-item.center.x, p.y-item.center.y) <= item.radius {
			return item.ink, item.alpha
		}
	}

	return bg, 1
}

// render 逐像素超采样生成图像。
func render(st style) *image.NRGBA {
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

					c, a := sample(st, point{px, py})
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
// 颜色按 alpha 加权后再除总 alpha，避免透明边缘在平均值里被“提亮”。
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

// writePNG 渲染并保存一张品牌图。
func writePNG(name string, st style) error {
	file, err := os.Create(name)
	if err != nil {
		return err
	}

	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err = encoder.Encode(file, downsample(render(st))); err != nil {
		_ = file.Close()
		return err
	}

	return file.Close()
}

// classicStyle 返回原版标识：圆角方 + 字形 L + 青绿箭头 + 句点。
//
// 单文件模式用它生成 docs/assets/logo.png，保证 README 不出现空图。
func classicStyle() style {
	bg := rgb{16.0 / 255, 23.0 / 255, 51.0 / 255}
	from := rgb{91.0 / 255, 140.0 / 255, 255.0 / 255}

	path := []point{{18, 13}, {18, 30}}
	for i := 0; i <= 12; i++ {
		theta := math.Pi - float64(i)*math.Pi/24
		path = append(path, point{23 + 5*math.Cos(theta), 30 + 5*math.Sin(theta)})
	}
	path = append(path, point{30, 35})

	return style{
		name: "classic",
		bg:   bg,
		from: from,
		to:   teal,
		strokes: []stroke{
			{path: path, width: 4.5},
			{path: []point{{30, 15}, {35, 19}, {30, 23}}, width: 3.5, ink: teal, solid: true},
		},
		disks: []disk{{center: point{33, 31}, radius: 2.6, ink: from, alpha: 1}},
	}
}

// candidateStyles 返回待选的 "lee" 品牌图方案。
//
// 六个方案共用同一套字形骨架（主笔 3.4、e 环半径 5.5、环宽 2.8），
// 只在外框与装饰上分叉，便于同一尺寸下横向比较。
func candidateStyles() []style {
	round := lowerStyle("round", 0)

	invert := lowerStyle("invert", 0)
	invert.inverted = true

	// window：顶部窗口栏模拟浏览器标题栏，字母整体下移让出空间。
	window := lowerStyle("window", 2.6)
	window.bars = []bar{{path: []point{{6.4, 9.4}, {41.6, 9.4}}, width: 1, alpha: 0.28, ink: goCyan}}
	window.window = []disk{
		{center: point{10.6, 6.6}, radius: 1, ink: goCyan, alpha: 0.55},
		{center: point{13.9, 6.6}, radius: 1, ink: teal, alpha: 0.55},
		{center: point{17.2, 6.6}, radius: 1, ink: teal, alpha: 0.4},
	}

	// badge：圆形徽章，字母微微上移贴合圆心。
	badge := lowerStyle("badge", -0.3)
	badge.circle = true

	// rule：字标下方压一条渐变基线，像编辑器里的下划线。
	rule := lowerStyle("rule", -2.4)
	rule.bars = []bar{{path: []point{{11, 36.6}, {37, 36.6}}, width: 2}}

	return []style{round, capStyle(), invert, window, badge, rule}
}

// lowerStyle 返回小写 lee 方案，dy 为整体纵向位移。
//
// 基线固定在 33.2；e 环半径 5.5、环宽 2.8，环外径 6.9，
// 环心落在基线减 6.9 处，开口统一朝右下。两个 e 之间保 1.8 的字距，
// 与主笔宽度接近，小尺寸下才不粘成一条。
func lowerStyle(name string, dy float64) style {
	const baseline = 33.2

	cy := baseline + dy - 6.9
	left, right := 18.5, 34.1

	return style{
		name: name,
		strokes: []stroke{
			{path: []point{{8.5, 16.2 + dy}, {8.5, baseline + dy}}, width: 3.4},
			{path: []point{{left - 5.5, cy}, {left + 5.5, cy}}, width: 2.4},
			{path: []point{{right - 5.5, cy}, {right + 5.5, cy}}, width: 2.4},
		},
		rings: []ring{
			{center: point{left, cy}, radius: 5.5, width: 2.8, from: 0, to: 310},
			{center: point{right, cy}, radius: 5.5, width: 2.8, from: 0, to: 310},
		},
	}
}

// capStyle 返回大小写混排的 Lee：大写 L 占满字高，两个 e 回到 x 高。
func capStyle() style {
	const baseline = 33.2
	const xHeight = 11.8 // e 的环外径

	cy := baseline - xHeight/2
	left, right := 21.8, 35.3

	return style{
		name: "cap",
		strokes: []stroke{
			{path: []point{{8, 15.1}, {8, baseline - 1.7}, {13, baseline - 1.7}}, width: 3.4},
			{path: []point{{left - 4.6, cy}, {left + 4.6, cy}}, width: 2.4},
			{path: []point{{right - 4.6, cy}, {right + 4.6, cy}}, width: 2.4},
		},
		rings: []ring{
			{center: point{left, cy}, radius: 4.6, width: 2.6, from: 0, to: 310},
			{center: point{right, cy}, radius: 4.6, width: 2.6, from: 0, to: 310},
		},
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: genlogo <output-dir> | <file.png>")
		os.Exit(2)
	}

	target := os.Args[1]
	if strings.HasSuffix(strings.ToLower(target), ".png") {
		if err := writePNG(target, classicStyle()); err != nil {
			fmt.Fprintf(os.Stderr, "write %s: %v\n", target, err)
			os.Exit(1)
		}
		fmt.Println("written:", target)
		return
	}

	if err := os.MkdirAll(target, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "create dir: %v\n", err)
		os.Exit(1)
	}

	for _, st := range candidateStyles() {
		name := filepath.Join(target, "lee-"+st.name+".png")
		if err := writePNG(name, st); err != nil {
			fmt.Fprintf(os.Stderr, "write %s: %v\n", name, err)
			os.Exit(1)
		}
		fmt.Println("written:", name)
	}
}
