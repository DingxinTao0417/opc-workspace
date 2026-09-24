# Agent Harness 交接 — 长期进度源

> 用途：Agent Harness 强化目标的唯一向前进度来源。每轮开始先重读本文件，再核对 Git / 工作树 / 服务 / 任务状态 / 依赖。
> 历史切片的验收细节仍以 [agent-harness-phases.md](./agent-harness-phases.md)、[PRD](../opc-workspace-PRD.md) 与 [模块索引](../modules/README.md) 为准；本文件只记录现状盘点、向前任务与本轮真实结果，不把规划写成已完成。
> 本文件取代 [agent-handoff.md](./agent-handoff.md)（HH-00…HH-07 已迁移到 §3.2）。

## 1. 环境快照（每轮重核）

| 项          | 本轮实测（2026-09-23）                                                                               | 核对方式                                    |
| ----------- | ---------------------------------------------------------------------------------------------------- | ------------------------------------------- |
| Git 根目录  | 仓库根目录（与工作区一致）                                                                           | `git rev-parse --show-toplevel`             |
| 当前分支    | `feature/ai-assistant`，相对 `origin` ahead 2                                                        | `git status -sb`                            |
| 工作树      | 开始时 864 项、收尾时 875 项未提交修改/未跟踪文件（含本轮新增），全部保留；未执行任何非只读 Git 操作 | `git status --short`                        |
| Sidecar     | `pnpm dev:web` 启动后 `127.0.0.1:9876` ready，API v1，**schema 079**                                 | 启动日志 `ready` 事件 / `GET /health`       |
| Web         | Vite `127.0.0.1:1420`                                                                                | 浏览器打开 `/ai`                            |
| 开发数据    | `.local/dev-data/` 有真实会话、1 个 Deepseek Provider、3 条需关注 Agent Run                          | `/ai` 续办队列                              |
| Go 基线     | `go test -count=1 ./...` 全部通过（api 包 495.8s）                                                   | `.local/baseline-go-test.txt`               |
| Web 基线    | `tsc -b` 通过；vitest 全量见 §5                                                                      | `pnpm --filter @opc/web typecheck` / `test` |
| 桌面 / Rust | 本轮未启动 Tauri 窗口；`pnpm check:rust` 通过（见 §5）                                               | `pnpm check:rust`                           |

约束（必须遵守）：

- 保留用户已有改动；不执行 reset / clean / stash / restore；不提交、不推送、不合并、不变基、不部署。
- 默认门禁保持确定性（不调真实模型、不要密钥、不依赖公网）。
- 不删除安全检查、不放宽权限、不跳过人工确认、不伪造成功；缺少服务端结果身份时不得显示“已执行/已完成”。
- mock、静态页面或确定性测试不得描述为真实 Provider、真实桌面、真实浏览器、真实 UAC、真实安装包或真实断电恢复。

## 2. 现状盘点（以代码、迁移、测试和运行页面为准）

| 方向                 | 已实现（证据）                                                                                                                                                                                                                                                                          | 缺口 / 待核实                                                                                                                                  |
| -------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| 1 Agent Run 生命周期 | `agent_runs`（071/074/075）：`queued/running/succeeded/failed/cancelled/interrupted` × 交付 `not_ready/pending/submitted/retained`，触发器约束组合；`attempt`、`parent_run_id`（精确 retry）、restart 证明、8+32 FIFO 准入（H5-G44）、取消意图、交付恢复、Submission 状态联动（H5-G43） | 已解决：统一生命周期表达（AH-02 DONE）；074 之前旧 Run 无法读取（AH-08 DONE）                                                                  |
| 2 工作计划与连续执行 | `ai_work_plan_revisions`（076）：步骤依赖、`replaces/superseded_by`、证据、续办就绪与 blockers；`ai_continuations`（077）有界后台续办、跨重启安全等待（H5-G12）、事件驱动唤醒（H5-G22）                                                                                                 | 已解决：继续/替换/重试已有；“用户关闭计划”由 AH-05 交付；自治跨 Run 首片“条件式延后启动”由 AH-07 按 ADR-030 交付                               |
| 3 工作台能力桥接     | 16 个单消息 scope；任务/项目/客户/Inbox/提醒/专注/内容/路线图/财务/发票/知识/自动化读写均走“提案 → 人工确认 → 事务 → 回执”；H5-G55/56/57 自然语言回执与结果身份门禁                                                                                                                     | 本轮未新增桥接能力；Run/确认卡回执经 AH-02 统一，结果身份门禁沿用 H5-G57。真实 Provider 下的逐模块回读质量属 ACC-01                            |
| 4 Agent Inbox 与恢复 | `GET /api/v1/ai/inbox` 11 类 metadata-only 来源（approval/access_request/review/automation/knowledge/generation/provider/adapter/maintenance/agent_run/continuation）+ 计划 Inbox + 计划外 Run + 恢复诊断                                                                               | 已解决：续办队列单列表呈现（AH-01 DONE）；桌面文件操作“结果未知”重启后以本机提醒重新发现（AH-04 DONE）；关闭计划后 Run 回到计划外账本（AH-05） |
| 5 文件和工作区       | 精确快照、完整差异、单文件无覆盖替换、原生确认 + 按次 UAC + 绑定 helper、v2 恢复记录、尝试占用记录（ADR-029）；右侧多标签：文件/Git/终端/Chromium/侧聊/Agent 执行，分栏与比例                                                                                                           | 真实 UAC/SACL/安装包/断电未验收（ACC-01）                                                                                                      |
| 6 Codex 风格 UI      | 运行页面实测：左栏（模式切换、新会话、定时任务、项目、续办队列、会话分组）、中间对话（思考过程、运行详情、确认卡）、右侧工作区启动器与多标签                                                                                                                                       | 已解决：续办队列布局（AH-01 DONE）；运行详情工具名统一（AH-06 DONE）；Run 状态徽标按阶段着色（AH-02）                                          |
| 7 可观测性与一致性   | 统一 `writeError` 错误码、请求日志带 `request_id`、严格前端解析、旧 Sidecar 零值兼容                                                                                                                                                                                                    | 已解决：客户动态角标 400（AH-03 DONE）；Run 未知组合显示“状态无法确认”（AH-02）；修复后开发日志无 4xx                                          |

