# 收件箱与本地工作编排模块

> 实现状态截止：2026-08-28（依据当前代码与测试）
>
> 当前基线：app v0.1.0 / API v1 / SQLite schema v12。T-11A1 手工 `inbox_items` 事实与 T-11B 人工受理/分诊纵切已交付；任务关联、Reminder、来源投影和 Agent 仍属于后续阶段。

导航：[文档中心](../README.md) · [整体功能架构](../functional-architecture.md) · [PRD v2.4](../opc-workspace-PRD.md) · [任务](tasks.md) · [Actor 与分派](actors.md)

## 定位与边界

收件箱是本地工作受理与编排中心。Inbox Item 说明“为什么需要处理”以及当前分诊展示状态；真正可执行的工作仍由 Task 承担，责任变化由 Assignment 承担，追加式操作历史由 Workflow Event 承担。

对象职责必须分离：

| 对象           | 保存的事实                             | 不保存的事实             |
| -------------- | -------------------------------------- | ------------------------ |
| Inbox Item     | 来源、分诊、已读、稍后、解决策略       | 任务执行状态、当前负责人 |
| Task           | 工作内容、生命周期、完成条件、验收结果 | 收件箱展示状态           |
| Assignment     | 当前责任人和改派历史                   | 任务完成状态             |
| Agent Run      | 一次本地执行尝试                       | 任务是否验收通过         |
| Task Artifact  | 文本、文件、链接或结构化产出           | 任务是否完成             |
| Workflow Event | 追加式操作时间线                       | 当前业务状态的第二副本   |

当前版本继续遵守以下边界：

- Inbox Item 不能直接分派。后续“分派收件箱项”必须先在一个事务中创建或关联 Task，再为 Task 创建 Assignment。
- `read_at`、`snoozed_until` 与 Inbox Item 主状态彼此独立；已读或稍后都不等于任务完成。
- 收件箱不复制 Task 进度或负责人事实。
- 项目、客户、发票继续维护各自状态；当前手工 Inbox Item 不会自动改写这些对象。
- 第一阶段不提供多人登录、云同步、远程通知、邮件/消息自动发送、线上 Agent 或模型服务。

## 当前实现状态

当前模块为**部分完成**：手工条目的本地受理、查看、编辑、分诊和归档闭环已接真实 SQLite/API/UI；完整任务编排和自动事件来源尚未交付。

### 已交付：T-11A1 手工 Inbox Item 事实

- schema v12 通过 `012_inbox_items.sql` 新增 `inbox_items`，从 v11 升级时只追加表和索引，不改写已有业务事实。
- 当前公开创建 API 强制 `kind = manual`、`source_entity_type = manual`、`resolution_policy = manual`；不接受 `source_entity_id` 或 `source_event_key`。
- 迁移为未来事件来源预留 `event / reminder` kind、`all_required_tasks_done` 策略及 nullable `source_event_key`，并用仅覆盖非空值的部分唯一索引去重；这些预留值当前不能通过创建 API 使用。
- 主状态约束为 `open / tracking / resolved / dismissed`。当前手工创建固定进入 `open`；`tracking` 为后续任务关联保留，现有公开命令不会主动进入该状态。
- 标题为 trim 后 2–200 字符，摘要最多 10,000 字符，优先级为 `P0 / P1 / P2 / P3`，截止时间使用 RFC 3339 UTC。
- `payload_json` 必须是 JSON object；当前前端不暴露该字段，也不会以此伪造来源事实。
- 解决和忽略终态由数据库约束成组校验：责任 Actor、时间、原因和模式不能出现半套事实。

### 已交付：T-11B 人工受理与分诊

