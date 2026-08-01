package web

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"quant/internal/portfolio"
)

type ReportFilter struct {
	Kind      string
	TradeDate string
	Status    TaskStatus
	Limit     int
}

type ReportRecord struct {
	ID                   int64
	TaskID               int64
	Kind                 string
	TaskStatus           TaskStatus
	ReportVersion        string
	TargetDate           string
	TradeDate            string
	GeneratedAt          string
	CodeVersion          string
	StrategyVersion      string
	DataVersion          string
	PortfolioSnapshotID  int64
	SnapshotTransactions int
	CreatedAt            string
	Report               *DailyReport
}

func persistReport(ctx context.Context, tx *sql.Tx, taskID int64, report *DailyReport, createdAt string) error {
	if report == nil {
		return nil
	}
	var kind string
	if err := tx.QueryRowContext(ctx, `SELECT kind FROM web_tasks WHERE id = ?`, taskID).Scan(&kind); err != nil {
		return err
	}
	var snapshotID any
	if report.SnapshotLedger != nil {
		ledgerJSON, err := json.Marshal(portfolio.Ledger{Transactions: report.SnapshotLedger})
		if err != nil {
			return fmt.Errorf("序列化持仓快照: %w", err)
		}
		digest := sha256.Sum256(ledgerJSON)
		hash := hex.EncodeToString(digest[:])
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO portfolio_snapshots(task_id, ledger_json, transaction_count, ledger_sha256, created_at)
			VALUES(?, ?, ?, ?, ?)
			ON CONFLICT(task_id) DO UPDATE SET
				ledger_json = excluded.ledger_json,
				transaction_count = excluded.transaction_count,
				ledger_sha256 = excluded.ledger_sha256`,
			taskID, string(ledgerJSON), len(report.SnapshotLedger), hash, createdAt); err != nil {
			return err
		}
		var id int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM portfolio_snapshots WHERE task_id = ?`, taskID).Scan(&id); err != nil {
			return err
		}
		snapshotID = id
	}
	reportJSON, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("序列化报告中心日报: %w", err)
	}
	generatedAt := ""
	if !report.GeneratedAt.IsZero() {
		generatedAt = report.GeneratedAt.Format("2006-01-02T15:04:05Z07:00")
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO web_reports(
			task_id, kind, report_version, target_date, trade_date, generated_at,
			code_version, strategy_version, data_version, portfolio_snapshot_id, report_json, created_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(task_id) DO UPDATE SET
			report_version = excluded.report_version,
			target_date = excluded.target_date,
			trade_date = excluded.trade_date,
			generated_at = excluded.generated_at,
			code_version = excluded.code_version,
			strategy_version = excluded.strategy_version,
			data_version = excluded.data_version,
			portfolio_snapshot_id = excluded.portfolio_snapshot_id,
			report_json = excluded.report_json`,
		taskID, kind, report.Version, report.TargetDate, report.TradeDate, generatedAt,
		report.CodeVersion, report.StrategyVersion, report.DataVersion, snapshotID, string(reportJSON), createdAt)
	return err
}

func (s *taskStore) reports(ctx context.Context, filter ReportFilter) ([]ReportRecord, error) {
	if filter.Limit <= 0 || filter.Limit > 200 {
		filter.Limit = 50
	}
	query := `
		SELECT r.id, r.task_id, r.kind, t.status, r.report_version, r.target_date, r.trade_date,
			r.generated_at, r.code_version, r.strategy_version, r.data_version, COALESCE(r.portfolio_snapshot_id, 0),
			COALESCE(p.transaction_count, 0), r.created_at
		FROM web_reports r
		JOIN web_tasks t ON t.id = r.task_id
		LEFT JOIN portfolio_snapshots p ON p.id = r.portfolio_snapshot_id
		WHERE 1 = 1`
	var args []any
	if filter.Kind != "" {
		query += ` AND r.kind = ?`
		args = append(args, filter.Kind)
	}
	if filter.TradeDate != "" {
		query += ` AND r.trade_date = ?`
		args = append(args, filter.TradeDate)
	}
	if filter.Status != "" {
		query += ` AND t.status = ?`
		args = append(args, filter.Status)
	}
	query += ` ORDER BY r.id DESC LIMIT ?`
	args = append(args, filter.Limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var reports []ReportRecord
	for rows.Next() {
		var report ReportRecord
		if err := rows.Scan(&report.ID, &report.TaskID, &report.Kind, &report.TaskStatus,
			&report.ReportVersion, &report.TargetDate, &report.TradeDate, &report.GeneratedAt,
			&report.CodeVersion, &report.StrategyVersion, &report.DataVersion, &report.PortfolioSnapshotID,
			&report.SnapshotTransactions, &report.CreatedAt); err != nil {
			return nil, err
		}
		reports = append(reports, report)
	}
	return reports, rows.Err()
}

func (s *taskStore) report(ctx context.Context, id int64) (*ReportRecord, error) {
	var record ReportRecord
	var reportJSON string
	err := s.db.QueryRowContext(ctx, `
		SELECT r.id, r.task_id, r.kind, t.status, r.report_version, r.target_date, r.trade_date,
			r.generated_at, r.code_version, r.strategy_version, r.data_version, COALESCE(r.portfolio_snapshot_id, 0),
			COALESCE(p.transaction_count, 0), r.created_at, r.report_json
		FROM web_reports r
		JOIN web_tasks t ON t.id = r.task_id
		LEFT JOIN portfolio_snapshots p ON p.id = r.portfolio_snapshot_id
		WHERE r.id = ?`, id).Scan(&record.ID, &record.TaskID, &record.Kind, &record.TaskStatus,
		&record.ReportVersion, &record.TargetDate, &record.TradeDate, &record.GeneratedAt,
		&record.CodeVersion, &record.StrategyVersion, &record.DataVersion, &record.PortfolioSnapshotID,
		&record.SnapshotTransactions, &record.CreatedAt, &reportJSON)
	if err != nil {
		return nil, err
	}
	var report DailyReport
	if err := json.Unmarshal([]byte(reportJSON), &report); err != nil {
		return nil, fmt.Errorf("解析报告: %w", err)
	}
	record.Report = &report
	return &record, nil
}

func (s *taskStore) reportSnapshot(ctx context.Context, snapshotID int64) (*portfolio.Ledger, error) {
	if snapshotID == 0 {
		return nil, nil
	}
	var ledgerJSON string
	err := s.db.QueryRowContext(ctx, `SELECT ledger_json FROM portfolio_snapshots WHERE id = ?`, snapshotID).Scan(&ledgerJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ledger portfolio.Ledger
	if err := json.Unmarshal([]byte(ledgerJSON), &ledger); err != nil {
		return nil, fmt.Errorf("解析持仓快照: %w", err)
	}
	return &ledger, nil
}

func validReportStatus(value string) TaskStatus {
	status := TaskStatus(strings.TrimSpace(value))
	switch status {
	case TaskSucceeded, TaskFailed:
		return status
	default:
		return ""
	}
}
