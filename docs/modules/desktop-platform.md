# 桌面平台、可靠性与发布模块

> 实现基线：app v0.1.0 / API v1 / SQLite schema v41（2026-08-29），本轮无桌面 migration。桌面基座、数据库父目录运行锁、启动阶段恢复进度、generation-aware 内置 Sidecar 有界自动恢复、父管道 EOF 退出、前端世代清理、安全应用重启，以及托盘显示/隐藏/显式退出最小源码闭环已实现；T-02 仍部分完成，托盘原生链接/三平台交互、真实父崩溃/进程树与安装包尚未验收。当前阶段只规划签名离线更新，不启用在线 Updater。

导航：[文档中心](../README.md) · [整体功能架构](../functional-architecture.md) · [PRD v9.77](../opc-workspace-PRD.md) · [数据管理](data-management.md) · [任务](tasks.md) · [本地提醒](reminders.md)

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
- Tauri `tray-icon` 已提供最小托盘：只有托盘构建成功才拦截主窗口关闭并隐藏；左键和固定“显示 opc-workspace”恢复窗口，固定“退出 opc-workspace”进入应用退出与 Sidecar 优雅关闭。构建失败时关闭请求按原路径继续，避免隐藏后无法恢复。无配置图标时使用代码内本地 RGBA fallback，不读取外部路径。
- 生产配置通过 externalBin 打包 opc-sidecar，开发可通过 OPC_SIDECAR_URL 连接外部 Sidecar。
- Tauri 获取 appDataDir 和 appLogDir，创建数据库、附件、Artifact、发票、备份和配置目录。
- 内置 Sidecar 每个 generation 都生成新的随机会话令牌，并以 `127.0.0.1:0` 重新请求 OS 分配动态端口；端口值允许被 OS 复用。
- Tauri 将 `appDataDir/opc-workspace.db` 作为 `OPC_DB_PATH`，将 `appDataDir/artifacts/` 作为 `OPC_ARTIFACT_DIR` 传给 Sidecar；Sidecar 默认从数据库父目录解析已由 Tauri 创建的 `appDataDir/backups/`。生产路径不出现在命令行，也不写入持久前端配置。
- Go Sidecar 在检查 pending restore、执行迁移或打开 SQLite 前，先在数据库父目录对固定 `.opc-sidecar-run.lock` 获取非阻塞 OS 独占运行锁；冲突立即失败且不接触数据库。锁文件退出后可保留，所有权只由 OS lock 表示。
- Sidecar 随后校验 Artifact root marker 的 `format_version / database_id / store_id`，用不可变数据库 ID 与一次性 `artifact_store_id` 做双向绑定，获取并持有独立的 Artifact root 进程锁，再依据 Artifact 事实和 immutable deletion tombstone 协调 `.staging/`、`objects/`、`.trash/`、`.quarantine/`；数据库运行锁和 Artifact root 锁分工不同，均不能省略。
- 解析 stdout ready JSON，拒绝非 loopback、端口 0、带凭据或带额外路径的地址。
- Sidecar 在 ready 前可输出 `{"event":"startup","stage":"…"}`；Tauri 严格只接受锁、恢复包校验/应用/复验/收尾、数据库、迁移、工作区和本地 API 的固定 stage code，并映射为恢复页文案。未知 JSON、未知 stage 或非 ready 协议行会终止本代；状态不保留本机路径、备份 ID、令牌、原始错误或用户输入。
- Tauri 原生健康探测与前端 sidecar_status 连接握手。
- `sidecar_status` 当前只使用 `starting / restarting / ready / error`，为受管内置 Sidecar 返回 `generation`，并可在 `starting` 返回白名单 `startupStage`，同时返回 app/API/schema 版本。
- 已启动 generation 只有真实 `Terminated` 才触发下一代，自动恢复最多 2 次，退避固定为 500 ms、2 s；当前 generation 连续 Ready 30 秒后重置预算。外部模式、显式 shutdown、事件流关闭但没有 `Terminated` 都不会自动重拉；内置二进制定位/child spawn 失败可在同一预算内重试。
- React 根节点的桌面服务恢复闸门：`starting / restarting` 时不渲染业务页，ready 后放行并持续观察，error/读取失败时显示不含原始 message 的全局恢复页。从 ready 进入非 ready 会清除运行期连接、取消并清空 TanStack Query；ready generation 变化也会覆盖漏过中间 `restarting` 的轮询。浏览器开发模式直接放行。
- 应用退出时向精确子进程写入 shutdown，最多等待 7 秒，超时后终止；Sidecar 优雅关闭 HTTP、checkpoint WAL 并关闭数据库。并发 shutdown 调用共享同一次 stop，后续调用等待第一次完成；若 ready 等待恰在 shutdown 已取走 child handle 后超时，握手任务不会伪造 exited。Tauri 还为内置模式注入 `OPC_EXIT_ON_STDIN_CLOSE=true`，父控制管道 EOF 触发同样的 Go 优雅退出；外部/开发默认 false。
- 恢复计划挂起后，设置页可调用 `restart_application`：若受管 child 存在，只有真实 `Terminated` 的 code 0 且无 signal 才允许 Tauri 重启应用；内置启动失败尚未创建 child 时允许继续，延迟到达的干净退出确认后可再次请求。外部 Sidecar、非零退出、signal 或未确认退出都会拒绝。
- Tauri capability 当前仅开放 core:default；前端不能任意调用 shell。
- manual Task 文件产出通过 WebView 文件选择与鉴权 multipart 上传进入 Sidecar 受控目录；前端不能指定服务端 `relative_path`，下载也只能经过鉴权 content API。

