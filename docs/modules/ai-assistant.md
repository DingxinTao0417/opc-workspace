# AI 助手模块

> 目标版本：待定（独立于 v0.1–v0.4 的独立轨道）。首个纵向切片已交付：远程 Provider 配置式接入 + 只读会话（含推理模型思考过程展示）+ 语义建任务建议卡片；agent harness 运行时与本地大模型接入（OpenAI 兼容端点）已按 [ADR-005](../adr/005-agent-harness-and-local-models.md) 交付，harness 纠错/反思/长期记忆（自进化的数据层闭环）已按 [ADR-006](../adr/006-harness-matrix-memory-evolution.md) 交付；[ADR-007](../adr/007-session-context-compaction-and-memory-tools.md) G1 的最新完整回合窗口、控制块剥离与精确请求预算已交付，摘要压缩及记忆工具仍在规划中。

## 定位与边界

AI 助手是面向用户的问答、摘要和建议入口。它帮助用户理解与组织工作，输出只读；任务创建必须经用户确认并通过既有任务领域 API 落地。

- 以 Provider 配置式接入大模型：远程 API（API key 模式）或本地部署（OpenAI 兼容端点，无需密钥）。协议注册表首批支持 `openai_chat`（OpenAI chat/completions 流式）与 `anthropic_messages`（Anthropic messages 流式）；本地部署固定走 `openai_chat`。
- API 密钥只保存在操作系统安全存储（首版 Windows 凭据管理器，`zalando/go-keyring`）；不进 SQLite、`localStorage`、日志、命令行、诊断包或前端持久化。无可用安全存储的平台明确 503 拒绝，不落盘退化。本地部署 Provider 不需要也不保存任何密钥。
- 聊天由 Sidecar `internal/harness` 运行时驱动（LLMClient 接口 + 运行循环 + 工具注册表 + 预算）；**生产不注册任何工具**，循环退化为单次调用，行为与会话契约不变（ADR-005）。工具机制仅为后续知识库检索等能力预留，启用任一真实工具须逐个评审授权。
- 远程 Provider 每次请求外发代码所有系统提示、用户确认的长期记忆、所选会话预算内的最近完整回合和当前用户输入；不读取任务/项目/客户等业务数据、不外发整库、无知识库、无遥测。任务确认卡片中再次编辑的字段只提交给本地任务 API，不二次发送给模型。本地 Provider 的模型请求只发往精确的 `localhost` 或 `127.0.0.1` 回环主机。
- 助手回复只读，不直接创建、修改或删除任何业务数据；模型输出视为不可信预览。
- 语义建任务：识别到任务意图时先自然语言确认，再在回复末尾输出 `[opc:task]{...}[/opc:task]` 结构化块（系统提示词为 Sidecar 代码常量）；前端隐藏协议块并解析为**可编辑待确认建议卡片**（标题必填、描述/截止可改、项目可选），用户确认后经既有 `POST /api/v1/tasks` 创建（新建固定 `todo`），创建成功后消息挂静态引用（task_id + task_title_snapshot，不可改绑）；点击卡片跳转 `tasks/:taskId`。兼容部分模型把结束标记误写成第二个 `[opc:task]` 的常见格式；只有结构块而无自然语言时，展示层补一条自然确认。完全没有结构块时保留模型的自然语言回复且不创建任务；存在任务/记忆标记但 JSON 非法或块未闭合时隐藏协议文本，改显自然语言失败说明且不创建任务。
- 不做：任务/项目/客户上下文读取、知识库检索（仅预留扩展点）、生产工具调用、Shell/SQL/任意写、自主代理、Inbox 投影、自动建任务（必须先确认）。
- AI 未配置、密钥无效或端点不可达时，所有既有核心模块完全可用；AI 失败不投影 Inbox。

## 当前实现状态

首个纵向切片已交付（代码与测试为据）：

