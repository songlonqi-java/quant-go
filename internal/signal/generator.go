package signal

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"quant/internal/data"
	"quant/internal/market"
	"quant/internal/sector"
	"quant/internal/strategy"
)

type SignalResult struct {
	Horizon                 strategy.Horizon
	Code                    string
	Name                    string
	Date                    string
	Close                   float64
	Strategies              map[string]SignalDetail
	GroupScores             map[string]float64
	BuyCount                int
	SellCount               int
	RawScore                float64
	TotalScore              float64
	Confidence              float64
	PositionPct             float64
	MarketSentiment         string
	HasMoneyflow            bool
	MoneyflowNetAmount      float64
	LargeMoneyflowNetAmount float64
	SectorName              string
	SectorTags              []string
	SectorChg1              float64
	HasRealtime             bool
	RealtimePrice           float64
	RealtimeChangePct       float64
	RealtimeUpdateAt        string
	IntradayLabels          []string
	Suppressed              bool
	SuppressionReason       string
	RiskLabels              []string
	Reasons                 []string
	HistoricalEvidence      *HistoricalEvidence
}

type SignalDetail struct {
	Signal strategy.SignalType
	Score  float64
}

func Generate(barsMap map[string][]data.DailyBar, strategies []strategy.Strategy, topN int, names map[string]string) []SignalResult {
	return GenerateWithContext(barsMap, strategies, topN, names, nil)
}

func GenerateWithContext(barsMap map[string][]data.DailyBar, strategies []strategy.Strategy, topN int, names map[string]string, marketStatus *market.MarketStatus) []SignalResult {
	return GenerateWithContextAndMoneyflow(barsMap, strategies, topN, names, marketStatus, nil)
}

func GenerateWithContextAndMoneyflow(barsMap map[string][]data.DailyBar, strategies []strategy.Strategy, topN int, names map[string]string, marketStatus *market.MarketStatus, moneyflowStore *data.MoneyflowStore) []SignalResult {
	for _, s := range strategies {
		if u, ok := s.(strategy.UniverseUser); ok {
			u.SetUniverse(barsMap)
		}
	}

	grouped := groupStrategiesByHorizon(strategies)
	var results []SignalResult
	for _, horizon := range strategy.HorizonOrder() {
		horizonResults := generateForHorizon(barsMap, grouped[horizon], topN, names, marketStatus, moneyflowStore, horizon)
		results = append(results, horizonResults...)
	}
	return results
}

func generateForHorizon(barsMap map[string][]data.DailyBar, strategies []strategy.Strategy, topN int, names map[string]string, marketStatus *market.MarketStatus, moneyflowStore *data.MoneyflowStore, horizon strategy.Horizon) []SignalResult {
	if len(strategies) == 0 {
		return nil
	}
	var results []SignalResult
	for code, bars := range barsMap {
		if len(bars) < 2 {
			continue
		}
		lastIdx := len(bars) - 1

		name := code
		if n, ok := names[code]; ok && n != "" {
			name = n
		}

		r := SignalResult{
			Horizon:     horizon,
			Code:        code,
			Name:        name,
			Date:        bars[lastIdx].TradeDate,
			Close:       bars[lastIdx].TradeClose(),
			Strategies:  make(map[string]SignalDetail),
			GroupScores: make(map[string]float64),
		}
		if marketStatus != nil {
			r.MarketSentiment = marketStatus.Sentiment
		}

		for _, s := range strategies {
			if lastIdx < s.Warmup() {
				continue
			}
			sig := s.Signal(bars, lastIdx)
			var score float64
			if sc, ok := s.(strategy.ScoreStrategy); ok {
				score = sc.Score(bars, lastIdx)
			}
			r.Strategies[s.Name()] = SignalDetail{Signal: sig, Score: score}

			if sig == strategy.Buy {
				r.BuyCount++
				r.GroupScores[strategyGroup(s.Name())] += boundedContribution(score)
			} else if sig == strategy.Sell {
				r.SellCount++
				r.GroupScores[strategyGroup(s.Name())] -= boundedContribution(score)
			}
		}

		if r.BuyCount > 0 || r.SellCount > 0 {
			r.RawScore = cappedGroupScore(r.GroupScores)
			r.TotalScore = applyMarketAdjustment(r.RawScore, marketStatus)
			r.Confidence = confidenceScore(r)
			r.PositionPct = suggestedPositionPct(r, marketStatus)
			r.RiskLabels = riskLabels(r, bars, lastIdx, marketStatus)
			r.Reasons = reasons(r)
			applyMoneyflowContext(&r, moneyflowStore)
			results = append(results, r)
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].TotalScore == results[j].TotalScore {
			return results[i].Code < results[j].Code
		}
		return results[i].TotalScore > results[j].TotalScore
	})

	return LimitByRecommendation(results, topN)
}

