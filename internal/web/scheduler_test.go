package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"quant/internal/config"
)

func TestSchedulePeriodCadences(t *testing.T) {
	now := time.Date(2026, 8, 3, 20, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	tests := []struct {
		schedule Schedule
		period   string
		due      bool
	}{
		{Schedule{Kind: taskKindDaily, Enabled: true, Hour: 17}, "20260803", true},
		{Schedule{Kind: taskKindValueMonthly, Enabled: true, Hour: 18, DayOfMonth: 3}, "202608", true},
		{Schedule{Kind: taskKindValueMonthly, Enabled: true, Hour: 18, DayOfMonth: 4}, "", false},
		{Schedule{Kind: taskKindValueQuarterly, Enabled: true, Hour: 19, DayOfMonth: 1, Months: "1,4,7,10"}, "", false},
		{Schedule{Kind: taskKindValueQuarterly, Enabled: true, Hour: 19, DayOfMonth: 1, Months: "2,5,8,11"}, "202608", true},
	}
	for _, test := range tests {
		period, due := schedulePeriod(test.schedule, now)
		if period != test.period || due != test.due {
			t.Errorf("schedulePeriod(%+v)=%q/%t, want %q/%t", test.schedule, period, due, test.period, test.due)
		}
	}
}

func TestSchedulePageUpdatesPersistedConfiguration(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "web.db")
	server, err := newServer(Options{
		Config:       &config.Config{Data: config.DataConfig{MetaDir: filepath.Dir(dbPath), RawDir: t.TempDir()}},
		DatabasePath: dbPath,
	}, func(context.Context, string, func(string)) (*DailyReport, error) { return &DailyReport{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	values := url.Values{
		"csrf_token": {server.csrfToken}, "enabled": {"1"}, "hour": {"18"},
		"minute": {"30"}, "day_of_month": {"2"}, "months": {""},
	}
	request := httptest.NewRequest(http.MethodPost, "/schedules/value_monthly", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("update status=%d body=%s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/schedules", nil)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "本地定时任务") || !strings.Contains(response.Body.String(), "value_monthly") {
		t.Fatalf("page status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestSchedulerEnqueuesDueTaskOnceThroughRunner(t *testing.T) {
	store, err := openTaskStore(filepath.Join(t.TempDir(), "web.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	var executions atomic.Int32
	runner := newTaskRunner(store, func(_ context.Context, kind string, _ func(string)) (*DailyReport, error) {
		if kind != taskKindValueMonthly {
			t.Fatalf("kind=%s", kind)
		}
		executions.Add(1)
		return &DailyReport{Version: "test", TradeDate: "20260803"}, nil
	})
	runner.start()
	defer runner.stop()
	if err := store.updateSchedule(context.Background(), Schedule{
		Kind: taskKindValueMonthly, Enabled: true, Hour: 18, Minute: 0, DayOfMonth: 1,
	}); err != nil {
		t.Fatal(err)
	}
	fixedNow := time.Date(2026, 8, 3, 20, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	scheduler := newTaskScheduler(store, runner, t.TempDir())
	scheduler.now = func() time.Time { return fixedNow }
	scheduler.isTradingDay = func(time.Time) bool { return true }
	scheduler.start()
	defer scheduler.stop()

	deadline := time.Now().Add(2 * time.Second)
	for executions.Load() != 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if executions.Load() != 1 {
		t.Fatalf("executions=%d", executions.Load())
	}
	scheduler.runDue(fixedNow)
	time.Sleep(50 * time.Millisecond)
	if executions.Load() != 1 {
		t.Fatalf("duplicate executions=%d", executions.Load())
	}
	schedules, err := store.schedules(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, schedule := range schedules {
		if schedule.Kind == taskKindValueMonthly && schedule.LastEnqueuedPeriod != "202608" {
			t.Fatalf("schedule=%+v", schedule)
		}
	}
}
