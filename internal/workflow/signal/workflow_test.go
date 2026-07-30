package signalworkflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"quant/internal/config"
	"quant/internal/data"
	"quant/internal/dataset"
	"quant/internal/news"
	"quant/internal/realtime"
)

func TestLoadPortfolioSummaryIgnoresMissingPortfolioFile(t *testing.T) {
	summary, err := loadPortfolioSummary(filepath.Join(t.TempDir(), "missing.yaml"), &dataset.Dataset{})

	if err != nil {
		t.Fatalf("loadPortfolioSummary() error = %v, want nil", err)
	}
	if summary != nil {
		t.Fatalf("summary = %+v, want nil", summary)
	}
}

func TestLoadPortfolioSummaryReturnsMalformedPortfolioError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "portfolio.yaml")
	if err := os.WriteFile(path, []byte("transactions: ["), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := loadPortfolioSummary(path, &dataset.Dataset{})

	if err == nil {
		t.Fatal("loadPortfolioSummary() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "加载组合失败") {
		t.Fatalf("error = %v, want 加载组合失败", err)
	}
}

func TestRunUsesInvestableUniverseForMarketStatus(t *testing.T) {
	root := t.TempDir()
	rawDir := filepath.Join(root, "raw")
	var bars []data.DailyBar
	for i, code := range []string{"000001.SZ", "000002.SZ", "000003.SZ", "000004.SZ"} {
		bars = append(bars, rocBars(code, 11.0+float64(i)*0.1)...)
	}
	stale := rocBars("000005.SZ", 9.0)
	stale = stale[:len(stale)-1]
	bars = append(bars, stale...)
	if err := data.WriteParquetFile(filepath.Join(rawDir, "daily", "2026.parquet"), bars); err != nil {
		t.Fatal(err)
	}
	if err := data.WriteStocksParquet(filepath.Join(rawDir, "stocks.parquet"), []data.StockInfo{
		{TsCode: "000001.SZ", Name: "甲"},
		{TsCode: "000002.SZ", Name: "乙"},
		{TsCode: "000003.SZ", Name: "丙"},
		{TsCode: "000004.SZ", Name: "丁"},
		{TsCode: "000005.SZ", Name: "过期股票"},
	}); err != nil {
		t.Fatal(err)
	}

	provider := &testRealtimeProvider{}
	result, err := Run(context.Background(), Options{
		Config: &config.Config{
			Data:   config.DataConfig{RawDir: rawDir},
			Signal: config.SignalConfig{DefaultStrategies: []string{"roc"}, TopN: 1},
		},
		PortfolioPath:    filepath.Join(root, "portfolio.yaml"),
		ForwardDir:       filepath.Join(root, "forward"),
		Realtime:         true,
		RealtimeProvider: provider,
		NewsAnalyzer: func(context.Context, string, int) (*news.NewsSummary, error) {
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Dataset.SkippedStale != 1 {
		t.Fatalf("SkippedStale = %d, want 1", result.Dataset.SkippedStale)
	}
	if result.MarketStatus.FallingCount != 0 {
		t.Fatalf("FallingCount = %d, want 0 after stale stock exclusion", result.MarketStatus.FallingCount)
	}
	if len(provider.codes) != 3 || result.RealtimeLoaded != 3 {
		t.Fatalf("realtime codes/loaded = %d/%d, want 3/3", len(provider.codes), result.RealtimeLoaded)
	}
}

func rocBars(code string, finalClose float64) []data.DailyBar {
	bars := make([]data.DailyBar, 0, 13)
	for i := 0; i < 13; i++ {
		closePrice := 10 + float64(i)*0.02
		if i == 12 {
			closePrice = finalClose
		}
		bars = append(bars, data.DailyBar{
			TsCode: code, TradeDate: "202601" + twoDigit(i+1),
			Open: closePrice - 0.01, High: closePrice + 0.02, Low: closePrice - 0.03, Close: closePrice,
			Vol: 1000, RawOpen: closePrice - 0.01, RawHigh: closePrice + 0.02, RawLow: closePrice - 0.03, RawClose: closePrice, AdjFactor: 1,
		})
	}
	return bars
}

func twoDigit(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

type testRealtimeProvider struct {
	codes []string
}

func (p *testRealtimeProvider) Fetch(codes []string) ([]realtime.Quote, error) {
	p.codes = append([]string(nil), codes...)
	quotes := make([]realtime.Quote, 0, len(codes))
	for _, code := range codes {
		quotes = append(quotes, realtime.Quote{
			Code: code, PrevClose: 10, Open: 10, Current: 10.1, ChangePct: 1,
			UpdateAt: "2026-01-13 10:00:00",
		})
	}
	return quotes, nil
}
