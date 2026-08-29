# 专注与工时模块

> 当前基线：app v0.1.0 / API v1 / SQLite schema v28（2026-08-28）。Focus 结构仍由 schema v11 引入；schema v12–v28 不改 Focus 表契约。Focus Core v0.1-A/B/C、v0.1-D1（历史与周期报告）、D2a（Task 详情记录），以及 D2b 的本地日期范围回顾与项目时间分布已经交付；标签分布、热力图、最佳时段和原生桌面反馈仍属后续。

## 定位与边界

本模块把“选择任务、进入专注、暂停或恢复、结束并累计工时”连接成一个本地闭环，并为今日统计和项目工时聚合提供可信事实。

核心边界：

- SQLite Focus Session 是工作阶段运行态的唯一事实源；前端 ticker 只根据服务端快照和绝对时间重绘，不保存工作时长事实。
- `focus_session_intervals` 保存实际计入的工作区间，用于暂停/恢复审计、跨午夜和用户时区统计；休息不写入该表。
- Task 保存最终可展示的整数 `actual_minutes`，`task_focus_totals` 保存 Focus 精确秒数和已应用分钟，避免多个短 Session 分别向下取整造成丢失。
- 开始或结束专注不会改变 Task 生命周期，也不会绕过 manual 验收。
- 当前设置和本地番茄循环仍使用 WebView `localStorage`；它们只负责下一工作块参数和休息/轮次表现，不替代 Session 事实。
- 白噪音、网站屏蔽、系统勿扰、原生通知、标签分布、热力图和最佳时段均未交付。

## 当前实现状态

### 已实现：Focus Core A+B+C

- schema v11 通过 `011_focus_sessions.sql` 重建旧 `focus_sessions`，删除 `duration_minutes / completed` 双写字段，并新增状态、计划秒数、累计秒数、恢复边界、结束原因、已入账分钟和版本。
- 新增 `focus_session_intervals` 有效区间账本和 `task_focus_totals` 精确秒数余数账本；历史 Session、Task 外键和旧时长按迁移规则保留。
- 数据库通过 partial unique index 保证全库最多一个 `active / paused / recovery_pending` Session，以及最多一个未关闭 interval。
- 已提供活动查询、创建、暂停、继续、恢复、停止和取消 API；已有资源命令使用 `If-Match`，创建/停止/取消支持可重放 `Idempotency-Key` 快照。
- Sidecar 默认每 15 秒刷新 active Session 的 `last_heartbeat_at`，且心跳不递增业务 `version`。Sidecar 启动时把旧进程遗留的 active Session 原子转为 `recovery_pending`，paused 保持暂停。
- stop 会在同一事务关闭 interval、完成 Session、累计 Task Focus 精确秒数、把新增整分钟加到 `tasks.actual_minutes`、每次结算递增 Task version、写 Workflow Event 和幂等快照；只有 `actual_minutes` 实际增加时，既有 trigger 才递增关联 Project 聚合版本。任何一步失败全部回滚。
- 活动 Session 关联的 Task 不允许硬删除，返回 `TASK_HAS_OPEN_FOCUS_SESSION`；终态 Session 在 Task 删除后按外键 `SET NULL` 保留历史。
- React 使用共享 TanStack Query 快照驱动 FocusPage、RightOverview、全局 ticker 和不可关闭的恢复对话框。刷新和普通路由切换不再依赖内存递减保存事实。
- FocusPage 支持选择任一未取消 Task；不绑定任务需要再次确认。RightOverview 展示真实 Session 任务，不再猜测第一条进行中任务。
- 工作块自动到时由前端使用稳定幂等键触发 stop；服务端结算始终封顶 `planned_seconds`。休息、轮次、自动开始和提示音由本地持久化的 presentation coordinator 保留，每个工作块单独创建 Session，休息不计工时。
- 设置入口可直接打开“专注”模块；Modal 草稿/预览与 committed 设置分离。预览可改变未开始界面的展示，但创建 Session、自动下一轮和提示音只读取 committed 设置；修改、保存或取消均不改写活动 Session。
- `/stats/today` 已按 IANA 时区的当地日边界对 completed Session 的 interval 做 overlap 聚合，支持跨午夜和 DST；返回 distinct Session 数、精确秒数和向下取整的展示分钟。

### 已实现：v0.1-D1 历史与七日报告

