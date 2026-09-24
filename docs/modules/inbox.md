# 收件箱与本地工作编排模块

AI 独立轨道增补（2026-09-21）：拆分确认后，实际新建 Task ID 按草稿顺序保存在同事务的不可修改审批事件中；对话结果卡可逐项打开，后续模型回执只携带历史 ID。旧审批无映射时不从当前关联推断，任务改名或删除不改变历史身份。人工拆分 API、分派与验收规则不变，无迁移。详见 [精确历史回执](ai-assistant.md#收件箱拆分的精确历史回执2026-09-21)。

> 人工基线记录：2026-08-29；AI 独立轨道增补：2026-09-18（依据当前代码与测试）
>
> 当前基线：app v0.1.1 / API v1 / SQLite schema v76（Inbox 既有事实仍由 v12/v13/v15 等迁移定义，v72 新增 AI 审批，v73 扩容其 JSON，v74–v76 仅扩展 Agent/AI 操作态）。T-11A1/B 手工受理分诊、T-11A2 已有 Task 关系、T-11A3 一次性及 daily/weekly/weekdays/monthly Reminder、T-11C 批量拆分/分派/自动结清，以及已登记来源投影均已交付。schema v33 的受限 Project 完成预设可追加一条本地“检查开票”Inbox Item；schema v35 的到期 Client Followup 会创建本地来源并只深链客户详情；schema v38 的审核/发布内容来源会通过内容 ID 精确深链最新详情；schema v40 的 monthly 仍为每个 occurrence 生成独立 Inbox 来源，不改 Inbox 表或解决契约；v0.1 不启用 AI、LLM 或 Agent Runtime。

导航：[文档中心](../README.md) · [整体功能架构](../functional-architecture.md) · [PRD v10.12](../opc-workspace-PRD.md) · [任务](tasks.md) · [Actor 与分派](actors.md) · [本地提醒](reminders.md) · [预设自动化](automation.md)

## 来源详情与原对话往返（H4-AT，2026-09-21）

当前已实现原生来源卡的精确导航：任务阻塞/临期、发票、项目、内容、客户回访和提醒保持对应对象；路线图改为 `/roadmap?milestone=<id>`；任务产出按历史快照的 Task/Submission 定位 `/tasks/:taskId/submissions/:submissionId`，不改成最新批次；自动化增加 `/settings/automation?run=<id>` 作为运行入口，另保留历史项目入口，不混为 Agent Run。

- 来源卡在构造路径前验证事件类别、参与路由的 canonical ID、来源 ID/事件键与快照的一致性。已标记删除的来源无活动导航；路由身份损坏或不支持的来源不猜目标。现有 API/导入验证继续独立负责完整数据契约。
- 各入口仅携带父页面提供的合法 `return_session`，不从来源 payload 或任意查询串继承会话/授权。提醒只保留单值合法的 Inbox `risk/view`，固定打开对应 fired occurrence，清除旧提醒身份、重复参数和其它导航参数。
- 提交批次→任务处理、自动化结果→Task/Inbox/Reminder 的相邻入口同样保留合法返回身份；自动化结果 ID 非法时不导航，不因此切换对话、交接内容或执行结果操作。
- 链接是历史快照定位线索，不表示当前外键已核验。目标页重新读取；批次页验证 Task↔Submission，不以此额外宣称入口 Artifact 当前归属已证明。模型需要当前来源证明时仍须显式授权调用 `workspace_inbox_source`。失效/错属读取保留失败状态，不替换成其它对象；不自动发送、审批、验收或获取文件。
- Inbox 自身编辑及解决/忽略/稍后表单期间禁用来源导航与返回；在途写入或关系操作也禁用，禁用入口不保留可绕过的 href。在途冻结本表单字段并拒绝重复提交，关闭未完成表单需明确舍弃，在途不能关闭；成功提交沿用既有成功回调。关系区未提交的选择/拆分草稿不由此父级门禁完整覆盖。
- 内容详情与提醒管理器的返回/关闭草稿保护见各自模块文档；这不是全站路由阻塞、刷新保护或全局草稿持久化。系统维护仍只打开原位置上的数据设置，不构造业务记录或自动恢复。

代码与确定性证据：`InboxSourceContext.tsx/.test.tsx`、`InboxItemDetailModal.tsx/.test.tsx`，以及既有精确批次、路线图、自动化定位页测试。API v1/schema 076、权限、业务事实与投影均未改变。

## 定位与边界

H5-F3（2026-09-21）在 Agent 失败来源卡保留原安全错误码并显示固定中文原因，兼容截断、过滤/拒绝和响应不合规三个新码。解释只基于快照安全码，不证明当前 Run 归属、不读取输出、不触发模型或通知重试；见 [执行错误契约](local-agents.md#执行完成信号与安全失败原因h5-f32026-09-21)。

H3-C3 新增 `agent_run_failed` 来源，由人工启用的失败预设唯一生成。固定 `agent-run:<原RunID>:failed` 去重，通用标题/说明与严格八字段安全身份快照不携带执行正文；重放不覆盖或重开已处理事项。筛选和来源卡识别此类型，精确定位 Task/Run、保留合法原对话返回身份；目标重新读取，快照链接不等于当前关系证明。模型查询另须 `work+outputs`，原 Run、通知 Automation Run 与 Task 的成功/完成含义各自独立。

活动诊断计入 Task 删除影响并阻止删除；终态诊断先写 source_deleted_at/审计。便携导入保留历史证明但排除 AgentRun，缺少 live Run 为 unavailable，不伪造删除或重新执行。无自动模型诊断，不向执行续办队列添加同一 Run 的副本。API v1/schema 076 不变；完整字段、导入/恢复和权限契约见 [Agent 失败诊断](automation.md#agent-失败诊断闭环h3-c32026-09-21)。

H4-I：Inbox 详情新增“交给智能体”，使用 `inbox_item` 类型和真实 ID 进入待办卡，不复制来源载荷、不自动已读或分诊。编辑表单、解决/忽略/稍后草稿或在途操作期间禁用入口；对话内仍需准备、授权、发送及逐项审批。详见 [AI 助手交接契约](ai-assistant.md#h4-i-从工作台事项进入智能体)。

收件箱是本地工作受理与编排中心。Inbox Item 说明“为什么需要处理”以及当前分诊展示状态；真正可执行的工作仍由 Task 承担，责任变化由 Assignment 承担，追加式操作历史由 Workflow Event 承担。

对象职责必须分离：

| 对象           | 保存的事实                             | 不保存的事实             |
| -------------- | -------------------------------------- | ------------------------ |
| Inbox Item     | 来源、分诊、已读、稍后、解决策略       | 任务执行状态、当前负责人 |
| Task           | 工作内容、生命周期、完成条件、验收结果 | 收件箱展示状态           |
| Assignment     | 当前责任人和改派历史                   | 任务完成状态             |
| Agent Run      | 一次本地执行尝试                       | 任务是否验收通过         |
| Task Artifact  | 文本、文件、链接或结构化产出           | 任务是否完成             |
| Workflow Event | 追加式操作时间线                       | 当前业务状态的第二副本   |

当前版本继续遵守以下边界：

- Inbox Item 不能直接分派。后续“分派收件箱项”必须先在一个事务中创建或关联 Task，再为 Task 创建 Assignment。
- `read_at`、`snoozed_until` 与 Inbox Item 主状态彼此独立；已读或稍后都不等于任务完成。
- 收件箱不复制 Task 进度或负责人事实。
- 项目、客户、发票继续维护各自状态；当前手工 Inbox Item 不会自动改写这些对象。
- 已启用的“项目完成后提醒检查开票”预设只创建本地 Inbox Item，不生成或发送发票，也不改变 Project/Client/财务事实。
- 第一阶段不提供多人登录、云同步、远程通知、邮件/消息自动发送、线上 Agent 或模型服务。

## 当前实现状态

- H2-Y 将 Project 产出的 follow-up Inbox ID/version 与实时 required 进度接入 `workspace_project_outputs`（需 `work+outputs`），不再依赖标题搜索猜关联。读侧复用原生 `loadProjectArtifactFollowups` 与进度统计；后续拆分/关联/必需标记仍须独立 `actions`、当前 Inbox/Task 版本及人工确认。Project version 不覆盖 Inbox 变化，不新建来源或复制状态，详见 [项目产出与跟进](projects.md#ai-项目产出与跟进闭环h2-y2026-09-21)。

- H4-H 补齐精确事项与提醒的来源对话返回：AI 消息/审批卡打开 `/inbox/:id` 或 `/inbox?reminder=:id` 时由 UI 附加返回身份，详情弹窗内可直接返回，读取失败也保留入口；写入中禁用返回。关闭后身份保留在列表 URL，打开/返回不会自动已读、分诊、发送消息或继承权限。

当前模块为**部分完成**：手工受理分诊、已有 Task 关系、一次性及 daily/weekly/weekdays/monthly Reminder、批量拆分分派、自动结清/重开和例外强制解决、T-11F 运营计数及已登记来源投影均已接真实 SQLite/API/UI。拆分面板使用共享 ProjectSelect，并从可信来源快照默认带入 Project，仍允许逐项清除/改选；独立完成条件、person 本地责任提示和共享 Task 详情已接通。Project Artifact 读模型显示 nullable follow-up/实时 required 进度，成功 Inbox mutation 通过 Project/Task/Today Query 失效刷新相关表面。物理卷同卷去重、无路径手动容量检查和 30 天容量趋势已交付；v0.1 人工闭环不调用 Agent/AI；独立轨道已接入下述受确认的受理分诊操作，H2-C2 已接入已有 Task 关联及受整批确认的原子拆分/初始分派。

### AI 来源定位（H2-AA，2026-09-21）

H2-AB 补齐客户回访方向：到期扫描按真实纳秒时刻投影；从本详情“查看客户回访”继续到客户记录时，保留合法的原对话返回身份，不自动已读、分诊或授权。智能体来源查询仍须 `work+clients`，完成/续排沿用独立人工确认，见 [回访闭环](client-followups.md#到期提醒与智能体处理闭环h2-ab2026-09-21)。

`workspace_inbox_source({inbox_item_id})` 在单次 `work` 授权下，通过 `inbox` 指南按需发现。它用真实 canonical Inbox UUID 查询当前来源身份，不按标题猜关联；原有 `workspace_search/get(inbox_item)` 的字段边界保持。参数只能有一个 `inbox_item_id`，额外、重复、别名、null 或非 canonical UUID 均拒绝。API v1/schema 076 不变，没有新 scope、HTTP 路由或迁移。

| 已登记来源                                       | 当前 subject / related                          | 额外范围  |
| ------------------------------------------------ | ----------------------------------------------- | --------- |
| Task 阻塞、到期                                  | Task                                            | 无        |
| Project 完成、内容审核/发布到期、路线图到期/达成 | Project、Content Item 或 Roadmap Milestone      | 无        |
| Reminder occurrence                              | 实际 fired Reminder，不是自动化规则             | 无        |
| Client Followup                                  | Followup / 实际 Client                          | `clients` |
| Invoice 到期                                     | Invoice                                         | `finance` |
| Task Artifact 跟进                               | Artifact / 实际 Task 与 Submission              | `outputs` |
| 内置项目完成自动化                               | 成功 Automation Run / 实际 Rule，不是 Agent Run | 无        |

返回同一只读事务的 Inbox ID/version/status、`as_of`、白名单 `source_type`、`lookup_status`、`required_scopes`、nullable `subject`、`related` 和快照比较。实体只有 type/ID/当前 version/status/代码生成 route；Artifact/Submission/Automation Run 没有自身业务版本时为 null，Artifact 与 Rule 没有 status 时为 null，不能拿 Inbox/Task/Rule 版本冒充。Artifact 路由定位所属真实提交批次，不声称自动聚焦其中某个文件。

- `available` 仅表示当前身份可核验，不证明业务完成。`source_version` 是不可变来源事件版本；可比较时 `snapshot_matches_current` 按当前对象版本核对，Task due 则比较真实截止时刻。false 表示历史证据已不同；null 表示没有相应比较证据，不等于匹配；true 也不代表 payload 所有字段逐项一致。历史事项仍可返回当前对象，不因历史版本不同自动丢失。
- `none` 表示合法 manual 事项无来源；`deleted` 表示已标记删除或 Artifact 软删除；`unavailable` 表示目标/必要关联缺失或身份证据不一致；`unsupported` 表示系统维护或其它非白名单来源，不回显任意自定义来源字符串。这些结果均不提供可操作来源身份或路由。Inbox 本身不存在、数据库故障和非法参数沿用脱敏错误，不能假称查询成功。
- 额外范围检查先于来源存在性、删除和身份查询。缺少 `outputs/clients/finance` 返回 `permission_required` 和缺少的范围，不读取或输出保护对象身份、版本、路由或快照；用户必须在新消息重新授权发送，不能默认全选或绕过查询。
- 来源 key/ID 与逐字段读取的不可变证明必须一致，且映射只使用代码白名单表名。Reminder 要核验 fired 和反向 Inbox 身份；Artifact 必须核验实际 Task/Submission 及 Submission owner；自动化核验成功 Run、实际 Rule/预设、逻辑键与反向结果；发票日期分类必须与事件一致。不返回原始 payload、来源 key、标题/摘要、正文、金额、文件或配置，也不推导没有外键保证的 Agent Run。
- 现有“交给智能体”提示先读 Inbox，再查来源，必要时读取对应领域指南与当前详情；只暂存原安全交接字段，推荐权限仍是 `work+actions`。执行过程显示“核对收件箱来源”。查到 route 不会自动打开，所有后续命令仍逐项审批；已读/解决 Inbox 不等于完成 Task、发布内容、验收产出或收到款项。

代码证据：[来源工具](../../services/sidecar/internal/api/ai_inbox_source.go)、[来源合约测试](../../services/sidecar/internal/api/ai_inbox_source_test.go)。内容到期扫描的纳秒边界及历史提醒后续改期见 [内容模块](content-calendar.md#到期扫描与-ai-来源闭环h2-aa2026-09-21)。真实 Provider 和原生桌面效果另行验收。

### AI 独立轨道：受确认的受理分诊（ADR-029 H2-C1）

- 已保存 AI 会话显式授权 work+actions 后可提出 inbox.create/update/read/snooze/unsnooze/resolve/dismiss/reopen，用户逐项确认才执行。只创建 manual 事项，不伪造 AI/提醒/系统来源；既有事项要求真实 ID 与版本。
- H4-U：Inbox 详情提供专用“交给智能体”，只暂存 `/inbox/<id>`、canonical Inbox Item ID、当前显示标题/状态和固定提示词；不复制摘要、来源 payload、活动正文、任务列表、会话或授权。编辑草稿、解决/忽略/稍后命令草稿、任务关系在途写入和其它在途操作会禁用交接；进入 `/ai` 后仍需用户显式带入问题、重新选 `work+actions` 并人工发送。模型必须先用 `workspace_get(type=inbox_item,id=...)` 读取最新事实，才可起草待确认 `inbox.*` 建议；交接本身不会编辑、已读、稍后、解决、忽略、重新打开、拆分任务或强制解决，详见 [收件箱事项交给智能体](ai-assistant.md#收件箱事项交给智能体h4-u)。
- 创建/编辑仅开放标题、说明、优先级和截止日期；稍后要求带时区的未来时间，解决/忽略要求原因。提议及确认复用人工领域 prepare/command，保留归档、来源只读、自动解决与重开规则。解决或忽略事项不改变 Task 或 fired Reminder，snooze 不新建 Reminder。
- 审批沿用 schema 072，不新增 Inbox 表/字段。领域写入、工作流事件、审批决定同事务，摘要与版本绑定，可重放而不重复执行；确认时旧版本或已过期稍后时间会失败，不能自动改参数。详情路由为 /inbox/:id，后续对话读取无正文审批回执。
- `workspace_get(inbox_item)` 查询当前阅读/稍后/截止、策略、来源类型、服务端时间与实时 required 进度；说明有界，不发送 payload/来源 ID/操作时间线正文。H2-AA 的独立来源工具仅在上述范围与身份核验后提供来源元数据，不扩大此详情响应。前端确认后失效 Inbox 计数/列表/详情/时间线及搜索，并取消、刷新 Project follow-up 读取，避免旧响应回填。
- 已有 Task 关联见下一节；Reminder 查询与受确认操作已由 H2-C3 接通，强制解决见 H2-C4，快照式全部已读见 H2-C5；原有人工作业接口继续可用。本段不扩大 v0.1 范围，详细权限与审批见 [AI 助手](ai-assistant.md) 和 [ADR-029](../adr/029-agent-workspace-capabilities.md)。

### AI 独立轨道：已有 Task 关系（ADR-029 H2-C2）

- work 范围新增 workspace_inbox_tasks 当前/历史关系分页（默认 10，最多 20，offset 上限 1000），返回真实 Task ID/版本/状态、必需标记、Inbox 版本及实时进度；不外发 Actor、解除原因、正文或来源 payload。删除历史只返回快照、不伪造可打开链接。
- work+actions 可提议 inbox.link_task、inbox.set_required、inbox.unlink_task；同时绑定 Inbox 与 Task 当前版本，必需标记显式布尔，解除需要原因。卡片显示关联、数量、策略与可能自动解决的前后状态；确认时其他任务进度改变会返回 AI_ACTION_PREVIEW_CHANGED，即使 Inbox 版本未变也不能执行旧效果。
- 人工和 AI 复用 inbox_task_commands.go 事务入口，所有业务写入、软解除历史、领域 reconciliation 与审批同事务；保留活动关系上限 100、重复关联/归档保护、Task 版本不变、零必需不结清和原有审计。确认/回读/重放/缓存失效沿用同一审批链，不需要新迁移。
- 原子拆分/初始分派已接续：inbox.split 支持原有 1–20 项完整草稿与层级、项目/标签/排期/完成条件、required 和验收策略；模型须先查真实 Actor/Tag 候选，人工核对整批预览后一次确认。person 仅记录本地责任，已就绪 agent 的分派不会启动执行，manual reviewer 固定 owner。
- 人工 API 与审批共用 inbox_split_commands.go，事务包含整批 Task、Assignment、关系、Inbox、领域事件与审批决定；任一失败整批回滚。确认复核 Inbox 版本、Actor/Project/Tag 资料及当前 required 进度，变化不能静默执行。成功刷新 Task/Today/Project 与 Inbox；回执仍为 Inbox 结果。原有 HTTP 幂等重放保留，关系空洞追加使用最大活动 position 而非数量，避免误报顺序冲突。
- Reminder 已由 H2-C3 接通，强制解决见下一节；人工入口不受影响。

### AI 独立轨道：例外强制解决（ADR-029 H2-C4）

- `inbox.force_resolve` 已通过 work+actions 及通用审批接入；不是普通 resolve 的自动降级。模型只能提出真实事项版本与原因，不能传人工同意。
- 卡片展示完整进度计数、阻塞/待验收/取消数量与 forced 结果，用户独立勾选后才允许确认。服务端只允许 all_required_tasks_done 活动事项，重验当前 Inbox 版本与预览进度；Task 进度变化即使没有递增 Inbox 版本，也拒绝旧卡。
- 人工 `/inbox-items/:id/force-resolve` 与审批共用事务命令，保持 If-Match、HTTP 幂等快照、owner/forced 审计语义；审批原子记录决定并支持幂等回读。Task、Assignment、required 关系、验收和来源均不改变；失败不留下部分事实。
- 确认后刷新 Inbox/Project follow-up/搜索，并将实际决定带回本会话后续对话；具体人工字段、错误码和测试入口见 [AI 助手 H2-C4](ai-assistant.md#收件箱强制解决h2-c4)。Reminder 的 AI 入口见 [H2-C3](ai-assistant.md#本地提醒h2-c3)，快照式全部已读见下一节。

### AI 独立轨道：快照式全部已读（ADR-029 H2-C5）

- `work` 授权下的 `workspace_search({type:"inbox_item"})` 除有界列表外返回同一服务端读快照的 `snapshot_at`、全局 `snapshot_unread_total` 与 `read_all_max_items=1000`。全局未读数和后续批量范围不受当前 query、status、页长或 offset 缩小，模型不得使用客户端时间、某页数量或自行猜测 cutoff。
- 保存会话在 `work+actions` 下可提出 `inbox.read_all`；`changes` 只能包含从上述工具原样取得的 `through_created_at`，且不能携带 `inbox_item_id` 或 `expected_version`。候选仍使用人工全部已读的可见范围与 `created_at/updated_at <= cutoff` 规则；空集合拒绝，超过 1000 条时要求用户转到 Inbox 页面人工处理。
- 提议预览保存 cutoff、候选数量及按稳定顺序编码的候选 `ID+version` SHA-256 指纹，不保存事项标题、摘要或来源 payload。人工卡展示将标记的真实数量和截止时点；用户逐项点击确认前不修改任何事项。
- 确认事务先重建完整预览；候选成员、版本、数量或指纹变化均返回 `409 AI_ACTION_PREVIEW_CHANGED`，不能把新计算出的范围当成原批准范围。通过后复用原生 `readAllInboxItemsInTransaction`，批量写入每条 `read_at` 与 `read` Event，并与审批决定同事务提交；快照后新到或被编辑、分诊、重开的事项沿用原生命令的保守排除规则。
- 此动作只改变已读状态，不改变 `status/triaged_at/snoozed_until/resolution_policy`，不解决、忽略、关联或拆分 Task，也不修改 Reminder 或来源。它没有单一业务实体；`result_id` 使用 Proposal 作为耐久回执身份，`result_version=1`。专用 `inbox_read_all_result` 和后续无正文回执返回实际 `through_created_at/marked_count`，路由固定为 `/inbox`，不伪装成任一 Inbox Item 详情。
- 主/侧确认卡共用严格解析与状态回读，确认或模糊失败后按既有顺序取消并刷新 Inbox 列表、计数、搜索及相关工作台缓存。API v1、schema 73 和原生 `/inbox-items/read-all` 合约不变；没有增加模型执行工具、跨轮权限或自主批量分诊。

### 已交付：T-11A1 手工 Inbox Item 事实

- schema v12 通过 `012_inbox_items.sql` 新增 `inbox_items`，从 v11 升级时只追加表和索引，不改写已有业务事实。
- 当前公开创建 API 强制 `kind = manual`、`source_entity_type = manual`、`resolution_policy = manual`；不接受 `source_entity_id` 或 `source_event_key`。
- 迁移为来源投影预留 `event / reminder` kind、`all_required_tasks_done` 策略及 nullable `source_event_key`，并用仅覆盖非空值的部分唯一索引去重。公开 Inbox 创建 API 仍不能使用这些值；schema v14 的内部 Reminder 调度器已经使用 `kind=reminder` 和稳定事件键。
- 主状态约束为 `open / tracking / resolved / dismissed`。当前手工创建固定进入 `open`；schema v13 的显式 Task 关系命令已经使用 `tracking`，受理/分诊命令本身不会隐式创建关系或进入该状态。
- 标题为 trim 后 2–200 字符，摘要最多 10,000 字符，优先级为 `P0 / P1 / P2 / P3`，截止时间使用 RFC 3339 UTC。
- `payload_json` 必须是 JSON object；当前前端不暴露该字段，也不会以此伪造来源事实。
- 解决和忽略终态由数据库约束成组校验：责任 Actor、时间、原因和模式不能出现半套事实。

### 已交付：T-11B 人工受理与分诊

- `/inbox` 已接真实列表、全局待处理未读数、搜索、优先级筛选、分页，以及“待处理 / 稍后 / 已归档”三个视图。
- 支持新建手工条目、查看详情、编辑标题/摘要/优先级/截止时间、单条已读和快照式全部已读；列表、创建、Reminder 和统一搜索入口均使用 `/inbox/:inboxItemId`，刷新可恢复精确详情。
- 支持设置稍后时间、提前恢复、到期自动回到待处理视图；稍后只改变可见性，不复制条目。列表每 15 秒低频刷新，以服务端时钟重新判定到期可见性。
- 支持带原因解决、带原因忽略和重新打开。解决/忽略不会隐式标为已读；未读终态仍可直接标为已读，无需先重开。重新打开保留 `read_at` 与 `triaged_at`，清除终态事实和 `snoozed_until`，并按是否存在活动 Task 关系进入 `tracking / open`。
- 详情提供追加式活动时间线，覆盖 `created / updated / read / snoozed / unsnoozed / resolved / dismissed / reopened`。
- 列表、详情和时间线均有加载、空、错误与重试状态；表单和命令有校验、禁用态及并发冲突提示，编辑草稿在冲突刷新后保留。

### 已交付：T-11A2 已有 Task 关系

- schema v13 通过 `013_inbox_item_tasks.sql` 新增活动/历史关系、required、稳定 position、软解除事实、原 Task ID/标题快照，以及活动关系阻止 Task 硬删除的数据库保护；v12→v13 是加法迁移，不改写既有业务事实或创建 demo 关系。
- 详情可查询全部活动关系和分页历史；服务端实时 JOIN 当前 Task，返回 Task 状态/版本以及 `active_total / required_total / required_done / required_remaining / required_blocked / required_waiting_review / required_cancelled / percent / all_required_done`。零个 required 时 `percent = null`、`all_required_done = false`，不会触发空集合完成。Task 状态与百分比不复制到 Inbox 表。
- 支持把已有 Task 关联到活动 Inbox Item、修改活动关系的 `is_required`，以及填写原因软解除。重新关联同一 Task 创建新关系行，不恢复或覆盖旧历史。
- 活动关系行可通过 stack-aware Modal 打开全局共享 Task 详情；历史关系只要实时 Task 仍存在也提供同一入口，关闭 Task 详情后返回原 Inbox 上下文；Task 已删除时仅显示不可变 ID/标题快照。
- `is_required` 必须由 Inbox 关系创建/修改或 T-11C 拆分草稿显式填写。Task 的父子层级、父任务自动待验收和系统 `child_rollup` Submission 都不会创建关系、继承 required 或改写现有标记。
- 第一条活动关系使 `open → tracking`；解除最后一条活动关系使 `tracking → open`；归档项重新打开时有活动关系进入 `tracking`，否则进入 `open`。关系命令不自动已读、不清除稍后、不改变 Task 生命周期，也不创建 Assignment。
- 关系 POST/PATCH/DELETE 使用 Inbox `ETag / If-Match` 和可选 `Idempotency-Key`，业务事实、Inbox 状态/version 与 `task_linked / task_requirement_changed / task_unlinked` 事件同事务提交。关系命令不递增 Task version。
- 任一活动 Inbox 关系会使 Task 硬删除返回 `409 TASK_HAS_ACTIVE_INBOX_RELATIONS`。带原因软解除后 Task 可以删除；历史行的实时 `task_id` 置空，但 `task_ref_id / task_title_snapshot` 与事件继续保留。

### 已交付：T-11A3 一次性与 daily/weekly/weekdays/monthly 本地 Reminder

- schema v14 新增独立 `reminders` 调度事实；公开 API 提供创建、分页/搜索/状态查询、详情、scheduled 编辑和带原因取消。
- Sidecar ready 前先补扫到期项，运行中每 15 秒扫描最多 100 条；以 `reminder:<id>:due` 为稳定事件键在同一事务中生成或复用一个 Reminder Inbox Item、记录 system 事件并把 Reminder 标记为 fired。
- Inbox Item 使用 `kind/source_entity_type=reminder`、指向 Reminder ID、继承标题/摘要/优先级/触发时间，并保持 `resolution_policy=manual`。用户随后在普通 Inbox 流程中阅读、稍后、解决、忽略或关联 Task，不反向修改 fired Reminder。
- schema v32 为每条 Reminder 增加系列、daily/weekly、IANA 时区和 occurrence 序号；schema v40 增加 monthly 与当地日锚点。到期事务在 fired/Inbox 事实之外创建唯一下一 occurrence；跨 DST 保持当地钟点，按月短月落到月末且后续恢复锚点，离线跨过多个周期只补当前一条，重复扫描/重启不重复；取消当前 scheduled occurrence 停止系列。
- Reminder 管理器从 Inbox 页打开，提供待提醒/已触发/已取消模块、搜索、分页、新建、编辑/改期、每日/每周/工作日/每月重复规则、带原因取消和触发后跳转 Inbox。法定节假日/自定义规则、系统原生通知和其他业务来源投影仍未交付。

### 已交付：T-11C 拆分、分派与自动结清

- schema v15 的 `015_inbox_task_orchestration.sql` 为自动完成规则增加查询索引和数据库保护：自动解决必须至少有一个活动必需 Task，且全部处于 `done`；不改写既有业务数据或创建 demo 记录。
- `POST /api/v1/inbox-items/:id/split` 在一个 SQLite 事务内创建 1–20 个 Task、父子关系、标签、`created` Inbox 关系、初始 owner/person Assignment、manual review 的 owner reviewer，以及 Task/Inbox 审计事件。任一字段、引用或写入失败时全部回滚。
- 拆分面板支持任务名称、说明、类型、优先级、项目、完成条件、父任务、必需标记、负责人和验收策略；父任务只能引用本批次中更早的任务，避免环和悬空引用。
- 对 `task_artifact / task / task_due / project_completion` 四类可信本地来源，只有 payload 中 canonical `project_id` 会成为默认 Project；首项和后续新增草稿继承它，但每项可显式清除或改选。来源快照是便利默认值，不是强制关联。
- 完成条件由独立输入写入 Task `completion_criteria`，不再用说明字段冒充。负责人只列 active owner/person；person 明确标为“仅本地责任记录”，不会登录、接收同步或直接操作应用。
- 每个拆分 Task 的项目字段复用共享 `ProjectSelect`：打开单个选择器时每页读取 20 条，输入经 250 ms 防抖后走 Project 服务端搜索，`q / page / includeArchived` 隔离 Query key，旧请求可取消，候选按 ID 去重。默认不列归档项目；当前选中项通过详情或名称 fallback 跨页/失败保留，只有显式清除才把该草稿的 `project_id` 设为 null。加载、空、错误重试、更多结果与 combobox 键盘语义由同一组件提供。
- `all_required_tasks_done` 策略由统一 reconciliation 维护：至少一个活动必需 Task 且全部 `done` 时由 system Actor 自动解决；自动解决后，任一必需 Task 因重开、返工等离开 `done` 会自动恢复为 `tracking`。
- Task 生命周期命令、产出提交/验收，以及 Inbox 关系的新增、required 修改、解除和拆分都会调用同一 reconciliation；进度仍实时来自 Task，不复制第二份状态。
- schema v30 父任务协调仍会在父 Task 状态变化后调用这条既有 reconciliation，但只影响已显式关联且 active/required 的 Task：父子 Task 关系本身不等于 Inbox 关系，父任务的直属子任务也不会被 Inbox 自动纳入 required 集合。
- 普通 `resolve` 不能绕过自动策略的未完成必需任务。危险操作 `force-resolve` 只用于自动策略，要求显式 `confirm=true` 和原因，并以 owner Actor、`forced` mode 与 `force_resolved` 事件留下不可变审计；手工/强制解决不会因后续 Task 变化自动重开。
- split 写命令失败时保留草稿，并使用 Inbox `If-Match` 与稳定幂等键；split API 成功响应后前端立即关闭 Modal，即使随后的后台刷新失败也不保留可重放草稿。所有会改变 follow-up 的成功 Inbox 编辑、命令、关系/required mutation、split 与强制解决会先取消可信来源 Project 的在途查询再失效缓存；Artifact 请求消费 `AbortSignal`。split 额外统一失效 Task、Today 和 Project 查询。缓存刷新不传播或复制跨聚合 version。

### 已交付：T-11F 运营计数与风险深链

- `GET /api/v1/stats/inbox` 在查询时从当前可见的 `open / tracking` 条目与活动必需 Task 实时派生 `pending / unread / tracking / blocked / waiting_review`；返回同一服务端 `server_now`。
- `snoozed_until > server_now` 的未来稍后项、resolved/dismissed 归档项、已解除关系、可选 Task 和已删除 Task 不进入相应当前风险计数；同一 Inbox 有多个同状态 Task 只计一次。
- 列表新增 `risk=tracking|blocked|waiting_review` 服务端筛选，blocked/waiting_review 使用活动必需关系和实时 Task 状态；全局 `unread_total` 继续不受 risk/search/priority 缩小。
- Sidebar 显示待处理徽标，Today 显示待处理/跟进中/待验收/有阻塞并跳转风险深链；统计 Query 位于 Inbox Query 前缀下，写入后统一失效并每 15 秒刷新。
- `source_entity_type` 查询过滤允许 `client_followup/content_item/roadmap_milestone/invoice_due/automation/agent_run_failed`；Today 使用前者准确读取到期回访。回访前端严格验证 `followup:<id>:due:<version>` 与 `client_followup_id / client_id / scheduled_at / timezone / channel` 快照，通过 `/clients/:clientId?followup=<id>` 精确定位，不能在 Inbox 伪造回访完成。
- `content_item` 来源严格校验内容 ID、事件类型、版本、计划时间和时区；来源存在时链接到 `/content-calendar?item=<content-item-id>` 并由内容日历单条 API 读取最新详情，不依赖当前月格。来源删除后只展示不可变排期快照并隐藏失效链接。

### 已交付：T-11E 第一项——Task Artifact follow-up 来源

- schema v23 通过 `023_task_artifact_inbox_projection.sql` 为 `source_entity_type=task_artifact` 增加查询索引、严格来源身份、不可变快照和删除协调 guards，不回填旧 Artifact、不创建 demo Inbox Item。
- `submit-output` 事务只消费本批次中显式 `requires_followup=true` 的 Artifact；每个 Artifact 以 `task-artifact:<artifact-id>:followup` 创建一个 `kind=event` Inbox Item，并由 system Actor 追加 `source_projected` 事件。未标记 Artifact 不创建，整个提交/投影/事件/幂等快照任一步失败时全部回滚。
- 来源 `payload_json` 只保存解释和导航所需的不可变快照：Artifact ID/名称/类型、Task ID/标题、Submission ID/序号及可选 Project ID/名称，不复制正文、文件、Task 状态或负责人。
- 列表把 event 显示为“任务产出跟进”；详情展示来源任务、批次、产出类型和项目快照，可按历史快照直达精确 Task Submission 批次，目标页重新读取并验证 Task/Submission 归属，不自动替换为最新批次。正文、下载、审核、返工和删除仍由 Task 领域负责。
- 活动 `open/tracking` 来源项会分别以 `ARTIFACT_HAS_ACTIVE_INBOX_SOURCE` 或 `TASK_HAS_ACTIVE_INBOX_SOURCES` 阻止 Artifact/Task 删除。用户先解决或忽略来源项后，删除事务会递增 Inbox 版本、写 `source_deleted_at` 和 `source_deleted` 事件，再删除来源；失败整体回滚。
- 来源删除后 Inbox Item 和 payload 快照继续保留，并明确显示“来源产出已删除”；重新打开仍可基于快照继续人工编排，但不会恢复来源 Artifact。
- `GET /api/v1/projects/:id/artifacts` 为该来源返回 nullable `followup`：Inbox ID/version/status/policy/`source_deleted_at` 与实时 required progress。响应 `ETag / meta.project_version` 仍只表示 Project 聚合版本；Inbox 写版本由 `followup.inbox_item_version` 独立表达，当前 Project 页面只深链 Inbox。Inbox 成功 mutation 通过来源 Project Query 失效刷新，不增加跨聚合版本 trigger。

### 已交付：T-11E 第二项——Task 阻塞来源

- schema v24 通过 `024_task_blocked_inbox_projection.sql` 约束 `source_entity_type=task` 的阻塞来源身份、按来源 Task/version 查询的索引、不可变快照和 Task 删除协调；升级不回填此前已经处于 blocked 的 Task，也不创建 demo Inbox Item。
- 每次受控 `block` 命令更新 Task、追加 `task_blocked` Event，并在同一事务以 `task:<task-id>:blocked:<block-version>` 创建一个 `kind=event` Inbox Item 和 system `source_projected` Event；命令幂等重放不重复创建，unblock 后再次 block 使用新版本生成新的独立阻塞事项。
- payload 只快照 Task ID/标题、阻塞原因/时间/来源状态、本次 block version 及可选 Project ID/名称；不复制当前 Task 状态、Assignment 或正文。unblock 不自动解决或删除 Inbox Item，保留 owner 对本次阻塞事件的人工受理权。
- 列表以警示图标区分 Task 阻塞；详情展示阻塞时间、原因、阻塞前状态、项目和精确 Task 入口。来源仍存在时可直达；Task 删除后隐藏失效链接并显示保留快照。
- 任一 `open/tracking` Task 来源项使 Task DELETE 返回 `TASK_HAS_ACTIVE_INBOX_SOURCES`。全部来源项解决/忽略后，删除事务统一标记 `source_deleted_at`、递增 Inbox version、追加 `source_deleted` Event，再删除 Task；多个阻塞批次及 Artifact 来源任一步冲突都会整体回滚。

### 已交付：T-11E 第三项——Task 临期来源

- schema v25 通过 `025_task_due_inbox_projection.sql` 新增 `source_entity_type=task_due` 的来源身份、按 Task/截止时间查询的索引、不可变快照和 Task 删除协调；升级不回填旧 Task、不创建 Inbox Item 或 demo 数据。
- Sidecar ready 前补扫，运行中复用 15 秒本地扫描周期。状态非 done/cancelled 且 `due_date` 已进入未来 24 小时窗口的 Task 按截止时间/ID 稳定排序，每批最多处理 100 条；已投影来源从后续批次排除，因此积压会继续推进而不会卡在首批。
- 每个 Task/截止时点使用 `task:<task-id>:due:<due-at>` 稳定键创建一个 `kind=event` Inbox Item 和 system `source_projected` Event。重复扫描/重启不重复；改期到新的截止时点会生成新的独立事项，改回相同语义时点仍复用原来源。
- payload 只快照 Task ID/标题、截止时间、投影时间、投影时的 `due_soon/overdue` 分类、固定 1440 分钟提前量和可选 Project ID/名称；不复制当前状态或责任。列表使用临期图标，详情展示截止/进入收件箱时间、项目和 Task 入口。
- 完成、取消或改期不替 owner 自动解决已经生成的来源项。活动来源阻止 Task 删除；全部来源归档后，Task 删除事务写 `source_deleted_at`、递增 Inbox version、追加 `source_deleted` Event 并保留快照。

### 已交付：T-11E 第四项——备份创建失败的系统维护来源

- schema v26 通过 `026_system_maintenance_inbox_projection.sql` 约束 `source_entity_type=system_maintenance` 的来源身份：`kind=event`、`source_entity_id=component:operation`、`source_event_key` 匹配 `system:<source-id>:<incident-id>`、投影初始优先级 P0/P1、无截止时间、payload 只含 `component / operation / failure_code / occurred_at / message`，并禁止写入 `source_deleted_at`。owner 仍可把条目重新分级为 P0–P3；升级保留既有 Inbox 事实，不回填或创建 demo incident。
- `POST /api/v1/backups` 失败仍向调用端返回 `BACKUP_CREATE_FAILED`；现有数据保持不变。Sidecar 随后尽力投影一条 Inbox Item。投影失败只记内部维护日志，不改变备份错误响应，也不把底层 Go error 暴露给收件箱。
- 当前只支持 `component=backup`、`operation=create`、`failure_code=backup_create_failed`。标题固定为“本地备份需要处理”，message 固定说明无法创建已验证备份且现有数据未被修改。payload 不保存 Go error、本机路径、备份 note、Token、request ID 或请求正文。
- 同一 `backup:create` 在 `open/tracking` 且未标记来源删除时只允许一个活动 incident；重复失败复用既有条目。resolve/dismiss 后再次失败使用新的 incident ID 和 `source_event_key` 开新条目。system Actor 追加 `source_projected` Event。
- 列表以硬盘图标区分系统维护项；详情标注“系统维护”，展示组件/操作/发生时间和固定说明，并提供“打开数据与备份”。前端 strict normalizer 拒绝额外 payload 字段、错误原文和未知 event key。
- 系统维护来源没有可删除的业务实体，因此不实现 `source_deleted_at` 协调。PATCH 不能给这类条目设置截止时间。

### 已交付：T-11E 第五项——备份校验失败的系统维护来源

- 复用 schema v26 的 `component:operation` 身份，不新增迁移。`POST /api/v1/backups/:id/verify` 在返回 `BACKUP_VERIFY_FAILED` 后尽力投影 `source_entity_id=backup:verify`。
- 标题固定为“本地备份校验需要处理”；payload 只含 `component=backup`、`operation=verify`、`failure_code=backup_verify_failed`、`occurred_at` 和固定说明。不保存 Go error、本机路径、备份 ID、note、Token 或请求正文。
- 同一 `backup:verify` 活动 incident 去重；resolve/dismiss 后再失败开新条目。`BACKUP_INVALID`（包损坏/篡改/额外文件）表示校验已完成，不投影 Inbox。

### 已交付：T-11E 第六项——备份恢复演练与恢复安排失败

- `POST /api/v1/backups/:id/drill` 的操作性启动失败或通过 manifest 校验后的隔离演练失败，尽力投影 `source_entity_id=backup:drill`；包不存在、请求 ID 非法或 `BACKUP_INVALID` 不投影。
- `POST /api/v1/backups/:id/restore` 在读取 pending 状态、检查源目录、读取工作区身份、创建恢复前回滚点或发布 pending 计划的操作性失败时，尽力投影 `source_entity_id=backup:restore`。确认缺失、包不存在/无效、工作区不匹配和已有恢复计划属于可解释业务结果，不投影。
- 恢复安排前的隔离演练失败复用 `backup:drill` 身份，不重复发明 restore incident。两类 payload 继续只保留 `component / operation / failure_code / occurred_at / message`，不含备份 ID、本机路径、底层 error、Token、请求正文或备注。
- 每个 source id 同时最多一个 `open/tracking` incident；原 API 错误码与 HTTP 状态保持不变，Inbox 投影失败只写内部日志。
- 列表/详情与创建失败共用系统维护图标和“打开数据与备份”；来源上下文分别显示恢复演练与恢复安排。

### 已交付：T-11E 第七项——数据库与 Sidecar 启动失败补偿

- 数据库尚未可写时不能直接创建 Inbox Item。Sidecar 因数据库打开、受保护迁移、迁移前回滚点、启动恢复、Router/Artifact 初始化、监听或 ready 输出失败退出前，只向 `OPC_LOG_DIR/startup-incidents-v1.json` 写入白名单 kind、稳定 UUID 和 UTC 时间；不写底层 error、本机路径、令牌、请求正文或业务数据。
- 白名单分别映射为 `database:startup`、`database:migration` 和 `sidecar:startup`。同一种 kind 在 journal 未消费前只保留最早一条，文件最多 16 条、64 KiB，使用同目录临时文件和原子替换；非普通文件、未知字段、非法 UUID/时间/类型、重复记录或超限文件会隔离为 `.startup-incidents-invalid-<uuid>.json`，不会作为事件读取。
- 下一次成功打开并迁移数据库后、Router ready 前补偿投影。payload 仍只有 `component / operation / failure_code / occurred_at / message`；`occurred_at` 使用原始失败时间。全部投影成功后删除 journal；投影或删除失败会保留/重现日志供后续重试。
- journal 中的稳定 incident ID 同时进入 `source_event_key`。即使数据库提交成功而 journal 删除结果不确定，重放也先按 event key 查询，用户已经 resolve/dismiss 的同一失败不会被重新创建。新的启动失败在旧 journal 已消费后获得新 ID，遵循活动 incident 去重规则。
- `OPC_LOG_DIR`/`--logs` 是独立受控诊断目录，默认使用数据库同级 `logs/`，不得与 Artifact/backup root 重叠。Sidecar 与 Tauri 壳分别写入 5 MiB/3 归档脱敏日志，设置和全局启动故障恢复页均可无路径打开目录；诊断包 v1 只导出系统维护错误码级汇总，明确不含原始日志。数据库打开前的恢复/迁移白名单进度已交付，启动前备份选择仍待开发。

### 已交付：T-11E 第八项——Project 完成节点

- schema v28 新增 `source_entity_type=project_completion`，不回填已经完成的历史 Project，也不创建 demo Inbox Item。只有用户通过 Project `complete` 命令产生的新完成事实会在原状态转换事务内投影；Project 命令、`project_completed` Workflow Event、Inbox Item 和 `source_projected` Event 共同成功或回滚。
- 稳定键为 `project:<project_id>:completed:<completion_version>`。同一完成周期不能重复；Project reopen 后再次 complete 会以新的完成后 Project version 形成独立周期和新事项。
- 不可变 payload 精确保存 `project_id / project_name / completed_at / completion_version / incomplete_task_count`。事项固定 P1、无截止时间、人工解决；未结任务数使用 complete 命令确认时的同事务快照，不随后续 Task 变化改写。
- Inbox 详情展示“项目完成”、完成时间、项目名和完成时未结任务数，并直达 `/projects/:id`。这只要求用户确认交付收尾、归档或其他人工后续，不自动创建 Task，不伪造验收、开票、收入或客户消息。
- 活动完成事项会阻止 Project 永久删除。全部来源事项先 resolve/dismiss 后，Project 删除事务写入 `source_deleted_at` 和 `source_deleted` Event，再删除 Project；历史 Inbox Item 保留快照并停止提供失效链接。

### 已交付：预设自动化的 Project 完成本地提醒

- schema v33 的 `project-completed-inbox` 预设默认禁用；用户在设置中预览并显式启用后，新发生的 Project complete 事件会追加一条标题为“检查开票”的本地 Inbox Item。
- 动作使用稳定 Rule/事件 dedupe key；同一完成版本重放不会重复创建，失败只留下 Automation Run 并按受控次数重试。自动化基础设施失败由外层 savepoint 隔离，不能回滚已成功提交的 Project 完成事实。
- 该条目只保存 Project ID/名称/完成版本等最小本地上下文，不创建发票、不确认收入、不联系客户，也不调用外部 API、AI、LLM 或 Agent。

### 已交付：T-11E 第九项——运行期数据库故障降级

- 版本化 API 通过统一数据库错误出口捕获非预期 SQLite 操作失败；`/health` 数据库 Ping、Focus Session 心跳和 Reminder/Task 到期来源扫描失败也进入同一链路。原 HTTP 状态、API 错误码及后台任务行为保持不变。
- 数据库仍可写时，Sidecar 尽力创建 `source_entity_id=database:runtime`、`failure_code=database_runtime_failed` 的 P1 系统维护事项；同一 source id 只保留一个活动 incident，用户解决/忽略后发生的新失败可创建新事项。
- 数据库不可写导致直接投影失败时，只把 `database_runtime`、稳定 UUID 和 UTC 时间写入并发安全的 `startup-incidents-v1.json`；下一次健康启动按既有严格校验和稳定 event key 补偿。Inbox/journal 均不含 SQL error、路径、Token、请求正文或业务数据。
- 该链路仍是故障后投影；主动容量监测由下一项独立负责。

### 已交付：T-11E 第十项——主动低磁盘空间监测

- Sidecar 在 ready 前及每 5 分钟检查数据库父目录、受控文件根和备份根；规范化绝对路径并去除重复路径，每轮读取 `app_settings.storage`，任一根可用空间低于默认 1 GiB、可配置 1–100 GiB 的阈值即形成 `storage:low_space`、`failure_code=storage_low_space` 的 P1 系统维护事项。
- 同一进程内持续低空间只触发一次；即使用户先解决事项也不会每 5 分钟重开。全部受控根恢复到阈值以上后解除周期锁存，之后再次跌破可形成新的独立 incident。重启后仍低空间会按新的运行周期检查，但活动事项仍由数据库唯一约束去重。
- 数据库不可写时复用并发安全 journal，以 `storage_low_space` 白名单 kind 在下次健康启动补偿。payload 和 journal 不保存盘符、根路径、精确容量或底层探测错误；探测 API 失败只写固定内部日志，不把“无法检测”伪装成“空间不足”。
- 前端严格接受该固定来源，显示“本地存储 / 容量检查”并提供“打开数据与备份”。阈值可在设置中预览并保存，三个固定逻辑位置可无路径手动检查；物理卷身份只用于进程内同卷去重，API 仅返回共享布尔值，30 天历史与默认 7 天趋势已开放。

### AI 独立轨道：系统维护续办（H5-E10）

- `/api/v1/ai/inbox?kind=maintenance_failure` 只读取上述九类仍为 `open/tracking` 的系统维护事项，并返回原 Inbox ID、创建时间和代码白名单生成的组件/操作/错误码；不会转发 Inbox title、summary、payload、固定 message、路径、容量、备份身份、日志或底层错误。
- 智能体续办项点击后打开精确 `/inbox/:id`，继续由本模块执行人工解决、忽略与安全设置导航。任何处理完成都会失效 Agent Inbox 查询；模型或列表不能自动重试、恢复、清理、重启或改变事项。
- 启动恢复诊断 H5-E9 仍由备份模块独立提供，在恢复门禁期间可读；本节不复制其状态，也不把普通 AI Inbox 宣称为维护模式下可用。

### 人工基线未覆盖与后续规划

- 法定节假日/自定义 Reminder、系统原生通知，以及 Task/Project/Client 等自由业务来源自动创建 Reminder；当前只有两个固定日历自动化预设；
- Project 完成以外的独立里程碑、Client/Invoice，以及其他尚未接入的系统故障来源投影；“检查开票”仅为本地人工提示，不是 Invoice 领域实现；
- 卷级容量历史趋势；
- 已交付来源以外的多态删除协调、Inbox Item 硬删除；
- Agent Actor、Adapter、Agent Run、自动执行、取消/重试、能力令牌和崩溃恢复；
- v0.1 不包含 AI/LLM；独立 AI 轨道已接入上述逐项审批的受理分诊、关系/拆分、Reminder、强制解决和快照式全部已读，但自主多步编排、智能排程、Agent 执行与自动报告尚未接通。

T-11C 只编排用户显式提交的 Task 草稿，不自动生成任务内容，不调用 AI/LLM，也不改变 owner/person 的本地责任记录边界。

## 当前用户流程

### 创建和编辑手工条目

1. 用户在收件箱点击“新建条目”。
2. 填写标题、说明、优先级和可选截止时间。
3. 前端只提交手工字段；Sidecar 固定补充 manual kind/source/policy，在同一事务中创建 Inbox Item 与 `created` Workflow Event。
4. 创建成功后打开真实详情。活动中的条目可编辑；归档条目必须先重新打开。
5. 有效编辑会写 `triaged_at`、递增版本并追加 `updated` 事件；完全相同的 PATCH 返回当前快照，不递增版本或重复写事件。

### 已读与快照式全部已读

- 单条 `read` 只在首次写入 `read_at`，不会写 `triaged_at`、改变主状态或清除稍后时间。
- 列表响应返回同一服务端时点的 `snapshot_at` 与 `server_now`。前端执行“全部标为已读”时，把当前响应的 `snapshot_at` 作为 `through_created_at` 提交。
- 批量命令把 `through_created_at` 作为时间截止，只处理 `created_at <= cutoff` 且 `updated_at <= cutoff`、当前处于 `open / tracking`、尚未读取，并且按该 cutoff 仍属于待处理可见范围的条目：`snoozed_until` 为空或不晚于截止时间。
- 归档项、截止时仍在稍后的项、`created_at` 晚于 cutoff 的新项，以及截止后发生编辑、分诊、重开等任何更新的项都不会被标记；当前页面的搜索、优先级或视图筛选也不会缩小批量范围。这是保守跳过新变化，不是对 cutoff 时历史状态的重建。
- 批量更新与每条 `read` 事件在一个事务中提交；任一事件写入失败时全部回滚。相同幂等键重放首次 `marked_count` 快照，不重复写事件。
- 本版是 `through_created_at` 时间截止，而不是 `created_at + ID/序列` 的不透明严格游标；纳秒 UTC 下极低概率同时间戳碰撞可能被同一截止范围纳入。若未来需要严格游标，应新增稳定序列/ID 组成的不透明快照令牌。
- AI H2-C5 复用同一领域事务和 cutoff 语义，但在执行前额外保存并重验最多 1000 个候选的 `ID+version` 指纹；人工批准的是确认卡上展示的精确候选集合，不是确认时任意重新计算的一批未读事项。

### 稍后与恢复

1. 用户设置一个晚于服务端当前时间的 RFC 3339 时间。
2. 条目保留 `open / tracking` 主状态，写入 `snoozed_until`；首次此类分诊会写 `triaged_at`。
3. 未来时间的条目进入“稍后”视图；列表每 15 秒低频刷新并按响应中的服务端时间重新查询，到达后无需调度器写库即可重新出现在“待处理”。
4. 用户可提前执行 `unsnooze` 清除时间并立即恢复；该操作不改变已读事实。

### 解决、忽略和重开

1. `resolve` 与 `dismiss` 只接受 `open / tracking`，均要求 trim 后非空且不超过 2,000 字符的原因。
2. 两个命令都会写入 `triaged_at`、清除稍后时间并进入相应终态，但不会写入 `read_at`。
3. 终态条目不能编辑或稍后；若终态仍未读，详情可直接执行 `read`，该操作不要求先重新打开，也不改变终态。
4. 终态条目无论已读与否均可 `reopen`。重开清除解决/忽略字段和稍后时间；有活动 Task 关系时进入 `tracking`，否则进入 `open`，并保留已读与分诊时间。

### 关联已有 Task

1. 用户在活动 Inbox Item 详情打开“关联任务”，从真实 Task 查询结果中选择已有 Task，并明确是否为必需任务。
2. 前端提交 Task ID、`is_required`、当前 Inbox `If-Match` 和稳定幂等键；服务端在同一事务创建 `relation_type=linked` 的关系、必要时把 `open` 推进为 `tracking`、递增 Inbox version 并写 `task_linked` 事件。
3. 关系列表的 active 部分不分页且最多 100 条；history 使用 `page/page_size` 分页。服务端实时读取 Task 状态，因此显示进度不依赖 Inbox 中的缓存字段。
4. 修改 required 使用同一路径 PATCH，并写 `task_requirement_changed`；它只改变关系事实，不在 A2 自动解决 Inbox Item。
5. 解除关系必须填写原因。DELETE 软写 `unlinked_* / unlink_reason` 与 `task_unlinked` 事件；最后一条活动关系解除时 `tracking → open`。
6. 重新关联同一 Task 创建新的关系 ID 和 linked 时间。历史记录保留此前 required、position、解除人、解除时间及原因。

### 关联 Task 的硬删除

1. Task 仍有活动 Inbox 关系时，Task DELETE 返回 `TASK_HAS_ACTIVE_INBOX_RELATIONS`，不会移动 Artifact 文件或删除 Task 聚合。
2. 用户回到对应 Inbox Item，带原因软解除全部活动关系。
3. Task 随后可按原有 `If-Match`、Focus Session 和 Artifact 删除契约硬删除。
4. 已解除历史关系的 nullable `task_id` 置空；不可变 `task_ref_id` 与 `task_title_snapshot` 继续用于显示“原任务已删除”。这不是 T-11E 的多态来源删除协调。

### 拆分、分派与自动结清

1. 用户从 Project Artifact 深链或直接在活动 Inbox Item 详情选择“拆分并分派”。可信来源中的 canonical Project 默认带入首项及后续草稿，但每项可清除或改选；用户填写说明与独立完成条件、父任务、owner/person 负责人、验收策略和 required。person 只作本地责任记录。
2. 前端提交 Inbox 当前版本和稳定幂等键。Sidecar 先完整校验，再在一个事务内创建 Task、标签、层级、Assignment、reviewer、`created` 关系与审计；失败时不保留部分数据。
3. 提交可保留 `manual`，也可切换 `all_required_tasks_done`。自动策略必须至少有一个必需 Task；none Task 可 direct complete，manual Task 提交后进入 waiting_review 并由 owner accept/request changes，所有活动必需 Task done 后 system 自动解决条目。
4. 必需 Task 处于 `todo / in_progress / blocked / waiting_review / cancelled` 时均不自动解决。自动解决后若必需 Task 通过 reopen、返工或其他受控命令离开 `done`，条目自动回到 `tracking`。
5. 若业务确实无需等待，用户展开“例外：强制解决”，填写原因并二次确认。该命令只作用于自动策略，保留未完成 Task，并记录 `forced` mode 与不可变事件。
6. 成功 Inbox mutation 失效可信来源 Project；split 另失效 Task、Today、Project。Project Artifact 再读时取得当前 follow-up/progress，但其 Project 数值 `ETag` 不承担 Inbox 并发语义。

## 列表与计数契约

| view      | 服务端范围                                                   |
| --------- | ------------------------------------------------------------ |
| `inbox`   | `open / tracking` 且未稍后，或 `snoozed_until <= server_now` |
| `snoozed` | `open / tracking` 且 `snoozed_until > server_now`            |
| `archive` | `resolved / dismissed`                                       |

- API 支持 `q`（标题/摘要）、`priority`、`page` 和 `page_size`；默认每页 50，最大 100，当前 UI 每页 30。
- 当前视图的 `meta.total` 会应用 view、搜索和优先级条件。
- `meta.unread_total` 始终是**全局当前待处理 `inbox` 视图**的未读数，不受当前 `view / q / priority` 影响，也不包含未来稍后项或归档项。
- 稳定排序依次为优先级 P0→P3、有截止时间优先、截止时间升序、创建时间倒序、ID 升序。

## 数据/API/状态与事件

### `inbox_items`（schema v12，在当前 schema v35 延续）

| 字段                                | 当前约束 / 说明                                                                                                                                                                                          |
| ----------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `id`                                | UUID 主键                                                                                                                                                                                                |
| `kind`                              | 表约束 manual/event/reminder；公开创建 API 仅 manual，内部 Reminder 使用 reminder，其余已交付业务来源使用 event                                                                                          |
| `title / summary`                   | 标题 2–200；摘要最多 10,000                                                                                                                                                                              |
| `source_entity_type`                | 公开创建 API 固定 manual；内部来源包括 reminder/task_artifact/task/task_due/project_completion/system_maintenance/client_followup/content_item/roadmap_milestone/invoice_due/automation/agent_run_failed |
| `source_entity_id`                  | 当前手工项必须为 null；系统维护项为 `backup:create/verify/drill/restore`、`database:startup/migration/runtime`、`sidecar:startup` 或 `storage:low_space`                                                 |
| `source_event_key`                  | nullable；非空值受部分唯一索引保护；Artifact、Task 阻塞/临期、Project 完成和系统维护分别使用稳定键                                                                                                       |
| `source_deleted_at`                 | 手工/Reminder/系统维护为 null；Artifact/Task/Task due/Project 完成来源归档后删除时原子写入，且不可再次修改                                                                                               |
| `priority`                          | P0 / P1 / P2 / P3                                                                                                                                                                                        |
| `status`                            | open / tracking / resolved / dismissed                                                                                                                                                                   |
| `resolution_policy`                 | manual/all_required_tasks_done；公开新建仍为 manual，T-11C 拆分可切换自动策略                                                                                                                            |
| `due_at`                            | 可空 RFC 3339 UTC                                                                                                                                                                                        |
| `read_at / triaged_at`              | 相互独立的已读与分诊时间                                                                                                                                                                                 |
| `snoozed_until`                     | 可空；未来值进入稍后视图，到期后按查询恢复                                                                                                                                                               |
| `resolved_* / resolution_*`         | resolved 终态事实；mode 为 manual/automatic/forced，自动模式使用 system Actor                                                                                                                            |
| `dismissed_* / dismiss_reason`      | dismissed 终态的 owner、时间和原因                                                                                                                                                                       |
| `payload_json`                      | 必须是 JSON object；当前 UI 不编辑                                                                                                                                                                       |
| `version / created_at / updated_at` | 乐观并发版本与 UTC 时间                                                                                                                                                                                  |

schema v13 不重建 `inbox_items`；schema v14 由 Reminder 调度器使用既有来源字段；schema v15 增加自动结清保护；schema v23–v25 冻结 Task Artifact、Task 阻塞与 Task 临期来源；schema v26 增加系统维护来源；schema v28 增加 Project 完成周期来源、不可变快照和删除协调。均不重建表、不回填旧事实。其他 event 来源仍是受约束的未来空间。

### `inbox_item_tasks`（schema v13）

| 字段                                 | 当前约束 / 说明                                                        |
| ------------------------------------ | ---------------------------------------------------------------------- |
| `id`                                 | UUID 主键                                                              |
| `inbox_item_id`                      | Inbox Item 外键                                                        |
| `task_ref_id`                        | 不可变原 Task UUID；Task 删除后仍保留                                  |
| `task_id`                            | nullable 实时 Task 外键，`ON DELETE SET NULL`                          |
| `task_title_snapshot`                | 建立关系时保存的标题；Task 删除后用于解释历史                          |
| `relation_type`                      | linked / created；A2 关联已有 Task 使用 linked，T-11C 拆分使用 created |
| `is_required`                        | 0/1；修改后立即参与自动策略 reconciliation                             |
| `position`                           | 大于等于 1；单条关系按末尾追加，活动列表稳定排序                       |
| `linked_by_actor_id / linked_at`     | 当前固定内置 owner 与 UTC 时间                                         |
| `unlinked_by_actor_id / unlinked_at` | 软解除 Actor/时间；与原因成组出现                                      |
| `unlink_reason`                      | trim 后 1–1,000 字符；历史行不可通过重新关联覆盖                       |

- 活动关系定义为 `unlinked_at IS NULL` 且 `task_id IS NOT NULL`；同 Inbox/Task 只允许一条活动关系，active 总数上限为 100。
- history 按 `unlinked_at DESC, linked_at DESC, id DESC` 稳定分页；重新关联写新行。Task 硬删除只允许作用于已解除关系，随后 `task_id` 置空，快照字段不变。
- 不增加 Task.version→Inbox.version trigger；关系 GET 每次实时 JOIN Task。Task/关系写命令在应用事务结束前显式调用统一 reconciliation。

### 已实现 API

| 方法   | 路径                                     | 契约摘要                                                                                                                                     |
| ------ | ---------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| GET    | `/api/v1/inbox-items`                    | 三视图、搜索、优先级、risk、分页、稳定排序；返回全局待处理未读数与快照时间                                                                   |
| GET    | `/api/v1/stats/inbox`                    | 实时派生 pending/unread/tracking/blocked/waiting_review 与 server_now                                                                        |
| POST   | `/api/v1/inbox-items`                    | 新建 manual 条目；可选 `Idempotency-Key`；返回 `201`、数据和 `ETag`                                                                          |
| POST   | `/api/v1/inbox-items/read-all`           | 以 `through_created_at` 批量已读；可选幂等键；不受当前筛选缩小                                                                               |
| GET    | `/api/v1/inbox-items/:id`                | 详情、可用动作和 `ETag`                                                                                                                      |
| PATCH  | `/api/v1/inbox-items/:id`                | 编辑标题/摘要/优先级/截止时间；强制 `If-Match`；终态拒绝；`system_maintenance` 不能设置截止时间                                              |
| GET    | `/api/v1/inbox-items/:id/events`         | 分页时间线，默认 50/最大 100；返回 `ETag` 与 `meta.inbox_item_version`                                                                       |
| POST   | `/api/v1/inbox-items/:id/read`           | 单条已读；强制 `If-Match`，可选幂等键                                                                                                        |
| POST   | `/api/v1/inbox-items/:id/snooze`         | 设置未来 `snoozed_until`；强制 `If-Match`，可选幂等键                                                                                        |
| POST   | `/api/v1/inbox-items/:id/unsnooze`       | 清除稍后时间；强制 `If-Match`，可选幂等键                                                                                                    |
| POST   | `/api/v1/inbox-items/:id/resolve`        | 必填原因，manual 解决；强制 `If-Match`，可选幂等键；不隐式已读                                                                               |
| POST   | `/api/v1/inbox-items/:id/dismiss`        | 必填原因，忽略归档；强制 `If-Match`，可选幂等键；不隐式已读                                                                                  |
| POST   | `/api/v1/inbox-items/:id/reopen`         | 重新打开并保留 read/triaged；强制 `If-Match`，可选幂等键                                                                                     |
| GET    | `/api/v1/inbox-items/:id/tasks`          | 返回 `data.active/history`；history 分页，meta 含 Inbox version 与实时 progress，响应携带 Inbox `ETag`                                       |
| POST   | `/api/v1/inbox-items/:id/tasks/:task_id` | body `{is_required}`；关联已有 Task，强制 Inbox `If-Match`，可选幂等键，第一条关系进入 tracking                                              |
| PATCH  | `/api/v1/inbox-items/:id/tasks/:task_id` | body `{is_required}`；修改活动关系 required，强制 Inbox `If-Match`，可选幂等键                                                               |
| DELETE | `/api/v1/inbox-items/:id/tasks/:task_id` | body `{reason}`；带原因软解除，强制 Inbox `If-Match`，可选幂等键，最后关系回到 open                                                          |
| POST   | `/api/v1/inbox-items/:id/split`          | 原子创建 1–20 个 Task、完成条件、层级、owner/person Assignment、manual owner reviewer、created 关系与审计；强制 Inbox `If-Match`，可选幂等键 |
| POST   | `/api/v1/inbox-items/:id/force-resolve`  | body `{confirm:true,reason}`；仅自动策略的例外解决；强制 Inbox `If-Match`，可选幂等键                                                        |

关系 GET 返回 `{data:{active,history},meta:{page,page_size,total,inbox_item_version,progress}}`；`page/page_size` 只作用于 history。单条关系命令返回 `{inbox_item,relation,progress}`；split 返回 `{inbox_item,tasks,relations,assignments,progress}`。Project Artifact 聚合另以 nullable `followup` 只读暴露 Inbox ID/version/status/policy/source deletion/progress；其响应仍使用 Project 数值 `ETag / meta.project_version`，不能替代 Inbox `If-Match`。Reminder 使用独立路由和内部到期投影；Task Artifact follow-up 由 `submit-output` 投影，Project 完成由 Project transition 投影。所有来源都没有公开创建路由，当前也没有 Inbox 删除路由。

### 幂等、并发与事务

- 创建、单条命令和全部已读支持 `Idempotency-Key`；同 key/endpoint/规范请求重放首次状态码与数据快照，同 key 不同请求返回 `409 IDEMPOTENCY_CONFLICT`。
- Task 关系 POST/PATCH/DELETE 同样保存规范化请求摘要和首次响应；摘要包含 Inbox expected version、Task ID 与 required/reason。重放发生在当前关系/Inbox 版本检查前，不重复关系或事件。
- 单条命令的请求摘要包含 expected version，并在读取当前数据库版本之前检查幂等快照，因此一次已经成功但响应丢失的命令可用原 key/原版本安全重放。
- PATCH 和所有单条命令使用资源 `ETag`/`If-Match`；缺失前置条件和旧版本分别被拒绝，不自动覆盖其他窗口的新事实。
- 关系写入以 Inbox 为聚合边界：成功只递增 Inbox version，不递增 Task version；Task 在关系提交前必须仍存在，Task 删除在同一 SQLite 写边界内检查活动关系。
- 关系 GET 实时 JOIN Task；没有 Task.version→Inbox.version 传播 trigger。Task 生命周期、产出验收和关系写入在各自事务内调用统一 reconciliation，前端同时失效相关查询。
- Project Artifact `followup` 同样是实时读投影：Inbox/Task 变化不递增 Project version。成功 mutation 根据严格来源类型和 payload `project_id`，按 cancel→invalidate 顺序刷新来源 Project；Artifact 请求消费 `AbortSignal`。split 额外失效 Task/Today/Project 前缀。`followup.inbox_item_version` 是 Inbox 并发版本，Project UI 当前只深链。
- 业务事实、Workflow Event 与幂等快照在同一个 SQLite 事务中提交；事件失败不遗留半完成状态。

### Workflow Event

- Inbox 事件使用 `aggregate_type = inbox_item`、条目 ID 作为 aggregate ID，并记录内置 owner Actor、request ID、前后 JSON 快照、命令序号和 UTC 时间。
- 当前 action 另包含 `tasks_split / automatically_resolved / automatically_reopened / force_resolved / source_projected / source_deleted`；拆分产生的 Task/Assignment 也写各自聚合事件。
- 关系事件的前后快照包含关系、读取时 Task 摘要、实时进度、Inbox 状态/version 与可选解除原因；这些是不可变审计快照，不是可写的 Task 或 Inbox 第二事实源。
- 事件沿用 schema v8/v9 的不可修改、不可删除保护；事件列表只读，不作为当前 Inbox Item 状态的第二副本。

## 与其他模块协作

| 模块     | 当前协作事实                                                                                                                                                                                                        | 后续扩展                                         |
| -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------ |
| 任务     | 可关联/拆分 Task；关系行打开共享详情；完成条件、owner/person、manual owner reviewer 与 reconciliation 已接通；follow-up/阻塞/临期来源已投影                                                                         | 更多筛选与跨模块统计继续扩展                     |
| 项目     | Artifact 聚合显示 nullable follow-up/实时 required 进度并深链；split 继承/清除来源 Project，成功 mutation 失效来源 Project；Project 与 Inbox version 保持独立                                                       | 其他里程碑只随真实状态/事实扩展                  |
| 客户     | 当前没有客户活动或回访来源                                                                                                                                                                                          | v0.4 回访到期生成去重 Inbox Item                 |
| 发票     | 当前没有财务来源                                                                                                                                                                                                    | v0.4 临期/逾期及开票节点生成本地待办             |
| Actor    | owner 执行拆分/强制解决；owner/person 可成为初始负责人，person 明确仅作本地责任记录；manual reviewer 固定 owner；system 执行自动结清/重开                                                                           | Agent Actor 仍延后                               |
| 今日     | 已展示待处理/跟进/阻塞/待验收实时计数并支持风险筛选深链                                                                                                                                                             | 随 T-11E 来源投影自然纳入更多业务事件            |
| 系统维护 | 备份四类操作失败、数据库启动/迁移、Sidecar 启动、运行期数据库操作失败和可配置低空间已安全投影；相关事项可打开数据与备份，物理卷同卷去重、无路径手动容量检查、数据库打开前白名单恢复进度和全局启动故障恢复页也已交付 | 启动前备份选择仍待                               |
| Agent    | 未实现                                                                                                                                                                                                              | v0.2 只通过受控 Adapter/Run 产生待验收或失败事件 |

完整协作图参见[整体功能架构](../functional-architecture.md)。

## 后续实施顺序

1. **前置 Actor/Task 工作流基础（已完成）**：Actor、Assignment、六状态命令、Task Event、manual Submission/Artifact。
2. **T-11A1 手工 Inbox Item 数据契约（已完成）**：schema v12、约束、索引和迁移保留测试。
3. **T-11B 人工受理与分诊（已完成）**：真实列表/详情/编辑、已读/快照式全部已读、稍后/恢复、解决/忽略/重开及事件 UI/API。
4. **T-11A2 Task 关系事实（已完成）**：schema v13、活动/历史关系、实时进度、已有 Task 关联、required 修改、带原因软解除、状态联动、事件，以及关联 Task 硬删除互锁和历史快照。多态来源删除协调不属于 A2。
5. **T-11A3 Reminder 事实（已完成）**：schema v14 一次性事实 + schema v32 daily/weekly 系列 + schema v40 monthly/当地日锚点 + schema v41 weekdays，创建/查询/编辑/取消、启动补偿、15 秒扫描、IANA/DST/月末推进、稳定事件键与幂等 Inbox/下一 occurrence 投影。
6. **T-11C 拆分与分派（已完成人工闭环）**：原子多任务/父子拆分、可信来源 Project 继承/清除/改选、独立完成条件、owner/person Assignment 与本地责任提示、manual owner reviewer、共享 Task 详情、统一 reconciliation、自动解决/重开和 force-resolve。
7. **T-11F 运营计数（已完成）**：实时统计 API、risk 列表筛选、Sidebar 徽标和 Today 风险卡。
8. **T-11E v0.1 来源投影（部分完成）**：显式 follow-up Task Artifact、Task 阻塞、提前 24 小时 Task 临期、Project 完成周期，以及备份四类操作失败、数据库启动/迁移、Sidecar 启动、运行期数据库操作失败和可配置低空间监测已完成；物理卷同卷去重和无路径手动容量检查已在设置页交付，后续按真实业务模块继续来源投影。
9. **T-11D v0.2 Agent**：健康 Adapter、Run、受控产出、取消/重试、人工验收、返工和崩溃恢复。
10. **后续业务事件**：随 v0.3/v0.4 路线图、发票和回访模块交付后启用。

## 验收状态

### 当前纵切已覆盖

- [x] 断开外网时手工创建、列表、详情、编辑、已读、稍后和归档命令只依赖本地 Sidecar/SQLite。
- [x] schema v11→v12 升级保留既有事实；fresh v12 约束、nullable 部分唯一来源键和外键检查有迁移测试。
- [x] 创建与命令幂等重放不重复资源或事件；异体复用被拒绝。
- [x] PATCH/命令拒绝旧版本；事件失败时事实与批量已读整体回滚。
- [x] 全局未读计数不受页面筛选影响；时间截止式全部已读只处理创建与最后更新时间都不晚于 cutoff、且按 cutoff 仍属于待处理可见范围的未读，截止后更新项保守跳过。
- [x] AI 全部已读只接受 `workspace_search(inbox_item)` 返回的服务端快照；预览绑定全局候选数量与 `ID+version` 指纹，人工确认时变化拒绝，执行与审批同事务且不改变分诊、任务、提醒或来源事实。
- [x] 解决/忽略原因必填且不隐式已读；重开保留 read/triaged。
- [x] 前端覆盖分页、筛选、加载、空、错误、重试和冲突草稿保留的自动测试。
- [x] 已有 Task 活动/历史关系、实时进度、required 修改和带原因软解除均经过真实 API/SQLite，不复制 Task 状态。
- [x] 关系写入使用 Inbox `If-Match`/幂等快照，与状态/version/事件同事务；重新关联不覆盖旧历史。
- [x] 第一条/最后一条活动关系驱动 `open / tracking`，reopen 按活动关系选择状态；A2 不自动解决。
- [x] 活动关系阻止 Task 硬删除，软解除后可删且历史保留 Task ID/标题快照。

### 完整人工编排仍需验收

- [x] T-11C 批量拆分、Assignment 和审计在一个事务中完成，失败不遗留部分事实。
- [x] T-11C 项目字段复用共享 ProjectSelect；每次只读 20 条候选，250 ms 搜索、请求取消、ID 去重、选中保留、显式清除和反馈/键盘交互均有自动化接线证据，生产路径不再串行拉全 Project。
- [x] 可信来源 canonical Project 默认带入首项/新增草稿且可逐项清除或改选；完成条件写入独立 Task 字段，person 显示仅本地责任记录。
- [x] 活动关系及仍有实时 Task 的历史关系可打开共享 Task 详情；已删除 Task 只保留快照。
- [x] 进度完全从活动必需 Task 派生，零必需任务不自动解决。
- [x] 非 done Task 不误触发自动解决；自动完成后依赖失效会重开，手工/强制解决不会误重开。
- [x] schema v30 父任务自动验收不创建 Inbox 关系、不继承或改写 `is_required`；只有显式 active required 关系参与 Inbox 自动解决/重开。
- [x] Reminder 与 Task 临期跨扫描/重启、follow-up Artifact 跨提交重放、同一次 Task block 幂等重放，以及同一备份/数据库/Sidecar 系统维护 source id 的活动 incident 均只生成一条 Inbox Item；启动/运行期 journal 模糊清理重放不重复。Task 改期、重复阻塞和归档后再失败按各自稳定事实生成独立来源。`BACKUP_INVALID` 不创建系统维护 incident。
- [x] 关系软解除、重新关联和关联 Task 删除后历史可解释。
- [x] Task Artifact/Task 阻塞/Task 临期多态来源删除会先限制活动项，归档后原子标记来源删除并保留快照；系统维护来源禁止 `source_deleted_at`。其他未来来源仍需逐项实现。
- [x] Sidebar/Today 计数与 risk 深链已接真实统计；ProjectSelect 的真实浏览器键盘/焦点、窄屏和 1,000/10,000 条项目性能，以及 Inbox 长列表和窄屏视觉仍需专项验收。
- [x] Project Artifact 返回 nullable follow-up 的 Inbox ID/version/status/policy/source deletion/实时 progress；响应保持 Project 数值 ETag，成功 Inbox mutation 失效来源 Project，split 另失效 Task/Today/Project。
- [x] Go 金链覆盖 `requires_followup → split(owner/person + manual owner reviewer) → complete + submit(waiting_review) → accept → automatic resolved/100%`；前端另覆盖 person 本地责任提示与提交载荷。
- [ ] v0.2 Agent 成功只进入 `waiting_review`，只有 owner 可接受，重试保留全部 Run 与 Artifact。

上述自动化不代表所有端到端人工浏览器验收已经完成；真实浏览器/WebView 的深链往返、弹层焦点、窄屏、1,000/10,000 条 Project/Task 及 Inbox 长列表仍待专项验证。

## 相关代码/PRD 链接

- [PRD：收件箱与本地工作编排中心](../opc-workspace-PRD.md#56-收件箱与本地工作编排中心)
- [整体功能架构](../functional-architecture.md)
- [schema v12 Inbox 迁移](../../services/sidecar/internal/database/migrations/012_inbox_items.sql)
- [schema v13 Inbox–Task 关系迁移](../../services/sidecar/internal/database/migrations/013_inbox_item_tasks.sql)
- [schema v14 Reminder 迁移](../../services/sidecar/internal/database/migrations/014_reminders.sql)
- [schema v32 重复 Reminder 迁移](../../services/sidecar/internal/database/migrations/032_recurring_reminders.sql)
- [schema v15 Inbox 编排迁移](../../services/sidecar/internal/database/migrations/015_inbox_task_orchestration.sql)
- [schema v23 Task Artifact 来源迁移](../../services/sidecar/internal/database/migrations/023_task_artifact_inbox_projection.sql)
- [schema v24 Task 阻塞来源迁移](../../services/sidecar/internal/database/migrations/024_task_blocked_inbox_projection.sql)
- [schema v25 Task 临期来源迁移](../../services/sidecar/internal/database/migrations/025_task_due_inbox_projection.sql)
- [schema v26 系统维护来源迁移](../../services/sidecar/internal/database/migrations/026_system_maintenance_inbox_projection.sql)
- [schema v28 Project 完成来源迁移](../../services/sidecar/internal/database/migrations/028_project_completion_inbox_projection.sql)
- [schema v29 存储设置迁移](../../services/sidecar/internal/database/migrations/029_storage_settings.sql)
- [Reminder 模块文档](reminders.md)
- [Inbox API](../../services/sidecar/internal/api/inbox_items.go)
- [共享 Inbox 领域命令](../../services/sidecar/internal/api/inbox_commands.go)
- [AI Inbox 审批与快照式全部已读](../../services/sidecar/internal/api/ai_inbox_actions.go)
- [AI Inbox 审批与隔离 HTTP 验证](../../services/sidecar/internal/api/ai_inbox_actions_test.go)
- [Inbox–Task 关系 API](../../services/sidecar/internal/api/inbox_item_tasks.go)
- [Inbox 编排 API](../../services/sidecar/internal/api/inbox_orchestration.go)
- [共享原子拆分命令](../../services/sidecar/internal/api/inbox_split_commands.go)
- [AI 整批拆分预览](../../services/sidecar/internal/api/ai_inbox_split.go)
- [AI 拆分与 Harness HTTP 回归](../../services/sidecar/internal/api/ai_inbox_split_test.go)
- [Inbox 来源投影服务](../../services/sidecar/internal/api/inbox_source_projections.go)
- [Project Artifact follow-up 读模型与金链](../../services/sidecar/internal/api/project_artifacts_test.go)
- [Task 临期来源扫描服务](../../services/sidecar/internal/api/task_due_projections.go)
- [系统维护来源投影](../../services/sidecar/internal/api/system_maintenance_inbox.go)
- [备份 API 测试](../../services/sidecar/internal/api/backups_test.go)
- [Inbox API 测试](../../services/sidecar/internal/api/inbox_items_test.go)
- [Inbox–Task 关系 API 测试](../../services/sidecar/internal/api/inbox_item_tasks_test.go)
- [Inbox 迁移测试](../../services/sidecar/internal/database/inbox_migration_test.go)
- [Project 完成来源迁移测试](../../services/sidecar/internal/database/project_completion_inbox_projection_migration_test.go)
- [Inbox–Task 关系迁移测试](../../services/sidecar/internal/database/inbox_task_migration_test.go)
- [Inbox 页面](../../apps/web/src/pages/InboxPage.tsx)
- [Inbox 详情](../../apps/web/src/components/InboxItemDetailModal.tsx)
- [Inbox–Task 关系 UI](../../apps/web/src/components/InboxItemTasksSection.tsx)
- [Inbox 时间线](../../apps/web/src/components/InboxItemEventsSection.tsx)
- [Inbox 来源上下文](../../apps/web/src/components/InboxSourceContext.tsx)
- [Inbox 拆分任务表单](../../apps/web/src/components/InboxTaskOrchestrationModal.tsx)
- [Inbox Query 失效协作](../../apps/web/src/api/hooks.ts)
- [共享 Project 选择器](../../apps/web/src/components/ProjectSelect.tsx)
