# 命令参考

## 全局参数

| 参数 | 说明 |
|------|------|
| `-c, --config <path>` | 配置文件路径，默认 `config.yaml` |

---

## `go-quant fetch` — 数据拉取

| 参数 | 说明 | 示例 |
|------|------|------|
| `(无)` | 全量历史行情 | `./go-quant fetch` |
| `--today` | 今日收盘数据（~16:00后） | `./go-quant fetch --today` |
| `--today --force` | 强制重拉 | `./go-quant fetch --today --force` |
| `--date YYYYMMDD` | 补拉某一天 | `./go-quant fetch --date 20260721` |
| `--start/--end` | 日期范围 | `./go-quant fetch --start 20260101 --end 20260722` |
| `--start-year/--end-year` | 年份范围 | `./go-quant fetch --start-year 2024` |
| `--stk-limit` | 每日涨跌停价格 | `./go-quant fetch --stk-limit --start 20260101 --end 20260722` |
| `--moneyflow` | 个股资金流向 | `./go-quant fetch --moneyflow --date 20260722` |
| `--daily-basic` | PE/PB/市值/股息率 | `./go-quant fetch --daily-basic` |
| `--financials` | ROE/利润率/利润表 | `./go-quant fetch --financials` |
| `--hs300` | 沪深300成分股 | `./go-quant fetch --hs300` |
| `--index` | 上证/深证/创业板指数 | `./go-quant fetch --index` |

`--stk-limit` 和 `--moneyflow` 支持 `--today`、`--date`、`--start/--end`、`--start-year/--end-year`，并按年份合并写入，不会因为补拉单日而覆盖整年文件。

---

## `go-quant sector build` — 板块日度聚合

| 参数 | 说明 | 示例 |
|------|------|------|
| `(无)` | 构建最新本地交易日 | `./go-quant sector build` |
| `--today` | 构建最新本地交易日，适合日终流程 | `./go-quant sector build --today` |
| `--date YYYYMMDD` | 重算某一天 | `./go-quant sector build --date 20260727` |
| `--start/--end` | 补算日期范围 | `./go-quant sector build --start 20260701 --end 20260727` |

当前第一阶段使用 `stocks.parquet` 的行业字段做行业板块聚合，输出到 `data/raw/sector_daily/YYYY.parquet`。同一天同一板块会覆盖写入，支持重复运行。

聚合字段包括涨跌幅、上涨家数占比、MA20 上方占比、涨跌停数量、成交额放大倍数、资金净流入、大单净流入、领涨股和异动标签。常见标签包括 `板块放量`、`赚钱效应扩散`、`涨停扩散`、`资金确认`、`资金背离`、`强势延续`、`高位退潮`、`孤立龙头`。

---

## `go-quant signal` — 交易信号

| 参数 | 说明 | 示例 |
|------|------|------|
| `-s, --strategy` | 指定策略（逗号分隔），默认全部 23 个 | `-s macd,rsi,bull_flag` |
| `-n, --top` | 每个周期买入/卖出各最多 N 条 | `-n 5` |
| `--watch` | 额外显示观察机会数量，默认 15，0 表示关闭 | `--watch 15` |
| `-f, --format` | `table` / `csv` / `json` | `-f json` |
| `--realtime` | 是否使用新浪实时行情做盘中校验，默认开启 | `--realtime=false` |

输出五个板块：市场概况 → 新闻热度 → 仓位策略 → 持仓概览 → 短线/中线/长线买卖建议。市场概况会展示赚钱效应、涨跌停数量和 A 股风险标签。

`signal` 会按周期分别聚合策略，表格只展示触发 BUY/SELL 的策略，不展示 HOLD 策略。CSV/JSON 输出包含 `horizon` 字段；如果本地有 `moneyflow` 数据，还会输出资金净额和大单净额，并在风险标签里标注资金确认、资金背离或资金分歧。

