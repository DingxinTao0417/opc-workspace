# 任务管理模块

> 实现状态截止：2026-08-27
>
> 版本边界：任务事实层已在 SQLite schema v6 和 API v1 中交付；当前 schema v7 已增加 Actor/Assignment/Event 数据基础、历史 owner 回填和 Actor 管理，T-18C Assignment 操作/UI 也已交付。扩展状态、产出和验收仍为 v0.1 后续纵切，Agent 执行为 v0.2 规划。

## 定位与边界

Task 是系统中唯一的**可执行工单**，回答“具体要完成什么、当前执行到哪一步、怎样才算完成”。

- Inbox Item 说明“为什么需要处理”，不能替代 Task，也不能直接被分派。
- Assignment 保存“由谁负责”及改派历史；Task 不复制负责人事实。
- Agent Run 保存一次本地执行尝试；Run 成功不等于 Task 完成。
- Task Artifact 保存实际产出；Artifact 本身不决定验收是否通过。
- Project 是任务的组织上下文，Focus Session 是工时来源，Workflow Event 是追加式审计。

父子任务只表达真实的完成层级。项目产出产生的“修改、发布、客户确认”等下游工单默认由收件箱关联，不因来源关系自动成为原任务的子任务。

## 当前实现状态

当前状态为**部分完成**。任务事实、层级、标签、分页筛选、批量安全操作、计划组排序和责任分派已经形成可运行纵切；受控生命周期、产出验收和专注工时仍未接入。

### 已实现

- SQLite schema v6 为 `tasks` 增加 `kind`、`parent_task_id`、`completion_criteria` 和 `version`，并为 `tags` 增加 `version`；旧任务默认 `kind = work`、`completion_criteria = ''`、`version = 1`。
- 任务类型支持 `work / review / followup / reminder`；状态暂时仍为 `todo / in_progress / done`。
- 新建、详情和编辑已接入项目、父任务、完成标准、标签、计划日期、截止时间、预计时长等事实；读取返回 `project_name`、`parent_task_title`、标签和子任务完成数。
- 父任务约束由 API 校验和数据库递归 trigger 双重保护，禁止自引用和循环。删除父任务时子任务按外键规则解除父级，不删除子任务；父级、子级标题/状态/关系变化会递增受影响任务版本，使嵌套列表缓存失效。
- 标签提供分页/搜索/排序、幂等新建、编辑和确认删除 API；名称按大小写不敏感唯一，颜色为 `#RRGGBB`，单任务最多 20 个标签。标签重命名、改色或删除会递增所有受影响任务的版本。
- 任务列表 API 默认每页 50、最大 100，支持状态、优先级、类型、项目、精确计划日期、计划/截止日期范围、重复 `tag_id`、父任务、根任务、标题/描述关键词和白名单排序。多个标签采用“同时包含”语义；排序始终以 `id` 兜底，空日期/空手动顺序排在末尾。
- 列表的计数、任务行、标签水合在同一只读事务中读取，避免返回相互矛盾的版本和标签快照。
- 任务页使用服务端分页和防抖搜索；支持状态、优先级、类型、项目、标签和精确计划日期筛选。无筛选时分页展示根任务并按需展开子任务；筛选结果使用父任务面包屑，任务行展示标签和子任务进度。
- `POST /tasks` 使用规范化 v2 请求摘要，覆盖类型、父任务、完成标准和排序后的标签 ID；保留对旧 v1 请求摘要的安全兼容。同 key 同请求在资源编辑或删除后仍重放首次 `201` 快照，异体复用返回 `409 IDEMPOTENCY_CONFLICT`。
- `GET /tasks/:id` 与创建响应返回 `ETag`；任务非状态编辑、三态状态更新和删除都必须携带 `If-Match`。缺少版本返回 `428`，旧版本返回 `409 VERSION_CONFLICT`；详情弹窗保留草稿并允许载入最新版或基于最新版重试。
- 任务详情可查询当前 assignee/reviewer 与分页结束历史，并执行首次分派、改派和结束。Assignment 写入同样使用 Task `If-Match`，成功递增 Task 版本；冲突时保留选择和原因并要求用户刷新确认。
- Task 转为 `done` 时在同一事务结束全部活动 Assignment 并逐条写 `assignment_ended`；重新打开不会恢复旧记录。显式结束 Assignment 不改变 Task 状态。
- `PATCH /api/v1/tasks/batch` 在单事务内执行 1–100 条任务的移动项目、设置/清除计划日期、添加标签或移除标签。所有任务 ID 和 `expected_version` 先整体校验，任一任务不存在、版本过期、项目不可分配、标签不存在或超过 20 标签时全部回滚。
- `PUT /api/v1/tasks/reorder` 对一个完整 `planned_date` 组（包括无计划日期组）原子写入手动顺序或恢复默认顺序。请求必须提交组内完整 ID/版本集合，组成员或版本变化时返回冲突；手动顺序以 1000 为间隔保存。
- 任务页在选择一个精确计划日期、清除其他筛选并切换“手动顺序”后，可通过上移/下移持久调整该计划组，并可恢复默认排序。当前尚未实现拖拽。
- 项目关联继续遵守归档约束；创建、单项编辑和批量移动都不能把任务新关联到已归档项目，既有关联任务仍可编辑未变更字段。
- 任务响应嵌入的 `project_name` 也受版本保护：项目名称变化或项目硬删除会递增关联任务版本；删除项目时外键置空后，旧任务 `If-Match` 不再可写。

