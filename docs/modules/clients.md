# 客户管理模块

> 实现基线：`HEAD 471f814`（2026-08-27）
>
> 版本边界：v0.1 只规划基础客户资料与关联；回访、发票和财务联动归入 v0.4。当前代码只有表与页面骨架。

## 定位与边界

Client 保存一人公司在本机维护的客户资料与业务关联，是项目、发票和后续回访的主数据，不是在线 CRM 或多人账号系统。

- 客户联系人不会自动成为 Actor。只有 owner 显式创建或关联 person Actor 后，联系人才能作为本地责任记录中的负责人。
- person Actor 不登录、不接收任务、不获得客户数据访问权；Client 与 Actor 必须保持不同语义。
- 客户累计收入只从 `confirmed` Financial Entry 聚合；paid Invoice 只用于关联与对账，不在客户表维护第二份金额。
- 客户活动仅来自用户手工记录或本地业务状态；未接入线上服务时不得显示邮件打开、回复、提案下载等虚假事件。
- v0.1 不自动发送邮件、短信、发票或其他外部消息。

## 当前实现状态

当前状态为**页面骨架**。

### 已实现

- SQLite `clients` 表，字段包含名称、联系人、邮箱、电话、备注、状态和时间戳。
- 当前状态约束为 `active / lead / inactive`。
- `projects.client_id` 为可选外键并使用 `ON DELETE SET NULL`；`invoices.client_id` 为必填外键并使用 `ON DELETE RESTRICT`。
- `/clients` 路由、页面标题、“新建客户”外观、固定 `0 个` 与空状态。

### 已知缺口

- 没有 Go Client model、API、校验、搜索、分页或测试。
- “新建客户”按钮没有行为；没有创建、编辑、停用或受约束删除。
- 没有客户表格、筛选、详情、项目/发票关联或真实活动时间线。
- 没有客户标签、附件、沟通笔记或累计收入聚合。
- 没有 `client_followups` 表、提醒或收件箱事件。
- 没有 Client 与 person Actor 的显式关联流程。

## 目标功能

### v0.1 基础客户资料

- 客户列表支持分页、搜索、按状态筛选，展示名称、联系人、项目数和最近本地活动。
- 支持创建、编辑、状态调整、停用和受约束删除。
- 对名称、邮箱、电话等字段执行服务端清洗与校验；可选字段保持可空语义。
- 客户详情展示基本资料、关联项目和可解释的本地活动时间线。
- 项目数从 Project 聚合；发票模块未交付前累计收入与发票区块明确标为不可用。

### 本地活动与附件

- 支持手工添加沟通笔记、会议记录和受控本地附件；记录创建者、时间和来源。
- 项目状态、付款确认等本地业务事件可进入活动时间线，但不得复制其业务状态。
- 删除或隐藏来源时保留最小可解释快照和审计，避免时间线出现无法说明的空记录。

### v0.4 回访与财务协作

- 回访包含计划时间、渠道、目的、状态、结果、下一步与下一次回访时间。
- 到期/逾期回访以稳定事件键生成 Inbox Item，可从今日或收件箱跳回客户详情。
- 发票和财务交付后，详情展示真实发票状态、已付款收入和聚合金额。
- 所有外发与付款确认仍由 owner 手动完成；本地 Agent 最多准备草稿，不改变客户或财务事实。

## 关键用户流程

### 建立客户并关联项目

1. owner 创建客户，填写名称和可选联系人信息。
2. Sidecar 校验并保存，客户进入 `active` 或 `lead`。
3. owner 新建/编辑项目时选择该客户。
4. 客户详情从 Project 聚合项目数和列表，不在 Client 记录中重复存储。

### 记录本地客户活动

1. owner 在客户详情添加沟通笔记、会议结果或附件。
2. Sidecar 保存本地活动和受控文件引用，写入 Workflow Event。
3. 客户时间线按时间展示，并可跳转到关联项目或发票。
4. 没有线上集成时，界面不提供“已打开邮件”等推测状态。

### 将联系人作为责任人

1. owner 从客户联系人显式选择“创建/关联本地责任人”。
2. 系统创建或关联 `person` Actor，并明确提示“仅本机记录，不会发送或同步”。
3. 后续任务通过 Assignment 分派给该 Actor；Client 本身不保存任务负责人状态。

### 回访提醒（v0.4）

1. owner 创建一次回访计划并指定本地负责人。
2. 到期调度器幂等生成 Inbox Item。
3. owner 在收件箱创建/关联 followup Task，线下完成后记录结果。
4. 需要下一次回访时创建新计划；历史记录不被覆盖。

## 数据/API/状态与事件

### 当前数据

- `clients(id, name, contact_name, email, phone, notes, status, created_at, updated_at)` 已存在。
- 当前只有表结构，没有 Client API 或前端数据请求。

