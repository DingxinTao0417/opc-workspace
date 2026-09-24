# 客户回访模块

> 目标版本：v0.4。该模块在基础客户 CRUD 完成后交付，不属于 v0.1。

## 定位与边界

- H5-E30（2026-09-21）：本条 `workspace_ui+clients` 可让智能体在已知真实回访 ID 后请求用户确认打开精确客户回访。Sidecar 核验当前记录并只在服务器推导 Client ID，工具回执不带归属、计划备注或结果；当前流给本地 UI 有界 `parent_id`，点击确认卡才跳至既有客户回访定位页。导航不联系客户、不完成回访、不继承写权限或人工同意。见 [AI 精确记录导航](ai-assistant.md#智能体精确记录导航扩展h5-e302026-09-21)。
  客户回访用于在本机计划、提醒和记录下一次客户沟通，形成可追溯的关系维护时间线。它管理“何时需要跟进、为什么跟进、结果是什么”，不充当邮件、短信、CRM 同步或外呼平台。

- 计划、备注、结果和提醒以本机 SQLite 为事实源；AI 独立轨道只有本次发送显式授予 `clients` 后，才可将所查询的记录白名单发送给所选模型，远程 Provider 会产生外发。
- 渠道字段只是用户的本地记录，不代表应用已连接或操作该渠道。
- 第一版不自动发送邮件、短信、社交平台消息或任何外部请求。
- `person` 负责人只表示线下责任归属，不代表已向该人员发送或同步任务。
- AI 或本地 Agent 可生成建议草稿，但不能联系客户、伪造沟通结果或自动完成回访。

## 到期提醒与智能体处理闭环（H2-AB，2026-09-21）

原生创建/编辑按 UTC RFC3339Nano 保存可变精度时间；到期扫描现复用列表的九位小数比较键选取与排序候选，在事务内重读当前 `scheduled_at`，按真实时刻与扫描开始的固定时间比较。已复现并修复整秒、短小数恰好到期时漏提醒及批次顺序错误；未来 1 纳秒不投影，历史固定九位时间兼容。候选中的非 UTC-Z、超过九位小数或不合法时间失败关闭，不回显原值，不改已有时间或迁移；这不是全部导入数据完整性验收。

- 原生全局和单客户回访列表的排序也已改用同一纳秒键，并以 ID 作同刻稳定排序；修复整秒记录错误落到小数时刻之后、分页顺序与 AI 查询不一致的问题。过滤、页码、总数和字段契约保持，没有将浮点日期函数用于排序。
- 单批仍最多 100 条；版本化来源键、原始到期快照、Inbox 和回访审计事务保持。每个计划版本最多投影一次，重扫不重复；101 条积压可连续推进。事务内按最新状态、排期和版本复核，已终态或晚于固定扫描时刻则跳过；仍到期的当前版本可在当次扫描投影。
- 从 Inbox 交给智能体仍需本条授权：`work` 可识别来源类型，但没有 `clients` 时只提示缺少范围，不读取回访/客户身份。用户新消息授权 `work+clients+actions` 后，先用 `workspace_inbox_source` 定位真实回访和客户，再用 `workspace_guide(topic=people_clients)` 与 `workspace_client_records` 读取规则和当前版本/字段，沿用原完成、续排或重排建议和人工确认。没有新的写动作或自动授权。
- 完成必须来自用户明示的真实结果/时间，并在确认卡独立勾选；完成并续排或重排仍是单个领域事务。旧回访/旧 Inbox 保留历史，新计划拥有独立 ID/version；复合回执链接定位新计划，不表示新计划已经完成。源事项 resolved 与回访 completed 是不同事实，模型不联系客户、不生成沟通活动或启动 Agent。
- 页面存在新建、编辑、完成、重排、跳过或取消草稿时，行内“交给智能体”禁用并显示原因，保存/取消后恢复；命令在途同样阻止。选定回访定位面板的“返回原对话”和“关闭定位”使用同一保护，避免卸载时丢草稿；单纯加载或读取失败不阻断返回。它不是全页面路由拦截，不改变客户活动面板的默认行为，也不会把未保存结果、备注或其它草稿复制给模型。
- Inbox 详情经“查看客户回访”打开来源时保留既有合法 `return_session`，让客户记录页继续提供返回原对话入口；返回身份来自页面上下文，不读取模型/来源 payload 中的同名字段，不继承权限、自动发送或执行。

确定性双协议 Harness 旅程使用真实原生创建/投影、查询和审批事务，覆盖缺少 clients 时不披露保护身份、下一条新授权后定位、完成并续排、缺少独立勾选时零写、确认重放幂等、旧 Inbox 历史不变、新旧计划状态/回执路由及授权不继承。模型由本地模拟 Provider 替代，不是实际模型或外部联系验收。

代码证据：[到期扫描](../../services/sidecar/internal/api/client_followup_inbox.go)、[原生时间边界测试](../../services/sidecar/internal/api/client_followup_inbox_time_test.go)、[原生分页排序测试](../../services/sidecar/internal/api/client_followup_query_order_test.go)、[双协议闭环](../../services/sidecar/internal/api/ai_client_followup_source_harness_test.go)、[回访页面](../../apps/web/src/components/ClientFollowupsSection.tsx)、[来源跳转](../../apps/web/src/components/InboxSourceContext.tsx)。API v1/schema 076、调度周期和权限类别不变；真实模型、原生桌面与多时区浏览器专项仍须单独验收。

## 当前实现状态

- Client 基础资料 CRUD、列表、基础详情和 Project 客户关联已交付；客户页面展示真实人工/系统活动，无活动时显示空状态，不伪造沟通记录。
- **C2 数据契约已完成**：schema v35 新增 `client_followups` 与 Go model；计划/完成/跳过/取消字段组合受数据库约束，负责人只允许 active owner/person，终态不可重开、写入必须递增 version，客户存在回访历史时禁止硬删除。插入、更新、删除会递增客户聚合版本，业务 JSON/ZIP 的显式白名单同步包含该表。
- **C3 计划 API 已完成**：`GET/POST /api/v1/client-followups`、`GET/PATCH /api/v1/client-followups/:id` 与 `GET /api/v1/clients/:id/followups` 已提供分页、客户/负责人/状态和服务端 `due_state=overdue` 筛选、创建幂等、详情和 `If-Match` 编辑；成功创建或编辑同事务追加不可变 Workflow Event。列表 `meta.server_now` 与逾期筛选使用同一 Sidecar 当前 UTC 时钟；逾期只包含 `planned` 且计划时刻严格早于该时刻的记录，纳秒边界由统一时间键比较；可与 `status=planned`、负责人组合，其他状态组合返回 `INVALID_FILTER`。计划类写入要求 Client 为 `active` 或 `lead`：停用客户的新建、编辑、重排，以及完成时附带下一计划均返回 `409 CLIENT_FOLLOWUP_CLIENT_INACTIVE`。API 使用 IANA 时区、RFC 3339 计划时间和 active owner/person 双层校验。
- **C4 执行 API 已完成**：`complete` 必填结果，并可选在同一事务创建下一次本地计划；`skip`/确认 `DELETE` 必填原因；`reschedule` 在同一事务取消旧计划、创建带 `rescheduled_from_id` 的新计划，并为两个聚合追加不可变 Workflow Event。终态不可重开。
- **C5 到期 Inbox 投影、详情管理与页面入口已完成**：Sidecar 启动与既有提醒扫描周期会读取到期的 `planned` 回访，以 `followup:<id>:due:<version>` 稳定键创建一条本地 Inbox 事件；重复扫描不重复创建，终态计划不会投影。客户详情通过 `GET /api/v1/clients/:id/followups` 严格校验并分页显示本地计划、终态结果、负责人、优先级、下一步和即时派生的逾期标识，具备加载、空和失败重试状态；同页已提供创建、编辑、完成、跳过、确认取消和重排表单，创建复用幂等键、写入复用回访 `If-Match`，负责人只可选 active owner/person。回访更新、完成、跳过、取消或重排在同一事务归档活动的旧到期 Inbox 投影，仍保留可审计事件，不会让已处理计划继续出现在 Today。Inbox 前端严格校验回访来源 key 与五字段 payload，并在来源上下文深链客户详情中的指定回访；Today 通过受限 `GET /api/v1/inbox-items?source_entity_type=client_followup` 读取前五项待办与准确总数。执行命令不在 Inbox 或 Today 重复实现。
- **C6 稳定性（进行中）**：客户详情把 `datetime-local` 墙上时间按输入的 IANA 时区转换为 UTC，不再用浏览器当前时区重写已有计划；夏令时前跳不存在的时刻明确拒绝，秋季回拨的重复时刻固定选较早物理时刻。时间线顶部已接入全部/待回访/已逾期/已完成/已跳过/已取消状态和 active owner/person 负责人筛选，每次筛选切换会回到第一页；“仅已逾期”严格传 `status=planned&due_state=overdue`，由 Sidecar 而非浏览器时钟计算。包含 planned 的筛选每 15 秒刷新 `server_now` 与到期/终态事实；终态专属筛选不轮询。负责人有任一 `planned` 回访时，Actor 停用 API 明确返回冲突；用户必须改派、完成、跳过或取消计划，终态历史不阻塞停用。客户停用不删除历史或既有计划，详情直接隐藏安排、编辑、重排和完成时续排入口，只保留完成、跳过、确认取消等收口命令；本窗口的新增、编辑或重排表单若在刷新后发现客户已停用会立即关闭，恢复 `active/lead` 后入口才可再次使用，Sidecar 仍负责最终拒绝。时区、筛选、逾期纳秒边界和停用恢复 API 单元测试已覆盖；并发编辑与真实多时区浏览器专项仍待。
- 当前代码没有邮件、短信或第三方 CRM 连接；历史设计材料中的线上客户行为不能作为真实事件使用，且后续以 React 实现与 PRD 为准。

## AI 独立轨道：查询与回访操作确认（H3-B1–B2，已实现）

- 回访切片在 app v0.1.1 / API v1 / schema 72 上交付，自身无迁移；当前 schema 76 另由审批长预览扩容、Agent Run 执行身份、可恢复产出登记及会话计划修订引入，不修改回访表。`workspace_client_records` 在单次 `clients` 授权下查询活动和回访：列表 limit 1–20、offset 0–1000，支持 Client、状态、负责人、逾期过滤；detail 使用真实 ID，备注/结果/下一步/跳过或取消原因一次读取一个 field，Unicode 分页每次 1–4000 字。返回版本、UTC server_now、总数/下一页和包含记录 ID 的客户页面链接，未读页不是已知事实。
- `work+clients+actions` 可提议 `client_followup.create/update/cancel/skip/complete/reschedule`，不提供模型执行入口。创建需真实客户、active owner/person、带偏移计划时刻、明确 IANA 时区、渠道和目的；备注可空，优先级 low/normal/high。修改不能换客户；取消和跳过必填 reason（1–1000 字）。计划允许过去时间，但会成为逾期待办。模型不能创建 Actor、伪造源身份或捏造沟通结果。
- 人工确认与原生 API 共用 `client_followup_commands.go` 的事务入口，重验 planned、expected_version、客户启用和负责人规则；还比较持久预览中的客户/负责人名称与状态，避免关联资料变化却未递增 Followup version 时静默换目标。业务写入、旧到期 Inbox 归档、领域事件及审批决定同事务，任何事件失败整体回滚；已确认卡重放不重复执行。
- 确认卡展示客户、负责人、UTC 时刻/IANA 时区、计划前后值和本地记录边界。无正文结果回执可用于后续对话但不继承权限；成功或不确定失败均取消旧 Client/Followup/Inbox/Actor/搜索请求并刷新事实。H4-D 链接通过 `?followup=<id>` 直接读取并定位该回访，不扫描或改写列表分页。
- `complete` 必填用户提供的实际 result（1–4000 字）和 RFC3339 completed_at，可选 next_step（至多 4000 字，可空）及完整 next_followup；AI 入口不默认猜测完成时刻。确认卡另需勾选“回访已实际完成，结果与时间属实”，仅人工决定接受 `confirm_followup_completed=true`，模型参数、其他动作和拒绝决定不能携带该标记。未勾选返回 `422 CLIENT_FOLLOWUP_COMPLETION_CONFIRMATION_REQUIRED`。停用客户仍可完成（不续排）、跳过或取消旧计划。
- `reschedule` 必填 reason 和完整新计划（负责人、scheduled_at、timezone、channel、purpose，可选 notes/priority），同事务取消并保留原计划，创建带 rescheduled_from_id 的新计划；不能用 update 或两个独立审批冒充。complete 可选 next_followup 使用相同计划字段，不能换客户或注入来源/结果/同意标记；旧计划完成、新计划创建、两个领域事件、Inbox 归档与审批一起提交或回滚。重排和续排要求客户 active/lead、新负责人 active owner/person。
- 确认预览将原计划的终态/结果与 `preview.next_followup` 分开展示，并重验新旧负责人的名称/状态。复合命令的 target_id 指原计划，result_id/result_version 指新计划；仅完成/跳过/取消则结果仍指原计划。后续会话据此得到真实执行回执，不把新计划当已完成。ClientActivity 人工审批写入已由 H3-B3 接通，H4-D 接通精确回访定位；整体 H2–H5 仍在实施，回访操作不自动生成 Activity。
- 证据：[AI 回访动作](../../services/sidecar/internal/api/ai_client_followup_actions.go)、[客户记录工具](../../services/sidecar/internal/api/ai_client_records.go)、[共享命令](../../services/sidecar/internal/api/client_followup_commands.go)、[读取/计划测试](../../services/sidecar/internal/api/ai_client_followups_test.go)、[生命周期/复合回执测试](../../services/sidecar/internal/api/ai_client_followup_lifecycle_test.go)。测试使用隔离 SQLite/mock 上游；真实模型与桌面交互另验收。

## 目标功能

- 为客户创建一次回访计划：时间、渠道、目的、备注、负责人和优先级。
- 按计划、服务端派生的逾期、已完成和已跳过筛选，并支持分页和客户搜索。
- 完成时记录实际时间、结果、下一步和可选的下一次回访。
- 支持跳过和重新安排；重新安排保留原计划及变更历史。
- 在客户详情展示回访时间线，在今日和收件箱展示到期工作。
- 支持本地一次性提醒和稳定去重，应用未运行时在下次启动补偿扫描。
- 客户删除、停用或负责人停用时保持历史可解释。

## 关键用户流程

1. **创建计划**：用户从 active/lead 客户详情选择“安排回访”，填写本地日期时间、渠道、目的、负责人和备注后保存；inactive 客户须先恢复状态。
2. **到期提醒**：计划输入按用户 IANA 时区解释并保存 UTC 时刻；调度器按真实 UTC 时刻判断到期，以计划 ID/version 的稳定事件键创建 Inbox Item，每个计划版本不重复提醒。
3. **处理回访**：用户从今日待办或收件箱来源上下文跳转客户详情，查看上下文并在线下完成沟通。
4. **记录结果**：用户选择完成，填写结果、下一步和实际完成时间；需要持续跟进时可展开下一次计划表单，完成事实、下一条计划及两条审计事件原子写入。inactive 客户可完成既有计划，但不能附带下一条计划。
5. **跳过或重排**：用户填写原因后跳过，或选择新时间；系统保留原时间、原因和责任变化事件。inactive 客户仍可跳过或确认取消既有计划，但不能重排。
6. **异常恢复**：调度漏跑或应用重启后补偿扫描，但不重复生成提醒，不自动改变回访状态。

## 数据、API、状态与事件

### 数据

新增 `client_followups`，至少包含：

- `id`、`client_id`、`assigned_actor_id`
- `scheduled_at`、`timezone`、`channel`、`purpose`、`notes`
- `status`、`priority`、`result`、`next_step`
- `completed_at`、`skipped_at`、`skip_reason`
- 可选 `rescheduled_from_id` 或独立历史事件引用
- `version`、`created_at`、`updated_at`

`client_id` 使用明确删除策略；客户存在任何回访历史时禁止硬删除。负责人仍有 `planned` 回访时禁止停用；终态历史不阻止停用，且不会被删除。

### API

- `GET / POST /api/v1/client-followups`
- `GET / PATCH / DELETE /api/v1/client-followups/:id`
- `POST /api/v1/client-followups/:id/complete`（可选 `next_followup`；与完成事实在同一事务创建）
- `POST /api/v1/client-followups/:id/skip`
- `POST /api/v1/client-followups/:id/reschedule`
- `GET /api/v1/clients/:id/followups`
- `GET /api/v1/inbox-items?source_entity_type=client_followup`（只接受该受限来源，用于 Today 的到期回访读取）

列表支持客户、负责人、状态、服务端 `due_state=overdue` 和分页筛选。写入使用幂等键和乐观并发版本。

### 状态与事件

- 持久状态：`planned / completed / skipped / cancelled`。
- `due` 与 `overdue` 由 `planned + 已存 UTC scheduled_at + 同次服务端时刻` 派生，不作为持久状态；IANA 时区用于输入解释和显示，不使用浏览器时钟判断到期。
- 事件示例：`client_followup.created`、`client_followup.rescheduled`、`client_followup.due`、`client_followup.completed`、`client_followup.skipped`。
- 到期事件通过 `followup:<id>:due:<计划版本>` 之类稳定键去重；重排后旧提醒被审计关闭，新计划产生新键。

## 与其他模块协作

- **客户**：客户详情是主要入口；回访只引用客户事实，不复制联系人资料。
- **Actor 与分派**：owner/person 表示本地负责人；person 不触发外部通知或账号权限。
- **今日与收件箱**：展示到期和逾期项，提供跳转、稍后、拆分任务与本地提醒。
- **任务**：复杂跟进可显式转换或关联 Task；完成 Task 不自动宣称客户已被联系。
- **自动化**：只允许根据本地回访事件创建 Inbox Item、Task 或提醒，不允许发送消息。
- **知识库/AI**：显式上下文仍按选择外发；AI 独立轨道另有单次 `clients` 查询授权，披露活动/回访正文和负责人身份，排除客户联系字段、附件和财务。计划命令另需 `work+actions` 与人工逐项确认；模型输出本身不执行操作。

## 分阶段实施

1. **C1 前置依赖（已完成）**：客户基础 CRUD/详情、Client 活动事实、person 显式关联、Actor/Assignment 基线和 Inbox 人工闭环已交付；回访仍需自己的递增迁移与领域契约。
2. **C2 数据契约（已完成）**：新增迁移、计划/终态字段组合、负责人和删除约束、版本步进、客户聚合失效，以及业务导入导出白名单。
3. **C3 CRUD（计划部分已完成）**：已实现列表、创建、编辑、详情、分页、时区口径、幂等/并发和定向 API 测试；创建/编辑成功后追加 Workflow Event。取消与终态执行转入 C4。
4. **C4 执行闭环（已完成）**：已实现完成、跳过、取消和重排的事务 API；完成表单可选同时安排下一次本地计划，二者及审计事件同一事务提交。
5. **C5 提醒协作（已完成）**：已接入本地调度、启动补偿、Inbox 和事件去重，并在客户详情完成严格 API 读取及本地创建/执行表单；Today 展示真实待办及总数，Inbox 来源上下文可回到客户详情，执行命令仍保持单一入口。
6. **C6 稳定性（进行中）**：已覆盖跨浏览器时区编辑、夏令时前跳拒绝和回拨确定性选择，以及 Actor 有待回访时拒绝停用、终态后允许停用；客户停用后禁止新建、编辑、重排和完成时续排，既有计划仍可完成、跳过或取消；仍需并发编辑及真实多时区浏览器专项。

## 验收标准

- 计划、完成、跳过、重排及下一次计划均可追踪，历史不会被覆盖。
- `due/overdue` 在用户时区、跨午夜和夏令时边界计算正确。
- 调度重扫、应用重启和请求重试不会产生重复提醒或重复下一次计划。
- 完成必须记录结果；跳过必须记录原因；重排保留原计划和审计事件。
- 有活动回访时客户删除约束明确，客户或负责人停用不破坏历史。
- 今日、收件箱和客户详情显示同一事实，跳转目标可定位。
- 加载、空、错误、重试、分页和并发冲突均有前后端测试。
- 断网时全部功能可用，并经检查确认不存在邮件、短信或第三方消息自动发送路径。

## 相关 PRD 与代码链接

- [产品 PRD](../opc-workspace-PRD.md)（§5.4、§10.4.17、§10.7、附录 C）
- [客户页](../../apps/web/src/pages/ClientsPage.tsx)
- [回访迁移](../../services/sidecar/internal/database/migrations/035_client_followups.sql)
- [回访迁移测试](../../services/sidecar/internal/database/client_followup_migration_test.go)
- [Sidecar 路由](../../services/sidecar/internal/api/router.go)
- [前端路由](../../apps/web/src/App.tsx)
