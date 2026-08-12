package main

import (
	"fmt"

	"quant/internal/dataset"
	"quant/internal/news"

	"github.com/spf13/cobra"
)

// newsCmd is the parent command for news fact-base tooling.
func newsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "news",
		Short: "新闻事实库与事件研究工具",
	}
	cmd.AddCommand(newsImpactCmd())
	return cmd
}

// newsImpactCmd measures what happened to stocks after they were mentioned in
// archived news, using next-session-open entry and the equal-weight market
// proxy as the benchmark. It is a rule-based event-study baseline on the road
// to the AI-assisted event pipeline, and can be rerun as the archive grows.
func newsImpactCmd() *cobra.Command {
	var (
		top     int
		workers int
	)
	cmd := &cobra.Command{
		Use:   "impact",
		Short: "新闻提及个股的样本外事件研究（下一交易日开盘成交）",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()
			ds, err := dataset.Load(dataset.LoadOptions{RawDir: cfg.Data.RawDir})
			if err != nil {
				return err
			}
			records, migrated, err := news.LoadArchive(cfg.Data.RawDir)
			if err != nil {
				return fmt.Errorf("加载新闻事实库: %w", err)
			}
			report, err := news.BuildNewsImpact(records, ds.AllCodeMap, ds.StockNames,
				cfg.Backtest.Commission, cfg.Backtest.Slippage)
			if err != nil {
				return err
			}
			if migrated {
				fmt.Println("提示: 使用旧 latest.parquet 迁移的记录，只有发布时间、没有首次获取时间。")
			}
			news.PrintImpact(report, top)
			return nil
		},
	}
	cmd.Flags().IntVar(&top, "top", 15, "个股明细条数")
	cmd.Flags().IntVar(&workers, "workers", 32, "并行工作数（保留兼容）")
	_ = workers
	return cmd
}
