# Actor 与本地责任分派模块

> 实现基线：app v0.1.0 / API v1 / SQLite schema v29（2026-08-28）；Actor/D2 结构仍分别由 schema v7/v9 引入；v16 的 app_settings、v18 的客户活动、v19 的客户附件、v21 的项目笔记和 v22 的项目附件引用 active Actor，v20 新增 Client–person 显式关联及停用保护；v23–v29 不改变 Actor 契约。
>
> 版本边界：T-18A Actor/Event、T-18B person 管理、T-18C Assignment、T-18D D1 生命周期与 D2 Submission/Artifact 验收均已交付。`agent` 类型仍只是数据库边界；Adapter、Run、能力令牌和自动执行属于 v0.2。

导航：[文档中心](../README.md) · [整体功能架构](../functional-architecture.md) · [PRD v9.10](../opc-workspace-PRD.md) · [任务模块](tasks.md) · [本地 Agent](local-agents.md)

## 定位与边界

Actor 统一表达本地责任主体，而不是在线账号：

- `owner`：当前设备的唯一操作者，负责代录、审核、撤回和删除；固定 UUID `00000000-0000-5000-8000-000000000001`。
- `person`：本机责任记录，可表示客户联系人、外包者或协作者；没有登录、权限、消息或同步能力。
- `system`：迁移、规则和系统动作；固定 UUID `00000000-0000-5000-8000-000000000002`。
- `agent`：未来本地 Agent 身份；v0.1 不开放创建、编辑、分派或执行。

Assignment 保存某个 Task 在某段时间内的角色事实：

- `assignee`：当前负责人；v0.1 只允许 active owner/person。
- `reviewer`：验收人；v0.1 只允许 active owner。

这套模型不等于多人协作系统。person 不会收到任务，owner 代录 person 的线下产出也不会冒充 person 发起 API 请求。

## 当前实现状态

- schema v7 幂等创建唯一 owner/system，保护内置类型、固定 ID、唯一性和不可停用规则。
- 历史 Task 按完成状态回填 owner assignee Assignment，并写 system 的 `migration_assignment_backfill`，重复迁移不产生重复记录。
- Actor API 支持分页、筛选、person 幂等创建、详情、`ETag / If-Match` 更新与停用；设置页“人员与责任”使用真实本地数据。
- Assignment API/UI 支持当前 assignee/reviewer、分页结束历史、首次分派、改派和结束。
- Task start 要求 active assignee；complete/cancel/accept 在同一事务结束全部活动 Assignment；reopen 不恢复旧分派。
- manual 输出提交要求 active assignee 和 active owner reviewer。Artifact producer 由服务端取当前 assignee，recorder 固定 owner；Submission submitter、reviewer、withdrawer 和 Artifact deleter 也固定 owner。
- Workflow Event 已覆盖 Actor、Assignment、Task 生命周期、策略修改、输出提交、验收、返工、撤回、Artifact 删除和迁移回填，并带可空 Assignment/Submission/Artifact 关联。
- Client 详情可显式关联已有 active person，或在一个事务中新建 person 后关联。每个 Client 同时最多一个 active contact；解除保留不可变原因与操作者历史，active Client 关联会阻止 person 停用。

## Actor 归属语义

| 事实                      | 当前 Actor 来源                | 用户可指定吗                                     |
| ------------------------- | ------------------------------ | ------------------------------------------------ |
| Task assignee             | active owner/person Assignment | 通过受控 Assignment 命令指定                     |
| Task reviewer             | active owner Assignment        | 通过受控 Assignment 命令指定；仅 owner           |
| Submission `submitted_by` | 内置 owner                     | 否                                               |
| Artifact `produced_by`    | 提交瞬间的 active assignee     | 否；服务端派生                                   |
| Artifact `recorded_by`    | 内置 owner                     | 否                                               |
| Submission `reviewed_by`  | 内置 owner                     | 否                                               |
| Submission `withdrawn_by` | 内置 owner                     | 否                                               |
| Artifact `deleted_by`     | 内置 owner                     | 否                                               |
| v7/v9 迁移事件 actor      | 内置 system                    | 否                                               |
| Client contact person     | active person                  | owner 显式选择或确认新建，不从联系人字段自动推断 |
| Client link/unlink actor  | 内置 owner                     | 否                                               |

UI 应表达为“负责人产出 / 我代录”。`submitted_by` 与 `produced_by` 可以不同，这是刻意的审计设计，不是数据错误。

## 关键用户流程

### 创建 person 并分派

1. 在设置“人员与责任”新建 person。
2. 服务端只接受 `type=person`，规范化 metadata，并以可选幂等键保存首次 201 快照。
3. 打开 Task 详情，为 active owner/person 创建 assignee；manual Task 还应把 owner 设为 reviewer。
4. Assignment 和 Workflow Event 在同一事务提交，Task 版本递增。
5. 前端失效 Task、Assignment、Event、Project 和 Today 缓存。