- **Provider 管理**：`ai_providers` 表（schema 052 + 055 的 `kind` 列，身份/kind/协议/端点/模型/健康/has_key，version 乐观锁与触发器）；`GET/POST /api/v1/ai/providers`、`GET/PATCH/DELETE /api/v1/ai/providers/:id`（Idempotency-Key 幂等、`ETag`/`If-Match` 并发、`ai_adapter_registered / ai_adapter_health_checked / ai_adapter_removed` 事件；删除供应商级联删除**实际用其生成过内容**的会话（messages + generations + sessions，依据 `ai_generations.provider_id` 外键，未用过的会话保留）；`POST /api/v1/ai/providers/:id/health` 真实连通性探测（远程：密钥缺失 `AI_KEY_UNAVAILABLE`、401/403 映射 `AI_KEY_INVALID`；Anthropic 探测携带版本头；本地：无密钥探测，不可达 `AI_ENDPOINT_UNREACHABLE`；成功置 `ready`）；连接身份或密钥改变会清除旧健康结论并要求重测；`POST /api/v1/ai/providers/:id/key` 写入 OS 安全存储（本地 Provider 409 `AI_KEY_NOT_ALLOWED`；密钥写入、替换、删除与数据库失败路径均恢复原安全存储状态，避免 DB/密钥分叉）。
- **Agent Harness**：`internal/harness` 运行时——`LLMClient` 接口（`modelclient` 流式适配器为生产实现）、`Run` 调用/执行循环（默认上限 8 轮）、`Tool`/`Registry`（名称唯一 allowlist）/`Executor`（单工具 30s 超时、结果 64 KiB 截断、panic 恢复、运行级工具结果总预算）；`chatAI` 经由 harness 执行；生产注册表为空，工具机制由单元测试以假 LLM/假工具验证（ADR-005）。**纠错**：工具失败以错误结果回填供模型重试，单次运行失败上限 3 次（ADR-006）。**反思（自发自评，无用户开关）**：代码所有提示词要求模型结束前自评草稿并在末尾输出 `[opc:selfcheck]` 自评块；harness 解析后自主决策——不充分则在全局轮次预算内自动触发一次非流式修订轮（自评 note 回填），充分则直接输出；修订成功以 `replace` 帧原子替换界面草稿，自评块防御性剥离且永不落库/展示（ADR-006）。
- **本地大模型**：`kind=local` 的 Provider 为回环 OpenAI 兼容端点（Go 使用 URL 解析后的精确 hostname 校验，仅允许 `http://127.0.0.1` 或 `http://localhost`，拒绝 userinfo/query/fragment，协议固定 `openai_chat`；回环流量不走代理）；无需密钥——聊天跳过密钥库读取，密钥端点 409 `AI_KEY_NOT_ALLOWED`；健康探测与流式聊天复用 `openai_chat` 适配器，前端无需 `has_key` 即可选择已就绪本地 Provider。
- **长程上下文（G1 已交付，压缩仍未实现）**：每次从数据库取最新 200 条消息，恢复为时间正序后按完整 user turn 从新到旧装入；历史助手消息先剥离 task/memory/selfcheck 控制块，当前用户输入永不静默丢弃。预算以两种协议最终序列化请求体的真实字节数为准（上限 64 KiB），当前输入与系统/记忆本身超限时在建会话、写消息或创建 generation 前返回 `AI_PROMPT_TOO_LARGE`。超预算的早期对话仍会硬截断，**没有摘要**；推理思考流不回填上下文，跨会话仅靠已确认长期记忆注入（≤8 KiB）。G2–G5 仍规划分层摘要/关键事实、后台压缩快照及记忆工具，见 ADR-007。
- **长期记忆（自进化闭环）**：`ai_memories` 表（schema 056，content ≤500 字、来源消息可选）；模型识别到持久偏好时输出 `[opc:memory]` 建议块（系统提示词代码所有），前端解析为待确认卡片，确认后经 `GET/POST/DELETE /api/v1/ai/memories` 管理（POST 幂等 + `ai_memory_created/deleted` 脱敏事件——事件只记 ID 不记内容）；已确认记忆注入后续会话 system 上下文（新→旧前 20 条、8 KiB 预算、超大条目跳过），openai/anthropic 两侧一致；进化只发生在数据层（用户确认的记忆），提示词与工具集永远代码所有（ADR-006）。
- **会话与消息**：`ai_sessions / ai_generations / ai_messages` 表（schema 053）；`GET/POST /api/v1/ai/sessions`、`GET/DELETE /api/v1/ai/sessions/:id`（先校验真实版本，再取消活动生成并级联删除）、`GET /api/v1/ai/sessions/:id/messages`（稳定倒序分页，`before_created_at + before_id` 游标；前端可继续加载更早消息并保持时间正序）。
- **流式聊天**：`POST /api/v1/ai/chat` 以 `opc-ai-sse-v1` SSE 返回 `meta / delta / reasoning / replace / done / error / cancelled` 事件（执行体为 harness Run，`replace` 仅在反思修订成功时出现）；推理类模型（DeepSeek `reasoning_content`、OpenRouter `reasoning`、Anthropic `thinking_delta`）的思考流被单独捕获为 `reasoning` 事件并持久化到 `ai_messages.reasoning`（schema 054），前端以可折叠「思考过程」区展示，绝不混入回答正文；新会话、用户消息与 generation 起始同事务写入，助手回复与 generation 完成状态再同事务收尾，收尾失败返回 `AI_MESSAGE_PERSIST_FAILED` 而不伪报 `done`；`POST /api/v1/ai/generations/:id/cancel` 与 WebView 断连均终止上游，取消保留已生成部分（assistant 消息 `status=cancelled`）；预算：首 token 90s、总时长 10min、响应 1 MiB、提示 64 KiB；每 Provider/每会话并发 1，忙时 409 `AI_PROVIDER_BUSY`；未就绪 Provider 409 `AI_PROVIDER_NOT_READY`；Sidecar 启动把遗留 queued/streaming 标 `cancelled/AI_GENERATION_INTERRUPTED`；`ai_generation_started/completed/failed/cancelled` 事件只记脱敏元数据。
- **任务引用**：`POST /api/v1/ai/messages/:id/task` 只做静态引用落地（消息与任务存在性校验、禁止改绑）；同一任务重复挂接是幂等成功。任务创建本身走既有任务 API（`task_created` 事件不重复写）；若任务已创建而挂接失败，前端保留 task ID，再次确认只重试挂接，不重复创建。
- **前端**：`/ai` 独立页（导航「执行 → AI 助手」、命令面板入口）；会话列表/新建/按真实 version 删除确认、消息向前分页、流式逐字渲染与停止按钮、输入框未就绪禁用、加载/空/错误态；语义建议卡片（隐藏控制块、自然语言确认、可编辑标题/描述/截止/项目，缺标题禁用确认）→ 确认创建 → 已创建任务卡片（静态快照）→ 点击跳任务详情；记忆建议卡片会记录来源消息（记住/忽略，确认后才落库）；设置「AI 助手」区（类型选择远程 API/本地部署、协议下拉——本地锁定 OpenAI 兼容、Base URL、模型名、远程供应商 API key 保存到安全存储、测试连接、删除含密钥清理、长期记忆列表与删除）。页脚按 Provider 类型说明远程外发或本地回环边界，不再笼统声称远程对话只留本机。
- **业务导出边界**：`ai_providers / ai_sessions / ai_generations / ai_messages / ai_memories` 五张表明确排除出业务 JSON/ZIP 导出面（ADR-004 密钥与隐私边界），导入兼容契约覆盖 v49→56。

