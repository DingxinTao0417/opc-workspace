# 任务管理模块

> 实现基线：app v0.1.0 / API v1 / SQLite schema v35（2026-08-29）；Task D2 结构仍由 schema v9 引入，schema v11 通过 Focus 精确秒数账本向 `actual_minutes` 追加完整分钟；schema v23–v25 分别为显式 follow-up Artifact、Task 阻塞与 Task 临期增加 Inbox 来源投影和删除协调 guards。schema v30 给 Submission 增加来源并交付父任务自动发起验收；schema v31 只约束 Project→Client Activity 来源，schema v32 只扩展 Reminder，schema v33 新增受限 Automation Rule/Run，schema v34 新增 Agent Adapter 诊断事实，schema v35 新增 Client Followup，均不改变 Task 表、API 或既有 manual 提交。v9.17 的 ProjectSelect 复用既有 Project API，不改变 app/API/schema 版本，也不新增迁移。
>
> 版本边界：任务事实层、Actor/Assignment、T-18D D1/D2、Focus 工时回写、Inbox Task 关系/拆分编排、一次性 Reminder、六状态看板与跨列受控生命周期、共享服务端 Project 选择、显式 follow-up Artifact/Task 阻塞/Task 临期→Inbox，以及有门禁的父任务自动发起验收已交付。自动建 Reminder 和本地 Agent Run 属于后续纵切。

导航：[文档中心](../README.md) · [整体功能架构](../functional-architecture.md) · [PRD v9.44](../opc-workspace-PRD.md) · [Actor 与分派](actors.md) · [数据管理](data-management.md)

## 定位与边界

Task 是 opc-workspace 唯一可执行工单。项目、未来 Inbox、提醒和 Agent 都只能关联或驱动 Task，不能另建一套互不一致的完成状态。

当前模块负责：

- Task 事实、父子关系、标签、Project 关联、计划与完成标准；
- assignee/reviewer 的当前责任与历史 Assignment；
- 六状态受控生命周期和追加式 Workflow Event；
- `none` 直接完成与 `manual` 提交验收两种策略；
- 直属子任务完成度派生、父任务自动进入待验收及失效后的撤回/重开；
- Submission 批次、四种 Artifact、受控文件下载、审计软删除及 Task 聚合硬删除；
- 所有真实页面的加载、空数据、错误、重试、版本冲突和草稿保留。

Task 不拥有 Inbox 的分诊/解决事实、Agent Runtime、远程协作/通知、自动生成产出、AI 分析或知识库。Inbox 拆分复用 Task 创建约束；Task 生命周期、提交与验收命令调用 Inbox reconciliation。schema v23 在产出提交事务内消费显式 follow-up 标记，schema v24 在 block 生命周期事务内消费本次阻塞事实；schema v25 的本地扫描器消费当前 Task 截止时间事实。Inbox 仍拥有来源、关系和结清策略。父子层级不会创建 Inbox–Task 关系、不会继承或改写 `is_required`；Inbox 自动结清继续只看用户显式标为 required 的活动关系，与父任务自动验收彼此独立。

## 已实现状态

- Task 新建、详情、非生命周期编辑、确认删除、服务端分页/筛选/搜索/排序、原子批量事实/生命周期操作和计划组排序已接通真实 SQLite；任务页支持项目当前客户、精确计划日期、计划/截止日期范围、最多 20 个持久保存视图，且在单一精确日期的手动顺序列表中支持同状态拖拽。客户条件使用与 Project 表单/筛选共享的 `ClientSelect`。Project 关联使用 Task 新建/编辑、Tasks 项目筛选、批量目标项目和 Inbox 拆分任务共用的 `ProjectSelect`：两个选择器都固定每页 20 条、250 ms 服务端搜索、稳定分页、取消信号、当前选择保留和完整反馈，不再串行拉取全部 Client 或 Project。列表 API 另提供仅供实时截止风险入口使用的 `due_state=overdue|due_soon`；Today 以该条件读取完整分页结果。六状态看板复用同一服务端筛选、当前页分页、最多 100 项选择和详情入口，空列保持可见；跨列拖拽只映射既有生命周期命令并二次确认，不直接修改状态。统一搜索结果使用 `/tasks/:taskId`，刷新可恢复同一详情，不存在资源保留明确错误反馈。
- `todo / in_progress / blocked / waiting_review / done / cancelled` 六状态只通过显式命令变化；旧状态 PATCH 固定返回 410。
- `review_policy` 可在新建时选择 `none / manual`；既有 Task 只在 `todo` 且没有任何 Submission 历史时允许切换。
- Assignment 支持活动 assignee/reviewer、首次分派、改派、结束与分页历史。assignee 只允许 active owner/person，reviewer 只允许 active owner。
- manual Task 可从 `todo / in_progress` 提交摘要及最多 20 个文本、链接、结构化 JSON 或文件 Artifact；混合文件与非文件提交使用 multipart manifest。
- Task 详情“产出与验收”模块展示 manual 前置条件、草稿、当前批次、接受/返工、分页历史、Artifact 详情、文件安全下载和确认软删除。
- `none` 策略明确提示无需产出验收，可使用直接完成命令；manual 缺 assignee 或 owner reviewer 时显示具体前置条件，不提交无效请求。
- 所有 Task 写命令使用 Task `ETag / If-Match`。输出、审核、删除、Assignment、生命周期和事实编辑在 UI 上互斥，避免同时对旧版本写入。
- 输出冲突会刷新最新 Task，但保留摘要、文本、链接、结构化 JSON 和原浏览器 `File` 对象；用户查看最新事实后必须再次明确提交。取消未保存设置/草稿不会静默写服务端。
- 成功写入会立即失效 Task、Submission、Artifact、Assignment、Event、Project 和 Today 相关 Query，避免等待缓存自然过期。
- Task 时间线已覆盖策略变化、提交、接受、返工、等待验收时撤回、Artifact 删除及 v9 迁移回填文案。
- 绑定 Task 的 Focus Session 只有 stop→completed 才把精确秒数写入 `task_focus_totals`，再把新增完整分钟追加到 `actual_minutes`；余秒跨 Session 保留，cancel/interrupted 不入账，也不改变 Task 生命周期。
- Task 详情可按需分页读取该任务的终态 Focus Session；completed 显示为已计入工时，cancelled/interrupted 仅作审计展示，读取或翻页不会修改 Task 草稿、版本或生命周期。
- Task 存在 active/paused/recovery_pending Focus Session 时，硬删除返回 `409 TASK_HAS_OPEN_FOCUS_SESSION`；Session 进入终态后可删除 Task，历史 Session 的 `task_id` 自动置空。
- Task 存在任一活动 Inbox 关系时，硬删除返回 `409 TASK_HAS_ACTIVE_INBOX_RELATIONS`，不会移动 Artifact 文件或删除聚合；用户带原因软解除后才可删除。已解除历史关系的实时 `task_id` 随删除置空，但原 Task UUID/标题快照继续保留。
- 父任务只统计直属子任务。只要至少有 1 个非取消直属子任务且它们全部为 done，并且父任务是 todo/in_progress、策略为 manual、存在 active owner/person assignee 和 active 内置 owner reviewer，系统才会创建无 Artifact 的 `child_rollup` Submission，并把父任务最多推进到 waiting_review；它不会自动 accept 或结束 Assignment。
- manual Submission 历史、既有 pending Submission，或曾被 `request_changes` 的 `child_rollup` 会阻止系统覆盖或重复提交。自动门禁或子任务完成条件失效时，pending 系统批次撤回；已经被 owner 接受的父任务若随后因直属子任务失效而不再满足条件，则系统重开父任务并沿祖先链协调。

