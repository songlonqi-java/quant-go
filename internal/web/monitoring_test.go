package web

import (
	"context"
	"path/filepath"
	"testing"

	"quant/internal/config"
)

func TestMonitoringReportsUnknownInsteadOfHealthyOnCheckFailures(t *testing.T) {
	root := t.TempDir()
	store, err := openTaskStore(filepath.Join(root, "web.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	server := &Server{store: store, config: &config.Config{Data: config.DataConfig{RawDir: filepath.Join(root, "missing")}}, backupDir: filepath.Join(root, "backups")}
	status := server.monitoringStatus(context.Background())
	if status.DiskKnown || status.DiskError == "" || status.FailedTasksKnown || status.FailedTasksError == "" || status.CalendarError == "" {
		t.Fatalf("monitoring should expose unknown states: %+v", status)
	}
}
