# 客户管理模块

> 实现状态截止：2026-08-28（依据当前实现）
>
> 当前基线：app v0.1.0 / API v1 / SQLite schema v30。客户基础事实由 schema v10 引入，schema v18 追加本地活动，schema v19 追加受控附件，schema v20 追加 Client–person 显式关联；schema v21–v29 的其他扩展不改变 Client 契约，schema v30 只增加 `task_submissions.origin` 与父任务推进规则，同样不改变 Client 表或 API 契约。v0.1 的资料 CRUD、Project 客户关联、人工备注/会议时间线、客户附件和本地联系人关联已交付，模块仍为**部分完成**；外部活动来源、回访和财务聚合尚未交付。

## 定位与边界

Client 保存一人公司在本机维护的客户资料与业务关联，是 Project、Invoice 和后续回访的主数据，不是在线 CRM 或多人账号系统。

- 客户联系人不会自动成为 Actor。只有 owner 在客户详情显式选择已有 active person，或确认“新建并关联”后，联系人才能作为本地责任记录；每个 Client 同时只允许一个 active `contact` 关联。
- person Actor 不登录、不接收远程消息、不获得客户数据访问权；Client 与 Actor 保持不同语义。
- `project_count` 从 Project 实时派生，不写回 `clients`。
- 客户累计收入未来只从 `confirmed` Financial Entry 聚合；paid Invoice 只用于关联与对账，不在客户表维护第二份金额。
- 客户最近动态只从未删除 `client_activities.occurred_at` 派生；没有活动时显示“暂无本地活动”。当前只允许人工记录备注或会议，不伪造邮件打开、回复或提案下载。
- v0.1 不自动发送邮件、短信、发票或其他外部消息。

## 当前实现状态

当前状态为**部分完成**，已经具备可运行的客户资料事实层、列表、基础详情、Project 客户关联、本地活动时间线、受控附件和 person 显式关联。

### 已实现：数据与 Sidecar

- schema v10 通过递增迁移为 `clients` 增加从 1 开始的 `version`，并增加名称、状态和更新时间查询索引；旧数据中的空白可选字段归一为 `NULL`。
- Go Client model 与以下 API 已实现：
  - `GET/POST /api/v1/clients`
  - `GET/PATCH/DELETE /api/v1/clients/:id`