尚未实现或仍待真实验收：

- 真实 Tauri/Sidecar 父进程崩溃、进程树、Windows/macOS/Linux 和安装包生命周期验收；当前没有 Windows Job Object、Unix 进程组或孙进程治理。hard-hung orphan 只会继续持有数据库运行锁并阻止新 Sidecar 接触同库，不会被自动识别或回收。
- 数据库打开前的备份选择与安全回滚交互；当前恢复页已显示白名单恢复/迁移进度，但不提供路径、备份 ID、原始错误或启动前选择器。
- `OPC_LOG_DIR` 已用于启动前安全故障 journal、下一次健康启动补偿和 Go Sidecar/Tauri 壳脱敏轮转日志；设置“运行诊断”可白名单化展示桌面生命周期/版本、复制基础脱敏摘要、下载诊断包 v1 并打开自身日志目录。诊断包包含版本/平台、SQLite 健康/迁移和维护错误码汇总，原始日志不进入该包。
- 托盘专注状态/快捷业务动作/设置开关、原生通知、其他 OS 全局快捷键、开机启动和业务文件对话框；基础托盘源码已接但当前主机尚未完成原生链接与交互验收。
- 签名离线更新包的选择、验签、迁移前备份、安装与回退。
- Windows/macOS/Linux CI 构建、代码签名、公证、安装包和干净系统验收。
- 当前主机上的 Rust 格式与静态单元测试不能替代真实 Sidecar 生命周期、安装包或三平台支持；当前环境缺少 MSVC `link.exe` 和 Windows SDK，`cargo check` / `cargo test` 的链接阶段受阻。

## 目标功能

### 启动与服务恢复

- 建立明确桌面状态机：初始化目录、启动 Sidecar、ready 握手、健康检查、兼容校验、可用或恢复。
- 检查桌面应用、Sidecar、API 和 schema 版本兼容，阻止错误组合进入写入模式。
- 启动超时、ready 格式错误、健康失败或迁移失败时显示专用恢复页。
- 恢复页已支持状态重查、打开脱敏日志、查看白名单版本、受控应用重启，以及数据库打开前的白名单恢复/迁移进度；检查数据目录和启动前选择备份仍待实现。
- 已实现内置 Sidecar 的两次有界自动重启、固定退避和 30 秒稳定预算重置；重试耗尽后进入 error 并要求用户处理。
- 新 generation 只在旧代真实 `Terminated` 或 child 尚未创建的 spawn failure 后启动；数据库运行锁进一步阻止未知旧进程与新进程同时接触数据库。

### 退出与孤儿治理

- 区分关闭窗口、最小化到托盘和真正退出。
- 真正退出先停止接受新业务操作，等待短事务，并让可取消的长任务进入 cancelled/interrupted 后再优雅关闭 Sidecar。若数据恢复已进入不可取消的 applying/restarting 阶段，桌面必须阻止普通退出，等待恢复协调器完成或回滚。
- 维护精确子进程句柄和 generation，不使用宽泛进程名终止其他实例；并发 shutdown 共享一次 stop。
- 父进程异常退出时，Go 受管模式可由父控制管道 EOF 优雅结束；若进程 hard-hung，下一次启动只会由数据库运行锁拒绝接触同库，当前不会识别、终止或回收孤儿/孙进程。
- Agent 子进程由本地 Agent 模块管理，但最终退出清理纳入桌面生命周期。

