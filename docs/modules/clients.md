# 客户管理模块

> 实现状态截止：2026-08-27（依据当前实现）
>
> 当前基线：app v0.1.0 / API v1 / SQLite schema v12。客户事实仍由 schema v10 引入；schema v12 只新增独立 Inbox Item，不改 Client 结构。v0.1 的基础资料 CRUD、基础详情和 Project 客户关联已交付，模块仍为**部分完成**；活动、附件、Actor 显式关联、回访和财务聚合尚未交付。

## 定位与边界

Client 保存一人公司在本机维护的客户资料与业务关联，是 Project、Invoice 和后续回访的主数据，不是在线 CRM 或多人账号系统。

- 客户联系人不会自动成为 Actor。只有 owner 显式创建或关联 person Actor 后，联系人才能作为本地责任记录中的负责人；该转换流程尚未实现。
- person Actor 不登录、不接收远程消息、不获得客户数据访问权；Client 与 Actor 保持不同语义。
- `project_count` 从 Project 实时派生，不写回 `clients`。
- 客户累计收入未来只从 `confirmed` Financial Entry 聚合；paid Invoice 只用于关联与对账，不在客户表维护第二份金额。
- 当前没有客户活动事实。界面显示“暂无本地活动”，不会把 `updated_at` 当作互动，也不会伪造邮件打开、回复或提案下载。
- v0.1 不自动发送邮件、短信、发票或其他外部消息。

## 当前实现状态

当前状态为**部分完成**，已经具备可运行的客户资料事实层、列表、基础详情和 Project 客户关联。

### 已实现：数据与 Sidecar

- schema v10 通过递增迁移为 `clients` 增加从 1 开始的 `version`，并增加名称、状态和更新时间查询索引；旧数据中的空白可选字段归一为 `NULL`。
- Go Client model 与以下 API 已实现：
  - `GET/POST /api/v1/clients`
  - `GET/PATCH/DELETE /api/v1/clients/:id`
- 列表支持分页、关键词、状态筛选和白名单排序。`q` 搜索名称、联系人、邮箱和电话，并转义 SQL `LIKE` 中的 `%`、`_` 与反斜杠。
- Client 响应包含 `project_count`、`version` 和 RFC 3339 UTC 时间戳；列表和详情都从 Project 实时聚合项目数。
- 创建支持可选 `Idempotency-Key`。服务端对清洗后的规范请求保存 SHA-256 与首次 `201` 响应快照；同键同请求稳定重放且不重复创建，同键异体返回 `409 IDEMPOTENCY_CONFLICT`。
- 创建、详情和更新响应返回 `ETag`；PATCH 与 DELETE 强制 `If-Match`。缺少版本返回 428，非法版本返回 400，旧版本返回 `409 VERSION_CONFLICT`。
- Project 插入、删除或 `client_id` 改绑会原子递增受影响 Client 的 `version` 与 `updated_at`，避免用户基于旧 `project_count` 编辑或永久删除。
- 保留既有反向传播：Client 名称改变或永久删除会递增关联 Project 的聚合版本，使 Project 的 `client_name` 快照失效。
- 永久删除使用 `DELETE /api/v1/clients/:id?confirm=true`，只允许 `inactive` Client，并再次校验最新 `If-Match`。Project 是可选关联，删除后由外键 `ON DELETE SET NULL` 保留项目并返回 `detached_projects`；Invoice 是强引用，存在时返回 `409 CLIENT_HAS_INVOICES`，客户、Project 关联和双方版本均不改变。

### 已实现：Web 纵切

