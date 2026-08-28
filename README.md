# opc-workspace

opc-workspace 是面向一人公司的本地优先桌面工作台。本仓库当前提供 v0.1 的可运行基座：Tauri 2 桌面壳、React/TypeScript/Vite 界面、Go Sidecar、SQLite 版本化迁移，以及沿用历史 Linear 深色设计方向的页面框架。

> 当前版本不包含 AI、LLM、智能排程、自然语言解析或自动生成报告；本地开发与桌面运行时均不使用 Docker。

## 当前完成范围

- Tauri 2 桌面窗口、单实例保护、应用数据目录初始化和 Go Sidecar 生命周期基础
- 生产 Sidecar 动态端口握手、启动期随机会话令牌、健康检查、退出 drain/checkpoint 与兜底清理；shutdown 已持有子进程时，ready 超时不会伪造 exited 状态或抢走清理职责
- Go `/health` 与版本化 `/api/v1`，统一请求 ID、错误响应、Bearer 鉴权和 Origin 白名单
- SQLite schema v10、WAL、外键、busy timeout 和嵌入式版本化迁移；v3–v8 依次增加项目生命周期、幂等快照、聚合版本、Task 事实、Actor/Assignment/Event 与六状态生命周期，v9 增加带不可变数据库 ID/一次性 store 绑定的 workspace identity、Submission、Artifact、当前提交引用和事件关联保护，v10 增加 Client 聚合版本、查询索引和 Project 关联版本触发器
- 任务完整事实与受控生命周期纵切：快照式幂等新建、详情、`If-Match` 非状态编辑/删除、项目与父子关系、标签、完成标准、服务端分页/搜索/六状态筛选/稳定排序、原子批量操作、计划日期组手动排序，以及开始/阻塞/解除阻塞/完成/取消/重新打开六个显式命令
- 标签分页/搜索/排序、幂等新建、并发安全编辑和确认删除；标签嵌入或父子聚合变化会递增受影响任务版本
- 项目 CRUD、服务端分页/搜索/状态筛选、快照式创建幂等、覆盖聚合事实的 `If-Match` 乐观锁、受控状态流转、归档/恢复和确认后硬删除；项目卡片与详情从关联任务派生进度和 `actual_minutes`
- 客户基础资料 CRUD、服务端分页/搜索/状态筛选/稳定排序、快照式创建幂等、`ETag`/`If-Match` 乐观锁和受约束硬删除；客户列表/基础详情与 Project 客户选择、改绑、解除及筛选已接真实 API
- Actor 管理纵切：schema v7 固定创建唯一 owner/system，幂等回填历史任务的 owner Assignment 与迁移事件；`/api/v1/actors` 提供分页筛选、person 幂等新建、详情和 `If-Match` 编辑/停用，设置页“人员与责任”接入真实本地数据
- Assignment 责任纵切：任务详情可查询当前负责人/审核人和分页历史，完成首次分派、改派与结束；命令以 Task `If-Match`/`version` 拒绝旧写入，支持可选幂等快照，并与 Assignment Workflow Event 在同一事务提交
- 任务活动时间线：详情按需分页读取 Task 聚合的生命周期、分派和迁移事件；同一命令内通过 `command_seq` 稳定展示自动结束分派与最终状态事件
- T-18D D2 产出验收纵切：新建任务和符合条件的任务编辑可选择 `review_policy = manual`；任务详情支持摘要及文本、链接、结构化 JSON、文件混合提交，owner 接受或要求返工，并分页查看 Submission/Artifact 历史
- 受控 Artifact 文件存储：Sidecar 以进程级独占锁管理 `artifacts/`；JSON marker 携带 `format_version / database_id / store_id`，schema v9 用不可变数据库身份和一次性 `artifact_store_id` 建立双向绑定，并使用 `.staging/`、`objects/`、`.trash/` 和 `.quarantine/`；校验文件大小与 SHA-256，关键文件/目录项做耐久同步。提交事务报错只清除数据库可证明无引用的 object，模糊 COMMIT 留给 reconcile；软删除与 Task 聚合硬删除通过 immutable tombstone 协调数据库和文件事务补偿
- React 三栏应用框架、今日/任务/项目/客户/专注基础页面；收入、发票、收件箱目前只有路由与页面骨架，路线图和内容日历为后续版本占位页
- `Ctrl/Cmd + K` 命令面板、`Ctrl/Cmd + N` 新建任务入口，以及加载、错误、重试和空状态
- 全局专注/休息计时状态机，以及可持久化的个人资料、默认首页、右侧概览开关、亮/暗主题、减少动效和专注参数设置

