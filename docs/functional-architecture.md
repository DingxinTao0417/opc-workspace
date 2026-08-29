# opc-workspace 整体功能架构

> 文档版本：2.79
> 日期：2026-08-29
> 依据：[PRD v9.68](opc-workspace-PRD.md)
> 当前实现基线：app v0.1.0 / API v1 / SQLite schema v39

> 2.79 说明：内容日历 CC4 拖拽新增立即视觉预移；月格按条目 IANA 排期时区归日，失败/冲突回滚卡片并回读服务端，成功后以服务端新版本收敛。schema 仍为 v39。

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
9. **层级与受理关系分离**：Task 父子层级只表达执行分解；Inbox `is_required` 只能来自显式关系事实，不能由父子层级推断或继承。

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
│ opc-workspace.db、.opc-sidecar-run.lock、artifacts、backups │
└──────────────────────────────────────────────────────────────┘
```

### 3.1 当前实现

- Tauri 已具备基础窗口、单实例、数据目录和 generation-aware Sidecar 启停基座。内置 Sidecar 每代生成新会话令牌并通过端口 `0` 重新请求动态分配；已启动 generation 只有真实 `Terminated` 才安排下一代，自动恢复最多 2 次，退避固定为 500 ms、2 s，当前代连续 Ready 30 秒才重置预算。外部开发模式、显式 shutdown 和事件流关闭但未收到 `Terminated` 都不自动重拉；并发 shutdown 调用共享同一次 stop。
- Go 受管 Sidecar 由 Tauri 注入 `OPC_EXIT_ON_STDIN_CLOSE=true`，父控制管道 EOF 进入与显式 shutdown 相同的 HTTP drain、WAL checkpoint 和数据库关闭；外部/开发模式默认 false。Sidecar 在检查 pending restore、执行迁移或打开 SQLite 前，先对数据库父目录固定 `.opc-sidecar-run.lock` 取得非阻塞 OS 独占锁；冲突立即失败且不接触数据库。
- React 已具备三栏框架、今日/任务/项目/客户能力、Project 任务树/平铺及项目内任务服务端搜索、状态/优先级/类型/标签/排期筛选和分页、可编辑人工笔记、所属 Task Artifact 产出聚合、项目级 Focus 报告/终态历史与追加式活动时间线、客户本地活动时间线、受控附件与 person 显式关联、客户回访详情管理、手工 Inbox 三视图/详情/分诊时间线与已有 Task 活动/历史关系管理，以及共享持久化 Session 驱动的 FocusPage、RightOverview、ticker 和恢复弹窗。共享 `ClientSelect` 已覆盖 Project 新建/编辑、Projects 筛选和 Tasks 筛选：固定以每页 20 条读取既有 Client API，输入经 250 ms 防抖后服务端搜索，稳定分页并传递取消信号，跨页或失败时保留当前选择，inactive Client 保持可见可选，且具备加载、空、错误重试、更多提示和 combobox 键盘语义，不再串行拉取全部 Client。共享 `ProjectSelect` 已覆盖 Task 新建/编辑、Tasks 项目筛选、批量目标项目和 Inbox 拆分任务：固定每页 20 条，250 ms 防抖搜索既有 Project API，以 `q / page / includeArchived` 隔离 Query key 并传递取消信号；候选按 ID 去重，只有显式清除才提交未归项目，选中详情或名称 fallback 保留跨页、失败及当前已归档选择，默认候选不列其他归档项目。生产路径已移除 `getAllProjects` 串行拉全。任务页的选中客户仍只作为列表查询条件，服务端沿 Task→Project→Client 当前关系筛选；计划/截止日期范围、非法区间查询门禁、SQLite 保存视图、根任务树、标签、批量、按钮排序和精确计划组同状态拖拽均保持原契约。Today 已接四组共享同日/跨日期拖拽、空精确日期/未排期落点、版本化任意日期安排、策略安全的开始/完成/开始专注快捷操作、直达共享编辑、版本化确认删除、逾期/未来 24 小时临期快捷筛选，以及受限来源筛选的客户回访待办，Project 已接独立产出/笔记/审计/Focus 反馈状态。
- 任务看板与列表消费同一个严格 Task 契约：切换看板后使用平铺服务端分页，固定显示六状态列，复用全部筛选、最多 100 项批量选择及共享详情入口；跨列拖拽只映射既有生命周期命令，经过确认、版本、负责人、原因及人工验收门禁，服务端成功前不改卡片状态。
- Go 已提供健康检查、Task/Project/Project Note/Client/Client Activity/Client Attachment/Client–Actor Link/Client Followup/Actor/Assignment、D1/D2、Focus Session、手工 Inbox 受理/分诊、已有 Task 关系、一次性与 daily/weekly Reminder、Today 统计，以及可选 Project 过滤的 Focus 终态历史/周期报告 API；Inbox 列表的受限 `source_entity_type=client_followup` 可读取真实到期回访。Task 列表提供与 Today 统计共享固定宽度 UTC 纳秒比较口径的 `due_state=overdue|due_soon`。Project 列表的每种排序均追加 `id ASC`，同名项目顺序确定，并在同一只读事务完成 `COUNT` 与当页读取。`/health` 返回真实 app/commit/API/schema 运行事实，项目笔记、客户关联、Attachment、Activity、Focus、Inbox/关系和 Reminder 写入使用 `If-Match`、幂等快照或事务维护事实。
- Project Artifact 读模型在同一只读事务返回 Artifact/Task/Submission 与 nullable follow-up：Inbox ID/version/status/policy/`source_deleted_at` 及当前 required progress。列表保留 Project 聚合数值 `ETag`，`meta.project_version` 与它表达同一 Project 并发版本；follow-up 不传播进 Project version，Inbox 写入应使用 `followup.inbox_item_version`。当前 Project UI 只深链 Inbox；所有可能改变 follow-up 的成功 Inbox mutation 会失效可信来源 Project，split 另失效 Task、Today、Project。
- React 项目详情把产出放在任务后，显示待拆分/跟进中/已解决/已忽略、required 完成度及阻塞/待验收/取消并深链 Inbox。Inbox split 对可信本地来源默认继承 Project，但每个草稿可清除/改选；独立完成条件写入 Task，person 明确为本地责任记录。活动关系和仍有实时 Task 的历史关系都用 stack-aware Modal 复用全局 Task detail。
- SQLite 当前为 schema v39：schema v11–v37 交付 Focus、Inbox/Reminder/编排、设置/保存视图、Client、Project 附件、来源 guards、重复 Reminder、受限 Automation、Agent Adapter、Client Followup、Roadmap 与 Content Item 数据契约；schema v38 增加 Content Item→Inbox 来源，schema v39 增加 Roadmap Milestone→Inbox 的本地日期补扫、版本化去重和删除协调约束。v30–v39 都不创建业务 demo 数据；Sidecar 只幂等登记默认禁用的 Automation 预设，Agent Adapter 必须由用户显式登记。
- 根级质量门禁与运行架构解耦：`check:source` 验证仓库可移植源码、文档和 Sidecar/Web 产物，`check:rust` 验证需要平台原生工具链的 Tauri/Rust 层，`check` 严格组合两者。源码门禁通过不等于桌面链接、安装包或三平台验收通过。
- 一致性备份与恢复已形成独立维护纵切：进程级数据库父目录运行锁先覆盖 pending restore、迁移与 SQLite open，进程内普通 API、Focus heartbeat 与 Reminder 扫描再共享维护读锁，创建/安排恢复取得写锁；SQLite 快照、全部 active objects/avatars、marker 和 manifest 在同卷 staging 中完整校验后原子发布。手工 `POST /api/v1/backups` 在幂等重放未命中后，迁移/导入/恢复内部链在各自不可逆边界前，统一按 SQLite 分配与数据库文件上界、active 受控文件、marker/manifest 估算载荷，增加 20% 且最低 64 MiB 余量，并只探测 backup root；恢复另把目标包 pending 副本与 plan 上界加入同一次需求。精确等于需求允许继续；空间不足/容量无法确认分别以 507/503 或启动失败安全拒绝。拒绝无备份 staging、新回滚包、业务变化或 generic incident。已有工作区启动时先执行非破坏性迁移；首个连续文件头带 `-- migration: destructive` 的迁移会触发迁移门禁。恢复安排通过容量准入后创建当前状态回滚包并冻结写入，下一次 Sidecar 启动在 live 资源打开前同时交换数据库、objects 和 avatars，失败整体回滚、成功以 applied 提交点防止重复执行。
- 健康启动后的恢复结果诊断由数据管理 API 持有：读取当前 pending、本进程 StartupRestoreResult、applied 清理残留、failed 隔离和 invalid 记录，只投影规范 ID、请求时间、状态与计数。设置页用它恢复重启门禁和展示结果；诊断不暴露路径/底层错误、不自动删除。数据库打开前则由 Sidecar stdout 的固定阶段码经 Tauri 映射为恢复页进度，二者不复用 API 或泄露恢复包身份。
- 基础业务 JSON 导出在单 SQLite 读事务中读取显式业务表白名单，以稳定表/列/行结构下载；Workspace Avatar 与 Task/Client/Project 文件只保留数据库元数据和 active 文件摘要，不嵌入正文，运行令牌、绝对路径、identity、幂等/迁移/墓碑/派生表不进入包。它是可迁移业务快照，不替代含文件的一致性备份。
- 含文件业务 ZIP 导出在维护写锁内完整生成后才响应：`manifest.json` 记录 source、业务 JSON 及全部 active 受控文件的安全相对路径、size/SHA-256，`files/` 携带正文；复制时任一文件漂移都会整体失败并清理 staging。该包排除 SQLite、identity、store marker 和运行维护事实。
- 业务 JSON 与含文件 ZIP 导入均先 strict 预检同 schema/固定表列/标量行/终态 Focus 和空目标；ZIP 额外校验 manifest、文件全集、安全路径、size/SHA-256 与数据库文件元数据。正式应用在维护写锁与备份互斥锁中再次预检后，先通过共享容量准入并创建已校验回滚备份；JSON 在单事务中写入，ZIP 先无覆盖发布文件，再于 DB 提交前复验磁盘正文，失败补偿本次文件。空间不足和容量无法确认分别返回导入专用脱敏 507/503，且不创建回滚包或改变业务事实。非空目标和跨 schema 包继续拒绝。
- 任务读取已返回项目/父任务标题、标签和子任务统计；任务与标签写入使用 `ETag`/`If-Match`，父子或嵌入标签事实变化会使相关任务版本失效。
- 任务批量移动项目、改计划日期、加/删标签和完整计划日期组排序都在事务中先校验全部 ID/版本，再整体提交或回滚。
- 任务响应嵌入的项目名也属于版本快照：Project 名称变化或硬删除会递增关联 Task 版本，避免基于旧项目上下文覆盖任务。
- 任务可关联项目；任务读取返回 `project_name`，项目读取从关联任务派生进度及 `actual_minutes` 合计。
- 归档项目不再接受新任务关联；schema v5 让任务、发票和客户聚合事实变化同步失效 Project `ETag`，避免基于旧汇总完成、归档或硬删除。
- Client 列表/详情/创建/编辑/停用/恢复/确认硬删除已接真实 API；创建支持首次响应快照幂等，PATCH/DELETE 使用聚合 `ETag`，项目数从 Project 实时派生，最近动态从未删除 Activity 派生。人工 note/meeting 支持幂等创建、稳定分页、活动版本化编辑和带原因软删除；Project complete/reopen 会把同一事务生成的 Workflow Event 投影到事件发生当下所关联 Client 的只读 `system_reference`，后续改绑不搬迁旧活动且不回填历史事件。Client Attachment 支持严格 multipart 上传、稳定分页、完整性下载、软删历史和聚合删除文件补偿；Client contact 支持已有/原子新建 person 二选一、单 active 关系、带原因解除和不可变历史。相关变化都会使旧 Client 版本失效。Project 客户关联变化使旧 Client 版本失效，Client 名称变化继续使旧 Project 版本失效；Invoice 强引用阻止删除，Project 可选关联按外键置空。
- schema v7 以固定 UUID 初始化唯一 owner 与 system，按历史任务完成状态幂等回填 owner Assignment 和 `migration_assignment_backfill` 事件；数据库保护内置主体、活动分派与引用历史。
- 设置中的“人员与责任”已接真实 Actor API：可管理本地 person、编辑 owner 展示名并查看 system；创建支持幂等重放，读取/更新使用 `ETag`/`If-Match`，存在活动 Assignment 时 API 与数据库共同拒绝停用。“关于”按需读取 `/health`；“运行诊断”再读取并白名单化 Tauri Sidecar 状态，展示环境/生命周期/版本兼容、复制脱敏摘要并下载诊断包 v1。诊断包只含版本/平台、SQLite 健康/迁移和系统维护错误码汇总，原始令牌、地址、路径、错误和业务正文不进入诊断模型或 ZIP。
- React 根节点先由桌面服务恢复闸门保护：浏览器开发模式直接放行；桌面 `starting / restarting` 时只显示安全启动/恢复页，`error` 或状态读取失败时拦截全部业务/设置 bootstrap，支持重新检查、打开日志和安全重启，且不渲染 Tauri 原始 message。受管状态携带 generation；从 ready 进入任一非 ready 状态会清除运行期连接、取消并清空 TanStack Query，下一次 ready 只有在清理完成后重挂业务树，ready generation 变化也会补偿前端漏过中间 `restarting` 的轮询。路由树另由全局渲染错误边界保护：页面或 AppShell 渲染失败时替换为安全恢复页，原始异常不显示或持久化；用户可重新渲染、返回今日，或打开位于错误边界外的设置运行诊断。路由变化会复位失败状态。
- 设置事实层已接 schema v16 与 `GET/PATCH /api/v1/settings`，schema v27 再接 `POST /settings/avatar` 与鉴权 content：头像选择只写 preview，保存时受控文件 replace/remove 与变化设置原子提交，取消恢复 committed。历史 localStorage Data URL 仅在服务端无头像时一次性导入，已存在服务端引用始终优先；验证后清理本地内容。
- 任务详情已接 Assignment API/UI：可查询当前 assignee/reviewer 与结束历史，完成首次分派、改派和结束；命令使用 Task `If-Match`/`version`、可选幂等快照和事务化 Workflow Event。完成 Task 会结束活动 Assignment，重新打开不会恢复旧记录。
- Task 已扩展为 `todo / in_progress / blocked / waiting_review / done / cancelled` 六状态，并通过 `start / block / unblock / complete / cancel / reopen` 六个显式命令改变生命周期；新建只能进入 `todo`，旧通用状态端点返回 410。开始要求活动负责人，阻塞/取消要求原因，解除阻塞由服务端恢复来源状态，完成/取消会原子结束活动 Assignment，重新打开不会恢复旧分派。
- 任务详情已提供按需加载的通用 Task Workflow Event 时间线；生命周期、Assignment 和迁移事件按时间与 `command_seq` 倒序展示，事件记录受数据库不可修改/删除保护。
- Project 创建、资料编辑、生命周期转换与永久删除也复用通用不可变 Workflow Event；producer 与原项目写命令同事务，创建幂等重放跳过 producer，事件失败回滚命令。`complete` 还在同一事务按 Project ID + 完成后 version 投影一个完成收尾 Inbox Item；`complete/reopen` 若当时存在 `client_id`，再以 Workflow Event ID 为稳定来源创建一条 Client 系统活动。任一投影失败都会回滚 Project 状态、事件与其他投影。Project 时间线按时间、命令序号和事件 ID 倒序读取，返回当前 Project 版本，不成为项目状态的第二事实来源。
- 人工 Project Note 是独立可编辑业务事实：创建、编辑和带原因软删除分别递增笔记版本及 Project 聚合版本；归档项目只读，删除历史不可再改写。它不写入或覆盖不可变 Workflow Event，Project 硬删除时随聚合级联删除。
- `review_policy = manual` 已在 Task 新建和受限编辑中开放；策略只可在 todo 且没有任何 Submission 历史时改变。manual Task 具备活动 assignee 与 owner reviewer 后，可提交摘要以及 text/link/structured/file Artifact，进入 waiting_review，由 owner 接受或要求返工。
- schema v9 和 UI 已交付 Submission/Artifact 历史、受控文件 store、安全下载、完整性状态、确认软删除、Task 聚合硬删除补偿，以及提交/审核/撤回/删除时间线。不可变 Artifact deletion tombstone 与删除事实同事务写入并在 Task 聚合删除后保留，供启动恢复判定授权删除。producer 来自活动 assignee，submitter/recorder/reviewer/withdrawer/deleter 为内置 owner。
- schema v30 已交付直属子任务自动协调：非取消直属子任务至少 1 个且全部 done、父任务为 todo/in_progress + manual、active owner/person assignee 与 active builtin owner reviewer 齐全时，由 system 创建零 Artifact 的 `origin=child_rollup` 批次并最多推进到 waiting_review。失效可撤回 pending 系统批次；accepted 父任务只在子任务完成条件失效时系统重开。manual 历史与 changes_requested 系统批次不被覆盖。
- Tauri 与开发脚本均提供独立 Artifact root；Sidecar 先在数据库父目录获取 `.opc-sidecar-run.lock`，再于 ready 前校验 marker 的 `format_version / database_id / store_id`，并用不可变数据库身份与一次性 `artifact_store_id` 建立双向绑定，随后获取 Artifact root 独占锁并协调 `.staging/objects/avatars/.trash/.quarantine`。数据库运行锁防止第二个进程恢复、迁移或打开同库，Artifact 锁防止双进程协调同一文件根，两者不可互相替代。Task/Client/Project 文件使用 `objects/<uuid>`，Workspace Avatar 使用 `avatars/<uuid>.<ext>`；schema v27 阻止四领域 ID 冲突。内容不经过任意路径 API，读取前复验 size 和 SHA-256。
- Focus Core A（事实迁移）、B（API/状态机/事务）、C（前端接入与恢复）、D1（历史与周期报告）、D2a（Task 详情记录）、Project 详情读取和 D2b 日期范围回顾已交付：15 秒 Sidecar heartbeat 不递增版本，启动把遗留 active 转为 recovery_pending；Today 和周期报告只按 completed 的已关闭正时长 interval 与 IANA 本地日边界 overlap 聚合；终态历史稳定分页，7/30 天、本月和最多 93 天自定义趋势与 Streak 均由服务端事实派生；Task/Project 详情按需读取关联历史，Project 过滤按 Task 查询时当前项目归属，均不复制或写回 Session；设置 committed/draft/preview 不改活动 Session。
- T-11A1/T-11B 已交付手工 Inbox Item 创建、三视图列表、详情编辑、单条/快照式全部已读、稍后/恢复、带原因解决/忽略、重开和 Inbox Event 时间线；T-11A2 已交付已有 Task 活动/历史关系、服务端实时进度、required 修改、带原因软解除、`open / tracking` 联动、按活动关系重开、关系事件和 Task 删除互锁；T-11A3 已交付一次性及 daily/weekly 本地 Reminder、启动补偿、周期扫描、DST 安全推进和幂等 Inbox/下一 occurrence 投影。
- 当前仍未实现 Focus 原生反馈、Client 的邮件/日历等外部活动来源及财务、每月/自定义 Reminder、Agent Runtime、非空目标/跨 schema 冲突导入，因此完整工作编排仍是部分完成。Project 生命周期的本地系统活动投影和客户回访本地计划/提醒已交付，但不代表客户互动或对外通信。Focus 分析与业务 JSON/含文件 ZIP 安全导入导出已交付；已登记来源、运行期数据库操作失败及按 1–100 GiB 设置阈值运行的低空间投影已接 Inbox；Sidecar generation-aware 有界重启、父管道 EOF 退出、数据库运行锁、双进程日志、request ID、全局恢复页和数据库打开前白名单恢复进度已交付，启动前备份选择仍待实现。T-02 仍部分完成：没有 OS Job Object、进程组或孙进程治理，hard-hung orphan 仅被运行锁挡住而不会自动回收，真实 Tauri/Sidecar 父崩溃、进程树、三平台与安装包尚未验收。

### 3.2 目标扩展

- v0.1：在已交付的 Task/Project/Client、共享服务端搜索 Client/Project 选择器、Actor/Assignment、D2、Focus、手工 Inbox/Reminder/Task 编排、Project Artifact→Inbox→Task 人工闭环、来源投影、基础备份闭环、业务导入导出和 Sidecar 有界恢复上，继续完成真实浏览器/WebView、父崩溃/进程树、三平台与安装包验收；Focus 原生反馈、重复/原生通知独立延后。v0.1 不启用 AI、LLM 或 Agent Runtime。
- v0.2：本地 Agent Runtime 和预设自动化。
- v0.3：路线图、内容日历、高级备份配置和规划增强。
- v0.4：收入/支出、发票和客户回访。
- 待定：本地知识库与 AI 助手。

## 4. 核心领域对象与事实归属

| 对象            | 拥有的事实                                                                                 | 不应保存的事实                                        |
| --------------- | ------------------------------------------------------------------------------------------ | ----------------------------------------------------- |
| Task            | 工作内容、生命周期、完成条件、验收策略、当前 Submission 指针、父子层级及直属子任务派生进度 | 收件箱已读/稍后、required 关系、Agent 单次运行状态    |
| Project         | 项目资料、客户关联、项目状态                                                               | 手工维护的任务完成百分比、Focus 汇总或 Inbox 跟进状态 |
| Project Note    | 项目人工上下文、记录时间、版本和软删除历史                                                 | Project 生命周期或系统命令审计                        |
| Client          | 客户资料、本地活动和受控附件元数据                                                         | 重复存储项目数、已付款总额或文件正文                  |
| Inbox Item      | 事件来源、分诊、已读、稍后、解决策略                                                       | Task 执行状态和负责人副本                             |
| Actor           | 本地责任主体身份和启停状态                                                                 | 某项任务的当前状态                                    |
| Assignment      | 当前负责人和改派历史                                                                       | Task 完成状态                                         |
| Agent Run       | 一次本地执行的输入快照、状态、错误和输出清单                                               | Task 是否最终验收完成                                 |
| Task Submission | 一次提交的来源、摘要、批次状态、提交/审核/撤回责任和时间                                   | Task 内容、Artifact payload                           |
| Task Artifact   | 文本、文件、链接或结构化产出、producer/recorder、完整性和软删除                            | 验收结论                                              |
| Reminder        | 本地触发时间和调度状态                                                                     | 到期后的处理进度                                      |
| Focus Session   | 专注区间、累计秒数、结束原因                                                               | Task 的业务完成状态或历史 Project 快照                |
| Invoice         | 发票金额、客户、日期和业务状态                                                             | 收件箱处理状态                                        |
| Workflow Event  | 谁在何时做了什么、状态前后值                                                               | 可被业务 API 修改的当前状态                           |

## 5. 功能模块协作总览

| 模块                                       | 主要输入                                                                                                                | 自己负责                                                                                                                                                                                  | 主要输出 / 下游                                                                                                                                                  |
| ------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [今日](modules/today.md)                   | Task、Focus、Inbox 派生统计、客户回访到期 Inbox 投影                                                                    | 当日执行入口、聚合展示、完整计划组排序、同日/跨日期拖拽、版本化改期、实时截止风险筛选、客户回访待办，以及受策略约束的生命周期/专注快捷操作                                                | 计划日期事实、源/目标组顺序结果、版本化开始/完成、绑定 Focus、打开收件箱或客户详情                                                                               |
| [任务](modules/tasks.md)                   | Project/Client 服务端选择结果、Actor、Inbox 关系与来源                                                                  | 唯一工单、六态生命周期、完成条件、Submission/Artifact、manual 验收、直属子任务汇总待验收，共享 Project 选择，以及沿 Task→Project→Client 当前关系执行客户筛选与阻塞来源投影                | Project 进度、Task 事件、阻塞 Inbox Item、后续 Inbox 进度与 Focus 工时                                                                                           |
| [项目](modules/projects.md)                | Client 服务端选择结果、Task、Focus、受控文件 store、Artifact 来源 Inbox                                                 | 已实现资料、生命周期、稳定分页读模型、任务/Artifact 聚合、nullable follow-up/实时 required 进度、笔记、附件、项目级 Focus、活动时间线及来源投影                                           | 产出状态深链 Inbox；完成收尾事项进入 Inbox；complete/reopen 向事件时关联 Client 输出只读系统活动；不复制 Inbox/Task 写事实                                       |
| [客户](modules/clients.md)                 | Project、Invoice、Activity、受控文件 store、person Actor、回访到期 Inbox 投影                                           | 当前已实现基础资料、状态、项目数/最近活动派生、供 Project/Task 使用的服务端分页搜索选择读模型、Project 关联、人工/项目状态时间线、Client Attachment、显式 contact 关联与本地回访管理      | Project complete/reopen 只读系统活动；回访到期 Inbox 投影、Today/Inbox→客户详情入口；其他外部来源和发票仍属后续纵切                                              |
| [收件箱](modules/inbox.md)                 | owner 手工录入、Reminder 到期、已有/新建 Task、follow-up Artifact、Task 阻塞/临期、Project 完成、客户回访与系统维护来源 | 已交付受理分诊、显式 required、来源 Project 继承/清除、完成条件、owner/person 分派、共享 Task 详情、自动结清/重开、来源删除协调、风险深链和客户回访来源上下文                             | 输出 Event、实时进度及 Today/Sidebar 计数；客户回访只深链客户详情；成功 mutation 失效来源 Project，split 另失效 Task/Today/Project；Task 层级不隐式创建 required |
| [本地提醒](modules/reminders.md)           | owner 输入、本地服务端时钟与浏览器 IANA 时区                                                                            | 一次性/daily/weekly 系列、独立 occurrence、scheduled/fired/cancelled、启动补偿与稳定键投影                                                                                                | Reminder Workflow Event、Reminder Inbox Item 与下一 scheduled occurrence；原生通知和复杂日历规则待后续                                                           |
| [Actor](modules/actors.md)                 | 设置中的本地 person 管理、任务详情 Assignment                                                                           | owner/person/system 身份、人工分派、生命周期责任与 D2 producer/recorder/reviewer 审计；agent 仅保留类型边界                                                                               | Task 时间线、Submission/Artifact 责任；未来 Agent Run                                                                                                            |
| [本地 Agent](modules/local-agents.md)      | agent Assignment、Task 上下文、能力授权                                                                                 | 单次受控执行                                                                                                                                                                              | Agent Run、Task Artifact、待验收或失败事件                                                                                                                       |
| [专注](modules/focus.md)                   | 当前 Task 与查询时当前 Project/Tag 关系                                                                                 | 活动 Session、有效工时、终态历史和 completed-only 周期读模型                                                                                                                              | Task actual_minutes、今日/统计数据，以及 Task/Project 详情只读历史与分析                                                                                         |
| [设置](modules/settings.md)                | schema v16 设置 API/Query committed、Actor API、`/health`、Tauri 状态与数据维护 API                                     | 本地偏好、person、诊断、手动备份容量反馈与草稿保留、备份闭环、业务 JSON 安全导入导出、含文件 ZIP 导出、全局启动故障恢复页 v1 和数据库打开前白名单恢复进度；高级导入与启动前备份选择待实现 | 布局、主题、Focus 默认值、Actor、版本、诊断、备份/导入导出和桌面行为                                                                                             |
| [命令面板/搜索](modules/command-search.md) | Task/Project/Client/活动 Inbox 当前事实                                                                                 | 参数化统一本地查找、确定性相关排序、非敏感有上限最近使用与快捷操作入口                                                                                                                    | 只输出稳定详情路由或触发既有受控命令，不复制业务事实                                                                                                             |
| [数据管理](modules/data-management.md)     | SQLite 与本地文件                                                                                                       | 已实现受控文件一致性、手工及内部自动回滚包容量准入、备份恢复、失败 Inbox，以及业务 JSON/含文件 ZIP 的空工作区安全导入导出；非空目标/跨 schema 冲突合并仍规划                              | 当前文件安全、已校验备份、恢复状态、准入反馈、失败 Inbox 与便携导入导出                                                                                          |
| [桌面平台](modules/desktop-platform.md)    | Web 与 Sidecar 生命周期                                                                                                 | 原生窗口、受管 Sidecar generation/重启预算/父管道与 shutdown、权限、运行日志和发布                                                                                                        | 可运行、可恢复、可诊断的本地应用环境                                                                                                                             |
| [财务/发票](modules/finance-invoices.md)   | Client、Project、owner 确认                                                                                             | 财务与发票业务事实                                                                                                                                                                        | 本地提醒、Inbox Item、客户聚合                                                                                                                                   |
| [客户回访](modules/client-followups.md)    | Client、Reminder、Actor                                                                                                 | 本地回访计划、终态结果、完成时原子安排下一次计划、客户详情管理、Today 待办和 Inbox→客户详情入口                                                                                           | Inbox 到期项；不自动创建客户活动或外部通信                                                                                                                       |
| [路线图](modules/roadmap.md)               | Project/Task 派生进度                                                                                                   | 已交付季度里程碑数据/API、项目关联、只读进度、服务端 Project 筛选/分页、新建/编辑/详情/归档恢复/保护性删除、同季度安全排序、年度跨季度移动和季度内精确日期调整；跨年度移动待后续          | 里程碑到期/达成已投影本地 Inbox 事件；原生通知待后续                                                                                                             |
| [内容日历](modules/content-calendar.md)    | Project、Task、日期                                                                                                     | 内容计划、六周月格、IANA/DST 安全改期、拖拽即时预移与失败回滚、准备 Task 关系、本地发布确认；CC2–CC5-B 已交付                                                                             | 准备 Task（读写已交付）；当前版本审核/发布时间到期事实投影到 Inbox（已交付）                                                                                     |
| [自动化](modules/automation.md)            | 当前消费 Project `project_completed` 与本地时钟；发票/Agent 事件待依赖交付                                              | 五个代码所有预设、版本化配置、next run、不可变 Run、attempt 与稳定去重                                                                                                                    | 当前创建本地 Inbox Item 或 Reminder；Task 动作待依赖预设交付                                                                                                     |
| [知识库](modules/knowledge-base.md)        | 本地文件                                                                                                                | 导入、FTS 索引、来源定位和删除                                                                                                                                                            | 搜索结果、可选 AI 上下文                                                                                                                                         |
| [AI 助手](modules/ai-assistant.md)         | 用户显式选择的本地上下文                                                                                                | 本地问答、摘要和建议                                                                                                                                                                      | 建议或待验收 Task Artifact                                                                                                                                       |

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

当前 Project Artifact → Inbox → Task 人工闭环：
requires_followup Artifact
  → 同事务幂等生成 Inbox Item(open)
  → Project Artifact 聚合返回 followup(Inbox ID/version/status/policy/progress)
  → Project 页面显示待拆分并深链 Inbox
  → split 默认继承可信来源 Project，可逐项清除/改选
  → 填写完成条件，分派 owner/person；manual Task 自动配置 owner reviewer
  → none Task 直接 complete；manual Task submit→waiting_review→owner accept
  → 所有活动 required Task done
  → Inbox Item automatic resolved / progress 100%
```

