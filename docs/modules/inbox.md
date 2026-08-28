# 收件箱与本地工作编排模块

> 实现状态截止：2026-08-28（依据当前代码与测试）
>
> 当前基线：app v0.1.0 / API v1 / SQLite schema v16。T-11A1/B 手工受理分诊、T-11A2 已有 Task 关系、T-11A3 一次性 Reminder，以及 T-11C 批量拆分/分派/自动结清已交付；schema v16 不改 Inbox 契约；非 Reminder 来源投影和 Agent 仍属于后续阶段。

导航：[文档中心](../README.md) · [整体功能架构](../functional-architecture.md) · [PRD v3.7](../opc-workspace-PRD.md) · [任务](tasks.md) · [Actor 与分派](actors.md) · [本地提醒](reminders.md)

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

当前模块为**部分完成**：手工受理分诊、已有 Task 关系、一次性 Reminder、批量拆分分派、自动结清/重开和例外强制解决，以及 T-11F Sidebar/Today 运营计数与风险深链筛选均已接真实 SQLite/API/UI。非 Reminder 自动事件来源和 Agent 尚未交付。

### 已交付：T-11A1 手工 Inbox Item 事实

- schema v12 通过 `012_inbox_items.sql` 新增 `inbox_items`，从 v11 升级时只追加表和索引，不改写已有业务事实。
- 当前公开创建 API 强制 `kind = manual`、`source_entity_type = manual`、`resolution_policy = manual`；不接受 `source_entity_id` 或 `source_event_key`。
- 迁移为来源投影预留 `event / reminder` kind、`all_required_tasks_done` 策略及 nullable `source_event_key`，并用仅覆盖非空值的部分唯一索引去重。公开 Inbox 创建 API 仍不能使用这些值；schema v14 的内部 Reminder 调度器已经使用 `kind=reminder` 和稳定事件键。
- 主状态约束为 `open / tracking / resolved / dismissed`。当前手工创建固定进入 `open`；schema v13 的显式 Task 关系命令已经使用 `tracking`，受理/分诊命令本身不会隐式创建关系或进入该状态。
- 标题为 trim 后 2–200 字符，摘要最多 10,000 字符，优先级为 `P0 / P1 / P2 / P3`，截止时间使用 RFC 3339 UTC。
- `payload_json` 必须是 JSON object；当前前端不暴露该字段，也不会以此伪造来源事实。
- 解决和忽略终态由数据库约束成组校验：责任 Actor、时间、原因和模式不能出现半套事实。

### 已交付：T-11B 人工受理与分诊

- `/inbox` 已接真实列表、全局待处理未读数、搜索、优先级筛选、分页，以及“待处理 / 稍后 / 已归档”三个视图。
- 支持新建手工条目、查看详情、编辑标题/摘要/优先级/截止时间、单条已读和快照式全部已读。
- 支持设置稍后时间、提前恢复、到期自动回到待处理视图；稍后只改变可见性，不复制条目。列表每 15 秒低频刷新，以服务端时钟重新判定到期可见性。
- 支持带原因解决、带原因忽略和重新打开。解决/忽略不会隐式标为已读；未读终态仍可直接标为已读，无需先重开。重新打开保留 `read_at` 与 `triaged_at`，清除终态事实和 `snoozed_until`，并按是否存在活动 Task 关系进入 `tracking / open`。
- 详情提供追加式活动时间线，覆盖 `created / updated / read / snoozed / unsnoozed / resolved / dismissed / reopened`。
- 列表、详情和时间线均有加载、空、错误与重试状态；表单和命令有校验、禁用态及并发冲突提示，编辑草稿在冲突刷新后保留。

### 已交付：T-11A2 已有 Task 关系

