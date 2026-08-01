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
| `--daily-basic` | PE/PB/市值/股息率/换手率；配合 `--today` 仅补当日快照 | `./go-quant fetch --daily-basic --today` |
| `--financials` | ROE/利润率/利润表 | `./go-quant fetch --financials` |
| `--hs300` | 沪深300成分股 | `./go-quant fetch --hs300` |
| `--index` | 上证/深证/创业板指数 | `./go-quant fetch --index` |

`daily_basic` 不随 `fetch --today` / `fetch --date` 自动更新。它的估值字段供价值投资模块使用，换手率供日常流动性校验使用；缺失换手率默认只显示提示，不会阻断买入，设置 `liquidity.require_turnover_data: true` 后才会失效关闭。月度筛选、季度复核或需要严格换手过滤前，使用 `./go-quant fetch --daily-basic --date <最近交易日>` 显式拉取并合并当日快照；构建完整历史证据前使用 `./go-quant fetch --daily-basic` 补齐历史。

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

聚合字段包括涨跌幅、上涨家数占比、MA20 上方占比、涨跌停数量、成交额放大倍数、资金净流入、大单净流入、领涨股、异动标签及估值快照。估值快照会持久化 `PEAvg`、`PETTMAvg`、`PBAvg`、各自有效样本数，以及更适合板块横向比较的 `PETTMAggregate`/`PBAggregate`。

`PETTMAggregate` 的口径为“盈利样本总市值 ÷ 其隐含 TTM 净利润之和”；亏损、零 PE 或缺少市值的股票不纳入，样本数会同时保存。因此它不是把个股 PE 简单平均，也不能与含有亏损公司的全样本算术均值混用。月度筛选或季度复核前，在显式拉取估值后运行 `./go-quant sector build --date <YYYYMMDD>`，即可将该日板块估值写入 `data/raw/sector_daily/YYYY.parquet`；`analyze <代码>` 会显示所属行业的该口径 PE_TTM。

---

## `go-quant value` — 慢频价值投资

价值模块不参与日常 `signal` 的短中线推荐，也不使用盘中实时行情。它只使用明确落盘的收盘后估值和财务数据，候选池不等同于立即买入清单。

月末先更新单日估值、重建该日行业聚合，再运行月度筛选：

```bash
./go-quant fetch --daily-basic --date 20260731
./go-quant sector build --date 20260731
./go-quant value monthly --date 20260731 -n 20
```

| 命令 | 说明 |
|------|------|
| `value monthly` | 以 PE_TTM/PB、行业聚合估值、ROE、利润及营收增速生成价值候选池，并保存到 `data/raw/value/YYYYMMDD.json` |
| `value quarterly` | 读取最近月度候选池，按最新财务与行业估值给出继续跟踪、估值回归或基本面恶化结论，保存到 `data/raw/value/review/YYYYMMDD.json` |
| `--date YYYYMMDD` | 指定已同步日线、估值和行业快照的交易日，默认本地最新日线日期 |
| `-n, --top N` | 月度任务保存/显示前 N 个候选；季度任务限制显示/保存的复核条数；`0` 表示全部 |

非金融行业使用正 PE_TTM、PB 合理、ROE 不低于 8%、利润与营收同比不为负，以及相对行业聚合 PE_TTM 至少 20% 折价；银行、保险、证券和多元金融改用 PB 与 ROE。季度复核中，ROE 低于 6% 或利润/营收同比低于 -10% 会移出价值池；相对行业估值折价收敛至 5% 内时标记为“评估分批止盈”。这些是固定、可复现的 `value-v1` 规则，不能把候选池当作即时交易指令。

---

## `go-quant signal` — 交易信号

