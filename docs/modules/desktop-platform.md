# 桌面平台、可靠性与发布模块

> 实现基线：app v0.1.0 / API v1 / SQLite schema v22（2026-08-28）。schema v12–v22 的 Inbox、Reminder、编排、app_settings、任务保存视图、客户活动/附件/person 关联和项目笔记/附件事实均不改变 Tauri 桌面生命周期契约。桌面基座、共享受控文件运行目录接线和 Sidecar Focus/Reminder 生命周期已实现；完整异常恢复、原生通知、系统集成和发布闭环未完成。当前阶段只规划签名离线更新，不启用在线 Updater。

导航：[文档中心](../README.md) · [整体功能架构](../functional-architecture.md) · [PRD v6.1](../opc-workspace-PRD.md) · [数据管理](data-management.md) · [任务](tasks.md) · [本地提醒](reminders.md)

## 定位与边界

本模块负责把 React 前端、Go Sidecar 和 SQLite 作为一个可安装、可启动、可恢复、可退出和可升级的本地桌面产品交付。它提供操作系统能力和进程生命周期，不拥有任务、收件箱、专注或备份等业务事实。

核心边界：

- 桌面运行时采用 Tauri + 内置 React + 随安装包交付的 Go Sidecar，不要求用户安装 Node、Go、Rust 或 Docker。
- Sidecar 只监听 127.0.0.1 动态端口，生产请求使用启动期随机会话令牌和 Origin 校验。
- 核心功能在断网环境完整可用。
- 平台能力按最小权限逐项启用；不可用或未授权时必须降级并解释。
- 更新以桌面壳、前端资源和 Sidecar 的同版本签名离线包为单位。
- 在线 Updater、联网更新检查和静默下载不在当前阶段，未来若引入必须新增 ADR、网络权限、失败回退和用户开关。
- 三平台支持只能在对应安装包、签名、公证、干净机和性能测试通过后声明。

## 当前实现状态

已实现：

- Tauri 2 桌面窗口、固定最小尺寸和内置 Web 前端。
- single-instance 插件；再次启动时显示、取消最小化并聚焦主窗口。
- 生产配置通过 externalBin 打包 opc-sidecar，开发可通过 OPC_SIDECAR_URL 连接外部 Sidecar。
- Tauri 获取 appDataDir 和 appLogDir，创建数据库、附件、Artifact、发票、备份和配置目录。
- 每次启动生成随机会话令牌，以 127.0.0.1:0 启动 Sidecar。
- Tauri 将 `appDataDir/opc-workspace.db` 作为 `OPC_DB_PATH`，将 `appDataDir/artifacts/` 作为 `OPC_ARTIFACT_DIR` 传给 Sidecar；Sidecar 默认从数据库父目录解析已由 Tauri 创建的 `appDataDir/backups/`。生产路径不出现在命令行，也不写入持久前端配置。
- Sidecar 在监听前校验 Artifact root marker 的 `format_version / database_id / store_id`，用不可变数据库 ID 与一次性 `artifact_store_id` 做双向绑定，获取并持有进程级独占锁，再依据 Artifact 事实和 immutable deletion tombstone 协调 `.staging/`、`objects/`、`.trash/`、`.quarantine/`；数据库换 root、root 换数据库、错 marker 或任一步失败都会阻止 ready。同一 root 已由另一 Sidecar 使用时第二进程启动失败，避免双进程文件协调。
- 解析 stdout ready JSON，拒绝非 loopback、端口 0、带凭据或带额外路径的地址。
- Tauri 原生健康探测与前端 sidecar_status 连接握手。
- Sidecar 状态当前为 starting、ready、error，并可返回 app/API/schema 版本。
- 应用退出时向精确子进程写入 shutdown，最多等待 7 秒，超时后终止；Sidecar 优雅关闭 HTTP、checkpoint WAL 并关闭数据库。若 ready 等待恰在 shutdown 已取走 child handle 后超时，握手任务不会伪造 exited 或提前唤醒优雅等待，仍由 shutdown 完成等待与兜底终止。
- 恢复计划挂起后，设置页可调用 `restart_application`：桌面壳先走同一安全关闭路径并等待受管 Sidecar 的真实退出确认，再请求 Tauri 重启应用；若 Sidecar 不由桌面管理或退出不能确认，则取消应用重启并返回可见错误。
- Tauri capability 当前仅开放 core:default；前端不能任意调用 shell。
- manual Task 文件产出通过 WebView 文件选择与鉴权 multipart 上传进入 Sidecar 受控目录；前端不能指定服务端 `relative_path`，下载也只能经过鉴权 content API。

