package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	quantai "quant/internal/ai"
	"quant/internal/config"
	"quant/internal/portfolio"
	"quant/internal/value"
)

type fakeAICompleter struct {
	system string
	prompt string
}

func (f *fakeAICompleter) Complete(_ context.Context, system, prompt string) (*quantai.Completion, error) {
	f.system, f.prompt = system, prompt
	return &quantai.Completion{Content: "报告原始数据\n测试\n模型推断\n测试\n数据不足\n无\n风险提示\n仅供参考", PromptTokens: 12, CompletionTokens: 8, TotalTokens: 20}, nil
}

func (f *fakeAICompleter) Model() string { return "deepseek-test" }

func TestFinishPersistsIndexedReportAndImmutablePortfolioSnapshot(t *testing.T) {
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
	if _, err := store.claimNext(ctx); err != nil {
		t.Fatal(err)
	}
	report := &DailyReport{
		Version: "daily-report-v1", GeneratedAt: time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC),
		TargetDate: "20260801", TradeDate: "20260731", CodeVersion: "abc123", DataVersion: "20260731",
		SnapshotLedger: []portfolio.Transaction{{
			Date: "20260701", Code: "000001.SZ", Action: "buy", Shares: 100, Price: 10,
		}},
	}
	if err := store.finish(ctx, task.ID, report, nil); err != nil {
		t.Fatal(err)
	}

	reports, err := store.reports(ctx, ReportFilter{Kind: taskKindDaily, TradeDate: "20260731", Status: TaskSucceeded})
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 || reports[0].SnapshotTransactions != 1 || reports[0].CodeVersion != "abc123" {
		t.Fatalf("reports=%+v", reports)
	}
	stored, err := store.report(ctx, reports[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Report == nil || stored.Report.TradeDate != "20260731" {
		t.Fatalf("stored report=%+v", stored)
	}
	if _, err := store.createPortfolioTransaction(ctx, portfolio.Transaction{
		Date: "20260702", Code: "600000.SH", Action: "buy", Shares: 200, Price: 9,
	}, "test"); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.reportSnapshot(ctx, stored.PortfolioSnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Transactions) != 1 || snapshot.Transactions[0].Code != "000001.SZ" {
		t.Fatalf("snapshot changed=%+v", snapshot)
	}
}

func TestMigrationBackfillsLegacyTaskReports(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE web_tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT, kind TEXT NOT NULL, status TEXT NOT NULL,
			created_at TEXT NOT NULL, started_at TEXT NOT NULL DEFAULT '', finished_at TEXT NOT NULL DEFAULT '',
			message TEXT NOT NULL DEFAULT '', error TEXT NOT NULL DEFAULT '', report_json TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE web_task_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT, task_id INTEGER NOT NULL, created_at TEXT NOT NULL, message TEXT NOT NULL
		);`)
	if err != nil {
		t.Fatal(err)
	}
	reportJSON, err := json.Marshal(DailyReport{Version: "daily-report-v1", TradeDate: "20260731", TargetDate: "20260801"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO web_tasks(kind, status, created_at, report_json) VALUES(?, ?, ?, ?)`,
		taskKindDaily, TaskSucceeded, timestamp(), string(reportJSON)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := openTaskStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	reports, err := store.reports(context.Background(), ReportFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 || reports[0].TradeDate != "20260731" || reports[0].CodeVersion != "legacy" {
		t.Fatalf("backfilled reports=%+v", reports)
	}
}

func TestReportCenterRendersListAndDetail(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "web.db")
	server, err := newServer(Options{
		Config:       &config.Config{Data: config.DataConfig{MetaDir: filepath.Dir(dbPath)}},
		DatabasePath: dbPath,
	}, func(context.Context, string, func(string)) (*TaskResult, error) {
		return analysisTaskResult(&DailyReport{}), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	task, err := server.store.createDaily(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.store.claimNext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := server.store.finish(context.Background(), task.ID, &DailyReport{
		Version: "daily-report-v1", TradeDate: "20260731", DataVersion: "20260731", CodeVersion: "test",
	}, nil); err != nil {
		t.Fatal(err)
	}
	reports, err := server.store.reports(context.Background(), ReportFilter{})
	if err != nil || len(reports) != 1 {
		t.Fatalf("reports=%+v err=%v", reports, err)
	}

	for _, path := range []string{"/reports", "/reports/" + strconv.FormatInt(reports[0].ID, 10)} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "20260731") {
			t.Fatalf("GET %s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}

func TestValueReportFactoriesKeepValueWorkflowSeparate(t *testing.T) {
	monthly := reportFromValueMonthly(&value.MonthlyReport{
		Kind: "monthly_screen", ScreenDate: "20260731", Policy: value.DefaultPolicy(),
		Scanned: 100, Qualified: 2,
	})
	if monthly.ValueMonthly == nil || monthly.ValueQuarterly != nil || monthly.Version != "value-monthly-report-v1" || monthly.StrategyVersion != "value-v1" {
		t.Fatalf("monthly=%+v", monthly)
	}
	quarterly := reportFromValueQuarterly(&value.QuarterlyReport{
		Kind: "quarterly_review", ReviewDate: "20260731", Policy: value.DefaultPolicy(),
	})
	if quarterly.ValueQuarterly == nil || quarterly.ValueMonthly != nil || quarterly.Version != "value-quarterly-report-v1" {
		t.Fatalf("quarterly=%+v", quarterly)
	}
}

func TestValueTaskRendersDedicatedReport(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "web.db")
	server, err := newServer(Options{
		Config:       &config.Config{Data: config.DataConfig{MetaDir: filepath.Dir(dbPath), RawDir: t.TempDir()}},
		DatabasePath: dbPath,
	}, func(_ context.Context, kind string, _ func(string)) (*TaskResult, error) {
		if kind != taskKindValueMonthly {
			t.Fatalf("kind=%s", kind)
		}
		return analysisTaskResult(reportFromValueMonthly(&value.MonthlyReport{
			Kind: "monthly_screen", ScreenDate: "20260731", Policy: value.DefaultPolicy(), Scanned: 10, Qualified: 1,
		})), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	task, err := server.runner.enqueueKind(context.Background(), taskKindValueMonthly, "manual")
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
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("task not completed: %+v", stored)
		}
		time.Sleep(10 * time.Millisecond)
	}
	request := httptest.NewRequest(http.MethodGet, "/tasks/"+strconv.FormatInt(task.ID, 10), nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "月度价值筛选") || !strings.Contains(response.Body.String(), "候选池只用于跟踪") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestOperationalTaskResultIsNotIndexedAsAnalysisReport(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "web.db")
	server, err := newServer(Options{
		Config: &config.Config{Data: config.DataConfig{MetaDir: filepath.Dir(dbPath), RawDir: t.TempDir()}}, DatabasePath: dbPath,
	}, func(_ context.Context, kind string, _ func(string)) (*TaskResult, error) {
		if kind != taskKindBackup {
			t.Fatalf("kind=%s", kind)
		}
		return &TaskResult{ResultVersion: "task-result-v1", GeneratedAt: time.Now().UTC(), Backup: &BackupReport{Path: "/safe/backup.tar.gz", Files: 3, Size: 42}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	task, err := server.runner.enqueueKind(context.Background(), taskKindBackup, "manual")
	if err != nil {
		t.Fatal(err)
	}
	waitForTaskStatus(t, server.store, task.ID, TaskSucceeded)
	reports, err := server.store.reports(context.Background(), ReportFilter{})
	if err != nil || len(reports) != 0 {
		t.Fatalf("operational result leaked into report center: %+v err=%v", reports, err)
	}
	request := httptest.NewRequest(http.MethodGet, "/tasks/"+strconv.FormatInt(task.ID, 10), nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "备份产物") || strings.Contains(response.Body.String(), "日报（") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestReportAIQuestionPersistsUsageAndScopedPrompt(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "web.db")
	fake := &fakeAICompleter{}
	server, err := newServer(Options{
		Config: &config.Config{Data: config.DataConfig{MetaDir: filepath.Dir(dbPath), RawDir: t.TempDir()}}, DatabasePath: dbPath, AIClient: fake,
	}, func(context.Context, string, func(string)) (*TaskResult, error) {
		return analysisTaskResult(&DailyReport{}), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	task, err := server.store.createDaily(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.store.claimNext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := server.store.finish(context.Background(), task.ID, &DailyReport{Version: "daily-report-v1", TradeDate: "20260731", DataVersion: "20260731"}, nil); err != nil {
		t.Fatal(err)
	}
	reports, _ := server.store.reports(context.Background(), ReportFilter{})
	values := url.Values{"csrf_token": {server.csrfToken}, "question": {"这份报告的主要风险？"}}
	request := httptest.NewRequest(http.MethodPost, "/reports/"+strconv.FormatInt(reports[0].ID, 10)+"/ask", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	answers, err := server.store.aiAnswers(context.Background(), reports[0].ID)
	if err != nil || len(answers) != 1 || answers[0].TotalTokens != 20 {
		t.Fatalf("answers=%+v err=%v", answers, err)
	}
	if !strings.Contains(fake.system, "报告原始数据") || !strings.Contains(fake.prompt, "20260731") || strings.Contains(fake.prompt, "portfolio_transactions") {
		t.Fatalf("unsafe or incomplete AI prompt: system=%q prompt=%q", fake.system, fake.prompt)
	}
}

func waitForTaskStatus(t *testing.T, store *taskStore, id int64, status TaskStatus) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, err := store.task(context.Background(), id, false)
		if err != nil {
			t.Fatal(err)
		}
		if task.Status == status {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task %d did not reach %s", id, status)
}
