package strategy

import "quant/internal/data"

// ROC 动量突破 (Rate of Change)
// N日涨跌幅突破阈值且放量 → 买入；N日跌幅突破阈值 → 卖出
type ROC struct {
	Period    int     // 12 日
	BuyThresh float64 // 买入阈值 5%
	SellThresh float64 // 卖出阈值 -5%
}

func NewROC(period int, buyThresh, sellThresh float64) *ROC {
	return &ROC{Period: period, BuyThresh: buyThresh, SellThresh: sellThresh}
}

func (r *ROC) Name() string { return "roc" }
func (r *ROC) Warmup() int  { return r.Period }

func (r *ROC) Signal(bars []data.DailyBar, idx int) SignalType {
	if idx < r.Warmup() || idx < 1 {
		return Hold
	}

	rocVal := roc(bars, idx, r.Period)

	if rocVal > r.BuyThresh && bars[idx].Close > bars[idx-1].Close {
		return Buy
	}
	if rocVal < r.SellThresh && bars[idx].Close < bars[idx-1].Close {
		return Sell
	}
	return Hold
}

func (r *ROC) Score(bars []data.DailyBar, idx int) float64 {
	if idx < r.Warmup() {
		return 0
	}
	return roc(bars, idx, r.Period)
}

func roc(bars []data.DailyBar, idx, period int) float64 {
	start := idx - period
	if start < 0 {
		start = 0
	}
	if bars[start].Close <= 0 {
		return 0
	}
	return (bars[idx].Close/bars[start].Close - 1) * 100
}
