# opc-workspace 整体功能架构

> 文档版本：2.16
> 日期：2026-08-28
> 依据：[PRD v6.6](opc-workspace-PRD.md)
> 当前实现基线：app v0.1.0 / API v1 / SQLite schema v25

## 1. 目的

本文说明各功能模块如何协作、跨模块状态如何流转、哪个对象拥有最终事实，以及后续实现应遵循的依赖顺序。

它不替代 PRD，也不把规划描述成当前代码。各模块的详细用户流程和验收条件见 [模块文档索引](modules/README.md)；当前实现仍以仓库代码和测试为准。

## 2. 总体设计原则

1. **本地优先**：核心功能断网可用，业务事实保存在 SQLite 和应用控制的本地文件目录。
2. **单一事实源**：一个状态只由一个领域对象负责，其他模块通过查询或事件派生展示。
3. **任务即工单**：项目产出拆出的工作、发票待办、审核和提醒后续动作都使用 Task，不新增第二套执行实体。
4. **收件箱负责编排**：Inbox Item 解释“为什么要处理”，Task 解释“具体做什么”。
5. **Actor 只表达责任**：Assignment 保存责任变化；person 不等于在线用户，agent 不等于已完成工作。
6. **Agent 必须验收**：Agent Run 成功只产生 Task Artifact；是否完成由 Task 的验收策略决定。
7. **事件可重放且去重**：业务事件使用稳定 key，重扫、重启和重试不得生成重复工作。
8. **规划与实现分离**：路由、按钮、样式或预留表不代表业务已交付。

## 3. 系统分层

```text
┌──────────────────────────────────────────────────────────────┐
│ Tauri / Rust 桌面层                                          │
│ 窗口、单实例、目录、Sidecar 生命周期、托盘/通知/文件选择规划 │
└──────────────────────────────┬───────────────────────────────┘
                               │ IPC：运行状态与受控桌面能力
┌──────────────────────────────▼───────────────────────────────┐
│ React / WebView 界面层                                       │
│ 页面、表单、收件箱编排、Query 缓存、短期 UI 状态             │
└──────────────────────────────┬───────────────────────────────┘
                               │ Bearer HTTP /api/v1
┌──────────────────────────────▼───────────────────────────────┐
│ Go Sidecar 应用服务层                                        │
│ 领域校验、事务、状态命令、审计、受控 Artifact、手工 Inbox     │
└──────────────────────────────┬───────────────────────────────┘
                               │ SQLite / 受控文件系统
┌──────────────────────────────▼───────────────────────────────┐
│ 本地事实层                                                   │
│ opc-workspace.db、attachments、artifacts、invoices、backups │
└──────────────────────────────────────────────────────────────┘
```

### 3.1 当前实现

