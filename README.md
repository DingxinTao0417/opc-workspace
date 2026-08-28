# opc-workspace

opc-workspace 是面向一人公司的本地优先桌面工作台。本仓库当前提供 v0.1 的可运行基座：Tauri 2 桌面壳、React/TypeScript/Vite 界面、Go Sidecar、SQLite 版本化迁移，以及沿用历史 Linear 深色设计方向的页面框架。

> 当前版本不包含 AI、LLM、智能排程、自然语言解析或自动生成报告；本地开发与桌面运行时均不使用 Docker。

## 当前完成范围

- Tauri 2 桌面窗口、单实例保护、应用数据目录初始化和 Go Sidecar 生命周期基础
- 生产 Sidecar 动态端口握手、启动期随机会话令牌、健康检查、退出 drain/checkpoint 与兜底清理
- Go `/health` 与版本化 `/api/v1`，统一请求 ID、错误响应、Bearer 鉴权和 Origin 白名单
- SQLite schema v7、WAL、外键、busy timeout 和嵌入式版本化迁移；v3 增加项目生命周期字段，v4 增加幂等快照，v5 让项目聚合变化原子递增版本，v6 增加任务类型、父子关系、完成标准及 Task/Tag 版本，v7 增加本地 Actor、Assignment 基础和 Workflow Event 审计
- 任务完整事实纵切：快照式幂等新建、详情、`If-Match` 非状态编辑/三态状态/删除、项目与父子关系、标签、完成标准、服务端分页/搜索/筛选/稳定排序、原子批量操作和计划日期组手动排序
- 标签分页/搜索/排序、幂等新建、并发安全编辑和确认删除；标签嵌入或父子聚合变化会递增受影响任务版本
- 项目 CRUD、服务端分页/搜索/状态筛选、快照式创建幂等、覆盖聚合事实的 `If-Match` 乐观锁、受控状态流转、归档/恢复和确认后硬删除；项目卡片与详情从关联任务派生进度和 `actual_minutes`
- Actor 管理纵切：schema v7 固定创建唯一 owner/system，幂等回填历史任务的 owner Assignment 与迁移事件；`/api/v1/actors` 提供分页筛选、person 幂等新建、详情和 `If-Match` 编辑/停用，设置页“人员与责任”接入真实本地数据
- Assignment 责任纵切：任务详情可查询当前负责人/审核人和分页历史，完成首次分派、改派与结束；命令以 Task `If-Match`/`version` 拒绝旧写入，支持可选幂等快照，并与 Assignment Workflow Event 在同一事务提交
- React 三栏应用框架、今日/任务/项目/专注基础页面；客户、收入、发票、收件箱目前只有路由与页面骨架，路线图和内容日历为后续版本占位页
- `Ctrl/Cmd + K` 命令面板、`Ctrl/Cmd + N` 新建任务入口，以及加载、错误、重试和空状态
- 全局专注/休息计时状态机，以及可持久化的个人资料、默认首页、右侧概览开关、亮/暗主题、减少动效和专注参数设置

受控任务生命周期（阻塞/待验收/取消）、基础客户 CRUD、项目客户选择、Artifact、项目事件/Inbox 集成、专注会话持久化、备份/恢复、系统托盘、全局系统快捷键、签名离线更新和三平台安装包仍属于后续实现；在线 Updater 不在当前阶段。[PRD v1.9](docs/opc-workspace-PRD.md) 已把 Actor 身份、person 管理和任务 Assignment API/UI 标为已交付，把验收、收件箱编排和本地 Agent 生命周期继续列为后续纵切。第一阶段不引入多人登录、云同步、远程通知或线上 Agent；`person` 仅作本机责任记录，不会收到任务或获得访问权限。客户回访、收入/支出、发票 CRUD/PDF、AI 助手和本地知识库已明确归入更后续版本。开发数据库默认从空业务数据开始；任务页和项目页在 Sidecar 可用时读取真实 SQLite 数据。项目工时目前只聚合任务表已有的 `actual_minutes`，专注计时尚未写入该字段。

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
- [产品需求文档（PRD v1.9）](docs/opc-workspace-PRD.md)
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

