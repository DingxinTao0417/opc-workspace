# ADR-003：本地 Agent Runtime 安全与传输边界

- 状态：已接受，执行实现仍受发布闸门约束
- 日期：2026-08-29
- 对应任务：T-19 v0.2-A
- 当前代码基线：app v0.1.0 / API v1 / SQLite schema v33

## 背景

opc-workspace 需要让本地执行器处理已明确分派的 Task，并把产出送入现有 Submission/Artifact/owner review 链路。执行器不是通用聊天助手，也不能获得 WebView 会话、SQLite、任意 Shell、任意目录或外部发送能力。

本 ADR 只冻结后续实现必须遵守的边界。它不代表仓库已经交付 Agent Adapter、Agent Run 或可执行 agent Actor。

## 决策

### 1. Runtime 不开放 HTTP 路由

- owner 仍通过普通 `/api/v1/tasks/:id/agent-runs` 创建 Run；该路由属于 WebView 业务 API，并继续使用当前短期会话令牌、Origin 和 request ID 保护。
- Adapter Runtime 不监听端口，也不提供 `/api/v1/agent-runtime/*`。Go Sidecar 为每个 Run 启动一个短生命周期子进程，通过 Sidecar 创建的匿名 stdin/stdout 管道交换单次协议帧。
- 匿名管道本身是本次 Run 的能力通道。Runtime 不接收 WebView Bearer Token，不在命令行、环境变量、SQLite、日志或 manifest 中保存可复用令牌。
- stderr 只作为受限诊断流读取，按字节上限截断并映射为固定错误码；不把原文直接返回 UI 或写入业务事件。

### 2. 每个 Run 一个进程和一个协议会话

- 不复用常驻 Adapter 进程，不跨 Run 共享 stdin/stdout、工作目录、环境或能力。
- Sidecar 先持久化 queued Run，再完成运行前复检；复检通过后才创建进程并转换为 running。
- 输入使用 UTF-8、长度前缀 JSON frame，首帧包含固定 `protocol_version=opc-agent-pipe-v1`、Run ID、不可猜测的进程内 nonce、允许能力、资源 ID、字节/文件/时间上限和脱敏 Task 快照。
- 输出只接受一个 manifest frame 及随后严格计数的文件流。未知字段、重复 frame、额外尾随字节、超限、非法 UTF-8、路径、类型或哈希会使 Run 失败。
- nonce 只存在于父子进程内存和匿名管道中，用于把输出绑定到本次进程；进程退出或 Run 终止后立即失效，不是 HTTP 凭据。

### 3. Adapter 注册不接受任意命令

- manifest 入口只能引用代码所有的 Adapter key 或桌面壳明确导入到应用受控 Adapter store 的不可变 executable object；WebView API 不接受绝对路径、命令字符串、参数模板、工作目录或环境变量。
- 首个实现阶段只登记代码所有的内置 manifest。外部 Adapter 导入必须另行增加桌面文件选择、包签名/哈希、受控复制和信任确认，不复用普通浏览器 file path。
- manifest 能力只能来自版本化枚举；首批允许规划为 `read_task_snapshot`、`read_allowed_artifact`、`write_text_artifact`、`write_structured_artifact`、`write_file_artifact`。没有 Shell、SQL、HTTP、网络、业务删除、客户沟通、发票发送或付款确认能力。
- manifest、Adapter 状态和健康结果可进入 SQLite/业务导入导出；密钥、Token、完整任务正文、产出正文和机器绝对路径不得进入。

### 4. 资源与文件授权使用业务 ID

- 输入只携带 Task/Project/Artifact 等 canonical 资源 ID 和 Sidecar 生成的最小快照，不携带 SQLite 路径或任意本机路径。
- 允许读取的既有文件由 Sidecar 验证数据库归属、active 状态、普通文件、size 和 SHA-256 后复制或只读映射到本次 Run 的隔离输入目录。
- 输出只能写入 Sidecar 创建的本次 Run staging 目录。Runtime 不知道最终 Artifact root；Sidecar 重新检查文件名、普通文件、链接/reparse、数量、MIME、size 和 SHA-256 后，才通过现有 Artifact store 无覆盖提升。
- Runtime 返回的相对文件名不得含分隔符、`.`/`..`、绝对路径、驱动器、ADS、NUL 或平台保留名。

### 5. 平台隔离是启用执行的硬闸门

Adapter 可以登记和接受健康诊断，但只有协议、可执行文件、manifest、资源限制、进程隔离和禁网全部为 verified 时才可 `execution_ready=true`、创建 agent Actor、被分派或启动 Run。

