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
	store := &taskStore{db: db}
	if err := store.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *taskStore) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
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
	`)
	return err
}

func (s *taskStore) createDaily(ctx context.Context) (*Task, error) {
	var active int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM web_tasks WHERE status IN (?, ?)`, TaskQueued, TaskRunning).Scan(&active); err != nil {
		return nil, err
	}
	if active > 0 {
		return nil, ErrTaskAlreadyActive
	}
	now := timestamp()
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO web_tasks(kind, status, created_at, message) VALUES(?, ?, ?, ?)`,
		taskKindDaily, TaskQueued, now, "任务已进入队列")
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err := s.addEvent(ctx, id, now, "已创建日终任务"); err != nil {
		return nil, err
	}
	return s.task(ctx, id, false)
}

func (s *taskStore) claimNext(ctx context.Context) (*Task, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM web_tasks WHERE status = ? ORDER BY id LIMIT 1`, TaskQueued).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	now := timestamp()
	result, err := s.db.ExecContext(ctx, `
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
	if err := s.addEvent(ctx, id, now, "开始执行，数据写入任务将串行运行"); err != nil {
		return nil, err
	}
	return s.task(ctx, id, false)
}

func (s *taskStore) updateProgress(ctx context.Context, taskID int64, message string) error {
	if message == "" {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE web_tasks SET message = ? WHERE id = ?`, message, taskID); err != nil {
		return err
	}
	return s.addEvent(ctx, taskID, timestamp(), message)
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
	if _, err := s.db.ExecContext(ctx, `
		UPDATE web_tasks SET status = ?, finished_at = ?, message = ?, error = ?, report_json = ? WHERE id = ?`,
		status, now, message, errText, reportJSON, taskID); err != nil {
		return err
	}
	if errText != "" {
		message += "：" + errText
	}
	return s.addEvent(ctx, taskID, now, message)
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
	_, err := s.db.ExecContext(ctx, `INSERT INTO web_task_events(task_id, created_at, message) VALUES(?, ?, ?)`, taskID, createdAt, message)
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
