// Package job 提供进程内任务调度。
//
// 设计要点：
//   - 单协程维护最小堆，按"下一次执行时间"唤醒，避免每个任务一个定时器；
//   - 执行并发有界（默认 GOMAXPROCS），没有空闲额度时直接丢弃并计数，
//     调度协程永远不会被业务任务阻塞；
//   - 语义为 at-most-once：进程崩溃或重启会丢失任务，不做持久化，
//     需要可靠性时应在业务侧落库；
//   - 只依赖标准库与仓库内 logging，不引入第三方调度库。
package job

import (
	"container/heap"
	"context"
	"fmt"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"gitee.com/leminewx/leego/logging"
)

// _DEFAULT_WORKERS 为默认并发执行上限。
const _DEFAULT_WORKERS = 0

// Job 表示一个可调度的任务。
//
// Run 中的 ctx 来自调度器的启动上下文，Stop 不会主动取消它；
// 需要中断时应由调用方取消 Start 传入的 ctx。
type Job interface {
	// Run 执行任务；返回错误会被错误处理器记录。
	Run(ctx context.Context) error
}

// JobFunc 把函数适配为 Job。
type JobFunc func(ctx context.Context) error

// Run 执行函数本身。
func (own JobFunc) Run(ctx context.Context) error {
	return own(ctx)
}

// Options 定义调度器行为。
type Options struct {
	// Workers 为并发执行上限，非正数时使用 GOMAXPROCS。
	Workers int
	// OnError 为任务错误处理器，nil 时记录告警日志。
	OnError func(job Job, err error)
}

// NewDefaultOptions 返回推荐配置。
func NewDefaultOptions() Options {
	return Options{Workers: _DEFAULT_WORKERS}
}

// normalize 修正非法配置。
func (own Options) normalize() Options {
	if own.Workers <= 0 {
		own.Workers = runtime.GOMAXPROCS(0)
	}

	if own.Workers <= 0 {
		own.Workers = 1
	}

	return own
}

// Option 修改 Options。
type Option func(*Options)

// WithWorkers 设置并发执行上限。
func WithWorkers(workers int) Option {
	return func(options *Options) {
		options.Workers = workers
	}
}

// WithErrorHandler 设置任务错误处理器。
func WithErrorHandler(handler func(job Job, err error)) Option {
	return func(options *Options) {
		options.OnError = handler
	}
}

// task 是堆中的一个调度项。
type task struct {
	// order 为注册序号，用于同一时刻的稳定排序。
	order uint64
	// next 为下一次执行时间。
	next time.Time
	// interval 为周期；0 表示只执行一次。
	interval time.Duration
	// job 为任务体。
	job Job
	// cancelled 标记任务已被取消。
	cancelled bool
	// running 标记周期任务的上一轮是否仍在执行。
	// 周期短于任务耗时时，本轮直接跳过，避免同一任务重叠执行。
	running bool
}

// taskHeap 是最小堆，按 next 升序、order 次升序排列。
type taskHeap []*task

// Len 返回堆长度。
func (own taskHeap) Len() int {
	return len(own)
}

// Less 判断顺序：先比时间，再比注册序号。
func (own taskHeap) Less(left, right int) bool {
	if own[left].next.Equal(own[right].next) {
		return own[left].order < own[right].order
	}

	return own[left].next.Before(own[right].next)
}

// Swap 交换两个元素。
func (own taskHeap) Swap(left, right int) {
	own[left], own[right] = own[right], own[left]
}

// Push 入堆。
func (own *taskHeap) Push(value any) {
	*own = append(*own, value.(*task))
}

// Pop 出堆。
func (own *taskHeap) Pop() any {
	items := *own
	last := len(items) - 1
	value := items[last]
	items[last] = nil
	*own = items[:last]

	return value
}

// Stats 是调度器的运行统计。
type Stats struct {
	// Runs 为已开始执行的任务次数。
	Runs uint64
	// Dropped 为因并发额度不足被丢弃的执行次数。
	Dropped uint64
	// Skipped 为因上一轮未结束而跳过的周期执行次数。
	Skipped uint64
	// Pending 为堆中待执行的任务数。
	Pending int
	// Started 标记调度器是否已启动。
	Started bool
	// Stopped 标记调度器是否已停止。
	Stopped bool
}

