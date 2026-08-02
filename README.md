# quant-go

Go 语言 A 股量化工具 — 数据拉取 · 策略回测 · 信号生成 · 持仓管理

## 功能

- **数据拉取**：Tushare 日线/涨跌停价/资金流向/基本面/财务/指数数据，带限速+重试+每日配额
- **日常交易策略**：短线 + 中线趋势、量价、资金与风控信号，可自由扩展
- **慢频价值模块**：月度低估筛选、季度财报复核、行业聚合 PE_TTM/PB 与候选池快照
- **信号生成**：按短线/中线/长线分别聚合打分 → 正式买卖排名 + 观察机会榜，输出 table/csv/json
- **盘中校验**：东方财富实时行情为主、新浪限速降级，覆盖候选股和持仓现价并提示交易可行性风险
- **回测引擎**：佣金/滑点建模，Sharpe/MaxDD/Calmar/胜率/盈亏比
- **历史验证**：全策略链路样本外回放，使用不重叠信号窗口、时间折 embargo 和同日收益聚类，为正式推荐提供胜率、期望收益与权重依据
- **市场概况**：指数趋势 + 市场宽度 + 板块热度
- **新闻热度**：新浪财经免费爬取 + 关键词提取 + 股票匹配
- **持仓管理**：交易流水格式，自动计算持仓盈亏和历史胜率
- **本地 Web 控制台（第一期）**：串行日终任务、执行记录与结构化日报浏览

## 快速开始

```bash
# 1. 配置
cp config.example.yaml config.yaml
# 编辑 config.yaml，填入 Tushare Token

# 2. 编译
go build -o go-quant ./cmd/go-quant/

# 3. 拉取数据
./go-quant fetch                    # 日线行情
./go-quant fetch --stk-limit        # 每日涨跌停价格
./go-quant fetch --moneyflow        # 个股资金流向
./go-quant fetch --daily-basic      # PE/PB/市值/换手率（流动性校验）
./go-quant fetch --financials       # 财务数据
./go-quant fetch --hs300            # 沪深300
./go-quant fetch --index            # 指数数据

# 月度价值筛选（不要放入日终流程）
./go-quant fetch --daily-basic --date 20260731
./go-quant sector build --date 20260731
./go-quant value monthly --date 20260731 -n 20

# 4. 构建历史验证证据（首次和策略/数据更新后执行）
./go-quant validate build

# 可执行链路的共享资金组合回测（默认 backtest 仅做单标的诊断）
./go-quant backtest --ensemble

# 5. 查看信号
./go-quant signal -n 5 --watch 15 # 每个周期买入/卖出各最多5条，额外显示15条观察机会
./go-quant forward validate       # 回填固定观察期及有状态退出的毛收益、成本和净收益
./go-quant forward migrate        # 迁移旧版前向测试CSV

# 盘中查看全市场宽度（均匀分批请求，默认约1分钟完成）
./go-quant market realtime --window 1m

# 本地 Web 控制台（默认仅监听 127.0.0.1）
go build -o quant-web ./cmd/quant-web/
./quant-web -c config.yaml
```

## 命令

| 命令 | 说明 |
|------|------|
| `./go-quant fetch` | 拉取日线/涨跌停价/资金流向/基本面/财务/指数数据 |
| `./go-quant market realtime` | 拉取全市场盘中报价，计算涨跌家数和涨跌停扩散 |
| `./go-quant signal` | 生成短线/中线/长线买卖信号（市场+新闻+持仓+策略） |
| `./go-quant backtest` | 单标的策略诊断；加 `--ensemble` 运行共享资金组合回测 |
| `./go-quant validate build` | 回放完整推荐链路，生成样本外验证证据 |
| `./go-quant value monthly` | 月度生成并持久化价值候选池 |
| `./go-quant value quarterly` | 季度复核价值候选池的基本面和估值回归 |
| `./go-quant forward validate` | 回填固定观察期及有状态退出的毛收益、双边成本和净收益 |
| `./go-quant forward migrate` | 迁移旧版前向测试 CSV schema |
| `./go-quant list` | 查看所有策略 |
| `./quant-web` | 本地页面：启动日终任务并查看报告 |

详见 [docs/commands.md](docs/commands.md)

## 策略

`signal` 输出会按交易周期分成三段：短线看明日到 5 个交易日，中线看 2 到 8 周，长线看数月以上趋势持有。相关指标不会重复按完整票数制造共识：系统展示原始信号数，但使用按策略家族折扣后的有效票和独立组数决定方向、置信度与正式资格。回测和验证统一使用初始止损、ATR 移动止损及短/中/长线 5/20/120 日时间止损，收盘触发后在下个可交易开盘卖出并记录跌停延迟。买入还会统一检查上市天数、ST/停牌、20 日平均成交额、换手率和订单成交占比；回测、历史证据与前向验证按成交占比计入平方根冲击成本。订单规模由 `portfolio.reference_equity × 建议仓位` 估算。风险标签统一显示为硬过滤、置信度/仓位扣分或仅提示，报告含义与真实资格规则一致。信号输出前还会先给出仓位策略：`空仓`、`观望`、`轻仓试错` 或 `正常买入`，避免市场不明确时强行推荐股票。日常信号不使用 PE、PE_TTM、PB、ROE 和分红条件；`daily_basic` 中的换手率只用于流动性校验，估值条件仍由独立的 `value monthly` / `value quarterly` 任务处理。首次使用、策略调整或补齐历史数据后先执行 `validate build`；当本地验证证据存在时，只有达到独立日期簇样本量、跨时间折数和收缩后期望收益门槛的候选，才能成为正式买入。`--watch` 会额外列出观察机会，只作为跟踪池，不等同于正式买入推荐。