- schema v13 通过 `013_inbox_item_tasks.sql` 新增活动/历史关系、required、稳定 position、软解除事实、原 Task ID/标题快照，以及活动关系阻止 Task 硬删除的数据库保护；v12→v13 是加法迁移，不改写既有业务事实或创建 demo 关系。
- 详情可查询全部活动关系和分页历史；服务端实时 JOIN 当前 Task，返回 Task 状态/版本以及 `active_total / required_total / required_done / required_remaining / required_blocked / required_waiting_review / required_cancelled / percent / all_required_done`。零个 required 时 `percent = null`、`all_required_done = false`，不会触发空集合完成。Task 状态与百分比不复制到 Inbox 表。
- 支持把已有 Task 关联到活动 Inbox Item、修改活动关系的 `is_required`，以及填写原因软解除。重新关联同一 Task 创建新关系行，不恢复或覆盖旧历史。
- 第一条活动关系使 `open → tracking`；解除最后一条活动关系使 `tracking → open`；归档项重新打开时有活动关系进入 `tracking`，否则进入 `open`。关系命令不自动已读、不清除稍后、不改变 Task 生命周期，也不创建 Assignment。
- 关系 POST/PATCH/DELETE 使用 Inbox `ETag / If-Match` 和可选 `Idempotency-Key`，业务事实、Inbox 状态/version 与 `task_linked / task_requirement_changed / task_unlinked` 事件同事务提交。关系命令不递增 Task version。
- 任一活动 Inbox 关系会使 Task 硬删除返回 `409 TASK_HAS_ACTIVE_INBOX_RELATIONS`。带原因软解除后 Task 可以删除；历史行的实时 `task_id` 置空，但 `task_ref_id / task_title_snapshot` 与事件继续保留。

### 已交付：T-11A3 一次性本地 Reminder

- schema v14 新增独立 `reminders` 调度事实；公开 API 提供创建、分页/搜索/状态查询、详情、scheduled 编辑和带原因取消。
- Sidecar ready 前先补扫到期项，运行中每 15 秒扫描最多 100 条；以 `reminder:<id>:due` 为稳定事件键在同一事务中生成或复用一个 Reminder Inbox Item、记录 system 事件并把 Reminder 标记为 fired。
- Inbox Item 使用 `kind/source_entity_type=reminder`、指向 Reminder ID、继承标题/摘要/优先级/触发时间，并保持 `resolution_policy=manual`。用户随后在普通 Inbox 流程中阅读、稍后、解决、忽略或关联 Task，不反向修改 fired Reminder。
- Reminder 管理器从 Inbox 页打开，提供待提醒/已触发/已取消模块、搜索、分页、新建、编辑/改期、带原因取消和触发后跳转 Inbox。重复提醒、系统原生通知和其他业务来源投影仍未交付。

### 已交付：T-11C 拆分、分派与自动结清

- schema v15 的 `015_inbox_task_orchestration.sql` 为自动完成规则增加查询索引和数据库保护：自动解决必须至少有一个活动必需 Task，且全部处于 `done`；不改写既有业务数据或创建 demo 记录。
- `POST /api/v1/inbox-items/:id/split` 在一个 SQLite 事务内创建 1–20 个 Task、父子关系、标签、`created` Inbox 关系、初始 owner/person Assignment、manual review 的 owner reviewer，以及 Task/Inbox 审计事件。任一字段、引用或写入失败时全部回滚。
- 拆分面板支持任务名称、说明、类型、优先级、项目、完成条件、父任务、必需标记、负责人和验收策略；父任务只能引用本批次中更早的任务，避免环和悬空引用。
- `all_required_tasks_done` 策略由统一 reconciliation 维护：至少一个活动必需 Task 且全部 `done` 时由 system Actor 自动解决；自动解决后，任一必需 Task 因重开、返工等离开 `done` 会自动恢复为 `tracking`。
- Task 生命周期命令、产出提交/验收，以及 Inbox 关系的新增、required 修改、解除和拆分都会调用同一 reconciliation；进度仍实时来自 Task，不复制第二份状态。
- 普通 `resolve` 不能绕过自动策略的未完成必需任务。危险操作 `force-resolve` 只用于自动策略，要求显式 `confirm=true` 和原因，并以 owner Actor、`forced` mode 与 `force_resolved` 事件留下不可变审计；手工/强制解决不会因后续 Task 变化自动重开。
- 前端失败时保留拆分草稿；写命令使用 Inbox `If-Match` 与稳定幂等键，成功后统一失效 Inbox、关系、Task、Today 和 Project 查询。

### 已交付：T-11F 运营计数与风险深链