### 系统托盘

- **当前已接源码**：托盘成功建立时关闭主窗口隐藏，左键/“显示”恢复，“退出”复用优雅关闭；失败时不拦截关闭。
- **后续**：首次行为明确提示并提供设置开关。
- **后续**：菜单增加快速新建任务、开始/暂停专注和设置。
- **后续**：图标区分空闲、专注、休息和本地服务故障。
- 退出动作必须关闭 Sidecar 和数据库；隐藏窗口不得误触发退出。

### 原生本地通知

- 支持任务、收件箱、专注、发票和系统维护的本地通知。
- 首次使用前解释用途并请求最小系统权限。
- 点击通知打开对应本地资源详情。
- 通知不可用或被拒绝时保留应用内提醒，不影响业务状态。
- 当前“应用内提醒”已由 Sidecar Reminder 到期生成 Inbox Item；操作系统通知权限、通知中心和点击通知跳转仍属于后续系统集成。
- 当前阶段不发送远程推送、邮件或第三方消息。

### OS 全局快捷键

- 已尝试注册命令面板 `⌘/Ctrl+Shift+K` 与新建任务 `⌘/Ctrl+Shift+N`；触发时显示/聚焦主窗口，再向 `main` WebView 发出固定 `command_palette/new_task` action。
- 注册失败、权限拒绝或与系统冲突时只报告 `unavailable`，并保留 WebView 内快捷键；运行诊断不显示原始平台错误。
- 开始/暂停专注和页面切换快捷键仍待后续。
- 系统快捷键只触发打开界面或安全动作，不能绕过确认、验收与权限。
- v0.3 再支持用户自定义和冲突调整。

### 文件对话框与路径授权

- 当前 Task 文件 Artifact 使用 WebView 文件选择器取得浏览器 `File`，以 multipart 上传；Sidecar 只接收字节流并复制到受控 Artifact store，不接收客户端本机绝对路径。
- 导入、导出、恢复、附件、发票和 Adapter 选择均使用原生文件对话框。
- 前端只获得受控文件引用或经过校验的用户选择，不接受任意路径字符串直接读写。
- 路径在 Rust 和 Sidecar 边界规范化，防止目录逃逸和符号链接攻击。

### 日志与诊断

- Sidecar 与 Tauri 壳已分别写入 appLogDir 的脱敏轮转日志；桌面壳只记录白名单生命周期 JSONL。
- Sidecar 日志保留受控进程阶段、版本、错误码、HTTP 路由模板、request ID 和耗时；桌面壳日志只保留生命周期事件与时间。两者均不包含令牌、完整客户资料、发票正文或 Agent 输入输出。WebView→Sidecar request ID 已实现；Tauri 壳不在 HTTP 请求路径上，不向生命周期事件伪造 request ID。
- 诊断页显示当前状态、最近失败和路径，支持复制脱敏摘要与打开日志目录。
- 系统维护失败可以幂等生成本地收件箱项。

当前已交付启动故障安全层和双进程日志纵切：Tauri 传入 `OPC_LOG_DIR`，Sidecar 也支持 `--logs` 和数据库同级默认 `logs/`；数据库启动/迁移及 Sidecar 启动失败写白名单 journal，下一次成功启动在 ready 前补偿为 Inbox Item。Sidecar 同时写 stderr 与 `opc-sidecar.log`，单文件最多 5 MiB、保留 `.1`～`.3` 三份归档；最终写入层遮盖会话令牌和 Bearer 值，访问记录不含 query/header/body，文件失效后降级 stderr。Tauri 壳以独立文件记录白名单生命周期事件。设置诊断页与脱敏诊断包 v1 已交付且不包含原始日志；无参数 command 可打开自身 `appLogDir`。WebView→Sidecar request ID 串联也已交付。

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
2. 为本 generation 生成新令牌并以端口 0 启动内置 Sidecar，同时注入 `OPC_EXIT_ON_STDIN_CLOSE=true`。
3. Sidecar 在每个数据库打开前阶段向受管父进程写入固定 stage code：取得数据库运行锁、检查或验证/应用/复验 pending restore、打开/迁移数据库、初始化工作区和启动本地 API；之后读取数据库身份、校验/创建绑定 marker、获取 Artifact root 独占锁并协调受控 store，全部成功后才输出 ready。任一进度码都不含路径、备份 ID 或原始错误。
4. Tauri 校验 loopback 地址并携带令牌调用 /health。
5. 版本与 schema 兼容后，WebView 取得当前进程内连接信息并加载业务页面。
6. 核心功能从首次启动起可离线使用。

