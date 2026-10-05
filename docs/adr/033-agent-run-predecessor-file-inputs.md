# ADR-033：门控 Run 以前置 Run 已验收的文件产出为输入

- 状态：Accepted（2026-10-04）
- 决策范围：T-19「v0.2 本地编排版」受控切片中“前置步骤产出作为后续步骤输入”的第一步；修订 [ADR-030](030-agent-run-start-gates.md) 的“不传递前置产出”，为 [ADR-027](027-builtin-agent-executor-and-run-lifecycle.md) 增加执行契约 v7。不升 app 或 API 版本，不新增迁移，`external` Runner 保持关闭
- 相关文档：[本地 Agent 模块](../modules/local-agents.md)、[AI 助手模块](../modules/ai-assistant.md)、[ADR-029](029-agent-workspace-capabilities.md)、[ADR-003](003-local-agent-runtime-security.md)

## 背景

ADR-030 让用户可以一次确认“任务 A 的产出被验收后，再启动任务 B 的这一次执行”，但只表达顺序：B 的输入在确认那一刻冻结，而那时 A 的产出还不存在。想让 B 读到 A 的文件，只能等 A 验收后再另起一次带跨任务文件的启动，用户必须在场两次且第二次要重新提议。

直接“验收即放行并带上文件”不可接受：已有约束要求文件正文的同意绑定到具体文件的 SHA-256 和具体 Provider（ADR-027），跨任务文件需要独立确认且计划依赖不传递授权（ADR-029）。确认 B 的时候文件不存在，人无法对不存在的字节给出这种同意。

## 决策

### 1. 两段人工确认，Run 行不变

- **第一段（提议与确认，文件尚不存在）**：`agent_run.start` 在门控三字段之外可以再带 `start_after_input: "predecessor_files"`。用户在确认卡上看到“延后绑定的输入”：前置任务与第几次执行、前置 Run 按契约将写出的文件名与 MIME、数量与大小上限、所选 Provider 是否离机，以及“文件现在不存在，验收后须逐份再次确认，未确认不启动、不读取、不发送”。除执行同意外必须另勾 `confirm_agent_predecessor_input`，缺失返回 422 `AGENT_PREDECESSOR_INPUT_CONFIRMATION_REQUIRED`。
- **第二段（文件存在之后）**：前置产出被人工验收接受后，门控**不**自动放行。用户在该 Run 的执行抽屉里核对每个文件的名称、MIME、大小、SHA-256 和来源批次，以及将接收它们的 Provider，勾选两项同意后确认。确认本身就是门控的放行。
- Run 在第一段确认时照常创建为 `queued`，其行与输入快照此后不再改变。两段确认之间没有任何文件引用被记录在 Run 上。

### 2. 执行契约 v7

v7 的快照是 v6 的全部字段加一个声明，且 `files` 固定为空数组：

```json
"deferred_inputs": {
  "source": "predecessor_files",
  "predecessor_run_id": "<前置 Run ID>",
  "expected_files": [{ "name": "research.md", "mime": "text/markdown" }]
}
```

`expected_files` 取自前置 Run 自己冻结的输出契约，不由模型或请求体提供。v7 不携带静态输入文件、返工上下文或 restart 关联。v1–v6 的快照字节不变（黄金测试固定）；v5/v6 解析器拒绝出现 `deferred_inputs`，v7 解析器拒绝缺少它。

### 3. 什么可以被传递

提议、确认和绑定时各校验一次，全部满足才成立：

- 启动条件必须是 `accepted`。`submitted`（未经人工验收）不能带输入声明。
- 前置 Run 冻结的输出契约是单文件或多文件。文本产出登记为文本 Artifact，不在范围内。
- 两个任务属于同一个非空项目。
- 绑定时前置 Run 为 `succeeded + submitted`，它的提交是前置任务的当前批次且已 `accepted`，文件 Artifact 未删除且已校验；集合取自前置 Run 的 `agent_run_output_submitted` 事件，数量、顺序、名称与 MIME 必须和声明完全一致。
- 深度为 1：前置 Run 自己若是仍在等待输入的 v7 Run，拒绝。
- 沿用既有上限：最多 4 个文件，单个 64 KiB，合计 128 KiB。

### 4. 事实与事件

仍然只用 `aggregate_type = agent_run` 的不可变 `workflow_events`，不新增表：

| 事件                            | v7 时的载荷                                                     |
| ------------------------------- | --------------------------------------------------------------- |
| `agent_run_start_gated`         | 原字段，另加 `input_slot`（与快照中的声明相同）                 |
| `agent_run_start_gate_released` | `predecessor_run_id`、`input_files`、`provider`、`confirmed_by` |
| `agent_run_start_gate_closed`   | `reason`；新增闭集成员 `predecessor_output_unavailable`         |