尚未实现：

- Sidecar 异常退出后的自动重启、退避、手动恢复和孤儿进程治理。
- 前端健康查询结果没有全局服务状态、启动失败恢复页或诊断操作。
- Go 日志没有使用 OPC_LOG_DIR 落盘、轮转或脱敏诊断包。
- 系统托盘、原生通知、OS 全局快捷键、开机启动和业务文件对话框。
- 签名离线更新包的选择、验签、迁移前备份、安装与回退。
- Windows/macOS/Linux CI 构建、代码签名、公证、安装包和干净系统验收。
- 当前主机上的 Rust 单元测试与源码检查不能替代真实 Sidecar 生命周期、安装包或三平台支持；当前环境缺少 MSVC `link.exe`，完整 Tauri 链接仍受环境限制。

## 目标功能

### 启动与服务恢复

- 建立明确桌面状态机：初始化目录、启动 Sidecar、ready 握手、健康检查、兼容校验、可用或恢复。
- 检查桌面应用、Sidecar、API 和 schema 版本兼容，阻止错误组合进入写入模式。
- 启动超时、ready 格式错误、健康失败或迁移失败时显示专用恢复页。
- 恢复页支持重试启动、打开脱敏日志、查看版本、检查数据目录和从备份恢复。
- Sidecar 运行中意外退出时进行有上限的自动重启和退避；反复失败后停止自动重启并要求用户处理。
- 启动新 Sidecar 前确认旧子进程和监听端口已经清理，避免双写数据库。

### 退出与孤儿治理

- 区分关闭窗口、最小化到托盘和真正退出。
- 真正退出先停止接受新业务操作，等待短事务，并让可取消的长任务进入 cancelled/interrupted 后再优雅关闭 Sidecar。若数据恢复已进入不可取消的 applying/restarting 阶段，桌面必须阻止普通退出，等待恢复协调器完成或回滚。
- 维护精确子进程句柄和本次启动标识，不使用宽泛进程名终止其他实例。
- 父进程异常退出后，下次启动识别并处理遗留锁、Run 和进程状态。
- Agent 子进程由本地 Agent 模块管理，但最终退出清理纳入桌面生命周期。

### 系统托盘

- 关闭主窗口默认隐藏到托盘；首次行为明确提示并允许设置。
- 托盘菜单提供显示主窗口、快速新建任务、开始/暂停专注、设置和退出。
- 托盘图标区分空闲、专注、休息和本地服务故障。
- 退出动作必须关闭 Sidecar 和数据库；隐藏窗口不得误触发退出。

### 原生本地通知

- 支持任务、收件箱、专注、发票和系统维护的本地通知。
- 首次使用前解释用途并请求最小系统权限。
- 点击通知打开对应本地资源详情。
- 通知不可用或被拒绝时保留应用内提醒，不影响业务状态。
- 当前“应用内提醒”已由 Sidecar Reminder 到期生成 Inbox Item；操作系统通知权限、通知中心和点击通知跳转仍属于后续系统集成。
- 当前阶段不发送远程推送、邮件或第三方消息。

### OS 全局快捷键

- 注册命令面板、新建任务、开始/暂停专注和页面切换快捷键。
- 注册失败、权限拒绝或与系统冲突时显示诊断，并保留 WebView 内快捷键。
- 系统快捷键只触发打开界面或安全动作，不能绕过确认、验收与权限。
- v0.3 再支持用户自定义和冲突调整。

