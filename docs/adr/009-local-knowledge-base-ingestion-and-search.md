# ADR-009：本地知识库导入、索引与检索边界

- 状态：Accepted，首个纵向切片已实现
- 日期：2026-09-08
- 决策范围：知识库 K1–K5 基线
- 相关文档：[知识库模块](../modules/knowledge-base.md)、[实施计划](../plans/knowledge-base-phases.md)、[ADR-008](008-ai-explicit-business-context.md)

## 背景

知识库需要在没有 AI、没有线上账号和没有远程 embedding 的情况下独立完成资料导入、检索、定位、重建和删除。首个实现还必须回答四个安全问题：前端是否能提交任意路径、原文放在哪里、FTS 派生数据怎样和原文保持一致、便携业务导出是否会意外带出敏感资料。

## 决策

### 1. 首版只接受受控上传副本

- 导入入口为 `multipart/form-data` 的单个 `file` part，可附可选 `title`；API 不接受绝对路径、相对路径、目录或符号链接引用。
- 服务端只把清理后的 basename 作为显示名称，原始客户端路径不会进入 SQLite、日志或错误。
- 首版支持 `.txt`、`.md`、`.markdown`，单文件最大 16 MiB；内容必须是非空 UTF-8 且不得包含 NUL。
- PDF、本地授权引用和来源变化探测不与文本基线混在一次决策中；它们需要新的提取器、桌面权限和资源上限评审。

### 2. 文本原文保存在 SQLite 受控副本中

- `knowledge_sources.original_content` 保存用户明确选择的原始 bytes，`knowledge_documents.content_text` 保存换行规范化后的派生文本。
- 这样来源、解析文本、chunks、FTS 行和 Index Job 处于同一事务及同一 SQLite 备份/恢复边界，不新增会绕过现有备份 manifest 的文件目录。
- 知识库表、FTS 虚表及 shadow tables 全部显式排除便携业务 JSON/ZIP；完整工作区备份继续覆盖数据库。
- 未来若 PDF 或大文件改用受控目录，必须同时扩展 marker、容量估算、备份 manifest、恢复校验、墓碑和启动 reconcile，不能只保存一个文件路径。

### 3. 本地单邮箱 Actor 驱动可取消索引

- 文本只做 CRLF/CR 到 LF 的确定性规范化，不解释 Markdown、不执行脚本、不下载外链。
- 分段上限为 1,200 Unicode 字符，优先在后半段换行处切分，保留 160 字符重叠；每段记录文档版本、字符范围、行范围和 SHA-256。
- HTTP 导入先在事务中保存 managed copy、`indexing` 来源与 `queued` Job，返回 `202 Accepted`；进程内单邮箱 `knowledgeIndexer` Actor 串行认领任务并推进 `extracting → chunking → indexing → complete`。
- Web 为每次导入生成 `Idempotency-Key`。Sidecar 以显示名、title、类型、大小和内容 SHA-256 计算请求哈希，重放返回首个 queued 响应且不重复创建 source/Job；同 key 不同内容返回 `IDEMPOTENCY_CONFLICT`。
- mailbox 固定 64 个待处理身份且入队不阻塞；队列不可用时 Job 转为带稳定错误码的 `failed`，不会挂住请求或丢失来源证据。
- cancel 在事务中把 queued/running Job 改为 `cancelled` 并恢复来源：已有 document 的重建回到 `ready`，首次导入回到 `failed`。Worker 在每阶段及最终提交前重验 Job 状态和 cancel flag，不发布已取消结果。
- 导入和重建的最终发布仍在单个 SQLite 事务中切换 document、chunks、FTS trigger、来源版本和 Job 终态。重建运行期间旧 document 仍可搜索；新版本失败或取消不会覆盖旧索引。
- 启动时把遗留 queued/running Job 标记为 `KNOWLEDGE_INDEX_INTERRUPTED`：已有 document 的来源恢复 `ready`，否则转为 `failed`，用户可按新来源版本 retry。重试产生新 Job/attempt 并保留 `retry_of_job_id`。

### 4. FTS5 只是真实 chunks 的派生索引

