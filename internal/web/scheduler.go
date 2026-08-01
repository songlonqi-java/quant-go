package web

import (
	"context"
	"sync"
	"time"

	"quant/internal/data"
)

type taskScheduler struct {
	store        *taskStore
	runner       *taskRunner
	isTradingDay func(time.Time) bool
	now          func() time.Time
	ctx          context.Context
	cancel       context.CancelFunc
	done         chan struct{}
	once         sync.Once
}

func newTaskScheduler(store *taskStore, runner *taskRunner, rawDir string) *taskScheduler {
	ctx, cancel := context.WithCancel(context.Background())
	tradingDates := make(map[string]bool)
	for _, date := range data.LoadTradeDates(rawDir, nil) {
		tradingDates[date] = true
	}
	return &taskScheduler{
		store: store, runner: runner, ctx: ctx, cancel: cancel, done: make(chan struct{}),
		now: time.Now,
		isTradingDay: func(now time.Time) bool {
			return tradingDates[now.Format("20060102")]
		},
	}
}

func (s *taskScheduler) start() {
	go s.loop()
}

func (s *taskScheduler) stop() {
	s.once.Do(func() {
		s.cancel()
		<-s.done
	})
}

func (s *taskScheduler) loop() {
	defer close(s.done)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	s.runDue(s.now())
	for {
		select {
		case <-s.ctx.Done():
			return
		case now := <-ticker.C:
			s.runDue(now)
		}
	}
}

func (s *taskScheduler) runDue(now time.Time) {
	china, err := time.LoadLocation("Asia/Shanghai")
	if err == nil {
		now = now.In(china)
	}
	if !s.isTradingDay(now) {
		return
	}
	schedules, err := s.store.schedules(s.ctx)
	if err != nil {
		return
	}
	for _, schedule := range schedules {
		period, due := schedulePeriod(schedule, now)
		if !due || period == schedule.LastEnqueuedPeriod {
			continue
		}
		_, _ = s.runner.enqueueScheduled(s.ctx, schedule.Kind, period)
	}
}

func schedulePeriod(schedule Schedule, now time.Time) (string, bool) {
	if !schedule.Enabled || now.Hour() < schedule.Hour || (now.Hour() == schedule.Hour && now.Minute() < schedule.Minute) {
		return "", false
	}
	switch schedule.Kind {
	case taskKindDaily, taskKindBackup:
		return now.Format("20060102"), true
	case taskKindValueMonthly, taskKindValuePrepare:
		if now.Day() < schedule.DayOfMonth {
			return "", false
		}
		return now.Format("200601"), true
	case taskKindValueQuarterly:
		months, err := parseScheduleMonths(schedule.Months)
		if err != nil || !months[int(now.Month())] || now.Day() < schedule.DayOfMonth {
			return "", false
		}
		return now.Format("200601"), true
	default:
		return "", false
	}
}
