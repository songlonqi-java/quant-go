# 架构说明

## 核心模块

`cmd/go-quant` 只负责命令行参数解析和结果打印。业务流程应放在 `internal/` 下，避免未来 Web、定时任务和 CLI 复制同一套逻辑。

`internal/dataset` 负责本地数据集装配：读取日线、应用涨跌停价、按最新交易日过滤、过滤 ST、加载股票名称、基本面、资金流和交易日历。新的分析入口优先依赖这个模块。

`internal/workflow/signal` 负责完整信号流程：选择策略、加载数据集、分析市场和新闻、计算持仓、应用实时行情、执行仓位策略、写入前向测试记录。CLI 和未来 Web 页面都应调用 `Run`，而不是重新拼流程。

`internal/strategy` 只放策略实现和策略元数据。周期和策略组由 `MetadataForStrategy` 统一维护，新增策略时要同时注册构造函数和元数据。

`internal/signal` 负责信号聚合、评分、资金/盘中标签、仓位策略和报告输出。它不负责读取本地文件。

`internal/forward` 负责前向测试记录和验证。CSV schema 与不同周期验证目标集中在 `schema.go`。

`internal/validation` 负责全量历史回放、样本外折统计、可成交约束与证据读取。它只暴露构建、加载和给候选添加证据的接口；信号工作流据此决定正式买入资格和风险预算，不在 CLI 中重放策略。

`internal/data` 负责 Tushare/Sina 之外的本地存储、Tushare 客户端和数据拉取。`Fetcher` 仍保持原接口，但内部按数据域逐步拆文件。

## 重构原则

- 新入口优先复用 `dataset.Load` 和 `signalworkflow.Run`。
- 不在 CLI、Web 或定时任务里复制策略选择、基本面注入、实时行情和前向记录逻辑。
- 新增策略时维护一个事实来源：`strategy.MetadataForStrategy`。
- 新增数据源时优先放在 `data` 中，进入分析前通过 `dataset` 暴露。
