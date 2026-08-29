# 设置模块

> 文档状态：部分实现；当前 schema v35。schema v34 新增独立 Agent Adapter 诊断事实，schema v35 新增 Client Followup 计划/终态事实，均不改变 `app_settings` 契约；设置左栏已接第 10 个“本地 Agent”模块。设置持久化、受控头像、Focus 解耦、低空间阈值、Actor、自动化、Adapter 登记/诊断、备份/导入导出、诊断与启动恢复均已交付。非空目标/跨 schema 高级导入、数据库打开前恢复进度和可执行 Agent Runner 仍是后续范围。

## 定位与边界

设置模块负责用户可控、可持久化的工作区偏好和本地能力配置。它统一管理工作区品牌、通用行为、外观、专注、Actor 入口、通知、数据与备份、快捷键和诊断，但不拥有这些业务模块的运行事实；owner 名称由 Actor 模块拥有。

边界约束：

- 非敏感用户设置以版本化 app_settings 为事实源。
- appDataDir/config 只保存数据库启动前必需的非敏感运行配置和迁移标记。
- Sidecar 会话令牌、Agent 单次能力令牌、API 密钥等敏感信息不得进入 app_settings、localStorage、日志或命令行；持久凭据使用操作系统安全存储。
- 设置草稿与已保存值分离。可逆的主题和布局可以预览，取消后必须完整恢复。
- 设置页只配置策略，不复制 Focus Session、Sidecar 状态、备份任务或 Agent Run 的业务状态。
- 当前阶段完全本地，不提供账号、云同步、远程通知或线上服务设置。
- 在线 Updater 不在当前阶段；设置页不得出现会暗示自动联网更新已经可用的开关。

## 当前实现状态

当前设置弹窗已实现：

- “个人资料”控制侧栏工作区品牌名称与头像，不改写 owner Actor 身份；名称通过 `app_settings.workspace.display_name` 保存，头像经严格 multipart 导入 `artifacts/avatars/`，支持 PNG/JPG/WebP、2 MiB 限制、选择即预览、取消恢复、保存后鉴权读取和确认移除。
- 通用：默认首页、右侧概览开关和减少动效。
- 外观：亮色与暗色主题，支持保存前预览。
- 专注：时长、休息时长、循环次数、自动开始休息/专注和结束提示音。
- 人员与责任：从真实 `/api/v1/actors` 读取固定 owner/system 与 person，支持新建/编辑/启用/停用 person，并可单独编辑 owner 展示名称。该模块每次操作独立保存，不经过设置弹窗的全局保存按钮。
- 自动化：从真实 `/api/v1/automations` 读取五个稳定预设；支持依赖状态、优先级或当地时间/IANA 时区即时服务端预览、规则配置独立保存、启停、下一次运行、最近 Run、空/加载/错误和失败手动重试。发票/Agent 预设明确 unavailable；该模块使用独立规则版本，不经过 `app_settings` 或设置弹窗全局保存按钮。
- 本地 Agent：从真实 `/api/v1/agent-adapters` 读取代码所有清单；支持空状态幂等登记、能力/安全闸门、手动诊断、加载/错误重试和未就绪启用禁用。当前诊断固定为隔离未验证，不启动进程、不创建 agent Actor/Assignment/Run；该模块使用 Adapter 自身版本，不经过 `app_settings` 或设置弹窗全局保存按钮。
- 数据与备份：可预览并保存 1–100 GiB 低空间提醒阈值，默认 1 GiB、下一轮扫描生效；可手动刷新数据库/受控文件/备份三个逻辑位置的容量，展示健康、低空间和局部不可用状态但不展示路径或探测错误。从真实 `/api/v1/backups` 读取本机备份并完成创建、校验、演练、恢复、删除；手工创建和导入/恢复内部回滚包在写入备份 staging 前通过仅探测 backup root 的容量准入，空间不足或容量无法确认时显示清理/刷新指引并保留未提交输入，不伪造成功或自动重试。恢复需求同时覆盖当前回滚点和 pending 目标副本。启动恢复诊断显示待重启、本次已应用、清理残留、失败隔离或无效记录，并可重新检查。可分别下载或导入版本化业务 JSON 与包含 manifest/活动受控文件的 ZIP。两类导入都先预检再确认，仅允许当前 schema、终态 Focus 且目标为空；应用前通过容量准入并自动创建已校验回滚备份。
- 关于：按需读取真实 `/health`，展示 Sidecar、应用名/运行版本/commit、API 版本、schema 与 SQLite 可用性；具备加载、错误、request ID、重试、手动重新检查和最近成功结果降级展示。该只读模块不显示保存/恢复默认操作。
- 运行诊断：联合 `/health` 与桌面 `sidecar_status` 展示浏览器开发/Tauri 环境、生命周期、app/API/schema 与版本兼容；支持重新检查、错误重试、复制脱敏摘要和下载诊断包 v1。桌面返回先经白名单规范化，`sessionToken`、`baseUrl` 和原始 `message` 不进入诊断对象、UI 或 ZIP。
- 应用启动由 `SettingsBootstrap` 在渲染业务界面前读取五个服务端模块；加载失败展示可重试的全屏错误，不使用可能过期的默认值进入应用。
- 当前设置状态明确分为三层：服务端确认值是 committed，弹窗表单是本地 draft，store 的 `preview` 只供可逆预览。保存成功后才以服务端规范化响应替换 committed；取消丢弃 preview。
- Zustand persist 不再保存头像内容；运行态只持有由鉴权 content 响应创建的 Blob URL。历史 `opc-settings-local-v1` 仅作为一次性迁移源，服务端事实确认后清理。
- Focus 页齿轮可直接打开 focus 模块；弹窗 draft 可以预览下一轮时长，但创建 Session 与全局 Focus ticker 都只读取 committed 设置。
- 命令面板可分别直达个人资料、通用、外观、专注、人员与责任、本地 Agent、自动化、数据与备份、运行诊断和关于模块；关闭设置后通用 Modal 恢复触发元素焦点。
- 活动 Session 的 `planned_seconds` 是服务端事实。修改、保存或取消 Focus draft/preview 都不会重置、缩短或改写当前 Session；保存后的 break、cycle、自动开始与提示音配置最早在当前工作段结束后的本地转场生效。