### 提交 person 的线下产出

1. manual Task 处于 todo/in_progress，person 为 active assignee，owner 为 active reviewer。
2. owner 在任务详情填写摘要及 Artifact；客户端不发送 producer ID。
3. Sidecar 在同一事务把 person 写为 `produced_by_actor_id`，owner 写为 Submission submitter 和 Artifact recorder。
4. Task 进入 waiting_review，owner 随后接受或填写原因要求返工。
5. 接受结束活动 Assignment；返工保留当前责任并回到 in_progress。

### 改派或结束

- `reassign` 原子结束旧 Assignment、创建新 Assignment、写一个前后快照事件，并只递增一次 Task version。
- `end` 要求原因，只结束指定活动 Assignment，不直接改变 Task status。
- 所有命令都要求 Task `If-Match`，可选稳定 `Idempotency-Key`；冲突后刷新并由用户重新确认。

### 停用 person

1. 若 person 仍有活动 Assignment 或 active Client contact 关联，API 返回冲突，不递增 Actor version、不写事件。
2. 先通过 Task 详情改派/结束全部活动 Assignment，并在客户详情解除所有 active 联系人关联；解除必须填写原因。
3. 使用 Actor `If-Match` 把状态改为 inactive；历史 Assignment、Artifact producer 与 Event Actor 摘要继续保留。

### 关联客户联系人

1. owner 在客户详情选择已有 active person，或确认创建一个新的 person；`clients.contact_name` 只作表单预填，不触发自动转换。
2. Sidecar 使用 Client `If-Match`，在一个事务中完成可选 person 创建、`actor_created` Event、单 active contact 校验和关联写入。
3. 关联或解除递增 Client 版本，不递增已有 Actor 版本；解除原因和双方 Actor 快照进入不可变历史。
4. Client 永久删除级联关系历史但保留 person；关系只表达本地责任，不创建账号、消息或访问权限。

### 启动本地 Agent（v0.2，未实现）

未来必须先注册 Adapter、健康检查能力、创建 agent Actor 并使用独立 Run 鉴权。不得把 person 或 WebView 会话令牌复用为 Agent 身份，也不得把 D2 的手工 Artifact 提交误写成自动执行。

## 数据与 API

### schema v9 已实现数据

- `actors`：类型、展示名、状态、备注、受限 metadata、内置标记、version 与时间。
- `task_assignments`：Task、Actor、role、分派/结束 Actor、原因、时间与 active 唯一性。
- `workflow_events`：聚合、action、actor、assignment/submission/artifact、request、不可变快照、command_seq 与时间。
- `task_submissions`：submit/review/withdraw Actor 和批次状态。
- `task_artifacts`：producer/recorder/deleter Actor 和产出事实。

`actors.metadata_json` 必须是 JSON object；API 限制规范化后最多 16 KiB、最多 6 层和 100 个 key，并拒绝疑似密码、token、credential、cookie、API key、private key 或 session ID 的 key。

### 已实现 API

| 方法     | 路径                                          | 说明                                                                  |
| -------- | --------------------------------------------- | --------------------------------------------------------------------- |
| GET      | `/api/v1/actors`                              | 默认 50/最大 100；type/status 筛选和白名单排序                        |
| POST     | `/api/v1/actors`                              | 只创建 person；可选幂等键；返回 Actor ETag                            |
| GET      | `/api/v1/actors/:id`                          | 详情与 ETag                                                           |
| PATCH    | `/api/v1/actors/:id`                          | Actor `If-Match`；owner 仅名称，system/agent 禁止，person 可编辑/停用 |
| GET      | `/api/v1/tasks/:id/assignments`               | 当前角色与分页历史；返回 Task ETag/meta.task_version                  |
| POST     | `/api/v1/tasks/:id/assignments`               | Task `If-Match`；创建活动分派                                         |
| POST     | `/api/v1/tasks/:id/reassign`                  | Task `If-Match`；原因必填；原子改派                                   |
| POST     | `/api/v1/assignments/:id/end`                 | 所属 Task `If-Match`；原因必填                                        |
| POST     | `/api/v1/tasks/:id/submit-output`             | producer 从 assignee 派生，submitter/recorder 为 owner                |
| POST     | `/api/v1/tasks/:id/review`                    | owner 接受或要求返工                                                  |
| DELETE   | `/api/v1/artifacts/:id?confirm=true`          | owner 确认软删并记录原因                                              |
| GET/POST | `/api/v1/clients/:id/actor-links`             | 读取关系或显式关联已有/新建 person；写入使用 Client `If-Match`        |
| DELETE   | `/api/v1/client-actor-links/:id?confirm=true` | owner 填原因解除；保留不可变历史并使用 Client `If-Match`              |