## 3. 任务清单

状态：`OPEN` / `IN_PROGRESS` / `BLOCKED` / `NEEDS_DECISION` / `DONE` / `OUT_OF_SCOPE`。依赖未满足的任务不得开始；一次只推进一个纵向切片。

### 3.1 本目标任务

| ID     | 任务                                                                                                                    | 方向 | 状态         | 依赖   | 验收标准                                                                                                                                                                                                                                          |
| ------ | ----------------------------------------------------------------------------------------------------------------------- | ---- | ------------ | ------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| AH-00  | 建立本交接文档并完成现状盘点                                                                                            | 全部 | DONE         | 无     | 文件存在；环境快照与实测一致；每个缺口有代码/运行证据                                                                                                                                                                                             |
| AH-01  | Agent Inbox 左栏可用性：单一滚动区、只显示有事项的分组、统一加载/错误、分组计数与优先级                                 | 4, 6 | DONE         | AH-00  | 真实页面中待办可见且可点击；空分组不占位，全部为空时只显示一条“已清空”；任一来源加载中/失败时不显示“已清空”；错误只显示一次并可重试；不改变 API、权限或导航；定向 vitest 通过；AI 模块文档同步                                                    |
| AH-02  | 统一 Agent Run 生命周期投影（pending/running/recovery_required/submitted/confirmed/succeeded/failed/cancelled/unknown） | 1, 7 | DONE         | AH-00  | 单一纯函数从 `status × output_delivery_status × submission` 推导；非法/未知组合为 `unknown` 且不显示成功或失败；“已提交/已确认”必须有服务端 Submission 身份；Drawer、任务执行区、续办队列、交接文本共用；单测覆盖全部合法组合与非法组合；文档同步 |
| AH-03  | 工作台侧栏客户动态角标请求越界                                                                                          | 7    | DONE         | AH-00  | 请求不超过服务端上限；读取失败不显示为 0；达到上限时明确显示“N+”；定向测试；无 API 变化                                                                                                                                                           |
| AH-04  | 桌面文件操作结果未知/恢复记录在重启后重新进入 Agent Inbox                                                               | 4, 5 | DONE         | AH-01  | 核实结论：helper 日志按 ADR-029 只在 helper 进程内读取，不改边界；改为原生调用前写入本机提醒、明确回执后删除、重启时残留转“结果未知”；仅含类别/状态/时间/会话 UUID；不提供重试；不接触写入门禁；测试与开发浏览器实测                              |
| AH-05  | 工作计划的取消/放弃与失败步骤受控重试入口核查                                                                           | 2    | DONE         | AH-00  | 先核实现有替换/续办/停止能力；缺口只做最小纵切；不自动执行；continuation 不重复执行                                                                                                                                                               |
| AH-06  | 对话内工具调用与 Agent Run 时间线呈现核查                                                                               | 6    | DONE         | AH-02  | 基于真实组件核实运行详情/工具步骤/Run 卡片；缺口最小纵切；不展示工具参数或正文                                                                                                                                                                    |
| AH-08  | 旧版 Agent Run（`execution_contract_version=0`）在执行抽屉与任务执行区无法读取                                          | 1, 4 | DONE         | AH-01  | 074 之前创建的旧 Run 可只读查看状态、产出与错误；不放宽其他身份/生命周期校验；重试/重启仍由服务端版本白名单把关；定向测试；真实页面抽屉可读取                                                                                                     |
| AH-07  | 自治跨 Run：持久授权 / 预算 / 等待者（原 HH-05）                                                                        | 2    | DONE（首片） | AH-05  | 用户 2026-09-23 选择“纳入下一阶段，按 ADR 实现”；ADR-030 冻结为“逐项确认 + 条件式延后启动”（不做范围授权）；门控事件/对账/派发过滤/认领防御/提案与回执/前端展示；Go 与前端确定性测试；文档同步                                                    |
| ACC-01 | 真实 Provider / 原生桌面 / 安装包 / UAC / 断电专项验收                                                                  | 全部 | BLOCKED      | 各切片 | 需要真实设备、凭据与人工观察，不能用确定性测试替代；未验收前不得宣称整体完成                                                                                                                                                                      |

