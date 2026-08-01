package data

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestApplyAdjFactorsMatchesByCodeAndDate(t *testing.T) {
	bars := []DailyBar{
		{TsCode: "000001.SZ", TradeDate: "20260101", Open: 10, High: 11, Low: 9, Close: 10},
		{TsCode: "000001.SZ", TradeDate: "20260102", Open: 10, High: 11, Low: 9, Close: 10},
	}
	factors := []AdjFactor{
		{TsCode: "000001.SZ", TradeDate: "20260101", AdjFactor: 2},
		{TsCode: "000001.SZ", TradeDate: "20260102", AdjFactor: 3},
	}

	got := ApplyAdjFactors(bars, factors)

	if got[0].Close != 20 || got[1].Close != 30 {
		t.Fatalf("adjusted closes = %.2f, %.2f; want 20, 30", got[0].Close, got[1].Close)
	}
	if got[0].RawClose != 10 || got[1].RawClose != 10 {
		t.Fatalf("raw closes = %.2f, %.2f; want preserved raw 10, 10", got[0].RawClose, got[1].RawClose)
	}
	if got[0].AdjFactor != 2 || got[1].AdjFactor != 3 {
		t.Fatalf("adj factors = %.2f, %.2f; want 2, 3", got[0].AdjFactor, got[1].AdjFactor)
	}
}

func TestFetchStockListIncludesAllListingStatuses(t *testing.T) {
	seen := make(map[string]int)
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var req TushareReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		status, _ := req.Params["list_status"].(string)
		seen[status]++
		code := map[string]string{"L": "000001.SZ", "D": "000002.SZ", "P": "000003.SZ"}[status]
		body, err := json.Marshal(TushareResp{
			Code: 0,
			Data: struct {
				Fields []string        `json:"fields"`
				Items  [][]interface{} `json:"items"`
			}{
				Fields: []string{"ts_code", "symbol", "name", "market", "industry", "list_date", "delist_date"},
				Items:  [][]interface{}{{code, code[:6], status, "主板", "测试", "20200101", ""}},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header)}, nil
	})

	client := NewClient("http://tushare.test", "token", 0)
	client.HTTPClient = &http.Client{Transport: transport}
	stocks, err := client.FetchStockList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(stocks) != 3 {
		t.Fatalf("len(stocks) = %d, want 3", len(stocks))
	}
	for _, status := range []string{"L", "D", "P"} {
		if seen[status] != 1 {
			t.Fatalf("status %s calls = %d, want 1", status, seen[status])
		}
	}
}
