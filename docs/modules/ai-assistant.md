# AI 助手模块

## 智能体提出“前置满足后再启动”的 Agent 执行（AH-07，2026-09-23）

在本条 `work+outputs+actions+agent_execution` 授权下，模型可以为 `agent_run.start` 附带前置条件：先用 `workspace_agent_runs` 读到仍在进行的前置 Run（其结果会带只读 `start_gate`，便于看出哪些后续执行正在等待），再提出 `start_after_run_id`、`start_after`（`submitted`/`accepted`）与 `start_within_hours`（1–24）。确认卡展示后续执行的完整快照与启动条件，用户勾选“同意在条件满足后由系统启动这一次执行”后才创建等待中的 Run；卡片随后显示“已确认 · 等待前置执行”，前置不满足时显示“未启动 · …”。这不是范围授权：没有被逐项确认的 Run 永远不会启动，也不会自动验收或重试。契约见 [本地 Agent AH-07](local-agents.md#跨-run-条件式延后启动ah-072026-09-23adr-030-首片) 与 [ADR-030](../adr/030-agent-run-start-gates.md)。

## 续办队列的“文件操作待核对”提醒（AH-04，2026-09-23）

续办队列新增前端本地来源“文件操作待核对”，位于“权限待核对”之后，计入左栏角标。它展示单文件写入/恢复在本窗口未观察到明确回执的操作（包括应用在操作进行中退出后重新启动的情况），点击打开右侧“文件”标签核对恢复记录，用户核对后手动移除。它不是 Sidecar 的 `GET /api/v1/ai/inbox` 来源，也不是执行回执；不含路径、文件名或内容，不提供重试。完整契约见 [桌面平台 AH-04](desktop-platform.md#单文件操作的本机结果待核对提醒ah-042026-09-23)。

## 运行详情时间线使用统一工具名（AH-06，2026-09-23）

核查对话内 Codex 式呈现：实时进度、生成后的“运行详情”时间线（模型轮次、工具调用、自检、引用校验、本地保存及字节/耗时/token）、计划卡、审批确认卡与 Agent Run 卡均基于真实服务端数据。唯一不一致是生成结束后的时间线直接显示工具内部标识（如 `工具调用 · memory_search`），而实时进度使用中文能力名，且两处各维护一份映射、缺少子智能体委派/续交办/子会话读取、右侧面板、记录定位与浏览器请求 7 个工具。现在两处共用 `lib/aiToolLabels.ts`，时间线显示“工具调用 · 查找会话记忆”；闭集之外的工具在实时进度中显示“调用工具”、在时间线中保留原标识，不编造能力描述。标签只描述工具能力，“准备……（仍需确认）/请求……（待你确认）”不代表操作已批准或执行。无 API、schema、scope 或工具行为变化；`aiToolLabels.test.ts` 与页面时间线测试覆盖，开发页实测时间线显示中文工具名。

## 用户关闭不再需要的计划（AH-05，2026-09-23）

计划修订规则要求保留所有既有步骤、只能追加或替换，此前没有任何方式放弃一个用户不再需要的计划：例如用户拒绝了计划里的操作后，计划会永久停在“待继续”，一直占用续办队列角标，“继续此计划”入口也一直出现。现在用户可以在计划卡上显式关闭**最新修订**：

- **入口与确认**：最新修订、非实时生成中，且计划处于“待继续”（没有待确认、执行中、子智能体执行中、结果待登记或已保留待处理的步骤）时，工具栏显示“关闭计划”。点击后展开内联说明并要求勾选“我已了解关闭只停止跟踪此计划，不会撤销任何已发生的操作”，再点“确认关闭计划”。
- **服务端门禁**：`POST /api/v1/ai/sessions/:id/plan/close`，请求体严格为 `{"expected_version":N,"confirm_close":true}`。同一事务内重新投影计划，只有计划 Inbox 状态为 `awaiting_continuation`、续办整轮检查结果为 `ready`（覆盖隐藏的同轮待确认/执行/待登记与活动生成）、且会话没有 `waiting/running` 后台续办时才写入；否则 `409 AI_PLAN_CLOSE_BLOCKED`。版本变化 `409 AI_PLAN_CLOSE_CHANGED`，证据缺失 `409 AI_PLAN_CLOSE_UNAVAILABLE`，缺少确认或版本越界 `422 AI_PLAN_CLOSE_INVALID`，未知字段 `400 INVALID_JSON`，会话/计划不存在 `404 AI_PLAN_NOT_FOUND`。重复确认返回首次关闭结果，不写第二条事件。
- **事实与存储**：关闭写入不可变 `workflow_events`（`aggregate_type=ai_work_plan`、`action=ai_work_plan_closed`、`current_json={session_id,version,generation_id}`），绑定精确修订版本；无新表、schema 或迁移。计划视图新增可选 `closed_at`，计划 Inbox 新增状态 `closed` 与 `meta.closed_total`（旧 Sidecar 缺省按 0 兼容），`attention` 不再包含已关闭计划。
- **不撤销、不隐藏**：关闭不拒绝审批、不取消 Run、不停止或创建续办、不修改任务或任何业务事实。已关闭修订不再“认领”它的 Proposal 与 Run：统一 Agent Inbox 的计划排除与 `plan_link=unplanned` 的 Run 账本都会跳过已关闭修订，因此计划里的失败 Run 会回到“独立执行”并计入角标。
- **续办**：续办检查对已关闭计划返回 `plan_closed`（不就绪、无阻塞组），“继续此计划”和后台续办入口隐藏；`POST .../continuation` 因不满足条件返回 `AI_CONTINUATION_NOT_ELIGIBLE`；若竞态下工作循环读到已关闭计划，以既有 `plan_changed` 停止。`workspace_plan` 读取已关闭计划时附加说明：不要继续它，只有用户在本条消息明确要求新的计划工作时才写新修订；新修订视为新意图，默认未关闭。

确定性证据：Go `TestAIWorkPlanClose*` 4 个用例覆盖生成中/待确认/执行中/已保留/活动续办拒绝、严格请求体、幂等重放、无业务副作用、`plan_closed` 续办原因、Inbox `closed` 过滤、新修订重新开放，以及失败 Run 回到计划外账本；前端 `aiWorkPlan.test.ts` 覆盖 `closed_at` 格式、`plan_closed` 与 `closed_at` 双向绑定、`closed` 不得出现在 attention 与关闭请求体，`AiWorkPlan.test.tsx` 覆盖显式勾选、服务端拒绝保持原状、四种未结状态不提供入口与已关闭展示。开发 Sidecar 实测路由返回 404（无计划）/422（缺确认），`state=closed` 返回 `closed_total`；开发库暂无计划，真实“关闭”点击需在有计划的会话中人工验收。代码证据：[关闭端点](../../services/sidecar/internal/api/ai_work_plan_close.go)、[计划投影](../../services/sidecar/internal/api/ai_work_plan.go)、[前端计划卡](../../apps/web/src/components/AiWorkPlan.tsx)。

## Agent Run 状态在对话与工作区中的统一表达（AH-02，2026-09-23）

`agent_run.start` 确认卡、续办队列的 Run 行、右侧“Agent 执行”标签与计划内 Run 交接提示改用本地 Agent 模块定义的统一生命周期（见 [AH-02 映射表](local-agents.md#统一的-agent-run-生命周期表达ah-022026-09-23)）。确认卡不再写“执行成功 · 产出已提交，待人工验收”这类把模型结束与任务交付混在一起的文案：带 Submission ID 的交付显示“已提交为任务产出”，只有回执中同一 Submission 为 `accepted` 时才显示“产出已验收”；`running + pending` 显示“结果待登记”而非“正在登记产出”；无法识别的组合显示“状态无法确认”。交接提示改为“当前为「阶段」：一句事实说明”，不会把执行结束写成任务完成。审批、权限、重试与恢复入口不变。

## Agent Inbox 左栏的单列表呈现（AH-01，2026-09-23）

续办队列原先为 14 个来源各自渲染一个 `.ai-rail-sessions` 滚动容器；智能体模式下这些容器都继承 `flex: 1; overflow-y: auto`，在 `overflow: hidden` 的左栏里被平分压成约 23px 高的小滚动框，真实待办几乎不可见，空来源也各占一行占位文案。现改为 `AiAgentInboxPanel`：

- 整个续办队列是一个 `role="region"` 的滚动区；分组本身不再滚动，只渲染有事项的分组，标题右侧显示已加载条数。
- 四个读取来源（智能体计划、统一 Agent Inbox、计划外独立执行、恢复诊断）各自只显示一次加载或失败状态，失败可单独重试并提示“列表可能不完整”。只有全部来源读取成功、没有事项且没有未加载分页时，才显示单条“没有需要你处理的事项”。
- 计划内与计划外 Run 同属 `agent_run` 来源；计划外 Run 账本读取失败时，不再把这些 Run 标成“计划内执行”，而是等账本恢复后再分组。
- 分组按需要用户决定的程度排序：等待确认、权限待核对、文件操作待核对（AH-04 本机提醒）、待验收、独立执行、计划内执行、智能体计划、后台续办、对话异常，然后是自动化、知识库、供应商、本地 Agent、数据恢复和系统维护。

行内容、导航目标、交接提示与计数角标均沿用原实现；不新增 API、schema、scope 或自动执行。确定性证据：`AiSessionRail.test.tsx` 覆盖只渲染有事项分组与计数、读取失败只报一次且不显示已清空、计划外账本不可用时不误标计划内执行；开发页面 `/ai` 实测“独立执行 3”完整可见并能打开执行抽屉。代码证据：[续办队列面板](../../apps/web/src/components/AiAgentInboxPanel.tsx)、[左栏](../../apps/web/src/components/AiSessionRail.tsx)。

## 确认但结果身份缺失时的诚实状态（H5-G57，2026-09-23）

确认卡不再把 `confirmed` 单独当作“已执行”证明：如果服务端没有返回规范 `result_id`，顶部状态显示“已确认 · 结果待回读”，且不渲染操作成功回执或“打开”成功入口。只有 `confirmed + result_id` 才能显示已执行及自然语言回执；既有 Agent、批量、导出和领域专用状态继续按各自的权威结果展示。该变化只修正前端表达，不放宽审批、权限、版本重验、事务或自动执行边界。

定向回归覆盖旧/不完整 Sidecar 返回 confirmed 但缺少结果身份的情况，确保不出现成功措辞。真实 Provider、网络或桌面故障仍需专项验收。

## 已确认工作台操作的统一自然语言回执（H5-G56，2026-09-23）

除任务、批量任务、Agent 执行和已有专用回执外，确认卡现在会把带有服务端 `result_id` 的客户、项目、收件箱、提醒、专注、内容、路线图、发票、财务等操作显示为自然语言“已创建/已更新/已取消”等回执，并保留服务端生成的精确“打开”入口。回执只在 `confirmed` 且结果身份存在时出现；待确认、拒绝、过期、失败或结果身份缺失不会显示成功措辞。动词只是前端表达，不推断执行结果，不改变审批、权限、版本重验或领域事务。

前端回归覆盖确认前不显示、确认后按动作动词和目标名称显示、精确路由以及既有专用回执不重复。真实 Provider、桌面和网络异常仍以服务端状态为准。

## 单任务操作的自然语言执行回执（H5-G55，2026-09-23）

单条 `task.create` / `task.update` 提案在用户完成人工确认、服务端返回规范 `result_id` 后，确认卡会额外显示自然语言回执：创建或更新的任务标题，以及由服务端生成的精确“打开任务”入口。待确认、拒绝、过期、执行失败或缺少结果身份时不会显示“已创建/已更新”，仍只显示提案和当前状态。回执不改变审批、版本重验、领域事务或权限边界；批量创建继续使用批量专用回执，旧消息任务确认继续以服务端绑定的真实 Message/Task 事实为准。

前端回归覆盖确认后真实 `result_id`、标题和链接，未确认提案不会提前显示成功。真实 Provider、桌面和网络异常仍按原审批回执与页面刷新规则处理。

## 后台计划续办纳入统一 Agent Inbox（H5-G54，2026-09-23）

`GET /api/v1/ai/inbox` 新增 metadata-only 的 `continuation` 来源和 `continuation_total` 计数，只返回保存会话中 `waiting`/`running` 的 `ai_continuations`：会话标题、续办状态、原因、已启动轮次/上限和过期时间。左侧 Agent Inbox 增加“后台续办”分组，点击可精确打开原会话计划；只读 handoff 仅带固定提示回到 AI，要求重新读取原计划与事实。

严格排除 workspace grant、计划正文、Provider 配置、工作区 JSON、结果和观测哈希；不自动批准、重试、停止、修改或执行，不继承上一条消息的授权。无新 schema/迁移、路由、scope 或 Runner wire；确定性 Go/前端回归覆盖隐私、kind 过滤、严格解析、会话导航和 handoff。真实 Provider/桌面长期运行仍单列验收。

## Agent Run 重试链的精确回看（H5-G53，2026-09-23）

智能体工作区打开执行详情时，重试 Run 会显示服务端确认的 `parent_run_id`，用户可点击“打开重试来源”回看同一 Task 的精确父 Run。前端保持 Task+Run 双身份查询，父记录缺失或归属不符时失败关闭，不用其他 attempt 或缓存冒充来源。此入口只读导航，不会自动重试、恢复、取消、验收、继续计划或继承权限；`parent_run_id` 仍只表示精确 retry，不表示父子 Agent 委派。

无新 API/schema/迁移、scope、Runner wire 或执行能力；确定性 AgentRunDrawer 回归覆盖精确父链导航，真实 Provider/桌面长链仍单列验收。

## Agent Run 纳入统一 Agent Inbox（H5-G52，2026-09-23）

统一 `GET /api/v1/ai/inbox` 新增 metadata-only 的 `agent_run` 来源及 `agent_run_total` 计数。它只投影当前需要关注的 Task Agent Run：`queued`/`running`/`failed`/`interrupted`，以及 `pending`/`retained` 产出交付；已存在连续精确 retry 子 Run 的失败父项、已由可验证 restart 证明替代的来源项和 `cancelled + not_ready` 不重复进入队列。前端续办队列对计划外 Run 继续使用更完整的独立 Run 查询；计划内的 actionable Run 从统一 Inbox 显示并可打开精确执行抽屉，也提供“交给智能体”入口。

每项只返回 Run/Task 身份、截断标题、attempt、Run 状态和产出交付状态，不返回结果正文、输入快照、文件/路径、Provider endpoint 或凭据。计划内入口只把这些 metadata 和固定提示带回当前 AI 对话；用户仍须在本条消息重新选择 `agent_execution` 授权，模型再调用 `workspace_agent_runs`/`workspace_outputs` 读取事实。统一 Inbox 不授予取消、重试、恢复或验收能力。复用既有表和重试证明，无 schema/迁移、scope、Runner wire 或自动调度变化；已通过 Agent Inbox Go 回归及前端严格解析/右栏测试。旧 Sidecar 缺少新计数时前端仅在无 `agent_run` 项的证明下兼容为 0。

代码证据：[统一 Inbox 投影](../../services/sidecar/internal/api/ai_agent_inbox.go)、[前端解析](../../apps/web/src/api/aiAgentInbox.ts)、[续办队列](../../apps/web/src/components/AiSessionRail.tsx)。

## 普通生成停止意图的持久化（H5-G51，2026-09-23，部分实现）

用户通过普通聊天取消 Generation 时，Sidecar 会先在 `workflow_events` 持久化 `ai_generation_stop_requested`，再发送本地取消信号。续办停止为了避免备份/维护写锁持有期间继续占用 Provider，会立即撤销当前进程中的续办 worker，再等待维护锁完成持久化；若 worker 收尾已先清掉活动 Generation 标记，停止事务会回看该续办最新轮次，仅对精确的 `queued`/`streaming`/`cancelled` Generation 幂等补记停止意图，不把自然完成/失败伪装成用户停止。停止接口只有在续办终态及 Generation 停止意图提交后才返回成功。若进程在该提交前退出且该事件不存在，启动恢复会如实把残留 Generation 标记为 `cancelled + AI_GENERATION_INTERRUPTED`；已提交停止事件的活动 Generation 则收敛为 `cancelled` 且不标记中断。无论哪种状态，都不会重放 Provider 请求。

该事件仅证明本地已持久化用户停止意图，不证明 Provider 已停止、计费已终止或副作用已撤销；普通取消接口的 `202 cancel_requested=true` 仍不是终态回执。普通 Generation 停止若持久化失败不会发送取消信号；续办停止可能已在进程内发出取消信号，但持久化失败会返回错误且不确认成功。复用既有工作流事件表，无 API、schema/迁移、权限或 Provider wire 变化。Go 回归覆盖普通取消的意图先于信号、续办在维护锁期间及时取消、worker 先收尾时的精确停止事件补记和启动后的显式停止/意外中断区分；真实 Provider 网络竞态和桌面体验仍待验收。

## 已确认子委派的安全启动恢复（H5-G50，2026-09-23，部分实现）

普通 Sidecar 重启只恢复新版本确认时在同一事务追加了 `ai_agent_delegation_queued` 事件、没有 `ai_agent_delegation_accepted` 事件、且当前仍没有子 Generation 的 `agent_delegate.spawn`。首次 Generation、用户消息与 accepted 事件在同一个事务持久化，Provider 调用只可能发生在该事务提交之后；因此只有排队事件、且无 Generation/accepted/stop 证据的记录可证明尚未受理。启动只重建冻结提案中的 Proposal/子会话/Provider/指令/授权，再通过原 Coordinator 与 `runAIGeneration` 路径重新核验；不会新建子会话、改写审批或扩大权限。旧版本没有排队事件的 confirmed spawn 继续登记 `AI_DELEGATION_INTERRUPTED`，不推断其启动意图。

父方停止请求在持有 Generation registry/Coordinator 锁时先追加 `ai_agent_delegation_stop_requested` 工作流事件，再发送取消信号。即使 Sidecar 在 worker 写终态前退出，启动也会依据该意图登记为 `cancelled`，不会复活已取消子项。若 Generation 已持久化，普通未请求停止的中断仍标记为 `cancelled + AI_GENERATION_INTERRUPTED`；普通聊天/续办中的显式停止由 H5-G51 依据 `ai_generation_stop_requested` 收敛为无中断错误码的 `cancelled`。两者均不重放；若 Generation 行缺失但留下 accepted 事件，则登记“受理结果不可确认”失败，不自动重试。此恢复仅覆盖 spawn；follow-up 与已受理/结果未知模型请求仍不重放。没有新增 API、scope、数据库表/schema 或 Runner wire。

确定性 Go 测试覆盖逐项/批量确认排队事件、精确重建冻结请求、父停止意图优先、旧版无事件记录失败关闭、Coordinator 重入队及 restore-pending 门禁。测试没有调用真实 Provider，也不代表跨进程 Provider 质量或断电验收。代码证据：[排队/恢复状态机](../../services/sidecar/internal/api/ai_agent_delegation.go)、[批量确认](../../services/sidecar/internal/api/ai_agent_delegation_batch.go)、[单项确认](../../services/sidecar/internal/api/ai_workspace_actions.go)、[停止意图](../../services/sidecar/internal/api/ai_agent_family_cancel.go)。

## 智能体请求工作区的初始分栏比例（H5-G49，2026-09-23）

已单次授权 `workspace_ui` 后，`workspace_open_panel` 的双面板请求可选 `split_ratio`（0.25–0.75），表示主面板占可用宽度的初始比例；仅能与 `companion_panel` 同时使用。省略时保留当前相同配对比例，新配对仍默认均分。Sidecar 严格检查范围并以双面板事件携带可选比例；前端再次校验后，在打开/激活精确面板配对时应用。之后用户仍可用分隔条随时调整；这只是本地布局请求，不读取面板内容、不改变标签能力或授权。

确定性测试覆盖模型工具参数、边界/非法值拒绝、SSE 解析、单次授权聊天事件、状态门禁与 UI 初始比例应用；旧的不带比例事件继续兼容。无 API 路由、数据库/schema、额外 scope 或 Runner wire 变化；真实模型对布局选择的质量及桌面 WebView 观感仍待专项验收。代码证据：[固定面板工具](../../services/sidecar/internal/api/ai_workspace_ui_tool.go)、[聊天事件桥](../../services/sidecar/internal/api/ai_generation_execution.go)、[工作区状态与显示](../../apps/web/src/store/ui.ts)、[Agent 工作区](../../apps/web/src/components/AgentWorkspace.tsx)。

## 智能体工作区双面板比例调整（H5-G48，2026-09-23）

右侧 Agent 工作区进入双面板布局后，用户可拖动两面板之间的分隔条调整主/次面板宽度，比例限制为 25%–75%。分隔条可键盘操作：左右方向键每次调整 2.5%，配合 Shift 调整 10%，Home/End 跳到边界，双击恢复均分；单面板模式不显示分隔条。该调整仅为本地临时布局状态，不改变标签身份或顺序，也不调整主聊天与右工作区之间的外层宽度。

确定性测试覆盖比例边界、新配对默认均分、键盘/拖动/双击恢复和单面板隐藏。没有 API、scope、schema、持久化或工具能力变化；真实桌面 WebView 的指针交互仍待人工验收。代码证据：[Agent 工作区](../../apps/web/src/components/AgentWorkspace.tsx)、[工作区状态](../../apps/web/src/store/workspacePanels.ts)。

## AI 文件提议的右侧只读预览（H5-G47，2026-09-23）

完整生成的单文件修改建议现在可由用户点击“在右侧工作区预览差异”，将右侧 Agent 工作区展开并打开绑定该 `proposalId` 的只读标签。标签只订阅当前 WebView 内存中的提议，展示相同的完整转义差异、文件相对路径、大小与当前操作状态；不会重新读取文件、不调用 Sidecar/Provider、不持久化正文，也不从历史记录恢复。

右侧标签不提供复核、勾选、写入、恢复或取消控件。唯一的审批和写入入口仍是左侧原对话卡片；该内部面板不加入 `workspace_open_panel` 工具枚举或工作区新建菜单。提议被关闭、过期、替换或所属会话卸载后，精确 ID 不再匹配，右侧显示不可用提示而不是其他提议或历史正文。没有 API、scope、schema、数据库、Runner wire 或文件能力变化。

确定性前端测试覆盖按需展开/打开、精确提议绑定、只读完整差异、不可用状态以及内部标签不进入模型可请求面板集合。代码证据：[提议卡片与只读镜像](../../apps/web/src/components/AiProjectFileReview.tsx)、[Agent 工作区面板](../../apps/web/src/components/AgentWorkspace.tsx)。

## 智能体请求双面板工作区布局（H5-G46，2026-09-23）

本条消息显式授权 `workspace_ui` 后，`workspace_open_panel` 仍可只打开/激活一个固定标签；现在也可额外给出一个不同的 `companion_panel`，让两个既有或新建标签直接并排显示。参数仍限 `overview|agents|files|review|terminal|browser|managed`；浏览器至多一个。Sidecar 对单面板保留原 `workspace_panel` SSE 和原 `panel` 工具回执字段；双面板使用严格的 `workspace_panels` SSE，并提供 `panels` 与 `layout=split` 回执。H5-G49 补入可选的有界 `split_ratio`，仅作主面板初始布局提示。

前端只在当前 generation 的合法事件到达后展开工作区，按顺序打开或激活两标签，以请求的第一个为主标签、第二个为分栏标签。每个布局请求只处理一次；过期或无效标签不会改变工作区。它不读取或回传面板内容、不接收路径/URL/命令，不启动终端、不导航或操作浏览器、不执行 Git/文件操作，也不创建业务写入、审批或 Agent Run。工作区 scope 仍为单消息授权，terminal 仅打开标签不启动进程，browser 不接受任意地址。

确定性证据：Go 工具测试覆盖单项兼容、双面板闭集和注入字段拒绝，聊天桥测试覆盖配对 SSE；`ai.test.ts` 覆盖事件严格解析，`aiChatLifecycle.test.tsx` 覆盖授权 generation 到布局请求，`RightOverview.test.tsx` 与工作区 store 测试覆盖精确分栏和浏览器限制。无公开 HTTP 路由、数据库/schema、权限范围或 Runner wire 变化；Sidecar 与前端需要配套更新。真实桌面 WebView/视觉交互仍需人工验收。

代码证据：[工作区工具](../../services/sidecar/internal/api/ai_workspace_ui_tool.go)、[聊天事件桥](../../services/sidecar/internal/api/ai_generation_execution.go)、[Agent 工作区](../../apps/web/src/components/AgentWorkspace.tsx)。

## Task Agent Run 的共享准入与 FIFO 执行队列（H5-G44，2026-09-23）

所有 Task Agent Run 创建入口（任务原生执行、AI 操作建议确认与 Run 重试）共用一个事务内容量门：单个 Sidecar 最多接受 8 个活动执行 + 32 个额外排队项。容量按 `queued/running + output_delivery_status=not_ready` 统计；达到 40 时返回 `429 AGENT_RUN_QUEUE_FULL`，创建及同事务的分派/Proposal 确认一并回滚。AI 提案保持 `pending`，用户可在容量释放后再次确认；无需重新生成建议。UI 明确说明本次没有创建执行或改变任务/审批状态。

持久化队列按 `created_at, id` FIFO 派发，启动恢复和每个执行 Worker 退出时都会补足最多 8 个并行槽；不在内存中重复维护待执行项。已经完成模型调用但处于 `running + pending` 的产出登记恢复不占执行容量，也不会重跑模型。升级前遗留的更大队列不删除、不阻断启动，继续 FIFO 排空；在计数降至 40 以下前不接纳新 Run。此功能不新增授权范围、HTTP 路由、schema/迁移或 Runner wire，不改变人工审批、Submission 登记与 Task 完成的事实边界；也不自动重试已失败的 Run。确定性 API/前端测试覆盖并行槽上限、FIFO、准入满载事务回滚和 pending 产出排除；真实 Provider/桌面体验仍需单独验收。

代码证据：[Agent Run 准入和 FIFO Worker](../../services/sidecar/internal/api/agent_runs.go)、[AI 操作确认](../../services/sidecar/internal/api/ai_workspace_actions.go)。

## Agent Run 产出与人工验收状态联动（H5-G43，2026-09-23）

`workspace_agent_runs`、`workspace_outputs(type=agent_run)` 与 `workspace_get(type=agent_run)` 对真实 Agent Run 的精确 Task Submission 只返回审核状态 metadata：`pending_review`、`accepted`、`changes_requested` 或 `withdrawn`。工作计划实时重读同一 Submission，并在“产出已提交 · 非任务完成”状态旁单独展示验收状态；`submitted` 仍只代表产出成功登记，不代表 Task 已完成或已验收。Submission 关联缺失/状态证据不可用时，计划和自动续办按证据不可用处理，不把它推断为已验收。

Task 原生审核事务提交后会无载荷唤醒 AI 计划续办扫描；前端同时取消并刷新工作计划、计划收件箱和续办投影。待验收仍可供智能体阅读并提出独立的 `task.review` 建议，但审核必须由用户另行检查并确认；本状态不暴露正文、审核原因或审核人，也不扩展 scope、执行权、自动审核、API 路由或数据库 schema。确定性 API/前端测试覆盖提交前待验收、审核后已接受以及计划投影刷新。代码证据：[Agent Run 回执读取](../../services/sidecar/internal/api/ai_agent_run_actions.go)、[工作空间查询](../../services/sidecar/internal/api/ai_workspace_tools.go)、[工作计划投影](../../services/sidecar/internal/api/ai_work_plan.go)、[人工审核提交后唤醒](../../services/sidecar/internal/api/task_outputs.go)。

## 子智能体全局并发准入与有界排队（H5-G41，2026-09-23，部分实现）

单个 Sidecar 进程内，父会话 spawn 与 follow-up 子 Generation 共用一个受限调度器：最多 8 个 Provider 尝试持有执行槽，最多额外排队 32 个已确认子项。排队数含等待槽位或等待 Provider/session 可用性的当前 worker；H5-G41 本身不约束普通对话或 Task Agent Run。H5-G44 为 Task Agent Run 单独提供数据库 FIFO 准入与执行队列；两个调度器各自计数，不共用容量，也不代表跨 Sidecar 实例全局配额。

单项确认和 H5-G40 批量确认都会在同一个确认数据库事务中预留对应容量，然后才提交 Proposal/子会话。容量不足返回 `429 AI_DELEGATION_QUEUE_FULL`，整项/整批事务回滚，Proposal 保持 `pending`，不会产生已确认但无运行槽的子会话；批量预留全有或全无。只有确认事务提交后才启动 worker。Provider/session busy 的 750ms 退避在执行槽外进行，使其他已排队子项可竞争运行槽；被接受 worker 仍沿用 10 分钟运行时限、父方精确取消和关闭行为。H5-G50 只为带持久确认排队事件的全新 `spawn` 子项恢复“Generation 事务尚未提交”的安全等待态；Generation 一旦存在便不重放，follow-up 及旧版无事件记录继续走失败关闭路径。父停止事件先落库再发信号。前端对队列满/调度器不可用给出确定的中文失败说明，和网络结果不明区分；批量审查会保留所选项与同意状态，重新读取提案后仍待确认时可由用户手动重试，不会自动重复确认。

此阶段没有新增权限、API 路由、数据表/迁移、模型工具参数或 Runner wire。容量是单 Sidecar 的运行时保护，不是跨进程全局配额，也不保证外部 Provider 侧限制。Go 确定性测试覆盖容量上限、失败批次不部分预约、达到上限时逐项及批量提案仍 pending、槽释放后队列继续、并发调用峰值及 H5-G50 的安全启动/取消恢复；真实 Provider 竞争、网络失败/停止竞态及桌面 UI 仍需人工验收。

## 子智能体委派的显式原子批量确认（H5-G40，2026-09-23，部分实现）

同一已完成父 Generation 若有至少两条待确认 `agent_delegate.spawn` 提案，右侧操作卡提供统一审查入口。用户可明确选择 2–4 项；没有默认选择，且每项仍分别展示完整任务名、指令、Provider/模型、scope、知识来源和数据是否离开设备。用户必须再勾选一次针对这组精确提案的同意，单项确认/拒绝按钮在批量审查中锁定。原有逐项确认路径保留。

新增 `POST /api/v1/ai/generations/:id/agent-delegations/confirm`。请求只含显式 `confirm_agent_delegations=true` 与有序的提案 ID/指纹列表；服务端严格拒绝未知/重复/null 字段、非规范 UUID、重复身份、错误指纹、超量字段及超过 4 KiB 的正文。Sidecar 要求全部提案属于同一已完成 Generation、同一父会话，且都仍待确认；逐一重算完整冻结预览，重验指令、scope、知识来源、Provider/配置与子会话身份。全部预检通过后，在同一事务内创建所有子会话、确认提案并写入事件；任一项变化或写入失败则整体回滚，不产生部分确认。事务提交后才分别启动各个子 Generation；每项启动失败独立登记，不撤销已提交的确认，也不自动重试。

精确完整批次的重放只返回既有结果，不重复创建或启动子会话；若其中部分提案已通过逐项路径决定，则批量请求冲突并要求刷新后重新审阅剩余项。批量确认不合并子权限、不提供跨子会话事务性执行保证，也不新增 scope、数据库迁移或 Runner wire。确定性 Go/API 与前端测试覆盖严格输入、全组漂移回滚、有序回执、独立启动及幂等重放；真实 Provider 并发失败及 Windows 桌面交互仍待人工验收。

## 父会话对子 Generation 的显式原子批量停止（H5-G39，2026-09-23，部分实现）

右侧“Agent 执行”可对保存的父会话列出当前仍活动的子 Generation。用户逐项选择 1–4 个当前子轮次后，先查看任务名并确认一次，才会发送 `POST /api/v1/ai/sessions/:id/agent-children/cancel`。请求只携带所选的 `child_session_id` 和 `generation_id`；服务端拒绝未知/重复字段、重复身份、非规范 UUID、空列表、超过四项或超过 4 KiB 的请求。

Sidecar 在只读事务中重新投影父会话的完整子关系，并校验每项仍是已确认 spawn/follow-up 授权的当前活动轮次；既检查持久 Generation 的子会话归属，也检查尚未落库的预受理 Coordinator Worker。随后在 Generation registry 与 Coordinator 锁下先对整批预检，再统一发送取消信号。任一项已过期、已进入最终化或已没有活动 Worker，则整批返回冲突且不发送任何信号。预受理 worker 与持久 Generation 可以出现在同一批次。

HTTP `202` 和有序回执只表示全部本地停止请求已接受；UI 显示“已请求停止 · 等待状态更新”并继续读取权威状态。它不撤销远端 Provider 请求、费用或既成操作，不级联到未选择的兄弟，也不停止子会话自行启动的其他轮次、Task Agent Run 或 Task。控制身份只用于右侧 UI，不进入模型工具。新增 API v1 写路由，不改 schema079、scope、数据库或 Runner wire。确定性 Go/前端测试覆盖批次边界、重复/乱序回执拒绝、全批身份重验和受理反馈；真实 Provider 网络故障、桌面并发与关闭恢复仍需人工验收。

## 内嵌 Chromium 可见链接的显式交接（H5-G38，2026-09-22，部分实现）

H5-G38 在 H5-G37 的用户点击式网页文本快照中补充可见链接目录，使智能体能准确理解页面上的链接文字与目标地址，并在用户另行要求时准备经确认的浏览器导航。固定 WebView2 脚本最多返回 20 个唯一、已渲染且非隐藏的 HTTP(S) 链接；目标地址移除 query/fragment，含 username/password 的链接直接拒绝，链接文字与目标地址合计最多 4 KiB。正文仍最多 8 KiB，正文与链接各有独立截断标记。`mailto:`、`javascript:`、`file:` 等非 HTTP(S) 链接不会交接。脚本不读取表单值、Cookie、存储、历史、下载或截图，不执行网页动作。

链接路径本身仍可能包含敏感资料，因此所有链接文字和脱敏目标地址都会完整显示在同一滚动交接预览中；用户仍需再点击“带入网页文本”并手动发送才会到 Provider。页面地址、标题、正文、链接文字和目标地址均为不可信数据。交接的 `scopes` 为空，既不授予 `workspace_browser` 也不会自动导航；若之后需要导航，用户须另行选择 `workspace_browser` 并逐次确认本地请求。没有任意 DOM/脚本工具、表单或点击能力。

验证：Rust 对原生返回执行严格字段解析、协议/凭据校验、脱敏与数量/字节上限检查；前端覆盖 query/hash 脱敏、注入定界、完整预览、链接独立截断、链接-only 页面及非 HTTP(S) 拒绝。真实站点可见性、敏感路径与 Windows/WebView2 行为仍需人工验收；不新增 API、数据库迁移、scope 或 Runner wire。

## 内嵌浏览器可见文本的显式交接（H5-G37，2026-09-22，部分实现）

Windows 桌面版内嵌 Chromium 工具栏新增“读取当前网页文本并预览给 AI”。只有用户点击后，主 WebView 才对当前已完成加载、未报错且不是应用内部来源的活动标签发起一次固定原生命令；Tauri 重新核对活动标签、来源及加载状态，WebView2 在当前页面上下文读取 `document.body.innerText`，最多扫描 100,000 个字符并返回至多 8 KiB UTF-8 文本。捕获前后均核对活动标签、URL 与导航代次，最长等待 8 秒；不接受任意脚本、选择器、网址或浏览器控制命令。

捕获只暂存于前端内存中的“来自工作台的交接”卡。快照包含脱敏到移除查询参数和片段的 HTTP(S) URL、最多 512 字符标题、最多 8 KiB 正文及截断标志；不含标签 ID、Cookie、存储、历史、下载、截图或 DOM 节点。卡片可滚动预览脱敏地址、标题与完整有界正文，正文截断时明确提示。用户还需点“带入网页文本”，再在聊天输入框单独手动发送，才会传给当前所选模型；点击捕获或“带入”均不发送、不调用模型。页面文字是不可信输入，可能含敏感信息或提示注入，模型应把它当待分析内容而非指令；该交接的 `scopes` 为空，不增加浏览器/网络/文件/Git/终端权限，也不触发导航、点击、表单或页面提交。文本读取失败、空页、标签切换、加载变化或超时都不会创建交接卡。

验证：固定脚本与严格结果解析有界单测；前端覆盖活动标签复核、脱敏、注入定界、大小上限、预览与失败不交接。真实网页可见文本敏感性、特定站点 DOM 行为及真实 Windows/WebView2 的专项体验仍需人工验收；无 API/schema/数据库迁移、scope 或 Runner wire 变化。

## 多子会话 wait-any / wait-all（H5-G36，2026-09-22，部分实现）

批量 `workspace_agent_children(view=wait,child_session_ids=[...])` 可选 `wait_mode=any|all`：省略时继续默认 `any`，首个子会话到达 `completed/failed/cancelled/unavailable` 即返回；`all` 则等待所有目标进入终态。两者均保留 10 秒默认、15 秒上限；到期时返回最新只读状态快照和已终态 ID，`timed_out` 表示所选等待条件是否尚未满足。单目标旧调用与回执形状不变。

此开关只改变何时结束等待，不读取结果正文、不创建续办租约、不确认工作台建议、不停止其余子会话，也不扩展授权。等待仍先订阅提交后无载荷唤醒，每次重读都在新的只读事务重新验证精确父子关系；1 秒重读保留为兜底。无新 HTTP 路由、数据库迁移、scope 或 Runner wire 变化。

## 多个子会话回复的一次性回收（H5-G35，2026-09-22，部分实现）

父会话可调用 `workspace_agent_children(view=results,child_session_ids=[...])`，在一个只读数据库快照中读取 1–4 个本会话已确认子会话的首个回复页。每个子项都独立重验父子关系、已确认委派/续交办、唯一完成消息及当前消息授权；任何一个子项未完成、不可用、身份不符或当前 scope/知识源/Provider/配置不满足时整批失败，不返回部分结果。请求顺序就是响应顺序。

每个子回复默认最多 1000 个 Unicode 字符，可通过 `content_limit` 下调；汇总正文原始 UTF-8 字节最多 8 KiB，超限时拒绝整批。响应逐项带有精确 `generation_id`、`content_sha256`、`reply_kind`、页偏移、后续偏移和 `content_trust=untrusted_child_reply`。较长内容仍须对单项使用既有 `view=result`，将 generation ID 和完整 SHA-256 绑定到后续页。内容不会自动合并父上下文，也不是工作台或 Task Agent Run 的事实；父模型仍须独立读取相应领域事实，用户仍须审批其工作台提议。

该批量读取没有新 scope/权限继承，不接收子会话自发 Generation，不读取文件权限、Provider 机密或恢复内容，不增加 API、数据库迁移或 Runner wire。确定性集成测试验证两子顺序读取、逐子新授权和整批失败无部分正文；真实 Provider 多子任务质量仍待专项验收。

## 多子会话的有界 wait（H5-G33，2026-09-22，部分实现）

父会话在本条既有 `work+outputs+actions+agent_execution` 授权下，可继续用 `workspace_agent_children(view=wait,child_session_id=<uuid>)` 等单个子会话；新增 `child_session_ids=[<uuid>...]` 可一次等待 1–4 个本父会话已确认的子会话。批量模式默认 wait-any，也可用 `wait_mode=all` 等到所有目标进入 `completed/failed/cancelled/unavailable`；返回同一只读快照中的所有目标状态及终态 ID。达到 10 秒默认、15 秒上限仍未满足选定条件则返回超时快照。等待会先订阅提交后无载荷唤醒，再用新的只读事务完整重验父子关系及状态；每 1 秒数据库重读仅作漏通知兜底，不跨等待持有事务。重复、空、超量、非规范或非本父会话子 ID 整组拒绝。

单目标旧参数和结果形状保持兼容。批量回复只含子会话身份、任务名、状态、错误码、待审批数与可用时的精确入口，不带任何回复正文、scope、Provider 或文件信息；任一等待模式都不读取结果、不创建续办租约、不授权/确认工作台操作，也不取消其余子会话。用户/当前生成取消仍中止等待。API v1 模型工具 schema 扩展，无 HTTP 路由、数据库迁移、scope 或 Runner wire 变化；真实 Provider 并行体验待专项验收。

### 子会话等待的提交后唤醒扇出（H5-G34，2026-09-22，部分实现）

子 Generation 终态及启动前失败/取消已经会在持久事实提交后唤醒续办协调器。现有信号仍保留给其单消费者扫描循环，同时以独立缓冲通知 fan-out 给活动子状态等待者；通知不含会话/Generation ID、状态或业务载荷。等待先订阅，再做首次数据库读取，避免提交发生在“读完后、睡眠前”的竞态；通知只触发新只读事务重新验证本父会话下所有指定子会话与权威 Generation 状态，不能作为状态或授权证据。每 1 秒仍完整重读，兜底未通知的外部删除等变化；等待取消、超时或结束时退订。当前只在 Sidecar 进程内有效，不提供系统唤醒；API、schema、scope、数据库及 Runner wire 不变。

## 父子会话的显式停止请求（H5-G32，2026-09-22，部分实现）

父会话右侧“Agent 执行”对子会话当前由父方已确认 spawn/follow-up 授权的生成轮次提供两步停止入口。用户先点“停止”，再点“确认停止”；Sidecar 每次按父 Session、精确子 Session、精确 Generation/预留 Proposal 身份重读并核验已确认父子关系。已持久化的 `queued/streaming` Generation 通过当前进程取消句柄停止；仍在 Coordinator 等 Provider/即将受理、尚无 Generation 行的轮次则取消其当前进程 worker，并在同一业务路径登记 `cancelled` 终态，防止重启后重放。普通子会话自行发起的生成、其他子项和 Task Agent Run 不能由此路由停止。对已有 Generation 的停止事件现在也由 H5-G51 先持久化，避免崩溃后把显式用户停止误报为普通进程中断。

接口 `POST /api/v1/ai/sessions/:id/agent-children/:child_id/generations/:generation_id/cancel` 的 `202 cancel_requested=true` 只表示本地已接受取消请求，不是 Generation 终态、远端请求已撤回或计费已停止的证明；右侧继续从权威 Generation 状态刷新。停止不撤回已发生的工具/业务副作用，不阻止用户随后再次启动子会话轮次，也不会级联停止兄弟子会话或 Task Agent Run。H5-G50 使预受理 worker 的停止意图先持久化再发本地信号，H5-G51 为已创建 Generation 也先写停止事件；若进程在终态前退出，启动会据此登记 `cancelled`，而不是重新启动。没有停止事件的普通中断仍登记 `AI_GENERATION_INTERRUPTED`；任何情况都不重放。可取消轮次身份只在右侧会话树 API 投影，不返回给模型 `workspace_agent_children` 工具。API v1 增加一条受关系校验的写路由；schema079、数据库、Provider/Runner wire 与模型 scope 不变。真实 Provider 中断行为仍待专项验收。

## 已有子会话续交办（H5-G31，2026-09-22，部分实现）

保存的非子会话在本条 `work+outputs+actions+agent_execution` 授权下，现在可由模型通过专用 `workspace_agent_followup(child_session_id,message,scopes)` 向自己已确认的现有子会话提议一次后续指令。普通 `workspace_propose` 仍拒绝该动作；子会话没有递归工具。提议只保存审批卡，不发送消息。前端严格解析完整 `agent_followup` 预览，展示原任务、精确子会话、完整新指令、权限/知识来源、Provider/模型/离机状态、子会话版本和上一轮 Generation；用户必须另行勾选确认。拒绝、过期或未确认不启动。

服务端在提议、确认及真正受理新子 Generation 时都重查已确认父子关系和当前子状态：首轮或最新子生成未完成、仍有待审批操作、子自动续办活动、Provider 变化或子会话漂移时拒绝；本次 scope 只能是原子任务非文件权限的子集，知识来源不能换。新生成使用 Proposal ID 单次受理；受理事务只排除与本次确认精确匹配的那条新记录，不忽略其他活动生成。重复确认/调度不重发不确定模型请求；确认后未受理而中断或重启只登记失败。`agent_followup_result` 返回权威新生成状态和子会话待审批数量；子会话不可用时不提供跳转。计划仍以真实终态及待审批计数阻塞，`completed` 不等于业务完成。

父会话现在可用 `workspace_agent_children(view=result)` 按需读取本会话已确认的最新子回复：有已确认续交办时默认选最新续交办 Generation，否则读首次委派回复；传入 `generation_id` 可精确选择自己授权的那一轮，不会读取子会话自行发起的其他 Generation。读取按被选轮次重验当前 scope、知识源和 Provider/配置；follow-up 只能是原 scope 的非文件子集。每页返回 `reply_kind/generation_id/content_sha256`，偏移续页必须同时回传 `generation_id` 与完整正文摘要。回复仍是不可信文本，不自动并入父上下文或成为业务事实。真实 Provider/桌面多轮交互、系统级后台唤醒、自动取消级联与任意 send/interrupt 仍待验收或实现；H5-G32 只增加父方人工发起的单个活动 Generation 停止请求。API v1/schema079/Runner wire 未变。详细阶段见 [H5-G31 计划](../plans/agent-harness-phases.md#2026-09-22-h5-g31-已有子会话续交办的接受门禁部分实现)。

## 子任务待审批阻塞可见性（H5-G30，2026-09-22）

已确认子会话的首轮 Generation `completed` 只说明模型回复结束，不代表子会话中提出的工作台操作已获人工确认。父会话的 `workspace_agent_children(view=list/wait/result)` 和右侧“Agent 执行”子会话行现在返回/显示 `pending_approvals`：只统计该子会话中**已完成 Generation** 所属、状态仍为 `pending` 且创建未超过 24 小时的操作建议。已拒绝、已过期、非子会话或生成未完成的建议不计入；不可用子会话固定为零。右侧有子会话时在终态后仍每 15 秒低频刷新，活动生成仍每 5 秒刷新，以跟上人工审批的变化。父会话只获得数量，仍须人工打开精确子会话审查完整卡片，不能由父智能体批准、执行或把 `completed` 当作业务完成。`view=wait` 仍只返回元数据，`view=result` 的正文继续要求原有逐次授权；待审批事实仍是 follow-up 阻塞条件。API v1 的只读响应增加字段，schema079、scope 与 Runner wire 不变；真实 Provider 和桌面交互待验收。

确认回执 `agent_delegation_result.pending_approvals` 使用同一只读计数。只要首轮生成完成但该计数大于零，计划步骤保持 `child_pending_approval`/未满足，后台续办返回 `pending_approval`，不会因模型已经回复而继续父轮次；用户决定子会话建议后，下次事实重查才可解除阻塞。前端把它与原 Proposal 自身尚未确认的 `pending` 区分。后续 follow-up 的执行入口仍须独立重验，不能信任这次列表快照。

## 子任务回复分页的一致性绑定（H5-G29，2026-09-22）

`workspace_agent_children(view=result)` 默认读取已确认子会话最新一轮父方授权回复（若无 follow-up 则为首次委派回复），也接受精确 `generation_id` 选择该父会话创建/确认的那轮。非零偏移续页必须同时带 `generation_id` 与 `expected_content_sha256`（64 位小写 SHA-256），服务端在同一个只读事务内重验原委派或续交办、Generation、唯一完成消息及完整正文摘要；不一致即拒绝该页并要求从第一页重读。`list/wait` 禁止携带分页参数。摘要只绑定读取内容，不是业务事实、签名、批准或授权。原有逐次 scope/知识来源/Provider 核验继续有效；原始逐份文件权限不转交，follow-up 回复只允许其冻结的非文件 scope 子集。没有新增自动合并或子代理中断能力；API v1、schema079、Runner wire 不变。真实 Provider 多轮体验仍待验收。

## 子任务终态等待与计划唤醒（H5-G28，2026-09-22）

保存父会话在本条 `work+outputs+actions+agent_execution` 授权下，可调用 `workspace_agent_children(view=wait,child_session_id=<uuid>,timeout_ms=...)` 等待自己已确认的单个子任务。`timeout_ms` 可选，默认 10000 毫秒、范围 1–15000；等待订阅无载荷提交后唤醒并在新只读事务重验父子关系和权威 Generation 状态，另每 1 秒重读作为兜底，不在等待期间持有数据库事务。`completed/failed/cancelled/unavailable` 立即返回，其他状态到期返回 `timed_out=true`；取消当前模型轮次会取消等待。H5-G33 另支持批量 wait-any；见上节。返回仍仅含状态元数据，不携带子指令、授权、回复或文件；完成后仍须用 H5-G27 的 `view=result` 逐次重验所选结果轮次的授权才可读回复。

已确认子 Generation 的终态提交或启动前失败记录成功提交后，会向既有计划续办器发送不含业务数据的进程内唤醒；同一通知现以独立 fan-out 订阅唤醒等待中的父工具，不会消费续办器自己的扫描信号。等待方醒来后只重查数据库，不信任通知中的身份/状态；1 秒周期读取兜底漏通知。续办器仍只重查原有租约/阻塞条件；通知不创建租约、不批准建议、不重跑请求，也不自动合并子回复。三秒续办周期扫描仍是兜底；应用关闭时没有系统级唤醒。确定性测试覆盖超时、终态变化、wait-any 回执、扇出与退订、外部子会话/非法参数/取消和独立终态唤醒；没有真实 Provider、桌面或跨重启专项验收。API v1、schema079、Runner wire 与模型 scope 不变；仍无递归委派、通用子会话 send/interrupt 或取消传播。

## 父智能体读取子任务进展与首轮回复（H5-G27，2026-09-22）

保存的非子会话在本条显式获得 `work+outputs+actions+agent_execution` 后，模型可通过 `workspace_agent_children(view=list)` 读取本会话已确认的最多四项子任务的身份、任务名与权威 `queued/streaming/completed/failed/cancelled/unavailable` 状态。读取从同一 AI Session → Generation → 已确认 `agent_delegate.spawn` Proposal → 子 Session/Generation 的只读投影完成；模型不能传任意父会话 ID、遍历其他对话或把 `confirmed` 当作完成。列表不含子任务指令、结果正文、scope、知识来源或 Provider 配置。

精确 `view=result,child_session_id=<uuid>` 在没有父方续交办结果时返回首次委派回复；存在续交办时默认返回最新已确认 follow-up 回复。可用 `generation_id` 在这两类父方授权回复中精确选择一轮，但不能指定子会话自行运行的 Generation。每页最多 4000 个 Unicode 字符；服务端核对已确认 Proposal、冻结预览、Generation 与唯一完成消息全文一致。读取当前轮次时重新核对本条授权覆盖其 scope、知识来源及版本，并要求与子任务使用同一个 Provider 及配置版本；follow-up scope 是原权限的非文件子集。原委派包含逐份 `agent_files/agent_project_files` 权限时仍不得读取正文，须由用户手动打开子会话审阅。读取失败不返回部分正文。子回复是不可信内容，不是工作台事实、授权或已执行回执；模型仍需按业务工具重读事实。

这是父会话内的按需只读查询，不是自动合并历史或通用 `send/follow-up/wait/interrupt`；不会让父会话继承子会话授权、批准子会话的建议或操作 Task Agent Run。仅增加模型工具及 `agents` 指南，不新增 HTTP 路由、scope、数据库迁移或 Runner wire；API v1/schema079 不变。确定性测试使用假 Provider/真实 SQLite 委派链，真实 Provider 质量与多轮人工体验仍待验收。

## 父子会话的只读导航与状态（H5-G26，2026-09-22）

智能体右侧“Agent 执行”现在另外显示当前保存对话的子会话列表；进入受委派子会话时可返回精确父会话。`GET /api/v1/ai/sessions/:id/agent-family` 在只读事务中从已确认的 `agent_delegate.spawn` Proposal、冻结预览、不可变 `result_id` 和子 Generation 投影关系。子行展示任务名与真实 `queued/streaming/completed/failed/cancelled` 状态；已确认但子会话丢失时显示不可点击的“会话不可用”，不伪造执行成功。活动子 Generation 每五秒刷新；请求失败可人工重试。

响应仅有会话/Proposal 身份、任务名、状态、错误码和时间，不返回子任务指令、scope、知识来源、Provider 配置、消息或文件内容；身份、预览或结果不一致时整体失败关闭。H5-G32 另增加仅给 UI 的当前父方授权活动/预受理轮次 ID；模型 `workspace_agent_children` 投影会剥离该字段。导航不自动继承权限、给父会话合并结果或级联传播取消；用户仍可在父会话里单独两步确认停止精确活动子轮。与 Task Agent Run 的委派账本并列展示，但 `parent_run_id` 仍只代表 Run 重试。API v1 新增只读和受校验写路由；schema079、scope、Runner wire、人工审批和真实 Provider/桌面验收边界不变。

## 受确认的有界子智能体委派（H5-G25，2026-09-22）

保存的主对话在本条消息显式获得 `actions+agent_execution` 等所需范围后，可调用 `workspace_delegate_agent` 提出一个独立子对话：模型必须给出规范 `task_name`、完整有界 `message` 和当前授权的明确子集；选择 `knowledge` 时会带上本条已选的全部知识来源及冻结版本，确认卡逐项列出。`workspace_ui`、`workspace_browser` 不能委派；子对话不继承主对话的历史授权、右侧文件/Git/终端/浏览器内容或任意本机路径。主对话最多保留四个待确认或已确认子委派，深度固定为一层；子对话没有再次委派工具。

委派仍走 `agent_delegate.spawn` Proposal：确认卡展示子任务文本、全部范围、知识来源、父/子会话及 Generation 身份、Provider/模型与离机提示；用户须为本次委派另行勾选确认。确认事务只创建独立的保存子会话，之后进程内协调器以 Proposal ID 作为子 Generation ID 有界启动一次。用户可通过严格的 `/ai?session=<uuid>` 打开子会话，回执与计划读取子 Generation 的真实 `queued/streaming/completed/failed/cancelled` 状态；排队不算完成，失败不算完成，完成但仍有待审批建议也不会满足计划。子会话尚未接受首次生成时，普通聊天返回 `AI_DELEGATION_PENDING`，不能抢占冻结的首条消息。

Provider 忙时只在生成被接受前短暂等待；一旦接受，不重放模型请求。普通重启若发现已确认委派尚无子 Generation，就登记可见的失败回执，不猜测之前是否已经发送请求；取消/超时和错误也不自动重试、继承权限或批准子会话中的业务建议。子智能体仍受原有单消息授权、工具预算与人工审批约束。此切片不是通用的 send/follow-up/wait/interrupt 协议，没有自动父子取消级联、跨应用后台服务、任意 Shell/文件/Git/浏览器控制或完整自治编排；H5-G32 仅支持父方用户从右侧单独确认停止已授权的活动子 Generation。`parent_run_id` 仍只表示 Agent Run 精确重试。API v1/schema079 不新增迁移或 Runner wire；真实 Provider、桌面交互和断电窗口仍需专项验收。

## 并行 Agent Run 的原子批量中断（H5-G24，2026-09-22）

AI 在同一轮计划中发现多个不同 Task 的 Agent Run 需要停止时，可以提出一次 `agent_run.cancel_many`。动作只接受 1–8 组规范 `agent_run_id / task_id / expected_task_version`，禁止重复 Run、重复 Task、额外字段和非正版本；它沿用 Agent 执行控制所需的 `work+outputs+actions+agent_execution`，不会因为“批量”获得新的读取或执行范围。确认卡会逐项显示 Task、Run、当前状态、attempt、Provider 与模型，用户必须为这一次跨任务停止单独勾选并确认。

服务端在确认事务中重新读取整批对象，并把预览冻结事实与当前 Run/Task 状态逐项比对。只要一项已结束、Task 版本变化、已有不同取消请求或身份不一致，整批保持未执行；只有所有取消请求在同一数据库事务提交后，才通知各自的进程内 Worker。第二项注入失败、事务回滚和重复确认均有确定性测试，确保不会出现“前几个已停、后一个失败”或提交前误发 Worker 信号。

确认回执只包含有序的 Run ID、Task ID 与状态，并使用严格地址 `/ai?workspace=agents` 返回 Agent 工作区。计划投影在任一目标仍是 `running` 时保持运行中且未满足，只有全部目标均为权威 `cancelled` 才把步骤标记为已取消。该动作不取消 Task、不删除产出、不撤回已登记结果，也不保证远程 Provider 已接收的请求或计费立即停止；它更不是 Codex 式父子 Agent `interrupt`。H5-G24 本身没有建立父子会话；后续 H5-G25 只补一层受确认 spawn，仍无通用 send/follow-up/wait 或取消传播，`parent_run_id` 仍只表示精确重试。API v1、schema079 与 Runner wire 不变。

## 当前会话委派与 Agent 执行关联（H5-G23，2026-09-22）

智能体右侧原“子智能体”页签已改为更准确的“Agent 执行”。只要当前主对话是规范保存会话，页签默认读取 `GET /api/v1/ai/sessions/:id/delegated-runs`，显示该对话经人工确认实际创建的 `agent_run.start/retry` 结果，而不是把全局 Run 账本误当成当前对话的子代理。用户仍可切换“全部记录”和“待登记”；两者继续使用原全局 metadata-only 接口。

会话投影通过 `AI Session → Generation → confirmed Proposal → result_id → Agent Run` 的既有不可变事实建立，并将 Proposal ID 与该会话最新计划修订中的步骤关联。每行可显示计划步骤标题，未进入最新计划则明确标为“未绑定当前计划”；打开详情时仍保留来源提示与精确原确认回执入口。Task 删除导致 Run 级联缺失时，历史确认决定和 result ID 仍显示为“执行记录已不可用”，只能返回原回执，不能查询或操作一个不存在的 Run。响应不包含 Proposal action/preview 正文、Run 输入快照、结果正文、文件引用、Provider endpoint/凭据或授权。

H5-G23 的列表本身不是自治子代理协议，不创建子会话；后续 H5-G25 的一层受确认 spawn 是另一条链，仍无通用 send/follow-up/wait/list/interrupt、父子消息邮箱或取消传播。`parent_run_id` 继续只表示同一冻结执行的精确重试。列表读取不会确认建议、启动/取消/重试 Run、恢复登记、验收 Submission、继续计划或调用模型。API v1 新增只读路由，schema079、scope、Runner wire 与人工确认边界不变。

## 权威事实提交后的事件驱动续办（H5-G22，2026-09-22）

已经由用户单独开启的有界续办，不再只能等待 3 秒安全轮询发现工作台变化。AI Proposal 的确认/拒绝事务提交后，以及 Agent Run 的取消、精确重试入队、产出恢复、身份失败、中断和最终交付状态持久化后，会向进程内协调器发送一次无载荷唤醒；协调器随后从 SQLite 重新读取计划、全部相关建议和 Run/Submission 权威事实。信号不携带 Proposal、Run、正文、文件、Provider 或授权内容，并用容量为一的通道合并突发变化。

唤醒不等于继续：若并行计划中仍有任一待确认建议、活动 Run、待登记产出或异常证据，续办继续等待；只有原 H5-G1 的全部门禁同时通过，才会在既有 TTL、最大轮数、冻结 Provider/范围和并发上限内开始下一次模型生成。普通对话没有续办租约时不会因此发消息；失败事务、仅重放已决定卡片和未持久化的执行结果也不授予推进权。周期扫描仍作为进程内丢失/合并通知的安全兜底，重启、恢复和停止规则不变。

确定性集成测试把兜底轮询延长到一小时，并移除关键等待路径的手动扫描；OpenAI-compatible 与 Anthropic 两条协议均通过真实 Executor 子进程、Run 交付、人工验收和两轮后台续办。这不是关闭应用后的系统级唤醒、跨进程消息总线、自动审批/重试/恢复/验收，也没有建立 Codex 式可收发消息的自治子代理。API v1、schema079、scope、Runner wire 和模型预算不变。

## 并行 Agent 计划的一次性完整阻塞摘要（H5-F9，2026-09-22）

“继续此计划”的只读核对不再只返回一个最高优先级原因。Sidecar 会遍历当前计划全部有效步骤、每个关联生成以及这些生成中未绑定到可见步骤的同轮建议，汇总 `evidence_unavailable / generation_unavailable / pending_approval / output_pending / run_active` 五类阻塞数量，并按固定优先级保留原 `reason` 兼容字段。每类同时返回去重、排序后的本会话 generation ID；响应不含 action preview、Task 正文、Run 输入/输出、Provider 配置或可复用授权。

主对话计划卡在人工点击续办检查后显示完整摘要，例如“待确认 2 · 结果待登记 1 · Agent 执行中 1”，并为所有相关回执组提供入口。这样一个计划在不同 Task 上并行等待多个 Agent Run 时，用户无需处理完第一个阻塞后才发现下一个。SQLite 仍禁止同一 Task 同时存在两个活动 Run；不同 Task 的 Run 可由同一计划并行跟踪。后台有界续办继续复用同一全量门禁，任一 Run 执行中、结果待登记或建议待确认都不会提前发起下一轮模型请求。

该摘要只增强可见性和协调，不自动确认建议、等待、重试、恢复产出、接受 Submission 或完成 Task，也不授予 `agent_execution` 等 scope。`GET /api/v1/ai/sessions/:id/plan/continuation` 新增 metadata-only `blockers:[{reason,total,generation_ids}]`；`blocking_generation_id` 和最高优先级 `reason` 保持兼容。API v1 与 schema079 不变。

## 计划回执到 Agent 执行与验收（H5-F8，2026-09-22）

主对话计划卡与右侧计划镜像不再把所有真实回执笼统显示为“查看工作台记录”。只有严格解析为精确 Agent Run 的地址显示“查看 Agent 执行”，严格解析为精确 Task Submission 的地址显示“查看待验收提交”；其他安全地址继续使用通用标签，外部或畸形地址不生成链接。打开工作台时会在前端附加当前保存会话的返回身份，route 本身仍由服务端真实回执提供，不接受模型自造路径。

右侧“子智能体”精确 Run 详情对 `submitted` 结果直接提供同一待验收批次入口，并明确成功执行不代表人工验收通过；`retained` 只解释保留原因，`pending` 仍只能恢复登记。该纵切把“计划 → Run → Submission → 返回原对话”的人工路线连通，但不自动导航、验收、继续计划、恢复或重跑模型，不新增权限、API、迁移或模型可见内容。详见 [本地 Agent H5-F8](local-agents.md#计划执行与人工验收的精确往返h5-f82026-09-22)。

## Agent 文件产出到右侧项目文件审查（H5-F7，2026-09-22）

用户现在可从 Task 的执行记录、执行过程抽屉，或智能体右侧“子智能体”中的精确 Agent Run 详情，选择一份已经 `submitted` 的 file/files 产出，直接展开或切换到右侧“文件”工作区。该动作只建立内存候选；用户仍须自己打开一个现有文本目标，再为该目标建立完整原生基线。界面随后显示 Agent 产出替换目标全文的完整差异和字节数，目标路径不会发送给模型，右侧预览内容也不会自动进入聊天。

候选要求 Run 成功、Submission/Artifact 身份完整、产出契约和载荷一致、正文为无 NUL UTF-8 且单份不超过 32 KiB；纯文本、未提交、待恢复、保留结果或不合规多文件载荷不能进入。建立目标基线前及真正提交前都会精确重读来源 Run，逐项比对提交身份、attempt、文件序号、契约和完整正文；读取失败只阻止本次动作，事实变化会释放旧基线，均不会调用原生写入。每次只处理一份产出和一个已有文件，不创建、删除、改名或批量写入。真正提交复用既有 `workspace_file_edit_apply`：页面明确勾选之后还要经过独立 Win32 审查、按次 UAC 和绑定 helper 的完整恢复/无覆盖执行链；普通开发构建只允许审查。取消、漂移、过期与结果未知均不会自动重试。

该桥接是人工工作区操作，不是模型工具或 Agent 自动落盘：不新增 scope、HTTP API、数据库迁移或 Runner 协议，不授予文件路径、Shell/Git/终端能力，也不会把文件写入成功等同于 Submission 验收或 Task 完成。代码与测试见 [本地 Agent H5-F7](local-agents.md#已提交文件产出到项目文件的人工桥接h5-f72026-09-22)；真实发行安装包的跨权限 UAC/SACL、桌面可访问性和断电恢复仍待专项验收。

## 文件写入安全门禁与只读恢复检查（2026-09-22，部分实现）

恢复面板的“复核恢复条件（只读）”会核对完整主文、普通附带元数据与可读权限，并冻结最多八项、十分钟的本机单次票据；页面再核对身份、模式和完整差异。若当前是带构建绑定 helper 的 Windows 桌面发行构建，用户可勾选后进入独立 Win32 完整审查，再由 Windows 为这一次操作请求 UAC；普通开发构建只显示动态不可用原因。票据、网页确认、原生确认和 UAC 是不同门禁，任一项都不能单独代表执行成功。独立 helper 会再做完整 SACL、恢复阶段和 started-only 无覆盖执行，父端只接受同一 operation ID 的规范终态回执；CLI、模型、Sidecar 与子 WebView 没有直接调用权。真实跨权限 UAC/SACL、安装包和断电验收尚未完成。见 [产品入口](desktop-platform.md#绑定发行构建的按次单文件操作入口2026-09-22部分实现)、[审查票据](desktop-platform.md#恢复条件审查票据与按次提权边界)与[辅助程序边界](desktop-platform.md#单文件辅助程序独立权限核心部分实现)。

Windows 实测证明独占文件句柄不能阻止晚建硬链接，因此版本 1 原地 writer 与 `workspace_file_recovery_apply` 永久保持关闭。现行 AI 写入不再改写原对象字节，而是把精确快照消费成一次性请求，由独立 helper 建立同目录候选、完整恢复意图并使用不覆盖的对象重命名。版本 2 恢复同样只能消费刚复核的票据。模型仍只能提出一份建议，不能调用 Tauri 或获得持久文件权限；网页勾选只是发起本次原生审查。

右侧文件面板先列所选目录的恢复记录，再由用户点击读取当前文件与原文的完整差异，损坏 UTF-8 以完整十六进制展示。刷新失败不保留旧列表，来源/目标身份变化、损坏记录和超量目录明确报错；不自动恢复、重试或给 AI 发送文件、路径和执行结果。绑定发行构建中的 AI 替换可正式生成版本 2 记录；记录含未加密的精确正文与路径，不属于业务数据库备份，当前不自动清理。详见 [桌面恢复与门禁](desktop-platform.md#绑定发行构建的按次单文件操作入口2026-09-22部分实现)。

确定性测试覆盖一份建议的逐次确认、动态能力、精确操作身份、取消、未知结果不重试、恢复票据单次消费及读缓存失效；原生测试使用隔离临时目录和普通子进程，不弹 UAC、不写用户项目文件。schema079、业务审批、终端手动和 Provider 外发边界不变；真实 Provider、已安装桌面与跨权限交互仍未验收。

版本 2 替换意图已接入同一恢复面板：人工点击后区分原对象、缺失目标、候选位置和冲突，优先展示缺失恢复或撤销回原文的实际方向；历史“原文 → 候选”差异单独折叠，空原文明确表示恢复后存在一个 0 字节文件。组件按项目选择与记录身份重建生命周期，迟到票据按旧选择释放。初步观察只核对身份与主文，不重验当前 ACL/附带元数据、不代表成功；只有下一步条件复核、独立原生审查和 UAC 都通过，helper 才可能执行。新用户占位文件不读取正文，内容不交给 AI。见 [版本 2 集成与限制](desktop-platform.md#版本-2-替换记录只读集成)。

## 项目文件修改建议与完整审查（2026-09-22，源码实现，仅审查）

在下面的单消息文件授权链之上，主聊天可接收模型针对**本次用户选中文件**的一份完整替换建议。建议生成或网页复核都不等于文件已修改；只有绑定发行构建中的动态能力、用户勾选、独立原生审查、按次 UAC、helper 全链核验与规范成功回执全部成立，UI 才显示已写入。

- 只有合法的本条 `project_files` 才注册 `workspace_propose_file_edit(path,base_sha256,content)`。保存和临时会话均可审查，普通工作台范围不能获得该工具，Headless 仍拒绝文件授权。必须精确匹配原路径和摘要，候选全文为无 NUL 的 UTF-8、最多 32 KiB；允许空文件替换，不支持创建、删除、重命名或多文件操作。无变化拒绝。每条生成最多一份建议，相同内容幂等，不允许模型覆盖已提出的建议；整个请求仍受原提示预算限制，不能以截断解决超限。
- 工具先产生不含正文的 `proposal_id/review_only/written:false` 回执，只有 Harness 接受成功且未取消的结果后才向当前 SSE 发出 `project_file_proposal`，字段严格为 `generation_id/proposal_id/path/base_sha256/content`。Sidecar 没有原生句柄或磁盘写权限；事件不是业务审批提议，也不保存到 generation 快照、消息或执行步骤。候选作为工具参数仍在本条模型上下文中，模型回复自行引用的内容遵循原历史规则。幂等重放/轮询恢复不会重放审查事件。
- 前端发送时消费外发许可，把原生基线转交给单个内存审查槽，而不是立即释放；会话、Provider ID/版本、精确全文、期限、request ID 和 generation ID 全部匹配才接收。仅正常收到同条 `done` 后展示完整差异；失败、取消、断流、身份不符、替换文件、新消息、切换会话/模型、卸载、丢弃或原期限到期都释放对应基线，不从历史恢复，也不继承下一条授权。当前 UI 仍只选择一个文件。
- 主聊天卡显示来源模型和相对目标，使用线性、有界的完整上下文替换差异；公共前后文保留，中段按整块增删显示（不保证最小 diff），全文不折叠省略。BOM、CRLF/LF、制表符、双向等不可见控制字符以转义形式展示，候选仅作文本，不执行 HTML。显示前后字节数、丢弃按钮与“复核当前文件（不写入）”。
- 人工“复核当前文件”只调用原生精确校验，不写文件。文件变化/不可读后失败关闭，旧建议不能再次通过；迟到结果不能复活已丢弃或替换的审查。动态 helper 可用时，额外勾选才调用 `workspace_file_edit_apply`，该命令单次消费快照、登记全局 operation 租约并进入独立原生审查/UAC/helper 链。取消会消费旧审查；其他失败按结果未知，要求查看右侧恢复记录且禁止重复提交。关闭写入中的卡片不等于取消，显式“取消本次操作”才发送匹配 root/operation 的取消请求；取消不能回滚已 started 的操作。

代码：`ai_project_file_proposal.go`、`ai_generation_execution.go`、`aiProjectFileReview.ts`、`AiProjectFileReview.tsx`、`workspace_file_snapshot/edits.rs`、`file_helper_image/launch.rs`。不增加数据库迁移或模型 scope，schema079 不变；前端、Sidecar 与桌面壳须配套。确定性验证覆盖服务端限制、SSE→聊天 store 生命周期、跨身份/期限/冲突、动态能力、完整差异、显式提交/取消和未知状态；真实 Provider、可见原生 UI、UAC/SACL 与安装包不属于该门禁的通过项。

## 精确项目文件的单消息分析（2026-09-22，已接源码）

桌面端右侧文本预览增加“加入本条消息文件上下文”：用户逐个点击后才调用原生完整读取，不使用旧预览缓存或截断文本；工作区最大化/窄屏时显露主对话。主聊天显示同一已选目录中的最多 4 个相对路径、每份字节数、合计字节数和每份可展开的完整外发内容；不允许混用目录，合计上限 32 KiB。可逐份移除或重新读取；加入、替换或移除任何一份都会使整组旧授权失效。用户在已存在的保存或临时会话里点击“仅授权下一条消息使用这些文件”，随后仍需手动发送；选目录或文件本身不授权。这是显式的一次消息附件组，不是模型可自主遍历目录的工具。

- 授权冻结当前会话、Provider ID/版本和整组快照实例，有效期取各基线中最早截止时间（最多十分钟）。切换模型/会话、撤销、加入/移除/替换文件及卸载主聊天使对应授权失效；在途读取/校验的迟到结果不能复活旧授权。发送前逐份原生复核；任一基线变化、读取失败、过期、授权不匹配或校验期间草稿/上下文改变时不发送任何一份。已选择但读取失败也须重新读取或显式移除，不能静默不带附件发送。
- 授权在发起发送时消费，包含未确定是否接收的网络结果；不会随下一条消息继承。原始内容放在独立 `project_files` 请求字段，而非用户草稿、浏览器存储、`AIMessage.Content` 或 `ContextSnapshot`。本条 generation 内工具轮次、自检、预算收尾和协议映射都保留这个独立上下文层，参与原来的 64 KiB 总请求预算，不靠截断放行。原始附件不从历史重建；**模型回复仍可能引用文件内容**，其保存、摘要与后续使用遵循该会话原有规则。正文可能含路径/密钥，不自动脱敏，确认卡明确提醒；“不附带根路径”不代表删除正文里已有的路径。
- `POST /api/v1/ai/chat` 新增可选 `project_files:{provider_id,provider_version,session_id,confirmed,expires_at,files:[{path,content,sha256}]}`。服务端核验显式确认、同一真实 Provider/版本和会话、有效期限、规范相对路径、SHA-256、有效 UTF-8/无 NUL、路径去重及有界数量/字节；受理前事务再次检查过期。接口与 UI 均最多四文件/合计 32 KiB；原生 256 KiB 基线上限不等于模型可用预算，与其他上下文组合过大时仍拒绝。文件必须由用户重新选择，不能用截断文本伪装完整文件。
- Headless 续办拒绝此字段；不注册目录读取/写入工具或额外工作台 scope。服务端不能据客户端请求证明原生磁盘事实，只把已提交内容作为本条不可信资料，原生目录/快照标识不进入请求。已接收请求按原幂等键恢复，不再次调用模型；未通过验证不写入 generation 或发送给 Provider。错误码为 `AI_PROJECT_FILES_CONSENT_INVALID/PROVIDER_CHANGED/EXPIRED/INVALID/TOO_LARGE/HEADLESS`。

前端、Tauri 壳和 Sidecar 需要配套构建；旧 Sidecar 会因未知请求字段拒绝，而不是忽略附件。无数据库迁移，schema079 不变。确定性测试覆盖主页面发送、权限失效、双协议序列化、请求预算、自检上下文及保存/临时会话不持久化/不继承原文件。修改提议与完整差异审查已由上节补齐，真实 Provider 质量和原生交互尚待验收。模型自主按目录读文件仍未开放；逐次批准的单文件写入/恢复只在绑定 Windows 发行构建动态可用，真实跨权限验收仍待。终端保持手动，不能将此链记成通用文件系统操控能力。

### H5-G45 同消息多文件显式上下文（2026-09-23）

文件预览可逐项把最多四个同目录的精确完整文件加入当前消息，上限为合计 32 KiB。聊天卡显示每份完整外发内容、相对路径及大小；用户可逐份移除或重新读取。任何选择变化都会创建新的 bundle 身份并撤销旧授权，Provider/会话/版本保持原绑定。发送时并行重验整组快照；任一读取失败、过期或内容漂移都阻止整组请求，不产生部分附件。删除/卸载会释放全部相关快照。

模型仍只可针对一份已选文件提出单个完整替换建议；建议按精确路径和 SHA-256 锁定对应基线。用户复核、Windows helper、按次 UAC 及恢复链仍一次处理一个文件，不会因同时提供多个文件而批量写入。没有目录扫描、自动读取、自动发送或权限范围扩展。Sidecar 契约本已支持最多四文件/合计 32 KiB，因此本阶段没有 API、数据库、schema、Runner wire 或迁移变化。

## 项目文件读写链基础层（2026-09-22，部分实现）

用户已确认显式目录、读取前模型外发说明和每次写入前完整差异审批的边界。基础层提供本机 Windows 原生精确文件基线：有效 UTF-8 原始字节、SHA-256、根目录/文件身份、十分钟有效期与变化后失效；页面、单消息外发、模型修改建议和绑定构建的人工单文件执行已接通，仍没有模型文件写入工具。详见 [桌面基线契约](desktop-platform.md#项目文件精确基线2026-09-22内部基础能力)。选择目录、获取或复核快照均不授予模型权限，旧预览交接不自动升级为编辑授权。

会话与 Provider 绑定、单消息外发授权、修改建议和完整差异审查见前两节；绑定发行构建可在后续独立原生确认与 UAC 后执行，普通开发构建只读，终端仍仅手动操作。基础层没有新增 scope 或 schema 迁移，不能把确定性测试当作真实跨权限端到端验收。

## 操作提议的可恢复限制反馈（2026-09-21）

`workspace_propose` 的工具错误现在区分 `AI_ACTION_PROPOSAL_LIMIT`（本回复已有八项，保留原建议且不执行，停止本轮提议并在用户处理后用新消息重新授权）、`AI_ACTION_CONVERSATION_UNAVAILABLE`（不再是活动的保存会话，不在当前运行重试）和 `AI_ACTION_PREVIEW_TOO_LARGE`（未保存建议，缩小操作范围或到工作台处理，不截断必要证据或原样重试）。相同指纹重试仍在数量检查前返回既有卡片。上限、审批、工具轮次和权限不变；未知数据库错误仍只返回通用信息，不暴露 SQL。产出证据沿用专用 `AI_OUTPUT_PREVIEW_TOO_LARGE`。这是工具反馈契约，不新增公开 HTTP 路由或数据库迁移。

## 收件箱拆分的精确历史回执（2026-09-21）

确认 `inbox.split` 时，领域命令真实返回的 1–20 个 Task ID 按草稿顺序写入同事务、不可修改的 `ai_workspace_action_confirmed` 工作流事件。审批响应及后续同会话模型回执新增可选 `created_task_ids`，人工卡片提供逐项任务入口并保留返回会话；模型侧不附带标题、描述、人员资料或整个事件。回读核对审批、会话生成、指纹、结果身份/版本、时间、数量和 UUID 唯一性，不扫描当前关系猜历史结果。

旧审批缺少该数组时保持缺失，界面明确说明无法恢复精确对应。任务以后改名、解除关联或删除不改写历史 ID；链接打开后仍须查询当前事实，不能据此认定任务存在、完成或沿用旧版本。下一步操作仍需本条授权及人工确认。事件写入失败与任务创建一起回滚，重复确认沿用原结果。数据库/API 版本不变，无迁移；审批事件延续现有审计留存，业务导入不恢复 AI 会话授权。本地运行中的旧二进制尚未加载此能力。

## 工作台授权的分层说明（2026-09-21）

16 项能力默认展示短名称与范围摘要，完整边界可通过“完整范围与风险”展开；选中的能力及尚未手选的敏感文件推荐自动展开完整说明。查询结果发送到所选模型、可能离开本机、仅下一条消息有效等提示继续常驻。复选框名称不再拼接整段说明，摘要用可访问描述关联。展开说明不会勾选或授权；依赖联动、文件手选、临时会话限制及最终授权按钮保持原有行为。此次只调整授权展示，不新增读取、写入或执行能力。

## 模式切换的窄屏可访问名称（2026-09-21）

工作台与智能体切换按钮保留固定可访问名称及原生悬浮提示；窄屏或收起导航隐藏文字后，图标仍可被辅助技术辨认。当前模式继续由 `aria-pressed` 表示，工作台返回位置及会话路由行为不变，无 API、权限或数据结构变化。

## 智能体页面按路由加载（2026-09-21）

`/ai` 与 `/knowledge` 页面代码改为进入路由时加载；应用外壳、全局生成状态与停止入口继续常驻。加载中显示页面提示，导入失败显示可重试状态；快速切换时旧导入完成不会覆盖新页面，也不显示上一页内容。未改变会话、授权或业务 API。构建测得主入口 JS 从 1887.95 KB 降至 1824.59 KB（gzip 从 515.92 KB 至 496.70 KB），页面分块另行请求；这是生产构建体积变化，不代表已测得启动耗时改善。共享组件仍在入口中，整体包体优化尚有空间。

综合回归补记（2026-09-21）：完整权限建议及精确标签的说明曾导致联合工具请求超出预算，现已压缩重复文字，所有工具及权限边界保留。双协议既有目录预算测试恢复通过，64 KiB 上限未调整；固定包络测量仍不替代真实长上下文验证。前端全层 3807 项测试与构建通过，详见阶段计划的综合回归记录。

## 标签交接按身份精确读取（2026-09-21）

`workspace_task_options` 新增可选 `id`，只用于 `type=tag` 的规范 UUID 精确查询，与 query/limit/offset/role 互斥。返回原 items 元数据结构中的唯一当前标签，缺失明确报错，不回退到同名候选；work 权限、版本、颜色及无业务写入边界不变。管理标签的实际交接先加载 tasks_projects 指南，再以 Tag ID 查询，标签改名不再导致旧名称搜索失败。列表查询仍保留原分页；API v1/schema079 无新增迁移。定向后端测试覆盖精确读取、非法/缺失身份、参数互斥和权限，前端交接及目录测试通过；运行服务须加载新版二进制后生效。

## 补权限时保留下一条所需的完整范围（2026-09-21）

升级与恢复：schema079 在既有 destructive migration 备份门禁之后、单一迁移事务内重建请求表，把 JSON 数量上限从 3 调整为 16、字节上限从 256 调整为 512；逐字保留 generation、scope JSON、open/dismissed/consumed 状态和时间戳，恢复原不可修改和状态迁移触发器及级联删除约束。失败由事务回滚，重新启动走原门禁；需回退时使用升级前完整备份和匹配版本，不能直接用旧二进制读取新增长清单。当前开发库已在本轮启动时通过该门禁升级到 schema079，并生成回滚备份；新消息权限校验及“至多三项新增缺失”由当前 generation 的服务端 policy 执行，账本不是 grant。

权限请求现在可以携带下一条消息所需的完整范围：服务端按当前冻结授权重算，必须有 1–3 项缺失范围，其余只能是仍需使用的当前范围，总数不超过现有 16 项闭集。全部已授权或超过三项缺失仍拒绝。例如本轮有 work+actions，补充 clients 时可建议 work+actions+clients，避免新消息只选 clients 而失去任务操作能力。建议本身不修改当前 Registry、不继承 grant；用户重新核对并确认下一条，output_files/agent_project_files 仍按原规则手选。SSE、保存请求和刷新回读支持完整清单，旧 1–3 项请求兼容；schema079 通过受备份门禁保护的表重建扩大限制，保留旧请求全部状态，但旧前端不能读取超过三项的新建议，前后端须配套更新。

## 权限续办等待期间的聊天状态变化（2026-09-21）

主聊天和右侧聊天共用的权限请求卡，在用户点击“停止自动续办并核对权限”后，会等待停止结果再准备草稿。若等待期间聊天变为不可操作（例如开始生成）或请求卡已卸载，迟到结果只更新已停止的续办缓存，不再追加草稿或打开权限面板；恢复可操作后，仍需用户重新点击。原有会话/请求身份和新版续办检查继续生效，正常路径使用最新回调准备草稿。停止失败仍保留请求供重试，不产生授权或自动发送。两项先失败后通过的回归覆盖不可操作与卸载，完整权限卡 11 项测试通过；此为前端时序修复，API、schema 和范围不变。

## 工作台交接按领域给出最小授权指引（H5-G21，2026-09-21）

后续调用链复核：`AiWorkbenchHandoffButton` 当前仅有组件及测试引用，实际七类详情页使用 `aiIssueHandoff.ts` 的专用 helper；下文通用入口增强不能单独作为页面体验已改善的证据。现已在实际 Task、Project、Client、Inbox、Reminder、Content、Milestone 以及任务责任分派 helper 中加入先加载对应 `workspace_guide` 的指引，避免直接要求使用尚未投影到模型目录的领域工具。Task 需要产出时使用 `tasks_projects + outputs` 联合指南；预算拒绝时提示分别加载，并遵守切换后目录替换语义。实际 Task 入口保留原 `work+outputs+actions` 推荐范围，其他专用入口原推荐范围和人工发送/确认行为不变。此处验证证明草稿及页面交接行为，不能证明真实模型一定按提示执行。

任务、项目、客户、收件箱事项、本地提醒、内容条目和路线图里程碑的通用“交给智能体”入口，仍然只在本地暂存规范记录身份、固定 route、推荐的最小 scope 与草稿提示，进入 AI 页后仍须人工选择权限并手动发送；现在草稿会按对象明确要求先读精确 `workspace_get`、再调用对应的 `workspace_guide`，并写明各领域的完成条件和禁止越界。Task 提醒提交、Agent Run 与人工验收不同，只有用户下一条另选 `outputs` 才可复查提交；启动/重试/停止 Agent 还需独立的 `agent_execution`。Project 不会连带修改 Task/客户/财务，Client 不会外发消息，Inbox 的拆分不自动运行 Agent、强制解决也不是普通解决的降级，Reminder 不伪造通知，Content 外链不抓取或发布，Milestone 不改 Project/Task 或自动验收。

该入口仍不复制页面正文、附件、文件、来源 payload、联系人/备注、财务资料、旧 grant、Provider 或会话；它也不创建业务写入、权限或模型调用。推荐 scope 未因更详细的引导扩大：仅 Client 带 `clients`，其他基础入口保持 `work+actions`。实际工具、单消息授权和每个操作的人工确认继续由既有 Harness 校验，API v1/schema078 不变。确定性前端测试覆盖七种记录身份、route、最小 scope、领域指南和关键边界；真实 Provider 的遵循质量仍需专项验收。

## 高负载跨领域指南按授权预算放行（H5-G20，2026-09-21）

`workspace_guide` 的 `tasks_projects + outputs` 和 `tasks_projects + agents` 不再对所有授权一律拒绝。Sidecar 按**当前消息实际授权**所注册的完整工具定义、服务端系统提示及本次联合指南结果，分别用当前 Provider 协议计算下一轮请求体大小；预留 8 KiB 历史操作回执和额外 4 KiB 空间后仍在 64 KiB 上限内，才接受联合指南。典型 `work+outputs+actions`，以及追加 `agent_execution`、受控文件范围的已测组合可一次装载任务与产出/执行定义；包含更多 UI、浏览器、知识、财务等范围的较大组合仍可能被拒绝，并提示缩小**下一条**消息授权或分别读取。拒绝不改变当前模型目录。工具实际执行仍核对原授权，操作仍要逐项人工确认；这不增加任何 scope、自动执行或数据读取，API v1/schema078 不变。

预算预检是固定边界估计，不保证任意长用户消息、额外业务上下文或后续工具结果都装得下；现有运行内提示窗口与 64 KiB 硬上限继续最终把关。确定性测试覆盖窄/中/全授权、两个高负载组合、拒绝不改变目录、成功接受后双协议请求体以及必要工具仍可见。真实长上下文和 Provider 质量仍待专项验收。

## 跨页面生成提示精确返回（H5-G19，2026-09-21）

应用级生成状态提示在离开 AI 页后不再笼统返回 `/ai`：若活动生成由后端标记为保存会话且 Session ID 是规范身份，“返回会话”打开 `/ai?session=<id>`，由 AI 页现有路由选择该对话。在 AI 页看着其他会话时，同一入口也可切到正在生成的目标；其他窗口发现的已保存生成另有逐条“查看会话”链接。临时生成、畸形 Session ID 或尚未回读到对应后端生成记录时，只保留通用 AI 入口或停止操作，不伪造可恢复的深链。链接只包含 Session ID，不包含生成内容、工作台授权或 Provider 凭据；不会重新发送、停止或改变生成。原全局停止与后台回读保持不变，API v1/schema078 不变。确定性组件测试覆盖跨页面、AI 页内切换、其他生成、临时与畸形身份；真实桌面路由仍待人工验收。

## 跨领域联合指南与工具目录（H5-G18，2026-09-21）

`workspace_guide` 现在可选填一个与 `topic` 不同、且属于**本条已授权范围**的 `related_topic`。例如用户要求同时核对任务与客户回访时，模型一次读取 `tasks_projects + people_clients`，下一轮便同时获得两组已授权查询定义及原有 `workspace_propose`（若本条确实拥有相应操作范围）；不用为第二领域额外消耗一次指南轮次。单领域旧参数和响应保持可用；新指南仍**替换**上次加载的领域辅助定义，不累计无限工具目录。结果中的两个领域、规则正文与工具名称在成功执行后由服务端重新计算并核验；执行超时、取消、伪造或未获权的第二领域均不能改变当前模型目录，完整已授权 Registry 和实际执行权限不变。指南不查询业务数据、不授予 scope、不批准或执行任何操作。

64 KiB 模型请求硬上限仍适用。H5-G20 已把两个高负载组合的固定拒绝改为按当前授权预算预检；最大授权、20 个知识来源与有界结构化回执仍会被拒绝，较窄授权可放行。真实长对话仍须通过原运行内窗口门禁，不能把固定边界测量当作任意上下文都保证可装入。确定性测试覆盖双协议任务/客户跨模块读取后生成**待人工确认**的新建客户建议（没有直接创建）、目录投影、篡改/越权拒绝与请求体预算；真实 Provider 的选用质量与原生桌面体验仍待验收。API v1/schema078 不变。

## 发送前显示本条工作台授权（H5-G17，2026-09-21）

主对话和右侧聊天的发送区现在显示**已人工确认、对当前 Provider 与会话仍有效**的本条工作台授权范围：逐项列出所有已选能力与总数，并提示“发送后不继承，操作仍需逐项确认”。弹窗内尚未确认的草稿不显示为授权；没有有效单消息授权时不显示清单。消息受理后原有清除逻辑使清单消失，下一条不会继承；切换 Provider、会话或 Provider 版本时原有失效逻辑同样不展示旧授权。点击原“工作台权限”入口仍可重新核对与修改；此处只是本地展示，不增加 scope、默认授权、API、迁移或业务操作权限。确定性组件与主/侧聊天测试覆盖显示、无授权和发送后清除；真实桌面视觉与实际 Provider 行为仍待单独验收。

## 工作台权限的常用选择与风险分组（H5-G16，2026-09-21）

主/侧聊天共用的“授权本次工作台能力”弹窗将原有范围按“常用查询与定位 / 操作建议与执行 / 敏感内容与文件”分组，保留每项完整说明和逐项复选框。顶部四个快捷选择分别准备 `work`、`work+workspace_ui`、`work+outputs+workspace_ui` 和保存会话可用的 `work+actions+workspace_ui`；点击只**替换弹窗内待选草稿**，不会立即授权或发送消息。最后仍须用户核对 Provider 外发说明、完整范围并点击“仅授权下一条消息”，且发送后不继承。快捷选择不包含财务、客户、知识正文、产出文件正文、Agent 执行或受控输入文件；这些能力仍只能逐项手选，Agent 执行和业务写入另需原有人工确认。临时会话禁用“准备操作建议”快捷选择，既有持久会话限定不变。切换快捷选择还会清除旧知识来源草稿，避免随后重新勾选知识范围时沿用隐藏的旧来源。没有新 scope、API、迁移或授权默认值；确定性组件测试覆盖分组完整性、草稿替换、敏感范围不自动加入及临时会话门禁，真实窗口布局仍待人工验收。

## 独立执行续办的已核验重新执行去重（H5-E38，2026-09-21）

`GET /api/v1/agent-runs?attention=1` 除 H5-E37 的精确重试链外，现在还排除已被**真实、可校验的同 Task“按当前事实重新执行”Run**替代的旧失败、中断或结果保留来源。服务端在同一只读事务中逐项校验 `agent_run_restarted` 的唯一事件、规范身份、原 Run 冻结终态、当前新 Run 执行快照哈希及事件时间；只有完整证明通过，来源 ID 才进入需处理投影的排除集合。事件 JSON 单独声称 `source_run` 不足以隐藏旧 Run；损坏、重复或错配证明使需处理读取明确失败，不伪称队列完整。筛选在权威 `meta.total` 与分页前应用，并可与 `status`、`plan_link=unplanned` 组合；全量运行账本、Task 历史和精确详情继续保留各次 Run。连续“重新执行→精确重试”只让当前待处理尝试占角标。全局 `active_total/pending_delivery_total/succeeded_total` 仍是未筛选的运行计数，不代表续办角标。这只是读模型变化，不自动重跑、取消、恢复登记、提交或验收，不复制旧输入/结果或授权。API v1/schema078 不变；确定性 API 测试覆盖四种终态、过滤/分页、混合链和损坏证明，真实 Provider/桌面交互仍待验收。

## 独立执行续办的精确重试去重（H5-E37，2026-09-21）

`GET /api/v1/agent-runs?attention=1` 的需处理投影现在只计精确重试链中仍需处理的最新尝试：如果失败或中断的 Run 已有同一 Task、`parent_run_id` 指向它且 `attempt` 恰好递增 1 的直接子 Run，父 Run 不再占用续办列表及角标。该过滤先于 `status`、`plan_link=unplanned` 的计数和分页；计划内 Run 仍按原规则排除。无 `attention=1` 的完整运行账本、Task 历史和 Run 详情继续保留旧失败记录。独立“按当前事实重新执行”不靠未核验的事件载荷推断为精确重试；其另有已核验凭据去重规则见 H5-E38。UI 复用服务端 `meta.total`，无需另存状态；这只消除重复提醒，不自动重试、取消、恢复、验收或授予权限。API v1/schema078 不变；确定性 API 测试覆盖计数、筛选、分页和历史保留，真实 Provider/桌面体验另验。

## 受确认的任务保存视图定位（H5-E36，2026-09-21）

单消息同时具备 `workspace_ui+work` 时，`workspace_request_record_navigation` 的闭集现在包含 `task_saved_view`。模型只能提交已查询到的规范视图 ID；Sidecar 再查 `task_saved_views` 存在性，工具结果和当前 generation 的导航事件仅携带 type/id，不带视图名称、定义、任务结果或模型指定的 route。前端显示本地确认卡，用户点击后才进入固定 `/tasks?task_view=<id>`，并保留可用的 `return_session`。任务页重新读取当前保存视图列表并按精确 ID 应用当前定义，读取期间不展示旧列表或未筛选任务；视图已删除或读取失败时明确提示并停用任务列表查询，失败可重试。筛选预设的应用不修改任何 Task，也不把结果自动回传模型。`workspace_task_views` 的普通读取和 `task_view.*` 人工审批仍独立；无新 scope、业务 API 或迁移，API v1/schema078 不变。确定性 Go/前端测试覆盖权限、规范 ID、失效视图、确认与返回链；真实 Provider/桌面交互仍待验收。

## 文件权限建议在授权面板中显式待选（H5-G15，2026-09-21）

智能体通过 `workspace_request_access` 建议 `output_files` 或 `agent_project_files` 后，主/侧聊天仍只准备下一条消息、打开原单消息授权面板，不自动发出消息或授予范围。这两项敏感文件权限本来就不会被推荐自动勾选；授权面板现在明确列出仍未勾选的建议项，并在对应复选项处标示待核对。用户亲自勾选后提示消失、既有依赖范围按原规则展开；不勾选时不会暗中加入文件权限。临时会话请求跨任务文件时，面板还说明该范围只在保存会话可用。手动打开普通权限面板不会残留上次建议提示。Provider 外发披露、文件二次确认、现有 scope 与审批门禁、API v1/schema078 均不变。前端确定性测试覆盖两项手选提示和不误选；真实桌面视觉仍待验收。

## 单次工具结果超限不再伪装成功（H5-G14，2026-09-21）

Harness 原先把超过单工具 64 KiB 上限的结果按原始字节截断，并以成功状态回填；截断可能破坏 UTF-8/JSON，也会让智能体误把残缺资料当完整事实。现在执行器只接受完整结果：超限返回无正文 `TOOL_RESULT_TOO_LARGE` 工具失败，执行步骤记录失败和零输出字节，模型在原有最多 3 次工具纠错预算内可缩小只读查询或核对当前事实。错误提示明确工具**可能已经改变状态**，不承诺可安全重试写操作；业务写入、审批与权限门禁不因失败回滚或放宽。恰好等于上限的结果仍完整返回，累计工具输出和模型提示预算保持原规则。API v1/schema078 不变，无新 scope、迁移、自动重试或 Provider 调用额度。确定性执行器与模拟模型回填测试覆盖；真实 Provider 行为仍待验收。

## 浏览器动作的本地执行回执与显式交接（H5-E35，2026-09-21）

H5-E31 的后退、前进、重新加载、停止加载请求在人工确认且原生 `browser_action` 无错误返回后，右侧当前标签显示本地回执：“命令已被接受”，不把它误写成网页加载完成或内容已读取。原生命令失败仍保留待确认请求和错误，供人工重试或拒绝；切换活动标签、开始新操作或出现新智能体浏览器请求时旧回执消失。回执仅属本次界面内存，不写数据库。

用户还可点击“把结果带入对话”，将固定的动作种类与“本机命令无错误返回、网页状态未知”说明暂存为交接卡；不携带 URL、标题、页面正文、截图、Cookie、历史、下载、原生标签 ID 或浏览器权限。聊天交接卡需再次由用户带入草稿、清除旧授权/上下文，再手动发送，模型才收到这条人工提供的无内容结果。此处没有自动工具回执或后台浏览器控制，不能把成功的命令调用当作页面加载、导航目标或业务状态成功的证明。API v1/schema078 不变；确定性前端/原生命令模拟测试覆盖，真实 WebView2 与 Provider 仍待专项验收。

## 右侧产出的原位智能体续办（H5-E34，2026-09-21）

H5-E33 的任务产出预览在完成新鲜读取、Artifact/Task/Submission 归属及未删除核验后，新增“继续交给智能体”。用户点击后仍留在同一 `/ai` 对话；若右侧已最大化则恢复聊天区域，窄窗口则收起右栏以显露交接卡。仅把三个规范 ID 和固定的“重新读取当前事实”问题暂存到内存，不复制预览正文、产出名称、文件内容、路径或既有授权。主对话里的交接卡还须由用户点击“带入问题并选择权限”，清除旧授权和上下文后推荐本条 `work+outputs`；用户重新核对权限并手动发送，模型才能读取它被授权的当前产出事实。文件正文仍须独立 `output_files`，交接不会自动下载、验收、提交或执行。读取中、失败、错配、删除与无效身份均不显示交接按钮。原完整详情入口保持可用；API v1/schema078 和业务事实不变。确定性组件与交接测试覆盖，真实模型/原生桌面仍待验收。

## 任务产出的对话内右侧预览（H5-E33，2026-09-21）

H5-E32 的单项产出确认卡现有两个明确选择：启用右侧工作区时，用户可点击“右侧预览”，在智能体模式保留当前对话并打开一个可关闭、可复用、可分栏的任务产出标签；也可点击“打开完整详情”，沿原深链进入精确提交批次页。右侧工作区在设置里被关闭时不提供不可见的预览按钮，完整详情仍可用。标签以已核验的 Artifact ID 唯一标识，复用时再次核对 Task/Submission 归属；达到 24 个工具标签上限或身份冲突时保留原确认卡并显示错误，不伪称已打开。

标签只在人工点击后调用既有精确 `GET /api/v1/artifacts/:id`，每次挂载要求新鲜读取并在显示正文前核对返回的 Artifact/Task/Submission 三重身份及未删除状态；加载、失败、归属不符或删除时隐藏缓存正文，允许用户手动刷新。文本和结构化产出在本地只读展示，链接只显示文本、不自动访问外部网址，文件仅显示元数据；完整详情页仍提供独立的人工下载。预览不把内容回传模型、不创建写入权限、不提交或验收。标签为应用内临时状态，刷新后不恢复；API v1/schema078、单消息 `workspace_ui+work+outputs` 与 H5-E32 服务端校验均不变。确定性确认卡、预览身份/错误和工作区标签测试覆盖；真实模型与原生桌面交互仍需人工验收。

## 任务产出的受确认精确定位（H5-E32，2026-09-21）

`workspace_request_record_navigation(record_type=task_artifact,record_id)` 现在可在本条消息显式授权 `workspace_ui+work+outputs` 后请求打开某项真实、未删除的 Task Artifact。模型只能提供规范 Artifact ID；Sidecar 联结 Task、Submission 并核对二者归属一致，只在当前聊天 SSE 向本地界面发送服务端推导的 `task_id/submission_id`。工具回执不含这两个归属 ID、名称、正文、文件路径、哈希或路由。用户确认卡不会自动跳转；点击“打开完整详情”才进入精确提交批次页并展开指定产出的只读卡片，H5-E33 另提供留在对话里的右侧预览。完整详情页重读当前批次与产出详情，不显示错误归属或旧缓存的正文；指定产出已删除或不在该批次时明确提示，不改为其他产出。文本与结构化内容按已有产出卡片预览，文件仅展示现有元数据，下载仍需用户另行点击；查看、下载都不是提交或验收，也不把内容自动回传模型。`output_files` 仍是模型读取受控文件正文的独立授权。API v1/schema078 不变；真实 Provider 与桌面交互仍需人工验收。

## 当前浏览器标签的逐次确认操作（H5-E31，2026-09-21）

独立单消息 `workspace_browser` 除 H5-E14 的新网址请求外，现注册 `workspace_request_browser_action(action)`，仅接受 `back|forward|reload|stop`。Sidecar 不读取浏览器状态或直接操作 Chromium；当前聊天流最多发出一个浏览器请求（新网址与标签动作共用名额），动作 SSE 只有 `generation_id/action`，工具回执只说明已请求、仍需人工确认，不自动回传标签 ID、地址、标题、网页内容或执行结果。Web 严格解析事件，展开右侧浏览器，在本地显示当前活动标签地址和动作；用户点击确认后，桌面端先重新读取原生快照，确认活动标签身份/地址未变且动作仍可用，才调用既有原生 `browser_action`。拒绝、无活动标签、网页模式、能力已失效或标签切换均不执行；普通网页模式只能拒绝，不伪称控制外部浏览器。用户自愿带回无网页内容的操作回执见 H5-E35；此能力仍不提供网页读取、DOM 点击/填写、下载、任意 URL/网络或后台浏览器自动化，也不让上一条 scope 延续。API v1/schema078 不变，真实 WebView2 与 Provider 行为仍待专项人工验收。

## 智能体精确记录导航扩展（H5-E30，2026-09-21）

`workspace_request_record_navigation(record_type,record_id)` 在原七类业务记录、Agent Run 和提交批次之外，现可请求定位项目笔记、客户活动、客户回访、收支记录或发票。本条消息仍须显式授予 `workspace_ui` 及该类现有读取范围：项目笔记需 `work`，客户活动/回访需 `clients`，收支/发票需独立 `finance`；`workspace_ui` 单独不注册该工具。Sidecar 对规范 ID、对应 scope、真实记录与未删除笔记/活动重新核验；笔记所属 Project 与活动/回访所属 Client 仅由服务端读取推导，不接受模型参数提供的关系。工具回执只含 type/id 和“待用户确认”，不含归属 ID、标题、正文、金额、备注或 route。当前聊天 SSE 对这三类从属记录额外发送 `parent_id`，前端严格校验并用代码生成精确项目/客户详情地址；收支与发票按精确 ID 生成既有详情地址。所有类型都只在用户点击本地确认卡后导航，并保留可用的返回原对话入口；拒绝或生成结束不会自动离开对话。导航不读取详情正文回传模型，不授权写入、执行、付款、外部发送或文件操作；后续领域建议仍走独立权限和逐项审批。前端还给浏览器与记录导航请求使用逐次递增的本地身份，旧卡的迟到关闭不会误清新的请求。API v1/schema078 不变；确定性 Go、SSE、前端解析与确认卡测试覆盖，真实 Provider/桌面专项仍待。

## 已选任务返回断点（H2-W13，2026-09-21）

任务页人工点击“交给智能体（已选任务）”后，本次应用会话在内存中暂存所选 1–20 个规范 Task ID、当前筛选条件、页码和列表/看板视图。用户从交接卡返回 `/tasks` 时恢复原视图，并经既有 `GET /api/v1/tasks/:id` 重新读取全部选中任务；所有 ID 都与返回值一致才恢复勾选和最新 Task 版本，批量操作仍受列表读取稳定性、原有版本预检与二次确认约束。任一读取失败或身份不符则整组不恢复，提示用户重新选择。读取期间禁用选择、交接和批量写入，返回状态在完成后清除；应用重载不保留。此断点不是持久任务事实、模型上下文或授权，不复制标题/正文/批量表单，也不自动发送或执行。仅前端行为，API v1/schema078 不变；确定性页面测试覆盖最新版本恢复与整组失败，原生桌面人工验收仍待执行。

## 已选任务精确成组读取（H2-W12，2026-09-21）

`workspace_tasks` 现增加与筛选/分页模式互斥的 `task_ids`：单消息 `work` 下接受 1–20 个规范、互不重复的 Task UUID，在同一只读事务按请求顺序返回全部当前任务的有界标题、状态、优先级、类型、计划/截止时间、版本、更新时间和路由。精确模式标题最多 100 字，过长以 `…` 明示截断，使满 20 项即使遇到 JSON 转义也保留全部行并遵守 24 KiB 工具结果上限；筛选分页模式仍是原最多 200 字。任一任务缺失则整组失败，不提供部分结果；空、重复、超过上限、别名/重复键、`null`、与 `filters/limit/offset` 混用均拒绝。它和原筛选模式使用同一字段白名单，不返回任务描述、完成标准、客户资料、项目名称、产出或路径。返回值仍有 `items/total_items/has_more/next_offset/window_limited/as_of`，精确模式的总数为已请求集合长度且没有下一页；每次调用重新读取，不冻结未来的版本或写入许可。

任务页 H2-W11 的人工交接改用 `workspace_tasks({task_ids:[...]})` 首先读取完整选择和批量事实，只有需要当前责任时再用 `workspace_task_assignments`，详情用 `workspace_get`。此前逐项读取优先级和日期在 20 项选择下会过度消耗工具预算；本模式不增加 `actions`、自动执行、原生 API 或数据库迁移。实际修改仍须保存会话 `work+actions` 下提出 `task.batch_update` 并经用户逐项核对确认。API v1/schema078 不变；确定性测试覆盖顺序、实时版本、字段隔离、缺失/重复/混用拒绝，真实 Provider 选择质量仍待验收。

## 已选任务交给智能体（H2-W11，2026-09-21）

任务页的批量选择区现可由用户点击“交给智能体（已选任务）”，一次交接 1–20 个规范、互不重复的 Task ID。选择超过 20 项时只提示减少选择；读取未稳定、批量写入在途或生命周期二次确认尚未处理时不可交接。交接只暂存用户选中的 ID 集合、固定问题与 `/tasks` 返回入口，不复制标题、描述、当前行版本、批量表单目标值或原因；H2-W13 在本次应用会话返回任务页时重新读取全部 Task，成功才恢复选择，失败则要求重新勾选。进入主/侧对话后才由用户准备草稿、核对推荐的 `work+actions` 单消息范围并手动发送，推荐不是授权。

模型须先用 `workspace_guide(tasks_projects)` 核对规则，再用 H2-W12 的 `workspace_tasks({task_ids})` 在同一只读快照精确重读真实任务、版本、状态、优先级和日期；需要责任时另用 `workspace_task_assignments`，需要项目或正文等详情时按 ID 用 `workspace_get`。任一任务缺失时不得悄悄缩小选择集；页面选择也不是当前事实或批量写入许可。用户明确要求同一处变更时才可提出既有 `task.batch_update` 一张逐项预览卡，确认仍重验当前事实并由用户决定；不同变更分开建议。交接本身不写 Task、不分派、不启动 Agent、不验收，也不新增工具、scope、HTTP API 或数据库迁移。API v1/schema078 不变；定向前端测试覆盖精确身份、隐私、上限与人工点击，真实 Provider 选择质量和原生桌面仍待验收。

## 批量优先级与截止时间（H2-W10，2026-09-21）

工作台任务页与智能体现可对明确选定的任务集批量设置同一优先级或截止时间。保存会话的 `work+actions` 可用 `task.batch_update` 的 `set_priority`（`changes.priority=P0–P3`）或 `set_due_date`（`changes.due_date` 为含明确时区偏移的 RFC3339 时间，或显式 `null` 清除）提出 1–20 个真实 Task 的一张确认卡；先在本条 `work` 授权下读取各 Task 当前 ID/版本，用户只说“周五”却未明确时区/时间时应先询问，不能猜。提议保存前将时间规范为 UTC，逐项展示版本及前后值。确认事务重验完整预览和所有 Task 版本，任一失败整批回滚；与原生 `PATCH /api/v1/tasks/batch` 使用相同的字段校验、版本语义及 Task 事实。没有新执行能力、自动操作或 scope；成功批次回执仍为 Proposal ID/version 1 与 `/tasks`，不代表每个 Task 的新版本。API v1/schema078 不变，旧 action 与卡片兼容。确定性测试覆盖时区规范化、显式清除、非法字段、版本冲突、前端本机时间输入与卡片展示；真实 Provider 和原生桌面仍待验收。

## 已有任务成组责任变更（H2-W9，2026-09-21）

单消息 `work` 下，`workspace_task_assignments({task_ids:[...]})` 可在同一只读快照精确读取 1–20 个已有 Task 的 ID、标题、当前版本/状态/验收策略及活动负责人、审核人的安全身份元数据；多任务查询不提供历史分页、Actor 备注、联系方式或 Adapter 配置。重复、缺失或不合规 ID 整组失败，不返回部分结果。单任务 `task_id` 的现有当前/历史查询仍保留。候选 Actor 仍须由 `workspace_task_options` 按角色读取，不凭名称猜 ID。

保存会话的 `work+actions` 可用既有 `task.batch_update` 提出 `set_assignee/clear_assignee/set_reviewer/clear_reviewer`，对同一组 1–20 个 Task 执行相同责任变更。`items` 与 `expected_versions` 一一对应；设置需要真实 `actor_id`，四种动作均需原因。预览逐 Task 列标题、版本、责任前后值及 Actor ID/类型/版本，一张卡由用户确认。设置同一 Actor、结束不存在的活动责任或不合格候选会拒绝。确认事务重算完整预览，预检全部 Task 版本，随后复用原生 Assignment 创建/改派/结束命令；Actor/Adapter 资格漂移、Task/责任变动或任一写入失败则整批回滚，保留原领域历史、父链和审核规则。person 不收到消息，agent 分派不启动 Run；审核人仅 owner，且本操作不提交或验收产出。成功回执仍为批量 Proposal ID/version 1 与 `/tasks`，不是逐 Task 新版本。API v1/schema078 不变。确定性测试覆盖批量读取、混合首次分派与改派、结束、审核人、Actor 漂移及中途失败回滚；真实 Provider 选择质量和桌面体验仍待专项验收。

## 批量建任务的可选初始分派（H2-W8，2026-09-21）

`task.batch_create` 的每个草案现可选填 `assignee_actor_id`，须先在本条 `work` 范围内通过 `workspace_task_options(type=actor,role=assignee)` 读取真实可分派 Actor ID；仍需保存会话的 `work+actions` 才能起草并由用户确认一张完整卡。预览逐任务列明初始负责人及其身份版本，若该草案为 `manual`，还列明自动设置的 active owner 审核人；没有提供负责人时保持旧行为，不隐式设置审核人。普通 person 分派只写本地责任，不联系人员；就绪 agent 分派也不启动 Run、不继承 `agent_execution`。

确认事务重新读取项目、标签、Actor/Adapter 资格及完整预览。创建任务后复用原生 Assignment 命令写入初始责任、Task 版本和领域事件；任一项资格漂移、插入失败或审批事件失败，整批任务和分派均回滚。Actor 改名、状态或版本改变使旧卡失效。成功批次回执仍以 Proposal ID/version 1 表示审批批次，逐任务入口和历史 `created_task_ids` 不充当新 Task 的当前版本；若要启动 Agent，须另行读取当前任务/责任事实，取得独立执行授权及二次人工同意。既有未带分派字段的待确认卡和旧展示保持兼容。API v1/schema078 不变，无自动跨 Run 调度。确定性前后端测试覆盖初始责任/审核、候选漂移、非法身份、整批回滚及预览展示；真实 Provider 与原生桌面仍待专项验收。

## 同项目批量建任务确认（H2-W7，2026-09-21）

`workspace_guide(tasks_projects)` 已说明 `task.batch_create`：在本条保存会话同时具备 `work+actions` 时，先读真实 Project ID/version 与需要的标签，再提出 1–20 项有序新 Task 草案。`parent_key` 只可引用更早草案；所有 Task 都归入同一 Project，默认待办。操作只保存一张待人工确认卡，完整展示项目、层级、日期、说明、完成标准、标签及验收策略。用户确认时服务端重算预览并复用原生 Task 创建事务，任一漂移/失败整批无写入；成功后卡片逐个给出真实 Task 入口。单次回复原有最多 8 张卡的限制不变，批量卡不代表 Agent 执行或 Task 完成。后续 H2-W8 为草案增加可选初始分派，详见上一节；未指定负责人时仍按本节原行为。参见 [Task 领域约束](tasks.md#ai-同项目批量建任务h2-w72026-09-21)。API v1/schema078 不变；确定性前后端测试已覆盖，真实 Provider/桌面仍待验收。

后续同会话模型消息在原有最多 16 项/8 KiB 的有界操作回执内，**确认成功的批次**另含按草案顺序排列的 `created_task_ids`（最多 20 个 UUID），而不是只有批次 Proposal ID；待确认、拒绝和失败均不携带。旧批次超出窗口时仍须重新查询。该历史身份列表不含标题、草案、描述、标签或正文，不证明任务仍存在、版本仍为 1 或已经完成。继续分派、更新或执行前仍须在新消息中重新取得相应 scope，按这些 ID 读取当前 Task 事实和版本，形成新的人工确认卡；回执自身不继承权限、不自动触发下一步。

## 待确认操作工具按领域加载（H5-G13，2026-09-21）

`workspace_propose` 的完整参数定义约占最大授权组合首轮请求的 11.7 KB；原 H5-E28 只延迟领域查询定义，仍每轮预加载全部写操作。现在有 `workspace_guide` 时，首轮只显示通用查找、计划、指南等核心工具；模型先读取相关领域指南并成功通过 Harness 接受后，下一轮才显示已授权的 `workspace_propose` 及该领域查询定义。仅选择 `workspace_ui` 不加载业务写定义；切换业务领域仍可再次加载。不含指南的小型授权目录保持原样。

完整 Registry 与单消息 scope 不变，目录隐藏不撤销已授予的执行能力，也不增加新的能力；实际提议继续校验本条 `actions`、真实 ID/版本、领域参数、Provider 身份及人工逐项确认。指南失败、取消或结果不匹配不能改变目录，新消息不继承上一轮选择。固定最大范围、20 个知识来源和有界回执的双协议首轮测量从 37,486/37,058 字节降到 25,815/25,416 字节，含额外来源字段仍须至少保留 32 KiB 余量；仍受 64 KiB、8 轮和原工具预算约束，增加一次指南调用可能消耗一轮，不保证任意历史或结果都能容纳。双协议模拟 Provider 验证“指南→真实任务查询→待确认创建→人工确认”，真实 Provider 选择质量待验收。API v1/schema078 不变，无新 scope、迁移或自动执行。

## 等待中自动续办的显式跨重启恢复（H5-G12，2026-09-21）

现有有界自动续办新增默认关闭的“本地服务重启后继续”等待项选项。用户必须在创建授权时另行勾选，并在最终确认中重新核对 Provider、完整范围、轮数与时限；请求只有 `resume_after_restart:true` 且 `confirm_restart_continuation:true` 才接受。未选择者保持原有重启中断行为。授权响应返回 `resume_after_restart`，旧服务未返回该字段时前端按未启用处理；只有明确选择此项时才向服务端发送新字段，普通旧版创建仍兼容。

- 选择写进不可变 `ai_continuations.workspace_json` 授权快照，不进入普通聊天单消息 grant，不增加 scope、任意 Shell/浏览器权限、预算或时限，也不复制模型正文。旧授权缺字段等同 false；后端要求第二个独立确认标记，前端切换选项会撤销此前的总确认。
- 正常关闭与意外重启只保留 `waiting`、当前无 `current_generation_id`、明确选择了跨重启的授权；`running` 或带未结清 generation 身份的等待项均记为 interrupted，不重放不确定请求。原受理事务先将授权转为 running、冻结 generation 与扣次，之后才发模型请求；因此保留的等待项没有已受理但未知的模型调用。重启后 coordinator 立即重读期限、轮数、Provider/知识来源、完整计划/建议与待人工审批，再决定等待、停止或发起**下一**轮；没有凭授权快照盲目执行。
- 授权到期、Provider/范围/计划变化和无进展仍按原门禁结束。物理备份恢复属于不同数据时间线，启动时即使旧快照曾选择跨重启，也统一以 `interrupted/restore_pending` 撤销；准备恢复的现有冻结同样不变。停止按钮仍能撤销续办，停止不撤销已批准的 Agent Run。已有授权不会被迁移为可恢复授权。
- API v1/schema078 不变，无迁移；该选择是既有不可变操作态 JSON 的加法字段，旧二进制回退会保守中断而不是继续。隔离测试覆盖默认拒绝、单独同意、正常关闭/意外重启、活动请求不重放、到期/Provider 漂移及备份恢复撤销；真实进程崩溃、Provider 计费和原生桌面仍需专项验收，不能宣称完整断电恢复或任意跨 Run 自治。

## 运行内较早只读结果收缩（H5-G11，2026-09-21）

同一次 Harness 运行多次查询工作台时，即使单个结果和 64 KiB 工具输出总量都未超限，后续 OpenAI/Anthropic 模型请求仍可能超过 64 KiB 提示预算。现在每次请求先沿 H5-E26 移出本轮之前的完整旧对话；仍超限时，才按时间顺序收缩**本轮较早、已完成、代码明确允许重查**的只读工具结果正文。工作台读工具通过审查过的代码 compactor 生成至多 4 KiB 的确定性证据胶囊；不能安全生成或没有足够节省时使用固定“结果已移除、需要重查”标记。最近一组工具调用及全部结果始终保留，调用 ID/名称/参数与结果消息配对也保留；后续请求不会恢复已收缩正文。初始用户请求、独立授权上下文、已确认记忆和业务审批回执均不因此缩减。

- 当前仅 `workspace_search/get/today/tasks/roadmap_milestones/content_items/client_records/automations/finance/focus/focus_report/project_notes/project_outputs/agent_runs/outputs/task_submissions/task_assignments/task_options/task_views/inbox_tasks/inbox_source` 的较早结果可收缩。`workspace_propose/plan`、权限请求、UI/浏览器请求、执行资格与文件候选/正文、知识引用、记忆工具及未来未审工具不在允许清单内。即使当前调用恰好是 `workspace_plan.read`，也因该工具兼具写能力而不收缩。
- 证据胶囊是字段白名单式确定性提炼，不调用额外模型：固定 `version=1/kind=compacted_read_evidence/complete=false/stale=true`，绑定原参数与完整结果 SHA-256、原字节数，只从规范 JSON 中提取最多 8 组、每组最多 18 个身份/关系 ID、版本、状态、类型、标题/名称、时间、计数、哈希及分页字段；自由正文、说明、备注、摘要、文件内容和任意未知字段不保留，长字符串有界截断。胶囊不是完整事实或新鲜性证明，未列字段一律未知；完整/当前事实必须按本条权限重查。无固定证据、非法结果、胶囊超过 4 KiB 或节省不足时退回纯省略标记，后者不携带任何业务事实。若旧历史及允许的旧只读结果都不足以腾出空间，仍返回 `AI_PROMPT_TOO_LARGE`，不会收缩最近结果、审批或写入回执，也不扩大 8 轮/32 调用/64 KiB 提示与结果预算。
- 模型/自检实时步骤可选 `compacted_tool_results`（本次新增收缩条数，1–32）；前端只显示次数和“需重查”说明，不显示结果内容。成功回复另加固定中文披露，保存会话刷新后仍可见；无新增数据库列、授权、模型调用、迁移或 API 版本。活动步骤计数仍只在本次进程快照，终态步骤回读不保存逐轮计数。真实 Provider 对重新查询的遵循程度和原生桌面长任务仍需独立验收。

确定性证据：Harness `prompt_tool_result_compaction_test.go` 双协议验证旧只读结果收缩、证据胶囊参数/结果绑定、最新/操作回执保留、单调收缩及超限硬失败；API `ai_tool_result_compaction_policy_test.go`、`ai_context_window_notice_test.go` 验证允许清单、正文排除、4 KiB 上限、摘要身份、最终披露和有界进度；前端 `aiProgress.test.ts`、`AiRunProgress.test.tsx` 验证严格解析及可见提示。API v1/schema079 不变；它不是 Codex 的模型语义摘要，也不是跨 Run 自治恢复。

## 路线图结构化查询（H2-W4，2026-09-21）

单消息明确授予 `work` 后，智能体可先从 `workspace_guide(topic=roadmap_content)` 发现 `workspace_roadmap_milestones`，再按与原生路线图列表相同的 `year/quarter/status/project_id/include_archived` 筛选当前里程碑；缺省保持年、季度、手工顺序，显式 `sort=target_date` 才按目标日期和 ID 跨季度排序。工具每页最多 20 条、`offset` 最多 1000，返回完整匹配数、下一页偏移、窗口限制及单次只读快照时间。列表只含里程碑 ID、标题、年季、纯日期、状态、手工序、版本、关联项目数、由关联项目 Task 派生的进度和精确详情入口；不带说明正文、项目名单或 Task 正文。完整详情仍须 `workspace_get(type=roadmap_milestone,id)`，未读页不能冒充已检查计划。

这补齐“本季度/今年有哪些目标”的集合读取，不改变原路线图或 Task 事实；`project_count=0` 和任务数 0 不等于目标已达成。相对季度或年份须先明确用户当地日期，不能把服务器 UTC 当用户日历。任何 `roadmap_milestone.*` 修改仍需另有本条 `actions`、真实 ID/版本及逐项人工确认；列表、模型回复和计划文本均不执行写入。工具输入拒绝重复、未知、null、非法筛选/排序及越界分页；Provider 变化和恢复维护继续使用既有门禁。API v1/schema078 不变，无迁移；与原生筛选/排序一致性、隐私和双协议 Harness 发现→读取的确定性测试通过，真实 Provider/桌面体验仍待验收。[工具实现](../../services/sidecar/internal/api/ai_roadmap_milestones.go)。

## 权限请求与自动续办的一步式人工交接（H5-G10，2026-09-21）

主对话和右侧聊天的权限请求卡现在读取原会话的续办授权状态。若授权仍为 `waiting/running`，主按钮明确显示“停止自动续办并核对权限”；用户点击后只调用既有 `POST /api/v1/ai/continuations/:id/stop`，确认返回同会话终态、更新续办查询后，才把代码所有的固定续问与推荐范围放入原输入区并打开原单消息权限面板。停止请求在途时不能重复点击或忽略；停止失败、响应不符、切换会话/请求或出现另一活动续办时不准备新草稿，并保留开放请求供重试。没有活动续办时保持原“核对权限并准备继续”。

这一按钮明确授权的是**停止本次有界续办**，不是授予模型新 scope；准备后仍需用户核对权限详情、确认单消息 grant 并亲自发送。原请求只会在明确忽略或新消息被服务端实际受理时关闭。停止可能取消当前后台生成，但不会撤销已人工批准的 Agent Run、审批或业务写入，也不会自动重启续办。主/侧共用同一请求卡逻辑，服务端发送互锁仍作最终防线。API v1/schema078 不变，无迁移或新权限；确定性前端测试覆盖成功与失败路径，真实桌面交互仍待验收。

## 后台缺失权限的人工作业交接（H5-G9，2026-09-21）

已单独授权的有界后台续办若确实缺少下一步必需的工作台范围，可调用既有 `workspace_request_access` 请求最多三项本轮未获权的白名单 scope。后台没有浏览器 SSE 或即时弹窗；工具仅在当前 generation 的受控快照登记请求，并明确回执 `requested:true/granted:false/requires_new_user_message:true`。只有保存会话的 assistant 回复成功完成，才与消息、generation 终态在同一事务写入 schema078 操作态请求；失败、取消、进程退出均不产生可恢复请求。原授权范围、工具 Registry、轮数/期限和业务事实不因此改变。

请求完成后，Agent Inbox 的“权限待核对”只显示来源身份；主/侧聊天从原消息显示 scope 卡。H5-G8 门禁让原续办保持 `waiting/pending_approval`、不再自动消耗该请求或追加模型调用。用户若需提供新范围，先停止活动续办，再核对权限并手动发送新消息；若明确忽略，旧授权仍按原进度与无进展门禁判断，不保证再运行模型。请求本身绝不是授权、执行许可或计划完成。请求账本的 `open` 状态是待核对事实：正常新消息受理时会关闭旧请求，不能用同时间戳的随机 UUID 或可变的 SQLite 隐式 rowid 推断“最新”；聊天卡查找服务端仍开放的请求，不依赖末条消息的排列。API v1/schema078 不变、无迁移；双协议真实 Harness/本地模拟 Provider 与同刻顺序回归通过，真实 Provider 判断请求是否必要及原生桌面仍待验收。

## 后台续办等待权限核对（H5-G8，2026-09-21）

已完成的保存会话若有开放权限请求，后台计划续办现在停在 `waiting/pending_approval`，不启动新一轮模型调用，也不把请求当成普通用户消息消费。创建授权时即显示等待；定时扫描与真正受理后台生成的事务内都重查同一开放状态，避免扫描后、受理前的竞态。用户可从原会话或 Agent Inbox 核对并明确忽略；关闭请求后，仍有效的原有有界授权才可能继续。若要核对后发送带新 scope 的消息，须先停止仍活动的续办，再独立确认单消息权限并手动发送；停止不自动发送草稿，新消息不继承原续办授权。

这是现有 `pending_approval` 等待原因的扩展，不新增状态或迁移。H5-G9 允许后台只提出待核对请求，但仍不允许自行获得或扩大 scope，不自动确认、发送、执行、重试或验收；`open` 请求保持由请求账本持有，Agent Inbox 仍只显示来源元数据。API v1/schema078 不变。确定性用例先复现原自动轮次误消费，再覆盖等待零调用、零轮次、请求保留及人工忽略后的继续；真实 Provider/桌面场景仍待专项验收。

## 生成中忽略权限请求（H5-G7，2026-09-21）

权限请求 SSE 一旦发出，`POST /api/v1/ai/generations/:id/access-request/dismiss` 现在可在生成仍运行、请求尚未落库时返回 204。Sidecar 只接受本进程真实活动、已发出请求的 generation；未知 ID、无请求的生成仍 404。忽略意图只保存在该活动生成的受控内存快照中，立即从生成状态读取隐藏卡片，并与最终保存事务串行：若回复成功，事务先按 schema078 约束写 `open`，再原子改为 `dismissed`，外部看不到开放窗口；若失败/取消/进程终止，本来就不保存请求。已落库请求仍按原接口更新，重复忽略幂等。不新增 grant、业务操作或数据库迁移。

主/侧卡点击“忽略”立即请求 Sidecar；成功后清理当前窗口卡并刷新消息与续办队列，失败则保留卡片提示重试。热更新期间旧 Sidecar 对活动生成返回 404 时，前端暂沿用完成消息出现后重试的旧路径；若在此期间卸载页面，旧版仍有原有局限，重启到新版 Sidecar 后才具备上述保证。模型请求 scope、本轮权限和人工审批规则不变。确定性测试覆盖活动请求、重复忽略、完成后的关闭状态及零队列泄露；真实 Provider 与桌面交互仍待单独验收。

## 权限核对中保留续办请求（H5-G6，2026-09-21）

“核对权限并准备继续”只把代码所有的固定续问放进当前主/侧输入框、推荐本次 scope 并打开原单消息授权面板，**不再关闭开放请求**。用户关闭面板、调整范围、换页或暂不发送时，原会话卡及 H5-G5 队列仍可找到请求；重复点击不会重复追加相同固定续问。只有显式点“忽略”才调用 dismiss 接口，或者新消息在服务端实际受理时按 H5-G4 消费旧请求。准备不等于授权，确认面板也不自动发送；服务端的 grant、逐项操作审批和旧消息不继承规则不变。API v1/schema078 不变、无迁移；主/侧组件与卡片定向测试覆盖准备后保留及忽略关闭，真实桌面交互仍待验收。

## 权限请求进入续办队列（H5-G5，2026-09-21）

`GET /api/v1/ai/inbox` 新增 `kind=access_request` 和 `access_request_total`。只从 `ai_workspace_access_requests` 中列出**保存会话、生成已完成、请求仍 open** 的建议；新消息受理会消费旧请求，故不需要按生成时间/随机 UUID 过滤。`all` 总数与分页包含这一类。队列行仅有 `kind/id/created_at/session_id/session_title/generation_id`，不返回请求 scope、grant、消息正文、Provider、路径或业务对象。智能体左栏“权限待核对”点击后只打开原保存会话，由原会话仍开放的消息卡读取并展示请求、供用户决定是否核对；既不自动授权，也不准备或发送消息。用户显式忽略后，队列查询随消息一起失效重读；准备核对仍保留请求，下一条消息受理并消费旧请求后才不再列出。非保存会话、失败生成及已关闭请求均不可见。热更新期间若旧 Sidecar 的 `all` 响应尚无新计数字段，前端仅在旧七类计数恰好等于总数且无权限请求行时按零兼容；其余不一致响应拒绝。API v1/schema078 不变，无迁移；这是发现与导航，不是新的执行权限。确定性 API、解析与前端导航测试覆盖，真实 Provider/桌面体验仍待人工验收。

## 权限请求卡的保存与恢复（H5-G4，2026-09-21）

H5-G3 的交互式请求仍不是授权。schema **078** 仅把**保存会话且成功完成**的生成所提出的 1–3 个白名单 scope 记入 `ai_workspace_access_requests`；该操作态记录与 assistant 消息和 generation 终态同事务提交。失败、取消、非保存会话不落请求；不会保存授权、提示词、工作台正文、对象 ID、文件路径或 Provider 信息。`GET /api/v1/ai/generations/:id` 与 `GET /api/v1/ai/sessions/:id/messages` 只向当前仍 `open` 的请求返回 `access_request.scopes`，主/侧聊天刷新后可从最新完成消息恢复卡片；流中断的活动生成也可通过生成状态恢复内存中的请求元数据。

用户点击“忽略”会关闭本卡：前端调用 `POST /api/v1/ai/generations/:id/access-request/dismiss`，服务端把开放记录改为 `dismissed`，生成中请求按 H5-G7 提前记录关闭意图；两者都不授予权限、不发送消息。“核对权限并准备继续”按 H5-G6 保留请求。保存会话接受下一条用户消息时，同一事务把此前 `open` 请求改为 `consumed`；新消息是否授予范围仍由其独立确认的 grant 决定。只有仍开放的 assistant 消息请求会展示，旧卡不会跨新消息复活。表随 generation/session 级联删除，排除便携业务 JSON/ZIP，SQLite 一致性备份覆盖；schema 77 包按冻结排除清单可显式迁入 78，降级仍拒绝。真实 Provider 措辞、桌面交互仍待人工验收。

## 缺失工作台权限的显式请求（H5-G3，2026-09-21）

现在即使本条消息没有选择工作台范围，Harness 也只额外提供 `workspace_request_access`：模型最多请求三项**尚未授权**的既有 scope。严格白名单、去重和当前 scope 检查由 Sidecar 执行；工具回执明确 `granted:false`，不会为当前 generation 注册缺失的读写工具、写权限审计或创建业务建议。每轮至多发出一张本地 UI 请求卡；取消、失败或新消息会清除本轮卡片。工具结果、SSE 和卡片不携带工作台正文、对象 ID、路径或 Provider 凭据。

主/侧聊天只在来源会话显示代码映射的权限名称，不展示模型可控链接或指令。“核对权限并准备继续”仅添加一段代码所有的续问并打开现有权限面板；高风险文件范围仍须手动选择，其他推荐范围虽可预选，也必须由用户核对详情、亲自确认并再次发送。旧消息权限不继承，不能自动重发、后台授权或把请求当成已执行操作。H5-G3 阶段后台没有可交互桥接、调用工具会得到不可用回执；当前 H5-G9 已改为只在成功保存的自动回复中生成待核对请求，并由 H5-G8 阻止自动消耗。成功的保存会话请求卡按 H5-G4 规则恢复；实际工作台读取、写入、执行和外发仍按原 scope、Provider、保存会话及逐项确认边界。

H5-G3 原始桥接为 API v1/schema077、无迁移；H5-G4 的保存/恢复另见上节。OpenAI/Anthropic 本地模拟 Provider 覆盖零 grant 的工具可见性、请求事件与零业务写入；前端测试覆盖严格事件解析、主/侧会话隔离和“准备但不发送”。真实 Provider 是否恰当选择最小权限、桌面体验仍需人工验收。[Sidecar 工具](../../services/sidecar/internal/api/ai_workspace_access_request.go)、[权限卡](../../apps/web/src/components/AiAccessRequestCard.tsx)。

## 任务顺序的人工确认（H2-W3，2026-09-21）

保存会话在本条消息具备 `work+actions` 时，模型可依据 `workspace_tasks` 的真实任务身份与版本提出 `workspace_propose(action="task.move")`。输入只含活动源任务的 `task_id/expected_version`，以及 `changes.anchor_task_id/expected_anchor_version/placement(before|after)`；两任务必须属于同一精确 `planned_date` 组，未排期为 `null`。不能借排序改变任务日期、状态、正文或跨组移动。

服务端按任务页原生手动排序读取最多 1,000 项的完整日期组，只移动活动任务并保留 done/cancelled 原槽位。建议卡显示源与锚点标题、日期、活动位置前后值和整组/活动数量；完整组的身份、版本、状态与手动顺序只形成 SHA-256 指纹，不把 1,000 个任务或正文复制进卡。相邻无变化、终态/跨组锚点及超大组拒绝提议。用户逐项确认时在同一事务中重新计算并逐字节比对预览，再复用 `PUT /api/v1/tasks/reorder` 背后的完整集合/版本原子命令；组内任何变化使旧卡失效且不部分写入。原生命令重新编号整组手动顺序，因而其他任务的版本也可能更新；卡片明确提示这一点。回执链接源任务详情，决定后刷新 Task/Today/Project 等聚合；没有自动确认、计划改期、任务完成或新权限。API v1/schema077 不变，无迁移。确定性后端与前端契约测试已覆盖；真实模型措辞与原生页面仍待人工验收。

## 今日工作台概览读取（H2-W2，2026-09-21）

获本条消息 `work` 读取授权后，模型可在 `planning` 或 `tasks_projects` 工具主题使用只读 `workspace_today`。它要求用户已确认的 IANA 时区；可指定本地 `YYYY-MM-DD`，省略日期才取该时区的今天，不能把服务端 UTC 当作用户时区。工具与原生 `GET /api/v1/stats/today` 共用统计函数，返回所选计划日期的任务总数、完成/剩余数、预估/实际分钟，所有活动任务的全局逾期/未来 24 小时临期数，以及该本地日已完成 Focus 区间的 Session/秒/分钟。它还明确标示统计口径，不返回任务标题、正文、单条 Focus、正在运行的周期或本地休息/轮次。询问具体任务时必须继续以 `workspace_tasks` 读取真实记录，不从概览计数推断。写入仍须独立建议与人工确认；无新 HTTP 路由、迁移或权限，API v1/schema077 不变。确定性测试验证与原生接口同值、夏令时跨日、取消任务及 Focus 排除、严格参数、授权和 Provider/恢复门禁；真实 Provider 对话效果未验收。

## 今日视图到智能体的显式交接（H4-AR，2026-09-21）

今日页标题区新增“梳理今日安排”。用户点击时只暂存当前所选本地日期、浏览器 IANA 时区和逾期/临期筛选，进入智能体后仍要在交接卡点击“带入问题并选择权限”并亲自发送；推荐 `work+actions` 范围不等于自动授予。提示引导先读 `planning` 规则，再用 `workspace_today` 重新读取同日期概览；风险视图另用 `workspace_tasks` 的全局 `due_state` 分页，普通视图按精确 `planned_date` 查具体任务。页面当前统计、任务标题/正文、列表页和未读数据都不被复制到模型；任何变更仍须逐项人工确认。保存的 `/today?date=YYYY-MM-DD&risk=overdue|due_soon` 返回链接重开同一日期和风险筛选，非法查询退回安全默认；午夜切换继续等待正在进行的拖拽或保存完成。无新后端 API、权限或 schema 迁移；真实模型措辞与浏览器视觉效果待人工验收。

## 后台续办结果可见性（H5-G2，2026-09-21）

活动续办结束后不再从全局入口无声消失。应用级状态区保留最近 24 小时最多 10 条终态授权（完成、停止、失败、轮数用尽、过期或中断），折叠展示保存会话标题、原因和已启动轮数，点击返回准确的保存会话；原会话仍以最新授权和 generation/消息历史为事实。活动授权继续单独查询与停止。状态读取失败时明确提示“不能判断是否仍在运行或已结束”，不能把网络错误解释为空列表；活动授权刚结束会立即重取近期结果。

`GET /api/v1/ai/continuations?status=recent` 只读返回 `updated_at` 在过去 24 小时内的终态，按终结时间逆序最多 10 条；原 `status=active` 保持最多 8 条，其他查询参数仍拒绝。两个列表从已保存会话只额外联结标题 `session_title`，响应沿用安全授权摘要，不增加 endpoint、密钥、观察 hash、生成正文或业务资料。不新增确认、唤醒、模型调用、重试或权限，24 小时列表不是持久通知/人工已读状态；更早结果仍可从原会话最新授权及消息查看。API v1/schema077 不变。

## 有界后台续办（H5-G1，2026-09-21）

当前代码与确定性验收已完成；不等于任意自治调度或真实 Provider 质量验收。普通发送的单消息授权保持不变，后台续办必须通过计划卡**另行授权**，不复制输入器旧权限。它在 Sidecar 存活期间工作，关闭页面不停止；默认在 Sidecar 重启/物理恢复后必须重新授权，只有上方 H5-G12 明确另行选择的安全等待态可跨普通重启，物理备份恢复仍一律撤销。

- 授权绑定保存会话、所见会话/计划版本、精确 Provider version/config_version、重新选择的完整 workspace grant。至少 `work+actions`；可显式加入其他现有领域及文件能力，知识来源仍冻结版本。`workspace_ui/workspace_browser` 不允许后台使用。范围是选择的整个业务域，不是只限计划中列出的 Task；没有隐式对象白名单或任意宿主文件权限。
- 默认最多 3 次续办生成、有效 30 分钟；服务端接受 1–8 次、1–120 分钟。这里的“一次”是一个 generation，内部仍有既有 Harness 的模型/工具/自检预算，不表示一次 HTTP 请求或固定费用。最多 8 个活动授权、同时 2 个后台 generation，另受 Provider/会话互斥；单轮最长 10 分钟且不得超过授权剩余时间。
- 每轮重新核验最新计划及初始来源、有效步骤绑定和本授权所有自动 generation 的完整建议组，包括没有绑定计划的建议。待人工决定、Agent Run 活动和 `running+pending` 产出都会等待，不占用新轮次；pending 必须登记恢复，不能重跑执行器。完成计划停止；缺失证据、外部改写计划、Provider/知识范围漂移、无实质进展、失败、取消或预算到期均停止。
- 受理事务同时创建 generation、用户消息、轮次账本、权限审计并扣减次数；同一已受理身份不能再次调用 Harness，不返还失败轮次。每轮 Registry、工具目录及 opaque 文件候选均独立，不继承上轮运行态。后台禁用不确定上游请求的透明重试和成功后的额外自动压缩请求。生成终态使用 CAS，只有首次终结者写回复、运行步骤及会话版本。
- 后台只能提出操作建议，任何写入、启动 Agent、重试、文件传输和人工验收仍走原逐项确认；停止续办先撤销授权，再取消它拥有的聊天 generation，不取消已人工批准的 Agent Run、不撤销业务命令，也不保证已送达远端的请求不计费。主/侧聊天可单独查看自动轮次及实时进度，不占用手动发送状态或草稿；活动续办期间手动发送返回 `409 AI_CONTINUATION_ACTIVE`，用户停止后再发送。
- 原 H5-G1 默认语义为 `waiting/running` 在重启时中断，关闭为 `interrupted/shutdown`，准备恢复为 `interrupted/restore_pending`。H5-G12 只允许另行同意且没有已受理 generation 的 `waiting` 授权跨普通重启重验；活动请求和物理备份恢复仍不重放，不能据此宣称跨进程未知请求可安全补跑。
- 备份占用维护写锁时，停止先撤销本进程续办资格并立即取消模型请求，再等待短存储锁登记停止；期间不得新建下一轮。后台 worker 已结束后清除活动 generation 标记，保留最后一轮身份和预算历史。前端创建/停止授权成功时取消过期查询，防止迟到的空值或旧授权覆盖成功回执。

### HTTP 与持久化

- `GET /api/v1/ai/sessions/:id/continuation`：最新授权或 `null`；`GET /api/v1/ai/continuations?status=active`：活动授权列表。
- `POST /api/v1/ai/sessions/:id/continuation`：`expected_session_version`、`expected_plan_version`、`provider_id`、`expected_provider_version`、`expected_provider_config_version`、`workspace`、`max_turns`、`ttl_minutes`、`confirm_automatic_continuation:true`；可选 `resume_after_restart:true` 必须同时带独立 `confirm_restart_continuation:true`，省略时默认不跨重启；不接受模型自行授权。
- `POST /api/v1/ai/continuations/:id/stop`：幂等停止，返回最新授权。原 generation cancel 入口也撤销其所属活动授权，不能取消一轮后又自动唤醒。
- 授权响应只含身份、Provider 安全快照、所选范围、状态/原因、轮次/期限及 generation ID；不返回 endpoint、密钥或观察 hash。自动 user/assistant 消息及 generation 查询新增可选 `origin:{kind:"plan_continuation",continuation_id,turn_index,max_turns}`，普通消息保持不带该字段。
- schema **077** 新增 `ai_continuations` 和只追加 `ai_continuation_turns`；授权身份、权限、上限与到期时间不可改，单会话活动唯一、轮次递增、同会话/Provider generation 唯一绑定，终态不可复活。消息 generation 唯一性改为 user/assistant 各自一条，旧消息不改写。迁移不删除或重写旧业务事实。
- 两表属于本机操作态：一致性物理备份包含，便携业务包排除；77 接受冻结形状的旧 76 包及既有传递兼容历史，旧代码不能导入/打开 77，降级使用匹配的旧备份和二进制。升级失败由既有事务/启动门禁处理，不通过手动删表恢复。

代码：`ai_continuations.go`、`ai_continuation_worker.go`、`ai_generation_execution.go`、`077_ai_continuations.sql`；前端 `AiPlanContinuation` 与独立自动 generation 监控。确定性测试使用假模型或本地模拟双协议 Provider，不使用真实凭据，不把自动测试替代人工原生体验或费用验收。

验收：双协议本地模拟 Provider + 真实 `ExecutorMain` 已覆盖“Run 真实提交 → 自动第一轮读取事实并提出验收 → 等待人工确认 → Task 实际通过验收 → 自动第二轮核验完成”；自动轮由 coordinator 启动，不是测试伪造聊天请求。另覆盖原子回滚/幂等、终态 CAS、无进展、未绑定建议、8授权/2worker、Provider 忙、停止/维护/到期/重启、来源标记与手动草稿隔离。`pnpm check:go`、`pnpm check:web`、文档及差异检查通过；最后的终态标记和迟到查询修复又经过相关 Go 回归/vet、119项前端定向/类型检查及最终构建。真实 Provider、原生窗口、安装包与断电专项仍待。开发实库随后在确认无活动运行、创建并再次验证 schema76 原生备份后升级到 77；这不替代真实模型/桌面验收。

## 跨任务成果交接（H5-F6，2026-09-21）

新增独立单消息 `agent_project_files`，须同时有 `work+outputs+actions+agent_execution+agent_files` 和已保存会话。主/侧权限面板必须人工选择，不随交接推荐自动勾选；撤销前置范围会清除它，下一条消息不继承。旧 `agent_files` 在输入来源上仍只涵盖目标 Task 文件和其 Project Attachment，不隐式开放其他 Task 成果，原文件输出权限保持。新能力只允许把明确选中的已验收文件发送给后继执行器，不让主聊天工具直接读取正文；聊天复查正文仍是独立 `output_files`。

- `workspace_agent_project_files(task_id,offset?,query?)` 按目标任务查询同项目其他 Task 当前已验收文件。返回 `task_id/task_version/input_file_candidates/file_limits/offset/limit/total/next_offset`；候选只有 `candidate_id/source_kind/name/mime/size_bytes/created_at/source_task`，不返回真实 Artifact ID、hash、路径或正文。AI 每页最多 20 个源记录，按实际 JSON 24 KiB 预算可缩页；始终按返回 `next_offset` 继续，不以可选项数计算偏移，不漏掉本页不合格记录后的候选。原生页面仍固定 20 个源记录。
- 候选与原 `workspace_agent_execution` 共用**本 generation、同目标 Task** 的 opaque ID 映射。模型仅通过 `agent_run.start.changes.input_file_candidate_ids` 选择，共享四文件/64 KiB 单文件/128 KiB 总输入限制；不能填写来源 proof、hash、路径、文件正文、协议或同意。当前 Provider/Task 执行资格仍须独立读取。服务端冻结来源任务版本、验收批次及文件 hash，完整人工预览显示来源，工具提议回执不外发该完整预览。
- 有新来源时预览为 execution contract v6；Provider 与输入/输出事实完整显示。除了原执行、文件及适用返工/重新执行同意，另须 `confirm_agent_project_files:true`，缺失返回 `AGENT_PROJECT_FILE_CONFIRMATION_REQUIRED`。拒绝或无新来源时传该字段亦拒绝。原生对应 `confirm_project_task_files`；模型没有确认工具。
- v6 原样 retry 必须本条同时授权 `agent_files+agent_project_files` 并重新确认，来源漂移不能重用旧快照。当前事实 start/restart 重新选文件、预检及同意，不继承旧 Run 的资料。计划只绑定真实建议/Run 回执，`depends_on` 不是文件授权，也不自动启动后继执行；只有真实 `submitted` 满足执行步骤，新 Task 仍待人工验收。pending 只恢复登记，不重新读取源文件或调用模型。

完整来源、v6 协议与删除互锁见 [执行契约](local-agents.md#同项目已验收文件交接h5-f62026-09-21)。API v1/schema 076 无迁移，未增加任意文件系统、Shell、浏览器操作、自治调度或授权继承。[双协议 Harness 集成测试](../../services/sidecar/internal/api/ai_agent_project_task_files_test.go) 验证原生 A 产出并验收、仅选一个文件、完整人工确认、实际 B 执行与提交、计划回读及权限不继承；模拟 Provider/真实本地子进程不等于真实模型质量或原生桌面验收。

## Anthropic 执行审批与计划（H5-F5，2026-09-21）

`workspace_agent_execution` 同时列出合规本地/远程 OpenAI 与远程 Anthropic Provider；所选 Anthropic 的完整 start/retry/restart 预览使用 execution contract v5，明确 `provider.protocol=anthropic_messages`、`leaves_device=true` 和 `runtime_limits.max_output_tokens=8192`。资格列表的共用时限不冒充所有 Provider 都有同一 token 预算，最终完整预览才是所选执行事实。旧 v1–v4 预览编码和权限含义保留。

v5 预览始终包含冻结 `output_contract`，但仅实际输入文件或 file/files 输出才附 `file_limits/input_files_leave_device`；纯文本及纯文本返工不增加文件 scope，纯文本审批卡以“文本结果”说明输出，不另列文件契约区。原 `work+outputs+actions+agent_execution`、保存会话和人工执行确认不变；实际文件能力另需本条 `agent_files`，发送输入文件另需文件确认，实际返工另需 `confirm_agent_rework`。模型不能提供协议、预算、凭据或同意字段；提议回执不把完整人类预览正文发回模型。

原生开始/重新执行可选择 Anthropic，v5 原样重试统一读取精确详情并展示协议、预算、输出及适用材料；前端拒绝缺失/畸形协议、预算和 Provider 冻结事实，不静默降级为纯文本。计划接续接受 v5，并核对快照协议/预算、精确 retry 输入或 restart 关联证明；只有实际 submitted 才满足执行步骤，Run succeeded 与 Task 人工验收仍分开。pending 恢复只消费 staging，不调用模型。

API v1/schema 076 不变，无自动重试、跨 Run 调度、工具或电脑控制权限增加。[执行协议和兼容边界](local-agents.md#anthropic-受控执行h5-f52026-09-21)是权威契约；确定性测试不代替真实 Provider 或原生桌面专项。

## 当前事实执行与计划接续（H5-F4，2026-09-21）

任务或配置变化导致原样 retry 不可用，以及旧结果为 `succeeded+retained` 时，现在可提议关联原 Run 的新执行，并在真实提交后接回计划。不是修改旧 Run 的冻结内容，也不是把失败/留存当作完成；原样 retry 仍保留 H5-E29 的严格历史身份约束。

- 沿用 `agent_run.start`，可选 `changes.restart_of_run_id` 必须是同 Task、已终态且符合 [来源契约](local-agents.md#按当前事实重新执行h5-f42026-09-21) 的精确 Run ID。仍须本条 `work+outputs+actions+agent_execution`，当前版本及显式 Provider 资格重验；含本次文件选择/输出还须 `agent_files`。不能继承旧消息授权、文件候选、返工输入或 retained 正文。
- 人工预览 `agent_run_start.restart` 显示来源十项元数据，其他部分显示完整当前 Task/责任/模型/资料与限制；除既有执行、适用文件/返工同意外，确认另须 `confirm_agent_restart:true`。缺失返回 `AGENT_RESTART_CONFIRMATION_REQUIRED`；拒绝或无重新执行来源的其他建议携带该标志亦拒绝。模型不能代填同意。未确认入口定位旧 Run，成功回执定位新 Run。
- 原生事务保存不可变关联事件，Run 回执及 `workspace_agent_runs`、`workspace_outputs(type=agent_run)`、`workspace_get(type=agent_run)` 可返回 `restart_of_run_id`；输入快照/路径/凭据/旧输出不随关联字段外发，Artifact 列表不混入 Run 字段。证明缺失或损坏不能用普通 start 回执掩盖。
- 计划继续使用新步骤的 `replaces`，无新增可写计划字段。上一版已绑定的同会话、同 Task 失败无产出 Run 可由精确 retry 或关联 start 接续；retained 只允许关联 start，不能原样 retry。新步骤须绑定真实建议；确认后验证不可变来源事件、当前冻结预览和新 Run，不能用普通无关联 start 或自然语言承诺略过旧问题。
- 派生 `evidence.restart_of_run_id` 与 `retry_of_run_id` 互斥。新 Run 的 parent 为空，attempt 可因当前 Actor 改变而重新从其序列分配；不得套用原样 retry 的同身份/递增父链规则。新步骤仍待人工确认/真实执行，只有新结果实际 `submitted` 才满足执行步骤，且不等于 Task 已验收。
- 中间建议 rejected/expired 仍锁定同一来源，包括计划首个绑定步骤本身就是关联 start 的情形；不能用撤回再替代洗掉来源。新执行自身失败时，下次接续改指该新 Run。旧审批、原失败/留存结果、历史修订保持不变，主/侧计划显示重新执行关联。12 步/128 修订/12 KiB 意图及整轮续办检查不变，活动和 pending 仍阻断。

代码/验证：[AI 来源校验](../../services/sidecar/internal/api/ai_agent_run_restart.go)、[计划替代](../../services/sidecar/internal/api/ai_work_plan_agent_retry.go)、[真实 Harness 回归](../../services/sidecar/internal/api/ai_work_plan_agent_restart_test.go)、[前端审批与计划解析](../../apps/web/src/api/aiAgentRunRestart.test.ts)。双协议指主聊天 Harness 的本地模拟 Provider，内置执行器协议并未扩为 Anthropic。API v1/schema 076 不变，但旧二进制不保证理解新事件、审批及替代语义；不新增自动执行、后台调度或真实模型质量保证。

## 执行失败反馈（H5-F3，2026-09-21）

内置 Agent Run 不再把模型截断、拒绝/过滤或缺少正常终态的文本登记为产出；任务列表、精确运行详情、右侧资源和 Inbox 来源保留安全错误码并显示固定中文原因。新增 `AGENT_MODEL_TRUNCATED/AGENT_MODEL_FILTERED/AGENT_MODEL_RESPONSE_INVALID`，仅为代码所有的失败 metadata，不把 Provider 原文、部分正文或凭据写进诊断。AI 操作回执 schema 和授权不扩展，查看详情不等于重试；主聊天工具调用与独立 Runner 的非流式交付协议仍分开。见 [执行完成与错误契约](local-agents.md#执行完成信号与安全失败原因h5-f32026-09-21)。

## 返工执行桥接（H5-F2，2026-09-21）

`agent_run.start` 可成对提供 `rework_submission_id/rework_artifact_ids`，基于真实当前退回批次启动新执行；模型先读 `workspace_task_submissions`，不编造退回意见或旧稿。服务端把完整意见和明确选中的至多四个非文件产出冻结在 v4 审批预览中（整个返工 JSON ≤64 KiB），另要求人工 `confirm_agent_rework`；空数组明确表示不带旧稿。原本条执行 scope、Provider/身份/版本重验、文件独立授权、成功后人工验收和计划重试事实规则保持。纯文本 v4 retry 不误判为文件执行；有受控文件输入或文件输出才需 `agent_files`。提议与元数据工具不外发冻结返工正文，确认后才通过 Runner 向所选 Provider 发送。完整 API、兼容和原生 UI 契约见 [返工执行](local-agents.md#退回意见驱动的新执行h5-f22026-09-21)；未实现自动返工或自治续办。

## 受控文件产出复查（H5-F1，2026-09-21）

新增独立手选的单消息 `output_files`（依赖 `work+outputs`，临时会话可用）：`workspace_read_artifact_file` 绑定精确 Task/Submission/Artifact、Task version 与 SHA-256，校验实际受管 UTF-8 文件后分页读取。旧 `outputs`、`agent_files` 与面板预览不隐式增加读取许可；授权明确披露当前 Provider 接收正文、远程离机、范围不限当前记录，交接推荐不自动选中。工具进度显示“校验并读取产出文件”，不显示正文或假称已验收。

已有产出/批次/待验收交接引导先查元数据、独立授权、完整分页复查，再依原规则起草人工验收或返工；不把 Run.result_text 副本当当前文件检查。只读不改完整性状态、不创建审批或 Run；人工确认、证据缺口和所有预算保持。API v1/schema 076 不变，完整字段、格式与安全边界见 [Task 文件复查](tasks.md#ai-受控文件产出复查h5-f12026-09-21)。

## Agent 失败诊断与精确来源（H3-C3，2026-09-21）

人工启用的失败预设把未来真实 Agent 失败接入本地 Inbox。`workspace_inbox_source` 对新 `agent_run_failed` 来源先要求 `work+outputs`，再核验原 Run/Task、失败事件与通知 Automation Run 的反向结果；只返回身份、状态、各自 nullable version 和固定 route，不返回输入/输出、原始 payload 或配置。权限不足不查保护对象，缺失/错配为 unavailable；导入未携带的 Run 不能冒充当前事实。

来源卡进入精确执行详情并沿合法 UI 返回身份回到原对话。通知创建成功不代表原 Run 成功；处理 Inbox 不完成 Task 或修复计划。105 的 `automation.retry` 使用独立严格人工预览，只重试通知写入；Agent 重跑仍须新 `agent_execution` 与人工确认，文件独立授权。规则默认关闭，无历史补发、自动重跑或新 scope；schema 076 不变但旧代码不理解新来源。详见 [失败诊断闭环](automation.md#agent-失败诊断闭环h3-c32026-09-21)。

## 收件箱来源详情往返（H4-AT，2026-09-21）

原生 Inbox 来源卡现将合法原对话身份贯穿十类业务来源；里程碑精确定位、任务产出历史批次与 Automation Run 入口不再只落列表或父对象。Inbox 表单、内容详情及提醒编辑在返回/关闭时保护未保存内容，在途请求不能通过这些入口离开；提醒管理器内部切换也使用明确舍弃确认。范围和剩余限制见 [来源详情往返](inbox.md#来源详情与原对话往返h4-at2026-09-21)。

这是人工导航闭环，不新增模型工具、scope、自动读取/发送或审批能力。来源卡校验的是快照身份自洽，当前对象仍由详情 API 读取；不能替代 `workspace_inbox_source` 的真实关系核验。API v1/schema 076 不变，非全局草稿持久化或全站路由拦截。

## 客户回访提醒与安全交接（H2-AB，2026-09-21）

修复原生回访到期扫描在整秒/短小数同秒漏提醒及排序错误，全局/单客户原生分页也改用与 AI 相同的纳秒时间排序；扫描事务重读排期并使用固定扫描时刻，原版本化来源和审计不变。Inbox→来源查询→`people_clients` 指南/当前回访→原有完成/续排或重排建议→人工确认沿用既有权限和领域事务，缺少 clients 必须新消息授权，不能直接解决 Inbox 冒充已联系客户。

回访页面有未保存的新建/编辑/生命周期草稿或在途命令时，不允许行内交接跳走，并显示原因；选定回访的返回原对话/关闭定位同样受保护，单纯读取失败仍可返回，不声称全页面导航拦截。Inbox 来源链接保留页面提供的合法返回对话身份。两者不自动复制正文、授予权限或发送，复合命令仍明确新旧计划，完成另需真实沟通的人工勾选。API v1/schema 076 不变，详见 [回访闭环](client-followups.md#到期提醒与智能体处理闭环h2-ab2026-09-21)。

## 收件箱到真实来源的闭环（H2-AA，2026-09-21）

`inbox` 指南新增 `workspace_inbox_source(inbox_item_id)`：本条 `work` 下核验当前 Task、Project、内容、里程碑、Reminder 或自动化来源；Artifact、客户回访和发票分别额外要求 `outputs/clients/finance`。额外权限先于保护对象存在性/删除检查，不足时只提示所需范围，不能默认全选或继承授权。结果只包含 Inbox 与真实来源身份、版本/状态、路由及历史快照比较，不读取或外发原始 payload、标题/正文、金额、文件或配置；Artifact/Submission/Automation Run 的无版本事实保留 null。

单次只读事务校验来源键与必要的真实外键、反向结果，不用标题搜索代替身份。manual、删除、缺失/不一致、未支持和未授权状态分别表达，不把旧事件当当前版本或实际工作完成。现有 Inbox 交接与工具指南引导先核对来源、再读取领域规则，推荐范围仍为 `work+actions`，需用户显式带入并发送；执行过程显示“核对收件箱来源”，不自动打开来源链接或执行变更。

内容到期扫描另复用纳秒时间键，并在事务内重读排期、与扫描开始的固定时刻比较，修复整秒/短小数恰好到期漏读与批次顺序；实际提醒→来源查询→最新内容→待确认改期→人工确认→历史提醒/当前内容复查走原有领域事务。发布、验收和回款仍各有独立边界。API v1/schema 076 不变，无新增权限、迁移或自治执行；详见 [Inbox 来源契约](inbox.md#ai-来源定位h2-aa2026-09-21) 和 [内容到期扫描](content-calendar.md#到期扫描与-ai-来源闭环h2-aa2026-09-21)。

## 内容排期到准备任务的闭环（H2-Z，2026-09-21）

`work` 下新增 `workspace_content_items`，由 `roadmap_content` 指南按需加载，可按平台、日期半开范围、项目、状态及准备进度精确筛选，再分页读取真实准备 Task ID/version。原生列表和 AI 复用时间/过滤/进度规则，修复整秒与小数时刻字符串比较造成的漏读及排序错误；只有 done 计完成，零任务、取消或待验收不被当作准备完成。内容版本与 Task 版本独立，跨页不是冻结快照。

内容页“梳理内容排期”只暂存实际视图条件、日期解释时区和固定提示；用户仍显式带入、重新授权、手动发送。创建/详情弹窗及排期修改未稳定时禁用入口，单条内容交接保持。工具结果不含备注、外链、任务正文或文件；进度显示“查询内容排期与准备任务”。后续改期、关系与 Task 操作复用原有建议卡和独立人工确认，完成准备不等于审核/发布，不能靠移除 required 或绕过验收伪造完成。

契约及确定性双协议 51-task→确认完成→重读→另行确认改期的证据见 [内容结构化查询](content-calendar.md#ai-结构化排期与准备任务查询h2-z2026-09-21)。API v1/schema 076 不变，无新 scope、迁移、自动发布、后台续办或任意浏览器/终端权限；实际 Provider 和原生桌面验收仍待。

## 项目产出到后续工作的闭环（H2-Y，2026-09-21）

新增 `work+outputs` 下的 `workspace_project_outputs`：直接按当前项目读取 Task 产出元数据、真实 follow-up Inbox 身份/版本和必需任务进度，可按精确 Artifact、跟进状态及删除历史筛选分页。无需逐个任务扫描或按标题猜关联；Project version 不代表 Inbox 版本，修改前仍重读当前关系并走原有逐项确认。来源缺失、待拆分、跟进中、已解决、已忽略，以及 Task 完成/待验收必须分别解释，不读取文件或项目附件正文。

项目产出区的“梳理项目跟进”只准备 Project ID 和固定提示，建议 `work+outputs+actions`；用户显式带入、授权并发送后，模型才查询事实并按需提议拆分/关联等动作。原单项 Artifact 复查仍只推荐只读范围；主/侧对话都复用既有交接和审批，不自动发送、分派、执行、验收或解决。工具由 outputs/tasks_projects/inbox 指南按需加载，执行过程显示中文查询状态。

完整字段、分页、来源一致性和版本边界见 [项目产出与跟进](projects.md#ai-项目产出与跟进闭环h2-y2026-09-21)。API v1/schema 076 不变，无迁移、新 scope 或写动作；确定性模拟不代替真实 Provider/原生桌面验收。

## 已有任务的验收策略与 Agent 交接（H2-X，2026-09-21）

`workspace_get(task)` 增加验收方式与当前可切换布尔值；`task.update` 可在 `work+actions` 下为已有任务提出 none/manual 策略修改。仅 todo 且无任何提交历史允许实际切换，提议及确认复用原生门禁；新卡显示完整前后值及子任务/责任条件，确认重验关联影响，不通过修改策略绕过已有验收。

父任务在全部非取消直属子任务完成、原生责任条件满足时，none→manual 会生成 child_rollup 并进入待验收，卡片明确披露；这不等于验收通过、任务完成或启动 Agent。策略确认后必须读取最新版本与责任，再按新单消息授权和独立确认启动执行。普通 task.update 回执仍只表示命令已登记，不能据此声称产出或任务完成。读侧不返回提交内容/身份，也不新增 outputs 或执行权限；手选上下文显示同一安全投影。

契约、错误与测试见 [Task 验收策略切换](tasks.md#ai-既有任务验收策略切换h2-x2026-09-21)。API v1/schema 076 不变，无迁移、自动授权、自动执行或后台续办；未带策略的历史更新与新建任务能力保持兼容，批量命令不扩大。

## 失败执行的精确重试接续（H5-E29，2026-09-21）

计划已支持把一次经过人工确认但执行失败的 Agent Run，与后续独立确认的精确重试关联起来；重试真正提交产出后，不再因为原失败步骤永远无法满足而卡住整个计划。沿用 `replaces` 与只读 `superseded_by`，不删除历史，不将旧失败改成成功，也不自动执行重试。

- 源必须是上一版已绑定的同会话 `agent_run.start` 或 `agent_run.retry`，真实 Run 为 `failed/cancelled/interrupted`、交付状态 `not_ready`，且没有结果、提交或待登记数据。新步骤须先绑定同会话、同 Task、针对该精确 Run 的真实 `agent_run.retry` Proposal，不能用未绑定步骤、无关命令或语言承诺替代。运行中、待登记、仅保留未提交、成功或缺失证据不允许走此路径；登记恢复仍使用原 Run，不新开 attempt。
- 服务端每次投影都重验原/新命令 fingerprint、严格参数、来源与真实 Run；确认后的子 Run 必须直接指向父 Run，attempt 大于父 attempt 且匹配冻结预览，并保持冻结执行身份、模型、完整输入和输出契约一致。attempt 按 Task+Actor 分配，不要求恰好父 attempt+1；不重新读取当前 Provider/Task 来改写历史冻结身份。历史版本和幂等重放同样核验，证据缺失或漂移明确失败。
- 重试尚待确认、已拒绝、过期或生成不可执行时，关系可读但新步骤不满足。拒绝/过期后再替代仍须重试原精确 Run，不能借中间建议洗掉执行目标；已确认的重试自身失败后，下次接续必须指向这次新失败 Run。原 12 步（含历史）、128 修订、12 KiB 意图和显式重接依赖规则不变。
- `evidence` 对 `agent_run.retry` 增加可选 `retry_of_run_id`，仅来自已核验命令身份，不含 ActionJSON、预览、输入、路径、产出正文或凭据。前端兼容旧普通重试缺少字段，但新的失败替代必须有精确匹配身份；错误类型/身份或跨动作字段拒绝。主/侧计划标注“重试接续”，保留旧失败和原/新审批入口；pending/running/output_pending 不显示完成，只有真实 submitted 满足该执行步骤，仍不代表 Task 已完成或人工验收通过。
- Inbox 和“继续此计划”沿用有效步骤统计及整轮核验；源历史不被当作成功，新审批、活动执行或待登记结果仍阻断续办。新提议/执行继续要求独立单消息授权、适用文件同意与原人工确认，不新增 scope、API 路由、数据库迁移、后台调度或模型调用。

确定性证据：`ai_work_plan_agent_retry_test.go`、`ai_work_plan_agent_retry_harness_test.go`、`aiWorkPlanAgentRetry.test.ts` 与 `AiWorkPlan.test.tsx`。双协议本地模拟 Provider 经过真实 Harness 和审批，运行进程以受控状态/完成事件替代，使用真实登记失败与恢复事务验证闭环；不替代真实模型或原生执行器验收。API v1/schema 076 不变，但旧二进制不保证理解已确认失败的替代语义，降级须配套代码或兼容备份。自治重试、并发子代理与自动后台续办仍未交付。

## 按领域加载工具定义（H5-E28，2026-09-21）

H5-E28 当时解决“启用很多能力后，每次模型请求都携带完整工具参数”的占用问题。注册表仍保存本条消息已授权的完整能力；当时模型提示常驻记忆、通用查找/详情、完整操作提议、计划、知识搜索/引用读取以及已授权 UI/浏览器导航工具。专注、财务、任务筛选/保存视图/分派、客户活动、项目笔记、自动化、产出与 Agent 前置查询等辅助定义通过既有 `workspace_guide(topic)` 按需加载。当前完整操作提议定义又由上方 H5-G13 延迟到领域指南成功接受后。

- 指南既供纯查询发现，也保留写前领域规则。成功结果增加 `available_tools`，列出**下一次**模型请求的完整可见目录；只列当前已注册工具，不返回业务数据。指南工具执行线程不改目录，只有未取消、未超时、未截断的结果经 Harness 接受并重验代码所有 topic、规则和名字后才切换。错误不改变目录；同批多次指南按接受顺序以最后一次成功选择为准。
- 选择是“常驻工具 + 当前领域辅助工具”的替换，不无限累加。模型可再次调用指南取回另一领域；每次指南仍消耗原工具/模型轮数，已经进入本轮历史的规则和查询结果不删除。新消息重新按新 grant 建立目录，选择不跨 generation、会话或 Provider 共享。没有指南的小型知识读取/UI/浏览器单独授权目录保持原样；未来未分类工具默认常驻，避免能力无入口。
- `Registry.Get/Names/Definitions` 始终表示完整授权集合，`ModelDefinitions` 只做提示投影；模型即使调用未展示但已授权的工具，仍走原严格参数、scope 和 Provider 身份校验。目录既不授予也不撤销权限，不替代写入的人工确认、独立财务/执行/文件同意或领域版本门禁。知识证据仍从原注册对象提取，不因目录切换丢失引用。
- 初始受理前预算、普通工具循环和自检读取同一代可见定义；第 8 次无工具收尾仍优先清空工具并硬拒绝调用。64 KiB 实际协议输入、8 次模型请求、32 次工具、原结果/时间/累计输出预算不变，不在后台自动增加模型请求。容量用例（全部 scope、20 个知识来源与最大有界精确回执）提高到至少预留 **24 KiB**；这是固定用例的门禁，不是任意历史或大工具结果都能容纳的保证，当前必需证据超限仍明确失败。
- 同时修复模型 schema 精简误删业务 `description` 的问题：只删除 schema 节点的说明注解，保留 `properties` 等字典中名为 `description` 的字段及完整类型/长度/必填约束，也保留枚举、默认值、例子等实例数据；不修改原 schema。任务/项目描述重新出现在提议参数中，确认前仍不创建或修改业务。

代码与确定性证据：`ai_tool_catalog.go`、`ai_tool_catalog_test.go`、`ai_tool_catalog_harness_test.go`、`ai_schema_descriptions_test.go` 及 Harness `model_tool_projection_test.go`。双协议本地模拟 Provider 验证指南发现→真实查询→待确认提议→目录替换与自检→人工确认落库，以及后续消息重新初始化。API v1/schema 076 不变，无新工具名、迁移或任意 Shell/文件/浏览器控制。真实模型的工具选择质量、运行内证据摘要和自治跨 Run 调度仍待专项验证。

## 计划接续的整轮核验（H5-E27，2026-09-21）

“继续此计划”不再直接使用缓存准备草稿。点击先核验用户当前所见版本、同会话活动生成和计划涉及的完整操作建议组，避免未绑定到计划的同轮建议或 Agent Run 被遗漏。通过后才保留草稿、清除旧授权/上下文并打开单消息权限选择；仍须用户确认权限和手动发送。

- 新增只读 `GET /api/v1/ai/sessions/:id/plan/continuation?expected_version=N`。要求保存会话、canonical ID、唯一且规范的 `expected_version=1..128`，不接受额外查询字段。同一只读事务读取最新修订与实际回执；版本变化返回 `409 AI_PLAN_CONTINUATION_CHANGED`，前端刷新展示并要求再次点击，不自动改用新计划。会话/计划不存在返回 `404 AI_PLAN_NOT_FOUND`；查询非法返回 `422 AI_PLAN_VERSION_INVALID`，ID 非法为 `422 AI_SESSION_ID_INVALID`。必需证据缺失导致原严格计划投影失败时返回 `409 AI_PLAN_CONTINUATION_UNAVAILABLE`，数据库故障仍明确失败，不改用缓存或宽松投影。
- 相关来源包括最新计划修订的 generation，以及每个未被替代、已绑定 action 的来源 generation；检查这些 generation 的全部建议，而非只检查计划里的卡片或截断后的回执窗口。同会话任何活动生成、相关组内 pending、未结束 Run、待登记结果、缺失或无法核验的证据都阻断。已替代历史不单独引入来源，但同一来源仍被有效步骤或最新修订引用时必须检查整组；无关历史建议不永久阻塞当前计划。纯分析或尚未提议操作的计划允许空建议组。
- 成功读取返回 `data:{plan,ready,reason,blocking_generation_id?}`。`plan` 沿用现有 metadata 投影；原因闭集为 `ready|generation_active|plan_complete|pending_approval|run_active|output_pending|evidence_unavailable|generation_unavailable`，只有 `reason=ready` 才可准备。阻塞来自具体相关组时可返回同会话 generation 身份，界面提供“查看阻塞这一轮的操作建议”，复用原审批卡；不返回 ActionJSON、PreviewJSON、产出、输入快照或凭据，不创建新的审批入口。
- 检查期间禁用重复点击。会话、Provider/配置、侧栏 owner、生成/受理状态、历史版本选择或新计划事件变化时丢弃迟到结果，即使切回原来源也不恢复旧请求；卸载会取消请求。主/侧稳定回调保留检查期间新输入的草稿，不因旧检查清除新授权或写入另一输入区。
- 通过只是截至 `plan.as_of` 的只读快照，不是执行租约或后续授权。发送后仍须读取最新计划/领域状态，继续使用原版本、预览和人工确认门禁；完整计划步骤已满足也不证明 Task 已验收。失败明确显示原因，可重试或打开原建议；不会自动调用模型、批准、重跑、恢复或增加 scope。

确定性证据：`ai_work_plan_continuation_test.go`、`aiWorkPlan.test.ts`、`AiWorkPlan.test.tsx` 及主/侧对话测试。API v1/schema 076 不变，无迁移；该阶段当时尚无后台自动续办或工具证据提炼。后续 H5-G1/G12 已补显式有界续办，H5-G11 已补确定性证据胶囊；完整模型语义摘要和真实 Provider/桌面验收仍未完成。

## 运行内历史窗口调整（H5-E26，2026-09-21）

初始消息原本会按最终协议请求体选择 64 KiB 内的完整历史回合，但工具结果和自检草稿追加后没有再次调整，因此第二轮也可能立即超限。现在每次模型请求（包括自检修订、末轮交接）都会重新计算 OpenAI/Anthropic 实际序列化字节数，必要时只从最旧的完整历史回合开始移出本次请求。参考 [Codex 上下文整理中保留近期输入与重新注入上下文的处理](https://github.com/openai/codex/blob/main/codex-rs/core/src/compact.rs)，这里采用确定性窗口调整，不声称实现了同等的模型摘要或无限长任务。

- 进入 Run 时固定本条用户消息边界；本条消息、自检草稿与修订指令不可移除，工具调用/结果不能作为旧完整回合移出。H5-G11 后续仅允许在旧回合已移尽仍超限时，收缩**较早且代码明确允许重查的只读结果正文**；最近一组结果、提议/计划/审批及其他受保护证据仍完整保留。自检追加的内部 user 消息不能把本轮证据误判为旧对话。原系统规则、已确认记忆、已有摘要/事实、本次业务/知识上下文、精确审批回执和当次工具注册全部保持，不借窗口调整增加 scope 或修改授权。
- 只整回合移出，不截断字符串或拆散工具调用/结果；同次运行的移出位置单调向前，后面禁用工具腾出空间也不重新塞回旧回合。真正调整后加入代码所有说明，提醒模型旧细节可能不完整、不得猜测旧约束、缺少重要信息须询问；说明本身也计入 64 KiB。较早原消息仍在本地聊天库，既有运行后摘要管线和水位线不被此过程改写。
- 无可移出的旧历史及 H5-G11 允许的较早只读结果时，当前消息、最近工具结果、受保护回执和显式上下文自身超限仍返回 `AI_PROMPT_TOO_LARGE`，不放宽预算、不把失败改成成功；普通失败会继续使本轮待确认建议不可执行。8 次模型请求、32 次工具调用、10 分钟、原工具结果和累计 1 MiB 输出预算不变，不新增模型调用、自动发送或后台续办。
- SSE/活动 generation 的模型与自检步骤新增可选整数 `trimmed_history_turns`，仅表示本次请求移出的完整旧回合数（1–200，0 省略），不含旧文字或任何工具数据。执行面板显示“已调整 N 轮较早对话”及保留边界；前端接受旧响应缺失字段，对其他步骤、非法值或额外字段仍拒绝。字段不新增数据库列，终态步骤回读不保留逐轮计数；成功回复会附固定中文披露并在持久化后通过 `replace` 返回，重新打开会话仍能看到本轮调整说明。临时会话只显示、不保存正文。失败/取消只保持原失败与部分回复路径，不能据步骤计数宣称成功。
- 固定披露与轮数预算交接提示共同计入最终正文/推理的 1 MiB 限制，不能截断模型回答凑出成功；无调整的原回复保持不变。此机制不等于所有旧约束都被完整摘要，旧细节重要时需用户补充或由已授权工具重新核验。
- 同轮联通检查补齐既有 `workspace_tasks` 的前端进度白名单，避免任务筛选查询的合法 SSE 被当作 `INVALID_RESPONSE` 中断；实时流与终态回读使用同一严格解析。仅接受无正文的工具进度标签，不扩大工具、scope 或结果读取范围，额外 `result` 字段仍拒绝。

确定性证据：Harness `prompt_window_test.go` / `prompt_window_edges_test.go`，API `ai_context_window_test.go` / `ai_context_window_notice_test.go`，前端 `aiProgress.test.ts` / `AiRunProgress.test.tsx`。覆盖实际双协议字节预算、完整旧回合、当前工具配对和上下文保留、自检与最后轮、人工确认回执、无可裁硬失败及可见披露。原阶段 API v1/schema 076 不变；后续 H5-G11 的只读结果正文收缩及确定性证据胶囊另见本文件顶部，完整模型语义摘要、自治跨 Run 调度及真实 Provider 长任务质量仍未完成。

## 审批冲突后的原意图重新核验（H4-AS，2026-09-21）

人工确认遇到 `VERSION_CONFLICT` 或 `AI_ACTION_PREVIEW_CHANGED` 后，原确认组可提供“撤回旧建议并重新核验”。用户先单独同意重新披露这一条建议的完整原操作参数，再点击撤回；组件重读整轮建议，以原 proposal/fingerprint 拒绝旧快照，随后再次读取实际决定。模糊网络失败不推定撤回成功，已确认不能当作拒绝，投影为 expired 也须先持久拒绝。其他同轮 pending 建议、活动或待登记 Run 仍阻断接续；原建议已拒绝但被整组阻断时保留此次重新核验入口，可处理其他事项后重读。来源切换后的迟到结果不追加到新输入器。

- 主/侧输入区保留现有草稿，清除旧单消息授权及原上下文，显示可移除的“重新核验”附件并打开新授权面板。只保存运行时来源身份，不把原参数复制到草稿或侧栏持久状态；没有新 grant 时不能发送。仍须用户手动发送，不自动调用模型、批准、重跑或恢复。权限按真实动作推荐，项目修改含 `client_id` 时补充 clients；`agent_files` 必须另行手选，不沿用原文件同意。
- `POST /api/v1/ai/chat` 新增可选 `action_recheck_proposal_id`，须同时提供精确 `action_receipt_generation_id`、当前保存会话和新 grant。服务端只接受同会话持久终态 generation 中已经持久 `rejected` 的原建议，核验不可变 `ActionJSON` 摘要并复用提议的领域权限门禁；Agent 文件字段及 v2/v3 retry、实际含文件输入/输出的 v4 retry 另须 `agent_files`。非法身份返回 `422 AI_ACTION_RECHECK_SOURCE_INVALID`，来源缺失/错配/未拒绝返回 `409 AI_ACTION_RECHECK_SOURCE_UNAVAILABLE`，不足授权返回 `422 AI_ACTION_RECHECK_SCOPE_REQUIRED`；均不创建消息或调用模型。
- 同一只读快照读取精确回执和原操作参数，受理事务再次核验来源。仅原 `ActionJSON` 进入本条 `BusinessContext`，可能包含用户原来提供的备注、财务或知识文本；不读取/复制完整 `PreviewJSON`、执行输入快照、Provider 配置或凭据。它是需重新评估的历史意图，不是当前事实、系统指令、成功回执或执行许可；模型必须按本次权限读取最新目标和版本，重新取得文件候选，生成新 Proposal 并等待新的人工确认。绑定计划的旧建议可按 H5-E23 显式替代，不能改写历史。
- 原参数不截断；实际双协议初始请求仍受 64 KiB 提示预算，超限在受理前返回 `AI_PROMPT_TOO_LARGE`。附件不另存为新用户消息的 ContextSnapshot、回执正文或运行事件，不自动附到后续消息。模型仍可能在保存的回答中引用本次资料，这是普通对话历史，不保证已披露文本永不再次出现。
- 来源在移除、会话/Provider 配置变化或被接收后清除；每次准备另建仅运行时的附件实例标识，后台恢复同时匹配该实例、owner、session、generation 和 proposal。即使重新选择同一条建议，旧请求的迟到回执也不能消费新附件或新授权，新草稿不删除。实例标识不进入 HTTP 或持久化，模糊请求重试也区分实例；服务端幂等身份包含 proposal ID，同一个 key 不可改选旧建议。没有该字段的原会话协议及请求预算不变。
- 侧边输入器与主输入器都按草稿编辑版本保护清空：无论在受理前把文字改回原文，还是受理后重新输入相同文字，旧发送完成和后台回读都不会删除这份新草稿。侧边版本只在组件内运行，不修改标签持久结构。

确定性证据：`ai_action_recheck_test.go` 覆盖双协议隔离 Provider 的版本冲突→人工拒绝→历史消息已移出窗口后恢复原意图→读取最新 Task→新建议→人工确认闭环，以及来源/授权/状态/删除/超预算的零受理边界；前端覆盖明确披露、撤回竞争、全组门禁、主侧隔离、附件移除和受理恢复。API v1/schema 076 不变，无新 scope、迁移或自治调度；真实 Provider 和原生桌面仍单列验收。

## 轮数预算收尾与执行后接续（H5-E24/E25，2026-09-21）

工具循环不再用尽第 8 轮执行工具后直接报错。现在最多仍是 8 次模型请求，但经过工具循环后的最后一次请求专用于自然语言交接：不提供工具定义，服务端也拒绝执行该轮返回的任何工具调用；第 7 轮自检不足触发的最后一次修订同样受此约束。不增加第 9 轮、自动下一条消息或后台续办。

- 成功的无工具收尾前加代码所有提示，明确“达到本轮上限，不代表任务完成”，说明待确认建议仍需人工决定、后续消息须按需重新授权。`replace` SSE 在持久化成功后发送完整提示与回答，保存会话的 generation 回读及 assistant 消息保留同一文本；非持久会话只在本次响应中展示，正文不落库。
- 仅这种正常收尾允许 generation 完成，之前合法保存的 pending Proposal 可继续走原确认事务，不会因轮数本身被作废。模型仍调用工具、网络/协议失败、取消、超时、提示/响应/工具预算失败都保持原失败或取消路径，不能据此批准旧建议。累计模型/工具输出仍为 1 MiB，添加固定提示后的正文与 reasoning 合计也不得越界；不截断证据来伪装成功。
- 自检充分、不充分或不可用照实记录，最后一次收尾不再因自检不足发起额外修订。generation 的 completed 只表示本次回复完整收尾，不证明计划满足、命令执行、Task 完成或人工验收；操作事实仍看原审批与 Run 回执。
- Agent Run 终态观察和原生命令返回现在也取消、失效其任务关联的完整审批组查询；利用已校验 Proposal 的 `task_id` 匹配来源，首次、空或尚无法判定归属的查询保守重读，已知无关组不刷新。迟到的旧请求不能把新回执回滚。这里只刷新已有本地事实，不直接按单个 Run 解锁整组：同轮仍有 pending 建议、运行中或待登记 Run 时仍不能继续，点击接续也再次读取全组。

确定性测试覆盖 Harness 最后一次普通/修订请求、双协议本地 mock Provider 到审批确认与持久回读、临时正文隔离，以及真实查询 Hook 驱动的执行卡/审批组同步。API v1/schema 076 不变，无新增权限、迁移或自动执行；真实 Provider 交接质量与原生桌面体验仍单列验收。

## 计划旧建议的显式替代（H5-E23，2026-09-21）

旧计划会永久保留被拒绝或过期的 action，而这些步骤不可能满足完成条件。现在模型可在新的 action 步骤上给出可选 `replaces`，将上一版已绑定审批的旧步骤明确纳入替代历史；旧步骤仍显示原本的拒绝/过期状态和审批入口，不伪装成成功。新建议仍须独立人工确认，替代关系本身不执行操作、不重试 Run、不新增权限。

- 普通替代只允许同会话中服务端当前投影为 `rejected` 或 `expired` 的 action；H5-E29 另允许严格限定的失败 Agent Run 精确重试，H5-F4 增加失败/留存 Run 的关联当前事实执行。关联来源跨中间拒绝/过期继续锁定。目标必须在上一版存在且已绑定 Proposal。新步骤只能引用前序旧步骤，每个旧步骤只有一个直接替代者；可以逐版形成链，但不能在已有步骤上补写、更换或移除替代关系。旧步骤 ID、kind、已绑定 Proposal 始终保留，被替代步骤的标题和依赖也冻结；旧版本不套用最新替代关系。
- 当前有效步骤（未被其他步骤替代）不能依赖历史替代步骤。更新者须显式修改依赖并调整步骤顺序，仍满足前序依赖的无环规则；不隐式重定向，也不把拒绝当作依赖满足。分析步骤、未绑定审批、待确认、运行中、待登记、证据缺失以及不满足 H5-E29 精确重试或 H5-F4 关联新执行条件的已确认操作不能用此关系略过。
- `GET /api/v1/ai/sessions/:id/plan` 与工具读取保留原 `state/ready/satisfied/evidence`，并在旧步骤派生只读 `superseded_by`；模型不能写入该派生字段。每次读取（含历史和幂等重放）重新核验拒绝/过期、H5-E29 精确重试或 H5-F4 关联新执行关系；缺失或漂移会明确读取失败，不能静默排除后声称完成。当前计划 Inbox 沿用整体投影错误行为，单个失效计划可能导致该来源列表读取失败，UI 提供重试且不展示虚假完整计数。
- Inbox 的 `step_total` 仍含全部步骤，新增 `superseded_step_total`；`satisfied_step_total` 只计有效步骤中同时 ready/satisfied 的数量，二者之和等于总步骤数才是计划完成。前端兼容缺少新计数的旧响应（按 0），以“已满足数／有效步骤数”显示进度，并另列历史替代数。主/侧计划、左栏队列和右侧概况口径一致，右侧也遵循来源 generation 的运行状态；继续按钮和推荐权限只考虑有效步骤，原审批记录仍可查看。
- 仍受每版 **12 步（含替代历史）**、128 个只追加修订、12 KiB 意图限制；达到容量后不删除历史、不自动开启新计划或自动放宽上限。`workspace_guide(planning)` 说明完整替代约束；同版本重放不多写修订/审计，失败不写计划或领域事实。

确定性证据：`ai_work_plan_supersession_test.go`、`ai_work_plan_supersession_harness_test.go`、`aiWorkPlan.test.ts`、`AiWorkPlan.test.tsx`、`WorkspaceResources.test.tsx`、`AiSessionRail.test.tsx`。本地 mock Provider 验证原提议→人工拒绝→重新授权读取→新建议与显式改依赖→再次确认→复核后计划完成；原拒绝保留且实际只创建新 Task。API v1/schema 076 不变，意图 JSON 为加法字段，新代码能读取旧修订；不支持旧二进制读取含替代字段的新修订，降级须使用匹配版本代码或操作前备份，不能只凭 schema 相同认定可回退。真实 Provider、原生桌面与自动调度仍未由这些测试验收。

## 审批后显式接续（H4-AR，2026-09-20）

无需先建立执行计划，用户也可以从操作建议组“继续处理这一轮”。入口出现在主对话、侧边聊天、计划关联审批和历史审批定位窗口；点击先重读同一 generation 的全部建议，精确单卡视图也不能跳过同轮其他 pending 项。只有全部建议已确认、拒绝或过期且没有运行中、待登记或无法核验的 Agent Run 时，才把固定续办提示追加到来源输入器。读取失败保留错误，不按旧缓存继续；切换来源或卸载后丢弃迟到结果。

- 准备接续保留并去重已有草稿，清除旧单消息 grant；主对话同时清除旧的工作台/知识上下文选择。按真实动作类型推荐新权限，不自动授予 `agent_files`、知识正文或工作区浏览权限；纯财务/知识操作不额外推荐工作事项操作，计划接续仍补齐 `work+actions`。用户必须重新确认权限并手动发送，按钮本身不调用 Provider、不批准、不重跑或恢复业务。
- 输入区显示可移除的“操作回执”附件，只有 generation 身份留在组件运行时，不写入侧栏草稿持久状态。发送时 `POST /api/v1/ai/chat` 可携带可选字符串 `action_receipt_generation_id`；空值或省略沿用默认最近回执。显式来源必须是当前已保存会话中具有操作建议的持久终态 generation，非法格式/无会话返回 `422 AI_ACTION_RECEIPT_SOURCE_INVALID`，来源缺失、错配、不保存或尚在生成返回 `409 AI_ACTION_RECEIPT_SOURCE_UNAVAILABLE`；拒绝时不创建聊天消息，也不调用模型。
- 服务端按指定来源重新投影最多 16 条、8 KiB 回执，保留 `as_of/limited` 并标识 `source_generation_id`。旧审批不再受“会话最近 16 条建议”窗口影响；只含建议/结果身份、动作、状态、版本、路由及既有异步执行事实，不发送 changes、预览、业务正文、文件或凭据。它是当前会话的操作事实，不是新的工作台权限；不带本条 grant 时仍没有工作台工具。
- 来源随会话/Provider 配置变化清除，发送被接收后消费；下一条消息不继承。若响应丢失，后台恢复在核对同一请求后也会发出只含请求/来源/owner 身份的运行时受理信号，由来源输入器消费附件和本次授权；新输入的草稿不被删除，未修改的已受理侧聊草稿会清除。该信号不持久化或包含消息正文；模糊重试同时比较主/侧 owner，不跨输入器复用命令。原计划“继续此计划”共用保留草稿、清旧授权的准备流程，但不携带审批来源附件。`confirmed` 不等于 Agent 完成、`submitted` 不等于任务完成或验收；固定提示要求先核验、不得重复已登记或被拒绝的操作，变更仍需新的人工确认。

确定性验收覆盖 Project 创建提议→人工确认→精确源回执→重新授权读取 Project→关联 Task 提议，以及无授权下一轮、跨会话/临时/生成中来源拒绝、历史窗口、主侧隔离、草稿保护和权限不继承。API v1 保持兼容，schema 076 不变；此切片是人工接续，不是后台自动调度、自动批准或自动重试。真实 Provider 与原生桌面仍单列验收。

## 结构化任务发现（H2-W，2026-09-20）

单消息 `work` 现在注册 `workspace_tasks`，可组合任务页的状态、优先级、类型、项目/当前客户、标签、计划日期、截止范围、逾期/临期、父子层级与排序条件，直接返回匹配任务的有界摘要。它复用原生筛选与排序函数，同一只读事务返回计数及当前页，提供下一页和窗口限制；模型不必先扫描全部任务再读取每个详情。保存视图定义也可显式作为筛选输入，视图本身不自动应用。

该工具仅返回 Task ID、短标题、状态/优先级/类型、排期/截止、版本、更新时间与路由；关键词沿用任务页在本地匹配标题和描述，响应不含描述、客户资料或产出正文。只有本条已授权消息可用，临时会话允许只读查询。无新 scope、写 API、迁移或执行能力。完整参数与验证见 [任务模块 H2-W](tasks.md#ai-结构化任务筛选h2-w2026-09-20)。执行过程会显示“按条件筛选任务”。

## 项目组合结构化查询（H2-W5，2026-09-21）

本条消息授予 `work` 后，`tasks_projects` 主题可发现只读 `workspace_projects({filters,limit,offset})`。它和原生 `GET /api/v1/projects` 共用状态/归档/名称或描述搜索条件与稳定排序；默认排除归档，显式 `status=archived` 或 `include_archived=true` 可读取历史。`filters` 可设 `q/status/include_archived/sort/client_id`，`sort` 只允许 `name/status/start_date/due_date/created_at/updated_at` 的最多四字段升降序组合，不开放按合同金额排序；`client_id` 还须同一消息有 `clients`，否则拒绝而不查询。`limit` 为 1–20、`offset` 为 0–1000，响应含总数、是否有后页、下一偏移、窗口边界和读取时间。各页是新实时快照，不保证跨页冻结一致，也不是批量动作授权。

工具用同一只读事务计算计数与有界结果，只选择项目 ID、短名称、状态、日期、版本、更新时间、详情路由及由当前关联 Task 实时派生的总数、完成/进行中/剩余、完成百分比和实际分钟；与项目页采用相同任务进度口径。`q` 可在本地匹配项目描述，但结果不回传描述、Client 身份/名称/备注、金额、发票或项目附件；即使授予 `clients` 用于筛选，也不扩大这些输出字段。需要完整项目资料仍须按本条授权另用 `workspace_get(type=project)`；执行过程显示“查询项目组合”。无新 scope、HTTP API、迁移、写入、自动建议/确认或执行能力。隔离测试覆盖原生列表同值、进度、分页、权限、严格参数和脱敏；真实模型选用时机与原生桌面效果未验收。

项目列表页现在有“梳理项目组合”人工交接入口：关键词、状态、客户筛选与页码成为本地 `/projects` URL 状态，AI 往返后可恢复同一视图；URL 仅接受有界关键词、白名单状态、canonical Client ID 和有效页码，重复/非法参数降为安全默认。点击只暂存这组筛选身份、固定提示和返回路由，不复制当前页项目卡片、描述、进度、客户名、合同金额、发票、附件或查询结果；即使列表为空或读取失败仍可准备重新查询。提示要求先读 `tasks_projects` 规则，再按同一筛选从第一页用 `workspace_projects` 重新分页核对；未读页和窗口限制不得猜测。推荐 `work+actions`，仅当前页面选了客户筛选时附加 `clients`；这些只是建议范围，进入主/侧聊天后用户仍要准备草稿、逐项确认单消息授权并手动发送，项目更改另经原确认卡。项目页搜索输入限制 200 字符；无新 HTTP API、scope 或迁移。

## 续办事项精确定位（H5-E22，2026-09-20）

左右续办入口现在将计划定位到原会话并展开计划卡；待确认建议携带原 `session/generation/proposal` 身份，在原会话中直接打开既有确认卡，不依赖最近 100 条消息或逐页加载历史。前端先通过既有 generation 查询核对保存状态及会话归属，再读取该轮建议并仅显示指定 Proposal；原建议已确认、拒绝、过期或缺失时显示当前事实，不替换为同轮其他建议。该入口复用 `AiWorkspaceActions` 的完整预览、额外确认和原决定 API，打开页面不发送消息、不恢复历史权限，也不批准操作。

位置参数由本地 UI 生成，`session`、`generation`、`proposal` 必须是唯一且规范的 UUID；`plan=1` 与审批目标互斥，消费后从 URL 移除。临时会话、身份错配、读取失败及建议缺失都有明确反馈。计划入口优先于上次使用会话并定位到消息区顶部；关闭审批窗口返回原对话，原消息和弹窗共享同一审批事实与缓存。

右侧“查看全部续办事项”会同时展开左栏及续办队列，并在路由切换和组件重新挂载后保留本次 UI 状态。读取失败时保留错误与重试入口；失败来源的陈旧条目与计数不作为当前事实显示，部分成功会标注数量不完整。生成期间计划/审批条目沿用左栏的会话切换限制。无新增 API、schema、scope 或自动调度；验证见 `AiApprovalContinuation.test.tsx`、`AiWorkspaceActions.test.tsx`、`AiAssistantPage.test.tsx`、`AiWorkPlan.test.tsx`、`WorkspaceResources.test.tsx`、`AiSessionRail.test.tsx`。

## 多步计划与显式接续（H5-D，2026-09-19）

保存会话已接通 `workspace_plan` 与主/侧对话共享计划卡。参考 [Codex update_plan 事件处理](https://github.com/openai/codex/blob/main/codex-rs/core/src/tools/handlers/plan.rs)，本项目把计划意图与审批/执行事实分开；不代表自治调度、并发子代理或整体长任务编排已完成。

- 权限：仅本条显式 `work+actions` 且保存会话可用；调用再次核对 Provider 配置、streaming generation 和归属。无授权后续消息没有此工具，计划正文不自动注入上下文；继续须重新授权后 `read`。本地卡片只读不外发资料。
- 工具：`operation=read` 不接受其他字段；`update` 必须给 `expected_version`（首次 0）和完整 `plan:{title,steps}`。标题最多 200 字，1–12 步，每步标题最多 300 字，总 JSON 12 KiB。步骤包含唯一 `id`（小写字母开头、后接字母/数字/下划线/连字符，最多 40 位）、`title`、`kind`、显式 `depends_on`。依赖只允许不重复的前序 ID，拒绝自引用/前向引用/环。
- 分析：`kind=analysis` 必须给 `report=pending|in_progress|reported_done`，不能绑定审批，最多一个分析中步骤。UI 明示模型自报，不证明业务结果；来源 generation 已结束但仍报告分析中时显示“待继续”，不冒充后台仍运行。
- 操作：`kind=action` 不接受 report，可先无绑定，再用 `workspace_propose` 返回的真实同会话 `proposal_id` 关联。禁止跨会话/重复绑定；后续修订不能删除既有步骤、改变种类或清除/替换已绑定审批，重试使用新步骤。显式 `null` 不能伪装成省略字段。读取真实审批及 Run 回执，分别表示待确认、拒绝、过期、排队、运行、待登记、已提交、仅保留、失败或证据缺失。`recorded` 只代表命令登记，`submitted` 只代表产出提交，都不等于 Task 完成或验收；导出批准也不代表下载。卡片展示关联动作名称，标题本身不是完成证明。
- 依赖 readiness 只是计划提示，不是领域执行闸门，不拦截用户在原确认卡的独立决定，不篡改已经发生的事实。计划不批准、执行、授予权限、调度、唤醒或自动发送下一条消息；不能通过勾选完成任务。
- 持久化：schema **076** 新增 `ai_work_plan_revisions`，同会话最多 128 个只追加版本；同 generation/expected_version/完整内容重放返回同版，旧版本不能覆盖新内容。修订与无正文 `ai_work_plan_updated` 审计同事务，失败整体回滚。运行时重建、主/侧对话、后续 generation 读取同一 SQLite 事实，不依赖内存或有损摘要。
- API：`GET /api/v1/ai/sessions/:id/plan` 返回最新计划或 `data:null`；单一 `?version=1..128` 返回指定历史版，缺失为 404 `AI_PLAN_NOT_FOUND`，非法版本为 422 `AI_PLAN_VERSION_INVALID`。包含 session/generation 身份、来源生成状态、版本、创建时间与本次 `as_of`。历史意图不变，证据按本次读取重新投影；只含身份、动作、状态及安全路径，不含审批预览、变更或产出正文。
- UI：展开、上一版、返回最新、刷新回执、查看原 generation 确认卡和具体工作台记录；复用既有审批，不另建弱化确认通道。流式/待确认/执行中定时读取，生成结束刷新；错误明示并可重试，不据旧缓存声称完成。接续仍需用户新消息与重新授权。计划卡根据已存在的真实操作回执给出最小推荐范围：Agent Run 自动包含 `work+outputs+actions+agent_execution`，产出、客户、财务、发票和知识管理动作分别补充对应范围；这只是权限面板的预选，不继承旧 grant、不会自动授予 `agent_files`，也不会发送或执行。主对话与侧边聊天使用同一推荐范围。运行详情会把工作台规则、计划、任务保存视图和知识库元数据查询显示为明确步骤名，不回退成泛化工具调用。非保存会话的授权面板明确提示操作建议与多步计划暂不可用，只保留可单次授权的工作台查询。
- 实时回执（H5-E18，2026-09-20）：`update` 的事务和无正文 `ai_work_plan_updated` 审计提交成功后，当前聊天流额外发送严格 `workspace_plan_updated {generation_id,version,step_count}`；既有确认卡的 Proposal 决定结算时也精确失效该 `sessionId` 的 plan query。主计划卡与右侧 metadata 镜像据此立即重取同一会话已授权的计划，不再等待最多 3 秒轮询；`read` 不发送事件，离线/无流的工具调用仍正常保存计划。事件不含 session ID、标题、步骤、Proposal、审批、产出或任何工作台正文，不新增 scope、读写、自动执行、续办或后台调度。
- Agent Run 投影同步（H5-E19/E20，2026-09-20）：任务页、全局执行页或右侧执行详情观察到 queued/running/pending Run 变为终态后，会取消并精确失效 Agent Inbox、计划 Inbox 和已缓存的会话计划查询；任务页或右侧工作区原生启动、重试、取消命令成功返回时也会触发同一重读。因此 submitted、retained、失败、取消或中断回执不必等到下一轮 3–5 秒计划轮询才反映在主卡、右侧概况和续办角标；取消返回只重读事实，不假定 Runner 已立刻终止。刷新只重新读取本地已授权的 metadata/回执，不创建计划、Proposal、scope、动作、恢复或调度；没有任何 Run 观察者或命令响应时仍由既有轮询收敛。
- 跨会话续办镜像（H5-E21/E22，2026-09-20）：右侧“子智能体（Agent Runs）”同时只读镜像保存会话的计划关注项，以及计划外的待确认建议、待验收提交。每类最多显示三项；计划入口切回原会话并展开最新计划，建议入口切回原会话并定位原确认卡，提交只打开精确任务/提交批次，溢出项展开左侧完整续办队列。右栏镜像本身不显示计划步骤、审批预览、产出正文或工作区内容，也不能在此授权、发送、确认、拒绝、启动、取消、重试、恢复或验收；实际操作仍在原会话的确认卡或原生任务记录中人工完成。
- 数据：计划是会话正文，不写入无正文 `ai_run_steps`；授权面板说明保存与后续重新授权外发。表排除业务 JSON/ZIP，SQLite 备份覆盖，随 generation/session 删除而清理，不撤销领域结果。076 加法迁移不改业务表，v49/v63–v75→76 显式兼容；072–075 排除清单冻结，新表不改写旧包契约。失败事务回滚、修复后可重试；旧程序回退需升级前备份。下一迁移为 077。

确定性证据：`ai_work_plan_test.go`、`ai_work_plan_migration_test.go`、`aiWorkPlan.test.ts`、`AiWorkPlan.test.tsx`。前端测试还验证 Agent Run 计划续办会推荐 `work+outputs+actions+agent_execution`，且主/侧入口均只打开新的单消息权限确认。隔离 mock Harness 验证计划→提议→绑定→人工确认→无授权下一轮→重新授权接续；最大 scope、20 个知识来源及 8 KiB 回执也进入两个 Provider 的 64 KiB 请求预算回归。真实模型规划质量、原生视觉体验仍单独验收。

## Agent Inbox 计划续办队列（H5-E1，2026-09-19）

智能体左栏已增加“续办队列”，聚合同一工作区内每个保存会话的最新计划修订，解决计划只能逐个打开会话查找的问题。本切片是基于计划的 Agent Inbox，不是独立 Run、自动调度器或后台自治队列。

- API：`GET /api/v1/ai/work-plans` 默认 `state=attention`，也接受 `all|running|needs_approval|needs_recovery|awaiting_continuation|completed`；`limit` 为 1–50、`offset` 为 0–999。服务端先选每个会话最新修订，再按本次读取的真实 Proposal/Run/交付回执重新投影，按最新更新时间和稳定身份排序。为避免无界扫描，最多检查最新 1,000 个会话计划，并通过 `window_limited` 明示窗口限制。
- 状态优先级：全部有效步骤 ready 且 satisfied 才是 `completed`，历史替代项单独计数且保留原状态（见 H5-E23）；`output_pending` 或成功但仅 `retained` 的待登记产出为 `needs_recovery`，其后依次为待人工确认 `needs_approval`、排队/运行 `running`、需要新消息接续 `awaiting_continuation`。队列同时返回全部步骤总数、历史替代数、有效已满足数、待确认数、活动数和待恢复数，不能把模型自报、已登记或已提交解释成 Task 已完成或人工验收通过。
- 最小披露：列表只返回 session/plan 身份、标题、生命周期、计数和时间，不返回计划步骤正文、审批预览/changes/fingerprint、文件、模型输出、产出正文或历史授权。打开条目切回原保存会话并展开最新计划（H5-E22），不读取右侧工作区，也不执行任何动作。
- UI：左栏显示需关注数量、加载/错误/空态和分页；活动、待确认或待恢复项短轮询，其余状态低频刷新。会话删除、生成结束和审批决定会失效队列缓存，列表始终以服务端最新投影收敛。
- 显式继续：最新且未完成、没有待确认、排队、运行或待登记 **action 步骤** 的计划显示“继续此计划”；分析步骤的 `pending` 仍表示尚待新一轮分析。H5-E27 接续核验还会检查最新版本、同会话活动生成和相关 generation 的完整建议组，只有通过才填入固定提示、打开既有单消息权限弹窗，推荐计划回执可证明所需的最小范围（至少 `work+actions`）。用户仍须点击“仅授权下一条消息”再手动发送；阻塞时可打开具体原建议组，版本变化须核对后再次点击。无可用 Provider、正在生成或历史版本不提供入口。继续后的模型仍须读取最新计划/工作台状态，不能继承旧授权、自动批准、自动发送或伪造执行证据。

确定性证据：`ai_work_plan_test.go` 覆盖最新版本选择、实时回执投影、状态优先级、过滤、分页窗口与最小披露；`aiWorkPlan.test.ts` 覆盖严格分页/计数契约；`AiSessionRail.test.tsx`、`AiWorkPlan.test.tsx`、`AiWorkspaceAccess.test.tsx` 及主/侧聊天测试覆盖队列选择和“填提示→显式单次授权”流程。H5-E1 本身不包含计划外 Run；下述 H5-E2 已补其可处理项的统一入口，H5-E3 再补计划外待确认操作与当前待验收提交。自动唤醒/调度、并发子代理和真实模型/桌面专项仍待。

## Agent Inbox 独立执行续办（H5-E2，2026-09-19）

智能体左栏“续办队列”现在同时展示最新计划和不属于最新计划的可处理 Agent Run。H5-E2 只补齐发现与精确进入执行详情的断点，不把列表变成后台调度器，也不在队列中自动取消、重试、恢复登记或接受产出。

- API：`GET /api/v1/agent-runs` 新增严格可选筛选 `attention=1` 与 `plan_link=unplanned`。前者只保留 queued/running/failed/interrupted，或 `output_delivery_status=pending|retained` 的 Run；后者通过 Proposal 的实际 `result_id`、所属 generation/session 与该会话最新 `ai_work_plan_revisions` 中的 `proposal_id` 关联排除计划内 Run。只参考最新修订，旧版本不会永久隐藏已经离开计划的执行。
- 去重与分页：去重在服务端计数和分页之前完成，`meta.total` 是两个筛选共同作用后的精确数量；既有 `active_total/pending_delivery_total/succeeded_total` 仍是无视筛选的全局轮询计数。非法筛选返回稳定 400，不降级成未过滤全集。
- 最小披露：列表仍只返回 Run/Task/执行身份、生命周期、交付状态、错误码和时间等 metadata，不返回 `result_text`、输入快照、恢复 staging、计划 JSON、Proposal 预览、文件或历史授权。点击某项后才用 Task+Run 双身份打开既有“执行过程”抽屉。
- UI：续办角标等于计划关注数加独立执行总数；独立执行明确区分排队、运行、失败、中断、结果待登记和结果已保留。首页最多展示 50 项，更多项进入既有右侧执行页；打开详情不切换或发送会话，也不受当前聊天流式状态误阻断。H5-G52 后，统一 Inbox 还投影 `agent_run` metadata；计划内 Run 由统一来源显示，独立执行区继续只展示计划外 Run，角标按来源去重。

确定性证据：`agent_run_output_delivery_test.go` 覆盖计划内/计划外去重、可处理筛选、稳定错误和最小披露；`agentRunDelivery.test.ts` 覆盖筛选参数；`AiSessionRail.test.tsx` 覆盖合并角标、独立分组及精确 Run 抽屉。计划外待确认和当前待验收提交由下述 H5-E3 接续；自动唤醒/调度、自治并发子代理和真实模型/桌面专项仍待。

## Agent Inbox 审批与验收续办（H5-E3，2026-09-19）

智能体左栏同一“续办队列”现已把计划外待确认工作台操作和当前待人工验收的 Task Submission 纳入权威角标与分组。计划内审批仍由对应“智能体计划”承载，服务端在计数和分页前排除其 Proposal，避免同一确认事项重复计数；Run 仍按 H5-E2 独立去重。

- API：新增 metadata-only `GET /api/v1/ai/inbox`，首版支持 `kind=all|approval|review`；H5-G52 追加 `kind=agent_run` 与 `agent_run_total`，保持 `limit=1..50`、`offset=0..999`、按时间倒序及类型/ID 稳定分页。非法 kind 返回稳定 `AI_INBOX_KIND_INVALID`，不降级为全量。
- 待确认：只读取保存会话中 `status=pending` 且未被该会话最新计划修订引用的 Proposal；列表只含 Proposal/action、session/generation 身份、会话标题和创建时间。点击切回原会话及原确认卡，不在列表执行、确认、拒绝或恢复授权。
- 待验收：只读取 Task 当前指针所指、Task 确实处于 `waiting_review` 的 `pending_review` Submission，排除已经被替换的历史批次；列表只含 Task/Submission 身份、任务标题、批次号、来源和提交时间。点击进入精确 `/tasks/:taskId/submissions/:submissionId`，仍由原页面读取正文/附件并进行人工验收。
- 最小披露：响应不返回 Proposal changes/preview/fingerprint、Submission summary/review reason、Artifact、文件、模型输出、历史 grant 或执行结果正文；前端按两类严格键集合解析，额外字段或错序/错计数整体拒绝。生成结束、审批决定、会话删除、Task/Submission/Run 事实变化会失效或短轮询该队列。
- 边界：角标等于最新计划关注数、计划外审批/当前验收数和计划外可处理 Run 数之和。队列仍不是自动审批、自动验收、后台调度器或权限继承通道；它只把已有人工闸门集中到可发现入口。

确定性证据：`ai_agent_inbox_test.go` 覆盖最新计划去重、当前批次筛选、两类计数/分页、稳定错误和正文不泄露；`aiAgentInbox.test.ts` 覆盖严格判别解析及请求参数；`AiSessionRail.test.tsx` 覆盖合并角标、返回原会话与精确验收路径。API v1 / schema 76 不变。

## Agent Inbox 自动化异常续办（H5-E5，2026-09-19）

同一“续办队列”现新增“自动化异常”，把各规则最新失败 attempt 集中到智能体入口。服务端只选择 `status=failed` 且没有后继 retry Run 的叶子失败；已经产生后续 attempt 的旧失败不重复显示。可自动重试、仅可人工重试和不可重试均可见，但列表本身不创建重试。

- API：`GET /api/v1/ai/inbox` 新增 `kind=automation_failure` 与 `automation_failure_total`；`all` 的总数同步包含该来源。条目仅含 Run/Rule 身份、代码所有的规则名称、attempt、是否可重试、下一重试时间、稳定错误码和结束时间。
- 最小披露：不返回配置/动作快照、逻辑键、来源事件、结果摘要、业务对象、审批、会话正文或权限。前端严格拒绝额外字段、错误计数和错序结果。
- 进入详情：点击使用真实 Run ID 打开 `/settings/automation?run=<id>` 的既有运行详情；人工可在原页面核对完整快照和可用操作。打开详情不自动重试、不启用规则、不授权模型，也不把详情内容送入 Harness。
- 边界：该切片只补自动化失败来源；知识索引失败由下述 H5-E6 接续。数据库/Provider 等其他恢复来源、后台唤醒、自动续跑和并发子代理仍未统一进入 Agent Inbox。

确定性证据：`ai_agent_inbox_test.go` 覆盖失败叶子选择、已产生后继尝试的旧失败去重、分来源计数和私有快照/摘要不泄露；`aiAgentInbox.test.ts` 覆盖严格元数据解析；`AiSessionRail.test.tsx` 覆盖合并角标、异常分组和精确 Run 路由。API v1 / schema 76 不变。

## Agent Inbox 知识库索引异常续办（H5-E6，2026-09-19）

同一“续办队列”新增“知识库异常”，把每个未删除来源当前最新、仍然失败的索引 Job 集中展示。服务端要求该 Job 同时是来源按 `created_at/id` 排序的最新 Job，且没有 `retry_of_job_id` 后继；已经重试成功、进入新 attempt 或被独立新索引替代的历史失败不会继续占用角标。

- API：`GET /api/v1/ai/inbox` 新增 `kind=knowledge_failure` 与 `knowledge_failure_total`，`all` 总数同步包含该来源。条目只含 Job/Source 身份、来源文件名、`import|reindex`、attempt、稳定错误码和完成时间；不返回原始 bytes、提取正文、分段、哈希、文件路径或检索内容。
- 严格定位：点击由前端使用已验证 UUID 重建 `/knowledge?source=<sourceId>&job=<jobId>`。知识库页面只在该 Job 仍是此来源的最新 failed Job 时选中并高亮来源；若已重试、删除或身份变化，则明确显示历史位置不可用，不用其他 Job 冒充。定位参数可单独关闭。
- 人工操作：用户仍在知识库原生来源卡中核对错误并点击重试；成功创建新 attempt、重建或删除来源后会失效 Agent Inbox 缓存。列表本身不读取正文、不重试、不删除来源、不授予模型知识或文件权限。
- 边界：这一步补的是失败发现与精确进入原页面，不是索引器自动恢复或智能体自主修复。数据库/Provider 等其余失败来源、后台唤醒、自治调度和并发子代理仍待。

确定性证据：`ai_agent_inbox_test.go` 覆盖当前失败、已产生后继 attempt 的旧失败去重、分来源计数和正文不泄露；`aiAgentInbox.test.ts` 覆盖严格判别响应；`knowledgeIndexLocation.test.ts`、`KnowledgeBasePage.test.tsx` 和 `AiSessionRail.test.tsx` 覆盖安全路由、精确当前 Job、高亮、历史替代拒绝与队列导航。API v1 / schema 76 不变。

## Agent Inbox 对话生成异常续办（H5-E7，2026-09-19）

同一“续办队列”新增“对话异常”，集中展示保存会话当前最新且需要人工处理的 generation：普通失败（`status=failed`），以及 Sidecar 启动恢复将遗留生成标记为 `status=cancelled`、`error_code=AI_GENERATION_INTERRUPTED` 的中断轮次。服务端按每个会话的 `created_at/id` 选择最新 generation；一旦用户已经发起新的生成，不论新一轮仍在运行、成功、取消或再次失败，旧失败/中断都不会继续作为当前事项重复出现。用户主动取消的 `AI_GENERATION_CANCELLED` 不进入异常队列，临时会话也不进入持久续办队列。

- API：`GET /api/v1/ai/inbox` 新增 `kind=generation_failure` 与 `generation_failure_total`，`all` 总数同步包含该来源。条目只含 generation/session 身份、会话标题、稳定错误码和失败/中断更新时间，不返回用户消息、部分回答、思考过程、工具参数/结果、Provider 地址/密钥、历史 grant 或计划正文。
- 进入会话：点击主行仅切回原保存会话；普通失败显示“生成失败”，启动恢复中断显示“服务重启时中断 · 可手动继续”。用户仍须自行核对原会话并决定是否重发；队列不自动重放原请求、不发送消息、不选择 Provider、不恢复权限。该行同时提供无工作台权限的“交给智能体”：只把会话标题、generation ID 和错误码写成排查提示带入 `/ai?session=<session_id>`，并区分未自动恢复的启动中断；不打开工作台权限弹窗，不授予任何 `workspace` scope。
- 收敛：生成终态会失效 Agent Inbox 缓存；页面可见时按既有频率轮询。删除会话会级联移除 generation 并刷新队列。由于选择的是当前最新 generation，不需要为“已恢复”另写重复事实，也不把历史失败永久保留在角标中。

2026-09-23 补强（H5-E39）：启动恢复的 `AI_GENERATION_INTERRUPTED` 现在也可从 Agent Inbox 发现。前端明确说明本轮没有自动恢复，主行仅返回原会话；交给智能体仍为空 scope，提示人工核对后再决定是否手动重发。用户主动取消继续排除；没有自动模型重试、权限继承或新迁移。

## Agent Inbox Provider 配置续办（H5-E8，2026-09-19）

智能体续办队列已能发现会直接阻断对话或 Agent Run 的 Provider 配置问题，不再要求用户先进入设置逐项排查。该来源只覆盖仍存在且状态为 `unconfigured`，或状态为 `unavailable` 且健康状态为 `unhealthy` 的 Provider；`ready+healthy` 与有意停用的配置不占待办。

- API：`GET /api/v1/ai/inbox` 新增 `kind=provider_issue` 与 `provider_issue_total`，`all` 总数同步包含该来源。条目只含 Provider ID、名称、模型、配置/健康状态、稳定错误码和更新时间，不返回 Base URL、API key、`has_key`、版本、评测历史、会话正文或历史授权。
- UI：左栏“续办队列”增加“供应商待处理”；点击主行只打开“设置 → AI 助手”，用户仍须人工补密钥、修改端点或执行连接检查。该行同时提供无工作台权限的“交给智能体”：只把 Provider 名称、模型、配置/健康状态和错误码写成排查提示带入 `/ai?settings=ai`，不打开工作台权限弹窗，不授予任何 `workspace` scope。Provider 创建、修改、删除、密钥保存和健康检查完成后同时失效 Provider 列表与 Agent Inbox 缓存。
- 边界：列表和交接都不自动写密钥、改端点、选择替代 Provider、执行健康检查、重放失败生成或恢复任何单消息权限。排查交接不读取 Base URL、API key、业务记录或评测历史；模型只能给出人工检查步骤，并提示用户在设置里操作。数据库启动恢复、后台自动唤醒、自治调度、并发子代理以及真实 Provider/桌面验收仍待。
- 边界：本切片改善 Provider/生成错误的发现与回到原会话，不是自动重试、后台唤醒或故障自愈。数据库启动故障仍由全局恢复页拥有；自治调度、并发子代理及真实 Provider/原生桌面验收仍待。

确定性证据：`ai_agent_inbox_test.go` 覆盖当前失败、后续成功替代、临时/非当前记录排除、分来源计数和部分回答不泄露；`aiAgentInbox.test.ts` 覆盖严格身份与额外正文字段拒绝；`AiSessionRail.test.tsx` 覆盖角标、异常分组、打开设置和无 scope 交给智能体；`AiWorkbenchHandoff.test.tsx` 与 `AiAssistantPage.test.tsx` 覆盖无权限待办只追加提示、不打开授权弹窗及 `/ai?settings=ai` 深链打开 AI 设置。API v1 / schema 76 不变。

## Agent Inbox 本地 Agent 适配器异常续办（H5-G58，2026-09-23）

统一续办队列新增 `adapter_issue`，把会阻断 Task Agent Run 的本地 Agent 适配器健康/隔离失败集中到智能体入口。数据库约束保证 `enabled` 适配器必然 `execution_ready=1` 且健康、隔离已验证，因此本来源只投影**终态失败**：`health_status ∈ {blocked, unhealthy}` 或 `isolation_status=unsupported`。有意停用、尚未检查的 `unknown` 以及已就绪仅被手动停用的适配器不占待办。

- API：`GET /api/v1/ai/inbox` 新增 `kind=adapter_issue` 与 `adapter_issue_total`；`all` 总数同步包含该来源。条目只含 adapter 身份、显示名、`adapter_key`、启停状态、健康/隔离状态、`execution_ready` 与稳定错误码；不返回 `executable_ref`、manifest、本机路径或构建摘要。
- UI：左栏“本地 Agent 待处理”；点击只打开“设置 → 本地 Agent”。该行同时提供无工作台权限的“交给智能体”，复用既有 `agentAdapterSettingsHandoff`（空 scope），只整理人工排查顺序，不自动登记、检查、启停或启动 Agent Run。
- 边界：本切片只补适配器失败发现；不改变 Adapter 约束、启停事务或 Runner wire。其余尚未建模的失败/恢复来源、自治调度与真实桌面验收仍待。

确定性证据：`TestAIAgentInboxProjectsEnabledAdapterIssuesOnly` 覆盖 blocked 投影、healthy/disabled 排除与 executable_ref/manifest 不泄露；`aiAgentInbox.test.ts` 覆盖严格字段、非法健康状态拒绝及旧 Sidecar 缺省 `adapter_issue_total` 的零值兼容。API v1 / schema 79 不变。

## Agent Inbox 数据恢复诊断续办（H5-E9，2026-09-19）

智能体左栏“续办队列”现在聚合既有启动恢复诊断。当诊断显示 `restart_required`、`cleanup_required` 或 `attention_required` 时，角标增加一个全局事项并显示“数据恢复”；`idle` 与仅表示本次已成功应用的 `restored` 不占待办。

- 权威来源仍是 `GET /api/v1/backups/restore-diagnostics`，不把恢复状态复制进会话、计划或 `/api/v1/ai/inbox`。等待重启期间大部分业务接口继续返回 `503 RESTORE_RESTART_REQUIRED`，但备份列表、幂等恢复重放和这一只读诊断端点可访问，使界面能解释当前门禁。
- 点击“本地数据恢复”主行只打开“设置 → 数据与备份”，由既有页面展示待重启、清理残留、失败隔离和无效条目计数。列表不展示本机路径或底层清理错误，也不读取备份内容。该行同时提供无工作台权限的“交给智能体”：只把恢复状态、失败/无效/残留计数和请求时间写成排查提示带入 `/ai?settings=data`，不打开工作台权限弹窗，不授予任何 `workspace` scope。
- 列表和交接都不自动重启 Sidecar、执行恢复、删除残留、重放失败恢复、选择备份或绕过人工确认；恢复生命周期与安全约束仍由备份模块拥有。智能体只能解释诊断含义和建议用户在数据设置里人工核对顺序，不能猜测本机路径/日志或把建议说成已经处理。后台自动唤醒、自治调度、并发子代理和真实断电/桌面验收仍待。

确定性证据：`TestScheduledRestoreCreatesRollbackAndAppliesBeforeNextDatabaseOpen` 覆盖挂起恢复期间诊断端点仍可读且返回 `restart_required`；`AiSessionRail.test.tsx` 覆盖统一角标、恢复分组、打开数据设置和无 scope 交给智能体；`aiIssueHandoff.test.ts` 覆盖恢复诊断 helper 的空 scope、idle 不显示和禁止自动重启/清理边界；`AiAssistantPage.test.tsx` 覆盖 `/ai?settings=data` 深链打开数据设置。API v1 / schema 76 不变。

## Agent Inbox 系统维护续办（H5-E10，2026-09-19）

智能体左栏“续办队列”现在还能发现工作台 Inbox 中仍为 `open/tracking` 的系统维护事项，覆盖备份创建、校验、演练、恢复安排，数据库启动、迁移、运行期，Sidecar 启动和本地低空间九类既有来源。事项仍由 Inbox 拥有，不复制成会话、计划或新的维护表。

- API：`GET /api/v1/ai/inbox` 新增 `kind=maintenance_failure` 与 `maintenance_failure_total`。服务端只依据代码白名单中的 `source_entity_id` 生成固定 `component / operation / error_code`，返回原 Inbox ID 和创建时间；不读取或返回 title、summary、payload、固定 message、底层错误、本机路径、精确容量、备份 ID 或日志。
- UI：“系统维护”分组显示固定的人类可读标签；点击只打开精确 `/inbox/:id`。原详情继续负责查看固定说明、进入安全设置，以及人工解决或忽略。解决、忽略或重开后，Inbox 写入完成会失效 Agent Inbox 缓存，使角标按真实活动状态收敛。
- 边界：列表不自动重试备份、重启 Sidecar、执行恢复、清理残留、删除文件、修改阈值或处理 Inbox。H5-E9 的启动恢复诊断仍通过独立备份端点读取，因为恢复门禁期间普通业务 API（包括 AI Inbox）可能不可用；两者不互相冒充或重复持久化。

确定性证据：`ai_agent_inbox_test.go` 覆盖真实系统维护投影、分来源计数、过滤及 payload/message 不泄露；`aiAgentInbox.test.ts` 覆盖严格组合与额外字段拒绝；`AiSessionRail.test.tsx` 覆盖角标、分组和精确 Inbox 导航；`inboxHooks.test.tsx` 覆盖处理原事项后刷新 Agent Inbox。API v1 / schema 76 不变。

## 智能体到右侧工作区的受控工具入口（H5-E4，2026-09-19）

智能体回复现在可使用七个固定本地入口：`/ai?workspace=overview|agents|files|review|terminal|browser|managed`。用户点击后才切换到智能体模式、展开右侧工作区，并通过既有标签管理器打开环境概览、子智能体、文件、变更审查、终端、浏览器或任务产出与附件。重复点击仍产生明确的新打开请求；既有标签容量、浏览器标签上限、终端关闭确认和单原生浏览器分栏约束保持不变。

这是 UI 导航能力，不是内容读取或执行授权。解析器只接受一个精确 `workspace` 参数和白名单值；任何额外参数、URL、文件路径、命令、片段、`file/chat` 内部面板或外部地址都会退化为普通文本。打开文件/Git/终端前仍由用户选择本地根目录；终端仍需用户手动启动和输入，浏览器地址仍由用户确认。面板内容不会自动进入模型上下文，点击不创建 workspace grant、审批或业务写入，模型也不能声称面板已在未点击时打开。

确定性证据：`aiWorkspaceLinks.test.ts` 覆盖七类精确入口和注入拒绝，`AiRichText.test.tsx` 覆盖切换 `/ai`、展开右栏及派发准确工具请求，`RightOverview.test.tsx` 覆盖请求落到真实工作区标签；`ai_workspace_tools_test.go` 证明 Harness 提示包含入口且未增加隐式工具授权。真正由模型自动操作文件、Shell/Git、终端或浏览器仍未开放。

## 智能体受控工作区导航（H5-E13，2026-09-20）

除 H5-E4 的人工点击链接外，用户现在可以只为**本条消息**授予独立 `workspace_ui`。该 scope 只注册 `workspace_open_panel`；模型可在流式对话中选择 `overview|agents|files|review|terminal|browser|managed` 之一，Sidecar 以短暂 SSE `workspace_panel` 事件让前端展开右侧工作区，并优先激活已存在的同类标签（没有时才新开）。H5-G46 在不改变单面板语义的基础上，允许附加一个不同的 `companion_panel` 并让两个标签自动分栏；详情见本模块顶部。H5-E4 的人工点击仍保留每次新开标签的语义。

- 严格边界：工具参数只含闭集 `panel`，拒绝 URL、路径、文件、命令、浏览器地址、终端输入和任意额外字段。它不读取或回传标签内容，不枚举文件、执行终端/Git/浏览器/文件系统操作，不创建 Proposal、审批、业务写入或 Agent Run。打开“终端”或“浏览器”标签不等于已启动终端、访问站点或执行命令。
- 授权与生命周期：`workspace_ui` 不依赖 `work`，可用于非持久会话；它既不发送工作台数据给 Provider，也不继承到下一消息。服务端只在当前已接受聊天流中安装 UI 桥；脱离流执行会稳定失败，避免伪称已经打开界面。SSE 事件不写入会话、运行步骤、前端存储或后续模型上下文。
- 前端：只接受当前 generation 的严格 `generation_id + panel` 键集合；面板值或额外字段无效即终止为 `INVALID_RESPONSE`。合法事件只改变本地 UI store 的右栏展开状态和固定标签请求，不能让过期/其他 generation 操控界面。

确定性证据：`ai_workspace_tools_test.go` 覆盖独立 scope、严格 schema、无桥失败和 Harness→SSE；`ai.test.ts` 覆盖严格 SSE 解析；`aiChatLifecycle.test.tsx` 覆盖只对当前 generation 展开固定标签；`AiWorkspaceAccess.test.tsx` 覆盖非持久会话的 UI-only 授权及“不发送工作台内容”披露。API v1 / schema 76 不变；真实 Provider 是否选择工具和桌面端视觉联调仍需单独验收。

## 智能体浏览器导航请求（H5-E14，2026-09-20）

用户可为**本条消息**额外选择独立 `workspace_browser`。H5-E14 当时只注册 `workspace_request_browser_navigation(url)`：模型只能提交一个最多 4096 字节、不含用户名或密码的绝对 HTTP/HTTPS 地址；Sidecar 仅在当前 generation 的 SSE 发出严格 `generation_id + url` 请求，前端先打开或激活右侧浏览器并显示完整地址。H5-E31 后同一 scope 还可请求当前标签闭集动作，见本文件顶部。

- 本地确认门：SSE 不是导航。用户必须在浏览器卡点击“在新标签打开”，桌面版才调用内嵌 Chromium 新建标签，网页模式才由这次点击使用外部浏览器；点击“拒绝”或不确认均不访问网络，流结束也不会自动访问，用户仍可随后确认当前显示请求。桌面原生创建进行中保持请求且禁用再次点击，只有收到成功的原生快照才消费；创建失败保留请求与原活动地址，显示错误后可人工重试或拒绝。达到 12 标签上限时确认按钮禁用，关闭一个后仍可继续。一个 generation 只接受一次待确认请求，避免模型形成导航队列。
- 严格边界：不接受相对地址、非 HTTP(S) scheme、控制字符、账号密码或额外字段；不会读取、截图、提取或发送网页正文、Cookie、登录态、浏览器历史、下载、页面交互或本机路径给 Provider。模型不得声称已打开、浏览、点击、填写或下载；`workspace_ui` 本身仍不能导航浏览器。H5-E31 的工具回执不证明执行成功；H5-E35 仅让用户事后自愿带回无网页内容的本地命令结果。
- 授权与生命周期：该 scope 不依赖 `work`，可用于非持久会话，不继承至下一条消息，不创建 Proposal、审批、业务写入、Agent Run 或持久事件。前端拒绝过期 generation、额外键和非法 URL；同一时间只保留一个待用户决定的请求，防止模型响应堆积本地操作。

确定性证据：`ai_workspace_tools_test.go` 覆盖 scope、严格 URL/schema、无 bridge fail-closed 与 Harness→SSE；`ai.test.ts`、`aiChatLifecycle.test.tsx`、`AiWorkspaceAccess.test.tsx`、`EmbeddedBrowser.test.tsx` 覆盖严格解析、当前流请求、非持久授权、成功后消费、原生失败保留与重试、标签上限及拒绝零导航。API v1 / schema 78 不变；真实网站、远程 Provider、Windows WebView2 和下载/登录态专项仍需人工验收。

## 智能体受控工作台记录定位（H5-E15，2026-09-20）

H5-E15 的基础七类定位在独立 `workspace_ui` 之外，还要求匹配的 `work` 或 `clients` 读取能力，才会注册 `workspace_request_record_navigation(record_type,record_id)`：`task|project|client|inbox_item|reminder|roadmap_milestone|content_item`。Sidecar 重新核对 canonical UUID、对应读取 scope 和记录存在性，再在当前 generation 的 SSE 发出严格 `generation_id + record_type + record_id`；后续新增类型及从属字段见 H5-E16/E17/E30。

- 本地确认门：前端不信任模型提供的 URL，而是从闭集 type/id 自行映射内部 route。它先显示“打开记录 / 留在对话”卡；只有用户点击“打开”才离开聊天，并在可用时追加 `return_session` 以返回原会话。每个 generation 只接受一次待确认请求；拒绝、未确认或流结束不会导航。
- 严格边界：`workspace_ui` 单独使用仍只能打开固定右栏标签；没有匹配读取 scope 时不会注册此工具。工具拒绝 person、任意路由、路径、URL、额外字段、非 canonical ID 和不存在记录；SSE 与前端也拒绝额外键或未知类型。定位请求不包含标题、正文、字段、文件、浏览器状态或本机路径，不新增模型可读资料。
- 授权与生命周期：可用于非持久会话，但不继承至后续消息；它不创建 Proposal、审批、业务写入、Agent Run、面板内容回传或后台任务。模型不得声称已打开、查看、修改或执行记录；记录本身仍遵循现有读取范围与写操作逐项确认。

确定性证据：`ai_workspace_tools_test.go` 覆盖双 scope、严格 schema、canonical ID、无 bridge、无 route 泄露与 Harness→SSE；`ai.test.ts`、`aiChatLifecycle.test.tsx`、`AiWorkspaceRecordNavigation.test.tsx`、`AiWorkspaceAccess.test.tsx` 覆盖严格解析、当前流待确认、保留 return_session、拒绝零导航及授权披露。API v1 / schema 76 不变；真实 Provider、原生桌面路由与跨页面返回专项仍需人工验收。

## 智能体受控 Agent Run 定位（H5-E16，2026-09-20）

H5-E15 的同一个 `workspace_request_record_navigation(record_type,record_id)` 现在可在本条消息已有 `workspace_ui`、`work` 与 `outputs` 时接受 `record_type=agent_run`，让模型在先读取真实执行元数据后请求用户打开该次执行过程；这不是新 scope、路由参数或执行控制面。

- 服务端边界：`agent_run` 仍受 `outputs` 的既有 `work` 依赖约束。Sidecar 以 Run ID 联结现存 Task 重验 canonical 身份和归属，**仅在服务端**推导 Task ID；工具回执仍只含 Run type/id、请求已登记与需用户确认，不含 Task ID、标题、执行输入/结果正文、文件、路径、Provider 配置或恢复 staging。
- 流与前端边界：`workspace_record_navigation` 仅在 `agent_run` 事件额外携带服务端生成的 `task_id`，且前端要求这一种事件严格具备该字段；其他记录事件出现 `task_id` 即拒绝。前端只从该对 Task/Run 身份构造 `/tasks/<task>?agent_run=<run>`，继续保留可用的 `return_session`，不接受模型提供的 route 或 Task 关系。
- 本地确认门：卡片显示“打开 Agent 执行 / 留在对话”；只有用户点击打开才跳转，拒绝、未确认或流结束均不导航。它不打开 Run 正文、不能启动、取消、重试、恢复登记、接受产出、修改 Task 或把执行过程自动带回模型。

确定性证据：`ai_workspace_tools_test.go` 覆盖 `workspace_ui+outputs` 注册、缺少输出范围拒绝、Task 归属服务端推导、工具回执零泄露及 Harness→SSE；`ai.test.ts`、`aiChatLifecycle.test.tsx`、`AiWorkspaceRecordNavigation.test.tsx` 覆盖 Agent Run 特有的严格 `task_id` 解析、待确认状态、精确 route 与拒绝零导航。API v1 / schema 76 不变；真实 Provider、原生桌面路由与跨页面返回专项仍需人工验收。

## 智能体受控任务提交批次定位（H5-E17，2026-09-20）

当本条消息同时有 `workspace_ui`、`work` 与 `outputs` 时，H5-E15 的 `workspace_request_record_navigation(record_type,record_id)` 还可接受 `record_type=task_submission`。它让模型在已通过 `workspace_task_submissions` 读取真实批次元数据后，提出一次“打开这个批次”的本地请求；它不增加 scope、批次正文访问、提交、验收或执行控制能力。

- 服务端边界：`task_submission` 与 `agent_run` 同受 `outputs` 的既有 `work` 依赖。Sidecar 只以 Submission ID 联结仍存在的 Task，重验 canonical 身份，并且**仅在服务端**推导 Task ID；工具参数与回执仍只有 record type/id、请求已登记及需用户确认，不含 Task ID、标题、批次摘要/验收意见、Artifact、文件、路径或任何路由。
- 流与前端边界：`workspace_record_navigation` 只为 `agent_run` 与 `task_submission` 附加服务端生成的 `task_id`；普通业务记录夹带该字段一律拒绝。前端只从该对 Task/Submission 身份构造固定 `/tasks/<task>/submissions/<submission>`，可附加合规 `return_session`，不接受模型提供的 route、Task 关系或额外字段。
- 本地确认门：卡片显示“打开提交批次 / 留在对话”；只有用户点击打开才跳转，拒绝、未确认或流结束均不导航。跳转本身不会读取批次正文或 Artifact、不会提交、接受、返工、启动/停止 Run，也不会自动将详情回传给模型。

确定性证据：`ai_workspace_tools_test.go` 覆盖 outputs 动态 schema、缺少 outputs 拒绝、Submission→Task 服务端推导、零泄露回执和 Harness→SSE；`ai.test.ts`、`aiChatLifecycle.test.tsx`、`AiWorkspaceRecordNavigation.test.tsx`、`AiWorkspaceAccess.test.tsx` 覆盖 Submission 特有的严格 `task_id`、本地待确认、精确详情 route、返回会话与授权披露。API v1 / schema 76 不变；真实 Provider、原生桌面路由与跨页面返回专项仍需人工验收。

## Git 变更审查显式快照交接（H4-AM，2026-09-20）

桌面端右侧“Git 变更审查”在成功读取当前未暂存或已暂存范围后，新增“交给智能体审查”。这是一次由用户点击发起的**内容交接**，不是新的 Harness 工具、工作台 scope 或仓库权限。

- 交接只带当前已显示的仓库标签（不含本机路径）、分支名、未暂存/已暂存范围、`git status` 与文本 diff 的原始 UTF-8 前 2 KiB / 8 KiB；JSON 转义后的字段也分别受相同字节上限约束，因而包含大量控制字符、引号或反斜杠时会更早截断。截断标记随快照进入草稿，不截断半个 Unicode 字符。整个待带入提示限于 24 KiB，代码、注释、分支名和 diff 都按未可信数据处理，不能成为模型指令或授权。
- 点击只暂存 `/ai?workspace=review` 与这份快照并打开既有 AI 入口；主/侧聊天仍要由用户点击“带入 Git 变更”并随后发送消息才会把内容交给所选 Provider。保存会话会按既有聊天规则保存该用户消息，因此卡片明确提示远程 Provider 会在发送后收到附带代码；未点击、取消或未发送时不外发。
- 不授予 `work`、`actions` 或新 scope，不能枚举仓库、续读更多 diff、执行 Git/Shell/终端/浏览器/文件/网络操作，也不会修改、暂存、提交、推送或创建业务记录。模型可以依据已附快照给出自然语言审查，若用户需要后续业务动作，仍须重新选择既有工作台权限并走逐项确认。

确定性证据：`aiIssueHandoff.test.ts` 覆盖 UTF-8 有界快照、无路径与无 scope；`WorkspaceFiles.test.tsx` 覆盖只交接当前原生 Git 读结果且不调用写命令；`AiWorkbenchHandoff.test.tsx` 覆盖用户可见的数据披露和“带入后仍需发送”的闸门。API v1 / schema 76 不变；真实远程模型与桌面 UI 专项仍需单独验收。

## 本地文本预览显式快照交接（H4-AN，2026-09-20）

桌面端右侧的普通文本文件预览在成功读取后新增“交给智能体分析”。它让用户可分析**正在看到的文本**，但不把选中的目录、root ID 或本机路径转为 Harness 文件能力。

- 只带文件名、页面返回的文件字节数与截断状态、以及已显示文本的原始 UTF-8 前 8 KiB；JSON 转义后的文本字段同样不超过 8 KiB，控制字符、引号或反斜杠密集时会更早截断，且不截断半个 Unicode 字符。快照不含 root ID、完整/相对路径、目录授权、文件 ID、文件哈希或任何未显示内容；图片、空文本和读取失败时不显示入口。
- 点击只暂存 `/ai?workspace=files` 与快照并打开既有 AI 入口；主/侧聊天仍需用户点击“带入文本预览”并随后发送，才会外发给所选 Provider。保存会话时遵从既有用户消息持久化规则；未点击、取消或未发送不外发。文件名和文本均是未可信数据，不能成为模型指令或授权。
- 不新增后端 API、migration、workspace grant、Harness 工具或原生 command。模型不能扫描、搜索、刷新、续读、下载、保存或修改本机文件，不能运行 Git/Shell/终端/浏览器/网络操作，也不能把分析建议说成已经修改代码或文件；需要业务操作仍要另选现有 scope 并逐项确认。

确定性证据：`aiIssueHandoff.test.ts` 覆盖文本快照的无路径、无 scope、UTF-8 和 JSON 转义字节边界；`WorkspaceFiles.test.tsx` 覆盖只交接当前已显示的文本预览，不携带相对/本机路径；`AiWorkbenchHandoff.test.tsx` 覆盖数据披露与“带入后仍发送”的闸门。API v1 / schema 76 不变；真实 Provider、桌面点击和包含敏感文本时的人工决策仍需单独验收。

## 手动终端输出显式快照交接（H4-AO，2026-09-20）

桌面端右侧手动 PowerShell 终端在已经收到输出后，新增“交给智能体分析”。这只解决用户手动复制诊断文本的断点；它不把正在运行的 PTY、工作目录或当前用户的 OS 权限交给 Harness。

- 前端仅在本次终端标签已接收到非空输出后保留最近 16 KiB 的解码文本流，用于用户点击时构造快照；较早的前端捕获内容被丢弃、或原生 1 MiB 输出环已丢弃旧内容，都会在快照中明确标记。交接本身只取该尾部的原始/JSON 转义后最多 8 KiB，保持 Unicode 边界；终端 ID、root ID、工作目录、路径、输入缓冲、环境变量和未捕获输出均不进入快照。终端输出可能包含命令、控制序列或提示注入，一律是不可信数据。
- 用户先点击“交给智能体分析”，再在主/侧聊天交接卡点击“带入终端输出”、最后发送，内容才会到所选 Provider；未点击、取消或未发送都不外发。交接卡明确提示这是近期有界输出，且不含终端身份或命令输入；保存会话时仍只遵循既有用户消息持久化规则。
- 不新增后端 API、migration、workspace grant、Harness 工具或原生 command。模型不能读取后续输出、读取滚动历史、启动/写入/调整/关闭终端，不能控制 Shell、文件、Git、浏览器或网络，也不能把输出中的命令当作已执行；需要真正操作仍须由用户在手动终端完成。

确定性证据：`aiIssueHandoff.test.ts` 覆盖最近输出、UTF-8 尾部及快照边界；`WorkspaceTerminal.test.tsx` 覆盖只有收到原生输出且用户点击后才暂存、不会泄露 root path 或 PTY ID，也不会写入终端；`AiWorkbenchHandoff.test.tsx` 覆盖用户可见披露与第二道发送闸门。API v1 / schema 76 不变；真实 PowerShell/远程 Provider 和敏感输出的人工发送决策仍需单独验收。

## 当前网页地址显式交接（H4-AQ，2026-09-20）

右侧内嵌 Chromium 的当前 HTTP(S) 标签现在提供“交给智能体分析”。它只弥合“用户正在看的网页与对话草稿”之间的上下文断点；不是浏览器工具，也不会把 WebView2 或页面内容变成模型能力。

- 用户先点击浏览器工具栏入口，前端才从**当前活动标签**解析一个地址。解析结果先移除查询参数、片段和账号信息；非 HTTP(S)、带用户名/密码、原始或百分号编码控制字符、或脱敏后超过 2 KiB 的地址不会提供交接。页面标题、正文、截图、Cookie、登录态、历史、下载、原生标签 ID、工作区 root 和本机路径都不进入快照。
- 交接只在内存暂存 `/ai?workspace=browser`、脱敏地址和固定提示词。主/侧聊天卡会再次显示实际将带入的地址与边界；用户必须点击“带入网页地址”，随后仍手动发送，才会把它作为普通用户消息交给所选 Provider。点击、关闭卡片或不发送均不会外发。
- H4-AQ 本身不新增后端 API、migration、workspace grant、Harness 工具或原生命令。模型不能从该地址交接自行访问、抓取、截图、点击或填写网页，不能读取 Cookie、登录态、历史或下载，也不能控制网络、文件、Git 或终端；需要打开新网页仍走 H5-E14 的单消息 `workspace_browser` 和本地确认门，当前标签受确认的有限操作另见 H5-E31。

确定性证据：`aiIssueHandoff.test.ts` 覆盖 URL 脱敏、快照结构、scheme/credentials/上限拒绝；`EmbeddedBrowser.test.tsx` 覆盖只有活动标签的人类点击才暂存、且不触发导航或浏览器动作；`AiWorkbenchHandoff.test.tsx` 覆盖用户可见地址、能力披露和“带入后仍发送”的闸门。API v1 / schema 76 不变；真实网站、WebView2、远程 Provider 和包含敏感路径时的人工发送判断仍需单独验收。

## 当前对话计划回到右侧执行工作区（H4-AP，2026-09-20）

右侧 `子智能体（Agent Runs）` 标签除全局执行账本外，会显示当前**主对话**已保存计划的紧凑状态卡。这样在查看执行、结果登记或重试时，用户不用猜当前 Run 与会话计划处于哪个阶段；它是现有 H5-D 计划的只读镜像，不是第二套计划或调度器。

- 卡片只读取当前 `activeSessionId` 的既有 `GET /api/v1/ai/sessions/:id/plan` 结果，并显示计划标题、版本、已满足步骤计数及从实时步骤状态投影的“待继续 / 等待确认 / 正在执行 / 结果待恢复 / 已完成”。H4-AP 的后续补强在同一只读响应中列出未被替代的步骤标题、状态及前置步骤提示，打开 Agent Run 详情时仍保留此卡；分析状态明确为模型自报，操作状态来自真实回执。它不显示审批预览/变更、文件、产出正文、历史授权、输入快照或右侧本地工作区内容；没有有效主会话或没有计划时不显示卡，读取中显示等待、失败时显示错误与人工重试，不以旧缓存伪造状态。
- 步骤关联真实 Proposal 时，卡片使用服务端回执的 generation/proposal 身份生成原会话精确建议入口；只有经过本地白名单验证的回执 route 才显示工作台记录入口。“在主对话查看并继续”仍回到原计划。用户必须在原计划卡显式点击“继续此计划”、重新选择本条 scope 并人工发送；待确认、待恢复、取消、重试、登记产出和验收仍分别在已有确认卡或原生执行详情完成。右侧卡不能发送消息、授予 scope、确认 Proposal、启动/取消/重试 Run，亦不改变 Task 状态。
- 继续沿用同一 React Query plan key；待确认、排队、运行或待登记 action 以短轮询收敛。没有新增 HTTP API、Harness 工具、workspace grant、原生 command、业务事实或迁移，API v1 / schema 76 不变。

确定性证据：`WorkspaceResources.test.tsx` 覆盖当前会话计划、未替代步骤、精确回到原建议/工作台记录、失败重试及不触发 Run 恢复/取消。真实 Provider、原生工作区和长任务恢复体验仍单独验收。

## 受人工确认的产出登记恢复（2026-09-19）

主/侧对话在本条 `work+outputs+actions+agent_execution` 下可提议 `agent_run.recover_output`，只接受真实 `task_id/agent_run_id/expected_version` 和空 `changes`。先读取全局或任务执行元数据及当前 Task 版本，仅 running+pending 可提出。此命令复用持久结果，不重新调用模型、不重新发送输入文件，因此不要求 `agent_files`；它不是 `agent_run.retry`。

确认卡展示原 Run/attempt、结果类型和字节数，预览额外冻结 SHA-256。正文和哈希不进入工具结果或会话回执；须独立勾选 `confirm_agent_output_recovery`，执行/文件/停止同意不能替代。确认时重算精确预览；Task、Run 或待登记结果变化拒绝旧卡。另一入口已登记也拒绝未确认旧卡，读取精确结果即可，不能再次重跑。

审批决定、Run 交付状态、Task/Submission/Artifact 及事件共享固定连接外层事务，文件在提交成功前不视为已交付。回调失败、COMMIT 失败或请求取消均回滚，补偿未被数据库引用的文件且保留 pending；相同确认重放不重复产出。成功回执只读实际 Run：submitted 定位精确提交批次，retained 定位原 Run 并说明未登记原因，二者都不表示任务完成或人工验收通过。删除后的缺失 Run 仍显示历史 tombstone。

确定性证据见 `ai_agent_run_recovery_test.go`、`agent_run_output_recovery_command_test.go`、`aiAgentRunRecovery.test.ts` 及共享确认卡测试。API v1/schema 76 不变；多步计划与显式接续由上节 H5-D 交付；完整自治编排及真实模型/桌面专项仍待。

## 受人工确认的执行重试（2026-09-19）

主/侧对话现可在本条 `work+outputs+actions+agent_execution` 权限下提出 `agent_run.retry`，参数为真实 `task_id/agent_run_id/expected_version` 与空 `changes`。只接受 failed/cancelled/interrupted，拒绝活动、pending 与 succeeded；确认不是执行成功。原生与 AI 复用重试准备命令，保留原 Task/Assignment/Actor/Adapter/Provider/config 身份及输入、输出契约。任何漂移拒绝，不静默更换模型或文件。

确认卡同时展示原 Run/attempt 与下一次完整执行快照；须重新勾选 `confirm_agent_execution`。v2 重试在准备文件前检查本条 `agent_files`，有输入文件时另须 `confirm_agent_files`，不继承上次文件许可；再次调用 Provider 可能再次产生费用。确认事务重算并严格比较预览，将审批、新 Run、排队事件一并提交，之后才启动；新 Run 的 `parent_run_id` 指向原 Run，重复确认不新建第二次尝试。原记录保留，结果按真实新 Run 显示并定位，成功仍不等于任务完成或验收。

确定性覆盖：`ai_agent_run_retry_test.go`、`aiAgentRunActions.test.ts`、`AiAgentRunActionCard.test.tsx`。此项不提供自动重试循环或任意文件权限；pending 登记恢复由上节独立命令处理，不走重试。完整多步编排与真实模型/桌面专项仍待。API v1 / schema 76 不变。

## 受人工确认的执行停止（2026-09-19）

主/侧聊天在单消息 `work+outputs+actions+agent_execution` 下可提议 `agent_run.cancel`。先用执行查询取得真实 Run/Task 身份，再读取当前 Task 版本；参数仅 `task_id/agent_run_id/expected_version` 与空 `changes`。它不是 `task.cancel`，不取消任务、不删已有产出，也不撤回已外发的模型请求或保证停止远程计费。

卡片冻结 Task 标题/版本、Run ID、queued/running 状态、attempt、Provider ID 和 model，必须独立勾选并发送 `confirm_agent_cancel=true`。确认时重读并严格比较；任务/执行状态变化、已有停止请求、已终态或 pending 产出均使旧提议不能执行。审批决定、取消意图、queued 的终态事件同事务；提交后才通知运行中的执行器，失败整体回滚，重复确认只回放并可补发取消信号，不建新 Run。

`confirmed` 只代表已接受停止请求。回执只读原 Run 安全元数据，running 显示“停止请求已接受 · 等待执行器收尾”，cancelled 才显示“执行已停止”；卡片轮询精确 Run 并保留返回原对话的详情入口。所属 Task 及 Run 被删除后显示历史 tombstone，不编造当前状态。回执不含输入、产出正文或 staging，也不继承权限。前后端确定性证据见 `ai_agent_run_cancel_test.go`、`aiAgentRunCancel.test.ts` 与 `AiAgentRunActionCard.test.tsx`。重试与登记恢复已由上文的独立命令接通；多步计划与显式接续见 H5-D，真实模型与原生桌面仍需单独验收。

全局执行查询：单消息 `work+outputs` 授权提供 `workspace_agent_runs`，不需要启动执行权限。按生命周期、交付状态、可选精确 `task_id` 与可选 `parent_run_id` 组合筛选，返回有界任务标题、状态、安全错误码、创建时间、可空 `started_at/completed_at`、`attempt`、可空 `parent_run_id` 和精确 Run 详情 route；这些 lifecycle/lineage 元数据用于区分排队、执行中、已结束待登记以及首次执行与重试链，不读取执行快照、产出正文或恢复 staging。每页 1–20（默认 10）、offset 0–1000；`total` 为筛选数，`global_counts.active_total/pending_delivery_total` 不受筛选影响，计数和列表同事务读取。超过查询窗口以 `has_more=true/window_limited=true/next_offset=null` 明示，不能声称已查全集。模型可引导用户从 Run 详情手动处理失败或待登记结果，但此工具不取消、重试或恢复登记；pending 不能重跑模型。主/侧聊天进度均识别该查询与既有执行资格查询。

产出复查读取支持可调页长：`workspace_get(type=artifact|agent_run)` 接受 `content_limit`（1–4000 个 Unicode 字符，默认 4000），用于在 JSON 转义触及 24 KiB 工具结果预算时缩小页面，按 `next_content_offset` 连续读取。其他类型拒绝此参数；权限、文件字节隔离、软删除保护和结果预算保持原规则。分页结束只证明读取完毕，不代表人工验收或任务完成。确定性用例见 `TestAIWorkspaceOutputPageCanRecoverFromEncodedBudget`。

> 目标版本：待定（独立于 v0.1–v0.4）。Provider、流式会话、Harness/记忆、显式工作台/知识能力、受人工确认的领域命令、H5-A AI→Agent Run 启动、H5-B 可恢复产出登记、H5-C 受控文本文件输入与有界多文件输出、H5-D 持久计划，以及 H5-E1/E2/E3 续办和 H5-E4 受控工具入口已交付；H5-G1 增加另行授权的有界后台计划续办，H5-G12 仅为明确选择的安全等待态提供普通重启后重验。运行中未知请求、物理备份恢复、并发子代理、其他失败/恢复来源的一体化 Inbox、任意工具执行和真实模型/桌面专项仍待。
>
> 实现基线：API v1 / SQLite schema 79；`079_ai_access_request_complete_scopes.sql` 已在备份门禁后应用。回退必须使用升级前的完整回滚备份，下一条迁移从 `080_*` 开始。

## 定位与边界

AI 助手是问答、摘要和工作台操作建议入口。模型输出本身不执行操作；显式授权后可起草任务、项目、收件箱、本地提醒与专注暂停/继续命令，用户逐项确认才通过共享领域事务落地。

- 以 Provider 配置式接入大模型：远程 API（API key 模式）或本地部署（OpenAI 兼容端点，无需密钥）。协议注册表首批支持 `openai_chat`（OpenAI chat/completions 流式）与 `anthropic_messages`（Anthropic messages 流式）；本地部署固定走 `openai_chat`。
- API 密钥只保存在操作系统安全存储（首版 Windows 凭据管理器，`zalando/go-keyring`）；不进 SQLite、`localStorage`、日志、命令行、诊断包或前端持久化。无可用安全存储的平台明确 503 拒绝，不落盘退化。本地部署 Provider 不需要也不保存任何密钥。
- 聊天由 Sidecar `internal/harness` 运行时驱动（LLMClient 接口 + 运行循环 + 工具注册表 + 预算）。默认只有 `memory_search / memory_write / memory_propose`；[ADR-029](../adr/029-agent-workspace-capabilities.md) 增加单次显式授权的工作台读工具、只提议不执行的 `workspace_propose`，以及 H5-A/H5-C 仅在独立 `agent_execution` 下注册的 `workspace_agent_execution`。H3-D2 另以 knowledge 来源范围授权提供 `knowledge_search / knowledge_read`；没有任意文件系统、Shell、Git、终端或浏览器控制工具，不能继承人工作业区权限。
- 远程 Provider 请求可包含系统提示、长期记忆、摘要/事实、最近回合、当前输入、memory_search 命中、预览确认的 Task/Project/Client 最小快照和最多 3 个知识 chunk，以及本次授权范围内的工作台查询结果。手选快照仍是每类最多一项；授权工具可以分页按需查多项。客户联系方式与任意文件系统正文不在工具白名单；H5-C 的例外只允许经独立 `agent_files`、opaque 候选映射和双重人工确认，把当前 Task/Project 范围内最多 4 个受控 UTF-8 文件发送给所选执行 Provider。远程 Provider 会使这些字节离开设备，本地 Provider 只发往配置的回环 origin。财务仅在独立 finance 授权下读取 H3-E1 字段白名单，不读取客户名称、备注、作废原因或 PDF。
- 助手回复只读，不直接创建、修改或删除任何业务数据；模型输出视为不可信预览。
- 语义建任务：模型先给自然语言待确认说明，再给 `[opc:task]{...}[/opc:task]`。前端隐藏控制块、允许编辑；用户明确确认后调用 `/ai/messages/:id/task-confirmation`，服务端复用 Task 领域规则原子创建与绑定（固定 todo），按消息身份幂等。显示“尚未创建/已创建”以服务端事实为准，已创建卡片跳转 `tasks/:taskId`；旧误闭合格式兼容，null/数组/错类型/多块/非法/未闭合内容均不崩溃或直接建任务，改显自然语言失败说明。
- 当前不做：未经本次授权读取工作台或知识库、Shell/任意文件系统/Git/终端/浏览器自动控制、静默业务写、自主多步代理、AI 自建 Inbox 来源或自动建任务。H5-E4 仅允许用户点击白名单链接打开本地工具标签，不改变这一边界。任务、项目、收件箱受理分诊与本地提醒等操作已支持逐项人工确认；H5-A/H5-B/H5-C 只在独立权限、冻结预览和人工双确认后启动一项受控 Run，并完成受控输入、文本/单文件或 2–4 文件结果与人工验收链。其他工具调用、更完整业务操作和多步编排仍按 ADR-029 分阶段实现，不能把当前切片标为整体强化完成。
- AI 未配置、密钥无效或端点不可达时，所有既有核心模块完全可用；AI 失败不投影 Inbox。

## 当前实现状态

H3-D1 已补齐知识引用核验往返：已验证来源卡链接真实 source/document/chunk 及版本，在知识库弹窗中读取原片段并返回原会话；PDF 显示提取页码，历史解码不再拒绝 PDF，旧文本缺页码兼容为 1/1。不存在或版本变化时提示，禁止拿新版代替旧证据；人工查询不调用模型、不继承权限。H3-D2 已接入来源范围授权的知识检索/读取及运行级引用，详见 [ADR-011 接续](../adr/011-ai-validated-knowledge-citations.md)。

H3-D2 使用 `workspace.scopes:["knowledge"]` 与 `knowledge_sources:[{source_id,expected_source_version}]`，可单独或组合其它范围，必须 1–20 个来源。Web 授权弹窗本地分页选来源；写前和接受事务内版本检查，变化返回 `AI_KNOWLEDGE_GRANT_CHANGED`；工具执行再次检查来源、Provider 与恢复状态。搜索 1–256 字、每页 1–5 项/offset 至 1000，单次完整读取要求三层 UUID 与文档版本；共最多 8 次成功知识结果、24 KiB JSON。只有经 Executor 接受的完整读取参与引用验证，搜索摘要不能引用；引用仍最多 3 个，无证据/缺块/越权可见。来源授权与版本进入幂等输入和无正文事件；动态正文不另存 context_snapshot。已保存及非持久会话来源卡均复用 D1；后者由 H4 的 SSE/内存回读补齐，不落正文或引用，无新 schema。

H4 临时引用反馈已接通：SSE `done` 附加服务端校验的 `citation_status/citations`；完成态 GET 从持久消息或短时非持久内存读取。后者最多 32 项/每项 16 KiB/15 分钟，读写淘汰、删除会话/恢复清除，绝不保存回答或工具正文。前端在 20 回合/8 MiB 内存预算中保留引用，主对话与侧边聊天共用来源卡及版本化核验链接；已收片段保持 `incomplete`，来源卡说明只对应完整服务端回答。过期、重启或旧服务缺字段显示不可恢复，不能按 not_requested 处理；字段非法或终态 generation 错配走失败恢复，不开启任何业务确认权限。实现证据：`ai_generation_recovery.go`、`ai_generation_citations_test.go`、`aiGenerationCitations.test.ts`、`aiChatLifecycle.test.tsx` 与 `AiCitationEvidence.tsx`。完整正文续传与真实模型/桌面专项仍待。

本轮可靠性契约由 [ADR-025](../adr/025-ai-reliability-confirmations-and-evaluation-identity.md) 接续，SQLite schema 068–069；整体回归结果见 [AI7 验证记录](../plans/ai-quality-gates.md)。应用级恢复、原子确认和临时记忆隐私不再依赖组件局部状态。

H4-B 已接通无正文实时进度：模型调用、工具、自检修订与引用核验在开始/结束时发送 `progress` SSE；成功提交另发保存标记。generation GET/按请求 ID/活动列表返回同序号 `progress`，活动用内存副本，终态投影已有 run steps；不新增迁移、不存工具参数或结果。主对话和侧边聊天共享当前步骤/耗时与展开列表，替换回答和切页不丢状态；断流未结算步骤显示“结果未确认”，停止后回读真实取消状态。进度纳入原内存预算，不写浏览器持久存储；提议完成仍需人工批准。旧服务缺字段兼容、非法字段/错序/回退失败关闭；准确字段、上限、测试与隐私约束见 [ADR-012](../adr/012-ai-run-steps-and-local-metrics.md)。更多跨模块定位、长任务恢复与计划编排仍待。

上游瞬时故障现已自动重试：连接中断、408/429/5xx 与“未产生任何内容的提前 EOF”在**尚未输出任何字节**时最多重试 2 次（退避 300ms→900ms，`Retry-After` 只接受秒数且上限 2s）；一旦有正文、推理或工具字节到达就不再重试，避免重复内容。`ErrTimeout`、提示超限、响应预算、截断/过滤、鉴权或参数类 4xx 不重试；父 context 结束时取消优先，返回 context 错误。重试是运行期可观测事实：实时进度（模型/自检步骤）携带 `retry_count` 与稳定原因码 `retry_reason`，主/侧运行详情显示“已自动重试 N 次”；终态 `ai_run_steps` 不加列，因此刷新后只保留步骤与耗时，不声称重试历史可回放。确定性证据：`modelclient/retry_test.go`、`harness/harness_test.go`、`AiRunProgress.test.tsx`；真实 Provider 抖动仍需实机验收。

H3-E1 增加独立 `finance` 查询授权：`workspace_finance` 可读取收支/发票白名单及显式币种、1–366 个自然日期内的统计，与收入页共用聚合。工作事项/客户权限不隐含开放财务，另有普通 actions 也不开放财务写入；H3-E2 另以 finance+finance_actions 提供收支创建/修改/作废提议，全部需要独立人工财务确认。结果不含备注、客户名称/联系资料、作废原因或 PDF；记录和报告链接保留精确身份/范围，可返回原会话，人工查看不向模型传递详情。见 [财务模块](finance-invoices.md#ai-财务查询与往返h3-e1)。

H4-E 增加自动化规则/运行的精确往返：`workspace_automations`、审批卡和无正文回执携带真实 `/settings/automation?rule=<id>` / `?run=<id>`；重试确认后定位实际新 attempt。主/侧对话只接受严格允许列表链接，UI 添加来源会话身份；同组件详情支持尝试链和返回，重新查询时不展示过期结果，错配/缺失不替换记录。人工查看快照不会把正文送给模型，不自动确认或重试。模式往返保留报告及记录参数；无新 schema 或模型权限，详见 [自动化模块](automation.md#规则与运行精确定位h4-e已实现源码与确定性测试)。

首个纵向切片已交付（代码与测试为据）：

- **Provider 管理**：schema 052/055/068；登记、PATCH、健康、密钥与删除均有 API。`version` 保护 HTTP 并发，`config_version` 标识真实请求配置；健康和名称变化不递增配置身份。已有会话/评测历史保护删除，不隐式删历史。准备/密钥读取与收尾使用短 Provider 锁，不跨模型或健康网络等待；同 origin 重定向与密钥脱敏边界不变。
- **Agent Harness**：最多 8 轮、32 次工具调用；单工具 30 秒/64 KiB/panic 隔离。模型轮、工具结果和最多一次自检修订共享 generation 的 10 分钟/累计 1 MiB 预算。两协议支持多工具及参数跨帧；记忆三工具默认注册，工作台读工具和领域提议工具按单次权限注册并在执行时再检查。工具失败可回填纠错（上限 3）；selfcheck 缺失/非法记录 unavailable，不假装通过，不充分最多一次修订；修订错误传播并保留已有部分回答。
- **工作台查询（ADR-029 H1）**：输入区“工作台权限”按 `work/clients/outputs` 勾选，仅用于下一条消息，切换会话或 Provider version 失效，接受后清除。工作事项覆盖任务、项目、收件箱、本地提醒、路线图、内容日历；客户独立授权，产出要求 work+outputs。搜索/列表返回分页标记、状态、版本与真实详情 route；详情使用字段白名单。Artifact/Run 文本按 4,000 Unicode 字符分段读取，软删除产出不可读，file 只元数据。系统不额外保存工具原始参数/结果；回答及会话事实遵守 persist。模型返回的白名单 UUID 详情链接和固定 /focus 页面链接可进入工作台，不冒充已验证 citation。
- **本地大模型**：`kind=local` 只允许 `http://127.0.0.1` 或 `http://localhost` 的 OpenAI 兼容端点，拒绝 userinfo/query/fragment，协议固定 `openai_chat` 且无密钥；健康和聊天均不走代理，并拒绝跨 origin 重定向，保证 prompt 不随 307/308 离开配置的回环 origin。
- **长程上下文（G1–G5 + ADR-025）**：最新 200 条按完整 user turn 和最终 64 KiB 请求预算装入，助手控制块剥离。窗口外正文超过 16 KiB 后后台压缩；单批 JSONL ≤32 KiB，大回合按 UTF-8 有界分段，持久 offset 不跳过未处理正文。每次触发最多 8 批/10 分钟，剩余标 pending，下次成功聊天继续。summary ≤4 KiB、五类 facts ≤32/4 KiB；替换快照/水位线/脱敏事件同事务，失败/取消/配置变化不推进。API/UI 区分运行、滞后、失败/取消及部分消息；完整计数不含只覆盖一部分的消息。
- **记忆与工作状态**：持久会话的 memory_write/search/propose 分别记录会话事实、查当前会话记忆/历史、提出待确认偏好；非持久会话只在当前运行共享内存中完成三项操作。永久记忆仍须用户另行明确确认，才进入 `ai_memories` 并注入后续会话（前 20 条、8 KiB）。schema 069 保存提议的 confirmed/rejected 与 memory_id，忽略/确认均可刷新回读，旧无 proposal_id 卡片按消息身份兼容。事件不记录内容，提示词与工具集仍代码所有。
- **会话与消息**：schema 053/069。`persist=false` 不保存消息、generation 正文、会话事实、记忆提议或摘要；请求身份与状态等无正文元数据可保存。应用内草稿/部分回答在内存中；storage 只保留恢复 ID。删除会话先取消生成/压缩，再清理 AI 操作态。
- **流式聊天**：`POST /api/v1/ai/chat` 返回 opc-ai-sse-v1 meta/progress/delta/reasoning/replace/done/error/cancelled；首 token 90 秒、generation 总 10 分钟/1 MiB、提示 64 KiB，每 Provider/会话并发 1。应用级状态支持切路由继续与全局停止；硬刷新/断连取消原连接并回读真实终态。接受过的 Idempotency-Key 不重发新消息。无完成事件 EOF、截断、过滤为失败，持久会话保存失败部分回答；非持久会话不落正文。
- **任务确认**：`POST /api/v1/ai/messages/:id/task-confirmation` 接收既有 Task 创建字段，复用共享领域创建函数，在一个事务内创建 Task 并保存消息静态引用。消息 ID 是服务端稳定确认身份；相同载荷返回真实 Task/完整 Message，改参 409，任务已删除 410，不重复创建。旧 `/messages/:id/task` 保留只挂接兼容。普通 Task 创建没有 `task_created` 事件，本次不新增虚构事件，沿用父级协调及其事件规则。
- **前端**：`/ai` 提供历史分页、Provider 切换、流式回答/停止、推理折叠、任务与记忆确认卡片，并显示“已压缩前 N 条消息”；会话事实每 15 秒刷新。会话列表改由应用外壳承载：进入 `/ai` 即智能体模式，左列整体替换为智能体侧栏（列顶工作台/智能体切换与工作台同一分段控件，位于品牌块之上；品牌块复用头像、显示名与整列隐藏开关，不再使用第二枚 Agent 字标；两侧左栏均不显示版本号，运行版本只在「设置 → 关于」中读取本地服务后显示；新会话、定时任务、项目、续办队列入口沿用工作台导航行样式，左栏不再提供会话搜索，`对话` 分组与按今天/昨天/更早分组的会话行，底部 AI 助手设置与工作台设置行同形），当前会话写入共享 store 供对话区读取，切换会话会清空未确认的任务草稿。左列与工作台同为 220px，主列顶栏与工作台页头同为 64px。从模式切换进入时右侧工作区先收起，由对话页头「打开右侧工作栏」再打开；展开后只在工作区工具栏收起，对话页头不再重复收起按钮。展开后为统一多标签工作区（文件、Chromium、Git 只读审查、手动终端、侧边聊天、子智能体、受控附件与环境信息），通过顶部 + 新建标签，支持关闭、键盘切换、双工具分栏、放大/还原。宽度统一默认 680px、范围 360–1200px，宽屏为聊天保留至少 360px；不足 961px 时打开工作区临时占满主面，收起即可回到聊天。收起后仅在聊天主列宽度至少 1320px 时显示 256px 悬浮状态卡，不挤偏对话轴线。原生能力只在桌面可用，具体权限、文件类型和生命周期见桌面模块与 ADR-028。助手回复不再渲染头像与名称行，只保留推理折叠、运行详情与正文，元信息时间不再逐条显示。设置「AI 助手」区管理 Provider、密钥、健康、pending 记忆建议的来源会话/标签/确认/忽略，以及已确认长期记忆。主对话输入区不再显示页脚免责声明（生成内容仅供参考、工作台逐项确认、远程外发或本地回环边界）；这些边界仍由授权面板、上下文预览和逐项确认卡披露。页面视觉统一由 `agent-workspace.css` 中限定于 `.app-shell-agent` 的亮/暗中性令牌承载：与工作台同宽的会话栏、平面导航、独立滚动会话列表与固定设置入口，白色圆角工作面、无头像助手正文、灰色用户气泡；消息和输入框共同使用最大 760px 的居中轴线，悬浮卡不再预留右侧 padding 挤偏聊天。输入框为 22px 圆角轻阴影卡片，上下文入口位于左下、模型选择与黑白上箭头发送键位于右下，保留停止、输入法组合与 Enter/Shift+Enter 行为。任务卡、推理区、运行详情、记忆、引用、上下文预览、右侧页签及浏览器控件使用统一边框与间距。已完成回复提供复制按钮，只复制显示后的自然语言正文，不复制隐藏控制块，失败会提示手动复制。业务工作台与 portal 设置弹窗不继承该主题；远程外发提示及任务/记忆确认边界不变。会话与项目之间当前没有数据关联，因此智能体侧栏仍按时间分组，不按项目归档。审查与手动终端现由可信主 WebView 调用 Rust native commands；AI 没有这些能力。侧边聊天使用独立会话与草稿，但主/侧共用单次生成控制器；默认不读取文件、Git、终端或网页，只有发送该条消息前显式选择的 workspace grant 会注册相应业务工具，发送接受后即清除且不继承。持久 assistant message 产生的工作台提议在原侧聊消息下逐项确认/拒绝；主/侧若同时打开同一 generation，则复用同一服务端提议与决定状态，不复制审批事实。旧任务控制块和长期记忆建议仍在主对话处理。
- **显式业务上下文（AI5）**：Composer 复用 TaskSelect/ProjectSelect/ClientSelect，最多各选一个。`POST /api/v1/ai/context/preview` 返回目标 Provider、本地/远程边界、全部白名单字段、截断标记、版本和字节数；用户确认后才能随下一条消息发送。Chat 在任何会话/消息/generation 写入前按 Provider/source version 重建同一快照，变化返回 `AI_CONTEXT_PROVIDER_CHANGED / AI_CONTEXT_CHANGED`。发送成功清空一次性选择，失败保留；历史 user message 展示实际发送来源卡片。
- **显式知识上下文（AI6）**：同一面板调用本地知识搜索，用户最多逐段选择 3 个 chunk；preview 展示完整实际正文、来源、source/document version、行/字符位置、Provider 外发边界和组合字节数。Chat 在任何 AI 写入前两次重验三层 identity 与版本，v2 `context_snapshot` 保存实际发送片段；历史展示 `来源 · Lx–y`。此路径不授予 knowledge tool；动态工具需另按 H3-D2 授权。代码所有提示把片段标为不可信引用并要求引用来源/行号。
- **可验证知识引用（AI7-Q1）**：有知识上下文时模型只声明实际使用的 `chunk_ids`；Sidecar 对本次 allowlist 做严格 JSON/UUID/重复/数量校验，并从已验证上下文重建来源、版本和位置。schema 060 `citations_snapshot` 保存 `validated / no_evidence / missing / invalid` 与最多 3 个引用，不复制正文；历史 UI 展示已验证来源或差异化质量警告。Raw citation control block 在持久化、历史 prompt 和流式 UI 中剥离。
- **运行步骤与本地指标（AI7-Q3）**：schema 061 `ai_run_steps` 保存 generation/model turn/记忆工具/self-check/citation/persistence 的状态、耗时、字节和稳定错误码，不保存正文/凭据。schema 062 只在 Provider 返回完整非负 usage 时保存 input/output token 与 `token_source=provider`；OpenAI 读根 usage，Anthropic 合并 start/delta，缺失/部分保持 unknown，不强制 stream_options。历史时间线与当前会话用量均按需展示；ADR-024 将终态根步骤按 UTC 日派生 1–30 天连续趋势，7/30 天面板切换不改变累计 totals。用量 API 可按 session/provider 聚合终态、活动、coverage、原始 token、bytes 与 duration；不新增表、不估算 token、不计算费用。
- **显式本地模型质量评测、分层套件与人工决定审计（AI7-Q2）**：schema 063–068。当前 dataset v4/24，仍有 smoke 8、full 24 和四个 topic 各 6；只显式运行 ready/healthy 本地 Provider。真实生产系统提示与 Harness，单邮箱 Actor 支持幂等/进度/取消/恢复/删除；只保存无正文指标。配置已知组按 Provider ID/config_version/dataset/suite 隔离，旧 NULL 身份保留原名称/模型分组及混合版本规则，不改写原评审记录。有限事实别名、否定/冲突与 FACT_MISSING/FACT_CONTRADICTED 和结构/关键词检查分层；后者为严重 code，自动通过仍不替代人工。full 才可能形成候选，smoke/topic 只诊断；owner 决定只追加审计，不改变 Provider/业务状态。
- **业务导出边界**：十二张 AI 表（含 schema 072 的 `ai_action_proposals`、076 的 `ai_work_plan_revisions`）、知识库操作表及 Agent Run 排除便携业务 JSON/ZIP，一致性 SQLite 备份覆盖。schema 076 只添加排除的会话计划表；兼容图包含 v49/v63–v75 递归迁入 v76，072–075 历史清单冻结，未来 schema/降级拒绝。后续迁移从 `077_*` 起。

多供应商支持（已交付）：可登记多个 Provider，聊天页手动切换。AI5/AI6 上下文预览绑定 Provider version；切换或配置变化会使旧确认失效。尚未实现：句子级 citation/引用覆盖率、更多事实覆盖/发布阈值、多 Provider 自动路由/并存生成、费用、非 Windows 平台安全存储验证。

### H2-E1 提交与验收事实查询

已授权 `work+outputs` 的消息新增 `workspace_task_submissions`，按 Task 查询提交批次（含历史、仅摘要和 child_rollup）、分段读取摘要/验收意见，或查看指定批次的产出元数据。Task 状态、批次状态和当前指针独立返回，避免把“有产出”“当前批次”误当验收通过；已删正文、文件字节和路径仍不可读取。主/侧对话进度复用“查询提交与验收记录”，授权只作用于本条消息。具体分页/字段/隐私契约见 [Task 模块](tasks.md#ai-提交批次读取h2-e1源码与隔离测试已实现)。此工具只读；提交和审核须走下述独立人工决定。

### H2-F 项目笔记

单条消息 work 授权后可读取真实项目下的笔记列表及按 Note 版本固定的完整分段正文，明确披露手写资料可能发送给当前模型。保存会话 work+actions 支持新建、修改、带原因软删除提议；完整前后正文与 Project 状态/版本由人工确认，删除额外勾选。原生接口与审批共用笔记领域事务，保留 Project 版本触发器、归档只读、软删除历史、重复确认与回滚。主/侧卡校验完整预览并取消旧查询后刷新，下一轮只回传实际结果身份。

H4-G 已把工具结果、已有笔记卡片和实际审批回执接到 `/projects/:projectId?note=:noteId`。项目页通过既有单条详情 API 独立读取并核对 Project+Note，不依赖列表分页或筛选；已删除笔记只显示删除元数据且正文为空，失效或错配目标不以其他笔记替代。来源会话只能由 UI 按显示消息追加 `return_session`；打开、重试或返回不会确认建议、调用模型或恢复 work/actions。未确认的新建建议没有 Note ID，仍只打开所属项目，确认后才使用实际结果身份。契约与证据见 [Project 模块](projects.md#ai-项目笔记桥接h2-f)；不开放附件或任意文件能力，API v1 / schema 73 不变。

### H4-H 业务记录与来源对话往返

主对话、侧边聊天的 Markdown 链接、审批卡及计划卡共用基础记录返回链：`/projects/:id`、`/clients/:id`、`/inbox/:id`、`/inbox?reminder=:id`、`/roadmap?milestone=:id`、`/content-calendar?item=:id`。`aiWorkspaceHref` 只按显示消息所属的合法 Session 追加 `return_session`；模型指定的返回身份、额外参数和外部地址仍不能通过链接白名单。

内容、里程碑、Inbox 和提醒详情弹窗直接提供“返回原对话”，加载或读取失败时也保留。内容/里程碑关闭详情后，返回身份保留在 URL，列表页继续提供入口；提醒分类、关闭及已触发事项跳转沿用同一身份。里程碑详情中的关联 Project 链接也传递该身份。返回会明确选择来源会话，不自动发消息、恢复单次权限或执行命令；正在提交内容、提醒或 Inbox 修改时返回入口禁用。项目/客户页复用已有入口，无新 API、表或事实副本。

确定性证据包括六类链接失败复现与白名单测试，消息点击→异月内容详情→弹窗/列表返回，里程碑关联 Project/失败/关闭返回，以及 Inbox/Reminder 准确目标、读取失败和保存中禁用；主、侧来源分别覆盖。实际模型和原生桌面体验仍需单独验收。

### H4-I 从工作台事项进入智能体

Client 详情提供“交给智能体”。入口仅接受 canonical UUID，暂存 `/clients/:id`、当前显示名称/状态和固定提示词，再进入 `/ai`；推荐 `work+clients+actions`，但不复制标题以外的联系字段、备注、活动/回访正文、附件、项目列表、财务/发票、Provider、会话或授权，不发模型请求，不修改业务。不存在或非法身份不显示入口，当前读模型仍是唯一业务事实。客户详情下的具体活动与回访另提供精确“交给智能体”：只暂存 `/clients/:clientId?activity=:id` 或 `?followup=:id`、推荐 `work+clients+actions`，提示模型先用 `workspace_client_records(type=activity|followup,view=detail,id=...)` 读取并核对 client_id，再起草待确认建议；不会把活动正文、回访结果、联系资料或授权一并复制。路线图里程碑、内容条目、本地提醒、Inbox Item、Task、Project 和 Client 详情已升级为下述 H4-R/H4-S/H4-T/H4-U/H4-V/H4-W/H4-X 专用交接，不再走通用记录提示词。

主对话输入区与右侧工作区 `WorkspaceChat` 输入区都会显示“来自工作台的事项”卡，可返回准确业务地址、暂不带入，或先使用现有入口新建会话。用户点击“带入问题并选择权限”才把包含类型/ID 的读取要求追加到对应输入框原草稿，并打开本条消息授权面板；旧草稿不被覆盖，旧 grant 和已选/确认的业务、知识上下文会清除且在卡片中明确告知。任务等推荐 `work+actions`，客户额外推荐 `clients`，推荐不等于授权。权限仍覆盖面板披露的业务范围，不是仅限这条记录的能力；人工可以取消、调整范围，再明确授权并点击发送。右侧文件预览、浏览器、终端或 Git 面板内容不会因交接进入提示词或 Harness。开始生成后才由既有 Harness 读取最新事实、提出待确认操作，逐项审批边界保持不变。

没有可用 Provider 或对话正在生成时，待办身份保持且准备按钮禁用，仍能取消。临时会话过滤 `actions`，仅能明确授权查询，不绕过保存会话的审批要求。Task 未保存字段/版本冲突、Content Item 未保存字段或尚未提交的任务选择、Reminder 草稿/取消确认、Inbox 编辑/操作草稿，以及这些界面的在途写请求会阻止交接，避免静默丢失当前编辑；不声称替代整个应用的离页保护。

确定性测试覆盖通用身份与安全路由、无效输入/旧取消不覆盖新待办、原草稿和权限隔离、主/侧输入区准备交接、未就绪/生成中/临时会话，以及客户活动/回访精确交接的 route、scope、prompt 与零业务执行。浏览器已验证现有 Task 详情 → 待办卡 → 权限面板及取消，未授权、未发送、未改业务。初次实机检查发现旧后端停留在 schema 71；2026-09-19 经用户授权、空闲检查、完整备份及二次校验后已升级到 76，计划/操作建议接口恢复 HTTP 200，页面不再显示相应读取失败。完整真实模型与最新原生桌面运行验收仍待，不以入口及服务升级验证替代。代码沿用 API v1 / schema 076，交接入口本身无新后端接口或迁移。

### H2-E2 提交与验收人工决定

保存会话同时授权 `work+outputs+actions`，可提议 `task.submit_output` 的摘要与 text/link/structured 产出，或针对精确当前批次的 `task.review` 接受/返工。人工卡展示完整摘要、正文、责任归属及状态影响；需用户另外勾选 `confirm_task_output`，文件仅元数据且明确要求人工检查，不以模型建议替代验收。确认前重验完整预览，与原生 API 共用领域事务，保留责任结束、父链、Inbox、历史及文件补偿边界。超过完整预览容量拒绝而非截断。

主/侧对话统一执行后回读、取消迟到旧查询，并刷新 Task/Today/Project/Inbox、分派、批次及 Artifact 详情。回执的结果 ID 是 Submission，版本是 Task，卡片通过实际 Task+Submission 身份链接精确历史批次，不把提交当任务完成或 Agent 执行。参数、容量和测试见 [Task 模块](tasks.md#ai-提交与验收命令h2-e2源码与隔离测试已实现)。H4-F 已接通批次只读页、按需产出核验/人工下载与返回来源对话；模型地址不能携带返回会话，界面添加实际来源身份，错配/缺失/过期缓存不替代目标。H5-A 的执行委派是下述独立权限与确认链路；真实模型/桌面验收仍待，不据此宣称整体产出编排完成。

### H2-D 任务责任操作

work 范围新增 workspace_task_assignments 当前/历史分页，返回 Task 版本和真实责任身份；workspace_task_options 可按 assignee/reviewer 筛选候选。仅保存会话的 work+actions 支持 task.assign/reassign/unassign 提议，逐项确认后与人工 API 共用分派事务，保留历史、父任务待验收协调、幂等决定和工作台缓存刷新。完整人工预览绑定原记录、角色、双方身份及当前子任务/验收快照，变化拒绝旧卡；不读取人员备注、不联系人员、不启动 Agent，不自动通过验收。主/侧对话均可用，具体参数和失败边界见 [Actor 模块](actors.md#ai-独立轨道的责任分派h2-c2--h2-d)。

### H5-A 受人工确认的 Agent Run 启动

`agent_execution` 是只对保存会话开放的单消息权限，自动绑定并要求 `work+outputs+actions`；接受发送后立即清除，切换会话、Provider 或 Provider version 后失效。文件预览、Git 差异、终端输出、浏览器页面、上一轮权限和审批回执都不能转成执行权。模型只能通过 `workspace_agent_execution` 查询安全资格，再提出 `agent_run.start`；动作只允许真实 Task/Provider ID 与版本，不能控制 Assignment、Actor、Adapter、attempt、parent Run、模型、endpoint、凭据、execution contract 或同意字段。

服务端与原生入口共用 Run 准备/创建命令，生成含 Task/完成条件/验收策略、责任、Agent、Adapter、Provider/config 与执行契约的完整本地冻结预览；该预览不作为工具结果回传。确认卡明确 10 分钟/64 KiB 有界结果、成功不等于完成，且必须另勾选 `confirm_agent_execution`。确认事务重新准备并 exact compare，漂移或已有活动 Run 均拒绝；审批决定、Run 与 queued 事件同事务提交，commit 后才 launch，重复确认不会再次启动。schema 074 保存冻结版本并提供数据库唯一门禁。

初始回执使用 `/tasks/:taskId?agent_run=:runId`；UI 从当前消息追加 `return_session`，任务页按 Task+Run 身份独立 GET 并核对，不以最新列表记录替代。下一轮回执不含执行正文、endpoint、凭据或 manifest，也不恢复范围。H5-B 完成登记后，submitted 回执优先改用精确 `/tasks/:taskId/submissions/:submissionId`；只要 Run 仍存在，其他状态指向精确 Run。Task 最终完成继续由原验收策略决定；终态 Task 删除后的回执例外见下文。详细执行边界见 [本地 Agent](local-agents.md)。

### H5-B Agent Run 可恢复产出登记（前后端已实现）

确认卡和执行抽屉从服务端 `AgentRun` 回读 `output_delivery_status`、`output_delivery_error_code`、`submission_id`、`artifact_id`，并严格校验状态组合：排队/普通执行及失败、取消、中断为 `not_ready`；事务待恢复只能是 `running + pending + AGENT_OUTPUT_DELIVERY_PENDING`；成功终态必须是带双 ID 的 `submitted` 或带原因码的 `retained`。非法组合不降级展示，尤其不会把单独的 `succeeded` 解释成已提交。

服务端成功 finalize 在一个外层事务内核对冻结 Task/Assignment/Actor 和当前 review/status；可提交时复用 Task output 领域命令，原子创建 Submission/Artifact 并落 `succeeded + submitted`，领域漂移则原子落 `succeeded + retained`。查询/事件/写入等瞬时故障整体回滚并先尝试持久化为 `running + pending`，绝不暴露 succeeded+无 disposition；若完成事务和首次 pending 写入都暂时失败，Worker 只在当前进程内保留有界正文并退避重试，首次 staging 落库后才可由重启恢复，落库前的进程退出/掉电仍是残余窗口。Agent 是 Submission submitter/Artifact producer，system 是 recorder/event actor；Task 只进入 waiting_review，不自动完成。启动先重试 `running + pending`，再返回并 launch `queued + not_ready`，只把失去执行进程且无 staging 的 `running + not_ready` 标为 interrupted；重复/并发恢复不重复产出，也不重跑已完成模型。

确认回执重建把 Run 当作可消失的实时事实：Task 只在所有 Run 终态后允许硬删除，聚合删除会级联 Run；Proposal 的 confirmed 决定及 `result_id/result_version` 继续保留，但保存会话后续轮次遇到缺失 Run 时安全省略 `agent_run_result`，不会因 `record not found` 使整份回执或聊天失败，也不会从历史审批载荷恢复结果正文或 staging。Web 只把这一精确 confirmed+缺失实时结果形态解析为 `route=""` 的 tombstone，显示“执行记录已删除/无法再读取运行状态”，不渲染详情链接、不调用 `getRun`、不启动轮询，也不把历史 confirmed 当作当前成功证据。

存续 Run 的已确认卡按精确 Task+Run 每 2 秒 GET；任务详情 Run 列表也持续读取其中的活动 Run。queued/running 或 delivery pending 时继续，进入 submitted/retained 或其他终态后停止；任一入口首次观察到新的终态签名时，统一刷新 Task、Submission、Artifact、Assignment、Event、Today、Project、Inbox、搜索、任务 Run 列表和全局 Run 概况，并按 QueryClient+Run 终态签名去重，避免确认卡与任务页重复刷新。右侧工作区的全局 Run 概况只读取不含 `result_text`、输入快照和 staging 的 metadata-only 摘要；响应 meta 在同一只读事务返回当前筛选的 `total`，以及不受筛选影响的 `active_total/pending_delivery_total/succeeded_total`。右栏直接显示权威计数，Agent 页签提供待登记快捷入口、严格服务端交付状态筛选与分页；全局 active 或 pending 非零时每 5 秒轮询，避免较旧 pending 被当前页的新终态记录掩盖。点击条目后才取精确详情并展示正文/动作。页面刷新后仍从 action 回执与精确 Run API 恢复显示；若已是上述 tombstone，则不发任何 Run 请求。submitted 链接真实 `/tasks/:taskId/submissions/:submissionId` 并明确“待人工验收”；retained 仅显示安全自然语言原因并链接原 Run；两者都不自动验收。

pending 只提供人工点击的“重试登记产出”，调用 `POST /api/v1/agent-runs/:id/output-delivery/retry`；轮询本身永远只 GET，不会无限自动 POST。200 回写精确 Run 并刷新上述事实，503 保持 pending 与结果留存，409 回读最新状态。解析、精确路由、fake timer 轮询停止、缓存失效和人工重试由 `agentRunDelivery*.test.*`、`aiAgentRunActions.test.ts`、`AiAgentRunActionCard.test.tsx` 与 `AgentRunDrawer.test.tsx` 覆盖。

### H5-C Agent 受控文件输入与有界多文件输出（前后端已实现）

`agent_files` 是只对已保存会话开放、仅当前消息有效的额外 scope，依赖 `work+outputs+actions+agent_execution`；它不单独开放任何工具。发送被接受后立即消费，切换会话、Provider 或 Provider version 失效，上一轮审批/回执和右侧文件、Git、终端、Chromium 内容都不能变成文件授权。未选择文件且输出为 `text` 时，后端把显式空数组/等价 no-op 规范化掉，继续生成 H5-A 的 v1 preview/Run；选中受控输入或要求单份 `file` 输出时使用 v2；要求 2–4 份 `files` 输出时使用 v3。

模型调用 `workspace_agent_execution` 时只看到本 generation、同一 Task 有效的 opaque `candidate_id`，以及来源种类、名称、MIME、字节数、创建时间等有界元数据。候选只来自当前 Task 的活跃 file Artifact，或该 Task 当前 Project 的活跃 Attachment；服务端最多列 20 个可用候选，实际一项 Run 最多选择 4 个。模型不能提交或推断真实文件 ID、路径、SHA-256、正文、consent、Actor/Adapter/Assignment、endpoint 或凭据。同一 generation 对同一 Task/文件身份重复查询复用稳定 opaque ID；跨 generation、跨 Task、未知或重复 ID 均拒绝。

服务端在 proposal 预览时解析 opaque 映射并冻结真实引用、名称/MIME、大小/SHA-256 与输出契约；v2 持久快照不保存路径或文件正文，v3 同样只冻结 2–4 个输出文件的名称/MIME 与顺序。输入文件单个最多 64 KiB、合计最多 128 KiB；全部结果聚合最多 64 KiB，必须非空、UTF-8 且无 NUL；prepare 和真正 launch 前都从受控 store 复核来源关系、完整性、大小与哈希。Task 改绑 Project、文件改名/替换/删除/篡改、Provider/Assignment/Task 漂移或畸形 v2/v3 快照都 fail-closed，不能把旧候选静默替换成新文件。retry 新建 attempt，但复用父 Run 冻结的文件引用顺序和输出契约。

人工确认分成两个独立事实：`confirm_agent_execution` 同意启动，存在输入文件时另需 `confirm_agent_files` 同意发送文件正文；一个不能代替另一个，拒绝时也不能携带同意标记。确认卡显示冻结文件元数据、总量、Provider 本地/远程和明确离机披露：remote 会把字节发送到设备外，local 只进入本机回环执行链。旧 v1 持久提议没有 H5-C `leaves_device` 字段仍按“无文件、text、字段不适用”兼容回读和确认，不会因为升级后的新增展示字段永久变成 `AI_ACTION_PREVIEW_CHANGED`。

输出契约允许一份 `text`、一份 `file`，或严格 2–4 份 `files`，聚合结果上限均为 64 KiB。AI 不能指定文件名或 MIME；单文件由服务端按 attempt 冻结 `agent-run-attempt-N.md` / `text/markdown`，多文件固定为同 attempt 的 Markdown 与 JSON。v3 模型只返回按冻结顺序排列的 JSON 字符串正文数组，不能提交文件对象、路径、名称或 MIME。交付先进入受控 staging，再复用既有 Submission/Artifact 事务：多文件在一次待人工复核 Submission 中生成多个 file Artifact，首个 Artifact 仅保留为 Run 的兼容 ID，事件保存全部 ID。Run succeeded 不等于 Task done。schema 075 继续以有界 `result_text` 保存恢复副本；v3 的副本是严格 JSON 数组并在恢复时重验，提交后的受控 Artifact 是文件交付权威；这不是向模型或列表新增结果正文权限。

模型审批回执保持最小披露，只含 Run/Task/Provider、状态、attempt、delivery 状态/安全错误码与可空 Submission/Artifact ID；不含候选映射、输入引用/哈希/正文、输出正文、路径、endpoint 或凭据。任务 Run 精确详情提供服务端冻结的输出元数据；v3 按该契约逐份预览/下载，不将内部恢复数组直接渲染为单段正文。H5-F2 的 v4 原生精确详情另返回供人工返工重试确认的完整返工上下文与冻结输入 metadata，不进入列表或模型回执。活动 `queued/running` Run 冻结引用会阻止来源 Task Artifact/Project Attachment 软删除及含该附件的 Project 永久删除；畸形 v2/v3/v4 快照使删除检查 fail-closed。真实 Provider 服从性、远程外发观察、原生桌面交互与断电窗口仍须单列验收。

### 工作台工具契约

额外支持 `actions` 单次范围（依赖 `work`）以及独立的 `finance_actions`、`invoice_actions`（各自依赖 `finance`），均只开放 `workspace_propose`，不开放业务执行工具；三种操作权限不相互继承。H5-A 的 `agent_execution` 另依赖 `work+outputs+actions`，只增加安全执行资格查询与 `agent_run.start` 提议；H5-C 的 `agent_files` 再依赖这四项，只让同一工具返回 generation-scoped opaque 文件候选并允许提议受控文件契约，真正启动/发送文件仍只能由服务端人工确认端点触发。仅已保存会话可用，非持久会话在接受前返回 `AI_ACTION_PERSIST_REQUIRED`。建议保存于 schema 072 创建、073 扩容的 `ai_action_proposals`，与只读工具原始结果不落库的规则分开披露。

### 工作台操作确认（ADR-029 H2-A–F、H3-A–E5）

- 输入区勾选“操作建议（逐项确认）”并发送请求。模型先查真实记录，再调用 `workspace_propose({action,task_id?,project_id?,project_note_id?,inbox_item_id?,reminder_id?,focus_session_id?,client_followup_id?,client_activity_id?,expected_version?,changes})`；单实体创建不带既有目标 ID/版本，`task.batch_create` 例外，须带所属现有 Project 的真实 ID/版本；其他既有记录动作必须带当前版本。每次回复最多 8 项，相同载荷复用同一卡。
- 临时会话禁用操作建议复选框并说明原因，但可继续单独授权只读范围；过期或失败生成的建议仍可手动拒绝收尾，不能确认执行。
- 双协议去重：同一 generation 存在工具建议时，不再显示旧 `[opc:task]` 确认按钮，旧创建接口也返回 `409 AI_ACTION_USE_PROPOSAL`；已拒绝、已过期或已执行的工具建议不能通过旧卡重新创建。查询未完成或失败时也不开放旧入口；无工具建议的历史消息继续兼容。
- 当前 Task action 为 `task.create/update/delete/start/block/unblock/complete/cancel/reopen`；create/update 支持标题、说明、类型、优先级、项目、父任务、计划日期、截止时间、预计分钟、完成条件和完整 `tag_ids` 集合。update 禁止直接修改状态；验收策略按 [H2-X](tasks.md#ai-既有任务验收策略切换h2-x2026-09-21) 的 todo/无提交历史门禁允许人工确认切换，并预览可能的待验收协调。标签先由 `workspace_task_options(type=tag)` 取真实 ID/颜色/版本；`[]` 明确移除全部标签，不表示“不改变”。同一候选读取支持 `tag.create/update/delete` 的真实身份：创建要求名称/颜色，修改只接受名称/颜色，永久删除以空 changes 冻结解除关联任务数并要求人工 `confirm_tag_delete`。确认时重验完整标签预览并复用原生 Tag 事务，删除不删除 Task。父任务先由 `workspace_search/get(task)` 读取真实 ID，`null` 表示移到顶层。Task 确认卡展示标签名称和父任务标题；确认时重新校验标签、Task 版本、父任务存在性/循环及完整预览，并复用原生父链协调。task.delete 只接受真实 Task ID/版本与空 changes，冻结全部本地删除/解除影响，另需人工 `confirm_task_delete`；影响变化或原生引用门禁拒绝执行，文件副作用沿用 trash/恢复补偿，成功回执导航任务列表。block/cancel 需要原因，其余生命周期 changes 为空。Task 创建默认 todo，可选择 manual review。未授权 actions 时仍保留旧建任务确认卡片。
- 前端读取服务端卡片，展示具体前后差异；没有用户点击就不执行业务变更。确认、Task/Project/Inbox/Reminder/Focus/ClientFollowup/ClientActivity 操作、决定与审计同事务；沿用版本冲突、active assignee、manual review、父任务和 Inbox 协调规则。
- `GET /api/v1/ai/generations/:id/actions` 返回 `{data:[...]}`；`POST /api/v1/ai/actions/:id/decision` 接收 `{fingerprint,decision:confirm|reject,...}` 与动作专用同意字段；Agent Run 使用 `confirm_agent_execution`，有输入文件时还要求独立的 `confirm_agent_files`。同意标记仅适用各自指定的确认动作，不可用于拒绝或其他动作。pending 仅在生成 completed 且创建后 24 小时内可确认；失败/取消显示 unavailable，超时显示 expired。拒绝可永久收尾；同决定幂等返回，相反决定 409。
- 成功依据服务端事实，不依据模型措辞；普通领域命令以 confirmed 为执行证据，automation.retry 还必须查看不可变 automation_run_result.status，确认已执行但重试失败不能说成目标已创建。网络模糊失败回读同一卡，可安全重试；不能重新生成不同 ID 当成原操作重试。刷新 Task/Today/Project/Inbox、搜索、分派及产出聚合，链接打开真实目标。版本冲突要求重新查询与新提议，不自动替换旧版本后重试。
- 后续对话会重新读取本持久会话最近审批回执（最多 16 条/8 KiB），作为独立服务端事实层进入 OpenAI/Anthropic 请求、工具循环和自检；不依赖模型摘要。只包含动作、状态、proposal/generation/目标/结果 ID、类型对应的详情 route、执行时版本与决定时间，不带变更正文，不查询实时业务数据。`as_of`/`limited` 表示时间与截断；confirmed 仅证明命令执行，不代表目标现在仍是同一状态。权限面板明确披露后续发送，旧授权不继承，临时会话及其他会话不注入。
- schema 072 加法创建审批表，保存规范化 action JSON、前后预览、generation、SHA-256、status/result_id/result_version/时间戳；输入/预览不可改，决定只允许 pending→confirmed/rejected。删除会话随 generation 清理卡，不撤销已执行 Task/Project/ProjectNote/Inbox/Reminder/Focus/ClientFollowup/ClientActivity/Agent Run。审批表、schema 074 的 Run 执行身份、schema 075 的交付状态及 schema 076 的会话计划修订均排除业务导出但进入 SQLite 备份；兼容 v49/v63–v75→76。失败由迁移事务回滚、修正后重启重试；旧程序回退使用升级前备份，不原地降级。
- 审批事件只有 ID/摘要/结果版本。确定性测试使用隔离 SQLite 与 mock 模型；真实 Provider 质量另验收。H5-A/H5-B/H5-C 已接单次受确认 Run、可恢复产出登记、Task Artifact/当前 Project Attachment 受控文本输入和单文件/有界多文件产出；v3 模型只能按冻结顺序返回 JSON 字符串正文数组，服务端在一次人工验收 Submission 中创建多个 Artifact，首个 Artifact 仅为 Run 的兼容指针。任意文件系统、Shell/Git/终端/浏览器自动访问、跨 Run 自治长任务编排仍未开放；计划与显式接续见 H5-D，不把本片标为整体完成。

#### 项目操作（H2-B / H2-P）

- `project.create/update/start/pause/resume/complete/reopen/archive/restore/delete` 已接入同一审批协议。创建只接受 name、description、start_date、due_date、color、client_id；编辑为这些字段的补丁。项目日期为 YYYY-MM-DD，任务截止时间仍为带时区 RFC3339；合同金额和附件内容不开放，项目笔记走独立 H2-F。现有项目使用顶层 project_id/expected_version；不能混用 task_id。
- `workspace_get(type=project,id=...)` 现在与原生项目详情共用 `availableProjectActions`，返回当前 `available_actions`（仅状态动作）及按 `task_total - task_done` 计算的 `incomplete_task_count`。模型应先读版本和这些当前事实，再起草允许的项目状态建议；列表摘要不代替详情。该列表仅是查询时快照，不授予 `actions`、不保证确认时仍可执行，也不包含客户、金额、发票或附件资料。项目状态/任务聚合变化仍由审批预览和确认事务重验。
- 设置或清除 client_id 必须同时授权 clients；普通项目编辑不读取或预览未改动的客户关联，不外发合同金额。关联客户存在性、已有发票禁止改绑客户、归档项目不可编辑等领域规则在执行事务再次校验。
- 项目状态动作 changes 必须为空，模型不能提供额外同意。complete 卡片显示当前 incomplete_task_count（沿用领域口径，除 done 外均未完成），大于零时必须勾选“保留未完成任务并完成项目”。decision 的可选 confirm_incomplete_tasks 只允许在确认 project.complete 时传入；未同意返回 409 INCOMPLETE_TASKS_CONFIRMATION_REQUIRED。项目聚合版本会随关联任务变化更新，旧卡不能绕过 VERSION_CONFLICT。完成项目不完成、取消或删除任务。
- 人工项目 API 与审批共用 project_commands.go；改名更新关联 Task 版本，complete/reopen 保留客户只读系统活动、完成节点 Inbox 与已启用自动化的投递。审批、项目、事件、来源投影和自动化捕获同事务；捕获成功后沿用既有投递消费与恢复。归档保留原状态、恢复回原状态，不新增 schema。
- 卡片与后续回执使用 /projects/:id，审批响应派生类型安全路由；前端不信任外部 route。决定后刷新 Project、Task/Today、Inbox、Client 活动、Roadmap、Focus、财务关联读模型、Automation 和搜索，网络模糊失败仍回读同一卡。项目 UI 报告归档、发票关联限制和版本冲突，不自动替换载荷。
- `project.delete` 只接受已归档 Project 的真实 ID/版本和空 changes。预览冻结任务、草稿发票、账本解除关联数，以及笔记、附件记录、本地附件文件和完成来源协调数；人工必须另行勾选 `confirm_project_delete`。确认重算完整影响，任何 Project 聚合版本变化使旧卡失效；路线图/内容、非草稿发票、发票关联或已作废账本、活动 Run 文件引用继续阻止。
- 删除执行与人工 API 共用 `project_delete_command.go`。Task、草稿发票和可解除的账本记录保留并清空项目关联；附件文件先进入 trash，事务/审批失败恢复，提交后清理。成功回执使用历史删除 ID/最后版本并导航 `/projects`；审批与备份仍保留，但应用内没有撤销。

#### 路线图里程碑（H2-G/H2-S/H2-AC）

- `workspace_get(roadmap_milestone)` 使用路线图自身读模型，返回真实版本、标题、有界说明、年/季度/目标日、状态、关联 Project 身份/名称和派生 Task 进度。不回传 Task 正文、项目客户/财务资料或 Inbox payload。
- `roadmap_milestone.create/update/archive/restore/delete/move` 已接入逐项审批。新建/修改只接受 title、description、year、quarter、target_date、`planned/active/achieved` status 和 project_ids；现有记录必须带 roadmap_milestone_id/expected_version。归档/恢复/删除 changes 必须为空，不能用 update 伪造 archived；delete 只接受已归档里程碑。
- H2-AC `move` 仅接受同季度非归档源/锚点的真实 ID 与双版本，以及 `before|after`。服务端从完整非归档季度（最多 100 项）计算位置、冻结整组指纹；无变化、跨季度、归档或超限拒绝。用户确认时整组任一成员或事实漂移使旧卡失效；与人工端点共用重排事务，全组重写 `manual_order` 和版本。卡片显示季度、锚点和位置，结果定位源里程碑。不改期、状态、项目/任务关联或 Inbox 来源。
- 预览冻结实际字段和关联 Project；确认时重新构造并逐字比较，版本或关联变化会拒绝旧卡。执行复用路线图的验证、排序、关联、Workflow Event 及 Inbox 到期/达成投影语义，不引入第二份业务事实。
- H2-S 删除预览冻结 `status + roadmap_milestone_version`、Project 关系数和待标记来源事项数，并要求独立 `confirm_roadmap_milestone_delete=true`。确认重算完整预览并调用原生共享删除命令；活动来源事项阻止提议/确认，审批事件失败会回滚里程碑、关系及来源标记。成功回执保留删除 ID/最后版本并导航 `/roadmap`，Project、Task 与 Inbox 审计快照保留，应用内无撤销。
- 确认后刷新 Roadmap/Inbox/Today/Project 相关读模型，普通回执与卡片仅从经验证的 UUID 派生 `/roadmap?milestone=<id>`。路线图排序仍须人工确认；Project/Task 状态变更和无人审批执行仍不开放。

#### 内容日历（H2-H/H2-R/H2-T）

- `workspace_get(content_item)` 读取条目自身读模型，返回真实版本、标题、平台、状态、排期与 IANA 时区、发布确认时间、Project、备注、外链文本、归档来源、required 进度和最多 50 条准备 Task 的 ID/标题/状态/required；超限显式标记截断。外链是 `untrusted_text_not_fetched`，工具不访问网络，也不把内容正文、Inbox payload 或人员资料混入结果。
- `content_item.create/update/schedule/unschedule/review/cancel/reopen/archive/restore/delete/publish/link_task/set_task_required/unlink_task` 接入逐项审批。创建只接受 draft/in_review/scheduled；带排期时必须使用 scheduled。更新只开放标题、平台、Project、备注和外链文本；排期必须同时给 RFC3339 时间与 IANA 时区；普通生命周期和删除动作 changes 为空。现有条目绑定 content_item_id/expected_version，不能混用其他目标身份。delete 只接受已归档条目。关系命令另外绑定 canonical Task ID 与 `expected_task_version`；新增/调整要求显式 `is_required`，解除不接受多余字段。
- 预览冻结实际变更；确认重新读取版本与状态并逐字比较，不接受旧卡或模型替换载荷。执行复用 Content Item 字段/排期校验、乐观版本、Project 存在性及旧 Inbox 来源终结事务；创建仍由既有周期调度器投影到期事项。已确认重放不重复执行，结果从真实 UUID 派生 `/content-calendar?item=<id>`。
- H2-R 的删除预览冻结 `status + content_item_version`、准备 Task 关系数和待标记来源事项数，并要求独立 `confirm_content_item_delete=true`。确认在审批事务内重算完整预览并调用原生共享删除命令；活动来源事项阻止提议/确认，审批事件失败会回滚 Content Item、关系及来源标记。成功回执保留删除 ID/最后版本并导航 `/content-calendar`；Task、Project 和 Inbox 审计快照保留，应用内无撤销。
- H2-T 的关系预览冻结 Task ID/标题/状态/版本、关系状态、required 与 Content Item 前后版本。确认重新读取条目、Task 和关系并逐字比较；新增只允许当前未关联，调整只允许已关联且 required 确有变化，解除只允许已关联。执行与人工 API 共用关系事务，审批事件失败会回滚关系、条目版本和旧 Inbox 投影终结。Task 本身保持原版本与状态。
- H2-AD 允许模型仅在用户明确报告外部已发布后提出 `content_item.publish`。目标必须是未发布活动条目的真实 ID/版本；changes 仅可含用户明确给出的 RFC3339 实际发布时间与外链文本/null，省略时间意味着用户确认时刻。卡片冻结状态、时间和外链，要求用户在外部平台自行核实并独立勾选 `confirm_content_published`；模型不能提供该字段。确认重验完整预览，并与原生入口共用发布事务，内容版本递增和旧 Inbox 来源终结与审批事件一同提交。失败整体回滚、漂移拒绝旧卡；成功定位最新内容详情。没有自动平台发布、外链抓取、手工重排或 Project/Task 状态修改。前端卡片明示关系操作只改准备关系并刷新 Content Item、Inbox 与统一搜索。无新 HTTP API 或迁移；真实 Provider 和原生桌面体验另验收。

#### 客户主档、联系人关系、本地人员与永久删除（H2-I/J/K/Q）

- `client.create/update/delete` 仅在保存会话且本条消息同时授予 `work+clients+actions` 时开放。创建要求 name，可选 contact_name/email/phone/notes/status；修改绑定真实 client_id/expected_version。状态只允许 active/lead/inactive，联系字段可显式清空；删除只接受 inactive Client 的真实身份与空 changes。模型提供任意附件、外发或隐藏 Project 解绑字段会被拒绝。
- `workspace_search(client)` 不返回联系字段或备注；`workspace_get(client)` 和显式客户上下文返回名称、状态、最多 2000 Unicode 字符的备注及当前 active contact 的 link/person ID、名称、类型、状态、版本，并标记备注截断。不返回 contact_name/email/phone、人员备注或 metadata。联系字段只能使用用户明确输入；备注还可来自本次已授权且未截断的完整客户快照。服务端复用原生 Client 校验后冻结规范 changes；完整旧/新值进入本地人工确认卡，下一轮回执不带正文。
- 确认前零业务写入；确认事务重验 Client 版本和完整预览，再复用原生创建/更新事务 helper。业务写入、审批决定和事件同事务，失败全回滚，重放不重复执行。确认结果从真实 UUID 派生 `/clients/<id>`，前端刷新 Client、Project、Invoice、Financial Entry 与搜索读模型。
- `client_contact.link/unlink` 也要求同一权限组合和保存会话。关联只接受候选工具中的现有 active person，拒绝 owner/agent/inactive person、人员创建和第二个 active contact；解除绑定当前 link ID 且必填原因。确认重验 Client version、人员身份/状态及冻结预览，并复用原生关联事务；解除保留 person 与关系历史。结果导航客户页并刷新 Client/Actor/Search。
- `workspace_search/get(person)` 在 `work` 下只返回本地 person 的名称、状态、版本，不搜索或返回已有备注/metadata。保存会话的 `work+actions` 可提出 `person.create/update`：创建固定 active，更新仅名称、状态、备注；备注只能来自用户本轮明确输入。确认前不写入，确认时重验版本和完整预览，再复用原生 Actor 事务。结果不伪造人员详情路由，确认卡提供“人员与责任”设置入口，并刷新 Actor/Task/Client/Inbox/Search。
- person 停用保留原生保护：存在活动 Assignment、active Client contact 或 planned Followup 时确认原子失败，旧提议保持待确认，不自动改派或解绑。需要新客户联系人时先确认 person.create，随后重新读取真实 actor ID，再另提联系人关联；`client_contact.link` 自身不隐式创建人员。
- 永久删除提议冻结 Project 解绑、本地活动、联系人关系历史、附件记录和受控文件删除数量，并要求独立 `confirm_client_delete=true`。发票、收支或任何回访会阻止；确认重算完整影响，复用原生删除事务和附件 trash/恢复补偿。Project/person 保留，成功回执返回客户列表；应用内无撤销，审批/审计保留，备份及外部副本不受影响。owner/system/agent 编辑、账号创建、联系客户、发送消息和任意附件处理仍不开放。API v1、schema 76 不变；真实 Provider 与桌面体验另验收。

#### 收件箱受理分诊（H2-C1）

- 新增 `inbox.create/update/read/snooze/unsnooze/resolve/dismiss/reopen`，现有事项使用顶层 `inbox_item_id/expected_version`，不得混用 Task/Project ID。创建/编辑仅接受 title、summary、priority、due_at，创建固定 manual kind/source/policy、空 payload；不接受来源身份、解决策略、Actor 或直接状态字段。
- snooze 必须填写带时区且晚于服务端当前时间的 snoozed_until；提议和确认时都检查，等待期间已过时的建议返回 422，不能静默调整时间。resolve/dismiss 必须有 1–2,000 字原因，reopen 原因可选，read/unsnooze 的 changes 为空。
- 人工 API 和审批共用 inbox_commands.go，预览复用只读 prepare 校验，确认、领域事件及审批决定在同一事务内完成。保留归档不可编辑、系统维护/项目完成来源不能改截止日期、自动策略必须有非空且全部完成的必需任务、重开按活动关系恢复 tracking/open，以及 read/triaged/snooze 独立语义。已读、解决或忽略不会完成 Task 或改写 Reminder/来源。
- workspace_get 的 inbox_item 详情新增 priority、due_at、read_at、snoozed_until、resolution_policy、source_entity_type、实时 task_progress 和 server_now；summary ≤2,000 字并标记截断。版本与进度在同一读取事务内组装，不外发 payload、来源 ID、活动正文或 Actor 信息。
- 确认卡显示本次字段差异、中文状态与边界提示，按服务端事实显示已执行，详情和下一轮无正文回执均指向 /inbox/:id；失败可回读与重试同一卡。决定后刷新 Inbox 列表/详情/事件/计数、搜索，并取消在途 Project 请求再刷新产出 follow-up；不依赖本地已缓存来源详情。
- 使用 schema 072 的通用审批表，无新迁移或业务 API 变更。任务关联见下节；Reminder 由 H2-C3 接续；强制解决由 H2-C4 接续，快照式全部已读由 H2-C5 接续。仍不宣称完整自主收件箱编排已接通。

#### 已有 Task 关系（H2-C2）

- 支持 `inbox.link_task / inbox.set_required / inbox.unlink_task`，目标仍为顶层 inbox_item_id/expected_version；changes 必须包含真实 task_id 和 expected_task_version。前两项显式传 is_required 布尔值，解除只接受 2–1,000 字 reason。禁止伪造 Actor、直接改 Task 状态、关系历史或额外字段。
- 前置读取 `workspace_inbox_tasks({inbox_item_id,state?,limit?,offset?})`，默认 active，history 仅返回已解除关系。分页 limit 默认 10、1–20，offset 0–1000，稳定排序并标记 has_more/next_offset/window_limited；同时返回 inbox_item_version/status/resolution_policy、实时进度及 server_now。Task 已删除的历史仅保留原 ID/标题快照，task_id/task_version/task_status 为 null、route 为空。字段白名单不含操作者、解除原因、Task 正文或来源 payload。
- 确认卡明确显示任务名称/ID/版本/状态、关联状态、必需标记、活动/必需/已完成/剩余数量，以及事项前后状态。自动策略要求非空必需集合；调整标记或解除可能触发自动解决、清除稍后，但不完成、删除或分派 Task。
- 人工 HTTP 关系 API 与审批复用 inbox_task_commands.go，保留 100 活动关系上限、重复/归档保护、软解除历史、幂等与原领域事件。预览/确认校验 Inbox 和 Task 版本；其他 Task 进度可能变化而不增加 Inbox version，因此确认事务重新计算预览，若与已展示快照不同返回 409 AI_ACTION_PREVIEW_CHANGED，必须重新读取并起草新建议，不能自动换参确认。
- 决定、关系写入、reconciliation 及事件同事务回滚，已确认重放不重复变更。结果使用 Inbox ID/版本、详情路由和既有无正文回执，卡片同时可打开事项与关联 Task；前端刷新 Inbox 关系/计数及 Project follow-up。没有新迁移、关系执行工具或自动权限继承；原子拆分/分派见下节，强制解决见 H2-C4。

#### 原子拆分与初始分派（H2-C2）

- `inbox.split` 以 inbox_item_id/expected_version 为目标，changes 包含 tasks（1–20 项）及可选 resolution_policy。草稿 key 唯一、parent_key 只引用同批更早草稿；支持标题、说明、类型、优先级、项目、标签、完成条件、计划/截止、预计分钟、none/manual 验收、显式 is_required 与真实 assignee_actor_id。默认 all_required_tasks_done，须有新建必需任务；manual 可无必需任务。
- 提议保存规范化后的整批载荷与 preview.tasks，不预分配业务 Task ID。确认卡逐项展示字段、父任务、负责人、验收人、项目及标签，同时展示 Inbox 策略/状态/数量/清除稍后效果；一次点击确认整批，不能只执行其中成功的部分。前端拒绝缺项或不匹配的预览。
- 人工 split API 与审批共用 inbox_split_commands.go；任务、父子关系、TaskTag、初始 Assignment、created 关系、Inbox/领域事件和审批决定同事务。manual reviewer 固定 active owner；person 仅本地责任，已有就绪 agent 可分派但不启动 Run。沿用 100 活动关系上限、归档/项目/标签/Actor 门控、父任务版本回读及原有 reconciliation。
- 确认时重建整批预览，对比负责人/验收人、项目、标签的名称与版本及关联进度。关联资料变化返回 AI_ACTION_PREVIEW_CHANGED，停用/删除等返回领域错误，不执行旧建议或自动换参。审批重放不重复建任务；人工 HTTP 幂等契约不变。解除关联形成位置空洞后，追加位置改为 MAX(active position)+1，避免数量与位置冲突。
- 决定后刷新 Inbox/Project follow-up 与 Task/Today 等聚合；结果路由仍为 /inbox/:id，从事项查看真实新任务。后续无正文回执只表明整批决定/Inbox 结果，不宣称任务已完成。不新增迁移，不支持任意负责人创建或隐式 Agent 执行。

#### 本地提醒（H2-C3）

- `work` 范围允许 `workspace_search/get` 的 `reminder` 类型；列表沿用分页并返回 UTC `server_now`。详情包含真实版本、优先级、触发时间、重复规则/间隔/IANA 时区/锚点、series_id、occurrence_number、fired_at 与生成的 inbox_item_id，summary 有界并标记截断；不发来源身份、操作者、事件键或取消原因。
- `work+actions` 允许 `reminder.create/update/cancel`。创建支持 title/summary/priority/trigger_at/recurrence_type/recurrence_interval/recurrence_timezone；修改仅这些字段的非空补丁，取消必填 1–1,000 字 reason。现有记录必须带顶层 reminder_id/expected_version，不能混用其他目标 ID，不开放来源、Actor、series/occurrence、锚点伪造或硬删除。
- 人工 API 和审批共用 reminder_commands.go；字段规范化、未来时间、版本、scheduled 状态、组合重复契约与锚点派生在预览/确认时重验。时间必须显式带偏移，重复规则使用 IANA 时区；系统提示要求模糊日期/时区先询问，不把 UTC server_now 当用户时区。none 固定 1/UTC；其余间隔 1–365，weekdays 仅周一至周五，monthly 沿用短月夹取和后续恢复锚点。
- 卡片展示计划和变更前后值；未修改的长 summary 不复制进改期/取消预览，明确编辑 summary 时仍完整展示前后值并受原审批大小限制。重复提醒取消当前 scheduled 即停止该系列后续生成，不删除已触发 occurrence 或 Inbox；编辑不改变历史。确认与领域事件、决定同事务，重放不重复操作，无变化编辑不递增版本。
- 目标已触发/被修改、等待期间新时间已过时拒绝旧提议；失败不静默改期。卡片和下一轮无正文回执使用 `/inbox?reminder=<UUID>` 打开已有提醒管理器，成功或失败后刷新 Reminder、Inbox 及搜索缓存。权限不会继承到下一条消息。没有新迁移，不开启原生通知、外部推送或关机唤醒；关闭应用后需启动补扫。
- 代码与确定性验收：[共享提醒命令](../../services/sidecar/internal/api/reminder_commands.go)、[提醒工具与预览](../../services/sidecar/internal/api/ai_reminder_actions.go)、[审批与 Harness 集成测试](../../services/sidecar/internal/api/ai_reminder_actions_test.go)。真实模型/桌面体验仍单独验收。

#### 收件箱强制解决（H2-C4）

- `work+actions` 可提议 `inbox.force_resolve`，只接受真实 inbox_item_id/expected_version 和 1–2,000 字 reason。仅适用于活动的 all_required_tasks_done 策略事项；手工策略应使用普通 resolve。模型不能传 confirm 或 confirm_force_resolve，也没有执行工具。
- 卡片展示原状态、解决策略、活动/必需/已完成/未完成数量，以及阻塞、待验收、已取消的必需任务数，目标状态明确为 resolved/forced。不外发 payload、未改动说明或历史操作正文；事项详情链接可查看完整关联任务。
- 用户必须另行勾选“按所示原因例外结清、保留未完成任务及验收状态”，再确认执行。只有人工 decision 接受 `confirm_force_resolve:true`；缺失或 false 返回 400 CONFIRMATION_REQUIRED，其他动作或 reject 携带此字段返回 422 AI_ACTION_DECISION_INVALID。已确认重放仍返回原决定，不要求重新勾选或再次执行。
- 确认事务重验版本、策略、活动状态和完整进度预览；即使 Inbox version 未变，Task 进度变化也返回 AI_ACTION_PREVIEW_CHANGED，旧提议不能自动换参重试。人工 API 与审批共用 inbox_force_resolve_commands.go，写入 owner/forced/原因与 force_resolved 事件；领域事实、事件和审批决定任一失败全部回滚。
- 强制解决只改变 Inbox，不完成、取消、解除或审核 Task，也不修改来源；刷新 Inbox/Project follow-up 与搜索，实际决定进入既有无正文回执。提示词禁止把 force_resolve、dismiss 或降低 required 当作普通 resolve 失败的自动绕过手段；例外仍须用户明确意图和额外人工勾选。无新迁移。
- 确定性证据：[共享命令](../../services/sidecar/internal/api/inbox_force_resolve_commands.go)、[审批与 Harness 集成测试](../../services/sidecar/internal/api/ai_inbox_force_resolve_test.go)、[前端确认卡](../../apps/web/src/components/AiWorkspaceActions.test.tsx)。真实模型效果另验收。

#### 收件箱快照式全部已读（H2-C5）

- `workspace_search({type:"inbox_item"})` 在每个有界结果页之外返回服务端 `snapshot_at`、不受筛选影响的 `snapshot_unread_total` 和 `read_all_max_items=1000`。模型要提出 `inbox.read_all`，必须把该 `snapshot_at` 原样放入唯一的 `changes.through_created_at`；不接受客户端时间、目标 Inbox ID、版本或其它 changes 字段。
- Sidecar 按原生全部已读谓词读取全局候选，不以当前工具页或筛选作为范围。空快照返回 `INBOX_READ_ALL_EMPTY`；候选超过 1000 条返回 `AI_INBOX_READ_ALL_TOO_LARGE`，引导用户使用原生 Inbox 页面。预览保存 cutoff、候选数量和稳定排序候选 `ID+version` 的 SHA-256 指纹，卡片只展示数量/时间与只改已读的边界，不把批量事项正文发回模型或写进回执。
- 只有人工确认才执行。决定事务重新计算完整预览；候选增减或版本变化返回 `AI_ACTION_PREVIEW_CHANGED`，不能批准新范围。随后调用与 `POST /api/v1/inbox-items/read-all` 共用的 `readAllInboxItemsInTransaction`，以同一事务写每条 `read_at`、`read` Event、审批决定与结果；快照后新到或被修改、分诊、重开的事项仍保守排除。
- 批量结果没有单一 Inbox 实体：`result_id` 是 Proposal 回执身份、`result_version=1`，`route=/inbox`。`inbox_read_all_result` 以及下轮审批回执单独携带真实 cutoff 与 `marked_count`；前端严格要求已确认卡才存在该结果，主/侧卡显示实际数量，并刷新 Inbox/搜索及相关工作台读模型。确认只改 read 状态，不解决、稍后、分诊、改策略、关联 Task、改 Reminder 或来源。
- 本切片复用 API v1 和 schema 73，没有新迁移、HTTP 路由、模型执行工具或权限继承。原生幂等接口继续用于人工页面；AI Proposal 自身承担耐久决定与重复确认回读。真实 Provider 对措辞与桌面交互仍需单列验收，不能用隔离工具调用代替。

#### 专注查询与会话操作（H3-A1/A2）

- `workspace_focus({view,id?,task_id?,status?,limit?,offset?})` 在 work 范围按 active/session/history 返回真实 Focus 快照。session 要求 canonical id；history 仅终态，可按 Task/状态筛选，limit 1–20、offset 0–1000，带 has_more/next_offset/window_limited。字段限于会话身份/版本/状态、绑定任务名称、计时/心跳/结束/入账事实与 UTC server_now，不读任务正文；查询不刷新心跳。
- `focus.pause/resume` 使用 focus_session_id/expected_version，changes 为空；没有自主执行工具。人工确认通过共享原生命令更新区间和会话，与审批/事件同事务；版本或关联资料变化拒绝旧卡。暂停结算到确认时刻，暂停期间不计时，继续不等于异常恢复。重复已确认决定不重新暂停/继续。
- 卡片展示前后状态、任务和计划秒数，明确不会结束会话、完成任务或重置轮次；成功/模糊失败均取消旧 Focus 请求并刷新共享快照，工作台右卡与专注页随之读取真实状态。无正文回执走既有链路；/focus 仅页面导航，不是某条 Session 详情链接。
- 返回 local_cycle_included=false；active null 不证明本地没有休息循环。recovery_pending 未结算区间不能算作已知有效时长，必须询问用户中断期间是否持续工作，不默认计入间隔。
- `focus.start` 的 changes 必须显式包含 task_id（UUID 或 null）和 planned_seconds（300–7200），绑定 Task 另需 expected_task_version；不接受顶层 Session ID/version。共享领域规则阻止覆盖任何未结束会话，确认重验 Task 版本/资料。人工卡需要另行勾选同意新循环替代本地休息/旧轮次，decision 传 `confirm_focus_start:true`；缺失/false 返回 `422 FOCUS_START_CONFIRMATION_REQUIRED`，模型参数不能传同意字段。
- `focus.stop/cancel` 使用真实 focus_session_id/expected_version 与空 changes。stop 在确认时封顶结算有效时间并累计 Task 余秒账本，不完成 Task；cancel 保留审计但不入账。手动结束不进入本地休息/下一轮。别处结束后旧 pending 卡必须冲突，历史 confirmed 重放只回原结果。
- `focus.recover` 只支持 recovery_pending，changes 仅 recovery_action：include_gap_resume、exclude_gap_resume 或 interrupt。预览展示最后心跳/此前已结算秒数，未知间隔计到实际确认时刻而非提议时刻。计入间隔还需人工 `confirm_focus_gap:true`，否则 `422 FOCUS_GAP_CONFIRMATION_REQUIRED`；排除按最后心跳保留有效工作，interrupt 不入账。两种同意字段只用于匹配的 confirm，reject 或其他动作携带返回 `422 AI_ACTION_DECISION_INVALID`。
- H3-A2 复用原生 focus_commands.go 的开始/结束/恢复事务，任何业务或审批事件失败整体回滚。前端回读活动事实后由 Session 身份感知 ticker 同步；失联回包不通过历史决定强制重置本地循环。恢复弹窗允许跳至智能体，`/ai` 不阻断聊天，跳转不恢复会话或自动授权。工具链、回执、确认卡和 Query/ticker 集成见 ai_focus_lifecycle_test.go 与 FocusTicker.integration.test.tsx。本地休息直接控制仍待，周期报告见下一节及 [专注模块](focus.md)。

#### 专注周期报告（H3-A3）

- work 范围的 `workspace_focus_report({view,date_from,date_to,timezone,project_id?,limit?,offset?})` 只读真实聚合。日期和用户确认的 IANA 时区必填，范围 1–93 个自然日；project_id 为可选真实 canonical UUID，缺省所有项目。summary 返回整个范围总量/连续天数，不接受分页；days/projects/tags/hours/heatmap 返回对应维度有界页及相同总量。
- 详细页 limit 1–20/默认 10，offset 0–1000；总条数、排序、has_more/next_offset/window_limited 明示。每次读取独立快照，跨页可能随业务更新变化；未读页不等于零。只统计 completed 有效区间，保留零值日/小时/热力格，项目/标签按查询时当前归属，多标签秒数及各桶 Session 数不得求和冒充总量，分钟按桶取整，连续天数仅限所查范围。
- 复用人工统计共享读模型，不刷新心跳，不写业务或审批；模型可引用总量与分布但没有 Task 正文/人员或区间账本权限。授权面板披露统计外发，Provider/维护/停止门禁及 24 KiB 结果预算仍适用。H4-C 的 route 已带 `/focus` 的 date_from/date_to/timezone/project_id?/report 深链；提示模型原样引用，点击后读取同条件当前报告并定位维度，不是冻结历史快照。证据：ai_focus_report.go、ai_focus_report_test.go、focus_history.go；真实模型质量与桌面使用仍单独验收。
- 主对话和侧聊通过 `AiRichText` 共用只读 Markdown/安全内部链接，只有各领域严格允许的参数可点击，返回会话由界面按消息 Session 附加，不能由模型指定。FocusPage 提供返回原对话入口，主聊天保留明确选择，不被另一条正在生成的会话或旧发送回调覆盖。非法链接不读取默认报告，切换日期保留链接时区/项目，清除后恢复本地范围；详细 URL、错误和测试契约见 [专注模块](focus.md#已实现h4-c-专注报告定位与返回对话)。H4-D/E/F/G 已把同一原则用于客户记录、自动化规则/Run、Task Submission 与 Project Note；其他业务的精确子记录定位仍在实施，不能据此宣称全部工作台往返已完成。

### 工作台只读工具

H4-D 已补齐客户活动/回访往返：查询结果与审批卡使用真实 `/clients/:id?activity=<id>` 或 `?followup=<id>`，工作台直接读取和核对具体记录，无须翻页找回。确认后的复合回访链接使用实际新计划 ID；消息/卡片的返回会话由界面提供，模型不能指定。定位、加载/错误/删除历史和原生手工命令规则见 [客户模块](clients.md#已实现h4-d-活动与回访精确定位)。没有新增写权限、模型调用或业务副本。

#### 客户活动与回访（H3-B1–B3）

- `workspace_client_records({type:activity|followup,view:list|detail,...})` 仅在 `clients` 范围注册，执行时再次校验。list 可选 client_id、limit（1–20，默认 10）、offset（0–1000）；followup 还支持 status、assigned_actor_id、due_state=overdue（只兼容 planned）。按活动 occurred_at 倒序/回访 scheduled_at 正序和 ID 稳定排序，使用纳秒 UTC 键与原生逾期谓词；返回 total_items、has_more/next_offset/window_limited、server_now，每次请求是新快照。
- detail 必须给真实 canonical UUID，不能夹带列表筛选。活动默认 field=body；回访默认 notes，可换 result/next_step/skip_reason/cancel_reason。每次仅该文本字段按 content_offset（0–1,000,000）/content_limit（1–4000，默认 4000）分段读取，返回长度/下一偏移；24 KiB JSON 上限仍适用，转义过多时缩小 content_limit。列表标签最多 160 字并显式 label_truncated；软删除活动不可读。角色、状态、排期与版本来自当前事实；system_reference 不等于实际客户沟通。
- 另需 `work+clients+actions` 才可提议 client_followup.create/update/cancel/skip/complete/reschedule。创建显式 client_id、assigned_actor_id、scheduled_at、timezone、channel、purpose，notes 可空、priority 默认 normal（low/normal/high）；编辑仅计划字段，不能换客户；取消/跳过仅 reason。现有目标以 client_followup_id/expected_version 绑定，创建不能传目标/版本；拒绝混合目标、越权字段、代理负责人和模型同意标记。时间/时区不清楚先问，过去计划允许但会逾期。
- 原生与 AI 共享 ClientFollowup 事务命令，确认重验 planned、版本、客户/负责人规则及持久预览身份，旧建议不能覆盖资料变化。计划、领域事件、旧到期 Inbox 归档与审批同事务回滚，重放不重复执行。前端严格验证卡片字段/载荷对应关系，展示本地计划边界，刷新 Client/Followup/Inbox/Actor/搜索缓存；路由由真实 Client ID 推导，不信任任意服务端链接。后续回执只含动作/状态/ID/结果版本，不继承查询授权。
- complete 仅记录用户已实际完成的回访，必须显式 result/completed_at，next_step 可空，next_followup 可省略；若提供，必须是完整计划且不得带 client_id。确认另需人工 `confirm_followup_completed=true`，否则返回 422 CLIENT_FOLLOWUP_COMPLETION_CONFIRMATION_REQUIRED。reschedule 必须提供原因与完整新计划，旧计划取消、新计划（带 rescheduled_from_id）创建、Inbox 归档、两个领域事件及审批同事务；完成+续排同样原子。停用客户允许完成旧计划但不允许续排或重排。
- `preview.next_followup` 单独展示新计划和真实负责人身份；客户端校验前后值、完整计划与原始载荷对应关系，拒绝隐藏或不一致的新计划。确认重验下一负责人资料。复合回执 target_id 是旧计划，result_id/result_version 是新计划；不带正文、不继承权限。H4-D 的 route 已定位具体活动/回访，确认后优先使用 result_id（重排/续排为新计划）；ClientActivity 写入见下方 H3-B3。不联系客户、不生成虚假沟通结果或活动。源码与测试见 [回访模块](client-followups.md)。

#### 自动化规则与运行（H3-C1/C2）

- 单次 work 下 `workspace_automations` 查询内置规则及配置/版本/权限、Run 白名单元数据，支持 rules/rule/runs/run；运行记录有界分页，不返回配置/动作快照、原始来源事件、去重键或结果正文，不写调度状态。
- work+actions 的 automation.enable/disable/update 绑定真实 automation_rule_id/expected_version；启停空 changes，事件只改 priority，定时必须显式 local_time 和 IANA timezone。共享原生规则事务，无自由规则、Shell 或模型执行工具。
- 确认卡完整显示触发、动作、权限、启用状态与前后配置；启用和修改已启用规则需独立 confirm_automation_effects 人工勾选，持续效果不与单次模型权限混同。确认重验稳定预览/版本并原子写规则/审计/决定；调度器推进的 next_run_at 不冻结进预览，相同配置不重排计划，保存不立即运行。
- 停用不撤销既已捕获事件的交付和重试，不删除历史或业务对象。五个预设均可人工启用，含 H3-C3 Agent 失败诊断和发票逾期本地任务；启用诊断不启动或重跑 Agent，不外发客户消息。
- 卡片打开现有自动化设置弹层，工具 ui_entry=automation_settings 不冒充页面路由；决定后取消旧查询并刷新规则/Run/Inbox/Reminder，后续回执不含配置、不继承权限。H3-C2 另提供原快照重试，详见 [自动化模块](automation.md) 和 ADR-029。

- `automation.retry` 同需 work+actions，使用 automation_run_id 和原 Run.rule_version 作为 expected_version，空 changes；不传当前规则配置或新的动作。完整 `preview.automation_retry` 只向人工展示原配置/动作，须另行 `confirm_automation_retry:true`，原生/后台/AI 共用事务，确认重验资格、当前规则/权限与唯一后续 attempt，审批失败回滚业务结果。
- 重试 confirmed 只表示已记录新 Run；`result_id` 是新 Run，`result_version` 是捕获规则版本而非 Run 版本。`automation_run_result` 为实际 succeeded/failed、attempt、可重试与当时 retry_at、安全错误码及白名单业务结果 ID/类型；卡片与无工具授权的下一轮回执均据此区别成功/失败，不发送业务快照，也不把后续退避当成已发生。重试决定还取消/刷新 Task/Project/Today/搜索等读模型，响应丢失回读同一卡。

#### 续办队列交给智能体（H4-J）

- 侧栏“续办队列”里真正存在智能体能力或安全排查价值的八类事项提供“交给智能体”：**知识库索引失败**（推荐 `knowledge_actions`，提示词含来源名称、尝试次数与错误码）、**自动化运行失败**（推荐 `work+actions`，提示词含规则名、尝试次数与错误码）、**未成功的 Agent 执行 / 结果待登记**（推荐 `work+outputs+actions+agent_execution`，提示词含任务标题、尝试次数与交付状态）、**当前待验收提交**（推荐 `work+outputs+actions`，提示词含 Task/Submission 身份、批次号与来源）、**系统维护事项**（推荐 `work+actions`，提示词只带 Inbox ID、固定维护分类和错误码，要求先读原 Inbox 事实）、**Provider 配置待处理**（无推荐 scope，只带配置状态和错误码，要求给出人工排查步骤）、**数据恢复诊断**（无推荐 scope，只带恢复诊断状态与计数，要求给出人工核对顺序）与**对话生成失败**（无推荐 scope，只带会话标题、generation ID 和错误码，要求给出继续对话/检查设置的人工排查建议）。待确认建议不提供该入口，因为不应代替原确认卡。
- 交接只暂存代码生成的标签、严格本地应用 route、原生处理入口与该条事项的元数据（名称/次数/错误码/诊断计数/generation ID/focus session ID）+ 固定提示词；不复制业务正文、授权、Provider 密钥、本机路径、备份内容、失败消息正文或历史 grant，不自动发送。route 必须是单斜杠开头且留在本应用 origin 内，协议相对地址、外部 URL、反斜杠以及真实或百分号编码的 ASCII 控制字符都会被拒绝。进入 `/ai` 后，主对话和右侧 `WorkspaceChat` 都能接收同一个待办卡：有推荐 scope 的事项由用户点击“带入问题并选择权限”才把提示词追加到当前输入框原草稿，并同时清空旧授权与已选上下文、按推荐范围重新打开权限面板；无推荐 scope 的 Provider/数据恢复/对话失败排查只显示“带入问题”，追加提示后不打开工作台权限。侧聊准备交接同样不读取右侧文件、浏览器、终端或 Git 面板内容，不切换主会话，也不自动发送。同一时刻只保留一个待办意图（记录型与异常型互相取代），用户可随时不带入。
- 提示词一律要求模型先读取真实事实（`knowledge_library` / `workspace_automations` / `workspace_agent_runs` 与 `workspace_outputs` / `workspace_task_submissions` / `workspace_get(type=inbox_item)` / `workspace_focus`）再提出待确认建议，并明确“不要把建议说成已执行”；待登记结果只能恢复、不能取消丢弃。待验收交接必须说明文件正文、外部页面或未读取证据仍需用户人工核对，只能起草待确认的通过/返工建议。系统维护交接只能帮助用户解释影响并在用户人工处理后起草 `inbox.resolve` / `inbox.dismiss` 建议，不能自动重试备份、重启服务、执行恢复、清理文件、修改阈值或猜测底层路径/日志。对话生成失败交接只能给继续对话、改写问题、检查设置或稍后重试的建议，不能自动重试、切换 Provider、恢复旧授权或读取原消息正文。专注恢复交接只能解释 `focus.recover include_gap_resume / exclude_gap_resume / interrupt` 的影响并起草待确认建议，不能猜测未知中断间隔、自动恢复、停止/取消专注或改写 Task 工时。整条链路没有新工具、没有新 scope、没有新迁移、也不改变续办队列原有的原生跳转与只读边界；真正的重试/恢复/验收/解决仍需逐项人工确认。
- 确定性证据：`lib/aiIssueHandoff.ts` 生成内容，`AiSessionRail.test.tsx` 覆盖知识库失败、失败 Run、待验收提交和系统维护事项的推荐范围、原生入口与提示词要点，`AiWorkbenchHandoff.test.tsx` 覆盖异常卡的准备、严格本地 route 拒绝、单一待办意图，`AiAssistantPage.test.tsx` 覆盖主对话“带入问题并选择权限”后的草稿、勾选范围与不自动发送，`WorkspaceChat.test.tsx` 覆盖侧聊记录/异常交接、旧 grant 清除、推荐 scope 打开和空 scope 不打开权限。真实模型措辞与桌面体验仍单列验收。

#### 专注恢复交给智能体（H4-AB）

- 业务页的恢复弹窗现在不再只是跳到 `/ai`：点击“在智能体中处理”会暂存“专注恢复”交接，route 为 `/focus`，推荐 `work+actions`，只携带 recovery_pending 会话的 canonical session ID、version、可选 canonical task ID、页面当前显示的已确认/不确定时长和固定提示词。进入 `/ai` 后仍需用户点击“带入问题并选择权限”、人工发送，并在确认卡中逐项同意。
- 提示词要求模型先用 `workspace_focus(view=session,id=...)` 读取真实状态、版本、最后心跳、已结算秒数和可用动作，再解释 `include_gap_resume`、`exclude_gap_resume`、`interrupt` 三种恢复选择。未知中断间隔不能由模型猜测；如果要计入间隔，必须让用户在确认卡额外勾选同意。
- 交接本身不调用 recover、不重置 WebView 本地番茄循环、不修改 Task 状态或工时，也不把页面展示的任务正文、历史授权、Provider 或会话内容带入模型。`/ai` 仍不会挂出阻断式恢复弹窗；待恢复 Session 保持在后端事实中，直到用户通过原生弹窗或受确认的 `focus.recover` 明确处理。
- 确定性证据：`FocusRecoveryModal.test.tsx` 覆盖点击后只暂存 `pendingIssue`、不调用 recover、不重置本地循环；`aiIssueHandoff.test.ts` 覆盖 `focusRecoveryHandoff` 的 route、scope、prompt 边界和非 canonical session 拒绝。

#### 财务与发票详情交给智能体（H4-K）

- 收支详情 `/income/:entryId` 与发票详情 `/invoices/:invoiceId` 现在也提供“交给智能体”。它复用 H4-J 的单槽位交接机制，但使用财务专用提示词：收支记录推荐 `finance+finance_actions`，发票推荐 `finance+invoice_actions`；进入 `/ai` 后仍需用户点击“带入问题并选择权限”、人工发送，并在确认卡中逐项同意。
- 交接只暂存 canonical UUID、本地 route 和固定提示词，不复制详情页正文、备注、客户名称、PDF 元数据、授权、Provider 或会话身份。模型必须先用 `workspace_finance(view=entry|invoice,id=...)` 读取白名单元数据，再决定是否起草 `financial_entry.*` 或 `invoice.*` 建议；普通 `work/actions` 不会被误用于财务或发票写入。
- 提示词明确不能假设外部付款、银行到账、发票已发送/已查看、PDF 已下载或备注内容；发票回款生成的账本记录与已作废记录仍按领域规则只读。真正修改、作废、状态变更、草稿删除、PDF 生成或回款登记仍需独立财务/发票确认卡和额外人工勾选。
- 确定性证据：`FinancialEntryDetailPage.test.tsx` 和 `InvoiceDetailPage.test.tsx` 覆盖详情页交接的精确 route、推荐 scope 与提示词要点；`AiWorkbenchHandoff.tsx` 的通用事项卡仍覆盖不自动发送、不继承权限、不自动修改记录。

#### 项目笔记交给智能体（H4-N）

- 项目笔记列表和精确定位卡现在也提供“交给智能体”。入口只暂存 `/projects/:projectId?note=:noteId`、笔记标题和固定提示词，推荐 `work+actions`；进入 `/ai` 后仍需用户点击“带入问题并选择权限”、人工发送，并在确认卡中逐项同意。
- 交接不复制笔记正文、删除原因、项目资料、授权、Provider 或会话身份；模型必须先用 `workspace_project_notes(project_id, view=detail, note_id)` 读取最新事实并核对项目归属。若笔记已删除或项目已归档，只说明限制；需要新增、修改或删除项目笔记时，必须基于当前版本提出待确认的 `project_note.*` 建议。
- 删除项目笔记仍要求用户提供真实原因，并在确认卡里再次人工同意；工作台点击交接不会创建、修改、删除笔记，也不会把页面上用户可见正文当作已授权给模型。
- 确定性证据：`ProjectNotesSection.test.tsx` 覆盖精确笔记交接的 route、推荐 scope、提示词身份和“不复制正文”；`aiIssueHandoff.test.ts` 覆盖 helper 的 canonical UUID 校验与提示词边界。

#### 任务提交批次交给智能体（H4-O）

- 精确提交批次页 `/tasks/:taskId/submissions/:submissionId` 现在提供“交给智能体”。入口只暂存 Task+Submission 路由、任务标题、批次号和固定提示词，推荐 `work+outputs+actions`；进入 `/ai` 后仍需用户点击“带入问题并选择权限”、人工发送，并在确认卡中逐项同意。
- 交接不复制批次摘要、验收意见、Artifact 元数据、文件正文、下载结果、Provider、会话或授权；模型必须先用 `workspace_task_submissions` 读取当前事实并核对该批次仍属于此 Task、是否仍为当前指针。需要时再用 `workspace_get` 分段读取已授权的文本产出；文件正文和外部证据仍需用户人工核对。
- 只有当它仍是当前待验收批次且证据足够时，智能体才可起草待确认的通过/返工建议；查看或交接批次不会提交、验收、下载文件或把历史批次替换为当前批次。
- 确定性证据：`TaskSubmissionPage.test.tsx` 覆盖精确批次交接的 route、推荐 scope、提示词身份和“不复制摘要”；`aiIssueHandoff.test.ts` 覆盖 helper 的 canonical UUID 校验与提示词边界。

#### 任务/项目产出单项交给智能体（H4-AC）

- Task 详情的当前提交/历史提交/全部产出卡片、Project 详情里的“项目产出”聚合列表，以及右侧工作区“任务产出”文件详情，现在都对每个未删除 Task Artifact 提供“交给智能体”。入口只暂存 Artifact、所属 Task、Submission 路由、可用批次号、产出名称、类型和固定提示词，推荐 `work+outputs`；进入 `/ai` 后仍需用户点击“带入问题并选择权限”、人工发送，但不会打开操作建议 scope。
- 提示词要求模型先用 `workspace_get(type=artifact,id=...)` 分段读取真实产出事实，并核对它仍属于页面上的 Task 和 Submission；需要批次上下文时再用 `workspace_task_submissions` 读取提交摘要、验收状态和产出清单。交接不复制项目页聚合摘要、文件正文、下载结果、附件、Provider、会话或授权。
- 该入口只用于复查或整理证据，不起草验收/返工、删除或修改建议；未获独立文件授权时文件产出只能当作元数据，模型不能声称已下载或检查文件正文。H5-F1 引导用户另行手选 `output_files`，再按最新版本/哈希调用 `workspace_read_artifact_file`；推荐仍不含新范围，不复制文件或把未读分页当已读完。若用户需要验收当前批次，仍使用精确提交批次页的 H4-O 入口并授予 `actions`。
- 确定性证据：`TaskOutputsSection.test.tsx`、`ProjectArtifactsSection.test.tsx` 与 `WorkspaceResources.test.tsx` 覆盖从源 Task 详情、项目产出行和右侧受控文件详情点击交接后只暂存 `pendingIssue`、推荐 `work+outputs` 且不触发文件打开或登记恢复；`aiIssueHandoff.test.ts` 覆盖 `taskArtifactHandoff` 的 route、scope、prompt 边界和 canonical UUID 拒绝。

#### 任务详情 Agent 执行记录交给智能体（H4-AD）

- Task 详情的“Agent 执行”列表、“执行过程”抽屉，以及右侧工作区“子智能体”详情现在对失败、中断、取消、产出登记待恢复，以及**已保留但未提交**的 Run 提供“交给智能体”。入口复用 `agentRunIssueHandoff`，只暂存规范 Task/Run 身份、任务标题、attempt、状态/交付状态和固定提示词，进入 `/ai` 后仍需用户手动带入问题、重新授权并发送。
- 推荐范围为 `work+outputs+actions+agent_execution`。模型必须先用 `workspace_agent_runs` 与 `workspace_outputs` 读取精确 Run/交付事实；待登记结果只能恢复登记，重试或停止只能起草待确认建议。已保留结果不能通过产出登记恢复接口再次提交，也不等于 Task 完成或验收；仅可据当前真实条件解释原因，必要时提出一条新的待确认重试建议。入口不会自动打开执行抽屉、取消、重试、下载或读取 Run 正文，也不会把建议说成已经执行。
- 已成功且已提交的 Run 不显示该入口；任务详情中的原生“查看过程、取消、重试、查看产出、下载”、抽屉中的“重试登记产出/下载文本”以及右侧资源面板中的“打开任务/重试/取消/重试登记产出”行为保持独立。规范 UUID 校验失败时不生成工作台深链，避免把伪造身份带入智能体。
- 确定性证据：`TaskAgentRunsSection.test.tsx` 覆盖失败 Run 点击交接后只暂存 `pendingIssue` 且不触发取消/重试/执行抽屉；`AgentRunDrawer.test.tsx` 覆盖从精确抽屉交接失败或 retained Run 后不打开任务、不变更所选 Run、也不触发登记恢复；`WorkspaceResources.test.tsx` 覆盖从右侧子智能体详情交接待登记 Run 后不触发恢复登记或取消；`aiIssueHandoff.test.ts` 覆盖失败与 retained Run 的 scope、route、工具提示、恢复边界与非法身份拒绝。

#### 自动化规则与运行交给智能体（H4-P）

- 自动化规则详情 `/settings/automation?rule=<id>` 和运行详情 `/settings/automation?run=<id>` 现在提供“交给智能体”。规则入口推荐 `work+actions`，提示模型先用 `workspace_automations(view=rule,id=...)` 读取真实规则、版本、可用性和权限，再决定是否起草 `automation.enable/disable/update`；运行入口同样推荐 `work+actions`，提示先读 `workspace_automations(view=run,id=...)`，只有失败且仍可重试时才起草 `automation.retry`。
- 交接只暂存严格本地 route、规则名称、状态、attempt、错误码和固定提示词；不复制配置快照、动作快照、运行结果摘要、业务对象正文、Provider、会话或授权。进入 `/ai` 后仍需用户点击“带入问题并选择权限”、人工发送，并在确认卡逐项核对。
- 提示词明确运行重试必须沿用原始快照和捕获 `rule_version`，不能修改当前规则、创建重复 attempt、执行自由规则/脚本/SQL/HTTP/外部发送，也不能把 `confirmed`、`recorded` 或建议说成业务动作已成功。原生保存、启停、重试按钮保持原人工事务路径，交接本身不写入任何自动化状态。
- 确定性证据：`AutomationLocationPage.test.tsx` 覆盖规则和 Run 详情点击交接后只暂存 `pendingIssue` 并导航到 `/ai`，且不触发 PATCH/enable/retry；`aiIssueHandoff.test.ts` 覆盖自动化 helper 的 route、scope、`workspace_automations` 读取要求、snapshot 不复制边界与 canonical UUID 校验。

#### 任务保存视图交给智能体（H4-Q）

- 任务页保存视图控件在已选中具体视图时提供“交给智能体”。入口推荐 `work+actions`，只暂存 `/tasks` route、canonical 视图 ID、当前显示名称和固定提示词；进入 `/ai` 后仍需用户点击“带入问题并选择权限”、人工发送，并在任何确认卡中逐项核对。
- 提示词要求模型先用 `workspace_task_views(view=view,id=...)` 读取真实名称、筛选定义、版本和更新时间，再解释这个保存视图代表的条件，或基于当前版本起草 `task_view.create/update/delete` 建议。交接不复制筛选定义快照、任务列表、任务正文、任务 ID、会话或授权，也不自动应用筛选、不读取筛选结果任务、不修改任何 Task。
- H4-Q 交接的 route 仍固定为 `/tasks`，不自动应用筛选；H5-E36 另提供经 `workspace_ui+work` 与用户确认的精确视图定位。保存视图只是本地筛选预设，删除仍需要 `confirm_task_view_delete` 人工确认，智能体不得把“已提出建议”表述为“已应用筛选”或“已修改任务”。
- 确定性证据：`TaskSavedViewsControl.test.tsx` 覆盖选中视图后点击交接只暂存 `pendingIssue` 并导航 `/ai`，不触发 create/update/delete 或额外 `onApply`；`aiIssueHandoff.test.ts` 覆盖 `taskSavedViewHandoff` 的 route、scope、prompt 边界和 canonical UUID 拒绝。

#### 路线图里程碑交给智能体（H4-R）

- 里程碑详情 `/roadmap?milestone=<id>` 现在使用专用“交给智能体”。入口推荐 `work+actions`，只暂存精确 route、canonical milestone ID、当前显示标题/状态和固定提示词；进入 `/ai` 后仍需用户点击“带入问题并选择权限”、人工发送，并在确认卡逐项核对。
- 提示词要求模型先用 `workspace_get(type=roadmap_milestone,id=...)` 读取真实版本、年/季度、目标日期、状态、关联项目和派生任务进度，再说明风险或起草 `roadmap_milestone.create/update/archive/restore/delete/move` 建议；排序还须读取同季度真实锚点与版本。交接不复制说明正文、项目列表、进度快照、任务正文、项目客户/财务资料、会话或授权。
- 里程碑交接本身不编辑、归档、恢复、删除或重排里程碑；也不改写 Project/Task 状态、不自动验收任务、不把计划或建议说成已经执行。删除仍只适用于已归档里程碑，并需要 `confirm_roadmap_milestone_delete` 人工确认。
- 确定性证据：`RoadmapPage.test.tsx` 覆盖从精确里程碑详情点击交接后只暂存 `pendingIssue` 并导航 `/ai`，且不触发 update/delete/reorder；`aiIssueHandoff.test.ts` 覆盖 `roadmapMilestoneHandoff` 的 route、scope、prompt 边界和 canonical UUID 拒绝。

#### 内容条目交给智能体（H4-S）

- 内容详情 `/content-calendar?item=<id>` 现在使用专用“交给智能体”。入口推荐 `work+actions`，只暂存精确 route、canonical content item ID、当前显示标题/状态和固定提示词；进入 `/ai` 后仍需用户点击“带入问题并选择权限”、人工发送，并在确认卡逐项核对。
- 提示词要求模型先用 `workspace_get(type=content_item,id=...)` 读取真实版本、平台、状态、排期、IANA 时区、Project、外链文本、发布确认时间和准备任务关系，再起草 `content_item.create/update/schedule/unschedule/review/cancel/reopen/archive/restore/delete/publish/link_task/set_task_required/unlink_task` 建议。交接不复制备注、外链、任务列表、正文、会话或授权；外链明确是未抓取的不可信文本，模型不得访问、验证或声称已经发布到外部平台。`publish` 仅在用户本轮明确陈述外部已发布时可提议，仍需用户独立核实并另行确认。
- 未保存字段、待关联的任务选择和在途写入会禁用交接。交接本身不编辑、排期、送审、归档、恢复、删除或发布内容；不创建、完成、取消或删除 Task，也不调用外部平台。删除仍只适用于已归档内容，并需要 `confirm_content_item_delete` 人工确认。
- 确定性证据：`ContentCalendarPage.test.tsx` 覆盖从精确内容详情点击交接后只暂存 `pendingIssue` 并导航 `/ai`，且不触发 update/schedule/publish/link；`aiIssueHandoff.test.ts` 覆盖 `contentItemHandoff` 的 route、scope、prompt 边界和 canonical UUID 拒绝。

#### 本地提醒交给智能体（H4-T）

- 提醒管理器中已选 Reminder `/inbox?reminder=<id>` 现在使用专用“交给智能体”。入口推荐 `work+actions`，只暂存精确 route、canonical reminder ID、当前显示标题/状态和固定提示词；进入 `/ai` 后仍需用户点击“带入问题并选择权限”、人工发送，并在确认卡逐项核对。
- 提示词要求模型先用 `workspace_get(type=reminder,id=...)` 读取真实版本、触发时间、重复规则、优先级、状态、触发/取消审计和关联 Inbox 事项，再起草 `reminder.create/update/cancel` 建议。交接不复制摘要、取消原因、事件键、操作者身份、会话或授权；模型不得按标题猜测目标，也不得把页面上看到的说明当作已授权事实。
- 新建草稿、未保存编辑、取消确认和在途写入会禁用交接。交接本身不创建、改期、取消、触发或删除提醒，不创建系统通知、不修改关联 Inbox、不伪造触发事实；已触发或已取消的提醒不能继续修改或取消。
- 确定性证据：`ReminderManagerModal.test.tsx` 覆盖已保存提醒点击交接后只暂存 `pendingIssue`，未保存标题会禁用入口，且不触发 update；`aiIssueHandoff.test.ts` 覆盖 `reminderHandoff` 的 route、scope、prompt 边界和 canonical UUID 拒绝。

#### 收件箱事项交给智能体（H4-U）

- Inbox 详情 `/inbox/<id>` 现在使用专用“交给智能体”。入口推荐 `work+actions`，只暂存精确 route、canonical inbox item ID、当前显示标题/状态和固定提示词；进入 `/ai` 后仍需用户点击“带入问题并选择权限”、人工发送，并在确认卡逐项核对。
- 提示词要求模型先用 `workspace_get(type=inbox_item,id=...)` 读取真实版本、优先级、截止时间、已读/稍后状态、解决策略、来源类型和实时任务进度，再起草 `inbox.create/update/read/snooze/unsnooze/resolve/dismiss/reopen/link_task/set_required/unlink_task/split/force_resolve` 建议。交接不复制摘要、来源 payload、活动正文、任务列表、会话或授权；模型不得按标题猜测目标，也不得把页面可见正文当作已授权事实。
- 编辑草稿、解决/忽略/稍后命令草稿、任务关系在途写入和其它在途操作会禁用交接。交接本身不编辑、已读、稍后、解决、忽略、重新打开或强制解决 Inbox，也不创建/修改 Task；拆分任务不会自动执行 Agent，强制解决不是普通解决的自动降级。
- 确定性证据：`InboxItemDetailModal.test.tsx` 覆盖编辑/命令草稿禁用交接，保存状态点击后只暂存 `pendingIssue` 并关闭详情，且不触发 update/command；`aiIssueHandoff.test.ts` 覆盖 `inboxItemHandoff` 的 route、scope、prompt 边界和 canonical UUID 拒绝。

#### 任务详情交给智能体（H4-V）

- Task 详情 `/tasks/<id>` 现在使用专用“交给智能体”。入口推荐 `work+outputs+actions`，只暂存精确 route、canonical task ID、当前显示标题/状态和固定提示词；进入 `/ai` 后仍需用户点击“带入问题并选择权限”、人工发送，并在确认卡逐项核对。
- 提示词要求模型先用 `workspace_get(type=task,id=...)` 读取真实版本、项目/父子关系、标签、计划/截止时间、完成条件和当前状态；需要责任事实时再用 `workspace_task_assignments`，需要提交或验收事实时在 outputs 授权下用 `workspace_task_submissions`。交接不复制描述、产出摘要、文件正文、任务列表、会话或授权；模型不得按标题猜测目标，也不得把页面可见内容当作已授权事实。
- 未保存草稿、版本冲突、责任/生命周期/产出写入忙碌、删除确认等状态会禁用交接。交接本身不保存编辑、不改生命周期、不分派、不提交产出、不验收、不删除、不启动 Agent，也不直接改 Project/Inbox/Content 状态；文件正文和外部证据仍需用户人工核对。
- 确定性证据：`TaskDetailModal.test.tsx` 覆盖未保存标题禁用交接，保存状态点击后只暂存 `pendingIssue` 并关闭详情，且不触发 update/lifecycle；`aiIssueHandoff.test.ts` 覆盖 `taskHandoff` 的 route、scope、prompt 边界和 canonical UUID 拒绝。

#### 任务责任分派交给智能体（H4-AF）

- Task 详情里的“责任分派”区现在提供专用“交给智能体”。入口推荐 `work+actions`，只暂存 `/tasks/<id>`、canonical Task ID、当前显示标题/状态和固定提示词；进入 `/ai` 后仍需用户点击“带入问题并选择权限”、人工发送，并在确认卡逐项核对。
- 提示词要求模型先用 `workspace_task_assignments(task_id=...)` 读取真实任务版本、当前负责人/审核人和历史；需要候选人时再用 `workspace_task_options` 按 actor/role 读取可用所有者或本地人员。交接不复制人员名称、历史原因、任务描述、产出摘要、会话或授权；模型不得按标题猜测目标，也不得把页面可见人员信息当作已授权事实。
- 责任编辑器打开、分派/改派/结束写入中或父级禁用时会禁用交接。交接本身不首次分派、不改派、不结束记录、不改任务状态、不验收产出、不联系人员、不启动 Agent；如需变更，只能基于当前版本起草待确认 `task.assign` / `task.reassign` / `task.unassign` 建议。本地人员分派仍只是本机责任记录，Agent 分派也不会启动执行。
- 确定性证据：`TaskAssignmentsSection.test.tsx` 覆盖点击后只暂存 `pendingIssue`，不触发 create/reassign/end；`aiIssueHandoff.test.ts` 覆盖 `taskAssignmentHandoff` 的 route、scope、prompt 边界和 canonical UUID 拒绝。API v1 / schema 076 不变。

#### 任务标签交给智能体（H4-AG）

- Task 页“管理标签”弹窗里的已保存标签行现在提供专用“交给智能体”。入口推荐 `work+actions`，只暂存 `/tasks` route、canonical Tag ID、当前显示名称/颜色和固定提示词；点击后关闭标签弹窗并进入 `/ai`，仍需用户带入问题、授权并人工发送。
- 提示词要求先加载 tasks_projects 指南，再用 `workspace_task_options(type=tag,id=...)` 读取唯一当前标签的 version、名称和颜色；ID 已不存在时停止，不按名称替换。交接不复制任务列表、关联任务数量、筛选结果、会话或授权；模型不得把页面可见颜色当成已重验事实。
- 标签行有未保存名称/颜色编辑、删除确认或写入中时会禁用交接。交接本身不创建、改名、改色或删除标签；如需变更，只能基于真实版本起草待确认 `tag.create` / `tag.update` / `tag.delete` 建议。删除仍必须经用户在确认卡里额外勾选，只删除标签并解除任务关联，不删除任务或改变任务状态。
- 确定性证据：`TagManagerModal.test.tsx` 覆盖点击后只暂存 `pendingIssue` 并关闭弹窗，不触发 update/delete；未保存编辑会禁用交接。`aiIssueHandoff.test.ts` 覆盖 `tagHandoff` 的 route、scope、prompt 边界和 canonical UUID 拒绝。API v1 / schema 076 不变。

#### 本地人员设置交给智能体（H4-AH）

- 设置页“人员与责任”的已保存本地人员行现在提供专用“交给智能体”。入口推荐 `work+actions`，只暂存 `/ai?settings=actors`、canonical person ID、当前显示名称/状态和固定提示词；进入 `/ai` 后打开人员设置作为原生核对入口，仍需用户带入问题、授权并人工发送。
- 提示词要求模型先用 `workspace_get(type=person,id=...)` 读取真实人员身份、状态和版本；也可用 `workspace_search(type=person,query=...)` 辅助核对，但必须精确匹配 person ID。交接不复制备注、metadata、客户关联、任务责任、联系方式、会话或授权，也不把页面可见备注当作已授权事实。
- 新建表单、编辑器打开或写入中时会禁用交接。交接本身不创建、修改、停用、改派任务、解除客户联系人或取消回访；如需变更，只能基于真实版本起草待确认 `person.create` / `person.update` 建议。备注只能来自用户本轮明确输入；本地人员不会收到消息、不会获得账号或访问权限，不能编辑 owner/system/agent，也不能删除人员或联系任何人。
- 确定性证据：`ActorSettings.test.tsx` 覆盖点击后只暂存 `pendingIssue` 且不触发 create/update，编辑器打开时禁用交接。`aiIssueHandoff.test.ts` 覆盖 `personHandoff` 的 route、scope、prompt 边界和 canonical UUID 拒绝。`AiAssistantPage.test.tsx` 覆盖 `/ai?settings=actors` 深链打开人员设置。API v1 / schema 076 不变。

#### 本地 Agent 设置交给智能体（H4-AI）

- 设置页“本地 Agent”的已登记 Adapter 卡片现在提供专用“交给智能体”。入口不推荐工作台 scope，只暂存 `/ai?settings=agent`、canonical Adapter ID、当前显示名称、启停状态、健康状态和是否 execution-ready 的固定提示词；进入 `/ai` 后打开本地 Agent 设置作为原生核对入口，用户仍需手动带入问题并发送。
- 当前 Harness 没有面向模型的 Adapter 配置读取工具，因此该交接是无授权排查入口：提示词明确要求智能体只基于交接携带的页面状态整理人工排查顺序，不能声称已读取最新 Adapter 配置或执行设置操作。若需要登记、检查运行条件、启用或停用，必须引导用户回到“设置 → 本地 Agent”人工点击。
- 交接不复制 Adapter manifest、内部执行器引用、端点、密钥、任务快照、文件、右侧工作区内容、会话或授权；也不允许模型要求查看本机路径、Shell、Git、终端或浏览器。交接本身不登记、不检查、不启停、不分派任务、不启动 Agent Run、不读取任务文件，也不把排查建议说成已经执行。
- 适配器登记、检查或启停写入中时会禁用交接，避免用户把正在变化的设置状态交给 AI。确定性证据：`AgentAdapterSettings.test.tsx` 覆盖点击后只暂存 `pendingIssue` 且不触发 Adapter POST；`aiIssueHandoff.test.ts` 覆盖 `agentAdapterSettingsHandoff` 的 route、无 scope、prompt 边界和 canonical UUID 拒绝；`AiAssistantPage.test.tsx` 覆盖 `/ai?settings=agent` 深链打开本地 Agent 设置。API v1 / schema 076 不变。

#### AI 供应商设置交给智能体（H4-AJ）

- 设置页“AI 助手”的已保存供应商卡片现在提供专用“交给智能体”。入口不推荐工作台 scope，只暂存 `/ai?settings=ai`、canonical Provider ID、供应商名称、模型名、本地/远程类型、当前页面状态和健康枚举的固定提示词；进入 `/ai` 后打开 AI 助手设置作为原生核对入口，用户仍需手动带入问题并发送。
- 该交接是无授权排查入口：提示词明确要求智能体只基于交接携带的页面状态整理人工排查顺序，不能声称已读取或修改供应商配置。若需要核对 Base URL、模型名、API 密钥、本地服务或执行连接测试，必须引导用户回到“设置 → AI 助手”人工查看或点击。
- 交接不复制 Base URL、API 密钥、密钥存在性以外的敏感配置、评测证据、会话正文、生成错误正文、右侧工作区内容、会话或授权；也不允许模型要求用户粘贴或复述 API 密钥。交接本身不保存密钥、不修改供应商、不切换当前会话 Provider、不测试连接、不删除供应商、不启动评测、不重放失败生成，也不把排查建议说成已经执行。
- 保存密钥、连接测试、删除供应商或创建评测 mutation 进行中，以及删除二次确认打开时会禁用交接，避免把正在变化的设置状态交给 AI。确定性证据：`AiProviderSettings.test.tsx` 覆盖点击后只暂存 `pendingIssue` 且不触发 set-key/health/delete/evaluation；`aiIssueHandoff.test.ts` 覆盖 `aiProviderSettingsHandoff` 的 route、无 scope、prompt 边界和 canonical UUID 拒绝。API v1 / schema 076 不变。

#### 数据与备份设置交给智能体（H4-AK）

- 设置页“数据与备份”顶部现在提供无授权“交给智能体”。入口只暂存 `/ai?settings=data`、页面级备份数量、计划备份启停/最近状态和启动恢复诊断状态的固定提示词；进入 `/ai` 后打开数据设置作为原生核对入口，用户仍需手动带入问题并发送。
- 该交接不推荐任何 workspace scope，也不复制备份包 ID、备份内容、导入/导出文件、数据库、受控文件、本机路径、磁盘容量、底层错误、右侧工作区内容、会话或授权。提示词要求智能体只整理人工检查顺序；如需创建备份、重新校验、恢复演练、安排恢复、删除备份、下载完整备份、导入/导出业务数据、修改计划策略、重启应用或清理恢复残留，必须引导用户回到“设置 → 数据与备份”人工点击并核对确认。
- 交接本身不调用备份、恢复、删除、导入、导出、计划策略保存或重启 API，也不能把排查建议说成已经执行。任一备份/导入导出/恢复/计划保存 mutation 进行中，或已有恢复计划等待重启时会禁用交接，避免把正在变化或冻结的维护状态交给 AI。
- 确定性证据：`BackupSettings.test.tsx` 覆盖点击后只暂存 `pendingIssue` 并跳转 `/ai`，且不触发 create/verify/drill/restore/delete/download/export/import/policy mutation；`aiIssueHandoff.test.ts` 覆盖 `dataBackupSettingsHandoff` 的 route、无 scope 和 prompt 边界。API v1 / schema 076 不变。

#### 运行诊断交给智能体（H4-AL）

- 设置页“运行诊断”成功读取安全状态后提供无授权“交给智能体”。入口只暂存 `/ai?settings=diagnostics`、环境/Sidecar 阶段/API 健康/版本兼容等脱敏状态和固定提示词；进入 `/ai` 后打开运行诊断作为原生核对入口，用户仍需手动带入问题并发送。
- 该交接不推荐任何 workspace scope，也不复制本机路径、日志正文、端口、会话令牌、诊断包、业务数据、右侧工作区内容、会话或授权。提示词只要求智能体解释状态和整理人工核对顺序；若需要重新检查、打开日志目录、生成诊断包、重启桌面应用或处理 Sidecar，必须引导用户回到“设置 → 运行诊断”人工点击。
- 交接本身不调用重新检查、日志目录、诊断包或重启 API；诊断刷新、诊断包生成或运行状态读取进行中时会禁用交接，不把不完整状态交给 AI，也不把排查建议说成已经执行。
- 确定性证据：`SettingsModal.test.tsx` 覆盖成功读取诊断后点击只暂存 `pendingIssue`，不调用桌面操作；`aiIssueHandoff.test.ts` 覆盖 `runtimeDiagnosticsHandoff` 的 route、无 scope 和脱敏边界。API v1 / schema 076 不变。

#### 项目详情交给智能体（H4-W）

- Project 详情 `/projects/<id>` 现在使用专用“交给智能体”。入口推荐 `work+actions`，只暂存精确 route、canonical project ID、当前显示名称/状态和固定提示词；进入 `/ai` 后仍需用户点击“带入问题并选择权限”、人工发送，并在确认卡逐项核对。
- 提示词要求模型先用 `workspace_get(type=project,id=...)` 读取真实版本、受限项目资料、日期、状态和任务进度，再起草相应 `project.*` 建议；该普通 `work` 查询不返回客户身份、金额、发票或可用动作，涉及这些事实须另行核对权限与原生信息，不能猜测。交接不复制项目说明、任务列表、财务摘要、附件、笔记、客户资料、会话或授权；模型不得按名称猜测目标，也不得把页面可见内容当作已授权事实。
- 项目生命周期写入或删除在途会禁用交接。交接本身不编辑、开始、暂停、完成、归档、恢复或删除项目；完成项目不会自动完成任务，删除只适用于已归档项目并需要 `confirm_project_delete` 人工确认。交接不改写 Task/Invoice/Finance/Content/Roadmap 状态。
- 确定性证据：`ProjectDetailPage.test.tsx` 覆盖从精确项目详情点击交接后只暂存 `pendingIssue`，且不触发 transition/delete；`aiIssueHandoff.test.ts` 覆盖 `projectHandoff` 的 route、scope、prompt 边界和 canonical UUID 拒绝。

#### 客户详情交给智能体（H4-X）

- Client 详情 `/clients/<id>` 现在使用专用“交给智能体”。入口推荐 `work+clients+actions`，只暂存精确 route、canonical client ID、当前显示名称/状态和固定提示词；进入 `/ai` 后仍需用户点击“带入问题并选择权限”、人工发送，并在确认卡逐项核对。
- 提示词要求模型先用 `workspace_get(type=client,id=...)` 读取真实版本、状态、受限备注、当前本地联系人安全身份、活动/回访摘要和可用动作；如需处理具体活动或回访，再用 `workspace_client_records` 读取 detail。交接不复制联系字段、备注、项目列表、附件、活动/回访正文、财务/发票、会话或授权；模型不得按名称猜测目标，也不得把页面可见内容当作已授权事实。
- 客户更新或删除在途会禁用交接。交接本身不编辑、停用/恢复、删除、关联联系人、记录活动或处理回访；联系字段只能来自用户明确提供，删除只适用于 inactive Client 并需要 `confirm_client_delete` 人工确认。交接不联系客户、不改写 Project/Invoice/Finance 状态。
- 确定性证据：`ClientDetailPage.test.tsx` 覆盖从精确客户详情点击交接后只暂存 `pendingIssue`，且不触发 update/delete；`aiIssueHandoff.test.ts` 覆盖 `clientHandoff` 的 route、scope、prompt 边界和 canonical UUID 拒绝。

#### 任务保存视图（H2-U）

- `workspace_task_views`（work）只读本地任务筛选预设：列表按名称分页（1–20/页、offset ≤1000、总量 ≤20）或按 canonical UUID 读详情，返回名称、完整定义、版本、更新时间与 /tasks 路由；不返回任务正文、任务 ID 或查询结果，读取不应用筛选、不授予写权限。
- `work+actions` 下可提议 `task_view.create`（现实名称 + 完整 definition）、`task_view.update`（`task_saved_view_id` + `expected_version` + 至少一个 name/definition）与 `task_view.delete`（空 changes + 人工另行勾选 `confirm_task_view_delete`）。definition 字段与 Task 列表白名单一致：q/status/priority/kind/project_id/client_id/tag_ids/planned_date/planned_from/planned_to/due_from/due_to/sort；`planned_date` 与计划范围互斥，范围不得倒置，未知字段、坏 UUID/日期/排序都被拒绝。
- 提议阶段复用原生规范化与名称规则，并冻结人工可读预览：名称、全部筛选字段、真实项目/客户/标签名称（引用已删除时保留 UUID）、旧值与新值、下一版本。确认事务与原生 API 共用同一组 `*TaskSavedViewInTransaction` helper，名称大小写不敏感唯一、20 个上限与版本检查仍由领域命令负责；确认时重算并逐字比较预览，漂移返回 `AI_ACTION_PREVIEW_CHANGED`。
- 错误与回执：名称重复 `TASK_SAVED_VIEW_NAME_EXISTS`、超过上限 `TASK_SAVED_VIEW_LIMIT_REACHED`、缺少删除同意 `422 TASK_VIEW_DELETE_CONFIRMATION_REQUIRED`；确认前零写入。result_id 是视图 ID（删除时为被删 ID），route 统一 `/tasks`，因为任务页没有按视图 ID 的深链——视图只是本地预设，需要用户自行在筛选栏选用，不声称自动应用，也不改变任何 Task。

#### 知识库来源与索引管理（H3-F）

- `knowledge_actions` 是独立、单消息、仅保存会话可用的授权，不依赖 `knowledge`，也不注册 `knowledge_search`/`knowledge_read`。它只注册元数据只读工具 `knowledge_library` 与 `workspace_propose`；内容读取仍必须另选“知识检索与引用”并逐版本选择来源，两种授权互不继承。
- `workspace_propose` 的模型侧 action 枚举按同一边界裁剪：只有 `knowledge_actions` 能看到 `knowledge_source.*`/`knowledge_index_job.*`，普通 `work+actions` 不会泄漏知识库管理动作；只有 `knowledge_actions` 时也不会看到 Task、财务、发票或 Agent 执行动作。服务端确认阶段仍再次执行完整权限和版本校验。
- `knowledge_library` 提供 `view=list|source|job`：列表按 `query`（≤200 字）、`status`（ready/indexing/failed）、`limit` 1–20、`offset` 0–1000 分页；`source`/`job` 必须用 canonical UUID。返回来源 ID/名称/类型/状态/版本/大小、已提取文档数、分段数、文档版本、最近索引时间与最新任务（id/operation/status/attempt/error_code），以及真实 route。响应不含受管原文、提取正文、分段内容、哈希、文件字节或路径，`content_included` 恒为 false。
- `workspace_propose` 另接受 `knowledge_source.create`：把用户本轮明确提供的文本存为新受管来源。`changes.name` 必须是 `.txt`/`.md`/`.markdown` 文件名（同时决定 source_type/MIME），`changes.content` 必须是完整 UTF-8 文本且 ≤24,000 字节，`changes.title` 可选；创建不接受目标 ID、版本、文件字节或路径，也不得虚构或扩写正文。校验、空值/类型判定与文本合法性（UTF-8、无 NUL、非空）完全复用原生导入，未知扩展或空内容返回稳定错误。
- 工具结果为 24 KiB 上限内的实时快照；写入前必须重新读取当前版本。任务 detail 返回其来源身份；只有“最新失败任务”的 route 使用 `/knowledge?source=<id>&job=<id>`，其余状态返回知识库根路径，避免原生页面把非失败任务显示成位置无效。
- `workspace_propose` 在同一授权下接受 `knowledge_source.reindex`、`knowledge_source.delete`、`knowledge_index_job.retry`、`knowledge_index_job.cancel`。来源动作使用顶层 `knowledge_source_id` 与当前来源 `expected_version`；任务动作使用顶层 `knowledge_index_job_id` 与该任务当前的来源版本，且不得混用另一个目标字段。四项 `changes` 必须为空，只有删除可带可选 `reason`（≤500 字，空值规范为服务端默认文案）。
- 创建预览不复制整篇正文，只冻结名称、source_type、目标状态 indexing/版本 1、内容字节数、SHA-256 与开头 ≤800 字（超出时标 `proposed_excerpt_truncated`），人工据此核对模型是否如实转存了用户文本；已有来源的预览冻结名称、类型、状态、版本、文档数、分段数与任务 operation/status/attempt/error_code。所有预览显式标记 `content_included:false`（不泄露库内正文），易变的 stage/progress 不进入预览，因此运行中任务仍可被人工取消。删除预览同时冻结将删除的分段/文档数量并把目标状态固定为 deleted。
- 确认先重算并逐字比较完整预览：来源版本、分段/文档数、任务状态或脚本身份漂移返回 `AI_ACTION_PREVIEW_CHANGED`，旧卡不执行也不自动换参。确认后复用原生共享命令（创建、删除、排队重建/重试、取消）；创建与重建/重试产生的新索引任务只在审批事务提交之后入队，提交失败不会留下孤立任务或队列项（创建则连来源也不落库）。取消按剩余文档把来源恢复为 ready/failed，不删除资料；重建与重试都新开 attempt，保留原执行证据。
- `knowledge_source.delete` 需要人工另行勾选 `confirm_knowledge_source_delete`；缺失返回 `422 KNOWLEDGE_SOURCE_DELETE_CONFIRMATION_REQUIRED`，且确认前零写入。删除永久移除来源、文档/分段与受管副本，无应用内撤销，审批与索引历史保留，不影响备份或外部副本。任务终态（重试非失败/取消、取消非排队/运行中）返回稳定错误码而不是静默跳过。
- 权限与隐私：`knowledge_actions` 在非持久会话接受前返回 `422 AI_ACTION_PERSIST_REQUIRED`；未知 scope 或重复 scope 返回 `422 AI_WORKSPACE_GRANT_INVALID`。来源名称/状态等元数据会发送给所选模型并写入本轮运行步骤，正文与文件字节永不进入工具结果、审批预览或后续回执。API v1 / schema 076 不变；无新迁移、无执行工具，真实 Provider 与桌面体验另验收。

#### 本地收支操作（H3-E2）

独立 `finance+finance_actions` 支持 `financial_entry.create/update/void`，不从普通 actions 或客户范围推断。创建明确类型、金额、币种、日期、状态与分类；修改/作废携带真实 `financial_entry_id/expected_version`，作废只接受原因。完整人工预览含前后备注与关联名称，三个动作都另需 `confirm_financial_effects:true`，模型不能提供同意。共享原生领域事务，审批失败全部回滚，发票回款记录及已作废记录受保护，版本或关联变化需重提议。详情与统计缓存取消旧请求后刷新，真实结果链接和后续无正文回执沿用同一闭环。财务输入/预览采用 64/128 KiB 上限，模型总预算不扩大；发票六命令由 H3-E3 独立接入，H3-E4A 另接草稿删除，H3-E4B 已接 PDF 生成/替换审批和本次文件的精确人工下载，H3-E5 的独立 `finance_exports` CSV 导出审批与人工下载见下方发票小节。详细字段与验收见 [财务模块](finance-invoices.md#ai-本地收支操作h3-e2源码与确定性测试已实现)。

#### 发票操作（H3-E3）

独立 `finance+invoice_actions` 支持 invoice.create/update/mark_sent/mark_viewed/mark_paid/mark_overdue，仅保存会话可提议，每项均需 `confirm_invoice_effects` 人工勾选；不从普通 actions 或账本 finance_actions 继承。创建/草稿编辑和受控状态转换共用原生事务；实际全额回款与日期必须由人核实，付款原子写唯一收入并解决到期 Inbox，不完成 Task。逾期可捕获已启用规则的本地自动化投递，实际 Run 成功需另查，不以发票确认代替。完整预览、原备注与关联名称不外发，确认重验版本和快照，日期保持本地口径；卡片回读、共享事实刷新、发票详情与下一轮无正文回执已接通。H3-E4A 另支持 invoice.delete：无账本关联的草稿，要求 confirm_invoice_effects 与额外 confirm_invoice_delete；预览绑定完整发票和已存 PDF 标识，版本/关联/PDF 更换需重提议。数据库与审批同事务、文件失败补偿，成功后跳转发票列表，保留审批/审计但无应用内撤销；结果版本是最后删除版本，不是现存记录。H3-E4B 另支持 invoice.generate_pdf，需 confirm_invoice_pdf 双重人工同意，复用原生文件补偿；实际资产身份与元数据保持历史回执，卡片精确下载拒绝被替换的文件，不自动外发。H3-E5 已接独立 finance_exports 的明确范围提议、人工批准和精确 CSV 下载，正文不发送给模型；下载结果不冒充已保存，详见 [财务模块](finance-invoices.md#ai-发票操作h3-e3源码与确定性测试已实现)。

#### 通用查询契约

H3-B3 另接通 `client_activity.create/update/delete`（同需 work+clients+actions）：仅人工备注/会议，创建完整实际记录、修改提供字段或带原因软删除。现有活动绑定 client_activity_id/expected_version；确认重验客户资料和完整前后预览，复用原生事务。删除需独立人工 `confirm_activity_delete`，保留历史和附件，无撤销；系统活动只读。正文替换完整展示旧值与新值，不用截断读页覆盖；非正文操作不复制无关正文。活动与 H3-E2/E3 账本/发票输入最多 64 KiB、审批预览最多 128 KiB；H2-W7 批量建任务也使用 64/128 KiB，其余普通动作仍为 24 KiB；总模型请求仍受 64 KiB 限制。schema 073 保留旧审批、扩容 JSON，成功或模糊失败都刷新 Client 时间线/最近动态等共享缓存。详细字段、错误及验收见 [客户活动](clients.md#智能体起草人工活动h3-b3ai-独立轨道)。

`POST /api/v1/ai/chat` 可选 `workspace: {provider_version: number, scopes: ("work" | "clients" | "outputs" | "output_files" | "actions" | "knowledge" | "knowledge_actions" | "finance" | "finance_actions" | "invoice_actions" | "finance_exports" | "agent_execution" | "agent_files" | "workspace_ui" | "workspace_browser")[], knowledge_sources?: {source_id, expected_source_version}[]}`。未知/重复/空范围、outputs/actions 缺少 work、output_files 缺少 work+outputs、finance_actions/invoice_actions/finance_exports 缺少 finance、agent_execution 缺少 work+outputs+actions，或 agent_files 缺少 work+outputs+actions+agent_execution 时，返回 `422 AI_WORKSPACE_GRANT_INVALID`；`knowledge_actions` 不依赖 `knowledge`，只提供元数据与人工确认管理命令。Provider version 不符返回 `409 AI_WORKSPACE_PROVIDER_CHANGED`，拒绝发生在 AI 写入前。此对象参与请求幂等摘要，不能改变授权后复用旧请求身份。

- `workspace_guide({topic})`：只读返回一个已授权领域的代码所有操作手册；topic 枚举按本次 scope 裁剪。核心权限、不可信数据、事实读取、人工确认与成功回执规则仍常驻系统提示；任务/项目、Inbox、提醒、专注、客户、财务、自动化、产出、Agent 和工作区入口等详细规则改为按需读取。指南不返回业务数据、不授予 scope、不确认或执行动作。完整手册的每条规则必须归入核心或恰好一个 topic，测试会阻止遗漏/重复。
- `workspace_search({type, query?, status?, project_id?, limit?, offset?})`：work 白名单为 task/project/person/inbox_item/roadmap_milestone/content_item/reminder，clients scope 另开放 client；query ≤200 字，limit 1–20（默认 10），offset 0–1000；project_id 仅适用于 task/content_item。返回 items、next_offset、has_more、window_limited，更新时间/ID 稳定倒序。`type=inbox_item` 另返回同一服务端快照的 `snapshot_at/snapshot_unread_total/read_all_max_items`；这些全局字段不受当前页和筛选缩小，只能用于 H2-C5 的人工审批。
- `workspace_get({type, id, content_offset?})`：详情 ID 必须为 UUID；普通业务详情复用有界说明（≤2,000 字）。artifact/agent_run 文本每页 ≤4,000 字，offset ≤1,000,000，并返回总长度与下一段偏移；不读取文件字节或执行内容。
- `workspace_outputs({type: "artifact" | "agent_run", task_id, limit?, offset?})`：同样分页上限；Agent Run 列表额外返回 `attempt` 与可空 `parent_run_id`，便于沿同一 Task 的重试链定位精确 Run；列表只有元数据，正文须再 get。`workspace_get(type=agent_run)` 同样回传这两个 lineage 字段，仍不返回输入快照或 staging。
- `workspace_task_submissions({task_id, view, submission_id?, status?, field?, content_offset?, content_limit?, limit?, offset?})`：仅 work+outputs；view 为 list/text/artifacts，按真实批次读取摘要、验收意见与产出元数据。字段按视图严格限制，详见上方 H2-E1。
- `workspace_read_artifact_file({task_id,task_version,submission_id,artifact_id,expected_sha256,content_offset?,content_limit?})`：另须独立 output_files，纯只读校验并分段返回受管 UTF-8 文件（≤64 KiB）；同页区分完整性与正文覆盖范围，不扩展 workspace_get，详见 [H5-F1](tasks.md#ai-受控文件产出复查h5-f12026-09-21)。
- `workspace_project_notes({project_id,view,note_id?,expected_version?,content_offset?,content_limit?,limit?,offset?})`：仅 work；list 返回未删除笔记元数据与精确 route，detail 必须同时绑定 Project、Note 和 Note version，并按 Unicode 分段读取正文。删除笔记不可由工具读取；人工 H4-G 页面仍可从原生详情查看删除元数据，但正文保持为空。
- `workspace_inbox_tasks({inbox_item_id,state?: "active" | "history",limit?,offset?})`：需要 work，按上方 H2-C2 契约查询有界关系，不读取关系事件正文。
- `workspace_task_options({type: "actor" | "tag",query?,limit?,offset?})`：需要 work，字面量名称搜索 ≤200 字，分页同 search。只返回 id/label/type/version；Actor 只含 active owner/person 或 enabled/execution_ready Adapter 对应的 active agent，不含备注、联系方式、metadata 或配置。分派不表示已执行。
- `workspace_finance({view,...})`：仅 finance 范围，五种视图为 entries/entry/invoices/invoice/summary；明细要求实际 UUID，列表每页 1–20、offset 0–1000，summary 必须明确币种及 1–366 个含首尾日期。字段、统计口径和工作台 route 见 [财务契约](finance-invoices.md#ai-财务查询与往返h3-e1)。不授予记账、付款、作废、PDF 或导出。
- 工具 Schema 枚举按授权裁剪；H5-E28 将辅助定义按指南领域加载，执行时仍再次验证 scope、Provider config_version/ready 与恢复状态。结果 JSON 最大 24 KiB，超过时报错要求缩小范围；仍受整次运行累计预算限制。最大 scope、20 个知识来源和近 8 KiB 审批回执的双协议初始请求必须在 64 KiB 上限内保留至少 24 KiB 余量；不提高上限、不丢审批回执，也不靠截断核心安全规则。
- 接受事务追加 `ai_workspace_access_granted`，只记录 generation/provider/config version/scopes；工具终态沿用 run steps。授权不保存为下一轮权限。H1 查询本身无迁移；H2-A 审批增加 schema 072 与上方两条路由，手选上下文、知识片段和旧任务确认接口保持兼容。

## 关键用户流程

需要助手主动查工作时：主对话或侧边聊天输入区“工作台权限” → 在当前输入器勾选范围并阅读模型外发说明 → “仅授权下一条消息” → 发送问题。运行后在“运行详情”查看工具终态，使用详情链接进入工作台；停止可终止后续查询但不能撤回已发送数据。需要普通操作时另勾选 actions，收支操作另勾选 finance_actions、发票另勾选 invoice_actions（两者均依赖 finance）；完整回复下方逐项核对变更后确认，侧聊提议就在原消息下处理，财务另需核验勾选；未确认的建议不改变工作台。发送接受后当前输入器清除授权，主/侧和下一轮均不继承。

1. **配置供应商**：设置 → AI 助手 → 「添加供应商」选择类型：
   - 远程 API：填名称/协议/Base URL/模型名登记 → 保存 API key（进系统安全存储）→ 测试连接至「已就绪」。
   - 本地部署：启动本地 OpenAI 兼容服务（Ollama 默认 `http://127.0.0.1:11434/v1`，LM Studio 默认 `http://127.0.0.1:1234/v1`）→ 填名称/Base URL/模型名登记（无需密钥，协议锁定 OpenAI 兼容）→ 测试连接至「已就绪」。
2. **发起会话**：进入 `/ai`，多个就绪供应商时在输入区下拉选择本次使用的供应商（本地供应商带「本地」标识），输入问题（Enter 发送）；输入框聚焦时由整个输入区显示单层圆角焦点边框，文本区不叠加方形轮廓，键盘切到上下文、供应商或发送按钮时保留各控件的独立焦点提示。回答流式逐字显示，可点「停止」，取消保留已生成部分。宽屏聊天工作区会填满 AppShell 的中央面板；当窗口进入紧凑布局时，会话列表收为页头的会话选择器和新建按钮，优先保证消息与输入区的可读宽度。
3. **显式业务上下文**：展开“上下文”→ 各选最多一个 Task/Project/Client →“预览发送内容”查看目标 Provider、全部字段/截断和字节数 →“确认用于下一条消息”→ 发送；版本变化会拒绝并保留选择供重新预览。
4. **显式知识上下文**：在同一面板搜索本地知识库 → 逐段选择最多 3 个结果 → preview 检查完整正文、来源、位置、版本和远程/本地披露 → 确认发送；搜索未选结果和整份来源不进入模型。
5. **语义建任务**：自然语言“尚未创建”与待确认建议 → 编辑/明确确认 → 服务端 Task 领域事务原子创建并绑定 → 显示真实 Task 名称和入口。响应丢失或刷新后同消息重试返回已有 Task，改参冲突；已删除任务不会用原建议重新创建。
6. **本地评测与审计**：设置中显式选择 smoke/topic/full → 本地 Actor 顺序执行 → 按 dataset/suite/真实配置身份展示计数、事实规则失败、趋势/Wilson。健康检查不拆组，真实配置变化形成新组，旧历史无需删除。只有当前 full 的同配置重复证据可形成只读人工候选；规则、重复次数和 Wilson 均不是发布许可。
7. **清理**：会话删除会取消生成/压缩并清理其操作态；供应商只在没有 generation 或评测历史时允许删除，远程同时清理安全存储密钥。终态评测历史可显式删除；知识来源删除不追溯改写已发送的会话快照。

## 数据、API、状态与事件

### 数据

- `ai_providers`：id/name/kind/protocol/base_url/model、状态/健康、has_key、last_health_at、HTTP `version`、schema 068 `config_version`、时间戳；配置身份只随类型/协议/端点/模型/实际密钥变化。
- `ai_sessions`：id、title、persist、version、时间戳；响应派生 `compacted_message_count`，不复制进表。
- `ai_generations`：id/session_id/provider_id、queued/streaming/completed/failed/cancelled、error_code、nullable content（≤1 MiB）、时间戳；schema 069 增加配对且不可变的 nullable request_key/request_hash，旧行保持 NULL。
- `ai_messages`：既有 role/status/content/reasoning/model/task 引用；schema 058 的 nullable `context_snapshot` 用于持久 user message，v1 保存 Provider + 业务 sources，向后兼容的 v2 另保存最多 3 个 knowledge chunk；数据库防线 32 KiB，业务层 16 KiB、知识层 12 KiB、组合 30 KiB。
- schema 060 为 `ai_messages` 增加 nullable `citations_snapshot`：只允许 completed assistant，version 1、最多 3 项、16 KiB 防线；只保存 Sidecar 重建的来源/文档/chunk/版本/位置，不保存正文或模型 quote。
- schema 061 新增 `ai_run_steps` 和 assistant generation link；schema 062 增加全有/全无的 nullable provider token 列。既有 generation 只回填 summary root 且 token unknown；会话删除级联 steps，便携业务导出排除。
- schema 063 新增 `ai_evaluation_runs / ai_evaluation_results`：Run 保存 Provider/数据集快照、状态/进度/取消/稳定错误码；Result 保存 case identity、passed/failed/error、稳定 failure code 数组、citation 数与无正文指标。每 Provider 最多一个活动 Run，Result 不可更新；终态 Run 删除级联 Result。
- schema 066 新增 `ai_evaluation_reviews`：保存精确质量组计数/Wilson/readiness/严重 code 快照、三值人工决定、必填理由与 builtin owner Actor 快照；UPDATE/DELETE 由 trigger 拒绝，且不依赖 Provider 外键。
- schema 067 受保护共同重建 Run/Result/Review 并逐列保留历史，把三表 suite CHECK 扩展为 smoke/full + 四个专题 key；恢复原外键、索引和不可变 trigger。
- `ai_memories`：id、content（1–500 字）、source_message_id（可空）、时间戳（schema 056；不进入业务导出）。
- `ai_memory_entries`：session_id、kind、content/tags/origin/status、source_message_id 与时间戳；schema 069 增加 source_message_offset、decision、memory_id。offset>0 为清理控制块后 UTF-8 已处理字节，0 为整条完成；确认/拒绝与快照 payload 均不可重写，active→superseded 受控。
- API 密钥：仅 OS 安全存储（服务名 `opc-workspace-ai`，账户 `ai:<provider-id>:api_key`）。
- 十一张 AI 表（含 schema 072 的 `ai_action_proposals`）均不进入业务 JSON/ZIP 导出（操作态与隐私边界，同 ADR-004/007/012/013/022）。

### API

- `GET /api/v1/ai/generations/:id/actions`、`POST /api/v1/ai/actions/:id/decision`（当前 generation 的持久审批列表与逐项人工决定，契约见 H2-A–F 及 H3 各领域接续）
- `POST /api/v1/ai/messages/:id/task-confirmation`（显式人工命令；原子 Task 创建/静态绑定，返回 `{data:{task,message}}`）
- `GET /api/v1/ai/active-generations`、`GET /api/v1/ai/generations/:id`、`GET /api/v1/ai/generations/by-request/:key`（活动内存/持久终态回读；完成态可含 citation_status/citations；非持久终态只在 15 分钟内、最多 32 项内存缓存中回读引用，不返回正文；缺失字段表示不可恢复）
- `GET /api/v1/ai/memory-proposals/:id`、`GET / DELETE /api/v1/ai/messages/:id/memory-decision`（新提议和旧无 proposal_id 消息的决定回读/忽略；GET 不产生写入）
- `GET /api/v1/ai/sessions/:id/compaction`（运行状态、稳定错误码、部分消息和完整覆盖消息数）

- `GET / POST /api/v1/ai/providers`、`GET / PATCH / DELETE /api/v1/ai/providers/:id`、`POST /api/v1/ai/providers/:id/health`、`POST /api/v1/ai/providers/:id/key`
- `GET / POST /api/v1/ai/sessions`、`GET / DELETE /api/v1/ai/sessions/:id`、`GET /api/v1/ai/sessions/:id/messages`
- `GET /api/v1/ai/sessions/:id/plan`、`GET /api/v1/ai/sessions/:id/plan/continuation?expected_version=N`、`POST /api/v1/ai/sessions/:id/plan/close`（AH-05 用户显式关闭最新修订）、`GET /api/v1/ai/work-plans?state=attention|all|running|needs_approval|needs_recovery|awaiting_continuation|completed|closed`
- `GET / POST /api/v1/ai/memories`、`DELETE /api/v1/ai/memories/:id`
- `GET /api/v1/ai/memory-proposals`、`DELETE /api/v1/ai/memory-proposals/:id`（确认复用 `POST /ai/memories` + proposal_id）
- `POST /api/v1/ai/context/preview`（Provider + 最多三类业务 source + 最多 3 个 knowledge chunk identity → 精确快照、版本、正文/位置、截断与外发边界）
- `POST /api/v1/ai/chat`（SSE `opc-ai-sse-v1`；反思为 agent 自发行为，无请求参数）、`POST /api/v1/ai/generations/:id/cancel`、`POST /api/v1/ai/messages/:id/task`
- `GET /api/v1/ai/generations/:id/steps`（内容无关的有序本地时间线与 bytes/duration 汇总）
- `GET /api/v1/ai/usage-summary?session_id=&provider_id=&trend_days=1..30`（可选双过滤；同一只读事务聚合 generation 根步骤、状态、Provider usage/unknown、原始 token、bytes、duration 和 Provider 分组；另返回连续 UTC 终态趋势）
- `GET / POST /api/v1/ai/evaluations`（分页历史；显式选择 Provider ID/version 并可带 Idempotency-Key 创建本地评测）
- `GET /api/v1/ai/evaluations/:id`、`POST /api/v1/ai/evaluations/:id/cancel`、`DELETE /api/v1/ai/evaluations/:id?confirm=true`（无正文详情、取消、终态历史删除）
- `GET /api/v1/ai/evaluation-summary?provider_id=&limit=`（单只读事务返回全部 Run 状态计数、succeeded-only 同口径质量分组、四类 Result category、failure-code affected/occurrences 及最近 1–50 次完整 Run 趋势）
- `GET / POST /api/v1/ai/evaluation-reviews`（倒序分页/Provider/decision 筛选；POST 在同一写事务重算精确证据，支持幂等并拒绝 stale 快照；无编辑/删除端点）
- 消息 API 另返回 `citation_status / citations`；`not_requested` 为 NULL 快照的派生态，其他四态来自 schema 060 服务器验证结果。

### 状态与事件

- schema 068：Provider `config_version` 与 HTTP `version` 分离；Run/Review `provider_config_version` 为 nullable 历史快照。相同配置的健康检查不拆组；旧 NULL 身份保留未知，不改写历史审计。数据集当前 v4/24，新增 `FACT_CONTRADICTED`，自动事实规则不等于通用语义判断。
- schema 069：Generation `request_key/request_hash` 配对且不可变；Message `task_confirmation_hash` 固化已确认载荷；MemoryEntry `decision/memory_id` 固化确认/拒绝，`source_message_offset` 记录 UTF-8 部分进度。确认 Task/Memory 被删除后，旧建议不重新创建。
- 记忆 GET 返回 `pending / confirmed / rejected`。旧无 proposal_id 卡片只在用户明确确认/忽略时 materialize 稳定记录；刷新可回读，已确认不能被“忽略”撤销。无法验证决定的旧 superseded 提议返回 `409 AI_MEMORY_DECISION_UNAVAILABLE`，不冒充已忽略；缓存重放仍检查记忆是否被删除，已删除返回 410。`persist=false` 三工具共享运行内内存，临时提议无持久 proposal_id；跨会话永久记忆须另外明确确认。
- 应用级生成状态跨 SPA 路由保留，并有全局停止入口；浏览器 storage 只保存恢复 ID。硬刷新/断连取消原 SSE，上游和终态通过回读收敛，不能自动重复发送。接受前失败保留草稿/上下文，不覆盖后来输入；纯内存草稿不承诺硬刷新恢复。中文输入法 composition Enter 不发送。
- 完整 SSE 终态回合仅在应用内存保留，最多 20 回合 / 8 MiB（用户输入、回答和思考合计），超限淘汰最旧回合，删除会话同步清除；持久消息按 generation 去重。无持久 message ID 的回合只读，不调用任务/记忆确认 API。恢复有命令代次检查，旧结果/404 不覆盖新请求；非持久终态只回元数据或服务重启 404 时，已收片段继续可见但标记不完整，不能宣称获得完整回答。
- 模型网络等待不占全局 maintenance/Provider 锁；数据库准备/收尾短锁仍保护恢复边界。整次 generation 共享 10 分钟/1 MiB，包含多轮、工具结果与自检修订。只有内容的 EOF 为失败；OpenAI stop 后继续收 usage；缺失/非法 selfcheck 为 unavailable，不伪装通过。
- Citation `validated` 仅证明引用来自本次已确认 allowlist，不能证明回答事实正确或逐句被引用支持。

- Generation 状态链：`queued / streaming → completed | failed | cancelled`；终态不可改（仅启动恢复把遗留活跃态标 cancelled）。
- Workflow Event（脱敏，不含提示/回答/任务/摘要/记忆正文）：Provider/Generation 既有事件，`ai_session_compacted`，以及 `ai_session_memory_written / ai_memory_proposed / ai_memory_proposal_confirmed / ai_memory_proposal_rejected`（aggregate `ai_session`，只记资源 ID、kind、tag/count）。
- 主要错误码：既有 Provider/Generation/Message/Memory 错误族，AI5 `AI_CONTEXT_*`、AI6 `AI_KNOWLEDGE_CONTEXT_*`，以及仅编码/持久化失败使用的 `AI_CITATION_PERSIST_FAILED`；模型 citation 缺失/非法是可解释质量状态，不伪装成网络失败。

## 与其他模块协作

- **任务**：人工确认接口与 `POST /api/v1/tasks` 复用同一创建领域逻辑；确认、Task 创建和静态绑定同事务，不跟踪后续任务状态，不绕过生命周期/验收。取消误建任务仍在任务模块操作。
- **设置**：Provider 配置区挂在设置「AI 助手」模块，独立自持保存（不走共享 draft/preview）。
- **数据管理**：AI 十表为操作态/隐私边界，排除出业务导出；一致性备份（SQLite 快照）仍覆盖它们。
- **知识库**：AI6 手选 identity 交给 context preview；H3-D2 由用户单次授予来源范围后模型可通过 knowledge_search/read 按需读取。共享 FTS 与精确 chunk 读模型，不扫描任意磁盘；重建/删除通过版本或不存在门禁使旧确认失效，搜索摘要不加入引用 allowlist。
- **诊断/日志**：普通日志只记 provider/generation ID、阶段与错误码；密钥由 operationlog 的 secrets 机制脱敏。
- **本地 Agent（v0.2）**：能力默认隔离。Agent Adapter 是受控执行器（ADR-003 匿名管道）；AI 助手只有在本次显式 `agent_execution`、冻结预览和执行确认后才能创建一个 Run。额外 `agent_files` 只把同 generation opaque 候选映射为当前 Task/Project 范围的受控 UTF-8 输入，且另需文件发送确认；它不继承 Adapter 内部配置、桌面 Shell、任意文件系统、Git、终端或浏览器能力。

## 分阶段实施

1. **AI2 评审**（已完成）：ADR-004 固化远程 Provider 授权、密钥与数据外发边界、语义建任务机制。
2. **AI3 Adapter 基线**（已完成）：Provider 登记/健康/密钥 API 与设置区、schema 052。
3. **AI4 只读会话**（已完成）：schema 053、SSE 流式聊天、取消/断连/超时/并发闸门、启动清理、会话与消息 API、`/ai` 页面。
4. **AI4.5 语义建任务**（已完成）：代码所有系统提示词、结构化块解析、待确认卡片、既有任务 API 落地与静态引用。
5. **AI4.6 Harness 与本地大模型**（已完成）：ADR-005；`internal/harness` 运行循环/工具注册表/执行器/预算（该阶段生产零工具，后由 AI4.8 G4 开放记忆三工具）、schema 055 `kind` 列、本地 Provider 无密钥全链路、设置区类型选择与本地徽标。
6. **AI4.7 纠错/反思/长期记忆**（已完成）：ADR-006；工具失败回填重试（上限 3）、模型自发自评块驱动的静默修订轮（上限 1）、schema 056 `ai_memories` 建议块确认流与注入预算、设置区记忆管理。
7. **AI4.8 长程上下文压缩与记忆工具**（已完成，[ADR-007](../adr/007-session-context-compaction-and-memory-tools.md)）：G1 完整回合窗口与精确预算；G2 schema 057 快照、后台压缩、水位线、摘要/事实注入；G3 OpenAI/Anthropic 工具协议；G4 记忆三工具与持久确认门禁；G5 压缩标记和 pending proposal 管理均已交付。
8. **AI5 显式上下文**（已完成，[ADR-008](../adr/008-ai-explicit-business-context.md)）：三类单选、最小字段白名单、发送前预览、Provider/source version 重验、一次性发送、schema 058 历史快照与来源卡片。
9. **AI6 知识库与来源**（已完成，[ADR-010](../adr/010-ai-explicit-knowledge-context.md)）：本地搜索、最多 3 个显式 chunk、完整发送预览、Provider/source/document 双重版本重验、v2 历史快照和来源 chips；没有新增工具。回答级 citation 已由 ADR-011 AI7-Q1 补齐。
10. **AI7 质量闸门与扩展**（进行中）：Q1 回答级 citation；Q2 v4/24、smoke/topic/full、本地 Actor、配置身份分组/趋势/category/failure/Wilson/人工决定审计；Q3 无正文 steps、原始 Provider token/unknown、聚合与 UTC 趋势已实现；ADR-025 修复运行、确认、恢复和隐私。费用、句子级证据覆盖、Q4 自动路由/更多协议待定，F4 编排/子代理未授权。
11. **H5 Agent 委派纵切**（部分完成）：H5-A 已接通独立授权/人工确认的 Run 创建，H5-B 已接通可恢复产出登记，H5-C 已接通当前 Task Artifact/当前 Project Attachment 的受控文本输入、local/remote 离机披露、独立文件确认和单文件/2–4 受控文件输出；H5-D/H5-E1 已接会话持久计划、真实回执投影、计划续办队列和显式单消息接续，H5-E2 又把计划外可处理 Run 去重接入同一续办入口，H5-E3 再集中计划外待确认操作与当前待验收提交，H5-E5–E10 补齐当前自动化、知识、生成、Provider、恢复诊断与系统维护发现，H5-E4 让回复中的七类严格链接可在用户点击后打开对应右侧工具标签。H5-G1 另行授权后可等待审批/执行事实并有界自动续办，H5-G12 只恢复明确选择且无在途生成的跨普通重启等待态，H5-G25 接通一层/四子会话的受确认委派；不能自行批准业务。任意文件系统、Shell/Git/终端/浏览器自动操作、未知请求跨重启重放、通用子代理消息/中断协议、尚未建模的失败来源与完整长任务自动恢复仍待独立设计和授权。

## 验收标准（当前切片已覆盖项）

- 未配置/密钥无效/端点不可达时：健康检查给出可读错误码，聊天 4xx/5xx 稳定错误，核心模块完全可用（以当前全量门禁结果为准）。
- 密钥不进 SQLite/日志/响应/导出：key 端点响应断言不含密钥原文；删除供应商清理安全存储（Go 测试覆盖）。
- 本地 Provider 无密钥 创建→健康→流式聊天 全链路（httptest 回环上游）；密钥端点 409 `AI_KEY_NOT_ALLOWED`；本地非回环端点与非法 kind 被拒；远程行为回归不变（Go 测试覆盖）。
- harness 单元测试：单轮直通、多轮工具循环与回填、轮数预算、取消传播、重复工具名拒绝、执行器超时/panic/截断/总预算（假 LLM/假工具覆盖）；`chatAI` 契约回归（既有 AI API 测试全绿）。
- 结构化任务/记忆 null、数组、字段错型、重复、缺失和未闭合不崩溃或建任务；历史坏消息可打开。确认复用 Task 领域事务；并发、响应丢失、刷新同身份不重复创建，改参/删除后重建受控拒绝。
- 取消/断连终止上游并保留部分内容；并发 409；启动恢复遗留生成（Go 测试覆盖）。
- 流式帧序 meta/progress/delta/reasoning/replace/done 与 openai/anthropic 双协议映射（mock 上游 Go 测试 + 前端 SSE 解析测试覆盖）；progress 为独立 Sidecar 事件，开始/终态与恢复使用同一序号，非法正文、错序或状态回退拒绝。
- 设置表单类型切换（本地隐藏密钥、提交载荷带 kind、协议锁定）、本地卡片无密钥行、聊天页「本地」标识（Web 测试覆盖）。
- 纠错/反思：工具失败回填重试与超限终止（harness 单测）；自评充分→单次调用且块被剥离、不充分→恰好一次内部修订并以 `replace` 更新界面（note 回填）、缺失/未闭合/非法块防御性剥离（API 契约测试 + harness 单测）。
- 记忆：建议块解析（合法/非法/超长）、确认落地与幂等、列表/删除、注入预算（数量/字节/超大跳过）、事件不含内容、导出排除（Go + Web 测试覆盖）。
- 上下文压缩：v56→57 加法迁移保留事实并约束单 active 快照/受控 supersede；严格 JSON 形状与摘要/事实预算、窗口外批次、水位线推进、连续快照、失败不落库、会话互斥/关闭取消、事件不含摘要正文，以及聊天注入三层上下文均有 Go 测试。
- 工具与前端：两协议单/多工具跨帧、异常参数、call/result 映射和 Harness 定义有单测；mock 会话覆盖真实 memory_write 两轮调用。持久会话 pending 提议可存操作表，但未经确认不写 ai_memories；非持久三工具仅内存。确认/忽略/删除后重试、事件脱敏和压缩状态有 Go/Web 回归。
- 稳定性：跨 origin redirect 不访问目标、同 origin redirect 可用；Provider 密钥并发写在 race detector 下保持 Keyring/SQLite 一致；已使用 Provider 删除受控拒绝且保留会话；远端错误回显密钥被脱敏；非持久会话不留正文；500 个 Unicode 字符记忆可保存。
- 显式上下文：Go 覆盖字段白名单/排除项、Unicode 截断、类型/重复/不存在来源、Provider/source 版本变化无脏写、prompt 注入、持久历史和非持久会话；Web 覆盖未预览禁发、精确预览、发送载荷、Provider 变化失效和历史来源卡片。
- 显式知识上下文：Go 覆盖 preview→chat→v2 snapshot→history、chunk/source/document 关系、source/document version 变化零写入；ModelClient/Harness 覆盖双协议不可信引用段、工具/自评轮上下文保持；Web 覆盖本地搜索、逐段选择、完整预览、精确版本载荷、历史来源 chips 和未确认禁发。
- 可验证引用：schema 059→060 保留消息并约束 completed assistant/shape/状态/数量；Go 覆盖 allowlist、空/缺失/非法/重复/越权/多块/未闭合、服务器 metadata 重建、控制块剥离与 history；Web 覆盖 strict 状态一致性、已验证来源卡、无答案/缺失/非法警告及流式块隐藏。
- 质量门禁、本地运行与分析：v4/24、6/8/24 套件严格加载；结构/关键词/有限事实规则/人工审核分层；配置身份正负例、健康不拆组、旧 NULL/审计兼容、生产提示与真实 Harness 修订/usage 链有回归。真实模型效果仍未由自动化验证。
- 运行步骤与聚合：schema 060→062 保留消息/generation 并回填 summary root/unknown usage；Harness step callback、PromptSize 字节、32 tool-call 上限、Chat 终态、OpenAI/Anthropic 完整 usage、API 禁止正文及 Web 时间线均有测试。聚合 API 另覆盖 session/provider 双过滤、空范围、终态/活动与 coverage 对账、Provider 分组、404/400；Web 会重算总计并显示 unknown/不计费边界。
- H2-C5 快照式全部已读：隔离测试覆盖服务端 snapshot 元数据、严格 changes、空/过大范围、候选 `ID+version` 指纹、快照后新增/更新、确认前变化冲突、事务回滚、重复确认、专用回执、主/侧卡解析及决定后的缓存刷新；真实模型和运行中旧服务不由这些确定性测试替代。
- 侧边工作区聊天：隔离测试覆盖单条 workspace grant 随发送透传并在接受后清除、主对话/前轮不继承、未授权不注册业务工具、持久 assistant message 下就地显示审批卡，以及主/侧打开同一 generation 时复用同一服务端 proposal/query 状态；旧任务块和长期记忆建议不在侧聊生成第二套确认入口。
- H5-C 受控文件：确定性测试覆盖无文件 text 保持 v1、受控输入/单文件输出使用 v2、2–4 输出文件使用 v3、Task Artifact/当前 Project Attachment 来源、4 个/64 KiB/128 KiB 与 UTF-8/NUL 门禁、prepare/launch 双复核、v2/v3 strict/fail-closed、重试冻结引用/输出契约、活动引用删除互锁、原生 `confirm_file_access`、AI generation-scoped opaque ID、执行/文件双确认、local/remote 离机披露、单个 Submission 的多 Artifact 登记/恢复和按冻结名称预览下载。真实 Provider、原生桌面、网络和断电故障仍单列，不由隔离测试替代。

### 本地模型真机验证步骤（需用户本机环境）

1. 安装并启动 Ollama（`ollama serve`，默认 11434）或 LM Studio（打开本地服务，默认 1234）。
2. 设置 → AI 助手 → 添加供应商 → 类型选「本地部署」→ Base URL 填对应地址 → 模型名填已拉取的模型（如 `qwen3`）→ 登记。
3. 点「测试连接」至「已就绪」；进入 `/ai` 选择该供应商发起会话。
4. 断开本地服务后「测试连接」应显示「端点无法访问」，聊天报稳定错误；核心模块不受影响。
5. 保持服务已就绪，在同一设置区先运行 8/8 快速检查或直接运行 24/24 完整评测；只有同配置完整套件可积累人工评审候选。该实机输出是当前模型快照的本地观察，不作为跨模型稳定真值，也不保存原回答。

## 相关 PRD 与代码链接

- [产品 PRD](../opc-workspace-PRD.md)（§5.10）
- [ADR-004：远程 Provider 接入与安全边界](../adr/004-ai-assistant-provider-access.md)
- [ADR-005：Agent Harness 架构与本地大模型接入](../adr/005-agent-harness-and-local-models.md)
- [ADR-006：Harness 完整组件矩阵与自进化边界](../adr/006-harness-matrix-memory-evolution.md)
- [ADR-007：会话上下文压缩与记忆工具](../adr/007-session-context-compaction-and-memory-tools.md)（G1–G5 已交付；[分阶段计划](../plans/context-memory-phases.md)）
- [ADR-008：AI 显式业务上下文与发送前预览](../adr/008-ai-explicit-business-context.md)（AI5 已交付；[分阶段计划](../plans/ai-explicit-context-phases.md)）
- [ADR-010：AI 显式知识片段上下文与来源引用](../adr/010-ai-explicit-knowledge-context.md)（AI6 已交付；[分阶段计划](../plans/ai-knowledge-context-phases.md)）
- [ADR-011：AI 回答的可验证知识引用](../adr/011-ai-validated-knowledge-citations.md)（AI7-Q1 已交付；[AI7 计划](../plans/ai-quality-gates.md)）
- [ADR-012：AI 运行步骤与本地无正文指标](../adr/012-ai-run-steps-and-local-metrics.md)（AI7-Q3 steps、Provider usage 与本地聚合已交付）
- [ADR-013：AI 本地模型质量评测 Actor](../adr/013-ai-local-quality-evaluation-runner.md)（AI7-Q2 显式本地运行闭环已交付）
- [ADR-014：AI 本地质量分组与时间趋势](../adr/014-ai-local-quality-trends.md)（AI7-Q2 同口径趋势已交付）
- [ADR-015：AI 本地质量类别分解](../adr/015-ai-local-quality-categories.md)（AI7-Q2 category 统计已交付）
- [ADR-016：AI 本地质量失败原因聚合](../adr/016-ai-local-quality-failure-codes.md)（AI7-Q2 failure-code 统计已交付）
- [ADR-017：AI 本地质量评测数据集 v2](../adr/017-ai-local-quality-dataset-v2.md)（AI7-Q2 12-case 多样本纵切已交付）
- [ADR-018：AI 本地质量 Wilson 区间与重复证据](../adr/018-ai-local-quality-confidence-intervals.md)（AI7-Q2 描述性不确定性已交付）
- [ADR-019：AI 本地质量人工评审候选](../adr/019-ai-local-quality-advisory-readiness.md)（AI7-Q2 advisory readiness 已交付）
- [ADR-020：AI 本地质量评测数据集 v3](../adr/020-ai-local-quality-dataset-v3.md)（AI7-Q2 24-case 事实覆盖已交付）
- [ADR-021：AI 本地质量分层评测套件](../adr/021-ai-local-quality-tiered-suites.md)（AI7-Q2 8-case smoke / 24-case full 已交付）
- [ADR-022：AI 本地质量人工决定审计](../adr/022-ai-local-quality-human-review-audit.md)（AI7-Q2 精确证据快照、必填理由与不可变历史已交付）
- [ADR-023：AI 本地质量代码所有专题套件](../adr/023-ai-local-quality-topic-suites.md)（AI7-Q2 四类 6-case 专题已交付）
- [ADR-024：AI 本地用量 UTC 时间趋势](../adr/024-ai-local-usage-time-trends.md)（AI7-Q3 1–30 天趋势已交付）
- [ADR-025：AI 运行可靠性、确认事务与评测配置身份](../adr/025-ai-reliability-confirmations-and-evaluation-identity.md)（schema 068–069；本轮验证见 AI7 计划）
- [MVP 计划草稿](../plans/ai-assistant-mvp.md)、[Harness 分阶段计划](../plans/agent-harness-phases.md)、[本地 Agent 执行](local-agents.md)
- Sidecar：`services/sidecar/internal/api/ai_providers.go`、`ai_sessions.go`、`ai_chat.go`、`ai_messages.go`、`ai_memories.go`、`ai_context_memory.go`、`ai_evaluations.go`、`ai_evaluation_runner.go`、`ai_evaluation_reviews.go`、`internal/aieval/`、`internal/harness/`、`internal/modelclient/`、`internal/keystore/`
- 前端：`apps/web/src/pages/AiAssistantPage.tsx`、`apps/web/src/components/AiProviderSettings.tsx`、`apps/web/src/api/ai.ts`、`apps/web/src/lib/aiTaskCard.ts`
- 迁移：`services/sidecar/internal/database/migrations/052_ai_providers.sql` 至 `069_ai_reliable_confirmations.sql`
- 工作台桥接：`services/sidecar/internal/api/ai_workspace_tools.go`、`ai_workspace_actions.go`、`ai_task_batch_actions.go`、`ai_agent_run_actions.go`、`agent_run_files.go`、`ai_project_actions.go`、`ai_inbox_actions.go`、`ai_inbox_context.go`、`inbox_commands.go`、`ai_focus.go`、`focus_commands.go`、`ai_action_receipts.go`、`ai_task_confirmation.go`，以及 `apps/web/src/components/AiWorkspaceAccess.tsx`、`AiWorkspaceActions.tsx`、`apps/web/src/api/aiWorkspaceActions.ts`；审批迁移 `072_ai_action_proposals.sql`。定向验收见同名 `*_test.go`、组件测试与 `AiAssistantPage.integration.test.tsx`。