`requires_followup=true` 已以 Artifact ID 稳定 key 同事务生成 Inbox Item；未标记产出返回 `followup=null`。Artifact 列表的 `ETag / meta.project_version` 继续表示 Project 聚合版本，实时 follow-up 不做跨聚合版本传播；`followup.inbox_item_version` 是 Inbox 写并发事实，Project UI 当前仅用于展示/深链。成功 Inbox mutation 通过来源 Project Query 失效刷新进度，split 额外失效 Task/Today/Project。由 Inbox 拆出的下游 Task 默认不成为来源 Task 的子任务，普通子任务产出只有再次显式标记才会产生新来源。活动来源项阻止 Artifact/Task 删除；归档后删除保留来源快照并追加审计。v0.1 仅有 owner/person 人工执行和 owner manual 复核，不调用 AI/LLM 或 Agent。

### 6.3 直属子任务驱动父任务验收

```text
直接 children(parent_task_id = parent.id)
  → 排除 cancelled；要求非取消数 > 0 且全部 done
  → 再检查 parent=todo/in_progress + review_policy=manual
  → active assignee(owner/person) + active builtin owner reviewer
  → system 创建 Submission(origin=child_rollup, pending_review, 0 Artifact)
  → parent 最多进入 waiting_review
  → owner accept 才进入 done；request_changes 回 in_progress 且系统不再覆盖

pending child_rollup + 子任务条件/父任务门禁失效
  → system withdrawn
  → parent in_progress
  → 若 parent 已 blocked：保持 blocked，仅 blocked_from waiting_review→in_progress

accepted child_rollup + 子任务条件失效
  → system reopen parent 为 todo
  → 保留 accepted Submission/Event，不恢复已结束 Assignment
  → 以 visited 防循环并沿完整有效祖先链协调
```

