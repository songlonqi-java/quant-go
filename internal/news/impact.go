package news

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"quant/internal/data"
	"quant/internal/execution"
)

// ImpactHorizonDays are the fixed event-study windows measured from the next
// session open after the news date. 20-day windows are included but will
// remain empty until the archive has enough history.
var ImpactHorizonDays = []int{1, 3, 5, 20}

// ImpactEvent is one stock mentioned in news on one trading date, aggregated
// across every article seen that day.
type ImpactEvent struct {
	Date     string
	Code     string
	Mentions int
	// NextSessionOpen is the first tradable session after the news date.
	NextSessionOpen string
	// EntryOpen is the raw opening price of that session (0 when unbuyable).
	EntryOpen float64
	// Returns holds net return per horizon; Proxy holds the equal-weight
	// market proxy net return over the same window.
	Returns map[int]float64
	Proxy   map[int]float64
}

// ImpactReport aggregates mention events into a readable summary.
type ImpactReport struct {
	DateRange       string
	TotalArticles   int
	EventDays       int
	UnbuyableEvents int
	ByMentions      []ImpactBucket
	TopStocks       []ImpactStockLine
	ActiveHorizons  []int
}

// ImpactBucket aggregates one mentions-bucket across horizons.
type ImpactBucket struct {
	Label    string
	Events   int
	Horizons map[int]HorizonStats
}

// HorizonStats summarizes net and excess returns for one horizon.
type HorizonStats struct {
	Events          int
	NetMeanPct      float64
	ExcessMeanPct   float64
	WinRatePct      float64
	MedianExcessPct float64
}

// ImpactStockLine ranks stocks by mentions and shows their excess returns.
type ImpactStockLine struct {
	Code     string
	Name     string
	Mentions int
	Events   int
	Horizons map[int]float64
}

type impactBuilder struct {
	nameToCode map[string]string
	bars       map[string][]data.DailyBar
	dates      []string
	proxyIndex map[string]execution.MarketProxyPoint
	proxyCosts execution.CostModel
}

// BuildNewsImpact measures what happened to stocks after they were mentioned
// in archived news. Entry is fixed at the next tradable session's open after
// the news date (news seen any time on date D is actionable from the next
// session, never from D's own open), so the study never uses information that
// was not yet available. Returns are net of commission and slippage; excess
// returns subtract the equal-weight market proxy over the same window.
func BuildNewsImpact(records []NewsRecord, barsMap map[string][]data.DailyBar, names map[string]string, commission, slippage float64) (*ImpactReport, error) {
	b := &impactBuilder{
		bars:       barsMap,
		nameToCode: make(map[string]string),
		proxyCosts: execution.CostModel{Commission: commission, Slippage: slippage},
	}
	for code, name := range names {
		b.nameToCode[name] = code
	}
	dates := make(map[string]bool)
	for _, bars := range barsMap {
		for _, bar := range bars {
			if bar.TradeDate != "" {
				dates[bar.TradeDate] = true
			}
		}
	}
	b.dates = make([]string, 0, len(dates))
	for date := range dates {
		b.dates = append(b.dates, date)
	}
	sort.Strings(b.dates)
	b.proxyIndex = execution.MarketProxyIndex(barsMap)

	latest := make(map[string]NewsRecord)
	for _, record := range records {
		current, ok := latest[record.CanonicalID]
		if !ok || record.Revision > current.Revision {
			latest[record.CanonicalID] = record
		}
	}
	events := b.collectEvents(records)
	report := summarizeEvents(events, names)
	report.TotalArticles = len(latest)
	if len(b.dates) > 0 {
		report.DateRange = b.dates[0] + " ~ " + b.dates[len(b.dates)-1]
	}
	return report, nil
}