| 类型 | 策略 | 说明 |
|------|------|------|
| 短线 | `sar` `kdj` `rsi` `bollinger` `volume_breakout` | 5-21 天周期 |
| 中线 | `ma_crossover` `macd` `relative_strength` `atr_breakout` `trend_pullback` | 20-120 天周期 |
| 长线 | `dividend_deviation` `quality_value` `earnings_growth` | 120-600 天周期 |

详见 [docs/strategies.md](docs/strategies.md)。原 24 个策略的重叠关系、统一诊断结果和 11 个退役项见 [docs/todo/strategy-autopsy.md](docs/todo/strategy-autopsy.md)。保留不代表策略已经被证明有效；新增策略仍需先用组合消融比较净收益、回撤、换手和跨年稳定性。

策略正确性审计和修复优先级见 [docs/todo/strategy-audit.md](docs/todo/strategy-audit.md)。

新的研究主线不再继续叠加技术指标，而是建设可追溯的新闻事件库、AI 结构化事件抽取、板块/个股映射和严格事件回测，详见 [docs/todo/event-driven-strategy.md](docs/todo/event-driven-strategy.md)。

## 数据口径

新拉取的日线数据同时保存真实 OHLC（`raw_*`）和前复权 OHLC。策略计算默认使用前复权价格；回测成交、信号展示价和前向验证使用真实价格。旧版 Parquet 没有 `raw_*` 字段，回测和前向验证会报错；重新执行 `fetch` 可补齐，临时排查可加 `--allow-adjusted-trades`。

`stk_limit` 会提供精确涨跌停价，用于回测、前向验证和涨停策略的不可成交判断；没有该数据时回落到配置的涨跌停百分比近似判断。`moneyflow` 会在 `signal` 输出中标注资金确认、资金背离或资金分歧，用来辅助筛选短线和中线信号。

`signal` 在 A 股连续竞价时段（09:30-11:30、13:00-15:00，Asia/Shanghai）会先用约一分钟分批拉取全市场行情，再复用该快照校验候选股和持仓：默认 `auto` 选择东方财富，只有主源异常或有效报价覆盖不足时才在同一限速规则下退回新浪。展示盘中宽度、实时价、涨跌幅和高开/涨停/走弱等标签。午休、盘后和周末不请求实时源，改用本地/Tushare 日线；实时数据不写入 `data/raw/daily/*.parquet`，也不参与正式历史回测。可用 `--realtime-source eastmoney|sina` 固定来源，或使用 `./go-quant signal --realtime=false` 关闭。若仓位策略判断应空仓，前向测试会写入 `CASH` 记录，用于验证“不买”是否规避了风险。

## 持仓管理

编辑 `portfolio.yaml`（交易流水格式）：

```yaml
transactions:
  - date: "20240315"
    code: "000001.SZ"
    action: buy
    shares: 100
    price: 10.00
    comment: "示例买入"
```

运行 `./go-quant signal` 自动显示持仓盈亏和已平仓历史统计。
详见 [docs/portfolio.md](docs/portfolio.md)

## 项目结构

```
quant-go/
├── cmd/go-quant/main.go        # CLI 入口
├── cmd/quant-web/main.go        # 本地 Web 入口
├── internal/
│   ├── config/                 # 配置管理
│   ├── data/                   # Tushare API + Parquet 存储
│   ├── strategy/               # 13 个策略实现（10 个日常、3 个慢频）
│   ├── backtest/               # 回测引擎 + 绩效指标 + 策略消融
│   ├── signal/                 # 多策略信号聚合
│   ├── validation/             # 样本外推荐资格验证与权重证据
│   ├── market/                 # 市场情绪分析
│   ├── news/                   # 新闻热度（新浪免费）
│   ├── realtime/               # 东方财富主源 + 新浪降级的盘中行情校验
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
| [docs/todo/strategy-autopsy.md](docs/todo/strategy-autopsy.md) | 指标尸检、去重依据与退役清单 |
| [docs/todo/strategy-audit.md](docs/todo/strategy-audit.md) | 策略审计、P0 修复状态与后续路线 |
| [docs/todo/event-driven-strategy.md](docs/todo/event-driven-strategy.md) | 事件驱动量化、AI 职责边界与分阶段实施路线 |
| [docs/portfolio.md](docs/portfolio.md) | 持仓管理流程 |
| [docs/prompts.md](docs/prompts.md) | AI 提示语速查 |
| [docs/web.md](docs/web.md) | 本地 Web 控制台第一期 |
| [agent.md](agent.md) | Agent 开发指南 |
