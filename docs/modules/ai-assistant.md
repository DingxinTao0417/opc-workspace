# AI 助手模块

> 目标版本：待定（独立于 v0.1–v0.4）。Provider、流式只读会话、Harness/记忆、ADR-007 G1–G5、[ADR-008](../adr/008-ai-explicit-business-context.md) AI5、[ADR-010](../adr/010-ai-explicit-knowledge-context.md) AI6 与 [ADR-011](../adr/011-ai-validated-knowledge-citations.md) AI7-Q1 可验证引用已交付。

## 定位与边界

AI 助手是面向用户的问答、摘要和建议入口。它帮助用户理解与组织工作，输出只读；任务创建必须经用户确认并通过既有任务领域 API 落地。

- 以 Provider 配置式接入大模型：远程 API（API key 模式）或本地部署（OpenAI 兼容端点，无需密钥）。协议注册表首批支持 `openai_chat`（OpenAI chat/completions 流式）与 `anthropic_messages`（Anthropic messages 流式）；本地部署固定走 `openai_chat`。
- API 密钥只保存在操作系统安全存储（首版 Windows 凭据管理器，`zalando/go-keyring`）；不进 SQLite、`localStorage`、日志、命令行、诊断包或前端持久化。无可用安全存储的平台明确 503 拒绝，不落盘退化。本地部署 Provider 不需要也不保存任何密钥。
- 聊天由 Sidecar `internal/harness` 运行时驱动（LLMClient 接口 + 运行循环 + 工具注册表 + 预算）；生产只注册 ADR-007 明确授权的 `memory_search / memory_write / memory_propose`，不注册业务、文件、Shell、网络或知识库工具。其他能力仍须逐个评审授权。
- 远程 Provider 请求可包含系统提示、长期记忆、摘要/事实、最近回合、当前输入、memory_search 命中，以及本条消息由用户预览确认的 Task/Project/Client 最小快照和最多 3 个知识 chunk。每类业务对象最多一项，知识只来自用户手工选择的本地搜索结果；不自动扩展关联对象或检索整库，不读取联系人、邮箱、电话、金额、活动、附件、Artifact 或发票。本地 Provider 请求只发往配置的回环 origin。
- 助手回复只读，不直接创建、修改或删除任何业务数据；模型输出视为不可信预览。
- 语义建任务：模型先给自然语言待确认说明，再给 `[opc:task]{...}[/opc:task]`。前端隐藏控制块、允许编辑；用户明确确认后调用 `/ai/messages/:id/task-confirmation`，服务端复用 Task 领域规则原子创建与绑定（固定 todo），按消息身份幂等。显示“尚未创建/已创建”以服务端事实为准，已创建卡片跳转 `tasks/:taskId`；旧误闭合格式兼容，null/数组/错类型/多块/非法/未闭合内容均不崩溃或直接建任务，改显自然语言失败说明。
- 不做：自动读取当前页面/最近对象、关系扩展、模型自主知识库检索、业务/Shell/文件/网络/knowledge 工具、任意业务写、自主代理、Inbox 投影、自动建任务（必须先确认）。
- AI 未配置、密钥无效或端点不可达时，所有既有核心模块完全可用；AI 失败不投影 Inbox。

## 当前实现状态

本轮可靠性契约由 [ADR-025](../adr/025-ai-reliability-confirmations-and-evaluation-identity.md) 接续，SQLite schema 068–069；整体回归结果见 [AI7 验证记录](../plans/ai-quality-gates.md)。应用级恢复、原子确认和临时记忆隐私不再依赖组件局部状态。

首个纵向切片已交付（代码与测试为据）：

