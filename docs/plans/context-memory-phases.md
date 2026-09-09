# 会话上下文压缩与记忆工具——分阶段实施计划

> 状态：G1–G5 功能实现完成（[ADR-007](../adr/007-session-context-compaction-and-memory-tools.md)）；相关门禁已通过，仓库全量 API 仍有两项可在未修改 AI 分支复现的 Automation 基线失败
> 授权：用户 2026-09-03 确定长程上下文方向（摘要 + 关键信息提取 + 滚动窗口 + 外部存储与工具调用）；G3/G4 将注册首个真实工具（记忆工具），系对 ADR-004 工具禁令的首次正式突破
> 流程约束：每阶段真实测试验证通过才进入下一阶段；全部阶段完成后回归测试；开发完成不提交代码，等用户确认

## 背景

当前长程上下文已经具备最新完整回合滚动窗口、长期记忆、前情摘要、关键事实注入和会话工作状态工具。目标形态已经落地：**分层上下文栈**（系统提示 / 长期记忆 / 前情摘要 / 关键事实 / 滚动窗口，各段独立预算）+ **后台异步压缩**（append-only 快照）+ **记忆工具**（push 保底、pull 增强）。

## G1：组装器重构与窗口修复（已交付）

1. `chatHistory` 读取最新 200 条后恢复时间正序，按完整 user turn 从最新往回装载；修复 `ORDER BY ASC LIMIT 200` 取到最老 200 条的方向性 bug。
2. 历史助手消息发送前剥离 `[opc:task]` / `[opc:memory]` / `[opc:selfcheck]` 块，用户原始消息不做协议块改写。
3. OpenAI/Anthropic 均按最终序列化请求体计算 64 KiB 预算；当前用户回合不会被静默裁掉，单回合超限在持久化会话/消息/generation 前返回 `AI_PROMPT_TOO_LARGE`。
4. 摘要/事实两段留空（G2 接入）。

**验收**：模型请求尺寸单测与 AI API 集成测试覆盖精确预算、200-bug 回归、最新/current 保留、历史助手控制块剥离和超限无脏会话；全量门禁结果见本次交付记录。

## G2：压缩管线与注入（已交付）

1. 迁移 057（加法）：`ai_memory_entries` append-only 快照表；业务导入兼容契约与导出排除同步扩展。
2. 压缩管线：Run 收尾后异步触发（窗口外 >16 KiB、会话互斥）、单批 ≤32 KiB、严格 JSON 快照（summary ≤4 KiB + facts ≤32 条）、同事务落库 + 水位线推进、`ai_session_compacted` 脱敏事件、失败静默降级为硬截断。
3. 注入接入：前情摘要 ≤4 KiB + 关键事实 ≤4 KiB 两段进组装器（最新 active 快照）。

**验收**：假 LLM 单测（快照延续、水位线推进、失败降级、并发互斥、事件不含内容）；mock 上游注入断言（各段存在、预算不超）；数据库迁移测试；全量回归。

**交付记录（2026-09-08）**：schema 057 新增 `ai_memory_entries`，约束单 active `context_snapshot`、受控 active→superseded 与源消息/会话级联；Run 成功后按 16 KiB 阈值和 32 KiB 批次后台压缩，严格校验 summary/facts JSON 与 4 KiB/32 条预算，快照替换和水位线在同一事务提交并写脱敏事件。Sidecar 关闭、会话或 Provider 删除会取消对应压缩；失败不推进水位线。Go 专项覆盖连续快照、失败降级、互斥/取消、事件脱敏与聊天注入；最终全量门禁结果以本次交付汇报为准。

## G3：工具调用协议（已交付）

1. `openai_chat` / `anthropic_messages` 两适配器：请求侧下发工具定义（名称/描述/参数 JSON Schema）、流式侧解析 tool_call 增量并聚合多工具调用。
2. harness：`Request.Tools` 下发、`Turn.ToolCalls` 承载真实解析结果、运行循环与纠错预算照旧；SSE 契约不变（工具过程不外显，F3 时间线另立）。

