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
var ErrPortfolioConflict = errors.New("交易记录已被修改，请刷新后重试")

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

type PortfolioAudit struct {
	ID            int64
	TransactionID int64
	Operation     string
	BeforeJSON    string
	AfterJSON     string
	Source        string
	CreatedAt     string
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

func (s *taskStore) updatePortfolioComment(ctx context.Context, id int64, version int, comment, source string) (*StoredTransaction, error) {
	comment = strings.TrimSpace(comment)
	if len([]rune(comment)) > 500 {
		return nil, fmt.Errorf("备注不能超过 500 个字符")
	}
	if source == "" {
		source = "web"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	before, err := portfolioTransactionByID(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if before.Status != "active" || before.Version != version {
		return nil, ErrPortfolioConflict
	}
	now := timestamp()
	result, err := tx.ExecContext(ctx, `
		UPDATE portfolio_transactions
		SET comment = ?, updated_at = ?, version = version + 1
		WHERE id = ? AND status = 'active' AND version = ?`, comment, now, id, version)
	if err != nil {
		return nil, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if changed != 1 {
		return nil, ErrPortfolioConflict
	}
	after := *before
	after.Trade.Comment = comment
	after.UpdatedAt = now
	after.Version++
	if err := insertPortfolioAudit(ctx, tx, id, "update_comment", before, &after, source, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &after, nil
}

func (s *taskStore) voidPortfolioTransaction(ctx context.Context, id int64, version int, source string) error {
	if source == "" {
		source = "web"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := portfolioTransactionByID(ctx, tx, id)
	if err != nil {
		return err
	}
	if before.Status != "active" || before.Version != version {
		return ErrPortfolioConflict
	}
	ledger, err := portfolioLedgerExcluding(ctx, tx, id)
	if err != nil {
		return err
	}
	if err := portfolio.ValidateLedger(ledger); err != nil {
		return fmt.Errorf("撤销后流水无效: %w", err)
	}
	now := timestamp()
	result, err := tx.ExecContext(ctx, `
		UPDATE portfolio_transactions
		SET status = 'void', updated_at = ?, version = version + 1
		WHERE id = ? AND status = 'active' AND version = ?`, now, id, version)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrPortfolioConflict
	}
	after := *before
	after.Status = "void"
	after.UpdatedAt = now
	after.Version++
	if err := insertPortfolioAudit(ctx, tx, id, "void", before, &after, source, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *taskStore) portfolioAudits(ctx context.Context, limit int) ([]PortfolioAudit, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, transaction_id, operation, before_json, after_json, source, created_at
		FROM portfolio_audit_logs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var audits []PortfolioAudit
	for rows.Next() {
		var audit PortfolioAudit
		if err := rows.Scan(&audit.ID, &audit.TransactionID, &audit.Operation, &audit.BeforeJSON,
			&audit.AfterJSON, &audit.Source, &audit.CreatedAt); err != nil {
			return nil, err
		}
		audits = append(audits, audit)
	}
	return audits, rows.Err()
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

func (s *taskStore) latestDailyReport(ctx context.Context) (*DailyReport, error) {
	var encoded string
	err := s.db.QueryRowContext(ctx, `
		SELECT report_json FROM web_tasks
		WHERE kind = ? AND status = ? AND report_json <> ''
		ORDER BY id DESC LIMIT 1`, taskKindDaily, TaskSucceeded).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var report DailyReport
	if err := json.Unmarshal([]byte(encoded), &report); err != nil {
		return nil, fmt.Errorf("解析最近日报: %w", err)
	}
	return &report, nil
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

func portfolioLedgerExcluding(ctx context.Context, queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, excludedID int64) (*portfolio.Ledger, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT trade_date, code, action, shares, price, comment
		FROM portfolio_transactions
		WHERE status = 'active' AND id <> ? ORDER BY trade_date, id`, excludedID)
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

func portfolioTransactionByID(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id int64) (*StoredTransaction, error) {
	var transaction StoredTransaction
	err := queryer.QueryRowContext(ctx, `
		SELECT id, trade_date, code, action, shares, price, comment, status, source, created_at, updated_at, version
		FROM portfolio_transactions WHERE id = ?`, id).Scan(
		&transaction.ID, &transaction.Trade.Date, &transaction.Trade.Code, &transaction.Trade.Action,
		&transaction.Trade.Shares, &transaction.Trade.Price, &transaction.Trade.Comment,
		&transaction.Status, &transaction.Source, &transaction.CreatedAt, &transaction.UpdatedAt, &transaction.Version)
	if err != nil {
		return nil, err
	}
	return &transaction, nil
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
