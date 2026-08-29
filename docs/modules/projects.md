# 项目管理模块

> 实现状态截止：2026-08-29（依据当前实现）
>
> 实现基线：app v0.1.0 / API v1 / SQLite schema v34。schema v21 新增项目笔记，schema v22 新增受控项目附件，schema v23–v25 依次新增显式 follow-up Task Artifact、Task 阻塞与 Task 临期→Inbox 来源投影和删除协调；schema v28 新增 Project 完成节点→Inbox 与父项目删除协调；schema v30 增加 `task_submissions.origin` 与父任务推进规则；schema v31 为 Project complete/reopen→Client 只读系统活动建立来源唯一约束；schema v32 只扩展 Reminder；schema v33 增加受限 Automation Rule/Run，并允许用户显式启用“项目完成后提醒检查开票”的纯本地 Inbox 动作；schema v34 只新增 Agent Adapter 诊断事实，不改变 Project。项目级 Focus 读取复用既有 Task/Session 关系。
>
> 版本边界：项目资料、基础生命周期、任务聚合与树/平铺视图、项目内组合筛选及服务端分页、Client 客户关联、共享 Project 选择读模型、项目笔记/附件、所属 Task Artifact 聚合及 nullable follow-up/实时 required 进度、活动时间线、Project complete/reopen→Client 系统活动、显式 follow-up/阻塞/临期/完成节点来源，以及项目 Focus 分析与终态历史已实现，模块仍为**部分完成**；Assignment/Submission 写入继续复用共享 Task/Inbox 详情，财务和其他真实里程碑尚未交付。

## 定位与边界

Project 是任务的上层业务组织单位，用于表达一项工作的目标、客户归属、生命周期、交付范围、时间和合同金额。

- Project 保存自己的业务状态，不把任务、收件箱或发票状态复制为项目状态。
- 项目进度从关联任务派生，不在项目表维护第二份可写百分比。
- 显式 follow-up 产出、Task 阻塞/临期和 Project 完成节点已经生成 Inbox Item；独立交付验收或开票节点仍待真实业务状态。Inbox Item 只跟进下一步，不替代项目生命周期。
- 任务可不归属项目；项目可暂时不关联客户。
- 发票、收入、成本、客户回访等后续业务由各自模块管理，项目详情只聚合读取已落地的事实。

## 当前实现状态

当前状态为**部分完成**，已经具备可运行的项目资料、生命周期、人工笔记、受控附件、所属 Task Artifact 聚合及 follow-up/required 进度、项目级 Focus 报告与终态 Session 历史、显式 follow-up 产出→Inbox→Task 人工闭环、追加式审计时间线、列表和详情纵切。v0.1 不启用 AI、LLM 或 Agent Runtime。

### 已实现

