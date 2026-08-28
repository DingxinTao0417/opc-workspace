# 设置模块

> 文档状态：部分实现；当前 schema v11。Focus Core 已完成设置运行态解耦，“人员与责任”已接真实 Actor API；除 Actor 外的现有偏好仍保存在 localStorage。把非敏感设置迁入版本化 SQLite、数据/诊断和桌面设置入口仍是后续范围。

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

- 当前“个人资料”界面实际控制侧栏品牌名称与头像（字段为 displayName/avatarDataUrl），尚未与未来 owner Actor 身份分离；支持 PNG/JPG/WebP、2 MB 限制和本地预览。
- 通用：默认首页、右侧概览开关和减少动效。
- 外观：亮色与暗色主题，支持保存前预览。
- 专注：时长、休息时长、循环次数、自动开始休息/专注和结束提示音。
- 人员与责任：从真实 `/api/v1/actors` 读取固定 owner/system 与 person，支持新建/编辑/启用/停用 person，并可单独编辑 owner 展示名称。该模块每次操作独立保存，不经过设置弹窗的全局保存按钮。
- 关于：硬编码应用版本、数据存储、桌面架构和云同步状态。
- Zustand persist 对输入进行边界清洗，历史存储键为 opc-focus-settings。
- 当前设置状态明确分为三层：persist 后的 store 值是 committed，弹窗表单是本地 draft，store 的 `preview` 只供可逆预览。保存提交 preview，取消丢弃 preview。
- Focus 页齿轮可直接打开 focus 模块；弹窗 draft 可以预览下一轮时长，但创建 Session 与全局 Focus ticker 都只读取 committed 设置。
- 活动 Session 的 `planned_seconds` 是服务端事实。修改、保存或取消 Focus draft/preview 都不会重置、缩短或改写当前 Session；保存后的 break、cycle、自动开始与提示音配置最早在当前工作段结束后的本地转场生效。

当前限制：

- 除“人员与责任”使用 SQLite Actor API 外，现有偏好仍只保存在当前浏览器或 WebView 的 localStorage；浏览器开发环境与桌面应用互不共享。
- 头像以 Data URL 存入 localStorage，尚未迁入受控文件目录。
- 没有 GET / PATCH /settings API，也没有 app_settings 表。
- 命令面板中的“专注设置”仍只打开通用设置；只有 Focus 页齿轮已直达 focus 模块。
- 默认首页草稿会立即导航；取消虽然返回原路由，但预览与运行状态耦合较紧。
- 已有 Actor 设置页，任务详情也已接负责人/审核人选择与分派历史；仍没有通知、数据/备份、快捷键、诊断或 Agent 设置页。
- “关于”没有读取真实 app、commit、API、schema 和 Sidecar 健康信息。
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

- 展示真实应用版本、commit、API 版本、schema 版本和 Sidecar 状态。
- 显示当前数据目录、日志目录、平台与架构，不展示令牌。
- 提供重新检查服务、手动恢复 Sidecar、打开日志和复制脱敏诊断信息。
- 清楚区分当前已实现能力、后续版本和平台不支持能力。

## 关键用户流程

### 编辑并保存设置

当前实现流程：

1. 用户打开设置弹窗；前端从 localStorage-backed store 的 committed 值建立独立 draft 和 preview。
2. 用户修改可预览项；preview 只影响可逆界面，Focus Session 始终继续以服务端快照计时。
3. 用户保存，清洗后的 preview 成为新的 committed 值并持久化；Focus 新设置不追写当前 Session。
4. 用户取消或关闭，preview 被丢弃，committed 与活动 Session 保持不变。

目标 SQLite 流程仍是规划：读取已提交的服务端设置，按模块 `PATCH` 并携带版本，成功后以服务端规范化响应更新 committed，失败保留 draft。

### 取消预览

1. 用户修改主题、布局或其他可预览选项。
2. 用户按 Escape、点击取消或关闭。
3. 前端恢复打开时的已提交快照和原路由。
4. Focus Session、Sidecar 和其他业务运行态不发生改变。

### 首次从 localStorage 迁移

1. 启动后读取 app_settings 和一次性迁移标记。
2. 仅当服务端对应 key 不存在时读取旧 opc-focus-settings。
3. 对个人资料、通用、外观和专注字段分别清洗后写入 SQLite。
4. 头像 Data URL 转换到受控文件并保存引用；转换失败不删除旧值。
5. 写入迁移完成标记；重复启动不覆盖较新的服务端设置。
6. 验证成功后才清理或忽略旧 localStorage。

### 管理 person Actor

1. owner 打开“人员与责任”，页面从 Sidecar 读取内置 owner/system 与 person 列表，并提供加载、错误重试和空状态。
2. 新建 person，填写名称、备注和可选 JSON object；界面明确说明这只是本机责任记录。
3. 编辑请求携带当前 `If-Match`；版本冲突时列表刷新并提示载入最新内容。
4. person 没有活动 Assignment 时可停用或重新启用；存在活动 Assignment 时返回冲突并保持原状态。
5. 当前没有 Actor 硬删除入口；person 可在任务详情用于新分派，停用前必须先结束或改派其活动记录。

### 数据恢复

1. 用户在“数据与备份”选择本地归档。
2. 应用显示 manifest、版本、校验和和将被替换的数据范围。
3. 校验完成后用户二次确认。
4. 数据管理模块完成恢复；设置页只展示进度与结果。
5. 失败时保留当前数据，并提供 request ID 与脱敏日志入口。