- `GET /api/v1/focus-sessions` 默认只列终态 Session，支持 completed/cancelled/interrupted、可选 Task 筛选和稳定分页；active 仍由单独快照端点负责。
- `GET /api/v1/stats/focus` 接受显式 IANA 时区和 1–93 个本地自然日；未传日期时默认最近七天。它只聚合 completed Session 的已关闭 interval，并按每日本地边界 overlap 切分，兼容跨午夜和 DST。
- 报告返回区间 distinct Session、精确秒数、向下取整分钟、逐日事实、项目分布、截至 `date_to` 的当前连续天数，以及区间内最长连续天数；零事实日保留在序列中。
- FocusPage 展示最近七日指标/柱形趋势、终态历史分页，以及独立的加载、错误、重试和空状态。Session 结束后自动失效 Today、周期报告与历史缓存。

### 已实现：v0.1-D2a Task 详情记录

- Task 详情通过 D1 API 的 `task_id` 筛选按需读取终态 Session，默认不在 Modal 打开时抢占请求。
- 每条记录展示 completed/cancelled/interrupted、实际累计时长和结束时间；分页切换不改 Task 草稿、版本或状态。
- completed 表示已计入任务工时；cancelled/interrupted 只保留审计事实。读取失败仅影响本区块并提供重试。

### 已实现：v0.1-D2b 本地日期范围回顾

- Focus 页在不影响活动 Session、休息循环或历史分页的前提下，支持切换最近 7 天、最近 30 天、本月与自定义日期范围。
- 自定义范围复用现有显式 IANA 时区 `GET /api/v1/stats/focus` 契约；前端在请求前拒绝倒置、无效或超过 93 天的范围，避免无效查询。
- 报告继续只使用已完成 Session 的关闭 interval，保留每日事实、总块数/时长、当前/区间最长连续天数；30 天、本月和最多 93 天自定义范围的柱形图可横向浏览。
- 同一 API 返回项目时间分布：按 Session 绑定 Task 的**查询时当前** `project_id` 聚合有效 interval 秒数和 distinct Session；未绑定 Task、Task 已删除或当前未归项目统一归入“未归项目”。它不是历史项目快照，移动 Task 后旧 Session 会随当前归属重新归类。
- Focus 页在每日趋势下按秒数降序显示项目占比、专注块和向下取整分钟；项目维度与日期总量复用同一 completed-only 区间窗口，不影响活动 Session、Task 工时或 Project 聚合版本。

### 尚未实现：D2b 其余能力与后续增强

- 标签时间分布、热力图和最佳专注时段。
- 原生本地通知、托盘控制、暂停应用通知和系统专注/勿扰引导；当前只有受 WebView 音频策略约束的短提示音。
- 长休息策略、白噪音和网站屏蔽。
- 完整 SQLite `app_settings`；Focus 参数和本地休息/轮次协调仍保存在当前 WebView。

## 当前用户流程

### 开始一次专注

1. 用户在 FocusPage 选择一项未取消 Task，或再次确认不绑定任务。
2. 前端以 committed 专注时长调用 `POST /api/v1/focus-sessions`，并为同一次网络重试复用 `Idempotency-Key`。
3. Sidecar 校验 Task、计划秒数和单一未结束 Session 约束，原子创建 active Session、开放 interval 和 `focus_started` 事件。
4. API 返回 Session、`server_now`、`elapsed_seconds`、`remaining_seconds` 和 Session `ETag`；前端从该快照渲染。

### 暂停、继续、停止与取消

1. pause 关闭当前 interval，把有效秒数计入 `accumulated_seconds`，清空 `last_resumed_at` 并进入 paused。
2. resume 从服务端当前 UTC 时间创建新 interval，进入 active。
3. stop 从 active 或 paused 进入 completed；active 的最后区间按 `planned_seconds` 封顶。
4. 绑定 Task 时，stop 使用精确秒数账本结转新增整分钟；多个不足一分钟的 Session 可以在同一 Task 上累计余数。Session 的 `credited_minutes` 表示本次 stop 实际增加到 Task 的分钟数。
5. cancel 关闭当前区间并保留审计秒数，但不进入 Task 工时或 Today completed-only 统计。
6. 成功后前端失效 Focus、Task、Project 和 Today Query；Session 结束不会自动完成 Task。

### 应用重启后的恢复

1. Sidecar 启动先把数据库中的 active Session 转为 recovery_pending 并递增业务版本；paused 不变。
2. 前端启动查询 `GET /api/v1/focus-sessions/active`。没有未结束 Session 时返回 `session: null`。
3. recovery_pending 在不可关闭对话框中展示已结算时间、最后心跳和不确定间隔。
4. 用户必须选择：
   - `include_gap_resume`：把开放 interval 结算到当前时间，再从当前时间继续；
   - `exclude_gap_resume`：只结算到 `last_heartbeat_at`，排除未知间隔，再从当前时间继续；
   - `interrupt`：只结算到最后心跳并终止为 interrupted，不累计 Task 工时。
