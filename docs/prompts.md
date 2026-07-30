# AI 提示语速查

**使用方法**：复制一整条发给 AI，AI 会直接执行命令并返回结果。无需额外解释。

---

## 使用前先复制这条（让 AI 了解项目环境）

```
我在 /home/xxx/quant-go 目录下有一个 Go 量化项目，
入口命令是 ./go-quant，配置文件是 config.yaml，持仓记录在 portfolio.yaml。

数据目录：
  data/raw/daily/*.parquet        日线行情（复权+真实成交价）
  data/raw/stk_limit/*.parquet    涨跌停价
  data/raw/moneyflow/*.parquet    个股资金流向
  data/raw/daily_basic/*.parquet  PE/PB/市值/股息率/换手率
  data/raw/fina/*.parquet          财务数据（ROE/利润表）
  data/raw/index/*.parquet         指数行情（上证/深证/沪深300）
  data/raw/news/latest.parquet     新闻热度缓存
  data/raw/reports/                 信号报告导出目录（json/csv）
  data/forward_test/                前向测试记录

关键命令：
  ./go-quant fetch   拉取数据（日线/涨跌停/资金/基本面/财务/指数）
  ./go-quant signal  生成买卖信号（默认请求新浪实时行情做盘中校验）
  ./go-quant backtest   回测策略
  ./go-quant forward validate   验证前向测试收益
  ./go-quant analyze <code>     个股深度分析
  ./go-quant list               列出所有可用策略

请先切换到项目目录：cd /home/xxx/quant-go
编译命令：go build -o go-quant ./cmd/go-quant/
可用 -c 指定配置文件路径，环境变量 QUANT_TUSHARE_TOKEN 可覆盖 Token，
QUANT_DATA_DIR 可覆盖数据目录。
```

---

## 数据拉取

```
切换到 /home/xxx/quant-go，编译代码，然后拉取今日收盘数据（日线行情、涨跌停价、资金流向、上证/深证/创业板指数）。
如果 Tushare 数据还没发布（16:00 前），提示我稍后再试。
命令:
go build -o go-quant ./cmd/go-quant/
./go-quant fetch --today
./go-quant fetch --stk-limit --today
./go-quant fetch --moneyflow --today
./go-quant fetch --index --today
```

```
切换到项目目录，拉取近 5 年完整数据集（默认 2020-2026）：
先拉日线行情，再拉涨跌停价和资金流向，再拉 PE/PB/市值/股息率，再拉财务数据（ROE/利润表），最后拉沪深 300 和指数数据。
分步执行，每步完成后告诉我进度和 API 调用次数。
也可用 --start-year 和 --end-year 指定年份范围，如 --start-year 2024 --end-year 2026。
命令依次是:
./go-quant fetch
./go-quant fetch --stk-limit
./go-quant fetch --moneyflow
./go-quant fetch --daily-basic
./go-quant fetch --financials
./go-quant fetch --hs300
./go-quant fetch --index
```

```
检查 data/raw/daily/ 目录下 2020-2026 各年份的 parquet 文件行数是否完整，
对比 data/raw/meta/trade_cal.parquet 交易日历找出缺失的交易日，
然后用 ./go-quant fetch --date <YYYYMMDD> 逐个补全缺失日期。
补完后用 ./go-quant fetch 重写年份 parquet 文件。
```

```
检查本地数据是否足够支撑今天的信号分析：
1. 日线行情是否已经到最新交易日
2. stk_limit 是否包含最新交易日
3. moneyflow 是否包含最新交易日
4. 新闻缓存 data/raw/news/latest.parquet 是否存在，signal 是否能刷新新闻热度
5. signal 是否能通过新浪实时行情拿到候选股和持仓的盘中价格
只检查和报告，不要修改代码。
```

---

## 信号分析

