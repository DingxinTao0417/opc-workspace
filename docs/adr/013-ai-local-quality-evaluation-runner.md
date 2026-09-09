# ADR-013：AI 本地模型质量评测 Actor

- 状态：Accepted，AI7-Q2 本地运行闭环已实现
- 日期：2026-09-09
- 决策范围：显式本地模型评测、Actor 生命周期、无正文结果持久化
- 相关文档：[AI 助手](../modules/ai-assistant.md)、[AI7 计划](../plans/ai-quality-gates.md)、[ADR-011](011-ai-validated-knowledge-citations.md)、[ADR-012](012-ai-run-steps-and-local-metrics.md)

## 背景

AI7-Q2 当时已有 dataset v1 的 4 个代码所有中英 grounded、no-evidence、conflicting-sources 和 quoted prompt-injection case，以及完全离线、确定性的 scorer。此前只能由测试代码给 scorer 注入人工构造 observation，用户无法显式选择一个真实本地模型运行同一数据集，也没有排队、取消、恢复、历史和隐私边界。当前数据集规模已由 [ADR-020](020-ai-local-quality-dataset-v3.md) 升级为 v3/24；本 ADR 的 Actor 与隐私边界保持不变。

当前产品要求保持本地优先，不引入线上服务。真实模型评测因此只能调用用户已经登记、健康且位于回环地址的 `kind=local / protocol=openai_chat` Provider；不能把评测自动扩展到远程 Provider，也不能把模型回答或知识正文保存为评测历史。

## 决策

### 1. 只有用户显式触发的本地 Provider 可以运行

- 设置页只对 `kind=local` 且 `status=ready / health_status=healthy` 的 Provider 显示“运行本地质量评测”。
- 创建命令携带 Provider ID 与当前 version；Sidecar 在落库前重验类型、协议、健康和 version，并保存名称、模型、协议和 version 快照。
- 评测不读取用户会话、长期记忆、业务对象或用户知识库。输入只来自二进制内置、代码评审过的数据集 v1。
- 远程 Provider、未就绪 Provider 和同一 Provider 的第二个活动 Run 均返回稳定 409，不做自动降级或静默换模型。

### 2. 单邮箱 Actor 串行执行

- `aiEvaluationRunner` 是固定容量 32 的单邮箱本地 Actor；HTTP 创建只验证并持久化 `queued` Run，再投递不可变 Run ID。
- Actor 依次执行当前 embedded dataset 的 case，每个 case 最长 3 分钟；同一进程所有评测串行，避免多个本地模型请求争用 CPU、内存与 SQLite。首版 v1 为 4 个，ADR-017 为 v2/12，ADR-020 当前为 v3/24。
- Run 状态为 `queued → running → succeeded | failed | cancelled`。`succeeded` 只表示完整数据集执行成功；质量是否通过由 `passed_cases / failed_cases` 与每个 Result 决定。
- 运行中取消会设置持久 `cancel_requested` 并取消当前模型 context；尚未完成的 case 不伪造 Result。应用重启把遗留 queued/running Run 标为 `failed / AI_EVALUATION_INTERRUPTED`，不自动重放模型请求。
- 创建支持 Idempotency-Key 快照；同 key 同请求返回原 Run，不重复投递。每个 Provider 同时最多一个 queued/running Run，由数据库部分唯一索引兜底。

### 3. schema 063 只保存无正文事实

- `ai_evaluation_runs` 保存 Provider 快照、数据集版本、状态、case 计数、当前 case ID、取消标记、稳定错误码与时间戳。
- `ai_evaluation_results` 保存 case ID、语言、类别、passed/failed/error、稳定 failure code 数组、citation 状态/数量、耗时、字节和可选 Provider 原始 token。
- 数据库没有 prompt、question、chunk、content、answer、reasoning、Base URL 或 API key 列；API/Web 发现这些字段会失败关闭。
- Result 创建后禁止 UPDATE；用户只能对终态 Run 二次确认删除整个聚合，级联清理 Result。活动 Run 必须先取消。Provider 存在评测历史时禁止删除，清理对应历史后才能删除 Provider。
- 两张表属于 AI 操作态，排除业务 JSON/ZIP，包含于一致性 SQLite 备份；首版 v49 业务包兼容扩展至 schema 63，ADR-017 后延续至 schema 64。

