# 项目管理模块

> 实现状态截止：2026-08-28（依据当前实现）
>
> 实现基线：app v0.1.0 / API v1 / SQLite schema v20。schema v12–v20 的 Inbox、Reminder、编排、设置、保存视图、客户活动/附件/person 关联迁移均不改 Project 表、聚合触发器或现有 Project API。
>
> 版本边界：项目资料、基础生命周期、任务聚合、Client 客户关联和追加式项目活动时间线已实现，模块仍为**部分完成**；Inbox 已可手工关联已有 Task，但项目产出/附件的来源投影、批量拆分和自动分诊尚未交付。

## 定位与边界

Project 是任务的上层业务组织单位，用于表达一项工作的目标、客户归属、生命周期、交付范围、时间和合同金额。

- Project 保存自己的业务状态，不把任务、收件箱或发票状态复制为项目状态。
- 项目进度从关联任务派生，不在项目表维护第二份可写百分比。
- 项目产出、阻塞、交付、验收和开票节点未来可以生成 Inbox Item，但 Inbox Item 只跟进下一步，不替代项目生命周期。
- 任务可不归属项目；项目可暂时不关联客户。
- 发票、收入、成本、客户回访等后续业务由各自模块管理，项目详情只聚合读取已落地的事实。

## 当前实现状态

当前状态为**部分完成**，已经具备可运行的项目资料、生命周期、追加式审计时间线、列表和详情纵切。

### 已实现

- SQLite schema v20 为当前基线：v3 增加 Project 生命周期版本，v4 为幂等记录增加请求摘要和响应快照，v5 用触发器在项目响应依赖的任务、发票和客户事实变化时递增 `projects.version`，v6 增加 Task 事实，v7 增加 Actor/Assignment 和通用 `workflow_events`，v8 重建 Task 六状态并为事件增加稳定命令顺序和不可变保护，v9 增加 Task Submission/Artifact 与受控文件，v10 增加 Client 聚合版本和 Project 客户关联反向传播 trigger，v11 让 completed Focus Session 经 Task `actual_minutes` 进入项目聚合；v12–v20 不改变 Project 表。项目时间线直接复用既有不可变事件事实，因此本次不新增 schema v21。
- Go Project model、路由、输入校验和集成测试已经存在。
- 项目 API 支持创建、列表、详情、非生命周期字段编辑，以及受约束的永久删除。
- 列表 API 支持分页、名称/描述搜索、状态和客户筛选、白名单排序；未指定状态时默认排除归档项目。
- 创建接口接受可选 `Idempotency-Key`：服务端对规范化请求计算 SHA-256，并保存首次响应快照；同键同请求即使资源后来已编辑或删除也重放首次响应，同键不同请求返回 `409 IDEMPOTENCY_CONFLICT`，旧版无快照记录则拒绝不安全重放。
- 编辑、状态流转和永久删除必须携带 `If-Match`；资源响应返回 `ETag`，旧版本写入返回 `409 VERSION_CONFLICT`。归档项目资料必须先恢复再编辑，否则 PATCH 返回 `409 PROJECT_ARCHIVED`。版本不仅覆盖项目资料和状态，也覆盖任务关联/状态/`actual_minutes`、发票关联/增删、客户名称/删除等会改变聚合响应或删除确认范围的事实。
- 状态只能通过 `start / pause / resume / complete / reopen / archive / restore` 命令流转。完成仍有未完成任务的项目需要显式确认，且不会修改任何任务状态。
- 归档会保存原状态，恢复时回到该状态；缺少历史归档来源的旧数据恢复到 `planning`。
- 永久删除只允许已归档项目，同时要求 `confirm=true` 和最新版本；删除会解除任务、发票的项目外键并返回解除数量，不会删除这些记录。
- 项目响应从关联任务实时派生总数、已完成数、进行中数、剩余数、完成百分比及 `actual_minutes` 合计，并返回客户名、发票数和当前允许操作。六状态上线后仍只有 `done` 计为已完成；cancelled 保留在总数和剩余口径中，项目语义未随 D1 改写。
- 项目页使用真实 API 展示卡片、服务端搜索、状态筛选和每页 12 条分页，并覆盖加载、空、错误和重试状态。
- 项目详情通过串行分页读取全部关联任务，支持资料编辑、从项目内新建并预选项目的任务、派生进度/工时、状态操作、归档恢复，以及带二次确认的永久删除；版本冲突会刷新详情事实。
- 项目新建/编辑已接真实 Client 选择器，可关联、改绑或解除客户；项目列表支持按 Client 筛选。普通项目列表默认排除归档项，Client 详情为读取完整关联历史显式使用 `include_archived=true`。
- 新建任务和任务详情已接项目选择器；任务列表、详情、编辑和状态更新响应关联项目时返回 `project_name`，任务行直接显示项目名称。
- 归档项目拒绝新建任务关联或把其他任务改入，返回 `409 PROJECT_ARCHIVED`；既有关联任务仍可编辑且保持原关联。归档转换进行中时，详情页的新建任务入口会禁用。
- 项目创建、资料编辑、`start/pause/resume/complete/reopen/archive/restore` 和永久删除会在同一数据库事务追加 `project_*` Workflow Event，记录内置 owner、请求 ID、时间及前后项目快照；事件写入失败时整个项目命令回滚，创建幂等重放不会重复追加事件。
- `GET /api/v1/projects/:id/events` 按 `created_at / command_seq / id` 稳定倒序分页，返回当前 Project `ETag` 和 `meta.project_version`。项目详情默认读取首批记录，展示状态变化或资料变更字段，并提供独立加载、空、错误重试和“加载更早”状态。