- SQLite schema v33 为当前基线：v3–v20 保留既有 Project 生命周期、聚合版本和不可变 Workflow Event；v21 以加法迁移新增 `project_notes`；v22 新增 `project_attachments`；v23–v25 新增显式 follow-up Artifact、Task 阻塞与 Task 临期→Inbox；v28 新增 Project 完成周期来源、不可变快照和删除协调；v29 增加版本化存储阈值设置；v30 增加 `task_submissions.origin` 与父任务推进规则；v31 只给 Project Workflow Event→Client 系统活动来源增加部分唯一索引，v32 只扩展 Reminder，v33 新增 Automation Rule/Run，均不改变 Project 表契约或回填历史完成动作。
- Go Project model、路由、输入校验和集成测试已经存在。
- 项目 API 支持创建、列表、详情、非生命周期字段编辑，以及受约束的永久删除。
- 列表 API 支持分页、名称/描述搜索、状态和客户筛选、`include_archived` 与白名单排序；未指定状态时默认排除归档项目。默认排序及每个显式排序都追加 `id ASC`，同名项目跨页仍有确定顺序；总数统计和当页读取在同一只读事务完成，避免 `meta.total` 与结果页来自不同快照。
- 创建接口接受可选 `Idempotency-Key`：服务端对规范化请求计算 SHA-256，并保存首次响应快照；同键同请求即使资源后来已编辑或删除也重放首次响应，同键不同请求返回 `409 IDEMPOTENCY_CONFLICT`，旧版无快照记录则拒绝不安全重放。
- 编辑、状态流转和永久删除必须携带 `If-Match`；资源响应返回 `ETag`，旧版本写入返回 `409 VERSION_CONFLICT`。归档项目资料必须先恢复再编辑，否则 PATCH 返回 `409 PROJECT_ARCHIVED`。版本不仅覆盖项目资料和状态，也覆盖任务关联/状态/`actual_minutes`、发票关联/增删、客户名称/删除等会改变聚合响应或删除确认范围的事实。
- 状态只能通过 `start / pause / resume / complete / reopen / archive / restore` 命令流转。完成仍有未完成任务的项目需要显式确认，且不会修改任何任务状态。
- 归档会保存原状态，恢复时回到该状态；缺少历史归档来源的旧数据恢复到 `planning`。
- 永久删除只允许已归档项目，同时要求 `confirm=true` 和最新版本；删除会解除任务、发票的项目外键并返回解除数量，不会删除这些记录。
- 项目响应从关联任务实时派生总数、已完成数、进行中数、剩余数、完成百分比及 `actual_minutes` 合计，并返回客户名、发票数和当前允许操作。六状态上线后仍只有 `done` 计为已完成；cancelled 保留在总数和剩余口径中，项目语义未随 D1 改写。
- 项目页使用真实 API 展示卡片、服务端搜索、状态筛选和每页 12 条分页，并覆盖加载、空、错误和重试状态。
- 项目详情任务浏览器直接按 Project 条件调用 Task 服务端分页，每页 20 项；无条件时默认读取根任务并复用 `parent_task_id` 查询按需展开任意深度子任务，也可切换平铺列表。标题/描述、状态、优先级、类型、单标签及已/未排期可组合筛选；条件激活时使用平铺结果并显示父任务上下文，避免把局部命中冒充完整树，清除后恢复任务树。详情同时支持资料编辑、从项目内新建并预选项目的任务、派生进度/工时、状态操作、归档恢复，以及带二次确认的永久删除；版本冲突会刷新详情事实。
- 项目新建/编辑与项目列表客户筛选共用 `ClientSelect`：每页从真实 Client API 读取 20 条，输入经 250 ms 防抖后执行服务端搜索，支持稳定上一页/下一页并向请求传递取消信号，不再串行拉取全部 Client。跨页或失败时保留当前选择，inactive Client 保持可见可选；用户仍可显式点击“清除客户”解除关联。普通项目列表默认排除归档项，Client 详情为读取完整关联历史显式使用 `include_archived=true`。
- Task 新建/编辑、Tasks 项目筛选、批量目标项目和 Inbox 拆分任务共用 `ProjectSelect`；组件固定每页 20 条、250 ms 防抖搜索，并以 `q / page / includeArchived` 隔离 Query key、传递取消信号、按 ID 去重。任务列表、详情、编辑和状态更新响应关联项目时返回 `project_name`，任务行直接显示项目名称。
- 归档项目拒绝新建任务关联或把其他任务改入，返回 `409 PROJECT_ARCHIVED`；既有关联任务仍可编辑且保持原关联。归档转换进行中时，详情页的新建任务入口会禁用。
- 项目创建、资料编辑、`start/pause/resume/complete/reopen/archive/restore` 和永久删除会在同一数据库事务追加 `project_*` Workflow Event，记录内置 owner、请求 ID、时间及前后项目快照；事件写入失败时整个项目命令回滚，创建幂等重放不会重复追加事件。
- complete/reopen 若事件发生时 Project 有 `client_id`，会在同一事务以对应 Workflow Event ID 向该 Client 写入一条只读 `system_reference`；无 Client 不投影，任一 Client Activity 或既有完成 Inbox 投影失败都会回滚 Project 状态与事件。
- `GET /api/v1/projects/:id/events` 按 `created_at / command_seq / id` 稳定倒序分页，返回当前 Project `ETag` 和 `meta.project_version`。项目详情默认读取首批记录，展示状态变化或资料变更字段，并提供独立加载、空、错误重试和“加载更早”状态。
- 项目详情支持人工笔记的幂等创建、稳定分页、版本化编辑、带原因软删除及删除历史查看；记录人固定为本地 owner。每次创建、编辑或软删除都由数据库 trigger 原子递增 Project 聚合版本，归档项目只读。
- 项目详情支持受控附件上传、稳定分页、按需下载、完整性状态、带原因软删除和删除历史；上传强制 metadata-first 严格 multipart，文件非空且最多 50 MiB，完整请求最多 100 MiB。创建/删除使用 Project `If-Match` 与可选幂等键，归档项目只读。
- Project Attachment 文件与 Task Artifact、Client Attachment 共享数据库身份绑定的受控 store，但三类元数据保持独立。下载前复验 size/SHA-256；缺失或不匹配会持久化完整性状态并拒绝下载。永久删除 Project 时 active 文件先移入 trash，数据库事务失败则恢复，成功后清理并保留 `project_attachment_deletion_tombstones`。
- `GET /api/v1/projects/:id/artifacts` 在同一只读事务中校验 Project、分页聚合其所属 Task 的既有 Artifact，并返回 Task 标题/当前状态、Submission 序号、Project 聚合版本，以及 nullable `followup`。存在稳定 task_artifact 来源时，follow-up 含 Inbox ID/version/status/policy/`source_deleted_at` 与实时 required 进度；未标记产出为 `null`。默认隐藏已删除产出，可显式查看删除历史；不复制 Artifact 正文、文件、删除或验收事实。
- Artifact 列表继续返回 Project 聚合数值 `ETag`，并与 `meta.project_version` 表示同一 Project 并发版本；follow-up 变化不传播进 Project version。Inbox 写并发由 `followup.inbox_item_version` 表达，当前 Project UI 只深链 Inbox。所有会改变 follow-up 的成功 Inbox mutation 会失效可信来源 Project，split 另失效 Task、Today 与 Project。
- 项目详情把产出区放到任务区之后；每项展示类型、来源 Task/批次、待拆分/跟进中/已解决/已忽略、required 完成度及阻塞/待验收/取消提示，可打开共享 Task 或深链 Inbox。实际正文、文件、删除和验收仍由 Task/Inbox 领域完成。
- `GET /api/v1/focus-sessions` 与 `GET /api/v1/stats/focus` 均支持可选 `project_id`。参数必须是小写 canonical UUID；空、非法、大小写或其他非 canonical 表达返回 `400 INVALID_PROJECT_ID`，规范但不存在的 Project 返回 `404 PROJECT_NOT_FOUND`，归档 Project 仍允许读取。
- 项目 Focus 归属在查询时通过 Session 当前绑定 Task 的当前 `project_id` 派生，不保存历史 Project 快照。Task 改绑后旧 Session 随当前归属重分类；Session 无 Task、Task 已删除或 Task 当前未归项目时，不进入任何项目过滤结果。
- 项目详情 Focus 区可切换最近 7 天、最近 30 天和本月，显示 completed-only 总时长、完成 Session 数、当前/区间最长连续天数及每日趋势；终态历史包含 completed/cancelled/interrupted，按 `ended_at DESC, id ASC` 稳定分页，并把取消/中断时长明确作为审计“记录时长”，不冒充有效项目工时。
- Focus 报告与历史具有各自的加载、空、错误、重试和分页状态，读取失败不阻断项目主详情、任务或其他子区块；历史页总数回缩时会收敛到仍有效页。归档项目仍可只读查看报告和历史。