- Tauri 已具备基础窗口、单实例、数据目录和 Sidecar 启停基座。
- React 已具备三栏框架、今日/任务/项目/客户能力、Project 可编辑人工笔记、所属 Task Artifact 产出聚合与追加式活动时间线、客户本地活动时间线、受控附件与 person 显式关联、手工 Inbox 三视图/详情/分诊时间线与已有 Task 活动/历史关系管理，以及共享持久化 Session 驱动的 FocusPage、RightOverview、ticker 和恢复弹窗；任务页已接服务端分页/搜索/筛选、Task→Project→Client 客户筛选、计划/截止日期范围、非法区间查询门禁、SQLite 保存视图、根任务树、标签、批量、按钮排序和精确计划组同状态拖拽，Today 已接四组共享同日/跨日期拖拽、空精确日期/未排期落点、版本化任意日期安排、策略安全的开始/完成/开始专注快捷操作，以及直达共享编辑和版本化确认删除，Project 已接 Client 选择/筛选和独立产出/笔记/审计反馈状态。
- Go 已提供健康检查、Task/Project/Project Note/Client/Client Activity/Client Attachment/Client–Actor Link/Actor/Assignment、D1/D2、Focus Session、手工 Inbox 受理/分诊、已有 Task 关系、一次性 Reminder 和 Today 统计 API；`/health` 返回真实 app/commit/API/schema 运行事实，项目笔记、客户关联、Attachment、Activity、Focus、Inbox/关系和 Reminder 写入使用 `If-Match`、幂等快照或事务维护事实。
- SQLite 当前为 schema v25：schema v11–v22 依次交付 Focus、Inbox/Reminder/编排、设置/保存视图、Client 扩展和 Project 笔记/附件；schema v23–v25 为 follow-up Task Artifact、Task 阻塞与 Task 临期增加 Inbox 来源身份、索引、不可变与删除协调 guards，不回填历史事实或创建 demo 数据。
- 手动一致性备份与恢复已形成独立维护纵切：普通 API、Focus heartbeat 与 Reminder 扫描共享维护读锁，创建/安排恢复取得写锁；SQLite 快照、active Task file Artifact + Client Attachment、marker 和 manifest 在同卷 staging 中完整校验后原子发布。恢复安排创建当前状态回滚包并冻结写入，下一次 Sidecar 启动在 live 资源打开前交换数据库和完整 objects，失败回滚、成功以 applied 提交点防止重复执行。设置页只发起/展示数据管理 API，不复制备份事实。
- 基础业务 JSON 导出在单 SQLite 读事务中读取显式业务表白名单，以稳定表/列/行结构下载；`project_notes` 与其他业务历史进入导出，Task Artifact 与 Client Attachment 只保留数据库元数据和 active 文件摘要，运行令牌、绝对路径、identity、幂等/迁移/墓碑/派生表不进入包。它是可迁移业务快照，不替代含文件的一致性备份。
- 任务读取已返回项目/父任务标题、标签和子任务统计；任务与标签写入使用 `ETag`/`If-Match`，父子或嵌入标签事实变化会使相关任务版本失效。
- 任务批量移动项目、改计划日期、加/删标签和完整计划日期组排序都在事务中先校验全部 ID/版本，再整体提交或回滚。
- 任务响应嵌入的项目名也属于版本快照：Project 名称变化或硬删除会递增关联 Task 版本，避免基于旧项目上下文覆盖任务。
- 任务可关联项目；任务读取返回 `project_name`，项目读取从关联任务派生进度及 `actual_minutes` 合计。
- 归档项目不再接受新任务关联；schema v5 让任务、发票和客户聚合事实变化同步失效 Project `ETag`，避免基于旧汇总完成、归档或硬删除。
- Client 列表/详情/创建/编辑/停用/恢复/确认硬删除已接真实 API；创建支持首次响应快照幂等，PATCH/DELETE 使用聚合 `ETag`，项目数从 Project 实时派生，最近动态从未删除 Activity 派生。人工 note/meeting 支持幂等创建、稳定分页、活动版本化编辑和带原因软删除；Client Attachment 支持严格 multipart 上传、稳定分页、完整性下载、软删历史和聚合删除文件补偿；Client contact 支持已有/原子新建 person 二选一、单 active 关系、带原因解除和不可变历史。相关变化都会使旧 Client 版本失效。Project 客户关联变化使旧 Client 版本失效，Client 名称变化继续使旧 Project 版本失效；Invoice 强引用阻止删除，Project 可选关联按外键置空。
- schema v7 以固定 UUID 初始化唯一 owner 与 system，按历史任务完成状态幂等回填 owner Assignment 和 `migration_assignment_backfill` 事件；数据库保护内置主体、活动分派与引用历史。
- 设置中的“人员与责任”已接真实 Actor API：可管理本地 person、编辑 owner 展示名并查看 system；创建支持幂等重放，读取/更新使用 `ETag`/`If-Match`，存在活动 Assignment 时 API 与数据库共同拒绝停用。“关于”按需读取 `/health`，严格校验后展示运行事实，失败保留明确错误与重试入口。
- 设置事实层已接 schema v16 与 `GET/PATCH /api/v1/settings`：固定 workspace/general/appearance/focus 四个非敏感模块，缺失行由 GET 返回未存储默认值，PATCH 以模块版本原子保存并追加不含设置值的审计事件。前端启动门禁严格读取并将 Query 快照作为 committed，保存仅发送变化模块；历史 localStorage 只原子回填未存储模块并在验证后清理。头像仍为独立本地兼容值，受控文件导入待实现。
- 任务详情已接 Assignment API/UI：可查询当前 assignee/reviewer 与结束历史，完成首次分派、改派和结束；命令使用 Task `If-Match`/`version`、可选幂等快照和事务化 Workflow Event。完成 Task 会结束活动 Assignment，重新打开不会恢复旧记录。
- Task 已扩展为 `todo / in_progress / blocked / waiting_review / done / cancelled` 六状态，并通过 `start / block / unblock / complete / cancel / reopen` 六个显式命令改变生命周期；新建只能进入 `todo`，旧通用状态端点返回 410。开始要求活动负责人，阻塞/取消要求原因，解除阻塞由服务端恢复来源状态，完成/取消会原子结束活动 Assignment，重新打开不会恢复旧分派。
- 任务详情已提供按需加载的通用 Task Workflow Event 时间线；生命周期、Assignment 和迁移事件按时间与 `command_seq` 倒序展示，事件记录受数据库不可修改/删除保护。
- Project 创建、资料编辑、生命周期转换与永久删除也复用通用不可变 Workflow Event；producer 与原项目写命令同事务，创建幂等重放跳过 producer，事件失败回滚命令。Project 时间线按时间、命令序号和事件 ID 倒序读取，返回当前 Project 版本，不成为项目状态的第二事实来源。
- 人工 Project Note 是独立可编辑业务事实：创建、编辑和带原因软删除分别递增笔记版本及 Project 聚合版本；归档项目只读，删除历史不可再改写。它不写入或覆盖不可变 Workflow Event，Project 硬删除时随聚合级联删除。
- `review_policy = manual` 已在 Task 新建和受限编辑中开放；策略只可在 todo 且没有任何 Submission 历史时改变。manual Task 具备活动 assignee 与 owner reviewer 后，可提交摘要以及 text/link/structured/file Artifact，进入 waiting_review，由 owner 接受或要求返工。
- schema v9 和 UI 已交付 Submission/Artifact 历史、受控文件 store、安全下载、完整性状态、确认软删除、Task 聚合硬删除补偿，以及提交/审核/撤回/删除时间线。不可变 Artifact deletion tombstone 与删除事实同事务写入并在 Task 聚合删除后保留，供启动恢复判定授权删除。producer 来自活动 assignee，submitter/recorder/reviewer/withdrawer/deleter 为内置 owner。
- Tauri 与开发脚本均提供独立 Artifact root；Sidecar 在 ready 前校验 marker 的 `format_version / database_id / store_id`，并用不可变数据库身份与一次性 `artifact_store_id` 建立双向绑定，再获取进程级独占锁并协调 `.staging/objects/.trash/.quarantine`。数据库换 root、root 换数据库或第二 Sidecar 指向同一 root 时均启动失败；Task Artifact 与 Client Attachment 共享受控 object 协议并由 schema v19 阻止 ID 冲突，无引用的受控 object/trash 候选进入 quarantine 而非自动永久删除。文件内容不经过任意路径 API，数据库只保存 `objects/<uuid>`，下载前复验 size 和 SHA-256。
- Focus Core A（事实迁移）、B（API/状态机/事务）、C（前端接入与恢复）、D1（历史与周期报告）、D2a（Task 详情记录）和 D2b 日期范围回顾已交付：15 秒 Sidecar heartbeat 不递增版本，启动把遗留 active 转为 recovery_pending；Today 和周期报告只按 completed 的已关闭 interval 与 IANA 本地日边界 overlap 聚合；终态历史稳定分页，7/30 天、本月和最多 93 天自定义趋势与 Streak 均由服务端事实派生；Task 详情只按需读取关联历史，不复制或写回 Session；设置 committed/draft/preview 不改活动 Session。
- T-11A1/T-11B 已交付手工 Inbox Item 创建、三视图列表、详情编辑、单条/快照式全部已读、稍后/恢复、带原因解决/忽略、重开和 Inbox Event 时间线；T-11A2 已交付已有 Task 活动/历史关系、服务端实时进度、required 修改、带原因软解除、`open / tracking` 联动、按活动关系重开、关系事件和 Task 删除互锁；T-11A3 已交付一次性本地 Reminder、启动补偿、周期扫描和幂等 Inbox 投影。
- 当前仍未实现 Focus 高级分析/原生反馈、Client 外部活动来源/回访/财务、系统故障来源、重复提醒、Agent Runtime、数据导入和含文件外部导出包，因此完整工作编排仍是部分完成。显式 follow-up Artifact、Task 阻塞和提前 24 小时 Task 临期已投影到 Inbox；Project 产出区仍只读聚合，正文/下载/验收继续由 Task 领域处理。

