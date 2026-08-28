# 数据管理、备份与恢复模块

> 文档状态：基础设施部分完成，业务能力未实现。v0.1 必须交付可验证的一致性备份与原子恢复；v0.3 才增加计划和高级导入。

## 定位与边界

本模块负责 opc-workspace 本地数据的生命周期：数据库迁移、一致性快照、导出、导入、恢复、附件与产出归档、完整性验证和诊断包内容选择。运行日志落盘与轮转由桌面平台统一负责；本模块只通过 request ID 消费日志用于恢复诊断。它是本地优先产品的发布闸门，而不是附加工具。

核心边界：

- 所有核心业务数据默认只保存在本机，不依赖线上存储或云备份。
- 正在使用 WAL 的 SQLite 数据库不能通过普通文件复制生成可信备份。
- v0.1 必须包含手动一致性备份、破坏性迁移前备份、基础 JSON 导出、manifest、SHA-256、临时库验证和原子恢复。
- v0.3 才增加计划备份、外部目录、保留策略、CSV 映射和高级导入；不能把基础恢复延后到 v0.3。
- 备份成功必须以完整性校验和实际可打开/可恢复验证为依据，不以“文件已生成”为依据。
- 恢复失败不得覆盖当前可用数据；恢复前先创建当前状态快照。
- 数据管理不改变 Task、Inbox Item、Assignment、Agent Run 等对象的事实边界，只保证它们作为一个一致集合被保存和恢复。
- 当前阶段没有线上服务或在线 Updater，备份流程不得静默联网。

## 当前实现状态

已实现基础：

- Tauri 在 appDataDir 创建主数据库目录以及 attachments、invoices、backups、config 等目录，并创建 appLogDir。
- Go 使用 SQLite 单物理连接，启用 foreign_keys、WAL 和 5 秒 busy_timeout。
- SQL 迁移嵌入 Sidecar，按递增版本逐个事务执行。
- schema_migrations 记录版本和迁移名，并拒绝未知版本或同版本不同文件名。
- 应用正常退出时执行 WAL checkpoint 后关闭数据库。
- 开发数据库位于仓库 .local/dev-data，与正式 appDataDir 隔离。

尚未实现：

- 没有 SQLite Online Backup API 或 VACUUM INTO 快照服务。
- 没有破坏性迁移前自动备份；当前 database.Open 会直接执行迁移。
- 没有备份 manifest、SHA-256、integrity_check、foreign_key_check 或实际恢复验证。
- 没有手动备份、恢复、JSON/CSV/SQLite 导入导出 API 和 UI。
- 没有维护锁、长任务进度、取消、低磁盘检查或原子替换流程。
- 没有统一 Artifact 目录及附件/产出归档协议；Tauri 当前也未创建 PRD 规划中的 artifacts 目录。
- Tauri 虽传入 OPC_LOG_DIR，当前 Go Sidecar 仍只向 stderr 写日志，没有日志落盘、轮转或脱敏诊断包。
- 没有备份失败或迁移失败的收件箱事件和恢复页面。

## 目标功能

### 应用数据布局

目标目录：

| 路径 | 内容 |
|------|------|
| appDataDir/opc-workspace.db | SQLite 主数据库 |
| appDataDir/attachments/ | 用户导入的附件 |
| appDataDir/artifacts/ | 人或本地 Agent 的 Task Artifact |
| appDataDir/invoices/ | 本地生成的发票 PDF |
| appDataDir/backups/ | 本机一致性快照与归档 |
| appDataDir/config/ | 数据库启动前必需的非敏感配置和迁移标记 |
| appLogDir/opc-workspace.log | 桌面平台拥有的脱敏轮转日志；默认不纳入业务备份，仅在用户确认的诊断包中引用 |

数据库和归档只保存受控目录下的相对路径；不得把任意绝对路径作为可直接读取的业务引用。

### v0.1 一致性备份