### 3.2 从 agent-handoff.md 迁移的任务

| ID     | 任务                                  | 状态                     | 说明                                                                                          |
| ------ | ------------------------------------- | ------------------------ | --------------------------------------------------------------------------------------------- |
| HH-00  | 建立 agent-handoff.md                 | DONE                     | 已被本文件取代                                                                                |
| HH-01  | Agent Inbox `adapter_issue` 纵切      | DONE                     | 见 [H5-G58](./agent-harness-phases.md)                                                        |
| HH-01R | 其余未建模失败/恢复来源               | 部分 DONE / OUT_OF_SCOPE | 文件操作结果未知已由 AH-04 交付；业务导入失败、发票 PDF 失败属于数据管理/财务模块，不在本目标 |
| HH-02  | 启动前备份选择                        | DONE                     | 见 [HH-02](./agent-harness-phases.md)                                                         |
| HH-03  | 业务导入非空目标合并 / 跨 schema 升级 | OUT_OF_SCOPE             | 数据管理能力，不属于 Agent Harness；保留在 PRD T-14 追踪                                      |
| HH-04  | 知识库授权引用与来源变化检测          | OUT_OF_SCOPE             | 知识库轨道，保留在 PRD T-16                                                                   |
| HH-05  | 自治跨 Run 最小设计                   | → AH-07（DONE 首片）     | 见上                                                                                          |
| HH-06  | 文件 helper 容量/人工清理与安装后撤销 | OUT_OF_SCOPE（本批）     | 涉及写入门禁与真实 UAC，先完成 AH-04 的只读发现                                               |
| HH-07  | 预设自动化自由规则                    | OUT_OF_SCOPE             | 自动化模块，保留在 PRD T-21                                                                   |

## 4. 当前任务

- **编号**：无（最终审计已完成，见 §9）
- **说明**：AH-00…AH-08 均为 DONE（AH-07 为 ADR-030 首片）；ACC-01 需要真机/凭据/人工观察，不能由智能体独立推进。本轮改动尚未提交：用户已选择“暂不提交”（只提交本轮文件会得到无法单独构建的提交，见 §7）。

## 5. 验证记录