### 3.2 目标扩展

- v0.1：在已交付的 Task/Project/Client、Actor/Assignment、D2、Focus Core+D1+D2a+D2b 日期范围回顾、手工 Inbox 受理/分诊、Reminder、Inbox Task 编排、基础备份闭环和业务 JSON 导出上，继续完成客户/项目增强、非 Reminder 来源投影、迁移前备份和桌面可靠性；Focus 高级分析、重复/原生通知独立延后。
- v0.2：本地 Agent Runtime、任务看板和预设自动化。
- v0.3：路线图、内容日历、高级备份配置和规划增强。
- v0.4：收入/支出、发票和客户回访。
- 待定：本地知识库与 AI 助手。

## 4. 核心领域对象与事实归属

| 对象            | 拥有的事实                                                             | 不应保存的事实                       |
| --------------- | ---------------------------------------------------------------------- | ------------------------------------ |
| Task            | 工作内容、生命周期、完成条件、验收策略、当前 Submission 指针、父子层级 | 收件箱已读/稍后、Agent 单次运行状态  |
| Project         | 项目资料、客户关联、项目状态                                           | 手工维护的任务完成百分比             |
| Project Note    | 项目人工上下文、记录时间、版本和软删除历史                             | Project 生命周期或系统命令审计       |
| Client          | 客户资料、本地活动和受控附件元数据                                     | 重复存储项目数、已付款总额或文件正文 |
| Inbox Item      | 事件来源、分诊、已读、稍后、解决策略                                   | Task 执行状态和负责人副本            |
| Actor           | 本地责任主体身份和启停状态                                             | 某项任务的当前状态                   |
| Assignment      | 当前负责人和改派历史                                                   | Task 完成状态                        |
| Agent Run       | 一次本地执行的输入快照、状态、错误和输出清单                           | Task 是否最终验收完成                |
| Task Submission | 一次提交的摘要、批次状态、提交/审核/撤回责任和时间                     | Task 内容、Artifact payload          |
| Task Artifact   | 文本、文件、链接或结构化产出、producer/recorder、完整性和软删除        | 验收结论                             |
| Reminder        | 本地触发时间和调度状态                                                 | 到期后的处理进度                     |
| Focus Session   | 专注区间、累计秒数、结束原因                                           | Task 的业务完成状态                  |
| Invoice         | 发票金额、客户、日期和业务状态                                         | 收件箱处理状态                       |
| Workflow Event  | 谁在何时做了什么、状态前后值                                           | 可被业务 API 修改的当前状态          |

## 5. 功能模块协作总览

