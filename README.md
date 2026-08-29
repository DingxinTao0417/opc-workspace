# opc-workspace

opc-workspace 是面向一人公司的本地优先桌面工作台。本仓库当前提供 v0.1 的可运行基座：Tauri 2 桌面壳、React/TypeScript/Vite 界面、Go Sidecar、SQLite 版本化迁移，以及沿用历史 Linear 深色设计方向的页面框架。

> 当前版本不包含 AI、LLM、智能排程、自然语言解析或自动生成报告；本地开发与桌面运行时均不使用 Docker。

## 当前完成范围

- Tauri 2 桌面窗口、单实例保护、应用数据目录初始化和 generation-aware Go Sidecar 生命周期；恢复计划挂起后可从设置页安全关闭受管 Sidecar 并重启桌面应用，受管 child 必须以 code 0 且无 signal 退出；内置 Sidecar 在 HTTP 就绪前通过固定启动阶段显示恢复、迁移与数据库打开进度，且不传递路径、备份 ID、令牌或原始错误；尚未创建 child 的启动失败仍允许重启应用，延迟到达的干净退出确认后可再次请求
- 生产内置 Sidecar 每代生成新的随机会话令牌，并通过端口 `0` 重新请求 OS 分配动态端口（端口值允许被 OS 复用）；只有真实 `Terminated` 才会为已启动 generation 安排下一代，最多自动重启 2 次（500 ms、2 s），当前 generation 连续 `ready` 30 秒后重置预算。外部开发 Sidecar、显式 shutdown、事件流关闭但没有 `Terminated` 都不会自动重拉；并发 shutdown 调用共享同一次 stop
- Go `/health` 与版本化 `/api/v1`，统一请求 ID、错误响应、Bearer 鉴权和 Origin 白名单；设置“关于”展示真实 app/commit/API/schema/SQLite 状态，“运行诊断”对照 Tauri Sidecar 生命周期、复制脱敏摘要并下载白名单诊断包 v1
- React 路由级全局错误边界：渲染异常时显示不含原始错误的恢复页，可重新渲染、返回今日或打开运行诊断，不让页面直接白屏
- SQLite schema v38、WAL、外键、busy timeout 和嵌入式版本化迁移；v3–v37 交付项目、Task/Actor/D2、Client、Focus、Inbox/Reminder/设置、受控文件、来源保护、重复 Reminder、受限预设自动化、Agent Adapter、客户回访、路线图与内容日历数据契约；v38 新增内容条目→Inbox 的版本化来源、去重和删除协调约束
- 任务完整事实与受控生命周期纵切：快照式幂等新建、详情、`If-Match` 非状态编辑/删除、项目与父子关系、标签、完成标准、服务端分页/搜索/六状态筛选/稳定排序、事实及六命令生命周期原子批量操作、计划日期组按钮及同状态拖拽排序，以及开始/阻塞/解除阻塞/完成/取消/重新打开六个显式单任务命令；直属非取消子任务至少 1 个且全部完成、manual 策略和责任门禁齐全时，系统创建零 Artifact 的 `child_rollup` 并最多把父任务推进到待验收，失效时撤回或重开而不覆盖人工/返工决策；Today 已消费计划组排序并提供四组活动任务的版本化任意日期/未排期安排
- 标签分页/搜索/排序、幂等新建、并发安全编辑和确认删除；标签嵌入或父子聚合变化会递增受影响任务版本
- 项目 CRUD、服务端分页/搜索/状态筛选、快照式创建幂等、覆盖聚合事实的 `If-Match` 乐观锁、受控状态流转、归档/恢复和确认后硬删除；项目卡片与详情从关联任务派生进度和 `actual_minutes`，项目任务浏览器支持树/平铺及搜索、状态、优先级、类型、标签和排期组合筛选；项目新建/编辑及项目列表客户筛选已接共享的服务端搜索 Client 选择器；项目详情还可按 Task 查询时当前项目归属查看 7 天/30 天/本月 Focus 趋势与终态 Session 历史
- Project Artifact→Inbox→Task 人工闭环：Artifact 聚合返回 nullable follow-up 的 Inbox ID/version/status/policy/source deletion 和实时 required 进度，同时保留 Project 数值 `ETag / meta.project_version`；产出区位于任务后，显示待拆分/跟进中/已解决/已忽略及阻塞/待验收/取消并深链 Inbox。split 继承但可逐项清除/改选可信来源 Project，支持独立完成条件、owner/person 和 manual owner reviewer；关系行以 stack-aware Modal 复用共享 Task 详情。成功 Inbox mutation 先取消来源 Project 在途读取再失效查询，split 另失效 Task/Today/Project
- Task 新建/编辑、Tasks 项目筛选、批量目标项目和 Inbox 拆分任务共用 `ProjectSelect`：每页只读 20 条，输入经 250 ms 防抖后使用既有 Project API 做服务端搜索，`q / page / includeArchived` 隔离 Query key，并向请求传递取消信号。候选按 ID 去重，显式清除才提交未归项目；选中详情或名称 fallback 使跨页、失败和当前已归档项目仍可见，默认候选不列其他归档项目。生产路径已移除串行拉取全部 Project 的 `getAllProjects`
- 客户基础资料 CRUD、服务端分页/搜索/状态筛选/稳定排序、快照式创建幂等、`ETag`/`If-Match` 乐观锁和受约束硬删除；客户列表/基础详情、Project 客户关联、人工备注/会议活动，以及关联 Project 完成/重新打开时生成的只读系统活动已接真实 API。客户回访 C2–C6 已接计划/执行 API、到期 Inbox 投影、客户详情管理、Today 待办和 Inbox→客户详情入口：完成/跳过/确认取消/原子重排、完成时原子创建下一次本地计划、负责人/状态/服务端逾期筛选、幂等、并发控制和不可变审计事件均在本机；inactive 客户只能收口既有计划（完成、跳过或取消），新建、编辑、重排与完成时续排会返回明确冲突，恢复 `active/lead` 后才重新允许。更新或终态命令会同事务归档旧到期投影，Today/Inbox 不重复写入回访状态。共享 `ClientSelect` 已覆盖 Project 新建/编辑、Projects 筛选和 Tasks 筛选：每页读取 20 条、250 ms 服务端搜索、稳定分页和取消信号，跨页或加载失败时保留当前选择，inactive 客户保持可见可选，并提供加载、空、错误重试、更多提示和 combobox 键盘语义；真实浏览器、窄屏及 1,000/10,000 条数据性能仍待专项验收
- Actor 管理纵切：schema v7 固定创建唯一 owner/system，幂等回填历史任务的 owner Assignment 与迁移事件；`/api/v1/actors` 提供分页筛选、person 幂等新建、详情和 `If-Match` 编辑/停用，设置页“人员与责任”接入真实本地数据
- Assignment 责任纵切：任务详情可查询当前负责人/审核人和分页历史，完成首次分派、改派与结束；命令以 Task `If-Match`/`version` 拒绝旧写入，支持可选幂等快照，并与 Assignment Workflow Event 在同一事务提交
- 任务活动时间线：详情按需分页读取 Task 聚合的生命周期、分派和迁移事件；同一命令内通过 `command_seq` 稳定展示自动结束分派与最终状态事件
- 项目活动时间线：创建、资料编辑、七种生命周期转换与永久删除同事务追加不可变事件；详情稳定分页展示 owner、时间、状态或资料字段变化，创建幂等重放不重复事件；complete/reopen 还会把事件发生当下的关联客户投影为只读本地活动，后续改绑不搬迁历史
- T-18D D2 产出验收纵切：新建任务和符合条件的任务编辑可选择 `review_policy = manual`；任务详情支持摘要及文本、链接、结构化 JSON、文件混合提交，owner 接受或要求返工，并分页查看带 `manual/child_rollup` 来源的 Submission/Artifact 历史；系统汇总仍必须由 owner 验收后才能完成
- 受控文件存储：Sidecar 以进程级独占锁管理 `artifacts/`；JSON marker 携带 `format_version / database_id / store_id`，schema v9 用不可变数据库身份和一次性 `artifact_store_id` 建立双向绑定，并使用 `.staging/`、`objects/`、`avatars/`、`.trash/` 和 `.quarantine/`；校验文件大小与 SHA-256，关键文件/目录项做耐久同步。Task/Client/Project 文件继续位于 `objects/`，工作区头像位于 `avatars/`；提交事务报错只清除数据库可证明无引用的文件，模糊 COMMIT 留给 reconcile
- Sidecar 在处理 pending restore、迁移或打开 SQLite 前，先对数据库父目录固定 `.opc-sidecar-run.lock` 获取非阻塞 OS 独占运行锁；冲突立即失败且不接触数据库。该锁与 Artifact root 锁分工不同：前者保护数据库/恢复入口，后者保护受控文件协调
- 客户附件纵切：客户详情支持选择本地文件后预览名称/大小、版本化幂等上传、稳定分页、完整性校验下载、带原因软删除和删除历史；Client Attachment 与 Task file Artifact 共享受控 store，跨表 object ID 唯一，Client 聚合硬删除也执行 tombstone/trash 补偿
- 客户责任关联纵切：客户详情可选择已有 active person，或原子新建 person 后显式关联；每个客户只允许一个 active contact，解除必须填写原因并保留不可变历史，关联变化传播 Client 聚合版本
- 一致性备份与安全恢复：设置“数据与备份”可创建、列出、重新校验、隔离演练、二次确认恢复和永久删除；Sidecar 在维护写锁内用 SQLite `VACUUM INTO` 建立快照，将全部 active 受控文件与身份 marker 写入同卷 staging，逐项验证后原子发布。手工备份及破坏性迁移、JSON/ZIP 导入、恢复安排使用的自动回滚包均在任何备份 staging 或 `VACUUM INTO` 前执行容量准入：以 `max(PRAGMA page_count × page_size, 当前数据库文件大小)`、经安全解析且实际普通文件大小与登记值一致的全部 active 受控文件、marker 和 manifest 上界之和为载荷，再增加 20% 且至少 64 MiB 余量，并只探测 backup root；恢复另合并预留目标 pending 副本和 plan。可用空间恰好等于需求时允许继续；空间不足/容量无法确认以脱敏 507/503 或迁移启动失败拒绝，不创建回滚包、不改业务数据，也不投影通用备份故障事项。已有工作区遇到带 `-- migration: destructive` 标记的迁移时，会先执行安全迁移、停在破坏性边界并通过准入创建同规格自动回滚包；失败则拒绝执行破坏性 SQL。恢复安排再次演练目标、通过合计容量准入后创建当前状态回滚包并冻结写入；下一次启动在 live 资源打开前交换数据库与完整 objects，最终验证失败自动回滚，成功以提交点防止重复应用
- 版本化业务 JSON 导出：设置页可下载单事务一致视图下的显式业务表白名单；包携带格式、应用/API/schema 版本、稳定列与行结构以及全部 active 受控文件摘要，保留 Task/Client/Project 文件和 Workspace Avatar 元数据但不嵌入正文，并明确排除会话令牌、机器绝对路径、数据库身份、幂等快照和派生/维护表
- 含文件业务 ZIP 导出：设置页可下载 manifest、同一版本化业务 JSON 和全部 active 受控文件；Sidecar 在维护写锁内逐文件复验 size/SHA-256，完整生成后才响应，缺失或篡改不会留下部分下载
- 业务 JSON 安全导入 v1：设置页先预检官方 JSON，再显式确认；仅支持当前 schema、无受控文件/活动 Focus 且目标为空，应用前自动创建已校验回滚备份，整批事务失败不改变现有数据
- 含文件业务 ZIP 安全导入 v1：设置页先预检 manifest、业务 JSON、文件全集/哈希与数据库元数据，再以独立确认词应用；仅支持当前 schema、终态 Focus 且目标为空，应用前自动创建已校验回滚备份，文件无覆盖发布并在 DB 提交前复验，失败补偿本次文件
- React 三栏应用框架、今日/任务/项目/客户页面，以及已接真实 Session 的专注页和右侧概览；项目详情已聚合所属 Task 的真实产出、项目级 Focus 报告与终态 Session 历史，并可直达任务验收，收入和发票目前只有路由与页面骨架；路线图 R3 基础界面已完成，R4 已接完整同季度集合的拖拽/键盘安全排序，目标日期/跨季度拖拽仍待；内容日历已接入六周月格、跨日/可见跨月拖拽、详情编辑、IANA/DST 安全改期、人工发布确认、准备任务关联，以及审核/发布到期的幂等 Inbox 协同
- Today 真实日期任务视图：支持日期导航，按所选浏览器本地日期分页拉全逾期、当天、本周稍后和未排期活动任务；四个可见组共享同日/跨日期拖拽，空的所选日期/未排期可接收任务，跨日期明确区分改期事实和两个组的排序结果；四组任务也可行内安排任意日期，模糊响应必须回读证明后才确认成功；todo 可行内开始、无需验收的 in_progress 可行内完成，Focus 空闲时可直接开始绑定专注，并可从任务行直达完整编辑或经版本化二次确认删除。统计条另提供与服务端当前时刻一致的逾期/未来 24 小时临期快捷筛选，读取完整分页结果而不拿当前日期分组冒充截止风险
- `Ctrl/Cmd + K` 命令面板、`Ctrl/Cmd + N` 新建任务入口；桌面可额外注册 `⌘/Ctrl+Shift+K` 与 `⌘/Ctrl+Shift+N`，跨应用显示并聚焦主窗口后只发送固定打开命令/新建任务事件；注册冲突只降级为应用内快捷键，运行诊断不暴露平台错误。命令面板以 200 ms 防抖调用统一本地搜索，跨真实 Task/Project/Client/活动 Inbox 返回确定性相关结果并直达可刷新详情/指定设置模块；空查询优先显示本地最近使用，容量/保留期受限且不保存搜索词或业务正文，已删资源在本地确认 404 后自动清理；具备加载、错误、重试、空状态、焦点圈闭/恢复和输入法保护，并与业务 Modal 共用叠层、背景滚动锁和最上层 Escape 边界
- Focus Core A+B+C+D1+D2a 和 D2b 日期范围回顾/项目/标签/小时/热力图：持久化 Session/interval、任务绑定、暂停/继续/停止/取消、服务端绝对时间、15 秒心跳、启动/刷新恢复、`If-Match`/幂等、精确秒数结转 Task 完整分钟、IANA 当地日 completed-only Today/周期统计、终态历史分页、7/30 天/本月/最多 93 天自定义趋势与 Streak、按 Task 当前归属的项目分布与非互斥当前标签分布、DST 安全 24 小时分布/最佳时段与周几×小时二维热力图、Task 详情按需专注记录、项目详情按当前 Task 项目归属查看报告与历史，以及共享前端循环/恢复 UI
- 手工 Inbox 受理/分诊纵切：真实创建、三视图列表、搜索/优先级/分页、详情编辑、单条已读、按列表快照全部已读、稍后/恢复、带原因解决/忽略、重开、全局待处理未读数和追加式事件时间线；列表每 15 秒按服务端时钟刷新到期可见性
- Inbox 已有 Task 关系纵切：详情查询活动/历史关系和服务端实时 required 进度，支持关联已有 Task、修改必需标记、带原因软解除、`open / tracking` 联动与按活动关系重开；关系写入使用 Inbox `If-Match`/幂等快照并追加事件，活动关系阻止 Task 硬删除，软解除后 Task 可删且历史 ID/标题快照保留。父子层级不会创建关系或继承/改写 required，Inbox 自动结清只看显式活动必需关系
- SQLite 持久化的工作区名称、默认首页、右侧概览开关、亮/暗主题、减少动效和专注参数设置；工作区头像通过严格 multipart 导入受控 `avatars/`，选择后即时预览，保存时与变化设置原子提交，取消恢复已提交头像；旧 localStorage Data URL 在服务端无头像时一次性迁移并在验证后清理
- 一次性与重复本地提醒：创建、分页/搜索/状态列表、并发安全编辑、带原因取消、启动补偿及 15 秒到期扫描；daily/weekly 规则按 IANA 当地日历在同一事务中生成独立下一 occurrence，跨 DST 保持当地钟点，离线积压只补当前一条。到期以 occurrence 稳定事件键生成 Reminder Inbox Item，重复扫描和重启不会重复投影