5. recover 使用 `If-Match`；前端不能在本地直接改写恢复结果。

### 番茄循环

1. 每个工作阶段对应一个独立 Focus Session。
2. 自动到时的 stop 成功后，presentation coordinator 按本轮 committed 设置进入休息或完成整轮，并在允许时播放提示音。
3. 休息使用绝对 `breakEndsAtMs` 和本地持久状态，支持暂停和跳过，不写 Session 或 Task 工时。
4. 休息结束后，用户手动开始或按 `autoStartFocus` 创建下一工作 Session；新的 Session 使用当时已保存的设置。

## 数据契约

### `focus_sessions`（schema v11）

| 字段                      | 约束 / 说明                                                                                                                           |
| ------------------------- | ------------------------------------------------------------------------------------------------------------------------------------- |
| `id`                      | UUID 主键                                                                                                                             |
| `task_id`                 | 可空 Task 外键，`ON DELETE SET NULL`                                                                                                  |
| `started_at / ended_at`   | RFC 3339 UTC；未结束时 `ended_at` 为空                                                                                                |
| `status`                  | `planned / active / paused / recovery_pending / completed / cancelled / interrupted`；当前公开创建直接进入 active，planned 是保留状态 |
| `legacy_imported`         | 旧 schema 迁入标记；仅允许终态为 1，用于无损保留历史上超过 120 分钟的合法记录                                                         |
| `planned_seconds`         | 新建 Session 为 300–7200 秒；旧 schema 的终态记录保留原时长，可能超过 7200 秒                                                         |
| `accumulated_seconds`     | 已关闭有效区间秒数，范围为 0 到 planned_seconds                                                                                       |
| `last_resumed_at`         | active/recovery_pending 的开放区间起点；paused/终态为空                                                                               |
| `last_heartbeat_at`       | active 期间最近 Sidecar 心跳；不改变业务版本                                                                                          |
| `end_reason`              | `user_stop / completed / cancelled / crash_recovery`                                                                                  |
| `credited_minutes`        | 本次 completed Session 实际增加到 Task 的整分钟；未绑定、取消和中断为 0                                                               |
| `version`                 | 从 1 开始的业务乐观锁版本                                                                                                             |
| `created_at / updated_at` | UTC 审计时间                                                                                                                          |

### `focus_session_intervals`

| 字段                    | 约束 / 说明                                                  |
| ----------------------- | ------------------------------------------------------------ |
| `id`                    | 自增主键                                                     |
| `session_id`            | Focus Session 外键，Session 删除时级联                       |
| `started_at / ended_at` | 计入工作的 UTC 区间；active 可有一个 ended_at 为空的开放区间 |
| `duration_seconds`      | 已关闭区间的封顶有效秒数；开放区间固定为 0                   |
| `created_at`            | 创建时间                                                     |

Today 统计以已关闭 interval 为事实，而不是把整个 Session 按 `started_at` 归到一天。

### `task_focus_totals`

| 字段              | 约束 / 说明                                  |
| ----------------- | -------------------------------------------- |
| `task_id`         | Task 主键外键，Task 删除时级联               |
| `exact_seconds`   | 所有已完成且已入账 Focus Session 的精确秒数  |
| `applied_minutes` | 已经加入 `tasks.actual_minutes` 的整分钟总数 |
| `updated_at`      | 最近入账时间                                 |

stop 的分钟增量为 `floor(exact_seconds / 60) - applied_minutes`，因此保留跨 Session 的秒数余数，也不会覆盖 Task 既有工时。

### schema v10 → v11 兼容迁移

- 旧 `completed = 1` 映射为 completed；其他有 `ended_at` 的记录映射为 cancelled；其余映射为 interrupted。
- `accumulated_seconds = duration_minutes * 60`；`planned_seconds` 至少为 300 秒且不小于旧累计时长。旧 schema 未限制 120 分钟上限，因此迁移记录以 `legacy_imported = 1` 无损保留超长终态；公开创建 API 仍严格限制 300–7200 秒。
- 每条旧 Session 生成一个 inferred 已关闭 interval；completed 且绑定 Task 的旧记录初始化相应 `credited_minutes` 和 `task_focus_totals`，迁移本身不再次修改 Task 工时。
- 迁移重建期间使用迁移器的单连接 `foreign_keys=off` 协议，并在提交前执行 `foreign_key_check`；成功或失败均恢复外键。
- v11 移除旧双写字段，代码和统计不再读取它们。

