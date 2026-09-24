# 专注与工时模块

> 当前基线：app v0.1.1 / API v1 / SQLite schema v76（2026-09-20）。Focus 结构仍由 schema v11 引入；后续迁移不改 Focus 表契约。Focus Core v0.1-A/B/C、v0.1-D1（历史与周期报告）、D2a（Task 详情记录）、项目详情的项目级报告/终态 Session 历史，以及 D2b 的本地日期范围回顾、项目/当前标签时间分布、最佳小时段与二维热力图已经交付。通用桌面托盘已接显示/隐藏/退出源码，但不读取 Focus 状态；专注控制、原生通知和勿扰仍属后续。

## 定位与边界

本模块把“选择任务、进入专注、暂停或恢复、结束并累计工时”连接成一个本地闭环，并为今日统计和项目工时聚合提供可信事实。

核心边界：

- SQLite Focus Session 是工作阶段运行态的唯一事实源；前端 ticker 只根据服务端快照和绝对时间重绘，不保存工作时长事实。
- `focus_session_intervals` 保存实际计入的工作区间，用于暂停/恢复审计、跨午夜和用户时区统计；休息不写入该表。
- Task 保存最终可展示的整数 `actual_minutes`，`task_focus_totals` 保存 Focus 精确秒数和已应用分钟，避免多个短 Session 分别向下取整造成丢失。
- 开始或结束专注不会改变 Task 生命周期，也不会绕过 manual 验收。
- 专注设置保存于版本化 SQLite `app_settings`，本地番茄循环使用 WebView `localStorage`；前者提供下一工作块参数，后者保存休息/轮次表现，均不替代 Session 事实。
- 白噪音、网站屏蔽、系统勿扰和原生通知均未交付。

## 当前实现状态

### 已实现：Focus Core A+B+C

- schema v11 通过 `011_focus_sessions.sql` 重建旧 `focus_sessions`，删除 `duration_minutes / completed` 双写字段，并新增状态、计划秒数、累计秒数、恢复边界、结束原因、已入账分钟和版本。
- 新增 `focus_session_intervals` 有效区间账本和 `task_focus_totals` 精确秒数余数账本；历史 Session、Task 外键和旧时长按迁移规则保留。
- 数据库通过 partial unique index 保证全库最多一个 `active / paused / recovery_pending` Session，以及最多一个未关闭 interval。
- 已提供活动查询、创建、暂停、继续、恢复、停止和取消 API；已有资源命令使用 `If-Match`，创建/停止/取消支持可重放 `Idempotency-Key` 快照。
- Sidecar 默认每 15 秒刷新 active Session 的 `last_heartbeat_at`，且心跳不递增业务 `version`。Sidecar 启动时把旧进程遗留的 active Session 原子转为 `recovery_pending`，paused 保持暂停。
- stop 会在同一事务关闭 interval、完成 Session、累计 Task Focus 精确秒数、把新增整分钟加到 `tasks.actual_minutes`、每次结算递增 Task version、写 Workflow Event 和幂等快照；只有 `actual_minutes` 实际增加时，既有 trigger 才递增关联 Project 聚合版本。任何一步失败全部回滚。
- 活动 Session 关联的 Task 不允许硬删除，返回 `TASK_HAS_OPEN_FOCUS_SESSION`；终态 Session 在 Task 删除后按外键 `SET NULL` 保留历史。
- React 使用共享 TanStack Query 快照驱动 FocusPage、工作台右侧 WorkbenchOverview、全局 ticker 和需明确决策的恢复对话框（业务页可跳转智能体，/ai 不阻断聊天）。刷新和普通路由切换不再依赖内存递减保存事实。工作台环形卡复用已有暂停/继续命令并携带 Session 当前版本，不自行创建 Session 或累计工时；命令失败提供可见提示。
- FocusPage 支持选择任一未取消 Task；不绑定任务需要再次确认。工作台右侧环形卡展示真实 Session 任务、计划时长与剩余时间，不猜测第一条进行中任务。左侧仅保留“专注”导航，不再显示重复计时器。
- 工作块自动到时由前端使用稳定幂等键触发 stop；服务端结算始终封顶 `planned_seconds`。休息、轮次、自动开始和提示音由本地持久化的 presentation coordinator 保留，每个工作块单独创建 Session，休息不计工时。
- 设置入口可直接打开“专注”模块；Modal 草稿/预览与 committed 设置分离。预览可改变未开始界面的展示，但创建 Session、自动下一轮和提示音只读取 committed 设置；修改、保存或取消均不改写活动 Session。
- `/stats/today` 已按 IANA 时区的当地日边界对 completed Session 的 interval 做 overlap 聚合，支持跨午夜和 DST；返回 distinct Session 数、精确秒数和向下取整的展示分钟。

