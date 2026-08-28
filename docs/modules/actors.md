# Actor 与本地责任分派模块

> 实现基线：app v0.1.0 / API v1 / SQLite schema v7（2026-08-27）
>
> 版本边界：T-18A 迁移与内置 Actor、T-18B Actor API 与设置页 person 管理、T-18C Assignment API 与任务详情责任 UI 已交付。受控验收状态、`review_policy` 与 Artifact 仍未交付；agent Actor、Adapter 和实际执行归入 v0.2。

## 定位与边界

Actor 是本机工作流中的责任主体，用统一模型表达应用所有者、外部责任人、本地 Agent 和系统规则。Actor 解决“谁对这项 Task 负责”，但不引入在线身份系统。

| 类型 | 含义 | 第一阶段能力 |
|------|------|--------------|
| `owner` | 当前设备上的唯一应用所有者 | 创建、分派、代录、验收、返工、解决；固定一个 |
| `person` | 本地通讯录式外部责任人 | 可被分派；不登录、不接收、不直接操作应用 |
| `system` | 内置本地调度/规则主体 | 生成去重事件、派生进度和维护告警；不能代替 owner 验收 |
| `agent` | 绑定健康本地 Adapter 的执行器 | v0.2 才可运行；提交产出但不能自行验收 |

边界约束：

- Actor 不是用户账号、客户联系人或权限角色；当前不提供多人登录、云同步或远程任务领取。
- Client 联系人只有在 owner 显式创建/关联后才能成为 person Actor。
- Assignment 保存 Task 的责任历史，Actor 不保存任务状态，Task 也不复制负责人历史。
- owner/system 是内置记录，不可删除；有历史引用的 person/agent 只能停用。
- agent 只有在 Adapter 注册、健康和能力验证通过后才可显示为可执行选项。

## 当前实现状态

当前状态为**部分完成**。

- SQLite schema v7 已新增 `actors`、`task_assignments` 和 `workflow_events`。固定 owner UUID 为 `00000000-0000-5000-8000-000000000001`，system UUID 为 `00000000-0000-5000-8000-000000000002`；迁移重复执行不会重复创建内置主体、回填分派或迁移事件。
- 历史未完成 Task 已回填活动 owner assignee；历史已完成 Task 已回填结束的 owner Assignment，结束时间优先使用 `completed_at`，缺失时回退 `updated_at`。每条回填都有唯一 `migration_assignment_backfill` Workflow Event，并明确标记为迁移推定。
- 数据库已约束唯一 owner/system、内置主体不可删除、owner 仅展示名称可改、system 不可编辑、负责人/分派人必须 active、v0.1 reviewer 必须是 owner、同一 Task/role 仅一个活动 Assignment，以及活动分派存在时禁止停用 Actor。
- `GET/POST /api/v1/actors` 与 `GET/PATCH /api/v1/actors/:id` 已接通真实 SQLite。列表支持分页、type/status 筛选和白名单稳定排序；创建只允许 person；详情/创建/更新返回 `ETag`，更新要求 `If-Match`。
- person 创建支持可选 `Idempotency-Key`：同一规范化请求重放首次 `201` 快照，不重复写 `actor_created` 事件；同 key 异体请求返回冲突。Actor 更新、停用同事务递增版本并写前后值和 request ID。
- 设置页已有“人员与责任”模块，可查看 owner/system/person，新建和编辑 person、启用/停用 person、单独修改 owner 展示名称；包含加载、空、错误、重试、校验、版本冲突和本地责任语义提示。
- 当前没有 Actor 删除路由；system 没有编辑入口。person 有活动 Assignment 时 API 与数据库双重拒绝停用，UI 提示先改派。
- Assignment 查询、创建、改派和结束 API 已接真实 SQLite，任务详情可管理当前负责人/审核人并分页查看结束历史；创建、改派和结束都会递增 Task 版本并写责任事件。
- Task 仍为三态，也没有 `review_policy`、`task_artifacts`、受控提交/验收、通用事件查询或 Agent Runtime。

## 目标功能

### 已实现：v0.1 Actor 管理

- 首次迁移幂等创建一个固定 UUID 的 owner 和一个 system。
- 设置页可管理 person 的名称、备注、非敏感元数据和 active/inactive 状态；owner 只允许修改展示名称，system 只读。
- owner/system 不可删除；当前 API 不提供任何 Actor 删除路由。历史引用存在时 person 只能停用，内置 Actor 的字段由 API 与数据库 trigger 双重限制。
- 客户联系人转为 person 必须显式确认，不默认复制全部客户资料。

### 已实现：v0.1 Assignment

