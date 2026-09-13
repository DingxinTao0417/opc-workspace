# ADR-027：内置 Agent 执行器与 Run 生命周期（v0.2-B 首片）

- 状态：Accepted；Windows 内置 Runner、任务 Run 界面与 v0.2-C 条件式文本提交已实现。本次补齐显式启停与初始化门控，验证范围见末节；其他平台/external 未开放。
- 日期：2026-09-12
- 决策范围：T-19 v0.2-B；修订 [ADR-003](003-local-agent-runtime-security.md) 的启用闸门分层
- 相关文档：[本地 Agent 模块](../modules/local-agents.md)、[ADR-005](005-agent-harness-and-local-models.md)

## 背景

ADR-003 把"平台沙箱/禁网/进程树回收全部验证"设为一切 Agent 执行的硬闸门，导致 `builtin-local-text-v1` 只能停留在诊断占位。本 ADR 回答三个问题：闸门是否可以按 Adapter 信任级分层、管道协议的具体帧格式、以及第一个真实执行器如何复用已有的本地模型 Provider。

## v0.2-B 初始决策（历史边界）

以下保留本地模型首片的决策背景；在线模型、文本提交与当前初始化流程以后的修订为准。旧文中的“仅本地”“无凭据”“Web UI 待后续”不代表当前 v0.2-C 状态。

### 1. Adapter 信任分层：builtin 与 external

| 层级       | 定义                                                                                                                                                        | execution_ready 条件                                                                         |
| ---------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| `builtin`  | 与 Sidecar 同仓库编译、随应用分发的执行器；执行器模式通过同一可执行文件的保留子命令 `agent-executor` 再入（`os.Executable()` 自再执行），不新增待打包二进制 | 生命周期验证矩阵：管道协议往返、进程树回收、超时/取消、中断恢复（Windows 已实测，见第 4 条） |
| `external` | 桌面壳导入的不可变可执行对象                                                                                                                                | 维持 ADR-003 全部闸门（沙箱/禁网/进程树三平台验证）；任一未验证即永不启用                    |

依据：builtin 执行器代码与 Sidecar 同等信任级（同仓库、同构建、同签名），其网络边界由代码强制（见第 3 条）而非 OS 沙箱；external 仍不可信，闸门不放松。ADR-003 的其余边界对两层一律不变：单次匿名管道、无 WebView 令牌、无 SQLite/Shell 能力、输出只经 staging 校验。

### 2. 管道协议 `opc-agent-pipe-v1`

- 帧格式：4 字节大端长度前缀 + UTF-8 JSON；单帧上限 1 MiB；未知字段拒绝。
- 输入首帧：`protocol_version`、`run_id`、`nonce`（单次随机，仅存于父子进程内存）、`capabilities`、`input`（脱敏 Task 快照 + 指令）、`model_endpoint`（限回环 http 的 JSON 端点）、`model`、`deadline_ms`、`max_result_bytes`。
- 输出：单个 manifest 帧 `{"protocol_version":"opc-agent-pipe-v1","run_id":...,"nonce":...,"result":{"type":"text","text":...}}`，`text` ≤ `max_result_bytes`；多帧、尾随字节、超限、run_id/nonce 不匹配都使 Run 失败。
- v0.2-B 结果只内联文本；文件流协议留给 v0.2-C Artifact 接入。

### 3. builtin-local-text-v1：本地模型文本执行器

- 输入 Task 快照（title/description/status/kind）+ 固定指令模板，调用帧内指定的 OpenAI 兼容 `/chat/completions` 端点；本地 Provider `kind=local` 本就禁止密钥，帧内不含任何凭据。
- 端点约束：仅接受 `http` + IP 字面量回环（`127.0.0.0/8`、`::1`），拒绝主机名、https、非回环；执行器在拨号前二次校验，Run 创建时 Sidecar 同样校验。
- Run 创建必须引用一个 `kind=local` 且健康的 Provider（provider_id + model 显式传入）；无可用本地 Provider 时返回稳定错误。
- 默认超时 10 分钟、结果上限 64 KiB；超限即 Run 失败。

### 4. Run 生命周期与门控

- schema 071 新增 `agent_runs`（attempt、parent_run_id、input/output 快照、稳定错误码）并给 `actors` 增加可空 `agent_adapter_id`（agent Actor 必须指向 Adapter）。
- 适配器 `enable` 成功时 Sidecar 幂等创建对应的 agent Actor；Assignment 放开 agent 类型，但仅当 Actor 活跃且其 Adapter `execution_ready` 且 enabled，否则返回稳定 409。
- Run 创建原子校验：任务状态可执行、活动 Assignment 指向该 agent Actor、Actor 的 Adapter 健康；校验通过先持久化 queued 再启动进程转 running。
- 状态机 `queued → running → succeeded|failed|cancelled|interrupted`；Sidecar 启动把遗留 running 标 `interrupted`；重试总是新 Run（parent_run_id + attempt）；取消先关 stdin，宽限期后 `TerminateJobObject`。
- Windows 生命周期矩阵（本机实测，2026-09-12）：kill-on-close Job Object + `TerminateJobObject` 回收整棵进程树（含孙进程）、1 GiB 进程内存上限、管道帧往返、取消/超时终止。未验证并保持关闭：external 层的 OS 禁网与 AppContainer/restricted token；macOS/Linux 全矩阵。