### 文件对话框与路径授权

- 当前 Task 文件 Artifact 使用 WebView 文件选择器取得浏览器 `File`，以 multipart 上传；Sidecar 只接收字节流并复制到受控 Artifact store，不接收客户端本机绝对路径。
- 导入、导出、恢复、附件、发票和 Adapter 选择均使用原生文件对话框。
- 前端只获得受控文件引用或经过校验的用户选择，不接受任意路径字符串直接读写。
- 路径在 Rust 和 Sidecar 边界规范化，防止目录逃逸和符号链接攻击。

### 日志与诊断

- Sidecar 和 Tauri 写入 appLogDir 的脱敏轮转日志。
- 日志包含 request ID、进程阶段、版本、错误码和耗时，不包含令牌、完整客户资料、发票正文或 Agent 输入输出。
- 诊断页显示当前状态、最近失败和路径，支持复制脱敏摘要与打开日志目录。
- 系统维护失败可以幂等生成本地收件箱项。

### 签名离线更新

- 用户主动选择本地签名更新包。
- 应用验证签名、版本、平台、架构和兼容范围。
- 更新前等待写事务、创建并验证一致性备份。
- 正常关闭 Sidecar 后由安装程序替换桌面壳、前端和 Sidecar。
- 新版本启动检查版本并迁移；失败进入恢复页。
- appDataDir 与安装目录分离，正常更新不覆盖业务数据。
- 不进行在线检查、后台下载或静默更新。

### 构建与发布

- CI 构建 windows-x86_64、darwin-x86_64、darwin-aarch64 和 linux-x86_64。
- 每个 Tauri 包内置匹配 target triple 的 Sidecar。
- Windows 代码签名，macOS 签名与公证，Linux 发布 SHA-256。
- 在无开发工具和 Docker 的干净系统验证安装、首次启动、备份、更新、卸载后数据保留和彻底删除入口。

## 关键用户流程

### 正常启动

1. Tauri 获取单实例锁并初始化 appDataDir / appLogDir，包括 `artifacts/` 目录。
2. 生成启动期随机令牌并以端口 0 启动内置 Sidecar。
3. Tauri 注入数据库和 Artifact root；Sidecar 打开数据库、执行安全迁移、读取数据库身份、校验/创建绑定 marker、获取 Artifact root 独占锁并协调受控 store，全部成功后才输出 ready。
4. Tauri 校验 loopback 地址并携带令牌调用 /health。
5. 版本与 schema 兼容后，WebView 取得当前进程内连接信息并加载业务页面。
6. 核心功能从首次启动起可离线使用。

### Sidecar 启动或运行失败

1. 桌面状态进入 error，停止业务写入。
2. 若属于瞬时运行失败，按有上限退避尝试一次或少量自动重启。
3. 仍失败则显示恢复页，不在后台无限重启。
4. 用户可手动重试、打开日志、检查版本或进入备份恢复。
5. 恢复成功后生成新的会话令牌并重新建立前端连接。

### 关闭窗口与退出

1. 用户关闭主窗口时，根据设置隐藏到托盘并保持 Sidecar。
2. 用户从托盘或菜单选择“退出”。
3. 应用冻结新长任务，等待写事务并通知专注/Agent 模块处理运行态。
4. 若数据恢复处于 applying/restarting，显示不可退出的维护状态，待完成或回滚；其他可取消任务按其协议结束。
5. Tauri 请求 Sidecar 优雅关闭；超时后只终止精确子进程。
6. 数据库 checkpoint 完成，进程退出且无残留。若操作系统强制终止恢复阶段，下次启动必须依据恢复 journal 完成或回滚，不能直接打开不确定数据库。

### 安装签名离线更新

1. 用户在设置中选择本地更新包。
2. Tauri 验证签名、版本、平台和架构，并展示变更与兼容风险。
3. 用户确认后，数据模块创建更新前一致性备份。
4. 应用关闭 Sidecar，安装程序原子替换程序文件。
5. 新版本启动并执行兼容迁移。
6. 失败时进入恢复页；数据库兼容时可回退应用，否则从更新前备份恢复。

