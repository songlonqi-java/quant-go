package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"quant/internal/config"
	"quant/internal/data"
)

func main() {
	code := "002594.SZ"
	if len(os.Args) > 1 {
		code = os.Args[1]
	}

	cfg := config.MustLoad("config.yaml")
	client := data.NewClient(cfg.Tushare.BaseURL, cfg.Tushare.Token, cfg.Tushare.RateLimitMs)
	ctx := context.Background()

	today := time.Now().Format("20060102")
	yesterday := time.Now().AddDate(0, 0, -1).Format("20060102")

	factors, err := client.FetchAdjFactors(ctx, code, yesterday, today)
	if err != nil || len(factors) == 0 {
		fmt.Println("获取复权因子失败，可能是非交易日")
		os.Exit(1)
	}

	latest := factors[len(factors)-1]
	adj := latest.AdjFactor

	if len(os.Args) > 2 {
		price, err := strconv.ParseFloat(os.Args[2], 64)
		if err == nil {
			fmt.Printf("券商价 %.2f → 前复权价 %.2f\n", price, price*adj)
			return
		}
	}

	fmt.Printf("=== %s 复权因子: %.4f (%s) ===\n", code, adj, latest.TradeDate)
	fmt.Printf("券商价 × %.4f = 前复权价\n", adj)
	fmt.Printf("前复权价 ÷ %.4f = 券商价\n", adj)
	fmt.Println()
	for _, raw := range []float64{70, 75, 80, 85, 88, 90, 95, 100} {
		fmt.Printf("  ¥%.2f → %.2f\n", raw, raw*adj)
	}
}