| 模块                                       | 主要输入                                                                                   | 自己负责                                                                                                       | 主要输出 / 下游                                                               |
| ------------------------------------------ | ------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| [今日](modules/today.md)                   | Task、Focus、Inbox 派生统计                                                                | 当日执行入口、聚合展示、完整计划组排序、同日/跨日期拖拽、版本化改期，以及受策略约束的生命周期/专注快捷操作     | 计划日期事实、源/目标组顺序结果、版本化开始/完成、绑定 Focus、打开收件箱      |
| [任务](modules/tasks.md)                   | Project、Actor、Inbox 关系与来源                                                           | 唯一工单、六态生命周期、完成条件、Submission/Artifact、manual 验收与阻塞来源投影                               | Project 进度、Task 事件、阻塞 Inbox Item、后续 Inbox 进度与 Focus 工时        |
| [项目](modules/projects.md)                | Client、Task、受控文件 store                                                               | 已实现资料、生命周期、任务/Artifact 聚合、笔记、附件、活动时间线、follow-up/阻塞/Task 临期→Inbox               | 项目自身交付/验收节点来源待实现；产出操作仍直达共享任务详情                   |
| [客户](modules/clients.md)                 | Project、Invoice、Activity、受控文件 store、person Actor                                   | 当前已实现基础资料、状态、项目数/最近活动派生、Project 关联、人工时间线、Client Attachment 和显式 contact 关联 | 外部来源、回访、发票和 Inbox 来源仍属后续纵切                                 |
| [收件箱](modules/inbox.md)                 | owner 手工录入、Reminder 到期、已有/新建 Task、follow-up Artifact、Task 阻塞与临期来源     | 已交付受理分诊、来源上下文、Task 编排、自动结清/重开、来源删除协调、强制例外和运营计数/风险深链                | 输出 Event、实时进度及 Today/Sidebar 计数；系统故障等来源待 T-11E             |
| [本地提醒](modules/reminders.md)           | owner 输入与本地服务端时钟                                                                 | 一次性 scheduled/fired/cancelled 调度事实、启动补偿与稳定键 Inbox 投影                                         | Reminder Workflow Event 与 Reminder Inbox Item；原生通知和重复规则待后续      |
| [Actor](modules/actors.md)                 | 设置中的本地 person 管理、任务详情 Assignment                                              | owner/person/system 身份、人工分派、生命周期责任与 D2 producer/recorder/reviewer 审计；agent 仅保留类型边界    | Task 时间线、Submission/Artifact 责任；未来 Agent Run                         |
| [本地 Agent](modules/local-agents.md)      | agent Assignment、Task 上下文、能力授权                                                    | 单次受控执行                                                                                                   | Agent Run、Task Artifact、待验收或失败事件                                    |
| [专注](modules/focus.md)                   | 当前 Task                                                                                  | 活动 Session 和有效工时                                                                                        | Task actual_minutes、今日/统计数据                                            |
| [设置](modules/settings.md)                | schema v16 设置 API/Query committed、旧值缺失模块迁移、Actor API、`/health` 与数据维护 API | 本地偏好、person 管理、只读运行诊断、备份闭环和业务 JSON 下载；头像受控文件、导入与完整诊断待实现              | 布局、主题、Focus 默认值、Actor、运行版本、备份/导出和桌面行为                |
| [命令面板/搜索](modules/command-search.md) | Task/Project/Client/活动 Inbox 当前事实                                                    | 参数化统一本地查找、确定性相关排序、非敏感有上限最近使用与快捷操作入口                                         | 只输出稳定详情路由或触发既有受控命令，不复制业务事实                          |
| [数据管理](modules/data-management.md)     | SQLite 与本地文件                                                                          | 已实现 Task Artifact/Client Attachment 一致性、手动备份完整闭环和基础业务 JSON 导出；导入与含文件外部包仍规划  | 当前文件安全、已校验备份、恢复后的完整应用状态与业务 JSON；未来导入包与诊断包 |
| [桌面平台](modules/desktop-platform.md)    | Web 与 Sidecar 生命周期                                                                    | 原生窗口、进程、权限、运行日志和发布                                                                           | 可运行、可诊断的本地应用环境                                                  |
| [财务/发票](modules/finance-invoices.md)   | Client、Project、owner 确认                                                                | 财务与发票业务事实                                                                                             | 本地提醒、Inbox Item、客户聚合                                                |
| [客户回访](modules/client-followups.md)    | Client、Reminder、Actor                                                                    | 本地回访计划与结果                                                                                             | Inbox 到期项、客户活动                                                        |
| [路线图](modules/roadmap.md)               | Project/Task 派生进度                                                                      | 季度和里程碑规划                                                                                               | 临期/达成 Inbox 事件                                                          |
| [内容日历](modules/content-calendar.md)    | Project、Task、日期                                                                        | 内容计划和准备工作                                                                                             | 准备 Task、审核/发布时间 Inbox 事件                                           |
| [自动化](modules/automation.md)            | Workflow Event、本地时钟                                                                   | 预设规则和去重执行                                                                                             | 本地 Inbox Item、Task 或 Reminder                                             |
| [知识库](modules/knowledge-base.md)        | 本地文件                                                                                   | 导入、FTS 索引、来源定位和删除                                                                                 | 搜索结果、可选 AI 上下文                                                      |
| [AI 助手](modules/ai-assistant.md)         | 用户显式选择的本地上下文                                                                   | 本地问答、摘要和建议                                                                                           | 建议或待验收 Task Artifact                                                    |

## 6. 跨模块主流程

### 6.1 手工 Inbox 受理与分诊（当前已实现）

```text
owner 手工创建 Inbox Item(open, manual source/policy)
  → 列表按服务端时钟进入 inbox 或 snoozed 视图
  → read 只写已读；snooze/unsnooze 只控制可见性
  → resolve(reason) 或 dismiss(reason) 进入 archive，不隐式已读
  → 终态未读仍可 read；reopen 回到 open 并保留 read/triaged
  → 每个有效写入追加 Inbox Workflow Event
```

