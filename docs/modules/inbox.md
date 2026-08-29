# 收件箱与本地工作编排模块

> 实现状态截止：2026-08-29（依据当前代码与测试）
>
> 当前基线：app v0.1.0 / API v1 / SQLite schema v34。T-11A1/B 手工受理分诊、T-11A2 已有 Task 关系、T-11A3 一次性及 daily/weekly Reminder、T-11C 批量拆分/分派/自动结清，以及已登记来源投影均已交付。schema v33 的受限 Project 完成预设可追加一条本地“检查开票”Inbox Item；schema v34 的 Adapter 诊断不创建 Inbox；重复 Reminder 继续为每个 occurrence 生成独立 Inbox 来源，不改 Inbox 表或解决契约；v0.1 不启用 AI、LLM 或 Agent Runtime。

导航：[文档中心](../README.md) · [整体功能架构](../functional-architecture.md) · [PRD v9.27](../opc-workspace-PRD.md) · [任务](tasks.md) · [Actor 与分派](actors.md) · [本地提醒](reminders.md) · [预设自动化](automation.md)

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
- 已启用的“项目完成后提醒检查开票”预设只创建本地 Inbox Item，不生成或发送发票，也不改变 Project/Client/财务事实。
- 第一阶段不提供多人登录、云同步、远程通知、邮件/消息自动发送、线上 Agent 或模型服务。

## 当前实现状态

当前模块为**部分完成**：手工受理分诊、已有 Task 关系、一次性及 daily/weekly Reminder、批量拆分分派、自动结清/重开和例外强制解决、T-11F 运营计数及已登记来源投影均已接真实 SQLite/API/UI。拆分面板使用共享 ProjectSelect，并从可信来源快照默认带入 Project，仍允许逐项清除/改选；独立完成条件、person 本地责任提示和共享 Task 详情已接通。Project Artifact 读模型显示 nullable follow-up/实时 required 进度，成功 Inbox mutation 通过 Project/Task/Today Query 失效刷新相关表面。物理卷同卷去重和无路径手动容量检查已交付；卷级趋势、Agent 与 AI 尚未交付。

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
- 支持新建手工条目、查看详情、编辑标题/摘要/优先级/截止时间、单条已读和快照式全部已读；列表、创建、Reminder 和统一搜索入口均使用 `/inbox/:inboxItemId`，刷新可恢复精确详情。
- 支持设置稍后时间、提前恢复、到期自动回到待处理视图；稍后只改变可见性，不复制条目。列表每 15 秒低频刷新，以服务端时钟重新判定到期可见性。
- 支持带原因解决、带原因忽略和重新打开。解决/忽略不会隐式标为已读；未读终态仍可直接标为已读，无需先重开。重新打开保留 `read_at` 与 `triaged_at`，清除终态事实和 `snoozed_until`，并按是否存在活动 Task 关系进入 `tracking / open`。
- 详情提供追加式活动时间线，覆盖 `created / updated / read / snoozed / unsnoozed / resolved / dismissed / reopened`。
- 列表、详情和时间线均有加载、空、错误与重试状态；表单和命令有校验、禁用态及并发冲突提示，编辑草稿在冲突刷新后保留。

### 已交付：T-11A2 已有 Task 关系

- schema v13 通过 `013_inbox_item_tasks.sql` 新增活动/历史关系、required、稳定 position、软解除事实、原 Task ID/标题快照，以及活动关系阻止 Task 硬删除的数据库保护；v12→v13 是加法迁移，不改写既有业务事实或创建 demo 关系。
- 详情可查询全部活动关系和分页历史；服务端实时 JOIN 当前 Task，返回 Task 状态/版本以及 `active_total / required_total / required_done / required_remaining / required_blocked / required_waiting_review / required_cancelled / percent / all_required_done`。零个 required 时 `percent = null`、`all_required_done = false`，不会触发空集合完成。Task 状态与百分比不复制到 Inbox 表。
- 支持把已有 Task 关联到活动 Inbox Item、修改活动关系的 `is_required`，以及填写原因软解除。重新关联同一 Task 创建新关系行，不恢复或覆盖旧历史。
- 活动关系行可通过 stack-aware Modal 打开全局共享 Task 详情；历史关系只要实时 Task 仍存在也提供同一入口，关闭 Task 详情后返回原 Inbox 上下文；Task 已删除时仅显示不可变 ID/标题快照。
- `is_required` 必须由 Inbox 关系创建/修改或 T-11C 拆分草稿显式填写。Task 的父子层级、父任务自动待验收和系统 `child_rollup` Submission 都不会创建关系、继承 required 或改写现有标记。
- 第一条活动关系使 `open → tracking`；解除最后一条活动关系使 `tracking → open`；归档项重新打开时有活动关系进入 `tracking`，否则进入 `open`。关系命令不自动已读、不清除稍后、不改变 Task 生命周期，也不创建 Assignment。
- 关系 POST/PATCH/DELETE 使用 Inbox `ETag / If-Match` 和可选 `Idempotency-Key`，业务事实、Inbox 状态/version 与 `task_linked / task_requirement_changed / task_unlinked` 事件同事务提交。关系命令不递增 Task version。
- 任一活动 Inbox 关系会使 Task 硬删除返回 `409 TASK_HAS_ACTIVE_INBOX_RELATIONS`。带原因软解除后 Task 可以删除；历史行的实时 `task_id` 置空，但 `task_ref_id / task_title_snapshot` 与事件继续保留。

### 已交付：T-11A3 一次性与 daily/weekly 本地 Reminder

