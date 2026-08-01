package web

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"quant/internal/portfolio"

	"gopkg.in/yaml.v3"
)

var ErrPortfolioAlreadyImported = errors.New("该 YAML 文件已经导入")

type StoredTransaction struct {
	ID        int64
	Trade     portfolio.Transaction
	Status    string
	Source    string
	CreatedAt string
	UpdatedAt string
	Version   int
}

type PortfolioImport struct {
	ID               int64
	SourcePath       string
	SourceSHA256     string
	TransactionCount int
	ImportedAt       string
}

func (s *taskStore) portfolioLedger(ctx context.Context) (*portfolio.Ledger, error) {
	transactions, err := s.portfolioTransactions(ctx, false)
	if err != nil {
		return nil, err
	}
	ledger := &portfolio.Ledger{Transactions: make([]portfolio.Transaction, 0, len(transactions))}
	for _, transaction := range transactions {
		ledger.Transactions = append(ledger.Transactions, transaction.Trade)
	}
	return ledger, nil
}

func (s *taskStore) portfolioTransactions(ctx context.Context, includeVoid bool) ([]StoredTransaction, error) {
	query := `
		SELECT id, trade_date, code, action, shares, price, comment, status, source, created_at, updated_at, version
		FROM portfolio_transactions`
	if !includeVoid {
		query += ` WHERE status = 'active'`
	}
	query += ` ORDER BY trade_date, id`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var transactions []StoredTransaction
	for rows.Next() {
		var transaction StoredTransaction
		if err := rows.Scan(&transaction.ID, &transaction.Trade.Date, &transaction.Trade.Code,
			&transaction.Trade.Action, &transaction.Trade.Shares, &transaction.Trade.Price,
			&transaction.Trade.Comment, &transaction.Status, &transaction.Source,
			&transaction.CreatedAt, &transaction.UpdatedAt, &transaction.Version); err != nil {
			return nil, err
		}
		transactions = append(transactions, transaction)
	}
	return transactions, rows.Err()
}