列表的 `unread_total` 是全局当前待处理视图未读，不受当前 view、搜索或优先级筛选影响。全部已读使用列表 `snapshot_at` 作为 `through_created_at` 时间截止，只处理创建与最后更新时间均不晚于 cutoff、且按该 cutoff 仍属于待处理可见范围的未读；截止后发生编辑、分诊、重开等更新的条目会保守跳过，避免旧批量操作覆盖新状态。这不是历史状态重建，也不是 `created_at + ID/序列` 的不透明严格游标；极低概率同时间戳碰撞仍可能落入同一截止范围。列表每 15 秒刷新，由服务端时钟使已到期 snooze 回到待处理视图。手工 Inbox 创建/受理/分诊流程不会隐式创建或关联 Task、Assignment 或 Reminder；已有 Task 只能由显式关系命令关联。Reminder 到期投影是独立 Sidecar 调度流程。

### 6.2 Task 手工产出验收与后续编排

```text
当前已实现：
manual Task(todo/in_progress) + active assignee + owner reviewer
  → owner 代录 summary / text / link / structured / file Artifact
  → Sidecar 派生 produced_by=assignee，recorded/submitted_by=owner
  → Task(waiting_review) + Submission(pending_review)
  → owner accept → Submission(accepted) + Task(done) + 结束活动 Assignment
  → 或 owner request_changes(reason) → Submission(changes_requested) + Task(in_progress)

当前 T-11A2：

owner 在活动 Inbox Item 关联已有 Task
  → 关系命令使用 Inbox If-Match 与幂等快照
  → 第一条活动关系使 open→tracking
  → GET 实时 JOIN Task 派生 required 进度/阻塞/待验收提示
  → required 修改或带原因软解除写追加式关系事件
  → 最后一条活动关系解除使 tracking→open
  → 活动关系阻止 Task 硬删；解除后可删且历史快照保留

当前 schema v25：
requires_followup Artifact
  → 幂等生成 Inbox Item
  → owner 批量拆分后续 Task 并分派，或复用已交付的单条已有 Task 关系
  → 必需 Task 全部 done
  → Inbox Item 派生 resolved
```

`requires_followup=true` 已以 Artifact ID 稳定 key 同事务生成 Inbox Item；未标记产出不创建。由 Inbox 拆出的下游 Task 默认不成为来源 Task 的子任务，普通子任务产出只有再次显式标记才会产生新来源。活动来源项阻止 Artifact/Task 删除；归档后删除保留来源快照并追加审计。

### 6.3 发票待办与催办（v0.4）

```text
Project 达到开票节点
  → 本地规则创建“准备发票” Inbox Item
  → 拆分核对金额 / 生成草稿 / owner 审核 Task
  → owner 手动确认发送
  → Reminder 在到期日生成去重 Inbox Item
  → owner 记录付款
  → Invoice 与 Financial Entry 在同一事务更新
  → 对应付款确认 Task 通过任务命令完成/验收
  → Inbox 领域服务按 resolution_policy 派生是否 resolved
```

付款事务不直接关闭 Inbox Item；若其他必需 Task 仍处于 blocked、waiting_review 或未完成状态，工作项继续跟踪。本地 Agent 只能生成草稿或 PDF Artifact，不能自动发送、确认付款或修改财务事实。

### 6.4 一次性本地提醒

```text
owner 创建 Reminder(scheduled)
  → 本地调度器到期扫描
  → source_event_key 幂等生成 Inbox Item(open)
  → Reminder(fired) 记录 inbox_item_id
  → owner 解决、稍后提醒或拆分 Task
```

Reminder 是调度事实；Inbox Item 是到期后的处理事实。两者不复用状态。

该链路已由 schema v14/T-11A3 交付：Sidecar 在 ready 前补扫到期 Reminder，运行中每 15 秒按稳定顺序扫描最多 100 条；每个到期项以 `reminder:<id>:due` 为唯一事件键，在一个事务中创建或复用 Reminder Inbox Item、写 system Inbox Event、标记 Reminder fired 并写 system Reminder Event。应用关闭期间不会后台唤醒，下次启动补偿；重复提醒和系统原生通知仍未实现。

### 6.5 Task 临期来源

```text
Task 具有非终态 due_date
  → Sidecar ready 前补偿并每 15 秒扫描
  → 截止时间进入未来 24 小时窗口
  → task:<task-id>:due:<due-at> 幂等生成 Inbox Item(open)
  → owner 处理、稍后、关联或拆分 Task
  → Task 改期可形成新的截止时点来源；旧事项仍由 owner 决定终态
```

该链路已由 schema v25/T-11E 第三项交付。扫描按截止时间/Task ID 稳定排序，每批最多 100 条，并在查询中排除已投影的 Task+截止时点，因此积压会继续推进。来源创建与 system Event 同事务；完成、取消、改期不会反向替 owner 解决已生成事项。活动来源阻止 Task 删除，归档后删除协调保留截止时间快照和审计。

### 6.6 专注与任务工时

```text
选择 Task
  → 创建 Focus Session(active)
  → pause/resume 使用绝对时间结算
  → 异常重启时 active 先进入 recovery_pending，由用户决定是否计入不确定间隔
  → stop 在同一幂等事务结算 interval/Session/精确秒数 ledger，每次递增 Task version，仅把新增完整分钟加入 Task.actual_minutes
  → 今日、项目和统计模块读取聚合结果
```