- `/inbox` 已接真实列表、全局待处理未读数、搜索、优先级筛选、分页，以及“待处理 / 稍后 / 已归档”三个视图。
- 支持新建手工条目、查看详情、编辑标题/摘要/优先级/截止时间、单条已读和快照式全部已读。
- 支持设置稍后时间、提前恢复、到期自动回到待处理视图；稍后只改变可见性，不复制条目。列表每 15 秒低频刷新，以服务端时钟重新判定到期可见性。
- 支持带原因解决、带原因忽略和重新打开。解决/忽略不会隐式标为已读；未读终态仍可直接标为已读，无需先重开。重新打开保留 `read_at` 与 `triaged_at`，清除终态事实和 `snoozed_until`，回到 `open`。
- 详情提供追加式活动时间线，覆盖 `created / updated / read / snoozed / unsnoozed / resolved / dismissed / reopened`。
- 列表、详情和时间线均有加载、空、错误与重试状态；表单和命令有校验、禁用态及并发冲突提示，编辑草稿在冲突刷新后保留。

### 明确未交付

- `inbox_item_tasks`、Task 创建/关联/软取消关联、父子任务拆分、必需标记、Assignment 消费、派生进度与自动解决；
- `reminders`、一次性调度器、改期/取消和到期生成 Inbox Item；
- Task/Project/Client/Invoice/系统故障等来源投影、Artifact `requires_followup` 消费和稳定事件扫描；
- `force-resolve`、来源删除协调、Inbox Item 硬删除；
- Sidebar 与 Today 的 Inbox 计数和带筛选跳转；
- Agent Actor、Adapter、Agent Run、自动执行、取消/重试、能力令牌和崩溃恢复；
- AI、LLM、自然语言解析、智能排程或自动报告。

当前代码虽然已经有 Task Assignment、六状态生命周期、Submission/Artifact 和 Task Event API，但本次 Inbox 纵切没有调用它们来拆分、关联、分派或验收任务。

## 当前用户流程

### 创建和编辑手工条目

1. 用户在收件箱点击“新建条目”。
2. 填写标题、说明、优先级和可选截止时间。
3. 前端只提交手工字段；Sidecar 固定补充 manual kind/source/policy，在同一事务中创建 Inbox Item 与 `created` Workflow Event。
4. 创建成功后打开真实详情。活动中的条目可编辑；归档条目必须先重新打开。
5. 有效编辑会写 `triaged_at`、递增版本并追加 `updated` 事件；完全相同的 PATCH 返回当前快照，不递增版本或重复写事件。

### 已读与快照式全部已读

- 单条 `read` 只在首次写入 `read_at`，不会写 `triaged_at`、改变主状态或清除稍后时间。
- 列表响应返回同一服务端时点的 `snapshot_at` 与 `server_now`。前端执行“全部标为已读”时，把当前响应的 `snapshot_at` 作为 `through_created_at` 提交。
- 批量命令把 `through_created_at` 作为时间截止，只处理 `created_at <= cutoff` 且 `updated_at <= cutoff`、当前处于 `open / tracking`、尚未读取，并且按该 cutoff 仍属于待处理可见范围的条目：`snoozed_until` 为空或不晚于截止时间。
- 归档项、截止时仍在稍后的项、`created_at` 晚于 cutoff 的新项，以及截止后发生编辑、分诊、重开等任何更新的项都不会被标记；当前页面的搜索、优先级或视图筛选也不会缩小批量范围。这是保守跳过新变化，不是对 cutoff 时历史状态的重建。
- 批量更新与每条 `read` 事件在一个事务中提交；任一事件写入失败时全部回滚。相同幂等键重放首次 `marked_count` 快照，不重复写事件。
- 本版是 `through_created_at` 时间截止，而不是 `created_at + ID/序列` 的不透明严格游标；纳秒 UTC 下极低概率同时间戳碰撞可能被同一截止范围纳入。若未来需要严格游标，应新增稳定序列/ID 组成的不透明快照令牌。

### 稍后与恢复

1. 用户设置一个晚于服务端当前时间的 RFC 3339 时间。
2. 条目保留 `open / tracking` 主状态，写入 `snoozed_until`；首次此类分诊会写 `triaged_at`。
3. 未来时间的条目进入“稍后”视图；列表每 15 秒低频刷新并按响应中的服务端时间重新查询，到达后无需调度器写库即可重新出现在“待处理”。
4. 用户可提前执行 `unsnooze` 清除时间并立即恢复；该操作不改变已读事实。

