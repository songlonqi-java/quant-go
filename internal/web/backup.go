package web

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type BackupReport struct {
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at"`
	Size      int64     `json:"size_bytes"`
	Files     int       `json:"files"`
}

func (s *Server) createBackup(ctx context.Context, update func(string)) (*BackupReport, error) {
	if err := os.MkdirAll(s.backupDir, 0o700); err != nil {
		return nil, err
	}
	tempDir, err := os.MkdirTemp(s.backupDir, ".building-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)
	dbSnapshot := filepath.Join(tempDir, "web.db")
	update("创建 SQLite 一致性快照")
	if _, err := s.store.db.ExecContext(ctx, `VACUUM INTO ?`, dbSnapshot); err != nil {
		return nil, fmt.Errorf("备份 SQLite: %w", err)
	}
	portfolioYAML, err := s.store.exportPortfolioYAML(ctx)
	if err != nil {
		return nil, err
	}
	portfolioSnapshot := filepath.Join(tempDir, "portfolio.yaml")
	if err := os.WriteFile(portfolioSnapshot, portfolioYAML, 0o600); err != nil {
		return nil, err
	}
	stamp := time.Now().In(time.Local).Format("20060102-150405")
	finalPath := filepath.Join(s.backupDir, "quant-backup-"+stamp+".tar.gz")
	temporary := finalPath + ".tmp"
	update("归档 SQLite、市场数据和持仓 YAML")
	files, err := writeBackupArchive(temporary, []backupSource{
		{Path: dbSnapshot, ArchiveName: "web.db"},
		{Path: portfolioSnapshot, ArchiveName: "portfolio.yaml"},
		{Path: s.config.Data.RawDir, ArchiveName: "data/raw"},
	})
	if err != nil {
		_ = os.Remove(temporary)
		return nil, err
	}
	if err := os.Rename(temporary, finalPath); err != nil {
		_ = os.Remove(temporary)
		return nil, err
	}
	info, err := os.Stat(finalPath)
	if err != nil {
		return nil, err
	}
	if err := pruneBackups(s.backupDir, s.backupRetention); err != nil {
		return nil, err
	}
	return &BackupReport{Path: finalPath, CreatedAt: time.Now().UTC(), Size: info.Size(), Files: files}, nil
}

type backupSource struct{ Path, ArchiveName string }

func writeBackupArchive(path string, sources []backupSource) (int, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	count := 0
	for _, source := range sources {
		err := filepath.WalkDir(source.Path, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(source.Path, path)
			if err != nil {
				return err
			}
			name := source.ArchiveName
			if relative != "." {
				name = filepath.Join(name, relative)
			}
			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			header.Name = filepath.ToSlash(name)
			if err := tw.WriteHeader(header); err != nil {
				return err
			}
			input, err := os.Open(path)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(tw, input)
			closeErr := input.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
			count++
			return nil
		})
		if err != nil {
			tw.Close()
			gz.Close()
			file.Close()
			return 0, err
		}
	}
	if err := tw.Close(); err != nil {
		return 0, err
	}
	if err := gz.Close(); err != nil {
		return 0, err
	}
	if err := file.Close(); err != nil {
		return 0, err
	}
	return count, nil
}

func latestBackup(pattern string) (os.FileInfo, error) {
	paths, err := filepath.Glob(pattern)
	if err != nil || len(paths) == 0 {
		return nil, err
	}
	sort.Strings(paths)
	return os.Stat(paths[len(paths)-1])
}

func pruneBackups(dir string, retention int) error {
	if retention <= 0 {
		retention = 14
	}
	paths, err := filepath.Glob(filepath.Join(dir, "quant-backup-*.tar.gz"))
	if err != nil {
		return err
	}
	sort.Strings(paths)
	for len(paths) > retention {
		path := paths[0]
		if filepath.Dir(path) != filepath.Clean(dir) || !strings.HasPrefix(filepath.Base(path), "quant-backup-") {
			return fmt.Errorf("拒绝清理意外备份路径")
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		paths = paths[1:]
	}
	return nil
}