// collectEvents maps news records to (stock, date) mention events, keeping
// only the latest revision of each canonical article.
func (b *impactBuilder) collectEvents(records []NewsRecord) []ImpactEvent {
	latest := make(map[string]NewsRecord)
	for _, record := range records {
		current, ok := latest[record.CanonicalID]
		if !ok || record.Revision > current.Revision {
			latest[record.CanonicalID] = record
		}
	}

	type key struct {
		date string
		code string
	}
	mentions := make(map[key]int)
	for _, record := range latest {
		date := newsTradeDate(record)
		if date == "" {
			continue
		}
		text := record.Title + " " + record.Content
		for name, code := range b.nameToCode {
			if _, inBars := b.bars[code]; !inBars {
				continue
			}
			if strings.Contains(text, name) {
				mentions[key{date, code}]++
			}
		}
	}

	events := make([]ImpactEvent, 0, len(mentions))
	for k, count := range mentions {
		event := ImpactEvent{Date: k.date, Code: k.code, Mentions: count}
		event.Returns = make(map[int]float64)
		event.Proxy = make(map[int]float64)
		event.NextSessionOpen = b.nextSession(k.date)
		b.fill(&event)
		events = append(events, event)
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].Date != events[j].Date {
			return events[i].Date < events[j].Date
		}
		return events[i].Code < events[j].Code
	})
	return events
}

// newsTradeDate resolves the trading date a news item belongs to: the first
// session after the item was seen, so that entry never happens on the same
// session's open. Falls back to published date when received time is missing.
func newsTradeDate(record NewsRecord) string {
	seen := record.ReceivedAt
	if seen == "" {
		seen = record.PublishedAt
	}
	parsed, err := time.Parse(time.RFC3339Nano, seen)
	if err != nil {
		return ""
	}
	return parsed.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("20060102")
}

func (b *impactBuilder) nextSession(date string) string {
	for _, candidate := range b.dates {
		if candidate > date {
			return candidate
		}
	}
	return ""
}

// fill returns fills event returns using raw open→close prices net of costs.
// Entry is skipped when the next open is a limit-up or gaps up more than 3%,
// matching the evidence and forward-test tradability rules.
func (b *impactBuilder) fill(event *ImpactEvent) {
	bars, ok := b.bars[event.Code]
	if !ok {
		return
	}
	idx := indexOfDate(bars, event.NextSessionOpen)
	if idx < 0 || idx == 0 {
		return
	}
	if !execution.CanBuyAtOpen(bars[idx-1], bars[idx], 0) {
		return
	}
	entry := bars[idx].TradeOpen()
	prevClose := bars[idx-1].TradeClose()
	if entry <= 0 || prevClose <= 0 || (entry/prevClose-1)*100 > 3 {
		return
	}
	event.EntryOpen = entry
	for _, horizon := range ImpactHorizonDays {
		exitIdx := idx + horizon
		if exitIdx >= len(bars) {
			continue
		}
		exit := bars[exitIdx].TradeClose()
		if exit <= 0 {
			continue
		}
		ret, ok := execution.RoundTripReturn(entry, exit, b.proxyCosts)
		if !ok {
			continue
		}
		event.Returns[horizon] = ret.NetReturnPct
		if proxy, ok := execution.MarketProxyReturn(b.proxyIndex, event.NextSessionOpen, bars[exitIdx].TradeDate, b.proxyCosts); ok {
			event.Proxy[horizon] = proxy.NetReturnPct
		}
	}
}

func indexOfDate(bars []data.DailyBar, date string) int {
	idx := sort.Search(len(bars), func(i int) bool { return bars[i].TradeDate >= date })
	if idx < len(bars) && bars[idx].TradeDate == date {
		return idx
	}
	return -1
}

func summarizeEvents(events []ImpactEvent, names map[string]string) *ImpactReport {
	report := &ImpactReport{ActiveHorizons: ImpactHorizonDays}
	for _, event := range events {
		report.EventDays++
		if event.NextSessionOpen != "" && event.EntryOpen <= 0 {
			report.UnbuyableEvents++
		}
	}
	report.ByMentions = make([]ImpactBucket, 0, 3)
	// Mention buckets: 1, 2-3, 4+
	for _, bucket := range []struct {
		label string
		match func(int) bool
	}{{"提及1次", func(n int) bool { return n == 1 }}, {"提及2-3次", func(n int) bool { return n >= 2 && n <= 3 }}, {"提及4次+", func(n int) bool { return n >= 4 }}} {
		cell := ImpactBucket{Label: bucket.label, Horizons: make(map[int]HorizonStats)}
		for _, event := range events {
			if !bucket.match(event.Mentions) {
				continue
			}
			for _, h := range ImpactHorizonDays {
				ret, ok := event.Returns[h]
				if !ok {
					continue
				}
				stats := cell.Horizons[h]
				stats.Events++
				stats.NetMeanPct += ret
				if proxy, ok := event.Proxy[h]; ok {
					stats.ExcessMeanPct += ret - proxy
				}
				if ret > 0 {
					stats.WinRatePct++
				}
				cell.Horizons[h] = stats
			}
		}
		cell.Events = bucketEventCount(cell)
		finalizeBucket(&cell)
		report.ByMentions = append(report.ByMentions, cell)
	}
	report.TopStocks = topStocks(events, names)
	return report
}

