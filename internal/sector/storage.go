package sector

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"quant/internal/data"

	"github.com/parquet-go/parquet-go"
)

func LoadIndustryMemberships(rawDir string) (MembershipStore, error) {
	path := filepath.Join(rawDir, "stocks.parquet")
	f, err := os.Open(path)
	if err != nil {
		return MembershipStore{}, err
	}
	defer f.Close()

	reader := parquet.NewReader(f, parquet.SchemaOf(&data.StockInfo{}))
	defer reader.Close()

	var stocks []data.StockInfo
	for {
		var stock data.StockInfo
		err := reader.Read(&stock)
		if err == io.EOF {
			break
		}
		if err != nil {
			return MembershipStore{}, fmt.Errorf("读取 %s 失败: %w", path, err)
		}
		stocks = append(stocks, stock)
	}
	return NewIndustryMemberships(stocks), nil
}

func WriteSectorDaily(rawDir string, rows []data.SectorDaily) error {
	byYear := make(map[string][]data.SectorDaily)
	for _, row := range rows {
		if len(row.TradeDate) < 4 {
			continue
		}
		year := row.TradeDate[:4]
		byYear[year] = append(byYear[year], row)
	}
	for year, yearRows := range byYear {
		path := filepath.Join(rawDir, "sector_daily", year+".parquet")
		if err := writeMergedSectorDaily(path, yearRows); err != nil {
			return err
		}
	}
	return nil
}

func LoadSectorDaily(rawDir string, startDate, endDate string) ([]data.SectorDaily, error) {
	dir := filepath.Join(rawDir, "sector_daily")
	files, err := filepath.Glob(filepath.Join(dir, "*.parquet"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	var out []data.SectorDaily
	for _, file := range files {
		rows, err := readSectorDailyFile(file)
		if err != nil {
			return out, err
		}
		for _, row := range rows {
			if startDate != "" && row.TradeDate < startDate {
				continue
			}
			if endDate != "" && row.TradeDate > endDate {
				continue
			}
			out = append(out, row)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TradeDate == out[j].TradeDate {
			if out[i].SectorType == out[j].SectorType {
				return out[i].SectorCode < out[j].SectorCode
			}
			return out[i].SectorType < out[j].SectorType
		}
		return out[i].TradeDate < out[j].TradeDate
	})
	return out, nil
}

func LoadReport(rawDir, tradeDate string) (*Report, error) {
	rows, err := LoadSectorDaily(rawDir, tradeDate, tradeDate)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return NewReport(rows), nil
}

func readSectorDailyFile(path string) ([]data.SectorDaily, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := parquet.NewReader(f, parquet.SchemaOf(&data.SectorDaily{}))
	defer reader.Close()

	var rows []data.SectorDaily
	for {
		var row data.SectorDaily
		err := reader.Read(&row)
		if err == io.EOF {
			break
		}
		if err != nil {
			return rows, fmt.Errorf("parquet读取错误 %s: %w", path, err)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func writeMergedSectorDaily(path string, incoming []data.SectorDaily) error {
	if len(incoming) == 0 {
		return nil
	}
	merged := make(map[string]data.SectorDaily)
	if existing, err := readSectorDailyFile(path); err == nil {
		for _, row := range existing {
			merged[sectorDailyKey(row)] = row
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	for _, row := range incoming {
		merged[sectorDailyKey(row)] = row
	}

	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]data.SectorDaily, 0, len(keys))
	for _, key := range keys {
		out = append(out, merged[key])
	}
	return writeSectorDailyFile(path, out)
}

func writeSectorDailyFile(path string, rows []data.SectorDaily) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return err
	}

	writer := parquet.NewWriter(f)
	for _, row := range rows {
		if err := writer.Write(row); err != nil {
			f.Close()
			os.Remove(tmpPath)
			return err
		}
	}
	if err := writer.Close(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}

func sectorDailyKey(row data.SectorDaily) string {
	return strings.Join([]string{row.TradeDate, row.SectorType, row.SectorCode}, "|")
}
