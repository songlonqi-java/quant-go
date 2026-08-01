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
	if err := validateBackupLocation(s.config.Data.RawDir, s.backupDir); err != nil {
		return nil, err
	}
	tempDir, err := os.MkdirTemp(s.backupDir, ".building-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)
	dbSnapshot := filepath.Join(tempDir, "web.db")
	if update != nil {
		update("创建 SQLite 一致性快照")
	}
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
	stamp := time.Now().In(time.Local).Format("20060102-150405.000000000")
	finalPath := filepath.Join(s.backupDir, "quant-backup-"+stamp+".tar.gz")
	temporary := finalPath + ".tmp"
	if update != nil {
		update("归档 SQLite、市场数据和持仓 YAML")
	}
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

// validateBackupLocation prevents the output archive from being discovered
// while rawDir is being walked. Without this guard, placing backupDir inside
// rawDir would make the archive read its own growing temporary file.
func validateBackupLocation(rawDir, backupDir string) error {
	raw, err := canonicalPath(rawDir)
	if err != nil {
		return fmt.Errorf("解析市场数据目录: %w", err)
	}
	backup, err := canonicalPath(backupDir)
	if err != nil {
		return fmt.Errorf("解析备份目录: %w", err)
	}
	if pathContains(raw, backup) || pathContains(backup, raw) {
		return fmt.Errorf("backup.dir 与 data.raw_dir 不能相同或互相包含")
	}
	return nil
}

func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return filepath.Clean(resolved), nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return abs, nil
}

func pathContains(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

type backupSource struct{ Path, ArchiveName string }

func writeBackupArchive(path string, sources []backupSource) (count int, returnErr error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	defer func() {
		for _, close := range []func() error{tw.Close, gz.Close, file.Close} {
			if err := close(); returnErr == nil && err != nil {
				returnErr = err
			}
		}
	}()
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
			return 0, err
		}
	}
	return count, nil
}

func latestBackup(dir string) (os.FileInfo, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() && strings.HasPrefix(name, "quant-backup-") && strings.HasSuffix(name, ".tar.gz") {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil, nil
	}
	sort.Strings(names)
	return os.Stat(filepath.Join(dir, names[len(names)-1]))
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
