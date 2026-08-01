package web

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type taskExecutor func(context.Context, string, func(string)) (*TaskResult, error)

// taskRunner is the only writer of market-data tasks in the Web process. Its
// small interface (enqueue plus persisted task state) hides queue wake-ups,
// progress recording and failure handling from HTTP handlers.
type taskRunner struct {
	store   *taskStore
	execute taskExecutor
	ctx     context.Context
	cancel  context.CancelFunc
	wake    chan struct{}
	done    chan struct{}
	once    sync.Once
}

func newTaskRunner(store *taskStore, execute taskExecutor) *taskRunner {
	ctx, cancel := context.WithCancel(context.Background())
	return &taskRunner{
		store:   store,
		execute: execute,
		ctx:     ctx,
		cancel:  cancel,
		wake:    make(chan struct{}, 1),
		done:    make(chan struct{}),
	}
}

func (r *taskRunner) start() {
	go r.loop()
	r.notify()
}

func (r *taskRunner) enqueue(ctx context.Context) (*Task, error) {
	return r.enqueueKind(ctx, taskKindDaily, "manual")
}

func (r *taskRunner) enqueueKind(ctx context.Context, kind, source string) (*Task, error) {
	task, err := r.store.createTask(ctx, kind, source)
	if err != nil {
		return nil, err
	}
	r.notify()
	return task, nil
}

func (r *taskRunner) enqueueScheduled(ctx context.Context, kind, period string) (*Task, error) {
	task, err := r.store.createScheduledTask(ctx, kind, period)
	if err != nil || task == nil {
		return task, err
	}
	r.notify()
	return task, nil
}

func (r *taskRunner) stop() {
	r.once.Do(func() {
		r.cancel()
		<-r.done
	})
}

func (r *taskRunner) notify() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *taskRunner) loop() {
	defer close(r.done)
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-r.wake:
			for {
				task, err := r.store.claimNext(r.ctx)
				if err != nil {
					if !waitForRetry(r.ctx, time.Second) {
						return
					}
					continue
				}
				if task == nil {
					break
				}
				report, runErr := r.runSafely(task.Kind, func(message string) {
					for r.ctx.Err() == nil {
						if err := r.store.updateProgress(r.ctx, task.ID, message); err == nil {
							return
						}
						if !waitForRetry(r.ctx, time.Second) {
							return
						}
					}
				})
				if !r.persistFinish(task.ID, report, runErr) {
					return
				}
				if r.ctx.Err() != nil {
					return
				}
			}
		}
	}
}

func (r *taskRunner) persistFinish(taskID int64, result *TaskResult, runErr error) bool {
	ctx := r.ctx
	cancel := func() {}
	if r.ctx.Err() != nil {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	}
	defer cancel()
	for {
		if err := r.store.finishResult(ctx, taskID, result, runErr); err == nil {
			return true
		}
		if !waitForRetry(ctx, time.Second) {
			return false
		}
	}
}

func (r *taskRunner) runSafely(kind string, update func(string)) (result *TaskResult, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("任务执行异常: %v", recovered)
		}
	}()
	return r.execute(r.ctx, kind, update)
}

func waitForRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
