# 数据管理、受控文件、备份与恢复模块

> 当前基线：app v0.1.0 / API v1 / SQLite schema v17（2026-08-28）
>
> 事实边界：SQLite 初始化/迁移、开发/正式数据隔离、Task Artifact 受控文件，以及 T-04B 手动一致性备份的创建、列表、完整校验、隔离恢复演练、重启前安全恢复、确认删除和基础业务 JSON 导出已经实现；导入、含文件导出包、迁移前自动备份、计划备份和完整跨版本恢复矩阵仍未实现。

导航：[文档中心](../README.md) · [整体功能架构](../functional-architecture.md) · [PRD v5.1](../opc-workspace-PRD.md) · [任务](tasks.md) · [桌面平台](desktop-platform.md)

## 定位与边界

本模块负责本机数据的物理边界、迁移一致性和未来可恢复性：

- SQLite 业务事实与版本化 schema；
- Task Artifact 文件与数据库元数据的一致性；
- 开发数据、正式数据和测试数据隔离；
- 未来一致性备份、验证、恢复、导入导出和保留策略。

本模块不负责云同步、多设备合并、在线账号、远程文件服务、SQLCipher 或自动上传。当前“备份成功”只表示 SQLite 快照、active Artifact、marker、manifest 与数据库一致性已经在 staging 中完整校验并原子发布；它不表示恢复、导出或外部介质复制已经完成。

## 当前实现状态

### 已实现

