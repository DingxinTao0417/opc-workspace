# 任务管理模块

> 实现基线：app v0.1.0 / API v1 / SQLite schema v9（2026-08-27）
>
> 版本边界：任务事实层、Actor/Assignment、T-18D D1 六状态生命周期与时间线，以及 T-18D D2 manual Submission/Artifact 提交验收均已交付。Inbox/Reminder 消费、本地 Agent Run、专注工时回写和任务看板属于后续纵切。

导航：[文档中心](../README.md) · [整体功能架构](../functional-architecture.md) · [PRD v2.1](../opc-workspace-PRD.md) · [Actor 与分派](actors.md) · [数据管理](data-management.md)

## 定位与边界

Task 是 opc-workspace 唯一可执行工单。项目、未来 Inbox、提醒和 Agent 都只能关联或驱动 Task，不能另建一套互不一致的完成状态。

当前模块负责：

- Task 事实、父子关系、标签、Project 关联、计划与完成标准；
- assignee/reviewer 的当前责任与历史 Assignment；
- 六状态受控生命周期和追加式 Workflow Event；
- `none` 直接完成与 `manual` 提交验收两种策略；
- Submission 批次、四种 Artifact、受控文件下载、审计软删除及 Task 聚合硬删除；
- 所有真实页面的加载、空数据、错误、重试、版本冲突和草稿保留。

当前不负责：Inbox 的来源消费与自动拆分、Agent Runtime、远程协作/通知、自动生成产出、AI 分析、知识库、备份/恢复或专注计时写入。

## 已实现状态

- Task 新建、详情、非生命周期编辑、确认删除、服务端分页/筛选/搜索/排序、批量安全操作和计划组排序已接通真实 SQLite。
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

## 数据模型与约束

### Task

| 字段 | 当前约束 |
| --- | --- |
| `status` | `todo / in_progress / blocked / waiting_review / done / cancelled` |
| `review_policy` | `none / manual`；策略改变只允许 `todo` 且 Submission 历史为 0 |
| `current_submission_id` | 指向同一 Task 的最新 Submission；接受、返工、取消后保留，reopen 清空 |
| `submitted_at / reviewed_at` | 当前/最近一次提交流程的快速状态时间；reopen 清空，历史以 Submission 为准 |
| `blocked_from_status` | block 时由服务端保存，unblock 只能恢复这个状态 |
| `version` | 任一影响 Task 聚合呈现或决策的写入递增，用作 ETag 和乐观锁 |

Task 创建仍只允许 `todo`；非 `todo` 创建返回 `LIFECYCLE_COMMAND_REQUIRED`。状态不能经通用 PATCH 修改。

### TaskSubmission

| 字段 | 含义 |
| --- | --- |
| `id / task_id / sequence` | UUID、所属 Task、Task 内从 1 递增且唯一的批次序号 |
| `status` | `pending_review / accepted / changes_requested / withdrawn`；每个 Task 至多一条 pending |
| `summary` | 可为空但最长 10,000 字符；提交时 summary 与 Artifact 至少一个存在 |
| `submitted_by_actor_id / submitted_at` | 当前实现固定内置 owner 代录及提交时间 |
| `reviewed_by_actor_id / reviewed_at / review_reason` | 接受或返工时由内置 owner 记录；返工原因必填 |
| `withdrawn_by_actor_id / withdrawn_at` | waiting-review Task 取消时由内置 owner 记录 |
| `is_inferred` | schema v9 从无歧义旧 manual 状态回填的批次为 true |

Submission 的 Task、序号、摘要、提交人、提交时间和 inferred 标记不可修改；只有 `pending_review` 可一次性转为 accepted、changes_requested 或 withdrawn。Task 仍存在时禁止直接硬删 Submission。

### TaskArtifact