### Sidecar 启动或运行失败

1. 初次启动失败或已启动 generation 的真实 `Terminated` 使桌面进入 `restarting`，停止业务写入并清除旧连接/查询。
2. 内置模式分别等待 500 ms、2 s 重试，最多两次；外部模式、显式 shutdown 或没有 `Terminated` 的事件流关闭不自动重试。
3. 两次仍未 Ready 则进入 error 并显示恢复页，不在后台无限重启；当前 generation 连续 Ready 30 秒才恢复下一轮完整预算。
4. 用户可手动重试、打开日志、检查版本或进入备份恢复。
5. 恢复成功后以前端可识别的新 generation、新会话令牌和重新申请的动态端口建立连接；即使轮询漏过 `restarting`，generation 变化也会触发一次清理与业务树重挂。

### 关闭窗口与退出

1. 当前托盘可用时，用户关闭主窗口会隐藏并保持 Sidecar；托盘不可用时不拦截关闭。根据设置切换该行为留到后续。
2. 用户从托盘或菜单选择“退出”。
3. 应用冻结新长任务，等待写事务并通知专注/Agent 模块处理运行态。
4. 若数据恢复处于 applying/restarting，显示不可退出的维护状态，待完成或回滚；其他可取消任务按其协议结束。
5. Tauri 请求 Sidecar 优雅关闭；并发调用共享一次 stop，超时后只终止精确 child generation。父管道 EOF 也会让受管 Go Sidecar 进入优雅关闭。
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

当前 `sidecar_status` 支持以下四个 phase；受管内置模式同时返回 generation，外部模式为 null：

- starting：本地服务正在启动或等待健康。
- restarting：内置 Sidecar 已安排有界自动恢复，业务连接不可用。
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

- sidecar_status：返回当前 phase、generation、运行期 API 地址、会话令牌和版本。
- restart_application：无业务参数；受管 child 存在时只接受 code 0 且无 signal 的真实退出，尚未创建 child 的内置启动失败可继续，延迟干净退出后可重试。浏览器开发模式、外部 Sidecar、非零/signal/未确认退出都会拒绝。

规划 command 或事件职责：

| 能力                        | 职责                                   |
| --------------------------- | -------------------------------------- |
| open_log_directory          | 打开应用日志目录                       |
| select_import / export_path | 原生文件选择和受控路径授权             |
| desktop_capabilities        | 返回托盘、通知、快捷键、自启和更新能力 |
| sidecar-state-changed       | 向 WebView 推送状态变化                |
| desktop-global-shortcut     | 已注册的命令面板或新建任务固定 action  |
| notification-activated      | 打开对应本地资源                       |

正式名称在实现 ADR 中冻结。所有高风险命令限制到 main 窗口的最小 Tauri capability，并验证调用参数。

### 会话与日志

- 会话令牌每个 generation 重新生成；动态端口每代通过端口 `0` 重新申请，端口值允许被 OS 复用。两者都只存在于进程内。
- 基础地址只能是 http://127.0.0.1:非零端口。
- 浏览器请求要求允许的 Origin 和 Bearer Token。
- WebView 每次请求生成 UUID v4；Sidecar 将规范 UUID 写入 `X-Request-ID` 响应头、统一错误体和脱敏访问日志，前端网络/超时错误也保留本次生成值。Tauri 生命周期日志保持独立白名单事件。
- Sidecar 离开 ready 时前端立即清除缓存连接并取消/清空 TanStack Query；新 generation ready 后重新获取。ready generation 变化还会补偿漏过中间状态的轮询。
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

- 已扩展前端全局服务状态：桌面 `starting/restarting/ready/error + generation`、非 ready 连接/Query 清理、generation 补偿和浏览器降级。
- 已实现启动失败恢复页 v1、内置 Sidecar 两次有界自动重启、500 ms/2 s 退避、连续 Ready 30 秒预算重置，以及每代新 token/动态端口申请。
- 已实现数据库父目录运行锁、父管道 EOF 优雅退出、安全应用重启门禁和并发 shutdown 共享 stop；孤儿/进程树治理仍待实现。
- 补真实 Tauri 与 Sidecar 父崩溃、进程树、三平台和安装包集成测试。