受控任务 D1/D2、父任务有门禁自动待验收、Project/Client、Focus、Today、搜索、设置/诊断、数据安全，以及 Inbox/Reminder/Task 编排已经交付；Reminder 已支持一次性与 daily/weekly 本地重复系列。v0.2 首个受限预设自动化纵切也已接通，本地 Agent 已交付代码所有 Adapter 登记与安全诊断但 Runner/Run 尚未实现。客户回访已完成本地计划、终态、到期 Inbox 与跨时区/DST 边界；路线图已完成数据/API、R3 基础界面和 R4 同季度安全排序首个纵切。内容日历已完成数据/API、六周月格、安全拖拽、详情编辑、人工发布确认、准备 Task 关联、六周范围自动分页聚合，以及审核/发布到期的启动补偿、周期扫描、版本化去重、旧来源终结与 Inbox 详情；schema v38 不变。内置 Sidecar 的有界重启、数据库运行锁、父管道 EOF、启动进度、原生全局快捷键和前端世代清理也已接通。v0.1 不调用 AI/LLM，也不创建 Agent Run；自动化没有 Shell/SQL/HTTP、外发或自由规则。app v0.1.0 / API v1 不变，SQLite 当前为 schema v38。T-02 仍部分完成。[PRD v9.63](docs/opc-workspace-PRD.md) 记录了完整边界。

