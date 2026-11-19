package testutil

import (
	"testing"
	"time"
)

// TestNewFrozenClockZeroUsesEpoch 验证零值时间回退到 Unix 纪元。
func TestNewFrozenClockZeroUsesEpoch(t *testing.T) {
	clock := NewFrozenClock(time.Time{})
	if !clock.Now().Equal(time.Unix(0, 0).UTC()) {
		t.Fatalf("零值时钟 = %s, want Unix 纪元", clock.Now())
	}
}

// TestFrozenClockAdvanceAndSet 验证时间推进、回退与直接设置。
func TestFrozenClockAdvanceAndSet(t *testing.T) {
	start := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	clock := NewFrozenClock(start)

	clock.Advance(time.Minute)
	if !clock.Now().Equal(start.Add(time.Minute)) {
		t.Fatalf("推进后时间 = %s", clock.Now())
	}

	clock.Advance(-30 * time.Second)
	if !clock.Now().Equal(start.Add(30 * time.Second)) {
		t.Fatalf("回退后时间 = %s", clock.Now())
	}

	next := start.Add(time.Hour)
	clock.Set(next)
	if !clock.Now().Equal(next) {
		t.Fatalf("设置后时间 = %s", clock.Now())
	}
}

// TestFrozenClockSetIgnoresZero 验证零值时间被忽略，保证 Now 永远可用。
func TestFrozenClockSetIgnoresZero(t *testing.T) {
	start := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	clock := NewFrozenClock(start)

	clock.Set(time.Time{})
	if !clock.Now().Equal(start) {
		t.Fatalf("零值设置不应改变时间: %s", clock.Now())
	}
}