### 已知缺口

- 项目详情内的任务列表和任务表单项目选项会按每页 100 条串行拉取全部结果，避免静默截断，但该项目详情区仍没有可见分页、状态筛选、任务树或内嵌 Assignment/Submission 控件；大数据量下的请求次数与响应性能仍待验证。Task 标签、父子、并发版本、Assignment 与 manual Submission/Artifact 验收已在共享任务详情交付。
- 项目工时对任务表当前 `actual_minutes` 求和；schema v11 已让 completed Focus Session 通过精确秒数账本向 Task 追加完整分钟，再沿既有聚合触发器刷新项目工时。项目级 Session 历史和高级分析仍未实现。
- 没有项目产出、附件、人工笔记或发票明细；当前只返回发票计数，用于解释硬删除影响。已交付的项目时间线是系统写命令的不可变审计，不等同于可编辑的项目笔记。
- schema v9 已有 Task 活动时间线与 `task_artifacts`；Project 现在已有事件生产器和读取时间线，但仍没有项目级 Artifact/附件聚合入口或 Inbox Item 来源投影。
- 没有项目里程碑、真实收入/成本聚合或开票操作。

## 当前用户流程

### 创建并启动项目

1. owner 输入名称，可选 Client、日期、金额和颜色；Client 选项从真实分页 API 拉取，也可明确选择“不关联客户”。前端为同一次失败重试复用 `Idempotency-Key`，服务端保存规范化请求摘要和首次 `planning` 项目响应快照。
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
2. UI 展示将被解除关联的任务和发票数量，并要求二次确认。
3. Sidecar 再次校验 `If-Match`、`confirm=true` 和项目归档状态。
4. 删除命令先在同一事务追加 `project_deleted` 事件，再删除 Project；关联任务和发票保留但其 `project_id` 被置空。删除事件保留在业务导出中，项目详情与事件 API 随 Project 删除后均返回 404；永久删除本身不可恢复。

### 查看活动时间线

1. 项目详情加载首批 20 条项目写命令事件；它们只描述项目事实变化，不承载当前状态。
2. 每条记录显示行为、发生时间和 owner，并在生命周期变化时显示前后状态，在资料编辑时只列出发生变化的字段名称，不直接展开完整快照。
3. 超过一页时可按稳定倒序加载更早记录；读取失败只影响时间线区，可独立重试，不阻断任务、状态和资料操作。

