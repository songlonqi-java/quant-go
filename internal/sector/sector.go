package sector

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"quant/internal/data"
)

const (
	TypeIndustry = "industry"
	SourceStock  = "stocks.parquet"
)

type Membership struct {
	TsCode     string
	SectorType string
	SectorCode string
	SectorName string
	Source     string
	InDate     string
	OutDate    string
}

type MembershipStore struct {
	byCode map[string][]Membership
}

func NewIndustryMemberships(stocks []data.StockInfo) MembershipStore {
	store := MembershipStore{byCode: make(map[string][]Membership)}
	for _, stock := range stocks {
		name := strings.TrimSpace(stock.Industry)
		if stock.TsCode == "" || name == "" {
			continue
		}
		m := Membership{
			TsCode:     stock.TsCode,
			SectorType: TypeIndustry,
			SectorCode: name,
			SectorName: name,
			Source:     SourceStock,
			InDate:     stock.ListDate,
			OutDate:    stock.DelistDate,
		}
		store.byCode[stock.TsCode] = append(store.byCode[stock.TsCode], m)
	}
	return store
}

func (s MembershipStore) Len() int {
	return len(s.byCode)
}

func (s MembershipStore) ForCodeAt(code, date string) []Membership {
	memberships := s.byCode[code]
	if len(memberships) == 0 {
		return nil
	}
	out := make([]Membership, 0, len(memberships))
	for _, m := range memberships {
		if activeOn(m, date) {
			out = append(out, m)
		}
	}
	return out
}

func (s MembershipStore) PrimaryIndustry(code, date string) (Membership, bool) {
	for _, m := range s.ForCodeAt(code, date) {
		if m.SectorType == TypeIndustry {
			return m, true
		}
	}
	return Membership{}, false
}

func activeOn(m Membership, date string) bool {
	if date == "" {
		return true
	}
	if m.InDate != "" && m.InDate > date {
		return false
	}
	if m.OutDate != "" && m.OutDate <= date {
		return false
	}
	return true
}

type AnalyzeOptions struct {
	Dates        []string
	UpdatedAt    string
	Fundamentals *data.FundamentalStore
}

func Analyze(codeMap map[string][]data.DailyBar, memberships MembershipStore, moneyflows *data.MoneyflowStore, opts AnalyzeOptions) []data.SectorDaily {
	if len(codeMap) == 0 || memberships.Len() == 0 {
		return nil
	}
	updatedAt := opts.UpdatedAt
	if updatedAt == "" {
		updatedAt = time.Now().Format(time.RFC3339)
	}
	dates := opts.Dates
	if len(dates) == 0 {
		dates = latestDateOnly(codeMap)
	}
	dateSet := make(map[string]bool, len(dates))
	for _, date := range dates {
		if date != "" {
			dateSet[date] = true
		}
	}
	if len(dateSet) == 0 {
		return nil
	}

	indexes := make(map[string]map[string]int, len(codeMap))
	codes := make([]string, 0, len(codeMap))
	for code, bars := range codeMap {
		sort.Slice(bars, func(i, j int) bool {
			if bars[i].TradeDate == bars[j].TradeDate {
				return bars[i].TsCode < bars[j].TsCode
			}
			return bars[i].TradeDate < bars[j].TradeDate
		})
		codeMap[code] = bars
		indexes[code] = indexByDate(bars)
		codes = append(codes, code)
	}
	sort.Strings(codes)

	byDateSector := make(map[string]map[string]*sectorAccumulator)
	for _, code := range codes {
		bars := codeMap[code]
		dateIndex := indexes[code]
		for date := range dateSet {
			idx, ok := dateIndex[date]
			if !ok {
				continue
			}
			for _, membership := range memberships.ForCodeAt(code, date) {
				key := membership.SectorType + "|" + membership.SectorCode
				if byDateSector[date] == nil {
					byDateSector[date] = make(map[string]*sectorAccumulator)
				}
				acc := byDateSector[date][key]
				if acc == nil {
					acc = &sectorAccumulator{
						tradeDate:  date,
						sectorType: membership.SectorType,
						sectorCode: membership.SectorCode,
						sectorName: membership.SectorName,
						source:     membership.Source,
					}
					byDateSector[date][key] = acc
				}
				acc.add(code, bars, idx, moneyflows, opts.Fundamentals)
			}
		}
	}

	sortedDates := make([]string, 0, len(byDateSector))
	for date := range byDateSector {
		sortedDates = append(sortedDates, date)
	}
	sort.Strings(sortedDates)

	var out []data.SectorDaily
	for _, date := range sortedDates {
		keys := make([]string, 0, len(byDateSector[date]))
		for key := range byDateSector[date] {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			row := byDateSector[date][key].daily(updatedAt)
			if row.MemberCount > 0 {
				out = append(out, row)
			}
		}
	}
	return out
}

