# 专注与工时模块

> 文档状态：部分实现；v0.1 目标是把当前前端计时器升级为可恢复、可审计的本地 Focus Session。

## 定位与边界

本模块把“选择任务、进入专注、暂停或恢复、结束并累计工时”连接成一个本地闭环。它既提供番茄钟交互，也为今日统计、任务实际工时和后续项目工时分析提供可信数据。

核心边界：

- Focus Session 是专注运行态的唯一事实源；迁移完成后以 SQLite 中的 status、accumulated_seconds 和 last_resumed_at 为准。
- 前端 ticker 只负责根据服务端快照和当前绝对时间显示倒计时，不负责保存事实。
- Task 保存累计 actual_minutes，不复制当前计时器状态。
- 开始专注不等于开始或完成任务；结束一个专注块也不自动把任务标记为 done。
- v0.1 支持本地任务绑定、会话持久化、恢复和基础统计；白噪音、网站屏蔽和高级分析后置。
- 系统专注或勿扰能力取决于平台与用户授权，不能作为跨平台强保证。

## 当前实现状态

当前为前端内存番茄钟：

- Zustand store 支持 focus / break 两个阶段、开始、暂停、重置、循环计数和自动衔接。
- 全局 FocusTicker 每秒递减 remainingSeconds，路由切换不会中断。
- 阶段结束可通过 WebAudio 播放短提示音。
- 专注页显示环形进度和基本控制。
- 设置页支持 5–120 分钟专注、5–30 分钟休息、1–8 轮、自动开始和提示音。
- schema v2 已有 focus_sessions 表，但没有对应 Model、API 或写入逻辑。
- 今日统计 API读取数据库中的 focus_sessions，因此当前 UI 计时不会进入统计。

已知缺口：

- 没有选择或绑定真实任务，也不累计 tasks.actual_minutes。
- 运行态刷新或重启后丢失，系统休眠与后台挂起会造成按秒递减漂移。
- 没有暂停/恢复/停止/取消 Session API，没有幂等停止。
- 专注设置实时预览会重置当前计时；取消设置不能恢复已消耗进度。
- “专注设置”入口不能直接打开设置中的专注模块。
- 跳过、历史、连续天数、日/周统计、项目/标签分布、原生通知和勿扰引导尚未实现。

## 目标功能

### 任务与会话

- 从今日、任务详情、专注页和命令面板选择一项未取消任务开始专注。
- 同一数据库同时最多一个未结束 Session（active / paused / recovery_pending）。
- 开始时保存 task_id、计划秒数、开始时间、状态和版本。
- 支持暂停、继续、正常完成、提前停止和取消。
- 暂停、停止和取消时在服务端按绝对时间结算当前有效区间。
- Session 结束后保留历史，任务被删除或解除关联时仍保留会话记录。

### 准确计时与恢复

- 以 accumulated_seconds + 当前 active 区间绝对时间计算已用时间。
- 浏览器刷新、页面切换、后台挂起和系统休眠不会依赖漏掉的 tick。
- 启动时查询未结束 Session；paused 可继续保持暂停，属于旧进程的 active 先原子转为 `recovery_pending`，不能直接标记 interrupted 或自动续跑。
- recovery_pending 让用户选择“计入中断间隔并恢复”“只计到 last_heartbeat_at 后恢复”或“结束并标记 interrupted”，不把未知时间静默计入。
- 重复 stop 请求返回同一结果，不重复累计任务工时。

### 番茄循环

- 保留当前专注/休息循环、自动开始和提示音。
- 专注 Session 只记录实际工作阶段；休息阶段不累计任务工时。
- 每个工作阶段完成后写入已完成会话，再按设置进入休息或下一轮。
- 跳过休息或下一轮时写清楚用户操作，不伪造工作时长。
- 当前活动 Session 期间修改时长设置默认只影响下一次 Session，避免破坏正在运行的事实。

### 统计与反馈

- 今日展示会话数、总专注时长和关联任务。
- 专注页展示历史、日/周汇总和连续专注天数。
- 项目和标签统计从 Session → Task → Project/Tag 关联派生。
- 任务详情显示专注记录和累计工时。
- 阶段结束通过应用内提示和可选原生本地通知反馈；通知失败不影响 Session 完成事务。

## 关键用户流程

