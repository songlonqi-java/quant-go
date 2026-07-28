package market

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"quant/internal/data"

	"github.com/parquet-go/parquet-go"
)

type MarketStatus struct {
	IndexCode      string
	IndexClose     float64
	IndexChg       float64
	MATrend        string
	Breadth        float64
	UpCount        int
	DownCount      int
	RisingCount    int
	FallingCount   int
	FlatCount      int
	LimitUpCount   int
	LimitDownCount int
	TurnoverAmount float64
	ProfitEffect   float64
	StrongSectors  []string
	WeakSectors    []string
	RiskFlags      []string
	Sentiment      string
	Advice         string
}

var stockIndustryMap map[string]string

func loadIndustries() map[string]string {
	if stockIndustryMap != nil {
		return stockIndustryMap
	}
	stockIndustryMap = make(map[string]string)

	f, err := os.Open("./data/raw/stocks.parquet")
	if err != nil {
		return stockIndustryMap
	}
	defer f.Close()

	reader := parquet.NewReader(f, parquet.SchemaOf(&data.StockInfo{}))
	defer reader.Close()

	for {
		var s data.StockInfo
		if err := reader.Read(&s); err != nil {
			break
		}
		ind := strings.TrimSpace(s.Industry)
		if ind == "" {
			ind = "其他"
		}
		stockIndustryMap[s.TsCode] = ind
	}
	return stockIndustryMap
}

func Analyze(bars []data.DailyBar) *MarketStatus {
	if len(bars) == 0 {
		return nil
	}
	codeMap := data.GroupByCode(bars)
	ms := &MarketStatus{}

	ms.analyzeIndex(codeMap)
	ms.analyzeBreadth(codeMap)
	ms.analyzeTradingStats(codeMap)
	ms.analyzeSectors(codeMap)
	ms.determineSentiment()

	return ms
}

func (ms *MarketStatus) analyzeTradingStats(codeMap map[string][]data.DailyBar) {
	total := 0
	var amount float64
	for code, bars := range codeMap {
		if len(bars) < 2 {
			continue
		}
		sort.Slice(bars, func(i, j int) bool { return bars[i].TradeDate < bars[j].TradeDate })
		last := len(bars) - 1
		cur := bars[last]
		prev := bars[last-1]
		closePrice := cur.TradeClose()
		prevClose := prev.TradeClose()
		if prevClose <= 0 || closePrice <= 0 {
			continue
		}
		total++
		switch {
		case closePrice > prevClose:
			ms.RisingCount++
		case closePrice < prevClose:
			ms.FallingCount++
		default:
			ms.FlatCount++
		}
		if isLimitUpClose(code, cur, prevClose) {
			ms.LimitUpCount++
		}
		if isLimitDownClose(code, cur, prevClose) {
			ms.LimitDownCount++
		}
		amount += cur.Amount
	}
	if total > 0 {
		ms.ProfitEffect = float64(ms.RisingCount) / float64(total) * 100
	}
	ms.TurnoverAmount = amount

	if total >= 20 {
		if ms.ProfitEffect <= 30 {
			ms.RiskFlags = append(ms.RiskFlags, "亏钱效应")
		}
		if ms.LimitDownCount >= 10 && ms.LimitDownCount > ms.LimitUpCount {
			ms.RiskFlags = append(ms.RiskFlags, "跌停扩散")
		}
		if ms.LimitUpCount <= 5 && ms.ProfitEffect < 45 {
			ms.RiskFlags = append(ms.RiskFlags, "涨停退潮")
		}
	}
}

func (ms *MarketStatus) analyzeIndex(codeMap map[string][]data.DailyBar) {
	if ms.tryLoadRealIndex("000001.SH") {
		return
	}
	for code, bars := range codeMap {
		if code == "000001.SH" {
			ms.computeFromBars(bars)
			return
		}
	}
	ms.computeCompositeIndex(codeMap)
}