- **Provider 管理**：schema 052/055/068；登记、PATCH、健康、密钥与删除均有 API。`version` 保护 HTTP 并发，`config_version` 标识真实请求配置；健康和名称变化不递增配置身份。已有会话/评测历史保护删除，不隐式删历史。准备/密钥读取与收尾使用短 Provider 锁，不跨模型或健康网络等待；同 origin 重定向与密钥脱敏边界不变。
- **Agent Harness**：最多 8 轮、32 次工具调用；单工具 30 秒/64 KiB/panic 隔离。模型轮、工具结果和最多一次自检修订共享 generation 的 10 分钟/累计 1 MiB 预算。两协议支持多工具及参数跨帧；只注册记忆三工具。工具失败可回填纠错（上限 3）；selfcheck 缺失/非法记录 unavailable，不假装通过，不充分最多一次修订；修订错误传播并保留已有部分回答。
- **本地大模型**：`kind=local` 只允许 `http://127.0.0.1` 或 `http://localhost` 的 OpenAI 兼容端点，拒绝 userinfo/query/fragment，协议固定 `openai_chat` 且无密钥；健康和聊天均不走代理，并拒绝跨 origin 重定向，保证 prompt 不随 307/308 离开配置的回环 origin。
- **长程上下文（G1–G5 + ADR-025）**：最新 200 条按完整 user turn 和最终 64 KiB 请求预算装入，助手控制块剥离。窗口外正文超过 16 KiB 后后台压缩；单批 JSONL ≤32 KiB，大回合按 UTF-8 有界分段，持久 offset 不跳过未处理正文。每次触发最多 8 批/10 分钟，剩余标 pending，下次成功聊天继续。summary ≤4 KiB、五类 facts ≤32/4 KiB；替换快照/水位线/脱敏事件同事务，失败/取消/配置变化不推进。API/UI 区分运行、滞后、失败/取消及部分消息；完整计数不含只覆盖一部分的消息。
- **记忆与工作状态**：持久会话的 memory_write/search/propose 分别记录会话事实、查当前会话记忆/历史、提出待确认偏好；非持久会话只在当前运行共享内存中完成三项操作。永久记忆仍须用户另行明确确认，才进入 `ai_memories` 并注入后续会话（前 20 条、8 KiB）。schema 069 保存提议的 confirmed/rejected 与 memory_id，忽略/确认均可刷新回读，旧无 proposal_id 卡片按消息身份兼容。事件不记录内容，提示词与工具集仍代码所有。
- **会话与消息**：schema 053/069。`persist=false` 不保存消息、generation 正文、会话事实、记忆提议或摘要；请求身份与状态等无正文元数据可保存。应用内草稿/部分回答在内存中；storage 只保留恢复 ID。删除会话先取消生成/压缩，再清理 AI 操作态。
- **流式聊天**：`POST /api/v1/ai/chat` 返回 opc-ai-sse-v1 meta/delta/reasoning/replace/done/error/cancelled；首 token 90 秒、generation 总 10 分钟/1 MiB、提示 64 KiB，每 Provider/会话并发 1。应用级状态支持切路由继续与全局停止；硬刷新/断连取消原连接并回读真实终态。接受过的 Idempotency-Key 不重发新消息。无完成事件 EOF、截断、过滤为失败，持久会话保存失败部分回答；非持久会话不落正文。
- **任务确认**：`POST /api/v1/ai/messages/:id/task-confirmation` 接收既有 Task 创建字段，复用共享领域创建函数，在一个事务内创建 Task 并保存消息静态引用。消息 ID 是服务端稳定确认身份；相同载荷返回真实 Task/完整 Message，改参 409，任务已删除 410，不重复创建。旧 `/messages/:id/task` 保留只挂接兼容。普通 Task 创建没有 `task_created` 事件，本次不新增虚构事件，沿用父级协调及其事件规则。
- **前端**：`/ai` 提供会话、新建/删除、历史分页、Provider 切换、流式回答/停止、推理折叠、任务与记忆确认卡片，并显示“已压缩前 N 条消息”；会话事实每 15 秒刷新。设置「AI 助手」区管理 Provider、密钥、健康、pending 记忆建议的来源会话/标签/确认/忽略，以及已确认长期记忆。页脚按 Provider 类型说明远程外发或本地回环边界。
- **显式业务上下文（AI5）**：Composer 复用 TaskSelect/ProjectSelect/ClientSelect，最多各选一个。`POST /api/v1/ai/context/preview` 返回目标 Provider、本地/远程边界、全部白名单字段、截断标记、版本和字节数；用户确认后才能随下一条消息发送。Chat 在任何会话/消息/generation 写入前按 Provider/source version 重建同一快照，变化返回 `AI_CONTEXT_PROVIDER_CHANGED / AI_CONTEXT_CHANGED`。发送成功清空一次性选择，失败保留；历史 user message 展示实际发送来源卡片。
- **显式知识上下文（AI6）**：同一面板调用本地知识搜索，用户最多逐段选择 3 个 chunk；preview 展示完整实际正文、来源、source/document version、行/字符位置、Provider 外发边界和组合字节数。Chat 在任何 AI 写入前两次重验三层 identity 与版本，v2 `context_snapshot` 保存实际发送片段；历史展示 `来源 · Lx–y`。生产仍无 knowledge tool，代码所有提示把片段标为不可信引用并要求引用来源/行号。
- **可验证知识引用（AI7-Q1）**：有知识上下文时模型只声明实际使用的 `chunk_ids`；Sidecar 对本次 allowlist 做严格 JSON/UUID/重复/数量校验，并从已验证上下文重建来源、版本和位置。schema 060 `citations_snapshot` 保存 `validated / no_evidence / missing / invalid` 与最多 3 个引用，不复制正文；历史 UI 展示已验证来源或差异化质量警告。Raw citation control block 在持久化、历史 prompt 和流式 UI 中剥离。
- **运行步骤与本地指标（AI7-Q3）**：schema 061 `ai_run_steps` 保存 generation/model turn/记忆工具/self-check/citation/persistence 的状态、耗时、字节和稳定错误码，不保存正文/凭据。schema 062 只在 Provider 返回完整非负 usage 时保存 input/output token 与 `token_source=provider`；OpenAI 读根 usage，Anthropic 合并 start/delta，缺失/部分保持 unknown，不强制 stream_options。历史时间线与当前会话用量均按需展示；ADR-024 将终态根步骤按 UTC 日派生 1–30 天连续趋势，7/30 天面板切换不改变累计 totals。用量 API 可按 session/provider 聚合终态、活动、coverage、原始 token、bytes 与 duration；不新增表、不估算 token、不计算费用。
- **显式本地模型质量评测、分层套件与人工决定审计（AI7-Q2）**：schema 063–068。当前 dataset v4/24，仍有 smoke 8、full 24 和四个 topic 各 6；只显式运行 ready/healthy 本地 Provider。真实生产系统提示与 Harness，单邮箱 Actor 支持幂等/进度/取消/恢复/删除；只保存无正文指标。配置已知组按 Provider ID/config_version/dataset/suite 隔离，旧 NULL 身份保留原名称/模型分组及混合版本规则，不改写原评审记录。有限事实别名、否定/冲突与 FACT_MISSING/FACT_CONTRADICTED 和结构/关键词检查分层；后者为严重 code，自动通过仍不替代人工。full 才可能形成候选，smoke/topic 只诊断；owner 决定只追加审计，不改变 Provider/业务状态。
- **业务导出边界**：十张 AI 表及知识库操作表仍排除便携业务 JSON/ZIP；schema 068–069 没有新增业务表。兼容显式增加 v67/v68→69，并沿用 v49/v63/v64/v65/v66 兼容路径，未知未来 schema 拒绝。

