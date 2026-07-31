package realtime

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultEastmoneyListURL  = "https://82.push2.eastmoney.com/api/qt/clist/get"
	defaultEastmoneyQuoteURL = "https://push2.eastmoney.com/api/qt/ulist.np/get"
	eastmoneySource          = "eastmoney"
	eastmoneyFields          = "f2,f3,f4,f12,f13,f14,f15,f16,f17,f18,f124"
	eastmoneyListFS          = "m:0 t:6,m:0 t:80,m:1 t:2,m:1 t:23,m:0 t:81 s:2048"
)

// EastmoneyProvider reads the public Eastmoney snapshot endpoints used by
// AKShare. Fetch is for a small, explicit list of candidates or holdings;
// FetchPaced is for the complete market snapshot and spreads its paginated
// requests across the requested refresh window.
type EastmoneyProvider struct {
	ListURL    string
	QuoteURL   string
	HTTPClient *http.Client
	PageSize   int
	BatchSize  int
}

func NewEastmoneyProvider() *EastmoneyProvider {
	return &EastmoneyProvider{
		ListURL:  defaultEastmoneyListURL,
		QuoteURL: defaultEastmoneyQuoteURL,
		HTTPClient: &http.Client{
			Timeout: 8 * time.Second,
		},
		PageSize:  100,
		BatchSize: 100,
	}
}

func (p *EastmoneyProvider) Fetch(codes []string) ([]Quote, error) {
	codes = UniqueCodes(codes)
	if len(codes) == 0 {
		return nil, nil
	}
	p.ensureDefaults()

	var out []Quote
	for start := 0; start < len(codes); start += p.BatchSize {
		end := start + p.BatchSize
		if end > len(codes) {
			end = len(codes)
		}
		quotes, err := p.fetchQuoteBatch(codes[start:end])
		if err != nil {
			return out, err
		}
		out = append(out, quotes...)
	}
	return out, nil
}

func (p *EastmoneyProvider) FetchPaced(codes []string, window time.Duration) ([]Quote, FetchStats, error) {
	codes = UniqueCodes(codes)
	if len(codes) == 0 {
		return nil, FetchStats{Source: eastmoneySource}, nil
	}
	p.ensureDefaults()
	startedAt := time.Now()

	first, firstRequestedAt, err := p.fetchFirstListPageWithRetry()
	if err != nil {
		return nil, FetchStats{Source: eastmoneySource, Requested: len(codes), Batches: 1, Elapsed: time.Since(startedAt)}, err
	}
	if first.Total <= 0 || len(first.Diff) == 0 {
		return nil, FetchStats{Source: eastmoneySource, Requested: len(codes), Batches: 1, Elapsed: time.Since(startedAt)}, fmt.Errorf("东方财富实时行情返回为空")
	}

	pageCount := (first.Total + p.PageSize - 1) / p.PageSize
	stats := FetchStats{
		Source:    eastmoneySource,
		Requested: len(codes),
		Batches:   pageCount,
		Interval:  pacingInterval(pageCount, window),
	}
	rows := append([]eastmoneyQuoteRow(nil), first.Diff...)
	lastRequestAt := firstRequestedAt
	for page := 2; page <= pageCount; page++ {
		pageRows, requestedAt, err := p.fetchListPageWithRetry(page, stats.Interval, lastRequestAt)
		if !requestedAt.IsZero() {
			lastRequestAt = requestedAt
		}
		if err != nil {
			stats.Elapsed = time.Since(startedAt)
			return filterEastmoneyQuotes(rows, codes), stats, err
		}
		rows = append(rows, pageRows...)
	}
	stats.Elapsed = time.Since(startedAt)
	return filterEastmoneyQuotes(rows, codes), stats, nil
}

func (p *EastmoneyProvider) fetchFirstListPageWithRetry() (eastmoneyListResponseData, time.Time, error) {
	var lastErr error
	var requestedAt time.Time
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Second)
		}
		requestedAt = time.Now()
		payload, err := p.fetchListPage(1)
		if err == nil {
			return payload, requestedAt, nil
		}
		lastErr = err
	}
	return eastmoneyListResponseData{}, requestedAt, fmt.Errorf("东方财富实时行情首页重试失败: %w", lastErr)
}

func (p *EastmoneyProvider) ensureDefaults() {
	if p.ListURL == "" {
		p.ListURL = defaultEastmoneyListURL
	}
	if p.QuoteURL == "" {
		p.QuoteURL = defaultEastmoneyQuoteURL
	}
	if p.HTTPClient == nil {
		p.HTTPClient = &http.Client{Timeout: 8 * time.Second}
	}
	if p.PageSize <= 0 {
		p.PageSize = 100
	}
	if p.BatchSize <= 0 {
		p.BatchSize = 100
	}
}

func (p *EastmoneyProvider) fetchListPageWithRetry(page int, interval time.Duration, lastRequestAt time.Time) ([]eastmoneyQuoteRow, time.Time, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if !lastRequestAt.IsZero() && interval > 0 {
			if wait := interval - time.Since(lastRequestAt); wait > 0 {
				time.Sleep(wait)
			}
		}
		requestedAt := time.Now()
		payload, err := p.fetchListPage(page)
		if err == nil {
			return payload.Diff, requestedAt, nil
		}
		lastErr = err
		lastRequestAt = requestedAt
	}
	return nil, lastRequestAt, fmt.Errorf("东方财富实时行情第%d页重试失败: %w", page, lastErr)
}

