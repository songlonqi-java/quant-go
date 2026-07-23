# AI 提示语速查

**使用方法**：复制一整条发给 AI，AI 会直接执行命令并返回结果。无需额外解释。

---

## 使用前先复制这条（让 AI 了解项目环境）

```
我在 /home/xxx/A_quant_go 目录下有一个 Go 量化项目，
入口命令是 ./go-quant，配置文件是 config.yaml，
日线数据在 data/raw/daily/*.parquet，
基本面数据在 data/raw/daily_basic/*.parquet，
我的持仓记录在 portfolio.yaml。

请先切换到项目目录：cd /home/xxx/A_quant_go
编译命令：go build -o go-quant ./cmd/go-quant/
```

---

## 数据拉取

```
切换到 /home/xxx/A_quant_go，编译代码，然后拉取今日收盘数据（日线行情）。
如果 Tushare 数据还没发布（15:30 前），提示我稍后再试。
命令: go build -o go-quant ./cmd/go-quant/ && ./go-quant fetch --today
```

```
切换到项目目录，拉取近 5 年完整数据集（2021-2026）：
先拉日线行情，再拉 PE/PB/市值/股息率，再拉财务数据（ROE/利润表），最后拉沪深 300 和指数数据。
分步执行，每步完成后告诉我进度和 API 调用次数。
命令依次是:
./go-quant fetch
./go-quant fetch --daily-basic
./go-quant fetch --financials
./go-quant fetch --hs300
./go-quant fetch --index
```

```
检查 data/raw/daily/ 目录下 2020-2026 各年份的 parquet 文件行数是否完整，
对比交易日历找出缺失的交易日，然后用 fill_gaps 脚本补全缺失数据。
```

---

## 信号分析

```
切换到 /home/xxx/A_quant_go，先编译，然后用全部 13 个策略（默认配置）跑信号分析，
显示前 20 条买入建议。
命令: go build -o go-quant ./cmd/go-quant/ && ./go-quant signal -n 20
注意输出包含：市场概况（上证指数/均线趋势/板块热度）→ 新闻热度 → 我的持仓盈亏 → 买入卖出建议。
分析时重点说明：
1. 当前市场情绪是偏多还是偏空，建议仓位多少
2. Top 5 买入信号中哪些是多策略共振（3 个以上策略同时买入）
3. 如果信号中包含我已持有的股票，提醒是加仓还是减仓
4. 强势板块和弱势板块分别是什么
```

```
分析通威股份（600438.SH），用全部策略看看买入信号有几个、卖出信号有几个。
如果有基本面数据（PE/ROE/市值），也一并分析。
如果当前持仓中有这只，告诉我盈亏状态。
```

```
先跑 ./go-quant fetch --today 拉取今日数据，再跑信号。
如果今日数据还没出来，用昨天数据的跑。
输出前 10 个信号，重点标出多策略共振的。
```

---

## 回测

```
切换到项目目录，对 ma_crossover 策略进行回测：
回测区间 2025-01-01 到最新日期，初始资金 10 万，显示绩效报告。
命令: go build -o go-quant ./cmd/go-quant/ && ./go-quant backtest -s ma_crossover --start 20250101
然后分析：总收益率、最大回撤、夏普比率、胜率分别是多少？这个策略表现如何？
```

```
对我持仓的中金黄金（600489.SH），从 data/raw/daily/ 中提取它的历史数据，
用全部 13 个策略逐一回测，按收益率从高到低排名，
分析哪个策略在这只股票上表现最好，以及为什么。
```

```
回测短线策略组合（sar、roc、kdj、bull_flag、limit_up 五个），
对比中线策略组合（ma_crossover、macd、value_ma60），
看哪个组合在 2024 年表现更好。
```

---

## 持仓管理

```
查看我的持仓盈亏：编译后跑 ./go-quant signal -n 5，
从输出中提取"持仓概览"部分，告诉我每只股票的成本、现价、收益率。
如果有已平仓的交易记录，也列出历史胜率和累计盈亏。
portfolio.yaml 文件在项目根目录，格式是 transactions[] 交易流水。
```

