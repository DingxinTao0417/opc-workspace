# Agent Handoff — 唯一进度来源

> [!IMPORTANT]
> 自 2026-09-23 起本文件已被 [agent-harness-handoff.md](./agent-harness-handoff.md) 取代，HH-00…HH-07 的状态已迁移过去；以下内容仅保留为历史记录，不再更新。

> 用途：无 Goal 模式下的持续工作交接。每轮开始必须重读本文件，并核对 Git / 工作树 / 服务 / 任务状态 / 依赖。
> 历史已交付切片不在此重复展开，见 [agent-harness-phases.md](./agent-harness-phases.md)、[PRD 当前基座任务清单](../opc-workspace-PRD.md#104-当前基座任务清单) 与 [模块索引](../modules/README.md)。
> 本文件只记录向前可执行任务与本轮结果；不得把规划写成已完成。

## 1. 环境快照（每轮重核）

| 项               | 本轮实测                                                                                     | 核对方式                        |
| ---------------- | -------------------------------------------------------------------------------------------- | ------------------------------- |
| Git 根目录       | 仓库根目录（与工作区一致）                                                                   | `git rev-parse --show-toplevel` |
| 当前分支         | `feature/ai-assistant`                                                                       | `git branch --show-current`     |
| 相对远端         | ahead 2（`38e9db7` 起本地未推送提交）                                                        | `git status -sb`                |
| 工作树           | **大量未提交修改与未跟踪文件**（AI/Agent Workspace、file-security-helper、docs、sidecar 等） | `git status --short`            |
| Web 开发服务     | **本轮不可达**                                                                               | `Invoke-WebRequest`             |
| Sidecar 开发服务 | **本轮不可达**                                                                               | `GET /health`                   |
| 进度源           | 本文件（`docs/plans/agent-handoff.md`）                                                      | 文件存在且可读                  |

约束（必须遵守）：

- 保留用户已有改动；不覆盖、不清理、不提交、不推送、不合并、不部署。
- 任何非只读 Git 操作必须先获用户明确确认。
- 默认门禁保持确定性（不调真实模型、不要密钥、不依赖公网）。
- 不删除安全检查、不放宽权限、不跳过人工确认、不伪造成功。

## 2. 任务清单

状态取值：`OPEN` / `IN_PROGRESS` / `BLOCKED` / `DONE` / `SKIPPED`。

依赖未满足的任务不得开始。一次只推进一个任务。

| ID     | 任务                                             | 状态                                  | 依赖               | 来源依据                                                                                                               | 验收要点（代码 + 文档 + 验证）                                                                                                                                                           |
| ------ | ------------------------------------------------ | ------------------------------------- | ------------------ | ---------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| HH-00  | 建立 `docs/plans/agent-handoff.md` 唯一进度源    | **DONE**                              | 无                 | 无 Goal 模式持续工作约定                                                                                               | 文件存在；环境快照与 Git/服务实测一致；任务清单可追溯到 PRD / phases / 模块文档；本轮记录已追加                                                                                          |
| HH-01  | 一体化 Agent Inbox：补齐剩余失败/恢复来源投影    | **DONE**（最小纵切：`adapter_issue`） | HH-00              | PRD T-11/T-19、模块文档多处“其他失败/恢复来源的一体化 Agent Inbox 仍待”；已有 H5-E5–E7、G52 等部分来源                 | 先枚举未投影来源与现有 `kind` 闭集；最小纵切只加 metadata-only 投影 + 左栏入口 + 重新授权 handoff；Go/前端定向测试；同步 AI / Local Agent / Inbox / PRD / 架构；不自动执行、不继承 scope |
| HH-01R | 一体化 Agent Inbox：其余未建模失败/恢复来源      | OPEN                                  | HH-01              | 见 §2.2 HH-01R 候选列表；须先核对是否已有等价投影                                                                      | 每次只做一个来源的最小 metadata-only 纵切                                                                                                                                                |
| HH-02  | 启动前备份选择                                   | **DONE**                              | HH-00              | PRD T-02 / T-04B / T-13 / 10.3.3 / 路线图 6–7 多处“仍待实现”                                                           | 桌面启动链路在打开数据库前提供有界选择（跳过 / 使用指定已有备份）；不静默覆盖；失败关闭；Rust + 文档 + 确定性测试；不把安装包/真机验收写成已完成                                         |
| HH-03  | 业务导入非空目标实际合并 / 跨 schema 升级        | OPEN                                  | HH-00              | PRD T-14、T-04B、路线图 6“实际合并/升级待开发”                                                                         | 先冻结冲突策略与 UUID 重映射规则（ADR）；最小纵切覆盖一种合并方向；回滚包；Go 迁移/导入测试；同步数据管理文档                                                                            |
| HH-04  | 本地知识库授权引用与来源变化检测                 | OPEN                                  | HH-00              | PRD T-16、路线图“授权引用与变化检测待后续”                                                                             | 变化检测 → stale 标记与原子新版本切换；引用不伪造来源；同步知识库 / AI / ADR                                                                                                             |
| HH-05  | 自治跨 Run：持久授权 / 预算 / 等待者最小设计纵切 | OPEN                                  | HH-01              | [agent-harness-phases.md](./agent-harness-phases.md) H5 整体目标“自治跨 Run 调度…仍待”；须单独设计，不复用单消息 grant | 先 ADR 冻结边界；禁止自动审批/绕过验收；确定性测试；不把计划依赖当自动执行                                                                                                               |
| HH-06  | 文件 helper：容量/人工清理与安装后撤销           | OPEN                                  | HH-00              | [desktop-platform](../modules/desktop-platform.md) / ADR-029 文件安全线程“下一步”                                      | 不解除写入门禁除非全部安全条件满足；真实 UAC/SACL/断电保持单独验收项                                                                                                                     |
| HH-07  | 预设自动化自由规则（受限）                       | OPEN                                  | HH-00              | PRD T-21“自由规则…仍待”                                                                                                | 不得自动对外发送；设置预览/保存/启停与不可变 Run；确定性测试                                                                                                                             |
| ACC-01 | 真实 Provider / 原生桌面 / 安装包 / 断电专项验收 | OPEN                                  | 各功能切片代码完成 | 大量 phases 条目“真实 … 仍待人工验收”                                                                                  | **不可用确定性测试替代**；须单独记录真机证据；未验收前不得宣称整体完成                                                                                                                   |

### 2.1 本轮任务记录

#### HH-00 建立唯一进度源

- **状态**：`DONE`
- **执行时间**：2026-09-23
- **本次完成内容**：
  - 确认 `docs/plans/agent-handoff.md` 此前不存在，无法从对话记忆恢复任务状态。
  - 重核 Git 根目录/分支/工作树、1420/9876 服务健康（schema 79）。
  - 从 PRD §10.4、路线图 6–9、`agent-harness-phases.md`、模块索引与 ADR-029 重建可追溯的向前任务清单（HH-01…HH-07、ACC-01）。
  - 写入本文件作为唯一进度来源；历史已交付门禁继续以 phases 为准，不在本文件重复标 DONE。
- **修改文件**：
  - `docs/plans/agent-handoff.md`（新建）
- **验证结果**：
  - 文件写入成功且可读。
  - 环境快照与实测一致：`feature/ai-assistant`、ahead 2、工作树脏、1420=200、9876 schema=79。
  - 任务 ID 均可回溯到 PRD / phases / 模块文档中的“仍待/待开发”原文，无虚构完成项。
- **剩余限制**：
  - 本文件不能替代 `agent-harness-phases.md` 中的历史验收细节。
  - 工作树中大量未提交改动未做逐文件审计；HH-01 开始前应在任务内先核对与目标相关的现有实现事实。
  - 本轮未跑全量 `pnpm check`（无行为代码变更）；未提交/推送。

## 3. 进度记录

| 轮次 | 日期       | 任务  | 结果             | 备注                                                              |
| ---- | ---------- | ----- | ---------------- | ----------------------------------------------------------------- |
| 1    | 2026-09-23 | HH-00 | DONE             | 建立本进度源；环境已核；下一任务 HH-01                            |
| 2    | 2026-09-23 | HH-01 | DONE（最小纵切） | 枚举 kind 闭集后新增 `adapter_issue`；其余未建模来源保留在 HH-01R |
| 3    | 2026-09-23 | HH-02 | DONE             | 恢复页备份选择 + `prepare-restore` 一次性命令；跳过/指定已有备份  |

### 2.2 HH-01 一体化 Agent Inbox：`adapter_issue` 最小纵切

- **状态**：`DONE`
- **执行时间**：2026-09-23
- **本次完成内容**：
  - 枚举现有 `kind`：approval / access_request / review / automation_failure / knowledge_failure / generation_failure / provider_issue / maintenance_failure / agent_run / continuation。
  - 定位真实缺口：`agent_adapters` 健康/隔离终态失败会阻断 Agent Run，却未进入 Agent Inbox。
  - 按 DB 约束（`enabled ⇒ execution_ready=1 ⇒ healthy+verified`）只投影终态失败：`health_status ∈ {blocked,unhealthy}` 或 `isolation_status=unsupported`。
  - 新增 `kind=adapter_issue`、`adapter_issue_total`、左栏“本地 Agent 待处理”、空 scope `adapterIssueHandoff`（复用 `agentAdapterSettingsHandoff`）。
  - 同步 AI / Local Agent / 模块索引 / PRD / 功能架构 / harness-phases。
- **修改文件**：
  - `services/sidecar/internal/api/ai_agent_inbox.go`
  - `services/sidecar/internal/api/ai_agent_inbox_test.go`
  - `apps/web/src/api/aiAgentInbox.ts`
  - `apps/web/src/api/aiAgentInbox.test.ts`
  - `apps/web/src/lib/aiIssueHandoff.ts`
  - `apps/web/src/components/AiSessionRail.tsx`
  - `docs/modules/ai-assistant.md`
  - `docs/modules/local-agents.md`
  - `docs/modules/README.md`
  - `docs/opc-workspace-PRD.md`
  - `docs/functional-architecture.md`
  - `docs/plans/agent-harness-phases.md`
  - `docs/plans/agent-handoff.md`
- **验证结果**：
  - `go -C services/sidecar vet ./internal/api/` 通过。
  - `go -C services/sidecar test ./internal/api -run 'TestAIAgentInbox' -count=1` 通过（含新 `TestAIAgentInboxProjectsEnabledAdapterIssuesOnly`）。
  - `pnpm --filter @opc/web test -- src/api/aiAgentInbox.test.ts src/lib/aiIssueHandoff.test.ts src/components/AiSessionRail.test.tsx`：123 项通过。
  - `gofmt` 已格式化触及的 Go 文件。
- **剩余限制**：
  - 本轮未跑全量 `pnpm check` / 全量 Go 套件；运行中 1420/9876 在本轮中途已不可达，未加载新 Sidecar。
  - 其余尚未建模的失败/恢复来源（见 HH-01R）未在本纵切处理。
  - 真实桌面/Provider/安装包验收（ACC-01）未做。
  - 未提交/推送。

#### HH-01R 剩余未建模失败/恢复来源（OPEN）

- 枚举后仍未投影、需后续独立纵切的候选（非本切片范围）：委派受理前失败若未以 child generation 落库时的父会话可见性、文件 helper unknown 结果、业务导入失败、发票 PDF 失败等。开始前须先在代码中确认是否已有等价投影，避免重复建模。

### 2.3 HH-02 启动前备份选择

- **状态**：`DONE`
- **执行时间**：2026-09-23
- **本次完成内容**：
  - 恢复页 error 态增加“从备份恢复 / 跳过并重启”。
  - Go：`ListStartupRestoreChoices`（metadata-only）与 `PrepareStartupRestore`（运行锁 + 当前库回滚 + pending plan）；Sidecar 子命令 `list-restore-choices` / `prepare-restore --backup-id`。
  - Rust：`list_startup_restore_choices` / `schedule_startup_restore`（调用一次性 prepare-restore）。
  - Web：严格解析选择列表、invalid 不可选、二次勾选确认、成功提示重启。
  - 同步桌面 / 数据管理 / 设置 / harness-phases / 本进度源。
- **修改文件**：
  - `services/sidecar/internal/api/startup_restore_select.go`
  - `services/sidecar/internal/api/startup_restore_lease.go`
  - `services/sidecar/internal/api/startup_restore_select_test.go`
  - `services/sidecar/cmd/server/main.go`
  - `apps/desktop/src-tauri/src/startup_restore.rs`
  - `apps/desktop/src-tauri/src/lib.rs`
  - `apps/web/src/api/desktop.ts`
  - `apps/web/src/components/ServiceRecoveryGate.tsx`
  - `apps/web/src/components/ServiceRecoveryGate.test.tsx`
  - `docs/modules/desktop-platform.md`
  - `docs/modules/data-management.md`
  - `docs/modules/settings.md`
  - `docs/plans/agent-harness-phases.md`
  - `docs/plans/agent-handoff.md`
- **验证结果**：
  - Go：`TestListStartupRestoreChoices…` / `TestPrepareStartupRestore…` 通过；`go vet ./internal/api ./cmd/server` 通过；gofmt 已执行。
  - Rust：`cargo test … startup_restore` 3 项通过。
  - Web：`ServiceRecoveryGate.test.tsx` 10 项通过。
- **剩余限制**：
  - 未跑全量 `pnpm check` / 全量 Go 套件；1420/9876 不可达，未真机点选。
  - 真实安装包 / UAC / 断电 / 父进程崩溃仍属 ACC-01。
  - `prepare-restore` 会短暂打开当前库以创建回滚包；这是既有恢复安排语义，不是业务 ready。
  - 未提交/推送。

## 4. 下一任务

- **下一任务编号**：HH-03
- **任务名称**：业务导入非空目标实际合并 / 跨 schema 升级
- **开始前检查**：
  1. 重读本文件与 PRD T-14/T-04B、数据管理模块导入章节；
  2. 先写/更新 ADR 冻结冲突策略与 UUID 重映射边界；
  3. 只做一个最小完整纵切（一种合并方向）；
  4. 回滚包 + Go 导入测试 + 文档同步；不静默覆盖、失败关闭。
