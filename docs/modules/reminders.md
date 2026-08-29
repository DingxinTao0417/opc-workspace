# 本地提醒模块

> 当前基线：app v0.1.0 / API v1 / SQLite schema v29（2026-08-28）；schema v28 不改变 Reminder 契约。
>
> 版本边界：T-11A3 一次性本地 Reminder 已交付。重复提醒、系统原生通知、远程推送、邮件/短信、业务来源自动建提醒和用户可配置扫描频率仍未实现。

导航：[文档中心](../README.md) · [整体功能架构](../functional-architecture.md) · [PRD v9.8](../opc-workspace-PRD.md) · [收件箱](inbox.md) · [桌面平台](desktop-platform.md)

## 定位与事实边界

Reminder 是“一次性时间调度事实”，Inbox Item 是提醒到期后的“待处理事实”。两者使用独立状态，不因为用户解决、忽略或稍后处理 Inbox Item 而改写已触发的 Reminder。

当前模块只负责：

- 由 owner 创建未来某一时刻触发的一次性本地提醒；
- 在提醒仍为 scheduled 时编辑标题、摘要、优先级和触发时间，或带原因取消；
- Sidecar 启动时补扫、运行中定时扫描到期提醒；
- 以稳定事件键在同一 SQLite 事务中生成一个 Reminder 类型 Inbox Item；
- 记录创建、编辑、取消和触发的追加式 Workflow Event。

当前不负责系统通知中心、托盘角标、声音、重复日程、时区日历规则、第三方 API、跨设备同步或自动联系客户。应用关闭期间不会在后台唤醒；下次 Sidecar 启动会补偿已经到期但尚未触发的记录。

## 用户流程与界面

1. 用户在收件箱页点击“本地提醒”，打开提醒管理器。
2. 左侧菜单在待提醒、已触发、已取消三个模块间切换；列表支持搜索、分页、加载、错误和空状态。
3. 用户创建提醒时填写标题、可选摘要、优先级和本地日期时间。前端转换为 RFC 3339 UTC，服务端以自己的当前时间再次校验必须在未来。
4. scheduled 提醒可直接编辑或重新安排；点击保存时使用当前 ETag。版本冲突时保留本地草稿，用户可明确加载最新事实后再处理。
5. scheduled 提醒可输入 1–1,000 字符原因取消。取消是终态，不执行硬删除。
6. fired 提醒显示触发时间和生成的收件箱条目，可跳转到对应 Inbox Item；cancelled 提醒显示取消时间与原因。

管理弹窗只在打开时挂载，避免后台无意义请求。列表每 15 秒轮询以反映 Sidecar 扫描结果；轮询只是读取，是否到期始终由服务端时钟决定。

## 数据契约

schema v14 的 `014_reminders.sql` 新增 `reminders`：

| 字段                                                   | 说明                                                         |
| ------------------------------------------------------ | ------------------------------------------------------------ |
| `id`                                                   | UUID 主键                                                    |
| `source_entity_type / source_entity_id`                | 当前公开创建固定为 `manual / null`；业务来源接入留待独立纵切 |
| `title / summary / priority`                           | 标题 2–200 字符，摘要最多 10,000 字符，优先级 P0–P3          |
| `trigger_at`                                           | RFC 3339 UTC，一次性触发时间                                 |
| `status`                                               | `scheduled / fired / cancelled`                              |
| `source_event_key`                                     | 创建时生成稳定唯一键 `reminder:<id>:due`                     |
| `created_by_actor_id`                                  | 当前固定内置 owner                                           |
| `fired_at / inbox_item_id`                             | fired 时成组出现并引用唯一 Inbox Item                        |
| `cancelled_by_actor_id / cancelled_at / cancel_reason` | cancelled 时成组出现，原因非空                               |
| `version / created_at / updated_at`                    | 乐观并发版本和 UTC 时间戳                                    |

数据库约束和 trigger 共同保证：

- scheduled 不得携带 fired/cancelled 终态字段；fired 与 cancelled 的字段必须分别完整成组；
- manual 来源不得伪造 `source_entity_id`；`source_event_key` 全局唯一且身份字段不可修改；
- fired 必须引用来源和事件键均匹配的 Reminder Inbox Item；
- fired/cancelled 为不可变终态，Reminder 不允许硬删除；
- 创建者和取消者必须是有效 Actor，当前公开 API 只使用内置 owner，触发事件由内置 system 记录。