## 数据、API 与状态机

### 当前数据

- 当前 schema v20 的 `projects` 字段仍为 `id, name, description, client_id, status, start_date, due_date, amount_minor, color, version, archived_from_status, created_at, updated_at`；`idempotency_keys` 另有 `request_hash`、`response_body` 和 `response_status`。v5 trigger 继续维护 Project 聚合版本，v8 在重建 Task 后恢复该机制；v10 补充 Project 关联变化对 Client 聚合版本的反向传播；v11 在 Focus 给 Task 新增完整分钟时沿用既有 `actual_minutes` 聚合触发器；v12–v20 不改变这些 Project 事实。
- 当前允许状态：`planning / in_progress / paused / completed / archived`。
- `version` 从 1 开始，每次资料编辑或状态流转递增；`archived_from_status` 只用于恢复归档前状态。
- 进度和工时不是项目表字段，而是查询时分别从任务状态和任务 `actual_minutes` 派生。
- 项目事件保存在通用 `workflow_events`，`aggregate_type='project'`、`aggregate_id=Project.id`；action 为 `project_created/project_updated/project_started/project_paused/project_resumed/project_completed/project_reopened/project_archived/project_restored/project_deleted`。事件追加后受既有数据库 trigger 保护，不能更新或删除。

### 当前 API

| 方法   | 路径                                | 当前行为                                                                                                                                                                                                          |
| ------ | ----------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| GET    | `/api/v1/projects`                  | `page` 默认 1，`page_size` 默认 50/最大 100；`q` 搜索名称/描述（最多 200 字符），`status/client_id` 筛选，`sort` 白名单排序；默认隐藏归档项目，`include_archived` 经布尔解析校验，true 时供需要完整关联历史的读取 |
| POST   | `/api/v1/projects`                  | 创建 `planning` 项目；可选 `Idempotency-Key` 保存规范化请求 SHA-256 与首次响应快照，同请求可稳定重放并拒绝不同请求复用                                                                                            |
| GET    | `/api/v1/projects/:id`              | 返回项目资料、派生任务汇总、发票数、允许操作和 `ETag`                                                                                                                                                             |
| GET    | `/api/v1/projects/:id/events`       | 项目活动时间线；`page` 默认 1，`page_size` 默认 20/最大 100，稳定倒序；返回 owner、request、前后快照、Project `ETag` 与 `meta.project_version`                                                                    |
| PATCH  | `/api/v1/projects/:id`              | 仅编辑非归档项目的资料字段；状态字段不能通过通用 PATCH 修改；缺少 `If-Match` 返回 428，旧版本或归档状态返回 409                                                                                                   |
| POST   | `/api/v1/projects/:id/transitions`  | 执行 `start/pause/resume/complete/reopen/archive/restore`；缺少 `If-Match` 返回 428，旧版本返回 409                                                                                                               |
| DELETE | `/api/v1/projects/:id?confirm=true` | 仅永久删除已归档项目；必须携带 `If-Match`，返回 `deleted_id/detached_tasks/detached_invoices`                                                                                                                     |
| GET    | `/api/v1/tasks?project_id=:id`      | 读取项目任务；任务资源读取与编辑/状态响应包含可选 `project_name`；归档项目不接受新的任务关联                                                                                                                      |

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

## 后续目标

### 客户与项目资料

- Client CRUD、客户选择/改绑/解除和项目列表客户筛选已交付；Client 停用保留关联，永久删除会把 Project 外键置空并使相关聚合版本失效。
- 后续只在真实需求出现时扩展客户标签、项目风险信号或更完整排序交互，不新增可写进度或复制客户活动事实。

### 任务、产出与后续工作