多供应商支持（已交付）：可登记多个 Provider，聊天页手动切换。AI5/AI6 上下文预览绑定 Provider version；切换或配置变化会使旧确认失效。尚未实现：句子级 citation/引用覆盖率、更多事实覆盖/发布阈值、多 Provider 自动路由/并存生成、费用、非 Windows 平台安全存储验证。

## 关键用户流程

1. **配置供应商**：设置 → AI 助手 → 「添加供应商」选择类型：
   - 远程 API：填名称/协议/Base URL/模型名登记 → 保存 API key（进系统安全存储）→ 测试连接至「已就绪」。
   - 本地部署：启动本地 OpenAI 兼容服务（Ollama 默认 `http://127.0.0.1:11434/v1`，LM Studio 默认 `http://127.0.0.1:1234/v1`）→ 填名称/Base URL/模型名登记（无需密钥，协议锁定 OpenAI 兼容）→ 测试连接至「已就绪」。
2. **发起会话**：进入 `/ai`，多个就绪供应商时在输入区下拉选择本次使用的供应商（本地供应商带「本地」标识），输入问题（Enter 发送）；回答流式逐字显示，可点「停止」，取消保留已生成部分。
3. **显式业务上下文**：展开“上下文”→ 各选最多一个 Task/Project/Client →“预览发送内容”查看目标 Provider、全部字段/截断和字节数 →“确认用于下一条消息”→ 发送；版本变化会拒绝并保留选择供重新预览。
4. **显式知识上下文**：在同一面板搜索本地知识库 → 逐段选择最多 3 个结果 → preview 检查完整正文、来源、位置、版本和远程/本地披露 → 确认发送；搜索未选结果和整份来源不进入模型。
5. **语义建任务**：自然语言“尚未创建”与待确认建议 → 编辑/明确确认 → 服务端 Task 领域事务原子创建并绑定 → 显示真实 Task 名称和入口。响应丢失或刷新后同消息重试返回已有 Task，改参冲突；已删除任务不会用原建议重新创建。
6. **本地评测与审计**：设置中显式选择 smoke/topic/full → 本地 Actor 顺序执行 → 按 dataset/suite/真实配置身份展示计数、事实规则失败、趋势/Wilson。健康检查不拆组，真实配置变化形成新组，旧历史无需删除。只有当前 full 的同配置重复证据可形成只读人工候选；规则、重复次数和 Wilson 均不是发布许可。
7. **清理**：会话删除会取消生成/压缩并清理其操作态；供应商只在没有 generation 或评测历史时允许删除，远程同时清理安全存储密钥。终态评测历史可显式删除；知识来源删除不追溯改写已发送的会话快照。

## 数据、API、状态与事件

### 数据

