# 任务管理模块

> 实现基线：`HEAD 471f814`（2026-08-27）
>
> 版本边界：当前代码只有三态任务基础链路；扩展状态、Actor 分派、产出和验收均为 v0.1 规划，Agent 执行为 v0.2 规划。

## 定位与边界

Task 是系统中唯一的**可执行工单**，回答“具体要完成什么、当前执行到哪一步、怎样才算完成”。

- Inbox Item 说明“为什么需要处理”，不能替代 Task，也不能直接被分派。
- Assignment 保存“由谁负责”及改派历史；Task 不复制一套负责人事实。
- Agent Run 保存一次本地执行尝试；Run 成功不等于 Task 完成。
- Task Artifact 保存实际产出；Artifact 本身不决定验收是否通过。
- Project 是任务的组织上下文，Focus Session 是工时来源，Workflow Event 是追加式审计。

父子任务只表达真实的完成层级。项目产出产生的“修改、发布、客户确认”等下游工单默认由收件箱关联，不因来源关系而自动成为原任务的子任务。

## 当前实现状态

当前状态为**部分完成**，已具备可运行的基础 CRUD 纵切的一部分。

### 已实现

- SQLite `tasks` 表，字段包含标题、描述、三态状态、优先级、项目外键、截止/计划日期、预估/实际分钟、手动顺序和时间戳。
- `tags`、`task_tags` 表已存在，但尚未接入业务 API。
- 后端支持分页列表、新建、单条读取、三态状态更新和删除。
- 列表 API 已支持状态、优先级、项目、计划日期、标题/描述关键词和多字段排序。
- 创建 API 校验字段并可接受 `Idempotency-Key`；项目 ID 存在时验证外键。
- 前端提供按 `in_progress / todo / done` 分组的列表、新建弹窗、客户端关键词搜索、完成/恢复按钮以及加载/空/错误/重试状态。

### 已知缺口

- 前端固定读取第一页 100 条，搜索也只覆盖已加载数据，没有分页或服务端搜索 UI。
- `PATCH /tasks/:id` 当前与状态路由共用处理器，只能更新 `status`，尚非完整编辑接口。
- 前端没有任务详情、编辑、删除确认或删除入口。
- 状态按钮只在 `done` 与 `todo` 间切换；不能从 UI 开始任务，也没有阻塞、待验收、取消、返工等状态。
- 项目选择器被禁用；列表响应没有真实项目名 join，标签也不会从后端返回。
- 没有父子任务、完成条件、验收策略、Assignment、Artifact、Workflow Event 或乐观并发版本。
- 没有批量操作、拖拽排序、看板、任务依赖和绑定专注工时。
- 当前幂等记录只按 key + endpoint 指向资源，尚无请求摘要、响应重放和冲突检测的完整契约。

## 目标功能

### 任务事实与组织

- 完整创建、读取、编辑、删除；支持服务端分页、筛选、搜索和排序。
- 支持 `kind = work / review / followup / reminder`、标签、项目、父任务、计划/截止时间、预估工时、完成条件和验收策略。
- 任务树防止自身引用和循环引用；父任务进度从有效子任务派生，不保存第二份完成百分比。
- 支持单项与批量操作；v0.1 交付列表视图，看板归入 v0.2。

### 生命周期与验收

- 状态扩展为 `todo / in_progress / blocked / waiting_review / done / cancelled`。
- 生命周期只能通过 `start / block / unblock / complete / submit-output / accept / request_changes / cancel / reopen` 等显式命令改变。
- `review_policy = none` 的普通人工任务可直接完成；manual、Agent 和高风险任务必须提交产出并由 owner 验收。
- 阻塞、取消和返工原因必填；状态变化与审计事件在同一事务中完成。

### 分派与产出

- 每个任务可有 assignee 和 reviewer；同一 role 同时只能有一个活动 Assignment。
- v0.1 支持 owner/person 的人工分派；person 仅是本地责任记录，由 owner 回填线下进度和产出。
- 支持文本、受控本地文件、链接引用和结构化摘要；区分产出者与录入者。
- v0.2 支持健康的本地 agent Actor；每次执行形成独立 Agent Run，成功后进入 `waiting_review`。

## 关键用户流程

### 新建并执行普通任务

1. owner 创建任务，填写计划日期、优先级、项目和预估时长。
2. 系统建立 owner 或 person 的活动 Assignment。
3. owner 执行 `start`，任务从 `todo` 进入 `in_progress`。
4. 对 `review_policy = none` 的人工任务执行 `complete`，任务进入 `done`。

### 从收件箱拆分工单

1. owner 在 Inbox Item 详情创建多条任务或关联已有任务。
2. 同一事务创建父子关系、Inbox 关联、必需标记和初始 Assignment。
3. 任务按自身状态执行；收件箱进度实时从必需任务派生。
4. 任一步失败时全部回滚，不留下孤立任务或无审计分派。

### 提交产出并验收

1. 人工负责人完成工作；person 的线下结果由 owner 代录。
2. `submit-output` 校验至少一个有效 Artifact 或结构化说明，任务进入 `waiting_review`。
3. owner 选择接受或要求返工。
4. 接受进入 `done`；返工保留原产出和审核历史并回到 `in_progress`。

### 改派或阻塞