### 已知缺口

- 项目详情任务区已使用服务端分页、树/平铺和组合筛选；ClientSelect 与 ProjectSelect 已消除候选项全量串行请求。Project Artifact→Inbox→Task 的事实链与自动化金链已交付，但真实浏览器/WebView 的深链往返、弹层焦点、窄屏、1,000/10,000 条 Project/Task 和 Inbox 长列表仍待专项验收。Task 并发版本、Assignment 与 manual Submission/Artifact 验收继续由共享任务详情承载，不在 Project 复制写控件。
- 项目工时继续对任务表当前 `actual_minutes` 求和；schema v11 已让 completed Focus Session 通过精确秒数账本向 Task 追加完整分钟，再沿既有聚合触发器刷新项目工时。项目详情报告直接读取 completed Session 的闭合正时长 interval，终态历史读取 Session 审计事实；两者都不成为第二份可写工时。
- 没有发票明细；当前只返回发票计数，用于解释硬删除影响。项目附件、Task Artifact 产出聚合、人工项目笔记和不可变系统写命令时间线各自维护事实，不互相替代。
- schema v23–v25 已接显式 follow-up Task Artifact、Task 阻塞与 Task 临期→Inbox；schema v28 把 Project `complete` 作为明确本地完成节点同事务投影到 Inbox。独立验收、开票等尚无真实 Project 状态，不提前伪造来源。
- 没有项目里程碑、真实收入/成本聚合或开票操作。

## 当前用户流程

### 创建并启动项目

1. owner 输入名称，可选 Client、日期、金额和颜色；共享 ClientSelect 每页读取 20 条并以 250 ms 防抖搜索服务端，可稳定上一页/下一页、重试失败请求，也可明确点击“清除客户”。已有选择即使不在当前页、搜索失败或为 inactive 仍保留并可见，只有用户显式清除才提交 `null`。前端为同一次失败重试复用 `Idempotency-Key`，服务端保存规范化请求摘要和首次 `planning` 项目响应快照。
2. 在项目详情新建任务时，任务弹窗自动预选当前项目；也可在普通任务新建/编辑中选择非归档项目。
3. owner 执行“开始项目”，服务端校验 `If-Match` 后从 `planning` 进入 `in_progress`。
4. 项目卡片和详情从任务数据实时计算完成进度与 `actual_minutes` 合计。

### 完成、归档与恢复

1. `in_progress` 或 `paused` 项目可完成；仍有未完成任务时，前后端都要求显式确认。
2. 完成项目只改变项目状态，不会自动完成、恢复或删除关联任务。
3. 任一非归档状态可归档；归档项目从默认列表隐藏，可通过“已归档”筛选找到。
4. 恢复项目回到归档前状态，并保留任务、任务工时和发票关联。

### 永久删除

1. 只有已归档项目才显示永久删除入口。
2. UI 展示将被解除关联的任务和发票数量，并明确项目笔记、附件元数据和附件文件会永久删除，再要求二次确认。
3. Sidecar 再次校验 `If-Match`、`confirm=true` 和项目归档状态，并把 active 项目附件移入受控 trash、写入聚合删除墓碑。
4. 删除命令在同一事务追加 `project_deleted` 事件并删除 Project；失败时恢复已移动附件，成功后清理 trash。关联任务和发票保留但其 `project_id` 被置空，项目笔记和附件元数据级联删除；删除事件与附件墓碑继续保留供审计/恢复判定，永久删除本身不可恢复。

### 查看活动时间线

1. 项目详情加载首批 20 条项目写命令事件；它们只描述项目事实变化，不承载当前状态。
2. 每条记录显示行为、发生时间和 owner，并在生命周期变化时显示前后状态，在资料编辑时只列出发生变化的字段名称，不直接展开完整快照。
3. 超过一页时可按稳定倒序加载更早记录；读取失败只影响时间线区，可独立重试，不阻断任务、状态和资料操作。

### 让关联客户看到项目状态事实

1. Project complete/reopen 先完成状态门禁并生成不可变 Project Workflow Event；若当时存在 `client_id`，Sidecar 在同一事务创建 Client 系统活动。
2. 活动标题保存事件时 Project 名称和“已完成/已重新打开”，来源身份固定为 Workflow Event ID，创建者为内置 system，body 为空；一个事件最多一条。
3. Project 后续改绑或解除 Client 不搬迁已经发生的活动；新的 reopen/complete 只投影到各自事件发生时的关联 Client。迁移和启动不回填历史 Project Event。
4. Client 时间线隐藏内部 event ID，并明确标为项目生命周期系统只读；该事实不代表客户回访、邮件或任何外部通知。

### 记录项目笔记

1. 非归档项目可填写 1–200 字符标题、1–10,000 字符正文和不晚于当前容差的 RFC 3339 发生时间；同一次失败重试复用 `Idempotency-Key`。
2. 项目详情按发生时间稳定倒序分页展示笔记，读取失败只影响笔记区；可切换查看已删除历史。
3. 编辑和删除都使用笔记自己的 `ETag` / `If-Match` 版本。删除必须 `confirm=true` 并填写原因；正文随后不再返回，但删除时间、执行人和原因保留。
4. 项目归档后笔记只读；永久删除 Project 会级联删除其笔记，UI 在确认前明确提示这一不可恢复边界。

### 查看项目产出