### 4. 使用生产 citation parser 和确定性 scorer

- 每个 case 通过生产 Harness/ModelClient 调用本地 Provider，但工具、记忆、摘要、业务上下文均为空；知识输入只包含该 case 的代码所有 chunk。
- 模型回答只在当前进程内存中存在。Sidecar 先用 ADR-011 的 production citation parser 对本次 allowlist 校验并剥离控制块，再把清洁回答和服务器验证的 citation identity 交给 `aieval.Evaluate`。
- 只持久化 scorer failure code，不持久化 failure detail、必需/禁止短语、回答或知识正文。Provider token 仍执行 ADR-012 的全有/全无规则；unknown 不估算。
- 模型调用/协议失败写一个 `status=error` Result 并终止 Run；普通质量未通过不会把 Run 伪装成基础设施失败，后续 case 继续执行。

### 5. API 与界面

- `POST /api/v1/ai/evaluations`：显式创建本地评测 Run。
- `GET /api/v1/ai/evaluations`：按 Provider/status 分页读取无正文历史。
- `GET /api/v1/ai/evaluations/:id`：读取有序 Result。
- `POST /api/v1/ai/evaluations/:id/cancel`：取消 queued/running Run。
- `DELETE /api/v1/ai/evaluations/:id?confirm=true`：删除终态 Run 与 Result。
- 设置页显示排队/运行/终态、完成度、通过/未通过/错误数、当前 case、稳定失败原因、citation 数和 token/unknown；活动状态每 500 ms 本地轮询，支持取消和二次确认删除。

## 威胁与控制

| 威胁                         | 控制                                                     | 验证               |
| ---------------------------- | -------------------------------------------------------- | ------------------ |
| 评测意外调用远程服务         | 仅接受 ready/healthy local loopback Provider             | API/Web 测试       |
| 回答或知识正文进入历史       | schema 无相应列；响应 parser 禁止字段；只存 failure code | 迁移/API/Web 测试  |
| 重复点击启动多个模型任务     | Idempotency-Key + Provider 活动部分唯一索引              | API 测试           |
| 本地模型占满资源             | 单邮箱 Actor、逐 case 串行、3 分钟 case timeout          | Actor/取消测试     |
| 重启后自动重放不确定模型请求 | 活动态启动恢复为稳定 interrupted failure，不自动重排     | Recovery 测试      |
| 质量失败被误报为运行失败     | Run succeeded 与 Result passed/failed 分层               | scorer/API/UI 测试 |
| token unknown 被估算         | Provider 完整 usage 才保存；部分/缺失全 NULL             | 迁移/API 测试      |
| Provider 删除使历史失去归属  | 有历史时删除受控拒绝；终态评测可由用户显式清理           | API 测试           |

## 被拒方案

1. **自动定时评测**：会无提示消耗本地资源，也可能在用户工作时抢占模型。
2. **同时评测远程 Provider**：违反本轮本地服务边界，并产生外发与费用问题。
3. **保存完整回答用于人工查看**：扩大敏感正文、备份和删除面；首片只保留可重复的失败码。
4. **并发跑完所有 case**：本地模型资源不可预测；单邮箱串行更容易取消和诊断。
5. **质量不通过就把 Run 标 failed**：混淆模型质量与执行基础设施状态。
6. **模型自行选择 case 或知识**：评测集和 allowlist 必须由代码拥有，模型不能改变测量对象。

## 后果与后续

- 优点：用户可以在断网状态下对实际本地模型运行与 CI scorer 相同的固定数据集，结果可追踪、可取消、可删除且不保存回答。
- 代价：请求数随 embedded dataset 增长；首版最多 4 个，ADR-017 v2 最多 12 个，ADR-020 当前 v3 最多 24 个串行模型请求。结果只解释固定 case，不代表所有真实业务问题的质量。
- 后续：ADR-014–020 已交付同口径趋势、category、failure-code、dataset v3、Wilson 区间与人工评审候选；ADR-021–023 已交付分层套件、不可变人工决定审计与四类代码所有专题套件。继续增加数据集仍必须递增 dataset version 并保持旧结果可解释。远程评测、自动路由、自动调参和模型输出留存继续不在授权范围内。