- SQLite 使用单物理连接、`foreign_keys=ON`、WAL 和 5000 ms `busy_timeout`；正常退出执行 `wal_checkpoint(TRUNCATE)`。
- 迁移 SQL 通过 Go `embed` 编入 Sidecar，按编号记录到 `schema_migrations`；未知版本/文件名不一致拒绝启动。
- 需要重建表的迁移可用首行 `-- migration: foreign_keys=off`，迁移器在固定连接上事务外关闭外键、事务内执行 SQL 与 `foreign_key_check`，成功或失败均恢复外键。
- schema v9 新增单例 `workspace_identity`：`database_id` 永久不可变，`artifact_store_id` 只能从空值绑定一次；两者把受控 Artifact root 与数据库一一对应。迁移还新增不可变 `artifact_deletion_tombstones`，在 file Artifact 软删或 Task 聚合硬删的同一事务保留删除授权事实；Submission/Artifact 迁移只对无歧义 manual 状态回填 inferred Submission 和 system 事件，不虚构 Artifact。
- schema v10 为 Client 增加聚合 `version`、名称/状态/更新时间查询索引和 Project 关联变化触发器，并把历史空白可选资料归一为 `NULL`；迁移不改写 schema v9 的 Artifact store 契约。
- schema v11 重建 `focus_sessions`，新增有效工作区间账本 `focus_session_intervals` 和精确秒数余量账本 `task_focus_totals`；旧 Focus 记录按终态映射并补 interval，不二次增加 Task 工时，并用只适用于迁入终态的 `legacy_imported` 标记无损保留旧 schema 中超过 120 分钟的合法记录。
- schema v12 以加法迁移新增 `inbox_items` 及列表/未读查询索引；`source_event_key` 只对非空值建立部分唯一索引。v11→v12 升级测试验证既有 Client 等业务事实不变，当前迁移不创建 demo Inbox 数据。
- schema v13 以加法迁移新增 `inbox_item_tasks`、活动关系唯一/position/软解除约束、原 Task ID/标题快照与活动关系 Task 删除保护。v12→v13 不重建 Task、Inbox Item 或其他模块表，也不创建 demo 关系。
- schema v14 以加法迁移新增 `reminders`、稳定来源事件键、终态字段分组、触发投影引用一致性、身份/终态不可变与硬删除保护。v13→v14 不重建既有表、不改写业务事实，也不创建 demo Reminder。
- 开发数据库与 Artifact 位于 `.local/dev-data/`；桌面正式数据位于 Tauri `appDataDir`，互不复用。
- Tauri 创建 `appDataDir/artifacts/`，通过 `OPC_ARTIFACT_DIR` 交给 Sidecar；开发脚本使用 `--artifacts .local/dev-data/artifacts`。
- Sidecar 声明并校验 Artifact root，管理含 `format_version / database_id / store_id` 的 JSON marker、进程级独占锁、staging、objects、trash 与 quarantine；拒绝卷根、符号链接/reparse point、非空无 marker、marker 不规范，或数据库 ID / store ID 任一不匹配的目录。同一 root 已被另一个 Sidecar 持有时拒绝启动。
- 文件上传流式计算 MIME、size、SHA-256，先写 staging，再无覆盖提升到 objects；数据库只保存 `objects/<artifact-id>` 相对路径。marker、文件数据、对象提升、移动/删除与关键目录项做耐久同步。
- 启动时清理普通 staging 残留、协调中断的已知 trash move，并用 tombstone 区分授权删除与未知候选。active trash 恢复前必须匹配数据库 size/SHA-256；错配移入 quarantine 并记录 mismatch。无引用且没有可验证删除授权的受控 UUID object/trash 候选同样只进 quarantine，不自动永久删除，也不递归跟随异常目录或链接。
- Artifact 软删除和 Task 聚合硬删除使用 trash 做文件/数据库补偿：事务失败恢复，提交后清除临时 trash 文件；物理 object 已缺失时仍允许确认软删或聚合硬删，软删同时记录 missing 完整性事实。
- 下载前重新验证 size 与 SHA-256；missing/mismatch 状态写回数据库并拒绝输出内容。
- Sidecar 从 `--backups` / `OPC_BACKUP_DIR` 获取备份根；统一开发脚本显式使用 `.local/dev-data/backups/`，文件数据库未显式配置时默认使用数据库同级 `backups/`。备份根必须是安全的专用目录，不得为卷根、包含数据库文件或与 Artifact root 重叠。
- `POST /api/v1/backups` 在进程内维护写锁中执行：所有普通 API 请求、Focus heartbeat 与 Reminder 扫描先完成或等待，避免数据库事实与 Artifact 文件跨写入边界漂移；同一时间只执行一个备份操作。
- SQLite 通过同一连接执行 `VACUUM INTO` 得到包含 WAL 已提交事实的一致性快照；active file Artifact 按数据库记录逐个复制并同时校验 size/SHA-256，marker 必须匹配不可变 `database_id / artifact_store_id`。
- 备份先写同一 backup root 下的 `.staging-<uuid>`，manifest 记录 app/commit/API/schema、创建与校验时间、可选说明、数据库/marker/Artifact 相对路径及 size/SHA-256；`quick_check`、`foreign_key_check`、schema、身份、active Artifact 元数据、文件全集和总量均通过后才原子重命名为 `backups/<backup-id>/`。
- 创建支持可选 `Idempotency-Key`；Sidecar 只在备份 manifest 保存 key 的 SHA-256 与规范请求摘要。模糊响应可安全重放同一包，不同说明复用同一 key 返回冲突。
- `GET /api/v1/backups` 只读取已发布 UUID 包并展示上次校验记录；损坏清单以 invalid 项显示。`POST /api/v1/backups/:id/verify` 重新逐字节校验完整包并刷新 `verified_at`，篡改、缺失、额外文件、路径或数据库事实不一致均拒绝。
- `POST /api/v1/backups/:id/drill` 在再次完整校验源包后，将数据库、marker 与 objects 复制到 backup root 内的唯一临时数据根；使用当前迁移器打开副本，执行最终 quick/foreign-key/schema/identity 校验，声明临时 Artifact store 并逐个验证 active file Artifact。成功或失败都关闭临时句柄并清理临时根，源备份和当前数据均不修改。
- `POST /api/v1/backups/:id/restore` 要求 `{ "confirm": true }`。Sidecar 在维护写锁内再次演练目标、为当前数据创建完整自动回滚包，再原子发布私有 pending package/plan；随后普通 v1 请求、Focus heartbeat 和 Reminder 扫描停止写入，同目标重放返回原安排，不同目标冲突。
- 下一次 Sidecar 启动会在打开正式 SQLite 与 Artifact lease 前验证目标包和回滚包，在同父目录准备并迁移数据库副本及完整 objects，逐步交换 live/old/new 路径并复验最终数据库、身份和文件全集。失败时恢复旧数据库（含 WAL/SHM）与 objects 并隔离计划；成功复验后先把 pending 原子推进为 applied 提交点，再清理旧副本，避免清理中断导致重复应用。
- `DELETE /api/v1/backups/:id?confirm=true` 永久删除一个 canonical UUID 包，不要求包仍能通过完整校验，因此损坏包也可清理。删除前递归拒绝 symlink/reparse 和非普通文件，再把精确包原子重命名为 `.deleting-<id>`、同步 backup root 后清理；中断后同一请求从隐藏路径续删。pending 恢复期间该路由与普通 API 一样被冻结。
- `GET /api/v1/exports/business-data` 在一个 SQLite 读事务内读取显式业务表白名单，输出 business-export format v1 的 attachment。表、列和行顺序稳定；文件 Artifact 的元数据保留，正文不嵌入并以 active 数量/字节摘要声明。schema migrations、workspace identity、幂等响应、删除墓碑、派生 Focus totals、会话令牌和机器绝对路径均不进入包；任一白名单表不可用时整体失败，不返回部分文件。
- 设置“数据与备份”提供业务 JSON 下载，以及备份说明、创建、加载/空/错误状态、摘要、显式重新校验、恢复演练、二次确认恢复和永久删除；安排恢复成功后，桌面模式提供一键安全重启，浏览器开发模式明确提示手动停止并重启外部服务。长操作使用 180 秒客户端窗口。当前没有导入或含文件导出按钮，避免以无行为控件暗示能力。