type sectorAccumulator struct {
	tradeDate  string
	sectorType string
	sectorCode string
	sectorName string
	source     string

	memberCount    int
	risingCount    int
	fallingCount   int
	flatCount      int
	aboveMA20Count int
	limitUpCount   int
	limitDownCount int

	chg1Sum  float64
	chg1Cnt  int
	chg5Sum  float64
	chg5Cnt  int
	chg20Sum float64
	chg20Cnt int

	amount       float64
	avgAmount20  float64
	netMoneyflow float64
	largeNetFlow float64
	leaders      []leader

	peSum      float64
	peCount    int
	peTTMSum   float64
	peTTMCount int
	pbSum      float64
	pbCount    int

	peTTMMarketCap float64
	peTTMProfit    float64
	pbMarketCap    float64
	pbEquity       float64
}

func (a *sectorAccumulator) add(code string, bars []data.DailyBar, idx int, moneyflows *data.MoneyflowStore, fundamentals *data.FundamentalStore) {
	if idx < 0 || idx >= len(bars) {
		return
	}
	cur := bars[idx]
	closePrice := cur.TradeClose()
	if closePrice <= 0 {
		return
	}

	a.memberCount++
	a.amount += cur.Amount
	a.avgAmount20 += avgAmount(bars, idx-1, 20)

	if idx >= 1 {
		prevClose := bars[idx-1].TradeClose()
		if prevClose > 0 {
			chg := (closePrice/prevClose - 1) * 100
			a.chg1Sum += chg
			a.chg1Cnt++
			a.leaders = append(a.leaders, leader{code: code, chg: chg})
			switch {
			case closePrice > prevClose:
				a.risingCount++
			case closePrice < prevClose:
				a.fallingCount++
			default:
				a.flatCount++
			}
			if isLimitUp(cur, prevClose) {
				a.limitUpCount++
			}
			if isLimitDown(cur, prevClose) {
				a.limitDownCount++
			}
		}
	}
	if idx >= 5 {
		if prev := bars[idx-5].TradeClose(); prev > 0 {
			a.chg5Sum += (closePrice/prev - 1) * 100
			a.chg5Cnt++
		}
	}
	if idx >= 20 {
		if prev := bars[idx-20].TradeClose(); prev > 0 {
			a.chg20Sum += (closePrice/prev - 1) * 100
			a.chg20Cnt++
		}
		if ma := avgClose(bars, idx, 20); ma > 0 && closePrice > ma {
			a.aboveMA20Count++
		}
	}
	if moneyflows != nil {
		if mf, ok := moneyflows.Get(code, cur.TradeDate); ok {
			a.netMoneyflow += mf.NetMfAmount
			a.largeNetFlow += mf.LargeNetAmount()
		}
	}
	if fundamentals != nil {
		a.addValuation(fundamentals.GetDailyBasic(code, cur.TradeDate))
	}
}

func (a *sectorAccumulator) addValuation(basic *data.DailyBasic) {
	if basic == nil {
		return
	}
	if basic.Pe > 0 {
		a.peSum += basic.Pe
		a.peCount++
	}
	if basic.PeTTM > 0 {
		a.peTTMSum += basic.PeTTM
		a.peTTMCount++
		if basic.TotalMv > 0 {
			a.peTTMMarketCap += basic.TotalMv
			a.peTTMProfit += basic.TotalMv / basic.PeTTM
		}
	}
	if basic.Pb > 0 {
		a.pbSum += basic.Pb
		a.pbCount++
		if basic.TotalMv > 0 {
			a.pbMarketCap += basic.TotalMv
			a.pbEquity += basic.TotalMv / basic.Pb
		}
	}
}

func (a *sectorAccumulator) daily(updatedAt string) data.SectorDaily {
	row := data.SectorDaily{
		TradeDate:      a.tradeDate,
		SectorType:     a.sectorType,
		SectorCode:     a.sectorCode,
		SectorName:     a.sectorName,
		Source:         a.source,
		MemberCount:    a.memberCount,
		PECount:        a.peCount,
		PETTMCount:     a.peTTMCount,
		PBCount:        a.pbCount,
		RisingCount:    a.risingCount,
		FallingCount:   a.fallingCount,
		FlatCount:      a.flatCount,
		LimitUpCount:   a.limitUpCount,
		LimitDownCount: a.limitDownCount,
		Amount:         a.amount,
		NetMoneyflow:   a.netMoneyflow,
		LargeNetFlow:   a.largeNetFlow,
		UpdatedAt:      updatedAt,
	}
	row.Chg1 = avg(a.chg1Sum, a.chg1Cnt)
	row.Chg5 = avg(a.chg5Sum, a.chg5Cnt)
	row.Chg20 = avg(a.chg20Sum, a.chg20Cnt)
	row.PEAvg = avg(a.peSum, a.peCount)
	row.PETTMAvg = avg(a.peTTMSum, a.peTTMCount)
	row.PBAvg = avg(a.pbSum, a.pbCount)
	if a.peTTMProfit > 0 {
		row.PETTMAggregate = a.peTTMMarketCap / a.peTTMProfit
	}
	if a.pbEquity > 0 {
		row.PBAggregate = a.pbMarketCap / a.pbEquity
	}
	if row.RisingCount+row.FallingCount+row.FlatCount > 0 {
		row.Breadth = float64(row.RisingCount) / float64(row.RisingCount+row.FallingCount+row.FlatCount) * 100
	}
	if row.MemberCount > 0 {
		row.AboveMA20Pct = float64(a.aboveMA20Count) / float64(row.MemberCount) * 100
	}
	if a.avgAmount20 > 0 {
		row.AmountRatio20 = row.Amount / a.avgAmount20
	}
	row.LeaderCodes = formatLeaders(a.leaders, 5)
	row.Tags = strings.Join(tagsFor(row), ",")
	return row
}

