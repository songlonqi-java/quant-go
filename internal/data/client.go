package data

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

type Client struct {
	BaseURL        string
	Token          string
	RateLimitMs    int
	DailyCallLimit int
	HTTPClient     *http.Client
	mu             sync.Mutex
	callCount      int
	callDate       string
	lastCall       time.Time
}

func NewClient(baseURL, token string, rateLimitMs int) *Client {
	return &Client{
		BaseURL:        baseURL,
		Token:          token,
		RateLimitMs:    rateLimitMs,
		DailyCallLimit: 5000,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) SetDailyLimit(limit int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.DailyCallLimit = limit
}

func (c *Client) CallCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.callCount
}

func (c *Client) ResetCallCount() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.callCount = 0
	c.callDate = time.Now().Format("20060102")
}

func (c *Client) checkDailyLimit() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.DailyCallLimit <= 0 {
		return nil
	}
	today := time.Now().Format("20060102")
	if c.callDate != today {
		c.callCount = 0
		c.callDate = today
	}
	if c.callCount >= c.DailyCallLimit {
		return fmt.Errorf("已达每日调用上限 %d 次，请明天再试或提高 daily_call_limit", c.DailyCallLimit)
	}
	return nil
}

func (c *Client) trackCall() {
	c.mu.Lock()
	defer c.mu.Unlock()
	today := time.Now().Format("20060102")
	if c.callDate != today {
		c.callCount = 0
		c.callDate = today
	}
	c.callCount++
	c.lastCall = time.Now()
}

func (c *Client) rateLimit() {
	c.mu.Lock()
	defer c.mu.Unlock()
	elapsed := time.Since(c.lastCall)
	minGap := time.Duration(c.RateLimitMs) * time.Millisecond
	if elapsed < minGap {
		c.mu.Unlock()
		time.Sleep(minGap - elapsed)
		c.mu.Lock()
	}
	c.lastCall = time.Now()
}

func (c *Client) Call(ctx context.Context, apiName string, params map[string]interface{}, fields string, maxRetries int) ([]string, [][]interface{}, error) {
	if err := c.checkDailyLimit(); err != nil {
		return nil, nil, err
	}

	reqBody := TushareReq{
		APIName: apiName,
		Token:   c.Token,
		Params:  params,
		Fields:  fields,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt)) * time.Second
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
			select {
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		default:
		}

		c.rateLimit()

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL, bytes.NewBuffer(jsonData))
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("HTTP请求失败: %w", err)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("读取响应失败: %w", err)
			continue
		}

		var result TushareResp
		if err := json.Unmarshal(body, &result); err != nil {
			lastErr = fmt.Errorf("JSON解析失败: %w, body=%s", err, string(body[:minInt(len(body), 500)]))
			continue
		}

		if result.Code != 0 {
			lastErr = fmt.Errorf("Tushare错误 [%d]: %s", result.Code, result.Msg)
			if result.Code == -2001 || result.Code == 2001 {
				select {
				case <-ctx.Done():
					return nil, nil, ctx.Err()
				case <-time.After(5 * time.Second):
				}
			}
			continue
		}

		c.trackCall()
		return result.Data.Fields, result.Data.Items, nil
	}

	return nil, nil, fmt.Errorf("请求失败(已重试%d次): %w", maxRetries, lastErr)
}

func (c *Client) CallOnce(ctx context.Context, apiName string, params map[string]interface{}, fields string) ([]string, [][]interface{}, error) {
	return c.Call(ctx, apiName, params, fields, 2)
}

func (c *Client) CallPaginated(ctx context.Context, apiName string, params map[string]interface{}, fields string, pageSize int) ([]string, [][]interface{}, error) {
	var allFields []string
	var allItems [][]interface{}
	offset := 0
	for {
		p := make(map[string]interface{})
		for k, v := range params {
			p[k] = v
		}
		p["offset"] = offset
		p["limit"] = pageSize

		fields, items, err := c.Call(ctx, apiName, p, fields, 2)
		if err != nil {
			return nil, nil, fmt.Errorf("分页拉取失败 (offset=%d): %w", offset, err)
		}
		if len(allFields) == 0 {
			allFields = fields
		}
		allItems = append(allItems, items...)

		fmt.Printf("    分页: offset=%d, 本页 %d 条, 累计 %d 条\n", offset, len(items), len(allItems))

		if len(items) < pageSize {
			break
		}
		offset += pageSize

		if len(items) == 0 {
			break
		}
	}
	return allFields, allItems, nil
}

func (c *Client) LogStatus() {
	c.mu.Lock()
	defer c.mu.Unlock()
	fmt.Printf(">>> API调用统计: 今日已调用 %d 次, 上限 %d 次\n", c.callCount, c.DailyCallLimit)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
