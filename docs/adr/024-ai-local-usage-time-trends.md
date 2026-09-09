# ADR-024：AI 本地用量 UTC 时间趋势

- 状态：Accepted，AI7-Q3 1–30 天本地用量趋势已实现
- 日期：2026-09-09
- 决策范围：用量时间窗口、终态根步骤、UTC 分桶、界面趋势、费用边界
- 相关文档：[ADR-012](012-ai-run-steps-and-local-metrics.md)、[AI 助手](../modules/ai-assistant.md)、[AI7 计划](../plans/ai-quality-gates.md)

## 背景

ADR-012 已提供当前 scope 的累计运行数、Provider 原始 token、bytes 和耗时，但用户无法看出近期使用量是否集中在某几天，也无法从当前会话内区别“没有使用”与“历史较早”。在没有用户价格配置或版本化价格快照的前提下，把 token 直接换算成费用会产生伪精确账单；联网按模型名查询价格则违反本地优先与可解释边界。

本次只增加可复算的时间趋势，不引入价格、货币、估算 token 或线上服务。

## 决策

### 1. 既有用量 API 增加趋势窗口

`GET /api/v1/ai/usage-summary` 新增可选 `trend_days=1..30`，缺省为 7。现有 `session_id` 和 `provider_id` 可任意组合，且同样约束趋势范围。

响应新增：

- `trend_days`：服务端确认的窗口长度；
- `trend[]`：固定长度、连续 UTC 日的点，从 `Now UTC - (days - 1)` 到 `Now UTC`，每项包含 `day` 与同一套运行/usage/bytes/duration 计数。

空日期仍返回零点，因此 7/30 天图形不会把相邻日压缩成错误连续使用。窗口只改变 `trend`；`totals` 与 `providers` 保持既有全量 scope 累计语义，避免 API 调用者误把最近 7 天当作全部历史。

### 2. 只统计终态 generation 的 sequence-1 根步骤

趋势只 JOIN `ai_generations` 的 `completed / failed / cancelled` 与 `ai_run_steps.sequence=1, kind=generation`，使用根步骤 `completed_at` 的 UTC 日期。queued/streaming 没有稳定结束事实，不进入趋势，也不会制造“今天已完成”的条目。

每点复用 ADR-012 的全有/全无 usage 口径：

- Provider 完整返回 input/output token 时才计入 `provider_usage_generations`、token 合计；
- 终态但没有完整 usage 时计入 `unknown_usage_generations`；
- bytes 与 duration 仍是本地运行指标，不冒充 token 或费用；
- 每点 `active_generations=0`，因为趋势只代表终态事实。

所有查询在同一只读 SQLite 事务内完成。不会读取 prompt、content、reasoning、工具参数/结果、Provider URL、key、消息正文或模型回答。

### 3. 会话 UI

当前会话“本地用量”面板增加紧凑的趋势区：

- 可切换 7 天/30 天；
- 每天显示终态 generation 数与 token coverage，条形图只表示相对运行数量；
- 清晰标识“UTC 日 · 仅终态运行”；
- 缺失 usage 继续显示 unknown，且所有面板保留“不估算 token，也不计算费用”。

新 query key 包含窗口长度；generation 结束后按 session 前缀失效全部窗口缓存，不让 30 天视图停留在旧事实。桌面与 390 px 下必须保留日期、数量、token coverage 和范围切换，不以条形长度作为唯一信息。

## 威胁与控制

| 威胁                             | 控制                                                        | 验证               |
| -------------------------------- | ----------------------------------------------------------- | ------------------ |
| 活动 generation 被当作已发生趋势 | 只查询终态根步骤和 completed_at                             | API 测试           |
| 零使用日期被省略而误导连续性     | 服务端以 Now UTC 补齐连续固定窗口                           | API/Web 测试       |
| 趋势窗口偷偷改变累计 totals      | 仅趋势受 `trend_days` 约束，累计字段不变                    | API 契约测试       |
| 部分 usage 被编造成 token        | 复用 Provider 全有/全无规则                                 | Model/API/Web 测试 |
| 客户端接受伪造日期或活动点       | 严格校验 ISO 日期、连续性、长度、状态恒等式和零活动         | Web API 测试       |
| 用户把 token 当费用              | UI 与 API 不提供 price/currency/cost 字段，并明确费用未启用 | 禁止字段/UI 测试   |

## 明确不做

- 不创建价格表、货币、估算费用、预算告警或账单；
- 不从模型名联网查询价格，不默认任何 Provider 价格；
- 不聚合到云端、跨设备或团队；
- 不记录 prompt、回答、reasoning、工具参数/结果、URL 或密钥；
- 不把运行量用于自动路由、限流、禁用 Provider 或业务决策；
- 不提供超过 30 天、按小时或按模型自动对比的图表。

## 验收

- `trend_days=1..30` 校验、session/provider 组合、UTC 连续零填充、completed/failed/cancelled、unknown/provider usage、bytes/duration 与活动排除均通过；
- 客户端严格拒绝成本字段、非连续/非法日期、活动趋势行、长度或 scope 不一致；
- 会话面板可切换 7/30 天，generation 结束后两个窗口均失效；
- 完整 Web、API race、`go vet`、gofmt、文档链接以及桌面/390 px 视觉检查通过；
- 完整 Go 若仍只有既有 Automation Event Delivery 基线失败，须明确记录。