func (p *EastmoneyProvider) fetchListPage(page int) (eastmoneyListResponseData, error) {
	params := url.Values{
		"pn":     {strconv.Itoa(page)},
		"pz":     {strconv.Itoa(p.PageSize)},
		"po":     {"1"},
		"np":     {"1"},
		"ut":     {"bd1d9ddb04089700cf9c27f6f7426281"},
		"fltt":   {"2"},
		"invt":   {"2"},
		"fid":    {"f12"},
		"fs":     {eastmoneyListFS},
		"fields": {eastmoneyFields},
	}
	var response eastmoneyResponse
	if err := p.getJSON(p.ListURL+"?"+params.Encode(), &response); err != nil {
		return eastmoneyListResponseData{}, err
	}
	if response.RC != 0 || response.Data == nil {
		return eastmoneyListResponseData{}, fmt.Errorf("东方财富实时行情响应异常: rc=%d", response.RC)
	}
	return *response.Data, nil
}

func (p *EastmoneyProvider) fetchQuoteBatch(codes []string) ([]Quote, error) {
	secids := make([]string, 0, len(codes))
	for _, code := range codes {
		secid, ok := ToEastmoneySecID(code)
		if ok {
			secids = append(secids, secid)
		}
	}
	if len(secids) == 0 {
		return nil, nil
	}
	params := url.Values{
		"fltt":   {"2"},
		"invt":   {"2"},
		"fields": {eastmoneyFields},
		"secids": {strings.Join(secids, ",")},
	}
	var response eastmoneyResponse
	if err := p.getJSON(p.QuoteURL+"?"+params.Encode(), &response); err != nil {
		return nil, err
	}
	if response.RC != 0 || response.Data == nil {
		return nil, fmt.Errorf("东方财富候选实时行情响应异常: rc=%d", response.RC)
	}
	return eastmoneyRowsToQuotes(response.Data.Diff), nil
}

func (p *EastmoneyProvider) getJSON(endpoint string, target any) error {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Referer", "https://quote.eastmoney.com/")
	resp, err := p.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("东方财富实时行情 HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("解析东方财富实时行情: %w", err)
	}
	return nil
}

func filterEastmoneyQuotes(rows []eastmoneyQuoteRow, codes []string) []Quote {
	wanted := make(map[string]bool, len(codes))
	for _, code := range codes {
		wanted[code] = true
	}
	quotes := eastmoneyRowsToQuotes(rows)
	out := make([]Quote, 0, len(quotes))
	for _, quote := range quotes {
		if wanted[quote.Code] {
			out = append(out, quote)
		}
	}
	return out
}

func eastmoneyRowsToQuotes(rows []eastmoneyQuoteRow) []Quote {
	quotes := make([]Quote, 0, len(rows))
	for _, row := range rows {
		code, ok := FromEastmoneyCode(row.Code, int(row.Market))
		if !ok {
			continue
		}
		quotes = append(quotes, Quote{
			Code:      code,
			Name:      row.Name,
			Open:      float64(row.Open),
			PrevClose: float64(row.PrevClose),
			Current:   float64(row.Current),
			High:      float64(row.High),
			Low:       float64(row.Low),
			Change:    float64(row.Change),
			ChangePct: float64(row.ChangePct),
			UpdateAt:  eastmoneyTimestamp(int64(row.Timestamp)),
			Source:    eastmoneySource,
		})
	}
	return quotes
}

func ToEastmoneySecID(tsCode string) (string, bool) {
	parts := strings.Split(strings.ToUpper(strings.TrimSpace(tsCode)), ".")
	if len(parts) != 2 || len(parts[0]) != 6 {
		return "", false
	}
	switch parts[1] {
	case "SH":
		return "1." + parts[0], true
	case "SZ", "BJ":
		return "0." + parts[0], true
	default:
		return "", false
	}
}

func FromEastmoneyCode(code string, market int) (string, bool) {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return "", false
	}
	if market == 1 || strings.HasPrefix(code, "6") {
		return code + ".SH", true
	}
	if strings.HasPrefix(code, "4") || strings.HasPrefix(code, "8") || strings.HasPrefix(code, "9") {
		return code + ".BJ", true
	}
	return code + ".SZ", true
}

func eastmoneyTimestamp(seconds int64) string {
	if seconds <= 0 {
		return ""
	}
	return time.Unix(seconds, 0).In(chinaLocation).Format("2006-01-02 15:04:05")
}

type eastmoneyResponse struct {
	RC   int                        `json:"rc"`
	Data *eastmoneyListResponseData `json:"data"`
}

type eastmoneyListResponseData struct {
	Total int                 `json:"total"`
	Diff  []eastmoneyQuoteRow `json:"diff"`
}

type eastmoneyQuoteRow struct {
	Current   eastmoneyNumber `json:"f2"`
	ChangePct eastmoneyNumber `json:"f3"`
	Change    eastmoneyNumber `json:"f4"`
	Code      string          `json:"f12"`
	Market    eastmoneyNumber `json:"f13"`
	Name      string          `json:"f14"`
	High      eastmoneyNumber `json:"f15"`
	Low       eastmoneyNumber `json:"f16"`
	Open      eastmoneyNumber `json:"f17"`
	PrevClose eastmoneyNumber `json:"f18"`
	Timestamp eastmoneyNumber `json:"f124"`
}

// Eastmoney uses "-" for a suspended or unavailable quote. A tolerant
// number avoids rejecting an otherwise useful market snapshot for one stock.
type eastmoneyNumber float64

func (n *eastmoneyNumber) UnmarshalJSON(data []byte) error {
	text := strings.Trim(string(data), "\"")
	if text == "" || text == "-" || text == "null" {
		*n = 0
		return nil
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return err
	}
	*n = eastmoneyNumber(value)
	return nil
}
