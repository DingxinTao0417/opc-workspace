# opc-workspace 整体功能架构

2026-09-23 AH-07 跨 Run 延后启动链（ADR-030）：AI `agent_run.start` 提案附带前置 Run 与条件 → 用户确认完整快照与启动条件 → 同一事务创建 `queued` Run 并追加 `agent_run_start_gated` → FIFO 派发与 Worker 认领均跳过门控未结束的 Run → 前置 Run 终态、交付恢复、人工验收或审批决定提交后唤醒对账（另 30 秒扫描）→ 满足则写 `agent_run_start_gate_released` 并回到原准入/FIFO/冻结身份复核，否则写 `agent_run_start_gate_closed` 并经既有取消命令取消。事实归属：门控只属于被确认的那一个 Run（不可变工作流事件），前置 Run 与 Task Submission 仍是条件判断的唯一事实来源；不新增表、scope 或 Runner wire。

2026-09-23 AH-05 计划关闭链：计划卡“关闭计划”+ 勾选确认 → `POST /api/v1/ai/sessions/:id/plan/close` → Sidecar 在同一事务重新投影最新修订，要求计划 Inbox 为 `awaiting_continuation`、整轮续办检查为 `ready`、无 `waiting/running` 后台续办 → 追加不可变 `workflow_events(ai_work_plan_closed, version)`。事实归属：关闭只属于该计划修订，不写 Proposal、Run、Task 或任何业务表。读模型联动：计划投影带 `closed_at`，计划 Inbox 以 `closed` 状态排除出 attention，续办检查返回 `plan_closed`；统一 Agent Inbox 的计划排除与 `plan_link=unplanned` Run 账本跳过已关闭修订，使其 Run 回到计划外队列。模型后续写出的新修订是新意图。无 schema/迁移、scope 或 Runner wire 变化。

2026-09-23 AH-04 单文件操作的本机结果待核对提醒：可信主 WebView 写入入口在调用 Tauri 原生命令前把“意图”写入前端本机存储，明确回执后删除，未确认或窗口重启残留的项进入续办队列“文件操作待核对”。事实归属：该提醒只是界面观察，恢复记录与当前文件仍是唯一权威；helper 的尝试/执行日志仍只在独立 helper 进程内读取，桌面主进程与 Sidecar 均不新增读取路径或数据。

2026-09-23 AH-02 Agent Run 统一生命周期表达：Web 端所有 Run 展示面经 `agentRunLifecycle()` 把 `status × output_delivery_status`（+ 同一 Submission 的审核状态）映射为闭集阶段；非法组合为 `unknown`，“已提交/已验收”必须有服务端 Submission 身份。仅表达层约定，服务端状态机不变。

2026-09-23 H5-G57 确认回执的结果身份门禁：AI 确认卡把 `confirmed` 与服务端 `result_id` 分开表达；缺少结果身份时只显示“已确认 · 结果待回读”，不显示已执行或成功自然语言，不生成操作成功入口。该 UI 保护不改变服务端审批、权限、事务或领域事实，也不把旧 Sidecar 的不完整响应升级为成功。

2026-09-23 H5-G56 统一工作台操作回执：任一已确认 Proposal 仅在服务端返回规范 `result_id` 后，由前端按闭集动作映射显示自然语言动词和目标名称，并使用服务端 `route` 提供精确打开入口；任务、批量、Agent、导出等已有专用回执保持优先，不重复渲染。没有结果身份、确认未成功或操作失败时不显示成功措辞。该变化只改善表达，不新增模型权限、审批入口、API/schema、领域事务或自动执行。

2026-09-23 H5-G55 单任务审批回执链：保存会话中的 `task.create/update` → 用户逐项确认 → Sidecar 在原子领域事务中写入真实 Task 并返回规范 `result_id/result_version/route` → 前端确认卡显示标题和自然语言“已创建/已更新”回执 → 用户可打开服务端推导的精确 Task 地址。没有 confirmed 结果身份时不显示成功；这只是回执表达补强，不新增模型权限、写工具、数据库字段或自动执行。

2026-09-23 H5-G54 后台计划续办统一投影：`GET /api/v1/ai/inbox` 在既有 Agent Inbox Union 增加 metadata-only `continuation` 来源，只联结持久 `ai_continuations` 与保存会话，筛选 `waiting`/`running`，返回会话标题、状态/原因、轮次和过期时间。Web 左侧新增“后台续办”分组，点击精确回到原会话计划，handoff 只带固定只读提示并要求重新读取计划事实。该来源不跨边界传递 workspace grant、计划正文、Provider 配置、工作区 JSON、结果或观测哈希，不产生审批、重试、停止、修改、执行或权限继承；无新 schema/迁移、路由、scope 或 Runner wire。

2026-09-23 H5-G53 Agent Run 重试链精确回看：Task/Agent 执行详情读取既有 `parent_run_id`，用户可从当前 retry Run 打开同一 Task 的精确父 Run；查询失败或身份不符时不替代为其他历史项。只读导航不创建、重试、恢复、取消或验收 Run，不把 retry 链解释为 Agent 父子关系；无 API/schema/迁移、scope 或 Runner wire 变化。

2026-09-23 H5-G52 Agent Run 统一续办投影：`GET /api/v1/ai/inbox` 在原有计划外审批、权限、验收和系统异常读模型旁新增 metadata-only `agent_run` Union。服务端按同一 `attention` 语义投影 `queued/running/failed/interrupted` 或 `pending/retained` 交付，先排除连续精确 retry 子项及经过完整 restart 证明替代的来源；`submitted` 与 `cancelled + not_ready` 不进入统一事项。Web 左侧续办队列保留独立 Run 查询以显示进度/模型并处理计划外项，统一 Inbox 的计划内 Run 可直达精确执行抽屉，也可通过只携带 metadata 的 handoff 回到 AI；该 handoff 要求重新选择 `agent_execution`，不继承上一条消息的授权。ID、Task 标题、attempt、状态和交付状态之外不跨边界传递正文、快照、文件/路径或 Provider 私密资料；完整事实仍走 `workspace_agent_runs`/`workspace_outputs`，没有新的执行或审批能力。复用既有 `agent_runs`/`tasks`/restart 事实，无 schema/迁移、scope、Runner wire 或自动调度变化。