- 列表支持分页、关键词、状态筛选和白名单排序。`q` 搜索名称、联系人、邮箱和电话，并转义 SQL `LIKE` 中的 `%`、`_` 与反斜杠。
- Client 响应包含 `project_count`、nullable `latest_activity_at`、`version` 和 RFC 3339 UTC 时间戳；列表和详情分别从 Project 与未删除 Client Activity 实时派生项目数和最近活动。
- 创建支持可选 `Idempotency-Key`。服务端对清洗后的规范请求保存 SHA-256 与首次 `201` 响应快照；同键同请求稳定重放且不重复创建，同键异体返回 `409 IDEMPOTENCY_CONFLICT`。
- 创建、详情和更新响应返回 `ETag`；PATCH 与 DELETE 强制 `If-Match`。缺少版本返回 428，非法版本返回 400，旧版本返回 `409 VERSION_CONFLICT`。
- Project 插入、删除或 `client_id` 改绑会原子递增受影响 Client 的 `version` 与 `updated_at`，避免用户基于旧 `project_count` 编辑或永久删除。
- 保留既有反向传播：Client 名称改变或永久删除会递增关联 Project 的聚合版本，使 Project 的 `client_name` 快照失效。
- 永久删除使用 `DELETE /api/v1/clients/:id?confirm=true`，只允许 `inactive` Client，并再次校验最新 `If-Match`。Project 是可选关联，删除后由外键 `ON DELETE SET NULL` 保留项目并返回 `detached_projects`；Invoice 是强引用，存在时返回 `409 CLIENT_HAS_INVOICES`，客户、Project 关联和双方版本均不改变。
- schema v18 新增 `client_activities`。人工 `note / meeting` 要求正文和明确发生时间，由服务端固定记录当前 owner；未来来源预留 `system_reference`，当前公开创建 API 不允许客户端伪造。
- 活动列表默认只返回未删除记录，按 `occurred_at DESC, id ASC` 稳定分页，可按 kind 筛选并显式包含删除历史；活动详情、修改和确认软删除均为版本化 API。
- 活动新增、修改或软删除由数据库 trigger 原子递增 Client 聚合 `version`；删除活动保留标题、记录人、发生时间、删除人/原因和时间，响应不再返回人工正文。已删除活动以及 system reference 均不可编辑。
- schema v19 新增 `client_attachments` 与不可变 `client_attachment_deletion_tombstones`。每个附件固定属于一个 Client，可选关联该 Client 的一条未删除 Activity；文件事实、记录人和创建时间不可改，删除只写成组软删除事实。
- 客户附件和 Task file Artifact 共用已经绑定数据库身份的受控 Artifact store，但拥有独立业务表和 API。schema trigger 阻止两张表产生相同 object UUID，物理路径仍只保存为 `objects/<uuid>`。
- 上传要求 Client `If-Match`，可使用 `Idempotency-Key`；严格 multipart 只接受首个 `metadata` JSON part 和随后唯一 `file` part。文件必须非空且不超过 50 MiB，完整请求不超过 100 MiB。
- 列表默认隐藏删除记录，按 `created_at DESC, id ASC` 稳定分页，可按 `activity_id` 筛选。详情和列表返回 `client_version`；下载只经鉴权 content 端点，读取前复验 size/SHA-256，缺失或错配时持久化完整性状态并拒绝返回正文。
- 删除要求 `confirm=true`、Client `If-Match`、1–1,000 字原因和可选幂等键；同事务写 tombstone、移动受控文件到 trash、软删除事实并递增 Client 版本，事务失败恢复文件。Client 聚合永久删除同样先协调全部 active 附件，成功后清理 trash，失败执行逆序补偿。
- schema v20 新增 `client_actor_links`。公开契约当前只允许 `role=contact` 且 Actor 必须是 active `person`；部分唯一索引保证每个 Client 同时最多一条 active contact 关联。
- 关联要求 Client `If-Match` 和可选 `Idempotency-Key`，请求必须二选一：引用已有 `actor_id`，或提供 `create_person`。后者在同一事务创建 person、记录 `actor_created` Event 并建立关联，任一步失败都会整体回滚。
- 解除要求 `confirm=true`、Client `If-Match`、1–1,000 字原因和可选幂等键；解除人、时间和原因作为成组终态写入，数据库阻止历史修改/硬删除。关联或解除都递增 Client 聚合版本；存在 active Client 关联的 person 不能停用。

### 已实现：Web 纵切

- `/clients` 使用真实 API 展示总数、紧凑表格、名称首字符头像、联系人、真实项目数、状态和分页；服务端搜索与状态筛选变化时回到第 1 页。
- 列表覆盖加载 skeleton、首次空状态、筛选无结果、错误和重试。累计收入固定显示“v0.4 后可用”，最近动态显示真实 `latest_activity_at`，无记录时显示“暂无本地活动”。
- 新建/编辑表单覆盖名称、联系人、邮箱、电话、备注和状态；空白可选值提交为 `null`，保存中禁止关闭和重复提交。
- 编辑发生版本冲突时刷新最新 Client 版本，但保留用户未提交草稿并要求重新确认。
- `/clients/:id` 展示基础资料、真实关联项目列表及分页、状态调整、危险区、本地活动时间线、客户附件和“本地联系人”。用户可选择已有 active person，或以联系人名称为默认值原子新建并关联；解除必须填写原因，历史按需展开。界面明确提示这不会创建账号、发送消息或授予访问权；回访和发票/收入仍标为后续版本。
- 停用或恢复只更新 Client 状态，不解除 Project。永久删除前必须先停用并二次确认，界面展示将解除的项目数；成功后回到客户列表并显示实际解除数量。
- Project 新建/编辑表单已接真实客户选择器，可关联、改绑或选择“不关联客户”；选项拉全分页结果并标注 inactive Client。客户选项加载失败时可重试，编辑已有 Project 时保留当前关联，不会静默提交 `null`。
- 项目列表已支持按 Client 筛选；Client 创建、编辑、删除和 Project 关联变化会刷新相关 Client/Project 查询缓存。

### 已知缺口