`input_files` 是被确认的文件引用（来源任务与批次、ID、名称、MIME、大小、SHA-256），`provider` 是确认时的 Provider `version`、`config_version` 与 `kind`，`confirmed_by` 是确认人的 Actor ID。事件不含文件正文和路径。带输入声明的门控只有携带这份绑定的放行事件才算放行；读取投影遇到不带绑定的放行会按损坏拒绝。

Run 读取中的 `start_gate` 增加只读 `input_slot`：`source`、`expected_files` 和 `status`。`status` 为 `awaiting_predecessor`（前置尚未被接受）、`awaiting_confirmation`（已接受，等人确认）、`bound`（已确认）或 `closed`。

### 5. 对账

对账器对带输入声明的门控从不放行。前置条件满足后它只读元数据核对产出是否仍然可用：可用则继续等待人，不可用则以 `predecessor_output_unavailable` 关闭并取消该 Run（例如验收后任务被重新打开、文件被删除、任务被移到其他项目）。前置失败、取消、仅保留、被要求返工或撤回，以及超时，沿用 ADR-030 的关闭原因。等待人确认不延长 `expires_at`：到期即按 `expired` 关闭。

### 6. 原生读取与确认端点

两个端点只在原生 API 上，模型工具无法到达：

- `GET /api/v1/agent-runs/:id/start-gate/inputs`：返回当前状态、前置任务身份、声明的文件、Provider 身份与是否离机、上限；处于 `awaiting_confirmation` 时读取候选文件并核对 SHA-256，返回文件清单、合计大小和 `fingerprint`。指纹覆盖 Run、前置 Run、每个文件引用和 Provider 身份。
- `POST /api/v1/agent-runs/:id/start-gate/inputs/confirm`：请求体为 `confirm_file_access`、`confirm_project_task_files`、`file_access_provider_confirmation{version, config_version, kind}` 和 `fingerprint`。事务内在与对账器相同的锁下重新读取并比对指纹与 Provider，检查 Adapter 仍可执行，按认领时的同一规则复核 Run 的冻结身份并重读每个文件的字节，然后写放行事件并唤醒派发。同一指纹重复确认幂等（`Idempotency-Replayed: true`）。

| 情况                                              | 响应                                 |
| ------------------------------------------------- | ------------------------------------ |
| 不是 v7 Run                                       | 404 `AGENT_RUN_INPUT_SLOT_NOT_FOUND` |
| 缺少任一同意或 Provider 确认                      | 422 `CONFIRMATION_REQUIRED`          |
| Provider 确认的取值或指纹的格式无效               | 422 `VALIDATION_ERROR`               |
| 前置尚未被接受、门控已关闭、已超时或 Run 不再排队 | 409 `AGENT_RUN_INPUT_NOT_READY`      |
| 已接受的文件不再可用                              | 409 `AGENT_RUN_INPUT_UNAVAILABLE`    |
| 文件或 Provider 在展示之后变化（指纹不符）        | 409 `AGENT_RUN_INPUT_CHANGED`        |
| Provider 版本、配置或类型与确认不符，或任务等漂移 | 409 `AGENT_RUN_IDENTITY_CHANGED`     |
| Adapter 已停用或不可执行                          | 409 `AGENT_RUN_NOT_EXECUTABLE`       |

读取或确认发现输入已不可用或已超时时会唤醒对账器，由它关闭门控并取消 Run。

### 7. 执行与互锁

- Worker 在认领和启动前把放行事件中的文件引用当作 v7 Run 的输入：再次核对绑定与声明、Provider 的 `version/config_version/kind` 与 Run 一致，并重读字节比对 SHA-256。任何不一致按 `AGENT_RUN_IDENTITY_CHANGED` 失败关闭，不改用其他文件。字节与 v6 一样以冻结文件输入进入内置 Runner 的管道，Runner wire 不变。
- 文件删除互锁把已绑定的文件视为活动 Run 的输入；尚未确认的 v7 Run 不占用任何文件。
- v7 Run 结束后**不能原样重试**（原生端点返回 409 `AGENT_RUN_NOT_RETRYABLE`，AI `agent_run.retry` 提案经同一命令以同一错误被拒绝）：那些文件只为那一次执行确认过。用户可以“按当前事实重新执行”，新执行不继承文件。界面不为 v7 Run 提供“重试”按钮。
- 带输入声明的门控计入既有的 4 个等待门控与 40 个准入额度。

