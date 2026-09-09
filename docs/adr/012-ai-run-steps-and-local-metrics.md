# ADR-012：AI 运行步骤与本地无正文指标

- 状态：Accepted，AI7-Q3 运行步骤、Provider usage、本地聚合与 UTC 时间趋势已实现；费用未启用
- 日期：2026-09-08
- 决策范围：AI generation 可解释性、运行步骤、本地用量基线
- 相关文档：[AI 助手](../modules/ai-assistant.md)、[AI7 计划](../plans/ai-quality-gates.md)、[ADR-011](011-ai-validated-knowledge-citations.md)

## 背景

AI 会话已经能解释显式上下文与引用，但一个 generation 经历了多少模型轮、是否调用记忆工具、是否触发 self-check 修订、在哪一步失败，仍只能从瞬时日志推断。日志不能保存 prompt 或正文，外部 Provider 也不总是返回可比较 token usage。Q3 需要先建立本地、确定、无正文的运行时间线，再决定 token 与费用统计。

## 决策

### 1. schema 061 保存 generation 时间线

- `ai_run_steps` 以 `generation_id + sequence` 唯一，类型只允许 `generation / model_turn / tool_call / self_check / citation_validation / persistence`。
- 每项只保存状态、可选 turn index/tool name、开始/结束、毫秒耗时、输入/输出序列化字节、可选 Provider 原始 token 和稳定错误码。
- 严禁保存 prompt、回答、reasoning、tool arguments/results、Base URL、API key、来源正文或模型错误原文。
- 每个 generation 在 user message/generation 启动事务中创建 sequence 1 的 `generation/running` 根步骤；终态事务更新为 succeeded/failed/cancelled，并插入其余 terminal steps 和最后的 persistence step。
- 根步骤是唯一允许的 update：仅 `running → terminal`，身份、序号、类型和开始时间不可变。其他步骤创建后不可修改。

### 2. Harness 产生内容无关步骤

- 每个模型调用产生 `model_turn`；生产 ModelClient 通过与真实请求相同的 `PromptSize` 计算输入序列化字节，输出字节包含 text/reasoning/tool-call arguments 的长度，但不保存这些内容。
- schema 062 为 step/root 增加 nullable `input_tokens / output_tokens / token_source=provider`。OpenAI 只读取流帧根 `usage.prompt_tokens/completion_tokens`；Anthropic 合并 `message_start.usage.input_tokens` 与 `message_delta.usage.output_tokens`。只有完整、非负整数对才标记 provider，部分/非法/缺失保持全 NULL。
- 为兼容本地或第三方 OpenAI-compatible 端点，请求不强制增加 `stream_options.include_usage`；Provider 主动返回才记录。系统绝不按 bytes、字符或 tokenizer 猜测。
- 每个实际工具执行产生 `tool_call`，只保存代码所有工具名、参数字节、结果字节、耗时和成功/失败/取消。
- self-check 未修订也产生成功步骤；自主修订产生带新 turn index 的 self-check 模型步骤。
- Chat 在完成时增加 citation_validation，随后持久事务增加 persistence。
- 单 generation 工具调用上限固定 32，模型轮上限保持 8，因此详细步骤有确定上界；collector 最多保留 62 项，不会因模型批量 tool calls 无界写库。

### 3. 失败、取消与崩溃恢复

- 成功时 assistant message 保存 `generation_id`，与 generation 一对一；取消的持久 assistant partial 也保存该 ID。user message 与非持久会话没有链接。
- 上游失败时没有 assistant message，但 generation steps 仍保存 root/model failure/persistence，可从 generation API 解释。
- 取消时 root 和已发生步骤保存 cancelled/succeeded 终态；partial 正文仍只在既有 message/generation 字段，不进入 steps。
- schema 061 为已有 generation 回填单个 summary root：从既有 status/created/updated/content byte length 派生，不声称知道历史内部轮次。
- 启动恢复把遗留 queued/streaming generation 与其 running root 同事务改为 cancelled；generation 保留 `AI_GENERATION_INTERRUPTED`，root 不复制错误正文。

### 4. API 与 UI