## 数据/API/状态与事件

### 当前与目标状态

当前 sidecar_status 支持：

- starting：本地服务正在启动或等待健康。
- ready：连接信息和版本可用。
- error：启动、握手、健康或运行失败。

目标桌面协调状态建议细化为：

| 状态               | 含义                                 |
| ------------------ | ------------------------------------ |
| initializing       | 创建目录和读取启动配置               |
| starting           | 启动 Sidecar 并等待 ready            |
| checking           | 健康、版本和 schema 兼容检查         |
| ready              | 业务可用                             |
| restarting         | 有上限自动或手动恢复中               |
| maintenance        | 备份、恢复、迁移或离线更新中         |
| incompatible       | 组件或 schema 不兼容，只允许恢复操作 |
| error              | 需要用户处理                         |
| stopping / stopped | 正常退出阶段                         |

前端只能依据桌面层和 Sidecar 返回的状态，不通过请求失败次数自行创造第二状态机。

### Tauri command 与事件

当前 command：

- sidecar_status：返回当前状态、运行期 API 地址、会话令牌和版本。
- restart_application：无业务参数；只在受管 Sidecar 真实退出后请求重启整个桌面应用，用于应用挂起的恢复计划。浏览器开发模式或外部 Sidecar 会被明确拒绝。

规划 command 或事件职责：

| 能力                        | 职责                                   |
| --------------------------- | -------------------------------------- |
| open_log_directory          | 打开应用日志目录                       |
| select_import / export_path | 原生文件选择和受控路径授权             |
| desktop_capabilities        | 返回托盘、通知、快捷键、自启和更新能力 |
| sidecar-state-changed       | 向 WebView 推送状态变化                |
| global-shortcut-invoked     | 打开命令面板、新建任务或切换专注       |
| notification-activated      | 打开对应本地资源                       |

正式名称在实现 ADR 中冻结。所有高风险命令限制到 main 窗口的最小 Tauri capability，并验证调用参数。

### 会话与日志

- 会话令牌每次 Sidecar 启动重新生成，只存在于进程内。
- 基础地址只能是 http://127.0.0.1:非零端口。
- 浏览器请求要求允许的 Origin 和 Bearer Token。
- 日志通过 request ID 关联 Tauri、Sidecar 和前端错误。
- Sidecar 重启使旧令牌失效，前端清除缓存连接后重新获取。
- 数据库路径和 Artifact root 由桌面层在每次启动时注入；WebView 不持有内部 `objects/<artifact-id>` 路径，也不能绕过 Sidecar 读取或删除文件。Sidecar HTTP read/write timeout 为 180 秒；Task 文件上传和下载采用 120 秒客户端端到端超时，普通小型 JSON API 继续使用较短超时。

## 与其他模块协作

- [数据管理](data-management.md)：启动迁移、恢复页、文件对话框、更新前备份和数据保留。
- [设置](settings.md)：展示平台能力、权限、托盘、自启、快捷键、诊断和离线更新入口。
- [命令与搜索](command-search.md)：OS 全局快捷键显示窗口并打开命令面板；服务状态控制业务搜索可用性。
- [专注](focus.md)：托盘控制、原生通知、退出前 Session 处理和系统勿扰引导。
- [收件箱](inbox.md)：通知点击打开资源；系统维护故障生成本地待办。
- [本地 Agent](local-agents.md)：受控进程、文件授权、沙箱、取消和退出清理。
- [任务](tasks.md) 与后续业务模块：桌面层只提供系统能力，不绕过其 API 与状态机。

## 分阶段实施

### v0.1-A：Sidecar 可靠性

- 扩展桌面状态机、状态事件和前端全局服务状态。
- 实现启动失败恢复页、手动重试和有上限退避。
- 完成孤儿进程、旧令牌失效、重复启动和数据库锁处理。
- 补 Tauri 与真实 Sidecar 进程集成测试。

### v0.1-B：日志与维护

