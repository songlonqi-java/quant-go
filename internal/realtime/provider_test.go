package realtime

import (
	"errors"
	"testing"
	"time"
)

func TestAutoProviderFallsBackWhenPrimaryCoverageIsInsufficient(t *testing.T) {
	primary := &recordingProvider{quotes: []Quote{{Code: "600000.SH", Current: 10, PrevClose: 9.9}}}
	fallback := &recordingProvider{quotes: []Quote{
		{Code: "600000.SH", Current: 10, PrevClose: 9.9},
		{Code: "000001.SZ", Current: 11, PrevClose: 10.9},
	}}
	provider := &AutoProvider{Primary: primary, Fallback: fallback}

	quotes, err := provider.Fetch([]string{"600000.SH", "000001.SZ"})
	if err != nil {
		t.Fatal(err)
	}
	if len(quotes) != 2 || primary.fetchCalls != 1 || fallback.fetchCalls != 1 {
		t.Fatalf("quotes/calls = %d/%d/%d", len(quotes), primary.fetchCalls, fallback.fetchCalls)
	}
}

func TestAutoProviderKeepsFullPrimarySnapshot(t *testing.T) {
	primary := &recordingProvider{quotes: []Quote{
		{Code: "600000.SH", Current: 10, PrevClose: 9.9},
		{Code: "000001.SZ", Current: 11, PrevClose: 10.9},
	}, stats: FetchStats{Source: SourceEastmoney, Batches: 3}}
	fallback := &recordingProvider{err: errors.New("should not run")}
	provider := &AutoProvider{Primary: primary, Fallback: fallback}

	quotes, stats, err := provider.FetchPaced([]string{"600000.SH", "000001.SZ"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(quotes) != 2 || stats.Source != SourceEastmoney || primary.pacedCalls != 1 || fallback.pacedCalls != 0 {
		t.Fatalf("quotes/stats/calls = %d/%+v/%d/%d", len(quotes), stats, primary.pacedCalls, fallback.pacedCalls)
	}
}

func TestAutoProviderReportsFallbackSource(t *testing.T) {
	primary := &recordingProvider{err: errors.New("primary unavailable"), stats: FetchStats{Source: SourceEastmoney}}
	fallback := &recordingProvider{quotes: []Quote{
		{Code: "600000.SH", Current: 10, PrevClose: 9.9},
	}, stats: FetchStats{Source: SourceSina, Batches: 1}}
	provider := &AutoProvider{Primary: primary, Fallback: fallback}

	_, stats, err := provider.FetchPaced([]string{"600000.SH"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Source != SourceSina || stats.FallbackFrom != SourceEastmoney {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestNewProviderRejectsUnknownSource(t *testing.T) {
	if _, err := NewProvider("unknown"); err == nil {
		t.Fatal("NewProvider(unknown) error = nil")
	}
	if provider, err := NewProvider(SourceAuto); err != nil {
		t.Fatal(err)
	} else if _, ok := provider.(*AutoProvider); !ok {
		t.Fatalf("provider = %T, want *AutoProvider", provider)
	}
}

type recordingProvider struct {
	quotes     []Quote
	err        error
	stats      FetchStats
	fetchCalls int
	pacedCalls int
}

func (p *recordingProvider) Fetch(codes []string) ([]Quote, error) {
	p.fetchCalls++
	return p.quotes, p.err
}

func (p *recordingProvider) FetchPaced(codes []string, window time.Duration) ([]Quote, FetchStats, error) {
	p.pacedCalls++
	return p.quotes, p.stats, p.err
}
