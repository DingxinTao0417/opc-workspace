# opc-workspace 产品需求文档 (PRD)

> **一人公司操作系统** · PRD v9.19
> 产品阶段：0 → 1 可运行基座（app v0.1.0）/ MVP 持续迭代
> 目标用户：独立创业者 / 自由职业者 / 一人公司经营者
> 技术架构：Tauri 2.0 + React + Go Sidecar + SQLite
> 文档日期：2026-08-29
> 实现基线：app v0.1.0 / API v1 / SQLite schema v31

> **v9.19 更新说明**：交付内置 Sidecar 的 generation-aware 有界自动恢复。已启动 generation 只有真实 `Terminated` 才按 500 ms、2 s 最多重拉两次，当前 generation 连续 Ready 30 秒后重置预算；外部模式、显式 shutdown、事件流关闭但无 `Terminated` 不自动重拉。每代生成新 token 并通过端口 `0` 重新请求动态 port（端口值允许被 OS 复用），React 在非 ready 时清除运行期连接与 TanStack Query，generation 改变补偿漏过的 `restarting`。Tauri 为内置模式注入 `OPC_EXIT_ON_STDIN_CLOSE=true`，父管道 EOF 触发 Go 优雅退出；外部/开发默认 false。Sidecar 在 pending restore、迁移、SQLite open 前取得数据库父目录固定 `.opc-sidecar-run.lock` OS 独占锁，冲突立即失败且不接触数据库。安全应用重启要求受管 child code 0 且无 signal；尚未创建 child 的 bundled 启动失败允许继续，延迟干净退出后可重试；并发 shutdown 共享一次 stop。T-02 仍部分完成：真实父崩溃/进程树、三平台与安装包未验收，没有 Job Object、进程组或孙进程治理，hard-hung orphan 只被运行锁挡住而不会自动回收。app v0.1.0 / API v1 / SQLite schema v31 不变，无 migration。

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

| 方案类型     | 代表产品               | 核心缺陷                                       |
| ------------ | ---------------------- | ---------------------------------------------- |
| 通用项目管理 | Notion、ClickUp、飞书  | 过于复杂，学习成本高，团队功能冗余             |
| 传统财务软件 | QuickBooks、金蝶       | 操作复杂，与日常工作流脱节，无法关联任务和项目 |
| 单一功能工具 | Todoist、Toggl、Forest | 只解决单一问题，数据孤岛严重                   |
| 纯云端 SaaS  | 多数 SaaS 产品         | 订阅制持续付费，数据在服务商，断网不可用       |
| 本地桌面应用 | 各类本地App            | 数据备份困难，升级容易丢数据，环境不一致       |

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

| 指标                   | 目标值                                           |
| ---------------------- | ------------------------------------------------ |
| 已初始化后的冷启动时间 | P95 < 2 秒（从点击图标到界面可交互）             |
| 桌面端安装包体积       | < 30MB（不含操作系统 WebView 运行时）            |
| 运行时内存占用         | P95 < 200MB（Tauri + WebView + Go Sidecar 合计） |
| 核心功能模块           | 6 个（今日/任务/项目/客户/收件箱/专注）          |
| 外部运行时依赖         | 0（无需 Docker、Node.js、Go）                    |
| 离线可用性             | 100% 核心功能离线可用                            |

---

## 3. 用户画像与核心场景

### 3.1 目标用户画像

| 用户类型   | 典型特征                             | 核心需求                                   | 使用频率      |
| ---------- | ------------------------------------ | ------------------------------------------ | ------------- |
| 独立开发者 | 接外包项目、做独立产品，技术背景强   | 项目进度追踪、工时记录、发票管理、客户沟通 | 每天 4-8 小时 |
| 自由设计师 | 品牌设计、UI 设计，多个客户并行      | 客户管理、项目里程碑、收款追踪、灵感收集   | 每天 3-6 小时 |
| 内容创作者 | 自媒体、博主、写作者，产出节奏不稳定 | 内容日历、任务排期、收入统计、专注写作     | 每天 2-5 小时 |
| 咨询顾问   | 企业咨询、教练服务，按小时收费       | 客户档案、时间计费、发票开具、收入分析     | 每天 2-4 小时 |

### 3.2 核心使用场景

**场景 1：晨间规划**

> 早上打开 App，今日任务按用户保存的顺序、优先级和截止时间展示。用户扫一眼右侧面板的本月收入进度和待跟进发票，拖拽调整两项任务顺序后，点击"开始专注"进入工作状态。

**场景 2：深度工作**

> 进入专注模式，番茄钟自动计时 50 分钟，暂停 opc-workspace 自身通知并隐藏无关界面，当前任务全屏显示。用户可按引导开启操作系统专注模式。完成一个番茄后自动记录该任务工时，提示休息 5 分钟，然后按用户设置开始下一个番茄。

**场景 3：项目产出拆分与跟进**

> 项目任务提交文档、设计稿或其他本地产出，并显式标记需要跟进后，系统生成一条收件箱项。用户可从项目产出查看待拆分/跟进中/已解决/已忽略状态和实时 required 进度，深链到 Inbox，继承或改选来源 Project，再拆成修改、发布、客户确认等关联任务；每项任务填写完成条件，分派给 owner 或仅作本地责任记录的 person。无需验收的任务可直接完成，manual 任务提交后进入待验收并由 owner 接受或返工；所有活动必需任务完成后 Inbox 自动解决。v0.1 不调用 AI/LLM，也不创建或运行 Agent。

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

| 技术       | 选型                  | 说明                                                                      |
| ---------- | --------------------- | ------------------------------------------------------------------------- |
| 框架       | React 18 + TypeScript | 生态成熟，组件复用性好                                                    |
| 样式       | Tailwind CSS v4       | 原型已使用，迁移成本极低                                                  |
| UI 组件    | 仓库内轻量 React 组件 | v0.1 使用 `Modal`、`PageHeader`、状态组件等自有实现；当前未安装 shadcn/ui |
| 状态管理   | Zustand               | 轻量，适合个人工具，避免 Redux 复杂度                                     |
| 服务端状态 | TanStack Query        | 数据请求、缓存、自动刷新                                                  |
| 路由       | React Router          | 标准路由方案                                                              |
| 图标       | Lucide React          | 原型已使用                                                                |
| 构建       | Vite                  | 开发体验好，构建快                                                        |

#### 后端

| 技术     | 选型                                                            | 说明                                                                       |
| -------- | --------------------------------------------------------------- | -------------------------------------------------------------------------- |
| 语言     | Go 1.22+                                                        | 单二进制、跨平台编译、内存占用低、并发好                                   |
| Web 框架 | Gin                                                             | 成熟、性能好、中间件丰富                                                   |
| ORM      | GORM                                                            | 功能完善，SQLite 支持好                                                    |
| 数据库   | SQLite (WAL 模式)                                               | 零配置、单文件、备份简单，个人工具足够                                     |
| 本地通信 | HTTP（当前）/ WebSocket（预留）                                 | 仅绑定 `127.0.0.1`，生产环境使用动态端口                                   |
| 认证     | WebView 启动期会话令牌（当前）/ Agent 单次能力令牌（v0.2 规划） | 两类令牌隔离；Agent 不得使用 WebView 令牌，具体传输与专用中间件由 ADR 定义 |
| API 文档 | PRD 契约 + Go 集成测试（当前）                                  | Swagger/OpenAPI 尚未接入，待接口范围稳定后补充                             |

#### 桌面端（Tauri）

| 功能         | 目标实现方式                                 | 当前状态                                                                                                                          |
| ------------ | -------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------- |
| 窗口管理     | Tauri 原生                                   | 已实现基础窗口                                                                                                                    |
| 系统托盘     | Tauri Tray API（`tray-icon` feature）        | 未开始                                                                                                                            |
| 全局快捷键   | Tauri plugin-global-shortcut                 | 未开始；当前只有 WebView 内 `Ctrl/Cmd+K`、`Ctrl/Cmd+N`                                                                            |
| 原生通知     | Tauri plugin-notification                    | 未开始                                                                                                                            |
| 签名离线更新 | Tauri Bundler / 本地更新包                   | 未开始；当前阶段不启用在线 Updater                                                                                                |
| 开机自启     | Tauri plugin-autostart                       | 未开始                                                                                                                            |
| 单实例锁     | Tauri plugin-single-instance                 | 已实现，第二次启动会唤醒主窗口                                                                                                    |
| 文件对话框   | Tauri plugin-dialog                          | 未开始                                                                                                                            |
| Sidecar 管理 | Tauri shell sidecar / Rust 子进程管理        | 部分完成：已实现 generation、有界自动重启、ready/health、前端世代清理、父管道 EOF 与并发 shutdown；真实父崩溃/进程树/三平台待验收 |
| 数据目录     | Tauri Path API（`appDataDir` / `appLogDir`） | 已实现目录初始化、启动故障 journal、Sidecar 与 Tauri 壳脱敏轮转日志                                                               |

#### 部署

| 技术       | 选型                 | 说明                                            |
| ---------- | -------------------- | ----------------------------------------------- |
| 前端交付   | Tauri `frontendDist` | Vite 构建产物内置于安装包，无需 Nginx           |
| 后端交付   | Tauri `externalBin`  | 每个目标平台打包对应架构的 Go Sidecar           |
| 数据持久化 | Tauri `appDataDir`   | 数据独立于应用程序文件，升级不覆盖              |
| 打包       | Tauri Bundler        | 生成各平台原生安装包，并包含前端和 Sidecar      |
| 开发环境   | 本机进程             | 直接运行 Vite、Go 和 `tauri dev`，不依赖 Docker |

### 4.3 技术栈选型理由

- **Tauri vs Electron**：Tauri 复用系统 WebView，应用运行时无需携带完整浏览器内核，更符合轻量桌面应用目标；实际包体、启动时间和内存以三平台验收测试为准。
- **Sidecar vs Docker**：Sidecar 随安装包交付，用户无需安装 Docker Desktop，也不需要拉取镜像或管理容器；Tauri 统一负责启动、健康检查、异常提示和退出清理。
- **Go vs Node.js**：Go 可编译为单二进制并按平台交叉构建，适合作为 Tauri Sidecar；业务逻辑与前端仍通过清晰的 API 边界解耦。
- **SQLite vs PostgreSQL**：单用户场景不需要网络数据库。SQLite 文件保存在系统应用数据目录，使用 WAL 提升并发读取；手动备份和破坏性迁移前自动回滚包均通过 `VACUUM INTO` 生成一致快照，并与全部 active 受控文件共同校验和发布。

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

> **当前状态**：部分完成。真实任务、持久化 Focus Session 和 Today Focus 汇总已接入，右栏读取真实活动 Session；任务已按所选本地日期完整分为逾期、当天、本周稍后和未排期，并支持日期导航。所选精确日期与未排期组支持按钮式/同组拖拽排序，四个可见分组支持按真实日期跨组拖拽，所有活动行支持任意日期或未排期安排、按状态/验收策略约束的开始/完成/开始专注快捷操作，以及共享完整编辑与版本化确认删除；顶部逾期/未来 24 小时临期统计可切换与服务端时钟一致的完整风险结果。真实收入和客户动态尚未实现。

今日工作台是用户每天打开应用看到的默认首页，承担**晨间规划、当日执行、状态概览**三大职责。采用三栏布局。

#### 页面布局

| 区域         | 位置       | 宽度   | 职责                                                                 |
| ------------ | ---------- | ------ | -------------------------------------------------------------------- |
| 左侧导航栏   | 左侧固定   | 220px  | 用户信息、全局搜索、模块导航、自动化卡片、本周效率                   |
| 顶部状态栏   | 内容区顶部 | 自适应 | 日期、连续专注天数、视图切换、筛选、新建按钮                         |
| 统计条       | 顶部下方   | 自适应 | 预计时长、待完成、逾期及临期任务数量；财务模块交付后再接真实收入指标 |
| 今日任务列表 | 内容区中部 | 自适应 | 逾期、"接下来·今天"和"稍后·本周"真实日期分组任务                     |
| 未排期任务   | 内容区下部 | 自适应 | 尚未设置 `planned_date` 的活动任务                                   |
| 右侧概览面板 | 右侧固定   | 280px  | 专注模式环形进度、临期事项、本月收入迷你图、客户动态                 |

#### 业务逻辑

1. 打开应用时按本地日期分页拉全逾期、今天、本周稍后和未排期活动任务，默认先按用户保存的手动顺序展示；未排序任务依次按优先级、截止时间和创建时间排列
2. 目标交互为拖拽调整并立即持久化，且提供“恢复默认排序”；任务页已接上移/下移，Today 四个可见活动分组已接共享拖拽与失败回滚：同日期保存完整顺序，跨日期先确认改期再分别保存源/目标顺序，空的所选日期和未排期也可作为明确落点；所有活动行仍提供版本化任意日期/未排期安排作为键盘替代
3. 任务行 hover/focus 提供安全快捷操作：todo 可开始，`in_progress + review_policy=none` 可完成，活动任务可在 Focus 服务端与本地循环均空闲时开始绑定专注；编辑直达共享详情，删除需要独立二次确认和当前版本，manual review、阻塞/取消等复杂流程继续进入详情页
4. 点击逾期或临期卡时，页面以 `status=active&due_state=...&sort=due_date` 分页读取完整服务端结果并替换普通四组；再次点击、切换另一卡或清除可恢复原日期分组。风险结果保留打开、编辑、安排、开始、完成、开始专注和删除入口，但不提供会混淆计划日期事实的拖拽/手动排序
5. 用户也可从右侧概览面板进入专注模式并启动番茄钟（默认 50 分钟）；主内容区不再重复放置专注卡片
6. 专注期间：当前已记录绑定任务的有效区间并显示环形进度；暂停本应用通知与系统级勿扰引导仍按平台能力延后
7. 番茄钟结束：完成当前专注轮次并提示休息 5 分钟；专注工时自动累计，任务是否完成由用户确认
8. 财务模块交付后，右侧收入数据根据本地已确认付款记录刷新；当前不得用静态数字模拟真实收入
9. 客户动态只显示用户手动记录或本地业务状态产生的事件；第一阶段不追踪提案下载、邮件回复或其他线上客户行为

#### 交互细节

- 任务行 hover/focus 时显示快速操作：当前已按状态提供开始、无需验收时完成和开始专注；编辑按钮直达共享详情，删除按钮打开独立确认弹窗并使用当前版本调用确认删除 API
- 统计条的逾期及临期数字可点击，快速筛选对应任务；按钮用 `aria-pressed` 暴露选择状态，统计与启用中的风险结果每 60 秒低频重取，任务写入仍立即失效相关 Query
- 日期 pill 点击可切换日期，查看过去/未来某天的任务安排
- 连续专注天数已由 Focus D1 交付；“周几 × 小时”专注热力图已由 Focus D2b 交付

---

### 5.2 任务管理

**历史原型（已移除）**：`tasks-linear.html`

> **当前状态**：部分完成。schema v9 延续任务事实、Actor/Assignment 与 D1 六状态，已交付 manual policy、Submission/Artifact、提交/接受/返工、等待验收取消撤回、受控文件与完整时间线；任务详情支持产出草稿、当前批次、分页历史、按需正文、安全下载及确认软删除。schema v13 已交付 Inbox 与已有 Task 的活动/历史关系；schema v15 已交付收件箱内原子拆分、父子 Task、初始 Assignment、自动结清/重开和例外强制解决；schema v17 新增保存视图但不改 Task 契约；schema v30 已交付直属子任务完成度、系统 child_rollup、父任务待验收、失效撤回与 accepted 后子任务失效重开。Today 与任务列表均已接计划组拖拽；六状态看板已接真实筛选、分页、选择、详情及受控跨列生命周期命令。任务新建/编辑、列表项目筛选、批量移动及 Inbox 拆分已统一使用服务端分页搜索 `ProjectSelect`，不再串行拉取全部项目；Agent 仍未实现。

目标提供全量任务视图；v0.1 已完成列表视图、看板读取闭环和跨列受控生命周期交互。

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

| 字段     | 类型     | 必填 | 说明                                                                       |
| -------- | -------- | ---- | -------------------------------------------------------------------------- |
| 标题     | 文本     | 是   | 2-200 字符                                                                 |
| 描述     | 富文本   | 否   | Markdown 支持                                                              |
| 类型     | 枚举     | 是   | work / review / followup / reminder                                        |
| 状态     | 枚举     | 是   | todo / in_progress / blocked / waiting_review / done / cancelled           |
| 优先级   | 枚举     | 否   | P0(紧急) / P1(高) / P2(中) / P3(低)                                        |
| 标签     | 多对多   | 否   | 可自定义颜色和名称                                                         |
| 关联项目 | 外键     | 否   | 关联到项目                                                                 |
| 父任务   | 外键     | 否   | 用于项目产出后的工单拆分和任务树                                           |
| 完成条件 | 文本     | 否   | 用于人工验收或 Agent 输出检查                                              |
| 验收策略 | 枚举     | 是   | none / manual；默认 none；创建时可选，既有任务只在 todo 且无提交历史时可改 |
| 当前提交 | 外键     | 自动 | 指向同 Task 最新 Submission；接受/返工/取消后保留，reopen 清空             |
| 截止日期 | 日期时间 | 否   | 含时间                                                                     |
| 预估时长 | 时长     | 否   | 分钟为单位                                                                 |
| 实际时长 | 时长     | 自动 | 专注模式自动累计                                                           |
| 计划日期 | 日期     | 否   | 安排在哪天做                                                               |
| 创建时间 | 时间戳   | 自动 |                                                                            |
| 更新时间 | 时间戳   | 自动 |                                                                            |

#### 核心功能

1. **双视图切换**
   - 列表视图：紧凑展示，适合批量操作，支持排序
   - 看板视图：已按六状态分列并复用筛选、分页、选择与详情；跨列拖拽先映射并确认受控生命周期命令，不能直接 PATCH 状态，人工验收仍在详情完成
2. **快速新建**
   - ⌘N 快捷键唤起新建任务面板
   - 标题为唯一必填项，可通过键盘快速补充截止时间、优先级和预估时长
3. **筛选与搜索**
   - 当前任务页已支持项目、标签（多标签同时包含）、状态、优先级、类型、精确计划日期、计划日期范围和截止日期范围筛选；API 另支持父任务与根任务筛选
   - 当前使用服务端 `LIKE` 搜索任务标题和描述；SQLite FTS 不在本纵切
   - 项目筛选与任务表单、批量移动和 Inbox 拆分共用 `ProjectSelect`；每页 20 条并以 250 ms 防抖做服务端搜索，搜索或翻页不会误清除既有项目
4. **批量操作**
   - 当前已支持事务化批量移动项目、设置/清除计划日期、添加/移除标签，以及开始、阻塞、解除阻塞、完成、取消和重新打开
   - 生命周期批量命令先校验全部任务的 `expected_version`、来源状态、负责人和验收策略，再在同一事务内更新 Task、Submission、Assignment、Workflow Event 与 Inbox 投影；任一失败整批回滚
   - 阻塞/取消要求统一原因，全部生命周期批量操作在前端二次确认；批量删除仍不提供
5. **拆分、分派与验收**
   - 父子任务只表达真实的完成层级；项目产出产生的下游工单默认通过收件箱关联，不自动成为来源任务的子任务
   - 任务分派给本地 Actor；分派历史独立保存，不在任务上复制负责人状态
   - 人工或 Agent 提交产出后进入 `waiting_review`；验收通过才进入 `done`，要求返工则回到 `in_progress`
   - 系统只从直属非取消子任务派生父任务进度；至少 1 个且全部 done，再满足父任务 manual、active owner/person assignee 与 active builtin owner reviewer 后，才创建无 Artifact 的 child_rollup 并最多进入 `waiting_review`
   - manual 历史、既有 pending 或 changes_requested child_rollup 优先于自动规则；系统撤回失效的 pending 批次，并只在 accepted 后的子任务完成条件失效时重开父任务
   - `person` 仅记录线下责任归属，不代表对方能够登录或远程操作应用
6. **任务产出**
   - 支持文本、文件、链接和结构化摘要，文件保存在应用控制的本地目录
   - 当前人工提交由活动 assignee 作为 producer，内置 owner 作为 recorder/submitter；未来 Agent Run 可在保持同一审计语义下接入
   - 产出记录来源 Task、Submission、Actor、校验值、完整性与创建时间；`requires_followup=true` 已在 schema v23 作为收件箱来源消费，每个 Artifact 以稳定事件键最多投影一条 Inbox Item
7. **任务依赖**（MVP 后）
   - 设置前置任务，前置任务完成后才出现在待办中

#### 状态变更契约

- `PATCH /tasks/:id` 当前编辑标题、描述、类型、优先级、项目、标签、日期、预估时长、父任务和完成条件等非生命周期字段，必须携带 `If-Match`，不允许直接写 `status`、`completed_at`、`submitted_at` 或 `reviewed_at`。
- 状态变化使用显式命令：开始、阻塞、解除阻塞、直接完成、提交验收、接受/返工、取消和重新打开。
- `block`、`cancel`、`request_changes` 必须填写原因；原因和前后状态写入 Workflow Event。
- `complete` 只允许 `review_policy = none` 的人工任务；`review_policy = manual`、Agent 任务和高风险任务必须使用“提交产出 → 验收”链路。
- `/tasks/:id/status` 已废弃并固定返回 `410 TASK_STATUS_ENDPOINT_DEPRECATED`；前端只能使用显式命令，不得用通用状态写入绕过规则。

| 命令            | 允许来源状态                                  | 目标状态            | 额外约束                                                       |
| --------------- | --------------------------------------------- | ------------------- | -------------------------------------------------------------- |
| start           | todo                                          | in_progress         | 存在活动 assignee；person 任务由 owner 代记                    |
| block           | todo / in_progress / waiting_review           | blocked             | 原因必填并保存 `blocked_from_status`                           |
| unblock         | blocked                                       | blocked_from_status | 仅允许恢复 todo / in_progress / waiting_review                 |
| complete        | todo / in_progress                            | done                | 仅人工任务且 review_policy = none                              |
| submit-output   | todo / in_progress                            | waiting_review      | review_policy = manual；至少提交一个有效 Artifact 或结构化说明 |
| accept          | waiting_review                                | done                | v0.1 只能由 owner reviewer 执行                                |
| request_changes | waiting_review                                | in_progress         | 原因必填；保留原产出和审核事件                                 |
| cancel          | todo / in_progress / blocked / waiting_review | cancelled           | 原因必填；不等同完成                                           |
| reopen          | done / cancelled                              | todo                | 清除终态时间，保留历史事件和产出                               |

当前 D1 与 D2 均已交付。所有生命周期、Assignment、submit-output、review 和 Artifact 删除写命令要求 Task `If-Match`，并可选稳定幂等键；同请求重放首次响应且不重复事件，同键异体请求冲突。manual 提交前必须有 active assignee 与 active owner reviewer；accept 在同一事务结束活动 Assignment，request_changes 回到 in_progress。waiting_review 取消会把 pending Submission 标记 withdrawn。accept、request_changes、cancel 保留最近 `current_submission_id`，reopen 才清空指针与快速提交时间，历史 Submission/Artifact/Event 始终保留。

schema v30 的父任务协调同样运行在触发命令的 SQLite 事务中。创建/改绑/解除父级/删除、单条与批量 complete/cancel/reopen、review accept、review policy 和 Assignment 变化会协调受影响父任务及完整有效祖先链，以 visited 集合防止循环；批量对父节点去重并返回最终 version。普通 pending child_rollup 失效后父任务回到 in_progress；父任务若正 blocked 且来源为 waiting_review，则保留 blocked、原因与时间，只把 `blocked_from_status` 改为 in_progress。accepted child_rollup 后的父任务只有在直属子任务条件失效时才由 system 重开为 todo；单纯 policy/Assignment 失效不重开 accepted 父任务，旧 Assignment 也不会自动恢复。

产出提交最多 20 个 Artifact；摘要最长 10,000 字符。text 最多 500,000 字符；link 仅允许无凭据 HTTP(S)、最多 4,096 bytes；structured 必须为 JSON object 且编码后最多 1 MiB；严格 JSON body 与 multipart `manifest` 各最多 1 MiB；单文件非空且最多 50 MiB，完整 multipart 请求最多 100 MiB。无文件使用严格 JSON；有文件时唯一 `manifest` JSON 必须是首个 part，其后只允许被 manifest 通过唯一 `file_field` 精确引用的文件 part，并可在 manifest 混合非文件项。客户端不得传 producer Actor，服务端从活动 assignee 派生；前端提交与文件下载使用 120 秒端到端超时。

#### 键盘快捷键

| 快捷键 | 功能                      |
| ------ | ------------------------- |
| ⌘K     | 全局命令面板（搜索/跳转） |
| ⌘N     | 新建任务                  |
| /      | 聚焦搜索框                |
| Space  | 快速完成/取消任务         |
| J/K    | 上下导航任务              |
| Enter  | 打开任务详情              |
| E      | 编辑当前任务              |
| D      | 删除当前任务（需确认）    |

---

### 5.3 项目管理

**历史原型（已移除）**：`projects-linear.html`

> **当前状态**：部分完成。当前 schema v31 保留并验证了由 schema v3–v5 交付的 Project model、CRUD、分页/搜索/筛选、快照式创建幂等、覆盖聚合事实的 `If-Match` 乐观锁、受控状态流转、归档恢复、确认硬删除，以及真实卡片/详情/任务聚合。Project 列表已对默认及全部白名单排序追加 `id ASC`，并在同一只读事务中读取 Count 与当前页，供共享 `ProjectSelect` 稳定分页。schema v21 新增 Project Note，schema v22 新增 Project Attachment；schema v23–v25 已依次投影显式 `requires_followup` Artifact、所属 Task 阻塞事件和提前 24 小时 Task 临期，schema v28 已把 Project 完成周期投影为收尾 Inbox Item 并协调父项目删除，schema v31 又把已关联 Client 的 Project 完成/重开事件投影为客户系统动态。Project Artifact 读模型现已同时返回 nullable follow-up Inbox 上下文和实时 required 进度，项目详情把产出放在任务区之后，显示跟进状态/阻塞/待验收/取消并深链 Inbox；后续拆分、owner/person 分派和 manual owner 验收继续复用 Inbox/Task 事实，不在 Project 新建写模型。Client CRUD、客户选择/改绑/解除和客户筛选已接通。Focus 完成会更新绑定 Task 工时并沿既有聚合链刷新 Project；项目详情还可按 Task 查询时当前项目归属查看 7 天/30 天/本月 completed-only 报告与终态 Session 历史。Project 写命令继续同事务追加不可变 Workflow Event；项目笔记、附件和 Focus 读模型不混入命令审计。schema v31 不改变 Project 表，只增加 Client Activity 来源唯一性。独立验收/开票等没有真实 Project 状态的节点不提前投影。Task 六状态上线后 Project 仍只把 done 计为完成，cancelled 留在总数/剩余口径。

项目采用卡片式网格布局，是任务的上层组织单位。

#### 项目属性

| 字段     | 类型   | 必填 | 说明                                                   |
| -------- | ------ | ---- | ------------------------------------------------------ |
| 名称     | 文本   | 是   | 2-100 字符                                             |
| 描述     | 文本   | 否   |                                                        |
| 客户     | 外键   | 否   | 关联客户                                               |
| 状态     | 枚举   | 是   | planning / in_progress / paused / completed / archived |
| 开始日期 | 日期   | 否   |                                                        |
| 截止日期 | 日期   | 否   |                                                        |
| 项目金额 | 金额   | 否   | 合同金额                                               |
| 颜色标记 | 颜色   | 否   | 用于看板和标签                                         |
| 进度     | 百分比 | 自动 | 已完成任务/总任务                                      |

#### 项目状态流转

```text
planning --start--> in_progress --pause--> paused --resume--> in_progress
                          \                   /
                           --complete--------> completed --reopen--> in_progress

任一非归档状态 --archive--> archived --restore--> 归档前状态

独立 DELETE：archived + If-Match + confirm=true --> 永久删除
```

| 状态   | 含义             | 可执行操作                                 |
| ------ | ---------------- | ------------------------------------------ |
| 规划中 | 已创建未开始     | 开始、编辑、归档                           |
| 进行中 | 正在执行         | 暂停、完成、编辑、归档                     |
| 已暂停 | 暂停执行         | 继续、完成、编辑、归档                     |
| 已完成 | 交付完成         | 重新打开、编辑、归档；生成发票仍属后续版本 |
| 已归档 | 不在默认活跃列表 | 恢复、确认后永久删除                       |

#### 项目详情页

点击项目卡片进入当前详情页，可查看/编辑项目资料、服务端分页的关联任务、新建并预选项目的任务、从任务状态派生的完成进度，以及任务 `actual_minutes` 合计。任务区默认查询根任务并复用 Task 分页 API 按展开动作读取子任务；也可切换到平铺列表，并按标题/描述、状态、优先级、类型、单标签及已/未排期组合筛选查看带父任务上下文的结果。有筛选时树视图停用，避免把命中子任务误当完整层级。状态操作由服务端返回的 `available_actions` 驱动；归档状态的可用转换只有恢复，永久删除是独立 `DELETE`。完成存在未完成任务的项目、归档和永久删除均有明确确认，版本冲突会刷新当前详情。

当前卡片工时聚合任务表 `actual_minutes`；完成的绑定 Focus Session 已在同一事务中按精确秒数 ledger 折算新增完整分钟写回 Task，并通过既有 trigger 递增 Project 聚合版本。项目新建/编辑和项目列表客户筛选共用真实 `ClientSelect`：每次从既有 Client API 读取 20 条，以 250 ms 防抖做服务端搜索并稳定翻页，不再串行拉取全部客户；跨页、inactive 或读取失败时保留当前选择，只有用户显式点击“清除客户”才提交 `null` 并解除关联。任务新建/编辑、Tasks 项目筛选、批量移动项目和 Inbox 批量拆分共用真实 `ProjectSelect`：固定每页 20 条并按名称服务端搜索，当前选择通过详情、当前页或名称快照保留；默认候选排除归档项目，但既有归档选择仍可辨认且不会因失败被改写。默认项目列表排除归档项，Client 详情为完整关联历史显式包含归档项目。项目详情已把所属任务产出区放到任务区之后，再依次展示 Focus、项目状态、人工笔记、受控附件和系统写命令时间线。产出卡按 follow-up 状态显示“待拆分 / 跟进中 / 已解决 / 已忽略”，展示实时必需任务进度及阻塞/待验收/取消数量，并可打开共享 Task 详情或深链来源 Inbox。Focus 报告只计算 completed Session 的闭合正时长 interval，终态历史另包含 cancelled/interrupted 审计记录；两者按 Task 查询时当前 Project 归属过滤，不复制历史项目快照。归档 Project 可读；Task 改绑会重分类，无 Task/Task 已删除/当前无项目不进入项目过滤结果。发票明细和收入/成本仍未接入。

Project Attachment 列表使用 `GET /api/v1/projects/:id/attachments`，默认每页 20、最大 100，按 `created_at DESC, id ASC` 稳定分页，并可用 `include_deleted=true` 查看审计历史。`POST /api/v1/projects/:id/attachments` 强制 Project `If-Match`、首个 `metadata` JSON part 和唯一 `file` part；可选 `Idempotency-Key` 保存首次响应快照。详情与下载分别使用 `GET /api/v1/project-attachments/:id` 和 `/content`；下载复验 size/SHA-256。确认软删除使用 `DELETE /api/v1/project-attachments/:id?confirm=true`、Project `If-Match`、1–1,000 字符原因和可选幂等键。归档 Project 允许读取/下载但拒绝上传和删除；永久删除 Project 会通过 tombstone、trash 与事务补偿协调 active 附件，并在 UI 明确提示附件不可恢复。

Project Artifact 列表使用 `GET /api/v1/projects/:id/artifacts`，每项除 Artifact/Task/Submission 摘要外还返回 nullable `followup`。存在稳定 `task_artifact` 来源时，`followup` 包含 `inbox_item_id / inbox_item_version / status / resolution_policy / source_deleted_at / progress`；`progress` 是同一只读事务内按当前活动关系和 Task 状态派生的 required 进度，不写回 Project 或 Inbox 第二份状态。响应继续使用 Project 聚合数值 `ETag`，并与 `meta.project_version` 表示同一个 Project 并发版本；follow-up 状态/进度不传播到该版本，由成功 Inbox mutation 的 Project Query 失效保证前端刷新。未来若 Project 页面直接发起 Inbox 写入，必须使用 `followup.inbox_item_version` 作为 Inbox `If-Match`；当前页面只深链 Inbox。该读模型无迁移、无新 API 版本，也不返回 Artifact 正文。

项目 Task 提交显式 `requires_followup` 产出后，schema v23 已按 Artifact ID 稳定去重并同事务创建收件箱项；未标记产出不创建。schema v24 已按 block 后 Task version 为每次任务阻塞创建独立收件箱项；schema v25 已按 Task+截止时点投影提前 24 小时临期事项。schema v28 把用户明确执行的 Project `complete` 命令作为完成收尾节点同事务投影，并按完成后 version 区分周期；schema v31 对事件发生时已关联 Client 的 `complete / reopen` 命令，以 Project Workflow Event ID 在同一事务创建唯一 Client Activity。无 Client 不投影，改绑不移动旧动态，迁移/启动不回填历史；尚不存在的独立验收、开票状态不伪造 Inbox 或客户动态事实。

---

### 5.4 客户管理

**历史原型（已移除）**：`clients-linear.html`

> **当前状态**：部分完成。schema v10 的 Client 基础资料和 Project 客户关联、schema v18 的本地人工活动时间线、schema v19 的受控附件、schema v20 的 person 显式关联均已交付；schema v31 已交付 Project 完成/重开的本地系统事实投影。Project/Task 三个入口共用的服务端分页搜索 ClientSelect 已交付，真实浏览器/窄屏/大数据量专项仍待验收。外部来源集成、客户回访、发票详情和财务聚合仍未实现。

客户列表采用表格视图，管理客户关系。

#### 客户属性

| 字段          | 类型   | 必填 | 说明                                                              |
| ------------- | ------ | ---- | ----------------------------------------------------------------- |
| 名称          | 文本   | 是   | trim 后 1–200 个 Unicode 字符                                     |
| 联系人        | 文本   | 否   | trim 后空值为 null，最多 200 字符                                 |
| 邮箱          | 邮箱   | 否   | trim 后空值为 null，单一邮箱，最多 320 字符                       |
| 电话          | 文本   | 否   | trim 后空值为 null，最多 50 字符；保留国际号码文本                |
| 备注          | 长文本 | 否   | trim 后空值为 null，最多 10,000 字符                              |
| 状态          | 枚举   | 是   | active / lead / inactive；创建默认 active                         |
| 项目数        | 整数   | 自动 | API 从关联 Project 实时派生，不写回 Client                        |
| 版本          | 整数   | 自动 | schema v10 聚合乐观锁，从 1 开始                                  |
| 创建/更新时间 | 时间戳 | 自动 | API 使用 RFC 3339 UTC                                             |
| 标签          | 多对多 | 否   | 规划，当前未实现                                                  |
| 累计收入      | 金额   | 自动 | v0.4 从该客户 `confirmed` Financial Entry 聚合；当前不提供        |
| 最近动态      | 时间戳 | 自动 | 从未删除 Client Activity 的最大 `occurred_at` 派生；无记录为 null |

#### 客户列表

当前表格使用真实 API 展示客户、联系人、项目数、状态和最近活动，支持关键词、状态筛选和分页；累计收入显示“v0.4 后可用”，最近动态读取 nullable `latest_activity_at`，无记录时显示“暂无本地活动”，不以模拟数据补齐。Sidecar 列表默认每页 50、最大 100；`q` 搜索名称/联系人/邮箱/电话，`sort` 只允许 `name / contact_name / status / project_count / created_at / updated_at`，默认 `updated_at DESC`，所有排序追加 `id ASC`。

Project 新建/编辑、Projects 筛选和 Tasks 筛选通过共享 `ClientSelect` 消费同一列表契约。选择器固定每页 20 条，搜索词 trim 后以 250 ms 防抖发送，页码与关键词进入 Query key，旧请求通过 AbortSignal 取消；同名 Client 仍以 ID 为事实键。当前选中项不在本页时由详情查询、当前结果或 Project 名称快照补齐，任何搜索、翻页或失败都不得自动提交空值；inactive Client 保持可见可选。组件提供加载/输入等待/无客户/无匹配/错误重试/更多结果和明确状态，并以 combobox/listbox、方向键、Home/End、Enter、Escape、Tab、PageUp/PageDown 和可聚焦清除按钮支持键盘操作。真实浏览器焦点、窄屏浮层和 1,000/10,000 条数据性能仍需专项证据，组件测试不能替代。

#### 客户详情页

- 已实现：基本信息卡片、资料编辑、停用/恢复、完整关联项目分页列表和受约束永久删除。
- 创建支持 `Idempotency-Key` 保存首次响应快照；创建/详情/更新返回 `ETag`，PATCH/DELETE 强制 `If-Match`。
- 永久删除要求 `inactive + confirm=true + 最新 If-Match`。Invoice 强引用返回可解释冲突且不改变 Client、Project 关联或双方版本；Project 可选外键置空并返回 `detached_projects`。
- Project 关联变化递增受影响 Client 聚合版本；Client 名称变化或删除继续递增关联 Project 版本。
- 已实现本地活动：人工 `note / meeting` 包含标题、正文和实际发生时间；创建可使用 `Idempotency-Key`，更新/删除强制活动 `If-Match`，删除要求原因并保留不可变审计历史。默认时间线隐藏删除历史，可显式查看；活动变化原子递增 Client 版本。
- 已实现本地系统事实：Project 在事件发生时关联当前 Client，则 `complete / reopen` 与 Workflow Event、`system_reference` Client Activity 及既有完成 Inbox 投影同事务提交。动态按 Workflow Event ID 唯一、只读且无正文；无 Client、历史迁移和后续改绑不会伪造或搬移事实。
- 已实现受控附件：本地文件复制到共享受控对象存储，支持可选关联同一 Client 的活动、稳定分页、完整性校验下载、带原因软删除和只读历史；上传/删除原子递增 Client 聚合版本，Client 永久删除同步清理文件并保留删除墓碑。
- 已实现 person 显式关联：用户必须选择已有 active person，或确认原子新建 person；系统不从联系人字段自动推断。每个 Client 同时只允许一个 active contact，关联/解除使用 Client `If-Match` 与可选幂等键，解除必须填写原因并保留不可变历史；active 关联会阻止 person 停用。
- 待实现：外部活动来源集成、关联发票详情和财务聚合；线上下载、邮件、回访或其他客户沟通只有在未来明确集成并记录真实事实后才能出现。

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

| 字段     | 类型 | 必填 | 说明                                   |
| -------- | ---- | ---- | -------------------------------------- |
| 发票编号 | 文本 | 自动 | 自动生成，如 INV-2026-001              |
| 客户     | 外键 | 是   |                                        |
| 项目     | 外键 | 否   |                                        |
| 金额     | 金额 | 是   |                                        |
| 币种     | 枚举 | 是   | CNY / USD 等                           |
| 状态     | 枚举 | 是   | draft / sent / viewed / paid / overdue |
| 开票日期 | 日期 | 是   |                                        |
| 到期日期 | 日期 | 是   |                                        |
| 付款日期 | 日期 | 否   |                                        |
| PDF 文件 | 文件 | 自动 | 系统生成                               |
| 备注     | 文本 | 否   |                                        |

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

> **当前状态**：部分完成。T-11A1/A2/A3/B/C 已交付受理分诊、Reminder、Task 关系、拆分分派和自动结清；T-11F 已交付实时运营计数、Sidebar/Today 展示及 Inbox 风险深链筛选；schema v23–v25 已交付显式 follow-up Task Artifact、Task 阻塞与 Task 临期来源投影/删除协调；系统维护来源已交付备份四类操作失败、数据库启动/迁移、Sidecar 启动、运行期数据库操作失败和 1–100 GiB 可配置主动低空间投影。本轮完成 Project Artifact→Inbox→Task 的人工闭环收口：拆分继承可信来源 Project 且可清除/改选，提供真实完成条件输入与 person 本地责任提示，活动/可用历史关系可打开共享 Task 详情；成功 Inbox mutation 会失效来源 Project，split 另失效 Task/Today/Project。设置页已交付物理卷同卷去重及无路径手动容量检查；卷级趋势、重复/原生通知和 Agent Run 仍是规划，v0.1 不启用 AI/LLM/Agent。

收件箱不是普通通知列表，而是统一承接本地业务事件、明确下一步工作、拆分任务、分派责任、跟踪执行和完成验收的**本地工作受理与编排中心**。

第一阶段完全运行在本机，不提供多人登录、云同步、远程通知、邮件或消息自动发送，也不依赖线上 Agent 或模型服务。

当前手工纵切的具体边界：

- 创建 API 强制 `kind = manual`、`source_entity_type = manual`、`resolution_policy = manual`，不接受来源 ID 或事件键；当前前端只提交标题、说明、优先级和可选截止时间。
- `inbox / snoozed / archive` 三视图、标题/摘要搜索、优先级和分页均由 Sidecar 查询；`unread_total` 始终统计全局当前待处理 inbox 视图未读，不受当前 view、搜索或优先级筛选影响。
- `read_at`、`snoozed_until` 与主状态独立。resolve/dismiss 要求 1–2,000 字符原因、清除稍后但不隐式已读；终态未读可直接 read，无需重开。reopen 清除终态和稍后事实、保留 read/triaged，并按是否存在活动 Task 关系进入 `tracking / open`。
- “全部标为已读”提交列表 `snapshot_at` 作为 `through_created_at` 时间截止，只覆盖创建与最后更新时间均不晚于 cutoff、且按该 cutoff 仍属于待处理可见范围的未读；截止后发生编辑、分诊、重开等更新的条目保守跳过，避免旧批量操作覆盖新状态。这不是历史状态重建，也不是 `created_at + ID/序列` 的严格游标，极低概率同时间戳碰撞仍可能落入同一截止范围。
- 前端列表每 15 秒低频刷新，以服务端时钟让 snooze 到期项回到待处理；不依赖 Reminder 调度器写库。
- 创建、单条命令和全部已读支持幂等快照；PATCH 与单条命令使用 `ETag`/`If-Match`，单条命令在版本检查前执行幂等重放。有效写入与 `created / updated / read / snoozed / unsnoozed / resolved / dismissed / reopened` 事件同事务提交。
- schema v13 的 T-11A2 允许活动 Inbox Item 关联已有 Task、修改活动关系的必需标记、查看活动/历史关系并带原因软解除。关系读取由服务端实时 JOIN 当前 Task，返回活动数、required 总数、已完成/剩余/阻塞/待验收/已取消数、nullable 百分比和“全部必需任务完成”布尔值；零个 required 时百分比为空且完成布尔值为 false。这些值不是 Inbox Item 中的第二份进度事实。
- `is_required` 必须由 Inbox 关系创建/修改或 T-11C 拆分草稿显式填写；Task 父子层级、父任务自动待验收和 child_rollup Submission 都不会创建 Inbox 关系、继承或改写 required。
- 第一条活动关系使 `open → tracking`，最后一条活动关系解除使 `tracking → open`；重新打开时有活动关系进入 `tracking`，否则进入 `open`。关系写入不自动已读、不清除稍后时间，也不创建 Assignment 或改变 Task 生命周期。
- 关系 POST/PATCH/DELETE 使用 Inbox `If-Match`，可重试命令保存幂等快照；关系、Inbox 状态、版本与 `task_linked / task_requirement_changed / task_unlinked` 事件在同一事务提交。Task 本身不被关系命令修改，因此不递增 Task version。
- 活动 Inbox 关系会阻止 Task 硬删除；用户必须先带原因软解除。解除后 Task 可删除，关系保留原 Task ID/标题等快照并显式标记来源 Task 已删除，不级联抹除历史。
- 活动关系行以及历史中仍有实时 Task 的关系行都可通过 stack-aware Modal 打开全局共享 Task 详情，关闭后返回原 Inbox 上下文；删除后只保留快照，不显示失效入口。所有会改变条目状态、解决策略、关系或 required 进度的成功 Inbox mutation 会先取消可信来源 Project 的在途读取，再失效查询；Project Artifact 请求消费 `AbortSignal`，避免旧响应回填。split 还统一失效 Task、Today 与 Project 查询。

#### 对象边界

| 对象           | 负责的问题                                                            | 不负责的问题                             |
| -------------- | --------------------------------------------------------------------- | ---------------------------------------- |
| Inbox Item     | 为什么需要处理、来自哪个业务对象、是否已受理、稍后提醒或解决          | 不保存任务执行状态，不直接保存负责人     |
| Task           | 具体要完成什么、执行到哪一步、完成条件和验收结果                      | 不重复保存收件箱已读、稍后提醒等展示状态 |
| Assignment     | 当前任务由谁负责，以及历史上如何改派                                  | 不拥有独立的任务完成状态                 |
| Agent Run      | 本地 Agent 某一次执行尝试的排队、运行、成功、失败或中断               | 执行成功不等于任务完成                   |
| Task Artifact  | 人或 Agent 的文本、文件、链接或结构化产出，同时区分实际产出者与录入者 | 不决定任务是否验收通过                   |
| Workflow Event | 创建、拆分、分派、执行、验收和返工的追加式时间线                      | 不作为业务状态的第二事实来源             |

核心约束：

1. 收件箱项不能直接分派。“分派收件箱项”在交互上必须原子地创建或关联 Task，再为 Task 创建 Assignment。
2. 已读、未读和稍后提醒是展示属性，不是工作流状态。
3. 收件箱进度由活动关系中的必需 Task 实时派生，不保存第二份进度；schema v15 已交付统一 reconciliation 和自动解决/重开。
   Task 层级与 Inbox 关系是独立维度；父任务状态变化只会让已经显式关联且 required 的 Task 重新参与既有 reconciliation。
4. Agent Run 的 `succeeded` 只表示产生了输出；默认将任务推进到 `waiting_review`，由 owner 验收后才进入 `done`。
5. 发票、项目、客户等业务对象继续维护自身状态；收件箱只跟进由这些状态产生的待处理工作。

#### 本地 Actor 模型

> **当前实现**：T-18A/T-18B 已交付 Actor 身份、历史 Assignment 回填、Actor API 和设置页“人员与责任”，T-18C 已交付任务负责人/审核人及责任历史，T-18D D1 已在 schema v8 交付六状态命令、终态 Assignment 联动和 Task 活动时间线，T-18D D2 已在 schema v9 交付 manual Submission/Artifact 提交验收与责任审计。agent Actor 仍仅保留数据类型边界，v0.1 API 不允许创建、编辑或分派 agent。

| Actor 类型 | 定义                                      | 目标能力                                                         |
| ---------- | ----------------------------------------- | ---------------------------------------------------------------- |
| `owner`    | 当前设备上的应用所有者，固定一个          | 创建、拆分、分派、改派、验收、返工、解决和重新打开工作           |
| `person`   | 本地通讯录式责任人记录                    | 可以成为任务负责人；不登录、不接收同步、不直接操作应用           |
| `agent`    | 由 Sidecar 管理且通过健康检查的本地执行器 | 在授权能力和本地资源范围内运行并提交产出，不能自行验收           |
| `system`   | 内置调度与规则主体                        | 生成去重事件、派生进度、记录本地故障；不能替代用户完成高风险验收 |

版本边界：v0.1 的 owner/person/system 身份管理、owner/person 人工 Assignment 与 manual 受控验收闭环已经交付；agent Actor、Adapter 和实际 Run 归入 v0.2。界面明确提示：`person` 仅记录本机责任归属，不会向对方发送任务、创建账号或授予访问权限。客户联系人不自动成为 Actor，只有用户显式创建或关联后才能作为负责人。当前 API 直接拒绝 agent assignee；只有本地执行器注册、健康和隔离验证交付后，才可启用 agent 选项。

#### 规划事件来源

v0.1 人工编排阶段：

- `reminder_due`：用户创建的一次性 Reminder 到期后生成收件箱项；Reminder 是调度事实，Inbox Item 是到期后的处理事实。
- `task_output_submitted`：仅项目交付类任务或 owner 显式标记 `requires_followup` 的产出生成；普通子任务产出默认更新当前任务/收件箱，不递归创建新项。去重键必须包含 Artifact ID。
- `task_due`：schema v25 已实现。非终态 Task 的截止时间进入未来 24 小时窗口时生成；同一 Task+截止时点稳定去重，投影时标记 due_soon/overdue，改期形成新的独立事实，已生成事项不随 Task 完成/取消/改期自动解决。
- `task_blocked`：任务进入阻塞状态；schema v24 已实现，每次 block 按阻塞后的 Task version 形成独立事件，unblock 不自动替 owner 解决该事项。
- `system_maintenance`：schema v26 已建立通用系统维护约束，当前已接备份创建/校验/恢复演练/恢复安排的操作性失败、数据库启动/迁移、Sidecar 启动、运行期数据库操作失败和主动低空间监测；手动备份的 `BACKUP_SPACE_INSUFFICIENT` / `BACKUP_CAPACITY_UNAVAILABLE` 是当前请求的准入拒绝，不投影 generic `backup:create` incident。版本化 API 非预期数据库错误、数据库健康检查、Focus 心跳及 Reminder/Task 到期扫描失败会先尽力投影 `database:runtime`；Sidecar 在 ready 前及每 5 分钟检查三个受控根，每轮读取 schema v29 引入并在当前 schema v31 延续的 `app_settings.storage`，任一可用空间低于默认 1 GiB、可配置 1–100 GiB 的阈值时投影 `storage:low_space`，持续低空间不重复提示，恢复后再跌破才开始新周期。若设置读取暂时失败，进程使用最近一次已验证值或默认值，不因数据库不可读而丢弃已知低空间事实；若 incident 投影失败，则把白名单 kind、稳定 UUID 和 UTC 时间写入并发安全 journal，下一次健康启动补偿。所有 Inbox payload 只含 component/operation/failure_code/occurred_at 和固定用户提示。同一 source id 仅允许一个 `open/tracking` incident，且 journal 稳定 ID 防止模糊清理重复；原错误码保持不变，阈值值、路径、盘符、精确容量和探测错误不进入 Inbox/journal。诊断包 v1 已能导出上述已投影系统维护事实的错误码级汇总。

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
  → owner 执行，或由 owner 记录 person 的线下结果
  → 提交产出
  → owner 验收或要求返工
  → 所有必需 Task 完成
  → Inbox Item(resolved)
```

schema v15 已实现 T-11C：单次命令在一个 SQLite 事务中创建父子任务、关系、必需标记、初始 Assignment/reviewer 与审计事件，任一步骤失败时全部回滚。它只消费用户显式填写的任务草稿，不自动生成任务内容；v0.1 全链不调用 AI/LLM，也不创建 Agent Run。

#### 状态机与完成规则

Inbox Item 主状态：

```text
open → tracking → resolved
  └────────────→ dismissed

resolved / dismissed → open 或 tracking（重新打开时按现有关联任务派生）
```

- `read_at` 与 `snoozed_until` 独立于主状态。
- 当前手工创建固定进入 `open`；schema v13 建立第一条活动 Task 关系时进入 `tracking`，解除最后一条活动关系时回到 `open`。
- 当前 resolve/dismiss 必须带原因、清除稍后但不隐式已读；终态未读仍可 read。reopen 保留 read/triaged、清除解决/忽略及稍后事实，并按是否存在活动 Task 关系进入 `tracking / open`。
- T-11C 拆分命令可把活动条目设为 `all_required_tasks_done`；公开手工创建 API 仍固定为 `manual`。
- 零个必需任务时不得使用空集合规则自动解决，必须由 owner 明确解决或先关联必需任务。
- 必需任务处于 `cancelled`、`blocked`、`waiting_review` 或 Agent 失败状态时不得自动解决收件箱项。
- `resolution_policy = manual` 时，owner 可带原因解决；`all_required_tasks_done` 下普通 resolve 会验证至少一个活动必需任务且全部 done，不能绕过未完成任务。
- 确实不再需要跟进时，owner 先取消/解除必需关联，或使用单独的危险操作 `force-resolve`；强制解决必须填写原因、二次确认并写入不可变审计，不能伪装成任务正常完成。
- 忽略必须带原因；解决、强制解决和忽略都必须可重新打开。
- 手工 reopen 按**活动关系是否存在**选择 `tracking / open`；统一 reconciliation 在至少一个活动必需 Task 且全部为 `done` 时由 system 自动解决。只有 `resolution_mode=automatic` 的终态会因必需 Task 离开 done 自动重开，manual/forced 不会。
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
- schema v30 已实现父任务自动待验收：只看直属子任务；cancelled 不进入分母；必须至少存在一个非取消直属子任务且它们全部 done。父任务还必须是 todo/in_progress + manual，具有 active owner/person assignee 与 active builtin owner reviewer，且没有 manual Submission 历史、既有 pending 或 changes_requested child_rollup。系统创建无 Artifact 的 child_rollup 并最多进入 `waiting_review`，不能跳过 owner 验收。门禁/子任务失效撤回 pending；blocked 保持 blocked 并更新来源状态；accepted 后只有子任务条件失效才系统重开。收件箱是否解决仍只看显式活动 `inbox_item_tasks.is_required`，父子层级不隐式改变该集合。

Agent Run 状态：

```text
queued → running → succeeded
                 ├→ failed
                 ├→ cancelled
                 └→ interrupted
```

应用异常退出后仍为 `running` 的记录在恢复时标记为 `interrupted`，不得静默视为成功或无条件自动重试。

#### 页面与交互

当前 T-11B 列表：

- 标签页为待处理、稍后和已归档；按“今天 / 更早”分组，展示未读、优先级和相对时间。
- 支持标题/摘要搜索、优先级筛选和分页；未读数为全局待处理口径。
- 支持单条已读、时间截止式全部已读、稍后/恢复、新建手工条目；列表每 15 秒刷新到期可见性。
- 已实现加载、空、筛选空、错误、重试和分页过渡状态。

当前 T-11B 详情：

- 展示手工来源、标题、摘要、优先级、截止、已读、稍后、解决/忽略原因和创建时间。
- 活动条目可编辑；归档条目不可编辑或稍后。未读归档项仍可直接已读，所有归档项均可重开。
- 操作为已读、编辑、稍后/恢复、解决、忽略和重新打开；解决/忽略要求原因。
- 时间线分页展示创建、编辑、已读、稍后、恢复、解决、忽略和重开事件及 owner Actor。

当前 T-11A2 Task 关系：

- 详情按需读取活动关系和已解除历史，服务端实时 JOIN Task 状态并返回必需进度、阻塞与待验收提示；Task 删除后的历史关系继续显示保留快照。
- 支持选择已有 Task 建立关系、切换活动关系的必需标记、带原因软解除，以及跳转对应 Task；终态 Inbox Item 必须先重新打开再修改关系。
- 活动关系行可直接打开共享 Task 详情；历史关系仅在实时 Task 仍存在时提供同一入口，Task 已删除时只显示不可变 ID/标题快照。
- 关系命令使用 Inbox 最新版本，冲突后刷新关系和 Inbox 详情并要求用户重新确认；关系操作不会创建/修改 Assignment。

当前 T-11C 拆分与分派：

- 详情提供“拆分并分派”，一次填写 1–20 项，支持任务说明、类型、优先级、项目、完成条件、父任务、必需标记、owner/person 负责人和 manual/none 验收策略。
- 对 `task_artifact / task / task_due / project_completion` 四类可信本地来源，若 payload 携带 canonical `project_id`，首项与后续新增草稿默认继承该 Project；每项仍可显式清除或改选，来源快照不构成强制归属。
- 完成条件使用独立输入并写入 Task `completion_criteria`，不再用说明字段冒充；person 选项明确标注“仅本地责任记录”，不会登录、接收同步或直接操作应用。
- 父任务只允许引用本批次中更早的 Task；服务端再次校验顺序、Actor、Project、层级和字段，原子创建 Task、Assignment、reviewer、关系与事件。
- 用户可保留 manual 策略或启用“全部必需任务完成后自动解决”。所有任务写入口统一 reconciliation；自动结清后必需 Task 离开 done 会自动重开。
- 未完成自动项可使用独立危险操作强制解决；UI 要求原因和二次确认，服务端记录 forced mode，不把例外伪装为正常完成。
- split 写入失败保留拆分草稿；split API 成功响应后前端立即关闭 Modal，即使后台刷新失败也不保留可重放草稿。成功 Inbox mutation 按 cancel→invalidate 顺序刷新可信来源 Project；split 额外失效 Task、Today 与 Project。

T-11E 目标列表与详情：

- 增加跟进中、待验收等 Task 派生筛选，以及来源、任务状态、项目和负责人筛选。
- 展示来源上下文、必需任务进度、当前负责人、本地产出、任务树、审核历史和来源事件。
- 增加来源筛选与投影上下文；任务接受结果、要求返工和阻塞操作继续复用 Task 详情，Agent Run 输出、错误、取消和重试属于 v0.2。

当前 T-11E 系统维护（备份、启动和运行期数据库失败已交付）：

- 列表以硬盘图标区分 `system_maintenance`；详情标注“系统维护”，展示固定安全说明，并提供“打开数据与备份”。
- payload 不含 Go/SQL error、本机路径、备份 note、Token、请求正文或备份 ID；同一 `backup:create`、`backup:verify`、`backup:drill`、`backup:restore` 或 `database:runtime` 活动 incident 各自去重，归档后可再开。数据库不可写时安全 journal 延迟补偿，不改变原 API 错误。
- `BACKUP_SPACE_INSUFFICIENT`、`BACKUP_CAPACITY_UNAVAILABLE` 和 `BACKUP_INVALID` 都不另开 Inbox incident；前两者的响应也不含备份根、盘符、精确容量或底层探测错误。

T-11C 拆分任务面板（已交付）：

- 一次创建多条父子任务。
- 支持任务说明、类型、优先级、来源 Project 默认值、独立完成条件、父任务、必需标记和验收策略；项目可逐项清除/改选，计划/截止日期仍通过 Task 后续编辑补充。
- v0.1 每条任务只选择 owner 或 person，person 显示“仅本地责任记录”；当前界面不显示 agent，也不调用 AI/LLM。
- 提交后原子创建任务、关联、分派和审计事件。

Actor 管理位于设置页。owner 和 system 为内置 Actor，不可删除；owner 只允许修改展示名称，类型与内置属性不可变；v0.1 的 person 支持名称、备注和停用；v0.2 的 agent 支持本地适配器、能力范围、启停和健康检查。已有历史引用的 Actor 只能停用，不能硬删除。

#### 本地 Agent 安全边界

- Agent 不得直接打开 SQLite，也不复用 Tauri WebView 的 Bearer Token。
- owner 通过普通 `/api/v1/tasks/:id/agent-runs` 创建 Run；Sidecar 再通过受控进程管道或专用 `/api/v1/agent-runtime/*` 路由向 Adapter 发放短时、单次 Run 能力令牌，普通业务路由不得接受该令牌。
- v0.2 Adapter 先通过应用层能力白名单拒绝网络、任意数据库、任意 Shell 和任意目录访问；同时必须在各平台评审可验证的进程沙箱/网络阻断机制。若某平台无法强制隔离，Adapter 只能保留为禁用诊断记录，正式 agent Actor、分派和执行入口不得启用。
- 发票发送、客户沟通、付款确认、删除业务数据等动作必须由 owner 执行。
- 所有写操作通过 Sidecar 事务完成，并记录 request ID、Actor、资源、动作和时间。
- 本地审计用于可追踪性，不宣称能够抵抗拥有操作系统文件权限的用户篡改。
- v0.1 的 reviewer 只能是 owner；当前 Assignment API 不允许 system 或 agent 成为 assignee。若后续启用 system Assignment，也只能负责明确的内部维护任务。创建 Agent Run 时，assignment 必须是该 Task 当前活动的 agent Assignment，且 `assignment.actor_id` 与 `agent_actor_id` 一致。

#### 验收条件

当前 T-11A1/A2/B 已达到：

- 屏蔽外部网络时，手工创建、列表、详情、编辑、已读、稍后、解决/忽略、重开和归档时间线可用。
- 创建、单条命令和全部已读的幂等重放不重复创建资源或事件；异体复用被拒绝。
- PATCH/单条命令拒绝旧版本；事务内事件失败不会遗留半完成事实。
- 全局待处理未读数不受当前视图和筛选影响；全部已读遵守 `through_created_at` 时间截止，并排除截止后变化的条目。
- resolve/dismiss 原因必填且不隐式已读；终态未读可直接 read，reopen 保留 read/triaged。
- 活动/历史 Task 关系、required 修改和带原因软解除使用 Inbox 乐观锁、幂等快照和同事务事件；重新关联创建新历史行。
- 关系进度由服务端实时 Task JOIN 派生；Task 状态变化不会复制进 Inbox Item，也不会在 A2 自动解决。
- 活动关系阻止 Task 硬删除；软解除后可删除且 Inbox 关系历史保留 Task 快照。

当前人工编排闭环已达到：

- person 分派明确显示“仅本地责任记录”，不会登录、发送或同步。
- 同一来源事件跨扫描、跨重启只生成一条 Inbox Item。
- 拆分失败时不遗留部分任务、关联、分派或审计事件。
- T-11C 自动解决只使用活动必需任务，零必需任务不自动解决。
- Project Artifact 可显示 nullable follow-up 的四种主状态、实时 required 完成百分比和阻塞/待验收/取消提示，并深链 Inbox；关系行复用共享 Task 详情。
- Go 金链证明 `requires_followup` 来源可拆给 owner/person，manual reviewer 固定 owner；一项直接完成、另一项由 person 产出并提交进入 `waiting_review`，接受后 Inbox 自动 `resolved` 且进度为 100%。前端接线测试另覆盖 person 的本地责任提示与提交载荷。
- Actor 停用不破坏历史分派和审计。

仍待验收：真实浏览器/WebView 的完整跨页人工操作、键盘与焦点返回、窄屏布局，以及 1,000/10,000 条项目/任务和 Inbox 长列表性能；v0.2 Agent Run 的提交、返工与恢复另行实现和验收。

---

### 5.7 专注模式

专注模式是核心差异化功能，提供无干扰的深度工作环境。

> **当前状态**：Focus Core A+B+C、D1、D2a、Project 详情读取，以及 D2b 日期范围回顾、项目/标签时间分布、最佳小时段与二维热力图已完成。schema v11、Session API、任务绑定、绝对时间快照、刷新/启动恢复、15 秒心跳、暂停/继续/停止/取消、精确工时记账、Today 当地日统计、终态历史、7/30 天/本月/自定义趋势、Streak、项目/标签/小时分布、周几×小时热力图、Task 详情记录和 Project 详情项目级报告/历史已接通；原生通知、托盘和勿扰集成未实现。

#### 番茄钟

- 默认时长：50 分钟工作 / 5 分钟休息，可自定义
- 环形进度条可视化时间流逝
- 支持暂停/继续、停止完成与取消；“跳过工作段”不是独立服务端命令
- 专注结束音效提醒

#### 专注期间行为

- 持久记录 Session 和每个实际运行 interval；刷新后从服务端绝对时间快照恢复显示
- 可绑定任意非 cancelled Task，也可二次确认后无任务启动；只在 stop→completed 时累计绑定任务工时
- Sidecar 每 15 秒刷新 active heartbeat；异常启动把遗留 active 变为 recovery_pending，用户必须选择计入间隔继续、排除间隔继续或按最后心跳中断
- 当前任务高亮显示
- 暂停应用内通知、原生通知、托盘状态和系统勿扰引导属于 Focus D2b，当前未实现
- 白噪音和网站阻断属于后续候选，不在当前交付范围

#### 专注统计

- **已实现**：Today 按请求 IANA 时区的本地自然日统计完成 Session 数、精确秒数和向下取整的分钟数；按已关闭 interval 与日边界的实际 overlap 计算，跨午夜和 DST 不漂移，且只计 `completed`
- **已实现 D1/D2b 日期范围回顾**：终态 Session 历史 UI/API；最近七个本地自然日，以及可选最近 30 天、本月或最多 93 天自定义范围的总块数、精确秒/分钟、每日趋势、当前连续天数和区间最长连续天数
- **已实现 D2a**：Task 详情按需展示该任务的终态 Session、累计时长、结束时间和分页；取消/中断仅作审计展示
- **已实现 Project 详情读取**：历史与周期 API 可选严格 canonical UUID `project_id`；项目详情显示 7 天/30 天/本月趋势、总时长、完成数、连续天数和终态历史分页，两路各自提供加载、空、错误和重试。归档 Project 可读，Task 当前归属变化会重分类旧 Session
- **Focus D2b 其余部分延后**：原生桌面反馈

#### 专注设置模态框

**历史原型（已移除）**：`modal-focus-settings-linear.html`

v0.1 第一版可配置：

| 配置项       | 默认值  | 可选范围 / 行为                                               |
| ------------ | ------- | ------------------------------------------------------------- |
| 专注时长     | 50 分钟 | 5–120 分钟，步进 5 分钟                                       |
| 休息时长     | 5 分钟  | 5–30 分钟，步进 5 分钟                                        |
| 循环次数     | 4 次    | 1–8 次                                                        |
| 自动开始休息 | 开启    | 专注阶段结束后是否立即开始休息计时                            |
| 自动开始专注 | 关闭    | 休息阶段结束后是否立即开始下一轮专注                          |
| 结束后提示音 | 开启    | 阶段切换时播放短提示音；受系统音频和 WebView 自动播放策略限制 |

当前前端在应用内容渲染前通过 `SettingsBootstrap` 严格读取由 schema v16 建立的 `GET /api/v1/settings`，workspace/general/appearance/focus 的服务端确认快照是 committed。设置弹窗另持有 draft，store 的 `preview` 只用于可逆预览；普通保存只 PATCH 变化模块并携带 `expected_version`。schema v27 的头像 replace/remove 通过严格 `POST /api/v1/settings/avatar` multipart 完成：manifest 必须首 part，replace 只接受一个 PNG/JPG/WebP 文件、最大 2 MiB，remove 不得带文件；头像操作与 1–4 个变化设置共同成功或失败，通用 PATCH 只能原样携带已有 `avatar_ref`。选择文件立即创建本地预览，取消恢复 committed；保存成功后经鉴权 content 端点复验并读取 Blob。历史 `opc-focus-settings`/`opc-settings-local-v1` Data URL 仅在服务端无头像时一次性导入，服务端已有头像始终优先，事实验证后清理本地内容。Focus draft/preview 继续不改写活动 Session。

长休息、暂停本应用通知、原生系统通知、托盘、系统专注/勿扰引导、白噪音和专注期间自动状态设置仍属于 Focus D 或更后续能力，不属于当前已交付范围。

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

> **当前状态**：核心本地搜索、本地最近使用、设置运行诊断入口、脱敏诊断包 v1、Sidecar/Tauri 壳脱敏轮转日志、桌面打开日志目录、WebView→Sidecar request ID、全局渲染错误恢复和全局 Sidecar 启动故障恢复页 v1 已完成。支持已交付页面/安全操作命令、真实 Task/Project/Client/活动 Inbox 统一搜索、精确可刷新详情路由和完整键盘交互；空查询可展示有上限的非敏感最近命令/资源，并在确认资源 404 后清理。OS 全局快捷键和数据库打开前备份选择/实时恢复进度仍待开发。

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

> **当前状态**：当前 v0.1 筛选闭环已完成。任务页已接入状态、优先级、类型、项目、客户、标签、精确计划日期、计划日期范围和截止日期范围，并由服务端分页查询；客户条件沿 Task→Project→Client 当前关系筛选，倒置日期区间在前端阻断且由 API 二次校验。客户条件复用每页 20 条、250 ms 防抖和稳定翻页的共享 `ClientSelect`，不再串行读取全部客户。schema v17 保存视图支持保存、应用、更新和确认删除完整条件，最多 20 个。

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

> **状态**：未开始；不属于 v0.1，目标版本和本地模型/运行时待单独评审。当前仓库没有模型 SDK、密钥、AI API、会话表或助手页面；PRD v6.5 当前阶段不接入线上模型服务。

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
 ├── TaskSubmission → TaskArtifact
 ├── FocusSession
 └── WorkflowEvent

Project → Client / Task / Invoice
Client → Project / Invoice / Activity / Attachment
Invoice → Client / Project
```

对象事实边界：Inbox Item 保存来源与分诊，Task 保存实际执行状态和最近 Submission 指针，Assignment 保存责任变化，Task Submission 保存一次提交/审核批次，Task Artifact 保存产出事实，Agent Run 保存未来单次本地执行，Workflow Event 保存追加式审计。禁止在多个表中复制同一个完成状态。

### SQLite 存储约定

- 主键统一使用 UUID 文本
- 时间戳统一使用 UTC；API 使用 RFC 3339，数据库默认值使用 `CURRENT_TIMESTAMP`
- 纯日期字段使用 `YYYY-MM-DD` 文本，并在用户本地时区解释
- 金额使用最小货币单位整数存储，例如 `amount_minor = 12345` 表示人民币 `123.45` 元，避免浮点误差
- 布尔值使用 `INTEGER` 的 `0/1` 表示，并通过 `CHECK` 约束取值
- Go Sidecar 每次建立 SQLite 连接时执行 `PRAGMA foreign_keys = ON`、`PRAGMA journal_mode = WAL` 和合理的 `busy_timeout`
- 外键必须明确删除策略；客户、项目等仍被发票引用时默认 `RESTRICT`，可选关联默认 `SET NULL`

### 主要数据表

**workspace_identity** - 数据库与受控文件根身份（schema v9 已实现）

| 字段              | 类型    | 约束 / 说明                                                                        |
| ----------------- | ------- | ---------------------------------------------------------------------------------- |
| singleton         | INTEGER | PRIMARY KEY，固定为 1，保证每个数据库只有一条身份记录                              |
| database_id       | TEXT    | 不可变、唯一、规范小写 UUID；Artifact JSON marker 必须携带同一值                   |
| artifact_store_id | TEXT    | 可空、唯一、规范小写 UUID；仅允许从空值绑定一次，之后必须与 marker `store_id` 相同 |
| created_at        | TEXT    | 非空创建时间                                                                       |

trigger 拒绝删除记录或改变身份事实，仅允许 `artifact_store_id` 从空值绑定一次。Sidecar 在 ready 前校验 Artifact root 的规范 `format_version + database_id + store_id` marker；首次声明空 root 后将 store ID 写回数据库，之后数据库不能静默换 root，root 也不能被另一数据库接管。

**tasks** - 任务表

| 字段                        | 类型    | 约束                                                                                        |
| --------------------------- | ------- | ------------------------------------------------------------------------------------------- |
| id                          | TEXT    | PRIMARY KEY (UUID)                                                                          |
| title                       | TEXT    | NOT NULL                                                                                    |
| description                 | TEXT    |                                                                                             |
| kind                        | TEXT    | schema v6；work / review / followup / reminder，默认 work                                   |
| status                      | TEXT    | schema v8：todo / in_progress / blocked / waiting_review / done / cancelled                 |
| priority                    | TEXT    | DEFAULT 'P2'                                                                                |
| project_id                  | TEXT    | FOREIGN KEY → projects.id                                                                   |
| parent_task_id              | TEXT    | schema v6；FOREIGN KEY → tasks.id ON DELETE SET NULL；禁止自引用与循环                      |
| completion_criteria         | TEXT    | schema v6；最多 10000 字符，默认空字符串                                                    |
| review_policy               | TEXT    | schema v8：none / manual；schema v9 API/UI 已开放；改变只允许 todo 且无任何 Submission 历史 |
| current_submission_id       | TEXT    | schema v9；FOREIGN KEY → task_submissions.id ON DELETE SET NULL；只能指向同 Task 最新批次   |
| blocked_reason / blocked_at | TEXT    | schema v8：阻塞原因与时间；只在 blocked 非空                                                |
| blocked_from_status         | TEXT    | schema v8：解除阻塞时恢复的 todo / in_progress / waiting_review                             |
| due_date                    | TEXT    | RFC 3339 UTC                                                                                |
| planned_date                | TEXT    | YYYY-MM-DD                                                                                  |
| estimated_minutes           | INTEGER |                                                                                             |
| actual_minutes              | INTEGER | DEFAULT 0                                                                                   |
| manual_order                | INTEGER | 用户手动排序值，可为空                                                                      |
| created_at                  | TEXT    | NOT NULL DEFAULT CURRENT_TIMESTAMP                                                          |
| updated_at                  | TEXT    | NOT NULL DEFAULT CURRENT_TIMESTAMP                                                          |
| completed_at                | TEXT    | RFC 3339 UTC                                                                                |
| submitted_at / reviewed_at  | TEXT    | schema v8 字段；D2 提交/审核维护的当前快速事实，reopen 清空；完整历史以 Submission 为准     |
| version                     | INTEGER | schema v6；乐观并发版本，默认 1                                                             |

**task_submissions** - Task 提交批次（schema v9；schema v30 增加来源）

| 字段                                               | 类型    | 约束 / 说明                                                                               |
| -------------------------------------------------- | ------- | ----------------------------------------------------------------------------------------- |
| id / task_id                                       | TEXT    | UUID；Task 外键 ON DELETE CASCADE                                                         |
| sequence                                           | INTEGER | Task 内从 1 递增且唯一                                                                    |
| status                                             | TEXT    | pending_review / accepted / changes_requested / withdrawn；每 Task 最多一个 pending       |
| origin                                             | TEXT    | schema v30；manual / child_rollup；既有和用户提交为 manual，系统父任务汇总为 child_rollup |
| summary                                            | TEXT    | 默认空；最多 10,000 字符                                                                  |
| submitted_by_actor_id / submitted_at               | TEXT    | manual 固定内置 owner；child_rollup 固定内置 system；保存 UTC 时间                        |
| reviewed_by_actor_id / reviewed_at / review_reason | TEXT    | accepted/changes_requested 使用；返工必须有原因                                           |
| withdrawn_by_actor_id / withdrawn_at               | TEXT    | manual cancel 为 owner；自动批次失效为 system                                             |
| is_inferred                                        | INTEGER | schema v9 从无歧义旧事实回填时为 1；child_rollup 必须为 0                                 |

Submission 的来源及其他创建事实不可修改；只允许 pending_review 一次性转为 accepted、changes_requested 或 withdrawn。child_rollup 禁止关联 Artifact。Task 存在时禁止成员硬删除。

**task_artifacts** - Task 产出（schema v9 已实现）

| 字段                                             | 类型           | 约束 / 说明                                                                                                     |
| ------------------------------------------------ | -------------- | --------------------------------------------------------------------------------------------------------------- |
| id / task_id / submission_id                     | TEXT           | UUID 与同 Task Submission 外键；Task/Submission 聚合删除时 cascade                                              |
| position                                         | INTEGER        | Submission 内从 1 开始且唯一                                                                                    |
| submission_status                                | API 派生       | 必填 pending_review / accepted / changes_requested / withdrawn；由父 Submission JOIN 得出，不另存第二份可写状态 |
| storage_kind                                     | TEXT           | text / file / link / structured                                                                                 |
| name                                             | TEXT           | trim 后 1–255 个安全字符                                                                                        |
| content_text                                     | TEXT           | 仅 text；非空；API 最多 500,000 字符                                                                            |
| reference_url                                    | TEXT           | 仅 link；API 限 HTTP(S)、无 userinfo、最多 4,096 bytes                                                          |
| structured_json                                  | TEXT           | 仅 structured；合法 JSON object；编码后 API 最大 1 MiB                                                          |
| relative_path / mime_type / size_bytes / sha256  | TEXT / INTEGER | 仅 file；相对路径固定 `objects/<artifact-id>`；单文件 API 最大 50 MiB                                           |
| requires_followup                                | INTEGER        | 0/1；为 true 时按 Artifact ID 稳定投影 follow-up Inbox Item，Project Artifact 读模型返回对应 nullable follow-up |
| produced_by_actor_id                             | TEXT           | 服务端从提交瞬间 active assignee 派生                                                                           |
| recorded_by_actor_id                             | TEXT           | 当前固定内置 owner                                                                                              |
| integrity_status / integrity_checked_at          | TEXT           | unverified / verified / missing / mismatch；unverified 时检查时间为空，其他状态必须非空                         |
| deleted_at / deleted_by_actor_id / delete_reason | TEXT           | owner 确认软删除；三者同为空或同为非空；原因 1–1,000 字符                                                       |
| created_at                                       | TEXT           | 非空 UTC 时间                                                                                                   |

四种 payload 严格互斥；Artifact 事实创建后不可编辑，只允许受控完整性状态和首次软删除变化。所有 Artifact 摘要与详情都必须返回父批次派生的 `submission_status`，供客户端判断 pending 禁删；服务端仍做最终校验。API 摘要不返回 payload 或 `relative_path`；详情按需返回正文，软删除详情正文固定为 null；文件内容只通过鉴权下载端点提供。

**artifact_deletion_tombstones** - 文件删除恢复事实（schema v9 已实现）

每条记录保存 Artifact/Task ID、固定 `objects/<artifact-id>` 路径、size、SHA-256、`deletion_scope = artifact / task` 与删除时间。file Artifact 软删或 Task 聚合硬删在同一事务写入；记录无指向 Task/Artifact 的外键且由 trigger 禁止修改/删除，因此聚合消失后仍能让启动恢复区分授权删除与未知候选。

**tags / task_tags** - 标签与任务标签关联

| 字段                       | 类型    | 约束                                            |
| -------------------------- | ------- | ----------------------------------------------- |
| tags.id                    | TEXT    | PRIMARY KEY (UUID)                              |
| tags.name                  | TEXT    | NOT NULL；API 按大小写不敏感唯一，1–50 字符     |
| tags.color                 | TEXT    | NOT NULL；`#RRGGBB`                             |
| tags.version               | INTEGER | schema v6；乐观并发版本，默认 1                 |
| task_tags.task_id / tag_id | TEXT    | 复合主键与外键；单任务由 API 限制最多 20 个标签 |

标签名称/颜色修改或删除会递增所有关联 Task 的 `version`。任务列表在同一只读事务内读取行、标签与分页计数，避免标签内容与任务版本不一致。

**projects** - 项目表

| 字段                 | 类型    | 约束                               |
| -------------------- | ------- | ---------------------------------- |
| id                   | TEXT    | PRIMARY KEY                        |
| name                 | TEXT    | NOT NULL                           |
| description          | TEXT    |                                    |
| client_id            | TEXT    | FOREIGN KEY → clients.id           |
| status               | TEXT    | NOT NULL DEFAULT 'planning'        |
| start_date           | TEXT    | YYYY-MM-DD                         |
| due_date             | TEXT    | YYYY-MM-DD                         |
| amount_minor         | INTEGER | 最小货币单位                       |
| color                | TEXT    |                                    |
| version              | INTEGER | schema v3；乐观并发版本，默认 1    |
| archived_from_status | TEXT    | schema v3；归档前状态，用于恢复    |
| created_at           | TEXT    | NOT NULL DEFAULT CURRENT_TIMESTAMP |
| updated_at           | TEXT    | NOT NULL DEFAULT CURRENT_TIMESTAMP |

**clients** - 客户表

| 字段         | 类型    | 约束                                                 |
| ------------ | ------- | ---------------------------------------------------- |
| id           | TEXT    | PRIMARY KEY                                          |
| name         | TEXT    | NOT NULL                                             |
| contact_name | TEXT    |                                                      |
| email        | TEXT    |                                                      |
| phone        | TEXT    |                                                      |
| notes        | TEXT    |                                                      |
| status       | TEXT    | NOT NULL DEFAULT 'active'                            |
| version      | INTEGER | schema v10；聚合乐观并发版本，默认 1，必须大于等于 1 |
| created_at   | TEXT    | NOT NULL DEFAULT CURRENT_TIMESTAMP                   |
| updated_at   | TEXT    | NOT NULL DEFAULT CURRENT_TIMESTAMP                   |

当前 API 对名称、联系人、邮箱、电话和备注执行 trim、限长和控制字符校验，可选空值保存为 `NULL`；`project_count` 从 `projects.client_id` 实时派生，`latest_activity_at` 从未删除活动的最大 `occurred_at` 派生，均不是表字段。schema v10 增加名称、状态和更新时间索引，并在 Project 新增、改绑、解除或删除时递增受影响 Client 版本；既有 schema v5 trigger 继续在 Client 名称变化或删除时递增关联 Project 版本。

**client_attachments** - 客户受控附件（schema v19 已实现）

- 保存 `id`、`client_id`、可选的同客户 `activity_id`、受控 `storage_key`、文件名、MIME、大小、SHA-256、录入 Actor、版本、软删除审计和时间戳。正文位于共享受控文件 store；读取逐字节校验，删除经过 Sidecar 鉴权、确认、原因、乐观锁、trash 补偿与不可变墓碑。

**invoices** - 发票表

| 字段           | 类型    | 约束                               |
| -------------- | ------- | ---------------------------------- |
| id             | TEXT    | PRIMARY KEY                        |
| invoice_number | TEXT    | NOT NULL UNIQUE                    |
| client_id      | TEXT    | NOT NULL FOREIGN KEY               |
| project_id     | TEXT    | FOREIGN KEY                        |
| amount_minor   | INTEGER | NOT NULL，最小货币单位             |
| currency       | TEXT    | NOT NULL DEFAULT 'CNY'             |
| status         | TEXT    | NOT NULL DEFAULT 'draft'           |
| issue_date     | TEXT    | NOT NULL，YYYY-MM-DD               |
| due_date       | TEXT    | NOT NULL，YYYY-MM-DD               |
| paid_date      | TEXT    | YYYY-MM-DD                         |
| notes          | TEXT    |                                    |
| created_at     | TEXT    | NOT NULL DEFAULT CURRENT_TIMESTAMP |
| updated_at     | TEXT    | NOT NULL DEFAULT CURRENT_TIMESTAMP |

**focus_sessions** - 专注会话事实（schema v11 已实现）

| 字段                    | 类型    | 约束 / 说明                                                                                                |
| ----------------------- | ------- | ---------------------------------------------------------------------------------------------------------- |
| id                      | TEXT    | PRIMARY KEY                                                                                                |
| task_id                 | TEXT    | 可空，引用 Task；Task 删除时 `SET NULL`，开放 Session 会先由 API 阻止删除                                  |
| started_at / ended_at   | TEXT    | RFC 3339 UTC；开放态 ended_at 为空，终态非空                                                               |
| status                  | TEXT    | planned / active / paused / recovery_pending / completed / cancelled / interrupted；API 当前不创建 planned |
| legacy_imported         | INTEGER | 0/1；仅旧 schema 迁入终态为 1，用于无损保留历史超长 Session                                                |
| planned_seconds         | INTEGER | 新建为 300–7200；旧 schema 迁入终态可保留超过 7200 秒的原时长                                              |
| accumulated_seconds     | INTEGER | 已结算有效秒数，0..planned_seconds                                                                         |
| last_resumed_at         | TEXT    | active/recovery_pending 的当前区间起点                                                                     |
| last_heartbeat_at       | TEXT    | active 最近 Sidecar 心跳；心跳不递增 version                                                               |
| end_reason              | TEXT    | user_stop / completed / cancelled / crash_recovery                                                         |
| credited_minutes        | INTEGER | 本次 completed Session 实际写入 Task 的新增完整分钟；其他终态为 0                                          |
| version                 | INTEGER | 乐观并发版本，从 1 开始                                                                                    |
| created_at / updated_at | TEXT    | UTC 时间戳                                                                                                 |

开放状态 `active / paused / recovery_pending` 全库最多一个。状态相关的 ended_at、resume/heartbeat、end_reason 和 credited_minutes 组合由数据库 CHECK 保护。

**focus_session_intervals** - 有效运行区间（schema v11 已实现）

| 字段                  | 类型    | 约束 / 说明                                      |
| --------------------- | ------- | ------------------------------------------------ |
| id                    | INTEGER | PRIMARY KEY AUTOINCREMENT                        |
| session_id            | TEXT    | 引用 Focus Session，级联删除                     |
| started_at / ended_at | TEXT    | RFC 3339 UTC；当前 active interval 可开放        |
| duration_seconds      | INTEGER | 非负；开放 interval 必须为 0，关闭时固化精确秒数 |
| created_at            | TEXT    | UTC 时间戳                                       |

全库最多一个开放 interval。Today 不按 Session 的开始日归属，而是按已关闭 interval 与请求本地日 UTC 边界的实际 overlap 统计。

**task_focus_totals** - Task 精确秒数累计账本（schema v11 已实现）

| 字段            | 类型    | 约束 / 说明                                                                 |
| --------------- | ------- | --------------------------------------------------------------------------- |
| task_id         | TEXT    | PRIMARY KEY，引用 Task 并级联删除                                           |
| exact_seconds   | INTEGER | 已完成绑定 Session 的精确秒数总和                                           |
| applied_minutes | INTEGER | 已经写入 `tasks.actual_minutes` 的完整分钟总数，不超过 `exact_seconds / 60` |
| updated_at      | TEXT    | UTC 时间戳                                                                  |

schema v11 通过重建 `focus_sessions` 删除旧 `duration_minutes/completed`。历史 `completed=1` 映射为 completed，已有 ended_at 的其他记录映射为 cancelled，其余映射为 interrupted；同时补齐关闭 interval 与 ledger，但迁移不再次增加历史 Task `actual_minutes`。旧 schema 没有 120 分钟上限，迁移记录使用 `legacy_imported=1` 无损保留超长终态，公开创建 API 仍只接受 300–7200 秒。新 Sidecar 启动时把遗留 active 原子转为 recovery_pending 并递增 version；用户再选择计入中断间隔继续、只计到最后 heartbeat 后继续，或结束为 interrupted。stop 将 interval、Session、ledger、Task 工时/version 和 Workflow Event 放在同一幂等事务中提交。

#### 本地工作编排数据表（Task/Actor/D2、手工 Inbox、已有 Task 关系与一次性 Reminder 已实现；Agent 仍规划）

schema v7 `007_actor_assignments.sql` 新增 `actors`、`task_assignments` 和 `workflow_events` 并回填稳定责任；schema v8–v22 依次交付 Task workflow、受控 Artifact、Client、Focus、Inbox/Reminder、设置/保存视图、客户活动/附件/person 关联和项目笔记/附件；schema v23–v26 依次交付 follow-up Artifact、Task 阻塞、Task 临期与系统维护 Inbox 来源 guards；schema v27 `027_workspace_avatar.sql` 新增受控工作区头像；schema v28 `028_project_completion_inbox_projection.sql` 新增 Project 完成节点来源、不可变快照和删除协调；schema v29 `029_storage_settings.sql` 在迁移前回滚包保护下扩展 `app_settings.storage` 并保留既有设置事实；schema v30 `030_task_parent_progress.sql` 非破坏性增加 Submission origin，保留既有行为 manual，并约束 system/non-inferred/no-Artifact child_rollup；schema v31 `031_client_project_activity_projection.sql` 为 Project Workflow Event 来源建立 Client Activity 部分唯一索引。v30 不扫描或回填历史父任务层级，v31 不回填历史 Project 事件。各版本不创建 demo 数据。`agent_adapters` 和 Agent Run 仍是规划；后续只能从 `032_*` 追加迁移，不得回写已发布版本。

**client_activities** - 客户本地活动（schema v18 已实现）

| 字段                                               | 类型    | 约束 / 说明                                                                                                       |
| -------------------------------------------------- | ------- | ----------------------------------------------------------------------------------------------------------------- |
| `id / client_id`                                   | TEXT    | UUID；Client 外键 `ON DELETE CASCADE`                                                                             |
| `kind`                                             | TEXT    | `note / meeting / system_reference`；公开创建只允许前两种                                                         |
| `title / body`                                     | TEXT    | 标题 1–200 字符；人工活动正文 1–10,000 字符；删除响应不返回正文                                                   |
| `occurred_at`                                      | TEXT    | RFC 3339；人工创建不得超过服务端当前时间 5 分钟                                                                   |
| `created_by_actor_id`                              | TEXT    | Actor 外键；人工创建固定当前 owner，不接受客户端冒充                                                              |
| `source_type / source_id`                          | TEXT    | 仅 system reference 成对必填；Project 生命周期使用 `project_workflow_event` + Event UUID，公开 API 不接受来源写入 |
| `version`                                          | INTEGER | 活动乐观锁，从 1 开始；PATCH/DELETE 使用 `If-Match`                                                               |
| `deleted_at / deleted_by_actor_id / delete_reason` | TEXT    | 三者同为空或同为非空；删除原因 1–1,000 字符；删除后终态不可变                                                     |
| `created_at / updated_at`                          | TEXT    | UTC；身份、来源和创建时间不可变                                                                                   |

活动新增、修改或软删除由数据库 trigger 在同一事务递增父 Client 的 `version / updated_at`。默认查询排除删除项，显式 `include_deleted=true` 才返回审计历史；列表按 `occurred_at DESC, id ASC` 稳定分页。

schema v31 对 `kind=system_reference AND source_type=project_workflow_event` 的 `source_id` 建立部分唯一索引。Project `complete / reopen` 在原状态事务中复用刚创建的 Workflow Event ID，写入 system Actor、空正文、事件同一时间戳和固定中文标题；失败时 Project、Event、Client Activity 与完成 Inbox 投影全部回滚。迁移不做历史回填，Project 改绑 Client 也不搬移既有动态。

**client_attachments** - 客户受控附件（schema v19 已实现）

| 字段                                               | 类型           | 约束 / 说明                                                              |
| -------------------------------------------------- | -------------- | ------------------------------------------------------------------------ |
| `id / client_id`                                   | TEXT           | UUID；Client 外键 `ON DELETE CASCADE`；对象 ID 不得与 Task Artifact 冲突 |
| `activity_id`                                      | TEXT           | 可空；存在时必须引用同一 Client 的未删除 Activity                        |
| `file_name / media_type`                           | TEXT           | 规范化文件名与 MIME；文件名最多 255 字符                                 |
| `size_bytes / sha256`                              | INTEGER / TEXT | 文件大小与小写 SHA-256；下载前逐字节验证                                 |
| `storage_key`                                      | TEXT           | 固定为受控 store 内 `objects/<attachment-id>`，不暴露绝对路径            |
| `version`                                          | INTEGER        | 附件乐观锁，从 1 开始；DELETE 使用 `If-Match`                            |
| `deleted_at / deleted_by_actor_id / delete_reason` | TEXT           | 三者同为空或同为非空；删除原因 1–1,000 字符；软删除终态不可变            |
| `created_by_actor_id / created_at / updated_at`    | TEXT           | 当前 owner 与 UTC 时间戳；创建身份和文件事实不可变                       |

上传严格要求 multipart 第一部分为 JSON `metadata`、第二部分为唯一 `file`；单文件最多 50 MiB、整个请求最多 100 MiB。文件先写 `.staging` 并校验，再在数据库事实事务与对象提交之间执行可补偿协调；启动时按 Task Artifact 与 Client Attachment 的统一活跃事实清理 staging、恢复 trash 并隔离无引用对象。列表默认隐藏删除项，按 `created_at DESC, id ASC` 稳定分页；下载逐字节验证 size/SHA-256，完整性失败不返回正文。软删除和 Client 永久删除使用 `.trash`、不可变墓碑与事务补偿，成功后再清理文件。

**client_actor_links** - 客户与本地人员关联（schema v20 已实现）

| 字段                                                 | 类型 | 约束 / 说明                                                           |
| ---------------------------------------------------- | ---- | --------------------------------------------------------------------- |
| `id / client_id / actor_id`                          | TEXT | UUID；Client 删除级联关系历史，Actor 删除受限                         |
| `role`                                               | TEXT | 当前固定 `contact`；每个 Client 同时最多一条 active contact           |
| `linked_by_actor_id / linked_at`                     | TEXT | active owner 与 UTC 时间；关联身份和时间不可变                        |
| `unlinked_by_actor_id / unlinked_at / unlink_reason` | TEXT | 三者同为空或同为非空；原因 1–1,000 字符；写入后整行历史不可修改或硬删 |

目标 Actor 必须是 active `person`，关联/解除操作者必须是 active owner。POST 必须在 `actor_id` 与 `create_person` 之间二选一；新建模式在同一事务创建 person、写 `actor_created` Workflow Event 并建立关系。GET 默认返回 active 关系，可用 `include_unlinked=true` 查询历史；写请求要求 Client `If-Match`，支持 `Idempotency-Key`，关系变化递增 Client 版本。存在 active Client 关系的 person 不允许停用；Client 删除不删除 Actor。

**project_notes** - 项目人工笔记（schema v21 已实现）

| 字段                                               | 类型    | 约束 / 说明                                                             |
| -------------------------------------------------- | ------- | ----------------------------------------------------------------------- |
| `id / project_id`                                  | TEXT    | UUID；Project 外键 `ON DELETE CASCADE`；项目永久删除时笔记随聚合删除    |
| `title / body`                                     | TEXT    | trim 后分别为 1–200 / 1–10,000 字符；正文允许换行                       |
| `occurred_at`                                      | TEXT    | RFC 3339 UTC；API 拒绝超过五分钟容差的未来时间                          |
| `created_by_actor_id`                              | TEXT    | 当前为内置 owner；与 project_id、created_at 一同构成不可变身份          |
| `version`                                          | INTEGER | 笔记乐观锁，从 1 开始；PATCH/DELETE 使用 `If-Match`                     |
| `deleted_at / deleted_by_actor_id / delete_reason` | TEXT    | 三者同为空或同为非空；原因 1–1,000 字符；软删除后整条笔记历史不可再修改 |
| `created_at / updated_at`                          | TEXT    | 非空 UTC 时间；新增、编辑、软删除分别递增父 Project 的 `version`        |

列表默认排除删除项，显式 `include_deleted=true` 才返回审计历史；按 `occurred_at DESC, id ASC` 稳定分页。笔记与 Project Workflow Event 分表：前者是用户可编辑上下文，后者是不可变系统命令审计。业务 JSON 导出包含 `project_notes`，完整 SQLite 备份自然覆盖该表。

**project_attachments** - 受控项目附件（schema v22 已实现）

| 字段                                               | 类型           | 约束 / 说明                                                                     |
| -------------------------------------------------- | -------------- | ------------------------------------------------------------------------------- |
| `id / project_id`                                  | TEXT           | UUID；Project 外键 `ON DELETE CASCADE`；对象 ID 不得与 Task/Client 文件事实冲突 |
| `name / relative_path / mime_type`                 | TEXT           | 名称最多 255 字符；路径固定为 `objects/<attachment-id>`；不暴露绝对路径         |
| `size_bytes / sha256`                              | INTEGER / TEXT | 非空文件大小与小写 SHA-256；下载前逐字节复验                                    |
| `recorded_by_actor_id / created_at`                | TEXT           | 当前固定 active owner 与 UTC 时间；创建身份和文件事实不可变                     |
| `integrity_status / integrity_checked_at`          | TEXT           | `verified / missing / mismatch` 与最近检查时间                                  |
| `deleted_at / deleted_by_actor_id / delete_reason` | TEXT           | 三者同为空或同为非空；原因 1–1,000 字符；软删除终态不可变                       |

上传严格要求 multipart 第一部分为唯一 JSON `metadata`、第二部分为唯一 `file`；单文件非空且最多 50 MiB、整个请求最多 100 MiB。文件先写 `.staging`，随后通过可补偿协调发布到受控 object。列表默认隐藏删除项，按 `created_at DESC, id ASC` 稳定分页；下载逐字节验证 size/SHA-256，完整性失败不返回正文。创建和删除使用 Project `If-Match` 与可选 `Idempotency-Key`，归档项目只读；两类写入均递增 Project 版本。软删除及 Project 永久删除使用 `.trash`、不可变 `project_attachment_deletion_tombstones` 和事务补偿。手动备份/恢复包含 active object；business-export format v1 仅包含元数据和 active 文件摘要，不嵌入文件正文。

**actors** - 本地责任主体（schema v7 已实现）

| 字段                    | 类型    | 约束 / 说明                                                              |
| ----------------------- | ------- | ------------------------------------------------------------------------ |
| id                      | TEXT    | PRIMARY KEY (UUID)；owner/system 使用下述固定 UUID                       |
| type                    | TEXT    | owner / person / agent / system                                          |
| display_name            | TEXT    | trim 后 1–100 字符，NOT NULL                                             |
| status                  | TEXT    | active / inactive；owner/system 固定 active                              |
| is_builtin              | INTEGER | owner/system 为 1，person/agent 为 0                                     |
| notes                   | TEXT    | 最多 2000 字符，默认空字符串                                             |
| metadata_json           | TEXT    | 必须是 JSON object，默认 `{}`；API 另做大小、深度、key 数和敏感 key 限制 |
| version                 | INTEGER | 乐观并发版本，默认 1                                                     |
| created_at / updated_at | TEXT    | 非空 UTC 时间戳                                                          |

约束：仅允许一个 owner 和一个 system；内置 Actor 不可删除。owner 只允许修改 `display_name`，system 完全不可编辑；活动 Assignment 或 active Client contact 关联存在时 Actor 不得停用。person 当前可修改展示名、备注、metadata 和 active/inactive，API 不提供 Actor 删除路由。agent 的 Adapter 和能力字段不在 `actors` 表内，随 v0.2 的 `agent_adapters` 单独建模。

schema v7 使用稳定 UUID `00000000-0000-5000-8000-000000000001` 幂等创建 owner“我”，使用 `00000000-0000-5000-8000-000000000002` 幂等创建 system“系统”。`actors.display_name` 是责任人名称的唯一事实源；旧 localStorage 的 displayName/avatarDataUrl 下一纵切只迁移为 `app_settings.workspace` 品牌资料，不得改写 owner 名称。由于 schema v6 及更早任务全部来自单用户版本，迁移为每条历史 Task 回填 owner Assignment：未完成任务保留活动分派；已完成任务以 `completed_at`（缺失时用 `updated_at`）结束分派。每条回填写 `migration_assignment_backfill` Workflow Event，并标明这是迁移推定，不宣称有更细的历史执行证据。回填 ID 由已有 Task ID 确定性派生，不依赖应用运行时随机值；重复执行不得生成第二份内置 Actor、Assignment 或事件。

**agent_adapters** - 本地 Agent 执行器注册（v0.2）

| 字段                                | 类型 | 约束 / 说明                                          |
| ----------------------------------- | ---- | ---------------------------------------------------- |
| id                                  | TEXT | PRIMARY KEY (UUID)                                   |
| adapter_key                         | TEXT | UNIQUE 稳定标识                                      |
| display_name                        | TEXT | 展示名称                                             |
| executable_ref                      | TEXT | 内置执行器 ID 或经文件对话框授权的本地可执行文件引用 |
| manifest_json                       | TEXT | 版本、入口、声明能力、输入/输出协议和平台要求        |
| status                              | TEXT | enabled / disabled                                   |
| sandbox_profile                     | TEXT | ADR 定义的隔离配置标识                               |
| last_health_status / last_health_at | TEXT | 最近一次健康检查结果，仅作诊断，不替代运行时检查     |
| created_at / updated_at             | TEXT | RFC 3339 UTC                                         |

Adapter 注册信息以该表为事实源；敏感凭据不进入 manifest。每次创建 Agent Run 都重新校验 Adapter 状态、manifest 版本、执行文件和平台隔离能力，不能只依赖上次健康结果。

**inbox_items** - 收件箱项（schema v12 已实现手工事实）

| 字段                                  | 类型    | 约束 / 说明                                                                        |
| ------------------------------------- | ------- | ---------------------------------------------------------------------------------- |
| id                                    | TEXT    | PRIMARY KEY (UUID)                                                                 |
| kind                                  | TEXT    | manual / event / reminder；当前创建 API 仅接受 manual                              |
| title / summary                       | TEXT    | trim 后标题 2–200 字符；摘要最多 10,000 字符                                       |
| source_entity_type / source_entity_id | TEXT    | 公开创建 API 固定 manual/null；内部已使用 reminder/task_artifact/task/task_due     |
| source_deleted_at                     | TEXT    | 来源被允许删除后的时间；保留最小快照并在 UI 标记来源不存在                         |
| source_event_key                      | TEXT    | nullable；非空值由部分唯一索引去重；当前手工 API 禁止设置                          |
| priority                              | TEXT    | P0 / P1 / P2 / P3                                                                  |
| status                                | TEXT    | open / tracking / resolved / dismissed；当前创建固定 open                          |
| resolution_policy                     | TEXT    | manual / all_required_tasks_done；当前 API 仅接受 manual                           |
| due_at                                | TEXT    | 可选截止时间                                                                       |
| read_at / triaged_at                  | TEXT    | 展示与分诊时间，独立于主状态                                                       |
| snoozed_until                         | TEXT    | 稍后提醒时间                                                                       |
| resolved_at / resolved_by_actor_id    | TEXT    | 解决记录                                                                           |
| resolution_reason                     | TEXT    | 解决原因始终非空；manual/forced 由 owner 填写，未来 automatic 由 system 写稳定说明 |
| resolution_mode                       | TEXT    | manual / forced / automatic；当前公开 resolve 只写 manual                          |
| dismissed_at / dismissed_by_actor_id  | TEXT    | 忽略记录                                                                           |
| dismiss_reason                        | TEXT    | 忽略原因                                                                           |
| payload_json                          | TEXT    | 必须是 JSON object；当前 UI 不编辑                                                 |
| version                               | INTEGER | 乐观并发版本                                                                       |
| created_at / updated_at               | TEXT    | RFC 3339 UTC                                                                       |

当前手工项的 `source_entity_id / source_event_key / source_deleted_at` 均为空；有效编辑、snooze/unsnooze、resolve/dismiss 会在首次分诊时写 `triaged_at`，单纯 read 不写 triaged。resolve/dismiss 清除 snooze 但不隐式 read；reopen 保留 read/triaged 并清除终态字段。schema v23–v25 已为 Task Artifact、Task 阻塞与 Task 临期实现多态来源和删除协调：Inbox Item 处于 open/tracking 时阻止来源删除；归档后删除保留最小快照、写 `source_deleted_at` 并追加 Workflow Event。其他来源继续逐项冻结契约。

**inbox_item_tasks** - 收件箱项与任务关联（schema v13 / T-11A2 已实现）

| 字段                                               | 类型    | 约束 / 说明                                                                 |
| -------------------------------------------------- | ------- | --------------------------------------------------------------------------- |
| id                                                 | TEXT    | PRIMARY KEY (UUID)                                                          |
| inbox_item_id                                      | TEXT    | Inbox Item 外键                                                             |
| task_ref_id / task_id                              | TEXT    | 不可变原 Task ID，以及 nullable 实时 Task 外键；Task 删除后 `task_id` 置空  |
| task_title_snapshot                                | TEXT    | 建立关系时保留的 Task 标题；Task 删除后仍可解释历史                         |
| relation_type                                      | TEXT    | created / linked；A2 手工关联固定 linked，created 由 T-11C 拆分使用         |
| is_required                                        | INTEGER | 当前关系是否属于必需工作；A2 可修改并审计，T-11C 自动策略只读取活动必需关系 |
| position                                           | INTEGER | 活动关系稳定顺序；单条关联默认追加                                          |
| linked_by_actor_id / linked_at                     | TEXT    | 建立关系的 Actor 与 RFC 3339 UTC 时间                                       |
| unlinked_by_actor_id / unlinked_at / unlink_reason | TEXT    | 带原因软解除；三者成组出现，不硬删除历史关系                                |

活动关系由 `unlinked_at IS NULL` 且 Task 仍存在派生；对 `(inbox_item_id, task_ref_id)` 建立仅覆盖活动行的唯一约束。A2 的 GET 使用实时 Task JOIN 派生 required 总数、done 数、剩余数以及 blocked/waiting_review 提示；不把 Task 状态或百分比写回关系表。重新关联同一 Task 时创建新行，不复用或覆盖旧记录。活动关系阻止 Task 硬删除；软解除后 Task 可删除，实时引用置空但原 ID、最小快照和关系事件继续保留。T-11C 才启用 `all_required_tasks_done` 自动解决，且必须有至少一个活动必需 Task。

**task_assignments** - 任务分派历史（schema v7 数据与 T-18C 操作 API/UI 已实现）

| 字段                        | 类型 | 约束 / 说明                                                  |
| --------------------------- | ---- | ------------------------------------------------------------ |
| id                          | TEXT | PRIMARY KEY (UUID)                                           |
| task_id / actor_id          | TEXT | 任务与负责人；Task 删除级联清理，Actor 删除受限              |
| role                        | TEXT | assignee / reviewer                                          |
| assigned_by_actor_id        | TEXT | 执行分派的 Actor；外键删除受限                               |
| assigned_at / unassigned_at | TEXT | 非空生效时间与可选结束时间；`unassigned_at IS NULL` 表示活动 |
| reason                      | TEXT | 最多 1000 字符，默认空字符串                                 |

Assignment 不设置自己的工作完成状态；同一任务、同一 role 同时只允许一个 `unassigned_at IS NULL` 的活动记录。新建活动 Assignment 时负责人和分派人必须 active；v0.1 assignee 只允许 owner/person，reviewer 只允许 owner，system/agent 当前不可分派。已结束记录的身份字段、结束时间和原因不可覆盖，保留 Task 存续期间的责任历史。查询、首次创建、原子改派和结束 API 已实现，并通过 Task `If-Match`/`version` 保护整个责任聚合；Task 转为 `done` 或 `cancelled` 会结束全部活动记录，重新打开不恢复旧分派，终态任务不能创建或改派 Assignment。当前 Task DELETE 是永久删除整个 Task 聚合，因此 `task_id ON DELETE CASCADE` 会一并删除其 Assignment；相关 Workflow Event 保留，但 `assignment_id` 按 `ON DELETE SET NULL` 清空，事件 JSON 快照仍可解释当时责任，文档不承诺 Assignment 跨 Task 硬删除保留。

**agent_runs** - 本地 Agent 单次执行

| 字段                                     | 类型           | 约束 / 说明                                                     |
| ---------------------------------------- | -------------- | --------------------------------------------------------------- |
| id                                       | TEXT           | PRIMARY KEY (UUID)                                              |
| task_id / assignment_id / agent_actor_id | TEXT           | 执行上下文                                                      |
| triggered_by_actor_id                    | TEXT           | 发起人                                                          |
| parent_run_id / attempt                  | TEXT / INTEGER | 重试链路                                                        |
| status                                   | TEXT           | queued / running / succeeded / failed / cancelled / interrupted |
| input_snapshot_json                      | TEXT           | 脱敏输入快照                                                    |
| output_summary / output_manifest_json    | TEXT           | 输出摘要与清单                                                  |
| error_code / error_message               | TEXT           | 结构化错误                                                      |
| queued_at / started_at / finished_at     | TEXT           | 生命周期时间                                                    |
| idempotency_key                          | TEXT           | 单次执行去重                                                    |
| version                                  | INTEGER        | 乐观并发版本                                                    |

重试必须创建新记录，不覆盖失败或中断记录。

Task Artifact 的已实现 schema 见本章 `task_artifacts` 主表。未来 Agent Run 不直接改写既有 Artifact facts；应通过同一 submit-output 领域服务创建 Submission/Artifact，并以 Workflow Event 或新增显式关联表达 Run 来源，具体 schema 需 T-19 ADR，不能回写 migration 009。

**workflow_events** - 追加式工作流审计（schema v9 已实现 Task D2 时间线）

当前字段包括 `id`、`aggregate_type`、`aggregate_id`、`action`、可选 `actor_id`、`assignment_id`、`submission_id`、`artifact_id`、`agent_run_id`、`request_id`、`previous_json`、`current_json`、`command_seq` 和 `created_at`。Actor/Assignment、Task 六命令、策略变化、输出提交、接受、返工、待审撤回、Artifact 删除、Project 创建/资料/生命周期/删除命令、手工 Inbox 创建/编辑/分诊命令、Inbox–Task 关系命令，以及 v7/v9 迁移回填都写入该表。Inbox action 为 `created / updated / read / snoozed / unsnoozed / resolved / dismissed / reopened / task_linked / task_requirement_changed / task_unlinked`，由 `GET /inbox-items/:id/events` 分页读取并返回 owner Actor 摘要；关系 ID、Task 引用、required、position 和解除原因保存在不可变前后快照中。Project action 使用 `project_*` 前缀，由 `GET /projects/:id/events` 稳定分页读取 owner、request 和前后快照。`command_seq` 对同一命令内多个事件稳定排序；历史迁移事件允许为空。schema v9 校验 Submission/Artifact 与 Task 聚合及彼此批次一致。普通 API 不提供修改或删除，trigger 只允许 Task 聚合硬删除时因外键清空已删除成员的 assignment/submission/artifact ID，JSON 快照保持不变；Project 删除事件没有聚合外键，继续留在业务导出中。Agent 等其他聚合仍待各纵切接入。

**reminders** - 本地提醒调度（schema v14 / T-11A3 已实现）

字段包括 `id`、`source_entity_type / source_entity_id`、`title / summary / priority`、`trigger_at`、`status`、`source_event_key`、`created_by_actor_id`、`fired_at / inbox_item_id`、`cancelled_by_actor_id / cancelled_at / cancel_reason`、`version` 和创建/更新时间。公开创建固定 `source_entity_type=manual` 且来源 ID 为空；状态为 `scheduled / fired / cancelled`。scheduled 不携带任一终态事实；fired 必须成组记录触发时间和来源/事件键均匹配的 Reminder Inbox Item；cancelled 必须成组记录 owner、时间和非空原因。身份字段不可修改，fired/cancelled 终态不可变，记录不允许硬删除。

创建时生成唯一稳定键 `reminder:<id>:due`。创建、编辑/改期、取消分别写 `reminder_created / reminder_updated / reminder_cancelled` owner Event；Sidecar 到期投影写 system Inbox `created` Event 和 `reminder_fired` Event。启动先补扫，运行中每 15 秒按 `trigger_at/id` 扫描最多 100 条；每个 Reminder 的 Inbox 创建/复用、事件和 fired 更新在同一事务提交，稳定键与条件更新使重复扫描和重启不会重复生成。第一阶段只支持一次性应用内提醒；应用关闭时不后台唤醒，重复规则和系统原生通知留待后续。

**idempotency_keys 增量字段** - 幂等重放契约

schema v4 已增加 `request_hash`、`response_status` 和 `response_body`。Task/Project/person/手工 Inbox/Reminder 创建、Client contact 关联/解除、Assignment 创建/改派/结束、Task 六命令、D2 submit-output/review/Artifact delete、Inbox 单条命令/read-all、Inbox–Task 关系 POST/PATCH/DELETE 与 Reminder cancel 均保存规范化请求摘要和首次响应，安全重放不重复创建业务事实或 Event，异体复用返回冲突。Inbox/Reminder/Client contact 命令摘要包含 expected version，并在当前版本检查前重放。文件提交摘要包含每项规范化 payload、MIME、size 和 SHA-256，不把文件正文写入幂等表。当前查询作用域为 key + endpoint；调用 Actor/过期策略仍待未来迁移评审。旧记录缺少安全快照时返回 `IDEMPOTENCY_REPLAY_UNAVAILABLE`。

**app_settings** - 版本化非敏感用户设置（schema v16 / v0.1-A 已实现）

字段为 `key`、`value_json`、`schema_version`、`version`、`updated_by_actor_id`、`updated_at`。固定 key 为 `workspace / general / appearance / focus / storage`；`workspace` 保存工作区品牌名称和受控头像引用，不作为 owner 身份，owner 名称只存在于 actors；`storage.low_space_threshold_gib` 默认 1，只接受 1–100 整数。迁移不插入默认行；GET 对缺失 key 返回服务端默认值、`stored=false / version=0`，供后续一次性迁移判断。PATCH 每项要求完整 JSON object 和 `expected_version`，服务端拒绝未知/缺失/null 非空字段、越界参数、Data URL 与未受控头像路径；1–5 项在同一事务中保存，任一冲突整批回滚。成功事件仅记录 stored/version/schema 元数据，不记录设置值。敏感凭据、Sidecar 会话令牌和 Agent 单次能力令牌不得进入该表或事件。

**workspace_avatars / workspace_avatar_deletion_tombstones** - 受控工作区头像（schema v27 / v0.1-B 已实现）

`workspace_avatars` 保存 `id`、固定 `avatars/<id>.<extension>` 相对路径、`png/jpg/webp` 扩展名、MIME、1–2 MiB size、SHA-256、`verified/missing/mismatch` 完整性事实、检查/创建时间和成组删除事实。部分唯一索引保证最多一个 active 头像；app_settings trigger 保证非空 `avatar_ref` 指向 active 行；四向 trigger 阻止与 Task Artifact、Client Attachment、Project Attachment 复用同一 UUID。替换或移除时先写不可变 tombstone，再软删除旧行；硬删除头像事实和 tombstone 均被禁止。业务 JSON 导出头像元数据但不含文件正文，删除墓碑属于运行维护事实并被排除。

**task_saved_views** - 任务筛选保存视图（schema v17 / T-07D 已实现）

字段为 `id`、大小写不敏感唯一的 `name`、`definition_json`、`schema_version`、`version` 和创建/更新时间。工作区最多 20 个；名称 1–80 字符，定义 JSON 最大 16 KiB，当前 `schema_version = 1`。定义严格保存搜索、状态、优先级、类型、Project/Client/Tag 引用、精确/范围计划日期、截止范围和排序，不保存页码、选择、折叠状态或查询结果。视图与引用对象不建立外键，避免删除业务对象时静默删除用户配置；应用时始终按当前 Task→Project→Client/Tag 事实重新查询。

`GET/POST /task-saved-views` 和 `PATCH/DELETE /task-saved-views/:id` 提供列表、创建、`If-Match` 更新和 `confirm=true` 删除。服务端与任务列表共享枚举、UUID、日期、范围及排序语义；计划精确日期与计划范围互斥。前端选择即完整应用并回到第一页，更新和删除使用当前版本，冲突只刷新视图列表，不自动覆盖。

---

## 7. 数据持久化方案

### 7.1 应用数据目录

所有业务数据保存在 Tauri Path API 返回的应用专属目录中，不依赖当前工作目录，也不写入安装目录。具体物理路径由操作系统和应用 Bundle Identifier 决定，业务代码不得硬编码路径。

```text
appDataDir/
├─ .opc-sidecar-run.lock     # DB 父目录固定运行锁文件；所有权由 OS 独占锁表示
├─ opc-workspace.db          # SQLite 主数据库
├─ attachments/              # 历史预留目录；当前业务不使用
├─ artifacts/                # 已实现：Task/Client/Project 文件与 Workspace Avatar 共享受控根
│  ├─ .opc-artifact-store-v1 # format_version + database_id + store_id marker
│  ├─ .opc-artifact-store.lock # Sidecar 进程级独占 lease
│  ├─ .staging/              # 上传暂存与启动清理
│  ├─ objects/               # 固定 objects/<object-id> 文件对象，跨业务表 ID 唯一
│  ├─ avatars/               # 固定 avatars/<avatar-id>.<png|jpg|webp>，最多一个 active
│  ├─ .trash/                # 删除事务补偿，不是用户回收站
│  └─ .quarantine/           # 无引用受控候选隔离，不自动永久删除
├─ invoices/                 # 预留；发票 PDF 未实现
├─ backups/                  # 已实现创建/列表/校验/演练/重启恢复/确认删除；业务 JSON 直接下载，不写入此目录
│  ├─ <backup-id>/           # manifest + database snapshot + marker + 全部 active objects/avatars
│  └─ .restore-pending-v1/   # 已确认但尚未在下次 Sidecar 启动应用的私有包与严格计划
└─ config/                   # 预留；当前非敏感设置与头像均不依赖此目录

appLogDir/
├─ startup-incidents-v1.json # 已实现：启动前白名单故障 journal；健康补偿后删除
├─ .startup-incidents-invalid-*.json # 损坏 journal 隔离，不自动读取
├─ opc-sidecar.log           # 已实现：Sidecar 当前脱敏运行日志，单文件最多 5 MiB
├─ opc-sidecar.log.1…3       # 已实现：最多 3 份轮转归档
└─ opc-workspace.log         # 已实现：Tauri 桌面壳白名单 JSONL 轮转日志
```

- 应用升级只替换程序文件，不覆盖 `appDataDir`
- 卸载流程默认不主动删除业务数据；彻底删除数据必须由用户执行独立的明确操作
- API 密钥、Sidecar 会话令牌等敏感信息不写入普通配置文件，持久凭据使用操作系统安全存储

### 7.2 SQLite 生命周期与迁移

1. Tauri 启动时先获取桌面单实例锁；Go Sidecar 在检查 pending restore、迁移或打开 SQLite 前，对数据库父目录固定 `.opc-sidecar-run.lock` 获取非阻塞 OS 独占锁，冲突立即失败且不接触数据库
2. Tauri 通过环境传递数据库路径、`appDataDir/artifacts/`、动态端口和随机会话令牌，避免敏感值暴露在进程命令行
3. Go Sidecar 在运行锁保护下应用 pending restore、执行版本化迁移并建立 SQLite 连接，再获取独立 Artifact root 锁并依据 Task/Client/Project 文件、Workspace Avatar 与 tombstone 事实声明、校验和协调受控文件 store；全部成功后才输出 ready
4. **已实现 / 部分诊断待补**：已有工作区在首个带 `-- migration: destructive` 文件头标记的迁移前，自动创建并完整校验一致性数据库+受控文件回滚包；失败时不执行破坏性 SQL并拒绝启动。迁移失败恢复界面仍未实现
5. 应用退出时先停止接收新请求，等待进行中的写事务结束，执行必要 checkpoint 后再关闭数据库和 Sidecar；shutdown 已持有 child handle 时，ready 超时任务不伪造 exited，也不抢走优雅等待与兜底终止职责
6. 数据库版本记录在 `schema_migrations` 表中；应用版本、Sidecar 版本和数据库版本必须建立兼容关系

schema v30 是普通非破坏性迁移：给 `task_submissions` 增加非空 origin，既有 v29 行默认 manual；trigger 限制 child_rollup 必须由内置 system、非 inferred 且不能拥有 Artifact，并把 origin 纳入 Submission 历史不可变保护。迁移和 Sidecar 启动不扫描或补写历史父任务层级；只有迁移后的相关业务写命令触发父任务 reconciliation。

schema v31 同样是普通非破坏性迁移：只为 `client_activities` 的 Project Workflow Event 系统来源增加部分唯一索引，不改现有行、不增加 `source_id` 外键，也不回填历史 Project 事件。正常 v30 工作区公开 API 不允许写 system reference；若数据库已被绕过 API 人工写入重复的该来源，迁移会安全失败并回滚，而不会静默合并事实。后续迁移从 `032_*` 继续。

### 7.3 备份策略

版本边界：手动一致性备份的创建/低空间准入、内部列表、完整校验、隔离恢复演练、恢复前自动回滚包、破坏性迁移前自动回滚包、下一次 Sidecar 启动原子替换、桌面一键安全重启、启动后恢复结果诊断、确认删除，以及业务 JSON/含文件业务 ZIP 的空工作区同 schema 安全导入导出已经交付；容量准入只覆盖手动 `POST /api/v1/backups`，不覆盖导入、恢复或迁移内部创建的自动回滚包。数据库打开前的实时进度/恢复页仍需桌面壳实现。v0.3 只增加可配置计划、外部目录、保留策略、CSV 映射和高级冲突导入工具。

1. **v0.1 一致性快照基础**
   - **已实现（手动）**：在维护写锁中使用 `VACUUM INTO` 创建一致性快照，不直接复制正在使用的 WAL 数据库文件；普通 API、Focus heartbeat 与 Reminder 扫描共享维护读锁
   - **已实现**：用户可手动创建；已有工作区在首个显式 destructive 迁移前自动创建同规格回滚包，备份失败不执行破坏性 SQL；v0.1 不提供每日调度
   - **已实现（仅手动 `POST /api/v1/backups`）**：维护写锁与备份互斥锁内先处理同键同请求幂等重放；仅未命中时才在任何 staging/`VACUUM INTO` 前，以 `max(PRAGMA page_count × page_size, 当前数据库文件大小)` + 全部 active Task/Client/Project/Avatar 受控文件 + marker 实际大小 + manifest 1 MiB 上界估算载荷，再增加向上取整 20% 且最低 64 MiB 余量。每个 active 文件先经受控路径解析、Lstat/open 普通文件复核，实际大小必须与数据库登记一致；复制链另限制最多读取登记大小 + 1 字节，避免检查后的增长写满目标卷。只对 backup root 探测可用/总字节，精确等于需求允许继续；估算溢出、PRAGMA/文件/list 无法确认、探测报错、总量为零或可用量大于总量均按无法确认拒绝
   - **已实现（门禁结果）**：可用空间小于需求返回 HTTP 507 `BACKUP_SPACE_INSUFFICIENT`，无法确认返回 HTTP 503 `BACKUP_CAPACITY_UNAVAILABLE`；统一错误响应和 Inbox 不含路径、盘符、精确容量、note 或底层错误。两类拒绝不创建 staging/新包、不改变业务数据，也不投影 generic `backup:create` incident；UI 分别提示清理备份位置/旧备份或刷新容量状态/确认本地存储可用，保留未成功提交的 note，不伪造成功、不自动重试
   - **明确不在本门禁范围**：导入前、恢复安排前和破坏性迁移前的自动回滚包调用内部备份创建链，仍按各自既有失败闭锁和完整校验处理，不能声称已经应用本手动容量门禁
   - **已实现**：快照先写 backup root 同卷 `.staging-<uuid>`；每个备份包含 SQLite 一致性快照、身份 marker、Task/Client/Project 的全部 active objects 与 Workspace Avatar，以及 app/commit/API/schema 版本、相对路径、大小和 SHA-256 manifest
   - **已实现**：发布前执行 `quick_check`、`foreign_key_check`、schema/identity/全部 active 受控文件元数据交叉校验并拒绝缺失、篡改或额外文件；通过后原子重命名为 `backups/<backup-id>/`
   - **已实现**：创建支持可选幂等键，列表展示上次校验事实，显式校验可重新逐字节验证并刷新 `verified_at`；绝对路径和幂等键明文不进入响应/manifest

2. **v0.1 基础手动导出**
   - **已实现**：随时下载 format v1 JSON 业务包；在单事务一致视图内覆盖 Client/Activity/Attachment、Project、Task/Tag、Actor/Assignment/Event、Submission/Artifact 元数据、Focus、Inbox/关系、Reminder、设置和保存视图
   - **已实现**：显式白名单与稳定表/列/行结构；包带 `schema_version`，排除令牌、绝对路径、workspace identity、幂等响应、迁移/墓碑及派生 totals，任一表失败则不返回部分文件
   - **已实现**：基础 JSON 只记录 active 受控文件数量/字节摘要与数据库元数据；另有同步 ZIP 以 manifest/hash 和受控相对路径携带全部 active 文件正文。ZIP 目前只支持导出，完整恢复继续使用一致性备份

3. **v0.1 恢复与基础导入**
   - **已实现**：再次完整校验后在唯一隔离临时根复制整个包，用当前迁移器打开数据库副本，执行最终 quick/foreign-key/schema/identity 和全部 active 受控文件完整验证；临时句柄关闭后清理临时根，不修改源包或当前数据
   - **已实现**：用户二次确认后，在维护写锁中再次演练目标并自动创建、完整校验当前状态回滚包；pending 发布成功后冻结普通 v1 API、Focus heartbeat 与 Reminder 扫描，直到关闭重开
   - **已实现**：下一次 Sidecar 启动在正式数据库和 Artifact lease 打开前准备/迁移目标副本，以同父目录 new/old 路径交换 SQLite、WAL/SHM 与完整 objects；最终验证失败恢复旧资源并隔离计划
   - **已实现**：最终验证成功后先把 pending 原子重命名为 applied 提交点，再清理 old/new 路径；清理警告不导致已恢复数据重复应用。同目标安排可安全重放，不同 pending 目标返回冲突
   - 导入前校验格式、版本、校验和及可用磁盘空间
   - **已实现**：恢复计划成功挂起后，桌面设置页调用无业务参数的 `restart_application`；Tauri 先等待受管 Sidecar 真实退出，再重启整个应用。若使用外部开发 Sidecar 或退出不能确认，则不重启并提示手动处理
   - **已实现**：健康启动后设置页通过只读诊断 API 展示待重启、本次已恢复、applied 清理残留、失败隔离和无效记录；不暴露路径/底层错误，也不自动删除
   - 数据库打开前的实时恢复进度页和脱敏错误日志入口仍待 Tauri 壳实现

4. **v0.3 可配置备份与高级导入**
   - 增加每日首次满足条件时执行、错过计划后的启动补偿、可配置保留策略和最近 30 份默认值
   - 引导用户选择独立外部备份目录，用于防范应用数据目录或磁盘损坏
   - 增加任务、客户、财务等 CSV 导出/映射导入和冲突预览
   - 提供周期性可恢复性抽检、失败提醒和历史执行记录

### 7.4 数据安全

- 当前阶段已实现的核心业务、Actor、任务分派、Submission/Artifact、手工 Inbox Item、已有 Task 关系和一次性 Reminder/到期 Inbox 投影均仅保存在本地，不主动访问线上服务；非 Reminder Inbox 来源投影、批量拆分/分派、自动解决以及 Agent Run 尚未实现
- Go Sidecar 仅绑定 `127.0.0.1` 动态端口，并校验由 Tauri 生成的启动期随机会话令牌
- 数据目录使用当前操作系统用户权限，日志不得记录令牌、完整客户信息、发票内容或第三方连接器请求正文
- Artifact root 必须带规范的 `format_version / database_id / store_id` JSON marker；database ID 不可变，store ID 首次声明后在数据库中一次性绑定，并由 Sidecar 进程独占锁定。数据库不能换用另一 root，root 不能被另一数据库接管；路径不能为卷根或穿越 symlink/reparse point。数据库只保存 `objects/<artifact-id>` 相对路径，下载前重验大小和 SHA-256；恢复 active trash 前同样核对 size/SHA，错配隔离并标记 mismatch。删除 tombstone 让授权删除可验证，无引用但无法验证授权的候选只移入 quarantine，不自动永久删除
- person Actor 不包含登录凭据，也不会收到远程任务；本地 Agent 必须默认禁网并只访问单次 Run 明确授权的资源，跨平台沙箱/网络阻断未验证通过时正式执行保持禁用
- 若未来引入更新检查、第三方连接器、远程模型或多人协作，必须另行评审网络访问、身份、权限、同步、密钥和数据外发边界，并由用户显式启用
- 应用锁定用于阻止界面访问；数据库静态加密使用 SQLCipher，计划在 MVP 后提供
- 备份归档可设置加密密码；密码遗失时明确提示无法恢复

---

## 8. 部署与分发

### 8.1 桌面应用分发

通过 Tauri Bundler 打包为各平台原生安装包。每个安装包必须同时包含 React 构建产物和对应平台/架构的 Go Sidecar，最终用户无需安装 Go、Node.js、Rust 或 Docker：

| 平台    | 格式                                                    |
| ------- | ------------------------------------------------------- |
| Windows | .exe 安装程序、.msi 企业安装包                          |
| macOS   | .dmg 镜像（Universal：Intel + Apple Silicon）           |
| Linux   | .deb（Debian/Ubuntu）、.rpm（Fedora/RedHat）、.AppImage |

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
4. Sidecar 先取得数据库父目录固定运行锁，再处理 pending restore、执行版本化迁移、打开 SQLite 并输出实际监听端口
       ↓
5. Tauri 调用 /health 完成就绪检查
   ├─ 健康 → WebView 加载内置 React 页面，注入运行期 API 地址
   └─ 超时 → 已显示恢复页 v1，可重查、打开日志或安全重启；从备份选择恢复仍待实现
       ↓
6. 用户开始使用，核心功能从首次启动起即可离线运行
```

首次启动不下载业务运行时或后端镜像。PRD v6.5 当前阶段不提供线上更新或第三方连接；安装、升级和核心使用均可在离线环境完成。

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
- Sidecar 异常退出时托盘显示恢复/错误状态，并复用当前最多两次有界自动重启及手动打开诊断页入口

### 8.5 全局快捷键

| 快捷键             | 功能          |
| ------------------ | ------------- |
| ⌘K / Ctrl+K        | 打开命令面板  |
| ⌘N / Ctrl+N        | 新建任务      |
| ⌘⇧F / Ctrl+Shift+F | 开始/暂停专注 |
| ⌘1-9 / Ctrl+1-9    | 快速切换页面  |
| ⌘W / Ctrl+W        | 最小化到托盘  |

---

## 9. MVP 范围与迭代计划

### 9.1 MVP（v0.1）范围

**目标**：交付一人公司日常工作的本地闭环；Actor 与新版收件箱扩大了原 v0.1 范围，完成详细任务拆分后重新估算周期，不沿用旧“8 周”承诺。

**包含功能**：

| 模块             | MVP 目标功能                                                                                   | 当前状态（2026-08-29）                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| ---------------- | ---------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 今日工作台       | 三栏布局、今日任务列表、手动排序、逾期及临期提示、任务详情操作、右侧概览面板                   | **部分完成**：日期导航、真实统计/四组任务、精确日期/未排期按钮式排序、四组共享同日/跨日期拖拽、空精确日期/未排期落点、任意日期/未排期安排、安全执行快捷操作、编辑/版本化确认删除入口、共享任务详情、服务端逾期/未来 24 小时临期快捷筛选、真实 Focus 概览与 completed-only 汇总已接通；收入与客户动态待实现                                                                                                                                                                                 |
| 任务管理         | 完整 CRUD、父子任务、状态流转、标签、项目关联、完成条件、人工验收、列表视图、搜索和快捷键      | **部分完成**：schema v6–v9 事实、责任、六状态、时间线与 manual Submission/Artifact 验收，快照幂等、`ETag`/`If-Match`、分页筛选、层级、批量/排序和受控文件均已实现；schema v11 Focus 工时已接入；schema v30 已交付直属子任务汇总、system child_rollup、待验收/失效撤回/accepted 后子任务失效重开；Today 与任务页均已消费计划组拖拽排序，六状态看板已接真实读取/筛选/分页/选择/详情和受控跨列生命周期命令                                                                                    |
| 项目管理         | 项目卡片、状态流转、项目进度、项目详情（任务列表）                                             | **部分完成**：CRUD、分页/搜索/状态筛选、创建幂等、乐观锁、受控状态、归档恢复、确认硬删除、卡片/详情、任务派生进度/工时、项目任务树/平铺切换及服务端搜索/状态/优先级/类型/标签/排期筛选与分页、客户选择/筛选、可编辑人工笔记、受控附件、产出聚合、活动时间线、项目级 Focus 报告/终态历史、显式 follow-up、Task 阻塞/临期与 Project 完成节点→Inbox 已实现；财务与真实里程碑增强待实现                                                                                                        |
| 客户管理         | 客户列表表格、客户详情、基本 CRUD                                                              | **部分完成**：基础资料 CRUD、分页/搜索/状态筛选/排序、创建幂等、并发控制、基础详情、受约束删除、Project 关联、本地活动、受控附件和 person 显式关联已实现；外部来源投影、回访和财务待实现                                                                                                                                                                                                                                                                                                   |
| 收件箱与人工编排 | 本地 Actor 基础、事件受理、已读/稍后、任务拆分/关联、人工分派、验收/返工、审计和自动解决       | **部分完成**：schema v12–v15 已交付受理分诊、Reminder、Task 关系/拆分编排；T-11F 已交付 Sidebar/Today 运营计数与风险深链；schema v23–v25 已交付 follow-up Artifact、Task 阻塞与 Task 临期来源投影/删除协调；备份四类操作性失败、数据库启动/迁移、Sidecar 启动、运行期数据库操作失败和可配置低空间监测已安全投影，手动备份容量准入拒绝明确不投影 generic incident；物理卷同卷去重及无路径手动容量检查已在设置页交付，卷级趋势仍待实现                                                       |
| 专注模式         | 番茄钟、环形进度、工时记录、连续天数统计、暂停本应用通知、系统专注模式引导                     | **Core A+B+C、D1、D2a、Project 详情读取与 D2b 分析已完成**：schema v11 Session/interval、任务绑定、绝对时间、心跳/恢复、并发/幂等、精确工时、Today 汇总、终态历史、7/30 天/本月/自定义趋势、Streak、项目/标签/小时分布、周几×小时热力图与 Task/Project 详情记录已实现；原生通知/托盘/DND 待实现                                                                                                                                                                                            |
| 全局功能         | 左侧导航、系统托盘、全局快捷键、自动启动、Go Sidecar 生命周期和健康检查                        | **部分完成**：导航、WebView 内快捷键、单实例、Sidecar 生命周期和健康检查已实现；托盘、系统全局快捷键、自动启动待实现                                                                                                                                                                                                                                                                                                                                                                       |
| 数据持久化       | Tauri `appDataDir`、SQLite 迁移、受控文件、手动/迁移前一致性备份、基础 JSON 导入导出与原子恢复 | **部分完成**：正式/开发隔离、WAL、外键、schema v31、受控 Task/Client/Project 文件与 Workspace Avatar、版本化存储阈值、数据库身份强绑定、手动备份低空间准入、手动/迁移前备份恢复完整闭环、桌面安全重启、启动后恢复结果诊断，以及业务 JSON/含文件业务 ZIP 的空工作区同 schema 安全导入导出已交付；v30 非破坏性保留 v29 Submission 为 manual 且不回填历史父任务，v31 不回填历史 Project 客户动态；门禁仅适用于手动 HTTP 创建，数据库打开前实时进度页仍待桌面壳实现；计划和高级冲突导入归 v0.3 |

**MVP 不包含**（后续版本）：

- 项目看板与任务看板高级编排（任务六状态读取和受控跨列生命周期已实现）
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

| 版本                   | 核心功能                                                                                                        | 开发周期                          |
| ---------------------- | --------------------------------------------------------------------------------------------------------------- | --------------------------------- |
| **v0.1 MVP**           | 今日、完整任务纵切、项目、基础客户、专注持久化、本地 Actor、收件箱人工受理/拆分/分派/验收、备份恢复和桌面可靠性 | 完成详细任务拆分后重新估算        |
| **v0.2 本地编排版**    | 本地 Agent Adapter 与 Run 生命周期、产出、取消/重试、人工审核返工和预设自动化                                   | 待 v0.1 完成后估算                |
| **v0.3 规划增强版**    | 路线图、内容日历、自动备份配置、数据导入工具、快捷键自定义和统计增强                                            | 待 v0.2 完成后估算                |
| **v0.4（后续业务版）** | 客户回访计划与提醒、收入/支出账本、财务统计、发票 CRUD 与 PDF                                                   | 待 v0.1 完成后估算                |
| **v1.0**               | 多币种支持、数据加密、自动化规则自定义、API 开放平台、性能优化、跨平台测试完善                                  | 6 周                              |
| **待开发（版本待定）** | AI 助手、本地模型 Adapter、本地知识库、文档导入与索引、带来源检索、权限/隐私控制和质量评测                      | 完成核心 MVP 与安全存储后单独估算 |

### 9.3 MVP 技术验收标准

- [ ] Windows/macOS/Linux 三平台安装包均内置正确架构的 Go Sidecar，可在未安装开发工具和 Docker 的干净系统运行
- [ ] 已完成初始化后的冷启动 P95 < 2 秒（参考设备连续测试至少 20 次，从点击图标到界面可交互）
- [ ] 首次启动无需下载业务运行时，数据库初始化和 Sidecar 就绪时间 P95 < 5 秒
- [ ] 安装包体积 < 30MB（不含操作系统 WebView 运行时）
- [ ] 稳态运行内存 P95 < 200MB（Tauri + WebView + Go Sidecar 合计）
- [ ] 生产环境 Sidecar 仅绑定 `127.0.0.1` 动态端口，未携带有效会话令牌的请求被拒绝
- [ ] 应用正常退出后不遗留 Sidecar 进程；Sidecar 异常退出时可诊断并按策略重启
- [ ] 应用升级、Sidecar 重启和程序文件替换后，`appDataDir` 中数据完整保留
- [x] WAL 模式下生成的手动备份通过 SQLite quick/foreign-key/schema/identity 与 Artifact size/SHA-256/全集校验
- [x] 手动备份在双锁内先处理幂等重放，再于 staging/`VACUUM INTO` 前按 SQLite/active 文件/marker/manifest + 20%（最低 64 MiB）余量只探测 backup root；不足/不可确认安全拒绝，精确边界放行，拒绝无新包、业务变化或 generic Inbox incident，UI 保留 note 草稿
- [x] 已校验备份能在临时数据根演练，并在自动回滚点保护下于下次 Sidecar 启动前原子恢复；失败恢复旧数据
- [ ] 首次启动后的所有核心功能在断网环境可用
- [ ] 离线更新只接受签名有效、平台/架构正确且版本兼容的安装包；在线 Updater 不属于当前阶段
- [x] ⌘K 命令面板可搜索已交付页面及真实 Task/Project/Client/活动 Inbox，直达可刷新详情/指定设置模块，并具备焦点圈闭与关闭后恢复
- [x] Focus Core 以服务端绝对时间计时，completed Session 的新增完整分钟自动且幂等记录到绑定任务；真实三平台后台/休眠专项验收仍单列为后续风险
- [x] 手工 Inbox 受理/分诊可离线使用；全局未读、时间截止式全部已读、稍后到期、原因归档、终态已读、重开和事件均以真实 API/SQLite 为事实
- [ ] 单机仅存在一个 owner；person 分派明确为本地责任记录，不产生登录、发送或同步行为
- [x] 收件箱拆分、任务关联、分派和审计在同一事务中完成，失败不遗留部分数据
- [x] 父任务自动推进只看直属非取消子任务且要求至少 1 个、全部 done；manual/active assignee/builtin owner reviewer 门禁齐全后仅生成 system child_rollup 并进入 waiting_review，最终仍需 owner 验收
- [x] pending rollup 失效撤回、blocked 来源修正、accepted 后子任务失效重开、manual/changes_requested 不覆盖和 Inbox 显式 required 独立性均有事务级测试
- [x] Reminder 到期事件跨扫描和应用重启不重复生成
- [ ] 其他来源事件跨扫描和应用重启不重复生成；收件箱解决状态由必需任务派生且可追溯

---

## 10. 实施基线、开发流程与实现追踪

> 状态截止：2026-08-29。当前版本是可运行、可扩展的 v0.1 基座；T-18A/B/C/D D1/D2、schema v30 父任务自动待验收、schema v31 Project 生命周期客户动态、T-12 Focus Core A+B+C/D1/D2a/D2b 项目/标签/小时分布与二维热力图、T-13 脱敏诊断包 v1、Go Sidecar/Tauri 壳脱敏轮转日志与打开日志目录、WebView→Sidecar request ID、全局 Sidecar 启动故障恢复页 v1、T-11A1/A2/A3/B/C/F、T-11E follow-up Artifact/Task 阻塞/Task 临期/Project 完成来源、备份四类操作性失败、数据库启动/迁移、Sidecar 启动、运行期数据库操作失败、可配置低空间投影、物理卷同卷去重和无路径手动容量检查、T-06A–H 与截止风险快捷筛选、T-07A–D、Project 详情 Focus 分析/终态历史，以及 T-04B 手动一致性备份低空间准入/备份完整闭环/迁移前自动回滚包/桌面安全重启/启动后恢复结果诊断/业务 JSON 与含文件 ZIP 安全导入导出 v1 已交付，但这不代表内部自动回滚包已应用手动容量门禁，也不代表第 9.1 节的完整 MVP、Focus 原生反馈、卷级趋势、数据库打开前备份选择/实时恢复进度、外部客户来源、Agent 或冲突合并导入已经交付。

### 10.1 文档口径与状态定义

| 状态     | 定义                                                             |
| -------- | ---------------------------------------------------------------- |
| 已完成   | 当前约定范围已有真实入口和实现，并完成与风险相称的测试或构建验证 |
| 部分完成 | 主链路或基座可运行，但同一模块仍有明确的 MVP 能力未接通          |
| 页面骨架 | 路由、原型样式和真实空状态可访问，但按钮或业务数据链路尚未实现   |
| 未开始   | 仅存在产品需求或技术设计，仓库中没有可用实现                     |

当前实现事实以仓库代码、测试和运行结果为准。PRD 中描述的目标接口、插件、迁移或页面，不因出现在文档中就视为已交付。文档和命令说明只使用仓库相对路径，不记录开发者机器的绝对工作目录或临时 HEAD。

| 基线项       | 当前实现                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| ------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 前端         | React 18.3、TypeScript 5.9、Vite 7、React Router 6、TanStack Query 5、Zustand 5、Lucide、Tailwind CSS v4 构建能力及集中式 `styles.css`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| 桌面         | Tauri 2、Rust 1.85、系统 WebView、shell 与 single-instance 插件                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| Sidecar      | Go 1.22+、Gin、GORM、纯 Go SQLite 驱动；构建时 `CGO_ENABLED=0`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| API / Schema | API v1；SQLite schema v31                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| 数据默认值   | 开发数据库默认空白，不自动注入 demo 业务数据                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| 明确边界     | 当前代码不使用 Docker；已实现 Task/Actor/D1/D2、Project、Client、Focus、Inbox/Reminder/Task 编排、已登记来源投影（含 Project 生命周期本地客户动态）、运行期数据库故障、可配置低空间安全投影、物理卷同卷去重和无路径手动容量检查、基础备份闭环、业务 JSON 与含文件 ZIP 的空工作区安全导入导出、Sidecar/Tauri 壳日志及打开目录、WebView→Sidecar request ID、全局启动故障恢复页 v1；未实现 Client 外部来源集成、Focus 原生反馈、卷级趋势、数据库打开前备份选择/实时恢复进度、重复/原生通知、Agent、非空目标/跨 schema 冲突合并导入、AI、知识库、回访或财务；person 只做本地责任记录，线上账号、云同步和远程协作均不在当前范围 |

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

1. 检查 Go 和 pnpm，创建 `.local/dev-data/` 与其 `artifacts/`、`backups/`、`logs/` 子目录。
2. 以 `--dev --db .local/dev-data/opc-workspace.db --artifacts .local/dev-data/artifacts --backups .local/dev-data/backups --logs .local/dev-data/logs --port 9876` 启动 Sidecar。
3. 注入开发会话令牌和精确 Origin 白名单，轮询带鉴权的 `/health`。
4. Sidecar 就绪后启动 Vite；Vite 固定监听 `127.0.0.1:1420`，代理 `/api` 和 `/health`。
5. `pnpm dev` 再启动 `tauri dev`，通过 `OPC_SIDECAR_URL` 复用同一个开发 Sidecar，避免重复打开数据库。
6. 必要子进程异常退出时，统一脚本关闭其余进程树。

开发数据固定在 `.local/dev-data/opc-workspace.db`、`.local/dev-data/artifacts/`、`.local/dev-data/backups/` 与 `.local/dev-data/logs/`，正式桌面数据库/Artifact 与启动 journal 分别固定在 Tauri `appDataDir` / `appLogDir`，两者不得互用。默认启动不传 `--seed`；只有显式 `--dev --seed` 才能创建幂等测试数据。

#### 10.3.2 API 开发约定

新增业务 API 的落地顺序为：迁移与 model → Go handler → `router.go` 注册 `/api/v1` → Go 集成测试 → 前端 model → `client.ts` → TanStack Query hook → 页面交互测试。

- Sidecar 只监听 `127.0.0.1`；生产模式使用端口 `0` 获取动态端口。
- 生产请求，包括 `/health`，必须携带 Bearer 会话令牌。
- v0.2 Agent Runtime 不接受上述 WebView 会话令牌；必须使用 ADR 定义的进程管道或专用路由与单次能力令牌。
- 浏览器请求必须携带匹配精确白名单的 Origin；不允许通配符。
- 前端每次请求生成 UUID v4 并发送 `X-Request-ID`；Sidecar 只接受 UUID、将合法值规范为 canonical lowercase，非法或缺失值替换为新 UUID。响应头和统一错误体 `{code, message, request_id}` 使用同一值；前端网络/超时错误沿用出站值。
- JSON 请求限制为 1 MiB，拒绝未知字段和多余 JSON 值。
- API 时间戳使用 RFC 3339 UTC，纯日期使用 `YYYY-MM-DD`，金额使用最小货币单位整数。
- 写操作使用事务；可重试创建操作使用 `Idempotency-Key`。

#### 10.3.3 前端状态分层

| 状态类型     | 当前实现方式                     | 示例                                                                                     |
| ------------ | -------------------------------- | ---------------------------------------------------------------------------------------- |
| 服务端事实   | TanStack Query                   | 健康检查、任务、项目、客户、Actor、手工 Inbox、今日统计和四模块非敏感设置                |
| 短期 UI 状态 | Zustand                          | 命令面板、新建任务、设置 draft/preview                                                   |
| 本地兼容配置 | Zustand persist + `localStorage` | 仅读取旧工作区头像 Data URL 作为一次性迁移源；运行态 Blob URL 与其他设置均不持久化到该键 |
| 运行态       | Zustand 内存状态机               | 当前专注阶段、剩余秒数、已完成循环                                                       |

普通 API 单次请求超时 8 秒；Artifact 上传和下载使用 120 秒客户端传输窗口，Sidecar HTTP 读写超时为 180 秒。健康检查每 15 秒刷新；任务和今日统计缓存 10 秒且失败重试 2 次。Tauri 前端通过全局根闸门读取 `sidecar_status`：当前受管状态为 `starting / restarting / ready / error + generation`。非 ready 拦截业务、清除运行期连接并取消/清空 TanStack Query；ready generation 变化补偿遗漏的中间状态，完成清理后重挂业务树。浏览器开发模式不启用该闸门。数据库打开前备份选择/实时恢复进度仍待实现。

#### 10.3.4 SQLite 迁移约定

迁移文件位于 `services/sidecar/internal/database/migrations/`，通过 Go `embed` 编入 Sidecar。当前 schema 为 v31，新增结构只能从 `032_*` 继续追加递增文件：

1. 启动时创建并读取 `schema_migrations`。
2. 按版本升序逐个事务执行迁移。
3. 成功后记录版本、文件名和执行时间。
4. 数据库包含未知版本或同版本文件名不一致时拒绝启动。
5. 每个迁移必须补充数据库测试，不得回写已发布迁移。

当前 `001_initial_schema.sql` 建立核心业务表和索引；002 清理旧固定 demo；003–010 依次增加 Project、Task、Actor/Assignment/Event、Submission/Artifact 与 Client 事实；011–014 依次交付 Focus Session、手工 Inbox、Inbox–Task 关系和一次性 Reminder；015 增加 Inbox 自动结清索引与数据库校验；016 增加空的版本化 `app_settings`；017 增加空的版本化 `task_saved_views`；018 增加客户活动；019 增加受控客户附件；020 增加 Client–person contact 关联；021 增加 Project Note；022 增加受控 Project Attachment、完整性/删除墓碑、跨领域 object ID 唯一保护和 Project 聚合版本传播；023 增加 Task Artifact follow-up 来源保护；024 增加 Task 阻塞事件来源身份、查询索引、不可变和删除协调保护；025 增加 Task 临期来源保护；026 增加系统维护来源身份、活动 incident 唯一索引、不可变快照和禁止来源删除标记；027 增加受控 Workspace Avatar 事实与 guards；028 增加 Project 完成节点 Inbox 来源与删除协调；029 经破坏性迁移闸门扩展 `app_settings.storage` 并保留既有设置事实；030 非破坏性增加 `task_submissions.origin`，既有提交默认 manual，限制 child_rollup 的 system Actor、非 inferred、来源不可变及零 Artifact，不建新索引、不回填父任务；031 非破坏性增加 Project Workflow Event 客户动态来源的部分唯一索引，不回填历史。迁移文件头可声明连续组合的 `-- migration: foreign_keys=off` 与 `-- migration: destructive`；迁移器在固定 `sql.Conn` 上于事务外关闭外键，事务内执行 SQL 与 `foreign_key_check`，提交/回滚后恢复外键，恢复失败与原错误一并返回；破坏性迁移还要求先生成并验证回滚包。数据库使用单物理连接，并启用外键、WAL 和 5000 ms `busy_timeout`；退出时执行 `wal_checkpoint(TRUNCATE)`。

### 10.4 当前基座任务清单

| 任务                                 | 当前状态                         | 本次基线范围                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| ------------------------------------ | -------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| T-01 工程目录与统一脚本              | 已完成                           | pnpm workspace、统一启动、Sidecar 构建脚本、开发数据隔离                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| T-02 Tauri 桌面壳与 Sidecar 生命周期 | 部分完成                         | 窗口、单实例、每代动态端口/令牌、generation、ready/health、两次有界重启与 30 秒预算重置、父管道 EOF、数据库运行锁、前端世代清理、并发 shutdown 和安全应用重启已实现；真实父崩溃/进程树、hard-hung orphan 自动回收、三平台/安装包与数据库打开前恢复进度仍待实现或验收                                                                                                                                                                                                                                                                            |
| T-03 Go 健康检查与 API 基础          | 已完成                           | 版本化路由、安全中间件、统一错误和健康检查                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| T-04 SQLite 初始化与迁移             | 已完成                           | schema v31、PRAGMA、嵌入迁移、demo 清理、Project/Task/Actor/Client/Focus/Inbox/Reminder、设置/保存视图、三类业务附件、Workspace Avatar、存储阈值、Submission origin、父任务 rollup guards、Project→Client Activity 唯一来源与其他来源 guards                                                                                                                                                                                                                                                                                                    |
| T-04B 一致性备份                     | 基础安全闭环完成                 | 专用 backup root、维护写锁、SQLite `VACUUM INTO`、全部 active 受控文件+marker+manifest、完整校验、同卷原子发布、创建幂等、手动创建低空间准入、列表/重新校验、隔离演练、恢复前/迁移前/导入前自动回滚点、pending/applied 提交点、重启原子恢复、桌面安全重启、启动后恢复结果诊断、确认删除、白名单业务 JSON 与含文件 ZIP 的空工作区安全导入导出、设置 UI、全局启动故障恢复页 v1，以及备份操作性失败安全 Inbox 投影已交付；容量门禁仅覆盖手动 HTTP 创建，不覆盖内部自动回滚包。非空目标/跨 schema 合并与数据库打开前备份选择/实时恢复进度待后续纵切 |
| T-05 前端 AppShell 与原型复刻        | 已完成                           | Linear 深色三栏框架、导航、响应式和公共组件                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| T-06 今日工作台                      | 部分完成                         | 日期切换/回到今天、真实日期分组与完整分页、真实任务/统计、共享任务详情、活动 Focus 概览、IANA 当地日 completed-only Focus 汇总和反馈状态                                                                                                                                                                                                                                                                                                                                                                                                        |
| T-07 任务管理纵向闭环                | 部分完成                         | 任务事实、关系/标签、版本/ETag、稳定分页、批量/排序、Assignment、六状态/时间线、D2 manual Submission/Artifact/受控文件、schema v30 父任务系统待验收及 Focus 自动工时已交付；T-07A–D 已交付任务页计划组拖拽、日期/客户筛选和版本化保存视图；新建/编辑/筛选/批量移动与 Inbox 拆分已统一使用服务端分页搜索 ProjectSelect；六状态看板已接读取闭环和受控跨列生命周期交互                                                                                                                                                                             |
| T-08 项目管理                        | 部分完成                         | CRUD、稳定分页/搜索/筛选、同事务 Count/Scan、创建幂等、乐观锁、受控状态、归档恢复、确认硬删除、任务聚合与树/平铺/项目内检索分页、服务端分页搜索客户与项目选择器、笔记、项目附件、Task Artifact 聚合及 nullable follow-up/实时 required 进度、Inbox 深链、活动时间线、项目级 Focus 分析/终态历史、显式 follow-up、Task 阻塞/临期与 Project 完成节点→Inbox；财务与真实里程碑增强待实现                                                                                                                                                            |
| T-09 客户管理                        | 部分完成                         | 基础资料 CRUD、列表/详情、创建幂等、乐观锁、删除约束、项目数聚合、Project 客户关联、本地活动、受控附件、person 显式关联及三处共享分页搜索选择器已交付；外部来源投影、回访/财务待实现                                                                                                                                                                                                                                                                                                                                                            |
| T-10 收入、支出与发票                | 页面骨架                         | 收入/发票路由和空状态已存在；支出、业务 API 与统计未开始，整体属于 v0.4                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| T-11 收件箱与工作编排中心            | 部分完成                         | T-11A1/A2/A3/B/C/F 已交付；拆分可继承/清除/改选可信来源 Project，写入独立完成条件并明确 person 本地责任，关系行可打开共享 Task；成功 mutation 会失效来源 Project，split 另失效 Task/Today/Project。T-11E 已交付 follow-up Artifact、Task 阻塞、提前 24 小时 Task 临期、Project 完成周期、备份四类操作性失败、数据库启动/迁移、Sidecar 启动、运行期数据库操作失败和可配置低空间投影；卷级趋势与 Agent 待实现                                                                                                                                     |
| T-12 专注设置与全局计时              | Core+D1+D2a+Project+D2b 分析完成 | A+B+C：schema v11 Session/interval、任务绑定、绝对时间、心跳/恢复、状态命令、幂等/并发、精确工时与 Today 汇总；D1/D2b：终态历史分页、7/30 天/本月/最多 93 天自定义本地日趋势、Streak、项目/当前标签分布、DST 安全小时分布/最佳时段与周几×小时二维热力图；D2a/Project：Task 详情按需记录与 Project 详情 7/30 天/月度分析及终态历史已交付；通知/托盘/DND 延后                                                                                                                                                                                     |
| T-13 命令面板与基础反馈              | 核心搜索、诊断与恢复反馈完成     | WebView 快捷键、已交付页面命令、Task/Project/Client/活动 Inbox 统一本地搜索、稳定详情路由、设置模块直达、combobox/listbox、焦点圈闭/恢复、IME 保护、加载/错误/重试/空状态、非敏感最近使用/404 资源清理、脱敏运行诊断/诊断包 v1、Sidecar/Tauri 壳脱敏轮转日志、打开日志目录、WebView→Sidecar request ID、全局渲染错误恢复与全局启动故障恢复页 v1；OS 全局快捷键和数据库打开前恢复进度待后续                                                                                                                                                      |
| T-14 设置持久化                      | 核心闭环完成                     | schema v16 四模块严格设置 + schema v27 受控头像；原子 PATCH/multipart、乐观锁、Query committed、即时预览/取消、旧 Data URL 迁移和备份恢复已交付；空工作区同 schema 业务 JSON/含文件 ZIP 导入已交付，非空目标/跨 schema 冲突合并待开发                                                                                                                                                                                                                                                                                                           |
| T-20 测试、构建与桌面验收            | 部分完成                         | Web/Go 自动测试与构建已接入；桌面完整编译和安装包验收受环境限制                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| T-15 AI 助手                         | 未开始                           | 已登记本地模型 Adapter、只读上下文、安全存储和质量闸门，尚无代码                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| T-16 本地知识库                      | 未开始                           | 已登记导入、FTS 检索、引用与删除要求，尚无数据结构或页面                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| T-17 客户回访                        | 未开始                           | 已登记回访计划、到期提醒、结果记录和后续开发顺序，属于 v0.4                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| T-18 本地 Actor 与任务分派           | 部分完成                         | T-18A/B/C 和 T-18D D1/D2 已交付，包括 Actor、Assignment、生命周期、Submission/Artifact 与 manual 验收；agent Adapter/Run 属于 T-19                                                                                                                                                                                                                                                                                                                                                                                                              |
| T-19 本地 Agent 执行                 | 未开始                           | Adapter、Run、产出、取消/重试、验收/返工和崩溃恢复规划，属于 v0.2                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |

#### 10.4.1 T-01 工程目录与统一脚本

- **需求映射**：4.4、8.1、9.1。
- **用户/开发者流程**：安装依赖后执行 `pnpm dev:web` 查看浏览器版，或执行 `pnpm dev` 联调桌面版。
- **实现方法**：根 `package.json` 统一开发、测试和构建命令；`scripts/dev.mjs` 按 Sidecar → Vite → Tauri 顺序启动并做就绪检查；`scripts/build-sidecar.mjs` 读取 `rustc --print host-tuple`，使用 `CGO_ENABLED=0` 构建 `opc-sidecar-<target-triple>`。
- **关键路径**：`package.json`、`pnpm-workspace.yaml`、`scripts/dev.mjs`、`scripts/build-sidecar.mjs`。
- **验证/剩余**：历史记录中浏览器开发链路运行通过，本次文档更新未复验；CI、多平台矩阵和发布流水线未建立。

#### 10.4.2 T-02 Tauri 桌面壳与 Sidecar 生命周期

- **需求映射**：4.1、4.2、7.2、8.2。
- **用户流程**：桌面应用启动后自动准备数据目录并连接本地服务；内置 Sidecar 意外退出时进入有界恢复并暂停业务；第二次启动复用单实例并聚焦主窗口；退出时先优雅关闭 Sidecar。
- **实现方法**：生产配置通过 `externalBin` 内置 Go Sidecar，开发配置通过 `OPC_SIDECAR_URL` 连接外部 Sidecar。Rust 为每个 generation 注入新令牌、通过端口 `0` 重新请求动态分配，并注入 `OPC_EXIT_ON_STDIN_CLOSE=true`；已启动代只有真实 `Terminated` 才按 500 ms、2 s 最多重拉两次，当前代连续 Ready 30 秒重置预算，外部模式、显式 shutdown 和无 `Terminated` 的事件流关闭不重拉。Go 在 pending restore、迁移和 DB open 前取得数据库父目录固定运行锁，再取得 Artifact root 锁并协调 store。`sidecar_status` 暴露四态与 generation；React 在非 ready 或 generation 改变时清连接与 TanStack Query。退出写入 `shutdown\n`，父管道 EOF 也触发 Go 优雅退出，并发 shutdown 共享一次 stop。应用重启仅接受受管 child code 0/no signal；无 child 启动失败允许继续，延迟干净退出可再试。
- **关键路径**：`apps/desktop/src-tauri/src/lib.rs`、`apps/desktop/src-tauri/src/sidecar.rs`、`tauri.conf.json`、`tauri.dev.conf.json`。
- **验证/剩余**：Rust 已新增覆盖 generation、两次预算/退避、30 秒稳定重置、真实 Terminated 门禁、shutdown 取消重试、旧代退出隔离、安全应用重启与并发 shutdown 的单元测试源码，并完成 P0/P1=0 的静态复核；受本机工具链限制，这些 Rust 测试尚未执行。Go 覆盖运行锁、父管道 EOF 开关和既有优雅退出；Web 覆盖四态/generation、非 ready 连接与 Query 清理。T-02 仍部分完成：真实 Tauri/Sidecar 父崩溃、OS 进程树、三平台和安装包未验收；没有 Job Object、进程组或孙进程治理，hard-hung orphan 只被锁挡住且不自动回收；数据库打开前恢复进度仍待实现。

#### 10.4.3 T-03 Go 健康检查与 API 基础

- **需求映射**：4.1、4.2、附录 C。
- **用户流程**：前端先确认 `/health`，再调用 `/api/v1`；服务不可用时显示可重试错误，而不是展示伪造业务数据。
- **实现方法**：Gin 路由依次接入 request ID、Origin、Bearer 鉴权、访问日志和 panic recovery 中间件。`/health` 返回应用、API 和 schema 版本；任务和今日统计路由挂载在 `/api/v1`。Sidecar 只绑定 IPv4 loopback，支持动态端口，并在 stdout 输出机器可读 ready 事件。
- **关键路径**：`services/sidecar/cmd/server/main.go`、`internal/api/router.go`、`middleware.go`、`errors.go`、`json.go`。
- **验证/剩余**：鉴权、Origin、健康检查、错误、主任务链路和 Task 文件 Artifact 已有 Go 集成测试；WebSocket、OpenAPI、通用附件/PDF 和业务调度服务未实现。

#### 10.4.4 T-04 SQLite 初始化、迁移与开发数据隔离

- **需求映射**：6、7、9.1。
- **用户流程**：首次启动自动创建数据库并迁移；后续启动只执行尚未应用的版本；开发和正式数据互不影响。
- **实现方法**：GORM 使用纯 Go SQLite 驱动和单物理连接，启用外键、WAL 与 5 秒 busy timeout。schema v1–v22 依次建立核心业务、Task/Actor/D2、Client、Focus、Inbox/Reminder、设置/保存视图、客户/项目扩展；schema v23–v26 新增来源 guards；schema v27 新增受控 Workspace Avatar；schema v28 新增 Project 完成节点来源和删除协调；schema v29 在破坏性迁移闸门和回滚包后重建 app_settings 约束以增加 storage key；schema v30 非破坏性新增 Submission origin 和 child_rollup guards，既有提交保持 manual，不扫描/回填历史父任务；schema v31 非破坏性增加 Project Workflow Event 客户动态来源唯一约束，不回填历史 Project 事件。迁移不创建 demo 数据。不可变 `database_id`、一次性 `artifact_store_id` 与 marker 双向绑定整个受控根。开发数据库/受控文件位于 `.local/dev-data/`，生产数据位于 Tauri `appDataDir`。
- **关键路径**：`services/sidecar/internal/database/database.go`、`migrate.go`、`migrations/001_initial_schema.sql` 至 `migrations/031_client_project_activity_projection.sql`。
- **验证/剩余**：迁移测试覆盖连续旧版本数据保留、核心关系/trigger、Focus ledger、客户/项目扩展、来源约束，以及 v29→v30 origin 默认值、system/inferred/no-Artifact/不可变 guards、失败回滚和外键检查；另覆盖 destructive 迁移门禁与自动回滚包。当前 schema 的空工作区 JSON 与含文件 ZIP 安全导入导出已实现；跨 schema 和非空目标合并仍未实现。

#### 10.4.4A T-04B 手动一致性备份

- **需求映射**：7.1–7.4、9.1、10.3.3。
- **用户流程**：设置 → 数据与备份可创建、校验、演练、恢复或删除真实本地包；“下载 JSON”保存轻量业务快照，“下载含文件 ZIP”保存 manifest、业务 JSON 和全部活动受控文件。选择 JSON 或 ZIP 后先显示 schema、总行数和可应用状态；ZIP 额外显示文件数与字节数。已有业务数据、不兼容版本、受控文件或活动 Focus 会在确认前阻断。可应用时用户再次确认，成功结果显示导入行数、文件数与自动回滚备份短 ID。
- **实现方法**：`--backups` / `OPC_BACKUP_DIR` 声明与数据库同级且不和受控文件 root 重叠的安全 root。所有普通 API、Focus heartbeat 与 Reminder 扫描共享维护读锁，创建备份、含文件导入导出和安排恢复取得写锁；手动 `POST /backups` 再持有备份互斥锁，先处理幂等重放，未命中时按 SQLite 分配/文件上界、active 受控文件、marker/manifest 及 20%（最低 64 MiB）余量估算，只探测 backup root，满足后才用 `VACUUM INTO` 创建 WAL 一致性 SQLite 快照并校验/原子发布完整包。不足/不可确认以 507/503 脱敏拒绝且不投影 generic incident，精确边界允许继续。业务 JSON 使用一个 SQLite 读事务和固定白名单；含文件 ZIP 在 backup root 私有临时文件中预检/生成，manifest 固定记录业务 JSON/活动文件的相对路径、size/SHA-256。ZIP 导入 preview 严格拒绝重复、额外、危险路径和 symlink，复验 format/API/schema、表列行、正文 hash/size 及数据库文件元数据；apply 强制确认并再次预检，先通过内部链创建已校验回滚备份，再把文件按既有 store 规则 staging 和无覆盖发布，数据库事务写入业务行、重建 `task_focus_totals`，并在提交前执行 `foreign_key_check/quick_check` 与磁盘正文复验；数据库失败补偿本次发布文件。导入/恢复/迁移内部回滚包不经过手动 HTTP 门禁。
- **关键路径**：`services/sidecar/internal/api/backups.go`、`backup_restore.go`、`business_export.go`、`business_package_export.go`、`business_import.go`、`business_package_import.go`、相关 Go 测试、`apps/web/src/components/BackupSettings.tsx`、`api/client.ts`、`api/hooks.ts`。
- **验证/剩余**：Go 集成测试覆盖备份恢复全链、手动容量需求的小载荷/溢出、空间差 1 字节拒绝、容量探测异常、只探测 backup root、响应脱敏、无 staging/包/业务/Inbox 副作用、精确边界放行与幂等重放绕过探测，以及启动恢复结果/残留/失败/无效记录诊断与脱敏、业务导出白名单/隐私、含文件 ZIP 的清单/正文/hash/空包/篡改拒绝/临时清理、JSON/ZIP 导入预检/确认/空目标/自动回滚/原子应用/补偿；前端组件测试覆盖 507/503 操作提示、note 草稿保留、不伪造成功/不自动重试和未知错误 request ID 回归。当前全量前端测试已通过。跨 schema 迁移、非空目标冲突合并、大数据量进度/取消、数据库打开前备份选择/实时恢复进度和真实磁盘故障仍未实现。

#### 10.4.5 T-05 前端 AppShell、原型复刻与基础页面

- **需求映射**：5.1–5.9、附录 A/B。
- **用户流程**：用户通过左侧导航访问今日、任务、项目、客户、收入、发票、收件箱和专注；右侧持续显示专注和业务概览。
- **实现方法**：React Router 负责页面路由；`AppShell` 固定 220 px 左栏、弹性主内容和 280 px 右栏，窄屏时隐藏右栏并收缩导航。历史原型风格已经沉淀到当前 React 组件和 `styles.css`，后续以实际渲染为视觉基线；公共结构复用 `PageHeader`、`Modal`、`TaskList` 和反馈组件。
- **关键路径**：`apps/web/src/App.tsx`、`components/AppShell.tsx`、`Sidebar.tsx`、`RightOverview.tsx`、`styles.css`。
- **验证/剩余**：全部规划路由可访问；项目、客户、收件箱与本地 Reminder 已从骨架升级为真实纵切，收入和发票仍未完成业务数据链路与交互。

#### 10.4.6 T-06 今日工作台

- **需求映射**：5.1。
- **用户流程**：进入首页后读取本地任务和今日统计，可新建任务、完成/恢复任务、打开详情编辑或删除，并从右侧概览开始/暂停专注或打开专注设置。
- **实现方法**：`TodayPage` 用用户本地日期和浏览器 IANA 时区请求 `/api/v1/stats/today`；任务 Query 用 `status=active`、计划日期范围及 `planned_state=unscheduled` 并行分页拉全四组。逾期/临期统计卡另用 `status=active&due_state=overdue|due_soon&sort=due_date` 读取完整风险分页；`due_state` 在 Sidecar 单次捕获 `Options.Now().UTC()`，并把已规范化的 UTC 截止时间与窗口边界转换为固定 9 位小数的 UTC 纳秒键，供 Today stats、筛选和 `due_date` 排序共同使用，保持亚毫秒精度及整秒/小数秒的真实时间顺序。两路时间派生 Query 每 60 秒低频刷新；风险视图以独立加载/空/错误/重试/分页状态替换四组，并保留 Task 行既有安全操作但关闭拖拽排序；排序写入期间锁定风险切换和行写操作，状态/错误在视图切换后仍可见。`TaskList` 可消费页面级共享拖拽源，使普通四个列表互为目标；落到具体任务使用该任务真实日期，所选日期和未排期另提供可接收空组的明确落点。`useMoveTaskAcrossPlans` 先拉全源/目标精确计划组并校验活动任务版本；同日期直接提交完整 reorder，跨日期先用 batch `set_planned_date` 确认改期，再并行提交源/目标完整 reorder 并分别报告结果。`TaskPlanModal` 继续提供任意日期键盘替代；网络/超时后回读 Task，仅当目标日期一致且版本前进才接受改期成功。行内开始/完成复用版本化 `POST /tasks/{id}/commands` 与现有幂等键，manual review 不显示完成；开始专注仅在活动 Session 查询成功且为空、本地循环为 idle 时调用幂等 create Session，成功后启动共享循环。Focus 统计只读取 completed Session 的已关闭正 interval，并按本地日 UTC 边界 overlap 求和。
- **关键路径**：`services/sidecar/internal/api/tasks.go`、`stats.go`、`task_due_filters_test.go`、`apps/web/src/pages/TodayPage.tsx`、`TodayPage.test.tsx`、`apps/web/src/components/TaskPlanModal.tsx`、`TaskList.tsx`、`RightOverview.tsx`、`apps/web/src/api/client.ts`、`api/hooks.ts`。
- **验证**：可变服务端时钟覆盖同一任务从临期转为逾期；固定带小数时钟覆盖同毫秒内已过期、无小数的刚过期值、`now`、`now+24h`、超窗、无截止、done/cancelled、混合小数精度真实排序、组合筛选、分页重放与非法冲突，并断言两张统计卡分别等于列表 `meta.total`。前端覆盖独立序列化、卡片互斥/再次清除、加载/空/错误重试、完整分页、页码收敛、详情入口、无排序控件、排序保存期间的风险切换/行写互斥和仅动态风险 Query 的低频轮询。
- **当前限制**：计划日期“逾期/本周”仍是多日期聚合，只有落到具体任务时才有不含猜测的目标日期；空组级落点仅对精确所选日期和未排期开放。截止日期风险视图是查询时动态读模型，不写第二份状态、不纳入静态保存视图，也不等同于可归档的 `task_due` Inbox 历史事项。跨日期的日期事实与两个顺序事务不是一个数据库事务，改期确认后排序失败不会反向覆盖日期，而是刷新并明确提示部分成功。需要原因的阻塞/取消以及 manual review 验收仍通过详情处理；快捷开始依赖活动负责人。真实收入和客户动态未实现。

#### 10.4.7 T-07 任务管理纵向闭环

- **需求映射**：5.2、附录 C。
- **用户流程**：用户按服务端条件搜索/筛选/分页任务，可按任务所属项目的当前客户筛选；客户与项目及其他条件取 AND。新建、详情编辑、列表项目筛选、批量移动项目及 Inbox 批量拆分均通过共享 `ProjectSelect` 逐页搜索项目；搜索、翻页、归档当前选择或读取失败不会隐式清空，只有显式清除才提交空项目。计划日期可选精确值或起止范围，截止日期可选起止范围，倒置区间就地提示且不发请求；常用完整条件可命名保存，之后选择即应用、以当前条件更新或确认删除。无筛选时展开根任务树，有筛选时查看父任务面包屑。新建可选择 none/manual；详情仅在 todo 且无任何 Submission 历史时允许改策略。manual Task 具备 active assignee 与 owner reviewer 后可填写摘要、text/link/structured/file 混合产出，提交待审后由 owner 接受或填写原因返工；历史批次分页查看，正文按需打开，文件安全下载，非 pending Artifact 可确认软删。父任务列表/详情以直属非取消子任务为进度分母并单列取消数：至少 1 个且全部 done、manual 和责任门禁齐全时，界面显示“子任务汇总”系统批次并等待 owner 验收；policy none 明确不会自动完成，manual 缺门禁则提示补齐负责人和所有者审核人。系统发起、失效撤回和 accepted 后子任务失效重开都有独立时间线文案。
- **实现方法**：schema v6 增加 Task facts/version，v8 扩展六状态，v9 增加 workspace identity、Submission/Artifact/deletion tombstone/current pointer/Event 关联，v17 增加独立 `task_saved_views`，v30 非破坏性增加 `task_submissions.origin=manual/child_rollup` 与 system/non-inferred/no-Artifact guards。父任务 readiness 只查询 `parent_task_id` 直属行：`total-cancelled>0` 且 `completed=total-cancelled`；再要求父任务 todo/in_progress + manual、active owner/person assignee 和 active builtin owner reviewer，并拒绝覆盖任何 manual 历史、现存 pending 或 changes_requested child_rollup。满足后 system 创建固定摘要、零 Artifact 的 pending child_rollup 并只转 waiting_review。子任务 readiness 或 pending 阶段父任务门禁失效会 system withdrawn；普通父任务回 in_progress，blocked 父任务仅把 `blocked_from_status` 改为 in_progress。accepted 后只有 readiness 失效会 system reopen 为 todo，保留历史且不恢复旧 Assignment。创建/改绑/解除父级/删除、单条或批量 complete/cancel/reopen、review accept、review policy 和 Assignment 变化在原 SQLite 事务中协调自身/父级及完整祖先链，以 visited 防循环；批量去重父节点并返回最终 version。迁移/启动不全库回填。通用 Task、生命周期、Assignment 和 D2 命令共享 Task `ETag`/`If-Match`；D2 可选稳定幂等快照。任务列表把 `client_id / planned_from / planned_to / due_from / due_to` 纳入版本化查询契约；`client_id` 严格校验 UUID 后使用 Project 关系子查询过滤，不向 Task 复制客户事实，也不因 JOIN 产生重复页。日期由服务端校验 ISO 格式和起止顺序，截止日期按已存时间戳的日期部分比较；Web 的客户条件复用共享 `ClientSelect`，以每页 20 条、250 ms 防抖、服务端搜索和稳定分页选择 ID，跨页/失败仍保留保存视图中的当前客户。共享 `ProjectSelect` 通过 `useProjectOptionsQuery(q,page,enabled,includeArchived)` 把 trim 后搜索词、页码和归档模式隔离进 Query key，将 TanStack Query 的 AbortSignal 传到 fetch；结果按 Project ID 去重，详情 Query、当前页或 `selectedName` 快照保留当前选择，默认候选排除 archived，但当前归档项目仍可显示。五个任务入口均已移除 `getAllProjects`，只有选择器打开时才读取对应 20 条；精确计划日期与计划范围继续互斥，非法范围停用 Query。`due_state=overdue|due_soon` 是动态查询条件，当前只由 Today 风险入口消费，不写入保存视图。保存视图最多 20 个、名称唯一。Sidecar 用数据库绑定 marker、进程级 root 锁与 `.staging/objects/.trash/.quarantine` 管理文件；前端严格解析 origin 和 `subtask_cancelled`，兼容 v29 durable snapshot 缺 origin→manual/缺取消数→0，并失效所有 Task 范围 Submission/Artifact/Assignment/Event 缓存以刷新可能旁路变化的祖先。
- **Actor/状态语义**：manual Artifact producer 从 active assignee 派生，Submission submitter 与 Artifact recorder 固定 owner；child_rollup submitter 固定内置 system、非 inferred、无 Artifact。owner 对两种 pending 来源执行 accept/request_changes。submit/rollup → waiting_review/pending；accept → done/accepted 并结束 Assignment；request_changes → in_progress/changes_requested 且系统不覆盖；waiting-review cancel → withdrawn；人工 reopen 清 current pointer但保留历史。自动撤回由 system 记录；accepted 后子任务失效的 system reopen 同样保留 accepted Submission/Event。
- **关键路径**：`services/sidecar/internal/database/migrations/009_task_submissions_artifacts.sql`、`migrations/017_task_saved_views.sql`、`migrations/030_task_parent_progress.sql`、`internal/api/tasks.go`、`internal/api/task_saved_views.go`、`internal/api/task_outputs.go`、`internal/api/task_parent_progress.go`、`internal/api/artifact_store.go`、`apps/web/src/api/client.ts`、`api/hooks.ts`、`components/ClientSelect.tsx`、`components/ProjectSelect.tsx`、`components/NewTaskModal.tsx`、`components/TaskList.tsx`、`components/TaskDetailModal.tsx`、`components/InboxTaskOrchestrationModal.tsx`、`components/TaskOutputsSection.tsx`、`components/TaskEventsSection.tsx`、`pages/TasksPage.tsx`。
- **验证/剩余**：Go 定向测试覆盖 D1/D2、直属 done/cancelled/全取消口径、manual/assignee/builtin owner reviewer 门禁、系统无 Artifact child_rollup、幂等重放、owner accept、失效撤回、blocked 来源更新、changes_requested/manual 不覆盖、accepted 祖先重开、创建/改绑/删除和批量最终版本；schema v29→v30 测试覆盖既有 Submission 默认 manual、origin/system/inferred/Artifact/不可变 guards、非破坏性门禁、回滚与外键完整性。前端定向测试覆盖 origin/取消计数严格解析及 v29 快照兼容、“子任务汇总”/门禁/时间线/进度显示、ClientSelect，以及 ProjectSelect 的首屏/搜索/分页/跨页保留、状态/客户上下文、归档当前项、加载/错误/清除/键盘语义和五处接线；typecheck 通过。父任务自动协调没有迁移/启动回填，accepted 后重开不恢复 Assignment；Windows 当前缺 MSVC `link.exe`，不能把未执行的桌面链接检查写成通过。两类选择器与日期控件的真实浏览器、窄屏和大数据量专项仍未完成。

#### 10.4.8 T-08 项目管理

- **需求映射**：5.3。
- **当前状态**：部分完成；项目资料、基础生命周期、确定性分页与同事务 Count/Scan、任务聚合与树/平铺视图、项目内任务搜索及状态/优先级/类型/标签/排期筛选和服务端分页、Client 客户关联及共享分页搜索选择器、任务入口共用的 ProjectSelect、可编辑人工笔记、受控项目附件、Task Artifact 聚合及 nullable follow-up/实时 required 进度、Inbox 深链、追加式项目活动时间线、项目级 Focus 报告/终态 Session 历史、显式 follow-up、Task 阻塞/临期、Project 完成节点→Inbox，以及 Project 完成/重开→Client Activity 已接通；Assignment/Submission 写入继续复用共享 Task/Inbox 详情，财务与其他真实里程碑仍未完成。
- **用户流程**：用户可分页查看和搜索/按状态或客户筛选项目，新建或编辑非归档项目资料并选择、改绑或解除 Client。项目表单与列表筛选共用服务端 Client 搜索选择器：输入防抖后每页读取 20 条，可稳定前后翻页，搜索、翻页、inactive 状态或临时读取失败不会清空当前选择，只有显式点击“清除客户”才解除关联。任务新建/编辑、Tasks 项目筛选、批量移动项目及 Inbox 拆分通过同类 `ProjectSelect` 选择当前项目，默认不列归档候选但保留既有归档选择。进入详情默认按父子树查看项目任务；紧随任务区的产出区显示待拆分/跟进中/已解决/已忽略、实时 required 进度与阻塞/待验收/取消提示，可打开共享 Task 或深链 Inbox。Inbox 拆分继承可信来源 Project 但可逐项清除/改选，owner/person 分派和 manual owner 验收都继续在既有详情完成。随后可查看 Focus、项目状态、笔记、附件与活动时间线，执行受控状态命令、归档/恢复，并在已归档状态二次确认永久删除。已关联客户的项目完成或重开后，用户在该客户详情的活动时间线看到只读项目生命周期事实；无客户时不出现，之后改绑也不改变历史归属。详情 Focus 区可切换 7 天/30 天/本月并浏览终态历史；任一子区失败不阻断主详情。
- **实现方法**：schema v3 增加 `projects.version`、`archived_from_status` 和查询索引，schema v4 为幂等键增加请求摘要与响应快照，schema v5 用 trigger 把任务关联/状态/`actual_minutes`、发票关联/增删、客户名称/删除纳入项目版本，schema v7–v9 提供通用不可变 `workflow_events`，schema v10 让 Project 客户关联变化反向递增 Client 聚合版本；schema v31 为 `project_workflow_event` Client Activity 来源增加部分唯一索引。Project `complete / reopen` 先取得同事务 Workflow Event ID，再按事件发生时的 `client_id` 创建 system Actor、空正文、同时间戳的只读动态；无客户跳过，任一下游写失败整体回滚。Project Focus 读取复用 schema v11 Session/interval、现有 Task/Project 外键和 API v1，没有为该读模型增加 migration；当前整体 schema 为 v31。Go Project API 提供既有 CRUD/状态/审计契约；列表先校验筛选，随后在带请求上下文的同一 SQLite 只读事务内按同一筛选执行 Count，再校验并应用排序后读取当前页。默认及 `name/status/start_date/due_date/amount_minor/created_at/updated_at` 全部白名单排序追加 `projects.id ASC`，从而给 `page_size=20` 的选择器确定性页边界。共享 `ProjectSelect` 复用 `GET /projects` 和 `GET /projects/:id`，把 trim 后的 `q`、页码与 `includeArchived` 纳入 Query key并传递 AbortSignal，按 ID 去重，以详情/当前页/名称快照保留选择；默认候选隐藏 archived，当前归档项仍保留，显式清除才输出空值。旧的 `getAllProjects` 已移除，不增加 API 或 migration。`GET /focus-sessions` 和 `GET /stats/focus` 通过可选 `project_id` 在同一只读事务检查 Project 并按 Task 查询时当前项目归属 JOIN。`project_id` 只接受 canonical UUID，非法/非 canonical 返回 400、Project 不存在返回 404、归档可读；Task 改绑会重分类，无 Task/Task 已删除/当前无 Project 不进入过滤结果。Client 选择器复用既有 `GET /clients` 的 `q/page/page_size/status/sort` 契约，以关键词和页码隔离 Query key、以 AbortSignal 取消旧请求，不增加 API 或 migration；当前选择通过详情 Query、当前页或项目名称快照补齐。历史按 `ended_at DESC, id ASC` 返回三种终态；报告只聚合 completed Session 的闭合正时长 interval，保持 IANA/DST/跨午夜/1–93 当地日、Streak 和零事实序列。前端 Project transition 成功或版本冲突会同时失效 Project 与 Client 查询前缀，刷新详情、版本和活动时间线；其余 Focus Query key 仍按项目/日期/页码隔离。
- **Artifact follow-up 读模型**：`GET /projects/:id/artifacts` 在同一只读事务读取 Artifact/Task/Submission、稳定 task_artifact 来源 Inbox 的 ID/version/status/policy/`source_deleted_at` 及实时 required progress。响应继续返回 Project 聚合数值 `ETag`，并与 `meta.project_version` 表示同一个 Project 并发版本；实时 follow-up 不传播进 Project version。`followup.inbox_item_version` 才用于未来从该读模型发起 Inbox 写入，当前 Project UI 只深链 Inbox。前端把产出区上移到任务区之后，状态和进度深链 Inbox；任何成功且可能改变 follow-up 的 Inbox mutation 都先 cancel 来源 Project 在途读取再 invalidate，Artifact 请求消费 `AbortSignal`；split 另失效 Task、Today、Project。该纵切不改表、不迁移、不提升 API 版本。
- **关键路径**：`services/sidecar/internal/database/migrations/003_project_lifecycle.sql`、`migrations/004_idempotency_snapshots.sql`、`migrations/005_project_aggregate_versions.sql`、`migrations/011_focus_sessions.sql`、`migrations/028_project_completion_inbox_projection.sql`、`migrations/031_client_project_activity_projection.sql`、`internal/models/project.go`、`internal/api/projects.go`、`internal/api/project_artifacts.go`、`internal/api/project_artifacts_test.go`、`internal/api/focus_history.go`、`internal/api/inbox_source_projections.go`、`internal/api/project_client_activity_projection_test.go`、`apps/web/src/api/client.ts`、`api/hooks.ts`、`components/ClientSelect.tsx`、`components/ProjectSelect.tsx`、`components/ProjectArtifactsSection.tsx`、`components/ProjectFormModal.tsx`、`components/NewTaskModal.tsx`、`components/TaskDetailModal.tsx`、`components/InboxTaskOrchestrationModal.tsx`、`pages/ProjectDetailPage.tsx`、`pages/ProjectsPage.tsx`、`pages/TasksPage.tsx`、`api/projectHooks.test.tsx`、`components/ClientActivitiesSection.tsx`。
- **验证/剩余**：代码审查确认 Project Count/Scan 共用请求上下文内的 SQLite 只读事务；Go 自动化覆盖所有排序字段的 `id ASC` 后备键、同名项目稳定分页、搜索/状态/客户/归档过滤、项目事件与 Client 投影、Project Note/Attachment/Artifact/Inbox、Focus→Task→Project 版本传播及项目 Focus 边界。本轮另覆盖 `followup=null`、Inbox version/status/policy/来源删除字段、实时 required 进度，以及 Artifact 列表继续返回 Project 数值 `ETag`/`meta.project_version`，并以 Go 金链验证 `requires_followup → split(owner/person + manual owner reviewer) → complete + submit(waiting_review) → accept → Inbox automatic resolved/100%`。前端自动化覆盖四种 follow-up 状态、阻塞/待验收/取消提示、Inbox 深链、产出区顺序、来源 Project 继承/清除、完成条件、person 文案、共享 Task 详情及缓存失效。真实浏览器/WebView 的完整人工跨页链路、窄屏浮层、键盘焦点、1,000/10,000 条客户/项目/任务和 Inbox 长列表性能仍需专项验收；不能把上述自动化写成所有端到端人工浏览器已验收。Project 不复制 Assignment/Submission 写控件。

#### 10.4.9 T-09 客户管理

- **需求映射**：5.4。
- **当前状态**：部分完成；Client 基础资料 CRUD、表格、详情、Project 客户关联、本地人工活动、受控附件、person 显式关联、Project 生命周期本地系统动态，以及 Project/Task 三处共用的服务端分页搜索选择器已交付；外部来源集成、回访和财务仍未实现。
- **用户流程**：用户可分页搜索/按状态筛选客户，新建或编辑名称、联系人、邮箱、电话、备注与状态；在 Project 新建/编辑、Projects 客户筛选和 Tasks 客户筛选中，通过同一选择器输入名称、每页浏览 20 条并选择 active/lead/inactive 客户，搜索或翻页不会误清空既有选择。详情展示完整关联项目、真实活动时间线、受控附件和本地联系人。人工 note/meeting 可编辑或带原因软删；项目完成/重开事实显示“项目生命周期 / 项目状态变更 / 系统只读”，不显示内部事件 ID，也不能编辑删除。用户可选择已有 active person 或原子新建并关联，解除必须填写原因且历史可查；关联不创建账号、消息或访问权。附件可预览后上传、校验下载、填写原因删除并查看历史；停用后可恢复，inactive 状态可二次确认永久删除。列表、表单、活动、附件和关联均覆盖加载、空、错误、重试；版本冲突刷新最新事实但保留可恢复的本地输入。
- **实现方法**：schema v10 建立 Client 聚合版本和 Project 传播；schema v18 建立活动事实；schema v19 建立受控附件；schema v20 建立 Client–person contact、单 active 约束、不可变解除历史、Actor 停用保护和版本传播；schema v31 只为 Project Workflow Event 来源增加部分唯一索引。Project 命令事务创建 system reference 时由既有 Client Activity trigger 原子递增 Client version/latest 派生结果；`source_id` 保持松耦合业务引用而非外键，公开 Client Activity POST 仍只允许 note/meeting。活动时间线和 `latest_activity_at` 先用 SQLite 把 RFC 3339 offset 归一到 UTC 秒，再从原文补齐固定 9 位小数，避免 `...00Z` 与 `...00.5Z` 的 TEXT 字典序颠倒且不丢纳秒。Client CRUD 使用稳定分页、快照幂等、`ETag`/`If-Match` 和受约束删除。共享 `ClientSelect` 直接消费既有列表契约，固定 `page_size=20&sort=name`，trim 后的搜索词与页码进入 Query key，250 ms 防抖并把 TanStack Query 的 AbortSignal 传到 fetch；结果按 ID 去重，选中详情与名称快照用于跨页/失败保留，不再使用 `getAllClients` 串行拉取全部分页。附件使用 metadata-first multipart、受控文件补偿和完整性验证。联系人 POST 在已有 `actor_id` 与 `create_person` 间二选一；新建 person/Event/关联同事务，关联/解除使用 Client `If-Match` 和稳定幂等快照。Client 永久删除级联关系历史但保留 Actor；业务导出包含关系事实。
- **关键路径**：`services/sidecar/internal/database/migrations/010_client_facts.sql`、`018_client_activities.sql`、`019_client_attachments.sql`、`020_client_actor_links.sql`、`031_client_project_activity_projection.sql`、`internal/models/client.go`、`client_activity.go`、`internal/api/clients.go`、`client_activities.go`、`projects.go`、`project_client_activity_projection_test.go`、`internal/database/client_project_activity_projection_migration_test.go`、`apps/web/src/api/client.ts`、`api/hooks.ts`、`components/ClientSelect.tsx`、`components/ProjectFormModal.tsx`、`pages/ProjectsPage.tsx`、`pages/TasksPage.tsx`、`pages/ClientDetailPage.tsx`、`components/ClientActivitiesSection.tsx`、`api/projectHooks.test.tsx`。
- **验证/剩余**：迁移、CRUD/校验/null、分页/过滤/排序、创建幂等、旧版本写入、删除约束、Project 传播、活动审计、Project 生命周期来源唯一/无回填/FK 边界/事务回滚、附件完整生命周期、已有/新建 person 关联、单 active contact、并发/幂等、带原因解除、历史不可变、Actor 停用保护、Client 删除、备份恢复和业务导出均有自动化覆盖；前端覆盖 Client API/Query、单页查询与 AbortSignal、选择器首屏/搜索/分页/跨页选中、inactive、加载/等待/空/错误重试/更多结果、键盘语义，以及项目表单、Projects/Tasks 筛选接线。真实浏览器键盘/焦点/窄屏浮层和 1,000/10,000 条客户性能仍需专项验收。客户回访/外部沟通保持后续版本；累计收入在 v0.4 只从 `confirmed` Financial Entry 聚合。

#### 10.4.10 T-10 收入、支出与发票

- **需求映射**：5.5。
- **版本归属**：v0.4 后续业务版，不属于 v0.1。收入和发票目前只有页面骨架；`invoices` 表已存在，支出表、业务 API、统计和 PDF 均未实现。
- **下一纵切流程**：先新增 `financial_entries` 递增迁移与收入/支出 CRUD，再实现按月份、分类、客户和项目的净现金流聚合；之后接入发票 CRUD、状态流转和 `/stats/income`。所有金额只以 `amount_minor` 整数计算，并用独立 `type` 字段区分收入与支出。
- **实现约束**：前端负责货币格式化，后端负责统计口径；PDF 作为独立服务写入 `appDataDir/invoices/`，生成失败不得改变发票状态。右侧概览和 KPI 只能使用真实聚合结果。
- **关键路径**：`apps/web/src/pages/IncomePage.tsx`、`apps/web/src/pages/InvoicesPage.tsx`、`services/sidecar/internal/database/migrations/001_initial_schema.sql`。
- **验收要求**：收入、支出、净现金流、删除确认、筛选、CSV 导出、发票状态和 PDF 分别通过测试后再标记完成。

#### 10.4.11 T-11 收件箱与工作编排中心

- **需求映射**：5.6。
- **当前状态**：部分完成。schema v12–v15 已交付手工受理分诊、已有 Task 关系、一次性 Reminder 和 T-11C 拆分编排；T-11F 已交付实时运营计数、Today/Sidebar 展示和风险深链筛选；schema v23–v25 已交付显式 follow-up Task Artifact、Task 阻塞与提前 24 小时 Task 临期来源投影、稳定事件键、来源上下文与删除协调。本轮补齐来源 Project 默认带入/逐项清除或改选、独立完成条件输入、person 本地责任说明、活动/可用历史关系打开共享 Task，以及来源 Project/Task/Today 缓存失效。系统维护已交付备份四类操作失败、数据库启动/迁移、Sidecar 启动、运行期数据库操作失败、可配置低空间投影、物理卷同卷去重和无路径手动容量检查。卷级趋势、重复/原生通知和 Agent Run 仍未实现；v0.1 不启用 AI/LLM/Agent。
- **对象边界**：Inbox Item 管来源、分诊、已读、稍后和解决策略；Task 是唯一可执行工单；Assignment 管责任历史；Agent Run 管单次本地执行；Task Artifact 管产出；Workflow Event 管审计。Inbox–Task required 是显式关系事实，Task 父子层级和 schema v30 child_rollup 不创建、继承或改写它。
- **当前并发/批量契约**：创建、单条命令、read-all、关系命令、split 与 force-resolve 支持幂等快照；Inbox PATCH 和各写命令要求 `If-Match`，命令在版本检查前重放。单条关系命令只递增 Inbox version；split 在一个事务内创建 Task/Assignment/关系并递增 Inbox version。关系 GET 实时 JOIN Task，不新增 Task.version→Inbox.version trigger；Task 生命周期、产出验收和关系写入显式调用统一 reconciliation。父任务自动待验收只在父 Task 已经存在 active required 关系时，通过其真实状态让既有 Inbox reconciliation 更新；它不隐式新增关系。前端对每个成功且可能改变 follow-up 的 Inbox mutation 失效可信来源 Project，split 另失效 Task、Today、Project；这只是读缓存刷新，不把 Task/Inbox version 传播进 Project version。read-all 用 `through_created_at` 同时限制 `created_at` 与 `updated_at`，截止后变化的条目保守跳过。
- **分阶段纵切**：
  1. **T-11A1 手工 Inbox Item（已完成）**：schema v12 表/约束/索引、v11→v12 数据保留和无 demo seed。
  2. **T-11B 人工受理与分诊（已完成）**：真实列表/详情/编辑、单条/截止式全部已读、稍后/恢复、解决/忽略/重开、事件和前端反馈状态。
  3. **T-11A2 Task 关系（已完成）**：schema v13 `inbox_item_tasks`、活动/历史查询、实时进度、已有 Task 关联、required 修改、带原因软解除、状态联动、关系事件，以及活动关系 Task 硬删除互锁与删除后快照；不实现多态来源删除协调。
  4. **T-11A3 Reminder（已完成）**：schema v14 `reminders`、创建/分页查询/详情、scheduled 编辑与改期、带原因取消、ETag/幂等、启动补偿、15 秒扫描和稳定事件键到期 Inbox 投影。
  5. **T-11C 拆分与分派（已完成）**：schema v15、原子父子 Task/显式 required 关系/Assignment/reviewer、统一 reconciliation、自动结清/重开与强制例外；UI 已支持可信来源 Project 继承并逐项清除/改选、真实完成条件、owner/person 责任提示，关系行可打开共享 Task。单条已有 Task 关系仍由 A2 负责。schema v30 的父任务推进不改变拆分时显式填写的 required，也不把子任务自动加入关系集合。
  6. **T-11F 运营计数（已完成）**：实时派生 pending/unread/tracking/blocked/waiting_review，接入 Sidebar、Today 与 Inbox 风险深链筛选。
  7. **T-11E 事件源（部分完成）**：schema v23–v26 已接显式 follow-up Task Artifact、Task 阻塞、提前 24 小时 Task 临期和备份创建/校验失败；随后复用系统维护契约交付备份恢复演练/恢复安排、数据库启动/迁移、Sidecar 启动、运行期数据库操作失败和可配置主动低空间监测。启动前或数据库不可写故障以独立安全 journal 延迟到下一次健康启动补偿，稳定 ID 防模糊清理重复。临期扫描在 ready 前补偿并每 15 秒按截止时间/ID 处理最多 100 条；磁盘在 ready 前及每 5 分钟读取 1–100 GiB 已保存阈值，持续低空间只提示一次。发票、客户回访和独立项目里程碑随对应业务模块启用。
  8. **T-11D 本地 Agent（v0.2）**：接入已注册本地 Adapter、Agent Run、产出、取消/重试、人工验收、返工和中断恢复。
- **关键路径**：`services/sidecar/internal/database/migrations/012_inbox_items.sql` 至 `015_inbox_task_orchestration.sql`、`023_task_artifact_inbox_projection.sql` 至 `026_system_maintenance_inbox_projection.sql`、`028_project_completion_inbox_projection.sql`、`services/sidecar/internal/api/inbox_items.go`、`inbox_item_tasks.go`、`inbox_orchestration.go`、`inbox_source_projections.go`、`project_artifacts.go`、`task_due_projections.go`、`system_maintenance_inbox.go`、`reminders.go`、`apps/web/src/api/hooks.ts`、`pages/InboxPage.tsx`、`InboxItemDetailModal.tsx`、`InboxItemTasksSection.tsx`、`InboxSourceContext.tsx`、`InboxTaskOrchestrationModal.tsx` 和 `ReminderManagerModal.tsx`。
- **验收要求**：当前纵切已覆盖纯离线手工受理、迁移保留、幂等/并发、事务回滚、全局未读、Task 关系/删除互锁、Reminder、follow-up Artifact、Task 阻塞、Task 临期及安全系统维护来源；并覆盖批量拆分全回滚、来源 Project 继承/清除、完成条件、owner/person 分派、manual owner reviewer、关系行打开共享 Task、来源 Project 与 Task/Today 缓存失效、自动结清/重开与强制例外审计。Go 金链覆盖一项直接 complete、一项 submit→waiting_review→accept，最终 Inbox automatic resolved/100%。schema v30 回归另证明父子层级/child_rollup 不新增 Inbox 关系或修改 required。真实浏览器/WebView、窄屏、焦点返回和大数据量仍待专项验收；Agent Run 是 v0.2 独立验收项，不能从本轮人工链路推断已实现。

#### 10.4.12 T-12 专注设置与全局计时

- **需求映射**：5.7。
- **完成范围**：Core A 事实迁移、Core B API/状态机/事务、Core C 前端接入与恢复、D1 终态历史/七日报告、D2a Task 详情记录、Project 详情项目级报告/终态历史，以及 D2b 7/30 天、本月、自定义日期范围回顾、项目/当前标签时间分布、DST 安全小时分布/最佳时段与周几×小时二维热力图已完成；原生系统集成延后。Today 与周期 completed-only 统计属于已交付闭环。
- **用户流程**：用户在 Focus 页选择非 cancelled Task 或二次确认无绑定启动；可暂停、继续、停止完成或取消。刷新会重新查询服务端活动 Session；启动发现 recovery_pending 时全局不可关闭弹窗要求选择计入间隔继续、排除间隔继续或中断。Focus 页齿轮和命令面板“专注设置”均直达 focus 设置。用户在 Project 详情可切换 7 天/30 天/本月报告并分页查看终态历史；报告与历史独立加载/空/错误/重试，归档 Project 可读。
- **实现方法**：schema v11 以 `focus_sessions`、`focus_session_intervals`、`task_focus_totals` 保存事实。API 快照统一返回 `session/server_now/elapsed_seconds/remaining_seconds` 并为 Session 返回 ETag；现有 Session 命令强制 `If-Match`，create/stop/cancel 支持 `Idempotency-Key`。Sidecar 启动把 active 转为 recovery_pending，每 15 秒刷新 heartbeat 且不递增 version。stop→completed 在一个事务内结算 interval、累计精确秒数、只把新增完整分钟写入 Task并每次递增 Task version；只有 `actual_minutes` 实际增加时既有 trigger 才递增 Project 聚合版本。cancel/interrupted 不入账。终态历史按 `ended_at DESC, id ASC` 分页；周期统计按显式 IANA 时区将 completed 的闭合正时长 interval overlap 切到 1–93 个本地日，并派生 distinct Session、秒/分钟和 Streak。历史与周期接口可选 canonical UUID `project_id`，不存在 404、归档可读，并按 Task 查询时当前 Project 归属分类；无 Task/Task 已删除/当前无项目不进入项目过滤。前端 TanStack Query 共享活动、历史与报告快照，并以 Project/日期/页码隔离 key；本地 persist store 只保存 work/break/cycle 编排与绝对休息截止时间。该 Focus 扩展没有新增自己的迁移；当前整体基线为 API v1/schema v31。
- **设置隔离**：Query 确认的服务端快照是 committed，`SettingsModal` 表单是 draft，store `preview` 只控制可逆预览；Zustand 不持久化头像 Blob URL，历史 Data URL 只作一次性迁移源。Session 创建和全局 ticker 读取 committed；draft/preview 的修改、保存或取消都不改变活动 Session 的服务端计划时长与进度。
- **关键路径**：`services/sidecar/internal/database/migrations/011_focus_sessions.sql`、`internal/api/focus_sessions.go`、`internal/api/focus_history.go`、`internal/api/stats.go`、`apps/web/src/api/client.ts`、`api/hooks.ts`、`store/settings.ts`、`store/focus.ts`、`components/FocusTicker.tsx`、`components/FocusRecoveryModal.tsx`、`components/TaskFocusHistorySection.tsx`、`components/ProjectFocusSection.tsx`、`pages/FocusPage.tsx`、`pages/ProjectDetailPage.tsx`。
- **验证/当前限制**：自动化测试覆盖迁移、状态机、绝对时间、心跳、恢复、并发创建/停止、幂等重放、事务回滚、跨 Session 余秒、Task/Project 版本、历史筛选/分页、Task/Project 详情按需读取/翻页/错误隔离，以及 IANA/DST/跨午夜 completed-only 日与周期统计、Streak、项目/未归项目分布、当前多标签/未加标签分布、小时/二维热力图和秋季重复小时合并。Project 过滤另覆盖 canonical UUID 400、Project 404、归档/空项目、Task 改绑/删除重分类、分页收敛、报告/历史独立状态和精确缓存失效。Focus 参数已从 SQLite committed 读取；原生通知、托盘和系统勿扰仍未实现，真实三平台后台/睡眠场景与大账本性能仍需桌面验收。

#### 10.4.13 T-13 命令面板、快捷键与反馈状态

- **需求映射**：5.9。
- **用户流程**：`Ctrl/Cmd+K` 打开命令面板，`Ctrl/Cmd+N` 打开新建任务；空查询优先显示最近使用，面板可搜索已交付页面、真实本地 Task/Project/Client/活动 Inbox、新建任务和设置，支持方向键、Enter、Esc 与 Tab。选择业务结果进入带精确 ID 的详情路由，刷新仍定位同一资源；设置命令可直接打开包括运行诊断在内的指定模块，关闭后焦点返回原触发元素。
- **实现方法**：全局键盘监听仅在当前 WebView 内生效，IME composition/229 期间不执行；页面/安全操作命令在前端过滤，业务输入 200 ms 后调用 `GET /api/v1/search?q=&page=1&page_size=12`。服务端跨四类表执行参数化 LIKE 并转义 `% / _ / \\`，`q` 去空格后必须非空且最多 200 字符；`types` 支持固定白名单筛选，页大小最大 100。排序依次为标题/名称精确、前缀、包含、次要字段包含，再以更新时间、资源类型和 ID 稳定兜底；归档 Project 和 resolved/dismissed Inbox 不返回。前端 strict normalizer 校验类型、资源 ID、matched fields 与路由一致性，并显示加载、错误重试和空态。Task/Inbox 分别由 `/tasks/:taskId`、`/inbox/:inboxItemId` 恢复弹窗，Project/Client 复用已有详情页。命令面板与业务 `Modal` 都 portal 到 `document.body`，面板使用 combobox/listbox 与 `aria-activedescendant`，打开时聚焦输入框、圈闭 Tab；二者共用 overlay stack、背景滚动锁、最上层 Escape/`aria-modal`/`inert` 语义与延迟焦点恢复，逐层关闭或父子同批卸载时都恢复到仍连接的正确触发元素。收入/发票仅有骨架，不注册命令。
- **最近使用实现**：前端本地键 `opc-command-recents-v1` 最多保存 8 条、90 天过期的命令 ID 或资源类型/ID；空查询时优先展示。业务资源的标题和状态不持久化，面板每次打开都通过现有本地详情 API 回读当前资料；确认 404 后自动删除记录，暂时失败则不展示伪造的缓存内容。
- **关键路径**：`services/sidecar/internal/api/search.go`、`search_test.go`、`apps/web/src/App.tsx`、`api/client.ts`、`api/hooks.ts`、`api/search.test.ts`、`components/GlobalShortcuts.tsx`、`components/overlayStack.ts`、`components/Modal.tsx`、`components/Modal.test.tsx`、`components/CommandPalette.tsx`、`components/CommandPalette.test.tsx`、`components/TaskDetailModal.tsx`、`pages/InboxPage.tsx`。
- **验证/当前限制**：Go 契约测试覆盖四类型结果、归档/终态过滤、类型去重、稳定分页、文字 `%/_`、非法查询和只读空库；前端覆盖 strict 契约、防抖、错误重试、精确导航及 Task/Inbox 直达路由、最近使用的容量/去重/过期/非敏感存储/404 清理、Sidecar 状态白名单规范化与诊断模块、全局错误边界，以及全局启动故障恢复页的状态门禁/脱敏/日志/重启。诊断包测试解压白名单 ZIP 并验证客户名、业务正文、Token、数据库路径和监听地址不泄漏。Sidecar/Tauri 壳已有脱敏轮转日志和打开目录入口，但不打包原始日志；当前不是 FTS/模糊搜索，没有高亮、数据库打开前恢复进度或 OS 全局快捷键；真实浏览器和桌面 WebView 焦点/视觉仍需人工验收。

#### 10.4.14 T-14 设置持久化

- **需求映射**：5.7 专注设置、8.4 本地配置、设置模块文档。
- **用户流程**：应用启动先读取四个设置模块并在成功应用 committed 前阻止业务界面渲染；若有头像引用则鉴权读取受控内容。设置弹窗允许名称/头像/通用/外观/专注即时预览，取消恢复打开时快照；保存头像时与全部变化模块一起提交，保存失败保留 draft/preview，版本冲突刷新 Query 并要求再次确认。
- **实现方法**：schema v16 的 `app_settings` 保存四模块完整 JSON 和版本；schema v27 的 `workspace_avatars`/tombstone 保存受控头像生命周期。普通设置走原子 PATCH；头像走首 manifest 的严格 multipart，replace 先 staging/提升文件，事务内退休旧头像、插入新身份并应用设置，失败补偿新文件，成功后清理旧文件。启动协调处理删除残留、完整性状态与未知文件 quarantine。前端 TanStack Query 缓存服务端快照，Zustand 只承载 committed 镜像和 draft/preview，Blob URL 不持久化。
- **兼容边界**：旧 displayName 迁入 workspace 品牌，不改 owner Actor；Data URL 只在服务端无头像时解码导入，不进入 SQLite、事件、日志或业务 JSON。已有服务端头像拒绝被旧缓存覆盖；没有旧缓存时不写入默认行。
- **关键路径**：`016_app_settings.sql`、`027_workspace_avatar.sql`、`internal/api/settings.go`、`settings_avatar.go`、`artifact_store.go`、`backups.go`、`backup_restore.go`、`apps/web/src/api/client.ts`、`api/hooks.ts`、`settings/bootstrap.ts`、`components/SettingsBootstrap.tsx`、`components/SettingsModal.tsx` 和 `store/settings.ts`。
- **验收**：API/迁移测试覆盖默认读取、严格 schema、原子保存/回滚、乐观锁、头像格式/大小、替换/移除墓碑、完整性读取、启动协调、备份/演练/恢复与业务导出；前端覆盖即时预览、取消、受控保存、旧 Data URL 原子迁移和服务端优先。真实浏览器/WebView 与窄屏视觉仍需人工验收。

#### 10.4.15 T-15 AI 助手

- **需求映射**：5.10、9.1、9.2。
- **当前状态**：未开始；没有模型依赖、本地 Adapter 配置、会话 API、前端路由或占位按钮。
- **建议开发流程**：
  1. 先完成本地运行时、资源预算、上下文权限和质量评测 ADR；PRD v6.5 当前阶段不评审远程 Provider。
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
- **当前状态**：部分完成。T-18A、T-18B、T-18C 和 T-18D D1/D2 已交付；任务已有负责人/审核人、六状态/六命令、Submission/Artifact、manual 提交验收、受控文件和通用 Task 时间线。剩余编排依赖 Inbox/Reminder，实际 agent 执行归 T-19。
- **T-18A（已完成）**：schema v7 新增 `actors`、`task_assignments` 和 `workflow_events`；使用固定 UUID 初始化唯一 owner/system，以 Task ID 确定性回填历史 owner assignee 和 `migration_assignment_backfill` 事件。未完成任务保持活动分派，已完成任务以 `completed_at`、缺失时 `updated_at` 结束；迁移重放不重复。数据库约束内置记录、活动 Actor/分派人、owner reviewer、同 Task/role 唯一活动记录、结束历史不可覆盖、外键与停用保护。Artifact 未在此迁移提前建表，避免脱离 T-18D 的受控存储、校验和验收事务单独落地。
- **T-18B（已完成）**：实现 `GET/POST /api/v1/actors` 与 `GET/PATCH /api/v1/actors/:id`。列表支持分页、type/status 筛选和白名单稳定排序；v0.1 只允许创建 person。创建可携带 `Idempotency-Key`，保存规范化 SHA-256 与首次 `201` 快照，重放返回相同资源且不重复写事件；不同请求体复用 key 返回冲突。详情、创建和更新返回 `ETag`，PATCH 强制 `If-Match`：缺失为 `428 VERSION_REQUIRED`，格式错误为 `400 INVALID_VERSION`，旧版本为 `409 VERSION_CONFLICT`。owner 只可修改展示名，system 与 agent 不可编辑，person 可编辑展示名、备注、非敏感 metadata 和 active/inactive；存在活动 Assignment 时返回 `409 ACTOR_HAS_ACTIVE_ASSIGNMENTS`，失败不递增版本也不写事件。成功创建、更新和停用与 `actor_created / actor_updated / actor_deactivated` 事件同事务提交。
- **设置页（已完成 T-18B 范围）**：“人员与责任”读取真实 API，展示内置主体和本地人员；支持新建/编辑/停用/重新启用 person、编辑 owner 展示名、错误重试、空状态、字段校验和版本冲突草稿。界面明确说明 person 只是本机记录，不发送、同步或创建线上账号；metadata 只接受 JSON object，并提示不得填写密码、令牌、API key 等敏感信息。该模块的写入独立保存，不代表其他设置已迁入 `app_settings`；本轮没有浏览器视觉验收证据。
- **T-18C（已完成）**：实现 `GET/POST /api/v1/tasks/:id/assignments`、`POST /api/v1/tasks/:id/reassign` 与 `POST /api/v1/assignments/:id/end`。查询返回当前 assignee/reviewer、分页结束历史、`meta.task_version` 和 Task `ETag`；三个命令强制 Task `If-Match`，成功递增 Task 版本，并支持可选 `Idempotency-Key` 保存请求摘要、首次响应和状态码。安全重放不重复写事件，异体复用返回 `IDEMPOTENCY_CONFLICT`。v0.1 assignee 只允许 active owner/person，reviewer 只允许 active owner；system/agent 不可分派。改派原子结束旧记录、创建新记录并写前后快照事件。Task 转为 `done` 时在同一事务结束全部活动 Assignment，重新打开不恢复；Assignment 没有 DELETE，Task 永久删除按外键级联删除 Assignment，并保留 `assignment_id` 置空的事件 JSON 快照。
- **T-18D D1（已完成）**：schema v8 扩展 Task 六状态与 lifecycle 字段，旧数据/版本/关系原样迁移；实现 start/block/unblock/complete/cancel/reopen、Task `If-Match`、可选幂等快照、活动 assignee/review policy 校验、完成/取消的 Assignment 原子联动，以及带 `command_seq` 的分页 Task 时间线与事件不可变保护。前端提供六状态分组、详情生命周期操作、冲突草稿和按需时间线。
- **T-18D D2（已完成）**：schema v9 新增带不可变 database ID/一次性 store 绑定的 `workspace_identity`、`task_submissions`、`task_artifacts`、不可变 `artifact_deletion_tombstones`、Task `current_submission_id` 和 Event `submission_id/artifact_id`；迁移对无歧义旧 manual 状态回填 inferred Submission 与 `migration_submission_backfill`，不编造 Artifact。创建/受限 PATCH 开放 none/manual；实现 JSON/multipart submit-output、owner accept/request_changes、列表/详情/content、确认软删除、Task 硬删除文件补偿，以及 `task_review_policy_changed / task_output_submitted / task_review_accepted / task_changes_requested / task_submission_withdrawn / task_artifact_deleted`。Artifact producer=active assignee；recorder/submission submitter/reviewer/withdrawer/deleter=owner。受控 root 使用 database/store 双向绑定 marker、进程级独占锁与 `.staging/objects/.trash/.quarantine`，file 路径固定 `objects/<artifact-id>`，关键文件/目录项耐久同步，文件下载复验 size/SHA-256；模糊 COMMIT 不误删对象，tombstone/哈希校验支持安全恢复，缺失 object 不阻断删除，无引用未知候选不自动永久删除。前端交付“产出与验收”模块、严格响应解析、分页历史、缺失/篡改/删除状态、120 秒安全传输和冲突草稿完整保留；服务端传输窗口为 180 秒。
- **限制与互锁**：manual 策略变化仅 todo+无历史；提交只允许 todo/in_progress 且需要 active assignee + owner reviewer；每批 summary 或至少一项 Artifact，最多 20；accept/request_changes/cancel 后保留最近 Submission 指针，reopen 清空。所有 Task 写入在 UI 互斥，成功立即失效 Task/Submission/Artifact/Assignment/Event/Project/Today cache。
- **关键路径**：`services/sidecar/internal/database/migrations/007_actor_assignments.sql`、`008_task_workflow.sql`、`009_task_submissions_artifacts.sql`、`internal/api/actors.go`、`assignments.go`、`tasks.go`、`task_outputs.go`、`artifact_store.go`、`apps/web/src/components/TaskAssignmentsSection.tsx`、`TaskOutputsSection.tsx`、`TaskArtifactCard.tsx`、`TaskDetailModal.tsx`、`apps/web/src/api/client.ts`、`hooks.ts`。
- **验证/剩余**：数据库 v6→v9、迁移回滚/外键恢复、索引/trigger/不可变与 inferred 历史均有测试；Go 全包与 `go vet` 通过，database 重复 10 次通过。前端全量测试、typecheck/build/format 通过，覆盖 manual 前置、混合提交、accept/request_changes、冲突 File 草稿、下载错误和软删确认。Artifact 基础备份恢复已由 T-04B 覆盖；Agent、其他 Inbox 来源投影和专项浏览器视觉验收仍未完成。

#### 10.4.19 T-19 本地 Agent 执行

- **需求映射**：5.6、6、9.2。
- **版本归属**：v0.2 本地编排版；在 Adapter、能力模型、路径授权和崩溃恢复协议完成前，不把 agent Actor 显示为可执行。
- **当前状态**：未开始；没有 Agent Adapter、Run、能力令牌、产出或恢复逻辑。
- **执行流程**：owner 启动 Run → Sidecar 校验 agent 健康与任务 Assignment → 生成短时单次能力令牌和输入快照 → 本地执行器在限定资源内运行 → 产出写入受控目录 → Run 成功后任务进入 `waiting_review` → owner 接受或返工。
- **安全约束**：无任意 Shell、无数据库直连、无 WebView Bearer Token；路径和操作逐项白名单；删除、对外发送、付款确认等高风险动作不可委托。v0.2 上线前必须形成 Adapter/专用鉴权/令牌传输/跨平台进程沙箱与网络阻断 ADR；无法强制禁网的平台不得启用正式 Agent 执行。
- **恢复策略**：异常退出后 running Run 标记 `interrupted`；重试创建新 Run；可能产生副作用的动作不得静默重试。
- **验收要求**：禁网可用、超时/取消可靠、失败有结构化错误、产出带 SHA-256、成功不自动完成任务、返工保留全部历史、Sidecar 重启后 Run/Assignment/时间线一致。

#### 10.4.20 T-20 测试、构建与桌面验收

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
- **环境边界**：当前 Windows 主机可完成 Go 全量/vet、Rust 格式与测试源码静态复核，但缺少 MSVC `link.exe` 和 Windows SDK，因此 `cargo check` / `cargo test` 在依赖构建脚本链接阶段受阻。当前结果不能作为桌面完整编译、安装包或跨平台验收证据。

### 10.5 当前验证矩阵

| 检查                               | 最近已知结果 | 说明                                                                                                                                                                                                                                                     |
| ---------------------------------- | ------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| schema v9→v10 迁移定向验证         | 通过         | 保留 v9 的 database/store、Submission/Artifact/Event 约束；覆盖 v9→v10 Client 数据保留、空白可选值归一、version、索引、Project 关联 trigger、外键检查和重复数据库回归                                                                                    |
| schema v11→v12 迁移定向验证        | 通过         | `012_inbox_items.sql` 为加法迁移；覆盖真实 v11 边界升级、既有 Client 事实保留、fresh v12 约束、nullable `source_event_key` 部分唯一索引和无 Inbox seed                                                                                                   |
| schema v12→v13 迁移定向验证        | 通过         | Go 测试覆盖加法迁移、既有事实保留、活动关系唯一、稳定非连续 position/软解除约束、nullable Task 外键、删除快照、活动关系 Task 删除保护、父 Inbox 级联和 `foreign_key_check`                                                                               |
| Client / Project 选择与关联        | 通过         | Go 测试覆盖 Client CRUD/关联及 Project 所有排序字段的 ID 后备键、同名跨页无重无漏与搜索/状态/客户/归档过滤；Web 覆盖 Client/Project 单页 Query 与取消、两类共享选择器搜索分页/跨页保留，以及 Project 表单、Task 新建/编辑/筛选/批量移动与 Inbox 拆分接线 |
| 手工 Inbox API / 事务              | 通过         | Go 测试覆盖 manual 边界、三视图/全局未读、搜索/分页、PATCH 乐观锁、命令幂等、read/snooze/终态独立事实、截止式 read-all、事件分页，以及事件失败时 PATCH/read-all 全事务回滚                                                                               |
| Inbox–Task 关系 API / 事务         | 通过         | Go/Web 测试覆盖活动/历史读取、实时进度、关联/required/软解除、幂等与 Inbox 版本冲突、`open / tracking`、按关系 reopen、关系事件、Task 删除互锁、删除后历史快照、候选失效、禁用传播及冲突草稿保留                                                         |
| D2 API / Artifact store            | 通过         | Go 定向测试覆盖 manifest-first multipart、大小/类型/Actor 归属、提交/接受/返工/撤回、幂等/版本冲突、分页/详情、完整性下载、软删/Task 硬删 tombstone、数据库绑定 marker、进程锁、耐久同步、模糊 COMMIT、trash 哈希恢复、quarantine 和启动 reconcile       |
| schema v29→v30 迁移                | 通过         | 定向数据库测试覆盖既有 Submission 保留并默认 manual、origin/system/non-inferred/no-Artifact/历史不可变 guards、无 destructive gate、迁移失败回滚与外键检查；最终工作区继续迁移到最新 schema=31                                                           |
| 父任务自动待验收                   | 通过         | Go 定向测试覆盖直属非取消数>0且全部 done、全取消、责任/策略门禁、system child_rollup、幂等不重复、owner accept、失效撤回、blocked 来源更新、manual/changes_requested 保护、accepted 祖先重开及创建/改绑/删除/批量版本                                    |
| schema v30→v31 迁移                | 通过         | 定向数据库测试覆盖 v30 事实与 Client version 保留、不回填历史 Project Event、`project_workflow_event` 部分唯一索引及其他来源豁免、无新增 source FK、无 destructive gate、回滚、外键检查及最新 schema=31                                                  |
| Project→Client Activity 投影       | 通过         | Go 定向测试覆盖 complete/reopen 完整行、system Actor/空正文/同时间、无 Client 跳过、Client version/latest、混合精度/offset 时间顺序、陈旧版本不重复、改绑不搬移，以及 Event/Activity/完成 Inbox 任一下游失败全事务回滚；Web 覆盖只读可读文案与缓存刷新   |
| 既有 Task/Actor/Assignment 回归    | 通过         | 六状态、策略锁定、Assignment 联动、Event 关联与不可变、旧 status 410、Task facts/Project 聚合及 Actor 权限由 Go 全量回归覆盖                                                                                                                             |
| `pnpm --filter @opc/web typecheck` | 通过         | 本次覆盖 `restarting / generation` 严格运行期契约、连接世代重置及既有 Task D2、Client/Project/Inbox 契约                                                                                                                                                 |
| 前端测试                           | 通过         | 全量 79 个文件 / 524 项通过；新增覆盖 restarting/generation、非 ready 连接与 TanStack Query 清理、generation 变化补偿，既有业务回归继续通过。组件测试不替代真实 WebView/父崩溃验收                                                                       |
| Web 生产构建                       | 通过         | `pnpm build:web` 的 TypeScript 与 Vite 构建通过；当前主入口 minified 约 817 kB、gzip 约 222 kB，仍有非阻断的 500 kB chunk 警告，后续按页面拆包                                                                                                           |
| 前端格式                           | 通过         | Web 范围 Prettier format check 通过                                                                                                                                                                                                                      |
| Go 全量测试与静态检查              | 通过         | `go test ./... -count=1` 与 `go vet ./...` 均通过；不虚构未统计的用例总数                                                                                                                                                                                |
| 文档链接/过期措辞/diff             | 通过         | 本次 v9.19 文档更新后 Prettier 与 `git diff --check` 通过                                                                                                                                                                                                |
| Rust 格式与源码复核                | 部分通过     | `cargo fmt --check` 通过；generation、重启预算/退避、稳定重置、真实退出门禁、安全应用重启和并发 shutdown 的新增单元测试源码经独立静态复核 P0/P1=0，但当前主机未执行 Rust 测试                                                                            |
| Rust / Tauri 链接                  | 环境受限     | 当前主机缺少 MSVC `link.exe` 和 Windows SDK；`cargo check` / `cargo test` 在链接阶段受阻，不能声明完整桌面链接通过                                                                                                                                       |
| 浏览器渲染 / 窄屏视觉              | 本次未验收   | 本轮未执行真实浏览器/WebView 人工深链、键盘与焦点、Portal 窄屏浮层及 1,000/10,000 条 Project/Task/Inbox 性能验收                                                                                                                                         |
| 桌面安装包与三平台验收             | 未执行       | 安装包、签名/公证、干净系统、性能和三平台矩阵仍未完成                                                                                                                                                                                                    |

根 `pnpm check` 当前只执行 TypeScript、Go 测试、Web 构建和 `cargo check`，没有覆盖前端测试、Rust 单元测试或格式检查；在修正聚合脚本前不得把单次 `pnpm check` 视为完整质量门禁。

### 10.6 已知限制与下一步顺序

1. **D1/D2 与父任务待验收已交付，收口任务体验**：schema v8 生命周期、schema v9 manual Submission/Artifact 与 schema v30 直属子任务驱动的 system child_rollup 已完成；系统最多进入 waiting_review，manual/changes_requested 不覆盖，pending 失效撤回，accepted 后子任务失效重开，Inbox required 保持显式独立。当前不做迁移/启动全库回填，旧 ready parent 要等待后续相关写命令；system reopen 不恢复已结束 Assignment。Today 已按 `planned_date` 完整查询并接入按钮、同日/跨日期拖拽和安全快捷操作，任务页也已支持精确日期计划组内同状态拖拽、六状态看板和跨列受控生命周期命令。继续做真实浏览器键盘/焦点/窄屏验收、长历史性能和错误恢复。
2. **收口项目基础纵切**：CRUD、聚合版本与生命周期、共享 Client/Project 选择器、任务聚合、笔记/附件、Task Artifact 聚合、活动时间线、项目级 Focus、Task 相关来源、Project 完成→Inbox 和 complete/reopen→Client Activity 已交付；本轮又交付 nullable follow-up/实时 required 进度、兼容的 Project 数值 ETag/独立 Inbox version 边界、产出区上移、Inbox 深链、来源 Project 继承、完成条件、owner/person 分派、manual owner 验收和 Go 自动化金链。下一步是两类选择器及完整人工闭环的真实浏览器/WebView、窄屏、焦点返回和 1,000/10,000 条数据专项，不再把功能事实链列为未实现。
3. **收口客户基础事实并推进真实扩展**：schema v10、Client CRUD/搜索/删除约束、基础详情和 Project 客户关联，schema v18 人工活动时间线、schema v19 受控附件、schema v20 person 显式关联、schema v31 Project 生命周期本地系统动态，以及三个入口共用的大客户量服务端搜索选择器已交付；下一步补真实浏览器/大数据量证据，再在具备真实集成来源时按独立纵切接入外部事实。回访与财务仍属于 v0.4。
4. **继续收件箱来源投影**：T-11A1/A2/A3/B/C/F 的手工受理分诊、已有 Task 关系、Reminder、批量拆分/Assignment、统一 reconciliation、自动解决和运营统计，以及 follow-up Artifact/Task 阻塞/Task 临期、备份四类操作失败、数据库启动/迁移、Sidecar 启动、运行期数据库操作失败和可配置主动低空间投影均已交付；物理卷同卷去重及无路径手动容量检查已在设置页交付，下一步继续真实业务来源，卷级趋势保持独立纵切。
5. **扩展 Focus D2b**：Core A+B+C、D1、D2a、Project 详情读取与日期范围回顾的持久化、恢复、精确工时、Today 统计、终态历史、7/30 天/本月/自定义趋势、Streak、项目/当前标签时间分布、DST 安全小时分布/最佳时段、周几×小时二维热力图，以及 Task/Project 详情记录已交付；后续独立实现经平台验收的原生通知、托盘和 DND 引导。
6. **补数据安全链路**：一致性备份完整闭环、迁移前自动回滚、启动后恢复结果诊断、业务 JSON 与含文件 ZIP 的空工作区同 schema 安全导入导出、脱敏诊断包 v1 和全局启动故障恢复页 v1 已交付；下一步补跨 schema/非空目标冲突映射、大数据量进度与数据库打开前备份选择/实时恢复进度。
7. **补桌面可靠性与发布能力**：有界 generation 重启、数据库运行锁、父管道 EOF、前端世代清理、安全应用重启、并发 shutdown、日志/request ID 和全局恢复页已交付；继续真实父崩溃/进程树、hard-hung orphan/孙进程治理、三平台安装包、数据库打开前恢复进度及系统集成。当前不宣称 Job Object 或进程组能力。
8. **实现 v0.2 本地 Agent**：确定 Adapter、能力令牌、路径授权和崩溃恢复协议后，再接 Agent Run、复用 D2 产出命令、取消/重试和审核返工。
9. **实现本地规划与预设自动化**：路线图、内容日历和规则引擎只创建本地收件箱项/任务，不自动对外发送。
10. **排期 v0.4 业务版**：实现客户回访、收入/支出/发票纵切及其本地事件源；先数据契约和 CRUD，再做提醒、统计图和 PDF。
11. **最后评审知识库与 AI**：先完成本地 FTS、引用、删除和恢复，再单独评审模型、数据外发与权限；不得让 AI 阻塞核心本地工作流。

非敏感用户设置的 schema v16 `app_settings`、严格服务端清洗、GET/PATCH API、前端 Query committed 和旧 `localStorage` 缺失模块迁移已交付；schema v27 已交付受控头像 multipart/读取/迁移/清理/备份恢复。`appDataDir/config` 只保存数据库启动前必需的非敏感运行配置；服务端 `stored/version` 与 `avatar_ref` 是迁移判据，验证后删除旧键。专注运行态已经是持久化 Session。

### 10.7 各模块具体实施计划

| 顺序 | 模块                      | 后端与数据                                                                                                                                                                                                                                                                                                                                                                                            | 前端与用户流程                                                                                                                                                                                                                                | 完成验收                                                                                                                                                                                                                                                                                                                             |
| ---- | ------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 1    | 任务基础事实与生命周期 D1 | schema v6 事实、schema v8 六状态/六命令、schema v30 父任务系统待验收、非状态 PATCH、项目/父子/标签/完成标准、Task/Tag 版本、分页筛选、批量安全操作、计划组排序和 Task 时间线已交付；任务 Project 选择只读稳定 Project API 单页，不再串行拉全分页                                                                                                                                                      | 详情冲突草稿、六状态分组、生命周期操作、按需事件、标签/层级/筛选/批量/按钮、同状态拖拽排序、六状态看板、跨列受控生命周期、非取消进度/取消数与 child_rollup/门禁/系统事件文案，以及新建/编辑/筛选/批量移动/Inbox 拆分共用 ProjectSelect 已交付 | 覆盖 CRUD、转换矩阵、负责人/原因、幂等/并发、终态 Assignment、事件顺序/不可变、cancelled 统计、ProjectSelect 请求取消/分页/选择保留/归档/反馈/键盘与五处接线、计划组拖拽、看板命令映射/确认/人工验收门禁，以及父任务直属子任务口径、system rollup、撤回/blocked/accepted 重开、manual/changes_requested 保护与 Inbox required 独立性 |
| 2    | Actor、分派与任务验收 D2  | T-18A/B/C 与 T-18D D1/D2 均已交付：manual policy、Submission/Artifact、submit-output、accept/request_changes、受控文件和迁移回填                                                                                                                                                                                                                                                                      | Actor/Assignment/生命周期/时间线、混合 Artifact 草稿、当前批次、分页历史、安全下载与确认软删均已交付                                                                                                                                          | 已覆盖角色限制、manual 不可绕过、版本/幂等、Actor 归属、产出历史、文件补偿、冲突保留 File 草稿；剩余真实浏览器键盘/焦点/窄屏和长历史性能验收                                                                                                                                                                                         |
| 3    | 项目                      | CRUD、schema v4 快照式创建幂等、schema v5 聚合版本、乐观锁、归档关联约束、状态流转、归档恢复、确认硬删除、任务派生进度/工时、Client 选择/筛选、稳定 ID 后备排序与同事务 Count/Scan、Project Note/Attachment/Event、Task Artifact 聚合及 nullable follow-up/实时 required 进度、项目级 Focus、schema v23 follow-up、schema v28 Project 完成→Inbox 及 schema v31 complete/reopen→Client Activity 已交付 | 卡片、筛选、共享选择器、任务树/平铺、产出区上移、四种跟进状态、阻塞/待验收/取消提示、Inbox 深链、人工笔记、附件、Focus、归档恢复和活动时间线已交付；写入复用 Inbox/Task 详情                                                                  | 自动化已覆盖兼容 Project 数值 ETag、独立 Inbox version、follow-up 契约、来源 Project 继承/清除、完成条件、owner/person + manual owner reviewer、共享 Task 详情、缓存失效及 complete/submit/accept→automatic resolved/100% 金链；真实浏览器/WebView、窄屏、焦点返回和大数据量仍待专项验收                                             |
| 4    | 客户                      | schema v10 基础事实、schema v18 活动、schema v19 附件、schema v20 person 关联和 schema v31 Project 生命周期系统动态已交付：Client CRUD/搜索/状态/排序、聚合版本、删除约束、项目/活动/附件，以及显式 contact 关联/解除历史；既有列表 API 已支持共享选择器，无新增 schema；外部来源集成及财务聚合待实现                                                                                                 | 表格、新建/编辑、基础详情、完整关联项目、停用/恢复、危险区、人工活动、只读项目状态动态、附件/本地联系人，以及 Project/Task 三入口的共享分页搜索 ClientSelect 已交付；回访/发票详情仍明确为后续且不伪造线上行为                                | 已覆盖字段校验、迁移、分页/并发、ClientSelect 单页 Query/取消/搜索/分页/跨页保留/状态/反馈/键盘、活动审计、项目来源唯一/回滚/改绑历史归属、附件生命周期/备份恢复、person 原子创建/关联/解除、停用保护、Client 删除和版本传播；剩余真实浏览器/1,000/10,000 条性能与外部来源集成，回访/财务保持 v0.4                                   |
| 5    | 收件箱人工编排            | **T-11A1/A2/A3/B/C/F 已交付**：Inbox/关系/Reminder/编排事实，manual 受理、Task 拆分分派、自动结清/重开和实时运营计数；follow-up/阻塞/临期/Project 完成及系统维护来源已交付                                                                                                                                                                                                                            | **已交付**：三视图、详情/分诊、Reminder、Task 关系/拆分、来源 Project 继承/清除/改选、完成条件、person 本地责任提示、共享 Task 详情、强制例外、风险筛选及来源 Project/Task/Today 缓存失效                                                     | 当前自动化覆盖事务回滚、实时进度、Task 删除互锁、稳定来源、拆分、owner/person/manual reviewer 和 automatic resolved/100% 金链；真实浏览器/WebView、窄屏、焦点和大数据量仍待验收，Agent/AI 不在 v0.1                                                                                                                                  |
| 6    | 今日                      | 按本地日期、逾期和本周范围查询；完整计划组排序事务；版本化单任务改期/生命周期/删除命令；Focus Session 幂等创建；按 IANA 时区计算 UTC 边界；收件箱派生计数                                                                                                                                                                                                                                             | 日期导航、真实分组、按钮/同日/跨日期拖拽、空精确日期/未排期落点、任意日期安排、安全执行快捷操作及编辑/确认删除入口已交付；财务卡标后续                                                                                                        | 已覆盖完整分页、排序集合/版本、终态槽位、跨组乐观回滚、空组、改期冲突/模糊响应、排序部分成功、快捷策略和删除保护；仍需真实浏览器验证 hover/focus、日期控件、指针拖拽、窄屏及午夜/夏令时边界                                                                                                                                          |
| 7    | 专注                      | **Core A+B+D1+D2a+Project+D2b 分析已交付**：schema v11 Session/interval/ledger、状态 API、绝对时间、心跳/恢复、幂等并发、Task 工时事务、IANA Today/周期统计、终态历史、Task/Project 过滤与当前关系派生                                                                                                                                                                                                | **Core C 与详情读取已交付**：任务选择、无绑定确认、共享活动快照、恢复弹窗、设置隔离与循环/休息、Focus 页面高级分析、Task 记录及 Project 7/30 天/月度分析与历史                                                                                | 自动化已覆盖跨午夜/DST、并发/重复 stop、恢复、余秒、事务、canonical Project 错误、归档/改绑/删除语义、分页、独立状态和缓存边界；真实三平台后台/睡眠、原生反馈与大账本性能后续验收                                                                                                                                                    |
| 8    | 设置与命令面板            | Actor API、health/version、Tauri Sidecar 生命周期、schema v16 app_settings、schema v27 受控头像、Query committed、旧 localStorage 迁移、手动备份/恢复/业务导出、诊断包、Sidecar/Tauri 壳轮转日志、打开日志目录、WebView→Sidecar request ID、全局启动故障恢复页 v1 及统一 search API 已接入                                                                                                            | “人员与责任”支持 owner/person；个人资料支持名称与头像即时预览/保存/取消；设置支持 8 个左栏模块，运行诊断可重查、复制脱敏摘要、下载诊断包并打开日志目录；命令面板支持多实体搜索和稳定详情路由；桌面故障时在设置 bootstrap 前提供安全恢复动作   | 已覆盖设置/头像/备份/导出、诊断包白名单与隐私、双进程日志脱敏/轮转/降级、request ID、启动门禁、搜索契约、IME、焦点恢复、health 与桌面状态脱敏；仍需真实 WebView/窄屏验收，通知、OS 快捷键和数据库打开前恢复进度仍为后续                                                                                                              |
| 9    | 数据安全                  | 已交付一致性快照、验证/演练/恢复/删除、迁移/恢复/导入前回滚点、启动后恢复结果诊断、业务 JSON 与含文件 ZIP 的空工作区安全导入导出、失败 Inbox 投影；v0.3 增加计划/映射                                                                                                                                                                                                                                 | 已交付备份全流程、恢复状态重查/门禁、两类业务下载及两类导入的预检、确认与结果反馈；冲突合并导入仍不开放                                                                                                                                       | 已覆盖真实 Artifact、篡改、恢复、恢复诊断脱敏、导出隐私/ZIP 完整性，以及 JSON/ZIP 导入门禁、回滚、trigger/外键、文件校验和失败补偿；仍须覆盖低磁盘、数据库打开前进度、跨 schema/非空目标与真实磁盘故障                                                                                                                               |
| 10   | 桌面与发布                | **部分完成**：generation-aware 两次有界重启、数据库运行锁、父管道 EOF、并发 shutdown、日志/request ID、全局恢复页与安全应用重启已交付；真实父崩溃/进程树、孤儿/孙进程治理、托盘/通知/快捷键/自启/签名更新及三平台 CI 待实现                                                                                                                                                                           | **部分完成**：starting/restarting/ready/error + generation、非 ready 连接/查询清理、状态重查、日志入口、安全重启和版本白名单已交付；备份选择/恢复进度、托盘与离线更新反馈待实现                                                               | Web 79 文件/524 测试、Go full/vet 与 Rust fmt 已通过，Rust 新测试源码静态复核 P0/P1=0；cargo check/test 受缺失 MSVC link.exe/Windows SDK 阻塞，真实父崩溃、进程树、签名、公证、干净机和三平台仍须验收                                                                                                                                |
| 11   | 本地 Agent（v0.2）        | Adapter ADR、专用鉴权、短时令牌、跨平台沙箱/网络边界、Agent Run、取消/重试/中断恢复                                                                                                                                                                                                                                                                                                                   | 只显示健康且隔离已验证的 Agent；启动、运行、输出、失败、重试、待验收和返工                                                                                                                                                                    | 无任意 Shell/数据库/目录；禁网无法验证时执行保持禁用；成功进入 waiting_review；产出校验和历史完整                                                                                                                                                                                                                                    |
| 12   | 预设自动化（v0.2）        | 规则和执行记录表，以 Workflow Event 触发，只允许创建本地 Inbox Item/Task/提醒，`rule_id + event_id` 去重                                                                                                                                                                                                                                                                                              | 规则开关、下一次触发、运行日志和失败详情                                                                                                                                                                                                      | 用户时区、漏执行补偿、去重、禁用、失败重试和递归循环防护；不自动对外发送                                                                                                                                                                                                                                                             |
| 13   | 路线图（v0.3）            | roadmap_milestones、季度/日期/状态/项目关联，进度从项目/任务派生                                                                                                                                                                                                                                                                                                                                      | 季度分组、里程碑新增/编辑、日期调整、归档和项目跳转                                                                                                                                                                                           | 季度边界、进度口径、项目删除、拖拽/编辑回滚和空/错误/重试                                                                                                                                                                                                                                                                            |
| 14   | 内容日历（v0.3）          | content_items、平台/状态/发布时间/项目与任务关联，审核/发布时间幂等生成 Inbox Item                                                                                                                                                                                                                                                                                                                    | 月视图、月份切换、详情、新建/编辑、拖拽排期和准备任务                                                                                                                                                                                         | 跨月、时区、拖拽回滚、关联任务和重复提醒；不自动发布外部平台                                                                                                                                                                                                                                                                         |
| 15   | 财务/发票/回访（v0.4）    | financial_entries、Invoice/Followup CRUD、聚合、PDF、本地调度和幂等 Inbox 事件                                                                                                                                                                                                                                                                                                                        | 账本、统计、发票状态/PDF、回访时间线和待办；外发/付款确认由 owner 操作                                                                                                                                                                        | 金额整数、编号唯一、PDF 失败不改状态、付款与财务记录原子一致、时区与去重正确                                                                                                                                                                                                                                                         |
| 16   | 知识库/AI（版本待定）     | 本地导入、FTS5、引用、删除和索引恢复先行；模型运行时另行 ADR                                                                                                                                                                                                                                                                                                                                          | 来源定位、索引状态、检索与建议预览；AI 只能提交建议或 Task Artifact                                                                                                                                                                           | 来源准确、无答案、提示注入、越权路径、删除完整、离线可用；任何写操作需批准                                                                                                                                                                                                                                                           |

依赖主线：

```text
已交付：Task 事实与项目关联 + Project 基础纵切
  → Task 完整事实层
  → Actor 身份 + Assignment/Event 数据基础
  → Assignment 操作
  → 受控任务状态 D1 + Task 时间线
  → Artifact + manual 提交验收 D2 + schema v30 父任务 child_rollup 待验收
  → 已交付：Client 基础 CRUD + Project 客户选择/筛选 + Client 人工活动时间线 + 受控附件 + person 显式关联
  → Client 来源投影 + Project 活动与事件增强
  → 已交付：手工 Inbox Item / 受理 / 分诊 / 归档事件
  → 已交付：Inbox 已有 Task 活动/历史关系、Reminder、拆分/人工分派与自动解决、follow-up/阻塞/临期/备份四类操作失败/数据库启动与迁移/Sidecar 启动/运行期数据库操作失败/固定低空间来源投影；继续阈值配置评审与真实业务来源
  → 已交付：Focus Core 持久化/工时/Today 统计与今日完整日期编排、执行快捷操作和编辑/确认删除入口；真实浏览器验收继续
  → 已交付手动备份创建/列表/完整校验/隔离演练/重启恢复/确认删除和基础业务 JSON；继续迁移前备份、桌面日志和故障恢复
  → 本地 Agent
  → 预设自动化与后续业务事件源
```

在对应纵切完成前，客户回访、发票“新建”和收入时间范围等无业务行为控件必须禁用或明确标记“后续版本”，不得用可点击外观暗示已经实现。Client 新建/编辑/基础详情/人工活动时间线/受控附件、Project 客户选择/筛选/追加式活动时间线、Inbox“全部已读”/已有 Task 关系/批量拆分分派/自动结清、一次性 Reminder，以及任务事实/生命周期原子批量操作和任务看板受控跨列交互已接入真实 API，不再属于占位清单；Client 外部来源投影、其他系统故障 Inbox 来源和重复/原生通知仍未实现。

---

## 附录

### A. 历史原型文件索引（已移除）

下列 HTML 文件名只用于追溯历史设计来源，文件已于 2026-08-27 从仓库移除。当前实现与后续视觉验收以 React 页面、`styles.css`、模块文档和实际渲染结果为准。

| 页面                 | 原型文件                                                                                |
| -------------------- | --------------------------------------------------------------------------------------- |
| 今日工作台           | `today-v1.html`、`today-linear.html`                                                    |
| 任务列表             | `tasks-linear.html`                                                                     |
| 项目列表             | `projects-linear.html`                                                                  |
| 客户列表             | `clients-linear.html`                                                                   |
| 收入看板             | `income-linear.html`                                                                    |
| 发票列表             | `invoices-linear.html`                                                                  |
| 收件箱               | `inbox-linear.html`                                                                     |
| 收件箱详情/拆分/分派 | 尚无新版原型；以 5.6 对象边界和工作流为准                                               |
| Actor 管理           | 无历史 HTML 原型；当前已实现于设置模块“人员与责任”，视觉验收以 React 组件和实际渲染为准 |
| 路线图               | `roadmap-linear.html`                                                                   |
| 内容日历             | `content-calendar-linear.html`                                                          |
| 新建任务弹窗         | `modal-new-task-linear.html`                                                            |
| 任务详情弹窗         | `modal-task-detail-linear.html`                                                         |
| 筛选弹窗             | `modal-filter-linear.html`                                                              |
| 命令面板             | `modal-command-palette-linear.html`                                                     |
| 专注设置             | `modal-focus-settings-linear.html`                                                      |
| 收入详情             | `modal-income-detail-linear.html`                                                       |
| 客户活动             | `modal-client-activity-linear.html`                                                     |
| 自动化               | `modal-automation-linear.html`                                                          |
| AI 助手              | 尚无原型；版本与交互方案待评审                                                          |
| 本地知识库           | 尚无原型；版本与交互方案待评审                                                          |
| 客户回访             | 尚无原型；规划归入 v0.4                                                                 |

### B. 设计规范

- **主题**：深色模式（Dark Mode）为默认，当前已支持亮/暗主题切换；后续再评审跟随系统与自定义主题
- **主色**：紫色 `#5E6AD2` / `#8b5cf6`
- **辅助色**：成功绿 `#4CB782`、信息蓝 `#4E8DF0`、警告红 `#E5484D`、提醒橙 `#F5A623`
- **字体**：Inter（英文）+ Noto Sans SC / PingFang SC（中文）
- **圆角**：4px（小）→ 6px（中）→ 8px（大）→ 12px（卡片）
- **间距**：4px 基准，8px 为单位递增

### C. API 约定

健康检查使用 `/health`；业务 API 统一使用 `/api/v1`。生产环境所有请求（包括健康检查）都必须携带 Tauri 启动 Sidecar 时生成的会话令牌。

| 方法                 | 路径                                      | 说明                                                                                     | 当前状态                                                                                                                                                                                                                                                                                                                                                                                                        |
| -------------------- | ----------------------------------------- | ---------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| GET                  | /health                                   | 健康检查，返回 app/API/schema 版本                                                       | 已实现                                                                                                                                                                                                                                                                                                                                                                                                          |
| GET                  | /api/v1/tasks                             | 分页查询任务                                                                             | 已实现；默认 50/最大 100，支持六个精确状态及 `status=active`、优先级/类型/项目/客户、计划与截止范围、`planned_state=scheduled/unscheduled`、实时 `due_state=overdue/due_soon`、搜索、重复标签 AND、父级/根任务和稳定排序；截止风险排除终态并与 Today 统计共享时钟口径，且拒绝与 due 范围混用；客户沿 Task→Project→Client 当前关系筛选；返回项目/父任务标题、标签，以及直属子任务 total/completed/cancelled 统计 |
| POST                 | /api/v1/tasks                             | 新建任务                                                                                 | 已实现；只允许 todo，支持 `review_policy=none/manual`、类型、父任务、完成标准和标签；`Idempotency-Key` 保存快照；返回 `ETag`                                                                                                                                                                                                                                                                                    |
| GET / DELETE         | /api/v1/tasks/:id                         | 获取、删除任务                                                                           | 已实现；读取返回完整任务事实、`current_submission_id` 与 `ETag`；删除要求 `If-Match`，活动 Inbox 关系返回 `409 TASK_HAS_ACTIVE_INBOX_RELATIONS`，软解除后可删；历史关系的实时 FK 置空但原 Task ID/标题快照保留。其余聚合删除继续协调 Artifact 并级联成员                                                                                                                                                        |
| PATCH                | /api/v1/tasks/:id                         | 更新非生命周期字段                                                                       | 已实现；支持 `review_policy` 与其他任务事实并要求 `If-Match`，拒绝 `status`；策略变化仅允许 todo 且无任何 Submission 历史                                                                                                                                                                                                                                                                                       |
| PATCH                | /api/v1/tasks/:id/status                  | 旧任务状态入口                                                                           | 已废弃；固定返回 `410 TASK_STATUS_ENDPOINT_DEPRECATED`                                                                                                                                                                                                                                                                                                                                                          |
| PATCH                | /api/v1/tasks/batch                       | 原子批量事实与生命周期操作                                                               | 已实现；1–100 条任务，支持 set_project/set_planned_date/add_tags/remove_tags/start/block/unblock/complete/cancel/reopen；每项 expected_version，block/cancel 原因必填，任一校验或副作用失败整批回滚                                                                                                                                                                                                             |
| PUT                  | /api/v1/tasks/reorder                     | 原子保存手动排序                                                                         | 已实现；提交完整 planned_date 组和每项 expected_version，支持 manual/default；组成员或版本变化时拒绝                                                                                                                                                                                                                                                                                                            |
| GET / POST           | /api/v1/task-saved-views                  | 列出或创建任务保存视图                                                                   | 已实现；工作区最多 20 个，名称大小写不敏感唯一；严格保存筛选/排序定义，不保存页码、选择或结果                                                                                                                                                                                                                                                                                                                   |
| PATCH / DELETE       | /api/v1/task-saved-views/:id              | 更新或确认删除任务保存视图                                                               | 已实现；要求 `If-Match`；更新重新校验完整定义，删除要求 `confirm=true`，版本冲突不自动覆盖                                                                                                                                                                                                                                                                                                                      |
| GET / POST           | /api/v1/tags                              | 分页查询/幂等新建标签                                                                    | 已实现；搜索、稳定排序、大小写不敏感名称唯一，颜色为 #RRGGBB                                                                                                                                                                                                                                                                                                                                                    |
| PATCH                | /api/v1/tags/:id                          | 更新标签                                                                                 | 已实现；要求 `If-Match`，名称/颜色变化会递增关联任务版本                                                                                                                                                                                                                                                                                                                                                        |
| DELETE               | /api/v1/tags/:id?confirm=true             | 永久删除标签                                                                             | 已实现；要求 `If-Match` 和明确确认，解除任务关联并递增受影响任务版本                                                                                                                                                                                                                                                                                                                                            |
| POST                 | /api/v1/tasks/:id/start                   | 开始任务                                                                                 | 已实现；todo → in_progress，要求活动 assignee、Task `If-Match` 和可选幂等键                                                                                                                                                                                                                                                                                                                                     |
| POST                 | /api/v1/tasks/:id/block                   | 带原因标记阻塞                                                                           | 已实现；保存来源状态/原因/时间，要求 Task `If-Match` 和可选幂等键                                                                                                                                                                                                                                                                                                                                               |
| POST                 | /api/v1/tasks/:id/unblock                 | 解除阻塞                                                                                 | 已实现；只恢复服务端 `blocked_from_status`，客户端不得指定任意目标                                                                                                                                                                                                                                                                                                                                              |
| POST                 | /api/v1/tasks/:id/complete                | 完成无需验收的人工任务                                                                   | 已实现；只允许 policy none 的 todo/in_progress，原子结束活动 Assignment                                                                                                                                                                                                                                                                                                                                         |
| POST                 | /api/v1/tasks/:id/cancel                  | 带原因取消                                                                               | 已实现；原子结束活动 Assignment，cancelled 不等同 done                                                                                                                                                                                                                                                                                                                                                          |
| POST                 | /api/v1/tasks/:id/reopen                  | 重新打开已完成/取消任务                                                                  | 已实现；回到 todo、保留事件、不恢复旧 Assignment                                                                                                                                                                                                                                                                                                                                                                |
| GET                  | /api/v1/actors                            | 分页查询 Actor                                                                           | 已实现；默认 50/最大 100，支持 type/status 筛选与 type/display_name/status/created_at/updated_at 白名单稳定排序；默认 owner→person→system→agent                                                                                                                                                                                                                                                                 |
| POST                 | /api/v1/actors                            | 新建 person                                                                              | 已实现；服务端固定非内置、version=1，接受 display_name/notes/metadata 和可选 active/inactive；可携带 `Idempotency-Key`，返回首次 `201` 快照、`ETag` 和重放标记；不允许创建 owner/system/agent                                                                                                                                                                                                                   |
| GET                  | /api/v1/actors/:id                        | 查看 Actor                                                                               | 已实现；返回 metadata JSON object、版本和 `ETag`                                                                                                                                                                                                                                                                                                                                                                |
| PATCH                | /api/v1/actors/:id                        | 更新或停用 Actor                                                                         | 已实现；强制 `If-Match`；owner 仅展示名，system/agent 不可编辑，person 可编辑展示名/备注/metadata/状态；活动 Assignment 存在时拒绝停用                                                                                                                                                                                                                                                                          |
| GET / POST           | /api/v1/agent-adapters                    | 查询、注册本地 Adapter                                                                   | v0.2 规划中                                                                                                                                                                                                                                                                                                                                                                                                     |
| POST                 | /api/v1/agent-adapters/:id/check          | 本地健康与能力检查                                                                       | v0.2 规划中                                                                                                                                                                                                                                                                                                                                                                                                     |
| GET / POST           | /api/v1/inbox-items                       | 三视图分页查询；创建立即生效的手工条目                                                   | 已实现；GET 支持 `view/q/priority/risk/page/page_size`，risk 为 tracking/blocked/waiting_review；返回全局待处理 `unread_total` 与时间快照；POST 强制 manual kind/source/policy                                                                                                                                                                                                                                  |
| GET                  | /api/v1/stats/inbox                       | 读取当前收件箱运营计数                                                                   | 已实现；从未归档且未未来 snooze 的 Inbox 与活动必需 Task 实时派生 pending/unread/tracking/blocked/waiting_review 和 `server_now`，不保存缓存事实                                                                                                                                                                                                                                                                |
| GET / PATCH          | /api/v1/inbox-items/:id                   | 查看详情；编辑标题/摘要/优先级/截止时间                                                  | 已实现；返回可用动作和 `ETag`；PATCH 强制 `If-Match`，终态拒绝编辑，有效变化写 triaged/event                                                                                                                                                                                                                                                                                                                    |
| POST                 | /api/v1/inbox-items/:id/read              | 标记单条已读                                                                             | 已实现；强制 `If-Match`、可选幂等键；只写 read，不改变 triaged、snooze 或主状态，终态未读亦可直接执行                                                                                                                                                                                                                                                                                                           |
| POST                 | /api/v1/inbox-items/read-all              | 以 `through_created_at` 时间截止批量标记已读                                             | 已实现；只处理 created_at/updated_at 均不晚于 cutoff 且按 cutoff 仍在待处理可见范围的未读；排除截止后发生任何更新的项；不受当前筛选缩小；可选幂等键，逐项 Event 与事实同事务                                                                                                                                                                                                                                    |
| POST                 | /api/v1/inbox-items/:id/snooze            | 设置稍后时间                                                                             | 已实现；时间必须晚于 `server_now`，强制 `If-Match`、可选幂等键；不改变 read 或主状态                                                                                                                                                                                                                                                                                                                            |
| POST                 | /api/v1/inbox-items/:id/unsnooze          | 清除 `snoozed_until` 并恢复待处理可见性                                                  | 已实现；强制 `If-Match`、可选幂等键；前端列表每 15 秒也会按服务端时钟重新显示自然到期项                                                                                                                                                                                                                                                                                                                         |
| GET                  | /api/v1/inbox-items/:id/tasks             | 查询活动关系、分页历史与实时 Task 进度                                                   | 已实现；`page/page_size` 只分页 history，active 全量返回且最多 100；返回 `{data:{active,history},meta:{page,page_size,total,inbox_item_version,progress}}` 与 Inbox `ETag`                                                                                                                                                                                                                                      |
| POST                 | /api/v1/inbox-items/:id/tasks/:task_id    | 关联已有 Task                                                                            | 已实现；body `{is_required:boolean}`，关系类型固定 linked；要求 Inbox `If-Match`、可选幂等键；第一条活动关系使 open→tracking，与 `task_linked` 事件同事务                                                                                                                                                                                                                                                       |
| PATCH                | /api/v1/inbox-items/:id/tasks/:task_id    | 修改活动关系的必需标记                                                                   | 已实现；body `{is_required:boolean}`，要求 Inbox `If-Match`、可选幂等键；只递增 Inbox version，不修改 Task version，并写 `task_requirement_changed`                                                                                                                                                                                                                                                             |
| DELETE               | /api/v1/inbox-items/:id/tasks/:task_id    | 带原因软解除活动关系                                                                     | 已实现；body `{reason:string}`，trim 后 1–1,000 字符；要求 Inbox `If-Match`、可选幂等键；最后一条活动关系使 tracking→open，写 `task_unlinked`；重新关联创建新历史行                                                                                                                                                                                                                                             |
| POST                 | /api/v1/inbox-items/:id/split             | 原子拆分任务、建立关系并分派                                                             | 已实现；要求 Inbox `If-Match`、可选幂等键；body 含 resolution_policy 与 1–20 个有序 Task 草稿，原子创建父子 Task、标签、Assignment、manual reviewer、created 关系和事件，失败整体回滚                                                                                                                                                                                                                           |
| POST                 | /api/v1/inbox-items/:id/resolve           | 带原因解决                                                                               | 已实现；manual 正常解决；all_required_tasks_done 要求至少一个活动必需 Task 且全部 done；原因 1–2,000 字符，强制 `If-Match`、可选幂等键，清除 snooze 但不隐式 read                                                                                                                                                                                                                                               |
| POST                 | /api/v1/inbox-items/:id/force-resolve     | 二次确认并带原因强制关闭异常工作项                                                       | 已实现；仅 `all_required_tasks_done` 策略，要求 Inbox `If-Match`、可选幂等键及 `{confirm:true,reason}`，写 owner/forced 终态与 `force_resolved` 不可变事件                                                                                                                                                                                                                                                      |
| POST                 | /api/v1/inbox-items/:id/dismiss           | 带原因忽略                                                                               | 已实现；原因 1–2,000 字符，强制 `If-Match`、可选幂等键；清除 snooze 但不隐式 read                                                                                                                                                                                                                                                                                                                               |
| POST                 | /api/v1/inbox-items/:id/reopen            | 重新打开                                                                                 | 已实现；回到 open、清除终态与 snooze、保留 read/triaged；强制 `If-Match`、可选幂等键                                                                                                                                                                                                                                                                                                                            |
| GET / POST           | /api/v1/reminders                         | 查询、创建一次性本地提醒                                                                 | 已实现；列表支持分页、搜索、状态和白名单排序并返回 `server_now`；创建只接受未来时间/manual 来源，返回 `ETag`，支持 `Idempotency-Key` 首次响应快照                                                                                                                                                                                                                                                               |
| GET / PATCH / DELETE | /api/v1/reminders/:id                     | 查看、调整或取消提醒                                                                     | 已实现；详情返回 `available_actions`/`ETag`；PATCH 仅允许 scheduled 且强制 `If-Match`；DELETE body `{reason}` 软取消、强制 `If-Match` 并支持幂等，fired/cancelled 终态不可修改或删除                                                                                                                                                                                                                            |
| GET / POST           | /api/v1/tasks/:id/assignments             | 查询当前角色/分页结束历史，或创建活动分派                                                | 已实现；GET 默认 50/最大 100，返回 Task `ETag`/`meta.task_version`，role 只过滤结束历史；POST 强制 Task `If-Match`，可选 `Idempotency-Key`；assignee 仅 active owner/person，reviewer 仅 active owner                                                                                                                                                                                                           |
| POST                 | /api/v1/tasks/:id/reassign                | 原子结束旧分派并创建新分派                                                               | 已实现；要求原因、Task `If-Match` 和可选幂等键，成功递增 Task 版本并写 `assignment_reassigned` 前后快照                                                                                                                                                                                                                                                                                                         |
| POST                 | /api/v1/assignments/:id/end               | 结束当前分派                                                                             | 已实现；要求原因、所属 Task `If-Match` 和可选幂等键，不改变 Task 状态；Assignment 没有 DELETE 路由                                                                                                                                                                                                                                                                                                              |
| POST                 | /api/v1/tasks/:id/submit-output           | 提交产出并进入验收                                                                       | 已实现；Task `If-Match` + 可选稳定幂等键；严格 JSON 或单 manifest multipart；用户入口固定创建 `origin=manual`，成功返回 Task/Submission/Artifact/Event 与新 `ETag`；每个 `requires_followup=true` Artifact 同事务投影一个稳定去重 Inbox Item                                                                                                                                                                    |
| POST                 | /api/v1/tasks/:id/review                  | 接受结果或要求返工                                                                       | 已实现；active builtin owner reviewer 对 current pending manual/child_rollup Submission 执行 accept/request_changes，返工原因必填；child_rollup 仍须人工接受且 changes_requested 后系统不覆盖；返回 Task/Submission/Event                                                                                                                                                                                       |
| GET                  | /api/v1/tasks/:id/submissions             | 分页查询 Submission 历史                                                                 | 已实现；sequence 倒序，默认 50/最大 100，返回 `origin`、Task `ETag`、`meta.task_version`、Actor 与 Artifact 摘要                                                                                                                                                                                                                                                                                                |
| GET                  | /api/v1/tasks/:id/artifacts               | 分页查询任务产出                                                                         | 已实现；支持 `submission_id`、`include_deleted`，摘要含父批次派生的必填 `submission_status`，不暴露 payload 或内部路径，返回 Task `ETag`/版本                                                                                                                                                                                                                                                                   |
| GET                  | /api/v1/artifacts/:id                     | 获取产出元数据与按需 payload                                                             | 已实现；必填 `submission_status`；text/link/structured 正文只在详情返回；已软删记录仍返回元数据但 payload 全为 null                                                                                                                                                                                                                                                                                             |
| GET                  | /api/v1/artifacts/:id/content             | 通过鉴权读取受控目录中的本地文件                                                         | 已实现；重新校验 size/SHA-256，attachment + nosniff + no-store；已删/缺失拒绝，篡改返回冲突                                                                                                                                                                                                                                                                                                                     |
| DELETE               | /api/v1/artifacts/:id?confirm=true        | owner 确认后软删除产出并写审计                                                           | 已实现；Task `If-Match`、稳定幂等键与 1–1,000 字符原因；pending 批次禁止，missing object 仍可删；若产出是活动 Inbox 来源则拒绝，来源项归档后同事务标记来源删除并保留快照                                                                                                                                                                                                                                        |
| GET / POST           | /api/v1/tasks/:id/agent-runs              | 查询、启动本地 Agent Run                                                                 | v0.2 规划中                                                                                                                                                                                                                                                                                                                                                                                                     |
| GET                  | /api/v1/agent-runs/:id                    | 查看本地执行详情                                                                         | v0.2 规划中                                                                                                                                                                                                                                                                                                                                                                                                     |
| POST                 | /api/v1/agent-runs/:id/cancel             | 取消执行                                                                                 | v0.2 规划中                                                                                                                                                                                                                                                                                                                                                                                                     |
| POST                 | /api/v1/agent-runs/:id/retry              | 创建新重试记录                                                                           | v0.2 规划中                                                                                                                                                                                                                                                                                                                                                                                                     |
| GET                  | /api/v1/inbox-items/:id/events            | 收件箱时间线                                                                             | 已实现；默认 50/最大 100，返回 Inbox `ETag`/版本、owner Actor、request、前后快照和原因；事件追加式且不可修改/删除                                                                                                                                                                                                                                                                                               |
| GET                  | /api/v1/tasks/:id/events                  | 任务时间线                                                                               | 已实现；默认 50/最大 100，返回 Task `ETag`/版本及 actor、assignment、request、前后快照、原因和 command_seq                                                                                                                                                                                                                                                                                                      |
| GET                  | /api/v1/projects                          | 分页查询，支持名称/描述搜索、状态/客户筛选、经布尔校验的 `include_archived` 和白名单排序 | 已实现；`page` 默认 1，`page_size` 默认 50/最大 100，默认隐藏归档项目；所有排序追加 `projects.id ASC`，Count 与 Scan 共用同一 SQLite 只读事务；Client 详情用 `include_archived=true` 读取完整关联历史，ProjectSelect 固定每页 20 条                                                                                                                                                                             |
| POST                 | /api/v1/projects                          | 创建 `planning` 项目                                                                     | 已实现；`Idempotency-Key` 保存规范化请求摘要和首次响应快照                                                                                                                                                                                                                                                                                                                                                      |
| GET                  | /api/v1/projects/:id                      | 获取项目、任务派生汇总、发票数和可用生命周期转换                                         | 已实现；返回 `ETag`                                                                                                                                                                                                                                                                                                                                                                                             |
| GET                  | /api/v1/projects/:id/events               | 分页获取项目写命令活动时间线                                                             | 已实现；默认 20/最大 100，按时间/命令/ID 稳定倒序，返回 owner、request、前后快照、Project `ETag` 和 `meta.project_version`                                                                                                                                                                                                                                                                                      |
| GET                  | /api/v1/projects/:id/artifacts            | 分页聚合所属 Task Artifact                                                               | 已实现；默认 20/最大 100，按创建时间/ID 稳定倒序，可显式包含删除历史；返回 Artifact 摘要、来源 Task 标题/当前状态、Submission 序号、Project `ETag` 和 `meta.project_version`，不复制正文或验收事实                                                                                                                                                                                                              |
| PATCH                | /api/v1/projects/:id                      | 更新非生命周期项目资料                                                                   | 已实现；缺少 `If-Match` 返回 428，旧版本返回 409，归档项目返回 `409 PROJECT_ARCHIVED`                                                                                                                                                                                                                                                                                                                           |
| POST                 | /api/v1/projects/:id/transitions          | 执行 start/pause/resume/complete/reopen/archive/restore                                  | 已实现；缺少 `If-Match` 返回 428，旧版本返回 409，未完成任务的 complete 需要确认；complete/reopen 在事件时有关联 Client 时同事务追加唯一只读客户动态，任一步失败整体回滚                                                                                                                                                                                                                                        |
| DELETE               | /api/v1/projects/:id?confirm=true         | 永久删除已归档项目并解除任务/发票关联                                                    | 已实现；缺少 `If-Match` 返回 428，要求前端二次确认，并返回解除任务/发票数量                                                                                                                                                                                                                                                                                                                                     |
| GET                  | /api/v1/clients                           | 分页查询客户                                                                             | 已实现；默认 50/最大 100，支持 `q/status/sort`，稳定追加 `id ASC`，返回实时 `project_count` 与 nullable `latest_activity_at`                                                                                                                                                                                                                                                                                    |
| POST                 | /api/v1/clients                           | 新建客户                                                                                 | 已实现；默认 active，可选 `Idempotency-Key` 保存规范请求与首次 `201` 快照，返回 `ETag`                                                                                                                                                                                                                                                                                                                          |
| GET                  | /api/v1/clients/:id                       | 获取客户基础详情                                                                         | 已实现；返回 nullable 可选字段、`project_count`、`latest_activity_at`、version 和 `ETag`                                                                                                                                                                                                                                                                                                                        |
| PATCH                | /api/v1/clients/:id                       | 部分更新客户资料或状态                                                                   | 已实现；强制 `If-Match`，可选字段支持显式 null，成功递增聚合版本                                                                                                                                                                                                                                                                                                                                                |
| DELETE               | /api/v1/clients/:id?confirm=true          | 永久删除 inactive 客户                                                                   | 已实现；强制 `If-Match`；Invoice 强引用返回 409 且无副作用，Project 外键置空并返回 `detached_projects`                                                                                                                                                                                                                                                                                                          |
| GET / POST           | /api/v1/clients/:id/activities            | 查询活动、新增本地人工活动                                                               | 已实现；列表默认 20/最大 100，可按 kind 筛选并显式包含删除历史，返回 Client `ETag`/版本，并包含内部投影的 Project 生命周期 system reference；POST 仍只接受 note/meeting，可用 `Idempotency-Key`                                                                                                                                                                                                                 |
| GET / PATCH / DELETE | /api/v1/client-activities/:id             | 获取、编辑或带原因软删除人工活动；系统引用/删除历史只读                                  | 已实现；GET 包含审计摘要，PATCH/DELETE 强制活动 `If-Match`，DELETE 另要求 `confirm=true` 与原因；活动变化在同一事务递增 Client 聚合版本                                                                                                                                                                                                                                                                         |
| GET / POST           | /api/v1/clients/:id/attachments           | 查询、导入受控附件                                                                       | 已实现；GET 稳定分页并可显式包含删除历史，返回 Client `ETag`/版本；POST 严格 metadata-first multipart、单文件 50 MiB/请求 100 MiB，可选同 Client Activity 与 `Idempotency-Key`，强制 Client `If-Match`                                                                                                                                                                                                          |
| GET                  | /api/v1/client-attachments/:id            | 获取附件元数据与审计摘要                                                                 | 已实现；删除历史不返回本地路径或正文，返回附件 `ETag`                                                                                                                                                                                                                                                                                                                                                           |
| GET                  | /api/v1/client-attachments/:id/content    | 完整性校验后下载附件正文                                                                 | 已实现；只允许未删除项，逐字节校验 size/SHA-256，缺失或损坏拒绝返回正文                                                                                                                                                                                                                                                                                                                                         |
| DELETE               | /api/v1/client-attachments/:id            | 带原因软删除附件并审计                                                                   | 已实现；要求 `confirm=true`、Client `If-Match` 和 1–1,000 字符原因，可选 `Idempotency-Key`；trash/墓碑/事务补偿后递增 Client 聚合版本                                                                                                                                                                                                                                                                           |
| GET / POST           | /api/v1/clients/:id/actor-links           | 查询或显式建立本地联系人关系                                                             | 已实现；GET 稳定分页并可包含解除历史；POST 要求 Client `If-Match`，已有 active person 与原子 `create_person` 二选一，可选 `Idempotency-Key`，每个 Client 只允许一个 active contact                                                                                                                                                                                                                              |
| DELETE               | /api/v1/client-actor-links/:id            | 带原因解除本地联系人并保留历史                                                           | 已实现；要求 `confirm=true`、所属 Client `If-Match`、1–1,000 字符原因及可选 `Idempotency-Key`；解除事实不可修改或硬删，person 保留                                                                                                                                                                                                                                                                              |
| GET / POST           | /api/v1/client-followups                  | 查询、新建客户回访                                                                       | v0.4 规划中                                                                                                                                                                                                                                                                                                                                                                                                     |
| GET / PATCH / DELETE | /api/v1/client-followups/:id              | 获取、更新、删除客户回访                                                                 | v0.4 规划中                                                                                                                                                                                                                                                                                                                                                                                                     |
| GET / POST           | /api/v1/financial-entries                 | 查询、新建收入/支出记录                                                                  | v0.4 规划中                                                                                                                                                                                                                                                                                                                                                                                                     |
| GET / PATCH / DELETE | /api/v1/financial-entries/:id             | 获取、更新、删除收入/支出记录                                                            | v0.4 规划中                                                                                                                                                                                                                                                                                                                                                                                                     |
| GET / POST           | /api/v1/invoices                          | 查询、新建发票                                                                           | v0.4 规划中                                                                                                                                                                                                                                                                                                                                                                                                     |
| GET / PATCH / DELETE | /api/v1/invoices/:id                      | 获取、更新、删除发票                                                                     | v0.4 规划中                                                                                                                                                                                                                                                                                                                                                                                                     |
| POST                 | /api/v1/invoices/:id/generate-pdf         | 生成发票 PDF                                                                             | v0.4 规划中                                                                                                                                                                                                                                                                                                                                                                                                     |
| GET                  | /api/v1/focus-sessions                    | 查询终态 Session 历史，可选 Task/Project、状态与分页                                     | 已实现；默认 terminal，按 `ended_at DESC, id ASC`；可选 `project_id` 严格 canonical UUID，非法/非 canonical 返回 `400 INVALID_PROJECT_ID`、不存在返回 `404 PROJECT_NOT_FOUND`、归档可读；按 Task 查询时当前 Project 归属，改绑会重分类，无 Task/Task 已删/无项目不进入项目过滤结果                                                                                                                              |
| GET                  | /api/v1/focus-sessions/active             | 查询活动/暂停/待恢复会话与绝对时间快照                                                   | 已实现；active 查询同时刷新 heartbeat，不递增 version；无开放 Session 返回 null 快照                                                                                                                                                                                                                                                                                                                            |
| POST                 | /api/v1/focus-sessions                    | 开始专注，可绑定 Task                                                                    | 已实现；`planned_seconds` 300–7200，Task 可空且不得为 cancelled，全库只允许一个开放 Session；支持 `Idempotency-Key`                                                                                                                                                                                                                                                                                             |
| POST                 | /api/v1/focus-sessions/:id/pause          | 暂停并结算当前 interval                                                                  | 已实现；仅 active，强制 `If-Match`                                                                                                                                                                                                                                                                                                                                                                              |
| POST                 | /api/v1/focus-sessions/:id/resume         | 按服务端绝对时间恢复并开启新 interval                                                    | 已实现；仅 paused，强制 `If-Match`                                                                                                                                                                                                                                                                                                                                                                              |
| POST                 | /api/v1/focus-sessions/:id/recover        | 处理 recovery_pending：计入/排除间隔继续或结束为 interrupted                             | 已实现；动作 `include_gap_resume / exclude_gap_resume / interrupt`，强制 `If-Match`                                                                                                                                                                                                                                                                                                                             |
| POST                 | /api/v1/focus-sessions/:id/stop           | 完成并幂等累计绑定 Task 工时                                                             | 已实现；active/paused，强制 `If-Match`，支持 `Idempotency-Key`；匹配的 completed 终态可稳定重放且只记账一次                                                                                                                                                                                                                                                                                                     |
| POST                 | /api/v1/focus-sessions/:id/cancel         | 取消且不累计工时                                                                         | 已实现；active/paused，强制 `If-Match`，支持 `Idempotency-Key`；匹配的 cancelled 终态可稳定重放                                                                                                                                                                                                                                                                                                                 |
| GET                  | /api/v1/stats/focus                       | 查询日期范围 Focus 报告，可选 Project                                                    | 已实现；显式 IANA 时区、1–93 个当地自然日、跨午夜/DST/Streak/完整零值序列；只统计 completed Session 的闭合正时长 interval；`project_id` 使用与历史相同的 canonical UUID/400/404/归档可读/当前 Task Project 归属语义                                                                                                                                                                                             |
| GET / PATCH          | /api/v1/settings                          | 查询、更新版本化非敏感设置                                                               | 已实现；前端启动、保存与旧值兼容迁移均已接入                                                                                                                                                                                                                                                                                                                                                                    |
| GET                  | /api/v1/backups                           | 列出本机已发布备份包和最近完整校验结果                                                   | 已实现；只接受 canonical UUID 包，损坏 manifest 以 invalid 项返回，不暴露本机绝对路径                                                                                                                                                                                                                                                                                                                           |
| POST                 | /api/v1/backups                           | 创建并完整校验 SQLite 与全部 active 受控文件的一致性备份                                 | 已实现；维护写锁+备份互斥锁内先重放幂等结果，否则按 SQLite/全部 active 文件/marker/manifest + 20%（最低 64 MiB）余量仅探测 backup root。小于需求返回 `507 BACKUP_SPACE_INSUFFICIENT`，无法确认返回 `503 BACKUP_CAPACITY_UNAVAILABLE`，等于需求允许继续；拒绝不泄露路径/盘符/精确容量/底层错误，不建 staging/包、不改业务事实、不投影 generic incident。通过后才完整校验并同卷原子发布                           |
| POST                 | /api/v1/backups/:id/verify                | 对指定备份包重新执行完整校验                                                             | 已实现；校验 manifest/预期文件全集、size/SHA-256、marker、SQLite quick/foreign-key/schema/identity 和 Artifact 元数据，仅成功时刷新 `verified_at`                                                                                                                                                                                                                                                               |
| POST                 | /api/v1/backups/:id/drill                 | 在隔离临时数据根执行恢复演练                                                             | 已实现；再次完整校验后复制数据库/marker/objects，用当前迁移器打开副本并声明临时 Artifact store，最终复验并清理临时数据；不修改源备份或当前数据                                                                                                                                                                                                                                                                  |
| POST                 | /api/v1/backups/:id/restore               | 明确确认并安排下一次 Sidecar 启动前恢复                                                  | 已实现；`confirm=true` 后重验目标、创建当前状态回滚包并发布 pending，冻结普通写入；同目标可重放、异目标冲突。启动时验证目标/回滚、交换 SQLite/WAL/SHM 与完整 objects，失败回滚隔离、成功推进 applied 提交点                                                                                                                                                                                                     |
| DELETE               | /api/v1/backups/:id                       | 永久删除指定内部备份包                                                                   | 已实现；要求查询参数 `confirm=true` 和 canonical UUID；有效/损坏包均先校验无 symlink/reparse/非普通文件，原子移入 `.deleting-<id>` 后清理同步，中断可续删；pending 恢复期间返回恢复重启门禁                                                                                                                                                                                                                     |
| GET                  | /api/v1/backups/restore-diagnostics       | 读取脱敏启动恢复结果与残留诊断                                                           | 已实现；汇总 pending、本次 applied、applied 清理残留、failed 隔离与 invalid 记录，只返回规范 ID/时间/状态/计数；不返回路径或底层错误，也不自动清理                                                                                                                                                                                                                                                              |
| GET                  | /api/v1/exports/business-data             | 下载版本化业务 JSON 一致视图                                                             | 已实现；单 SQLite 读事务、显式业务表白名单和稳定表/列/行结构；受控文件只含元数据和 active 文件摘要，不含正文；排除令牌、绝对路径、identity、幂等/迁移/墓碑/派生表，任一白名单读取失败整体拒绝                                                                                                                                                                                                                   |
| GET                  | /api/v1/exports/business-package          | 下载含活动受控文件的业务 ZIP                                                             | 已实现；维护写锁内完整生成 `manifest.json`、`business-data.json` 与 `files/<relative_path>`，逐文件校验 size/SHA-256，失败不返回部分 ZIP；排除 SQLite、identity、marker、令牌、绝对路径与运行维护表                                                                                                                                                                                                             |
| POST                 | /api/v1/imports/business-data/preview     | 预检版本化业务 JSON                                                                      | 已实现；16 MiB 上限，strict format/API/schema/表列行/标量检查，拒绝受控文件、活动 Focus 与不兼容包，返回行数和目标是否为空                                                                                                                                                                                                                                                                                      |
| POST                 | /api/v1/imports/business-data             | 原子导入版本化业务 JSON                                                                  | 已实现；要求 `X-Import-Confirmation: replace-empty-workspace`、空目标和已配置备份；应用前创建已校验回滚包，维护写锁内原子替换并复验 trigger/外键/quick-check                                                                                                                                                                                                                                                    |
| POST                 | /api/v1/imports/business-package/preview  | 预检含活动受控文件的业务 ZIP                                                             | 已实现；2 GiB/10,000 文件上限，strict manifest/业务 JSON/路径/文件全集/size/SHA-256/元数据检查，返回行数、文件总量和目标门禁                                                                                                                                                                                                                                                                                    |
| POST                 | /api/v1/imports/business-package          | 原子导入含活动受控文件的业务 ZIP                                                         | 已实现；要求 `X-Import-Confirmation: replace-empty-workspace-with-controlled-files`、空目标和已配置备份；无覆盖发布受控文件，DB 提交前复验磁盘正文，失败补偿本次文件                                                                                                                                                                                                                                            |
| GET                  | /api/v1/stats/today?date=&timezone=       | 今日任务和专注统计                                                                       | 已实现；IANA 本地日、DST 安全 interval overlap、completed-only；兼容 `timezone_offset_minutes`，未指定按 UTC                                                                                                                                                                                                                                                                                                    |
| GET                  | /api/v1/stats/income                      | 收入/支出与净现金流统计                                                                  | v0.4 规划中                                                                                                                                                                                                                                                                                                                                                                                                     |
| GET                  | /api/v1/search?q=&types=&page=&page_size= | 统一多实体全局搜索                                                                       | 已实现；查询 Task/Project/Client/活动 Inbox，支持类型白名单、1–100 条分页、200 字符输入、LIKE 转义和确定性相关性排序；返回 strict 资源身份、摘要、命中字段、状态、时间及精确详情路由，过滤归档 Project 与终态 Inbox                                                                                                                                                                                             |
| GET / POST           | /api/v1/knowledge/documents               | 查询、导入知识库文档                                                                     | 版本待定；需 ADR 后确认                                                                                                                                                                                                                                                                                                                                                                                         |
| POST                 | /api/v1/knowledge/search                  | 本地知识检索并返回来源                                                                   | 版本待定；需 ADR 后确认                                                                                                                                                                                                                                                                                                                                                                                         |
| POST                 | /api/v1/ai/chat                           | 显式调用 AI 助手                                                                         | 版本待定；流式协议需 ADR 后确认                                                                                                                                                                                                                                                                                                                                                                                 |

**本地 API 安全与响应约定**：

- 基础地址由 Tauri 在运行时注入前端，不写死生产端口，也不保存到持久配置
- 使用 `Authorization: Bearer <session-token>`；令牌只存在于当前应用进程生命周期
- Sidecar 只接受明确允许的 WebView Origin，并拒绝缺少或不匹配 Origin 的浏览器请求
- 错误响应统一包含 `code`、`message`、`request_id`；日志通过 `request_id` 关联且不得包含敏感正文
- 列表接口统一支持分页、排序和筛选；写操作使用事务，可能重试的创建操作支持幂等键
- API 时间戳使用 RFC 3339 UTC，金额字段使用最小货币单位整数
- 创建、关联、解除、拆分、分派、运行、验收、返工、解决和忽略等可重试命令按各阶段支持 `Idempotency-Key`；幂等记录必须包含请求摘要和可重放响应，同一 key 携带不同请求体返回 409。当前 Assignment、Task 生命周期、D2 submit/review/Artifact delete、Client Attachment upload/delete、Focus create/stop/cancel、手工 Inbox 创建/单条命令/read-all/Task 关系写入，以及 Reminder create/cancel 已实现；Client Attachment、Focus、Inbox 与 Reminder 命令在版本检查前重放同键同请求，不重复写文件、记账、改写事实或写事件
- 状态变化携带 `expected_version` 或 `If-Match`；并发版本不一致时拒绝旧写入并返回 409。Focus Session 命令缺少 `If-Match` 返回 428，旧版本返回 409；heartbeat 不改变 version。匹配当前终态的 stop-on-completed/cancel-on-cancelled 可稳定返回，反向终态命令仍返回状态冲突
- 输出严格 JSON body、multipart manifest 和单个 structured object 各限 1 MiB，单文件限 50 MiB、完整 multipart 限 100 MiB；Sidecar HTTP read/write timeout 为 180 秒，前端上传/下载采用 120 秒端到端超时
- schema v12 已为非空 `source_event_key` 建立部分唯一约束；schema v14 Reminder 使用 `reminder:<id>:due`，schema v23 follow-up Artifact 使用 `task-artifact:<artifact-id>:followup`，schema v24 Task 阻塞使用 `task:<task-id>:blocked:<block-version>`，schema v25 Task 临期使用 `task:<task-id>:due:<due-at>`，schema v26 系统维护使用 `system:<component>:<operation>:<incident-id>`，同一 `backup:create/verify/drill/restore`、`database:startup/migration` 或 `sidecar:startup` 仅允许一个活动 incident。Artifact/阻塞分别与 Task 提交/block Event 同事务，临期来源与 system Event 同扫描事务，备份操作失败与调用失败响应解耦且尽力投影；启动前失败持久化稳定 incident ID，补偿投影跨模糊清理重放不重复；后续来源仍必须使用稳定键
- Actor 详情、创建和更新返回 `ETag`；Actor PATCH 缺少 `If-Match` 返回 428，格式错误返回 400，旧版本返回 409。person 创建幂等重放不得重复写 `actor_created`；停用被活动 Assignment 拒绝时不得递增版本或写事件
- Agent 不使用 WebView 会话令牌；Sidecar 为单次 Run 发放短时、能力受限且不可复用的本地令牌
- Agent Runtime 使用独立路由组和鉴权中间件，或直接使用受控进程管道；具体传输、Origin 处理、撤销和泄漏防护必须由 v0.2 ADR 确定
- 外部 URL 产出只作为不可自动抓取的引用；任意本地业务文件必须先复制到共享受控文件目录。Task Artifact 的读取/删除经过 Sidecar 鉴权并写 Workflow Event；Client Attachment 的读取/删除经过相同鉴权与完整性/墓碑边界，并以 Client 聚合版本和附件审计事实追踪
- schema v13 的**关联 Task**删除互锁仍独立存在；schema v23–v25 另为 `source_entity_type=task_artifact/task/task_due` 实现多态来源删除协调：活动来源项阻止 Artifact/Task 删除，归档后删除写 `source_deleted_at`、保留最小快照并显示来源已删除。其他未来来源仍需逐项实现同类契约

### D. 架构决策记录

**ADR-001：桌面运行时采用 Go Sidecar，移除 Docker 依赖**

| 项目        | 内容                                                                                                          |
| ----------- | ------------------------------------------------------------------------------------------------------------- |
| 状态        | 已接受                                                                                                        |
| 日期        | 2026-08-25                                                                                                    |
| 决策        | React 静态资源和 Go Sidecar 随 Tauri 安装包交付；SQLite 使用 `appDataDir`；本地开发直接运行 Vite、Go 和 Tauri |
| 原因        | 降低安装门槛、启动耗时和资源占用，避免 Docker Desktop、镜像拉取、固定端口与容器生命周期带来的复杂度           |
| 影响        | 必须维护多平台 Sidecar 构建矩阵、进程生命周期、动态端口握手、更新兼容和 SQLite 迁移/备份机制                  |
| Docker 边界 | 不属于 MVP 桌面运行时或本地开发依赖；未来仅可作为可选自托管、集成测试或服务端部署方案单独评审                 |

**ADR-002：采用单机 Actor 与收件箱工作编排模型**

| 项目     | 内容                                                                                                                                                                      |
| -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 状态     | 已接受                                                                                                                                                                    |
| 日期     | 2026-08-27                                                                                                                                                                |
| 决策     | 使用 owner/person/agent/system 四类本地 Actor；收件箱负责事件受理与跟进，Task 是唯一可执行工单，Assignment 保存责任历史，Agent Run 保存单次本地执行，owner 负责高风险验收 |
| 原因     | 在不引入线上服务的前提下，统一表达本人、外部责任人、本地 Agent 和系统规则的责任关系，并支持项目产出、发票待办和本地提醒持续跟进到完成                                     |
| 影响     | person 仅作本地责任记录；agent 只有在本地 Adapter 健康且能力明确时可执行；收件箱进度必须从任务派生；所有拆分、分派、产出、验收和返工保留审计                              |
| 非目标   | 多人登录、远程任务领取、云同步、实时消息、远程 Agent、远程模型、自动发送邮件/发票/客户消息                                                                                |
| 未来扩展 | 若引入线上服务，必须重新评审身份、权限、同步冲突、通知、密钥、数据外发和撤销机制，不沿用 person 的本地责任语义冒充在线协作                                                |

### E. 版本变更记录

| PRD 版本 | 日期       | 变更                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| -------- | ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| v1.2     | 2026-08-25 | 确立 Tauri 2 + React + Go Sidecar + SQLite 的本地优先架构和 v0.1 MVP 范围，移除桌面运行与本地开发的 Docker 依赖                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| v1.3     | 2026-08-26 | 增加 app v0.1.0 / API v1 / schema v2 实施基线、统一开发流程、逐任务实现方法、验证矩阵与已知限制；修正原型路径、当前技术栈、专注设置、API 状态和模块完成度                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| v1.4     | 2026-08-26 | 将 AI Provider 接入与本地知识库加入版本待定的后续工作包，补充功能边界、建议实施流程、隐私安全闸门和验收要求；v0.1 继续保持无 AI/LLM 依赖                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| v1.5     | 2026-08-26 | 将 AI 助手与知识库拆为独立工作包；新增客户回访与收入/支出需求；把客户回访、收入/支出和发票业务归入 v0.4 后续业务版，进一步收紧 v0.1 范围                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| v1.6     | 2026-08-27 | 将收件箱升级为本地工作受理与编排中心；接受单机 Actor 模型和无线上服务边界；增加任务拆分、Assignment、本地 Agent Run、产出、验收/返工、提醒、审计的数据/API 规划，并补齐各模块实施顺序、验收标准和当前文档基线；PRD 移入 `docs/`，新增整体功能架构与模块文档，历史 HTML 原型从仓库移除                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| v1.7     | 2026-08-27 | 将当前基线推进到 SQLite schema v6；交付任务类型、父子关系、完成标准、Task/Tag 版本、标签管理、服务端分页筛选、层级列表、事务化批量安全操作、计划组按钮排序与 `ETag`/`If-Match` 冲突处理；保留扩展生命周期、Actor/验收、今日页拖拽和专注工时为后续纵切                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| v1.8     | 2026-08-27 | 将当前基线推进到 SQLite schema v7；交付 T-18A Actor/Assignment/Event 数据基础、固定 owner/system、历史任务幂等分派回填和约束保护，以及 T-18B Actor API、创建幂等、`ETag`/`If-Match`、审计事件与设置页 person 管理；Assignment API/UI、受控状态、Artifact/Agent、Inbox 编排和 app_settings 保留为后续纵切                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| v1.9     | 2026-08-27 | 在 schema v7 上交付 T-18C Assignment 查询/创建/改派/结束、Task `If-Match`/version、可选幂等快照、责任事件、done 自动结束/reopen 不恢复、Task DELETE 聚合语义和任务详情负责人/审核人/分页历史 UI；T-18D 受控状态/Artifact/验收、Inbox 消费与 Agent 保留为后续纵切                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| v2.0     | 2026-08-27 | 将基线推进到 schema v8，交付 T-18D D1 六状态、start/block/unblock/complete/cancel/reopen、Task 幂等/并发、完成/取消的 Assignment 原子联动、Task 活动时间线、command_seq 与事件不可变保护；manual policy 入口、Artifact、submit/review/返工明确留给 D2                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| v2.1     | 2026-08-27 | 将基线推进到 schema v9，交付 T-18D D2 manual Submission/Artifact、submit/review/历史/安全下载/确认软删、Actor 归属与迁移回填；受控文件根增加数据库/store 身份绑定、进程级独占锁、不可变 deletion tombstone、耐久同步、哈希恢复、quarantine 与软/硬删除补偿，前端交付冲突草稿保留和完整验收 UI                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| v2.2     | 2026-08-27 | 将基线推进到 schema v10，交付 Client 基础资料 CRUD、分页/搜索/状态/稳定排序、创建幂等、聚合乐观锁、受约束删除、基础详情与 Project 客户选择/改绑/解除/筛选；活动、附件、Actor 显式关联、回访和财务继续延期                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| v2.3     | 2026-08-28 | 将基线推进到 schema v11，交付 Focus Core A+B+C：Session/interval/Task ledger、状态机、绝对时间、心跳/恢复、幂等与 `If-Match`、任务精确工时、IANA interval-overlap Today 统计，以及前端共享 Session 与设置运行态隔离；历史、周报、Streak、高级分析和原生通知/托盘/DND 归入延后的 D                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| v2.4     | 2026-08-28 | 将基线推进到 schema v12，交付 T-11A1/T-11B 手工 Inbox Item、三视图与全局未读、详情编辑、单条/时间截止式全部已读、稍后/恢复、带原因解决/忽略、终态已读、重开和 Inbox Event；Task 关系/拆分/分派/自动解决、Reminder/来源投影、Today/Sidebar 计数与 Agent 继续延期                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| v2.5     | 2026-08-28 | 将基线推进到 schema v13，交付 T-11A2 已有 Task 活动/历史关系、实时进度、关联、required 修改、带原因软解除、open/tracking 与 reopen 联动、关系事件、Task 硬删除活动关系互锁和删除后快照；批量拆分、Assignment、自动解决、Reminder、来源投影与 Agent 继续延期                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| v2.6     | 2026-08-28 | 将基线推进到 schema v14，交付 T-11A3 一次性本地 Reminder、CRUD/分页搜索、ETag/幂等、scheduled 编辑/改期、带原因取消、启动补偿、15 秒有界扫描、稳定事件键与事务化 Reminder Inbox 投影；重复/原生通知、其他来源投影、拆分/分派/自动解决与 Agent 继续延期                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| v2.7     | 2026-08-28 | 将基线推进到 schema v15，交付 T-11C 原子批量 Task 拆分、父子层级、owner/person Assignment、manual reviewer、统一 reconciliation、自动结清/依赖失效重开、危险 force-resolve、幂等/乐观锁和前端拆分面板；非 Reminder 来源投影、Sidebar/Today 计数与 Agent 继续延期                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| v2.8     | 2026-08-28 | 交付 T-11F 收件箱运营计数：从可见活动 Inbox 与必需 Task 实时派生 pending/unread/tracking/blocked/waiting_review，新增统计 API、Inbox 风险筛选/深链、Sidebar 徽标与 Today 风险卡；不增加缓存表，非 Reminder 来源投影、原生通知与 Agent 继续延期                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| v2.9     | 2026-08-28 | 交付 T-06A Today 真实计划日期分组：Task 列表新增 active 与 scheduled/unscheduled 服务端筛选，前端按本地日期分页拉全逾期、今天、本周稍后和未排期活动任务，移除前 3/后 3 与 100 条静默截断；日期切换、Today 持久排序/拖拽与快捷操作继续延期                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| v3.0     | 2026-08-28 | 交付 T-06B Today 日期导航：支持前一天、后一天、回到今天，任务四分组与 Today 统计统一消费所选日期，非当天视图使用“所选日期”语义；Today 持久排序/拖拽、行内快捷操作、项目/客户/财务概览继续延期                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| v3.1     | 2026-08-28 | 交付 T-13A 命令面板 Task 搜索：200 ms 防抖服务端查询完整 Task 集合、200 字符/安全 LIKE 边界、最近更新前 12 项、精确详情打开、加载/错误/重试/空态；移除未交付收入/发票命令。统一多实体搜索、稳定详情路由、焦点恢复和 OS 全局快捷键继续延期                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| v3.2     | 2026-08-28 | 交付 T-13B 命令面板键盘与设置直达：6 个当前设置模块精确打开，combobox/listbox 活动项关联、初始聚焦、Tab 圈闭、背景滚动锁定、关闭后焦点恢复和 IME composition/229 保护；统一多实体搜索、稳定详情路由、真实浏览器/WebView 验收和 OS 全局快捷键继续延期                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| v3.3     | 2026-08-28 | 交付设置“关于与诊断”健康事实基线：严格校验 `/health`，展示真实 Sidecar/app/version/commit/API/schema/SQLite 状态，提供加载、错误/request ID、重试、手动检查、最近成功结果降级与只读页脚；完整诊断和 Sidecar 手动恢复继续延期                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| v3.4     | 2026-08-28 | 交付 T-06C Today 按钮式持久排序：所选精确日期与未排期活动 Task 可上移/下移和恢复默认，完整计划组与版本原子校验，支持跨活动状态移动并保留终态槽位；保存中禁写，失败保序并显示错误；拖拽、跨日期改期与行内快捷操作继续延期                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| v3.5     | 2026-08-28 | 交付 T-06D Today 同组拖拽：精确日期与未排期活动 Task 通过拖动手柄乐观预览顺序，Sidecar 校验完整活动 ID 集合/全部版本并保留终态槽位后原子写入；成功刷新、失败/冲突回滚，按钮保留为键盘替代；跨日期改期与行内快捷操作继续延期                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| v3.6     | 2026-08-28 | 将基线推进到 schema v16，交付设置 v0.1-A 服务端事实层：空的版本化 app_settings、四模块未存储默认值与严格完整 schema、1–4 模块原子 PATCH、expected_version 乐观锁和不含设置值的 Workflow Event；前端 Query、旧 localStorage 迁移与受控头像文件继续延期                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| v3.7     | 2026-08-28 | 交付设置 v0.1-A 前端闭环和 v0.1-B 兼容迁移：启动门禁、strict normalizer、Query committed、按变化模块版本化保存、失败保留 draft/preview，以及旧 opc-focus-settings 仅回填缺失模块、模糊失败重试和新本地头像空值优先；受控头像文件继续延期                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| v3.8     | 2026-08-28 | 交付 T-06E Today 明确改期：四组活动任务均提供行内任意日期/所选日期/后一天/未排期安排，单任务复用原子 batch set_planned_date 与 expected_version；同日期不写入，冲突刷新，网络/超时回读证明后才确认成功；跨分组拖拽及其他快捷操作继续延期                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| v3.9     | 2026-08-28 | 交付 T-06F Today 安全执行快捷操作：todo 行内开始、无需验收的 in_progress 行内完成、活动任务在服务端 Session 与本地循环空闲时一键创建绑定 Focus；任务命令携带 expected_version 并复用幂等键，写入互斥、错误可见，manual review/阻塞/取消不被绕过；跨分组拖拽和行内编辑/删除继续延期                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| v4.0     | 2026-08-28 | 交付 T-06G Today 跨分组拖拽：四个可见组共享拖拽源，具体任务提供真实目标日期与相对位置，空的所选日期/未排期提供组级末尾落点；同日提交完整 reorder，跨日期先以 batch+版本/模糊验证确认改期，再分别保存源/目标完整顺序，部分失败明确区分且刷新事实；行内编辑/删除继续延期                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| v4.1     | 2026-08-28 | 交付 T-06H Today 行内编辑/删除入口：编辑复用共享完整任务详情；删除使用独立不可恢复确认、当前 Task 版本和既有确认删除 API，成功刷新 Task/Project/Today/Inbox，版本冲突刷新事实，活动 Inbox 关系保护显示解除指引；真实浏览器 hover/focus/窄屏验收继续延期                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| v4.2     | 2026-08-28 | 交付 T-07A 任务页精确计划组拖拽：仅单一精确计划日期、手动顺序和无其他筛选时启用；同状态行乐观预览，hook 回读最多 1,000 项完整计划组并校验源/目标日期、状态和版本，重建同状态槽位后原子提交；分页不冒充完整集合，失败清除预览并刷新，上移/下移保留为键盘替代                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| v4.3     | 2026-08-28 | 交付 T-07B 任务页日期范围筛选：计划精确日期与计划范围互斥，计划/截止起止条件进入稳定服务端分页；Web 对倒置区间即时标错并停用查询，API 校验格式与顺序并按日期部分筛选含时间截止值；补齐 due 范围客户端契约、合法计划/截止范围、序列化与非法范围测试；客户筛选和保存视图仍待开发                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| v4.4     | 2026-08-28 | 交付 T-07C 任务客户筛选：不复制客户事实，API 严格校验 client_id 并以 Task→Project→Client EXISTS 条件保持稳定计数/排序/分页；Web 复用真实 Client options，客户与项目等条件显式 AND，纳入分页、条件计数、清除和排序门禁；接口、序列化与页面测试覆盖，保存视图仍待开发                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| v4.5     | 2026-08-28 | 将基线推进到 schema v17，交付 T-07D 任务保存视图：独立空表、20 项上限、名称唯一、JSON/schema/version/大小约束；API 严格规范化完整筛选定义并提供版本化更新/确认删除；Web 支持保存、选择即应用、更新所选和二次确认删除，不保存页码/选择/结果；迁移、API、客户端与交互测试覆盖                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| v4.6     | 2026-08-28 | 交付 T-04B 手动一致性备份第一段：专用 backup root 和维护写锁，`VACUUM INTO` SQLite 快照、active file Artifact 与 marker/manifest 打包，经文件全集、哈希、数据库事实和身份完整校验后同卷原子发布；API 与设置页支持幂等创建、列表和重新校验；恢复、删除、JSON 导出和迁移前自动备份继续独立实现                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| v4.7     | 2026-08-28 | 交付 T-04B 隔离恢复演练：再次完整校验源包后复制到唯一临时数据根，用当前迁移器打开数据库副本并执行最终 quick/foreign-key/schema/identity 检查，声明临时 Artifact store、逐个复验 active file Artifact 后关闭句柄并清理；API/设置页提供演练状态，源包与当前数据不变；原子替换继续独立实现                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| v4.8     | 2026-08-28 | 交付 T-04B 重启前安全恢复：严格确认后重复演练目标并创建当前状态自动回滚包，原子发布私有 pending 后冻结普通 API/后台写入；下一次 Sidecar 启动前验证目标/回滚包、迁移副本、交换 SQLite/WAL/SHM 与完整 objects，失败回滚并隔离，成功以 applied 提交点防重复；设置页提供二次确认和关闭重开指引，自动重启/删除/导出继续独立实现                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| v4.9     | 2026-08-28 | 交付 T-04B 备份确认删除：canonical UUID 包在 `confirm=true` 后递归拒绝 symlink/reparse/非普通文件，先同根原子重命名为 `.deleting-<id>` 并同步，再永久清理；中断后可从隐藏态续删，有效和损坏包均可删除，pending 恢复门禁阻止并发删除；设置页提供独立二次确认和反馈，JSON 导出继续独立实现                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| v5.0     | 2026-08-28 | 交付 T-04B 基础业务 JSON 导出：单 SQLite 读事务按显式白名单生成 format v1 attachment，稳定记录 source、表/列/行和 active file 摘要；Artifact 元数据保留但正文不嵌入，运行令牌、绝对路径、identity、幂等/迁移/墓碑/派生表排除，白名单故障整体失败；设置页提供真实下载和边界说明，含文件包/导入继续独立实现                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| v5.1     | 2026-08-28 | 交付 T-04B 恢复后桌面一键安全重启：设置页只在 pending 已发布后显示入口；`restart_application` 复用精确 Sidecar shutdown/等待/兜底终止协议，真实退出确认后才请求 Tauri 重启，失败取消重启并显示错误；外部开发 Sidecar 明确拒绝并提供手动降级，恢复进度/诊断继续独立实现                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| v5.2     | 2026-08-28 | 将基线推进到 schema v18，交付 Client 本地活动纵切：人工备注/会议的幂等创建、稳定分页、活动版本化编辑、带原因软删除与只读历史；Client 聚合版本及最近动态同步传播，客户详情接入真实时间线和完整状态；附件、来源投影、Actor 关联与 v0.4 回访/财务继续独立实现                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| v5.3     | 2026-08-28 | 将基线推进到 schema v19，交付 Client 受控附件纵切：metadata-first multipart 幂等上传、稳定分页、同客户活动关联、完整性校验下载、带原因软删除与只读历史；共享受控 store、启动协调、Client 硬删除、备份/恢复和业务 JSON 导出统一覆盖 Task Artifact 与 Client Attachment，客户详情接入真实附件交互；来源投影、Actor 关联与 v0.4 回访/财务继续独立实现                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| v5.4     | 2026-08-28 | 将基线推进到 schema v20，交付 Client–person 显式关联：已有 active person 与原子新建二选一、单 active contact、Client 乐观锁和幂等快照、带原因解除与不可变历史、Actor 停用保护、Client 删除边界和业务 JSON 导出；客户详情接入本地责任关联 UI，明确不创建账号/消息/权限；外部来源投影及 v0.4 回访/财务继续独立实现                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| v5.5     | 2026-08-28 | 交付 Focus v0.1-D1：终态 Session 稳定分页与可选状态/任务筛选、显式 IANA 时区 1–93 日本地日周期统计、completed interval overlap、每日与区间 distinct Session、当前/最长 Streak，以及 Focus 页面七日趋势、历史分页和完整反馈状态；schema 保持 v20，原生通知/托盘/DND 与高级分析独立延后                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| v5.6     | 2026-08-28 | 交付 Focus v0.1-D2a：Task 详情按需读取 task-filtered 终态 Session，展示 completed/cancelled/interrupted、实际累计时长、结束时间、稳定分页及独立加载/错误/空状态；复用 D1 API 和事实，不新增表、不复制状态、不把取消/中断计入工时；高级分析和原生桌面反馈归 D2b                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| v5.7     | 2026-08-28 | 交付 Project 追加式活动时间线：创建、资料编辑、七种生命周期转换和永久删除与不可变 `project_*` Workflow Event 同事务提交，记录 owner/request/前后快照；创建幂等重放不重复事件，事件失败回滚命令；新增稳定分页事件 API，项目详情展示状态/字段变更及完整反馈状态；schema 保持 v20，产出/附件/人工笔记与 Inbox 来源投影继续独立实现                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| v5.8     | 2026-08-28 | 交付 T-13 v0.1-B 统一本地搜索与核心详情直达：`GET /api/v1/search` 跨 Task/Project/Client/活动 Inbox 参数化查询，冻结 200 字符、类型白名单、1–100 条分页、LIKE 转义和确定性相关排序，过滤归档/终态资源；命令面板使用 strict 契约并进入 Task/Project/Client/Inbox 精确路由，Task 与 Inbox 支持刷新恢复；schema 保持 v20，最近使用、健康诊断和 OS 快捷键继续独立实现                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| v5.9     | 2026-08-28 | 将基线推进到 schema v21，交付 Project 人工笔记纵切：独立 `project_notes`、稳定时间线、身份/删除历史保护与 Project 版本传播；API 支持幂等创建、分页、笔记级乐观编辑、带原因软删除和归档只读，业务 JSON 纳入笔记事实；项目详情接入完整状态并区分可编辑笔记与不可变命令审计，项目硬删除明确级联笔记；产出/附件与 Inbox 来源投影继续独立实现                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| v6.0     | 2026-08-28 | 交付 Project 产出聚合纵切：只读 API 在 Project 范围聚合既有 Task Artifact，稳定分页、删除历史、来源 Task/Submission 上下文和 Project 版本同一事务返回；项目详情新增真实产出区与任务详情直达，不复制正文、文件、验收或删除事实；项目附件与 Inbox 来源投影继续独立实现                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| v6.1     | 2026-08-28 | 将基线推进到 schema v22，交付受控 Project Attachment：严格 multipart 幂等上传、稳定分页、鉴权完整性下载、带原因软删除、归档只读、Project 版本传播与不可变墓碑；父项目删除协调 trash/事务补偿，启动恢复、手动备份/恢复演练和业务 JSON 元数据导出覆盖第三类受控文件；项目详情接入预览、上传、下载、完整性、删除和历史状态，Inbox 来源投影继续独立实现                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| v6.2     | 2026-08-28 | 将基线推进到 schema v23，交付 T-11E 第一项来源投影：显式 `requires_followup` Task Artifact 与 Inbox Item/系统事件同事务创建，稳定 Artifact 事件键防重；前端展示来源 Task/批次/产出/项目快照并直达任务；活动来源阻止 Artifact/Task 删除，归档后删除原子标记 `source_deleted_at`、保留快照和追加式审计；任务临期/阻塞与系统故障来源继续独立实现                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| v6.3     | 2026-08-28 | 将基线推进到 schema v24，交付 T-11E Task 阻塞来源：每次 block 与 Inbox Item/system Event 同事务提交，以 Task ID + block 后 version 稳定区分批次，幂等重放不重复；详情展示原因、时间、来源状态和项目并直达 Task；活动来源阻止 Task 删除，全部归档后删除原子标记来源删除并保留快照/审计；任务临期与系统故障继续独立实现                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| v6.4     | 2026-08-28 | 将基线推进到 schema v25，交付 T-11E Task 临期来源：ready 前补偿并每 15 秒扫描非终态 Task，提前 24 小时按 Task+截止时点稳定投影；100 条批次排除已投影事实以持续推进积压，重复扫描/重启不重复，改期生成独立来源；详情展示临期/逾期、截止/投影时间和项目并直达 Task；活动来源阻止删除，归档后协调保留快照/审计；系统故障继续独立实现                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| v6.5     | 2026-08-28 | 交付 Focus D2b 日期范围回顾：复用既有 IANA 时区 completed-only 统计，Focus 页可切换 7 天、30 天、本月和最多 93 天自定义范围；本地先拒绝无效/倒置/超长范围，周期图表按完整每日事实可横向浏览，且不改写活动 Session、休息循环或历史分页；高级分布、原生通知/托盘/DND 继续独立实现                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| v6.6     | 2026-08-28 | 交付 T-13 本地最近使用：命令面板空查询优先展示最近命令/资源；`opc-command-recents-v1` 仅保存命令 ID 或资源类型/ID，最大 8 条、90 天过期，不记录搜索词、标题、摘要或正文；资源展示前回读本地详情，确认 404 即清理，服务故障不伪造资源；健康诊断与 OS 全局快捷键继续独立实现                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| v6.7     | 2026-08-28 | 将基线推进到 schema v26，交付 T-11E 系统维护来源第一段：备份创建失败仍返回 `BACKUP_CREATE_FAILED`，并尽力投影一条安全 Inbox Item；payload 只含 backup/create/`backup_create_failed`/occurred_at/固定提示，不记录 Go error、路径、note、Token 或请求正文；同一 backup:create 仅一个活动 incident，归档后可再开；迁移/Sidecar 启动前失败与诊断包继续独立实现                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| v6.8     | 2026-08-28 | 交付 T-11E 系统维护来源第二段：备份校验操作失败仍返回 `BACKUP_VERIFY_FAILED`，并尽力投影 `backup:verify` 安全 Inbox Item；payload 只含 backup/verify/`backup_verify_failed`/occurred_at/固定提示；同一 backup:verify 仅一个活动 incident。包损坏/篡改保持 `BACKUP_INVALID` 且不投影 Inbox；schema 仍为 v26；迁移/Sidecar 启动前失败、恢复/演练失败与诊断包继续独立实现                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| v6.9     | 2026-08-28 | 交付 T-04B 破坏性迁移前自动回滚包：迁移器解析连续文件头 `-- migration: destructive`；已有工作区先执行安全迁移并在首个破坏性边界停住，以边界 schema 创建、完整校验并原子发布 SQLite+全部 active 受控文件包，关闭旧连接后才继续迁移；备份失败不执行破坏性 SQL、不输出 ready，新建空库跳过；schema 保持 v26，恢复诊断继续延期                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| v7.0     | 2026-08-28 | 将基线推进到 schema v27，交付受控 Workspace Avatar：严格 PNG/JPG/WebP 2 MiB multipart，将 replace/remove 与变化设置原子提交；前端选择即预览、取消恢复、保存后鉴权读取，旧 Data URL 只在服务端无头像时一次性迁移。头像具有单 active、完整性、不可变墓碑和跨四领域 ID guards，并进入启动协调、业务 JSON 元数据、备份/演练/恢复完整链路。                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| v7.1     | 2026-08-28 | 继续交付 T-11E 系统维护来源：备份恢复演练的操作性失败/已验证包不可安全打开尽力投影 `backup:drill`，恢复安排的 pending/源目录/身份读取、回滚点创建或计划发布失败尽力投影 `backup:restore`；复用活动 incident 唯一约束和五字段安全 payload，不泄露备份 ID/路径/error/Token/备注/正文，保持原 API 错误。`BACKUP_INVALID` 与可解释业务结果不投影；schema 保持 v27。                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| v7.2     | 2026-08-28 | 交付数据库与 Sidecar 启动失败补偿：数据库不可用时把三类白名单 kind、稳定 UUID 和 UTC 时间原子写入独立 `OPC_LOG_DIR` journal；下一次健康启动在 ready 前补偿为 `database:startup` / `database:migration` / `sidecar:startup`。稳定 event key 防模糊清理重放，16 条/64 KiB 限额及 strict JSON/UUID/UTC/重复检查隔离损坏文件，不记录 error/路径/Token/正文；完整日志与恢复页继续延期，schema 保持 v27。                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| v7.3     | 2026-08-28 | 将基线推进到 schema v28，交付 Project 完成节点→Inbox：`complete`、Project 审计、P1 收尾事项和 system 投影事件同事务提交；以 Project ID + 完成后 version 稳定区分 reopen 后的新周期，冻结项目名/完成时间/未结任务数快照；活动来源阻止父项目删除，归档后协调保留快照；前端展示来源并直达 Project，不自动建 Task 或伪造验收/开票/收入。                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| v7.4     | 2026-08-28 | 交付项目详情任务层级视图：复用完整项目任务查询筛选根任务，以既有 `parent_task_id` 分页 API 按需递归展开子任务，并提供显示全部任务及父任务上下文的平铺切换；不新增 Project 专用层级状态或复制任务/进度事实，schema 保持 v28。项目级筛选、完整集合可见分页、大数据量性能和高级分析继续延期。                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| v7.5     | 2026-08-28 | 交付项目详情任务浏览器分页与筛选：顶层树/平铺查询改为每页 20 条服务端读取，支持防抖标题/描述搜索和状态筛选；筛选结果强制平铺并显示父任务上下文，清除条件后恢复树，分页计数不再冒充完整集合。复用既有 Task API 与 Query 缓存，不新增 schema；大数据量专项、Focus 高级分析及内嵌 Assignment/Submission 控件继续延期。                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| v7.6     | 2026-08-28 | 交付设置运行诊断：联合严格 `/health` 与 Tauri `sidecar_status` 展示环境、生命周期、app/API/schema 和兼容性，支持重新检查、最近成功降级及复制脱敏摘要；桌面状态先经白名单规范化，令牌、监听地址、原始错误和路径不会进入诊断模型或摘要。命令面板可直达该只读模块；完整日志落盘/轮转、诊断包、恢复页和 OS 全局快捷键继续延期，schema 保持 v28。                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| v7.7     | 2026-08-28 | 交付全局渲染错误恢复：错误边界包裹完整路由/AppShell，异常时显示安全恢复页并支持重新渲染、返回今日和打开边界外设置中的运行诊断；路由变化自动复位，原始异常不持久化、不展示、不复制。组件测试覆盖信息脱敏、重试、诊断直达和跨路由恢复；完整日志/诊断包、启动恢复页和 OS 全局快捷键继续延期，schema 保持 v28。                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| v7.8     | 2026-08-28 | 交付任务生命周期原子批量操作：`/tasks/batch` 新增 start/block/unblock/complete/cancel/reopen，先验证完整选择集的版本、状态、负责人和验收策略，再在单事务内复用 Assignment 收束、待验收撤回、Workflow Event 和 Inbox 投影；任一失败整批回滚。任务页提供原因输入和二次确认，定向测试覆盖非法集合不变、版本冲突不变、事件审计及请求映射；schema 保持 v28，任务看板仍归 v0.2。                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| v7.9     | 2026-08-28 | 交付 Focus D2b 项目时间分布：周期 API 在既有 completed-only interval overlap 窗口内按 Task 查询时当前项目归属聚合 distinct Session、秒数与分钟；未绑定、Task 已删或当前无项目合并为“未归项目”，移动 Task 后历史随当前归属重分类。Focus 页在每日趋势下展示项目占比与用时；自动测试覆盖项目/未归项目、客户端规范化与 UI，schema 保持 v28。                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| v8.0     | 2026-08-28 | 交付 Focus D2b 当地小时分布与最佳时段：周期 API 固定返回 0–23 共 24 桶，completed interval 按 IANA 当地小时和真实 UTC 流逝秒数归类；秋季重复小时合并、春季跳过小时保留零值。Focus 页展示 24 格强度、分钟/块数提示和最佳小时段；严格客户端校验 24 个顺序桶，测试覆盖 DST 重复小时与 UI，schema 保持 v28。                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| v8.1     | 2026-08-28 | 交付 Focus D2b 周几×小时二维热力图：周期 API 固定返回周一至周日、0–23 点共 168 格，与小时分布共用 completed-only、IANA/DST 安全的真实 UTC 秒数归类。Focus 页展示可横向浏览的 7×24 强度矩阵和逐格分钟/块数提示；严格客户端校验 168 个顺序格，测试覆盖跨午夜、DST 重复小时与 UI，schema 保持 v28。                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| v8.2     | 2026-08-28 | 交付 Focus D2b 标签时间分布：completed interval 按绑定 Task 查询时当前标签归类，多标签非互斥覆盖并明确可能重复计时，无任务/无当前标签归入“未加标签”；标签删除或改绑会让历史 Session 随当前关系重分类。Focus 页展示颜色、分钟、块数和口径提示；严格客户端校验 nullable 标签三元组与颜色，测试覆盖多标签/未加标签，schema 保持 v28。                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| v8.3     | 2026-08-28 | 交付脱敏诊断包 v1：本地 API 生成 manifest/runtime/database/maintenance 四个白名单 JSON 的 ZIP，包含版本/平台、SQLite quick-check/PRAGMA/迁移清单和系统维护错误码级汇总；排除 Token、地址、路径、底层错误与业务正文，明确不含原始日志。设置运行诊断支持真实下载与成功/错误反馈，客户端严格校验格式头；测试解压验证文件全集和敏感 canary 不泄漏，schema 保持 v28。                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| v8.4     | 2026-08-28 | 交付 Go Sidecar 脱敏轮转日志：配置成功后同步写 stderr 与 `OPC_LOG_DIR/opc-sidecar.log`，单文件上限 5 MiB、保留 3 份归档；会话令牌和 Bearer 值在最终 writer 遮盖，访问日志排除 query/header/body，panic 排除 recovered 值。日志文件符号链接被拒绝，打开或运行期写入失败自动降级 stderr 且不阻断应用；诊断包继续排除原始日志，Tauri 壳日志和打开日志入口仍待实现，schema 保持 v28。                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| v8.5     | 2026-08-28 | 交付业务 JSON 安全导入 v1：preview/apply 严格校验同 schema 官方表列格式、标量行、无受控文件/活动 Focus 和空目标；apply 强制确认并在导入前创建已校验回滚备份，维护写锁内保持 trigger 生效地原子写入业务表、重建 Focus ledger 并复验外键/quick-check。设置页接入文件选择、预检、阻断、确认及结果；跨 schema、非空目标合并和含文件导入继续后续，schema 保持 v28。                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| v8.6     | 2026-08-28 | 交付含文件业务导出包 v1：维护写锁内生成临时 ZIP，包含业务 JSON、版本化 manifest 和全部 active 受控文件；清单记录稳定相对路径、size/SHA-256 与总量，复制时逐文件复验，缺失/篡改时整体失败并清理 staging。包排除 SQLite、identity、marker、令牌、绝对路径与运行维护事实；设置页提供独立下载，含文件导入仍未开放，schema 保持 v28。                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| v8.7     | 2026-08-28 | 交付含文件业务包安全导入 v1：preview 严格校验同 schema 官方 ZIP 的 manifest、业务 JSON、路径、文件全集、size/SHA-256 与数据库元数据；apply 要求空目标和显式确认，先建已校验回滚备份，再无覆盖发布受控文件并于数据库事务提交前复验磁盘正文。数据库失败补偿本次文件；设置页接入选择、预检、阻断、确认和结果反馈，schema 保持 v28。                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| v8.8     | 2026-08-28 | 交付启动恢复结果诊断：只读 API 汇总待重启、本次已应用、applied 清理残留、failed 隔离和无效恢复记录，仅返回规范 ID/时间/状态/计数；设置页支持重新检查，并从服务端恢复重启门禁。清理 warning、路径与底层错误不进入响应，失败/残留不自动删除；数据库打开前实时进度页仍待 Tauri 壳实现，schema 保持 v28。                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| v8.9     | 2026-08-28 | 交付运行期数据库故障安全投影：版本化 API 非预期数据库错误、`/health` 数据库不可用、Focus 心跳和 Reminder/Task 到期来源扫描失败先尝试去重创建 `database:runtime` 系统维护 Inbox Item；数据库不可写时复用并发安全的白名单 journal，下次健康启动补偿。响应错误码不变，原始 SQL 错误、路径、Token 和请求数据不进入 Inbox/journal；主动低磁盘阈值监测仍待实现，schema 保持 v28。                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| v9.0     | 2026-08-28 | 交付主动低磁盘空间监测：ready 前和每 5 分钟检查数据库/受控文件/备份根，去除重复规范路径，任一根低于固定 1 GiB 时投影 `storage:low_space`；持续低空间周期只提示一次，恢复后再跌破才新建。数据库不可写时安全 journal 补偿；路径、盘符、精确容量和探测错误不进入 Inbox/journal，探测失败不伪造低空间事实；阈值配置仍待评审，schema 保持 v28。                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| v9.1     | 2026-08-28 | 交付桌面打开日志目录：设置运行诊断调用无参数 Tauri command，Rust 仅打开自身 `appLogDir`，不接受前端路径；Windows/macOS/Linux 分别调用系统文件管理器，浏览器模式明确禁用，错误提示不含路径。当前可查看 Sidecar 脱敏轮转日志，Tauri 壳自身日志仍待实现，schema 保持 v28。                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| v9.2     | 2026-08-28 | 交付 Tauri 壳自身脱敏轮转日志：`appLogDir/opc-workspace.log` 仅记录白名单 JSONL 生命周期事件与 Unix 毫秒时间，覆盖 setup、Sidecar 状态、shutdown/restart/exit；不接受任意文本、路径、Token、原始错误或业务数据。5 MiB/3 归档，拒绝 symlink/非普通目标，文件故障降级 stderr 不阻断启动；既有日志目录入口可查看，schema 保持 v28。                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| v9.3     | 2026-08-28 | 交付可配置低空间阈值：schema v29 在破坏性迁移闸门和已验证回滚包后扩展 `app_settings.storage`，保留既有设置事实/triggers；默认 1 GiB，只接受 1–100 GiB 整数。数据与备份页面点击即预览、保存后下一轮扫描生效、取消不写入；Sidecar 每轮读取最新设置，读取失败沿用进程最近安全值或默认值，既有周期抑制/journal 降级保持。Inbox 不记录阈值、路径或精确容量，API 保持 v1。                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| v9.4     | 2026-08-28 | 交付无路径手动容量检查：鉴权 `GET /api/v1/diagnostics/storage` 固定返回数据库/受控文件/备份三个逻辑位置的 `healthy/low/unavailable`、可用/总字节、检查时间和已保存阈值；不返回路径、盘符或探测错误，局部失败独立展示。设置页提供加载/重试/刷新并区分阈值草稿与实际检查阈值；只读检查不投影 Inbox、不改变周期锁存。API v1/schema v29 不变。                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| v9.5     | 2026-08-28 | 交付 WebView→Sidecar request ID：前端每次 API 调用生成 UUID v4 并发送 `X-Request-ID`；Sidecar 规范化合法 UUID、替换非法值，在响应头、错误体和脱敏访问日志中复用。HTTP/网络/超时错误均保留可展示 ID；日志不增加 query/header/body，Tauri 生命周期日志保持独立白名单事件。API v1/schema v29 不变。                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| v9.6     | 2026-08-28 | 交付全局 Sidecar 启动故障恢复页 v1：React 根节点在设置 bootstrap 前消费白名单 `sidecar_status`，桌面 starting/error 拦截全部业务，ready 后放行并持续观察；支持立即重查、无路径日志入口和既有受管 Sidecar 安全重启，只显示 app/API/schema，排除原始 message/路径/Token/业务数据。浏览器开发直通；原进程内重拉起、备份选择/实时恢复进度仍待实现。API v1/schema v29 不变。                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| v9.7     | 2026-08-28 | 交付任务六状态看板 v1：列表/看板切换复用真实服务端筛选与分页，看板强制平铺并固定显示六列/空态/当前页计数；卡片展示现有项目、优先级、截止和标签，复用最多 100 项批量选择及共享详情。v1 不做跨列拖拽，生命周期继续经过既有版本/责任/原因/验收门禁。API v1/schema v29 不变。                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| v9.8     | 2026-08-28 | 交付低空间物理卷身份/同卷去重：Windows 卷 GUID、Unix 设备号只在 Sidecar 单次检查内分组，同卷逻辑位置只探测一次；身份失败退回规范路径，组内失败尝试下一逻辑路径。容量 API 只新增 `shared_volume`，不返回/持久化卷 ID、路径、盘符或底层错误；设置页提示共享容量。API v1/schema v29 不变。                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| v9.9     | 2026-08-28 | 交付任务看板跨列受控生命周期：拖拽只映射既有六命令并二次确认，携带 `expected_version`；阻塞/取消要求原因，解除阻塞只回来源状态，manual/待验收不允许绕过人工接受。服务端成功前不移动卡片，冲突刷新列表。API v1/schema v29 不变。                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| v9.10    | 2026-08-28 | 交付项目任务浏览器组合筛选：搜索/状态之外新增优先级、类型、标签和已/未排期，全部与 `project_id` 在真实 Task API 取 AND；筛选强制平铺并显示父任务上下文，清除后恢复树，工具栏响应窄屏。API v1/schema v29 不变。                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| v9.11    | 2026-08-28 | 交付仅手动 `POST /backups` 的一致性备份低空间准入：双锁内先幂等重放，再于 staging/VACUUM 前按 SQLite 上界、经路径/实际大小复核的 active 文件、marker/manifest 加 20% 且最低 64 MiB 余量，只探测 backup root；精确边界放行，不足/不可确认返回 507/503。拒绝不泄露机器容量信息、不建包、不改业务事实、不投影 generic incident；UI 保留 note 并提供清理/刷新指引。内部自动回滚包不在范围。API v1/schema v29 不变。                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| v9.12    | 2026-08-29 | 交付 Project 详情项目级 Focus 分析与终态 Session 历史：历史/周期 API 可选 canonical UUID `project_id`，非法/非 canonical 返回 400、不存在返回 404、归档可读；按 Task 查询时当前 Project 归属，改绑重分类，无 Task/Task 已删/无项目不进入过滤。报告只统计 completed 的闭合正时长 interval，保持 IANA/DST/跨午夜/Streak；详情提供 7/30 天/本月、总时长/完成数/连续天数、稳定历史分页和两路独立反馈，并精确失效相关 Query。API v1/schema v29 不变，无 migration。                                                                                                                                                                                                                                                                                                                                                                                                                       |
| v9.13    | 2026-08-29 | 交付 Today 截止风险快捷筛选：Task 列表新增 `due_state=overdue/due_soon`，单请求固定 Sidecar UTC `now`，以固定宽度 UTC 纳秒键分别计算 `< now` 与 `[now, now+24h]` 并排除 done/cancelled，同时保证混合小数精度的真实截止顺序；拒绝 due 范围或非 active 状态冲突，固定时钟下列表总数与 Today 统计完全一致。Today 补齐临期卡和逾期/临期可访问切换，提供完整服务端分页、加载/空/错误/重试/页码收敛、既有安全任务操作与低频时钟刷新；排序写入期间锁定风险切换与行写，关闭筛选恢复日期分组且风险视图不拖拽。API v1/schema v29 不变，无 migration，不改保存视图。                                                                                                                                                                                                                                                                                                                            |
| v9.14    | 2026-08-29 | 交付有门禁的父任务自动待验收：schema v30 为 Submission 增加 `origin=manual/child_rollup` 并保留既有行为 manual；只统计直属非取消子任务，要求至少 1 个且全部 done，再检查父任务 todo/in_progress + manual、active owner/person assignee 与 active builtin owner reviewer。system 创建固定摘要、无 Artifact 的 pending child_rollup，并最多进入 waiting_review；owner accept 后才 done。pending 在 readiness/gate 失效时撤回，blocked 保持 blocked 并修正来源；accepted 仅在 readiness 失效时重开。manual/changes_requested 不覆盖，Inbox required 显式独立。API v1/app v0.1.0 不变；迁移/启动不回填历史父任务。                                                                                                                                                                                                                                                                       |
| v9.15    | 2026-08-29 | 交付 Project 生命周期→Client Activity 本地系统事实：schema v31 为 `project_workflow_event` 来源增加部分唯一索引；已关联 Client 的 complete/reopen 在原事务内以 Workflow Event ID 创建 system Actor、空正文、同时间戳的只读动态，并由既有 trigger 递增 Client 聚合版本。无 Client 跳过，迁移/启动不回填，改绑不搬移旧事实，任一下游失败使 Project/Event/Activity/完成 Inbox 整体回滚。客户时间线显示可读业务来源并隐藏内部 UUID；外部沟通/回访仍属后续。API v1/app v0.1.0 不变。                                                                                                                                                                                                                                                                                                                                                                                                      |
| v9.16    | 2026-08-29 | 交付 Project/Task 共用的服务端分页搜索 ClientSelect：Project 新建/编辑、Projects 客户筛选和 Tasks 客户筛选固定每页 20 条，以 250 ms 防抖复用既有列表 API，Query key 隔离关键词/页码并传递 AbortSignal；按 ID 去重，跨页、inactive、读取失败时保留当前选择，只有显式清除才输出空值。加载/等待/空/错误重试/更多结果、combobox/listbox 和完整键盘语义有组件测试；移除 `getAllClients` 串行全量读取。真实浏览器、窄屏和 1,000/10,000 条性能待验收。API v1/app v0.1.0/schema v31 不变，无 migration。                                                                                                                                                                                                                                                                                                                                                                                     |
| v9.17    | 2026-08-29 | 交付任务相关五入口共用的服务端分页搜索 ProjectSelect：新建、详情编辑、项目筛选、批量移动及 Inbox 拆分固定每页 20 条，以 250 ms 防抖隔离关键词/页码/归档模式并传递 AbortSignal；按 ID 去重，以详情/当前页/名称快照保留跨页、失败和归档当前选择，只有显式清除才输出空值。移除 `getAllProjects`；Project 列表所有排序追加 `projects.id ASC`，Count 与 Scan 共用同一只读事务。组件与接线测试覆盖状态/客户上下文、加载/等待/空/错误/更多结果及完整键盘语义。真实浏览器、窄屏和 1,000/10,000 条性能待验收。API v1/app v0.1.0/schema v31 不变，无 migration。                                                                                                                                                                                                                                                                                                                               |
| v9.18    | 2026-08-29 | 交付 Project Artifact→Inbox→Task 人工闭环：Artifact 列表返回 nullable follow-up 的 Inbox ID/version/status/policy/source deletion 与实时 required progress，响应继续使用 Project 数值 ETag 且 `meta.project_version` 保持 Project-only，Inbox 写版本由 `followup.inbox_item_version` 独立表达；项目产出上移到任务后，展示四种跟进状态及阻塞/待验收/取消并深链 Inbox；split 继承可信来源 Project 且可清除/改选，新增完成条件/person 本地责任提示，关系行打开共享 Task；命令面板与 Modal 共用叠层、滚动锁、最上层 Escape 和延迟焦点恢复；成功 Inbox mutation 失效来源 Project，split 另失效 Task/Today/Project。Go 金链覆盖 owner/person + manual owner reviewer、complete、submit/waiting_review、accept 和 automatic resolved/100%，前端另覆盖 person 本地责任提示与提交载荷。app v0.1.0/API v1/schema v31 不变，无 migration、无 AI/LLM/Agent；真实浏览器、窄屏和大数据量仍待验收。 |
| v9.19    | 2026-08-29 | 交付 bundled Sidecar generation-aware 有界恢复：真实 Terminated 后按 500 ms/2 s 最多重启两次，当前代连续 Ready 30 秒重置预算；每代生成新 token 并重新请求动态 port，非 ready 清连接/TanStack Query，generation 变化补偿遗漏 restarting。受管 Go 通过 `OPC_EXIT_ON_STDIN_CLOSE=true` 响应父管道 EOF；DB 父目录固定运行锁在 restore/migration/open 前阻止第二进程触碰数据库；安全应用重启要求 child code 0/no signal，并发 shutdown 共享 stop。T-02 仍部分完成，真实父崩溃/进程树/三平台/安装包及 orphan/孙进程治理未验收或未实现。app v0.1.0/API v1/schema v31 不变，无 migration。                                                                                                                                                                                                                                                                                                   |