## 数据模型与约束

### TaskSavedView（schema v17）

`task_saved_views` 独立保存筛选定义，不把视图塞入固定四模块 `app_settings`，也不复制任务结果。名称 1–80 字符、大小写不敏感唯一；定义 JSON 最大 16 KiB、`schema_version = 1`、`version >= 1`，当前每个工作区最多 20 个。定义只包含搜索、状态、优先级、类型、项目、客户、标签、精确/范围计划日期、截止范围和排序；不保存页码、当前选择、展开状态或查询结果。

API 提供列表、新建、`If-Match` 更新和带 `confirm=true` 的删除。服务端按与 Task 列表一致的枚举、UUID、日期、范围和排序白名单规范化；计划精确值与计划范围互斥。保存视图引用的 Project、Client 或 Tag 删除后不级联删除视图，应用时按当前事实自然返回空或剩余结果，避免用历史快照伪造现存关系。Project 候选继续消费 `GET /api/v1/projects`：默认排除归档项，所有排序追加 `id ASC`，列表总数与当页数据在同一只读事务读取；这只是既有 API v1 的稳定性收口。

### Task

| 字段                         | 当前约束                                                                 |
| ---------------------------- | ------------------------------------------------------------------------ |
| `status`                     | `todo / in_progress / blocked / waiting_review / done / cancelled`       |
| `review_policy`              | `none / manual`；策略改变只允许 `todo` 且 Submission 历史为 0            |
| `current_submission_id`      | 指向同一 Task 的最新 Submission；接受、返工、取消后保留，reopen 清空     |
| `submitted_at / reviewed_at` | 当前/最近一次提交流程的快速状态时间；reopen 清空，历史以 Submission 为准 |
| `blocked_from_status`        | block 时由服务端保存，unblock 只能恢复这个状态                           |
| `version`                    | 任一影响 Task 聚合呈现或决策的写入递增，用作 ETag 和乐观锁               |

Task 创建仍只允许 `todo`；非 `todo` 创建返回 `LIFECYCLE_COMMAND_REQUIRED`。状态不能经通用 PATCH 修改。

### TaskSubmission

| 字段                                                 | 含义                                                                                    |
| ---------------------------------------------------- | --------------------------------------------------------------------------------------- |
| `id / task_id / sequence`                            | UUID、所属 Task、Task 内从 1 递增且唯一的批次序号                                       |
| `status`                                             | `pending_review / accepted / changes_requested / withdrawn`；每个 Task 至多一条 pending |
| `origin`                                             | `manual / child_rollup`；既有和用户提交固定 manual，系统父任务汇总固定 child_rollup     |
| `summary`                                            | 可为空但最长 10,000 字符；manual 提交时 summary 与 Artifact 至少一个存在                |
| `submitted_by_actor_id / submitted_at`               | manual 固定内置 owner 代录；child_rollup 固定内置 system；同时保存提交时间              |
| `reviewed_by_actor_id / reviewed_at / review_reason` | 接受或返工时由内置 owner 记录；返工原因必填                                             |
| `withdrawn_by_actor_id / withdrawn_at`               | 用户取消 manual 待审时为内置 owner；自动批次失效撤回时为内置 system                     |
| `is_inferred`                                        | schema v9 从无歧义旧 manual 状态回填的批次为 true；child_rollup 必须为 false            |

Submission 的 Task、序号、来源、摘要、提交人、提交时间和 inferred 标记不可修改；只有 `pending_review` 可一次性转为 accepted、changes_requested 或 withdrawn。`child_rollup` 必须由内置 system 创建、`is_inferred = 0` 且 Artifact 数为 0；Task 仍存在时禁止直接硬删 Submission。

### TaskArtifact