### 8. AI 提议桥

- `start_after_input` 只能与完整的门控字段、`start_after: "accepted"` 同用，不能与 `input_file_candidate_ids`、`rework_submission_id`、`rework_artifact_ids` 同用。
- 本条消息必须同时具备 `agent_files` 与 `agent_project_files` 授权，重新核对旧提案时同样要求。
- 预览中的 `start_gate.input_slot` 给出 `source`、`expected_files` 与三个上限；`input_files_leave_device` 在 Provider 为远程时为真。回执中的 `start_gate` 携带 `input_slot`。
- 模型不能代替第二段确认：系统提示写明验收后须由人在执行过程里逐份确认。

### 9. 明确不做

- 不自动验收，不在验收界面顺带放行，不做任何范围授权。
- 不传递文本产出，不跨项目，不链式传递（深度 1）。
- 前置 Run 被重试或重新执行后不转移声明：需要对新的 Run 重新提议并确认。
- 不为 `external` Runner 开放任何路径。Adapter 的数据库约束仍然只允许 `kind = 'builtin'`，确认端点另以 `validateAgentRunAdapter` 拒绝不可执行的 Adapter。

## 影响

- 新增 `agent_run.start` 可选字段 `start_after_input`、同意字段 `confirm_agent_predecessor_input`、预览与读取字段 `start_gate.input_slot`、关闭原因 `predecessor_output_unavailable`、执行契约 v7，以及上面两个原生端点。旧前端遇到新字段或 v7 会严格拒绝，前后端须同版发布。
- `GET /api/v1/tasks/:id/agent-runs` 与 `GET /api/v1/agent-runs/:id` 的响应现在携带 `start_gate`。此前这两个读取只计算了投影而没有写入响应，执行抽屉的“启动条件”一栏因此拿不到数据；全局 Run 列表与 AI 回执一直携带。
- v7 详情的 `input_files` 在确认前为空，确认后为被确认的文件。
- 取消一个等待中的 Run 之后，对账器稍后才以 `run_not_queued` 关闭它的门控；这之间的读取会得到“已取消，门控仍在等待”。这个瞬时状态自 ADR-030 起就存在，而前端解析器会把它当作无效响应；上面两个读取开始携带 `start_gate` 之后，它正好落在取消后的立即刷新上。前端现在按对账规则把它读作已以 `run_not_queued` 关闭（显示为“已取消”），服务端行为不变。
- 无 schema、迁移、scope 名称或 Runner wire 变化。

## 已知限制

- 统一 Agent Inbox 投影没有为“待确认输入文件”增加专门的条目类型：等待中的 v7 Run 在那里仍显示为排队中的执行。任务详情的执行列表、执行抽屉和 AI 确认卡会显示“等待前置产出”或“待确认输入文件”，确认只能在执行抽屉里进行。
- 等待人确认的时间计入门控有效期（1–24 小时），不能延长；错过后需要重新提议。

## 验证

- 确定性测试（Go）：`TestAgentRunDeferredInputsReachTheSuccessorOnlyAfterTheyAreConfirmed`（验收后不被派发；读取与确认；模型请求中出现被确认的文件字节）、`…CloseWhenTheAcceptedFilesAreNoLongerAvailable`（重新打开、移出项目、删除文件）、`…AreLockedAndRecheckedAfterConfirmation`（删除互锁、指纹不符、幂等重放、Provider 漂移）、`TestAgentRunDeferredInputProposalBoundaries`（各类拒绝与同意缺失）、`TestAgentRunDeferredInputsNeedAnExecutableAdapterAndOneStep`（Adapter 停用、深度 1、不可重试、数据库拒绝 `external`）、`TestAgentRunSnapshotContractsKeepTheirBytes`（v1–v7 快照字节）。
- 确定性测试（前端）：`start_gate.input_slot` 与门控状态一致性、v7 详情、读取与确认端点的严格解析与请求体、提议与回执的严格解析、确认卡的独立同意、生命周期文案、执行抽屉确认面板的各个状态（读取中、前置未验收、读取失败可重试、已关闭、待确认需两项勾选、确认被拒后清空勾选并重读、已确认）、v7 不提供重试。
- 真实 Provider 下实际收到的字节与离机披露、长时间等待与重启后的行为、桌面 WebView 中的确认交互，属于真机专项验收，见 [PRD 10.5.4 真机验收清单](../opc-workspace-PRD.md#1054-真机验收清单acc-01全部待验收) 的 A9，不由确定性测试代替。
