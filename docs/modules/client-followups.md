# 客户回访模块

> 目标版本：v0.4。该模块在基础客户 CRUD 完成后交付，不属于 v0.1。

## 定位与边界

客户回访用于在本机计划、提醒和记录下一次客户沟通，形成可追溯的关系维护时间线。它管理“何时需要跟进、为什么跟进、结果是什么”，不充当邮件、短信、CRM 同步或外呼平台。

- 所有计划、备注、结果和提醒只保存在本机。
- 渠道字段只是用户的本地记录，不代表应用已连接或操作该渠道。
- 第一版不自动发送邮件、短信、社交平台消息或任何外部请求。
- `person` 负责人只表示线下责任归属，不代表已向该人员发送或同步任务。
- AI 或本地 Agent 可生成建议草稿，但不能联系客户、伪造沟通结果或自动完成回访。

## 当前实现状态

- Client 基础资料 CRUD、列表、基础详情和 Project 客户关联已交付；客户页面当前明确显示没有本地活动事实，不伪造沟通记录。
- **C2 数据契约已完成**：schema v35 新增 `client_followups` 与 Go model；计划/完成/跳过/取消字段组合受数据库约束，负责人只允许 active owner/person，终态不可重开、写入必须递增 version，客户存在回访历史时禁止硬删除。插入、更新、删除会递增客户聚合版本，业务 JSON/ZIP 的显式白名单同步包含该表。
- **C3 计划 API 已完成**：`GET/POST /api/v1/client-followups`、`GET/PATCH /api/v1/client-followups/:id` 与 `GET /api/v1/clients/:id/followups` 已提供分页、客户/负责人/状态筛选、创建幂等、详情和 `If-Match` 编辑；成功创建或编辑同事务追加不可变 Workflow Event。API 使用 IANA 时区、RFC 3339 计划时间和 active owner/person 双层校验。
- **C4 执行 API 已完成**：`complete` 必填结果、`skip`/确认 `DELETE` 必填原因；`reschedule` 在同一事务取消旧计划、创建带 `rescheduled_from_id` 的新计划，并为两个聚合追加不可变 Workflow Event。终态不可重开。
- **C5 到期 Inbox 投影基础已完成**：Sidecar 启动与既有提醒扫描周期会读取到期的 `planned` 回访，以 `followup:<id>:due:<version>` 稳定键创建一条本地 Inbox 事件；重复扫描不重复创建，终态计划不会投影。今日工作台展示和客户详情时间线仍待页面纵切。
- 当前代码没有邮件、短信或第三方 CRM 连接；历史设计材料中的线上客户行为不能作为真实事件使用，且后续以 React 实现与 PRD 为准。

## 目标功能

- 为客户创建一次回访计划：时间、渠道、目的、备注、负责人和优先级。
- 按计划、到期、逾期、已完成和已跳过筛选，并支持分页和客户搜索。
- 完成时记录实际时间、结果、下一步和可选的下一次回访。
- 支持跳过和重新安排；重新安排保留原计划及变更历史。
- 在客户详情展示回访时间线，在今日和收件箱展示到期工作。
- 支持本地一次性提醒和稳定去重，应用未运行时在下次启动补偿扫描。
- 客户删除、停用或负责人停用时保持历史可解释。

## 关键用户流程

1. **创建计划**：用户从客户详情选择“安排回访”，填写本地日期时间、渠道、目的、负责人和备注后保存。
2. **到期提醒**：调度器按用户 IANA 时区计算到期范围，以稳定事件键创建 Inbox Item；同一次计划只提醒一次。
3. **处理回访**：用户从今日或收件箱跳转客户详情，查看上下文并在线下完成沟通。
4. **记录结果**：用户选择完成，填写结果、下一步和实际完成时间；需要持续跟进时原子创建下一次计划。
5. **跳过或重排**：用户填写原因后跳过，或选择新时间；系统保留原时间、原因和责任变化事件。
6. **异常恢复**：调度漏跑或应用重启后补偿扫描，但不重复生成提醒，不自动改变回访状态。

## 数据、API、状态与事件

### 数据

新增 `client_followups`，至少包含：

- `id`、`client_id`、`assigned_actor_id`
- `scheduled_at`、`timezone`、`channel`、`purpose`、`notes`
- `status`、`priority`、`result`、`next_step`
- `completed_at`、`skipped_at`、`skip_reason`
- 可选 `rescheduled_from_id` 或独立历史事件引用
- `version`、`created_at`、`updated_at`

