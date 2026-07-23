package portfolio

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"quant/internal/data"

	"gopkg.in/yaml.v3"
)

type Holding struct {
	Code   string  `yaml:"code"`
	Shares float64 `yaml:"shares"`
	Cost   float64 `yaml:"cost"`
	Date   string  `yaml:"date"`
	Name   string  `yaml:"name"`
}

type Portfolio struct {
	Holdings []Holding `yaml:"holdings"`
}

type PositionStatus struct {
	Code       string
	Name       string
	Shares     float64
	Cost       float64
	LastPrice  float64
	MarketVal  float64
	PnL        float64
	PnLPct     float64
	HoldDays   int
	LatestDate string
	Signals    []string
}

func Load(path string) (*Portfolio, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p Portfolio
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (p *Portfolio) ActiveCodes() []string {
	var codes []string
	for _, h := range p.Holdings {
		if h.Code != "" && h.Shares > 0 {
			codes = append(codes, h.Code)
		}
	}
	return codes
}

func (p *Portfolio) IsHolding(code string) bool {
	for _, h := range p.Holdings {
		if h.Code == code && h.Shares > 0 {
			return true
		}
	}
	return false
}

func Analyze(pf *Portfolio, barsMap map[string][]data.DailyBar, names map[string]string) []PositionStatus {
	if pf == nil || len(pf.Holdings) == 0 {
		return nil
	}

	var statuses []PositionStatus
	for _, h := range pf.Holdings {
		if h.Code == "" || h.Shares <= 0 {
			continue
		}
		bars, ok := barsMap[h.Code]
		if !ok || len(bars) == 0 {
			continue
		}
		sort.Slice(bars, func(i, j int) bool { return bars[i].TradeDate < bars[j].TradeDate })
		last := bars[len(bars)-1]
		lastPrice := last.Close
		name := h.Name
		if name == "" {
			if n, ok2 := names[h.Code]; ok2 {
				name = n
			} else {
				name = h.Code
			}
		}

		statuses = append(statuses, PositionStatus{
			Code:       h.Code,
			Name:       name,
			Shares:     h.Shares,
			Cost:       h.Cost,
			LastPrice:  lastPrice,
			MarketVal:  lastPrice * h.Shares,
			PnL:        (lastPrice - h.Cost) * h.Shares,
			PnLPct:     (lastPrice/h.Cost - 1) * 100,
			LatestDate: last.TradeDate,
		})
	}
	return statuses
}

func PrintStatus(statuses []PositionStatus) {
	if len(statuses) == 0 {
		return
	}

	var totalCost, totalMkt float64
	for _, s := range statuses {
		totalCost += s.Cost * s.Shares
		totalMkt += s.MarketVal
	}

	fmt.Println("\n========== 持仓概览 ==========")
	for _, s := range statuses {
		sign := "+"
		if s.PnLPct < 0 {
			sign = ""
		}
		bar := strings.Repeat("█", minInt(20, maxInt(0, int((s.PnLPct+50)/5))))
		if s.PnLPct < 0 {
			bar = strings.Repeat("█", minInt(20, maxInt(0, int((s.PnLPct*-1+50)/5))))
		}
		_ = bar
		fmt.Printf("  %-10s %-8s %6.0f股 | 成本%.2f | 现价%.2f | %s%.2f%% | %s%.0f\n",
			s.Code, s.Name, s.Shares, s.Cost, s.LastPrice, sign, s.PnLPct, sign, s.PnL)
	}
	totalPnL := (totalMkt/totalCost - 1) * 100
	fmt.Printf("  ----------------------------------------\n")
	fmt.Printf("  总成本: %.0f | 总市值: %.0f | 总盈亏: %+.2f%%\n", totalCost, totalMkt, totalPnL)
	fmt.Println("==============================")
}

func AnnotateSignals(results *[]interface{}, pf *Portfolio) {
	if pf == nil {
		return
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