- `GET /api/v1/stats/inbox` 在查询时从当前可见的 `open / tracking` 条目与活动必需 Task 实时派生 `pending / unread / tracking / blocked / waiting_review`；返回同一服务端 `server_now`。
- `snoozed_until > server_now` 的未来稍后项、resolved/dismissed 归档项、已解除关系、可选 Task 和已删除 Task 不进入相应当前风险计数；同一 Inbox 有多个同状态 Task 只计一次。
- 列表新增 `risk=tracking|blocked|waiting_review` 服务端筛选，blocked/waiting_review 使用活动必需关系和实时 Task 状态；全局 `unread_total` 继续不受 risk/search/priority 缩小。
- Sidebar 显示待处理徽标，Today 显示待处理/跟进中/待验收/有阻塞并跳转风险深链；统计 Query 位于 Inbox Query 前缀下，写入后统一失效并每 15 秒刷新。

### 明确未交付

- 重复 Reminder、系统原生通知，以及 Task/Project/Client 等业务来源自动创建 Reminder；
- Task/Project/Client/Invoice/系统故障等来源投影、Artifact `requires_followup` 消费和稳定事件扫描；
- `source_entity_type=task` 等多态来源删除协调、Inbox Item 硬删除；
- Agent Actor、Adapter、Agent Run、自动执行、取消/重试、能力令牌和崩溃恢复；
- AI、LLM、自然语言解析、智能排程或自动报告。

T-11C 只编排用户显式提交的 Task 草稿，不自动生成任务内容，不调用 AI/LLM，也不改变 owner/person 的本地责任记录边界。

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
4. 终态条目无论已读与否均可 `reopen`。重开清除解决/忽略字段和稍后时间；有活动 Task 关系时进入 `tracking`，否则进入 `open`，并保留已读与分诊时间。

### 关联已有 Task

1. 用户在活动 Inbox Item 详情打开“关联任务”，从真实 Task 查询结果中选择已有 Task，并明确是否为必需任务。
2. 前端提交 Task ID、`is_required`、当前 Inbox `If-Match` 和稳定幂等键；服务端在同一事务创建 `relation_type=linked` 的关系、必要时把 `open` 推进为 `tracking`、递增 Inbox version 并写 `task_linked` 事件。
3. 关系列表的 active 部分不分页且最多 100 条；history 使用 `page/page_size` 分页。服务端实时读取 Task 状态，因此显示进度不依赖 Inbox 中的缓存字段。
4. 修改 required 使用同一路径 PATCH，并写 `task_requirement_changed`；它只改变关系事实，不在 A2 自动解决 Inbox Item。
5. 解除关系必须填写原因。DELETE 软写 `unlinked_* / unlink_reason` 与 `task_unlinked` 事件；最后一条活动关系解除时 `tracking → open`。
6. 重新关联同一 Task 创建新的关系 ID 和 linked 时间。历史记录保留此前 required、position、解除人、解除时间及原因。

### 关联 Task 的硬删除

1. Task 仍有活动 Inbox 关系时，Task DELETE 返回 `TASK_HAS_ACTIVE_INBOX_RELATIONS`，不会移动 Artifact 文件或删除 Task 聚合。
2. 用户回到对应 Inbox Item，带原因软解除全部活动关系。
3. Task 随后可按原有 `If-Match`、Focus Session 和 Artifact 删除契约硬删除。
4. 已解除历史关系的 nullable `task_id` 置空；不可变 `task_ref_id` 与 `task_title_snapshot` 继续用于显示“原任务已删除”。这不是 T-11E 的多态来源删除协调。

### 拆分、分派与自动结清

1. 用户在活动 Inbox Item 详情选择“拆分并分派”，填写有序 Task 草稿。每项可引用本批次更早的父任务，并选择 owner/person 负责人、项目、验收策略和是否必需。
2. 前端提交 Inbox 当前版本和稳定幂等键。Sidecar 先完整校验，再在一个事务内创建 Task、标签、层级、Assignment、reviewer、`created` 关系与审计；失败时不保留部分数据。
3. 提交可保留 `manual`，也可切换 `all_required_tasks_done`。自动策略必须至少有一个必需 Task；所有活动必需 Task 完成后 system 自动解决条目。
4. 必需 Task 处于 `todo / in_progress / blocked / waiting_review / cancelled` 时均不自动解决。自动解决后若必需 Task 通过 reopen、返工或其他受控命令离开 `done`，条目自动回到 `tracking`。
5. 若业务确实无需等待，用户展开“例外：强制解决”，填写原因并二次确认。该命令只作用于自动策略，保留未完成 Task，并记录 `forced` mode 与不可变事件。

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