- `knowledge_chunks` 是定位事实源，`knowledge_chunks_fts` 由 insert/update/delete trigger 同步，不把 FTS 行当原文。
- FTS5 使用 `unicode61`。为弥补其对连续中文没有词典分词的问题，应用额外生成只用于检索的中文单字/双字 `search_text`；API 从不返回这列，也不把它当引用正文。
- 查询被拆成最多 16 个字母/数字词项并逐项引用，不能把用户输入直接当 FTS 语法。来源筛选最多 20 个 canonical UUID，结果最多 50 条。
- 每条结果返回来源、文档版本、chunk、行号、字符范围、纯文本 excerpt 和 Unicode 字符高亮范围；前端不使用 `dangerouslySetInnerHTML`。

### 5. 删除必须清除事实和派生数据

- 删除来源需要 `confirm=true` 与匹配的 `If-Match`。事务删除当前 document/chunks，trigger 清除 FTS，并把来源改为 tombstone、清空 `original_content`、保存删除原因和新版本。
- 单文档删除保留受控原文，把来源改为 `pending`；用户可用新来源版本重新索引。
- 清空整个知识库额外要求精确 `X-Knowledge-Confirmation: DELETE KNOWLEDGE BASE`，硬删除来源并级联文档、chunks、Job 和 FTS。
- 删除不触碰用户最初选择的磁盘文件，因为首版从未持有该路径。

### 6. 网络与 AI 边界

- 导入、提取、分段、索引、搜索、重建和删除不创建网络客户端，不调用 AI Provider，也不产生 Inbox 项。
- 搜索结果文本包含不可信内容；即使它看起来像指令，也只是资料正文。
- AI 集成已由 [ADR-010](010-ai-explicit-knowledge-context.md) 单独完成：显式片段选择、发送前远程/本地披露、Provider/source/document 版本重验、大小预算和历史来源快照。AI 仍不得直接扫描知识库表或自动检索整库。

## 威胁与控制

| 威胁                             | 控制                                                       | 当前验证                 |
| -------------------------------- | ---------------------------------------------------------- | ------------------------ |
| 任意路径、目录遍历、符号链接越界 | 只收 multipart bytes；basename 仅显示；无 path 字段        | API 与单元测试           |
| 超大文件耗尽内存/数据库          | HTTP 17 MiB 请求上限、16 MiB 文件上限、客户端预检          | API 边界测试待扩充       |
| 损坏/二进制文本污染索引          | 扩展名白名单、UTF-8、NUL、空白校验                         | Go API 测试              |
| FTS 查询注入                     | 词项化、引用、数量与长度上限                               | Go API 测试              |
| 中文无法检索                     | 原文索引 + 本地中文单字/双字检索列                         | 中英文回归测试           |
| 重建竞争覆盖新版来源             | `If-Match`、事务内 version/hash 重验                       | Go API 测试              |
| 网络重试重复导入                 | Idempotency-Key + 规范请求哈希 + 首响应重放                | Go/Web 重放测试          |
| 删除留下 FTS/正文                | FK cascade + FTS trigger + 清空 BLOB                       | 迁移/API 清理测试        |
| 业务导出泄露资料                 | 所有知识库与 FTS shadow table 显式 excluded                | business export 分类测试 |
| 正文进入日志/错误                | access log 仅记录 route/status/duration；错误使用稳定 code | 代码审查与 API 测试      |
| 提示注入变成系统指令             | ADR-010 只允许用户显式选择的引用片段，并置于不可信引用段   | AI6 双协议/历史测试      |

## 被拒方案

1. **前端直接提交绝对路径**：浏览器和 Sidecar 都无法证明路径经过用户授权，也会扩大目录遍历与日志泄露面。
2. **首版把文件复制到新的 `knowledge/` 目录**：现有一致性备份只认识当前受控文件域；只加目录会产生无法验证的恢复缺口。
3. **先接远程向量库或 embedding**：破坏断网闭环，引入新的数据外发、密钥、成本和可删除性问题。
4. **直接用用户查询作为 FTS MATCH 表达式**：允许操作符改变语义，也会制造语法错误与拒绝服务面。
5. **让 AI 自动扫描或自动引用整个来源**：无法满足最小披露、逐项确认和来源版本解释要求。

## 后果

- 优点：首个切片没有新增生产依赖或在线服务；文本知识在一个事务和备份边界内可检索、重建和删除。
- 代价：16 MiB BLOB 会增加 SQLite 体积；单邮箱限制并行吞吐，但避免多个导入同时争用 SQLite；PDF、大文件和授权引用仍需扩展受控文件体系。
- 后续门禁：先完成 PDF 资源治理、授权引用和低磁盘/大文件性能基线，再评审 AI6 显式片段上下文；向量检索另立 ADR。
