package signal

import (
	"fmt"

	"quant/internal/data"
	"quant/internal/realtime"
)

func ApplyRealtimeQuotes(results []SignalResult, quotes map[string]realtime.Quote, limitStore *data.StkLimitStore) {
	if len(quotes) == 0 {
		return
	}
	for i := range results {
		q, ok := quotes[results[i].Code]
		if !ok || q.Current <= 0 {
			continue
		}
		results[i].HasRealtime = true
		results[i].RealtimePrice = q.Current
		results[i].RealtimeChangePct = q.ChangePct
		results[i].RealtimeUpdateAt = q.UpdateAt
		results[i].IntradayLabels = intradayLabels(results[i], q, limitStore)
		if len(results[i].IntradayLabels) > 0 {
			results[i].Reasons = append(results[i].Reasons, fmt.Sprintf("盘中: %s", joinLabels(results[i].IntradayLabels)))
		}
	}
	RefreshRiskPolicy(results)
}

func intradayLabels(r SignalResult, q realtime.Quote, limitStore *data.StkLimitStore) []string {
	labels := make([]string, 0, 4)
	limitUp, limitDown := realtimeLimitStatus(q, limitStore)

	switch r.Recommendation() {
	case "买入":
		if limitUp {
			labels = append(labels, "涨停风险")
		}
		if limitDown {
			labels = append(labels, "跌停风险")
		}
		if q.PrevClose > 0 && (q.Open/q.PrevClose-1)*100 > 3 {
			labels = append(labels, "高开>3%")
		}
		if q.ChangePct > 5 && !limitUp {
			labels = append(labels, "涨幅偏高")
		}
		if q.ChangePct < -2 {
			labels = append(labels, "盘中走弱")
		}
	case "卖出":
		if limitDown {
			labels = append(labels, "跌停风险")
		}
		if q.ChangePct < -3 && !limitDown {
			labels = append(labels, "卖压确认")
		}
		if q.ChangePct > 2 {
			labels = append(labels, "卖出缓和")
		}
	}
	return labels
}

func realtimeLimitStatus(q realtime.Quote, limitStore *data.StkLimitStore) (bool, bool) {
	if limitStore != nil {
		if limit, ok := limitStore.Get(q.Code, q.TradeDate()); ok {
			limitUp := limit.UpLimit > 0 && q.Current >= limit.UpLimit*0.999
			limitDown := limit.DownLimit > 0 && q.Current <= limit.DownLimit*1.001
			return limitUp, limitDown
		}
	}
	return data.IsApproxLimitUp(q.Code, q.Current, q.PrevClose), data.IsApproxLimitDown(q.Code, q.Current, q.PrevClose)
}

func joinLabels(labels []string) string {
	out := ""
	for i, label := range labels {
		if i > 0 {
			out += ","
		}
		out += label
	}
	return out
}
