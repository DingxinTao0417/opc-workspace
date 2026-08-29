# 本地提醒模块

> 当前基线：app v0.1.0 / API v1 / SQLite schema v33（2026-08-29）；schema v32 在既有一次性 Reminder 上追加每日/每周重复系列和 occurrence 事实，schema v33 的预设自动化可创建来源为 automation 的一次性 Reminder，但不改变 Reminder 表。
>
> 版本边界：T-11A3 一次性本地 Reminder 及每日/每周重复规则已交付；预设“每日 Today 提醒”和“每周回顾提醒”已能按 IANA 时区创建本地一次性 Reminder。每月/自定义日历规则、系统原生通知、远程推送、邮件/短信、自由业务规则和用户可配置扫描频率仍未实现。

导航：[文档中心](../README.md) · [整体功能架构](../functional-architecture.md) · [PRD v9.23](../opc-workspace-PRD.md) · [收件箱](inbox.md) · [预设自动化](automation.md) · [桌面平台](desktop-platform.md)

## 定位与事实边界

Reminder 是“某个提醒系列中的一次时间调度事实”，Inbox Item 是该次提醒到期后的“待处理事实”。一次性提醒只有一个 occurrence；重复提醒每次到期后追加一条新的 scheduled occurrence，不复用或重置已 fired 的旧行。两者使用独立状态，不因为用户解决、忽略或稍后处理 Inbox Item 而改写已触发的 Reminder。

当前模块只负责：

- 由 owner 创建未来某一时刻触发的一次性、按天或按周重复的本地提醒，或由已启用的受限预设自动化创建下一条一次性提醒；
- 在提醒仍为 scheduled 时编辑标题、摘要、优先级、触发时间和重复规则，或带原因取消当前唯一的后续 occurrence；
- Sidecar 启动时补扫、运行中定时扫描到期提醒；
- 以 occurrence 专属稳定事件键在同一 SQLite 事务中生成一个 Reminder 类型 Inbox Item；重复系列同时创建下一条 scheduled occurrence；
- 记录创建、编辑、取消和触发的追加式 Workflow Event。

当前不负责系统通知中心、托盘角标、声音、每月/复杂日历规则、第三方 API、跨设备同步或自动联系客户。应用关闭期间不会在后台唤醒；下次 Sidecar 启动会补偿已经到期但尚未触发的 occurrence。若离线跨过多个重复周期，只生成一条当前到期 Inbox Item，并把下一次推进到服务端当前时间之后，避免启动刷屏。

## 用户流程与界面

1. 用户在收件箱页点击“本地提醒”，打开提醒管理器。
2. 左侧菜单在待提醒、已触发、已取消三个模块间切换；列表支持搜索、分页、加载、错误和空状态。
3. 用户创建提醒时填写标题、可选摘要、优先级和本地日期时间，并选择不重复、按天或按周；重复间隔为 1–365，使用浏览器提供的稳定 IANA 时区。前端把首个时刻转换为 RFC 3339 UTC，服务端以自己的当前时间再次校验必须在未来，并验证时区。
4. scheduled 提醒可直接编辑或重新安排；修改重复规则作用于当前尚未触发的 occurrence 及其之后生成的 occurrence。点击保存时使用当前 ETag。版本冲突时保留本地草稿，用户可明确加载最新事实后再处理。
5. scheduled 提醒可输入 1–1,000 字符原因取消。重复提醒同时只存在一个 scheduled occurrence，因此取消即停止该系列后续生成；历史 fired/cancelled occurrence 不删除。
6. fired 提醒显示触发时间、系列 occurrence 序号和生成的收件箱条目，可跳转到对应 Inbox Item；cancelled 提醒显示取消时间与原因。

管理弹窗只在打开时挂载，避免后台无意义请求。列表每 15 秒轮询以反映 Sidecar 扫描结果；轮询只是读取，是否到期始终由服务端时钟决定。

## 数据契约

schema v14 的 `014_reminders.sql` 新增 `reminders`，schema v32 的 `032_recurring_reminders.sql` 追加系列字段与约束：

| 字段                                                   | 说明                                                                    |
| ------------------------------------------------------ | ----------------------------------------------------------------------- |
| `id`                                                   | UUID 主键                                                               |
| `source_entity_type / source_entity_id`                | 公开创建固定为 `manual / null`；预设调度器使用 `automation / <rule-id>` |
| `title / summary / priority`                           | 标题 2–200 字符，摘要最多 10,000 字符，优先级 P0–P3                     |
| `trigger_at`                                           | RFC 3339 UTC，一次性触发时间                                            |
| `status`                                               | `scheduled / fired / cancelled`                                         |
| `source_event_key`                                     | 创建时生成稳定唯一键 `reminder:<id>:due`                                |
| `created_by_actor_id`                                  | 当前固定内置 owner                                                      |
| `series_id`                                            | 系列稳定 ID；首个 occurrence 等于自己的 Reminder ID                     |
| `recurrence_type / recurrence_interval`                | `none / daily / weekly` 与 1–365 间隔                                   |
| `recurrence_timezone`                                  | 重复规则使用的稳定 IANA 时区；一次性固定 `UTC`                          |
| `occurrence_number`                                    | 系列序号；离线跳过周期时按实际日历步数递增                              |
| `fired_at / inbox_item_id`                             | fired 时成组出现并引用唯一 Inbox Item                                   |
| `cancelled_by_actor_id / cancelled_at / cancel_reason` | cancelled 时成组出现，原因非空                                          |
| `version / created_at / updated_at`                    | 乐观并发版本和 UTC 时间戳                                               |

数据库约束和 trigger 共同保证：

