# 自动化模块

H5-F3（2026-09-21）将内置执行器新增的 `AGENT_MODEL_TRUNCATED/AGENT_MODEL_FILTERED/AGENT_MODEL_RESPONSE_INVALID` 纳入失败诊断的安全闭集，原事件、动作、Inbox payload 和来源校验保留该码；固定中文解释不读取原始模型错误。105 仍须人工启用，仅处理未来真实失败；通知重试不重试 Agent，业务导入仍只保留历史证明。无新字段、迁移或规则，见 [执行失败契约](local-agents.md#执行完成信号与安全失败原因h5-f32026-09-21)。

> 当前基线：app v0.1.1 / API v1 / SQLite schema 76（2026-09-21）。五个内置预设已可用、默认关闭，H3-C3 已接 Agent 失败诊断；不提供自由规则。[ADR-029](../adr/029-agent-workspace-capabilities.md) H3-C1/C2 增加智能体读取、人工审批规则启停/有限配置及失败 Run 原快照重试；新来源不新增迁移或模型执行能力。

## 定位与边界

自动化把本机已经发生的 Workflow Event 或本地时间条件转换为明确的下一步工作，减少重复整理。它是一个受限的本地规则引擎，不是通用脚本平台、云工作流服务或对外消息机器人。

- 触发器只读取本地事件和本地调度时间。
- 当前动作白名单为本地 Inbox Item、Task 或 Reminder，分别由内置预设选择，不能由模型或配置切换。
- 不执行任意 Shell/SQL/HTTP，不读取未授权目录，不自动发送邮件、发票、客户消息或内容平台请求。
- 不自动确认付款、完成回访、发布内容或修改其他模块的受控业务事实。
- v0.2 只提供内置预设规则和有限参数，不提供自由表达式或用户代码编辑器。
- AI 或本地 Agent 不能自行创建、启用或扩大规则权限。智能体可起草既有规则的启停与有限配置，只有用户逐项确认才生效；启用或修改已启用规则还须额外确认持续效果。

## 当前实现状态

- H4-AT：项目完成预设生成的 Inbox 来源卡增加精确 Automation Run 入口 `/settings/automation?run=<id>`，并保留历史项目入口；均携带 UI 验证的原对话身份。快照只作导航线索，详情重新读取，失效不改投其它 Run；运行结果→Task/Inbox/Reminder 也保留合法会话，仅接受受控类别和合法 ID，不自动切换原对话或执行业务操作。不属于 Agent Run，不自动重试/启用规则。见 [来源往返](inbox.md#来源详情与原对话往返h4-at2026-09-21)。

- schema v33 已新增 `automation_rules` 与不可变终态 `automation_runs`。五个代码所有的稳定预设在 Sidecar 初始化时幂等登记，数据库只保存开关、受限配置、下一计划时间、版本和运行快照，不保存脚本。
- 五个预设当前可用：Project `project_completed` → 本地核对开票 Inbox Item、每日查看今日任务 Reminder、周五本周复盘 Reminder、Invoice `invoice_overdue` → 本地跟进 Task，以及 H3-C3 的 Agent `agent_run_failed` → 本地诊断 Inbox。均默认关闭、须人工启用，失败预设不补发历史 Run。
- 设置左栏已有“自动化”模块：规则列表、依赖状态、配置即时服务端预览、IANA 时区/当地时间、优先级、保存、启停、最近运行、空/加载/错误和失败手动重试均接入真实 API。规则详情和运行详情还提供“交给智能体”，仅暂存规则/Run 身份与固定提示词，仍由用户回到 `/ai` 后显式授权和发送。
- 智能体“续办队列”另以 metadata-only 方式汇总没有后继 attempt 的最新失败 Run；仅显示规则/Run 身份、attempt、重试状态、稳定错误码和结束时间，点击进入既有精确运行详情。队列不读取配置/动作快照或结果摘要，也不自动重试、启用规则或授权模型。
- 事件执行器挂接明确的 Project 完成、Invoice 逾期与 Agent 执行失败 Workflow Event，通过 `automation_event_deliveries` 在来源事务内捕获不可变规则/配置/动作快照，提交后与后台消费者投递。同一 `rule_id + source_event_id` 最多一个首轮 attempt。动作、成功 Run 和审计同事务；动作失败回滚动作并保留失败 Run，投递基础设施失败保留捕获记录重试，不撤销已经提交的来源事实。
- 本地调度器在 Sidecar ready 前和每 15 秒扫描：daily/weekly 按 IANA 当地钟点计算，DST 缺失分钟推进到首个有效分钟，重复分钟选择第一次；离线旧窗口折叠为一条 skipped Run，只在当前当地日创建 Reminder，并推进唯一下一窗口。
- Run 失败最多 3 次 attempt，后台退避为 1 分钟、5 分钟；手动重试创建新 attempt，保留原规则版本、配置和动作快照。停用阻止新触发与定时规则重试，但**不撤销已捕获事件的投递及其重试**；配置修改也不改变这些捕获快照，历史和既有对象不删除。基础设施投递重试与 Run attempt 是不同层次。
- 业务 JSON/ZIP 导入导出已包含规则与 Run；导入只接受五个稳定预设身份、当前 schema、合法配置/关系和空目标，未改动的默认禁用规则不阻塞 fresh-target 判定。
- 当前实现没有外部工作流连接器、后台网络发送、Shell/SQL/HTTP 动作、自由表达式或 Agent 执行路径。

## 目标功能

- 展示内置预设规则，允许用户启用/禁用并调整受限参数。
- 支持事件触发和本地时间触发，显示下一次计划运行时间。
- 在启用前预览触发条件、将创建的本地对象和权限范围。
- 记录每次运行的触发来源、动作结果、耗时、错误、重试和去重信息。
- 失败运行可由用户手动重试；可重试错误使用有上限的退避策略。
- 对应用关机期间错过的时间计划执行有界补偿，不补发过期且已失去意义的重复提醒。
- 防止同一事件重复执行、规则递归触发和循环创建工作项。

首批预设可包括：

- 项目完成后创建“核对并准备发票”Inbox Item。
- 每日指定时间创建“查看今日任务”提醒。
- 发票逾期事件创建本地跟进任务（已实现）；临期自动化不在此预设范围。
- 周五指定时间创建本周复盘提醒。
- 本地 Agent Run 失败后创建一条诊断 Inbox Item；该内置规则是唯一生产者，固定使用 `agent-run:<run_id>:failed` 去重，重放不覆盖或重开用户已处理的事项。

## 关键用户流程

1. **浏览规则**：用户打开自动化设置，查看规则名称、触发条件、动作、所需模块和当前可用性。
2. **配置预览**：用户调整本地时间、阈值或目标列表；界面展示示例输入、将创建的对象和明确的“不对外发送”边界。
3. **启用规则**：Sidecar 校验依赖、参数、时区和动作白名单后保存版本化配置。
4. **事件执行**：Workflow Event 到达后生成稳定去重键，事务性创建目标对象并写 Automation Run。
5. **计划执行**：本地调度器按 IANA 时区计算时间；重启后执行有界补偿并跳过已处理窗口。
6. **查看与重试**：用户查看运行详情；失败时手动重试会创建新的 attempt，但复用原逻辑去重边界。
7. **停用**：停用立即阻止新触发，但保留规则版本、历史 Run、已捕获事件的交付责任和已经创建的本地对象。

### Agent 失败诊断闭环（H3-C3，2026-09-21）

预设 `agent-run-failed-inbox`（稳定 ID 尾号 `105`）只消费真实 `agent_run_failed`：queued 身份复检失败和执行器失败都在终态事务中冻结证据、追加事件并捕获当时已启用规则。取消、中断、成功、pending 和 retained 不触发此预设；pending 只能恢复登记。规则打开、保存或查询不启动模型、不补发旧失败。

- 失败 Workflow Event 严格冻结 `agent_run_id/task_id/attempt/error_code/failed_at` 五字段；动作快照另加 `action_type=inbox_item/priority`。时间为固定九位小数 UTC；错误码为代码白名单，未知原码映射 `AGENT_RUN_FAILED`，不带 Task 标题、输入/输出、Provider、凭据、路径或 stderr。
- Inbox 初始 open/manual，通用标题/说明，`kind=event`、`source_entity_type=agent_run_failed`、`source_entity_id=<原 Agent Run ID>`、`source_event_key=agent-run:<id>:failed`。payload 为上述五字段加 `automation_rule_id/automation_run_id/source_event_id` 共八字段。原 Agent Run、通知 Automation Run 与失败 Workflow Event 是不同身份；自动化仍用 `event:<rule_id>:<event_id>` 作为逻辑键。
- 失败终态与捕获同事务；独立持久消费者原子创建通知、成功 Automation Run 和审计。重复/重启扫描不重复通知，停用或改配置不撤销已捕获责任。通知写入失败只重试本地动作，不重跑 Agent；投递前原 Run/Task 删除或失配则记录不可重试 `SOURCE_UNAVAILABLE`，不创建假链接。
- 原生来源卡定位 `/tasks/:taskId?agent_run=:runId` 并保留 UI 提供的合法原对话身份，目标页重新验证归属。`workspace_inbox_source` 先要求 `work+outputs`，再验证原失败 Run、owner Task、失败事件和成功 Automation Run 的反向结果，只返回身份、状态、nullable version 与固定 route。缺少权限不查保护表；缺失/错配来源不返回活动链接。
- 解决诊断不修改 Run、完成 Task、接受产出或自动接续计划。执行续办队列仍由原 Run 事实派生，不加入同一运行的诊断副本；`automation_failure` 专指通知等本地自动化动作失败。Agent 重试仍须 `agent_execution` 和独立人工确认，文件另需 `agent_files`。105 的 `automation.retry` 卡使用严格七字段快照，明确只重试创建通知。
- Task 删除影响计入诊断来源；活动来源阻止删除，终态来源先写删除标记/审计再级联 Run。便携 JSON/ZIP 仍排除 `agent_runs`、待投递表和执行正文；导入验证失败事件→通知 Run→Inbox 的历史闭合证明。缺少 live Run 仅为 unavailable，不伪造 source_deleted_at；明确删除标记须有删除审计。

API v1/schema 076 不变，无新迁移。旧代码不理解新来源与快照，不能仅凭相同 schema 安全降级；业务包须由支持 H3-C3 的版本读取。确定性证据见 `automation_agent_run_failure_test.go`、`ai_inbox_agent_failure_test.go` 及对应导入、删除、前端测试；真实 Provider、桌面与断电专项另行验收。

### 智能体桥接（H3-C1，已实现源码与确定性测试）

- 单次 `work` 授权注册 `workspace_automations`，支持 `view=rules/rule/runs/run`。规则列表最多五个代码所有预设；详情必须使用真实 UUID。运行列表可按 `rule_id/status` 过滤，limit 1–20、offset 0–1000，固定时间/ID 排序，返回分页边界与 `server_now`，不是完整历史汇总。
- 规则读取复用原生目录和配置读模型。Run 只选择 ID、规则版本、触发类型、计划窗口、终态、attempt、retryable/retry_at、安全错误码、白名单结果 ID/类型及开始/结束时间；不读业务快照、原始事件、去重/因果键或结果正文。未知错误码替换为 UNKNOWN_ERROR，存储异常脱敏；读取不写调度时间、执行动作或重试。
- `work+actions` 下的 `workspace_propose` 增加 `automation.enable/disable/update`，目标为 `automation_rule_id/expected_version`。启停 changes 必须为空；事件配置仅接受 priority，定时配置必须明确给出 local_time（HH:mm）和 IANA timezone，缺失不猜默认值。不允许创建预设、改变触发/动作/权限、Shell 或任意脚本。
- 预览完整展示稳定权限、触发/动作定义、可用性、启用状态和前后配置。`next_run_at` 由调度器维护且可不增加版本地推进，不冻结进审批；确认按当前时间计算变更后的计划，相同配置保持版本及下一窗口，不立即运行。
- 启用或修改已启用规则需 decision 额外携带人工 `confirm_automation_effects:true`；模型不能提供此标记。停用和修改禁用规则不接受该标记。待确认卡缺失/false 返回 `422 AUTOMATION_ENABLE_CONFIRMATION_REQUIRED`；版本或稳定预览变化拒绝旧卡。原生 API 与 AI 共用 `automation_commands.go`，规则、领域审计与审批决定同事务；重复确认不重复执行，已确认重放不要求再次勾选。
- 确认卡通过 H4-E 的 `/settings/automation?rule=<id>` 或 `?run=<id>` 定位实际规则/运行；设置弹层入口及兼容字段 `ui_entry=automation_settings` 保留，不制造 `/automations` 页面。决定后取消旧查询并刷新规则、Run、Inbox、Reminder；后续会话仅携带无配置正文的审批回执，不继承工具授权。
- 失败运行的人工审批重试见下述 H3-C2；原生设置的手动重试继续可用。真实模型与原生桌面专项未由隔离测试代替。

### 智能体失败重试（H3-C2，已实现源码与确定性测试）

- `automation.retry` 需要单次 `work+actions`，只接受真实 `automation_run_id`、原运行的 `rule_version` 作为 `expected_version`、空 `changes`。Run 是不可变终态，没有可编辑版本；不能传规则目标、替换配置/动作、执行代码或人工同意字段。
- `preview.automation_retry` 为仅本地人工查看的完整预览：原运行/规则 ID、捕获与当前规则版本、当前启用状态、触发/动作类型、权限、原计划窗口、失败错误码、当前/下次 attempt，以及原配置和动作快照。快照可能含发票描述等业务资料，不发给模型；模型工具只返回提议标识。预览沿用 24 KiB 上限，超限明确拒绝，不截断后要求确认。
- decision 必须额外携带人工 `confirm_automation_retry:true`，缺失/false 返回 `422 AUTOMATION_RETRY_CONFIRMATION_REQUIRED`；不能用于拒绝或其他动作。确认重验原版本、完整预览、资格与既有后续 attempt。原生手动、后台与 AI 共用 `prepareAutomationRetry/retryAutomationRunInTransaction`，只复用原快照，不使用规则新配置；最多三次 attempt，同一失败 Run 只能有一个后续尝试。已停用定时规则拒绝，已捕获事件继续保留交付责任。
- 审批、重试记录、业务对象与领域事件在同一事务内；审批记录或事件失败全部回滚。重复确认只返回原结果；原生/后台已抢先重试则返回 `409 AUTOMATION_RETRY_ALREADY_EXISTS`，不会重复执行。当前规则或权限在预览后变化要求重新提议。
- `confirmed` 只表示新尝试已记录，不保证动作成功。`result_id` 是新 Run ID，`result_version` 是其捕获规则版本。API 与后续会话的 `automation_run_result` 返回不可变新 Run 元数据（状态、attempt、retryable/retry_at、安全错误码和白名单结果 ID/类型），不返回业务快照或结果正文；succeeded 才显示创建入口，failed 明确本次未创建目标。`retry_at` 仅是该次失败时的计划，不代表后续自动重试已发生，最新链仍从运行查询/设置读取。
- 确认卡展示原动作、独立勾选、成功或再次失败、真实业务对象链接和自动化设置入口。响应丢失回读原提议，不生成新的重复操作；决定后取消迟到查询并刷新 Automation/Inbox/Reminder/Task/Project/Today/搜索等受影响读模型。再次可重试失败仍沿用已存在的有界自动退避，不扩展下一轮工具授权、不启用停用规则。

### 规则与运行精确定位（H4-E，已实现源码与确定性测试）

- 工具的规则列表/详情、Run 列表/详情及审批回执返回代码生成的 `route`：`/settings/automation?rule=<uuid>` 或 `?run=<uuid>`。待确认/拒绝的重试指向原 Run，确认后优先指向实际新 attempt 的 `result_id`，不从文字或 `retry_at` 推断执行结果。
- 主/侧对话及确认卡共用严格链接允许列表：只能有一个规则或 Run UUID，不接受额外/重复参数、跨站地址、片段或模型指定的 `return_session`。返回身份由 UI 从来源消息会话追加，只用于返回原对话；查看不批准建议、不调用模型，也不扩大读取范围。
- 新页面复用 `AutomationSettings` 及 `AutomationRunDetailModal` 的行内详情，原设置弹层继续可用。规则定位从五个预设中选择指定身份，缺失时明确提示，不改选首项；运行直接调用既有详情 API，不依赖最近历史页。完整尝试链切换写入 URL，浏览器前后导航保持目标，手动重试成功回到新 Run。
- 定位时重新读取事实，读取期间或失败不展示旧缓存正文、缺失规则不发起配置预览；Run 响应身份必须匹配请求。离开页面取消读取；手动重试已经发出后仍可完成，但迟到响应不会把用户从原对话拉回详情。配置草稿沿用原表单，显式切换目标不把草稿带到另一规则。
- 规则详情与运行详情提供“交给智能体”：规则交接推荐 `work+actions`，提示先用 `workspace_automations(view=rule,id=...)` 核对当前版本、可用性和权限，再起草 `automation.enable/disable/update`；运行交接推荐 `work+actions`，提示先用 `workspace_automations(view=run,id=...)` 核对捕获规则版本、尝试链和安全结果，再在失败且可重试时起草 `automation.retry`。点击只暂存本地 route、名称、状态/attempt/错误码等元数据，不复制配置快照、动作快照、结果摘要、授权或 Provider；不会保存、启停、重试或发送消息。
- 本地人工详情可查看原配置/动作快照，但这些内容仍不进入模型工具或后续回执。保存、启停、重试仍须人手点击，沿用既有版本与事务约束；没有新增 API、schema 或业务事实副本。工作台/智能体模式往返保留原页面查询参数和片段，不丢失报告或记录条件。
- 确定性验收覆盖实际规则选择、尝试链导航/返回、错配/过期缓存/失败、取消请求、迟到重试，以及规则/Run 交接只暂存 pendingIssue、不自动保存/启用/重试。浏览器已验证真实禁用规则定位和模式往返。当前真实库无 Run，完整尝试链与重试只在隔离夹具验证，真实模型和桌面专项仍待。

## 数据、API、状态与事件

### 数据

- `automation_rules`：`id`、稳定 `preset_key`、`enabled`、严格 `config_json`、nullable `next_run_at`、`version` 和审计时间。名称、描述、触发与动作定义来自代码目录，身份不可改。
- `automation_runs`：`id`、规则及原规则版本、`event/schedule` 来源、计划窗口、`logical_key/dedupe_key`、终态、1–3 attempt、重试关系、配置/动作快照、安全错误码、结果引用、开始/结束时间和有界因果字段。
- 预设定义由代码版本化；数据库只保存允许用户修改的参数，不保存可执行代码。
- `logical_key + attempt` 全局唯一；事件首轮另以 `rule_id + source_event_id` 唯一，计划首轮以 `rule_id + scheduled_for` 唯一。重放、重启和重复扫描不会产生第二个目标对象。
- 因果元数据预留 `caused_by_run_id`，数据库把深度约束在 0–4；当前五个可用预设不消费 Automation 自身事件，因此不能形成链式递归。

### API

- `GET /api/v1/automations/rules`
- `GET / PATCH /api/v1/automations/rules/:id`
- `POST /api/v1/automations/rules/:id/preview`
- `POST /api/v1/automations/rules/:id/enable`
- `POST /api/v1/automations/rules/:id/disable`
- `GET /api/v1/automations/runs`
- `GET /api/v1/automations/runs/:id`
- `POST /api/v1/automations/runs/:id/retry`

API 不提供任意 trigger/action 类型创建接口；所有枚举都由服务端白名单校验。

### 状态与事件

- 规则状态：`enabled / disabled / unavailable`；`unavailable` 表示依赖模块未交付或权限条件不足。
- 当前 Run 只持久化不可变终态：`succeeded / failed / skipped / cancelled`；首版同步本地动作不暴露瞬时 queued/running 事实。
- 当前 Workflow Event action：`automation_rule_updated / enabled / disabled` 与 `automation_run_succeeded / failed / skipped`。
- 自动化自身事件默认不能再次触发同一规则；确需链式处理必须由预设显式声明并受深度限制。

## 与其他模块协作

- **Workflow Event**：唯一业务触发事实，不从页面展示文本或数据库轮询猜测状态变化。
- **收件箱、任务、提醒**：v0.2 唯一允许的动作目标；写入和 Run 成功在同一事务中提交。
- **项目/发票/回访/路线图/内容日历**：各模块只发布本地事件；对应模块未上线时相关规则显示 unavailable。
- **本地 Agent**：Agent Run 可成为事件源，但自动化不能提升 Agent 能力或绕过人工验收。
- **桌面通知**：未来可由 Reminder 统一触发本应用通知；通知权限被拒绝时仍保留应用内提醒。

## 分阶段实施

1. **A1 前置事实层（已完成当前依赖）**：Workflow Event、Project 完成、Inbox 与 Reminder 动作目标均可用。
2. **A2 ADR 与预设目录（已完成首版）**：五个稳定预设、动作白名单、IANA/DST、离线折叠、三次 attempt、递归和权限边界已固化在代码与 schema v33。
3. **A3 数据/API（已完成）**：规则/Run 迁移、列表、详情、配置、启停、预览、历史和手动重试已实现。
4. **A4 事件执行器（已完成当前三个预设）**：Project 完成、Invoice 逾期及 Agent 失败均接通事务捕获及恢复投递；不支持自由规则或任意链式消费者。
5. **A5 本地调度器（已完成 daily/weekly）**：下一次运行、IANA/DST、启动/周期扫描、离线折叠与重复窗口防护已实现。
6. **A6 界面与验收（部分完成）**：React 设置、运行日志、重试、依赖不可用及自动化测试已完成；真实 WebView、休眠唤醒、系统时区变更与超长历史专项仍待验收。

## 验收标准

- 同一规则和事件无论重放、并发或重启多少次，最多产生一份目标工作项。
- 时间规则在用户时区、夏令时、休眠、跨午夜和关机补偿场景下结果正确。
- 停用后不再捕获新事件或开始新计划；已捕获事件继续投递与重试，历史和已创建对象不删除。
- 失败重试有上限、有新 attempt 记录，不覆盖原失败；不可重试错误不会循环。
- 当前无链式预设且 Automation 自身事件没有消费者；数据库因果深度约束已测试。未来开放链式预设前仍须补循环图的确定性测试。
- 依赖模块不可用时规则不能启用，UI 明确说明原因。
- 所有动作均落在本地白名单；测试验证不存在 HTTP、任意 Shell、外部消息或未经确认修改业务事实的路径。
- 加载、空、错误、重试、长历史分页和并发配置冲突均有测试。

## 相关 PRD 与代码链接

- [产品 PRD](../opc-workspace-PRD.md)（§5.9“自动化”、§10.7）
- [Sidecar 路由](../../services/sidecar/internal/api/router.go)
- [schema v33 自动化迁移](../../services/sidecar/internal/database/migrations/033_preset_automations.sql)
- [预设目录](../../services/sidecar/internal/api/automation_catalog.go)
- [规则 API](../../services/sidecar/internal/api/automations.go)
- [共享规则命令](../../services/sidecar/internal/api/automation_commands.go)
- [AI 查询](../../services/sidecar/internal/api/ai_automations.go)、[AI 提议](../../services/sidecar/internal/api/ai_automation_actions.go)、[AI 确定性验收](../../services/sidecar/internal/api/ai_automations_test.go)
- [AI 原快照重试与结果](../../services/sidecar/internal/api/ai_automation_retry.go)、[重试事务/隐私/Harness 验收](../../services/sidecar/internal/api/ai_automation_retry_test.go)、[重试确认卡](../../apps/web/src/components/AiAutomationRetryDetails.tsx)
- [执行器与调度器](../../services/sidecar/internal/api/automation_engine.go)
- [Agent 失败诊断与严格快照](../../services/sidecar/internal/api/automation_agent_run_failure.go)、[生命周期/投递验收](../../services/sidecar/internal/api/automation_agent_run_failure_test.go)
- [Agent 来源权限与当前关系](../../services/sidecar/internal/api/ai_inbox_agent_failure.go)、[双协议 Harness 验收](../../services/sidecar/internal/api/ai_inbox_agent_failure_test.go)
- [便携历史证明](../../services/sidecar/internal/api/automation_agent_failure_import.go)、[Task 删除协调](../../services/sidecar/internal/api/agent_run_failure_inbox_sources.go)、[导入/删除验收](../../services/sidecar/internal/api/agent_failure_automation_import_test.go)
- [设置界面](../../apps/web/src/components/AutomationSettings.tsx)
- [后端自动化验收](../../services/sidecar/internal/api/automations_test.go)
- [前端自动化验收](../../apps/web/src/components/AutomationSettings.test.tsx)
- [精确定位页面](../../apps/web/src/pages/AutomationLocationPage.tsx)、[页面往返验收](../../apps/web/src/pages/AutomationLocationPage.test.tsx)、[链接允许列表](../../apps/web/src/lib/automationLocation.ts)
- [自动化交给智能体提示词](../../apps/web/src/lib/aiIssueHandoff.ts)、[交接边界验收](../../apps/web/src/lib/aiIssueHandoff.test.ts)
- [AI 导航身份验收](../../services/sidecar/internal/api/ai_automation_location_test.go)