### 已实现：v0.1-D1 历史与七日报告

- `GET /api/v1/focus-sessions` 默认只列终态 Session，支持 completed/cancelled/interrupted、可选 Task/Project 筛选和稳定分页；active 仍由单独快照端点负责。
- `GET /api/v1/stats/focus` 接受显式 IANA 时区、1–93 个本地自然日和可选 Project 筛选；未传日期时默认最近七天。它只聚合 completed Session 的已关闭正时长 interval，并按每日本地边界 overlap 切分，兼容跨午夜和 DST。
- 报告返回区间 distinct Session、精确秒数、向下取整分钟、逐日事实、项目分布、当前标签分布、0–23 点小时分布、周一至周日×24 小时热力图、截至 `date_to` 的当前连续天数，以及区间内最长连续天数；零事实日、小时和热力格保留在序列中。
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
- 同一有效 interval 按用户 IANA 时区的当地小时归入固定 24 桶；跨午夜与 DST 使用真实 UTC 流逝秒数，秋季重复小时合并到同一当地小时，春季跳过小时保留零值。Focus 页展示 24 格强度和秒数最高的最佳小时段；并列时取较早小时。
- 同一次区间遍历还把 interval 归入固定 168 格，顺序为周一至周日、每行 0–23 点；distinct Session 在每格独立去重。Focus 页以 7×24 矩阵展示当地星期/小时规律，支持横向滚动和逐格分钟/块数提示。
- 标签分布读取 Session 绑定 Task 的查询时当前 `task_tags`；一个 Task 有多个标签时，同一有效 interval 分别完整计入每个标签，所以标签分钟之和可能超过区间总分钟。无任务、Task 已删除或当前无标签统一归入“未加标签”。标签改绑/删除会重分类旧 Session，不是历史标签快照。

### 已实现：Project 详情专注分析与终态历史

- 两个 D1 读接口可选 `project_id`，且不改变未传参数时的全局响应。参数必须为小写 canonical UUID；空、非法、大小写或其他非 canonical 形式返回 `400 INVALID_PROJECT_ID`，规范但不存在的 Project 返回 `404 PROJECT_NOT_FOUND`，归档 Project 仍可读。
- Project 归属按 Session 绑定 Task 的**查询时当前** `project_id` 派生；Task 从 Project A 改绑到 B 后，旧 Session 会从 A 重分类到 B。无 Task、Task 已删除或当前 Task 无 Project 的 Session 不进入项目过滤结果。
- Project 详情在任务区之后独立加载 7 天、30 天和本月报告，展示 completed-only 总时长、distinct Session 完成数、当前/区间最长连续天数及每日趋势。
- 同一区块另以每页 6 条稳定分页展示 completed/cancelled/interrupted 终态历史；取消/中断仅显示审计“记录时长”，不进入报告或 Task 工时。
- 报告与历史各自提供加载、空、错误、重试和分页状态，任一失败不阻塞 Project 主详情或另一卡；总页数回缩时前端收敛到有效页，归档 Project 保持只读可查。
- 该纵切复用 schema v11 Focus 表、现有 Task/Project 外键和 API v1，仅增加可选查询参数与读模型，本身不新增迁移；当前 schema v30–v35 的新增分别涉及 Task Submission、Client Activity、Reminder、Automation、Agent Adapter 与 Client Followup，不改变该 Focus 读模型。

