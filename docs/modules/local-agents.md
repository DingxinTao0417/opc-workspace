# 本地 Agent 执行模块

> 当前基线：app v0.1.1 / API v1 / SQLite schema 71。T-19 v0.2-A Adapter 登记/诊断、v0.2-B Windows 内置 Runner，以及 v0.2-C 条件式文本提交和任务详情 Run 界面已交付。初始化与显式启停遵循下述门控契约；跨平台、external 执行器、文件流与 Agent Inbox 投影仍未交付。决策见 [ADR-027](../adr/027-builtin-agent-executor-and-run-lifecycle.md)。

## 定位与边界

本模块把明确分派的任务交给代码所有的内置文本执行器，记录一次 Run，并在符合任务验收条件时提交产出。它不是 AI 聊天页面，也不是可以自由操作电脑的自治代理。

- 执行器在本机短生命周期子进程内运行；可显式选择本地或在线的 OpenAI 兼容模型。在线 Provider 会接收任务提示，不等于全部离线。
- 生产能力仅为读取 Task 快照并返回文本；不接受任意 Shell、SQL、执行路径、目录访问或 WebView Bearer Token。
- Run 的 succeeded 只代表有输出，不等于 Task done。只有 manual-review 条件满足时才创建 Submission/Artifact 并进入 waiting_review，owner 接受或要求返工。
- 删除业务数据、发送消息/发票、确认付款和其他高风险动作不交给执行器。
- builtin 按 ADR-027 的代码信任及平台生命周期矩阵门控；Windows 已有矩阵证据，macOS/Linux 未验证。external 仍须满足 ADR-003 全部沙箱/禁网/进程树闸门，当前不可执行。
- 初始化不自动启用 Adapter、不写演示 Actor/Assignment、不因打开设置或任务页调用模型。登记、启用、分派、启动是可区分的用户动作。

## 当前实现

### Adapter、Actor 与任务门控

- schema 034 的 `agent_adapters` 保存代码所有 manifest、状态、健康、隔离与 `execution_ready`。唯一受控预设为 `builtin-local-text-v1`，响应不暴露内部执行器引用。
- 登记后保持 disabled。Windows builtin 可由已验证构建矩阵返回 healthy/verified/ready；其他平台返回真实 unknown/blocked 等状态，不将所有健康结果硬编码为隔离未验证。健康观察不递增用户配置版本。
- 设置中的检查、显式启用和显式停用调用真实 API，使用 Adapter 的 `If-Match`，不经过 `app_settings` 或设置全局保存。
- 启用必须在事务内重新校验版本、健康和就绪条件；更新行数或 Actor 一致性异常时拒绝并回滚，不能只依据之前的 Query 快照创建 Actor。明确启用成功才幂等建立匹配的 agent Actor；固定 ID 冲突、inactive、关联错误、已 enabled 却缺 Actor 返回 `409 AGENT_ADAPTER_ACTOR_CONFLICT`，不静默修复。
- 任务页同时读取 Adapter、Actor 与当前 Assignment。只有 Adapter enabled/healthy/execution_ready、Actor type=agent/status=active 且 `agent_adapter_id` 与该 Adapter 一致时，才允许后续启动。
- 缺少登记、未启用、健康不可用、缺 Actor、身份不匹配或读取失败均关闭启动入口，并引导进入“设置 → 本地 Agent”处理；不能用固定 UUID 当作已存在的 Actor，也不通过捕获启动失败后偷偷补分派。
- 无 assignee 时，界面明示“启动会分派给该 Agent”，用户启动后通过既有 Assignment 领域 hook 提交并刷新关联缓存，再创建 Run；已有他人 assignee 时不静默改派，须先由用户在责任区显式调整。并发版本冲突不覆盖较新分派。
- 启动前重读 Adapter/Actor/Assignment/Provider/Run，并使用 Assignment 响应中的 Task version。启动和重试只对 todo/in_progress 开放；重试要求已有匹配 assignee，不代为补分派。读取/执行错误有中文反馈和刷新；取消不因 Adapter 或 Provider 未就绪而禁用。

