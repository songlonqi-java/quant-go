package data

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/parquet-go/parquet-go"
	_ "modernc.org/sqlite"
)

func ReadParquetDir(dir string) ([]DailyBar, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.parquet"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("目录 %s 中没有找到 parquet 文件", dir)
	}

	var allBars []DailyBar
	for _, f := range files {
		if strings.Contains(f, "stocks.parquet") {
			continue
		}
		bars, err := ReadParquetFile(f)
		if err != nil {
			return nil, fmt.Errorf("读取 %s 失败: %w", f, err)
		}
		allBars = append(allBars, bars...)
	}
	return allBars, nil
}

func ReadParquetFile(filepath string) ([]DailyBar, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var bars []DailyBar
	reader := parquet.NewReader(file, parquet.SchemaOf(&DailyBar{}))
	for {
		var bar DailyBar
		err := reader.Read(&bar)
		if err != nil {
			break
		}
		bars = append(bars, bar)
	}
	reader.Close()
	return bars, nil
}

func WriteParquetFile(filepath string, bars []DailyBar) error {
	dir := filepath[:strings.LastIndex(filepath, "/")]
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tmpPath := filepath + ".tmp"
	file, err := os.Create(tmpPath)
	if err != nil {
		return err
	}

	writer := parquet.NewWriter(file)
	for _, bar := range bars {
		if err := writer.Write(bar); err != nil {
			file.Close()
			os.Remove(tmpPath)
			return err
		}
	}
	if err := writer.Close(); err != nil {
		file.Close()
		os.Remove(tmpPath)
		return err
	}
	file.Close()

	if err := os.Rename(tmpPath, filepath); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

func WriteStocksParquet(filepath string, stocks []StockInfo) error {
	dir := filepath[:strings.LastIndex(filepath, "/")]
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tmpPath := filepath + ".tmp"
	file, err := os.Create(tmpPath)
	if err != nil {
		return err
	}

	writer := parquet.NewWriter(file)
	for _, s := range stocks {
		if err := writer.Write(s); err != nil {
			file.Close()
			os.Remove(tmpPath)
			return err
		}
	}
	if err := writer.Close(); err != nil {
		file.Close()
		os.Remove(tmpPath)
		return err
	}
	file.Close()
	return os.Rename(tmpPath, filepath)
}

func InitSQLite(dbPath string) error {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	schema := `
	CREATE TABLE IF NOT EXISTS daily_factors (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ts_code TEXT NOT NULL,
		trade_date TEXT NOT NULL,
		open REAL,
		high REAL,
		low REAL,
		close REAL,
		vol REAL,
		ma5 REAL,
		ma10 REAL,
		ma20 REAL,
		ma60 REAL,
		rsi14 REAL,
		macd_dif REAL,
		macd_dea REAL,
		macd_hist REAL,
		boll_upper REAL,
		boll_mid REAL,
		boll_lower REAL,
		volume_ratio REAL,
		signal TEXT,
		UNIQUE(ts_code, trade_date)
	);
	CREATE INDEX IF NOT EXISTS idx_code_date ON daily_factors(ts_code, trade_date);
	CREATE TABLE IF NOT EXISTS fetch_manifest (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		year INTEGER NOT NULL UNIQUE,
		row_count INTEGER,
		completed_at TEXT,
		status TEXT
	);
	`
	_, err = db.Exec(schema)
	return err
}

func GroupByCode(bars []DailyBar) map[string][]DailyBar {
	m := make(map[string][]DailyBar)
	for _, b := range bars {
		m[b.TsCode] = append(m[b.TsCode], b)
	}
	return m
}

func readGenericParquet[T any](filePath string) ([]T, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var sample T
	reader := parquet.NewReader(f, parquet.SchemaOf(&sample))
	defer reader.Close()

	var result []T
	for {
		var item T
		err := reader.Read(&item)
		if err != nil {
			break
		}
		result = append(result, item)
	}
	return result, nil
}

func writeGenericParquet[T any](path string, data []T) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	file, err := os.Create(tmpPath)
	if err != nil {
		return err
	}

	writer := parquet.NewWriter(file)
	for i := range data {
		if err := writer.Write(data[i]); err != nil {
			file.Close()
			os.Remove(tmpPath)
			return err
		}
	}
	if err := writer.Close(); err != nil {
		file.Close()
		os.Remove(tmpPath)
		return err
	}
	file.Close()
	return os.Rename(tmpPath, path)
}