| 日期       | 任务  | 命令                                                                                                                                                              | 结果                                                                                 |
| ---------- | ----- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------ |
| 2026-09-23 | 基线  | `go -C services/sidecar test -count=1 ./...`                                                                                                                      | 全部通过                                                                             |
| 2026-09-23 | 基线  | `pnpm --filter @opc/web typecheck`                                                                                                                                | 通过                                                                                 |
| 2026-09-23 | 基线  | `pnpm --filter @opc/web test`（改动前）                                                                                                                           | 238 文件 / 4029 用例通过                                                             |
| 2026-09-23 | AH-01 | `vitest run AiSessionRail.test.tsx AppShell.test.tsx WorkspaceResources.test.tsx`                                                                                 | 3 文件 / 87 用例通过（新增 3）                                                       |
| 2026-09-23 | AH-01 | `pnpm --filter @opc/web typecheck`；`prettier --check`（改动文件）；`node scripts/check-docs.mjs`                                                                 | 通过；62 个 Markdown 文件与本地链接通过                                              |
| 2026-09-23 | AH-01 | 开发页 `/ai` → 续办队列                                                                                                                                           | “独立执行 3”完整可见，点击可打开执行抽屉                                             |
| 2026-09-23 | AH-08 | `vitest run agentRunDelivery/Restart/ReworkClient/Anthropic/ProjectFiles + AgentRunDrawer + TaskAgentRunsSection`                                                 | 7 文件 / 400 用例通过（新增 6）                                                      |
| 2026-09-23 | AH-08 | 开发页 `/ai` → 续办队列 → 旧 Run                                                                                                                                  | 抽屉显示状态、“结果已保留，未提交”与完整产出                                         |
| 2026-09-23 | AH-03 | `vitest run Sidebar.test.tsx WorkbenchOverview.test.tsx`                                                                                                          | 2 文件 / 20 用例通过（新增 2）                                                       |
| 2026-09-23 | AH-03 | 开发页 `/today` 加载后查看 Sidecar 请求日志                                                                                                                       | 修复后 `GET /api/v1/client-activities` 均为 200                                      |
| 2026-09-23 | AH-02 | `pnpm --filter @opc/web typecheck`；受影响 9 个测试文件                                                                                                           | 通过；466 用例通过                                                                   |
| 2026-09-23 | AH-02 | `pnpm --filter @opc/web test`（全量）                                                                                                                             | 239 文件 / 4059 用例通过                                                             |
| 2026-09-23 | AH-02 | `prettier --check`（本轮改动文件）；`node scripts/check-docs.mjs`                                                                                                 | 通过；62 个 Markdown 文件与链接通过                                                  |
| 2026-09-23 | AH-02 | 开发页 `/ai` → 续办队列 → 旧 retained Run                                                                                                                         | 行与徽标显示“结果已保留 · 未提交”，提醒色                                            |
| 2026-09-23 | AH-05 | `go test ./internal/api -run TestAIWorkPlanClose -v`                                                                                                              | 4 个用例通过                                                                         |
| 2026-09-23 | AH-05 | `go test ./internal/api -run "TestAIWorkPlanClose\|TestAIWorkPlanContinuation\|TestAIWorkPlanInbox\|TestAIWorkPlanDurable\|TestAIAgentInbox\|TestAIContinuation"` | 通过                                                                                 |
| 2026-09-23 | AH-05 | `go vet ./internal/api/`；`node scripts/gofmt.mjs`                                                                                                                | 通过；632 个 Go 文件已格式化                                                         |
| 2026-09-23 | AH-05 | `pnpm --filter @opc/web typecheck`；`vitest run aiWorkPlan.test.ts AiWorkPlan.test.tsx AiSessionRail.test.tsx WorkspaceResources.test.tsx`                        | 通过；4 文件 / 195 用例                                                              |
| 2026-09-23 | AH-05 | 重启 `pnpm dev:web` 后探测 `POST /api/v1/ai/sessions/:id/plan/close` 与 `GET /api/v1/ai/work-plans?state=closed`                                                  | 无计划 404、缺确认 422；`closed_total` 返回 0                                        |
| 2026-09-23 | AH-05 | `go -C services/sidecar test -count=1 ./...`（全量）                                                                                                              | 全部通过（api 481.9s）                                                               |
| 2026-09-23 | AH-06 | `typecheck`；`vitest run aiToolLabels.test.ts AiRunProgress.test.tsx AiAssistantPage.test.tsx aiProgress.test.ts`                                                 | 通过；4 文件 / 171 用例                                                              |
| 2026-09-23 | AH-06 | 开发页 `/ai` 展开三条回复的“运行详情”                                                                                                                             | 显示“工具调用 · 查找会话记忆”                                                        |
| 2026-09-23 | AH-04 | `vitest run fileOperationReminders.test.ts AiSessionRail.test.tsx aiProjectFileReview.test.ts AgentWorkspaceFileReview.test.tsx AgentRunDrawer.test.tsx`          | 通过                                                                                 |
| 2026-09-23 | AH-04 | `pnpm --filter @opc/web test`（全量）                                                                                                                             | 241 文件 / 4083 用例通过                                                             |
| 2026-09-23 | AH-04 | 开发浏览器写入 `in_flight` 提醒并刷新 → 续办队列 → 移除                                                                                                           | 显示“应用在操作进行中退出 · 结果未知”，角标 3→4；移除后回到 3、存储为 `[]`           |
| 2026-09-23 | 全部  | `prettier --check`（与 `format:check` 同范围）                                                                                                                    | 通过（见 §6 格式说明）                                                               |
| 2026-09-23 | 全部  | `pnpm check:source`（format:check 含 prettier/gofmt/rustfmt、check:docs、check:web、check:go）                                                                    | 通过：Web 241 文件 / 4083 用例 + 构建；Go 全量通过（api 347.5s）+ vet + Sidecar 构建 |
| 2026-09-23 | 全部  | `pnpm check:rust`（build-desktop 脚本测试、两个 crate `cargo check`、`cargo test`）                                                                               | 通过：脚本 7/7；桌面 206 通过 / 3 忽略；helper 135 通过 / 3 忽略；helper CLI 4 通过  |
| 2026-09-23 | AH-05 | `check:source` 之后补充 `canClose` 排除 `streaming`：`typecheck`；`vitest run AiWorkPlan.test.tsx`                                                                | 通过；52 用例                                                                        |
| 2026-09-23 | AH-07 | `go test ./internal/api -run TestAgentRunStartGate -count=1`（连续 3 次）；相关 api 与 harness 包；`go vet`                                                       | 5 个门控用例通过；相关包与 vet 通过                                                  |
| 2026-09-23 | AH-07 | `pnpm --filter @opc/web typecheck`；`pnpm --filter @opc/web test`（全量）                                                                                         | 通过；241 文件 / 4098 用例                                                           |
| 2026-09-23 | AH-07 | 首次 `pnpm check:source`                                                                                                                                          | 失败 1 项：`TestAITwoDomainGuideNextTurnFitsBound`，两域提示 65626 > 65536 字节      |
| 2026-09-23 | AH-07 | 删去系统提示末行与核心提示完全重复的约束后重跑该测试                                                                                                              | 通过：OpenAI 65508 / Anthropic 64781 字节；上限与测试未改                            |
| 2026-09-23 | 预算  | 再压缩任务/路线图/内容指南及系统提示中与核心提示、引用规则重复的说法后重跑 `TestAITwoDomainGuideNextTurnFitsBound`                                                | 通过：OpenAI 64730 / Anthropic 64003 字节；上限与测试未改；余量约 806 字节           |
| 2026-09-23 | AH-07 | `pnpm check:source`（重跑）                                                                                                                                       | 通过：Web 241 文件 / 4098 用例 + 构建；Go 全量通过（api 339.0s）；gofmt 634 文件     |
| 2026-09-23 | AH-07 | 开发库副本 + 新 Sidecar（9877）+ 独立 Vite（1421），不动原开发服务器；探测接口并打开 `/ai`                                                                        | 3 个真实 Run 与续办接口 200；续办队列与执行抽屉正常，无门控时不显示“启动条件”        |