| 字段                              | 含义与约束                                                                                                                               |
| --------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| `position`                        | 批次内从 1 开始且唯一，保持客户端提交顺序                                                                                                |
| `submission_status`（API 派生）   | 必填 `pending_review / accepted / changes_requested / withdrawn`，由父 Submission JOIN 得出；不是第二份可写状态                          |
| `storage_kind`                    | `text / link / structured / file`                                                                                                        |
| payload                           | text→`content_text`，link→`reference_url`，structured→`structured_json`，file→受控 `relative_path = objects/<artifact-id>`；四者严格互斥 |
| `name`                            | trim 后 1–255 个安全字符，不允许控制字符                                                                                                 |
| `mime_type / size_bytes / sha256` | 仅文件需要；SHA-256 为 64 位小写十六进制                                                                                                 |
| `requires_followup`               | 人工标记需要后续动作；true 时同事务投影一个稳定去重 Inbox Item，不自动生成 Task                                                          |
| `produced_by_actor_id`            | 提交瞬间的活动 assignee，由服务端派生，客户端不能指定                                                                                    |
| `recorded_by_actor_id`            | 固定内置 owner，表达“我代录”                                                                                                             |
| `integrity_status`                | `unverified / verified / missing / mismatch`；unverified 的检查时间必须为空，其他状态必须有检查时间                                      |
| `deleted_*`                       | owner 软删除时间、操作人和 1–1,000 字符原因，三者同为空或同为非空                                                                        |

Artifact 的事实与 payload 创建后不可编辑；仅完整性检查状态和首次软删除元数据允许受控变化。Task 仍存在时禁止直接硬删 Artifact；硬删除整个 Task 聚合时由外键级联清理数据库成员。

schema v9 的 `artifact_deletion_tombstones` 保存 file Artifact ID、Task ID、固定相对路径、size、SHA-256、删除范围 `artifact / task` 与时间。单 Artifact 软删或 Task 聚合硬删会在同一事务写入；记录不可修改或删除，也不引用将被级联删除的 Task/Artifact，因此启动恢复能区分已授权删除与未知候选。

### WorkflowEvent

事件是追加式事实，包含 Task 聚合、actor、可空 assignment/submission/artifact 关联、request ID、前后 JSON 快照、`command_seq` 和创建时间。关联对象随 Task 聚合硬删除时外键 ID 可安全置空，快照继续保留；其余更新/删除由 trigger 拒绝。

D2 新增事件：

- `task_review_policy_changed`
- `task_output_submitted`
- `task_review_accepted`
- `task_changes_requested`
- `task_submission_withdrawn`
- `task_artifact_deleted`
- `task_parent_review_requested`（system 创建 child_rollup 并发起验收）
- `task_parent_review_withdrawn`（system 因子任务或父任务门禁失效撤回）
- `task_parent_reopened`（已接受父任务因子任务条件失效而由 system 重开）
- `migration_submission_backfill`（system Actor，schema v9 inferred 历史）

## 状态机

```text
todo ──start──> in_progress
  │                 │
  ├──── block ──────┼──> blocked ──unblock──> 服务端记录的来源状态
  │                 │
  ├─ complete ──────┴──> done                 （仅 policy none）
  │
  └─ submit-output ─────> waiting_review      （仅 policy manual）
                             ├─ accept ──────> done
                             ├─ request_changes -> in_progress
                             ├─ block/unblock -> blocked/waiting_review
                             └─ cancel ──────> cancelled + Submission withdrawn

todo / in_progress / blocked / waiting_review ──cancel──> cancelled
done / cancelled ──reopen──> todo

直属非取消子任务至少 1 个且全部 done + 父任务 manual/责任门禁
  ── system child_rollup ──> waiting_review ── owner accept ──> done
                  │                    │
                  └─ 门禁/子任务失效 ──┴─> withdrawn + in_progress
accepted parent done + 子任务失效 ── system reopen ──> todo
```

规则：

- start 需要 active assignee。
- block/cancel 原因必填；unblock 不能由客户端选择目标状态。
- 每次成功 block 都按阻塞后的 Task version 生成独立 `task:<task-id>:blocked:<version>` Inbox 来源；幂等重放不重复投影，unblock 不删除或自动解决该人工待处理项。
- `none` 可从 todo/in_progress 直接 complete；manual 必须提交并经 owner accept。
- submit-output 只允许 manual 的 todo/in_progress，且同时需要 active assignee 与 active owner reviewer。
- accept 在同一事务完成 Task 并结束所有活动 Assignment；request_changes 保留 Assignment 并返回 in_progress。
- cancel 结束活动 Assignment；若当前是 waiting_review（包括从该状态 block 后），还会先把 pending Submission 撤回。
- accept、request_changes、cancel 保留 `current_submission_id` 作为最近批次指针；reopen 清空指针和快速时间字段，但保留 Submission、Artifact 与 Event 历史，也不恢复旧 Assignment。
- 自动父任务判定只看 `parent_task_id` 直接指向该父任务的子任务；cancelled 不进入分母，非取消数必须大于 0，且每一项都必须 done。孙任务只会先影响自己的直接父任务，再通过祖先协调逐层传播，不能绕过中间父任务。
- 自动发起还要求父任务当前为 todo/in_progress、`review_policy = manual`、active assignee 为 owner/person、active reviewer 为 builtin owner；系统只创建固定摘要的无 Artifact `origin=child_rollup` Submission 并进入 waiting_review，最终仍由 owner review。
- 任何 manual Submission 历史都阻止 child_rollup；系统也不覆盖现有 pending Submission，且 owner 对 child_rollup 要求返工后，该 `changes_requested` 历史阻止自动重提。manual 和 changes_requested 均保留人的明确决策优先级。
- pending child_rollup 的子任务条件或 review policy/Assignment 门禁失效时由 system 撤回。普通父任务回到 in_progress；若父任务正 blocked 且 `blocked_from_status=waiting_review`，状态和阻塞原因/时间保持 blocked，只把 `blocked_from_status` 更新为 in_progress。
- child_rollup 已 accepted 且父任务 done 后，如直属子任务条件失效，system 把父任务重开为 todo，清理当前快速提交/审核指针和终态事实，但保留 accepted Submission/Event；旧 Assignment 不自动恢复。系统沿祖先链继续协调。

## API 契约

### Task 与生命周期

