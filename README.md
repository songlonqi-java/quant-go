# quant-go

Go 语言 A 股量化工具 — 数据拉取 · 策略回测 · 信号生成 · 持仓管理

## 功能

- **数据拉取**：Tushare 日线/基本面/财务/指数数据，带限速+重试+每日配额
- **17 个策略**：短线(11) + 中线(5) + 长线(1)，可自由扩展
- **信号生成**：多策略聚合打分 → 买入/卖出排名，输出 table/csv/json
- **回测引擎**：佣金/滑点建模，Sharpe/MaxDD/Calmar/胜率/盈亏比
- **市场概况**：指数趋势 + 市场宽度 + 板块热度
- **新闻热度**：新浪财经免费爬取 + 关键词提取 + 股票匹配
- **持仓管理**：交易流水格式，自动计算持仓盈亏和历史胜率

## 快速开始

```bash
# 1. 配置
cp config.example.yaml config.yaml
# 编辑 config.yaml，填入 Tushare Token

# 2. 编译
go build -o go-quant ./cmd/go-quant/

# 3. 拉取数据
./go-quant fetch                    # 日线行情
./go-quant fetch --daily-basic      # PE/PB/市值
./go-quant fetch --financials       # 财务数据
./go-quant fetch --hs300            # 沪深300
./go-quant fetch --index            # 指数数据

# 4. 查看信号
./go-quant signal -n 20
```

## 命令

| 命令 | 说明 |
|------|------|
| `./go-quant fetch` | 拉取日线/基本面/财务/指数数据 |
| `./go-quant signal` | 生成买卖信号（市场+新闻+持仓+策略） |
| `./go-quant backtest` | 策略历史回测 |
| `./go-quant list` | 查看所有策略 |

详见 [docs/commands.md](docs/commands.md)

## 策略

| 类型 | 策略 | 说明 |
|------|------|------|
| 短线 | `limit_up` `sar` `kdj` `roc` `williams_r` `rsi` `mfi` `bull_flag` `bollinger` `donchian` `volume_breakout` | 1-21 天周期 |
| 中线 | `ma_crossover` `etf_rotation` `macd` `ma_sticky` `value_ma60` | 20-60 天周期 |
| 长线 | `dividend_deviation` | 600 天周期 |

详见 [docs/strategies.md](docs/strategies.md)

## 持仓管理

编辑 `portfolio.yaml`（交易流水格式）：

```yaml
transactions:
  - date: "20240315"
    code: "600489.SH"
    action: buy
    shares: 100
    price: 22.501
    comment: "黄金板块启动"
```

运行 `./go-quant signal` 自动显示持仓盈亏和已平仓历史统计。
详见 [docs/portfolio.md](docs/portfolio.md)

## 项目结构

```
quant-go/
├── cmd/go-quant/main.go        # CLI 入口
├── internal/
│   ├── config/                 # 配置管理
│   ├── data/                   # Tushare API + Parquet 存储
│   ├── strategy/               # 17 个策略实现
│   ├── backtest/               # 回测引擎 + 绩效指标
│   ├── signal/                 # 多策略信号聚合
│   ├── market/                 # 市场情绪分析
│   ├── news/                   # 新闻热度（新浪免费）
│   └── portfolio/              # 持仓管理
├── docs/                       # 文档
├── config.example.yaml         # 配置模板
└── portfolio.yaml              # 个人持仓（gitignore）
```

## 文档

| 文档 | 内容 |
|------|------|
| [docs/commands.md](docs/commands.md) | 完整 CLI 命令参考 |
| [docs/strategies.md](docs/strategies.md) | 全部策略详解 |
| [docs/portfolio.md](docs/portfolio.md) | 持仓管理流程 |
| [docs/prompts.md](docs/prompts.md) | AI 提示语速查 |
| [agent.md](agent.md) | Agent 开发指南 |