## 目录结构

```text
apps/
  web/                    React 18 + TypeScript + Vite + Tailwind CSS v4
  desktop/                Tauri 2 桌面项目
    src-tauri/
      binaries/           构建时生成的 target-triple Sidecar（不入库）
services/
  sidecar/                Go HTTP API、SQLite、迁移和测试
scripts/
  dev.mjs                 统一开发进程编排
  build-sidecar.mjs       当前平台 Sidecar 构建与 Tauri 命名
  gofmt.mjs               跨平台 Go 格式写入/检查
  check-docs.mjs          文档链接、冲突标记和机器路径检查
docs/                     PRD、整体功能架构和各模块功能文档
.local/dev-data/          开发数据库（已忽略）
```

## 产品文档

- [文档索引](docs/README.md)
- [产品需求文档（PRD v9.63）](docs/opc-workspace-PRD.md)
- [整体功能架构](docs/functional-architecture.md)

## 开发依赖

- Node.js 20.19–26
- pnpm 10+
- Go 1.22+
- Rust 1.85+（rustup/cargo）
- 对应平台的 Tauri 系统依赖

Windows 还需要 WebView2 Runtime、Visual Studio C++ Build Tools 与 Windows SDK；macOS 和 Linux 的系统依赖见 Tauri 2 官方 prerequisites。最终用户安装已构建应用后不需要 Node.js、pnpm、Go、Rust 或 Docker。

