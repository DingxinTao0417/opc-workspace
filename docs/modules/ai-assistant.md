# AI 助手模块

> 目标版本：待定（独立于 v0.1–v0.4 的独立轨道）。首个纵向切片已交付：远程 Provider 配置式接入 + 只读会话（含推理模型思考过程展示）+ 语义建任务建议卡片。远程 Provider 接入经用户 2026-09-01 明确授权并固化于 [ADR-004](../adr/004-ai-assistant-provider-access.md)；本地部署模型适配由用户显式延后，仍属后续评审范围。

## 定位与边界

AI 助手是面向用户的问答、摘要和建议入口。它帮助用户理解与组织工作，输出只读；任务创建必须经用户确认并通过既有任务领域 API 落地。

- 首版以 API key 配置式接入远程大模型；协议注册表首批支持 `openai_chat`（OpenAI chat/completions 流式）与 `anthropic_messages`（Anthropic messages 流式）。
- API 密钥只保存在操作系统安全存储（首版 Windows 凭据管理器，`zalando/go-keyring`）；不进 SQLite、`localStorage`、日志、命令行、诊断包或前端持久化。无可用安全存储的平台明确 503 拒绝，不落盘退化。
- 本版仅外发该次用户输入；不读取业务数据作上下文、不外发整库、无知识库、无遥测。任务正文不经模型外发。
- 助手回复只读，不直接创建、修改或删除任何业务数据；模型输出视为不可信预览。
- 语义建任务：识别到任务意图时回复末尾输出 `[opc:task]{...}[/opc:task]` 结构化块（系统提示词为 Sidecar 代码常量）；前端解析为**可编辑待确认建议卡片**（标题必填、描述/截止可改、项目可选），用户确认后经既有 `POST /api/v1/tasks` 创建（新建固定 `todo`），创建成功后消息挂静态引用（task_id + task_title_snapshot，不可改绑）；点击卡片跳转 `tasks/:taskId`。块缺失/非法时降级纯文本，不出现卡片。
- 不做：任务/项目/客户上下文读取、知识库检索、工具调用、Shell/SQL/任意写、自主代理、Inbox 投影、自动建任务（必须先确认）、本地部署模型适配（显式延后）。
- AI 未配置、密钥无效或端点不可达时，所有既有核心模块完全可用；AI 失败不投影 Inbox。

## 当前实现状态

首个纵向切片已交付（代码与测试为据）：

- **Provider 管理**：`ai_providers` 表（schema 052，身份/协议/端点/模型/健康/has_key，version 乐观锁与触发器）；`GET/POST /api/v1/ai/providers`、`GET/PATCH/DELETE /api/v1/ai/providers/:id`（Idempotency-Key 幂等、`ETag`/`If-Match` 并发、`ai_adapter_registered / ai_adapter_health_checked / ai_adapter_removed` 事件）；`POST /api/v1/ai/providers/:id/health` 真实连通性探测（密钥缺失 `AI_KEY_UNAVAILABLE`、401/403 映射 `AI_KEY_INVALID`、不可达 `AI_ENDPOINT_UNREACHABLE`，成功置 `ready`）；`POST /api/v1/ai/providers/:id/key` 写入 OS 安全存储（安全存储不可用 503 `AI_KEY_STORE_UNAVAILABLE`，DB 更新失败即回滚删除密钥）。
- **会话与消息**：`ai_sessions / ai_generations / ai_messages` 表（schema 053）；`GET/POST /api/v1/ai/sessions`、`GET/DELETE /api/v1/ai/sessions/:id`（删除即联取删除消息与生成并先取消活动生成）、`GET /api/v1/ai/sessions/:id/messages`（稳定倒序分页，`before_created_at + before_id` 游标）。
- **流式聊天**：`POST /api/v1/ai/chat` 以 `opc-ai-sse-v1` SSE 返回 `meta / delta / reasoning / done / error / cancelled` 事件；推理类模型（DeepSeek `reasoning_content`、OpenRouter `reasoning`、Anthropic `thinking_delta`）的思考流被单独捕获为 `reasoning` 事件并持久化到 `ai_messages.reasoning`（schema 054），前端以可折叠「思考过程」区展示，绝不混入回答正文；完成时用户消息与助手回复（原样含结构化块）同事务持久化并递增会话版本；`POST /api/v1/ai/generations/:id/cancel` 与 WebView 断连均终止上游，取消保留已生成部分（assistant 消息 `status=cancelled`）；预算：首 token 90s、总时长 10min、响应 1 MiB、提示 64 KiB；每 Provider/每会话并发 1，忙时 409 `AI_PROVIDER_BUSY`；未就绪 Provider 409 `AI_PROVIDER_NOT_READY`；Sidecar 启动把遗留 queued/streaming 标 `cancelled/AI_GENERATION_INTERRUPTED`；`ai_generation_started/completed/failed/cancelled` 事件只记脱敏元数据。
- **任务引用**：`POST /api/v1/ai/messages/:id/task` 只做静态引用落地（消息与任务存在性校验、`AI_MESSAGE_TASK_ALREADY_LINKED` 禁止改绑）；任务创建本身走既有任务 API（`task_created` 事件不重复写）。
- **前端**：`/ai` 独立页（导航「执行 → AI 助手」、命令面板入口）；会话列表/新建/删除确认、流式逐字渲染与停止按钮、输入框未就绪禁用、加载/空/错误态；语义建议卡片（可编辑标题/描述/截止/项目，缺标题禁用确认）→ 确认创建 → 已创建任务卡片（静态快照）→ 点击跳任务详情；设置「AI 助手」区（协议下拉、Base URL、模型名、API key 保存到安全存储、测试连接、删除含密钥清理）。
- **业务导出边界**：`ai_providers / ai_sessions / ai_generations / ai_messages` 明确排除出业务 JSON/ZIP 导出面（ADR-004 密钥与隐私边界），导入兼容契约覆盖 v49→53。

