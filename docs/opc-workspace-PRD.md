# opc-workspace 产品需求文档 (PRD)

> **一人公司操作系统** · PRD v1.6
> 产品阶段：0 → 1 可运行基座（app v0.1.0）/ MVP 持续迭代
> 目标用户：独立创业者 / 自由职业者 / 一人公司经营者
> 技术架构：Tauri 2.0 + React + Go Sidecar + SQLite
> 文档日期：2026-08-27
> 实现基线：app v0.1.0 / API v1 / SQLite schema v2

> **v1.6 更新说明**：将收件箱从被动通知列表升级为本地工作受理与编排中心，引入 `owner / person / agent / system` Actor、任务拆分与分派、产出、验收、返工和审计规划；明确第一阶段不引入线上服务、多人登录、云同步、远程通知或远程 Agent，并补充各模块的具体实施顺序与验收条件。当前实现基线仍为 app v0.1.0 / API v1 / SQLite schema v2，新增内容均为规划，不代表代码已交付。

> 文档导航：[文档中心](README.md) · [整体功能架构](functional-architecture.md) · [模块文档](modules/README.md)

---

## 目录

1. [需求背景](#1-需求背景)
2. [产品目标](#2-产品目标)
3. [用户画像与核心场景](#3-用户画像与核心场景)
4. [技术架构方案](#4-技术架构方案)
5. [功能模块详情](#5-功能模块详情)
   - 5.1 [今日工作台](#51-今日工作台)
   - 5.2 [任务管理](#52-任务管理)
   - 5.3 [项目管理](#53-项目管理)
   - 5.4 [客户管理](#54-客户管理)
   - 5.5 [收入、支出与发票（后续版本）](#55-收入支出与发票后续版本)
   - 5.6 [收件箱与本地工作编排中心](#56-收件箱与本地工作编排中心)
   - 5.7 [专注模式](#57-专注模式)
   - 5.8 [路线图与内容日历](#58-路线图与内容日历)
   - 5.9 [全局功能](#59-全局功能)
   - 5.10 [AI 助手（待开发）](#510-ai-助手待开发)
   - 5.11 [本地知识库（待开发）](#511-本地知识库待开发)
6. [数据模型设计](#6-数据模型设计)
7. [数据持久化方案](#7-数据持久化方案)
8. [部署与分发](#8-部署与分发)
9. [MVP 范围与迭代计划](#9-mvp-范围与迭代计划)
10. [实施基线、开发流程与实现追踪](#10-实施基线开发流程与实现追踪)
    - 10.7 [各模块具体实施计划](#107-各模块具体实施计划)

---

## 1. 需求背景

### 1.1 问题陈述

独立创业者、自由职业者和一人公司经营者在日常工作中面临严重的**工具碎片化问题**：

- 任务管理用 Todoist / Things，客户管理用 Excel 或简道云，财务记账用随手记/QuickBooks，时间追踪用 Toggl，内容排期用 Notion 或 Trello
- 数据在各个工具之间割裂，每天需要在 **5-8 个不同应用**之间切换
- 工具切换带来的上下文损耗占工作时间的 15-20%
- 现有 SaaS 工具面向团队协作设计，功能臃肿，一人公司需要为大量用不上的团队协作功能付费
- 78% 的独立创业者担心云端 SaaS 的数据安全问题，希望数据完全掌握在自己手中
- 纯本地工具无法跨设备同步，纯云端工具断网不可用

### 1.2 现有方案的不足

| 方案类型 | 代表产品 | 核心缺陷 |
|----------|----------|----------|
| 通用项目管理 | Notion、ClickUp、飞书 | 过于复杂，学习成本高，团队功能冗余 |
| 传统财务软件 | QuickBooks、金蝶 | 操作复杂，与日常工作流脱节，无法关联任务和项目 |
| 单一功能工具 | Todoist、Toggl、Forest | 只解决单一问题，数据孤岛严重 |
| 纯云端 SaaS | 多数 SaaS 产品 | 订阅制持续付费，数据在服务商，断网不可用 |
| 本地桌面应用 | 各类本地App | 数据备份困难，升级容易丢数据，环境不一致 |

### 1.3 产品机会

将任务管理、项目追踪、客户关系、财务记录、时间专注五大核心能力整合在一个**本地优先（Local-First）**的桌面应用中。前端资源、Go 后端和 SQLite 数据库随桌面安装包统一交付，无需用户额外安装运行环境，让独立创业者真正拥有一个属于自己的、数据完全自主的工作中枢。

---

## 2. 产品目标

### 2.1 产品定位

opc-workspace 是**一人公司的全栈操作系统**，是独立创业者每天打开的第一个、关闭的最后一个应用。

### 2.2 核心设计原则

1. **本地优先（Local-First）**：所有业务数据默认存储在操作系统应用数据目录中，用户完全拥有数据，支持离线使用
2. **All-in-One**：整合任务、项目、客户、财务、专注五大模块，消灭工具切换
3. **轻量快速**：使用 Tauri 系统 WebView 和内置 Go Sidecar，减少运行时依赖与后台资源占用
4. **安装即用**：React 前端、Go 后端和数据库初始化逻辑随安装包交付，不要求用户安装 Docker、Node.js 或 Go
5. **键盘优先**：完整的快捷键支持，⌘K 命令面板，高效操作
6. **可审计编排**：业务事件必须能追溯到来源、任务、责任 Actor、产出与验收；收件箱、任务和 Agent 执行各自只维护自己的事实，不复制状态

### 2.3 MVP 成功指标

| 指标 | 目标值 |
|------|--------|
| 已初始化后的冷启动时间 | P95 < 2 秒（从点击图标到界面可交互） |
| 桌面端安装包体积 | < 30MB（不含操作系统 WebView 运行时） |
| 运行时内存占用 | P95 < 200MB（Tauri + WebView + Go Sidecar 合计） |
| 核心功能模块 | 6 个（今日/任务/项目/客户/收件箱/专注） |
| 外部运行时依赖 | 0（无需 Docker、Node.js、Go） |
| 离线可用性 | 100% 核心功能离线可用 |

---

## 3. 用户画像与核心场景

### 3.1 目标用户画像

| 用户类型 | 典型特征 | 核心需求 | 使用频率 |
|----------|----------|----------|----------|
| 独立开发者 | 接外包项目、做独立产品，技术背景强 | 项目进度追踪、工时记录、发票管理、客户沟通 | 每天 4-8 小时 |
| 自由设计师 | 品牌设计、UI 设计，多个客户并行 | 客户管理、项目里程碑、收款追踪、灵感收集 | 每天 3-6 小时 |
| 内容创作者 | 自媒体、博主、写作者，产出节奏不稳定 | 内容日历、任务排期、收入统计、专注写作 | 每天 2-5 小时 |
| 咨询顾问 | 企业咨询、教练服务，按小时收费 | 客户档案、时间计费、发票开具、收入分析 | 每天 2-4 小时 |

### 3.2 核心使用场景

**场景 1：晨间规划**
> 早上打开 App，今日任务按用户保存的顺序、优先级和截止时间展示。用户扫一眼右侧面板的本月收入进度和待跟进发票，拖拽调整两项任务顺序后，点击"开始专注"进入工作状态。

**场景 2：深度工作**
> 进入专注模式，番茄钟自动计时 50 分钟，暂停 opc-workspace 自身通知并隐藏无关界面，当前任务全屏显示。用户可按引导开启操作系统专注模式。完成一个番茄后自动记录该任务工时，提示休息 5 分钟，然后按用户设置开始下一个番茄。

**场景 3：项目产出拆分与跟进**
> 项目任务提交文档、设计稿或其他本地产出后，系统生成一条“审核并拆分后续工作”的收件箱项。用户查看来源、产出和完成条件，将其拆成修改、发布、客户确认等关联任务，并分别分派给本人、仅作责任记录的外部人员或已注册的本地 Agent。收件箱持续聚合这些任务的执行、阻塞、Agent 输出和验收状态，直到所有必需任务完成后自动归档。

**场景 4（后续业务版）：发票待办与催办**
> 项目达到开票节点、发票临近到期或已经逾期时，本地调度器生成去重后的收件箱项。用户创建并分派“准备发票”“核对金额”“准备催款内容”等任务。本地 Agent 只能生成草稿、PDF 或本地文件，不自动向客户发送消息，也不自动确认付款；外部发送与付款确认由用户手动完成并记录。

**场景 5：本地提醒与异常跟进**
> 用户可以创建一次性本地提醒；任务临期、任务阻塞、本地 Agent 执行失败、备份失败等本地事件也可生成收件箱项。用户可立即处理、稍后提醒、忽略、关联已有任务或拆成新的任务。

**场景 6：周度复盘**
> 周五下午，用户打开统计页查看本周汇总：完成 18 项任务，总专注时间 22 小时，本周收入 ¥12,000，Top 3 时间花费项目一目了然。用户填写复盘备注后归档。

---

## 4. 技术架构方案

### 4.1 架构总览

采用 **"Tauri 桌面壳 + 内置前端 + Go Sidecar + 本地 SQLite"** 的单机架构，桌面用户运行时不依赖 Docker：

- **桌面层（Tauri/Rust）**：目标上提供原生窗口、系统托盘、本应用通知、全局快捷键和签名离线更新；在线 Updater 属于未来单独评审能力。当前基座已实现窗口、单实例保护、应用数据目录和 Go Sidecar 生命周期
- **界面层（React）**：构建后的静态资源直接打包进 Tauri，由系统 WebView 加载，无需 Nginx 或本地前端服务器
- **服务层（Go Sidecar）**：随安装包交付的单二进制后端，仅监听 `127.0.0.1` 动态端口；当前提供 HTTP API，规划中的本地工作流协调器、提醒调度器和 Agent Runner 也归属这一进程，第一阶段不得访问线上服务
- **数据层（SQLite）**：数据库、附件、发票和配置保存在 Tauri `appDataDir`，与应用程序文件和版本升级解耦

```
┌──────────────────────────────────────────────────────────┐
│                 Tauri 2.0 桌面进程                        │
│  原生窗口 │ 系统托盘 │ 全局快捷键 │ 签名更新 │ Sidecar 管理 │
│                                                          │
│  ┌────────────────────────────────────────────────────┐  │
│  │ 系统 WebView：加载安装包内置 React 静态资源          │  │
│  └──────────────────────┬─────────────────────────────┘  │
└─────────────────────────┼────────────────────────────────┘
                          │ HTTP / WebSocket
                          │ 127.0.0.1:动态端口 + 会话令牌
┌─────────────────────────▼────────────────────────────────┐
│                   Go Backend Sidecar                      │
│     业务逻辑 API │ 工作流/提醒 │ 本地 Agent │ 备份与迁移     │
└─────────────────────────┬────────────────────────────────┘
                          │ SQLite（WAL）/ 文件系统
┌─────────────────────────▼────────────────────────────────┐
│                     Tauri appDataDir                      │
│   opc-workspace.db │ attachments/ │ invoices/ │ config/   │
└──────────────────────────────────────────────────────────┘
```

### 4.2 技术栈详情

#### 前端

| 技术 | 选型 | 说明 |
|------|------|------|
| 框架 | React 18 + TypeScript | 生态成熟，组件复用性好 |
| 样式 | Tailwind CSS v4 | 原型已使用，迁移成本极低 |
| UI 组件 | 仓库内轻量 React 组件 | v0.1 使用 `Modal`、`PageHeader`、状态组件等自有实现；当前未安装 shadcn/ui |
| 状态管理 | Zustand | 轻量，适合个人工具，避免 Redux 复杂度 |
| 服务端状态 | TanStack Query | 数据请求、缓存、自动刷新 |
| 路由 | React Router | 标准路由方案 |
| 图标 | Lucide React | 原型已使用 |
| 构建 | Vite | 开发体验好，构建快 |

#### 后端

| 技术 | 选型 | 说明 |
|------|------|------|
| 语言 | Go 1.22+ | 单二进制、跨平台编译、内存占用低、并发好 |
| Web 框架 | Gin | 成熟、性能好、中间件丰富 |
| ORM | GORM | 功能完善，SQLite 支持好 |
| 数据库 | SQLite (WAL 模式) | 零配置、单文件、备份简单，个人工具足够 |
| 本地通信 | HTTP（当前）/ WebSocket（预留） | 仅绑定 `127.0.0.1`，生产环境使用动态端口 |
| 认证 | WebView 启动期会话令牌（当前）/ Agent 单次能力令牌（v0.2 规划） | 两类令牌隔离；Agent 不得使用 WebView 令牌，具体传输与专用中间件由 ADR 定义 |
| API 文档 | PRD 契约 + Go 集成测试（当前） | Swagger/OpenAPI 尚未接入，待接口范围稳定后补充 |

#### 桌面端（Tauri）

| 功能 | 目标实现方式 | 当前状态 |
|------|--------------|----------|
| 窗口管理 | Tauri 原生 | 已实现基础窗口 |
| 系统托盘 | Tauri Tray API（`tray-icon` feature） | 未开始 |
| 全局快捷键 | Tauri plugin-global-shortcut | 未开始；当前只有 WebView 内 `Ctrl/Cmd+K`、`Ctrl/Cmd+N` |
| 原生通知 | Tauri plugin-notification | 未开始 |
| 签名离线更新 | Tauri Bundler / 本地更新包 | 未开始；当前阶段不启用在线 Updater |
| 开机自启 | Tauri plugin-autostart | 未开始 |
| 单实例锁 | Tauri plugin-single-instance | 已实现，第二次启动会唤醒主窗口 |
| 文件对话框 | Tauri plugin-dialog | 未开始 |
| Sidecar 管理 | Tauri shell sidecar / Rust 子进程管理 | 已实现启动、ready 握手、健康检查、状态查询和退出清理基础 |
| 数据目录 | Tauri Path API（`appDataDir` / `appLogDir`） | 已实现目录初始化；日志落盘管线未接入 |

#### 部署

| 技术 | 选型 | 说明 |
|------|------|------|
| 前端交付 | Tauri `frontendDist` | Vite 构建产物内置于安装包，无需 Nginx |
| 后端交付 | Tauri `externalBin` | 每个目标平台打包对应架构的 Go Sidecar |
| 数据持久化 | Tauri `appDataDir` | 数据独立于应用程序文件，升级不覆盖 |
| 打包 | Tauri Bundler | 生成各平台原生安装包，并包含前端和 Sidecar |
| 开发环境 | 本机进程 | 直接运行 Vite、Go 和 `tauri dev`，不依赖 Docker |

### 4.3 技术栈选型理由

- **Tauri vs Electron**：Tauri 复用系统 WebView，应用运行时无需携带完整浏览器内核，更符合轻量桌面应用目标；实际包体、启动时间和内存以三平台验收测试为准。
- **Sidecar vs Docker**：Sidecar 随安装包交付，用户无需安装 Docker Desktop，也不需要拉取镜像或管理容器；Tauri 统一负责启动、健康检查、异常提示和退出清理。
- **Go vs Node.js**：Go 可编译为单二进制并按平台交叉构建，适合作为 Tauri Sidecar；业务逻辑与前端仍通过清晰的 API 边界解耦。
- **SQLite vs PostgreSQL**：单用户场景不需要网络数据库。SQLite 文件保存在系统应用数据目录，使用 WAL 提升并发读取，并通过 Online Backup API 生成一致性备份。

### 4.4 本地开发与构建

本地开发不使用 Docker，开发脚本统一编排三个进程：

```text
pnpm dev
├─ Go Backend：go run ./cmd/server --db ./.local/dev-data/opc-workspace.db --port 9876
├─ Vite Dev Server：提供前端热更新，并将 /api 代理到 Go Backend
└─ Tauri Dev：加载 Vite 页面并提供桌面能力
```

- `9876` 仅作为开发环境固定端口；生产环境由 Go Sidecar 监听随机可用端口
- 开发数据库与正式数据隔离，统一放在仓库 `.local/dev-data/` 并加入 `.gitignore`
- Go、Node.js/pnpm、Rust 和 Tauri CLI 只属于开发依赖，不要求最终用户安装
- CI 按目标平台和 CPU 架构编译 Go Sidecar，并使用 Tauri `externalBin` 的 target-triple 命名规则打包
- 构建产物必须验证 Sidecar 已随安装包包含，且在无 Go、Node.js、Rust 环境的干净系统上可启动

---

## 5. 功能模块详情

> 历史 HTML 原型已于 2026-08-27 从仓库移除。当前视觉事实以 `apps/web/src/`、`apps/web/src/styles.css` 和实际渲染为准；下列原型文件名仅保留为历史设计来源记录，不再是可访问文件或实施依赖。

### 5.1 今日工作台

**历史原型（已移除）**：`today-v1.html`

> **当前状态**：部分完成。真实任务和今日统计已接入，但任务仍按前 3/后 3 切片而非计划日期分组；日期切换、持久化拖拽、临期筛选、真实收入和客户动态尚未实现。

今日工作台是用户每天打开应用看到的默认首页，承担**晨间规划、当日执行、状态概览**三大职责。采用三栏布局。

#### 页面布局

| 区域 | 位置 | 宽度 | 职责 |
|------|------|------|------|
| 左侧导航栏 | 左侧固定 | 220px | 用户信息、全局搜索、模块导航、自动化卡片、本周效率 |
| 顶部状态栏 | 内容区顶部 | 自适应 | 日期、连续专注天数、视图切换、筛选、新建按钮 |
| 统计条 | 顶部下方 | 自适应 | 专注块数量、预计时长、月收入进度、逾期及临期任务数量 |
| 专注中卡片 | 内容区上部 | 自适应 | 当前任务、番茄钟倒计时、标签、控制按钮 |
| 今日任务列表 | 内容区中部 | 自适应 | "接下来·今天"分组任务，显示时间/时长/项目 |
| 本周稍后任务 | 内容区下部 | 自适应 | "稍后·本周"分组任务 |
| 右侧概览面板 | 右侧固定 | 280px | 专注模式环形进度、临期事项、本月收入迷你图、客户动态 |

#### 业务逻辑

1. 打开应用时自动加载当日数据，默认先按用户保存的手动顺序展示；未排序任务依次按优先级、截止时间和创建时间排列
2. 用户可拖拽调整任务顺序，调整结果立即持久化；提供“恢复默认排序”操作
3. 点击专注中卡片进入专注模式，启动番茄钟（默认 50 分钟）
4. 专注期间：暂停本应用通知、记录该任务实际工时、显示环形进度；系统级勿扰由用户授权并按平台能力启用
5. 番茄钟结束：完成当前专注轮次并提示休息 5 分钟；专注工时自动累计，任务是否完成由用户确认
6. 财务模块交付后，右侧收入数据根据本地已确认付款记录刷新；当前不得用静态数字模拟真实收入
7. 客户动态只显示用户手动记录或本地业务状态产生的事件；第一阶段不追踪提案下载、邮件回复或其他线上客户行为

#### 交互细节

- 任务行 hover 时显示快速操作按钮（完成、编辑、删除、开始专注）
- 统计条的逾期及临期数字可点击，快速筛选对应任务
- 日期 pill 点击可切换日期，查看过去/未来某天的任务安排
- 连续专注天数 hover 显示专注热力图

---

### 5.2 任务管理

**历史原型（已移除）**：`tasks-linear.html`

> **当前状态**：部分完成。已实现列表、新建、读取、基础状态更新、后端删除和前端关键词搜索；完整编辑、详情、删除 UI、项目/标签、父子任务、分派、验收、服务端分页筛选和拖拽均未实现。

目标提供全量任务视图；v0.1 先完成列表视图，任务看板归入 v0.2。

#### 页面布局

- 顶部：页面标题 + 新建任务按钮
- 工具栏：搜索框、筛选按钮、视图切换（列表/看板）
- 内容区：按状态分组展示任务
  - 待办（Todo）
  - 进行中（In Progress）
  - 阻塞（Blocked）
  - 待验收（Waiting Review）
  - 已完成（Done）
  - 已取消（Cancelled，默认折叠）

#### 任务属性

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| 标题 | 文本 | 是 | 2-200 字符 |
| 描述 | 富文本 | 否 | Markdown 支持 |
| 类型 | 枚举 | 是 | work / review / followup / reminder |
| 状态 | 枚举 | 是 | todo / in_progress / blocked / waiting_review / done / cancelled |
| 优先级 | 枚举 | 否 | P0(紧急) / P1(高) / P2(中) / P3(低) |
| 标签 | 多对多 | 否 | 可自定义颜色和名称 |
| 关联项目 | 外键 | 否 | 关联到项目 |
| 父任务 | 外键 | 否 | 用于项目产出后的工单拆分和任务树 |
| 完成条件 | 文本 | 否 | 用于人工验收或 Agent 输出检查 |
| 验收策略 | 枚举 | 是 | none / manual；普通快速任务默认 none，Agent 和高风险任务强制 manual |
| 截止日期 | 日期时间 | 否 | 含时间 |
| 预估时长 | 时长 | 否 | 分钟为单位 |
| 实际时长 | 时长 | 自动 | 专注模式自动累计 |
| 计划日期 | 日期 | 否 | 安排在哪天做 |
| 创建时间 | 时间戳 | 自动 | |
| 更新时间 | 时间戳 | 自动 | |

#### 核心功能

1. **双视图切换**
   - 列表视图：紧凑展示，适合批量操作，支持排序
   - 看板视图：按状态分列，支持拖拽卡片改变状态
2. **快速新建**
   - ⌘N 快捷键唤起新建任务面板
   - 标题为唯一必填项，可通过键盘快速补充截止时间、优先级和预估时长
3. **筛选与搜索**
   - 按项目、标签、状态、优先级、日期范围筛选
   - 全文搜索任务标题和描述
4. **批量操作**
   - 批量完成、批量删除、批量移动项目、批量加标签
5. **拆分、分派与验收**
   - 父子任务只表达真实的完成层级；项目产出产生的下游工单默认通过收件箱关联，不自动成为来源任务的子任务
   - 任务分派给本地 Actor；分派历史独立保存，不在任务上复制负责人状态
   - 人工或 Agent 提交产出后进入 `waiting_review`；验收通过才进入 `done`，要求返工则回到 `in_progress`
   - `person` 仅记录线下责任归属，不代表对方能够登录或远程操作应用
6. **任务产出**
   - 支持文本、文件、链接和结构化摘要，文件保存在应用控制的本地目录
   - 产出记录来源 Task、Actor、Agent Run、校验值和创建时间，可作为后续收件箱项的来源
7. **任务依赖**（MVP 后）
   - 设置前置任务，前置任务完成后才出现在待办中

#### 状态变更契约

- `PATCH /tasks/:id` 只编辑标题、描述、优先级、项目、标签、日期、预估时长、父任务和完成条件等非生命周期字段，不允许直接写 `status`、`completed_at`、`submitted_at` 或 `reviewed_at`。
- 状态变化使用显式命令：开始、阻塞、解除阻塞、直接完成、提交验收、接受/返工、取消和重新打开。
- `block`、`cancel`、`request_changes` 必须填写原因；原因和前后状态写入 Workflow Event。
- `complete` 只允许 `review_policy = none` 的人工任务；`review_policy = manual`、Agent 任务和高风险任务必须使用“提交产出 → 验收”链路。
- 当前 `/tasks/:id/status` 只用于兼容已实现的三态基础版本；引入扩展状态迁移后由显式命令替代，前端不得继续用通用状态按钮绕过验收。

| 命令 | 允许来源状态 | 目标状态 | 额外约束 |
|------|--------------|----------|----------|
| start | todo | in_progress | 存在活动 assignee；person 任务由 owner 代记 |
| block | todo / in_progress / waiting_review | blocked | 原因必填并保存 `blocked_from_status` |
| unblock | blocked | blocked_from_status | 仅允许恢复 todo / in_progress / waiting_review |
| complete | todo / in_progress | done | 仅人工任务且 review_policy = none |
| submit-output | todo / in_progress | waiting_review | review_policy = manual；至少提交一个有效 Artifact 或结构化说明 |
| accept | waiting_review | done | v0.1 只能由 owner reviewer 执行 |
| request_changes | waiting_review | in_progress | 原因必填；保留原产出和审核事件 |
| cancel | todo / in_progress / blocked / waiting_review | cancelled | 原因必填；不等同完成 |
| reopen | done / cancelled | todo | 清除终态时间，保留历史事件和产出 |

#### 键盘快捷键

| 快捷键 | 功能 |
|--------|------|
| ⌘K | 全局命令面板（搜索/跳转） |
| ⌘N | 新建任务 |
| / | 聚焦搜索框 |
| Space | 快速完成/取消任务 |
| J/K | 上下导航任务 |
| Enter | 打开任务详情 |
| E | 编辑当前任务 |
| D | 删除当前任务（需确认） |

---

### 5.3 项目管理

**历史原型（已移除）**：`projects-linear.html`

> **当前状态**：页面骨架。已有 `projects` 表、路由和空状态；无 Go model、API、CRUD、卡片或详情。

项目采用卡片式网格布局，是任务的上层组织单位。

#### 项目属性

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| 名称 | 文本 | 是 | 2-100 字符 |
| 描述 | 文本 | 否 | |
| 客户 | 外键 | 否 | 关联客户 |
| 状态 | 枚举 | 是 | planning / in_progress / paused / completed / archived |
| 开始日期 | 日期 | 否 | |
| 截止日期 | 日期 | 否 | |
| 项目金额 | 金额 | 否 | 合同金额 |
| 颜色标记 | 颜色 | 否 | 用于看板和标签 |
| 进度 | 百分比 | 自动 | 已完成任务/总任务 |

#### 项目状态流转

```
规划中 → 进行中 → 已完成 → 已归档
              ↓         ↑
              └→ 暂停 ──┘
```

| 状态 | 含义 | 可执行操作 |
|------|------|-----------|
| 规划中 | 已创建未开始 | 开始、编辑、删除、归档 |
| 进行中 | 正在执行 | 暂停、完成、编辑、归档 |
| 已完成 | 交付完成 | 重新打开、归档、生成发票 |
| 已归档 | 不在活跃列表 | 恢复、彻底删除 |

#### 项目详情页

点击项目卡片进入详情页，包含：
- 项目基本信息
- 该项目下所有任务列表（可按状态筛选）
- 时间记录汇总（总投入工时）
- 关联发票记录
- 项目笔记/文件附件
- 收入与成本记录

项目任务提交产出、进入阻塞或达到交付/验收节点时，可由本地规则创建带稳定去重键的收件箱项。项目状态与收件箱状态互不替代：项目继续维护自身生命周期，收件箱只跟进需要处理的下一步工作。

---

### 5.4 客户管理

**历史原型（已移除）**：`clients-linear.html`

> **当前状态**：页面骨架。已有 `clients` 表、路由和空状态；无 API、CRUD、表格、详情或真实活动。

客户列表采用表格视图，管理客户关系。

#### 客户属性

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| 名称 | 文本 | 是 | 公司或个人名称 |
| 联系人 | 文本 | 否 | 对接人姓名 |
| 邮箱 | 邮箱 | 否 | |
| 电话 | 文本 | 否 | |
| 备注 | 长文本 | 否 | |
| 标签 | 多对多 | 否 | VIP、潜在、老客户等 |
| 累计收入 | 金额 | 自动 | v0.4 从该客户 `confirmed` Financial Entry 聚合；paid Invoice 只作关联与对账 |
| 项目数 | 整数 | 自动 | 关联项目数 |
| 状态 | 枚举 | 是 | active / lead / inactive |
| 最近动态 | 时间戳 | 自动 | 最近一次互动时间 |
| 创建时间 | 时间戳 | 自动 | |

#### 客户列表

表格列：客户（头像+名称）、联系人、项目数、累计收入、状态、最近动态、操作

#### 客户详情页

- 基本信息卡片
- 客户动态时间线（本地记录的付款、会议、沟通笔记、项目状态等）；线上下载或邮件事件只有在未来明确集成后才能出现
- 关联项目列表
- 关联发票列表（状态、金额、日期）
- 沟通记录笔记
- 文件附件

#### 客户回访（后续版本）

> **状态**：未开始，不属于 v0.1。v0.1 只保留基础客户资料目标，回访计划、提醒和结果分析在后续版本实现。

- 为客户建立下一次回访日期、本地 Actor 负责人、渠道、目的和备注；`person` 负责人只作为本地责任记录。
- 回访持久状态为 `planned / completed / skipped / cancelled`；`due / overdue` 由 planned、计划时间和用户时区派生，避免时间流逝造成持久状态漂移。完成时记录结果、下一步和下一次回访时间。
- 今日工作台和收件箱后续展示到期/逾期回访，并可跳转客户详情。
- 第一版只做本地计划、记录和提醒，不自动发送邮件、短信或外部消息。
- 后续通过递增迁移增加 `client_followups`，并提供分页、到期筛选、完成/跳过和重新安排 API。

---

### 5.5 收入、支出与发票（后续版本）

**历史原型（已移除）**：`income-linear.html`、`invoices-linear.html`

> **状态**：后续版本。当前只有收入和发票导航页面骨架；v0.1 不交付收入/支出账本、统计、发票 CRUD 或 PDF。

#### 收入看板

- **顶部 KPI 卡片**
  - MRR（月度经常性收入）
  - 本月收入（环比增长率）
  - 年度累计收入
  - 平均客单价
- **收入趋势图**：折线图，支持按月/季度/年切换
- **收入明细列表**：可按客户、项目、日期筛选

#### 收入与支出账本

- 统一记录收入和支出，字段包括类型、金额、币种、日期、分类、客户、项目、发票、备注和附件。
- 金额继续使用最小货币单位整数；支出必须使用负债无关的独立类型字段，不以负数金额隐式表达。
- 支持按月、分类、客户和项目统计收入、支出、净现金流，并明确区分已发生与待确认金额。
- 支持新增、编辑、删除、筛选和 CSV 导出；删除财务记录需二次确认并保留可审计的更新时间。
- 后续新增 `financial_entries` 迁移和版本化 API；任何 KPI、趋势图和右侧概览都必须来自真实聚合查询。

#### 发票管理

**发票属性**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| 发票编号 | 文本 | 自动 | 自动生成，如 INV-2026-001 |
| 客户 | 外键 | 是 | |
| 项目 | 外键 | 否 | |
| 金额 | 金额 | 是 | |
| 币种 | 枚举 | 是 | CNY / USD 等 |
| 状态 | 枚举 | 是 | draft / sent / viewed / paid / overdue |
| 开票日期 | 日期 | 是 | |
| 到期日期 | 日期 | 是 | |
| 付款日期 | 日期 | 否 | |
| PDF 文件 | 文件 | 自动 | 系统生成 |
| 备注 | 文本 | 否 | |

**发票状态流转**：

```
草稿 → 已发送 → 已查看 → 已付款
         ↓
       逾期（到期未付款自动标记）
```

**发票功能**：
- 项目完成后一键生成发票（自动填充客户和金额）
- 导出发票为 PDF
- 手动标记已付款
- 逾期自动提醒（到期前3天、当天、逾期后每天）
- owner 确认付款时，由发票领域事务创建或关联唯一的 `confirmed` Financial Entry；该入账不是自动化规则，避免重复入账和越权写财务事实
- 达到开票节点、临近到期或逾期时，由本地调度器生成去重后的收件箱项，并可拆分为准备发票、核对金额、准备催款内容等任务
- 本地 Agent 可以生成草稿或本地 PDF，但不得自动向客户发送消息、确认付款或改变财务事实；这些动作必须由 owner 完成

#### 模态框：收入详情

**历史原型（已移除）**：`modal-income-detail.html`

点击收入数字打开详情模态框，展示：
- 该月收入明细列表
- 按客户/项目分类的饼图
- 同比环比对比
- 收入来源分析

---

### 5.6 收件箱与本地工作编排中心

**历史原型（已移除）**：`inbox-linear.html`。其中依赖在线客户行为、远程消息或外部服务的内容不属于第一阶段。

> **当前状态**：页面骨架。当前只有路由、固定未读数、无行为的“全部标为已读”和空状态；以下 Actor、Inbox Item、Assignment、提醒、Agent Run 和审计均为规划。

收件箱不是普通通知列表，而是统一承接本地业务事件、明确下一步工作、拆分任务、分派责任、跟踪执行和完成验收的**本地工作受理与编排中心**。

第一阶段完全运行在本机，不提供多人登录、云同步、远程通知、邮件或消息自动发送，也不依赖线上 Agent 或模型服务。

#### 对象边界

| 对象 | 负责的问题 | 不负责的问题 |
|------|------------|--------------|
| Inbox Item | 为什么需要处理、来自哪个业务对象、是否已受理、稍后提醒或解决 | 不保存任务执行状态，不直接保存负责人 |
| Task | 具体要完成什么、执行到哪一步、完成条件和验收结果 | 不重复保存收件箱已读、稍后提醒等展示状态 |
| Assignment | 当前任务由谁负责，以及历史上如何改派 | 不拥有独立的任务完成状态 |
| Agent Run | 本地 Agent 某一次执行尝试的排队、运行、成功、失败或中断 | 执行成功不等于任务完成 |
| Task Artifact | 人或 Agent 的文本、文件、链接或结构化产出，同时区分实际产出者与录入者 | 不决定任务是否验收通过 |
| Workflow Event | 创建、拆分、分派、执行、验收和返工的追加式时间线 | 不作为业务状态的第二事实来源 |

核心约束：

1. 收件箱项不能直接分派。“分派收件箱项”在交互上必须原子地创建或关联 Task，再为 Task 创建 Assignment。
2. 已读、未读和稍后提醒是展示属性，不是工作流状态。
3. 收件箱进度由关联的必需任务实时派生，不保存第二份进度。
4. Agent Run 的 `succeeded` 只表示产生了输出；默认将任务推进到 `waiting_review`，由 owner 验收后才进入 `done`。
5. 发票、项目、客户等业务对象继续维护自身状态；收件箱只跟进由这些状态产生的待处理工作。

#### 本地 Actor 模型

| Actor 类型 | 定义 | 目标能力 |
|------------|------|--------------|
| `owner` | 当前设备上的应用所有者，固定一个 | 创建、拆分、分派、改派、验收、返工、解决和重新打开工作 |
| `person` | 本地通讯录式责任人记录 | 可以成为任务负责人；不登录、不接收同步、不直接操作应用 |
| `agent` | 由 Sidecar 管理且通过健康检查的本地执行器 | 在授权能力和本地资源范围内运行并提交产出，不能自行验收 |
| `system` | 内置调度与规则主体 | 生成去重事件、派生进度、记录本地故障；不能替代用户完成高风险验收 |

版本边界：v0.1 先交付 owner/person/system 的人工受理、分派和验收闭环；agent Actor、Adapter 和实际 Run 归入 v0.2。界面必须明确提示：分派给 `person` 仅记录责任归属，不会向对方发送任务或授予访问权限。客户联系人不自动成为 Actor，只有用户显式创建或关联后才能作为负责人。没有已注册并健康的本地执行器时，`agent` 选项必须禁用并说明原因，不能用占位 Actor 暗示 Agent 已可执行。

#### 规划事件来源

v0.1 人工编排阶段：

- `reminder_due`：用户创建的一次性 Reminder 到期后生成收件箱项；Reminder 是调度事实，Inbox Item 是到期后的处理事实。
- `task_output_submitted`：仅项目交付类任务或 owner 显式标记 `requires_followup` 的产出生成；普通子任务产出默认更新当前任务/收件箱，不递归创建新项。去重键必须包含 Artifact ID。
- `task_due`：任务临期或逾期。
- `task_blocked`：任务进入阻塞状态。
- `system_maintenance`：备份、迁移或 Sidecar 出现需要处理的异常。

v0.2 本地 Agent 阶段：

- `agent_run_failed`：Agent Runner 只追加失败事件；内置自动化投影器是诊断 Inbox Item 的唯一生产者，使用 `agent-run:<run_id>:failed` 去重。已经有活动 Inbox 时更新现有项。
- `agent_review_required`：Agent Runner 只追加待验收事件；内置投影器仅在 Task 未关联任何活动 Inbox Item 时创建验收项，否则更新现有收件箱进度和时间线。去重键包含 Agent Run ID。

关联模块交付后再启用：

- `invoice_due / invoice_overdue`：发票临期或逾期。
- `client_followup_due`：客户回访到期。
- `project_milestone_reached`：项目达到交付、验收或开票节点。

同一来源事件必须生成稳定的 `source_event_key`，应用重启或调度器重复扫描时不得创建重复项。外部客户行为、邮件回复、提案下载等事件在没有本地导入或明确集成前不得伪造。

#### 业务主链路

```text
本地业务事件
  → Inbox Item(open)
  → 查看来源与完成条件
  → 创建/关联一个或多个 Task
  → 为 Task 分派 Actor
  → 人工执行或启动本地 Agent Run
  → 提交产出
  → owner 验收或要求返工
  → 所有必需 Task 完成
  → Inbox Item(resolved)
```

项目产出拆分必须在一个 SQLite 事务中完成：创建父子任务或关联已有任务、标记必需任务、建立初始 Assignment、写入审计事件，并把收件箱项推进为跟进中；任一步骤失败时全部回滚。

#### 状态机与完成规则

Inbox Item 主状态：

```text
open → tracking → resolved
  └────────────→ dismissed

resolved / dismissed → open 或 tracking（重新打开时按现有关联任务派生）
```

- `read_at` 与 `snoozed_until` 独立于主状态。
- 默认完成策略为 `all_required_tasks_done`。
- 零个必需任务时不得使用空集合规则自动解决，必须由 owner 明确解决或先关联必需任务。
- 必需任务处于 `cancelled`、`blocked`、`waiting_review` 或 Agent 失败状态时不得自动解决收件箱项。
- `resolution_policy = manual` 时，owner 可带原因解决；`all_required_tasks_done` 时普通 resolve 必须验证所有活动必需任务均为 done，不能绕过未完成任务。
- 确实不再需要跟进时，owner 先取消/解除必需关联，或使用单独的危险操作 `force-resolve`；强制解决必须填写原因、二次确认并写入不可变审计，不能伪装成任务正常完成。
- 忽略必须带原因；解决、强制解决和忽略都必须可重新打开。
- 重新打开时，存在未完成的必需任务则进入 `tracking`；否则进入 `open`，且在新增/重新激活必需任务前不触发自动解决，避免立即弹回 `resolved`。
- “待验收”“Agent 处理中”“阻塞”等只是关联 Task/Agent Run 派生的列表筛选，不是 Inbox Item 的额外主状态。

Task 状态：

```text
todo → in_progress → waiting_review → done
  │          │              └→ in_progress（返工）
  │          └→ blocked → todo / in_progress
  └────────────────────────→ cancelled
```

- `person` 负责人无法直接更新应用状态，由 owner 根据线下结果记录。
- Agent 输出成功后默认进入 `waiting_review`；验收通过进入 `done`，返工回到 `in_progress`，每次重试创建新的 Agent Run。
- 第一阶段不允许 Agent 自行关闭任务。
- `review_policy = none` 的普通人工任务可由 owner 直接完成；`review_policy = manual`、Agent 任务和高风险任务必须经过提交产出与验收命令。
- 父任务进度由非取消子任务派生；只有至少存在一个非取消子任务且它们全部完成时，父任务最多自动进入 `waiting_review`，不能跳过验收。若所有子任务均取消，则不自动推进，owner 必须重新打开子任务、取消父任务或按策略明确处理。收件箱是否解决仍只看活动 `inbox_item_tasks.is_required` 标记的任务。

Agent Run 状态：

```text
queued → running → succeeded
                 ├→ failed
                 ├→ cancelled
                 └→ interrupted
```

应用异常退出后仍为 `running` 的记录在恢复时标记为 `interrupted`，不得静默视为成功或无条件自动重试。

#### 页面与交互

收件箱列表：

- 标签页：待处理、跟进中、稍后提醒、待验收、已解决。
- 支持来源、优先级、截止时间、任务状态、项目和负责人筛选。
- 展示来源对象、必需任务进度、当前负责人和最近活动。
- 支持单条已读、全部已读、稍后提醒和恢复。

收件箱详情：

- 来源上下文与创建时的最小快照。
- 处理目标、完成条件、本地产出和附件。
- 任务树及必需/可选标记。
- 当前负责人、审核人和改派历史。
- Agent Run、输出、错误、取消和重试记录（v0.2）。
- 创建、拆分、分派、状态变化、验收和返工时间线。
- 操作：拆分任务、关联已有任务、分派/改派、接受结果、要求返工、阻塞、稍后提醒、忽略、重新打开；启动/取消/重试 Agent 属于 v0.2。

拆分任务面板：

- 一次创建多条父子任务。
- 支持优先级、计划日期、截止日期、项目、完成条件和必需标记。
- v0.1 每条任务选择 owner 或 person；v0.2 仅在本地 Adapter 健康且权限已验证时显示 agent。
- 提交后原子创建任务、关联、分派和审计事件。

Actor 管理位于设置页。owner 和 system 为内置 Actor，不可删除；owner 只允许修改展示名称，类型与内置属性不可变；v0.1 的 person 支持名称、备注和停用；v0.2 的 agent 支持本地适配器、能力范围、启停和健康检查。已有历史引用的 Actor 只能停用，不能硬删除。

#### 本地 Agent 安全边界

- Agent 不得直接打开 SQLite，也不复用 Tauri WebView 的 Bearer Token。
- owner 通过普通 `/api/v1/tasks/:id/agent-runs` 创建 Run；Sidecar 再通过受控进程管道或专用 `/api/v1/agent-runtime/*` 路由向 Adapter 发放短时、单次 Run 能力令牌，普通业务路由不得接受该令牌。
- v0.2 Adapter 先通过应用层能力白名单拒绝网络、任意数据库、任意 Shell 和任意目录访问；同时必须在各平台评审可验证的进程沙箱/网络阻断机制。若某平台无法强制隔离，Adapter 只能保留为禁用诊断记录，正式 agent Actor、分派和执行入口不得启用。
- 发票发送、客户沟通、付款确认、删除业务数据等动作必须由 owner 执行。
- 所有写操作通过 Sidecar 事务完成，并记录 request ID、Actor、资源、动作和时间。
- 本地审计用于可追踪性，不宣称能够抵抗拥有操作系统文件权限的用户篡改。
- v0.1 的 reviewer 只能是 owner；system 只能负责明确的内部维护任务。创建 Agent Run 时，assignment 必须是该 Task 当前活动的 agent Assignment，且 `assignment.actor_id` 与 `agent_actor_id` 一致。

#### 验收条件

- 屏蔽外部网络时，人工受理、拆分、分派、执行跟踪、验收和归档仍可完整运行。
- person 分派明确显示“仅记录负责人，不会发送或同步”。
- 同一来源事件跨扫描、跨重启只生成一条 Inbox Item。
- 拆分失败时不遗留部分任务、关联、分派或审计事件。
- 收件箱进度完全由关联必需任务派生。
- Agent Run 成功后任务进入 `waiting_review`；只有 owner 验收通过才进入 `done`。
- 返工保留原 Run 和原产出，新重试形成独立记录。
- Actor 停用不破坏历史分派和审计。
- 幂等请求重放不重复创建资源或时间线事件。
- 并发改派、验收或解决发生版本冲突时拒绝旧写入并提示刷新。

---

### 5.7 专注模式

专注模式是核心差异化功能，提供无干扰的深度工作环境。

> **当前状态**：部分完成。前端内存番茄钟、循环、设置和提示音可用；任务绑定、Session API、工时累计、绝对时间校正、异常恢复和原生通知未实现。

#### 番茄钟

- 默认时长：50 分钟工作 / 5 分钟休息，可自定义
- 环形进度条可视化时间流逝
- 支持暂停/继续/跳过
- 专注结束音效提醒

#### 专注期间行为

- 自动暂停 opc-workspace 自身通知
- 提供开启系统专注/勿扰模式的引导；系统级控制按平台能力和用户授权实现，不作为跨平台强保证
- 记录该任务的起止时间，累计实际工时
- 当前任务高亮显示
- 可选：白噪音播放（雨声、咖啡馆、白噪音、粉红噪音）
- 可选：屏蔽指定网站（通过系统 hosts 或浏览器扩展）

#### 专注统计

- 每日专注块数量、总专注时长
- 连续专注天数（Streak）
- 周/月专注报告（类似 GitHub 贡献图的热力图）
- 按项目/标签统计时间分配
- 最佳专注时段分析

#### 专注设置模态框

**历史原型（已移除）**：`modal-focus-settings-linear.html`

v0.1 第一版可配置：

| 配置项 | 默认值 | 可选范围 / 行为 |
|--------|--------|-----------------|
| 专注时长 | 50 分钟 | 5–120 分钟，步进 5 分钟 |
| 休息时长 | 5 分钟 | 5–30 分钟，步进 5 分钟 |
| 循环次数 | 4 次 | 1–8 次 |
| 自动开始休息 | 开启 | 专注阶段结束后是否立即开始休息计时 |
| 自动开始专注 | 关闭 | 休息阶段结束后是否立即开始下一轮专注 |
| 结束后提示音 | 开启 | 阶段切换时播放短提示音；受系统音频和 WebView 自动播放策略限制 |

当前实现使用 Zustand `persist` 将个人资料、通用、外观和专注配置保存在当前浏览器或桌面 WebView 的 `localStorage`，历史存储键仍为 `opc-focus-settings`。设置支持实时预览；专注参数变化会立即暂停并重置当前计时，取消只恢复原参数，不能恢复已经消耗的时间和运行状态，这是待修复的行为缺口。浏览器开发环境和桌面 WebView 的设置彼此独立。

长休息、暂停本应用通知、系统专注/勿扰引导、白噪音和专注期间自动状态设置仍属于后续能力，不属于当前设置弹窗的已交付范围。

---

### 5.8 路线图与内容日历

> **当前状态**：两个路由均为明确的“后续版本”占位页，无表、API 或业务交互；目标版本为 v0.3。

#### 路线图

**历史原型（已移除）**：`roadmap-linear.html`

- 新增 `roadmap_milestones`，至少包含标题、季度、目标日期、状态、说明和项目关联。
- 支持季度分组、里程碑新增/编辑、日期调整、归档和项目跳转。
- 进度只从关联项目/任务派生，不重复存储第二份完成百分比。
- 里程碑临期或达成可幂等生成本地 Inbox Item，但不自动改变项目状态。
- 验收覆盖季度边界、关联项目删除、进度口径、拖拽/编辑失败回滚和空/错误/重试。

#### 内容日历

**历史原型（已移除）**：`content-calendar-linear.html`

- 新增 `content_items`，至少包含标题、平台、状态、发布时间、项目和准备任务关联。
- 提供月视图、月份切换、详情、新建/编辑、拖拽发布日期和准备任务入口。
- 内容卡片展示标题、平台（公众号/小红书/抖音/博客等）、状态（草稿/审核中/已发布）和发布时间。
- 审核到期或计划发布时间可创建 Inbox Item；第一阶段不自动发布到任何外部平台。
- 验收覆盖跨月、用户时区、拖拽回滚、关联任务状态、重复提醒和空/错误/重试。

---

### 5.9 全局功能

#### 命令面板（⌘K）

**历史原型（已移除）**：`modal-command-palette.html`

> **当前状态**：部分完成。支持页面命令、最多 12 条已加载任务和基本键盘导航；无后端全局搜索、详情直达、最近使用或项目/客户/收件箱搜索。

- 模糊搜索所有页面、任务、项目、客户、发票
- 快速执行操作：新建任务、切换页面、开始专注、生成发票
- 最近使用记录优先
- 键盘全操作：上下导航、Enter 选择、Esc 关闭

#### 自动化

**历史原型（已移除）**：`modal-automation.html`

> **当前状态**：未开始；目标版本为 v0.2 预设本地规则。历史 HTML 原型已移除，当前无规则表、执行器、调度器或运行记录。

- 内置自动化规则，用户可配置开关
- 预设规则示例：
  - 项目标记为"已完成"时 → 提示生成发票
  - 每日早上 9 点 → 推送今日任务安排
  - 发票逾期 → 每天发送提醒通知
  - 周五下午 5 点 → 提醒用户进行本周复盘
- 自动化执行记录展示

第一阶段自动化只消费本地 Workflow Event，并创建本地 Inbox Item、Task 或应用内提醒；不得自动发送邮件、发票、客户消息或访问线上工作流。每次执行以 `rule_id + event_id` 去重，并防止规则递归触发。

#### 筛选器

**历史原型（已移除）**：`modal-filter.html`

> **当前状态**：未开始；任务页“筛选”按钮目前没有业务行为。

- 多维度筛选面板
- 支持保存常用筛选为视图
- 筛选条件：状态、标签、日期范围、项目、客户、优先级

#### 新建任务模态框

**历史原型（已移除）**：`modal-new-task.html`

- 快速填写任务所有属性
- 支持 Markdown 描述编辑
- 标签选择器
- 日期时间选择器
- 项目关联选择

#### 任务详情模态框

**历史原型（已移除）**：`modal-task-detail.html`

- 任务完整信息
- 子任务列表
- 评论/备注时间线
- 时间记录历史
- 编辑/删除/开始专注操作

---

### 5.10 AI 助手（待开发）

> **状态**：未开始；不属于 v0.1，目标版本和本地模型/运行时待单独评审。当前仓库没有模型 SDK、密钥、AI API、会话表或助手页面；v1.6 当前阶段不接入线上模型服务。

#### 目标能力

- 提供独立 AI 助手入口，支持用户显式发起问答、摘要和辅助建议。
- 先通过统一 Adapter 接口评估用户主动配置的本地模型，业务代码不绑定单一运行时。
- 提供本地运行时配置、模型选择、连通性测试、超时/资源提示和撤销授权。
- 用户可主动选择任务、项目、客户或知识库检索结果作为上下文；当前阶段上下文不得离开本机。
- 接入知识库后，回答必须展示来源引用；没有可靠依据时显示“未检索到可靠来源”。

#### 规划实现方法与边界

- Go Sidecar 定义运行时无关的本地 Adapter 接口，统一请求、响应、流式输出、超时、取消和错误码；React 实现助手会话、上下文选择和来源展示。
- 本地运行配置和敏感凭据不得进入普通 SQLite 表、`localStorage`、日志或命令行，必须使用应用配置边界或操作系统安全存储。
- 第一阶段模型输出只读，不得自动修改任务、项目、客户、发票、收入或支出。
- 自主代理、智能排程、自然语言直接执行写操作和自动生成经营报告必须另行立项和授权。
- AI 不可用、未配置或离线时，本地核心功能必须继续正常运行。
- 远程 Provider 若未来重新进入范围，必须新增 ADR 并获得用户明确授权，重新定义数据外发、密钥、用量、撤销和失败边界。

### 5.11 本地知识库（待开发）

> **状态**：未开始；不属于 v0.1。当前仓库没有知识库表、文档提取器、索引、检索 API、向量依赖或知识库页面。

#### 目标能力

- 支持导入 Markdown、TXT、PDF 等本地资料，记录来源、更新时间、索引状态和可删除范围。
- 支持关键词搜索、增量更新、重新索引、单文档删除和知识库彻底清理。
- 检索结果可定位到原始文件或具体分段，并可作为 AI 助手的显式上下文。
- 语义检索和向量存储在技术验证后决定，不预先绑定远程向量数据库或特定 embedding 服务。

#### 规划实现方法与上线闸门

- 先通过递增迁移增加知识源、文档、分段和索引任务元数据，使用 SQLite FTS5 建立可验证的本地关键词检索基线。
- Go Sidecar 负责导入、文本提取、分段、索引、检索和删除；索引任务必须可取消、可重试并显示进度。
- 原始文件、派生文本、索引和缓存之间建立可验证的级联删除关系；用户能导出来源清单并彻底清理知识库。
- 通过来源准确性、无答案处理、提示注入、越权读取、删除完整性、索引恢复和离线检索测试后才能交付。
- 是否加入本地向量索引或本地 embedding 必须单独记录包体、性能和跨平台影响；远程 embedding 若未来进入范围，必须新增 ADR、数据外发预览和用户明确授权。

---

## 6. 数据模型设计

### 核心实体关系

```text
Owner（单机唯一用户）
 └── Actor
      ├── owner
      ├── person
      ├── agent
      └── system

InboxItem
 ├── source → Task / Project / Client / Invoice / System
 ├── InboxItemTask → Task
 └── WorkflowEvent

Reminder → InboxItem（到期时以 source_event_key 幂等生成）

Task
 ├── parent → Task
 ├── Tag（多对多）
 ├── Project（多对一）
 ├── TaskAssignment → Actor
 ├── AgentRun → agent Actor
 ├── TaskArtifact
 ├── FocusSession
 └── WorkflowEvent

Project → Client / Task / Invoice
Client → Project / Invoice / Activity
Invoice → Client / Project
```

对象事实边界：Inbox Item 保存来源与分诊，Task 保存实际执行状态，Assignment 保存责任变化，Agent Run 保存单次本地执行，Task Artifact 保存产出，Workflow Event 保存追加式审计。禁止在多个表中复制同一个完成状态。

### SQLite 存储约定

- 主键统一使用 UUID 文本
- 时间戳统一使用 UTC；API 使用 RFC 3339，数据库默认值使用 `CURRENT_TIMESTAMP`
- 纯日期字段使用 `YYYY-MM-DD` 文本，并在用户本地时区解释
- 金额使用最小货币单位整数存储，例如 `amount_minor = 12345` 表示人民币 `123.45` 元，避免浮点误差
- 布尔值使用 `INTEGER` 的 `0/1` 表示，并通过 `CHECK` 约束取值
- Go Sidecar 每次建立 SQLite 连接时执行 `PRAGMA foreign_keys = ON`、`PRAGMA journal_mode = WAL` 和合理的 `busy_timeout`
- 外键必须明确删除策略；客户、项目等仍被发票引用时默认 `RESTRICT`，可选关联默认 `SET NULL`

### 主要数据表

**tasks** - 任务表

| 字段 | 类型 | 约束 |
|------|------|------|
| id | TEXT | PRIMARY KEY (UUID) |
| title | TEXT | NOT NULL |
| description | TEXT | |
| kind | TEXT | work / review / followup / reminder；规划字段 |
| status | TEXT | 当前为 todo / in_progress / done；规划扩展 blocked / waiting_review / cancelled |
| priority | TEXT | DEFAULT 'P2' |
| project_id | TEXT | FOREIGN KEY → projects.id |
| parent_task_id | TEXT | FOREIGN KEY → tasks.id；规划字段 |
| completion_criteria | TEXT | 可验证的完成条件；规划字段 |
| review_policy | TEXT | none / manual；普通快速任务默认 none，Agent/高风险任务强制 manual；规划字段 |
| blocked_reason / blocked_at | TEXT | 阻塞原因与时间；规划字段 |
| blocked_from_status | TEXT | 解除阻塞时恢复的 todo / in_progress / waiting_review；规划字段 |
| due_date | TEXT | RFC 3339 UTC |
| planned_date | TEXT | YYYY-MM-DD |
| estimated_minutes | INTEGER | |
| actual_minutes | INTEGER | DEFAULT 0 |
| manual_order | INTEGER | 用户手动排序值，可为空 |
| created_at | TEXT | NOT NULL DEFAULT CURRENT_TIMESTAMP |
| updated_at | TEXT | NOT NULL DEFAULT CURRENT_TIMESTAMP |
| completed_at | TEXT | RFC 3339 UTC |
| submitted_at / reviewed_at | TEXT | 产出提交与验收时间；规划字段 |
| version | INTEGER | 乐观并发版本；规划字段 |

**projects** - 项目表

| 字段 | 类型 | 约束 |
|------|------|------|
| id | TEXT | PRIMARY KEY |
| name | TEXT | NOT NULL |
| description | TEXT | |
| client_id | TEXT | FOREIGN KEY → clients.id |
| status | TEXT | NOT NULL DEFAULT 'planning' |
| start_date | TEXT | YYYY-MM-DD |
| due_date | TEXT | YYYY-MM-DD |
| amount_minor | INTEGER | 最小货币单位 |
| color | TEXT | |
| created_at | TEXT | NOT NULL DEFAULT CURRENT_TIMESTAMP |
| updated_at | TEXT | NOT NULL DEFAULT CURRENT_TIMESTAMP |

**clients** - 客户表

| 字段 | 类型 | 约束 |
|------|------|------|
| id | TEXT | PRIMARY KEY |
| name | TEXT | NOT NULL |
| contact_name | TEXT | |
| email | TEXT | |
| phone | TEXT | |
| notes | TEXT | |
| status | TEXT | NOT NULL DEFAULT 'active' |
| created_at | TEXT | NOT NULL DEFAULT CURRENT_TIMESTAMP |
| updated_at | TEXT | NOT NULL DEFAULT CURRENT_TIMESTAMP |

**client_activities / client_attachments** - 客户本地活动与附件（v0.1 规划）

- `client_activities` 至少包含 `id`、`client_id`、`kind`、标题/正文、`occurred_at`、`created_by_actor_id`、可选来源类型/ID、`version`、`deleted_at` 和时间戳。人工 note/meeting 可编辑；system_reference 只引用 Workflow Event，不复制业务事实。
- `client_attachments` 至少包含 `id`、`client_id`、可选 `activity_id`、受控相对路径、文件名、MIME、大小、SHA-256、录入 Actor、`deleted_at` 和时间戳。文件读取和软删除必须经过 Sidecar 与审计。

**invoices** - 发票表

| 字段 | 类型 | 约束 |
|------|------|------|
| id | TEXT | PRIMARY KEY |
| invoice_number | TEXT | NOT NULL UNIQUE |
| client_id | TEXT | NOT NULL FOREIGN KEY |
| project_id | TEXT | FOREIGN KEY |
| amount_minor | INTEGER | NOT NULL，最小货币单位 |
| currency | TEXT | NOT NULL DEFAULT 'CNY' |
| status | TEXT | NOT NULL DEFAULT 'draft' |
| issue_date | TEXT | NOT NULL，YYYY-MM-DD |
| due_date | TEXT | NOT NULL，YYYY-MM-DD |
| paid_date | TEXT | YYYY-MM-DD |
| notes | TEXT | |
| created_at | TEXT | NOT NULL DEFAULT CURRENT_TIMESTAMP |
| updated_at | TEXT | NOT NULL DEFAULT CURRENT_TIMESTAMP |

**focus_sessions** - 专注记录表（当前 schema v2 与规划扩展）

| 字段 | 类型 | 约束 |
|------|------|------|
| id | TEXT | PRIMARY KEY |
| task_id | TEXT | FOREIGN KEY |
| started_at | TEXT | NOT NULL，RFC 3339 UTC |
| ended_at | TEXT | RFC 3339 UTC |
| duration_minutes | INTEGER | 当前 schema v2 字段；规划迁移后删除，以 accumulated_seconds 为唯一事实 |
| completed | INTEGER | 当前 schema v2 字段；规划迁移后删除，以 status 为唯一事实 |
| created_at | TEXT | NOT NULL DEFAULT CURRENT_TIMESTAMP |
| status | TEXT | planned / active / paused / recovery_pending / completed / cancelled / interrupted；规划字段 |
| planned_seconds | INTEGER | 本次计划时长；规划字段 |
| accumulated_seconds | INTEGER | 已结算有效秒数；规划字段 |
| last_resumed_at | TEXT | active 区间起点；paused 时为空；规划字段 |
| last_heartbeat_at | TEXT | Sidecar 活跃时最近心跳，用于崩溃恢复边界；规划字段 |
| end_reason | TEXT | user_stop / completed / cancelled / crash_recovery；规划字段 |
| version | INTEGER | 乐观并发版本；规划字段 |

规划迁移通过重建 `focus_sessions` 删除旧 `duration_minutes/completed`，不能让新旧字段并行可写。历史 `completed = 1` 迁为 status=completed；其他历史记录根据 ended_at 映射为 cancelled 或 interrupted；`accumulated_seconds = duration_minutes * 60`。同一迁移版本必须同步更新今日统计查询。迁移后同一数据库同时最多一个未结束 Session。新 Sidecar 发现属于旧进程的 active Session 时先原子转为 recovery_pending；用户再选择计入中断间隔恢复、只计到 last_heartbeat_at 后恢复，或结束为 interrupted。显示剩余时间时使用服务端绝对时间计算，前端 tick 不作为事实；pause/stop/cancel 在事务中结算当前区间，stop 与任务 `actual_minutes` 累计使用同一幂等事务。

#### 本地工作编排规划表（尚未实现）

以下结构必须通过 `003_...` 起的新增递增迁移实现，不得回写已经发布的 `001_initial_schema.sql`。Task 状态 CHECK 扩展时需要在事务内重建相关表，并验证历史数据、外键和索引。

**actors** - 本地责任主体

| 字段 | 类型 | 约束 / 说明 |
|------|------|-------------|
| id | TEXT | PRIMARY KEY (UUID) |
| type | TEXT | owner / person / agent / system |
| display_name | TEXT | NOT NULL |
| status | TEXT | active / inactive |
| is_builtin | INTEGER | owner/system 为 1 |
| adapter_id | TEXT | agent 对应的 `agent_adapters.id`；其他类型为空 |
| capabilities_json | TEXT | 本地能力白名单，不包含凭据 |
| metadata_json | TEXT | 联系备注或非敏感配置 |
| created_at / updated_at | TEXT | RFC 3339 UTC |

约束：仅允许一个 owner；owner/system 不可删除；已经被历史记录引用的 Actor 只能停用。

首次 Actor 迁移必须使用稳定的内置 UUID 幂等创建一个 owner 和一个 system。owner 初始名称使用本地默认值“我”，其 `actors.display_name` 是责任人名称的唯一事实源；旧 localStorage 的 displayName/avatarDataUrl 只迁移为 `app_settings.workspace` 品牌资料，不得改写 owner 名称。由于现有任务全部来自单用户版本，迁移为每条历史 Task 回填 owner Assignment：未完成任务保留活动分派；已完成任务以 `completed_at`（缺失时用 `updated_at`）结束分派。每条回填写 `migration_assignment_backfill` Workflow Event，并标明这是迁移推定，不宣称有更细的历史执行证据。重复运行迁移不得生成第二份内置 Actor、Assignment 或事件。

**agent_adapters** - 本地 Agent 执行器注册（v0.2）

| 字段 | 类型 | 约束 / 说明 |
|------|------|-------------|
| id | TEXT | PRIMARY KEY (UUID) |
| adapter_key | TEXT | UNIQUE 稳定标识 |
| display_name | TEXT | 展示名称 |
| executable_ref | TEXT | 内置执行器 ID 或经文件对话框授权的本地可执行文件引用 |
| manifest_json | TEXT | 版本、入口、声明能力、输入/输出协议和平台要求 |
| status | TEXT | enabled / disabled |
| sandbox_profile | TEXT | ADR 定义的隔离配置标识 |
| last_health_status / last_health_at | TEXT | 最近一次健康检查结果，仅作诊断，不替代运行时检查 |
| created_at / updated_at | TEXT | RFC 3339 UTC |

Adapter 注册信息以该表为事实源；敏感凭据不进入 manifest。每次创建 Agent Run 都重新校验 Adapter 状态、manifest 版本、执行文件和平台隔离能力，不能只依赖上次健康结果。

**inbox_items** - 收件箱项

| 字段 | 类型 | 约束 / 说明 |
|------|------|-------------|
| id | TEXT | PRIMARY KEY (UUID) |
| kind | TEXT | 本地事件类型 |
| title / summary | TEXT | 标题与摘要 |
| source_entity_type / source_entity_id | TEXT | 来源类型与本地资源 ID |
| source_deleted_at | TEXT | 来源被允许删除后的时间；保留最小快照并在 UI 标记来源不存在 |
| source_event_key | TEXT | UNIQUE；调度和重扫去重键 |
| priority | TEXT | P0 / P1 / P2 / P3 |
| status | TEXT | open / tracking / resolved / dismissed |
| resolution_policy | TEXT | manual / all_required_tasks_done |
| due_at | TEXT | 可选截止时间 |
| read_at / triaged_at | TEXT | 展示与分诊时间，独立于主状态 |
| snoozed_until | TEXT | 稍后提醒时间 |
| resolved_at / resolved_by_actor_id | TEXT | 解决记录 |
| resolve_reason | TEXT | owner 手动解决时必填；自动解决为空 |
| resolution_mode | TEXT | derived / manual / forced |
| dismissed_at / dismissed_by_actor_id | TEXT | 忽略记录 |
| dismiss_reason | TEXT | 忽略原因 |
| payload_json | TEXT | 创建时的最小来源快照，不是业务事实源 |
| version | INTEGER | 乐观并发版本 |
| created_at / updated_at | TEXT | RFC 3339 UTC |

`triaged_at` 在首次拆分/关联任务、明确解决或忽略时写入；单纯已读或稍后提醒不算完成分诊。多态来源无法依靠 SQLite 外键统一约束：Inbox Item 处于 open/tracking 时默认阻止来源硬删除；已解决/忽略后如确需删除来源，则将 `source_entity_id` 置空、写入 `source_deleted_at`，保留最小快照并追加 Workflow Event，界面显示“来源已不存在”。

**inbox_item_tasks** - 收件箱项与任务关联

| 字段 | 类型 | 约束 / 说明 |
|------|------|-------------|
| id | TEXT | PRIMARY KEY (UUID) |
| inbox_item_id / task_id | TEXT | 收件箱项与任务 |
| relation_type | TEXT | created / linked |
| is_required | INTEGER | 是否影响收件箱自动解决 |
| linked_by_actor_id | TEXT | 建立关联的 Actor |
| linked_at | TEXT | RFC 3339 UTC |
| unlinked_by_actor_id / unlinked_at | TEXT | 取消关联记录；不硬删除历史关系 |

进度和自动解决只统计 `unlinked_at IS NULL` 的活动关联；对 `(inbox_item_id, task_id)` 建立仅覆盖活动行的唯一索引。重新关联同一 Task 时创建新行，不复用或覆盖旧记录，以保留完整关联历史。

**task_assignments** - 任务分派历史

| 字段 | 类型 | 约束 / 说明 |
|------|------|-------------|
| id | TEXT | PRIMARY KEY (UUID) |
| task_id / actor_id | TEXT | 任务与负责人 |
| role | TEXT | assignee / reviewer |
| assigned_by_actor_id | TEXT | 执行分派的 Actor |
| assigned_at / unassigned_at | TEXT | 生效与结束时间 |
| reason | TEXT | 改派原因 |

Assignment 不设置自己的工作完成状态；同一任务、同一 role 同时只允许一个 `unassigned_at IS NULL` 的活动记录。

**agent_runs** - 本地 Agent 单次执行

| 字段 | 类型 | 约束 / 说明 |
|------|------|-------------|
| id | TEXT | PRIMARY KEY (UUID) |
| task_id / assignment_id / agent_actor_id | TEXT | 执行上下文 |
| triggered_by_actor_id | TEXT | 发起人 |
| parent_run_id / attempt | TEXT / INTEGER | 重试链路 |
| status | TEXT | queued / running / succeeded / failed / cancelled / interrupted |
| input_snapshot_json | TEXT | 脱敏输入快照 |
| output_summary / output_manifest_json | TEXT | 输出摘要与清单 |
| error_code / error_message | TEXT | 结构化错误 |
| queued_at / started_at / finished_at | TEXT | 生命周期时间 |
| idempotency_key | TEXT | 单次执行去重 |
| version | INTEGER | 乐观并发版本 |

重试必须创建新记录，不覆盖失败或中断记录。

**task_artifacts** - 人或 Agent 的本地产出

字段至少包括 `id`、`task_id`、`agent_run_id`、`storage_kind`、`name`、`content_text`、`reference_url`、`relative_path`、`mime_type`、`size_bytes`、`sha256`、`requires_followup`、`produced_by_actor_id`、`recorded_by_actor_id`、`deleted_at`、`created_at`。person 的线下结果由 owner 录入时，前者记录 person、后者记录 owner；本地 Agent 产出必须关联 Agent Run。只有项目交付类产出或 owner 显式标记 `requires_followup = 1` 才能新建后续收件箱项。文件只能写入应用控制的产出目录，数据库只保存相对路径和校验值；外部 URL 只作为不可自动抓取的引用，`file:` 等本地路径必须先导入受控目录。

**workflow_events** - 追加式工作流审计

字段至少包括 `id`、`aggregate_type`、`aggregate_id`、`action`、`actor_id`、`assignment_id`、`agent_run_id`、`request_id`、`previous_json`、`current_json`、`created_at`。普通业务 API 不提供修改或删除历史事件的入口。

**reminders** - 本地提醒调度

字段至少包括 `id`、`source_entity_type`、`source_entity_id`、`title`、`trigger_at`、`status`、`source_event_key`、`created_by_actor_id`、`fired_at`、`inbox_item_id`、`created_at`、`updated_at`。状态为 `scheduled / fired / cancelled`；改期直接更新尚未触发记录并写 Workflow Event。到期后幂等创建一条 Inbox Item 并回写 `inbox_item_id`。第一阶段只支持一次性本地提醒；重复规则在自动化工作包中实现。

**idempotency_keys 增量字段** - 幂等重放契约

在现有表上通过递增迁移增加 `request_hash`、`response_status`、`response_body`、`created_by_actor_id` 和 `expires_at`。key 的作用域至少包含 Actor、HTTP 方法和规范化路径；同一 key 携带不同请求摘要必须返回 `409 CONFLICT`，过期清理不得破坏仍需审计的 Workflow Event。`response_body` 只保存有大小上限且已脱敏的资源响应，不缓存文件、Task Artifact 正文或 Agent 输入输出。

**app_settings** - 版本化非敏感用户设置

字段至少包括 `key`、`value_json`、`version`、`updated_by_actor_id`、`updated_at`。`workspace` key 保存工作区品牌名称和头像引用，不作为 owner 身份；owner 名称只存在于 actors。服务端按设置模块清洗 schema；敏感凭据、Sidecar 会话令牌和 Agent 单次能力令牌不得进入该表。

---

## 7. 数据持久化方案

### 7.1 应用数据目录

所有业务数据保存在 Tauri Path API 返回的应用专属目录中，不依赖当前工作目录，也不写入安装目录。具体物理路径由操作系统和应用 Bundle Identifier 决定，业务代码不得硬编码路径。

```text
appDataDir/
├─ opc-workspace.db          # SQLite 主数据库
├─ attachments/              # 用户附件
├─ artifacts/                # 人或本地 Agent 的任务产出
├─ invoices/                 # 生成的发票 PDF
├─ backups/                  # 本机操作性快照
└─ config/                   # 非敏感应用配置

appLogDir/
└─ opc-workspace.log         # 脱敏运行日志
```

- 应用升级只替换程序文件，不覆盖 `appDataDir`
- 卸载流程默认不主动删除业务数据；彻底删除数据必须由用户执行独立的明确操作
- API 密钥、Sidecar 会话令牌等敏感信息不写入普通配置文件，持久凭据使用操作系统安全存储

### 7.2 SQLite 生命周期与迁移

1. Tauri 启动时先获取单实例锁，避免多个 Sidecar 同时写同一数据库
2. Tauri 将数据库路径和动态端口配置传给 Go Sidecar；随机会话令牌通过继承环境或进程管道传递，避免暴露在进程命令行
3. Go Sidecar 建立连接后启用外键、WAL 和 `busy_timeout`，然后执行版本化数据库迁移
4. 每次破坏性迁移前自动创建一致性备份；迁移失败时停止启动并进入恢复界面
5. 应用退出时先停止接收新请求，等待进行中的写事务结束，执行必要 checkpoint 后再关闭数据库和 Sidecar
6. 数据库版本记录在 `schema_migrations` 表中；应用版本、Sidecar 版本和数据库版本必须建立兼容关系

### 7.3 备份策略

版本边界：v0.1 必须交付手动一致性备份、迁移前备份、基础 JSON 导出、校验和、临时库验证和原子恢复，作为本地数据发布闸门；v0.3 只增加可配置计划、外部目录、保留策略、CSV 映射和高级导入工具，不重复定义基础恢复能力。

1. **v0.1 一致性快照基础**
   - 使用 SQLite Online Backup API 或 `VACUUM INTO` 创建一致性快照，不直接复制正在使用的 WAL 数据库文件
   - 支持用户手动创建和破坏性迁移前自动创建；v0.1 不提供每日调度
   - 快照写入 `appDataDir/backups/`，每个备份生成包含 app/API/schema 版本、文件清单和 SHA-256 的 manifest
   - 创建后至少执行一次 `integrity_check`、`foreign_key_check` 和可打开性验证

2. **v0.1 基础手动导出**
   - 随时导出带 `schema_version` 的完整 JSON 数据包，覆盖任务、Actor、分派、收件箱、提醒和审计事实
   - 可导出 SQLite 一致性快照，不直接暴露正在写入的主数据库文件
   - 导出附件和 Task Artifact 时生成包含数据库、附件、产出和 manifest 的归档文件

3. **v0.1 恢复与基础导入**
   - 恢复前自动创建当前状态快照
   - 导入前校验格式、版本、校验和及可用磁盘空间
   - 在临时数据库完成验证和迁移后再原子替换正式数据库
   - 恢复失败不得覆盖当前可用数据，并提供可定位的脱敏错误日志

4. **v0.3 可配置备份与高级导入**
   - 增加每日首次满足条件时执行、错过计划后的启动补偿、可配置保留策略和最近 30 份默认值
   - 引导用户选择独立外部备份目录，用于防范应用数据目录或磁盘损坏
   - 增加任务、客户、财务等 CSV 导出/映射导入和冲突预览
   - 提供周期性可恢复性抽检、失败提醒和历史执行记录

### 7.4 数据安全

- 当前阶段所有核心业务、Actor、任务分派、提醒、产出和 Agent Run 均仅保存在本地，不主动访问线上服务
- Go Sidecar 仅绑定 `127.0.0.1` 动态端口，并校验由 Tauri 生成的启动期随机会话令牌
- 数据目录使用当前操作系统用户权限，日志不得记录令牌、完整客户信息、发票内容或第三方连接器请求正文
- person Actor 不包含登录凭据，也不会收到远程任务；本地 Agent 必须默认禁网并只访问单次 Run 明确授权的资源，跨平台沙箱/网络阻断未验证通过时正式执行保持禁用
- 若未来引入更新检查、第三方连接器、远程模型或多人协作，必须另行评审网络访问、身份、权限、同步、密钥和数据外发边界，并由用户显式启用
- 应用锁定用于阻止界面访问；数据库静态加密使用 SQLCipher，计划在 MVP 后提供
- 备份归档可设置加密密码；密码遗失时明确提示无法恢复

---

## 8. 部署与分发

### 8.1 桌面应用分发

通过 Tauri Bundler 打包为各平台原生安装包。每个安装包必须同时包含 React 构建产物和对应平台/架构的 Go Sidecar，最终用户无需安装 Go、Node.js、Rust 或 Docker：

| 平台 | 格式 |
|------|------|
| Windows | .exe 安装程序、.msi 企业安装包 |
| macOS | .dmg 镜像（Universal：Intel + Apple Silicon） |
| Linux | .deb（Debian/Ubuntu）、.rpm（Fedora/RedHat）、.AppImage |

- Windows 安装包执行代码签名；macOS 应用执行签名和公证；Linux 发布 SHA-256 校验值
- CI 至少构建 `windows-x86_64`、`darwin-x86_64`、`darwin-aarch64`、`linux-x86_64`
- 支持的操作系统必须具备兼容的系统 WebView；Windows 安装器需检测 WebView2，并明确在线引导与离线安装策略
- 安装包发布前必须在不含开发工具的干净系统中验证 Sidecar 启动、数据库创建、备份与卸载后数据保留行为

### 8.2 首次启动流程

```
1. 用户安装并启动桌面应用
       ↓
2. Tauri 获取单实例锁并初始化 appDataDir / appLogDir
       ↓
3. 生成随机会话令牌，启动安装包内置 Go Sidecar
   启动配置：数据库路径、日志目录、127.0.0.1:0；令牌通过继承环境或进程管道传递，不出现在命令行
       ↓
4. Sidecar 初始化 SQLite、执行版本化迁移并输出实际监听端口
       ↓
5. Tauri 调用 /health 完成就绪检查
   ├─ 健康 → WebView 加载内置 React 页面，注入运行期 API 地址
   └─ 超时 → 显示恢复页面，可重试、打开日志或从备份恢复
       ↓
6. 用户开始使用，核心功能从首次启动起即可离线运行
```

首次启动不下载业务运行时或后端镜像。v1.6 当前阶段不提供线上更新或第三方连接；安装、升级和核心使用均可在离线环境完成。

### 8.3 更新机制

Tauri 桌面壳、React 前端和 Go Sidecar 使用同一个应用版本并作为单个签名更新包发布，避免组件版本漂移。当前无线上服务阶段先支持用户主动选择或手动安装的签名离线更新包：

1. 用户在本地选择签名更新包，应用验证签名、版本、平台和架构
2. 用户确认更新后，应用先完成当前写事务并创建更新前一致性备份
3. Tauri 正常关闭 Go Sidecar，再由安装程序替换桌面壳、前端资源和 Sidecar
4. 新版本首次启动时校验应用/Sidecar 版本并执行数据库迁移
5. 迁移或健康检查失败时进入恢复界面；在数据库版本兼容时允许回退应用，否则从更新前备份恢复

`appDataDir` 独立于程序安装目录，正常更新不会覆盖业务数据。更新签名私钥必须存储在受控 CI 密钥系统中并制定轮换与灾难恢复方案。未来若增加基于 HTTPS 的在线 Updater，必须单独更新 ADR、网络权限、失败回退和用户开关，不能把它作为当前本地服务的隐式依赖。

### 8.4 系统托盘功能

- 最小化到托盘而非关闭
- 托盘菜单：快速新建任务、开始/暂停专注、显示主窗口、设置、退出
- 托盘图标显示专注状态（空闲/专注中/休息中）
- 托盘通知气泡（本地任务、发票、收件箱和系统维护提醒）
- 关闭主窗口默认仅隐藏窗口并保留 Sidecar；用户点击“退出”时正常关闭 Sidecar 和数据库连接
- Sidecar 异常退出时托盘显示错误状态，并提供一次自动重启和手动打开诊断页入口

### 8.5 全局快捷键

| 快捷键 | 功能 |
|--------|------|
| ⌘K / Ctrl+K | 打开命令面板 |
| ⌘N / Ctrl+N | 新建任务 |
| ⌘⇧F / Ctrl+Shift+F | 开始/暂停专注 |
| ⌘1-9 / Ctrl+1-9 | 快速切换页面 |
| ⌘W / Ctrl+W | 最小化到托盘 |

---

## 9. MVP 范围与迭代计划

### 9.1 MVP（v0.1）范围

**目标**：交付一人公司日常工作的本地闭环；Actor 与新版收件箱扩大了原 v0.1 范围，完成详细任务拆分后重新估算周期，不沿用旧“8 周”承诺。

**包含功能**：

| 模块 | MVP 目标功能 | 当前状态（2026-08-27） |
|------|--------------|--------------------------|
| 今日工作台 | 三栏布局、今日任务列表、手动排序、逾期及临期提示、专注卡片、右侧概览面板 | **部分完成**：布局、真实任务/今日统计、专注卡片和基础概览已接通；按日期筛选、拖拽排序、收入与客户动态待实现 |
| 任务管理 | 完整 CRUD、父子任务、状态流转、标签、项目关联、完成条件、人工验收、列表视图、搜索和快捷键 | **部分完成**：列表、新建、读取、基础状态更新、后端删除和关键词搜索已实现；完整编辑、详情、父子任务、验收、前端删除、标签、项目选择、筛选和批量操作待实现 |
| 项目管理 | 项目卡片、状态流转、项目进度、项目详情（任务列表） | **页面骨架**：可导航空状态已实现，CRUD 与详情未开始 |
| 客户管理 | 客户列表表格、客户详情、基本 CRUD | **页面骨架**：可导航空状态已实现，CRUD 与详情未开始 |
| 收件箱与人工编排 | 本地 Actor 基础、事件受理、已读/稍后、任务拆分/关联、人工分派、验收/返工、审计和自动解决 | **页面骨架**：空状态已实现；Actor、Inbox Item、Assignment、拆分、提醒和审计均未开始 |
| 专注模式 | 番茄钟、环形进度、工时记录、连续天数统计、暂停本应用通知、系统专注模式引导 | **部分完成**：全局计时、专注/休息循环、设置、自动衔接、重置和提示音已实现；会话入库、任务工时、连续天数和通知控制待实现 |
| 全局功能 | 左侧导航、系统托盘、全局快捷键、自动启动、Go Sidecar 生命周期和健康检查 | **部分完成**：导航、WebView 内快捷键、单实例、Sidecar 生命周期和健康检查已实现；托盘、系统全局快捷键、自动启动待实现 |
| 数据持久化 | Tauri `appDataDir`、SQLite 迁移、手动/迁移前一致性备份、基础 JSON 导出与原子恢复 | **部分完成**：正式/开发数据隔离、WAL、外键、迁移入口和 schema v2 已实现；一致性备份、恢复和基础导入导出待实现；每日计划和高级导入归 v0.3 |

**MVP 不包含**（后续版本）：
- 看板视图（任务看板、项目看板）
- 内容日历
- 自动化规则引擎
- 白噪音
- 网站屏蔽
- 数据加密（SQLCipher）
- 多币种支持
- 移动端伴侣 App
- 云同步（可选的端到端加密同步）
- AI 助手与模型运行时接入
- 本地知识库、文档索引与带来源检索
- 客户回访计划、记录和提醒
- 收入/支出账本、统计、发票业务和 PDF
- 本地 Agent Adapter、Agent Run 和自动执行；v0.1 先完成纯本地人工受理、分派和验收闭环
- 多人账号、远程任务领取、线上协作、远程通知和任何形式的云端工作流

### 9.2 迭代计划

| 版本 | 核心功能 | 开发周期 |
|------|----------|----------|
| **v0.1 MVP** | 今日、完整任务纵切、项目、基础客户、专注持久化、本地 Actor、收件箱人工受理/拆分/分派/验收、备份恢复和桌面可靠性 | 完成详细任务拆分后重新估算 |
| **v0.2 本地编排版** | 本地 Agent Adapter 与 Run 生命周期、产出、取消/重试、人工审核返工、任务看板和预设自动化 | 待 v0.1 完成后估算 |
| **v0.3 规划增强版** | 路线图、内容日历、自动备份配置、数据导入工具、快捷键自定义和统计增强 | 待 v0.2 完成后估算 |
| **v0.4（后续业务版）** | 客户回访计划与提醒、收入/支出账本、财务统计、发票 CRUD 与 PDF | 待 v0.1 完成后估算 |
| **v1.0** | 多币种支持、数据加密、自动化规则自定义、API 开放平台、性能优化、跨平台测试完善 | 6 周 |
| **待开发（版本待定）** | AI 助手、本地模型 Adapter、本地知识库、文档导入与索引、带来源检索、权限/隐私控制和质量评测 | 完成核心 MVP 与安全存储后单独估算 |

### 9.3 MVP 技术验收标准

- [ ] Windows/macOS/Linux 三平台安装包均内置正确架构的 Go Sidecar，可在未安装开发工具和 Docker 的干净系统运行
- [ ] 已完成初始化后的冷启动 P95 < 2 秒（参考设备连续测试至少 20 次，从点击图标到界面可交互）
- [ ] 首次启动无需下载业务运行时，数据库初始化和 Sidecar 就绪时间 P95 < 5 秒
- [ ] 安装包体积 < 30MB（不含操作系统 WebView 运行时）
- [ ] 稳态运行内存 P95 < 200MB（Tauri + WebView + Go Sidecar 合计）
- [ ] 生产环境 Sidecar 仅绑定 `127.0.0.1` 动态端口，未携带有效会话令牌的请求被拒绝
- [ ] 应用正常退出后不遗留 Sidecar 进程；Sidecar 异常退出时可诊断并按策略重启
- [ ] 应用升级、Sidecar 重启和程序文件替换后，`appDataDir` 中数据完整保留
- [ ] WAL 模式下生成的备份可通过校验，并能在临时数据库中完整恢复
- [ ] 首次启动后的所有核心功能在断网环境可用
- [ ] 离线更新只接受签名有效、平台/架构正确且版本兼容的安装包；在线 Updater 不属于当前阶段
- [ ] ⌘K 命令面板可搜索所有页面和任务
- [ ] 专注模式番茄钟计时准确，工时自动记录到任务
- [ ] 单机仅存在一个 owner；person 分派明确为本地责任记录，不产生登录、发送或同步行为
- [ ] 收件箱拆分、任务关联、分派和审计在同一事务中完成，失败不遗留部分数据
- [ ] 同一来源事件跨扫描和应用重启不重复生成；收件箱解决状态由必需任务派生且可追溯

---

## 10. 实施基线、开发流程与实现追踪

> 状态截止：2026-08-27。当前版本是可运行、可扩展的 v0.1 基座，不代表第 9.1 节的完整 MVP 或 v1.6 新增的 Actor/收件箱规划已经交付。

### 10.1 文档口径与状态定义

| 状态 | 定义 |
|------|------|
| 已完成 | 当前约定范围已有真实入口和实现，并完成与风险相称的测试或构建验证 |
| 部分完成 | 主链路或基座可运行，但同一模块仍有明确的 MVP 能力未接通 |
| 页面骨架 | 路由、原型样式和真实空状态可访问，但按钮或业务数据链路尚未实现 |
| 未开始 | 仅存在产品需求或技术设计，仓库中没有可用实现 |

当前实现事实以仓库代码、测试和运行结果为准。PRD 中描述的目标接口、插件、迁移或页面，不因出现在文档中就视为已交付。开发时优先使用仓库相对路径；2026-08-27 本次核对的实际 Git 根为 `/Users/tao/Documents/WorkSpace/opc-workspace`，HEAD 为 `471f814`。

| 基线项 | 当前实现 |
|--------|----------|
| 前端 | React 18.3、TypeScript 5.9、Vite 7、React Router 6、TanStack Query 5、Zustand 5、Lucide、Tailwind CSS v4 构建能力及集中式 `styles.css` |
| 桌面 | Tauri 2、Rust 1.85、系统 WebView、shell 与 single-instance 插件 |
| Sidecar | Go 1.22+、Gin、GORM、纯 Go SQLite 驱动；构建时 `CGO_ENABLED=0` |
| API / Schema | API v1；SQLite schema v2 |
| 数据默认值 | 开发数据库默认空白，不自动注入 demo 业务数据 |
| 明确边界 | 当前代码不使用 Docker，也未实现 Actor、工作编排、Agent 执行、AI 助手、知识库、客户回访或收入/支出/发票业务；v0.1 规划中的 person 只做本地责任记录，线上账号、云同步和远程协作均不在当前范围 |

### 10.2 单项任务统一开发流程

每个开发任务按以下顺序推进，并在任务完成后同步更新本章：

1. **路径与变更保护**：确认实际仓库、Git 状态、目标文件、运行端口和数据目录；保留 PRD、模块文档、当前 React UI、用户已有修改和真实数据。
2. **需求与交互映射**：确定对应 PRD 条目、模块文档、版本边界、用户入口和验收条件；现有页面修改以当前 React 组件、`styles.css` 和实际渲染为基线，新模块先在模块文档中形成可验证的界面契约。
3. **数据契约设计**：涉及持久化时先定义表字段、迁移、API 输入输出、错误码和前端类型。PRD 与原型冲突时，以 PRD 的业务默认值为准并记录决定。
4. **后端纵切实现**：新增递增迁移、Go model、handler、路由、输入校验、事务和定向测试；不得修改已发布迁移。
5. **前端纵切实现**：在 `client.ts` 做字段规范化和错误转换，在 `hooks.ts` 管理 Query/Mutation，再实现页面、弹窗和交互状态。
6. **完整状态处理**：同时实现加载、成功、空数据、错误、重试和不可用状态；禁止以 demo 死数据伪装业务完成。
7. **视觉与交互核对**：完成代表性金样后再复用到其他页面；检查键盘、焦点、禁用态、窄屏和路由切换。
8. **验证与留痕**：运行格式、类型、Go/前端测试、Web 构建和可行的 Tauri 检查；更新 PRD 的状态、实现路径、验证证据、限制和下一步。

单项任务只有满足以下条件才可标记“已完成”：有真实可用入口；数据去向明确；加载/空/错误状态齐全；关键逻辑有定向测试；类型检查和相关构建通过；没有把未实现按钮或规划接口写成可用功能。

### 10.3 技术实现约定

#### 10.3.1 开发启动与数据流

浏览器联调：

```powershell
pnpm dev:web
```

桌面联调：

```powershell
pnpm dev
```

`scripts/dev.mjs` 的实际流程：

1. 检查 Go 和 pnpm，创建 `.local/dev-data/`。
2. 以 `--dev --db .local/dev-data/opc-workspace.db --port 9876` 启动 Sidecar。
3. 注入开发会话令牌和精确 Origin 白名单，轮询带鉴权的 `/health`。
4. Sidecar 就绪后启动 Vite；Vite 固定监听 `127.0.0.1:1420`，代理 `/api` 和 `/health`。
5. `pnpm dev` 再启动 `tauri dev`，通过 `OPC_SIDECAR_URL` 复用同一个开发 Sidecar，避免重复打开数据库。
6. 必要子进程异常退出时，统一脚本关闭其余进程树。

开发数据固定在 `.local/dev-data/opc-workspace.db`，正式桌面数据固定在 Tauri `appDataDir`，两者不得互用。默认启动不传 `--seed`；只有显式 `--dev --seed` 才能创建幂等测试数据。

#### 10.3.2 API 开发约定

新增业务 API 的落地顺序为：迁移与 model → Go handler → `router.go` 注册 `/api/v1` → Go 集成测试 → 前端 model → `client.ts` → TanStack Query hook → 页面交互测试。

- Sidecar 只监听 `127.0.0.1`；生产模式使用端口 `0` 获取动态端口。
- 生产请求，包括 `/health`，必须携带 Bearer 会话令牌。
- v0.2 Agent Runtime 不接受上述 WebView 会话令牌；必须使用 ADR 定义的进程管道或专用路由与单次能力令牌。
- 浏览器请求必须携带匹配精确白名单的 Origin；不允许通配符。
- 请求 ID 使用 UUID；错误体统一为 `{code, message, request_id}`。
- JSON 请求限制为 1 MiB，拒绝未知字段和多余 JSON 值。
- API 时间戳使用 RFC 3339 UTC，纯日期使用 `YYYY-MM-DD`，金额使用最小货币单位整数。
- 写操作使用事务；可重试创建操作使用 `Idempotency-Key`。

#### 10.3.3 前端状态分层

| 状态类型 | 当前实现方式 | 示例 |
|----------|--------------|------|
| 服务端事实 | TanStack Query | 健康检查、任务、今日统计 |
| 短期 UI 状态 | Zustand | 命令面板、新建任务、设置弹窗开关 |
| 本地配置 | Zustand persist + `localStorage` | 个人资料、默认首页、右栏、主题、减少动效和专注参数；规划迁移到版本化本地配置 |
| 运行态 | Zustand 内存状态机 | 当前专注阶段、剩余秒数、已完成循环 |

API 单次请求超时 8 秒；健康检查每 15 秒刷新；任务和今日统计缓存 10 秒且失败重试 2 次。Tauri 前端通过 `sidecar_status` 等待 `starting → ready`；当前全局壳没有展示 health 结果，只有部分业务页显示服务不可用，后续需补全局状态条、诊断和重连入口。

#### 10.3.4 SQLite 迁移约定

迁移文件位于 `services/sidecar/internal/database/migrations/`，通过 Go `embed` 编入 Sidecar。新增结构只能追加如 `003_add_xxx.sql` 的递增文件：

1. 启动时创建并读取 `schema_migrations`。
2. 按版本升序逐个事务执行迁移。
3. 成功后记录版本、文件名和执行时间。
4. 数据库包含未知版本或同版本文件名不一致时拒绝启动。
5. 每个迁移必须补充数据库测试，不得回写已发布迁移。

当前 `001_initial_schema.sql` 建立核心业务表和索引；`002_remove_default_demo_seed.sql` 只清理旧版本固定 UUID 的 demo 记录，不删除用户创建的数据。数据库使用单物理连接，并启用外键、WAL 和 5000 ms `busy_timeout`；退出时执行 `wal_checkpoint(TRUNCATE)`。

### 10.4 当前基座任务清单

| 任务 | 当前状态 | 本次基线范围 |
|------|----------|--------------|
| T-01 工程目录与统一脚本 | 已完成 | pnpm workspace、统一启动、Sidecar 构建脚本、开发数据隔离 |
| T-02 Tauri 桌面壳与 Sidecar 生命周期 | 部分完成 | 窗口、单实例、动态端口、令牌、ready/health、退出清理 |
| T-03 Go 健康检查与 API 基础 | 已完成 | 版本化路由、安全中间件、统一错误和健康检查 |
| T-04 SQLite 初始化与迁移 | 已完成 | schema v2、PRAGMA、嵌入式迁移、demo 清理 |
| T-05 前端 AppShell 与原型复刻 | 已完成 | Linear 深色三栏框架、导航、响应式和公共组件 |
| T-06 今日工作台 | 部分完成 | 真实任务/统计、专注卡片、基础概览和反馈状态 |
| T-07 任务管理纵向闭环 | 部分完成 | 列表、新建、读取、状态更新、删除 API、前端搜索 |
| T-08 项目管理 | 页面骨架 | 路由、标题、新建入口外观和空状态 |
| T-09 客户管理 | 页面骨架 | 路由、标题、新建入口外观和空状态 |
| T-10 收入、支出与发票 | 页面骨架 | 收入/发票路由和空状态已存在；支出、业务 API 与统计未开始，整体属于 v0.4 |
| T-11 收件箱与工作编排中心 | 页面骨架 | 当前仅有路由、全部已读入口外观和空状态；新版对象、事件和任务编排均未实现 |
| T-12 专注设置与全局计时 | 部分完成 | 设置持久化、专注/休息循环、提示音、跨路由计时；未绑定任务或写入工时 |
| T-13 命令面板与基础反馈 | 部分完成 | WebView 快捷键、页面/任务搜索、加载/错误/空状态 |
| T-14 测试、构建与桌面验收 | 部分完成 | Web/Go 自动测试与构建已接入；桌面完整编译和安装包验收受环境限制 |
| T-15 AI 助手 | 未开始 | 已登记本地模型 Adapter、只读上下文、安全存储和质量闸门，尚无代码 |
| T-16 本地知识库 | 未开始 | 已登记导入、FTS 检索、引用与删除要求，尚无数据结构或页面 |
| T-17 客户回访 | 未开始 | 已登记回访计划、到期提醒、结果记录和后续开发顺序，属于 v0.4 |
| T-18 本地 Actor 与任务分派 | 未开始 | owner/person/agent/system、Assignment、停用与分派历史规划，尚无迁移或代码 |
| T-19 本地 Agent 执行 | 未开始 | Adapter、Run、产出、取消/重试、验收/返工和崩溃恢复规划，属于 v0.2 |

#### 10.4.1 T-01 工程目录与统一脚本

- **需求映射**：4.4、8.1、9.1。
- **用户/开发者流程**：安装依赖后执行 `pnpm dev:web` 查看浏览器版，或执行 `pnpm dev` 联调桌面版。
- **实现方法**：根 `package.json` 统一开发、测试和构建命令；`scripts/dev.mjs` 按 Sidecar → Vite → Tauri 顺序启动并做就绪检查；`scripts/build-sidecar.mjs` 读取 `rustc --print host-tuple`，使用 `CGO_ENABLED=0` 构建 `opc-sidecar-<target-triple>`。
- **关键路径**：`package.json`、`pnpm-workspace.yaml`、`scripts/dev.mjs`、`scripts/build-sidecar.mjs`。
- **验证/剩余**：历史记录中浏览器开发链路运行通过，本次文档更新未复验；CI、多平台矩阵和发布流水线未建立。

#### 10.4.2 T-02 Tauri 桌面壳与 Sidecar 生命周期

- **需求映射**：4.1、4.2、7.2、8.2。
- **用户流程**：桌面应用启动后自动准备数据目录并连接本地服务；第二次启动复用单实例并聚焦主窗口；退出时先优雅关闭 Sidecar。
- **实现方法**：生产配置通过 `externalBin` 内置 Go Sidecar，开发配置通过 `OPC_SIDECAR_URL` 连接外部 Sidecar。Rust 创建 `appDataDir`、`appLogDir` 及附件/发票/备份/配置目录，生成两个 UUID 拼接的会话令牌，以端口 `0` 启动 Sidecar，解析 stdout 的单行 ready JSON，并强制校验 `http://127.0.0.1:<非零端口>`。健康检查成功后通过 `sidecar_status` 向前端暴露连接状态和版本。退出时写入 `shutdown\n`，最多等待 7 秒，超时后终止精确子进程。
- **关键路径**：`apps/desktop/src-tauri/src/lib.rs`、`apps/desktop/src-tauri/src/sidecar.rs`、`tauri.conf.json`、`tauri.dev.conf.json`。
- **验证/剩余**：ready 解析、loopback 校验和状态序列化已有 Rust 单元测试；系统托盘、原生通知、自动启动、签名离线更新、OS 全局快捷键、异常自动重启和恢复页未实现；在线 Updater 不属于当前阶段。

#### 10.4.3 T-03 Go 健康检查与 API 基础

- **需求映射**：4.1、4.2、附录 C。
- **用户流程**：前端先确认 `/health`，再调用 `/api/v1`；服务不可用时显示可重试错误，而不是展示伪造业务数据。
- **实现方法**：Gin 路由依次接入 request ID、Origin、Bearer 鉴权、访问日志和 panic recovery 中间件。`/health` 返回应用、API 和 schema 版本；任务和今日统计路由挂载在 `/api/v1`。Sidecar 只绑定 IPv4 loopback，支持动态端口，并在 stdout 输出机器可读 ready 事件。
- **关键路径**：`services/sidecar/cmd/server/main.go`、`internal/api/router.go`、`middleware.go`、`errors.go`、`json.go`。
- **验证/剩余**：鉴权、Origin、健康检查、错误和主任务链路已有 Go 集成测试；WebSocket、OpenAPI、文件/PDF 和业务调度服务未实现。

#### 10.4.4 T-04 SQLite 初始化、迁移与开发数据隔离

- **需求映射**：6、7、9.1。
- **用户流程**：首次启动自动创建数据库并迁移；后续启动只执行尚未应用的版本；开发和正式数据互不影响。
- **实现方法**：GORM 使用纯 Go SQLite 驱动，连接池固定为一个物理连接；打开后启用外键、WAL 和 5 秒 busy timeout。SQL 迁移嵌入二进制并逐个事务执行。schema v1 建立 clients、projects、tasks、tags、task_tags、invoices、focus_sessions、idempotency_keys 和索引；schema v2 精确删除旧 demo 固定 ID。开发数据库位于 `.local/dev-data/`，生产数据库位于 Tauri `appDataDir`。
- **关键路径**：`internal/database/database.go`、`migrate.go`、`migrations/001_initial_schema.sql`、`migrations/002_remove_default_demo_seed.sql`。
- **验证/剩余**：迁移、PRAGMA、未知版本防护、demo 清理和 seed 幂等已有测试；破坏性迁移前备份、一致性快照、恢复、导入导出和校验仍未实现。

#### 10.4.5 T-05 前端 AppShell、原型复刻与基础页面

- **需求映射**：5.1–5.9、附录 A/B。
- **用户流程**：用户通过左侧导航访问今日、任务、项目、客户、收入、发票、收件箱和专注；右侧持续显示专注和业务概览。
- **实现方法**：React Router 负责页面路由；`AppShell` 固定 220 px 左栏、弹性主内容和 280 px 右栏，窄屏时隐藏右栏并收缩导航。历史原型风格已经沉淀到当前 React 组件和 `styles.css`，后续以实际渲染为视觉基线；公共结构复用 `PageHeader`、`Modal`、`TaskList` 和反馈组件。
- **关键路径**：`apps/web/src/App.tsx`、`components/AppShell.tsx`、`Sidebar.tsx`、`RightOverview.tsx`、`styles.css`。
- **验证/剩余**：全部规划路由可访问；今日是当前金样，项目、客户、收入、发票和收件箱尚未完成原型中的业务密度与交互。

#### 10.4.6 T-06 今日工作台

- **需求映射**：5.1。
- **用户流程**：进入首页后读取本地任务和今日统计，可新建任务、完成/恢复任务、开始/暂停专注并打开专注设置。
- **实现方法**：`TodayPage` 用用户本地日期请求 `/api/v1/stats/today`，同时读取任务 Query；统计条使用后端真实值，任务列表和专注卡共享全局状态。加载时显示骨架，空库显示首个任务入口，任务或统计请求失败时分别显示重试。
- **关键路径**：`apps/web/src/pages/TodayPage.tsx`、`apps/web/src/components/RightOverview.tsx`、`apps/web/src/api/hooks.ts`。
- **当前限制**：页面当前将全部未完成任务按 API 顺序取前 3 项作为“今天”、第 4–6 项作为“本周”，尚未真正按 `planned_date` 过滤；日期切换、拖拽排序、逾期筛选、真实收入和客户动态未实现。

#### 10.4.7 T-07 任务管理纵向闭环

- **需求映射**：5.2、附录 C。
- **用户流程**：用户可查看 SQLite 中的任务，按前端关键词搜索，打开新建弹窗，并在列表中完成或恢复任务。
- **实现方法**：Go 列表接口支持分页、状态、优先级、项目、计划日期、关键词和白名单排序；创建接口校验字段和项目外键，支持 `Idempotency-Key`；获取、状态更新和删除使用 UUID 校验。前端 `client.ts` 负责 snake_case → camelCase 规范化，Mutation 成功后使任务和今日统计缓存失效。
- **关键路径**：`services/sidecar/internal/api/tasks.go`、`apps/web/src/api/client.ts`、`apps/web/src/api/hooks.ts`、`apps/web/src/pages/TasksPage.tsx`、`apps/web/src/components/NewTaskModal.tsx`、`apps/web/src/components/TaskList.tsx`。
- **当前限制**：`PATCH /tasks/:id` 与 `PATCH /tasks/:id/status` 当前都只更新状态；前端没有完整编辑、删除、详情、项目选择、标签、筛选器、拖拽排序和批量操作。

#### 10.4.8 T-08 项目管理

- **需求映射**：5.3。
- **当前状态**：页面骨架；`projects` 表已存在，但 API 和业务交互未实现。
- **下一纵切流程**：先补项目 model/API 和状态流转测试，再增加前端类型、Query/Mutation、新建/编辑弹窗、卡片网格和项目详情；进度必须由关联任务统计派生，金额使用最小货币单位整数。
- **关键路径**：当前页面为 `apps/web/src/pages/ProjectsPage.tsx`；未来后端放入 `services/sidecar/internal/api/projects.go`，新增迁移只在表结构变化时创建。
- **验收要求**：新建按钮必须接真实持久化；卡片、详情、加载、空、错误和重试同时可验证后，才能从“页面骨架”升级。

#### 10.4.9 T-09 客户管理

- **需求映射**：5.4。
- **当前状态**：页面骨架；`clients` 表已存在，但 API、表格和详情未实现。
- **下一纵切流程**：实现客户 CRUD、输入校验和删除约束，再接前端表格、新建/编辑、详情、项目/发票关联和活动时间线。累计收入在 v0.4 只从 `confirmed` Financial Entry 聚合，paid Invoice 只用于关联与对账，不在客户记录中重复存储。
- **关键路径**：当前页面为 `apps/web/src/pages/ClientsPage.tsx`；未来 API 建议放入 `internal/api/clients.go`。
- **验收要求**：真实空状态、创建、编辑、查询、受关联约束的删除和错误恢复均通过测试。

#### 10.4.10 T-10 收入、支出与发票

- **需求映射**：5.5。
- **版本归属**：v0.4 后续业务版，不属于 v0.1。收入和发票目前只有页面骨架；`invoices` 表已存在，支出表、业务 API、统计和 PDF 均未实现。
- **下一纵切流程**：先新增 `financial_entries` 递增迁移与收入/支出 CRUD，再实现按月份、分类、客户和项目的净现金流聚合；之后接入发票 CRUD、状态流转和 `/stats/income`。所有金额只以 `amount_minor` 整数计算，并用独立 `type` 字段区分收入与支出。
- **实现约束**：前端负责货币格式化，后端负责统计口径；PDF 作为独立服务写入 `appDataDir/invoices/`，生成失败不得改变发票状态。右侧概览和 KPI 只能使用真实聚合结果。
- **关键路径**：`apps/web/src/pages/IncomePage.tsx`、`apps/web/src/pages/InvoicesPage.tsx`、`services/sidecar/internal/database/migrations/001_initial_schema.sql`。
- **验收要求**：收入、支出、净现金流、删除确认、筛选、CSV 导出、发票状态和 PDF 分别通过测试后再标记完成。

#### 10.4.11 T-11 收件箱与工作编排中心

- **需求映射**：5.6。
- **当前状态**：页面骨架；尚无 Actor、Inbox Item、Assignment、提醒、审计或 Agent Run 表/API，“全部标为已读”按钮暂无业务行为。
- **对象边界**：Inbox Item 管来源、分诊、已读、稍后和解决策略；Task 是唯一可执行工单；Assignment 管责任历史；Agent Run 管单次本地执行；Task Artifact 管产出；Workflow Event 管审计。
- **分阶段纵切**：
  1. **T-11A 收件箱数据契约**：在 T-18 的 Actor/Task/Assignment/审计基础上新增 Inbox Item、关联表和 Reminder 迁移；T-11 不重复拥有 Actor 或 Task 状态迁移。
  2. **T-11B 人工受理**：实现列表、详情、已读/全部已读、稍后提醒、手动提醒、解决、忽略和重新打开。
  3. **T-11C 拆分与分派**：原子创建/关联父子任务、标记必需任务、建立 Assignment，并由关联任务派生进度和自动解决。
  4. **T-11D 本地 Agent**：在 v0.2 接入已注册本地 Adapter、Agent Run、产出、取消/重试、人工验收、返工和中断恢复。
  5. **T-11E 事件源**：v0.1 先接显式 follow-up 的项目产出、任务临期/阻塞和系统故障；v0.2 由内置自动化投影器作为 Agent 失败及未被现有 Inbox 跟踪验收项的唯一生产者；发票、客户回访和项目里程碑随对应业务模块启用。
- **关键路径**：当前页面为 `apps/web/src/pages/InboxPage.tsx`。
- **验收要求**：纯离线完整运行；同一事件不重复；拆分失败全事务回滚；person 明确不发送/同步；Agent 成功不能跳过验收；返工保留历史 Run/产出；Actor 停用不破坏历史；并发旧写入返回冲突。

#### 10.4.12 T-12 专注设置与全局计时

- **需求映射**：5.7。
- **用户流程**：用户可从今日页、专注页或命令面板打开设置，调整时长、循环、自动开始和提示音；保存后各页面共享同一计时器。
- **实现方法**：`store/settings.ts` 使用 Zustand persist 保存个人资料、通用、外观和专注设置并清洗越界值；`SettingsModal` 支持草稿预览、取消、恢复默认和保存。`store/focus.ts` 实现专注/休息状态机、循环计数、自动衔接、暂停、重置和完成状态；全局 `FocusTicker` 每秒推进状态，因此普通路由切换不会中断。阶段结束时使用短 WebAudio 振荡器提示，并安全处理不可用或被系统阻止的情况。
- **关键路径**：`apps/web/src/store/settings.ts`、`apps/web/src/store/focus.ts`、`apps/web/src/components/SettingsModal.tsx`、`apps/web/src/components/FocusTicker.tsx`、`apps/web/src/pages/FocusPage.tsx`。
- **当前限制**：设置只保存在当前浏览器/WebView 的 `localStorage`；运行态刷新后重置。专注页和命令面板的“专注设置”当前仍固定打开通用模块；专注参数实时预览会重置计时且取消不能恢复进度。计时尚未绑定具体任务、写入 `focus_sessions` 或累计 `actual_minutes`；跳过、连续天数、系统勿扰和原生通知未实现。

#### 10.4.13 T-13 命令面板、快捷键与反馈状态

- **需求映射**：5.9。
- **用户流程**：`Ctrl/Cmd+K` 打开命令面板，`Ctrl/Cmd+N` 打开新建任务；面板可搜索页面、已加载任务、新建任务和专注设置，支持方向键、Enter 和 Esc。
- **实现方法**：全局键盘监听仅在当前 WebView 内生效；命令面板在前端过滤页面命令和最多 12 项已加载任务。`AppShell` 当前只发起健康查询但不展示查询结果；`feedback.tsx` 提供骨架、错误、重试和空状态。
- **关键路径**：`App.tsx`、`components/CommandPalette.tsx`、`components/feedback.tsx`。
- **当前限制**：不是操作系统全局快捷键；没有最近使用排序、项目/客户/发票搜索、任务详情直达、桌面启动恢复页和全局错误边界。

#### 10.4.14 T-14 测试、构建与桌面验收

- **开发流程**：定向测试通过后，依次运行格式检查、TypeScript、前端测试、Go 测试、Web 生产构建、Rust 格式/元数据和可行的 `cargo check`。正式构建先生成目标 triple Sidecar，再构建 Web 和 Tauri。
- **命令**：

```powershell
pnpm format:check
pnpm typecheck
pnpm --filter @opc/web test
pnpm test:go
pnpm build:web
pnpm check:tauri

pnpm build:sidecar
pnpm build:desktop
```

- **当前实现方法**：前端使用 Vitest + Testing Library；Go 使用包级单元/集成测试和内存 SQLite；Rust 测试覆盖 ready 握手及运行状态。`build-sidecar.mjs` 注入应用版本、去除调试路径和符号，并按 Rust target triple 输出 Tauri `externalBin`。
- **环境边界**：2026-08-27 文档更新主机未安装 Go/Rust；此前 Windows 环境又缺少完整 MSVC Build Tools / Windows SDK。两者都不能作为桌面完整编译、安装包或跨平台验收证据，不能据此勾选 9.3 的三平台、签名、性能或干净系统验收项。

#### 10.4.15 T-15 AI 助手

- **需求映射**：5.10、9.1、9.2。
- **当前状态**：未开始；没有模型依赖、本地 Adapter 配置、会话 API、前端路由或占位按钮。
- **建议开发流程**：
  1. 先完成本地运行时、资源预算、上下文权限和质量评测 ADR；v1.6 当前阶段不评审远程 Provider。
  2. 对本地运行配置和敏感凭据使用应用配置边界或操作系统安全存储，验证其不进入普通 SQLite、`localStorage`、命令行和日志。
  3. 在 Go Sidecar 定义本地 Adapter 接口，统一普通/流式响应、取消、超时、资源限制和错误映射。
  4. 先实现无业务写权限的独立助手，再接入用户显式选择的任务、项目、客户或知识库上下文。
  5. 建立固定问题集、无答案处理、提示注入、越权读取、超时和本地运行时不可用测试。
- **实现边界**：模型输出默认只读，不得直接写任务、客户、项目、发票、收入或支出；自主代理和自然语言写操作另行立项。
- **验收要求**：禁用网络时仍可工作，本地执行可取消，敏感正文不落日志，关闭 AI 或未安装本地运行时时核心功能完整可用。
- **版本条件**：完成 v0.1、备份恢复和桌面安全存储后再排期。
- **与本地 Agent 的区别**：T-15 是面向用户的对话/模型能力；T-19 是受任务、能力和验收约束的本地执行生命周期。引入 Actor/Assignment 不代表已接入 AI，也不允许 T-15 绕过 T-19 的本地权限与验收边界。

#### 10.4.16 T-16 本地知识库

- **需求映射**：5.11、9.1、9.2。
- **当前状态**：未开始；没有知识库 schema、提取器、索引进程、检索 API、前端页面或向量依赖。
- **建议开发流程**：
  1. 新增递增迁移，规划 `knowledge_sources`、`knowledge_documents`、`knowledge_chunks` 和 `knowledge_index_jobs`。
  2. 先使用 SQLite FTS5 实现确定性关键词检索，再独立评审本地向量索引或 embedding。
  3. Go Sidecar 实现导入 → 提取 → 分段 → 索引 → 检索 → 来源定位与级联删除。
  4. 提供可取消/重试的索引任务和版本化 API，再实现资料列表、索引状态、搜索和引用跳转页面。
  5. 建立来源准确性、增量更新、无结果、提示注入、权限隔离、删除完整性和索引恢复测试。
- **实现边界**：默认本地索引；未经明确选择不把原文或分段发送给远程模型。
- **验收要求**：每条检索结果可定位原文；原文、分段、索引和缓存可完全删除；知识库故障不影响其他模块。
- **依赖关系**：知识库独立可用，AI 助手只能通过受控检索 API读取用户选定结果。

#### 10.4.17 T-17 客户回访

- **需求映射**：5.4、9.1、9.2。
- **版本归属**：v0.4 后续业务版；当前没有数据表、API、提醒任务或回访页面。
- **建议开发流程**：
  1. 新增 `client_followups` 迁移，字段至少包括客户、计划时间、渠道、目的、状态、结果、下一步和完成时间。
  2. 实现创建、编辑、分页、派生到期/逾期筛选、完成、跳过、取消和重新安排 API。
  3. 在客户详情显示时间线，在今日工作台和收件箱显示到期提醒，并支持跳回客户。
  4. 为时区边界、重复提醒、客户删除约束、完成后续约和逾期统计补充测试。
- **实现边界**：第一版只提供本地记录和提醒，不自动向客户发送邮件、短信或第三方消息。
- **验收要求**：回访计划可追踪、到期提醒准确、完成结果可审计、重新安排不丢历史，空/错误/重试状态齐全。

#### 10.4.18 T-18 本地 Actor 与任务分派

- **需求映射**：5.2、5.6、6、9.1。
- **当前状态**：未开始；当前仓库没有 Actor、Assignment、父子任务、验收状态或分派历史。
- **用户流程**：owner 在设置中维护 person；在任务详情或收件箱拆分面板中把任务分派给 owner/person；person 只记录线下责任，进度和结果由 owner 回填。
- **实现方法**：新增 `actors`、`task_assignments`、`task_artifacts`、`workflow_events`，扩展 tasks 的父任务、完成条件、验收策略、状态和乐观并发版本；同一任务同一 role 只允许一个活动 Assignment，改派通过同一事务结束旧记录并新增记录完成。T-18 是这些基础对象和迁移的唯一归属，T-11 只消费它们。
- **前端范围**：Actor 设置页、负责人选择器、当前负责人、改派历史、任务提交产出、接受/返工/阻塞/取消操作和时间线。
- **验收要求**：仅一个 owner；owner/system 初始化与历史任务 owner Assignment 回填可重复执行且不重复；内置 Actor 不可删除；person 停用保留历史；person UI 不暗示已发送任务；并发改派拒绝旧写入；状态流转和审计在同一事务中完成。

#### 10.4.19 T-19 本地 Agent 执行

- **需求映射**：5.6、6、9.2。
- **版本归属**：v0.2 本地编排版；在 Adapter、能力模型、路径授权和崩溃恢复协议完成前，不把 agent Actor 显示为可执行。
- **当前状态**：未开始；没有 Agent Adapter、Run、能力令牌、产出或恢复逻辑。
- **执行流程**：owner 启动 Run → Sidecar 校验 agent 健康与任务 Assignment → 生成短时单次能力令牌和输入快照 → 本地执行器在限定资源内运行 → 产出写入受控目录 → Run 成功后任务进入 `waiting_review` → owner 接受或返工。
- **安全约束**：无任意 Shell、无数据库直连、无 WebView Bearer Token；路径和操作逐项白名单；删除、对外发送、付款确认等高风险动作不可委托。v0.2 上线前必须形成 Adapter/专用鉴权/令牌传输/跨平台进程沙箱与网络阻断 ADR；无法强制禁网的平台不得启用正式 Agent 执行。
- **恢复策略**：异常退出后 running Run 标记 `interrupted`；重试创建新 Run；可能产生副作用的动作不得静默重试。
- **验收要求**：禁网可用、超时/取消可靠、失败有结构化错误、产出带 SHA-256、成功不自动完成任务、返工保留全部历史、Sidecar 重启后 Run/Assignment/时间线一致。

### 10.5 当前验证矩阵

| 检查 | 最近已知结果 | 说明 |
|------|--------------|------|
| `pnpm format:check` | 通过 | 最近代码基线检查通过；根脚本不格式化 PRD，本次文档改动另做 Markdown 结构检查 |
| `pnpm typecheck` | 通过 | 前端 TypeScript 工程最近检查通过；本次只改文档，未重跑 |
| 前端测试 | 条件通过 | 当前为 5 个文件、29 项；Node 25.9 默认 experimental webstorage 会导致 localStorage 相关失败，使用 `NODE_OPTIONS=--no-experimental-webstorage` 时 29/29 通过，需在工具链中正式修复 |
| Go 测试 | 历史通过，本机未复验 | 当前文档更新主机无 Go；现有 11 个测试函数主要覆盖配置、安全、任务主流程、迁移、seed 和退出控制 |
| `pnpm build:web` | 通过 | 最近 Vite 生产构建成功；本次只改文档，未重跑 |
| Rust 格式/元数据 | 历史通过，本机未复验 | 当前文档更新主机无 Rust；Rust 共 6 个 ready/状态单元测试，未覆盖真实 Sidecar 生命周期 |
| `cargo check` / Rust tests | 环境受限 | 当前主机无 Rust；此前 Windows 环境缺少完整 MSVC Build Tools / Windows SDK |
| 本地运行 | 历史通过 | 历史记录中浏览器开发链路和 Sidecar 健康检查成功；不是本次文档更新的复验结果 |
| 桌面安装包与三平台验收 | 未执行 | Sidecar target binary、签名环境和干净系统矩阵尚未准备 |

根 `pnpm check` 当前只执行 TypeScript、Go 测试、Web 构建和 `cargo check`，没有覆盖前端测试、Rust 单元测试或格式检查；在修正聚合脚本前不得把单次 `pnpm check` 视为完整质量门禁。

### 10.6 已知限制与下一步顺序

1. **完成任务事实层**：补完整编辑/详情/删除、父子任务、扩展状态、完成条件、标签、项目选择、分页筛选和持久化手动排序，使今日页真正按 `planned_date` 展示。
2. **建立本地 Actor 与 Assignment**：先交付 owner/person 的本地责任记录、改派历史和人工验收；没有本地执行器时 agent 不可选。
3. **完成项目与基础客户**：按 API → Query → 页面/弹窗 → 详情实现，并把项目任务产出建模为可审计的 Task Artifact；客户回访不混入这一阶段。
4. **交付收件箱人工闭环**：实现 Inbox Item、手动提醒、已读/稍后、任务拆分/关联、分派、验收/返工、审计与自动解决，先接任务产出、临期/阻塞和系统故障事件源。
5. **接通专注持久化**：新增 Focus Session API，将开始/暂停/停止写入 SQLite，以绝对时间校正，并在成功事务中累计任务 `actual_minutes`。
6. **补数据安全链路**：一致性备份、恢复、导入导出、Task Artifact 归档、维护锁和诊断包内容选择。
7. **补桌面可靠性与发布能力**：Sidecar 故障恢复、统一日志落盘/轮转、托盘、原生通知、OS 全局快捷键、自动启动、签名更新和恢复页逐项最小授权实现。
8. **实现 v0.2 本地 Agent**：确定 Adapter、能力令牌、路径授权和崩溃恢复协议后，再接 Agent Run、产出、取消/重试和审核返工。
9. **实现本地规划与预设自动化**：路线图、内容日历和规则引擎只创建本地收件箱项/任务，不自动对外发送。
10. **排期 v0.4 业务版**：实现客户回访、收入/支出/发票纵切及其本地事件源；先数据契约和 CRUD，再做提醒、统计图和 PDF。
11. **最后评审知识库与 AI**：先完成本地 FTS、引用、删除和恢复，再单独评审模型、数据外发与权限；不得让 AI 阻塞核心本地工作流。

非敏感用户设置统一迁入版本化 SQLite `app_settings` 表；`appDataDir/config` 只保存数据库启动前必需的非敏感运行配置和迁移标记。首次升级兼容读取旧 `localStorage`，仅在服务端没有对应值时清洗迁移，成功后记录一次性迁移标记。专注运行态确定为持久化 Session：旧进程遗留的 active 会话先进入 recovery_pending，用户再选择计入中断间隔恢复、排除间隔恢复或结束为 interrupted；在该功能交付前继续明确显示“仅当前进程有效”。

### 10.7 各模块具体实施计划

| 顺序 | 模块 | 后端与数据 | 前端与用户流程 | 完成验收 |
|------|------|------------|----------------|----------|
| 1 | 任务基础事实 | 非状态字段完整 PATCH、分页/筛选/排序、标签、父子任务、批量操作和基础乐观锁；先保持现有三态兼容 | 详情、编辑、删除确认、项目/标签、父子任务、批量操作；看板后置 | CRUD、父子循环防护、全部子任务取消不自动推进、分页边界、并发冲突和加载/空/错误/重试完整 |
| 2 | Actor、分派与任务验收 | T-18 交付 actors、task_assignments、Task 扩展状态/完成条件/验收策略、task_artifacts、workflow_events 和受控状态命令 | Actor 管理、负责人/改派、阻塞/取消、提交产出、人工验收/返工和时间线 | person 不发送/同步；唯一活动分派；受控状态不能被 PATCH 绕过；停用保留历史；并发旧写入冲突 |
| 3 | 项目 | Project CRUD/状态流转、任务派生进度和客户关联；产出与 Inbox Item 事件集成在 T-11 完成后启用 | 卡片、筛选、新建/编辑、详情任务树/工时/产出/时间线、归档恢复 | CRUD、外键、进度口径、归档；T-11 后再验收项目产出→拆分→分派→验收和事件去重 |
| 4 | 客户 | Client CRUD/搜索/状态、删除约束、项目数聚合；发票上线后再聚合收入 | 表格、新建/编辑、详情资料/项目/活动/附件；不伪造邮件打开或下载行为 | 可选字段校验、分页筛选、受约束删除和关联可解释；回访保持 v0.4 |
| 5 | 收件箱人工编排 | T-11 交付 inbox_items、inbox_item_tasks、reminders、拆分/分派事务、source_event_key 去重和派生解决策略 | 列表/详情、Reminder、已读/稍后、拆分/关联任务、分派/改派、验收/返工、解决/忽略/重开 | 纯离线；零必需任务不自动解决；拆分失败全回滚；进度由任务派生；来源删除可解释；关联取消可审计 |
| 6 | 今日 | 按本地日期、逾期和本周范围查询；排序事务；按 IANA 时区计算 UTC 边界；增加收件箱派生计数 | 日期切换、真实分组、拖拽/回滚、恢复默认排序、编辑/删除/专注快捷操作；未上线的财务卡标后续 | 超过 100 项、午夜/夏令时、排序刷新、列表与统计一致；真实浏览器验证拖拽、键盘和窄屏 |
| 7 | 专注 | 活动 Session、开始/暂停/继续/停止/取消、绝对时间、异常恢复；停止与累计 actual_minutes 同事务且幂等 | 任务选择、恢复/结束上次会话、历史与日/周统计；ticker 只负责显示 | 后台挂起、系统休眠、跨午夜、重复 stop、崩溃恢复和工时只累计一次 |
| 8 | 设置与命令面板 | 版本化 app_settings、旧 localStorage 一次性迁移、头像文件引用、统一 search API、真实 health/version | 增加 Actor、通知、数据/备份、快捷键、诊断设置；v0.2 增加 Agent；入口可指定目标模块；搜索详情直达；Modal 焦点管理 | 旧设置不丢且不覆盖新值；取消专注草稿不损失进度；搜索目标可定位；键盘、焦点恢复和服务不可用完整覆盖 |
| 9 | 数据安全 | v0.1 一致性快照、manifest/SHA-256、迁移前备份、临时库验证、原子恢复和基础导入导出；v0.3 增加计划/映射 | 手动备份/恢复、进度、确认、失败诊断；v0.3 增加外部目录和高级导入 | WAL 活跃、低磁盘、损坏/未知 schema/校验失败均不覆盖当前数据；备份实际恢复验证 |
| 10 | 桌面与发布 | Sidecar 自动/手动恢复、孤儿治理、日志、版本兼容；托盘/通知/全局快捷键/文件对话框/自启/签名离线更新；三平台 CI | 全局服务状态、恢复页、托盘语义、权限引导、离线安装/更新反馈 | 崩溃/超时/退出无残留；签名、公证、干净机、性能和数据保留逐平台验证后才宣称支持 |
| 11 | 本地 Agent（v0.2） | Adapter ADR、专用鉴权、短时令牌、跨平台沙箱/网络边界、Agent Run、取消/重试/中断恢复 | 只显示健康且隔离已验证的 Agent；启动、运行、输出、失败、重试、待验收和返工 | 无任意 Shell/数据库/目录；禁网无法验证时执行保持禁用；成功进入 waiting_review；产出校验和历史完整 |
| 12 | 预设自动化（v0.2） | 规则和执行记录表，以 Workflow Event 触发，只允许创建本地 Inbox Item/Task/提醒，`rule_id + event_id` 去重 | 规则开关、下一次触发、运行日志和失败详情 | 用户时区、漏执行补偿、去重、禁用、失败重试和递归循环防护；不自动对外发送 |
| 13 | 路线图（v0.3） | roadmap_milestones、季度/日期/状态/项目关联，进度从项目/任务派生 | 季度分组、里程碑新增/编辑、日期调整、归档和项目跳转 | 季度边界、进度口径、项目删除、拖拽/编辑回滚和空/错误/重试 |
| 14 | 内容日历（v0.3） | content_items、平台/状态/发布时间/项目与任务关联，审核/发布时间幂等生成 Inbox Item | 月视图、月份切换、详情、新建/编辑、拖拽排期和准备任务 | 跨月、时区、拖拽回滚、关联任务和重复提醒；不自动发布外部平台 |
| 15 | 财务/发票/回访（v0.4） | financial_entries、Invoice/Followup CRUD、聚合、PDF、本地调度和幂等 Inbox 事件 | 账本、统计、发票状态/PDF、回访时间线和待办；外发/付款确认由 owner 操作 | 金额整数、编号唯一、PDF 失败不改状态、付款与财务记录原子一致、时区与去重正确 |
| 16 | 知识库/AI（版本待定） | 本地导入、FTS5、引用、删除和索引恢复先行；模型运行时另行 ADR | 来源定位、索引状态、检索与建议预览；AI 只能提交建议或 Task Artifact | 来源准确、无答案、提示注入、越权路径、删除完整、离线可用；任何写操作需批准 |

依赖主线：

```text
任务事实层
  → Actor / Assignment
  → 项目与基础客户
  → Inbox Item / 拆分 / 人工分派与验收
  → 今日与专注持久化
  → 备份恢复、桌面日志和故障恢复
  → 本地 Agent
  → 预设自动化与后续业务事件源
```

在对应纵切完成前，项目/客户/发票“新建”、收件箱“全部已读”、任务“筛选”和收入时间范围等无业务行为控件必须禁用或明确标记“后续版本”，不得用可点击外观暗示已经实现。

---

## 附录

### A. 历史原型文件索引（已移除）

下列 HTML 文件名只用于追溯历史设计来源，文件已于 2026-08-27 从仓库移除。当前实现与后续视觉验收以 React 页面、`styles.css`、模块文档和实际渲染结果为准。

| 页面 | 原型文件 |
|------|----------|
| 今日工作台 | `today-v1.html`、`today-linear.html` |
| 任务列表 | `tasks-linear.html` |
| 项目列表 | `projects-linear.html` |
| 客户列表 | `clients-linear.html` |
| 收入看板 | `income-linear.html` |
| 发票列表 | `invoices-linear.html` |
| 收件箱 | `inbox-linear.html` |
| 收件箱详情/拆分/分派 | 尚无新版原型；以 5.6 对象边界和工作流为准 |
| Actor 管理 | 尚无原型；规划放入设置模块 |
| 路线图 | `roadmap-linear.html` |
| 内容日历 | `content-calendar-linear.html` |
| 新建任务弹窗 | `modal-new-task-linear.html` |
| 任务详情弹窗 | `modal-task-detail-linear.html` |
| 筛选弹窗 | `modal-filter-linear.html` |
| 命令面板 | `modal-command-palette-linear.html` |
| 专注设置 | `modal-focus-settings-linear.html` |
| 收入详情 | `modal-income-detail-linear.html` |
| 客户活动 | `modal-client-activity-linear.html` |
| 自动化 | `modal-automation-linear.html` |
| AI 助手 | 尚无原型；版本与交互方案待评审 |
| 本地知识库 | 尚无原型；版本与交互方案待评审 |
| 客户回访 | 尚无原型；规划归入 v0.4 |

### B. 设计规范

- **主题**：深色模式（Dark Mode）为默认，当前已支持亮/暗主题切换；后续再评审跟随系统与自定义主题
- **主色**：紫色 `#5E6AD2` / `#8b5cf6`
- **辅助色**：成功绿 `#4CB782`、信息蓝 `#4E8DF0`、警告红 `#E5484D`、提醒橙 `#F5A623`
- **字体**：Inter（英文）+ Noto Sans SC / PingFang SC（中文）
- **圆角**：4px（小）→ 6px（中）→ 8px（大）→ 12px（卡片）
- **间距**：4px 基准，8px 为单位递增

### C. API 约定（预览）

健康检查使用 `/health`；业务 API 统一使用 `/api/v1`。生产环境所有请求（包括健康检查）都必须携带 Tauri 启动 Sidecar 时生成的会话令牌。

| 方法 | 路径 | 说明 | 当前状态 |
|------|------|------|----------|
| GET | /health | 健康检查，返回 app/API/schema 版本 | 已实现 |
| GET / POST | /api/v1/tasks | 查询、新建任务 | 已实现 |
| GET / DELETE | /api/v1/tasks/:id | 获取、删除任务 | 已实现；删除尚无前端入口 |
| PATCH | /api/v1/tasks/:id | 更新任务 | 部分实现；当前只接受 `status` |
| PATCH | /api/v1/tasks/:id/status | 更新三态任务状态 | 当前已实现；扩展状态上线后废弃，不能用于 waiting_review/done 等受控转换 |
| PUT | /api/v1/tasks/reorder | 原子保存手动排序 | v0.1 规划中 |
| POST | /api/v1/tasks/:id/start | 开始任务 | v0.1 规划中 |
| POST | /api/v1/tasks/:id/block | 带原因标记阻塞 | v0.1 规划中 |
| POST | /api/v1/tasks/:id/unblock | 由服务端恢复持久化的 blocked_from_status，客户端不得指定任意目标 | v0.1 规划中 |
| POST | /api/v1/tasks/:id/complete | 完成无需验收的人工任务 | v0.1 规划中 |
| POST | /api/v1/tasks/:id/cancel | 带原因取消 | v0.1 规划中 |
| POST | /api/v1/tasks/:id/reopen | 重新打开已完成/取消任务 | v0.1 规划中 |
| GET / POST | /api/v1/actors | 查询 Actor；v0.1 只允许新建 person，v0.2 才允许基于健康 Adapter 创建 agent | 分阶段规划中 |
| GET / PATCH | /api/v1/actors/:id | 查看、更新或停用 Actor | v0.1 规划中 |
| GET / POST | /api/v1/agent-adapters | 查询、注册本地 Adapter | v0.2 规划中 |
| POST | /api/v1/agent-adapters/:id/check | 本地健康与能力检查 | v0.2 规划中 |
| GET / POST | /api/v1/inbox-items | 分页查询、创建立即生效的手动工作项；定时提醒使用 reminders API | v0.1 规划中 |
| GET / PATCH | /api/v1/inbox-items/:id | 查看详情、编辑标题/优先级/截止时间 | v0.1 规划中 |
| POST | /api/v1/inbox-items/:id/read | 标记单条已读 | v0.1 规划中 |
| POST | /api/v1/inbox-items/read-all | 批量标记已读 | v0.1 规划中 |
| POST | /api/v1/inbox-items/:id/snooze | 设置稍后提醒 | v0.1 规划中 |
| POST | /api/v1/inbox-items/:id/unsnooze | 清除 snoozed_until 并恢复当前派生视图 | v0.1 规划中 |
| POST | /api/v1/inbox-items/:id/split | 原子拆分任务、关联并分派 | v0.1 规划中 |
| POST / DELETE | /api/v1/inbox-items/:id/tasks/:task_id | 关联或软取消关联；DELETE 写 unlinked 字段和审计，不删除历史 | v0.1 规划中 |
| POST | /api/v1/inbox-items/:id/resolve | manual 策略带原因解决，或验证 all_required_tasks_done | v0.1 规划中 |
| POST | /api/v1/inbox-items/:id/force-resolve | 二次确认并带原因强制关闭异常工作项 | v0.1 规划中 |
| POST | /api/v1/inbox-items/:id/dismiss | 带原因忽略 | v0.1 规划中 |
| POST | /api/v1/inbox-items/:id/reopen | 重新打开 | v0.1 规划中 |
| GET / POST | /api/v1/reminders | 查询、创建一次性本地提醒 | v0.1 规划中 |
| GET / PATCH / DELETE | /api/v1/reminders/:id | 查看、调整或取消提醒 | v0.1 规划中 |
| GET / POST | /api/v1/tasks/:id/assignments | 查询和创建分派 | v0.1 规划中 |
| POST | /api/v1/tasks/:id/reassign | 原子结束旧分派并创建新分派 | v0.1 规划中 |
| POST | /api/v1/assignments/:id/end | 结束当前分派 | v0.1 规划中 |
| POST | /api/v1/tasks/:id/submit-output | 提交产出并进入验收 | v0.1 规划中 |
| POST | /api/v1/tasks/:id/review | 接受结果或要求返工 | v0.1 规划中 |
| GET | /api/v1/tasks/:id/artifacts | 查询任务产出 | v0.1 规划中 |
| GET | /api/v1/artifacts/:id | 获取产出元数据、文本或引用 | v0.1 规划中 |
| GET | /api/v1/artifacts/:id/content | 通过鉴权读取受控目录中的本地文件 | v0.1 规划中 |
| DELETE | /api/v1/artifacts/:id | owner 确认后软删除产出并写审计 | v0.1 规划中 |
| GET / POST | /api/v1/tasks/:id/agent-runs | 查询、启动本地 Agent Run | v0.2 规划中 |
| GET | /api/v1/agent-runs/:id | 查看本地执行详情 | v0.2 规划中 |
| POST | /api/v1/agent-runs/:id/cancel | 取消执行 | v0.2 规划中 |
| POST | /api/v1/agent-runs/:id/retry | 创建新重试记录 | v0.2 规划中 |
| GET | /api/v1/inbox-items/:id/events | 收件箱时间线 | v0.1 规划中 |
| GET | /api/v1/tasks/:id/events | 任务时间线 | v0.1 规划中 |
| GET / POST | /api/v1/projects | 查询、新建项目 | 规划中 |
| GET / PATCH / DELETE | /api/v1/projects/:id | 获取、更新、删除项目 | 规划中 |
| GET / POST | /api/v1/clients | 查询、新建客户 | 规划中 |
| GET / PATCH / DELETE | /api/v1/clients/:id | 获取、更新、删除客户 | 规划中 |
| GET / POST | /api/v1/clients/:id/activities | 查询、新增本地人工活动 | v0.1 规划中 |
| GET / PATCH / DELETE | /api/v1/client-activities/:id | 编辑/软删除人工活动；系统引用只读 | v0.1 规划中 |
| GET / POST | /api/v1/clients/:id/attachments | 查询、导入受控附件 | v0.1 规划中 |
| GET / DELETE | /api/v1/client-attachments/:id | 读取或软删除附件并审计 | v0.1 规划中 |
| GET / POST | /api/v1/client-followups | 查询、新建客户回访 | v0.4 规划中 |
| GET / PATCH / DELETE | /api/v1/client-followups/:id | 获取、更新、删除客户回访 | v0.4 规划中 |
| GET / POST | /api/v1/financial-entries | 查询、新建收入/支出记录 | v0.4 规划中 |
| GET / PATCH / DELETE | /api/v1/financial-entries/:id | 获取、更新、删除收入/支出记录 | v0.4 规划中 |
| GET / POST | /api/v1/invoices | 查询、新建发票 | v0.4 规划中 |
| GET / PATCH / DELETE | /api/v1/invoices/:id | 获取、更新、删除发票 | v0.4 规划中 |
| POST | /api/v1/invoices/:id/generate-pdf | 生成发票 PDF | v0.4 规划中 |
| GET | /api/v1/focus-sessions/active | 查询可恢复的活动会话 | v0.1 规划中；数据表已存在 |
| POST | /api/v1/focus-sessions | 开始专注并返回会话 ID | v0.1 规划中；数据表已存在 |
| POST | /api/v1/focus-sessions/:id/pause | 暂停并结算当前区间 | v0.1 规划中 |
| POST | /api/v1/focus-sessions/:id/resume | 按绝对时间恢复 | v0.1 规划中 |
| POST | /api/v1/focus-sessions/:id/recover | 处理 recovery_pending：计入/排除间隔恢复或结束为 interrupted | v0.1 规划中 |
| POST | /api/v1/focus-sessions/:id/stop | 停止并幂等累计任务工时 | v0.1 规划中 |
| POST | /api/v1/focus-sessions/:id/cancel | 取消且不累计工时 | v0.1 规划中 |
| GET / PATCH | /api/v1/settings | 查询、更新版本化非敏感设置 | v0.1 规划中 |
| GET | /api/v1/stats/today?date=&timezone= | 今日任务和专注统计 | 已实现 date；IANA timezone 与 UTC 边界扩展规划中 |
| GET | /api/v1/stats/income | 收入/支出与净现金流统计 | v0.4 规划中 |
| GET | /api/v1/search?q= | 全局搜索 | 规划中；当前命令面板在前端搜索已加载任务 |
| GET / POST | /api/v1/knowledge/documents | 查询、导入知识库文档 | 版本待定；需 ADR 后确认 |
| POST | /api/v1/knowledge/search | 本地知识检索并返回来源 | 版本待定；需 ADR 后确认 |
| POST | /api/v1/ai/chat | 显式调用 AI 助手 | 版本待定；流式协议需 ADR 后确认 |

**本地 API 安全与响应约定**：

- 基础地址由 Tauri 在运行时注入前端，不写死生产端口，也不保存到持久配置
- 使用 `Authorization: Bearer <session-token>`；令牌只存在于当前应用进程生命周期
- Sidecar 只接受明确允许的 WebView Origin，并拒绝缺少或不匹配 Origin 的浏览器请求
- 错误响应统一包含 `code`、`message`、`request_id`；日志通过 `request_id` 关联且不得包含敏感正文
- 列表接口统一支持分页、排序和筛选；写操作使用事务，可能重试的创建操作支持幂等键
- API 时间戳使用 RFC 3339 UTC，金额字段使用最小货币单位整数
- 创建、拆分、分派、运行、验收、返工、解决和忽略均支持 `Idempotency-Key`；幂等记录必须包含请求摘要和可重放响应，同一 key 携带不同请求体返回 `409 CONFLICT`
- 状态变化携带 `expected_version` 或 `If-Match`；并发版本不一致时拒绝旧写入并返回 `409 CONFLICT`
- 本地调度事件以 `source_event_key` 建立唯一约束；幂等重放不得重复写 Workflow Event
- Agent 不使用 WebView 会话令牌；Sidecar 为单次 Run 发放短时、能力受限且不可复用的本地令牌
- Agent Runtime 使用独立路由组和鉴权中间件，或直接使用受控进程管道；具体传输、Origin 处理、撤销和泄漏防护必须由 v0.2 ADR 确定
- 外部 URL 产出只作为不可自动抓取的引用；任意本地文件必须先复制到受控 Artifact 目录，读取和删除都经过 Sidecar 鉴权与 Workflow Event
- Inbox 多态来源在 open/tracking 时默认限制硬删除；来源消失时保留最小快照并显式显示“来源已不存在”

### D. 架构决策记录

**ADR-001：桌面运行时采用 Go Sidecar，移除 Docker 依赖**

| 项目 | 内容 |
|------|------|
| 状态 | 已接受 |
| 日期 | 2026-08-25 |
| 决策 | React 静态资源和 Go Sidecar 随 Tauri 安装包交付；SQLite 使用 `appDataDir`；本地开发直接运行 Vite、Go 和 Tauri |
| 原因 | 降低安装门槛、启动耗时和资源占用，避免 Docker Desktop、镜像拉取、固定端口与容器生命周期带来的复杂度 |
| 影响 | 必须维护多平台 Sidecar 构建矩阵、进程生命周期、动态端口握手、更新兼容和 SQLite 迁移/备份机制 |
| Docker 边界 | 不属于 MVP 桌面运行时或本地开发依赖；未来仅可作为可选自托管、集成测试或服务端部署方案单独评审 |

**ADR-002：采用单机 Actor 与收件箱工作编排模型**

| 项目 | 内容 |
|------|------|
| 状态 | 已接受 |
| 日期 | 2026-08-27 |
| 决策 | 使用 owner/person/agent/system 四类本地 Actor；收件箱负责事件受理与跟进，Task 是唯一可执行工单，Assignment 保存责任历史，Agent Run 保存单次本地执行，owner 负责高风险验收 |
| 原因 | 在不引入线上服务的前提下，统一表达本人、外部责任人、本地 Agent 和系统规则的责任关系，并支持项目产出、发票待办和本地提醒持续跟进到完成 |
| 影响 | person 仅作本地责任记录；agent 只有在本地 Adapter 健康且能力明确时可执行；收件箱进度必须从任务派生；所有拆分、分派、产出、验收和返工保留审计 |
| 非目标 | 多人登录、远程任务领取、云同步、实时消息、远程 Agent、远程模型、自动发送邮件/发票/客户消息 |
| 未来扩展 | 若引入线上服务，必须重新评审身份、权限、同步冲突、通知、密钥、数据外发和撤销机制，不沿用 person 的本地责任语义冒充在线协作 |

### E. 版本变更记录

| PRD 版本 | 日期 | 变更 |
|----------|------|------|
| v1.2 | 2026-08-25 | 确立 Tauri 2 + React + Go Sidecar + SQLite 的本地优先架构和 v0.1 MVP 范围，移除桌面运行与本地开发的 Docker 依赖 |
| v1.3 | 2026-08-26 | 增加 app v0.1.0 / API v1 / schema v2 实施基线、统一开发流程、逐任务实现方法、验证矩阵与已知限制；修正原型路径、当前技术栈、专注设置、API 状态和模块完成度 |
| v1.4 | 2026-08-26 | 将 AI Provider 接入与本地知识库加入版本待定的后续工作包，补充功能边界、建议实施流程、隐私安全闸门和验收要求；v0.1 继续保持无 AI/LLM 依赖 |
| v1.5 | 2026-08-26 | 将 AI 助手与知识库拆为独立工作包；新增客户回访与收入/支出需求；把客户回访、收入/支出和发票业务归入 v0.4 后续业务版，进一步收紧 v0.1 范围 |
| v1.6 | 2026-08-27 | 将收件箱升级为本地工作受理与编排中心；接受单机 Actor 模型和无线上服务边界；增加任务拆分、Assignment、本地 Agent Run、产出、验收/返工、提醒、审计的数据/API 规划，并补齐各模块实施顺序、验收标准和当前文档基线；PRD 移入 `docs/`，新增整体功能架构与模块文档，历史 HTML 原型从仓库移除 |
