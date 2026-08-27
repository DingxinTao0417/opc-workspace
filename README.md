# opc-workspace

opc-workspace 是面向一人公司的本地优先桌面工作台。本仓库当前提供 v0.1 的可运行基座：Tauri 2 桌面壳、React/TypeScript/Vite 界面、Go Sidecar、SQLite 版本化迁移，以及与现有 Linear 深色原型一致的页面框架。

> 当前版本不包含 AI、LLM、智能排程、自然语言解析或自动生成报告；本地开发与桌面运行时均不使用 Docker。

## 当前完成范围

- Tauri 2 桌面窗口、单实例保护、应用数据目录初始化和 Go Sidecar 生命周期基础
- 生产 Sidecar 动态端口握手、启动期随机会话令牌、健康检查、退出 drain/checkpoint 与兜底清理
- Go `/health` 与版本化 `/api/v1`，统一请求 ID、错误响应、Bearer 鉴权和 Origin 白名单
- SQLite WAL、外键、busy timeout 和嵌入式版本化迁移
- 任务列表、新建、读取、状态更新、删除及今日统计 API
- React 三栏应用框架、今日工作台金样、任务/项目/客户/收入/发票/收件箱/专注页面和后续版本占位页
- `Ctrl/Cmd + K` 命令面板、`Ctrl/Cmd + N` 新建任务入口，以及加载、错误、重试和空状态
- 全局专注/休息计时状态机，以及可持久化专注时长、休息时长、循环、自动开始和提示音的专注设置

项目和基础客户的完整 CRUD、专注会话持久化、备份/恢复、系统托盘、全局系统快捷键、自动更新和三平台签名安装包仍属于后续实现。客户回访、收入/支出、发票 CRUD/PDF、AI 助手和本地知识库已明确归入更后续版本。开发数据库默认从空业务数据开始；任务页在 Sidecar 可用时优先读取真实 SQLite 数据。

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
pages/                    原始 HTML 设计参考（保持不改）
.local/dev-data/          开发数据库（已忽略）
```

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
GET    /api/v1/tasks
POST   /api/v1/tasks
GET    /api/v1/tasks/:id
PATCH  /api/v1/tasks/:id
PATCH  /api/v1/tasks/:id/status
DELETE /api/v1/tasks/:id
GET    /api/v1/stats/today?date=YYYY-MM-DD
```

## SQLite 与迁移

迁移 SQL 位于 `services/sidecar/internal/database/migrations/`，随 Sidecar 二进制嵌入。启动时按文件版本顺序执行，并记录到 `schema_migrations`。每个连接启用：

- `PRAGMA foreign_keys = ON`
- `PRAGMA journal_mode = WAL`
- `PRAGMA busy_timeout = 5000`

新增迁移时只添加新的递增版本文件，不修改已发布迁移。破坏性迁移前的一致性自动备份与完整恢复流程尚未在本基座实现。

## 产品边界

PRD v1.5 是范围与当前实施状态依据。v0.1 基座明确不实现任务/项目看板、内容日历业务功能、客户回访、收入/支出/发票业务、自动化规则引擎、白噪音、网站屏蔽、SQLCipher、多币种、移动端、云同步、AI 助手或知识库。这些能力已登记为后续工作包；对应导航或规划描述不代表功能已经交付。
