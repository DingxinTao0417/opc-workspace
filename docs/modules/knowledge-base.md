# 本地知识库模块

> 目标版本：待定。TXT/Markdown/PDF 本地检索、Actor 异步任务、ADR-010 AI6 显式知识上下文与 ADR-011 回答级结构化 citation 已交付；授权引用、来源变化和句子级证据覆盖仍待后续。回答级来源身份验证不等于事实正确性证明。
>
> 决策：[ADR-009](../adr/009-local-knowledge-base-ingestion-and-search.md) · [ADR-026](../adr/026-local-knowledge-pdf-extraction.md) · [实施计划](../plans/knowledge-base-phases.md)

## 定位与边界

知识库把用户明确选择的本地资料导入、解析、索引并提供可定位来源的检索。它应在没有 AI 的情况下独立可用，并坚持原文、索引和派生数据都由用户控制。

- 默认只处理本地文件，索引保存在本机 SQLite 与受控应用目录。
- 首版先使用 SQLite FTS5 建立确定性的关键词检索，不依赖远程向量数据库或 embedding 服务。
- 当前只评估本地向量索引或本地 embedding；远程 embedding 不在范围内。
- 不自动扫描磁盘、浏览器、邮件或云盘；只有用户通过文件对话框明确选择的来源才能进入知识库。
- 不自动上传、同步或分享原文、分段、索引、查询或检索结果。
- PDF 导入是本地文本提取（ADR-026），不包含 OCR、密码破解或任意代码执行；复杂格式另行评审。

## 当前实现状态

- schema 059 已新增 `knowledge_sources`、`knowledge_documents`、`knowledge_chunks`、`knowledge_index_jobs` 与 FTS5/trigger；知识库操作态显式排除便携业务导出，完整 SQLite 备份覆盖原文与索引。schema 070 把 `source_type` 扩展到 `pdf` 并为 `knowledge_chunks` 增加 `start_page`/`end_page` 页码定位。
- Sidecar 已交付 TXT/Markdown/PDF 单文件受控上传、幂等重放、UTF-8/NUL/空白/16 MiB 校验（PDF 另有魔数预检）、换行规范化、Unicode 分段、中文辅助词项、来源过滤检索、纯文本高亮、行/字符定位、PDF 页码定位、版本化重建、单文档删除、来源删除和全库清理 API。
- PDF 提取（ADR-026）由 `ledongthuc/pdf` 纯 Go 解释器驱动：中英文经 ToUnicode CMap 提取，无文本页跳过，加密/损坏/退化内容流以稳定错误码失败（`KNOWLEDGE_PDF_ENCRYPTED/INVALID/NO_TEXT/TEXT_TOO_LARGE/TOO_COMPLEX/TOO_MANY_PAGES`），文本与操作预算防止压缩炸弹膨胀；文档 `extractor_version` 为 `pdf-text-v1`。
- Web 已新增知识库页面、主导航与命令面板入口，展示本地-only 边界、来源状态/类型/版本/分段/大小、Actor 阶段/进度、取消/retry、来源清单导出、筛选检索、位置引用（PDF 显示页码范围）、重建及删除确认。H5-E6 另把每个来源当前最新且没有后继 attempt 的 failed Job 以 metadata-only 方式接入智能体续办队列；点击以 Source+Job 双身份定位并高亮原来源卡，身份变化时拒绝替代，不自动重试或读取正文。
- 当前文本与 PDF 索引由进程内单邮箱 Actor 异步执行：HTTP 持久化 queued Job 后返回 202，Worker 推进阶段，支持运行中取消、failed/cancelled retry attempt 和启动中断恢复。最终文档/chunks/FTS/来源/Job 单事务发布，重建期间旧文档保持可搜索。
- 文件对话框授权引用、来源变化自动标 stale、向量索引尚未实现；来源清单 CSV 与 ADR-010 AI6 显式知识片段已经交付，没有远程 embedding（PDF 提取依赖是仓库新增的唯一生产依赖，纯 Go、零传递依赖）。

### H3-D1：对话引用到知识原片段（已实现，独立轨道）