| 字段 | 含义与约束 |
| --- | --- |
| `position` | 批次内从 1 开始且唯一，保持客户端提交顺序 |
| `submission_status`（API 派生） | 必填 `pending_review / accepted / changes_requested / withdrawn`，由父 Submission JOIN 得出；不是第二份可写状态 |
| `storage_kind` | `text / link / structured / file` |
| payload | text→`content_text`，link→`reference_url`，structured→`structured_json`，file→受控 `relative_path = objects/<artifact-id>`；四者严格互斥 |
| `name` | trim 后 1–255 个安全字符，不允许控制字符 |
| `mime_type / size_bytes / sha256` | 仅文件需要；SHA-256 为 64 位小写十六进制 |
| `requires_followup` | 人工标记需要后续动作；当前只展示，不自动生成 Task |
| `produced_by_actor_id` | 提交瞬间的活动 assignee，由服务端派生，客户端不能指定 |
| `recorded_by_actor_id` | 固定内置 owner，表达“我代录” |
| `integrity_status` | `unverified / verified / missing / mismatch`；unverified 的检查时间必须为空，其他状态必须有检查时间 |
| `deleted_*` | owner 软删除时间、操作人和 1–1,000 字符原因，三者同为空或同为非空 |

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
```

规则：

- start 需要 active assignee。
- block/cancel 原因必填；unblock 不能由客户端选择目标状态。
- `none` 可从 todo/in_progress 直接 complete；manual 必须提交并经 owner accept。
- submit-output 只允许 manual 的 todo/in_progress，且同时需要 active assignee 与 active owner reviewer。
- accept 在同一事务完成 Task 并结束所有活动 Assignment；request_changes 保留 Assignment 并返回 in_progress。
- cancel 结束活动 Assignment；若当前是 waiting_review（包括从该状态 block 后），还会先把 pending Submission 撤回。
- accept、request_changes、cancel 保留 `current_submission_id` 作为最近批次指针；reopen 清空指针和快速时间字段，但保留 Submission、Artifact 与 Event 历史，也不恢复旧 Assignment。

## API 契约

### Task 与生命周期

| 方法 | 路径 | 关键约束 |
| --- | --- | --- |
| POST | `/api/v1/tasks` | 仅 todo；支持 `review_policy`; 可选稳定幂等键 |
| GET | `/api/v1/tasks/:id` | 完整 Task、关系、版本和 `ETag` |
| PATCH | `/api/v1/tasks/:id` | `If-Match`；不写 status；策略变化仅 todo+无历史 |
| DELETE | `/api/v1/tasks/:id` | `If-Match`；硬删整个 Task 聚合并协调文件清理 |
| POST | `/api/v1/tasks/:id/start|block|unblock|complete|cancel|reopen` | `If-Match`；可选稳定幂等键；显式状态机 |
| GET | `/api/v1/tasks/:id/events` | 默认 50/最大 100；返回 Task ETag 与 `meta.task_version` |

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
      "structured_json": {"outcome":"ok"},
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

服务端不接受 `produced_by_actor_id`。成功返回 `{data:{task,submission,artifacts,event}}` 并附新版 Task `ETag`。

提交限制：summary 最长 10,000 字符；最多 20 Artifact；`client_ref` 1–100 且同批唯一；文本最多 500,000 字符；HTTP(S) 链接最多 4,096 bytes、必须有 host 且不能含 userinfo；structured 必须是 JSON object 且编码后最多 1 MiB；严格 JSON body 与 multipart `manifest` 各最多 1 MiB；单文件非空且最多 50 MiB；完整 multipart 请求最多 100 MiB。Sidecar HTTP read/write timeout 为 180 秒；前端对提交和文件下载使用 120 秒端到端超时，并在发起 multipart 前估算 manifest 与文件总量，避免明知超过服务端边界仍上传。

### 验收

`POST /api/v1/tasks/:id/review` 要求 Task `If-Match`、可选稳定幂等键，body 为：

```json
{"decision":"accept","reason":"可选说明"}
```

或：

```json
{"decision":"request_changes","reason":"必须说明返工原因"}
```

reason 最长 1,000 字符。只有 manual + waiting_review + current pending Submission + active owner reviewer 可审核。成功返回 `{data:{task,submission,event}}`。

### 历史、详情与下载

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/tasks/:id/submissions?page=&page_size=` | sequence DESC；每条带 Actor 摘要、Artifact 摘要和总数；包含已软删 Artifact 摘要 |
| GET | `/api/v1/tasks/:id/artifacts?page=&page_size=&submission_id=&include_deleted=` | 默认隐藏软删；按批次倒序、position/id 正序 |
| GET | `/api/v1/artifacts/:id` | 返回元数据及按类型的正文；软删详情仍 200，但 payload 全为 null |
| GET | `/api/v1/artifacts/:id/content` | 仅 file；鉴权后校验大小和 SHA-256，再作为 attachment 下载 |

两个列表默认 `page=1 / page_size=50`、最大 100，返回 `{data, meta:{page,page_size,total,task_version}}` 和 Task `ETag`。所有 Artifact 摘要和详情都必须带由父 Submission 派生的 `submission_status`；摘要不暴露正文或 `relative_path`。前端依据该必填状态禁用 pending-review 删除，但服务端仍执行最终授权校验。

下载成功设置 Content-Type、Content-Length、安全 UTF-8 Content-Disposition、`X-Content-Type-Options: nosniff`、`Cache-Control: no-store` 和 SHA-256 ETag。非 file 返回 `ARTIFACT_CONTENT_UNAVAILABLE`；已删/缺失为 410；大小或哈希不符为 `ARTIFACT_INTEGRITY_MISMATCH` 并禁止输出内容。