- 当前活动只覆盖人工备注和会议；尚无邮件、回访或其他业务来源写入 `system_reference` 的投影器。附件可选关联活动，但不会自动生成或伪造 Activity。
- 没有客户标签、去重合并、批量操作或导入导出。
- 当前仅支持一个 `contact` 角色，不支持多联系人、角色自定义、客户门户或远程协作。
- 没有发票详情、累计收入或 Financial Entry 聚合；这些能力归入 v0.4。
- 没有 `client_followups`、提醒、今日或 Inbox 联动；回访归入 v0.4。
- 仍需真实浏览器完成键盘焦点、窄屏表格和大客户量分页性能验收。

## 当前用户流程

### 显式关联或解除本地联系人

1. owner 在客户详情“本地联系人”选择已有 active person，或切换到“新建并关联”；系统不会根据 `contact_name` 自动生成 Actor。
2. Web 使用当前 Client 聚合版本发送二选一请求。Sidecar 在同一事务校验 person 状态、单 active contact 约束，并创建关联；新建模式同时创建 person 和 `actor_created` Event。
3. 成功后 Client 版本递增，关联区、客户详情和 Actor 选项缓存刷新；并发冲突会读取最新事实，但不会替用户自动重试旧意图。
4. 解除前必须填写原因并明确确认。成功后历史保留关联/解除双方 Actor 摘要、时间和原因，原 person 仍存在且可在没有其他活动责任时单独停用。

### 上传、校验下载与删除客户附件

1. owner 在客户详情选择本地文件；Web 保留真实 `File` 草稿并展示名称和大小，未明确确认前不上传。
2. Web 以当前 Client 版本发送 `metadata + file` multipart，可重试请求复用同一幂等键。Sidecar 将文件流式写入 `.staging`，计算大小、MIME 与 SHA-256，再原子提升到 `objects/<uuid>` 并在同一事务创建附件事实。
3. 成功后 Client 版本递增，附件列表和客户详情刷新。并发冲突不会覆盖新事实，保留浏览器文件草稿供用户基于最新版本再次确认。
4. 下载只通过 `/content`；Sidecar 打开受控普通文件并复验大小与 SHA-256，成功才返回强制 attachment、`nosniff`、`no-store` 和内容 hash ETag。
5. 删除先填写原因并确认。Sidecar 同事务记录不可变 tombstone、把文件移入 trash、写软删除事实；提交失败恢复文件，成功后物理清理 trash。删除历史只显示元数据和原因，不提供正文下载。

### 记录、修订与删除客户活动

1. owner 在客户详情选择“记录活动”，填写备注或会议的标题、正文和实际发生时间；客户端先校验，服务端再次限制字段长度及未来时间偏差。
2. 创建请求可带 `Idempotency-Key`；成功后活动与 Client 版本在同一事务更新，列表“最近动态”和详情时间线刷新。
3. 编辑活动时提交活动自身 `version` 对应的 `If-Match`。并发冲突不会盲目覆盖，界面重新读取时间线并要求用户再次确认。
4. 删除要求 `confirm=true`、最新 `If-Match` 和 1–1,000 字符原因。记录转为不可变删除历史，正文不再通过 API 返回；默认时间线隐藏删除历史，用户可显式打开审计视图。

### 建立客户并关联项目

1. owner 在客户列表新建 Client，填写名称、可选联系人资料与状态。
2. Web 先做即时校验；Sidecar 以服务端规则清洗并持久化，创建默认为 `active`，也可显式选择 `lead / inactive`。
3. owner 在 Project 新建或编辑表单中选择该 Client，或选择“不关联客户”。
4. Project 外键变化在同一数据库事务中使相关 Client 版本失效；客户详情从 Project 查询真实项目列表和数量。

### 编辑、停用与恢复

1. owner 从客户详情打开编辑表单，提交当前 Client `version` 对应的 `If-Match`。
2. 成功后 Client 版本递增；若名称改变，关联 Project 版本也递增。
3. 发生并发冲突时，界面刷新最新事实但不覆盖表单草稿。
4. 停用与恢复只改变 `status`，不删除资料、不解除 Project，也不改变 Invoice 引用。

### 永久删除

1. owner 先把 Client 调整为 `inactive`。
2. 详情危险区展示当前 `project_count`，并要求二次确认。
3. Sidecar 在事务中重新校验 Client 版本、inactive 状态和 Invoice 强引用。
4. 若存在 Invoice，返回可解释冲突且事务不改变任何事实；否则永久删除 Client，由 FK 将关联 Project 的 `client_id` 置空，并返回实际 `detached_projects`。