当前 Sidecar 设置事实层已实现：

- schema v16 新增空表 `app_settings`，迁移不写默认行、不改写既有业务事实，也不创建 demo 数据；GET 对缺失模块返回服务端默认值并显式标记 `stored=false / version=0`。
- schema v29 通过破坏性迁移闸门在保留既有设置行、版本、Actor 和时间的前提下扩展 `storage` key，并重建 active Actor、key 不可变、硬删除保护和头像引用 triggers；迁移前由启动链创建已验证回滚包。
- 固定模块 key 为 `workspace / general / appearance / focus / storage`；每个值必须是完整 JSON object，服务端拒绝未知、缺失、null 非空字段、越界值和未受控头像引用。`storage.low_space_threshold_gib` 仅允许 1–100 的整数。
- `PATCH /api/v1/settings` 可一次原子保存 1–5 个不同模块；每项携带 `expected_version`，缺失行要求 0，旧版本返回 `409 SETTINGS_VERSION_CONFLICT`，任一项失败整批回滚。
- 写入者固定为当前内置 owner；数据库 trigger 要求 active Actor、禁止改变 key 和硬删除设置行。
- 每个成功模块写一条不可变 `settings_updated` Workflow Event；事件只记录 stored/version/schema 元数据，不写设置值、头像引用或敏感凭据。
- schema v27 新增 `workspace_avatars` 与 `workspace_avatar_deletion_tombstones`；最多一个 active 头像，路径固定 `avatars/<uuid>.<png|jpg|webp>`，记录 MIME、size、SHA-256 和完整性状态，并与 Task/Client/Project 受控文件 ID 互斥。
- `POST /api/v1/settings/avatar` 以首 part `manifest` + 可选唯一 `file` 原子提交 replace/remove 和 1–5 个设置模块；通用 PATCH 只能原样携带已有 `avatar_ref`，不能伪造或绕过受控文件入口。
- `GET /api/v1/settings/avatar/content` 只读取当前 active 引用，逐次复验 size/SHA-256，缺失或篡改时更新完整性事实并拒绝输出。

当前限制：

- 五个非敏感设置模块和工作区头像引用均以 SQLite/受控文件为事实源；Blob URL 只用于当前 WebView 展示，不是持久事实。
- 版本冲突会刷新 Query 并保留当前 draft，要求用户基于最新值再次确认；当前没有字段级三方合并。
- 默认首页草稿会立即导航；取消虽然返回原路由，但预览与运行状态耦合较紧。
- 已有 Actor、自动化、Agent Adapter 诊断、低空间阈值、备份/导入导出和脱敏诊断；但仍没有通知、非空目标/跨 schema 冲突合并、快捷键、数据库打开前恢复进度或可执行 Agent Runner。Adapter 设置只登记代码清单并显示 blocked，不等于 agent 身份、分派或执行能力。
- 通用 Modal 已支持 Escape、背景关闭、初始聚焦、Tab 焦点圈闭和关闭后焦点恢复；仍需补真实浏览器与窄屏验收。

## 目标功能

### 工作区资料与 owner 身份

- `app_settings.workspace` 保存工作区名称和品牌头像，只影响侧栏/窗口展示，不作为责任人或审计身份。
- `actors.display_name` 是 owner 名称的唯一事实源，通过 Actor 管理单独编辑；工作区改名不得同步修改 owner。
- 工作区头像导入应用控制的文件目录，设置只保存受控引用和必要元数据；owner 若未来需要独立头像，另由 Actor 资料字段定义。
- 移除头像、文件丢失和格式不支持时提供可恢复反馈。