Task 是否完成仍由任务模块决定，专注结束不能自动完成任务。

上述链路已实现。Task Focus 余秒跨 Session 保存在 `task_focus_totals`；只有 `actual_minutes` 实际变化时，既有 trigger 才递增 Project 聚合版本。cancel/interrupted 不入账，专注结束也不自动完成 Task。

### 6.7 本地 Agent 执行（v0.2）

```text
Task 已分派给健康 agent Actor
  → owner 创建 Agent Run
  → Sidecar 校验 Adapter、Assignment、路径和能力
  → 发放短时单次能力令牌或受控进程管道
  → Adapter 本地执行并写入受控 Artifact 目录
  → Run succeeded / failed / interrupted
  → succeeded 通过与人工提交一致的 Submission/Artifact 领域命令进入 waiting_review
  → owner accept 或 request_changes
```

若 Task 已被活动 Inbox Item 跟踪，Agent 输出只更新该工作项；只有未被收件箱跟踪的 Agent Task 才生成一条去重的验收项。

## 7. 状态传播规则

### 7.1 不允许跨模块直接写状态

- Inbox 不能直接把 Task 标记完成，只能调用任务状态命令。
- Project 进度从 Task 派生，不能由项目页面单独覆盖。
- Focus 只能累计工时，不能修改 Task 完成状态。
- Agent Run 不能直接修改 Project、Client、Invoice 或财务事实。
- Automation 只能创建本地 Inbox Item、Task 或 Reminder；高风险业务写入进入人工审核。

### 7.2 派生与自动关闭

- Inbox 进度只统计 `unlinked_at IS NULL` 的活动关联。
- schema v13 的关系 GET 已实时 JOIN Task 并返回活动关系、历史和进度；Task 状态不复制到 Inbox 表，Task version 变化也不会通过 trigger 递增 Inbox version。客户端在 Task 写入成功后失效相关关系查询。
- `all_required_tasks_done` 只有至少一个活动必需 Task 且全部 done 时成立。
- cancelled、blocked、waiting_review 或失败中的必需 Task 会阻止自动解决。
- 强制关闭是 owner 的危险操作，必须二次确认、填写原因并写入审计。
- 父 Task 只有至少一个非取消子 Task 且全部完成时才允许自动推进；所有子任务取消不触发空集合完成。
- schema v15/T-11C 已交付统一 reconciliation、自动解决、自动重新打开与 `force-resolve`；T-11A2 的实时派生读模型仍是唯一进度来源。

### 7.3 当前 Task 生命周期边界

- Task 新建固定为 `todo`，状态只由显式生命周期命令改变；`PATCH /tasks/:id/status` 已废弃并固定返回 410。
- `start` 只允许 `todo → in_progress`，并要求存在 active assignee；`block` 保存原因、时间和来源状态，`unblock` 只能恢复该服务端快照。
- `complete` 只允许 `review_policy = none` 的 `todo / in_progress`。manual Task 从 todo/in_progress 通过 submit-output 进入 waiting_review，accept 到 done，request_changes 回 in_progress；提交要求 active assignee 与 active owner reviewer。
- `cancel` 不是完成：它清除阻塞事实并进入 `cancelled`；若当前待审，还先把 pending Submission 标记 withdrawn。`complete`、`cancel` 和 review accept 在同一事务结束活动 Assignment。
- accept、request_changes 和 cancel 保留 `current_submission_id` 指向最近批次；`reopen` 返回 todo 并清空该指针，但保留 Submission/Artifact/Event 历史且不恢复旧 Assignment。
- `review_policy` 在 Task 新建时可选 none/manual；既有 Task 仅在 todo 且没有任何 Submission 历史时允许改变。
- Today 活跃列表和总数、剩余、逾期、临期、预计时长排除 cancelled；实际分钟仍保留并计入所选计划日期统计。Project 继续只把 `done` 计为完成，cancelled 仍留在任务总数/剩余口径中。

## 8. 事件与幂等

### 8.1 事件来源

- v0.1：Reminder 到期、显式 follow-up Task Artifact、Task 阻塞和提前 24 小时 Task 临期已交付；备份/迁移/Sidecar 故障待实现。
- v0.2：Agent Runner 追加 Workflow Event，内置自动化投影器以统一 source_event_key 作为 Agent 失败/验收 Inbox Item 的唯一生产者；其他预设自动化也复用同一去重框架。
- v0.3：路线图里程碑、内容审核与发布时间。
- v0.4：Invoice 到期/逾期、客户回访和项目开票节点。

### 8.2 去重规则

- 每个本地业务事件产生稳定 `source_event_key`。
- Artifact follow-up key 包含 Artifact ID；Agent 验收 key 包含 Run ID。
- 自动化使用 `rule_id + event_id` 去重。
- 当前可重试写请求按 `Idempotency-Key + endpoint` 查找，并把 Task 预期版本及规范化 payload 纳入 SHA-256 请求摘要；调用 Actor 作用域仍是未来强化项。
- 同 key/endpoint 不同请求摘要返回 `409 IDEMPOTENCY_CONFLICT`。
- 幂等重放不重复写 Workflow Event。