**验收**：两协议 tool_call 流式解析单测（mock 上游增量帧：单工具、多工具、跨帧参数拼接、异常帧）；harness 端到端（假工具真实走协议解析）；既有套件全绿。

**交付记录（2026-09-08）**：Provider 中立 `ToolDefinition/ToolCall` 已接入模型请求与 Harness。OpenAI 下发 function tools 并聚合跨帧 `tool_calls`，Anthropic 下发 `input_schema` 并聚合 `tool_use/input_json_delta`；两侧都把 assistant tool call 与 tool result 映射回各自协议。参数必须聚合成 JSON object，名称/参数字节计入响应预算；异常帧安全失败。Harness 每轮只下发当前 Registry 的显式 allowlist，并在后续轮携带 call ID 与结果。

## G4：记忆工具（已交付）

1. `memory_write`（会话级事实，agent 自发、无需确认、随会话亡）、`memory_propose`（持久记忆仅提议 pending，经既有确认卡片落地）、`memory_search`（关键词/tags 检索 active 条目与历史消息，确定性检索无向量）。
2. 三工具经 allowlist 注册进 harness，工具执行预算与纠错机制复用。

**验收**：三工具行为与边界测试（write 自发落库、propose 不经确认绝不持久化、search 命中与空态、预算约束、事件脱敏）；端到端 mock 会话（agent 通过工具读写工作状态）。

**交付记录（2026-09-08）**：生产 Registry 只注册 `memory_search / memory_write / memory_propose`。`memory_write` 写会话级 active fact 并精确去重，`memory_search` 在当前会话 active snapshot/fact 与历史消息中做确定性关键词/标签检索，`memory_propose` 只创建 pending proposal；proposal 经对话卡片或设置区显式确认后才进入 `ai_memories`，忽略则受控 supersede。三类工具均使用严格参数 Schema、数量/字符/字节预算与脱敏事件；mock 上游两轮工具调用金链已覆盖真实协议解析、执行与结果回填。

## G5：前端透明化与管理增强（已交付）

1. 会话详情展示"已压缩至 N 条"标记（透明性，不展示摘要全文）。
2. 设置区长期记忆管理扩展（pending 提议列表的确认/驳回入口，复用记忆卡片流）。

**验收**：Web 测试（标记渲染、提议确认/驳回流）；typecheck + vitest 全量；构建。

**交付记录（2026-09-08）**：会话列表响应返回 active 水位线覆盖的 `compacted_message_count`，AI 页显示“已压缩前 N 条消息”并每 15 秒刷新会话事实。设置区新增 pending proposal 列表、来源会话、标签和“记住/忽略”入口；确认与对话卡片复用同一幂等长期记忆 API，前端 Query 同步失效 pending/confirmed 两组缓存。

## 回归与收尾

- 全量门禁：Go（vet + 全量 test）、Web（typecheck + vitest + build）、check:docs、gofmt/prettier。
- 文档同步：模块文档、PRD §5.10、架构文档、索引、根 README（schema 057 基线与能力表述）。
- 剩余限制如实标注：压缩质量依赖所配模型；无向量检索；token 预算后置；F3 步骤追踪另行。

**本次验证（2026-09-08）**：Web typecheck、113 文件/1063 测试、生产构建、文档/Prettier/gofmt、Go vet、数据库/modelclient/harness/cmd/config/operationlog/runlease 全量及 AI/业务导入兼容专项通过；Provider 密钥/压缩并发专项通过 race detector。`internal/api` 全量除 `TestAutomationEventDeliveryReplayDoesNotRecaptureCompletedRunAfterRuleEdit` 与 `TestDisabledEventAutomationRunStillRetriesWhenDue` 外通过；两项在未修改的 `origin/feature/ai-assistant@57cd42d` 同样失败，属于既有 Automation 时间基线问题，不由 G2–G5 引入。

## 完成标准

- 实现与 ADR-007 一致；每阶段真实测试证据；长会话下"前情"经压缩保留、agent 可自发读写会话工作状态、持久记忆门禁不松；外部契约无回归；不提交代码。
