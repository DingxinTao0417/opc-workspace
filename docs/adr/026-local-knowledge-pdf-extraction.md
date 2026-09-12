# ADR-026：本地知识库 PDF 文本提取与页码定位

- 状态：Accepted，K3b 纵向切片已实现
- 日期：2026-09-12
- 决策范围：知识库 K3b（PDF 导入、页码定位、资源上限）
- 相关文档：[ADR-009](009-local-knowledge-base-ingestion-and-search.md)、[知识库模块](../modules/knowledge-base.md)、[实施计划](../plans/knowledge-base-phases.md)

## 背景

ADR-009 的文本基线只接受 TXT/Markdown，把 PDF 留给独立评审。PDF 提取需要回答四个问题：纯 Go 依赖选型（Sidecar 在 `CGO_ENABLED=0` 下构建，无 C 编译器）、中文 PDF 的文本可提取性、页码级引用如何进入既有 chunk/FTS/AI 引用链，以及压缩炸弹、加密文件与无文本页的资源上限。

## 决策

### 1. 依赖：`github.com/ledongthuc/pdf`（纯 Go、零传递依赖）

- 实测验证（gopdf + 仓库内 Noto Sans SC 生成的中英文 PDF）：中文经 ToUnicode CMap 完整提取、行结构保留、页边界清晰；非法头、截断文件返回明确错误；加密文件在空用户密码解密失败时拒绝打开。
- 该库沿袭 rsc.io/pdf，用 panic 报告结构错误。Sidecar 侧不直接调用其 `GetPlainText`，而是用其导出的 `Interpret`/`Value`/`Font` API 自实现同一文本操作符遍历，外包一层 `recover` 与预算控制（见第 3 条）。库的 MIT 许可与零传递依赖保持包体影响最小。

### 2. 页码定位：chunk 增加 `start_page` / `end_page`

- schema 070 重建 `knowledge_sources` 以把 `source_type` 扩展为 `('text','markdown','pdf')`，并给 `knowledge_chunks` 增加 `start_page`/`end_page`（默认 1，`end_page >= start_page`）；文本来源恒为 1/1。
- 提取按页进行并归一化换行；无文本页不贡献行号，由有文本页连续覆盖全部行号，chunk 的行号范围经页映射得到页码范围。FTS5、检索、AI6 显式片段、citation 元数据都以增量字段携带页码。

### 3. 资源上限与稳定失败码

| 风险             | 控制                                                                | 稳定错误码                                                   |
| ---------------- | ------------------------------------------------------------------- | ------------------------------------------------------------ |
| 压缩炸弹文本膨胀 | 解释器累计文本上限 16,777,216 runes（与 `content_text` CHECK 一致） | `KNOWLEDGE_PDF_TEXT_TOO_LARGE`                               |
| 退化内容流耗 CPU | 解释器操作数上限 5,000,000，页数上限 20,000                         | `KNOWLEDGE_PDF_TOO_COMPLEX` / `KNOWLEDGE_PDF_TOO_MANY_PAGES` |
| 加密文件         | 打开阶段空密码失败即拒绝                                            | `KNOWLEDGE_PDF_ENCRYPTED`                                    |
| 损坏/非 PDF      | 头部魔数 `%PDF-` 预检（上传时）与解析错误（索引时）                 | `KNOWLEDGE_PDF_INVALID`                                      |
| 扫描件/无文本页  | 无文本页跳过；全库无文本时失败                                      | `KNOWLEDGE_PDF_NO_TEXT`                                      |

- 上传时只做魔数与 16 MiB 预检；完整提取仍由单邮箱 Actor 在 `extracting` 阶段异步执行，失败进入既有 failed/retry 生命周期，不阻塞请求。
- 提取不创建网络连接、不执行嵌入脚本、不做 OCR；文档 `extractor_version` 记录为 `pdf-text-v1`。

## 被拒方案

1. **MuPDF/Poppler 绑定（go-fitz 等）**：需要 CGO 与原生库，破坏 `CGO_ENABLED=0` 构建与跨平台包体约束。
2. **`pdfcpu`**：以 PDF 操作为主，文本提取非其稳定能力，且依赖树显著更大。
3. **直接调用上游 `GetPlainText`**：无法注入文本/操作预算，压缩炸弹可放大到多 GB 内存；自实现遍历是唯一能强制预算的边界。
4. **把 PDF 页渲染为图片做 OCR**：OCR 质量与包体代价高，另需独立 ADR。

## 后果

- 优点：中文/英文 PDF 在本地闭环内可检索并保留页码级引用；知识库事实、派生文本、FTS 与页码仍在同一 SQLite 备份边界。
- 代价：纯 Go 提取对复杂排版（表格、多栏、非 ToUnicode 编码）的还原有限；这些输入以稳定错误码失败或文本缺失，不伪造内容。
- 后续门禁：文件对话框授权引用、来源变化 stale 检测与增量重建仍按 [knowledge-base-phases](../plans/knowledge-base-phases.md) 推进；向量检索仍需单独 ADR。
