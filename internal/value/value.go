// Package value implements the slow, fundamental-driven investment workflow.
// Its interface deliberately exposes only monthly screening and quarterly
// review; daily trading code neither calls nor depends on this package.
package value

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"quant/internal/data"
	"quant/internal/sector"
)

const (
	SnapshotDir = "value"
	ReviewDir   = "value/review"
)

// Policy is intentionally fixed in the initial workflow so that successive
// snapshots are comparable. A policy revision should be made explicitly in
// code and recorded through PolicyVersion, rather than changed ad hoc.
type Policy struct {
	Version             string  `json:"version"`
	MinROE              float64 `json:"min_roe_pct"`
	MinProfitGrowth     float64 `json:"min_profit_growth_pct"`
	MinRevenueGrowth    float64 `json:"min_revenue_growth_pct"`
	MaxPB               float64 `json:"max_pb"`
	MinIndustryDiscount float64 `json:"min_industry_discount_pct"`
	ReversionDiscount   float64 `json:"reversion_discount_pct"`
	DeteriorationGrowth float64 `json:"deterioration_growth_pct"`
	DeteriorationROE    float64 `json:"deterioration_roe_pct"`
}

func DefaultPolicy() Policy {
	return Policy{
		Version:             "value-v1",
		MinROE:              8,
		MinProfitGrowth:     0,
		MinRevenueGrowth:    0,
		MaxPB:               3,
		MinIndustryDiscount: 20,
		ReversionDiscount:   5,
		DeteriorationGrowth: -10,
		DeteriorationROE:    6,
	}
}

type MonthlyOptions struct {
	RawDir       string
	Date         string
	TopN         int
	MinMarketCap float64 // 亿元，0 表示不限制
}

type QuarterlyOptions struct {
	RawDir string
	Date   string
	TopN   int
}

type Candidate struct {
	Code           string   `json:"code"`
	Name           string   `json:"name"`
	Industry       string   `json:"industry"`
	ValuationBasis string   `json:"valuation_basis"`
	Close          float64  `json:"close"`
	MarketCap      float64  `json:"market_cap_yi"`
	PE             float64  `json:"pe"`
	PETTM          float64  `json:"pe_ttm"`
	IndustryPETTM  float64  `json:"industry_pe_ttm"`
	PB             float64  `json:"pb"`
	IndustryPB     float64  `json:"industry_pb"`
	DiscountPct    float64  `json:"discount_pct"`
	ROE            float64  `json:"roe_pct"`
	ProfitGrowth   float64  `json:"profit_growth_pct"`
	RevenueGrowth  float64  `json:"revenue_growth_pct"`
	DividendYield  float64  `json:"dividend_yield_pct"`
	Score          float64  `json:"score"`
	Reasons        []string `json:"reasons"`
}

type MonthlyReport struct {
	Kind         string         `json:"kind"`
	ScreenDate   string         `json:"screen_date"`
	CreatedAt    time.Time      `json:"created_at"`
	Policy       Policy         `json:"policy"`
	Scanned      int            `json:"scanned"`
	Qualified    int            `json:"qualified"`
	Rejected     map[string]int `json:"rejected"`
	Candidates   []Candidate    `json:"candidates"`
	SnapshotPath string         `json:"snapshot_path"`
}

type ReviewDecision string

const (
	DecisionKeep   ReviewDecision = "继续跟踪"
	DecisionRevert ReviewDecision = "估值回归，评估分批止盈"
	DecisionExit   ReviewDecision = "基本面恶化，移出价值池"
	DecisionWait   ReviewDecision = "数据不足，等待财报"
)

type ReviewItem struct {
	Candidate
	Decision ReviewDecision `json:"decision"`
	Comment  string         `json:"comment"`
}

type QuarterlyReport struct {
	Kind           string       `json:"kind"`
	ReviewDate     string       `json:"review_date"`
	CreatedAt      time.Time    `json:"created_at"`
	SourceSnapshot string       `json:"source_snapshot"`
	Policy         Policy       `json:"policy"`
	Items          []ReviewItem `json:"items"`
	ReviewPath     string       `json:"review_path"`
}

type screenInput struct {
	date         string
	bars         map[string]data.DailyBar
	names        map[string]string
	memberships  sector.MembershipStore
	sectorReport *sector.Report
	fundamentals *data.FundamentalStore
	policy       Policy
	minMarketCap float64
}

