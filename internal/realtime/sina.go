package realtime

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	defaultSinaURL = "https://hq.sinajs.cn/list="
	sinaSource     = "sina"
)

type SinaProvider struct {
	BaseURL    string
	HTTPClient *http.Client
	BatchSize  int
}

func NewSinaProvider() *SinaProvider {
	return &SinaProvider{
		BaseURL: defaultSinaURL,
		HTTPClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		BatchSize: 80,
	}
}

func (p *SinaProvider) Fetch(codes []string) ([]Quote, error) {
	codes = UniqueCodes(codes)
	if len(codes) == 0 {
		return nil, nil
	}
	if p.HTTPClient == nil {
		p.HTTPClient = &http.Client{Timeout: 5 * time.Second}
	}
	if p.BaseURL == "" {
		p.BaseURL = defaultSinaURL
	}
	if p.BatchSize <= 0 {
		p.BatchSize = 80
	}

	var out []Quote
	for start := 0; start < len(codes); start += p.BatchSize {
		end := start + p.BatchSize
		if end > len(codes) {
			end = len(codes)
		}
		quotes, err := p.fetchBatch(codes[start:end])
		if err != nil {
			return out, err
		}
		out = append(out, quotes...)
	}
	return out, nil
}

func (p *SinaProvider) fetchBatch(codes []string) ([]Quote, error) {
	symbols := make([]string, 0, len(codes))
	for _, code := range codes {
		symbol, ok := ToSinaSymbol(code)
		if ok {
			symbols = append(symbols, symbol)
		}
	}
	if len(symbols) == 0 {
		return nil, nil
	}

	req, err := http.NewRequest(http.MethodGet, p.BaseURL+strings.Join(symbols, ","), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://finance.sina.com.cn")

	resp, err := p.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("新浪实时行情 HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return ParseSinaResponse(string(body)), nil
}

func ToSinaSymbol(tsCode string) (string, bool) {
	parts := strings.Split(strings.ToUpper(strings.TrimSpace(tsCode)), ".")
	if len(parts) != 2 || len(parts[0]) != 6 {
		return "", false
	}
	switch parts[1] {
	case "SH":
		return "sh" + parts[0], true
	case "SZ":
		return "sz" + parts[0], true
	case "BJ":
		return "bj" + parts[0], true
	default:
		return "", false
	}
}

func FromSinaSymbol(symbol string) (string, bool) {
	symbol = strings.ToLower(strings.TrimSpace(symbol))
	if len(symbol) != 8 {
		return "", false
	}
	code := strings.ToUpper(symbol[2:])
	switch symbol[:2] {
	case "sh":
		return code + ".SH", true
	case "sz":
		return code + ".SZ", true
	case "bj":
		return code + ".BJ", true
	default:
		return "", false
	}
}

var sinaLineRE = regexp.MustCompile(`var hq_str_(\w+)="(.*?)";`)

func ParseSinaResponse(response string) []Quote {
	var quotes []Quote
	for _, line := range strings.Split(response, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		match := sinaLineRE.FindStringSubmatch(line)
		if len(match) != 3 || match[2] == "" {
			continue
		}
		q, ok := parseSinaAShareQuote(match[1], match[2])
		if ok {
			quotes = append(quotes, q)
		}
	}
	return quotes
}

func parseSinaAShareQuote(symbol, payload string) (Quote, bool) {
	code, ok := FromSinaSymbol(symbol)
	if !ok {
		return Quote{}, false
	}
	fields := strings.Split(payload, ",")
	if len(fields) < 32 {
		return Quote{}, false
	}

	prevClose := parseFloat(fields[2])
	current := parseFloat(fields[3])
	change := round2(current - prevClose)
	changePct := 0.0
	if prevClose > 0 {
		changePct = round2(change / prevClose * 100)
	}
	updateAt := fields[30]
	if fields[31] != "" {
		updateAt += " " + fields[31]
	}

	return Quote{
		Code:      code,
		Name:      fields[0],
		Open:      parseFloat(fields[1]),
		PrevClose: prevClose,
		Current:   current,
		High:      parseFloat(fields[4]),
		Low:       parseFloat(fields[5]),
		Change:    change,
		ChangePct: changePct,
		UpdateAt:  updateAt,
		Source:    sinaSource,
	}, true
}

func parseFloat(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return v
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