- `ai_providers`：id/name/kind/protocol/base_url/model、状态/健康、has_key、last_health_at、HTTP `version`、schema 068 `config_version`、时间戳；配置身份只随类型/协议/端点/模型/实际密钥变化。
- `ai_sessions`：id、title、persist、version、时间戳；响应派生 `compacted_message_count`，不复制进表。
- `ai_generations`：id/session_id/provider_id、queued/streaming/completed/failed/cancelled、error_code、nullable content（≤1 MiB）、时间戳；schema 069 增加配对且不可变的 nullable request_key/request_hash，旧行保持 NULL。
- `ai_messages`：既有 role/status/content/reasoning/model/task 引用；schema 058 的 nullable `context_snapshot` 用于持久 user message，v1 保存 Provider + 业务 sources，向后兼容的 v2 另保存最多 3 个 knowledge chunk；数据库防线 32 KiB，业务层 16 KiB、知识层 12 KiB、组合 30 KiB。
- schema 060 为 `ai_messages` 增加 nullable `citations_snapshot`：只允许 completed assistant，version 1、最多 3 项、16 KiB 防线；只保存 Sidecar 重建的来源/文档/chunk/版本/位置，不保存正文或模型 quote。
- schema 061 新增 `ai_run_steps` 和 assistant generation link；schema 062 增加全有/全无的 nullable provider token 列。既有 generation 只回填 summary root 且 token unknown；会话删除级联 steps，便携业务导出排除。
- schema 063 新增 `ai_evaluation_runs / ai_evaluation_results`：Run 保存 Provider/数据集快照、状态/进度/取消/稳定错误码；Result 保存 case identity、passed/failed/error、稳定 failure code 数组、citation 数与无正文指标。每 Provider 最多一个活动 Run，Result 不可更新；终态 Run 删除级联 Result。
- schema 066 新增 `ai_evaluation_reviews`：保存精确质量组计数/Wilson/readiness/严重 code 快照、三值人工决定、必填理由与 builtin owner Actor 快照；UPDATE/DELETE 由 trigger 拒绝，且不依赖 Provider 外键。
- schema 067 受保护共同重建 Run/Result/Review 并逐列保留历史，把三表 suite CHECK 扩展为 smoke/full + 四个专题 key；恢复原外键、索引和不可变 trigger。
- `ai_memories`：id、content（1–500 字）、source_message_id（可空）、时间戳（schema 056；不进入业务导出）。
- `ai_memory_entries`：session_id、kind、content/tags/origin/status、source_message_id 与时间戳；schema 069 增加 source_message_offset、decision、memory_id。offset>0 为清理控制块后 UTF-8 已处理字节，0 为整条完成；确认/拒绝与快照 payload 均不可重写，active→superseded 受控。
- API 密钥：仅 OS 安全存储（服务名 `opc-workspace-ai`，账户 `ai:<provider-id>:api_key`）。
- 十张 AI 表均不进入业务 JSON/ZIP 导出（操作态与隐私边界，同 ADR-004/007/012/013/022）。

### API

- `POST /api/v1/ai/messages/:id/task-confirmation`（显式人工命令；原子 Task 创建/静态绑定，返回 `{data:{task,message}}`）
- `GET /api/v1/ai/active-generations`、`GET /api/v1/ai/generations/:id`、`GET /api/v1/ai/generations/by-request/:key`（活动内存/持久终态回读；非持久终态不返回正文）
- `GET /api/v1/ai/memory-proposals/:id`、`GET / DELETE /api/v1/ai/messages/:id/memory-decision`（新提议和旧无 proposal_id 消息的决定回读/忽略；GET 不产生写入）
- `GET /api/v1/ai/sessions/:id/compaction`（运行状态、稳定错误码、部分消息和完整覆盖消息数）