多供应商支持（已交付）：可登记多个 Provider（名称唯一，远程/本地可混用），设置区渲染全部供应商卡片并可逐个保存密钥（远程）/测试连接/删除；聊天页在多个就绪供应商之间手动切换（输入区下拉，本地供应商带「本地」标识，占位符随所选供应商变化）。尚未实现（规划，非当前能力）：任务/项目/客户上下文选择与发送前预览、知识库检索与来源引用（当前代码尚无 `ContextSource` 接口或检索实现，只有 harness/上下文组装的架构接入方向）、多 Provider 自动路由与并存并发生成（当前每 Provider 并发仍为 1）、用量统计、非 Windows 平台安全存储验证。

## 关键用户流程

1. **配置供应商**：设置 → AI 助手 → 「添加供应商」选择类型：
   - 远程 API：填名称/协议/Base URL/模型名登记 → 保存 API key（进系统安全存储）→ 测试连接至「已就绪」。
   - 本地部署：启动本地 OpenAI 兼容服务（Ollama 默认 `http://127.0.0.1:11434/v1`，LM Studio 默认 `http://127.0.0.1:1234/v1`）→ 填名称/Base URL/模型名登记（无需密钥，协议锁定 OpenAI 兼容）→ 测试连接至「已就绪」。
2. **发起会话**：进入 `/ai`，多个就绪供应商时在输入区下拉选择本次使用的供应商（本地供应商带「本地」标识），输入问题（Enter 发送）；回答流式逐字显示，可点「停止」，取消保留已生成部分。
3. **语义建任务**：助手先给自然语言确认，协议块只用于生成「建议任务」chip且不会原样展示 → 点击展开待确认卡片（标题必填可改）→「确认创建」→ 经任务 API 创建（`todo`）→ 消息显示已创建任务卡片 → 点击跳转任务详情。创建成功但引用挂接短暂失败时可原位重试，且不会重复建任务。
4. **清理**：会话删除（级联删除消息与生成）；供应商删除（远程同删安全存储密钥）。