## 安装

```powershell
pnpm install
go -C services/sidecar mod download
```

## 本地开发

桌面联调：

```powershell
pnpm dev
```

统一脚本会依次启动：

1. Go Sidecar：`127.0.0.1:9876`
2. Vite：`127.0.0.1:1420`，`/api` 与 `/health` 代理到 Sidecar
3. Tauri：通过 `OPC_SIDECAR_URL` 连接上述开发 Sidecar，不重复启动后端

只启动 Sidecar 与浏览器版前端：

```powershell
pnpm dev:web
```

开发数据库固定保存到 `.local/dev-data/opc-workspace.db`；Sidecar 运行时还会在同级创建并保留 `.local/dev-data/.opc-sidecar-run.lock`，所有权只由 OS 锁表示。开发 Artifact、手动备份和启动故障 journal 分别保存到 `.local/dev-data/artifacts/`、`.local/dev-data/backups/`、`.local/dev-data/logs/`，并与正式数据完全隔离。统一开发脚本默认不写入 demo 数据；从旧版本升级时，迁移只会清理先前 demo seed 使用的固定记录。开发令牌只用于本机联调，不得用于生产构建。

## 检查与测试

```powershell
pnpm check:source
```

`check:source` 是不依赖 Rust 链接器的源码门禁，统一执行 Prettier、Go/Rust 格式检查、文档本地链接与机器路径检查、Web 类型检查/全量测试/生产构建，以及 Go 无缓存测试/vet/Sidecar 构建。

桌面工具链完整时运行完整门禁；它会先执行上述源码门禁，再执行 `cargo check` 和 Rust 测试：

```powershell
pnpm check
```

也可按层定向运行 `pnpm check:web`、`pnpm check:go`、`pnpm check:rust`、`pnpm check:docs`，或运行 `pnpm test:web`、`pnpm test:go`、`pnpm test:rust`。Windows 的 Rust/Tauri 链接检查需要 Visual Studio C++ Build Tools 与 Windows SDK；缺少 `link.exe` 时，`check:source` 仍可作为本机源码质量门禁，但不能替代桌面编译和安装包验收。

## 构建

先为当前 Rust target triple 生成 Go Sidecar：

```powershell
pnpm build:sidecar
```

脚本读取 `rustc --print host-tuple`，并把当前 Git 短提交写入 Sidecar；工作树未提交时追加 `-dirty`，CI 可用只含字母、数字、点、下划线、加减号的 `OPC_BUILD_COMMIT` 覆盖。随后生成类似以下文件：

```text
apps/desktop/src-tauri/binaries/opc-sidecar-x86_64-pc-windows-msvc.exe
```

随后可分别或统一构建：

```powershell
pnpm build:web
pnpm build:desktop
pnpm build
```

`pnpm build:desktop` 会由 Tauri Bundler 把 `apps/web/dist` 与对应架构的 Sidecar 一并打包。跨平台发布应在各目标平台或相应 CI runner 上构建，并在无开发工具的干净系统中验证。

## 数据目录

生产环境不依赖当前工作目录，也不向安装目录写业务数据。Tauri 在操作系统提供的 `appDataDir` 和 `appLogDir` 中创建：

```text
appDataDir/
  .opc-sidecar-run.lock     # 数据库父目录固定运行锁文件；所有权由 OS 独占锁表示
  opc-workspace.db
  attachments/
  artifacts/
    .opc-artifact-store-v1
    .opc-artifact-store.lock
    .staging/
    objects/
	avatars/                    # 受控工作区头像（PNG/JPG/WebP，最多一个 active）
    .trash/
    .quarantine/
  invoices/
  backups/
    <backup-id>/             # 已校验 SQLite + Artifact 备份包
  config/

appLogDir/
  startup-incidents-v1.json # 启动前安全故障 journal；成功补偿后删除
  .startup-incidents-invalid-*.json # 损坏 journal 隔离；不自动读取
  opc-sidecar.log            # Sidecar 脱敏运行日志，5 MiB，保留 .1～.3
  opc-workspace.log          # Tauri 桌面壳白名单 JSONL 日志，5 MiB，保留 .1～.3
```

具体物理路径由操作系统和应用标识 `com.opcworkspace.desktop` 决定，业务代码不硬编码该路径。升级应用程序文件不会覆盖这里的数据。

## 本地 API 约定