- `GET / POST /api/v1/ai/providers`、`GET / PATCH / DELETE /api/v1/ai/providers/:id`、`POST /api/v1/ai/providers/:id/health`、`POST /api/v1/ai/providers/:id/key`
- `GET / POST /api/v1/ai/sessions`、`GET / DELETE /api/v1/ai/sessions/:id`、`GET /api/v1/ai/sessions/:id/messages`
- `GET / POST /api/v1/ai/memories`、`DELETE /api/v1/ai/memories/:id`
- `GET /api/v1/ai/memory-proposals`、`DELETE /api/v1/ai/memory-proposals/:id`（确认复用 `POST /ai/memories` + proposal_id）
- `POST /api/v1/ai/context/preview`（Provider + 最多三类业务 source + 最多 3 个 knowledge chunk identity → 精确快照、版本、正文/位置、截断与外发边界）
- `POST /api/v1/ai/chat`（SSE `opc-ai-sse-v1`；反思为 agent 自发行为，无请求参数）、`POST /api/v1/ai/generations/:id/cancel`、`POST /api/v1/ai/messages/:id/task`
- `GET /api/v1/ai/generations/:id/steps`（内容无关的有序本地时间线与 bytes/duration 汇总）
- `GET /api/v1/ai/usage-summary?session_id=&provider_id=&trend_days=1..30`（可选双过滤；同一只读事务聚合 generation 根步骤、状态、Provider usage/unknown、原始 token、bytes、duration 和 Provider 分组；另返回连续 UTC 终态趋势）
- `GET / POST /api/v1/ai/evaluations`（分页历史；显式选择 Provider ID/version 并可带 Idempotency-Key 创建本地评测）
- `GET /api/v1/ai/evaluations/:id`、`POST /api/v1/ai/evaluations/:id/cancel`、`DELETE /api/v1/ai/evaluations/:id?confirm=true`（无正文详情、取消、终态历史删除）
- `GET /api/v1/ai/evaluation-summary?provider_id=&limit=`（单只读事务返回全部 Run 状态计数、succeeded-only 同口径质量分组、四类 Result category、failure-code affected/occurrences 及最近 1–50 次完整 Run 趋势）
- `GET / POST /api/v1/ai/evaluation-reviews`（倒序分页/Provider/decision 筛选；POST 在同一写事务重算精确证据，支持幂等并拒绝 stale 快照；无编辑/删除端点）
- 消息 API 另返回 `citation_status / citations`；`not_requested` 为 NULL 快照的派生态，其他四态来自 schema 060 服务器验证结果。

### 状态与事件

- schema 068：Provider `config_version` 与 HTTP `version` 分离；Run/Review `provider_config_version` 为 nullable 历史快照。相同配置的健康检查不拆组；旧 NULL 身份保留未知，不改写历史审计。数据集当前 v4/24，新增 `FACT_CONTRADICTED`，自动事实规则不等于通用语义判断。
- schema 069：Generation `request_key/request_hash` 配对且不可变；Message `task_confirmation_hash` 固化已确认载荷；MemoryEntry `decision/memory_id` 固化确认/拒绝，`source_message_offset` 记录 UTF-8 部分进度。确认 Task/Memory 被删除后，旧建议不重新创建。
- 记忆 GET 返回 `pending / confirmed / rejected`。旧无 proposal_id 卡片只在用户明确确认/忽略时 materialize 稳定记录；刷新可回读，已确认不能被“忽略”撤销。无法验证决定的旧 superseded 提议返回 `409 AI_MEMORY_DECISION_UNAVAILABLE`，不冒充已忽略；缓存重放仍检查记忆是否被删除，已删除返回 410。`persist=false` 三工具共享运行内内存，临时提议无持久 proposal_id；跨会话永久记忆须另外明确确认。
- 应用级生成状态跨 SPA 路由保留，并有全局停止入口；浏览器 storage 只保存恢复 ID。硬刷新/断连取消原 SSE，上游和终态通过回读收敛，不能自动重复发送。接受前失败保留草稿/上下文，不覆盖后来输入；纯内存草稿不承诺硬刷新恢复。中文输入法 composition Enter 不发送。
- 完整 SSE 终态回合仅在应用内存保留，最多 20 回合 / 8 MiB（用户输入、回答和思考合计），超限淘汰最旧回合，删除会话同步清除；持久消息按 generation 去重。无持久 message ID 的回合只读，不调用任务/记忆确认 API。恢复有命令代次检查，旧结果/404 不覆盖新请求；非持久终态只回元数据或服务重启 404 时，已收片段继续可见但标记不完整，不能宣称获得完整回答。
- 模型网络等待不占全局 maintenance/Provider 锁；数据库准备/收尾短锁仍保护恢复边界。整次 generation 共享 10 分钟/1 MiB，包含多轮、工具结果与自检修订。只有内容的 EOF 为失败；OpenAI stop 后继续收 usage；缺失/非法 selfcheck 为 unavailable，不伪装通过。
- Citation `validated` 仅证明引用来自本次已确认 allowlist，不能证明回答事实正确或逐句被引用支持。

- Generation 状态链：`queued / streaming → completed | failed | cancelled`；终态不可改（仅启动恢复把遗留活跃态标 cancelled）。
- Workflow Event（脱敏，不含提示/回答/任务/摘要/记忆正文）：Provider/Generation 既有事件，`ai_session_compacted`，以及 `ai_session_memory_written / ai_memory_proposed / ai_memory_proposal_confirmed / ai_memory_proposal_rejected`（aggregate `ai_session`，只记资源 ID、kind、tag/count）。
- 主要错误码：既有 Provider/Generation/Message/Memory 错误族，AI5 `AI_CONTEXT_*`、AI6 `AI_KNOWLEDGE_CONTEXT_*`，以及仅编码/持久化失败使用的 `AI_CITATION_PERSIST_FAILED`；模型 citation 缺失/非法是可解释质量状态，不伪装成网络失败。

## 与其他模块协作