创建、改绑/解除父级、删除、单条或批量生命周期、review accept、review policy 与 Assignment 变化都在原命令 SQLite 事务中协调受影响父任务；批量先去重父节点，再返回协调后的最终版本。迁移和启动不全库扫描历史层级。任何 manual Submission 历史、已有 pending Submission 或 changes_requested child_rollup 都优先于系统规则。该链路只复用 Task 与 Submission 事实；它不会创建 Inbox 关系，也不会继承或改写 `inbox_item_tasks.is_required`。

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

### 6.4 一次性与重复本地提醒

```text
owner 创建 Reminder(scheduled)
  → 本地调度器到期扫描
  → source_event_key 幂等生成 Inbox Item(open)
  → Reminder(fired) 记录 inbox_item_id
  → daily/weekly 按 IANA 当地日历生成下一 occurrence
  → owner 解决、稍后提醒或拆分 Task
```

Reminder occurrence 是调度事实；Inbox Item 是该次到期后的处理事实。两者不复用状态。

该链路由 schema v14/T-11A3 建立一次性事实，schema v32 扩展 daily/weekly 系列：Sidecar 在 ready 前补扫到期 Reminder，运行中每 15 秒按稳定顺序扫描最多 100 条；每个到期 occurrence 以 `reminder:<id>:due` 为唯一事件键，在一个事务中创建或复用 Reminder Inbox Item、写 system Inbox Event、标记 Reminder fired、写 system Reminder Event，并按 IANA 当地日历创建同系列唯一下一 occurrence。跨 DST 保持当地钟点；离线漏过多个周期只补当前一条并把序号推进到下一未来时刻。取消唯一 scheduled occurrence 停止系列。系统原生通知、每月/自定义日历规则仍未实现。

