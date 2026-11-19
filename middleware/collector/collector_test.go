package collector

import (
	"sync"
	"testing"
	"time"
)

// TestNewCollector 验证采集器的元信息与默认状态。
func TestNewCollector(t *testing.T) {
	monitor := NewCollector("GET", "/users/:id", " 获取用户 ", " v1 ", "", "auth")

	if monitor.GetMethod() != "GET" {
		t.Fatalf("method = %q, want GET", monitor.GetMethod())
	}

	if monitor.GetPattern() != "/users/:id" {
		t.Fatalf("pattern = %q, want /users/:id", monitor.GetPattern())
	}

	if monitor.GetName() != "获取用户" {
		t.Fatalf("name = %q, want 获取用户（应去除首尾空白）", monitor.GetName())
	}

	tags := monitor.GetTags()
	if len(tags) != 2 || tags[0] != "v1" || tags[1] != "auth" {
		t.Fatalf("tags = %v, want [v1 auth]（空标签应被过滤）", tags)
	}

	if !monitor.IsEnabled() {
		t.Fatal("新建采集器默认必须处于启用状态")
	}

	if snapshot := monitor.GetSnapshot(); snapshot.Total != 0 || snapshot.Failed != 0 {
		t.Fatalf("初始快照 = %+v, want 全零", snapshot)
	}
}

// TestNewCollectorWithoutTags 验证没有标签时的默认行为。
func TestNewCollectorWithoutTags(t *testing.T) {
	monitor := NewCollector("POST", "/orders")

	if monitor.GetName() != "" {
		t.Fatalf("name = %q, want 空字符串", monitor.GetName())
	}

	if monitor.GetTags() != nil {
		t.Fatalf("tags = %v, want nil", monitor.GetTags())
	}
}

// TestNewCollectorLimitsTags 验证标签数量上限。
func TestNewCollectorLimitsTags(t *testing.T) {
	monitor := NewCollector("GET", "/limited", "name", "t1", "t2", "t3", "t4", "t5", "t6", "t7")

	if len(monitor.GetTags()) != _MAX_TAG_COUNT {
		t.Fatalf("tags 数量 = %d, want %d", len(monitor.GetTags()), _MAX_TAG_COUNT)
	}
}

// TestEnableAndDisable 验证启停状态切换。
func TestEnableAndDisable(t *testing.T) {
	monitor := NewCollector("GET", "/toggle")

	monitor.Disable()
	if monitor.IsEnabled() {
		t.Fatal("Disable 后 IsEnabled 必须为 false")
	}

	monitor.Enable()
	if !monitor.IsEnabled() {
		t.Fatal("Enable 后 IsEnabled 必须为 true")
	}
}

// TestIncreaseAndDecrease 验证在途计数与状态码统计。
func TestIncreaseAndDecrease(t *testing.T) {
	monitor := NewCollector("GET", "/decrease")

	monitor.Increase()
	monitor.Increase()

	if snapshot := monitor.GetSnapshot(); snapshot.Flights != 2 {
		t.Fatalf("flies = %d, want 2", snapshot.Flights)
	}

	monitor.Decrease(200, 10*time.Millisecond)
	monitor.Decrease(0, -5*time.Millisecond) // 0 视为 200，负耗时裁剪为 0
	monitor.Decrease(500, 30*time.Millisecond)

	snapshot := monitor.GetSnapshot()

	if snapshot.Flights != -1 {
		t.Fatalf("flights = %d, want -1（Decrease 次数多于 Increase）", snapshot.Flights)
	}

	if snapshot.Total != 3 {
		t.Fatalf("total = %d, want 3", snapshot.Total)
	}

	if snapshot.Failed != 1 {
		t.Fatalf("failed = %d, want 1", snapshot.Failed)
	}

	if len(snapshot.Metrics) != 2 {
		t.Fatalf("metrics 数量 = %d, want 2（200 与 500）", len(snapshot.Metrics))
	}

	if snapshot.Metrics[0].Code != 200 || snapshot.Metrics[0].Total != 2 {
		t.Fatalf("第一个指标 = %+v, want code=200 total=2", snapshot.Metrics[0])
	}

	if snapshot.Metrics[1].Code != 500 || snapshot.Metrics[1].Total != 1 {
		t.Fatalf("第二个指标 = %+v, want code=500 total=1", snapshot.Metrics[1])
	}

	if snapshot.Metrics[0].MinCostMicro != 0 {
		t.Fatalf("min cost = %d, want 0（负耗时被裁剪为 0）", snapshot.Metrics[0].MinCostMicro)
	}

	if snapshot.Metrics[0].MaxCostMicro != 10000 {
		t.Fatalf("max cost = %d, want 10000", snapshot.Metrics[0].MaxCostMicro)
	}
}

