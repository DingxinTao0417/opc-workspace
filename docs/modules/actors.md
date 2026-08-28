# Actor 与本地责任分派模块

> 实现基线：`HEAD 471f814`（2026-08-27）
>
> 版本边界：当前 Actor/Assignment 完全未实现。schema v6 已交付 Task 父子、完成标准和乐观并发版本，但不包含责任主体、验收策略或审计。v0.1 交付 owner/person/system 的人工责任记录；agent Actor、Adapter 和实际执行归入 v0.2。

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

当前状态为**未开始**。

- 数据库没有 `actors`、`task_assignments`、`agent_adapters`、`agent_runs` 或 `workflow_events`。
- Task 已有 `completion_criteria` 和 `version`，非状态编辑、三态状态更新和删除已要求 `If-Match`；仍没有 assignee/reviewer、`review_policy`、扩展状态或分派历史。
- 前端没有 Actor 设置页、负责人选择器、当前负责人、改派历史或停用功能。
- API 已支持任务基础三态、父子任务和事实层并发冲突；没有分派、改派、提交产出或验收接口。
- 当前所有任务隐含属于单用户，但数据库中没有可审计的 owner Assignment 历史。
- 没有本地 Agent Runtime、安全能力令牌、受控执行或崩溃恢复。

## 目标功能

### v0.1 Actor 管理

- 首次迁移幂等创建一个稳定 UUID 的 owner 和一个 system。
- 设置页可管理 person 的名称、备注、非敏感元数据和 active/inactive 状态。
- owner/system 不可删除；历史引用存在时 person 只能停用，内置 Actor 的状态变更由服务端受限管理。
- 客户联系人转为 person 必须显式确认，不默认复制全部客户资料。

### v0.1 Assignment

- 每条 Task 可有 `assignee` 和 `reviewer`；同一 Task、同一 role 同时仅一个活动 Assignment。
- 分派记录负责人、分派人、开始/结束时间和改派原因。
- 改派在一个事务中结束旧记录、创建新记录、递增 Task 版本并写 Workflow Event。
- person 任务由 owner 代记开始、阻塞、产出与完成；UI 明确提示不会通知对方。
- v0.1 reviewer 只能是 owner；system 仅用于明确的内部维护任务。

### 历史任务迁移

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

1. owner 在设置中创建 person，填写名称与备注。
2. 在任务详情或收件箱拆分面板选择该 person。
3. UI 显示“仅本机记录负责人，不会发送任务或授予访问权限”。
4. Sidecar 创建 Assignment 与 Workflow Event。
5. person 线下完成后，owner 以 person 为 `produced_by_actor_id`、owner 为 `recorded_by_actor_id` 录入产出。

### 改派

1. owner 选择新 Actor 并填写改派原因。
2. 请求携带 Task 当前版本。
3. Sidecar 原子结束旧 Assignment、创建新 Assignment并记录前后值。
4. 并发版本已变化时返回 `409`，前端刷新后由用户确认，不覆盖新分派。

### 停用 person

1. owner 查看该 person 的活动任务和历史引用。
2. 存在活动 Assignment 时先改派或明确处理；不得静默留下无效负责人。
3. Actor 标记为 inactive，历史记录继续显示原名称和类型。
4. 新分派选择器不再显示该 Actor，已有时间线不变化。

### 启动本地 Agent（v0.2）

1. owner 注册 Adapter 并执行健康、版本和能力检查。
2. 创建 agent Actor 并把 Task 分派给它。
3. owner 启动 Run；Sidecar 校验活动 Assignment、能力和授权路径。
4. Adapter 在受控资源内产出 Artifact，Run 进入 `succeeded`，Task 进入 `waiting_review`。
5. owner 接受或返工；返工重试创建新 Run。

## 数据/API/状态与事件

### v0.1 规划数据

- `actors`：`id`、`type`、`display_name`、`status`、`is_builtin`、可选 `adapter_id`、能力/元数据 JSON 和时间戳。
- `task_assignments`：Task/Actor、`assignee / reviewer`、分派人、生效/结束时间和原因。
- `task_artifacts`：Task、可选 Agent Run、存储信息、校验和、实际产出者、录入者和 follow-up 标记。
- `workflow_events`：聚合对象、动作、Actor、Assignment/Run、request ID、前后值和时间。
- Task 已有完成条件和 `version`；本纵切继续增加验收策略、扩展状态及与 Assignment/Artifact/Event 同事务的版本递增规则。

