package router

// Route 定义路由的数据结构
type Route[T any] struct {
	Pattern string   // 路由模式
	Handler T        // 处理函数
	Names   []string // 参数名列表
}
