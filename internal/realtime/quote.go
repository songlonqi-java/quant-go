package realtime

import (
	"sort"
	"strings"
	"time"
)

type Quote struct {
	Code      string
	Name      string
	Open      float64
	PrevClose float64
	Current   float64
	High      float64
	Low       float64
	Change    float64
	ChangePct float64
	UpdateAt  string
	Source    string
}

type Provider interface {
	Fetch(codes []string) ([]Quote, error)
}

// PacedProvider supports the slower, full-market refresh used during trading
// hours. Candidate-only checks only require Provider.Fetch.
type PacedProvider interface {
	Provider
	FetchPaced(codes []string, window time.Duration) ([]Quote, FetchStats, error)
}

func MapByCode(quotes []Quote) map[string]Quote {
	out := make(map[string]Quote, len(quotes))
	for _, q := range quotes {
		if q.Code != "" {
			out[q.Code] = q
		}
	}
	return out
}

func UniqueCodes(codes []string) []string {
	seen := make(map[string]bool, len(codes))
	out := make([]string, 0, len(codes))
	for _, code := range codes {
		code = strings.TrimSpace(code)
		if code == "" || seen[code] {
			continue
		}
		seen[code] = true
		out = append(out, code)
	}
	sort.Strings(out)
	return out
}

func (q Quote) TradeDate() string {
	date := q.UpdateAt
	if idx := strings.IndexByte(date, ' '); idx >= 0 {
		date = date[:idx]
	}
	return strings.ReplaceAll(date, "-", "")
}