- scheduled 不得携带 fired/cancelled 终态字段；fired 与 cancelled 的字段必须分别完整成组；
- manual 来源不得伪造 `source_entity_id`；`source_event_key` 全局唯一且身份字段不可修改；
- `series_id + occurrence_number` 唯一，同一系列最多一个 scheduled occurrence；系列 ID 和 occurrence 序号不可修改；
- `none` 固定 interval 1/UTC；daily/weekly interval 为 1–365，服务端 API 和业务导入预检必须验证 IANA 时区；
- fired 必须引用来源和事件键均匹配的 Reminder Inbox Item；
- fired/cancelled 为不可变终态，Reminder 不允许硬删除；
- 创建者和取消者必须是有效 Actor，当前公开 API 只使用内置 owner，触发事件由内置 system 记录。

schema v32 为普通加法迁移：既有 Reminder 幂等回填 `series_id=id / recurrence_type=none / interval=1 / timezone=UTC / occurrence=1`，不创建 demo Reminder，也不改变其标题、状态、触发时间、Inbox 引用或历史事件。schema v33 新增 Automation Rule/Run 表，未启用规则不会创建 Reminder；后续迁移必须从 `034_*` 继续。

## API 契约

| 方法   | 路径                    | 当前行为                                                                                            |
| ------ | ----------------------- | --------------------------------------------------------------------------------------------------- |
| GET    | `/api/v1/reminders`     | `page/page_size` 分页；支持 `q`、`status` 和白名单 `sort`；所有排序追加 `id ASC`，返回 `server_now` |
| POST   | `/api/v1/reminders`     | 创建 future scheduled Reminder；可带重复三字段；支持 `Idempotency-Key` 和首次 201 快照              |
| GET    | `/api/v1/reminders/:id` | 返回完整事实、`available_actions` 和 ETag                                                           |
| PATCH  | `/api/v1/reminders/:id` | 仅 scheduled 可编辑；强制 `If-Match`，旧版本返回 409                                                |
| DELETE | `/api/v1/reminders/:id` | 带 `{reason}` 的软取消；强制 `If-Match`，支持幂等快照；不提供硬删除                                 |

列表状态只接受 `scheduled / fired / cancelled`。排序字段只接受 `trigger_at / created_at / updated_at / priority / title` 及升降序。严格 JSON 会拒绝未知字段、重复 JSON、空请求和多余尾随内容。创建、编辑或重新安排的 `trigger_at` 必须晚于服务端当前时间。重复三字段必须作为一致契约提交：一次性为 `none/1/UTC`，重复为 `daily|weekly / 1..365 / IANA timezone`。

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
  → 若 daily/weekly：按 IANA 当地日历推进到 server_now 之后
  → 创建同 series_id 的下一条 scheduled occurrence
  → 写 reminder_recurrence_scheduled Event（system）
```

Inbox Item 继承 Reminder 的标题、摘要、优先级和触发时间，`source_entity_type=reminder`、`source_entity_id=<occurrence reminder-id>`、`resolution_policy=manual`。唯一 `source_event_key`、系列部分唯一索引、Reminder 条件更新和事务回滚共同保证重复扫描、重启及已存在投影不会生成第二条 Inbox Item、下一 occurrence 或重复事件。下一时刻使用 IANA 当地日历 `AddDate`，跨 DST 保持当地钟点；离线漏过的周期增加 occurrence 序号但不逐条创建历史空壳。任何中间写入失败都会回滚 Reminder、Inbox、下一 occurrence 和事件事实。

## 验证与代码证据

当前自动化覆盖：

- v13→v14 既有业务事实保留、零 demo 数据、约束、外键和不可变终态；
- CRUD、严格校验、分页/搜索/排序、ETag 冲突、幂等创建/取消；
- 重复扫描、未来/取消提醒忽略、系统事件、事务故障回滚与重启补偿；
- schema v31→v32 一次性事实保留、系列/occurrence 唯一约束、daily/weekly API、DST 当地钟点、离线跳过积压和重复扫描不重复生成下一 occurrence；
- 业务导入对篡改 recurrence timezone 的预检拒绝；
- 前端 API 规范化、Query/Mutation 缓存行为、创建/编辑/取消、冲突草稿和终态跳转；
- Reminder Inbox 投影的一致性校验。
- 预设自动化的 IANA/DST 预览、离线折叠、稳定来源键和失败重试不会重复创建 Reminder。

相关代码：

- [schema v14 迁移](../../services/sidecar/internal/database/migrations/014_reminders.sql)
- [schema v32 重复提醒迁移](../../services/sidecar/internal/database/migrations/032_recurring_reminders.sql)
- [Reminder API 与调度投影](../../services/sidecar/internal/api/reminders.go)
- [Reminder API 测试](../../services/sidecar/internal/api/reminders_test.go)
- [Reminder 迁移测试](../../services/sidecar/internal/database/reminder_migration_test.go)
- [前端提醒管理器](../../apps/web/src/components/ReminderManagerModal.tsx)
- [前端 Reminder API](../../apps/web/src/api/client.ts)
- [预设自动化引擎](../../services/sidecar/internal/api/automation_engine.go)

## 后续范围

- 原生系统通知、托盘/角标、声音及 DND 引导；
- 每月、工作日、自定义日历规则、系列批量改期和单次例外；
- Task、Project、Client 回访、发票等自由业务来源自动创建 Reminder；当前仅开放两个固定日历预设；
- Reminder 到期后已自然进入 Today/Sidebar 的 Inbox 派生计数；独立“待提醒/即将提醒”计数仍未实现；
- 用户可配置扫描频率、产品化历史清理或导出；
- 任何远程推送、邮件、短信、第三方日历或云同步。
