# 本地知识库模块

> 目标版本：待定。TXT/Markdown 本地检索、Actor 异步任务、ADR-010 AI6 显式知识上下文与 ADR-011 回答级结构化 citation 已交付；PDF、授权引用、来源变化和句子级证据覆盖仍待后续。回答级来源身份验证不等于事实正确性证明。
>
> 决策：[ADR-009](../adr/009-local-knowledge-base-ingestion-and-search.md) · [实施计划](../plans/knowledge-base-phases.md)

## 定位与边界

知识库把用户明确选择的本地资料导入、解析、索引并提供可定位来源的检索。它应在没有 AI 的情况下独立可用，并坚持原文、索引和派生数据都由用户控制。

- 默认只处理本地文件，索引保存在本机 SQLite 与受控应用目录。
- 首版先使用 SQLite FTS5 建立确定性的关键词检索，不依赖远程向量数据库或 embedding 服务。
- 当前只评估本地向量索引或本地 embedding；远程 embedding 不在范围内。
- 不自动扫描磁盘、浏览器、邮件或云盘；只有用户通过文件对话框明确选择的来源才能进入知识库。
- 不自动上传、同步或分享原文、分段、索引、查询或检索结果。
- PDF 导入是本地文本提取，不包含 OCR、密码破解或任意代码执行；复杂格式另行评审。

## 当前实现状态

- schema 059 已新增 `knowledge_sources`、`knowledge_documents`、`knowledge_chunks`、`knowledge_index_jobs` 与 FTS5/trigger；知识库操作态显式排除便携业务导出，完整 SQLite 备份覆盖原文与索引。
- Sidecar 已交付 TXT/Markdown 单文件受控上传、幂等重放、UTF-8/NUL/空白/16 MiB 校验、换行规范化、Unicode 分段、中文辅助词项、来源过滤检索、纯文本高亮、行/字符定位、版本化重建、单文档删除、来源删除和全库清理 API。
- Web 已新增知识库页面、主导航与命令面板入口，展示本地-only 边界、来源状态/版本/分段/大小、Actor 阶段/进度、取消/retry、来源清单导出、筛选检索、位置引用、重建及删除确认。
- 当前文本索引由进程内单邮箱 Actor 异步执行：HTTP 持久化 queued Job 后返回 202，Worker 推进阶段，支持运行中取消、failed/cancelled retry attempt 和启动中断恢复。最终文档/chunks/FTS/来源/Job 单事务发布，重建期间旧文档保持可搜索。
- PDF、文件对话框授权引用、来源变化自动标 stale、向量索引尚未实现；来源清单 CSV 与 ADR-010 AI6 显式知识片段已经交付，没有远程 embedding 或新增生产依赖。

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
4. **搜索定位**：用户输入关键词，按来源筛选，打开结果可看到原文件、分段和位置引用。
5. **更新来源**：检测到文件变化后标记 stale，由用户确认增量或完整重建；旧索引在新版本成功前仍可解释。
6. **删除清理**：用户确认删除范围，系统事务性删除元数据并清理受控文件与索引，随后执行可验证扫描。
7. **提供 AI 上下文（已实现）**：用户在 AI 面板本地搜索并显式选择最多 3 个结果，检查完整发送预览后模型才能读取；知识库不会自动把整个来源交给模型，也不提供 knowledge tool。

## 数据、API、状态与事件

### 数据

- `knowledge_sources`：当前保存显示名、`managed_copy`、类型、大小、内容哈希、原始 bytes、状态、版本和审计时间；授权引用、路径作用域与 mtime 为后续字段。
- `knowledge_documents`：来源解析版本、标题、语言、提取器版本、文本校验和及状态。
- `knowledge_chunks`：文档、顺序、位置引用、正文、正文哈希和索引版本。
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
- `POST /api/v1/knowledge/search`
- `GET /api/v1/knowledge/index-jobs/:id`
- `POST /api/v1/knowledge/index-jobs/:id/cancel`
- `POST /api/v1/knowledge/index-jobs/:id/retry`
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
3. **K3 文本导入（TXT/Markdown 已完成）**：格式、UTF-8、NUL、空白和损坏输入已覆盖；PDF 待 K3b。
4. **K4 本地检索（已完成）**：FTS5、中文辅助词项、来源过滤、高亮、位置引用和无结果已交付。
5. **K5 生命周期（核心闭环已完成）**：版本化重建、文档/来源删除、彻底清理与 metadata-only 来源清单已交付；变化检测、stale 待 K5b。
6. **K6 AI 集成（已完成，[ADR-010](../adr/010-ai-explicit-knowledge-context.md)）**：只开放用户显式选择的 1–3 个受控片段并保存来源历史；没有自动整库检索。本地向量/embedding 另做包体、性能和跨平台 ADR。

## 验收标准

- 每条结果都能定位来源、文档版本和具体分段；高亮与原文一致。
- 未授权文件、目录遍历、符号链接越界和任意绝对路径被拒绝。
- 导入、索引、取消、失败重试和崩溃恢复不会产生重复或半可见索引。
- 来源更新失败时旧可用版本仍可解释；成功切换为原子操作。
- 删除后原文副本、派生文本、chunks、FTS 行和缓存均可验证清除，备份策略明确。
- 日志、错误、事件和诊断不包含完整正文或敏感片段。
- 断网时导入、索引、搜索、更新和删除完整可用；网络监测确认没有远程 embedding 或数据外发。
- 关键词检索的准确来源、无结果、提示注入文本、损坏 PDF、大文件、低磁盘和跨平台路径均有测试。
- 知识库故障不影响任务、项目、客户等核心模块。

## 相关 PRD 与代码链接

- [产品 PRD](../opc-workspace-PRD.md)（§5.11、§10.4.16、§10.7、附录 C）
- [Sidecar 路由](../../services/sidecar/internal/api/router.go)
- [数据库迁移入口](../../services/sidecar/internal/database/migrate.go)
- [数据库目录](../../services/sidecar/internal/database/)
- [前端路由](../../apps/web/src/App.tsx)
