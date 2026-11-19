package leego

// Validatable 定义可在绑定后自动校验的请求对象。
//
// 语义约定：
//   - Bind、BindJSON、BindXML、BindForm 在解码成功后若发现目标实现了本接口，
//     会立即调用 Validate；
//   - 校验失败直接返回错误，是否为客户端错误由业务层决定，框架不做状态码推断；
//   - 未实现本接口的对象行为与旧版本完全一致，不产生额外开销。
//
// 典型用法是让 DTO 转发到 validate 包：
//
//	func (own *CreateUser) Validate() error { return validate.Check(own) }
type Validatable interface {
	// Validate 校验对象是否合法；返回 nil 表示通过。
	Validate() error
}

// validateBound 在绑定成功后触发可选校验。
func validateBound(object any) error {
	if validatable, ok := object.(Validatable); ok {
		return validatable.Validate()
	}

	return nil
}