受控任务生命周期 D1、T-18D D2（manual policy、Submission、Artifact、提交、验收、返工和受控文件），以及客户基础资料 CRUD/基础详情/Project 客户关联已经交付。客户活动与附件、Actor 显式关联、项目事件/Inbox 集成、专注会话持久化、备份/恢复、系统托盘、全局系统快捷键、签名离线更新和三平台安装包仍属于后续实现；在线 Updater 不在当前阶段。[PRD v2.2](docs/opc-workspace-PRD.md) 记录了这条边界。第一阶段不引入多人登录、云同步、远程通知或线上 Agent；`person` 仅作本机责任记录，不会收到任务或获得访问权限。Artifact 的 `produced_by` 来自提交瞬间的活动 assignee，`recorded_by`、Submission 的 `submitted_by` 以及审核/撤回/删除操作人均为内置 owner，表达“负责人产出、我代录/验收”，不代表远程协作。客户回访、收入/支出、发票 CRUD/PDF、AI 助手和本地知识库已明确归入更后续版本。开发数据库默认从空业务数据开始；任务页、项目页和客户页在 Sidecar 可用时读取真实 SQLite 数据。项目工时目前只聚合任务表已有的 `actual_minutes`，专注计时尚未写入该字段。

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
docs/                     PRD、整体功能架构和各模块功能文档
.local/dev-data/          开发数据库（已忽略）
```

## 产品文档

- [文档索引](docs/README.md)
- [产品需求文档（PRD v2.2）](docs/opc-workspace-PRD.md)
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

开发数据库固定保存到 `.local/dev-data/opc-workspace.db`，开发 Artifact 固定保存到 `.local/dev-data/artifacts/`，并与正式数据完全隔离。统一开发脚本默认不写入 demo 数据；从旧版本升级时，迁移只会清理先前 demo seed 使用的固定记录。开发令牌只用于本机联调，不得用于生产构建。

## 检查与测试

```powershell
pnpm typecheck
pnpm test:go
pnpm --filter @opc/web test
pnpm build:web
pnpm check:tauri
```

也可在工具链完整时运行聚合检查：

```powershell
pnpm check
```

## 构建

先为当前 Rust target triple 生成 Go Sidecar：

```powershell
pnpm build:sidecar
```

脚本读取 `rustc --print host-tuple`，生成类似以下文件：

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
  opc-workspace.db
  attachments/
  artifacts/
    .opc-artifact-store-v1
    .opc-artifact-store.lock
    .staging/
    objects/
    .trash/
    .quarantine/
  invoices/
  backups/
  config/

appLogDir/
  opc-workspace.log          # 日志落盘管线后续接入
```

具体物理路径由操作系统和应用标识 `com.opcworkspace.desktop` 决定，业务代码不硬编码该路径。升级应用程序文件不会覆盖这里的数据。

## 本地 API 约定

- Sidecar 仅监听 `127.0.0.1`；开发默认固定端口，桌面生产运行使用端口 `0` 获取随机空闲端口。
- 生产请求（包括 `/health`）必须携带 `Authorization: Bearer <session-token>`。
- Tauri 通过环境变量把数据库路径、日志目录、端口和令牌交给 Sidecar，令牌不出现在命令行。
- Tauri 通过 `OPC_ARTIFACT_DIR` 把 `appDataDir/artifacts/` 交给 Sidecar；开发脚本等价地使用 `--artifacts .local/dev-data/artifacts`。
- 业务接口统一位于 `/api/v1`；错误格式为 `{ "code", "message", "request_id" }`。
- API 时间使用 RFC 3339 UTC，纯日期使用 `YYYY-MM-DD`，金额使用最小货币单位整数。

当前可用端点：