- schema v14 新增独立 `reminders` 调度事实；公开 API 提供创建、分页/搜索/状态查询、详情、scheduled 编辑和带原因取消。
- Sidecar ready 前先补扫到期项，运行中每 15 秒扫描最多 100 条；以 `reminder:<id>:due` 为稳定事件键在同一事务中生成或复用一个 Reminder Inbox Item、记录 system 事件并把 Reminder 标记为 fired。
- Inbox Item 使用 `kind/source_entity_type=reminder`、指向 Reminder ID、继承标题/摘要/优先级/触发时间，并保持 `resolution_policy=manual`。用户随后在普通 Inbox 流程中阅读、稍后、解决、忽略或关联 Task，不反向修改 fired Reminder。
- schema v32 为每条 Reminder 增加系列、daily/weekly、IANA 时区和 occurrence 序号；到期事务在 fired/Inbox 事实之外创建唯一下一 occurrence。跨 DST 保持当地钟点，离线跨过多个周期只补当前一条，重复扫描/重启不重复；取消当前 scheduled occurrence 停止系列。
- Reminder 管理器从 Inbox 页打开，提供待提醒/已触发/已取消模块、搜索、分页、新建、编辑/改期、重复规则、带原因取消和触发后跳转 Inbox。每月/自定义规则、系统原生通知和其他业务来源投影仍未交付。

### 已交付：T-11C 拆分、分派与自动结清

- schema v15 的 `015_inbox_task_orchestration.sql` 为自动完成规则增加查询索引和数据库保护：自动解决必须至少有一个活动必需 Task，且全部处于 `done`；不改写既有业务数据或创建 demo 记录。
- `POST /api/v1/inbox-items/:id/split` 在一个 SQLite 事务内创建 1–20 个 Task、父子关系、标签、`created` Inbox 关系、初始 owner/person Assignment、manual review 的 owner reviewer，以及 Task/Inbox 审计事件。任一字段、引用或写入失败时全部回滚。
- 拆分面板支持任务名称、说明、类型、优先级、项目、完成条件、父任务、必需标记、负责人和验收策略；父任务只能引用本批次中更早的任务，避免环和悬空引用。
- 对 `task_artifact / task / task_due / project_completion` 四类可信本地来源，只有 payload 中 canonical `project_id` 会成为默认 Project；首项和后续新增草稿继承它，但每项可显式清除或改选。来源快照是便利默认值，不是强制关联。
- 完成条件由独立输入写入 Task `completion_criteria`，不再用说明字段冒充。负责人只列 active owner/person；person 明确标为“仅本地责任记录”，不会登录、接收同步或直接操作应用。
- 每个拆分 Task 的项目字段复用共享 `ProjectSelect`：打开单个选择器时每页读取 20 条，输入经 250 ms 防抖后走 Project 服务端搜索，`q / page / includeArchived` 隔离 Query key，旧请求可取消，候选按 ID 去重。默认不列归档项目；当前选中项通过详情或名称 fallback 跨页/失败保留，只有显式清除才把该草稿的 `project_id` 设为 null。加载、空、错误重试、更多结果与 combobox 键盘语义由同一组件提供。
- `all_required_tasks_done` 策略由统一 reconciliation 维护：至少一个活动必需 Task 且全部 `done` 时由 system Actor 自动解决；自动解决后，任一必需 Task 因重开、返工等离开 `done` 会自动恢复为 `tracking`。
- Task 生命周期命令、产出提交/验收，以及 Inbox 关系的新增、required 修改、解除和拆分都会调用同一 reconciliation；进度仍实时来自 Task，不复制第二份状态。
- schema v30 父任务协调仍会在父 Task 状态变化后调用这条既有 reconciliation，但只影响已显式关联且 active/required 的 Task：父子 Task 关系本身不等于 Inbox 关系，父任务的直属子任务也不会被 Inbox 自动纳入 required 集合。
- 普通 `resolve` 不能绕过自动策略的未完成必需任务。危险操作 `force-resolve` 只用于自动策略，要求显式 `confirm=true` 和原因，并以 owner Actor、`forced` mode 与 `force_resolved` 事件留下不可变审计；手工/强制解决不会因后续 Task 变化自动重开。
- split 写命令失败时保留草稿，并使用 Inbox `If-Match` 与稳定幂等键；split API 成功响应后前端立即关闭 Modal，即使随后的后台刷新失败也不保留可重放草稿。所有会改变 follow-up 的成功 Inbox 编辑、命令、关系/required mutation、split 与强制解决会先取消可信来源 Project 的在途查询再失效缓存；Artifact 请求消费 `AbortSignal`。split 额外统一失效 Task、Today 和 Project 查询。缓存刷新不传播或复制跨聚合 version。

### 已交付：T-11F 运营计数与风险深链

- `GET /api/v1/stats/inbox` 在查询时从当前可见的 `open / tracking` 条目与活动必需 Task 实时派生 `pending / unread / tracking / blocked / waiting_review`；返回同一服务端 `server_now`。
- `snoozed_until > server_now` 的未来稍后项、resolved/dismissed 归档项、已解除关系、可选 Task 和已删除 Task 不进入相应当前风险计数；同一 Inbox 有多个同状态 Task 只计一次。
- 列表新增 `risk=tracking|blocked|waiting_review` 服务端筛选，blocked/waiting_review 使用活动必需关系和实时 Task 状态；全局 `unread_total` 继续不受 risk/search/priority 缩小。
- Sidebar 显示待处理徽标，Today 显示待处理/跟进中/待验收/有阻塞并跳转风险深链；统计 Query 位于 Inbox Query 前缀下，写入后统一失效并每 15 秒刷新。

### 已交付：T-11E 第一项——Task Artifact follow-up 来源

