package backtest

import (
	"math"
	"reflect"
	"sync"
	"testing"

	"quant/internal/data"
	"quant/internal/strategy"
)

func TestPreparedPortfolioEnvironmentMatchesDirectReplayAndIsReusable(t *testing.T) {
	source := map[string][]data.DailyBar{
		"000001.SZ": {
			portfolioBar("000001.SZ", "20260104", 10.4),
			portfolioBar("000001.SZ", "20260101", 10.0),
			portfolioBar("000001.SZ", "20260103", 10.2),
			portfolioBar("000001.SZ", "20260102", 10.1),
		},
	}
	newStrategies := func() []strategy.Strategy {
		return []strategy.Strategy{
			portfolioTestStrategy{name: "limit_up"},
			portfolioTestStrategy{name: "sar"},
			portfolioTestStrategy{name: "kdj"},
		}
	}
	options := func(strategies []strategy.Strategy) PortfolioOptions {
		return PortfolioOptions{
			Strategies: strategies, TopN: 1, Config: Config{InitialCapital: 100_000, LotSize: 100},
			MaxTotalPct: 70, MaxSinglePct: 15, MaxSectorPct: 25,
		}
	}

	directOptions := options(newStrategies())
	directOptions.CodeMap = source
	direct, err := RunPortfolio(directOptions)
	if err != nil {
		t.Fatal(err)
	}
	environment, err := PreparePortfolioEnvironment(source)
	if err != nil {
		t.Fatal(err)
	}
	preparedOptions := options(newStrategies())
	preparedOptions.Environment = environment
	prepared, err := RunPortfolio(preparedOptions)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(prepared, direct) {
		t.Fatalf("prepared replay differs from direct replay:\nprepared=%+v\ndirect=%+v", prepared, direct)
	}

	// The constructor owns a sorted copy: caller mutation after preparation
	// must not change either the environment or a subsequent replay.
	source["000001.SZ"][0].Close = 999
	source["000001.SZ"][0].RawClose = 999
	reusedOptions := options(newStrategies())
	reusedOptions.Environment = environment
	reused, err := RunPortfolio(reusedOptions)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reused, prepared) {
		t.Fatalf("reused environment changed after source mutation:\nreused=%+v\nprepared=%+v", reused, prepared)
	}
}

func TestRunPortfolioRejectsInvalidPreparedEnvironment(t *testing.T) {
	_, err := RunPortfolio(PortfolioOptions{
		Environment: &PortfolioEnvironment{}, Strategies: []strategy.Strategy{portfolioTestStrategy{name: "limit_up"}},
		Config: Config{InitialCapital: 100_000, LotSize: 100},
	})
	if err == nil {
		t.Fatal("expected invalid prepared environment error")
	}
}

func TestPreparedPortfolioEnvironmentSupportsIndependentConcurrentRuns(t *testing.T) {
	bars := []data.DailyBar{
		portfolioBar("000001.SZ", "20260101", 10.0),
		portfolioBar("000001.SZ", "20260102", 10.1),
		portfolioBar("000001.SZ", "20260103", 10.2),
		portfolioBar("000001.SZ", "20260104", 10.4),
	}
	environment, err := PreparePortfolioEnvironment(map[string][]data.DailyBar{"000001.SZ": bars})
	if err != nil {
		t.Fatal(err)
	}
	var results [2]*Result
	var runErrors [2]error
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			results[index], runErrors[index] = RunPortfolio(PortfolioOptions{
				Environment: environment,
				Strategies: []strategy.Strategy{
					portfolioTestStrategy{name: "limit_up"}, portfolioTestStrategy{name: "sar"}, portfolioTestStrategy{name: "kdj"},
				},
				TopN: 1, Config: Config{InitialCapital: 100_000, LotSize: 100},
				MaxTotalPct: 70, MaxSinglePct: 15, MaxSectorPct: 25,
			})
		}(i)
	}
	wg.Wait()
	for _, runErr := range runErrors {
		if runErr != nil {
			t.Fatal(runErr)
		}
	}
	if !reflect.DeepEqual(results[0], results[1]) {
		t.Fatalf("concurrent runs differ:\nfirst=%+v\nsecond=%+v", results[0], results[1])
	}
}

