# 任务管理模块

## AI Agent Run 审核状态与续办联动（H5-G43，2026-09-23）

用户通过任务页原生审核 AI Agent Run 已提交的 Task Submission 后，成功事务提交会唤醒 AI 计划续办扫描；幂等重放不制造第二次事实变更。AI 工作计划独立显示最新 Submission 状态，但 Agent Run 的 `submitted` 不会被改写成 Task 完成或审核状态。刷新只是重读当前事实；所有审核仍由用户检查证据并明确决定。无 Task 状态、路由、数据库/schema 或审核规则变化。详见 [AI 助手 H5-G43](ai-assistant.md#agent-run-产出与人工验收状态联动h5-g432026-09-23)。

## AI 保存视图精确定位（H5-E36，2026-09-21）

智能体在本条 `workspace_ui+work` 授权下，可对真实存在的保存视图提出待人工确认的打开请求；只传规范视图 ID，不带筛选定义或任务结果。用户确认后进入 `/tasks?task_view=<id>`，任务页重新读取当前视图并应用其筛选，保留可用的返回原对话入口；加载期间不显示旧任务列表。已删除视图明确报不存在、读取失败可重试，二者均不退化显示未筛选任务。此路径不修改 Task，不代表 AI 已执行任何任务或获得写权限。原 `task_view.*` 提议、版本校验和人工确认仍独立，API v1/schema078 不变。参见 [AI 定位链](ai-assistant.md#受确认的任务保存视图定位h5-e362026-09-21)。

## AI 右侧产出原位续办（H5-E34，2026-09-21）

用户在智能体右侧打开指定 Task Artifact 后，可点击“继续交给智能体”，直接在原对话准备复查问题，无需先离开到完整提交页。交接只暂存 Artifact/Task/Submission ID 与固定问题，不复制预览正文、名称或文件；需再次人工带入、选择本条 `work+outputs` 并发送。模型须重读当前真实产出，文件正文还需独立 `output_files`，操作与验收仍另行确认。预览失效时不能发起交接；完整详情页和人工文件下载不变。见 [AI 助手](ai-assistant.md#右侧产出的原位智能体续办h5-e342026-09-21)。

## AI 对话内产出预览（H5-E33，2026-09-21）

用户确认 AI 建议的具体 Task Artifact 后，可在右侧工作区打开独立只读标签并留在原对话，或进入原精确提交批次页。右侧标签用 Artifact/Task/Submission 三重身份核对新鲜详情；读取中、失败、错配和已删除均不显示缓存正文。文本/结构化内容只在本地展示，链接不自动访问，文件只显示元数据并引导至完整详情页人工下载。它不把内容交给模型，也不提交、验收或修改 Task；关闭右栏设置时只提供完整详情。见 [AI 助手](ai-assistant.md#任务产出的对话内右侧预览h5-e332026-09-21)。

## AI 精确产出定位（H5-E32，2026-09-21）

在 `workspace_ui+work+outputs` 的单消息授权下，AI 可提议打开真实、未删除的 Task Artifact，但不能自定 Task/Submission 归属或 URL。用户选择完整详情时进入 `/tasks/:taskId/submissions/:submissionId?artifact=:artifactId`；页面重读该批次并只展开匹配的产出卡片，缺失或已删除时显示错误而不替换目标。H5-E33 另允许确认后留在对话里右侧预览。文本/结构化产出只读预览，文件仍需用户手动下载；不会自动把正文发给 AI、提交、验收或修改 Task。参见 [AI 助手](ai-assistant.md#任务产出的受确认精确定位h5-e322026-09-21)。

## 已选任务的返回断点（H2-W13，2026-09-21）

从任务页人工交接已选任务到智能体后，本次应用会话在内存保存原筛选、页码、列表/看板视图和最多 20 个 Task ID。返回 `/tasks` 会先恢复视图，再通过既有 `GET /api/v1/tasks/:id` 对每个 ID 重新读取；全部成功且身份匹配才恢复勾选，批量命令使用新版本。任一失败不恢复部分选择，提示重新选择；读取中不能选任务或发起批量写入。返回状态不持久化，刷新应用后不恢复；不会复制行正文、批量表单或授予 AI 权限。API v1/schema078 不变，原有版本冲突与人工二次确认继续生效。详见 [AI 返回断点](ai-assistant.md#已选任务返回断点h2-w132026-09-21)。

## AI 已选任务精确摘要（H2-W12，2026-09-21）

`workspace_tasks({task_ids:[...]})` 在单消息 `work` 下可按请求顺序一次读取 1–20 个真实 Task 的有界标题、状态、优先级、类型、计划/截止时间、版本、更新时间和路由；精确模式标题最多 100 字，过长有省略号，保证满 20 项仍在工具结果预算内。它与 `filters/limit/offset` 互斥，缺失、重复或非规范 ID 使整组失败，不把选择悄悄缩小。此模式复用结构化任务查询的脱敏字段，不含描述、客户/项目名称、产出或文件；返回的是实时只读快照，不是批量写入许可。H2-W11 交接用它先读完整选择，责任另读 `workspace_task_assignments`，详情按 ID 另读。无新原生 API、scope 或迁移，修改仍须 `actions` 和人工确认；见 [AI 助手](ai-assistant.md#已选任务精确成组读取h2-w122026-09-21)。

## 已选任务的智能体交接（H2-W11，2026-09-21）

任务页选中 1–20 个真实 Task 后，可人工点击“交给智能体（已选任务）”。交接只带规范 Task ID 与固定读取要求，不带列表行、未保存批量操作字段或授权；超过 20 项需先缩小选择，当前写入/二次确认未结清时不可交接。进入对话后仍须人工核对单消息 `work+actions`、发送；模型用 `workspace_tasks(task_ids)` 重新读取完整选择的当前版本、优先级和日期，需要责任时再用 `workspace_task_assignments`，才可提出已有 `task.batch_update` 的待确认建议。任一任务不再存在则重新选择，不把剩余任务当原集合。H2-W13 允许同次应用会话返回 `/tasks` 后全部重读成功再恢复勾选；交接不自动批量写入、分派或运行 Agent。API v1/schema078 不变。详见 [AI 助手交接](ai-assistant.md#已选任务交给智能体h2-w112026-09-21)。

## 批量优先级与截止时间（H2-W10，2026-09-21）

`PATCH /api/v1/tasks/batch` 增加 `set_priority` 与 `set_due_date`：仍需 1–100 个 `{id,expected_version}`，优先级仅 `P0–P3`，截止时间必须是含时区偏移的 RFC3339 时间或显式 `null`。其它字段和动作混用会拒绝；服务端把时间规范为 UTC，统一预检全部 Task 版本，同一事务逐项修改并回传 changed 和任务当前状态，任一项失败整批回滚。无变化的 Task 不升版本。任务页的批量选择增加优先级选项与本机时区 `datetime-local` 输入；空值清除截止时间，发送前转换为绝对 UTC 时间。派生任务与 Today 查询按既有缓存失效刷新；到期 Inbox 仍由现有投影规则处理，不在此命令里伪造已提醒事实。

AI 独立轨道在保存会话 `work+actions` 下对 1–20 个真实 Task 复用这两种变更，冻结逐项版本与前后值，经一张人工确认卡重验后整批提交。模型必须获得当前 Task ID/版本；截止时间不猜用户未明确的时区或时间。没有 schema 迁移、新 scope、自动执行或任意批量写入；详见 [AI 批量卡](ai-assistant.md#批量优先级与截止时间h2-w102026-09-21)。

## AI 已有任务批量责任变更（H2-W9，2026-09-21）

`workspace_task_assignments(task_ids)` 在单次 `work` 授权内精确返回 1–20 个 Task 的当前责任和 Task 版本；不返回历史或 Actor 私有资料。保存会话的 `work+actions` 可对该组用 `task.batch_update` 的 `set_assignee/clear_assignee/set_reviewer/clear_reviewer` 提出一张卡。设置需真实 `actor_id`，全部责任动作需原因，`items/expected_versions` 须逐项对应；预览显示每个任务的责任前后值和身份。确认时重验 Task、Actor/Adapter、Assignment 和完整预览，并复用原生分派/改派/结束命令；任一失败整批回滚。首次分派、历史、manual 审核人和父任务待验收仍按原领域规则；分派 person 不联系，分派 agent 不启动 Run。回执代表审批批次，后续操作须重新读取单项当前版本。API v1/schema078 不变；见 [AI 助手](ai-assistant.md#已有任务成组责任变更h2-w92026-09-21)。

## AI 同项目批量建任务（H2-W7，2026-09-21）

保存会话的 `work+actions` 可提出 `task.batch_create`，把一个真实 Project 拆为 1–20 个新 Task，并用一张完整草案卡让用户确认。顶层 `project_id/expected_version` 必须来自当前项目详情；`changes.drafts` 依次包含唯一的短 `key`、`title`，可用 `parent_key` 指向本批次更早草案，另可提供描述、类型、优先级、计划/截止时间、预计分钟、完成标准、验收策略及真实标签 ID。H2-W8 允许每项可选一个真实可分派 `assignee_actor_id`；没有任意父 Task ID、状态或执行字段。全部新任务为同一项目下的 `todo`，不关联 Inbox、不启动 Agent。

提议前复用 Task 创建校验，确认卡冻结项目名称/版本、每个任务的完整可显示字段和标签名称；指定初始负责人时还冻结 Actor 名称/类型/状态/版本，`manual` 草案同时显示 owner 审核人。确认事务重读 Project、标签、Actor/Adapter 资格并逐字比对冻结预览，再按原生 Task 创建事务逐个写入、协调父链并复用原生 Assignment 命令写初始责任；任一失败整批回滚，旧卡不能悄然适配新事实。未指定负责人则不隐式创建审核人，兼容原行为。成功回执以 Proposal ID/version 1 代表整批，打开 `/projects/:id`，同时列出每个新 Task 的精确 `/tasks/:id` 入口；拒绝或未确认时没有创建回执。任务 UUID 由 Proposal ID 与草案序号确定，保证回读历史回执仍能定位原创建项。后续同会话模型操作回执仅额外带有成功创建项的有序 UUID，不带任务正文；当前存在性与版本仍须经新授权读取，不据历史回执直接执行。API v1/schema078 不变，无新 HTTP 端点或迁移；单次提议仍受 64 KiB 输入及 128 KiB 预览上限约束。

确定性测试覆盖父子关系、精确回执、项目版本漂移、第二项失败整批回滚、字段拒绝和前端确认/导航。真实 Provider 是否能稳定提取项目版本并选择批量提议，以及桌面视觉，仍须单独验收。

H5-F6（2026-09-21）：后继任务可在开始/当前事实重新执行中分页搜索、明确选择同项目其他已完成任务当前已验收批次的受控文本文件。来源 Task 版本及文件 hash 预检、文件发送与跨任务独立确认后，实际新结果仍进入原人工验收；旧来源和批次不改写。活动后继引用阻止源文件、源 Task 及 Project 删除；原样重试重验原来源，pending 只恢复登记。无新 Task 状态或迁移，详见 [同项目文件交接](local-agents.md#同项目已验收文件交接h5-f62026-09-21)。

H5-F5（2026-09-21）：任务执行可显式选择远程 Anthropic，覆盖 text/file/files、返工和按当前事实重新执行。v5 原样重试先读取完整冻结详情，确认协议/8192 token 预算及适用文件、返工范围；成功仍需实际提交和人工验收。无新 Task 状态或迁移，见 [Anthropic 受控执行](local-agents.md#anthropic-受控执行h5-f52026-09-21)。

H5-F4（2026-09-21）：执行列表、Run 详情和右侧资源增加“按当前事实重新执行”，可从失败/取消/中断或仅保留的旧结果发起关联新 Run。先读取完整当前任务/责任/模型/资料，再分别确认执行与来源，适用文件/返工还需独立同意；不继承旧输入，也不自动分派。新 Run 提交后仍进入原人工验收，旧记录保持，pending 只恢复登记。见 [当前事实执行契约](local-agents.md#按当前事实重新执行h5-f42026-09-21)。

H5-F3（2026-09-21）：Agent 执行列表和详情显示固定中文失败原因，区分模型截断、过滤/拒绝与异常响应；这些结果不创建提交或产出，不推进 Task。正常结束后的登记仍进入原人工验收链，查看错误不触发重试。见 [执行完成与失败契约](local-agents.md#执行完成信号与安全失败原因h5-f32026-09-21)。

H5-F2（2026-09-21）补齐“退回后再次执行”：从当前 `changes_requested` 批次明确选择最多四个非文件旧产出（或只选退回意见），完整预览并另行同意后启动新的 v4 Agent Run。Task/当前批次/版本/责任与源证据重新核验，活动引用阻止产出删除；不覆盖旧批次、不把成功 Run 当失败重试，也不自动验收新结果。文件仍须单独选择和确认，详见 [返工执行契约](local-agents.md#退回意见驱动的新执行h5-f22026-09-21)。

## AI 受控文件产出复查（H5-F1，2026-09-21）

新增独立单消息 `output_files`，必须同时有 `work+outputs`；不依赖 `actions`、`agent_execution` 或 `agent_files`，临时会话可用。原 `outputs` 仍只读取文件元数据和已授权非文件文本。主/侧权限面板必须人工手选新范围，不随交接推荐选中；授权不限于当前页面或某个文件，模型可按需读取任意 Task 当前/历史提交中的合规产出。界面披露所读正文会发送给当前 Provider，远程 Provider 会使内容离开本机、已发送内容不可撤回；停止生成阻止后续工具查询，不撤回已发送数据。

- 工具 `workspace_read_artifact_file` 必填 canonical `task_id/submission_id/artifact_id`、1–9007199254740991 的整数 `task_version` 与 64 位小写十六进制 `expected_sha256`；先用 `workspace_task_submissions(view=artifacts)` 获取最新 Task 版本、真实批次和文件 SHA-256，不从 Run ID 或文本副本猜测。可选 `content_offset` 为 0–65536、默认 0，`content_limit` 为 1–4000、默认 4000；均按 Unicode 字符计算。偏移超过正文末尾返回空摘录而非完整正文，不能据 `has_more=false` 声称读过之前的页。
- 每次在同一读事务核对现存 Task 版本、Submission 归属、未删除的 `storage_kind=file` Artifact 及 metadata SHA-256，再安全打开受管对象并全量核验实际大小和 SHA-256。只接受不超过 64 KiB、合法 UTF-8 且无 NUL 的代码白名单文本文件；空文本可读。允许 txt/log、Markdown、CSV、HTML/CSS、JS/TS、JSON/XML/YAML、Go/Python/Shell/SQL 等安全扩展名与兼容 MIME；只当文本，不执行文件指令。PDF、图片、压缩包、任意路径、Project Attachment、Run 输入及 recovery staging 均不开放。
- 结果区分 Task 当前版本/状态、Submission 状态与 `is_current`；Submission 无独立版本，不能用 Task version 或 sequence 冒充。每页带大小/SHA-256、正文总字符数、偏移、`has_more/next_offset`。完整文件的完整性校验不代表本页覆盖全文，更不是已通过验收；历史批次即使可读也不能用来验收当前批次。跨页必须继续绑定同一版本、身份和 SHA-256，版本或文件漂移重新读取元数据后再决定，不拼接不同证据。
- 缺权限在查询保护对象前拒绝；身份错配、软删除、版本/哈希漂移、缺失/损坏、非法文本和超限均不返回正文或本机路径。每次以实际字节重新核验，不把数据库旧 `integrity_status` 当当前证据，也不修改它。原单工具 24 KiB 编码结果、累计预算和取消边界保持，转义使结果超预算时须减小分页；未读页不得声称已读完。
- Task/Project/右侧任务产出、精确提交批次及待验收交接保留原推荐范围，只新增独立文件授权与工具的引导，不自动下载、发送或继承执行文件许可。读完后仍只有 `work+outputs+actions` 可提出原 `task.review`，当前待验收批次、owner、版本/完整预览与独立 `confirm_task_output` 门禁不变；文件复查不是自动审批，PDF/外部证据等仍需人工检查。

API v1/schema 076 不变，无新增 HTTP 路由、迁移、文件写入或执行权限；工具不持久化原始文件结果，模型可能在回答中引用，遵循会话保存设置。此阶段只完成受控文本文件复查，跨格式/视觉验收、全批次持久阅读证明、自治复查与真实 Provider/原生桌面专项仍待。

代码证据：[文件读取](../../services/sidecar/internal/api/ai_artifact_file_tool.go)、[权限界面](../../apps/web/src/components/AiWorkspaceAccess.tsx)、[交接提示](../../apps/web/src/lib/aiIssueHandoff.ts)。确定性测试：[文件契约与双协议 Harness](../../services/sidecar/internal/api/ai_output_file_read_test.go) 覆盖原生文件提交、读取、验收提议、缺少独立人工同意时零写、确认后真实状态及下一条不继承授权；[权限组件测试](../../apps/web/src/components/AiWorkspaceAccess.test.tsx) 覆盖手选、依赖撤销及临时会话。真实 Provider/桌面仍需单列验收。

H3-C3（2026-09-21）：Task 删除影响统计与事务纳入 `agent_run_failed` 诊断来源。活动诊断须先解决或忽略（`TASK_HAS_ACTIVE_INBOX_SOURCES`）；终态来源先写 source_deleted_at/审计再级联 Run，来源证明损坏返回 `TASK_AGENT_FAILURE_SOURCE_INVALID`，不靠快照猜归属。便携导入未携带 Run 时仍按闭合历史证明保护来源，不自动重建 Run。精确执行详情沿合法 UI 返回身份回原对话；Task 有编辑草稿/在途命令时禁用执行详情入口，登记恢复在途不能经详情返回离开。此为局部入口保护，不是全站导航阻塞；API v1/schema 076 不变，详见 [失败诊断](automation.md#agent-失败诊断闭环h3-c32026-09-21)。

> 实现基线：app v0.1.1 / API v1 / SQLite schema v78（2026-09-21）。Task D2 由 schema v9 引入；v11 增加 Focus 工时回写，v23–v25 增加 Artifact/阻塞/临期 Inbox 来源，v30 增加父任务自动发起验收，v69 增加 AI 确认身份，v71/v74 建立并强化关联 Task 的 Agent Run，v75 为 Run 产出登记增加精确 Submission/Artifact 关联，v76 增加会话计划修订；v77/v78 是 AI 续办及权限请求操作态迁移，不改变 Task 表；Task 生命周期与人工验收仍由本模块拥有。
>
> 版本边界：任务事实层、Actor/Assignment、T-18D D1/D2、Focus 工时回写、Inbox Task 关系/拆分编排、一次性 Reminder、六状态看板与跨列受控生命周期、共享服务端 Project 选择、显式 follow-up Artifact/Task 阻塞/Task 临期→Inbox、有门禁的父任务自动发起验收，以及 Windows 内置 Agent Run、受确认 AI 启动、可恢复产出登记和 H5-C 受控文件输入/单文件输出已交付。自动建 Reminder、任意文件系统或 Shell/browser/Git/terminal 自动访问、跨平台/external Runner 与多步自主编排属于后续纵切。

导航：[文档中心](../README.md) · [整体功能架构](../functional-architecture.md) · [PRD v10.12](../opc-workspace-PRD.md) · [Actor 与分派](actors.md) · [数据管理](data-management.md)

## 定位与边界

H4-AT：Inbox 产出来源卡按历史快照进入 `/tasks/:taskId/submissions/:submissionId`，保留合法原对话入口，不跳到最新提交。目标读取独立核 Task/Submission 归属；这并非验证入口 Artifact 当前归属，也不会自动下载或验收。批次页的“打开任务详情处理”继续保留返回身份；阻塞/临期来源仍进入精确 Task 详情，详见 [Inbox 来源往返](inbox.md#来源详情与原对话往返h4-at2026-09-21)。

H4-I：任务详情新增“交给智能体”，只暂存本 Task 类型/ID 并关闭详情进入对话，随后由人带入草稿并重新授权；不自动发送或执行。未保存字段、版本冲突及在途任务命令会禁用交接，详见 [AI 助手交接契约](ai-assistant.md#h4-i-从工作台事项进入智能体)。

Task 是 opc-workspace 唯一可执行工单。项目、未来 Inbox、提醒和 Agent 都只能关联或驱动 Task，不能另建一套互不一致的完成状态。

当前模块负责：

- Task 事实、父子关系、标签、Project 关联、计划与完成标准；
- assignee/reviewer 的当前责任与历史 Assignment；
- 六状态受控生命周期和追加式 Workflow Event；
- `none` 直接完成与 `manual` 提交验收两种策略；
- 直属子任务完成度派生、父任务自动进入待验收及失效后的撤回/重开；
- Submission 批次、四种 Artifact、受控文件下载、审计软删除及 Task 聚合硬删除；
- 所有真实页面的加载、空数据、错误、重试、版本冲突和草稿保留。

Task 不拥有 Inbox 的分诊/解决事实、Agent Runtime、远程协作/通知、AI 分析或知识库。Agent Run 只冻结并引用 Task 快照；运行、错误与重试链由本地 Agent 模块拥有，成功后仍通过本模块既有 Submission/Artifact/验收规则协调。Inbox 拆分复用 Task 创建约束；Task 生命周期、提交与验收命令调用 Inbox reconciliation。schema v23 在产出提交事务内消费显式 follow-up 标记，schema v24 在 block 生命周期事务内消费本次阻塞事实；schema v25 的本地扫描器消费当前 Task 截止时间事实。Inbox 仍拥有来源、关系和结清策略。父子层级不会创建 Inbox–Task 关系、不会继承或改写 `is_required`；Inbox 自动结清继续只看用户显式标为 required 的活动关系，与父任务自动验收彼此独立。

## 已实现状态

- AI 独立轨道通过 [ADR-029](../adr/029-agent-workspace-capabilities.md) 增加任务提议的逐项人工确认：创建、编辑/排期、开始/阻塞/解除/完成/取消/重开复用共享 Task 领域函数。模型只提出建议；确认事务重验版本、负责人、manual review 与父级/Inbox 规则，决定持久幂等。schema 072 只新增 AI 审批表，不改变 Task 表、状态和手动 API。
- H2-W3 增加同一精确计划日期组的 AI 顺序建议：模型只指定一个活动 Task 相对另一活动 Task 的前/后位置与双版本；服务端读取完整组、保留终态槽位、冻结整组指纹。人工确认时重验预览，并复用本模块原生完整集合/版本事务；超过 1,000 项、跨组、终态锚点和无变化拒绝。Task 仍拥有 `manual_order` 与版本事实，AI 不获得直接排序写权限，详见 [AI 顺序确认](ai-assistant.md#任务顺序的人工确认h2-w32026-09-21)。
- H5-A 另以 `agent_execution` 单消息授权和二次人工确认启动指定 Task 的受控文本 Run。完整预览冻结 Task/完成条件、责任、Agent/Adapter 与 Provider/config；确认事务提交后才启动，路由以 Task+Run 双身份定位。分派或普通 Task 操作不会隐式执行，Run succeeded 也不等于 Task done。
- H5-B 把成功文本结果落为明确处置：`succeeded + submitted` 才通过共享 Task output 命令创建 Submission/Artifact 并进入 `waiting_review`；`succeeded + retained` 只在 Run 保存正文且不改变 Task；瞬时登记失败先尝试持久化为 `running + pending`，落库后由启动或显式 retry 幂等恢复登记而不重跑模型。完成事务与首次 pending 写入同时暂时失败时，Worker 只在进程存活期间以内存保留有界结果并重试；首次落库前不承诺崩溃恢复。普通人工/AI 代录与受信 Agent 交付使用不同的服务端归属规则，见下文。
- H5-C 在同一 Run 生命周期上增加受控文件：仅当前 Task 的活动 file Artifact 和该 Task 当前 Project 的活动 Attachment 可作为输入；最多 4 个、单文件 64 KiB、总计 128 KiB，且必须是无 NUL 的 UTF-8。服务端在 prepare 与 prelaunch 两次复核身份、归属、大小和 SHA-256；持久快照只冻结来源引用、显示名、MIME、大小和哈希，不保存路径或正文。无文件且文本输出继续使用 v1；选择受控输入或文件输出才使用严格 v2。文件结果使用服务端生成名称并复用现有 Submission/Artifact 人工验收链；Run succeeded 仍不等于 Task done。

- Task 新建、详情、非生命周期编辑、确认删除、服务端分页/筛选/搜索/排序、原子批量事实/生命周期操作和计划组排序已接通真实 SQLite；任务页支持项目当前客户、精确计划日期、计划/截止日期范围、最多 20 个持久保存视图，且在单一精确日期的手动顺序列表中支持同状态拖拽。客户条件使用与 Project 表单/筛选共享的 `ClientSelect`。Project 关联使用 Task 新建/编辑、Tasks 项目筛选、批量目标项目和 Inbox 拆分任务共用的 `ProjectSelect`：两个选择器都固定每页 20 条、250 ms 服务端搜索、稳定分页、取消信号、当前选择保留和完整反馈，不再串行拉取全部 Client 或 Project。列表 API 另提供仅供实时截止风险入口使用的 `due_state=overdue|due_soon`；Today 以该条件读取完整分页结果。六状态看板复用同一服务端筛选、当前页分页、最多 100 项选择和详情入口，空列保持可见；跨列拖拽只映射既有生命周期命令并二次确认，不直接修改状态。统一搜索结果使用 `/tasks/:taskId`，刷新可恢复同一详情，不存在资源保留明确错误反馈。
- `todo / in_progress / blocked / waiting_review / done / cancelled` 六状态只通过显式命令变化；旧状态 PATCH 固定返回 410。
- `review_policy` 可在新建时选择 `none / manual`；既有 Task 只在 `todo` 且没有任何 Submission 历史时允许切换。
- Assignment 支持活动 assignee/reviewer、首次分派、改派、结束与分页历史。assignee 允许 active owner/person 或关联 enabled/execution_ready Adapter 的 active agent，reviewer 只允许 active owner。AI 独立轨道 H2-D 已接通相同三种责任命令的逐项人工审批及当前/历史查询，绑定 Task 版本、责任 ID 与完整预览；共享事务保留父任务待验收协调和历史，不启动 Agent。详见 [Actor 责任分派](actors.md#ai-独立轨道的责任分派h2-c2--h2-d)。
- manual Task 可从 `todo / in_progress` 提交摘要及最多 20 个文本、链接、结构化 JSON 或文件 Artifact；混合文件与非文件提交使用 multipart manifest。
- Task 详情“产出与验收”模块展示 manual 前置条件、草稿、当前批次、接受/返工、分页历史、Artifact 详情、文件安全下载和确认软删除。
- `none` 策略明确提示无需产出验收，可使用直接完成命令；manual 缺 assignee 或 owner reviewer 时显示具体前置条件，不提交无效请求。
- 所有 Task 写命令使用 Task `ETag / If-Match`。输出、审核、删除、Assignment、生命周期和事实编辑在 UI 上互斥，避免同时对旧版本写入。
- 输出冲突会刷新最新 Task，但保留摘要、文本、链接、结构化 JSON 和原浏览器 `File` 对象；用户查看最新事实后必须再次明确提交。取消未保存设置/草稿不会静默写服务端。
- 成功写入会立即失效 Task、Submission、Artifact、Assignment、Event、Project 和 Today 相关 Query，避免等待缓存自然过期。
- Task 时间线已覆盖策略变化、提交、接受、返工、等待验收时撤回、Artifact 删除及 v9 迁移回填文案。
- 绑定 Task 的 Focus Session 只有 stop→completed 才把精确秒数写入 `task_focus_totals`，再把新增完整分钟追加到 `actual_minutes`；余秒跨 Session 保留，cancel/interrupted 不入账，也不改变 Task 生命周期。
- Task 详情可按需分页读取该任务的终态 Focus Session；completed 显示为已计入工时，cancelled/interrupted 仅作审计展示，读取或翻页不会修改 Task 草稿、版本或生命周期。
- Task 存在 active/paused/recovery_pending Focus Session 时，硬删除返回 `409 TASK_HAS_OPEN_FOCUS_SESSION`；Session 进入终态后可删除 Task，历史 Session 的 `task_id` 自动置空。
- Task 存在任一活动 Inbox 关系时，硬删除返回 `409 TASK_HAS_ACTIVE_INBOX_RELATIONS`，不会移动 Artifact 文件或删除聚合；用户带原因软解除后才可删除。已解除历史关系的实时 `task_id` 随删除置空，但原 Task UUID/标题快照继续保留。
- Task 存在 `queued` 或 `running` Agent Run 时，硬删除返回 `409 TASK_HAS_ACTIVE_AGENT_RUN`，不会依赖外键静默级联活动执行事实。`running + pending` 已完成模型执行，须先重试产出登记；其他活动 Run 须先取消或等待终态。只有 Run 全部终态后才允许删除，届时历史 Run 随 Task 聚合级联清理。
- 父任务只统计直属子任务。只要至少有 1 个非取消直属子任务且它们全部为 done，并且父任务是 todo/in_progress、策略为 manual、存在 active owner/person assignee 和 active 内置 owner reviewer，系统才会创建无 Artifact 的 `child_rollup` Submission，并把父任务最多推进到 waiting_review；它不会自动 accept 或结束 Assignment。
- manual Submission 历史、既有 pending Submission，或曾被 `request_changes` 的 `child_rollup` 会阻止系统覆盖或重复提交。自动门禁或子任务完成条件失效时，pending 系统批次撤回；已经被 owner 接受的父任务若随后因直属子任务失效而不再满足条件，则系统重开父任务并沿祖先链协调。

## 数据模型与约束

### TaskSavedView（schema v17）

`task_saved_views` 独立保存筛选定义，不把视图塞入固定四模块 `app_settings`，也不复制任务结果。名称 1–80 字符、大小写不敏感唯一；定义 JSON 最大 16 KiB、`schema_version = 1`、`version >= 1`，当前每个工作区最多 20 个。定义只包含搜索、状态、优先级、类型、项目、客户、标签、精确/范围计划日期、截止范围和排序；不保存页码、当前选择、展开状态或查询结果。

API 提供列表、新建、`If-Match` 更新和带 `confirm=true` 的删除。服务端按与 Task 列表一致的枚举、UUID、日期、范围和排序白名单规范化；计划精确值与计划范围互斥。保存视图引用的 Project、Client 或 Tag 删除后不级联删除视图，应用时按当前事实自然返回空或剩余结果，避免用历史快照伪造现存关系。Project 候选继续消费 `GET /api/v1/projects`：默认排除归档项，所有排序追加 `id ASC`，列表总数与当页数据在同一只读事务读取；这只是既有 API v1 的稳定性收口。

### AI 既有任务验收策略切换（H2-X，2026-09-21）

既有 Task 现在可通过 `task.update` 提议 `review_policy=none|manual`，不必回到原生详情才能为 Agent 产出准备人工验收。需要保存会话、本条 `work+actions`、真实 Task ID/version 和独立人工确认；这不是任务验收、分派或执行许可。

- `workspace_get(task)` 在已有 `work` 读取范围返回 `review_policy` 与 `review_policy_change_allowed`。后者在同一查询中按 `todo` 且没有任何 Submission 历史计算，只代表当时是否允许**切换**；已撤回、接受或返工的历史也会锁定。它不返回历史数量、提交 ID、摘要、产出或验收正文，不获得 `outputs` 权限。显式手选 Task 上下文使用同一投影，发送预览显示中文字段；原版本/供应商检查与正文预算不变。
- 提议和确认均复用原生 `validateTaskReviewPolicyChange`；实际切换只允许 `todo`、无任何 Submission 历史，确认还须 Task 版本匹配。与当前值相同沿用原生更新语义，不触发策略变更事件或策略导致的父任务协调，不能用它绕过已有提交。AI 规范化策略首尾空白，拒绝 null/未知枚举及错误类型；带策略的更新也拒绝 schema 不允许的指针字段 null，合法的日期/项目/父任务/预计工时清空规则不变。批量任务命令不新增策略切换。
- 确认卡冻结 `review_policy`、状态的前后值，以及 `after.subtask_total/subtask_completed/subtask_cancelled/parent_rollup_gates_ready/will_request_parent_review`。计数与责任条件来自原生只读规则；实际 `none→manual` 且至少一个非取消直属子任务全部完成、活动 owner/person assignee 与 active owner reviewer 均满足时，原生事务可生成 `child_rollup` 并让当前 Task 进入 `waiting_review`。这是自动发起待验收，不是自动验收通过或完成；卡片明确披露，不能笼统承诺“不改状态”。同值设置不会因已有完成子任务重新触发该协调。
- 确认时重算并逐字比较预览，子任务计数/完成度或责任条件变化即使未增加 Task version，也拒绝旧卡并返回 `AI_ACTION_PREVIEW_CHANGED`。状态/Submission 锁返回 `TASK_REVIEW_POLICY_LOCKED`，版本变化保留 `VERSION_CONFLICT`；界面说明原因，不自动换参、强行确认或重试。真实写入继续走 `updateTaskInTransaction`，策略事件、父链/Inbox 协调与审批决定共同提交或回滚，重复确认只回读原结果。
- `manual→none` 的卡片明确说明后续可以直接完成而无需人工验收；不能为绕过验收而替用户决定此修改。改成 manual 不自动新增 reviewer/assignee，也不启动 Agent。应先等待策略确认，再读取当前责任/版本/执行资格，按需独立确认分派与 Agent 执行；产出提交后仍需 owner 另行验收。

确定性证据：`ai_task_review_policy_test.go`、`ai_task_review_policy_context_test.go`、`ai_task_review_policy_harness_test.go`、`aiTaskReviewPolicyActions.test.ts`、`AiWorkspaceActions.test.tsx` 和主对话上下文预览测试。双协议旅程使用本地模拟 Provider、真实审批和交付事务，执行器完成由受控事件替代，不作为真实 Provider/原生 Runner 验收。API v1/schema 076、Task 状态机、权限集合及原生可编辑条件不变；没有新路由或迁移。

### AI 保存视图读取与命令（H2-U，源码与隔离测试已实现）

结构化任务读取现已由下述 H2-W 接通：保存视图本身仍只保存筛选定义，模型可读取定义后显式调用 `workspace_tasks` 查询匹配任务。

- `work` 授权下新增只读 `workspace_task_views({view:"list"|"view",id?,query?,limit?,offset?})`：列表按名称不敏感排序分页（1–20/页、offset ≤1000、总量 ≤20），详情要求 canonical UUID，返回名称、完整筛选定义、版本、更新时间和 `/tasks` 路由；不含任何任务正文、任务 ID 或查询结果。视图是本地筛选预设，读取它不会应用筛选，也不会授予任务写权限。
- 任务页保存视图控件在选中具体视图后提供“交给智能体”。入口只暂存视图 ID、当前显示名称、`/tasks` route 和固定提示词，推荐 `work+actions`；进入 `/ai` 后仍需用户点击“带入问题并选择权限”并人工发送。提示词要求模型先用 `workspace_task_views({view:"view",id})` 读取真实名称、定义、版本和更新时间，再解释筛选条件或起草 `task_view.*` 建议；交接本身不自动应用筛选、不读取筛选结果任务、不创建/更新/删除保存视图，也不修改任何 Task。
- `work+actions` 下新增 `task_view.create/update/delete` 三种提议，均无模型直写工具。创建要求现实名称与完整定义；修改绑定 `task_saved_view_id` 与当前版本且至少给出 name/definition 之一；删除要求空 `changes` 并须人工另行勾选 `confirm_task_view_delete`。定义字段沿用 Task 列表白名单：q/status/priority/kind/project_id/client_id/tag_ids/planned_date/planned_from/planned_to/due_from/due_to/sort；`planned_date` 与计划范围互斥，范围起止不得倒置，UUID 与日期格式在提议阶段即校验，未知字段被严格拒绝。
- 提议阶段即复用原生规范化（`normalizeTaskSavedViewDefinition`）与名称规则，并冻结人工可读预览：视图名称、全部筛选字段、真实项目/客户/标签名称（引用已删除时保留 UUID 而不伪造名称）、旧值与新值以及下一版本。确认事务与原生 API 共用 `createTaskSavedViewInTransaction/updateTaskSavedViewInTransaction/deleteTaskSavedViewInTransaction`，名称大小写不敏感唯一、20 个上限、`If-Match` 等价版本检查和 `schema_version` 继续由同一领域命令负责。
- 确认先重算并逐字比较完整预览，视图被改名、改筛选或删除时返回 `AI_ACTION_PREVIEW_CHANGED`，旧卡不执行也不自动换参；名称冲突返回 `TASK_SAVED_VIEW_NAME_EXISTS`，超过 20 个返回 `TASK_SAVED_VIEW_LIMIT_REACHED`，缺失删除同意返回 `422 TASK_VIEW_DELETE_CONFIRMATION_REQUIRED`，且确认前零写入。回执 result_id 是视图 ID（删除时为被删 ID），版本为新版本（删除为最后已知版本），route 统一 `/tasks`：任务页没有按视图 ID 的深链，视图需要用户在筛选栏自行选用，不声称自动应用。
- 前端严格解析三种预览（动作与目标字段互斥、定义字段白名单、changes 与 preview 逐字段一致、before/after 与 typed preview 对齐），卡片显示筛选条件并单独渲染“已删除视图不影响任何任务”的说明；确认后刷新任务视图与任务列表缓存，不改变任何 Task、不自动应用筛选。确定性证据：`ai_task_view_actions_test.go`、`aiWorkspaceActions.test.ts`、`AiWorkspaceActions.test.tsx`；真实 Provider 措辞与桌面交互仍需单列验收。

### AI 结构化任务筛选（H2-W，2026-09-20）

`work` 单消息授权下新增只读 `workspace_tasks({filters?,limit?,offset?})`，用于按任务页同一规则查找“本周到期的高优先级任务”、某客户关联项目中的任务或未排期任务。普通关键词搜索仍保留；此工具直接在服务端过滤排序，避免模型遍历无关任务再逐条读取详情。H2-W12 另加互斥的 `workspace_tasks({task_ids:[...]})` 精确读取模式，详见上节。

- `filters` 复用保存视图的 `q/status/priority/kind/project_id/client_id/tag_ids/planned_date/planned_from/planned_to/due_from/due_to/sort`，另支持任务页既有 `planned_state/due_state/parent_task_id/root_only`。标签最多 20 个，ID/日期/排序严格校验；精确计划日期与计划范围互斥，未排期条件不能同时指定计划日期；`due_state=overdue|due_soon` 不与截止范围混用，只允许活动状态。父任务与根任务条件按原生 AND 语义组合，不推断为其他任务。
- 参数总长最多 8 KiB；外层及 `filters` 必须使用 schema 的精确字段名，拒绝大小写别名、重复键（含转义后等价键）、显式 null、额外字段和尾随数据，不能将无效条件静默降级为无筛选查询。
- 复用 `normalizeTaskSavedViewDefinition`、`applyTaskFilters`、`applyTaskSort`，截止风险和 `as_of` 使用同一个读取时钟。每页 1–20 条，offset 最大 1000；同一只读事务读取总数与当前页，`has_more/next_offset/window_limited` 明示分页和窗口限制。不同页是各自的实时快照，不等于冻结的批量操作对象集合。
- 关键词 `q` 与任务页一致，在本地匹配标题和描述；截止范围按已存 UTC 日期筛选，临期为服务端当前时刻起未来 24 小时，不能当作未明确时区的本地“今天”。返回任务身份、最多 200 字标题、状态、优先级、类型、计划/截止时间、版本、更新时间和真实 Task 路由；不返回描述、完成标准、客户名称/联系方式、产出、附件或路径。`client_id` 只按任务当前 Project 的客户关系过滤，仍在 `work` 范围；客户资料需独立客户查询权限。
- 无保存会话要求、无写入、无新 scope 或迁移；任务详情仍使用 `workspace_get`，实际排期/批量修改仍需 `actions` 提议及人工确认。工具在后续消息不继承权限。确定性证据见 `ai_tasks_tool_test.go` 及既有最大 scope 的 Provider 请求预算回归。

### AI 批量任务命令（H2-V，源码与隔离测试已实现）

- `work+actions` 下新增 `task.batch_update` 提议，用于同一变更同时作用到 1–20 个真实 Task。模型必须先读取每个 Task 的 canonical UUID 与当前版本，并在 `changes.items` 与 `changes.expected_versions` 中按同一顺序给出完整集合；不得提供顶层 `task_id/project_id/expected_version`，也不能把当前页或搜索结果冒充完整选择。
- 支持的 `changes.batch_action` 为 `set_project / set_planned_date / add_tags / remove_tags / start / block / unblock / complete / cancel / reopen`，H2-W9 另增加 `set_assignee / clear_assignee / set_reviewer / clear_reviewer`，H2-W10 另增加 `set_priority / set_due_date`。`set_priority` 必须给 `P0–P3`，`set_due_date` 必须给含时区偏移的 RFC3339 时间或 `null`；`set_project` 必须显式给 `project_id`（UUID 或 `null`），`set_planned_date` 必须显式给 `planned_date`（YYYY-MM-DD 或 `null`），标签动作必须给 1–20 个真实 `tag_ids`，block/cancel 和全部责任动作必须给原因；责任设置另需真实 `actor_id`，其余不接受附加字段。项目、标签、生命周期与责任门禁在提议阶段即校验，标签上限会提前阻止生成可确认卡。
- 提议预览逐任务冻结标题、版本、变更字段、前值和后值，block/cancel 与责任动作额外显示原因，责任动作还显示 Actor 身份；确认时重算完整预览并逐字比较，任何任务版本、项目/标签、生命周期或责任事实变化都会拒绝旧卡。原项目/标签/生命周期动作沿用原生批量接口的整体读取、版本校验、原子写入及父链/自身进度延迟协调；H2-W9 责任动作在相同审批事务内复用原生单项 Assignment 领域命令并整体回滚。成功回执使用 Proposal ID/version 1，route 固定 `/tasks`，因为整批没有单个任务目标。
- 前端把批量动作作为一等审批卡处理：严格拒绝伪造单任务目标、错序 items/version、缺失显式 null、错 count 或非 proposal 回执；卡片显示逐任务前后值，确认后刷新 Task/Today/Project/Inbox 等任务聚合缓存。确定性证据：`ai_task_batch_actions_test.go`、`aiWorkspaceActions.test.ts`、`AiWorkspaceActions.test.tsx`；真实模型是否稳定选择批量而非多张单项卡仍需实机观察。

### Task

| 字段                         | 当前约束                                                                 |
| ---------------------------- | ------------------------------------------------------------------------ |
| `status`                     | `todo / in_progress / blocked / waiting_review / done / cancelled`       |
| `review_policy`              | `none / manual`；策略改变只允许 `todo` 且 Submission 历史为 0            |
| `current_submission_id`      | 指向同一 Task 的最新 Submission；接受、返工、取消后保留，reopen 清空     |
| `submitted_at / reviewed_at` | 当前/最近一次提交流程的快速状态时间；reopen 清空，历史以 Submission 为准 |
| `blocked_from_status`        | block 时由服务端保存，unblock 只能恢复这个状态                           |
| `version`                    | 任一影响 Task 聚合呈现或决策的写入递增，用作 ETag 和乐观锁               |

Task 创建仍只允许 `todo`；非 `todo` 创建返回 `LIFECYCLE_COMMAND_REQUIRED`。状态不能经通用 PATCH 修改。

### TaskSubmission

| 字段                                                 | 含义                                                                                                    |
| ---------------------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| `id / task_id / sequence`                            | UUID、所属 Task、Task 内从 1 递增且唯一的批次序号                                                       |
| `status`                                             | `pending_review / accepted / changes_requested / withdrawn`；每个 Task 至多一条 pending                 |
| `origin`                                             | `manual / child_rollup`；人工/AI 代录和受信 Agent Run 交付均为 manual，系统父任务汇总固定 child_rollup  |
| `summary`                                            | 可为空但最长 10,000 字符；manual 提交时 summary 与 Artifact 至少一个存在                                |
| `submitted_by_actor_id / submitted_at`               | 人工/AI 代录为内置 owner；受信 Agent Run 交付为冻结 agent；child_rollup 为内置 system；同时保存提交时间 |
| `reviewed_by_actor_id / reviewed_at / review_reason` | 接受或返工时由内置 owner 记录；返工原因必填                                                             |
| `withdrawn_by_actor_id / withdrawn_at`               | 用户取消 manual 待审时为内置 owner；自动批次失效撤回时为内置 system                                     |
| `is_inferred`                                        | schema v9 从无歧义旧 manual 状态回填的批次为 true；child_rollup 必须为 false                            |

Submission 的 Task、序号、来源、摘要、提交人、提交时间和 inferred 标记不可修改；只有 `pending_review` 可一次性转为 accepted、changes_requested 或 withdrawn。`child_rollup` 必须由内置 system 创建、`is_inferred = 0` 且 Artifact 数为 0；Task 仍存在时禁止直接硬删 Submission。

### TaskArtifact

| 字段                              | 含义与约束                                                                                                                               |
| --------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| `position`                        | 批次内从 1 开始且唯一，保持客户端提交顺序                                                                                                |
| `submission_status`（API 派生）   | 必填 `pending_review / accepted / changes_requested / withdrawn`，由父 Submission JOIN 得出；不是第二份可写状态                          |
| `storage_kind`                    | `text / link / structured / file`                                                                                                        |
| payload                           | text→`content_text`，link→`reference_url`，structured→`structured_json`，file→受控 `relative_path = objects/<artifact-id>`；四者严格互斥 |
| `name`                            | trim 后 1–255 个安全字符，不允许控制字符                                                                                                 |
| `mime_type / size_bytes / sha256` | 仅文件需要；SHA-256 为 64 位小写十六进制                                                                                                 |
| `requires_followup`               | 人工标记需要后续动作；true 时同事务投影一个稳定去重 Inbox Item，不自动生成 Task                                                          |
| `produced_by_actor_id`            | 人工/AI 代录取提交瞬间 active assignee；受信 Agent Run 交付取重验后的冻结 agent；客户端不能指定                                          |
| `recorded_by_actor_id`            | 人工/AI 代录固定内置 owner，表达“我代录”；受信 Agent Run 交付固定内置 system                                                             |
| `integrity_status`                | `unverified / verified / missing / mismatch`；unverified 的检查时间必须为空，其他状态必须有检查时间                                      |
| `deleted_*`                       | owner 软删除时间、操作人和 1–1,000 字符原因，三者同为空或同为非空                                                                        |

Artifact 的事实与 payload 创建后不可编辑；仅完整性检查状态和首次软删除元数据允许受控变化。Agent 交付还以 `workflow_events.agent_run_id` 和 Run 的精确 `submission_id/artifact_id` 双向关联；system 是该事件 actor。两种 manual 来源都只进入 `pending_review`，owner 仍负责接受、返工、撤回与删除，绝不自动 accept/complete。H5-C 文件输出由服务端命名并作为 file Artifact 进入同一人工验收链；schema v75 的 `result_text` 仍保留有界重复字节，只用于完成事务失败后的登记恢复，不是第二份可编辑产出；schema v76 仅增加会话计划修订。Task 仍存在时禁止直接硬删 Artifact；硬删除整个 Task 聚合时由外键级联清理数据库成员。

### H5-C 受控文件边界

- 候选集只来自当前 Task 未软删的 file Artifact，以及该 Task 当前 `project_id` 下未软删的 Project Attachment。跨 Task、历史 Project、软删除项、非文件 Artifact 与任意本机路径都不进入候选集。
- 原生 Task 入口通过 `GET /api/v1/tasks/:id/agent-run-files` 读取候选并在创建 Run 时提交精确来源引用；存在输入文件时必须同时提交 `confirm_file_access=true`，以及人工确认时所见 Provider 的 `version / config_version / kind`。这项确认与“启动 Agent”确认相互独立，不能由模型文本或普通执行确认替代；无输入时不得携带文件确认字段。
- 创建事务在插入 Run 与 `agent_run_queued` 事件之前重读 Provider，并精确比较上述三项身份。未分派 Task 可在同一请求携带真实 agent Actor 和刷新后的 Task version；Sidecar 先复用 Assignment 领域命令，再准备并创建 Run，全部仍处于同一事务。任一漂移返回 `409 AGENT_RUN_IDENTITY_CHANGED` 并整笔回滚，因此不留下 Task 版本变化、Assignment、分派/排队事实、Run、幂等记录或 launch；已有匹配 assignee 的旧请求形态保持兼容。原生创建 Run 的幂等身份包含规范化 Task UUID，同 key/body 在不同 Task 上各自创建，同一 Task 才执行 replay/摘要冲突判断；旧模板范围记录仅在关联 Run 属于当前 Task 时兼容重放。前端检测到身份变化或收到该 409 后会清除旧文件确认，要求用户重新核对 local/remote 离机边界。
- 最多 4 个输入，单个最多 64 KiB、总计最多 128 KiB，只接受无 NUL 的 UTF-8。服务端在创建/prepare 时读取受控对象，并在子进程启动前再次按冻结引用核对来源可用性、Task/Project 归属、大小与 SHA-256；期间 Task 改绑 Project、Assignment/Provider/Task 漂移或文件字节漂移都会拒绝旧启动。
- Durable v2 快照与 retry 只冻结来源身份和 output contract，不持久化本机路径或输入正文。执行协议会在内存匿名管道中传递受限字节，但不会把真实路径交给 Runner。无文件且 `output_kind=text` 的 no-op 字段会规范化回 v1；只要选择文件或要求 `output_kind=file` 才生成严格 v2，格式损坏的活动 v2 快照按 fail closed 处理。
- queued/running v2 Run 引用的 Task Artifact 不允许软删除；引用的 Project Attachment 不允许软删除，其所属 Project 也不允许永久删除，均返回 `409 AGENT_RUN_FILE_REFERENCED`。用户须先取消 Run 或等待其终态，系统不会静默改写冻结输入。
- 本能力不是任意 filesystem/Shell/browser/Git/terminal 工具授权。真实 Provider、桌面交互和断电恢复继续作为确定性门禁之外的独立验收。

schema v9 的 `artifact_deletion_tombstones` 保存 file Artifact ID、Task ID、固定相对路径、size、SHA-256、删除范围 `artifact / task` 与时间。单 Artifact 软删或 Task 聚合硬删会在同一事务写入；记录不可修改或删除，也不引用将被级联删除的 Task/Artifact，因此启动恢复能区分已授权删除与未知候选。

### WorkflowEvent

事件是追加式事实，包含 Task 聚合、actor、可空 assignment/submission/artifact/agent_run 关联、request ID、前后 JSON 快照、`command_seq` 和创建时间。关联对象随 Task 聚合硬删除时外键 ID 可安全置空，快照继续保留；其余更新/删除由 trigger 拒绝。

D2 新增事件：

- `task_review_policy_changed`
- `task_output_submitted`
- `task_review_accepted`
- `task_changes_requested`
- `task_submission_withdrawn`
- `task_artifact_deleted`
- `task_parent_review_requested`（system 创建 child_rollup 并发起验收）
- `task_parent_review_withdrawn`（system 因子任务或父任务门禁失效撤回）
- `task_parent_reopened`（已接受父任务因子任务条件失效而由 system 重开）
- `migration_submission_backfill`（system Actor，schema v9 inferred 历史）

## 状态机

```text
todo ──start──> in_progress
  │                 │
  ├──── block ──────┼──> blocked ──unblock──> 服务端记录的来源状态
  │                 │
  ├─ complete ──────┴──> done                 （仅 policy none）
  │
  └─ submit-output ─────> waiting_review      （仅 policy manual）
                             ├─ accept ──────> done
                             ├─ request_changes -> in_progress
                             ├─ block/unblock -> blocked/waiting_review
                             └─ cancel ──────> cancelled + Submission withdrawn

todo / in_progress / blocked / waiting_review ──cancel──> cancelled
done / cancelled ──reopen──> todo

直属非取消子任务至少 1 个且全部 done + 父任务 manual/责任门禁
  ── system child_rollup ──> waiting_review ── owner accept ──> done
                  │                    │
                  └─ 门禁/子任务失效 ──┴─> withdrawn + in_progress
accepted parent done + 子任务失效 ── system reopen ──> todo
```

规则：

- start 需要 active assignee。
- block/cancel 原因必填；unblock 不能由客户端选择目标状态。
- 每次成功 block 都按阻塞后的 Task version 生成独立 `task:<task-id>:blocked:<version>` Inbox 来源；幂等重放不重复投影，unblock 不删除或自动解决该人工待处理项。
- `none` 可从 todo/in_progress 直接 complete；manual 必须提交并经 owner accept。
- 用户 `submit-output` 只允许 manual 的 todo/in_progress，且同时需要 active assignee 与 active owner reviewer；人工与 AI 审批入口按 owner 代录归属。受信 Agent delivery 在成功 finalize/recovery 内部复用同一领域命令，但还须重验冻结 agent/Assignment/Task 版本，并按 agent/system 归属写入。
- accept 在同一事务完成 Task 并结束所有活动 Assignment；request_changes 保留 Assignment 并返回 in_progress。
- cancel 结束活动 Assignment；若当前是 waiting_review（包括从该状态 block 后），还会先把 pending Submission 撤回。
- accept、request_changes、cancel 保留 `current_submission_id` 作为最近批次指针；reopen 清空指针和快速时间字段，但保留 Submission、Artifact 与 Event 历史，也不恢复旧 Assignment。
- 自动父任务判定只看 `parent_task_id` 直接指向该父任务的子任务；cancelled 不进入分母，非取消数必须大于 0，且每一项都必须 done。孙任务只会先影响自己的直接父任务，再通过祖先协调逐层传播，不能绕过中间父任务。
- 自动发起还要求父任务当前为 todo/in_progress、`review_policy = manual`、active assignee 为 owner/person、active reviewer 为 builtin owner；系统只创建固定摘要的无 Artifact `origin=child_rollup` Submission 并进入 waiting_review，最终仍由 owner review。
- 任何 manual Submission 历史都阻止 child_rollup；系统也不覆盖现有 pending Submission，且 owner 对 child_rollup 要求返工后，该 `changes_requested` 历史阻止自动重提。manual 和 changes_requested 均保留人的明确决策优先级。
- pending child_rollup 的子任务条件或 review policy/Assignment 门禁失效时由 system 撤回。普通父任务回到 in_progress；若父任务正 blocked 且 `blocked_from_status=waiting_review`，状态和阻塞原因/时间保持 blocked，只把 `blocked_from_status` 更新为 in_progress。
- child_rollup 已 accepted 且父任务 done 后，如直属子任务条件失效，system 把父任务重开为 todo，清理当前快速提交/审核指针和终态事实，但保留 accepted Submission/Event；旧 Assignment 不自动恢复。系统沿祖先链继续协调。

## API 契约

### Task 与生命周期

| 方法   | 路径                                                                  | 关键约束                                                                                                                                               |
| ------ | --------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| POST   | `/api/v1/tasks`                                                       | 仅 todo；支持 `review_policy`; 可选稳定幂等键                                                                                                          |
| POST   | `/api/v1/ai/messages/:id/task-confirmation`                           | 用户明确确认；共享 Task 创建校验与事务，原子关联消息，消息 ID 保证跨刷新幂等                                                                           |
| GET    | `/api/v1/tasks/:id`                                                   | 完整 Task、关系、版本和 `ETag`                                                                                                                         |
| PATCH  | `/api/v1/tasks/:id`                                                   | `If-Match`；不写 status；策略变化仅 todo+无历史                                                                                                        |
| DELETE | `/api/v1/tasks/:id`                                                   | `If-Match`；先拒绝开放 Focus、活动 Inbox 关系/来源及 queued/running Agent Run；活动 Run 返回 `409 TASK_HAS_ACTIVE_AGENT_RUN`，全终态后才硬删并级联 Run |
| PATCH  | `/api/v1/tasks/batch`                                                 | 1–100 项、逐项 expected version；事实或六种生命周期命令同事务，任一失败整批回滚                                                                        |
| POST   | `/api/v1/tasks/:id/{start\|block\|unblock\|complete\|cancel\|reopen}` | `If-Match`；可选稳定幂等键；显式状态机                                                                                                                 |
| GET    | `/api/v1/tasks/:id/events`                                            | 默认 50/最大 100；返回 Task ETag 与 `meta.task_version`                                                                                                |
| GET    | `/api/v1/tasks/:id/agent-run-files`                                   | H5-C 受控输入候选；只列当前 Task 活动 file Artifact 与当前 Project 活动 Attachment，不返回路径或正文                                                   |

批量生命周期使用与单任务命令相同的转换矩阵和领域副作用。服务端先读取并校验完整选择集的版本、状态、active assignee 和 review policy，确认全部可执行后才开始写入；complete/cancel 会结束活动 Assignment，waiting-review cancel 会撤回当前 Submission，block 会创建对应 Inbox 来源，每个 Task 都追加独立 Workflow Event。创建、改绑/解除父任务、删除，以及单条/批量 complete/cancel/reopen 和 review accept 都会在同一事务协调受影响的父任务与祖先；start/unblock 还会重算任务自身，使迁移前已经满足子任务条件但未回填的父任务可在后续明确写命令中进入待验收。批量会去重父任务并返回协调后的最终 version。阻塞/取消使用一份 1–1,000 字符统一原因，前端对六种生命周期命令均要求二次确认。批量 API 依靠 expected version 保证重试可判定，不提供单任务命令的 Idempotency-Key 重放响应。

### AI 建议的任务确认（独立轨道）

AI 只生成建议，不拥有任务创建成功的事实。用户在确认表单提交现有 Task 创建字段后，`POST /api/v1/ai/messages/:id/task-confirmation` 调用共享 `createTaskInTransaction`，在同一事务中完成标题、日期、标签、项目和父级校验、任务创建、父级协调及 AI 消息关联。返回 `{data:{task,message}}`，其中 message 是完整服务端快照。普通 Task 创建当前没有 `task_created` 事件；父级协调复用已有事件，不为 AI 另造业务事件或绕过 Assignment、review policy 和生命周期规则。

消息 ID 是稳定的确认命令身份：相同载荷重复确认返回已有结果，不再创建任务；不同载荷冲突；已关联任务删除后返回 410，不生成替代任务。响应丢失时可读取会话消息或重试同一确认恢复结果。未确认明确显示“尚未创建”；确认后名称和入口使用 `task_id / task_title_snapshot`，模型文字不能作为成功凭证。旧 `POST /ai/messages/:id/task` 仅保留静态挂接兼容，新 UI 不再先创建再挂接。

### AI 任务标签创建与整组替换（H2-L，源码与隔离测试已实现）

- 保存会话单条消息明确授权 `work+actions` 后，`task.create/update` 可在 `changes.tag_ids` 中携带最多 20 个标签；ID 必须来自 `workspace_task_options({type:"tag"})`，不能由名称猜测。`tag_ids` 表示任务的完整目标标签集合，空数组会移除全部标签，不是追加或移除单个标签的隐式命令。
- 新建沿用 `createTaskInTransaction`，创建 Task 与写入 `task_tags` 在同一事务；修改沿用 `updateTaskInTransaction`，绑定 Task 当前 `expected_version`，版本递增、标签存在性检查和整组替换保持原生工作台语义。没有新增标签事实、副本、API 或迁移。
- Proposal 保存规范 UUID 集合；本地确认卡仅展示按稳定顺序读取的标签名称，不把 UUID 当成人工可读信息。确认事务重新生成完整预览；标签被删除/重命名或 Task 版本变化会拒绝旧卡，不自动换成同名标签。业务写入、审批决定与审计任一步失败都会整体回滚。
- 前端严格校验标签名称数组与 ID 数量边界，明确提示“完整替换”和空集合效果；确认后复用 Task/Today/Project/Inbox/搜索等现有缓存失效。多任务追加/移除标签由 H2-V 的 `task.batch_update` 承载；标签自身写入见 H2-O。

### AI 标签管理（H2-O，源码与隔离测试已实现）

- 保存会话在本条消息明确授予 `work+actions` 后，可用 `tag.create/update/delete` 提议创建标签、改名/改色或永久删除。`workspace_task_options(type=tag)` 现返回真实 `id/name/color/version`；模型不得按名称猜 ID，创建要求 1–50 字名称与 `#RRGGBB`，修改至少包含 name/color 之一。
- 三种动作都只创建 Proposal，不存在模型直写工具。确认事务与原生 Tag API 共用 `createTagInTransaction/updateTagInTransaction/deleteTagInTransaction`；大小写不敏感的名称唯一性、版本冲突、颜色规范化、关联 Task 版本递增继续由同一领域命令负责，不另建 AI 写入旁路。
- 删除卡冻结标签名称、颜色、版本与解除关联 Task 数，必须额外勾选 `confirm_tag_delete`。确认时即使 Tag 版本未变，也会重算并逐字比较完整预览；关系数量变化返回 `AI_ACTION_PREVIEW_CHANGED`，不会批准新增影响。删除只移除 Tag/TaskTag 并递增受影响 Task 版本，不删除任务或改变任务状态，成功回执导航任务页且无应用内撤销。
- 前端严格解析三种预览、拒绝额外目标/字段/非 canonical UUID/小写颜色及伪造影响；确认后刷新 Tag、Task 和 Focus 标签报告读模型。确定性证据：`ai_tag_actions_test.go`、`aiWorkspaceActions.test.ts`、`AiWorkspaceActions.test.tsx`。真实 Provider 措辞与桌面交互仍需单列验收。

证据：[提议与共享事务](../../services/sidecar/internal/api/ai_workspace_actions.go)、[隔离创建/替换/迟到删除测试](../../services/sidecar/internal/api/ai_workspace_actions_test.go)、[前端协议测试](../../apps/web/src/api/aiWorkspaceActions.test.ts)、[确认卡测试](../../apps/web/src/components/AiWorkspaceActions.test.tsx)。真实模型选对标签的质量仍需单独验收。

### AI 子任务创建与父任务调整（H2-M，源码与隔离测试已实现）

- 保存会话单条消息明确授权 `work+actions` 后，`task.create/update` 可在 `changes.parent_task_id` 中使用由 `workspace_search/get(task)` 读取的 canonical Task UUID；修改时传 `null` 表示移到顶层。不能按标题猜 ID，也不能用该字段修改父任务本身的内容或状态。
- Proposal 保存真实父任务 ID，但本地确认卡只展示原/新父任务标题，不把 UUID 当作人工可读信息。创建、改绑和解除都在确认前重验目标 Task 版本、父任务存在性和递归父链；自指或把后代设为父级返回 `TASK_PARENT_CYCLE`。
- 执行复用 `createTaskInTransaction/updateTaskInTransaction`。原生父链协调会在同一事务重新计算原父级与新父级的 child-rollup 待验收、撤回或重开事实；审批决定和审计失败同样整体回滚，不留下半完成层级关系。
- `task.create/update` 现在都在确认时重新生成并逐字比较完整预览。父任务改名/删除、循环变化、标签改名/删除或目标 Task 并发更新会拒绝旧卡，要求重新查询和提议；不会把旧标题静默绑定到另一个 ID。

### AI 永久删除任务（H2-N，源码与隔离测试已实现）

- 保存会话在本条消息明确授权 `work+actions` 后，可用真实 `task_id`、`expected_version` 和空 `changes` 提议 `task.delete`。模型没有确认工具，也不能把删除混入 update 或用删除代替 complete/cancel；人工必须在本地卡片另行勾选永久删除同意。
- 预览冻结任务标题以及 `task_deleted`、直属子任务移到顶层、提交批次、全部 Artifact、当前受控 file Artifact、Assignment、Agent Run、Focus Session 解除关联、Inbox 关系解除、三类 Task 来源协调和 TaskTag 删除数量。确认时在写事务内重算并逐字比较；即使 Task 行版本未变，关系数量变化也返回 `AI_ACTION_PREVIEW_CHANGED`，业务与审批都不写入。
- 执行与原生 DELETE 共用 `task_delete_command.go`：先复核 Task 版本及现有活动 Run/待登记产出、开放专注、活动 Inbox 关系/来源和 Content Item 关联门禁，再把受控文件移入 trash、删除聚合并协调父链。事务或审批事件失败会恢复文件，提交成功后才清理 trash；直属子任务由数据库规则移到顶层，Submission/Artifact/Assignment/Run/TaskTag 按既有外键级联，Focus/Inbox 历史只解除 Task 关联。
- confirmed 回执保存已删除 Task ID 与最后版本，导航固定为 `/tasks`；重复确认幂等回读，不伪造仍可打开的详情。审批/Workflow Event 是操作历史，备份或应用外副本不受删除影响；应用内没有撤销。

证据：[提议与确认实现](../../services/sidecar/internal/api/ai_workspace_actions.go)、[共享删除命令](../../services/sidecar/internal/api/task_delete_command.go)、[层级/删除/迟到变化测试](../../services/sidecar/internal/api/ai_workspace_actions_test.go)、[前端严格协议测试](../../apps/web/src/api/aiWorkspaceActions.test.ts)、[人类可读确认卡测试](../../apps/web/src/components/AiWorkspaceActions.test.tsx)。API v1 / schema 76 不变；真实 Provider 选取正确目标及桌面体验仍需单独验收。

### AI 提交批次读取（H2-E1，源码与隔离测试已实现）

单条消息明确授权 `work+outputs` 后，可用 `workspace_task_submissions` 查看任务提交事实；`actions`、客户或财务范围不自动包含此能力。支持临时会话，不新增 schema 或业务 HTTP API，不提交产出、不通过验收，也不启动 Agent。

- H4-V：Task 详情提供专用“交给智能体”，只暂存 `/tasks/<id>`、canonical Task ID、当前显示标题/状态和固定提示词；不复制描述、产出摘要、文件正文、任务列表、会话或授权。未保存草稿、版本冲突、责任/生命周期/产出写入忙碌、删除确认等状态会禁用交接；进入 `/ai` 后仍需用户显式带入问题、重新选 `work+outputs+actions` 并人工发送。模型必须先用 `workspace_get(type=task,id=...)` 读取最新事实，按需再读 `workspace_task_assignments` 或 `workspace_task_submissions`，才可起草待确认 `task.*` 建议；交接本身不会保存编辑、改生命周期、分派、提交产出、验收、删除或启动 Agent，详见 [任务详情交给智能体](ai-assistant.md#任务详情交给智能体h4-v)。
- H4-AF：Task 详情“责任分派”区提供更窄的专用“交给智能体”。入口只暂存 Task 身份、标题/状态和固定提示词，推荐 `work+actions`；模型必须先用 `workspace_task_assignments(task_id=...)` 读取真实任务版本、当前负责人/审核人和历史，需要候选人时再用 `workspace_task_options` 读取 actor/role 候选。它不复制人员名称、历史原因、任务描述、产出摘要或授权；责任编辑器打开、分派写入中或父级禁用时不可交接。变更仍只能起草待确认 `task.assign` / `task.reassign` / `task.unassign`，不会联系人员、修改任务状态、验收产出或启动 Agent，详见 [任务责任分派交给智能体](ai-assistant.md#任务责任分派交给智能体h4-af)。
- H4-AG：Task 页“管理标签”弹窗里的已保存标签行提供专用“交给智能体”。入口只暂存 Tag ID、当前显示名称/颜色和 `/tasks` route，推荐 `work+actions`；模型必须先用 `workspace_task_options(type=tag,id=...)` 精确读取同一 Tag ID 的最新元数据，取得 version 后才能起草 `tag.create` / `tag.update` / `tag.delete`。它不复制任务列表、关联任务数量、筛选结果或授权；未保存标签编辑、删除确认或写入中不可交接。交接本身不写标签，删除建议仍需额外人工同意且不会删除任务或改变任务状态，详见 [任务标签交给智能体](ai-assistant.md#任务标签交给智能体h4-ag)。
- H4-AC：Task 详情当前批次、历史批次和全部产出里的未删除 Artifact 卡片，以及右侧工作区“任务产出”文件详情，提供单项“交给智能体”。入口只暂存 canonical Artifact/Task/Submission 身份、产出名称/类型、提交状态和批次号，推荐 `work+outputs`；模型必须先用 `workspace_get(type=artifact,id=...)` 读取真实产出事实，必要时再用 `workspace_task_submissions` 核对批次上下文。它不复制卡片摘要、不读取文件正文或本机路径、不下载文件、不授予 `actions`，也不会验收、返工、删除或修改产出；需要验收/返工时仍走精确提交批次交接并另授 `actions`，详见 [任务/项目产出单项交给智能体](ai-assistant.md#任务项目产出单项交给智能体h4-ac)。
- H4-AD：Task 详情“Agent 执行”列表、“执行过程”抽屉和右侧工作区“子智能体”详情对失败、中断、取消和产出登记待恢复的 Run 提供单项“交给智能体”。入口只暂存规范 Task/Run 身份与有限状态元数据，推荐 `work+outputs+actions+agent_execution`；模型必须先用 `workspace_agent_runs`/`workspace_outputs` 读取真实执行和交付事实。它不自动打开执行抽屉、打开任务、取消、重试、下载或读取执行正文；待登记只能恢复登记，重试/停止仍需原有确认链路，详见 [任务详情 Agent 执行记录交给智能体](ai-assistant.md#任务详情-agent-执行记录交给智能体h4-ad)。

- 必须填写真实 `task_id` 和 `view=list|text|artifacts`。响应始终包含同一读事务的 Task 版本、状态、验收策略、`current_submission_id` 及 Task 详情地址。
- `list`：按 sequence DESC / id DESC 分页，包含仅摘要和 `child_rollup` 批次。可选 status 为 pending_review / accepted / changes_requested / withdrawn；返回批次身份、来源、时间、摘要/验收意见字符数、产出总数及已删除数，不直接返回正文或人员私有资料。`is_current` 只代表 Task 当前指针，已接受、返工或撤回的批次也可能仍是 current，不能据此判断待验收。
- `text`：必须填写属于该 Task 的 `submission_id`，显式选择 `field=summary|review_reason`。SQL 先按 Unicode 字符分页，content_offset 0–1,000,000，content_limit 1–4000（默认 4000）；响应给出 content_length 与 next_content_offset。JSON 转义后超出既有 24 KiB 工具预算会拒绝，可缩小 content_limit 重新读取，不截断成假完整结果。摘要和意见可能包含用户手写敏感资料，授权面板明确披露。
- `artifacts`：必须填写精确批次 ID，按 position ASC / id ASC 分页，返回名称、种类、文件大小/MIME/SHA-256、follow-up 标记与 deleted_at 等元数据；包含已软删行，但不含删除原因、文件路径或正文。未删除的非文件正文继续用 workspace_get 的真实 Artifact ID 读取；已删除正文拒绝，文件始终只有元数据，不代表已验证实际字节或内容。
- list/artifacts 的 limit 1–20、offset 0–1000，与现有 has_more/next_offset/window_limited 语义一致。每次请求是新的事务快照，不把跨页期间变化当冻结历史；非法字段组合、空/伪造 ID、跨任务批次、无权限、取消、恢复中或 Provider 配置变化均拒绝。

主/侧对话的运行进度显示“查询提交与验收记录”，不显示为审批成功。此工具本身只读；受人工确认的提交/接受/返工另见 H2-E2。证据：[读取实现](../../services/sidecar/internal/api/ai_task_submissions.go)、[隔离批次与 Harness 测试](../../services/sidecar/internal/api/ai_task_submissions_test.go)。

### AI 提交与验收命令（H2-E2，源码与隔离测试已实现）

保存会话在单条消息同时授权 `work+outputs+actions` 后，`workspace_propose` 支持 `task.submit_output` 与 `task.review`。仅查询、操作建议单独授权、财务权限或临时会话均不能替代这些条件；工具只能起草，实际写入仍由用户决定。schema 73 不变，不新增模型执行工具。

- 两种动作均要求真实 `task_id`、当前 `expected_version` 与严格 `changes`。提交需 `summary` 和 `artifacts` 数组（0–20 项，摘要或产出至少一项非空），每项包含唯一 `client_ref`、`storage_kind=text|link|structured`、`name`、`requires_followup` 和唯一匹配的正文属性。沿用人工校验，不接受文件、路径、producer 覆盖或模型同意字段。提交按人工入口记录，owner 代录，产出归属确认时的实际负责人；不证明负责人实际执行，不启动 Agent Run。
- 验收需精确当前 `submission_id`、`decision=accept|request_changes` 和 `reason`。接受可空说明，返工必须填写原因，最多 1000 字符。Task 必须 manual + waiting_review，该批次必须属于该 Task 且 pending_review，验收人为当前 active owner；manual 和 child_rollup 均复用原生规则。历史批次不能被当作当前提交审核。
- 人工卡绑定 Task 状态/验收策略/完成条件/当前指针/子任务数量、活动责任与 Actor 名称/类型/状态/版本，展示完整摘要及未删除的非文件正文。文件只有大小与 SHA-256，明确未读/未核验实际内容；需到任务详情下载检查。已删除产出不恢复正文。提交/验收的影响与责任归属另需用户勾选 `confirm_task_output=true`，普通“确认执行”或模型文字不能代替核验；拒绝不能携带该同意标记。
- 动作 JSON 最大 64 KiB、人工预览 JSON 最大 128 KiB；验收先在 SQL 核对最多 20 项及非删除正文合计不超过 96 KiB，再加载完整证据。任何超限整体拒绝，提示在原生任务详情处理，不截断后继续审批。完整预览只发给本地人工卡，不回传为模型工具结果或后续回执；提交草稿本身由本次模型起草。
- 确认在一个事务中重新生成并比较完整预览；任务版本、责任身份/名称或证据变化拒绝旧建议。原生 HTTP 与审批共用 [task_output_commands.go](../../services/sidecar/internal/api/task_output_commands.go)：Submission、Artifact、Task、责任结束、父任务/Inbox 协调、领域事件及 AI 审批一并提交或回滚。原生 multipart 仍由原 HTTP 层负责 staging、文件 commit、失败补偿及幂等快照，AI 不获取文件写权限。
- 提交只进入待验收，标记需跟进的产出沿用 Inbox 投影；接受完成任务并结束活动责任、协调父链；返工回到进行中并保留责任与产出历史。重复确认返回同一结果，不重复写入。结果 `result_id` 是本次 Submission ID，`result_version` 是操作后的 Task 版本，导航通过 Task+Submission 双重身份定位精确批次，不能把批次 ID 当 Task ID；待确认提交尚无新批次时仍打开所属 Task。
- 主/侧对话共用完整证据卡、单独同意、错误回读与工作台缓存刷新；包含单个 Artifact 详情，并先取消旧请求，防止迟到的待验收快照覆盖新事实。下一条消息可收到无正文的历史审批回执，不继承授权、不自动读取最新业务。卡片显示历史批次身份；H4-F 已接通精确批次页面定位、只读核验和返回来源对话，见下节。

验证证据：[命令与预览](../../services/sidecar/internal/api/ai_task_output_actions.go)、[隔离事务/权限/Harness 测试](../../services/sidecar/internal/api/ai_task_output_actions_test.go)、[前端载荷核验](../../apps/web/src/api/aiTaskOutputActions.test.ts)、[迟到请求及缓存测试](../../apps/web/src/api/taskOutputActionFacts.test.ts)。测试使用隔离数据库与模拟上游，不替代真实模型、文件人工核验或桌面验收。

### 精确批次定位与原对话往返（H4-F，源码与隔离测试已实现）

- 智能体查询的每个批次及 text/artifacts 结果提供 `/tasks/:taskId/submissions/:submissionId`。提交确认后与验收建议/回执也用真实批次身份生成同一路径，历史审批不会随新提交改指当前批次；未确认的新提交尚无批次，仍链接所属任务。前端从校验后的 ID 重建卡片地址，不采信模型拼接地址。
- 新增只读 `GET /api/v1/tasks/:id/submissions/:submissionId`，用 Task+Submission 双重归属查询，不扫描历史分页也不替换为最新批次。响应为 `{data:{task:{id,title,status,version,current_submission_id},submission}}`，Task 与完整批次摘要/验收意见、Actor 和全部 Artifact 摘要在同一读事务读取，含软删除元数据；不预读 Artifact 正文、文件字节或路径。Task `ETag` 与 `Cache-Control: no-store` 返回当前事实，而非审批时的冻结快照。
- Task ID 沿用原生 UUID 校验，submissionId 要求规范 UUID；API 不接受查询参数。无 Task 为 `404 TASK_NOT_FOUND`，批次不存在或跨任务为 `404 TASK_SUBMISSION_NOT_FOUND`，非法批次 ID 为 `400 INVALID_SUBMISSION_ID`，额外参数为 `400 INVALID_QUERY`。无新迁移、写操作、Agent Run 或模型权限。
- 主/侧消息的链接由界面附加真实来源 `return_session`，模型只能提供无参数的批次路径。页面仅接受一个有效来源 UUID；未知/重复参数或非法身份不发请求。直接刷新同一路径仍读取同一批次，切换身份丢弃旧请求；加载、失败、错配时不展示旧缓存，不回退到新批次。页面明确区分当前指针、历史、待验收、接受、返工与撤回。
- 复用只读 Artifact 卡：正文按需读取并复核 Task/Submission 归属，软删后不展示正文；文件须用户单独点击下载，复用受控存储的大小/SHA-256 校验，不自动执行、上传给模型或声称文件内容已验收。离页取消请求，忽略迟到结果；下载成功只表示交给浏览器，不保证落盘。此页不提供提交、审核或删除命令，可显式打开任务详情处理，或用“返回原对话”激活来源会话；不恢复一次性工具权限。

证据：[精确只读接口](../../services/sidecar/internal/api/task_submission_location.go)、[真实提交/返工/新批次/删除测试](../../services/sidecar/internal/api/task_submission_location_test.go)、[批次页面](../../apps/web/src/pages/TaskSubmissionPage.tsx)、[导航/身份/旧缓存/下载取消测试](../../apps/web/src/pages/TaskSubmissionPage.test.tsx)。真实模型链接质量、更新服务后的桌面往返及文件内容人工核验仍单独验收；这不是 Agent 执行委派或完整长任务恢复的完成声明。

### 提交

`POST /api/v1/tasks/:id/submit-output` 要求 Task `If-Match`，可选 `Idempotency-Key`。

无文件时发送严格 JSON：

```json
{
  "summary": "交付说明",
  "artifacts": [
    {
      "client_ref": "note-1",
      "storage_kind": "text",
      "name": "结论",
      "content_text": "正文",
      "requires_followup": false
    },
    {
      "client_ref": "link-1",
      "storage_kind": "link",
      "name": "参考链接",
      "reference_url": "https://example.com/result",
      "requires_followup": true
    },
    {
      "client_ref": "json-1",
      "storage_kind": "structured",
      "name": "结构化结果",
      "structured_json": { "outcome": "ok" },
      "requires_followup": false
    }
  ]
}
```

有文件时发送 multipart：

- 正好一个文本字段 `manifest`，内容是同一 JSON 契约且必须作为首个 part；
- 此后只允许 manifest 中 file 项通过唯一 `file_field` 精确引用的同名文件 part；
- 可以在同一 manifest 中混合 text/link/structured/file；
- 未被引用、重复引用、一个字段多个文件或额外文本字段均拒绝。

服务端不接受 `produced_by_actor_id`。成功返回 `{data:{task,submission,artifacts,event}}` 并附新版 Task `ETag`；该用户入口创建的 Submission 固定 `origin=manual`。

本批次每个 `requires_followup=true` Artifact 都在同一 SQLite 事务中生成一个 `kind=event / source_entity_type=task_artifact` Inbox Item，并追加 system `source_projected` 事件；稳定键为 `task-artifact:<artifact-id>:followup`。快照只含来源导航/解释字段，不含正文或文件。提交或投影任一步失败时 Submission、Artifact、Task 状态、Inbox、事件和幂等快照全部回滚；同一提交幂等重放不重复投影。未标记 Artifact 不创建 Inbox Item。

提交限制：summary 最长 10,000 字符；最多 20 Artifact；`client_ref` 1–100 且同批唯一；文本最多 500,000 字符；HTTP(S) 链接最多 4,096 bytes、必须有 host 且不能含 userinfo；structured 必须是 JSON object 且编码后最多 1 MiB；严格 JSON body 与 multipart `manifest` 各最多 1 MiB；单文件非空且最多 50 MiB；完整 multipart 请求最多 100 MiB。Sidecar HTTP read/write timeout 为 180 秒；前端对提交和文件下载使用 120 秒端到端超时，并在发起 multipart 前估算 manifest 与文件总量，避免明知超过服务端边界仍上传。

### 验收

`POST /api/v1/tasks/:id/review` 要求 Task `If-Match`、可选稳定幂等键，body 为：

```json
{ "decision": "accept", "reason": "可选说明" }
```

或：

```json
{ "decision": "request_changes", "reason": "必须说明返工原因" }
```

reason 最长 1,000 字符。只有 manual policy + waiting_review + current pending Submission + active owner reviewer 可审核；这里的 Submission 来源可以是 manual 或 child_rollup。成功返回 `{data:{task,submission,event}}`。owner 接受 child_rollup 后父任务才变 done；要求返工会保留 changes_requested 系统批次并禁止自动覆盖。

### 历史、详情与下载

| 方法 | 路径                                                                           | 说明                                                                                                                          |
| ---- | ------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------- |
| GET  | `/api/v1/tasks/:id/submissions?page=&page_size=`                               | sequence DESC；每条带 `origin`、Actor 摘要、Artifact 摘要和总数；包含已软删 Artifact 摘要                                     |
| GET  | `/api/v1/tasks/:id/submissions/:submissionId`                                  | 精确 Task+Submission，只读事务返回 Task 身份/状态/版本/当前指针及完整批次元数据；不含 Artifact 正文或文件字节，不接受查询参数 |
| GET  | `/api/v1/tasks/:id/artifacts?page=&page_size=&submission_id=&include_deleted=` | 默认隐藏软删；按批次倒序、position/id 正序                                                                                    |
| GET  | `/api/v1/artifacts/:id`                                                        | 返回元数据及按类型的正文；软删详情仍 200，但 payload 全为 null                                                                |
| GET  | `/api/v1/artifacts/:id/content`                                                | 仅 file；鉴权后校验大小和 SHA-256，再作为 attachment 下载                                                                     |

两个列表默认 `page=1 / page_size=50`、最大 100，返回 `{data, meta:{page,page_size,total,task_version}}` 和 Task `ETag`。所有 Artifact 摘要和详情都必须带由父 Submission 派生的 `submission_status`；摘要不暴露正文或 `relative_path`。前端依据该必填状态禁用 pending-review 删除，但服务端仍执行最终授权校验。

下载成功设置 Content-Type、Content-Length、安全 UTF-8 Content-Disposition、`X-Content-Type-Options: nosniff`、`Cache-Control: no-store` 和 SHA-256 ETag。非 file 返回 `ARTIFACT_CONTENT_UNAVAILABLE`；已删/缺失为 410；大小或哈希不符为 `ARTIFACT_INTEGRITY_MISMATCH` 并禁止输出内容。

前端对已冻结的 D2 错误码提供中文反馈：策略/提交/审核前置分别覆盖 `TASK_MANUAL_REVIEW_REQUIRED`、`TASK_ASSIGNEE_REQUIRED`、`TASK_REVIEWER_REQUIRED`、`TASK_SUBMISSION_NOT_ALLOWED`、`TASK_SUBMISSION_ALREADY_PENDING` 和 `TASK_REVIEW_NOT_ALLOWED`；Artifact 状态覆盖 `ARTIFACT_PENDING_REVIEW`、`ARTIFACT_ALREADY_DELETED`、`ARTIFACT_DELETED`、`ARTIFACT_FILE_MISSING` 与 `ARTIFACT_INTEGRITY_MISMATCH`。未识别错误仍显示服务端 message 和可重试边界，不吞掉 `request_id`。

### 软删除与硬删除

`DELETE /api/v1/artifacts/:id?confirm=true` 要求 Task `If-Match`、可选稳定幂等键和 `{ "reason": "1–1000 字符" }`。pending-review 批次禁止删除；作为 queued/running v2 Agent Run 冻结输入的 Artifact 也禁止删除并返回 `409 AGENT_RUN_FILE_REFERENCED`。非文件只写软删除元数据；file 删除在同一事务写不可变 tombstone，存在的文件先移入 `.trash/`，事务失败恢复，成功提交后清除 trash 文件。若物理文件已经缺失，确认软删除仍成功，并将完整性记录为 `missing` 及检查时间。列表可用 `include_deleted=true` 审计，详情隐藏已删 payload，文件下载返回 410。活动 v2 快照无法严格解析时，引用检查 fail closed，不以“无法证明引用”作为删除授权。

若 Artifact 是尚处于 `open/tracking` 的 follow-up Inbox 来源，删除返回 `409 ARTIFACT_HAS_ACTIVE_INBOX_SOURCE`；用户必须先解决或忽略来源项。随后删除在同一事务先写 Inbox `source_deleted_at`、递增 Inbox 版本并追加 `source_deleted` 事件，再软删 Artifact；任何后续文件、数据库或事件失败都会整体回滚。

Task DELETE 是聚合硬删除：服务端先确认没有开放 Focus Session、`unlinked_at IS NULL` 的活动 Inbox 关系、`open/tracking` 的 Artifact 来源 Inbox，也没有 `queued/running` Agent Run；命中时分别返回 `TASK_HAS_OPEN_FOCUS_SESSION`、`TASK_HAS_ACTIVE_INBOX_RELATIONS`、`TASK_HAS_ACTIVE_INBOX_SOURCES` 或 `TASK_HAS_ACTIVE_AGENT_RUN`，且不移动文件、不删除部分事实。pending Run 已完成模型执行，提示先恢复产出登记；其他活动 Run 提示先取消或等待。通过检查后，同一事务先给终态来源项写 `source_deleted_at` 和审计，再为 active file 写 `deletion_scope = task` tombstone并移入 trash；随后级联删除 Task 聚合及终态 Run 历史。终态 Focus Session、已解除关系快照和来源 Inbox 快照继续保留；失败恢复文件并回滚来源标记，提交后清理物理文件。若被级联的 Run 来自已确认 `agent_run.start`，Proposal 决定及 `result_id/result_version` 仍保留；回执省略实时 `agent_run_result`，前端显示不可导航的“执行记录已删除” tombstone，不再查询 Run，也不能把历史 confirmed 当作当前成功证据。

## 受控文件目录

```text
artifacts/
  .opc-artifact-store-v1
  .opc-artifact-store.lock
  .staging/
  objects/
  .trash/
  .quarantine/
```

schema v9 为数据库创建单例 `workspace_identity`：`database_id` 永久不可变，`artifact_store_id` 只能从空值绑定一次。Sidecar 只接受自己声明过的 root：未绑定数据库声明空目录时写规范 JSON marker `{format_version,database_id,store_id}`，再把 store ID 写回数据库；后续格式/版本/数据库 ID/store ID 任一不符、已绑定数据库改用另一空 root、非空缺 marker、卷根、符号链接或 reparse point 路径均拒绝启动。启动协调前通过 `.opc-artifact-store.lock` 获取进程级非阻塞独占锁，并在 router 生命周期内持有；第二个指向同一 root 的 Sidecar 会启动失败。文件名使用服务端 Artifact UUID，数据库固定只保存 `objects/<artifact-id>` 相对路径。提交事务报错后先查数据库，只有能证明无引用才清除已提升 object；无法排除模糊 COMMIT 已落库时保留给启动 reconcile。marker、暂存文件、对象提升、移动/删除与关键目录项在返回成功前做耐久同步。启动恢复 active trash 前核对 size/SHA-256；错配候选移入 `.quarantine/` 并把 Artifact 标为 mismatch。tombstone 对应且校验一致的授权删除可清理，其余无引用受控候选进入 quarantine；意外目录、链接与无法识别的名称不会被递归跟随或清理。

## 前端交互与并发

- 筛选面板可保存当前完整条件、选择并立即应用视图、以当前条件更新所选视图，或二次确认删除；保存视图在 SQLite 中跨重启保留。创建达到 20 个上限、同名、网络错误和版本冲突都有明确反馈。
- 应用视图会原子替换搜索、状态、优先级、类型、项目、客户、标签、日期和排序并回到第一页，不恢复旧页码或任务选择；更新/删除携带当前视图版本，冲突时刷新列表且不自动覆盖。
- 选中保存视图后，“交给智能体”只把该视图身份带到 AI 助手，由用户在 `/ai` 手动带入并授权；不会触发原生应用视图、保存、更新或删除流程，也不会把当前任务列表页冒充为该视图的完整结果集。
- 客户筛选复用共享 ClientSelect，而不是预先拉全 Client options。控件每页请求 20 条，输入经 250 ms 防抖后发送 `q`，支持稳定上一页/下一页、取消旧请求、跨页/失败保留当前选择、inactive 可见可选，以及加载、空、错误重试、更多结果提示和 combobox 键盘语义。服务端仍沿 Task 的 Project 当前 `client_id` 过滤；Task 不保存客户副本。客户与项目、标签、状态及日期等条件取 AND，并完整保留到后续分页和保存视图应用。
- Project 选择复用共享 ProjectSelect，而不是预先拉全 Project options。Task 新建/编辑、Tasks 项目筛选、批量 `set_project` 的目标和 Inbox 拆分中的每个任务都使用同一组件；每页请求 20 条，输入经 250 ms 防抖后发送 `q`，`q / page / includeArchived` 进入 Query key，旧列表和选中详情请求可取消，候选按 ID 去重。默认候选不列归档项目；详情查询或调用方名称 fallback 使跨页、失败、保存视图及既有归档关联仍保留当前选择。只有用户点击可聚焦的清除按钮才提交空筛选或 `project_id=null`，搜索、翻页和错误不得隐式解除关联。
- ProjectSelect 显示项目状态与客户上下文，并覆盖输入等待、首批加载、无项目、无匹配、错误重试、更多结果和前后翻页；combobox/listbox 使用方向键、Home/End、Enter、Escape、Tab 和 PageUp/PageDown，输入法合成期间不解释快捷键。Task 项目筛选改变后回到第一页，继续与客户、标签、状态和日期条件取 AND；批量目标只改变用户选中的 Task，不把当前列表页冒充完整选择集。
- 计划日期支持精确值或起止范围二选一，截止日期支持独立起止范围；设置任一计划范围端点会清空精确值，设置精确值会清空计划范围。
- `due_state=overdue|due_soon` 按单次请求捕获的 Sidecar UTC 时刻派生，并固定排除 done/cancelled；逾期为 `< now`，临期为 `[now, now+24h]`。它与按 UTC 日期片段过滤的 `due_from/due_to` 语义不同且不可同时提交；显式状态只允许 `active`。比较和 `due_date` 排序使用固定宽度 UTC 纳秒键，避免亚毫秒精度丢失与 RFC3339 整秒/小数秒文本排序误差。
- `due_state` 当前只服务 Today 的动态风险快捷视图，不纳入 schema v17 保存视图。保存视图继续表示用户明确选择的静态任务页条件，不能静默保存或恢复会随服务端时钟自动变化的结果集。
- 起点晚于终点时，对应日期控件显示无效状态和就地错误，主 Task Query 暂停，旧任务结果不继续展示；服务端仍二次校验格式和顺序，避免绕过 UI。
- 任务页只有在选中一个精确计划日期、排序为 `manual_order` 且没有搜索、状态、优先级、类型、项目或标签筛选时启用排序；上移/下移与拖动手柄同时保留。
- 拖拽限定在同一状态分组，不会通过视觉移动暗中改变 Task 生命周期。前端立即预览当前页相对位置，但 hook 按源/目标重新读取完整计划日期组，校验日期、状态和可见版本，再把同状态顺序织回完整组的原槽位并调用既有原子 reorder API。
- 看板固定展示六个生命周期列，列计数明确是当前服务端分页结果；卡片可选择进入既有原子批量操作，点击正文进入共享任务详情。跨列拖拽按来源/目标解析为 `start / block / unblock / complete / cancel / reopen`，确认后携带卡片版本执行；阻塞/取消要求原因，blocked 只能解除回 `blocked_from_status`，manual 与 waiting_review 不得绕过详情中的人工验收。
- 看板不做乐观状态写入；命令成功后由 Query 失效读取服务端事实，失败时卡片保持原列。版本冲突关闭旧确认、刷新列表并要求用户基于新版本重新拖动，不自动重试。
- 分页页面不把当前 50 行作为完整计划组提交；完整集合由 hook 自动分页读取，最多 1,000 项。集合/版本变化、网络错误或服务端拒绝时清除乐观预览并刷新 Task/Project/Today/Inbox，页面回到服务端事实。
- 新建与详情编辑提供 review policy；详情只有在 todo 且无 current/历史 Submission 时开放修改。
- 输出编辑器允许 summary 加最多 20 个条目；文件草稿保留浏览器 `File`，不会先复制或上传。
- waiting_review 展示当前批次和完整 Artifact 摘要，接受与返工互斥；返工原因空白时前端阻止提交。
- 列表与详情把进度分母显示为直属非取消子任务数，并另列取消数；child_rollup 标记为“子任务汇总”，明确这是系统发起的父任务验收且没有 Artifact，不伪装成人工产出。
- manual 父任务缺少 assignee 或 builtin owner reviewer 时，产出区提示补齐门禁后才会自动发起；policy none 明确不会因子任务完成自动完成。系统请求、撤回和重开事件及失效原因在时间线中有独立文案。
- Artifact 正文按需加载；missing/mismatch/deleted/corrupt 响应均有明确提示和重试边界，不将下载错误伪装为成功。
- 未删除 Artifact 卡片可交给智能体做单项复查准备；该入口只推荐 `work+outputs` 读取受控产出事实，不读取文件字节、不下载、不验收、不删除或修改产出。
- 上传与下载的 120 秒传输期间显示 busy 状态并锁定其他 Task 写入；超时或失败保留未提交草稿，不伪造成功。
- 删除需要确认并填写原因；pending-review 项不显示可执行删除动作。
- 被 queued/running v2 Agent Run 引用的输入 Artifact 返回 `AGENT_RUN_FILE_REFERENCED`；UI 不自动取消执行或替换输入，须先等待终态或显式取消 Run。
- Task 删除若被活动 Inbox 关系阻止，前端显示可解释冲突并提示先到收件箱解除活动关系，不自动替用户解除或重试删除；当前尚无 Task→Inbox 反向关系列表或直达导航。
- Task 删除若被活动 Agent Run 阻止，服务端返回稳定 `TASK_HAS_ACTIVE_AGENT_RUN`；pending 提示先恢复交付，普通 queued/running 提示取消或等待，不自动取消子进程、丢弃 staging 或重试删除。
- 终态 Task 删除成功后，来源会话中已确认的 Agent Run 回执保留历史决定但不提供链接；UI 不因缺失实时 Run 报错或调用 `getRun`，而是明确提示已删除、无法再读取运行状态。
- 所有 D2 写操作与事实编辑、Assignment、生命周期命令互斥。
- 版本冲突时客户端保留完整草稿，刷新 Task/Assignment/Submission/Artifact/Event/Project/Today 缓存，再要求用户重新确认；不会用旧 `If-Match` 自动重试。
- 成功提交或写命令使用同一稳定幂等 key；重新确认冲突后生成新命令上下文，避免把不同预期版本复用到旧 key。

## 迁移与兼容

schema v9 的 `009_task_submissions_artifacts.sql`：

- 新建单例 `workspace_identity`：规范 UUID `database_id` 不可变，`artifact_store_id` 只允许从空值绑定一次，用于把受控 Artifact root 与当前数据库一一对应；
- 新建 Submission/Artifact 表，以及跨聚合删除保留且不可变的 `artifact_deletion_tombstones`；
- 给 Task 增加 `current_submission_id` 并要求只能指向同 Task 最新批次；
- 给 Workflow Event 增加 nullable `submission_id / artifact_id`，校验聚合一致性并延续追加式保护；
- 对 schema v8 已存在且无歧义的 manual 提交/审核事实生成 `is_inferred = 1` 的 sequence 1 Submission；
- 用内置 owner 填提交/审核/撤回 Actor，按旧状态推断 pending/accepted/changes_requested/withdrawn；
- 写一条 system Actor 的 `migration_submission_backfill` 事件；
- 不为历史任务编造 Artifact。

迁移在固定连接上事务外临时关闭外键，事务提交前运行 `foreign_key_check`，成功或失败都恢复外键；异常时整体回滚。

schema v13 的 `013_inbox_item_tasks.sql` 不改写 Task 表或 D2 文件契约，只新增关系表及 Task 删除保护。关系 GET 实时 JOIN `tasks`；A2 不新增 Task.version→Inbox.version trigger。Task 删除前由 API/数据库共同拒绝活动关系，已解除历史关系使用 nullable FK `SET NULL` 并保留原 ID/标题快照。

schema v23 的 `023_task_artifact_inbox_projection.sql` 不重建 Task/Submission/Artifact/Inbox 表，也不回填历史 `requires_followup`。它增加 Artifact 来源索引和 insert/update/delete guards：只允许存在且未删的 follow-up Artifact 建立规范来源；来源身份/payload 不可变；Artifact 软删或聚合硬删前必须先完成 Inbox 来源协调。

schema v24 的 `024_task_blocked_inbox_projection.sql` 不回填迁移前已阻塞 Task。它以阻塞后的 Task version 区分每次 block，约束 `source_entity_type=task` 的事件键和最小快照，冻结来源身份；活动来源阻止 Task 硬删除，来源项终态后删除事务先写 `source_deleted_at` 与 Inbox 审计，再删除 Task。

schema v25 的 `025_task_due_inbox_projection.sql` 不回填迁移前已进入临期窗口的 Task。Sidecar ready 前及运行中每 15 秒扫描状态非终态且截止时间不晚于未来 24 小时的 Task，以 `task:<task-id>:due:<due-at>` 为稳定键；每批最多 100 条且排除已投影截止事实，积压可持续推进。改期形成新截止事实，已生成事项不随完成/取消/改期自动归档。活动来源阻止 Task 删除，来源项终态后复用统一删除协调保留快照。schema v26–v29 的系统维护、Workspace Avatar、Project 完成来源和存储设置迁移不改写 Task 表；schema v30 只给 `task_submissions` 增加 `origin` 并新增父任务推进规则，不改变 `tasks` 表字段或既有生命周期状态集合。

schema v30 的 `030_task_parent_progress.sql` 是非破坏性追加迁移：

- 给 `task_submissions` 增加非空 `origin`，只允许 `manual / child_rollup`；既有行通过默认值保持 manual，不重写其状态、Actor、摘要或时间；
- 约束 child_rollup 只能由内置 system 创建、必须 `is_inferred=0`，并禁止其拥有 Task Artifact；`origin` 与其他 Submission 身份事实同样不可变；
- 不在 migration 或 Sidecar 启动时扫描、补写既有父任务层级。只有迁移后的相关 Task/Assignment/review policy 写命令触发事务内 reconciliation；
- 不改变 Inbox 表或 `inbox_item_tasks.is_required`，不创建 demo 数据。schema v31 已追加 Project→Client Activity 来源约束，schema v32 已扩展 Reminder，schema v33 已新增受限 Automation Rule/Run，schema v34 已新增 Agent Adapter，schema v35 已新增 Client Followup。这些是各版本迁移当时的变更；当前迁移链已到 v69，后续只能单调追加，不重写历史迁移。

## 已验证与后续

当前自动验证覆盖（既有历史验收与本次完整门禁分别记录；本次统一结果见 [AI7 计划](../plans/ai-quality-gates.md)）：

- ADR-025 的 Task 共享创建校验、原子创建与消息绑定、重复确认、改载荷冲突、任务删除后不重建，以及前端响应丢失后恢复和真实创建结果文案；自动化使用隔离 HTTP/数据库夹具，不代表真实模型已验收。

- migration v8→v9 数据保留、约束、inferred 回填、事件关联、重跑/回滚和外键恢复；
- JSON 与 multipart 混合提交、Actor 归属、限制、并发、幂等重放和补偿；
- 接受、返工、取消撤回、reopen、软删、Task 硬删、文件丢失/篡改和安全下载头；
- 前端 manual 前置条件、混合草稿、审核、冲突时 `File` 保留、下载错误与软删确认；
- 前端全量测试、typecheck、Web build、format check；Go 全包测试、database 重复测试和 `go vet`。
- 任务页精确计划日期下的同状态拖拽、乐观顺序、完整计划组槽位重建和版本校验。
- 任务页列表/看板切换、看板平铺查询、六列空状态、卡片详情直达、跨列命令映射/原因/确认/版本及人工验收门禁，以及与现有批量选择共用事实。
- 任务页计划/截止日期范围序列化、合法范围分页请求、倒置范围查询门禁，以及 Sidecar 的合法/非法范围过滤。
- Today 截止风险客户端序列化、Sidecar 固定时钟边界/终态排除/冲突拒绝、稳定分页，以及同一时钟下逾期/临期列表 `meta.total` 与 Today 统计严格一致。
- 任务页 ClientSelect 的首批/搜索/上一页与下一页有界分页、查询键切换与卸载取消、跨页和失败选中保留、inactive、加载/空/错误重试/更多结果反馈、combobox 键盘交互及页面接线；客户端 `client_id` 序列化、保存视图恢复、Sidecar UUID 拒绝及 Task→Project→Client 正向过滤继续覆盖。
- ProjectSelect 的 250 ms 搜索、首批/前后页有界请求、`q / page / includeArchived` Query key、实际取消、按 ID 去重、当前归档/失败 fallback、显式清除、加载/空/错误/更多结果和 combobox 键盘交互已有组件与 hook 级覆盖；Task 新建/编辑、Tasks 筛选/批量目标和 Inbox 拆分均有接线验证。Go 定向测试覆盖 Project 同名排序以 `id ASC` 收口、所有排序字段和既有过滤契约；代码审查确认 `COUNT` 与 `SCAN` 共用请求上下文内的只读事务。模块文档不单独维护易过期的全仓测试总数。
- schema v16→v17 事实保留、保存视图 JSON/名称/schema 约束、API 规范化/并发/确认删除，以及前端应用/创建/更新/删除交互。
- schema v22→v23 不发明 Inbox 数据；follow-up Artifact 提交/幂等重放/事务回滚、来源上下文、活动删除阻止、归档后 Artifact/Task 删除协调和来源快照保留。
- schema v23→v24 不回填既有 blocked Task；每次 block 的稳定来源、幂等重放、重复阻塞、前端来源上下文、活动删除阻止及终态后 Task 删除协调和快照保留。
- schema v24→v25 不回填既有 Task；提前 24 小时/启动补偿扫描、逾期分类、稳定 Task+截止时间来源键、100 条批次推进、改期独立来源、事务回滚、前端上下文和 Task 删除协调。
- schema v29→v30 数据保留、既有 Submission 默认 manual、origin/system/inferred/Artifact 约束、迁移失败回滚、外键完整性和无破坏性迁移门禁。
- 直属子任务 done/cancelled 口径、零非取消子任务、manual/assignee/builtin owner reviewer 门禁、系统无 Artifact child_rollup、owner accept、失效撤回、blocked 来源更新、changes_requested/manual 不覆盖，以及 accepted 父任务与祖先重开。
- 创建、改绑、解除父级、删除、单条与批量生命周期、review accept、policy 和 Assignment 变更均在事务内协调；批量去重且返回最终版本，Inbox 显式 required 关系保持独立。
- 前端 origin/取消计数兼容规范化、“子任务汇总”/门禁/时间线文案、列表与详情的非取消进度展示；typecheck 与定向组件测试覆盖。

已知环境边界：当前自动协调只由迁移后的相关写命令触发，没有全库启动回填；accepted 父任务被系统重开时不会恢复已经结束的 Assignment，需要 owner 重新分派后才能继续人工流转。祖先协调使用 visited 集合防止循环，并沿完整有效祖先链传播。ClientSelect 与 ProjectSelect 均尚未完成真实浏览器键盘/焦点、窄屏和 1,000/10,000 条数据性能专项；组件测试不能替代这些证据，Project 的包含式 `LIKE` 搜索也不能仅凭有界分页推断大数据量响应性能。Windows 桌面 Rust 原生测试在未安装 MSVC linker 的主机仍可能无法执行，这不影响已通过的 Go 定向测试和前端 typecheck，但不能据此宣称完整跨平台桌面验收。

仍属后续：其他业务来源投影、自动创建 Reminder、Client 外部来源、跨平台/external Runner、多步自主计划，以及任意 filesystem/Shell/browser/Git/terminal 自动访问。显式 follow-up Artifact、Task 阻塞与 Task 临期来源、Windows 内置 Runner、受确认 AI→Run、H5-C 受控文件输入和单文件产出已交付；真实 Provider、桌面交互、断电恢复及文件内容人工核验单独验收。AI 的人工确认创建/分派不代表自主执行，只有独立 `agent_execution` 确认才能创建单次 Run；文件输入还需要独立文件确认。

## 相关代码/PRD 链接

- [PRD 任务需求与 T-18D](../opc-workspace-PRD.md)
- [schema v9 迁移](../../services/sidecar/internal/database/migrations/009_task_submissions_artifacts.sql)
- [schema v13 Inbox–Task 关系迁移](../../services/sidecar/internal/database/migrations/013_inbox_item_tasks.sql)
- [schema v23 Artifact 来源迁移](../../services/sidecar/internal/database/migrations/023_task_artifact_inbox_projection.sql)
- [schema v24 Task 阻塞来源迁移](../../services/sidecar/internal/database/migrations/024_task_blocked_inbox_projection.sql)
- [schema v25 Task 临期来源迁移](../../services/sidecar/internal/database/migrations/025_task_due_inbox_projection.sql)
- [schema v30 父任务自动推进迁移](../../services/sidecar/internal/database/migrations/030_task_parent_progress.sql)
- [Task output API](../../services/sidecar/internal/api/task_outputs.go)
- [Inbox 来源投影服务](../../services/sidecar/internal/api/inbox_source_projections.go)
- [Task 临期扫描服务](../../services/sidecar/internal/api/task_due_projections.go)
- [受控 Artifact store](../../services/sidecar/internal/api/artifact_store.go)
- [Task 生命周期](../../services/sidecar/internal/api/task_workflow.go)
- [父任务自动推进服务](../../services/sidecar/internal/api/task_parent_progress.go)
- [Task API](../../services/sidecar/internal/api/tasks.go)
- [共享 Task 创建领域逻辑](../../services/sidecar/internal/api/task_creation.go)
- [AI 任务原子确认 API](../../services/sidecar/internal/api/ai_task_confirmation.go)
- [ADR-025：可靠性与确认事务](../adr/025-ai-reliability-confirmations-and-evaluation-identity.md)
- [Task 保存视图 API](../../services/sidecar/internal/api/task_saved_views.go)
- [schema v17 保存视图迁移](../../services/sidecar/internal/database/migrations/017_task_saved_views.sql)
- [Task output model](../../services/sidecar/internal/models/artifact.go)
- [前端 Task output 组件](../../apps/web/src/components/TaskOutputsSection.tsx)
- [前端 Task 专注记录组件](../../apps/web/src/components/TaskFocusHistorySection.tsx)
- [任务列表页](../../apps/web/src/pages/TasksPage.tsx)
- [任务看板](../../apps/web/src/components/TaskBoard.tsx)
- [任务看板生命周期确认](../../apps/web/src/components/TaskBoardTransitionModal.tsx)
- [任务保存视图控件](../../apps/web/src/components/TaskSavedViewsControl.tsx)
- [共享 Client 选择器](../../apps/web/src/components/ClientSelect.tsx)
- [共享 Project 选择器](../../apps/web/src/components/ProjectSelect.tsx)
- [任务列表页测试](../../apps/web/src/pages/TasksPage.test.tsx)
- [前端 Artifact 卡片](../../apps/web/src/components/TaskArtifactCard.tsx)
- [Go D2 测试](../../services/sidecar/internal/api/task_outputs_test.go)
- [父任务自动推进测试](../../services/sidecar/internal/api/task_parent_progress_test.go)
- [schema v30 迁移测试](../../services/sidecar/internal/database/task_parent_progress_migration_test.go)
- [迁移测试](../../services/sidecar/internal/database/task_artifacts_migration_test.go)
- [Inbox–Task 删除互锁测试](../../services/sidecar/internal/api/inbox_item_tasks_test.go)
- [前端 D2 测试](../../apps/web/src/components/TaskOutputsSection.test.tsx)
