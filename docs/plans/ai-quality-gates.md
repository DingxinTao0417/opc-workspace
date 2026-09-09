# AI7 质量闸门与可解释性计划

> 依据：[ADR-011](../adr/011-ai-validated-knowledge-citations.md)、[ADR-012](../adr/012-ai-run-steps-and-local-metrics.md)、[ADR-013](../adr/013-ai-local-quality-evaluation-runner.md)、[ADR-014](../adr/014-ai-local-quality-trends.md)、[ADR-015](../adr/015-ai-local-quality-categories.md)、[ADR-016](../adr/016-ai-local-quality-failure-codes.md)、[ADR-017](../adr/017-ai-local-quality-dataset-v2.md)、[ADR-018](../adr/018-ai-local-quality-confidence-intervals.md)、[ADR-019](../adr/019-ai-local-quality-advisory-readiness.md)、[ADR-020](../adr/020-ai-local-quality-dataset-v3.md)、[ADR-021](../adr/021-ai-local-quality-tiered-suites.md)、[ADR-022](../adr/022-ai-local-quality-human-review-audit.md)、[ADR-023](../adr/023-ai-local-quality-topic-suites.md)、[ADR-024](../adr/024-ai-local-usage-time-trends.md)、[AI 助手模块](../modules/ai-assistant.md)
>
> 当前状态（2026-09-09）：AI7-Q1 已完成；Q2 当前范围已交付离线 scorer、本地 Provider Actor、dataset v3、8-case smoke/四类 6-case topic/24-case full、suite 隔离趋势/category/failure/Wilson、只读人工评审候选与不可变人工决定审计；Q3 已交付 run steps、Provider token/unknown、本地聚合与 UTC 时间趋势，费用仍待。Q4 自动路由及更多协议尚未完成。

## 分阶段

| 阶段                 | 状态         | 交付                                                                                                                                           | 完成门禁                                                                                 |
| -------------------- | ------------ | ---------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| Q1 可验证 citation   | 已完成       | schema 060、strict control parser、allowlist 校验、五状态、历史 API/UI                                                                         | 越权/重复/缺失/空引用/控制块隐藏测试通过                                                 |
| Q2 离线/本地质量评测 | 当前范围完成 | 内置 scorer；ADR-013 Actor；ADR-014–020 数据集/趋势/category/failure/Wilson/候选；ADR-021 smoke/full；ADR-022 人工决定审计；ADR-023 四类 topic | 6/8/24-case 代码所有套件可运行且不留正文；仅 full 可候选；精确证据上的人工决定可追加审计 |
| Q3 用量与步骤追踪    | 部分完成     | ADR-012/schema 061–062：完整步骤、字节/耗时、双协议原始 token/unknown、会话/Provider 聚合；ADR-024 UTC 趋势与按需 UI                           | 无正文步骤、原始 token、聚合与 1–30 天趋势已验收；费用配置待后续                         |
| Q4 路由与协议扩展    | 待后续       | 显式手动策略后再评审自动 Provider 路由、更多协议                                                                                               | 费用/隐私/降级可预览，不静默换 Provider                                                  |
| Q5 编排/子代理       | 未授权       | 只读建议之外的编排和子代理需单独 ADR                                                                                                           | 不绕过 Agent Runtime、Task 验收或业务写门禁                                              |

## Q1 固定契约

1. Citation block 只在显式知识上下文存在时允许，位于 task/memory block 之后、self-check 之前。
2. 模型只提交 `chunk_ids`；Sidecar 用本次 allowlist 重建所有来源和位置字段。
3. `validated` 必须有 1–3 项；`no_evidence / missing / invalid` 必须为空；无知识时 API 派生 `not_requested`。
4. Raw citation block 不进入 message、generation、历史 prompt 或 Web 显示。
5. Citation 错误不伪造来源，也不隐式附上全部上下文；以质量状态保留回答。

## Q1 验证