多供应商支持（已交付）：可登记多个 Provider（名称唯一），设置区渲染全部供应商卡片并可逐个保存密钥/测试连接/删除；聊天页在多个就绪供应商之间手动切换（输入区下拉，占位符随所选供应商变化）。尚未实现（规划，非当前能力）：任务/项目/客户上下文选择与发送前预览、知识库检索与来源引用、多 Provider 自动路由与并存并发生成（当前每 Provider 并发仍为 1）、用量统计、本地部署模型适配、非 Windows 平台安全存储验证。

## 关键用户流程

1. **配置供应商**：设置 → AI 助手 → 「添加供应商」填名称/协议/Base URL/模型名登记（可添加多个）→ 逐个保存 API key（进系统安全存储）→ 测试连接至「已就绪」。
2. **发起会话**：进入 `/ai`，多个就绪供应商时在输入区下拉选择本次使用的供应商，输入问题（Enter 发送）；回答流式逐字显示，可点「停止」，取消保留已生成部分。
3. **语义建任务**：回复中出现「建议任务」chip → 点击展开待确认卡片（标题必填可改）→「确认创建」→ 经任务 API 创建（`todo`）→ 消息显示已创建任务卡片 → 点击跳转任务详情。
4. **清理**：会话删除（级联删除消息与生成）；供应商删除（同删安全存储密钥）。

## 数据、API、状态与事件

### 数据

- `ai_providers`：id、name（唯一）、protocol（`openai_chat / anthropic_messages`）、base_url（https 或回环 http）、model、status（`unconfigured / checking / ready / unavailable / disabled`）、health_status（`unknown / healthy / unhealthy`）、health_error_code、has_key、last_health_at、version、时间戳；version 步进触发器。
- `ai_sessions`：id、title、persist、version、时间戳。
- `ai_generations`：id、session_id、provider_id、status（`queued / streaming / completed / failed / cancelled`）、error_code、content（≤1 MiB）、时间戳。
- `ai_messages`：id、session_id、role（`user / assistant`）、status（`completed / cancelled / failed`）、content、reasoning（可空，仅推理模型产生；不进入业务导出）、model_snapshot（JSON，不导出）、task_id + task_title_snapshot（成对可空，落定后不可改绑）、时间戳。
- API 密钥：仅 OS 安全存储（服务名 `opc-workspace-ai`，账户 `ai:<provider-id>:api_key`）。
- 四张表均不进入业务 JSON/ZIP 导出（操作态与隐私边界，同 ADR-004）。

### API

- `GET / POST /api/v1/ai/providers`、`GET / PATCH / DELETE /api/v1/ai/providers/:id`、`POST /api/v1/ai/providers/:id/health`、`POST /api/v1/ai/providers/:id/key`
- `GET / POST /api/v1/ai/sessions`、`GET / DELETE /api/v1/ai/sessions/:id`、`GET /api/v1/ai/sessions/:id/messages`
- `POST /api/v1/ai/chat`（SSE `opc-ai-sse-v1`）、`POST /api/v1/ai/generations/:id/cancel`、`POST /api/v1/ai/messages/:id/task`

### 状态与事件

