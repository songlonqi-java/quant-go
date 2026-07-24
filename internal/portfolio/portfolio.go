package portfolio

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"quant/internal/data"
	"quant/internal/realtime"

	"gopkg.in/yaml.v3"
)

type Transaction struct {
	Date    string  `yaml:"date"`
	Code    string  `yaml:"code"`
	Action  string  `yaml:"action"` // buy / sell
	Shares  float64 `yaml:"shares"`
	Price   float64 `yaml:"price"`
	Comment string  `yaml:"comment"`
}

type Ledger struct {
	Transactions []Transaction `yaml:"transactions"`
}

type Holding struct {
	Code      string
	Name      string
	Shares    float64
	AvgCost   float64
	TotalCost float64
}

type ClosedTrade struct {
	Code      string
	Name      string
	BuyDate   string
	SellDate  string
	Shares    float64
	BuyPrice  float64
	SellPrice float64
	Return    float64
	PnL       float64
}

type Summary struct {
	Holdings      []PositionStatus
	ClosedTrades  []ClosedTrade
	TotalRealized float64
	WinCount      int
	LossCount     int
	WinRate       float64
	AvgReturn     float64
}

type PositionStatus struct {
	Code      string
	Name      string
	Shares    float64
	Cost      float64
	LastPrice float64
	MarketVal float64
	PnL       float64
	PnLPct    float64
}

func Load(path string) (*Ledger, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var l Ledger
	if err := yaml.Unmarshal(data, &l); err != nil {
		return nil, err
	}
	return &l, nil
}

func (l *Ledger) CurrentHoldings() []Holding {
	pos := make(map[string]*Holding)
	for _, t := range l.Transactions {
		if pos[t.Code] == nil {
			pos[t.Code] = &Holding{Code: t.Code}
		}
		h := pos[t.Code]
		if t.Action == "buy" {
			totalBefore := h.TotalCost
			h.Shares += t.Shares
			h.TotalCost += t.Shares * t.Price
			if h.Shares > 0 {
				h.AvgCost = h.TotalCost / h.Shares
			}
			_ = totalBefore
		} else if t.Action == "sell" {
			if h.Shares > 0 {
				ratio := t.Shares / h.Shares
				h.TotalCost -= h.TotalCost * ratio
				h.Shares -= t.Shares
				if h.Shares > 0 {
					h.AvgCost = h.TotalCost / h.Shares
				} else {
					h.AvgCost = 0
					h.TotalCost = 0
				}
			}
		}
	}

	var holdings []Holding
	for _, h := range pos {
		if h.Shares > 0 {
			holdings = append(holdings, *h)
		}
	}
	sort.Slice(holdings, func(i, j int) bool { return holdings[i].Code < holdings[j].Code })
	return holdings
}

func (l *Ledger) ClosedTrades() []ClosedTrade {
	type open struct {
		date   string
		price  float64
		shares float64
	}
	openBuys := make(map[string][]open)
	var closed []ClosedTrade

	for _, t := range l.Transactions {
		if t.Action == "buy" {
			openBuys[t.Code] = append(openBuys[t.Code], open{t.Date, t.Price, t.Shares})
		} else if t.Action == "sell" {
			remaining := t.Shares
			for len(openBuys[t.Code]) > 0 && remaining > 0 {
				first := &openBuys[t.Code][0]
				match := first.shares
				if match > remaining {
					match = remaining
				}
				pnl := match * (t.Price - first.price)
				ret := (t.Price/first.price - 1) * 100
				closed = append(closed, ClosedTrade{
					Code:      t.Code,
					BuyDate:   first.date,
					SellDate:  t.Date,
					Shares:    match,
					BuyPrice:  first.price,
					SellPrice: t.Price,
					Return:    ret,
					PnL:       pnl,
				})
				first.shares -= match
				remaining -= match
				if first.shares <= 0 {
					openBuys[t.Code] = openBuys[t.Code][1:]
				}
			}
		}
	}
	sort.Slice(closed, func(i, j int) bool { return closed[i].SellDate < closed[j].SellDate })
	return closed
}

func Analyze(ledger *Ledger, barsMap map[string][]data.DailyBar, names map[string]string) *Summary {
	if ledger == nil {
		return nil
	}

	s := &Summary{}

	holdings := ledger.CurrentHoldings()
	for _, h := range holdings {
		name := h.Code
		if n, ok := names[h.Code]; ok {
			name = n
		}
		bars, ok := barsMap[h.Code]
		lastPrice := h.AvgCost
		if ok && len(bars) > 0 {
			sort.Slice(bars, func(i, j int) bool { return bars[i].TradeDate < bars[j].TradeDate })
			lastPrice = bars[len(bars)-1].TradeClose()
		}
		s.Holdings = append(s.Holdings, PositionStatus{
			Code:      h.Code,
			Name:      name,
			Shares:    h.Shares,
			Cost:      h.AvgCost,
			LastPrice: lastPrice,
			MarketVal: lastPrice * h.Shares,
			PnL:       (lastPrice - h.AvgCost) * h.Shares,
			PnLPct:    (lastPrice/h.AvgCost - 1) * 100,
		})
	}

	s.ClosedTrades = ledger.ClosedTrades()
	for _, t := range s.ClosedTrades {
		if n, ok := names[t.Code]; ok {
			t.Name = n
		}
		s.TotalRealized += t.PnL
		if t.Return > 0 {
			s.WinCount++
		} else {
			s.LossCount++
		}
	}
	totalTrades := s.WinCount + s.LossCount
	if totalTrades > 0 {
		s.WinRate = float64(s.WinCount) / float64(totalTrades) * 100
	}
	return s
}