| 平台    | 必须验证的最小机制                                                              | 当前状态 |
| ------- | ------------------------------------------------------------------------------- | -------- |
| Windows | 受限 Token/AppContainer 方案、Job Object 子树回收、低权限工作目录、出站网络阻断 | 未验证   |
| macOS   | 签名/公证与子进程沙箱方案、受控容器目录、网络阻断、进程组清理                   | 未验证   |
| Linux   | user/mount/network namespace、seccomp、资源限制、进程组/cgroup 清理             | 未验证   |

任何一项无法验证时：

- Adapter 保持 `diagnostic_only` 或 disabled；
- 不创建可分派 agent Actor；
- Assignment API 继续拒绝 agent；
- Run 创建返回稳定的依赖不可用错误；
- UI 必须展示具体缺失闸门，不显示“可执行”或模拟成功。

不能只依赖 Adapter 自报“禁网”，也不能把应用层没有主动发请求当成操作系统隔离已经成立。

### 6. 状态、取消和恢复

- `queued → running → succeeded|failed|cancelled|interrupted` 为唯一主状态链；终态不可修改。
- succeeded 只表示输出已校验并通过现有 D2 领域服务形成 Submission/Artifact，Task 最多进入 `waiting_review`，不能直接进入 done。
- 取消先关闭输入并发出协议级 cancel；宽限期后只终止精确 Run 进程及其已验证子树。无法确认进程树清理时 Run 不能标记为安全成功。
- Sidecar 启动把遗留 running Run 标为 interrupted，不自动重试。重试总是创建新 Run，以 parent Run/attempt 保留历史。
- 可能产生副作用的动作不提供能力，因此当前不存在静默副作用重试。

### 7. 事实、审计和隐私

- Adapter、Run、Assignment、Submission、Artifact 和 Workflow Event 各自保存自己的事实，不复制 Task 完成状态。
- Adapter 注册/启停/健康、Run 排队/启动/终态、产出提交和 owner 审核都写追加式事件；并发写使用 version/`If-Match`，可重放创建使用 `Idempotency-Key` 快照。
- 日志只记录 request ID、Adapter ID、Run ID、阶段、耗时和白名单错误码。Task/Artifact 正文、管道 frame、nonce、路径、环境和 stderr 原文不进入普通日志或诊断包。
- 业务导出可包含 Adapter/Run 元数据和安全错误码；机器 executable object、运行 staging、nonce、会话令牌和原始日志不进入。

## 被拒绝的方案

- **Runtime HTTP + Bearer Token**：增加端口、Origin、令牌泄漏和跨 Run 复用面，不采用。
- **复用 WebView Token**：会赋予 Runtime 普通业务 API 权限，明确禁止。
- **任意 executable path / command line**：等价于远程 Shell，本阶段禁止。
- **常驻 Adapter daemon**：扩大跨 Run 状态、凭据和清理边界，首版不采用。
- **仅靠提示词或 Adapter 自律禁网**：无法验证，不构成安全边界。
- **Run succeeded 直接完成 Task**：绕过 owner review 和既有 D2 事实，禁止。

## 实施顺序

1. schema v34 先交付 Adapter 注册/诊断事实、严格 manifest、启停与健康 API；当前平台隔离未验证时 execution_ready 必须为 false。
2. 建立 Windows/macOS/Linux 的可重复隔离探针和失败测试，再允许对应 profile 进入 verified。
3. schema v35 及后续交付 Run 状态、匿名管道 Runner、取消/超时/interrupted 和单次 staging。
4. 复用现有 submit-output/review 领域服务接入 Artifact 和 owner review；不得复制一套验收逻辑。
5. 完成真实进程树、禁网、崩溃、休眠、安装包和三平台门禁后，才开放正式 agent Assignment。

## 验收闸门

- 普通 WebView Token 无法被 Runtime 使用，Runtime 不存在可访问的 HTTP 监听端口。
- 未验证隔离的 Adapter 可以诊断但不能被启用、分派或运行。
- 输入/输出越权、路径穿越、链接逃逸、超限、协议尾随和 nonce 不匹配都失败且无半成品 Artifact。
- 取消、超时、Sidecar 崩溃和桌面退出不会遗留已授权进程或把 running 静默改为 succeeded。
- Task 只有 owner 接受 current pending Submission 后才进入 done。
- SQLite、日志、诊断包、命令行、环境和前端状态不包含会话令牌、nonce、原始路径或敏感正文。

## 影响

该决策牺牲了快速接入任意本地工具的便利，换取可验证的单次能力边界。T-19 可以先交付 Adapter 诊断与数据契约，但在平台隔离矩阵通过前，仓库仍应明确报告“没有可执行 Agent”。