### 解决、忽略和重开

1. `resolve` 与 `dismiss` 只接受 `open / tracking`，均要求 trim 后非空且不超过 2,000 字符的原因。
2. 两个命令都会写入 `triaged_at`、清除稍后时间并进入相应终态，但不会写入 `read_at`。
3. 终态条目不能编辑或稍后；若终态仍未读，详情可直接执行 `read`，该操作不要求先重新打开，也不改变终态。
4. 终态条目无论已读与否均可 `reopen`。重开清除解决/忽略字段和稍后时间，状态固定回到 `open`；已读与分诊时间保留，便于解释历史。

## 列表与计数契约

| view      | 服务端范围                                                   |
| --------- | ------------------------------------------------------------ |
| `inbox`   | `open / tracking` 且未稍后，或 `snoozed_until <= server_now` |
| `snoozed` | `open / tracking` 且 `snoozed_until > server_now`            |
| `archive` | `resolved / dismissed`                                       |

- API 支持 `q`（标题/摘要）、`priority`、`page` 和 `page_size`；默认每页 50，最大 100，当前 UI 每页 30。
- 当前视图的 `meta.total` 会应用 view、搜索和优先级条件。
- `meta.unread_total` 始终是**全局当前待处理 `inbox` 视图**的未读数，不受当前 `view / q / priority` 影响，也不包含未来稍后项或归档项。
- 稳定排序依次为优先级 P0→P3、有截止时间优先、截止时间升序、创建时间倒序、ID 升序。

## 数据/API/状态与事件

### `inbox_items`（schema v12）

| 字段                                | 当前约束 / 说明                                                   |
| ----------------------------------- | ----------------------------------------------------------------- |
| `id`                                | UUID 主键                                                         |
| `kind`                              | 表约束 manual/event/reminder；当前创建 API 仅 manual              |
| `title / summary`                   | 标题 2–200；摘要最多 10,000                                       |
| `source_entity_type`                | 当前创建 API 固定 manual                                          |
| `source_entity_id`                  | 当前手工项必须为 null                                             |
| `source_event_key`                  | nullable；非空值受部分唯一索引保护；当前手工 API 禁止设置         |
| `source_deleted_at`                 | 当前手工项为 null；来源删除协调尚未实现                           |
| `priority`                          | P0 / P1 / P2 / P3                                                 |
| `status`                            | open / tracking / resolved / dismissed                            |
| `resolution_policy`                 | 表约束 manual/all_required_tasks_done；当前 API 仅 manual         |
| `due_at`                            | 可空 RFC 3339 UTC                                                 |
| `read_at / triaged_at`              | 相互独立的已读与分诊时间                                          |
| `snoozed_until`                     | 可空；未来值进入稍后视图，到期后按查询恢复                        |
| `resolved_* / resolution_*`         | resolved 终态的 owner、时间、原因和模式；当前命令只写 mode=manual |
| `dismissed_* / dismiss_reason`      | dismissed 终态的 owner、时间和原因                                |
| `payload_json`                      | 必须是 JSON object；当前 UI 不编辑                                |
| `version / created_at / updated_at` | 乐观并发版本与 UTC 时间                                           |

schema v12 尚未创建 `inbox_item_tasks` 或 `reminders`。`tracking`、event/reminder kind、派生策略及非空来源事件键是未来迁移/API 会使用的受约束空间，不代表对应工作流已经上线。

### 已实现 API