func ApplyRealtimeQuotes(s *Summary, quotes map[string]realtime.Quote) {
	if s == nil || len(quotes) == 0 {
		return
	}
	for i := range s.Holdings {
		q, ok := quotes[s.Holdings[i].Code]
		if !ok || q.Current <= 0 {
			continue
		}
		h := &s.Holdings[i]
		h.LastPrice = q.Current
		h.MarketVal = h.LastPrice * h.Shares
		h.PnL = (h.LastPrice - h.Cost) * h.Shares
		if h.Cost > 0 {
			h.PnLPct = (h.LastPrice/h.Cost - 1) * 100
		}
	}
}

func PrintSummary(s *Summary) {
	if s == nil || (len(s.Holdings) == 0 && len(s.ClosedTrades) == 0) {
		return
	}

	fmt.Println("\n========== 持仓概览 ==========")
	if len(s.Holdings) == 0 {
		fmt.Println("  空仓")
	} else {
		var totalCost, totalMkt float64
		for _, h := range s.Holdings {
			totalCost += h.Cost * h.Shares
			totalMkt += h.MarketVal
			sign := "+"
			if h.PnLPct < 0 {
				sign = ""
			}
			bar := pnlBar(h.PnLPct)
			fmt.Printf("  %-10s %-8s %6.0f股 | %s | %s | %s%.1f%% | %s%.0f %s\n",
				h.Code, h.Name, h.Shares,
				fmtCost(h.Cost), fmtCost(h.LastPrice),
				sign, h.PnLPct, sign, h.PnL, bar)
		}
		totalPnL := (totalMkt/totalCost - 1) * 100
		fmt.Printf("  %s\n", strings.Repeat("-", 60))
		fmt.Printf("  总成本: %.0f | 总市值: %.0f | 浮动盈亏: %+.2f%%\n", totalCost, totalMkt, totalPnL)
	}
	fmt.Println("==============================")

	if len(s.ClosedTrades) > 0 {
		fmt.Println("\n========== 历史交易 ==========")
		var totalPnL float64
		for _, t := range s.ClosedTrades {
			name := t.Code
			if t.Name != "" {
				name = t.Name
			}
			totalPnL += t.PnL
			sign := "+"
			if t.Return < 0 {
				sign = ""
			}
			fmt.Printf("  %s → %s  %-10s %-8s %4.0f股 | %.2f→%.2f | %s%.1f%% | %s%.0f\n",
				t.BuyDate, t.SellDate, t.Code, name, t.Shares,
				t.BuyPrice, t.SellPrice, sign, t.Return, sign, t.PnL)
		}
		winRate := float64(s.WinCount) / float64(s.WinCount+s.LossCount) * 100
		fmt.Printf("  %s\n", strings.Repeat("-", 60))
		fmt.Printf("  已平仓 %d 笔 | 胜率 %.0f%% | 累计盈亏 %+.0f\n",
			s.WinCount+s.LossCount, winRate, totalPnL)
		fmt.Println("==============================")
	}
}

func fmtCost(v float64) string {
	if v >= 1000 {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.2f", v)
}

func pnlBar(pct float64) string {
	n := int(pct/10 + 0.5)
	if n < 0 {
		n = -n
	}
	if n > 15 {
		n = 15
	}
	if pct > 0 {
		return strings.Repeat("█", n)
	}
	return strings.Repeat("░", n)
}

type ReportData struct {
	GeneratedAt   string           `json:"generated_at"`
	Holdings      []PositionStatus `json:"holdings"`
	ClosedTrades  []ClosedTrade    `json:"closed_trades"`
	TotalRealized float64          `json:"total_realized"`
	WinRate       float64          `json:"win_rate"`
	TotalTrades   int              `json:"total_trades"`
}

func SaveReport(s *Summary, path string) error {
	if s == nil {
		return nil
	}
	os.MkdirAll(path, 0755)

	r := ReportData{
		GeneratedAt:   time.Now().Format("2006-01-02 15:04:05"),
		Holdings:      s.Holdings,
		ClosedTrades:  s.ClosedTrades,
		TotalRealized: s.TotalRealized,
		WinRate:       s.WinRate,
		TotalTrades:   s.WinCount + s.LossCount,
	}

	// JSON: 完整数据
	jsonPath := path + "/portfolio_summary.json"
	data, _ := json.MarshalIndent(r, "", "  ")
	os.WriteFile(jsonPath, data, 0644)

	// CSV: 已平仓交易(方便表格统计)
	csvPath := path + "/closed_trades.csv"
	f, err := os.Create(csvPath)
	if err == nil {
		defer f.Close()
		f.WriteString("代码,名称,买入日,卖出日,股数,买入价,卖出价,收益率%,盈亏\n")
		for _, t := range s.ClosedTrades {
			name := t.Code
			if t.Name != "" {
				name = t.Name
			}
			f.WriteString(fmt.Sprintf("%s,%s,%s,%s,%.0f,%.2f,%.2f,%.1f,%.0f\n",
				t.Code, name, t.BuyDate, t.SellDate, t.Shares, t.BuyPrice, t.SellPrice, t.Return, t.PnL))
		}
	}

	// CSV: 当前持仓
	holdPath := path + "/current_holdings.csv"
	hf, err := os.Create(holdPath)
	if err == nil {
		defer hf.Close()
		hf.WriteString("代码,名称,股数,成本价,现价,市值,浮动盈亏%,浮动盈亏\n")
		for _, h := range s.Holdings {
			hf.WriteString(fmt.Sprintf("%s,%s,%.0f,%.2f,%.2f,%.0f,%.1f,%.0f\n",
				h.Code, h.Name, h.Shares, h.Cost, h.LastPrice, h.MarketVal, h.PnLPct, h.PnL))
		}
	}

	fmt.Printf(">>> 报表已保存: %s/*.json, *.csv\n", path)
	return nil
}