### 开始一次专注

1. 用户从任务、今日或专注页选择任务。
2. 界面展示任务、计划时长和当前专注设置。
3. 用户点击开始，前端调用创建 Session API。
4. Sidecar 检查没有其他 active/paused/recovery_pending Session，写入 active Session 并返回服务端时间基准。
5. 前端根据快照与绝对时间渲染倒计时；ticker 只触发重绘。

### 暂停、继续与停止

1. 暂停时，Sidecar 结算 now - last_resumed_at 到 accumulated_seconds，清空 last_resumed_at 并进入 paused。
2. 继续时，Sidecar 写入新的 last_resumed_at 并进入 active。
3. 正常完成或提前停止时，Sidecar结算最后区间，将 Session 标记 completed，并在同一幂等事务中累计任务实际工时。
4. 取消时将 Session 标记 cancelled，不累计任务工时。
5. UI 刷新 Session、Task 和今日统计缓存。

### 应用重启后的恢复

1. 应用启动查询 /focus-sessions/active。
2. 若没有活动会话，正常进入首页。
3. 若发现 paused 或 recovery_pending Session，显示任务、开始时间、已结算时间、last_heartbeat_at 和不确定间隔。
4. paused 可正常 resume；recovery_pending 必须由用户选择计入间隔恢复、排除间隔恢复，或结束为 interrupted。
5. 服务端按用户选择执行受控状态命令并返回最新事实；界面不得在本地直接改写。

### 完成一个番茄循环

1. active 工作阶段达到 planned_seconds。
2. 前端或本地调度触发幂等 stop；服务端完成 Session 和工时累计。
3. 应用播放本地提示，并按设置进入休息阶段。
4. 休息结束后，用户或自动开始策略创建下一次工作 Session。
5. 任务是否完成仍由用户在任务模块确认或进入验收链路。

## 数据/API/状态与事件

### Focus Session 数据

规划迁移重建 focus_sessions，删除旧 duration_minutes 和 completed 双写字段：

| 字段 | 用途 |
|------|------|
| id | Session UUID |
| task_id | 可空任务引用；主要工作流要求优先绑定任务 |
| started_at / ended_at | UTC 起止时间 |
| status | planned / active / paused / recovery_pending / completed / cancelled / interrupted |
| planned_seconds | 本次计划工作时长 |
| accumulated_seconds | 已结算的唯一有效时长 |
| last_resumed_at | 当前 active 区间起点；paused 时为空 |
| last_heartbeat_at | Sidecar 活跃期间的最近心跳；用于界定崩溃后的不确定间隔 |
| end_reason | user_stop / completed / cancelled / crash_recovery |
| version | 乐观并发版本 |

历史迁移规则：

- completed=1 的旧记录迁为 completed。
- 其他旧记录依据 ended_at 映射为 cancelled 或 interrupted。
- accumulated_seconds 由旧 duration_minutes 乘 60 得到。
- 同一迁移版本同步更新今日统计，不能让新旧时长字段并行可写。

### API

| 方法与路径 | 用途 |
|------------|------|
| GET /api/v1/focus-sessions/active | 查询启动时需要处理的活动会话 |
| POST /api/v1/focus-sessions | 创建并开始 Session |
| POST /api/v1/focus-sessions/:id/pause | 结算当前区间并暂停 |
| POST /api/v1/focus-sessions/:id/resume | 按绝对时间恢复 |
| POST /api/v1/focus-sessions/:id/recover | 处理 recovery_pending：计入/排除间隔恢复或结束为 interrupted |
| POST /api/v1/focus-sessions/:id/stop | 完成并幂等累计任务工时 |
| POST /api/v1/focus-sessions/:id/cancel | 取消且不累计工时 |
| GET /api/v1/stats/today | 返回当天 Session 数和专注分钟 |

所有状态写入携带 expected_version 或 If-Match。start、stop、cancel 支持 Idempotency-Key。

### 状态与事件

允许的主要转换：

- planned → active
- active → paused / recovery_pending / completed / cancelled
- paused → active / completed / cancelled
- recovery_pending → active / interrupted
- completed、cancelled、interrupted 为终态，不原地重新打开

关键事件包括 focus_started、focus_paused、focus_resumed、focus_completed、focus_cancelled、focus_interrupted 和 task_actual_time_added。停止 Session 与 task_actual_time_added 必须在同一事务中成功或回滚。