### v0.1 规划数据与 API

- `client_activities`：`id`、`client_id`、`kind`（note/meeting/system_reference）、标题/正文、`occurred_at`、`created_by_actor_id`、可选来源类型/ID、`version`、`deleted_at` 和时间戳。只有 note/meeting 等人工记录可编辑；system_reference 只引用来源 Workflow Event，不复制或改写业务事实。
- `client_attachments`：`id`、`client_id`、可选 `activity_id`、受控相对路径、文件名、MIME、大小、SHA-256、录入 Actor、`deleted_at` 和时间戳。文件读取/删除通过 Sidecar，不能保存任意绝对路径。

- `GET/POST /api/v1/clients`：分页、关键词、状态筛选和创建。
- `GET/PATCH/DELETE /api/v1/clients/:id`：详情、编辑和受约束删除。
- `GET/POST /api/v1/clients/:id/activities`：查询和新增人工活动。
- `GET/PATCH/DELETE /api/v1/client-activities/:id`：编辑或软删除人工活动；系统引用不可编辑。
- `GET/POST /api/v1/clients/:id/attachments`：查询或导入受控附件。
- `GET/DELETE /api/v1/client-attachments/:id`：读取元数据/内容或软删除附件并写审计。
- 客户详情聚合项目数和活动；发票/收入字段在对应模块交付后再增加。
- 写操作支持幂等键和乐观锁；被发票等强引用时删除返回可解释冲突。

### v0.4 规划数据与 API

- 新增 `client_followups`，包含客户、计划时间、渠道、目的、状态、结果、下一步和完成时间。
- `GET/POST /api/v1/client-followups` 与单条读取/编辑/取消；支持到期、逾期、客户和负责人筛选。
- 回访持久状态：`planned / completed / skipped / cancelled`；`due / overdue` 由计划时间和用户时区派生，改期和完成保留历史事件。

### 事件规则

- v0.1 可记录 `client_created / client_updated / client_status_changed / client_activity_recorded`。
- v0.4 的 `client_followup_due` 使用稳定 `source_event_key` 创建 Inbox Item，跨扫描和重启不重复。
- 发票付款、项目状态等事件保留原模块为事实源，客户时间线只引用事件。

## 与其他模块协作

| 模块 | 协作方式 |
|------|----------|
| 项目 | Project 可关联 Client；客户详情从项目表派生项目数与列表。 |
| 任务 | 客户相关工作应落为 Task，通常通过项目或收件箱建立上下文。 |
| Actor | 联系人只有显式转换/关联后才成为 person；二者数据和权限不自动同步。 |
| 收件箱 | v0.4 承接回访、发票和本地客户事件的处理，不保存 Client 状态。 |
| 发票/财务 | Invoice 强引用客户；累计收入从已付款事实聚合。 |
| 今日 | 后续显示到期回访与真实本地活动，并跳转详情。 |

总体依赖见[整体功能架构](../functional-architecture.md)。

## 分阶段实施

1. **v0.1 客户事实层**：Go model、CRUD、校验、分页/搜索/状态筛选、乐观锁和删除约束测试。
2. **v0.1 前端纵切**：表格、新建/编辑、详情、关联项目、加载/空/错误/重试。
3. **v0.1 本地活动**：手工活动、附件、时间线与审计；不接线上行为。
4. **Actor 显式关联**：联系人转为/关联 person 的确认流程和语义提示。
5. **v0.4 业务增强**：回访计划、本地提醒、发票和财务聚合；第一版仍不自动对外发送。

## 验收标准

- 客户创建、编辑、分页、搜索、筛选和状态变更真实持久化。
- 邮箱、电话、可选字段和状态均有服务端校验；错误可定位并可重试。
- 项目数、发票和收入由关联表聚合，不在客户记录中手工维护重复事实。
- 有强引用的客户不能被无提示删除；停用或删除后的关联结果可解释。
- 联系人不会自动成为 Actor；显式关联时清楚提示不发送、不登录、不同步。
- 活动时间线只展示可追溯的本地记录，不伪造线上事件。
- v0.4 回访跨时区准确、重复扫描不重复、改期/完成不丢历史。
- 列表和详情覆盖加载、空、错误、重试、键盘和窄屏真实浏览器验收。

## 相关代码/PRD链接

- [PRD：客户管理](../opc-workspace-PRD.md#54-客户管理)
- [PRD：客户回访实施计划](../opc-workspace-PRD.md#10417-t-17-客户回访)
- [整体功能架构](../functional-architecture.md)
- [ClientsPage.tsx](../../apps/web/src/pages/ClientsPage.tsx)
- [初始数据库迁移](../../services/sidecar/internal/database/migrations/001_initial_schema.sql)
- [当前 API 路由](../../services/sidecar/internal/api/router.go)