### `inbox_items`（schema v12，在当前 schema v16 延续）

| 字段                                | 当前约束 / 说明                                                                       |
| ----------------------------------- | ------------------------------------------------------------------------------------- |
| `id`                                | UUID 主键                                                                             |
| `kind`                              | 表约束 manual/event/reminder；公开创建 API 仅 manual，内部 Reminder 投影使用 reminder |
| `title / summary`                   | 标题 2–200；摘要最多 10,000                                                           |
| `source_entity_type`                | 当前创建 API 固定 manual                                                              |
| `source_entity_id`                  | 当前手工项必须为 null                                                                 |
| `source_event_key`                  | nullable；非空值受部分唯一索引保护；当前手工 API 禁止设置                             |
| `source_deleted_at`                 | 当前手工项为 null；来源删除协调尚未实现                                               |
| `priority`                          | P0 / P1 / P2 / P3                                                                     |
| `status`                            | open / tracking / resolved / dismissed                                                |
| `resolution_policy`                 | manual/all_required_tasks_done；公开新建仍为 manual，T-11C 拆分可切换自动策略         |
| `due_at`                            | 可空 RFC 3339 UTC                                                                     |
| `read_at / triaged_at`              | 相互独立的已读与分诊时间                                                              |
| `snoozed_until`                     | 可空；未来值进入稍后视图，到期后按查询恢复                                            |
| `resolved_* / resolution_*`         | resolved 终态事实；mode 为 manual/automatic/forced，自动模式使用 system Actor         |
| `dismissed_* / dismiss_reason`      | dismissed 终态的 owner、时间和原因                                                    |
| `payload_json`                      | 必须是 JSON object；当前 UI 不编辑                                                    |
| `version / created_at / updated_at` | 乐观并发版本与 UTC 时间                                                               |

schema v13 不重建 `inbox_items`；schema v14 由 Reminder 调度器使用既有来源字段；schema v15 增加自动结清校验 trigger 和 required 查询索引，不重建该表。event kind 和其他来源投影仍是受约束的未来空间。

### `inbox_item_tasks`（schema v13）

| 字段                                 | 当前约束 / 说明                                                        |
| ------------------------------------ | ---------------------------------------------------------------------- |
| `id`                                 | UUID 主键                                                              |
| `inbox_item_id`                      | Inbox Item 外键                                                        |
| `task_ref_id`                        | 不可变原 Task UUID；Task 删除后仍保留                                  |
| `task_id`                            | nullable 实时 Task 外键，`ON DELETE SET NULL`                          |
| `task_title_snapshot`                | 建立关系时保存的标题；Task 删除后用于解释历史                          |
| `relation_type`                      | linked / created；A2 关联已有 Task 使用 linked，T-11C 拆分使用 created |
| `is_required`                        | 0/1；修改后立即参与自动策略 reconciliation                             |
| `position`                           | 大于等于 1；单条关系按末尾追加，活动列表稳定排序                       |
| `linked_by_actor_id / linked_at`     | 当前固定内置 owner 与 UTC 时间                                         |
| `unlinked_by_actor_id / unlinked_at` | 软解除 Actor/时间；与原因成组出现                                      |
| `unlink_reason`                      | trim 后 1–1,000 字符；历史行不可通过重新关联覆盖                       |

- 活动关系定义为 `unlinked_at IS NULL` 且 `task_id IS NOT NULL`；同 Inbox/Task 只允许一条活动关系，active 总数上限为 100。
- history 按 `unlinked_at DESC, linked_at DESC, id DESC` 稳定分页；重新关联写新行。Task 硬删除只允许作用于已解除关系，随后 `task_id` 置空，快照字段不变。
- 不增加 Task.version→Inbox.version trigger；关系 GET 每次实时 JOIN Task。Task/关系写命令在应用事务结束前显式调用统一 reconciliation。

### 已实现 API