func bucketEventCount(bucket ImpactBucket) int {
	if stats, ok := bucket.Horizons[1]; ok {
		return stats.Events
	}
	for _, stats := range bucket.Horizons {
		return stats.Events
	}
	return 0
}

func finalizeBucket(bucket *ImpactBucket) {
	for h, stats := range bucket.Horizons {
		if stats.Events == 0 {
			delete(bucket.Horizons, h)
			continue
		}
		stats.NetMeanPct /= float64(stats.Events)
		stats.ExcessMeanPct /= float64(stats.Events)
		stats.WinRatePct = stats.WinRatePct / float64(stats.Events) * 100
		bucket.Horizons[h] = stats
	}
}

func topStocks(events []ImpactEvent, names map[string]string) []ImpactStockLine {
	byCode := make(map[string][]ImpactEvent)
	for _, event := range events {
		byCode[event.Code] = append(byCode[event.Code], event)
	}
	lines := make([]ImpactStockLine, 0, len(byCode))
	for code, evts := range byCode {
		line := ImpactStockLine{Code: code, Name: names[code], Horizons: make(map[int]float64)}
		for _, event := range evts {
			line.Mentions += event.Mentions
		}
		line.Events = len(evts)
		for _, h := range ImpactHorizonDays {
			total, count := 0.0, 0
			for _, event := range evts {
				if ret, ok := event.Returns[h]; ok && event.Proxy[h] != 0 {
					total += ret - event.Proxy[h]
					count++
				}
			}
			if count > 0 {
				line.Horizons[h] = total / float64(count)
			}
		}
		lines = append(lines, line)
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i].Mentions > lines[j].Mentions })
	return lines
}

// PrintImpact renders the report as a compact table.
func PrintImpact(report *ImpactReport, printed int) {
	fmt.Printf("新闻提及 → 事件研究（等权市场代理为基准，扣费后超额收益）\n")
	fmt.Printf("归档新闻: %d 篇, 事件日: %d, 无法成交: %d\n", report.TotalArticles, report.EventDays, report.UnbuyableEvents)
	fmt.Printf("%-12s", "提及强度")
	for _, h := range report.ActiveHorizons {
		fmt.Printf(" %3d日(净/超额)", h)
	}
	fmt.Println()
	for _, bucket := range report.ByMentions {
		if bucket.Events == 0 {
			continue
		}
		fmt.Printf("%-12s", bucket.Label)
		winRate := "-"
		for _, h := range report.ActiveHorizons {
			stats, ok := bucket.Horizons[h]
			if !ok {
				fmt.Printf(" %16s", "-")
				continue
			}
			fmt.Printf(" %5.2f/%-5.2f", stats.NetMeanPct, stats.ExcessMeanPct)
			if winRate == "-" {
				winRate = fmt.Sprintf("%.0f%%", stats.WinRatePct)
			}
		}
		fmt.Printf("  (事件%d, 1日胜率%s)", bucket.Events, winRate)
		fmt.Println()
	}
	if len(report.TopStocks) > 0 {
		fmt.Println("\nTop 提及个股（超额收益均值, %）:")
		fmt.Printf("%-12s %-10s %-6s %-6s", "代码", "名称", "提及", "事件")
		for _, h := range report.ActiveHorizons {
			fmt.Printf(" %3d日", h)
		}
		fmt.Println()
		for i, line := range report.TopStocks {
			if i >= printed {
				break
			}
			fmt.Printf("%-12s %-10s %-6d %-6d", line.Code, truncate(line.Name, 10), line.Mentions, line.Events)
			for _, h := range report.ActiveHorizons {
				if v, ok := line.Horizons[h]; ok {
					fmt.Printf(" %5.2f%%", v)
				} else {
					fmt.Printf(" %5s", "-")
				}
			}
			fmt.Println()
		}
	}
	fmt.Println("\n口径: 新闻收到时间后下一交易日开盘入场（一字涨停/高开>3%视为不可成交）；收益扣手续费0.03%/边+滑点0.01%/边；超额=个股净收益-同窗口等权市场代理净收益。")
}

func truncate(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max]) + ".."
}