- 每条 Task 可有 `assignee` 和 `reviewer`；同一 Task、同一 role 同时仅一个活动 Assignment。
- 分派记录负责人、分派人、开始/结束时间和改派原因。
- 改派在一个事务中结束旧记录、创建新记录、递增 Task 版本并写 Workflow Event。
- person 只表达本机责任归属；现阶段由 owner 代记 Task 三态变化，阻塞、产出与验收等受控操作待 T-18D。UI 明确提示不会通知对方。
- v0.1 assignee 只允许 active owner/person，reviewer 只允许 active owner；当前 Assignment API 不接受 system 或 agent 作为 assignee。
- Task 转为 `done` 时在同一事务结束全部活动 Assignment；重新打开 Task 不恢复旧分派。显式结束 Assignment 不改变 Task 状态。

### 已实现：历史任务迁移

- 未完成历史 Task 回填活动 owner assignee。
- 已完成 Task 回填已结束 owner Assignment，结束时间优先使用 `completed_at`，缺失时使用 `updated_at`。
- 每条回填写 `migration_assignment_backfill` 事件，并标明是迁移推定，不伪造更细历史。
- 迁移重复执行不得重复创建内置 Actor、Assignment 或事件。

### v0.2 本地 Agent

- 注册本地 Adapter、声明能力白名单、执行文件和健康状态；凭据不得进入 manifest。
- 仅当 Task 当前活动 agent Assignment 与 Run 的 agent 一致时可启动执行。
- 每个 Run 获取短时、单次、能力受限的令牌；不得复用 WebView Bearer Token。
- Run 成功只表示产出成功，并将 Task 推进到 `waiting_review`；owner 接受后才完成。
- 取消、失败、中断和重试均保留独立记录；Sidecar 恢复时将遗留 running Run 标为 `interrupted`。

## 关键用户流程

### 创建 person 并分派任务

1. **已实现**：owner 在设置“人员与责任”中创建 person，填写名称、备注和可选非敏感 JSON 元数据。
2. **已实现**：UI 持续显示“仅本机记录负责人，不会发送任务或授予访问权限”的边界说明。
3. **已实现于任务详情**：选择 active owner/person 并由 Sidecar 创建 Assignment 与 Workflow Event；收件箱拆分面板仍待 T-11。
4. **待 T-18D**：person 线下完成后，owner 以 person 为 `produced_by_actor_id`、owner 为 `recorded_by_actor_id` 录入产出。

### 改派

1. owner 选择新 Actor 并填写改派原因。
2. 请求携带 Task 当前版本。
3. Sidecar 原子结束旧 Assignment、创建新 Assignment并记录前后值。
4. 并发版本已变化时返回 `409 VERSION_CONFLICT`；前端刷新 Assignment，保留选择和原因，再由用户决定载入最新记录或基于新版本确认，不自动覆盖。

### 停用 person

1. owner 在设置页编辑 person 并选择停用。
2. API 在事务中检查活动 Assignment，数据库 trigger 同时兜底；存在活动分派时返回 `409 ACTOR_HAS_ACTIVE_ASSIGNMENTS`，Actor 版本和事件均不变化。
3. 没有活动分派时，Actor 标记为 inactive、版本递增并写 `actor_deactivated` 事件；可再次编辑为 active。
4. Actor 记录及其既有 Assignment 引用继续保留，历史不会因停用而删除；Assignment 不保存名称快照，展示时读取 Actor 当前资料。任务详情的新分派选择器只列 active Actor。

### 启动本地 Agent（v0.2）

1. owner 注册 Adapter 并执行健康、版本和能力检查。
2. 创建 agent Actor 并把 Task 分派给它。
3. owner 启动 Run；Sidecar 校验活动 Assignment、能力和授权路径。
4. Adapter 在受控资源内产出 Artifact，Run 进入 `succeeded`，Task 进入 `waiting_review`。
5. owner 接受或返工；返工重试创建新 Run。

## 数据/API/状态与事件

### schema v7 已实现数据

| 表 | 当前字段与约束 |
|----|----------------|
| `actors` | `id`、`type`、`display_name`、`status`、`is_builtin`、`notes`、对象型 `metadata_json`、`version`、`created_at`、`updated_at`；名称 1–100 字符、备注最多 2000 字符、版本从 1 开始 |
| `task_assignments` | `id`、Task/Actor、`assignee / reviewer`、`assigned_by_actor_id`、`assigned_at / unassigned_at`、`reason`；`unassigned_at IS NULL` 表示活动，同 Task/role 仅一个活动记录；永久删除 Task 时按 `ON DELETE CASCADE` 一并删除其 Assignment，属于删除整个 Task 聚合 |
| `workflow_events` | `id`、`aggregate_type / aggregate_id`、`action`、可选 Actor/Assignment/Agent Run/request ID、前后 JSON 和时间；迁移回填事件按 Task 唯一；Assignment 被 Task 级联删除时事件的 `assignment_id` 按 `ON DELETE SET NULL` 处理 |

