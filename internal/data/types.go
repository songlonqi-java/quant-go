package data

type DailyBar struct {
	TsCode    string  `parquet:"ts_code"`
	TradeDate string  `parquet:"trade_date"`
	Open      float64 `parquet:"open"`
	High      float64 `parquet:"high"`
	Low       float64 `parquet:"low"`
	Close     float64 `parquet:"close"`
	Vol       float64 `parquet:"vol"`
	Amount    float64 `parquet:"amount"`
	RawOpen   float64 `parquet:"raw_open"`
	RawHigh   float64 `parquet:"raw_high"`
	RawLow    float64 `parquet:"raw_low"`
	RawClose  float64 `parquet:"raw_close"`
	AdjFactor float64 `parquet:"adj_factor"`
	UpLimit   float64 `parquet:"up_limit"`
	DownLimit float64 `parquet:"down_limit"`
}

type StockInfo struct {
	TsCode     string `parquet:"ts_code"`
	Symbol     string `parquet:"symbol"`
	Name       string `parquet:"name"`
	Market     string `parquet:"market"`
	Industry   string `parquet:"industry"`
	ListDate   string `parquet:"list_date"`
	DelistDate string `parquet:"delist_date"`
}

type AdjFactor struct {
	TsCode    string  `parquet:"ts_code"`
	TradeDate string  `parquet:"trade_date"`
	AdjFactor float64 `parquet:"adj_factor"`
}

type TushareReq struct {
	APIName string                 `json:"api_name"`
	Token   string                 `json:"token"`
	Params  map[string]interface{} `json:"params"`
	Fields  string                 `json:"fields"`
}

type TushareResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Fields []string        `json:"fields"`
		Items  [][]interface{} `json:"items"`
	} `json:"data"`
}

type DailyBasic struct {
	TsCode       string  `parquet:"ts_code"`
	TradeDate    string  `parquet:"trade_date"`
	Pe           float64 `parquet:"pe"`
	PeTTM        float64 `parquet:"pe_ttm"`
	Pb           float64 `parquet:"pb"`
	Ps           float64 `parquet:"ps"`
	PsTTM        float64 `parquet:"ps_ttm"`
	DvRatio      float64 `parquet:"dv_ratio"`
	DvTTM        float64 `parquet:"dv_ttm"`
	TotalMv      float64 `parquet:"total_mv"`
	CircMv       float64 `parquet:"circ_mv"`
	TotalShare   float64 `parquet:"total_share"`
	TurnoverRate float64 `parquet:"turnover_rate"`
	VolumeRatio  float64 `parquet:"volume_ratio"`
}

type FinaIndicator struct {
	TsCode            string  `parquet:"ts_code"`
	AnnDate           string  `parquet:"ann_date"`
	EndDate           string  `parquet:"end_date"`
	Roe               float64 `parquet:"roe"`
	Roa               float64 `parquet:"roa"`
	GrossProfitMargin float64 `parquet:"grossprofit_margin"`
	NetProfitMargin   float64 `parquet:"netprofit_margin"`
	ProfitDedt        float64 `parquet:"profit_dedt"`
	BasicEps          float64 `parquet:"basic_eps"`
	NIncomeYoY        float64 `parquet:"dt_netprofit_yoy"`
	RevenueYoY        float64 `parquet:"total_revenue_ratio"`
}

type Income struct {
	TsCode       string  `parquet:"ts_code"`
	AnnDate      string  `parquet:"ann_date"`
	EndDate      string  `parquet:"end_date"`
	TotalRevenue float64 `parquet:"total_revenue"`
	Revenue      float64 `parquet:"revenue"`
	NIncome      float64 `parquet:"n_income"`
	ReportType   string  `parquet:"report_type"`
}

type HsConst struct {
	TsCode  string `parquet:"ts_code"`
	HsType  string `parquet:"hs_type"`
	InDate  string `parquet:"in_date"`
	OutDate string `parquet:"out_date"`
	IsNew   string `parquet:"is_new"`
}

type TradeCal struct {
	Exchange     string `parquet:"exchange"`
	CalDate      string `parquet:"cal_date"`
	IsOpen       int    `parquet:"is_open"`
	PretradeDate string `parquet:"pretrade_date"`
}

type IndexBar struct {
	TsCode    string  `parquet:"ts_code"`
	TradeDate string  `parquet:"trade_date"`
	Close     float64 `parquet:"close"`
	Open      float64 `parquet:"open"`
	High      float64 `parquet:"high"`
	Low       float64 `parquet:"low"`
	Vol       float64 `parquet:"vol"`
	Amount    float64 `parquet:"amount"`
}

type StkLimit struct {
	TradeDate string  `parquet:"trade_date"`
	TsCode    string  `parquet:"ts_code"`
	PreClose  float64 `parquet:"pre_close"`
	UpLimit   float64 `parquet:"up_limit"`
	DownLimit float64 `parquet:"down_limit"`
}