## 6. 切片记录

### AH-01 Agent Inbox 左栏单列表呈现（DONE）

- **修改文件**：`apps/web/src/components/AiAgentInboxPanel.tsx`（新建）、`apps/web/src/components/AiSessionRail.tsx`、`apps/web/src/components/AiSessionRail.test.tsx`、`apps/web/src/ai-page.css`、`apps/web/src/agent-workspace.css`、`docs/modules/ai-assistant.md`、`docs/plans/agent-harness-phases.md`。
- **文档边界**：纯前端呈现，不改 API、schema、scope、产品范围或模块关系，因此 PRD、功能架构与模块索引无需修改。
- **剩余限制**：分组计数为已加载条数；Agent Inbox 有更多分页时需点击底部“加载更多续办事项”。

### AH-08 旧版 Agent Run 保持可读（DONE）

- **修改文件**：`apps/web/src/api/client.ts`（契约版本下限 1 → 0）、`apps/web/src/api/agentRunDelivery.test.ts`、`docs/modules/local-agents.md`、`docs/plans/agent-harness-phases.md`。
- **文档边界**：只修正前端解析对既有 DB 状态的接受范围，无 API/schema/产品范围变化；PRD 与架构无需修改。
- **剩余限制**：旧 Run 的状态文案仍由各组件各自映射（例如“执行成功”+“结果已保留”），统一表达在 AH-02 处理。

### AH-03 客户动态角标请求越界（DONE）

- **修改文件**：`apps/web/src/components/Sidebar.tsx`、`apps/web/src/components/Sidebar.test.tsx`、`docs/modules/today.md`。
- **文档边界**：只修正客户端对既有接口上限的使用与失败表达；API、产品范围不变，PRD/架构无需修改。
- **剩余限制**：角标仍只是提醒，精确条数以客户页为准。

### AH-02 统一 Agent Run 生命周期表达（DONE）

- **修改文件**：`apps/web/src/lib/agentRunLifecycle.ts`（新建）与 `.test.ts`（新建）、`AgentRunDrawer.tsx`、`TaskAgentRunsSection.tsx`、`WorkspaceResources.tsx`、`AiAgentInboxPanel.tsx`、`AiWorkspaceActions.tsx`、`lib/aiIssueHandoff.ts`、`styles.css`，以及 `AgentRunDrawer/TaskAgentRunsSection/WorkspaceResources/AiAgentRunActionCard` 测试；文档 `docs/modules/local-agents.md`（权威映射表）、`docs/modules/ai-assistant.md`、`docs/functional-architecture.md`（Agent Run 事实归属行）、`docs/plans/agent-harness-phases.md`。
- **文档边界**：统一的是前端表达与“已提交/已验收需要服务端身份”的展示规则，属于跨模块读模型约定，因此同步了架构事实归属；服务端状态机、API 与产品范围未变，PRD 无需修改。
- **剩余限制**：Run 读取本身不带 Submission 审核状态，任务执行区与抽屉只能显示到“已提交为任务产出”；计划步骤状态表（`planStateLabels`）仍是计划证据专用词汇，未并入。

