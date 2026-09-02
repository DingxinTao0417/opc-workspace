# ADR-004：AI 助手远程 Provider 接入与安全边界

- 状态：已接受（AI 助手首个纵向切片实施）
- 日期：2026-09-01
- 对应任务：AI2 评审 + AI3/AI4 会话纵切（[计划](../plans/ai-assistant-mvp.md)）
- 明确授权：用户 2026-09-01 授权首个版本以 API key 配置式接入远程大模型，支持多协议（首批 `openai_chat` / `anthropic_messages`），不做本地部署模型适配；任务部分采用语义识别 + 建议卡片待确认
- 当前代码基线：app v0.1.0 / API v1 / SQLite schema 052+053（本纵切新增）

## 背景

AI 助手模块此前文档边界为"仅评估本地模型，不接入线上模型服务"。用户明确授权远程 Provider 后，本 ADR 固化远程接入的数据外发、密钥、语义建任务、失败与撤销边界。它不赋予模型任何业务写能力，也不改变既有模块状态机。

## 决策

### 1. 远程 Provider 协议适配器

- Sidecar 定义运行时无关的协议映射器，首批支持 `openai_chat`（OpenAI chat/completions 流式）与 `anthropic_messages`（Anthropic messages 流式）；映射器注册表可扩展。
- Provider 由用户配置：名称、协议、base URL、模型名、API key；配置存 SQLite，密钥存操作系统安全存储。

### 2. 数据外发边界

- 本版仅外发该次会话的用户输入与该次模型回复；不读取未选择数据、不外发整库、无知识库、无遥测/用量上报。
- 任务正文不经过模型：模型只输出文本建议与结构化建议块（标题/描述/截止），任务字段由用户确认后在既有任务 API 落地。

### 3. 语义建任务与人工确认

- 系统提示词由 Sidecar 代码所有（常量），指示模型在识别到任务意图时于回复末尾输出结构化块：`[opc:task]{title,description?,due?}[/opc:task]`（标题必填，描述/截止尽力推断）。
- 前端把结构化块解析为**可编辑待确认建议卡片**；缺标题时确认不可用；卡片可丢弃。
- **不设自动创建路径**：确认后由既有 `POST /api/v1/tasks` 领域 API 创建（新建固定 `todo`，任务模块校验原样生效）；创建成功后消息挂静态引用（task_id + task_title_snapshot），点击跳转 `tasks/:taskId`。
- 结构化块缺失/非法时降级为纯文本回复，不创建任务；模型输出视为不可信预览，不直接成为业务事实。

### 4. 密钥边界

- API key 仅存操作系统安全存储（首版 Windows 凭据管理器），Sidecar 进程内读入、用后即弃。
- 密钥不进 SQLite、`localStorage`、日志、命令行、诊断包或任何前端持久化；access log 不记录密钥请求体（operationlog 把密钥头注册为 secret 脱敏）。
- 平台无可用安全存储（如 Linux 无 Secret Service）时：登记/健康/聊天返回稳定不可用错误（503），明确拒绝，**不落盘退化**。首版验证平台为 Windows；macOS/Linux 作为平台门禁后续补齐。

### 5. 失败边界与资源预算

- Provider 不可用、超时、限流、无密钥 → 稳定大写错误码（`AI_PROVIDER_NOT_FOUND / AI_ENDPOINT_INVALID / AI_PROVIDER_BUSY / AI_STREAM_ERROR / AI_GENERATION_TIMEOUT / AI_KEY_UNAVAILABLE` 等）。
- 取消与 WebView 断链必须终止上游 HTTP 请求；取消保留已生成部分并标记 cancelled（incomplete）。
- 预算：首 token 90 秒、总时长 10 分钟、响应 1 MiB、提示 64 KiB；每 Provider + 每会话最多 1 个活跃生成，忙时 409 `AI_PROVIDER_BUSY`；Sidecar 启动把遗留 streaming/queued 标记为 cancelled。
- AI 不可用、未配置或功能关闭时，所有既有核心模块必须完全可用。

### 6. 撤销

- 删除 Provider 配置即撤销接入（同事务/同流程清理安全存储密钥条目）。
- 已建任务按任务模块既有 `cancel` 命令撤销；AI 助手侧不做 undo，不触碰任务状态机。

### 7. 事件与日志

- 追加式 Workflow Event（脱敏元数据，不记录提示/回答/任务正文）：`ai_adapter_registered / ai_adapter_health_checked / ai_adapter_removed / ai_generation_started / ai_generation_completed / ai_generation_failed / ai_generation_cancelled`；任务创建各自使用既有 `task_created` 事件，AI 侧不重复写。
- 普通日志只记录 provider/generation ID、阶段、耗时与白名单错误码。

## 被拒方案

- **本地部署模型适配**：用户显式延后，本版不做。
- **SQLite / localStorage 存密钥**：明文落盘，泄露面大，拒绝。
- **WebView 直达 Provider**：绕过 Sidecar 出站控制与鉴权统一，拒绝。
- **模型直接调任务 API / 自动建任务**：模型输出不可信，必须先人工确认并经既有领域 API，拒绝自动落地。
- **工具调用、Shell、SQL、业务写**：本版不提供任何工具能力。
- **运行时 HTTP 端口（ADR-003 风格独立监听）**：远程接入由 Sidecar 单一出站完成，不新增监听面。

## 验收闸门

- 断网/无可用 Provider 时登记、健康检查、聊天失败路径均返回稳定错误码，其余模块不受影响。
- key 不出现于 SQLite、日志、诊断输出、命令行、前端状态。
- 语义识别缺失/非法结构化块时不创建任何任务；确认创建的任务只经既有任务 API 且新建为 `todo`。
- 取消/断链终止上游请求；遗留活跃生成在启动时被清理。
- 每 Provider/每会话并发超限返回 409；超时与响应上限返回稳定错误码。
- Windows 凭据管理器读写与删除验证通过；无安全存储平台明确 503 且不落盘。

## 影响

该决策允许首版以用户自备 API key 接入远程大模型，换取明确的出站与密钥边界；语义建任务保留"模型输出不是事实、人工确认后经既有领域 API 落地"的仓库既有原则。后续本地部署适配、知识库来源引用、任务/项目/客户上下文读取、用量统计与多 Provider 并发路由均需另行评审。
