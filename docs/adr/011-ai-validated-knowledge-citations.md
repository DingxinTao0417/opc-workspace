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

## 2026-09-18 接续：H3-D1 本地引用核验

- `validated` 引用新增“核验原片段”：链接仅由服务器引用的 source/document/chunk UUID 与两个版本生成，进入知识库的本地预览，可返回原会话；模型写出的任意链接不获得同等待遇。
- 新增 `GET /api/v1/knowledge/chunks/:id?source_id=UUID&document_id=UUID&source_version=N&document_version=N`。要求全部身份和版本，拒绝重复/未知参数；与 AI6 共用同一精确 join，只读取一个 chunk（正常分段 1,200 Unicode 字符，存储上限 4,096），不读取整篇文档或原始文件。响应 `Cache-Control: no-store`，无 AI 调用或业务写入。
- 当前仍存在的片段若版本不符返回 `409 KNOWLEDGE_LOCATION_CHANGED`；已删除、重建替换或身份不匹配返回 `404 KNOWLEDGE_CHUNK_NOT_FOUND`。前端不使用新版代替旧证据，不回退任意路径，重新打开或重试会重新核验；查询失败时不继续显示缓存正文。
- PDF 引用历史解码现允许 `pdf` 并严格校验正整数、有序页码；文本旧快照缺少页码或为 0/0 时只在读取结果中兼容为 1/1，不改写保存记录。PDF 无有效页码不推测为第一页；前端同步校验。PDF 预览是带页码的提取文本，不是 PDF 原版面。
- 不变：历史 citation/context 生命周期不随知识库删除而改写；D1 本身只增加人工作业往返。动态工具由以下 H3-D2 独立授权接续。

## 2026-09-18 接续：H3-D2 运行级知识证据

- 显式 knowledge scope 及 1–20 个版本化来源可注册 `knowledge_search/read`，详见 ADR-029。仅搜索摘要不获得引用资格；成功读取的完整 chunk 在 Harness 接受结果后加入本次 allowlist，与手选片段合并，最终仍最多引用 3 个 chunk。
- 私有 `AcceptResult` 门禁在 Executor 成功返回之后、写入工具历史之前执行；超时后迟到的工具 goroutine、失败、截断 JSON、超出知识累计预算的结果不能添加证据，也不会被作为成功结果交给模型。普通内容无关运行日志不接收正文。
- 授权检索但未读到片段时也校验引用：空数组为 no_evidence，缺块为 missing，引用搜索摘要或历史 chunk 为 invalid。没有手选片段且没有本次知识授权时仍为 not_requested。
- 动态引用的位置、版本及 PDF 页码来自实际工具读取的服务端元数据，绝不从模型名称、行号或链接重建。源内容是非可信证据，引用身份成立仍不证明语义正确。
- 工具正文不另存 context_snapshot；持久会话保存引用位置，模型可能将正文写入回答或会话记忆。删除来源后动态引用可能无法再核验，不宣称保有历史正文副本；手选快照保持原有生命周期。非持久会话不落消息或引用快照；临时引用的终态回传、回读及前端展示由以下 H4 接续。

## 2026-09-18 接续：H4 临时引用与恢复

- `opc-ai-sse-v1` 的 `done` 附加 `citation_status` 与 `citations`，只由最终校验结果构造，不从流式正文猜测。持久及非持久会话都回传五种结果；失败、取消及进行中的生成没有最终引用。旧客户端忽略新增字段，旧服务缺少字段在新客户端中表示“未知”，不映射成 `not_requested`。
- generation GET 完成态可回读同一结果：持久会话从已保存的 assistant message 严格解码；非持久会话使用独立纯内存缓存，只留来源身份/名称/版本/位置，不含回答、思考或 chunk 正文。缓存最多 32 项，每项编码不超过 16 KiB，15 分钟有效；读写时淘汰过期项，满额淘汰最旧项。删除会话、数据恢复清空相关缓存，进程退出后不恢复；HTTP 使用 `no-store`。
- 临时结果在受维护读锁保护的完成事务提交前发布，事务失败撤回，避免 completed 回读先于引用可见；回读必须先核验真实 generation/session，不能凭缓存复活已删除对象。缓存读取与存入复制列表，调用者不能改写共享证据。
- 前端对 SSE/GET 复用引用解析并校验完整字段、规范 UUID、非重复 chunk、1–3 项、版本/位置和字节上限。错配 generation 终态、非法引用字段不能被当作成功；传输停止并进入既有恢复链。引用只在应用内存中保留，计入 20 回合/8 MiB，不写 storage、日志或持久响应缓存。
- 主对话与侧边聊天共用来源卡，可按版本核验原片段并返回会话。拿到完整 SSE 终态的回复仍为完成；断流后仅回读到元数据不能补全正文，已收到的片段仍为 `incomplete`，卡片注明来源对应服务端完整回答，不证明片段语义。缓存过期/重启/旧服务导致引用字段缺失时明确提示不可恢复，不虚构无证据或验证通过；不会覆盖窗口内已收到的有效引用。临时回合仍只读，不新增任务或记忆确认权限。
- 不新增数据库迁移；这是运行内恢复，不是完整正文断点续传、句子级引用或真实模型质量验收。

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
- 接续：AI7-Q2 离线评测见对应计划；PDF 页码已由 ADR-026 与上述历史兼容补齐；句子级引用或自动修订仍须另行评审。