- 使用 SQLite Online Backup API 或 VACUUM INTO 创建一致性快照。
- 支持用户手动创建和破坏性迁移前自动创建。
- 在开始前检查目标空间和当前维护任务；同一时刻只允许一个备份或恢复作业。
- 每份备份生成 manifest，至少包含格式版本、创建时间、app/API/schema 版本、数据库文件、附件与 Artifact 清单、大小和 SHA-256。
- 创建后执行 integrity_check、foreign_key_check 和临时打开验证。
- 只有全部验证通过才把临时文件原子改名为可见备份。
- 备份列表展示创建原因、版本、大小、验证状态和可恢复性。

### v0.1 基础导出

- 导出带 schema_version 和 export_format_version 的完整 JSON 数据包。
- “完整”表示覆盖导出版本已经交付的全部业务与非敏感配置事实，包括 Task、Project、Client、Client Activity/Attachment、Actor、Assignment、Inbox Item、Reminder、Task Artifact 元数据、Focus Session、Financial/Invoice（若已交付）、Workflow Event 和 `app_settings`；会话令牌、Agent 单次令牌及操作系统安全存储凭据明确排除并写入 manifest。
- 支持导出 SQLite 一致性快照。
- 用户选择包含附件和 Artifact 时，生成数据库、文件和 manifest 的统一归档。
- 外部 URL 只作为引用保存，不在导出时自动下载。
- 导出过程不得在日志记录客户正文、发票内容或任务产出正文。

### v0.1 验证与原子恢复

- 恢复开始前自动创建当前状态快照。
- 先读取 manifest，校验格式、版本、平台无关性、文件清单、SHA-256 和可用磁盘。
- 在独立临时目录和临时数据库中完成解包、完整性检查、外键检查和必要迁移。
- 验证附件与 Artifact 路径、大小和校验值，拒绝路径穿越和符号链接逃逸。
- 进入短暂维护模式，停止新写入并等待进行中的事务完成。
- 原子替换数据库和受控文件集合；替换失败回滚到当前状态快照。
- 重启 Sidecar，完成 health、schema 与抽样读取验证后才报告成功。
- 恢复失败保持原数据可用，提供 request ID、阶段和脱敏日志。

### v0.1 基础导入

- 仅接受 opc-workspace 自身导出的明确版本完整 JSON、SQLite 快照或归档格式。
- v0.1 只支持“以完整快照恢复/替换”，不提供与当前数据库合并；预览展示覆盖范围、版本和校验结果，而不是伪造新增/更新冲突合并。
- UUID、外键、状态机、唯一约束、Artifact 校验和与 manifest 在临时库/目录中验证。
- 经验证的完整快照必须原样保留已验收、已付款、Agent succeeded 等历史事实及其审计，不能通过业务命令重放或降级这些状态。
- 恢复操作追加独立的本机恢复记录，但不得改写快照内部的历史 Workflow Event。

### 迁移安全

- 已发布的迁移只增不改，新迁移使用递增版本。
- 破坏性迁移前自动创建并验证备份；失败则不开始迁移。
- 迁移在事务中执行，完成后检查 schema、外键和关键索引。
- 迁移失败时 Sidecar 进入可诊断恢复状态，桌面显示恢复页面。
- 应用、Sidecar、API 和 schema 建立明确兼容矩阵；数据库版本过新时拒绝写入。

### v0.3 高级能力

- 每日首次满足条件时运行计划备份，并在错过计划后进行启动补偿。
- 用户通过系统文件对话框选择独立外部备份目录。
- 可配置保留策略，默认保留最近 30 份；删除前检查备份状态和路径范围。
- 支持任务、客户、财务等 CSV 导出与字段映射导入。
- 导入前提供映射、规范化和冲突预览。
- 周期性执行可恢复性抽检，并把失败作为本地系统维护事件进入收件箱。

## 关键用户流程

### 手动创建备份

1. 用户从设置进入“数据与备份”，点击创建备份。
2. 应用显示数据范围、目标目录、预计空间和当前版本。
3. Sidecar 获取维护作业锁，创建临时一致性快照。
4. 系统生成 manifest 和 SHA-256，执行数据库与文件完整性验证。
5. 验证通过后原子发布备份，并记录成功结果。
6. 验证失败则删除或隔离临时产物，保留主数据并显示可定位错误。

### 破坏性迁移前备份

