package web

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