- **任务**：人工确认接口与 `POST /api/v1/tasks` 复用同一创建领域逻辑；确认、Task 创建和静态绑定同事务，不跟踪后续任务状态，不绕过生命周期/验收。取消误建任务仍在任务模块操作。
- **设置**：Provider 配置区挂在设置「AI 助手」模块，独立自持保存（不走共享 draft/preview）。
- **数据管理**：AI 十表为操作态/隐私边界，排除出业务导出；一致性备份（SQLite 快照）仍覆盖它们。
- **知识库**：AI 只调用用户触发的本地 search，并把选中 identity 交给 context preview；Sidecar 重新加载完整 chunk。无 knowledge tool，不自动扫描；知识来源重建/删除通过版本或不存在门禁使旧确认失效。
- **诊断/日志**：普通日志只记 provider/generation ID、阶段与错误码；密钥由 operationlog 的 secrets 机制脱敏。
- **本地 Agent（v0.2）**：互不共享能力。Agent Adapter 是受控执行器（ADR-003 匿名管道）；AI 助手是远程只读对话，不获得任何执行能力。

## 分阶段实施

1. **AI2 评审**（已完成）：ADR-004 固化远程 Provider 授权、密钥与数据外发边界、语义建任务机制。
2. **AI3 Adapter 基线**（已完成）：Provider 登记/健康/密钥 API 与设置区、schema 052。
3. **AI4 只读会话**（已完成）：schema 053、SSE 流式聊天、取消/断连/超时/并发闸门、启动清理、会话与消息 API、`/ai` 页面。
4. **AI4.5 语义建任务**（已完成）：代码所有系统提示词、结构化块解析、待确认卡片、既有任务 API 落地与静态引用。
5. **AI4.6 Harness 与本地大模型**（已完成）：ADR-005；`internal/harness` 运行循环/工具注册表/执行器/预算（该阶段生产零工具，后由 AI4.8 G4 开放记忆三工具）、schema 055 `kind` 列、本地 Provider 无密钥全链路、设置区类型选择与本地徽标。
6. **AI4.7 纠错/反思/长期记忆**（已完成）：ADR-006；工具失败回填重试（上限 3）、模型自发自评块驱动的静默修订轮（上限 1）、schema 056 `ai_memories` 建议块确认流与注入预算、设置区记忆管理。
7. **AI4.8 长程上下文压缩与记忆工具**（已完成，[ADR-007](../adr/007-session-context-compaction-and-memory-tools.md)）：G1 完整回合窗口与精确预算；G2 schema 057 快照、后台压缩、水位线、摘要/事实注入；G3 OpenAI/Anthropic 工具协议；G4 记忆三工具与持久确认门禁；G5 压缩标记和 pending proposal 管理均已交付。
8. **AI5 显式上下文**（已完成，[ADR-008](../adr/008-ai-explicit-business-context.md)）：三类单选、最小字段白名单、发送前预览、Provider/source version 重验、一次性发送、schema 058 历史快照与来源卡片。
9. **AI6 知识库与来源**（已完成，[ADR-010](../adr/010-ai-explicit-knowledge-context.md)）：本地搜索、最多 3 个显式 chunk、完整发送预览、Provider/source/document 双重版本重验、v2 历史快照和来源 chips；没有新增工具。回答级 citation 已由 ADR-011 AI7-Q1 补齐。
10. **AI7 质量闸门与扩展**（进行中）：Q1 回答级 citation；Q2 v4/24、smoke/topic/full、本地 Actor、配置身份分组/趋势/category/failure/Wilson/人工决定审计；Q3 无正文 steps、原始 Provider token/unknown、聚合与 UTC 趋势已实现；ADR-025 修复运行、确认、恢复和隐私。费用、句子级证据覆盖、Q4 自动路由/更多协议待定，F4 编排/子代理未授权。

## 验收标准（当前切片已覆盖项）

