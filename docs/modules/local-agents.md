# 本地 Agent 执行模块

## 跨 Run 条件式延后启动（AH-07，2026-09-23，ADR-030 首片）

用户现在可以一次确认“任务 A 的产出满足条件后，再启动任务 B 的这一次执行”，而不必在 A 结束的那一刻在场。设计与边界见 [ADR-030](../adr/030-agent-run-start-gates.md)，要点如下：

- **仍是逐项人工确认**：`agent_run.start` 新增可选 `start_after_run_id`（前置 Run，必须属于不同任务且仍在排队/执行/待登记）、`start_after`（`submitted` 产出登记为任务提交，或 `accepted` 该提交被人工验收接受）与 `start_within_hours`（1–24，从确认时刻起算），三者必须同时出现，且不能与 `restart_of_run_id` 同用。确认卡照常展示 B 的完整冻结快照，另加“满足条件后由系统启动 · 不是现在启动”说明，执行勾选文案改为“同意在条件满足后由系统启动这一次执行”。
- **事实**：确认事务照常创建 `queued` Run，并在同一事务追加不可变事件 `agent_run_start_gated`（前置 Run、条件、截止时间）；放行写 `agent_run_start_gate_released`，终止写 `agent_run_start_gate_closed`（原因闭集：`predecessor_failed/cancelled/retained/not_accepted/unavailable`、`expired`、`run_not_queued`），终止时经既有取消命令把仍在排队的 B 转为 `cancelled`。无新表、迁移或 scope。
- **派发**：FIFO 查询排除门控未结束的 Run，Worker 认领事务再检查一次（放弃认领，不失败不重试）；放行后仍走 8+32 准入与认领前/启动前的冻结身份复核，等待期间任务/Agent/Provider 变化则按既有规则失败关闭。对账在 Run 终态、交付恢复、人工验收与审批决定提交后被唤醒，另每 30 秒扫描一次（`AgentRunGateScanInterval`），进程内串行；Sidecar 启动时先对账。维护写锁或待恢复时跳过。
- **规则**：前置 `interrupted` 视为失败、从不放行；前置被重试或按当前事实重启不转移门控；同时等待的门控最多 4 个；预览只冻结前置 Run/任务身份、第几次、条件与时限，前置的实时状态在预览与确认时重新校验，已满足（应改用普通启动）或已不可能满足时拒绝。前置产出不会成为 B 的输入。
- **读取与展示**：Run 列表、详情与 `workspace_agent_runs` 工具新增只读 `start_gate`（`predecessor_run_id/require/expires_at/status/close_reason`）；`agent_run_result` 回执同样携带。统一生命周期新增“等待前置执行”（queued + waiting）以及“未启动 · 前置条件未满足/等待超时”（cancelled + 关闭原因）；执行抽屉增加“启动条件”一栏；确认卡显示“已确认 · 等待前置执行”。

确定性证据：Go `TestAgentRunStartGate*` 覆盖决策表每一行、等待中不被 FIFO 与直接认领启动、提交后放行且不重复结算、前置失败与过期的取消路径、提案边界（缺字段/非法值/同任务/未知或不可满足前置/与 restart 同用）与 4 个门控上限；前端覆盖 `start_gate` 与 Run 状态一致性解析、提案/回执严格绑定、生命周期文案、确认卡与抽屉展示。真实 Provider 下的长链执行、Windows 桌面上长时间等待与重启后的实际体验仍属专项验收。代码证据：[门控与对账](../../services/sidecar/internal/api/agent_run_start_gate.go)、[提案与回执](../../services/sidecar/internal/api/ai_agent_run_actions.go)、[前端生命周期](../../apps/web/src/lib/agentRunLifecycle.ts)。

## 统一的 Agent Run 生命周期表达（AH-02，2026-09-23）

Run 在服务端由两列独立状态描述：`status`（`queued/running/succeeded/failed/cancelled/interrupted`）与 `output_delivery_status`（`not_ready/pending/submitted/retained`），合法组合由 schema 075 触发器固定；Submission 的人工审核状态是 Task 事实，只有部分读取（如 AI 动作回执、计划证据）携带。此前任务执行区显示“已成功”、执行抽屉显示“执行成功”、右侧 Agent 列表直接显示英文 `succeeded`，而这些 Run 的结果其实只是“已保留、未提交”；确认卡把 `running + pending` 写成“正在登记产出”，交接文本写成“结果已完成但等待登记”。

现在所有 Run 展示面共用 `agentRunLifecycle()`（`apps/web/src/lib/agentRunLifecycle.ts`），把服务端事实映射为一个闭集阶段：

| 服务端事实                                              | 阶段                | 显示                 | 需要人工处理 |
| ------------------------------------------------------- | ------------------- | -------------------- | ------------ |
| `queued + not_ready`                                    | `pending`           | 排队中               | 否           |
| `running + not_ready`                                   | `running`           | 执行中（或执行进度） | 否           |
| `running + pending`                                     | `recovery_required` | 结果待登记           | 是（勿重跑） |
| `succeeded + retained`                                  | `succeeded`         | 结果已保留 · 未提交  | 是           |
| `succeeded + submitted` + Submission ID，未读到审核状态 | `submitted`         | 已提交为任务产出     | 否           |
| 同上 + `pending_review`                                 | `submitted`         | 已提交 · 待人工验收  | 是           |
| 同上 + `changes_requested` / `withdrawn`                | `submitted`         | 已要求返工 / 已撤回  | 返工为是     |
| 同上 + `accepted`                                       | `confirmed`         | 产出已验收           | 否           |
| `failed + not_ready` / `interrupted + not_ready`        | `failed`            | 执行失败 / 执行中断  | 是           |
| `cancelled + not_ready`                                 | `cancelled`         | 已取消               | 否           |
| 其他任何组合、缺失 Submission ID 的 `submitted`         | `unknown`           | 状态无法确认         | 是           |

规则：`unknown` 不显示成功或失败，也不暗示可重试；只有带服务端 Submission ID 的交付才算“已提交”，只有同一 Submission 的 `accepted` 审核状态才算“已验收”（确认卡只在回执与实时 Run 的 Submission ID 一致时采用回执中的审核状态）。执行中断的说明会明确“中断前的模型调用是否产生费用无法确认”。状态徽标新增 `data-phase`，结果待登记/已保留/无法确认使用提醒色，不再沿用 `succeeded` 的成功绿色。

接入面：任务执行区、执行抽屉（徽标与历史下拉）、右侧“Agent 执行”列表与详情（新增一句自然语言说明）、续办队列 Run 行、计划内 Run 交接提示、`agent_run.start` 确认卡状态。服务端状态机、API、schema、scope 与 Runner wire 不变。确定性证据：`agentRunLifecycle.test.ts` 覆盖全部合法组合、审核状态、缺失 Submission ID 与 8 种非法组合；`AiAgentRunActionCard.test.tsx` 覆盖同/不同 Submission 的验收表达；抽屉、任务执行区、右侧工作区、续办队列与交接测试同步更新。开发页实测旧 retained Run 在续办队列与抽屉中均显示“结果已保留 · 未提交”，徽标为提醒色。

## 冻结执行身份之前的旧 Run 保持可读（AH-08，2026-09-23）

schema 074 为已存在的 `agent_runs` 行回填 `task_version`、`actor_version`、`adapter_version`、`provider_version`、`provider_config_version`、`execution_contract_version` 为 0，`assignment_assigned_at` 为空串（DB `CHECK >= 0` 允许）。前端解析器此前只接受契约版本 1–6，这些旧 Run 在任务执行区与执行抽屉中整组读取失败，显示“无法读取最新执行记录”，续办队列里的旧 retained/failed Run 也无法打开。

现在 `execution_contract_version = 0` 被视为“冻结执行身份之前的只读历史”：可以查看状态、产出登记、错误码与完整结果，也可使用既有“按当前事实重新执行”入口（它重新核对当前 Task/负责人/Provider，并不复用旧快照）。其余身份、生命周期与交付组合校验不变；越界版本（负数、大于 6、非整数）仍拒绝，旧记录携带返工上下文仍拒绝。精确重试与返工重试继续由 Sidecar 的显式版本白名单把关，旧记录不会因此获得新的执行能力。

无 API、schema、scope 或 Runner wire 变化。确定性证据：`agentRunDelivery.test.ts` 覆盖旧 retained/failed Run 的列表与精确详情解析、越界版本拒绝与旧契约返工字段拒绝；开发库中三条 074 之前的 Run 可从续办队列打开并显示“结果已保留，未提交”。代码证据：[Run 解析](../../apps/web/src/api/client.ts)、[074 迁移](../../services/sidecar/internal/database/migrations/074_agent_run_execution_identity.sql)。

## 适配器异常纳入 Agent Inbox（H5-G58，2026-09-23）