当前可重试业务命令继续保存请求摘要与首次响应。Reminder 到期使用 `reminder:<id>:due`；follow-up Artifact 使用 `task-artifact:<artifact-id>:followup`；Task 阻塞使用 `task:<task-id>:blocked:<block-version>`；Task 临期使用 `task:<task-id>:due:<due-at>`。Artifact/阻塞分别与 Task 提交/block Event 同事务，临期来源由启动补偿和 15 秒扫描器按 Task+截止时点稳定投影；命令重放、重复扫描和重启都不重复创建，改期或重复阻塞按新事实形成独立事项。幂等 key 仍未加入调用 Actor 作用域；系统故障和其他业务来源仍待后续纵切。

schema v8 为同一请求产生的多个 Workflow Event 增加正整数 `command_seq`：自动结束 Assignment 的事件从 1 递增，Task 主事件最后写入并取得最高序号。schema v9 再增加 nullable `submission_id / artifact_id`，并校验二者与 Task 聚合、彼此批次一致。Task 与 Project 时间线均按创建时间、命令序号和事件 ID 倒序读取；历史迁移事件允许序号为空，当前每个 Project 命令只产出一条序号 1 的事件。Workflow Event 不提供修改/删除 API，数据库 trigger 也拒绝更新和删除，唯一例外是 Task 聚合硬删除时由外键把已删除的 Assignment/Submission/Artifact 关联 ID 置空，其他快照保持不变。Project 删除没有事件外键级联，因此 `project_deleted` 及之前快照继续保留在业务导出中；资源 API 在 Project 不存在后返回 404。

## 9. 本地安全与权限边界

- WebView 使用启动期随机 Bearer Token 调用普通业务 API。
- 本地 Agent 不使用 WebView Token，也不能直接打开 SQLite。
- Agent Runtime 使用专用路由/中间件和单次能力令牌，或使用受控进程管道。
- Adapter 默认只获得本次 Run 明确授权的输入、路径和输出目录。
- 跨平台进程沙箱和网络阻断必须经过 ADR 与实际验证；无法强制时 Adapter 只能保留为禁用诊断记录，正式 Agent 分派与执行入口不得启用。
- person 无账号、无登录、无远程通知；由 owner 记录线下进度和结果。
- Artifact producer 由 Sidecar 从 active assignee 派生，客户端不能上传 Actor ID 冒充产出者；Submission submitter 与 Artifact recorder 固定 owner。
- 文件 Artifact 只允许上传到带数据库绑定 JSON marker 且由 Sidecar 进程独占锁定的受控 root，数据库仅保存 `objects/<artifact-id>` 相对路径；multipart 的 manifest 必须是首个 part，之后只接受被它精确且唯一引用的文件 part。严格 JSON body、manifest 与单个 structured object 各限 1 MiB，单文件限 50 MiB、完整 multipart 限 100 MiB，服务端 HTTP read/write timeout 为 180 秒、客户端上传/下载端到端超时 120 秒。下载通过鉴权 API 重新校验大小和 SHA-256，并强制 attachment/nosniff/no-store；关键文件与目录项在成功前做耐久同步。
- Artifact 软删除需要确认、Task `If-Match` 和原因；pending-review 批次禁止删除。Task 聚合硬删除和文件软删除都通过 `.trash/` 做数据库事务补偿，并在同一事务留下不可变 tombstone；物理文件已经缺失时仍允许删除，软删记录 missing 完整性事实。
- 当前阶段不提供线上更新、云同步、远程模型或自动对外发送。

## 10. 故障与恢复协作