## API、并发与幂等

| 方法与路径                                | 当前行为                                                                                               |
| ----------------------------------------- | ------------------------------------------------------------------------------------------------------ |
| `GET /api/v1/focus-sessions/active`       | 返回唯一未结束 Session 或 `session: null`，以及服务端时间基准；active 查询同时刷新心跳但不递增 version |
| `POST /api/v1/focus-sessions`             | 创建并开始 Session；`task_id` 可空，`planned_seconds` 为 300–7200；支持创建快照幂等                    |
| `POST /api/v1/focus-sessions/:id/pause`   | 要求 Session `If-Match`；关闭 active interval 并进入 paused                                            |
| `POST /api/v1/focus-sessions/:id/resume`  | 要求 `If-Match`；从 paused 创建新 interval 并进入 active                                               |
| `POST /api/v1/focus-sessions/:id/recover` | 要求 `If-Match`；body action 为三种恢复动作之一                                                        |
| `POST /api/v1/focus-sessions/:id/stop`    | 要求 `If-Match`；支持幂等快照，完成 Session 并原子累计 Task 工时                                       |
| `POST /api/v1/focus-sessions/:id/cancel`  | 要求 `If-Match`；支持幂等快照，取消且不累计 Task 工时                                                  |
| `GET /api/v1/stats/today?date=&timezone=` | `timezone` 接受 IANA 名称；按当地日边界聚合 completed interval overlap                                 |

所有 Session 响应使用：

```json
{
  "data": {
    "session": null,
    "server_now": "2026-08-28T12:00:00Z",
    "elapsed_seconds": 0,
    "remaining_seconds": 0
  }
}
```

有 Session 时 `session` 包含上述持久字段和只读 `task_title`，响应带 `ETag: "<version>"`。

并发和重试约定：

- pause/resume/recover/stop/cancel 缺少 `If-Match` 返回 428，格式错误返回 400，旧版本返回 `409 VERSION_CONFLICT`。
- 创建、stop 和 cancel 的同 key 同请求重放首次响应并返回 `Idempotency-Replayed: true`；同 key 不同规范请求返回 `409 IDEMPOTENCY_CONFLICT`。
- 已 completed 的重复 stop、已 cancelled 的重复 cancel，即使携带旧版本或新 key，也返回当前稳定终态，不重复写 interval、Task 工时或事件。
- 同时 create 只有一个成功；同时 stop 可都取得同一终态，但 Session、Task 工时和完成事件只写一次。
- 主要领域冲突包括 `ACTIVE_FOCUS_SESSION_EXISTS`、`INVALID_FOCUS_SESSION_STATE`、`TASK_CANCELLED` 和 `TASK_HAS_OPEN_FOCUS_SESSION`。

## 状态机与事件

```text
POST create ───────────────→ active
active ──pause─────────────→ paused
paused ──resume────────────→ active
active/paused ──stop───────→ completed
active/paused ──cancel─────→ cancelled
active ──Sidecar restart───→ recovery_pending
recovery_pending ──include/exclude gap──→ active
recovery_pending ──interrupt────────────→ interrupted
```

completed、cancelled 和 interrupted 是终态；matching 的重复 stop/cancel 只作稳定重放。当前公开 API 不创建 planned Session。

已写入的 Workflow Event 包括 `focus_started`、`focus_paused`、`focus_resumed`、`focus_completed`、`focus_cancelled`、`focus_interrupted` 和 `task_actual_time_added`。心跳不写业务事件。

## 与其他模块协作

- [任务](tasks.md)：选择未取消 Task；stop 递增 `actual_minutes` 与 Task version。活动 Session 阻止 Task 硬删除，Focus 不改变 Task 状态。
- [今日](today.md)：RightOverview 读取共享活动 Session；Today stats 按 completed interval 的用户当地日 overlap 聚合。
- [项目](projects.md)：既有 Task `actual_minutes` 聚合和 trigger 会在 Focus 入账后更新项目工时与聚合版本；Session 不复制 Project 状态。
- [设置](settings.md)：committed 参数用于新 Session 与自动下一轮；draft/preview 不改写活动 Session。
- [命令与搜索](command-search.md)：当前命令可导航到 FocusPage，并可让“专注设置”直达 focus 模块；从命令结果直接绑定任务仍未交付。
- [桌面平台](desktop-platform.md)：原生通知、托盘和系统勿扰仍待实现。
- [数据管理](data-management.md)：备份/恢复必须同时覆盖 Session、interval、Task Focus 余数账本、Task 工时和事件。