```
切换到 /home/xxx/quant-go，先编译，然后用默认策略跑信号分析，
按短线/中线/长线分别显示买卖建议。
命令: go build -o go-quant ./cmd/go-quant/ && ./go-quant signal -n 5 --watch 15
注意输出包含：市场概况（上证指数/均线趋势/板块热度）→ 新闻热度 → 我的持仓盈亏 → 短中长买卖建议。
分析时重点说明：
1. 当前市场情绪是偏多还是偏空，建议仓位多少
2. 短线/中线/长线 Top 买入信号中哪些是多策略共振
3. 观察机会榜里是否有单策略很强、排名靠后、共振不足但值得跟踪的票
4. 资金流向是否出现资金确认、资金背离、资金分歧
5. 新浪实时行情是否提示高开、涨幅偏高、涨停风险、盘中走弱或卖压确认
6. 新闻热度里是否有政策、监管、行业、公司或市场级别的大事件
7. 强势板块和弱势板块是否与新闻/政策互相印证
8. 如果信号中包含我已持有的股票，提醒是加仓、减仓、止盈、止损还是继续持有
9. 明日开盘或盘中是否存在涨停买不进、跌停卖不出的风险
输出时请明确区分：正式买入推荐、观察机会、量化信号、资金流向、盘中实时价格、新闻/政策催化、风险提示、最终建议。
```

```
分析目标股票（把 000001.SZ 替换为实际代码），用全部策略看看买入信号有几个、卖出信号有几个。
如果有基本面数据（PE/ROE/市值），也一并分析。
如果当前持仓中有这只，告诉我盈亏状态。
```

```
先跑 ./go-quant fetch --today、./go-quant fetch --stk-limit --today、./go-quant fetch --moneyflow --today、./go-quant fetch --index --today 拉取今日数据，再跑信号。
如果今日数据还没出来，用昨天数据的跑。
输出前 10 个正式信号，并额外查看观察机会榜，重点标出多策略共振、资金确认/背离、新闻或政策催化。
如果新闻和量化信号冲突，请明确降级或标注风险，不要强行推荐。
```

```
最新数据已经拉取到了，请在 /home/xxx/quant-go 编译并运行：
./go-quant signal -n 5 --watch 15

请按短线、中线、长线分别输出买入和卖出/回避信号。
分析时优先关注：
1. 多策略共振
2. 观察机会中是否有单策略强但暂未通过正式推荐的候选
3. 资金确认/背离/分歧
4. 新浪实时行情显示的盘中价格、涨跌幅和交易可行性标签
5. 新闻热度和政策/行业/公司重大事件
6. 涨跌停不可成交风险
7. 我的持仓操作建议
8. 短线/中线/长线正式候选是否已写入 forward_test，方便后续按周期验证；观察机会只跟踪，不当作正式买入记录
```

```
只使用指定策略生成信号（用 -s 逗号分隔，例如只看趋势类策略）：
./go-quant signal -s macd,rsi,bollinger,ma_crossover -n 5

短线专用策略组合：
./go-quant signal -s sar,roc,kdj,bull_flag,limit_up,atr_breakout -n 10

中线专用策略组合：
./go-quant signal -s ma_crossover,macd,value_ma60,trend_pullback -n 10
```

```
输出信号为 JSON 格式（供程序解析或回测系统对接）：
./go-quant signal -f json -n 10

输出信号为 CSV 格式（供 Excel/数据分析）：
./go-quant signal -f csv -n 20
包含完整字段：信号周期、当前价、资金流向金额、盘中标签、风险原因等。
```

```
新闻缓存刷新：
signal 命令自动从新浪拉取最新 80 条新闻并缓存到 data/raw/news/latest.parquet。
如果怀疑新闻数据过旧，删除该文件后重新运行 signal 即可强制刷新。
也可联网核验外部新闻，与本地新闻热度互相印证。
```

---


## 盘中实时修正