| 故障                            | 责任模块                         | 对其他模块的行为                                                                                                                                                                                                                                                                                                                                                                                 |
| ------------------------------- | -------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Sidecar 启动失败                | 桌面平台                         | 展示全局恢复页；业务页面不得显示伪数据；shutdown 已持有 child 时 ready 超时处理不伪造 exited，仍由 shutdown 完成等待与兜底终止                                                                                                                                                                                                                                                                   |
| Agent 中断                      | 本地 Agent + 自动化投影器        | Runner 将 Run 标记 interrupted 并追加事件；内置投影器以统一 key 创建/更新 Inbox Item，Task 保持未完成                                                                                                                                                                                                                                                                                            |
| 备份失败                        | 数据管理                         | 当前同步命令保留现有数据库/Artifact、清理未发布 staging，并返回带 request ID 的错误；桌面日志落盘和 system_maintenance Inbox 投影仍未实现                                                                                                                                                                                                                                                        |
| 恢复等待重启 / 启动 applying    | 数据管理 + 桌面平台              | 安排阶段在维护锁内创建回滚包并发布 pending，随后普通 API 返回 `RESTORE_RESTART_REQUIRED`；桌面设置页可调用 `restart_application`，先等待受管 Sidecar 真实退出再请求 Tauri 重启。浏览器开发模式不接管外部 Sidecar，并提示手动重启。下一次 Sidecar 在数据库/Artifact lease 打开前交换同父目录资源，最终校验失败恢复 old 并隔离计划，成功将 pending 原子推进为 applied 后再清理；恢复进度页仍待实现 |
| 来源资源删除（T-11E）           | 来源模块 + Inbox                 | Task Artifact、Task 阻塞与 Task 临期已实现：open/tracking 来源项阻止 Artifact/Task 删除；归档后删除原子标记 `source_deleted_at`、保留快照并显示来源已删除。其他来源仍需逐项实现；它与 schema v13 的关联 Task 删除互锁相互独立                                                                                                                                                                    |
| 关联 Task 硬删除                | Task + Inbox                     | 任一活动 Inbox 关系存在时返回 `TASK_HAS_ACTIVE_INBOX_RELATIONS`，不移动 Artifact 文件或删除聚合；用户带原因软解除后才可删除，历史关系的 `task_id` 置空而 `task_ref_id / task_title_snapshot` 与事件继续保留                                                                                                                                                                                      |
| 并发旧写入                      | Sidecar 领域服务                 | Task/Tag 当前事实、父子/标签嵌入、Assignment、生命周期、Submission/Artifact、Project/Client 聚合和 Actor 变化都会使旧 `If-Match` 或 `expected_version` 返回 409；输出前端保留 summary、text、link、structured 与浏览器 File 草稿，Client 编辑前端保留资料草稿，刷新后要求用户再次明确提交，不用旧版本自动重试                                                                                    |
| 受控文件缺失/篡改               | Task/Client/Project + 数据管理   | 保留元数据和审计，标记 missing/mismatch，拒绝下载；缺失不阻断确认软删或父聚合硬删，软删保留 missing 检查事实                                                                                                                                                                                                                                                                                     |
| 受控文件/数据库提交中断         | Task/Client/Project + 文件 store | 提交报错后查询三类数据库引用，仅删除可证明无引用的 object；模糊 COMMIT 保留给 reconcile。恢复 active trash 前校验 size/SHA，错配隔离并记 mismatch；三类 tombstone 让已授权删除与未知候选可区分，意外目录/链接不递归处理                                                                                                                                                                          |
| 数据库与 Artifact root 不匹配   | 数据管理 + 受控文件 store        | marker 的 `database_id / store_id` 分别匹配 workspace 的不可变数据库 ID 与一次性绑定 store ID；错库、换 root、未知 marker 格式或版本在 ready 前拒绝启动                                                                                                                                                                                                                                          |
| 第二 Sidecar 共用 Artifact root | 桌面平台 + 受控文件 store        | 进程级非阻塞独占锁使后启动进程在 ready 前失败，禁止双进程协调同一文件根                                                                                                                                                                                                                                                                                                                          |
| Focus 进程中断                  | Focus + Sidecar                  | 启动把遗留 active 原子改为 recovery_pending；全局不可关闭弹窗要求用户计入间隔继续、排除至最后 heartbeat 后继续，或中断。heartbeat 和活动查询不会递增业务 version                                                                                                                                                                                                                                 |

## 11. 实施依赖顺序

已落地的基础纵切包括 Task D1/D2、Project/Client、Actor/Assignment 以及 Focus Core A+B+C。后续依赖顺序为：

```text
已交付：Task D1 + D2 / Workflow Event / manual Submission / Artifact store
  → 已交付：Client 基础 CRUD / Project 客户选择与筛选
  → 已交付：Client 人工活动时间线 + 受控附件 + person 显式关联 + Project 活动事件 / 待实现：来源投影
  → 已交付：手工 Inbox Item / 受理 / 分诊 / 归档事件
  → 已交付：已有 Task 活动/历史关系 / 实时进度 / 软解除 / 删除互锁
  → 已交付：一次性 Reminder / 启动补偿 / 到期 Inbox 投影
  → 已交付：批量拆分 / 人工分派 / 自动解决；来源投影继续
  → 已交付：Focus 持久化/Task 工时/IANA Today Focus 统计、Today 完整日期分组/导航/按钮式排序、四组同日/跨日期拖拽与空精确日期/未排期落点、行内任意日期改期、安全的开始/完成/开始专注快捷操作，以及编辑/版本化确认删除入口
  → 备份恢复 / 桌面日志与故障恢复
  → v0.2 本地 Agent / 预设自动化 / Task 看板
  → v0.3 路线图 / 内容日历 / 高级数据管理
  → v0.4 财务 / 发票 / 客户回访
  → 本地知识库
  → AI 助手
```

在前置事实层未完成时，下游模块只能展示明确的占位或禁用态，不能以静态数据、无行为按钮或预留表冒充可用功能。

## 12. 跨模块验收基线

- 断开网络后，当前已实现的 Task/Assignment/manual 提交验收、Project/Client、手工 Inbox、Reminder、Task 编排、follow-up Artifact/Task 阻塞/Task 临期来源投影与删除协调、备份闭环和业务 JSON 导出均可用；其他业务来源、Client 外部活动来源和原生通知仍待交付。
- 每个业务状态有且只有一个事实源。
- 跨模块写操作具备事务、幂等和冲突检测。
- 任何来源事件重扫和重启后不重复创建工作。
- 已有 Task 关系与批量拆分写入失败均不遗留部分 Task、关系、Assignment、Inbox 状态或审计。
- person 分派明确显示仅作本地责任记录。
- manual 人工产出不能绕过 waiting_review 和 owner 验收；未来 Agent 必须复用同一 Submission/Artifact 领域命令。
- Artifact 文件不能通过任意路径访问；缺失、篡改、软删与硬删后历史均可解释且不会泄露已删正文。
- 模块删除、归档和恢复后，关联关系与历史仍可解释。
- 加载、成功、空、错误、重试、禁用和不可用状态均有真实 UI。
- 页面、API、数据迁移和验收测试同时交付后，模块才可从“骨架/部分完成”升级。