- Sidecar 仅监听 `127.0.0.1`；开发默认固定端口，桌面生产运行使用端口 `0` 获取随机空闲端口。
- 生产请求（包括 `/health`）必须携带 `Authorization: Bearer <session-token>`。
- Tauri 通过环境变量把数据库路径、日志目录、端口和令牌交给 Sidecar，令牌不出现在命令行。
- 桌面 `sidecar_status` 当前只使用 `starting / restarting / ready / error`，并为受管内置 Sidecar 返回 `generation`；`starting` 可携带受控 `startupStage`，只允许固定的锁、恢复、迁移、数据库和本地 API 阶段。每代生成新令牌并重新请求动态端口。非 ready 状态会清除前端运行期连接与 TanStack Query 缓存，`generation` 变化还能覆盖前端漏过中间 `restarting` 轮询的情况。
- Tauri 启动内置 Sidecar 时固定注入 `OPC_EXIT_ON_STDIN_CLOSE=true`，父控制管道 EOF 会触发 Go 的 HTTP drain、WAL checkpoint 和数据库关闭；外部/开发模式默认 `false`，普通 stdin EOF 不会使服务自行退出。
- Sidecar 在 pending restore、迁移和数据库打开前获取数据库父目录 `.opc-sidecar-run.lock` 的 OS 独占锁；锁冲突立即失败且不触碰数据库。
- Sidecar 通过 `OPC_LOG_DIR` 或 `--logs` 使用独立诊断目录；开发默认使用数据库同级 `logs/`。该目录不得与受控 Artifact 或备份根重叠。
- Tauri 通过 `OPC_ARTIFACT_DIR` 把 `appDataDir/artifacts/` 交给 Sidecar；开发脚本等价地使用 `--artifacts .local/dev-data/artifacts`。
- Sidecar 默认把数据库同级 `backups/` 用作本地备份根；也可由桌面层通过 `OPC_BACKUP_DIR` 或命令行 `--backups` 指定，且不得与 Artifact root 重叠。
- 手动 `POST /api/v1/backups` 以及迁移、导入、恢复安排创建的内部自动回滚包共用容量准入，并且只探测上述备份根；恢复安排还会合计 pending 目标副本。拒绝响应保持统一 `{ "code", "message", "request_id" }`，不返回本机路径、盘符、总量、剩余量或探测错误，也不会自动重试。
- 业务接口统一位于 `/api/v1`；错误格式为 `{ "code", "message", "request_id" }`。
- API 时间使用 RFC 3339 UTC，纯日期使用 `YYYY-MM-DD`，金额使用最小货币单位整数。

当前可用端点：

```text
GET    /health
GET    /api/v1/backups
GET    /api/v1/exports/business-data
POST   /api/v1/backups
POST   /api/v1/backups/:id/verify
POST   /api/v1/backups/:id/drill
POST   /api/v1/backups/:id/restore
DELETE /api/v1/backups/:id?confirm=true
GET    /api/v1/clients/:id/activities
POST   /api/v1/clients/:id/activities
GET    /api/v1/client-activities/:id
PATCH  /api/v1/client-activities/:id
DELETE /api/v1/client-activities/:id?confirm=true
GET    /api/v1/clients/:id/attachments
POST   /api/v1/clients/:id/attachments
GET    /api/v1/client-attachments/:id
GET    /api/v1/client-attachments/:id/content
DELETE /api/v1/client-attachments/:id?confirm=true
GET    /api/v1/settings
PATCH  /api/v1/settings
POST   /api/v1/settings/avatar
GET    /api/v1/settings/avatar/content
GET    /api/v1/actors
POST   /api/v1/actors
GET    /api/v1/actors/:id
PATCH  /api/v1/actors/:id
GET    /api/v1/tasks
POST   /api/v1/tasks
PATCH  /api/v1/tasks/batch
PUT    /api/v1/tasks/reorder
GET    /api/v1/tasks/:id
PATCH  /api/v1/tasks/:id
PATCH  /api/v1/tasks/:id/status       # 已废弃，固定返回 410
POST   /api/v1/tasks/:id/start
POST   /api/v1/tasks/:id/block
POST   /api/v1/tasks/:id/unblock
POST   /api/v1/tasks/:id/complete
POST   /api/v1/tasks/:id/cancel
POST   /api/v1/tasks/:id/reopen
POST   /api/v1/tasks/:id/submit-output
POST   /api/v1/tasks/:id/review
GET    /api/v1/tasks/:id/submissions
GET    /api/v1/tasks/:id/artifacts
GET    /api/v1/tasks/:id/events
GET    /api/v1/tasks/:id/assignments
POST   /api/v1/tasks/:id/assignments
POST   /api/v1/tasks/:id/reassign
POST   /api/v1/assignments/:id/end
GET    /api/v1/artifacts/:id
GET    /api/v1/artifacts/:id/content
DELETE /api/v1/artifacts/:id?confirm=true
DELETE /api/v1/tasks/:id
GET    /api/v1/tags
POST   /api/v1/tags
PATCH  /api/v1/tags/:id
DELETE /api/v1/tags/:id?confirm=true
GET    /api/v1/projects
POST   /api/v1/projects
GET    /api/v1/projects/:id
GET    /api/v1/projects/:id/events
GET    /api/v1/projects/:id/artifacts
GET    /api/v1/projects/:id/attachments
POST   /api/v1/projects/:id/attachments
GET    /api/v1/project-attachments/:id
GET    /api/v1/project-attachments/:id/content
DELETE /api/v1/project-attachments/:id?confirm=true
PATCH  /api/v1/projects/:id
POST   /api/v1/projects/:id/transitions
DELETE /api/v1/projects/:id?confirm=true
GET    /api/v1/clients
POST   /api/v1/clients
GET    /api/v1/clients/:id
PATCH  /api/v1/clients/:id
DELETE /api/v1/clients/:id?confirm=true
GET    /api/v1/inbox-items
POST   /api/v1/inbox-items
POST   /api/v1/inbox-items/read-all
GET    /api/v1/inbox-items/:id
PATCH  /api/v1/inbox-items/:id
GET    /api/v1/inbox-items/:id/events
POST   /api/v1/inbox-items/:id/read
POST   /api/v1/inbox-items/:id/snooze
POST   /api/v1/inbox-items/:id/unsnooze
POST   /api/v1/inbox-items/:id/resolve
POST   /api/v1/inbox-items/:id/dismiss
POST   /api/v1/inbox-items/:id/reopen
GET    /api/v1/inbox-items/:id/tasks
GET    /api/v1/stats/inbox
POST   /api/v1/inbox-items/:id/tasks/:task_id
PATCH  /api/v1/inbox-items/:id/tasks/:task_id
DELETE /api/v1/inbox-items/:id/tasks/:task_id
POST   /api/v1/inbox-items/:id/split
POST   /api/v1/inbox-items/:id/force-resolve
GET    /api/v1/reminders
POST   /api/v1/reminders
GET    /api/v1/reminders/:id
PATCH  /api/v1/reminders/:id
DELETE /api/v1/reminders/:id
GET    /api/v1/automations/rules
GET    /api/v1/automations/rules/:id
PATCH  /api/v1/automations/rules/:id
POST   /api/v1/automations/rules/:id/preview
POST   /api/v1/automations/rules/:id/enable
POST   /api/v1/automations/rules/:id/disable
GET    /api/v1/automations/runs
GET    /api/v1/automations/runs/:id
POST   /api/v1/automations/runs/:id/retry
GET    /api/v1/focus-sessions?page=1&page_size=20&status=terminal&project_id=<UUID>
GET    /api/v1/focus-sessions/active
POST   /api/v1/focus-sessions
POST   /api/v1/focus-sessions/:id/pause
POST   /api/v1/focus-sessions/:id/resume
POST   /api/v1/focus-sessions/:id/recover
POST   /api/v1/focus-sessions/:id/stop
POST   /api/v1/focus-sessions/:id/cancel
GET    /api/v1/stats/focus?date_from=YYYY-MM-DD&date_to=YYYY-MM-DD&timezone=<IANA>&project_id=<UUID>
GET    /api/v1/stats/today?date=YYYY-MM-DD&timezone=<IANA>
```