### 仍未实现

- 导入预览/执行、包含 Artifact 文件正文的外部包；
- 选择外部备份包、路径对话框和跨版本恢复兼容矩阵；
- 破坏性迁移前产品化自动备份；
- 计划备份、保留策略、增量备份、加密和云目标。

## 当前应用数据布局

物理根由 Tauri/操作系统决定，文档和业务逻辑不得硬编码机器绝对路径：

```text
appDataDir/
  opc-workspace.db
  attachments/                 # 预留；客户/项目附件尚未实现
  artifacts/
    .opc-artifact-store-v1     # format_version + database_id + store_id JSON marker
    .opc-artifact-store.lock   # 进程级独占 lease 文件
    .staging/                   # 上传暂存；启动清理普通孤儿文件
    objects/                    # UUID 文件对象
    .trash/                     # 删除事务补偿区；非用户回收站
    .quarantine/                # 无引用候选隔离区；不自动永久删除
  invoices/                    # 预留；PDF 业务尚未实现
  backups/
    <backup-id>/               # 已发布且创建时完成全量校验的备份包
      manifest.json
      database/opc-workspace.db
      artifacts/.opc-artifact-store-v1
      artifacts/objects/<artifact-id>
  config/                      # 预留；当前部分设置仍在前端 localStorage

appLogDir/
  opc-workspace.log            # 日志落盘管线尚未完成
```

开发环境等价使用：

```text
.local/dev-data/
  opc-workspace.db
  artifacts/
    .opc-artifact-store-v1
    .opc-artifact-store.lock
    .staging/
    objects/
    .trash/
    .quarantine/
  backups/                     # 与正式备份完全隔离
```

`attachments/` 与 `artifacts/` 不是同一协议：当前只有 Task Artifact 已经受 Sidecar 管理；客户/项目附件仍待单独设计。`.trash/` 是提交前后短暂补偿目录，不是可由用户浏览或恢复的长期回收站；`.quarantine/` 保留无法安全自动归属的受控候选，当前没有用户级查看或清理入口。

## Artifact 一致性契约

### Root 声明

