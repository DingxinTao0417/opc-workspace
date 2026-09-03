# AI 助手 MVP 实施记录

> **状态**：已评审通过并实施完成（2026-09-01）。实现状态与验收证据以 [AI 助手模块文档](../modules/ai-assistant.md) 为准；本文保留为实施记录。
> **日期**：2026-09-01
> **原实施分支**：`feature/ai-assistant`（不记录动态领先/落后状态；以 Git 事实为准）
> **排期**：独立于 v0.1–v0.4 的独立轨道；目标版本仍待定。
> **说明**：本文保留首个远程 Provider MVP 的原始范围和实施记录。当前事实以 `docs/modules/ai-assistant.md`、PRD §5.10、功能架构及后续 ADR-005/006/007 为准；后续已增加本地 Provider、Harness、长期记忆、自评修订与 G1 上下文窗口。

## 范围

- 以 API key 配置式接入远程大模型，多协议（首批 `openai_chat` / `anthropic_messages`，协议注册表可扩展），**不做本地部署模型适配**（用户显式延后）。
- 语义识别 + 建议卡片待确认建任务（Linear 式）：模型只做语义理解与字段推断，创建经既有任务 API、人工确认后才落地；会话内显示已创建任务卡片（静态快照），点击跳转任务详情。
- Provider 登记/健康/密钥管理、只读会话、SSE 流式聊天、取消、会话管理、语义建任务（建议卡片→确认→既有任务 API→已创建卡片→跳转）。
- **不做**：任务/项目/客户上下文读取、知识库、其他模块联动（无 Inbox 投影、无遥测）、工具调用、本地部署适配。

## 1. ADR（新增，文档先行）`docs/adr/004-ai-assistant-provider-access.md`

- 状态：已接受（本纵切）；记录用户对远程 Provider 与语义建任务的明确授权、日期、对应任务（AI2 评审 + AI3/AI4 会话纵切）。
- 协议适配器：`openai_chat`（chat/completions 流式）、`anthropic_messages`（messages 流式）；映射器注册表可扩展。
- 数据外发边界：远程请求包含代码所有系统提示、所选会话预算内历史和当前用户输入；后续长期记忆纵切还会注入已确认记忆。不读业务对象、不外发整库、无知识库、无遥测。确认卡片再次编辑的字段只提交本地任务 API，不二次发送给模型。
- 语义建任务机制：系统提示词（Sidecar 代码所有常量）指示模型在识别到任务意图时于回复末尾输出结构化块（标题必填，描述/截止尽力推断）；前端解析为**可编辑待确认卡片**。模型输出被视为不可信预览：不设自动创建路径，缺标题时确认不可用（服务端 `title` 必填同样强制）。
- 密钥边界：API key 仅存操作系统安全存储（首版 Windows 凭据管理器），Sidecar 进程内用后即弃；不进 SQLite/localStorage/日志/命令行/诊断包；access log 不记录密钥请求体（operationlog sanitize）。
- 失败边界：provider 不可用/超时/限流/无 key → 稳定错误码；取消与断链终止上游；AI 不可用时核心模块完全可用。结构化块缺失时保留自然语言回复、不建任务；存在协议标记但 JSON 非法或块未闭合时隐藏协议文本并显示自然语言失败说明，同样不建任务。
- 撤销：删除 Provider 配置即撤销（同删安全存储条目）；已建任务按任务模块既有 `cancel` 命令撤销，AI 侧不做 undo。
- 被拒方案：本地部署适配（显式延后）、SQLite/localStorage 存密钥、WebView 直达 provider、模型直接调任务 API/自动建任务（必须先确认）、工具调用、未经确认落地任务。

## 2. Go Sidecar（核心改动）

- 迁移（纯加法，无 destructive）：
  - `052_ai_providers.sql`：`ai_providers`（id、name、protocol、base_url、model、status unconfigured/checking/ready/unavailable/disabled、health_status、health_error_code、last_health_at、version、时间戳；触发器仿 `034_agent_adapters.sql`：身份不可变、版本步进、禁硬删）。
  - `053_ai_sessions.sql`：`ai_sessions`（id、title、persist、时间戳、版本）+ `ai_generations`（id、provider_id、status queued/streaming/completed/failed/cancelled、error_code、部分正文、时间戳）+ `ai_messages`（id、session_id、role、status、content、model_snapshot、**task_id 可空 + task_title_snapshot 可空**、时间戳）。