## 数据/API/状态与事件

### app_settings

| 字段                | 用途                                                       |
| ------------------- | ---------------------------------------------------------- |
| key                 | 模块化稳定 key，例如 workspace、general、appearance、focus |
| value_json          | 经服务端 schema 清洗的非敏感值                             |
| version             | 乐观并发版本                                               |
| updated_by_actor_id | 修改者；交互设置通常为 owner                               |
| updated_at          | UTC 更新时间                                               |

设置 schema 由 Sidecar 按模块版本化。未知字段不能无条件回写，降级版本不得覆盖新版本设置。

### API

| 方法与路径                     | 用途                                                                                                            |
| ------------------------------ | --------------------------------------------------------------------------------------------------------------- |
| GET /api/v1/settings           | **规划**：返回全部可见非敏感设置、schema 和版本                                                                 |
| PATCH /api/v1/settings         | **规划**：按模块更新，返回服务端规范化结果                                                                      |
| GET / POST /api/v1/actors      | **已实现**：分页/筛选 Actor 或幂等创建 person；创建返回 `ETag`                                                  |
| GET / PATCH /api/v1/actors/:id | **已实现**：详情与 `If-Match` 更新；person 可改资料/状态，owner 只改展示名称，system 不可编辑，活动分派阻止停用 |
| GET /health                    | 提供真实 app、commit、API 和 schema 版本                                                                        |

备份、桌面能力与 Agent Adapter 通过各自模块 API 或 Tauri command 提供；设置页不建立第二份状态。

### 前端状态

- committed：当前已保存的设置；现阶段除 Actor 外来自 localStorage-backed store，未来迁移后才代表最近一次服务端确认值。
- draft：当前弹窗内尚未保存的编辑值。
- preview：仅用于主题、布局和下一轮 Focus 参数等可逆展示，不写入活动 Focus Session 或其他业务事实。
- saving / error：保存中和可重试错误。
- capability：由桌面层或 Sidecar 返回的当前平台能力，只读展示。

当前 Actor 创建、更新和停用已写 `actor_created / actor_updated / actor_deactivated` Workflow Event；失败写入和幂等重放不重复写事件。`settings_migrated`、`settings_updated`、`backup_started` 和 `desktop_capability_changed` 仍是规划，敏感值不能进入任何 Workflow Event。

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

- 新增 app_settings 递增迁移、服务端 schema 清洗和 GET / PATCH API。
- 定义模块 key、版本、默认值、乐观锁和错误码。
- 将前端 Query 作为已提交事实源，Zustand 仅保存弹窗草稿和短期 UI 状态。

### v0.1-B：兼容迁移与头像

- 一次性迁移 opc-focus-settings：旧 displayName/avatarDataUrl 只回填 `app_settings.workspace`，不得覆盖或改写 owner Actor 名称；其他字段按模块迁移且仅在服务端不存在时回填。
- 将头像导入受控目录，补格式、大小、丢失和清理策略。
- 验证浏览器开发环境与桌面 WebView 的升级路径。

### v0.1-C：设置页面补齐

- “人员与责任”的 Actor 管理范围和任务详情 Assignment 入口已完成；通知、数据/备份、快捷键和诊断模块待实现。
- **部分完成**：UI store 和 Focus 页入口已支持指定 activeModule；命令面板直达 focus 仍待接入。
- 展示真实健康和版本信息，移除硬编码“关于”事实。
- 补真实浏览器/窄屏的键盘与焦点验收，并实现持久化设置保存错误状态。

### v0.1-D：运行态解耦

- **已完成（Focus Core）**：committed/draft/preview 分离；专注设置的 draft/preview 不改变活动 Session，取消不损失进度，新 committed 值只影响后续创建或工作段后的本地转场。
- 默认首页预览不破坏当前导航。
- 数据恢复、Sidecar 重启等长操作使用各自任务状态，不阻塞普通设置保存。

### v0.2 与 v0.3

- v0.2 增加本地 Agent Adapter、健康和能力设置。
- v0.3 增加备份计划、外部目录、高级导入和快捷键自定义。
- 任何远程 Provider、在线 Updater 或云同步设置都需要新的 ADR 与用户明确授权，不属于当前路线。

## 验收标准

- 非敏感设置重启后从 SQLite 恢复，浏览器与桌面环境不再形成两套长期事实。
- 旧 localStorage 只迁移一次，不覆盖已经存在的服务端值；迁移失败可重试且不丢旧值。
- app_settings、日志和诊断信息中不包含会话令牌、Agent 能力令牌或持久敏感凭据。
- 保存返回服务端规范化值；并发旧版本更新返回 409。
- 取消主题和布局预览能完整恢复；关闭后焦点返回触发元素。
- 修改、保存或取消专注设置不重置活动 Session，也不丢失已消耗进度。
- Focus 页齿轮可直接打开 focus 模块；命令面板仍待达到同一行为。
- person UI 已明确说明不会发送或同步；停用受活动 Assignment 保护，历史分派基础由 schema v7 建立并在当前 schema v11 延续。
- “关于”显示真实 app、commit、API、schema 和 Sidecar 状态，不使用硬编码运行事实。
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
- [当前 Actor 设置](../../apps/web/src/components/ActorSettings.tsx)
- [当前 Actor 设置测试](../../apps/web/src/components/ActorSettings.test.tsx)
- [当前通用 Modal](../../apps/web/src/components/Modal.tsx)
- [当前健康 API](../../services/sidecar/internal/api/router.go)
