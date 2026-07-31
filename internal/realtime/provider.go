package realtime

import (
	"fmt"
	"strings"
	"time"
)

const (
	SourceAuto      = "auto"
	SourceEastmoney = "eastmoney"
	SourceSina      = "sina"
)

const minimumPrimaryCoverage = 0.90

// AutoProvider uses Eastmoney first and only falls back to Sina when the
// primary source fails or does not return enough usable quotes. This keeps
// normal traffic on the paginated public endpoint while retaining continuity
// when either upstream service changes or becomes temporarily unavailable.
type AutoProvider struct {
	Primary  Provider
	Fallback Provider
}

func NewAutoProvider() *AutoProvider {
	return &AutoProvider{
		Primary:  NewEastmoneyProvider(),
		Fallback: NewSinaProvider(),
	}
}

func NewProvider(source string) (Provider, error) {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "", SourceAuto:
		return NewAutoProvider(), nil
	case SourceEastmoney:
		return NewEastmoneyProvider(), nil
	case SourceSina:
		return NewSinaProvider(), nil
	default:
		return nil, fmt.Errorf("未知实时行情来源 %q（可选 auto、eastmoney、sina）", source)
	}
}

func (p *AutoProvider) Fetch(codes []string) ([]Quote, error) {
	codes = UniqueCodes(codes)
	quotes, err := p.Primary.Fetch(codes)
	if err == nil && hasUsableCoverage(quotes, codes) {
		return quotes, nil
	}
	fallbackQuotes, fallbackErr := p.Fallback.Fetch(codes)
	if fallbackErr == nil {
		return fallbackQuotes, nil
	}
	return nil, fallbackError("实时行情", err, fallbackErr)
}

func (p *AutoProvider) FetchPaced(codes []string, window time.Duration) ([]Quote, FetchStats, error) {
	codes = UniqueCodes(codes)
	primaryQuotes, primaryStats, primaryErr := fetchPaced(p.Primary, codes, window)
	if primaryErr == nil && hasUsableCoverage(primaryQuotes, codes) {
		return primaryQuotes, primaryStats, nil
	}
	fallbackQuotes, fallbackStats, fallbackErr := fetchPaced(p.Fallback, codes, window)
	fallbackStats.FallbackFrom = primaryStats.Source
	if fallbackStats.FallbackFrom == "" {
		fallbackStats.FallbackFrom = SourceEastmoney
	}
	if fallbackErr == nil {
		return fallbackQuotes, fallbackStats, nil
	}
	return nil, fallbackStats, fallbackError("全市场实时行情", primaryErr, fallbackErr)
}

func fetchPaced(provider Provider, codes []string, window time.Duration) ([]Quote, FetchStats, error) {
	if paced, ok := provider.(PacedProvider); ok {
		return paced.FetchPaced(codes, window)
	}
	quotes, err := provider.Fetch(codes)
	return quotes, FetchStats{Requested: len(codes), Batches: 1}, err
}

func hasUsableCoverage(quotes []Quote, codes []string) bool {
	if len(codes) == 0 {
		return true
	}
	wanted := make(map[string]bool, len(codes))
	for _, code := range codes {
		wanted[code] = true
	}
	seen := make(map[string]bool, len(quotes))
	for _, quote := range quotes {
		if wanted[quote.Code] && quote.Current > 0 && quote.PrevClose > 0 {
			seen[quote.Code] = true
		}
	}
	return float64(len(seen))/float64(len(wanted)) >= minimumPrimaryCoverage
}

func fallbackError(scope string, primaryErr, fallbackErr error) error {
	if primaryErr == nil {
		return fmt.Errorf("%s主数据源覆盖不足，新浪降级失败: %w", scope, fallbackErr)
	}
	return fmt.Errorf("%s主数据源失败（%v），新浪降级失败: %w", scope, primaryErr, fallbackErr)
}