1. 项目详情按 Artifact 创建时间和 ID 稳定倒序读取所属任务的产出；默认每页 6 项，服务端最大每页 100 项。
2. 产出区紧随项目任务区；列表显示安全摘要、来源 Task、当前 Task 状态和 Submission 序号，不在 Project 创建第二份正文、文件或验收状态。
3. `followup=null` 表示该 Artifact 没有稳定来源事项；存在时显示待拆分/跟进中/已解决/已忽略、required 完成度及阻塞/待验收/取消提示，并提供 Inbox 深链。
4. owner 可切换删除历史；已删除项只展示墓碑摘要和原因。“打开任务”进入共享详情处理正文、下载、删除和验收；“打开/查看跟进”进入 `/inbox/:id`。
5. 归档项目仍可读取历史产出。列表 `ETag` 与 `meta.project_version` 都是 Project 聚合版本；follow-up 的 Inbox 并发版本单独位于 `followup.inbox_item_version`，实时变化依靠成功 Inbox mutation 失效 Project Query 刷新。

### 从项目产出发起后续工作

1. owner 在项目所属的 manual Task 提交产出，并只对确实需要下一步工作的 Artifact 勾选 `requires_followup`。
2. Sidecar 在提交事务内为每个标记 Artifact 创建一条稳定去重 Inbox Item；条目保存 Artifact/Task/Submission 及 Project ID/名称快照，不复制正文或项目状态。
3. owner 从 Project 产出深链 Inbox；拆分面板对可信来源快照默认带入 Project，新增草稿同样继承，但每项可清除或改选。用户填写独立完成条件，选择 required、owner/person 负责人及 none/manual 验收策略；person 只记录本地责任。
4. none Task 可直接 complete；manual Task 由 owner/person assignee 产出、owner reviewer 接受或返工。所有活动 required Task done 后 Inbox 自动 resolved/100%。
5. 来源项仍活动时 Artifact 或来源 Task 不可删除；解决/忽略来源项后才可删除，Inbox 会保留快照并显示来源已删除。

### 管理项目附件

1. 非归档项目可选择一个本地文件，先在页面预览原文件名、可编辑附件名称和大小，再明确确认上传；浏览器 File 草稿在请求前不写入数据库。
2. 前端携带当前 Project `If-Match` 和稳定幂等键，Sidecar 严格读取首个 `metadata` part 与唯一 `file` part，写入 staging、校验限制并原子发布到 `objects/<attachment-id>`。
3. 列表默认只显示 active 附件，可切换删除历史；每项展示大小、记录人、时间和 verified/missing/mismatch 状态。下载只走鉴权 content API，并在响应前复验大小和 SHA-256。
4. 删除必须填写原因并再次使用最新 Project 版本；服务端先写不可变墓碑、移动文件到 trash，再软删除元数据。数据库失败会恢复文件，物理文件已缺失时仍记录 missing 并完成授权删除。
5. 归档项目仍可读取和下载附件，但添加和删除入口禁用；必须先恢复项目才可写入。

### 查看项目专注分析与 Session 历史

1. 项目详情在任务区与产出区之后独立加载 Focus 区；用户可切换最近 7 天、最近 30 天或本月。浏览器发送当前 IANA 时区，服务端按当地自然日边界处理跨午夜和 DST。
2. 报告卡展示 completed Session 的闭合正时长 interval 总时长、distinct 完成数、当前/区间最长连续天数和逐日趋势；零事实日期仍保留，空项目显示明确空态。
3. 历史卡每页 6 条，按稳定终态顺序展示 completed/cancelled/interrupted、当前 Task 标题或“任务不可用”、结束时间与记录时长；翻页不会改变项目或 Task 事实。
4. 报告与历史各自区分加载、错误、重试和空状态；任一区失败不阻塞另一卡或项目主详情。归档项目保持可读。
5. 归属始终按 Task 查询时当前项目判断。Task 从项目 A 改到 B 后，旧 Session 会从 A 的结果移到 B；无 Task、Task 已删除或 Task 当前无项目的 Session 不进入 A/B 的过滤结果。

## 数据、API 与状态机

### 当前数据

- 当前 schema v33 的 `projects` 字段仍为 `id, name, description, client_id, status, start_date, due_date, amount_minor, color, version, archived_from_status, created_at, updated_at`；`project_notes` 保存版本化人工笔记；`project_attachments` 保存受控附件事实。附件新增/软删除通过 trigger 递增 Project 聚合版本，删除墓碑在父项目删除后仍保留；v30–v33 的扩展不改变这些 Project 字段。
- 当前允许状态：`planning / in_progress / paused / completed / archived`。
- `version` 从 1 开始，每次资料编辑或状态流转递增；`archived_from_status` 只用于恢复归档前状态。
- 进度和工时不是项目表字段，而是查询时分别从任务状态和任务 `actual_minutes` 派生。
- 项目级 Focus 报告和历史同样不是 Project 表字段：服务端只读 JOIN `focus_sessions`、`focus_session_intervals`、`tasks` 和现有标签关系，不增加 schema migration，也不复制 Project 快照。
- 项目事件保存在通用 `workflow_events`，`aggregate_type='project'`、`aggregate_id=Project.id`；action 为 `project_created/project_updated/project_started/project_paused/project_resumed/project_completed/project_reopened/project_archived/project_restored/project_deleted`。事件追加后受既有数据库 trigger 保护，不能更新或删除。complete/reopen 的 Client 系统活动只引用事件 ID 作为稳定来源，不复制或替代 Project 当前状态。

### 当前 API

