# Agent Harness 纵切——分阶段实施计划

> 状态：已实施完成（2026-09-03 夜间自主执行；每阶段验收证据见下，未提交代码，等用户确认）
> 授权与边界：[ADR-005](../adr/005-agent-harness-and-local-models.md)（用户 2026-09-03 授权：harness 核心组件 + 多协议 + 本地大模型 + 知识库扩展点仅设计；本轮仅做 AI 助手与模型设置内容，不新增模块联动；完成后不提交代码）
> 流程约束：每阶段真实测试验证通过才进入下一阶段；全部阶段完成后做回归测试。

## 背景与目标

把 `chatAI` 的单次调用内联管线演进为 harness 运行时（运行循环、LLMClient 接口、工具注册/执行机制、上下文组装扩展点、预算集中），并接入本地大模型（OpenAI 兼容端点、无密钥、回环防护）。外部行为约束：`opc-ai-sse-v1` SSE 契约、持久化行为、语义建任务卡片流程全部不变。

## 阶段 A：设计文档 ✅

1. `docs/adr/005-agent-harness-and-local-models.md`：架构决策（harness 组件、本地 Provider kind、多协议扩展路径、知识库扩展点、被拒方案）。
2. 本计划文档：阶段拆分、每阶段验收标准。

**验收证据**：两份文档已交付；`pnpm check:docs` 28 个 Markdown 文件与本地链接通过。

## 阶段 B：Harness 核心（Sidecar `internal/harness`）✅

### 规格与验收标准

1. `harness.Run`：以 `LLMClient` 接口执行"调用→（工具调用→执行→回填→下一轮）→最终回复"循环；最大轮数预算（默认 8）；无工具时单轮即返回，行为与现管线等价。
2. `Tool`/`Registry`：名称唯一、显式 allowlist、有序注册；`Executor`：单工具超时 30s、结果上限 64 KiB、panic 恢复为错误。
3. 取消传播：`ctx` 取消立即终止循环与执行中的工具。
4. `modelclient` 适配 `LLMClient`（包装 `StreamChat`，累计正文/思考流）；`chatAI` 改经 harness，SSE 事件与持久化路径不变。
5. 生产不注册任何工具（ADR-004/005 边界）。

**验收证据（真实运行）**：`internal/harness` 14 个单元测试全绿（假 LLMClient/假工具：单轮直通、两轮工具循环与回填、轮数预算耗尽、取消传播、重复工具名/空名/nil 拒绝、执行器超时/panic/截断/父取消优先、总预算触发）；`internal/api` 全部 AI 测试与后续全量套件全绿（SSE 契约回归）。gofmt/vet 干净。

## 阶段 C：本地大模型接入（Schema 055 + Provider API）✅

### 规格与验收标准

1. 迁移 `055_ai_provider_kind.sql`（加法）：`ai_providers` 增 `kind TEXT NOT NULL DEFAULT 'remote' CHECK (kind IN ('remote','local'))`；业务导入兼容契约覆盖 schema 55。
2. Provider API：create/patch 接受 `kind`；`kind=local` 强制回环 http 端点且协议固定 `openai_chat`（Go 校验）；响应/事件带 `kind`。
3. 密钥规则：`kind=local` 不允许保存密钥（`POST /key` → 409 `AI_KEY_NOT_ALLOWED`）；聊天跳过密钥库；远程行为不变（无密钥仍 `AI_KEY_UNAVAILABLE`）。
4. 健康探测与流式聊天复用 `openai_chat` 适配器（本地即 OpenAI 兼容端点）。

**验收证据（真实运行）**：`internal/api` 3 个新测试全绿——本地无密钥 创建→健康→流式聊天（httptest 回环上游）+ 密钥端点 409 + 不落密钥；本地非法端点/协议/kind 拒绝 + 默认 remote + patch 往返；本地不可达健康 `AI_ENDPOINT_UNREACHABLE` + 聊天 409 `AI_PROVIDER_NOT_READY`。`internal/database` 47 个测试文件 schema 断言 bump 至 55 后全量绿；业务导入"更新 schema 拒绝"夹具随 55 入契约升至 56。修复存量 flaky 测试：`TestAutomationBusinessImportAcceptsPortableNonretryableSafetyFailures` 依赖测试执行时刻（真实时钟写审计事件 vs 固定时钟造 run），给 `newBackupTestAPI` 增加可选冻结时钟并在该测试注入——此前 UTC 10:00 后运行必然失败，MVP 全量绿属时间窗巧合。`internal/api` 全量 321s 绿。

## 阶段 D：前端（模型设置 + AI 助手页）✅

### 规格与验收标准

1. `AiProvider.kind` 类型；设置表单新增「类型」选择（远程 API / 本地部署）；本地隐藏密钥输入并给出常见本地服务示例（Ollama `http://127.0.0.1:11434/v1`、LM Studio `http://127.0.0.1:1234/v1`）。
2. AI 页供应商下拉区分「本地」徽标；本地供应商无密钥也能就绪并被选择。
3. 错误提示映射新增 `AI_KEY_NOT_ALLOWED`。

