# 收件箱与本地工作编排模块

> 实现状态截止：2026-08-27（依据当前实现）
>
> 版本边界：当前只有页面骨架。v0.1 交付纯本地人工受理、拆分、owner/person 分派与 owner 验收；agent Actor、Adapter 和 Agent Run 归入 v0.2。

## 定位与边界

收件箱不是被动通知列表，而是**本地工作受理与编排中心**：统一承接项目产出、任务风险、发票待办、回访和其他本地提醒，明确下一步工作，并持续跟进到完成。

对象职责必须分离：

| 对象           | 保存的事实                             | 不保存的事实             |
| -------------- | -------------------------------------- | ------------------------ |
| Inbox Item     | 来源、分诊、已读、稍后、解决策略       | 任务执行状态、当前负责人 |
| Task           | 工作内容、生命周期、完成条件、验收结果 | 收件箱展示状态           |
| Assignment     | 当前责任人和改派历史                   | 任务完成状态             |
| Agent Run      | 一次本地执行尝试                       | 任务是否验收通过         |
| Task Artifact  | 文本、文件、链接或结构化产出           | 任务是否完成             |
| Workflow Event | 追加式操作时间线                       | 当前业务状态的第二副本   |

核心边界：

- Inbox Item 不能直接分派。“分派收件箱项”必须原子地创建/关联 Task，再为 Task 创建 Assignment。
- 已读、未读和稍后提醒只是展示属性，不是工作流主状态。
- 收件箱进度从活动的必需任务实时派生，不保存第二份百分比。
- 项目、客户、发票继续维护各自状态；收件箱只管理由它们产生的待处理工作。
- 第一阶段不提供多人登录、云同步、远程领取、远程通知、邮件/消息自动发送或线上 Agent。

## 当前实现状态

当前状态为**页面骨架**。

### 已实现

- `/inbox` 路由、标题、固定“0 条未读”、空状态和“全部标为已读”按钮外观。
- 初始数据库中有任务、项目、客户、发票等业务表，可作为未来本地事件来源。

### 已知缺口

- “全部标为已读”没有行为，且没有收件箱列表、详情、筛选或真实计数。
- 当前 SQLite schema v10 保留 schema v7–v9 已交付的 Actor/Assignment、Task 六状态命令、Submission/Artifact 和可查询的 Workflow Event 时间线，并新增 Client 基础事实；仍没有 `inbox_items`、`inbox_item_tasks` 或 `reminders` 表，也没有收件箱拆分/分派消费、Client 活动事件或 Artifact follow-up 事件投影链路。
- 没有任何 Inbox/Reminder/Artifact API、Query 或 Mutation；Assignment API 已由任务/Actor 纵切提供，但当前收件箱页面没有调用。
- 没有来源事件、稳定去重键、调度器、拆分事务、派生进度、解决/忽略/重开或审计。
- 没有 Agent Adapter、Agent Run、能力令牌、取消/重试或崩溃恢复；这些也不属于 v0.1。

## 目标功能

### 事件受理与分诊

- 列表标签：待处理、跟进中、稍后提醒、待验收、已解决。
- 支持来源、优先级、截止时间、任务状态、项目和负责人筛选。
- 支持单条/全部已读、稍后提醒/恢复、手动工作项、一次性 Reminder。
- 详情展示来源快照、处理目标、完成条件、任务树、当前负责人、产出与时间线。

### 拆分、关联与分派

- 一次创建多条父子任务，或关联已有任务；每条可设置必需/可选、优先级、日期、项目和完成条件。
- v0.1 分派给 owner/person；person 明确显示“仅记录负责人，不会发送或同步”。
- 提交时在一个 SQLite 事务中创建 Task、关联、Assignment、审计并推进 Inbox Item；任一步失败全部回滚。
- 取消关联采用软取消字段，重新关联新建历史记录，不覆盖过去事实。

### 跟踪、验收与关闭

- 默认 `resolution_policy = all_required_tasks_done`；所有活动必需任务为 `done` 后派生解决。
- 零个必需任务不得以空集合自动解决。
- `cancelled / blocked / waiting_review` 或 Agent 失败不能满足自动解决。
- manual 解决、强制解决、忽略均需原因；强制解决二次确认并记录 `resolution_mode = forced`。
- resolved/dismissed 可重新打开；根据现有关联任务恢复为 `open` 或 `tracking`。
- owner 负责 v0.1 的验收与返工；Agent Run 成功也只能进入 `waiting_review`。