| 方法   | 路径                                                                  | 关键约束                                                                                |
| ------ | --------------------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| POST   | `/api/v1/tasks`                                                       | 仅 todo；支持 `review_policy`; 可选稳定幂等键                                           |
| GET    | `/api/v1/tasks/:id`                                                   | 完整 Task、关系、版本和 `ETag`                                                          |
| PATCH  | `/api/v1/tasks/:id`                                                   | `If-Match`；不写 status；策略变化仅 todo+无历史                                         |
| DELETE | `/api/v1/tasks/:id`                                                   | `If-Match`；先拒绝开放 Focus/活动 Inbox 关系或来源，再硬删 Task 聚合并协调来源/文件清理 |
| PATCH  | `/api/v1/tasks/batch`                                                 | 1–100 项、逐项 expected version；事实或六种生命周期命令同事务，任一失败整批回滚         |
| POST   | `/api/v1/tasks/:id/{start\|block\|unblock\|complete\|cancel\|reopen}` | `If-Match`；可选稳定幂等键；显式状态机                                                  |
| GET    | `/api/v1/tasks/:id/events`                                            | 默认 50/最大 100；返回 Task ETag 与 `meta.task_version`                                 |

批量生命周期使用与单任务命令相同的转换矩阵和领域副作用。服务端先读取并校验完整选择集的版本、状态、active assignee 和 review policy，确认全部可执行后才开始写入；complete/cancel 会结束活动 Assignment，waiting-review cancel 会撤回当前 Submission，block 会创建对应 Inbox 来源，每个 Task 都追加独立 Workflow Event。创建、改绑/解除父任务、删除，以及单条/批量 complete/cancel/reopen 和 review accept 都会在同一事务协调受影响的父任务与祖先；start/unblock 还会重算任务自身，使迁移前已经满足子任务条件但未回填的父任务可在后续明确写命令中进入待验收。批量会去重父任务并返回协调后的最终 version。阻塞/取消使用一份 1–1,000 字符统一原因，前端对六种生命周期命令均要求二次确认。批量 API 依靠 expected version 保证重试可判定，不提供单任务命令的 Idempotency-Key 重放响应。

### 提交

`POST /api/v1/tasks/:id/submit-output` 要求 Task `If-Match`，可选 `Idempotency-Key`。

无文件时发送严格 JSON：

```json
{
  "summary": "交付说明",
  "artifacts": [
    {
      "client_ref": "note-1",
      "storage_kind": "text",
      "name": "结论",
      "content_text": "正文",
      "requires_followup": false
    },
    {
      "client_ref": "link-1",
      "storage_kind": "link",
      "name": "参考链接",
      "reference_url": "https://example.com/result",
      "requires_followup": true
    },
    {
      "client_ref": "json-1",
      "storage_kind": "structured",
      "name": "结构化结果",
      "structured_json": { "outcome": "ok" },
      "requires_followup": false
    }
  ]
}
```

有文件时发送 multipart：

- 正好一个文本字段 `manifest`，内容是同一 JSON 契约且必须作为首个 part；
- 此后只允许 manifest 中 file 项通过唯一 `file_field` 精确引用的同名文件 part；
- 可以在同一 manifest 中混合 text/link/structured/file；
- 未被引用、重复引用、一个字段多个文件或额外文本字段均拒绝。

服务端不接受 `produced_by_actor_id`。成功返回 `{data:{task,submission,artifacts,event}}` 并附新版 Task `ETag`；该用户入口创建的 Submission 固定 `origin=manual`。

本批次每个 `requires_followup=true` Artifact 都在同一 SQLite 事务中生成一个 `kind=event / source_entity_type=task_artifact` Inbox Item，并追加 system `source_projected` 事件；稳定键为 `task-artifact:<artifact-id>:followup`。快照只含来源导航/解释字段，不含正文或文件。提交或投影任一步失败时 Submission、Artifact、Task 状态、Inbox、事件和幂等快照全部回滚；同一提交幂等重放不重复投影。未标记 Artifact 不创建 Inbox Item。

提交限制：summary 最长 10,000 字符；最多 20 Artifact；`client_ref` 1–100 且同批唯一；文本最多 500,000 字符；HTTP(S) 链接最多 4,096 bytes、必须有 host 且不能含 userinfo；structured 必须是 JSON object 且编码后最多 1 MiB；严格 JSON body 与 multipart `manifest` 各最多 1 MiB；单文件非空且最多 50 MiB；完整 multipart 请求最多 100 MiB。Sidecar HTTP read/write timeout 为 180 秒；前端对提交和文件下载使用 120 秒端到端超时，并在发起 multipart 前估算 manifest 与文件总量，避免明知超过服务端边界仍上传。

### 验收

`POST /api/v1/tasks/:id/review` 要求 Task `If-Match`、可选稳定幂等键，body 为：

```json
{ "decision": "accept", "reason": "可选说明" }
```

或：

```json
{ "decision": "request_changes", "reason": "必须说明返工原因" }
```

reason 最长 1,000 字符。只有 manual policy + waiting_review + current pending Submission + active owner reviewer 可审核；这里的 Submission 来源可以是 manual 或 child_rollup。成功返回 `{data:{task,submission,event}}`。owner 接受 child_rollup 后父任务才变 done；要求返工会保留 changes_requested 系统批次并禁止自动覆盖。

### 历史、详情与下载

| 方法 | 路径                                                                           | 说明                                                                                      |
| ---- | ------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------- |
| GET  | `/api/v1/tasks/:id/submissions?page=&page_size=`                               | sequence DESC；每条带 `origin`、Actor 摘要、Artifact 摘要和总数；包含已软删 Artifact 摘要 |
| GET  | `/api/v1/tasks/:id/artifacts?page=&page_size=&submission_id=&include_deleted=` | 默认隐藏软删；按批次倒序、position/id 正序                                                |
| GET  | `/api/v1/artifacts/:id`                                                        | 返回元数据及按类型的正文；软删详情仍 200，但 payload 全为 null                            |
| GET  | `/api/v1/artifacts/:id/content`                                                | 仅 file；鉴权后校验大小和 SHA-256，再作为 attachment 下载                                 |

