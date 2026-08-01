package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Config struct {
	BaseURL string
	APIKey  string
	Model   string
	Timeout time.Duration
}

type Client struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type completionRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type completionResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type Completion struct {
	Content          string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.APIKey) == "" || strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("AI base_url、api_key 和 model 均不能为空")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}
	return &Client{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"), apiKey: cfg.APIKey, model: cfg.Model,
		http: &http.Client{Timeout: cfg.Timeout},
	}, nil
}

func (c *Client) Complete(ctx context.Context, system, prompt string) (*Completion, error) {
	payload, err := json.Marshal(completionRequest{Model: c.model, Messages: []Message{
		{Role: "system", Content: system}, {Role: "user", Content: prompt},
	}})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	var decoded completionResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("解析 AI 响应失败（HTTP %d）", response.StatusCode)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if decoded.Error != nil && decoded.Error.Message != "" {
			return nil, fmt.Errorf("AI 请求失败（HTTP %d）：%s", response.StatusCode, decoded.Error.Message)
		}
		return nil, fmt.Errorf("AI 请求失败（HTTP %d）", response.StatusCode)
	}
	if len(decoded.Choices) == 0 || strings.TrimSpace(decoded.Choices[0].Message.Content) == "" {
		return nil, fmt.Errorf("AI 响应没有内容")
	}
	totalTokens := decoded.Usage.TotalTokens
	if totalTokens == 0 && (decoded.Usage.PromptTokens > 0 || decoded.Usage.CompletionTokens > 0) {
		totalTokens = decoded.Usage.PromptTokens + decoded.Usage.CompletionTokens
	}
	return &Completion{
		Content: strings.TrimSpace(decoded.Choices[0].Message.Content), PromptTokens: decoded.Usage.PromptTokens,
		CompletionTokens: decoded.Usage.CompletionTokens, TotalTokens: totalTokens,
	}, nil
}

func (c *Client) Model() string { return c.model }
