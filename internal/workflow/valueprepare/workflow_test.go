package valueprepare

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"quant/internal/config"
	"quant/internal/data"

	"github.com/parquet-go/parquet-go"
)

func TestRunRefreshesValuationBuildsSectorAndChecksCoverage(t *testing.T) {
	const tradeDate = "20260731"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request data.TushareReq
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if request.APIName != "daily_basic" || request.Token != "token" {
			t.Errorf("request=%+v", request)
		}
		response := data.TushareResp{Code: 0}
		response.Data.Fields = []string{"ts_code", "trade_date", "pe", "pe_ttm", "pb", "total_mv"}
		response.Data.Items = [][]any{{"000001.SZ", tradeDate, 10.0, 10.0, 1.0, 100000.0}, {"000002.SZ", tradeDate, 20.0, 20.0, 2.0, 100000.0}}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	rawDir := t.TempDir()
	if err := data.WriteParquetFile(filepath.Join(rawDir, "daily", "2026.parquet"), []data.DailyBar{
		{TsCode: "000001.SZ", TradeDate: tradeDate, Close: 10, RawClose: 10},
		{TsCode: "000002.SZ", TradeDate: tradeDate, Close: 20, RawClose: 20},
	}); err != nil {
		t.Fatal(err)
	}
	if err := data.WriteStocksParquet(filepath.Join(rawDir, "stocks.parquet"), []data.StockInfo{
		{TsCode: "000001.SZ", Industry: "科技"}, {TsCode: "000002.SZ", Industry: "科技"},
	}); err != nil {
		t.Fatal(err)
	}
	writeTestParquet(t, filepath.Join(rawDir, "fina", "fina_indicator.parquet"), []data.FinaIndicator{
		{TsCode: "000001.SZ", AnnDate: tradeDate, Roe: 10}, {TsCode: "000002.SZ", AnnDate: tradeDate, Roe: 10},
	})
	cfg := &config.Config{
		Tushare: config.TushareConfig{Token: "token", BaseURL: server.URL},
		Data:    config.DataConfig{RawDir: rawDir},
		Fetch:   config.FetchConfig{StockPrefixes: []string{"00"}},
	}
	result, err := Run(context.Background(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Readiness == nil || !result.Readiness.Ready || result.Readiness.DailyBasicCoverage != 1 || result.Readiness.SectorCoverage != 1 {
		t.Fatalf("readiness=%+v", result.Readiness)
	}
}

func writeTestParquet[T any](t *testing.T, path string, rows []T) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := parquet.NewWriter(file)
	for _, row := range rows {
		if err := writer.Write(row); err != nil {
			file.Close()
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