### AI 独立轨道：查询与受确认的会话操作（H3-A1/A2，schema 072）

- 单次 `work` 授权提供 `workspace_focus`：`view=active` 返回唯一未结束会话或 null；`view=session` 按 canonical UUID 读取；`view=history` 读取 completed/cancelled/interrupted，支持 Task ID、状态及 1–20 条/offset 0–1000 的有界分页。返回真实版本、绑定任务名称、计划/累计/剩余秒数、开始/结束/心跳与入账分钟，以及 UTC `server_now`；不带 Task 正文或人员资料。查询直接读取共享快照，不调用会刷新心跳的人工 active HTTP 端点。
- `local_cycle_included=false` 明确不含 WebView 休息和轮次；无未结束会话不能解释为本地循环空闲。recovery_pending 的工具显示秒数不含未结算区间，须明确选择未知间隔处理方式；不伪装成仍在准确计时。可在专注页处理，也可从恢复弹窗选择“在智能体中处理”：该入口只暂存 `workspace_focus(view=session,id=...)` 排查/恢复提示并跳到 `/ai`，不自动恢复、不发送消息、不授予权限。`/ai` 不挂出阻断式恢复弹窗，但会话继续保持待恢复。
- `work+actions` 可提议 `focus.pause/resume`，绑定 `focus_session_id/expected_version`、空 changes；只在人工确认时执行。`focus_commands.go` 为原生 API 与审批的共享事务入口，预览/执行均校验状态和版本，确认另重验关联任务名称/计划快照。暂停按确认时刻关闭区间并封顶，继续从确认时刻创建区间；不结算 Task 工时、不改变任务状态。
- 领域事件、区间、会话与审批同事务回滚；已执行卡重放只读原决定，不重新控制当前会话。前端取消旧 Focus 在途查询后刷新活动/历史/报告及关联缓存，不重置本地循环。结果与后续无正文回执链接到 `/focus` 页面，不虚构会话深链。
- H3-A2 增加 `focus.start`：changes 必须显式给出 `task_id`（canonical UUID 或 null）及 `planned_seconds`（300–7200）；绑定任务另需 `expected_task_version`，不绑定时不能带任务版本。顶层不得带 Session ID/version。提议与确认均校验任务可用、任务版本和唯一未结束会话；已有 active/paused/recovery_pending 时不覆盖。人工 decision 必须另传 `confirm_focus_start:true`，缺失/false 返回 `422 FOCUS_START_CONFIRMATION_REQUIRED`。确认卡明确新循环会替代本地休息及旧轮次，不绑定任务需明确勾选；模型不能提供同意字段。
- `focus.stop/cancel` 绑定 Session ID/version、空 changes，只接受 active/paused。stop 在确认时结算并封顶计划秒数，复用余秒账本累计 Task 工时；cancel 不入账，两者均不完成 Task、不创建休息或下一块。另一入口已结束会话时旧 pending 提议冲突，不借原生命令的终态幂等绕过预览验证。
- `focus.recover` 仅接受 recovery_pending 和 changes 中唯一 `recovery_action`：`include_gap_resume` 计入至确认时刻的间隔并继续；`exclude_gap_resume` 仅计到最后心跳并从现在继续；`interrupt` 按最后心跳结束为中断且不入账。预览含最后心跳和已结算秒数，不冻结持续变化的未知间隔。计入间隔另需人工 `confirm_focus_gap:true`，缺失/false 返回 `422 FOCUS_GAP_CONFIRMATION_REQUIRED`；两种同意字段只能用于对应的人工 confirm，拒绝或其他动作不能携带。已经确认的重放只返回原决定。
- 所有新动作均复用 `focus_commands.go`；领域事实、区间、工时和审批事件同事务。前端只在回读到最新活动事实后由全局 ticker 协调本地循环，历史确认卡不能复活旧 Session 或重置新的休息。AI 周期报告由下方 H3-A3 接续；本地休息的直接控制和原生反馈仍未开放，H3/整体强化尚未完成。
- 验证入口：`ai_focus_test.go`、`ai_focus_lifecycle_test.go`、`focus_sessions_test.go`、`AiWorkspaceActions.test.tsx`、`aiWorkspaceActions.test.ts`、`focusHooks.test.tsx`、`FocusTicker.integration.test.tsx`。包括真实 Harness→隔离模拟模型→读取/提议→人工确认→下一次无授权回执、三种恢复/回滚、额外同意、响应丢失与旧卡不重放；不调用真实模型。