**验收证据（真实运行）**：`AiProviderSettings.test.tsx` 8 个测试全绿（原登记载荷补 kind、本地模式提交 kind=local + 协议锁定 + Ollama 提示、本地卡片无密钥行、`AI_KEY_NOT_ALLOWED` 人话映射）；`npx tsc --noEmit` 干净；vitest 全量 112 文件 / 1039 测试全绿；`vite build` 成功；prettier 格式化通过。

## 阶段 E：回归测试与文档同步 ✅

1. Go 全量（`go vet ./...` + `go test ./...`）；Web 全量（typecheck + vitest 全量 + build）；`check:docs`；gofmt/prettier。
2. 文档同步：`docs/modules/ai-assistant.md`（harness 架构、本地 Provider、kind 字段、错误码、真机验证步骤）、PRD §5.10、`docs/functional-architecture.md`、`docs/modules/README.md`、`docs/README.md`、根 `README.md`（schema 55 基线）。
3. 明确剩余限制：真机本地模型联调需用户环境（步骤已写入模块文档）；工具机制未向生产注册任何工具；知识库仅扩展点。

**验收**：见文末"回归测试结果"；`git status` 显示改动未提交（按用户要求不提交）。

## F 阶段组：完整组件矩阵与自进化（2026-09-03 用户追加授权，见 ADR-006）

### F1 纠错 + 反思（harness 能力扩展）✅

1. 纠错：工具失败以错误结果回填，模型可在预算内重试（单次运行失败上限 3）。
2. 反思（自发自评，无用户开关）：代码所有提示词要求模型结束前自评并输出 `[opc:selfcheck]` 结论块；harness 解析后自主决策——不充分自动触发一次静默修订轮（note 回填），充分直接输出；自评块防御性剥离永不落库/展示；SSE 契约与请求参数不变。

**验收证据（真实运行）**：harness 20 个单测全绿（纠错回填重试/超限终止、自评充分单调用、不充分恰好一次修订且 note 回填、缺失/未闭合/非法块防御性剥离、修订失败容忍、相同修订不替换）；api 契约测试 `TestAIChatSelfCheckDrivesAutonomousRevision` / `TestAIChatSelfCheckAffirmativeStaysSingleCall` 全绿（持久化内容不含自评块）；Web 套件全绿（流式渲染剥离自评块）。

### F2 长期记忆（自进化闭环，数据层进化 + 人工门禁）✅

1. 迁移 056：`ai_memories`（id、content、source_message_id 可空、created/updated）；排除出业务导出（两份分类清单同步）。
2. 系统提示词增加记忆块指令（`[opc:memory]{"content":"..."}`，代码所有）；`Request.Memories` 注入已确认记忆（前 20 条、8 KiB 预算），openai/anthropic 两侧 system 合并一致。
3. API：`GET/POST/DELETE /api/v1/ai/memories`（POST 幂等 + `ai_memory_created/deleted` 事件）。
4. 前端：建议块解析为待确认卡片 → 确认创建；设置「AI 助手」区记忆列表与删除。

**验收证据（真实运行）**：解析器测试（合法/非法/缺 content/双块剥离）；api `TestAIMemoryLifecycle`（创建/幂等重放/校验/列表/删除/事件不含内容）、`TestAIChatInjectsConfirmedMemories`（mock 上游断言 system 含记忆且代码提示词保留、不落消息表）、`TestAIMemoryInjectionBudgets`（数量/字节预算、超大跳过）全绿；Web 页面记忆卡片确认流与设置区列表/删除测试全绿。

### F3 运行追踪与时间线（设计就绪，未实施）

- `ai_runs`/`ai_run_steps`（llm/tool/reflection 步骤、耗时、结果摘要）+ 会话内执行时间线 UI；随工具纵切与真实数据一起交付，不提前建空表。

### F4 编排、规划与子代理（仅设计）

- 依赖 F3 与真实工具；需独立评审授权。

## 回归测试结果（2026-09-03，F 阶段组完成后）

- Web：typecheck 干净；vitest 112 文件 / 1045 测试全绿；prettier 干净。
- Go：`go vet ./...` 干净；全量套件通过——cmd/server、api（519s，含纠错/反思/记忆新测试）、database（148s，schema 56）、harness、config、operationlog、runlease 全部 ok。
- 文档：`pnpm check:docs` 31 个 Markdown 全过。

## 回归测试结果（2026-09-03，E 阶段后首次全量）

- Go：`go vet ./...` 干净；`go test ./...` 全部包通过（api 321s 含全部 AI/业务导入/自动化导入测试、database 38s schema 55、harness/modelclient/config/operationlog/runlease 全绿）。
- Web：typecheck 干净；vitest 112 文件 / 1039 测试全绿；`vite build` 成功（chunk 体积警告为存量提示）。
- 文档：`pnpm check:docs` 通过；gofmt / prettier 无差异。
- 未提交：全部改动留在工作区，等待用户确认后决定提交。

## 完成标准

- 实现与 ADR-005 授权范围一致；每阶段有真实测试证据；外部契约（SSE、任务卡片、远程 Provider 行为）无回归；文档如实反映实现；不擅自提交。
