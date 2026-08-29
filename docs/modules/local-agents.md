# 本地 Agent 执行模块

> 文档状态：T-19 v0.2-A Adapter 登记与安全诊断已完成；目标版本为 v0.2。当前仓库仍未实现 Runner、Run 或任何可执行 Agent。

## 定位与边界

本模块负责让已注册的本地执行器在明确的任务、当前分派和最小能力范围内完成一次受控执行，并把产出送回任务验收链路。它是任务执行基础设施，不是通用聊天助手，也不是可以自由操作电脑的自治代理。

必须遵守以下边界：

- 完全在本机运行，不依赖线上模型、远程 Agent、云同步或多人账号。
- v0.1 只交付 owner、person、system 的人工编排；实际 agent Actor、Adapter 和 Agent Run 到 v0.2 才启用。
- Agent 只能执行当前活动 Assignment 指向自己的任务，不能领取未授权任务。
- Agent Run 的 succeeded 只表示本次执行产生了可读取的输出，不表示任务已经完成。
- Agent 任务强制使用 manual 验收策略，不能配置为 none；成功后进入 waiting_review，只有 owner 可以验收为 done。
- 不允许任意 Shell、SQLite 直连、任意目录访问或复用 WebView Bearer Token。
- 删除业务数据、对外发送消息或发票、确认付款等高风险动作不能委托给 Agent。
- “本地禁网”必须在目标平台通过进程沙箱与网络阻断验证；无法强制隔离的平台可以登记 Adapter 供诊断，但正式执行保持禁用并明确原因。
- 面向用户的 AI 助手与知识库是独立后续模块，不得绕过本模块的能力、分派、产出和验收约束。

## 当前实现状态

当前状态为**v0.2-A 诊断纵切已完成、执行能力未开始**：

- [ADR-003](../adr/003-local-agent-runtime-security.md) 已冻结首版 Runtime 边界：每个 Run 一个短生命周期子进程；Sidecar 匿名 stdin/stdout 管道是唯一能力通道；不开放 Runtime HTTP、不传 WebView Token、不接受任意命令/路径；资源只用业务 ID 和单次 staging；各平台进程沙箱与禁网未验证时 `execution_ready=false`，不得创建可分派 agent Actor 或启动 Run。

- 当前 SQLite schema v35；其中 schema v34 新增空的 `agent_adapters`，保存代码所有清单、协议、启停、诊断、隔离和 `execution_ready`，schema v35 仅新增 Client Followup，不改变 Adapter 契约。迁移不创建 Adapter、agent Actor、Assignment 或 Run。身份字段不可变，未达到 healthy + verified 时数据库拒绝 enabled，诊断观察不递增用户版本。
- Sidecar 已提供 Adapter 列表、幂等登记、详情、手动诊断、启用拒绝和停用 API。唯一预设为 `builtin-local-text-v1`；响应不暴露 `executable_ref`。当前诊断只验证代码清单并记录 `blocked / PLATFORM_ISOLATION_UNVERIFIED`，不会启动进程。
- 设置新增独立“本地 Agent”模块和命令面板入口，覆盖加载、空、错误、登记、能力/安全闸门、诊断反馈与禁用启用入口；当前没有 agent 负责人选项、Run 详情、输出预览或 Agent 验收入口。
- 业务 JSON/ZIP 导入导出包含 Adapter 行，并严格接受代码所有身份、disabled、version=1、`execution_ready=false` 以及 unknown 或固定 blocked 诊断状态；普通备份随 SQLite 自动包含该事实，恢复后仍必须重新诊断。
- 当前 API 仍明确拒绝 agent assignee，仓库没有 `agent_runs`、Runner、超时、取消、重试、中断恢复、Agent 专用鉴权或单次能力协议实现。未来 Agent 必须复用已交付的 Submission/Artifact 验收领域命令，不能另建绕过 owner 的完成路径。
- Tauri 当前只管理 Go Sidecar，没有额外 Agent 子进程的生命周期与隔离策略。

因此，界面在没有已注册且健康的本地 Adapter 时必须隐藏或禁用 agent 分派，不能用占位 Actor 暗示功能已经可用。

## 目标功能

### Adapter 注册与健康

- 当前只登记代码内置清单；用户文件选择和外部可执行文件注册必须在独立威胁评审后再决定，不属于 v0.2-A。
- 保存稳定 adapter_key、展示名称、版本、输入输出协议、声明能力和平台要求。
- 支持启用、停用、手动健康检查和运行前强制复检。
- Adapter 可先注册供诊断；只有健康、manifest 兼容且平台隔离条件满足时，才能创建可执行 agent Actor 和接受分派。
- 敏感凭据不得写入 manifest、普通 SQLite、日志、命令行或前端状态。

