# opc-workspace 整体功能架构

> 文档版本：1.2
> 日期：2026-08-27
> 依据：[PRD v1.9](opc-workspace-PRD.md)
> 当前实现基线：app v0.1.0 / API v1 / SQLite schema v7

## 1. 目的

本文说明各功能模块如何协作、跨模块状态如何流转、哪个对象拥有最终事实，以及后续实现应遵循的依赖顺序。

它不替代 PRD，也不把规划描述成当前代码。各模块的详细用户流程和验收条件见 [模块文档索引](modules/README.md)；当前实现仍以仓库代码和测试为准。

## 2. 总体设计原则

1. **本地优先**：核心功能断网可用，业务事实保存在 SQLite 和应用控制的本地文件目录。
2. **单一事实源**：一个状态只由一个领域对象负责，其他模块通过查询或事件派生展示。
3. **任务即工单**：项目产出拆出的工作、发票待办、审核和提醒后续动作都使用 Task，不新增第二套执行实体。
4. **收件箱负责编排**：Inbox Item 解释“为什么要处理”，Task 解释“具体做什么”。
5. **Actor 只表达责任**：Assignment 保存责任变化；person 不等于在线用户，agent 不等于已完成工作。
6. **Agent 必须验收**：Agent Run 成功只产生 Task Artifact；是否完成由 Task 的验收策略决定。
7. **事件可重放且去重**：业务事件使用稳定 key，重扫、重启和重试不得生成重复工作。
8. **规划与实现分离**：路由、按钮、样式或预留表不代表业务已交付。

## 3. 系统分层

```text
┌──────────────────────────────────────────────────────────────┐
│ Tauri / Rust 桌面层                                          │
│ 窗口、单实例、目录、Sidecar 生命周期、托盘/通知/文件选择规划 │
└──────────────────────────────┬───────────────────────────────┘
                               │ IPC：运行状态与受控桌面能力
┌──────────────────────────────▼───────────────────────────────┐
│ React / WebView 界面层                                       │
│ 页面、表单、收件箱编排、Query 缓存、短期 UI 状态             │
└──────────────────────────────┬───────────────────────────────┘
                               │ Bearer HTTP /api/v1
┌──────────────────────────────▼───────────────────────────────┐
│ Go Sidecar 应用服务层                                        │
│ 领域校验、事务、状态命令、提醒调度、审计、备份、Agent 管理   │
└──────────────────────────────┬───────────────────────────────┘
                               │ SQLite / 受控文件系统
┌──────────────────────────────▼───────────────────────────────┐
│ 本地事实层                                                   │
│ opc-workspace.db、attachments、artifacts、invoices、backups │
└──────────────────────────────────────────────────────────────┘
```

### 3.1 当前实现

- Tauri 已具备基础窗口、单实例、数据目录和 Sidecar 启停基座。
- React 已具备三栏框架、今日/任务/专注基础能力、项目卡片与详情、设置和命令面板；任务页已接服务端分页/搜索/筛选、根任务树、标签管理、批量安全操作和计划组手动排序。
- Go 已提供健康检查、任务事实与标签 API、任务批量/排序 API、项目 CRUD/生命周期 API、Actor 查询/创建/更新 API 和今日统计。
- SQLite schema v7 已有 tasks、projects、clients、invoices、focus_sessions，以及 `actors`、`task_assignments`、`workflow_events` 等基础表；v3 为 Project 增加生命周期版本，v4 增加安全幂等快照，v5 用触发器在任务/发票关联与聚合字段、客户名称或删除变化时原子递增 `projects.version`，v6 增加 Task 类型、父子关系、完成标准、Task/Tag 版本及关系索引/循环保护 trigger，v7 增加责任主体、分派历史和工作流事件基础。
- 任务读取已返回项目/父任务标题、标签和子任务统计；任务与标签写入使用 `ETag`/`If-Match`，父子或嵌入标签事实变化会使相关任务版本失效。
- 任务批量移动项目、改计划日期、加/删标签和完整计划日期组排序都在事务中先校验全部 ID/版本，再整体提交或回滚。
- 任务响应嵌入的项目名也属于版本快照：Project 名称变化或硬删除会递增关联 Task 版本，避免基于旧项目上下文覆盖任务。
- 任务可关联项目；任务读取返回 `project_name`，项目读取从关联任务派生进度及 `actual_minutes` 合计。
- 归档项目不再接受新任务关联；schema v5 让任务、发票和客户聚合事实变化同步失效 Project `ETag`，避免基于旧汇总完成、归档或硬删除。
- schema v7 以固定 UUID 初始化唯一 owner 与 system，按历史任务完成状态幂等回填 owner Assignment 和 `migration_assignment_backfill` 事件；数据库保护内置主体、活动分派与引用历史。
- 设置中的“人员与责任”已接真实 Actor API：可管理本地 person、编辑 owner 展示名并查看 system；创建支持幂等重放，读取/更新使用 `ETag`/`If-Match`，存在活动 Assignment 时 API 与数据库共同拒绝停用。
- 任务详情已接 Assignment API/UI：可查询当前 assignee/reviewer 与结束历史，完成首次分派、改派和结束；命令使用 Task `If-Match`/`version`、可选幂等快照和事务化 Workflow Event。完成 Task 会结束活动 Assignment，重新打开不会恢复旧记录。
- 当前仍未实现 Client CRUD、项目产出/附件/Inbox 集成、受控任务状态与 Artifact、通用 Workflow Event 时间线和 Focus Session 持久化，因此完整人工编排与项目模块仍是部分完成。

