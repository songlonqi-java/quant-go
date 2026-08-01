package web

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

func (s *taskStore) createScheduledTask(ctx context.Context, kind, period string) (*Task, error) {
	if !validTaskKind(kind) || period == "" {
		return nil, fmt.Errorf("定时任务类型或周期无效")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var enabled bool
	var lastPeriod string
	if err := tx.QueryRowContext(ctx, `SELECT enabled, last_enqueued_period FROM web_schedules WHERE kind = ?`, kind).Scan(&enabled, &lastPeriod); err != nil {
		return nil, err
	}
	if !enabled || lastPeriod == period {
		return nil, nil
	}
	now := timestamp()
	result, err := tx.ExecContext(ctx, `
		INSERT INTO web_tasks(kind, status, created_at, message, trigger_source)
		SELECT ?, ?, ?, ?, 'schedule'
		WHERE NOT EXISTS (SELECT 1 FROM web_tasks WHERE kind = ? AND status IN (?, ?))`,
		kind, TaskQueued, now, "任务已进入队列", kind, TaskQueued, TaskRunning)
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
	if err := addEvent(ctx, tx, id, now, fmt.Sprintf("已创建%s任务（schedule）", taskKindLabel(kind))); err != nil {
		return nil, err
	}
	mark, err := tx.ExecContext(ctx, `
		UPDATE web_schedules SET last_enqueued_period = ?, updated_at = ?
		WHERE kind = ? AND enabled = 1 AND last_enqueued_period <> ?`, period, now, kind, period)
	if err != nil {
		return nil, err
	}
	marked, err := mark.RowsAffected()
	if err != nil {
		return nil, err
	}
	if marked != 1 {
		return nil, errors.New("定时设置已变化，取消本次入队")
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.task(ctx, id, false)
}

type Schedule struct {
	Kind               string
	Enabled            bool
	Hour               int
	Minute             int
	DayOfMonth         int
	Months             string
	Timezone           string
	LastEnqueuedPeriod string
	UpdatedAt          string
}

func (s *taskStore) schedules(ctx context.Context) ([]Schedule, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT kind, enabled, hour, minute, day_of_month, months, timezone, last_enqueued_period, updated_at
		FROM web_schedules ORDER BY CASE kind
			WHEN 'daily' THEN 1 WHEN 'value_monthly' THEN 2 WHEN 'value_quarterly' THEN 3 ELSE 4 END`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var schedules []Schedule
	for rows.Next() {
		var schedule Schedule
		if err := rows.Scan(&schedule.Kind, &schedule.Enabled, &schedule.Hour, &schedule.Minute,
			&schedule.DayOfMonth, &schedule.Months, &schedule.Timezone,
			&schedule.LastEnqueuedPeriod, &schedule.UpdatedAt); err != nil {
			return nil, err
		}
		schedules = append(schedules, schedule)
	}
	return schedules, rows.Err()
}

func (s *taskStore) updateSchedule(ctx context.Context, schedule Schedule) error {
	if !validTaskKind(schedule.Kind) {
		return fmt.Errorf("不支持的任务类型: %s", schedule.Kind)
	}
	if schedule.Hour < 0 || schedule.Hour > 23 || schedule.Minute < 0 || schedule.Minute > 59 {
		return fmt.Errorf("执行时间无效")
	}
	if schedule.DayOfMonth < 1 || schedule.DayOfMonth > 28 {
		return fmt.Errorf("日期必须在 1 到 28 之间")
	}
	if schedule.Kind == taskKindValueQuarterly {
		if _, err := parseScheduleMonths(schedule.Months); err != nil {
			return err
		}
	} else {
		schedule.Months = ""
	}
	if schedule.Timezone == "" {
		schedule.Timezone = "Asia/Shanghai"
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE web_schedules SET enabled = ?, hour = ?, minute = ?, day_of_month = ?,
			months = ?, timezone = ?, updated_at = ? WHERE kind = ?`,
		schedule.Enabled, schedule.Hour, schedule.Minute, schedule.DayOfMonth,
		schedule.Months, schedule.Timezone, timestamp(), schedule.Kind)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("定时设置不存在: %s", schedule.Kind)
	}
	return nil
}

func parseScheduleMonths(value string) (map[int]bool, error) {
	months := make(map[int]bool)
	for _, text := range strings.Split(value, ",") {
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		month, err := strconv.Atoi(text)
		if err != nil || month < 1 || month > 12 {
			return nil, fmt.Errorf("季度月份必须是 1 到 12 的逗号分隔数字")
		}
		months[month] = true
	}
	if len(months) == 0 {
		return nil, fmt.Errorf("季度月份不能为空")
	}
	return months, nil
}