### 能力与资源授权

- 为每类 Adapter 定义可枚举的能力白名单，不接受任意命令字符串。
- 每次 Run 固化脱敏输入快照、允许读取的资源 ID、允许写入的受控 Artifact 目录和资源上限。
- Sidecar 为单次 Run 发放短时、不可复用、可撤销的能力令牌，或通过受控进程管道传输等价能力。
- Agent Runtime 不开放 HTTP；每个 Run 使用 Sidecar 创建的短生命周期子进程和匿名 stdin/stdout 管道，普通业务 API 不接受进程内 nonce，Agent 也不能获得 WebView 令牌。
- 路径授权使用规范化后的受控引用，防止路径穿越、符号链接逃逸和越权读取。

### Run 生命周期

- owner 从任务或收件箱详情启动一次 Run。
- Sidecar 原子校验任务状态、当前活动 Assignment、Agent Actor、Adapter 健康和能力范围。
- 支持排队、运行、超时、用户取消、结构化失败和应用异常后的 interrupted 恢复。
- 重试永远创建新的 Run，并通过 parent_run_id 与 attempt 保留完整历史。
- 对可能产生副作用的步骤不做静默自动重试。

### 产出与验收

- 文本、结构化结果和文件都登记为 Task Artifact。
- 文件先写入应用控制的临时位置，校验大小、类型和 SHA-256 后再原子移入受控目录。
- 产出记录实际 produced_by_actor_id、录入者、Agent Run、任务和创建时间。
- Run succeeded 后提交产出并把任务推进到 waiting_review。
- owner 可以接受、要求返工、阻塞或取消；返工后再次执行会产生新的 Run 和新产出，旧记录不可覆盖。
- 只有项目交付类产出或 owner 显式标记 requires_followup 的产出可以幂等触发新的收件箱项，避免递归制造工单。

### 可观测性与恢复

- Run 详情显示输入摘要、能力范围、时间、输出清单、结构化错误和审计时间线。
- Sidecar 或应用重启后，将遗留 running Run 标记为 interrupted，不得静默判定成功。
- 支持取消时的宽限期和精确子进程终止，退出后不得遗留 Agent 进程。
- 日志只记录 request ID、Run ID、阶段、耗时和错误码，不记录令牌、完整任务正文或产出正文。

## 关键用户流程

### 首次配置本地 Agent

1. owner 在设置的“本地 Agent”页登记代码所有的内置诊断适配器。
2. 应用展示稳定 manifest、协议、请求能力和三个安全闸门；请求不携带文件路径或命令。
3. owner 点击检查，Sidecar 以 `If-Match` 重新校验代码清单并保存脱敏诊断。
4. 当前平台隔离尚未验证，结果固定为 blocked，Adapter 保持 disabled，不创建 agent Actor 或 Run。
5. 未来只有平台隔离全部验证后，才进入创建 agent Actor、运行前复检和 Run 流程；历史健康结果不能替代当前保证。

### 从任务启动执行

1. owner 为任务设置完成条件和 manual 验收策略。
2. owner 将任务分派给一个健康的 agent Actor。
3. owner 点击“启动 Agent”，确认本次输入、授权资源和允许的输出范围。
4. Sidecar 创建 queued Run，校验后启动本地执行器并进入 running。
5. 执行器提交产出；Sidecar 校验、登记 Artifact，并将 Run 标记为 succeeded。
6. 任务进入 waiting_review，owner 查看差异和证据。
7. owner 验收通过后任务进入 done；若要求返工，则任务回到 in_progress，下一次执行创建新 Run。

### 失败、取消与恢复

1. 超时或运行错误将 Run 标记为 failed，并保留结构化错误。
2. 用户取消时先请求受控终止，超过宽限期后终止精确子进程，Run 标记为 cancelled。
3. 应用异常退出后，下次启动把遗留 running Run 标记为 interrupted。
4. owner 查看已有产出和可能的副作用后决定是否重试；重试不覆盖原记录。

## 数据/API/状态与事件

### 规划数据