```
现在还没到 16:00，Tushare 今日正式日线可能还没有发布。
请在 /home/xxx/quant-go 编译并运行：
./go-quant signal -n 5 --watch 15

（如果不需要新浪实时行情，加 --realtime=false 跳过；
新浪实时行情请求失败时会自动退回本地数据。）

请用"昨天收盘后的量化信号 + 新浪实时行情"做盘中修正。
重点判断：
1. 昨天短线 Top 候选和观察机会今天是否已经高开超过 3%，高开过多就不要追
2. 候选股是否接近涨停或已经涨停，提示可能买不进
3. 候选股是否盘中走弱，量化信号是否失效
4. 卖出/回避股是否盘中继续下跌，风险是否确认
5. 我的持仓用实时价重新计算盈亏，是否需要止盈、止损或继续观察
6. 观察机会只能给“可低吸观察/等待确认/放弃追高”，不能直接当正式买入
7. 最终只给“可低吸观察、等待回落、放弃追高、风险确认”这类盘中动作建议
```

```
请只做盘中可交易性校验，不要重写历史数据，也不要把新浪实时价当成正式日线回测数据。
如果新浪实时行情请求失败，请明确说明“无法做盘中修正”，然后退回使用本地最新 Tushare 日线和新闻/资金数据分析。
```

---

## 新闻和政策校验

```
请基于 ./go-quant signal -n 5 --watch 15 的输出做新闻和政策校验。
要求：
1. 先列出本地新闻热度中的热门话题和受关注个股
2. 判断这些新闻属于政策催化、行业催化、公司利好、公司利空、监管风险还是宏观风险
3. 对每个短线/中线 Top 买入候选，判断新闻是确认、背离、中性还是风险
4. 新闻只作为确认层和风险否决层，不要单独因为新闻热度高就给买入建议
5. 如果新闻和资金流向、量化信号互相冲突，请降低建议优先级并说明原因
```

```
如果本地新闻数据不足或明显过旧，请联网核验最近 24 小时的重要政策、监管、宏观、行业和公司新闻。
要求：
1. 必须给出具体日期和来源
2. 优先核验会影响全市场风险偏好、强势板块、弱势板块和候选股票的新闻
3. 不使用未经证实的传闻作为买入理由
4. 如果外部新闻与量化信号冲突，请明确标注为风险或否决项
5. 最终仍以量化信号、资金流向、涨跌停可交易性和持仓风控为主
```

```
请把今天的新闻/政策影响整理成交易前检查清单：
1. 今日是否有宏观、监管、政策级别的大事件
2. 哪些行业受到正面催化，哪些行业受到负面冲击
3. 我的候选买入票是否直接受益或受损
4. 我的持仓是否有突发利好、利空或监管风险
5. 哪些信号应该提高优先级，哪些应该降级或剔除
```

---

## 回测

```
切换到项目目录，对 ma_crossover 策略进行回测：
回测区间 2025-01-01 到 2025-12-31，初始资金 10 万，显示绩效报告。
命令: go build -o go-quant ./cmd/go-quant/ && ./go-quant backtest -s ma_crossover --start 20250101 --end 20251231 --capital 100000
然后分析：总收益率、最大回撤、夏普比率、胜率、盈亏比、年化波动率分别是多少？这个策略表现如何？
（如果数据是旧版行情缺少真实成交价字段，加 --allow-adjusted-trades 临时使用复权价近似）
```

```
用逗号分隔同时回测多个策略并对比绩效：
./go-quant backtest -s ma_crossover,macd,rsi,bollinger --start 20250101
按总收益率、最大回撤、胜率从高到低排名，分析各策略的表现差异。
```

```
回测短线策略组合：
./go-quant backtest -s sar,roc,kdj,bull_flag,limit_up --start 20240101 --end 20241231
对比中线策略组合：
./go-quant backtest -s ma_crossover,macd,value_ma60 --start 20240101 --end 20241231
看哪个组合在 2024 年表现更好。
```