| 方法   | 路径                                     | 契约摘要                                                                                               |
| ------ | ---------------------------------------- | ------------------------------------------------------------------------------------------------------ |
| GET    | `/api/v1/inbox-items`                    | 三视图、搜索、优先级、risk、分页、稳定排序；返回全局待处理未读数与快照时间                             |
| GET    | `/api/v1/stats/inbox`                    | 实时派生 pending/unread/tracking/blocked/waiting_review 与 server_now                                  |
| POST   | `/api/v1/inbox-items`                    | 新建 manual 条目；可选 `Idempotency-Key`；返回 `201`、数据和 `ETag`                                    |
| POST   | `/api/v1/inbox-items/read-all`           | 以 `through_created_at` 批量已读；可选幂等键；不受当前筛选缩小                                         |
| GET    | `/api/v1/inbox-items/:id`                | 详情、可用动作和 `ETag`                                                                                |
| PATCH  | `/api/v1/inbox-items/:id`                | 编辑标题/摘要/优先级/截止时间；强制 `If-Match`；终态拒绝                                               |
| GET    | `/api/v1/inbox-items/:id/events`         | 分页时间线，默认 50/最大 100；返回 `ETag` 与 `meta.inbox_item_version`                                 |
| POST   | `/api/v1/inbox-items/:id/read`           | 单条已读；强制 `If-Match`，可选幂等键                                                                  |
| POST   | `/api/v1/inbox-items/:id/snooze`         | 设置未来 `snoozed_until`；强制 `If-Match`，可选幂等键                                                  |
| POST   | `/api/v1/inbox-items/:id/unsnooze`       | 清除稍后时间；强制 `If-Match`，可选幂等键                                                              |
| POST   | `/api/v1/inbox-items/:id/resolve`        | 必填原因，manual 解决；强制 `If-Match`，可选幂等键；不隐式已读                                         |
| POST   | `/api/v1/inbox-items/:id/dismiss`        | 必填原因，忽略归档；强制 `If-Match`，可选幂等键；不隐式已读                                            |
| POST   | `/api/v1/inbox-items/:id/reopen`         | 重新打开并保留 read/triaged；强制 `If-Match`，可选幂等键                                               |
| GET    | `/api/v1/inbox-items/:id/tasks`          | 返回 `data.active/history`；history 分页，meta 含 Inbox version 与实时 progress，响应携带 Inbox `ETag` |
| POST   | `/api/v1/inbox-items/:id/tasks/:task_id` | body `{is_required}`；关联已有 Task，强制 Inbox `If-Match`，可选幂等键，第一条关系进入 tracking        |
| PATCH  | `/api/v1/inbox-items/:id/tasks/:task_id` | body `{is_required}`；修改活动关系 required，强制 Inbox `If-Match`，可选幂等键                         |
| DELETE | `/api/v1/inbox-items/:id/tasks/:task_id` | body `{reason}`；带原因软解除，强制 Inbox `If-Match`，可选幂等键，最后关系回到 open                    |
| POST   | `/api/v1/inbox-items/:id/split`          | 原子创建 1–20 个 Task、层级、Assignment、created 关系与审计；强制 Inbox `If-Match`，可选幂等键         |
| POST   | `/api/v1/inbox-items/:id/force-resolve`  | body `{confirm:true,reason}`；仅自动策略的例外解决；强制 Inbox `If-Match`，可选幂等键                  |

关系 GET 返回 `{data:{active,history},meta:{page,page_size,total,inbox_item_version,progress}}`；`page/page_size` 只作用于 history。单条关系命令返回 `{inbox_item,relation,progress}`；split 返回 `{inbox_item,tasks,relations,assignments,progress}`。Reminder 使用独立路由和内部到期投影；当前仍没有非 Reminder 自动投影或 Inbox 删除路由。

### 幂等、并发与事务

- 创建、单条命令和全部已读支持 `Idempotency-Key`；同 key/endpoint/规范请求重放首次状态码与数据快照，同 key 不同请求返回 `409 IDEMPOTENCY_CONFLICT`。
- Task 关系 POST/PATCH/DELETE 同样保存规范化请求摘要和首次响应；摘要包含 Inbox expected version、Task ID 与 required/reason。重放发生在当前关系/Inbox 版本检查前，不重复关系或事件。
- 单条命令的请求摘要包含 expected version，并在读取当前数据库版本之前检查幂等快照，因此一次已经成功但响应丢失的命令可用原 key/原版本安全重放。
- PATCH 和所有单条命令使用资源 `ETag`/`If-Match`；缺失前置条件和旧版本分别被拒绝，不自动覆盖其他窗口的新事实。
- 关系写入以 Inbox 为聚合边界：成功只递增 Inbox version，不递增 Task version；Task 在关系提交前必须仍存在，Task 删除在同一 SQLite 写边界内检查活动关系。
- 关系 GET 实时 JOIN Task；没有 Task.version→Inbox.version 传播 trigger。Task 生命周期、产出验收和关系写入在各自事务内调用统一 reconciliation，前端同时失效相关查询。
- 业务事实、Workflow Event 与幂等快照在同一个 SQLite 事务中提交；事件失败不遗留半完成状态。

