package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"quant/internal/config"
	"quant/internal/portfolio"
)

func TestPortfolioYAMLImportExportAndAudit(t *testing.T) {
	store, err := openTaskStore(filepath.Join(t.TempDir(), "web.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	path := filepath.Join(t.TempDir(), "portfolio.yaml")
	contents := "transactions:\n  - date: \"20260102\"\n    code: 000001.sz\n    action: BUY\n    shares: 200\n    price: 10\n    comment: test\n  - date: \"20260103\"\n    code: 000001.SZ\n    action: sell\n    shares: 100\n    price: 11\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	imported, err := store.importPortfolioYAML(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if imported.TransactionCount != 2 {
		t.Fatalf("imported=%+v", imported)
	}
	if _, err := store.importPortfolioYAML(context.Background(), path); !errors.Is(err, ErrPortfolioAlreadyImported) {
		t.Fatalf("second import error=%v", err)
	}
	ledger, err := store.portfolioLedger(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.Transactions) != 2 || ledger.Transactions[0].Code != "000001.SZ" || ledger.Transactions[0].Action != "buy" {
		t.Fatalf("ledger=%+v", ledger)
	}
	exported, err := store.exportPortfolioYAML(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(exported), "000001.SZ") || !strings.Contains(string(exported), "transactions:") {
		t.Fatalf("export=%s", exported)
	}
	var audits int
	if err := store.db.QueryRow(`SELECT COUNT(1) FROM portfolio_audit_logs`).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 2 {
		t.Fatalf("audits=%d, want 2", audits)
	}
}

func TestCreatePortfolioTransactionRejectsOversell(t *testing.T) {
	store, err := openTaskStore(filepath.Join(t.TempDir(), "web.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	ctx := context.Background()
	if _, err := store.createPortfolioTransaction(ctx, portfolio.Transaction{
		Date: "20260102", Code: "000001.SZ", Action: "buy", Shares: 100, Price: 10,
	}, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.createPortfolioTransaction(ctx, portfolio.Transaction{
		Date: "20260103", Code: "000001.SZ", Action: "sell", Shares: 200, Price: 11,
	}, "test"); err == nil || !strings.Contains(err.Error(), "卖出") {
		t.Fatalf("oversell error=%v", err)
	}
	ledger, err := store.portfolioLedger(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.Transactions) != 1 {
		t.Fatalf("ledger after rejected transaction=%+v", ledger)
	}
}

func TestPortfolioUpdateAndVoidUseOptimisticLockAndPreserveValidLedger(t *testing.T) {
	store, err := openTaskStore(filepath.Join(t.TempDir(), "web.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	ctx := context.Background()
	buy, err := store.createPortfolioTransaction(ctx, portfolio.Transaction{
		Date: "20260102", Code: "000001.SZ", Action: "buy", Shares: 100, Price: 10,
	}, "test")
	if err != nil {
		t.Fatal(err)
	}
	sell, err := store.createPortfolioTransaction(ctx, portfolio.Transaction{
		Date: "20260103", Code: "000001.SZ", Action: "sell", Shares: 100, Price: 11,
	}, "test")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.updatePortfolioComment(ctx, buy.ID, buy.Version, "长期观察", "test")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Trade.Comment != "长期观察" || updated.Version != buy.Version+1 {
		t.Fatalf("updated=%+v", updated)
	}
	if _, err := store.updatePortfolioComment(ctx, buy.ID, buy.Version, "过期页面", "test"); !errors.Is(err, ErrPortfolioConflict) {
		t.Fatalf("stale update error=%v", err)
	}
	if err := store.voidPortfolioTransaction(ctx, buy.ID, updated.Version, "test"); err == nil || !strings.Contains(err.Error(), "撤销后流水无效") {
		t.Fatalf("void buy error=%v", err)
	}
	if err := store.voidPortfolioTransaction(ctx, sell.ID, sell.Version, "test"); err != nil {
		t.Fatalf("void sell: %v", err)
	}
	if err := store.voidPortfolioTransaction(ctx, buy.ID, updated.Version, "test"); err != nil {
		t.Fatalf("void buy after sell: %v", err)
	}
	transactions, err := store.portfolioTransactions(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(transactions) != 2 || transactions[0].Status != "void" || transactions[1].Status != "void" {
		t.Fatalf("transactions=%+v", transactions)
	}
}

func TestPortfolioHTTPCreateRequiresCSRFAndRendersPage(t *testing.T) {
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

	values := url.Values{
		"date": {"20260102"}, "code": {"000001.SZ"}, "action": {"buy"},
		"shares": {"100"}, "price": {"10.5"},
	}
	request := httptest.NewRequest(http.MethodPost, "/portfolio/transactions", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	server.mux.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("missing csrf status=%d", response.Code)
	}

	values.Set("csrf_token", server.csrfToken)
	request = httptest.NewRequest(http.MethodPost, "/portfolio/transactions", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response = httptest.NewRecorder()
	server.mux.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/portfolio?status=created" {
		t.Fatalf("create status=%d location=%q body=%s", response.Code, response.Header().Get("Location"), response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/portfolio", nil)
	response = httptest.NewRecorder()
	server.mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "000001.SZ") || !strings.Contains(response.Body.String(), "当前持仓") {
		t.Fatalf("portfolio page status=%d body=%s", response.Code, response.Body.String())
	}
}