### AI 独立轨道：专注周期报告（H3-A3，schema 072 不变）

- 单次 `work` 授权增加 `workspace_focus_report`，必须显式提供 `date_from/date_to`（含首尾，1–93 个本地自然日）和用户确认的 IANA `timezone`；不默认采用服务端 UTC 或猜测用户时区。可选 `project_id` 必须为真实 canonical UUID，归档项目可查，归属按当前 Task 关系读取。
- `view=summary` 返回整个范围的 distinct Session 数、秒数、向下取整分钟及截至 date_to/区间内连续天数；`days/projects/tags/hours/heatmap` 分维度返回上述总量和有界 `items`，支持 limit 1–20（默认 10）、offset 0–1000，并标注 total_items、has_more、next_offset、window_limited 和排序。summary 不接受分页字段，所有参数拒绝 null、未知字段和跨类型参数。
- 人工 `GET /stats/focus` 与工具共用 `focus_history.go` 的 `readFocusPeriodStats`，在单个只读事务加载区间、Task/Project 和标签事实后计算，不进行 HTTP 自调用或复制聚合算法。原生默认最近七日、旧时区偏移兼容及错误码保持不变；只计 completed 的已关闭正时长区间，保留 DST、跨日、零值桶与非互斥标签语义，不刷新心跳、不累计 Task 工时、不生成审批。
- 结果明确 `project_and_tag_attribution=current_task_relationships_not_historical`、标签秒数和各桶 Session 数不可相加、分钟按桶取整、连续天数仅在请求范围内。小时固定 0–23，热力图按周一 1 至周日 7 再按小时；未归项目/标签用 null 表示。分页是每次查询的新快照，不能当跨请求冻结报告；有变化应重新核对，不把未读页当零值。结果受既有 24 KiB 工具预算限制，既有 Harness 超时/停止会传播到查询和聚合。
- 授权面板披露工时与分布外发；不返回 Task 正文/名称、人员、客户或区间明细。H4-C 的结果 route 已携带日期、时区、可选项目及报告维度，点击后才在专注页读取对应当前事实，详见下方定位契约。没有新增 API、SQLite 迁移、独立统计副本或自动业务写入。
- 确定性测试见 `ai_focus_report_test.go`：与人工报告逐维逐页比对、春秋 DST/跨午夜、当前项目/标签重分类、93 日边界、空值/分页上限、取消/恢复/Provider 变更门禁、只读心跳与脱敏存储失败，以及真实 Harness→隔离模拟模型→聚合结果回填→下一轮不继承授权。真实模型体验仍需单独验收。

### 已实现：H4-C 专注报告定位与返回对话