### 通用与外观

- 默认首页、右侧概览、减少动效和主题。
- 草稿预览可取消；保存失败时恢复已提交值，不留下半保存状态。
- 默认首页只影响下次进入根路由，不在编辑草稿时破坏当前页面上下文。
- 后续可增加语言、日期格式和时区，但业务日期仍由统一 IANA 时区解释。

### 专注

- 配置默认专注/休息时长、循环、自动开始和提示音。
- 活动 Focus Session 期间保存的新参数只作用于下一轮，不能重置或改写当前 Session。
- 所有入口可以指定打开 focus 设置模块。
- 显示原生通知与系统勿扰能力是否可用及所需授权。

### Actor 与本地 Agent

- v0.1 Actor 管理已接 SQLite/API：person 可编辑名称、备注、非敏感 metadata、启用和停用；owner 只允许编辑展示名称，system 只读，当前没有 Actor 删除路由。
- 明确提示 person 只记录本地责任，不发送任务、不创建账号、不授予访问权限。
- 客户联系人只有用户显式创建或关联后才成为 person Actor。
- person 存在活动 Assignment 时，API 与数据库共同拒绝停用并提示先改派；任务详情已提供 Assignment 创建、改派和结束入口。
- v0.2-A 已增加代码所有 Adapter 登记、能力摘要、健康诊断、启停边界和隔离未验证提示；agent Actor 管理和实际执行仍待 Runner 安全闸门通过后实现。
- 未注册或不健康的本地执行器不能创建可分派 Agent。

### 通知、快捷键与启动

- 管理应用内提醒和原生本地通知偏好。
- 显示系统权限、当前可用性和打开系统设置的引导。
- 管理托盘、开机启动和 OS 全局快捷键时，先由桌面层报告平台能力。
- v0.1 可展示固定快捷键及冲突诊断；自定义快捷键归入 v0.3。

### 数据与备份

- 手动创建一致性备份、导出、校验并查看结果。
- 手动备份在创建任何 staging/SQLite 快照前按数据库与 active 文件载荷加安全余量检查 backup root；空间不足或容量无法确认时保留说明草稿并给出可操作反馈。
- 选择恢复包，先校验再确认原子恢复。
- 显示数据目录、备份目录、最近成功时间和失败诊断。
- 设置 1–100 GiB 低空间提醒阈值；修改时就地展示提醒口径，保存后由 Sidecar 下一轮扫描读取。
- 手动容量检查只返回三个固定逻辑位置、状态与容量；阈值草稿不会伪装为本次检查口径，页面同时显示服务端实际使用的已保存阈值。
- v0.3 增加外部备份目录、计划、保留策略和高级导入。
- 彻底删除数据属于独立危险操作，必须二次确认并与普通“恢复默认设置”分离。

### 关于与诊断

- **已完成基线**：展示真实应用版本、commit、API 版本、schema 版本和 Sidecar 状态；Sidecar 构建脚本注入 Git 短提交并为未提交工作树追加 `-dirty`，也支持 CI 显式覆盖；前端严格校验响应契约，失败不回退为硬编码事实。
- **已完成脱敏运行诊断**：浏览器模式明确外部开发进程；桌面模式读取 `sidecar_status` 后只保留环境、`starting/ready/error` 生命周期和三类版本。版本与 `/health` 对照，摘要只包含环境、生命周期、版本、commit 和 health 状态。
- **已完成诊断包 v1**：`GET /api/v1/diagnostics/package` 返回 manifest、runtime、database、maintenance 四个 JSON 的 ZIP；数据库部分只含 quick-check 结果、foreign-key/journal/page 摘要和迁移清单，维护部分只含错误码、状态、数量与最近时间。包不含业务行、附件、原始日志、Token、端口、地址、路径或底层错误。
- 显示当前数据目录、日志目录、平台与架构，不展示令牌。
- 已提供重新检查服务、复制脱敏诊断摘要、下载诊断包和桌面打开日志目录；该 command 不接受路径，浏览器模式禁用。Tauri 壳只写白名单生命周期 JSONL 并与 Sidecar 分文件轮转；WebView→Sidecar request ID 已可关联前端错误与访问日志。全局启动故障恢复页 v1 在设置 bootstrap 之前运行，并复用状态重查、日志入口和安全应用重启。
- 清楚区分当前已实现能力、后续版本和平台不支持能力。

## 关键用户流程

### 编辑并保存设置

当前实现流程：

1. 应用启动先从 `GET /api/v1/settings` 读取服务端 committed；用户打开设置弹窗时据此建立独立 draft 和 preview。
2. 用户修改可预览项；preview 只影响可逆界面，Focus Session 始终继续以服务端快照计时。
3. 用户保存时，前端只提交发生变化的模块，每项携带当前 `expected_version`；若头像 replace/remove，则强制包含 workspace 更新并走 multipart 入口。
4. Sidecar 先把新头像写入 staging/受控目录，再在一个 SQLite 事务中写头像身份/旧头像墓碑和全部设置；失败时补偿新文件，成功后清理旧文件。前端以返回快照更新 Query，并重新鉴权读取头像。
5. 网络、超时、校验或版本冲突失败时弹窗保留 draft 和预览并展示错误，不把未确认值写成 committed。
6. 用户取消或关闭，preview 被丢弃，committed 与活动 Session 保持不变。