迁移为纯加法，不创建 demo Reminder，也不改写 schema v13 的 Task、Inbox、Client、Project、Focus 或 Artifact 事实。其后的 schema v15–v29 增加 Inbox 编排、app_settings、任务保存视图、客户/项目扩展、Workspace Avatar、存储设置以及 Artifact/Task/Project/系统维护 Inbox 来源；v23–v29 不改变 Reminder 契约，后续迁移必须从 `030_*` 继续。

## API 契约

| 方法   | 路径                    | 当前行为                                                                                            |
| ------ | ----------------------- | --------------------------------------------------------------------------------------------------- |
| GET    | `/api/v1/reminders`     | `page/page_size` 分页；支持 `q`、`status` 和白名单 `sort`；所有排序追加 `id ASC`，返回 `server_now` |
| POST   | `/api/v1/reminders`     | 创建 future scheduled Reminder；支持 `Idempotency-Key`，保存规范化请求摘要和首次 201 快照           |
| GET    | `/api/v1/reminders/:id` | 返回完整事实、`available_actions` 和 ETag                                                           |
| PATCH  | `/api/v1/reminders/:id` | 仅 scheduled 可编辑；强制 `If-Match`，旧版本返回 409                                                |
| DELETE | `/api/v1/reminders/:id` | 带 `{reason}` 的软取消；强制 `If-Match`，支持幂等快照；不提供硬删除                                 |

列表状态只接受 `scheduled / fired / cancelled`。排序字段只接受 `trigger_at / created_at / updated_at / priority / title` 及升降序。严格 JSON 会拒绝未知字段、重复 JSON、空请求和多余尾随内容。创建、编辑或重新安排的 `trigger_at` 必须晚于服务端当前时间。

幂等创建和取消在版本检查前重放同键同请求的首次响应；同一键配不同规范化请求返回 `409 IDEMPOTENCY_CONFLICT`。PATCH 不保存幂等快照，使用 `If-Match` 保护。终态修改或取消返回 `409 REMINDER_TERMINAL`。

## 调度、事务与幂等

Sidecar 创建路由器时先同步执行一次到期投影；失败会使启动失败，不能静默跳过。启动成功后每 15 秒扫描，单批最多处理 100 条，按 `trigger_at/id` 稳定顺序执行。大量积压由后续 tick 继续处理。

每个到期 Reminder 在一个事务内完成：

```text
Reminder(scheduled, trigger_at <= server_now)
  → 按 source_event_key 查找或创建 Inbox Item(kind=reminder, status=open)
  → 写 Inbox created Event（system）
  → Reminder 改为 fired 并记录 inbox_item_id
  → 写 reminder_fired Event（system）
```

Inbox Item 继承 Reminder 的标题、摘要、优先级和触发时间，`source_entity_type=reminder`、`source_entity_id=<reminder-id>`、`resolution_policy=manual`。唯一 `source_event_key`、Reminder 条件更新和事务回滚共同保证重复扫描、重启及已存在投影不会生成第二条 Inbox Item 或重复事件。任何中间写入失败都会回滚 Reminder、Inbox 和事件事实。

## 验证与代码证据

当前自动化覆盖：

- v13→v14 既有业务事实保留、零 demo 数据、约束、外键和不可变终态；
- CRUD、严格校验、分页/搜索/排序、ETag 冲突、幂等创建/取消；
- 重复扫描、未来/取消提醒忽略、系统事件、事务故障回滚与重启补偿；
- 前端 API 规范化、Query/Mutation 缓存行为、创建/编辑/取消、冲突草稿和终态跳转；
- Reminder Inbox 投影的一致性校验。

相关代码：

- [schema v14 迁移](../../services/sidecar/internal/database/migrations/014_reminders.sql)
- [Reminder API 与调度投影](../../services/sidecar/internal/api/reminders.go)
- [Reminder API 测试](../../services/sidecar/internal/api/reminders_test.go)
- [Reminder 迁移测试](../../services/sidecar/internal/database/reminder_migration_test.go)
- [前端提醒管理器](../../apps/web/src/components/ReminderManagerModal.tsx)
- [前端 Reminder API](../../apps/web/src/api/client.ts)

## 后续范围

- 原生系统通知、托盘/角标、声音及 DND 引导；
- 重复提醒、日历/时区规则和批量改期；
- Task、Project、Client 回访、发票等业务来源自动创建 Reminder；
- Reminder 到期后已自然进入 Today/Sidebar 的 Inbox 派生计数；独立“待提醒/即将提醒”计数仍未实现；
- 用户可配置扫描频率、产品化历史清理或导出；
- 任何远程推送、邮件、短信、第三方日历或云同步。