`client_id` 使用明确删除策略；有未完成回访的客户默认禁止硬删除。负责人停用只能阻止新分派，不删除历史。

### API

- `GET / POST /api/v1/client-followups`
- `GET / PATCH / DELETE /api/v1/client-followups/:id`
- `POST /api/v1/client-followups/:id/complete`
- `POST /api/v1/client-followups/:id/skip`
- `POST /api/v1/client-followups/:id/reschedule`
- `GET /api/v1/clients/:id/followups`

列表支持客户、负责人、状态、到期范围、逾期和分页筛选。写入使用幂等键和乐观并发版本。

### 状态与事件

- 持久状态：`planned / completed / skipped / cancelled`。
- `due` 与 `overdue` 优先由 `planned + scheduled_at + 用户时区` 派生，避免时间流逝导致持久状态漂移。
- 事件示例：`client_followup.created`、`client_followup.rescheduled`、`client_followup.due`、`client_followup.completed`、`client_followup.skipped`。
- 到期事件通过 `followup:<id>:due:<计划版本>` 之类稳定键去重；重排后旧提醒被审计关闭，新计划产生新键。

## 与其他模块协作

- **客户**：客户详情是主要入口；回访只引用客户事实，不复制联系人资料。
- **Actor 与分派**：owner/person 表示本地负责人；person 不触发外部通知或账号权限。
- **今日与收件箱**：展示到期和逾期项，提供跳转、稍后、拆分任务与本地提醒。
- **任务**：复杂跟进可显式转换或关联 Task；完成 Task 不自动宣称客户已被联系。
- **自动化**：只允许根据本地回访事件创建 Inbox Item、Task 或提醒，不允许发送消息。
- **知识库/AI**：只有用户显式选择的客户备注或回访记录可作为本地上下文；模型输出仍是建议。

## 分阶段实施

1. **C1 前置依赖（已完成）**：客户基础 CRUD/详情、Client 活动事实、person 显式关联、Actor/Assignment 基线和 Inbox 人工闭环已交付；回访仍需自己的递增迁移与领域契约。
2. **C2 数据契约（已完成）**：新增迁移、计划/终态字段组合、负责人和删除约束、版本步进、客户聚合失效，以及业务导入导出白名单。
3. **C3 CRUD（计划部分已完成）**：已实现列表、创建、编辑、详情、分页、时区口径、幂等/并发和定向 API 测试；创建/编辑成功后追加 Workflow Event。取消与终态执行转入 C4。
4. **C4 执行闭环（API 已完成）**：已实现完成、跳过、取消和重排的事务 API；下一次计划向导和客户时间线转入页面纵切。
5. **C5 提醒协作（Inbox 基础已完成）**：已接入本地调度、启动补偿、Inbox 和事件去重；Today 展示、详情时间线和 Inbox 反向执行入口仍待页面纵切。
6. **C6 稳定性**：覆盖时区、夏令时、并发编辑、客户/Actor 停用和恢复测试。

## 验收标准

- 计划、完成、跳过、重排及下一次计划均可追踪，历史不会被覆盖。
- `due/overdue` 在用户时区、跨午夜和夏令时边界计算正确。
- 调度重扫、应用重启和请求重试不会产生重复提醒或重复下一次计划。
- 完成必须记录结果；跳过必须记录原因；重排保留原计划和审计事件。
- 有活动回访时客户删除约束明确，客户或负责人停用不破坏历史。
- 今日、收件箱和客户详情显示同一事实，跳转目标可定位。
- 加载、空、错误、重试、分页和并发冲突均有前后端测试。
- 断网时全部功能可用，并经检查确认不存在邮件、短信或第三方消息自动发送路径。

## 相关 PRD 与代码链接

- [产品 PRD](../opc-workspace-PRD.md)（§5.4、§10.4.17、§10.7、附录 C）
- [客户页](../../apps/web/src/pages/ClientsPage.tsx)
- [回访迁移](../../services/sidecar/internal/database/migrations/035_client_followups.sql)
- [回访迁移测试](../../services/sidecar/internal/database/client_followup_migration_test.go)
- [Sidecar 路由](../../services/sidecar/internal/api/router.go)
- [前端路由](../../apps/web/src/App.tsx)