// Scheduler 是进程内任务调度器，零值不可用，需通过 NewScheduler 创建。
type Scheduler struct {
	// mu 保护 tasks、started、stopped 与 seq。
	mu sync.Mutex
	// tasks 为待执行任务堆。
	tasks taskHeap
	// seq 为注册序号。
	seq uint64
	// wake 用于唤醒调度协程重新计算定时器。
	wake chan struct{}
	// stop 用于通知调度协程退出。
	stop chan struct{}
	// done 在调度协程退出后关闭。
	done chan struct{}
	// slots 为并发额度信号量。
	slots chan struct{}
	// workers 为并发上限。
	workers int
	// onError 为任务错误处理器。
	onError func(job Job, err error)
	// running 为在途任务计数。
	running sync.WaitGroup
	// runs 为执行次数。
	runs atomic.Uint64
	// dropped 为丢弃次数。
	dropped atomic.Uint64
	// skipped 为周期任务重叠而跳过的次数。
	skipped atomic.Uint64
	// started 标记是否已启动。
	started bool
	// stopped 标记是否已停止。
	stopped bool
}

// NewScheduler 创建调度器。
func NewScheduler(options ...Option) *Scheduler {
	settings := NewDefaultOptions()
	for _, option := range options {
		if option != nil {
			option(&settings)
		}
	}
	settings = settings.normalize()

	return &Scheduler{
		wake:    make(chan struct{}, 1),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
		slots:   make(chan struct{}, settings.Workers),
		workers: settings.Workers,
		onError: settings.OnError,
	}
}

// GetWorkers 返回并发执行上限。
func (own *Scheduler) GetWorkers() int {
	return own.workers
}

// ScheduleEvery 注册周期任务，返回取消函数。
//
// 调度器已停止或参数非法时返回空操作函数。
func (own *Scheduler) ScheduleEvery(interval time.Duration, job Job) func() {
	if interval <= 0 {
		return func() {}
	}

	return own.schedule(interval, interval, job)
}

// ScheduleAfter 注册一次性任务，延迟 d 后执行。
func (own *Scheduler) ScheduleAfter(delay time.Duration, job Job) {
	if delay < 0 {
		return
	}

	own.schedule(0, delay, job)
}

// schedule 注册任务；interval 为 0 表示一次性任务。
func (own *Scheduler) schedule(interval, delay time.Duration, job Job) func() {
	if job == nil {
		return func() {}
	}

	own.mu.Lock()
	if own.stopped {
		own.mu.Unlock()
		return func() {}
	}

	own.seq++
	item := &task{
		order:    own.seq,
		next:     time.Now().Add(delay),
		interval: interval,
		job:      job,
	}
	heap.Push(&own.tasks, item)
	own.mu.Unlock()

	own.signal()

	return func() {
		own.mu.Lock()
		item.cancelled = true
		own.mu.Unlock()
		own.signal()
	}
}

// Start 启动调度协程，可重复调用。
func (own *Scheduler) Start(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}

	own.mu.Lock()
	if own.started || own.stopped {
		own.mu.Unlock()
		return
	}
	own.started = true
	own.mu.Unlock()

	go own.run(ctx)
}