// Monthly produces a persisted value watchlist. It requires an explicitly
// refreshed daily_basic snapshot and a sector snapshot for the same date.
func Monthly(opts MonthlyOptions) (*MonthlyReport, error) {
	input, err := loadScreenInput(opts.RawDir, opts.Date, opts.MinMarketCap)
	if err != nil {
		return nil, err
	}
	report := screen(input)
	report.Qualified = len(report.Candidates)
	if opts.TopN > 0 && len(report.Candidates) > opts.TopN {
		report.Candidates = report.Candidates[:opts.TopN]
	}
	report.SnapshotPath = snapshotPath(opts.RawDir, report.ScreenDate)
	if err := writeJSON(report.SnapshotPath, report); err != nil {
		return nil, err
	}
	return report, nil
}

// Quarterly reviews the newest persisted monthly value watchlist against the
// newest explicitly prepared valuation and financial data.
func Quarterly(opts QuarterlyOptions) (*QuarterlyReport, error) {
	monthly, sourcePath, err := loadLatestSnapshot(opts.RawDir)
	if err != nil {
		return nil, err
	}
	input, err := loadScreenInput(opts.RawDir, opts.Date, 0)
	if err != nil {
		return nil, err
	}
	input.policy = monthly.Policy
	report := &QuarterlyReport{
		Kind:           "quarterly_review",
		ReviewDate:     input.date,
		CreatedAt:      time.Now().UTC(),
		SourceSnapshot: sourcePath,
		Policy:         monthly.Policy,
	}
	for _, previous := range monthly.Candidates {
		report.Items = append(report.Items, review(input, previous))
	}
	sort.Slice(report.Items, func(i, j int) bool {
		return reviewPriority(report.Items[i].Decision) < reviewPriority(report.Items[j].Decision)
	})
	if opts.TopN > 0 && len(report.Items) > opts.TopN {
		report.Items = report.Items[:opts.TopN]
	}
	report.ReviewPath = reviewPath(opts.RawDir, report.ReviewDate)
	if err := writeJSON(report.ReviewPath, report); err != nil {
		return nil, err
	}
	return report, nil
}

func loadScreenInput(rawDir, requestedDate string, minMarketCap float64) (screenInput, error) {
	if rawDir == "" {
		rawDir = "./data/raw"
	}
	bars, err := data.ReadParquetDir(filepath.Join(rawDir, "daily"))
	if err != nil {
		return screenInput{}, fmt.Errorf("加载日线数据失败: %w", err)
	}
	date := requestedDate
	if date == "" {
		for _, bar := range bars {
			if bar.TradeDate > date {
				date = bar.TradeDate
			}
		}
	}
	if len(date) != 8 {
		return screenInput{}, fmt.Errorf("未找到有效交易日期")
	}
	byCode := make(map[string]data.DailyBar)
	for _, bar := range bars {
		if bar.TradeDate == date {
			byCode[bar.TsCode] = bar
		}
	}
	if len(byCode) == 0 {
		return screenInput{}, fmt.Errorf("日线数据不含 %s", date)
	}
	fetcher := data.NewFetcher(nil, rawDir, nil)
	fundamentals, err := fetcher.LoadDailyBasicStore()
	if err != nil {
		return screenInput{}, fmt.Errorf("加载估值数据失败: %w", err)
	}
	finaStore, err := fetcher.LoadFinaStore()
	if err != nil {
		return screenInput{}, fmt.Errorf("加载财务数据失败: %w", err)
	}
	fundamentals.MergeFrom(finaStore)
	memberships, err := sector.LoadIndustryMemberships(rawDir)
	if err != nil {
		return screenInput{}, fmt.Errorf("加载行业归属失败: %w", err)
	}
	sectorReport, err := sector.LoadReport(rawDir, date)
	if err != nil {
		return screenInput{}, fmt.Errorf("加载行业估值快照失败: %w", err)
	}
	if sectorReport == nil {
		return screenInput{}, fmt.Errorf("缺少 %s 行业估值快照；请先运行 go-quant sector build --date %s", date, date)
	}
	return screenInput{
		date:         date,
		bars:         byCode,
		names:        data.LoadStockNames(filepath.Join(rawDir, "stocks.parquet")),
		memberships:  memberships,
		sectorReport: sectorReport,
		fundamentals: fundamentals,
		policy:       DefaultPolicy(),
		minMarketCap: minMarketCap,
	}, nil
}