统一 `GET /api/v1/ai/inbox` 以 metadata-only 的 `adapter_issue` 项投影本地 Agent 适配器的终态健康/隔离失败（`blocked`/`unhealthy` 或 `unsupported`），左侧可打开“设置 → 本地 Agent”并可选空 scope 排查 handoff。不返回 executable/manifest/路径，不自动检查、启停或启动 Run；enabled 且已就绪、有意停用与未检查 unknown 不进入该来源。详见 [AI 助手 H5-G58](ai-assistant.md#agent-inbox-本地-agent-适配器异常续办h5-g582026-09-23)。

## 活动计划续办纳入 Agent Inbox（H5-G54，2026-09-23）

统一 `GET /api/v1/ai/inbox` 现在以 metadata-only 的 `continuation` 项投影保存会话中仍处于 `waiting`/`running` 的后台计划续办。项目包含原会话标题、续办状态与原因、已启动轮次/上限和过期时间；只读前端入口显示在左侧 Agent Inbox 的“后台续办”分组，可精确回到原会话计划，并可通过只含固定提示的 handoff 重新交给 AI 核对原计划事实。

该投影不携带 workspace grant、Provider 配置、工作区 JSON、计划正文或结果，不授予自动批准、重试、停止、修改或执行能力，也不继承原消息权限；不新增 schema/迁移、路由、scope 或 Runner wire。确定性 Go/前端回归覆盖 kind 过滤、严格字段解析、隐私排除、会话导航和只读 handoff；真实 Provider/桌面长期续办仍需专项验收。

## Agent Run 重试链的精确回看（H5-G53，2026-09-23）

任务执行详情现在读取服务端返回的 `parent_run_id`，在显示 attempt 的同时提供“打开重试来源”入口。点击只把同一 Task 的精确父 Run ID 重新交给执行详情抽屉，抽屉继续按原有精确 Task+Run 查询重读事实；找不到父 Run 时显示明确的精确读取错误，不用其他历史 Run 代替。该入口不重试、不恢复产出、不改变 Task 或授权，也不把 `parent_run_id` 当作子 Agent 关系；它只表示同一冻结执行的精确 retry 链。

无 API/schema/迁移、Runner wire 或权限变化；前端回归覆盖父 Run 精确导航及缺失来源不替代。真实 Provider/桌面长链体验仍需专项验收。

## Agent Run 统一续办入口（H5-G52，2026-09-23）

全局 Agent Inbox 现在以只读 `agent_run` 项投影需要人工关注的 Task Agent Run：活动 `queued`/`running`、失败/中断未被连续精确 retry 替代的 Run，以及 `pending`/`retained` 产出交付。已提交产出、仅取消且没有保留结果的 Run 不进入该来源；已验证的 restart 来源也从统一计数和分页排除，保留新 Run 自身事实。右侧执行页和 `workspace_agent_runs` 仍是更完整的 Run 账本，负责进度、模型和精确恢复/重试入口。

统一 Inbox 只携带 Run/Task ID、有限标题、attempt、状态和交付状态；不泄露输入快照、结果正文、文件、路径、Provider endpoint 或凭据，也不改变人工取消、恢复登记、重试和验收门禁。计划内 Run 行可将这组 metadata 交回 AI 对话，但必须重新选择 `agent_execution` 授权，不能继承上一条消息的 scope。无新 schema/迁移、Runner wire、scope 或自动调度；确定性 API/前端测试覆盖 retry 父项去重、取消排除、retained 保留、分页、内容排除及计划内 handoff。

## Task Agent Run 的有界 FIFO 调度（H5-G44，2026-09-23）

原生任务 Run、AI Agent Run 建议确认和精确重试共用同一创建事务容量检查。每个 Sidecar 上限为 8 个活动执行 Worker + 32 个额外排队 Run；只计 `status IN ('queued','running') AND output_delivery_status='not_ready'`。容量已满时，API 返回 HTTP 429 / `AGENT_RUN_QUEUE_FULL`，Run 创建和同事务的任务自动分派、AI Proposal 确认均回滚；前端保留可重试状态并告知用户。

调度器从 `agent_runs` 按 `created_at, id` 读取最旧 queued 项，最多启动 8 个 Worker。Worker 退出后继续补位；Sidecar 启动恢复先恢复遗留状态，再按同一 FIFO 启动 queued 项。等待队列是数据库中的 Run 事实，不另存进程内副本。此前版本已持久化、超过新上限的队列会保留并逐项排空，新准入在活动执行数低于 40 前失败关闭。`running + pending` 表示模型执行已结束、产出登记待恢复，不占运行容量且只走原恢复登记，不重新调用模型。此限制按单一 Sidecar 运行时执行；不自动重跑终态 Run、不扩大 Runner/文件/终端/浏览器能力，也不改变人工验收。无 schema、migration、scope、HTTP 路由或 Runner wire 变化。

确定性回归覆盖并发槽位竞态、FIFO 查询、队列满时 AI 提案留待确认且不写确认事件、pending 登记不占容量；真实 Provider、原生桌面及历史大队列运行体验仍待单独验收。代码证据：[准入和队列 Worker](../../services/sidecar/internal/api/agent_runs.go)、[队列回归](../../services/sidecar/internal/api/agent_run_queue_test.go)。

## Agent Run 的 Task Submission 审核状态投影（H5-G43，2026-09-23）

AI 只读 Run 列表、产出列表/详情及工作计划，可查看与该 Agent Run 同 Task、同 `submission_id` 的当前 Submission 状态（待验收、已接受、已要求返工、已撤回）。此字段是动态业务事实，不是 Run 状态；`succeeded + submitted` 仍只表示 Runner 结果已登记。模型结果不包含 Submission 正文、审核原因、审核人、文件路径或恢复暂存。精确关联缺失时计划不推断完成。原生任务审核成功提交后仅发送进程内、无载荷续办唤醒；它不启动/重试 Agent Run，也不替用户验收。无 Runner、数据库/schema、scope 或 API 路由变化；真实 Provider 和原生桌面验收仍单独进行。

## Task Agent Run 的瞬时阶段可见性（H5-G42，2026-09-23）

运行中的本地 Run 在既有只读 UI API 上可选返回 `{progress:{phase,elapsed_ms}}`：`preparing` 准备执行上下文、`calling_model` 运行受控模型调用、`registering_result` 校验并登记完整结果。阶段由 Sidecar 内存跟踪，最多 256 个活动项；Run 退出时清除，进程重启不恢复，容量已满或旧 Sidecar 时 UI 仍只显示原有 `running` 状态。沿用现有轮询，不新增持久步骤、数据库迁移或 Runner wire。

阶段字段只出现在 Task Run 列表、全局 Agent Run 列表/详情，以及原生 AI 右侧的会话委派摘要；AI `workspace_agent_runs` 工具结果不包含它。响应不含模型部分正文、推理、token、文件、路径、Provider 凭据或自动操作信息。内置 Runner 仍为单次非流式调用，只有收到并完整校验最终 manifest 后才进入既有成功/产出登记状态；阶段显示不是百分比，也不证明 Task 完成、提交已验收或 Provider 计费已结束。

验证覆盖进程内阶段的顺序/容量与清除、真实本地 Runner 子进程连接阻塞的模拟模型请求时三个原生 API 读取路径返回 `calling_model`，解除阻塞并完成提交后阶段消失；前端严格解析拒绝未知阶段、负数/非安全整数、额外正文及终态阶段，同时保留旧 Sidecar 兼容。该确定性测试不调用真实 Provider，不替代原生桌面/安装包验收。

## AI 父会话批量停止不改变 Agent Run（H5-G39，2026-09-23）

父会话右侧可一次选择并人工确认最多四个已确认委派的精确活动子 Generation。Sidecar 重新核验全部父子和 Generation 身份后，以运行时锁实现全有或全无的本地停止信号；不级联、不触碰本模块的 Task Agent Run、Task、Submission 或 Artifact。`parent_run_id` 继续只表示精确重试链。多子停止是 AI Session/Generation 能力，不扩大 `agent-runs/:id/cancel` 的作用范围，也不能保证远端请求、计费或操作撤回。

## 父 AI 会话的批量 wait-any / wait-all 不改变 Agent Run（H5-G36，2026-09-22）

父 AI 会话可在 `workspace_agent_children(view=wait)` 选择批量等待模式：默认 `wait_mode=any`，或等所有已确认子会话进入终态的 `wait_mode=all`。这是 AI Session/Generation 的 metadata-only 等待，不创建或改变 Task Agent Run、Submission、Artifact、`parent_run_id`，也不产生后台续办能力。

## AI 父会话批量回收子回复仍不代表 Agent Run 事实（H5-G35，2026-09-22）

父会话现可用 `workspace_agent_children(view=results)` 在一个只读快照内读取 1–4 个已确认子 AI 会话的首个回复页；这只回收不可信模型文本，不读取 Runner 输入/文件或 Provider 私密配置。它不会把子 AI Generation 转成 Task Agent Run/Submission/Artifact，也不改变 `parent_run_id` 重试关系。需要确认 Agent Run 状态、产出或验收时仍必须读取本模块原有的 Run/Submission 事实。

## AI 父会话停止子 Generation 与 Task Agent Run 分离（H5-G32，2026-09-22）

AI 会话右侧可以由用户两步确认，向其已确认委派的精确活动子 Generation 请求本进程内停止；H5-G39 也允许用户一次选择最多四项并对整批再确认。该请求属于 AI Session/Generation 生命周期，不使用 `/api/v1/agent-runs/:id/cancel`，不会停止 Task Agent Run、改变 Task/Submission、删除结果或证明远端 Provider 请求/计费已撤销；自动续办或自动取消级联仍未开放。`parent_run_id` 仍只属于 Task Agent Run 重试链，AI 子会话停止不会改写该模型。

## 子 AI 终态等待仍不代替 Task Agent Run（H5-G28，2026-09-22）

父会话现在可有界等待自己已确认的子 AI Generation 终态；其只读状态和终态通知都不能证明 Task Agent Run 已交付、Submission 已验收或 Task 已完成。Agent Run 仍须从独立账本重读；见 [AI 子任务等待](ai-assistant.md#子任务终态等待与计划唤醒h5-g282026-09-22)。

## 子 AI 回复读取不代替 Task Agent Run 事实（H5-G27，2026-09-22）

父会话的 `workspace_agent_children` 可在本条完整授权下读取已确认子 AI Generation 的首轮回复，但回复中的“Run 成功”“产出已提交”仍只是模型文字；Task Agent Run 的状态、交付、Submission 与验收必须从原有 `workspace_agent_runs`、`workspace_task_submissions` 和精确工作台记录重读。新工具不读取 Runner 输入或文件产出、不修改 `parent_run_id`、不代替人工验收；见 [AI 助手 H5-G27](ai-assistant.md#父智能体读取子任务进展与首轮回复h5-g272026-09-22)。

## AI 会话树与 Task Agent Run 继续分离（H5-G26，2026-09-22）

右侧“Agent 执行”现在同时展示 AI 父子会话导航与原有 Task Agent Run 委派账本。父子关系只由 AI `agent_delegate.spawn` 的已确认 Proposal 和子 Generation 产生；Run 列表仍由 `agent_run.start/retry` 的回执和真实 `agent_runs` 产生。两种身份不能互换，`parent_run_id` 不用于会话树，打开子会话也不会自动创建、停止或验收 Task Agent Run。只读接口与边界见 [AI 助手 H5-G26](ai-assistant.md#父子会话的只读导航与状态h5-g262026-09-22)。

## AI 子会话委派与 Agent Run 的边界（H5-G25，2026-09-22）

AI 助手现在能通过独立人工确认创建一层保存的子对话，并由主聊天 Harness 在进程内启动一次子 Generation。这是 **AI Session/Generation** 的委派，不是本模块 `agent_runs` 表中的父子执行关系。子对话若想启动受控 Task Agent Run，仍须在自己的单条授权下提出原有 `agent_run.start/retry`、由用户另行确认，经过原 Runner 身份冻结、文件许可、交付和 Submission 验收链。委派本身不创建 Task、Agent Run 或产出，也不自动验收。

本模块的 `parent_run_id` 仍仅表示同一冻结执行的精确重试；不保存子会话身份、不继承父对话范围，也没有 Run 级联取消、自动重试或自动分派。H5-G23 的会话委派账本继续只描述已确认的 Agent Run Proposal，不能把 AI 子 Generation 混作 Agent Run。AI 子 Generation 的单项人工停止见 [H5-G32](ai-assistant.md#父子会话的显式停止请求h5-g322026-09-22部分实现)，但它不控制本模块的 Run。API v1/schema079 和 Runner wire 不变；见 [AI 助手 H5-G25](ai-assistant.md#受确认的有界子智能体委派h5-g252026-09-22)。

## 多 Run 原子停止请求（H5-G24，2026-09-22）

`agent_run.cancel_many` 把 1–8 个不同 Task 的精确活动 Run 放入一张人工确认卡。每项都绑定规范 Run ID、Task ID 和预期 Task version，预览从数据库冻结 Task 标题、版本、Run 状态、attempt、Provider ID 与模型；重复 Run/Task、额外字段、终态 Run 或不一致身份失败关闭。前端严格复核预览和回执顺序，用户必须勾选一次跨任务停止声明才能确认。

确认路径先在同一事务内重新生成全部预览，再逐项调用既有取消状态机。任一项漂移或写入失败会回滚整批，包括工作流事件；只有事务提交成功才调用对应 Worker 的内存取消函数。排队 Run 可在事务内直接进入 `cancelled`，执行中 Run 先登记取消请求，之后由权威生命周期事实进入终态；因此批量计划步骤会在全部 Run 实际为 `cancelled` 前保持未满足。重复确认重放同一不可变回执，不重复写入状态或事件；对仍在运行的 Worker 只重复发出幂等取消通知。

批量结果 ID 仍是 Proposal ID、version 固定为 1，结果只公开有序的 `{run_id, task_id, status}` 和 `/ai?workspace=agents`；不公开输入快照、结果正文、文件、Provider endpoint 或凭据。该动作不取消 Task、不删除产出、不保证撤回远程请求或费用，也没有定义父子 Run。`parent_run_id` 继续仅表示精确重试；真正的子 Agent spawn/send/wait/interrupt 和沿委派树传播取消仍待后续。API v1、schema079、Runner wire 与现有 scope 不变。

## 当前会话委派账本（H5-G23，2026-09-22）

`GET /api/v1/ai/sessions/:id/delegated-runs` 现可按一个保存会话读取已经人工确认并产生 Agent Run 结果的 `agent_run.start/retry` 回执。服务端只联结同会话 Generation、不可变 Proposal `result_id`、真实 Run 与 Task 标题，并可从该会话最新计划修订投影步骤 ID/标题/替代关系。分页、交付状态筛选、会话内 active/pending/unavailable 计数在同一只读事务完成；当前对话有活动或待登记 Run 时前端继续有界轮询。

该列表沿用全局 Run 的 metadata-only 摘要，不返回结果正文、输入快照、文件引用、执行 Provider 配置或审批内容。Run 已随终态 Task 删除时保留 `run:null` 的历史回执；一个 Run 出现多个同会话确认来源、Proposal 指纹/动作损坏、真实 Task 不匹配或最新计划无法核验时整次读取失败关闭。右侧“Agent 执行”默认展示当前会话委派，也保留全局账本和待登记入口；只有可用 Run 才能进入既有精确详情。

会话委派来源不是执行父子关系。`parent_run_id` 仍只表示原冻结事实的直接精确重试。H5-G23 的只读接口本身不创建子对话；后续 H5-G25 仅在 AI 会话层新增一层受确认 spawn，仍无通用 send/wait/list/interrupt、共享消息邮箱或自动取消级联。H5-G32 允许用户从父会话右侧单独停止一个精确、已授权的活动子 Generation；该能力不改变 Run、Submission、Task、scope、Runner wire 或调度，API v1/schema079 无迁移。

## 执行事实提交后的续办通知（H5-G22，2026-09-22）

Agent Run 的关键耐久变化现在会在事务提交后通知已经存在的 AI 续办协调器：原生取消或精确重试入队、待登记产出恢复、启动前身份失败、执行中断，以及成功/失败/取消/`output_pending` 终态落盘。AI 审批事务也在确认或拒绝真正提交后通知。通知只是一个进程内信号，不包含 Run ID、结果、文件、Provider、凭据或授权；重复变化可合并，协调器必须重新查询数据库，不能把通知本身当成执行或交付事实。

并行 Run 的正确性不依赖通知顺序。任一 Run 完成触发的扫描仍会检查计划全部相关生成和兄弟建议；只要另一个 Run 仍活动或有待确认/待登记项，就不会开始下一模型轮次。最后一个阻塞消失后，已有且未过期的 H5-G1 续办租约可立即响应；没有租约时不创建租约或模型调用。3 秒轮询保留为安全兜底，应用未运行时没有系统级后台唤醒。

这项变化没有给 Runner 增加工具、消息邮箱、子代理 spawn/send/wait、父子取消传播或文件系统权限，也不改变同一 Task 唯一活动 Run、人工执行/文件确认和 Submission 验收规则。API v1、schema079、执行输入和管道协议不变。

## 并行执行计划的全量等待状态（H5-F9，2026-09-22）

一个保存会话的计划可同时关联不同 Task 的多个 Agent Run；数据库仍以 `ux_agent_runs_task_active` 保证同一 Task 只有一个 `queued/running` Run。续办检查现扫描当前计划涉及的所有生成及其同轮兄弟建议，不会因第一个 Run 已终态就忽略另一个仍在 `queued/running`、`output_pending` 或待人工确认的建议。最高优先级原因继续用于现有状态机，新增的阻塞摘要同时返回每类数量和已核验的本会话 generation ID。

主对话会一次显示所有阻塞类别及全部相关回执组入口；Run ID、输入快照、结果正文、文件和 Provider 信息均不进入该摘要。后台续办仍只在所有相关阻塞消失后才可能进行下一轮，且仍受原有最大轮数、期限、冻结 Provider/计划身份和人工预授权约束。该能力不是 parent/child 子代理模型，不新增 delegation depth、自动 spawn、跨 Task 取消传播或共享上下文；`parent_run_id` 仍只表示精确重试链，不能当成委派父子关系。API v1/schema079/Runner wire 不变。

## 计划、执行与人工验收的精确往返（H5-F8，2026-09-22）

Agent 计划步骤的真实回执现在按服务端已核验 route 区分下一站：精确 Run 地址显示“查看 Agent 执行”，精确 Submission 地址显示“查看待验收提交”，其他安全工作台地址仍使用通用标签。标签不根据模型措辞、action 名或状态猜测；只有通过严格路由解析的 `/tasks/<task>?agent_run=<run>` 与 `/tasks/<task>/submissions/<submission>` 才显示专用含义，并由前端附加当前保存会话的返回身份。

智能体右侧“子智能体”精确 Run 详情也补齐产出登记出口：`submitted` 且带真实 Submission ID 时明确提示“执行成功没有代替人工验收”，并直达该批次；`retained` 显示固定保留原因，`pending` 继续只提供登记恢复，三者不会相互降级。此入口只导航到现有人工验收页，不自动接受/退回 Submission、不完成 Task、不控制 Run，也不把产出正文或右栏内容再次发送给模型。无新 scope、HTTP API、数据库迁移、Runner wire 或后台调度。

代码证据：`apps/web/src/api/aiWorkPlan.ts`、`AiWorkPlan.tsx`、`WorkspaceResources.tsx`。确定性测试覆盖主计划卡和右侧计划镜像的严格目的地标签、保存会话返回地址，以及精确 Run 详情到 Submission 的往返；真实桌面路由和人工验收体验仍待专项验收。

## 已提交文件产出到项目文件的人工桥接（H5-F7，2026-09-22）

成功且已经登记为 `submitted` 的单文件或有界多文件 Agent Run，现在可由用户把其中一份 UTF-8 文件产出带到智能体右侧“文件”工作区，选择一个现有文本文件作为目标并查看完整替换差异。入口同时出现在 Task 执行列表、执行过程抽屉和右侧“子智能体”精确 Run 详情；第三种入口可在智能体模式的当前工作区内直接切到“文件”标签，不必先离开右栏。纯文本、`retained`、`pending`、失败、缺少 Submission/Artifact 身份、载荷与冻结 output contract 不一致、含 NUL 或单份超过 32 KiB 的结果不形成候选。多文件结果仍逐份选择，不会批量写入。

- 候选只存在于当前 WebView 内存，绑定 Run/Task/Submission/Artifact、attempt、文件序号、名称、MIME 和完整正文。选择候选只展开右侧文件面板，不自动选目录、目标路径、批准或执行；目标路径不回传 Runner、Sidecar 或模型。
- 用户打开一个未截断的现有文本文件后，须另点“准备替换当前文件”；前端先精确重读来源 Run 并逐项核对提交身份、attempt、契约序号、名称/MIME 和完整正文，再建立不超过 32 KiB 的原生目标基线。界面展示完整差异和前后字节数，可先只读复核；切换候选、目标冲突、过期或关闭会释放旧基线，一次审查只绑定一个目标。真正提交前再次做同样的来源重读；读取失败只阻止操作并允许重试，事实不符会释放基线，二者都发生在原生命令之前，不能误报“结果未知”。
- 真正写入复用桌面 `workspace_file_edit_apply` 的既有产品门禁：新的规范 operation ID、页面勾选、独立 Win32 完整审查、按次 UAC、helper 磁盘/SACL/恢复记录/无覆盖替换与同 operation ID 终态回执。普通开发构建因没有绑定 helper 只可审查；取消消费旧审查，结果未知或需恢复时禁止重复提交并引导核对“文件恢复”。
- 这条桥接不改变 Agent Run、Submission、Artifact 或 Task 的事实：`submitted` 仍只表示待人工验收，写入项目文件既不接受 Submission，也不完成 Task；项目文件写入成功也不会反写 Runner 结果。它没有新增执行协议、模型 scope、任意文件系统、Shell、Git、终端或后台自动写入能力。

代码证据：`apps/web/src/store/agentRunFileApply.ts`、`TaskAgentRunsSection.tsx`、`AgentRunDrawer.tsx`、`WorkspaceResources.tsx`、`WorkspaceFiles.tsx`。前端确定性测试覆盖候选资格、单/多文件解析、迟到快照释放、完整差异、复核/提交参数、原生回执状态及三处 Run 入口；测试不弹真实 UAC、不调用已提权 helper、不修改用户文件。API v1、schema079、Runner wire 和恢复记录格式不变；真实发行安装包的 UAC/SACL/断电恢复仍须专项验收。

按当前事实重新执行的续办去重（H5-E38）：`attention=1` 不再重复计入已被真实新 Run 替代的失败、中断或结果保留来源。与精确 `parent_run_id` 重试不同，这条关系只在唯一 `agent_run_restarted` 事件与新旧 Run 冻结身份、执行哈希、时间均经服务端核验后成立；损坏证据使读取失败关闭，不能仅凭事件 JSON 隐藏来源。过滤先于 `meta.total` 和分页，完整 Run/Task 历史不删除。新 Run 若随后精确重试，旧来源与中间尝试均保留历史，但需处理投影只计当前尝试。无自动执行、文件/Provider 授权继承、交付或验收规则变化，API v1/schema078 不变。

需处理 Run 列表的精确重试去重（H5-E37）：`GET /api/v1/agent-runs?attention=1` 在计数和分页前排除已有同 Task、连续 attempt、`parent_run_id` 直接子 Run 的失败或中断父 Run；新尝试仍按自身状态进入续办。无 `attention` 的账本、Task Run 历史和精确详情不删旧记录。按当前事实重新执行属于另一条经核验事件连接的链，不从未核验载荷推断并合并。该变化不自动发起 Run、不改变冻结身份、交付或验收归属，API v1/schema078 不变。

H5-G1 的有界后台续办只推进聊天计划：等待真实 Run 终态和 pending 产出登记后，使用新的 generation 查询事实、提出后续建议。它不自动批准 Run、重试、恢复登记或验收；停止授权仅取消其聊天生成，已经独立人工批准的执行继续走本模块生命周期。schema077新增的是会话授权/轮次操作态，不改变 v1–v6 执行快照、文件权限或Run交付契约。见 [AI续办边界](ai-assistant.md#有界后台续办h5-g12026-09-21)。

schema078 只让保存会话中已完成的缺失权限建议恢复显示；`agent_execution`、`agent_files`、`agent_project_files` 即使列于请求中也不构成 Agent Run 执行或受控文件授权。执行仍要求新消息的明确 scope、冻结身份与逐项人工确认，不从卡片或后台续办继承。

## 同项目已验收文件交接（H5-F6，2026-09-21）

跨页候选选择器仅在搜索词实际变化后防抖并回到第一页；初始空搜索不再延时重置用户已翻到的页码。已选择的文件仍跨页保留，实际执行继续按下述来源与人工确认边界校验。

后继 Task 的新 Agent Run 可明确选择**同一项目内另一 Task 当前已验收批次的受控文本文件**，不必手工复制为项目附件。来源 Task 必须为 `done`、`current_submission_id` 指向实际 `accepted` 的 Submission；Artifact 必须属于该批次、未删除、`storage_kind=file`、完整性已验证，并通过实际大小、SHA-256、UTF-8、扩展名/MIME 与受管路径校验。目标与来源须属于同一非空 Project；跨项目、历史/未验收批次和任意本机路径均不接受。

- 新来源 `project_task_artifact` 冻结 `source_task:{project_id,task_id,task_title,task_version,submission_id,submission_sequence}` 及文件 `sha256`；标题遵守 Task 的 2–200 Unicode 字符约束。原 `task_artifact/project_attachment` 请求仅含 `source_kind/id`，不允许新 proof。新来源请求必须有完整 proof 和 hash；重复、别名、null 等歧义字段拒绝。
- `GET /api/v1/tasks/:id/agent-runs/project-task-files` 只读返回 `{data,meta:{offset,limit:20,total,next_offset}}`。`offset` 为规范非负整数，可选 `query`（≤200 字符）按来源任务标题/文件名做字面子串搜索；按文件 `created_at DESC,id DESC` 稳定分页。候选含来源、文件元数据和 `eligible/error_code`，不含正文或路径；不合格文件不可选择。原 `/agent-run-files` 不扩展来源。
- `POST` 同一路径加 `/preview` 接受 `{input_files:[...]}`，仅允许 1–4 个新来源，按选择顺序返回冻结文件引用（元数据），用于跨页选择后的预检，不启动 Run。与旧来源共享最多 **4 个、每个 64 KiB、合计 128 KiB**，不另增额度。原生开始/当前事实重新执行须独立 `confirm_project_task_files:true`，同时保留 `confirm_file_access/file_access_provider_confirmation`；返工同意独立。未选新来源不得附该确认。
- 实际含新来源时，新执行使用规范 **v6**，字段为 `task/provider_kind/model_protocol/max_output_tokens/files/output_contract` 与可选 `rework`。本地/远程 OpenAI 为 `openai_chat + 0`（沿用 Provider 默认，HTTP 不新增 max_tokens）；远程 Anthropic 为 `anthropic_messages + 8192`。至少一个新来源且全部 proof 匹配目标 Project；v1–v5 编码、来源边界及协议含义不变，旧契约拒绝新来源。256 KiB 快照、输出及时限预算不增加。
- prepare、人工审批、实际启动前及原样重试均重读来源身份、版本、当前验收批次、文件 metadata 和实际字节；变化即拒绝，不静默替换。管道除原 `read_controlled_files` 外必须有独立 `read_project_task_files`，且仅实际含新来源时携带；prompt 明确来源，文件仍是不可信业务资料，不授予工具或宿主权限。
- v6 精确 `GET /api/v1/agent-runs/:id` 提供完整 `input_files`、`output_contract`、适用返工、`model_protocol/max_output_tokens/execution_provider_confirmation`；OpenAI token 字段显式为 0。列表及创建/重试回执不携带这些 GET-only 来源细节。UI 原样重试须读取完整详情，重新确认文件和跨任务来源；仅 `failed/cancelled/interrupted + not_ready` 且无结果/提交/staging 可重试，旧字节及 identity 必须相同。
- 活动后继 Run 引用的文件、来源 Task 和对应 Project 受删除互锁保护；来源 Task 即使无自身活动 Run，也不能删除。终态后解除该引用锁，其他领域门禁仍适用。`pending` 仅恢复已存产出登记、不重读来源生成或再调模型；新提交仍需人工验收，不自动完成任务或续办。

AI 的独立授权、opaque 候选和审批见 [跨任务成果交接](ai-assistant.md#跨任务成果交接h5-f62026-09-21)。API v1/schema 076 无迁移、依赖或启动变化；Sidecar/执行器须配套，旧二进制不理解 v6，不能因 schema 相同就降级。真实 Provider、原生 UI 和安装包验收仍单列。

代码证据：[来源查询/预检](../../services/sidecar/internal/api/agent_run_project_task_files.go)、[v6 契约](../../services/sidecar/internal/api/agent_run_model_protocol.go)、[管道验证](../../services/sidecar/internal/agentexec/project_task_files.go)、[真实执行链测试](../../services/sidecar/internal/api/agent_run_project_task_files_integration_test.go)。

## Anthropic 受控执行（H5-F5，2026-09-21）

当前内置执行器支持本地/远程 `openai_chat`，以及远程 `anthropic_messages`。Anthropic 必须 ready、healthy、有密钥；本地 Provider 仍仅支持 OpenAI。任务启动、返工、原样重试、按当前事实重新执行，以及 AI 审批和计划接续共用同一资格、身份和人工验收边界。

- 不含 H5-F6 新来源的新 Anthropic Run 使用 execution contract **v5**，规范 JSON 冻结 `task/provider_kind/model_protocol/max_output_tokens/files/output_contract` 及可选 `rework`。协议只能为 `anthropic_messages`、kind 为 `remote`、输出预算固定 **8192 token**；这是执行器的产品预算，不是所有模型的最大容量。完整字段、规范编码及原 256 KiB 快照上限严格校验；重复、别名、未知字段以及必需协议字段的缺失、null 或错配拒绝，Task 原可空字段仍可空。v1–v4 的 OpenAI 编码及含义保留，历史身份校验明确拒绝协议漂移。
- `opc-agent-pipe-v1` 新增可选 `model_protocol/max_output_tokens`，旧运行两字段仍省略。新执行仅单次非流式请求 `BaseURL` 去尾斜线后加 `/v1/messages`，使用 `x-api-key` 和 `anthropic-version: 2023-06-01`，不发送 Bearer，不启用 tools、thinking、stop_sequences，不自动重试、续写、跟随重定向或切换协议。密钥只经密钥存储、管道和内存，不进入快照、事件、日志或提示词。
- 仅显式 `end_turn` 且全部 content block 为 text 时，按原顺序无额外分隔符合并并校验最终输出。`max_tokens/model_context_window_exceeded` 返回 `AGENT_MODEL_TRUNCATED`，`refusal` 返回 `AGENT_MODEL_FILTERED`；工具、暂停、stop_sequence、缺失/未知终态及非文本内容返回 `AGENT_MODEL_RESPONSE_INVALID`。不把部分正文登记为交付物；HTTP 200 不等于成功。
- v5 支持 text、单 file、2–4 个冻结 files 及可选返工。输入最多四个、单个 64 KiB、总计 128 KiB；返工上下文 64 KiB、输出合计 64 KiB、HTTP 响应 8 MiB、管道帧 1 MiB、时限十分钟沿用现值。实际提交仍进入人工验收，不自动完成 Task。
- 仅精确 `GET /api/v1/agent-runs/:id` 的 v5 详情返回新增 `model_protocol/max_output_tokens/execution_provider_confirmation:{version,config_version,kind}` 与可选私有 `input_files/rework_context`；损坏快照返回 409。冻结 `output_contract` 是既有元数据，可在列表和创建回执出现，v5 详情必须包含。列表和创建回执不带新 GET-only 详情或返工正文，AI metadata 不扩大读取范围。v4 保留旧 `rework_provider_confirmation`。
- 所有 v5 UI 原样重试先读取精确详情并完整核对当前 Provider；纯文本且无返工的原生 retry 保留空 body。实际输入文件须独立 `confirm_file_access/file_access_provider_confirmation`，返工须独立 `confirm_rework_context/rework_provider_confirmation`；只输出文件不意味着发送输入文件。v5 retry 仅允许 `failed/cancelled/interrupted + not_ready` 且无结果、提交或 staging；成功/retained 另走当前事实新执行，pending 只能恢复登记。prepare/prelaunch 复核原规范快照、文件及可选返工；活动引用删除互锁和恢复幂等保持。

API v1/schema 076 不变，无迁移、新 scope、依赖或后台服务；执行器与 Sidecar 须配套，旧代码不理解 v5，不能仅凭 schema 相同认定可降级。真实 Provider 质量、原生界面和安装包仍须专项验收。

依据：[Messages API](https://platform.claude.com/docs/en/api/messages/create)、[完成原因](https://platform.claude.com/docs/en/build-with-claude/handling-stop-reasons)。代码：[规范快照](../../services/sidecar/internal/api/agent_run_model_protocol.go)、[响应验收](../../services/sidecar/internal/agentexec/anthropic.go)、[原生集成测试](../../services/sidecar/internal/api/agent_run_anthropic_test.go)。[AI 审批边界](ai-assistant.md#anthropic-执行审批与计划h5-f52026-09-21)另述。

## 按当前事实重新执行（H5-F4，2026-09-21）

当旧 Run 已失败/取消/中断，或结果仅保留而未提交时，用户可以选择“按当前事实重新执行”。它与“原样重试”分开：重新冻结当前 Task、Assignment、Actor、Adapter、显式选择的 Provider、资料和输出契约，保留旧 Run、结果及审批历史。Task 或模型配置变化不再只能卡在旧快照重试；当前资格、并发、版本和人工验收门禁仍须通过。

- 来源限同 Task 的真实 `failed/cancelled/interrupted + not_ready`、无结果/提交/暂存，或有合法完整保留结果的 `succeeded + retained`。须有有效完成时间及正数冻结 Task version/attempt；旧 version 0 记录不能声明为已核验的关联来源。活动、pending、submitted、跨 Task 或损坏来源拒绝。pending 仍只能恢复登记，不能重跑模型。
- 新只读 `POST /api/v1/tasks/:taskId/agent-runs/restart-preview` 接收 `provider_id`、`restart:{run_id,expected_task_version}` 及本次可选 `input_files/output_contract/rework`，返回 `task_id/task_version/source_run/current_start/fingerprint`。`source_run` 只有 `run_id/task_id/status/output_delivery_status/attempt/task_version/actor_id/provider_id/model/completed_at` 十项元数据；`current_start` 是完整当前执行预览。旧文件、旧返工资料或 retained 正文不会自动带入新输入。
- 原创建接口须发送同样选择，另加 `confirm_restart:true` 和 `restart_preview_hash`；hash 绑定完整预览、选择和输入快照。缺确认返回 `CONFIRMATION_REQUIRED`，来源不合法或确认 hash 格式错误返回 `AGENT_RUN_RESTART_INVALID`，当前 Task 版本或完整预览变化返回 `AGENT_RUN_IDENTITY_CHANGED`，均不创建 Run。不能同时 `auto_assign`，缺负责人先完成原分派流程；文件和返工仍各自要求既有独立确认及 Provider 版本确认。
- 任务执行列表、运行详情和右侧资源共用完整确认界面：先明确选择当前模型/资料，再读取完整事实，分别确认执行、来源关联及适用文件/返工离机范围。改变选择或来源清除旧同意；提交前重读预览，迟到或已关闭来源不能触发尚未发送的创建。已提交请求仍须用原取消命令处理，不承诺撤回 Provider 调用。
- 新 Run 与 queued/restarted 事件在同一事务创建。`agent_run_restarted` 是不可变关联事实：aggregate/agent_run 均指新 Run，`current_json` 严格包含十项 `source_run` 与 `execution_sha256`；hash 绑定新 Run 不可变执行身份和输入，不绑定生命周期状态。事件不含输入正文，也不是授权。重复、畸形、hash/来源不符的证明必须失败，不能降级成无关联 Run。
- `parent_run_id` 仍仅表示原样重试，新执行保持为空；attempt 按 Task+Actor 分配，换当前 Actor 后不要求大于旧 attempt。原生列表/详情/回执及 AI Run 查询和执行回执只在证明核验后投影可选 `restart_of_run_id`；Artifact 列表不带此字段。原样重试一个已关联的新 Run 仍走 parent 链，不复制 restart 事件，且不得绕过损坏的原关联证明。

AI 和计划接续见 [AI 当前事实执行](ai-assistant.md#当前事实执行与计划接续h5-f42026-09-21)。API v1/schema 076、执行输入 v1–v4、wire 及预算不变，无新迁移；事件 hash 编码应保持兼容，不能因 schema 相同就降级到不理解新事件/审批语义的旧二进制。没有自动重试、后台续办、旧结果自动复用或自动验收。

代码证据：[预览与关联证明](../../services/sidecar/internal/api/agent_run_restart.go)、[创建入口](../../services/sidecar/internal/api/agent_runs.go)、[确认界面](../../apps/web/src/components/AgentRunRestartModal.tsx)。确定性证据：[原生契约](../../services/sidecar/internal/api/agent_run_restart_test.go)、[计划与双协议 Harness](../../services/sidecar/internal/api/ai_work_plan_agent_restart_test.go)、[前端接口](../../apps/web/src/api/agentRunRestart.test.ts)。本地模拟 Provider 和隔离数据库测试不替代真实 Provider、原生桌面或安装包验收。

## 执行完成信号与安全失败原因（H5-F3，2026-09-21）

内置执行器的非流式 OpenAI-compatible 响应仅在单个文本候选明确 `finish_reason="stop"`、无拒绝或工具调用，且通过原有内容/大小/输出契约校验后，才进入产出登记。HTTP 200 或非空文本本身不是完整交付证明；该检查适用于 v1–v4 的 text/file/files，包括返工及人工重试。协议完整不代表业务质量合格，成功提交仍须人工验收。

- `length` 返回 `AGENT_MODEL_TRUNCATED`；`content_filter` 或正常终态中的非空 `refusal` 返回 `AGENT_MODEL_FILTERED`。
- 缺失、null、空值、未知或类型错误的结束标志，工具/旧函数调用、非文本或歧义响应返回 `AGENT_MODEL_RESPONSE_INVALID`。不截取部分正文充当成功，不调用工具，不额外请求模型。兼容服务也必须返回明确正常终态；此前省略该字段的服务会被拒绝，需修正其响应契约，不提供静默降级。
- 三个新码及已有安全模型/结果错误码经已匹配身份的单一 error manifest 传递。成功写出安全错误帧表示通信完成，子进程正常退出，但 Run 仍为 failed；畸形输入、非法端点、写帧失败、异常退出、尾随字节/第二帧等仍失败关闭。Runner 不放宽进程退出、身份或 EOF 门禁。
- 失败不登记 Submission/Artifact、不推进任务完成，也不把部分结果放进 pending/retained 恢复流程。失败码在 Run、原有失败诊断预设及来源校验中保持闭集；未知文字不扩为新码。进程 stderr 不输出上游报错正文、地址或凭据。
- 任务执行列表、运行详情、右侧资源和 Inbox 来源显示固定中文说明并保留原安全码；通知仍不是当前 Run 外键证明。重试仍需原冻结身份及人工确认，诊断通知重试只重试本地通知写入，不重跑模型。

API v1/schema 076、执行输入 v1–v4 与 wire `opc-agent-pipe-v1` 不变，无新 scope、迁移、自动重试或协议切换。[OpenAI Chat Completions 文档](https://developers.openai.com/api/reference/resources/chat/subresources/completions/methods/create)明确区分正常结束、截断、过滤和工具调用；仅接受明确完整文本是本执行器的验收选择，不把主聊天已有的双协议或瞬时故障重试外推为 Runner 支持。

代码证据：[响应验收](../../services/sidecar/internal/agentexec/executor_response.go)、[执行器](../../services/sidecar/internal/agentexec/executor.go)、[进程验收](../../services/sidecar/internal/agentrunner/runner.go)、[失败诊断](../../services/sidecar/internal/api/automation_agent_run_failure.go)、[固定失败说明](../../apps/web/src/lib/agentRunFailureSource.ts)。确定性验证包括本地模拟 HTTP 服务与真实执行器子进程，不代替真实 Provider、原生桌面或安装包验收。

## 退回意见驱动的新执行（H5-F2，2026-09-21）

返工使用新的 `agent_run.start`，不是重试已成功的 Run：Task 必须是 `in_progress/manual`，所选 Submission 必须仍是 Task 当前的 `changes_requested` 批次，且有真实退回原因和审核时间。原批次和运行历史不覆盖，新执行不设 `parent_run_id`；成功交付仍经原命令创建新待验收批次，不自动通过验收。失败/取消/中断的返工执行才可按既有规则重试，并冻结原输入及直接 parent 链；Task、责任、Provider 或源证据漂移均拒绝。

- 原生创建请求增加可选 `rework: {submission_id, artifact_ids, expected_task_version}`。`artifact_ids` 必须显式给出数组，可为 `[]`（只带意见，不带旧稿），最多四个且唯一，只选同批次活动的 `text/link/structured` Artifact。完整原始内容和 SHA-256 一并冻结，结构化内容不经前端重新序列化；不自动带入摘要、其他旧产出或文件正文。带 `rework` 时不允许 `auto_assign`，缺责任先单独分派后重新读取 Task 版本。
- 新只读 `POST /api/v1/tasks/:id/agent-runs/rework-preview` 接收同一 `rework` 对象，返回 `task_id/task_version/rework_context`。UI 先显示完整退回原因、审核时间、所选旧产出正文及哈希；加载、错误、超限或过期预览不能执行。预览不建 Run、不写库、不调用模型。选择、Task/Provider 变化须重新核对，不能用迟到响应恢复旧同意。任务页与右侧精确详情复用返工重试确认界面；预检期间关闭或切换来源（含切走后切回）阻止尚未发送的执行请求，不承诺撤回已提交的 Run，后者仍走原生取消命令。
- 原生开始须独立 `confirm_rework_context=true` 与 `rework_provider_confirmation={version,config_version,kind}`，后者绑定当前 Provider；远程 Provider 会收到这些正文。文件输入仍走原 `input_files` 与独立 `confirm_file_access/file_access_provider_confirmation`，返工选择不接受 file Artifact，`output_files` 阅读权限不变成执行许可。
- 仅精确 `GET /api/v1/agent-runs/:id` 详情可以返回合法 v4 的 `rework_context/input_files/rework_provider_confirmation`，后两者只含冻结输入文件 metadata 与 Provider 的 version/config_version/kind，用于完整复核后人工重试；全局/Task Run 列表、AI Run 元数据、创建回执均不附带返工正文。`POST /api/v1/agent-runs/:id/retry` 对 v4 重新要求 `confirm_rework_context/rework_provider_confirmation`，不能由列表直接裸重试；含文件输入时另须 `confirm_file_access/file_access_provider_confirmation`。非 v4 既有空 body 重试兼容不变，多余确认字段拒绝。
- AI 的 `agent_run.start` 增加成对的 `changes.rework_submission_id/rework_artifact_ids`，先由 `workspace_task_submissions` 读取真实批次、退回意见与产出 metadata，再由服务端读取、冻结完整选中证据。仍需本条 `work+outputs+actions+agent_execution`；建议卡完整显示 `agent_run_start.rework_context` 和 Provider 范围，确认时另须 `confirm_agent_rework=true`。AI 不得提交意见、正文或确认字段。v4 retry 的文件 scope 按实际输入/文件输出契约决定，纯文本返工不额外要求 `agent_files`；有输入则再独立文件确认。提议工具仅返回未执行的 metadata，不将人类审批预览正文回传给模型。
- 新 execution contract **v4** 冻结 Task、Provider kind、受控文件引用、输出契约和 `rework`；`ReworkContext` 包含 `submission_id/sequence/review_reason/reviewed_at/artifacts`，每项为 `id/storage_kind/name/content/sha256`。整个返工 JSON 最大 **64 KiB**，超限拒绝而不截断；原整体快照、审批预览和协议帧预算不扩大。v1/v2/v3 序列化保留，wire 仍为 `opc-agent-pipe-v1`，v4 增加显式 `read_rework_context` 有界资料能力；不增加任意工具执行权限。业务要求作为工作输入，资料中试图改变权限、泄露凭据或调用宿主工具的指令不能覆盖运行边界。
- prepare、人工确认和实际启动前重读同一来源事实并比较完整快照；活动 Run 引用的返工非文件产出与受控文件受删除互锁保护。pending 只恢复已保存输出登记，不重新调用模型或重新读取返工输入来生成结果。无数据库迁移，API v1/schema 076 保持；旧运行器不理解 v4，新旧 Sidecar/执行器应配套，不能只凭 schema 相同判定可降级。

代码证据：[返工冻结与只读预览](../../services/sidecar/internal/api/agent_run_rework.go)、[执行协议](../../services/sidecar/internal/agentexec/rework.go)、[AI 桥接](../../services/sidecar/internal/api/ai_agent_run_actions.go)、[任务返工选择](../../apps/web/src/components/TaskAgentRework.tsx)、[重试确认界面](../../apps/web/src/components/AgentReworkRetryModal.tsx)。确定性验证：[契约与双协议 Harness](../../services/sidecar/internal/api/ai_agent_rework_contract_test.go)、[计划重试链](../../services/sidecar/internal/api/ai_work_plan_agent_rework_test.go)、[完整预览 API](../../apps/web/src/api/agentRunRework.test.ts)、[原生调用与隐私边界](../../apps/web/src/api/agentRunReworkClient.test.ts)。这是受人工确认的单次返工闭环，不是后台续办、自动验收或并发子代理；真实 Provider 质量和原生桌面验收仍单独进行。

H5-F1 另接聊天侧的受控文件产出复查：独立 `output_files+work+outputs` 可校验并读取已登记 Task Artifact 的当前实际 UTF-8 字节，不能用 Run.result_text 副本冒充文件已检查。该许可不来自 `agent_files`，不启动/重试 Runner、不读输入或 recovery staging，也不替代人工验收；未登记 retained/pending 结果不能由此变成已提交文件。见 [Task 文件复查](tasks.md#ai-受控文件产出复查h5-f12026-09-21)。

H3-C3（2026-09-21）将真实 `agent_run_failed` 的 queued 身份失效及执行失败接入默认禁用的诊断预设；人工启用后，终态事务冻结五字段证据并捕获规则，独立持久消费者创建本地 Inbox。不把 cancelled/interrupted/pending/retained 当失败，不补发旧 Run，不重跑模型。通知失败重试只是本地写入，原 Run/Task 已删除则以不可重试 SOURCE_UNAVAILABLE 记录；通知成功不改变 Run/Task/Submission 状态。活动诊断阻止 Task 删除，终态来源先标记删除并审计。精确来源查询需 work+outputs，原生详情保留合法返回会话；便携导入保留证明、不包含 AgentRun。API v1/schema 076 不变，完整契约见 [失败诊断闭环](automation.md#agent-失败诊断闭环h3-c32026-09-21)。

> 当前基线：app v0.1.1 / API v1 / SQLite schema 79。T-19 v0.2-A Adapter 登记/诊断、v0.2-B Windows 内置 Runner、v0.2-C 条件式提交和任务详情 Run 界面、H5-A 受人工确认的 AI→Agent Run 启动、H5-B 可恢复产出登记，以及 H5-C 受控文本文件输入/单文件与有界多文件输出已经交付。schema 071 建立 Run，schema 074 冻结完整执行身份并增加唯一并发门禁，schema 075 固化产出登记状态、精确 Submission/Artifact 身份和安全恢复 staging，schema 076 增加会话计划修订，schema 077 增加有界续办授权/轮次及消息角色唯一约束，schema 078 增加保存会话权限请求，schema 079 扩大下一条消息完整范围建议并保留历史请求/决定状态；当前开发库已通过备份门禁完成 079 升级。H5-E18 只在成功保存计划后以 `{generation_id,version,step_count}` 通知当前聊天刷新，H5-E19/E20 只让已观察的终态或用户原生命令成功后立即重读现有投影，H5-E21 则在右侧 Agent 工作区镜像其他保存会话的续办 metadata 并回到原入口处理，H5-G52 又将 actionable Agent Run metadata 纳入统一 Agent Inbox，均不改变 Run 调度或执行权限。跨平台、external 执行器、任意文件系统/工具调用与自治多步计划仍未交付。决策见 [ADR-027](../adr/027-builtin-agent-executor-and-run-lifecycle.md) 与 [ADR-029](../adr/029-agent-workspace-capabilities.md)。

## 定位与边界

H2-X 补齐已有普通待办转人工验收的 AI 入口：`work+actions` 可在原生 todo/无提交历史条件下提议 `task.update(review_policy=manual)`，人工确认后重新读取版本与责任再准备 Agent；不会自动添加验收人、分派、启动或赋予 `agent_execution`。父任务可能按原有规则进入 child_rollup 待验收，该影响必须在确认前披露。最终 Agent `submitted` 仍只表示提交，需 owner 验收。见 [Task 策略切换](tasks.md#ai-既有任务验收策略切换h2-x2026-09-21)。

H5-E29 将失败 Run 的人工重试接入计划：只有 `agent_run.start/retry` 的真实失败/取消/中断且 `not_ready`、无产出执行，才可由已绑定的同会话同 Task 精确 retry 通过 `replaces` 接续。确认后重验直接 parent、递增 attempt、冻结身份/输入/输出契约；不读取当前配置来替换历史。拒绝/过期的中间重试继续保留原 Run 约束，不能换无关操作消除失败。接续不改写原失败状态与审批，新 confirmed 不是成功；submitted 只满足执行步骤，不等于任务验收。计划 `evidence.retry_of_run_id` 只含身份，不外发冻结快照或新增权限。待登记只恢复同一 Run，不重试模型，详见 [精确重试接续](ai-assistant.md#失败执行的精确重试接续h5-e292026-09-21)。schema 076 不变，旧代码读取新替代语义仍需配套兼容。

H4-AS 允许明确撤回发生确认冲突的旧 Agent 建议后，在新消息中重新核验原意图；仍需 `work+outputs+actions+agent_execution`，含文件字段、v2/v3 retry，或实际有文件输入/输出的 v4 retry 还须另选 `agent_files`。附件只披露原命令参数，不读取执行输入快照、文件正文或完整审批预览；旧候选须重新取得并独立人工确认。同轮活动/待登记 Run 继续阻断接续，不自动重跑或登记。见 [审批冲突重新核验](ai-assistant.md#审批冲突后的原意图重新核验h4-as2026-09-21)。

H5-E25 补齐执行与对话接续的状态同步：观察到 Run 终态或收到原生命令回执后，既有刷新链也取消、失效任务关联的完整 AI 审批组查询，避免单卡已提交而组级仍显示运行中。未知关联保守重读，已知无关组不刷新；全组仍有 pending 建议或活动/待登记 Run 时继续阻断，不自动确认、发送、恢复或重跑。详见 [预算收尾与执行后接续](ai-assistant.md#轮数预算收尾与执行后接续h5-e24e252026-09-21)。

AI 产出恢复现通过 `agent_run.recover_output` 提议与独立人工确认复用 `recoverAgentRunOutputInTransaction`；审批决定与登记同事务，文件补偿等待实际外层 commit/rollback，HTTP 取消也必须释放已开启事务。原生恢复入口行为不变。只消耗持久 staging，不调用 Provider 或启动子进程；对话回执区别 submitted 与 retained，详见 [AI 恢复契约](ai-assistant.md#受人工确认的产出登记恢复2026-09-19)。

对话重试已复用原生 `prepareAgentRunRetry`：AI 仅提议 failed/cancelled/interrupted 的精确 Run，以原身份与输入/输出契约准备下一次尝试，用户重新同意后才创建 parent 链并在提交后启动。v2 须本条文件权限，有输入须再次文件确认；审批失败整体回滚。原生重试的既有终态范围不变。详见 [AI 重试契约](ai-assistant.md#受人工确认的执行重试2026-09-19)。

本模块把明确分派的任务交给代码所有的内置受控执行器，记录一次 Run，并在符合任务验收条件时提交产出。它不是 AI 聊天页面，也不是可以自由操作电脑的自治代理。

- 执行器在本机短生命周期子进程内运行；可显式选择本地/在线 OpenAI 兼容模型或在线 Anthropic Messages 模型。在线 Provider 会接收任务提示及已确认资料，不等于全部离线。不含跨任务文件时 v1–v4 为 OpenAI、v5 为 Anthropic；含跨任务文件时两协议均使用上述 v6。
- 生产能力只读取冻结 Task 快照，以及用户明确选择的当前 Task 活跃 file Artifact / Task 当前 Project 的活跃 Attachment；输出为文本、一份受控 UTF-8 文件，或 2–4 份服务端冻结的受控 UTF-8 文件。不接受任意文件路径、目录枚举、Shell、SQL、Git、终端、浏览器控制或 WebView Bearer Token。
- Run 的 `succeeded` 只代表模型执行已产生并安全处置输出，不等于 Task done；它必须同时是带精确 Submission/Artifact 身份的 `submitted`，或带稳定原因码的 `retained`。只有 manual-review 条件满足时才进入 `waiting_review`，owner 接受或要求返工。
- Task 硬删除在事务内拒绝所有 `queued/running` Run（含 `running + pending`），返回稳定 `TASK_HAS_ACTIVE_AGENT_RUN`；pending 须先恢复产出登记，其他活动 Run 须先取消或等待。终态 Run 才允许随 Task 聚合级联删除，避免子进程继续运行或 recovery staging 被静默抹除。
- 删除业务数据、发送消息/发票、确认付款和其他高风险动作不交给执行器。
- builtin 按 ADR-027 的代码信任及平台生命周期矩阵门控；Windows 已有矩阵证据，macOS/Linux 未验证。external 仍须满足 ADR-003 全部沙箱/禁网/进程树闸门，当前不可执行。
- 初始化不自动启用 Adapter、不写演示 Actor/Assignment、不因打开设置或任务页调用模型。登记、启用、分派、启动是可区分的用户动作。
- AI 对话里的责任分派仍不等于启动。只有保存会话为该条消息显式授予 `agent_execution`、模型形成严格提议、用户查看完整冻结预览并额外确认后，才可创建精确 Run；若还要选择受控文件，必须同一消息再授予 `agent_files` 并独立确认文件发送。文件/Git/终端/浏览器面板不会隐式成为执行上下文或权限。
- 单消息 `workspace_ui` 只可让 AI 打开一个固定右侧工作区标签，不读取标签内容，也不能启动/输入终端、导航浏览器、访问文件/Git 或创建、取消、重试、恢复 Agent Run；它不是 `agent_execution` 的替代或前置条件。
- 单消息 `workspace_browser` 只可让 AI 提出一个待用户确认的绝对 HTTP/HTTPS 新标签请求。模型不直接导航，不能读取网页、Cookie、登录态、历史、下载或路径，也不能点击页面、填写表单、控制 Chromium 或创建、取消、重试、恢复 Agent Run；它不是 `agent_execution` 的替代或前置条件。
- H4-AQ 是另一条反向、人工双闸门：用户只能从当前显示的 HTTP(S) 浏览器标签显式带入已移除查询参数、片段和账号信息的地址，交接卡再次带入并发送后才会给 Provider；它不带页面标题/正文、Cookie、登录态、历史、下载或原生标签 ID，不增加 scope、浏览器或网络控制能力，也不能被模型用来访问、抓取、点击或填写网页。
- 只有同时具备 `workspace_ui` 与对应 `work` 或 `clients` 读取范围时，AI 才可请求用户确认定位一个已授权的 Task、Project、Client、Inbox Item、Reminder、Roadmap Milestone 或 Content Item。另在 `workspace_ui+work+outputs` 下可定位一个 Agent Run 或 Task Submission：服务端只在联结现存 Task 后推导其 Task ID，流事件仅供前端组成固定执行详情或提交批次 route，工具回执不泄露该关系、标题、批次摘要、Artifact、执行内容或文件。三类请求都只带模型提供的 type/id、前端自行映射本地 route；模型不能给 route、路径或 URL，不能查看、修改、执行、提交、验收或创建、取消、重试、恢复 Agent Run；它不是 `agent_execution` 的替代或前置条件。

## 当前实现

### 取消意图与结果登记竞态

原生取消命令先在事务内写不可变 `agent_run_cancel_requested` 工作流事件，提交后才向运行中的执行器发送取消信号。queued Run 在同一事务终止；running Run 保持活动（仍阻止重复执行与受控文件删除），直到执行器退出/收尾或启动恢复时转为 cancelled。取消请求和最终取消事件均幂等，不新增 schema 字段。

所有成功、失败、待登记 fallback、启动前中断及启动恢复路径在终态事务内检查该意图：取消先提交时丢弃迟到结果，不创建 Submission/Artifact 或 staging；pending 先提交时取消返回 `AGENT_OUTPUT_DELIVERY_PENDING`，已持久结果只能恢复登记，不能被取消抹除。已终态 Run 的取消保持幂等无操作。取消不能撤回已经发给在线 Provider 的请求或保证停止其服务端计费。

确定性回归见 `agent_run_cancel_command_test.go`：重复请求、迟到结果、失败、staging fallback、中断、重启收敛、事务回滚及提交后才发送信号。对话现以 `agent_run.cancel` 提议复用此命令；只在单消息 `work+outputs+actions+agent_execution` 下可提议，用户另行确认后才执行，见 AI 助手模块。

### Adapter、Actor 与任务门控

- schema 034 的 `agent_adapters` 保存代码所有 manifest、状态、健康、隔离与 `execution_ready`。唯一受控预设为 `builtin-local-text-v1`，响应不暴露内部执行器引用。
- 登记后保持 disabled。Windows builtin 可由已验证构建矩阵返回 healthy/verified/ready；其他平台返回真实 unknown/blocked 等状态，不将所有健康结果硬编码为隔离未验证。健康观察不递增用户配置版本。
- 设置中的检查、显式启用和显式停用调用真实 API，使用 Adapter 的 `If-Match`，不经过 `app_settings` 或设置全局保存。
- 设置中的已登记 Adapter 卡片可人工点击“交给智能体”，但这是无工作台 scope 的排查提示入口：只暂存 Adapter ID、当前启停/健康/就绪页面状态和 `/ai?settings=agent`，进入 AI 后不会让模型读取 Adapter 配置、调用检查/启停 API、分派任务或启动 Run。用户若要登记、检查、启用或停用，仍必须回到原生“设置 → 本地 Agent”人工点击。
- 启用必须在事务内重新校验版本、健康和就绪条件；更新行数或 Actor 一致性异常时拒绝并回滚，不能只依据之前的 Query 快照创建 Actor。明确启用成功才幂等建立匹配的 agent Actor；固定 ID 冲突、inactive、关联错误、已 enabled 却缺 Actor 返回 `409 AGENT_ADAPTER_ACTOR_CONFLICT`，不静默修复。
- 任务页同时读取 Adapter、Actor 与当前 Assignment。只有 Adapter enabled/healthy/execution_ready、Actor type=agent/status=active 且 `agent_adapter_id` 与该 Adapter 一致时，才允许后续启动。
- 缺少登记、未启用、健康不可用、缺 Actor、身份不匹配或读取失败均关闭启动入口，并引导进入“设置 → 本地 Agent”处理；不能用固定 UUID 当作已存在的 Actor，也不通过捕获启动失败后偷偷补分派。
- 无 assignee 时，界面明示“启动会分派给该 Agent”，并把刷新后读到的 agent Actor 与 Task version 作为可选 `auto_assign` 一并提交给创建 Run 接口。Sidecar 在同一数据库事务中复用 Assignment 领域命令、重新准备执行身份并创建 Run；任一后续 Provider/文件/身份门禁失败都会同时回滚 Task 版本、Assignment、分派事件、Run 与排队事件，提交成功后才启动 worker。已有他人 assignee 时不静默改派，须先由用户在责任区显式调整；并发版本冲突不覆盖较新分派。已有正确 assignee 的请求不携带 `auto_assign`，继续沿用原创建路径。
- 启动前重读 Adapter/Actor/Assignment/Provider/Run，并把当前 Task version 用作自动分派的乐观并发前置。启动和重试只对 todo/in_progress 开放；重试要求已有匹配 assignee，不代为补分派。读取/执行错误有中文反馈和刷新；普通 queued/running 的取消不因 Adapter 或 Provider 未就绪而禁用，但 output delivery pending 表示模型执行已结束，任务页不再提供必然冲突的取消，而引导打开执行过程并人工重试登记。

### Run、受控文件与产出

- schema 071 新增 `agent_runs` 与 `actors.agent_adapter_id`；schema 074 为 Run 增加 Task、Assignment、Actor、Adapter、Provider、Provider config 与 execution contract 冻结版本，并以每 Task 单一 active Run及 `(task, actor, attempt)` 唯一索引兜底并发。schema 075 增加 `output_delivery_status/error_code/submission_id/artifact_id` 与不出 API 的 recovery staging。迁移先按 UTF-8 bytes 重算合法旧结果；对超过 64 KiB 或无法形成合法有界结果的旧 succeeded，若不可变事件与当前关系能唯一证明既有 Submission/Artifact 链，则保留 `succeeded + submitted`，只把 Run 内的结果副本替换为有界说明，完整 Artifact 继续是权威产出；没有该唯一链的非法旧行才转为 `failed + AGENT_RESULT_TOO_LARGE/AGENT_RESULT_INVALID`。其余合法历史 succeeded 仅在同样的唯一证据下回填 `submitted`，否则回填 `retained + AGENT_OUTPUT_DELIVERY_LEGACY`。该规范化由破坏性迁移回滚包保护。旧 Run 的新增身份字段保持 0/空且执行时 fail-closed，不伪造当时身份。Sidecar 提供创建、列表、详情、取消、终态重试和 pending 产出恢复 API；先提交 queued，再由受控子进程执行。
- 内置执行器经 Sidecar 保留子命令 `agent-executor` 自再执行；匿名管道的 wire `protocol_version` 继续是 `opc-agent-pipe-v1`，使用 4 字节大端长度前缀 JSON、1 MiB 帧和 64 KiB 聚合结果上限，并校验身份/nonce/未知字段。父进程在唯一结果帧后要求 stdout 精确 EOF 和执行器在 500 ms 内以状态码 0 正常退出；尾随内容、第二帧、非零退出或滞留全部失败，稳定 error manifest 也不得绕过该收尾门禁。execution contract v1 只含 Task 文本快照；v2 为受控输入或单文件输出；v3 为 2–4 份输出文件。v2/v3 都只在本次内存帧中携带复核后的受控 UTF-8 字节与冻结输出契约，不含宿主路径。父进程总时限 10 分钟；Windows Job Object 管理子树回收。
- 当前模型协议为非流式 OpenAI 兼容 `/chat/completions` 及远程 Anthropic `/v1/messages`；Anthropic 在 v5/v6 显式冻结协议，不能从 URL 猜测或把旧 Run 改解释为新协议。本地端点仍限 OpenAI HTTP 回环；在线模型凭据仅在父子进程内存管道传递，禁止重定向，不进入 Run 快照或前端。
- 任务详情提供 ready/healthy 的本地或远程 OpenAI、远程 Anthropic 模型选择，及 Run 列表、活动时每 2 秒轮询、取消、终态重试、展开有界结果与文本型 HTML/Markdown 下载。旧 v1–v3 原生重试可含 succeeded，另受当前任务状态与分派前置约束；v4/v5/v6 仅 failed/cancelled/interrupted 且没有产出或待登记结果可重试，有已处置产出的终态返回 `AGENT_RUN_NOT_RETRYABLE`，queued/running 仍返回 `AGENT_RUN_NOT_TERMINAL`。queued/running 或产出登记 pending 时继续轮询，明确交付终态后停止；旧结果下载不会执行产出，file 交付的权威下载仍是既有受控 Artifact 链。
- 任务详情 Agent 区提供“查看执行过程”，启动成功后自动打开右侧抽屉。抽屉按精确 Task+Run 独立回读，可切换历史 Run，查看状态、时间、模型、错误码、产出登记状态和最终文本；submitted 打开精确提交批次，retained 解释安全留存原因，pending 只在人工点击后调用一次“重试登记产出”。关闭抽屉不取消执行、不清除历史；抽屉不新增启动、整次 Run 重试或取消控制，也不展示模型内部思考、Token 流、工具调用或伪进度。执行成功、提交待验收和人工验收是三个不同事实。
- 右侧工作区的全局 Agent Run 列表只读取 metadata-only 摘要，不携带 `result_text`、输入快照或隐藏 staging。接口支持严格的生命周期 `status` 与 `output_delivery_status=not_ready|pending|submitted|retained` 服务端筛选，未知值以稳定 400 拒绝；`meta.total` 跟随当前筛选，`active_total`（queued/running）、`pending_delivery_total` 与 `succeeded_total` 则忽略列表筛选，并与当前页在同一只读事务形成权威快照。右栏概况直接使用三项计数；Agent 页签按 20 项分页，提供“待登记”入口并用服务端 pending 筛选，因此即使旧 pending 被 50 条更新终态挤出首页也仍可发现。只要全局 active 或 pending 计数非零，列表每 5 秒轮询；两者均为零后停止。用户点击某一行后，前端才按精确 Run ID 调用详情接口读取正文并决定可用动作；列表数据不能冒充详情，也不能直接触发重试、取消或交付恢复。
- 智能体只读工具在同一 metadata 边界内补充 `attempt`、可空 `parent_run_id` 以及可空 `started_at/completed_at`：`workspace_agent_runs`、`workspace_outputs(type=agent_run)` 和 `workspace_get(type=agent_run)` 可据此还原首次执行与人工确认重试的 lineage，并区分排队、执行中和已结束但待登记的真实阶段；全局查询可按 canonical Task 或父 Run 精确缩小范围。它们不会返回输入快照、文件引用、Provider 端点、恢复 staging 或结果正文以外的未授权字段。该链只帮助模型避免把重试当成新的独立任务；重试、取消和产出恢复仍必须走原确认卡。
- 成功结果和产出处置在同一可观察事务完成。冻结身份与当前 Task/Assignment/Actor 仍一致、Task 为 manual review 且可提交时，Sidecar 复用共享 Task output 命令创建 pending_review Submission + text/file Artifact、更新 Task 为 waiting_review，并将 Run 原子置为 `succeeded + submitted`；Submission submitter/Artifact producer 为 agent Actor，Artifact recorder 和事件 Actor 为 system，origin 仍为 manual。file Artifact 使用启动时冻结的安全名称/MIME，AI 名称由服务端生成。任何一步失败都会回滚，不会暴露 succeeded 但缺失处置身份的中间态。
- 冻结 Task version、活动 Assignment/Actor 或 review/status 前置已经漂移时，不把旧快照结果挂到新事实；Run 的有界结果以 `succeeded + retained` 留存并附稳定原因码。数据库或事件写入等瞬时故障则尝试持久化为 `running + pending + AGENT_OUTPUT_DELIVERY_PENDING`，正文只在隐藏 staging 中等待恢复，不误分类为领域漂移。若完成事务与 pending staging 的首次写入都暂时失败，Worker 只在当前进程内保留这份有界正文并退避重试；只有 staging 首次成功持久化后，结果才具备重启恢复能力。进程退出或掉电发生在首次持久化之前仍是明确的残余窗口，当前实现不宣称 crash-proof。成功提交记录 `agent_run_output_submitted`；retained/pending 分别记录安全状态事件，响应只公开状态、原因码与精确 ID，不公开 recovery staging。
- `POST /api/v1/agent-runs/:id/output-delivery/retry` 只恢复 pending；submitted/retained 幂等回读，not_ready 拒绝。并发或重复恢复由事务、唯一索引与状态条件保证不重复创建 Submission/Artifact。pending 已完成模型执行，不能用 cancel 丢弃 staging。Sidecar 启动先重试 pending，只把旧 `running + not_ready` 标为 interrupted，并重新 launch 已提交但尚未启动的 `queued + not_ready` Run；不会重跑 pending 的模型。若 queued claim 遇瞬时存储故障，同一进程按 50 ms、200 ms、1 s 有界退避重排，每次重新经过 maintenance/cancel/CAS 门禁；预算耗尽仍保留 queued，供下次启动安全恢复。
- 整次 Run 重试仍创建新 Run，记录 parent_run_id/attempt 并复用原快照及冻结执行身份，不覆盖历史；不是自动续跑或自动重新读取全部任务事实。创建、claim 与进程启动前分别校验冻结身份，任何 Task/Assignment/Actor/Adapter/Provider/config 漂移均 fail-closed。
- 普通备份包含 SQLite 中的 Adapter、Actor、Run、有界结果恢复副本和事件；便携业务导出不包含 `agent_runs`，Adapter 导入仍按平台与代码清单重新门控。

### H5-C 受控文件输入与有界多文件输出

以下 v1–v3 分型描述 H5-C 的 OpenAI 契约；不含跨任务文件时 OpenAI 显式返工使用 v4、Anthropic 使用 v5，含 H5-F6 来源则使用上述 v6。不能将下述旧来源范围或“无文件 text 使用 v1”外推到新契约。

- 无受控输入且输出为 `text` 时继续保存 `execution_contract_version=1` 和原有 Task 快照 JSON；显式传入等价的空文件列表或 text no-op 也会规范化为该 v1 形态。受控输入或单文件输出使用 v2；2–4 份输出文件使用 v3。三者的重试均复用父 Run 的精确快照与输出契约，不重新扩大候选范围。
- 候选来源严格限于当前 Task 的活跃、未软删、完整性已验证 file Artifact，以及 Task 当前 `project_id` 下的活跃、未软删、完整性已验证 Project Attachment。每次最多 4 个、单个最多 64 KiB、合计最多 128 KiB；正文必须非空、UTF-8 且不含 NUL，名称/MIME/大小/SHA-256 必须与受控存储事实一致。
- prepare 阶段读取并冻结来源种类、真实 ID、名称、MIME、字节数和 SHA-256；v2/v3 持久快照不保存路径或文件正文。claim/启动前再次从受控 store 读取字节并逐项比较冻结身份，文件被删除、改绑、篡改、改名、大小/哈希变化，或 Task 改绑 Project 时均以身份漂移 fail-closed。畸形、非 canonical 或未知字段的 v2/v3 快照同样拒绝执行，不能降级成 v1。
- 管道只携带这批已复核的 UTF-8 字节，不携带本机路径；执行器仍没有文件系统句柄。原生任务入口实际选择输入文件时必须同时提交 `confirm_file_access=true` 和人工勾选时所见 Provider 的 `{version, config_version, kind}`；无输入时禁止携带这组文件授权字段。服务端在创建事务内、写入 Run 与 `agent_run_queued` 事件之前重读并精确比较 Provider 身份；版本、配置版本或 local/remote 类型任一漂移都返回 `409 AGENT_RUN_IDENTITY_CHANGED`。若同一请求还要求为未分派 Task 自动建立 assignee，该 409 会连同 Task version、Assignment 与 `assignment_created` 一并回滚，因此不留下任何分派、Run、排队事件、幂等结果或 worker。前端观察到这三项身份变化，或收到该 409，都会清除旧文件确认并要求重新核对离机提示。确认区明确区分 local Provider（正文留在本机回环调用）与 remote Provider（正文会离开设备）。
- v2 输出契约允许一份 `text` 或一份受控 `file`；v3 只允许 2–4 份受控 `files`。聚合结果仍不超过 64 KiB；所有文件名/MIME 在运行前冻结，模型只按冻结顺序提供 UTF-8 正文，不能返回路径、文件身份、名称或 MIME。AI 路径的 v3 由服务端按 attempt 固定 Markdown 与 JSON 两份文件，原生入口仅可从固定 allowlist 选择。
- 单文件或多文件结果先进入受控 staging，再复用既有 manual-review Submission/Artifact 事务；多文件在一次 Submission 中以冻结顺序创建多个 file Artifact，`agent_runs.artifact_id` 保持首个 Artifact 的兼容指针，追加事件记录全部 `artifact_ids`。冻结 agent 仍是 submitter/producer，system 是 recorder/event actor，Task 最多到 `waiting_review`。Run succeeded 与文件已生成都不等于 Task 已完成，owner 仍须人工验收。
- schema 075 当前没有独立的文件结果恢复正文列；有界 UTF-8 `result_text` 仍作为兼容/恢复副本参与 pending/finalize。v3 在此字段保存严格的 JSON 字符串正文数组，恢复时按冻结契约重新校验；提交成功后的受控 file Artifact 才是交付物与下载权威。该副本不扩大全局列表、AI 回执或工具结果的正文披露。
- 删除互锁扫描全部 `queued/running` Run：v1 明确没有文件引用；v2/v3 必须通过规范 JSON、来源种类、UUID、容量、哈希、名称/MIME 和输出契约的完整语义校验。任一 v2/v3 Run 引用的 Task Artifact/Project Attachment 均阻止对应文件软删除，Project 永久删除也会在其活跃附件被引用时拒绝；未知 execution contract version 或非法快照同样 fail-closed。

## 关键用户流程

### 首次配置与显式启停

1. 打开设置的“本地 Agent”，读取真实 Adapter；空状态可明确登记受控预设。
2. 阅读能力与平台状态，需要时点击“检查运行条件”或“重新检查”；结果显示服务端实际健康/隔离/就绪原因。
3. 只有服务端认为可启用时才能明确点击“启用适配器”。成功响应确认 enabled，并由服务端事务建立匹配 Actor。
4. 若启用失败或发生版本冲突，界面保留错误并重读当前事实；不会把失败视作成功，不补 seed、不自动调用模型。
5. 已启用时可以明确停用；停用关闭新的分派/启动条件，但不删除历史 Actor、Assignment 或 Run。已有 Run 的取消仍是独立动作。

### 从任务启动与验收

1. 任务详情先读取 Adapter/Actor/Assignment。初始化未完成时显示原因和“设置 → 本地 Agent”路径文字指引，禁止先发送 Run 请求再补救；不直接跳转关闭尚未保存的任务草稿。
2. 用户选择可用模型。没有负责人时显示将分派的真实 Agent；已有其他负责人则引导显式改派，不自动替换。
3. 用户可保持默认文本执行，或从当前 Task Artifact / 当前 Project Attachment / 同项目其他 Task 当前已验收文件中共选择最多 4 个受控文本文件，并选择文本、一个文件或 2–4 个固定文件输出。选择输入文件须确认正文发送，跨任务来源还须单独确认；确认绑定当时 Provider 的 version、config version 与 local/remote 类型，在线 Provider 明示正文离开本机。
4. 用户点击启动；若需首次分派，前端把真实 agent Actor 与刷新得到的 Task version 放入同一次创建 Run 请求。Sidecar 在一个事务内先执行受门控的 assignee 创建，再 prepare 并创建 Run；Provider 或文件身份漂移以 409 回滚整笔事务，不会留下 Task 版本变化、Assignment、分派/排队事件、Run 或子进程。已有匹配 assignee 的请求保持原路径。通过后真正 launch 前仍继续核对文件、Task/Project、Provider、责任和执行契约。
5. 启动成功后自动打开右侧执行过程抽屉，也可由“查看执行过程”打开并切换历史 Run。抽屉展示状态、错误和最终结果；关闭只改变界面，启动、取消与重试仍在原任务 Agent 区操作。
6. 成功后仅在 manual-review 提交条件满足时原子进入 `succeeded + submitted` 和 waiting_review；领域漂移时为 `succeeded + retained`，瞬时登记故障则先尝试持久化 `running + pending`，落库后可由启动或人工恢复。首次 staging 写入前只由当前进程内存重试，不提供跨崩溃保证。owner 仍在既有产出/验收区接受或返工，任何交付状态都不自动完成 Task。
7. 活动 Run 存在时目标 Task 不可硬删除；冻结引用同时阻止来源文件、跨任务文件的来源 Task 和对应 Project 被删除。pending 先通过产出恢复 API 结束登记，普通 queued/running 先取消或等待终态。删除没有其他活动引用的终态 Task 聚合，按既有外键语义清理自身 Run 历史。

### 从 AI 对话受确认启动（H5-A/H5-C）

1. 用户在保存会话中为当前消息显式选择“Agent 执行”；界面同时绑定 `work+outputs+actions+agent_execution`。需要受控文件时还要为同一条消息单独选择 `agent_files`；切换会话、Provider 或 Provider version 后失效，发送后立即消费。
2. 模型只能用 `workspace_agent_execution` 查询可执行事实，再提出 `agent_run.start`。没有 `agent_files` 时工具不列文件；有该 scope 时只返回本 generation、同一 Task 有效的 opaque `candidate_id` 和有界元数据。模型不能提供或推断真实文件 ID、路径、SHA-256、正文、Actor、Assignment、Adapter、模型、endpoint、凭据、attempt、parent Run 或任何同意字段。
3. 服务端使用与原生入口共用的 `prepareAgentRun` 解析 generation 内候选映射，生成完整本地预览，展示 Task/完成条件/验收策略、责任、Agent、Adapter、Provider/config、冻结文件元数据、local/remote 离机提示、输出契约与并发影响；真实文件身份、哈希和执行身份只进入本地人工预览，不作为工具结果回传模型。
4. 用户必须另勾选执行确认；存在输入文件时还必须独立勾选文件发送确认，两者不能互相代替。确认事务重新准备并逐字段比较预览；Task/Project/文件/Provider/责任/健康状态变化、已有活动 Run或身份漂移均拒绝旧卡。Proposal 决定、Run 与审计事件同事务提交，事务成功后才 launch；重复确认返回原 Run，不重复启动，也不能把旧确认重放成新的文件授权。
5. 回执只公开 Run/Task/Provider、交付状态、安全错误码及 Submission/Artifact ID，不带输入文件清单/路径/哈希/正文、结果正文、endpoint 或凭据。`submitted` 优先打开 `/tasks/:taskId/submissions/:submissionId`，其他存续状态打开精确 `/tasks/:taskId?agent_run=:runId`；终态 Task 删除后保留决定与结果标识，但省略实时 Run并显示不可导航 tombstone。Run 成功仍须按原 Task 验收策略处理。

## 数据与 API

| 对象                              | 当前事实                                                                                                                                                                                |
| --------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| agent_adapters                    | schema 034；代码所有身份、版本化启停、诊断与执行门控                                                                                                                                    |
| actors                            | schema 071 增加可空 agent_adapter_id；agent 必须关联 Adapter，普通 Actor 保持 NULL                                                                                                      |
| task_assignments                  | 现有 Task 领域分派与历史；Task 版本保护并发                                                                                                                                             |
| agent_runs                        | schema 071 建表，074 冻结执行身份及唯一门禁，075 增加交付状态、精确结果 ID 与隐藏恢复 staging；v1 保存文本快照，v2 冻结受控输入/单文件输出，v3 冻结有界多文件输出；不保存输入路径或正文 |
| task_submissions / task_artifacts | 现有 manual-review 文本/文件产出事实；Agent 路径复用共享命令，不另建自动完成路径                                                                                                        |
| project_attachments               | 仅 Task 当前 Project 的活跃、完整性已验证附件可作输入；活动 Run 引用时阻止附件或 Project 删除                                                                                           |
| workflow_events                   | Adapter/Run 动作以及 submitted/retained/pending/recovered 的追加式安全审计                                                                                                              |

Actor 读模型继续提供可空 `agent_adapter_id`，API v1 不变；当前数据库提升至 schema 76。非 agent 返回 null；兼容旧响应缺失字段时前端规范化为 null，因缺少关联无法证实可执行，必须关闭启动而非猜测。schema 074–076 只扩展操作态 Run/计划与索引，不把执行事实加入便携业务导出；业务导入兼容图显式允许既有 v49/v63–v75 递归迁入 v76。

| 方法与路径                                        | 用途                                                                                        |
| ------------------------------------------------- | ------------------------------------------------------------------------------------------- |
| GET / POST /api/v1/agent-adapters                 | 查询、幂等登记代码内置 Adapter                                                              |
| GET /api/v1/agent-adapters/:id                    | 详情、ETag 与实际健康/就绪状态                                                              |
| POST /api/v1/agent-adapters/:id/check             | If-Match 诊断；不自动启用                                                                   |
| POST /api/v1/agent-adapters/:id/enable            | 版本化、健康和 Actor 一致性门控的显式启用                                                   |
| POST /api/v1/agent-adapters/:id/disable           | 版本化显式停用，保留历史                                                                    |
| GET /api/v1/actors                                | 查询真实 Actor 及可空 agent_adapter_id                                                      |
| POST /api/v1/tasks/:id/assignments                | 复用 Task If-Match 创建分派；不是直接数据库补行                                             |
| GET /api/v1/tasks/:id/agent-run-files             | 列出当前 Task/Project 范围的 metadata-only 候选与文件/结果上限                              |
| GET / POST /api/v1/tasks/:id/agent-runs           | 查询历史或创建 Run；创建支持幂等重放，并可用 `auto_assign` 将空 assignee 与 Run 原子提交    |
| GET /api/v1/agent-runs                            | 全局 metadata-only 分页摘要；严格 lifecycle/delivery 筛选，meta 含筛选 total 与三项全局计数 |
| GET /api/v1/agent-runs/:id                        | 精确 Run 状态、有界结果、稳定错误码和可用动作依据                                           |
| POST /api/v1/agent-runs/:id/cancel                | 请求取消；不是任务取消命令                                                                  |
| POST /api/v1/agent-runs/:id/retry                 | 基于终态新建 attempt；不覆盖历史                                                            |
| POST /api/v1/agent-runs/:id/output-delivery/retry | 只恢复 pending 产出登记；交付终态幂等回读                                                   |
| POST /api/v1/tasks/:id/review                     | owner 接受或要求返工                                                                        |

Adapter 与 Assignment 的写入使用各自版本契约；原生创建保持 HTTP 幂等，AI 创建以不可变 Proposal 决定幂等。原生创建 Run 的 `Idempotency-Key` 按规范化 Task UUID 隔离：同一 Task 的同摘要请求重放原响应、不同摘要返回 409，不同 Task 可以安全复用同一个 key；升级前以路由模板保存的记录只在其不可变 Run 仍能证明属于当前 Task 时兼容重放。Run schema 定义 queued/running/succeeded/failed/cancelled/interrupted；运行中人工取消稳定落为 cancelled，超时与真实执行失败仍为 failed。前端已观察的 Run 从活动/待登记转为终态时，以及任务页/右侧工作区原生创建、重试或取消命令成功返回时，都会立即失效 Task 输出、全局执行、Agent Inbox 和计划投影缓存；取消命令只请求并重读，不能假称已经终态。这只加快本地事实重读，不会启动、取消、重试、恢复或验收任何 Run。

## 后续范围与已知限制

- **H5-C 之后**：受控文本文件输入、哈希双复核和单文件/有界多文件 Artifact 输出已经交付；Agent 专属 Inbox 投影已覆盖计划外的可处理 Run。失败、中断、取消、待登记与 retained 的已保留结果可从执行详情交给智能体做 metadata-only 排查；其中只有 `pending` 能恢复登记，`retained` 必须按当前事实重新提议，绝不等同已提交或已验收。任意二进制/大型文件和更完整产出编排仍待。
- **v0.2-D**：macOS/Linux 生命周期矩阵、external 沙箱/禁网/导入、跨平台发布验收。未验证平台保持关闭，不以 Windows 证据代替。
- **当前结果边界**：CompletionCriteria 与 review policy 已由冻结快照拼入提示词；文本、单文件或每份有界多文件结果超过 64 KiB 时以 `AGENT_RESULT_TOO_LARGE` 整体失败，不截断或产生无效 UTF-8；运行中人工取消落为 cancelled。仍不支持流式 Token、任意文件系统、Shell/Git/终端/浏览器工具、任意数量/二进制结果或多步续跑。
- 本轮不在用户开发库创建 Run 或手工补数据，不调用真实模型；运行链路使用隔离夹具验证。隔离 API/组件测试不替代真实模型质量、网络故障和原生界面验收。

### 后续规划：按能力划分的审批面（尚未实现）

以下是对齐外部实践后记录的设计方向，**当前没有任何代码或接口实现**，不要在验收里按已完成处理；落实前需要先改 ADR-027 的权限边界。

当前 Runner 可按冻结契约交付 UTF-8 文本、单文件或最多 4 份受控文本文件；这些结果由 Sidecar 登记为产出，不等于直接编辑用户项目目录。它不调用通用工具。H5-C 已把“执行确认”和“受控文件访问确认”拆开；若未来再放开其他工具调用，审批仍不应收敛成"一个万能确认弹窗"，而要按能力分面：

- **每个能力一个审批面**：文件读、文件写、进程执行、网络抓取、外部应用控制等各自有独立的确认组件，各自展示该能力专属的关键信息（路径、命令行、目标地址、影响范围）。
- **规则层与展示层分离**：审批规则（哪些命令属高危、哪些路径不可写）独立于确认界面，便于单独测试和审计，不把判断逻辑写进 UI 组件。
- **等待态必须有上界**：任何"等待用户确认"的运行态都必须带超时或显式取消路径，超时后释放进程并落终态，不允许永久占用执行器。当前 Runner 的执行上界为 10 分钟（`defaultAgentRunTimeout`，由 `agentrunner.RunExecutor` 以 `context.WithTimeout` 落实），人工审核发生在 Run 终结之后、不占用进程。
- **审批记录不可变**：每次批准/拒绝是独立审计事实，不改写历史决定。

## 本次初始化验收口径

- 未登记、未启用、不健康、未就绪、缺 Actor、关联不一致和加载失败均禁止启动；只提供明确设置引导。
- 打开页面、健康检查、读取旧 Actor 响应均不隐式启用、分派或调用模型。
- 启用/停用成功来自真实响应；并发旧版本拒绝，Actor 建立失败时 Adapter 更新一并回滚，身份冲突不覆盖。
- 无负责人时明确告知并通过领域 hook 分派；已有他人负责人不静默改派，版本冲突不覆盖，失败可恢复。
- 成功只表示 Run 产出；提交与 owner 验收分别取服务端事实。外部执行器与未验证平台不因 UI 修复放宽安全。
- 本次测试记录以最终 API/组件门禁结果为准，不沿用历史矩阵冒充本轮真机测试。

## 相关文档与代码

- [PRD T-19](../opc-workspace-PRD.md#10419-t-19-本地-agent-执行)、[功能架构](../functional-architecture.md#67-本地-agent-执行v02)
- [Actor 与分派](actors.md)、[任务](tasks.md)、[设置](settings.md)、[数据管理](data-management.md)
- [ADR-003](../adr/003-local-agent-runtime-security.md)、[ADR-027](../adr/027-builtin-agent-executor-and-run-lifecycle.md)、[ADR-029 H5](../adr/029-agent-workspace-capabilities.md#h5-a受人工确认的-agent-run-启动schema-074)
- [Adapter API](../../services/sidecar/internal/api/agent_adapters.go)、[Actor API](../../services/sidecar/internal/api/actors.go)、[Run API](../../services/sidecar/internal/api/agent_runs.go)、[受控文件契约](../../services/sidecar/internal/api/agent_run_files.go)
- [Assignment API](../../services/sidecar/internal/api/assignments.go)、[Runner](../../services/sidecar/internal/agentrunner/runner.go)、[执行器](../../services/sidecar/internal/agentexec/executor.go)
- [Adapter 设置](../../apps/web/src/components/AgentAdapterSettings.tsx)、[任务 Run 区](../../apps/web/src/components/TaskAgentRunsSection.tsx)
- [schema 034](../../services/sidecar/internal/database/migrations/034_agent_adapters.sql)、[schema 071](../../services/sidecar/internal/database/migrations/071_agent_runs.sql)、[schema 074](../../services/sidecar/internal/database/migrations/074_agent_run_execution_identity.sql)、[schema 075](../../services/sidecar/internal/database/migrations/075_agent_run_output_delivery.sql)
