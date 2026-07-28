package strategy

import "quant/internal/data"

// BottomReversal 底部放量反弹策略
// 前提: 股票从高点回落后, 在低位出现放量上涨 → 博超跌反弹, 2-3天离场
// 质量过滤: PE>0(盈利) + 市值下限 + 非ST + 有新闻提及(外部传入)
type BottomReversal struct {
	Lookback      int     // 回看天数 20
	DropThreshold float64 // 跌幅阈值 -15% (20日内跌超此幅度才算低位)
	VolumeRatio   float64 // 放量倍数 2.0
	MinMarketCap  float64 // 最小市值(亿) 100
	MinTurnover   float64 // 最小换手率 0.5
	fundStore     *data.FundamentalStore
}

func NewBottomReversal(lookback int, dropThreshold, volumeRatio float64, minMarketCap, minTurnover float64) *BottomReversal {
	return &BottomReversal{
		Lookback:      lookback,
		DropThreshold: dropThreshold,
		VolumeRatio:   volumeRatio,
		MinMarketCap:  minMarketCap,
		MinTurnover:   minTurnover,
	}
}

func (b *BottomReversal) SetFundStore(fs interface{}) {
	if s, ok := fs.(*data.FundamentalStore); ok {
		b.fundStore = s
	}
}

func (b *BottomReversal) Name() string { return "bottom_reversal" }
func (b *BottomReversal) Warmup() int  { return b.Lookback + 1 }

func (b *BottomReversal) Signal(bars []data.DailyBar, idx int) SignalType {
	if idx < b.Warmup() || idx < 1 {
		return Hold
	}

	cur := bars[idx]
	prev := bars[idx-1]

	// 1. 近期是否超跌
	start := idx - b.Lookback
	if start < 0 {
		start = 0
	}
	highest := bars[start].High
	for i := start; i <= idx; i++ {
		if bars[i].High > highest {
			highest = bars[i].High
		}
	}
	isOversold := false
	if highest > 0 {
		dropFromHigh := (cur.Close/highest - 1) * 100
		isOversold = dropFromHigh <= b.DropThreshold
	}

	// 2. 今日是否放量上涨
	avgVol := avgVolume(bars, idx-1, b.Lookback)
	isVolumeUp := avgVol > 0 && cur.Vol > avgVol*b.VolumeRatio
	isPriceUp := cur.Close > prev.Close

	// 3. 不是连续暴跌
	isStableRange := false
	if cur.Low > 0 {
		dayRange := (cur.High - cur.Low) / cur.Low * 100
		isStableRange = dayRange < 15
	}

	// 4. 趋势确认: MA5已拐头向上 且 价格站上MA20
	ma5 := sma(bars, idx, 5)
	prevMA5 := sma(bars, idx-1, 5)
	ma10 := sma(bars, idx, 10)
	ma20 := sma(bars, idx, 20)
	trendTurning := ma5 > 0 && ma10 > 0 && ma20 > 0 && ma5 > prevMA5 && cur.Close > ma20

	if isOversold && isVolumeUp && isPriceUp && isStableRange && trendTurning {
		// 质量过滤: 有基本面数据时, 检查PE和市值
		if b.fundStore != nil {
			code := bars[idx].TsCode
			mv := b.fundStore.GetMarketCap(code, bars[idx].TradeDate)
			if mv > 0 && mv < b.MinMarketCap*10000 {
				return Hold
			}
			pe, _, ok := b.fundStore.GetPEAsOf(code, bars[idx].TradeDate)
			if ok && pe <= 0 {
				return Hold
			}
			turnover := b.fundStore.GetTurnover(code, bars[idx].TradeDate)
			if turnover > 0 && turnover < b.MinTurnover {
				return Hold
			}
		}
		return Buy
	}

	// 卖出1: 收盘低于前日最低 (超短止损)
	if cur.Close < prev.Low {
		return Sell
	}
	// 卖出2: 跌破MA5 (反弹趋势走弱)
	if idx >= 5 {
		ma5 := sma(bars, idx, 5)
		prevMA5 := sma(bars, idx-1, 5)
		if ma5 > 0 && prevMA5 > 0 && cur.Close < ma5 && prev.Close >= prevMA5 {
			return Sell
		}
	}

	return Hold
}

func (b *BottomReversal) Score(bars []data.DailyBar, idx int) float64 {
	if idx < b.Warmup() {
		return 0
	}
	start := idx - b.Lookback
	if start < 0 {
		start = 0
	}
	highest := bars[start].High
	for i := start; i <= idx; i++ {
		if bars[i].High > highest {
			highest = bars[i].High
		}
	}
	if highest <= 0 {
		return 0
	}
	dropPct := (bars[idx].Close/highest - 1) * 100
	volBoost := 0.0
	avgV := avgVolume(bars, idx-1, b.Lookback)
	if avgV > 0 {
		volBoost = bars[idx].Vol / avgV
	}
	return -dropPct*0.3 + volBoost*2
}
