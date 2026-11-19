package job

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// waitEvent 等待一次任务执行。
func waitEvent(t *testing.T, events <-chan struct{}, timeout time.Duration) {
	t.Helper()

	select {
	case <-events:
	case <-time.After(timeout):
		t.Fatal("等待任务执行超时")
	}
}

// expectNoEvent 断言在给定时间内没有新执行。
func expectNoEvent(t *testing.T, events <-chan struct{}, duration time.Duration) {
	t.Helper()

	select {
	case <-events:
		t.Fatal("不应继续执行任务")
	case <-time.After(duration):
	}
}

// drainEvents 清空已缓冲的事件。
func drainEvents(events chan struct{}) {
	for {
		select {
		case <-events:
		default:
			return
		}
	}
}

func TestEveryRunsRepeatedly(t *testing.T) {
	scheduler := NewScheduler(UseWorkers(4))
	events := make(chan struct{}, 64)

	cancel := scheduler.Every(10*time.Millisecond, JobFunc(func(context.Context) error {
		select {
		case events <- struct{}{}:
		default:
		}
		return nil
	}))

	scheduler.Start(context.Background())
	waitEvent(t, events, 2*time.Second)
	waitEvent(t, events, 2*time.Second)

	cancel()
	time.Sleep(60 * time.Millisecond)
	drainEvents(events)
	expectNoEvent(t, events, 80*time.Millisecond)

	if stats := scheduler.GetStats(); !stats.Started || stats.Stopped {
		t.Fatalf("状态异常: %+v", stats)
	}

	if err := scheduler.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if stats := scheduler.GetStats(); !stats.Stopped || stats.Pending != 0 {
		t.Fatalf("停止后状态异常: %+v", stats)
	}
}

func TestAfterRunsOnce(t *testing.T) {
	scheduler := NewScheduler(UseWorkers(2))
	events := make(chan struct{}, 4)

	scheduler.After(10*time.Millisecond, JobFunc(func(context.Context) error {
		events <- struct{}{}
		return nil
	}))

	scheduler.Start(context.Background())
	waitEvent(t, events, 2*time.Second)
	expectNoEvent(t, events, 80*time.Millisecond)

	if err := scheduler.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if runs := scheduler.GetStats().Runs; runs != 1 {
		t.Fatalf("执行次数 = %d, want 1", runs)
	}
}

func TestCancelBeforeStart(t *testing.T) {
	scheduler := NewScheduler(UseWorkers(2))
	events := make(chan struct{}, 4)

	cancel := scheduler.Every(5*time.Millisecond, JobFunc(func(context.Context) error {
		events <- struct{}{}
		return nil
	}))
	cancel()

	scheduler.Start(context.Background())
	expectNoEvent(t, events, 80*time.Millisecond)

	if err := scheduler.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestStopWaitsForRunningJob(t *testing.T) {
	scheduler := NewScheduler(UseWorkers(2))
	started := make(chan struct{}, 1)
	release := make(chan struct{})

	scheduler.After(0, JobFunc(func(context.Context) error {
		started <- struct{}{}
		<-release
		return nil
	}))

	scheduler.Start(context.Background())
	waitEvent(t, started, 2*time.Second)

	stopped := make(chan error, 1)
	go func() {
		stopped <- scheduler.Stop(context.Background())
	}()

	select {
	case err := <-stopped:
		t.Fatalf("在途任务未结束时 Stop 不应返回: %v", err)
	case <-time.After(40 * time.Millisecond):
	}

	close(release)

	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("Stop() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("等待 Stop 超时")
	}
}

func TestStopReturnsDeadlineExceeded(t *testing.T) {
	scheduler := NewScheduler(UseWorkers(1))
	started := make(chan struct{}, 1)
	release := make(chan struct{})

	scheduler.After(0, JobFunc(func(context.Context) error {
		started <- struct{}{}
		<-release
		return nil
	}))

	scheduler.Start(context.Background())
	waitEvent(t, started, 2*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	err := scheduler.Stop(ctx)
	cancel()

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop() error = %v, want DeadlineExceeded", err)
	}

	close(release)
}

func TestDropsWhenWorkersBusy(t *testing.T) {
	scheduler := NewScheduler(UseWorkers(1))
	started := make(chan struct{}, 1)
	release := make(chan struct{})

	scheduler.Every(5*time.Millisecond, JobFunc(func(context.Context) error {
		select {
		case started <- struct{}{}:
		default:
		}

		<-release
		return nil
	}))

	scheduler.Start(context.Background())
	waitEvent(t, started, 2*time.Second)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if scheduler.GetStats().Dropped >= 2 {
			break
		}

		time.Sleep(5 * time.Millisecond)
	}

	if dropped := scheduler.GetStats().Dropped; dropped < 2 {
		t.Fatalf("丢弃次数 = %d, want >= 2", dropped)
	}

	close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := scheduler.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestScheduleAfterStopIsIgnored(t *testing.T) {
	scheduler := NewScheduler(UseWorkers(1))

	if err := scheduler.Stop(context.Background()); err != nil {
		t.Fatalf("空调度器 Stop() error = %v", err)
	}

	var runs atomic.Int64
	cancel := scheduler.Every(5*time.Millisecond, JobFunc(func(context.Context) error {
		runs.Add(1)
		return nil
	}))
	cancel()

	scheduler.Start(context.Background())
	time.Sleep(60 * time.Millisecond)

	if runs.Load() != 0 {
		t.Fatalf("停止后仍执行了 %d 次", runs.Load())
	}

	if stats := scheduler.GetStats(); !stats.Stopped {
		t.Fatalf("状态异常: %+v", stats)
	}
}

func TestErrorHandlerReceivesFailure(t *testing.T) {
	reported := make(chan error, 1)
	scheduler := NewScheduler(UseWorkers(1), UseErrorHandler(func(_ Job, err error) {
		select {
		case reported <- err:
		default:
		}
	}))

	want := errors.New("boom")
	scheduler.After(0, JobFunc(func(context.Context) error {
		return want
	}))

	scheduler.Start(context.Background())

	select {
	case err := <-reported:
		if !errors.Is(err, want) {
			t.Fatalf("错误 = %v, want %v", err, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("错误处理器未被调用")
	}

	if err := scheduler.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestNilJobIsIgnored(t *testing.T) {
	scheduler := NewScheduler()

	cancel := scheduler.Every(10*time.Millisecond, nil)
	cancel()

	scheduler.After(10*time.Millisecond, nil)
	scheduler.Start(context.Background())

	if err := scheduler.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if stats := scheduler.GetStats(); stats.Runs != 0 {
		t.Fatalf("执行次数 = %d, want 0", stats.Runs)
	}
}

func TestStartIsIdempotent(t *testing.T) {
	scheduler := NewScheduler(UseWorkers(2))
	events := make(chan struct{}, 32)

	scheduler.Every(10*time.Millisecond, JobFunc(func(context.Context) error {
		select {
		case events <- struct{}{}:
		default:
		}
		return nil
	}))

	var group sync.WaitGroup
	for index := 0; index < 4; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			scheduler.Start(context.Background())
		}()
	}
	group.Wait()

	waitEvent(t, events, 2*time.Second)

	if err := scheduler.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestUseWorkersDefault(t *testing.T) {
	scheduler := NewScheduler()
	if workers := scheduler.GetWorkers(); workers < 1 {
		t.Fatalf("默认并发上限 = %d", workers)
	}
}
