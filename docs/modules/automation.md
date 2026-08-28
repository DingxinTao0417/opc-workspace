# 自动化模块

> 目标版本：v0.2。首版只提供可审计的预设本地规则，不属于 v0.1。

## 定位与边界

自动化把本机已经发生的 Workflow Event 或本地时间条件转换为明确的下一步工作，减少重复整理。它是一个受限的本地规则引擎，不是通用脚本平台、云工作流服务或对外消息机器人。

- 触发器只读取本地事件和本地调度时间。
- v0.2 动作白名单仅允许创建本地 Inbox Item、Task 或 Reminder，以及记录运行结果。
- 不执行任意 Shell/SQL/HTTP，不读取未授权目录，不自动发送邮件、发票、客户消息或内容平台请求。
- 不自动确认付款、完成回访、发布内容或修改其他模块的受控业务事实。
- v0.2 只提供内置预设规则和有限参数，不提供自由表达式或用户代码编辑器。
- AI 或本地 Agent 不能自行创建、启用或扩大规则权限。

## 当前实现状态

- 历史自动化视觉原型已移除；当前没有 React 入口、规则表、API、执行器、调度器或运行记录，后续以本文和 PRD 为准。
- Workflow Event、Task 六状态命令、manual Submission/Artifact 验收以及独立的手工 Inbox Item 已经交付，可作为未来自动化的只读事实基础；Reminder、业务来源到 Inbox 的投影、自动化专用事件订阅与执行器仍处于规划阶段。
- 当前代码没有外部工作流连接器，也没有后台网络发送能力。

## 目标功能

- 展示内置预设规则，允许用户启用/禁用并调整受限参数。
- 支持事件触发和本地时间触发，显示下一次计划运行时间。
- 在启用前预览触发条件、将创建的本地对象和权限范围。
- 记录每次运行的触发来源、动作结果、耗时、错误、重试和去重信息。
- 失败运行可由用户手动重试；可重试错误使用有上限的退避策略。
- 对应用关机期间错过的时间计划执行有界补偿，不补发过期且已失去意义的重复提醒。
- 防止同一事件重复执行、规则递归触发和循环创建工作项。

首批预设可包括：

- 项目完成后创建“核对并准备发票”Inbox Item。
- 每日指定时间创建“查看今日任务”提醒。
- 发票临期/逾期事件创建本地跟进任务；发票模块上线前保持禁用。
- 周五指定时间创建本周复盘提醒。
- 本地 Agent Run 失败后创建或更新诊断 Inbox Item；该内置规则是此类工作项的唯一生产者，固定使用 `agent-run:<run_id>:failed` 去重，仅在本地 Agent 完成后启用。

## 关键用户流程

1. **浏览规则**：用户打开自动化设置，查看规则名称、触发条件、动作、所需模块和当前可用性。
2. **配置预览**：用户调整本地时间、阈值或目标列表；界面展示示例输入、将创建的对象和明确的“不对外发送”边界。
3. **启用规则**：Sidecar 校验依赖、参数、时区和动作白名单后保存版本化配置。
4. **事件执行**：Workflow Event 到达后生成稳定去重键，事务性创建目标对象并写 Automation Run。
5. **计划执行**：本地调度器按 IANA 时区计算时间；重启后执行有界补偿并跳过已处理窗口。
6. **查看与重试**：用户查看运行详情；失败时手动重试会创建新的 attempt，但复用原逻辑去重边界。
7. **停用**：停用立即阻止新运行，但保留规则版本、历史 Run 和已经创建的本地对象。

## 数据、API、状态与事件

### 数据

- `automation_rules`：`id`、稳定 `preset_key`、名称、启用状态、触发类型、受限配置、动作类型、动作配置、时区、下一次运行、版本和审计时间。
- `automation_runs`：`id`、规则及规则版本、来源事件、计划窗口、去重键、状态、attempt、开始/结束时间、错误码、结果摘要和因果链。
- 预设定义由代码版本化；数据库只保存允许用户修改的参数，不保存可执行代码。
- `dedupe_key` 对成功/进行中的相同运行唯一；`rule_id + event_id` 是事件触发的最低去重边界。
- 因果元数据记录 `caused_by_run_id` 和深度；超过上限或检测到同规则循环时跳过并写审计。