### Workflow Event

- Inbox 事件使用 `aggregate_type = inbox_item`、条目 ID 作为 aggregate ID，并记录内置 owner Actor、request ID、前后 JSON 快照、命令序号和 UTC 时间。
- 当前 action 另包含 `tasks_split / automatically_resolved / automatically_reopened / force_resolved`；拆分产生的 Task/Assignment 也写各自聚合事件。
- 关系事件的前后快照包含关系、读取时 Task 摘要、实时进度、Inbox 状态/version 与可选解除原因；这些是不可变审计快照，不是可写的 Task 或 Inbox 第二事实源。
- 事件沿用 schema v8/v9 的不可修改、不可删除保护；事件列表只读，不作为当前 Inbox Item 状态的第二副本。

## 与其他模块协作

| 模块     | 当前协作事实                                                                             | 后续扩展                                         |
| -------- | ---------------------------------------------------------------------------------------- | ------------------------------------------------ |
| 任务     | 可关联已有 Task，或原子拆分并建立父子 Task/Assignment；任务写入触发 Inbox reconciliation | 来源投影、更多筛选与跨模块统计继续扩展           |
| 项目     | 当前没有项目来源投影                                                                     | 显式 follow-up 产出和项目节点使用稳定事件键投影  |
| 客户     | 当前没有客户活动或回访来源                                                               | v0.4 回访到期生成去重 Inbox Item                 |
| 发票     | 当前没有财务来源                                                                         | v0.4 临期/逾期及开票节点生成本地待办             |
| Actor    | owner 执行拆分/强制解决；owner/person 可成为初始负责人；system 执行自动结清/重开         | Agent Actor 仍延后                               |
| 今日     | 已展示待处理/跟进/阻塞/待验收实时计数并支持风险筛选深链                                  | 随 T-11E 来源投影自然纳入更多业务事件            |
| 系统维护 | 当前不投影备份、迁移或 Sidecar 故障                                                      | 对应故障链路完成后生成可追踪维护项               |
| Agent    | 未实现                                                                                   | v0.2 只通过受控 Adapter/Run 产生待验收或失败事件 |

完整协作图参见[整体功能架构](../functional-architecture.md)。

## 后续实施顺序

1. **前置 Actor/Task 工作流基础（已完成）**：Actor、Assignment、六状态命令、Task Event、manual Submission/Artifact。
2. **T-11A1 手工 Inbox Item 数据契约（已完成）**：schema v12、约束、索引和迁移保留测试。
3. **T-11B 人工受理与分诊（已完成）**：真实列表/详情/编辑、已读/快照式全部已读、稍后/恢复、解决/忽略/重开及事件 UI/API。
4. **T-11A2 Task 关系事实（已完成）**：schema v13、活动/历史关系、实时进度、已有 Task 关联、required 修改、带原因软解除、状态联动、事件，以及关联 Task 硬删除互锁和历史快照。多态来源删除协调不属于 A2。
5. **T-11A3 Reminder 事实（已完成）**：schema v14、创建/查询/编辑/取消、启动补偿、15 秒扫描、稳定事件键与幂等到期 Inbox 投影。
6. **T-11C 拆分与分派（已完成）**：原子多任务/父子拆分、owner/person Assignment、统一 reconciliation、自动解决/重开和 force-resolve。
7. **T-11F 运营计数（已完成）**：实时统计 API、risk 列表筛选、Sidebar 徽标和 Today 风险卡。
8. **T-11E v0.1 来源投影**：显式 follow-up 产出、任务临期/阻塞和系统故障；逐项验证稳定事件键。
9. **T-11D v0.2 Agent**：健康 Adapter、Run、受控产出、取消/重试、人工验收、返工和崩溃恢复。
10. **后续业务事件**：随 v0.3/v0.4 路线图、发票和回访模块交付后启用。