- AI 已验证来源卡提供“核验原片段”，通过 `/knowledge` 的 `source_id/document_id/chunk_id/source_version/document_version` 参数定位，弹窗读取同版本完整 chunk；可通过仅接受 UUID 的 `return_session` 返回原会话。地址不接受文件路径或任意返回 URL，模型正文链接不充当已验证引用。
- 原片段只在本地显示，含文件名、文档标题、版本、字符/行范围和 PDF 页码；PDF 显示提取文本，不渲染原版面。服务失败可重试，旧版本冲突或片段消失明确提示，不以最新资料冒充历史证据。
- 预览与 AI6 共用精确读模型，不读取整个原始文件、不发起模型请求、不改业务状态；响应不缓存，关闭取消在途读取，重新打开重新核验。历史会话中的实际发送快照仍保留，删除知识库不静默删除会话。
- 此处补齐的是已验证引用的人工往返；H3-D2 另提供按条消息授权的模型检索/读取，不将人工预览当作工具授权。

### H3-D2：智能体检索与读取（已接入，独立轨道）

用户在“工作台权限 → 知识检索与引用”选择 1–20 个来源及版本，只授权下一条消息。`knowledge_search` 与人工搜索共用 FTS 读模型，摘要只供发现；`knowledge_read` 精确读取完整 chunk 后才加入运行级证据列表。共最多 8 次成功结果、24 KiB JSON，最终引用最多 3 项；来源变化需要重新授权。无知识导入/删除、任意磁盘、联网或自动扩大来源范围能力。

来源与 Provider 在发送及执行时重验，搜索分页/空结果/额度失败明确返回；超时迟到、截断、失败结果不能形成已验证来源。动态工具正文不单独持久化，引用只保存位置/版本，删除后可能无法核验原文；手选 AI6 仍保存实际正文快照。模型可能把读到内容写入回答/会话记忆。非持久运行不落正文或引用；H4 已接入终态引用回传与主/侧对话来源卡，可核验原片段并返回原会话。短时恢复仅有元数据，不能把收到的部分正文标为完整；不可恢复引用时明确提示未知。安全/API 契约见 [ADR-029](../adr/029-agent-workspace-capabilities.md) 与 [ADR-011](../adr/011-ai-validated-knowledge-citations.md)。

### H3-F：智能体来源与索引管理（已接入，独立轨道）

`knowledge_actions` 是与内容读取分离的独立、单消息、仅保存会话可用的授权。它只开放**元数据**：`knowledge_library` 按 1–20 条分页列出或按真实 UUID 读取来源名称、类型、状态、版本、大小、已提取文档数、分段数与最新索引任务（operation/status/attempt/error_code），也可按任务 ID 读取其来源身份；响应不含受管原文、提取正文、分段内容、哈希、文件字节或路径。读取来源正文仍只能在另选“知识检索与引用”并逐版本选择来源后通过 `knowledge_search`/`knowledge_read` 完成；两个授权互不继承。

`workspace_propose` 的模型侧 action 枚举也按该边界过滤：只有 `knowledge_actions` 才能看到 `knowledge_source.*` 与 `knowledge_index_job.*`，普通 `work+actions` 不会泄漏这些动作；反向仅授予 `knowledge_actions` 时不会暴露任务、财务、发票或 Agent 执行动作。确认端仍由服务端重新解析并执行同一权限门禁。

该工具的对外 schema 与处理器接受的字段完全一致（`view=list|source|job` 必填，`id/query/status/limit/offset` 可选，`additionalProperties:false`）。这是回归修复：此前它缺少专用 schema 分支，落到通用搜索 schema 后宣传 `type/status/project_id`，而处理器只认 `view`，真实模型按宣传字段调用必然失败；`TestAIMetadataToolSchemasMatchTheirHandlers` 现在同时断言字段集合一致、schema 形状的参数可真实调用、通用搜索参数被拒绝。