- schema v23 通过 `023_task_artifact_inbox_projection.sql` 为 `source_entity_type=task_artifact` 增加查询索引、严格来源身份、不可变快照和删除协调 guards，不回填旧 Artifact、不创建 demo Inbox Item。
- `submit-output` 事务只消费本批次中显式 `requires_followup=true` 的 Artifact；每个 Artifact 以 `task-artifact:<artifact-id>:followup` 创建一个 `kind=event` Inbox Item，并由 system Actor 追加 `source_projected` 事件。未标记 Artifact 不创建，整个提交/投影/事件/幂等快照任一步失败时全部回滚。
- 来源 `payload_json` 只保存解释和导航所需的不可变快照：Artifact ID/名称/类型、Task ID/标题、Submission ID/序号及可选 Project ID/名称，不复制正文、文件、Task 状态或负责人。
- 列表把 event 显示为“任务产出跟进”；详情展示来源任务、批次、产出类型和项目快照，可直达共享 Task 详情。正文、下载、审核、返工和删除仍由 Task 领域负责。
- 活动 `open/tracking` 来源项会分别以 `ARTIFACT_HAS_ACTIVE_INBOX_SOURCE` 或 `TASK_HAS_ACTIVE_INBOX_SOURCES` 阻止 Artifact/Task 删除。用户先解决或忽略来源项后，删除事务会递增 Inbox 版本、写 `source_deleted_at` 和 `source_deleted` 事件，再删除来源；失败整体回滚。
- 来源删除后 Inbox Item 和 payload 快照继续保留，并明确显示“来源产出已删除”；重新打开仍可基于快照继续人工编排，但不会恢复来源 Artifact。
- `GET /api/v1/projects/:id/artifacts` 为该来源返回 nullable `followup`：Inbox ID/version/status/policy/`source_deleted_at` 与实时 required progress。响应 `ETag / meta.project_version` 仍只表示 Project 聚合版本；Inbox 写版本由 `followup.inbox_item_version` 独立表达，当前 Project 页面只深链 Inbox。Inbox 成功 mutation 通过来源 Project Query 失效刷新，不增加跨聚合版本 trigger。

### 已交付：T-11E 第二项——Task 阻塞来源

- schema v24 通过 `024_task_blocked_inbox_projection.sql` 约束 `source_entity_type=task` 的阻塞来源身份、按来源 Task/version 查询的索引、不可变快照和 Task 删除协调；升级不回填此前已经处于 blocked 的 Task，也不创建 demo Inbox Item。
- 每次受控 `block` 命令更新 Task、追加 `task_blocked` Event，并在同一事务以 `task:<task-id>:blocked:<block-version>` 创建一个 `kind=event` Inbox Item 和 system `source_projected` Event；命令幂等重放不重复创建，unblock 后再次 block 使用新版本生成新的独立阻塞事项。
- payload 只快照 Task ID/标题、阻塞原因/时间/来源状态、本次 block version 及可选 Project ID/名称；不复制当前 Task 状态、Assignment 或正文。unblock 不自动解决或删除 Inbox Item，保留 owner 对本次阻塞事件的人工受理权。
- 列表以警示图标区分 Task 阻塞；详情展示阻塞时间、原因、阻塞前状态、项目和精确 Task 入口。来源仍存在时可直达；Task 删除后隐藏失效链接并显示保留快照。
- 任一 `open/tracking` Task 来源项使 Task DELETE 返回 `TASK_HAS_ACTIVE_INBOX_SOURCES`。全部来源项解决/忽略后，删除事务统一标记 `source_deleted_at`、递增 Inbox version、追加 `source_deleted` Event，再删除 Task；多个阻塞批次及 Artifact 来源任一步冲突都会整体回滚。

### 已交付：T-11E 第三项——Task 临期来源

- schema v25 通过 `025_task_due_inbox_projection.sql` 新增 `source_entity_type=task_due` 的来源身份、按 Task/截止时间查询的索引、不可变快照和 Task 删除协调；升级不回填旧 Task、不创建 Inbox Item 或 demo 数据。
- Sidecar ready 前补扫，运行中复用 15 秒本地扫描周期。状态非 done/cancelled 且 `due_date` 已进入未来 24 小时窗口的 Task 按截止时间/ID 稳定排序，每批最多处理 100 条；已投影来源从后续批次排除，因此积压会继续推进而不会卡在首批。
- 每个 Task/截止时点使用 `task:<task-id>:due:<due-at>` 稳定键创建一个 `kind=event` Inbox Item 和 system `source_projected` Event。重复扫描/重启不重复；改期到新的截止时点会生成新的独立事项，改回相同语义时点仍复用原来源。
- payload 只快照 Task ID/标题、截止时间、投影时间、投影时的 `due_soon/overdue` 分类、固定 1440 分钟提前量和可选 Project ID/名称；不复制当前状态或责任。列表使用临期图标，详情展示截止/进入收件箱时间、项目和 Task 入口。
- 完成、取消或改期不替 owner 自动解决已经生成的来源项。活动来源阻止 Task 删除；全部来源归档后，Task 删除事务写 `source_deleted_at`、递增 Inbox version、追加 `source_deleted` Event 并保留快照。

### 已交付：T-11E 第四项——备份创建失败的系统维护来源

- schema v26 通过 `026_system_maintenance_inbox_projection.sql` 约束 `source_entity_type=system_maintenance` 的来源身份：`kind=event`、`source_entity_id=component:operation`、`source_event_key` 匹配 `system:<source-id>:<incident-id>`、投影初始优先级 P0/P1、无截止时间、payload 只含 `component / operation / failure_code / occurred_at / message`，并禁止写入 `source_deleted_at`。owner 仍可把条目重新分级为 P0–P3；升级保留既有 Inbox 事实，不回填或创建 demo incident。
- `POST /api/v1/backups` 失败仍向调用端返回 `BACKUP_CREATE_FAILED`；现有数据保持不变。Sidecar 随后尽力投影一条 Inbox Item。投影失败只记内部维护日志，不改变备份错误响应，也不把底层 Go error 暴露给收件箱。
- 当前只支持 `component=backup`、`operation=create`、`failure_code=backup_create_failed`。标题固定为“本地备份需要处理”，message 固定说明无法创建已验证备份且现有数据未被修改。payload 不保存 Go error、本机路径、备份 note、Token、request ID 或请求正文。
- 同一 `backup:create` 在 `open/tracking` 且未标记来源删除时只允许一个活动 incident；重复失败复用既有条目。resolve/dismiss 后再次失败使用新的 incident ID 和 `source_event_key` 开新条目。system Actor 追加 `source_projected` Event。
- 列表以硬盘图标区分系统维护项；详情标注“系统维护”，展示组件/操作/发生时间和固定说明，并提供“打开数据与备份”。前端 strict normalizer 拒绝额外 payload 字段、错误原文和未知 event key。
- 系统维护来源没有可删除的业务实体，因此不实现 `source_deleted_at` 协调。PATCH 不能给这类条目设置截止时间。

