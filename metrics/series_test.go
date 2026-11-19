package metrics

import (
	"testing"
	"time"

	"gitee.com/leminewx/leego/middleware/collector"
)

// TestNewTimeSeriesFallbacks 验证非法窗口与桶数会回退为默认值。
func TestNewTimeSeriesFallbacks(t *testing.T) {
	series := NewTimeSeries(0, 0)
	if len(series.buckets) != _DEFAULT_BUCKETS {
		t.Fatalf("桶数 = %d, want %d", len(series.buckets), _DEFAULT_BUCKETS)
	}

	// 默认窗口为 1 分钟，均分到 60 个桶后桶宽恰为 1 秒。
	if width := series.GetWidth(); width != time.Second {
		t.Fatalf("桶宽 = %s, want 1s", width)
	}

	narrow := NewTimeSeries(time.Nanosecond, 10)
	if width := narrow.GetWidth(); width != _MIN_BUCKET_WIDTH {
		t.Fatalf("桶宽 = %s, want %s", width, _MIN_BUCKET_WIDTH)
	}
}

// TestAppendPointSkipsZeroTime 验证零值时间点被丢弃，不会污染首个时间片。
func TestAppendPointSkipsZeroTime(t *testing.T) {
	series := NewTimeSeries(time.Second, 5)
	series.AppendPoint(Point{})

	points := series.Query(time.Time{}, time.Now(), time.Second)
	if len(points) != 0 {
		t.Fatalf("零值时间点应被忽略: %+v", points)
	}
}

// TestAppendSnapshotComputesAggregates 验证快照写入后的平均与最大耗时。
func TestAppendSnapshotComputesAggregates(t *testing.T) {
	series := NewTimeSeries(time.Minute, 60)
	at := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)

	snapshot := collector.Snapshot{
		Total:          2,
		TotalCostMicro: 600,
		Metrics: []collector.Metric{
			{Code: 200, Total: 1, MaxCostMicro: 100},
			{Code: 201, Total: 1, MaxCostMicro: 400},
		},
	}
	series.Append(at, snapshot)

	points := series.Query(at.Add(-time.Minute), at.Add(time.Minute), time.Second)
	if len(points) != 1 {
		t.Fatalf("时间片数量 = %d, want 1: %+v", len(points), points)
	}

	point := points[0]
	if point.Total != 2 || point.AvgMicro != 300 || point.MaxMicro != 400 {
		t.Fatalf("聚合结果异常: %+v", point)
	}
}

// TestQueryMergesBucketsByStep 验证相邻时间片按 step 合并并重新计算平均耗时。
func TestQueryMergesBucketsByStep(t *testing.T) {
	series := NewTimeSeries(time.Hour, 3600)
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)

	series.AppendPoint(Point{At: base, Total: 1, AvgMicro: 100})
	series.AppendPoint(Point{At: base.Add(time.Second), Total: 2, AvgMicro: 200})

	points := series.Query(base, base.Add(10*time.Second), time.Minute)
	if len(points) != 1 {
		t.Fatalf("合并后时间片数量 = %d, want 1: %+v", len(points), points)
	}

	// (100*1 + 200*2) / 3 = 166。
	if points[0].Total != 3 || points[0].AvgMicro != 166 {
		t.Fatalf("合并结果异常: %+v", points[0])
	}
}

// TestQueryEmptyIsNonNil 验证空查询返回非 nil 切片，便于直接序列化。
func TestQueryEmptyIsNonNil(t *testing.T) {
	series := NewTimeSeries(time.Minute, 60)
	now := time.Now()

	points := series.Query(now, now, time.Second)
	if points == nil {
		t.Fatal("Query 应返回非 nil 切片")
	}

	if len(points) != 0 {
		t.Fatalf("空查询不应有数据: %+v", points)
	}
}