同一授权可提出五项人工确认命令。`knowledge_source.create` 把用户本轮明确提供的文本存为新受管来源：`changes.name` 必须是 `.txt`/`.md`/`.markdown` 文件名（决定 source_type/MIME），`changes.content` 必须是完整 UTF-8 文本且 ≤24,000 字节，`changes.title` 可选；校验、去空、类型判定与文本合法性完全复用原生导入（同名、空内容、NUL/非 UTF-8、未知扩展均拒绝），且不接受任何目标 ID、版本、文件字节或路径。其余四项针对已有来源：`knowledge_source.reindex`（按受管副本重建索引）、`knowledge_source.delete`（删除来源、文档/分段与受管副本）、`knowledge_index_job.retry`（仅失败/取消任务，新建带 `retry_of_job_id` 的 attempt）与 `knowledge_index_job.cancel`（仅排队中/运行中任务）。来源动作使用 `knowledge_source_id` 与当前来源版本，任务动作使用 `knowledge_index_job_id` 与任务当前来源版本；这四项都要求空 `changes`，只有删除可带可选 `reason`（≤500 字）。

创建预览不复制整篇正文，只冻结文件名、source_type、目标状态 indexing/版本 1、内容字节数、SHA-256 与开头 ≤800 字（超出时标记 `proposed_excerpt_truncated`），因此人工可以用大小、哈希与前缀核对模型是否如实转存了用户提供的文本。已有来源的动作冻结稳定事实（名称、类型、状态、版本、文档数、分段数、任务 operation/status/attempt/error_code），不含运行阶段的易变 progress，因此运行中的任务仍可被取消而不产生假冲突。确认事务重算完整预览并逐字比较，来源版本、分段/文档数、任务状态或脚本身份变化都会以 `AI_ACTION_PREVIEW_CHANGED` 拒绝旧卡。确认后复用原生共享命令：创建走 `createKnowledgeSourceInTransaction`，删除走 `deleteKnowledgeSourceInTransaction`，重建/重试走 `queueKnowledgeReindexInTransaction`（新任务在审批事务提交**之后**才入队，提交失败不产生孤立任务），取消走 `cancelKnowledgeIndexJobInTransaction`；取消按剩余文档把来源恢复到 ready/failed，不删除已有资料。创建与重建/重试的结果身份都是新来源或新索引任务；删除需人工另行勾选 `confirm_knowledge_source_delete`，永久移除来源、索引与受管副本且应用内无撤销，审批与索引历史保留。回执路由统一为 `/knowledge`：原生知识库页只对“最新失败任务”做精确高亮，其余状态打开列表而不是声称定位成功。

固定证据：`ai_knowledge_source_actions_test.go` 覆盖创建（含索引成功与内容落库）、重建/删除/重试/取消、缺失同意零副作用、预览漂移拒绝、幂等重放、任务终态规则、超限/未知扩展/空/null 内容拒绝、范围隔离（`knowledge` 不注册管理工具、`knowledge_actions` 不注册内容检索）、非持久会话拒绝与只读元数据不泄露正文/哈希；前端 `aiWorkspaceActions.test.ts`、`AiWorkspaceActions.test.tsx` 与 `AiWorkspaceAccess.test.tsx` 覆盖创建/管理预览的严格解析（含摘录必须来自同一正文、字节数与哈希格式）、独立同意、路由与授权面板。API v1 / schema 076 不变；真实 Provider 质量与原生桌面体验仍单列验收。

## 目标功能

- 导入 Markdown、TXT 和可提取文本的 PDF，展示来源名称、类型、大小、更新时间和索引状态。
- 当前支持 SQLite 受控副本；后续再接授权引用，并明确模式、权限和文件缺失行为。
- 本地提取、规范化、分段和 FTS5 索引，任务可取消、重试并显示进度。
- 关键词搜索、结果高亮、来源过滤、分段预览和跳转原文位置。
- 检测来源变化并允许增量更新或完整重建。
- 单文档删除、来源批量删除和知识库彻底清理；原文副本、派生文本、分段、索引和缓存级联删除。
- 导出来源清单和索引状态，不默认导出原始敏感内容。

## 关键用户流程