func (ms *MarketStatus) tryLoadRealIndex(code string) bool {
	f, err := os.Open(fmt.Sprintf("./data/raw/index/%s.parquet", code))
	if err != nil {
		return false
	}
	defer f.Close()

	reader := parquet.NewReader(f, parquet.SchemaOf(&data.IndexBar{}))
	defer reader.Close()

	var bars []data.IndexBar
	for {
		var bar data.IndexBar
		if err := reader.Read(&bar); err != nil {
			break
		}
		bars = append(bars, bar)
	}
	if len(bars) < 60 {
		return false
	}
	sort.Slice(bars, func(i, j int) bool { return bars[i].TradeDate < bars[j].TradeDate })
	last := len(bars) - 1
	ms.IndexCode = "上证指数"
	ms.IndexClose = bars[last].Close
	if last >= 1 {
		ms.IndexChg = (bars[last].Close/bars[last-1].Close - 1) * 100
	}

	ma20 := avgIndexClose(bars, last, 20)
	ma60 := avgIndexClose(bars, last, 60)
	if ma20 > 0 && ma60 > 0 {
		cl := bars[last].Close
		if cl > ma20 && cl > ma60 && ma20 > ma60 {
			ms.MATrend = "多头排列 ↑"
		} else if cl > ma20 && cl > ma60 {
			ms.MATrend = "偏多"
		} else if cl < ma20 && cl < ma60 && ma20 < ma60 {
			ms.MATrend = "空头排列 ↓"
		} else if cl < ma20 && cl < ma60 {
			ms.MATrend = "偏空"
		} else {
			ms.MATrend = "震荡"
		}
	}
	return true
}

func (ms *MarketStatus) computeFromBars(bars []data.DailyBar) {
	sort.Slice(bars, func(i, j int) bool { return bars[i].TradeDate < bars[j].TradeDate })
	last := len(bars) - 1
	if last < 0 {
		return
	}
	ms.IndexClose = bars[last].Close
	if last >= 1 {
		ms.IndexChg = (bars[last].Close/bars[last-1].Close - 1) * 100
	}
	if last >= 59 {
		ma20 := sma(bars, last, 20)
		ma60 := sma(bars, last, 60)
		cl := bars[last].Close
		if cl > ma20 && cl > ma60 && ma20 > ma60 {
			ms.MATrend = "多头排列 ↑"
		} else if cl > ma20 && cl > ma60 {
			ms.MATrend = "偏多"
		} else if cl < ma20 && cl < ma60 && ma20 < ma60 {
			ms.MATrend = "空头排列 ↓"
		} else if cl < ma20 && cl < ma60 {
			ms.MATrend = "偏空"
		} else {
			ms.MATrend = "震荡"
		}
	}
}

func avgIndexClose(bars []data.IndexBar, idx, period int) float64 {
	if idx < period-1 {
		return 0
	}
	var sum float64
	for i := idx - period + 1; i <= idx; i++ {
		sum += bars[i].Close
	}
	return sum / float64(period)
}

func (ms *MarketStatus) computeCompositeIndex(codeMap map[string][]data.DailyBar) {
	var closes []float64
	var prevCloses []float64
	var score int
	total := 0

	for _, bars := range codeMap {
		if len(bars) < 2 {
			continue
		}
		sort.Slice(bars, func(i, j int) bool { return bars[i].TradeDate < bars[j].TradeDate })
		last := len(bars) - 1
		closes = append(closes, bars[last].Close)
		prevCloses = append(prevCloses, bars[last-1].Close)

		if len(bars) >= 60 {
			ma20 := sma(bars, last, 20)
			ma60 := sma(bars, last, 60)
			if bars[last].Close > ma20 && bars[last].Close > ma60 && ma20 > ma60 {
				score += 2
			} else if bars[last].Close > ma20 {
				score += 1
			} else if bars[last].Close < ma60 && ma20 < ma60 {
				score -= 2
			} else if bars[last].Close < ma20 {
				score -= 1
			}
		}
		total++
	}

	var avgClose, avgPrev float64
	for _, c := range closes {
		avgClose += c
	}
	avgClose /= float64(len(closes))
	for _, c := range prevCloses {
		avgPrev += c
	}
	avgPrev /= float64(len(prevCloses))

	ms.IndexCode = "全市场(合成)"
	ms.IndexClose = avgClose
	if avgPrev > 0 {
		ms.IndexChg = (avgClose/avgPrev - 1) * 100
	}

	if total > 0 {
		ratio := float64(score) / float64(total*2)
		switch {
		case ratio > 0.3:
			ms.MATrend = "多头排列 ↑"
		case ratio > 0.1:
			ms.MATrend = "偏多"
		case ratio < -0.3:
			ms.MATrend = "空头排列 ↓"
		case ratio < -0.1:
			ms.MATrend = "偏空"
		default:
			ms.MATrend = "震荡"
		}
	}
}