Actor 详情、创建和更新返回 `ETag`；更新必须携带 `If-Match`。Actor 新建只接受 `person`，可选 `Idempotency-Key` 会保存规范化请求与首次 `201` 快照；owner 只允许修改展示名称，system 不可编辑，存在活动 Assignment 的 person 不能停用。当前没有 Actor 删除路由。Assignment 查询返回当前 assignee/reviewer、分页结束历史和 Task `ETag`；创建、改派和结束必须携带 Task `If-Match`，成功递增 Task 版本，并可用 `Idempotency-Key` 重放首次响应快照而不重复写事件。v0.1 assignee 仅允许 active owner/person，reviewer 仅允许 active owner。

Task 只能以 `todo` 新建；创建和非生命周期 PATCH 接受 `review_policy = none | manual`，但策略变更只允许在 `todo` 且从未产生 Submission 时执行。非生命周期 PATCH 不能写状态，旧 `/tasks/:id/status` 固定返回 `410 TASK_STATUS_ENDPOINT_DEPRECATED`。六个生命周期命令必须携带 Task `If-Match`，可选 `Idempotency-Key`；开始前必须有 active assignee，阻塞和取消必须填写原因，解除阻塞只恢复服务端保存的来源状态，直接完成只允许 `review_policy = none`。完成或取消会在同一事务结束全部活动 Assignment 并只递增一次 Task 版本；等待验收时取消还会把当前 Submission 标为 `withdrawn`。重新打开回到 `todo`、清空 `current_submission_id`，但保留提交历史且不恢复旧分派。

manual Task 只能从 `todo / in_progress` 提交，提交前必须同时具备 active assignee 与 active owner reviewer。JSON 可提交文本、HTTP(S) 链接或 JSON object；包含文件时使用 multipart，唯一 `manifest` 必须是首个 part，之后只允许它通过唯一 `file_field` 精确引用的文件 part。摘要最长 10,000 字符；每批最多 20 个 Artifact；名称 1–255 个安全字符；文本最多 500,000 字符；链接最多 4,096 bytes 且禁止 URL credentials；严格 JSON body、multipart `manifest` 和单个 structured object 各不超过 1 MiB；单文件非空且最多 50 MiB，完整 multipart 请求最多 100 MiB。Sidecar HTTP read/write timeout 为 180 秒，前端上传与下载使用 120 秒端到端超时。提交进入 `waiting_review`；owner 接受后进入 `done` 并结束活动 Assignment，要求返工时必须填写不超过 1,000 字符的原因并回到 `in_progress`。`current_submission_id` 在接受、返工和取消后用于指向最近一批，只有 reopen 清空。

父任务自动待验收只看直属子任务：cancelled 不进分母，非取消数必须大于 0 且全部 done；父任务还必须为 todo/in_progress + manual，具有 active owner/person assignee 与 active builtin owner reviewer。满足时 system 创建固定摘要、无 Artifact 的 `origin=child_rollup` Submission，并最多进入 `waiting_review`。owner 仍须接受；manual 历史、既有 pending 或 changes_requested child_rollup 不被覆盖。pending 条件失效会由 system 撤回；blocked 保持 blocked 只修正来源状态；accepted 后只有子任务条件失效才 system reopen，且不恢复旧 Assignment。该协调没有迁移/启动回填。

Submission/Artifact 列表默认每页 50、最大 100，并返回 Task `ETag` 和 `meta.task_version`；Artifact 列表可按 `submission_id` 过滤，并用 `include_deleted=true` 查看软删除记录。所有 Artifact 摘要/详情都携带由父 Submission 派生的必填 `submission_status`。详情只在按需读取时返回文本、链接或结构化正文，删除后正文固定隐藏。文件下载只由鉴权 content 端点提供，响应强制 attachment、`nosniff`、`no-store`，并以 SHA-256 作为 `ETag`；缺失或校验不符会更新 `integrity_status` 并拒绝下载。Artifact 删除要求 `confirm=true`、Task `If-Match`、1–1,000 字符原因，pending-review 批次禁止删除；schema v9 在同一事务写入不可变 `artifact_deletion_tombstones`，文件经受控 trash 完成数据库/文件补偿后物理清理。tombstone 在 Task 聚合删除后仍保留，供启动恢复区分授权删除与未知候选；恢复 active trash 前复验 size/SHA-256，错配项进入 quarantine 并标记 mismatch。物理文件已经缺失时，确认软删除仍会成功并记录 `integrity_status = missing`；Task 聚合硬删除也不会被缺失 object 阻断。Task 硬删除在移动文件前还会拒绝开放 Focus Session 和活动 Inbox 关系；用户带原因软解除关系后才可继续。删除随后级联 Submission/Artifact，已解除 Inbox 关系的实时 `task_id` 置空而原 ID/标题快照保留；Workflow Event 中已失效的关联 ID 可置空，JSON 快照仍保留。

