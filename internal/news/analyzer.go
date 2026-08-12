package news

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"

	"quant/internal/data"

	"github.com/parquet-go/parquet-go"
)

type HotTopic struct {
	Keyword string
	Count   int
	Stocks  []string
}

type NewsSummary struct {
	TotalNews       int
	DateRange       string
	HotTopics       []HotTopic
	RecentNews      int
	RecentHotTopics []HotTopic
	HotStocks       []HotStock
	Sentiment       string
}

type HotStock struct {
	TsCode   string
	Name     string
	Mentions int
}

var stopWords = map[string]bool{
	"公司": true, "股份": true, "有限": true, "集团": true, "中国": true,
	"市场": true, "投资": true, "发布": true, "同比": true, "增长": true,
	"一个": true, "可以": true, "没有": true, "他们": true, "我们": true,
	"已经": true, "现在": true, "这个": true, "进行": true, "表示": true,
	"什么": true, "亿元": true, "万元": true, "记者": true, "报道": true,
	"信息": true, "数据": true, "情况": true, "问题": true, "影响": true,
}

func Analyze(ctx context.Context, client *data.Client, rawDir string, topN int) (*NewsSummary, error) {
	records, migrated, err := LoadArchive(rawDir)
	if err != nil {
		return nil, fmt.Errorf("加载新闻事实库: %w", err)
	}
	var incoming []data.NewsItem

	sinaNews, err := FetchSinaNews(80)
	if err == nil && len(sinaNews) > 0 {
		incoming = append(incoming, sinaNews...)
	}

	if len(incoming) == 0 && client != nil && client.Token != "" {
		today := time.Now().Format("20060102")
		weekAgo := time.Now().AddDate(0, 0, -7).Format("20060102")
		apiNews, err := client.FetchNews(ctx, weekAgo, today)
		if err == nil && len(apiNews) > 0 {
			incoming = append(incoming, apiNews...)
		}
	}
	if len(incoming) > 0 {
		records = mergeNewsRecords(records, incoming, time.Now())
	}
	if migrated || len(incoming) > 0 {
		if err := saveNewsArchive(rawDir, records); err != nil {
			return nil, err
		}
	}
	allNews := latestNewsItems(records)

	if len(allNews) == 0 {
		return nil, nil
	}

	summary := &NewsSummary{
		TotalNews: len(allNews),
	}

	if len(allNews) > 0 {
		summary.DateRange = fmt.Sprintf("%s ~ %s",
			allNews[len(allNews)-1].Datetime[:minInt(10, len(allNews[len(allNews)-1].Datetime))],
			allNews[0].Datetime[:minInt(10, len(allNews[0].Datetime))])
	}

	summary.HotTopics = extractKeywords(allNews, topN)
	recentNews := newsFromRecentCalendarDays(allNews, time.Now(), 2)
	summary.RecentNews = len(recentNews)
	summary.RecentHotTopics = extractKeywords(recentNews, topN)
	summary.HotStocks = matchStocks(allNews, rawDir, topN)

	return summary, nil
}

func (s *NewsSummary) Print() {
	if s == nil || s.TotalNews == 0 {
		return
	}
	fmt.Printf("\n========== 新闻热度 ==========\n")
	fmt.Printf("近7日新闻: %d 条 (%s)\n", s.TotalNews, s.DateRange)

	if len(s.HotTopics) > 0 {
		fmt.Println("\n热门话题:")
		for _, t := range s.HotTopics {
			stockStr := ""
			if len(t.Stocks) > 0 {
				stockStr = fmt.Sprintf(" → %s", strings.Join(t.Stocks, ", "))
			}
			fmt.Printf("  %s (×%d)%s\n", t.Keyword, t.Count, stockStr)
		}
	}
	if s.RecentNews > 0 {
		fmt.Printf("\n近2日热点（%d 条）:\n", s.RecentNews)
		for _, topic := range s.RecentHotTopics {
			fmt.Printf("  %s (×%d)\n", topic.Keyword, topic.Count)
		}
	}

	if len(s.HotStocks) > 0 {
		fmt.Println("\n受关注个股:")
		for _, s := range s.HotStocks {
			fmt.Printf("  %-10s %-8s 提及 %d 次\n", s.TsCode, s.Name, s.Mentions)
		}
	}
	fmt.Println("==============================")
}