- 未配置/密钥无效/端点不可达时：健康检查给出可读错误码，聊天 4xx/5xx 稳定错误，核心模块完全可用（以当前全量门禁结果为准）。
- 密钥不进 SQLite/日志/响应/导出：key 端点响应断言不含密钥原文；删除供应商清理安全存储（Go 测试覆盖）。
- 本地 Provider 无密钥 创建→健康→流式聊天 全链路（httptest 回环上游）；密钥端点 409 `AI_KEY_NOT_ALLOWED`；本地非回环端点与非法 kind 被拒；远程行为回归不变（Go 测试覆盖）。
- harness 单元测试：单轮直通、多轮工具循环与回填、轮数预算、取消传播、重复工具名拒绝、执行器超时/panic/截断/总预算（假 LLM/假工具覆盖）；`chatAI` 契约回归（既有 AI API 测试全绿）。
- 结构化任务/记忆 null、数组、字段错型、重复、缺失和未闭合不崩溃或建任务；历史坏消息可打开。确认复用 Task 领域事务；并发、响应丢失、刷新同身份不重复创建，改参/删除后重建受控拒绝。
- 取消/断连终止上游并保留部分内容；并发 409；启动恢复遗留生成（Go 测试覆盖）。
- 流式帧序 meta/delta/reasoning/replace/done 与 openai/anthropic 双协议映射（mock 上游 Go 测试 + 前端 SSE 解析测试覆盖）。
- 设置表单类型切换（本地隐藏密钥、提交载荷带 kind、协议锁定）、本地卡片无密钥行、聊天页「本地」标识（Web 测试覆盖）。
- 纠错/反思：工具失败回填重试与超限终止（harness 单测）；自评充分→单次调用且块被剥离、不充分→恰好一次内部修订并以 `replace` 更新界面（note 回填）、缺失/未闭合/非法块防御性剥离（API 契约测试 + harness 单测）。
- 记忆：建议块解析（合法/非法/超长）、确认落地与幂等、列表/删除、注入预算（数量/字节/超大跳过）、事件不含内容、导出排除（Go + Web 测试覆盖）。
- 上下文压缩：v56→57 加法迁移保留事实并约束单 active 快照/受控 supersede；严格 JSON 形状与摘要/事实预算、窗口外批次、水位线推进、连续快照、失败不落库、会话互斥/关闭取消、事件不含摘要正文，以及聊天注入三层上下文均有 Go 测试。
- 工具与前端：两协议单/多工具跨帧、异常参数、call/result 映射和 Harness 定义有单测；mock 会话覆盖真实 memory_write 两轮调用。持久会话 pending 提议可存操作表，但未经确认不写 ai_memories；非持久三工具仅内存。确认/忽略/删除后重试、事件脱敏和压缩状态有 Go/Web 回归。
- 稳定性：跨 origin redirect 不访问目标、同 origin redirect 可用；Provider 密钥并发写在 race detector 下保持 Keyring/SQLite 一致；已使用 Provider 删除受控拒绝且保留会话；远端错误回显密钥被脱敏；非持久会话不留正文；500 个 Unicode 字符记忆可保存。
- 显式上下文：Go 覆盖字段白名单/排除项、Unicode 截断、类型/重复/不存在来源、Provider/source 版本变化无脏写、prompt 注入、持久历史和非持久会话；Web 覆盖未预览禁发、精确预览、发送载荷、Provider 变化失效和历史来源卡片。
- 显式知识上下文：Go 覆盖 preview→chat→v2 snapshot→history、chunk/source/document 关系、source/document version 变化零写入；ModelClient/Harness 覆盖双协议不可信引用段、工具/自评轮上下文保持；Web 覆盖本地搜索、逐段选择、完整预览、精确版本载荷、历史来源 chips 和未确认禁发。
- 可验证引用：schema 059→060 保留消息并约束 completed assistant/shape/状态/数量；Go 覆盖 allowlist、空/缺失/非法/重复/越权/多块/未闭合、服务器 metadata 重建、控制块剥离与 history；Web 覆盖 strict 状态一致性、已验证来源卡、无答案/缺失/非法警告及流式块隐藏。
- 质量门禁、本地运行与分析：v4/24、6/8/24 套件严格加载；结构/关键词/有限事实规则/人工审核分层；配置身份正负例、健康不拆组、旧 NULL/审计兼容、生产提示与真实 Harness 修订/usage 链有回归。真实模型效果仍未由自动化验证。
- 运行步骤与聚合：schema 060→062 保留消息/generation 并回填 summary root/unknown usage；Harness step callback、PromptSize 字节、32 tool-call 上限、Chat 终态、OpenAI/Anthropic 完整 usage、API 禁止正文及 Web 时间线均有测试。聚合 API 另覆盖 session/provider 双过滤、空范围、终态/活动与 coverage 对账、Provider 分组、404/400；Web 会重算总计并显示 unknown/不计费边界。

### 本地模型真机验证步骤（需用户本机环境）

1. 安装并启动 Ollama（`ollama serve`，默认 11434）或 LM Studio（打开本地服务，默认 1234）。
2. 设置 → AI 助手 → 添加供应商 → 类型选「本地部署」→ Base URL 填对应地址 → 模型名填已拉取的模型（如 `qwen3`）→ 登记。
3. 点「测试连接」至「已就绪」；进入 `/ai` 选择该供应商发起会话。
4. 断开本地服务后「测试连接」应显示「端点无法访问」，聊天报稳定错误；核心模块不受影响。
5. 保持服务已就绪，在同一设置区先运行 8/8 快速检查或直接运行 24/24 完整评测；只有同配置完整套件可积累人工评审候选。该实机输出是当前模型快照的本地观察，不作为跨模型稳定真值，也不保存原回答。