### AH-05 用户关闭不再需要的计划（DONE）

- **核查结论**：继续（续办提示、H5-G1 后台续办）、替换与重试（`replaces` + 精确 retry / 当前事实 restart）已有完整实现；**缺口是“取消/放弃”**——修订规则只允许追加或替换步骤，被拒绝后停在“待继续”的计划会永久占用续办队列。
- **修改文件**：`services/sidecar/internal/api/ai_work_plan_close.go`（新建）与 `_test.go`（新建）、`ai_work_plan.go`（`closed_at` 投影、Inbox `closed` 状态/计数、工具说明）、`ai_work_plan_continuation.go`（`plan_closed`）、`ai_continuation_worker.go`（`plan_closed` → `plan_changed` 停止）、`ai_agent_inbox.go` 与 `agent_runs.go`（已关闭修订不认领 Proposal/Run）、`router.go`；`apps/web/src/api/aiWorkPlan.ts` 与 `.test.ts`、`components/AiWorkPlan.tsx` 与 `.test.tsx`、`ai-page.css`；文档 `docs/modules/ai-assistant.md`（新章节 + API 列表）、`docs/opc-workspace-PRD.md`、`docs/functional-architecture.md`、`docs/plans/agent-harness-phases.md`。另对既有未格式化的 `ai_work_plan_test.go` 做了一处 gofmt 对齐（仅空白），以满足 `check:go` 格式门禁。
- **文档边界**：新增 API 路由与计划 Inbox 状态，因此同步了模块 API、PRD 与架构；无 schema/迁移，README 的启动/依赖不变，无需修改。
- **剩余限制（AH-05）**：开发库暂无计划，真实会话中的“关闭计划”点击与关闭后界面未做人工验收（需用户在有计划的会话中操作，或授权用真实 Provider 生成计划）；关闭仅作用于最新修订，已保留（retained）的结果需先恢复或按当前事实重新执行后才能关闭。

### AH-06 运行详情时间线统一工具名（DONE）

- **核查结论**：实时进度、生成后运行详情（模型轮次/工具调用/自检/引用校验/本地保存 + 字节/耗时/token）、计划卡、审批卡、Agent Run 确认卡均基于真实服务端数据；缺口是生成后时间线显示工具内部名，且与实时进度两套映射、缺 7 个工具。
- **修改文件**：`apps/web/src/lib/aiToolLabels.ts`（新建）与 `.test.ts`（新建）、`components/AiRunProgress.tsx`、`pages/AiAssistantPage.tsx` 与 `.test.tsx`；文档 `docs/modules/ai-assistant.md`、`docs/plans/agent-harness-phases.md`。
- **文档边界**：仅前端表达，无 API/产品范围变化，PRD/架构无需修改。
- **剩余限制**：新增服务端工具时需同步 `aiToolLabels.ts`；未收录的工具在时间线中保留原标识。

### AH-04 单文件操作的本机结果待核对提醒（DONE）

- **核查结论**：helper 的尝试/执行阶段日志只编译进独立 `file-security-helper` 进程，桌面主进程按 ADR-029 不读取；让主进程解析这些日志属于安全架构变更，且真实链路需绑定发行构建 + UAC，本环境无法端到端验证，因此**不采用**。改为前端“先写意图、后结算”的本机提醒：覆盖应用在 helper 运行期间退出的窗口，且不接触写入门禁与进程边界。
- **修改文件**：`apps/web/src/store/fileOperationReminders.ts`（新建）与 `.test.ts`（新建）、`store/aiProjectFileReview.ts` 与 `.test.ts`、`store/agentRunFileApply.ts`、`components/WorkspaceFileReplacementReview.tsx`、`components/WorkspaceFileRecovery.tsx`、`components/AiAgentInboxPanel.tsx`、`components/AiSessionRail.tsx` 与 `.test.tsx`；文档 `docs/modules/desktop-platform.md`（契约）、`docs/modules/ai-assistant.md`、`docs/functional-architecture.md`、`docs/plans/agent-harness-phases.md`。
- **文档边界**：新增前端本地读模型与续办队列来源，跨模块事实归属已写入架构；无 API/schema/Rust 变化，PRD 版本范围不变，无需修改。
- **剩余限制**：提醒只覆盖本窗口发起的写入；在其他设备/清除浏览器存储后不会保留；真实桌面 helper/UAC 流程中的提醒行为属 ACC-01。

