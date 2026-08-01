package web

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"testing"

	"quant/internal/config"
)

func TestWriteBackupArchiveAndPrune(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "raw")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "data.txt"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "quant-backup-20260101-000000.tar.gz")
	count, err := writeBackupArchive(archive, []backupSource{{Path: source, ArchiveName: "data/raw"}})
	if err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	f, _ := os.Open(archive)
	defer f.Close()
	gz, _ := gzip.NewReader(f)
	defer gz.Close()
	header, err := tar.NewReader(gz).Next()
	if err != nil || header.Name != "data/raw/data.txt" {
		t.Fatalf("header=%v err=%v", header, err)
	}
	second := filepath.Join(dir, "quant-backup-20260102-000000.tar.gz")
	if err := os.WriteFile(second, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := pruneBackups(dir, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(archive); !os.IsNotExist(err) {
		t.Fatalf("old backup was not pruned: %v", err)
	}
	if _, err := os.Stat(second); err != nil {
		t.Fatal(err)
	}
}

func TestBackupRejectsOverlappingDataDirectory(t *testing.T) {
	rawDir := filepath.Join(t.TempDir(), "raw")
	if err := os.MkdirAll(filepath.Join(rawDir, "backups"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := validateBackupLocation(rawDir, filepath.Join(rawDir, "backups")); err == nil {
		t.Fatal("expected overlapping backup directory to be rejected")
	}
}

func TestCreateBackupProducesRestorableDatabaseAndRawData(t *testing.T) {
	root := t.TempDir()
	rawDir := filepath.Join(root, "raw")
	backupDir := filepath.Join(root, "backups")
	if err := os.MkdirAll(rawDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rawDir, "sample.txt"), []byte("market-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := openTaskStore(filepath.Join(root, "web.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	server := &Server{store: store, config: &config.Config{Data: config.DataConfig{RawDir: rawDir}}, backupDir: backupDir, backupRetention: 2}
	report, err := server.createBackup(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	restoreDir := filepath.Join(root, "restore")
	if err := extractTestArchive(report.Path, restoreDir); err != nil {
		t.Fatal(err)
	}
	restoredDB, err := sql.Open("sqlite", filepath.Join(restoreDir, "web.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restoredDB.Close()
	var migrations int
	if err := restoredDB.QueryRow(`SELECT COUNT(1) FROM schema_migrations`).Scan(&migrations); err != nil || migrations != len(schemaMigrations) {
		t.Fatalf("restored migrations=%d err=%v", migrations, err)
	}
	contents, err := os.ReadFile(filepath.Join(restoreDir, "data", "raw", "sample.txt"))
	if err != nil || string(contents) != "market-data" {
		t.Fatalf("restored raw data=%q err=%v", contents, err)
	}
}

func extractTestArchive(path, destination string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(destination, filepath.FromSlash(header.Name))
		if !pathContains(destination, target) {
			return os.ErrInvalid
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(output, reader)
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
}
