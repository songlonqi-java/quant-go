package web

import (
	"context"
	"fmt"
	"syscall"
	"time"

	"quant/internal/data"
	"quant/internal/value"
)

type MonitoringStatus struct {
	ValueReadiness   *value.Readiness
	ValueError       string
	CalendarLatest   string
	CalendarReady    bool
	CalendarError    string
	DiskFreeGB       float64
	DiskKnown        bool
	DiskError        string
	RecentFailed     int
	FailedTasksKnown bool
	FailedTasksError string
	LatestBackup     string
	LatestBackupSize int64
	BackupError      string
}

func (s *Server) monitoringStatus(ctx context.Context) MonitoringStatus {
	status := MonitoringStatus{}
	readiness, err := value.CheckReadiness(s.config.Data.RawDir)
	if err != nil {
		status.ValueError = err.Error()
	} else {
		status.ValueReadiness = readiness
	}
	dates := data.LoadTradeDates(s.config.Data.RawDir, nil)
	if len(dates) > 0 {
		status.CalendarLatest = dates[len(dates)-1]
		status.CalendarReady = true
	} else {
		status.CalendarError = "交易日历缺失或无法读取"
	}
	cutoff := time.Now().In(time.Local).Add(-7 * 24 * time.Hour).Format(time.RFC3339)
	if err := s.store.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM web_tasks WHERE status = ? AND created_at >= ?`, TaskFailed, cutoff).Scan(&status.RecentFailed); err != nil {
		status.FailedTasksError = err.Error()
	} else {
		status.FailedTasksKnown = true
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(s.config.Data.RawDir, &stat); err == nil {
		status.DiskFreeGB = float64(stat.Bavail) * float64(stat.Bsize) / (1024 * 1024 * 1024)
		status.DiskKnown = true
	} else {
		status.DiskError = err.Error()
	}
	if backup, err := latestBackup(s.backupDir); err == nil && backup != nil {
		status.LatestBackup = backup.Name()
		status.LatestBackupSize = backup.Size()
	} else if err != nil {
		status.BackupError = fmt.Sprintf("读取备份目录失败: %v", err)
	}
	return status
}