Actor 和 Assignment 均没有 DELETE 路由。Task 聚合硬删除会级联 Assignment/Submission/Artifact；Workflow Event 关联 ID 可因外键置空，但 Actor 与 JSON 快照继续提供历史语义。

## 与其他模块协作

- [任务](tasks.md)：Assignment 是状态命令和 D2 提交的前置；Artifact 保存 producer/recorder。
- [收件箱](inbox.md)：单条已有 Task 关系只连接事实，不隐式创建 Assignment；T-11C 拆分命令已可为新建 Task 原子创建 owner/person 初始 Assignment 和 manual owner reviewer。
- [项目](projects.md)：Task 责任或产出变化通过 Task/Project cache 与版本关系呈现，不直接修改 Project 状态。
- [客户](clients.md)：Client contact 只能显式关联 active person；活动关系阻止停用，解除后历史仍可审计。
- [本地 Agent](local-agents.md)：未来 agent Actor 只表达身份，实际执行必须由 Adapter/Run 管理。
- [数据管理](data-management.md)：历史 Actor 引用与受控 Artifact 文件必须一起纳入未来备份/恢复。

## 分阶段实施

1. **T-18A（已完成）**：schema v7 Actor/Assignment/Event、内置主体、历史 Assignment 回填与保护。
2. **T-18B（已完成）**：Actor API、幂等/ETag、设置页 person 管理。
3. **T-18C（已完成）**：Assignment 查询、创建、改派、结束、Task 版本和责任 UI。
4. **T-18D D1（已完成）**：schema v8 六状态、显式命令、事件顺序/不可变保护与时间线。
5. **T-18D D2（已完成）**：schema v9、manual policy、Submission/Artifact、受控文件、提交/接受/返工/撤回/软删。
6. **Inbox/Reminder（部分实现）**：独立手工 Inbox Item、人工分诊、已有 Task 关系、一次性 Reminder，以及 T-11C Task 拆分/owner-person 分派/系统自动结清已交付；其他来源消费和 Agent 未实现。
7. **Client contact（已完成）**：schema v20 显式关联、原子新建 person、单 active contact、带原因解除、不可变历史和 person 停用保护。
8. **T-19 v0.2（未实现）**：agent Adapter、Run、能力令牌、取消/重试与崩溃恢复。

## 验收状态

- [x] 固定唯一 owner/system，可重复迁移且不重复回填。
- [x] person 创建、编辑、停用和敏感 metadata 拒绝有 API/UI/测试。
- [x] Assignment 创建/改派/结束与 Task 状态命令共享 Task 乐观锁和幂等快照。
- [x] manual 提交明确区分 producer 与 owner submitter/recorder。
- [x] owner reviewer 前置、接受/返工、取消撤回与终态 Assignment 联动均为事务化实现。
- [x] Workflow Event 关联 Assignment/Submission/Artifact 并保持追加式历史。
- [ ] agent Actor、Adapter/Run、能力令牌、权限撤销和实际本地执行。
- [x] 独立的手工 Inbox Item 与人工分诊，不隐式创建 Assignment。
- [x] Inbox 可关联/解除已有 Task，关系动作不隐式创建 Assignment。
- [x] 一次性 Reminder 的 owner 创建/取消与 system 到期触发审计。
- [x] Inbox Task 批量拆分、owner/person 初始分派与 system 自动结清/重开。
- [x] Client contact 显式关联/解除、原子新建 person、单 active 约束和停用保护。
- [ ] 非 Reminder Inbox 来源事件消费。

## 相关代码/PRD 链接

- [PRD Actor 与编排设计](../opc-workspace-PRD.md)
- [schema v7 Actor 迁移](../../services/sidecar/internal/database/migrations/007_actor_assignments.sql)
- [schema v8 生命周期迁移](../../services/sidecar/internal/database/migrations/008_task_workflow.sql)
- [schema v9 Submission/Artifact 迁移](../../services/sidecar/internal/database/migrations/009_task_submissions_artifacts.sql)
- [schema v20 Client–Actor 关联迁移](../../services/sidecar/internal/database/migrations/020_client_actor_links.sql)
- [Actor model](../../services/sidecar/internal/models/actor.go)
- [Artifact model](../../services/sidecar/internal/models/artifact.go)
- [Assignment API](../../services/sidecar/internal/api/assignments.go)
- [Task output API](../../services/sidecar/internal/api/task_outputs.go)
- [Client–Actor 关联 API](../../services/sidecar/internal/api/client_actor_links.go)
- [设置页 Actor UI](../../apps/web/src/components/ActorSettings.tsx)
- [任务详情责任 UI](../../apps/web/src/components/TaskAssignmentsSection.tsx)
- [任务详情产出 UI](../../apps/web/src/components/TaskOutputsSection.tsx)