1. **选择来源**：当前 Web 文件输入只把用户明确选择的文件 bytes 交给 Sidecar；未来 Tauri 文件对话框再提供授权引用模式。
2. **导入索引**：Sidecar 先事务保存受控副本和 queued Job，本地 Actor 异步提取/分段；最终在同一 SQLite 事务发布文档、chunks、FTS 和 succeeded 状态。
3. **查看进度**：Job API 返回 queued/running/terminal、阶段和进度；用户可取消 running/queued，failed/cancelled 可产生带 `retry_of_job_id` 的新 attempt，启动中断不会留下半可见索引。
4. **搜索定位**：用户输入关键词，按来源筛选，结果卡显示 excerpt 与来源/分段/行号及 PDF 页码；AI 的已验证来源卡另可进入知识库核验同版本完整原片段并返回对话。
5. **更新来源**：检测到文件变化后标记 stale，由用户确认增量或完整重建；旧索引在新版本成功前仍可解释。
6. **删除清理**：用户确认删除范围，系统事务性删除元数据并清理受控文件与索引，随后执行可验证扫描。
7. **提供 AI 上下文（已实现）**：可保留 AI6 手选最多 3 个结果并检查完整发送预览；也可按 H3-D2 授予下一条消息读取选中版本化来源的权限，由模型按需检索/读取。不默认把整库或全部来源正文发送给模型。

## 数据、API、状态与事件

### 数据

- `knowledge_sources`：当前保存显示名、`managed_copy`、类型（`text`/`markdown`/`pdf`）、大小、内容哈希、原始 bytes、状态、版本和审计时间；授权引用、路径作用域与 mtime 为后续字段。
- `knowledge_documents`：来源解析版本、标题、语言、提取器版本（`plain-text-v1` / `pdf-text-v1`）、文本校验和及状态。
- `knowledge_chunks`：文档、顺序、位置引用（字符/行范围与 `start_page`/`end_page` 页码范围，文本来源恒为 1）、正文、正文哈希和索引版本。
- `knowledge_index_jobs`：来源/文档、任务类型、状态、阶段、进度、attempt、错误码、取消标记和时间。
- FTS5 虚表及映射由迁移创建；不能让 FTS 行成为原文事实源。
- 任意文件路径都通过 Tauri 授权和受控引用保存；日志、API 错误和事件不包含完整正文。

### API

- `GET / POST /api/v1/knowledge/sources`（已实现）
- `GET /api/v1/knowledge/sources/export.csv`（已实现；只含 metadata/status，需 confirm）
- `GET / DELETE /api/v1/knowledge/sources/:id`
- `POST /api/v1/knowledge/sources/:id/reindex`
- `GET /api/v1/knowledge/documents`
- `GET / DELETE /api/v1/knowledge/documents/:id`
- `GET /api/v1/knowledge/chunks/:id`：必填 `source_id/document_id/source_version/document_version`，单次精确身份与版本读取；422 `INVALID_KNOWLEDGE_LOCATION`、404 `KNOWLEDGE_CHUNK_NOT_FOUND`、409 `KNOWLEDGE_LOCATION_CHANGED`；不返回整篇文档或文件路径。
- `POST /api/v1/knowledge/search`
- `GET /api/v1/knowledge/index-jobs/:id`
- `POST /api/v1/knowledge/index-jobs/:id/cancel`
- `POST /api/v1/knowledge/index-jobs/:id/retry`
- `GET /api/v1/ai/inbox?kind=knowledge_failure`（H5-E6 已实现；只读当前失败 Job 的安全元数据，原始 bytes、正文、分段、哈希和路径不进入响应）
- `DELETE /api/v1/knowledge`

以上路由均已注册；cancel 只对非终态 Job 生效，retry 只接受 failed/cancelled Job 并要求来源 `If-Match`。Web 对 indexing 来源进行短间隔轮询，来源完成后自动停止。

导入必须使用文件对话框返回的本地授权引用或受控上传管道，不接受前端任意绝对路径。

### 状态与事件