## 数据、校验与 API

### 当前数据

`clients` 当前字段为：

| 字段                      | 类型      | 当前约束 / 语义                                                              |
| ------------------------- | --------- | ---------------------------------------------------------------------------- |
| `id`                      | TEXT      | UUID 主键                                                                    |
| `name`                    | TEXT      | trim 后必填，1–200 个 Unicode 字符；不要求唯一                               |
| `contact_name`            | TEXT NULL | trim 后空白为 `NULL`，最多 200 个字符                                        |
| `email`                   | TEXT NULL | trim 后空白为 `NULL`，单一邮箱地址，最多 320 个字符；不要求唯一              |
| `phone`                   | TEXT NULL | trim 后空白为 `NULL`，最多 50 个字符；保留国际号码文本，不做破坏性数字归一化 |
| `notes`                   | TEXT NULL | trim 后空白为 `NULL`，最多 10,000 个字符；允许换行/制表符但拒绝其他控制字符  |
| `status`                  | TEXT      | `active / lead / inactive`，默认 `active`                                    |
| `version`                 | INTEGER   | schema v10，默认 1，必须大于等于 1；作为 Client 聚合乐观锁                   |
| `created_at / updated_at` | TEXT      | UTC；API 统一为 RFC 3339                                                     |
| `project_count`           | API 派生  | 从 `projects.client_id` 实时计数，不是 Client 表字段                         |
| `latest_activity_at`      | API 派生  | 未删除活动最大 `occurred_at`；没有活动为 `null`                              |

名称、联系人、邮箱和电话拒绝控制字符。服务端是最终校验边界；未知 JSON 字段、无可编辑字段的 PATCH 和不合法状态均被拒绝。

### 当前列表契约

- `page` 默认 1，`page_size` 默认 50、最大 100。
- `q` trim 后最多 200 字符，搜索 `name / contact_name / email / phone`。
- `status` 只接受 `active / lead / inactive`；未传时包含全部状态。
- `sort` 支持逗号分隔和 `-` 降序，白名单为 `name / contact_name / status / project_count / created_at / updated_at`。
- 默认 `updated_at DESC`；所有排序最后追加 `id ASC`，保证跨分页稳定。
- 返回 `{ "data": [...], "meta": { "page", "page_size", "total" } }`。

### Client Activity 数据与列表契约

`client_activities` 保存 `id / client_id / kind / title / body / occurred_at / created_by_actor_id / source_type / source_id / version / deleted_* / created_at / updated_at`。人工活动仅允许 `note / meeting`，正文必填 1–10,000 字符、标题 1–200 字符，发生时间必须是 RFC 3339 且不得超过服务端当前时间 5 分钟。来源字段只供 `system_reference` 使用，身份、来源和创建时间不可改；软删除后三元删除事实完整且整行终态不可变。

活动列表 `page` 默认 1，`page_size` 默认 20、最大 100；`kind` 可筛选三种持久类型，`include_deleted` 只接受 `true / false`，默认 false；结果按发生时间倒序和 ID 正序稳定分页，meta 同时返回 `client_version`。

### Client Attachment 数据与列表契约

`client_attachments` 保存 `id / client_id / activity_id / name / relative_path / mime_type / size_bytes / sha256 / recorded_by_actor_id / integrity_* / deleted_* / created_at`。`activity_id` 可空；存在时必须属于同一 Client 且 Activity 未删除。名称为 1–255 个安全字符，size 为 1–52,428,800 bytes，hash 为小写 64 位十六进制；相对路径严格等于 `objects/<id>`。除 `integrity_*` 派生观察和一次性的删除状态外，业务事实不可修改。

附件列表 `page` 默认 1、`page_size` 默认 20/最大 100；`include_deleted` 仅接受 `true / false`，可选 `activity_id` 必须是 canonical UUID；按创建时间倒序与 ID 正序分页，meta 返回 `client_version`。业务 JSON 导出包含附件数据库元数据但不嵌入文件正文；内部一致性备份、恢复演练和正式恢复包含所有 active Task file Artifact 与 Client Attachment objects。

