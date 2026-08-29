# 自动化模块

> 当前基线：app v0.1.0 / API v1 / SQLite schema v33（2026-08-29）。首个 v0.2 纵向切片已交付可审计的预设本地规则；本地 Agent、发票和自由规则仍未交付。

## 定位与边界

自动化把本机已经发生的 Workflow Event 或本地时间条件转换为明确的下一步工作，减少重复整理。它是一个受限的本地规则引擎，不是通用脚本平台、云工作流服务或对外消息机器人。

- 触发器只读取本地事件和本地调度时间。
- v0.2 目标动作白名单允许本地 Inbox Item、Task 或 Reminder；当前已交付执行器只实现 Inbox Item 与 Reminder，Task 动作随其依赖预设另行交付。
- 不执行任意 Shell/SQL/HTTP，不读取未授权目录，不自动发送邮件、发票、客户消息或内容平台请求。
- 不自动确认付款、完成回访、发布内容或修改其他模块的受控业务事实。
- v0.2 只提供内置预设规则和有限参数，不提供自由表达式或用户代码编辑器。
- AI 或本地 Agent 不能自行创建、启用或扩大规则权限。

## 当前实现状态

- schema v33 已新增 `automation_rules` 与不可变终态 `automation_runs`。五个代码所有的稳定预设在 Sidecar 初始化时幂等登记，数据库只保存开关、受限配置、下一计划时间、版本和运行快照，不保存脚本。
- 三个预设当前可用：Project `project_completed` → 本地核对开票 Inbox Item、每日查看今日任务 Reminder、周五本周复盘 Reminder。发票逾期和 Agent Run 失败预设可见但固定为 `unavailable`，不能启用。
- 设置左栏已有“自动化”模块：规则列表、依赖状态、配置即时服务端预览、IANA 时区/当地时间、优先级、保存、启停、最近运行、空/加载/错误和失败手动重试均接入真实 API。
- 事件执行器只挂接明确的 Project 完成 Workflow Event；同一 `rule_id + source_event_id` 最多一个首轮 attempt。动作、成功 Run 和审计事件同事务提交；动作失败回滚动作写入并保留失败 Run，自动化基础设施异常由外层 savepoint 隔离，不回滚已经完成的 Project。
- 本地调度器在 Sidecar ready 前和每 15 秒扫描：daily/weekly 按 IANA 当地钟点计算，DST 缺失分钟推进到首个有效分钟，重复分钟选择第一次；离线旧窗口折叠为一条 skipped Run，只在当前当地日创建 Reminder，并推进唯一下一窗口。
- 失败最多 3 次 attempt，后台退避为 1 分钟、5 分钟；手动重试创建新 attempt，保留原规则版本、配置和动作快照。停用阻止新运行与后台重试，历史和既有对象不删除。
- 业务 JSON/ZIP 导入导出已包含规则与 Run；导入只接受五个稳定预设身份、当前 schema、合法配置/关系和空目标，未改动的默认禁用规则不阻塞 fresh-target 判定。
- 当前实现没有外部工作流连接器、后台网络发送、Shell/SQL/HTTP 动作、自由表达式或 Agent 执行路径。

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

- `automation_rules`：`id`、稳定 `preset_key`、`enabled`、严格 `config_json`、nullable `next_run_at`、`version` 和审计时间。名称、描述、触发与动作定义来自代码目录，身份不可改。
- `automation_runs`：`id`、规则及原规则版本、`event/schedule` 来源、计划窗口、`logical_key/dedupe_key`、终态、1–3 attempt、重试关系、配置/动作快照、安全错误码、结果引用、开始/结束时间和有界因果字段。
- 预设定义由代码版本化；数据库只保存允许用户修改的参数，不保存可执行代码。
- `logical_key + attempt` 全局唯一；事件首轮另以 `rule_id + source_event_id` 唯一，计划首轮以 `rule_id + scheduled_for` 唯一。重放、重启和重复扫描不会产生第二个目标对象。
- 因果元数据预留 `caused_by_run_id`，数据库把深度约束在 0–4；当前三个可用预设不消费 Automation 自身事件，因此不能形成链式递归。

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
- 当前 Run 只持久化不可变终态：`succeeded / failed / skipped / cancelled`；首版同步本地动作不暴露瞬时 queued/running 事实。
- 当前 Workflow Event action：`automation_rule_updated / enabled / disabled` 与 `automation_run_succeeded / failed / skipped`。
- 自动化自身事件默认不能再次触发同一规则；确需链式处理必须由预设显式声明并受深度限制。