### 已知缺口

- 状态仍只有三态。列表按钮主要完成或恢复任务；`blocked / waiting_review / cancelled`、显式生命周期命令、返工原因和审计均未实现。
- `completion_criteria` 已保存和编辑；Assignment 操作和责任事件已实现，但 `review_policy`、Artifact、受控状态事件和通用任务事件查询与真正的验收流程尚未实现，因此完成标准目前只是事实字段，不会自动判定完成。
- 父任务只展示子任务统计，不会因子任务完成而自动推进；“所有子任务取消不自动完成”等规则要等扩展状态和受控生命周期上线后实现。
- 批量 API 只允许移动项目、改计划日期、加/删标签，不批量改状态或删除，避免在受控生命周期上线前形成绕过入口。
- 任务页实现了按钮式计划组排序，今日页尚未消费该纵切，仍没有真实日期分组、跨日期改期或拖拽回滚。
- 项目/父任务选择器会串行拉取最多每页 100 条的全部选项，没有选择器内搜索；大数据量性能仍需专项验证。
- 看板、任务依赖和绑定专注工时未实现；`actual_minutes` 字段与项目聚合已存在，但 Focus Session 尚不会自动累计。
- Task/Tag 创建幂等仍没有 Actor 作用域、过期清理或 Workflow Event 去重；这些随本地工作编排实现。

## 当前数据与约束

| 字段 | 当前契约 |
|------|----------|
| `kind` | `work / review / followup / reminder`，默认 `work` |
| `status` | `todo / in_progress / done`；扩展状态待开发 |
| `parent_task_id` | 可空，自外键 `ON DELETE SET NULL`，禁止自引用与循环 |
| `completion_criteria` | 最多 10000 字符，默认空字符串 |
| `tag_ids` / `tags` | 写入使用最多 20 个 UUID；读取返回按名称稳定排序的完整标签 |
| `version` | 从 1 开始；任务事实、状态、父子聚合或嵌入标签变化时递增 |
| `manual_order` | 可空；计划组手动排序使用 1000、2000……；改计划日期时清空 |
| `due_date` | 可空 RFC 3339 时间戳，服务端规范化为 UTC |
| `planned_date` | 可空 `YYYY-MM-DD` 纯日期 |

`title` 为 2–200 字符；描述与完成标准分别最多 10000 字符；预计时长不得为负数。通用 PATCH 不接受 `status` 或生命周期时间。

## 当前 API