### 6.4.1 受限预设自动化（v0.2 首个纵切已实现）

```text
Project project_completed Workflow Event ─┐
                                          ├→ 匹配 enabled 代码预设
本地 daily/weekly IANA 计划窗口 ──────────┘
  → 生成 logical_key / dedupe_key
  → action savepoint 内创建本地 Inbox Item 或 Reminder
  → 同事务写 immutable terminal Automation Run + Workflow Event
  → 失败回滚动作，写 failed Run；1/5 分钟有界重试，最多 3 次 attempt
```

schema v33 只保存五个稳定预设的可编辑配置与 Run 快照，规则名称、触发、动作和权限由代码目录所有。Project 完成规则以 `rule_id + source_event_id` 唯一，计划规则以 `rule_id + scheduled_for` 唯一，所有 attempt 再以 `logical_key + attempt` 唯一。Project 来源调用外层 savepoint 隔离整个自动化基础设施，失败不会回滚已经完成的 Project；成功动作、Run 与审计仍原子提交。

daily/weekly 调度先执行到期重试，再按 `next_run_at/id` 扫描最多 100 条；DST 缺失分钟落到首个有效分钟，重复分钟取第一次。离线跨多个窗口只保留最新 due：旧当地日期写一条 `skipped/SCHEDULE_WINDOW_EXPIRED` 并推进，不创建过期 Reminder；当前当地日最多创建一条 Reminder。停用规则阻止新运行和后台重试但不删除历史。Automation 自身 Workflow Event 没有预设消费者，因此当前不能形成递归链；因果深度字段仍由数据库限制在 0–4。