### API

- `GET /api/v1/automations/rules`
- `GET / PATCH /api/v1/automations/rules/:id`
- `POST /api/v1/automations/rules/:id/preview`
- `POST /api/v1/automations/rules/:id/enable`
- `POST /api/v1/automations/rules/:id/disable`
- `GET /api/v1/automations/runs`
- `GET /api/v1/automations/runs/:id`
- `POST /api/v1/automations/runs/:id/retry`

API 不提供任意 trigger/action 类型创建接口；所有枚举都由服务端白名单校验。

### 状态与事件

- 规则状态：`enabled / disabled / unavailable`；`unavailable` 表示依赖模块未交付或权限条件不足。
- Run 状态：`queued / running / succeeded / failed / skipped / cancelled`。
- 事件示例：`automation.rule_enabled`、`automation.run_started`、`automation.run_succeeded`、`automation.run_failed`、`automation.run_skipped`。
- 自动化自身事件默认不能再次触发同一规则；确需链式处理必须由预设显式声明并受深度限制。

## 与其他模块协作

- **Workflow Event**：唯一业务触发事实，不从页面展示文本或数据库轮询猜测状态变化。
- **收件箱、任务、提醒**：v0.2 唯一允许的动作目标；写入和 Run 成功在同一事务中提交。
- **项目/发票/回访/路线图/内容日历**：各模块只发布本地事件；对应模块未上线时相关规则显示 unavailable。
- **本地 Agent**：Agent Run 可成为事件源，但自动化不能提升 Agent 能力或绕过人工验收。
- **桌面通知**：未来可由 Reminder 统一触发本应用通知；通知权限被拒绝时仍保留应用内提醒。

## 分阶段实施

1. **A1 前置事实层（部分完成）**：Workflow Event、Task 生命周期、Submission/Artifact 审计与独立的手工 Inbox Item 已交付；继续实现 Reminder、业务来源投影与自动化消费约定。
2. **A2 ADR 与预设目录**：固化触发/动作白名单、时区、补偿、重试、递归和权限边界。
3. **A3 数据/API**：新增规则与 Run 迁移，实现列表、配置、启停、预览和历史查询。
4. **A4 事件执行器**：实现事务写入、`rule_id + event_id` 去重、失败分类和有界重试。
5. **A5 本地调度器**：实现下一次运行、时区、休眠/关机补偿和重复窗口防护。
6. **A6 界面与验收**：实现 React 设置界面、运行日志、失败详情、依赖不可用状态和真实重启测试。

## 验收标准

- 同一规则和事件无论重放、并发或重启多少次，最多产生一份目标工作项。
- 时间规则在用户时区、夏令时、休眠、跨午夜和关机补偿场景下结果正确。
- 停用后不再开始新运行；运行历史和已创建对象不被删除。
- 失败重试有上限、有新 attempt 记录，不覆盖原失败；不可重试错误不会循环。
- 递归、循环和因果深度限制有确定性测试。
- 依赖模块不可用时规则不能启用，UI 明确说明原因。
- 所有动作均落在本地白名单；测试验证不存在 HTTP、任意 Shell、外部消息或未经确认修改业务事实的路径。
- 加载、空、错误、重试、长历史分页和并发配置冲突均有测试。

## 相关 PRD 与代码链接

- [产品 PRD](../opc-workspace-PRD.md)（§5.9“自动化”、§10.7）
- [Sidecar 路由](../../services/sidecar/internal/api/router.go)
- [初始数据库迁移](../../services/sidecar/internal/database/migrations/001_initial_schema.sql)
- [收件箱页](../../apps/web/src/pages/InboxPage.tsx)
- [前端路由](../../apps/web/src/App.tsx)