### Client–Actor Link 数据与列表契约

`client_actor_links` 保存 `id / client_id / actor_id / role / linked_by_actor_id / linked_at / unlinked_by_actor_id / unlinked_at / unlink_reason`。当前 `role` 固定为 `contact`；Actor 必须是 active `person`，关联/解除操作者固定为 active owner。活动关联不可修改或硬删，解除后三项必须同时存在并成为不可变历史；Client 永久删除级联关系历史，但不删除 Actor。

列表 `page` 默认 1、`page_size` 默认 20/最大 100；`include_unlinked` 仅接受 `true / false`，默认只返回 active 关联；按 `linked_at DESC, id ASC` 稳定分页并返回 `client_version`。业务 JSON 导出包含关联历史；内部备份天然随 SQLite 包含全部关系事实。

### 当前 API

| 方法      | 路径                                                   | 当前行为                                                                                                          |
| --------- | ------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------- |
| GET       | `/api/v1/clients`                                      | 分页、关键词、状态筛选和稳定白名单排序；返回 `project_count`                                                      |
| POST      | `/api/v1/clients`                                      | 创建 Client，默认 active；可选 `Idempotency-Key` 保存规范请求与首次响应快照；返回 `201` 和 `ETag`                 |
| GET       | `/api/v1/clients/:id`                                  | 返回完整基本资料、`project_count` 与 `ETag`                                                                       |
| PATCH     | `/api/v1/clients/:id`                                  | 部分更新资料或状态；可选字段可显式 `null`；强制 `If-Match`，成功递增版本                                          |
| DELETE    | `/api/v1/clients/:id?confirm=true`                     | 强制 `If-Match`；只允许 inactive；Invoice 强引用阻止删除，Project 外键置空并返回 `deleted_id / detached_projects` |
| GET       | `/api/v1/projects?client_id=:id&include_archived=true` | 查询 Client 的完整关联项目历史；普通项目列表仍默认排除归档项，项目新建/编辑支持关联、改绑或解除 Client            |
| GET/POST  | `/api/v1/clients/:id/activities`                       | 稳定分页读取活动；POST 只创建人工 note/meeting，可用 `Idempotency-Key` 安全重放                                   |
| GET/PATCH | `/api/v1/client-activities/:id`                        | 读取单条（包括删除历史）；PATCH 强制活动 `If-Match`，只允许修改未删除人工活动                                     |
| DELETE    | `/api/v1/client-activities/:id?confirm=true`           | 强制活动 `If-Match` 和删除原因，执行可审计软删除；system reference 与已删除记录只读                               |
| GET/POST  | `/api/v1/clients/:id/attachments`                      | 分页读取附件或严格 multipart 上传；上传强制 Client `If-Match`，可选幂等键                                         |
| GET       | `/api/v1/client-attachments/:id`                       | 读取附件元数据和 Client `ETag`，包括显式查询到的删除历史                                                          |
| GET       | `/api/v1/client-attachments/:id/content`               | 对 active 受控文件执行 size/SHA-256 校验后下载；删除、缺失或错配时拒绝正文                                        |
| DELETE    | `/api/v1/client-attachments/:id?confirm=true`          | 强制 Client `If-Match`、确认、原因，可选幂等键；执行 tombstone + trash + 软删除补偿                               |
| GET/POST  | `/api/v1/clients/:id/actor-links`                      | 分页读取 active/历史关联；POST 强制 Client `If-Match`，已有 person 与原子新建 person 二选一，可选幂等键           |
| DELETE    | `/api/v1/client-actor-links/:id?confirm=true`          | 强制所属 Client `If-Match`、确认、原因和可选幂等键；写入不可变解除历史                                            |

### 当前错误与并发语义