| 方法 | 路径 | 当前行为 |
|------|------|----------|
| GET | `/api/v1/tasks` | 分页、搜索、筛选和稳定排序；可查询根任务或指定父任务 |
| POST | `/api/v1/tasks` | 创建完整任务事实；可选 `Idempotency-Key`；返回 `ETag` |
| GET | `/api/v1/tasks/:id` | 返回项目/父任务标题、标签、子任务统计和 `ETag` |
| PATCH | `/api/v1/tasks/:id` | `If-Match` 保护的非生命周期编辑 |
| PATCH | `/api/v1/tasks/:id/status` | `If-Match` 保护的三态兼容接口 |
| GET | `/api/v1/tasks/:id/assignments` | 当前 assignee/reviewer、分页结束历史、Task 版本与 `ETag` |
| POST | `/api/v1/tasks/:id/assignments` | Task `If-Match` 保护的首次分派；可选幂等键 |
| POST | `/api/v1/tasks/:id/reassign` | 原子结束旧分派、创建新分派并写事件；要求原因、Task `If-Match` 和可选幂等键 |
| POST | `/api/v1/assignments/:id/end` | 结束活动分派并保留历史；要求原因、所属 Task `If-Match` 和可选幂等键 |
| DELETE | `/api/v1/tasks/:id` | `If-Match` 保护；父任务删除时只解除子任务父级 |
| PATCH | `/api/v1/tasks/batch` | 原子 `set_project / set_planned_date / add_tags / remove_tags` |
| PUT | `/api/v1/tasks/reorder` | 原子保存或清除一个完整计划组的 `manual_order` |
| GET | `/api/v1/tags` | 分页、搜索和排序 |
| POST | `/api/v1/tags` | 幂等新建标签 |
| PATCH | `/api/v1/tags/:id` | `If-Match` 保护的名称/颜色编辑 |
| DELETE | `/api/v1/tags/:id?confirm=true` | `If-Match` + 明确确认，解除任务关联并失效任务版本 |

任务筛选参数包括 `page`、`page_size`、`q`、`status`、`priority`、`kind`、`project_id`、`planned_date`、`planned_from`、`planned_to`、`due_from`、`due_to`、重复 `tag_id`、`parent_task_id`、`root_only` 和 `sort`。排序字段为 `manual_order / priority / due_date / planned_date / created_at / updated_at / title / status / kind`，前缀 `-` 表示倒序。

## 当前 Assignment 与后续生命周期/产出

- 当前每个任务可有 assignee 和 reviewer；同一 role 同时只能有一个活动 Assignment。
- v0.1 已支持 active owner/person assignee 和 owner reviewer；person 仅是本地责任记录，由 owner 回填线下进度和产出。system/agent 当前不可作为 assignee。
- T-18D 将状态扩展为 `todo / in_progress / blocked / waiting_review / done / cancelled`，引入 `review_policy = none / manual`，并只允许通过 `start / block / unblock / complete / submit-output / accept / request_changes / cancel / reopen` 等显式命令改变生命周期。
- T-18D 将支持文本、受控本地文件、链接引用和结构化摘要，并区分产出者与录入者；这些能力当前尚未实现。
- v0.2 才支持健康的本地 agent Actor；每次执行形成独立 Agent Run，成功后进入 `waiting_review`。

## 关键用户流程

### 当前：组织与整理任务

1. 用户创建任务，选择类型、项目、父任务、标签、计划/截止时间和完成标准。
2. 任务页按服务端条件筛选和分页；无筛选时展开父子层级，筛选时显示父级面包屑。
3. 用户在详情修改事实；请求携带当前 `If-Match`，冲突时保留草稿并选择重新载入或重试。
4. 用户可选择多条任务原子改项目、计划日期或标签；任一版本失效时整批不写入。
5. 用户在精确计划日期组内调整顺序；服务端只接受该组完整且版本一致的集合。

### 后续：提交产出并验收

1. 人工负责人完成工作；person 的线下结果由 owner 代录。
2. `submit-output` 校验至少一个有效 Artifact 或结构化说明，任务进入 `waiting_review`。
3. owner 接受则进入 `done`，要求返工则保留历史并回到 `in_progress`。
4. 状态、分派、产出和审计在同一事务或可重放命令中保持一致。

## 与其他模块协作

| 模块 | 协作方式 |
|------|----------|
| 今日 | 已可复用任务的计划日期、分页筛选和计划组排序 API；今日页尚待真正接入。 |
| 项目 | 当前已支持项目选择、`project_name` 和项目任务聚合；归档项目拒绝新关联，任务聚合变化会递增项目版本。 |
| Actor | 当前由 Assignment 关联 Actor 与 Task；Task 状态保持唯一事实。 |
| 收件箱 | 后续创建/关联 Task，并从必需任务派生进度；不复制 Task 状态。 |
| 客户 | 任务通过 Project 间接获得客户上下文；客户联系人不会自动变成 Actor。 |
| 专注 | Focus Session 后续绑定 Task，停止时幂等累计 `actual_minutes`。 |
| 发票/回访 | 后续业务事件经收件箱生成待办任务，不直接改写任务状态。 |