1. Artifact root 不能为空、不能等于数据库文件、不能是文件系统卷根。
2. schema v9 的不可变 `workspace_identity.database_id` 是数据库身份；`artifact_store_id` 初始为空且只能绑定一次。未绑定数据库首次声明空 root 时写入 `.opc-artifact-store-v1`，其规范 JSON 包含 `format_version`、数据库 ID 与新 `store_id`，随后把同一 store ID 写回数据库。
3. 后续启动必须同时匹配 marker 格式、版本、数据库 ID 和已绑定 store ID；已绑定数据库不能改用另一空 root，已有 root 也不能由另一数据库接管。
4. root、objects、staging、trash 必须是实际目录，不能穿越 symlink/reparse point。
5. 协调或读写前通过 `.opc-artifact-store.lock` 获取非阻塞进程级独占锁，并持有到 router 关闭；第二个 Sidecar 不能共用同一 root。

### 上传

1. API 只接收 multipart 文件流，不接收任意本机路径。
2. 严格 JSON body、multipart `manifest` 和单个 structured object 各最大 1 MiB；单文件最大 50 MiB 且必须非空；完整 multipart 请求最大 100 MiB。Sidecar HTTP read/write timeout 为 180 秒，Web 客户端上传与下载端到端超时为 120 秒。
3. 写 `.staging/<artifact-uuid>.part` 时同步计算 SHA-256、size 和内容探测 MIME。
4. 使用 no-replace hard-link 提升为 `objects/<artifact-id>`，不覆盖已有对象；暂存文件、目标目录项和后续移除按支持平台执行耐久同步后才报告成功。
5. SQLite 事务报错时先查询 Artifact 事实，只有能证明 object 无引用才清理；查询失败或模糊 COMMIT 无法证明时保留 object 交给下次 reconcile，避免误删已经落库的文件。成功时只保留 objects 文件与相对路径元数据。

### 读取与完整性

- `relative_path` 固定为 `objects/<artifact-id>`（小写 UUID），API 从不返回该内部路径。
- `integrity_status` 为 `unverified / verified / missing / mismatch`。
- 文件创建时已计算完整性并标记 verified；每次下载仍重新比较实际 size 和 SHA-256。
- 缺失返回 410 并标记 missing；大小/哈希不符返回 409 并标记 mismatch；任何情况都不输出部分或可疑内容。
- 成功下载强制 attachment、`nosniff`、`no-store` 和 SHA-256 ETag。

### 软删除与聚合硬删除

- 单 Artifact 删除要求确认、Task `If-Match` 和原因；pending-review 批次禁止删除。
- text/link/structured 只写软删除元数据；详情仍可看元数据，但正文固定为 null。
- file object 软删在同一事务写 immutable tombstone（size/SHA、`deletion_scope = artifact`）并移到 `.trash/<artifact-id>-<uuid>.trash`；数据库失败则恢复，成功提交后物理清除。object 已缺失时仍完成软删除，并写 `integrity_status = missing` 与检查时间。
- Task DELETE 先检查开放 Focus Session 与活动 Inbox 关系；任一存在都在移动 Artifact 文件前返回冲突。用户带原因软解除关系后，删除在同一事务为每个 active file 写 `deletion_scope = task` tombstone，再移动仍存在的 objects；缺失 object 不阻断聚合删除。随后事务级联删除 Task/Submission/Artifact，并把已解除历史关系的 nullable `task_id` 置空，保留 `task_ref_id / task_title_snapshot`。失败逆序恢复，提交后清除。tombstone 不引用被删聚合且不可修改/删除，供后续启动恢复判定授权删除。
- 已软删文件在单 Artifact 删除成功时已无 object；Task 后续硬删除不重复寻找该文件。

### 启动协调