| 参数 | 说明 | 示例 |
|------|------|------|
| `-s, --strategy` | 指定策略（逗号分隔），默认 10 个日常策略 | `-s macd,rsi,volume_breakout` |
| `-n, --top` | 每个周期买入/卖出各最多 N 条 | `-n 5` |
| `--watch` | 额外显示观察机会数量，默认 15，0 表示关闭 | `--watch 15` |
| `-f, --format` | `table` / `csv` / `json` | `-f json` |
| `--realtime` | 仅在连续竞价时段使用实时行情，默认开启 | `--realtime=false` |
| `--market-realtime` | 盘中加载全市场行情并复用至候选和持仓，默认开启 | `--market-realtime=false` |
| `--market-window` | 全市场行情刷新窗口，默认 1 分钟 | `--market-window 1m` |
| `--realtime-source` | `auto`（东方财富主源、失败时新浪降级）/ `eastmoney` / `sina` | `--realtime-source eastmoney` |

输出五个板块：市场概况 → 新闻热度 → 仓位策略 → 持仓概览 → 短线/中线/长线买卖建议。市场概况会展示赚钱效应、涨跌停数量和 A 股风险标签。日常默认策略不使用 PE、PE_TTM、PB、ROE、分红等慢频估值条件；这些条件仅在 `value` 命令使用，但会读取 `daily_basic` 的换手率执行流动性校验。

`signal` 会按周期分别聚合策略，表格只展示触发 BUY/SELL 的策略，不展示 HOLD 策略。CSV/JSON 输出包含 `horizon` 字段；如果本地有 `moneyflow` 数据，还会输出资金净额和大单净额，并在风险标签里标注资金确认、资金背离或资金分歧。表格和 Web 显示“风险执行”及紧凑流动性摘要，CSV/JSON 还保存上市天数、平均成交额、换手率、预计订单金额、成交占比和冲击成本。上市不足、ST、停牌、成交额不足、换手率不足或订单超过成交占比上限均为硬过滤。

`signal` 只在 A 股连续竞价时段（09:30-11:30、13:00-15:00，Asia/Shanghai）请求实时行情。默认 `auto` 先用东方财富的全市场分页快照，在约一分钟窗口内均匀铺开请求，输出全市场盘中宽度，并复用同一批报价校验候选和持仓；主源请求失败或有效报价覆盖低于 90% 时，才以相同限速规则退回新浪。午休、盘后和周末不请求实时源，转而使用本地/Tushare 日线。常见盘中标签包括 `高开>3%`、`涨幅偏高`、`盘中走弱`、`涨停风险`、`跌停风险`、`卖压确认`。实时行情只作为盘中交易可行性校验，不写入历史日线 Parquet，也不参与正式回测。

仓位策略会先判断 `空仓` / `观望` / `轻仓试错` / `正常买入`。当市场偏弱、资金缺少确认、候选股盘中走弱或存在涨停/高开不可成交风险时，买入候选会被降级为观望，避免为了凑满 Top N 强行推荐。

`--watch` 会在正式买卖建议后追加“观察机会”榜。观察榜不等于买入推荐，主要收纳单策略较强、共振不足、排名靠后、存在卖出冲突、被仓位/风控过滤的候选，方便盘中人工跟踪，避免只看正式榜而错过潜在机会。

`signal` 会自动把每个周期前 5 个合格买入候选写入 `data/forward_test/`。短线验证 1/3/5 日，中线验证 10/20/40 日，长线验证 60/120/250 日。如果仓位策略判断应空仓，会写入一条 `CASH` 记录，表示当天不新增买入。
当 `data/raw/validation/evidence.json` 已由 `validate build` 生成时，正式买入还必须通过历史独立日期样本量、样本外正收益折数与收缩后期望收益门槛；未通过的结果仅保留在观察机会中。表格会以“日期样本/交易”同时展示两个数字，CSV 和 JSON 也会分开输出；资格门槛只使用日期簇数，不使用同日股票数。若证据文件缺失，命令会提示先构建，但不会中断日常信号输出。

当使用 `PrintWithWatch` 的 CLI 输出时，JSON 会返回 `{ "signals": [...], "watchlist": [...] }`，CSV 会增加 `类别` 和 `观察原因` 字段，用于区分正式信号和观察机会。

