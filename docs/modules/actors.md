# Actor 与本地责任分派模块

H2-W9（2026-09-21）：AI 独立轨道现支持按 `task_ids` 精确批量读取 1–20 个已有 Task 的活动责任（单任务历史读取不变），并在一张 `task.batch_update` 卡中统一设置或结束负责人/审核人。每项冻结 Task 版本与 Actor 安全身份，确认重验资格、责任事实与预览，在原生 Assignment 事务中整批成功或回滚。原因必填，审核人仍仅 owner；person 不收到消息，agent 分派不启动 Run。无 schema/API 迁移，见 [AI 责任批量命令](ai-assistant.md#已有任务成组责任变更h2-w92026-09-21)。

> 实现基线：app v0.1.1 / API v1 / SQLite schema 78。Actor/D2 分别由 schema v7/v9 引入；v20 Client–person 关联、v35 回访停用保护继续有效。schema 071 为 agent 增加可空 `agent_adapter_id`，schema 074 在 Agent Run 中冻结 Actor/Assignment/Adapter 等执行身份，schema 075 增加 Run 交付状态，schema 076 增加 AI 会话计划；后续 schema 077/078 不改 Actor 表。
>
> 版本边界：T-18A–D 人工闭环已交付；T-19 v0.2 已有 Windows 内置 Agent 的 Adapter/Actor/Run 与条件式文本提交。未验证平台、external 执行器及其他执行能力仍关闭，详见 [ADR-027](../adr/027-builtin-agent-executor-and-run-lifecycle.md)。

导航：[文档中心](../README.md) · [整体功能架构](../functional-architecture.md) · [PRD](../opc-workspace-PRD.md) · [任务模块](tasks.md) · [本地 Agent](local-agents.md)

## 定位与边界

Actor 统一表达本地责任主体，而不是在线账号：

- `owner`：当前设备的唯一操作者，负责代录、审核、撤回和删除；固定 UUID `00000000-0000-5000-8000-000000000001`。
- `person`：本机责任记录，可表示客户联系人、外包者或协作者；没有登录、权限、消息或同步能力。
- `system`：迁移、规则和系统动作；固定 UUID `00000000-0000-5000-8000-000000000002`。
- `agent`：v0.2 的受控执行身份，仅由显式启用 Adapter 的服务端事务建立；普通 Actor POST/PATCH 不开放创建或编辑 agent。

Assignment 保存某个 Task 在某段时间内的角色事实：

- `assignee`：当前负责人；v0.1 人工路径为 active owner/person，v0.2 Agent 路径还要求真实 active agent 关联 enabled/healthy/execution_ready 的 Adapter。
- `reviewer`：验收人；v0.1 只允许 active owner。

这套模型不等于多人协作系统。person 不会收到任务，owner 代录 person 的线下产出也不会冒充 person 发起 API 请求。

## 当前实现状态

- schema v7 幂等创建唯一 owner/system，保护内置类型、固定 ID、唯一性和不可停用规则。
- 历史 Task 按完成状态回填 owner assignee Assignment，并写 system 的 `migration_assignment_backfill`，重复迁移不产生重复记录。
- Actor API 支持分页、筛选、person 幂等创建、详情、`ETag / If-Match` 更新与停用；设置页“人员与责任”使用真实本地数据。
- Actor 列表/详情返回只读可空 `agent_adapter_id`；非 agent 为 null。前端兼容旧响应缺失该字段为 null，但不能凭缺失关联或固定 ID 推断 Agent 已初始化。
- Assignment API/UI 支持当前 assignee/reviewer、分页结束历史、首次分派、改派和结束。
- 任务 Agent 入口先验证 Adapter 状态和真实 Actor 的 type/status/关联；无 assignee 时明示启动将分派并复用既有 Assignment hook，已有他人 assignee 必须先显式改派。页面读取、初始化缺失或启动失败均不自动启用 Adapter、补 Actor 或静默改派。
- Task start 要求 active assignee；complete/cancel/accept 在同一事务结束全部活动 Assignment；reopen 不恢复旧分派。
- 人工 manual 输出提交要求 active assignee 和 active owner reviewer。Artifact producer 由服务端取当前 assignee，recorder 固定 owner；人工 Submission submitter、reviewer、withdrawer 和 Artifact deleter 也固定 owner。Agent Run 条件式提交的身份语义见下表。
- Workflow Event 已覆盖 Actor、Assignment、Task 生命周期、策略修改、输出提交、验收、返工、撤回、Artifact 删除和迁移回填，并带可空 Assignment/Submission/Artifact 关联。
- Client 详情可显式关联已有 active person，或在一个事务中新建 person 后关联。每个 Client 同时最多一个 active contact；解除保留不可变原因与操作者历史，active Client 关联会阻止 person 停用。待回访 Client Followup 也会阻止其负责人停用，终态回访仅保留历史，不会阻止停用。

### AI 独立轨道的责任分派（H2-C2 / H2-D）

ADR-029 的 work 范围可通过 workspace_task_options 分页读取可分派 active owner/person 及就绪 agent 的 ID、名称、类型、版本；可选 role=reviewer 仅返回 active owner，tag 查询不能使用 role。不读取备注、metadata、联系方式、客户关联或 Adapter 配置。work+actions 的 inbox.split 保存整批拆分建议，经人工确认后才创建初始 Assignment；manual reviewer 固定 active owner。

H2-W8 的 `task.batch_create` 也可在每个项目任务草案中可选指定一个上述真实 `assignee_actor_id`，经一张完整确认卡原子创建任务和初始 Assignment；若该草案为 `manual` 且指定了负责人，同时由 active owner 担任 reviewer。预览冻结 Actor 身份名称/状态/版本，确认重验候选资格与 Adapter 就绪，任何失败整批回滚。未指定负责人不隐式分派或加审核人。该批次仍不启动 Agent Run、不联系 person；后续改派、执行各需独立权限和确认，见 [批量任务初始分派](ai-assistant.md#批量建任务的可选初始分派h2-w82026-09-21)。

H2-J 在显式 `work+clients+actions` 下复用同一 Actor 候选白名单来选择客户联系人，但服务端只接受 `type=person,status=active`；owner、agent 与 inactive person 即使出现在其他任务场景的候选中也不能关联。Client 安全详情只返回当前联系人 link/person 身份与 person 版本，不返回人员备注或 metadata。关联/解除逐项人工确认并复用原生事务；解除保留 person 和关系历史，不创建、删除、停用人员，也不发送消息。

H2-K 在保存会话的 `work+actions` 下增加 `workspace_search/get(person)` 和 `person.create/update`。读取只返回 person 的 ID、名称、状态、版本，不搜索或返回备注/metadata。创建固定为 active 本地 person；更新只接受名称、状态、备注，备注必须来自用户本轮明确输入。模型不能编辑 owner/system/agent、创建账号、删除人员或发送消息。每项在人工确认前零写入；确认重新读取版本和完整预览，并与 Actor HTTP API 共用创建/更新事务。停用继续由既有数据库与领域门禁阻止仍有活动 Assignment、active Client contact 或 planned Followup 的人员，不自动改派或解除关系。

H4-AH 将设置页“人员与责任”的已保存 person 行接入“交给智能体”：入口只暂存 person ID、当前显示名称/状态、`/ai?settings=actors` 和固定提示词，推荐 `work+actions`；不会复制备注、metadata、客户关系、任务责任、联系方式、会话或授权。进入 `/ai` 后仍需用户手动带入问题、重新授权并发送；模型必须先用 `workspace_get(type=person,id=...)` 或精确匹配的 `workspace_search(type=person)` 读取最新身份，才能起草待确认的 `person.create/update`。新建/编辑表单或写入中禁用交接，交接本身不创建、修改、停用、改派、解绑、取消回访、删除人员、联系人员或授予访问权限。

H2-D 新增 workspace_task_assignments：单项查询使用实际 task_id、可选 state=active/history（默认 active）、role、limit 1–20、offset 0–1000；只返回责任记录 ID、角色、当前 Actor 名称/类型/状态/版本、起止时间，以及同一读事务内的 Task 版本、状态、review_policy 和详情地址。历史不等于当前责任，不返回结束原因、操作者或人员私有资料。分页明确 has_more/next_offset/window_limited。H2-W9 另可只传 `task_ids`（1–20 个不重复的精确 UUID）及可选 role，返回同一快照的逐 Task 当前责任和版本；不能混用单项/历史/分页参数。

保存会话的 work+actions 可提出三种操作，均由人逐项确认：

- task.assign：task_id/expected_version；changes 必须为 role、真实 actor_id，只能首次分派空缺角色。
- task.reassign：同上，另须 changes.assignment_id（该任务/角色当前活动记录）与 1–1000 字符 reason，保留旧记录并新建责任。
- task.unassign：task_id/expected_version；changes 为 role、assignment_id、reason，不接受 actor_id，只结束指定活动责任。

预览绑定 Task 标题/版本、原责任 ID、双方 Actor 名称/类型/版本、当前验收策略/批次及子任务数量。确认重验当前角色、Actor/Adapter 可用性并比较完整预览，改名、停用或资料变化不能悄然替换确认目标。原生 Assignment API 与审批共用 assignment_commands.go；分派、Task 升版本、领域事件、父任务待验收协调及审批在同一事务提交，失败全部回滚，重复确认不重复分派。父任务可按既有规则发起/撤回待验收或重开，不自动接受产出。

主/侧对话复用确认卡与 Task 详情链接，确认或模糊失败回读后刷新任务聚合、分派、产出、事件及相关工作台缓存；下轮只回传审批/Task 结果身份，不自动续授权限。人员不可用、责任已变化、角色不允许及任务已结束提供具体提示，需重新读取后提议，不自动改派或把分派失败误报为任务创建失败。person 不收消息；责任分派动作本身只分派，不启动 Run、不启用 Adapter。H5-A 已提供独立的 `agent_execution` 单次授权与第二次人工确认链路；它复用当前 active agent Assignment，并将 Actor/Assignment/Adapter 版本冻结进 schema 074 的 Run，不能把一次分派确认当成执行确认。

源码证据：[共享分派事务](../../services/sidecar/internal/api/assignment_commands.go)、[AI 读取/提议](../../services/sidecar/internal/api/ai_assignment_actions.go)、[隔离验收](../../services/sidecar/internal/api/ai_assignment_actions_test.go)、[前端契约](../../apps/web/src/api/aiAssignmentActions.ts)。确定性测试不代替真实模型/桌面验收。

## Actor 归属语义

| 事实                      | 当前 Actor 来源                               | 用户可指定吗                                     |
| ------------------------- | --------------------------------------------- | ------------------------------------------------ |
| Task assignee             | active owner/person 或就绪 agent Assignment   | 通过受控 Assignment 命令指定                     |
| Task reviewer             | active owner Assignment                       | 通过受控 Assignment 命令指定；仅 owner           |
| Submission `submitted_by` | 人工提交为 owner；Agent Run 提交为 agent      | 否；由对应受控提交路径派生                       |
| Artifact `produced_by`    | 提交瞬间的 active assignee                    | 否；服务端派生                                   |
| Artifact `recorded_by`    | 人工代录为 owner；Agent Run 自动提交为 system | 否                                               |
| Submission `reviewed_by`  | 内置 owner                                    | 否                                               |
| Submission `withdrawn_by` | 内置 owner                                    | 否                                               |
| Artifact `deleted_by`     | 内置 owner                                    | 否                                               |
| v7/v9 迁移事件 actor      | 内置 system                                   | 否                                               |
| Client contact person     | active person                                 | owner 显式选择或确认新建，不从联系人字段自动推断 |
| Client link/unlink actor  | 内置 owner                                    | 否                                               |

人工提交 UI 表达为“负责人产出 / 我代录”。Agent Run 条件式提交另记录 agent producer/submitter 与 system recorder；任务仍由 owner 验收。`submitted_by` 与 `produced_by` 可以不同，不允许客户端伪造任意身份。

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

1. 若 person 仍有活动 Assignment、active Client contact 关联或 `planned` Client Followup，API 返回冲突，不递增 Actor version、不写事件。
2. 先通过 Task 详情改派/结束全部活动 Assignment，在客户详情解除所有 active 联系人关联，并改派、完成、跳过或取消全部待回访计划；解除和终态命令均保留原因或结果。
3. 使用 Actor `If-Match` 把状态改为 inactive；历史 Assignment、Artifact producer 与 Event Actor 摘要继续保留。

### 关联客户联系人

1. owner 在客户详情选择已有 active person，或确认创建一个新的 person；`clients.contact_name` 只作表单预填，不触发自动转换。
2. Sidecar 使用 Client `If-Match`，在一个事务中完成可选 person 创建、`actor_created` Event、单 active contact 校验和关联写入。
3. 关联或解除递增 Client 版本，不递增已有 Actor 版本；解除原因和双方 Actor 快照进入不可变历史。
4. Client 永久删除级联关系历史但保留 person；关系只表达本地责任，不创建账号、消息或访问权限。

### 启动本地 Agent（v0.2，Windows 内置路径）

1. 用户在“设置 → 本地 Agent”显式登记、检查并启用 Adapter；只有受平台矩阵支持、健康和就绪时才能成功启用。
2. 启用事务幂等建立关联的 active agent Actor；版本、Actor 类型/状态/关联冲突不得覆盖，失败回滚 Adapter 更新。固定 ID 冲突、Actor 停用/关联错误或已启用但缺 Actor 返回 `409 AGENT_ADAPTER_ACTOR_CONFLICT`。
3. 任务页刷新真实 Actor、Adapter、Assignment、Provider 和 Run；缺登记、未启用、缺 Actor、关联不符或读取失败时禁用启动并用文字指明设置路径。
4. 仅 todo/in_progress 任务允许启动/重试。无负责人时，启动前明示将分派给 Agent；通过已有 Assignment `If-Match`/hook 及读取返回的 `task_version` 完成。已有其他负责人时先由用户显式改派，不能靠捕获启动错误偷偷重试分派；重试不补分派。
5. Run 使用单次进程管道，不复用 person 或 WebView 会话令牌。符合 manual-review 条件时文本进入 waiting_review，owner 验收后才完成；否则产出留在 Run 记录。

## 数据与 API

### 已实现数据（当前 schema 76；Actor 表自 schema 71 未变）

- `actors`：类型、展示名、状态、备注、受限 metadata、内置标记、version 与时间；schema 071 另有只读可空 `agent_adapter_id`。schema 074 不改 Actor 表，只把创建 Run 时观察到的 Actor/Assignment/Adapter 版本冻结在 `agent_runs`。
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
- [本地 Agent](local-agents.md)：agent Actor 只表达身份；初始化、启停和真实执行由 Adapter/Run 管理，不以固定 Actor ID 或 UI 占位代替健康门控。
- [数据管理](data-management.md)：历史 Actor 引用与受控 Artifact 文件必须一起纳入未来备份/恢复。

## 分阶段实施

1. **T-18A（已完成）**：schema v7 Actor/Assignment/Event、内置主体、历史 Assignment 回填与保护。
2. **T-18B（已完成）**：Actor API、幂等/ETag、设置页 person 管理。
3. **T-18C（已完成）**：Assignment 查询、创建、改派、结束、Task 版本和责任 UI。
4. **T-18D D1（已完成）**：schema v8 六状态、显式命令、事件顺序/不可变保护与时间线。
5. **T-18D D2（已完成）**：schema v9、manual policy、Submission/Artifact、受控文件、提交/接受/返工/撤回/软删。
6. **Inbox/Reminder（当前人工闭环已实现）**：独立手工 Inbox Item、人工分诊、已有 Task 关系、一次性 Reminder、T-11C Task 拆分/owner-person 分派/系统自动结清，以及已登记的 follow-up Artifact、Task 阻塞/临期、Project 完成和系统维护来源均已交付；未来 Client/Invoice/里程碑来源与 Agent 仍未实现。
7. **Client contact（已完成）**：schema v20 显式关联、原子新建 person、单 active contact、带原因解除、不可变历史和 person 停用保护。
8. **T-19 v0.2（部分交付）**：Windows builtin Adapter/Actor/Run、管道能力、文本提交及初始化门控；跨平台、external 与 Agent 专属 Inbox 投影仍待。

## 验收状态

- [x] 固定唯一 owner/system，可重复迁移且不重复回填。
- [x] person 创建、编辑、停用和敏感 metadata 拒绝有 API/UI/测试。
- [x] Assignment 创建/改派/结束与 Task 状态命令共享 Task 乐观锁和幂等快照。
- [x] manual 提交明确区分 producer 与 owner submitter/recorder。
- [x] owner reviewer 前置、接受/返工、取消撤回与终态 Assignment 联动均为事务化实现。
- [x] Workflow Event 关联 Assignment/Submission/Artifact 并保持追加式历史。
- [x] Windows builtin Adapter 显式启用建立 agent Actor，Run 和文本提交已有代码；平台证据与限制见 ADR-027。
- [x] 本轮初始化回归覆盖 Actor 关联字段兼容、缺失/不一致禁发、启用回滚与版本冲突、不静默改派；通过隔离 API/组件测试，不视为真实模型测试。
- [x] 独立的手工 Inbox Item 与人工分诊，不隐式创建 Assignment。
- [x] Inbox 可关联/解除已有 Task，关系动作不隐式创建 Assignment。
- [x] 一次性 Reminder 的 owner 创建/取消与 system 到期触发审计。
- [x] Inbox Task 批量拆分、owner/person 初始分派与 system 自动结清/重开。
- [x] Client contact 显式关联/解除、原子新建 person、单 active 约束，以及活动分派/联系人/待回访计划的停用保护。
- [x] 已登记的非 Reminder Inbox 来源事件消费：follow-up Artifact、Task 阻塞/临期、Project 完成和系统维护事件。
- [ ] 未来 Client/Invoice/其他里程碑来源与 v0.2 Agent 事件消费。

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