| HTTP / code                                                             | 含义                                   |
| ----------------------------------------------------------------------- | -------------------------------------- |
| 400 `INVALID_JSON / INVALID_FILTER / INVALID_SORT / INVALID_PAGINATION` | 请求体、筛选、排序或分页不合法         |
| 400 `INVALID_CLIENT_ID / INVALID_VERSION / INVALID_IDEMPOTENCY_KEY`     | ID、If-Match 或幂等键格式不合法        |
| 404 `CLIENT_NOT_FOUND`                                                  | Client 不存在                          |
| 409 `IDEMPOTENCY_CONFLICT`                                              | 同一幂等键被用于不同规范请求           |
| 409 `VERSION_CONFLICT`                                                  | Client 聚合已变化，需刷新后重新确认    |
| 409 `CLIENT_NOT_INACTIVE`                                               | 永久删除前必须先停用                   |
| 409 `CLIENT_HAS_INVOICES`                                               | Invoice 强引用仍存在，删除事务无副作用 |
| 422 `VALIDATION_ERROR`                                                  | 字段或状态校验失败                     |
| 422 `CONFIRMATION_REQUIRED`                                             | 永久删除缺少 `confirm=true`            |
| 428 `VERSION_REQUIRED`                                                  | PATCH / DELETE 缺少 `If-Match`         |

活动接口复用 `INVALID_FILTER / INVALID_PAGINATION / INVALID_VERSION / VERSION_REQUIRED / VERSION_CONFLICT / IDEMPOTENCY_* / VALIDATION_ERROR / CONFIRMATION_REQUIRED`；缺失活动返回 `CLIENT_ACTIVITY_NOT_FOUND`，只读来源或已删除记录的修改返回 `CLIENT_ACTIVITY_READ_ONLY`。

附件接口另返回 `CLIENT_ATTACHMENT_NOT_FOUND / CLIENT_ATTACHMENT_DELETED / CLIENT_ATTACHMENT_ALREADY_DELETED / CLIENT_ACTIVITY_UNAVAILABLE / ATTACHMENT_STORAGE_UNAVAILABLE / CLIENT_ATTACHMENT_FILE_MISSING / CLIENT_ATTACHMENT_INTEGRITY_MISMATCH`；超限返回 413，multipart 结构错误返回 `INVALID_MULTIPART`。缺失物理文件不阻断确认删除，但会写 `integrity_status=missing`。

关联接口另返回 `CLIENT_ACTOR_LINK_NOT_FOUND / CLIENT_CONTACT_ACTOR_ALREADY_LINKED / CLIENT_LINK_ACTOR_UNAVAILABLE / CLIENT_ACTOR_LINK_ALREADY_UNLINKED`；Actor 停用若仍承担活动客户联系人关系则返回 `ACTOR_HAS_ACTIVE_CLIENT_LINKS`。所有冲突均不产生半条 person、关联或解除事实。

### schema v10 聚合版本传播

```text
Project INSERT(client_id=A)       → Client A version +1
Project UPDATE(client_id=A→B)     → Client A version +1；Client B version +1
Project UPDATE(client_id=A→NULL)  → Client A version +1
Project DELETE(client_id=A)       → Client A version +1

Client PATCH(name changed)        → linked Project version +1
Client DELETE                     → linked Project version +1 + FK SET NULL
Invoice still references Client   → DELETE 409；Client/Project/version 全部不变
Attachment INSERT/soft delete     → Client version +1
Attachment integrity observation  → Client version 不变
Client DELETE                     → active attachment tombstone + trash + cascade；失败恢复文件
Client contact link/unlink         → Client version +1
Client DELETE                     → link history cascade；person Actor 保留
```

当前本地活动以独立 `client_activities` 聚合提供时间线，不复用 Task `workflow_events`。`system_reference` 只保留受约束数据边界，当前没有来源投影器；不得把外部互动写成已交付事实。

## 与其他模块协作

| 模块      | 当前协作方式                                                                                                    |
| --------- | --------------------------------------------------------------------------------------------------------------- |
| 项目      | 已实现 Project 可选关联、改绑、解除和列表筛选；Client 详情显式包含归档项目，从 Project 派生完整数量与分页列表。 |
| 任务      | 客户相关工作仍应通过 Project 或未来 Inbox 落为 Task；Client 本身不拥有执行状态。                                |
| Actor     | 联系人不会自动成为 person；owner 可显式关联已有 active person 或原子新建并关联。active 关联会阻止 person 停用。 |
| 收件箱    | 尚未消费 Client 事件；v0.4 回访与财务事件未来进入 Inbox。                                                       |
| 发票/财务 | Invoice 强引用删除约束已生效；业务 API、发票详情和收入聚合仍属 v0.4。                                           |
| 数据管理  | 客户附件复用受控 store；内部备份/演练/恢复包含 active objects，业务 JSON 仅导出元数据，不含文件正文。           |
| 今日      | 尚未显示客户回访；已存在的客户活动只在客户列表/详情展示，不自动生成 Today 工作项。                              |

