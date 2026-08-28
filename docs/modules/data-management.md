# 数据管理、受控文件、备份与恢复模块

> 当前基线：app v0.1.0 / API v1 / SQLite schema v13（2026-08-28）
>
> 事实边界：SQLite 初始化/迁移、开发/正式数据隔离和 Task Artifact 受控文件目录已经实现；产品化备份、恢复、导入、导出、计划备份和跨版本恢复仍未实现。存在目录骨架不等于已有备份能力。

导航：[文档中心](../README.md) · [整体功能架构](../functional-architecture.md) · [PRD v2.5](../opc-workspace-PRD.md) · [任务](tasks.md) · [桌面平台](desktop-platform.md)

## 定位与边界

本模块负责本机数据的物理边界、迁移一致性和未来可恢复性：

- SQLite 业务事实与版本化 schema；
- Task Artifact 文件与数据库元数据的一致性；
- 开发数据、正式数据和测试数据隔离；
- 未来一致性备份、验证、恢复、导入导出和保留策略。

本模块不负责云同步、多设备合并、在线账号、远程文件服务、SQLCipher 或自动上传。v0.1 当前没有“备份成功”按钮或恢复 API，不能把数据库文件复制说明写成已交付功能。

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
- 开发数据库与 Artifact 位于 `.local/dev-data/`；桌面正式数据位于 Tauri `appDataDir`，互不复用。
- Tauri 创建 `appDataDir/artifacts/`，通过 `OPC_ARTIFACT_DIR` 交给 Sidecar；开发脚本使用 `--artifacts .local/dev-data/artifacts`。
- Sidecar 声明并校验 Artifact root，管理含 `format_version / database_id / store_id` 的 JSON marker、进程级独占锁、staging、objects、trash 与 quarantine；拒绝卷根、符号链接/reparse point、非空无 marker、marker 不规范，或数据库 ID / store ID 任一不匹配的目录。同一 root 已被另一个 Sidecar 持有时拒绝启动。
- 文件上传流式计算 MIME、size、SHA-256，先写 staging，再无覆盖提升到 objects；数据库只保存 `objects/<artifact-id>` 相对路径。marker、文件数据、对象提升、移动/删除与关键目录项做耐久同步。
- 启动时清理普通 staging 残留、协调中断的已知 trash move，并用 tombstone 区分授权删除与未知候选。active trash 恢复前必须匹配数据库 size/SHA-256；错配移入 quarantine 并记录 mismatch。无引用且没有可验证删除授权的受控 UUID object/trash 候选同样只进 quarantine，不自动永久删除，也不递归跟随异常目录或链接。
- Artifact 软删除和 Task 聚合硬删除使用 trash 做文件/数据库补偿：事务失败恢复，提交后清除临时 trash 文件；物理 object 已缺失时仍允许确认软删或聚合硬删，软删同时记录 missing 完整性事实。
- 下载前重新验证 size 与 SHA-256；missing/mismatch 状态写回数据库并拒绝输出内容。

### 未实现

- SQLite Online Backup API 或等价一致性快照；
- 包含数据库与 Artifact objects 的原子备份包；
- backup manifest、校验、临时恢复演练与原子替换；
- 设置页中的手动备份/恢复、导入/导出进度和诊断；
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
  backups/                     # 预留；备份功能尚未实现
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

当前 schema v13：

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

新增 schema 只能从 `014_*` 继续追加，不修改已发布迁移。迁移测试必须覆盖：真实旧版本数据保留、幂等重跑、约束/索引/trigger/外键、`foreign_key_check`、故障回滚以及外键状态恢复。

## 未来 v0.1 备份/恢复目标

### 一致性备份

备份必须同时包含：

- SQLite 一致性快照，而不是仅复制可能仍有 WAL 的主文件；
- 所有 active file Artifact objects；
- manifest 与每个文件的 size/SHA-256；
- app/API/schema/export format 版本；
- 创建时间、平台与可选说明。

备份进行时需要定义写入冻结或快照边界，避免数据库引用与 Artifact objects 跨时点不一致。`attachments/`、`invoices/` 在业务实现后也必须进入同一清单。

### 验证与恢复

目标流程：

1. 在临时目录展开备份，验证 manifest、文件 size/hash，并校验 Artifact marker 的 format/database/store ID 与恢复数据库的 `workspace_identity` 一致。
2. 打开临时数据库，运行 `quick_check`/`foreign_key_check` 与 schema 兼容检查。
3. 校验每个 active file Artifact 都有对应 object，且没有路径越界。
4. 停止业务写入，备份当前正式数据以便回滚。
5. 使用同卷临时路径原子替换数据库和受控文件根；任一步失败恢复原数据。
6. 重启 Sidecar 并完成 health/schema/Artifact reconciliation；成功后再清理旧副本。