```
帮我记录一笔交易。编辑 portfolio.yaml，在 transactions 列表末尾追加：
- date: "20260725"
  code: "600438.SH"
  action: buy
  shares: 200
  price: 140.50
  comment: "多策略共振 通威股份"
追加后显示当前持仓状态。
```

```
帮我记录一笔卖出。编辑 portfolio.yaml，追加：
- date: "20260728"
  code: "600489.SH"
  action: sell
  shares: 100
  price: 310.00
  comment: "触及布林上轨 止盈"
然后重新跑信号，看看已平仓交易的盈亏统计。
```

```
全面分析我的持仓：每只股票当前策略信号（该买还是该卖？），
对比买入成本，哪些该止盈、哪些该止损、哪些可以加仓。
结合当前市场情绪和板块热度给出操作建议。
```

---

## 日常一键操作

```
完整日终分析流程：
1. cd /home/xxx/A_quant_go && go build -o go-quant ./cmd/go-quant/
2. ./go-quant fetch --today （如果数据还没出就跳过）
3. ./go-quant signal -n 20
4. 分析输出：市场概况 + 新闻热度 + 持仓盈亏 + 交易信号
5. 整理成一段简洁的日报：今日市场判断、推荐买入的股票、我的持仓操作建议
```

```
盘前快速分析（不拉数据，用昨天收盘数据）：
./go-quant signal -n 15
只告诉我：
1. 今天该买什么（Top 5）
2. 我的持仓该不该动
3. 一句话市场判断
```

---

## 个股深度分析

```
分析 601899.SH 紫金矿业：
1. 从 data/raw/daily/ 提取它的历史数据，计算最近 60 日的 MA5/MA10/MA20/MA60 趋势
2. 从 data/raw/daily_basic/ 提取 PE/PB/市值/股息率变化
3. 从 data/raw/fina/ 提取 ROE 变化
4. 用全部 13 个策略跑一遍信号
5. 从新闻热度中找有没有提及紫金矿业的新闻
6. 综合给出：该买、该卖、还是持有？理由是什么？
```

```
对比分析两只黄金股：中金黄金（600489.SH）和紫金矿业（601899.SH），
从技术面（均线趋势、策略信号）、基本面（PE/ROE/市值）、
新闻热度三个方面对比，告诉我哪只更适合当前买入。
```

---

## 策略管理

```
列出当前所有可用策略，按短线/中线/长线分组，显示每个策略的预热期和信号逻辑。
命令: ./go-quant list
然后读 internal/strategy/ 目录下的策略文件，补充说明每个策略的具体参数。
```

```
新增一个策略：日内反转策略（hammer），逻辑如下：
- 买入：前一日收阴线，当日收阳线，且下影线长度 > 实体长度 × 2
- 卖出：前一日收阳线，当日收阴线，且上影线长度 > 实体长度 × 2
- 命名为 "hammer"，预热期 1 天
请在 internal/strategy/ 下创建 hammer.go，实现 Strategy 接口，
注册到 registry.go，更新 config 默认策略列表，
编译测试，成功后告诉我结果。
```

---

## 数据维护

```
检查数据完整性：
1. 用 trade_cal API 获取 2020-2026 全部交易日
2. 读取 data/raw/daily/*.parquet，统计每个年份的行数
3. 对比找出缺失的交易日
4. 用 FetchAllDailyByDate 逐个补全缺失日期
5. 补完后重写年份 parquet 文件
6. 告诉我补齐了多少条数据
```

```
我的 Tushare 额度只剩 2000 次了，帮我统计最近 7 天各 API 分别调用了多少次。
如果额度紧张，建议我接下来优先拉什么数据。
```

---

## 问题排查

```
拉取数据时报 "JSON 解析失败: unexpected end of JSON input"，
帮我检查 client.go 的重试次数是不是 0，并排查 HTTP 超时和响应截断问题。
```

```
回测结果里"总交易次数"显示为 0 或 1，胜率显示 0%，
帮我排查 backtest/engine.go 和 metrics.go 的交易计数和胜率计算逻辑，
看看是不是只买了没卖导致统计不对。
```

```
信号输出中有些日期显示的是 2024 年的旧数据而不是最新交易日的，
检查 signal/generator.go 和 cmd 中的筛选逻辑，
确保只输出最新交易日有数据的股票。
```