开发数据库固定保存到 `.local/dev-data/opc-workspace.db`，并与正式数据完全隔离。统一开发脚本默认不写入 demo 数据；从旧版本升级时，迁移只会清理先前 demo seed 使用的固定记录。开发令牌只用于本机联调，不得用于生产构建。

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
PATCH  /api/v1/tasks/:id/status
GET    /api/v1/tasks/:id/assignments
POST   /api/v1/tasks/:id/assignments
POST   /api/v1/tasks/:id/reassign
POST   /api/v1/assignments/:id/end
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
GET    /api/v1/stats/today?date=YYYY-MM-DD
```

Actor 详情、创建和更新返回 `ETag`；更新必须携带 `If-Match`。Actor 新建只接受 `person`，可选 `Idempotency-Key` 会保存规范化请求与首次 `201` 快照；owner 只允许修改展示名称，system 不可编辑，存在活动 Assignment 的 person 不能停用。当前没有 Actor 删除路由。Assignment 查询返回当前 assignee/reviewer、分页结束历史和 Task `ETag`；创建、改派和结束必须携带 Task `If-Match`，成功递增 Task 版本，并可用 `Idempotency-Key` 重放首次响应快照而不重复写事件。v0.1 assignee 仅允许 active owner/person，reviewer 仅允许 active owner；完成 Task 会在同一事务结束全部活动 Assignment，重新打开不会恢复旧分派。Assignment 没有 DELETE 路由；永久删除 Task 会级联删除其 Assignment，并保留已脱离 Assignment 外键的 Workflow Event JSON 快照。任务资料、三态状态、删除及标签编辑/删除同样必须携带 `If-Match` 版本；批量任务和计划组排序在请求体中携带每项 `expected_version` 并整批校验。项目修改、状态流转和硬删除也必须携带 `If-Match`；任务/发票/客户聚合事实变化会递增项目版本。永久删除只允许已归档项目，并会按外键策略解除任务和发票关联而不删除这些业务记录。归档项目资料必须先恢复再编辑，也不接受新的任务关联；其既有关联任务仍可编辑。

## SQLite 与迁移

迁移 SQL 位于 `services/sidecar/internal/database/migrations/`，随 Sidecar 二进制嵌入。当前最新版本为 schema v7；启动时按文件版本顺序执行，并记录到 `schema_migrations`。schema v6 为任务增加类型、父任务、完成标准与版本，为标签增加版本，并建立父子循环保护、聚合版本失效及排序/标签索引。schema v7 新增 `actors`、`task_assignments` 和 `workflow_events`，以固定 UUID 幂等创建 owner/system，并为历史任务回填可审计的 owner 分派；未完成任务保留活动分派，已完成任务使用 `completed_at`、缺失时回退 `updated_at` 作为结束时间。每个连接启用：

- `PRAGMA foreign_keys = ON`
- `PRAGMA journal_mode = WAL`
- `PRAGMA busy_timeout = 5000`

新增迁移时只添加新的递增版本文件，不修改已发布迁移。破坏性迁移前的一致性自动备份与完整恢复流程尚未在本基座实现。

## 产品边界

[PRD v1.9](docs/opc-workspace-PRD.md) 是范围、目标契约与当前实施状态依据。v0.1 基座明确不实现任务/项目看板、内容日历业务功能、客户回访、收入/支出/发票业务、自动化规则引擎、白噪音、网站屏蔽、SQLCipher、多币种、移动端、云同步、AI 助手或知识库。本地 Actor 身份、person 管理、Assignment 操作与责任事件已经交付；受控验收、新版收件箱和 Agent 仍未交付。对应数据库基础或 API 规划不代表完整工作流已经可用。