func (s *taskStore) createPortfolioTransaction(ctx context.Context, transaction portfolio.Transaction, source string) (*StoredTransaction, error) {
	transaction = normalizePortfolioTransaction(transaction)
	if err := portfolio.ValidateTransaction(transaction); err != nil {
		return nil, err
	}
	if source == "" {
		source = "web"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	ledger, err := portfolioLedgerFromQuery(ctx, tx)
	if err != nil {
		return nil, err
	}
	ledger.Transactions = append(ledger.Transactions, transaction)
	if err := portfolio.ValidateLedger(ledger); err != nil {
		return nil, err
	}
	now := timestamp()
	result, err := tx.ExecContext(ctx, `
		INSERT INTO portfolio_transactions(
			trade_date, code, action, shares, price, comment, status, source, created_at, updated_at
		) VALUES(?, ?, ?, ?, ?, ?, 'active', ?, ?, ?)`,
		transaction.Date, transaction.Code, transaction.Action, transaction.Shares,
		transaction.Price, transaction.Comment, source, now, now)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	stored := &StoredTransaction{
		ID: id, Trade: transaction, Status: "active", Source: source,
		CreatedAt: now, UpdatedAt: now, Version: 1,
	}
	if err := insertPortfolioAudit(ctx, tx, id, "create", nil, stored, source, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return stored, nil
}

func (s *taskStore) importPortfolioYAML(ctx context.Context, path string) (*PortfolioImport, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var ledger portfolio.Ledger
	if err := yaml.Unmarshal(contents, &ledger); err != nil {
		return nil, fmt.Errorf("解析持仓 YAML: %w", err)
	}
	for i := range ledger.Transactions {
		ledger.Transactions[i] = normalizePortfolioTransaction(ledger.Transactions[i])
	}
	if err := portfolio.ValidateLedger(&ledger); err != nil {
		return nil, err
	}
	digest := sha256.Sum256(contents)
	hash := hex.EncodeToString(digest[:])
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var existing int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM portfolio_imports WHERE source_sha256 = ?`, hash).Scan(&existing); err != nil {
		return nil, err
	}
	if existing > 0 {
		return nil, ErrPortfolioAlreadyImported
	}
	current, err := portfolioLedgerFromQuery(ctx, tx)
	if err != nil {
		return nil, err
	}
	if len(current.Transactions) > 0 {
		return nil, fmt.Errorf("SQLite 中已有交易流水；为避免重复，只允许导入到空流水")
	}
	now := timestamp()
	for _, transaction := range ledger.Transactions {
		result, err := tx.ExecContext(ctx, `
			INSERT INTO portfolio_transactions(
				trade_date, code, action, shares, price, comment, status, source, created_at, updated_at
			) VALUES(?, ?, ?, ?, ?, ?, 'active', 'yaml_import', ?, ?)`,
			transaction.Date, transaction.Code, transaction.Action, transaction.Shares,
			transaction.Price, transaction.Comment, now, now)
		if err != nil {
			return nil, err
		}
		id, err := result.LastInsertId()
		if err != nil {
			return nil, err
		}
		stored := &StoredTransaction{ID: id, Trade: transaction, Status: "active", Source: "yaml_import", CreatedAt: now, UpdatedAt: now, Version: 1}
		if err := insertPortfolioAudit(ctx, tx, id, "import", nil, stored, "yaml_import", now); err != nil {
			return nil, err
		}
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO portfolio_imports(source_path, source_sha256, transaction_count, imported_at)
		VALUES(?, ?, ?, ?)`, path, hash, len(ledger.Transactions), now)
	if err != nil {
		return nil, err
	}
	importID, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &PortfolioImport{ID: importID, SourcePath: path, SourceSHA256: hash, TransactionCount: len(ledger.Transactions), ImportedAt: now}, nil
}

func (s *taskStore) exportPortfolioYAML(ctx context.Context) ([]byte, error) {
	ledger, err := s.portfolioLedger(ctx)
	if err != nil {
		return nil, err
	}
	contents, err := yaml.Marshal(ledger)
	if err != nil {
		return nil, fmt.Errorf("生成持仓 YAML: %w", err)
	}
	return contents, nil
}

func portfolioLedgerFromQuery(ctx context.Context, queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}) (*portfolio.Ledger, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT trade_date, code, action, shares, price, comment
		FROM portfolio_transactions WHERE status = 'active' ORDER BY trade_date, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ledger := &portfolio.Ledger{}
	for rows.Next() {
		var transaction portfolio.Transaction
		if err := rows.Scan(&transaction.Date, &transaction.Code, &transaction.Action,
			&transaction.Shares, &transaction.Price, &transaction.Comment); err != nil {
			return nil, err
		}
		ledger.Transactions = append(ledger.Transactions, transaction)
	}
	return ledger, rows.Err()
}

func insertPortfolioAudit(ctx context.Context, execer sqlExecer, transactionID int64, operation string, before, after any, source, createdAt string) error {
	beforeJSON, err := marshalAuditValue(before)
	if err != nil {
		return err
	}
	afterJSON, err := marshalAuditValue(after)
	if err != nil {
		return err
	}
	_, err = execer.ExecContext(ctx, `
		INSERT INTO portfolio_audit_logs(transaction_id, operation, before_json, after_json, source, created_at)
		VALUES(?, ?, ?, ?, ?, ?)`, transactionID, operation, beforeJSON, afterJSON, source, createdAt)
	return err
}

func marshalAuditValue(value any) (string, error) {
	if value == nil {
		return "", nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func normalizePortfolioTransaction(transaction portfolio.Transaction) portfolio.Transaction {
	transaction.Date = strings.TrimSpace(transaction.Date)
	transaction.Code = strings.ToUpper(strings.TrimSpace(transaction.Code))
	transaction.Action = strings.ToLower(strings.TrimSpace(transaction.Action))
	transaction.Comment = strings.TrimSpace(transaction.Comment)
	return transaction
}
