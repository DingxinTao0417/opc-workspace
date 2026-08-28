# 设置模块

> 文档状态：部分实现；当前 schema v24。v0.1-A 已交付由 schema v16 引入的版本化 `app_settings`、服务端 schema 清洗、GET/PATCH API、前端 Query 接入和旧设置按模块兼容迁移；schema v17–v24 的保存视图、客户/项目扩展和 Artifact/Task Inbox 来源不改变设置契约。Focus Core 已完成设置运行态解耦，“人员与责任”已接真实 Actor API，“数据与备份”已接一致性备份完整闭环与基础业务 JSON 下载，“关于”已接真实健康与版本事实。头像仍是仅保存在本地 WebView 的兼容 Data URL；受控头像文件、数据导入和完整桌面诊断入口仍是后续范围。

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

- “个人资料”控制侧栏工作区品牌名称与头像，不改写 owner Actor 身份；名称通过 `app_settings.workspace.display_name` 保存，头像暂以本地兼容 Data URL 保存，支持 PNG/JPG/WebP、2 MB 限制和预览。
- 通用：默认首页、右侧概览开关和减少动效。
- 外观：亮色与暗色主题，支持保存前预览。
- 专注：时长、休息时长、循环次数、自动开始休息/专注和结束提示音。
- 人员与责任：从真实 `/api/v1/actors` 读取固定 owner/system 与 person，支持新建/编辑/启用/停用 person，并可单独编辑 owner 展示名称。该模块每次操作独立保存，不经过设置弹窗的全局保存按钮。
- 数据与备份：从真实 `/api/v1/backups` 读取本机备份，支持可选说明的幂等创建、展示 schema/Artifact 数量/大小、显式重新完整校验、不触碰当前数据的隔离恢复演练、二次确认后创建自动回滚点并安排下次启动恢复，以及二次确认永久删除有效或损坏包；同时可从 `/api/v1/exports/business-data` 下载版本化业务 JSON。具备加载、空、错误、进行中与成功状态，不把不含文件正文的 JSON 冒充完整备份。
- 关于：按需读取真实 `/health`，展示 Sidecar、应用名/运行版本/commit、API 版本、schema 与 SQLite 可用性；具备加载、错误、request ID、重试、手动重新检查和最近成功结果降级展示。该只读模块不显示保存/恢复默认操作。
- 应用启动由 `SettingsBootstrap` 在渲染业务界面前读取四个服务端模块；加载失败展示可重试的全屏错误，不使用可能过期的默认值进入应用。
- 当前设置状态明确分为三层：服务端确认值是 committed，弹窗表单是本地 draft，store 的 `preview` 只供可逆预览。保存成功后才以服务端规范化响应替换 committed；取消丢弃 preview。
- Zustand persist 新键 `opc-settings-local-v1` 只保留尚未受控文件化的头像 Data URL，不再持久化工作区名称、通用、外观或专注设置。
- Focus 页齿轮可直接打开 focus 模块；弹窗 draft 可以预览下一轮时长，但创建 Session 与全局 Focus ticker 都只读取 committed 设置。
- 命令面板可分别直达个人资料、通用、外观、专注、人员与责任、数据与备份和关于模块；关闭设置后通用 Modal 恢复触发元素焦点。
- 活动 Session 的 `planned_seconds` 是服务端事实。修改、保存或取消 Focus draft/preview 都不会重置、缩短或改写当前 Session；保存后的 break、cycle、自动开始与提示音配置最早在当前工作段结束后的本地转场生效。

当前 Sidecar 设置事实层已实现：

- schema v16 新增空表 `app_settings`，迁移不写默认行、不改写既有业务事实，也不创建 demo 数据；GET 对缺失模块返回服务端默认值并显式标记 `stored=false / version=0`。
- 固定模块 key 为 `workspace / general / appearance / focus`；每个值必须是完整 JSON object，服务端拒绝未知、缺失、null 非空字段、越界值和未受控头像引用。
- `PATCH /api/v1/settings` 可一次原子保存 1–4 个不同模块；每项携带 `expected_version`，缺失行要求 0，旧版本返回 `409 SETTINGS_VERSION_CONFLICT`，任一项失败整批回滚。
- 写入者固定为当前内置 owner；数据库 trigger 要求 active Actor、禁止改变 key 和硬删除设置行。
- 每个成功模块写一条不可变 `settings_updated` Workflow Event；事件只记录 stored/version/schema 元数据，不写设置值、头像引用或敏感凭据。