func (ms *MarketStatus) analyzeBreadth(codeMap map[string][]data.DailyBar) {
	total := 0
	aboveMA20 := 0
	for _, bars := range codeMap {
		if len(bars) < 20 {
			continue
		}
		sort.Slice(bars, func(i, j int) bool { return bars[i].TradeDate < bars[j].TradeDate })
		last := len(bars) - 1
		ma20 := sma(bars, last, 20)
		if ma20 > 0 {
			total++
			if bars[last].Close > ma20 {
				aboveMA20++
			}
		}
	}
	if total > 0 {
		ms.Breadth = float64(aboveMA20) / float64(total) * 100
		ms.UpCount = aboveMA20
		ms.DownCount = total - aboveMA20
	}
}

func (ms *MarketStatus) analyzeSectors(codeMap map[string][]data.DailyBar) {
	industries := loadIndustries()

	type perf struct {
		chg float64
		cnt int
	}
	sectorPerf := make(map[string]*perf)

	for code, bars := range codeMap {
		if len(bars) < 6 {
			continue
		}
		sort.Slice(bars, func(i, j int) bool { return bars[i].TradeDate < bars[j].TradeDate })
		last := len(bars) - 1
		chg5 := (bars[last].Close/bars[maxInt(0, last-5)].Close - 1) * 100

		ind := industries[code]
		if ind == "" {
			ind = "其他"
		}

		if _, ok := sectorPerf[ind]; !ok {
			sectorPerf[ind] = &perf{}
		}
		sectorPerf[ind].chg += chg5
		sectorPerf[ind].cnt++
	}

	type avg struct {
		name string
		chg  float64
		cnt  int
	}
	var avgs []avg
	for name, p := range sectorPerf {
		if p.cnt >= 3 {
			avgs = append(avgs, avg{name, p.chg / float64(p.cnt), p.cnt})
		}
	}
	sort.Slice(avgs, func(i, j int) bool { return avgs[i].chg > avgs[j].chg })

	for i := 0; i < minInt(5, len(avgs)); i++ {
		ms.StrongSectors = append(ms.StrongSectors,
			fmt.Sprintf("%s(%.1f%%)", avgs[i].name, avgs[i].chg))
	}
	sort.Slice(avgs, func(i, j int) bool { return avgs[i].chg < avgs[j].chg })
	for i := 0; i < minInt(5, len(avgs)); i++ {
		if avgs[i].chg < 0 {
			ms.WeakSectors = append(ms.WeakSectors,
				fmt.Sprintf("%s(%.1f%%)", avgs[i].name, avgs[i].chg))
		}
	}
}

