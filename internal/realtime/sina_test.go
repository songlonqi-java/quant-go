package realtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestToSinaSymbol(t *testing.T) {
	tests := map[string]string{
		"600000.SH": "sh600000",
		"000001.SZ": "sz000001",
		"430047.BJ": "bj430047",
	}
	for input, want := range tests {
		got, ok := ToSinaSymbol(input)
		if !ok || got != want {
			t.Fatalf("ToSinaSymbol(%q) = %q, %v; want %q, true", input, got, ok, want)
		}
	}
}

func TestParseSinaResponse(t *testing.T) {
	payload := record(32, ",",
		0, "浦发银行",
		1, "10.30",
		2, "10.11",
		3, "10.45",
		4, "10.57",
		5, "9.91",
		30, "2026-07-23",
		31, "15:00:00",
	)
	response := fmt.Sprintf("var hq_str_sh600000=\"%s\";\n", payload)

	quotes := ParseSinaResponse(response)

	if len(quotes) != 1 {
		t.Fatalf("len(quotes) = %d, want 1", len(quotes))
	}
	q := quotes[0]
	if q.Code != "600000.SH" || q.Name != "浦发银行" {
		t.Fatalf("quote identity = %s/%s, want 600000.SH/浦发银行", q.Code, q.Name)
	}
	if q.Open != 10.30 || q.PrevClose != 10.11 || q.Current != 10.45 || q.High != 10.57 || q.Low != 9.91 {
		t.Fatalf("quote prices = %+v, want parsed OHLC", q)
	}
	if q.Change != 0.34 || q.ChangePct != 3.36 {
		t.Fatalf("change = %.2f/%.2f%%, want 0.34/3.36%%", q.Change, q.ChangePct)
	}
	if q.UpdateAt != "2026-07-23 15:00:00" {
		t.Fatalf("UpdateAt = %q, want 2026-07-23 15:00:00", q.UpdateAt)
	}
}

func TestSinaProviderFetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != "https://finance.sina.com.cn" {
			t.Fatalf("Referer = %q, want finance.sina.com.cn", r.Header.Get("Referer"))
		}
		if !strings.Contains(r.URL.String(), "sh600000") || !strings.Contains(r.URL.String(), "sz000001") {
			t.Fatalf("request URL = %q, want both symbols", r.URL.String())
		}
		fmt.Fprintf(w, "var hq_str_sh600000=\"%s\";\n", record(32, ",", 0, "浦发银行", 1, "10.30", 2, "10.11", 3, "10.45", 4, "10.57", 5, "9.91", 30, "2026-07-23", 31, "15:00:00"))
		fmt.Fprintf(w, "var hq_str_sz000001=\"%s\";\n", record(32, ",", 0, "平安银行", 1, "10.00", 2, "10.10", 3, "10.20", 4, "10.30", 5, "9.90", 30, "2026-07-23", 31, "15:00:00"))
	}))
	defer server.Close()

	provider := &SinaProvider{
		BaseURL:    server.URL + "/list=",
		HTTPClient: server.Client(),
		BatchSize:  80,
	}

	quotes, err := provider.Fetch([]string{"600000.SH", "000001.SZ", "600000.SH"})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if len(quotes) != 2 {
		t.Fatalf("len(quotes) = %d, want 2", len(quotes))
	}
	byCode := MapByCode(quotes)
	if byCode["600000.SH"].Current != 10.45 {
		t.Fatalf("600000 current = %.2f, want 10.45", byCode["600000.SH"].Current)
	}
	if byCode["000001.SZ"].Current != 10.20 {
		t.Fatalf("000001 current = %.2f, want 10.20", byCode["000001.SZ"].Current)
	}
}

func record(size int, sep string, fields ...any) string {
	values := make([]string, size)
	for i := range values {
		values[i] = "0"
	}
	for i := 0; i+1 < len(fields); i += 2 {
		idx := fields[i].(int)
		values[idx] = fields[i+1].(string)
	}
	return strings.Join(values, sep)
}