两个列表默认 `page=1 / page_size=50`、最大 100，返回 `{data, meta:{page,page_size,total,task_version}}` 和 Task `ETag`。所有 Artifact 摘要和详情都必须带由父 Submission 派生的 `submission_status`；摘要不暴露正文或 `relative_path`。前端依据该必填状态禁用 pending-review 删除，但服务端仍执行最终授权校验。

下载成功设置 Content-Type、Content-Length、安全 UTF-8 Content-Disposition、`X-Content-Type-Options: nosniff`、`Cache-Control: no-store` 和 SHA-256 ETag。非 file 返回 `ARTIFACT_CONTENT_UNAVAILABLE`；已删/缺失为 410；大小或哈希不符为 `ARTIFACT_INTEGRITY_MISMATCH` 并禁止输出内容。

前端对已冻结的 D2 错误码提供中文反馈：策略/提交/审核前置分别覆盖 `TASK_MANUAL_REVIEW_REQUIRED`、`TASK_ASSIGNEE_REQUIRED`、`TASK_REVIEWER_REQUIRED`、`TASK_SUBMISSION_NOT_ALLOWED`、`TASK_SUBMISSION_ALREADY_PENDING` 和 `TASK_REVIEW_NOT_ALLOWED`；Artifact 状态覆盖 `ARTIFACT_PENDING_REVIEW`、`ARTIFACT_ALREADY_DELETED`、`ARTIFACT_DELETED`、`ARTIFACT_FILE_MISSING` 与 `ARTIFACT_INTEGRITY_MISMATCH`。未识别错误仍显示服务端 message 和可重试边界，不吞掉 `request_id`。

### 软删除与硬删除

`DELETE /api/v1/artifacts/:id?confirm=true` 要求 Task `If-Match`、可选稳定幂等键和 `{ "reason": "1–1000 字符" }`。pending-review 批次禁止删除。非文件只写软删除元数据；file 删除在同一事务写不可变 tombstone，存在的文件先移入 `.trash/`，事务失败恢复，成功提交后清除 trash 文件。若物理文件已经缺失，确认软删除仍成功，并将完整性记录为 `missing` 及检查时间。列表可用 `include_deleted=true` 审计，详情隐藏已删 payload，文件下载返回 410。

若 Artifact 是尚处于 `open/tracking` 的 follow-up Inbox 来源，删除返回 `409 ARTIFACT_HAS_ACTIVE_INBOX_SOURCE`；用户必须先解决或忽略来源项。随后删除在同一事务先写 Inbox `source_deleted_at`、递增 Inbox 版本并追加 `source_deleted` 事件，再软删 Artifact；任何后续文件、数据库或事件失败都会整体回滚。

Task DELETE 是聚合硬删除：服务端先确认没有开放 Focus Session、`unlinked_at IS NULL` 的活动 Inbox 关系，也没有 `open/tracking` 的 Artifact 来源 Inbox；命中时分别返回 `TASK_HAS_OPEN_FOCUS_SESSION`、`TASK_HAS_ACTIVE_INBOX_RELATIONS` 或 `TASK_HAS_ACTIVE_INBOX_SOURCES`，且不移动文件、不删除部分事实。用户必须先解除关系并解决/忽略来源项。通过检查后，同一事务先给终态来源项写 `source_deleted_at` 和审计，再为 active file 写 `deletion_scope = task` tombstone并移入 trash；随后级联删除 Task 聚合。终态 Focus Session、已解除关系快照和来源 Inbox 快照继续保留；失败恢复文件并回滚来源标记，提交后清理物理文件。

## 受控文件目录

```text
artifacts/
  .opc-artifact-store-v1
  .opc-artifact-store.lock
  .staging/
  objects/
  .trash/
  .quarantine/
```

schema v9 为数据库创建单例 `workspace_identity`：`database_id` 永久不可变，`artifact_store_id` 只能从空值绑定一次。Sidecar 只接受自己声明过的 root：未绑定数据库声明空目录时写规范 JSON marker `{format_version,database_id,store_id}`，再把 store ID 写回数据库；后续格式/版本/数据库 ID/store ID 任一不符、已绑定数据库改用另一空 root、非空缺 marker、卷根、符号链接或 reparse point 路径均拒绝启动。启动协调前通过 `.opc-artifact-store.lock` 获取进程级非阻塞独占锁，并在 router 生命周期内持有；第二个指向同一 root 的 Sidecar 会启动失败。文件名使用服务端 Artifact UUID，数据库固定只保存 `objects/<artifact-id>` 相对路径。提交事务报错后先查数据库，只有能证明无引用才清除已提升 object；无法排除模糊 COMMIT 已落库时保留给启动 reconcile。marker、暂存文件、对象提升、移动/删除与关键目录项在返回成功前做耐久同步。启动恢复 active trash 前核对 size/SHA-256；错配候选移入 `.quarantine/` 并把 Artifact 标为 mismatch。tombstone 对应且校验一致的授权删除可清理，其余无引用受控候选进入 quarantine；意外目录、链接与无法识别的名称不会被递归跟随或清理。

## 前端交互与并发