## 数据、API、状态与事件

### 数据

- `ai_providers`：id、name（唯一）、kind（`remote / local`，默认 remote，schema 055）、protocol（`openai_chat / anthropic_messages`；本地固定 `openai_chat`）、base_url（https 或回环 http；本地强制回环 http）、model、status（`unconfigured / checking / ready / unavailable / disabled`）、health_status（`unknown / healthy / unhealthy`）、health_error_code、has_key、last_health_at、version、时间戳；version 步进触发器。
- `ai_sessions`：id、title、persist、version、时间戳。
- `ai_generations`：id、session_id、provider_id、status（`queued / streaming / completed / failed / cancelled`）、error_code、content（≤1 MiB）、时间戳。
- `ai_messages`：id、session_id、role（`user / assistant`）、status（`completed / cancelled / failed`）、content、reasoning（可空，仅推理模型产生；不进入业务导出）、model_snapshot（JSON，不导出）、task_id + task_title_snapshot（成对可空，落定后不可改绑）、时间戳。
- `ai_memories`：id、content（1–500 字）、source_message_id（可空）、时间戳（schema 056；不进入业务导出）。
- API 密钥：仅 OS 安全存储（服务名 `opc-workspace-ai`，账户 `ai:<provider-id>:api_key`）。
- 五张表均不进入业务 JSON/ZIP 导出（操作态与隐私边界，同 ADR-004）。

### API

- `GET / POST /api/v1/ai/providers`、`GET / PATCH / DELETE /api/v1/ai/providers/:id`、`POST /api/v1/ai/providers/:id/health`、`POST /api/v1/ai/providers/:id/key`
- `GET / POST /api/v1/ai/sessions`、`GET / DELETE /api/v1/ai/sessions/:id`、`GET /api/v1/ai/sessions/:id/messages`
- `GET / POST /api/v1/ai/memories`、`DELETE /api/v1/ai/memories/:id`
- `POST /api/v1/ai/chat`（SSE `opc-ai-sse-v1`；反思为 agent 自发行为，无请求参数）、`POST /api/v1/ai/generations/:id/cancel`、`POST /api/v1/ai/messages/:id/task`

### 状态与事件