### 3.2 目标扩展

- v0.1：在已交付的项目纵切、任务事实层、Actor 身份和 Assignment 人工分派基础上，继续完成受控任务状态、客户、项目产出与收件箱人工编排、专注持久化、基础备份恢复和桌面可靠性。
- v0.2：本地 Agent Runtime、任务看板和预设自动化。
- v0.3：路线图、内容日历、高级备份配置和规划增强。
- v0.4：收入/支出、发票和客户回访。
- 待定：本地知识库与 AI 助手。

## 4. 核心领域对象与事实归属

| 对象 | 拥有的事实 | 不应保存的事实 |
|------|------------|----------------|
| Task | 工作内容、生命周期、完成条件、验收策略、父子层级 | 收件箱已读/稍后、Agent 单次运行状态 |
| Project | 项目资料、客户关联、项目状态 | 手工维护的任务完成百分比 |
| Client | 客户资料和本地业务关系 | 重复存储项目数或已付款总额 |
| Inbox Item | 事件来源、分诊、已读、稍后、解决策略 | Task 执行状态和负责人副本 |
| Actor | 本地责任主体身份和启停状态 | 某项任务的当前状态 |
| Assignment | 当前负责人和改派历史 | Task 完成状态 |
| Agent Run | 一次本地执行的输入快照、状态、错误和输出清单 | Task 是否最终验收完成 |
| Task Artifact | 文本、文件、链接或结构化产出 | 验收结论 |
| Reminder | 本地触发时间和调度状态 | 到期后的处理进度 |
| Focus Session | 专注区间、累计秒数、结束原因 | Task 的业务完成状态 |
| Invoice | 发票金额、客户、日期和业务状态 | 收件箱处理状态 |
| Workflow Event | 谁在何时做了什么、状态前后值 | 可被业务 API 修改的当前状态 |

## 5. 功能模块协作总览