| 对象             | 关键事实                                                                                                                                                         |
| ---------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| actors           | type=agent、状态、Adapter 引用和能力摘要；历史引用后只能停用                                                                                                     |
| agent_adapters   | **schema v34 已实现**：稳定代码所有标识、内部执行器引用、manifest、启停、隔离、就绪和最近脱敏诊断；当前不保存路径或凭据                                          |
| task_assignments | Task 当前 assignee；启动 Run 时必须指向同一个 agent Actor                                                                                                        |
| agent_runs       | 一次执行的任务、分派、Agent、输入快照、状态、尝试次数、输出摘要和错误                                                                                            |
| task_artifacts   | 当前 D2 已有受控文本/文件/链接/结构化产出、相对路径、SHA-256 与 producer/recorder；未来 Run 来源通过新增显式关联或 Workflow Event 表达，不回写已发布的 schema v9 |
| workflow_events  | 注册、分派、启动、取消、失败、产出、验收和返工的追加式审计                                                                                                       |

敏感凭据和单次能力令牌不进入上述表。

### 当前与规划 API

| 方法与路径                              | 用途                                              |
| --------------------------------------- | ------------------------------------------------- |
| GET / POST /api/v1/agent-adapters       | **已实现**：查询或幂等登记代码内置 Adapter        |
| GET /api/v1/agent-adapters/:id          | **已实现**：详情与数值 ETag，不返回内部执行器引用 |
| POST /api/v1/agent-adapters/:id/check   | **已实现**：版本化诊断；当前固定返回隔离未验证    |
| POST /api/v1/agent-adapters/:id/enable  | **已实现拒绝路径**：未就绪时返回 409，不改变事实  |
| POST /api/v1/agent-adapters/:id/disable | **已实现**：版本化停用；当前 disabled 时稳定返回  |
| GET / POST /api/v1/tasks/:id/agent-runs | 查询历史或由 owner 启动 Run                       |
| GET /api/v1/agent-runs/:id              | 查看 Run、输出和错误                              |
| POST /api/v1/agent-runs/:id/cancel      | 取消当前 Run                                      |
| POST /api/v1/agent-runs/:id/retry       | 基于旧 Run 创建新尝试                             |
| POST /api/v1/tasks/:id/review           | owner 接受产出或要求返工                          |

Agent Runtime 的传输、令牌撤销、Origin 处理和进程管道协议已由 [ADR-003](../adr/003-local-agent-runtime-security.md) 冻结为单次匿名管道方案；实现仍必须通过平台隔离、禁网、进程树清理和协议验收，不能仅凭 ADR 标记为可执行。

### 状态与事件

Agent Run 状态为：

| 状态        | 含义                       | 允许的后续                                |
| ----------- | -------------------------- | ----------------------------------------- |
| queued      | 已创建，等待最终校验或资源 | running、failed、cancelled                |
| running     | 本地执行器正在运行         | succeeded、failed、cancelled、interrupted |
| succeeded   | 输出已校验并登记           | 不直接改变为任务 done                     |
| failed      | 执行失败                   | owner 可创建新重试                        |
| cancelled   | owner 已取消               | owner 可按需创建新重试                    |
| interrupted | 进程或应用异常中断         | owner 检查后决定是否重试                  |

当前已写 `agent_adapter_registered / agent_adapter_health_checked`。未来关键 Workflow Event 还至少包括 agent_adapter_disabled、agent_assigned、agent_run_queued、agent_run_started、agent_run_succeeded、agent_run_failed、agent_run_cancelled、agent_run_interrupted、task_output_submitted、task_review_accepted 和 task_rework_requested。

所有创建、取消、重试和验收写入都使用幂等键与 expected_version；版本冲突返回 409，不覆盖较新的分派或验收。

## 与其他模块协作

- [任务](tasks.md)：Task 是执行目标和完成状态的唯一事实源；Agent 不能直接修改为 done。
- [Actor 与分派](actors.md)：只有当前活动的 agent Assignment 才能启动 Run；改派后旧 Agent 不再获得新能力。
- [收件箱](inbox.md)：展示 Run 进度、失败与待验收；已有活动 Inbox Item 时只更新时间线，不重复创建验收项。
- [项目](projects.md)：项目交付类 Artifact 可触发后续拆分工单，但必须使用 source_event_key 去重。
- [设置](settings.md)：维护 Adapter、Agent Actor、能力范围、健康和停用；敏感配置走操作系统安全存储。
- [数据管理](data-management.md)：备份必须覆盖 Adapter 注册、Run 元数据、Artifact 和审计；恢复后重新校验执行文件与平台能力。
- [桌面平台](desktop-platform.md)：提供文件选择、受控进程、沙箱、取消、退出清理和本地日志能力。
- 通知：只发送应用内或原生本地状态提醒，不向外部人员或服务发送任务。

## 分阶段实施