数据库约束至少包括：仅一个 owner；内置 Actor 不可删除；同一 Task/role 只有一个活动 Assignment；停用不级联删除历史。

### v0.1 规划 API

- `GET/POST /api/v1/actors`：查询 Actor；只允许创建 person。
- `GET/PATCH /api/v1/actors/:id`：详情、更新和停用。
- `GET/POST /api/v1/tasks/:id/assignments`：查询和首次分派。
- `POST /api/v1/tasks/:id/reassign`：原子改派。
- `POST /api/v1/assignments/:id/end`：结束活动分派。
- Task 的 submit-output/review/events API 消费 Actor 与 Assignment 上下文。

### v0.2 规划数据与 API

- `agent_adapters` 保存执行器注册、manifest、版本、健康状态与能力声明。
- `agent_runs` 保存任务、Assignment、agent Actor、重试链、状态、输入快照、输出/错误和时间。
- 提供 Adapter 注册/健康检查、Run 创建/查询/取消/重试 API；Agent Runtime 使用独立鉴权中间件或受控进程管道。

### 状态和事件

- Actor：`active / inactive`；停用是生命周期事实，不删除历史。
- Assignment：以 `unassigned_at IS NULL` 表示活动，不另设“任务完成”状态。
- Run：`queued → running → succeeded / failed / cancelled / interrupted`。
- Workflow Event 记录 actor 创建/更新/停用、assignment 创建/改派/结束、产出录入、Run 生命周期和验收/返工。

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

1. **T-18A 迁移与内置 Actor**：在 schema v6 Task 事实层上新增 actors/assignments/artifacts/events，幂等创建 owner/system，回填历史任务。
2. **T-18B person 管理**：设置页 CRUD（删除改为受约束停用）、校验、筛选和语义提示。
3. **T-18C Assignment**：任务负责人选择、初次分派、改派、结束、唯一活动约束和时间线。
4. **T-18D 受控工作流**：扩展 Task 状态、完成条件、提交产出、owner 验收/返工和乐观并发。
5. **T-11 消费**：收件箱拆分和分派复用 T-18 基础，不重复定义 Actor 或 Task 状态。
6. **T-19 v0.2 Agent**：Adapter ADR、注册/健康、能力令牌、Run、受控 Artifact、取消/重试和恢复。

## 验收标准

- 数据库中始终只有一个 owner；owner/system 初始化和历史回填可重复执行且不重复。
- owner/system 不可删除；person 停用保留所有 Assignment、Artifact 和 Event。
- 同一 Task、同一 role 同时只有一个活动 Assignment；并发改派拒绝旧写入。
- person UI 始终清楚说明不登录、不发送、不远程同步；联系人不自动转为 Actor。
- person 线下产出能区分实际产出者与 owner 录入者。
- 分派、改派、状态变化和审计在同一事务中完成，失败无部分记录。
- v0.1 没有可执行 agent 占位；未注册健康 Adapter 时 agent 选项禁用并解释原因。
- v0.2 Agent 不复用 WebView Token、不直连 SQLite、不拥有任意 Shell/路径；只有经平台验证后才宣称禁网。
- Agent 成功不自动完成 Task；owner 验收、返工、Run 重试和崩溃恢复历史完整。
- Actor/Assignment API 的加载、空、错误、重试、冲突和权限边界均有定向测试。

## 相关代码/PRD链接

- [PRD：本地 Actor 模型](../opc-workspace-PRD.md#本地-actor-模型)
- [PRD：本地 Actor 与任务分派实施计划](../opc-workspace-PRD.md#10418-t-18-本地-actor-与任务分派)
- [PRD：架构决策记录（含 ADR-002）](../opc-workspace-PRD.md#d-架构决策记录)
- [整体功能架构](../functional-architecture.md)
- [当前 Task model](../../services/sidecar/internal/models/task.go)
- [当前任务 API](../../services/sidecar/internal/api/tasks.go)
- [当前 API 路由](../../services/sidecar/internal/api/router.go)
- [初始数据库迁移](../../services/sidecar/internal/database/migrations/001_initial_schema.sql)
