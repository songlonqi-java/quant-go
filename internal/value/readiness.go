package value

import (
	"fmt"
	"path/filepath"
	"sort"

	"quant/internal/data"
	"quant/internal/sector"
)

const MinimumCoverage = 0.95

type Readiness struct {
	TradeDate          string
	Stocks             int
	DailyBasicCount    int
	FinancialCount     int
	SectorCount        int
	DailyBasicCoverage float64
	FinancialCoverage  float64
	SectorCoverage     float64
	MinimumCoverage    float64
	SectorReady        bool
	Ready              bool
	Issues             []string
}

// CheckReadiness verifies the slow-data inputs without running the screen or
// weakening its policy. It is intentionally independent from daily signals.
func CheckReadiness(rawDir string) (*Readiness, error) {
	bars, err := data.ReadParquetDir(filepath.Join(rawDir, "daily"))
	if err != nil {
		return nil, fmt.Errorf("读取日线: %w", err)
	}
	result := &Readiness{}
	for _, bar := range bars {
		if bar.TradeDate > result.TradeDate {
			result.TradeDate = bar.TradeDate
		}
	}
	if result.TradeDate == "" {
		return nil, fmt.Errorf("没有最新交易日")
	}
	codes := make(map[string]bool)
	for _, bar := range bars {
		if bar.TradeDate == result.TradeDate {
			codes[bar.TsCode] = true
		}
	}
	result.Stocks = len(codes)
	result.MinimumCoverage = MinimumCoverage
	fetcher := data.NewFetcher(nil, rawDir, nil)
	basic, basicErr := fetcher.LoadDailyBasicStore()
	fina, finaErr := fetcher.LoadFinaStore()
	memberships, membershipErr := sector.LoadIndustryMemberships(rawDir)
	report, sectorErr := sector.LoadReport(rawDir, result.TradeDate)
	ordered := make([]string, 0, len(codes))
	for code := range codes {
		ordered = append(ordered, code)
	}
	sort.Strings(ordered)
	for _, code := range ordered {
		if basic != nil && basic.GetDailyBasic(code, result.TradeDate) != nil {
			result.DailyBasicCount++
		}
		if fina != nil {
			if _, ok := fina.GetFinaIndicatorAsOf(code, result.TradeDate); ok {
				result.FinancialCount++
			}
		}
		if membershipErr == nil && report != nil {
			if membership, ok := memberships.PrimaryIndustry(code, result.TradeDate); ok {
				if _, ok := report.Find(membership.SectorType, membership.SectorCode); ok {
					result.SectorCount++
				}
			}
		}
	}
	result.DailyBasicCoverage = coverage(result.DailyBasicCount, result.Stocks)
	result.FinancialCoverage = coverage(result.FinancialCount, result.Stocks)
	result.SectorCoverage = coverage(result.SectorCount, result.Stocks)
	result.SectorReady = sectorErr == nil && membershipErr == nil && result.SectorCoverage >= MinimumCoverage
	if basicErr != nil {
		result.Issues = append(result.Issues, "加载 daily_basic 失败: "+basicErr.Error())
	} else if result.DailyBasicCoverage < MinimumCoverage {
		result.Issues = append(result.Issues, fmt.Sprintf("最新交易日 daily_basic 覆盖不足：%d/%d（要求至少 %.0f%%）", result.DailyBasicCount, result.Stocks, MinimumCoverage*100))
	}
	if finaErr != nil {
		result.Issues = append(result.Issues, "加载财务指标失败: "+finaErr.Error())
	} else if result.FinancialCoverage < MinimumCoverage {
		result.Issues = append(result.Issues, fmt.Sprintf("财务指标覆盖不足：%d/%d（要求至少 %.0f%%）", result.FinancialCount, result.Stocks, MinimumCoverage*100))
	}
	if sectorErr != nil {
		result.Issues = append(result.Issues, "加载行业估值快照失败: "+sectorErr.Error())
	} else if membershipErr != nil {
		result.Issues = append(result.Issues, "加载行业归属失败: "+membershipErr.Error())
	} else if !result.SectorReady {
		result.Issues = append(result.Issues, fmt.Sprintf("行业估值覆盖不足：%d/%d（要求至少 %.0f%%）", result.SectorCount, result.Stocks, MinimumCoverage*100))
	}
	result.Ready = len(result.Issues) == 0
	return result, nil
}

func coverage(count, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(count) / float64(total)
}
