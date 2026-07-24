package data

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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

type PriceDataQuality struct {
	Total      int
	MissingRaw int
}

func CheckPriceDataQuality(bars []DailyBar) PriceDataQuality {
	q := PriceDataQuality{Total: len(bars)}
	for _, b := range bars {
		if !b.HasRawPrices() {
			q.MissingRaw++
		}
	}
	return q
}

func (q PriceDataQuality) HasCompleteRawPrices() bool {
	return q.Total > 0 && q.MissingRaw == 0
}

func (q PriceDataQuality) Summary() string {
	if q.Total == 0 {
		return "无行情数据"
	}
	return fmt.Sprintf("真实价字段缺失 %d/%d 行", q.MissingRaw, q.Total)
}

func ReadParquetFile(filepath string) ([]DailyBar, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var bars []DailyBar
	reader := parquet.NewReader(file, parquet.SchemaOf(&DailyBar{}))
	defer reader.Close()
	for {
		var bar DailyBar
		err := reader.Read(&bar)
		if err == io.EOF {
			break
		}
		if err != nil {
			return bars, fmt.Errorf("parquet读取错误 %s: %w", filepath, err)
		}
		bars = append(bars, bar)
	}
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

func TradingDatesFromBars(bars []DailyBar) []string {
	seen := make(map[string]bool)
	for _, b := range bars {
		if b.TradeDate != "" {
			seen[b.TradeDate] = true
		}
	}
	dates := make([]string, 0, len(seen))
	for d := range seen {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	return dates
}

func LoadTradeDates(rawDir string, fallbackBars []DailyBar) []string {
	calPath := filepath.Join(rawDir, "trade_cal.parquet")
	cal, err := readGenericParquet[TradeCal](calPath)
	if err == nil && len(cal) > 0 {
		return TradingDays(cal)
	}
	return TradingDatesFromBars(fallbackBars)
}

func LoadStockNames(path string) map[string]string {
	names := make(map[string]string)
	f, err := os.Open(path)
	if err != nil {
		return names
	}
	defer f.Close()

	reader := parquet.NewReader(f, parquet.SchemaOf(&StockInfo{}))
	defer reader.Close()

	for {
		var s StockInfo
		if err := reader.Read(&s); err != nil {
			break
		}
		if s.Name != "" {
			names[s.TsCode] = s.Name
		}
	}
	return names
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
		if err == io.EOF {
			break
		}
		if err != nil {
			return result, fmt.Errorf("parquet读取错误 %s: %w", filePath, err)
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

func writeMergedGenericParquet[T any](path string, incoming []T, keyFn func(T) string) error {
	if len(incoming) == 0 {
		return nil
	}
	merged := make(map[string]T)
	if existing, err := readGenericParquet[T](path); err == nil {
		for _, item := range existing {
			merged[keyFn(item)] = item
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	for _, item := range incoming {
		merged[keyFn(item)] = item
	}

	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]T, 0, len(keys))
	for _, key := range keys {
		out = append(out, merged[key])
	}
	return writeGenericParquet(path, out)
}