```
用 -n 限制回测股票数量（提速），例如只回测前 50 只：
./go-quant backtest -s ma_crossover --start 20250101 -n 50
```

---

## 前向测试

```
验证前向测试记录的收益表现（短线回填 1/3/5 日，中线回填 10/20/40 日，长线回填 60/120/250 日）：
./go-quant forward validate

如果原始行情数据缺少真实成交价字段（旧版 parquet），使用复权价近似：
./go-quant forward validate --allow-adjusted-trades

迁移旧版 forward_test 记录到当前 schema（按新版本信号输出保持一致）：
./go-quant forward migrate

前向测试记录存放在 data/forward_test/ 目录，
每日马克档由 systemd 定时任务自动生成。目录可指定 --dir <路径>。
```

```

昨天信号中的短线买入候选，若系统已写入 forward_test，可以在收盘后运行：
./go-quant forward validate
对比 1 日/3 日/5 日收益，检验短线信号的准确性和稳定性。
```

---

## 持仓管理

```
查看我的持仓盈亏：编译后跑 ./go-quant signal -n 5 --watch 15，
从输出中提取"持仓概览"部分，告诉我每只股票的成本、现价、收益率。
如果有已平仓的交易记录，也列出历史胜率和累计盈亏。
portfolio.yaml 文件在项目根目录，格式是 transactions[] 交易流水。
```

```
帮我记录一笔交易。编辑 portfolio.yaml，在 transactions 列表末尾追加：
- date: "20260725"
  code: "000001.SZ"
  action: buy
  shares: 200
  price: 10.00
  comment: "示例买入"
追加后显示当前持仓状态。
```

```
帮我记录一笔卖出。编辑 portfolio.yaml，追加：
- date: "20260728"
  code: "000001.SZ"
  action: sell
  shares: 100
  price: 11.00
  comment: "示例止盈"
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
1. go build -o go-quant ./cmd/go-quant/
2. ./go-quant fetch --today （如果数据还没出就跳过）
3. ./go-quant fetch --stk-limit --today
4. ./go-quant fetch --moneyflow --today
5. ./go-quant fetch --index --today
6. ./go-quant signal -n 10 --watch 5 （默认请求新浪实时行情，获取正式候选、观察机会和持仓当前价）
7. 分析输出：市场概况 + A股赚钱效应/涨跌停扩散 + 新闻热度 + 仓位策略 + 持仓实时盈亏 + 短线/中线/长线交易信号 + 观察机会榜
8. 重点校验：是否应该空仓 + 多策略共振 + 观察机会是否值得跟踪 + 资金确认/背离 + 新浪当前价/盘中涨跌幅 + 新闻/政策催化 + 涨跌停不可成交风险 + A股风险标签
9. 如果仓位策略是空仓或观望，正式推荐买入写"无"，观察机会只能写"可跟踪/等待确认"，不要为了凑满 Top N 强行推荐
10. 整理成一段简洁的日报：今日市场判断、是否空仓、推荐买入的股票、观察机会、我的持仓操作建议（日报行数最好不好超过60行，字数太多影响观感）
```

```
盘中获取当前价格并修正建议：
./go-quant signal -n 5 --watch 15
请重点看输出里的“盘中”列，基于新浪实时当前价判断：
1. 候选股当前价、盘中涨跌幅、更新时间
2. 哪些票高开过多、涨停买不进、盘中走弱
3. 我的持仓按当前价计算后的盈亏和操作建议
4. 哪些昨天的短线候选或观察机会仍可低吸，哪些应该放弃追高
5. 如果仓位策略为“空仓”或“观望”，正式买入建议写无；观察机会只给跟踪建议
```