### 前置条件：v0.1 人工编排闭环

- 在已交付 Task 六状态/乐观锁之上完成 Artifact、manual 提交验收与返工。
- 复用已完成的 Actor 与 Assignment；继续完成 Artifact、受控 Workflow Event 时间线与收件箱人工跟进。
- 完成基础备份恢复、日志与 Sidecar 故障恢复。

### v0.2-A：安全与 Adapter 契约

- [x] 编写 Adapter、专用传输、单次管道能力、路径授权、沙箱和网络阻断 ADR。
- [x] 完成 schema v34 Adapter 表、代码所有 manifest、列表/幂等登记/详情/停用/健康诊断和未就绪启用拒绝 API。
- [x] 设置增加本地 Agent 模块，不能验证的隔离能力明确标注并保持执行关闭。
- [ ] 在 Windows、macOS、Linux 建立并验证实际沙箱/禁网/进程树能力矩阵；当前三个闸门均未验证。

### v0.2-B：Runner 与生命周期

- 实现 Agent Run 迁移、状态命令、任务 Assignment 校验和幂等。
- 实现受控子进程启动、输入快照、超时、取消、精确清理和 interrupted 恢复。
- 增加 Run 列表、详情、错误和健康不可用状态。

### v0.2-C：产出、验收与收件箱

- 接入受控 Artifact 目录、SHA-256、大小和路径校验。
- Run 成功后统一走 submit-output 与 waiting_review。
- 接通 owner 验收、返工、失败提醒、项目后续工单和完整时间线。

### v0.2-D：跨平台硬化

- 在 Windows、macOS、Linux 分别验证权限、禁网边界、取消、崩溃和孤儿进程。
- 补充资源上限、诊断日志、备份恢复和升级兼容测试。
- 只有全部发布闸门通过后，才在正式界面启用可执行 Agent。

## 验收标准

- 断开外部网络后，已配置的本地 Adapter 仍能完成允许范围内的执行。
- 未注册、不健康、已停用或隔离条件不满足的 Adapter 不能被分派或启动。
- Agent 令牌不能调用普通业务 API，WebView 令牌不能冒充 Agent Runtime。
- Agent 无法直接访问 SQLite、未授权目录、任意 Shell 或未声明能力。
- 启动 Run 时 Assignment、Actor 和 Adapter 不一致会被拒绝。
- succeeded Run 只把任务推进到 waiting_review；未经 owner 验收不能进入 done。
- 返工和重试创建新 Run，历史输入、输出、错误和 Artifact 完整保留。
- 取消、超时、Sidecar 崩溃和应用退出均不遗留运行进程；遗留记录正确标记为 interrupted。
- 文件产出保存在受控目录，路径、大小、类型和 SHA-256 可验证。
- 幂等重放不重复启动执行或创建产出；并发旧写入返回 409。
- 日志、数据库、进程参数和前端状态中不出现会话令牌或单次能力令牌。
- 若某平台无法强制禁网或沙箱隔离，可保留禁用的 Adapter 诊断信息，但正式 Agent 执行、分派和启动入口保持关闭。

## 相关代码/PRD链接

- [PRD：收件箱与本地工作编排中心](../opc-workspace-PRD.md#56-收件箱与本地工作编排中心)
- [PRD：本地工作编排数据表](../opc-workspace-PRD.md#本地工作编排数据表taskactord2-已实现inboxagent-仍规划)
- [PRD：T-19 本地 Agent 执行](../opc-workspace-PRD.md#10419-t-19-本地-agent-执行)
- [PRD：API 约定](../opc-workspace-PRD.md#c-api-约定)
- [当前 Sidecar 路由](../../services/sidecar/internal/api/router.go)
- [schema v34 Agent Adapter 迁移](../../services/sidecar/internal/database/migrations/034_agent_adapters.sql)
- [Agent Adapter API](../../services/sidecar/internal/api/agent_adapters.go)
- [Agent Adapter API 测试](../../services/sidecar/internal/api/agent_adapters_test.go)
- [本地 Agent 设置模块](../../apps/web/src/components/AgentAdapterSettings.tsx)
- [本地 Agent 设置测试](../../apps/web/src/components/AgentAdapterSettings.test.tsx)
- [当前 WebView 鉴权与 Origin 中间件](../../services/sidecar/internal/api/middleware.go)
- [当前 Tauri Sidecar 生命周期](../../apps/desktop/src-tauri/src/sidecar.rs)
- [ADR-003：本地 Agent Runtime 安全与传输边界](../adr/003-local-agent-runtime-security.md)