func groupStrategiesByHorizon(strategies []strategy.Strategy) map[strategy.Horizon][]strategy.Strategy {
	grouped := make(map[strategy.Horizon][]strategy.Strategy)
	for _, s := range strategies {
		horizon := strategy.HorizonForStrategy(s.Name())
		grouped[horizon] = append(grouped[horizon], s)
	}
	return grouped
}

func LimitByRecommendation(results []SignalResult, topN int) []SignalResult {
	if topN <= 0 {
		return results
	}
	var limited []SignalResult
	for _, horizon := range strategy.HorizonOrder() {
		limited = append(limited, limitRecommendationGroup(FilterByHorizon(results, horizon), topN)...)
	}
	return limited
}

// SelectCandidatePool keeps enough ranked signals to replace candidates that
// fail intraday execution checks. A non-positive topN intentionally means the
// full universe, matching LimitByRecommendation.
func SelectCandidatePool(results []SignalResult, topN int) []SignalResult {
	if topN <= 0 {
		return LimitByRecommendation(results, topN)
	}
	return LimitByRecommendation(results, topN*3)
}

func limitRecommendationGroup(results []SignalResult, topN int) []SignalResult {
	buys := filterByRecommendation(results, "买入")
	sells := filterByRecommendation(results, "卖出")
	sort.Slice(buys, func(i, j int) bool {
		if buys[i].TotalScore == buys[j].TotalScore {
			return buys[i].Code < buys[j].Code
		}
		return buys[i].TotalScore > buys[j].TotalScore
	})
	sort.Slice(sells, func(i, j int) bool {
		if sells[i].TotalScore == sells[j].TotalScore {
			return sells[i].Code < sells[j].Code
		}
		return sells[i].TotalScore < sells[j].TotalScore
	})
	if len(buys) > topN {
		buys = buys[:topN]
	}
	if len(sells) > topN {
		sells = sells[:topN]
	}
	return append(buys, sells...)
}

func SelectWatchlist(results []SignalResult, formal []SignalResult, limit int) []SignalResult {
	if limit <= 0 {
		return nil
	}
	formalKeys := make(map[string]bool, len(formal))
	for _, r := range formal {
		formalKeys[signalKey(r)] = true
	}

	watch := make([]SignalResult, 0, limit)
	for _, r := range results {
		if formalKeys[signalKey(r)] || !isWatchCandidate(r) {
			continue
		}
		watch = append(watch, r)
	}
	sort.Slice(watch, func(i, j int) bool {
		left := watchScore(watch[i])
		right := watchScore(watch[j])
		if left == right {
			if watch[i].TotalScore == watch[j].TotalScore {
				return watch[i].Code < watch[j].Code
			}
			return watch[i].TotalScore > watch[j].TotalScore
		}
		return left > right
	})
	if len(watch) > limit {
		watch = watch[:limit]
	}
	return watch
}

func signalKey(r SignalResult) string {
	return string(r.Horizon) + "|" + r.Code
}

func isWatchCandidate(r SignalResult) bool {
	if r.BuyCount == 0 || r.TotalScore <= 0 {
		return false
	}
	if rawRecommendation(r) == "卖出" {
		return false
	}
	return true
}

func watchScore(r SignalResult) float64 {
	score := r.TotalScore + float64(r.BuyCount)*0.25 + r.Confidence/100
	if r.SellCount > 0 {
		score -= float64(r.SellCount) * 0.8
	}
	if r.Suppressed {
		score -= 0.2
	}
	return score
}