func TestRunPortfolioUsesSharedAccountAggregationAndNextOpen(t *testing.T) {
	bars := []data.DailyBar{
		portfolioBar("000001.SZ", "20260101", 10.0),
		portfolioBar("000001.SZ", "20260102", 10.1),
		portfolioBar("000001.SZ", "20260103", 10.2),
		portfolioBar("000001.SZ", "20260104", 10.4),
	}
	strategies := []strategy.Strategy{
		portfolioTestStrategy{name: "limit_up"},
		portfolioTestStrategy{name: "sar"},
		portfolioTestStrategy{name: "kdj"},
	}
	flows := data.NewMoneyflowStore([]data.Moneyflow{{
		TsCode: "000001.SZ", TradeDate: "20260101", NetMfAmount: 100,
		BuyLgAmount: 100, BuyElgAmount: 100,
	}})

	result, err := RunPortfolio(PortfolioOptions{
		CodeMap:     map[string][]data.DailyBar{"000001.SZ": bars},
		Strategies:  strategies,
		Moneyflows:  flows,
		TopN:        1,
		Config:      Config{InitialCapital: 100_000, LotSize: 100},
		MaxTotalPct: 70, MaxSinglePct: 15, MaxSectorPct: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Trades) != 2 || result.Trades[0].Action != "BUY" || result.Trades[1].Action != "SELL" {
		t.Fatalf("trades = %+v, want next-open buy and sell", result.Trades)
	}
	if result.Trades[0].SignalDate != "20260101" || result.Trades[0].Date != "20260102" {
		t.Fatalf("buy = %+v", result.Trades[0])
	}
	if result.Trades[1].SignalDate != "20260103" || result.Trades[1].Date != "20260104" {
		t.Fatalf("sell = %+v", result.Trades[1])
	}
	if result.FinalEquity <= 100_000 {
		t.Fatalf("FinalEquity = %.2f, want profitable replay", result.FinalEquity)
	}
}

func TestRunPortfolioRecordsAuditableExecutionCosts(t *testing.T) {
	bars := []data.DailyBar{
		portfolioBar("000001.SZ", "20260101", 10.0),
		portfolioBar("000001.SZ", "20260102", 10.1),
		portfolioBar("000001.SZ", "20260103", 10.2),
		portfolioBar("000001.SZ", "20260104", 10.4),
	}
	result, err := RunPortfolio(PortfolioOptions{
		CodeMap: map[string][]data.DailyBar{"000001.SZ": bars},
		Strategies: []strategy.Strategy{
			portfolioTestStrategy{name: "limit_up"}, portfolioTestStrategy{name: "sar"}, portfolioTestStrategy{name: "kdj"},
		},
		Moneyflows: data.NewMoneyflowStore([]data.Moneyflow{{
			TsCode: "000001.SZ", TradeDate: "20260101", NetMfAmount: 100, BuyLgAmount: 100, BuyElgAmount: 100,
		}}),
		TopN: 1, Config: Config{InitialCapital: 100_000, Commission: 0.001, Slippage: 0.002, LotSize: 100},
		MaxTotalPct: 70, MaxSinglePct: 15, MaxSectorPct: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Trades) != 2 {
		t.Fatalf("trades = %+v", result.Trades)
	}
	buy, sell := result.Trades[0], result.Trades[1]
	for _, trade := range result.Trades {
		if trade.RawPrice <= 0 || trade.RawNotional <= 0 || trade.ExecutionNotional <= 0 || trade.CommissionAmount <= 0 || trade.SlippageAmount <= 0 {
			t.Fatalf("incomplete trade ledger: %+v", trade)
		}
		if math.Abs(trade.TotalCostAmount-trade.CommissionAmount-trade.SlippageAmount-trade.ImpactAmount) > 1e-8 {
			t.Fatalf("trade cost does not reconcile: %+v", trade)
		}
	}
	if buy.Price <= buy.RawPrice || sell.Price >= sell.RawPrice {
		t.Fatalf("adverse prices not recorded: buy=%+v sell=%+v", buy, sell)
	}
	if !sell.HasReturn || math.Abs((sell.GrossPnLAmount-sell.NetPnLAmount)-(buy.TotalCostAmount+sell.TotalCostAmount)) > 1e-8 {
		t.Fatalf("closed trade gross/net P&L does not reconcile: buy=%+v sell=%+v", buy, sell)
	}
	attribution := CalculateCostAttribution(result, 100_000)
	if math.Abs(attribution.GrossPnLAmount-attribution.NetPnLAmount-attribution.TotalCostAmount) > 1e-8 {
		t.Fatalf("portfolio attribution does not reconcile: %+v", attribution)
	}
}

func TestRunPortfolioKeepsPreStartBarsForWarmup(t *testing.T) {
	bars := []data.DailyBar{
		portfolioBar("000001.SZ", "20260101", 10.0),
		portfolioBar("000001.SZ", "20260102", 10.1),
		portfolioBar("000001.SZ", "20260103", 10.2),
	}
	strategies := []strategy.Strategy{
		portfolioWarmupStrategy{name: "limit_up"},
		portfolioWarmupStrategy{name: "sar"},
		portfolioWarmupStrategy{name: "kdj"},
	}
	flows := data.NewMoneyflowStore([]data.Moneyflow{{
		TsCode: "000001.SZ", TradeDate: "20260102", NetMfAmount: 100, BuyLgAmount: 100,
	}})
	result, err := RunPortfolio(PortfolioOptions{
		CodeMap: map[string][]data.DailyBar{"000001.SZ": bars}, Strategies: strategies, Moneyflows: flows,
		StartDate: "20260102", TopN: 1, Config: Config{InitialCapital: 100_000, LotSize: 100},
		MaxTotalPct: 70, MaxSinglePct: 15, MaxSectorPct: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Trades) != 1 || result.Trades[0].SignalDate != "20260102" || result.Trades[0].Date != "20260103" {
		t.Fatalf("trades = %+v, want signal to use pre-start warmup and buy next open", result.Trades)
	}
}

func TestRunPortfolioManagedExitRecordsLimitDownDelay(t *testing.T) {
	bars := []data.DailyBar{
		portfolioBar("000001.SZ", "20260101", 10),
		portfolioBar("000001.SZ", "20260102", 10),
		portfolioBar("000001.SZ", "20260103", 10),
		portfolioBar("000001.SZ", "20260104", 10),
		portfolioBar("000001.SZ", "20260105", 10),
		portfolioBar("000001.SZ", "20260106", 10),
		portfolioBar("000001.SZ", "20260107", 9),
		portfolioBar("000001.SZ", "20260108", 8.9),
	}
	bars[6].Open, bars[6].High, bars[6].Low, bars[6].Close = 9, 9, 9, 9
	bars[6].RawOpen, bars[6].RawHigh, bars[6].RawLow, bars[6].RawClose = 9, 9, 9, 9
	bars[6].DownLimit = 9
	strategies := []strategy.Strategy{
		portfolioBuyOnlyStrategy{name: "limit_up"},
		portfolioBuyOnlyStrategy{name: "sar"},
		portfolioBuyOnlyStrategy{name: "kdj"},
	}
	flows := data.NewMoneyflowStore([]data.Moneyflow{{
		TsCode: "000001.SZ", TradeDate: "20260101", NetMfAmount: 100, BuyLgAmount: 100, BuyElgAmount: 100,
	}})
	result, err := RunPortfolio(PortfolioOptions{
		CodeMap: map[string][]data.DailyBar{"000001.SZ": bars}, Strategies: strategies, Moneyflows: flows, TopN: 1,
		Config:      Config{InitialCapital: 100_000, LotSize: 100},
		MaxTotalPct: 70, MaxSinglePct: 15, MaxSectorPct: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Trades) != 2 {
		t.Fatalf("trades = %+v", result.Trades)
	}
	sell := result.Trades[1]
	if sell.Reason != "time_stop" || sell.Date != "20260108" || sell.DelayDays != 1 {
		t.Fatalf("managed sell = %+v", sell)
	}
	if result.DelayedExitTrades != 1 || result.ExitReasonCounts["time_stop"] != 1 {
		t.Fatalf("exit stats = %+v", result)
	}
}

type portfolioTestStrategy struct{ name string }

func (s portfolioTestStrategy) Name() string { return s.name }
func (s portfolioTestStrategy) Warmup() int  { return 0 }
func (s portfolioTestStrategy) Signal(_ []data.DailyBar, idx int) strategy.SignalType {
	switch idx {
	case 0:
		return strategy.Buy
	case 2:
		return strategy.Sell
	default:
		return strategy.Hold
	}
}
func (s portfolioTestStrategy) Score(_ []data.DailyBar, _ int) float64 { return 20 }

type portfolioWarmupStrategy struct{ name string }

func (s portfolioWarmupStrategy) Name() string { return s.name }
func (s portfolioWarmupStrategy) Warmup() int  { return 1 }
func (s portfolioWarmupStrategy) Signal(_ []data.DailyBar, idx int) strategy.SignalType {
	if idx == 1 {
		return strategy.Buy
	}
	return strategy.Hold
}
func (s portfolioWarmupStrategy) Score(_ []data.DailyBar, _ int) float64 { return 20 }

type portfolioBuyOnlyStrategy struct{ name string }

func (s portfolioBuyOnlyStrategy) Name() string { return s.name }
func (s portfolioBuyOnlyStrategy) Warmup() int  { return 0 }
func (s portfolioBuyOnlyStrategy) Signal(_ []data.DailyBar, idx int) strategy.SignalType {
	if idx == 0 {
		return strategy.Buy
	}
	return strategy.Hold
}
func (s portfolioBuyOnlyStrategy) Score(_ []data.DailyBar, _ int) float64 { return 20 }

func portfolioBar(code, date string, price float64) data.DailyBar {
	return data.DailyBar{
		TsCode: code, TradeDate: date, Open: price, High: price + .1, Low: price - .1, Close: price,
		RawOpen: price, RawHigh: price + .1, RawLow: price - .1, RawClose: price, AdjFactor: 1, Vol: 1000,
	}
}