- `.staging/`：只删除普通文件；意外目录或链接留给人工诊断。
- `.trash/`：仍需存在的 active Artifact 先用数据库 size/SHA-256 校验 trash；匹配时才恢复或清理确定的重复项，错配项进入 `.quarantine/` 并标记 mismatch。已无 Artifact 时，只有与 immutable tombstone 校验一致的授权删除候选可清理，其余候选进入 quarantine。
- `objects/`：只检查文件名可解析为小写 UUID 的普通文件；没有 active file Artifact 引用时移入 `.quarantine/`。非 UUID 文件、目录或链接不递归清理。
- `.quarantine/`：保存无法安全归属的受控候选；启动协调不会自动永久删除，后续诊断/清理能力必须另设显式确认。

这套协调降低中断后的悬挂文件风险，但不是备份、版本历史或用户级恢复功能。

## SQLite 迁移契约

当前 schema v17：

- schema v15 以加法迁移新增 required 关系查询索引与 automatic resolution 校验 trigger；升级不改写业务事实或创建 demo 数据。
- schema v16 以加法迁移新增空的版本化 `app_settings`、active Actor 写入约束和不可变 key/硬删除保护；不插入服务端默认值、不改写 v15 事实或创建 demo 数据。
- schema v17 以加法迁移新增版本化 `task_saved_views`；不改写 Task、设置或其他既有事实，也不创建默认视图/demo 数据。

- 001：核心业务表；
- 002：删除旧固定 demo seed，不删除用户数据；
- 003–005：Project 生命周期、幂等快照和聚合版本；
- 006：Task facts、标签/父子关系和版本保护；
- 007：Actor、Assignment、Workflow Event 与历史责任回填；
- 008：Task 六状态、review policy、生命周期字段、command_seq 和事件不可变保护；
- 009：带不可变 database ID/一次性 store 绑定的 workspace identity、Submission、Artifact、不可变 deletion tombstone、current submission、事件关联及 inferred manual 历史回填；
- 010：Client 聚合版本、查询索引、空白可选值归一，以及 Project 客户关联变化的版本传播 trigger；
- 011：Focus Session 状态/版本重建、有效 interval、Task 精确秒数余量账本、单一开放 Session/interval 约束和 v10 历史兼容迁移。
- 012：手工 Inbox Item 事实、终态成组约束、查询索引及 nullable `source_event_key` 部分唯一索引；不创建 Task 关系、Reminder 或来源投影。
- 013：Inbox–Task 活动/历史关系、required、稳定 position、带原因软解除、原 Task ID/标题快照、nullable 实时 Task 外键和活动关系 Task 删除保护；不创建 Task、Assignment、Reminder、来源投影或自动解决。
- 014：一次性本地 Reminder、稳定唯一 `source_event_key`、scheduled/fired/cancelled 成组约束、fired Inbox 引用一致性、身份/终态不可变和硬删除保护；不创建 demo Reminder，也不改写 v13 事实。
- 015：required Task reconciliation 查询索引与 automatic resolution 数据库校验，不改写 v14 事实。
- 016：版本化 app_settings、设置 schema/version、active 更新 Actor 约束、不可变 key 和硬删除保护；默认值只由 API 读取时提供，不落默认行。
- 017：任务保存视图、JSON/大小/schema/version 约束、大小写不敏感唯一名称及更新时间索引；不保存任务结果。

新增 schema 只能从 `018_*` 继续追加，不修改已发布迁移。迁移测试必须覆盖：真实旧版本数据保留、幂等重跑、约束/索引/trigger/外键、`foreign_key_check`、故障回滚以及外键状态恢复。

## v0.1 备份/恢复目标与当前进度

### 一致性备份

当前已实现的手动备份同时包含：

- SQLite 一致性快照，而不是仅复制可能仍有 WAL 的主文件；
- 所有 active file Artifact objects；
- manifest 与每个文件的 size/SHA-256；
- app/API/schema/export format 版本；
- 创建时间、平台与可选说明。

当前通过进程内维护写锁冻结普通 API、Focus heartbeat 和 Reminder 扫描，再依次完成 SQLite 快照与 Artifact 复制，避免数据库引用与 objects 跨时点不一致。只读 health 仍可响应；备份列表/校验由独立备份文件锁串行化。`attachments/`、`invoices/` 在业务实现后也必须进入同一清单。