恢复绝不能直接覆盖正在打开的 SQLite 文件或部分覆盖 `objects/`。

### 导出/导入

基础导出目标是版本化 JSON 业务包和可选 Artifact 文件，不包含会话令牌、绝对路径或未来 Agent secret。导入必须预览冲突、支持取消、使用新 UUID 或显式映射，并在一个可回滚作业中完成。当前没有冻结 API 或 UI。

### 计划备份（v0.3）

后续再评审定时计划、保留数量、磁盘空间阈值、增量方式、加密和可选外部目标。不得在 v0.1 悄悄启用后台上传或付费云资源。

## 目标状态与 API（未实现）

建议作业状态：`queued / running / verifying / succeeded / failed / cancelled`。建议命令/API 在实现前另写 ADR，至少覆盖：创建备份、列出备份、验证、恢复、导出、导入预览/执行、取消和删除。PRD 出现这些名称不代表当前路由存在。

未来事件应最少包含 `backup_created / backup_verified / restore_started / restore_completed / restore_failed / export_completed / import_completed`，但不得记录业务正文、文件内容、凭据或机器绝对路径。

## 与其他模块协作

- [任务](tasks.md)：Submission/Artifact 元数据和受控 objects 必须作为一个恢复单元。
- [Actor](actors.md)：Actor/Assignment/Event 历史引用必须保留，不能只导出当前 Task。
- [桌面平台](desktop-platform.md)：负责 appData/appLog 定位、停写协调和重启；Sidecar 负责 SQLite 与 Artifact 一致性。
- [设置](settings.md)：未来展示备份作业、路径选择和错误；当前没有真实入口。
- [客户](clients.md) / [财务与发票](finance-invoices.md)：文件业务实现后扩展备份清单。

## 验收状态

### 已实现

- [x] 开发/正式数据库和 Artifact root 隔离。
- [x] schema v12 嵌入迁移、回滚、外键恢复和测试；v11→v12 数据保留与 Inbox 约束已有定向覆盖。
- [x] schema v13 嵌入迁移、v12→v13 数据保留、关系约束、删除保护和外键恢复已由定向及全量 Go 测试覆盖。
- [x] 数据库绑定 JSON marker、进程级独占锁、受控相对路径、staging/objects/trash/quarantine 与 symlink/reparse 防护。
- [x] JSON/manifest/文件/总请求大小、SHA-256、180 秒服务端与 120 秒客户端传输边界、下载重新校验和 missing/mismatch 拒绝。
- [x] 关键文件/目录项耐久同步与未知受控候选隔离而非自动永久删除。
- [x] Artifact 软删除和 Task 硬删除的文件/数据库补偿。

### 未实现

- [ ] 创建一个同时包含数据库与 Artifact 的一致性备份包。
- [ ] 在临时数据库和目录完整验证备份。
- [ ] 原子恢复及失败回滚。
- [ ] 破坏性迁移前自动备份。
- [ ] 导入/导出、保留策略、计划备份和跨版本兼容矩阵。

## 相关代码/PRD链接

- [PRD 数据持久化](../opc-workspace-PRD.md)
- [SQLite 入口](../../services/sidecar/internal/database/database.go)
- [迁移器](../../services/sidecar/internal/database/migrate.go)
- [schema v9 迁移](../../services/sidecar/internal/database/migrations/009_task_submissions_artifacts.sql)
- [schema v10 Client 迁移](../../services/sidecar/internal/database/migrations/010_client_facts.sql)
- [schema v11 Focus 迁移](../../services/sidecar/internal/database/migrations/011_focus_sessions.sql)
- [schema v12 Inbox 迁移](../../services/sidecar/internal/database/migrations/012_inbox_items.sql)
- [schema v13 Inbox–Task 关系迁移](../../services/sidecar/internal/database/migrations/013_inbox_item_tasks.sql)
- [受控 Artifact store](../../services/sidecar/internal/api/artifact_store.go)
- [Task output API](../../services/sidecar/internal/api/task_outputs.go)
- [Tauri Sidecar 生命周期](../../apps/desktop/src-tauri/src/sidecar.rs)
- [统一开发脚本](../../scripts/dev.mjs)
- [迁移测试](../../services/sidecar/internal/database/task_artifacts_migration_test.go)
- [schema v13 迁移测试](../../services/sidecar/internal/database/inbox_task_migration_test.go)
