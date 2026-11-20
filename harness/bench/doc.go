// Package bench 汇总 leego 的基准测试。
//
// 基准分三层，避免把不同性质的开销混在一起解读：
//
//   - 引擎层：路由匹配、参数解析、序列化、中间件链，全部走真实的 ServeHTTP；
//   - 组件层：响应写入器、采集器、指标存储、配置、校验、网关、指标后台；
//   - 端到端：真实监听器配合压测客户端，统计吞吐与延迟分位。
//
// 运行方式：
//
//	go test ./bench -run '^$' -bench . -benchmem -benchtime=1s -count=3
//	go test ./bench -run TestEndToEndLoad -v -count=3
//
// 微基准用自带的 discardWriter 替代 httptest.ResponseRecorder，避免测试工具
// 自身的分配掩盖框架开销；端到端测试才引入网络与真实客户端。
//
// 端到端压测在客户端与服务器同进程的条件下运行，QPS 只作容量参考；
// 延迟分位还受平台时钟粒度限制，两者都已在 docs/performance.md 中标注。
//
// 结果解读、环境说明与优化建议见 docs/performance.md。
package bench
