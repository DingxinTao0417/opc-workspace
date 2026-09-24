# ADR-027：内置 Agent 执行器与 Run 生命周期（v0.2-B 首片）

2026-09-23 AH-07 补充：[ADR-030](030-agent-run-start-gates.md) 允许人工确认的 Run 在 `queued` 状态等待一个前置 Run 满足条件后才进入 FIFO；门控是不可变工作流事件，放行后仍走本 ADR 的准入、认领与冻结身份复核，终止时经既有取消命令转为 `cancelled`。不改变执行器、管道协议或产出登记。

2026-09-23 H5-G44 并发/排队补充：单 Sidecar 限制 Task Agent Run 到最多 8 个 Worker 和 32 个额外 queued Run；共享创建事务以 `queued/running + output_delivery_status=not_ready` 做 40 个准入上限，满载拒绝并回滚。持久队列按创建时间/ID FIFO，在启动恢复及 Worker 退出时派发。模型执行结束而待登记的 `running + pending` 不占执行槽，只恢复原产出，不再次调用模型。历史队列不删除，先排空；不增加数据库结构或授权，也不承诺跨 Sidecar 配额、终态重跑或自动编排。详细验收边界见 [本地 Agent H5-G44](../modules/local-agents.md#task-agent-run-的有界-fifo-调度h5-g442026-09-23)。

2026-09-22 H5-F7 补充：Runner 仍只通过原 Submission/Artifact 链登记 file/files 产出，不直接获得项目路径或写入能力；但用户可从 `succeeded + submitted` 的完整身份结果中逐份选择一份有界 UTF-8 文件，在右侧另选现有目标并经精确基线、完整差异和 ADR-029 的按次原生 helper 链执行替换。该人工后处理不改变 Run/Submission/Artifact/Task 状态，不新增 Runner 协议、scope 或自动落盘。普通开发构建保持只读，真实 UAC/SACL/断电验收仍待，见 [本地 Agent H5-F7](../modules/local-agents.md#已提交文件产出到项目文件的人工桥接h5-f72026-09-22)。

2026-09-21 下一切片规划：用户已批准项目目录内的显式 AI 文件读取及经完整差异逐次确认的文件写入，详见 [ADR-029](029-agent-workspace-capabilities.md)。此批准不扩大当前内置 Runner 的输入输出协议，也不开放任意文件系统或 Shell；现有受控文件产出仍只登记产出，不直接改项目文件。原生目录授权、精确快照、修改审批及恢复链完成并验收前，不把规划记为已实现。

2026-09-21 H5-F6 补充：同项目已验收 Task 文件使用独立 v6 来源 proof 和管道 `read_project_task_files`，不扩大 v1–v5 的输入含义。prepare/审批/prelaunch/retry 重验源 Task/当前验收批次与实际文件；活动引用保护源文件、源 Task/Project 删除。OpenAI/Anthropic 协议、原输入输出预算和人工验收边界保持，恢复不再次请求模型。API v1/schema 076 无迁移，旧二进制不理解 v6，见 [来源契约](../modules/local-agents.md#同项目已验收文件交接h5-f62026-09-21)。

2026-09-21 H5-F5 补充：新增远程 Anthropic Messages 单次非流式路径。新 v5 规范快照冻结协议与 8192 token 产品预算，旧 v1–v4 仍为 OpenAI；鉴权分离，只有完整纯文本 end_turn 可交付，截断/拒绝/工具等安全失败。文件、返工、身份复核、人工验收及 pending 恢复沿原边界，不复用带自动重试的聊天客户端，不增加通用工具。API v1/schema 076 无迁移，执行器/Sidecar 必须同版，见 [H5-F5](../modules/local-agents.md#anthropic-受控执行h5-f52026-09-21)。

- 状态：Accepted；Windows 内置 Runner、任务 Run 界面、v0.2-C/H5-B 可恢复产出登记及 H5-C 受控文件输入/单文件与有界多文件输出已实现。显式启停与初始化门控已经补齐；任意工具访问、其他平台与 external 未开放。
- 日期：2026-09-12
- 决策范围：T-19 v0.2-B；修订 [ADR-003](003-local-agent-runtime-security.md) 的启用闸门分层
- 相关文档：[本地 Agent 模块](../modules/local-agents.md)、[ADR-005](005-agent-harness-and-local-models.md)

## 背景

2026-09-21 H5-F4 补充：为解决历史身份漂移或 retained 结果无法接续，另开受确认的当前事实 start，不放宽 exact retry。旧 Run 保持，来源十项 metadata 与新执行 hash 写入不可变 `agent_run_restarted`，与 Run 原子提交；新 Run parent 为空，attempt 按当前 Actor 序列分配。预览/创建重验当前资格及来源，文件/返工重新选择并各自确认，pending 不可重跑。API v1/schema 076、输入 v1–v4 与 Runner 协议不变，但旧代码不理解新事件时不能仅凭 schema 相同降级，详见 [重新执行契约](../modules/local-agents.md#按当前事实重新执行h5-f42026-09-21)。

2026-09-21 H5-F3 补充：内置非流式响应须明确正常完成后才可登记，截断/过滤/拒绝及不合规响应返回固定安全码。完整写出的安全失败帧是通信成功、业务执行失败，子进程正常退出让 Runner 识别原码；输入/输出协议错误、异常退出与尾随内容仍拒绝，不放宽 Runner。stderr 不携带上游原始错误，调用次数及输入 v1–v4 不变。兼容端点缺失完成原因不再接受，详见 [执行完成信号](../modules/local-agents.md#执行完成信号与安全失败原因h5-f32026-09-21)。

2026-09-21 H3-C3 补充：仅 `agent_run_failed` 的两条真实生产路径把安全失败证据与已启用预设投递捕获放入终态事务；消费者只创建本地诊断事项，不运行或重试 Agent。取消、启动中断、pending 与 retained 保持各自含义，未知错误码不外泄。活动诊断纳入 Task 删除保护、终态来源留审计；原 Run 仍不进入便携业务包。无新迁移，详见 [诊断预设](../modules/automation.md#agent-失败诊断闭环h3-c32026-09-21)。

2026-09-19 取消可靠性补充：运行中取消意图复用不可变 workflow event `agent_run_cancel_requested`，与命令事务一起落库，再发送进程内取消信号。执行器收尾前不提前释放 running 身份；各最终处置事务与启动恢复优先收敛已接受的取消，不能让迟到结果变成成功产出。取消与 pending 登记以事务提交顺序裁决，pending 先提交则拒绝取消并保留恢复路径。无需新迁移；详细边界与测试见本地 Agent 模块。

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
- 输入首帧：`protocol_version`、`run_id`、`nonce`（单次随机，仅存于父子进程内存）、`capabilities`、`input`（脱敏 Task 快照 + 指令）、`model_endpoint`（限回环 http 的 JSON 端点）、`model`、`deadline_ms`、`max_result_bytes`。父进程异步写入该唯一输入帧；即使执行器不读取或只读取部分 stdin，取消和超时仍会关闭管道、终止进程树并等待子进程退出。
- 输出：单个 manifest 帧 `{"protocol_version":"opc-agent-pipe-v1","run_id":...,"nonce":...,"result":{"type":"text","text":...}}`，`text` ≤ `max_result_bytes`；父进程在首帧后继续读取并要求 stdout 精确 EOF，再要求执行器在同一 500 ms 收尾窗口内以状态码 0 正常退出。多帧、尾随字节、超限、run_id/nonce 不匹配、非零退出或输出后滞留都使 Run 失败；稳定 `error` manifest 也只有在精确 EOF 与正常退出后才可映射为对应安全错误码。
- v0.2-B 结果只内联文本；这是历史边界。H5-C 后续以 v2 扩展受控输入和 text/file 结果、以 v3 扩展有界多文件结果，同时保留无文件文本 v1 兼容，见后续修订。

### 3. builtin-local-text-v1：本地模型文本执行器

- 输入 Task 快照（title/description/status/kind）+ 固定指令模板，调用帧内指定的 OpenAI 兼容 `/chat/completions` 端点；本地 Provider `kind=local` 本就禁止密钥，帧内不含任何凭据。
- 端点约束：仅接受 `http` + IP 字面量回环（`127.0.0.0/8`、`::1`），拒绝主机名、https、非回环；执行器在拨号前二次校验，Run 创建时 Sidecar 同样校验。
- Run 创建必须引用一个 `kind=local` 且健康的 Provider（provider_id + model 显式传入）；无可用本地 Provider 时返回稳定错误。
- 默认超时 10 分钟、结果上限 64 KiB；超限即 Run 失败。

### 4. Run 生命周期与门控

- schema 071 新增 `agent_runs`（attempt、parent_run_id、input/output 快照、稳定错误码）并给 `actors` 增加可空 `agent_adapter_id`（agent Actor 必须指向 Adapter）。
- 适配器 `enable` 成功时 Sidecar 幂等创建对应的 agent Actor；Assignment 放开 agent 类型，但仅当 Actor 活跃且其 Adapter `execution_ready` 且 enabled，否则返回稳定 409。
- Run 创建原子校验：任务状态可执行、活动 Assignment 指向该 agent Actor、Actor 的 Adapter 健康；校验通过先持久化 queued 再启动进程转 running。
- 状态机 `queued → running → succeeded|failed|cancelled|interrupted`；Sidecar 启动把遗留 running 标 `interrupted`；重试总是新 Run（parent_run_id + attempt）。取消/超时覆盖输入写入、响应读取和执行器退出等待；终止路径关闭 stdin 后回收并等待整棵进程树。
- Windows 生命周期矩阵（本机实测，2026-09-12；2026-09-18 补充阻塞写入与句柄清理回归）：kill-on-close Job Object + `TerminateJobObject` 回收整棵进程树（含孙进程）、1 GiB 进程内存上限、管道帧往返、取消/超时终止；正常退出与失败退出都会幂等移除跟踪项并关闭 Job handle。未验证并保持关闭：external 层的 OS 禁网与 AppContainer/restricted token；macOS/Linux 全矩阵。

## 修订（2026-09-12，v0.2-C）

1. **模型来源扩展**：Run 创建接受 `kind=local` 与 `kind=remote` 两类 Provider。本地仍强制回环 http；在线模型的运行级凭据经管道帧（`model_api_key`）从密钥库读出并仅在父子进程内存中存在，不落库、不进日志或事件；执行器禁止重定向以防凭据外带。
2. **产出语义**：执行指令要求直接产出任务交付物本身（而非交付说明）。本修订最初记录的完成条件提示词缺口已由后续 schema 074 / H5-A 切片补齐；当时文件/工具/多步协议不在执行契约内，其中“受控输入+单文件/有界多文件输出”已由 H5-C 修订取代，任意工具和多步协议仍未交付。
3. **产出提交（v0.2-C；由 H5-B/H5-C 收口）**：Run 产生有界结果后由 Sidecar 复用既有 manual-review 提交命令创建 `TaskSubmission` + text/file `TaskArtifact`。文件 output contract 的安全 basename/MIME 在启动前冻结；AI 入口的文件名由服务端按 attempt 生成。Submission submitter 与 Artifact producer 都是冻结的 agent Actor，Artifact recorder 和事件 Actor 是 system，origin 保持 `manual`；任务最多进入 `waiting_review`，owner 验收/返工仍是唯一完成路径。schema 075 要求成功终态同时为带精确双 ID 的 `submitted` 或带稳定原因码的 `retained`，不再使用模糊的 skipped 事件表达产出处置。文件输出仍在 `result_text` 保存同一份最多 64 KiB 的有界恢复副本，以便登记失败时幂等恢复；权威交付物是关联的 Artifact，该字段不是第二份可编辑文件。
4. **重试语义**：旧 v1–v3 原生 API 中，任意终态 Run（含 succeeded）可作为不可变新 attempt 的历史父级，但新 Run 仍重新校验当前 Task 必须为 `todo/in_progress` 及完整冻结身份；submitted 已把 Task 推进 `waiting_review` 时不能再启动，不以“跳过重复提交”伪装一次新成功。H5-F2 的 v4 返工契约收窄为 failed/cancelled/interrupted 且没有产出或待登记结果；尤其 succeeded+retained 不得重跑。返工后新业务执行使用新的 start，失败重试冻结原上下文并重新人工确认，见 [返工执行契约](../modules/local-agents.md#退回意见驱动的新执行h5-f22026-09-21)。

## 修订（2026-09-12，初始化与显式启停）

- 保持 app v0.1.1 / API v1 / schema 71，不新增迁移，不补开发库 seed，不在启动应用或打开页面时自动启用/分派/运行。
- 设置的登记、诊断、启用与停用分别调用真实 API；显示服务端实际健康/隔离/就绪结果，不固定写成 blocked。启用/停用使用 Adapter `If-Match`，不混入 `app_settings` 的保存流程。
- 启用事务内重读版本、健康/就绪与实际 Actor，检查条件更新结果。固定 Actor ID 冲突、inactive、关联错误或已经 enabled 却缺 Actor 返回 `409 AGENT_ADAPTER_ACTOR_CONFLICT`；不静默修复身份、不覆盖已有 Actor，失败回滚 Adapter 更新。新 Actor 只在符合条件的显式启用中创建。
- Actor 读 API 暴露可空 `agent_adapter_id`；非 agent 为 null。Web 兼容缺失字段规范化为 null，并将其视作无法证实关联而非已初始化。
- 任务页在启动前重读 Adapter/Actor/Assignment/Provider/Run：必须 Adapter enabled/healthy/execution_ready、真实 Actor type=agent/status=active 且关联相同 Adapter。缺登记、未启用、缺 Actor、不匹配或读取错误禁用启动，并提供中文原因、刷新和“设置 → 本地 Agent”路径文字指引，不通过跳转关闭未保存任务草稿。
- 启动与重试的 UI 只对 todo/in_progress 任务开放；模型选择只展示 ready/healthy 的 `openai_chat` Provider。无 assignee 时明示“启动先分派再执行”，把真实 agent Actor 与读取到的 Task version 作为 `auto_assign` 随创建 Run 一次提交；后端在同一事务复用 Assignment 命令并继续 prepare/create。已有匹配 assignee 的原请求兼容不变，已有他人 assignee 不静默改派；重试不补分派，要求当前已有匹配责任人。原生创建 Run 的幂等 endpoint identity 使用规范化 Task UUID，不允许同 key/body 跨 Task 重放；同 Task 的 replay/摘要冲突语义保持不变，旧模板记录仅在关联 Run 证明同 Task 时兼容读取。
- 取消不受初始化/模型未就绪阻断；停用 Adapter 不等于取消活动 Run，不删除已有身份、分派或历史。
- 初始化补链当时不处理完成条件提示词、运行中取消终态、结果超限或通用协议适配。后续 schema 074 / H5-A 已把冻结完成条件拼入提示词，running 人工取消稳定落 `cancelled`，超长结果以 `AGENT_RESULT_TOO_LARGE` 整体失败而不截断；H5-C 增加受控文件输入与单文件/有界多文件输出，H5-F5 再增加上述远程 Anthropic 单次请求。仍不提供任意 filesystem/Shell/browser/Git/terminal 工具、多步协议或完整自主 Agent。

## 修订（2026-09-18，H5-B 产出登记与启动恢复，schema 075）

- `agent_runs` 持久化 `output_delivery_status = not_ready|pending|submitted|retained`、安全原因码及 nullable `submission_id/artifact_id`。恢复正文/字节数/完成时间只保存在不出 API 的 staging 字段；触发器约束 Run 终态、双 ID、原因码、staging 和 UTF-8 字节数的合法组合，submitted/retained 的结果必须为 1–65536 UTF-8 bytes 且计数精确。迁移先修正合法旧结果的字节数；旧 succeeded 的结果若超限或非法但有不可变事件与当前关系唯一证明的 Submission/Artifact 链，则保持 `succeeded + submitted`，Run 结果改为有界说明，完整 Artifact 仍是权威产出。没有唯一链的非法旧行才转为 `failed + AGENT_RESULT_TOO_LARGE/AGENT_RESULT_INVALID`；其余合法历史只在唯一证据下回填 submitted，否则以 `AGENT_OUTPUT_DELIVERY_LEGACY` retained，绝不伪造关联。规范化由升级前破坏性迁移回滚包保护。
- 成功执行先在同一外层事务中核对 schema 074 的冻结 Task/Assignment/Actor 身份，再通过共享 Task output 命令登记产出并提交 Run。全部成功才公开 `succeeded + submitted`；Task/review/责任等领域事实已漂移时，同事务公开 `succeeded + retained`。数据库查询、事件或写入等瞬时错误回滚业务事务，并尝试把结果持久化为 `running + pending + AGENT_OUTPUT_DELIVERY_PENDING`，不能误记为 retained，也不能出现 succeeded 但无处置身份。若完成事务与 pending 首次持久化同时暂时失败，Worker 在进程存活期间只以内存保留有界正文并退避重试；首次 staging 落库后才可由重启恢复，首次落库前的进程退出/掉电仍可能丢失该内存结果。
- `POST /api/v1/agent-runs/:id/output-delivery/retry` 恢复 pending，terminal disposition 幂等回读，not_ready 拒绝。状态条件、事务和唯一索引保证重复/并发 finalize、启动恢复和人工恢复不重复创建 Submission/Artifact；pending 已完成模型执行，取消不能丢弃 staging。
- 启动恢复先尝试所有 pending 产出，重新 launch 已提交但未启动的 `queued + not_ready`，只把遗留 `running + not_ready` 标为 interrupted。pending 只恢复登记而不重跑模型；queued claim 的瞬时存储故障在同一进程有界退避重排，并在每次尝试重新经过取消、维护锁和状态 CAS，预算耗尽仍保留 queued 供重启恢复。活动 `queued/running`（含 pending）阻止 Task 硬删除；全部终态后 Task 聚合删除可级联 Run，已确认 AI 回执保留历史决定/结果标识并以缺失 live fact 的 tombstone 安全呈现。业务便携导入因 schema 075 仍只扩展被排除的操作态 `agent_runs`，显式兼容 v74→v75，并递归兼容既有允许链。

## 修订（2026-09-18，H5-C 受控文件输入与单文件输出）

1. **v1/v2 契约分层**：没有输入文件且输出为 text 的请求继续规范化为 `execution_contract_version=1` 与 `opc-agent-pipe-v1`，即使调用方显式提供 `output_kind=text` 或空 `input_file_candidate_ids` 也不产生语义相同的第二种快照。只要有受控输入或 file 输出才使用严格 v2。v2 snapshot 只冻结 Task、文件来源引用（ID/source/name/MIME/size/SHA-256）和 output contract，不保存受控 store 路径或文件正文；未知字段、格式错误或非法组合 fail closed。
2. **候选与容量**：输入仅限当前 Task 未软删的 file Artifact，以及 Task 当前 Project 未软删的 Attachment。每次最多 4 个、单个 64 KiB、总计 128 KiB，只接受无 NUL 的 UTF-8 文本文件。prepare 读取受控对象并校验身份、归属、大小/哈希；子进程 launch 前再次读取和逐字段比对。Task 改绑 Project、文件删除/漂移或来源事实不再匹配均拒绝，不静默换文件。
3. **协议暴露**：execution contract v2 仍使用 4-byte 大端长度前缀的单次 JSON 管道，wire `protocol_version` 保持 `opc-agent-pipe-v1`；仅在内存帧中携带已授权字节，不包含本机路径、数据库句柄、通用输出目录或其他工具权限。output contract 只允许 text 或一个安全 UTF-8 文本文件；返回类型必须精确匹配。当前协议不支持任意 filesystem、Shell、browser、Git、terminal、多文件输出或多步工具编排。
4. **人工确认与离机披露**：原生 Task 入口在执行确认之外要求 `confirm_file_access`，并把人工勾选绑定到当时 Provider 的 `version / config_version / kind`；创建事务在写 Run/排队事件前重读并精确比较，任一漂移以 `409 AGENT_RUN_IDENTITY_CHANGED` 整笔回滚，不创建 Run、不排队也不 launch。若请求同时为未分派 Task 携带 `auto_assign`，Task 版本、Assignment 与分派事件也处于同一事务，故该 409 不留下孤立分派或 worker；已有 assignee 的 v1/v2 创建路径不变。原生前端观察到三项身份变化或收到该 409 后清除旧文件确认，要求重新核对 local/remote 离机边界。AI 入口只在保存会话、同时授权 `work+outputs+actions+agent_execution+agent_files` 时开放候选；模型仅得到 generation+Task 绑定的 opaque ID 和安全元数据，不能提供真实文件 ID、路径、哈希、正文或 consent。`confirm_agent_execution` 与 `confirm_agent_files` 独立；local Provider 显示文件不离机，remote Provider 明确显示字节会离开设备。确认、prepare 与 prelaunch 构成三层边界，任一漂移都拒绝旧授权。
5. **输出与恢复**：text 输出继续生成 text Artifact；file 输出以冻结安全名称/MIME 生成 file Artifact，AI 文件名由服务端按 attempt 固定为 Markdown。两者都复用 H5-B 的 Submission/Artifact 人工复核事务，Run succeeded 不完成 Task。schema 075 不新增迁移：`result_text` 对 file 输出仍保存同一有界字节副本，只用于 pending 登记恢复；Artifact 是权威文件事实。receipt 只返回 Run/Task/Submission/Artifact 身份、状态和安全原因码，不回传正文、路径或哈希。
6. **重试与删除互锁**：终态 retry 沿用 parent v2 的冻结文件引用与 output contract，不从新候选重选。删除门扫描全部 queued/running Run，v1 明确不携带文件引用；v2 必须通过规范 JSON、来源种类、canonical UUID、容量/哈希、名称/MIME 和输出契约校验。v2 Run 引用的 Task Artifact 或 Project Attachment 不能软删除，包含被引用 Attachment 的 Project 不能永久删除，统一返回 `AGENT_RUN_FILE_REFERENCED`；未知 execution contract version 或非法 v2 snapshot 的引用检查 fail closed。Task 本身仍由任一 queued/running Run 的既有 `TASK_HAS_ACTIVE_AGENT_RUN` 保护。
7. **兼容与验收**：旧 v1 AI 提议没有 `provider.leaves_device` 时，确认路径按冻结 Provider 类型兼容重建；不能因为升级新增字段让既有 pending 卡永久变成 `AI_ACTION_PREVIEW_CHANGED`，也不能用兼容分支跳过任何 v2 文件确认。确定性协议/API/组件测试属于默认门禁；真实 Provider、原生桌面、网络中断、崩溃和断电窗口另行验收。

## 修订（2026-09-20，H5-C v3 有界多文件输出）

1. **契约版本**：v1 继续表示无输入的 text；v2 表示受控输入或一份 file 输出；v3 只表示 2–4 份 `files` 输出。v3 snapshot 只冻结安全 basename、MIME 与顺序，不保存路径、输入正文或结果正文；未知字段、重复（忽略大小写）名称、非法名称/MIME、少于两份或超过四份均 fail closed。wire `protocol_version` 仍是 `opc-agent-pipe-v1`。
2. **模型与恢复边界**：模型只在冻结顺序提供 UTF-8 正文，manifest 的 `result.files[]` 不能携带文件名、MIME、路径或对象；Runner 将其规范化为严格 JSON 字符串数组，聚合仍受 64 KiB 限制。该数组只作为 schema 075 的 `result_text` / pending recovery 内部载荷；恢复和 finalize 都按 v3 冻结契约重新验证，不能把畸形正文当作普通文本交付。
3. **人工验收交付**：成功在一次 H5-B Submission 事务内按冻结顺序创建多个 file Artifact；agent 仍为 submitter/producer，system 仍为 recorder/event actor。`agent_runs.artifact_id` 保持首个 Artifact 的兼容指针，追加审计事件记录全量 `artifact_ids`。任何 Run 成功或文件生成都不自动完成 Task。
4. **AI 与原生入口**：原生界面只允许从本地固定 allowlist 选择 2–4 个输出预设；AI `output_kind=files` 由服务端为 attempt 固定 Markdown/JSON 两份输出。二者继续要求既有执行确认；输入文件仍单独要求文件发送确认，v3 本身不扩大输入或电脑控制权限。精确 Run 详情只返回冻结输出元数据，界面据此逐份预览/下载。
5. **未改变的边界**：不新增迁移，schema 仍为 076；不开放任意 filesystem、Shell、browser、Git、terminal、自动多步调度或并发子代理。真实 Provider、原生桌面、网络故障、崩溃和断电恢复仍须单独验收。

### 验证边界

确定性门禁以隔离协议/API/组件测试覆盖显式启停、真实健康、Actor 字段兼容、版本与身份冲突回滚、启动前刷新、不静默改派、v1/v2 规范化、候选 scope/容量、prepare/prelaunch 漂移、双确认、local/remote 披露、文件登记恢复与删除互锁。它不调用真实 Provider，也不在用户库启用 Adapter/建立分派或启动 Run；历史 Windows 生命周期矩阵不冒充本轮实机测试。真实模型、桌面交互、父进程崩溃和断电恢复窗口仍须单独记录。

## 被拒方案

1. **Sidecar 进程内直接执行**（无子进程）：失去崩溃隔离、资源上限和统一清理，Run 状态不可信。
2. **执行器复用会话令牌调用业务 API**：违反 ADR-003；能力经管道帧一次性下发。
3. **为 builtin 要求 OS 级禁网**：本地模型执行必须回环拨号，OS 禁网与之互斥；回环边界由代码校验承担，builtin 信任级等同 Sidecar 自身。
4. **独立执行器二进制**：桌面安装包需多打一个可执行文件；自再执行子命令零打包成本。

## 后果

- 优点：Windows 上首次具备真实可执行的内置 Agent 链路（含真实本地模型调用），Run 事实、事件与恢复语义完整，external 安全边界未被削弱。
- 代价：builtin 与 external 的启用语义分叉，文档必须持续区分；网络边界依赖代码纪律而非 OS 强制。
- 已有的 Web Run 界面、条件式 Submission/text Artifact 提交和 H5-C 受控输入/单文件/有界多文件 Artifact 提交不再列为未来功能；后续继续完善任意工具以外的明确纵切、Agent Inbox 投影、自治多步编排、执行器已知缺口及 macOS/Linux 矩阵。external 闸门验证前其执行保持关闭。