前端对已冻结的 D2 错误码提供中文反馈：策略/提交/审核前置分别覆盖 `TASK_MANUAL_REVIEW_REQUIRED`、`TASK_ASSIGNEE_REQUIRED`、`TASK_REVIEWER_REQUIRED`、`TASK_SUBMISSION_NOT_ALLOWED`、`TASK_SUBMISSION_ALREADY_PENDING` 和 `TASK_REVIEW_NOT_ALLOWED`；Artifact 状态覆盖 `ARTIFACT_PENDING_REVIEW`、`ARTIFACT_ALREADY_DELETED`、`ARTIFACT_DELETED`、`ARTIFACT_FILE_MISSING` 与 `ARTIFACT_INTEGRITY_MISMATCH`。未识别错误仍显示服务端 message 和可重试边界，不吞掉 `request_id`。

### 软删除与硬删除

`DELETE /api/v1/artifacts/:id?confirm=true` 要求 Task `If-Match`、可选稳定幂等键和 `{ "reason": "1–1000 字符" }`。pending-review 批次禁止删除。非文件只写软删除元数据；file 删除在同一事务写不可变 tombstone，存在的文件先移入 `.trash/`，事务失败恢复，成功提交后清除 trash 文件。若物理文件已经缺失，确认软删除仍成功，并将完整性记录为 `missing` 及检查时间。列表可用 `include_deleted=true` 审计，详情隐藏已删 payload，文件下载返回 410。

Task DELETE 是聚合硬删除：同一事务先为每个 active file 写 `deletion_scope = task` tombstone，并把仍存在的文件放入 trash；缺失 object 不阻断删除。随后级联删除 Task、Submission、Artifact、Assignment 等成员，失败恢复已移动文件，提交后物理清理；tombstone 继续保留供启动恢复。它不是单 Artifact 删除的替代入口。

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

- 新建与详情编辑提供 review policy；详情只有在 todo 且无 current/历史 Submission 时开放修改。
- 输出编辑器允许 summary 加最多 20 个条目；文件草稿保留浏览器 `File`，不会先复制或上传。
- waiting_review 展示当前批次和完整 Artifact 摘要，接受与返工互斥；返工原因空白时前端阻止提交。
- Artifact 正文按需加载；missing/mismatch/deleted/corrupt 响应均有明确提示和重试边界，不将下载错误伪装为成功。
- 上传与下载的 120 秒传输期间显示 busy 状态并锁定其他 Task 写入；超时或失败保留未提交草稿，不伪造成功。
- 删除需要确认并填写原因；pending-review 项不显示可执行删除动作。
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

## 已验证与后续

当前自动验证覆盖：

- migration v8→v9 数据保留、约束、inferred 回填、事件关联、重跑/回滚和外键恢复；
- JSON 与 multipart 混合提交、Actor 归属、限制、并发、幂等重放和补偿；
- 接受、返工、取消撤回、reopen、软删、Task 硬删、文件丢失/篡改和安全下载头；
- 前端 manual 前置条件、混合草稿、审核、冲突时 `File` 保留、下载错误与软删确认；
- 前端全量测试、typecheck、Web build、format check；Go 全包测试、database 重复测试和 `go vet`。

仍属后续：Inbox/Reminder 编排、Agent Adapter/Run、自动生成 Artifact、Artifact 备份恢复、专注工时持久化、Client/Finance 业务、AI 助手与知识库。

## 相关代码/PRD 链接

- [PRD 任务需求与 T-18D](../opc-workspace-PRD.md)
- [schema v9 迁移](../../services/sidecar/internal/database/migrations/009_task_submissions_artifacts.sql)
- [Task output API](../../services/sidecar/internal/api/task_outputs.go)
- [受控 Artifact store](../../services/sidecar/internal/api/artifact_store.go)
- [Task 生命周期](../../services/sidecar/internal/api/task_workflow.go)
- [Task API](../../services/sidecar/internal/api/tasks.go)
- [Task output model](../../services/sidecar/internal/models/artifact.go)
- [前端 Task output 组件](../../apps/web/src/components/TaskOutputsSection.tsx)
- [前端 Artifact 卡片](../../apps/web/src/components/TaskArtifactCard.tsx)
- [Go D2 测试](../../services/sidecar/internal/api/task_outputs_test.go)
- [迁移测试](../../services/sidecar/internal/database/task_artifacts_migration_test.go)
- [前端 D2 测试](../../apps/web/src/components/TaskOutputsSection.test.tsx)