type Moneyflow struct {
	TsCode        string  `parquet:"ts_code"`
	TradeDate     string  `parquet:"trade_date"`
	BuySmVol      float64 `parquet:"buy_sm_vol"`
	BuySmAmount   float64 `parquet:"buy_sm_amount"`
	SellSmVol     float64 `parquet:"sell_sm_vol"`
	SellSmAmount  float64 `parquet:"sell_sm_amount"`
	BuyMdVol      float64 `parquet:"buy_md_vol"`
	BuyMdAmount   float64 `parquet:"buy_md_amount"`
	SellMdVol     float64 `parquet:"sell_md_vol"`
	SellMdAmount  float64 `parquet:"sell_md_amount"`
	BuyLgVol      float64 `parquet:"buy_lg_vol"`
	BuyLgAmount   float64 `parquet:"buy_lg_amount"`
	SellLgVol     float64 `parquet:"sell_lg_vol"`
	SellLgAmount  float64 `parquet:"sell_lg_amount"`
	BuyElgVol     float64 `parquet:"buy_elg_vol"`
	BuyElgAmount  float64 `parquet:"buy_elg_amount"`
	SellElgVol    float64 `parquet:"sell_elg_vol"`
	SellElgAmount float64 `parquet:"sell_elg_amount"`
	NetMfVol      float64 `parquet:"net_mf_vol"`
	NetMfAmount   float64 `parquet:"net_mf_amount"`
	TradeCount    float64 `parquet:"trade_count"`
}

type SectorDaily struct {
	TradeDate      string  `parquet:"trade_date"`
	SectorType     string  `parquet:"sector_type"`
	SectorCode     string  `parquet:"sector_code"`
	SectorName     string  `parquet:"sector_name"`
	Source         string  `parquet:"source"`
	MemberCount    int     `parquet:"member_count"`
	RisingCount    int     `parquet:"rising_count"`
	FallingCount   int     `parquet:"falling_count"`
	FlatCount      int     `parquet:"flat_count"`
	Chg1           float64 `parquet:"chg1"`
	Chg5           float64 `parquet:"chg5"`
	Chg20          float64 `parquet:"chg20"`
	Breadth        float64 `parquet:"breadth"`
	AboveMA20Pct   float64 `parquet:"above_ma20_pct"`
	LimitUpCount   int     `parquet:"limit_up_count"`
	LimitDownCount int     `parquet:"limit_down_count"`
	Amount         float64 `parquet:"amount"`
	AmountRatio20  float64 `parquet:"amount_ratio20"`
	NetMoneyflow   float64 `parquet:"net_moneyflow"`
	LargeNetFlow   float64 `parquet:"large_net_flow"`
	LeaderCodes    string  `parquet:"leader_codes"`
	Tags           string  `parquet:"tags"`
	UpdatedAt      string  `parquet:"updated_at"`
}

func (b DailyBar) AdjustedPrice() float64 {
	return b.Close
}

func (b DailyBar) HasRawPrices() bool {
	return b.RawOpen > 0 && b.RawHigh > 0 && b.RawLow > 0 && b.RawClose > 0 && b.AdjFactor > 0
}

func (b DailyBar) TradeOpen() float64 {
	if b.RawOpen > 0 {
		return b.RawOpen
	}
	return b.Open
}

func (b DailyBar) TradeHigh() float64 {
	if b.RawHigh > 0 {
		return b.RawHigh
	}
	return b.High
}

func (b DailyBar) TradeLow() float64 {
	if b.RawLow > 0 {
		return b.RawLow
	}
	return b.Low
}

func (b DailyBar) TradeClose() float64 {
	if b.RawClose > 0 {
		return b.RawClose
	}
	return b.Close
}

func (b DailyBar) HasLimitPrices() bool {
	return b.UpLimit > 0 && b.DownLimit > 0
}

func (b DailyBar) IsLimitUpPrice(price float64) bool {
	return b.UpLimit > 0 && price >= b.UpLimit*0.999
}

func (b DailyBar) IsLimitDownPrice(price float64) bool {
	return b.DownLimit > 0 && price <= b.DownLimit*1.001
}

func (b DailyBar) IsLimitUpClose() bool {
	return b.IsLimitUpPrice(b.TradeClose())
}

func (b DailyBar) IsLimitDownClose() bool {
	return b.IsLimitDownPrice(b.TradeClose())
}

func (b DailyBar) IsLimitUpOpen() bool {
	return b.IsLimitUpPrice(b.TradeOpen())
}

func (b DailyBar) IsLimitDownOpen() bool {
	return b.IsLimitDownPrice(b.TradeOpen())
}

type NewsItem struct {
	Datetime string `parquet:"datetime"`
	Content  string `parquet:"content"`
	Title    string `parquet:"title"`
	Source   string `parquet:"source"`
}