- 筛选面板可保存当前完整条件、选择并立即应用视图、以当前条件更新所选视图，或二次确认删除；保存视图在 SQLite 中跨重启保留。创建达到 20 个上限、同名、网络错误和版本冲突都有明确反馈。
- 应用视图会原子替换搜索、状态、优先级、类型、项目、客户、标签、日期和排序并回到第一页，不恢复旧页码或任务选择；更新/删除携带当前视图版本，冲突时刷新列表且不自动覆盖。
- 客户筛选复用共享 ClientSelect，而不是预先拉全 Client options。控件每页请求 20 条，输入经 250 ms 防抖后发送 `q`，支持稳定上一页/下一页、取消旧请求、跨页/失败保留当前选择、inactive 可见可选，以及加载、空、错误重试、更多结果提示和 combobox 键盘语义。服务端仍沿 Task 的 Project 当前 `client_id` 过滤；Task 不保存客户副本。客户与项目、标签、状态及日期等条件取 AND，并完整保留到后续分页和保存视图应用。
- Project 选择复用共享 ProjectSelect，而不是预先拉全 Project options。Task 新建/编辑、Tasks 项目筛选、批量 `set_project` 的目标和 Inbox 拆分中的每个任务都使用同一组件；每页请求 20 条，输入经 250 ms 防抖后发送 `q`，`q / page / includeArchived` 进入 Query key，旧列表和选中详情请求可取消，候选按 ID 去重。默认候选不列归档项目；详情查询或调用方名称 fallback 使跨页、失败、保存视图及既有归档关联仍保留当前选择。只有用户点击可聚焦的清除按钮才提交空筛选或 `project_id=null`，搜索、翻页和错误不得隐式解除关联。
- ProjectSelect 显示项目状态与客户上下文，并覆盖输入等待、首批加载、无项目、无匹配、错误重试、更多结果和前后翻页；combobox/listbox 使用方向键、Home/End、Enter、Escape、Tab 和 PageUp/PageDown，输入法合成期间不解释快捷键。Task 项目筛选改变后回到第一页，继续与客户、标签、状态和日期条件取 AND；批量目标只改变用户选中的 Task，不把当前列表页冒充完整选择集。
- 计划日期支持精确值或起止范围二选一，截止日期支持独立起止范围；设置任一计划范围端点会清空精确值，设置精确值会清空计划范围。
- `due_state=overdue|due_soon` 按单次请求捕获的 Sidecar UTC 时刻派生，并固定排除 done/cancelled；逾期为 `< now`，临期为 `[now, now+24h]`。它与按 UTC 日期片段过滤的 `due_from/due_to` 语义不同且不可同时提交；显式状态只允许 `active`。比较和 `due_date` 排序使用固定宽度 UTC 纳秒键，避免亚毫秒精度丢失与 RFC3339 整秒/小数秒文本排序误差。
- `due_state` 当前只服务 Today 的动态风险快捷视图，不纳入 schema v17 保存视图。保存视图继续表示用户明确选择的静态任务页条件，不能静默保存或恢复会随服务端时钟自动变化的结果集。
- 起点晚于终点时，对应日期控件显示无效状态和就地错误，主 Task Query 暂停，旧任务结果不继续展示；服务端仍二次校验格式和顺序，避免绕过 UI。
- 任务页只有在选中一个精确计划日期、排序为 `manual_order` 且没有搜索、状态、优先级、类型、项目或标签筛选时启用排序；上移/下移与拖动手柄同时保留。
- 拖拽限定在同一状态分组，不会通过视觉移动暗中改变 Task 生命周期。前端立即预览当前页相对位置，但 hook 按源/目标重新读取完整计划日期组，校验日期、状态和可见版本，再把同状态顺序织回完整组的原槽位并调用既有原子 reorder API。
- 看板固定展示六个生命周期列，列计数明确是当前服务端分页结果；卡片可选择进入既有原子批量操作，点击正文进入共享任务详情。跨列拖拽按来源/目标解析为 `start / block / unblock / complete / cancel / reopen`，确认后携带卡片版本执行；阻塞/取消要求原因，blocked 只能解除回 `blocked_from_status`，manual 与 waiting_review 不得绕过详情中的人工验收。
- 看板不做乐观状态写入；命令成功后由 Query 失效读取服务端事实，失败时卡片保持原列。版本冲突关闭旧确认、刷新列表并要求用户基于新版本重新拖动，不自动重试。
- 分页页面不把当前 50 行作为完整计划组提交；完整集合由 hook 自动分页读取，最多 1,000 项。集合/版本变化、网络错误或服务端拒绝时清除乐观预览并刷新 Task/Project/Today/Inbox，页面回到服务端事实。
- 新建与详情编辑提供 review policy；详情只有在 todo 且无 current/历史 Submission 时开放修改。
- 输出编辑器允许 summary 加最多 20 个条目；文件草稿保留浏览器 `File`，不会先复制或上传。
- waiting_review 展示当前批次和完整 Artifact 摘要，接受与返工互斥；返工原因空白时前端阻止提交。
- 列表与详情把进度分母显示为直属非取消子任务数，并另列取消数；child_rollup 标记为“子任务汇总”，明确这是系统发起的父任务验收且没有 Artifact，不伪装成人工产出。
- manual 父任务缺少 assignee 或 builtin owner reviewer 时，产出区提示补齐门禁后才会自动发起；policy none 明确不会因子任务完成自动完成。系统请求、撤回和重开事件及失效原因在时间线中有独立文案。
- Artifact 正文按需加载；missing/mismatch/deleted/corrupt 响应均有明确提示和重试边界，不将下载错误伪装为成功。
- 上传与下载的 120 秒传输期间显示 busy 状态并锁定其他 Task 写入；超时或失败保留未提交草稿，不伪造成功。
- 删除需要确认并填写原因；pending-review 项不显示可执行删除动作。
- Task 删除若被活动 Inbox 关系阻止，前端显示可解释冲突并提示先到收件箱解除活动关系，不自动替用户解除或重试删除；当前尚无 Task→Inbox 反向关系列表或直达导航。
- 所有 D2 写操作与事实编辑、Assignment、生命周期命令互斥。
- 版本冲突时客户端保留完整草稿，刷新 Task/Assignment/Submission/Artifact/Event/Project/Today 缓存，再要求用户重新确认；不会用旧 `If-Match` 自动重试。
- 成功提交或写命令使用同一稳定幂等 key；重新确认冲突后生成新命令上下文，避免把不同预期版本复用到旧 key。

## 迁移与兼容

schema v9 的 `009_task_submissions_artifacts.sql`：

