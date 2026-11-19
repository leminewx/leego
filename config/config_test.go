package config_test

import (
	"flag"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"gitee.com/leminewx/leego/config"
)

// writeJSON 写入临时 JSON 配置文件。
func writeJSON(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
}

func TestLoadPriority(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeJSON(t, path, `{
		"Server": {"Port": 9000, "read_timeout": "5s"},
		"name": "from-file"
	}`)

	t.Setenv("LEEGO_TEST_SERVER_PORT", "9100")

	set := flag.NewFlagSet("test", flag.ContinueOnError)
	set.String("name", "default", "服务名")
	if err := set.Parse([]string{"-name=from-cli"}); err != nil {
		t.Fatalf("解析参数失败: %v", err)
	}

	instance, err := config.Load(
		config.NewMapSource(map[string]any{
			"server.port":         8080,
			"server.read_timeout": "1s",
			"flag_only":           "keep",
		}),
		config.NewJSONSource(path),
		config.NewEnvSource("LEEGO_TEST_"),
		config.NewFlagSource(set),
	)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}

	if got := instance.GetInt("server.port", 0); got != 9100 {
		t.Fatalf("server.port = %d, 期望 9100（环境变量应覆盖文件）", got)
	}

	if got := instance.GetString("name", ""); got != "from-cli" {
		t.Fatalf("name = %q, 期望来自命令行", got)
	}

	if got := instance.GetDuration("server.read_timeout", 0); got != 5*time.Second {
		t.Fatalf("server.read_timeout = %v, 期望 5s", got)
	}

	if got := instance.GetString("flag_only", ""); got != "keep" {
		t.Fatal("未被子类来源覆盖的默认值应保留")
	}
}

func TestGetFallbacks(t *testing.T) {
	instance, err := config.Load(config.NewMapSource(nil))
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}

	if instance.GetString("missing", "fallback") != "fallback" {
		t.Fatal("字符串回退失效")
	}

	if instance.GetInt("missing", 7) != 7 {
		t.Fatal("整数回退失效")
	}

	if !instance.GetBool("missing", true) {
		t.Fatal("布尔回退失效")
	}

	if instance.GetFloat64("missing", 1.5) != 1.5 {
		t.Fatal("浮点回退失效")
	}

	if instance.GetDuration("missing", time.Minute) != time.Minute {
		t.Fatal("时长回退失效")
	}

	if len(instance.GetSnapshot()) != 0 {
		t.Fatal("空配置快照应为空")
	}
}

type bindTarget struct {
	Host     string        `config:"server.host" default:"127.0.0.1"`
	Port     int           `config:"server.port"`
	Secure   bool          `config:"server.secure"`
	Ratio    float64       `config:"server.ratio"`
	Timeout  time.Duration `config:"server.timeout"`
	Retries  uint          `config:"server.retries"`
	Origins  []string      `config:"server.origins"`
	Name     *string       `config:"server.name"`
	Skipped  string
	internal string
}

func TestBind(t *testing.T) {
	name := "leego"
	instance, err := config.Load(config.NewMapSource(map[string]any{
		"server.host":    "0.0.0.0",
		"server.port":    8080,
		"server.secure":  "true",
		"server.ratio":   "0.75",
		"server.timeout": "3s",
		"server.retries": 5,
		"server.origins": "a.example.com, b.example.com",
		"server.name":    name,
	}))
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}

	var target bindTarget
	if err = instance.Bind(&target); err != nil {
		t.Fatalf("绑定失败: %v", err)
	}

	if target.Host != "0.0.0.0" || target.Port != 8080 || !target.Secure {
		t.Fatalf("基础字段绑定错误: %+v", target)
	}

	if target.Ratio != 0.75 || target.Timeout != 3*time.Second || target.Retries != 5 {
		t.Fatalf("数值或时长绑定错误: %+v", target)
	}

	if len(target.Origins) != 2 || target.Origins[1] != "b.example.com" {
		t.Fatalf("切片绑定错误: %+v", target.Origins)
	}

	if target.Name == nil || *target.Name != name {
		t.Fatalf("指针绑定错误: %v", target.Name)
	}

	if err = instance.Bind(nil); err == nil {
		t.Fatal("nil 目标应报错")
	}

	var notPointer bindTarget
	if err = instance.Bind(notPointer); err == nil {
		t.Fatal("非指针目标应报错")
	}
}

func TestBindDefaultTag(t *testing.T) {
	instance, err := config.Load(config.NewMapSource(nil))
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}

	var target bindTarget
	if err = instance.Bind(&target); err != nil {
		t.Fatalf("绑定失败: %v", err)
	}

	if target.Host != "127.0.0.1" {
		t.Fatalf("Host = %q, 期望使用 default 标签", target.Host)
	}
}

func TestWatchFileUpdatesSubscription(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeJSON(t, path, `{"server": {"port": 8080, "name": "before"}}`)

	instance, err := config.Load(config.NewJSONSource(path, config.UseWatchInterval(10*time.Millisecond)))
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}

	portChanged := make(chan any, 4)
	nameChanged := make(chan any, 4)
	cancelPort := instance.SubscribeChange("server.port", func(value any) { portChanged <- value })
	defer cancelPort()
	instance.SubscribeChange("server.name", func(value any) { nameChanged <- value })

	if err = instance.Watch(10 * time.Millisecond); err != nil {
		t.Fatalf("启动监听失败: %v", err)
	}

	// 直接改写同一文件，模拟配置下发。
	writeJSON(t, path, `{"server": {"port": 9090, "name": "before"}}`)

	select {
	case value := <-portChanged:
		if number, _ := value.(float64); number != 9090 {
			t.Fatalf("回调值 = %v, 期望 9090", value)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("等待端口变更超时")
	}

	select {
	case value := <-nameChanged:
		t.Fatalf("未变化的 key 不应触发回调: %v", value)
	case <-time.After(100 * time.Millisecond):
	}

	if got := instance.GetInt("server.port", 0); got != 9090 {
		t.Fatalf("快照未更新: %d", got)
	}

	if err = instance.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}

	// 关闭后再次修改不应再触发回调。
	writeJSON(t, path, `{"server": {"port": 7070}}`)
	select {
	case value := <-portChanged:
		t.Fatalf("关闭后仍收到回调: %v", value)
	case <-time.After(150 * time.Millisecond):
	}

	if err = instance.Close(); err != nil {
		t.Fatalf("重复关闭应幂等: %v", err)
	}
}