---

## `go-quant market realtime` — 全市场盘中行情

```bash
./go-quant market realtime --window 1m
```

从本地可交易股票池分批请求实时行情，计算盘中覆盖率、上涨/下跌家数、赚钱效应和精确涨跌停数量。默认 `auto` 使用东方财富；只有主源异常或有效报价覆盖不足时才退回新浪。全市场分页请求按一整分钟均匀铺开，避免对公共行情接口突发访问；遇到请求失败会在相同限速约束下重试。

该快照仅用于盘中风险监控，**不写入日线、不参与回测或历史验证，也不会在收盘前将“空仓”结论升级为“买入”**。覆盖率低于 90% 时会明确标记为不完整。

| 参数 | 说明 | 示例 |
|------|------|------|
| `--window` | 完成一轮全市场刷新所用的窗口 | `--window 1m` |
| `--min-coverage` | 接受快照所需的最低覆盖率 | `--min-coverage 95` |
| `--source` | `auto` / `eastmoney` / `sina` | `--source eastmoney` |

---

## `go-quant backtest` — 策略回测

| 参数 | 说明 | 示例 |
|------|------|------|
| `-s, --strategy` | 回测策略 | `-s quality_value,macd` |
| `--start / --end` | 日期范围 | `--start 20250101` |
| `--capital` | 初始资金 | `--capital 200000` |
| `-n, --top` | 单策略模式：按代码前 N 只；组合模式：每周期候选数 | `-n 20` |
| `--ensemble` | 多股票、多策略共享资金的组合回测 | `--ensemble` |
| `--ablation <策略>` | 仅组合模式；对比当前基线与加入指定策略后的结果 | `--ensemble --ablation quality_value` |
| `--allow-adjusted-trades` | 允许用复权价近似成交价，仅用于旧数据临时验证 | `--allow-adjusted-trades` |

绩效指标：总收益、年化收益、最大回撤、夏普比率、卡玛比率、胜率、盈亏比

默认模式逐只股票、逐策略独立满仓，只用于诊断指标本身。`--ensemble` 才会使用共享现金账户，逐日复用多策略聚合、历史市场状态、买入资格、Top-N、已有持仓暴露以及组合/单票/行业预算。未显式传 `-s` 时，回测与日终信号共用 `DailyStrategyNames` 过滤；即使旧 `config.yaml` 仍列出慢频策略，也不会悄悄混入默认交易基线。显式 `-s` 仍可用于慢频或实验研究。组合模式示例：

```bash
./go-quant backtest --ensemble --start 20210101 --end 20260731
./go-quant backtest --ensemble -s sar,kdj,rsi,bollinger,volume_breakout -n 5
./go-quant backtest --ensemble --ablation quality_value --start 20230101 --end 20260731
```

`--ablation` 会从基线策略列表中移除目标策略，再用一组全新策略实例把它加回实验组，避免两次回测共享状态。两组只共用一次准备的不可变排序行情、日期索引、代码顺序和历史市场状态，策略缓存、现金和持仓状态仍完全隔离。报告对比净收益、最大回撤、夏普、胜率、盈亏比、半边换手率和逐年收益改善次数。它是研究工具，不读取当前 `evidence.json`，也不会自动修改默认策略。

准入门禁会失效关闭：实验组年化收益必须为正且改善基线，最大回撤不得恶化，换手增幅不超过 5%，至少要有 3 个可比年份且其中不少于 2/3 的年份收益改善。“亏得少一点”不等于可进入默认组合。

全量消融会连续回放两次共享资金组合，命令会分别输出环境准备、基线和实验组耗时。开发时可先用 `-s relative_strength,atr_breakout,trend_pullback` 做端到端冒烟，但策略准入必须回到完整默认组合和多年区间。