- schema 059→060 保留既有消息；只允许 completed assistant 保存受约束 snapshot。
- Go parser 覆盖合法顺序、空数组、缺失、非法 JSON、未知字段、越权 UUID、重复、多块和未闭合 tail。
- AI6 金链覆盖 preview→chat→clean answer→validated snapshot→history，引用 metadata 来自 Sidecar，正文不复制。
- Web 严格解析引用状态与 items；历史展示来源卡，缺失/非法/无答案给出不同反馈；流式完整/半截块不显示。

### 本轮验证记录

- 完整 Web 门禁通过：121 个测试文件、1,109 项测试，typecheck 与 production build 成功；仅有既有 bundle size warning。
- schema 060 迁移与完整 database 包通过；AI citation/AI6 金链 race 专项及 `go vet ./...` 通过。
- `go test ./internal/aieval -count=1` 通过：dataset v3 的 24 个代码所有 case good observations 全绿，中英各 12、四类各 6；坏观察、非法版本、超 32 个 case 和非法数据集稳定失败。
- Harness/API/schema 061 覆盖模型轮、工具、自检、citation、persistence、32 tool-call 上限、success/failure/cancel/recovery、旧 generation summary 回填、消息链接和无正文 steps API；Web 覆盖禁止字段 fail-closed 与按需时间线。
- schema 062/ModelClient/API/Web 覆盖 OpenAI 根 usage、Anthropic start/delta 合并、完整非负整数门禁、缺失/部分 usage 保持 NULL、root 聚合、token_source=provider 与 UI 原值展示；请求不强制 stream_options，不计算费用。
- 完整 Go 仍只有已在 untouched AI branch 独立复现的两条 Automation Event Delivery 基线失败，其他包通过。
- 文档 52 个 Markdown 文件及本地链接通过。
- 真实本地 Sidecar/schema 060 + Web 视觉检查通过：validated 来源卡和 missing 警告清晰；窄宽度检查发现并修复“上下文”按钮换行。未调用模型，临时服务已停止且数据已移入废纸篓。
- 真实本地 Sidecar/schema 061 + Web 运行时间线检查通过：根步骤、模型轮、自检、引用校验、本地保存、bytes/duration 和无正文提示在展开/收起状态均清晰；临时服务已停止且数据已移入废纸篓。
- 真实本地 Sidecar/schema 062 + Web Provider usage 检查通过：完整 usage 原值显示 `321 in / 45 out`，缺失 usage 明确显示 unknown；根步骤与模型轮只显示 Provider 原始 token，保存步骤不伪造 token，费用仍不计算。临时服务已停止且数据已移入废纸篓。
- `GET /api/v1/ai/usage-summary` 已覆盖空范围、session/provider 双过滤、终态/活动状态、Provider usage/unknown 覆盖、原始 token/bytes/duration 合计、Provider 稳定分组、非法/不存在过滤对象及禁止字段；Web 严格重算分组并按需展示当前会话用量。
- 真实本地 Sidecar/schema 062 + Web 聚合面板检查通过：会话头部显示 `1/2 有 token`，面板显示 `321 in / 45 out`、累计 `1.6 s` 与 `1 次未返回 usage`，并明确“不估算 token，也不计算费用”；临时服务已停止且数据已移入废纸篓。
- ADR-013/schema 063 覆盖 4-case 全通过、质量失败但 Run succeeded、远程/未就绪/同 Provider 并发拒绝、Idempotency-Key、运行取消、启动 interrupted、Provider 删除保护、终态历史清理、Result 不可更新和正文禁存；Web 覆盖严格响应、显式启动、进度/结果、failure code、token/unknown 与删除确认。
- 真实本地 Sidecar/schema 063 + Web 设置页检查通过：本地 Provider 卡显示显式评测入口，历史摘要显示 4/4、3 通过/1 未通过；展开后四类 case、稳定失败说明、citation 数、Provider token/unknown 和二次删除确认清晰，未展示问题、知识正文或模型回答。本次只用受控种子验证 UI，未调用真实模型；临时服务已停止且数据已移入废纸篓。
- ADR-014 本地趋势 API/Web 覆盖单事务状态计数、succeeded-only 质量、Provider ID/名称快照/模型快照/dataset version 四字段分组、全通过 Run/原始 case 数、最近 N 条正序趋势、Provider/limit 校验、分组/时序/禁止字段 fail-closed 和缓存失效；不新增 schema 或线上服务。
- 真实本地 Sidecar/schema 063 + Web 趋势检查通过：alpha/beta 模型快照分别显示 88%（7/8）与 50%（2/4），最近完整 Run 以 100%→75%→50% 条形图正序展示；运行失败 1、取消 1 只显示在状态区，不进入质量比例。本次使用受控种子验证 UI，临时服务已停止且数据已移入废纸篓。
- ADR-015 category API/Web 覆盖 succeeded Run→Result JOIN、四字段父组 + category 唯一分组、固定顺序、passed/failed/total、父组归属和三项合计；设置页按模型/dataset 显示四类精确分子/分母与条形图。failed/cancelled/active 不进入，不新增 schema、正文或线上服务。
- 真实本地 Sidecar/schema 063 + Web category 检查通过：alpha 组四类显示 2/2、2/2、2/2、1/2，beta 组显示 1/1、1/1、0/1、0/1；类别卡与总体组/时间趋势层级清晰且分母始终可见。本次使用受控完整 Run/Result 种子验证 UI，临时服务已停止且数据已移入废纸篓。
- ADR-016 failure-code API/Web 覆盖 succeeded+failed Result、SQLite json_each、代码白名单、四字段父组 + code 唯一、`affected_cases=COUNT(DISTINCT result)`、occurrences 重复保留、父组失败上界/最小覆盖和禁止字段；设置页显示稳定标签及两种精确计数，不新增 schema、failure detail 或线上服务。
- 真实本地 Sidecar/schema 063 + Web failure-code 检查通过：alpha/beta 分组分别显示稳定中文失败标签；beta `REQUIRED_PHRASE_MISSING` 精确显示“影响 2 case · 出现 3 次”，验证同一 case 的重复失败码不会冒充更多唯一失败 case。页面未展示问题、资料、模型回答或 failure detail；本次使用受控种子，未调用模型或线上服务。
- ADR-017/schema 064 覆盖 v63→v64 逐列保留 dataset v1 Run/Result、正整数版本、v2/12 新 Run、Provider/FK/单活动索引/token/不可变 trigger，以及 v49→64、v63→64 便携业务导入兼容；Actor 金链完成 12/12，质量失败仍保持 Run succeeded。数据集分布固定为中英各 6、四类各 3，不保存正文或调用线上服务。
- 真实本地 Sidecar/schema 064 + Web dataset 兼容检查通过：同一 Provider/模型的 v1 历史显示 `4/4`、每类 `1/1`，v2 显示 `12/12`、每类 `3/3`，总体、趋势、category 与历史卡均按版本分开。视觉检查先发现并修复 Web 仍硬编码 dataset v1 的失败关闭缺口；修复后正整数版本严格解析与 v1/v2 兼容测试通过。临时服务已停止，数据已移入废纸篓。
- ADR-018 API/Web 覆盖固定 95% Wilson basis-point 公式、0/1 与 1/1 边界、总体/category 原始计数重算、伪造区间/证据标签/置信口径失败关闭，以及 1/2/3+ 次完整 Run 的单次/有限重复/重复观察。无迁移、物化表、自动阈值或线上服务。
- 真实本地 Sidecar/schema 064 + Web Wilson 检查通过：v1 单次 `4/4` 显示 `95% 51.01%–100% · 单次观察`；v2 三次合计 `33/36` 显示 `91.67%`、`95% 78.17%–97.13% · 重复观察`，四类各保留分母与区间。桌面和 390 px 窄屏层级、换行与滚动正常；页面明确“不代表发布许可”。临时服务已停止，数据已移入废纸篓。
- ADR-019 API/Web 覆盖 advisory policy 的当前 dataset、最少 3 Run、总体下界 8000、category 下界 6000、四类必需类别、三项严重 failure code，以及 Provider version min/max 单一性；旧 dataset、Run 不足、配置版本混合、总体/类别不足、严重失败和 review candidate 都由稳定状态/有序原因表达并由 Web 重算。无迁移、自动 Provider/聊天/业务动作或线上服务。
- 真实本地 Sidecar/schema 064 + Web 人工评审候选检查通过：candidate-model 三次全通过显示“可进入人工评审”；legacy-model 显示“不是当前 dataset v2”；mixed-model 显示“包含多个 Provider 配置版本”；weak-model 同时完整显示总体 80%、类别 60% 与严重失败原因。桌面与 390 px 窄屏卡片均不截断原因，颜色之外保留完整文字；页面明确候选不代表发布许可。临时服务已停止，数据已移入废纸篓。
- ADR-020 dataset v3 覆盖 24/24 离线 scorer 与真实 Actor 请求序列，中英各 12、四类各 6；新 Run 固定 v3/24，v1/v2 summary 历史保持独立并标 OUTDATED，当前 v3 少于 3 Run 标 RUN_COUNT_LOW。无 schema、用户正文、并发模型调用或线上服务。
- 真实本地 Sidecar/schema 064 + Web dataset v3 检查通过：legacy-v2 `12/12` 显示“不是当前 dataset v3”；v3-single 显示 `24/24`、每类 `6/6` 与“少于 3 次完整运行”；v3-candidate 三次显示 `72/72`、每类 `18/18`、总体下界 `94.93%` 与“可进入人工评审”。桌面和 390 px 窄屏分母、版本、区间、状态与趋势均清晰；临时服务已停止，数据已移入废纸篓。
- ADR-021/schema 065 覆盖旧 Run 默认 full、suite CHECK/索引、默认/显式创建、未知 suite、smoke 8-case 中英各 4/四类各 2、full 24-case、同 Provider 单活动、suite 进入幂等摘要，以及 parent/category/failure/trend 五字段分组。Web 严格解析 suite/policy，切换 suite 轮换幂等 key；smoke 固定 SUITE_NOT_ELIGIBLE。
- ADR-022/schema 066 覆盖精确质量组证据重算、三值人工决定、必填理由、内置 owner Actor、Idempotency-Key、stale/not-found、不可更新/删除、筛选分页与禁止正文；设置页提供组内表单和只读历史。记录只审计，不改变 Provider、聊天或业务状态，并排除便携业务导出。
- ADR-023/schema 067 覆盖 grounded/no-evidence/prompt-injection/conflicting-sources 四个代码所有 6-case suite，每类中英各 3；受保护共同重建 Run/Result/Review 并恢复外键、索引和不可变 trigger。Actor、summary、人工审计和 Web 均支持 topic；smoke/topic 固定 SUITE_NOT_ELIGIBLE，设置页不开放任意 case。
- ADR-024 无迁移地扩展 usage summary：`trend_days=1..30` 以 terminal root completed_at 生成连续 UTC 零填充点；每点沿用 Provider usage/unknown、token、bytes/duration 口径并固定 active=0。Web 严格校验日期/连续性/窗口/禁止字段，当前会话可切换 7/30 天，generation 结束时失效全部窗口缓存；不配置价格或费用。
- ADR-022 完整 Web 门禁通过：122 个测试文件、1,118 项测试、typecheck 与 production build 成功；仅保留既有 bundle size warning。schema 066 database 包、人工决定 API race 专项、`go vet ./...`、53 个 Markdown 文件/本地链接、gofmt 与 diff 检查通过。完整 Go 仍只有已在 untouched AI branch 独立复现的两条 Automation Event Delivery 基线失败，其他包通过。
- 真实本地 Sidecar/schema 066 + Web 人工决定审计检查通过：三次 full/72 case 候选组显示总体与最低类别 Wilson、组内三值决定/必填理由和只读历史；390×844 窄屏保留分母、理由、Actor 与“仅审计”边界。窄屏端到端保存从 1 条追加到 2 条，表单关闭且新理由立即出现；未调用真实模型或线上服务，临时环境已清理。
- 真实本地 Sidecar/schema 065 + Web 分层套件检查通过：ready 本地 Provider 同时显示“快速质量检查”“完整质量评测”；同模型/dataset 的三次 full 为 `72/72` 并可进入人工评审，三次 smoke 为 `24/24` 但明确“快速套件只用于诊断”。趋势、category 与历史分别显示 24/8、18/6 分母；桌面和 390 px 窄屏正常。临时服务已停止，数据已移入废纸篓。
- ADR-023 完整 Web 门禁通过：122 个测试文件、1,121 项测试、typecheck 与 production build 成功；仅保留既有 bundle size warning。schema 067 完整 database 包、专题 Actor/summary/review/import API race 专项、`go vet ./...`、54 个 Markdown 文件/本地链接、302 个 Go 文件格式与 diff 检查通过。完整 Go 仍只有两条已确认 Automation Event Delivery 基线失败，其他包通过。
- 真实本地 Sidecar/schema 067 + Web 专题套件检查通过：设置页显示四项 6-case 选择、显式运行按钮及“不形成候选资格”；受控 prompt-injection 组显示 `6/6`、单类别 `60.97%–100%`、`SUITE_NOT_ELIGIBLE` 和追加式 `needs_more_evidence` 审计。桌面与真实 390×844 视口的选择器、按钮、分母、理由和历史均清晰；未调用真实模型或线上服务，临时环境已停止并移入废纸篓。
- ADR-024 完整 Web 门禁通过：122 个测试文件、1,123 项测试、typecheck 与 production build 成功；仅保留既有 bundle size warning。UTC trend API race 专项、`go vet ./...`、55 个 Markdown 文件/本地链接、gofmt 与 diff 检查通过。完整 Go 仍只有两条已确认 Automation Event Delivery 基线失败，其他包通过。
- 真实本地 Sidecar/schema 067 + Web 用量趋势检查通过：受控 root steps 在 7 天窗口显示 `09/03`、`09/07`、`09/09` 的终态运行、两天 Provider usage 和一天 unknown，中间零日未省略；切换 30 天后窗口连续扩展且累计 `200 in / 50 out`、`9.0 s` 不变。390×844 视口隐藏会话 rail 但提供会话选择/新建控制，趋势浮层与主栏等宽，30 天列表限高滚动；费用说明仍明确“不估算 token，也不计算费用”。临时环境已停止并移入废纸篓。