2026-09-23 H5-G51 普通 AI Generation 显式停止：普通聊天取消先在既有 `workflow_events` 中记录 `ai_generation_stop_requested`，再向当前进程发送本地取消信号；续办停止则可在维护写锁等待期间先撤销进程内 worker，并在 worker 先收尾清除活动标记的竞态中回看本续办最新轮次，对精确的 `queued/streaming/cancelled` Generation 幂等补记停止意图，但不把自然完成/失败标为用户停止；只有停止意图/续办状态提交后才确认 HTTP 成功。Sidecar 启动时将具有持久停止事实的残留活动 Generation 收敛为 `cancelled`，未请求停止或停止未持久确认的残留 Generation 标记 `AI_GENERATION_INTERRUPTED`。两者都不重放远端 Provider 请求，事件不证明 Provider/费用/副作用已停止。复用既有表，无 schema/API/scope 变化。已确认子委派尚未受理阶段仍由 H5-G50 处理。见 [AI 助手 H5-G51](modules/ai-assistant.md#普通生成停止意图的持久化h5-g512026-09-23部分实现)。

2026-09-23 H5-G50 已确认子委派安全启动恢复：spawn 确认事务追加排队事件 → Provider 受理事务原子写入 Generation、用户消息和 accepted 事件 → 父方取消在 Generation/Coordinator 双锁下先写停止意图再发信号 → Sidecar 启动只选择有排队事实、无 accepted/停止事件且无 Generation 的冻结委派 → 通过原 Coordinator 重入既有受理事务。旧无排队事件提案失败关闭；stop intent 登记 cancelled，accepted 标记但 Generation 缺失则记结果不明失败。Follow-up 与已受理/结果未知请求不重放；不新增 schema/API/scope/Runner wire。见 [AI 助手 H5-G50](modules/ai-assistant.md#已确认子委派的安全启动恢复h5-g502026-09-23部分实现)。

2026-09-23 H5-E39 对话中断续办：Sidecar 启动恢复将遗留生成标为 `cancelled + AI_GENERATION_INTERRUPTED` → Agent Inbox 只在其仍是保存会话当前最新 generation 时返回最小元数据 → UI 标示服务重启中断并回到原会话 → 用户人工核对后自行决定是否重新发送。主动取消、临时会话和已被新 generation 替代的旧事项不显示；不自动重放或继承授权，无 API/schema 变化。

2026-09-23 H5-G49 智能体请求双面板初始比例：显式 `workspace_ui` → 固定 `workspace_open_panel(panel, companion_panel, split_ratio?)` → Sidecar 对比例范围及其双面板前置条件做严格校验 → `workspace_panels` 仅在给定时携带比例 → 前端验证后打开指定配对并应用主面板初始比例。无比例时维持当前配对，或为新配对均分；用户可继续拖动。该字段不新增面板访问、内容数据边或持久化状态，旧事件保持兼容；无 API/schema/scope 变化，见 [AI 助手 H5-G49](modules/ai-assistant.md#智能体请求工作区的初始分栏比例h5-g492026-09-23)。

2026-09-23 H5-G48 Agent 工作区双面板比例：只有存在主/次标签的右侧工作区显示可拖动 separator → 本地 `splitRatio` 约束 25%–75% → 指针、键盘或双击更新布局；单标签时隐藏。比例只影响右工作区内部，不读取面板内容、不影响外层聊天比例或标签事实；无 API/schema/scope/持久化变化，见 [AI 助手 H5-G48](modules/ai-assistant.md#智能体工作区双面板比例调整h5-g482026-09-23)。

2026-09-23 H5-G47 AI 单文件修改提议的只读工作区投影：模型已完成的提议保存在当前 WebView 内存 → 用户主动点击左侧提议卡 → 创建含精确 `proposalId` 的内部工作区标签并展开右栏 → 右栏只读投影同一提议的完整差异/状态。关闭、过期或替换会使旧标签显示不可用；没有文件重读、历史恢复、向模型回传、审批/写入第二入口或 Sidecar/API/schema/scope 变化，详见 [AI 助手 H5-G47](modules/ai-assistant.md#ai-文件提议的右侧只读预览h5-g472026-09-23)。

2026-09-23 H5-G46 智能体双面板布局：单消息显式授予 `workspace_ui` → `workspace_open_panel(panel[, companion_panel])` 只接受固定面板枚举 → 单面板保持旧 `workspace_panel` 事件，双面板发出仅含两个面板名的 `workspace_panels` → 主 WebView 顺序激活/打开对应标签并将第二项设为分栏。布局不创建数据访问边，不读取或回传标签内容；不会接收路径/URL/命令、启动终端、操作浏览器/Git/文件或写业务数据。SSE、store、工作区组件共同拒绝未知/重复标签；没有 HTTP/schema/scope/数据库变化，见 [AI 助手 H5-G46](modules/ai-assistant.md#智能体请求双面板工作区布局h5-g462026-09-23)。

2026-09-23 H5-G45 精确项目文件的多文件消息上下文：可信主 WebView 中用户逐项选择同一目录的 1–4 个完整快照（合计最多 32 KiB）→ 主聊天逐份显示相对路径与完整内容 → 按当前 Provider/会话整组显式授权 → 发送前逐份重验 → 单条 project_files 请求 → Sidecar 对路径、摘要、数量和总字节再次校验。改变任一选择都撤销整组授权；失败不发送部分文件。模型至多对其中一份提出修改，审查/原生写入仍精确绑定该文件、一次只写一份；没有目录遍历、自动发送、额外 scope、API/schema/迁移或终端能力，详见 [AI 助手 H5-G45](modules/ai-assistant.md#h5-g45-同消息多文件显式上下文2026-09-23)。

2026-09-23 H5-G44 Task Agent Run 准入/调度闭环：任务执行或 AI Proposal 确认事务 → 检查本 Sidecar 持久化 `queued/running + not_ready` 数量是否低于 40 → 满载时 429 且完整回滚任务分派/提案决定 → 接受后按 `created_at,id` FIFO 派发至最多 8 个 Worker → Worker 退出/启动恢复时补位。模型已完成但产出仍 `pending` 的 Run 不占执行容量，继续使用原产出恢复链。AI 子会话 H5-G41 有独立 8+32 Coordinator，不共享容量。无 schema、scope、Runner wire 或自动重跑变化，详见 [AI 助手 H5-G44](modules/ai-assistant.md#task-agent-run-的共享准入与-fifo-执行队列h5-g442026-09-23) 与 [本地 Agent H5-G44](modules/local-agents.md#task-agent-run-的有界-fifo-调度h5-g442026-09-23)。

2026-09-23 H5-G43 Task 人工审核状态与 Agent Run 计划回执联动：Task Submission 审核事务提交 → 仅在非幂等重放后发送无载荷续办唤醒 → 续办扫描器重读原 proposal、Run 与精确 Submission → 计划/API 投影展示当前审核状态。工作计划 `submitted` 仍表示产出已登记，不等于任务完成；待验收状态允许智能体准备独立人工审核建议，但不代替用户查看、批准或返工，也不引入自动审核/重跑、scope、路由、schema 或 Runner wire。详见 [AI 助手 H5-G43](modules/ai-assistant.md#agent-run-产出与人工验收状态联动h5-g432026-09-23) 与 [本地 Agent H5-G43](modules/local-agents.md#agent-run-的-task-submission-审核状态投影h5-g432026-09-23)。

2026-09-23 H5-G42 Task Agent Run 阶段展示：Runner 已认领 Run → Sidecar 在有界进程内登记准备/受控模型调用/完整结果登记三个内容无关阶段 → Task Run、全局 Agent Run 和会话委派的只读 UI 查询附加 `{phase,elapsed_ms}` → 原有轮询更新任务卡、执行详情与 Agent 工作区。信息在 worker 退出时清除，不入库、不进入 `workspace_agent_runs` 模型工具结果，不包含 token、推理、正文、文件或凭据，也不改变单次非流式完整校验协议、产出登记及人工验收。旧 Sidecar 允许不返回阶段字段；无 schema、scope、Runner wire 或调度变化，见 [本地 Agent H5-G42](modules/local-agents.md#task-agent-run-的瞬时阶段可见性h5-g422026-09-23)。

2026-09-23 H5-G41 子智能体并发准入：逐项/批量人工确认 → Sidecar Coordinator 在审批事务提交前原子预留全局进程内容量（最多 8 个活动 Provider 尝试 + 32 个已接受排队项）→ 满载则事务回滚、proposal 留在 pending → 提交后 worker 竞争执行槽，busy 退避释放槽 → 接受/终态仍写回既有 AI Generation 与审批事实。队列覆盖父会话 spawn/follow-up，不包含普通对话和 Task Agent Run，不跨 Sidecar 实例、不持久化、不在重启后重放；不新增 scope、数据库或 Runner wire。见 [AI 助手 H5-G41](modules/ai-assistant.md#子智能体全局并发准入与有界排队h5-g412026-09-23部分实现)。

2026-09-23 H5-G40 子智能体委派的原子批次确认：父 Generation 结束 → 用户逐项查看并选择 2–4 条待确认 spawn 提案 → 单独勾选同意精确集合 → Sidecar 重验父 Generation、父会话及每条提案完整冻结预览 → 单个事务创建所有子会话、确认提案并记事件（任一失败全回滚）→ 提交后独立启动各子 Generation → 按提案分别读取启动/执行状态。逐项确认仍可用；混合已确认/待确认的批次必须刷新重审，精确整批重放不重复创建。授权始终按子提案独立冻结；不合并权限、不新增 scope、数据库迁移或 Runner wire。见 [AI 助手模块](modules/ai-assistant.md#子智能体委派的显式原子批量确认h5-g402026-09-23部分实现)。

2026-09-23 H5-G39 父会话对子 Generation 原子批量停止：右侧显式选择最多四项 → 二次确认精确清单 → POST 批量停止路由 → 只读事务重验完整父子关系与所有当前 Generation/预受理 Worker → Generation registry + Coordinator 双锁全量预检 → 全部通过后统一发信号，任一失败全批拒绝 → 有序 `202` 回执 → UI 继续读取权威终态。没有自动级联、未选兄弟停止、Task Agent Run 控制、远端请求/费用回滚或授权变化；schema079、数据库与 Runner wire 不变。见 [AI 助手模块](modules/ai-assistant.md) 与 [本地 Agent 边界](modules/local-agents.md)。

2026-09-22 H5-G38 浏览器可见链接显式交接：用户在可信主 WebView 点击 → 原生复核活动标签/来源/导航代次 → 固定脚本提取有界正文和可见 HTTP(S) 链接 → 原生与前端再次校验并移除链接查询参数/片段、拒绝含凭据目标 → 内存交接卡完整预览 → 用户单独带入草稿并手动发送。正文与链接按不可信网页数据处理，不增加 browser/network scope、不自动访问目标、不读取网页私有数据；无数据库、scope 或 Runner wire 改动，见 [AI 助手 H5-G38](modules/ai-assistant.md#内嵌-chromium-可见链接的显式交接h5-g382026-09-22部分实现)。

2026-09-22 H5-G37 浏览器文本显式交接：用户点击主 WebView 浏览器工具栏 → 原生按活动标签/URL/加载状态/导航代次重验 → 固定 WebView2 脚本采集最多 8 KiB 可见正文 → 前端内存卡片显示脱敏地址和完整有界预览 → 用户再次带入聊天草稿并手动发送。网页内容按不可信输入处理，不增加 browser/network scope，不由模型调用捕获/导航/点击，不自动发送；页面敏感内容必须由用户审阅。无业务 API、数据库、scope 或 Runner wire 变化，见 [桌面浏览器边界](modules/desktop-platform.md#内嵌-chromium-多标签浏览器)及 [AI 助手 H5-G37](modules/ai-assistant.md#内嵌浏览器可见文本的显式交接h5-g372026-09-22部分实现)。

2026-09-22 H5-G36 子会话批量等待条件：父 AI 会话在既有授权下调用 `workspace_agent_children(view=wait,wait_mode=any|all)`；省略维持 any，all 等全部 1–4 个已确认子会话终态。条件满足或有界超时后，以新只读快照重验并返回 metadata-only 状态；不读取子回复、不确认、不续办、不级联取消，也不影响 Task Agent Run / Submission / Artifact。

2026-09-22 H5-G35 子会话回复批量回收：当前保存父会话 + 本条授权 → `workspace_agent_children(view=results,child_session_ids=...)` → 同一只读快照验证 1–4 个精确子关系、父确认的 generation/message 与逐子 scope/知识源/Provider/配置 → 每子至多 1000 Unicode 首页且批次正文最多 8 KiB → 以不可信文本返回父模型。任一目标不通过就整批无部分正文返回；无新增存储或授权，内容不自动并入对话，也不成为 Task/Agent Run/业务回执。长回复走现有 generation/SHA-256 绑定的单子分页。

2026-09-22 H5-G32 父方停止请求链：右侧“Agent 执行”用户两步确认 → `POST /api/v1/ai/sessions/:id/agent-children/:child_id/generations/:generation_id/cancel` → 同一只读事务按父/子/Proposal/frozen delegation/当前轮次精确重验 → 对已受理 Generation 发信号，或对 Coordinator 预受理 worker 记取消意图并由 worker 原子登记 `cancelled` 终态 → 返回 `202 cancel_requested` → 右侧继续轮询真实状态。授权字段仅在 UI 家族投影；`workspace_agent_children` 会剥离。它不取消子会话自行启动的其他 Generation、Task Agent Run 或兄弟子会话，也不证明远端请求/计费已停止。API v1 增受校验写路由，schema079 与 Runner wire 不变。见 [AI 助手 H5-G32](modules/ai-assistant.md#父子会话的显式停止请求h5-g322026-09-22部分实现)。

2026-09-22 H5-G31 续交办产品链（部分实现）：保存父会话本条授权 → 专用 `workspace_agent_followup` 仅提议精确现有子会话的新指令 → 已确认 spawn 关系 + 最新子 Generation/版本/Provider/待审批/续办事实 → 冻结完整预览 → 前端单独勾选与人工确认 → 以 Proposal ID 单次受理新子 Generation → 权威状态/待审批计数回执与计划阻塞 → 父方仅在逐次权限核验后读取该 follow-up 回复。普通 `workspace_propose` 不能伪造；子会话不递归，确认和受理都重验身份，重启只登记未受理失败、不重发不确定请求。结果不会自动合并，真实 Provider/桌面多轮验收仍待。见 [AI 助手 H5-G31](modules/ai-assistant.md#已有子会话续交办h5-g312026-09-22部分实现)。

2026-09-22 H5-G30 计划安全门禁：子会话确认回执与会话树共用有效待审批计数；`completed + pending_approvals>0` → 计划步骤 `child_pending_approval`/未满足 → 后台续办 `pending_approval`。人工决定后重查子 Proposal 事实，不凭之前的终态快照继续父轮次；不跨会话代批，待审批清零后 follow-up 仍需另行确认。见 [AI 助手 H5-G30](modules/ai-assistant.md#子任务待审批阻塞可见性h5-g302026-09-22)。

2026-09-22 H5-G30 子会话审批阻塞投影：已确认的父→子 Proposal/Generation → 子会话中已完成 Generation 的有效 `pending` Action Proposal → `pending_approvals` 元数据 → 父工具 list/wait/result 与右侧导航。只统计数量，不返回建议正文、授予审批权或把模型完成等同业务完成；过期/已决定/未完成生成不计。后续续交办须重查该阻塞事实，本轮未开放发送。见 [AI 助手 H5-G30](modules/ai-assistant.md#子任务待审批阻塞可见性h5-g302026-09-22)。

2026-09-22 H5-G29/H5-G31 子结果分页一致性：已确认父子关系与本条授权 → 首轮或父方确认的 follow-up Generation/唯一完成消息精确一致 → 计算完整回复 SHA-256 → 每页返回相同 Generation 身份和摘要；非零偏移续页必须同时携带 `generation_id` 与预期摘要，若事实漂移则失败关闭。摘要不是认证签名或业务回执，不放宽文件、Provider、scope 或知识来源边界；不建立自动合并或通用子会话发送链。见 [AI 助手 H5-G29](modules/ai-assistant.md#子任务回复分页的一致性绑定h5-g292026-09-22)。

2026-09-22 H5-G28 子任务等待链：当前父会话工具调用 → 精确 child_session_id → 每轮独立只读事务核验已确认父子 Proposal 与子 Generation → 终态/超时只回元数据。子终态或启动前失败提交 → 无载荷进程内唤醒 → 既有计划续办器重查数据库、租约和所有阻塞。等待与唤醒不传回复、不授予权限、不生成新模型任务；回复仍通过 G27/G31 的逐次授权结果读取，Task Agent Run 事实独立。见 [AI 助手 H5-G28](modules/ai-assistant.md#子任务终态等待与计划唤醒h5-g282026-09-22)。

2026-09-22 H5-G27 子结果只读回收链：当前保存父 AI Session + 本条授权 → `workspace_agent_children(view=list)` → 已确认 Proposal/子 Generation 状态；精确 `view=result` 读取原始委派或该父会话已确认的 follow-up Generation，并重核对应 scope/知识来源版本及 Provider/配置版本、唯一完成消息 → 有界 Unicode 回复页。不会读取子会话自行运行的其他轮次。审批账本、会话/Generation 和 Task Agent Run 各保留自己的事实归属；子回复仅为不可信模型文本，不能成为业务命令回执或 Task 验收。逐份文件授权不自动跨父子传递；无自动消息合并、通用 send/interrupt、HTTP API、schema 或 Runner wire 变化。见 [AI 助手 H5-G27](modules/ai-assistant.md#父智能体读取子任务进展与首轮回复h5-g272026-09-22)。

2026-09-22 H5-G26 只读会话树：已确认 `agent_delegate.spawn` Proposal → 冻结父/子身份及任务名 → 不可变 child `result_id` → 子 AI Session/Generation 权威状态 → 右侧“Agent 执行”的人工跳转和子会话返回父会话。关系投影不携带指令、范围、Provider/文件正文，不启动模型或改变任何业务事实；缺失子会话只形成不可点击历史行。Task Agent Run 委派账本继续独立从 `agent_run.start/retry` 投影，`parent_run_id` 仅表示精确重试。API v1 新增只读路由，schema079/Runner wire 不变；见 [AI 助手 H5-G26](modules/ai-assistant.md#父子会话的只读导航与状态h5-g262026-09-22)。

2026-09-22 H5-G25 受确认 AI 子会话链：保存主 AI Session + 本条显式 grant → `workspace_delegate_agent` 的有界任务/子集 → 冻结 `agent_delegate.spawn` Proposal/人工确认 → 独立保存子 AI Session → 进程内协调器以 Proposal ID 启动一次子 Generation。Proposal 属于审批账本，子会话/Generation 属于 AI；只通过既有工具和独立单消息权限接触工作台，不能因父对话、右侧面板或 Agent Run 获得隐式范围。子 Generation 的权威状态反馈回执/计划，确认本身、排队和失败都不是子任务完成。

首次生成落库前普通聊天不能抢占子会话；进程重启面对已确认但无 Generation 的不确定窗口，只登记失败，不重发模型。委派限制一层/四子会话；无通用消息邮箱、send/follow-up/wait/interrupt、自动取消树或系统级后台唤醒。H5-G32 提供父会话用户显式确认的单个活动子轮停止；它与 Task Agent Run 不建立父子关系，`parent_run_id` 仍仅表示 Run 重试。API v1/schema079/Runner wire 不变。见 [AI 助手 H5-G25](modules/ai-assistant.md#受确认的有界子智能体委派h5-g252026-09-22) 与 [本地 Agent 边界](modules/local-agents.md#ai-子会话委派与-agent-run-的边界h5-g252026-09-22)。

2026-09-22 H5-G24 并行 Run 原子中断链：AI 计划/对话提出 `agent_run.cancel_many`（1–8 个不同 Task 的精确 Run 身份与 Task version）→ 一张人工确认卡逐项展示冻结执行元数据 → 确认事务重投影整批 → 所有取消请求原子提交 → 提交后才逐个通知进程内 Worker → 计划持续读取每个 Run 的权威状态，全部 `cancelled` 后才满足步骤。任一对象漂移或写入失败都会拒绝/回滚整批，不产生部分工作流事件或提交前 Worker 信号。

取消决定属于 Proposal，Run 生命周期仍属于 Agent Run，Task/Submission/Artifact 事实不受批量回执替代。回执只携带有序 Run/Task/状态与固定 Agent 工作区地址；不删除产出、不取消 Task，也不保证远程 Provider 请求或计费可撤回。这是有界并行 Run 控制，不是父子 Agent 委派：没有 spawn/send/follow-up/wait、独立子线程或取消树，`parent_run_id` 仍只表示精确重试。沿用既有 Agent 执行 scope，API v1/schema079/Runner wire 不变。见 [AI 助手 H5-G24](modules/ai-assistant.md#并行-agent-run-的原子批量中断h5-g242026-09-22) 与 [本地 Agent H5-G24](modules/local-agents.md#多-run-原子停止请求h5-g242026-09-22)。

2026-09-22 H5-G23 会话委派只读链：当前 AI Session → 该会话 Generation → 已人工确认的 `agent_run.start/retry` Proposal → 不可变 `result_id` → 真实 Agent Run/Task metadata；同一 Proposal ID 再与最新 `ai_work_plan_revisions` 步骤做可选关联。右侧“Agent 执行”默认使用该投影区分当前对话委派与全局执行账本，并可返回原确认回执。Run 随 Task 删除时保留历史 result ID 而不伪造实时 Run；重复绑定、身份错配或损坏计划失败关闭。

事实归属没有改变：会话/Generation 属于 AI，决定属于 Proposal，计划只保存意图与引用，执行生命周期属于 Agent Run，Task/Submission 仍是业务与验收事实源。投影不携带 action/preview、输入快照、结果正文、文件或 Provider 机密，不产生权限和写入。`parent_run_id` 只属于重试链；Codex 式独立子线程和 spawn/send/wait/interrupt 尚未实现。API v1 增加只读路由，schema079/Runner wire 不变。见 [AI 助手 H5-G23](modules/ai-assistant.md#当前会话委派与-agent-执行关联h5-g232026-09-22) 与 [本地 Agent H5-G23](modules/local-agents.md#当前会话委派账本h5-g232026-09-22)。

2026-09-22 H5-G22 事件驱动续办：审批决定事务和 Agent Run 关键生命周期事务成功提交后 → 非阻塞、可合并的进程内唤醒 → 续办协调器重新投影计划全部 Proposal/Run/Submission 事实 → 仍被阻塞则保持等待，全部门禁满足才复用既有 H5-G1 租约生成下一轮。通知不携带领域身份或正文，不持久化为新的业务事实，也不替代数据库读取；周期扫描继续负责通知合并、进程调度竞态和安全兜底。

提交边界是硬约束：事务回滚、文件补偿失败或仅重放既有决定不得产生新的推进授权。Run 完成通知不会越过仍活动的兄弟 Run，取消请求也只有权威终态/计划投影允许时才解除阻塞。该链只提升已授权续办的响应性，不新增后台系统服务、跨进程事件总线、自动批准/重试/恢复/验收或子代理通信。API v1/schema079/Runner wire 不变。见 [AI 助手 H5-G22](modules/ai-assistant.md#权威事实提交后的事件驱动续办h5-g222026-09-22)。

2026-09-22 H5-F9 并行 Agent 计划阻塞投影：计划有效步骤 → 关联生成 → 每个生成的全部兄弟建议 → 权威执行回执。服务端先扫描完全部来源，再按固定优先级选择兼容 `reason`，同时返回所有阻塞类别的 `total` 与去重 generation ID；一个待确认建议不会再遮住同时存在的 `output_pending`、活动 Run 或证据异常。主对话只用这些元数据展示完整摘要并回到既有审批组，不读取或缓存 Run 正文；后台续办复用同一全量检查，只有所有相关阻塞都消失才可能进入下一模型轮次。

事实归属不变：Proposal 决定属于 AI 审批账本，Run 生命周期与交付属于 Agent Run，计划只保存意图和已核验引用。阻塞摘要不是新的执行事实或授权，不能替代 Run/Submission 查询。数据库仍按 Task 仲裁唯一活动 Run；跨 Task 并行只是计划协调，不是 parent/child 委派、共享执行上下文或取消传播。API v1/schema079 不变。见 [本地 Agent H5-F9](modules/local-agents.md#并行执行计划的全量等待状态h5-f92026-09-22)。

2026-09-22 H5-F8 人工验收往返：持久计划步骤 → 读取时投影真实 action receipt → 严格解析服务端 route → 精确 Run 标注“查看 Agent 执行” / 精确 Submission 标注“查看待验收提交” → 工作台详情附加当前保存会话返回身份。右侧“子智能体”精确 Run 详情对 `submitted` 结果复用同一 Submission 地址，对 `retained` 只解释原因、对 `pending` 只恢复登记。任何阶段都不根据模型文字猜 route，不自动接受/退回、完成 Task、重跑 Run 或继续计划；不增加 scope、API、迁移、Runner wire 或内容外发，见 [本地 Agent H5-F8](modules/local-agents.md#计划执行与人工验收的精确往返h5-f82026-09-22)。

2026-09-22 H5-F7 Agent 产出到项目文件人工桥接：成功且 `submitted` 的 file/files Run → Task 执行列表、执行过程抽屉或右侧“子智能体”精确详情中的人工选择 → 前端核对 Run/Task/Submission/Artifact 与冻结 output contract → 用户逐份选择有界 UTF-8 候选 → 右侧“文件”中人工选择现有目标 → 精确重读来源 Run → 原生精确快照 → 完整差异与页面勾选 → 再次精确重读来源 Run → 既有 Win32 审查/按次 UAC/helper 无覆盖替换与恢复记录。右侧详情入口只切换同一工作区标签，不引入第二套写入路径。来源读取失败或身份/契约/正文漂移在原生命令前失败关闭；候选和目标绑定仅在 WebView 内存，目标路径不进入模型、Sidecar 或 Runner。纯文本、未提交/待恢复/保留结果、不合规载荷和超过 32 KiB 的单份产出拒绝；一次只处理一个文件。文件操作不改变 Submission/Artifact/Task 验收事实，不新增 scope、业务 API、迁移、Runner wire 或自动调度，见 [本地 Agent H5-F7](modules/local-agents.md#已提交文件产出到项目文件的人工桥接h5-f72026-09-22)。

2026-09-22 产品入口补充（当前事实）：可信主 WebView → 动态核验同目录、编译摘要绑定的 helper → 用户完整差异确认 → 单次消费精确文件快照或 v2 恢复票据 → 全局唯一、root/operation-bound 本机租约 → 独立 Win32 审查 → 固定 `runas` 请求本次 UAC → 已签名安装配对/父进程世代/同用户/一次性管道 → helper 独立复核和 started-only 执行 → 同 operation ID 规范回执。普通开发构建因无编译绑定保持只读；CLI、模型、Sidecar 与浏览器子 WebView 无直接入口。明确取消消耗旧审查，其他异常保持 unknown 且禁止重试；取消不能回滚 started 后移动。旧 v1 原地恢复仍永久关闭。真实安装构建的 UAC/SACL、发布签名、断电/强杀及人工恢复尚待专项验收，见 [产品入口与边界](modules/desktop-platform.md#绑定发行构建的按次单文件操作入口2026-09-22部分实现)。以下同日段落按底层能力落地先后保留；其中“产品未接/写入关闭”均是当时阶段状态，以本段为当前事实。

2026-09-22 H5-G33 子会话并行状态等待：父方已确认的 1–4 个精确子会话可由单次 wait-any 工具调用等待；每次读取在新的只读事务中重验证父子事实，任一到达终态返回所有目标的当前 metadata-only 快照，期限最多 15 秒。没有回复正文、权限继承、自动续办、审批或兄弟取消；单目标旧协议不变。见 [AI 助手 H5-G33](modules/ai-assistant.md#多子会话的有界-wait-anyh5-g332026-09-22部分实现)。

2026-09-22 H5-G34 子会话 wait 唤醒：复用提交后无载荷通知并独立 fan-out 到活动 wait 调用，保留续办器原扫描消费者；等待先订阅，通知只促使 fresh read-only transaction 重验当前授权父子关系与数据库状态，1 秒重读为兜底。不将事件本身视为状态/授权，不增 API/schema/scope/Runner wire；详见 [等待唤醒边界](modules/ai-assistant.md#子会话等待的提交后唤醒扇出h5-g342026-09-22部分实现)。

2026-09-22 执行阶段补充：原生确认 → attempt 占用 → 同一固定对象的正文/普通元数据/完整权限观察 → `prepared` 冻结完整请求及恢复所需描述 → `materialized` 绑定已核验候选 → 固定恢复目录 CREATE_NEW/刷盘/回读 v2 intent → `recovery_intent` 同时绑定恢复记录摘要、记录文件身份及源/候选身份 → 新的同步 AuditScope 重验 live 普通元数据、可读权限与完整 SACL → 重验原期限并最后写 `started` → 保持 root/parent 固定、把只读观察句柄换成重新核验的精确 DELETE 句柄 → 只用 `ReplaceIfExists=false` 的对象重命名完成 Replace / RestoreMissing / UndoInstalled → 从仍持有的目标与保留对象句柄完整回读身份、正文、普通元数据、可读权限和完整 SACL → 写一次 success `outcome` → 通过原请求绑定管道返回同一 operation ID 的规范终态回执。恢复绑定后的新候选不再由析构删除；首个移动后的任何错误、竞争占位或回执写入失败都不自动回滚，started 无有效 outcome 只能是 unknown 且禁止重试。helper 仅对已验签安装配对、父进程世代、同用户和一次性管道全部通过的六字段 bootstrap 开放该链；路径、请求 JSON 和解锁参数仍无入口。阶段文件不送模型、未加密且 checksum 非认证，重启恢复仍须核对 live 对象；真实 UAC/SACL、断电及人工恢复专项未完成，见 [阶段边界](modules/desktop-platform.md#辅助端完整准备与结果未知阶段内部实现未执行文件操作)。

2026-09-22 尝试占用补充：同用户固定目录 → 原生审查/恢复记录复核 → 独占尝试存储、检查操作/审查双身份 → 新建最小元数据记录、刷盘回读 → 再验配对/连接/期限 → 完整权限观察。内部观察持有占用，失败不删除或重试；可被用户改写的目录不是认证防重放边界、完整恢复意图或成功回执。两处写门禁仍关闭，见 [尝试占用契约](modules/desktop-platform.md#辅助端持久尝试占用内部实现非执行回执)。

2026-09-22 恢复记录复核补充：辅助端配对/父进程世代 → 同用户内核 SID 与无模拟线程 → 系统固定 app-local-data 路径 → 原生确认后的 v2 记录只读固定/严格解码 → 与冻结请求逐字段比对 → 重验连接/期限后完整权限观察 → 记录和请求再验。新替换不读取来源记录，失败不创建/改写/重试；v2 checksum 不是历史认证，部分 ACL 不能回放。共享旧格式、无迁移，生产执行仍关闭，见 [固定恢复存储与验证范围](modules/desktop-platform.md#辅助端固定目录的恢复记录复核内部实现)。

2026-09-22 完整权限观察补充：辅助端私有原生确认交接 → 重验配对/请求 → 短时独立审计令牌 → 原路径与身份/正文/元数据核对 → 同对象完整安全描述捕获/回读 → 先关闭文件句柄、再恢复线程身份 → 重验配对/请求。该观察自身只返回进程内事实，不返回文件句柄、不等同 journal 或执行许可；SACL 访问权不等于 OS 只读权限，实际捕获没有 setter。后续持久阶段、完整保留和 started-only 执行见本文顶部；真实跨权限特权与产品验收仍待，见 [观察实现与验证范围](modules/desktop-platform.md#原生确认后的完整权限观察内部接线真实特权未验收)。此前进程/期限回归已分别改为直接观察应用目录锁和原始单调期限，生产流程未改，见 [测试校正证据](modules/desktop-platform.md#进程生命周期与请求期限的回归校正仅测试)。

2026-09-22 辅助端独立审查补充：绑定接收的原请求 → 独立完整 Win32 审查 → 同请求摘要/原期限/管道复核 → 仅内部的已展示交接，仍不授权写入。安装配对贯穿窗口，短回调只观察同一父进程、连接和取消；取消仅有固定绑定回执，断连后的旧“继续”不能生效。普通通信仍最多 30 秒，人工审查改用原请求剩余绝对期限，不因墙钟回退或交接续签十分钟。CLI/写入门禁仍关闭，真实人工批准、完整 SACL、可信持久记录和执行待验收，见 [内部审查与期限](modules/desktop-platform.md#辅助端独立原生审查与原期限等待内部实现)。

2026-09-22 辅助接收补充：未可信启动元数据 → 已固定签名配对 → 父进程句柄/创建时间与映像文件核对 → 内核管道对端 → 一次性严格请求接收 → 仍未批准的请求/独立只读磁盘复核，已在内部串联。配对对象与进程/连接贯穿接收生命周期，失败消费；父进程活着但通道已关闭也会使请求失效。持久阶段与 started-only 执行后来已补齐；产品装载代码来源、真实人工批准和跨权限运行仍待，CLI 不接操作参数，见 [内部接收边界](modules/desktop-platform.md#辅助端绑定请求与断连失效内部实现)。

2026-09-22 构建配对补充：构建内临时私钥 → helper 内置公钥 → helper 摘要进入桌面 → 最终程序摘要的域分离签名清单 → helper 以原生 CNG 验签并固定三份安装文件，消除互相嵌入摘要的循环。公钥只能由 helper 编译绑定，不信任清单/argv/env 替代；私钥不导出或持久化。新增普通权限只读自检，不是父子操作会话认证、发布者身份或批准，见 [边界](modules/desktop-platform.md#构建本地签名配对与只读自检部分实现)。

2026-09-22 原生启动编排补充：父端固定映像 → 完整原生审查的不可反序列化交接 → 本次参数/管道绑定 → 内部固定 runas 适配 → 直接保留返回句柄并验证映像 → 一次请求/未验证回执，已在内部串联。每个边界及等待中检查原范围/期限，失效锁存取消，不重签期限或自动重试；取消不证明回滚。完整安全、持久阶段与 started-only 执行后来已在辅助端内部补齐；产品入口现由本页首段接入，helper CLI 仍拒绝操作参数，实际 UAC 与跨权限端到端验收仍待，见 [当前边界](modules/desktop-platform.md#原生审查后的单次启动编排绑定产品入口已启用)。

2026-09-22 返回进程绑定补充：父端内部已将固定映像 → 启动返回句柄的最小权限同对象副本 → 内核映像路径及目录/文件身份核对 → 同一进程的管道对端校验 → 持续固定映像的一次性请求/回执串联。真实测试子进程验证了该链，但可信调用层仍需保证先固定再启动，不得用既存进程冒充启动结果；没有生产启动或提权。路径/摘要不是内存度量、父端认证或批准，见 [适配边界](modules/desktop-platform.md#返回进程与映像固定到通信的绑定内部实现)。

2026-09-22 辅助构建边界：独立 helper Cargo 包（无桌面/模型依赖）→ 明确本机目标构建 → 安全暂存并计算确定产物摘要 → 仅传入桌面编译与本次 Tauri 打包输入 → 父端内部固定自身安装目录/固定文件名/只读句柄及身份、核对编译摘要。没有 runtime 环境、PATH、网页参数或模型输入的信任回退。此链只绑定构建内容，未认证发布者/父端、未启动进程或授予执行权限，见 [构建与后续可信启动前提](modules/desktop-platform.md#独立辅助包与构建产物绑定部分实现)。

2026-09-22 单份审查绑定补充：完整 CheckedRequest → 原生不可变审查文档（原文/候选、明确操作方向、目录选择、原期限和整份请求摘要）→ 独立 Win32 只读文本窗口、完整控件回读/原期限/当前范围检查 → 按实例消费的继续/取消意图 → 仍未批准执行的原请求。成功交接不重新解码或延长期限，不单独授予执行能力；当前绑定产品入口会继续进入 UAC/helper 链。见 [原生窗口边界](modules/desktop-platform.md#独立-win32-完整审查窗口已接绑定产品入口)。

2026-09-22 单文件内部准备链：普通端消费文件基线/恢复审查 → 受保护重读并冻结完整单操作请求、沿用剩余期限 → 同一共享契约编码 → OS 绑定通信 → 辅助端结构/期限验证 → 独立只读固定路径并核对目录/对象身份、完整正文/时间、普通元数据和可读权限。辅助接收层在磁盘复核前后重验通信存活/取消/期限，仍不产生批准；缺失位置不是预留，当前 journal 与完整 SACL 仍待执行端独立核验。实际生产尚无启动/批准/写入调用，该链不接 Sidecar/Provider，不扩大工作台或模型授权，见 [磁盘复核及未证明边界](modules/desktop-platform.md#辅助端独立磁盘复核内部只读基础)。

2026-09-22 恢复条件链：版本 2 本地观察 → 人工另点复核 → Rust 重验目录/记录/双方主文与可读元数据 → 有界内存审查票据 → UI 比较正在显示的完整差异 → 关闭/过期释放。票据不进入 Sidecar/Provider 或持久记录，当前没有生产消费执行命令；ACL 的可读部分不等于 SACL 已验证。单文件辅助 binary 与权限原语已独立于主桌面 library，但仅常量状态入口可运行，尚无批准通信/操作编排或主进程调用；不能以此扩大模型权限，见 [票据与提权边界](modules/desktop-platform.md#恢复条件审查票据与按次提权边界)。

2026-09-22 替换意图只读链：同一右侧恢复列表按 version 分流 → 用户点击 v2 → 独立原生命令核对所选根/父目录及三个位置的记录对象身份 → 本机完整原文/候选差异与当前主文观察。v2 独立位于 `appLocalDataDir/workspace-file-recovery-v2`，不迁移 v1、不产生恢复快照或写授权；陌生占位对象不读取正文。观察不证明当前权限/附带流完整或写入成功，不经 Sidecar、模型或数据库。替换写引擎仍仅测试编译，见 [只读契约](modules/desktop-platform.md#版本-2-替换记录只读集成)。

2026-09-22 文件建议审查链：本条合法文件上下文 → 独立 `workspace_propose_file_edit` → 精确路径/摘要与候选限制 → Harness 成功结果接受钩子 → 当前 SSE 的单份候选 → 前端以 request/generation/会话/Provider 与本机基线绑定 → 生成正常完成后展示完整差异 → 人工只读复核。建议不落库、不进入后续授权或轮询恢复，没有可用的原生写命令；原生基线在本地审查期间有界保留，到期/切换/丢弃释放。模型回复引用仍遵循聊天历史规则。业务 `workspace_propose` 审批与该临时文件建议独立，见 [仅审查契约](modules/ai-assistant.md#项目文件修改建议与完整审查2026-09-22源码实现仅审查)。

2026-09-22 本地恢复原型：可信主 WebView 的右侧文件面板 → 所选 root 的记录元数据 → 用户点击 → 原生重验文件身份 → 完整文本/十六进制差异，只在本机展示。实验 journal 位于 `appLocalDataDir/workspace-file-recovery-v1`，含未加密的原文/候选及路径，不归 Sidecar 数据库/备份，不自动送给 AI。独占句柄仍可晚建硬链接，故原地写入原型不能保证目录范围；前端及两个原生 apply 命令都关闭，无文件操作可通过确认绕过。恢复快照冻结所审查原文、关闭/过期释放；数据库 schema079 不变。安全替换及中断恢复仍待，见 [桌面边界](modules/desktop-platform.md#项目文件恢复原型与关闭的写入门禁2026-09-22)。

2026-09-22 精确文件外发链：文件预览人工点击 → Rust 精确快照 → 主聊天展示完整正文/相对路径/Provider 并单独确认 → 发送前原生复核与当前会话/模型/草稿检查 → 单次 `project_files` 请求 → Sidecar 受理前校验 → Harness 独立 `ProjectFiles` 上下文，双协议按原总预算发送。本条结束不从数据库或历史恢复原附件；保存/临时会话都不保存原始附件，回复可能引用的内容仍按原聊天历史规则处理。无隐式目录访问、写入或终端能力，Headless 续办拒绝；详见 [单消息文件分析](modules/ai-assistant.md#精确项目文件的单消息分析2026-09-22已接源码)。

2026-09-22 项目文件桥接基础层：可信主 WebView → 已选 root ID → Rust 固定路径句柄并完整读取 → 有界内存精确快照；再次复核比较根目录/文件身份、修改时间和全部字节，冲突使旧快照失效。基础层本身不外发、不写磁盘；外发必须经过上面的显式授权链。它不是跨调用写锁或授权，逐次确认写入仍须独立实现，终端边界不变。见 [桌面基线](modules/desktop-platform.md#项目文件精确基线2026-09-22内部基础能力)。

2026-09-21：AI Inbox 拆分结果由共享领域命令返回，经确认事务写入不可修改工作流事件的 `created_task_ids`；审批输出及同会话模型回执读取有界身份投影，不把当前 Inbox 关系当历史创建事实。无新表或权限，旧事件缺少映射时不回填。见 AI 模块“收件箱拆分的精确历史回执”。

前端路由加载（2026-09-21）：App 常驻外壳及全局生成监视器，`/ai`、`/knowledge` 通过 `DeferredPage` 按需导入。组件以加载函数和重试序号隔离结果，卸载/切路由后忽略迟到结果；加载失败可人工重试，不触发模型或业务写入。页面内部 API、store 与权限归属不变。

当前源码与运行服务均为 schema079：权限请求账本重建扩大清单容量，保留既有业务事实、审批来源和不可变请求身份；本轮启动已通过原备份门禁完成迁移并生成回滚备份。旧版本回退必须使用升级前完整备份，不能用旧二进制直接读取 schema079。

权限补选链更新（2026-09-21）：模型建议完整必要范围 → Sidecar 对冻结 policy 重算缺失项并要求 1–3 项 → 全清单由原 SSE/请求账本交付 UI → 人工核对下一条授权。当前 Registry 不变，旧 grant 不继承；完整清单最多为现有 16 项，字段不变、schema079 重建请求表，旧客户端三项限制要求前后端配套更新。

H5-G21 调用链补正：实际详情页面 → `aiIssueHandoff.ts` 专用 helper → `pendingIssue` → 人工带入/选权/发送 → 对应领域指南 → 当前目录下工具。通用 `AiWorkbenchHandoffButton` 当前无业务页面调用；实际 Task 仍推荐 `work+outputs+actions`，需要产出时先发现联合工具，预算拒绝则分开加载。以下通用组件描述不替代实际专用入口及其原有安全边界。

H5-G21 领域化工作台交接链：详情页人工点击 → 临时 UI store 只保存规范对象身份、原生 route、最小推荐 scope 与领域化草稿 → `/ai` 的待办卡 → 用户重新确认单消息授权并手动发送 → Harness 先按精确身份重读，再按对象加载指南，必要写入仍形成独立人工确认。交接不复制页面正文、文件、来源 payload、客户资料、旧 grant/Provider/session，也不自动发送、读产出、运行 Agent 或改业务；Task 产出/验收与 Agent scope、Content 外部发布、Inbox 强制解决、Reminder 状态及里程碑的跨领域限制均由草稿和服务端工具门禁共同约束。API v1/schema078 不变，见 [AI 模块](modules/ai-assistant.md#工作台交接按领域给出最小授权指引h5-g212026-09-21)。

H5-G20 高负载工具目录链：本条授权构造完整且服务端持有的 Registry → `workspace_guide(tasks_projects,outputs|agents)` 组合当前注册的工具定义与代码所有指南结果 → 按当前 Provider 协议测量含系统提示、8 KiB 历史回执和额外 4 KiB 留位的请求 → 仅可装入时由 Harness 在工具成功接受后替换模型可见目录；越界保持原目录并提示缩小后续授权或分开读取。实际 Registry/单消息权限、业务事实、人工审批和运行内 64 KiB 最终门禁不变；见 [AI 模块](modules/ai-assistant.md#高负载跨领域指南按授权预算放行h5-g202026-09-21)。

H5-G19 生成提示导航链：Sidecar 当前活动生成元数据 → Web 全局回读 → 仅当 `persist=true` 且 Session ID 规范时构造 `/ai?session=<id>` → AI 页选择该会话；临时/无效身份不提供精确链接。其他窗口生成逐条使用自身身份，当前生成在 AI 页看着别的会话时也能切回。URL 不带正文、授权或 Provider 凭据；生成状态、停止语义、Session 事实归属和 API/schema 不变。见 [AI 模块](modules/ai-assistant.md#跨页面生成提示精确返回h5-g192026-09-21)。

H5-G18 工具发现链：本条冻结授权 Registry → `workspace_guide` 校验主领域与可选的不同且已授权的关联领域 → 返回两个领域完整规则和 Registry 中已有工具名称的有序并集 → 仅在 Harness 成功接受、服务端重算结果一致后替换本 generation 的模型可见定义。任务/项目与产出或 Agent 联合现由 H5-G20 按当前授权预算预检。完整 Registry、单消息 scope、Sidecar 工具执行校验、业务事实及人工确认链均不随目录投影变更；下次指南替换而非永久累积。见 [AI 模块](modules/ai-assistant.md#跨领域联合指南与工具目录h5-g182026-09-21)。

H5-G17 单消息授权展示链：原权限弹窗人工确认的 grant → 主/侧聊天依据当前 Provider、版本和会话导出有效授权 → 发送区只读展示完整 scope 清单与数量 → 原发送受理链清除 grant，展示随之消失。草稿建议不进入清单，旧授权不跨 Provider/会话展示；业务工具授权及逐项人工审批仍由原 Sidecar 门禁负责。无新事实归属、scope、API/schema，见 [AI 模块](modules/ai-assistant.md#发送前显示本条工作台授权h5-g172026-09-21)。

H5-G16 授权 UI 链：用户打开主/侧聊天共用权限弹窗 → 选择代码固定的常用范围组合或逐项修改现有 scope → 核对所选 Provider 的本地/远程外发说明与全部范围 → 人工点击“仅授权下一条消息” → 原单消息 grant 随发送受理，之后清除。快捷选择只改本地未确认草稿，敏感文件/财务/知识正文/Agent 执行不自动加入；持久会话限定、领域人工审批和工具门禁仍由原链负责。无新业务事实、权限或 API/schema 变化，见 [AI 模块](modules/ai-assistant.md#工作台权限的常用选择与风险分组h5-g162026-09-21)。

H5-E38 已核验重启续办投影：`agent_run_restarted` 事件与新旧 Run 的身份、冻结来源、执行哈希、创建时间在同一只读事务经完整验证 → 旧失败/中断/保留来源从 `attention=1` 的筛选总数和分页排除 → 新 Run 仍按自身状态判断；若新 Run 又精确重试，H5-E37 继续排除中间失败尝试。损坏证据失败关闭，不以未核验事件 JSON 隐藏来源。完整执行账本与 Task 历史、全局运行计数、任务验收及人工权限门禁均不变；API v1/schema078 不变。见 [AI 模块](modules/ai-assistant.md#独立执行续办的已核验重新执行去重h5-e382026-09-21)。

H5-E37 独立 Run 续办投影：原生 Run 账本和 Task 历史继续保存所有尝试；`attention=1` 只在失败/中断父 Run 存在同 Task、连续 attempt 的直接 `parent_run_id` 子 Run 时，从需处理计数、分页和角标排除父项。后继 Run 自身仍按当前状态判断；计划内排除规则独立叠加。按当前事实重新执行不从未经核验的事件内容推断重试链。Run 执行与交付事实、Task 验收事实及人工门禁均不变，API v1/schema078 不变。见 [AI 模块](modules/ai-assistant.md#独立执行续办的精确重试去重h5-e372026-09-21)。

H5-E36 保存视图反向定位链：本条 `workspace_ui+work` → 模型请求 `task_saved_view` 规范 ID → Sidecar 校验真实预设存在，只向当前 UI 发 type/id → 用户确认 → 前端固定 `/tasks?task_view=<id>` → Task 页重读当前定义并应用筛选。加载/缺失/失败不展示未筛选或旧任务结果；视图定义与任务结果不随导航事件回传模型，Task 事实仍归领域表，读取不等于写入或授权。见 [AI 模块](modules/ai-assistant.md#受确认的任务保存视图定位h5-e362026-09-21)。

H5-E35 浏览器动作反馈链：单消息 `workspace_browser` 仅向用户请求闭集动作 → 本机用户确认 → 可信桌面端重验活动标签并调用原生 `browser_action` → 无错误返回才显示短期本地“命令已接受”回执 → 用户可另行暂存无网页内容的固定结果、在聊天卡带入并手动发送。失败保留请求，切换标签或新请求使旧回执失效。网页事实仍由隔离 Chromium 持有，Sidecar 工具与模型不自动得知动作结果，更不读取页面。无 API/schema 或领域事实变化，见 [H5-E35](modules/ai-assistant.md#浏览器动作的本地执行回执与显式交接h5-e352026-09-21)。

H5-E34 右侧产出续办链：已核验的 Artifact 预览 → 人工点击“继续交给智能体” → 只在本地内存暂存三个 ID 与固定复查问题 → 原对话交接卡再次人工带入、清除旧授权/上下文并推荐新消息 `work+outputs` → 用户选权并手动发送 → 模型通过既有工具重读当前事实。右侧正文、名称、文件与旧授权不随预览转移；文件正文需另有 `output_files`，业务写入还需独立建议和人工审批。无 API/schema 或事实归属变化，见 [H5-E34](modules/ai-assistant.md#右侧产出的原位智能体续办h5-e342026-09-21)。

H5-E33 右侧产出预览链：H5-E32 服务端授权的 Artifact/Task/Submission 身份 → 当前对话确认卡 → 用户选“右侧预览”后在本地多标签工作区打开精确 Artifact 标签，保持对话 → `GET /api/v1/artifacts/:id` 新鲜读取 → UI 再校验三重身份和未删除状态后才呈现可内联正文；另一选择仍到原精确批次页。右栏关闭/标签上限/身份错配不暗中导航或展示旧内容；链接不自动访问，文件不自动下载。Task/Submission/Artifact 事实仍由原生业务表持有，右侧标签只属临时 UI，不授予模型额外读取或写入。API v1/schema078 不变，见 [H5-E33](modules/ai-assistant.md#任务产出的对话内右侧预览h5-e332026-09-21)。

H5-E32 产出精确定位链：单消息 `workspace_ui+work+outputs` → 模型仅提出规范 `task_artifact` ID → Sidecar 联结未删除 Artifact、Submission、Task，并只向当前 UI SSE 给出推导的 Task/Submission ID → 本地确认卡 → 人工点击后生成固定提交批次 `artifact` 深链 → 页面重新读取并只读展开匹配产出。工具回执不含归属或正文；失效/跨批次目标不被替换；文件下载仍是独立人工动作，模型文件读取另受 `output_files` 门禁。API v1/schema078 不变，见 [H5-E32](modules/ai-assistant.md#任务产出的受确认精确定位h5-e322026-09-21)。

H5-E31 浏览器标签动作链：单消息 `workspace_browser` → 闭集 `workspace_request_browser_action(back|forward|reload|stop)` → Sidecar 仅在当前 generation SSE 发动作请求，与 H5-E14 新网址请求共用一个名额 → Web 严格解析并展示本机当前标签地址 → 人工点击 → 桌面端重新读取原生快照、核对活动标签及动作资格 → 调用现有 `browser_action`。Sidecar/模型不接收标签身份、网页、地址或结果；无用户确认、网页模式或标签漂移不执行。浏览器事实仍由隔离的原生 WebView2 持有，不新增通用页面自动化。API v1/schema078 不变，见 [H5-E31](modules/ai-assistant.md#当前浏览器标签的逐次确认操作h5-e312026-09-21)。

H2-W13 前端返回链：任务页手选的规范 Task ID、筛选、页码、视图 → 点击交接后仅内存暂存 → 返回 `/tasks` 通过既有 `GET /api/v1/tasks/:id` 全部重读并核对 ID → 全部成功才恢复勾选及最新版本，失败则清空并要求重新选择 → 后续批量命令仍受当前列表稳定性、版本预检和人工确认约束。此本地状态不持久化、不进入 Provider，也不替代 H2-W12 的模型读取；Task 仍为唯一事实。API v1/schema078 不变，见 [H2-W13](modules/ai-assistant.md#已选任务返回断点h2-w132026-09-21)。

H5-E30 精确记录定位链：用户给本条消息 `workspace_ui` 与对应 `work`（项目笔记）、`clients`（客户活动/回访）或 `finance`（账本/发票）→ 代码所有闭集 schema 仅广告已授权类型 → Sidecar 校验规范 ID、当前记录与未删除约束，仅在服务端推导笔记所属 Project 或活动/回访所属 Client → 工具回执不含归属、route 或正文，当前 SSE 只给从属记录附加 `parent_id` → 前端严格解析、生成固定详情地址 → 用户点击确认后才离开对话并保留返回会话。记录仍由各业务表持有；导航不读详情给模型，不写业务或执行 Agent。API v1/schema078 不变，见 [H5-E30](modules/ai-assistant.md#智能体精确记录导航扩展h5-e302026-09-21)。

H2-W12 精确任务读取链：用户手选的规范 Task ID 集合 → 本条 `work` 授权下的 `workspace_tasks(task_ids)` 互斥于筛选/分页 → 单一只读事务查询全部 1–20 个 Task，缺失即整组失败 → 按输入顺序只返任务有界元数据、版本与实时日期/优先级 → 需要责任另用 `workspace_task_assignments`，需要正文/项目另用按 ID 的详情查询。读取结果不是持久选择或写入许可；Task 仍是事实唯一归属，后续写入重验并由人工确认。API v1/schema078 不变，见 [H2-W12](modules/ai-assistant.md#已选任务精确成组读取h2-w122026-09-21)。

H2-W11 已选任务交接链：任务页人工勾选最多 20 项 → 点击后暂存规范 Task ID 集合和固定问题、切换至智能体模式 → 用户核对单消息 `work+actions` 并手动发送 → H2-W12 的 `workspace_tasks(task_ids)` 在同一只读快照重新确认全部 Task、版本和批量事实，需要责任时另用 `workspace_task_assignments(task_ids)`，详情用 `workspace_get` → 明确的同一变更才由 `task.batch_update` 形成一张逐项确认卡 → 原有审批事务重验并写入。任务页选择不是 Task 的持久事实、读授权或写许可；交接不复制页面行与批量表单，H2-W13 的返回恢复必须重读全部 Task，不启动 Agent。Task/Assignment 继续各自拥有事实，API v1/schema078 不变，见 [H2-W11](modules/ai-assistant.md#已选任务交给智能体h2-w112026-09-21)。

H2-W10 批量任务事实链：任务页用户选择或智能体在本条 `work` 下精确读取 Task ID/版本 → 原生 `PATCH /api/v1/tasks/batch` 或 `work+actions` 的 `task.batch_update` 指定 `set_priority/set_due_date`，时间由带偏移输入规范为 UTC，`null` 明确清除 → 原生事务/智能体人工确认事务预检全部版本 → 只对有变化的 Task 升版本，任一错误整批回滚。Task 是优先级和截止时间唯一事实源；Today/列表及到期 Inbox 仍依其既有读模型/投影，不新增第二套调度。智能体回执是批次身份，不是单项版本或自动执行授权。API v1/schema078 不变，见 [H2-W10](modules/ai-assistant.md#批量优先级与截止时间h2-w102026-09-21)。

H2-W9 成组责任链：单消息 `work` → `workspace_task_assignments(task_ids)` 对 1–20 个 Task 精确查询当前责任和版本，同一快照且不返回历史/私有资料 → `workspace_task_options(role)` 查询真实候选 → 保存会话 `work+actions` 中 `task.batch_update` 冻结逐任务责任及 Actor 身份 → 一张人工确认卡 → 同一事务重验并复用原生 Assignment 创建/改派/结束命令，任一失败整批回滚。Task 拥有版本/生命周期，Assignment 拥有责任历史；Agent Run 不因分派启动，person 不收到消息。API v1/schema078 不变，见 [H2-W9](modules/ai-assistant.md#已有任务成组责任变更h2-w92026-09-21)。

H2-W8 项目拆解初始责任链：本条 `work` 下 `workspace_task_options(type=actor,role=assignee)` 读取真实候选 → 保存会话 `work+actions` 的 `task.batch_create` 逐草案可选绑定 Actor → 同一确认卡冻结完整 Task 与 Actor 名称/类型/状态/版本，已分派 `manual` 草案还显示 owner reviewer → 确认事务重验 Project、Tag、Actor/Adapter 与预览 → 原生 Task 创建和 Assignment 命令依次执行，全部同一事务回滚边界。Task 拥有生命周期与版本、Assignment 拥有责任，Agent Run 仍须另行授权和确认；批次回执的 Proposal version 1 不是逐项 Task 当前版本。API v1/schema078 不变。

H2-W7 接续回执链：批量创建确认事务原子落 Task 与 Proposal 结果 → 同会话下一次发送从持久 Proposal 重建历史操作回执，仅当结果已确认且身份校验通过时加入有序 `created_task_ids` → 模型如要继续处理单项 Task，必须在新消息的当前授权下按 ID 读取实时版本/状态，再提出新的待人工批准动作。回执无草案/正文/标签，不是当前 Task 事实、可继承 grant 或自动调度信号；API v1/schema078 不变。

H2-W7 项目任务拆解链：本条 `work+actions` → `workspace_get(project)` 取得真实 ID/version，必要时 `workspace_task_options(tag)` 取得标签 → `workspace_propose(task.batch_create)` 保存同一项目 1–20 项有序草案及完整预览 → 人工确认 → 同事务重验项目、标签与冻结预览 → 复用 Task 创建事务逐项写入、协调父链；任何一步失败整个审批/业务事务回滚。Project 只提供归属事实，Task 仍拥有状态、层级、标签与生命周期；回执以 Proposal ID 表示批次并列出每个 Task 精确路由，不生成 Inbox 关系或 Agent 执行。API v1/schema078 不变，见 [Task 模块](modules/tasks.md#ai-同项目批量建任务h2-w72026-09-21)。

H5-G15 文件权限建议链：模型请求缺失的 `output_files`/`agent_project_files` → 原会话人工准备新消息 → 授权面板显示待手选建议但不预勾选文件权限 → 用户核对并亲自选择后才形成单消息 grant。临时会话不能选择跨任务文件；关闭/手动重开不继承建议提示。前置范围、文件二次确认、Provider 外发边界及服务端 grant 校验保持原样。见 [权限建议核对](modules/ai-assistant.md#文件权限建议在授权面板中显式待选h5-g152026-09-21)。

H5-G14 工具结果链：Executor 收到完整结果 → 超出单工具 64 KiB 则不交给 AcceptResult 或模型，记录无正文 `TOOL_RESULT_TOO_LARGE` 失败 → 原纠错预算允许模型缩小查询或重新核对当前状态。原按字节截取并返回成功的路径已移除；超限前工具本身可能已有副作用，错误不承诺回滚或可盲目重试。累计结果、提示预算、权限与人工审批仍独立执行。见 [超限结果契约](modules/ai-assistant.md#单次工具结果超限不再伪装成功h5-g142026-09-21)。

H5-G13 工具目录链：冻结的单消息 grant 建立完整 Registry → 初始模型请求只显示核心读、计划与指南，不预加载大体积 `workspace_propose` 定义 → 模型先读相关领域 `workspace_guide` → Harness 验证并接受代码所有结果后才把获权提议和该领域辅助读定义放入下一请求。选择 UI 专题不加载业务写定义；切换专题或新消息重建模型可见目录，不修改 Registry、scope、审批或业务事实。最大授权组合的双协议固定首轮测量分别减少 11,671/11,642 字节；原 64 KiB/8 轮硬边界不变。见 [H5-G13](modules/ai-assistant.md#待确认操作工具按领域加载h5-g132026-09-21)。

H5-G12 续办跨普通重启链：计划卡另行勾选重启恢复并确认完整持续授权 → 既有不可变 `workspace_json` 冻结该布尔 → 正常关闭或下一次启动仅保留 `waiting` 且无未结清 generation 的授权 → coordinator 重新核验剩余期限/轮数、Provider/知识范围、计划/建议/审批，再决定下一轮。生成受理事务先持久置 `running` 并扣次，之后才发模型请求；运行中及身份不确定者始终 interrupted，物理备份恢复撤销全部旧授权。旧授权默认中断，用户停止优先，业务建议仍需逐项人工审批。API v1/schema078 不变，见 [H5-G12](modules/ai-assistant.md#等待中自动续办的显式跨重启恢复h5-g122026-09-21)。

H2-W6 项目操作事实链：单消息 `work` 下的 `workspace_get(project)` 读取 Project 当前版本、状态及与原生页面共用的 `availableProjectActions`，并从同次 Task 聚合派生 `incomplete_task_count` → 智能体只对当前允许的状态动作起草建议 → 独立 `actions` 授权和人工确认卡重新核验 Project 聚合版本、未完成数量及完整预览 → 原项目领域事务写入。读取快照不成为权限或状态第二事实；客户/金额/发票/附件仍隔离，项目完成不改 Task。API v1/schema078 不变，见 [项目 AI 操作](modules/ai-assistant.md#项目操作h2-b--h2-p)。

H5-G11 模型请求窗口链：Harness 按 OpenAI/Anthropic 实际序列化字节核算 → 优先调整本条消息之前的完整旧回合 → 仍超限才收缩本轮更早且允许重查的只读工具结果正文 → 对已审查的工作台读工具生成至多 4 KiB、绑定参数/结果摘要的身份证据胶囊，否则使用纯省略标记 → 保留调用配对、最近工具组和提议/计划/权限/文件/引用回执 → 重新测量，无法收缩则硬失败。胶囊只含白名单身份/版本/状态/分页字段并固定不完整、可能过时，不是完整事实源；模型需要完整或当前事实时必须按本条权限重新查询。操作回执仍由原 SQLite 账本与确认事务拥有，实时计数不持久化。API v1/schema079 不变，见 [H5-G11](modules/ai-assistant.md#运行内较早只读结果收缩h5-g112026-09-21)。

H2-W5 项目组合往返链：项目页受校验 URL 保存关键词/状态/客户/页码 → 人工点击“梳理项目组合”只暂存筛选身份与返回路由 → 主/侧聊天另行确认单消息范围并发送 → `workspace_guide(topic=tasks_projects)` 发现 `workspace_projects` → 与原生项目列表共用状态/归档/搜索/排序谓词 → 同一只读事务返回有界项目身份及由 Task 实时派生的进度 → 返回同一 URL 可恢复筛选。Client 筛选另要求本条 `clients`，查询结果仍不带客户、描述、合同/发票或附件；Project、Task、Client、Finance 各自保有事实，页面数据不随交接复制，列表不写第二份进度，也不授予 `actions`。API v1/schema078 不变，见 [H2-W5](modules/ai-assistant.md#项目组合结构化查询h2-w52026-09-21)。

H2-W4 路线图读取链：本条 `work` 授权 → `workspace_guide(topic=roadmap_content)` 发现工具 → `workspace_roadmap_milestones` 与原生 `GET /api/v1/roadmap/milestones` 复用筛选、排序和派生进度事实 → 有界元数据页与精确路线图入口；说明/项目及 Task 明细留给按 ID 的独立读取。Route/Task/Project 仍是各自事实源，列表不写第二份进度，也不授予 `actions`；修改仍经现有版本化提议与人工决定。API v1/schema078 不变，见 [H2-W4](modules/ai-assistant.md#路线图结构化查询h2-w42026-09-21)。

H5-G10 人工权限交接链：主/侧原会话仍开放的请求卡 + 同会话活动有界续办 → 按钮明确说明停止效果 → 既有续办停止 API 返回同会话终态 → 本地续办查询更新 → 原输入区准备固定续问和推荐范围、打开单消息授权面板。失败、来源切换或新活动续办不准备；请求状态仍由 schema078 账本持有，只有明确忽略或新消息受理才关闭。准备不等于 grant，服务端的活动续办发送互锁和独立人工审批不变。API v1/schema078 不变，见 [H5-G10](modules/ai-assistant.md#权限请求与自动续办的一步式人工交接h5-g102026-09-21)。

H5-G9 后台缺失权限链：独立有界授权的后台 generation → 常驻 `workspace_request_access` 只记录缺失范围与未授予回执，不发 SSE 或执行缺失工具 → 成功的保存回复与建议同事务落 `ai_workspace_access_requests` → Agent Inbox/原会话供人工核对 → H5-G8 阻止同一续办自动消费。用户须停止活动续办才可另发带新单消息 grant 的请求；失败/取消不留建议。待核对以请求账本的 `open` 状态为事实，新消息受理关闭旧请求，不依赖相同时刻 generation 的随机 UUID 或隐式 rowid；请求、授权、业务事实各自仍有独立来源。API v1/schema078 不变，见 [H5-G9](modules/ai-assistant.md#后台缺失权限的人工作业交接h5-g92026-09-21)。

H5-G8 权限与续办互锁：`ai_workspace_access_requests` 中已完成保存生成且仍开放的记录 → 创建有界续办、coordinator 扫描和后台 generation 受理事务的共同门禁 → `waiting/pending_approval`，不消耗请求、不调用模型或扣轮次。人工明确忽略后由原授权账本决定能否继续；若需另发带新 scope 的用户消息，须先停止活动续办，再走单消息授权。请求状态仍属权限账本，续办状态仍属授权账本，不把后台消息冒充人工核对。API v1/schema078 不变，见 [H5-G8](modules/ai-assistant.md#后台续办等待权限核对h5-g82026-09-21)。

H5-G7 活动忽略链：Harness 发出本轮权限请求 → Sidecar 先把请求放入当前生成快照再发 SSE → 人工点击忽略 → 仅对真实活动请求在串行闸门下清快照并记内存关闭标记 → 成功完成事务将请求 open 插入、同事务改 dismissed；失败/取消/进程退出不落请求。已完成请求仍由原账本更新，未知 ID 不接受。请求状态与授权/业务执行分离，API v1/schema078 不变，见 [活动忽略](modules/ai-assistant.md#生成中忽略权限请求h5-g72026-09-21)。

H5-G6 权限核对链：原会话开放请求 → 用户点击准备 → 本地主/侧草稿与单消息权限面板；取消/暂缓不写请求状态，仍能从原卡和 Agent Inbox 返回。只有“忽略”写 dismissed，或服务端接受下一条用户消息同事务写 consumed；权限授予仍在新消息独立确认与服务端重验，业务操作仍逐项人工审批。API v1/schema078 不变，见 [核对契约](modules/ai-assistant.md#权限核对中保留续办请求h5-g62026-09-21)。

H5-G5 权限请求续办链：保存会话的成功生成与开放 `ai_workspace_access_requests` → 同一只读事务按请求 `open` 状态筛选，新消息受理关闭旧请求 → Agent Inbox 仅返回来源身份/标题/时间及计数 → 用户点击回原会话读取 H5-G4 权限卡 → 独立核对并决定是否另发新消息。请求账本拥有建议状态，会话消息拥有展示内容，单消息 grant 仍由新一轮授权持有；队列不携带 scope、不授权、不发送、不执行。API v1/schema078 不变，见 [续办契约](modules/ai-assistant.md#权限请求进入续办队列h5-g52026-09-21)。

H2-W3 任务顺序链：`workspace_tasks` 的真实源/锚点及版本 → `task.move` 仅提交两个 Task 身份和前/后意图 → Sidecar 读取同一完整日期组、冻结有界预览与整组指纹 → 人工确认时重算比对 → 与原生 `PUT /api/v1/tasks/reorder` 共用完整集合/版本原子命令 → Task `manual_order`/版本更新并刷新工作台聚合。AI 不保存第二套排序事实，不批量发送组内所有任务给模型；终态槽位不动，跨日期移动、无变化或组漂移拒绝。API v1/schema077 无迁移，见 [AI 顺序确认](modules/ai-assistant.md#任务顺序的人工确认h2-w32026-09-21)。

H4-AR 今日双向链：Today 的日期/时区/风险 UI 状态 → 用户点击暂存安全提示 → AI 交接卡再次带入并重新选单消息权限 → 手动发送 → H2-W2 统计与真实 Task 查询 → 独立人工审批（若有写入）；返回 `/today?date=...&risk=...` 重建同一视图。暂存状态不含业务正文、统计快照或自动授予权限，Today/Task/Focus 仍是事实源；没有新后端服务或迁移，见 [交接契约](modules/ai-assistant.md#今日视图到智能体的显式交接h4-ar2026-09-21)。

H2-W2 今日聚合只读链：单消息 `work` → `workspace_guide(planning|tasks_projects)` → `workspace_today` 严格 IANA 当地日期 → 与原生 `/stats/today` 共用 Task 风险及 completed Focus 统计 → 只向模型返回聚合和口径。Task/Focus 仍拥有各自事实；具体任务需继续授权读取 `workspace_tasks`，任何更改仍走独立建议/人工确认。没有第二份 Today 状态、迁移或新权限，见 [AI 今日概览](modules/ai-assistant.md#今日工作台概览读取h2-w22026-09-21)。

H5-G2 在续办账本上增加最近终态的有界只读投影：活动列表与最近24小时终态列表各自查询，前端在活动消失时重取终态并返回保存会话。终态仍由 coordinator/数据库拥有，前端只展示，不从列表推导执行成功或重新授权；查询失败不能当作无活动。无迁移/新执行路径，见 [续办结果](modules/ai-assistant.md#后台续办结果可见性h5-g22026-09-21)。

H5-G1 增加独立、显式的有界续办授权：用户选定当前保存会话/计划、Provider 和范围 → schema077 授权账本 → coordinator 同事务检查事实并受理/扣次 → 共用 transport-independent generation runner → 新计划/待确认建议 → 人工领域命令及真实 Agent Run/交付回执 → 再次核验续办。最多8个授权、2个后台生成；任何轮次仍不能自批业务命令。授权账本拥有期限与轮数，generation拥有聊天终态，Run/Task继续分别拥有执行与验收事实；它们不能互相代替。关闭/恢复先取消后台worker，等待退出后才关闭存储；恢复维护锁内不等待finalizer。默认重启中断旧授权而不补发未知请求；H5-G12 仅为另行选择的安全等待态提供普通重启后重新核验，物理恢复和运行中请求仍中断。详见 [有界后台续办](modules/ai-assistant.md#有界后台续办h5-g12026-09-21)。

H5-F6 增加同项目成果依赖：来源 Task `done` + 当前 Submission `accepted` + 受管文件 → 原生分页选择/AI 本 generation opaque 候选 → 独立跨任务和文件同意 → v6 冻结来源 proof/hash → prepare/prelaunch 重验 → 后继 Run → 原提交/验收链。源 Task/Submission/Artifact 仍拥有验收和文件事实，Run 只保存授权引用，不复制为附件或改写旧批次。活动引用保护源文件、源 Task/Project 删除；计划依赖仅是进度关系，不传递权限或触发执行。API v1/schema 076 无迁移，见 [H5-F6](modules/local-agents.md#同项目已验收文件交接h5-f62026-09-21)。

H5-F5 在原执行链上增加远程 Anthropic：当前 Provider 资格与完整审批 → v5 冻结协议/预算、Task、文件与可选返工 → 启动前重验 → 子进程单次 Messages 请求 → 完整终态及输出契约验收 → 原 Run/Submission/Artifact 事务与计划投影。密钥只经密钥存储到管道内存，协议与预算进入不可变输入而非由 URL 猜测；旧 v1–v4 明确保留 OpenAI。Run 拥有执行事实、Task 拥有人工验收，恢复不重新请求模型；不增加读取面板或任意宿主操作能力。API v1/schema 076 不变，见 [v5 契约](modules/local-agents.md#anthropic-受控执行h5-f52026-09-21)。

H5-F4 接续链：同 Task 合法终态来源 → 当前 Task/责任/Provider 与本次资料的完整预览 → 独立执行/来源/适用文件与返工确认 → 新 Run 和不可变 `agent_run_restarted` 事件同事务创建 → 实际输出登记 → 计划有效步骤投影。事件保存来源元数据和新执行身份 hash，不复制旧输入或结果；Run 仍拥有执行/交付事实，Task 拥有验收。`parent_run_id` 留给原样 retry，新的 `restart_of_run_id` 由证明派生，不是可写计划事实；跨拒绝/过期替代仍锁定来源。无迁移或自动调度，见 [关联新执行契约](modules/local-agents.md#按当前事实重新执行h5-f42026-09-21)。

H5-F3 执行链：冻结输入 → 单次模型请求 → 非流式完成原因/文本/输出契约校验 → 单一结果或安全失败帧 → Runner 身份、EOF、退出验收 → 既有终态事务。完整安全失败帧不再被通用进程失败覆盖，但异常退出不能借帧内容冒充成功；三类新增稳定码沿既有诊断来源投影，中文说明由前端固定映射。拒绝的部分结果不进入提交或恢复状态，不扩展 AI 回执、权限或模型调用，见 [完成与错误边界](modules/local-agents.md#执行完成信号与安全失败原因h5-f32026-09-21)。

H5-F2 返工链：Task 当前退回批次 → 显式选择非文件旧产出 → 服务端完整只读预览 → 独立人工返工/适用文件确认 → v4 冻结输入的新 Run → 原生输出登记 → 新批次人工验收。Submission 仍持有退回原因，Artifact 持有旧产出；Run 只冻结明确选中的执行资料，不覆盖来源或充当新完成事实。确认及启动前重验，活动引用删除互锁，pending 只恢复登记；无自动审批、跨 Run 调度或新 OS 权限，详见 [返工执行契约](modules/local-agents.md#退回意见驱动的新执行h5-f22026-09-21)。

H5-F1 文件复查链：既有产出交接→用户手选单消息 `output_files+work+outputs` 并明确 Provider 离机范围→Task/Submission 元数据→绑定 Task version/Artifact SHA-256 的受管对象只读校验→Unicode 分页证据→可选原 `task.review` 人工确认。当前文件以 Artifact 受管对象为事实，不以 Agent Run 的文本副本或旧 integrity_status 替代；读取不写库、不改变提交/验收状态，也不增加 Runner、Project Attachment 或 OS 文件权限。主/侧对话复用同一授权/进度链，见 [Task 文件复查](modules/tasks.md#ai-受控文件产出复查h5-f12026-09-21)。

H3-C3 失败诊断链：Agent 真实失败终态+五字段失败 Workflow Event+已启用规则快照同事务提交 → 既有持久投递 → 诊断 Inbox+成功 Automation Run+审计原子提交 → work+outputs 下核验原 Run/Task 与反向通知结果 → 原生精确处理入口。通知成功/事项解决不改变执行或验收事实，失败重试只写通知；源在投递前被删则记录不可重试 SOURCE_UNAVAILABLE。活动诊断加入 Task 删除互锁、终态来源留删除审计；业务包排除 Run、保留历史闭合证明。无新迁移或自动模型执行，见 [失败诊断契约](modules/automation.md#agent-失败诊断闭环h3-c32026-09-21)。

H4-AT 人工导航链：Inbox 不可变快照身份自洽校验 → 固定业务路由/精确历史批次 → 目标 API 重新读取 → UI 提供的合法原对话返回身份。该链不替代 H2-AA 的授权来源核验，不新增事实、权限或自动动作。Inbox 表单、内容详情与提醒管理器的显式离开保护由各页面编辑/在途状态控制，非全局路由拦截；系统维护仍为原位置的设置浮层。见 [来源详情往返](modules/inbox.md#来源详情与原对话往返h4-at2026-09-21)。

H2-AB 回访链：原生 UTC 排期 → 共享纳秒键选择候选/事务重读排期并与固定扫描时刻比较 → 原版本化 Inbox → 显式 work+clients 来源核验与客户查询 → 原有完成/续排/重排建议 → 人工确认的单一领域事务 → 旧事项终结及真实新旧计划回读。来源、回访和客户仍各有独立事实；UI 只保留合法返回对话上下文，存在草稿或在途命令时禁止回访行内交接离开，不产生授权或业务写入。见 [回访闭环](modules/client-followups.md#到期提醒与智能体处理闭环h2-ab2026-09-21)。

H2-AA 收件箱来源链：真实 Inbox ID → 本条 work 与适用的 outputs/clients/finance → 同一只读事务核验白名单来源证明/实际外键/反向结果 → 当前来源身份和历史版本比较 → 对应领域指南/详情 → 原有独立审批命令。Inbox、来源对象和 related 各持有自己的事实与版本；未知/损坏来源不可操作，查询不返回 payload/正文或自动导航。内容调度另共享纳秒时间键，事务内重读排期并与扫描开始的固定时刻比较；版本化投影与旧来源终结规则不变，不增加发布能力。见 [来源契约](modules/inbox.md#ai-来源定位h2-aa2026-09-21)。

H2-Z 内容准备链：页面实际视图条件交接 → 本条 work 授权 → 共享原生排期谓词/纳秒时间键与准备进度聚合 → 分页读取真实 Task ID/version → 既有建议/人工确认命令 → 重读实时准备与排期事实。内容仍拥有排期/关系/发布确认，Task 拥有执行与验收状态，进度不复制持久化；单次查询同一只读事务，内容版本不覆盖任务状态变化。新工具不读取备注/外链/任务正文，不自动完成、改期或发布，见 [内容结构化查询](modules/content-calendar.md#ai-结构化排期与准备任务查询h2-z2026-09-21)。

H2-Y 项目交付链：Project 身份交接 → 本条 work+outputs 授权 → 按 Task 当前项目归属读取 Artifact 元数据 → 共享来源校验及 Inbox required 进度 → 重读当前 Inbox/Task 版本 → 既有人工确认的拆分/关联/完成/验收 → 原生 reconciliation → 重新查询项目跟进。Task Artifact、Inbox 和 Task 仍分别持有产出、事项和执行事实，Project version 不吸收 Inbox 版本；新查询不写状态、创建来源或读附件正文。详见 [项目产出与跟进](modules/projects.md#ai-项目产出与跟进闭环h2-y2026-09-21)。

H2-X 验收策略链：同查询读取 Task 策略与无提交历史的可切换标记 → 新授权下 task.update 提议 → 原生策略门禁与子任务/责任条件预览 → 人工确认同事务重验 → 共享 Task 更新及可选 child_rollup 待验收协调。策略事实仍属 Task，不归 AI 或 Agent Run；读取只新增 metadata，预览不执行，确认不增加 Assignment/执行权限。后续 Agent 必须重读版本并重新按各自授权/审批执行，提交仍须人工验收。见 [Task 策略切换](modules/tasks.md#ai-既有任务验收策略切换h2-x2026-09-21)。

H5-E29 重试接续链：已确认但失败且无产出的 Agent Run → 新授权下的同 Task 精确 retry Proposal → 新计划步骤显式 `replaces` → 独立人工执行确认 → 原生子 Run/交付事实 → 有效步骤进度。投影重验两端命令、直接父链和冻结执行契约，保留原失败并跨拒绝/过期中间步骤传递精确 Run 约束；不把计划、确认或模型回复当作成功。只新增 metadata `retry_of_run_id`，不输出输入快照、凭据或产出内容；事实归属、整轮续办门禁与人工审批不变。无迁移/自动执行，见 [精确重试接续](modules/ai-assistant.md#失败执行的精确重试接续h5-e292026-09-21)。

H5-E28 工具目录链：本条消息冻结 grant → 完整授权 Registry → 常驻模型定义 → `workspace_guide` 返回代码所有领域规则/下一轮目录 → Harness 接受成功结果后替换辅助定义 → 初始预检、普通请求与自检采用同一可见集合。H5-G13 又把当时常驻的大体积提议定义延迟到业务领域指南成功接受后。完整 Registry 继续承担执行查找与知识证据归属，目录不是权限；所有工具仍经过原身份/范围/参数检查及适用的人工审批。选择仅存当前 generation 内存，末轮无工具收尾优先；不删除本轮证据、不新建事实或增加预算。模型 schema 的精简仅处理注解，保留任务/项目业务 `description`。见 [按领域加载工具定义](modules/ai-assistant.md#按领域加载工具定义h5-e282026-09-21)。

H5-E27 计划接续链：用户所见计划版本 → 只读事务重读最新意图、同会话活动生成、最新修订与有效绑定来源的完整 Proposal/Run 事实 → 固定原因及可选阻塞 generation 身份 → 打开原审批卡处理，或来源仍有效时准备新草稿/单消息权限。无关历史不引入门禁，缺失证据不放行，版本变化不自动接续；检查只表示当时状态，不替代发送后的重新读取、领域并发控制或人工决定。未新增领域事实、权限或持久状态，详见 [整轮接续核验](modules/ai-assistant.md#计划接续的整轮核验h5-e272026-09-21)。

H5-E26 上下文链：初始历史窗口 → Run 固定本条用户边界 → 每次模型/自检前按实际协议测量 → 必要时单调移出最旧完整回合并加入代码所有说明 → 保留本轮工具配对、证据及独立授权上下文 → 原 Harness 执行与审批流程。无数据库删除、摘要生成或权限变更，当前必需内容仍超限则失败关闭。调整数仅作为 SSE/活动进度 metadata，成功正文的固定披露由原保存事务落库并随后 `replace`，不把窗口重整当作业务事实或成功证据。见 [运行内窗口调整](modules/ai-assistant.md#运行内历史窗口调整h5-e262026-09-21)。

H4-AS 冲突接续链：确认版本/预览冲突 → 用户同意披露原参数并按原 fingerprint 撤回 → 重读持久拒绝及整轮门禁 → 来源输入器保留草稿、清旧授权并附运行时身份 → 新 grant 与手动发送 → Sidecar 同快照读取精确回执及不可变 ActionJSON、受理事务复验 → 本条 BusinessContext 中作为不可信历史意图 → 读取最新事实、创建新 Proposal、再次人工确认。原参数不复制进草稿、ContextSnapshot 或事件，不自动继承；完整 PreviewJSON 不外发，原领域表仍是当前事实来源。与提议共用领域/文件权限，不产生第二套审批或自动调度，API v1/schema 076 不变。见 [审批冲突重新核验](modules/ai-assistant.md#审批冲突后的原意图重新核验h4-as2026-09-21)。

H5-E24/E25 补齐有限运行的交接链：工具循环 → 最后一次模型请求移除工具且执行器拒绝额外调用 → 自然语言交接及固定预算提示 → 保存成功后完整 `replace`/可回读回复 → 原 Proposal 人工确认。正常收尾不撤销 pending，但所有硬错误仍失败关闭；下一轮需要用户新消息及新 grant。真实 Agent Run 终态 → 取消并失效任务关联审批组查询 → 全组重新投影 → 原显式接续入口，不用单个 Run 的成功代替整组门禁。没有新事实表、权限或调度器，API v1/schema 076 不变。详见 [预算与接续](modules/ai-assistant.md#轮数预算收尾与执行后接续h5-e24e252026-09-21)。

H5-E23 计划替代链：当前消息 `work+actions` → 读取最新修订与真实拒绝/过期回执 → 新 action 的 `replaces` 引用上一版前序已绑定步骤，并显式改写有效依赖 → 共享计划事务校验/追加修订 → 读取时复验旧回执并派生 `superseded_by` → 主/侧卡、Inbox 与右侧概况只统计有效步骤。历史意图、审批决定和领域执行仍是不同事实，替代不会确认、执行或删除旧审批；证据失效拒绝投影，不产生虚假完成。API v1/schema 076 不变，无后台调度或新增业务写入口。

H4-AR 确认后接续链：原会话操作组重新核对整轮事实 → 来源主/侧输入器追加固定提示并清旧 grant/context → 用户重新选择权限和发送 → `action_receipt_generation_id` 在 Sidecar 核验保存会话/生成归属 → 重建该轮有界 metadata 回执 → Harness 仅凭新消息 grant 读取最新工作台记录并提出下一步确认建议。回执无变更正文，不保存第二套业务事实；发送幂等身份包含来源，来源错配或失效在消息创建和模型调用前拒绝。计划接续复用同一草稿保护与新授权流程，仍不获得后台调度权。

H2-W 任务发现链：单消息 `work` → `workspace_tasks` 严格参数 → 复用 Task 筛选/排序 → 同一只读事务读取总数和有界摘要页 → 模型选择真实 Task 身份继续读取或提出待确认命令。保存视图只提供可复用定义，任务与客户关系仍来自现有领域表，列表不复制正文或生成新的任务事实。H5-E22 续办导航链：Inbox metadata → 前端生成精确 session/generation/proposal 位置 → 原会话核验生成归属 → 原 `AiWorkspaceActions` 卡与决定 API；计划位置则展开最新会话计划。旧审批可独立于聊天分页读取，导航不发送消息或继承 grant；右侧队列失败不把陈旧缓存作为当前事实。

H4-I/H4-R–X/H4-AF–AQ 反向交接链：工作台详情的人工点击 → 临时 UI store 只保存记录身份、原生 route、推荐范围与固定提示词 → `/ai` 待办卡 → 用户准备时在主输入区或右侧 `WorkspaceChat` 追加草稿并清空对应输入区旧授权/所选上下文 → 新的单消息范围确认 → 人工发送 → 既有 Harness 读取最新领域事实和创建待确认 Proposal。临时会话不推荐操作权限，当前编辑/在途命令受保护；导航本身不查询模型、不写业务、不创建权限。入口没有新表或 API，且不是记录级授权，完整范围仍由权限面板披露；专用入口额外声明各模块不复制正文/附件/财务/人员备注/metadata 等边界。本地 Agent 设置、AI 供应商设置、数据备份设置与运行诊断交接都是无 scope 的排查提示，分别打开 `/ai?settings=agent`、`/ai?settings=ai`、`/ai?settings=data` 与 `/ai?settings=diagnostics`，不复制 Adapter 配置、执行器引用、Provider Base URL/API 密钥/评测证据、备份包内容、本机路径、导入/导出文件、容量细节、日志或诊断包，也不授权检查/启停/连接测试/Run 启动/重放生成/备份/恢复/删除/导入导出/重启/日志访问。右侧文件预览、Chromium、终端、Git 差异和工作区 root 不随交接进入 Harness；H4-AM/H4-AN/H4-AO 仅分别允许用户在已显示的 Git、文本预览或已有终端输出中点击，二次带入、发送有界快照。H4-AQ 只允许从当前活动 HTTP(S) Chromium 标签人工带入移除 query/hash/credentials 的地址；不含 title/body/Cookie/login/history/download/tab/root/path，且不能访问或控制网页。H4-AO 不含 PTY/root/路径或输入，也不授予 Shell。见 [AI 助手 H4-I](modules/ai-assistant.md#h4-i-从工作台事项进入智能体)。

H4-H 基础记录往返链：主/侧消息及审批/计划卡 → 严格业务链接 + UI 附加来源 Session → Project/Client/Inbox/Reminder/Content Item/Milestone 既有详情读取 → 弹窗内或关闭后的“返回原对话” → 明确选择原会话。加载失败仍可返回，写请求未完成时禁用返回，里程碑关联 Project 保留同一来源；导航不写业务事实、不调用模型、不恢复 grant。

Harness 指令链：单消息 grant → Registry 按 scope 注册业务工具与 `workspace_guide` topic → 初始请求只携带常驻安全核心、核心工具 Schema 与历史/回执 → 模型按需读取一个或最多两个已授权领域的代码所有指南并加载获权辅助定义及提议定义 → 查询真实事实或创建待确认 Proposal。指南没有业务数据和执行权，领域命令仍由服务端解析、冻结预览、人工确认及共享事务门禁；H5-G18 限制两个重载领域的联合选择，其余双领域在最大固定夹具下仍低于 64 KiB 硬上限，不保证任意长用户输入都能容纳。

H5-E13 工作区导航链由 H5-G46/H5-G49 补充：用户为一条消息显式选择 `workspace_ui` → Registry 仅注册固定面板枚举的 `workspace_open_panel(panel[, companion_panel, split_ratio])` → Sidecar 单面板沿用 `workspace_panel`，双面板发送可含受限初始比例的 `workspace_panels` → 前端验证当前 generation、按请求激活/打开标签并可设置其主面板比例。该链没有面板→模型的数据边，不记录事件，不读取内容、不接收 URL/路径/命令，也不启动终端、导航浏览器、执行 Git/文件操作或创建 Agent Run；H5-E4 的人工点击链接仍是无需 scope 的替代入口且每次新开标签。

H5-E14 浏览器新网址请求链：用户显式选择 `workspace_browser` → Registry 注册 `workspace_request_browser_navigation(url)` 的 HTTP(S) schema → 模型提出一个地址 → 当前 generation 的 SSE `workspace_browser_navigation` → 前端严格验证后显示完整 URL → 用户明确确认 → 桌面原生命令成功返回新标签快照后才消费请求（网页模式为同次点击打开外部浏览器）。原生失败保留待确认请求和原活动地址，用户可重试或拒绝；12 标签上限时禁用确认直到腾出容量。H5-E31 又在同一 scope 增加顶部所述闭集当前标签动作，两类请求共用一次名额。模型不直接导航，拒绝、未确认或流结束均不会自动访问；页面正文、Cookie、登录态、历史、下载和路径不会进入模型上下文。

H5-E15 基础七类记录定位链：用户选择 `workspace_ui` 加 `work` 或 `clients` → Registry 才注册 `workspace_request_record_navigation(record_type,record_id)` 的闭集 schema → Sidecar 重验 canonical ID、对应读取 scope 和记录存在性 → 当前 generation SSE `workspace_record_navigation` 只带 type/id → 前端以代码映射内部 route 并显示确认卡 → 用户明确确认 → 打开原生记录并保留可用的 `return_session`。后续 H5-E16/E17/E30 扩大闭集但不改变这些原类型的无额外字段契约。模型不能传入 route、路径或 URL，定位请求不携带记录正文/标题/文件，不创建业务写入或 Agent Run；拒绝、未确认或流结束不导航。

H5-E16/E17 Agent Run 与 Submission 定位链：在 H5-E15 同一工具上，用户选择 `workspace_ui+work+outputs` → Registry 才把 `agent_run|task_submission` 加入 `record_type` 闭集 → Sidecar 以 Run 或 Submission ID 联结现存 Task、重验双 canonical ID 并只在服务端推导 owner Task → 当前 generation SSE 仅为这两类附加 `task_id` → 前端严格要求该字段，代码构造 `/tasks/<task>?agent_run=<run>` 或 `/tasks/<task>/submissions/<submission>` 并显示确认卡 → 用户明确确认后才打开精确详情。工具回执、模型参数和其他类型 SSE 都不含 Task 关系、route、执行正文、批次摘要、Artifact 或文件；没有 outputs、拒绝、未确认或流结束不导航，也不产生 Run/Submission 控制、验收或自动续办。

H2-I/J/K 客户与人员命令链：单消息显式授权 → 客户/person 白名单读取（person 仅名称、状态、版本，不含备注/metadata）→ `workspace_propose(client.create/update/person.create/update/client_contact.link/unlink)` 规范化并冻结预览 → 人工逐项确认 → 重验版本、关系占用与完整预览 → 与原生 API 共用领域事务 → 无正文回执和安全导航。人员备注只能来自用户本轮明示；创建固定 active，停用受任务责任、客户关系和计划回访保护。新联系人必须先确认人员创建、重新读取真实 ID，再单独关联；特权身份编辑、账号/消息和任意附件操作不开放。

H2-Q 客户永久删除命令链：`workspace_search/get(client)` 取得 inactive Client 的真实 ID/版本 → `client.delete` 以空 changes 冻结 Project 解绑及活动、联系人关系历史、附件记录和受控文件删除数量 → 本地卡片要求独立永久删除同意 → 确认事务重算完整影响并执行发票、收支、回访保护门禁 → 原生 API 与 AI 审批共用删除命令，附件文件先入 trash，失败恢复、提交后清理。person 与 Project 保留，成功回执导航 `/clients`；备份及外部副本不受影响。

H2-L/H2-O 任务标签命令链：`workspace_task_options(type=tag)` 读取真实 ID/名称/颜色/版本 → `task.create/update` 保存规范化完整 `tag_ids` 集合，或 `tag.create/update/delete` 冻结标签字段/删除影响 → 本地确认卡显示人类可读名称与变化，永久删除另行人工勾选 → 确认事务重读 Task/Tag/关系事实并比较完整预览 → TaskTag 替换或共享原生 Tag 事务与审批原子提交。空集合明确清空任务标签；删除标签只解除关系并递增关联 Task 版本，不删除 Task，不把标签名称猜成 ID。

H2-M 任务层级命令链：`workspace_search/get(task)` 读取真实父任务身份 → `task.create/update` 保存 canonical `parent_task_id`（修改时 null 为顶层）→ 本地确认卡显示原/新父任务标题 → 确认事务重验目标 Task 版本、父任务存在性、递归循环和完整预览 → 共享 Task 事务写层级并协调原/新父链 child-rollup。标题变化或层级竞争拒绝旧卡；不按标题重选、不隐式修改父任务事实。

H2-N 任务永久删除命令链：`workspace_search/get(task)` 取得真实 Task ID/版本 → `task.delete` 以空 changes 冻结十类删除/解除影响 → 本地卡片要求独立永久删除同意 → 确认事务重算完整影响并执行原生活动 Run/专注/Inbox/内容关联门禁 → 共享删除命令先把受控文件移入 trash，再原子提交聚合删除、父链协调、审批和审计；失败恢复文件，提交后清理。成功回执保留历史 ID/最后版本并导航 `/tasks`，不伪造已删除详情。

H2-V 批量任务命令链：`workspace_search/get(task)`、`workspace_task_options(tag)` 和 Project 读取取得真实 ID/版本 → `task.batch_update` 冻结 1–20 个 Task 的同序版本、同一批量动作、逐项前后值及原因 → 人工确认 → 确认事务整体重读任务集合、重验项目/标签/生命周期与完整预览 → 复用原生批量语义原子写入并延迟协调父链/自身进度。批量没有单个业务结果，回执以 Proposal ID/version 1 导航 `/tasks`；它不创建第二套 Task 状态，也不把当前列表页冒充完整集合。

H2-P 项目永久删除命令链：`workspace_search/get(project)` 取得已归档 Project 的真实 ID/版本 → `project.delete` 以空 changes 冻结任务/草稿发票/账本解除关联与笔记/附件/本地文件/完成来源协调影响 → 本地卡片要求独立永久删除同意 → 确认事务重算完整影响并执行路线图/内容、发票/账本及活动 Run 文件引用门禁 → 原生 API 与 AI 审批共用删除命令，附件文件先入 trash，失败恢复、提交后清理。Task、草稿发票和可解除账本保留，成功回执导航 `/projects`。

H2-S 路线图里程碑永久删除命令链：`workspace_search/get(roadmap_milestone)` 取得已归档里程碑的真实 ID/版本 → `roadmap_milestone.delete` 以空 changes 冻结 Project 关系和终态来源事项影响 → 本地卡片要求独立永久删除同意 → 确认事务重算完整影响并执行活动来源门禁 → 原生 API 与 AI 审批共用删除命令，级联关系、标记来源删除并与审批事件原子提交。Project、Task 与 Inbox 审计快照保留，成功回执导航 `/roadmap`。

H2-AC 路线图顺序链：`workspace_search/get(roadmap_milestone)` 取得源与同季度锚点真实 ID/版本 → `roadmap_milestone.move` 仅表达 before/after 意图 → Sidecar 查询最多 100 条完整非归档季度，计算位置并冻结整组指纹 → 人工确认事务重查完整集合、重验预览 → 与人工重排端点共用命令重写全组 `manual_order`/版本并提交审批事件。此链只改排序事实；不改里程碑日期/状态、Project/Task 或 Inbox 来源，也不授予无人审批执行。

H2-AD 内容已发布事实链：用户明确报告外部已发布 → `workspace_get(content_item)` 获取真实 ID/版本 → `content_item.publish` 仅冻结本地状态、实际时间与外链文本（未给时间显示确认时刻） → 用户独立核实平台事实并单独勾选 → 审批事务重读完整预览并与原生端点共用 `publishContentItemInTransaction` → 内容状态/时间/可选外链/版本、旧 Inbox 来源终结与审批事件原子提交。内容模块持有发布事实；AI 只提出建议，不产生外部回执、不访问链接、不自动发布，Task 准备状态仍属于 Task。

H5-G3/G4 缺失权限链：本轮 Registry 仅含已有授权工具及常驻 `workspace_request_access` → 模型提出最多三项缺失 scope → Sidecar 白名单/当前范围校验；交互生成仅发一次元数据 SSE，H5-G9 后台生成仅待保存 → 成功的保存会话在 generation、assistant 消息同事务提交 schema078 请求建议 → 来源会话的主/侧卡片展示代码所有名称，刷新从开放请求恢复 → 用户可打开原权限面板并准备下一条固定续问 → 用户亲自确认并发送后，Sidecar 才为**新 generation**重新验证 grant、建立新 Registry。请求卡/工具回执不是授权事实，不写工作台、会话 grant 或业务数据；失败/取消和非保存会话不留请求，忽略标记 dismissed，新消息受理时标记 consumed，删除会话级联清理。活动交互生成完成前卡片仍为临时状态。后台续办不因请求而扩权或自动发送。

H2-T 内容准备任务关系命令链：`workspace_get(content_item)` 与 Task 查询取得 Content Item/Task 双 ID、双版本及当前关系 → `content_item.link_task/set_task_required/unlink_task` 冻结任务名称、状态、版本、关系状态、required 与条目新旧版本 → 人工逐项确认 → 确认事务重读双对象及关系并比较完整预览 → 人工 API 与 AI 审批共用 `mutateContentItemTaskInTransaction`，关系写入、Content Item 版本递增、旧 Inbox 来源终结和审批事件原子提交。三个动作语义互斥，不修改 Task 状态/责任/版本，不把标题猜成 ID。

H5-D/H4-AP/H5-E18 计划链：单消息 work+actions → workspace_plan → 同会话只追加版本/无正文事件 → **提交后**当前流仅发 `{generation_id,version,step_count}` 失效回执，或原确认卡的 Proposal 决定结算精确失效同会话 query → 主/侧计划卡、右侧 `子智能体` 标签重读同一会话计划 → 原 Proposal/Run 真实回执与原确认卡/工作台详情。`ai_work_plan_revisions` 只拥有意图，不拥有业务状态；分析自报不证明执行，产出提交不替代验收。实时回执不携带 session、标题、步骤、Proposal、审批、产出或本地工具内容，`read` 与无流调用不发送但仍正常保存。右侧镜像仅从 `activeSessionId` 的既有计划读取标题、版本、计数、状态与未替代步骤的标题/状态；真实回执给出的关联身份可回到原建议，白名单 route 可回到工作台记录，但不在右栏展示审批预览或完成操作，读取失败也不能伪装为无计划。计划卡根据已存在的真实 action receipt 向主/侧输入器推荐继续所需的最小额外 scope，但仍需用户重新确认；不推断 `agent_files`，不继承旧 grant。存在待确认、排队、运行或待登记的 action 时，计划卡不提供继续按钮，必须先处理原确认/恢复事实，防止重复提议或重跑。计划不自动注入模型上下文，新消息重新授权后显式读取；自动唤醒/调度和并发编排尚未交付。

H5-E1/H5-E2/H5-E3/H5-E5/H5-E6/H5-E7/H5-E8/H5-E9/H5-E10 续办链：Agent Inbox 读取每个保存会话的最新计划身份，以当前 Proposal/Run/交付事实派生需恢复、待确认、运行中、待接续或完成；`output_pending` 与 `retained` 都归入计划的需恢复状态；从全局 Run 账本筛出 queued/running/failed/interrupted/pending/retained，并排除最新计划已引用的 Run；另以 metadata-only 跨来源读模型汇总计划外待确认 Proposal、Task 当前指针所指的待验收 Submission、没有后继 attempt 的自动化失败 Run、每个未删除来源当前最新且没有后继 attempt 的知识索引失败 Job、每个保存会话当前最新的失败 generation、异常 Provider，以及活动系统维护 Inbox Item。H5-E9 由前端单独读取备份模块拥有的启动恢复诊断，将待重启、清理残留或人工核对作为一个全局事项并入角标；H5-E10 则从 Inbox 事实只投影代码白名单维护身份并返回精确详情。各路只返回原会话或对应原生恢复入口。H5-E4 让模型可输出七种严格 `/ai?workspace=...` 本地工具入口；用户点击才切到智能体模式、展开右栏并打开标签。H5-E13 则在独立单消息 `workspace_ui` 下让模型经当前流 SSE 选择同一闭集标签；H5-E14 在独立 `workspace_browser` 下只能请求一个本地待确认 HTTP(S) 新标签；H5-E15 在 `workspace_ui` 加匹配读取 scope 下只能请求一条待确认的本地记录定位，H5-E16/E17 只在 `workspace_ui+work+outputs` 下把已核对 owner Task 的 Agent Run 与 Task Submission 定位扩展到同一确认卡。H4-AQ 则在反向人工交接中仅带入当前活动标签的已脱敏地址；它不提供网页访问。五条 H5 路径和 H4-AQ 都拒绝额外参数、路径、命令和面板或网页内容进入模型。它们不继承 grant、不自动发送、执行、重试、恢复、确认、验收、修改 Provider 或读取对话/知识正文；后台调度尚未交付。

> 文档版本：3.51
> 日期：2026-09-23
> 依据：[PRD v10.72](opc-workspace-PRD.md)
> 当前实现基线：app v0.1.1 / API v1 / SQLite schema 79（052–067 为既有 AI/知识库及评测；068 为配置身份，069 为可靠确认、请求恢复和分段水位线；070 为知识库 PDF 与页码定位，071 为 Agent Run，072 为持久审批建议，073 为审批长预览扩容，074 为 Agent Run 执行身份冻结与唯一并发门禁，075 为可恢复产出登记，076 为会话计划修订，077 为有界续办授权/轮次及消息角色唯一约束，078 为保存会话权限建议，079 为下一条消息完整范围建议；独立轨道交付）；下一条迁移从 `080_*` 开始

> 3.26 说明：H2-H 把内容日历的安全读取及九类受控提议接入逐项审批。确认前不写入；确认时重验版本、完整预览和状态，执行保留 Content Item 与旧 Inbox 投影事务语义。发布确认和破坏性操作不开放。API v1 / schema 76 不变；真实模型与原生桌面体验仍单独验收。
>
> 3.36 说明：H2-R 将已归档 Content Item 永久删除接入独立同意审批。完整预览冻结准备 Task 关系与终态来源事项影响；活动来源阻止，确认重验后复用原生删除事务并与审批事件同事务提交，保留 Task、Project 和 Inbox 审计快照。API v1 / schema 76 不变。
>
> 3.37 说明：H2-S 将已归档 Roadmap Milestone 永久删除接入独立同意审批。完整预览冻结 Project 关系与终态来源事项影响；活动来源阻止，确认重验后复用原生删除事务并与审批事件同事务提交，保留 Project、Task 和 Inbox 审计快照。API v1 / schema 76 不变。
>
> 3.38 说明：H2-T 将 Content Item 的准备 Task 新增、required 调整和解除接入逐项审批，绑定 Content Item/Task 双版本和完整关系预览；确认与人工 API 共用关系事务，只改变关系及条目版本，不改变 Task 事实。API v1 / schema 76 不变。

> 3.39 说明：H2-V 将 AI 批量 Task 更新接入同一人工确认边界。批量卡冻结 1–20 个 Task 的同序版本和逐项前后值，确认时复用原生批量事务语义，整批成功或整批失败。API v1 / schema 76 不变。
>
> 3.40 说明：H4-AK 将“设置 → 数据与备份”接入无授权排查交接。只暂存页面级备份数量、计划状态和恢复诊断状态，不复制备份内容、路径、导入/导出文件或容量细节，不授予备份、恢复、删除、导入导出、保存计划或重启能力。API v1 / schema 76 不变。
>
> 3.41 说明：H4-AL 将“设置 → 运行诊断”接入无授权状态解释交接。只暂存环境、Sidecar 阶段、API 健康和版本兼容等脱敏状态，不复制日志、路径、令牌、端口或诊断包，不授予重新检查、日志目录、诊断包或重启能力。API v1 / schema 76 不变。
>
> 3.28 说明：H2-J 把现有 active person 的客户联系人关联/解除接入 `work+clients+actions` 的逐项审批。Client 安全详情返回当前关系与 person 身份版本，不返回人员备注；确认复用原生事务，解除保留历史并刷新 Client/Actor/Search。API v1 / schema 76 不变。
>
> 3.29 说明：H2-K 把本地 person 创建/修改接入 `work+actions` 逐项审批。读取不暴露备注/metadata，备注仅来自本轮明示；确认复用 Actor 事务，停用重验 Assignment、Client contact 和 planned Followup，并刷新 Actor/Task/Client/Inbox/Search。API v1 / schema 76 不变。
>
> 3.30 说明：H2-L 补齐 Task 创建/修改的完整标签集合。标签 ID 来自现有有界候选，确认卡展示名称，确认时重验 Tag 与 Task 版本并复用原生 Task 事务；空数组明确清空，不新增 Tag 写能力。API v1 / schema 76 不变。
>
> 3.32 说明：H2-N 接通 Task 永久删除审批。完整影响预览、独立人工同意、原生引用门禁、共享事务和受控文件补偿确保确认前零副作用、变化拒绝旧卡、失败不丢文件。API v1 / schema 76 不变。
>
> 3.33 说明：Harness 把完整领域手册改为 scope 裁剪的 `workspace_guide` 按需读取，核心安全规则继续常驻；最大能力、20 个知识来源和近 8 KiB 回执的双协议请求新增至少 16 KiB 余量门禁。无新 API、schema 或执行权限。
>
> 3.35 说明：H2-Q 将 inactive Client 永久删除接入完整影响预览、独立人工同意、原生共享事务和附件文件补偿；发票、收支或回访继续阻止，Project/person 保留。API v1 / schema 76 不变。

## 1. 目的

对话产出恢复链：单消息执行 scope → pending Run/Task 的冻结元数据预览 → 人工独立恢复确认 → `recoverAgentRunOutputInTransaction` 与审批决定共享外层事务 → 实际 submitted/retained 回执与精确批次/Run 导航。数据库拥有最终交付事实，文件补偿在事务终结后核对引用；请求取消不阻断回滚。此链不启动执行器、不调用 Provider，也不复制 staging 到模型上下文。

对话重试链：`workspace_propose(agent_run.retry)` → 共享原生重试准备（原 Run 冻结身份/文件契约）→ 本地完整人工预览与重新同意 → 同事务决定、新 Run/parent 链及 queued 事件 → commit 后启动。v2 在读取受控字节前要求本条 `agent_files`；确认重验快照，不复用旧文件同意。回执仅新 Run 安全元数据，不复制执行或任务事实，不改变人工验收归属。

对话停止桥接：`workspace_propose(agent_run.cancel)` → 持久冻结预览 → 人工 `confirm_agent_cancel` → 审批决定与共享取消意图同事务 → commit 后通知执行器。单消息执行 scope 不等于直接执行权限；回执回读 Run 状态与精确详情，running 与 cancelled 不混淆，Task 状态不变。

Agent 取消链：人工命令事务 → 不可变 `agent_run_cancel_requested` 事件 → commit 后取消执行器 context → 收尾事务将 Run 转为 cancelled。成功/失败/staging fallback/重启恢复复用该意图，不另建任务事实；与产出 pending 按事务提交顺序互斥，已持久输出优先保留。running 在收尾前继续承担活动 Run 与文件引用锁定职责。

执行查询链新增：主/侧聊天单消息 `work+outputs` → `workspace_agent_runs` → 同事务 Agent Run 元数据（含 `attempt/parent_run_id` 重试 lineage）与全局 active/pending 计数 → 精确任务执行详情。查询不触发 Run 命令，不带结果正文或 staging；失败恢复仍由详情中的人工入口执行。它不引入第二套执行事实或新的持久状态。

AI 独立轨道 H4-B：Harness 同步步骤回调 → Chat 无正文 `progress` SSE / 活动 registry → 主对话与侧边聊天共享进度。终态恢复直接投影已有 `ai_run_steps`，与最终时间线同序号，不新增业务事实、表或工具结果日志；断流未确认状态不得当成完成，工具建议完成不等于人工授权。详见 [ADR-012](adr/012-ai-run-steps-and-local-metrics.md)。

本文说明各功能模块如何协作、跨模块状态如何流转、哪个对象拥有最终事实，以及后续实现应遵循的依赖顺序。

它不替代 PRD，也不把规划描述成当前代码。各模块的详细用户流程和验收条件见 [模块文档索引](modules/README.md)；当前实现仍以仓库代码和测试为准。

## 2. 总体设计原则

1. **本地优先**：核心功能断网可用，业务事实保存在 SQLite 和应用控制的本地文件目录。
2. **单一事实源**：一个状态只由一个领域对象负责，其他模块通过查询或事件派生展示。
3. **任务即工单**：项目产出拆出的工作、发票待办、审核和提醒后续动作都使用 Task，不新增第二套执行实体。
4. **收件箱负责编排**：Inbox Item 解释“为什么要处理”，Task 解释“具体做什么”。
5. **Actor 只表达责任**：Assignment 保存责任变化；person 不等于在线用户，agent 不等于已完成工作。
6. **Agent 必须验收**：Agent Run 成功表示结果已安全处置；只有 `submitted` 创建 Task Submission/Artifact 并进入待验收，`retained` 只保留 Run 结果。任何交付状态都不自动完成 Task。
7. **事件可重放且去重**：业务事件使用稳定 key，重扫、重启和重试不得生成重复工作。
8. **规划与实现分离**：路由、按钮、样式或预留表不代表业务已交付。
9. **层级与受理关系分离**：Task 父子层级只表达执行分解；Inbox `is_required` 只能来自显式关系事实，不能由父子层级推断或继承。

## 3. 系统分层

```text
┌──────────────────────────────────────────────────────────────┐
│ Tauri / Rust 桌面层                                          │
│ 窗口、单实例、目录、Sidecar 生命周期、基础托盘；通知/文件选择规划 │
└──────────────────────────────┬───────────────────────────────┘
                               │ IPC：运行状态与受控桌面能力
┌──────────────────────────────▼───────────────────────────────┐
│ React / WebView 界面层                                       │
│ 页面、表单、收件箱编排、知识来源/本地检索、Query 缓存        │
└──────────────────────────────┬───────────────────────────────┘
                               │ Bearer HTTP /api/v1
┌──────────────────────────────▼───────────────────────────────┐
│ Go Sidecar 应用服务层                                        │
│ 领域校验、事务、状态命令、受控 Artifact、知识索引、手工 Inbox │
└──────────────────────────────┬───────────────────────────────┘
                               │ SQLite / 受控文件系统
┌──────────────────────────────▼───────────────────────────────┐
│ 本地事实层                                                   │
│ opc-workspace.db（含 FTS5）、运行锁、artifacts、backups      │
└──────────────────────────────────────────────────────────────┘
```

智能体右侧浏览器采用独立原生子 WebView：React 只拥有工具栏与可见区域，Rust 拥有标签列表、活动标签和 WebView2 的真实导航状态。状态快照通过递增 revision 定向发送给 `main` WebView；页面标题、重定向、历史、加载结果由 Chromium 提供。收起面板、切换路由和弹窗遮挡只隐藏网页，关闭标签才释放视图。浏览网页不自动进入 AI 上下文，不调用 Sidecar，不写 Task/Artifact/会话事实。主界面 capability 按 WebView label 限定，外部子视图禁用 WebMessage，所有自定义 command 再检查主界面调用者。多视图窗口的托盘和快捷键使用原生 Window 句柄，不依赖单视图窗口假设。详细接口、浏览数据与平台边界见 [桌面平台模块](modules/desktop-platform.md#内嵌-chromium-多标签浏览器)。

### 3.1 关闭到托盘事实流

```text
SQLite app_settings.general.close_to_tray（持久事实）
          │ GET /api/v1/settings / PATCH 保存
          ▼
React committed ──启动同步──┐
React draft/preview ─点击───┼─固定 bool command─► Rust AtomicBool
React cancel ─串行恢复──────┘                         │
                                                     ▼
                                 CloseRequested + tray available
                                      ├─ true  → hide + keep Sidecar
                                      └─ false → 不拦截正常关闭
```

SQLite 是跨启动事实源，Rust 原子布尔只拥有当前进程的窗口决策，不回写设置。显式托盘“退出”不读取该偏好，始终进入 Sidecar 优雅关闭。Web preview 调用按顺序排队，确保快速切换后取消不会被较早的异步 command 覆盖。

### 3.2 当前实现

- Tauri 已具备基础窗口、单实例、数据目录和 generation-aware Sidecar 启停基座。内置 Sidecar 每代生成新会话令牌并通过端口 `0` 重新请求动态分配；已启动 generation 只有真实 `Terminated` 才安排下一代，自动恢复最多 2 次，退避固定为 500 ms、2 s，当前代连续 Ready 30 秒才重置预算。外部开发模式、显式 shutdown 和事件流关闭但未收到 `Terminated` 都不自动重拉；并发 shutdown 调用共享同一次 stop。
- Go 受管 Sidecar 由 Tauri 注入 `OPC_EXIT_ON_STDIN_CLOSE=true`，父控制管道 EOF 进入与显式 shutdown 相同的 HTTP drain、WAL checkpoint 和数据库关闭；外部/开发模式默认 false。Sidecar 在检查 pending restore、执行迁移或打开 SQLite 前，先对数据库父目录固定 `.opc-sidecar-run.lock` 取得非阻塞 OS 独占锁；冲突立即失败且不接触数据库。
- React 已具备三栏框架、今日/任务/项目/客户能力、Project 任务树/平铺及项目内任务服务端搜索、状态/优先级/类型/标签/排期筛选和分页、可编辑人工笔记、所属 Task Artifact 产出聚合、项目级 Focus 报告/终态历史与追加式活动时间线、客户本地活动时间线、受控附件与 person 显式关联、客户回访详情管理、手工 Inbox 三视图/详情/分诊时间线与已有 Task 活动/历史关系管理，以及共享持久化 Session 驱动的 FocusPage、RightOverview、ticker 和恢复弹窗。共享 `ClientSelect` 已覆盖 Project 新建/编辑、Projects 筛选和 Tasks 筛选：固定以每页 20 条读取既有 Client API，输入经 250 ms 防抖后服务端搜索，稳定分页并传递取消信号，跨页或失败时保留当前选择，inactive Client 保持可见可选，且具备加载、空、错误重试、更多提示和 combobox 键盘语义，不再串行拉取全部 Client。共享 `ProjectSelect` 已覆盖 Task 新建/编辑、Tasks 项目筛选、批量目标项目和 Inbox 拆分任务：固定每页 20 条，250 ms 防抖搜索既有 Project API，以 `q / page / includeArchived` 隔离 Query key 并传递取消信号；候选按 ID 去重，只有显式清除才提交未归项目，选中详情或名称 fallback 保留跨页、失败及当前已归档选择，默认候选不列其他归档项目。生产路径已移除 `getAllProjects` 串行拉全。任务页的选中客户仍只作为列表查询条件，服务端沿 Task→Project→Client 当前关系筛选；计划/截止日期范围、非法区间查询门禁、SQLite 保存视图、根任务树、标签、批量、按钮排序和精确计划组同状态拖拽均保持原契约。Today 已接四组共享同日/跨日期拖拽、空精确日期/未排期落点、版本化任意日期安排、策略安全的开始/完成/开始专注快捷操作、直达共享编辑、版本化确认删除、逾期/未来 24 小时临期快捷筛选，以及受限来源筛选的客户回访待办，Project 已接独立产出/笔记/审计/Focus 反馈状态。
- 任务看板与列表消费同一个严格 Task 契约：切换看板后使用平铺服务端分页，固定显示六状态列，复用全部筛选、最多 100 项批量选择及共享详情入口；跨列拖拽只映射既有生命周期命令，经过确认、版本、负责人、原因及人工验收门禁，服务端成功前不改卡片状态。
- Go 已提供健康检查、Task/Project/Project Note/Client/Client Activity/Client Attachment/Client–Actor Link/Client Followup/Actor/Assignment、D1/D2、Focus Session、手工 Inbox 受理/分诊、已有 Task 关系、一次性与 daily/weekly/weekdays/monthly Reminder、Today 统计，以及可选 Project 过滤的 Focus 终态历史/周期报告 API；Inbox 列表的受限 `source_entity_type=client_followup` 可读取真实到期回访。Task 列表提供与 Today 统计共享固定宽度 UTC 纳秒比较口径的 `due_state=overdue|due_soon`。Project 列表的每种排序均追加 `id ASC`，同名项目顺序确定，并在同一只读事务完成 `COUNT` 与当页读取。`/health` 返回真实 app/commit/API/schema 运行事实，项目笔记、客户关联、Attachment、Activity、Focus、Inbox/关系和 Reminder 写入使用 `If-Match`、幂等快照或事务维护事实。
- Project Artifact 读模型在同一只读事务返回 Artifact/Task/Submission 与 nullable follow-up：Inbox ID/version/status/policy/`source_deleted_at` 及当前 required progress。列表保留 Project 聚合数值 `ETag`，`meta.project_version` 与它表达同一 Project 并发版本；follow-up 不传播进 Project version，Inbox 写入应使用 `followup.inbox_item_version`。当前 Project UI 只深链 Inbox；所有可能改变 follow-up 的成功 Inbox mutation 会失效可信来源 Project，split 另失效 Task、Today、Project。
- React 项目详情把产出放在任务后，显示待拆分/跟进中/已解决/已忽略、required 完成度及阻塞/待验收/取消并深链 Inbox。Inbox split 对可信本地来源默认继承 Project，但每个草稿可清除/改选；独立完成条件写入 Task，person 明确为本地责任记录。活动关系和仍有实时 Task 的历史关系都用 stack-aware Modal 复用全局 Task detail。
- SQLite 当前为 schema 79：v11–v51 交付核心业务；v52–v60 增加 AI/知识库/citation；v61–v62 增加无正文 run steps 与 Provider token；v63–v67 增加评测 Run/Result、dataset/suite 和不可变人工审计。v68 增加 Provider/Run/Review 配置身份，v69 增加请求身份、消息确认摘要、记忆决定和部分压缩水位线，不新增 AI 表；v70（ADR-026）把知识库来源类型扩展到 PDF 并为 chunk 增加页码范围；v71（ADR-027）新增 agent_runs 执行事实与 agent Actor 的 Adapter 关联；v72（ADR-029）新增 ai_action_proposals；v73 扩充审批完整预览容量；v74 为 Run 固定 Task/Assignment/Actor/Adapter/Provider/执行契约版本并增加唯一门禁；v75 为 Run 增加产出登记状态、精确 Submission/Artifact 身份及恢复 staging；v76 新增保存会话的追加式工作计划修订，v77 增加有界自动续办操作态，v78 增加已保存权限请求操作态，v79 扩大下一条消息完整范围建议并保留历史请求/决定状态。ADR-024 的 UTC 趋势仍只从根步骤派生。AI/知识库/Run/计划/权限请求操作态排除便携业务导出，但覆盖于一致性 SQLite 备份；已有版本按仓库迁移链升级至 v79，不接受未知未来 schema（见 ADR-004–029）。
- Client Activity 继续以 `client_activities` 为唯一事实；`GET /api/v1/client-activities` 只读分页聚合所有客户未删除的 note/meeting/system_reference，按规范 UTC 纳秒键和 ID 稳定排序，并附带当前客户名称/状态。左侧客户导航角标按近 7 天统计条数作提醒，点击进客户页，不复制活动、不生成 Inbox，也不推断邮件或提案下载等线上行为。
- Roadmap Milestone 继续以 `roadmap_milestones`、关联 Project 与派生 Task 汇总为唯一事实；列表白名单 `sort=target_date` 在分页前按纯日期和 ID 稳定排序，缺省仍保留季度手工顺序。左侧路线图导航角标统计目标日期落在今天起 7 天内的 planned/active 节点数作提醒，点击进路线图页，不写 Today 副本。路线图卡片仍以 `?milestone=<id>` 打开同一个最新详情读模型，关闭/编辑只清理该参数并保留其他 URL 上下文。左侧导航同时是模式切换入口：工作台模式（默认）保留业务导航，进入 `/ai` 的智能体模式则把该列整体换成会话列表，列宽与工作台同为 220px，主列顶栏同为 64px。从模式切换进入时右侧工具栏先收起，页头「打开右侧工作栏」再打开；展开后只在工作区工具栏收起。当前会话由共享 store 提供给对话区。工作台右侧由独立 `WorkbenchOverview` 聚合专注环形卡、最近 3 条 planned/active 节点、本地当月 CNY 已确认收入、最近 3 条客户动态，直接复用各模块 Query，不新增业务事实。默认宽 280px，可在 240–480px 范围拖动；通用设置可隐藏，窄屏不超过 1180px 自动隐藏。智能体收起/最大化状态与其独立；多标签工具工作栏只属于智能体模式（`/ai`），同一开关切换统一标签工作区与悬浮状态卡。工作区包含本地文件预览、Chromium 网页、Git 只读审查、Windows 手动 PTY、独立侧边聊天与现有 Agent/附件读模型，默认宽 680px，可在 360–1200px 范围拖动（给主对话保留 360px），支持双工具分栏与放大；窄屏打开时临时占满主面，关闭返回聊天。收起后仅在主列宽度至少 1320px 时显示悬浮卡。React 持有短期 tab/root ID，Rust 持有目录授权与原生资源；文件、Git、终端和浏览器内容不入 Harness，不改变 Task/Artifact/业务事实。侧边聊天仍通过既有 Sidecar 会话流（主/侧共用一次生成），不用窗口共享会话 ID；默认只有显式输入与会话上下文。用户可为一条侧边消息显式选择现有 workspace grant，发送接受后清除且不从主聊天或前轮继承；对应持久 assistant generation 的提议卡就在原侧聊消息下逐项人工确认。主/侧同时打开同一 generation 时共享服务端 proposal/query/decision，不复制审批事实；旧任务块和长期记忆建议仍需主对话。安全决策见 [ADR-028](adr/028-human-operated-agent-workspace.md)。工作台环形卡与 FocusPage 共用同一活动 Session/本地循环，不重复累计工时；左侧边栏不再挂载计时组件，仅保留“专注”导航。
- H4-AM/H4-AN/H4-AO/H4-AQ 是四项严格例外：用户在已打开的 Git 只读审查、普通文本预览、已有输出的手动终端或当前活动 Chromium 标签主动点击交接，React 才会把无本机路径的有界 Git status/diff、已显示文本、近期终端输出尾部或已脱敏网页地址作为普通聊天草稿；用户仍须再次带入并发送。H4-AQ 先移除 URL 的 query/hash/credentials，且不含 title/body/Cookie/login/history/download/tab/root/path；H4-AO 不含 PTY/root/路径或输入，且标出本地捕获或原生输出环截断。快照不构成 Harness 工具、workspace grant、`agent_files`、终端或浏览器能力，不授予 Git/终端/文件/浏览器/网络控制能力，模型也不能刷新、扩展、扫描、访问或控制对应内容。
- 根级质量门禁与运行架构解耦：`check:source` 验证仓库可移植源码、文档和 Sidecar/Web 产物，`check:rust` 验证需要平台原生工具链的 Tauri/Rust 层，`check` 严格组合两者。源码门禁通过不等于桌面链接、安装包或三平台验收通过。
- 一致性备份与恢复已形成独立维护纵切：进程级数据库父目录运行锁先覆盖 pending restore、迁移与 SQLite open，进程内普通 API、Focus heartbeat 与 Reminder 扫描再共享维护读锁，创建/安排恢复取得写锁；SQLite 快照、全部 active objects/avatars、marker 和 manifest 在同卷 staging 中完整校验后原子发布。手工 `POST /api/v1/backups` 在幂等重放未命中后，迁移/导入/恢复内部链在各自不可逆边界前，统一按 SQLite 分配与数据库文件上界、active 受控文件、marker/manifest 估算载荷，增加 20% 且最低 64 MiB 余量，并只探测 backup root；恢复另把目标包 pending 副本与 plan 上界加入同一次需求。精确等于需求允许继续；空间不足/容量无法确认分别以 507/503 或启动失败安全拒绝。拒绝无备份 staging、新回滚包、业务变化或 generic incident。已有工作区启动时先执行非破坏性迁移；首个连续文件头带 `-- migration: destructive` 的迁移会触发迁移门禁。恢复安排通过容量准入后创建当前状态回滚包并冻结写入，下一次 Sidecar 启动在 live 资源打开前同时交换数据库、objects 和 avatars，失败整体回滚、成功以 applied 提交点防止重复执行。
- schema v44 的计划备份复用同一维护写锁、备份互斥锁、容量准入与完整验证链。Sidecar ready 时执行一次错过计划补偿，之后默认每分钟按策略 IANA 当地日扫描；同一当地日原子认领一次。manifest/list 的 `kind=scheduled` 是保留事实，超限清理只处理 scheduled 包；manual、内部回滚以及 pending restore 的目标/回滚不参与。策略与最近结果属于数据管理维护事实，不进入业务 JSON/ZIP、Workflow Event 或云服务。
- 健康启动后的恢复结果诊断由数据管理 API 持有：读取当前 pending、本进程 StartupRestoreResult、applied 清理残留、failed 隔离和 invalid 记录，只投影规范 ID、请求时间、状态与计数。设置页用它恢复重启门禁和展示结果；诊断不暴露路径/底层错误、不自动删除。数据库打开前则由 Sidecar stdout 的固定阶段码经 Tauri 映射为恢复页进度，二者不复用 API 或泄露恢复包身份。
- 基础业务 JSON 导出在单 SQLite 读事务中读取显式业务表白名单，以稳定表/列/行结构下载；Workspace Avatar 与 Task/Client/Project 文件只保留数据库元数据和 active 文件摘要，不嵌入正文，运行令牌、绝对路径、identity、幂等/迁移/墓碑/派生表不进入包。它是可迁移业务快照，不替代含文件的一致性备份。
- 含文件业务 ZIP 导出在维护写锁内完整生成后才响应：`manifest.json` 记录 source、业务 JSON 及全部 active 受控文件的安全相对路径、size/SHA-256，`files/` 携带正文；复制时任一文件漂移都会整体失败并清理 staging。该包排除 SQLite、identity、store marker 和运行维护事实。
- 业务 JSON 与含文件 ZIP 导入均先 strict 预检。当前 schema 包继续验证固定表列、标量行、终态 Focus；ZIP 额外校验 manifest、文件全集、安全路径、size/SHA-256、数据库文件元数据与目标文件碰撞。预检在只读事务中从 SQLite 主键事实流式统计每张非空目标表，返回目标行数、源行数和精确主键重叠数；builtin Actor 与未修改的默认 Automation Rule 不冒充用户事实。跨 schema 包通过基础封装/标量校验后分类为 `source_schema_older / source_schema_newer`，不把未验证列写成兼容。同 schema 空目标返回 `replace_empty`，非空且零主键冲突返回 `append`，两者使用不同确认词；append 保留目标 owner/设置/业务事实，只把源规则合入仍未修改的默认 Automation。正式应用在维护写锁与备份互斥锁中再次预检后，先通过共享容量准入并创建已校验回滚备份；JSON 在单事务中写入，ZIP 先无覆盖发布文件，再于 DB 提交前复验磁盘正文，失败补偿本次文件。空间不足和容量无法确认分别返回导入专用脱敏 507/503，且不创建回滚包或改变业务事实。主键冲突逐条策略、UUID 重映射和跨 schema 升级继续拒绝。
- 任务读取已返回项目/父任务标题、标签和子任务统计；任务与标签写入使用 `ETag`/`If-Match`，父子或嵌入标签事实变化会使相关任务版本失效。
- 任务批量移动项目、改计划日期、加/删标签和完整计划日期组排序都在事务中先校验全部 ID/版本，再整体提交或回滚。
- 任务响应嵌入的项目名也属于版本快照：Project 名称变化或硬删除会递增关联 Task 版本，避免基于旧项目上下文覆盖任务。
- 任务可关联项目；任务读取返回 `project_name`，项目读取从关联任务派生进度及 `actual_minutes` 合计。
- 归档项目不再接受新任务关联；schema v5 让任务、发票和客户聚合事实变化同步失效 Project `ETag`，避免基于旧汇总完成、归档或硬删除。
- Client 列表/详情/创建/编辑/停用/恢复/确认硬删除已接真实 API；创建支持首次响应快照幂等，PATCH/DELETE 使用聚合 `ETag`，项目数从 Project 实时派生，最近动态从未删除 Activity 派生。人工 note/meeting 支持幂等创建、稳定分页、活动版本化编辑和带原因软删除；Project complete/reopen 会把同一事务生成的 Workflow Event 投影到事件发生当下所关联 Client 的只读 `system_reference`，后续改绑不搬迁旧活动且不回填历史事件。Client Attachment 支持严格 multipart 上传、稳定分页、完整性下载、软删历史和聚合删除文件补偿；Client contact 支持已有/原子新建 person 二选一、单 active 关系、带原因解除和不可变历史。相关变化都会使旧 Client 版本失效。Project 客户关联变化使旧 Client 版本失效，Client 名称变化继续使旧 Project 版本失效；Invoice 强引用阻止删除，Project 可选关联按外键置空。
- schema v7 以固定 UUID 初始化唯一 owner 与 system，按历史任务完成状态幂等回填 owner Assignment 和 `migration_assignment_backfill` 事件；数据库保护内置主体、活动分派与引用历史。
- 设置中的“人员与责任”已接真实 Actor API：可管理本地 person、编辑 owner 展示名并查看 system；创建支持幂等重放，读取/更新使用 `ETag`/`If-Match`，存在活动 Assignment 时 API 与数据库共同拒绝停用。“关于”按需读取 `/health`；“运行诊断”再读取并白名单化 Tauri Sidecar 状态，展示环境/生命周期/版本兼容、复制脱敏摘要并下载诊断包 v1。诊断包只含版本/平台、SQLite 健康/迁移和系统维护错误码汇总，原始令牌、地址、路径、错误和业务正文不进入诊断模型或 ZIP。
- React 根节点先由桌面服务恢复闸门保护：浏览器开发模式直接放行；桌面 `starting / restarting` 时只显示安全启动/恢复页，`error` 或状态读取失败时拦截全部业务/设置 bootstrap，支持重新检查、打开日志和安全重启，且不渲染 Tauri 原始 message。受管状态携带 generation；从 ready 进入任一非 ready 状态会清除运行期连接、取消并清空 TanStack Query，下一次 ready 只有在清理完成后重挂业务树，ready generation 变化也会补偿前端漏过中间 `restarting` 的轮询。路由树另由全局渲染错误边界保护：页面或 AppShell 渲染失败时替换为安全恢复页，原始异常不显示或持久化；用户可重新渲染、返回今日，或打开位于错误边界外的设置运行诊断。路由变化会复位失败状态。
- 设置事实层已接 schema v16 与 `GET/PATCH /api/v1/settings`，schema v27 再接受控头像，schema v42 把设置值升级到 v2 并加入 `general.close_to_tray`。头像选择只写 preview；关闭到托盘点击会排队调用固定布尔 Tauri command，保存持久化，取消在已有预览调用之后恢复 committed；启动门禁应用服务端快照后同步 Rust 状态。浏览器调用安全返回 false，不伪造桌面事实。
- 任务详情已接 Assignment API/UI：可查询当前 assignee/reviewer 与结束历史，完成首次分派、改派和结束；命令使用 Task `If-Match`/`version`、可选幂等快照和事务化 Workflow Event。完成 Task 会结束活动 Assignment，重新打开不会恢复旧记录。
- Task 已扩展为 `todo / in_progress / blocked / waiting_review / done / cancelled` 六状态，并通过 `start / block / unblock / complete / cancel / reopen` 六个显式命令改变生命周期；新建只能进入 `todo`，旧通用状态端点返回 410。开始要求活动负责人，阻塞/取消要求原因，解除阻塞由服务端恢复来源状态，完成/取消会原子结束活动 Assignment，重新打开不会恢复旧分派。
- 任务详情已提供按需加载的通用 Task Workflow Event 时间线；生命周期、Assignment 和迁移事件按时间与 `command_seq` 倒序展示，事件记录受数据库不可修改/删除保护。
- Project 创建、资料编辑、生命周期转换与永久删除也复用通用不可变 Workflow Event；producer 与原项目写命令同事务，创建幂等重放跳过 producer，事件失败回滚命令。`complete` 还在同一事务按 Project ID + 完成后 version 投影一个完成收尾 Inbox Item；`complete/reopen` 若当时存在 `client_id`，再以 Workflow Event ID 为稳定来源创建一条 Client 系统活动。任一投影失败都会回滚 Project 状态、事件与其他投影。Project 时间线按时间、命令序号和事件 ID 倒序读取，返回当前 Project 版本，不成为项目状态的第二事实来源。
- 人工 Project Note 是独立可编辑业务事实：创建、编辑和带原因软删除分别递增笔记版本及 Project 聚合版本；归档项目只读，删除历史不可再改写。它不写入或覆盖不可变 Workflow Event，Project 硬删除时随聚合级联删除。
- `review_policy = manual` 已在 Task 新建和受限编辑中开放；策略只可在 todo 且没有任何 Submission 历史时改变。manual Task 具备活动 assignee 与 owner reviewer 后，可提交摘要以及 text/link/structured/file Artifact，进入 waiting_review，由 owner 接受或要求返工。
- schema v9 和 UI 已交付 Submission/Artifact 历史、受控文件 store、安全下载、完整性状态、确认软删除、Task 聚合硬删除补偿，以及提交/审核/撤回/删除时间线。不可变 Artifact deletion tombstone 与删除事实同事务写入并在 Task 聚合删除后保留，供启动恢复判定授权删除。普通人工/AI 代录路径的 producer 来自活动 assignee，submitter/recorder/reviewer/withdrawer/deleter 为内置 owner；H5-B Agent 交付是受信领域例外，冻结 agent 同时作为 submitter/producer，system 作为 recorder/event actor，owner 仍负责验收。
- schema v30 已交付直属子任务自动协调：非取消直属子任务至少 1 个且全部 done、父任务为 todo/in_progress + manual、active owner/person assignee 与 active builtin owner reviewer 齐全时，由 system 创建零 Artifact 的 `origin=child_rollup` 批次并最多推进到 waiting_review。失效可撤回 pending 系统批次；accepted 父任务只在子任务完成条件失效时系统重开。manual 历史与 changes_requested 系统批次不被覆盖。
- Tauri 与开发脚本均提供独立 Artifact root；Sidecar 先在数据库父目录获取 `.opc-sidecar-run.lock`，再于 ready 前校验 marker 的 `format_version / database_id / store_id`，并用不可变数据库身份与一次性 `artifact_store_id` 建立双向绑定，随后获取 Artifact root 独占锁并协调 `.staging/objects/avatars/.trash/.quarantine`。数据库运行锁防止第二个进程恢复、迁移或打开同库，Artifact 锁防止双进程协调同一文件根，两者不可互相替代。Task/Client/Project 文件使用 `objects/<uuid>`，Workspace Avatar 使用 `avatars/<uuid>.<ext>`；schema v27 阻止四领域 ID 冲突。内容不经过任意路径 API，读取前复验 size 和 SHA-256。
- Focus Core A（事实迁移）、B（API/状态机/事务）、C（前端接入与恢复）、D1（历史与周期报告）、D2a（Task 详情记录）、Project 详情读取和 D2b 日期范围回顾已交付：15 秒 Sidecar heartbeat 不递增版本，启动把遗留 active 转为 recovery_pending；Today 和周期报告只按 completed 的已关闭正时长 interval 与 IANA 本地日边界 overlap 聚合；终态历史稳定分页，7/30 天、本月和最多 93 天自定义趋势与 Streak 均由服务端事实派生；Task/Project 详情按需读取关联历史，Project 过滤按 Task 查询时当前项目归属，均不复制或写回 Session；设置 committed/draft/preview 不改活动 Session。 AI 独立轨道的 workspace_focus 读取同一快照但不刷新心跳；focus.pause/resume 复用 focus_commands.go 并与审批同事务，按确认时刻结算，保留 Task/本地循环事实。审批结果取消在途查询后刷新共享缓存；Session 身份与本地异步转换已由 H3-A2 前置补齐，AI 开始、结束/取消与三种中断恢复也已接入共享事务；开始新循环和计入中断间隔各有额外人工同意，stop 计入 Task 工时但不完成 Task，cancel/interrupt 不入账。H3-A3 的 workspace_focus_report 与人工 /stats/focus 共用 readFocusPeriodStats，在单只读事务取事实，按显式 1–93 日/IANA 窗口分维度有界返回总量、分布和连续天数；保持当前 Project/Tag 归属与非互斥标签、非冻结分页语义，不写心跳/工时。 H4-C 由工具代码生成同条件报告 URL，主/侧聊天共用严格允许列表并附加原会话导航身份；FocusPage 按 URL 读取当前报告与定位维度，返回主聊天不继承授权、不覆盖人工选择。报告错误独立于活动快照，筛选不影响计时或最近历史，无新事实副本。
- T-11A1/T-11B 已交付手工 Inbox Item 创建、三视图列表、详情编辑、单条/快照式全部已读、稍后/恢复、带原因解决/忽略、重开和 Inbox Event 时间线；T-11A2 已交付已有 Task 活动/历史关系、服务端实时进度、required 修改、带原因软解除、`open / tracking` 联动、按活动关系重开、关系事件和 Task 删除互锁；T-11A3 已交付一次性及 daily/weekly/weekdays/monthly 本地 Reminder、启动补偿、周期扫描、DST/月末安全推进和幂等 Inbox/下一 occurrence 投影。
- 当前仍未实现 Focus 原生反馈、Client 的邮件/日历等外部活动来源及客户级财务聚合、法定节假日/自定义 Reminder、跨平台/external Agent Runtime、任意 filesystem/Shell/browser/Git/terminal 自动访问、主键冲突逐条合并/UUID 重映射与跨 schema 升级导入，因此完整工作编排仍是部分完成。Windows 内置 Runner、受人工确认的 AI→Agent Run 启动，以及 H5-C 受控 Task Artifact/当前 Project Attachment 输入与单文件/有界多文件输出已交付，但不等于自治电脑操作或多步计划器。导入的同 schema 零主键冲突追加、只读冲突清单与 schema 方向分类已交付；存在冲突的包仍不能应用。Project 生命周期的本地系统活动投影和客户回访本地计划/提醒已交付，但不代表客户互动或对外通信。Focus 分析与业务 JSON/含文件 ZIP 安全导入导出已交付；已登记来源、运行期数据库操作失败及按 1–100 GiB 设置阈值运行的低空间投影已接 Inbox；Sidecar generation-aware 有界重启、父管道 EOF 退出、数据库运行锁、双进程日志、request ID、全局恢复页和数据库打开前白名单恢复进度已交付，启动前备份选择仍待实现。T-02 仍部分完成：没有通用 OS Job Object、进程组或孙进程治理，hard-hung orphan 仅被运行锁挡住而不会自动回收，真实 Tauri/Sidecar 父崩溃、进程树、三平台与安装包尚未验收。

H3-E1 独立 finance 授权只向 Harness 注册 workspace_finance；工作事项和客户 scope 不继承财务读取。账本/发票白名单排除备注、联系资料、客户名称、PDF 和审计正文；同币种/明确日期统计与原生收入 API 共用 readIncomeStats，模型不做第二套聚合。代码返回真实记录和报告 route，UI 绑定原会话并在工作台重新读取当前事实；人工详情与财务命令不把正文或权限反向自动交给模型。schema 045–048 的账本、发票原子付款入账、PDF 与到期 Inbox 基础链已在源码实现，H3-E2 已接独立 finance_actions 的收支创建/修改/作废建议，共用 financial_entry_commands.go 事务入口；确认需额外人工勾选、版本与完整预览重验，关联名称和本地备注只在人工卡展示，不发送工具/回执。账本、领域审计与审批同事务，确认/模糊失败后取消旧查询并刷新账本和共享统计。H3-E3 另以 finance+invoice_actions 与 confirm_invoice_effects 人工确认接入六种发票命令，原生/审批共享 invoice_commands.go；完整预览重验，日期按同一本地时钟。付款的唯一收入、活动到期 Inbox 解决及审批同事务，逾期领域事件/启用规则投递同事务捕获、提交后执行，不能以发票确认推断自动化成功。关联查询取消旧响应后刷新。H3-E4A 的 invoice.delete 复用 invoice_delete_command.go：仅草稿且无账本关联，原生/审批按 PDF store→DB 顺序加锁，文件先入受控 trash，提交后清理、失败恢复，启动 reconciler 处理崩溃窗口。额外 confirm_invoice_delete 与稳定 PDF 身份预览防止未经同意的删除/替换；审批和审计历史保留，结果链接发票列表。H3-E4B 以 invoice.generate_pdf 和额外 confirm_invoice_pdf 同意接通 PDF 生成/替换，原生/审批共享 invoice_pdf_command.go 的文件生命周期；asset/内部分区幂等历史结果/决定同事务，补偿跟随外层提交。结果 ID 是 PDF 资产，route 为发票；人工按 asset_id+sha256 精确下载，旧文件被替换不能冒充新文件，文件内容不入模型。H3-E5 以独立 finance_exports 提议明确范围 CSV，与原生共用有界编码/公式防护。审批保存筛选、完整匹配数和内容指纹；人工确认与下载分离，两次重验数据，不保存正文或发送给模型。历史结果只是批准身份，下载无法证明用户已保存；范围变化需新提议，无新业务事实或 schema。整体 v0.4 验收仍待。详见 [财务模块](modules/finance-invoices.md)。

### 3.2 目标扩展

- v0.1：在已交付的 Task/Project/Client、共享服务端搜索 Client/Project 选择器、Actor/Assignment、D2、Focus、手工 Inbox/Reminder/Task 编排、Project Artifact→Inbox→Task 人工闭环、来源投影、基础备份闭环、业务导入导出和 Sidecar 有界恢复上，继续完成真实浏览器/WebView、父崩溃/进程树、三平台与安装包验收；Focus 原生反馈、重复/原生通知独立延后。v0.1 不启用 AI、LLM 或 Agent Runtime。
- v0.2：Windows builtin Agent Run、可恢复产出登记、H5-C 受控文件输入/单文件与有界多文件输出、H5-D 会话计划、H5-E1/E2/E3 续办、H5-E4 人工点击的严格工作区工具入口、H5-G58 适配器异常投影和首批预设自动化已交付；其余尚未建模的失败/恢复来源、任意文件系统自动操作或自治多步工具编排、external/cross-platform Runner 仍待。
- v0.3：路线图、内容日历、高级备份配置和规划增强。
- v0.4：收入/支出、发票和客户回访。
- 独立轨道：AI 助手 AI5/AI6、citation、离线 scorer/Q2 本地模型 8-case smoke/四类 6-case topic/24-case full Actor、suite 趋势/category/failure/Wilson/人工评审候选/不可变人工决定审计、Q3 run steps/Provider token/本地聚合/UTC 用量趋势已交付；知识库已交付文本/Actor/FTS/生命周期与 PDF 页码提取（ADR-026）。授权引用、费用和自动路由待定。

## 4. 核心领域对象与事实归属

| 对象            | 拥有的事实                                                                                                                                                                                                                                                                               | 不应保存的事实                                                                                                             |
| --------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| Task            | 工作内容、生命周期、完成条件、验收策略、当前 Submission 指针、父子层级及直属子任务派生进度                                                                                                                                                                                               | 收件箱已读/稍后、required 关系、Agent 单次运行状态                                                                         |
| Project         | 项目资料、客户关联、项目状态                                                                                                                                                                                                                                                             | 手工维护的任务完成百分比、Focus 汇总或 Inbox 跟进状态                                                                      |
| Project Note    | 项目人工上下文、记录时间、版本和软删除历史                                                                                                                                                                                                                                               | Project 生命周期或系统命令审计                                                                                             |
| Client          | 客户资料、本地活动和受控附件元数据                                                                                                                                                                                                                                                       | 重复存储项目数、已付款总额或文件正文                                                                                       |
| Inbox Item      | 事件来源、分诊、已读、稍后、解决策略                                                                                                                                                                                                                                                     | Task 执行状态和负责人副本                                                                                                  |
| Actor           | 本地责任主体身份和启停状态                                                                                                                                                                                                                                                               | 某项任务的当前状态                                                                                                         |
| Assignment      | 当前负责人和改派历史                                                                                                                                                                                                                                                                     | Task 完成状态                                                                                                              |
| Agent Run       | 一次本地执行的 v1/v2/v3 输入快照、受控文件引用、冻结执行身份/输出契约、生命周期状态、结果及 `submitted/retained/pending` 处置事实；Web 端所有展示面经 `agentRunLifecycle()` 统一映射为 pending/running/recovery_required/succeeded/submitted/confirmed/failed/cancelled/unknown（AH-02） | Task 是否最终验收完成（`confirmed` 只来自同一 Submission 的 `accepted` 事实）；Task Submission/Artifact 权威正文；本机路径 |
| Task Submission | 一次提交的来源、摘要、批次状态、提交/审核/撤回责任和时间                                                                                                                                                                                                                                 | Task 内容、Artifact payload                                                                                                |
| Task Artifact   | 文本、文件、链接或结构化产出、producer/recorder、完整性和软删除                                                                                                                                                                                                                          | 验收结论                                                                                                                   |
| Reminder        | 本地触发时间和调度状态                                                                                                                                                                                                                                                                   | 到期后的处理进度                                                                                                           |
| Focus Session   | 专注区间、累计秒数、结束原因                                                                                                                                                                                                                                                             | Task 的业务完成状态或历史 Project 快照                                                                                     |
| Invoice         | 发票金额、客户、日期和业务状态                                                                                                                                                                                                                                                           | 收件箱处理状态                                                                                                             |
| Workflow Event  | 谁在何时做了什么、状态前后值                                                                                                                                                                                                                                                             | 可被业务 API 修改的当前状态                                                                                                |

## 5. 功能模块协作总览

| 模块                                       | 主要输入                                                                                                                      | 自己负责                                                                                                                                                                                   | 主要输出 / 下游                                                                                                                                                            |
| ------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [今日](modules/today.md)                   | Task、Focus、Inbox 派生统计、客户回访到期 Inbox 投影                                                                          | 当日执行入口、聚合展示、完整计划组排序、同日/跨日期拖拽、版本化改期、实时截止风险筛选、客户回访待办，以及受策略约束的生命周期/专注快捷操作                                                 | 计划日期事实、源/目标组顺序结果、版本化开始/完成、绑定 Focus、打开收件箱或客户详情                                                                                         |
| [任务](modules/tasks.md)                   | Project/Client 服务端选择结果、Actor、Inbox 关系与来源                                                                        | 唯一工单、六态生命周期、完成条件、Submission/Artifact、manual 验收、直属子任务汇总待验收，共享 Project 选择，以及沿 Task→Project→Client 当前关系执行客户筛选与阻塞来源投影                 | Project 进度、Task 事件、阻塞 Inbox Item、后续 Inbox 进度与 Focus 工时                                                                                                     |
| [项目](modules/projects.md)                | Client 服务端选择结果、Task、Focus、受控文件 store、Artifact 来源 Inbox                                                       | 已实现资料、生命周期、稳定分页读模型、任务/Artifact 聚合、nullable follow-up/实时 required 进度、笔记、附件、项目级 Focus、活动时间线及来源投影                                            | 产出状态深链 Inbox；完成收尾事项进入 Inbox；complete/reopen 向事件时关联 Client 输出只读系统活动；不复制 Inbox/Task 写事实                                                 |
| [客户](modules/clients.md)                 | Project、Invoice、Activity、受控文件 store、person Actor、回访到期 Inbox 投影                                                 | 当前已实现基础资料、状态、项目数/最近活动派生、供 Project/Task 使用的服务端分页搜索选择读模型、Project 关联、人工/项目状态时间线、Client Attachment、显式 contact 关联与本地回访管理       | Project complete/reopen 只读系统活动；回访到期 Inbox 投影、Today/Inbox→客户详情入口；其他外部来源和发票仍属后续纵切                                                        |
| [收件箱](modules/inbox.md)                 | owner 手工录入、Reminder 到期、已有/新建 Task、follow-up Artifact、Task 阻塞/临期、Project 完成、客户回访与系统维护来源       | 已交付受理分诊、显式 required、来源 Project 继承/清除、完成条件、owner/person 分派、共享 Task 详情、自动结清/重开、来源删除协调、风险深链和客户回访来源上下文                              | 输出 Event、实时进度及 Today/Sidebar 计数；客户回访精确定位客户详情中的回访；成功 mutation 失效来源 Project，split 另失效 Task/Today/Project；Task 层级不隐式创建 required |
| [本地提醒](modules/reminders.md)           | owner 输入、本地服务端时钟与浏览器 IANA 时区                                                                                  | 一次性/daily/weekly/weekdays/monthly 系列、当地日锚点、独立 occurrence、scheduled/fired/cancelled、启动补偿与稳定键投影                                                                    | Reminder Workflow Event、Reminder Inbox Item 与下一 scheduled occurrence；原生通知和复杂日历规则待后续                                                                     |
| [Actor](modules/actors.md)                 | 设置中的本地 person 管理、任务详情 Assignment                                                                                 | owner/person/system 身份、人工分派、生命周期责任与 D2 producer/recorder/reviewer 审计；agent 绑定受控 Adapter，但分派本身不启动执行                                                        | Task 时间线、Submission/Artifact 责任；经独立确认创建的 Agent Run                                                                                                          |
| [本地 Agent](modules/local-agents.md)      | agent Assignment、Task 上下文、冻结的 Provider/Adapter/执行契约身份、显式选择的受控 Task/Project 文件、执行与文件两项人工确认 | 单次受控 v1 文本、v2 输入/单文件或 v3 2–4 文件输出执行，prepare/prelaunch 复核与可恢复结果处置                                                                                             | `submitted` 输出 Submission/Artifact 并进入待验收；`retained` 留在 Run；`pending` 等待恢复                                                                                 |
| [专注](modules/focus.md)                   | 当前 Task 与查询时当前 Project/Tag 关系                                                                                       | 活动 Session、有效工时、终态历史和 completed-only 周期读模型                                                                                                                               | Task actual_minutes、今日/统计数据，以及 Task/Project 详情只读历史与分析                                                                                                   |
| [设置](modules/settings.md)                | schema v42 设置 API/Query committed、schema v44 计划策略、Actor API、`/health`、Tauri 状态与数据维护 API                      | 本地偏好、关闭到托盘、计划备份即时预览/版本保存、person、诊断、备份闭环、业务 JSON/ZIP 零冲突追加；外部目录、冲突合并、升级与启动前备份选择待实现                                          | 布局、主题、Focus 默认值、Actor、版本、诊断、计划/备份/导入导出，以及经固定 command 同步的桌面关闭行为                                                                     |
| [命令面板/搜索](modules/command-search.md) | Task/Project/Client/活动 Inbox 当前事实                                                                                       | 参数化统一本地查找、确定性相关排序、非敏感有上限最近使用与快捷操作入口                                                                                                                     | 只输出稳定详情路由或触发既有受控命令，不复制业务事实                                                                                                                       |
| [数据管理](modules/data-management.md)     | SQLite 与本地文件                                                                                                             | 已实现受控文件一致性、手工/计划/内部回滚容量准入、备份恢复、每日计划/自动包保留、业务 JSON/ZIP 零冲突追加及冲突预检；外部目录、冲突合并与升级仍规划                                        | 当前文件安全、已校验 manual/scheduled 包、计划/恢复状态、准入反馈、便携导入导出、追加结果与只读冲突事实                                                                    |
| [桌面平台](modules/desktop-platform.md)    | Web committed/preview 关闭偏好与 Sidecar 生命周期                                                                             | 原生窗口、托盘可用性×关闭偏好决策、受管 Sidecar generation/重启预算/父管道与 shutdown、权限、运行日志和发布                                                                                | 可运行、可恢复、可诊断的本地应用环境；隐藏不等于退出，显式退出继续关闭 Sidecar                                                                                             |
| [财务/发票](modules/finance-invoices.md)   | Client、Project、owner 确认                                                                                                   | 财务与发票业务事实                                                                                                                                                                         | 本地提醒、Inbox Item、客户聚合                                                                                                                                             |
| [客户回访](modules/client-followups.md)    | Client、Reminder、Actor                                                                                                       | 本地回访计划、终态结果、完成时原子安排下一次计划、客户详情管理、Today 待办和 Inbox→客户详情入口                                                                                            | Inbox 到期项；不自动创建客户活动或外部通信                                                                                                                                 |
| [路线图](modules/roadmap.md)               | Project/Task 派生进度                                                                                                         | 已交付季度里程碑数据/API、项目关联、只读进度、服务端 Project 筛选/分页、新建/编辑/详情/归档恢复/保护性删除、同季度安全排序、年度跨季度/跨年度移动和季度内精确日期调整                      | 里程碑到期/达成已投影本地 Inbox 事件；原生通知待后续                                                                                                                       |
| [内容日历](modules/content-calendar.md)    | Project、Task、日期                                                                                                           | 内容计划、六周月格、IANA/DST 安全改期、拖拽/卡片键盘逐日改期即时预移与失败回滚、人工及 H2-T 受确认的准备 Task 关系、本地发布确认；指定详情由 `?item=<id>` 和单条读取承载；CC2–CC5-B 已交付 | 准备 Task（人工与 AI 审批读写已交付）；审核/发布时间到期事实投影到 Inbox，Inbox 通过内容 ID 精确回到最新详情（已交付）                                                     |
| [自动化](modules/automation.md)            | 消费 Project 完成、Invoice 逾期、Agent 失败与本地时钟                                                                         | 五个代码所有预设、版本化配置、next run、不可变 Run、attempt 与稳定去重                                                                                                                     | 创建本地 Inbox Item、Task 或 Reminder；H3-C1/C2 经人工审批配置/启停及原快照重试                                                                                            |
| [知识库](modules/knowledge-base.md)        | 用户明确选择的 TXT/Markdown/PDF bytes；不接受路径                                                                             | SQLite 受控副本、单邮箱 Actor、FTS5/中文辅助检索、定位（文本行号 + PDF 页码）、重建和级联删除；向 AI 只提供用户选中的 chunk identity                                                       | 带来源/文档版本/行号/字符/页码范围的搜索结果、metadata-only CSV、AI6 显式片段 preview                                                                                      |
| [AI 助手](modules/ai-assistant.md)         | 当前输入、最近回合、摘要/事实、长期记忆、单次显式业务/知识/执行/受控文件范围、显式选择的 Provider                             | 预览/重验、流式问答、记忆、citation、受确认领域命令、受确认 Agent Run/文件启动、无正文 steps/usage/评测；只向模型暴露 generation 内 opaque 文件候选，不拥有业务或执行事实                  | user context、assistant citation、generation/run steps/usage、审批记录与最小回执、会话/记忆；业务、文件与 Run 事实仍归原模块                                               |

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

AI H2-C5 沿用同一事实流，但把“列出快照”和“执行批量写”之间增加不可变人工批准：`workspace_search(inbox_item)` 返回服务端 `snapshot_at`、不受筛选影响的全局未读数及 1000 项上限；`inbox.read_all` 只能复制该时间。提议在本地读取完整候选并保存 `ID+version` 指纹与数量，确认时重算，变化即 `AI_ACTION_PREVIEW_CHANGED`，不会批准另一个新范围。通过后与原生入口共用 `readAllInboxItemsInTransaction`，Inbox `read_at`、逐条 Event、审批和批量回执同事务；批量没有单一 Inbox 结果，以 Proposal 为回执身份并返回实际 cutoff/count。模型不获得执行工具，动作只改已读事实，不分诊、归档、关联 Task 或修改 Reminder/来源。

### 6.2 Task 手工产出验收与后续编排

```text
当前已实现：
manual Task(todo/in_progress) + active assignee + owner reviewer
  → owner 代录 summary / text / link / structured / file Artifact
  → Sidecar 派生 produced_by=assignee，recorded/submitted_by=owner
  → Task(waiting_review) + Submission(pending_review)
  → owner accept → Submission(accepted) + Task(done) + 结束活动 Assignment
  → 或 owner request_changes(reason) → Submission(changes_requested) + Task(in_progress)

受信 Agent Run 交付：
  → 冻结 agent 作为 Submission submitter 与 Artifact producer，system 作为 recorder/Event actor
  → `succeeded + submitted` 才创建 `origin=manual` 的 Submission/文本 Artifact，并把 Task 推进到 waiting_review
  → `succeeded + retained` 只在 Run 保存正文和稳定原因，不创建 Submission/Artifact、不改变 Task
  → 瞬时登记失败先尝试持久化私有 staging 为 `running + pending`；成功落库后，启动或显式 retry 只恢复登记，不重跑模型

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

上图的项目开票节点预填/拆分仍是后续设计。当前付款事务会直接解决该发票的活动 invoice_due 来源，但不完成关联 Task；其他来源仍按各自 resolution_policy 跟踪。AI H3-E3 仅在独立权限下提出草稿/状态/实际回款建议，人工核实确认后通过共享发票事务执行，不自动外发、付款或核验银行到账。PDF 生成/替换由 H3-E4B 额外人工确认后执行，下载仍是独立人工动作，不改变发票状态或自动外发。

### 6.4 一次性与重复本地提醒

```text
owner 创建 Reminder(scheduled)
  → 本地调度器到期扫描
  → source_event_key 幂等生成 Inbox Item(open)
  → Reminder(fired) 记录 inbox_item_id
  → daily/weekly/weekdays/monthly 按 IANA 当地日历生成下一 occurrence
  → owner 解决、稍后提醒或拆分 Task
```

Reminder occurrence 是调度事实；Inbox Item 是该次到期后的处理事实。两者不复用状态。

该链路由 schema v14/T-11A3 建立一次性事实，schema v32 扩展 daily/weekly 系列，schema v40 增加 monthly 与服务端派生的当地日锚点：Sidecar 在 ready 前补扫到期 Reminder，运行中每 15 秒按稳定顺序扫描最多 100 条；每个到期 occurrence 以 `reminder:<id>:due` 为唯一事件键，在一个事务中创建或复用 Reminder Inbox Item、写 system Inbox Event、标记 Reminder fired、写 system Reminder Event，并按 IANA 当地日历创建同系列唯一下一 occurrence。跨 DST 保持当地钟点；monthly 在短月落到月末、后续长月恢复锚点日期；离线漏过多个周期只补当前一条并把序号推进到下一未来时刻。取消唯一 scheduled occurrence 停止系列。系统原生通知、法定节假日/自定义日历规则仍未实现。

### 6.4.1 受限预设自动化（v0.2 首个纵切已实现）

```text
Project 完成 / Invoice 逾期事件 ──────────┐
                                          ├→ 匹配 enabled 代码预设（事件在来源事务捕获）
本地 daily/weekly IANA 计划窗口 ──────────┘
  → 生成 logical_key / dedupe_key
  → action savepoint 内创建本地 Inbox Item、Task 或 Reminder
  → 同事务写 immutable terminal Automation Run + Workflow Event
  → 失败回滚动作，写 failed Run；1/5 分钟有界重试，最多 3 次 attempt
```

自动化表始于 schema v33，保存五个稳定预设的配置与 Run 快照，规则名称、触发、动作和权限由代码目录所有。事件规则以 `rule_id + source_event_id` 唯一，计划以 `rule_id + scheduled_for` 唯一，所有 attempt 以 `logical_key + attempt` 唯一。`automation_event_deliveries` 在来源事务内捕获配置/动作；投递基础设施失败保留快照恢复，不撤销已提交的来源事实；成功动作、Run 与审计原子提交。

daily/weekly 调度先执行到期重试，再按 `next_run_at/id` 扫描最多 100 条；DST 缺失分钟落到首个有效分钟，重复分钟取第一次。离线跨多个窗口只保留最新 due：旧当地日期写一条 `skipped/SCHEDULE_WINDOW_EXPIRED` 并推进，不创建过期 Reminder；当前当地日最多创建一条 Reminder。停用阻止新触发及定时重试，但不撤销已捕获事件的投递/重试，不删除历史。Automation 自身 Workflow Event 没有预设消费者，不能形成递归链；因果深度字段由数据库限制在 0–4。

设置界面通过 `/api/v1/automations` 读取规则、预览、`If-Match` 保存/启停并展示 Run/手动重试。五个预设可用且默认关闭；H3-C3 仅在人工启用后为未来真实 Agent failed 创建本地诊断，不重跑模型。动作没有 Shell、SQL、HTTP 或外发路径；业务 JSON/ZIP 包含 Rule/Run，导入前验证稳定身份、配置、关系和 fresh-target。

ADR-029 H3-C1 的 workspace_automations 在 work 下只选择规则/Run 元数据，不加载原始事件、业务快照或结果正文；模型建议 automation.enable/disable/update 需 work+actions，真实 ID/version，完整稳定预览。原生与 AI 人工确认共享 automation_commands.go 领域事务，规则/事件/审批一起提交；启用或修改已启用规则额外 require confirm_automation_effects，仅 human decision 接受。调度器 next_run_at 可独立推进，不纳入固定预览，实际变更按确认时刻重算；无变化保留下一窗口。前端打开既有设置弹层，取消旧查询并刷新 Automation/Inbox/Reminder；后续回执仅记录动作/状态/ID/版本，不扩大授权。H3-C2 另接入 automation.retry：原失败 Run 的捕获 rule_version 绑定审批，完整配置/动作快照只向人工展示；confirm_automation_retry 必须单独勾选，和原生/后台共用重试事务，不改规则/原快照，保留三次上限及唯一后续尝试。confirmed 只指新尝试已记录，API/后续回执额外读取该不可变 Run 的安全元数据，不读业务实体/正文；实际 succeeded/failed 驱动反馈。决定后还取消并刷新 Task/Project/Today/搜索等可能受影响缓存。不新增事实副本或迁移。

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

H3-A2 前置同步把原生创建/结束/恢复后的本地轮次转换集中到共享 mutation hook，页面不再用异步回调重置循环。持久 presentation state v2 绑定 Session ID，运行时 revision 隔离创建期间的本地重置；v1 休息与轮次保留迁移，不新增服务端事实。活动缓存拒绝跨 Session 的迟到命令和同 Session 旧版本，命令前后取消旧查询再回读。ticker 同步所有未结束状态，明确空快照才清理旧 work；自动 stop 在途保留完成意图，查询未知/刷新/失败时不自动开始下一块。独立观察到的新 Session 开始新轮次，本地确认的下一块才接续。AI 开始/结束/恢复通过共享领域审批接入，前端只回读实时快照，不从旧确认卡重放循环。恢复弹窗可显式跳至 /ai，该路由不阻断聊天；不自动恢复或授权。

### 6.7 本地 Agent 执行（v0.2，H5-C 受控文件纵切已交付）

传输与安全边界由 [ADR-003](adr/003-local-agent-runtime-security.md) 与 [ADR-027](adr/027-builtin-agent-executor-and-run-lifecycle.md) 共同约束。当前已实现 Windows builtin 文本 Runner、任务详情 Run 界面及条件式人工验收提交；external 与未验证平台仍禁止执行。首次使用需要用户显式登记和启用，读取设置或打开任务不会自动创建 Adapter、Actor、Assignment 或 Run：

```text
设置登记 builtin-local-text-v1
  → Sidecar 保存代码所有 manifest（无路径、无凭据）
  → owner 请求安全检查，设置显示 API 返回的真实健康和就绪状态
  → 已验证 Windows builtin 可 healthy / verified / execution_ready=true
  → owner 显式启用，以 If-Match 校验 Adapter 版本
  → 同一事务校验身份、健康与就绪，并创建或校验关联 agent Actor
  → 任一步冲突整笔回滚；未验证平台/external 保持关闭
```

登记和诊断写入 `agent_adapter_registered / agent_adapter_health_checked` Workflow Event。启用与停用是独立于普通应用设置保存的显式操作；固定 Actor ID 冲突、Actor 停用/关联错误，或已启用 Adapter 缺失 Actor，返回 `409 AGENT_ADAPTER_ACTOR_CONFLICT`，不静默补种或改写责任人。Actor 读取 API 增加可空只读 `agent_adapter_id`；旧响应缺失时前端归一化为 `null`，不能凭显示名称或固定 ID 推断可执行。该初始化修复在 schema 71 / API v1 上交付；当前 schema 76 另由 072–073 的审批事实/容量、074 的 Agent Run 执行身份冻结与唯一门禁、075 的可恢复产出登记及 076 的会话计划修订推进。

任务详情的当前执行链为：

```text
启动前刷新 Adapter / Actor / Assignment / Provider / Run
  → Task 必须为 todo/in_progress，Adapter enabled/healthy/execution_ready
  → 真实 active agent Actor 必须关联该 Adapter，否则禁用并引导设置
  → 无 assignee：明示将先分派，把真实 agent Actor 与当前 task_version 作为 auto_assign 随创建 Run 一次提交
  → 已有其他 assignee：提示先处理责任分派，不静默改派
  → owner 选择 ready/healthy 的 openai_chat 模型；可显式选择当前 Task Artifact / 当前 Project Attachment 和 text/file/files 输出
  → 文件输入另行确认 local/remote 离机边界，并绑定 Provider version/config_version/kind；执行确认不能替代文件确认
  → Sidecar 在同一创建事务内复用 Assignment 命令（仅空 assignee），再重读并精确比较文件授权身份；漂移 409 会连同 Task version、Assignment、分派/排队事件与 Run 一起回滚，且不 launch
  → 原生 Idempotency-Key 以 canonical Task UUID 为资源作用域；同 Task 重放/冲突，不跨 Task 返回其他 Run
  → 身份一致后再校验 Assignment/Adapter/Provider；无文件+text 规范化为 v1，受控输入或单 file 输出冻结为 v2，2–4 files 输出冻结为 v3
  → prepare 读取并校验受控文件，prelaunch 再核对引用/归属/大小/SHA-256，经一次性匿名管道执行
  → Run 得到有界结果后核对冻结 Task / Assignment / Actor / Provider / 文件引用 / 输出契约与当前验收事实
  → 满足 manual-review 条件：同事务建立一个 Submission + text/file Artifact（v3 可含 2–4 个 file Artifact），Run → succeeded+submitted，Task → waiting_review
  → 领域事实漂移：同事务保留有界结果，Run → succeeded+retained + 稳定原因码
  → 瞬时登记故障：先尝试把 Run/staging 持久化为 running+pending；成功落库后等待启动或人工恢复
  → owner accept 或 request_changes；任何 Run 交付状态都不等于 Task done
```

执行器使用 4-byte 长度前缀 JSON 匿名管道，wire `protocol_version` 保持 `opc-agent-pipe-v1`。OpenAI 无返工/无文件且文本输出用 execution contract v1；受控输入或单 file 用 v2；2–4 个冻结 files 用 v3；显式返工用 v4。H5-F5 的远程 Anthropic text/file/files 及可选返工统一用 v5，显式冻结协议和 8192 token 预算，旧编码不变。所有协议输入最多 4 个、单个 64 KiB、合计 128 KiB，只接受无 NUL 的 UTF-8；多文件模型只能按冻结顺序返回 JSON 字符串正文数组，输出合计仍最多 64 KiB。候选仅为当前 Task 未删 file Artifact 与 Task 当前 Project 的未删 Attachment。Provider 身份或协议变化及服务端身份漂移 409 清除旧文件确认，不继承旧同意。受控文件快照不含路径或正文，实际字节只经 prepare/prelaunch 复核后送管道；可选返工正文按独立确认和上限冻结。不提供任意 filesystem/Shell/browser/Git/terminal 能力。在线 Provider 会接收本次已确认资料，不是全程离线；凭据只由 Sidecar 从 keyring 传入子进程内存，不开放 WebView Token、数据库或 Shell。

结果仍受 64 KiB 聚合上限约束。文本输出复用既有 text Artifact；文件输出由服务端冻结名称/MIME（AI v3 固定同 attempt 的 Markdown/JSON），并复用共享领域提交命令创建 file Artifact。v3 在一次 Submission 中按冻结顺序创建多个 Artifact，首个 ID 仅为 Run 的兼容指针，事件另保留全部 `artifact_ids`。所有路径均使用 `origin=manual`、submitter/producer 为冻结 agent、recorder/event actor 为 system，并进入相同 `pending_review` 人工验收链。schema 075 的 `result_text` 对文件输出仍保留有界恢复副本；v3 使用严格 JSON 数组并在恢复时重验，权威交付是关联的 Submission/Artifact，不是可独立编辑的第二份文件。

终态重试创建带 `parent_run_id` 的新 Run，沿用原 v1/v2 输入快照、冻结文件引用、输出 contract 和模型选择；界面同样预检环境、Task 状态及现有 Assignment，但不为重试补分派，也不悄然替换已经漂移的文件。取消已有 Run 不受初始化未就绪阻断，但已经完成模型执行的 `running + pending` 只能恢复登记，不能通过取消丢弃 staging；停用 Adapter 不等于取消已有 Run。Sidecar 启动先重试 pending 交付；随后在同一事务中将遗留 `running + not_ready` 标为 interrupted 并收集 `queued + not_ready`，等 Router 其余初始化完成后才重新 launch 收集到的 queued Run。业务导出包含 Adapter 元数据，并在导入平台重新门控就绪状态；Run 本体不进入便携导出。

任务详情的“查看执行过程”和启动成功后的自动打开均指向右侧抽屉。它复用现有 Run 列表数据，仅有 queued/running Run 时每 2 秒轮询，可切换历史 Run，展示状态、时间、模型、错误码与最终文本，并提供打开任务和下载。抽屉是现有运行事实的展示，不新增执行控制、协议或数据表；关闭不取消 Run、不清除历史，也不改变 Task、Assignment 或验收状态。它不展示模型内部思考、Token 流、工具调用或伪进度；Run succeeded 仍不代表任务完成或验收成功。

H5-C 的受控文件输入和单文件/有界多文件输出、H5-D 会话计划、H5-E1/E2/E3/E5–E10 续办、H5-G52/G54/G58 的 Run/续办/适配器异常投影及 H5-E4 人工点击的严格工作区工具入口已经交付；后续仍包括其余尚未建模的失败/恢复来源、任意工具自动操作和自治多步编排，macOS/Linux 生命周期矩阵与 external 执行也仍待。当前执行器已把完成条件拼入提示词，运行中取消稳定落为 `cancelled`，超限或非法结果按稳定错误整体失败而不截断。删除门扫描全部 queued/running Run：v1 没有文件引用，v2/v3 需严格验证规范 JSON 与全部文件/输出语义；v2/v3 引用阻止对应 Task Artifact/Project Attachment 软删除及含引用 Attachment 的 Project 永久删除，未知 contract version 或非法快照均 fail closed。真实 Provider、崩溃/断电恢复、网络故障与原生桌面仍需专项验收，见 [本地 Agent 模块](modules/local-agents.md)。

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

该读操作不冻结业务写入；后台扫描在恢复维护锁下运行，pending restore 时跳过。探测失败只写固定脱敏日志，不创建错误的低空间事项。Inbox/journal 只保存固定来源和提示，不保存根路径、盘符、总量或剩余字节。阈值默认 1 GiB，可在“数据与备份”以 1–100 GiB 预览并保存，下一轮扫描生效。Windows 以卷 GUID、Unix 以设备号在单次检查进程内分组，同卷三个逻辑位置只探测一次。后台扫描与显式 `POST /diagnostics/storage/check` 为每个物理组按逻辑位置组合写入 15 分钟桶，保留 30 天；失败探测不造样本，写历史失败不阻断低空间提醒。`GET /diagnostics/storage/history?days=1..30` 只返回逻辑 scope、时间、容量、阈值和状态，不返回卷 ID、路径或盘符；设置页默认读取 7 天趋势。

### 6.12 显式本地模型质量评测

```text
owner 在设置中选择 ready/healthy local Provider 并点击运行
  → Sidecar 重验 Provider ID/version/local loopback 边界
  → 幂等创建 ai_evaluation_run(queued)
  → 用户显式选择 8-case smoke、四类之一的 6-case topic 或 24-case full；单邮箱 Actor 顺序执行
  → 生产系统提示与真实 Harness 执行；回答只在内存中经 production citation parser + dataset v4 分层 scorer
  → 每个 case 只保存 passed/failed/error、failure codes、citation 数和无正文指标
  → 全部执行完成：Run succeeded；质量结果仍按 case 独立展示
  → 用户可取消活动 Run；重启遗留态标 interrupted；终态可二次确认删除
  → summary 在同一只读快照分开状态计数与 succeeded-only 质量
  → 按 Provider ID/config_version/dataset version/suite 分组，并正序显示最近完整 Run；名称/模型只作快照说明
  → 同组再从无正文 Result 分解四类 category，类别三项合计必须与父组一致
  → failed Result 的稳定 code 展开为 affected cases + occurrences，不返回 failure detail
  → 总体/category 即时计算 95% Wilson 区间；1/2/3+ 次完整 Run 标记重复程度
  → 仅 full：当前 dataset + 已知配置身份 + 3 Run + 下界/严重 code 生成只读人工评审候选
  → owner 选择人工决定并填写理由；写事务重新聚合相同组，证据变化则拒绝旧快照
  → 追加 ai_evaluation_review；不可编辑/删除，只读历史显示决定、理由、证据与 Actor
```

评测不读取用户会话、长期记忆、业务对象或用户知识库，也不调用远程 Provider。Run succeeded 只表示执行结束，不等于质量通过；failed/cancelled/active 只进入状态计数。四个 6-case 专题与 8-case smoke 只供诊断，24-case full 才参与候选；case 可命中多个 failure code，各 code 的 affected 不能相加冒充唯一失败数。dataset v4 的结构校验、词语匹配、数据驱动的有限事实规则（含否定/冲突与 `FACT_CONTRADICTED`）和人工判断分层，不构成通用语义证明；回答级 validated 也只证明引用身份在 allowlist 内。

Provider `version` 仍是 HTTP ETag；配置身份 `config_version` 只在类型、协议、端点、模型或实际凭据变化时递增，健康检查不拆分证据。准备/提交阶段短锁并复验配置，模型网络等待不持有 Provider 或全局维护锁。Run/Review 保存 nullable `provider_config_version`，旧记录保持 NULL 与原审计，不冒充当前配置或阻止新配置积累独立证据。Wilson 区间和人工评审候选只是固定数据集观察，不是发布许可。人工决定只追加，不启停 Provider、不阻止聊天、不创建 Inbox/Task；已有 Run 保护 Provider 删除，独立审计则保留历史解释。自动化使用隔离 mock/回环夹具，未宣称真实模型质量已经验证。

### 6.13 AI 运行恢复与人工确认

H2-C5 把 Inbox 全局未读快照元数据接入 `workspace_search`，把 `inbox.read_all` 接入保存会话 work+actions 的人工提议。当前页和筛选不缩小批量范围；服务端最多绑定 1000 个候选的 `ID+version` SHA-256 指纹。确认事务重建完整预览，变化拒绝旧卡，再复用原生批量领域事务；快照后新增或变化的事项仍被 cutoff 保守排除。批量结果以 Proposal ID/version 1 作为无单一实体的回执身份，专用结果与后续回执携带实际 cutoff/count 并导航 `/inbox`。API v1/schema 73 不变，不新增业务副本、执行工具或跨轮权限。见 [Inbox 模块](modules/inbox.md#ai-独立轨道快照式全部已读adr-029-h2-c5)。

H2-F 把 Project Note 列表与版本固定的分页正文接入单次 work 授权，把创建、修改、带原因软删除接入保存会话 work+actions 的人工提议。原生 API 与审批共用 project_note_commands.go，Note 与 Project 版本 trigger、审批结果/事件同事务，不伪造 Project 生命周期事件；完整正文与项目身份/状态/版本变化拒绝旧预览，删除另需人工同意。主/侧聊天取消旧 Project 查询后刷新，回执不携带正文或继承权限。H4-G 进一步把工具结果、已有笔记卡与实际回执接到 `/projects/:projectId?note=:noteId`：项目页通过既有单条详情 API 独立读取并核对 Project+Note，不依赖当前列表页或筛选；删除项只返回删除元数据，正文为空。来源会话仅由显示消息的 UI 追加，查看与返回不执行动作、不调用模型或恢复权限；未确认创建因尚无 Note ID 仍只打开所属项目。没有新增 API、schema 或可写副本，附件、财务与任意路径权限也不由此开放。见 [Project 模块](modules/projects.md#ai-项目笔记桥接h2-f)。

H2-D 把 Task 当前/历史 Assignment 读模型接入单次 work 授权，把首次分派、改派与结束责任接入 work+actions 的人工提议审批。Task/Actor/Assignment 仍为原模块事实；原生 API 和审批共用 assignment_commands.go，不复制责任状态。Task 版本、Actor 身份/版本、活动责任与子任务/验收预览在确认事务重验；责任、Task 版本、父任务 reconciliation、领域事件和审批同时提交或回滚。确认结果指向实际 Task，主/侧对话刷新共享聚合缓存。分派本身不会启动 Agent、发送消息或授予工具权限；独立执行链由 H5-A 接续，受确认的提交与验收由 H2-E2 接续。

H5-A 增加只用于启动受控文本 Run 的 `agent_execution` 单次权限。它只允许保存会话，并依赖 `work+outputs+actions`；普通工作台操作权限、右侧文件/Git/终端/浏览器内容、上一轮权限或模型文本都不能隐式升级为执行权。Registry 仅开放安全的 `workspace_agent_execution` 事实查询和 `agent_run.start` 提议，不向模型暴露 endpoint、凭据、Adapter manifest、任意命令、路径或既有结果正文。模型只能指定真实 Task、Task 版本及 Provider/配置版本，不能指定 Actor、Assignment、Adapter、attempt、parent Run、模型、endpoint、credential 或人工同意字段。

Sidecar 使用与原生入口共用的 prepare/create 命令构造完整本地预览，冻结 Task/完成条件、Assignment、Actor、Adapter、Provider/config 与执行契约身份。确认必须额外提交 `confirm_agent_execution=true`；事务内再次准备并逐字段比较旧预览，任何身份、版本、健康、状态或配置漂移均拒绝旧卡。Proposal 决定、Run、审计事件在同一事务提交，只有提交成功后才启动执行器；重复确认只回放原 Run，不重复启动。schema 074 为 Run 保存冻结版本并以每 Task 单一活动 Run、`(task, actor, attempt)` 唯一索引兜底并发；执行前 claim 与进程启动前再做身份漂移门控。Run 成功只代表有界结果已被安全处置，验收仍走 Task 原有策略。回执使用实际 `/tasks/:taskId?agent_run=:runId`，任务页按双重身份读取精确 Run；来源会话由 UI 追加，不恢复任何权限。

H5-B 在 schema 075 把 Run 结果处置变成持久状态机：成功 finalize 必须同一事务得到 `succeeded + submitted` 或 `succeeded + retained`，绝不公开 succeeded+无 disposition。提交路径复用 Task output 领域命令，冻结 agent 同时是 Submission submitter 与 Artifact producer，system 是 recorder/event actor；Task 仍只到 waiting_review。版本、责任、Actor、review/status 漂移确定性 retained，数据库/事件等瞬时错误则回滚并先尝试持久化 `running + pending + AGENT_OUTPUT_DELIVERY_PENDING`。若完成事务与首次 pending 写入都暂时失败，Worker 在进程存活期间以内存保留有界正文并退避重试；首次 staging 落库后才可跨重启恢复，落库前的进程退出/掉电仍有残余窗口。人工恢复 endpoint、启动恢复、条件更新和唯一索引共同保证已持久化状态的幂等；启动先恢复 pending，随后在同一事务中中断无 staging 的旧 `running + not_ready` 并收集 `queued + not_ready`，Router 初始化末尾才重新 launch 收集到的 queued Run。`queued/running` Run 阻止 Task 硬删除，避免活动子进程或 pending staging 被级联抹除；全为终态后聚合删除可清理 Run。若已确认 `agent_run.start` 的 Run 因 Task 删除而消失，Proposal 决定及 `result_id/result_version` 仍作为历史回执，Sidecar 省略实时 `agent_run_result`；Web 只把这一精确形态解析为 `route=""` 的不可导航 tombstone，显示执行记录已删除，不发起 Run 查询/轮询，也不把它当作当前成功证据。AI 回执只公开安全状态、原因码和精确 ID；全局 Run 列表也只返回 metadata-only 摘要，不含结果正文、输入快照或 staging，用户点击后才按精确 ID 获取详情正文与动作。submitted 指向精确 Submission，未扩大正文读取。历史 succeeded 仅凭不可变事件与关系的唯一证据回填提交身份；超限/非法结果若已有该唯一提交链，则保留 succeeded+submitted、以有界 Run 说明和完整权威 Artifact 表达，未关联非法行才失败，其余无法证明的合法历史为 legacy retained。业务导入因 075 只改被排除的 agent_runs，兼容图显式增加 74→75。

H5-C 在同一授权模型中新增只供保存会话使用的 `agent_files` 单消息 scope，并要求同时具备 `work+outputs+actions+agent_execution`。既有 `workspace_agent_execution` 在该 scope 下只列当前 Task 活动 file Artifact 与该 Task 当前 Project 活动 Attachment，并为当前 generation+Task 建立稳定 opaque candidate ID；模型只看到 candidate ID、来源类别、显示名、MIME、大小和创建时间，不能得到或提交真实文件 ID、路径、SHA-256、正文或 consent。candidate 不能跨 generation、Task 或未保存会话复用。模型若显式给 `output_kind=text` 且没有输入（包括空候选数组），后端规范化掉 no-op 字段，继续生成 v1 预览；有受控输入或单 file 输出使用 v2，2–4 个 files 输出使用 v3。

AI 预览在本地把 opaque candidate 解析为真实受控引用，并与 Task、Assignment、Actor、Adapter、Provider/config、output contract 一起冻结。执行同意 `confirm_agent_execution` 与文件同意 `confirm_agent_files` 是两项独立确认；local Provider 显示不离机，remote Provider 明确披露所选字节会离开设备。确认事务重算完整预览并比较，Task/Project/Assignment/Provider 或候选事实变化拒绝旧卡；提交成功后 prepare/prelaunch 仍按大小/SHA-256/归属复核。旧 v1 提议缺少 `leaves_device` 时按其冻结 Provider 类型兼容重建，不能因版本升级把既有 pending 卡永久锁死，也不能据此绕过新 v2 文件披露。重复确认只重放同一 Run，不重复启动。

AI file 输出不允许模型决定本机路径或最终名称；服务端按 attempt 生成安全名称/MIME，经 H5-B 的 Submission/file Artifact 人工复核链登记。v3 固定 2–4 个文件，模型仅按冻结顺序返回 JSON 字符串正文数组，服务端一次创建一个 Submission 和多个 Artifact；Run 的 `artifact_id` 是首个兼容指针，事件记录完整 `artifact_ids`。receipt 只披露 Run/Task/Submission/Artifact 身份、状态和安全原因码，不回传输入正文、路径、哈希或输出正文。schema 075 的有界 `result_text` 副本继续只服务登记恢复，v3 载荷恢复时重新校验；Run succeeded 不等于 Task done。queued/running 引用阻止输入 Artifact/Attachment 与包含该 Attachment 的 Project 删除，严格 v2/v3 解析失败时 fail closed；retry 沿用冻结引用和 contract。右侧工作区的文件预览、Chromium、Git、终端和侧边工具内容不因此进入 Harness；任意 filesystem/Shell/browser/Git/terminal 自动访问仍不支持。真实 Provider、桌面与断电恢复另行验收。

H2-E1 在同一 outputs 授权边界接入 TaskSubmission 读模型：批次列表、指定 summary/review_reason 的 SQL 字符分页及双重身份定位的 Artifact 元数据，连同同事务 Task 版本和当前指针返回。查询不写 Submission、Task 或事件，不启动 Run，也不自动接受；历史/当前状态、仅摘要/child_rollup 和已删除元数据独立表述。正文可能含敏感信息，文件无字节/路径，不能凭元数据宣称审查过文件。具体契约见 [Task 模块](modules/tasks.md#ai-提交批次读取h2-e1源码与隔离测试已实现)。

H2-E2 在保存会话的 work+outputs+actions 授权下接入 task.submit_output/review 提议与逐项人工确认。TaskSubmission/Artifact 仍为领域事实，原生 HTTP 与审批共用 task_output_commands.go；提交归属于当前负责人、owner 代录，验收绑定精确当前批次。完整正文/责任/版本预览与额外 confirm_task_output 由人核验，变化拒绝旧卡，文件只有元数据，过大证据整体拒绝。业务、审批、事件、责任结束及父链/Inbox 协调同事务；原生文件生命周期不交给 AI。主/侧对话在决定后取消旧请求，刷新任务聚合及单个产出详情；历史回执不带正文、不继承权限，结果 ID 是 Submission、版本是 Task，通过 Task+Submission 真实身份链接具体批次。H4-F 新只读接口在同一事务返回当前 Task 版本/指针与所选批次，历史链接不改为最新批次；主/侧界面附加来源会话，页面按需读取正文、人工下载并支持返回，不将内容自动交给模型。缺失/错配/旧缓存拒绝替代目标，离页取消文件请求；查看/下载不提交或验收，不能把提交当验收或执行成功。详细契约与验证见 [Task 模块](modules/tasks.md#ai-提交与验收命令h2-e2源码与隔离测试已实现)。

H3-B1–B3 的客户活动/回访使用单次 `clients` 授权，按字段白名单从业务表只读分页；活动和回访互不冒充，读取不投影新事实。回访创建/编辑/取消/跳过/完成/重排另需 `work+actions`，人工决定与原生 API 共用 `client_followup_commands.go`：同一事务校验 Client/Actor、Followup planned/version 与稳定身份预览，更新计划、归档旧到期 Inbox、追加领域事件和审批结果；完成+续排或重排的新计划同事务提交，失败整体回滚，已确认重放不再次执行。完成需另行人工勾选核实实际结果与时间。客户端取消旧 Client/Followup/Inbox/Actor/搜索请求并刷新事实；复合回执 target_id 指旧计划，result_id/result_version 指新计划，客户路由携带实际结果记录 ID，定位最新事实但不继承读取权限。回访计划不代表实际联系客户，不自动生成 ClientActivity；H3-B3 的人工备注/会议创建、修订与软删除由 client_activity_commands.go 共享原生事务，确认重验活动版本、客户身份和完整正文；删除另需人工勾选，保留历史及附件，不可写系统来源或改作者。Client 聚合版本与审批同事务；前端取消旧 Client/搜索查询后刷新详情、时间线、最近动态及角标。schema 073 只扩容审批 JSON，旧载荷和决定原样保留；精确记录定位与原对话往返已由 H4-D 接通。详见 [ADR-029](adr/029-agent-workspace-capabilities.md)。

H2-I 的客户主档 create/update 同样要求单次 `clients`、`work` 与 `actions`。Client 读取白名单包含名称、状态和最多 2000 Unicode 字符的备注，不含 contact_name/email/phone；联系字段只能来自用户明确输入，备注可来自用户输入或本次已授权且 `truncated_fields` 不含 `notes` 的完整客户快照。规范化后的完整前后值保存在本地审批预览。确认事务重建 Client 预览并校验版本，随后调用与原生 API 共用的创建/更新 helper；Client 写入、审批决定及无正文事件同事务提交。结果刷新 Client/Project/Invoice/Financial/Search 查询并导航真实客户。删除、附件、Actor 联系人关系、Project 解绑和外部通信仍由原生人工流程独占。

H2-J 在同一权限组合下扩展 Client 安全读取，返回当前 active contact 的 link ID 与 person ID/名称/类型/状态/版本；不读取人员备注、metadata 或联系方式。`client_contact.link` 只接受现有 active person 的真实 actor_id，`client_contact.unlink` 绑定当前 link ID 并要求原因。Proposal 冻结 Client/person 身份及关系状态；确认事务重验 Client version、人员可用性和逐字预览，再复用 `client_actor_links.go` 的原生共享 helper。关系写入、审批决定与事件同事务；解除保留 person 与关系历史。结果导航 Client 并刷新 Client/Actor/Search；H2-J 关系动作本身不隐式创建/删除人员、不建立第二 active contact，也不外联。

H2-K 独立开放 person 档案，不扩大 Client 关系动作：`workspace_search/get(person)` 只返回名称、状态、版本；`person.create/update` 需保存会话 `work+actions`，创建固定 active，更新只接受名称、状态及本轮明示备注。确认重建并比对预览后调用 `actors.go` 共享事务 helper，Actor 事件与审批同事务。停用时原生 Assignment/Client link/Followup 门禁仍生效；结果不生成不存在的人员详情 URL，前端改为设置入口并刷新所有投影。新联系人要在下一次读取取得已创建 person 的真实 ID 后另行提议关系，不能由模型把两笔审批合并。

生成由 React 应用级内存状态持有，SPA 切页后继续执行并保留全局停止入口。`POST /api/v1/ai/chat` 的稳定 `Idempotency-Key` 与原始输入摘要绑定；已接受的同键重试返回 `AI_CHAT_ALREADY_ACCEPTED` 及 generation/session ID，不另建消息。`GET /ai/active-generations`、`GET /ai/generations/:id` 和 `GET /ai/generations/by-request/:key` 回读真实状态，`POST /ai/generations/:id/cancel` 停止运行（以上相对路径均位于 `/api/v1`）。浏览器仅持久化恢复标识；刷新/断连终止旧 SSE，上游取消与落库竞态通过回读收敛，不承诺恢复模型计算；Sidecar 重启使用 interrupted/cancelled 恢复语义。草稿只在应用内存保留，接受前失败不覆盖后来输入，硬刷新不保存草稿。

任务路径是“模型建议 → 用户编辑并确认 → `POST /ai/messages/:id/task-confirmation` → 共享 Task 领域事务创建并原子关联消息”。消息 ID 是稳定确认身份，同载荷重放返回 `{data:{task,message}}`，改载荷冲突，已删任务返回 410、不重建。Task 拥有字段校验、父级协调和生命周期事实；普通创建没有 `task_created` 事件，不新增虚构事件。UI 只有读取真实 Task/Message 关联才显示成功，旧 `/ai/messages/:id/task` 仅保留挂接兼容。模型没有业务写工具。

记忆卡以 `GET /ai/memory-proposals/:id` 回读 pending/confirmed/rejected 与 memory_id；旧无 proposal_id 卡使用 `GET /ai/messages/:id/memory-decision` 从消息派生，GET 不落库。对应 DELETE 才持久忽略；确认使用既有 `POST /ai/memories` 并校验会话/内容，重复确认不重建已删除记忆。确认与忽略是人工决定，不是仅隐藏卡片。

`GET /ai/sessions/:id/compaction` 返回 idle/running/pending/succeeded/failed/cancelled 与部分消息标记。摘要逐段推进，`source_message_offset` 表示剥离控制块后的 UTF-8 字节水位线；正 offset 的部分消息不计入已压缩消息数。快照和水位线同事务，失败/取消保留上次成功进度。`persist=false` 的正文、事实、工具提议和摘要只存在当前运行内存，永久记忆另需人工明确确认。

模型各轮、工具结果、自检修订共享 10 分钟/累计 1 MiB，另有限制 8 轮、32 次工具、每工具 30 秒/64 KiB。流必须有明确终态；OpenAI stop 后继续读 usage，无终态 EOF/截断/过滤不算成功。非法或缺失 selfcheck 为 unavailable，修订失败保留部分回答。锁、隐私、迁移及验证边界详见 [ADR-025](adr/025-ai-reliability-confirmations-and-evaluation-identity.md)。

### 6.14 知识引用的人工作业往返（H3-D1）

AI validated citation → 服务器身份/版本生成的知识库地址 → `GET /knowledge/chunks/:id` → 同版本单片段纯文本预览 → 返回原会话。API 与 AI6 共享 `readKnowledgeChunk` 精确 join，知识库继续拥有当前文档/chunk，会话拥有历史引用与手选外发快照；两者不能相互覆盖。版本变化返回冲突，删除/重建导致旧 chunk 不存在则不可核验，不悄然切换新证据。PDF 带真实页码，旧文本快照只在解码时补 1/1 页。这个人工读操作无需 Provider、不进入 Harness、不额外外发、不新增业务事实。

H3-D2 增加独立来源范围 grant → 写前/事务内版本校验 → knowledge_search 共享 FTS → knowledge_read 共享单片段 join → Executor 成功后的私有 AcceptResult → 运行级 allowlist → 最终 citation 验证。搜索摘要、失败、超时迟到、截断或超预算结果不能进入证据；来源授权不隐含业务/文件/终端权限。动态正文仅在运行工具历史，不另建手选 context_snapshot；持久会话保存引用元数据，模型仍可能引用正文到回答/记忆。无 schema 变化；H4 已补齐非持久引用卡：终态验证结果经 SSE done 或 generation GET 回到共享聊天 store，主/侧对话共用来源核验组件。临时引用只进 Sidecar 有界内存（32 项、每项 ≤16 KiB、15 分钟有效，读写时淘汰）及窗口内存（纳入 20 回合/8 MiB）；删除会话、恢复与进程退出清除后端缓存。元数据缺失不等于 not_requested，部分正文保持 incomplete；真实模型/桌面验收仍待，见 ADR-011/029。

### 6.15 客户子记录的人工作业往返（H4-D）

模型工具及审批回执只生成实际 Client/Activity/Followup ID 的允许列表地址，Web 使用原鉴权详情接口核对客户归属后展示并复用原手工表单；UI 追加原消息会话身份供返回，不重新授权模型。直接读不依赖分页，不复制记录；缓存遵从原 Client 聚合失效。Today/Inbox/右侧客户动态共享此入口，错误不代用默认记录。详见 [客户定位契约](modules/clients.md#已实现h4-d-活动与回访精确定位)。

### 6.16 自动化规则与运行的人工作业往返（H4-E）

`workspace_automations` / 审批卡 / 无正文回执 → 真实规则或 Run ID 的 `/settings/automation` 地址 → 既有规则读模型或不可变 Run 详情 → 原表单/尝试链 → 返回原会话。重试确认的结果地址优先采用实际新 Run；页面读取与模型白名单读取分离，人工快照不进入工具结果。严格路由校验、请求取消与身份校验防止错配，过期或读取失败不展示旧正文，不自动选择其他规则；URL 支持前后导航，模式切换保留查询参数。没有新事实副本、模型授权或执行捷径，人工操作继续复用既有领域事务。详见 [自动化模块](modules/automation.md#规则与运行精确定位h4-e已实现源码与确定性测试)。

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
- v0.2 当前：Project complete/reopen 已追加 Workflow Event；自动化消费者已订阅 `project_completed`、`invoice_overdue` 与 `agent_run_failed`，所有预设均须人工启用；Automation Run 自身事件不进入当前消费者。
- v0.3：内容审核/发布时间与路线图里程碑到期/达成已投影为本地 Inbox 事件；路线图季度内精确日期和跨年度移动已交付，原生通知仍待。
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
- Adapter 只获得本次 Run 明确授权的 Task 文本、模型请求配置及可选受控文件字节；协议不提供真实文件路径、通用输出目录、数据库、WebView Token 或任意工具句柄。
- 跨平台进程沙箱和网络阻断必须经过 ADR 与实际验证；无法强制时 Adapter 只能保留为禁用诊断记录，正式 Agent 分派与执行入口不得启用。
- person 无账号、无登录、无远程通知；由 owner 记录线下进度和结果。
- 普通人工或 AI 代录产出由 Sidecar 从 active assignee 派生 Artifact producer，manual Submission submitter 与 Artifact recorder 固定 owner；受信 Agent Run 交付则重验并使用冻结 agent 作为 producer/submitter、内置 system 作为 recorder/Event actor。零 Artifact 的 child_rollup Submission submitter 固定内置 system。所有身份均由服务端派生，客户端不能上传 Actor ID 冒充。
- 文件 Artifact 只允许上传到带数据库绑定 JSON marker 且由 Sidecar 进程独占锁定的受控 root，数据库仅保存 `objects/<artifact-id>` 相对路径；multipart 的 manifest 必须是首个 part，之后只接受被它精确且唯一引用的文件 part。严格 JSON body、manifest 与单个 structured object 各限 1 MiB，单文件限 50 MiB、完整 multipart 限 100 MiB，服务端 HTTP read/write timeout 为 180 秒、客户端上传/下载端到端超时 120 秒。下载通过鉴权 API 重新校验大小和 SHA-256，并强制 attachment/nosniff/no-store；关键文件与目录项在成功前做耐久同步。
- Artifact 软删除需要确认、Task `If-Match` 和原因；pending-review 批次禁止删除，queued/running v2 Run 引用的 Task Artifact/Project Attachment 也禁止删除。Task 聚合硬删除和文件软删除都通过 `.trash/` 做数据库事务补偿，并在同一事务留下不可变 tombstone；物理文件已经缺失时仍允许删除，软删记录 missing 完整性事实，但不能越过活动 Run 引用保护。
- H5-C 文件授权与执行授权分离：原生入口使用 `confirm_file_access`，并把文件确认绑定 Provider `version/config_version/kind`；三项身份漂移以 409 在 Run/排队事件创建前整笔拒绝，前端同时清除旧确认。AI 入口使用 `confirm_agent_files` 与 `confirm_agent_execution`。local Provider 显示不离机，remote Provider 显示字节会离开设备。模型只能使用 generation+Task 绑定的 opaque candidate，不能提供真实 ID、路径、哈希、正文或 consent；持久快照也不保存路径/正文。文件输出名称/MIME 由服务端冻结并进入人工复核 Artifact 链。
- AI 助手是明确授权的例外（ADR-004–011、025、029）：远程请求可外发系统提示、长期记忆、会话摘要/事实、最近回合、当前输入、memory_search 命中、手选业务快照、最多 3 个手选知识 chunk，以及本次明确授权 scope 的工作台查询结果。无授权仍只有记忆三工具；新增业务读取不能使用 Shell、任意文件、网络或知识库检索。手选上下文继续在 AI 写入前重验版本；工具授权绑定 Provider version，执行时复核 config_version/ready/恢复状态，模型不能给自身授权。业务事实由原模块拥有；读取工具不创建副本，workspace_propose 仅起草 Task/Project 创建、编辑与生命周期命令、Inbox 受理分诊与 Reminder 计划命令，用户逐项确认后经共享领域事务执行；Project 客户关联需 clients 范围，完成且有未完成任务时需人工额外勾选，Task 状态不联动修改。Project 完成 Inbox、客户活动及已启用自动化沿用原领域事务与投递，财务字段仍禁用。Inbox create/update/read/snooze/unsnooze/resolve/dismiss/reopen 复用人工命令与只读预校验，确认重验版本、未来时间、来源截止规则和必需任务，不改变 Task/Reminder；详情读取策略/日期/实时进度，不外发来源 payload。新增 workspace_inbox_tasks 在 work 权限下分页读取当前/历史关系（不含 Actor/解除原因），inbox.link_task/set_required/unlink_task 通过共享关系事务执行，绑定 Inbox/Task 双版本；确认重算预览并比对，其他任务进度变化返回 AI_ACTION_PREVIEW_CHANGED，不能悄然改变自动结清效果。关系调整保留软解除历史、不改变 Task/Assignment，仍由既有 reconciliation 决定事项结清。inbox.split 则经整批人工确认创建 1–20 项 Task/父子关系/标签/初始 Assignment，work 的 workspace_task_options 只读取真实可分派 Actor/Tag 基本身份；manual reviewer 固定 owner，person 无外部消息，agent 不自动执行。人工与审批共用 inbox_split_commands.go；预览包含全部草稿与关联名称/版本，确认重算并比对，资料变化不执行旧提议，业务、审计、审批决定同事务回滚。split 决定另刷新 Task/Today/Project 等聚合。决定后取消在途 Project 查询再刷新 follow-up，并刷新 Inbox/搜索，/inbox/:id 与无正文回执贯通。Reminder 在 work 下可分页查询时间/重复/系列白名单，work+actions 的 create/update/cancel 经 reminder_commands.go 与人工 API 共用事务，重验当前 occurrence 版本、scheduled 与未来时间/IANA/锚点，领域事件和审批一起回滚；取消不删历史，沿用到期 Inbox 和下一 occurrence 投影。确认卡/回执链接 /inbox?reminder=UUID，决定后刷新 Reminder/Inbox/搜索，无新通知或后台唤醒能力。inbox.force_resolve 仅起草自动策略事项的例外，decision 必须额外携带人工 confirm_force_resolve:true；确认重验 Inbox 版本及完整进度预览，Task 进度变化即使没有版本变化也拒绝旧提议。人工与审批共用 inbox_force_resolve_commands.go，owner/forced 事件与决定同事务，任务、验收、required 关系与来源不改变。schema 072 的 ai_action_proposals 明确保存载荷/前后预览/决定/结果，版本与摘要绑定，不接受模型自行确认。只读工具原始参数/结果不另行持久化；模型回答与会话事实遵守 persist。`persist=false` 禁止操作建议权限，且不把正文、事实、提议或摘要写入数据库、日志、事件、幂等缓存或前端 storage。授权审计只记 generation/provider/config/scopes，工具终态复用 run steps。远程 key 仍在 OS 安全存储，本地仅精确回环且拒绝跨 origin 重定向。详见 [ADR-029](adr/029-agent-workspace-capabilities.md)；更多领域命令与编排仍在实施。
- H3-F 另把知识库来源与索引管理接入同一条审批链：独立 `knowledge_actions`（仅保存会话）只注册元数据只读 `knowledge_library` 与 `workspace_propose`，不读正文、不授予 `knowledge` 检索；可提议 `knowledge_source.reindex/delete` 与 `knowledge_index_job.retry/cancel`，来源动作绑定来源版本、任务动作绑定任务当前来源版本，四项 changes 为空（删除可带可选 reason）。删除另需人工勾选并永久移除来源、索引与受管副本，重建/重试只新开 attempt，取消按剩余文档恢复 ready/failed 且不删资料。确认复用原生共享命令，新任务只在审批事务提交后入队，回执打开 `/knowledge`；知识库仍拥有来源、索引与受管副本事实，AI 只保存审批载荷、预览与结果身份。

- 当前阶段不提供线上更新、云同步或自动对外发送。
- 审批回执由 Sidecar 在持久会话下一次发送前，从本会话 `ai_action_proposals` 重建最近 16 条/8 KiB 的状态与结果标识，不读取目标业务正文或复制审批事实。回执层独立于模型摘要，并进入 Harness 工具循环/修订及实际请求预算；confirmed 是历史命令执行结果，不是实时 Task 完成状态，也不授予下一轮能力。已确认的 Agent Run 因终态 Task 硬删除而缺失时，只保留决定与结果标识并省略实时 Run，前端显示不可导航 tombstone，不再查询或链接该 Run。授权面板披露其后续外发，非持久会话不注入。同一 generation 的工具建议与旧任务块互斥，不能经旧确认路径重复写 Task。

## 10. 故障与恢复协作

| 故障                               | 责任模块                         | 对其他模块的行为                                                                                                                                                                                                                                                                                                                                                                                                                           |
| ---------------------------------- | -------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Sidecar 启动失败                   | 桌面平台                         | **已交付 v1**：桌面根闸门在 `starting/restarting/error` 时拦截业务页和设置 bootstrap，提供状态重查、打开脱敏日志目录和安全重启，不展示原始 message。内置启动失败可消耗有界重试预算；若尚未创建 child，受管应用重启无需伪造退出结果即可继续。数据库打开前的恢复/迁移固定阶段进度已显示；启动前备份选择仍待实现                                                                                                                              |
| Sidecar 运行中意外退出             | 桌面平台 + Web                   | 已启动 generation 只有真实 `Terminated` 才按 500 ms、2 s 最多重拉两次；当前代连续 Ready 30 秒重置预算。每代生成新 token 并重新请求动态 port，非 ready 清连接和全部 TanStack Query，generation 改变补偿遗漏的 `restarting`。外部模式、显式 shutdown、仅事件流关闭均不自动重拉                                                                                                                                                               |
| AI 流中断、预算耗尽或 Sidecar 重启 | AI + 应用级生成状态              | 明确失败/取消并保留可用部分回答；按 generation/request key 回读，不自动重复发送。模型等待不持全局维护锁，取消不等待维护写锁；恢复安排取消生成、压缩和评测但不持锁等待退出。禁用备份扫描不拿写锁，AI 失败不阻断核心模块或投影 Inbox                                                                                                                                                                                                         |
| Agent 启动恢复                     | 本地 Agent                       | 先幂等恢复 `running + pending` 的产出登记；随后在同一事务中把失去执行进程且无 staging 的 `running + not_ready` 改为 `interrupted`，并收集 `queued + not_ready`；Router 其余初始化完成后才 launch 收集到的 queued Run。v2 retry/恢复沿用冻结文件引用与 output contract，并在 prelaunch 复核漂移；不重跑已完成模型，也不把未知结果当成功；H3-C3 诊断只消费 failed，不把此启动中断或 pending 当失败                                           |
| 备份操作失败                       | 数据管理 + Inbox                 | 创建/校验/恢复演练/恢复安排的操作性失败分别尽力创建 `backup:create` / `backup:verify` / `backup:drill` / `backup:restore` Inbox Item，只记录固定安全字段并保持原错误响应。手动创建的 `BACKUP_SPACE_INSUFFICIENT` / `BACKUP_CAPACITY_UNAVAILABLE` 准入拒绝，以及 `BACKUP_INVALID` 等可解释结果不投影；UI 保留 note 并提示清理或刷新，启动应用失败转入下一行的 journal 补偿                                                                  |
| 数据库/Sidecar/存储故障            | 数据管理 + 桌面 + Inbox          | 数据库启动/迁移和 Sidecar 启动失败先写独立白名单 journal；运行期数据库失败及低空间先直接投影，数据库不可写时同样降级 journal。下一次健康启动在 ready 前补偿。稳定 incident ID 防模糊清理重放；原错误、路径、卷 ID、容量和敏感内容不进入 journal/Inbox。Sidecar/Tauri 壳脱敏日志、WebView→Sidecar request ID、全局启动故障恢复页 v1、数据库打开前白名单恢复进度、可配置低空间监测、物理卷同卷去重、无路径手动容量检查和 30 天本地趋势已交付 |
| 恢复等待重启 / 启动 applying       | 数据管理 + 桌面平台              | 安排阶段在维护锁内创建回滚包并发布 pending，随后普通 API 返回 `RESTORE_RESTART_REQUIRED`；桌面设置页可调用 `restart_application`。若受管 child 存在，只有 code 0 且无 signal 的真实退出才允许重启应用；内置启动失败未创建 child 时允许继续，延迟到达的干净退出确认后可再次请求。健康启动后只读诊断 API 汇总恢复结果；数据库打开前恢复、验证、收尾及迁移阶段由白名单 stdout 协议显示，不暴露恢复包身份或路径；启动前备份选择仍待实现        |
| 来源资源删除（T-11E）              | 来源模块 + Inbox                 | Task Artifact、Task 阻塞、Task 临期、Project 完成、Content Item 与 Roadmap Milestone 已实现：open/tracking 来源项阻止来源硬删除；允许删除前原子标记 `source_deleted_at`、保留快照并显示来源已删除。系统维护来源禁止 `source_deleted_at`。其他来源仍需逐项实现；它与 schema v13 的关联 Task 删除互锁相互独立                                                                                                                                |
| 关联 Task 硬删除                   | Task + Inbox + 本地 Agent        | 任一活动 Inbox 关系存在时返回 `TASK_HAS_ACTIVE_INBOX_RELATIONS`；任一 `queued/running` Run（含 pending）存在时返回 `409 TASK_HAS_ACTIVE_AGENT_RUN`，pending 先恢复登记，其他活动 Run 先取消或等待。全部 Run 终态后允许删除并级联 Run；历史 Inbox 关系保留快照。已确认 `agent_run.start` 回执保留决定/result ID/version，但省略已删除实时 Run 并显示不可导航 tombstone                                                                      |
| 并发旧写入                         | Sidecar 领域服务                 | Task/Tag 当前事实、父子/标签嵌入、Assignment、生命周期、Submission/Artifact、Project/Client 聚合和 Actor 变化都会使旧 `If-Match` 或 `expected_version` 返回 409；输出前端保留 summary、text、link、structured 与浏览器 File 草稿，Client 编辑前端保留资料草稿，刷新后要求用户再次明确提交，不用旧版本自动重试                                                                                                                              |
| 受控文件缺失/篡改                  | Task/Client/Project + 数据管理   | 保留元数据和审计，标记 missing/mismatch，拒绝下载。普通缺失文件不阻断确认软删或父聚合硬删，软删保留 missing 检查事实；但作为 queued/running v2 Run 冻结输入时仍受活动引用保护，不能借缺失/篡改绕过删除互锁，prepare/prelaunch 会拒绝执行                                                                                                                                                                                                   |
| Agent 文件引用删除                 | 本地 Agent + Task/Project        | 扫描全部 queued/running Run；v1 明确无文件引用，v2 Run 引用的 Task Artifact/Project Attachment 返回 `AGENT_RUN_FILE_REFERENCED`，含被引用 Attachment 的 Project 也不能永久删除。未知 contract version 或规范/语义非法 v2 snapshot 均 fail closed；须先取消 Run 或等待终态，不自动换用新文件                                                                                                                                                |
| 受控文件/数据库提交中断            | Task/Client/Project + 文件 store | 提交报错后查询三类数据库引用，仅删除可证明无引用的 object；模糊 COMMIT 保留给 reconcile。恢复 active trash 前校验 size/SHA，错配隔离并记 mismatch；三类 tombstone 让已授权删除与未知候选可区分，意外目录/链接不递归处理                                                                                                                                                                                                                    |
| 数据库与 Artifact root 不匹配      | 数据管理 + 受控文件 store        | marker 的 `database_id / store_id` 分别匹配 workspace 的不可变数据库 ID 与一次性绑定 store ID；错库、换 root、未知 marker 格式或版本在 ready 前拒绝启动                                                                                                                                                                                                                                                                                    |
| 第二 Sidecar 共用数据库父目录      | 桌面平台 + 数据管理              | 固定 `.opc-sidecar-run.lock` 的非阻塞 OS 独占锁在 pending restore、迁移和 DB open 前使后启动进程立即失败且不接触数据库；锁文件可保留，所有权只由 OS lock 表示。hard-hung orphan 会继续持锁并阻止新进程，但当前不会被自动识别或回收                                                                                                                                                                                                         |
| 第二 Sidecar 共用 Artifact root    | 桌面平台 + 受控文件 store        | Artifact root 进程级非阻塞独占锁使后启动进程在 ready 前失败，禁止双进程协调同一文件根；它不替代数据库父目录运行锁                                                                                                                                                                                                                                                                                                                          |
| Focus 进程中断                     | Focus + Sidecar                  | 启动把遗留 active 原子改为 recovery_pending；业务页恢复弹窗要求选择计入间隔、排除至最后 heartbeat 后继续或中断，也可显式跳至智能体处理；/ai 不阻断聊天，恢复仍须人工确认，计入间隔另需勾选。heartbeat 和活动查询不会递增业务 version                                                                                                                                                                                                       |

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
  → 本地知识库（TXT/Markdown/PDF 受控导入、Actor 异步索引/取消/retry/恢复、FTS5、行号/页码引用、重建与删除已交付；授权引用待后续）
  → AI 助手（远程/本地会话、记忆、显式业务/知识上下文、回答级 citation、无正文步骤/用量、dataset v4 配置身份评测、ADR-025 原子人工确认与运行恢复已交付；句子级证据、费用、自动路由和真实模型质量验收仍待）
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
- manual 人工产出不能绕过 waiting_review 和 owner 验收；已交付的 Agent text/file 产出复用同一 Submission/Artifact 领域命令，也只能进入 `pending_review`，Run succeeded 不等于 Task done。
- child_rollup 只统计直属非取消子任务，必须至少 1 个且全部 done，并满足 manual/active owner-or-person assignee/active builtin owner reviewer；系统不得越过 waiting_review，不得覆盖 manual 或 changes_requested，也不得用父子层级隐式改写 Inbox required。
- 子任务/门禁失效撤回 pending 系统批次；blocked 保持 blocked 并修正来源状态；accepted 父任务只在子任务条件失效时系统重开，历史保留且 Assignment 不恢复。
- Artifact 文件不能通过任意路径访问；H5-C 只允许活动 Task file Artifact/当前 Project Attachment，最多 4 个、单个 64 KiB、合计 128 KiB、UTF-8 无 NUL，并在 prepare/prelaunch 双复核。缺失、篡改、软删与硬删后历史均可解释且不会泄露路径或已删正文。
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