- 接通 OPC_LOG_DIR，统一 Tauri/Go 脱敏日志、轮转和 request ID。
- 增加诊断页、打开日志和脱敏摘要。
- 接入迁移失败、备份恢复和 maintenance 状态。

### v0.1-C：系统集成

- 逐项实现托盘、原生通知、OS 全局快捷键、文件对话框和开机启动。
- 每项能力使用独立最小权限、平台检测和降级说明。
- 完成窗口关闭/隐藏/退出语义和专注状态显示。

### v0.1-D：离线更新与发布

- 定义签名离线包、兼容矩阵、验签、更新前备份和失败回退。
- 建立多平台 Sidecar 与 Tauri 构建 CI。
- 完成签名、公证、安装包、性能和干净机矩阵。

### 后续评审

- 在线 Updater 若未来进入范围，必须单独 ADR 和用户开关；当前实现、设置和权限中保持关闭。
- 新平台、远程连接或云同步需要独立安全与数据边界评审。

## 验收状态

### 当前已验证

- [x] 生产 Sidecar 配置只接受 127.0.0.1，动态端口、会话令牌与精确 Origin 契约已有测试。
- [x] single-instance 只聚焦现有窗口，不有意启动第二个桌面 Sidecar。
- [x] ready 地址为非 loopback、端口 0、带凭据或路径时被拒绝。
- [x] Tauri 创建 Artifact root 并通过 `OPC_ARTIFACT_DIR` 传给 Sidecar；开发脚本使用独立开发 root。
- [x] Sidecar 在 ready 前验证数据库绑定 marker/目录、获取 root 进程级独占锁并协调 staging/objects/trash/quarantine；错库或第二 Sidecar 共用 root 时启动失败，文件读写只经过受控 API。
- [x] 正常退出发送 shutdown，等待 drain/WAL checkpoint，超时只终止精确子进程句柄；ready 超时与 shutdown 竞态不会伪造 exited。
- [x] 恢复计划挂起后可从设置页请求安全重启；command 拒绝外部 Sidecar，并且只有真实退出确认后才重启应用。
- [x] 在线 Updater 未启用，也不是启动依赖。

### 仍待验收

- [ ] Sidecar 异常退出后的有上限重启、手动恢复与孤儿治理。
- [ ] 全局服务恢复页、日志落盘/轮转和脱敏诊断包。
- [ ] 托盘、原生通知、OS 全局快捷键、开机启动和原生业务文件对话框。
- [ ] 签名离线更新、迁移前验证备份与失败回退。
- [ ] Windows、macOS、Linux 对应签名/公证、干净机、备份恢复、更新和性能证据。
- [ ] 当前主机补齐 MSVC `link.exe` 后的完整 `cargo test` / Tauri 链接与安装包检查。

## 相关代码/PRD链接

- [PRD：技术架构方案](../opc-workspace-PRD.md#4-技术架构方案)
- [PRD：部署与分发](../opc-workspace-PRD.md#8-部署与分发)
- [PRD：T-02 Tauri 桌面壳与 Sidecar 生命周期](../opc-workspace-PRD.md#1042-t-02-tauri-桌面壳与-sidecar-生命周期)
- [PRD：MVP 技术验收标准](../opc-workspace-PRD.md#93-mvp-技术验收标准)
- [当前 Tauri 应用入口](../../apps/desktop/src-tauri/src/lib.rs)
- [当前 Sidecar 生命周期](../../apps/desktop/src-tauri/src/sidecar.rs)
- [当前 Tauri 配置](../../apps/desktop/src-tauri/tauri.conf.json)
- [当前最小 capability](../../apps/desktop/src-tauri/capabilities/default.json)
- [当前 Sidecar 进程入口](../../services/sidecar/cmd/server/main.go)
- [当前 Artifact store](../../services/sidecar/internal/api/artifact_store.go)
- [当前 API 安全中间件](../../services/sidecar/internal/api/middleware.go)
- [当前前端连接发现](../../apps/web/src/api/client.ts)
