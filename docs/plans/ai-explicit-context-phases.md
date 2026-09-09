# AI 显式业务上下文——分阶段实施计划

> 状态：C1–C4 功能与相关门禁已完成；依据 [ADR-008](../adr/008-ai-explicit-business-context.md)

## C1：契约与安全边界（已完成）

1. 固化 Task/Project/Client 最小字段白名单、2,000 字截断、最多三来源和 16 KiB 总预算。
2. 固化 Preview → 用户确认 → Chat 按 Provider/source version 重建的链路。
3. 明确一次性选择、远程外发、本地回环、历史快照和不自动扩展关系。

## C2：Sidecar 纵向切片（已完成）

1. schema 058 为 user message 增加 `context_snapshot`；同步迁移、业务导入兼容与导出排除契约。
2. 实现 `POST /api/v1/ai/context/preview`、统一 ContextBuilder、错误码和严格输入。
3. Chat 在持久化前重新验证 Provider/source version，把上下文作为独立 prompt 段并保存实际快照。
4. 消息 API 返回历史上下文；非持久会话不落正文或上下文。

## C3：Web 选择、预览与历史（已完成）

1. 复用 TaskSelect/ProjectSelect/ClientSelect，最多各选一个；改变来源或 Provider 后旧确认失效。
2. Preview Modal 展示目标 Provider、是否离开设备、序列化大小、实体字段和截断标记。
3. 只有确认后的 preview 可随下一条消息发送；成功后清空，失败或版本变化保留。
4. 当前发送和历史 user message 展示上下文来源卡片。

## C4：验证与收尾（已完成）

1. Go：白名单、截断、预算、重复/不存在/版本冲突、发送前无脏写、双协议注入、持久/非持久消息。
2. Web：选择、预览、Provider/来源变化失效、发送载荷、失败保留、历史卡片和远程外发文案。
3. 全量 typecheck/test/build、Go vet/test、迁移与业务导入兼容、文档链接和格式检查。
4. 更新 PRD、功能架构、AI 模块、索引、README 与 Sidecar 说明；只按实际验证结果标记完成。

## 交付记录（2026-09-08）

- schema 058、统一白名单 Builder、Preview API、Provider/source version 重验、user message 快照和历史响应已交付；上下文进入独立 prompt 段并计入最终 64 KiB 请求预算。
- Web 已交付三类选择、未预览禁发、本地/远程逐字段 Modal、版本变化失效、一次性清理与历史 Provider/source 卡片；真实本地 Sidecar 浏览器检查通过。
- Web typecheck、113 文件/1068 测试与生产构建通过；文档、Prettier、gofmt、Go vet、数据库/modelclient/harness/cmd/config/operationlog/runlease 全量，以及 AI5/导入兼容专项和 race detector 通过。
- `internal/api` 全量仍只有两项在未修改 AI 分支同样失败的 Automation 时间基线测试；AI5 未新增失败，详见上下文记忆计划的同一验证边界。