| 方法   | 路径                                           | 当前行为                                                                                                                                                                                                          |
| ------ | ---------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| GET    | `/api/v1/projects`                             | `page` 默认 1，`page_size` 默认 50/最大 100；`q` 搜索名称/描述（最多 200 字符），`status/client_id` 筛选，`sort` 白名单排序；默认隐藏归档项目，`include_archived` 经布尔解析校验，true 时供需要完整关联历史的读取 |
| POST   | `/api/v1/projects`                             | 创建 `planning` 项目；可选 `Idempotency-Key` 保存规范化请求 SHA-256 与首次响应快照，同请求可稳定重放并拒绝不同请求复用                                                                                            |
| GET    | `/api/v1/projects/:id`                         | 返回项目资料、派生任务汇总、发票数、允许操作和 `ETag`                                                                                                                                                             |
| GET    | `/api/v1/projects/:id/events`                  | 项目活动时间线；`page` 默认 1，`page_size` 默认 20/最大 100，稳定倒序；返回 owner、request、前后快照、Project `ETag` 与 `meta.project_version`                                                                    |
| GET    | `/api/v1/projects/:id/artifacts`               | 所属 Task Artifact 只读聚合；稳定分页/删除历史；每项返回 nullable followup 的 Inbox ID/version/status/policy/source deletion/实时 required progress；响应 Project 数值 ETag 与 `meta.project_version`，不返回正文 |
| GET    | `/api/v1/projects/:id/attachments`             | 项目附件稳定倒序分页；默认隐藏软删除记录，`include_deleted=true` 可查看删除历史；返回 Project `ETag` 与 `meta.project_version`                                                                                    |
| POST   | `/api/v1/projects/:id/attachments`             | Project `If-Match` + 可选幂等键；严格 `metadata` + 单文件 multipart；归档项目拒绝写入                                                                                                                             |
| GET    | `/api/v1/project-attachments/:id`              | 返回附件元数据、记录人、完整性/删除状态和当前 Project 聚合版本                                                                                                                                                    |
| GET    | `/api/v1/project-attachments/:id/content`      | 仅 active 附件；复验 size/SHA-256 后以 attachment/no-store/nosniff 响应，缺失或不匹配会持久化状态并拒绝下载                                                                                                       |
| DELETE | `/api/v1/project-attachments/:id?confirm=true` | Project `If-Match` + 1–1,000 字符原因 + 可选幂等键；写墓碑、trash 补偿后软删除，归档项目拒绝写入                                                                                                                  |
| GET    | `/api/v1/projects/:id/notes`                   | 项目笔记稳定倒序分页；`include_deleted=true` 可查看删除历史；返回 Project `ETag` 与 `meta.project_version`                                                                                                        |
| POST   | `/api/v1/projects/:id/notes`                   | 幂等创建人工笔记；归档项目返回 `409 PROJECT_ARCHIVED`                                                                                                                                                             |
| GET    | `/api/v1/project-notes/:id`                    | 读取单条笔记及其版本、记录人和当前 Project 聚合版本                                                                                                                                                               |
| PATCH  | `/api/v1/project-notes/:id`                    | `If-Match` 编辑标题、正文或发生时间；已删除或归档项目拒绝修改                                                                                                                                                     |
| DELETE | `/api/v1/project-notes/:id?confirm=true`       | `If-Match` + 删除原因执行软删除，隐藏正文并保留审计字段                                                                                                                                                           |
| PATCH  | `/api/v1/projects/:id`                         | 仅编辑非归档项目的资料字段；状态字段不能通过通用 PATCH 修改；缺少 `If-Match` 返回 428，旧版本或归档状态返回 409                                                                                                   |
| POST   | `/api/v1/projects/:id/transitions`             | 执行 `start/pause/resume/complete/reopen/archive/restore`；缺少 `If-Match` 返回 428，旧版本返回 409                                                                                                               |
| DELETE | `/api/v1/projects/:id?confirm=true`            | 仅永久删除已归档项目；必须携带 `If-Match`，返回 `deleted_id/detached_tasks/detached_invoices`                                                                                                                     |
| GET    | `/api/v1/tasks?project_id=:id`                 | 读取项目任务；任务资源读取与编辑/状态响应包含可选 `project_name`；归档项目不接受新的任务关联                                                                                                                      |
| GET    | `/api/v1/focus-sessions?project_id=:id`        | 按 Task 查询时当前项目归属读取终态 Session，支持既有 `task_id/status/page/page_size`；归档项目可读，canonical UUID 非法返回 400，不存在返回 404；无 Task/Task 已删除/当前无项目不进入结果                         |
| GET    | `/api/v1/stats/focus?project_id=:id`           | 按既有 `date_from/date_to/timezone` 契约读取项目报告；只聚合 completed Session 的闭合正时长 interval，保留 1–93 当地日、IANA/DST、跨午夜、Streak 与完整零值序列                                                   |

项目创建和编辑会校验名称 2–100 字符、描述最多 10000 字符、日期格式与先后顺序、非负金额、`#RRGGBB` 颜色和已有客户外键。

### 当前状态流转

```text
planning --start--> in_progress --pause--> paused --resume--> in_progress
                          \                   /
                           --complete--------> completed --reopen--> in_progress

planning / in_progress / paused / completed --archive--> archived
archived --restore--> archived_from_status（缺失时回到 planning）

独立 DELETE：archived + If-Match + confirm=true --> permanently deleted
```

归档、恢复和完成均不改写关联任务状态。`available_actions` 只返回当前允许的生命周期转换；归档状态只返回 `restore`，永久删除是独立且额外确认的 `DELETE`，前端不把它混作状态命令。

项目 `version` 是整个响应的并发令牌：直接项目写入会显式递增，任务插入/删除、项目关联/状态/`actual_minutes` 变化、发票关联/增删，以及客户名称/删除由 schema v5 trigger 原子递增。这样用户在看到旧任务数、工时、发票数或客户名时，完成、归档、编辑和硬删除都会因旧 `If-Match` 被拒绝并刷新。

