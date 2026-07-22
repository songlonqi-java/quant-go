package data

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

func (c *Client) FetchStockList(ctx context.Context) ([]StockInfo, error) {
	fields := "ts_code,symbol,name,market,list_date,delist_date"
	params := map[string]interface{}{
		"list_status": "L",
	}
	fieldList, items, err := c.CallOnce(ctx, "stock_basic", params, fields)
	if err != nil {
		return nil, err
	}

	idx := indexMap(fieldList)
	var stocks []StockInfo
	for _, item := range items {
		stocks = append(stocks, StockInfo{
			TsCode:     getStr(item, idx, "ts_code"),
			Symbol:     getStr(item, idx, "symbol"),
			Name:       getStr(item, idx, "name"),
			Market:     getStr(item, idx, "market"),
			ListDate:   getStr(item, idx, "list_date"),
			DelistDate: getStr(item, idx, "delist_date"),
		})
	}
	return stocks, nil
}

func (c *Client) FetchDaily(ctx context.Context, tsCode, startDate, endDate string) ([]DailyBar, error) {
	fields := "ts_code,trade_date,open,high,low,close,vol,amount"
	params := map[string]interface{}{
		"ts_code":    tsCode,
		"start_date": startDate,
		"end_date":   endDate,
	}
	fieldList, items, err := c.CallOnce(ctx, "daily", params, fields)
	if err != nil {
		return nil, err
	}

	idx := indexMap(fieldList)
	var bars []DailyBar
	for _, item := range items {
		bars = append(bars, DailyBar{
			TsCode:    getStr(item, idx, "ts_code"),
			TradeDate: getStr(item, idx, "trade_date"),
			Open:      getFloat(item, idx, "open"),
			High:      getFloat(item, idx, "high"),
			Low:       getFloat(item, idx, "low"),
			Close:     getFloat(item, idx, "close"),
			Vol:       getFloat(item, idx, "vol"),
			Amount:    getFloat(item, idx, "amount"),
		})
	}
	return bars, nil
}

func (c *Client) FetchAdjFactors(ctx context.Context, tsCode, startDate, endDate string) ([]AdjFactor, error) {
	fields := "ts_code,trade_date,adj_factor"
	params := map[string]interface{}{
		"ts_code":    tsCode,
		"start_date": startDate,
		"end_date":   endDate,
	}
	fieldList, items, err := c.CallOnce(ctx, "adj_factor", params, fields)
	if err != nil {
		return nil, err
	}

	idx := indexMap(fieldList)
	var factors []AdjFactor
	for _, item := range items {
		factors = append(factors, AdjFactor{
			TsCode:    getStr(item, idx, "ts_code"),
			TradeDate: getStr(item, idx, "trade_date"),
			AdjFactor: getFloat(item, idx, "adj_factor"),
		})
	}
	return factors, nil
}

func (c *Client) FetchAllDailyByDate(ctx context.Context, tradeDate string) ([]DailyBar, error) {
	fields := "ts_code,trade_date,open,high,low,close,vol,amount"
	params := map[string]interface{}{
		"trade_date": tradeDate,
	}
	fieldList, items, err := c.CallOnce(ctx, "daily", params, fields)
	if err != nil {
		return nil, err
	}

	idx := indexMap(fieldList)
	var bars []DailyBar
	for _, item := range items {
		bars = append(bars, DailyBar{
			TsCode:    getStr(item, idx, "ts_code"),
			TradeDate: getStr(item, idx, "trade_date"),
			Open:      getFloat(item, idx, "open"),
			High:      getFloat(item, idx, "high"),
			Low:       getFloat(item, idx, "low"),
			Close:     getFloat(item, idx, "close"),
			Vol:       getFloat(item, idx, "vol"),
			Amount:    getFloat(item, idx, "amount"),
		})
	}
	return bars, nil
}

func (c *Client) FetchAdjFactorsByDate(ctx context.Context, tradeDate string) ([]AdjFactor, error) {
	fields := "ts_code,trade_date,adj_factor"
	params := map[string]interface{}{
		"trade_date": tradeDate,
	}
	fieldList, items, err := c.CallOnce(ctx, "adj_factor", params, fields)
	if err != nil {
		return nil, err
	}

	idx := indexMap(fieldList)
	var factors []AdjFactor
	for _, item := range items {
		factors = append(factors, AdjFactor{
			TsCode:    getStr(item, idx, "ts_code"),
			TradeDate: getStr(item, idx, "trade_date"),
			AdjFactor: getFloat(item, idx, "adj_factor"),
		})
	}
	return factors, nil
}

