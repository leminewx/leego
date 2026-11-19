package stress

import (
	"testing"
	"time"
)

// TestNewLatenciesWithCapacityIgnoresNonPositive 验证非正容量退化为 0。
func TestNewLatenciesWithCapacityIgnoresNonPositive(t *testing.T) {
	collector := NewLatenciesWithCapacity(-10)
	if collector.GetLen() != 0 {
		t.Fatalf("初始样本数 = %d, want 0", collector.GetLen())
	}

	collector.Append(time.Millisecond)
	if collector.GetLen() != 1 {
		t.Fatalf("追加后样本数 = %d, want 1", collector.GetLen())
	}
}

// TestLatenciesMergeNilAndEmpty 验证空来源合并不改变样本。
func TestLatenciesMergeNilAndEmpty(t *testing.T) {
	collector := NewLatencies()
	collector.Merge(nil)
	collector.Merge(NewLatencies())

	if collector.GetLen() != 0 {
		t.Fatalf("空合并后样本数 = %d, want 0", collector.GetLen())
	}

	other := NewLatencies()
	other.Append(time.Millisecond)
	other.Append(2 * time.Millisecond)

	collector.Merge(other)
	if collector.GetLen() != 2 {
		t.Fatalf("合并后样本数 = %d, want 2", collector.GetLen())
	}
}

// TestLatenciesSortIsIdempotent 验证重复排序不会改变顺序。
func TestLatenciesSortIsIdempotent(t *testing.T) {
	collector := NewLatencies()
	collector.Append(3 * time.Millisecond)
	collector.Append(time.Millisecond)
	collector.Append(2 * time.Millisecond)

	collector.Sort()
	first := collector.GetLatencies()

	collector.Sort()
	second := collector.GetLatencies()

	for index := range first {
		if first[index] != second[index] {
			t.Fatalf("重复排序改变顺序: %v vs %v", first, second)
		}
	}

	if first[0] != time.Millisecond || first[2] != 3*time.Millisecond {
		t.Fatalf("排序结果异常: %v", first)
	}
}
