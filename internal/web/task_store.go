package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const taskKindDaily = "daily"

type TaskStatus string

const (
	TaskQueued    TaskStatus = "queued"
	TaskRunning   TaskStatus = "running"
	TaskSucceeded TaskStatus = "succeeded"
	TaskFailed    TaskStatus = "failed"
)

var ErrTaskAlreadyActive = errors.New("已有日终任务正在排队或运行")

type Task struct {
	ID         int64
	Kind       string
	Status     TaskStatus
	CreatedAt  string
	StartedAt  string
	FinishedAt string
	Message    string
	Error      string
	Report     *DailyReport
	Events     []TaskEvent
}

type TaskEvent struct {
	CreatedAt string
	Message   string
}

// taskStore is private to the Web module so its schema can evolve without
// coupling raw market data storage to page/task concerns.
type taskStore struct {
	db *sql.DB
}

type schemaMigration struct {
	version int
	name    string
	sql     string
}

var schemaMigrations = []schemaMigration{
	{
		version: 1,
		name:    "initial task tables",
		sql: `
			CREATE TABLE IF NOT EXISTS web_tasks (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				kind TEXT NOT NULL,
				status TEXT NOT NULL,
				created_at TEXT NOT NULL,
				started_at TEXT NOT NULL DEFAULT '',
				finished_at TEXT NOT NULL DEFAULT '',
				message TEXT NOT NULL DEFAULT '',
				error TEXT NOT NULL DEFAULT '',
				report_json TEXT NOT NULL DEFAULT ''
			);
			CREATE INDEX IF NOT EXISTS idx_web_tasks_status_created ON web_tasks(status, created_at DESC);
			CREATE TABLE IF NOT EXISTS web_task_events (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				task_id INTEGER NOT NULL,
				created_at TEXT NOT NULL,
				message TEXT NOT NULL,
				FOREIGN KEY(task_id) REFERENCES web_tasks(id)
			);
			CREATE INDEX IF NOT EXISTS idx_web_task_events_task_id ON web_task_events(task_id, id);
		`,
	},
	{
		version: 2,
		name:    "portfolio transactions and audit log",
		sql: `
			CREATE TABLE IF NOT EXISTS portfolio_transactions (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				trade_date TEXT NOT NULL,
				code TEXT NOT NULL,
				action TEXT NOT NULL CHECK(action IN ('buy', 'sell')),
				shares REAL NOT NULL CHECK(shares > 0),
				price REAL NOT NULL CHECK(price > 0),
				comment TEXT NOT NULL DEFAULT '',
				status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active', 'void')),
				source TEXT NOT NULL DEFAULT 'web',
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				version INTEGER NOT NULL DEFAULT 1
			);
			CREATE INDEX IF NOT EXISTS idx_portfolio_transactions_date_id
				ON portfolio_transactions(trade_date, id);
			CREATE INDEX IF NOT EXISTS idx_portfolio_transactions_code_status
				ON portfolio_transactions(code, status);
			CREATE TABLE IF NOT EXISTS portfolio_audit_logs (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				transaction_id INTEGER,
				operation TEXT NOT NULL,
				before_json TEXT NOT NULL DEFAULT '',
				after_json TEXT NOT NULL DEFAULT '',
				source TEXT NOT NULL,
				created_at TEXT NOT NULL,
				FOREIGN KEY(transaction_id) REFERENCES portfolio_transactions(id)
			);
			CREATE INDEX IF NOT EXISTS idx_portfolio_audit_transaction
				ON portfolio_audit_logs(transaction_id, id);
			CREATE TABLE IF NOT EXISTS portfolio_imports (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				source_path TEXT NOT NULL,
				source_sha256 TEXT NOT NULL UNIQUE,
				transaction_count INTEGER NOT NULL,
				imported_at TEXT NOT NULL
			);
		`,
	},
	{
		version: 3,
		name:    "report center and portfolio snapshots",
		sql: `
			CREATE TABLE IF NOT EXISTS portfolio_snapshots (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				task_id INTEGER NOT NULL UNIQUE,
				ledger_json TEXT NOT NULL,
				transaction_count INTEGER NOT NULL,
				ledger_sha256 TEXT NOT NULL,
				created_at TEXT NOT NULL,
				FOREIGN KEY(task_id) REFERENCES web_tasks(id)
			);
			CREATE TABLE IF NOT EXISTS web_reports (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				task_id INTEGER NOT NULL UNIQUE,
				kind TEXT NOT NULL,
				report_version TEXT NOT NULL,
				target_date TEXT NOT NULL DEFAULT '',
				trade_date TEXT NOT NULL DEFAULT '',
				generated_at TEXT NOT NULL DEFAULT '',
				code_version TEXT NOT NULL DEFAULT '',
				strategy_version TEXT NOT NULL DEFAULT '',
				data_version TEXT NOT NULL DEFAULT '',
				portfolio_snapshot_id INTEGER,
				report_json TEXT NOT NULL,
				created_at TEXT NOT NULL,
				FOREIGN KEY(task_id) REFERENCES web_tasks(id),
				FOREIGN KEY(portfolio_snapshot_id) REFERENCES portfolio_snapshots(id)
			);
			CREATE INDEX IF NOT EXISTS idx_web_reports_trade_date_id
				ON web_reports(trade_date DESC, id DESC);
			CREATE INDEX IF NOT EXISTS idx_web_reports_kind_created
				ON web_reports(kind, created_at DESC);
			INSERT OR IGNORE INTO web_reports(
				task_id, kind, report_version, target_date, trade_date, generated_at,
				code_version, strategy_version, data_version, report_json, created_at
			)
			SELECT id, kind,
				COALESCE(json_extract(report_json, '$.version'), 'daily-report-v1'),
				COALESCE(json_extract(report_json, '$.target_date'), ''),
				COALESCE(json_extract(report_json, '$.trade_date'), ''),
				COALESCE(json_extract(report_json, '$.generated_at'), ''),
				'legacy', 'legacy', COALESCE(json_extract(report_json, '$.trade_date'), ''),
				report_json, created_at
			FROM web_tasks WHERE report_json <> '';
		`,
	},
}

