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
| `--today` | 今日收盘数据（~15:30后） | `./go-quant fetch --today` |
| `--today --force` | 强制重拉 | `./go-quant fetch --today --force` |
| `--date YYYYMMDD` | 补拉某一天 | `./go-quant fetch --date 20260721` |
| `--start/--end` | 日期范围 | `./go-quant fetch --start 20260101 --end 20260722` |
| `--start-year/--end-year` | 年份范围 | `./go-quant fetch --start-year 2024` |
| `--daily-basic` | PE/PB/市值/股息率 | `./go-quant fetch --daily-basic` |
| `--financials` | ROE/利润率/利润表 | `./go-quant fetch --financials` |
| `--hs300` | 沪深300成分股 | `./go-quant fetch --hs300` |
| `--index` | 上证/深证/创业板指数 | `./go-quant fetch --index` |

---

## `go-quant signal` — 交易信号

| 参数 | 说明 | 示例 |
|------|------|------|
| `-s, --strategy` | 指定策略（逗号分隔），默认全部 21 个 | `-s macd,rsi,bull_flag` |
| `-n, --top` | 显示前 N 条，实际输出不再额外放大 | `-n 10` |
| `-f, --format` | `table` / `csv` / `json` | `-f json` |

输出四个板块：市场概况 → 新闻热度 → 持仓概览 → 买入/卖出建议

`signal` 会自动把前 5 个买入候选写入 `data/forward_test/`，用于后续前向测试。
如果最近两个交易日缺少真实价字段，`signal` 会跳过 `limit_up` 并给出警告；重新拉取行情后会恢复。

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

回测成交口径：T 日收盘产生信号，T+1 开盘成交，并考虑基础滑点、手续费以及开盘涨跌停不可成交约束。输出包含样本平均/中位收益、正收益股票数、收益区间，以及单只最佳表现。

旧版行情 Parquet 没有 `raw_open/raw_high/raw_low/raw_close/adj_factor` 字段。回测默认拒绝这类数据，避免把前复权价当真实成交价；重新执行 `fetch` 可补齐。

---

## `go-quant forward` — 前向测试

| 参数 | 说明 | 示例 |
|------|------|------|
| `validate` | 用本地行情回填前向测试 1/3/5 日表现 | `./go-quant forward validate` |
| `migrate` | 迁移旧版 `picks.csv` 到当前 schema | `./go-quant forward migrate` |
| `--dir` | 前向测试目录，默认 `data/forward_test` | `./go-quant forward --dir data/forward_test validate` |
| `validate --allow-adjusted-trades` | 允许用复权价近似验证价格，仅用于旧数据临时验证 | `./go-quant forward validate --allow-adjusted-trades` |

前向验证会执行入场失效规则：目标日高开超过 3% 标记为 `no_trade_gap`，盘中跌破前一交易日低点标记为 `invalid_break_prev_low`，这些记录不会继续计算 3/5 日持有收益。

---

## `go-quant list` — 查看策略

列出所有已注册策略及预热期。