整体依赖见[整体功能架构](../functional-architecture.md)。

## 分阶段实施

1. **任务基础事实（已交付）**：schema v6、类型、父子、完成标准、标签、版本/ETag、任务页服务端分页筛选、批量安全操作和计划组手动排序。
2. **Actor 与 Assignment（已交付）**：Actor 管理、历史 owner 回填、Assignment API/UI、任务版本并发、幂等责任命令和责任事件。
3. **受控状态（待交付）**：扩展状态、验收策略、Artifact、提交/验收/返工和通用 Workflow Event 时间线。
4. **收件箱集成**：支持原子拆分/关联、必需标记和由任务派生的收件箱解决规则。
5. **今日与专注集成**：今日页接入日期范围与排序 API；持久化 Focus Session 并累计工时。
6. **v0.2 增强**：任务看板、本地 Agent Run、取消/重试和返工闭环；任务依赖另行排期。

## 验收标准

### 当前任务事实层

- CRUD、分页、筛选、排序和搜索均在真实 Sidecar/SQLite 上工作，超过 100 条不静默丢失。
- 父任务不能指向自身或形成环；父任务删除后子任务保留且版本变化可见。
- 任一任务/标签旧 `If-Match` 写入返回冲突，不覆盖新数据。
- 标签大小写不敏感唯一；超过 20 个标签整次写入回滚。
- 批量操作任一任务不存在、版本过期或约束失败时不产生部分写入。
- 排序请求缺少完整计划组、成员变化或版本过期时拒绝写入；恢复默认后 `manual_order` 清空。
- 非生命周期 PATCH 无法写入 `status` 或完成时间。
- 归档 Project 不能接受新任务或改入任务，既有关联任务仍可编辑。
- 加载、空、错误、重试、冲突、删除确认和分页状态有真实 UI。

### 当前 Assignment

- 同一任务同一 role 只有一个活动 Assignment；并发旧版本的创建、改派或结束被拒绝。
- person 分派明确说明不发送、不登录、不同步；inactive person 不进入候选，停用后既有历史仍可查。
- 幂等重放不重复 Assignment 或责任事件；完成 Task 只递增一次 Task 版本并结束全部活动记录。
- Assignment 没有独立 DELETE。Task 永久删除会级联删除 Assignment，保留 `assignment_id` 已置空且含 JSON 快照的 Workflow Event。

### 后续工作流

- manual、Agent 和高风险任务无法绕过“提交产出 → owner 验收”。
- 并发验收拒绝旧写入，且受控状态不能绕过提交产出与 owner 验收。
- Artifact 文件只从应用受控目录读取，包含元数据和 SHA-256；软删除可审计。

## 相关代码/PRD 链接

- [PRD：任务管理](../opc-workspace-PRD.md#52-任务管理)
- [PRD：任务表与编排规划表](../opc-workspace-PRD.md#主要数据表)
- [整体功能架构](../functional-architecture.md)
- [schema v6 迁移](../../services/sidecar/internal/database/migrations/006_task_facts.sql)
- [任务 API](../../services/sidecar/internal/api/tasks.go)
- [Assignment API](../../services/sidecar/internal/api/assignments.go)
- [Assignment API 测试](../../services/sidecar/internal/api/assignments_test.go)
- [任务批量与排序 API](../../services/sidecar/internal/api/task_operations.go)
- [标签 API](../../services/sidecar/internal/api/tags.go)
- [当前 Task model](../../services/sidecar/internal/models/task.go)
- [TasksPage.tsx](../../apps/web/src/pages/TasksPage.tsx)
- [TaskList.tsx](../../apps/web/src/components/TaskList.tsx)
- [TaskDetailModal.tsx](../../apps/web/src/components/TaskDetailModal.tsx)
- [TaskAssignmentsSection.tsx](../../apps/web/src/components/TaskAssignmentsSection.tsx)
- [NewTaskModal.tsx](../../apps/web/src/components/NewTaskModal.tsx)
- [前端 API client](../../apps/web/src/api/client.ts)