- `/clients` 使用真实 API 展示总数、紧凑表格、名称首字符头像、联系人、真实项目数、状态和分页；服务端搜索与状态筛选变化时回到第 1 页。
- 列表覆盖加载 skeleton、首次空状态、筛选无结果、错误和重试。累计收入固定显示“v0.4 后可用”，最近动态固定显示“暂无本地活动”。
- 新建/编辑表单覆盖名称、联系人、邮箱、电话、备注和状态；空白可选值提交为 `null`，保存中禁止关闭和重复提交。
- 编辑发生版本冲突时刷新最新 Client 版本，但保留用户未提交草稿并要求重新确认。
- `/clients/:id` 展示基础资料、真实关联项目列表及分页、状态调整和危险区。活动/回访、发票/收入区明确标为后续版本，不展示模拟业务数据。
- 停用或恢复只更新 Client 状态，不解除 Project。永久删除前必须先停用并二次确认，界面展示将解除的项目数；成功后回到客户列表并显示实际解除数量。
- Project 新建/编辑表单已接真实客户选择器，可关联、改绑或选择“不关联客户”；选项拉全分页结果并标注 inactive Client。客户选项加载失败时可重试，编辑已有 Project 时保留当前关联，不会静默提交 `null`。
- 项目列表已支持按 Client 筛选；Client 创建、编辑、删除和 Project 关联变化会刷新相关 Client/Project 查询缓存。

### 已知缺口

- 没有 `client_activities`、沟通笔记、会议记录、客户 Workflow Event 时间线或“最近动态”事实源。
- 没有 `client_attachments` 或受控客户文件目录。
- 没有客户标签、去重合并、批量操作或导入导出。
- 没有 Client 与 person Actor 的显式创建/关联流程。
- 没有发票详情、累计收入或 Financial Entry 聚合；这些能力归入 v0.4。
- 没有 `client_followups`、提醒、今日或 Inbox 联动；回访归入 v0.4。
- 仍需真实浏览器完成键盘焦点、窄屏表格和大客户量分页性能验收。

## 当前用户流程

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

名称、联系人、邮箱和电话拒绝控制字符。服务端是最终校验边界；未知 JSON 字段、无可编辑字段的 PATCH 和不合法状态均被拒绝。

### 当前列表契约

- `page` 默认 1，`page_size` 默认 50、最大 100。
- `q` trim 后最多 200 字符，搜索 `name / contact_name / email / phone`。
- `status` 只接受 `active / lead / inactive`；未传时包含全部状态。
- `sort` 支持逗号分隔和 `-` 降序，白名单为 `name / contact_name / status / project_count / created_at / updated_at`。
- 默认 `updated_at DESC`；所有排序最后追加 `id ASC`，保证跨分页稳定。
- 返回 `{ "data": [...], "meta": { "page", "page_size", "total" } }`。

### 当前 API

| 方法   | 路径                                                   | 当前行为                                                                                                          |
| ------ | ------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------- |
| GET    | `/api/v1/clients`                                      | 分页、关键词、状态筛选和稳定白名单排序；返回 `project_count`                                                      |
| POST   | `/api/v1/clients`                                      | 创建 Client，默认 active；可选 `Idempotency-Key` 保存规范请求与首次响应快照；返回 `201` 和 `ETag`                 |
| GET    | `/api/v1/clients/:id`                                  | 返回完整基本资料、`project_count` 与 `ETag`                                                                       |
| PATCH  | `/api/v1/clients/:id`                                  | 部分更新资料或状态；可选字段可显式 `null`；强制 `If-Match`，成功递增版本                                          |
| DELETE | `/api/v1/clients/:id?confirm=true`                     | 强制 `If-Match`；只允许 inactive；Invoice 强引用阻止删除，Project 外键置空并返回 `deleted_id / detached_projects` |
| GET    | `/api/v1/projects?client_id=:id&include_archived=true` | 查询 Client 的完整关联项目历史；普通项目列表仍默认排除归档项，项目新建/编辑支持关联、改绑或解除 Client            |

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

### schema v10 聚合版本传播