```text
GET    /health
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
PATCH  /api/v1/projects/:id
POST   /api/v1/projects/:id/transitions
DELETE /api/v1/projects/:id?confirm=true
GET    /api/v1/clients
POST   /api/v1/clients
GET    /api/v1/clients/:id
PATCH  /api/v1/clients/:id
DELETE /api/v1/clients/:id?confirm=true
GET    /api/v1/stats/today?date=YYYY-MM-DD
```

Actor 详情、创建和更新返回 `ETag`；更新必须携带 `If-Match`。Actor 新建只接受 `person`，可选 `Idempotency-Key` 会保存规范化请求与首次 `201` 快照；owner 只允许修改展示名称，system 不可编辑，存在活动 Assignment 的 person 不能停用。当前没有 Actor 删除路由。Assignment 查询返回当前 assignee/reviewer、分页结束历史和 Task `ETag`；创建、改派和结束必须携带 Task `If-Match`，成功递增 Task 版本，并可用 `Idempotency-Key` 重放首次响应快照而不重复写事件。v0.1 assignee 仅允许 active owner/person，reviewer 仅允许 active owner。

Task 只能以 `todo` 新建；创建和非生命周期 PATCH 接受 `review_policy = none | manual`，但策略变更只允许在 `todo` 且从未产生 Submission 时执行。非生命周期 PATCH 不能写状态，旧 `/tasks/:id/status` 固定返回 `410 TASK_STATUS_ENDPOINT_DEPRECATED`。六个生命周期命令必须携带 Task `If-Match`，可选 `Idempotency-Key`；开始前必须有 active assignee，阻塞和取消必须填写原因，解除阻塞只恢复服务端保存的来源状态，直接完成只允许 `review_policy = none`。完成或取消会在同一事务结束全部活动 Assignment 并只递增一次 Task 版本；等待验收时取消还会把当前 Submission 标为 `withdrawn`。重新打开回到 `todo`、清空 `current_submission_id`，但保留提交历史且不恢复旧分派。

manual Task 只能从 `todo / in_progress` 提交，提交前必须同时具备 active assignee 与 active owner reviewer。JSON 可提交文本、HTTP(S) 链接或 JSON object；包含文件时使用 multipart，唯一 `manifest` 必须是首个 part，之后只允许它通过唯一 `file_field` 精确引用的文件 part。摘要最长 10,000 字符；每批最多 20 个 Artifact；名称 1–255 个安全字符；文本最多 500,000 字符；链接最多 4,096 bytes 且禁止 URL credentials；严格 JSON body、multipart `manifest` 和单个 structured object 各不超过 1 MiB；单文件非空且最多 50 MiB，完整 multipart 请求最多 100 MiB。Sidecar HTTP read/write timeout 为 180 秒，前端上传与下载使用 120 秒端到端超时。提交进入 `waiting_review`；owner 接受后进入 `done` 并结束活动 Assignment，要求返工时必须填写不超过 1,000 字符的原因并回到 `in_progress`。`current_submission_id` 在接受、返工和取消后用于指向最近一批，只有 reopen 清空。

Submission/Artifact 列表默认每页 50、最大 100，并返回 Task `ETag` 和 `meta.task_version`；Artifact 列表可按 `submission_id` 过滤，并用 `include_deleted=true` 查看软删除记录。所有 Artifact 摘要/详情都携带由父 Submission 派生的必填 `submission_status`。详情只在按需读取时返回文本、链接或结构化正文，删除后正文固定隐藏。文件下载只由鉴权 content 端点提供，响应强制 attachment、`nosniff`、`no-store`，并以 SHA-256 作为 `ETag`；缺失或校验不符会更新 `integrity_status` 并拒绝下载。Artifact 删除要求 `confirm=true`、Task `If-Match`、1–1,000 字符原因，pending-review 批次禁止删除；schema v9 在同一事务写入不可变 `artifact_deletion_tombstones`，文件经受控 trash 完成数据库/文件补偿后物理清理。tombstone 在 Task 聚合删除后仍保留，供启动恢复区分授权删除与未知候选；恢复 active trash 前复验 size/SHA-256，错配项进入 quarantine 并标记 mismatch。物理文件已经缺失时，确认软删除仍会成功并记录 `integrity_status = missing`；Task 聚合硬删除也不会被缺失 object 阻断。Task 硬删除随后由外键级联 Submission/Artifact；Workflow Event 中已失效的关联 ID 可置空，JSON 快照仍保留。

