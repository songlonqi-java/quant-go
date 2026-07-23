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
| `-s, --strategy` | 指定策略（逗号分隔），默认全部 17 个 | `-s macd,rsi,bull_flag` |
| `-n, --top` | 显示前 N 条 | `-n 10` |
| `-f, --format` | `table` / `csv` / `json` | `-f json` |

输出四个板块：市场概况 → 新闻热度 → 持仓概览 → 买入/卖出建议

---

## `go-quant backtest` — 策略回测

| 参数 | 说明 | 示例 |
|------|------|------|
| `-s, --strategy` | 回测策略 | `-s value_ma60,macd` |
| `--start / --end` | 日期范围 | `--start 20250101` |
| `--capital` | 初始资金 | `--capital 200000` |
| `-n, --top` | 前 N 只股票（0=全部） | `-n 100` |

绩效指标：总收益、年化收益、最大回撤、夏普比率、卡玛比率、胜率、盈亏比

---

## `go-quant list` — 查看策略

列出所有已注册策略及预热期。