### 取消预览

1. 用户修改主题、布局或其他可预览选项。
2. 用户按 Escape、点击取消或关闭。
3. 前端恢复打开时的已提交快照和原路由。
4. Focus Session、Sidecar 和其他业务运行态不发生改变。

### 首次从 localStorage 迁移

1. 启动先读取 `app_settings`，再清洗历史键 `opc-focus-settings`；格式损坏的缓存不会被当作有效迁移源。
2. 只为 `stored=false / version=0` 的模块生成更新；已经存在的服务端模块始终优先，不被旧缓存覆盖。
3. workspace/general/appearance/focus 的缺失模块在同一个 PATCH 中原子回填；storage 没有历史 localStorage 来源，保持服务端默认且不创建无意义的设置行。
4. 响应成功后使用服务端返回快照；若写请求发生冲突、网络中断或超时，则重新读取并且只有在全部目标模块已经存在时才接受为成功，否则保留旧键供下次重试。
5. 服务端已有 `avatar_ref` 时始终优先，不接受 localStorage 覆盖；服务端无头像时，显式本地头像快照优先于更旧缓存，Data URL 被解码为 File 并与缺失模块更新一次原子提交。
6. 只有重新读取服务端确认 `avatar_ref` 与设置模块后才删除历史键；Data URL 不写入 SQLite、事件、日志或业务导出。

### 管理 person Actor

1. owner 打开“人员与责任”，页面从 Sidecar 读取内置 owner/system 与 person 列表，并提供加载、错误重试和空状态。
2. 新建 person，填写名称、备注和可选 JSON object；界面明确说明这只是本机责任记录。
3. 编辑请求携带当前 `If-Match`；版本冲突时列表刷新并提示载入最新内容。
4. person 没有活动 Assignment 时可停用或重新启用；存在活动 Assignment 时返回冲突并保持原状态。
5. 当前没有 Actor 硬删除入口；person 可在任务详情用于新分派，停用前必须先结束或改派其活动记录。

### 创建并校验手动备份

1. owner 打开“数据与备份”，页面读取内部 backup root 的已发布 UUID 包；损坏 manifest 显示 invalid，而不是从文件名猜测成功。
2. owner 可填写最多 200 字说明并点击“立即备份”；前端为同一次尝试保留稳定 Idempotency-Key，显示长操作进行中并阻止重复点击。
3. Sidecar 获取维护写锁和备份互斥锁，先查找同键同说明的已发布幂等结果；命中时直接重放，不再次探测容量或创建包。没有结果时，以 `max(page_count × page_size, 数据库文件大小)`、安全解析并复核实际大小与登记值一致的全部 active 受控文件、marker 和 manifest 上界估算载荷，再增加 20% 且最低 64 MiB 余量，并且只探测 backup root；文件不一致时在探测和 staging 前安全拒绝。
4. 可用空间小于需求时返回 HTTP 507 `BACKUP_SPACE_INSUFFICIENT`，容量无法可靠确认时返回 HTTP 503 `BACKUP_CAPACITY_UNAVAILABLE`；恰好等于需求允许继续。响应不含路径、盘符、精确容量或底层错误；拒绝不创建 staging/新包、不修改业务数据，也不生成 generic `backup:create` Inbox Item。页面分别提示清理备份位置/旧备份，或刷新容量状态并确认本地存储可用；当前 note 草稿保持原值，没有成功提示，也不会自动重试。
5. 通过门禁后 Sidecar 才用 `VACUUM INTO` 创建 SQLite 一致性快照并复制全部 active Task/Client/Project 受控文件、Workspace Avatar 和身份 marker。
6. staging 包只有在逐项 size/SHA-256、预期文件全集、数据库 quick/foreign-key/schema/identity/active Artifact 校验全部通过后才原子发布；失败不改变当前数据库或 Artifact root。
7. 页面刷新列表并展示创建时间、说明、schema、文件数量和总大小；owner 可点击“重新校验”再次逐字节验证并刷新 `verified_at`。
8. owner 可点击“恢复演练”；Sidecar 再次校验源包，在隔离临时数据根复制、打开/迁移数据库、声明 Artifact store 并逐文件复验，成功或失败后都清理临时数据，不改当前数据库、Artifact root 或源备份。
9. owner 可点击“恢复此备份”进入二次确认；确认后 Sidecar 重验目标并为当前状态创建完整自动回滚包，成功挂起后页面锁定普通备份操作并显示目标/回滚短 ID。该内部回滚包不经过上述手动 HTTP 容量门禁。
10. 桌面模式下 owner 点击“立即安全重启”；Tauri 等待受管 Sidecar 优雅退出（必要时仅终止精确子进程）并确认退出后重启应用。浏览器开发模式不接管外部 Sidecar，页面提示手动停止并重新启动本地服务。
11. owner 可对任意有效或 invalid 包点击“删除备份”并二次确认；Sidecar 先把精确 UUID 包移入隐藏删除态再永久清理，删除不改变当前数据。
12. owner 可点击“下载 JSON”；Sidecar 在单事务内生成 format v1 业务表白名单快照，浏览器或 WebView 以服务端安全文件名保存。页面明确提示文件正文未包含，完整恢复仍使用已校验备份。
13. owner 可点击“下载含文件 ZIP”；Sidecar 在维护写锁内生成 manifest、同格式业务 JSON 和全部活动受控文件，逐项复验 size/SHA-256，完成后才下载。
14. owner 可选择业务 JSON 或含文件 ZIP；页面先上传预检并展示 schema/总行数，ZIP 额外展示文件数与字节数。只有当前 schema、终态 Focus 和空目标才可确认；Sidecar 再次预检，通过只探测 backup root 的回滚包容量准入后创建已校验回滚包并原子应用，ZIP 还会无覆盖发布文件并在数据库提交前复验正文。容量不足或无法确认时页面显示专用提示且保留当前选择，业务事实不变。
15. 页面读取启动恢复诊断；待重启计划即使关闭再打开设置也继续冻结备份写操作。已恢复、清理残留、失败隔离和无效记录只显示安全状态/计数，不显示路径或底层错误，也不提供未经确认的自动清理。

