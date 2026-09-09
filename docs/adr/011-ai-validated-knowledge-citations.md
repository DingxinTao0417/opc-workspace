# ADR-011：AI 回答的可验证知识引用

- 状态：Accepted，AI7-Q1 已实现
- 日期：2026-09-08
- 决策范围：AI7 质量闸门 / Knowledge citation
- 相关文档：[AI 助手](../modules/ai-assistant.md)、[AI6 计划](../plans/ai-knowledge-context-phases.md)、[AI7 计划](../plans/ai-quality-gates.md)、[ADR-010](010-ai-explicit-knowledge-context.md)

## 背景

ADR-010 已能把用户确认的知识 chunk 安全地交给模型，但“模型看到了哪些资料”和“模型声明回答使用了哪些资料”仍是两件事。仅要求模型在自然语言中写文件名或行号无法验证：它可能引用未选来源、伪造位置、漏掉引用，或者把资料正文复制成所谓 quote。AI7 的第一项质量闸门需要一个可校验、可解释、不会把模型声明当事实的 citation 契约。

## 决策

### 1. 模型只声明 chunk identity

- 仅当本次 prompt 含显式知识上下文时，代码所有系统提示要求模型在任务/记忆块之后、self-check 之前输出一个 citation 控制块：

```text
[opc:citations]{"chunk_ids":["实际用于回答的 chunk_id"]}[/opc:citations]
```

- `chunk_ids` 最多 3 个、不得重复，并且必须属于本次 ADR-010 已确认 allowlist。
- 若已选资料不足以回答，模型应在自然语言说明“没有可靠证据”，并输出空数组。
- 没有知识上下文时禁止输出 citation block；Sidecar 会剥离模型自行伪造的块且不保存引用。

### 2. Sidecar 验证声明，不相信模型元数据

- Sidecar 只解析一个严格 JSON object，拒绝未知字段、尾随值、超 4 KiB、多个块、未闭合块、非法/重复/越权 UUID。
- 模型不能提交来源名称、文档版本、行号、quote 或可信标志。合法 chunk ID 对应的全部 citation metadata 都从本次已验证 `aiKnowledgeContextSource` 重建。
- 保存项包括 source/document/chunk identity、类型、source/document version、chunk index、行/字符范围；不重复保存正文。
- Citation 证明“模型声明使用了这个已授权 chunk”，不证明回答本身一定正确，也不建立句子级归因。

### 3. 五种可解释状态

- `not_requested`：本次没有知识上下文；数据库保持 NULL，API 派生空列表。
- `validated`：一个且仅一个合法块，至少一个 allowlisted chunk；保存服务器重建引用。
- `no_evidence`：合法空数组；UI 明确显示模型判断资料不足。
- `missing`：有知识上下文但模型没给 citation block；回答保留，UI 警告缺少可验证引用。
- `invalid`：块格式错误、重复、越权或多个；回答保留，引用清空，UI 明确说明已被 Sidecar 拒绝。

Missing/invalid 不自动把回答改成有来源，也不把整个生成判失败；用户仍能看到回答和质量警告。编码/持久化本身失败才以 `AI_CITATION_PERSIST_FAILED` 终止。

### 4. schema 060 保存服务器验证结果

- `ai_messages.citations_snapshot` 只允许 `role=assistant AND status=completed`，最大 16 KiB，要求 version=1、合法状态、JSON array 且最多 3 项。
- Raw `[opc:citations]` block 在持久化 assistant message 和 generation content 前移除；历史 prompt 也剥离 citation/task/memory/selfcheck 控制块。
- 取消中的 partial output 会防御性剥离完整或未闭合 citation tail，但不保存 citation snapshot。
- Portable business export 已排除整个 AI message 表；一致性 SQLite 备份继续覆盖引用证据。

### 5. 前端只展示服务器结果

- 流式显示防御性隐藏完整/半截 citation block，避免协议文本闪现。
- `validated` 显示“已验证来源”卡片及文件、行号、文档版本、chunk 序号。
- `no_evidence / missing / invalid` 分别显示资料不足、缺失引用、越权/非法引用警告。
- 前端严格复核状态与 items 一致：只有 validated 可携带 1–3 项，其他状态必须为空；不一致响应按 `INVALID_RESPONSE` 失败关闭。

## 威胁与控制

| 威胁                         | 控制                                                | 验证                   |
| ---------------------------- | --------------------------------------------------- | ---------------------- |
| 模型引用未选来源             | chunk ID 必须命中本次 allowlist                     | parser/API 测试        |
| 模型伪造文件名、行号或 quote | 控制块只允许 `chunk_ids`，metadata 由 Sidecar 重建  | strict JSON 与快照测试 |
| 重复/过量 citation           | UUID 去重与最多 3 条                                | parser/schema 测试     |
| 缺失引用被误装成有来源       | `missing` 空列表 + 明确 UI 警告                     | Go/Web 测试            |
| 无答案仍自由发挥             | 空数组映射 `no_evidence`，UI 暴露状态               | parser/Web 测试        |
| 控制块泄露到历史或 UI        | 服务端持久化前剥离，Web 流式防御性剥离              | Go/TS 测试             |
| 恶意历史 snapshot            | schema shape + API decode identity/status/invariant | migration/API 测试     |

## 被拒方案

1. **解析 Markdown 脚注或文件名**：自然语言无法可靠映射到 allowlisted identity。
2. **让模型返回完整 citation metadata**：文件名、版本、位置和 quote 都可被伪造。
3. **缺失引用时自动附上全部已选来源**：会把“可用上下文”误写成“回答实际使用的证据”。
4. **任何 citation 错误都丢弃整条回答**：格式兼容问题不应伪装成模型/网络失败；显式质量警告更可解释。
5. **保存正文副本到 citation snapshot**：user context snapshot 已保存实际外发内容，citation 只需不可变位置元数据。

## 后果与限制

- 优点：越权引用无法落库，用户能区分已验证、资料不足、缺失和非法引用。
- 代价：schema 060 新增一列；模型需遵守一个控制协议，不兼容模型会显示 missing/invalid 警告。
- 限制：当前是回答级 chunk citation，不验证每个自然语言断言，也不计算引用覆盖率或事实正确率。
- 后续：AI7-Q2 建立离线标准问答/无答案/冲突/提示注入评测；PDF 接入后扩展页码；句子级引用或自动修订必须另行评审。