`signal` 默认会对候选股和当前持仓请求新浪实时行情，输出实时价、盘中涨跌幅和盘中标签。常见标签包括 `高开>3%`、`涨幅偏高`、`盘中走弱`、`涨停风险`、`跌停风险`、`卖压确认`。实时行情只作为盘中交易可行性校验，不写入历史日线 Parquet，也不参与正式回测。

仓位策略会先判断 `空仓` / `观望` / `轻仓试错` / `正常买入`。当市场偏弱、资金缺少确认、候选股盘中走弱或存在涨停/高开不可成交风险时，买入候选会被降级为观望，避免为了凑满 Top N 强行推荐。

`--watch` 会在正式买卖建议后追加“观察机会”榜。观察榜不等于买入推荐，主要收纳单策略较强、共振不足、排名靠后、存在卖出冲突、被仓位/风控过滤的候选，方便盘中人工跟踪，避免只看正式榜而错过潜在机会。

`signal` 会自动把每个周期前 5 个合格买入候选写入 `data/forward_test/`。短线验证 1/3/5 日，中线验证 10/20/40 日，长线验证 60/120/250 日。如果仓位策略判断应空仓，会写入一条 `CASH` 记录，表示当天不新增买入。
如果最近两个交易日缺少真实价字段，`signal` 会跳过 `limit_up` 并给出警告；重新拉取行情后会恢复。如果本地有 `stk_limit` 数据，涨停策略会优先使用精确涨停价判断。

当使用 `PrintWithWatch` 的 CLI 输出时，JSON 会返回 `{ "signals": [...], "watchlist": [...] }`，CSV 会增加 `类别` 和 `观察原因` 字段，用于区分正式信号和观察机会。

---

## `go-quant backtest` — 策略回测

| 参数 | 说明 | 示例 |
|------|------|------|
| `-s, --strategy` | 回测策略 | `-s value_ma60,macd` |
| `--start / --end` | 日期范围 | `--start 20250101` |
| `--capital` | 初始资金 | `--capital 200000` |
| `-n, --top` | 按代码排序后的前 N 只股票（0=全部） | `-n 100` |
| `--allow-adjusted-trades` | 允许用复权价近似成交价，仅用于旧数据临时验证 | `--allow-adjusted-trades` |

绩效指标：总收益、年化收益、最大回撤、夏普比率、卡玛比率、胜率、盈亏比

回测成交口径：T 日收盘产生信号，T+1 开盘成交，并考虑基础滑点、手续费以及开盘涨跌停不可成交约束。输出包含样本平均/中位收益、正收益股票数、收益区间，以及单只最佳表现。如果本地有 `stk_limit` 数据，会优先使用精确涨跌停价；否则使用 `backtest.limit_pct` 近似判断。

旧版行情 Parquet 没有 `raw_open/raw_high/raw_low/raw_close/adj_factor` 字段。回测默认拒绝这类数据，避免把前复权价当真实成交价；重新执行 `fetch` 可补齐。

---

## `go-quant forward` — 前向测试

| 参数 | 说明 | 示例 |
|------|------|------|
| `validate` | 用本地行情回填短/中/长线前向测试表现 | `./go-quant forward validate` |
| `migrate` | 迁移旧版 `picks.csv` 到当前 schema | `./go-quant forward migrate` |
| `--dir` | 前向测试目录，默认 `data/forward_test` | `./go-quant forward --dir data/forward_test validate` |
| `validate --allow-adjusted-trades` | 允许用复权价近似验证价格，仅用于旧数据临时验证 | `./go-quant forward validate --allow-adjusted-trades` |

前向验证会执行入场失效规则：目标日高开超过 3% 标记为 `no_trade_gap`，盘中跌破前一交易日低点标记为 `invalid_break_prev_low`，这些记录不会继续计算后续持有收益。

`picks.csv` 包含 `horizon` 字段；短线回填 `day3/day5`，中线回填 `day10/day20/day40`，长线回填 `day60/day120/day250`。

---

## `go-quant list` — 查看策略

列出所有已注册策略及预热期。