### 已交付：T-11E 第五项——备份校验失败的系统维护来源

- 复用 schema v26 的 `component:operation` 身份，不新增迁移。`POST /api/v1/backups/:id/verify` 在返回 `BACKUP_VERIFY_FAILED` 后尽力投影 `source_entity_id=backup:verify`。
- 标题固定为“本地备份校验需要处理”；payload 只含 `component=backup`、`operation=verify`、`failure_code=backup_verify_failed`、`occurred_at` 和固定说明。不保存 Go error、本机路径、备份 ID、note、Token 或请求正文。
- 同一 `backup:verify` 活动 incident 去重；resolve/dismiss 后再失败开新条目。`BACKUP_INVALID`（包损坏/篡改/额外文件）表示校验已完成，不投影 Inbox。

### 已交付：T-11E 第六项——备份恢复演练与恢复安排失败

- `POST /api/v1/backups/:id/drill` 的操作性启动失败或通过 manifest 校验后的隔离演练失败，尽力投影 `source_entity_id=backup:drill`；包不存在、请求 ID 非法或 `BACKUP_INVALID` 不投影。
- `POST /api/v1/backups/:id/restore` 在读取 pending 状态、检查源目录、读取工作区身份、创建恢复前回滚点或发布 pending 计划的操作性失败时，尽力投影 `source_entity_id=backup:restore`。确认缺失、包不存在/无效、工作区不匹配和已有恢复计划属于可解释业务结果，不投影。
- 恢复安排前的隔离演练失败复用 `backup:drill` 身份，不重复发明 restore incident。两类 payload 继续只保留 `component / operation / failure_code / occurred_at / message`，不含备份 ID、本机路径、底层 error、Token、请求正文或备注。
- 每个 source id 同时最多一个 `open/tracking` incident；原 API 错误码与 HTTP 状态保持不变，Inbox 投影失败只写内部日志。
- 列表/详情与创建失败共用系统维护图标和“打开数据与备份”；来源上下文分别显示恢复演练与恢复安排。

### 已交付：T-11E 第七项——数据库与 Sidecar 启动失败补偿

- 数据库尚未可写时不能直接创建 Inbox Item。Sidecar 因数据库打开、受保护迁移、迁移前回滚点、启动恢复、Router/Artifact 初始化、监听或 ready 输出失败退出前，只向 `OPC_LOG_DIR/startup-incidents-v1.json` 写入白名单 kind、稳定 UUID 和 UTC 时间；不写底层 error、本机路径、令牌、请求正文或业务数据。
- 白名单分别映射为 `database:startup`、`database:migration` 和 `sidecar:startup`。同一种 kind 在 journal 未消费前只保留最早一条，文件最多 16 条、64 KiB，使用同目录临时文件和原子替换；非普通文件、未知字段、非法 UUID/时间/类型、重复记录或超限文件会隔离为 `.startup-incidents-invalid-<uuid>.json`，不会作为事件读取。
- 下一次成功打开并迁移数据库后、Router ready 前补偿投影。payload 仍只有 `component / operation / failure_code / occurred_at / message`；`occurred_at` 使用原始失败时间。全部投影成功后删除 journal；投影或删除失败会保留/重现日志供后续重试。
- journal 中的稳定 incident ID 同时进入 `source_event_key`。即使数据库提交成功而 journal 删除结果不确定，重放也先按 event key 查询，用户已经 resolve/dismiss 的同一失败不会被重新创建。新的启动失败在旧 journal 已消费后获得新 ID，遵循活动 incident 去重规则。
- `OPC_LOG_DIR`/`--logs` 是独立受控诊断目录，默认使用数据库同级 `logs/`，不得与 Artifact/backup root 重叠。Sidecar 与 Tauri 壳分别写入 5 MiB/3 归档脱敏日志，设置和全局启动故障恢复页均可无路径打开目录；诊断包 v1 只导出系统维护错误码级汇总，明确不含原始日志。数据库打开前的恢复/迁移白名单进度已交付，启动前备份选择仍待开发。

### 已交付：T-11E 第八项——Project 完成节点

- schema v28 新增 `source_entity_type=project_completion`，不回填已经完成的历史 Project，也不创建 demo Inbox Item。只有用户通过 Project `complete` 命令产生的新完成事实会在原状态转换事务内投影；Project 命令、`project_completed` Workflow Event、Inbox Item 和 `source_projected` Event 共同成功或回滚。
- 稳定键为 `project:<project_id>:completed:<completion_version>`。同一完成周期不能重复；Project reopen 后再次 complete 会以新的完成后 Project version 形成独立周期和新事项。
- 不可变 payload 精确保存 `project_id / project_name / completed_at / completion_version / incomplete_task_count`。事项固定 P1、无截止时间、人工解决；未结任务数使用 complete 命令确认时的同事务快照，不随后续 Task 变化改写。
- Inbox 详情展示“项目完成”、完成时间、项目名和完成时未结任务数，并直达 `/projects/:id`。这只要求用户确认交付收尾、归档或其他人工后续，不自动创建 Task，不伪造验收、开票、收入或客户消息。
- 活动完成事项会阻止 Project 永久删除。全部来源事项先 resolve/dismiss 后，Project 删除事务写入 `source_deleted_at` 和 `source_deleted` Event，再删除 Project；历史 Inbox Item 保留快照并停止提供失效链接。

### 已交付：预设自动化的 Project 完成本地提醒

- schema v33 的 `project-completed-inbox` 预设默认禁用；用户在设置中预览并显式启用后，新发生的 Project complete 事件会追加一条标题为“检查开票”的本地 Inbox Item。
- 动作使用稳定 Rule/事件 dedupe key；同一完成版本重放不会重复创建，失败只留下 Automation Run 并按受控次数重试。自动化基础设施失败由外层 savepoint 隔离，不能回滚已成功提交的 Project 完成事实。
- 该条目只保存 Project ID/名称/完成版本等最小本地上下文，不创建发票、不确认收入、不联系客户，也不调用外部 API、AI、LLM 或 Agent。

