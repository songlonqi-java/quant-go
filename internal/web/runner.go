package web

import (
	"context"
	"sync"
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
					return
				}
				if task == nil {
					break
				}
				report, runErr := r.execute(r.ctx, func(message string) {
					_ = r.store.updateProgress(r.ctx, task.ID, message)
				})
				if err := r.store.finish(context.Background(), task.ID, report, runErr); err != nil {
					return
				}
				if r.ctx.Err() != nil {
					return
				}
			}
		}
	}
}