func FilterByHorizon(results []SignalResult, horizon strategy.Horizon) []SignalResult {
	filtered := make([]SignalResult, 0, len(results))
	for _, r := range results {
		if r.Horizon == horizon {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

func (r SignalResult) SignalSummary() string {
	parts := []string{}
	if r.BuyCount > 0 {
		parts = append(parts, fmt.Sprintf("买入×%d", r.BuyCount))
	}
	if r.SellCount > 0 {
		parts = append(parts, fmt.Sprintf("卖出×%d", r.SellCount))
	}
	if len(parts) == 0 {
		return "观望"
	}
	return strings.Join(parts, " ")
}

func (r SignalResult) Recommendation() string {
	if r.Suppressed && rawRecommendation(r) == "买入" {
		return "观望"
	}
	return rawRecommendation(r)
}

func rawRecommendation(r SignalResult) string {
	if r.BuyCount > r.SellCount && r.TotalScore > 0 {
		return "买入"
	} else if r.SellCount > r.BuyCount && r.TotalScore < 0 {
		return "卖出"
	}
	return "观望"
}

func strategyGroup(name string) string {
	return string(strategy.GroupForStrategy(name))
}

func groupCap(group string) float64 {
	switch group {
	case "trend":
		return 2.2
	case "volume":
		return 1.5
	case "reversal":
		return 1.5
	case "value":
		return 1.2
	case "pattern":
		return 1.2
	default:
		return 1.0
	}
}

func boundedContribution(score float64) float64 {
	if math.IsNaN(score) || math.IsInf(score, 0) {
		return 1
	}
	// Score implementations historically mixed signed bullishness and unsigned
	// setup quality. Once a strategy emits BUY or SELL, the signal supplies the
	// direction and score magnitude supplies confidence.
	score = math.Min(30, math.Abs(score))
	contribution := 1 + score*0.01
	if contribution < 0.5 {
		return 0.5
	}
	if contribution > 1.3 {
		return 1.3
	}
	return contribution
}

func cappedGroupScore(groupScores map[string]float64) float64 {
	var total float64
	for group, score := range groupScores {
		cap := groupCap(group)
		if score > cap {
			score = cap
		}
		if score < -cap {
			score = -cap
		}
		total += score
	}
	return total
}

func applyMarketAdjustment(score float64, marketStatus *market.MarketStatus) float64 {
	if marketStatus == nil || score <= 0 {
		return score
	}
	switch {
	case strings.Contains(marketStatus.Sentiment, "强烈看空"):
		return score * 0.4
	case strings.Contains(marketStatus.Sentiment, "偏空"):
		return score * 0.65
	case strings.Contains(marketStatus.Sentiment, "中性"):
		return score * 0.85
	case strings.Contains(marketStatus.Sentiment, "强烈看多"):
		return score * 1.15
	default:
		return score
	}
}

func confidenceScore(r SignalResult) float64 {
	dominant, opposing := r.BuyCount, r.SellCount
	if r.TotalScore < 0 {
		dominant, opposing = r.SellCount, r.BuyCount
	}
	conf := 45 + math.Abs(r.TotalScore)*10 + float64(dominant-opposing)*3 - float64(opposing)*15
	if conf < 0 {
		return 0
	}
	if conf > 95 {
		return 95
	}
	return conf
}

func suggestedPositionPct(r SignalResult, marketStatus *market.MarketStatus) float64 {
	if r.Recommendation() != "买入" {
		return 0
	}
	pos := 3 + r.Confidence/15
	cap := 10.0
	if marketStatus != nil {
		switch {
		case strings.Contains(marketStatus.Sentiment, "强烈看空"):
			cap = 2
		case strings.Contains(marketStatus.Sentiment, "偏空"):
			cap = 5
		case strings.Contains(marketStatus.Sentiment, "中性"):
			cap = 7
		case strings.Contains(marketStatus.Sentiment, "强烈看多"):
			cap = 15
		}
	}
	if pos > cap {
		return cap
	}
	return pos
}

func riskLabels(r SignalResult, bars []data.DailyBar, idx int, marketStatus *market.MarketStatus) []string {
	labels := make([]string, 0, 4)
	if r.SellCount > 0 {
		labels = append(labels, "有卖出冲突")
	}
	if r.BuyCount <= 2 {
		labels = append(labels, "信号较少")
	}
	if idx >= 5 && bars[idx-5].Close > 0 {
		chg5 := (bars[idx].Close/bars[idx-5].Close - 1) * 100
		if chg5 > 25 {
			labels = append(labels, "5日涨幅过高")
		}
	}
	if idx >= 20 && bars[idx-20].Close > 0 {
		chg20 := (bars[idx].Close/bars[idx-20].Close - 1) * 100
		if chg20 > 40 {
			labels = append(labels, "20日涨幅过高")
		}
	}
	if marketStatus != nil && (strings.Contains(marketStatus.Sentiment, "偏空") || strings.Contains(marketStatus.Sentiment, "强烈看空")) {
		labels = append(labels, "市场偏弱")
	}
	if marketStatus != nil {
		for _, flag := range marketStatus.RiskFlags {
			switch flag {
			case "亏钱效应", "跌停扩散", "涨停退潮":
				labels = append(labels, flag)
			}
		}
	}
	return labels
}

func applyMoneyflowContext(r *SignalResult, moneyflowStore *data.MoneyflowStore) {
	if moneyflowStore == nil {
		return
	}
	mf, ok := moneyflowStore.Get(r.Code, r.Date)
	if !ok {
		return
	}

	r.HasMoneyflow = true
	r.MoneyflowNetAmount = mf.NetMfAmount
	r.LargeMoneyflowNetAmount = mf.LargeNetAmount()

	net := r.MoneyflowNetAmount
	large := r.LargeMoneyflowNetAmount
	switch r.Recommendation() {
	case "买入":
		switch {
		case net > 0 && large > 0:
			r.RiskLabels = append(r.RiskLabels, "资金确认")
			r.Reasons = append(r.Reasons, fmt.Sprintf("资金净额%+.0f万，大单净额%+.0f万", net, large))
		case net < 0 && large < 0:
			r.RiskLabels = append(r.RiskLabels, "资金背离")
			r.Reasons = append(r.Reasons, fmt.Sprintf("资金净额%+.0f万，大单净额%+.0f万", net, large))
		case (net > 0 && large < 0) || (net < 0 && large > 0):
			r.RiskLabels = append(r.RiskLabels, "资金分歧")
			r.Reasons = append(r.Reasons, fmt.Sprintf("资金净额%+.0f万，大单净额%+.0f万", net, large))
		}
	case "卖出":
		if net < 0 || large < 0 {
			r.RiskLabels = append(r.RiskLabels, "资金流出")
			r.Reasons = append(r.Reasons, fmt.Sprintf("资金净额%+.0f万，大单净额%+.0f万", net, large))
		}
	}
}

func ApplySectorContext(results []SignalResult, report *sector.Report, memberships sector.MembershipStore) {
	if memberships.Len() == 0 {
		return
	}
	for i := range results {
		membership, ok := memberships.PrimaryIndustry(results[i].Code, results[i].Date)
		if !ok {
			continue
		}
		results[i].SectorName = membership.SectorName
		if report == nil {
			continue
		}
		row, ok := report.Find(membership.SectorType, membership.SectorCode)
		if !ok {
			continue
		}
		results[i].SectorName = row.SectorName
		results[i].SectorTags = sector.SplitTags(row.Tags)
		results[i].SectorChg1 = row.Chg1
		switch rawRecommendation(results[i]) {
		case "买入":
			if sector.HasTag(row, "板块放量") || sector.HasTag(row, "赚钱效应扩散") || sector.HasTag(row, "涨停扩散") || sector.HasTag(row, "强势延续") {
				addUnique(&results[i].RiskLabels, "板块共振")
				results[i].Reasons = append(results[i].Reasons, fmt.Sprintf("%s板块%+.1f%%，%s", row.SectorName, row.Chg1, row.Tags))
			}
			if sector.HasTag(row, "资金确认") {
				addUnique(&results[i].RiskLabels, "板块资金确认")
			}
			if sector.HasTag(row, "资金背离") {
				addUnique(&results[i].RiskLabels, "板块资金背离")
			}
			if sector.HasTag(row, "高位退潮") {
				addUnique(&results[i].RiskLabels, "板块退潮")
			}
		case "卖出":
			if sector.HasTag(row, "高位退潮") || sector.HasTag(row, "资金背离") {
				addUnique(&results[i].RiskLabels, "板块风险")
			}
		}
	}
}

func reasons(r SignalResult) []string {
	var buyNames []string
	var sellNames []string
	for name, detail := range r.Strategies {
		switch detail.Signal {
		case strategy.Buy:
			buyNames = append(buyNames, name)
		case strategy.Sell:
			sellNames = append(sellNames, name)
		}
	}
	sort.Strings(buyNames)
	sort.Strings(sellNames)

	var out []string
	if len(buyNames) > 0 {
		out = append(out, "买入: "+strings.Join(buyNames, ","))
	}
	if len(sellNames) > 0 {
		out = append(out, "卖出冲突: "+strings.Join(sellNames, ","))
	}
	return out
}