## 分阶段实施状态

### v0.1-A：统一事实与迁移（已完成）

- schema v11、三张 Focus 表、单一未结束 Session/开放 interval 约束、旧数据映射和迁移测试已完成。

### v0.1-B：Session API 与事务（已完成）

- active/create/pause/resume/recover/stop/cancel、服务端绝对时间、15 秒心跳、启动恢复、乐观锁和快照幂等已完成。
- stop、精确秒数结转、Task version、条件式 Project 版本传播、Workflow Event 和幂等记录使用同一事务。
- Today 查询已切换为 completed interval overlap 和 IANA 当地日边界。

### v0.1-C：前端接入与恢复（已完成）

- 共享 Session Query、纯显示 ticker、任务选择、未绑定确认、恢复 Modal、错误重试和缓存失效已完成。
- RightOverview 已接真实 Session；专注设置入口定向和草稿不破坏活动 Session 已修复。
- 本地番茄循环继续提供休息、轮次、自动开始和提示音。

### v0.1-D1：历史与七日报告（已完成）

- 终态 Session 历史、状态/Task 筛选、稳定分页。
- 显式 IANA 时区的七日趋势、总块数/时长、当前和区间最长 Streak。
- completed-only、跨午夜、DST、空数据和错误状态自动测试。

### v0.1-D2a：Task 详情专注记录（已完成）

- 按需读取、Task 筛选、状态/时长/结束时间、稳定分页。
- 空状态、独立错误重试和 Task 草稿隔离测试。

### v0.1-D2b：高级报告与桌面反馈（部分完成）

- 已交付自定义日期/月度报告与按 Task 当前归属派生的项目时间分布；标签分布、热力图和最佳时段仍未实现。
- 原生通知、托盘、应用通知暂停与系统勿扰引导。
- 真实桌面环境的后台挂起、休眠、异常退出与三平台矩阵；当前已有可控时钟、跨午夜、DST、并发和恢复自动测试，不能替代桌面验收。

### 后续增强

- 标签分布、热力图、最佳时段、长休息、白噪音和网站屏蔽。

## 已验证的 Core 验收

- v10→v11 数据保留、约束/索引、外键检查和重复数据库升级。
- 单一未结束 Session、单一开放 interval、并发 create 和并发 stop。
- pause/resume/stop/cancel、三种恢复动作、旧版本冲突和非法转换。
- 同 key 快照重放、同 key 不同请求冲突、不同 key 重复终态，以及重复 stop 不重复入账或写事件。
- stop 事务故障时 Session、interval、Task、余数账本、事件和幂等记录全部回滚。
- 多个短 Session 的精确秒数余数结转；每次 completed 结算只递增一次 Task 版本，Project 聚合版本仅在新增完整分钟使 `actual_minutes` 变化时递增。
- Sidecar 启动恢复、15 秒心跳不递增业务版本、Router 关闭后停止心跳。
- 活动 Task 删除被阻止；终态 Task 删除后 Session 与 interval 保留。
- IANA 时区、跨午夜、DST 23/25 小时边界、completed-only 和 distinct Session 统计。
- 前端快照规范化、稳定幂等重试、缓存失效、刷新恢复、设置草稿隔离、恢复对话框、RightOverview 与番茄循环。

## 相关代码/PRD 链接

- [PRD：专注模式](../opc-workspace-PRD.md#57-专注模式)
- [PRD：主要数据表](../opc-workspace-PRD.md#主要数据表)
- [PRD：T-12 专注设置与全局计时](../opc-workspace-PRD.md#10412-t-12-专注设置与全局计时)
- [schema v11 Focus 迁移](../../services/sidecar/internal/database/migrations/011_focus_sessions.sql)
- [Focus Session API](../../services/sidecar/internal/api/focus_sessions.go)
- [Focus 历史与周期统计 API](../../services/sidecar/internal/api/focus_history.go)
- [今日统计 API](../../services/sidecar/internal/api/stats.go)
- [前端 Focus 时钟与循环](../../apps/web/src/store/focus.ts)
- [全局 ticker](../../apps/web/src/components/FocusTicker.tsx)
- [专注页面](../../apps/web/src/pages/FocusPage.tsx)
- [Task 详情专注记录](../../apps/web/src/components/TaskFocusHistorySection.tsx)
- [恢复对话框](../../apps/web/src/components/FocusRecoveryModal.tsx)
- [右侧概览](../../apps/web/src/components/RightOverview.tsx)
