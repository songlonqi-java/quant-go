package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"quant/internal/config"
	"quant/internal/dataset"
	"quant/internal/signal"
	"quant/internal/workflow/daily"
	signalworkflow "quant/internal/workflow/signal"
)

func TestTaskStoreLifecycleRejectsConcurrentDailyTask(t *testing.T) {
	store, err := openTaskStore(filepath.Join(t.TempDir(), "web.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	ctx := context.Background()

	task, err := store.createDaily(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.createDaily(ctx); !errors.Is(err, ErrTaskAlreadyActive) {
		t.Fatalf("second create error = %v, want ErrTaskAlreadyActive", err)
	}
	claimed, err := store.claimNext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil || claimed.ID != task.ID || claimed.Status != TaskRunning {
		t.Fatalf("claimed = %+v", claimed)
	}
	if err := store.updateProgress(ctx, task.ID, "正在生成日报"); err != nil {
		t.Fatal(err)
	}
	report := &DailyReport{TargetDate: "20260731", Position: signal.PositionDecision{Action: signal.PositionActionCash}}
	if err := store.finish(ctx, task.ID, report, nil); err != nil {
		t.Fatal(err)
	}
	stored, err := store.task(ctx, task.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != TaskSucceeded || stored.Report == nil || stored.Report.Position.Action != signal.PositionActionCash {
		t.Fatalf("stored task = %+v", stored)
	}
	if len(stored.Events) < 4 {
		t.Fatalf("events = %+v, want lifecycle events", stored.Events)
	}
	latest, err := store.latestDailyReport(ctx)
	if err != nil || latest == nil || latest.TargetDate != "20260731" {
		t.Fatalf("latest daily report=%+v err=%v", latest, err)
	}
}

func TestTaskStoreConcurrentCreateIsAtomic(t *testing.T) {
	store, err := openTaskStore(filepath.Join(t.TempDir(), "web.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()

	const attempts = 8
	var wg sync.WaitGroup
	errs := make(chan error, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.createDaily(context.Background())
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	created := 0
	rejected := 0
	for err := range errs {
		switch {
		case err == nil:
			created++
		case errors.Is(err, ErrTaskAlreadyActive):
			rejected++
		default:
			t.Fatalf("unexpected create error: %v", err)
		}
	}
	if created != 1 || rejected != attempts-1 {
		t.Fatalf("created=%d rejected=%d, want 1/%d", created, rejected, attempts-1)
	}
}

func TestTaskStoreMigrationAndInterruptedRecovery(t *testing.T) {
	store, err := openTaskStore(filepath.Join(t.TempDir(), "web.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()

	var migrations int
	if err := store.db.QueryRow(`SELECT COUNT(1) FROM schema_migrations`).Scan(&migrations); err != nil {
		t.Fatal(err)
	}
	if migrations != len(schemaMigrations) {
		t.Fatalf("migrations=%d, want %d", migrations, len(schemaMigrations))
	}

	task, err := store.createDaily(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.claimNext(context.Background()); err != nil {
		t.Fatal(err)
	}
	recovered, err := store.recoverInterrupted(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 1 {
		t.Fatalf("recovered=%d, want 1", recovered)
	}
	stored, err := store.task(context.Background(), task.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != TaskFailed || stored.FinishedAt == "" || !strings.Contains(stored.Error, "服务重启") {
		t.Fatalf("recovered task=%+v", stored)
	}
	if len(stored.Events) == 0 || !strings.Contains(stored.Events[len(stored.Events)-1].Message, "已中断") {
		t.Fatalf("events=%+v", stored.Events)
	}
	if _, err := store.createDaily(context.Background()); err != nil {
		t.Fatalf("create after recovery: %v", err)
	}
}

func TestTaskRunnerConvertsPanicToFailedTask(t *testing.T) {
	store, err := openTaskStore(filepath.Join(t.TempDir(), "web.db"))
	if err != nil {
		t.Fatal(err)
	}
	runner := newTaskRunner(store, func(context.Context, string, func(string)) (*TaskResult, error) {
		panic("boom")
	})
	runner.start()
	defer func() {
		runner.stop()
		store.close()
	}()

	task, err := runner.enqueue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		stored, err := store.task(context.Background(), task.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Status == TaskFailed {
			if !strings.Contains(stored.Error, "任务执行异常: boom") {
				t.Fatalf("task error=%q", stored.Error)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("task did not fail: %+v", stored)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestServerCreatesAndRendersDailyReport(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "web.db")
	server, err := newServer(Options{
		Config:       &config.Config{Data: config.DataConfig{MetaDir: filepath.Dir(dbPath)}},
		DatabasePath: dbPath,
	}, func(_ context.Context, _ string, progress func(string)) (*TaskResult, error) {
		progress("测试任务已完成")
		return analysisTaskResult(&DailyReport{
			TargetDate: "20260731",
			TradeDate:  "20260731",
			Position:   signal.PositionDecision{Action: signal.PositionActionCash, Advice: "等待确认"},
		}), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	task, err := server.runner.enqueue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		stored, err := server.store.task(context.Background(), task.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Status == TaskSucceeded {
			if stored.Report == nil || stored.Report.TradeDate != "20260731" {
				t.Fatalf("report = %+v", stored.Report)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("task did not finish: %+v", stored)
		}
		time.Sleep(10 * time.Millisecond)
	}
	request := httptest.NewRequest(http.MethodGet, "/tasks/"+strconv.FormatInt(task.ID, 10), nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("page status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "正式推荐买入") || !strings.Contains(response.Body.String(), "不强行推荐买入") {
		t.Fatalf("page body missing report policy: %s", response.Body.String())
	}
}

func TestReportFromDailySuppressesFormalBuysWhenCash(t *testing.T) {
	report := reportFromDaily(&daily.Result{
		TargetDate: "20260731",
		Signal: &signalworkflow.Result{
			Dataset:          &dataset.Dataset{LatestDate: "20260731"},
			PositionDecision: signal.PositionDecision{Action: signal.PositionActionCash},
			Signals: []signal.SignalResult{{
				Code: "000001.SZ", BuyCount: 2, TotalScore: 1,
			}},
		},
	})
	if len(report.Signals) != 1 {
		t.Fatalf("signals = %+v, want retained full evidence", report.Signals)
	}
	if len(report.Recommendations) != 0 {
		t.Fatalf("recommendations = %+v, want none when cash", report.Recommendations)
	}
}

func TestDecodeTaskResultKeepsLegacyCompatibility(t *testing.T) {
	analysisJSON, err := json.Marshal(DailyReport{Version: "daily-report-v1", TradeDate: "20260731"})
	if err != nil {
		t.Fatal(err)
	}
	analysis, err := decodeTaskResult(analysisJSON)
	if err != nil || analysis.Analysis == nil || analysis.Analysis.TradeDate != "20260731" {
		t.Fatalf("analysis=%+v err=%v", analysis, err)
	}
	operation, err := decodeTaskResult([]byte(`{"version":"backup-report-v1","backup":{"path":"legacy.tar.gz","files":2}}`))
	if err != nil || operation.Backup == nil || operation.Backup.Path != "legacy.tar.gz" || operation.Analysis != nil {
		t.Fatalf("operation=%+v err=%v", operation, err)
	}
}