func TestSubscribeChangeCancel(t *testing.T) {
	instance, err := config.Load(config.NewMapSource(nil))
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}

	var count atomic.Int32
	cancel := instance.SubscribeChange("server.port", func(any) { count.Add(1) })
	cancel()
	cancel() // 幂等

	if err = instance.Watch(10 * time.Millisecond); err != nil {
		t.Fatalf("启动监听失败: %v", err)
	}

	instance.SubscribeChange("", nil)
	if err = instance.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}

	if count.Load() != 0 {
		t.Fatal("取消后不应触发回调")
	}
}

func TestSubscribeChangeCancelDoesNotAffectSibling(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeJSON(t, path, `{"server": {"port": 8080}}`)

	instance, err := config.Load(config.NewJSONSource(path, config.UseWatchInterval(10*time.Millisecond)))
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}

	// 两个订阅来自同一个函数字面量：闭包共享代码地址，
	// 旧实现按函数指针取消会把兄弟订阅一起删掉。
	cancelled := make(chan any, 4)
	kept := make(chan any, 4)
	subscribe := func(target chan any) func() {
		return instance.SubscribeChange("server.port", func(value any) { target <- value })
	}

	cancelFirst := subscribe(cancelled)
	subscribe(kept)
	cancelFirst()

	if err = instance.Watch(10 * time.Millisecond); err != nil {
		t.Fatalf("启动监听失败: %v", err)
	}

	writeJSON(t, path, `{"server": {"port": 9091}}`)

	select {
	case value := <-kept:
		if number, _ := value.(float64); number != 9091 {
			t.Fatalf("回调值 = %v, 期望 9091", value)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("取消一个订阅后，兄弟订阅必须继续生效")
	}

	select {
	case value := <-cancelled:
		t.Fatalf("已取消的订阅不应触发: %v", value)
	case <-time.After(100 * time.Millisecond):
	}

	if err = instance.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
}

func TestJSONSourceInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	writeJSON(t, path, `{not json}`)

	if _, err := config.Load(config.NewJSONSource(path)); err == nil {
		t.Fatal("非法 JSON 应返回错误")
	}

	if _, err := config.Load(config.NewJSONSource(filepath.Join(t.TempDir(), "missing.json"))); err == nil {
		t.Fatal("文件不存在应返回错误")
	}
}

func TestFlagSourceOnlySetFlags(t *testing.T) {
	set := flag.NewFlagSet("test", flag.ContinueOnError)
	set.Int("port", 8080, "端口")
	if err := set.Parse([]string{"-port=9000"}); err != nil {
		t.Fatalf("解析参数失败: %v", err)
	}

	values, err := config.NewFlagSource(set).Load()
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}

	if values["port"] != "9000" {
		t.Fatalf("port = %v", values["port"])
	}

	if _, err = config.NewFlagSource(nil).Load(); err == nil {
		t.Fatal("nil flag set 应报错")
	}
}

// TestWatchZeroIntervalKeepsSourceOption 验证 Watch(0) 保留来源自身的轮询间隔。
func TestWatchZeroIntervalKeepsSourceOption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeJSON(t, path, `{"server": {"port": 8080}}`)

	instance, err := config.Load(config.NewJSONSource(path, config.UseWatchInterval(5*time.Millisecond)))
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	defer instance.Close()

	changed := make(chan any, 1)
	instance.SubscribeChange("server.port", func(value any) { changed <- value })

	if err = instance.Watch(0); err != nil {
		t.Fatalf("启动监听失败: %v", err)
	}

	writeJSON(t, path, `{"server": {"port": 9090}}`)

	select {
	case value := <-changed:
		if number, _ := value.(float64); number != 9090 {
			t.Fatalf("回调值 = %v, 期待 9090", value)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Watch(0) 应保留来源配置的 5ms 间隔")
	}
}

// TestWatchIgnoresFormattingOnlyChange 验证仅调整空白时不触发回调，真实变化仍可检测。
func TestWatchIgnoresFormattingOnlyChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeJSON(t, path, `{"server":{"port":8080}}`)

	instance, err := config.Load(config.NewJSONSource(path))
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	defer instance.Close()

	changed := make(chan any, 4)
	instance.SubscribeChange("server.port", func(value any) { changed <- value })

	if err = instance.Watch(5 * time.Millisecond); err != nil {
		t.Fatalf("启动监听失败: %v", err)
	}

	// 仅空白不同，内容语义一致，不应触发回调。
	writeJSON(t, path, "{ \"server\" : { \"port\" : 8080 } }\n")
	select {
	case value := <-changed:
		t.Fatalf("纯格式变化不应触发回调: %v", value)
	case <-time.After(100 * time.Millisecond):
	}

	// 真实变化应继续被检测到，证明监听未因忽略格式而被阻断。
	writeJSON(t, path, `{"server":{"port":9090}}`)
	select {
	case value := <-changed:
		if number, _ := value.(float64); number != 9090 {
			t.Fatalf("回调值 = %v, 期待 9090", value)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("真实变化未触发回调")
	}
}