当前限制：

- 四个非敏感设置模块已经以 SQLite 为统一事实源；头像仍以 Data URL 存入当前浏览器或 WebView 的 localStorage，尚未迁入受控文件目录，因此头像暂不跨前端运行容器共享。
- 版本冲突会刷新 Query 并保留当前 draft，要求用户基于最新值再次确认；当前没有字段级三方合并。
- 默认首页草稿会立即导航；取消虽然返回原路由，但预览与运行状态耦合较紧。
- 已有 Actor 设置页、手动备份完整闭环和基础业务 JSON 下载，任务详情也已接负责人/审核人选择与分派历史；仍没有通知、数据导入、快捷键、完整诊断或 Agent 设置页。
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
- v0.2 增加 Adapter 与 agent Actor 管理、能力摘要、健康检查、启停和隔离边界。
- 未注册或不健康的本地执行器不能创建可分派 Agent。

### 通知、快捷键与启动

- 管理应用内提醒和原生本地通知偏好。
- 显示系统权限、当前可用性和打开系统设置的引导。
- 管理托盘、开机启动和 OS 全局快捷键时，先由桌面层报告平台能力。
- v0.1 可展示固定快捷键及冲突诊断；自定义快捷键归入 v0.3。

### 数据与备份

- 手动创建一致性备份、导出、校验并查看结果。
- 选择恢复包，先校验再确认原子恢复。
- 显示数据目录、备份目录、最近成功时间和失败诊断。
- v0.3 增加外部备份目录、计划、保留策略和高级导入。
- 彻底删除数据属于独立危险操作，必须二次确认并与普通“恢复默认设置”分离。

### 关于与诊断

- **已完成基线**：展示真实应用版本、commit、API 版本、schema 版本和 Sidecar 状态；Sidecar 构建脚本注入 Git 短提交并为未提交工作树追加 `-dirty`，也支持 CI 显式覆盖；前端严格校验响应契约，失败不回退为硬编码事实。
- 显示当前数据目录、日志目录、平台与架构，不展示令牌。
- 提供重新检查服务、手动恢复 Sidecar、打开日志和复制脱敏诊断信息。
- 清楚区分当前已实现能力、后续版本和平台不支持能力。

## 关键用户流程

### 编辑并保存设置

当前实现流程：

1. 应用启动先从 `GET /api/v1/settings` 读取服务端 committed；用户打开设置弹窗时据此建立独立 draft 和 preview。
2. 用户修改可预览项；preview 只影响可逆界面，Focus Session 始终继续以服务端快照计时。
3. 用户保存时，前端只提交发生变化的模块，每项携带当前 `expected_version`；头像单独写入本地兼容存储。
4. Sidecar 在一个事务中规范化并保存全部模块；成功后前端以返回快照更新 Query 和 committed，Focus 新设置不追写当前 Session。
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
3. workspace/general/appearance/focus 的缺失模块在同一个 PATCH 中原子回填；没有旧缓存时不为默认值创建无意义的设置行。
4. 响应成功后使用服务端返回快照；若写请求发生冲突、网络中断或超时，则重新读取并且只有在全部目标模块已经存在时才接受为成功，否则保留旧键供下次重试。
5. 服务端事实验证成功后才删除历史键。历史头像暂复制到新本地头像键；新键即使显式保存 `null` 也优先于残留旧头像，避免头像被复活。
6. 受控头像文件导入仍待后续纵切；本轮不会把 Data URL 写入 `app_settings`。

### 管理 person Actor

1. owner 打开“人员与责任”，页面从 Sidecar 读取内置 owner/system 与 person 列表，并提供加载、错误重试和空状态。
2. 新建 person，填写名称、备注和可选 JSON object；界面明确说明这只是本机责任记录。
3. 编辑请求携带当前 `If-Match`；版本冲突时列表刷新并提示载入最新内容。
4. person 没有活动 Assignment 时可停用或重新启用；存在活动 Assignment 时返回冲突并保持原状态。
5. 当前没有 Actor 硬删除入口；person 可在任务详情用于新分派，停用前必须先结束或改派其活动记录。