- 将任务页已交付的分页、筛选、标签、任务树、Assignment、完整生命周期和 Artifact 验收能力按项目范围复用到项目详情；事实和共享任务详情已由 Task/Actor 纵切交付，当前缺口是 Project 详情内的聚合入口与交互复用。
- 项目交付类 Task 提交 Artifact，或 owner 显式标记 `requires_followup` 时，才可按稳定事件键幂等生成 Inbox Item。
- owner 在收件箱把产出拆成修改、发布、客户确认等工单并分派给 owner/person；v0.2 才可选择健康且隔离已验证的本地 agent。
- 项目达到交付、验收或开票节点的自动事件必须等 Workflow Event、Inbox 规则和对应业务模块交付后再启用。

### 项目详情增强

- 追加式项目写命令活动时间线已交付；任务产出聚合、项目附件和可编辑笔记仍待开发。
- Focus Core 已把 completed Session 的新增完整分钟幂等累计到 Task；项目继续从 `actual_minutes` 派生工时，不新增第二份 Focus 汇总事实。项目级 Session 历史和高级分析仍待实现。
- 发票模块交付后显示关联发票；财务模块交付后显示真实收入/成本。

## 与其他模块协作

| 模块      | 当前与后续协作方式                                                                                                                                                                            |
| --------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 任务      | 当前已支持项目选择、项目名展示、项目任务列表、Task 标签/父子/版本、派生进度/工时，以及共享任务详情中的 D2 产出与验收；项目详情内的任务树和项目级产出聚合仍待接入。                            |
| 客户      | 已支持 Client CRUD、可选 `client_id`、客户名称聚合、表单选择/改绑/解除、项目列表客户筛选、Client 详情中的完整关联项目读取、人工活动时间线、受控附件和 person 显式关联；外部活动来源仍待实现。 |
| 收件箱    | 项目产出、阻塞、交付和后续工单的目标承接者；Project 自身审计事件已交付，但尚未投影为 Inbox 来源。                                                                                             |
| Actor     | 项目本身不分派；项目内可执行工作必须落为 Task，再通过已交付的任务详情 Assignment API/UI 分派。                                                                                                |
| 专注      | 已从 completed Focus Session 经 Task 精确秒数账本取得新增完整分钟，并沿既有 Task 聚合刷新项目工时；项目级 Session 历史和高级分析仍待实现。                                                    |
| 发票/财务 | 当前只聚合发票数量用于删除说明；发票详情、收入和成本属于后续版本。                                                                                                                            |
| 今日      | 可展示已有任务项目名；项目节点和风险聚合尚未接入。                                                                                                                                            |

整体协作关系参见[整体功能架构](../functional-architecture.md)。

## 分阶段实施

1. **项目事实与 API（已实现）**：当前 schema v20 保留 schema v3–v10 的 Project 结构与聚合 trigger，并接入 schema v11 Focus → Task 实际工时传播；schema v12–v20 的 Inbox、Reminder、编排、设置、保存视图和客户扩展迁移均不改变 Project 结构。Go model、CRUD、校验、分页/搜索/筛选、快照式创建幂等、覆盖聚合事实的乐观锁、状态流转、归档恢复和受约束硬删除均已实现。
2. **前端基础纵切（已实现）**：真实新建/编辑、卡片列表、详情、加载/空/错误/重试、状态操作、归档恢复和删除确认。
3. **任务与工时协作（部分实现）**：项目选择、串行分页拉全项目选项与项目任务、`project_name`、Task 事实版本、派生进度和 `actual_minutes` 已接通；Focus Core 已接入 Task 工时传播。任务页已有分页/筛选/标签/父子层级，但项目详情尚未复用这些交互；大数据量性能和项目级 Focus 历史仍待实现。
4. **客户协作（基础范围、人工活动/附件/person 关联已实现）**：Client CRUD、项目客户选择/改绑/解除、客户筛选、双向聚合版本传播、Client 人工活动时间线、受控附件和显式 contact 关联已接通；外部来源、回访和财务仍待实现。
5. **项目审计（已实现）**：Project 创建/编辑/全生命周期/删除与追加式 Workflow Event 同事务提交，幂等创建重放不重复写事件；分页 API 和详情时间线覆盖状态变化、资料字段变化、加载/空/错误/重试/更早记录。
6. **产出与 Inbox 协作（部分实现）**：Task Artifact、Task Workflow Event、人工分派、验收，以及 Inbox 手工关联已有 Task 已交付；项目级产出聚合、项目附件、Inbox 来源投影、批量拆分和来源事件去重仍待实现。
7. **后续业务增强**：v0.3 里程碑，v0.4 发票/财务；不阻塞基础项目管理纵切。

