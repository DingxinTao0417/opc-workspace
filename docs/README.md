# opc-workspace 文档中心

H5-G54 将保存会话中仍处于 `waiting`/`running` 的后台计划续办以 metadata-only `continuation` 项纳入统一 Agent Inbox；左侧新增“后台续办”分组，可精确回到原会话计划并以只读 handoff 重新核对。无 workspace grant、计划正文、Provider/工作区私密资料、自动审批/重试/停止/执行或新 schema/scope/Runner wire。见 [AI 助手 H5-G54](modules/ai-assistant.md#后台计划续办纳入统一-agent-inboxh5-g542026-09-23)。

H5-G53 为执行详情补充精确 retry 父链回看；只读导航不自动重试、不恢复、不继承权限。见 [AI 助手 H5-G53](modules/ai-assistant.md#agent-run-重试链的精确回看h5-g532026-09-23)。

H5-G52 将 Task Agent Run 的活动、未被精确 retry 替代的失败/中断，以及 `pending`/`retained` 交付以 metadata-only `agent_run` 项并入统一 Agent Inbox；连续 retry 父项和已验证 restart 来源去重，前端计划内项可直达精确执行抽屉并可将 metadata 重新授权交回 AI，计划外项继续使用完整 Run 账本。无正文、输入快照、文件/路径或 Provider 私密字段，无新 schema/scope/Runner wire 或执行能力。见 [AI 助手 H5-G52](modules/ai-assistant.md#agent-run-纳入统一-agent-inboxh5-g522026-09-23)。

H5-G51 将普通聊天的停止意图先写入既有工作流事件再发本地取消信号；续办可先撤销进程内 worker，worker 先收尾时停止事务回看本续办最新轮次，对精确 `queued/streaming/cancelled` Generation 幂等补记事件，且只在停止事实提交后确认 HTTP 成功。重启能区分已持久确认的主动停止与意外/未确认中断，均不重放 Provider 请求；无 API/schema/scope 变化。Go 确定性回归覆盖取消顺序、竞态补记及恢复分类；真实 Provider 网络竞态待验收。见 [AI 助手 H5-G51](modules/ai-assistant.md#普通生成停止意图的持久化h5-g512026-09-23部分实现)。

H5-G50 为新确认的 `agent_delegate.spawn` 增加安全启动恢复：仅持久排队、没有 accepted/停止事件且无 Generation 的精确冻结子委派可经 Coordinator 重新入队；accepted/停止事件及旧无排队证据记录均不会重放。父方停止意图先落工作流事件再发信号；无 schema/API/scope 变化。确定性 Go 测试通过，真实 Provider 重启专项待完成。见 [AI 助手 H5-G50](modules/ai-assistant.md#已确认子委派的安全启动恢复h5-g502026-09-23部分实现)。

H5-E39 将 Sidecar 启动恢复标记的对话生成中断纳入保存会话的 Agent Inbox；用户手动核对并决定是否继续，用户主动取消仍排除，不自动重放或复用授权。无 schema/API 变化，定向 Go/前端验证通过。见 [AI 助手模块](modules/ai-assistant.md)。

H5-G49 让本条消息已授权的 Agent 工作区双面板请求可选带主面板初始宽度比例（0.25–0.75）；不扩大标签能力或内容访问，用户仍能手工重调，旧事件兼容。定向 Go/前端契约测试通过，真实 Provider/桌面验收待完成。见 [AI 助手模块](modules/ai-assistant.md#智能体请求工作区的初始分栏比例h5-g492026-09-23)。

H5-G48 为 Agent 工作区双标签增加有界比例调整：用户可拖动分隔条或用键盘调整 25%–75% 的主/次面板比例，双击恢复均分；单标签无分隔条，不影响外层聊天比例、工具或权限。确定性前端测试通过，桌面 WebView 指针验收待完成。见 [AI 助手模块](modules/ai-assistant.md#智能体工作区双面板比例调整h5-g482026-09-23)。

H5-G44 为 Task Agent Run 增加单 Sidecar 有界 FIFO 调度：最多 8 个执行 Worker + 32 个排队 Run；任务原生启动、AI 提案确认和重试共用事务准入，满载返回 429 且不更改任务/Proposal。启动恢复与 Worker 退出按创建时间/ID 补位，pending 产出登记不占容量且历史积压不删除。与 AI 子会话 H5-G41 配额独立；不新增 schema/scope/Runner wire。见 [AI 助手模块](modules/ai-assistant.md#task-agent-run-的共享准入与-fifo-执行队列h5-g442026-09-23) 与 [本地 Agent 模块](modules/local-agents.md#task-agent-run-的有界-fifo-调度h5-g442026-09-23)。

H5-G41 为 Sidecar 内的 AI 子会话 spawn/follow-up 增加有界准入：最多 8 个 Provider 尝试并发、最多 32 个已接受排队项；单项与批量审批提交前原子预留，满载则事务回滚并让提案保持待确认，busy 退避释放执行槽。UI 明确说明拒绝原因并保留批次选择供人工重试。仅为进程内调度上限，不覆盖 Task Agent Run/普通对话，不跨进程、不持久化或重启重放；无 schema/scope/Runner wire 变化。见 [AI 助手模块](modules/ai-assistant.md#子智能体全局并发准入与有界排队h5-g412026-09-23部分实现)。

H5-G39 将父会话右侧的子 Generation 停止扩展为最多四项的显式原子批量操作：整批重验、任一项漂移则无一项停止，`202` 仍仅代表请求已接受。不会触及未选兄弟、Task Agent Run 或远端撤回；见 [AI 助手模块](modules/ai-assistant.md) 与 [Local Agent 边界](modules/local-agents.md)。

H5-E36 使 AI 可在 `workspace_ui+work` 下建议打开已存在的任务保存视图；经用户确认后任务页重读并应用当前筛选，失效时不展示未筛选任务。见 [保存视图定位](modules/ai-assistant.md#受确认的任务保存视图定位h5-e362026-09-21)。

H5-E35 让智能体请求的内嵌浏览器标签动作在本地获得可见执行回执；用户可另行手动把不含网页内容的结果带回对话，不自动授予模型浏览器能力。见 [浏览器动作回执](modules/ai-assistant.md#浏览器动作的本地执行回执与显式交接h5-e352026-09-21)。

H5-E34 将右侧 Task Artifact 预览接回原智能体对话：人工两步带入仅含身份的复查问题，重新选择本条权限并手动发送；预览正文不自动送给模型。见 [右侧产出原位续办](modules/ai-assistant.md#右侧产出的原位智能体续办h5-e342026-09-21)。

H5-E33 允许用户确认后把单项任务产出直接在智能体右侧工作区只读预览，保持对话并支持标签/分栏；完整批次页仍可进入。见 [对话内产出预览](modules/ai-assistant.md#任务产出的对话内右侧预览h5-e332026-09-21)。

H5-E32 将 AI 受确认的本地导航延伸到单项任务产出：服务端核验归属，用户点击后打开指定批次的只读产出卡片，文件下载和模型文件读取保持独立。见 [任务产出精确定位](modules/ai-assistant.md#任务产出的受确认精确定位h5-e322026-09-21)。

H5-E30 扩展智能体的受确认本地记录定位：项目笔记、客户活动/回访、账本与发票各需匹配读取范围，服务端推导从属关系，用户点击后才打开精确详情。见 [精确记录导航](modules/ai-assistant.md#智能体精确记录导航扩展h5-e302026-09-21)。

H2-W13 为已选任务交接补上同次应用会话内的返回断点：原筛选/页码/视图恢复，全部 Task 重新读取成功才恢复勾选和最新版本；失败则不恢复部分选择。见 [返回断点](modules/ai-assistant.md#已选任务返回断点h2-w132026-09-21)。

H2-W12 让智能体按 1–20 个精确 Task ID 成组重读当前状态、优先级、日期与版本；任一缺失整组失败，选择集不成为写入权限。见 [精确任务读取](modules/ai-assistant.md#已选任务精确成组读取h2-w122026-09-21)。

H2-W11 让任务页用户勾选的最多 20 项任务人工交接到智能体；只传规范 ID，重新授权、读取真实版本后才能提出待确认批量建议。见 [已选任务交接](modules/ai-assistant.md#已选任务交给智能体h2-w112026-09-21)。

H2-W10 使工作台任务页与智能体都能成组设置任务优先级或明确时区的截止时间；AI 仍须一张人工确认卡且任一失败整批回滚。见 [批量优先级与截止时间](modules/ai-assistant.md#批量优先级与截止时间h2-w102026-09-21)。

H2-W9 允许对 1–20 个已有任务精确查询当前责任并在一张人工确认卡里成组设置/结束负责人或审核人，复用原生 Assignment 事务，整批成功或回滚，不自动启动 Agent。见 [已有任务成组责任变更](modules/ai-assistant.md#已有任务成组责任变更h2-w92026-09-21)。

H2-W8 允许智能体在项目批量建任务时逐项可选初始负责人，用一张卡确认任务、责任及人工审核人；确认事务整体成功或回滚，不自动启动 Agent。见 [批量创建与初始分派](modules/ai-assistant.md#批量建任务的可选初始分派h2-w82026-09-21)。

H2-W7 允许智能体把同一项目拆成 1–20 个新任务，先展示完整草案，经一次人工确认后原子创建，并返回每个任务的精确入口；不自动执行。见 [批量建任务](modules/ai-assistant.md#同项目批量建任务确认h2-w72026-09-21)。

H5-G13 把待确认操作的大参数定义延迟到业务领域指南成功接受后，减少首轮提示占用；单消息授权和人工审批不变。见 [AI 工具目录](modules/ai-assistant.md#待确认操作工具按领域加载h5-g132026-09-21)。

H5-G12 为有界自动续办增加单独同意的普通重启后安全等待态恢复；默认仍中断，运行中的未知请求和物理备份恢复不重放。见 [AI 重启续办边界](modules/ai-assistant.md#等待中自动续办的显式跨重启恢复h5-g122026-09-21)。

H2-W5 让智能体按工作台项目页条件只读分页查询项目组合与实时任务进度；项目页可人工交接当前筛选并从原 URL 返回，不复制卡片结果、不外发客户/合同/发票/描述，也不自动修改项目。见 [AI 项目组合查询](modules/ai-assistant.md#项目组合结构化查询h2-w52026-09-21)。

H5-G11 在运行内提示预算紧张时仅收起较早、可重查的只读工具结果；最近结果与操作/权限回执保留，界面和成功回复披露需重新查询。见 [AI 运行内结果收缩](modules/ai-assistant.md#运行内较早只读结果收缩h5-g112026-09-21)。

H2-W4 为智能体增加与工作台同口径的路线图年季/项目结构化查询，只读分页，不改变里程碑审批边界。见 [AI 路线图查询](modules/ai-assistant.md#路线图结构化查询h2-w42026-09-21)。

H5-G10 让主/侧权限请求卡在有界续办仍活动时以明确按钮先停止续办，成功后才准备原单消息授权面板；不自动授权或发送。见 [人工交接](modules/ai-assistant.md#权限请求与自动续办的一步式人工交接h5-g102026-09-21)。

H5-G7 让生成中已出现的权限请求可立即忽略，并在成功保存时保持关闭；见 [活动权限请求](modules/ai-assistant.md#生成中忽略权限请求h5-g72026-09-21)。

H5-G6 让权限请求在打开核对面板后继续保留，取消或暂缓不丢失原会话及续办队列入口；见 [权限核对](modules/ai-assistant.md#权限核对中保留续办请求h5-g62026-09-21)。

H5-G5 把已保存且仍开放的权限请求加入 Agent Inbox，点击只返回原会话核对，不在队列授权或展示 scope；见 [权限请求续办](modules/ai-assistant.md#权限请求进入续办队列h5-g52026-09-21)。

H5-G3/G9 允许交互对话和有界后台续办显式请求缺失的工作台权限；后台不弹即时卡，只在成功保存的原会话与 Agent Inbox 留下待核对建议。请求不是授权，用户仍须停止活动续办、核对并另发新消息。见 [后台权限交接](modules/ai-assistant.md#后台缺失权限的人工作业交接h5-g92026-09-21)。

H5-G2 为后台续办增加最近24小时终态的有界只读回看与原会话入口，状态读取失败不再伪装为无活动；见 [AI助手模块](modules/ai-assistant.md#后台续办结果可见性h5-g22026-09-21)。

H5-G1 在AI独立轨道新增单独人工授权的有界后台续办（schema077，本地实现与确定性验收完成），不是默认常开。H5-G12 仅让另行选择的安全等待态跨普通重启，未知请求不重放；轮数/期限、等待审批、停止及便携导出排除规则见 [AI助手模块](modules/ai-assistant.md#有界后台续办h5-g12026-09-21)。

H5-G4/schema078 为成功的保存会话补上权限请求卡恢复与忽略/消费账本；它不是工作台授权或自动执行。见 [AI 权限请求恢复](modules/ai-assistant.md#权限请求卡的保存与恢复h5-g42026-09-21)。

H3-C3 已接人工启用的 Agent 失败诊断：未来真实失败→持久本地 Inbox→精确运行及原对话；五个预设均可用但默认关闭。通知重试不执行模型，来源查询需 work+outputs，历史导入不携带 Run，删除有来源互锁/审计。模块整体仍部分完成，详见 [自动化模块](modules/automation.md#agent-失败诊断闭环h3-c32026-09-21)。

H2-I/J/K 已将客户主档、现有联系人关系和本地 person 创建/修改接入逐项人工确认。person 读取只含名称、状态、版本，不外发备注/metadata；备注只接受用户本轮明示值。停用在确认事务中重验活动责任、客户关系和计划回访；删除、特权身份编辑、账号创建、外部联系、附件与项目解绑不开放。

AI 独立轨道 H5-D 已接会话持久计划与主/侧计划卡（schema 076），分析自报和真实操作回执分开，后续消息重新授权后读取接续；H5-E18 在计划提交后只以 `generation_id/version/step_count` 立即失效当前会话的计划卡，不携带计划正文或增加执行能力；不是自治多步执行。见 [AI 助手模块](modules/ai-assistant.md) 与 [Harness 阶段计划](plans/agent-harness-phases.md)。

H3-F/H3-G 已把知识库来源与索引管理接入独立 `knowledge_actions`：只读 `knowledge_library` 仅返回来源/索引元数据（不外发库内正文、哈希、文件字节或路径），`knowledge_source.create` 可把用户本轮明确提供的文本存为新来源（`.txt`/`.md`/`.markdown`、≤24,000 字节，预览只含名称/类型/字节数/SHA-256/开头摘录），`knowledge_source.reindex/delete` 与 `knowledge_index_job.retry/cancel` 经冻结预览、独立人工同意与原生共享事务执行；删除另需勾选并永久移除来源、索引与受管副本，创建/重建/重试只新开 attempt，取消不删除资料。见 [知识库模块](modules/knowledge-base.md) 与 [AI 助手模块](modules/ai-assistant.md)。

H2-U 已把任务保存视图接入智能体：`work` 下只读 `workspace_task_views`（名称、筛选定义、版本；不含任务正文与查询结果），`work+actions` 下 `task_view.create/update/delete` 经完整人工预览与版本比较后复用原生事务，删除需另行勾选；视图是本地筛选预设，不自动应用也不改变任务。见 [Task 模块](modules/tasks.md)。

Harness 现在会在**尚未输出任何内容**时对瞬时上游故障（连接中断、408/429/5xx、提前 EOF）自动重试最多 2 次并汇报到实时进度；一旦有内容到达即不再重试，取消优先。见 [ADR-025](adr/025-ai-reliability-confirmations-and-evaluation-identity.md) 与 [AI 助手模块](modules/ai-assistant.md)。

H4-J/H4-AE 让侧栏续办队列里的知识库索引失败、自动化运行失败与未成功/待登记的 Agent 执行可以“交给智能体”：只暂存标签、原生入口与该条元数据加固定提示词，进入 `/ai` 后可在主对话或右侧侧聊带入问题、重新选择权限才发送，不自动重试或恢复任何记录，也不读取右侧文件/浏览器/终端内容。H4-AM 另允许用户从已读取的桌面 Git 审查面板明确带入有界 status/diff 快照：交接卡再次确认且用户发送后才到 Provider，不含本机路径、不增加 workspace grant，不能续读仓库、运行 Git/Shell/终端或提交推送。H4-AH 进一步把设置页已保存本地人员行接入同一交接机制，只带 person ID 和固定提示词，不复制备注/metadata；H4-AI/H4-AJ/H4-AK/H4-AL 把已登记本地 Agent Adapter、已保存 AI 供应商、数据/备份设置与运行诊断接入无授权排查交接，只带安全 ID/页面级状态，不复制配置、密钥、备份内容、路径、日志或诊断包，也不授予检查/启停/连接测试/启动 Run/备份恢复/导入导出/重启能力。见 [AI 助手模块](modules/ai-assistant.md)。

H4-AN 补齐同一交接链的本地文本预览：只有用户在已读取的普通文本预览点击、在主/侧交接卡再次带入并发送，文件名、页面可见大小/截断状态及有界文本快照才会到 Provider。快照没有 root ID、路径、目录授权、文件 ID、哈希或未显示内容，不增加 `agent_files` 或任何工作台 scope；AI 不能续读、搜索、下载、保存或修改文件，也不能运行 Git/Shell/终端/浏览器/网络操作。见 [AI 助手模块](modules/ai-assistant.md#本地文本预览显式快照交接h4-an2026-09-20)。

H4-AO 补齐手动终端的显式输出交接：只有终端已有输出且用户点击、在主/侧交接卡再次带入并发送，近期有界输出尾部才会到 Provider。快照不含 PTY/root ID、工作目录、路径、环境、输入缓冲或滚动历史，不增加 workspace scope 或终端能力；AI 不能续读、启动、写入、调整或关闭终端，也不能执行 Shell/文件/Git/浏览器/网络操作。见 [AI 助手模块](modules/ai-assistant.md#手动终端输出显式快照交接h4-ao2026-09-20)。

H4-AQ 补齐当前网页地址的显式交接：只有用户在当前活动 Chromium 标签点击、在主/侧交接卡再次带入并发送，已移除 query、fragment 与账号信息的 HTTP(S) 地址才会到 Provider。快照不含网页标题/正文/截图、Cookie、登录态、历史、下载、tab/root ID 或本机路径，不增加 workspace scope、浏览器或网络能力；AI 不能访问、抓取、点击、填写或控制网页。见 [AI 助手模块](modules/ai-assistant.md#当前网页地址显式交接h4-aq2026-09-20)。

H4-AP 让右侧“子智能体”执行账本只读显示当前主对话的计划汇总和未替代步骤标题/状态，支持返回原计划、精确建议及经验证的工作台记录；读取失败明确提示并可重试。不显示审批预览、产出正文或本地工作区内容，也不能在右栏发送、授权、确认、启动、取消、重试或恢复。见 [AI 助手模块](modules/ai-assistant.md#当前对话计划回到右侧执行工作区h4-ap2026-09-20)。

本目录集中维护 opc-workspace 的产品范围、整体功能架构和模块级实现契约。

[ADR-012](adr/012-ai-run-steps-and-local-metrics.md) H4-B 已补齐主/侧聊天实时执行进度及同序号恢复；只报告步骤/时间，断流结果不误报完成，不扩大工具或业务操作权限。整体强化仍在进行。

H3-C1/C2 已接通[自动化](modules/automation.md)规则/运行元数据读取及人工审批启停、有限配置；启用或修改已启用规则需单独确认持续效果，停用不撤销已捕获事件，失败 Run 可经人工核对原快照、单独勾选后重试，区分审批完成与实际成功/失败。H4-E 已接通具体规则/Run 定位、尝试链与原对话往返；无新增迁移，整体智能体强化尚未完成。

> 当前代码基线为 app v0.1.1 / API v1 / SQLite schema 79。schema 079 在既有备份门禁后重建保存会话权限建议账本，扩大下一条消息完整范围建议的容量，保留历史请求与决定状态；当前开发库已完成该升级，最近一次启动生成了 schema 79 回滚备份。PRD v10.55 与 [ADR-025](adr/025-ai-reliability-confirmations-and-evaluation-identity.md) 收口 AI 运行隔离/预算、流终态、应用级恢复、任务确认事务、记忆决定、分段压缩和 dataset v4 配置身份评测；[ADR-026](adr/026-local-knowledge-pdf-extraction.md) 交付知识库 PDF 提取与页码定位（schema 070）；[ADR-027](adr/027-builtin-agent-executor-and-run-lifecycle.md) 交付内置 Agent 执行器、Run 生命周期及 H5-C 受控文件输入/单文件与有界多文件输出（schema 071/074/075）；[ADR-029](adr/029-agent-workspace-capabilities.md) 增加工作台查询、受人工确认的领域命令、AI→Agent Run 桥接、可恢复产出登记、单消息 `agent_files` 授权、H5-E1–E10 续办来源与工作区入口，并新增 H5-E13 的 UI-only `workspace_ui` 固定标签导航、H5-E14 的 `workspace_browser` 用户确认新标签请求、H5-E15 的业务记录定位、H5-E16/H5-E17 的 `workspace_ui+work+outputs` Agent Run/Task Submission 定位，以及 H5-E18 的计划提交 metadata-only 即时失效回执和 H4-AQ 当前网页地址双重人工交接。它们都不自动执行或读取面板/网页/执行内容；H5-E16/E17 的 owner Task 只由服务端推导，H5-E18 不传计划正文，H4-AQ 只传移除 query/hash/credentials 的用户选中地址，前端不能接收模型提供的路径、URL 或 Task 关系。H4-AP 在右侧 Agent Run 账本增加当前主对话计划的只读状态镜像。无文件+text 保持 v1，受控输入/单 file 输出使用 v2，2–4 个冻结 files 输出使用 v3；文件访问与执行独立确认，Run 成功仍须人工验收。任意 filesystem/Shell/Git 自动操作、自治调度、业务冲突合并、外部备份目录和覆盖仍待。当前 Windows x64 已完成 Tauri 原生链接、Rust 测试及未签名本地打包；实机模型、真实故障与跨平台发布验收不由隔离自动化代替。

H2-D 已接通已有任务的当前/历史责任查询、首次分派、改派和结束责任的逐项人工审批，复用 Task 版本、父任务待验收与历史事务；分派本身不启动 Agent。H5-A 另以独立 `agent_execution` 权限、冻结执行身份和二次人工确认启动精确 Run，确认事务提交后才启动。详见 [Actor 责任分派](modules/actors.md) 与 [本地 Agent](modules/local-agents.md)，整体 H2–H5 仍在推进。H2-E1 另接通单次产出授权下的提交批次、摘要/验收意见和批次产出查询；查询本身不写入。H2-E2 另接通 work+outputs+actions 下的非文件提交和当前批次接受/返工，经完整人工核验及单独勾选后共享领域事务落地；H4-F 已接精确批次只读页、按需产出核验/人工下载和返回来源对话，历史批次不被最新提交替代；实际文件内容仍需人工检查，真实模型/桌面及整体编排未据此宣称完成，见 [Task 模块](modules/tasks.md)。

AI 独立轨道 H2-F 已接通 [项目笔记](modules/projects.md#ai-项目笔记桥接h2-f) 的单次授权读取及新建/修改/软删除人工审批，含完整正文、版本重验、删除额外同意、同事务回滚及实际回执；H4-G 进一步接通 Project+Note 双身份的精确定位、删除元数据查看与原对话往返，独立详情不依赖当前列表分页或筛选，查看不执行操作或恢复权限。整体状态不变。

AI 独立轨道 H2-C5 已接通 [Inbox 快照式全部已读](modules/inbox.md#ai-独立轨道快照式全部已读adr-029-h2-c5)：模型只能使用 `workspace_search(inbox_item)` 的服务端 cutoff 提议，人工卡绑定全局候选数量与 `ID+version` 指纹；确认时范围变化拒绝，执行复用原生事务且只改已读状态。批量以 Proposal 作为回执身份并报告真实 cutoff/count；API v1/schema 73 不变，完整多步编排仍待。

## 阅读顺序与事实优先级

H3-D1 已接通 AI 已验证来源到[知识库原片段](modules/knowledge-base.md)的本地核验与返回对话；H3-D2 增加逐条消息选择版本化来源的检索/完整片段读取，只有成功读取的内容可引用，来源变化拒绝旧授权。工具正文不另存快照；H4 已接入非持久引用终态回传、短时内存回读与主/侧对话来源卡，缺失信息不冒充无来源或完整回复。真实模型验收及整体强化目标仍未完成。

AI 独立轨道 H3-B1–B3 已补客户活动/回访的单次授权分页读取，以及回访创建/编辑/取消/跳过/完成续排/原子重排的人工确认闭环；完成须额外核实实际结果与时间，复用本地域事务与 Inbox 投影，不联系客户。另支持活动创建/修改/软删除审批，删除需独立勾选；schema 073 保留旧卡并扩大长正文审批容量。H4-D 已接通具体活动/回访与原对话往返；整体 H2–H5 仍在推进，见 [回访模块](modules/client-followups.md) 和 [阶段计划](plans/agent-harness-phases.md)。

1. [产品需求文档（PRD v10.55）](opc-workspace-PRD.md)：产品范围、版本边界、数据/API 目标契约和当前状态。
2. [整体功能架构](functional-architecture.md)：模块如何协作、事件如何流转、谁拥有哪类事实。
3. [模块文档](modules/README.md)：单个模块的用户流程、数据、API、依赖、实施阶段和验收条件。
4. 仓库代码与测试：判断“现在实际实现了什么”的最终证据。
5. 当前 React 页面与 `styles.css`：判断现有视觉和交互事实；历史 HTML 原型已于 2026-08-27 从仓库移除。

发生冲突时，当前实现事实以代码和测试为准；目标范围以 PRD 为准；跨模块关系以整体功能架构为准；模块文档负责展开落地细节。

## 核心模块

| 模块           | 当前状态                                                                                                                                                            | 目标版本                    | 文档                                 |
| -------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------- | ------------------------------------ |
| 今日工作台     | 部分完成（T-06A–H、截止风险、客户回访待办，以及右侧真实客户动态/临近路线图节点已交付）                                                                              | v0.1                        | [today.md](modules/today.md)         |
| 任务管理       | 部分完成（事实层、D1/D2、筛选/保存视图、共享服务端 Client/Project 选择、计划组拖拽、受控跨列看板与父任务自动待验收已交付）                                          | v0.1                        | [tasks.md](modules/tasks.md)         |
| 项目管理       | 部分完成（含 Artifact nullable follow-up/实时 required 进度、四种跟进状态、Inbox 深链、任务/Focus/Client 活动协作）                                                 | v0.1                        | [projects.md](modules/projects.md)   |
| 客户管理       | 部分完成（基础资料、共享分页搜索选择器、Project 关联、人工活动/附件/person、回访 API/到期 Inbox 投影、详情管理及 H2-I/J/K AI 主档、联系人关系与人员档案审批已交付） | v0.1；回访/财务 v0.4        | [clients.md](modules/clients.md)     |
| 收件箱工作编排 | 部分完成（人工闭环含来源 Project 继承/清除、完成条件、person 本地责任、共享 Task 详情、缓存失效与 automatic resolved/100% 金链）                                    | 人工闭环 v0.1；Agent v0.2   | [inbox.md](modules/inbox.md)         |
| 本地提醒       | 一次性及 daily/weekly/weekdays/monthly Reminder、启动补偿与到期 Inbox 投影已完成                                                                                    | v0.1；复杂规则/原生通知后续 | [reminders.md](modules/reminders.md) |
| Actor 与分派   | 人工闭环已交付；v0.2 增加受 Adapter 门控的真实 agent 关联与任务启动前初始化检查                                                                                     | v0.1 / v0.2                 | [actors.md](modules/actors.md)       |
| 专注与工时     | Core A+B+C+D1+D2a、日期范围回顾与项目详情 Focus 读取已完成；原生反馈延后                                                                                            | v0.1                        | [focus.md](modules/focus.md)         |

## 平台与共享能力

| 模块                       | 当前状态                                                                                                                                                                                                         | 目标版本            | 文档                                               |
| -------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------- | -------------------------------------------------- |
| 本地 Agent Runtime         | Windows 内置 Runner、Run 界面、可恢复 text/单 file/有界多 file 提交、H5-C 受控文件输入与初始化/显式启停已交付；Agent Run 已以 metadata-only 方式进入统一 Agent Inbox；其他平台、external、任意工具与自治多步仍待 | v0.2                | [local-agents.md](modules/local-agents.md)         |
| 设置                       | 部分完成                                                                                                                                                                                                         | v0.1 / v0.2         | [settings.md](modules/settings.md)                 |
| 命令面板与搜索             | 核心本地搜索、详情直达、本地最近使用、脱敏运行诊断/诊断包和全局渲染错误恢复完成；OS 快捷键待后续                                                                                                                 | v0.1                | [command-search.md](modules/command-search.md)     |
| 数据、受控文件、备份与恢复 | 迁移、Artifact store、备份/恢复完整闭环、每日计划/启动补偿/自动包安全保留、业务 JSON/含文件 ZIP 空目标及同 schema 零主键冲突追加已交付；启动前备份选择、外部目录、冲突合并及升级待实现                           | v0.1；高级配置 v0.3 | [data-management.md](modules/data-management.md)   |
| 桌面平台与发布             | 部分完成（Sidecar 有界恢复/父管道/运行锁/并发 shutdown、托盘最小源码闭环和运行诊断能力快照已交付；托盘原生链接、真实父崩溃/进程树、三平台与安装包仍待验收）                                                      | v0.1 发布闸门       | [desktop-platform.md](modules/desktop-platform.md) |

## 后续业务与规划模块

| 模块             | 当前状态                                                                                                                                                                                                                                                                                | 目标版本         | 文档                                               |
| ---------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------- | -------------------------------------------------- |
| 收入、支出与发票 | 部分完成：账本/统计、发票/PDF/到期来源已实现；AI H3-E1 独立财务查询与精确往返已接通，H3-E2 收支创建/修改/作废审批已接通，H3-E3 发票草稿/状态审批已接通，H3-E4A 草稿删除审批已接入，H3-E4B PDF 生成/替换审批及精确人工下载已接入，H3-E5 CSV 范围审批及精确人工下载已接入，完整验收待后续 | v0.4             | [finance-invoices.md](modules/finance-invoices.md) |
| 客户回访         | C2–C5 数据/API、原子下一次计划、到期 Inbox 投影、详情管理及 Today/Inbox 入口完成                                                                                                                                                                                                        | v0.4             | [client-followups.md](modules/client-followups.md) |
| 路线图           | R2/R3/R5 完成，R4 同季度排序、跨季度/跨年度移动与季度内精确日期拖拽已交付；H2-G 已接 AI 读取与受审批的新建/修改/归档/恢复                                                                                                                                                               | v0.3             | [roadmap.md](modules/roadmap.md)                   |
| 内容日历         | CC1–CC5-B、CC6-A、指定详情 URL 与 H2-H/R/T/AD AI 安全读取、受审批操作、删除审批和用户独立核实后的本地发布事实登记已交付；拖拽/键盘改期即时预移且失败回滚，不自动外发                                                                                                                    | v0.3             | [content-calendar.md](modules/content-calendar.md) |
| 预设自动化       | 首个纵向切片完成                                                                                                                                                                                                                                                                        | v0.2             | [automation.md](modules/automation.md)             |
| 本地知识库       | TXT/Markdown/PDF（ADR-026 页码定位）、Actor、FTS5/中文检索、定位、重建、删除、清单与 ADR-010 AI6 显式片段已交付；授权引用、来源变化待后续                                                                                                                                               | 独立轨道         | [knowledge-base.md](modules/knowledge-base.md)     |
| AI 助手          | Provider/Harness/记忆/显式上下文/citation、dataset v4 配置身份评测/人工审计、steps/用量趋势，以及 ADR-025 可靠性、确认与恢复已交付；真机场景待专项验收                                                                                                                                  | 待定（独立轨道） | [ai-assistant.md](modules/ai-assistant.md)         |

## 全局产品边界

- 所有核心业务、Actor、任务、收件箱、提醒、产出和运行记录默认只保存在本机。
- v0.1 不引入账号、多人登录、远程任务领取、云同步或线上工作流。
- v0.1 不调用 AI/LLM，不创建或运行 Agent；Project Artifact→Inbox→Task 只使用 owner/person 与 owner manual review。AI 助手按 ADR-004 在独立轨道交付（远程 Provider + 只读会话 + 语义建任务确认卡片），不并入 v0.1–v0.4。
- `person` Actor 只记录线下责任，不会向对方发送任务或授予应用权限。
- manual Artifact 的 producer 由当前 active assignee 派生；内置 owner 负责代录、提交、审核、撤回和删除，不能由客户端伪造 Actor ID。
- Task file Artifact、Client Attachment、Project Attachment 与 Workspace Avatar 只保存在 Sidecar 声明的同一受控目录并经鉴权 API 下载；受控根通过身份 marker、Artifact root 锁、耐久同步与 quarantine 防止错库、双写和误删。数据库父目录另用固定 `.opc-sidecar-run.lock` 在任何恢复、迁移或打开前阻止第二个 Sidecar 接触同库。应用已能管理 SQLite+active files 内部备份，以及业务 JSON/含文件业务 ZIP 的空目标和同 schema 零主键冲突追加；预检可只读列出非空目标表/主键重叠、目标文件碰撞并分类跨 schema 方向，真实冲突策略、UUID 重映射与版本升级仍未实现。
- 实际 Agent 执行归入 v0.2，必须使用受控本地 Adapter、专用鉴权和可验证的隔离边界。
- Agent Run 成功只表示产生了结果；高风险或要求审核的任务必须由 owner 验收后才完成。
- 发票、客户沟通、付款确认、数据删除等高风险动作不得由 Agent 无审核完成。
- 当前阶段使用签名离线更新包，不依赖在线 Updater。

## 模块文档维护模板

每份模块文档至少维护以下内容：

1. 定位与非目标。
2. 当前实现状态与代码证据。
3. 目标功能和用户流程。
4. 数据事实、API、状态机和事件。
5. 与其他模块的输入、输出和依赖。
6. 分阶段实施顺序。
7. 可验证的验收标准。

代码交付一个纵向切片时，应同时更新对应模块文档的当前状态、PRD 的实现追踪和实际验证证据。只有页面、按钮外观、静态样式或数据库预留表时，不得将模块标记为“已完成”。

## 规划草稿（待评审）

- [AI 助手 MVP 实施计划（已评审并实施；实现状态以 [模块文档](modules/ai-assistant.md) 为准）](plans/ai-assistant-mvp.md)
- [Agent Harness 分阶段计划（ADR-005/006 已实施；实现状态以 [模块文档](modules/ai-assistant.md) 为准）](plans/agent-harness-phases.md)
- [会话上下文压缩与记忆工具分阶段计划（ADR-007；G1–G5 已交付）](plans/context-memory-phases.md)
- [AI 显式业务上下文分阶段计划（ADR-008；AI5 已交付）](plans/ai-explicit-context-phases.md)
- [本地知识库分阶段计划（ADR-009/026；K1–K6 与 K3b PDF 已交付）](plans/knowledge-base-phases.md)
- [AI6 显式知识片段实施计划（ADR-010；已交付）](plans/ai-knowledge-context-phases.md)
- [AI7 质量闸门与可解释性计划（ADR-011–025；引用、本地质量/用量、可靠性与确认恢复；验证记录集中维护）](plans/ai-quality-gates.md)
- [Agent Harness 交接（长期进度源：现状盘点、任务状态、验证记录与最终审计）](plans/agent-harness-handoff.md)

## 架构决策

- [ADR-029：智能体与工作台能力桥接（单次授权读取首片，业务命令与编排持续实施）](adr/029-agent-workspace-capabilities.md)

- [ADR-003：本地 Agent Runtime 安全与传输边界](adr/003-local-agent-runtime-security.md)
- [ADR-004：AI 助手远程 Provider 接入与安全边界](adr/004-ai-assistant-provider-access.md)
- [ADR-005：Agent Harness 架构与本地大模型接入](adr/005-agent-harness-and-local-models.md)
- [ADR-006：Harness 完整组件矩阵与自进化边界](adr/006-harness-matrix-memory-evolution.md)
- [ADR-007：会话上下文压缩与记忆工具（G1–G5 已交付）](adr/007-session-context-compaction-and-memory-tools.md)
- [ADR-008：AI 显式业务上下文与发送前预览（AI5 已交付）](adr/008-ai-explicit-business-context.md)
- [ADR-009：本地知识库导入、索引与检索边界（文本基线已交付；PDF 由 ADR-026 接续）](adr/009-local-knowledge-base-ingestion-and-search.md)
- [ADR-010：AI 显式知识片段上下文与来源引用（AI6 已交付）](adr/010-ai-explicit-knowledge-context.md)
- [ADR-011：AI 回答的可验证知识引用（AI7-Q1 已交付）](adr/011-ai-validated-knowledge-citations.md)
- [ADR-012：AI 运行步骤与本地无正文指标（AI7-Q3 steps、Provider usage 与本地聚合已交付）](adr/012-ai-run-steps-and-local-metrics.md)
- [ADR-013：AI 本地模型质量评测 Actor（AI7-Q2 显式本地运行闭环已交付）](adr/013-ai-local-quality-evaluation-runner.md)
- [ADR-014：AI 本地质量分组与时间趋势（AI7-Q2 同口径趋势已交付）](adr/014-ai-local-quality-trends.md)
- [ADR-015：AI 本地质量类别分解（AI7-Q2 category 统计已交付）](adr/015-ai-local-quality-categories.md)
- [ADR-016：AI 本地质量失败原因聚合（AI7-Q2 failure-code 统计已交付）](adr/016-ai-local-quality-failure-codes.md)
- [ADR-017：AI 本地质量评测数据集 v2（12-case 多样本纵切已交付）](adr/017-ai-local-quality-dataset-v2.md)
- [ADR-018：AI 本地质量 Wilson 区间与重复证据（描述性不确定性已交付）](adr/018-ai-local-quality-confidence-intervals.md)
- [ADR-019：AI 本地质量人工评审候选（advisory readiness 已交付）](adr/019-ai-local-quality-advisory-readiness.md)
- [ADR-020：AI 本地质量评测数据集 v3（24-case 事实覆盖已交付）](adr/020-ai-local-quality-dataset-v3.md)
- [ADR-021：AI 本地质量分层评测套件（8-case smoke / 24-case full 已交付）](adr/021-ai-local-quality-tiered-suites.md)
- [ADR-022：AI 本地质量人工决定审计（精确证据快照与不可变理由已交付）](adr/022-ai-local-quality-human-review-audit.md)
- [ADR-023：AI 本地质量代码所有专题套件（四类 6-case 已交付）](adr/023-ai-local-quality-topic-suites.md)
- [ADR-024：AI 本地用量 UTC 时间趋势（1–30 天已交付）](adr/024-ai-local-usage-time-trends.md)
- [ADR-025：AI 运行可靠性、确认事务与评测配置身份（schema 068–069、dataset v4）](adr/025-ai-reliability-confirmations-and-evaluation-identity.md)
- [ADR-026：本地知识库 PDF 文本提取与页码定位（K3b 已交付）](adr/026-local-knowledge-pdf-extraction.md)
- [ADR-027：内置 Agent 执行器与 Run 生命周期（v0.2-B 首片已交付）](adr/027-builtin-agent-executor-and-run-lifecycle.md)
- [ADR-028：智能体人工作业工作区（目录授权、只读审查、手动 PTY 与 AI 隔离）](adr/028-human-operated-agent-workspace.md)
- [ADR-030：跨 Run 条件式延后启动（start gate，AH-07 首片已交付）](adr/030-agent-run-start-gates.md)

## 核心术语

| 术语              | 含义                                                                                              |
| ----------------- | ------------------------------------------------------------------------------------------------- |
| Inbox Item        | 说明为什么需要处理、来源和跟进策略，不承担任务执行状态                                            |
| Inbox–Task 关系   | 保存活动/历史关联、required、顺序和软解除；进度实时从 Task 派生                                   |
| Task              | 唯一可执行工单实体，保存工作内容、状态、完成条件和验收策略                                        |
| Actor             | 本地责任主体：owner、person、agent、system                                                        |
| Assignment        | Task 当前负责人和历史改派记录                                                                     |
| Task Submission   | 一次提交批次；`origin=manual/child_rollup` 区分人工产出和系统子任务汇总，并保存审核状态与操作责任 |
| Agent Run         | 本地 Agent 的一次执行尝试；成功不等于 Task 完成                                                   |
| Task Artifact     | text/file/link/structured 产出，区分实际产出者与 owner 录入者，带完整性和软删除审计               |
| Client Attachment | 客户本地受控文件，可选关联 Activity，带完整性、软删除和聚合删除补偿                               |
| Client Actor Link | Client 与 active person 的显式本地 contact 关系，带不可变解除历史                                 |
| Reminder          | 本地调度事实；到期后幂等生成 Inbox Item                                                           |
| Workflow Event    | 创建、拆分、分派、执行、验收和返工的追加式审计时间线                                              |
| Focus Session     | 服务端持久化的一次工作段；interval 保存实际计入区间，前端 ticker 只派生显示                       |