### 数据恢复

1. 用户在“数据与备份”选择已校验的本地包，先可运行无副作用的恢复演练。
2. 用户点击“恢复此备份”，界面显示目标时间/短 ID、自动回滚与关闭重开的影响，再进行二次确认。
3. Sidecar 同步重验目标、创建当前数据回滚包并发布 pending plan；只有全部成功才返回 `202`。
4. 设置页显示目标和回滚短 ID，冻结其余备份动作；桌面模式可一键安全重启，浏览器开发模式提供手动重启说明。
5. Sidecar 在数据库和 Artifact lease 打开前执行替换和最终复验；失败恢复旧资源并拒绝启动，成功后正常进入应用。全局恢复页会在数据库打开前显示白名单恢复/迁移阶段，可打开脱敏日志并安全重启；启动前备份选择仍待开发。

## 数据/API/状态与事件

### app_settings

| 字段                | 用途                                                           |
| ------------------- | -------------------------------------------------------------- |
| key                 | 模块化稳定 key：workspace、general、appearance、focus、storage |
| value_json          | 经服务端 schema 清洗的非敏感值                                 |
| schema_version      | 当前固定为 1；未知版本不能被旧 Sidecar 覆盖                    |
| version             | 乐观并发版本                                                   |
| updated_by_actor_id | 修改者；交互设置通常为 owner                                   |
| updated_at          | UTC 更新时间                                                   |

设置 schema 由 Sidecar 按模块版本化。schema v16 不预置默认行；schema v29 只扩展允许的 storage key，不写默认行。缺失模块由 GET 返回默认值、`stored=false` 和 `version=0`，供一次性旧设置迁移判断；首次 PATCH 创建为 version 1。未知字段不能无条件回写，降级版本不得覆盖新版本设置。

### API