```
盘前快速分析（不拉数据，用昨天收盘数据）：
./go-quant signal -n 15 --watch 20
只告诉我：
1. 今天是否应该空仓
2. 如果不是空仓，今天该买什么（Top 5 正式推荐）
3. 观察机会里有哪些只适合跟踪、等待确认或低吸，不要和正式买入混在一起
4. 如果新浪实时行情已经有开盘数据，哪些票高开过多、涨停买不进或盘中走弱
5. 我的持仓按实时价该不该动
6. 新闻/政策有没有明显风险或催化
7. 一句话市场判断
```

---

## 个股深度分析

```
分析 601899.SH 紫金矿业：
先运行内置分析命令获取技术面和基本面概览：
./go-quant analyze 601899.SH
（输出：均线 MA5/10/20/60/120/250、PE/PB/市值/股息率/换手率、全部策略信号）

然后在此基础上深入：
1. 从 data/raw/moneyflow/ 提取最新资金流向，判断资金确认、背离还是分歧
2. 从新闻热度中找有没有提及紫金矿业的新闻，必要时核验最近 24 小时外部新闻
3. 综合给出：该买、该卖、还是持有？理由是什么？
```

```
对比分析两只同一行业股票（示例 000001.SZ 和 600000.SH，请按实际目标替换），
从技术面（均线趋势、策略信号）、资金流向、基本面（PE/ROE/市值）、
新闻热度和政策/行业催化几个方面对比，告诉我哪只更适合当前买入。
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
1. 读取 data/raw/meta/trade_cal.parquet 获取 2020-2026 全部交易日
2. 读取 data/raw/daily/*.parquet，统计每个年份的行数
3. 对比找出缺失的交易日
4. 用 ./go-quant fetch --date <YYYYMMDD> 逐个补全缺失日期
5. 补完后重新运行 ./go-quant fetch 重写年份 parquet 文件
6. 告诉我补齐了多少条数据
```

```
我的 Tushare 额度只剩 2000 次了，帮我统计最近 7 天各 API 分别调用了多少次。
如果额度紧张，建议我接下来优先拉什么数据。
```

---

## 配置管理

```
查看和修改量化项目配置（config.yaml）：
关键配置项：
  fetch.min_market_cap    市值过滤（默认 0，不限制；设为 100 表示过滤 100 亿以下股票）
  fetch.stock_prefixes    股票池范围（默认 60/00/001 = 上海主板/深圳主板/中小板）
  tushare.daily_call_limit  每日 API 调用上限（默认 5000）

环境变量覆盖（优先级高于 config.yaml）：
  QUANT_TUSHARE_TOKEN      Tushare Token
  QUANT_DATA_DIR           数据目录
  QUANT_INITIAL_CAPITAL    回测初始资金
  QUANT_RISK_FREE_RATE     无风险利率
```

```
API 调用次数监控：
每次 fetch 完成后系统会自动输出调用统计（今日已调用 X 次，上限 Y 次）。
如果额度紧张（剩余 < 500），优先拉取日线行情和涨跌停价，资金流向和基本面可以延后。

检查当日调用次数：
看最后一次 fetch 命令的输出结尾，或运行任意 fetch 命令查看统计。
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

```
行情数据质量校验：
如果回测或前向验证报错 "真实价字段缺失"，说明旧的 parquet 文件仅有复权价，
缺少 raw_open/raw_high/raw_low/raw_close/adj_factor 字段。
解决方案：重新拉取数据，例如 ./go-quant fetch --today --force
临时方案：加 --allow-adjusted-trades 使用复权价近似（精度稍低但通常可接受）。

检查特定年份数据是否有真实成交价字段：
读取 data/raw/daily/<年份>.parquet 的 schema，确认是否包含 raw_open 等字段。
```

```
数据文件损坏排查：
如果某年份 parquet 文件读取报错，先用 parquet-tools 检查文件完整性，
确认不是下载中断导致的截断文件。如果是，删除该年份文件后重新拉取：
./go-quant fetch --start-year <YYYY> --end-year <YYYY>
或逐个日期补全：./go-quant fetch --date <YYYYMMDD>
```