| 模块 | 主要输入 | 自己负责 | 主要输出 / 下游 |
|------|----------|----------|-----------------|
| [今日](modules/today.md) | Task、Focus、Inbox 派生统计 | 当日执行入口和聚合展示 | 任务操作、开始专注、打开收件箱 |
| [任务](modules/tasks.md) | Project、Actor、Inbox 来源 | 唯一工单、状态、完成条件、产出 | Project 进度、Inbox 进度、Focus 工时 |
| [项目](modules/projects.md) | Client、Task | 当前已实现资料、受控生命周期、归档恢复和任务聚合 | 交付 Artifact、附件、时间线和 Inbox 事件仍待实现 |
| [客户](modules/clients.md) | Project、Invoice、Activity | 客户资料和本地业务关系 | 回访、发票和 Inbox 来源 |
| [收件箱](modules/inbox.md) | 项目产出、Reminder、Task/Agent/系统事件 | 受理、拆分、关联、跟进和解决 | Task 与 Assignment；今日待处理统计 |
| [Actor](modules/actors.md) | 设置中的本地 person 管理、任务详情 Assignment | 已实现 owner/person/system 责任身份与启停、owner/person 人工分派与审核人历史；agent 仅保留类型边界 | 受控验收、Artifact 与通用事件时间线仍待接入 |
| [本地 Agent](modules/local-agents.md) | agent Assignment、Task 上下文、能力授权 | 单次受控执行 | Agent Run、Task Artifact、待验收或失败事件 |
| [专注](modules/focus.md) | 当前 Task | 活动 Session 和有效工时 | Task actual_minutes、今日/统计数据 |
| [设置](modules/settings.md) | 当前 localStorage 偏好与 Actor API | 本地偏好界面及已实现的 person 管理；版本化 app_settings 仍待实现 | 布局、主题、Actor、备份和桌面行为 |
| [命令面板/搜索](modules/command-search.md) | Task/Project/Client/Inbox 索引 | 统一查找与快捷操作入口 | 跳转详情或触发受控命令 |
| [数据管理](modules/data-management.md) | 全部本地事实与文件 | 备份、校验、恢复、导入导出和诊断归档 | 恢复后的完整应用状态和诊断包 |
| [桌面平台](modules/desktop-platform.md) | Web 与 Sidecar 生命周期 | 原生窗口、进程、权限、运行日志和发布 | 可运行、可诊断的本地应用环境 |
| [财务/发票](modules/finance-invoices.md) | Client、Project、owner 确认 | 财务与发票业务事实 | 本地提醒、Inbox Item、客户聚合 |
| [客户回访](modules/client-followups.md) | Client、Reminder、Actor | 本地回访计划与结果 | Inbox 到期项、客户活动 |
| [路线图](modules/roadmap.md) | Project/Task 派生进度 | 季度和里程碑规划 | 临期/达成 Inbox 事件 |
| [内容日历](modules/content-calendar.md) | Project、Task、日期 | 内容计划和准备工作 | 准备 Task、审核/发布时间 Inbox 事件 |
| [自动化](modules/automation.md) | Workflow Event、本地时钟 | 预设规则和去重执行 | 本地 Inbox Item、Task 或 Reminder |
| [知识库](modules/knowledge-base.md) | 本地文件 | 导入、FTS 索引、来源定位和删除 | 搜索结果、可选 AI 上下文 |
| [AI 助手](modules/ai-assistant.md) | 用户显式选择的本地上下文 | 本地问答、摘要和建议 | 建议或待验收 Task Artifact |

## 6. 跨模块主流程

### 6.1 项目产出拆分为后续工单

```text
Project Task 提交 Artifact
  → Task 根据 review_policy 进入 waiting_review
  → 明确 requires_followup 的交付产出生成 Inbox Item
  → owner 验收来源 Task
  → 在 Inbox 中拆分/关联后续 Task
  → 为每个 Task 创建 Assignment
  → 人工执行或 v0.2 本地 Agent 执行
  → 提交 Artifact → 验收/返工
  → 所有活动必需 Task 完成
  → Inbox Item resolved
```

防循环规则：由 Inbox 拆出的下游 Task 默认不成为来源 Task 的子任务；普通子任务产出也不递归创建新的 Inbox Item。只有项目交付类产出或 owner 显式标记 `requires_followup` 才生成新项。

### 6.2 发票待办与催办（v0.4）

```text
Project 达到开票节点
  → 本地规则创建“准备发票” Inbox Item
  → 拆分核对金额 / 生成草稿 / owner 审核 Task
  → owner 手动确认发送
  → Reminder 在到期日生成去重 Inbox Item
  → owner 记录付款
  → Invoice 与 Financial Entry 在同一事务更新
  → 对应付款确认 Task 通过任务命令完成/验收
  → Inbox 领域服务按 resolution_policy 派生是否 resolved
```

付款事务不直接关闭 Inbox Item；若其他必需 Task 仍处于 blocked、waiting_review 或未完成状态，工作项继续跟踪。本地 Agent 只能生成草稿或 PDF Artifact，不能自动发送、确认付款或修改财务事实。

### 6.3 一次性本地提醒

```text
owner 创建 Reminder(scheduled)
  → 本地调度器到期扫描
  → source_event_key 幂等生成 Inbox Item(open)
  → Reminder(fired) 记录 inbox_item_id
  → owner 解决、稍后提醒或拆分 Task
```