## 验收口径

### 当前基础纵切

- 创建、编辑、查询、分页、搜索、状态筛选、状态流转、归档和恢复均持久化且可重试。
- 名称、日期、金额、客户外键、版本和状态转换均由服务端校验。
- 项目进度和工时汇总与关联任务一致，且没有第二份可写进度。
- 完成项目不会隐式完成任务；归档/恢复保留关联；硬删除仅在归档和双重确认后解除关联。
- 旧 `If-Match` 写入被拒绝；同幂等键不同创建请求被拒绝。
- 任务/发票/客户聚合事实变化会使旧项目版本失效；归档项目不能接受新的任务关联，既有关联任务编辑不被误拒绝。
- 列表和详情具备加载、空、错误和重试状态。
- 项目创建、资料修改、状态流转和删除各追加一次不可变事件；事件失败回滚项目写入，创建幂等重放不重复事件。
- 时间线分页稳定，返回真实 owner、请求 ID、前后快照和 Project 版本；前端的加载、空、错误重试与“加载更早”不影响项目其他区域。

### 完整模块仍待验收

- Client 大数据量选择器/筛选性能和真实浏览器交互仍需专项验收；外部活动来源、回访与财务不属于已交付客户纵切。
- 超过 100 条项目任务与项目选项已避免截断；项目详情内的可见分页/筛选/任务树、大数据量性能和项目级 Focus 历史/分析仍待验收。Focus → Task → Project 的整数分钟传播已有自动化覆盖。
- 项目时间线和 Workflow Event 的事务、幂等重放与失败回滚已有自动化覆盖；产出、附件及 Inbox Item 来源投影的事务、去重和失败恢复仍待验收。
- 真实浏览器中的键盘、焦点、窄屏和返回定位回归。

## 相关代码/PRD 链接

- [PRD：项目管理](../opc-workspace-PRD.md#53-项目管理)
- [PRD：Project 数据表](../opc-workspace-PRD.md#主要数据表)
- [整体功能架构](../functional-architecture.md)
- [ProjectsPage.tsx](../../apps/web/src/pages/ProjectsPage.tsx)
- [ProjectDetailPage.tsx](../../apps/web/src/pages/ProjectDetailPage.tsx)
- [ProjectEventsSection.tsx](../../apps/web/src/components/ProjectEventsSection.tsx)
- [projects.go](../../services/sidecar/internal/api/projects.go)
- [ProjectFormModal.tsx](../../apps/web/src/components/ProjectFormModal.tsx)
- [NewTaskModal.tsx](../../apps/web/src/components/NewTaskModal.tsx)
- [项目 API](../../services/sidecar/internal/api/projects.go)
- [项目 API 测试](../../services/sidecar/internal/api/projects_test.go)
- [Project model](../../services/sidecar/internal/models/project.go)
- [schema v3 项目迁移](../../services/sidecar/internal/database/migrations/003_project_lifecycle.sql)
- [schema v4 幂等快照迁移](../../services/sidecar/internal/database/migrations/004_idempotency_snapshots.sql)
- [schema v5 聚合版本迁移](../../services/sidecar/internal/database/migrations/005_project_aggregate_versions.sql)
- [schema v6 任务事实迁移](../../services/sidecar/internal/database/migrations/006_task_facts.sql)