| 方法  | 路径                               | 契约摘要                                                               |
| ----- | ---------------------------------- | ---------------------------------------------------------------------- |
| GET   | `/api/v1/inbox-items`              | 三视图、搜索、优先级、分页、稳定排序；返回全局待处理未读数与快照时间   |
| POST  | `/api/v1/inbox-items`              | 新建 manual 条目；可选 `Idempotency-Key`；返回 `201`、数据和 `ETag`    |
| POST  | `/api/v1/inbox-items/read-all`     | 以 `through_created_at` 批量已读；可选幂等键；不受当前筛选缩小         |
| GET   | `/api/v1/inbox-items/:id`          | 详情、可用动作和 `ETag`                                                |
| PATCH | `/api/v1/inbox-items/:id`          | 编辑标题/摘要/优先级/截止时间；强制 `If-Match`；终态拒绝               |
| GET   | `/api/v1/inbox-items/:id/events`   | 分页时间线，默认 50/最大 100；返回 `ETag` 与 `meta.inbox_item_version` |
| POST  | `/api/v1/inbox-items/:id/read`     | 单条已读；强制 `If-Match`，可选幂等键                                  |
| POST  | `/api/v1/inbox-items/:id/snooze`   | 设置未来 `snoozed_until`；强制 `If-Match`，可选幂等键                  |
| POST  | `/api/v1/inbox-items/:id/unsnooze` | 清除稍后时间；强制 `If-Match`，可选幂等键                              |
| POST  | `/api/v1/inbox-items/:id/resolve`  | 必填原因，manual 解决；强制 `If-Match`，可选幂等键；不隐式已读         |
| POST  | `/api/v1/inbox-items/:id/dismiss`  | 必填原因，忽略归档；强制 `If-Match`，可选幂等键；不隐式已读            |
| POST  | `/api/v1/inbox-items/:id/reopen`   | 重新打开并保留 read/triaged；强制 `If-Match`，可选幂等键               |

当前没有 split、task relation、Reminder、force-resolve、自动投影或删除路由。

### 幂等、并发与事务

- 创建、单条命令和全部已读支持 `Idempotency-Key`；同 key/endpoint/规范请求重放首次状态码与数据快照，同 key 不同请求返回 `409 IDEMPOTENCY_CONFLICT`。
- 单条命令的请求摘要包含 expected version，并在读取当前数据库版本之前检查幂等快照，因此一次已经成功但响应丢失的命令可用原 key/原版本安全重放。
- PATCH 和所有单条命令使用资源 `ETag`/`If-Match`；缺失前置条件和旧版本分别被拒绝，不自动覆盖其他窗口的新事实。
- 业务事实、Workflow Event 与幂等快照在同一个 SQLite 事务中提交；事件失败不遗留半完成状态。

### Workflow Event

- Inbox 事件使用 `aggregate_type = inbox_item`、条目 ID 作为 aggregate ID，并记录内置 owner Actor、request ID、前后 JSON 快照、命令序号和 UTC 时间。
- 当前 action 为 `created / updated / read / snoozed / unsnoozed / resolved / dismissed / reopened`。
- 事件沿用 schema v8/v9 的不可修改、不可删除保护；事件列表只读，不作为当前 Inbox Item 状态的第二副本。

## 与其他模块协作

| 模块     | 当前协作事实                                        | 后续扩展                                                |
| -------- | --------------------------------------------------- | ------------------------------------------------------- |
| 任务     | 当前 Inbox 不创建、关联、分派或改变 Task            | T-11A2/C 增加关系、拆分、Assignment、派生进度和自动解决 |
| 项目     | 当前没有项目来源投影                                | 显式 follow-up 产出和项目节点使用稳定事件键投影         |
| 客户     | 当前没有客户活动或回访来源                          | v0.4 回访到期生成去重 Inbox Item                        |
| 发票     | 当前没有财务来源                                    | v0.4 临期/逾期及开票节点生成本地待办                    |
| Actor    | 当前人工命令审计记录固定 owner；不创建新 Assignment | 任务拆分阶段复用已交付 owner/person Assignment          |
| 今日     | 当前没有 Inbox 派生计数                             | 后续展示待处理/跟进/阻塞/待验收计数与筛选跳转           |
| 系统维护 | 当前不投影备份、迁移或 Sidecar 故障                 | 对应故障链路完成后生成可追踪维护项                      |
| Agent    | 未实现                                              | v0.2 只通过受控 Adapter/Run 产生待验收或失败事件        |