### 本地事件来源

- v0.1：一次性提醒到期、任务临期/逾期、任务阻塞、显式 follow-up 的项目任务产出、备份/迁移/Sidecar 故障。
- v0.2：Agent Runner 只追加失败/待验收 Workflow Event；内置自动化投影器是诊断 Inbox Item 的唯一生产者，统一使用 `agent-run:<run_id>:failed` 等 source_event_key。已有活动 Inbox 时更新现有项，不再新建。
- 对应模块交付后：发票临期/逾期、客户回访到期、项目里程碑。
- 所有调度事件使用稳定 `source_event_key`；重复扫描和重启不生成重复项。

## 关键用户流程

### 项目产出拆分

1. 项目交付类 Task 提交 Artifact，或 owner 显式标记 `requires_followup`。
2. Sidecar 以 Artifact ID 生成稳定事件键并创建 `open` Inbox Item。
3. owner 查看来源和完成条件，拆成修改、发布、客户确认等必需/可选 Task。
4. 系统原子建立任务、关系、Assignment 和 Workflow Event，Inbox Item 进入 `tracking`。
5. 所有必需 Task 验收完成后，Inbox Item 派生为 `resolved`。

### 发票待办（v0.4）

1. 项目到达开票节点、发票临期或逾期，调度器生成去重 Inbox Item。
2. owner 拆成准备发票、核对金额、准备催款内容等任务。
3. person 只记录线下责任；v0.2 本地 Agent 可准备草稿/PDF，但不能发送或确认付款。
4. owner 完成外部发送或付款确认，业务模块更新发票事实，再验收相关任务。

### 其他提醒

1. owner 创建一次性 Reminder，或系统检测任务临期/阻塞、备份失败等本地事件。
2. 到期后幂等生成 Inbox Item；Reminder 记录调度事实，Inbox Item 记录处理事实。
3. owner 选择立即处理、稍后提醒、忽略、关联已有任务或拆分新任务。
4. 稍后到期仅恢复可见性，不复制或重建工作项。

### 验收和返工

1. 人或 v0.2 Agent 提交 Artifact，Task 进入 `waiting_review`。
2. owner 在收件箱详情检查产出，选择接受或要求返工。
3. 返工保留旧产出、审核原因和 Agent Run；任务回到 `in_progress`。
4. 新一次 Agent 重试必须创建新 Run，不能覆盖失败记录。

## 数据/API/状态与事件

### v0.1 规划数据

- `inbox_items`：来源、`source_event_key`、优先级、主状态、解决策略、已读/分诊/稍后、解决/忽略信息、最小来源快照和版本。
- `inbox_item_tasks`：Task 关联、created/linked、必需标记、建立/取消关联历史。
- `reminders`：来源、触发时间、`scheduled / fired / cancelled`、事件键和生成的 Inbox Item。
- 依赖任务/Actor 基础：`actors`、`task_assignments`、`task_artifacts`、`workflow_events`。
- 所有新结构必须从当前 schema v10 之后追加 `011_...` 或更高版本迁移，不能修改任何已发布迁移。

### 主状态

```text
open → tracking → resolved
  └────────────→ dismissed
resolved / dismissed → open 或 tracking
```

`read_at` 与 `snoozed_until` 独立于主状态。“待验收”“Agent 处理中”“阻塞”是从 Task/Run 派生的筛选，不是 Inbox Item 新状态。

### v0.1 规划 API

- `GET/POST /api/v1/inbox-items`、`GET/PATCH /api/v1/inbox-items/:id`。
- 单条/全部已读、snooze/unsnooze、split、关联/软取消关联、resolve、force-resolve、dismiss、reopen。
- `GET/POST /api/v1/reminders` 与单条读取/改期/取消。
- Task Assignment、六个生命周期命令、Task events、Artifact 和 submit/review API 已由共享工作流基础提供；Inbox events API 与来源投影仍待 T-11。

### 幂等与并发

- 创建、拆分、分派、验收、返工、解决和忽略均支持 `Idempotency-Key`，记录请求摘要和可重放响应。
- 同一 key 携带不同请求体返回 `409 CONFLICT`；重放不能重复写 Workflow Event。
- 状态写入携带 `expected_version` 或 `If-Match`；并发旧写入返回 `409` 并要求刷新。
- 多态来源在 open/tracking 时默认阻止硬删除；来源允许删除后保留最小快照并显示“来源已不存在”。