### 创建并校验手动备份

1. owner 打开“数据与备份”，页面读取内部 backup root 的已发布 UUID 包；损坏 manifest 显示 invalid，而不是从文件名猜测成功。
2. owner 可填写最多 200 字说明并点击“立即备份”；前端为同一次尝试保留稳定 Idempotency-Key，显示长操作进行中并阻止重复点击。
3. Sidecar 获取维护写锁，等待普通业务请求、Focus heartbeat 与 Reminder 扫描结束；随后用 `VACUUM INTO` 创建 SQLite 一致性快照并复制全部 active Task Artifact / Client Attachment 文件和身份 marker。
4. staging 包只有在逐项 size/SHA-256、预期文件全集、数据库 quick/foreign-key/schema/identity/active Artifact 校验全部通过后才原子发布；失败不改变当前数据库或 Artifact root。
5. 页面刷新列表并展示创建时间、说明、schema、文件数量和总大小；owner 可点击“重新校验”再次逐字节验证并刷新 `verified_at`。
6. owner 可点击“恢复演练”；Sidecar 再次校验源包，在隔离临时数据根复制、打开/迁移数据库、声明 Artifact store 并逐文件复验，成功或失败后都清理临时数据，不改当前数据库、Artifact root 或源备份。
7. owner 可点击“恢复此备份”进入二次确认；确认后 Sidecar 重验目标并为当前状态创建完整自动回滚包，成功挂起后页面锁定普通备份操作并显示目标/回滚短 ID。
8. 桌面模式下 owner 点击“立即安全重启”；Tauri 等待受管 Sidecar 优雅退出（必要时仅终止精确子进程）并确认退出后重启应用。浏览器开发模式不接管外部 Sidecar，页面提示手动停止并重新启动本地服务。
9. owner 可对任意有效或 invalid 包点击“删除备份”并二次确认；Sidecar 先把精确 UUID 包移入隐藏删除态再永久清理，删除不改变当前数据。
10. owner 可点击“下载 JSON”；Sidecar 在单事务内生成 format v1 业务表白名单快照，浏览器或 WebView 以服务端安全文件名保存。页面明确提示文件正文未包含，完整恢复仍使用已校验备份。

### 数据恢复

1. 用户在“数据与备份”选择已校验的本地包，先可运行无副作用的恢复演练。
2. 用户点击“恢复此备份”，界面显示目标时间/短 ID、自动回滚与关闭重开的影响，再进行二次确认。
3. Sidecar 同步重验目标、创建当前数据回滚包并发布 pending plan；只有全部成功才返回 `202`。
4. 设置页显示目标和回滚短 ID，冻结其余备份动作；桌面模式可一键安全重启，浏览器开发模式提供手动重启说明。
5. Sidecar 在数据库和 Artifact lease 打开前执行替换和最终复验；失败恢复旧资源并拒绝启动，成功后正常进入应用。启动失败的桌面恢复页与脱敏日志入口仍待开发。

## 数据/API/状态与事件

### app_settings

| 字段                | 用途                                                       |
| ------------------- | ---------------------------------------------------------- |
| key                 | 模块化稳定 key，例如 workspace、general、appearance、focus |
| value_json          | 经服务端 schema 清洗的非敏感值                             |
| schema_version      | 当前固定为 1；未知版本不能被旧 Sidecar 覆盖                |
| version             | 乐观并发版本                                               |
| updated_by_actor_id | 修改者；交互设置通常为 owner                               |
| updated_at          | UTC 更新时间                                               |

设置 schema 由 Sidecar 按模块版本化。schema v16 不预置默认行：缺失模块由 GET 返回默认值、`stored=false` 和 `version=0`，供一次性旧设置迁移判断；首次 PATCH 创建为 version 1。未知字段不能无条件回写，降级版本不得覆盖新版本设置。

### API