## 与其他模块协作

- [任务](tasks.md)：选择任务开始专注；停止后累计 actual_minutes；专注结束不自动完成任务。
- [今日](today.md)：显示当前 Session、今日块数和时长，并提供开始/暂停快捷操作。
- [项目](projects.md)：通过任务关联派生项目和标签时间分布，不在 Session 复制项目状态。
- [设置](settings.md)：提供默认时长、循环、自动开始和提示音；活动 Session 期间的新设置应用于下一轮。
- [命令与搜索](command-search.md)：可定位任务并开始或切换专注；未选择任务时先要求确认。
- [桌面平台](desktop-platform.md)：提供原生本地通知、托盘控制和可选系统勿扰引导。
- [收件箱](inbox.md)：专注本身通常不生成 Inbox Item；需要处理的恢复失败或系统维护异常可以进入收件箱。
- [数据管理](data-management.md)：备份、恢复和迁移必须覆盖 Session 与任务工时的一致性。

## 分阶段实施

### v0.1-A：统一事实与迁移

- 设计并新增递增迁移，重建 focus_sessions。
- 建立单一活动 Session 约束、乐观锁和历史数据映射。
- 同步修改今日统计查询并补迁移测试。

### v0.1-B：Session API 与事务

- 实现查询活动会话、开始、暂停、继续、停止和取消。
- 使用服务端 UTC 时间和绝对时间结算。
- stop 与 tasks.actual_minutes 累计同事务、可幂等重放。
- 覆盖重复请求、并发状态改变和任务删除边界。

### v0.1-C：前端接入与恢复

- 将 useFocusStore 收缩为服务端快照和纯显示状态，不再保存运行事实。
- 增加任务选择、恢复/结束上次会话、错误重试和服务不可用状态。
- 让今日、专注页和命令面板共享同一 Session Query。
- 修复设置入口定向和设置草稿重置当前计时的问题。

### v0.1-D：历史、统计与桌面反馈

- 增加 Session 历史、日/周统计和连续天数。
- 接入应用内提示、原生本地通知和托盘控制。
- 用真实桌面环境验证后台挂起、休眠、跨午夜和异常退出。

### 后续增强

- 项目/标签时间分布、热力图和最佳时段分析。
- 长休息策略、白噪音和网站屏蔽需另行评审权限、包体和跨平台边界。

## 验收标准

- 同一数据库任何时刻最多存在一个未结束 Session（active / paused / recovery_pending）。
- 页面切换、刷新、后台挂起和系统休眠后，显示时间由绝对时间正确恢复。
- 应用重启不会自动续跑；用户可明确选择恢复或结束上次会话。
- pause、resume、stop、cancel 的非法状态转换被拒绝，并发旧写入返回 409。
- 重复 stop 不重复累计 tasks.actual_minutes。
- cancel 不累计任务工时；休息阶段不累计工作时长。
- Session 完成和任务工时累计同事务成功或同时回滚。
- 今日统计与 Session 明细一致，跨午夜和用户时区边界正确。
- 修改或取消设置草稿不会损坏活动 Session 或丢失已消耗进度。
- 专注结束不自动将任务标为 done，manual 验收任务仍走任务验收流程。
- 原生通知、音频或系统勿扰不可用时，核心 Session 仍可完成并持久化。
- 加载、空、错误、重试和离线状态均有真实反馈，不使用伪造统计。

## 相关代码/PRD链接

- [PRD：专注模式](../opc-workspace-PRD.md#57-专注模式)
- [PRD：focus_sessions 数据规划](../opc-workspace-PRD.md#主要数据表)
- [PRD：T-12 专注设置与全局计时](../opc-workspace-PRD.md#10412-t-12-专注设置与全局计时)
- [当前专注状态机](../../apps/web/src/store/focus.ts)
- [当前全局 ticker](../../apps/web/src/components/FocusTicker.tsx)
- [当前专注页面](../../apps/web/src/pages/FocusPage.tsx)
- [当前设置存储](../../apps/web/src/store/settings.ts)
- [当前 focus_sessions schema](../../services/sidecar/internal/database/migrations/001_initial_schema.sql)
- [当前今日统计](../../services/sidecar/internal/api/stats.go)