`actors.metadata_json` 必须是 JSON object；API 进一步限制规范化后最多 16 KiB、最多 6 层和 100 个 key，并拒绝可能承载密码、令牌、凭据、cookie、API key、private key 或 session ID 的 key。`agent_adapters`、`agent_runs`、`task_artifacts` 及 Task 扩展状态尚未建表；Artifact 的受控文件存储、校验和、软删除与验收事务归 T-18D/T-19，不能把缺表写成已实现。

### 已实现 Actor 与 Assignment API

| 方法与路径 | 当前行为 |
|------------|----------|
| `GET /api/v1/actors` | 默认每页 50、最大 100；支持 `type`、`status` 和 `type/display_name/status/created_at/updated_at` 白名单排序，默认 owner → person → system → agent 且以名称/ID 稳定排序 |
| `POST /api/v1/actors` | 只创建 person；接受名称、备注、非敏感 metadata 和可选 active/inactive，服务端固定非内置、version=1；可选 `Idempotency-Key`；返回 `201`、`ETag` 和首次资源快照，重放带 `Idempotency-Replayed: true` |
| `GET /api/v1/actors/:id` | UUID 校验、详情和 `ETag`；不存在返回 `ACTOR_NOT_FOUND` |
| `PATCH /api/v1/actors/:id` | 必须 `If-Match`；person 可改名称/备注/metadata/active/inactive，owner 只可改展示名称，system 与 v0.1 agent 不可编辑；更新与 Workflow Event 同事务 |
| `GET /api/v1/tasks/:id/assignments` | 返回当前 assignee/reviewer、分页结束历史、`meta.task_version` 和 Task `ETag`；历史支持 `role` 与 `assigned_at/-assigned_at` 排序 |
| `POST /api/v1/tasks/:id/assignments` | 首次创建 role 的活动分派；必须携带 Task `If-Match`，成功返回 `201`、新 Task `ETag`、Assignment 与 Task |
| `POST /api/v1/tasks/:id/reassign` | 原子结束当前 role、创建新 Assignment 并写 `assignment_reassigned`；必须提供 1–1000 字符原因和 Task `If-Match` |
| `POST /api/v1/assignments/:id/end` | 结束活动 Assignment 并写 `assignment_ended`；必须提供原因和所属 Task `If-Match`，不改变 Task 状态 |

Actor PATCH 与 Assignment 命令缺少 `If-Match` 返回 `428 VERSION_REQUIRED`，格式错误返回 `400 INVALID_VERSION`，旧版本返回 `409 VERSION_CONFLICT`。Assignment 三个命令都支持可选 `Idempotency-Key`：服务端保存包含预期 Task 版本的规范化请求摘要、首次响应和状态码；安全重放返回原快照并带 `Idempotency-Replayed: true`，不重复写事件，异体复用返回 `409 IDEMPOTENCY_CONFLICT`。Actor 和 Assignment 都没有 DELETE 路由。Task submit-output/review/events API 仍为规划。

Assignment 查询默认历史每页 50、最大 100；`role` 只过滤结束历史，当前两个 role 始终完整返回。任务详情按每页 20 条加载更早记录，区分迁移推定，并在 person 候选与当前卡片上解释本地责任语义。完成 Task 会以统一原因 `Task completed` 结束活动记录并逐条写事件，前端显示为“任务完成后自动结束”；重新打开不恢复旧分派。永久删除整个 Task 聚合时，Assignment 按外键级联删除，Workflow Event 保留且 `assignment_id` 置空，事件 JSON 快照继续用于解释历史。

### v0.2 规划数据与 API

- `agent_adapters` 保存执行器注册、manifest、版本、健康状态与能力声明。
- `agent_runs` 保存任务、Assignment、agent Actor、重试链、状态、输入快照、输出/错误和时间。
- 提供 Adapter 注册/健康检查、Run 创建/查询/取消/重试 API；Agent Runtime 使用独立鉴权中间件或受控进程管道。

### 状态和事件

- Actor：`active / inactive`；停用是生命周期事实，不删除历史。
- Assignment：以 `unassigned_at IS NULL` 表示活动，不另设“任务完成”状态。
- Run：`queued → running → succeeded / failed / cancelled / interrupted`。
- 当前 Workflow Event 已记录 actor 创建/更新/停用，以及 assignment 创建/改派/结束；产出录入、Run 生命周期和验收/返工事件随 T-18D/T-19 后续接入。

## 与其他模块协作