设置界面通过 `/api/v1/automations` 读取规则、服务端预览、`If-Match` 保存/启停并展示 Run/手动重试。发票 Task 和 Agent failure Inbox 预设因依赖未交付固定 unavailable。动作执行器没有 Shell、SQL、HTTP 或外发路径；业务 JSON/ZIP 导入导出包含 Rule/Run，并在回滚包前验证稳定身份、配置、关系和 fresh-target。

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
  → 今日和 Project 卡片读取 Task.actual_minutes 聚合
  → Focus/Task/Project 详情从 Session/interval 与 Task 当前关系读取历史和统计
```

Task 是否完成仍由任务模块决定，专注结束不能自动完成任务。

上述链路已实现。Task Focus 余秒跨 Session 保存在 `task_focus_totals`；只有 `actual_minutes` 实际变化时，既有 trigger 才递增 Project 聚合版本。cancel/interrupted 不入账，专注结束也不自动完成 Task。

Project 详情读取复用 `GET /api/v1/focus-sessions` 与 `GET /api/v1/stats/focus` 的可选 `project_id`，不新增 Project 专用 Focus 表或 migration。参数严格要求 canonical UUID：非法/非 canonical 返回 400，不存在返回 404，归档 Project 可读。History JOIN 当前 Task，展示 completed/cancelled/interrupted 并稳定分页；Report 只扫描 completed Session 的闭合正时长 interval，保留既有 IANA 时区、跨午夜、DST、1–93 当地日、Streak 和零桶语义。Task 改绑会重分类旧 Session；Session 无 Task、Task 已删除或当前无 Project 时不进入项目过滤结果。

React Query 的派生缓存按项目、日期和页码隔离，失效边界固定为：Task 编辑/删除与批量 `set_project` 刷新历史+报告；批量标签变更、Tag 更新/删除与 Project 更新只刷新报告；Project 删除把报告标为 stale 但不在导航前 refetch 已删除 ID。Task 改期/排序/生命周期和 Artifact/Submission 操作不触碰这两个派生读模型；Focus stop/cancel/recover 仍按 Focus 事实变化刷新活动、历史、报告、Today、Task 与 Project。

### 6.7 本地 Agent 执行（v0.2）

传输与安全边界已由 [ADR-003](adr/003-local-agent-runtime-security.md) 冻结。当前 v0.2-A 只实现下面的诊断流；平台隔离未验证时必须在这里停止：

```text
设置登记 builtin-local-text-v1
  → Sidecar 保存代码所有 manifest（无路径、无凭据）
  → owner 以 If-Match 请求安全检查
  → 校验稳定 Adapter 身份与协议
  → 保存 blocked / PLATFORM_ISOLATION_UNVERIFIED / execution_ready=false
  → 设置展示三个未通过闸门，启用与 agent 分派继续关闭
```

登记和诊断写入 `agent_adapter_registered / agent_adapter_health_checked` Workflow Event；业务导出/导入只接受代码所有身份及 unknown/blocked 安全状态。该流不创建 agent Actor、Assignment、Run，也不启动任何进程。

下图仍是 v0.2-B/C 目标流程，不表示当前代码已实现 Runner 或 Run：

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

### 6.8 备份操作失败的系统维护来源

```text
POST /api/v1/backups 手动创建
  → 维护写锁 + 备份互斥锁内先查幂等重放；命中直接返回，不探测
  → 未命中时在 staging/VACUUM 前估算 SQLite 上界 + 经受控路径/实际大小复核的 active 文件 + marker/manifest + 20%（最低 64 MiB）余量
  → 只探测 backup root；可用量小于需求返回 507 BACKUP_SPACE_INSUFFICIENT，无法确认返回 503 BACKUP_CAPACITY_UNAVAILABLE
  → 恰好等于需求允许继续；拒绝无 staging/新包/业务变化，也不投影 backup:create

POST /api/v1/backups 通过准入后实际创建失败
  → 调用端仍收到 BACKUP_CREATE_FAILED；现有数据不变
  → Sidecar 尽力投影 source_entity_id=backup:create

POST /api/v1/backups/:id/verify 操作失败
  → 调用端仍收到 BACKUP_VERIFY_FAILED
  → Sidecar 尽力投影 source_entity_id=backup:verify

BACKUP_INVALID（损坏/篡改）
  → 不投影 Inbox；设置页已展示包无效
```

恢复演练的操作性失败或通过 manifest 校验后不可安全打开
→ 原错误响应保持不变
→ Sidecar 尽力投影 source_entity_id=backup:drill

恢复安排读取 pending/源目录/工作区身份、创建回滚点或发布计划失败
→ 原错误响应保持不变
→ Sidecar 尽力投影 source_entity_id=backup:restore

该链路复用 schema v26 的系统维护约束，当前已交付 `create / verify / drill / restore` 四个 operation。payload 只含 `component / operation / failure_code / occurred_at / message`。投影失败只记内部日志，不改变备份错误响应；`BACKUP_SPACE_INSUFFICIENT`、`BACKUP_CAPACITY_UNAVAILABLE`、`BACKUP_INVALID`、请求错误、包不存在、工作区不匹配和已有恢复计划均不投影。容量拒绝的 API 响应也不包含路径、盘符、精确容量、note 或底层探测错误。数据库/Sidecar 启动失败由下一节的安全 journal 补偿。诊断包 v1 只导出实际已投影来源的错误码、状态、数量和最近发生时间，不导出 payload 正文。

### 6.9 启动前故障 journal 与补偿投影

```text
数据库打开 / 受保护迁移 / 启动恢复 / Router / listen / ready 失败
  → 进程仍返回原失败码并输出内部错误
  → 只将白名单 kind + 稳定 UUID + UTC 时间原子写入 OPC_LOG_DIR/startup-incidents-v1.json
  → 不写 raw error / 路径 / Token / 请求正文 / 业务数据

下一次数据库成功打开并完成迁移
  → Router ready 前严格读取 journal
  → kind 映射为 database:startup / database:migration / sidecar:startup
  → 用原失败时间和稳定 incident ID 投影 system_maintenance Inbox Item + system Event
  → 全部成功后删除 journal；失败保留重试
```

journal 最多 16 条/64 KiB，同 kind 未消费前只保留最早一条。文件必须是非 symlink 普通文件，JSON 拒绝未知字段、未知 kind、非规范 UUID、非 UTC 时间和重复记录；非法文件隔离，不参与投影。投影先按稳定 `source_event_key` 查重，再检查同 source id 的 active incident，因此“数据库已提交但 journal 清理不确定”不会在用户处理旧事项后制造重复。`OPC_LOG_DIR`/`--logs` 默认落在数据库同级 `logs/`，并与 Artifact/backup root 隔离；Sidecar 与 Tauri 壳均在该目录写 5 MiB/3 归档的脱敏轮转日志，设置和全局启动故障恢复页 v1 可无路径打开目录。WebView 每次请求生成 UUID v4，Sidecar 规范化后在响应头、错误体和访问日志中复用；Tauri 生命周期日志保持独立白名单事件，不伪造 HTTP request ID。数据库打开前的恢复/迁移进度仅经过严格 stage code 传到 Tauri，启动前备份选择仍未实现。

### 6.10 运行期数据库故障投影与降级

```text
版本化 API 非预期数据库错误 / health Ping 失败 / Focus 心跳失败 / 到期来源扫描失败
  → 调用端继续收到原有安全错误码，后台循环继续记录既有脱敏日志
  → 若数据库可写：创建或复用活动的 database:runtime 系统维护 Inbox Item
  → 若数据库不可写：把 database_runtime 白名单记录写入并发安全 journal
  → 下一次健康启动在 ready 前按稳定 incident ID 补偿，并在成功后清理 journal
