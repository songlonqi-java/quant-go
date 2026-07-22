# go-quant Agent 指南

## 项目概述

`go-quant` 是个人 A 股量化工具，用 Go 编写，提供三大核心能力：

1. **数据拉取** — 从 Tushare 拉取日线行情，存储为 Parquet 格式
2. **策略回测** — 9 种量化策略的历史回测，含完整绩效报告（Sharpe/回撤/胜率）
3. **信号生成** — 多策略聚合打分，输出今日买入/卖出建议

**数据源限制：** Tushare 免费 API 仅提供**日线收盘后数据**（~15:30 后发布），无盘中实时数据。所有策略均为日线级别。

---

## 仓库结构

```
cmd/go-quant/main.go            # Cobra CLI 入口，4 个子命令
internal/
  config/config.go              # YAML 配置 + 环境变量覆盖
  data/
    types.go                    # 共享类型：DailyBar, StockInfo, DailyBasic, AdjFactor
    client.go                   # HTTP 客户端：30s 超时 + 指数退避重试 + 限速
    tushare.go                  # Tushare API 封装：stock_basic, daily, adj_factor, daily_basic
    fetcher.go                  # 数据拉取编排：全量历史 / 今日 / 指定日期 / 日期范围
    storage.go                  # Parquet 原子读写 + SQLite DDL + 分组工具
  strategy/
    interface.go                # Strategy 接口 + SignalType 枚举 + ScoreStrategy
    registry.go                 # 策略注册表（DefaultRegistry 预注册全部 9 个）
    ma_crossover.go             # 双均线交叉 MA(5,20)
    macd.go                     # MACD(12,26,9)
    rsi.go                      # RSI(14,30,70)
    bollinger.go                # 布林带(20,2.0)
    volume_breakout.go          # 放量突破(20,1.5)
    value_ma60.go               # 价值回归+均线过滤(60,20,2.0)
    etf_rotation.go             # ETF 动量+估值轮动(20,5,10,2.0%)
    dividend_deviation.go       # 高股息偏离度(600,0.8,1.2)
    bull_flag.go                # 强势龙头回踩(10,20,0.5)
  backtest/
    engine.go                   # 回测引擎：含佣金、滑点建模
    metrics.go                  # 绩效指标：Sharpe/MaxDD/Calmar/胜率/盈亏比
  signal/
    generator.go                # 信号生成：多策略并发打分 → 聚合排名
    reporter.go                 # 输出格式化：table(终端表格) / CSV / JSON
config.example.yaml             # 配置模板（复制为 config.yaml 使用）
go.mod / go.sum                 # Go 模块依赖
```

### 包依赖关系

```
cmd/go-quant → internal/{config, data, strategy, backtest, signal}
                     │
internal/config  (独立，无内部依赖)
internal/data    (独立，无内部依赖)
internal/strategy ← internal/data
internal/backtest ← internal/data, internal/strategy
internal/signal   ← internal/data, internal/strategy
```

---

## 快速开始

### 1. 配置

```bash
cp config.example.yaml config.yaml
# 编辑 config.yaml，填入 Tushare Token
# 或者设置环境变量（优先级高于配置文件）：
export QUANT_TUSHARE_TOKEN="your_token_here"
```

### 2. 拉取数据

```bash
go build -o go-quant ./cmd/go-quant/

# 全量历史数据（按 config.yaml 的年份范围）
./go-quant fetch

# 仅今日数据（收盘后 ~15:30 才能拉到）
./go-quant fetch --today
```

### 3. 生成信号 / 回测

```bash
./go-quant signal                    # 今日买卖建议
./go-quant backtest -s ma_crossover  # 双均线回测
```

---

## 完整命令参考

### `go-quant [全局参数]`

| 参数 | 说明 |
|------|------|
| `-c, --config <path>` | 配置文件路径，默认 `config.yaml` |

---

### `go-quant fetch` — 数据拉取

| 参数 | 说明 | 示例 |
|------|------|------|
| `(无)` | 按配置年份范围拉取全量历史数据 | `./go-quant fetch` |
| `--today` | 拉取今日收盘数据（~15:30后可用） | `./go-quant fetch --today` |
| `--today --force` | 强制重拉今日数据（覆盖已有文件） | `./go-quant fetch --today --force` |
| `--date YYYYMMDD` | 补拉历史某一天 | `./go-quant fetch --date 20260721` |
| `--start YYYYMMDD --end YYYYMMDD` | 按日期范围拉取 | `./go-quant fetch --start 20260101 --end 20260722` |
| `--start-year N --end-year M` | 按年份范围拉取 | `./go-quant fetch --start-year 2024 --end-year 2026` |