1. owner 改派时提供原因和 `expected_version`。
2. Sidecar 在一个事务中结束旧 Assignment、创建新 Assignment、递增版本并写 Workflow Event。
3. 若任务阻塞，必须记录原因与 `blocked_from_status`；解除时由服务端恢复原允许状态。

## 数据/API/状态与事件

### 当前数据与 API

- 当前 schema：`tasks`、`tags`、`task_tags`；Task 状态仅 `todo / in_progress / done`。
- 当前 API：`GET/POST /api/v1/tasks`、`GET/PATCH/DELETE /api/v1/tasks/:id`、`PATCH /api/v1/tasks/:id/status`。
- 当前 `PATCH /tasks/:id` 不是非状态字段编辑接口，扩展状态上线后应废弃通用三态状态写入。

### v0.1 规划数据

- 扩展 `tasks`：`kind`、`parent_task_id`、`completion_criteria`、`review_policy`、阻塞信息、提交/审核时间和 `version`。
- 新增 `task_assignments`、`task_artifacts`、`workflow_events`；Actor 数据见 [Actor 模块](actors.md)。
- 扩展状态 CHECK 必须通过 `003_...` 起的递增迁移重建表，不能回写已发布的初始迁移。

### v0.1 规划 API

- 完整 `PATCH /api/v1/tasks/:id` 只修改非生命周期字段。
- `PUT /api/v1/tasks/reorder`、显式状态命令、Assignment 查询/改派、Artifact 查询/提交和任务时间线。
- 状态写入使用 `expected_version` 或 `If-Match`；旧版本返回 `409 CONFLICT`。
- 创建、分派、提交、验收和返工支持带请求摘要的完整幂等重放。

### 状态和事件规则

```text
todo → in_progress → waiting_review → done
  │          │              └→ in_progress
  │          └→ blocked → todo / in_progress / waiting_review
  └────────────────────────→ cancelled
done / cancelled → todo（reopen）
```

Workflow Event 至少记录创建、编辑、开始、阻塞/解除、改派、产出提交、接受、返工、取消和重新打开。它是审计时间线，不作为 Task 当前状态的第二事实源。

## 与其他模块协作

| 模块 | 协作方式 |
|------|----------|
| 收件箱 | Inbox Item 创建/关联 Task，并从必需任务派生进度；Task 状态不复制 Inbox 状态。 |
| Actor | Assignment 把任务分派给 owner/person；v0.2 才支持可执行 agent。 |
| 项目 | Task 可归属 Project；项目进度由项目下任务派生。 |
| 客户 | 任务通过 Project 间接获得客户上下文；客户联系人不会自动变成 Actor。 |
| 专注 | Focus Session 绑定 Task，停止时幂等累计 `actual_minutes`。 |
| 今日 | 按计划日期、优先级和手动顺序聚合任务，并发起受控命令。 |
| 发票/回访 | 后续业务事件经收件箱生成待办任务，不直接改写任务状态。 |

整体依赖见[整体功能架构](../functional-architecture.md)。

## 分阶段实施

1. **任务基础事实**：完整 PATCH、删除 UI、分页/筛选/排序、标签、项目选择、父子任务、批量操作和基础乐观锁，暂时保留三态兼容。
2. **Actor 与受控状态**：引入 Actor、Assignment、扩展状态、完成条件、验收策略、Artifact 和 Workflow Event；迁移历史任务到 owner Assignment。
3. **收件箱集成**：支持原子拆分/关联、必需标记和由任务派生的收件箱解决规则。
4. **专注集成**：绑定持久化 Session，停止与工时累计使用同一幂等事务。
5. **v0.2 增强**：任务看板、本地 Agent Run、取消/重试和返工闭环；任务依赖另行排期。

## 验收标准

- CRUD、分页、筛选、排序和搜索均在真实 Sidecar/SQLite 上工作，超过 100 条不丢失。
- 父任务不能指向自身或形成环；所有子任务取消时父任务不会自动完成。
- 非生命周期 PATCH 无法写入 `status`、完成/提交/审核时间。
- manual、Agent 和高风险任务无法绕过“提交产出 → owner 验收”。
- 同一任务同一 role 只有一个活动 Assignment；并发改派或验收拒绝旧写入。
- person 分派明确说明不发送、不登录、不同步；停用后历史仍完整可查。
- Artifact 文件只从应用受控目录读取，包含元数据和 SHA-256；软删除可审计。
- 重复命令和幂等重放不重复建任务、累计工时或写时间线。
- 加载、空、错误、重试、删除确认、键盘导航和真实浏览器流程均通过。

## 相关代码/PRD链接

- [PRD：任务管理](../opc-workspace-PRD.md#52-任务管理)
- [PRD：任务表与编排规划表](../opc-workspace-PRD.md#主要数据表)
- [整体功能架构](../functional-architecture.md)
- [TasksPage.tsx](../../apps/web/src/pages/TasksPage.tsx)
- [TaskList.tsx](../../apps/web/src/components/TaskList.tsx)
- [NewTaskModal.tsx](../../apps/web/src/components/NewTaskModal.tsx)
- [前端 API client](../../apps/web/src/api/client.ts)
- [任务 API](../../services/sidecar/internal/api/tasks.go)
- [当前 Task model](../../services/sidecar/internal/models/task.go)
- [初始数据库迁移](../../services/sidecar/internal/database/migrations/001_initial_schema.sql)
