# AI6 显式知识片段实施计划

> 依据：[ADR-010](../adr/010-ai-explicit-knowledge-context.md)、[ADR-009](../adr/009-local-knowledge-base-ingestion-and-search.md)
>
> 当前状态（2026-09-10）：AI6、ADR-011 回答级 citation、AI7-Q2 离线 scorer/本地模型评测及固定无答案/冲突/注入套件已交付；评测后续由 [AI7 计划](ai-quality-gates.md) 与 [ADR-025](../adr/025-ai-reliability-confirmations-and-evaluation-identity.md) 接续。知识搜索仍由用户发起，模型没有整库工具；PDF、句子级覆盖率仍待后续。

## 阶段与状态

| 阶段                 | 状态     | 交付                                                                                             |
| -------------------- | -------- | ------------------------------------------------------------------------------------------------ |
| AI6-1 决策与威胁模型 | 已完成   | ADR-010：显式片段、提示注入、版本、预算、历史与删除边界                                          |
| AI6-2 预览契约       | 已完成   | `/ai/context/preview` 接 1–3 knowledge identities，返回 Provider/版本/位置/正文/字节数           |
| AI6-3 Chat 原子门禁  | 已完成   | Provider/source/document 两次重验；任何 AI 写入前拒绝 changed/not-found                          |
| AI6-4 Prompt/Harness | 已完成   | 独立不可信引用段；双协议、工具轮与 self-check 修订保持上下文；无 knowledge tool                  |
| AI6-5 Web 与历史     | 已完成   | AI 面板本地搜索/逐段选择、预览确认、远程披露、发送、历史来源 chips                               |
| AI6-6 评测增强       | 部分完成 | 回答级 allowlist citation、无答案/冲突/注入固定集与本地评测已交付；句子级覆盖率和 PDF 页码待后续 |

## 固定契约

1. 每条消息最多 3 个知识 chunk，可与最多各一个 Task/Project/Client 组合。
2. Preview 输入不相信前端正文；Sidecar 从三层 identity 重建完整 chunk。
3. Chat 输入必须携带 preview 返回的 source/document version；第二次事务重建结果必须逐字节一致。
4. 知识上下文 12 KiB，组合 snapshot 30 KiB，最终 Provider 请求 64 KiB；任一超限显式失败。
5. v1 business-only snapshot 继续可读；v2 保存 `knowledge`，历史 API 输出 `context_knowledge`。
6. 生产工具仍只有 `memory_search / memory_write / memory_propose`。

## 验证记录

- Go API：knowledge preview→chat→snapshot→history 金链、source/document version 变化零写入、identity/duplicate/not-found 边界。
- Model client/Harness：OpenAI/Anthropic 独立不可信引用段、工具循环和 self-check 修订保留知识上下文。
- Web API/Page：preview/chat 精确载荷、发送前禁用、片段上限、远程披露、选择变更失效和历史来源 chips。
- 完整 Web 门禁通过：121 个测试文件、1,109 项测试，typecheck 与 production build 成功；仅有既有 bundle size warning。
- AI5/AI6 + Knowledge race 专项、Harness 与 ModelClient 通过；完整 Go 仍只有已在 untouched AI branch 独立复现的两条 Automation Event Delivery 基线失败，与本切片无关。
- 文档 52 个 Markdown 文件及本地链接通过。真实本地 Sidecar/schema 059 + Web 已检查本地 Provider、知识搜索、选择 chip、行号/版本、Local-only 披露和完整不可信正文 preview；未发送模型请求，临时服务已停止且数据已移入废纸篓。

## 后续

1. 带预期事实、允许来源、无答案/冲突/注入的离线评测集与本地 runner 已交付；继续扩展事实覆盖，真实模型质量须实机验证，规则评分不替代人工评审。
2. 回答级结构化 citation、合法 chunk allowlist 和缺失/非法反馈已按 ADR-011 完成；后续评审句子级归因与覆盖率，不能把回答级声明写成事实正确性证明。
3. PDF 交付后扩展 page/location 引用；向量检索若立项需单独 ADR。
