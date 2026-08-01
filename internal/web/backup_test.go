package web

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
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