### 验证与恢复

恢复的基础安全闭环已经实现：

1. **已实现**：在专用临时根复制备份，验证 manifest、文件 size/hash，并校验 Artifact marker 的 format/database/store ID 与恢复数据库的 `workspace_identity` 一致。
2. **已实现**：用当前数据库入口打开并迁移临时副本，运行 `quick_check`/`foreign_key_check` 与最终 schema 兼容检查。
3. **已实现**：声明隔离 Artifact store，校验每个 active file Artifact 都有对应 object、size/hash 匹配且没有额外对象或路径越界；关闭句柄后清理临时根。
4. **已实现**：二次确认后取得维护写锁，重复演练目标，并完整备份当前正式数据作为自动回滚点；发布 pending 后冻结普通 API 与后台写入。
5. **已实现**：下一次 Sidecar 启动在正式资源打开前，用同父目录 new/old 路径替换数据库、WAL/SHM 和完整 objects；任一步或最终验证失败都恢复旧资源并隔离失败计划。
6. **已实现**：设置页仅在恢复计划成功挂起后显示“立即安全重启”。Tauri `restart_application` 只接受桌面管理的 Sidecar，发送优雅 shutdown、等待真实退出并在超时后只终止精确子进程；退出无法确认时取消重启并返回错误。外部开发 Sidecar 不由桌面壳终止。
7. **已实现**：下一次启动完成最终 schema、identity、数据库一致性与 Artifact 全集验证后，将 pending 原子推进为 applied 提交点，再清理旧副本；清理警告不会重复应用已成功恢复的数据。

恢复绝不能直接覆盖正在打开的 SQLite 文件或部分覆盖 `objects/`。

### 导出/导入

基础业务 JSON 已实现：顶层记录 `format_version / exported_at / source / artifact_files / excluded_operational_tables / tables`；每张表携带稳定 `columns` 和二维 `rows`，便于后续导入器在不依赖 JSON 对象键顺序的前提下映射。当前格式不包含 Artifact 文件正文，会明确声明 `artifact_files.included=false`，因此它不是可直接恢复的备份替代品。

导入必须预览冲突、支持取消、使用新 UUID 或显式映射，并在一个可回滚作业中完成。含文件导出包需先定义 manifest、hash 和相对路径契约，继续排除会话令牌、绝对路径或未来 Agent secret。当前没有导入 API/UI。

### 计划备份（v0.3）

后续再评审定时计划、保留数量、磁盘空间阈值、增量方式、加密和可选外部目标。不得在 v0.1 悄悄启用后台上传或付费云资源。

## 当前 API 与后续作业状态

当前创建和校验是同步本地命令，API 只在完整成功后返回；前端使用 180 秒超时并展示进行中状态。已实现：

| 方法与路径                          | 当前契约                                                                                                                  |
| ----------------------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| `GET /api/v1/backups`               | 列出内部备份根中的 UUID 包和上次成功校验事实；清单损坏项显示为 invalid，不暴露绝对路径                                    |
| `POST /api/v1/backups`              | 可选 `note`（最多 200 字）和 `Idempotency-Key`；冻结写入、创建、完整校验并原子发布 SQLite+Artifact 包                     |
| `POST /api/v1/backups/:id/verify`   | 对 canonical UUID 包重新校验 manifest、预期文件全集、hash/size、marker、数据库 quick/foreign-key/schema/identity/Artifact |
| `POST /api/v1/backups/:id/drill`    | 在隔离临时数据根复制、打开/迁移数据库并声明 Artifact store，验证可恢复性后清理临时数据；不替换当前数据                    |
| `POST /api/v1/backups/:id/restore`  | 严格确认后重验目标、创建当前状态回滚包并挂起计划；下一次 Sidecar 启动前原子替换并最终复验，失败恢复旧资源                 |
| `DELETE /api/v1/backups/:id`        | 要求 `confirm=true`；安全校验精确 UUID 目录，先原子移入隐藏删除态再清理并同步；损坏包可删，不安全文件系统项拒绝           |
| `GET /api/v1/exports/business-data` | 单事务生成 format v1 业务 JSON attachment；显式白名单、稳定结构，文件仅元数据，排除运行维护事实和机器私有信息             |