任务资料、Task output/review/Artifact 删除及 Assignment 命令均使用 Task 版本拒绝旧写入；可重试命令可携带稳定 `Idempotency-Key`，同键同请求重放首次响应，同键不同请求返回冲突。前端遇到版本冲突会刷新最新 Task，同时保留摘要、文本、链接、结构化 JSON 与浏览器 `File` 对象草稿，要求用户再次明确提交。事件查询默认每页 50、最大 100，返回 Task `ETag`、`meta.task_version` 和按时间/命令顺序倒序的追加式事件。Assignment 没有 DELETE 路由。批量任务和计划组排序在请求体中携带每项 `expected_version` 并整批校验。项目修改、状态流转和硬删除也必须携带 `If-Match`；任务/发票/客户聚合事实变化会递增项目版本。永久删除只允许已归档项目，并会按外键策略解除任务和发票关联而不删除这些业务记录。归档项目资料必须先恢复再编辑，也不接受新的任务关联；其既有关联任务仍可编辑。项目列表默认排除归档项；只有需要读取完整关联历史的页面才显式传 `include_archived=true`。

Client 列表默认每页 50、最大 100，支持 `q`、`status` 和白名单 `sort`，所有排序追加 `id ASC`；响应实时返回 `project_count`。名称和可选联系人、邮箱、电话、备注由服务端 trim、限长并校验，可选空白保存为 `null`，状态只接受 `active / lead / inactive`。创建可使用 `Idempotency-Key` 重放首次 `201` 快照；创建、详情和更新返回 Client `ETag`，PATCH/DELETE 必须携带 `If-Match`。永久删除还要求 `confirm=true` 且 Client 已停用；Invoice 强引用返回可解释冲突且不改变事实，Project 可选外键置空并返回 `detached_projects`。Project 关联变化会使 Client 聚合版本失效，Client 名称变化继续使关联 Project 版本失效。

## SQLite 与迁移

迁移 SQL 位于 `services/sidecar/internal/database/migrations/`，随 Sidecar 二进制嵌入。当前最新版本为 schema v10；启动时按文件版本顺序执行，并记录到 `schema_migrations`。schema v6 为任务增加类型、父任务、完成标准与版本，schema v7 新增 `actors`、`task_assignments` 和 `workflow_events` 并回填历史 owner 分派，schema v8 安全重建六状态 Task 并增加生命周期字段和事件保护。schema v9 新增单例 `workspace_identity`、`task_submissions`、四种互斥 payload 的 `task_artifacts`、不可变 `artifact_deletion_tombstones`、Task `current_submission_id` 以及 Workflow Event 的 `submission_id / artifact_id`；不可变 `database_id` 与首次声明后不可更换的 `artifact_store_id` 将数据库和 Artifact marker 双向绑定。schema v10 为 `clients` 增加 `version` 与查询索引，将历史空白可选值归一为 `NULL`，并用触发器在 Project 关联变化时递增受影响 Client 的聚合版本。需要临时关闭外键的迁移由迁移器锁定单连接，在事务提交前执行 `foreign_key_check`，成功或失败都恢复外键；一致性失败会整体回滚。每个连接启用：

- `PRAGMA foreign_keys = ON`
- `PRAGMA journal_mode = WAL`
- `PRAGMA busy_timeout = 5000`

新增迁移时只添加新的递增版本文件，不修改已发布迁移。破坏性迁移前的一致性自动备份与完整恢复流程尚未在本基座实现。

## 产品边界

[PRD v2.2](docs/opc-workspace-PRD.md) 是范围、目标契约与当前实施状态依据。v0.1 基座明确不实现任务/项目看板、内容日历业务功能、客户回访、收入/支出/发票业务、自动化规则引擎、白噪音、网站屏蔽、SQLCipher、多币种、移动端、云同步、AI 助手或知识库。本地 Actor 身份、person 管理、Assignment、六状态生命周期、Task 时间线、manual Submission/Artifact 验收，以及 Client 基础资料 CRUD/Project 客户关联已经交付；客户活动/附件/Actor 显式关联、新版收件箱、提醒编排、Agent Runtime、备份/恢复和其他业务模块仍未交付。对应页面骨架、数据表或 API 规划不代表完整工作流已经可用。