func openTaskStore(path string) (*taskStore, error) {
	if path == "" {
		return nil, fmt.Errorf("任务数据库路径不能为空")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("创建任务数据库目录: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// A single connection matches the single-worker execution policy and also
	// avoids lock contention when progress messages are stored during a run.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA foreign_keys = ON; PRAGMA busy_timeout = 5000; PRAGMA journal_mode = WAL;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("配置任务数据库: %w", err)
	}
	store := &taskStore{db: db}
	if err := store.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *taskStore) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TEXT NOT NULL
		)`); err != nil {
		return err
	}
	for _, migration := range schemaMigrations {
		var applied int
		err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM schema_migrations WHERE version = ?`, migration.version).Scan(&applied)
		if err != nil {
			return err
		}
		if applied > 0 {
			continue
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, migration.sql); err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, name, applied_at) VALUES(?, ?, ?)`,
				migration.version, migration.name, timestamp())
		}
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("执行数据库迁移 %d (%s): %w", migration.version, migration.name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("提交数据库迁移 %d (%s): %w", migration.version, migration.name, err)
		}
	}
	return nil
}

func (s *taskStore) createDaily(ctx context.Context) (*Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := timestamp()
	result, err := tx.ExecContext(ctx, `
		INSERT INTO web_tasks(kind, status, created_at, message)
		SELECT ?, ?, ?, ?
		WHERE NOT EXISTS (
			SELECT 1 FROM web_tasks WHERE kind = ? AND status IN (?, ?)
		)`, taskKindDaily, TaskQueued, now, "任务已进入队列", taskKindDaily, TaskQueued, TaskRunning)
	if err != nil {
		return nil, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if changed == 0 {
		return nil, ErrTaskAlreadyActive
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err := addEvent(ctx, tx, id, now, "已创建日终任务"); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.task(ctx, id, false)
}

// recoverInterrupted marks tasks left running by an unclean shutdown as
// failed. Queued tasks are intentionally preserved and will be claimed by the
// new worker after startup.
func (s *taskStore) recoverInterrupted(ctx context.Context) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id FROM web_tasks WHERE status = ? ORDER BY id`, TaskRunning)
	if err != nil {
		return 0, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	now := timestamp()
	const message = "服务重启，任务已中断"
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `
			UPDATE web_tasks SET status = ?, finished_at = ?, message = ?, error = ?
			WHERE id = ? AND status = ?`, TaskFailed, now, message, message, id, TaskRunning); err != nil {
			return 0, err
		}
		if err := addEvent(ctx, tx, id, now, message); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int64(len(ids)), nil
}