- 新建单例 `workspace_identity`：规范 UUID `database_id` 不可变，`artifact_store_id` 只允许从空值绑定一次，用于把受控 Artifact root 与当前数据库一一对应；
- 新建 Submission/Artifact 表，以及跨聚合删除保留且不可变的 `artifact_deletion_tombstones`；
- 给 Task 增加 `current_submission_id` 并要求只能指向同 Task 最新批次；
- 给 Workflow Event 增加 nullable `submission_id / artifact_id`，校验聚合一致性并延续追加式保护；
- 对 schema v8 已存在且无歧义的 manual 提交/审核事实生成 `is_inferred = 1` 的 sequence 1 Submission；
- 用内置 owner 填提交/审核/撤回 Actor，按旧状态推断 pending/accepted/changes_requested/withdrawn；
- 写一条 system Actor 的 `migration_submission_backfill` 事件；
- 不为历史任务编造 Artifact。

迁移在固定连接上事务外临时关闭外键，事务提交前运行 `foreign_key_check`，成功或失败都恢复外键；异常时整体回滚。

schema v13 的 `013_inbox_item_tasks.sql` 不改写 Task 表或 D2 文件契约，只新增关系表及 Task 删除保护。关系 GET 实时 JOIN `tasks`；A2 不新增 Task.version→Inbox.version trigger。Task 删除前由 API/数据库共同拒绝活动关系，已解除历史关系使用 nullable FK `SET NULL` 并保留原 ID/标题快照。

schema v23 的 `023_task_artifact_inbox_projection.sql` 不重建 Task/Submission/Artifact/Inbox 表，也不回填历史 `requires_followup`。它增加 Artifact 来源索引和 insert/update/delete guards：只允许存在且未删的 follow-up Artifact 建立规范来源；来源身份/payload 不可变；Artifact 软删或聚合硬删前必须先完成 Inbox 来源协调。

schema v24 的 `024_task_blocked_inbox_projection.sql` 不回填迁移前已阻塞 Task。它以阻塞后的 Task version 区分每次 block，约束 `source_entity_type=task` 的事件键和最小快照，冻结来源身份；活动来源阻止 Task 硬删除，来源项终态后删除事务先写 `source_deleted_at` 与 Inbox 审计，再删除 Task。

schema v25 的 `025_task_due_inbox_projection.sql` 不回填迁移前已进入临期窗口的 Task。Sidecar ready 前及运行中每 15 秒扫描状态非终态且截止时间不晚于未来 24 小时的 Task，以 `task:<task-id>:due:<due-at>` 为稳定键；每批最多 100 条且排除已投影截止事实，积压可持续推进。改期形成新截止事实，已生成事项不随完成/取消/改期自动归档。活动来源阻止 Task 删除，来源项终态后复用统一删除协调保留快照。schema v26–v29 的系统维护、Workspace Avatar、Project 完成来源和存储设置迁移不改写 Task 表；schema v30 只给 `task_submissions` 增加 `origin` 并新增父任务推进规则，不改变 `tasks` 表字段或既有生命周期状态集合。

schema v30 的 `030_task_parent_progress.sql` 是非破坏性追加迁移：

- 给 `task_submissions` 增加非空 `origin`，只允许 `manual / child_rollup`；既有行通过默认值保持 manual，不重写其状态、Actor、摘要或时间；
- 约束 child_rollup 只能由内置 system 创建、必须 `is_inferred=0`，并禁止其拥有 Task Artifact；`origin` 与其他 Submission 身份事实同样不可变；
- 不在 migration 或 Sidecar 启动时扫描、补写既有父任务层级。只有迁移后的相关 Task/Assignment/review policy 写命令触发事务内 reconciliation；
- 不改变 Inbox 表或 `inbox_item_tasks.is_required`，不创建 demo 数据。schema v31 已追加 Project→Client Activity 来源约束，schema v32 已扩展 Reminder，schema v33 已新增受限 Automation Rule/Run，schema v34 已新增 Agent Adapter，schema v35 已新增 Client Followup；下一迁移只能从 `036_*` 追加。

## 已验证与后续

当前自动验证覆盖：

- migration v8→v9 数据保留、约束、inferred 回填、事件关联、重跑/回滚和外键恢复；
- JSON 与 multipart 混合提交、Actor 归属、限制、并发、幂等重放和补偿；
- 接受、返工、取消撤回、reopen、软删、Task 硬删、文件丢失/篡改和安全下载头；
- 前端 manual 前置条件、混合草稿、审核、冲突时 `File` 保留、下载错误与软删确认；
- 前端全量测试、typecheck、Web build、format check；Go 全包测试、database 重复测试和 `go vet`。
- 任务页精确计划日期下的同状态拖拽、乐观顺序、完整计划组槽位重建和版本校验。
- 任务页列表/看板切换、看板平铺查询、六列空状态、卡片详情直达、跨列命令映射/原因/确认/版本及人工验收门禁，以及与现有批量选择共用事实。
- 任务页计划/截止日期范围序列化、合法范围分页请求、倒置范围查询门禁，以及 Sidecar 的合法/非法范围过滤。
- Today 截止风险客户端序列化、Sidecar 固定时钟边界/终态排除/冲突拒绝、稳定分页，以及同一时钟下逾期/临期列表 `meta.total` 与 Today 统计严格一致。
- 任务页 ClientSelect 的首批/搜索/上一页与下一页有界分页、查询键切换与卸载取消、跨页和失败选中保留、inactive、加载/空/错误重试/更多结果反馈、combobox 键盘交互及页面接线；客户端 `client_id` 序列化、保存视图恢复、Sidecar UUID 拒绝及 Task→Project→Client 正向过滤继续覆盖。
- ProjectSelect 的 250 ms 搜索、首批/前后页有界请求、`q / page / includeArchived` Query key、实际取消、按 ID 去重、当前归档/失败 fallback、显式清除、加载/空/错误/更多结果和 combobox 键盘交互已有组件与 hook 级覆盖；Task 新建/编辑、Tasks 筛选/批量目标和 Inbox 拆分均有接线验证。Go 定向测试覆盖 Project 同名排序以 `id ASC` 收口、所有排序字段和既有过滤契约；代码审查确认 `COUNT` 与 `SCAN` 共用请求上下文内的只读事务。模块文档不单独维护易过期的全仓测试总数。
- schema v16→v17 事实保留、保存视图 JSON/名称/schema 约束、API 规范化/并发/确认删除，以及前端应用/创建/更新/删除交互。
- schema v22→v23 不发明 Inbox 数据；follow-up Artifact 提交/幂等重放/事务回滚、来源上下文、活动删除阻止、归档后 Artifact/Task 删除协调和来源快照保留。
- schema v23→v24 不回填既有 blocked Task；每次 block 的稳定来源、幂等重放、重复阻塞、前端来源上下文、活动删除阻止及终态后 Task 删除协调和快照保留。
- schema v24→v25 不回填既有 Task；提前 24 小时/启动补偿扫描、逾期分类、稳定 Task+截止时间来源键、100 条批次推进、改期独立来源、事务回滚、前端上下文和 Task 删除协调。
- schema v29→v30 数据保留、既有 Submission 默认 manual、origin/system/inferred/Artifact 约束、迁移失败回滚、外键完整性和无破坏性迁移门禁。
- 直属子任务 done/cancelled 口径、零非取消子任务、manual/assignee/builtin owner reviewer 门禁、系统无 Artifact child_rollup、owner accept、失效撤回、blocked 来源更新、changes_requested/manual 不覆盖，以及 accepted 父任务与祖先重开。
- 创建、改绑、解除父级、删除、单条与批量生命周期、review accept、policy 和 Assignment 变更均在事务内协调；批量去重且返回最终版本，Inbox 显式 required 关系保持独立。
- 前端 origin/取消计数兼容规范化、“子任务汇总”/门禁/时间线文案、列表与详情的非取消进度展示；typecheck 与定向组件测试覆盖。

