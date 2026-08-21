package dataset

import (
	"path/filepath"
	"testing"

	"quant/internal/data"
)

func TestLoadFiltersLatestAndST(t *testing.T) {
	rawDir := filepath.Join(t.TempDir(), "raw")
	if err := data.WriteParquetFile(filepath.Join(rawDir, "daily", "2026.parquet"), []data.DailyBar{
		testBar("000001.SZ", "20260101", 10),
		testBar("000001.SZ", "20260102", 11),
		testBar("000002.SZ", "20260101", 20),
		testBar("000003.SZ", "20260102", 30),
	}); err != nil {
		t.Fatalf("WriteParquetFile() error = %v", err)
	}
	if err := data.WriteStocksParquet(filepath.Join(rawDir, "stocks.parquet"), []data.StockInfo{
		{TsCode: "000001.SZ", Name: "平安银行"},
		{TsCode: "000002.SZ", Name: "旧数据"},
		{TsCode: "000003.SZ", Name: "ST测试"},
	}); err != nil {
		t.Fatalf("WriteStocksParquet() error = %v", err)
	}

	ds, err := Load(LoadOptions{RawDir: rawDir, LatestOnly: true, FilterST: true})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if ds.LatestDate != "20260102" {
		t.Fatalf("LatestDate = %s, want 20260102", ds.LatestDate)
	}
	if ds.SkippedStale != 1 {
		t.Fatalf("SkippedStale = %d, want 1", ds.SkippedStale)
	}
	if ds.FilteredST != 1 {
		t.Fatalf("FilteredST = %d, want 1", ds.FilteredST)
	}
	if len(ds.CodeMap) != 1 {
		t.Fatalf("len(CodeMap) = %d, want 1", len(ds.CodeMap))
	}
	if _, ok := ds.CodeMap["000001.SZ"]; !ok {
		t.Fatalf("remaining codes = %v, want 000001.SZ", ds.CodeMap)
	}
	if len(ds.AllCodeMap) != 3 || ds.AllCodeMap["000002.SZ"] == nil || ds.AllCodeMap["000003.SZ"] == nil {
		t.Fatalf("AllCodeMap = %v, want stale and ST securities retained for monitoring", ds.AllCodeMap)
	}
}

func TestFilterByMarketCapAndActiveBarsUseSameUniverse(t *testing.T) {
	store := data.NewFundamentalStore()
	store.LoadDailyBasics([]data.DailyBasic{
		{TsCode: "000001.SZ", TradeDate: "20260102", TotalMv: 1500000},
		{TsCode: "000002.SZ", TradeDate: "20260102", TotalMv: 500000},
	})
	large := []data.DailyBar{
		testBar("000001.SZ", "20260101", 10),
		testBar("000001.SZ", "20260102", 11),
	}
	small := []data.DailyBar{
		testBar("000002.SZ", "20260101", 10),
		testBar("000002.SZ", "20260102", 9),
	}
	ds := &Dataset{
		CodeMap:      map[string][]data.DailyBar{"000001.SZ": large, "000002.SZ": small},
		AllCodeMap:   map[string][]data.DailyBar{"000001.SZ": large, "000002.SZ": small},
		Fundamentals: store,
		LatestDate:   "20260102",
	}

	ds.filterByMarketCap(100)

	if len(ds.CodeMap) != 1 || ds.CodeMap["000001.SZ"] == nil {
		t.Fatalf("CodeMap = %v, want only 000001.SZ", ds.CodeMap)
	}
	if ds.FilteredMarketCap != 1 {
		t.Fatalf("FilteredMarketCap = %d, want 1", ds.FilteredMarketCap)
	}
	active := ds.ActiveBars()
	if len(active) != len(large) {
		t.Fatalf("len(ActiveBars()) = %d, want %d", len(active), len(large))
	}
	for _, bar := range active {
		if bar.TsCode != "000001.SZ" {
			t.Fatalf("active bar code = %s, want only 000001.SZ", bar.TsCode)
		}
	}
}

func testBar(code, date string, close float64) data.DailyBar {
	return data.DailyBar{
		TsCode:    code,
		TradeDate: date,
		Open:      close,
		High:      close,
		Low:       close,
		Close:     close,
		Vol:       1000,
		RawOpen:   close,
		RawHigh:   close,
		RawLow:    close,
		RawClose:  close,
		AdjFactor: 1,
	}
}
