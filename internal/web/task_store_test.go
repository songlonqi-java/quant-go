package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
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
}

func TestServerCreatesAndRendersDailyReport(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "web.db")
	server, err := newServer(Options{
		Config:       &config.Config{Data: config.DataConfig{MetaDir: filepath.Dir(dbPath)}},
		DatabasePath: dbPath,
	}, func(_ context.Context, progress func(string)) (*DailyReport, error) {
		progress("测试任务已完成")
		return &DailyReport{
			TargetDate: "20260731",
			TradeDate:  "20260731",
			Position:   signal.PositionDecision{Action: signal.PositionActionCash, Advice: "等待确认"},
		}, nil
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