| 模块 | 协作方式 |
|------|----------|
| 任务 | Assignment 关联 Actor 与 Task；Task 状态保持唯一事实。 |
| 收件箱 | 拆分面板原子创建任务和 Assignment，详情展示负责人/改派历史。 |
| 客户 | 联系人仅经显式动作关联 person；Client 与 Actor 不互相冒充。 |
| 项目 | 项目不可直接分派，必须拆成 Task 后分派 Actor。 |
| 发票/回访 | 外发、付款确认等高风险动作只允许 owner；person/agent 可准备或被记录。 |
| 设置 | v0.1 承载 person 管理；v0.2 增加 Adapter、agent 能力与健康管理。 |
| 审计/备份 | Actor、Assignment、Artifact、Run 和事件全部纳入本地导出与恢复。 |

总体对象边界参见[整体功能架构](../functional-architecture.md)。

## 分阶段实施

1. **T-18A 迁移与内置 Actor（已完成）**：schema v7 新增 actors、assignments、events，幂等创建 owner/system 并回填历史任务；Artifact 延后到受控工作流纵切。
2. **T-18B person 管理（已完成）**：Actor API、设置页新建/编辑/停用、校验、筛选、乐观锁、幂等创建、审计、加载/空/错误/重试和本地责任语义提示。
3. **T-18C Assignment（已完成）**：任务负责人/审核人选择、初次分派、改派、结束、唯一活动约束、分页历史、任务版本并发、幂等命令、责任事件和完成任务自动结束。
4. **T-18D 受控工作流**：扩展 Task 状态、完成条件、提交产出、owner 验收/返工和乐观并发。
5. **T-11 消费**：收件箱拆分和分派复用 T-18 基础，不重复定义 Actor 或 Task 状态。
6. **T-19 v0.2 Agent**：Adapter ADR、注册/健康、能力令牌、Run、受控 Artifact、取消/重试和恢复。

## 验收标准

- **已验证**：数据库始终只有一个 owner/system；固定内置记录和历史回填可重复执行且不重复。
- **已验证**：owner/system 不可删除；owner 只有展示名称可改，system 不可编辑；活动 Assignment 阻止 person 停用。
- **已验证**：同一 Task、同一 role 同时只有一个活动 Assignment，负责人/分派人必须 active，reviewer 必须 owner；旧 Task 版本不能改派或结束，失败不产生部分写入。
- **已验证**：person UI 清楚说明不登录、不发送、不远程同步；联系人不会自动转为 Actor。
- **已验证**：Actor 创建、编辑、停用的成功事件与前后值可审计；幂等重放和失败停用不重复写事件。
- **待实现**：person 线下产出区分实际产出者与 owner 录入者；受控状态、Artifact 和验收事务。
- v0.1 没有可执行 agent 占位；未注册健康 Adapter 时 agent 选项禁用并解释原因。
- v0.2 Agent 不复用 WebView Token、不直连 SQLite、不拥有任意 Shell/路径；只有经平台验证后才宣称禁网。
- Agent 成功不自动完成 Task；owner 验收、返工、Run 重试和崩溃恢复历史完整。
- Actor 与 Assignment API，以及设置页和任务详情责任 UI，均已有加载、空、错误、重试、校验、幂等、冲突和权限边界的定向测试；前端全量 16 个测试文件、89 项测试通过，尚未宣称浏览器视觉验收完成。

## 相关代码/PRD链接

- [PRD：本地 Actor 模型](../opc-workspace-PRD.md#本地-actor-模型)
- [PRD：本地 Actor 与任务分派实施计划](../opc-workspace-PRD.md#10418-t-18-本地-actor-与任务分派)
- [PRD：架构决策记录（含 ADR-002）](../opc-workspace-PRD.md#d-架构决策记录)
- [整体功能架构](../functional-architecture.md)
- [schema v7 Actor 迁移](../../services/sidecar/internal/database/migrations/007_actor_assignments.sql)
- [Actor/Assignment/Event models](../../services/sidecar/internal/models/actor.go)
- [Actor API](../../services/sidecar/internal/api/actors.go)
- [Actor API 测试](../../services/sidecar/internal/api/actors_test.go)
- [Assignment API](../../services/sidecar/internal/api/assignments.go)
- [Assignment API 测试](../../services/sidecar/internal/api/assignments_test.go)
- [Actor 迁移测试](../../services/sidecar/internal/database/actor_migration_test.go)
- [设置页 Actor 管理](../../apps/web/src/components/ActorSettings.tsx)
- [设置页 Actor 测试](../../apps/web/src/components/ActorSettings.test.tsx)
- [任务详情 Assignment UI](../../apps/web/src/components/TaskAssignmentsSection.tsx)
- [任务详情 Assignment 测试](../../apps/web/src/components/TaskAssignmentsSection.test.tsx)
- [当前 Task model](../../services/sidecar/internal/models/task.go)
- [当前任务 API](../../services/sidecar/internal/api/tasks.go)
- [当前 API 路由](../../services/sidecar/internal/api/router.go)