导入与含文件导出尚无路由。恢复安排同步返回 `202` 和目标/回滚 ID，实际应用发生在下一次启动，期间普通 v1 API 返回 `503 RESTORE_RESTART_REQUIRED`。数据量增长后再把同步 JSON 导出和其他长操作升级为 `queued / running / verifying / succeeded / failed / cancelled` 作业；迁移前需先写 ADR，不能把同步成功响应和后台状态混用。

当前备份事实只存在于备份包的 manifest，不写 `workflow_events`，避免把机器维护动作伪装成 Task/Project 业务事件。未来若增加诊断事件，最少包含 `backup_created / backup_verified / restore_started / restore_completed / restore_failed / export_completed / import_completed`，但不得记录业务正文、文件内容、凭据或机器绝对路径。

## 与其他模块协作

- [任务](tasks.md)：Submission/Artifact 元数据和受控 objects 必须作为一个恢复单元。
- [Actor](actors.md)：Actor/Assignment/Event 历史引用必须保留，不能只导出当前 Task。
- [桌面平台](desktop-platform.md)：负责 appData/appLog 定位、受管 Sidecar 安全退出和应用重启；浏览器开发模式保持外部 Sidecar 的人工生命周期。Sidecar 负责停写、SQLite 与 Artifact 一致性。
- [设置](settings.md)：当前发起手动创建、列出、重新校验、隔离演练、二次确认恢复、安全重启、永久删除和业务 JSON 导出；未来再接路径选择、导入和作业诊断。
- [客户](clients.md) / [财务与发票](finance-invoices.md)：文件业务实现后扩展备份清单。

## 验收状态

### 已实现

- [x] 开发/正式数据库和 Artifact root 隔离。
- [x] schema v12 嵌入迁移、回滚、外键恢复和测试；v11→v12 数据保留与 Inbox 约束已有定向覆盖。
- [x] schema v13 嵌入迁移、v12→v13 数据保留、关系约束、删除保护和外键恢复已由定向及全量 Go 测试覆盖。
- [x] schema v14 嵌入迁移、v13→v14 数据保留、Reminder 状态/投影引用/不可变/删除约束已由定向及全量 Go 测试覆盖。
- [x] schema v15 嵌入迁移、v14→v15 数据保留、自动结清索引/trigger 与非法 automatic 终态拒绝已由迁移测试覆盖。
- [x] schema v16 嵌入迁移、v15→v16 业务事实保留、空设置表及 Actor/key/硬删除约束已由迁移测试覆盖。
- [x] schema v17 嵌入迁移、v16→v17 业务事实保留、空保存视图表及 JSON/名称/schema/version 约束已由迁移测试覆盖。
- [x] 数据库绑定 JSON marker、进程级独占锁、受控相对路径、staging/objects/trash/quarantine 与 symlink/reparse 防护。
- [x] JSON/manifest/文件/总请求大小、SHA-256、180 秒服务端与 120 秒客户端传输边界、下载重新校验和 missing/mismatch 拒绝。
- [x] 关键文件/目录项耐久同步与未知受控候选隔离而非自动永久删除。
- [x] Artifact 软删除和 Task 硬删除的文件/数据库补偿。
- [x] 维护写锁覆盖普通 API、Focus heartbeat 和 Reminder 扫描，备份期间不会产生新的数据库/Artifact 写入。
- [x] 创建同时包含 SQLite `VACUUM INTO` 一致性快照、身份 marker 和全部 active file Artifact 的同卷 staging 包，并在校验后原子发布。
- [x] manifest 记录版本、身份、相对路径、size/SHA-256 与总量；临时数据库执行 quick/foreign-key/schema/identity/active Artifact 交叉校验，并拒绝缺失、篡改、额外文件和路径漂移。
- [x] 创建幂等重放、列表、显式重新校验，以及设置页加载/空/错误/成功状态已有 API、客户端和组件测试。
- [x] 恢复演练再次校验源包，在唯一临时数据根复制、打开/迁移数据库、声明 Artifact store、复验全部 active file Artifact 并清理临时数据；源备份和当前数据保持不变。
- [x] 恢复安排再次演练目标并创建完整自动回滚包，发布后冻结业务写入；同目标请求可安全重放，不同 pending 目标被拒绝。
- [x] 下一次 Sidecar 启动在打开 live 资源前准备和迁移副本，交换 SQLite/WAL/SHM 与完整 objects，最终验证失败恢复旧资源，成功以 applied 提交点防止重复应用。
- [x] 备份永久删除要求明确确认，支持有效/损坏 UUID 包，原子移入可续删隐藏态后清理和同步；拒绝 symlink/reparse、非普通文件及 pending 恢复期间删除。
- [x] 基础业务 JSON 在单事务内按显式白名单、稳定表/列/行结构生成；包含文件元数据但不含正文，排除令牌、绝对路径及运行维护表，失败不返回部分包。
- [x] 恢复挂起后桌面设置页可调用 `restart_application`；只在受管 Sidecar 真实退出后重启应用，浏览器/外部 Sidecar 明确降级为手动重启。

