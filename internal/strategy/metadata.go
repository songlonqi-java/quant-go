package strategy

type Group string

const (
	GroupTrend    Group = "trend"
	GroupVolume   Group = "volume"
	GroupReversal Group = "reversal"
	GroupValue    Group = "value"
	GroupPattern  Group = "pattern"
	GroupOther    Group = "other"
)

type Metadata struct {
	Name    string
	Horizon Horizon
	Group   Group
}

var strategyMetadata = map[string]Metadata{
	"limit_up":           {Name: "limit_up", Horizon: HorizonShort, Group: GroupVolume},
	"sar":                {Name: "sar", Horizon: HorizonShort, Group: GroupTrend},
	"kdj":                {Name: "kdj", Horizon: HorizonShort, Group: GroupReversal},
	"roc":                {Name: "roc", Horizon: HorizonShort, Group: GroupTrend},
	"williams_r":         {Name: "williams_r", Horizon: HorizonShort, Group: GroupReversal},
	"rsi":                {Name: "rsi", Horizon: HorizonShort, Group: GroupReversal},
	"mfi":                {Name: "mfi", Horizon: HorizonShort, Group: GroupReversal},
	"bull_flag":          {Name: "bull_flag", Horizon: HorizonShort, Group: GroupPattern},
	"bollinger":          {Name: "bollinger", Horizon: HorizonShort, Group: GroupReversal},
	"donchian":           {Name: "donchian", Horizon: HorizonShort, Group: GroupTrend},
	"volume_breakout":    {Name: "volume_breakout", Horizon: HorizonShort, Group: GroupVolume},
	"bottom_reversal":    {Name: "bottom_reversal", Horizon: HorizonShort, Group: GroupReversal},
	"ma_crossover":       {Name: "ma_crossover", Horizon: HorizonMid, Group: GroupTrend},
	"etf_rotation":       {Name: "etf_rotation", Horizon: HorizonMid, Group: GroupTrend},
	"macd":               {Name: "macd", Horizon: HorizonMid, Group: GroupTrend},
	"ma_sticky":          {Name: "ma_sticky", Horizon: HorizonMid, Group: GroupVolume},
	"value_ma60":         {Name: "value_ma60", Horizon: HorizonMid, Group: GroupValue},
	"relative_strength":  {Name: "relative_strength", Horizon: HorizonMid, Group: GroupTrend},
	"atr_breakout":       {Name: "atr_breakout", Horizon: HorizonMid, Group: GroupVolume},
	"trend_pullback":     {Name: "trend_pullback", Horizon: HorizonMid, Group: GroupPattern},
	"dividend_deviation": {Name: "dividend_deviation", Horizon: HorizonLong, Group: GroupValue},
	"quality_value":      {Name: "quality_value", Horizon: HorizonLong, Group: GroupValue},
	"earnings_growth":    {Name: "earnings_growth", Horizon: HorizonLong, Group: GroupValue},
}

func MetadataForStrategy(name string) Metadata {
	if meta, ok := strategyMetadata[name]; ok {
		return meta
	}
	return Metadata{Name: name, Horizon: HorizonMid, Group: GroupOther}
}

func GroupForStrategy(name string) Group {
	return MetadataForStrategy(name).Group
}