### Run 与文本产出

- schema 071 新增 `agent_runs` 与 `actors.agent_adapter_id`。Sidecar 提供创建、列表、详情、取消和终态重试 API；先保存 queued，再由受控子进程执行。
- 内置执行器经 Sidecar 保留子命令 `agent-executor` 自再执行；`opc-agent-pipe-v1` 使用 4 字节大端长度前缀 JSON，1 MiB 帧、64 KiB 结果上限，校验身份/nonce/未知字段/尾随内容。父进程总时限 10 分钟；Windows Job Object 管理子树回收。
- 当前模型协议是非流式 OpenAI 兼容 `/chat/completions`，不能将 AI 助手的 Anthropic 支持视为此执行器也已支持。本地端点要求 HTTP 回环 IP 字面量；在线模型凭据仅在父子进程内存管道传递，禁止重定向，不进入 Run 快照或前端。
- 任务详情已提供 ready/healthy 且 openai_chat 的模型选择、Run 列表、活动时每 2 秒轮询、取消、终态重试（含 succeeded，另受当前任务状态与分派前置约束）、展开文本及 HTML/Markdown 下载。下载不会执行产出，也不是文件流执行能力。
- 任务详情 Agent 区提供“查看执行过程”，启动成功后自动打开右侧抽屉。抽屉复用现有 Run 列表数据，仅有 queued/running Run 时每 2 秒轮询；可切换历史 Run，查看状态、时间、模型、错误码和最终文本，并提供打开任务和下载。关闭抽屉不取消执行、不清除历史；抽屉不新增启动、重试或取消控制，也不展示模型内部思考、Token 流、工具调用或伪进度。
- 成功 Run 的文本保留在记录中。Task 为 manual review、todo/in_progress 且负责人/owner reviewer 等条件满足时，Sidecar 创建 pending_review Submission + text Artifact，并更新 Task 为 waiting_review；producer 为 agent Actor，recorder 为 system，Submission origin 仍为 manual。
- 提交前置不满足时，Run 产出保留，追加 `agent_run_submission_skipped` 原因事件，不能把“执行成功”显示成“已经提交/已经完成”。成功提交以 `agent_run_output_submitted` 关联 Submission/Artifact。
- 重试创建新 Run，记录 parent_run_id/attempt 并复用原快照，不覆盖历史；不是自动续跑或自动重新读取全部任务事实。Sidecar 启动把遗留 queued/running 标为 interrupted。
- 普通备份包含 SQLite 中的 Adapter、Actor、Run、文本产出和事件；便携业务导出不包含 `agent_runs`，Adapter 导入仍按平台与代码清单重新门控。

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
3. 用户点击启动；若需首次分派，通过既有 Task `If-Match`/Assignment hook 完成，再请求创建 Run。
4. 启动成功后自动打开右侧执行过程抽屉，也可由“查看执行过程”打开并切换历史 Run。抽屉展示状态、错误和最终文本，关闭只改变界面；启动、取消与重试仍在原任务 Agent 区操作。
5. 成功后仅在 manual-review 提交条件满足时进入 waiting_review。owner 在既有产出/验收区接受或返工；不满足条件时仅有 Run 文本，不产生已验收任务。

## 数据与 API

| 对象                              | 当前事实                                                                           |
| --------------------------------- | ---------------------------------------------------------------------------------- |
| agent_adapters                    | schema 034；代码所有身份、版本化启停、诊断与执行门控                               |
| actors                            | schema 071 增加可空 agent_adapter_id；agent 必须关联 Adapter，普通 Actor 保持 NULL |
| task_assignments                  | 现有 Task 领域分派与历史；Task 版本保护并发                                        |
| agent_runs                        | schema 071；任务、分派、Actor、Adapter、输入快照、结果、错误与重试链               |
| task_submissions / task_artifacts | 现有 manual-review 和文本产出事实；不另建自动完成路径                              |
| workflow_events                   | Adapter/Run 动作和成功提交或跳过原因的追加式审计                                   |

