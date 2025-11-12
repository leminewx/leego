package leego

import (
	"path"
	"testing"
)

func TestGroup_UseMiddlewareFuncs(t *testing.T) {
	group := mockGroup(NewEngine(), nil, "/")
	group.UseMiddlewareFuncs(func(next HandleFunc) HandleFunc {
		t.Log("init middleware 1")
		return func(ctx *Context) {
			next(ctx)
		}
	}, func(next HandleFunc) HandleFunc {
		t.Log("init middleware 2")
		return func(ctx *Context) {
			next(ctx)
		}
	})

	group.UseMiddlewareFuncs(func(next HandleFunc) HandleFunc {
		t.Log("init middleware 3")
		return func(ctx *Context) {
			next(ctx)
		}
	}, func(next HandleFunc) HandleFunc {
		t.Log("init middleware 4")
		return func(ctx *Context) {
			next(ctx)
		}
	})
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