完整协作图参见[整体功能架构](../functional-architecture.md)。

## 后续实施顺序

1. **前置 Actor/Task 工作流基础（已完成）**：Actor、Assignment、六状态命令、Task Event、manual Submission/Artifact。
2. **T-11A1 手工 Inbox Item 数据契约（已完成）**：schema v12、约束、索引和迁移保留测试。
3. **T-11B 人工受理与分诊（已完成）**：真实列表/详情/编辑、已读/快照式全部已读、稍后/恢复、解决/忽略/重开及事件 UI/API。
4. **T-11A2 Task 关系事实**：新增 `inbox_item_tasks`、活动关系部分唯一、软取消和来源删除协调。
5. **T-11A3 Reminder 事实**：新增 `reminders`、一次性调度、改期/取消及幂等到期投影。
6. **T-11C 拆分与分派**：原子多任务拆分/关联、必需标记、owner/person 分派、派生进度和自动解决。
7. **T-11E v0.1 来源投影**：显式 follow-up 产出、任务临期/阻塞和系统故障；逐项验证稳定事件键。
8. **T-11D v0.2 Agent**：健康 Adapter、Run、受控产出、取消/重试、人工验收、返工和崩溃恢复。
9. **后续业务事件**：随 v0.3/v0.4 路线图、发票和回访模块交付后启用。

## 验收状态

### 当前纵切已覆盖

- [x] 断开外网时手工创建、列表、详情、编辑、已读、稍后和归档命令只依赖本地 Sidecar/SQLite。
- [x] schema v11→v12 升级保留既有事实；fresh v12 约束、nullable 部分唯一来源键和外键检查有迁移测试。
- [x] 创建与命令幂等重放不重复资源或事件；异体复用被拒绝。
- [x] PATCH/命令拒绝旧版本；事件失败时事实与批量已读整体回滚。
- [x] 全局未读计数不受页面筛选影响；时间截止式全部已读只处理创建与最后更新时间都不晚于 cutoff、且按 cutoff 仍属于待处理可见范围的未读，截止后更新项保守跳过。
- [x] 解决/忽略原因必填且不隐式已读；重开保留 read/triaged。
- [x] 前端覆盖分页、筛选、加载、空、错误、重试和冲突草稿保留的自动测试。

### 完整人工编排仍需验收

- [ ] Task 关联/拆分、Assignment 和审计在一个事务中完成，失败不遗留部分事实。
- [ ] 进度完全从活动必需 Task 派生，零必需任务不自动解决。
- [ ] blocked、cancelled、waiting_review 和失败任务不误触发自动解决。
- [ ] Reminder 与每种来源事件跨扫描、跨重启只生成一条 Inbox Item。
- [ ] 来源删除、关系软取消和重新关联都保留可解释历史。
- [ ] Sidebar/Today 计数、真实浏览器键盘/焦点、长列表和窄屏视觉完成专项验收。
- [ ] v0.2 Agent 成功只进入 `waiting_review`，只有 owner 可接受，重试保留全部 Run 与 Artifact。

## 相关代码/PRD 链接

- [PRD：收件箱与本地工作编排中心](../opc-workspace-PRD.md#56-收件箱与本地工作编排中心)
- [整体功能架构](../functional-architecture.md)
- [schema v12 Inbox 迁移](../../services/sidecar/internal/database/migrations/012_inbox_items.sql)
- [Inbox API](../../services/sidecar/internal/api/inbox_items.go)
- [Inbox API 测试](../../services/sidecar/internal/api/inbox_items_test.go)
- [Inbox 迁移测试](../../services/sidecar/internal/database/inbox_migration_test.go)
- [Inbox 页面](../../apps/web/src/pages/InboxPage.tsx)
- [Inbox 详情](../../apps/web/src/components/InboxItemDetailModal.tsx)
- [Inbox 时间线](../../apps/web/src/components/InboxItemEventsSection.tsx)