Reminder 是调度事实；Inbox Item 是到期后的处理事实。两者不复用状态。

### 6.4 专注与任务工时

```text
选择 Task
  → 创建 Focus Session(active)
  → pause/resume 使用绝对时间结算
  → 异常重启时 active 先进入 recovery_pending，由用户决定是否计入不确定间隔
  → stop 在同一幂等事务累计 Task.actual_minutes
  → 今日、项目和统计模块读取聚合结果
```

Task 是否完成仍由任务模块决定，专注结束不能自动完成任务。

当前项目详情已经读取任务 `actual_minutes` 的合计，但 Focus Session 尚未持久化，也不会自动更新该字段；上述停止事务仍是后续目标契约。

### 6.5 本地 Agent 执行（v0.2）

```text
Task 已分派给健康 agent Actor
  → owner 创建 Agent Run
  → Sidecar 校验 Adapter、Assignment、路径和能力
  → 发放短时单次能力令牌或受控进程管道
  → Adapter 本地执行并写入受控 Artifact 目录
  → Run succeeded / failed / interrupted
  → succeeded 默认令 Task 进入 waiting_review
  → owner accept 或 request_changes
```

若 Task 已被活动 Inbox Item 跟踪，Agent 输出只更新该工作项；只有未被收件箱跟踪的 Agent Task 才生成一条去重的验收项。

## 7. 状态传播规则

### 7.1 不允许跨模块直接写状态

- Inbox 不能直接把 Task 标记完成，只能调用任务状态命令。
- Project 进度从 Task 派生，不能由项目页面单独覆盖。
- Focus 只能累计工时，不能修改 Task 完成状态。
- Agent Run 不能直接修改 Project、Client、Invoice 或财务事实。
- Automation 只能创建本地 Inbox Item、Task 或 Reminder；高风险业务写入进入人工审核。

### 7.2 派生与自动关闭

- Inbox 进度只统计 `unlinked_at IS NULL` 的活动关联。
- `all_required_tasks_done` 只有至少一个活动必需 Task 且全部 done 时成立。
- cancelled、blocked、waiting_review 或失败中的必需 Task 会阻止自动解决。
- 强制关闭是 owner 的危险操作，必须二次确认、填写原因并写入审计。
- 父 Task 只有至少一个非取消子 Task 且全部完成时才允许自动推进；所有子任务取消不触发空集合完成。

## 8. 事件与幂等

### 8.1 事件来源

- v0.1：Reminder 到期、显式 follow-up 的项目产出、Task 临期/阻塞、备份/迁移/Sidecar 故障。
- v0.2：Agent Runner 追加 Workflow Event，内置自动化投影器以统一 source_event_key 作为 Agent 失败/验收 Inbox Item 的唯一生产者；其他预设自动化也复用同一去重框架。
- v0.3：路线图里程碑、内容审核与发布时间。
- v0.4：Invoice 到期/逾期、客户回访和项目开票节点。

### 8.2 去重规则

- 每个本地业务事件产生稳定 `source_event_key`。
- Artifact follow-up key 包含 Artifact ID；Agent 验收 key 包含 Run ID。
- 自动化使用 `rule_id + event_id` 去重。
- 可重试写请求使用 `Idempotency-Key + Actor + HTTP 方法 + 规范化路径` 作用域。
- 同 key 不同请求摘要返回 `409 CONFLICT`。
- 幂等重放不重复写 Workflow Event。

当前 Task、Tag、Project 与 person Actor 创建已实现该原则的基础版本：schema v4 保存规范化请求 SHA-256、首次资源响应快照与原始 `201` 状态，因此资源后续编辑或删除不影响同请求重放；不同请求复用 key 会冲突，旧记录缺少快照时拒绝不安全重放。Task 创建的 v2 摘要覆盖类型、父级、完成标准和稳定排序后的标签，同时兼容安全的旧 v1 快照；schema v5 把 Project 聚合依赖纳入版本冲突检测，schema v6 再为 Task/Tag 事实、父子聚合和标签嵌入提供版本失效。Actor 创建同样保存规范化请求摘要与首次 `201` 快照，并保证重放不重复写 `actor_created` 事件。Assignment 创建、改派和结束也可选保存包含 Task 预期版本的请求摘要、首次响应快照与原状态码；同请求重放不重复写责任事件，异体复用返回冲突。Inbox 与其他通用事件投影的幂等契约仍待后续编排纵切实现。

