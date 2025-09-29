package metric

// Metadata 定义路由元数据的数据结构
type Metadata struct {
	Name    string `json:"name" xml:"name"`       // 路由名称
	Method  string `json:"method" xml:"method"`   // 路由方法
	Pattern string `json:"pattern" xml:"pattern"` // 路由模式
}
