# ADR-022：AI 本地质量人工决定审计

> 后续修订：[ADR-025](025-ai-reliability-confirmations-and-evaluation-identity.md) 接续运行预算/锁、确认事务/隐私、部分压缩进度及评测配置身份。本文保留当时决策与验证记录；当前行为以 ADR-025 和模块文档为准。

- 状态：Accepted，AI7-Q2 人工决定审计已实现
- 日期：2026-09-09
- 决策范围：人工决定、证据快照、不可变审计、无业务副作用
- 相关文档：[ADR-019](019-ai-local-quality-advisory-readiness.md)、[ADR-021](021-ai-local-quality-tiered-suites.md)、[AI 助手](../modules/ai-assistant.md)、[AI7 计划](../plans/ai-quality-gates.md)

## 背景

ADR-019 只给出建议性的 `review_candidate / needs_attention / insufficient_evidence`，它不能替用户作决定。实际试用本地模型时，用户仍需要记录“是否接受本机试用、为什么接受或拒绝、当时看到了哪组证据”。若只保存最终标签而不固化证据，后续新增 Run、切换模型或修改 Provider 后就无法解释旧决定；若决定直接启停 Provider、阻止聊天或写入业务对象，又会把质量观测错误升级为自动发布机制。

因此人工决定必须是对一个精确质量组快照的追加式本地审计。它可以记录用户判断，但不得修改评测事实、Provider、聊天或任何业务状态。

## 决策

### 1. schema 66 新增不可变本地审计表

`ai_evaluation_reviews` 保存：

- Provider ID、名称、模型、最小/最大配置版本快照；
- dataset version、suite、最后完成时间、Run/case 计数；
- 总体 Wilson 下界、最低 category 及其 Wilson 下界；
- readiness 状态、有序原因与命中的严重 failure code；
- 人工决定、必填理由、内置 owner Actor ID/名称快照和创建时间。

决定固定为：

- `accepted_for_local_use`：接受本机试用；
- `needs_more_evidence`：需要更多证据；
- `rejected`：拒绝使用。

理由去除首尾空白后必须为 1–1000 个字符。数据库 trigger 拒绝 UPDATE/DELETE；只能追加新记录，不允许覆盖历史判断。记录不依赖 Provider 外键，因此清理评测 Run 或 Provider 后仍保留当时的本地审计解释。

### 2. 创建时在同一写事务重算证据

`POST /api/v1/ai/evaluation-reviews` 要求客户端提交当前可见质量组的完整身份和可比较证据：Provider/模型快照、dataset/suite、Provider 版本范围、最后完成时间、Run/case 计数及 readiness 状态/原因。

Sidecar 在写事务内从 succeeded Run/Result 重新聚合总体、category、failure code、Wilson 与 ADR-019/021 readiness。任一提交字段与当前快照不同，返回 `409 AI_EVALUATION_REVIEW_STALE`，不写审计记录。不存在的组返回 `404 AI_EVALUATION_GROUP_NOT_FOUND`。创建支持 Idempotency-Key；同一请求重试返回同一记录，不同请求不得复用 key。

快照匹配只防止“依据已经变化仍误写旧结论”，不限制用户必须选择某个决定。即使是 smoke、旧 dataset 或 `needs_attention`，用户仍可留下拒绝或补证据的审计说明；系统不会把它变成发布许可。

### 3. 查询与 Web 交互

`GET /api/v1/ai/evaluation-reviews` 按创建时间倒序分页，可按 Provider snapshot 和 decision 筛选。响应不包含问题、知识正文、模型回答、reasoning、Provider URL 或 key。

设置页在每个质量组卡片提供“记录人工决定”：

- 默认建议只影响表单初值；候选组默认“接受本机试用”，其他组默认“需要更多证据”；
- 用户必须显式选择决定并填写理由；
- 保存成功后表单关闭并刷新审计历史；证据过期时刷新 summary 并要求重新核对；
- 历史展示决定、模型/dataset/suite/Provider version、证据计数、总体/最低类别下界、严重 failure code、理由、Actor 和时间；不提供编辑或删除。

界面持续显示“仅审计，不改变模型、聊天、Provider 或业务状态”。桌面和 390 px 窄屏都必须保留决定、理由与证据分母，不以颜色作为唯一信号。

### 4. 导出、备份和边界

- `ai_evaluation_reviews` 排除便携业务 JSON/ZIP 导出；自由文本理由不会进入业务迁移包。
- 一致性 SQLite 备份仍覆盖这张本地操作态表。
- schema 66 不改变任何便携业务表列；显式维护 v49→66、v63→66、v64→66 与 v65→66 兼容边，并按源 schema 校验其历史 `excluded_operational_tables` 清单。
- 审计写入不启用/禁用 Provider、不修改健康状态、不切换聊天模型、不创建 Task/Inbox/提醒/发票或其他业务事实。

## 威胁与控制

| 威胁                         | 控制                                                             | 验证                            |
| ---------------------------- | ---------------------------------------------------------------- | ------------------------------- |
| 在旧证据上记录新决定         | 写事务内重算精确组快照；不一致返回 stale                         | API 金链                        |
| 修改或删除不利决定           | UPDATE/DELETE trigger 全部拒绝                                   | 迁移测试                        |
| 人工决定被误当自动发布许可   | 三值只表示本地审计；无 Provider/聊天/业务写路径                  | API/UI 副作用测试               |
| 理由或模型内容进入便携业务包 | 审计表排除业务导出；响应无问题/回答/URL/key                      | 导出分类与 API/Web 禁止字段测试 |
| 重试重复写记录               | Idempotency-Key 绑定规范化请求摘要                               | API/hook 测试                   |
| 客户端接受伪造快照           | Web 重算总体 Wilson，校验状态/原因、严重 code、Actor、计数与分页 | Web API 测试                    |

## 明确不做

- 不自动启用、禁用、路由或升级 Provider；
- 不把 `accepted_for_local_use` 当成发布、生产或安全认证；
- 不允许编辑/删除历史决定，也不提供“当前唯一有效决定”覆盖语义；
- 不保存评测问题、知识片段、模型回答、reasoning、URL、key 或费用；
- 不调用线上服务，不新增云同步或团队审批；
- 本 ADR 不实现专题套件（后续由 ADR-023 交付）、自动回归调度或跨版本模型排名。

## 验收

- schema 65→66 创建表并保留既有数据；约束、Actor、JSON、不可变 trigger 和禁止正文列测试通过；
- API 覆盖候选/非候选记录、幂等重放/冲突、stale/not-found、筛选分页、禁止字段和零业务副作用；
- Web 覆盖严格解析、精确请求快照、失败重试 key 复用/输入变化轮换、表单必填、stale 提示与只读历史；
- 设置页桌面和 390 px 视觉检查通过；
- `go vet`、数据库/API 专项、完整 Web、文档链接和 gofmt 门禁通过；完整 Go 若仍只有已确认的 Automation Event Delivery 基线失败，须单独记录而不能归因于本 ADR。