1. Sidecar 检测到待执行的破坏性迁移。
2. 在应用新版本写入任何新 schema 前创建迁移前快照。
3. 快照验证失败时停止启动并进入恢复界面。
4. 快照通过后执行事务迁移和迁移后检查。
5. 迁移失败保留原数据库与备份，并允许打开日志或从备份恢复。

### 从备份恢复

1. 用户选择本地备份或归档文件。
2. 应用只读解析 manifest，展示来源版本、时间、内容和风险。
3. 用户确认后，系统先备份当前状态。
4. 在临时目录完成校验、迁移和可打开性测试。
5. 进入维护模式并原子替换。
6. Sidecar 重启且 health、schema、外键和抽样读取通过后，界面报告恢复成功。
7. 任一步骤失败都回到原数据，并保留可诊断信息。

### 导出完整数据

1. 用户选择 JSON、SQLite 快照或含附件的完整归档。
2. 系统通过原生文件对话框选择目标文件，不接受任意前端路径字符串。
3. Sidecar 生成临时导出并校验。
4. Tauri 将已验证文件写入用户选择的位置。
5. 应用展示文件位置、大小、校验值和格式版本。

## 数据/API/状态与事件

### 备份 manifest

建议 v0.1 固定以下最小字段：

| 字段 | 含义 |
|------|------|
| format_version | 归档协议版本 |
| backup_id / created_at | 稳定 ID 和 UTC 时间 |
| reason | manual / pre_migration / pre_restore |
| app_version / api_version / schema_version | 兼容性判断 |
| files | 归档内相对路径、大小、类型和 SHA-256 |
| checks | integrity_check、foreign_key_check、open_test 结果 |
| source_platform | 诊断信息；不得阻止平台无关数据导入 |

### 作业状态

备份作业：

- queued → snapshotting → verifying → succeeded
- queued / snapshotting / verifying → failed 或 cancelled

恢复作业：

- inspecting → validating → ready_to_apply → applying → restarting → succeeded
- 任一非终态 → failed；进入 applying 后不允许普通取消

数据库维护锁和作业状态必须由 Sidecar 或专用恢复控制面统一管理，前端轮询或订阅展示，不自行推断。进入 applying/restarting 后，桌面平台阻止普通退出；恢复协调器写入本地 journal，使操作系统强制终止后的下次启动可以完成或回滚原子替换。

### API 与桌面命令

PRD v1.9 尚未冻结备份端点。实现前建议确定以下职责：

| 能力 | 建议接口职责 |
|------|--------------|
| 备份列表/创建/验证 | Sidecar 业务 API，使用 WebView 会话鉴权和作业 ID |
| 归档检查 | 只读检查接口，返回 manifest 与兼容性，不立刻应用 |
| 恢复确认 | 独立危险命令，携带检查结果 ID、二次确认和幂等键 |
| 导出/导入 | Sidecar 负责数据与校验，Tauri 文件对话框负责用户授权路径 |
| 迁移失败恢复 | Tauri command 或恢复模式 Sidecar；不得暴露未鉴权文件访问 |

所有写操作返回 request_id。恢复、导入和迁移使用独占维护锁；状态变化写入数据库维护事件，并通过桌面统一日志管线记录脱敏运行摘要。

### 事件

本地维护事件包括 backup_started、backup_verified、backup_failed、restore_started、restore_succeeded、restore_failed、migration_backup_created、migration_failed 和 data_exported。

失败事件可用稳定 source_event_key 幂等生成 system_maintenance Inbox Item。Workflow Event 不记录归档密码、绝对敏感路径、客户正文或文件内容。

## 与其他模块协作

- [设置](settings.md)：提供手动备份、导出、恢复、目录和诊断 UI。
- [桌面平台](desktop-platform.md)：提供 appDataDir、文件对话框、维护恢复页、Sidecar 重启、签名离线更新和日志目录。
- [任务](tasks.md)、[项目](projects.md)、[客户](clients.md)、[收件箱](inbox.md)：作为统一事务事实被导出和恢复，不由数据模块重写其业务状态。
- [Actor](actors.md) 与 [本地 Agent](local-agents.md)：备份 Actor、Assignment、Run、Artifact 和审计；恢复后重新校验本地执行文件和权限。
- [专注](focus.md)：Session 与 tasks.actual_minutes 必须保持一致，恢复验证覆盖重复累计。
- [财务与发票](finance-invoices.md)：v0.4 后归档发票 PDF 与财务事实，付款状态不能通过普通合并导入绕过确认。
- [收件箱](inbox.md)：备份、恢复和迁移失败可生成去重的系统维护待办。