## Q2 建议数据结构

首个制品已放在 `internal/aieval/testdata/knowledge_quality_cases.json`，不进入用户数据库，记录：

- case ID、语言、问题、允许的 knowledge chunk fixture；
- 必须包含/不得包含的事实、是否应回答 `no_evidence`；
- 允许 citation chunk IDs 与禁止来源；
- 提示注入和冲突来源的期望防御行为；
- Provider/protocol/model 仅作为运行维度，不把一次外部模型输出当稳定真值。

`internal/aieval` 已提供严格数据集校验、单 case `Evaluate` 和稳定排序的 `EvaluateAll`。当前四类 good observations 全通过；坏观察会稳定报告 case mismatch、控制块泄露、必需事实缺失、禁止事实出现、citation 状态/allowlist/重复/数量/集合错误以及 observation 缺失。

ADR-013 已实现真实**本地**模型 runner；ADR-014–020 已交付 dataset v3、趋势、category、failure-code、Wilson 和人工评审候选；ADR-021 已提供 8-case smoke 与 24-case full 并按 suite 隔离；ADR-022 已提供对精确证据快照的不可变人工决定与理由审计；ADR-023 已提供四个各 6-case 的 category 专题套件。同一个 case 可贡献多个原因，因此各 code affected 不得相加冒充唯一失败 case；重复同一 case 也不得当作新的事实覆盖。当前不开放任意 case 选择。任何自动动作仍需单独设计。