// Stop 停止调度并等待在途任务，超时返回 ctx 的错误。
//
// 停止后注册的任务会被忽略，已注册但未执行的任务不再执行。
func (own *Scheduler) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	own.mu.Lock()
	if !own.stopped {
		own.stopped = true
		close(own.stop)

		if !own.started {
			// 调度协程未启动：直接标记完成，避免 Stop 永久阻塞。
			close(own.done)
		}
	}

	own.tasks = nil
	own.mu.Unlock()

	select {
	case <-own.done:
	case <-ctx.Done():
		return ctx.Err()
	}

	finished := make(chan struct{})
	go func() {
		own.running.Wait()
		close(finished)
	}()

	select {
	case <-finished:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// GetStats 返回运行统计。
func (own *Scheduler) GetStats() Stats {
	own.mu.Lock()
	stats := Stats{
		Pending: len(own.tasks),
		Started: own.started,
		Stopped: own.stopped,
	}
	own.mu.Unlock()

	stats.Runs = own.runs.Load()
	stats.Dropped = own.dropped.Load()
	stats.Skipped = own.skipped.Load()
	return stats
}

// signal 唤醒调度协程；通道带缓冲，重复唤醒会被合并。
func (own *Scheduler) signal() {
	select {
	case own.wake <- struct{}{}:
	default:
	}
}

// run 是调度主循环。
func (own *Scheduler) run(ctx context.Context) {
	timer := time.NewTimer(time.Hour)
	stopTimer(timer)

	for {
		own.mu.Lock()
		var next time.Time
		var ok bool
		if len(own.tasks) > 0 {
			next = own.tasks[0].next
			ok = true
		}
		own.mu.Unlock()

		var tick <-chan time.Time
		if ok {
			delay := time.Until(next)
			if delay < 0 {
				delay = 0
			}

			timer.Reset(delay)
			tick = timer.C
		}

		select {
		case <-ctx.Done():
			stopTimer(timer)
			close(own.done)
			return
		case <-own.stop:
			stopTimer(timer)
			close(own.done)
			return
		case <-own.wake:
			stopTimer(timer)
		case <-tick:
			own.fire(ctx, time.Now())
		}
	}
}

// fire 执行所有到期任务，并重新排入周期任务。
func (own *Scheduler) fire(ctx context.Context, now time.Time) {
	own.mu.Lock()
	if own.stopped {
		// Stop 与 tick 同时就绪时 select 可能选中 tick：停止后不得再派发。
		own.mu.Unlock()
		return
	}

	ready := make([]*task, 0, 4)
	for len(own.tasks) > 0 {
		item := own.tasks[0]
		if item.next.After(now) {
			break
		}

		heap.Pop(&own.tasks)
		if item.cancelled {
			continue
		}

		if item.interval > 0 {
			if item.running {
				// 上一轮尚未结束：跳过本轮，只推进下一次执行时间，
				// 避免同一任务重叠执行导致数据竞争或重复处理。
				own.skipped.Add(1)
				item.next = item.next.Add(item.interval)
				if !item.next.After(now) {
					item.next = now.Add(item.interval)
				}

				heap.Push(&own.tasks, item)
				continue
			}

			item.next = item.next.Add(item.interval)
			if !item.next.After(now) {
				// 避免长时间停顿后的补跑风暴：从当前时刻重新计时。
				item.next = now.Add(item.interval)
			}

			heap.Push(&own.tasks, item)
		}

		ready = append(ready, item)
	}
	own.mu.Unlock()

	for _, item := range ready {
		own.dispatch(ctx, item)
	}
}

// dispatch 在并发额度内异步执行任务。
func (own *Scheduler) dispatch(ctx context.Context, item *task) {
	own.mu.Lock()
	stopped := own.stopped
	own.mu.Unlock()
	if stopped {
		// 停止后不再启动新任务，“已注册但未执行”的任务就此作废。
		return
	}

	select {
	case own.slots <- struct{}{}:
	default:
		// 额度已满：丢弃本次执行并计数，绝不阻塞调度协程。
		own.dropped.Add(1)
		return
	}

	// 先置位再启动协程，确保下一轮 fire 能看到“正在运行”。
	own.mu.Lock()
	item.running = true
	own.mu.Unlock()

	own.running.Add(1)
	own.runs.Add(1)

	go func() {
		// 额度归还必须无论如何都执行，因此放在最外层的 defer。
		defer func() {
			<-own.slots
			own.running.Done()

			own.mu.Lock()
			item.running = false
			own.mu.Unlock()
		}()

		// 业务任务 panic 不能传染到调度器：转为错误上报后归还额度，
		// 否则会直接杀死进程，并让 Stop 永远等不到在途任务结束。
		defer func() {
			if recovered := recover(); recovered != nil {
				own.report(item.job, fmt.Errorf("job: task panicked: %v\n%s", recovered, debug.Stack()))
			}
		}()

		if err := item.job.Run(ctx); err != nil {
			own.report(item.job, err)
		}
	}()
}

// report 报告任务错误。
//
// 错误处理器由调用方提供，其自身的 panic 在此被吞掉：
// 上报失败不应影响额度归还，也不应终止进程。
func (own *Scheduler) report(current Job, err error) {
	defer func() {
		_ = recover()
	}()

	if own.onError != nil {
		own.onError(current, err)
		return
	}

	logging.LogWarn("job: task failed: %v", err)
}

// stopTimer 安全地停止并排空定时器。
func stopTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}
