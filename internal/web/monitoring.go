package web

import (
	"context"
	"path/filepath"
	"syscall"

	"quant/internal/data"
	"quant/internal/value"
)

type MonitoringStatus struct {
	ValueReadiness   *value.Readiness
	ValueError       string
	CalendarLatest   string
	CalendarReady    bool
	DiskFreeGB       float64
	RecentFailed     int
	LatestBackup     string
	LatestBackupSize int64
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
	}
	_ = s.store.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM web_tasks WHERE status = ? AND created_at >= datetime('now', '-7 days')`, TaskFailed).Scan(&status.RecentFailed)
	var stat syscall.Statfs_t
	if syscall.Statfs(s.config.Data.RawDir, &stat) == nil {
		status.DiskFreeGB = float64(stat.Bavail) * float64(stat.Bsize) / (1024 * 1024 * 1024)
	}
	if backup, err := latestBackup(filepath.Join(s.backupDir, "*.tar.gz")); err == nil && backup != nil {
		status.LatestBackup = backup.Name()
		status.LatestBackupSize = backup.Size()
	}
	return status
}