### 已交付：T-11E 第九项——运行期数据库故障降级

- 版本化 API 通过统一数据库错误出口捕获非预期 SQLite 操作失败；`/health` 数据库 Ping、Focus Session 心跳和 Reminder/Task 到期来源扫描失败也进入同一链路。原 HTTP 状态、API 错误码及后台任务行为保持不变。
- 数据库仍可写时，Sidecar 尽力创建 `source_entity_id=database:runtime`、`failure_code=database_runtime_failed` 的 P1 系统维护事项；同一 source id 只保留一个活动 incident，用户解决/忽略后发生的新失败可创建新事项。
- 数据库不可写导致直接投影失败时，只把 `database_runtime`、稳定 UUID 和 UTC 时间写入并发安全的 `startup-incidents-v1.json`；下一次健康启动按既有严格校验和稳定 event key 补偿。Inbox/journal 均不含 SQL error、路径、Token、请求正文或业务数据。
- 该链路仍是故障后投影；主动容量监测由下一项独立负责。

### 已交付：T-11E 第十项——主动低磁盘空间监测

- Sidecar 在 ready 前及每 5 分钟检查数据库父目录、受控文件根和备份根；规范化绝对路径并去除重复路径，每轮读取 `app_settings.storage`，任一根可用空间低于默认 1 GiB、可配置 1–100 GiB 的阈值即形成 `storage:low_space`、`failure_code=storage_low_space` 的 P1 系统维护事项。
- 同一进程内持续低空间只触发一次；即使用户先解决事项也不会每 5 分钟重开。全部受控根恢复到阈值以上后解除周期锁存，之后再次跌破可形成新的独立 incident。重启后仍低空间会按新的运行周期检查，但活动事项仍由数据库唯一约束去重。
- 数据库不可写时复用并发安全 journal，以 `storage_low_space` 白名单 kind 在下次健康启动补偿。payload 和 journal 不保存盘符、根路径、精确容量或底层探测错误；探测 API 失败只写固定内部日志，不把“无法检测”伪装成“空间不足”。
- 前端严格接受该固定来源，显示“本地存储 / 容量检查”并提供“打开数据与备份”。阈值可在设置中预览并保存，三个固定逻辑位置可无路径手动检查；物理卷身份只用于进程内同卷去重，API 仅返回共享布尔值，卷级趋势尚未开放。

### 明确未交付

- 每月/自定义 Reminder、系统原生通知，以及 Task/Project/Client 等自由业务来源自动创建 Reminder；当前只有两个固定日历自动化预设；
- Project 完成以外的独立里程碑、Client/Invoice，以及其他尚未接入的系统故障来源投影；“检查开票”仅为本地人工提示，不是 Invoice 领域实现；
- 卷级容量历史趋势；
- 已交付来源以外的多态删除协调、Inbox Item 硬删除；
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

1. 用户从 Project Artifact 深链或直接在活动 Inbox Item 详情选择“拆分并分派”。可信来源中的 canonical Project 默认带入首项及后续草稿，但每项可清除或改选；用户填写说明与独立完成条件、父任务、owner/person 负责人、验收策略和 required。person 只作本地责任记录。
2. 前端提交 Inbox 当前版本和稳定幂等键。Sidecar 先完整校验，再在一个事务内创建 Task、标签、层级、Assignment、reviewer、`created` 关系与审计；失败时不保留部分数据。
3. 提交可保留 `manual`，也可切换 `all_required_tasks_done`。自动策略必须至少有一个必需 Task；none Task 可 direct complete，manual Task 提交后进入 waiting_review 并由 owner accept/request changes，所有活动必需 Task done 后 system 自动解决条目。
4. 必需 Task 处于 `todo / in_progress / blocked / waiting_review / cancelled` 时均不自动解决。自动解决后若必需 Task 通过 reopen、返工或其他受控命令离开 `done`，条目自动回到 `tracking`。
5. 若业务确实无需等待，用户展开“例外：强制解决”，填写原因并二次确认。该命令只作用于自动策略，保留未完成 Task，并记录 `forced` mode 与不可变事件。
6. 成功 Inbox mutation 失效可信来源 Project；split 另失效 Task、Today、Project。Project Artifact 再读时取得当前 follow-up/progress，但其 Project 数值 `ETag` 不承担 Inbox 并发语义。

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

### `inbox_items`（schema v12，在当前 schema v33 延续）

| 字段                                | 当前约束 / 说明                                                                                                                                          |
| ----------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `id`                                | UUID 主键                                                                                                                                                |
| `kind`                              | 表约束 manual/event/reminder；公开创建 API 仅 manual，内部 Reminder 使用 reminder，其余已交付业务来源使用 event                                          |
| `title / summary`                   | 标题 2–200；摘要最多 10,000                                                                                                                              |
| `source_entity_type`                | 公开创建 API 固定 manual；内部已使用 reminder/task_artifact/task/task_due/project_completion/system_maintenance                                          |
| `source_entity_id`                  | 当前手工项必须为 null；系统维护项为 `backup:create/verify/drill/restore`、`database:startup/migration/runtime`、`sidecar:startup` 或 `storage:low_space` |
| `source_event_key`                  | nullable；非空值受部分唯一索引保护；Artifact、Task 阻塞/临期、Project 完成和系统维护分别使用稳定键                                                       |
| `source_deleted_at`                 | 手工/Reminder/系统维护为 null；Artifact/Task/Task due/Project 完成来源归档后删除时原子写入，且不可再次修改                                               |
| `priority`                          | P0 / P1 / P2 / P3                                                                                                                                        |
| `status`                            | open / tracking / resolved / dismissed                                                                                                                   |
| `resolution_policy`                 | manual/all_required_tasks_done；公开新建仍为 manual，T-11C 拆分可切换自动策略                                                                            |
| `due_at`                            | 可空 RFC 3339 UTC                                                                                                                                        |
| `read_at / triaged_at`              | 相互独立的已读与分诊时间                                                                                                                                 |
| `snoozed_until`                     | 可空；未来值进入稍后视图，到期后按查询恢复                                                                                                               |
| `resolved_* / resolution_*`         | resolved 终态事实；mode 为 manual/automatic/forced，自动模式使用 system Actor                                                                            |
| `dismissed_* / dismiss_reason`      | dismissed 终态的 owner、时间和原因                                                                                                                       |
| `payload_json`                      | 必须是 JSON object；当前 UI 不编辑                                                                                                                       |
| `version / created_at / updated_at` | 乐观并发版本与 UTC 时间                                                                                                                                  |

