package leego

type API struct {
	// Service 服务名称
	Service string `json:"service" xml:"service"`
	// Type 接口类型
	Type string `json:"type" xml:"type"`
	// Name 接口名称
	Name string `json:"name" xml:"name"`
	// Method 接口方法
	Method string `json:"method" xml:"method"`
	// Pattern 接口模式
	Pattern string `json:"pattern" xml:"pattern"`
}
