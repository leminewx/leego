package leego

import "path"

func newTestGroup(engine *Engine, parent *group, prefix string) *group {
	g := &group{engine: engine, parent: parent, middlewares: make([]MiddlewareFunc, 0, 10)}

	if parent != nil {
		g.prefix = path.Join(parent.prefix, prefix)
	} else {
		g.prefix = prefix
	}

	return g
}