总体依赖见[整体功能架构](../functional-architecture.md)。

## 分阶段实施

1. **v0.1 客户事实层（已实现）**：schema v10、Go model、CRUD、校验、分页/搜索/状态筛选、快照幂等、乐观锁、项目数聚合和删除约束。
2. **v0.1 前端纵切（已实现基础范围）**：列表、新建/编辑、基础详情、关联项目、Project 客户选择/筛选、加载/空/错误/重试与冲突草稿保留。
3. **v0.1 本地活动与附件（已实现）**：人工备注/会议、可追溯时间线、受控附件、安全下载、软删除审计和聚合删除补偿已交付；外部来源投影仍待实现，不接线上行为。
4. **v0.1 Actor 显式关联（已实现）**：已有 person / 原子新建二选一、单 active contact、聚合乐观锁、幂等重放、带原因解除、不可变历史和本地责任语义提示。
5. **v0.4 业务增强（待实现）**：回访计划、本地提醒、发票和财务聚合；第一版仍不自动对外发送。

## 验证与验收边界

当前自动化测试覆盖 schema v9→v10、v17→v18、v18→v19 与 v19→v20 数据保留、索引/约束/trigger/外键、Client CRUD/校验、分页/搜索/状态/排序、活动创建幂等、列表/详情/版本化编辑/软删除/删除历史/system reference 只读、附件严格上传/幂等/分页/下载完整性/软删/聚合硬删/崩溃恢复、已有/新建 person 关联、单 active contact、幂等/并发、带原因解除、Actor 停用保护与 Client 删除边界、聚合版本与最近活动传播、Project 关联传播、内部备份恢复与 Invoice 删除冲突。Web 侧覆盖 Client、Activity、Attachment、Actor Link API 规范化，附件上传预览/删除原因/历史、联系人关联/解除/历史，以及客户列表/详情和 Project 客户选择/筛选。

以下仍不能据此宣称完成：真实浏览器键盘/焦点/窄屏验收、大数据量分页性能、Activity 外部来源投影、多联系人/自定义关系、回访、财务或任何线上互动。

## 相关代码/PRD 链接

- [PRD：客户管理](../opc-workspace-PRD.md#54-客户管理)
- [PRD：T-09 客户管理](../opc-workspace-PRD.md#1049-t-09-客户管理)
- [整体功能架构](../functional-architecture.md)
- [Client API](../../services/sidecar/internal/api/clients.go)
- [Client model](../../services/sidecar/internal/models/client.go)
- [schema v10 迁移](../../services/sidecar/internal/database/migrations/010_client_facts.sql)
- [schema v18 活动迁移](../../services/sidecar/internal/database/migrations/018_client_activities.sql)
- [Client API 测试](../../services/sidecar/internal/api/clients_test.go)
- [Client Activity API](../../services/sidecar/internal/api/client_activities.go)
- [Client Activity API 测试](../../services/sidecar/internal/api/client_activities_test.go)
- [schema v19 附件迁移](../../services/sidecar/internal/database/migrations/019_client_attachments.sql)
- [schema v20 Client–Actor 关联迁移](../../services/sidecar/internal/database/migrations/020_client_actor_links.sql)
- [Client Attachment API](../../services/sidecar/internal/api/client_attachments.go)
- [Client Attachment API 测试](../../services/sidecar/internal/api/client_attachments_test.go)
- [客户附件界面](../../apps/web/src/components/ClientAttachmentsSection.tsx)
- [Client–Actor 关联 API](../../services/sidecar/internal/api/client_actor_links.go)
- [客户联系人关联界面](../../apps/web/src/components/ClientActorLinksSection.tsx)
- [Client 列表](../../apps/web/src/pages/ClientsPage.tsx)
- [Client 详情](../../apps/web/src/pages/ClientDetailPage.tsx)
- [Client 表单](../../apps/web/src/components/ClientFormModal.tsx)
- [Client Activity 时间线](../../apps/web/src/components/ClientActivitiesSection.tsx)
- [Project 表单](../../apps/web/src/components/ProjectFormModal.tsx)