## 分阶段实施

### v0.1-A：备份内核

- 定义 manifest、归档布局、作业状态和错误码。
- 实现 Online Backup API 或 VACUUM INTO、SHA-256 和临时发布。
- 增加 WAL 活跃、低磁盘、数据库损坏和并发写入测试。

### v0.1-B：迁移前保护

- 将 database.Open 拆为兼容检查、备份、迁移和验证阶段。
- 标记破坏性迁移并强制创建已验证快照。
- 建立迁移失败恢复状态和桌面恢复入口。

### v0.1-C：手动导出与恢复

- 实现完整 JSON、SQLite 快照和含受控文件的归档。
- 实现只读检查、恢复前快照、临时库迁移和原子替换。
- 接入设置页进度、确认、错误、重试和诊断。

### v0.1-D：文件与诊断归档一致性

- 增加 artifacts 目录、受控相对路径和 SHA-256 校验。
- 消费桌面平台提供的脱敏日志索引，生成用户确认的诊断包；本模块不实现第二套日志落盘或轮转。
- 让系统维护失败幂等进入收件箱。

### v0.3：计划与高级导入

- 增加计划、启动补偿、外部目录和保留策略。
- 增加 CSV 字段映射、冲突预览和安全合并；合并导入不得直接构造已验收、已付款或 Agent succeeded 等高风险事实，必须转换为待确认数据或走受控业务命令并记录导入来源。
- 增加周期性可恢复性抽检和历史执行记录。

## 验收标准

- WAL 有未 checkpoint 写入时创建的备份仍能通过 integrity_check 和 foreign_key_check。
- 每份成功备份都有完整 manifest、文件清单和 SHA-256，且在独立临时库实际打开验证。
- 破坏性迁移没有已验证备份时不能开始。
- 恢复前必定创建当前状态快照；损坏、未知 schema、版本过新、校验失败或低磁盘均不覆盖当前数据。
- 恢复只在临时库验证成功后进入原子替换；替换失败可以回到原数据。
- 路径穿越、绝对路径、符号链接逃逸和清单外文件被拒绝。
- 重复创建、恢复确认和导入请求不会生成重复资源或重复业务事实。
- 数据库、附件、Artifact 和发票文件恢复后引用一致，没有静默缺失。
- Focus Session 与任务工时、Inbox Item 与必需任务、Assignment 与 Actor 的一致性检查通过。
- 整个基础备份恢复流程断网可用，不调用线上服务。
- v0.1 不依赖计划备份或外部目录才能恢复；v0.3 高级能力不会重新定义基础格式。
- 日志和 manifest 不包含会话令牌、能力令牌、归档密码或敏感正文。
- 至少在每个支持平台的干净系统完成一次“备份 → 修改数据 → 恢复 → 核对”验收。

## 相关代码/PRD链接

- [PRD：数据持久化方案](../opc-workspace-PRD.md#7-数据持久化方案)
- [PRD：备份策略](../opc-workspace-PRD.md#73-备份策略)
- [PRD：数据安全](../opc-workspace-PRD.md#74-数据安全)
- [PRD：SQLite 初始化与迁移](../opc-workspace-PRD.md#1044-t-04-sqlite-初始化迁移与开发数据隔离)
- [当前数据库初始化](../../services/sidecar/internal/database/database.go)
- [当前迁移执行器](../../services/sidecar/internal/database/migrate.go)
- [当前初始 schema](../../services/sidecar/internal/database/migrations/001_initial_schema.sql)
- [当前桌面数据目录创建](../../apps/desktop/src-tauri/src/sidecar.rs)
- [当前 Sidecar 退出与 checkpoint](../../services/sidecar/cmd/server/main.go)