| 方法与路径                                    | 用途                                                                                                                                                                                                                                                                     |
| --------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| GET /api/v1/settings                          | **已实现**：按稳定顺序返回五个非敏感模块、默认/存储标记、设置 schema、版本、修改者和时间                                                                                                                                                                                 |
| PATCH /api/v1/settings                        | **已实现**：原子更新 1–5 个模块；每项要求完整值和 `expected_version`，返回全部服务端规范化结果                                                                                                                                                                           |
| GET /api/v1/diagnostics/storage               | **已实现**：按物理卷去重后手动检查三个固定逻辑位置，返回已保存阈值、容量、状态与 `shared_volume`；不返回卷 ID、路径、盘符或探测错误                                                                                                                                      |
| POST /api/v1/settings/avatar                  | **已实现**：严格 multipart replace/remove；头像文件与全部设置更新共同成功或失败，通用 PATCH 不可改头像引用                                                                                                                                                               |
| GET /api/v1/settings/avatar/content           | **已实现**：鉴权读取当前头像，复验 MIME/size/SHA-256，缺失或篡改拒绝输出                                                                                                                                                                                                 |
| GET / POST /api/v1/actors                     | **已实现**：分页/筛选 Actor 或幂等创建 person；创建返回 `ETag`                                                                                                                                                                                                           |
| GET / PATCH /api/v1/actors/:id                | **已实现**：详情与 `If-Match` 更新；person 可改资料/状态，owner 只改展示名称，system 不可编辑，活动分派阻止停用                                                                                                                                                          |
| GET / POST /api/v1/backups                    | **已实现**：列出本地包；手动创建在双锁内先重放幂等结果，否则按 SQLite/active 文件/marker/manifest + 20%（最低 64 MiB）余量仅探测 backup root。空间不足返回 507，容量无法确认返回 503，拒绝无副作用且不投影 generic incident；通过后才完整创建并校验 SQLite+Artifact 备份 |
| POST /api/v1/backups/:id/verify               | **已实现**：重新验证 UUID 包的 manifest、文件全集、哈希、marker 和临时数据库事实                                                                                                                                                                                         |
| POST /api/v1/backups/:id/drill                | **已实现**：在隔离临时根复制并打开/迁移数据库、声明 Artifact store、逐文件验证后清理，不改当前数据                                                                                                                                                                       |
| POST /api/v1/backups/:id/restore              | **已实现**：要求 `confirm=true`，重验目标，按回滚包+pending 目标副本执行容量准入后创建回滚包并挂起；空间不足/无法确认返回脱敏 507/503，下次 Sidecar 启动前原子替换和最终复验                                                                                             |
| DELETE /api/v1/backups/:id                    | **已实现**：要求查询参数 `confirm=true`，原子移入隐藏删除态后永久清理；损坏包可删，不安全文件系统项拒绝                                                                                                                                                                  |
| GET /api/v1/exports/business-data             | **已实现**：下载 format v1 单事务业务白名单快照；文件仅元数据，排除凭据、绝对路径和运行维护表                                                                                                                                                                            |
| GET /api/v1/exports/business-package          | **已实现**：下载 format v1 含文件 ZIP；manifest/业务 JSON/活动受控文件完整生成并逐项校验，失败不返回部分包                                                                                                                                                               |
| POST /api/v1/imports/business-data/preview    | **已实现**：strict 预检同 schema 无文件 JSON，返回表/总行数与空目标门禁                                                                                                                                                                                                  |
| POST /api/v1/imports/business-data            | **已实现**：固定确认头、维护写锁内先执行回滚包容量准入，再创建回滚备份并原子应用；容量拒绝不改业务事实                                                                                                                                                                   |
| POST /api/v1/imports/business-package/preview | **已实现**：strict 预检含文件 ZIP 的 manifest、业务 JSON、文件全集/哈希和空目标门禁                                                                                                                                                                                      |
| POST /api/v1/imports/business-package         | **已实现**：独立确认头、回滚包容量准入、回滚备份、文件无覆盖发布、DB 提交前正文复验与失败补偿                                                                                                                                                                            |
| GET /api/v1/backups/restore-diagnostics       | **已实现**：读取脱敏 pending/applied/failed/invalid 状态；设置页恢复重启门禁并支持重新检查                                                                                                                                                                               |
| GET /health                                   | 提供真实 app、commit、API 和 schema 版本                                                                                                                                                                                                                                 |

备份、隔离恢复演练和实际恢复由数据管理 API 提供，设置页只通过 Query/Mutation 展示服务端返回事实，不建立第二份备份状态。恢复计划成功后，设置页调用桌面 `restart_application` command；该命令不接受业务参数，也不绕过 Sidecar 的 pending 恢复协议。浏览器开发模式明确降级为手动重启，Agent Adapter 继续由本地 Agent 模块负责。

### 前端状态

- committed：最近一次由启动读取或保存响应确认的服务端设置；头像展示 URL 来自当前服务端受控文件响应。
- draft：当前弹窗内尚未保存的编辑值。
- preview：仅用于主题、布局和下一轮 Focus 参数等可逆展示，不写入活动 Focus Session 或其他业务事实。
- saving / error：保存中和可重试错误。
- capability：由桌面层或 Sidecar 返回的当前平台能力，只读展示。

当前 Actor 创建、更新和停用已写 `actor_created / actor_updated / actor_deactivated` Workflow Event；设置 API 的每个成功模块写 `settings_updated`，仅含 stored/version/schema 元数据，整批失败不留事件。备份创建/校验事实当前只写备份 manifest，不进入 Task/Project `workflow_events`；手工及导入/恢复自动回滚包的容量准入拒绝也不创建通用备份故障 Inbox/Event。未来诊断事件需单独设计。`settings_migrated`、`backup_started` 和 `desktop_capability_changed` 仍是规划，敏感值不能进入任何事件。

## 与其他模块协作

- [Actor 与分派](actors.md)：设置页维护 person 和内置 Actor 展示；Assignment 状态由任务模块管理。
- [本地 Agent](local-agents.md)：v0.2 管理 Adapter、Agent 健康和能力，Run 生命周期仍归 Agent 模块。
- [专注](focus.md)：提供下一轮默认参数，不改写活动 Session。
- [命令与搜索](command-search.md)：入口可直接打开指定设置模块，快捷键配置由桌面能力约束。
- [数据管理](data-management.md)：设置页发起和展示备份恢复，不自行复制任务状态。
- [桌面平台](desktop-platform.md)：读取通知、托盘、自启、全局快捷键、文件对话框和服务健康能力。
- [今日](today.md)：默认首页、右侧概览和减少动效影响展示，不改变今日数据口径。

