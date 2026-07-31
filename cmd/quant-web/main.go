package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"quant/internal/config"
	"quant/internal/web"
)

func main() {
	var configPath string
	var address string
	var databasePath string
	var portfolioPath string
	flag.StringVar(&configPath, "config", "config.yaml", "配置文件路径")
	flag.StringVar(&configPath, "c", "config.yaml", "配置文件路径")
	flag.StringVar(&address, "addr", "127.0.0.1:8080", "监听地址；第一期只建议使用回环地址")
	flag.StringVar(&databasePath, "db", "", "Web 任务数据库路径（默认 data/meta/web.db）")
	flag.StringVar(&portfolioPath, "portfolio", "portfolio.yaml", "持仓流水路径")
	flag.Parse()

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "加载配置失败:", err)
		os.Exit(1)
	}
	app, err := web.New(web.Options{
		Config:        cfg,
		DatabasePath:  databasePath,
		PortfolioPath: portfolioPath,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "启动 Web 服务失败:", err)
		os.Exit(1)
	}
	defer app.Close()

	server := &http.Server{
		Addr:              address,
		Handler:           app,
		ReadHeaderTimeout: 10 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		fmt.Printf("go-quant Web 已启动：http://%s（仅本机访问）\n", address)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(os.Stderr, "Web 服务异常:", err)
			stop()
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintln(os.Stderr, "Web 服务关闭失败:", err)
	}
}