## 9. 本地安全与权限边界

- WebView 使用启动期随机 Bearer Token 调用普通业务 API。
- 本地 Agent 不使用 WebView Token，也不能直接打开 SQLite。
- Agent Runtime 使用专用路由/中间件和单次能力令牌，或使用受控进程管道。
- Adapter 默认只获得本次 Run 明确授权的输入、路径和输出目录。
- 跨平台进程沙箱和网络阻断必须经过 ADR 与实际验证；无法强制时 Adapter 只能保留为禁用诊断记录，正式 Agent 分派与执行入口不得启用。
- person 无账号、无登录、无远程通知；由 owner 记录线下进度和结果。
- 当前阶段不提供线上更新、云同步、远程模型或自动对外发送。

## 10. 故障与恢复协作

| 故障 | 责任模块 | 对其他模块的行为 |
|------|----------|------------------|
| Sidecar 启动失败 | 桌面平台 | 展示全局恢复页；业务页面不得显示伪数据 |
| Agent 中断 | 本地 Agent + 自动化投影器 | Runner 将 Run 标记 interrupted 并追加事件；内置投影器以统一 key 创建/更新 Inbox Item，Task 保持未完成 |
| 备份失败 | 数据管理 | 通过桌面日志管线记录 request ID，并产生 system_maintenance Inbox Item |
| 恢复进入 applying | 数据管理 + 桌面平台 | 获取维护锁、阻止普通退出；强制终止后依据 journal 完成或回滚 |
| 来源资源删除 | 来源模块 + Inbox | open/tracking 时默认限制；允许删除后保留快照并显示来源不存在 |
| 并发旧写入 | Sidecar 领域服务 | Task/Tag 当前事实、父子/标签嵌入、Assignment 责任变化、Project 资料/状态或其任务/发票/客户聚合变化，以及 Actor 资料/启停变化，都会使旧 `If-Match` 或 `expected_version` 返回 409；Assignment 前端保留选择/原因并要求用户基于刷新后的版本重新确认 |
| Artifact 文件缺失 | Task/Agent + 数据管理 | 保留元数据和审计，标记损坏，不把 Task 静默视为完成 |

## 11. 实施依赖顺序

已落地的基础纵切是 Task 三态 CRUD、项目/父子/标签/完成标准、分页筛选、批量安全操作、计划组排序和乐观锁，Project CRUD、乐观锁、受控状态、归档恢复和任务聚合，以及 Actor 身份、历史 Assignment 回填、Assignment API/UI 和责任审计事件。后续依赖顺序为：

```text
受控 Task 状态 / review_policy / Artifact / 通用 Workflow Event 时间线
  → Client CRUD / Project 客户选择、产出、附件与事件
  → Inbox Item / Reminder / 拆分 / 人工分派与验收
  → Today 正确日期聚合 / Focus 持久化
  → 备份恢复 / 桌面日志与故障恢复
  → v0.2 本地 Agent / 预设自动化 / Task 看板
  → v0.3 路线图 / 内容日历 / 高级数据管理
  → v0.4 财务 / 发票 / 客户回访
  → 本地知识库
  → AI 助手
```

在前置事实层未完成时，下游模块只能展示明确的占位或禁用态，不能以静态数据、无行为按钮或预留表冒充可用功能。

## 12. 跨模块验收基线

- 断开网络后，v0.1 人工工作流完整可用。
- 每个业务状态有且只有一个事实源。
- 跨模块写操作具备事务、幂等和冲突检测。
- 任何来源事件重扫和重启后不重复创建工作。
- 任务拆分失败不遗留部分 Task、关联、Assignment 或审计。
- person 分派明确显示仅作本地责任记录。
- Agent 成功不能绕过 waiting_review 和 owner 验收。
- 模块删除、归档和恢复后，关联关系与历史仍可解释。
- 加载、成功、空、错误、重试、禁用和不可用状态均有真实 UI。
- 页面、API、数据迁移和验收测试同时交付后，模块才可从“骨架/部分完成”升级。