schema v13 不重建 `inbox_items`；schema v14 由 Reminder 调度器使用既有来源字段；schema v15 增加自动结清保护；schema v23–v25 冻结 Task Artifact、Task 阻塞与 Task 临期来源；schema v26 增加系统维护来源；schema v28 增加 Project 完成周期来源、不可变快照和删除协调。均不重建表、不回填旧事实。其他 event 来源仍是受约束的未来空间。

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

| 方法   | 路径                                     | 契约摘要                                                                                                                                     |
| ------ | ---------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| GET    | `/api/v1/inbox-items`                    | 三视图、搜索、优先级、risk、分页、稳定排序；返回全局待处理未读数与快照时间                                                                   |
| GET    | `/api/v1/stats/inbox`                    | 实时派生 pending/unread/tracking/blocked/waiting_review 与 server_now                                                                        |
| POST   | `/api/v1/inbox-items`                    | 新建 manual 条目；可选 `Idempotency-Key`；返回 `201`、数据和 `ETag`                                                                          |
| POST   | `/api/v1/inbox-items/read-all`           | 以 `through_created_at` 批量已读；可选幂等键；不受当前筛选缩小                                                                               |
| GET    | `/api/v1/inbox-items/:id`                | 详情、可用动作和 `ETag`                                                                                                                      |
| PATCH  | `/api/v1/inbox-items/:id`                | 编辑标题/摘要/优先级/截止时间；强制 `If-Match`；终态拒绝；`system_maintenance` 不能设置截止时间                                              |
| GET    | `/api/v1/inbox-items/:id/events`         | 分页时间线，默认 50/最大 100；返回 `ETag` 与 `meta.inbox_item_version`                                                                       |
| POST   | `/api/v1/inbox-items/:id/read`           | 单条已读；强制 `If-Match`，可选幂等键                                                                                                        |
| POST   | `/api/v1/inbox-items/:id/snooze`         | 设置未来 `snoozed_until`；强制 `If-Match`，可选幂等键                                                                                        |
| POST   | `/api/v1/inbox-items/:id/unsnooze`       | 清除稍后时间；强制 `If-Match`，可选幂等键                                                                                                    |
| POST   | `/api/v1/inbox-items/:id/resolve`        | 必填原因，manual 解决；强制 `If-Match`，可选幂等键；不隐式已读                                                                               |
| POST   | `/api/v1/inbox-items/:id/dismiss`        | 必填原因，忽略归档；强制 `If-Match`，可选幂等键；不隐式已读                                                                                  |
| POST   | `/api/v1/inbox-items/:id/reopen`         | 重新打开并保留 read/triaged；强制 `If-Match`，可选幂等键                                                                                     |
| GET    | `/api/v1/inbox-items/:id/tasks`          | 返回 `data.active/history`；history 分页，meta 含 Inbox version 与实时 progress，响应携带 Inbox `ETag`                                       |
| POST   | `/api/v1/inbox-items/:id/tasks/:task_id` | body `{is_required}`；关联已有 Task，强制 Inbox `If-Match`，可选幂等键，第一条关系进入 tracking                                              |
| PATCH  | `/api/v1/inbox-items/:id/tasks/:task_id` | body `{is_required}`；修改活动关系 required，强制 Inbox `If-Match`，可选幂等键                                                               |
| DELETE | `/api/v1/inbox-items/:id/tasks/:task_id` | body `{reason}`；带原因软解除，强制 Inbox `If-Match`，可选幂等键，最后关系回到 open                                                          |
| POST   | `/api/v1/inbox-items/:id/split`          | 原子创建 1–20 个 Task、完成条件、层级、owner/person Assignment、manual owner reviewer、created 关系与审计；强制 Inbox `If-Match`，可选幂等键 |
| POST   | `/api/v1/inbox-items/:id/force-resolve`  | body `{confirm:true,reason}`；仅自动策略的例外解决；强制 Inbox `If-Match`，可选幂等键                                                        |

关系 GET 返回 `{data:{active,history},meta:{page,page_size,total,inbox_item_version,progress}}`；`page/page_size` 只作用于 history。单条关系命令返回 `{inbox_item,relation,progress}`；split 返回 `{inbox_item,tasks,relations,assignments,progress}`。Project Artifact 聚合另以 nullable `followup` 只读暴露 Inbox ID/version/status/policy/source deletion/progress；其响应仍使用 Project 数值 `ETag / meta.project_version`，不能替代 Inbox `If-Match`。Reminder 使用独立路由和内部到期投影；Task Artifact follow-up 由 `submit-output` 投影，Project 完成由 Project transition 投影。所有来源都没有公开创建路由，当前也没有 Inbox 删除路由。

### 幂等、并发与事务