Artifact 聚合沿用该 Project 数值 `ETag`，`meta.project_version` 与其相同；它不是完整响应内容哈希。Inbox 状态与实时 required 进度不递增 Project version；前端在成功 Inbox mutation 后先取消来源 Project 的在途读取，再失效查询，Artifact 请求消费 `AbortSignal`，避免旧响应回填。`followup.inbox_item_version` 才是 Inbox `If-Match` 值；当前 Project 页面仅导航到 Inbox，不直接提交写命令。

## 后续目标

### 客户与项目资料

- Client CRUD、共享服务端搜索/分页选择器、客户选择/改绑/解除和项目列表客户筛选已交付；Client 停用保留关联且在选择器中继续可见可选，永久删除会把 Project 外键置空并使相关聚合版本失效。
- 后续只在真实需求出现时扩展客户标签、项目风险信号或更完整排序交互，不新增可写进度或复制客户活动事实。

### 任务、产出与后续工作

- 项目详情任务浏览器已复用 Task 父子事实、服务端搜索/状态/优先级/类型/标签/排期筛选、顶层与展开分页和共享详情入口。Assignment、完整生命周期和 Artifact 验收仍统一在共享任务详情处理，不在 Project 复制写命令。
- 项目交付类 Task 提交 Artifact，或 owner 显式标记 `requires_followup` 时，才可按稳定事件键幂等生成 Inbox Item。
- 上述显式标记链路已由 schema v23 交付：每个 Artifact 一条稳定来源，项目名称只作为不可变导航快照，不把 Project 状态复制到 Inbox。v9.18 读模型在 Project 暴露 nullable follow-up 和实时 required 进度，但保持 Project/Inbox 版本边界分离。
- owner 在收件箱把产出拆成修改、发布、客户确认等工单，默认继承但可清除/改选来源 Project，填写完成条件并分派给 owner/person；manual Task 由 owner reviewer 验收。v0.1 不显示或执行 agent。
- 项目达到交付、验收或开票节点的自动事件必须等 Workflow Event、Inbox 规则和对应业务模块交付后再启用。

### 项目详情增强

- 追加式项目写命令活动时间线、可编辑人工笔记、受控项目附件、所属 Task Artifact 产出聚合，以及项目级 Focus 报告/终态 Session 历史均已交付。
- Focus Core 已把 completed Session 的新增完整分钟幂等累计到 Task；项目继续从 `actual_minutes` 派生卡片工时，详情分析则复用 completed-only interval 读模型，不新增第二份 Focus 汇总事实或历史项目快照。
- 发票模块交付后显示关联发票；财务模块交付后显示真实收入/成本。

## 与其他模块协作

| 模块      | 当前与后续协作方式                                                                                                                                                                                                                                         |
| --------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 任务      | 当前已支持项目选择、项目名展示、项目任务树/平铺列表、项目内搜索/状态/优先级/类型/标签/排期筛选与服务端分页、Task 父子/版本、派生进度/工时、共享任务详情中的 D2 产出与验收，以及项目详情的只读 Artifact 聚合；内嵌 D2 写控件不在 Project 复制。             |
| 客户      | 已支持 Client CRUD、可选 `client_id`、客户名称聚合、共享服务端搜索/稳定分页的表单选择和项目/任务筛选、改绑/解除、Client 详情中的完整关联项目读取、人工活动时间线、Project complete/reopen 只读系统活动、受控附件和 person 显式关联；其他外部来源仍待实现。 |
| 收件箱    | 显式 follow-up Task Artifact、Task 阻塞/临期和 Project 完成节点已投影；Artifact 聚合读取 Inbox version/status/policy/progress 并深链，split 可继承来源 Project；成功 mutation 失效来源 Project，保持两个聚合版本独立。                                     |
| Actor     | 项目本身不分派；项目内可执行工作必须落为 Task，再通过已交付的任务详情 Assignment API/UI 分派。                                                                                                                                                             |
| 专注      | 已从 completed Focus Session 经 Task 精确秒数账本取得新增完整分钟，并沿既有 Task 聚合刷新项目工时；项目详情通过可选 `project_id` 按 Task 当前归属读取 completed-only 报告和终态历史，不复制 Session 或 Project 事实。                                      |
| 发票/财务 | 当前只聚合发票数量用于删除说明；发票详情、收入和成本属于后续版本。                                                                                                                                                                                         |
| 今日      | 可展示已有任务项目名；项目节点和风险聚合尚未接入。                                                                                                                                                                                                         |

整体协作关系参见[整体功能架构](../functional-architecture.md)。

## 分阶段实施