```text
Project INSERT(client_id=A)       → Client A version +1
Project UPDATE(client_id=A→B)     → Client A version +1；Client B version +1
Project UPDATE(client_id=A→NULL)  → Client A version +1
Project DELETE(client_id=A)       → Client A version +1

Client PATCH(name changed)        → linked Project version +1
Client DELETE                     → linked Project version +1 + FK SET NULL
Invoice still references Client   → DELETE 409；Client/Project/version 全部不变
```

当前没有 Client Workflow Event 生产器；`client_created / client_updated / client_status_changed / client_activity_recorded` 仍是后续事件契约，不得写成已交付时间线。

## 与其他模块协作

| 模块      | 当前协作方式                                                                                                    |
| --------- | --------------------------------------------------------------------------------------------------------------- |
| 项目      | 已实现 Project 可选关联、改绑、解除和列表筛选；Client 详情显式包含归档项目，从 Project 派生完整数量与分页列表。 |
| 任务      | 客户相关工作仍应通过 Project 或未来 Inbox 落为 Task；Client 本身不拥有执行状态。                                |
| Actor     | 联系人不会自动成为 person；显式转换/关联流程尚未实现。                                                          |
| 收件箱    | 尚未消费 Client 事件；v0.4 回访与财务事件未来进入 Inbox。                                                       |
| 发票/财务 | Invoice 强引用删除约束已生效；业务 API、发票详情和收入聚合仍属 v0.4。                                           |
| 今日      | 尚未显示客户回访或活动；等待真实 Reminder/Activity 事实。                                                       |

总体依赖见[整体功能架构](../functional-architecture.md)。

## 分阶段实施

1. **v0.1 客户事实层（已实现）**：schema v10、Go model、CRUD、校验、分页/搜索/状态筛选、快照幂等、乐观锁、项目数聚合和删除约束。
2. **v0.1 前端纵切（已实现基础范围）**：列表、新建/编辑、基础详情、关联项目、Project 客户选择/筛选、加载/空/错误/重试与冲突草稿保留。
3. **v0.1 本地活动与附件（待实现）**：人工活动、受控附件、可追溯时间线和审计；不接线上行为。
4. **Actor 显式关联（待实现）**：联系人转为/关联 person 的确认流程和语义提示。
5. **v0.4 业务增强（待实现）**：回访计划、本地提醒、发票和财务聚合；第一版仍不自动对外发送。

## 验证与验收边界

当前自动化测试覆盖 schema v9→v10 数据保留与空值归一、索引和外键、Client CRUD/校验、分页/搜索/状态/排序、创建幂等快照、旧版本 PATCH/DELETE、Project 关联版本传播、Invoice 删除冲突无副作用，以及 Client 改名/删除后的 Project 版本传播。现有 Go 回归与静态检查通过；Web 侧覆盖 Client API 规范化、Query hooks、表单、列表、详情和 Project 客户选择/筛选的组件测试。

以下仍不能据此宣称完成：真实浏览器键盘/焦点/窄屏验收、大数据量分页性能、Activity/Attachment、Actor 关联、回访、财务或任何线上互动。

## 相关代码/PRD 链接

- [PRD：客户管理](../opc-workspace-PRD.md#54-客户管理)
- [PRD：T-09 客户管理](../opc-workspace-PRD.md#1049-t-09-客户管理)
- [整体功能架构](../functional-architecture.md)
- [Client API](../../services/sidecar/internal/api/clients.go)
- [Client model](../../services/sidecar/internal/models/client.go)
- [schema v10 迁移](../../services/sidecar/internal/database/migrations/010_client_facts.sql)
- [Client API 测试](../../services/sidecar/internal/api/clients_test.go)
- [Client 列表](../../apps/web/src/pages/ClientsPage.tsx)
- [Client 详情](../../apps/web/src/pages/ClientDetailPage.tsx)
- [Client 表单](../../apps/web/src/components/ClientFormModal.tsx)
- [Project 表单](../../apps/web/src/components/ProjectFormModal.tsx)