## 验收状态

### 当前纵切已覆盖

- [x] 断开外网时手工创建、列表、详情、编辑、已读、稍后和归档命令只依赖本地 Sidecar/SQLite。
- [x] schema v11→v12 升级保留既有事实；fresh v12 约束、nullable 部分唯一来源键和外键检查有迁移测试。
- [x] 创建与命令幂等重放不重复资源或事件；异体复用被拒绝。
- [x] PATCH/命令拒绝旧版本；事件失败时事实与批量已读整体回滚。
- [x] 全局未读计数不受页面筛选影响；时间截止式全部已读只处理创建与最后更新时间都不晚于 cutoff、且按 cutoff 仍属于待处理可见范围的未读，截止后更新项保守跳过。
- [x] 解决/忽略原因必填且不隐式已读；重开保留 read/triaged。
- [x] 前端覆盖分页、筛选、加载、空、错误、重试和冲突草稿保留的自动测试。
- [x] 已有 Task 活动/历史关系、实时进度、required 修改和带原因软解除均经过真实 API/SQLite，不复制 Task 状态。
- [x] 关系写入使用 Inbox `If-Match`/幂等快照，与状态/version/事件同事务；重新关联不覆盖旧历史。
- [x] 第一条/最后一条活动关系驱动 `open / tracking`，reopen 按活动关系选择状态；A2 不自动解决。
- [x] 活动关系阻止 Task 硬删除，软解除后可删且历史保留 Task ID/标题快照。

### 完整人工编排仍需验收

- [x] T-11C 批量拆分、Assignment 和审计在一个事务中完成，失败不遗留部分事实。
- [x] 进度完全从活动必需 Task 派生，零必需任务不自动解决。
- [x] 非 done Task 不误触发自动解决；自动完成后依赖失效会重开，手工/强制解决不会误重开。
- [x] Reminder 跨扫描、跨重启只生成一条 Inbox Item；其他来源事件仍待 T-11E 逐项验收。
- [x] 关系软解除、重新关联和关联 Task 删除后历史可解释。
- [ ] T-11E 多态来源删除保留快照并可解释；不要以关联 Task 删除互锁冒充来源协调。
- [x] Sidebar/Today 计数与 risk 深链已接真实统计；真实浏览器键盘/焦点、长列表和窄屏视觉仍需专项验收。
- [ ] v0.2 Agent 成功只进入 `waiting_review`，只有 owner 可接受，重试保留全部 Run 与 Artifact。

## 相关代码/PRD 链接

- [PRD：收件箱与本地工作编排中心](../opc-workspace-PRD.md#56-收件箱与本地工作编排中心)
- [整体功能架构](../functional-architecture.md)
- [schema v12 Inbox 迁移](../../services/sidecar/internal/database/migrations/012_inbox_items.sql)
- [schema v13 Inbox–Task 关系迁移](../../services/sidecar/internal/database/migrations/013_inbox_item_tasks.sql)
- [schema v14 Reminder 迁移](../../services/sidecar/internal/database/migrations/014_reminders.sql)
- [schema v15 Inbox 编排迁移](../../services/sidecar/internal/database/migrations/015_inbox_task_orchestration.sql)
- [Reminder 模块文档](reminders.md)
- [Inbox API](../../services/sidecar/internal/api/inbox_items.go)
- [Inbox–Task 关系 API](../../services/sidecar/internal/api/inbox_item_tasks.go)
- [Inbox 编排 API](../../services/sidecar/internal/api/inbox_orchestration.go)
- [Inbox API 测试](../../services/sidecar/internal/api/inbox_items_test.go)
- [Inbox–Task 关系 API 测试](../../services/sidecar/internal/api/inbox_item_tasks_test.go)
- [Inbox 迁移测试](../../services/sidecar/internal/database/inbox_migration_test.go)
- [Inbox–Task 关系迁移测试](../../services/sidecar/internal/database/inbox_task_migration_test.go)
- [Inbox 页面](../../apps/web/src/pages/InboxPage.tsx)
- [Inbox 详情](../../apps/web/src/components/InboxItemDetailModal.tsx)
- [Inbox–Task 关系 UI](../../apps/web/src/components/InboxItemTasksSection.tsx)
- [Inbox 时间线](../../apps/web/src/components/InboxItemEventsSection.tsx)