- Generation 状态链：`queued / streaming → completed | failed | cancelled`；终态不可改（仅启动恢复把遗留活跃态标 cancelled）。
- Workflow Event（脱敏，不含提示/回答/任务正文）：`ai_adapter_registered / ai_adapter_health_checked / ai_adapter_removed`（aggregate `ai_provider`）、`ai_generation_started / ai_generation_completed / ai_generation_failed / ai_generation_cancelled`（aggregate `ai_generation`）。
- 主要错误码：`AI_PROVIDER_NOT_FOUND / AI_PROVIDER_NAME_TAKEN / AI_PROTOCOL_INVALID / AI_ENDPOINT_INVALID / AI_MODEL_INVALID / AI_PROVIDER_NOT_READY / AI_PROVIDER_DISABLED / AI_PROVIDER_BUSY / AI_KEY_UNAVAILABLE / AI_KEY_INVALID / AI_KEY_STORE_UNAVAILABLE / AI_KEY_MALFORMED / AI_STREAM_ERROR / AI_GENERATION_TIMEOUT / AI_GENERATION_NOT_ACTIVE / AI_GENERATION_ALREADY_TERMINAL / AI_MESSAGE_TASK_ALREADY_LINKED`。

## 与其他模块协作

- **任务**：任务创建一律经既有 `POST /api/v1/tasks` 门禁（用户在建议卡片显式确认）；消息仅保存静态快照引用，不跟踪任务状态，不触碰任务状态机；取消误建任务须在任务模块操作。
- **设置**：Provider 配置区挂在设置「AI 助手」模块，独立自持保存（不走共享 draft/preview）。
- **数据管理**：AI 四表为操作态/隐私边界，排除出业务导出；一致性备份（SQLite 快照）仍覆盖它们。
- **诊断/日志**：普通日志只记 provider/generation ID、阶段与错误码；密钥由 operationlog 的 secrets 机制脱敏。
- **本地 Agent（v0.2）**：互不共享能力。Agent Adapter 是受控执行器（ADR-003 匿名管道）；AI 助手是远程只读对话，不获得任何执行能力。

## 分阶段实施

1. **AI2 评审**（已完成）：ADR-004 固化远程 Provider 授权、密钥与数据外发边界、语义建任务机制。
2. **AI3 Adapter 基线**（已完成）：Provider 登记/健康/密钥 API 与设置区、schema 052。
3. **AI4 只读会话**（已完成）：schema 053、SSE 流式聊天、取消/断连/超时/并发闸门、启动清理、会话与消息 API、`/ai` 页面。
4. **AI4.5 语义建任务**（已完成）：代码所有系统提示词、结构化块解析、待确认卡片、既有任务 API 落地与静态引用。
5. **AI5 显式上下文**（规划）：任务/项目/客户上下文选择、发送前范围预览、最小快照。
6. **AI6 知识库与来源**（规划）：依赖本地知识库；引用可定位与“未检索到可靠来源”处理。
7. **AI7 质量闸门与扩展**（规划）：提示注入/越权/资源耗尽测试集、多 Provider、本地部署适配（需新 ADR 与授权）。

## 验收标准（当前切片已覆盖项）

- 未配置/密钥无效/端点不可达时：健康检查给出可读错误码，聊天 4xx/5xx 稳定错误，核心模块完全可用（Web 全量 1033 测试、Go 全量套件通过）。
- 密钥不进 SQLite/日志/响应/导出：key 端点响应断言不含密钥原文；删除供应商清理安全存储（Go 测试覆盖）。
- 结构化块缺失/非法不建任务；确认创建只经既有任务 API 且新建为 `todo`（解析器与页面测试覆盖）。
- 取消/断连终止上游并保留部分内容；并发 409；启动恢复遗留生成（Go 测试覆盖）。
- 流式帧序 meta/delta/done 与 openai/anthropic 双协议映射（mock 上游 Go 测试 + 前端 SSE 解析测试覆盖）。

## 相关 PRD 与代码链接

- [产品 PRD](../opc-workspace-PRD.md)（§5.10）
- [ADR-004：远程 Provider 接入与安全边界](../adr/004-ai-assistant-provider-access.md)
- [MVP 计划草稿](../plans/ai-assistant-mvp.md)
- Sidecar：`services/sidecar/internal/api/ai_providers.go`、`ai_sessions.go`、`ai_chat.go`、`ai_messages.go`、`internal/modelclient/`、`internal/keystore/`
- 前端：`apps/web/src/pages/AiAssistantPage.tsx`、`apps/web/src/components/AiProviderSettings.tsx`、`apps/web/src/api/ai.ts`、`apps/web/src/lib/aiTaskCard.ts`
- 迁移：`services/sidecar/internal/database/migrations/052_ai_providers.sql`、`053_ai_sessions.sql`