func ApplyAdjFactors(bars []DailyBar, factors []AdjFactor) []DailyBar {
	fm := make(map[string]float64)
	for _, f := range factors {
		fm[f.TsCode] = f.AdjFactor
	}
	result := make([]DailyBar, len(bars))
	for i, b := range bars {
		result[i] = b
		if adj, ok := fm[b.TsCode]; ok && adj > 0 {
			result[i].Open = b.Open * adj
			result[i].High = b.High * adj
			result[i].Low = b.Low * adj
			result[i].Close = b.Close * adj
		}
	}
	return result
}

func FilterStocksByPrefix(stocks []StockInfo, prefixes []string) []StockInfo {
	if len(prefixes) == 0 {
		return stocks
	}
	var result []StockInfo
	for _, s := range stocks {
		for _, p := range prefixes {
			if strings.HasPrefix(s.TsCode, p) {
				result = append(result, s)
				break
			}
		}
	}
	return result
}

func (c *Client) FetchDailyBasic(ctx context.Context, tsCode, startDate, endDate string) ([]DailyBasic, error) {
	fields := "ts_code,trade_date,pe,pe_ttm,pb,ps,ps_ttm,dv_ratio,dv_ttm,total_mv,circ_mv,total_share,turnover_rate,volume_ratio"
	params := map[string]interface{}{
		"ts_code":    tsCode,
		"start_date": startDate,
		"end_date":   endDate,
	}
	fieldList, items, err := c.CallOnce(ctx, "daily_basic", params, fields)
	if err != nil {
		return nil, err
	}
	idx := indexMap(fieldList)
	var basics []DailyBasic
	for _, item := range items {
		basics = append(basics, DailyBasic{
			TsCode:       getStr(item, idx, "ts_code"),
			TradeDate:    getStr(item, idx, "trade_date"),
			Pe:           getFloat(item, idx, "pe"),
			PeTTM:        getFloat(item, idx, "pe_ttm"),
			Pb:           getFloat(item, idx, "pb"),
			Ps:           getFloat(item, idx, "ps"),
			PsTTM:        getFloat(item, idx, "ps_ttm"),
			DvRatio:      getFloat(item, idx, "dv_ratio"),
			DvTTM:        getFloat(item, idx, "dv_ttm"),
			TotalMv:      getFloat(item, idx, "total_mv"),
			CircMv:       getFloat(item, idx, "circ_mv"),
			TotalShare:   getFloat(item, idx, "total_share"),
			TurnoverRate: getFloat(item, idx, "turnover_rate"),
			VolumeRatio:  getFloat(item, idx, "volume_ratio"),
		})
	}
	return basics, nil
}

func (c *Client) FetchDailyBasicByDate(ctx context.Context, tradeDate string) ([]DailyBasic, error) {
	fields := "ts_code,trade_date,pe,pe_ttm,pb,ps,ps_ttm,dv_ratio,dv_ttm,total_mv,circ_mv,total_share,turnover_rate,volume_ratio"
	params := map[string]interface{}{
		"trade_date": tradeDate,
	}
	fieldList, items, err := c.CallOnce(ctx, "daily_basic", params, fields)
	if err != nil {
		return nil, err
	}
	idx := indexMap(fieldList)
	var basics []DailyBasic
	for _, item := range items {
		basics = append(basics, DailyBasic{
			TsCode:       getStr(item, idx, "ts_code"),
			TradeDate:    getStr(item, idx, "trade_date"),
			Pe:           getFloat(item, idx, "pe"),
			PeTTM:        getFloat(item, idx, "pe_ttm"),
			Pb:           getFloat(item, idx, "pb"),
			Ps:           getFloat(item, idx, "ps"),
			PsTTM:        getFloat(item, idx, "ps_ttm"),
			DvRatio:      getFloat(item, idx, "dv_ratio"),
			DvTTM:        getFloat(item, idx, "dv_ttm"),
			TotalMv:      getFloat(item, idx, "total_mv"),
			CircMv:       getFloat(item, idx, "circ_mv"),
			TotalShare:   getFloat(item, idx, "total_share"),
			TurnoverRate: getFloat(item, idx, "turnover_rate"),
			VolumeRatio:  getFloat(item, idx, "volume_ratio"),
		})
	}
	return basics, nil
}