组合回测还会输出交易成本归因。每个成交边在内存账本中保存真实开盘价、实际成交价、原始/实际成交额、手续费、固定滑点、市场冲击金额和成交占比；平仓成交同时保存成本前毛盈亏和扣费净盈亏。报告中的“成本前毛盈亏”是在实际成交路径上把已记录成本加回，用于严格对账，不代表按无成本资金重新调整仓位后的反事实收益。

回测成交口径：T 日收盘产生信号，T+1 开盘成交，并考虑滑点、手续费、信号日可知的平均成交额、订单成交占比、平方根冲击成本以及开盘涨跌停不可成交约束。超过 `liquidity.max_participation_pct` 的买单会被拒绝；退出不会因成交占比过高而永久阻断，而是使用上限内的冲击估计继续卖出。有精确 `stk_limit` 时只使用精确价格，缺失时按主板/创业板与科创板/北交所的 10%/20%/30% 规则保守兜底。持仓退出还会执行统一的初始止损、ATR 移动止损和时间止损；都只在收盘确认，下一市场交易日开盘卖出。卖单遇到停牌或跌停会保留到首次可成交开盘，输出同时统计退出原因、延迟天数、冲击成本和尾部亏损；买入订单只尝试下一市场交易日。

组合回测不会把当前 `evidence.json` 直接应用到过去，因为当前证据包含之后日期的汇总，会造成前视；它按当日可见的信号资格规则回放。正式推荐是否具备样本外证据仍由 `validate build` 和持续的前向测试判断。

