package news

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"quant/internal/data"
)

const sinaNewsURL = "https://feed.mix.sina.com.cn/api/roll/get?pageid=153&lid=2509&k=&num=80&page=1&r=%f&callback=jQuery"

var htmlTagRe = regexp.MustCompile(`<[^>]*>`)
var entityRe = regexp.MustCompile(`&[a-z]+;`)

type sinaResp struct {
	Result struct {
		Status struct {
			Code int `json:"code"`
		} `json:"status"`
		Total int `json:"total"`
		Data  []struct {
			Title    string `json:"title"`
			CTime    string `json:"ctime"`
			URL      string `json:"url"`
			WapURL   string `json:"wapurl"`
			Media    string `json:"media_name"`
			Intro    string `json:"intro"`
			Keywords string `json:"keywords"`
		} `json:"data"`
	} `json:"result"`
}

func FetchSinaNews(top int) ([]data.NewsItem, error) {
	url := fmt.Sprintf(sinaNewsURL, float64(time.Now().UnixNano())/1e9)
	url = strings.ReplaceAll(url, "&callback=jQuery", "")

	body, err := httpGet(url)
	if err != nil {
		return nil, err
	}

	var resp sinaResp
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, fmt.Errorf("解析新浪新闻失败: %w, body=%.200s", err, body)
	}

	var items []data.NewsItem
	for i, d := range resp.Result.Data {
		if top > 0 && i >= top {
			break
		}
		items = append(items, data.NewsItem{
			Datetime: parseSinaTime(d.CTime),
			Title:    cleanHTML(d.Title),
			Content:  cleanHTML(d.Intro),
			Source:   d.Media,
		})
	}
	return items, nil
}

func FetchSinaArticleContent(url string) string {
	if url == "" {
		return ""
	}
	body, err := httpGet(url)
	if err != nil {
		return ""
	}

	re := regexp.MustCompile(`(?s)<div class="article"[^>]*>(.*?)</div>`)
	matches := re.FindStringSubmatch(body)
	if len(matches) < 2 {
		re = regexp.MustCompile(`(?s)<div id="artibody"[^>]*>(.*?)</div>`)
		matches = re.FindStringSubmatch(body)
	}
	if len(matches) >= 2 {
		return cleanHTML(matches[1])
	}
	return ""
}

func parseSinaTime(ctime string) string {
	if ctime == "" {
		return time.Now().Format("20060102150405")
	}
	ts := 0
	fmt.Sscanf(ctime, "%d", &ts)
	if ts > 1000000000 {
		return time.Unix(int64(ts), 0).Format("20060102150405")
	}
	t, err := time.Parse("2006-01-02 15:04:05", ctime)
	if err != nil {
		return ctime
	}
	return t.Format("20060102150405")
}

func cleanHTML(s string) string {
	s = htmlTagRe.ReplaceAllString(s, " ")
	s = entityRe.ReplaceAllString(s, " ")
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimSpace(s)
	return s
}

func httpGet(url string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Referer", "https://finance.sina.com.cn/")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