1. **项目事实与 API（已实现）**：当前 schema v33 保留 schema v3–v20 的 Project 结构与聚合 trigger，包含独立 `project_notes`、受控 `project_attachments`、follow-up Artifact、Task 阻塞/临期、Project 完成 Inbox 与 Client 生命周期活动协调；v31–v33 不改变 Project 表。Go model、CRUD、校验、分页/搜索/筛选、快照式创建幂等、覆盖聚合事实的乐观锁、状态流转、归档恢复和受约束硬删除均已实现。
2. **前端基础纵切（已实现）**：真实新建/编辑、卡片列表、详情、加载/空/错误/重试、状态操作、归档恢复和删除确认。
3. **任务与工时协作（当前纵切已实现）**：Task 新建/编辑、Tasks 筛选/批量目标和 Inbox 拆分共用有界分页 ProjectSelect，生产路径不再串行拉全项目；`project_name`、Task 事实版本、派生进度和 `actual_minutes` 已接通，Focus Core 已接入 Task 工时传播。项目详情复用父子任务树、平铺列表、组合筛选、顶层/子层分页和共享任务详情，并已接项目级 7 天/30 天/本月报告与终态历史；真实浏览器与大数据量性能仍待专项验证。
4. **客户协作（基础范围已实现）**：Client CRUD、项目客户选择/改绑/解除、Project/Task 客户筛选已共用每页 20 条、250 ms 服务端搜索和稳定分页的 ClientSelect；当前选择保留、inactive 可见可选、取消信号、加载/空/错误重试/更多提示和 combobox 键盘语义已接通。双向聚合版本传播、人工活动、Project complete/reopen 系统活动、受控附件和显式 contact 关联也已交付；真实浏览器/窄屏/大数据量专项以及邮件/日历等其他来源、回访和财务仍待验收或实现。
5. **项目审计（已实现）**：Project 创建/编辑/全生命周期/删除与追加式 Workflow Event 同事务提交，幂等创建重放不重复写事件；分页 API 和详情时间线覆盖状态变化、资料字段变化、加载/空/错误/重试/更早记录。
6. **人工笔记（已实现）**：schema v21、幂等创建、稳定分页、严格响应、笔记级乐观锁、带原因软删除、归档只读、Project 版本传播、业务 JSON 导出和详情完整交互。
7. **受控附件（已实现）**：schema v22、metadata-first multipart、稳定分页、完整性校验、鉴权下载、幂等上传/删除、归档只读、Project 版本传播、软删墓碑、父聚合删除补偿、备份恢复和业务 JSON 元数据导出均已接通。
8. **产出与 Inbox 协作（已实现当前人工闭环）**：Task Artifact、Workflow Event、项目级 nullable follow-up/实时 required 读模型、产出区状态与 Inbox 深链、来源 Project 继承/清除、完成条件、owner/person 分派、manual owner 验收、共享 Task 详情、缓存失效及自动解决已交付。Go 金链覆盖 complete + submit/waiting_review + accept→resolved/100%；不把开票等未来节点混入当前状态机，也不接 AI/Agent。
9. **后续业务增强**：v0.3 里程碑，v0.4 发票/财务；不阻塞基础项目管理纵切。

## 验收口径

### 当前基础纵切

- 创建、编辑、查询、分页、搜索、状态筛选、状态流转、归档和恢复均持久化且可重试。
- 名称、日期、金额、客户外键、版本和状态转换均由服务端校验。
- 项目进度和工时汇总与关联任务一致，且没有第二份可写进度。
- 完成项目不会隐式完成任务；归档/恢复保留关联；硬删除仅在归档和双重确认后解除关联。
- 旧 `If-Match` 写入被拒绝；同幂等键不同创建请求被拒绝。
- 任务/发票/客户聚合事实变化会使旧项目版本失效；归档项目不能接受新的任务关联，既有关联任务编辑不被误拒绝。
- 列表和详情具备加载、空、错误和重试状态。
- Project 新建/编辑和项目列表筛选共用 ClientSelect；首批及后续页请求有界，250 ms 搜索走服务端，旧请求可取消，跨页/错误不清除当前选择，inactive 客户可见可选，加载、空、错误重试、更多提示和 combobox 键盘语义均有自动化覆盖。
- Task 新建/编辑、Tasks 项目筛选、批量目标和 Inbox 拆分共用 ProjectSelect；每页 20 条、250 ms 服务端搜索、`q / page / includeArchived` Query key、AbortSignal、ID 去重、显式清除、选中详情/名称 fallback、加载/空/错误重试/更多提示和 combobox 键盘语义均有实现与自动化证据。默认候选排除归档项目，当前归档选择仍可见；同名排序追加 `id ASC`，列表 `COUNT` 与当页 `SCAN` 共用只读事务。
- 项目创建、资料修改、状态流转和删除各追加一次不可变事件；事件失败回滚项目写入，创建幂等重放不重复事件。
- 有 Client 的 complete/reopen 各产生一条来源唯一的只读 Client Activity；无 Client、旧版本或失败事务不产生孤立活动，改绑不移动历史，Client 聚合版本和最近动态随成功投影更新。
- 笔记创建可幂等重放且不重复写入；编辑与删除使用笔记版本，软删除隐藏正文并保留原因；归档项目拒绝笔记写入。
- 笔记新增、编辑和删除分别只递增一次 Project 聚合版本；旧 Project 并发视图因此失效。
- 项目产出只返回所属 Task Artifact，稳定分页、默认排除删除项、显式历史可见且不泄漏正文；每项 `followup` 为 null 或严格包含 Inbox ID/version/status/policy/source deletion/实时 required progress。
- Artifact 列表继续返回 Project 数值 `ETag / meta.project_version`；Inbox/Task 派生变化不传播为 Project version，`followup.inbox_item_version` 独立用于 Inbox 写并发；成功 mutation 的刷新按 cancel→invalidate 顺序执行，Artifact 请求消费 `AbortSignal`。
- 项目详情产出区位于任务区之后，能区分加载、空、错误与删除历史，显示四种跟进状态及阻塞/待验收/取消提示，通过精确 Task ID 打开共享详情并深链 Inbox，不在 Project 复制查看、下载或验收写命令。
- 项目附件上传严格限制请求顺序、数量和大小，使用幂等键与 Project 版本；列表稳定分页，下载复验完整性，软删除保留原因和墓碑，归档状态只读。
- Project 永久删除对附件文件和数据库执行可补偿操作；手动备份/恢复包含 active Project objects，业务 JSON 只导出附件元数据与 active 文件摘要而不嵌入正文。
- 时间线分页稳定，返回真实 owner、请求 ID、前后快照和 Project 版本；前端的加载、空、错误重试与“加载更早”不影响项目其他区域。
- 两个 Focus 读接口未传 `project_id` 时保持原全局行为；传参时严格区分 canonical UUID 400、Project 不存在 404 与归档 Project 正常读取。
- 项目历史按 Task 当前项目归属稳定分页，包含三种终态；Task 改绑会重分类，Task 删除/无项目后从项目过滤结果消失，取消/中断记录不进入有效报告。
- 项目报告只统计 completed Session 的闭合正时长 interval，保留 IANA 时区、跨午夜、DST、日期上限、连续天数和零事实序列；项目详情的 7 天/30 天/本月切换与报告/历史独立反馈不阻塞主详情。
- Query key 以项目、日期和页码隔离；Task 编辑/删除及批量改项目使报告与历史失效，批量标签变更、Tag 更新/删除和 Project 更新只使报告失效，Project 删除只把报告标为 stale 且不在导航前重取已删除项目。改期、排序、生命周期、Artifact/Submission 等不影响当前项目归属/标签/标题的操作不会刷新这两个读模型。