```

该链路不把 SQL error、数据库路径、Token、请求数据或后台任务内容写入 Inbox/journal。`database:runtime` 覆盖实际 SQLite 运行失败；主动低空间监测由下一节独立负责，不用推断替代磁盘事实。

### 6.11 主动低磁盘空间监测

```text
Sidecar ready 前 + 每 5 分钟
  → 规范化数据库父目录 / Artifact root / backup root，去除重复路径
  → 查询各根所在文件系统的调用者可用字节
  → 每轮读取 app_settings.storage；任一低于 1–100 GiB 的已保存阈值：投影 storage:low_space
  → 持续低空间不重复；全部恢复后重新允许下一次低空间 incident
  → 数据库不可写：安全 journal 延迟到健康启动补偿
```

该读操作不冻结业务写入；后台扫描在恢复维护锁下运行，pending restore 时跳过。探测失败只写固定脱敏日志，不创建错误的低空间事项。Inbox/journal 只保存固定来源和提示，不保存根路径、盘符、总量或剩余字节。阈值默认 1 GiB，可在“数据与备份”以 1–100 GiB 预览并保存，下一轮扫描生效。Windows 以卷 GUID、Unix 以设备号在单次检查进程内分组，同卷三个逻辑位置只探测一次；身份读取失败时退回规范路径独立检查，避免错误合并。鉴权 `GET /api/v1/diagnostics/storage` 支持手动刷新数据库、受控文件、备份三个逻辑位置的容量和 `healthy/low/unavailable` 状态，并只以 `shared_volume` 布尔值提示逻辑位置共享容量；响应不含卷 ID、路径、盘符或探测错误，部分失败不伪造其他位置。卷级历史趋势仍未交付。

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
- Project Artifact 聚合读取 follow-up 实时事实，但仍返回 Project 聚合数值 `ETag / meta.project_version`；Inbox/Task 变化不传播为 Project version。`followup.inbox_item_version` 独立表达 Inbox 并发边界，当前 Project UI 只深链，不直接写 Inbox。
- 成功的 Inbox 编辑、命令、关系/required 变更、split 或强制解决若携带可信来源 Project，就先取消该 Project 的在途查询，再失效 Project 查询；Artifact API 请求消费 `AbortSignal`。split 无论来源是否带 Project，还失效 Task、Today 和 Project 前缀。split API 成功响应后前端立即关闭 Modal，即使后台刷新失败也不保留可重放草稿。缓存刷新不复制跨聚合状态。
- `all_required_tasks_done` 只有至少一个活动必需 Task 且全部 done 时成立。
- cancelled、blocked、waiting_review 或失败中的必需 Task 会阻止自动解决。
- 强制关闭是 owner 的危险操作，必须二次确认、填写原因并写入审计。
- 父 Task 只统计直属子 Task；至少一个非取消子 Task且全部 done 时才满足子任务门禁，所有子任务取消不触发空集合完成。满足子任务门禁仍必须通过 manual/assignee/builtin owner reviewer 门禁，且系统最多推进到 waiting_review。
- schema v15/T-11C 已交付统一 reconciliation、自动解决、自动重新打开与 `force-resolve`；T-11A2 的实时派生读模型仍是唯一进度来源。
- Inbox 的 required 集合只来自显式活动关系，Task 父子层级和 child_rollup 不自动加入该集合；父任务状态变化只会让既有显式关系重新计算。

### 7.3 当前 Task 生命周期边界

- Task 新建固定为 `todo`，状态只由显式生命周期命令改变；`PATCH /tasks/:id/status` 已废弃并固定返回 410。
- `start` 只允许 `todo → in_progress`，并要求存在 active assignee；`block` 保存原因、时间和来源状态，`unblock` 只能恢复该服务端快照。
- `complete` 只允许 `review_policy = none` 的 `todo / in_progress`。manual Task 从 todo/in_progress 通过 submit-output 进入 waiting_review，accept 到 done，request_changes 回 in_progress；提交要求 active assignee 与 active owner reviewer。
- `cancel` 不是完成：它清除阻塞事实并进入 `cancelled`；若当前待审，还先把 pending Submission 标记 withdrawn。`complete`、`cancel` 和 review accept 在同一事务结束活动 Assignment。
- accept、request_changes 和 cancel 保留 `current_submission_id` 指向最近批次；`reopen` 返回 todo 并清空该指针，但保留 Submission/Artifact/Event 历史且不恢复旧 Assignment。
- `review_policy` 在 Task 新建时可选 none/manual；既有 Task 仅在 todo 且没有任何 Submission 历史时允许改变。
- `origin=manual` 的历史、已有 pending 或 `origin=child_rollup/status=changes_requested` 会阻止系统覆盖。pending child_rollup 的子任务/父任务门禁失效由 system 撤回；accepted child_rollup 只在直属子任务条件失效时重开父任务，单纯 policy/Assignment 失效不会重开 accepted 父任务。
- blocked 父任务不会被自动改成其他状态；若其待审来源失效，阻塞原因/时间与 blocked 状态保留，只把 `blocked_from_status` 从 waiting_review 更新为 in_progress。
- Today 活跃列表和总数、剩余、逾期、临期、预计时长排除 cancelled；实际分钟仍保留并计入所选计划日期统计。截止风险列表以请求捕获的 Sidecar UTC `now` 派生：逾期为 `< now`，临期为 `[now, now+24h]`，二者排除 done/cancelled，并与 Today 统计及截止排序复用相同固定宽度 UTC 纳秒键；`due_from/due_to` 仍是 UTC 日期片段范围，不能与 `due_state` 混用或冒充滚动窗口。Project 继续只把 `done` 计为完成，cancelled 仍留在任务总数/剩余口径中。

## 8. 事件与幂等

### 8.1 事件来源

- v0.1：Reminder 到期、显式 follow-up Task Artifact、Task 阻塞、提前 24 小时 Task 临期、备份四类操作性失败（不含可解释容量准入拒绝）、数据库启动/迁移、Sidecar 启动、运行期数据库操作失败和可配置低空间监测已交付。
- v0.2 当前：Project complete/reopen 已追加 Workflow Event；首个自动化消费者只订阅 `project_completed`。未来 Agent Runner 追加事件后，才启用固定的 Agent 失败预设；Automation Run 自身事件不进入当前消费者。
- v0.3：内容审核/发布时间与路线图里程碑到期/达成已投影为本地 Inbox 事件；路线图季度内精确日期拖拽和键盘输入已交付，跨年度移动与原生通知仍待。
- v0.4：Invoice 到期/逾期、客户回访和项目开票节点。

### 8.2 去重规则

- 每个本地业务事件产生稳定 `source_event_key`。
- Artifact follow-up key 包含 Artifact ID；Agent 验收 key 包含 Run ID。
- 自动化事件首轮使用 `rule_id + source_event_id`、计划首轮使用 `rule_id + scheduled_for`，所有 attempt 使用 `logical_key + attempt` 去重；目标对象另使用稳定来源键防止重放。
- 当前可重试写请求按 `Idempotency-Key + endpoint` 查找，并把 Task 预期版本及规范化 payload 纳入 SHA-256 请求摘要；调用 Actor 作用域仍是未来强化项。
- 同 key/endpoint 不同请求摘要返回 `409 IDEMPOTENCY_CONFLICT`。
- 幂等重放不重复写 Workflow Event。

当前可重试业务命令继续保存请求摘要与首次响应。Reminder 到期使用 `reminder:<id>:due`；follow-up Artifact 使用 `task-artifact:<artifact-id>:followup`；Task 阻塞使用 `task:<task-id>:blocked:<block-version>`；Task 临期使用 `task:<task-id>:due:<due-at>`；系统维护来源使用 `system:<component>:<operation>:<incident-id>`，且同一 source id 在 open/tracking 时只允许一个活动 incident。Artifact/阻塞分别与 Task 提交/block Event 同事务，临期来源由启动补偿和 15 秒扫描器按 Task+截止时点稳定投影；除容量准入拒绝外，已登记的备份操作性失败直接尽力投影，启动前或运行期数据库不可写失败用稳定 journal 延迟投影，均不改变原错误。手工 `BACKUP_*`、导入 `IMPORT_BACKUP_*`、恢复 `RESTORE_ROLLBACK_*` 容量错误、`BACKUP_INVALID` 与其他可解释业务结果不投影。手工备份幂等重放还在容量探测之前返回，避免因当前容量状态改变而破坏首次成功响应。命令重放、重复扫描和重启都不重复创建活动事项，改期、重复阻塞和新故障按新事实形成独立事项。幂等 key 仍未加入调用 Actor 作用域；其他系统故障和业务来源仍待后续纵切。

路线图到期使用 `roadmap:<milestone-id>:due:<milestone-version>`，达成使用 `roadmap:<milestone-id>:achieved:<milestone-version>`；到期扫描同时按里程碑 ID 与纯日期 `target_date` 查询历史来源，所以标题编辑或同季度重排造成的无关版本增长不会再次提醒同一计划日期。改期或状态语义改变会解决活动来源，归档终结来源，删除只在来源终态后标记 `source_deleted_at` 并保留快照。纯日期以 `Options.Now()` 所在位置的本地日历日比较，不能强制解释为 UTC 零点。

父任务自动协调追加 system Actor 的 `task_parent_review_requested / task_parent_review_withdrawn / task_parent_reopened` Workflow Event；请求/撤回关联对应 child_rollup Submission，重开保留既有 accepted 批次历史。它不是可重扫来源投影：协调发生在原业务写事务中，生命周期幂等重放不得重复创建 Submission 或 Event。

schema v8 为同一请求产生的多个 Workflow Event 增加正整数 `command_seq`：自动结束 Assignment 的事件从 1 递增，Task 主事件最后写入并取得最高序号。schema v9 再增加 nullable `submission_id / artifact_id`，并校验二者与 Task 聚合、彼此批次一致。Task 与 Project 时间线均按创建时间、命令序号和事件 ID 倒序读取；历史迁移事件允许序号为空，当前每个 Project 命令只产出一条序号 1 的事件。Workflow Event 不提供修改/删除 API，数据库 trigger 也拒绝更新和删除，唯一例外是 Task 聚合硬删除时由外键把已删除的 Assignment/Submission/Artifact 关联 ID 置空，其他快照保持不变。Project 删除没有事件外键级联，因此 `project_deleted` 及之前快照继续保留在业务导出中；资源 API 在 Project 不存在后返回 404。

## 9. 本地安全与权限边界

- WebView 使用启动期随机 Bearer Token 调用普通业务 API。
- 本地 Agent 不使用 WebView Token，也不能直接打开 SQLite。
- Agent Runtime 不开放 HTTP；每个 Run 使用 Sidecar 创建的短生命周期子进程和匿名 stdin/stdout 管道，进程内 nonce 只绑定本次协议会话。
- Adapter 默认只获得本次 Run 明确授权的输入、路径和输出目录。
- 跨平台进程沙箱和网络阻断必须经过 ADR 与实际验证；无法强制时 Adapter 只能保留为禁用诊断记录，正式 Agent 分派与执行入口不得启用。
- person 无账号、无登录、无远程通知；由 owner 记录线下进度和结果。
- Artifact producer 由 Sidecar 从 active assignee 派生，客户端不能上传 Actor ID 冒充产出者；manual Submission submitter 与 Artifact recorder 固定 owner，零 Artifact 的 child_rollup Submission submitter 固定内置 system。
- 文件 Artifact 只允许上传到带数据库绑定 JSON marker 且由 Sidecar 进程独占锁定的受控 root，数据库仅保存 `objects/<artifact-id>` 相对路径；multipart 的 manifest 必须是首个 part，之后只接受被它精确且唯一引用的文件 part。严格 JSON body、manifest 与单个 structured object 各限 1 MiB，单文件限 50 MiB、完整 multipart 限 100 MiB，服务端 HTTP read/write timeout 为 180 秒、客户端上传/下载端到端超时 120 秒。下载通过鉴权 API 重新校验大小和 SHA-256，并强制 attachment/nosniff/no-store；关键文件与目录项在成功前做耐久同步。
- Artifact 软删除需要确认、Task `If-Match` 和原因；pending-review 批次禁止删除。Task 聚合硬删除和文件软删除都通过 `.trash/` 做数据库事务补偿，并在同一事务留下不可变 tombstone；物理文件已经缺失时仍允许删除，软删记录 missing 完整性事实。
- 当前阶段不提供线上更新、云同步、远程模型或自动对外发送。

## 10. 故障与恢复协作

| 故障                            | 责任模块                         | 对其他模块的行为                                                                                                                                                                                                                                                                                                                                                                                                                             |
| ------------------------------- | -------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Sidecar 启动失败                | 桌面平台                         | **已交付 v1**：桌面根闸门在 `starting/restarting/error` 时拦截业务页和设置 bootstrap，提供状态重查、打开脱敏日志目录和安全重启，不展示原始 message。内置启动失败可消耗有界重试预算；若尚未创建 child，受管应用重启无需伪造退出结果即可继续。数据库打开前的恢复/迁移固定阶段进度已显示；启动前备份选择仍待实现                                                                                                                                |
| Sidecar 运行中意外退出          | 桌面平台 + Web                   | 已启动 generation 只有真实 `Terminated` 才按 500 ms、2 s 最多重拉两次；当前代连续 Ready 30 秒重置预算。每代生成新 token 并重新请求动态 port，非 ready 清连接和全部 TanStack Query，generation 改变补偿遗漏的 `restarting`。外部模式、显式 shutdown、仅事件流关闭均不自动重拉                                                                                                                                                                 |
| Agent 中断                      | 本地 Agent + 自动化投影器        | Runner 将 Run 标记 interrupted 并追加事件；内置投影器以统一 key 创建/更新 Inbox Item，Task 保持未完成                                                                                                                                                                                                                                                                                                                                        |
| 备份操作失败                    | 数据管理 + Inbox                 | 创建/校验/恢复演练/恢复安排的操作性失败分别尽力创建 `backup:create` / `backup:verify` / `backup:drill` / `backup:restore` Inbox Item，只记录固定安全字段并保持原错误响应。手动创建的 `BACKUP_SPACE_INSUFFICIENT` / `BACKUP_CAPACITY_UNAVAILABLE` 准入拒绝，以及 `BACKUP_INVALID` 等可解释结果不投影；UI 保留 note 并提示清理或刷新，启动应用失败转入下一行的 journal 补偿                                                                    |
| 数据库/Sidecar/存储故障         | 数据管理 + 桌面 + Inbox          | 数据库启动/迁移和 Sidecar 启动失败先写独立白名单 journal；运行期数据库失败及低空间先直接投影，数据库不可写时同样降级 journal。下一次健康启动在 ready 前补偿。稳定 incident ID 防模糊清理重放；原错误、路径、卷 ID、容量和敏感内容不进入 journal/Inbox。Sidecar/Tauri 壳脱敏日志、WebView→Sidecar request ID、全局启动故障恢复页 v1、数据库打开前白名单恢复进度、可配置低空间监测、物理卷同卷去重和无路径手动容量检查已交付；卷级趋势仍待实现 |
| 恢复等待重启 / 启动 applying    | 数据管理 + 桌面平台              | 安排阶段在维护锁内创建回滚包并发布 pending，随后普通 API 返回 `RESTORE_RESTART_REQUIRED`；桌面设置页可调用 `restart_application`。若受管 child 存在，只有 code 0 且无 signal 的真实退出才允许重启应用；内置启动失败未创建 child 时允许继续，延迟到达的干净退出确认后可再次请求。健康启动后只读诊断 API 汇总恢复结果；数据库打开前恢复、验证、收尾及迁移阶段由白名单 stdout 协议显示，不暴露恢复包身份或路径；启动前备份选择仍待实现          |
| 来源资源删除（T-11E）           | 来源模块 + Inbox                 | Task Artifact、Task 阻塞、Task 临期、Project 完成、Content Item 与 Roadmap Milestone 已实现：open/tracking 来源项阻止来源硬删除；允许删除前原子标记 `source_deleted_at`、保留快照并显示来源已删除。系统维护来源禁止 `source_deleted_at`。其他来源仍需逐项实现；它与 schema v13 的关联 Task 删除互锁相互独立                                                                                                                                  |
| 关联 Task 硬删除                | Task + Inbox                     | 任一活动 Inbox 关系存在时返回 `TASK_HAS_ACTIVE_INBOX_RELATIONS`，不移动 Artifact 文件或删除聚合；用户带原因软解除后才可删除，历史关系的 `task_id` 置空而 `task_ref_id / task_title_snapshot` 与事件继续保留                                                                                                                                                                                                                                  |
| 并发旧写入                      | Sidecar 领域服务                 | Task/Tag 当前事实、父子/标签嵌入、Assignment、生命周期、Submission/Artifact、Project/Client 聚合和 Actor 变化都会使旧 `If-Match` 或 `expected_version` 返回 409；输出前端保留 summary、text、link、structured 与浏览器 File 草稿，Client 编辑前端保留资料草稿，刷新后要求用户再次明确提交，不用旧版本自动重试                                                                                                                                |
| 受控文件缺失/篡改               | Task/Client/Project + 数据管理   | 保留元数据和审计，标记 missing/mismatch，拒绝下载；缺失不阻断确认软删或父聚合硬删，软删保留 missing 检查事实                                                                                                                                                                                                                                                                                                                                 |
| 受控文件/数据库提交中断         | Task/Client/Project + 文件 store | 提交报错后查询三类数据库引用，仅删除可证明无引用的 object；模糊 COMMIT 保留给 reconcile。恢复 active trash 前校验 size/SHA，错配隔离并记 mismatch；三类 tombstone 让已授权删除与未知候选可区分，意外目录/链接不递归处理                                                                                                                                                                                                                      |
| 数据库与 Artifact root 不匹配   | 数据管理 + 受控文件 store        | marker 的 `database_id / store_id` 分别匹配 workspace 的不可变数据库 ID 与一次性绑定 store ID；错库、换 root、未知 marker 格式或版本在 ready 前拒绝启动                                                                                                                                                                                                                                                                                      |
| 第二 Sidecar 共用数据库父目录   | 桌面平台 + 数据管理              | 固定 `.opc-sidecar-run.lock` 的非阻塞 OS 独占锁在 pending restore、迁移和 DB open 前使后启动进程立即失败且不接触数据库；锁文件可保留，所有权只由 OS lock 表示。hard-hung orphan 会继续持锁并阻止新进程，但当前不会被自动识别或回收                                                                                                                                                                                                           |
| 第二 Sidecar 共用 Artifact root | 桌面平台 + 受控文件 store        | Artifact root 进程级非阻塞独占锁使后启动进程在 ready 前失败，禁止双进程协调同一文件根；它不替代数据库父目录运行锁                                                                                                                                                                                                                                                                                                                            |
| Focus 进程中断                  | Focus + Sidecar                  | 启动把遗留 active 原子改为 recovery_pending；全局不可关闭弹窗要求用户计入间隔继续、排除至最后 heartbeat 后继续，或中断。heartbeat 和活动查询不会递增业务 version                                                                                                                                                                                                                                                                             |

## 11. 实施依赖顺序

已落地的基础纵切包括 Task D1/D2、父任务自动待验收、Project/Client、Actor/Assignment 以及 Focus Core A+B+C。后续依赖顺序为：

```text
已交付：Task D1 + D2 / Workflow Event / manual + child_rollup Submission / Artifact store
  → 已交付：Client 基础 CRUD / Project 表单与 Project/Task 筛选共享服务端搜索、稳定分页的 ClientSelect
  → 已交付：Project 稳定分页读模型 / Task 新建与编辑、Tasks 筛选与批量目标、Inbox 拆分共享服务端搜索的 ProjectSelect
  → 已交付：Client 人工活动时间线 + 受控附件 + person 显式关联 + Project Workflow Event→Client 只读系统活动来源投影
  → 待实现：邮件、日历、回访等其他真实 Client 来源；回访与财务保持 v0.4
  → 已交付：手工 Inbox Item / 受理 / 分诊 / 归档事件
  → 已交付：已有 Task 活动/历史关系 / 实时进度 / 软解除 / 删除互锁
  → 已交付：一次性 Reminder / 启动补偿 / 到期 Inbox 投影
  → 已交付：Project Artifact nullable follow-up/实时 required 进度与 Inbox 深链；批量拆分继承/清除来源 Project、完成条件、owner/person 人工分派、manual owner 验收及自动解决金链
  → 已交付：Focus 持久化/Task 工时/IANA Today 与周期统计、Task/Project 详情历史、Project 7/30 天/本月分析，以及 Today 完整日期分组/导航/按钮式排序、四组同日/跨日期拖拽与空精确日期/未排期落点、行内任意日期改期、安全的开始/完成/开始专注快捷操作、编辑/版本化确认删除入口和服务端截止风险快捷筛选
  → 已交付数据库启动/迁移、Sidecar 启动、运行期数据库与可配置低空间故障补偿、generation-aware 有界重启、数据库运行锁、父管道 EOF 退出、数据库打开前白名单恢复进度、Sidecar/Tauri 壳脱敏轮转日志、request ID 和全局恢复页；继续启动前备份选择与真实父崩溃/进程树/三平台验收
  → v0.2 本地 Agent / 预设自动化
  → v0.3 路线图 / 内容日历 / 高级数据管理
  → v0.4 财务 / 发票 / 客户回访
  → 本地知识库
  → AI 助手