## 修订（2026-09-12，v0.2-C）

1. **模型来源扩展**：Run 创建接受 `kind=local` 与 `kind=remote` 两类 Provider。本地仍强制回环 http；在线模型的运行级凭据经管道帧（`model_api_key`）从密钥库读出并仅在父子进程内存中存在，不落库、不进日志或事件；执行器禁止重定向以防凭据外带。
2. **产出语义**：执行指令要求直接产出任务交付物本身（而非交付说明）。完成条件虽已进入输入 frame，但当前 `buildPrompt` 尚未拼入该字段；这是待修复缺口，不作为本次初始化修复的完成项。
3. **产出提交（v0.2-C）**：Run 成功后由 Sidecar 经既有 manual-review 提交链创建 `TaskSubmission` + text 类型 `TaskArtifact`（producer=agent Actor，origin 保持 `manual`，来源追溯靠 producer 与 `agent_run_output_submitted` 事件），任务进入 `waiting_review`；owner 验收/返工是唯一完成路径。领域前置不满足（无 owner 审核人、任务不可提交等）时产出留在 Run 记录并追加 `agent_run_submission_skipped` 事件（仅稳定原因码）。`requireTaskOutputActors` 相应允许 agent 负责人提交。
4. **重试语义**：重试对任意终态 Run 开放（含 succeeded）；等待验收期间的新成功 Run 按领域规则跳过重复提交。

## 修订（2026-09-12，初始化与显式启停）

- 保持 app v0.1.1 / API v1 / schema 71，不新增迁移，不补开发库 seed，不在启动应用或打开页面时自动启用/分派/运行。
- 设置的登记、诊断、启用与停用分别调用真实 API；显示服务端实际健康/隔离/就绪结果，不固定写成 blocked。启用/停用使用 Adapter `If-Match`，不混入 `app_settings` 的保存流程。
- 启用事务内重读版本、健康/就绪与实际 Actor，检查条件更新结果。固定 Actor ID 冲突、inactive、关联错误或已经 enabled 却缺 Actor 返回 `409 AGENT_ADAPTER_ACTOR_CONFLICT`；不静默修复身份、不覆盖已有 Actor，失败回滚 Adapter 更新。新 Actor 只在符合条件的显式启用中创建。
- Actor 读 API 暴露可空 `agent_adapter_id`；非 agent 为 null。Web 兼容缺失字段规范化为 null，并将其视作无法证实关联而非已初始化。
- 任务页在启动前重读 Adapter/Actor/Assignment/Provider/Run：必须 Adapter enabled/healthy/execution_ready、真实 Actor type=agent/status=active 且关联相同 Adapter。缺登记、未启用、缺 Actor、不匹配或读取错误禁用启动，并提供中文原因、刷新和“设置 → 本地 Agent”路径文字指引，不通过跳转关闭未保存任务草稿。
- 启动与重试的 UI 只对 todo/in_progress 任务开放；模型选择只展示 ready/healthy 的 `openai_chat` Provider。无 assignee 时明示“启动先分派再执行”，通过共享 Assignment hook 与读取到的 Task version 提交。已有他人 assignee 不静默改派；重试不补分派，要求当前已有匹配责任人。
- 取消不受初始化/模型未就绪阻断；停用 Adapter 不等于取消活动 Run，不删除已有身份、分派或历史。
- 本次不修复完成条件提示词、运行中取消终态、结果截断或通用协议适配等其他缺口：当前执行器只支持 OpenAI 兼容请求；超长文本可能先截断；running 取消仍可能落 failed/AGENT_RUN_CANCELLED。不得借初始化补链宣称完整自主 Agent 已验收。

### 验证边界

本轮以隔离 API/组件测试验证显式启停、真实健康、Actor 字段兼容、版本与身份冲突回滚、启动前刷新、不静默改派及失败反馈。验收命令和结果随最终交付记录；未调用真实模型、未在用户库启用 Adapter/建立分派或启动 Run。历史 Windows 生命周期矩阵不冒充本轮实机测试。

## 被拒方案

1. **Sidecar 进程内直接执行**（无子进程）：失去崩溃隔离、资源上限和统一清理，Run 状态不可信。
2. **执行器复用会话令牌调用业务 API**：违反 ADR-003；能力经管道帧一次性下发。
3. **为 builtin 要求 OS 级禁网**：本地模型执行必须回环拨号，OS 禁网与之互斥；回环边界由代码校验承担，builtin 信任级等同 Sidecar 自身。
4. **独立执行器二进制**：桌面安装包需多打一个可执行文件；自再执行子命令零打包成本。

## 后果

- 优点：Windows 上首次具备真实可执行的内置 Agent 链路（含真实本地模型调用），Run 事实、事件与恢复语义完整，external 安全边界未被削弱。
- 代价：builtin 与 external 的启用语义分叉，文档必须持续区分；网络边界依赖代码纪律而非 OS 强制。
- 已有的 Web Run 界面与条件式 Submission/text Artifact 提交不再列为未来功能；后续继续完善文件流、Agent Inbox 投影、执行器已知缺口及 macOS/Linux 矩阵。external 闸门验证前其执行保持关闭。
