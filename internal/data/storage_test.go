package data

import (
	"path/filepath"
	"testing"
)

func TestReadParquetDirDeduplicatesByCodeAndDate(t *testing.T) {
	dir := t.TempDir()
	if err := WriteParquetFile(filepath.Join(dir, "2026.parquet"), []DailyBar{
		{TsCode: "000001.SZ", TradeDate: "20260729", Close: 10},
		{TsCode: "000002.SZ", TradeDate: "20260729", Close: 20},
	}); err != nil {
		t.Fatal(err)
	}
	if err := WriteParquetFile(filepath.Join(dir, "today_20260729.parquet"), []DailyBar{
		{TsCode: "000001.SZ", TradeDate: "20260729", Close: 11},
		{TsCode: "000003.SZ", TradeDate: "20260729", Close: 30},
	}); err != nil {
		t.Fatal(err)
	}

	bars, err := ReadParquetDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(bars) != 3 {
		t.Fatalf("len(bars) = %d, want 3", len(bars))
	}
	for _, bar := range bars {
		if bar.TsCode == "000001.SZ" && bar.TradeDate == "20260729" && bar.Close != 11 {
			t.Fatalf("duplicate bar close = %.2f, want today override 11", bar.Close)
		}
	}
}

func TestMergeDailyBarsOverridesAndSorts(t *testing.T) {
	merged := MergeDailyBars([]DailyBar{
		{TsCode: "000002.SZ", TradeDate: "20260730", Close: 20},
		{TsCode: "000001.SZ", TradeDate: "20260729", Close: 10},
	}, []DailyBar{
		{TsCode: "000001.SZ", TradeDate: "20260729", Close: 11},
		{TsCode: "000001.SZ", TradeDate: "20260730", Close: 12},
	})

	if len(merged) != 3 {
		t.Fatalf("len(merged) = %d, want 3", len(merged))
	}
	want := []struct {
		code  string
		date  string
		close float64
	}{
		{"000001.SZ", "20260729", 11},
		{"000001.SZ", "20260730", 12},
		{"000002.SZ", "20260730", 20},
	}
	for i, w := range want {
		if merged[i].TsCode != w.code || merged[i].TradeDate != w.date || merged[i].Close != w.close {
			t.Fatalf("merged[%d] = %s %s %.2f, want %s %s %.2f",
				i, merged[i].TsCode, merged[i].TradeDate, merged[i].Close, w.code, w.date, w.close)
		}
	}
}
