package leego

import (
	"path"
	"testing"
)

func TestGroup_UseMiddlewares(t *testing.T) {
	group := mockGroup(NewEngine(), nil, "/")
	group.UseMiddlewares(func(next HandleFunc) HandleFunc {
		t.Log("init middlewares 1")
		return func(ctx *Context) {
			next(ctx)
		}
	}, func(next HandleFunc) HandleFunc {
		t.Log("init middlewares 2")
		return func(ctx *Context) {
			next(ctx)
		}
	})

	group.UseMiddlewares(func(next HandleFunc) HandleFunc {
		t.Log("init middlewares 3")
		return func(ctx *Context) {
			next(ctx)
		}
	}, func(next HandleFunc) HandleFunc {
		t.Log("init middlewares 4")
		return func(ctx *Context) {
			next(ctx)
		}
	})
}

// TestContainsPathTraversal 校验静态文件服务的路径遍历防护
func TestContainsPathTraversal(t *testing.T) {
	blocked := []string{
		"../etc/passwd",           // 直接上跳
		"a/../../etc/passwd",      // 中途上跳
		"..",                      // 纯上跳段
		"a/..",                    // 结尾上跳段
		"..\\..\\windows\\win.ini", // 反斜杠上跳（Windows）
		"/etc/passwd",             // 绝对路径
		"C:/windows/win.ini",      // Windows 盘符
		"//server/share/file",     // UNC 路径
		"a/\x00b",                 // NUL 截断
	}
	for _, p := range blocked {
		if !containsPathTraversal(p) {
			t.Errorf("应被拦截但放行: %q", p)
		}
	}

	allowed := []string{
		"css/style.css",
		"images/photo.jpg",
		"./style.css",      // 旧实现误报
		"a//b.css",         // 旧实现误报
		"..foo/bar.txt",    // 以 .. 开头的合法文件名，旧实现误报
		"foo..bar.css",     // 文件名中含 ..，旧实现误报
		"my.file.name.txt",
	}
	for _, p := range allowed {
		if containsPathTraversal(p) {
			t.Errorf("合法路径被误拦截: %q", p)
		}
	}
}

func mockGroup(engine *Engine, parent *group, prefix string) *group {
	g := &group{engine: engine, parent: parent, middlewares: make([]MiddlewareFunc, 0, 10)}

	if parent != nil {
		g.prefix = path.Join(parent.prefix, prefix)
	} else {
		g.prefix = prefix
	}

	return g
}