## 相关 PRD 与代码链接

- [产品 PRD](../opc-workspace-PRD.md)（§5.10）
- [ADR-004：远程 Provider 接入与安全边界](../adr/004-ai-assistant-provider-access.md)
- [ADR-005：Agent Harness 架构与本地大模型接入](../adr/005-agent-harness-and-local-models.md)
- [ADR-006：Harness 完整组件矩阵与自进化边界](../adr/006-harness-matrix-memory-evolution.md)
- [ADR-007：会话上下文压缩与记忆工具](../adr/007-session-context-compaction-and-memory-tools.md)（G1–G5 已交付；[分阶段计划](../plans/context-memory-phases.md)）
- [ADR-008：AI 显式业务上下文与发送前预览](../adr/008-ai-explicit-business-context.md)（AI5 已交付；[分阶段计划](../plans/ai-explicit-context-phases.md)）
- [ADR-010：AI 显式知识片段上下文与来源引用](../adr/010-ai-explicit-knowledge-context.md)（AI6 已交付；[分阶段计划](../plans/ai-knowledge-context-phases.md)）
- [ADR-011：AI 回答的可验证知识引用](../adr/011-ai-validated-knowledge-citations.md)（AI7-Q1 已交付；[AI7 计划](../plans/ai-quality-gates.md)）
- [ADR-012：AI 运行步骤与本地无正文指标](../adr/012-ai-run-steps-and-local-metrics.md)（AI7-Q3 steps、Provider usage 与本地聚合已交付）
- [ADR-013：AI 本地模型质量评测 Actor](../adr/013-ai-local-quality-evaluation-runner.md)（AI7-Q2 显式本地运行闭环已交付）
- [ADR-014：AI 本地质量分组与时间趋势](../adr/014-ai-local-quality-trends.md)（AI7-Q2 同口径趋势已交付）
- [ADR-015：AI 本地质量类别分解](../adr/015-ai-local-quality-categories.md)（AI7-Q2 category 统计已交付）
- [ADR-016：AI 本地质量失败原因聚合](../adr/016-ai-local-quality-failure-codes.md)（AI7-Q2 failure-code 统计已交付）
- [ADR-017：AI 本地质量评测数据集 v2](../adr/017-ai-local-quality-dataset-v2.md)（AI7-Q2 12-case 多样本纵切已交付）
- [ADR-018：AI 本地质量 Wilson 区间与重复证据](../adr/018-ai-local-quality-confidence-intervals.md)（AI7-Q2 描述性不确定性已交付）
- [ADR-019：AI 本地质量人工评审候选](../adr/019-ai-local-quality-advisory-readiness.md)（AI7-Q2 advisory readiness 已交付）
- [ADR-020：AI 本地质量评测数据集 v3](../adr/020-ai-local-quality-dataset-v3.md)（AI7-Q2 24-case 事实覆盖已交付）
- [ADR-021：AI 本地质量分层评测套件](../adr/021-ai-local-quality-tiered-suites.md)（AI7-Q2 8-case smoke / 24-case full 已交付）
- [ADR-022：AI 本地质量人工决定审计](../adr/022-ai-local-quality-human-review-audit.md)（AI7-Q2 精确证据快照、必填理由与不可变历史已交付）
- [ADR-023：AI 本地质量代码所有专题套件](../adr/023-ai-local-quality-topic-suites.md)（AI7-Q2 四类 6-case 专题已交付）
- [ADR-024：AI 本地用量 UTC 时间趋势](../adr/024-ai-local-usage-time-trends.md)（AI7-Q3 1–30 天趋势已交付）
- [ADR-025：AI 运行可靠性、确认事务与评测配置身份](../adr/025-ai-reliability-confirmations-and-evaluation-identity.md)（schema 068–069；本轮验证见 AI7 计划）
- [MVP 计划草稿](../plans/ai-assistant-mvp.md)、[Harness 分阶段计划](../plans/agent-harness-phases.md)
- Sidecar：`services/sidecar/internal/api/ai_providers.go`、`ai_sessions.go`、`ai_chat.go`、`ai_messages.go`、`ai_memories.go`、`ai_context_memory.go`、`ai_evaluations.go`、`ai_evaluation_runner.go`、`ai_evaluation_reviews.go`、`internal/aieval/`、`internal/harness/`、`internal/modelclient/`、`internal/keystore/`
- 前端：`apps/web/src/pages/AiAssistantPage.tsx`、`apps/web/src/components/AiProviderSettings.tsx`、`apps/web/src/api/ai.ts`、`apps/web/src/lib/aiTaskCard.ts`
- 迁移：`services/sidecar/internal/database/migrations/052_ai_providers.sql` 至 `069_ai_reliable_confirmations.sql`