信号输出中的“原始买/卖”用于审计触发策略数量，“有效买/卖(组)”才用于方向、置信度和正式资格。相关性调整规则及各周期门槛见 [策略参考](strategies.md#聚合与风控)。

旧版行情 Parquet 没有 `raw_open/raw_high/raw_low/raw_close/adj_factor` 字段。回测默认拒绝这类数据，避免把前复权价当真实成交价；重新执行 `fetch` 可补齐。

---

## `go-quant validate build` — 历史推荐资格验证

```bash
./go-quant validate build
./go-quant validate build --start 20200101 --end 20251231 --workers 16
./go-quant validate build -s macd,rsi,trend_pullback
```

该命令回放“策略信号 → 市场仓位策略 → 次日可成交入场 → 有状态退出”完整链路，在历史后半段按时间顺序切分样本外折，并按短、中、长周期记录策略组合与市场状态的胜率、期望收益、波动和最大回撤。同一股票+周期在上一笔完整交易结束前不再重复入样；每折开头分别设置短线 5 日、中线 20 日、长线 120 日的 embargo，折尾不足以完成实际退出的样本直接清除。同一信号日的多只股票收益先等权合成一个日期簇，再计算胜率、期望、波动、回撤和正收益折，避免把同日市场共振当成大量独立样本。

入场只尝试紧邻信号日的下一市场交易日，并拒绝停牌、一字涨停、高开超过 3%、上市时间/成交额/换手率不合格或订单成交占比过高等信号日已知的不可执行场景；开盘后跌破前低只记录为风险，不会反向抹除已经成交的样本。短/中/长线默认使用 5%/8%/12% 初始止损、2/2.5/3 倍 14 日 ATR 移动止损，以及 5/20/120 个市场交易日时间止损。退出遇到停牌或跌停会延迟并持续尝试，但完整交易不会越过所属时间折或指定回测截止日。历史回放不使用今天的股票名称倒推过去的 ST 状态；在补充 Tushare 名称变更历史前，这一项会作为证据限制明确保存。

证据默认写入 `data/raw/validation/evidence.json`，并被 `signal` 读取。文件会保存原始交易数、独立日期簇数、重叠/embargo/跨折/流动性过滤数、退出原因、延迟退出、冲击成本和尾部亏损统计，同时包含采样规则以及策略、费用、流动性参数、参考权益、完整行情/上市信息和构建过程指纹。数据补齐、策略、参数、费用、参考权益或决策实现变更后必须重新构建；本版本证据格式为 v5、决策模型为 v8，升级后必须重新执行 `validate build`。

资格判定优先使用“同策略组合 + 同市场状态”；如果该状态从未出现过这一策略组合，才使用跨市场状态的同策略组合证据。同周期+同市场状态和整个周期统计只是收缩先验，不能单独使候选合格；先验也不能把策略自身的负期望收益翻转成正式资格。输出会分别显示“资格依据”和“收缩先验”。`validation.min_samples` 表示策略专属证据的独立信号日期簇数，`validation.prior_samples` 是宽泛先验在收缩时的最大等效样本权重，实际权重不会超过先验自身的日期样本数。验证启用时，证据缺失、过期或不兼容会让正式买入失效关闭。历史抓取现在会覆盖退市和暂停上市证券，但旧归档必须重新执行 `fetch` 才能补齐，且仍不能替代持续的 `forward validate` 前向测试。

| 参数 | 说明 | 示例 |
|------|------|------|
| `-s, --strategy` | 回放策略，默认使用 `signal.default_strategies` | `-s macd,rsi` |
| `--start / --end` | 限定回放信号日期 | `--start 20200101 --end 20251231` |
| `--workers` | 并行回放工作数，默认 GOMAXPROCS | `--workers 16` |
| `--output` | 自定义证据文件路径 | `--output /tmp/evidence.json` |
| `--allow-adjusted-trades` | 允许旧数据用复权价近似成交价，仅临时排查使用 | `--allow-adjusted-trades` |

---

## `go-quant forward` — 前向测试

| 参数 | 说明 | 示例 |
|------|------|------|
| `validate` | 用本地行情回填短/中/长线毛收益、交易成本和净收益 | `./go-quant forward validate` |
| `migrate` | 迁移旧版 `picks.csv` 到当前 schema | `./go-quant forward migrate` |
| `--dir` | 前向测试目录，默认 `data/forward_test` | `./go-quant forward --dir data/forward_test validate` |
| `validate --allow-adjusted-trades` | 允许用复权价近似验证价格，仅用于旧数据临时验证 | `./go-quant forward validate --allow-adjusted-trades` |

前向验证会执行入场可行性规则：目标日高开超过 3%、开盘涨停或信号日流动性不合格会标记为未成交；如果开盘已经成交，盘中跌破前一交易日低点只在 `notes` 中记录风险事件，仍保留并计算后续持有收益。旧版 `invalid_break_prev_low` 状态会迁移为已成交记录并补算后续收益。

`picks.csv` 包含 `horizon` 字段；短线回填 `day3/day5`，中线回填 `day10/day20/day40`，长线回填 `day60/day120/day250`。这些固定观察点继续用于横向比较：原有 `next_return_pct`、`dayN_return_pct` 是开盘到观察期收盘的毛收益，对应的 `*_cost_pct` 是双边手续费、滑点和冲击成本造成的收益率拖累，`*_net_return_pct` 是净收益。另一组 `exit_model`、`managed_exit_status`、`exit_trigger_date`、`exit_reason`、`exit_date`、`exit_delay_days`、`exit_*_return_pct` 和 `exit_tail_loss` 字段记录与历史证据相同的实际退出路径。每行同时保存流动性模型/参数、预计订单额、平均成交额、换手率、上市天数、进出成交占比与冲击率，以及退出模型、`cost_model`、`commission_rate` 和 `slippage_rate`；任一模型版本、费率、流动性参数或参考权益变化都会从原始价格路径重算。

旧 CSV 运行 `forward validate` 时会先迁移 schema，再用已保存的开盘/收盘价补算成本和净收益；毛收益不会丢失。手续费或滑点配置改变后，净收益会从原始价格重算，不会在旧净收益上重复扣费。`CASH` 行的等权市场代理也同时保存毛收益和假设执行后的净收益，便于公平比较空仓反事实。

---

## `go-quant list` — 查看策略

列出所有已注册策略及预热期。