- 来源状态：`pending / indexing / ready / stale / missing / failed / deleted`。
- Index Job 状态：`queued / running / succeeded / failed / cancelled`。
- 目标事件：`knowledge.source_added`、`knowledge.index_started`、`knowledge.index_succeeded`、`knowledge.index_failed`、`knowledge.source_deleted`；当前 Actor 文本切片保存完整 Job 状态与 attempt 证据，但尚未写入通用事件流。
- 重新索引以来源版本和内容哈希去重；失败重试产生新 attempt，不覆盖原执行证据。

## 与其他模块协作

- **AI 助手**：ADR-010 复用受控 search 与 `/ai/context/preview` 提供用户选定片段；Chat 在写入前重验 source/document version，历史保存实际发送快照。AI 不得直接扫描表或绕过来源权限。
- **任务/项目/客户**：未来可保存本地引用，但默认不自动索引这些业务对象，也不越权拼接客户敏感信息。
- **Task Artifact**：用户确认后可把本地产出作为来源；删除原 Artifact 时必须处理引用和索引一致性。
- **备份恢复**：数据库、受控原文、派生数据和索引 manifest 必须能一致备份、恢复和验证。
- **桌面权限**：文件对话框、路径作用域、日志脱敏和磁盘空间检查是上线前置条件。

## 分阶段实施

1. **K1 ADR 与威胁模型（已完成）**：ADR-009 明确受控副本、无路径 API、格式/资源上限、删除、备份和本地-only 网络边界。
2. **K2 数据与任务框架（已完成）**：schema 059、单邮箱 Actor、阶段进度、取消/retry、旧索引保留和启动中断恢复已交付。
3. **K3 文本导入（已完成）**：TXT/Markdown 格式、UTF-8、NUL、空白和损坏输入已覆盖；PDF（K3b，ADR-026/schema 070）已交付页码定位、损坏/加密/无文本页与预算防护。
4. **K4 本地检索（已完成）**：FTS5、中文辅助词项、来源过滤、高亮、位置引用和无结果已交付。
5. **K5 生命周期（核心闭环已完成）**：版本化重建、文档/来源删除、彻底清理与 metadata-only 来源清单已交付；变化检测、stale 待 K5b。H3-F/H3-G 已把「把用户文本存为新来源」、来源重建、失败/取消任务重试、运行中取消与来源永久删除接入智能体的人工确认轨道，模型只见元数据，库内正文不外发。
6. **K6 AI 集成（已完成，[ADR-010](../adr/010-ai-explicit-knowledge-context.md)）**：只开放用户显式选择的 1–3 个受控片段并保存来源历史；没有自动整库检索。本地向量/embedding 另做包体、性能和跨平台 ADR。

## 验收标准

- 每条结果都能定位来源、文档版本和具体分段；高亮与原文一致。
- 未授权文件、目录遍历、符号链接越界和任意绝对路径被拒绝。
- 导入、索引、取消、失败重试和崩溃恢复不会产生重复或半可见索引。
- 来源更新失败时旧可用版本仍可解释；成功切换为原子操作。
- 删除后原文副本、派生文本、chunks、FTS 行和缓存均可验证清除，备份策略明确。
- 日志、错误、事件和诊断不包含完整正文或敏感片段。
- 断网时导入、索引、搜索、更新和删除完整可用；网络监测确认没有远程 embedding 或数据外发。
- 关键词检索的准确来源、无结果、提示注入文本、损坏/加密/无文本 PDF、大文件、低磁盘和跨平台路径均有测试；纯 Go 提取对复杂排版（表格、多栏、非 ToUnicode 编码）以稳定错误码失败或不产出文本，不伪造内容。
- 知识库故障不影响任务、项目、客户等核心模块。

## 相关 PRD 与代码链接

- [产品 PRD](../opc-workspace-PRD.md)（§5.11、§10.4.16、§10.7、附录 C）
- [Sidecar 路由](../../services/sidecar/internal/api/router.go)
- [数据库迁移入口](../../services/sidecar/internal/database/migrate.go)
- [数据库目录](../../services/sidecar/internal/database/)
- [前端路由](../../apps/web/src/App.tsx)