func newsFromRecentCalendarDays(items []data.NewsItem, now time.Time, days int) []data.NewsItem {
	if days < 1 {
		return nil
	}
	location := now.Location()
	today := now.In(location).Format("20060102")
	cutoff := now.In(location).AddDate(0, 0, -(days - 1)).Format("20060102")
	recent := make([]data.NewsItem, 0, len(items))
	for _, item := range items {
		date := newsDateKey(item.Datetime)
		if date >= cutoff && date <= today {
			recent = append(recent, item)
		}
	}
	return recent
}

func newsDateKey(value string) string {
	value = strings.TrimSpace(value)
	digits := make([]byte, 0, 8)
	for i := 0; i < len(value) && len(digits) < 8; i++ {
		if value[i] >= '0' && value[i] <= '9' {
			digits = append(digits, value[i])
		}
	}
	if len(digits) != 8 {
		return ""
	}
	return string(digits)
}

func extractKeywords(news []data.NewsItem, topN int) []HotTopic {
	freq := make(map[string]int)
	allText := ""
	for _, n := range news {
		allText += n.Title + " "
	}

	runes := []rune(allText)
	for i := 0; i < len(runes)-1; i++ {
		for l := 2; l <= 5 && i+l <= len(runes); l++ {
			phrase := string(runes[i : i+l])
			if isValidPhrase(phrase) {
				freq[phrase]++
			}
		}
	}

	type kv struct {
		k string
		v int
	}
	var sorted []kv
	for k, v := range freq {
		if v >= 2 && !stopWords[k] {
			sorted = append(sorted, kv{k, v})
		}
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].v > sorted[j].v })

	n := topN
	if n > len(sorted) {
		n = len(sorted)
	}
	var topics []HotTopic
	for i := 0; i < n; i++ {
		topics = append(topics, HotTopic{Keyword: sorted[i].k, Count: sorted[i].v})
	}
	return topics
}

func isValidPhrase(s string) bool {
	for _, r := range s {
		if !unicode.Is(unicode.Han, r) && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	runes := []rune(s)
	if len(runes) < 2 {
		return false
	}
	hasHan := false
	for _, r := range runes {
		if unicode.Is(unicode.Han, r) {
			hasHan = true
			break
		}
	}
	return hasHan
}

func matchStocks(news []data.NewsItem, rawDir string, topN int) []HotStock {
	names := loadStockNameMap(rawDir)
	if len(names) == 0 {
		return nil
	}

	allText := ""
	for _, n := range news {
		allText += n.Title + " " + n.Content + " "
	}

	mentions := make(map[string]int)
	nameToCode := make(map[string]string)
	for code, name := range names {
		nameToCode[name] = code
		count := strings.Count(allText, name)
		if count > 0 {
			mentions[code] = count
		}
	}

	type kv struct {
		code string
		cnt  int
	}
	var sorted []kv
	for code, cnt := range mentions {
		sorted = append(sorted, kv{code, cnt})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].cnt > sorted[j].cnt })

	n := topN
	if n > len(sorted) {
		n = len(sorted)
	}
	var stocks []HotStock
	for i := 0; i < n; i++ {
		stocks = append(stocks, HotStock{
			TsCode:   sorted[i].code,
			Name:     names[sorted[i].code],
			Mentions: sorted[i].cnt,
		})
	}
	return stocks
}

func loadStockNameMap(rawDir string) map[string]string {
	names := make(map[string]string)
	f, err := os.Open(rawDir + "/stocks.parquet")
	if err != nil {
		return names
	}
	defer f.Close()

	reader := parquet.NewReader(f, parquet.SchemaOf(&data.StockInfo{}))
	defer reader.Close()
	for {
		var s data.StockInfo
		if err := reader.Read(&s); err != nil {
			break
		}
		if s.Name != "" {
			names[s.TsCode] = s.Name
		}
	}
	return names
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