func (c *Client) FetchFinaIndicator(ctx context.Context, tsCode, startDate, endDate string) ([]FinaIndicator, error) {
	fields := "ts_code,ann_date,end_date,roe,roa,grossprofit_margin,netprofit_margin,profit_dedt,basic_eps,dt_netprofit_yoy,total_revenue_ratio"
	params := map[string]interface{}{
		"ts_code":    tsCode,
		"start_date": startDate,
		"end_date":   endDate,
	}
	fieldList, items, err := c.CallOnce(ctx, "fina_indicator", params, fields)
	if err != nil {
		return nil, err
	}
	idx := indexMap(fieldList)
	var result []FinaIndicator
	for _, item := range items {
		result = append(result, FinaIndicator{
			TsCode:            getStr(item, idx, "ts_code"),
			AnnDate:           getStr(item, idx, "ann_date"),
			EndDate:           getStr(item, idx, "end_date"),
			Roe:               getFloat(item, idx, "roe"),
			Roa:               getFloat(item, idx, "roa"),
			GrossProfitMargin: getFloat(item, idx, "grossprofit_margin"),
			NetProfitMargin:   getFloat(item, idx, "netprofit_margin"),
			ProfitDedt:        getFloat(item, idx, "profit_dedt"),
			BasicEps:          getFloat(item, idx, "basic_eps"),
			NIncomeYoY:        getFloat(item, idx, "dt_netprofit_yoy"),
			RevenueYoY:        getFloat(item, idx, "total_revenue_ratio"),
		})
	}
	return result, nil
}

func (c *Client) FetchIncome(ctx context.Context, tsCode, startDate, endDate string) ([]Income, error) {
	fields := "ts_code,ann_date,end_date,total_revenue,revenue,n_income,report_type"
	params := map[string]interface{}{
		"ts_code":    tsCode,
		"start_date": startDate,
		"end_date":   endDate,
	}
	fieldList, items, err := c.CallOnce(ctx, "income", params, fields)
	if err != nil {
		return nil, err
	}
	idx := indexMap(fieldList)
	var result []Income
	for _, item := range items {
		result = append(result, Income{
			TsCode:       getStr(item, idx, "ts_code"),
			AnnDate:      getStr(item, idx, "ann_date"),
			EndDate:      getStr(item, idx, "end_date"),
			TotalRevenue: getFloat(item, idx, "total_revenue"),
			Revenue:      getFloat(item, idx, "revenue"),
			NIncome:      getFloat(item, idx, "n_income"),
			ReportType:   getStr(item, idx, "report_type"),
		})
	}
	return result, nil
}

func (c *Client) FetchHsConst(ctx context.Context, hsType string) ([]HsConst, error) {
	fields := "ts_code,hs_type,in_date,out_date,is_new"
	params := map[string]interface{}{
		"hs_type": hsType,
	}
	fieldList, items, err := c.CallOnce(ctx, "hs_const", params, fields)
	if err != nil {
		return nil, err
	}
	idx := indexMap(fieldList)
	var result []HsConst
	for _, item := range items {
		result = append(result, HsConst{
			TsCode:  getStr(item, idx, "ts_code"),
			HsType:  getStr(item, idx, "hs_type"),
			InDate:  getStr(item, idx, "in_date"),
			OutDate: getStr(item, idx, "out_date"),
			IsNew:   getStr(item, idx, "is_new"),
		})
	}
	return result, nil
}

func (c *Client) FetchTradeCal(ctx context.Context, exchange, startDate, endDate string) ([]TradeCal, error) {
	fields := "exchange,cal_date,is_open,pretrade_date"
	params := map[string]interface{}{
		"exchange":   exchange,
		"start_date": startDate,
		"end_date":   endDate,
	}
	fieldList, items, err := c.CallOnce(ctx, "trade_cal", params, fields)
	if err != nil {
		return nil, err
	}
	idx := indexMap(fieldList)
	var result []TradeCal
	for _, item := range items {
		result = append(result, TradeCal{
			Exchange:     getStr(item, idx, "exchange"),
			CalDate:      getStr(item, idx, "cal_date"),
			IsOpen:       int(getFloat(item, idx, "is_open")),
			PretradeDate: getStr(item, idx, "pretrade_date"),
		})
	}
	return result, nil
}

func TradingDays(calendars []TradeCal) []string {
	var days []string
	for _, c := range calendars {
		if c.IsOpen == 1 {
			days = append(days, c.CalDate)
		}
	}
	return days
}

func indexMap(fields []string) map[string]int {
	m := make(map[string]int)
	for i, f := range fields {
		m[f] = i
	}
	return m
}

func getStr(item []interface{}, idx map[string]int, key string) string {
	i, ok := idx[key]
	if !ok || i < 0 || i >= len(item) || item[i] == nil {
		return ""
	}
	if v, ok := item[i].(string); ok {
		return v
	}
	return fmt.Sprintf("%v", item[i])
}

func getFloat(item []interface{}, idx map[string]int, key string) float64 {
	i, ok := idx[key]
	if !ok || i < 0 || i >= len(item) || item[i] == nil {
		return 0
	}
	switch v := item[i].(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case string:
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return 0
}