### v0.1-B：日志与维护

- 已接通 OPC_LOG_DIR 的启动故障 journal、原子更新、损坏隔离和 ready 前补偿，以及 Go Sidecar/Tauri 壳脱敏日志、5 MiB/3 归档轮转、敏感信息排除和文件故障降级；WebView→Sidecar request ID 已完成。
- 诊断页、脱敏摘要、诊断包 v1、无参数 `open_log_directory`、Tauri 壳自身日志和 API request ID 关联已完成。
- 数据库启动/迁移、Sidecar 启动、备份恢复、运行期数据库操作失败和 1–100 GiB 可配置低空间已接 maintenance 状态；物理卷同卷去重与无路径手动容量检查已在设置页交付，卷身份不离开 Sidecar 进程，卷级趋势仍待评审。

### v0.1-C：系统集成

- 托盘最小源码闭环已接：显示/隐藏/退出、固定动作白名单、fallback 图标和不可用安全降级；原生链接/实机交互、设置开关、业务动作和状态图标仍待。
- 继续逐项实现原生通知、其他 OS 全局快捷键、文件对话框和开机启动。
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
- [x] 数据库父目录固定 `.opc-sidecar-run.lock` 在 pending restore、迁移和 DB open 前取得 OS 独占锁；冲突立即失败且不触碰数据库。
- [x] 内置 Sidecar 最多按 500 ms、2 s 自动重启两次，当前 generation 连续 Ready 30 秒重置预算；只有真实 `Terminated` 才为已启动代际重拉，外部/shutdown/无 Terminated 流关闭均不触发。
- [x] 正常退出发送 shutdown，等待 drain/WAL checkpoint，超时只终止精确 child generation；并发调用共享一次 stop，ready 超时竞态不会伪造 exited，父管道 EOF 由 `OPC_EXIT_ON_STDIN_CLOSE=true` 触发 Go 优雅关闭。
- [x] 恢复计划挂起后可从设置页请求安全重启；command 拒绝外部 Sidecar，受管 child 只接受 code 0/no signal，未创建 child 的 bundled 启动失败允许继续，延迟干净退出后可重试。
- [x] 当前源码门禁通过 Web 全量 90 个文件 / 602 项、Go `go test ./... -count=1` 与 `go vet ./...`、Sidecar 构建、Rust 格式和锁定 Cargo metadata；托盘新增单元测试源码已完成静态复核，但受工具链限制未执行 Rust 测试或原生链接。
- [x] 在线 Updater 未启用，也不是启动依赖。

### 仍待验收

- [ ] 真实 Tauri/Sidecar 父进程崩溃、进程树、hard-hung orphan 与孙进程治理；当前运行锁只阻止第二个进程接触同库，不自动回收。
- [x] 启动前白名单 incident journal、稳定 ID 重放、损坏隔离及健康启动补偿。
- [x] 脱敏诊断包 v1（不含原始日志）。
- [x] Go Sidecar 脱敏日志落盘/轮转与 stderr 降级。
- [x] 设置运行诊断可通过无参数 Tauri command 打开自身 `appLogDir`；浏览器模式不伪造路径。
- [x] Tauri 壳白名单 JSONL 生命周期日志、5 MiB/3 归档、非普通目标拒绝与 stderr 降级。
- [x] WebView→Sidecar request ID：每次请求使用 UUID v4，响应头、错误体、前端错误和访问日志可关联；非法客户端值由 Sidecar 替换为规范 UUID。
- [x] 全局服务恢复页 v1：starting/restarting/error 拦截业务页，ready 自动放行；generation、查询清理、状态重查、脱敏日志入口、安全重启、版本白名单与原始错误排除。
- [ ] 数据库打开前备份选择与实时恢复进度。
- [ ] 托盘源码已完成最小闭环；MSVC 工具链补齐后验证 Windows 链接、关闭/恢复/退出与 Sidecar 无残留，再补 macOS/Linux、设置开关、状态和业务动作。原生通知、其他 OS 全局快捷键、开机启动和原生业务文件对话框仍待。
- [ ] 签名离线更新、迁移前验证备份与失败回退。
- [ ] Windows、macOS、Linux 对应签名/公证、干净机、备份恢复、更新和性能证据。
- [ ] 当前主机补齐 MSVC `link.exe` 与 Windows SDK 后的 `cargo check` / `cargo test`、Tauri 链接与安装包检查。

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
