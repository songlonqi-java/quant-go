# go-quant Agent 指南

A 股量化工具：日线数据拉取 + 17 个策略回测 + 买卖信号 + 持仓管理 + 新闻热度 + 市场情绪。

## 快速开始

```bash
go build -o go-quant ./cmd/go-quant/

# 首次拉取数据（按顺序执行）
./go-quant fetch                # 日线行情
./go-quant fetch --daily-basic  # PE/PB/市值
./go-quant fetch --financials   # 财务数据
./go-quant fetch --hs300        # 沪深300
./go-quant fetch --index        # 指数数据

# 每日使用
./go-quant fetch --today        # 盘后拉今日数据
./go-quant signal -n 20         # 生成买卖信号
./go-quant backtest -s macd     # 回测
```

## 文档索引

| 文档 | 内容 |
|------|------|
| [docs/commands.md](docs/commands.md) | 完整 CLI 命令参考（fetch/signal/backtest/list） |
| [docs/strategies.md](docs/strategies.md) | 全部 17 个策略详解（短线11/中线5/长线1） |
| [docs/portfolio.md](docs/portfolio.md) | 持仓管理：交易流水 → 自动盈亏 + 报表导出 |

## 仓库结构

```
cmd/go-quant/main.go            # Cobra CLI 入口
internal/
  config/config.go              # YAML 配置 + 环境变量覆盖
  data/                         # 数据层：Tushare API / Parquet存储 / 基本面Store
  strategy/                     # 17 个策略（interface → registry → 各实现）
  backtest/                     # 回测引擎 + 绩效指标
  signal/                       # 多策略信号聚合 + 输出格式化
  market/sentiment.go           # 市场情绪：指数/宽度/板块热度
  news/                         # 新闻热度：新浪免费爬取 + 关键词提取 + 股票匹配
  portfolio/portfolio.go        # 持仓管理：交易流水 → 持仓盈亏 + 历史统计
config.example.yaml             # 配置模板
portfolio.yaml                  # 交易流水（个人持仓）
```

## 包依赖

```
cmd → config, data, strategy, backtest, signal, market, news, portfolio
data ← (无内部依赖, 独立)
strategy ← data
backtest ← data, strategy
signal ← data, strategy
market ← data
news ← data
portfolio ← data
```

## 配置

复制 `config.example.yaml` → `config.yaml`，填 Tushare Token。或设置环境变量 `QUANT_TUSHARE_TOKEN`。

关键配置项：
```yaml
tushare:
  rate_limit_ms: 350      # API 调用间隔
  daily_call_limit: 5000  # 每日调用上限

fetch:
  stock_prefixes: ["60", "00", "001"]  # 股票池前缀
  min_market_cap: 100     # 最小市值(亿)

backtest:
  commission: 0.0003      # 佣金 0.03%
  slippage: 0.0001        # 滑点 0.01%
```

环境变量：`QUANT_TUSHARE_TOKEN`, `QUANT_DAILY_CALL_LIMIT`, `QUANT_INITIAL_CAPITAL` 等。

## 数据存储

| 路径 | 内容 |
|------|------|
| `data/raw/daily/*.parquet` | 日线 OHLCV（按年） |
| `data/raw/daily_basic/*.parquet` | PE/PB/市值/股息率 |
| `data/raw/fina/*.parquet` | 财务指标 + 利润表 |
| `data/raw/index/*.parquet` | 上证/深证/创业板指数 + 沪深300成分股 |
| `data/raw/stocks.parquet` | 股票基础信息（含行业分类） |
| `data/raw/reports/` | 持仓报表（每次 signal 自动生成） |

所有 Parquet 写入使用原子操作（.tmp → rename）。

## 关键设计决策

### 策略接口

```go
type Strategy interface {
    Name() string
    Warmup() int
    Signal(bars []data.DailyBar, idx int) SignalType  // Buy/Sell/Hold
}
```

无状态：给定历史 bars + 当前 idx，返回信号。新策略实现该接口并在 `registry.go` 注册即可。

### 信号聚合

多策略并发运行 → 每个策略独立判断 → Buy +1 / Sell -1 → 加权综合评分 → 排名输出。基本面策略可通过 `FundStoreUser` 接口自动注入 PE/ROE/市值数据。

### 市值过滤

拉取流程：`stock_basic` → 前缀过滤 → `daily_basic` 获取最新市值 → 只保留 ≥100 亿的股票 → 所有后续分析只用到这些股票。

### 数据拉取效率

采用按交易日批量拉取（`trade_date` 参数），API 调用量比逐股拉降低 95%。含每日调用计数器和超限保护。

## 已知限制

| 限制 | 说明 |
|------|------|
| 无盘中数据 | Tushare 免费 API 仅日线收盘后数据 |
| 全量内存加载 | 1171 只 × 6 年 ≈ 300MB 内存 |
| 幸存者偏差 | `stock_basic` 仅返回当前上市股票 |
| 板块分类 | 基于 Tushare `industry` 字段（证监会行业分类） |
| 新闻源 | 新浪财经滚动快讯（最近 80 条） |

## Agent 工作规则

1. Token 通过配置文件或环境变量注入，**禁止硬编码**
2. Parquet 写入必须使用原子操作（先写 .tmp 再 rename）
3. 新策略放 `internal/strategy/` 下，在 `registry.go` 注册
4. 构建前运行 `go vet ./...`
5. `config.yaml` 和 `data/` 已加入 `.gitignore`
6. `portfolio.yaml` 是个人交易流水，可提交追踪
7. Tushare 接口源码参考：`~/github/tushare`
8. 遇到 Tushare 接口字段、限速规则等问题时，优先查 `~/github/tushare` 源码