### 完整模块仍待验收

- ClientSelect 的全量串行拉取已消除，但 1,000/10,000 条客户下的真实响应、输入和翻页性能，以及真实浏览器键盘/焦点/窄屏交互仍需专项验收；除 Project 状态外的其他活动来源、回访与财务不属于已交付客户纵切。
- 项目任务浏览器不再拉全完整集合，顶层每页 20 项、子任务每页 100 项；树/平铺切换、搜索及状态/优先级/类型/标签/排期筛选和分页已有自动化覆盖。ProjectSelect 同样只读取当前 20 条候选，但真实浏览器键盘/焦点/窄屏及 1,000/10,000 条项目下的搜索和翻页性能仍未专项验收；有界网络与 DOM 不能替代真实性能数据。Focus → Task → Project 的整数分钟传播，以及项目级 Focus 过滤、当前归属重分类、报告/历史 UI 与缓存失效已有自动化覆盖。
- 项目时间线、产出聚合和项目附件已有自动化覆盖；本轮另覆盖 nullable follow-up、来源 Project 继承/清除、完成条件、owner/person/manual reviewer、共享 Task 详情、缓存失效，以及 direct complete + submit/waiting_review + accept→automatic resolved/100% Go 金链。
- 真实浏览器/WebView 的键盘、焦点、深链返回、窄屏和 1,000/10,000 条项目/任务及 Inbox 长列表仍待专项；自动化金链不能替代这项人工验收。

## 相关代码/PRD 链接

- [PRD：项目管理](../opc-workspace-PRD.md#53-项目管理)
- [PRD：Project 数据表](../opc-workspace-PRD.md#主要数据表)
- [整体功能架构](../functional-architecture.md)
- [ProjectsPage.tsx](../../apps/web/src/pages/ProjectsPage.tsx)
- [ProjectDetailPage.tsx](../../apps/web/src/pages/ProjectDetailPage.tsx)
- [ProjectEventsSection.tsx](../../apps/web/src/components/ProjectEventsSection.tsx)
- [ProjectNotesSection.tsx](../../apps/web/src/components/ProjectNotesSection.tsx)
- [ProjectArtifactsSection.tsx](../../apps/web/src/components/ProjectArtifactsSection.tsx)
- [ProjectAttachmentsSection.tsx](../../apps/web/src/components/ProjectAttachmentsSection.tsx)
- [ProjectFocusSection.tsx](../../apps/web/src/components/ProjectFocusSection.tsx)
- [projects.go](../../services/sidecar/internal/api/projects.go)
- [project_notes.go](../../services/sidecar/internal/api/project_notes.go)
- [project_artifacts.go](../../services/sidecar/internal/api/project_artifacts.go)
- [Project Artifact follow-up/金链测试](../../services/sidecar/internal/api/project_artifacts_test.go)
- [project_attachments.go](../../services/sidecar/internal/api/project_attachments.go)
- [focus_history.go](../../services/sidecar/internal/api/focus_history.go)
- [ProjectFormModal.tsx](../../apps/web/src/components/ProjectFormModal.tsx)
- [ClientSelect.tsx](../../apps/web/src/components/ClientSelect.tsx)
- [ProjectSelect.tsx](../../apps/web/src/components/ProjectSelect.tsx)
- [NewTaskModal.tsx](../../apps/web/src/components/NewTaskModal.tsx)
- [InboxItemTasksSection.tsx](../../apps/web/src/components/InboxItemTasksSection.tsx)
- [InboxTaskOrchestrationModal.tsx](../../apps/web/src/components/InboxTaskOrchestrationModal.tsx)
- [项目 API](../../services/sidecar/internal/api/projects.go)
- [项目 API 测试](../../services/sidecar/internal/api/projects_test.go)
- [Project model](../../services/sidecar/internal/models/project.go)
- [schema v3 项目迁移](../../services/sidecar/internal/database/migrations/003_project_lifecycle.sql)
- [schema v4 幂等快照迁移](../../services/sidecar/internal/database/migrations/004_idempotency_snapshots.sql)
- [schema v5 聚合版本迁移](../../services/sidecar/internal/database/migrations/005_project_aggregate_versions.sql)
- [schema v6 任务事实迁移](../../services/sidecar/internal/database/migrations/006_task_facts.sql)
- [schema v21 项目笔记迁移](../../services/sidecar/internal/database/migrations/021_project_notes.sql)
- [schema v22 项目附件迁移](../../services/sidecar/internal/database/migrations/022_project_attachments.sql)
- [schema v23 Artifact 来源迁移](../../services/sidecar/internal/database/migrations/023_task_artifact_inbox_projection.sql)
- [schema v24 Task 阻塞来源迁移](../../services/sidecar/internal/database/migrations/024_task_blocked_inbox_projection.sql)
- [schema v25 Task 临期来源迁移](../../services/sidecar/internal/database/migrations/025_task_due_inbox_projection.sql)
- [schema v28 Project 完成来源迁移](../../services/sidecar/internal/database/migrations/028_project_completion_inbox_projection.sql)
- [schema v31 Project→Client Activity 来源约束](../../services/sidecar/internal/database/migrations/031_client_project_activity_projection.sql)