- Generation 状态链：`queued / streaming → completed | failed | cancelled`；终态不可改（仅启动恢复把遗留活跃态标 cancelled）。
- Workflow Event（脱敏，不含提示/回答/任务正文）：`ai_adapter_registered / ai_adapter_health_checked / ai_adapter_removed`（aggregate `ai_provider`）、`ai_generation_started / ai_generation_completed / ai_generation_failed / ai_generation_cancelled`（aggregate `ai_generation`）。
- 主要错误码：`AI_PROVIDER_NOT_FOUND / AI_PROVIDER_NAME_TAKEN / AI_PROVIDER_KIND_INVALID / AI_PROTOCOL_INVALID / AI_ENDPOINT_INVALID / AI_MODEL_INVALID / AI_PROVIDER_NOT_READY / AI_PROVIDER_DISABLED / AI_PROVIDER_BUSY / AI_KEY_UNAVAILABLE / AI_KEY_INVALID / AI_KEY_NOT_ALLOWED / AI_KEY_STORE_UNAVAILABLE / AI_KEY_MALFORMED / AI_STREAM_ERROR / AI_GENERATION_TIMEOUT / AI_PROMPT_TOO_LARGE / AI_MESSAGE_PERSIST_FAILED / AI_GENERATION_NOT_ACTIVE / AI_GENERATION_ALREADY_TERMINAL / AI_MESSAGE_TASK_ALREADY_LINKED / AI_MEMORY_CONTENT_INVALID / INVALID_AI_MEMORY_ID`。

## 与其他模块协作

- **任务**：任务创建一律经既有 `POST /api/v1/tasks` 门禁（用户在建议卡片显式确认）；消息仅保存静态快照引用，不跟踪任务状态，不触碰任务状态机；取消误建任务须在任务模块操作。
- **设置**：Provider 配置区挂在设置「AI 助手」模块，独立自持保存（不走共享 draft/preview）。
- **数据管理**：AI 五表为操作态/隐私边界，排除出业务导出；一致性备份（SQLite 快照）仍覆盖它们。
- **诊断/日志**：普通日志只记 provider/generation ID、阶段与错误码；密钥由 operationlog 的 secrets 机制脱敏。
- **本地 Agent（v0.2）**：互不共享能力。Agent Adapter 是受控执行器（ADR-003 匿名管道）；AI 助手是远程只读对话，不获得任何执行能力。

## 分阶段实施

1. **AI2 评审**（已完成）：ADR-004 固化远程 Provider 授权、密钥与数据外发边界、语义建任务机制。
2. **AI3 Adapter 基线**（已完成）：Provider 登记/健康/密钥 API 与设置区、schema 052。
3. **AI4 只读会话**（已完成）：schema 053、SSE 流式聊天、取消/断连/超时/并发闸门、启动清理、会话与消息 API、`/ai` 页面。
4. **AI4.5 语义建任务**（已完成）：代码所有系统提示词、结构化块解析、待确认卡片、既有任务 API 落地与静态引用。
5. **AI4.6 Harness 与本地大模型**（已完成）：ADR-005；`internal/harness` 运行循环/工具注册表/执行器/预算（生产零工具）、schema 055 `kind` 列、本地 Provider 无密钥全链路、设置区类型选择与本地徽标。
6. **AI4.7 纠错/反思/长期记忆**（已完成）：ADR-006；工具失败回填重试（上限 3）、模型自发自评块驱动的静默修订轮（上限 1）、schema 056 `ai_memories` 建议块确认流与注入预算、设置区记忆管理。
7. **AI4.8 长程上下文压缩与记忆工具**（部分完成，[ADR-007](../adr/007-session-context-compaction-and-memory-tools.md)）：G1 的最新 200 条读取、完整回合窗口、历史助手控制块剥离、最终序列化请求预算及超限原子拒绝已交付；G2 压缩快照（057）与摘要/事实注入、G3 两协议工具调用、G4 记忆三工具、G5 前端透明化仍规划。
8. **AI5 显式上下文**（规划）：任务/项目/客户上下文选择、发送前范围预览、最小快照。
9. **AI6 知识库与来源**（规划）：依赖本地知识库；需要新增受控上下文来源接口或检索工具后接入（逐个评审授权）；引用可定位与“未检索到可靠来源”处理。
10. **AI7 质量闸门与扩展**（规划）：提示注入/越权/资源耗尽测试集、多 Provider 自动路由、更多协议适配器、运行步骤追踪与执行时间线（ai_run_steps，ADR-006 F3）、编排与子代理（F4，需独立评审）。

## 验收标准（当前切片已覆盖项）