**数据存储规则：**
- 全量按年分文件：`data/raw/daily/2024.parquet`、`2025.parquet` ...
- 今日/指定日期独立文件：`data/raw/daily/today_20260722.parquet`
- 股票列表：`data/raw/daily/stocks.parquet`
- 所有写入先写 `.tmp` 文件，成功后 rename（原子写入）
- `--force` 可以强制覆盖已存在的文件

---

### `go-quant signal` — 生成交易信号

| 参数 | 说明 | 示例 |
|------|------|------|
| `-s, --strategy <name,...>` | 指定策略（逗号分隔），默认全部 | `-s macd,rsi,bull_flag` |
| `-n, --top <N>` | 显示前 N 条信号 | `-n 10` |
| `-f, --format <fmt>` | 输出格式：`table` / `csv` / `json` | `-f json` |

**输出格式说明：**
- `table`（默认）：终端对齐表格，按"买入建议""卖出建议"分组
- `csv`：逗号分隔，含策略详情列
- `json`：结构化 JSON，方便对接飞书/Webhook

**信号聚合逻辑：**
- 每个策略独立判断当前信号（Buy/Sell/Hold）
- Buy +1 分，Sell -1 分，叠加 Score 权重
- 综合评分 > 0 → 买入建议；< 0 → 卖出建议
- 按评分降序排列，输出 Top N

---

### `go-quant backtest` — 策略回测

| 参数 | 说明 | 示例 |
|------|------|------|
| `-s, --strategy <name,...>` | 回测策略（逗号分隔），默认全部 | `-s value_ma60,macd` |
| `--start YYYYMMDD` | 回测起始日期 | `--start 20250101` |
| `--end YYYYMMDD` | 回测结束日期 | `--end 20260722` |
| `--capital <金额>` | 初始资金 | `--capital 200000` |
| `-n, --top <N>` | 只回测前 N 只股票（0=全部） | `-n 100` |

**绩效报告包含指标：**

| 指标 | 说明 |
|------|------|
| 总收益率 | (最终资产 - 本金) / 本金 × 100% |
| 年化收益率 | 按 252 个交易日折算 |
| 最大回撤 | 净值曲线从峰值到谷底的最大跌幅 |
| 年化波动率 | 日收益率标准差年化 |
| 夏普比率 | (年化收益 - 无风险利率) / 年化波动 |
| 卡玛比率 | 年化收益 / 最大回撤 |
| 胜率 | 盈利交易次数 / 总交易次数 |
| 平均盈利/亏损 | 单笔交易的平均盈亏百分比 |
| 盈亏比 | 总盈利 / 总亏损 |

**回测假设：**
- 佣金 0.03%（按 config 可调）
- 滑点 0.01%（买入加价、卖出减价）
- 以当日收盘价成交
- 全仓进出（每次买入用全部可用资金）
- 无风险利率默认 3%

---

### `go-quant list` — 查看策略

列出所有已注册策略及其名称和预热期（Warmup 天数）。

---

## 全部策略详解

### 策略 1-5：技术指标经典策略

| 命令名 | 预热期 | 类型 | 买入信号 | 卖出信号 |
|--------|--------|------|----------|----------|
| `ma_crossover` | 20 天 | 趋势跟踪 | MA5 ↑ 上穿 MA20 ↑ | MA5 ↓ 下穿 MA20 ↓ |
| `macd` | 35 天 | 趋势跟踪 | MACD 柱由负转正（穿零轴） | 柱由正转负 |
| `rsi` | 15 天 | 均值回归 | RSI 从超卖区(<30)回升 | RSI 从超买区(>70)回落 |
| `bollinger` | 20 天 | 均值回归 | 价格从下轨下方反弹突破 | 价格从上轨上方回落跌破 |
| `volume_breakout` | 21 天 | 量价共振 | 放量(>1.5x均值)上涨突破 MA20 | 放量下跌 |

### 策略 6-9：文档定制策略（来自 text.md）

| 命令名 | 预热期 | 对应文档 | 买入信号 | 卖出信号 |
|--------|--------|----------|----------|----------|
| `value_ma60` | 60 天 | 策略1 价值回归+均线 | 股价上穿 MA60（站上生命线） | 触及布林上轨 或 跌破 MA60 |
| `etf_rotation` | 20 天 | 策略2 ETF 双轮驱动 | 20日动量>2% 且 MA5 金叉 MA10 | MA5 死叉 MA10 |
| `dividend_deviation` | 600 天 | 策略3 高股息偏离度 | 价格<3年均价×0.8 且收阳线 | 价格>3年均价×1.2 且长上影线 |
| `bull_flag` | 20 天 | 策略4 龙头回踩 | 缩量(<峰值50%)回踩 MA10 不破 | 跌破 MA10 或 收盘<前日最低 |