本次只给 Actor 读模型增加可空 `agent_adapter_id`，不新增数据库迁移、不改变 API v1 或 schema 71。非 agent 返回 null；兼容旧响应缺失字段时前端规范化为 null，因缺少关联无法证实可执行，必须关闭启动而非猜测。

| 方法与路径                              | 用途                                            |
| --------------------------------------- | ----------------------------------------------- |
| GET / POST /api/v1/agent-adapters       | 查询、幂等登记代码内置 Adapter                  |
| GET /api/v1/agent-adapters/:id          | 详情、ETag 与实际健康/就绪状态                  |
| POST /api/v1/agent-adapters/:id/check   | If-Match 诊断；不自动启用                       |
| POST /api/v1/agent-adapters/:id/enable  | 版本化、健康和 Actor 一致性门控的显式启用       |
| POST /api/v1/agent-adapters/:id/disable | 版本化显式停用，保留历史                        |
| GET /api/v1/actors                      | 查询真实 Actor 及可空 agent_adapter_id          |
| POST /api/v1/tasks/:id/assignments      | 复用 Task If-Match 创建分派；不是直接数据库补行 |
| GET / POST /api/v1/tasks/:id/agent-runs | 查询历史或创建 Run；创建支持幂等重放            |
| GET /api/v1/agent-runs/:id              | Run 状态、文本和稳定错误码                      |
| POST /api/v1/agent-runs/:id/cancel      | 请求取消；不是任务取消命令                      |
| POST /api/v1/agent-runs/:id/retry       | 基于终态新建 attempt；不覆盖历史                |
| POST /api/v1/tasks/:id/review           | owner 接受或要求返工                            |

Adapter 与 Assignment 的写入使用各自版本契约，不能宣称所有 Run 命令都已有统一 If-Match/幂等保证。Run schema 定义 queued/running/succeeded/failed/cancelled/interrupted；具体未收口限制见下节。

## 后续范围与已知限制

- **v0.2-C 后续**：受控文件流、staging/文件哈希、Agent 专属 Inbox 投影及完整产出联动；当前只自动提交 text Artifact，不把下载按钮算作文件执行器。
- **v0.2-D**：macOS/Linux 生命周期矩阵、external 沙箱/禁网/导入、跨平台发布验收。未验证平台保持关闭，不以 Windows 证据代替。
- **当前执行器待修复**：CompletionCriteria 已在输入 frame 中但尚未由 buildPrompt 拼入；超长文本目前先截断，不能宣称无损或超限必失败；running 取消的最终状态仍可能记录 failed/AGENT_RUN_CANCELLED。初始化修复不等于这些问题已解决。
- 本轮不在用户开发库创建 Run 或手工补数据，不调用真实模型；运行链路使用隔离夹具验证。隔离 API/组件测试不替代真实模型质量、网络故障和原生界面验收。

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
- [ADR-003](../adr/003-local-agent-runtime-security.md)、[ADR-027](../adr/027-builtin-agent-executor-and-run-lifecycle.md)
- [Adapter API](../../services/sidecar/internal/api/agent_adapters.go)、[Actor API](../../services/sidecar/internal/api/actors.go)、[Run API](../../services/sidecar/internal/api/agent_runs.go)
- [Assignment API](../../services/sidecar/internal/api/assignments.go)、[Runner](../../services/sidecar/internal/agentrunner/runner.go)、[执行器](../../services/sidecar/internal/agentexec/executor.go)
- [Adapter 设置](../../apps/web/src/components/AgentAdapterSettings.tsx)、[任务 Run 区](../../apps/web/src/components/TaskAgentRunsSection.tsx)
- [schema 034](../../services/sidecar/internal/database/migrations/034_agent_adapters.sql)、[schema 071](../../services/sidecar/internal/database/migrations/071_agent_runs.sql)