func (ms *MarketStatus) determineSentiment() {
	if ms.IndexClose == 0 {
		ms.Sentiment = "无法判断"
		ms.Advice = "建议观望"
		return
	}

	score := 0
	switch {
	case strings.Contains(ms.MATrend, "多头"):
		score += 2
	case strings.Contains(ms.MATrend, "偏多"):
		score += 1
	case strings.Contains(ms.MATrend, "空头"):
		score -= 2
	case strings.Contains(ms.MATrend, "偏空"):
		score -= 1
	}

	if ms.Breadth >= 70 {
		score += 2
	} else if ms.Breadth >= 50 {
		score += 1
	} else if ms.Breadth <= 30 {
		score -= 2
	} else if ms.Breadth <= 40 {
		score -= 1
	}

	if ms.IndexChg > 1 {
		score++
	} else if ms.IndexChg < -1 {
		score--
	}

	if ms.RisingCount+ms.FallingCount+ms.FlatCount > 0 {
		if ms.ProfitEffect >= 65 {
			score++
		} else if ms.ProfitEffect <= 25 {
			score -= 2
		} else if ms.ProfitEffect <= 35 {
			score--
		}
	}
	if ms.LimitUpCount >= 30 && ms.LimitUpCount > ms.LimitDownCount*2 {
		score++
	}
	if ms.LimitDownCount >= 10 && ms.LimitDownCount > ms.LimitUpCount {
		score--
	}
	if ms.LimitDownCount >= 20 && ms.LimitDownCount > ms.LimitUpCount*2 {
		score--
	}
	if contains(ms.RiskFlags, "跌停扩散") {
		score--
	}

	switch {
	case score >= 4:
		ms.Sentiment = "强烈看多"
		ms.Advice = "可积极做多，仓位70%+"
	case score >= 2:
		ms.Sentiment = "偏多"
		ms.Advice = "可适度参与，仓位50%左右"
	case score >= 0:
		ms.Sentiment = "中性震荡"
		ms.Advice = "精选个股，仓位30%"
	case score >= -2:
		ms.Sentiment = "偏空"
		ms.Advice = "减仓/观望，仓位<20%"
	default:
		ms.Sentiment = "强烈看空"
		ms.Advice = "建议空仓"
	}
}

func (ms *MarketStatus) Print() {
	if ms == nil {
		return
	}
	fmt.Println("\n========== 市场概况 ==========")
	fmt.Printf("全市场指数: %.2f  (%.2f%%)\n", ms.IndexClose, ms.IndexChg)
	fmt.Printf("均线趋势: %s\n", ms.MATrend)
	fmt.Printf("市场宽度: %.0f%% (MA20上方 %d/%d)\n", ms.Breadth, ms.UpCount, ms.UpCount+ms.DownCount)
	if ms.RisingCount+ms.FallingCount+ms.FlatCount > 0 {
		fmt.Printf("赚钱效应: %.0f%% (涨%d/跌%d/平%d), 涨停%d/跌停%d\n",
			ms.ProfitEffect, ms.RisingCount, ms.FallingCount, ms.FlatCount, ms.LimitUpCount, ms.LimitDownCount)
	}
	if ms.TurnoverAmount > 0 {
		fmt.Printf("成交额: %.0f亿\n", ms.TurnoverAmount/100000)
	}
	if len(ms.StrongSectors) > 0 {
		fmt.Printf("强势板块: %s\n", strings.Join(ms.StrongSectors, " / "))
	}
	if len(ms.WeakSectors) > 0 {
		fmt.Printf("弱势板块: %s\n", strings.Join(ms.WeakSectors, " / "))
	}
	if len(ms.RiskFlags) > 0 {
		fmt.Printf("A股风险: %s\n", strings.Join(ms.RiskFlags, " / "))
	}
	fmt.Printf("市场情绪: %s\n", ms.Sentiment)
	fmt.Printf("仓位建议: %s\n", ms.Advice)
	fmt.Println("==============================")
}

func sma(values []data.DailyBar, idx, period int) float64 {
	if idx < period-1 {
		return 0
	}
	var sum float64
	for i := idx - period + 1; i <= idx; i++ {
		sum += values[i].Close
	}
	return sum / float64(period)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func isLimitUpClose(code string, cur data.DailyBar, prevClose float64) bool {
	if cur.IsLimitUpClose() {
		return true
	}
	return cur.TradeClose() >= prevClose*(1+defaultLimitPct(code))*0.999
}

func isLimitDownClose(code string, cur data.DailyBar, prevClose float64) bool {
	if cur.IsLimitDownClose() {
		return true
	}
	return cur.TradeClose() <= prevClose*(1-defaultLimitPct(code))*1.001
}

func defaultLimitPct(code string) float64 {
	code = strings.ToUpper(code)
	switch {
	case strings.HasSuffix(code, ".BJ"):
		return 0.30
	case strings.HasPrefix(code, "300") || strings.HasPrefix(code, "301") ||
		strings.HasPrefix(code, "688") || strings.HasPrefix(code, "689"):
		return 0.20
	default:
		return 0.10
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
