package execution

import "quant/internal/data"

func CanBuyAtOpen(prev, current data.DailyBar, limitPct float64) bool {
	open := current.TradeOpen()
	if open <= 0 || current.Vol <= 0 {
		return false
	}
	prevClose := prev.TradeClose()
	if current.UpLimit > 0 {
		return !current.IsLimitUpOpen()
	}
	if limitPct > 0 {
		return !data.IsApproxLimitUpWithThreshold(open, prevClose, limitPct)
	}
	return !data.IsApproxLimitUp(current.TsCode, open, prevClose)
}

func CanSellAtOpen(prev, current data.DailyBar, limitPct float64) bool {
	open := current.TradeOpen()
	if open <= 0 || current.Vol <= 0 {
		return false
	}
	prevClose := prev.TradeClose()
	if current.DownLimit > 0 {
		return !current.IsLimitDownOpen()
	}
	if limitPct > 0 {
		return !data.IsApproxLimitDownWithThreshold(open, prevClose, limitPct)
	}
	return !data.IsApproxLimitDown(current.TsCode, open, prevClose)
}