func (s *taskStore) claimNext(ctx context.Context) (*Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM web_tasks WHERE status = ? ORDER BY id LIMIT 1`, TaskQueued).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	now := timestamp()
	result, err := tx.ExecContext(ctx, `
		UPDATE web_tasks SET status = ?, started_at = ?, message = ? WHERE id = ? AND status = ?`,
		TaskRunning, now, "任务开始运行", id, TaskQueued)
	if err != nil {
		return nil, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if changed == 0 {
		return nil, nil
	}
	if err := addEvent(ctx, tx, id, now, "开始执行，数据写入任务将串行运行"); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.task(ctx, id, false)
}

func (s *taskStore) updateProgress(ctx context.Context, taskID int64, message string) error {
	if message == "" {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE web_tasks SET message = ? WHERE id = ?`, message, taskID); err != nil {
		return err
	}
	if err := addEvent(ctx, tx, taskID, timestamp(), message); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *taskStore) finish(ctx context.Context, taskID int64, report *DailyReport, runErr error) error {
	status := TaskSucceeded
	message := "任务完成，报告已保存"
	errText := ""
	if runErr != nil {
		status = TaskFailed
		message = "任务失败"
		errText = runErr.Error()
	}
	reportJSON := ""
	if report != nil {
		encoded, err := json.Marshal(report)
		if err != nil {
			return fmt.Errorf("序列化日报: %w", err)
		}
		reportJSON = string(encoded)
	}
	now := timestamp()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		UPDATE web_tasks SET status = ?, finished_at = ?, message = ?, error = ?, report_json = ? WHERE id = ?`,
		status, now, message, errText, reportJSON, taskID); err != nil {
		return err
	}
	if report != nil {
		if err := persistReport(ctx, tx, taskID, taskKindDaily, report, now); err != nil {
			return err
		}
	}
	if errText != "" {
		message += "：" + errText
	}
	if err := addEvent(ctx, tx, taskID, now, message); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *taskStore) task(ctx context.Context, id int64, withEvents bool) (*Task, error) {
	task, err := scanTask(s.db.QueryRowContext(ctx, `
		SELECT id, kind, status, created_at, started_at, finished_at, message, error, report_json
		FROM web_tasks WHERE id = ?`, id))
	if err != nil {
		return nil, err
	}
	if withEvents {
		events, err := s.events(ctx, id)
		if err != nil {
			return nil, err
		}
		task.Events = events
	}
	return task, nil
}

func (s *taskStore) list(ctx context.Context, limit int) ([]Task, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, kind, status, created_at, started_at, finished_at, message, error, report_json
		FROM web_tasks ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []Task
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, *task)
	}
	return tasks, rows.Err()
}

func (s *taskStore) events(ctx context.Context, taskID int64) ([]TaskEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT created_at, message FROM web_task_events WHERE task_id = ? ORDER BY id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []TaskEvent
	for rows.Next() {
		var event TaskEvent
		if err := rows.Scan(&event.CreatedAt, &event.Message); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *taskStore) addEvent(ctx context.Context, taskID int64, createdAt, message string) error {
	return addEvent(ctx, s.db, taskID, createdAt, message)
}

type sqlExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func addEvent(ctx context.Context, execer sqlExecer, taskID int64, createdAt, message string) error {
	_, err := execer.ExecContext(ctx, `INSERT INTO web_task_events(task_id, created_at, message) VALUES(?, ?, ?)`, taskID, createdAt, message)
	return err
}

func (s *taskStore) close() error {
	return s.db.Close()
}

type taskScanner interface {
	Scan(dest ...any) error
}

func scanTask(scanner taskScanner) (*Task, error) {
	var task Task
	var reportJSON string
	if err := scanner.Scan(&task.ID, &task.Kind, &task.Status, &task.CreatedAt, &task.StartedAt,
		&task.FinishedAt, &task.Message, &task.Error, &reportJSON); err != nil {
		return nil, err
	}
	if reportJSON != "" {
		var report DailyReport
		if err := json.Unmarshal([]byte(reportJSON), &report); err != nil {
			return nil, fmt.Errorf("解析任务报告: %w", err)
		}
		task.Report = &report
	}
	return &task, nil
}

func timestamp() string {
	return time.Now().In(time.Local).Format(time.RFC3339)
}