任务资料、Task output/review/Artifact 删除及 Assignment 命令均使用 Task 版本拒绝旧写入；可重试命令可携带稳定 `Idempotency-Key`，同键同请求重放首次响应，同键不同请求返回冲突。前端遇到版本冲突会刷新最新 Task，同时保留摘要、文本、链接、结构化 JSON 与浏览器 `File` 对象草稿，要求用户再次明确提交。事件查询默认每页 50、最大 100，返回 Task `ETag`、`meta.task_version` 和按时间/命令顺序倒序的追加式事件。Assignment 没有 DELETE 路由。批量任务和计划组排序在请求体中携带每项 `expected_version` 并整批校验。项目修改、状态流转和硬删除也必须携带 `If-Match`；任务/发票/客户聚合事实变化会递增项目版本。永久删除只允许已归档项目，并会按外键策略解除任务和发票关联而不删除这些业务记录。归档项目资料必须先恢复再编辑，也不接受新的任务关联；其既有关联任务仍可编辑。项目列表默认排除归档项；只有需要读取完整关联历史的页面才显式传 `include_archived=true`。

Project 列表默认每页 50、最大 100，支持名称/描述 `q`、状态、客户、`include_archived` 和白名单排序；默认候选排除归档项目。所有排序追加 `id ASC`，同名项目也具有确定分页顺序；每次列表请求在同一只读事务内完成总数统计与当页读取，避免 `meta.total` 和结果页来自不同读快照。共享 `ProjectSelect` 固定每页 20 条、250 ms 服务端搜索，并把 `q / page / includeArchived` 纳入 Query key、向列表和选中详情请求传递取消信号。当前选中项可由详情或调用方名称补齐，加载失败不会自动清空；Task 详情中的既有归档关联继续可见但默认不能作为新的候选。

`GET /api/v1/projects/:id/artifacts` 在 Artifact/Task/Submission 摘要之外，为每项返回 nullable `followup`。存在稳定 follow-up 来源时包含 `inbox_item_id / inbox_item_version / status / resolution_policy / source_deleted_at / progress`，其中 progress 实时从活动 Inbox–Task 关系与当前 Task 状态派生；未标记 Artifact 返回 `null`。响应继续使用 Project 聚合数值 `ETag`，并与 `meta.project_version` 表示同一 Project 并发版本；它不是完整响应内容哈希，也不能用于 Inbox 写入。`followup.inbox_item_version` 才是 Inbox `If-Match`，当前 Project UI 只深链 Inbox。Inbox/Task 变化不递增 Project version；前端刷新先取消来源 Project 的在途查询再失效缓存，Artifact 请求消费 `AbortSignal`，避免旧响应回填。

Client 列表默认每页 50、最大 100，支持 `q`、`status` 和白名单 `sort`，所有排序追加 `id ASC`；响应实时返回 `project_count` 和未删除活动派生的 nullable `latest_activity_at`。名称和可选联系人、邮箱、电话、备注由服务端 trim、限长并校验，可选空白保存为 `null`，状态只接受 `active / lead / inactive`。创建可使用 `Idempotency-Key` 重放首次 `201` 快照；创建、详情和更新返回 Client `ETag`，PATCH/DELETE 必须携带 `If-Match`。永久删除还要求 `confirm=true` 且 Client 已停用；Invoice 强引用返回可解释冲突且不改变事实，Project 可选外键置空并返回 `detached_projects`。Project 关联变化会使 Client 聚合版本失效，Client 名称变化继续使关联 Project 版本失效。

Client Activity 列表默认每页 20、最大 100，按 `occurred_at DESC, id ASC` 稳定分页，默认隐藏软删除记录，可按 kind 筛选并用 `include_deleted=true` 查看审计历史。公开创建只接受人工 `note / meeting`，标题、正文与 RFC 3339 发生时间由服务端校验，可使用 `Idempotency-Key`；PATCH/DELETE 使用活动 `If-Match`，删除还要求 `confirm=true` 和原因。活动新增、修改或软删除会在同一事务递增 Client 聚合版本；删除响应不返回人工正文，删除记录和预留的 `system_reference` 均只读。

Client Attachment 列表默认每页 20、最大 100，按 `created_at DESC, id ASC` 稳定分页，支持 `activity_id` 和 `include_deleted=true`。上传强制 Client `If-Match`，使用首个 `metadata` JSON + 唯一 `file` 的严格 multipart，可带 `Idempotency-Key`；单文件非空且最多 50 MiB，完整请求最多 100 MiB。下载只经鉴权 content 端点并复验 size/SHA-256；确认删除要求 Client `If-Match`、原因和可选幂等键。附件新增/软删除递增 Client 版本；Client 永久删除会协调全部 active 附件的 tombstone、trash 与事务补偿。

Project Attachment 使用相同受控文件协议，按 `created_at DESC, id ASC` 稳定分页并支持 `include_deleted=true`。上传和确认删除都使用 Project `If-Match` 与可选幂等键，归档项目只读；新增/软删除递增 Project 聚合版本，永久删除 Project 会协调 active 附件的 tombstone、trash、事务回滚和最终清理。内部备份包含 active Task/Client/Project objects，业务 JSON 只含附件元数据和 active 文件摘要，不嵌入正文。

Client contact 关系列表默认每页 20、最大 100，按 `linked_at DESC, id ASC` 稳定分页，`include_unlinked=true` 显式读取解除历史。关联必须使用 Client `If-Match`，并在已有 active person 的 `actor_id` 与原子 `create_person` 间二选一；解除要求 `confirm=true`、Client `If-Match`、原因和可选幂等键。每个 Client 同时只允许一个 active contact；关联/解除递增 Client 版本，active 关系阻止 person 停用，Client 删除级联关系历史但保留 Actor。业务 JSON 导出包含该关系历史。

