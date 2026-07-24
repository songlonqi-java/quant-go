package strategy

type Horizon string

const (
	HorizonShort Horizon = "short"
	HorizonMid   Horizon = "mid"
	HorizonLong  Horizon = "long"
)

func HorizonOrder() []Horizon {
	return []Horizon{HorizonShort, HorizonMid, HorizonLong}
}

func HorizonForStrategy(name string) Horizon {
	switch name {
	case "limit_up", "sar", "kdj", "roc", "williams_r", "rsi", "mfi", "bull_flag", "bollinger", "donchian", "volume_breakout", "bottom_reversal":
		return HorizonShort
	case "dividend_deviation":
		return HorizonLong
	default:
		return HorizonMid
	}
}

func HorizonLabel(h Horizon) string {
	switch h {
	case HorizonShort:
		return "短线"
	case HorizonMid:
		return "中线"
	case HorizonLong:
		return "长线"
	default:
		return "未分类"
	}
}

func HorizonDescription(h Horizon) string {
	switch h {
	case HorizonShort:
		return "明日到5个交易日，关注入场条件和快速失效"
	case HorizonMid:
		return "2到8周，关注趋势延续、回踩和波动突破"
	case HorizonLong:
		return "数月以上，关注估值、分红、质量和长期配置"
	default:
		return ""
	}
}