func tagsFor(row data.SectorDaily) []string {
	var tags []string
	if row.AmountRatio20 >= 1.5 {
		tags = append(tags, "板块放量")
	}
	if row.Breadth >= 70 {
		tags = append(tags, "赚钱效应扩散")
	}
	if row.LimitUpCount >= 3 && float64(row.LimitUpCount)/math.Max(1, float64(row.MemberCount))*100 >= 2 {
		tags = append(tags, "涨停扩散")
	}
	if row.NetMoneyflow > 0 && row.LargeNetFlow > 0 {
		tags = append(tags, "资金确认")
	}
	if row.Chg1 > 0 && row.NetMoneyflow < 0 && row.LargeNetFlow < 0 {
		tags = append(tags, "资金背离")
	}
	if row.Chg5 > 5 && row.Chg20 > 10 && row.AboveMA20Pct >= 60 {
		tags = append(tags, "强势延续")
	}
	if row.Chg1 < -1 && (row.LimitDownCount >= 2 || (row.NetMoneyflow < 0 && row.LargeNetFlow < 0)) {
		tags = append(tags, "高位退潮")
	}
	if row.Chg1 > 1 && row.Breadth < 50 {
		tags = append(tags, "孤立龙头")
	}
	return tags
}

func avg(sum float64, count int) float64 {
	if count <= 0 {
		return 0
	}
	return sum / float64(count)
}

func avgAmount(bars []data.DailyBar, idx, period int) float64 {
	if idx < period-1 {
		return 0
	}
	var sum float64
	for i := idx - period + 1; i <= idx; i++ {
		sum += bars[i].Amount
	}
	return sum / float64(period)
}

func avgClose(bars []data.DailyBar, idx, period int) float64 {
	if idx < period-1 {
		return 0
	}
	var sum float64
	for i := idx - period + 1; i <= idx; i++ {
		sum += bars[i].TradeClose()
	}
	return sum / float64(period)
}

func isLimitUp(bar data.DailyBar, prevClose float64) bool {
	return bar.IsLimitUpCloseWithFallback(prevClose)
}

func isLimitDown(bar data.DailyBar, prevClose float64) bool {
	return bar.IsLimitDownCloseWithFallback(prevClose)
}

func indexByDate(bars []data.DailyBar) map[string]int {
	index := make(map[string]int, len(bars))
	for i, bar := range bars {
		index[bar.TradeDate] = i
	}
	return index
}

func latestDateOnly(codeMap map[string][]data.DailyBar) []string {
	var latest string
	for _, bars := range codeMap {
		if len(bars) == 0 {
			continue
		}
		if date := bars[len(bars)-1].TradeDate; date > latest {
			latest = date
		}
	}
	if latest == "" {
		return nil
	}
	return []string{latest}
}

type leader struct {
	code string
	chg  float64
}

func formatLeaders(leaders []leader, limit int) string {
	if len(leaders) == 0 || limit <= 0 {
		return ""
	}
	sort.Slice(leaders, func(i, j int) bool {
		if leaders[i].chg == leaders[j].chg {
			return leaders[i].code < leaders[j].code
		}
		return leaders[i].chg > leaders[j].chg
	})
	if len(leaders) > limit {
		leaders = leaders[:limit]
	}
	parts := make([]string, 0, len(leaders))
	for _, leader := range leaders {
		parts = append(parts, fmt.Sprintf("%s(%.1f%%)", leader.code, leader.chg))
	}
	return strings.Join(parts, ",")
}

func SplitTags(tags string) []string {
	if strings.TrimSpace(tags) == "" {
		return nil
	}
	parts := strings.Split(tags, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func HasTag(row data.SectorDaily, tag string) bool {
	for _, value := range SplitTags(row.Tags) {
		if value == tag {
			return true
		}
	}
	return false
}