## 分阶段实施

### v0.1-A：设置数据契约

- **后端已完成**：schema v16 `app_settings` 递增迁移、服务端 schema 清洗和 GET/PATCH API。
- **后端已完成**：固定模块 key、完整值契约、默认值、原子批量保存、乐观锁、`SETTINGS_VERSION_CONFLICT` 和无敏感值审计。
- **前端已完成**：启动门禁、严格响应校验、Query 缓存、按变化模块保存、版本冲突刷新、保存错误保留 draft，以及服务端响应驱动 committed。

### v0.1-B：兼容迁移与头像（已完成）

- **已完成**：一次性迁移 `opc-focus-settings`；旧 displayName 只回填缺失的 `app_settings.workspace`，不得覆盖或改写 owner Actor 名称，其他字段也只回填未存储模块。成功验证后删除旧键，模糊失败保留旧值重试。
- **已完成**：旧 Data URL 在服务端无头像时导入受控目录；已存在的服务端头像优先。PNG/JPG/WebP、2 MiB、缺失/篡改、替换/删除墓碑、启动协调及备份恢复已有约束和测试。
- 验证浏览器开发环境与桌面 WebView 的升级路径。

### v0.1-C：设置页面补齐

- “人员与责任”、Assignment，“数据与备份”的完整备份链、启动后恢复结果诊断、业务 JSON/含文件 ZIP 空工作区安全导入导出，以及脱敏诊断、Sidecar/Tauri 壳日志、桌面打开日志目录和含白名单恢复/迁移进度的全局启动故障恢复页已完成；通知、非空目标/跨 schema 冲突合并导入、快捷键和启动前备份选择待实现。
- **已完成**：UI store、Focus 页入口和命令面板均支持指定 activeModule；命令面板注册全部当前设置模块的直达入口。
- **已完成**：展示真实健康和版本信息，移除硬编码“关于”运行事实，并提供加载、失败重试、手动重新检查和只读页脚。
- **已完成**：运行诊断区分浏览器/Tauri 环境，白名单化桌面 Sidecar 状态，对照 health 版本，可复制脱敏摘要并下载诊断包 v1；命令面板可精确直达。
- **已完成（组件层）**：持久化设置加载、保存中、保存失败和冲突提示；仍需真实浏览器/窄屏的键盘、焦点和视觉验收。

### v0.1-D：运行态解耦

- **已完成（Focus Core）**：committed/draft/preview 分离；专注设置的 draft/preview 不改变活动 Session，取消不损失进度，新 committed 值只影响后续创建或工作段后的本地转场。
- 默认首页预览不破坏当前导航。
- 数据恢复、Sidecar 重启等长操作使用各自任务状态，不阻塞普通设置保存。

### v0.2 与 v0.3

- [x] v0.2-A 增加本地 Agent Adapter 登记、健康诊断和能力/闸门设置；Runner 与 agent Actor 分派继续延期。
- v0.3 增加备份计划、外部目录、高级导入和快捷键自定义。
- 任何远程 Provider、在线 Updater 或云同步设置都需要新的 ADR 与用户明确授权，不属于当前路线。

## 验收标准

