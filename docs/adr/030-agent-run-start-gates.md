# ADR-030：跨 Run 条件式延后启动（start gate）

- 状态：Accepted（2026-09-23，AH-07 首片）
- 决策范围：本地 Agent 的自治跨 Run 调度第一步；修订 [ADR-027](027-builtin-agent-executor-and-run-lifecycle.md) 的“只在人工确认时立即排队”语义
- 相关文档：[本地 Agent 模块](../modules/local-agents.md)、[AI 助手模块](../modules/ai-assistant.md)、[ADR-029](029-agent-workspace-capabilities.md)

## 背景

用户希望智能体能跨多个 Agent Run 连续工作：例如“任务 A 的产出被我验收后，再开始任务 B”。现状是每个 Run 只能在人确认的那一刻立即排队；后台续办（H5-G1）在 A 结束后只能**提出**新的 `agent_run.start` 建议，仍需要用户当时在场确认。

完整的“持久授权”（一次授权、多次无人确认地启动未知 Run）会把逐项人工确认替换成范围授权，风险过高：模型可以在授权期内自行决定启动什么。本 ADR 选择更窄的一步。

## 决策

### 1. 语义：人确认完整 Run，Sidecar 只延后“何时启动”

`agent_run.start` 提案可以附带一个前置条件：

- `start_after_run_id`：前置 Run 的精确 ID（模型须先经 `workspace_agent_runs` 读取）。
- `start_after`：`submitted`（前置 Run 成功并登记为任务提交）或 `accepted`（前置 Run 的该次提交已被人工验收接受）。
- `start_within_hours`：1–24，门控有效期，从**人工确认时刻**起算。

确认卡照常展示后续 Run 的完整冻结快照（任务、负责人、Agent、Provider、输入、输出契约），并额外展示前置 Run 的任务标题、第几次、当前状态、条件与有效期；用户必须勾选“我同意在满足条件后由系统启动这一次执行”。确认后，Run 与现在一样在同一事务中创建为 `queued`，同时追加不可变事件 `agent_run_start_gated`。**Sidecar 不会为这次授权创建任何其他 Run，也不能改变其快照。**

### 2. 状态与事实

门控事实全部是 `aggregate_type=agent_run` 的不可变 `workflow_events`，不新增表或迁移：

| 事件                            | 载荷                                          | 含义                                                            |
| ------------------------------- | --------------------------------------------- | --------------------------------------------------------------- |
| `agent_run_start_gated`         | `predecessor_run_id`、`require`、`expires_at` | 门控开启；Run 为 `queued` 但不可被派发                          |
| `agent_run_start_gate_released` | `predecessor_run_id`                          | 条件满足，Run 回到普通 FIFO                                     |
| `agent_run_start_gate_closed`   | `reason`                                      | 门控终止；若 Run 仍在排队，同事务经既有取消命令转为 `cancelled` |

关闭原因闭集：`predecessor_failed`、`predecessor_cancelled`、`predecessor_retained`、`predecessor_not_accepted`、`predecessor_unavailable`、`expired`、`run_not_queued`。

### 3. 对账规则（纯函数，服务端唯一裁决）

| 前置 Run 事实                                            | `submitted`                     | `accepted`                       |
| -------------------------------------------------------- | ------------------------------- | -------------------------------- |
| `queued` / `running`（含 `running+pending`）             | 等待                            | 等待                             |
| `succeeded+submitted`，Submission `pending_review`       | 放行                            | 等待                             |
| `succeeded+submitted`，Submission `accepted`             | 放行                            | 放行                             |
| `succeeded+submitted`，`changes_requested` / `withdrawn` | 放行                            | 关闭：`predecessor_not_accepted` |
| `succeeded+retained`                                     | 关闭：`predecessor_retained`    | 同左                             |
| `failed` / `interrupted`                                 | 关闭：`predecessor_failed`      | 同左                             |
| `cancelled`                                              | 关闭：`predecessor_cancelled`   | 同左                             |
| 前置 Run 不存在或状态组合非法                            | 关闭：`predecessor_unavailable` | 同左                             |

此外：超过 `expires_at` 一律关闭为 `expired`；后续 Run 已不在 `queued`（例如用户手动取消）则关闭为 `run_not_queued`。前置 Run 之后被重试或按当前事实重启**不**转移门控，门控始终绑定原前置 Run。

### 4. 派发与恢复

- FIFO 派发查询排除门控未结束的 Run；Worker 认领事务再检查一次，门控未结束则放弃认领（不失败、不重试）。
- 放行后仍走既有 8+32 准入、FIFO、认领前与启动前的冻结身份复核：快照在等待期间漂移时，按既有规则失败关闭为 `AGENT_RUN_IDENTITY_CHANGED`，不改用新事实。
- 对账在事实提交唤醒（Run 终态、交付恢复、人工验收、审批决定）与每 30 秒的扫描中进行，进程内串行化；Sidecar 启动时先对账再派发。维护写锁与待恢复状态下跳过。
- 等待中的 Run 计入 40 个 `queued/running + not_ready` 准入额度。

### 5. 预算与限制

- 同时开启的门控最多 4 个（全局）；超过时拒绝提案或确认，原 Proposal 保持待确认。
- 有效期 1–24 小时；前置 Run 与后续 Run 必须属于不同任务（同一任务同时只能有一个活动 Run）。
- 提出或确认时前置条件已满足、已不可能满足，或前置 Run 为非法状态时拒绝，并说明应改用普通启动。
- 首片只表达**顺序依赖**，不传递前置产出作为后续输入：后续 Run 的输入在确认时冻结。

### 6. 明确不做

- 不提供任何“范围授权”：没有被人逐项确认的 Run 永远不会启动。
- 不自动验收前置产出，不自动重试失败的前置 Run 或后续 Run。
- 不把后台续办授权、计划依赖或模型文字当作启动许可。
- 结果未知（`interrupted`）不放行。

## 影响

- 新增 `agent_run.start` 可选字段与预览字段 `start_gate`；Run 读取响应新增只读投影 `start_gate`（`predecessor_run_id`、`require`、`expires_at`、`status`、`close_reason`）。旧前端遇到新字段会严格拒绝，因此前后端须同版发布。
- 无 schema/迁移、scope、Runner wire 变化。
- 验收：确定性测试覆盖对账表每一行、预算上限、过期、重启后不误派发、认领防御与取消路径；真实 Provider 下的长链体验属专项验收。