- `GET /api/v1/ai/generations/:id/steps` 返回有序步骤及根汇总 `status / total / input_bytes / output_bytes / duration_ms`。
- schema 061 给 `ai_messages` 增加 nullable、唯一 `generation_id`，历史 assistant message 才显示“运行详情”。
- Web 按需加载并展示步骤、状态、耗时、字节和可用的 Provider input/output token；客户端发现 prompt/content/reasoning/arguments/result/api_key/base_url 等禁止字段或部分/伪造 token 组合时以 `INVALID_RESPONSE` 失败关闭。
- UI 对缺失 usage 明确显示“Provider 未返回 token usage”，并写“仅字节与耗时，不保存正文”。Bytes 不等同 token，token 也不自动换算费用。
- `GET /api/v1/ai/usage-summary` 可按 canonical `session_id`、`provider_id` 或两者过滤，在同一只读事务中只聚合每个 generation 的 sequence 1 根步骤。响应分开返回终态/活动生成数、Provider usage 覆盖数、unknown 数、原始 token 合计、字节、耗时及 Provider 分组；不存在的过滤对象返回 404，合法但无 generation 的范围返回全零和空分组。
- AI 会话头部按需展开“本地用量”，只在展开时读取当前会话聚合；生成收尾会失效该会话缓存。客户端校验状态总数、终态 usage 覆盖和 Provider 分组合计，发现正文、reasoning、URL、key 或不一致计数时失败关闭。

### 5. 数据生命周期

- `ai_run_steps` 属于 AI 操作态，显式排除便携业务导出，随一致性 SQLite 备份。
- 删除会话级联 generations，再级联 run steps；message generation link 使用 `ON DELETE SET NULL`，避免跨子表删除顺序问题。
- 当前没有遥测、远程上报、跨设备聚合或费用估算。
- 聚合读模型不新增物化表或定时任务；删除会话/generation 后，下次读取自然反映当前本地事实。

## 威胁与控制

| 威胁                                | 控制                                           | 验证          |
| ----------------------------------- | ---------------------------------------------- | ------------- |
| 指标表泄露正文/凭据                 | 固定列，无 JSON metadata；API/Web 禁止字段检查 | API/Web 测试  |
| 模型制造无限步骤                    | 8 轮、32 tool calls、62 collector 上限         | Harness 单测  |
| 失败/取消没有解释                   | root + terminal detail 同事务                  | Chat API 测试 |
| 崩溃留下永久 running                | 启动恢复 generation/root                       | Recovery 测试 |
| 历史回填伪造内部步骤                | 只回填一个 summary root                        | 迁移测试      |
| 把字节误称 token/费用               | API 区分 bytes/provider token，不自动换算费用  | 契约审查      |
| 部分或估算 usage 冒充 Provider 数据 | schema 全有/全无；双协议 parser 只接完整整数   | 迁移/协议测试 |
| 聚合掩盖 unknown 或活动 generation  | 终态 coverage 与 active 分列；客户端重算校验   | API/Web 测试  |
| 非法步骤被后续篡改                  | DB CHECK、唯一序号、根受限 update trigger      | 迁移测试      |

## 被拒方案

1. **把完整请求/响应写进 steps**：重复敏感正文并扩大导出、日志和删除面。
2. **用字符数/字节冒充 token**：Provider tokenizer 不同，无法准确比较或计费；缺失 usage 保持 unknown。
3. **每个 SSE delta 写一步**：产生高频 SQLite 写与大量低价值数据。
4. **只记录日志**：日志轮转且不是关联查询事实，无法稳定支持历史 UI。
5. **为旧 generation 猜测工具/自检步骤**：没有证据，只能回填 summary root。

## 后果与后续

- 优点：成功、失败、取消和启动恢复都有本地可查询、无正文的时间线；历史消息可定位对应 generation。
- 代价：每次生成增加有限行数和少量写放大；缺少 Provider usage 的 generation 不会有 token 汇总。
- 后续：ADR-024 已交付 1–30 天 UTC 终态趋势。费用仍需要用户显式配置或版本化价格快照，不得从模型名联网猜价或把估算值伪装成账单。