func screen(input screenInput) *MonthlyReport {
	report := &MonthlyReport{
		Kind:       "monthly_screen",
		ScreenDate: input.date,
		CreatedAt:  time.Now().UTC(),
		Policy:     input.policy,
		Rejected:   make(map[string]int),
	}
	codes := make([]string, 0, len(input.bars))
	for code := range input.bars {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for _, code := range codes {
		report.Scanned++
		candidate, reason, ok := evaluate(input, code)
		if !ok {
			report.Rejected[reason]++
			continue
		}
		report.Candidates = append(report.Candidates, candidate)
	}
	sort.Slice(report.Candidates, func(i, j int) bool {
		if report.Candidates[i].Score == report.Candidates[j].Score {
			return report.Candidates[i].Code < report.Candidates[j].Code
		}
		return report.Candidates[i].Score > report.Candidates[j].Score
	})
	return report
}

func evaluate(input screenInput, code string) (Candidate, string, bool) {
	bar, ok := input.bars[code]
	if !ok {
		return Candidate{}, "缺少日线", false
	}
	name := input.names[code]
	if isST(name) {
		return Candidate{}, "ST", false
	}
	basic := input.fundamentals.GetDailyBasic(code, input.date)
	if basic == nil {
		return Candidate{}, "缺少当日估值", false
	}
	if input.minMarketCap > 0 && basic.TotalMv < input.minMarketCap*10000 {
		return Candidate{}, "市值不足", false
	}
	membership, ok := input.memberships.PrimaryIndustry(code, input.date)
	if !ok {
		return Candidate{}, "缺少行业归属", false
	}
	sectorRow, ok := input.sectorReport.Find(membership.SectorType, membership.SectorCode)
	if !ok {
		return Candidate{}, "缺少行业估值", false
	}
	fi, hasFinancials := input.fundamentals.GetFinaIndicatorAsOf(code, input.date)
	if !hasFinancials {
		return Candidate{}, "缺少财务指标", false
	}
	if fi.Roe < input.policy.MinROE {
		return Candidate{}, "ROE不足", false
	}
	if fi.NIncomeYoY < input.policy.MinProfitGrowth || fi.RevenueYoY < input.policy.MinRevenueGrowth {
		return Candidate{}, "盈利或营收未增长", false
	}

	candidate := Candidate{
		Code:          code,
		Name:          name,
		Industry:      membership.SectorName,
		Close:         bar.TradeClose(),
		MarketCap:     basic.TotalMv / 10000,
		PE:            basic.Pe,
		PETTM:         basic.PeTTM,
		PB:            basic.Pb,
		ROE:           fi.Roe,
		ProfitGrowth:  fi.NIncomeYoY,
		RevenueGrowth: fi.RevenueYoY,
		DividendYield: basic.DvTTM,
	}

	if isFinancialIndustry(membership.SectorName) {
		if basic.Pb <= 0 || sectorRow.PBAggregate <= 0 {
			return Candidate{}, "金融行业PB不可用", false
		}
		candidate.ValuationBasis = "PB"
		candidate.IndustryPB = sectorRow.PBAggregate
		candidate.DiscountPct = discount(basic.Pb, sectorRow.PBAggregate)
		if candidate.DiscountPct < input.policy.MinIndustryDiscount {
			return Candidate{}, "行业PB折价不足", false
		}
	} else {
		if basic.PeTTM <= 0 || sectorRow.PETTMAggregate <= 0 {
			return Candidate{}, "PE_TTM不可用", false
		}
		if basic.Pb <= 0 || basic.Pb > input.policy.MaxPB {
			return Candidate{}, "PB不合理", false
		}
		candidate.ValuationBasis = "PE_TTM"
		candidate.IndustryPETTM = sectorRow.PETTMAggregate
		candidate.DiscountPct = discount(basic.PeTTM, sectorRow.PETTMAggregate)
		if candidate.DiscountPct < input.policy.MinIndustryDiscount {
			return Candidate{}, "行业PE_TTM折价不足", false
		}
	}
	candidate.Score = score(candidate)
	candidate.Reasons = []string{
		fmt.Sprintf("%s较行业聚合估值折价%.1f%%", candidate.ValuationBasis, candidate.DiscountPct),
		fmt.Sprintf("ROE %.1f%%，归母净利润同比 %.1f%%，营收同比 %.1f%%", candidate.ROE, candidate.ProfitGrowth, candidate.RevenueGrowth),
	}
	return candidate, "", true
}

func review(input screenInput, previous Candidate) ReviewItem {
	current, reason, ok := evaluateWithRelaxedDiscount(input, previous.Code)
	if !ok {
		return ReviewItem{Candidate: previous, Decision: DecisionWait, Comment: "无法完成复核：" + reason}
	}
	if current.ROE < input.policy.DeteriorationROE || current.ProfitGrowth < input.policy.DeteriorationGrowth || current.RevenueGrowth < input.policy.DeteriorationGrowth {
		return ReviewItem{Candidate: current, Decision: DecisionExit, Comment: "ROE、利润增速或营收增速显著恶化"}
	}
	if current.DiscountPct <= input.policy.ReversionDiscount {
		return ReviewItem{Candidate: current, Decision: DecisionRevert, Comment: "相对行业估值折价已收敛，需结合持仓成本分批止盈"}
	}
	return ReviewItem{Candidate: current, Decision: DecisionKeep, Comment: "基本面未恶化，估值折价仍在；继续等待价值回归"}
}

func evaluateWithRelaxedDiscount(input screenInput, code string) (Candidate, string, bool) {
	input.policy.MinIndustryDiscount = -100
	input.policy.MinROE = -100
	input.policy.MinProfitGrowth = -1000
	input.policy.MinRevenueGrowth = -1000
	input.policy.MaxPB = math.MaxFloat64
	candidate, reason, ok := evaluate(input, code)
	return candidate, reason, ok
}

func discount(value, benchmark float64) float64 {
	if value <= 0 || benchmark <= 0 {
		return 0
	}
	return (1 - value/benchmark) * 100
}

func score(candidate Candidate) float64 {
	return candidate.DiscountPct + math.Min(candidate.ROE, 30)*0.8 + math.Min(candidate.ProfitGrowth, 50)*0.2 + math.Min(candidate.RevenueGrowth, 50)*0.1 + math.Min(candidate.DividendYield, 8)*0.5
}

func isFinancialIndustry(industry string) bool {
	for _, keyword := range []string{"银行", "保险", "证券", "多元金融"} {
		if strings.Contains(industry, keyword) {
			return true
		}
	}
	return false
}

func isST(name string) bool {
	upper := strings.ToUpper(strings.TrimSpace(name))
	return strings.Contains(upper, "ST")
}

func reviewPriority(decision ReviewDecision) int {
	switch decision {
	case DecisionExit:
		return 0
	case DecisionRevert:
		return 1
	case DecisionWait:
		return 2
	default:
		return 3
	}
}

func snapshotPath(rawDir, date string) string {
	return filepath.Join(rawDir, SnapshotDir, date+".json")
}

func reviewPath(rawDir, date string) string {
	return filepath.Join(rawDir, ReviewDir, date+".json")
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	contents, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, append(contents, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func loadLatestSnapshot(rawDir string) (*MonthlyReport, string, error) {
	files, err := filepath.Glob(filepath.Join(rawDir, SnapshotDir, "????????.json"))
	if err != nil {
		return nil, "", err
	}
	if len(files) == 0 {
		return nil, "", fmt.Errorf("未找到价值月度快照；请先运行 go-quant value monthly")
	}
	sort.Strings(files)
	path := files[len(files)-1]
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	var report MonthlyReport
	if err := json.Unmarshal(contents, &report); err != nil {
		return nil, "", fmt.Errorf("读取价值月度快照失败: %w", err)
	}
	if report.Kind != "monthly_screen" {
		return nil, "", fmt.Errorf("价值快照格式无效: %s", path)
	}
	return &report, path, nil
}

func (r *MonthlyReport) Print() {
	fmt.Printf("\n========== 月度价值筛选 ==========\n")
	fmt.Printf("数据日期: %s | 扫描: %d | 价值候选: %d | 规则: %s\n", r.ScreenDate, r.Scanned, len(r.Candidates), r.Policy.Version)
	if len(r.Candidates) == 0 {
		fmt.Println("价值候选: 无（不因数量不足放宽估值或质量门槛）")
	} else {
		fmt.Println("排名  代码         名称       行业       估值     折价    ROE     利润增速  营收增速")
		for index, candidate := range r.Candidates {
			value := candidate.PETTM
			if candidate.ValuationBasis == "PB" {
				value = candidate.PB
			}
			fmt.Printf("%-4d %-11s %-10s %-10s %-12s %-7s %-7s %-9s %-9s\n",
				index+1, candidate.Code, candidate.Name, candidate.Industry,
				fmt.Sprintf("%s %.2f", candidate.ValuationBasis, value),
				fmt.Sprintf("%.1f%%", candidate.DiscountPct), fmt.Sprintf("%.1f%%", candidate.ROE),
				fmt.Sprintf("%.1f%%", candidate.ProfitGrowth), fmt.Sprintf("%.1f%%", candidate.RevenueGrowth))
		}
	}
	fmt.Printf("快照: %s\n", r.SnapshotPath)
	fmt.Println("==================================")
}

func (r *QuarterlyReport) Print() {
	fmt.Printf("\n========== 季度价值复核 ==========\n")
	fmt.Printf("复核日期: %s | 月度快照: %s\n", r.ReviewDate, r.SourceSnapshot)
	if len(r.Items) == 0 {
		fmt.Println("复核对象: 无")
	}
	for _, item := range r.Items {
		fmt.Printf("%-11s %-10s %-14s | %s\n", item.Code, item.Name, item.Decision, item.Comment)
	}
	fmt.Printf("复核结果: %s\n", r.ReviewPath)
	fmt.Println("==================================")
}
