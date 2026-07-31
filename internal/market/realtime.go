package market

import (
	"fmt"
	"sort"

	"quant/internal/data"
	"quant/internal/realtime"
)

// IntradayStatus is a non-persistent snapshot derived from realtime quotes.
// It deliberately stays separate from MarketStatus: it cannot replace a
// completed daily bar in strategy calculation, historical validation, or
// end-of-day position decisions.
type IntradayStatus struct {
	AsOf              string
	Requested         int
	Quoted            int
	CoveragePct       float64
	Complete          bool
	RisingCount       int
	FallingCount      int
	FlatCount         int
	LimitUpCount      int
	LimitDownCount    int
	LimitPriceCovered int
	ProfitEffect      float64
	AverageChangePct  float64
}

// AnalyzeIntraday calculates cross-sectional intraday breadth from quotes.
// Limit-up/down counts use exact daily limit prices only when they are locally
// available, rather than guessing different board and ST limit rules.
func AnalyzeIntraday(quotes []realtime.Quote, requested int, limits *data.StkLimitStore) *IntradayStatus {
	status := &IntradayStatus{Requested: requested}
	if requested <= 0 {
		return status
	}
	seen := make(map[string]bool, len(quotes))
	var sumChange float64
	for _, quote := range quotes {
		if quote.Code == "" || seen[quote.Code] || quote.Current <= 0 || quote.PrevClose <= 0 {
			continue
		}
		seen[quote.Code] = true
		status.Quoted++
		sumChange += quote.ChangePct
		if date := quote.TradeDate(); date > status.AsOf {
			status.AsOf = date
		}
		switch {
		case quote.Current > quote.PrevClose:
			status.RisingCount++
		case quote.Current < quote.PrevClose:
			status.FallingCount++
		default:
			status.FlatCount++
		}
	}
	if status.Quoted > 0 {
		status.CoveragePct = float64(status.Quoted) / float64(requested) * 100
		status.ProfitEffect = float64(status.RisingCount) / float64(status.Quoted) * 100
		status.AverageChangePct = sumChange / float64(status.Quoted)
	}
	status.Complete = status.CoveragePct >= 90

	if limits == nil || status.AsOf == "" {
		return status
	}
	for _, quote := range quotes {
		limit, ok := limits.Get(quote.Code, status.AsOf)
		if !ok || limit.UpLimit <= 0 || limit.DownLimit <= 0 || quote.Current <= 0 {
			continue
		}
		status.LimitPriceCovered++
		if quote.Current >= limit.UpLimit*0.999 {
			status.LimitUpCount++
		}
		if quote.Current <= limit.DownLimit*1.001 {
			status.LimitDownCount++
		}
	}
	return status
}

func (s *IntradayStatus) Print() {
	if s == nil {
		return
	}
	fmt.Println("\n========== 全市场盘中行情（非日线） ==========")
	if s.AsOf != "" {
		fmt.Printf("行情日期: %s\n", s.AsOf)
	}
	fmt.Printf("覆盖率: %d/%d (%.1f%%)\n", s.Quoted, s.Requested, s.CoveragePct)
	fmt.Printf("盘中赚钱效应: %.1f%% (涨%d/跌%d/平%d), 平均涨跌%+.2f%%\n",
		s.ProfitEffect, s.RisingCount, s.FallingCount, s.FlatCount, s.AverageChangePct)
	if s.LimitPriceCovered > 0 {
		fmt.Printf("精确涨跌停: 涨停%d/跌停%d（涨跌停价覆盖%d只）\n", s.LimitUpCount, s.LimitDownCount, s.LimitPriceCovered)
	} else {
		fmt.Println("精确涨跌停: 当日涨跌停价未覆盖，未统计")
	}
	if !s.Complete {
		fmt.Println("数据状态: 不完整（覆盖率低于90%，不可用于改变仓位结论）")
	} else {
		fmt.Println("数据状态: 覆盖完整，仅作盘中风险监控，不替代收盘日线")
	}
	fmt.Println("========================================")
}

// SortedQuoteCodes extracts a deterministic full-market request list from a
// locally filtered, investable universe.
func SortedQuoteCodes(codeMap map[string][]data.DailyBar) []string {
	codes := make([]string, 0, len(codeMap))
	for code := range codeMap {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}