- `workspace_focus_report` 由代码生成 `/focus?date_from=YYYY-MM-DD&date_to=YYYY-MM-DD&report=summary|days|projects|tags|hours|heatmap&timezone=IANA`，项目过滤额外携带 `project_id`。沿用实际聚合窗口，不携带模型权限、工具分页 offset/limit 或业务正文；人工页展示该范围的完整报告，而非一页工具结果。
- 主/侧对话共用安全富文本渲染，允许这组精确参数；拒绝重复、未知、缺失和非法参数、外部地址/片段及模型指定的返回会话。界面按产生回复的真实 Session 补 `return_session=UUID`；“返回原对话”在主聊天区选中该会话，不发送消息、不继承授权，也不被另一条后台生成抢走。会话已删除时沿用聊天页的不可用状态，不重新创建。
- FocusPage 初次渲染、刷新、同页换链接及浏览器后退都按 URL 条件读取，不先请求默认窗口。读取完成定位并聚焦指定维度；空报告/失败回到报告区。日期输入不被自动滚动抢焦点。修改日期或快捷范围保留链接时区/项目；快捷范围以该时区的今日计算并写成明确日期，“清除链接筛选”恢复本机时区、全部项目和最近七天。
- 日期限 1–93 个自然日，时区必须可解析为 IANA，项目为 canonical UUID；无效链接展示错误并禁用报告读取，不静默降级。项目删除显示不可用、可清除筛选；报告与活动计时读取独立，查询失败可重试且不展示失败后的旧数据。筛选只作用于报告，不改变活动 Session、Today、本地循环或最近历史分页。
- 页面明确重新读取的是当前事实，不保证与对话时刻相同。链接是导航线索，不是经过核验的知识引用；真实模型遵循返回链接的质量仍须单独验收。验证：`focusReportLocation.test.ts`、`FocusPage.test.tsx`、主/侧聊天测试与 `ai_focus_report_test.go`；无 schema/API 版本变化。

### 已实现：H3-A2 前置——Session 身份与异步循环协调

- 本地循环增加 `sessionId`，区分同一 Task 上的不同工作块；运行时 `revision` 防止创建期间的本地重置被旧回调撤销，不持久化 revision。存储键仍为 `opc-workspace-focus-cycle-v1`，Zustand payload version 升至 2；v1 升级保留休息绝对截止时间、Task 和轮次，Session 身份先置空，旧 work 在读取同一任务的真实未结束 Session 时绑定，不补写服务端事实。SQLite/API 版本不变。
- 创建、手动结束/取消与中断恢复的本地转换统一在共享 mutation hook 执行，不依赖 Today/FocusPage/恢复弹窗是否仍挂载。只有匹配 Session 的自动到时 stop 才计入下一休息/轮次；重复结果不重复计数，A 的旧结束/恢复结果不能重置 B，即使两者绑定同一 Task。
- active 查询传递 AbortSignal；原生命令前及成功回包后取消旧活动查询。更新缓存时检查 Session ID、版本和更新期间的空快照，不允许旧 stop 清空另一个会话、旧 pause 降低版本或旧 create 覆盖新空快照；随后重新读取活动事实。网络/超时的模糊失败也触发回读。
- 创建的幂等重试可能返回已结束会话的首次快照；即使它与当前空快照处于同一服务端秒，也不能视为仍在运行。重试成功后先读活动端点，再校验 ID 并同步；回读失败不重放旧创建快照，保留查询恢复路径。
- 全局 ticker 同步 active/paused/recovery_pending，会接管从其他入口观察到的新 Session；新外部会话开始独立轮次，不凭相同 Task 自动接续旧休息。只有本地“开始下一块”/自动下一块的成功创建才保留已完成轮数。服务端明确为空时清理过期 work；自动 stop 回包仍在途时保留该次完成意图。查询加载、刷新、失败或已有未结束会话时，不从旧本地休息自动创建下一块。
- 验证包括真实 QueryClient + mutation + ticker 集成、延迟停止/创建/恢复回包、同会话版本竞争、v1 本地状态升级、轮次幂等和未知活动状态门禁。前置本身不增加 AI 命令；后续接入见上方 H3-A2，不声明真实模型或桌面联调通过。

### 尚未实现：原生反馈与后续增强

- 原生本地通知、托盘专注状态/控制、暂停应用通知和系统专注/勿扰引导；当前只有受 WebView 音频策略约束的短提示音。通用托盘仅显示/隐藏窗口和退出，不改变 Session。
- 长休息策略、白噪音和网站屏蔽。
- Focus 参数已保存于版本化 SQLite `app_settings`；长休息和更高级的本地编排策略仍未实现。

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
6. “在智能体中处理”不会执行 recover，只把 recovery_pending Session 的 ID、version 和页面显示的已确认/不确定时长放入待带入的智能体提示；进入 `/ai` 后仍需人工选择权限、发送消息和确认 `focus.recover` 建议。

### 番茄循环