- **已验证**：非敏感设置写入 SQLite 后可重新读取，前端启动与保存均消费严格校验的服务端快照。
- **已验证（自动化）**：旧 localStorage 只回填服务端缺失模块，不覆盖已有值；原子迁移失败可重试且不丢旧值，显式清空的新头像不会被残留旧值恢复。
- **已验证（API/组件）**：选择头像立即预览但不写服务端；取消恢复原头像；保存通过 multipart 原子提交文件与设置；替换/移除保留不可变墓碑，内容端点复验完整性。
- app_settings、日志和诊断信息中不包含会话令牌、Agent 能力令牌或持久敏感凭据。
- **已验证（API）**：保存返回服务端规范化值；并发旧版本更新返回 409；批量中任一冲突会整批回滚。
- 取消主题和布局预览能完整恢复；关闭后焦点返回触发元素。
- 修改、保存或取消专注设置不重置活动 Session，也不丢失已消耗进度。
- Focus 页齿轮和命令面板均可直接打开指定设置模块；关闭后焦点返回触发元素。
- person UI 已明确说明不会发送或同步；停用受活动 Assignment、active Client contact 关联，以及 Client Activity/Attachment/Project Note/Project Attachment 历史外键保护，历史分派基础由 schema v7 建立并在当前 schema v35 延续。schema v12–v29 的其他迁移不改变 Assignment 约束；schema v30–v35 的扩展不改变 Actor、Assignment 或设置表契约。
- “关于”显示真实 app、commit、API、schema 和 Sidecar 状态，不使用硬编码运行事实；加载、无服务、重试和最近成功数据均有明确状态。
- “运行诊断”不展示、复制或打包会话令牌、监听地址、原始错误、本地路径和业务正文；桌面状态畸形时拒绝使用，浏览器开发模式不伪造 Tauri 事实。诊断包严格限制四个白名单 JSON，并明确不含原始日志。
- “数据与备份”只在 Sidecar 完成 SQLite+Artifact 全量验证、隔离恢复演练、安全挂起恢复、永久删除、业务 JSON 或含文件 ZIP 完整导出/导入后显示相应成功；列表、空态、读取失败、创建中、创建失败、重新校验、演练中/失败、恢复/删除二次确认、两类导入导出中/失败、预检阻断、挂起提示和 invalid 包均有明确状态。
- **已验证（API/组件定向）**：手动创建空间不足与容量无法确认分别显示清理备份位置/旧备份和刷新容量状态/确认本地存储可用的提示；失败保留 note 草稿，不显示成功、不自动重试，未知错误仍保留 request ID。后端覆盖锁/重放顺序、估算溢出、精确容量放行、只探测 backup root、507/503 脱敏响应，以及拒绝后无 staging/新包/业务变化/Inbox incident。
- 启动恢复诊断只显示规范 ID、时间、状态与计数；重新打开设置可恢复待重启门禁，本次恢复成功、清理残留、失败隔离与无效记录不会泄露本机路径或底层错误，也不会触发自动删除。
- 不支持或尚未实现的桌面能力被禁用并说明原因。
- 备份、恢复和 Sidecar 恢复失败不会被设置页伪装为成功。
- 在线 Updater 不作为当前设置项、启动依赖或默认网络行为出现。

## 相关代码/PRD链接

- [PRD：专注设置模态框](../opc-workspace-PRD.md#专注设置模态框)
- [PRD：app_settings 规划](../opc-workspace-PRD.md#本地工作编排数据表taskactord2-已实现inboxagent-仍规划)
- [PRD：各模块具体实施计划](../opc-workspace-PRD.md#107-各模块具体实施计划)
- [当前设置 store](../../apps/web/src/store/settings.ts)
- [当前设置弹窗](../../apps/web/src/components/SettingsModal.tsx)
- [当前设置测试](../../apps/web/src/components/SettingsModal.test.tsx)
- [桌面诊断规范化](../../apps/web/src/api/desktop.ts)
- [设置启动门禁](../../apps/web/src/components/SettingsBootstrap.tsx)
- [设置兼容迁移](../../apps/web/src/settings/bootstrap.ts)
- [设置 API 契约测试](../../apps/web/src/api/settings.test.ts)
- [当前 Actor 设置](../../apps/web/src/components/ActorSettings.tsx)
- [当前 Actor 设置测试](../../apps/web/src/components/ActorSettings.test.tsx)
- [当前自动化设置](../../apps/web/src/components/AutomationSettings.tsx)
- [当前自动化设置测试](../../apps/web/src/components/AutomationSettings.test.tsx)
- [自动化模块契约](automation.md)
- [当前本地 Agent 设置](../../apps/web/src/components/AgentAdapterSettings.tsx)
- [当前本地 Agent 设置测试](../../apps/web/src/components/AgentAdapterSettings.test.tsx)
- [本地 Agent 模块契约](local-agents.md)
- [当前备份设置](../../apps/web/src/components/BackupSettings.tsx)
- [当前备份设置测试](../../apps/web/src/components/BackupSettings.test.tsx)
- [当前通用 Modal](../../apps/web/src/components/Modal.tsx)
- [当前健康 API](../../services/sidecar/internal/api/router.go)
- [当前设置 API](../../services/sidecar/internal/api/settings.go)
- [受控头像 API](../../services/sidecar/internal/api/settings_avatar.go)
- [schema v27 头像迁移](../../services/sidecar/internal/database/migrations/027_workspace_avatar.sql)
- [schema v16 设置迁移](../../services/sidecar/internal/database/migrations/016_app_settings.sql)
- [设置 API 测试](../../services/sidecar/internal/api/settings_test.go)
- [备份 API](../../services/sidecar/internal/api/backups.go)
- [含文件业务 ZIP API](../../services/sidecar/internal/api/business_package_export.go)
- [含文件业务 ZIP 导入 API](../../services/sidecar/internal/api/business_package_import.go)
- [含文件业务 ZIP 导入测试](../../services/sidecar/internal/api/business_package_import_test.go)
- [隔离恢复演练](../../services/sidecar/internal/api/backup_drill.go)
- [重启前安全恢复](../../services/sidecar/internal/api/backup_restore.go)
- [启动恢复结果诊断](../../services/sidecar/internal/api/backup_restore_diagnostics.go)
- [确认删除](../../services/sidecar/internal/api/backup_delete.go)
- [备份 API 测试](../../services/sidecar/internal/api/backups_test.go)