### AH-07 跨 Run 条件式延后启动（DONE，ADR-030 首片）

- **决策**：用户选择“纳入下一阶段，按 ADR 实现”。ADR-030 选定最保守的形态：每个 Run 仍由人逐项确认完整快照，Sidecar 只延后“何时启动”；不做范围授权、自动验收或自动重试。
- **修改文件**：`docs/adr/030-agent-run-start-gates.md`（新建）、`services/sidecar/internal/api/agent_run_start_gate.go` 与 `_test.go`（新建）、`agent_runs.go`（FIFO 过滤、认领防御、三个读取接口投影）、`ai_agent_run_actions.go`（字段、解析、预览、确认事务、回执、工具说明）、`ai_agent_runs_tool.go`、`ai_continuation_worker.go`（事实提交唤醒门控）、`router.go`（协调器生命周期与 `AgentRunGateScanInterval`）、`internal/models/agent_run.go`；`apps/web/src/types/models.ts`、`api/client.ts`、`api/aiWorkspaceActions.ts`、`lib/agentRunLifecycle.ts`、`components/AiAgentRunStartDetails.tsx`、`AiWorkspaceActions.tsx`、`AgentRunDrawer.tsx` 及对应测试；文档 `docs/modules/local-agents.md`、`docs/modules/ai-assistant.md`、`docs/opc-workspace-PRD.md`、`docs/functional-architecture.md`、`docs/adr/027-…`（补充）、`docs/README.md`（索引）、`docs/plans/agent-harness-phases.md`。
- **提示预算**：模型看到的提议 schema 会剥离嵌套说明，但三个新属性仍使 tasks_projects+roadmap_content 两域提示超出 64 KiB 约 90 字节。先删除 `workspaceSystemPrompt` 末行中与 `aiWorkspaceCorePrompt` 完全重复的一句，随后再压缩任务/项目/路线图/内容指南，以及界面、浏览器和知识提示里与同一次请求已有规则重复的说法。字段、枚举、人工确认和引用规则都保留。上限和测试未改。现测 OpenAI 64730 / Anthropic 64003 字节。
- **文档边界**：新增 ADR、API 可选字段与只读投影，已同步模块/PRD/架构/索引；无 schema/迁移，根 README 无需修改。
- **剩余限制**：只表达顺序依赖，不把前置产出传给后续输入；门控固定绑定原前置 Run；全局最多 4 个等待中的门控；真实 Provider 长链与桌面长时间等待、重启后的实际体验属 ACC-01。

### 格式说明（本轮收尾）

- 为使 `format:check` 通过，除本轮改动文件外，还格式化了此前会话留下的 6 个文件（`aiAgentInbox.test.ts`、`aiWorkspaceActions.test.ts`、`desktop.ts`、`ServiceRecoveryGate.tsx` 与 `.test.tsx`、`docs/README.md`），仅空白/换行变化；Go 侧另有 `ai_work_plan_test.go` 一处对齐。
- `apps/web/src/api/knowledge.test.ts` 是已跟踪且未修改的文件，仅因本机 `core.autocrlf=true` 检出为 CRLF 被 prettier 标记；已写回 LF，与索引内容一致（`git diff` 0 行），`git status` 中的 ` M` 只是过期 stat 标记。

## 7. 阻塞与待决策

- **AH-07**：已由用户决定推进；本轮只交付不改变逐项确认边界的首片。真正的“范围授权”（一次授权、多次无人确认启动）仍不做，若需要须另立 ADR 并再次决策。
- **提交范围**：本轮改动依赖此前会话留下的大量未跟踪文件（如 `api/aiAgentInbox.ts`、`ai_work_plan.go`），且 `client.ts`、`agent_runs.go` 等已跟踪文件本身带有之前会话的未提交改动；只提交本轮文件会得到无法单独构建的提交。用户选择暂不提交。
- **ACC-01**：真实 Provider、桌面 WebView2、UAC、安装包、断电需要真机与人工观察。
- **风险（非阻塞）**：全范围 tasks_projects+roadmap_content 两域提示现为 OpenAI 64730 / Anthropic 64003 字节，距 65536 约 806 字节。字段、枚举、人工确认和引用规则都保留，只删了与核心提示或同一次请求里已有规则重复的说法。tasks_projects+outputs / agents 仍被预检拒绝。不得放宽上限；下一次较大的提议 schema 或指南增长仍须先测量这条测试。

## 8. 下一任务

没有可由智能体独立推进的剩余任务。后续需要用户参与：