1. 每个工作阶段对应一个独立 Focus Session。
2. 自动到时的 stop 成功后，presentation coordinator 按本轮 committed 设置进入休息或完成整轮，并在允许时播放提示音。
3. 休息使用绝对 `breakEndsAtMs` 和本地持久状态，支持暂停和跳过，不写 Session 或 Task 工时。
4. 休息结束后，用户手动开始或按 `autoStartFocus` 创建下一工作 Session；新的 Session 使用当时已保存的设置。

### 在项目详情查看专注事实

1. 用户进入存在或已归档 Project 的详情页，Focus 区分别请求带该 Project canonical UUID 的报告和终态历史；主详情不等待这两个请求。
2. 用户切换 7 天、30 天或本月时，只改变报告的本地日期范围；历史页码与活动 Session 不受影响。
3. 报告继续按浏览器 IANA 时区切分 completed Session 的闭合正时长 interval；历史可同时展示 cancelled/interrupted 作为审计记录。
4. Task 改绑、删除或移出 Project 后，刷新结果按当前关系重分类；本功能不维护历史 Project 快照，也不回写 Task、Project 或 Session。
5. 某一卡失败时用户可独立重试；空报告与空历史分别展示明确空态。归档 Project 可读，已永久删除的 Project 返回 `404 PROJECT_NOT_FOUND`。

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

| 方法与路径                                | 当前行为                                                                                                               |
| ----------------------------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| `GET /api/v1/focus-sessions/active`       | 返回唯一未结束 Session 或 `session: null`，以及服务端时间基准；active 查询同时刷新心跳但不递增 version                 |
| `POST /api/v1/focus-sessions`             | 创建并开始 Session；`task_id` 可空，`planned_seconds` 为 300–7200；支持创建快照幂等                                    |
| `POST /api/v1/focus-sessions/:id/pause`   | 要求 Session `If-Match`；关闭 active interval 并进入 paused                                                            |
| `POST /api/v1/focus-sessions/:id/resume`  | 要求 `If-Match`；从 paused 创建新 interval 并进入 active                                                               |
| `POST /api/v1/focus-sessions/:id/recover` | 要求 `If-Match`；body action 为三种恢复动作之一                                                                        |
| `POST /api/v1/focus-sessions/:id/stop`    | 要求 `If-Match`；支持幂等快照，完成 Session 并原子累计 Task 工时                                                       |
| `POST /api/v1/focus-sessions/:id/cancel`  | 要求 `If-Match`；支持幂等快照，取消且不累计 Task 工时                                                                  |
| `GET /api/v1/focus-sessions`              | 终态历史；支持 `task_id/status/page/page_size` 及可选 canonical UUID `project_id`，按 `ended_at DESC, id ASC` 稳定分页 |
| `GET /api/v1/stats/focus`                 | 1–93 个当地日的 completed-only 报告；支持显式 IANA 时区和可选 canonical UUID `project_id`                              |
| `GET /api/v1/stats/today?date=&timezone=` | `timezone` 接受 IANA 名称；按当地日边界聚合 completed interval overlap                                                 |

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
- `project_id` 缺失时维持全局读模型；空、非法或非 canonical 返回 `400 INVALID_PROJECT_ID`，canonical 但不存在返回 `404 PROJECT_NOT_FOUND`。归档 Project 不冲突，按正常只读结果返回。