// TestSnapshotAverageAndFailureRate 验证派生指标的计算。
func TestSnapshotAverageAndFailureRate(t *testing.T) {
	monitor := NewCollector("GET", "/derived")

	monitor.Increase()
	monitor.Decrease(200, 10*time.Millisecond)
	monitor.Increase()
	monitor.Decrease(503, 30*time.Millisecond)

	snapshot := monitor.GetSnapshot()

	if got := snapshot.GetAverageCostMicro(); got != 20000 {
		t.Fatalf("平均耗时 = %d, want 20000", got)
	}

	if got := snapshot.GetFailureRate(); got != 0.5 {
		t.Fatalf("失败率 = %v, want 0.5", got)
	}

	if got := snapshot.Metrics[0].GetAverageCostMicro(); got != 10000 {
		t.Fatalf("状态码 200 平均耗时 = %d, want 10000", got)
	}

	empty := Snapshot{}
	if empty.GetAverageCostMicro() != 0 || empty.GetFailureRate() != 0 {
		t.Fatal("空快照的派生指标必须为 0，避免除零")
	}
}

// TestReset 验证重置统计但保留元信息。
func TestReset(t *testing.T) {
	monitor := NewCollector("GET", "/reset", "reset-name", "tag")
	monitor.Increase()
	monitor.Decrease(200, time.Millisecond)
	monitor.Disable()

	monitor.Reset()

	snapshot := monitor.GetSnapshot()
	if snapshot.Total != 0 || snapshot.Failed != 0 || snapshot.Flights != 0 || len(snapshot.Metrics) != 0 {
		t.Fatalf("Reset 后快照 = %+v, want 全零", snapshot)
	}

	if snapshot.Pattern != "/reset" || snapshot.Name != "reset-name" {
		t.Fatalf("Reset 不应清除元信息，实际 = %+v", snapshot)
	}

	if snapshot.Enabled {
		t.Fatal("Reset 不应改变启停状态")
	}
}

// TestCloneIsIndependent 验证副本与原采集器完全独立。
func TestCloneIsIndependent(t *testing.T) {
	monitor := NewCollector("GET", "/clone", "clone-name")
	monitor.Increase()
	monitor.Decrease(200, time.Millisecond)

	clone := monitor.Clone()

	monitor.Increase()
	monitor.Decrease(500, 2*time.Millisecond)

	cloneSnapshot := clone.GetSnapshot()
	if cloneSnapshot.Total != 1 || cloneSnapshot.Failed != 0 {
		t.Fatalf("副本快照 = %+v, want total=1 failed=0", cloneSnapshot)
	}

	if got := monitor.GetSnapshot().Total; got != 2 {
		t.Fatalf("原采集器 total = %d, want 2", got)
	}
}

// TestGetTagsReturnsCopy 验证标签返回副本，外部修改不影响内部状态。
func TestGetTagsReturnsCopy(t *testing.T) {
	monitor := NewCollector("GET", "/tags", "name", "tag")

	tags := monitor.GetTags()
	tags[0] = "modified"

	if monitor.GetTags()[0] != "tag" {
		t.Fatal("GetTags 必须返回副本")
	}
}

// TestCollectorConcurrency 验证并发采集下计数准确。
func TestCollectorConcurrency(t *testing.T) {
	monitor := NewCollector("GET", "/concurrent")

	const workers = 16
	const iterations = 500

	var waitGroup sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		waitGroup.Add(1)

		go func(worker int) {
			defer waitGroup.Done()

			for index := 0; index < iterations; index++ {
				monitor.Increase()
				code := 200
				if (worker+index)%7 == 0 {
					code = 500
				}

				monitor.Decrease(code, time.Duration(index)*time.Microsecond)
			}
		}(worker)
	}

	waitGroup.Wait()

	snapshot := monitor.GetSnapshot()
	if snapshot.Total != workers*iterations {
		t.Fatalf("total = %d, want %d", snapshot.Total, workers*iterations)
	}

	if snapshot.Flights != 0 {
		t.Fatalf("flights = %d, want 0", snapshot.Flights)
	}

	if snapshot.Metrics[0].Code != 200 || snapshot.Metrics[1].Code != 500 {
		t.Fatalf("指标必须按状态码升序排列，实际 = %+v", snapshot.Metrics)
	}
}

// BenchmarkIncreaseDecrease 测量一次完整采集的开销。
func BenchmarkIncreaseDecrease(b *testing.B) {
	monitor := NewCollector("GET", "/bench")

	b.ReportAllocs()
	b.ResetTimer()

	for index := 0; index < b.N; index++ {
		monitor.Increase()
		monitor.Decrease(200, time.Microsecond)
	}
}

// BenchmarkGetSnapshot 测量快照生成的开销。
func BenchmarkGetSnapshot(b *testing.B) {
	monitor := NewCollector("GET", "/bench")
	for code := 200; code < 210; code++ {
		monitor.Increase()
		monitor.Decrease(code, time.Microsecond)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for index := 0; index < b.N; index++ {
		_ = monitor.GetSnapshot()
	}
}