## 与其他模块协作

- **Workflow Event**：唯一业务触发事实，不从页面展示文本或数据库轮询猜测状态变化。
- **收件箱、任务、提醒**：v0.2 唯一允许的动作目标；写入和 Run 成功在同一事务中提交。
- **项目/发票/回访/路线图/内容日历**：各模块只发布本地事件；对应模块未上线时相关规则显示 unavailable。
- **本地 Agent**：Agent Run 可成为事件源，但自动化不能提升 Agent 能力或绕过人工验收。
- **桌面通知**：未来可由 Reminder 统一触发本应用通知；通知权限被拒绝时仍保留应用内提醒。

## 分阶段实施

1. **A1 前置事实层（已完成当前依赖）**：Workflow Event、Project 完成、Inbox 与 Reminder 动作目标均可用。
2. **A2 ADR 与预设目录（已完成首版）**：五个稳定预设、动作白名单、IANA/DST、离线折叠、三次 attempt、递归和权限边界已固化在代码与 schema v33。
3. **A3 数据/API（已完成）**：规则/Run 迁移、列表、详情、配置、启停、预览、历史和手动重试已实现。
4. **A4 事件执行器（部分完成）**：Project 完成预设已事务接通并隔离来源；发票和 Agent 事件源待对应模块交付。
5. **A5 本地调度器（已完成 daily/weekly）**：下一次运行、IANA/DST、启动/周期扫描、离线折叠与重复窗口防护已实现。
6. **A6 界面与验收（部分完成）**：React 设置、运行日志、重试、依赖不可用及自动化测试已完成；真实 WebView、休眠唤醒、系统时区变更与超长历史专项仍待验收。

## 验收标准

- 同一规则和事件无论重放、并发或重启多少次，最多产生一份目标工作项。
- 时间规则在用户时区、夏令时、休眠、跨午夜和关机补偿场景下结果正确。
- 停用后不再开始新运行；运行历史和已创建对象不被删除。
- 失败重试有上限、有新 attempt 记录，不覆盖原失败；不可重试错误不会循环。
- 当前无链式预设且 Automation 自身事件没有消费者；数据库因果深度约束已测试。未来开放链式预设前仍须补循环图的确定性测试。
- 依赖模块不可用时规则不能启用，UI 明确说明原因。
- 所有动作均落在本地白名单；测试验证不存在 HTTP、任意 Shell、外部消息或未经确认修改业务事实的路径。
- 加载、空、错误、重试、长历史分页和并发配置冲突均有测试。

## 相关 PRD 与代码链接

- [产品 PRD](../opc-workspace-PRD.md)（§5.9“自动化”、§10.7）
- [Sidecar 路由](../../services/sidecar/internal/api/router.go)
- [schema v33 自动化迁移](../../services/sidecar/internal/database/migrations/033_preset_automations.sql)
- [预设目录](../../services/sidecar/internal/api/automation_catalog.go)
- [规则 API](../../services/sidecar/internal/api/automations.go)
- [执行器与调度器](../../services/sidecar/internal/api/automation_engine.go)
- [设置界面](../../apps/web/src/components/AutomationSettings.tsx)
- [后端自动化验收](../../services/sidecar/internal/api/automations_test.go)
- [前端自动化验收](../../apps/web/src/components/AutomationSettings.test.tsx)