### 仍未实现

- [ ] 启动恢复进度页和 applied 清理警告诊断。
- [ ] 破坏性迁移前自动备份。
- [ ] 数据导入、含文件导出包、保留策略、计划备份和跨版本兼容矩阵。

## 相关代码/PRD链接

- [PRD 数据持久化](../opc-workspace-PRD.md)
- [SQLite 入口](../../services/sidecar/internal/database/database.go)
- [迁移器](../../services/sidecar/internal/database/migrate.go)
- [schema v9 迁移](../../services/sidecar/internal/database/migrations/009_task_submissions_artifacts.sql)
- [schema v10 Client 迁移](../../services/sidecar/internal/database/migrations/010_client_facts.sql)
- [schema v11 Focus 迁移](../../services/sidecar/internal/database/migrations/011_focus_sessions.sql)
- [schema v12 Inbox 迁移](../../services/sidecar/internal/database/migrations/012_inbox_items.sql)
- [schema v13 Inbox–Task 关系迁移](../../services/sidecar/internal/database/migrations/013_inbox_item_tasks.sql)
- [schema v14 Reminder 迁移](../../services/sidecar/internal/database/migrations/014_reminders.sql)
- [schema v15 Inbox 编排迁移](../../services/sidecar/internal/database/migrations/015_inbox_task_orchestration.sql)
- [受控 Artifact store](../../services/sidecar/internal/api/artifact_store.go)
- [备份 API 与校验器](../../services/sidecar/internal/api/backups.go)
- [备份 API 测试](../../services/sidecar/internal/api/backups_test.go)
- [隔离恢复演练](../../services/sidecar/internal/api/backup_drill.go)
- [重启前安全恢复](../../services/sidecar/internal/api/backup_restore.go)
- [确认删除](../../services/sidecar/internal/api/backup_delete.go)
- [业务 JSON 导出](../../services/sidecar/internal/api/business_export.go)
- [设置备份界面](../../apps/web/src/components/BackupSettings.tsx)
- [Task output API](../../services/sidecar/internal/api/task_outputs.go)
- [Tauri Sidecar 生命周期](../../apps/desktop/src-tauri/src/sidecar.rs)
- [统一开发脚本](../../scripts/dev.mjs)
- [迁移测试](../../services/sidecar/internal/database/task_artifacts_migration_test.go)
- [schema v13 迁移测试](../../services/sidecar/internal/database/inbox_task_migration_test.go)
- [schema v14 迁移测试](../../services/sidecar/internal/database/reminder_migration_test.go)