### v0.2 Agent 扩展

- 增加 Agent Adapter、Agent Run、单次能力令牌、受控产出目录、取消/重试和中断恢复。
- Agent Run 状态为 `queued / running / succeeded / failed / cancelled / interrupted`。
- Agent 不使用 WebView Bearer Token，不直连 SQLite，不拥有任意 Shell/目录/网络能力；跨平台强制边界需 ADR 和验证后才能宣称成立。

## 与其他模块协作

| 模块     | 事件进入收件箱                                    | 收件箱反馈                                        |
| -------- | ------------------------------------------------- | ------------------------------------------------- |
| 任务     | 临期、逾期、阻塞、显式 follow-up 产出             | 创建/关联工单、分派、验收、返工；不直接改业务状态 |
| 项目     | 产出、交付/验收/开票节点                          | 跟踪后续工单；Project 状态保持独立                |
| 客户     | v0.4 回访到期、本地活动                           | 创建 followup Task 并跳回客户详情                 |
| 发票     | v0.4 临期、逾期、待开票                           | 准备/核对/催款工单；发送和付款确认仍由 owner      |
| Actor    | Assignment 记录 owner/person；v0.2 使用健康 agent | 展示当前负责人和改派历史                          |
| 今日     | 展示待处理、跟进、阻塞、待验收计数                | 提供带筛选的详情入口                              |
| 系统维护 | 备份、迁移、Sidecar 故障                          | 生成可追踪的维护工作项                            |

完整协作图参见[整体功能架构](../functional-architecture.md)。

## 分阶段实施

1. **前置：Actor/Task 工作流基础（已完成）**：actors、Assignment、六状态命令、Task Workflow Event、Artifact 与 manual 提交验收已由 T-18D D2 交付。
2. **T-11A 数据契约**：新增 Inbox Item、关联表、Reminder、索引、去重与乐观锁迁移。
3. **T-11B 人工受理**：列表、详情、已读/全部已读、稍后、手动提醒、解决、忽略和重开。
4. **T-11C 拆分与分派**：原子多任务拆分/关联、必需标记、owner/person 分派、派生进度和自动解决。
5. **T-11E v0.1 事件源**：显式 follow-up 产出、任务临期/阻塞和系统故障；逐项验证去重。
6. **T-11D v0.2 Agent**：健康 Adapter、Run、产出、取消/重试、人工验收、返工和崩溃恢复。
7. **后续业务事件**：随 v0.4 发票/回访和 v0.3 里程碑模块交付后启用。

## 验收标准

- 断开外网后，v0.1 人工受理、拆分、分派、跟踪、验收和归档完整可用。
- person 分派始终提示仅本地记录，不发送、不登录、不同步。
- 同一来源事件跨扫描、跨重启只生成一条 Inbox Item。
- 拆分任一步失败不遗留 Task、关联、Assignment 或 Workflow Event。
- 进度完全从活动必需任务派生；零必需任务不自动解决。
- blocked、cancelled、waiting_review 和 Agent 失败不会误触发自动解决。
- force-resolve、dismiss、返工和重开均有原因、确认和不可变审计。
- 并发改派、验收或解决拒绝旧写入；幂等重放不重复资源或事件。
- Actor 停用、来源删除和取消关联均保留可解释历史。
- v0.2 Agent 成功只进入 `waiting_review`；只有 owner 可接受，重试保留全部 Run 与 Artifact。
- 列表/详情覆盖分页、筛选、加载、空、错误、重试、键盘和真实浏览器操作。

## 相关代码/PRD链接

- [PRD：收件箱与本地工作编排中心](../opc-workspace-PRD.md#56-收件箱与本地工作编排中心)
- [PRD：本地工作编排数据表](../opc-workspace-PRD.md#本地工作编排数据表taskactord2-已实现inboxagent-仍规划)
- [PRD：架构决策记录（含 ADR-002）](../opc-workspace-PRD.md#d-架构决策记录)
- [整体功能架构](../functional-architecture.md)
- [InboxPage.tsx](../../apps/web/src/pages/InboxPage.tsx)
- [当前 API 路由](../../services/sidecar/internal/api/router.go)
- [初始数据库迁移](../../services/sidecar/internal/database/migrations/001_initial_schema.sql)