- 创建、单条命令和全部已读支持 `Idempotency-Key`；同 key/endpoint/规范请求重放首次状态码与数据快照，同 key 不同请求返回 `409 IDEMPOTENCY_CONFLICT`。
- Task 关系 POST/PATCH/DELETE 同样保存规范化请求摘要和首次响应；摘要包含 Inbox expected version、Task ID 与 required/reason。重放发生在当前关系/Inbox 版本检查前，不重复关系或事件。
- 单条命令的请求摘要包含 expected version，并在读取当前数据库版本之前检查幂等快照，因此一次已经成功但响应丢失的命令可用原 key/原版本安全重放。
- PATCH 和所有单条命令使用资源 `ETag`/`If-Match`；缺失前置条件和旧版本分别被拒绝，不自动覆盖其他窗口的新事实。
- 关系写入以 Inbox 为聚合边界：成功只递增 Inbox version，不递增 Task version；Task 在关系提交前必须仍存在，Task 删除在同一 SQLite 写边界内检查活动关系。
- 关系 GET 实时 JOIN Task；没有 Task.version→Inbox.version 传播 trigger。Task 生命周期、产出验收和关系写入在各自事务内调用统一 reconciliation，前端同时失效相关查询。
- Project Artifact `followup` 同样是实时读投影：Inbox/Task 变化不递增 Project version。成功 mutation 根据严格来源类型和 payload `project_id`，按 cancel→invalidate 顺序刷新来源 Project；Artifact 请求消费 `AbortSignal`。split 额外失效 Task/Today/Project 前缀。`followup.inbox_item_version` 是 Inbox 并发版本，Project UI 当前只深链。
- 业务事实、Workflow Event 与幂等快照在同一个 SQLite 事务中提交；事件失败不遗留半完成状态。

### Workflow Event

- Inbox 事件使用 `aggregate_type = inbox_item`、条目 ID 作为 aggregate ID，并记录内置 owner Actor、request ID、前后 JSON 快照、命令序号和 UTC 时间。
- 当前 action 另包含 `tasks_split / automatically_resolved / automatically_reopened / force_resolved / source_projected / source_deleted`；拆分产生的 Task/Assignment 也写各自聚合事件。
- 关系事件的前后快照包含关系、读取时 Task 摘要、实时进度、Inbox 状态/version 与可选解除原因；这些是不可变审计快照，不是可写的 Task 或 Inbox 第二事实源。
- 事件沿用 schema v8/v9 的不可修改、不可删除保护；事件列表只读，不作为当前 Inbox Item 状态的第二副本。

## 与其他模块协作

| 模块     | 当前协作事实                                                                                                                                                                                                        | 后续扩展                                         |
| -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------ |
| 任务     | 可关联/拆分 Task；关系行打开共享详情；完成条件、owner/person、manual owner reviewer 与 reconciliation 已接通；follow-up/阻塞/临期来源已投影                                                                         | 更多筛选与跨模块统计继续扩展                     |
| 项目     | Artifact 聚合显示 nullable follow-up/实时 required 进度并深链；split 继承/清除来源 Project，成功 mutation 失效来源 Project；Project 与 Inbox version 保持独立                                                       | 其他里程碑只随真实状态/事实扩展                  |
| 客户     | 当前没有客户活动或回访来源                                                                                                                                                                                          | v0.4 回访到期生成去重 Inbox Item                 |
| 发票     | 当前没有财务来源                                                                                                                                                                                                    | v0.4 临期/逾期及开票节点生成本地待办             |
| Actor    | owner 执行拆分/强制解决；owner/person 可成为初始负责人，person 明确仅作本地责任记录；manual reviewer 固定 owner；system 执行自动结清/重开                                                                           | Agent Actor 仍延后                               |
| 今日     | 已展示待处理/跟进/阻塞/待验收实时计数并支持风险筛选深链                                                                                                                                                             | 随 T-11E 来源投影自然纳入更多业务事件            |
| 系统维护 | 备份四类操作失败、数据库启动/迁移、Sidecar 启动、运行期数据库操作失败和可配置低空间已安全投影；相关事项可打开数据与备份，物理卷同卷去重、无路径手动容量检查、数据库打开前白名单恢复进度和全局启动故障恢复页也已交付 | 卷级趋势和启动前备份选择仍待                     |
| Agent    | 未实现                                                                                                                                                                                                              | v0.2 只通过受控 Adapter/Run 产生待验收或失败事件 |

完整协作图参见[整体功能架构](../functional-architecture.md)。

## 后续实施顺序

1. **前置 Actor/Task 工作流基础（已完成）**：Actor、Assignment、六状态命令、Task Event、manual Submission/Artifact。
2. **T-11A1 手工 Inbox Item 数据契约（已完成）**：schema v12、约束、索引和迁移保留测试。
3. **T-11B 人工受理与分诊（已完成）**：真实列表/详情/编辑、已读/快照式全部已读、稍后/恢复、解决/忽略/重开及事件 UI/API。
4. **T-11A2 Task 关系事实（已完成）**：schema v13、活动/历史关系、实时进度、已有 Task 关联、required 修改、带原因软解除、状态联动、事件，以及关联 Task 硬删除互锁和历史快照。多态来源删除协调不属于 A2。
5. **T-11A3 Reminder 事实（已完成）**：schema v14 一次性事实 + schema v32 daily/weekly 系列、创建/查询/编辑/取消、启动补偿、15 秒扫描、IANA/DST 推进、稳定事件键与幂等 Inbox/下一 occurrence 投影。
6. **T-11C 拆分与分派（已完成人工闭环）**：原子多任务/父子拆分、可信来源 Project 继承/清除/改选、独立完成条件、owner/person Assignment 与本地责任提示、manual owner reviewer、共享 Task 详情、统一 reconciliation、自动解决/重开和 force-resolve。
7. **T-11F 运营计数（已完成）**：实时统计 API、risk 列表筛选、Sidebar 徽标和 Today 风险卡。
8. **T-11E v0.1 来源投影（部分完成）**：显式 follow-up Task Artifact、Task 阻塞、提前 24 小时 Task 临期、Project 完成周期，以及备份四类操作失败、数据库启动/迁移、Sidecar 启动、运行期数据库操作失败和可配置低空间监测已完成；物理卷同卷去重和无路径手动容量检查已在设置页交付，后续按真实业务模块继续来源投影并独立评审卷级趋势。
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
- [x] T-11C 项目字段复用共享 ProjectSelect；每次只读 20 条候选，250 ms 搜索、请求取消、ID 去重、选中保留、显式清除和反馈/键盘交互均有自动化接线证据，生产路径不再串行拉全 Project。
- [x] 可信来源 canonical Project 默认带入首项/新增草稿且可逐项清除或改选；完成条件写入独立 Task 字段，person 显示仅本地责任记录。
- [x] 活动关系及仍有实时 Task 的历史关系可打开共享 Task 详情；已删除 Task 只保留快照。
- [x] 进度完全从活动必需 Task 派生，零必需任务不自动解决。
- [x] 非 done Task 不误触发自动解决；自动完成后依赖失效会重开，手工/强制解决不会误重开。
- [x] schema v30 父任务自动验收不创建 Inbox 关系、不继承或改写 `is_required`；只有显式 active required 关系参与 Inbox 自动解决/重开。
- [x] Reminder 与 Task 临期跨扫描/重启、follow-up Artifact 跨提交重放、同一次 Task block 幂等重放，以及同一备份/数据库/Sidecar 系统维护 source id 的活动 incident 均只生成一条 Inbox Item；启动/运行期 journal 模糊清理重放不重复。Task 改期、重复阻塞和归档后再失败按各自稳定事实生成独立来源。`BACKUP_INVALID` 不创建系统维护 incident。
- [x] 关系软解除、重新关联和关联 Task 删除后历史可解释。
- [x] Task Artifact/Task 阻塞/Task 临期多态来源删除会先限制活动项，归档后原子标记来源删除并保留快照；系统维护来源禁止 `source_deleted_at`。其他未来来源仍需逐项实现。
- [x] Sidebar/Today 计数与 risk 深链已接真实统计；ProjectSelect 的真实浏览器键盘/焦点、窄屏和 1,000/10,000 条项目性能，以及 Inbox 长列表和窄屏视觉仍需专项验收。
- [x] Project Artifact 返回 nullable follow-up 的 Inbox ID/version/status/policy/source deletion/实时 progress；响应保持 Project 数值 ETag，成功 Inbox mutation 失效来源 Project，split 另失效 Task/Today/Project。
- [x] Go 金链覆盖 `requires_followup → split(owner/person + manual owner reviewer) → complete + submit(waiting_review) → accept → automatic resolved/100%`；前端另覆盖 person 本地责任提示与提交载荷。
- [ ] v0.2 Agent 成功只进入 `waiting_review`，只有 owner 可接受，重试保留全部 Run 与 Artifact。