- 未配置/密钥无效/端点不可达时：健康检查给出可读错误码，聊天 4xx/5xx 稳定错误，核心模块完全可用（以当前全量门禁结果为准）。
- 密钥不进 SQLite/日志/响应/导出：key 端点响应断言不含密钥原文；删除供应商清理安全存储（Go 测试覆盖）。
- 本地 Provider 无密钥 创建→健康→流式聊天 全链路（httptest 回环上游）；密钥端点 409 `AI_KEY_NOT_ALLOWED`；本地非回环端点与非法 kind 被拒；远程行为回归不变（Go 测试覆盖）。
- harness 单元测试：单轮直通、多轮工具循环与回填、轮数预算、取消传播、重复工具名拒绝、执行器超时/panic/截断/总预算（假 LLM/假工具覆盖）；`chatAI` 契约回归（既有 AI API 测试全绿）。
- 结构化块缺失/非法不建任务；正常闭合和常见的重复开始标记都能生成卡片，协议文本不展示且始终有自然语言确认；非法或未闭合标记改显自然语言失败说明；确认创建只经既有任务 API且新建为 `todo`，挂接重试不重复创建（解析器、页面与 API 测试覆盖）。
- 取消/断连终止上游并保留部分内容；并发 409；启动恢复遗留生成（Go 测试覆盖）。
- 流式帧序 meta/delta/reasoning/replace/done 与 openai/anthropic 双协议映射（mock 上游 Go 测试 + 前端 SSE 解析测试覆盖）。
- 设置表单类型切换（本地隐藏密钥、提交载荷带 kind、协议锁定）、本地卡片无密钥行、聊天页「本地」标识（Web 测试覆盖）。
- 纠错/反思：工具失败回填重试与超限终止（harness 单测）；自评充分→单次调用且块被剥离、不充分→恰好一次内部修订并以 `replace` 更新界面（note 回填）、缺失/未闭合/非法块防御性剥离（API 契约测试 + harness 单测）。
- 记忆：建议块解析（合法/非法/超长）、确认落地与幂等、列表/删除、注入预算（数量/字节/超大跳过）、事件不含内容、导出排除（Go + Web 测试覆盖）。

### 本地模型真机验证步骤（需用户本机环境）

1. 安装并启动 Ollama（`ollama serve`，默认 11434）或 LM Studio（打开本地服务，默认 1234）。
2. 设置 → AI 助手 → 添加供应商 → 类型选「本地部署」→ Base URL 填对应地址 → 模型名填已拉取的模型（如 `qwen3`）→ 登记。
3. 点「测试连接」至「已就绪」；进入 `/ai` 选择该供应商发起会话。
4. 断开本地服务后「测试连接」应显示「端点无法访问」，聊天报稳定错误；核心模块不受影响。

## 相关 PRD 与代码链接

- [产品 PRD](../opc-workspace-PRD.md)（§5.10）
- [ADR-004：远程 Provider 接入与安全边界](../adr/004-ai-assistant-provider-access.md)
- [ADR-005：Agent Harness 架构与本地大模型接入](../adr/005-agent-harness-and-local-models.md)
- [ADR-006：Harness 完整组件矩阵与自进化边界](../adr/006-harness-matrix-memory-evolution.md)
- [ADR-007：会话上下文压缩与记忆工具](../adr/007-session-context-compaction-and-memory-tools.md)（G1 已交付，G2–G5 待实施；[分阶段计划](../plans/context-memory-phases.md)）
- [MVP 计划草稿](../plans/ai-assistant-mvp.md)、[Harness 分阶段计划](../plans/agent-harness-phases.md)
- Sidecar：`services/sidecar/internal/api/ai_providers.go`、`ai_sessions.go`、`ai_chat.go`、`ai_messages.go`、`ai_memories.go`、`internal/harness/`、`internal/modelclient/`、`internal/keystore/`
- 前端：`apps/web/src/pages/AiAssistantPage.tsx`、`apps/web/src/components/AiProviderSettings.tsx`、`apps/web/src/api/ai.ts`、`apps/web/src/lib/aiTaskCard.ts`
- 迁移：`services/sidecar/internal/database/migrations/052_ai_providers.sql`、`053_ai_sessions.sql`、`054_ai_message_reasoning.sql`、`055_ai_provider_kind.sql`、`056_ai_memories.sql`