**关于基本面数据：**
- `value_ma60` 和 `dividend_deviation` 文档版需要 PE 分位、ROE、股息率等基本面数据
- 当前实现已完成技术信号部分（MA60 交叉、价格偏离度、布林带）
- 基本面筛选（PE 分位 < 30%、ROE > 15%）可后续接入 Tushare `daily_basic` 和 `fina_indicator` API 增强
- 相关 API 方法已预留：`FetchDailyBasic()`、`FetchDailyBasicByDate()`

---

## 配置参考

### config.yaml 完整字段

```yaml
tushare:
  token: ""             # Tushare Token（必填）
  base_url: "http://api.tushare.pro"
  rate_limit_ms: 350    # 每次请求间隔（毫秒）

data:
  raw_dir: "./data/raw"
  meta_dir: "./data/meta"
  db_path: "./data/meta/quant.db"

fetch:
  start_year: 2020
  end_year: 2026
  stock_prefixes: ["60", "00", "001"]  # 股票代码前缀过滤

backtest:
  initial_capital: 100000.0
  commission: 0.0003     # 0.03%
  slippage: 0.0001       # 0.01%
  risk_free_rate: 0.03   # 3%

signal:
  default_strategies:    # signal 命令默认使用的策略
    - ma_crossover
    - macd
    - rsi
  top_n: 20
```

### 环境变量覆盖

所有配置项均可通过环境变量覆盖（优先级：环境变量 > config.yaml > 默认值）：

| 环境变量 | 对应配置 |
|----------|----------|
| `QUANT_TUSHARE_TOKEN` | `tushare.token` |
| `QUANT_BASE_URL` | `tushare.base_url` |
| `QUANT_RATE_LIMIT_MS` | `tushare.rate_limit_ms` |
| `QUANT_DATA_DIR` | `data.raw_dir/meta_dir`（自动推导子目录） |
| `QUANT_INITIAL_CAPITAL` | `backtest.initial_capital` |
| `QUANT_RISK_FREE_RATE` | `backtest.risk_free_rate` |

---

## 典型每日工作流

```bash
# ===== 上午/中午 =====
# 不需要拉数据，直接分析（基于昨日收盘数据）
./go-quant signal -s value_ma60,bull_flag -n 20

# ===== 收盘后（~15:30） =====
# 1. 拉取今日收盘数据
./go-quant fetch --today

# 2. 如果需要补拉当日 PE/PB（可选，用于基本面策略）
#    (需要先实现 daily_basic 数据的拉取管道)

# 3. 更新信号（基于今日最新数据）
./go-quant signal -s value_ma60,bull_flag -n 20 -f table

# 4. 回测检查策略表现（可选，周末/月末做）
./go-quant backtest -s value_ma60 --start 20250101
```

**说明：** Tushare 免费 API 仅提供日线收盘后数据。中午 12 点无法获取当日上午的成交信息。如果需要盘中数据，需接入其他实时行情源。

---

## 架构设计

### Strategy 接口

```go
type Strategy interface {
    Name() string                                    // 策略标识名
    Warmup() int                                     // 所需最少历史天数
    Signal(bars []data.DailyBar, idx int) SignalType  // 返回 Buy/Sell/Hold
}

type ScoreStrategy interface {
    Strategy
    Score(bars []data.DailyBar, idx int) float64      // 信号强度评分
}
```

- 策略是无状态的：`Signal(bars, idx)` 给定历史 bars 和当前 idx，返回信号
- 实现 `ScoreStrategy` 接口的策略会参与信号聚合排名
- 预热期内不产生信号（返回 Hold）

### 如何添加新策略

1. 在 `internal/strategy/` 下创建新文件
2. 定义策略结构体，实现 `Strategy` 接口（至少 `Name`、`Warmup`、`Signal` 三个方法）
3. 可选实现 `ScoreStrategy` 接口的 `Score` 方法
4. 在 `registry.go` 的 `DefaultRegistry()` 中注册

示例（最简策略）：
```go
type AlwaysBuy struct{}

func (a *AlwaysBuy) Name() string { return "always_buy" }
func (a *AlwaysBuy) Warmup() int  { return 1 }
func (a *AlwaysBuy) Signal(bars []data.DailyBar, idx int) SignalType {
    if idx >= 1 && bars[idx].Close > bars[idx-1].Close {
        return Buy
    }
    return Hold
}
```