上述自动化不代表所有端到端人工浏览器验收已经完成；真实浏览器/WebView 的深链往返、弹层焦点、窄屏、1,000/10,000 条 Project/Task 及 Inbox 长列表仍待专项验证。

## 相关代码/PRD 链接

- [PRD：收件箱与本地工作编排中心](../opc-workspace-PRD.md#56-收件箱与本地工作编排中心)
- [整体功能架构](../functional-architecture.md)
- [schema v12 Inbox 迁移](../../services/sidecar/internal/database/migrations/012_inbox_items.sql)
- [schema v13 Inbox–Task 关系迁移](../../services/sidecar/internal/database/migrations/013_inbox_item_tasks.sql)
- [schema v14 Reminder 迁移](../../services/sidecar/internal/database/migrations/014_reminders.sql)
- [schema v32 重复 Reminder 迁移](../../services/sidecar/internal/database/migrations/032_recurring_reminders.sql)
- [schema v15 Inbox 编排迁移](../../services/sidecar/internal/database/migrations/015_inbox_task_orchestration.sql)
- [schema v23 Task Artifact 来源迁移](../../services/sidecar/internal/database/migrations/023_task_artifact_inbox_projection.sql)
- [schema v24 Task 阻塞来源迁移](../../services/sidecar/internal/database/migrations/024_task_blocked_inbox_projection.sql)
- [schema v25 Task 临期来源迁移](../../services/sidecar/internal/database/migrations/025_task_due_inbox_projection.sql)
- [schema v26 系统维护来源迁移](../../services/sidecar/internal/database/migrations/026_system_maintenance_inbox_projection.sql)
- [schema v28 Project 完成来源迁移](../../services/sidecar/internal/database/migrations/028_project_completion_inbox_projection.sql)
- [schema v29 存储设置迁移](../../services/sidecar/internal/database/migrations/029_storage_settings.sql)
- [Reminder 模块文档](reminders.md)
- [Inbox API](../../services/sidecar/internal/api/inbox_items.go)
- [Inbox–Task 关系 API](../../services/sidecar/internal/api/inbox_item_tasks.go)
- [Inbox 编排 API](../../services/sidecar/internal/api/inbox_orchestration.go)
- [Inbox 来源投影服务](../../services/sidecar/internal/api/inbox_source_projections.go)
- [Project Artifact follow-up 读模型与金链](../../services/sidecar/internal/api/project_artifacts_test.go)
- [Task 临期来源扫描服务](../../services/sidecar/internal/api/task_due_projections.go)
- [系统维护来源投影](../../services/sidecar/internal/api/system_maintenance_inbox.go)
- [备份 API 测试](../../services/sidecar/internal/api/backups_test.go)
- [Inbox API 测试](../../services/sidecar/internal/api/inbox_items_test.go)
- [Inbox–Task 关系 API 测试](../../services/sidecar/internal/api/inbox_item_tasks_test.go)
- [Inbox 迁移测试](../../services/sidecar/internal/database/inbox_migration_test.go)
- [Project 完成来源迁移测试](../../services/sidecar/internal/database/project_completion_inbox_projection_migration_test.go)
- [Inbox–Task 关系迁移测试](../../services/sidecar/internal/database/inbox_task_migration_test.go)
- [Inbox 页面](../../apps/web/src/pages/InboxPage.tsx)
- [Inbox 详情](../../apps/web/src/components/InboxItemDetailModal.tsx)
- [Inbox–Task 关系 UI](../../apps/web/src/components/InboxItemTasksSection.tsx)
- [Inbox 时间线](../../apps/web/src/components/InboxItemEventsSection.tsx)
- [Inbox 来源上下文](../../apps/web/src/components/InboxSourceContext.tsx)
- [Inbox 拆分任务表单](../../apps/web/src/components/InboxTaskOrchestrationModal.tsx)
- [Inbox Query 失效协作](../../apps/web/src/api/hooks.ts)
- [共享 Project 选择器](../../apps/web/src/components/ProjectSelect.tsx)
