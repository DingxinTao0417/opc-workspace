# 本地知识库分阶段实施计划

> 依据：[ADR-009](../adr/009-local-knowledge-base-ingestion-and-search.md) 与 [知识库模块](../modules/knowledge-base.md)
>
> 当前状态（2026-09-08）：K1–K4、K5 核心文本闭环与 K6 已完成。TXT/Markdown 由本地单邮箱 Actor 异步索引，支持可观察取消、失败重试与启动中断恢复；来源清单与 AI 显式片段已交付。PDF、授权引用/变化检测尚未完成。

## 阶段与完成定义

| 阶段              | 状态                | 交付内容                                                                                    | 完成门禁                                     |
| ----------------- | ------------------- | ------------------------------------------------------------------------------------------- | -------------------------------------------- |
| K1 决策与威胁模型 | 已完成              | ADR-009 固化来源、路径、格式、上限、备份、删除、网络与 AI 边界                              | ADR 与模块文档一致                           |
| K2 数据与任务框架 | 已完成              | schema 059 + 单邮箱 Actor；queued/running/terminal、阶段进度、取消、retry attempt、启动恢复 | Actor 竞态、旧索引保留和中断无半成品测试通过 |
| K3 文本导入       | TXT/Markdown 已完成 | 单文件受控上传、UTF-8/NUL/空白校验、换行规范化、1,200 字符分段                              | 16 MiB 与损坏输入；PDF 另阶段                |
| K4 本地检索       | 已完成              | FTS5、中文单字/双字辅助列、来源筛选、rank、纯文本高亮与行/字符引用                          | 中文、英文、无结果、过滤和注入输入测试通过   |
| K5 生命周期       | 核心闭环已完成      | 版本化重建、单文档删除后恢复、来源删除、全库清理、FTS 级联、metadata-only CSV               | 来源变化自动标 stale 仍待                    |
| K6 AI 集成        | 已完成              | ADR-010 显式片段 + ADR-011 回答级 allowlist citation、无证据/缺失/非法状态                  | AI 无整库扫描工具；句子级覆盖率待 AI7-Q2     |

## 本次文件范围

- 数据：`059_local_knowledge_base.sql` 和 `models/knowledge.go`。
- Sidecar：`knowledge.go` 与 `/api/v1/knowledge/*` 路由。
- Web：`KnowledgeBasePage`、客户端严格解析、主导航与命令面板入口。
- 文档：ADR-009、知识库模块、PRD、整体架构、README 与 Sidecar README。

## 已实现契约

1. `POST /api/v1/knowledge/sources` 只接受一个 TXT/Markdown multipart 文件；不接受路径；保存 queued Job 后返回 `202`。Web 默认生成 `Idempotency-Key`，同请求重放不重复创建来源或 Job。
2. 单邮箱本地 Actor 推进阶段；source、document、chunks、FTS 和 succeeded Job 在最终单事务发布。
3. `POST /api/v1/knowledge/search` 返回来源、文档版本、chunk、行/字符范围和 Unicode 高亮范围。
4. 重建、单文档删除和来源删除均使用来源 `If-Match`；来源删除另需 `confirm=true`。重建运行时旧 document 保持可搜索。
5. 全库清理需要 query 与精确确认 header；所有事实与 FTS 派生行级联清除。
6. 知识库表不进入便携业务导出，完整 SQLite 备份覆盖知识库；单独 CSV 只含来源元数据、版本和索引状态，不含正文。
7. AI6 只允许用户显式选择的 1–3 个 chunk，完整正文经 preview 确认并在 Chat 写入前重验版本；无 knowledge tool。

## 验证记录

- `go test ./internal/database -count=1`：通过，包含 FTS trigger 插入/级联删除。
- `go test ./internal/api -run '^TestKnowledge' -count=1`：通过，覆盖异步导入/幂等重放、Actor queued/running、运行中取消、retry attempt、启动中断恢复、旧索引保留、中文检索、来源过滤、文档定位、重建、删除、清理和非法文本。
- `vitest`：知识库 API 客户端与页面测试通过，覆盖 multipart、严格解析、`If-Match`、来源筛选、高亮、Actor 阶段/进度、取消/retry、metadata-only CSV、导入和删除确认。
- 完整 Web 门禁通过：121 个测试文件、1,109 项测试，typecheck 与 production build 成功；仅保留既有 bundle size warning。
- 完整 Go 中 database、Harness、ModelClient 与其他包通过；`internal/api` 仍只有已在 untouched AI branch 独立复现的两条 Automation Event Delivery 基线失败：`TestAutomationEventDeliveryReplayDoesNotRecaptureCompletedRunAfterRuleEdit`、`TestDisabledEventAutomationRunStillRetriesWhenDue`。
- race detector 通过 Knowledge Actor 取消/retry/恢复/旧索引与 AI5/AI6 上下文专项；文档 52 个 Markdown 文件及本地链接通过。
- 真实本地 Sidecar/schema 059 + Web 视觉检查通过：来源/进度卡、中文高亮、筛选、删除确认、AI 本地搜索、逐段选择和完整不可信引用 preview 均正常；临时服务已停止，临时数据已移入废纸篓。

## 后续顺序

1. K3b：评审本地 PDF 提取依赖、页码定位、压缩炸弹/密码文件/无文本页和跨平台包体；补低磁盘与较大文本性能基线。
2. K5b：桌面授权引用、mtime/hash 变化检测、stale 与原子新版本切换。
3. AI6 已按 ADR-010 完成；后续结构化 citation 和无答案评测归 AI7，不开放自动整库检索。
