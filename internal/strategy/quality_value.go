package strategy

import "quant/internal/data"

// QualityValue 质量价值策略。
// 只在盈利、ROE、估值和长期趋势同时满足时给出长线买入信号。
type QualityValue struct {
	MAPeriod    int
	MaxPETTM    float64
	MaxPB       float64
	MinROE      float64
	MinDividend float64
	fundStore   *data.FundamentalStore
}

func NewQualityValue(maPeriod int, maxPETTM, maxPB, minROE, minDividend float64) *QualityValue {
	return &QualityValue{
		MAPeriod:    maPeriod,
		MaxPETTM:    maxPETTM,
		MaxPB:       maxPB,
		MinROE:      minROE,
		MinDividend: minDividend,
	}
}

func (q *QualityValue) SetFundStore(fs interface{}) {
	if s, ok := fs.(*data.FundamentalStore); ok {
		q.fundStore = s
	}
}

func (q *QualityValue) Name() string { return "quality_value" }
func (q *QualityValue) Warmup() int  { return maxInt(q.MAPeriod, 250) }

func (q *QualityValue) Signal(bars []data.DailyBar, idx int) SignalType {
	if idx < q.Warmup() || idx < 1 || q.fundStore == nil {
		return Hold
	}
	ma := sma(bars, idx, q.MAPeriod)
	prevMA := sma(bars, idx-1, q.MAPeriod)
	ma250 := sma(bars, idx, 250)
	if ma <= 0 || prevMA <= 0 || ma250 <= 0 {
		return Hold
	}
	closePrice := bars[idx].Close
	if closePrice < ma250 {
		return Sell
	}
	if !q.fundamentalsOK(bars[idx].TsCode, bars[idx].TradeDate) {
		if closePrice < ma {
			return Sell
		}
		return Hold
	}
	enteredTrend := bars[idx-1].Close <= prevMA && closePrice > ma
	stableTrend := closePrice > ma && ma >= prevMA && closePrice/ma <= 1.15
	if enteredTrend || stableTrend {
		return Buy
	}
	return Hold
}

func (q *QualityValue) Score(bars []data.DailyBar, idx int) float64 {
	if idx < q.Warmup() || q.fundStore == nil {
		return 0
	}
	db := q.fundStore.GetDailyBasicAsOf(bars[idx].TsCode, bars[idx].TradeDate)
	fi, ok := q.fundStore.GetFinaIndicatorAsOf(bars[idx].TsCode, bars[idx].TradeDate)
	if db == nil || !ok {
		return 0
	}
	pe := peValue(db)
	if pe <= 0 || db.Pb <= 0 {
		return 0
	}
	ma := sma(bars, idx, q.MAPeriod)
	trendScore := 0.0
	if ma > 0 {
		trendScore = (bars[idx].Close/ma - 1) * 20
	}
	valueScore := (q.MaxPETTM-pe)*0.5 + (q.MaxPB-db.Pb)*2
	qualityScore := (fi.Roe - q.MinROE) * 0.8
	dividendScore := dividendYield(db) * 1.5
	return valueScore + qualityScore + dividendScore + trendScore
}

func (q *QualityValue) fundamentalsOK(code, date string) bool {
	db := q.fundStore.GetDailyBasicAsOf(code, date)
	fi, ok := q.fundStore.GetFinaIndicatorAsOf(code, date)
	if db == nil || !ok {
		return false
	}
	pe := peValue(db)
	if pe <= 0 || pe > q.MaxPETTM {
		return false
	}
	if db.Pb <= 0 || db.Pb > q.MaxPB {
		return false
	}
	if fi.Roe < q.MinROE {
		return false
	}
	if dividendYield(db) < q.MinDividend {
		return false
	}
	return true
}

func peValue(db *data.DailyBasic) float64 {
	if db == nil {
		return 0
	}
	if db.PeTTM > 0 {
		return db.PeTTM
	}
	return db.Pe
}
