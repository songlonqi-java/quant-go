package realtime

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEastmoneyProviderFetchUsesCandidateBatchEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/quote" {
			t.Fatalf("path = %s, want /quote", r.URL.Path)
		}
		if got := r.URL.Query().Get("secids"); got != "0.000001,0.430047,1.600000" {
			t.Fatalf("secids = %q", got)
		}
		if r.Header.Get("Referer") != "https://quote.eastmoney.com/" {
			t.Fatalf("Referer = %q", r.Header.Get("Referer"))
		}
		_, _ = w.Write([]byte(`{"rc":0,"data":{"total":3,"diff":[
{"f2":10.45,"f3":3.36,"f4":0.34,"f12":"600000","f13":1,"f14":"浦发银行","f15":10.57,"f16":9.91,"f17":10.30,"f18":10.11,"f124":1785479355},
{"f2":10.20,"f3":1.00,"f4":0.10,"f12":"000001","f13":0,"f14":"平安银行","f15":10.30,"f16":9.90,"f17":10.00,"f18":10.10,"f124":1785479355},
{"f2":"-","f3":"-","f4":"-","f12":"430047","f13":0,"f14":"诺思兰德","f15":"-","f16":"-","f17":"-","f18":8.17,"f124":1785479355}]}}`))
	}))
	defer server.Close()

	provider := &EastmoneyProvider{QuoteURL: server.URL + "/quote", HTTPClient: server.Client(), BatchSize: 100}
	quotes, err := provider.Fetch([]string{"600000.SH", "000001.SZ", "430047.BJ"})
	if err != nil {
		t.Fatal(err)
	}
	if len(quotes) != 3 {
		t.Fatalf("len(quotes) = %d, want 3", len(quotes))
	}
	byCode := MapByCode(quotes)
	q := byCode["600000.SH"]
	if q.Name != "浦发银行" || q.Open != 10.30 || q.PrevClose != 10.11 || q.Current != 10.45 || q.Source != eastmoneySource {
		t.Fatalf("600000 quote = %+v", q)
	}
	if q.UpdateAt != "2026-07-31 14:29:15" {
		t.Fatalf("UpdateAt = %q", q.UpdateAt)
	}
	if byCode["430047.BJ"].Current != 0 {
		t.Fatalf("suspended quote should be zero: %+v", byCode["430047.BJ"])
	}
}

func TestEastmoneyProviderFetchPacedPaginatesAndFiltersUniverse(t *testing.T) {
	var pages []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/list" {
			t.Fatalf("path = %s, want /list", r.URL.Path)
		}
		if got := r.URL.Query().Get("fs"); got != eastmoneyListFS {
			t.Fatalf("fs = %q", got)
		}
		pages = append(pages, r.URL.Query().Get("pn"))
		switch r.URL.Query().Get("pn") {
		case "1":
			_, _ = w.Write([]byte(`{"rc":0,"data":{"total":3,"diff":[
{"f2":10.45,"f3":3.36,"f4":0.34,"f12":"600000","f13":1,"f14":"浦发银行","f15":10.57,"f16":9.91,"f17":10.30,"f18":10.11,"f124":1785479355},
{"f2":10.20,"f3":1,"f4":0.1,"f12":"000001","f13":0,"f14":"平安银行","f15":10.30,"f16":9.90,"f17":10,"f18":10.10,"f124":1785479355}]}}`))
		case "2":
			_, _ = w.Write([]byte(`{"rc":0,"data":{"total":3,"diff":[
{"f2":8.50,"f3":2,"f4":0.17,"f12":"430047","f13":0,"f14":"诺思兰德","f15":8.60,"f16":8.20,"f17":8.30,"f18":8.33,"f124":1785479355}]}}`))
		default:
			t.Fatalf("unexpected page %q", r.URL.Query().Get("pn"))
		}
	}))
	defer server.Close()

	provider := &EastmoneyProvider{ListURL: server.URL + "/list", HTTPClient: server.Client(), PageSize: 2}
	quotes, stats, err := provider.FetchPaced([]string{"600000.SH", "430047.BJ"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(pages, ","); got != "1,2" {
		t.Fatalf("pages = %q, want 1,2", got)
	}
	if len(quotes) != 2 || MapByCode(quotes)["430047.BJ"].Current != 8.5 {
		t.Fatalf("quotes = %+v", quotes)
	}
	if stats.Source != eastmoneySource || stats.Requested != 2 || stats.Batches != 2 || stats.Interval != 0 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestEastmoneyProviderFetchPacedRetriesInitialPage(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"rc":0,"data":{"total":1,"diff":[{"f2":10.45,"f3":3.36,"f4":0.34,"f12":"600000","f13":1,"f14":"浦发银行","f15":10.57,"f16":9.91,"f17":10.30,"f18":10.11,"f124":1785479355}]}}`))
	}))
	defer server.Close()

	provider := &EastmoneyProvider{ListURL: server.URL, HTTPClient: server.Client(), PageSize: 1}
	quotes, stats, err := provider.FetchPaced([]string{"600000.SH"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 2 || len(quotes) != 1 || stats.Batches != 1 {
		t.Fatalf("attempts/quotes/stats = %d/%d/%+v", attempts, len(quotes), stats)
	}
}

func TestEastmoneyCodeConversions(t *testing.T) {
	if got, ok := ToEastmoneySecID("600000.SH"); !ok || got != "1.600000" {
		t.Fatalf("ToEastmoneySecID SH = %q, %v", got, ok)
	}
	if got, ok := ToEastmoneySecID("430047.BJ"); !ok || got != "0.430047" {
		t.Fatalf("ToEastmoneySecID BJ = %q, %v", got, ok)
	}
	if got, ok := FromEastmoneyCode("430047", 0); !ok || got != "430047.BJ" {
		t.Fatalf("FromEastmoneyCode BJ = %q, %v", got, ok)
	}
}

func TestEastmoneyTimestampUsesChinaTime(t *testing.T) {
	if got := eastmoneyTimestamp(1785479355); got != "2026-07-31 14:29:15" {
		t.Fatalf("timestamp = %q", got)
	}
	if got := eastmoneyTimestamp(0); got != "" {
		t.Fatalf("zero timestamp = %q", got)
	}
}

func TestEastmoneyPacingIntervalUsesWholeWindow(t *testing.T) {
	if got := pacingInterval(56, time.Minute); got != time.Minute/56 {
		t.Fatalf("interval = %s, want %s", got, time.Minute/56)
	}
}