然后在 `registry.go` 中：
```go
r.Register(&AlwaysBuy{})
```

无需修改 CLI 或其他文件——Registry 会动态发现并支持新策略。

---

## 技术细节

### HTTP 客户端

- **超时**：30 秒
- **重试**：失败后指数退避（1s → 2s → 4s），最多重试配置次数
- **限速**：调用间隔 `rate_limit_ms` 毫秒（默认 350ms，即 ~171 次/分钟，留余量在 Tushare 200 次限制内）
- **Context**：支持外部取消

### 数据存储

- **Parquet**：列式存储，按年份分区。原子写入（先写 `.tmp` → `os.Rename`）
- **SQLite**：元数据管理（`daily_factors` 表含因子字段，`fetch_manifest` 表记录拉取状态）
- **内存**：当前全量加载到内存（~5000 股票 × 1500 天 × 100 字节 ≈ 750MB），未来可改为流式

### 复权计算

使用 Tushare `daily` + `adj_factor` 两个 API：
1. `daily` 获取原始 OHLCV
2. `adj_factor` 获取复权因子
3. `ApplyAdjFactors` 将原始价格 × adj_factor = 前复权价格

### 股票池过滤

`fetch.stock_prefixes` 配置控制哪些股票纳入：
- `["60", "00", "001"]` → 仅沪市主板和深市主板
- `[]` → 全部 A 股
- 策略信号生成时使用本地存储的所有股票数据

---

## 代码规范

- **注释**：只在必要时添加，不添加多余的注释
- **命名**：驼峰命名，缩写全大写（URL、API、MA）
- **错误处理**：所有 error 必须处理或显式忽略
- **包组织**：每个 `internal/` 子包单一职责，禁止循环依赖
- **导入顺序**：标准库 → 第三方 → 内部包

---

## Agent 工作规则

1. **不提交 data/ 下的生成文件**（已在 .gitignore）
2. **不在代码中硬编码 Token**，必须通过 config.yaml 或环境变量
3. **不修改 Strategy 接口**，除非有充分理由（会影响所有策略）
4. **修改 Tushare API 行为前先查最新文档**，字段和限速可能变化
5. **Parquet 写入必须使用原子写入**（临时文件 + rename，参见 `WriteParquetFile`）
6. **新策略文件放在 `internal/strategy/` 下，在 `registry.go` 中注册**
7. **构建前运行 `go vet ./...` 确保无警告**
8. **旧文件** `main.go.bak` 和 `backtest.go.bak` 是上一版单文件实现，仅供参考，不作为源码维护

---

## 已知限制

| 限制 | 说明 |
|------|------|
| 无盘中数据 | Tushare 免费 API 仅日线收盘后数据 |
| 无 ETF 代码库 | `etf_rotation` 策略适用于 ETF，需自行确保数据中是 ETF 代码 |
| 基本面数据未接入 | `daily_basic` API 已封装但未接入拉取管道 |
| 全量内存加载 | data 量大时(~5000只×6年)需 ~750MB 内存 |
| 无并发回测 | 回测按股票串行执行 |
| 幸存者偏差 | `stock_basic` 仅返回当前上市股票 |
| 无分时/分钟数据 | 所有策略均为日线级别 |

---

## Tushare API 参考

| API | 用途 | 关键参数 |
|-----|------|----------|
| `stock_basic` | 股票基础信息 | `list_status=L` |
| `daily` | 日线 OHLCV | `ts_code`, `trade_date`, `start_date`, `end_date` |
| `adj_factor` | 复权因子 | `ts_code`, `trade_date`, `start_date`, `end_date` |
| `daily_basic` | 每日基本面指标 | `ts_code`, `trade_date`，返回 PE/PB/市值/换手率/股息率等 |
| `fina_indicator` | 财务指标 | `ts_code`, `start_date`, `end_date`，返回 ROE/ROA/利润率等 |
| `income` | 利润表 | `ts_code`, `start_date`, `end_date`，返回营收/净利润 |
| `trade_cal` | 交易日历 | `exchange`, `start_date`, `end_date` |
| `hs_const` | 沪深300成分股 | `hs_type=SH\|SZ` |

Tushare 接口源码: `~/github/tushare` — 遇到接口字段、限速规则、参数细节等问题时，优先从该源码项目中查找。

## 依赖清单

| 依赖 | 用途 |
|------|------|
| `github.com/spf13/cobra` | CLI 框架 |
| `gopkg.in/yaml.v3` | YAML 配置解析 |
| `github.com/parquet-go/parquet-go` | Parquet 列式存储读写 |
| `modernc.org/sqlite` | 纯 Go SQLite 驱动（免 CGO） |