| 方法与路径                        | 用途                                                                                                            |
| --------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| GET /api/v1/settings              | **已实现**：按稳定顺序返回四个非敏感模块、默认/存储标记、设置 schema、版本、修改者和时间                        |
| PATCH /api/v1/settings            | **已实现**：原子更新 1–4 个模块；每项要求完整值和 `expected_version`，返回全部服务端规范化结果                  |
| GET / POST /api/v1/actors         | **已实现**：分页/筛选 Actor 或幂等创建 person；创建返回 `ETag`                                                  |
| GET / PATCH /api/v1/actors/:id    | **已实现**：详情与 `If-Match` 更新；person 可改资料/状态，owner 只改展示名称，system 不可编辑，活动分派阻止停用 |
| GET / POST /api/v1/backups        | **已实现**：列出本地包，或在维护写锁内幂等创建并完整校验 SQLite+Artifact 备份                                   |
| POST /api/v1/backups/:id/verify   | **已实现**：重新验证 UUID 包的 manifest、文件全集、哈希、marker 和临时数据库事实                                |
| POST /api/v1/backups/:id/drill    | **已实现**：在隔离临时根复制并打开/迁移数据库、声明 Artifact store、逐文件验证后清理，不改当前数据              |
| POST /api/v1/backups/:id/restore  | **已实现**：要求 `confirm=true`，重验目标、创建当前状态回滚包并挂起；下次 Sidecar 启动前原子替换和最终复验      |
| DELETE /api/v1/backups/:id        | **已实现**：要求查询参数 `confirm=true`，原子移入隐藏删除态后永久清理；损坏包可删，不安全文件系统项拒绝         |
| GET /api/v1/exports/business-data | **已实现**：下载 format v1 单事务业务白名单快照；文件仅元数据，排除凭据、绝对路径和运行维护表                   |
| GET /health                       | 提供真实 app、commit、API 和 schema 版本                                                                        |

备份、隔离恢复演练和实际恢复由数据管理 API 提供，设置页只通过 Query/Mutation 展示服务端返回事实，不建立第二份备份状态。恢复计划成功后，设置页调用桌面 `restart_application` command；该命令不接受业务参数，也不绕过 Sidecar 的 pending 恢复协议。浏览器开发模式明确降级为手动重启，Agent Adapter 继续由本地 Agent 模块负责。

### 前端状态

- committed：最近一次由启动读取或保存响应确认的服务端设置；仅头像是本地兼容值。
- draft：当前弹窗内尚未保存的编辑值。
- preview：仅用于主题、布局和下一轮 Focus 参数等可逆展示，不写入活动 Focus Session 或其他业务事实。
- saving / error：保存中和可重试错误。
- capability：由桌面层或 Sidecar 返回的当前平台能力，只读展示。

当前 Actor 创建、更新和停用已写 `actor_created / actor_updated / actor_deactivated` Workflow Event；设置 API 的每个成功模块写 `settings_updated`，仅含 stored/version/schema 元数据，整批失败不留事件。备份创建/校验事实当前只写备份 manifest，不进入 Task/Project `workflow_events`；未来诊断事件需单独设计。`settings_migrated`、`backup_started` 和 `desktop_capability_changed` 仍是规划，敏感值不能进入任何事件。

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

### v0.1-B：兼容迁移与头像

- **部分完成**：一次性迁移 `opc-focus-settings`；旧 displayName 只回填缺失的 `app_settings.workspace`，不得覆盖或改写 owner Actor 名称，其他字段也只回填未存储模块。成功验证后删除旧键，模糊失败保留旧值重试。
- **待完成**：将头像导入受控目录，补格式、大小、丢失和清理策略；当前只迁移到 `opc-settings-local-v1` 本地兼容值。
- 验证浏览器开发环境与桌面 WebView 的升级路径。

### v0.1-C：设置页面补齐

- “人员与责任”的 Actor 管理、任务详情 Assignment 入口，以及“数据与备份”的创建/列表/校验/恢复演练/重启恢复/确认删除/业务 JSON 下载已完成；通知、数据导入、快捷键和完整诊断模块待实现。
- **已完成**：UI store、Focus 页入口和命令面板均支持指定 activeModule；命令面板注册全部当前设置模块的直达入口。
- **已完成**：展示真实健康和版本信息，移除硬编码“关于”运行事实，并提供加载、失败重试、手动重新检查和只读页脚。
- **已完成（组件层）**：持久化设置加载、保存中、保存失败和冲突提示；仍需真实浏览器/窄屏的键盘、焦点和视觉验收。