1. **提交决策**：是否把当前整个工作树（门禁已全部通过的状态）作为一个提交，或按其他范围拆分。
2. **ACC-01 专项验收**（需真机与人工）：真实 Provider 下的计划关闭与续办、绑定发行构建中的单文件写入/恢复（含 UAC 与提醒在 helper 运行中退出后的表现）、安装包、断电恢复。
3. 如需提交或推送本轮改动，请明确授权（本轮未执行任何非只读 Git 操作）。

## 9. 最终审计（2026-09-23）

逐项对照目标原文，以当前代码、测试与运行页面为证据：

| 目标要求                                                                                                           | 证据                                                                                                                                             | 结论                                              |
| ------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------- |
| 1 Run 生命周期：创建/排队/执行/进度/完成/失败/取消/重试/恢复；attempt、parent_run_id、结果身份、执行来源、最终回执 | 既有 H5-G42/G43/G44/G53、071/074/075 触发器；AH-02 `agentRunLifecycle()` 闭集九阶段与 `unknown` 兜底；AH-08 旧 Run 可读                          | 已满足；“已提交/已验收”必须有同一 Submission 身份 |
| 1 缺少服务端结果身份不得显示已执行/已完成                                                                          | H5-G57（确认卡）+ AH-02（Run：缺 Submission ID 为 `unknown`，确认卡仅在同 Submission `accepted` 时显示“产出已验收”）                             | 已满足                                            |
| 2 计划：顺序、受控恢复、继续/取消/替换/重试、continuation 不重复、识别过期/重复/不一致、知道下一步                 | 既有依赖/`replaces`/精确 retry 与 restart/H5-E37/E38/G22 去重/续办 blockers；AH-05 用户关闭计划与 `plan_closed`                                  | 已满足；自治跨 Run 首片由 AH-07/ADR-030 交付      |
| 3 工作台桥接：统一事实读取、提案+显式授权、回读、自然语言回执、权限不足给原因                                      | 既有 16 scope 与 H2/H3 各领域审批链、H5-G3–G10 权限请求、H5-G55/56 回执；本轮未改变授权边界                                                      | 已满足（真实 Provider 质量属 ACC-01）             |
| 4 Inbox 与恢复：统一展示失败 Run/待确认/待回读/结果未知/恢复记录/权限问题；重启重新发现；结果未知禁止自动重试      | 既有 11 类服务端来源 + 恢复诊断；AH-01 单列表；AH-04 文件操作本机提醒（重启重新发现、无重试入口）；AH-05 关闭后 Run 回到计划外                   | 已满足                                            |
| 5 文件和工作区：预览、快照、差异、受控单文件修改及安全链；右侧面板能力                                             | 既有 ADR-029 链路与多标签工作区（开发页实测启动器：变更审查/终端/浏览器/文件/侧边聊天/Agent 执行，分栏/放大/收起）；本轮未放开任何写入或遍历能力 | 已满足（真实 UAC/SACL/安装包属 ACC-01）           |
| 6 Codex 风格 UI                                                                                                    | 开发页实测三栏布局；AH-01 续办队列；AH-06 运行详情工具名；AH-02 状态徽标；AH-05 计划卡关闭                                                       | 已满足                                            |
| 7 可观测性与一致性                                                                                                 | AH-03（失败不显示为 0、不再 400）；AH-02（未知不冒充成败）；重启后开发日志除刻意探测外无 4xx/5xx；新增数据均不含凭据/路径/正文                   | 已满足                                            |
| 维护交接文档（状态/已完成/未完成/当前任务/依赖/验收/修改文件/验证/阻塞/下一项）                                    | 本文件 §1–§9                                                                                                                                     | 已满足                                            |
| 文档同步                                                                                                           | AI 助手、本地 Agent、桌面平台、今日模块；PRD（AH-05）；功能架构（AH-02/04/05）；阶段计划；`check:docs` 通过                                      | 已满足                                            |
| 测试与检查                                                                                                         | `pnpm check:source`（AH-07 后重跑）、`pnpm check:rust`（AH-07 无 Rust 改动）全部通过                                                             | 已满足                                            |
| AH-07 跨 Run 延后启动（用户决定纳入）                                                                              | ADR-030；门控事件/对账/派发过滤/认领防御；提案、预览、回执与三处读取投影；Go 5 个门控用例与前端解析/展示测试；隔离实例真实页面回归               | 已满足首片；范围授权不做                          |
| 不把 mock/确定性测试描述为真实能力；不执行非只读 Git                                                               | 各切片“剩余限制”与 ACC-01 明确列出未验收项；未执行 add/commit/push/reset/clean/stash/restore                                                     | 已满足                                            |