前端 Focus 派生读模型使用独立 Query 前缀，并把 Project、日期范围、Task/状态和页码保留在 key 中。当前精确失效边界为：Task 编辑/删除及批量 `set_project` 刷新历史与报告；批量标签变更、Tag 更新/删除和 Project 更新只刷新报告；Project 删除只把报告标为 stale 且不在详情导航前重取已删除 ID。计划日期、排序、生命周期和 Artifact/Submission 等不改变当前标题/项目/标签归属的操作不刷新这两个派生读模型；Focus stop/cancel/recover 仍按既有 Focus 事实边界刷新活动、历史、报告、Today、Task 与 Project 查询。

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
- [今日](today.md)：右侧 WorkbenchOverview 读取共享活动 Session；Today stats 按 completed interval 的用户当地日 overlap 聚合。
- [项目](projects.md)：既有 Task `actual_minutes` 聚合和 trigger 会在 Focus 入账后更新项目工时与聚合版本；Project 详情另以可选 `project_id` 按 Task 当前归属读取报告和终态历史，Session 不复制 Project 状态或历史归属。
- [设置](settings.md)：committed 参数用于新 Session 与自动下一轮；draft/preview 不改写活动 Session。
- [命令与搜索](command-search.md)：当前命令可导航到 FocusPage，并可让“专注设置”直达 focus 模块；从命令结果直接绑定任务仍未交付。
- [桌面平台](desktop-platform.md)：通用托盘最小源码闭环已接；Focus 状态/动作、原生通知和系统勿扰仍待实现。
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
- 工作台右侧环形卡已接真实 Session；专注设置入口定向和草稿不破坏活动 Session 已修复。
- 本地番茄循环继续提供休息、轮次、自动开始和提示音。

### v0.1-D1：历史与七日报告（已完成）

- 终态 Session 历史、状态/Task 筛选、稳定分页。
- 显式 IANA 时区的七日趋势、总块数/时长、当前和区间最长 Streak。
- completed-only、跨午夜、DST、空数据和错误状态自动测试。

### v0.1-D2a：Task 详情专注记录（已完成）

- 按需读取、Task 筛选、状态/时长/结束时间、稳定分页。
- 空状态、独立错误重试和 Task 草稿隔离测试。

### v0.1-D2b：高级报告与桌面反馈（部分完成）

- 已交付自定义日期/月度报告、按 Task 当前归属派生的项目时间分布、按 Task 当前多标签派生的非互斥标签分布、DST 安全的 24 小时分布/最佳时段与周几×小时二维热力图。
- 原生通知、托盘专注状态/控制、应用通知暂停与系统勿扰引导；通用显示/隐藏/退出托盘不计入本项。
- 真实桌面环境的后台挂起、休眠、异常退出与三平台矩阵；当前已有可控时钟、跨午夜、DST、并发和恢复自动测试，不能替代桌面验收。

### Project 详情读取（已完成）

- D1 历史与周期报告增加可选 `project_id`，严格 canonical UUID/400/404、归档可读和当前 Task 项目归属语义已完成。
- Project 详情已接 7 天/30 天/本月趋势、总时长、完成数、连续天数、终态历史分页，以及报告/历史独立加载、空、错误和重试。
- 该读取不增加 schema migration；API 保持 v1，SQLite 当前为 schema v35，v30–v35 不改变 Focus 表或读取契约。

### 后续增强

- 长休息、白噪音和网站屏蔽。

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
- Project 过滤覆盖 canonical UUID 400、不存在 404、归档/空 Project、当前 Task 项目重分类、Task 删除/无项目排除、终态历史分页，以及 completed-only 报告的跨午夜/DST/零事实序列。
- Project 详情覆盖 7 天/30 天/本月、总时长/完成数/Streak、终态历史、分页收敛、两路独立加载/空/错误/重试和归档只读；缓存测试覆盖必要失效与改期/排序等无关写入不失效。
- 前端快照规范化、稳定幂等重试、缓存失效、刷新恢复、设置草稿隔离、恢复对话框、右侧专注卡与番茄循环。
- Session 身份绑定、v1→v2 本地循环迁移、A 的迟到结果不覆盖 B、重复完成不重复计轮、查询未确认空闲时不自动开始。

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
- [Query / ticker / 循环集成测试](../../apps/web/src/components/FocusTicker.integration.test.tsx)
- [专注页面](../../apps/web/src/pages/FocusPage.tsx)
- [Task 详情专注记录](../../apps/web/src/components/TaskFocusHistorySection.tsx)
- [Project 详情专注分析](../../apps/web/src/components/ProjectFocusSection.tsx)
- [恢复对话框](../../apps/web/src/components/FocusRecoveryModal.tsx)
- [工作台右侧专注卡](../../apps/web/src/components/WorkbenchOverview.tsx)
