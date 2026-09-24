# 内容日历模块

> 目标版本：v0.3。该模块提供本地内容排期与准备工作协同，不属于 v0.1。当前已完成 CC1–CC5-B（schema v38），包括内容排期、准备任务和审核/发布到期 Inbox 协同。

## 定位与边界

内容日历用于规划内容主题、平台、审核状态、目标发布时间及准备任务。它是本地编辑计划，不是社交媒体发布器或外部内容管理系统。

- 所有内容元数据、草稿引用、任务关系和提醒保存在本机。
- 平台字段仅用于分类；第一阶段不保存平台凭据，不调用发布 API，不自动上传或发送内容。
- `published` 只表示 owner 手动确认的本地事实，不代表应用获得了外部平台回执。
- 内容正文编辑器、素材生产、版权检查、远程协作和数据分析不在首版范围。
- AI 或本地 Agent 可准备草稿/建议产物，但必须由用户验收，不能自动发布。

## AI 已发布事实确认（H2-AD，2026-09-21）

`work+actions` 现在可对 `draft/in_review/scheduled` 条目提出 `content_item.publish` 建议，但仅在用户已明确表示外部发布实际发生后。提议只保存卡片，不修改内容。模型须先读取真实 Content Item ID 与版本；`changes` 只允许用户明确提供的 RFC3339 实际发布时间 `published_at` 和外链文本/null `external_link`，不允许状态等额外字段。若省略发布时间，卡片标明使用用户确认时刻。外链始终是不可信的本地文本，应用不会访问、验证或向外部平台发布。

卡片冻结发布前状态、时间、外链和发布后的对应值。用户须自行在平台核实内容确实发布，检查时间与链接并单独勾选 `confirm_content_published=true`；智能体不能代填同意。确认事务重读版本和完整预览，复用原生 `publishContentItemInTransaction`，将状态、实际发布时间、可选外链、版本及旧 Inbox 来源终结与审批事件原子提交。版本/状态变化或审计写入失败时旧卡不执行；成功回到指定内容详情。`published` 不表示平台回执，准备 Task 状态也不因此改变。原生人工发布确认入口仍保留；手工重排、链接抓取和自动发布仍未实现。API v1 / schema 077 不变，无迁移。

确定性测试覆盖不写入的提议、独立同意、错误回滚、版本冲突和默认确认时间，以及前端卡片；真实 Provider 措辞、外部平台事实和原生桌面体验需另行人工验收。代码与测试见 [AI 内容命令](../../services/sidecar/internal/api/ai_content_item_actions.go)、[原生共享事务](../../services/sidecar/internal/api/content_calendar.go)、[后端测试](../../services/sidecar/internal/api/ai_content_item_actions_test.go)、[确认卡](../../apps/web/src/components/AiWorkspaceActions.tsx)。

## 到期扫描与 AI 来源闭环（H2-AA，2026-09-21）

启动/15 秒周期的到期扫描现在复用列表的固定九位小数时间键选取与排序候选，事务内重读并解析当前 `scheduled_at`，与扫描开始的固定时刻比较。已复现并修复整秒与短小数排期在同秒到期被漏过、以及批次顺序错误；恰好到期应进入，未来 1 纳秒不进入。存储时间保持原样，事务复核只接受原生 UTC-Z、0–9 位小数格式；选中的不合法时间失败关闭且不回显原值，不把 SQL 键截断当合法时间。

每批仍最多 100 条，既有内容版本/事件键去重、重读后改期检查和 Inbox/审计事务保持，101 条积压可继续推进，不新开调度器或增加自动发布能力。该修复不代表所有模块的调度时间或导入数据完整性已验收。