已知环境边界：当前自动协调只由迁移后的相关写命令触发，没有全库启动回填；accepted 父任务被系统重开时不会恢复已经结束的 Assignment，需要 owner 重新分派后才能继续人工流转。祖先协调使用 visited 集合防止循环，并沿完整有效祖先链传播。ClientSelect 与 ProjectSelect 均尚未完成真实浏览器键盘/焦点、窄屏和 1,000/10,000 条数据性能专项；组件测试不能替代这些证据，Project 的包含式 `LIKE` 搜索也不能仅凭有界分页推断大数据量响应性能。Windows 桌面 Rust 原生测试在未安装 MSVC linker 的主机仍可能无法执行，这不影响已通过的 Go 定向测试和前端 typecheck，但不能据此宣称完整跨平台桌面验收。

仍属后续：其他业务来源投影、自动创建 Reminder、Agent Adapter/Run、自动生成 Artifact、Focus 高级分析、Client 外部来源/回访/财务，以及 AI 助手与知识库；显式 follow-up Artifact、Task 阻塞与 Task 临期来源已经交付。

## 相关代码/PRD 链接

- [PRD 任务需求与 T-18D](../opc-workspace-PRD.md)
- [schema v9 迁移](../../services/sidecar/internal/database/migrations/009_task_submissions_artifacts.sql)
- [schema v13 Inbox–Task 关系迁移](../../services/sidecar/internal/database/migrations/013_inbox_item_tasks.sql)
- [schema v23 Artifact 来源迁移](../../services/sidecar/internal/database/migrations/023_task_artifact_inbox_projection.sql)
- [schema v24 Task 阻塞来源迁移](../../services/sidecar/internal/database/migrations/024_task_blocked_inbox_projection.sql)
- [schema v25 Task 临期来源迁移](../../services/sidecar/internal/database/migrations/025_task_due_inbox_projection.sql)
- [schema v30 父任务自动推进迁移](../../services/sidecar/internal/database/migrations/030_task_parent_progress.sql)
- [Task output API](../../services/sidecar/internal/api/task_outputs.go)
- [Inbox 来源投影服务](../../services/sidecar/internal/api/inbox_source_projections.go)
- [Task 临期扫描服务](../../services/sidecar/internal/api/task_due_projections.go)
- [受控 Artifact store](../../services/sidecar/internal/api/artifact_store.go)
- [Task 生命周期](../../services/sidecar/internal/api/task_workflow.go)
- [父任务自动推进服务](../../services/sidecar/internal/api/task_parent_progress.go)
- [Task API](../../services/sidecar/internal/api/tasks.go)
- [Task 保存视图 API](../../services/sidecar/internal/api/task_saved_views.go)
- [schema v17 保存视图迁移](../../services/sidecar/internal/database/migrations/017_task_saved_views.sql)
- [Task output model](../../services/sidecar/internal/models/artifact.go)
- [前端 Task output 组件](../../apps/web/src/components/TaskOutputsSection.tsx)
- [前端 Task 专注记录组件](../../apps/web/src/components/TaskFocusHistorySection.tsx)
- [任务列表页](../../apps/web/src/pages/TasksPage.tsx)
- [任务看板](../../apps/web/src/components/TaskBoard.tsx)
- [任务看板生命周期确认](../../apps/web/src/components/TaskBoardTransitionModal.tsx)
- [任务保存视图控件](../../apps/web/src/components/TaskSavedViewsControl.tsx)
- [共享 Client 选择器](../../apps/web/src/components/ClientSelect.tsx)
- [共享 Project 选择器](../../apps/web/src/components/ProjectSelect.tsx)
- [任务列表页测试](../../apps/web/src/pages/TasksPage.test.tsx)
- [前端 Artifact 卡片](../../apps/web/src/components/TaskArtifactCard.tsx)
- [Go D2 测试](../../services/sidecar/internal/api/task_outputs_test.go)
- [父任务自动推进测试](../../services/sidecar/internal/api/task_parent_progress_test.go)
- [schema v30 迁移测试](../../services/sidecar/internal/database/task_parent_progress_migration_test.go)
- [迁移测试](../../services/sidecar/internal/database/task_artifacts_migration_test.go)
- [Inbox–Task 删除互锁测试](../../services/sidecar/internal/api/inbox_item_tasks_test.go)
- [前端 D2 测试](../../apps/web/src/components/TaskOutputsSection.test.tsx)