```

在前置事实层未完成时，下游模块只能展示明确的占位或禁用态，不能以静态数据、无行为按钮或预留表冒充可用功能。

## 12. 跨模块验收基线

- 断开网络后，当前已实现的 Task/Assignment/manual 提交验收、直属子任务驱动的系统待验收、Project/Client、手工 Inbox、Reminder、Task 编排、follow-up Artifact/Task 阻塞/Task 临期/备份四类操作性失败/数据库启动与迁移/Sidecar 启动/运行期数据库操作失败/可配置低空间来源投影、手工与内部自动回滚包容量准入、备份闭环、业务 JSON 与含文件 ZIP 导出均可用；容量准入拒绝不伪造故障 Inbox，Client 外部活动来源和原生通知仍待交付。
- 每个业务状态有且只有一个事实源。
- 跨模块写操作具备事务、幂等和冲突检测。
- 任何来源事件重扫和重启后不重复创建工作。
- 已有 Task 关系与批量拆分写入失败均不遗留部分 Task、关系、Assignment、Inbox 状态或审计。
- person 分派明确显示仅作本地责任记录。
- Project Artifact 对未标记产出返回 `followup=null`；已标记产出返回 Inbox ID/version/status/policy/source deletion 和实时 required progress。Artifact 列表 `ETag / meta.project_version` 继续是 Project 聚合版本，不能用于 Inbox 写入。
- Go 金链覆盖 `requires_followup → split(owner/person + manual owner reviewer) → complete + submit(waiting_review) → accept → Inbox automatic resolved/100%`；person 本地责任提示与提交载荷、关系行打开共享 Task 和 Project/Task/Today 缓存失效另有前端自动化证据。
- manual 人工产出不能绕过 waiting_review 和 owner 验收；未来 Agent 必须复用同一 Submission/Artifact 领域命令。
- child_rollup 只统计直属非取消子任务，必须至少 1 个且全部 done，并满足 manual/active owner-or-person assignee/active builtin owner reviewer；系统不得越过 waiting_review，不得覆盖 manual 或 changes_requested，也不得用父子层级隐式改写 Inbox required。
- 子任务/门禁失效撤回 pending 系统批次；blocked 保持 blocked 并修正来源状态；accepted 父任务只在子任务条件失效时系统重开，历史保留且 Assignment 不恢复。
- Artifact 文件不能通过任意路径访问；缺失、篡改、软删与硬删后历史均可解释且不会泄露已删正文。
- 模块删除、归档和恢复后，关联关系与历史仍可解释。
- Project Focus 读取严格区分 canonical UUID 400、不存在 404 与归档可读；Task 改绑/删除后的当前归属重分类、completed-only 报告口径和终态审计历史均可解释，且未新增 schema migration。
- Project 新建/编辑、Projects 筛选和 Tasks 筛选共用的 ClientSelect 只消费既有分页 Client 读接口：每页 20 条、250 ms 服务端搜索、稳定分页、请求取消、当前选中保留、inactive 可见可选和完整反馈均有自动化证据；真实浏览器键盘/焦点、窄屏及 1,000/10,000 条数据性能仍须专项验收，不能由组件测试替代。
- Task 新建/编辑、Tasks 项目筛选、批量目标项目和 Inbox 拆分任务共用的 ProjectSelect 只消费既有分页 Project 读接口：每页 20 条、250 ms 服务端搜索、`q / page / includeArchived` Query key、请求取消、按 ID 去重、显式清除和选中详情/名称 fallback 均有实现证据；默认不列归档项目但保留当前归档选择。服务端同名排序追加 `id ASC`，`COUNT` 与当页读取共用一个只读事务。真实浏览器键盘/焦点、窄屏及 1,000/10,000 条项目数据性能仍须专项验收，不能由组件测试替代。
- Project→Inbox→Task 的自动化金链不等于全部人工端到端浏览器验收；真实浏览器/WebView 的深链返回、弹层焦点、窄屏及 1,000/10,000 条项目/任务和 Inbox 长列表性能仍须专项完成。
- 受管 Sidecar 状态只使用 `starting / restarting / ready / error` 并携带 generation；有界重启、30 秒预算重置、每代新 token/动态端口申请、非 ready 查询清理、真实 `Terminated` 门禁、父管道 EOF、数据库运行锁、安全应用重启和并发 shutdown 共享 stop 均已编写测试并完成源码静态复核；本机仍未执行 Rust 测试。
- 上述自动化证据不等于真实 Tauri/Sidecar 父进程崩溃、OS 进程树、三平台或安装包验收；当前没有 Job Object、进程组或孙进程治理，hard-hung orphan 只会被数据库运行锁阻止，不会被自动回收。
- 根 `pnpm check:source` 必须覆盖格式、文档、Web 类型/测试/构建与 Go 无缓存测试/vet/Sidecar 构建；根 `pnpm check` 必须在此基础上继续执行 Rust/Tauri 检查和 Rust 测试。任一层失败都不得以较窄的定向命令替代为“完整门禁通过”。
- 加载、成功、空、错误、重试、禁用和不可用状态均有真实 UI。
- 页面、API、数据迁移和验收测试同时交付后，模块才可从“骨架/部分完成”升级。