`workspace_inbox_source` 在 `work` 下能从真实审核/发布提醒取得当前 Content Item ID/version/route，同时说明事件版本是否仍匹配。智能体再读 `roadmap_content` 指南及最新内容事实，沿用待确认改期建议；人工确认后旧事项按原生规则终结、不可变来源 payload 保留，新查询显示历史与当前版本差异。提醒已解决不等于内容已发布。见 [Inbox 来源权限契约](inbox.md#ai-来源定位h2-aa2026-09-21)。

代码与测试：[扫描器](../../services/sidecar/internal/api/content_calendar_inbox.go)、[时间边界回归](../../services/sidecar/internal/api/content_calendar_inbox_time_test.go)。API v1/schema 076 不变，无迁移、外部发布或真实业务数据操作。

## AI 结构化排期与准备任务查询（H2-Z，2026-09-21）

AI 独立轨道增加 `workspace_content_items`，仅在本条消息授予 `work` 时注册，通过 `workspace_guide(topic=roadmap_content)` 按需发现。查询只读，API v1/schema 076 不变，无新权限、迁移或发布能力。

- `view=list` 的可选 `filters` 支持精确 `platform`、`status`、canonical `project_id`、RFC3339 `scheduled_from` / `scheduled_to`、`schedule_state=scheduled|unscheduled`、`include_archived` 与 `task_state=all|any_incomplete|required_incomplete`。范围为 `[from,to)`，无时间边界时可读全部排期；`schedule_state` 表示有无计划时间，不是内容状态，unscheduled 不可同时带日期。默认排除归档，显式 `status=archived` 可读历史。
- `task_state` 在分页前按真实关系筛选。只有 `done` 计完成，`waiting_review/cancelled` 仍未完；零关联任务不匹配 any_incomplete，零必需任务不匹配 required_incomplete。返回内容 ID、最多 200 Unicode 字符标题、状态/版本、平台、UTC 计划时刻/IANA 时区、Project ID、全部/必需任务总数及完成数、详情 route，不读取备注、外链、Task 正文、文件或 Actor 信息。
- `view=tasks` 必须提供 canonical `content_item_id`，可用 `required_only`（默认 false）和顶层 `task_state=all|incomplete`。返回匹配的真实 Task ID、最多 200 字符标题、状态、**Task version**、required 与任务 route，并附同事务的 `content_item` 全量准备进度。突破旧详情最多 50 个任务摘要的定位限制；这不是修改内容详情或原生任务关系 API 的响应契约。
- 两个 view 均支持 `limit=1..20`、`offset=0..1000`，返回过滤后的 `total_items`、`next_offset`、`has_more`、`window_limited` 和 `as_of`。未读页不是已知事实，窗口封顶须缩小条件；每次查询是新的只读事务，不承诺跨页冻结。条目、总数、关系及进度属于同一快照；Task 状态变化不要求内容版本变化，内容 version 不能代替 Task version。参数严格拒绝额外字段、重复字段、大小写别名、null、类型错误和跨 view 参数；编码结果超过原 24 KiB 门禁失败关闭，可减小 limit 重查。
- 原生 `GET /api/v1/content-items` 和 AI 共享非 Gin 解析、谓词、排序与进度统计；原生增加相同 `task_state` 筛选。修复可变精度 RFC3339Nano 字符串导致整秒/小数排序和范围漏读：输入按真实时刻比较，SQL 使用固定九位小数比较键，不改已存时间。原生列表现在也在同一只读事务加载详情/准备进度。原有参数兼容保持，AI 层额外严格校验。
- 页头“梳理内容排期”仅交接代码生成的当前视图条件和固定提示词：月格带实际六周半开范围、状态和 IANA 时区（用于日期解释，不是工具参数）；未排期/归档视图带各自实际过滤，不继承隐藏月份或状态。创建/详情弹窗、改期请求或尚待回读的预移会禁用页级入口；单项“交给智能体”保留。列表、备注、外链、准备任务、会话和授权不复制；用户仍需带入、重新选择 `work+actions` 并手动发送。页级导航不是查询已完成，也不会调用模型或改排期。
- 完整链路为：读排期 → 分页找到实际准备任务 → 必要时另读 `tasks_projects` 指南/任务当前事实 → 起草既有 Task 或 `content_item.*` 建议 → 人工确认 → 重新读取事实。准备完成不自动审核、发布、改期或更新内容状态；不得靠去掉 required 或绕过验收伪造完成。关系变更依然只改关系，Task 状态操作沿用独立 Task 命令和验收门禁；H2-AD 增加已发布事实的独立人工确认卡，不代表工具自动发布。

确定性双协议 Harness 用例覆盖 51 个真实准备任务分页、未确认零写、人工完成最后一项后准备进度归满且内容版本不变、另行审批改期后退出旧时间范围，以及新消息不继承权限、幂等确认和正文隔离。它们不是实际模型或原生桌面体验验收。H2-Z 当时只修复查询边界；到期扫描随后由上文 H2-AA 单独复现与修复，不据此宣称所有调度边界均已验收。

代码与测试：[查询工具](../../services/sidecar/internal/api/ai_content_items.go)、[共享原生查询](../../services/sidecar/internal/api/content_item_query.go)、[工具合约测试](../../services/sidecar/internal/api/ai_content_items_test.go)、[双协议闭环](../../services/sidecar/internal/api/ai_content_items_harness_test.go)。

## 当前实现状态

- H4-AT：内容详情的“返回原对话”和“交给智能体”共用未保存信息、排期、外链及待关联任务的草稿门禁；有未保存更改时显示原因且不导航。详情的底部关闭、右上关闭、Escape 与遮罩统一处理：在途写入不可关闭；存在草稿先显示“舍弃未保存更改？”确认，默认继续编辑，关闭确认本身也保留原草稿。只有用户明确舍弃才关闭本地编辑，不触发写入。保存信息、改期、发布确认、任务关系和删除成功仍沿各自既有成功路径关闭并保留来源查询参数；失败保留输入与错误。在途操作同时冻结表单输入，避免等待回执时新写的草稿被成功关闭丢弃。此项只保护详情弹窗及其返回入口，不是浏览器刷新、历史导航或全局草稿持久化方案。

- H4-S：内容详情使用专用“交给智能体”，只暂存 `/content-calendar?item=<id>`、canonical 内容 ID、当前显示标题/状态和固定提示词；不复制备注、外链、准备任务列表、正文、会话或授权。未保存字段、待关联的任务选择及在途操作会禁用入口；进入 `/ai` 后仍需用户显式带入问题、重新选 `work+actions` 并人工发送。模型必须先用 `workspace_get(type=content_item,id=...)` 读取最新事实，才可起草待确认 `content_item.*` 建议；外链仍是未抓取的不可信文本。已发布事实可由用户在原生入口或 H2-AD 独立确认卡登记，详见 [内容条目交给智能体](ai-assistant.md#内容条目交给智能体h4-s)。

- `/content-calendar` 已替换为真实本地工作区：包含月格、无排期和已归档视图，可切换月份、按状态筛选月格、显示平台与准备任务进度，并从表单新建内容条目；加载、空和连接错误均有反馈状态。
- schema v37 已新增空的 `content_items` 与 `content_item_tasks`：标题、平台、状态、计划/确认发布时间、计划 IANA 时区、Project、备注、外部链接文本、排序、归档来源、版本及准备任务的 required 关联均受 SQLite 约束；迁移不创建 demo 数据，业务导入导出覆盖两表。
- schema v38 新增 `content_item` Inbox 来源契约：只接受与当前内容版本、状态、排期、时区一致的 `review_due`/`publish_due` payload，来源键固定为 `content:<id>:<event_type>:<version>`；来源身份不可变、同键唯一，活动来源会阻止永久删除内容。迁移不创建 Inbox Item。
- Sidecar 启动补偿和统一 15 秒周期扫描已接入：`in_review` 在计划时刻生成 P2 审核事项，`scheduled` 生成 P1 发布事项；单批最多处理 100 条，查询排除已投影版本，连续扫描可推进积压。相同内容版本、事件类型重复扫描或重启不会重复创建。
- 内容资料、状态、改期或准备 Task 关系引起版本变化时，同事务解决旧的活动 Inbox 来源；人工发布确认、取消/归档同样终结旧事项。归档内容永久删除时，终态来源先写入 `source_deleted_at` 并保留快照和 Inbox 审计事件。
- Inbox 列表和详情已能严格解析 `content_item` 来源，显示审核/发布类型、计划时间、IANA 时区、内容版本及内容日历入口；来源被删除后保留解释快照但隐藏活动链接。
- 月格卡片与未删除的 Inbox 内容来源统一写入 `/content-calendar?item=<content-item-id>`；页面以既有 `GET /api/v1/content-items/:id` 独立读取指定最新详情，所以目标条目不在当前月份、筛选或已加载分页中也可直达。详情读取提供加载、失败重试和关闭入口；关闭只删除 `item`，保留其余查询上下文。
- Go model 与 `/api/v1/content-items` API 已交付：列表的时间范围/平台/状态/项目筛选，创建、读取、带 `If-Match` 的编辑、改期、人工发布确认、准备 Task 的关联/解除关联，以及“仅归档后永久删除”。所有写操作在 SQLite 事务内加载版本并返回 ETag。
- 已发布条目保留本地发布确认时间；在没有历史归档字段前，API 明确拒绝将已发布条目归档，避免静默丢失发布事实。未发布条目可归档并可恢复为明确的非终态。
- 点击列表条目可打开详情弹窗，编辑标题、平台、状态、项目和备注，独立保存改期，并由 owner 明确确认本地已发布和记录外部链接文本。每个动作使用当前 ETag 版本，`VERSION_CONFLICT` 后刷新列表；外部链接不会被应用访问。
- 详情弹窗列出真实准备 Task，可从当前 Project 的任务（无 Project 时为全部任务）中选择并标记 required/optional，或解除关联；关联写入成功后关闭详情并刷新最新条目版本，避免继续使用旧 ETag。
- 当前为包含前后月日期的固定六周月格；前端以每页 100 条稳定连续读取整个可见范围，聚合全部已加载页面，并在后台加载后续页时显示读取进度。可编辑条目可拖到任一可见日期，包括相邻月份日期；聚焦卡片后也可用 `Alt+←/→` 逐日移动。两种入口都保留条目原 IANA 时区和墙上发布时间，月格按该时区归入日期，使用当前版本写入；DST 不存在时间会阻止写入并提示改用详情表单，键盘移动越过六周边界时提示切换月份或使用详情。
- 已发布、已归档和已取消条目不可拖拽；详情中的 `datetime-local` 表单使用相同 IANA/DST 转换，作为键盘和精确时间替代。月度工作区的新建表单要求计划日期；无排期视图可新建无计划时间的内容，不会生成到期 Inbox。
- 历史内容日历视觉原型已移除，以当前 React 实现、本文和 PRD 为准。
- 拖拽与卡片键盘逐日改期通过同一页面内排期覆盖立即把卡片预移到目标日期，保存期间冻结重复移动、月份/筛选切换和条目编辑；失败或版本冲突立即移回原日期、显示错误并回读服务端事实，成功后保留预览直到失效查询返回新版本，避免旧缓存闪回。
- 内容正文、外部发布、准备任务阻塞的专用内容来源和真实浏览器大数据验收仍待后续阶段。
- AI 独立轨道 H2-H 已接 `workspace_get(content_item)`：返回版本、平台、状态、排期/IANA 时区、发布确认时间、Project、备注、外链文本、required 进度及最多 50 条准备 Task 摘要，截断会显式标记；外链标注为不可信文本且不会被工具访问。`work+actions` 可提议新建、编辑、排期/取消排期、送审、取消/重开、归档/恢复，逐项人工确认前不写入，确认时重验版本和冻结预览并同步终结旧 Inbox 投影。H2-R 进一步接通已归档条目的永久删除：空 changes、精确 ID/版本，卡片冻结准备 Task 关系和待标记来源事项数量，并要求独立人工永久删除同意。H2-T 又接通 `link_task/set_task_required/unlink_task`：模型必须使用安全详情或 Task 查询得到的 Content Item ID/版本、Task ID/版本和明确 required 值；卡片冻结任务标题、状态、版本、当前关系和条目新旧版本。
- H2-T 的关系确认会重新核对完整预览，并与人工接口共用 `mutateContentItemTaskInTransaction`；新增、调整必需性和解除是三个互斥命令，无关系、重复关系、required 无变化或任一版本变化都会拒绝旧卡。审批决定、关系写入、条目版本递增和旧 Inbox 投影终结同事务提交；失败整体回滚。该能力只改变 `content_item_tasks`，不会创建、分派、完成、取消或删除 Task。
- `content_item.delete` 确认时重算完整影响并复用原生删除事务；活动来源 Inbox Item 会在提议或确认阶段阻止。成功只删除 Content Item 和级联关系，Task、Project 与 Inbox 审计快照保留，终态来源写入 `source_deleted_at`，回执返回 `/content-calendar` 列表。H2-AD 的 `published` 本地事实登记单独要求用户核实并勾选，手工重排、外部平台调用、外链抓取或关联 Project/Task 状态修改仍不开放；这些边界不能由提示词、普通 update 或关系命令绕过。其他确认结果使用 `/content-calendar?item=<id>` 回到最新详情，后续回执不携带备注、链接或 Task 正文。

## 目标功能

- 月视图和月份切换，按用户时区显示计划发布日期。
- 新建、编辑、查看、归档内容条目，支持标题、平台、状态、发布时间、项目和准备任务关联。
- 在月历中拖拽调整发布日期，并提供键盘/表单替代操作。
- 从内容条目创建或关联准备任务，展示任务完成情况但不复制任务状态。
- 审核临期、计划发布时间到达或准备任务阻塞时生成本地 Inbox Item。
- 用户手动确认已发布、取消或归档，所有变化保留本地审计。

## 关键用户流程

H4-H 已补齐 AI 来源往返：消息/确认卡的内容链接由 UI 附加来源会话，详情弹窗在成功、加载和失败时均可返回原对话；H4-AT 补齐未保存草稿及写入期间禁用返回，关闭详情须等待写入完成并明确舍弃未保存更改。关闭只移除 `item`，保留返回身份并在页面顶部继续提供入口；查看与返回不发布内容、不调用模型或恢复权限。

1. **创建排期**：用户填写标题、目标平台、计划时间、状态、项目和所需准备任务。
2. **月历规划**：用户切换月份，查看每日条目及状态；跨月拖拽后服务端原子保存新时间。
3. **准备工作**：从条目详情创建或关联 Task，回到任务模块执行；内容页只展示派生完成情况。
4. **审核提醒**：计划审核或发布时间临近时，本地调度器创建去重 Inbox Item。
5. **来源直达**：用户从 Inbox 内容来源打开指定内容 ID；页面独立读取最新详情，不要求先切换到排期所在月份。关闭后只清理定位参数。
6. **手动发布**：用户在外部平台完成发布后回到应用，明确标记为已发布并可记录外部链接文本；应用不验证或访问该链接。
7. **失败恢复**：拖拽、编辑或任务关联失败时恢复旧值，保留可重试输入并显示请求 ID。

## 数据、API、状态与事件

### 数据

- 新增 `content_items`：`id`、标题、平台、状态、计划发布时间、实际确认发布时间、项目、备注、外部链接文本、排序、版本及审计时间。
- 新增 `content_item_tasks` 关联表，支持准备任务的必需/可选标记和唯一组合约束。
- 时间戳以 RFC 3339 UTC 保存，并保留用于月历解释的用户 IANA 时区。
- 草稿正文或素材若后续加入，应使用受控本地文件引用；不得把大文件或任意绝对路径直接写入普通字段。

### API

- `GET /api/v1/content-items`：支持 `scheduled_from` / `scheduled_to`（RFC 3339 带偏移时刻，转换 UTC 的半开区间）、`platform`、`status`、`project_id`、`schedule_state=scheduled|unscheduled`、`include_archived`、`task_state=all|any_incomplete|required_incomplete` 与分页筛选。时间比较/排序保持纳秒精度，条目及准备进度在同一只读事务返回。
- `POST /api/v1/content-items`、`GET /api/v1/content-items/:id`、`PATCH /api/v1/content-items/:id` 与 `DELETE /api/v1/content-items/:id?confirm=true`。创建不接受 `published`/`archived`；发布和归档通过受控写入完成；永久删除只允许已归档的未发布条目。
- `PUT /api/v1/content-items/:id/schedule`：`scheduled_at` 与 IANA `scheduled_timezone` 必须同时提交，排期自动转换为 UTC 并将可编辑条目标为 `scheduled`。
- `POST /api/v1/content-items/:id/publish-confirmation`：owner 明确确认本地已发布，可写入外部链接文本；不访问、验证或发送到该链接。
- `GET / POST /api/v1/content-items/:id/tasks` 与 `DELETE /api/v1/content-items/:id/tasks/:taskId`：准备任务仅保存关系与 required 标记，并返回由 Task 状态派生的 required 完成度。

除创建、读取与列表外，所有变更均要求 `If-Match: "<version>"`；冲突返回 `409 VERSION_CONFLICT`，成功响应的 `ETag` 为新版本。

列表支持月份范围、平台、状态、项目和分页筛选；重排/改期使用幂等键和乐观并发。

### 状态与事件

- 当前状态：`draft / in_review / scheduled / published / cancelled / archived`，由 schema v37 固化。
- “准备完成度”由关联必需任务派生，不作为独立可写状态。
- 事件示例：`content_item.created`、`content_item.rescheduled`、`content_item.review_due`、`content_item.publish_due`、`content_item.published_confirmed`。
- 调度事件键包含内容 ID、事件类型和计划版本，跨重启补偿扫描不得重复提醒。

## 与其他模块协作

- **任务**：准备工作由 Task 执行；内容日历只维护关联和派生进度，不直接绕过任务状态机。
- **项目**：内容可关联项目用于筛选和上下文，项目删除策略必须可解释。
- **收件箱**：审核、计划发布和阻塞事件进入本地工作受理流程。
- **路线图**：可共享项目和季度上下文，但两者不互相复制状态。
- **自动化**：只创建本地 Inbox Item、Task 或 Reminder；不调用任何内容平台。
- **知识库/AI**：用户可显式选择本地资料作为草稿上下文；产出需人工验收，发布始终由 owner 完成。

## 分阶段实施

1. **CC1 数据契约（已完成）**：状态、计划时区、内容任务关联、Project/Task 删除保护和版本约束已由 schema v37 固化。
2. **CC2 API 闭环（已完成）**：CRUD、时间范围查询、改期、手动发布确认、任务关联、乐观锁、保护性归档/删除与 API 测试已交付。
3. **CC3 基础界面（已完成）**：月份切换、状态筛选、本月列表、新建和完整反馈状态已交付。
4. **CC4 排期交互（已完成当前范围）**：CC4-A 的详情编辑、表单改期、人工发布确认、乐观锁刷新和错误反馈，以及 CC4-B 的六周月格、跨日/可见跨月拖拽、卡片 `Alt+←/→` 逐日改期、IANA/DST 保持和表单替代均已交付；两种月格入口均立即视觉预移，失败/冲突完整回滚并回读服务端。
5. **CC5 工作协同（已完成当前范围）**：CC5-A 已交付 Task 关联、required 标记、解除关联和派生完成度；CC5-B 已由 schema v38 和 Sidecar 固化 Inbox 来源、版本化去重、启动/周期扫描、旧投影终结与删除协调，React Inbox 可展示并回到内容日历。准备任务阻塞的专用内容来源留到后续范围。
6. **CC6 稳定性（部分完成）**：CC6-A 已交付可见六周范围的 100 条/页自动连续读取、跨页聚合、进度与后续页错误重试；组件层已覆盖拖拽与键盘逐日改期，继续覆盖真实浏览器大数据、指针/焦点、窄屏和更多夏令时/并发边界。
7. **CC7 跨模块定位（已完成当前范围）**：月格与 Inbox 共享 URL 指定详情，单条 API 读取不依赖当前月格列表；关闭保留其他查询参数。外部分享和跨设备链接不在本地首版范围。
8. **CC8 AI 安全桥接（H2-H/H2-R/H2-T/H2-Z/H2-AD，已完成当前范围）**：安全详情读取、结构化排期/准备进度筛选与任务分页、九类普通受审批操作、三类准备 Task 关系命令、带独立同意的已归档永久删除，以及独立核实后的本地发布事实登记已接通。内容命令不改 Task 状态，Task 操作仍经独立审批及原生验收门禁；外部发布不开放。真实 Provider 措辞与桌面操作体验另行验收。

## 验收标准

- 月视图在跨月、跨年、用户时区和夏令时边界显示正确。
- 拖拽和表单改期结果持久化；失败或并发冲突时完整回滚且可重试。
- 准备完成度只由关联任务派生，解除关联和任务取消均有明确行为。
- 调度补偿不会重复创建审核/发布提醒。
- 标记 `published` 必须由 owner 明确确认，不能由定时器、AI、Agent 或规则自动完成。
- 加载、空、错误、重试、筛选和大量条目均有测试；真实浏览器覆盖拖拽、键盘和窄屏。
- 详情未保存字段/待关联任务阻止返回；四种关闭入口统一要求明确舍弃，取消确认保留草稿。六类在途写入期间不可关闭或继续编辑；失败保留、成功明确关闭且来源会话与其他查询参数保持。组件回归不替代真实桌面焦点/指针验收。
- 断网完整可用，代码、配置和网络测试均证明没有自动连接或发布到外部平台。

## 相关 PRD 与代码链接

- [产品 PRD](../opc-workspace-PRD.md)（§5.8、§10.7）
- [路线注册](../../apps/web/src/App.tsx)
- [内容日历页面](../../apps/web/src/pages/ContentCalendarPage.tsx)
- [内容日历组件测试](../../apps/web/src/pages/ContentCalendarPage.test.tsx)
- [AI 内容日历动作](../../services/sidecar/internal/api/ai_content_item_actions.go)
- [内容准备任务共享命令](../../services/sidecar/internal/api/content_item_task_commands.go)
- [AI 内容日历确定性测试](../../services/sidecar/internal/api/ai_content_item_actions_test.go)
- [任务页](../../apps/web/src/pages/TasksPage.tsx)