当前 Inbox 创建 API 只接受 `kind / source_entity_type / resolution_policy = manual` 的手工条目，不接受来源 ID 或事件键。列表支持 `inbox / snoozed / archive` 三视图、标题/摘要搜索、优先级和分页；`meta.unread_total` 始终统计全局当前待处理视图的未读，不受当前视图或筛选影响。`read_at`、`snoozed_until` 与主状态相互独立；resolve/dismiss 要求 1–2,000 字符原因、清除稍后但不隐式已读，未读终态仍可直接 read。reopen 清除终态和稍后事实，保留 read/triaged，并按是否存在活动 Task 关系进入 `tracking / open`。PATCH 与单条命令使用 `ETag`/`If-Match`；创建、命令和 read-all 支持幂等快照。read-all 提交列表 `snapshot_at` 作为 `through_created_at` 时间截止，只标记创建与最后更新时间均不晚于 cutoff、且按该 cutoff 仍属于待处理可见范围的未读；截止后变化的条目保守跳过。

Task 关系 GET 返回实时 progress，活动关系与仍有实时 Task 的历史关系可打开共享 Task 详情。split 可原子创建父子 Task、独立完成条件、owner/person Assignment、manual owner reviewer、显式 required 关系与事件；可信来源 Project 作为默认值带入但可逐项清除/改选，person 只作本地责任记录。自动策略由 system 结清/重开；父子层级和 child_rollup 不创建 Inbox 关系，也不继承或改写 required。所有会改变 follow-up 的成功 Inbox mutation 失效来源 Project，split 另失效 Task、Today、Project。`submit-output` 对每个显式 follow-up Artifact 同事务创建稳定去重来源项；活动来源阻止 Artifact/Task 删除，归档后保留来源快照。当前没有公开来源创建或 Inbox 删除路由。

Reminder API 提供一次性及 daily/weekly 本地提醒的分页/搜索/状态列表、创建、详情、并发安全编辑和带原因停止系列。公开创建固定为 manual 来源；自动化调度器可创建 `source_entity_type=automation` 的一次性 Reminder。Sidecar ready 前补扫，随后每 15 秒扫描；稳定键、系列唯一约束和单事务保证 occurrence、Inbox 与下一 occurrence 恰好一次。跨 DST 保持当地钟点，离线积压折叠；系统原生通知、远程推送、每月和自由日历规则仍未实现。

Automation API 只暴露五个代码所有的稳定预设，不提供创建任意 trigger/action 的接口。规则配置、启停使用版本化 `If-Match`；preview 由服务端规范化参数并返回下一计划与权限。当前可用 Project 完成 Inbox、daily/weekly Reminder；发票 Task 和 Agent failure Inbox 固定 unavailable。Run 保留终态、attempt、快照、结果/安全错误码，失败最多三次并可手动重试。没有 Shell、SQL、HTTP、外发、AI/LLM 或 Agent Runtime。

Agent Adapter API 只允许登记代码所有的 `builtin-local-text-v1`，提供列表、详情、手动诊断、启用拒绝和停用；写命令使用幂等键或 `If-Match`。设置“本地 Agent”展示协议、允许能力和三个安全闸门。当前诊断固定为 `PLATFORM_ISOLATION_UNVERIFIED`，`execution_ready=false`，不会接受可执行路径、启动子进程、创建 agent Actor/Assignment/Run 或执行任务。

Focus API 快照统一返回 `session / server_now / elapsed_seconds / remaining_seconds`，有 Session 时携带 ETag。`planned_seconds` 为 300–7200；已有 Session 命令强制 `If-Match`，create/stop/cancel 支持 `Idempotency-Key`，匹配终态的重复 stop/cancel 不重复记账。Sidecar 启动把遗留 active 变为 recovery_pending，用户必须选择计入中断间隔继续、排除间隔继续或中断。只有 completed Session 进入 Task 工时、Today 和周期报告；Today/周期报告以 IANA 本地日边界与已关闭正时长 interval 的实际 overlap 计算，兼容旧 `timezone_offset_minutes`。终态历史和周期报告均可选严格 canonical UUID `project_id`：空值、非法或非 canonical 返回 `400 INVALID_PROJECT_ID`，不存在返回 `404 PROJECT_NOT_FOUND`，归档项目仍可读。项目筛选按 Session 绑定 Task 的查询时当前 `project_id` 归类，Task 改绑会重分类旧 Session；无 Task、Task 已删除或当前无项目的记录不进入项目过滤结果。

## SQLite 与迁移

迁移 SQL 位于 `services/sidecar/internal/database/migrations/`，随 Sidecar 二进制嵌入。当前最新版本为 schema v38；启动时按文件版本顺序执行，并记录到 `schema_migrations`。v6–v37 交付 Task/Actor/D2、Client、Focus、Inbox/Reminder、设置、受控文件、来源 guards、重复 Reminder、Automation、Agent Adapter、Client Followup、Roadmap 与 Content Item 数据契约；schema v38 新增 Content Item→Inbox 的来源校验、唯一事件键和删除协调，迁移不创建业务数据或投影；后续从 `039_*` 追加。每个连接启用：

- `PRAGMA foreign_keys = ON`
- `PRAGMA journal_mode = WAL`
- `PRAGMA busy_timeout = 5000`

新增迁移时只添加新的递增版本文件，不修改已发布迁移。会删除、重建或不可逆改写既有事实的迁移必须在连续文件头标记 `-- migration: destructive`；已有工作区启动时先完成安全迁移，再在首个破坏性迁移前生成并验证自动回滚包。新建空库不创建无意义的迁移备份，备份失败则数据库停留在破坏性边界之前并拒绝 ready。

## 产品边界

[PRD v9.41](docs/opc-workspace-PRD.md) 是范围、目标契约与当前实施状态依据。v0.1 基座已交付核心人工闭环、数据安全、启动恢复基座和命令面板/新建任务的原生快捷键；客户回访 C2–C5 已完成并支持完成时原子下一次计划，C6 已覆盖跨浏览器时区与 DST 墙上时间、详情时间线状态/负责人/服务端逾期筛选、待回访责任的人员停用保护及停用客户不可再续写回访计划的收口边界，其他并发/真实多时区专项仍待。v0.2 首个受限预设自动化纵切已交付本地 Inbox/Reminder 动作，本地 Agent 已完成 Adapter 登记/诊断但尚无 Runner/Run。明确无 AI/LLM、可执行 Agent Runtime、外发和自由规则。真实浏览器/WebView 休眠/时区切换、真实父崩溃/进程树、三平台安装包与后续客户/财务/桌面能力仍未完成。
