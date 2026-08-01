package web

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type taskExecutor func(context.Context, func(string)) (*DailyReport, error)

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
	task, err := r.store.createDaily(ctx)
	if err != nil {
		return nil, err
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
				report, runErr := r.runSafely(func(message string) {
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

func (r *taskRunner) persistFinish(taskID int64, report *DailyReport, runErr error) bool {
	ctx := r.ctx
	cancel := func() {}
	if r.ctx.Err() != nil {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	}
	defer cancel()
	for {
		if err := r.store.finish(ctx, taskID, report, runErr); err == nil {
			return true
		}
		if !waitForRetry(ctx, time.Second) {
			return false
		}
	}
}

func (r *taskRunner) runSafely(update func(string)) (report *DailyReport, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("任务执行异常: %v", recovered)
		}
	}()
	return r.execute(r.ctx, update)
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
