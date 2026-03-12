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

func mockGroup(engine *Engine, parent *group, prefix string) *group {
	g := &group{engine: engine, parent: parent, middlewares: make([]MiddlewareFunc, 0, 10)}

	if parent != nil {
		g.prefix = path.Join(parent.prefix, prefix)
	} else {
		g.prefix = prefix
	}

	return g
}