- `internal/models/ai_*.go`：GORM 模型。
- `internal/api/ai_providers.go`（仿 `agent_adapters.go`：Idempotency-Key+哈希回放、ETag/If-Match、错误码、事件）：
  - `GET/POST /api/v1/ai/providers`、`GET/PATCH /api/v1/ai/providers/:id`、`POST /api/v1/ai/providers/:id/health`（真实连通性探测+模型枚举）、`DELETE /api/v1/ai/providers/:id`。
  - `POST /api/v1/ai/providers/:id/key`：写入 OS 安全存储（zalando/go-keyring；Windows 凭据管理器无 cgo；macOS/Linux 按平台验证，不可用 503 明确拒绝，不落盘退化）。
- `internal/api/ai_sessions.go`：`GET/POST /api/v1/ai/sessions`、`GET/DELETE /api/v1/ai/sessions/:id`、`GET /api/v1/ai/sessions/:id/messages`（稳定分页，返回 task_ref）。
- `internal/api/ai_chat.go` + `internal/modelclient/`：
  - 代码所有的 system prompt 常量（任务意图→结构化块指令、拒绝越界请求、只读基调）。
  - `POST /api/v1/ai/chat`（body：provider_id、session_id?、message）→ 协议映射器 → 上游 HTTP 流式请求（key 经 go-keyring 读入内存用后即弃）→ 翻译为 `opc-ai-sse-v1` 事件（meta/delta/done/error/cancelled）→ gin 流式写；完成时持久化用户消息与助手回复（正文原样含结构化块，前端显示时剥离）。
  - `POST /api/v1/ai/generations/:id/cancel`；WebView 断连（ctx.Done）同样终止上游；取消保留已生成部分并标 cancelled（incomplete）。
  - 超时/并发闸门：首 token 90s、总时长 10min、响应 1 MiB、提示 64 KiB；每 provider+每 session 最多 1 个活跃生成，忙时 409 `AI_PROVIDER_BUSY`；Sidecar 启动把遗留 streaming/queued 标 cancelled。
- `internal/api/ai_messages.go`：`POST /api/v1/ai/messages/:id/task`（body：task_id）——任务创建本身由既有 `POST /api/v1/tasks` 领域 API 完成（AI 模块不复制任务逻辑）；本端点只做静态引用落地：只读校验消息存在、任务存在且可读，写 task_id + task_title_snapshot（不可改绑、稳定快照），返回更新后消息。
- 事件（脱敏元数据）：`ai_adapter_registered / ai_adapter_health_checked / ai_adapter_removed / ai_generation_started / ai_generation_completed / ai_generation_failed / ai_generation_cancelled`；任务创建各自使用既有 `task_created` 事件。
- 错误码沿用 `writeError` 大写固定码（`AI_PROVIDER_NOT_FOUND`、`AI_ENDPOINT_INVALID`、`AI_PROVIDER_BUSY`、`AI_STREAM_ERROR`、`AI_GENERATION_TIMEOUT`、`AI_KEY_UNAVAILABLE` 等）。
- 日志：只记 provider/generation ID、阶段、耗时、白名单错误码；提示/回答/任务正文不进日志。

## 3. 前端（React/WebView）

- `types/models.ts`：`AiProvider`、`AiSession`、`AiMessage`（含 taskRef）、`AiGeneration`、`AiChatStreamEvent`。
- `api/client.ts`：providers CRUD/health/key、sessions、messages、`POST messages/:id/task`；`api/ai.ts`：`streamAiChat`（复用鉴权头，`accept: text/event-stream` + `ReadableStream` 解析 SSE + `AbortController`）；`lib/aiTaskCard.ts`：结构化块解析工具（取首个合法块，非法→null，字段 title/description/due，标题必填校验）。密钥输入只走受控端点，前端不持久化 key。
- `api/hooks.ts`：`["ai","providers"]` / `["ai","sessions"]` / `["ai","messages",id]` query key；hooks：providers 查询/登记/健康/删除/保存 key、sessions、messages、`useAiChatStream` 自定义流式 hook、`useAttachTaskToMessage`。
- 页面 `pages/AiAssistantPage.tsx`：Provider 就绪横幅（未登记→引导设置）、会话列表/新建/删除（确认弹窗）、消息区（流式逐字渲染+停止按钮；回复正文显示时剥离结构化块）、输入框（provider 未就绪禁用），加载/空/错误态齐全（复用 `feedback.tsx`）。
- **语义建任务（Linear 式建议卡片）**：
  - 助手回复中解析出结构化块 → 该回复下方出现**待确认建议卡片**：标题必填（可编辑，缺标题时确认按钮禁用）、描述/截止可编辑（尽力推断值预填）、项目可选（复用既有 `ProjectSelect`）；优先级/标签/负责人/验收策略不暴露走默认；卡片上同时给"取消创建"（丢弃卡片，不影响会话）。
  - 用户点"确认创建" → 调既有 `createTask`（`POST /api/v1/tasks` 门禁，新建固定 todo）→ 成功 → `POST /api/v1/ai/messages/:id/task` 落地静态引用 → 失效率应缓存 → 卡片转"已创建任务"（标题+快照）。
  - 点击已创建卡片 `navigate("/tasks/{taskId}")`（既有 `tasks/:taskId` 路由打开详情）；卡片静态快照，不实时跟踪。
  - 无结构化块 → 纯文本回复，不出现卡片（降级路径）。