### v0.1-D：运行态解耦

- **已完成（Focus Core）**：committed/draft/preview 分离；专注设置的 draft/preview 不改变活动 Session，取消不损失进度，新 committed 值只影响后续创建或工作段后的本地转场。
- 默认首页预览不破坏当前导航。
- 数据恢复、Sidecar 重启等长操作使用各自任务状态，不阻塞普通设置保存。

### v0.2 与 v0.3

- v0.2 增加本地 Agent Adapter、健康和能力设置。
- v0.3 增加备份计划、外部目录、高级导入和快捷键自定义。
- 任何远程 Provider、在线 Updater 或云同步设置都需要新的 ADR 与用户明确授权，不属于当前路线。

## 验收标准

- **已验证**：非敏感设置写入 SQLite 后可重新读取，前端启动与保存均消费严格校验的服务端快照。
- **已验证（自动化）**：旧 localStorage 只回填服务端缺失模块，不覆盖已有值；原子迁移失败可重试且不丢旧值，显式清空的新头像不会被残留旧值恢复。
- app_settings、日志和诊断信息中不包含会话令牌、Agent 能力令牌或持久敏感凭据。
- **已验证（API）**：保存返回服务端规范化值；并发旧版本更新返回 409；批量中任一冲突会整批回滚。
- 取消主题和布局预览能完整恢复；关闭后焦点返回触发元素。
- 修改、保存或取消专注设置不重置活动 Session，也不丢失已消耗进度。
- Focus 页齿轮和命令面板均可直接打开指定设置模块；关闭后焦点返回触发元素。
- person UI 已明确说明不会发送或同步；停用受活动 Assignment、active Client contact 关联，以及 Client Activity/Attachment/Project Note/Project Attachment 历史外键保护，历史分派基础由 schema v7 建立并在当前 schema v24 延续。schema v12–v24 的 Inbox、Reminder、编排、设置、保存视图、客户/项目扩展和 Artifact/Task 来源迁移不改变 Assignment 约束。
- “关于”显示真实 app、commit、API、schema 和 Sidecar 状态，不使用硬编码运行事实；加载、无服务、重试和最近成功数据均有明确状态。
- “数据与备份”只在 Sidecar 完成 SQLite+Artifact 全量验证、隔离恢复演练、安全挂起恢复、永久删除或业务 JSON 生成/下载后显示相应成功；列表、空态、读取失败、创建中、创建失败、重新校验、演练中/失败、恢复/删除二次确认、导出中/失败、挂起提示和 invalid 包均有明确状态。
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
- [设置启动门禁](../../apps/web/src/components/SettingsBootstrap.tsx)
- [设置兼容迁移](../../apps/web/src/settings/bootstrap.ts)
- [设置 API 契约测试](../../apps/web/src/api/settings.test.ts)
- [当前 Actor 设置](../../apps/web/src/components/ActorSettings.tsx)
- [当前 Actor 设置测试](../../apps/web/src/components/ActorSettings.test.tsx)
- [当前备份设置](../../apps/web/src/components/BackupSettings.tsx)
- [当前备份设置测试](../../apps/web/src/components/BackupSettings.test.tsx)
- [当前通用 Modal](../../apps/web/src/components/Modal.tsx)
- [当前健康 API](../../services/sidecar/internal/api/router.go)
- [当前设置 API](../../services/sidecar/internal/api/settings.go)
- [schema v16 设置迁移](../../services/sidecar/internal/database/migrations/016_app_settings.sql)
- [设置 API 测试](../../services/sidecar/internal/api/settings_test.go)
- [备份 API](../../services/sidecar/internal/api/backups.go)
- [隔离恢复演练](../../services/sidecar/internal/api/backup_drill.go)
- [重启前安全恢复](../../services/sidecar/internal/api/backup_restore.go)
- [确认删除](../../services/sidecar/internal/api/backup_delete.go)
- [备份 API 测试](../../services/sidecar/internal/api/backups_test.go)
