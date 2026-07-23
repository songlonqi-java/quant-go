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
	TsCode          string  `parquet:"ts_code"`
	AnnDate         string  `parquet:"ann_date"`
	EndDate         string  `parquet:"end_date"`
	Roe             float64 `parquet:"roe"`
	Roa             float64 `parquet:"roa"`
	GrossProfitMargin  float64 `parquet:"grossprofit_margin"`
	NetProfitMargin    float64 `parquet:"netprofit_margin"`
	ProfitDedt         float64 `parquet:"profit_dedt"`
	BasicEps           float64 `parquet:"basic_eps"`
	NIncomeYoY         float64 `parquet:"dt_netprofit_yoy"`
	RevenueYoY         float64 `parquet:"total_revenue_ratio"`
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

func (b DailyBar) AdjustedPrice() float64 {
	return b.Close
}

type NewsItem struct {
	Datetime string `parquet:"datetime"`
	Content  string `parquet:"content"`
	Title    string `parquet:"title"`
	Source   string `parquet:"source"`
}