- 设置区 `components/AiProviderSettings.tsx`（仿 `AgentAdapterSettings.tsx`）：协议下拉、名称/base URL/模型名、API key 输入保存、测试连接、状态展示、删除（确认；删 DB 记录+安全存储条目）。注册 `store/ui.ts` SettingsModule 加 `"ai"`、`SettingsModal.tsx` modules 数组与 moduleContent/页脚分支。
- 导航：`App.tsx` 加 `<Route path="ai">`、`Sidebar.tsx` 新增 "AI" 组（`{label:"AI 助手", to:"/ai", icon: Sparkles}`）、`CommandPalette.tsx` 加 `/ai` 项。

## 4. 测试与门禁

- Go：provider CRUD/健康/删除、key 端点（写后即弃、日志脱敏断言）、chat SSE（httptest 模拟 OpenAI/Anthropic 流式上游，断言 meta/delta/done 帧序）、取消（部分正文保留+状态 cancelled）、断连终止、并发 409、超时、启动清理、`messages/:id/task` 引用落地（存在性/不可改绑/快照）、事件写入、安全存储不可用映射、system prompt 常量存在性。
- Web：`aiTaskCard` 解析工具测试（合法/非法/缺标题）、`AiAssistantPage.test.tsx`（流式渲染/停止/空会话/错误/建议卡片→确认→已创建→跳转）、`AiProviderSettings.test.tsx`、`useAiChatStream`（mock fetch `ReadableStream` SSE）测试。
- 门禁：`pnpm check:source` 全绿（prettier/gofmt、check-docs 链接存活、web typecheck+test+build、go vet+test+build:sidecar）。不改 Rust/桌面层，`check:rust` 不必扩展；真机联调需用户自备 API key（门禁全程 mock）。

## 5. 文档同步（评审通过并实施后，按 AGENTS.md 要求同次交付）

- 新增 ADR-004（见 1）。
- `docs/modules/ai-assistant.md`：定位与边界改写（远程 Provider 授权生效；语义建任务机制与"建议卡片待确认"边界；本地部署标注"用户显式延后"）；实现状态更新（AI2 评审 + AI3/AI4 会话纵切已交付）；API/状态机/事件按实际代码更新；分阶段表更新；目标版本仍待定。
- `docs/opc-workspace-PRD.md` §5.10：状态与授权记录（ADR-004）更新；排期标注独立轨道。
- `docs/functional-architecture.md`：§3.2 补 AI 模块事实句；§5 模块协作表 AI 助手行更新；§11 依赖顺序注明"AI 助手远程只读会话纵切独立于知识库先推进，无检索能力"；同步校正过期 schema 基线声明（文档"v44" vs 代码实际 051 → 本次 052/053，不虚构 045–051 功能）。
- `docs/modules/README.md` + `docs/README.md`：状态行、链接、基线校正（check-docs 验证）。
- 根 `README.md`：交付范围补"AI 助手只读会话（远程 Provider 配置式 + 语义建任务建议卡片）"。
- 迁移与接口说明：052/053 为加法迁移、无 destructive、回滚兼容与验证方法。

## 6. 验证顺序与汇报

1. 确认分支/工作树（已在 `feature/ai-assistant`）。
2. 按序实现：ADR → 迁移 → models → Sidecar API → 前端解析/页面/设置/导航 → 测试。
3. `pnpm check:source` 全绿；`git diff` 同审代码与文档一致。
4. 完成标准：实现与授权范围一致、测试通过、模块文档反映真实状态、PRD/架构/索引/README 已同步、不把占位当已交付。
5. 不擅自提交/推送；汇报后由用户确认是否 `git commit` / `git push`（AGENTS.md 要求）。

## 剩余限制（如实标注）

- 真实 provider 联调需本机提供 API key 与可达端点；门禁全程 mock。
- Windows（凭据管理器）为首版验证平台；macOS/Linux 安全存储与端点验证作为平台门禁后续补齐，不可用明确禁用不落盘。
- 语义识别的完整性依赖模型输出结构化块；块缺失时只显示模型自然语言，块非法或未闭合时显示自然语言失败说明（均不建任务），后续可加"手动唤起建议卡片"兜底。
- 卡片为静态快照+跳转；任务编辑/验收/取消仍完全在任务模块内完成，AI 不触碰任务状态机。
- 任务/项目/客户上下文读取、知识库来源引用、用量统计、多 provider 并发路由为后续纵切。
