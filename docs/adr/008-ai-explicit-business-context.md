# ADR-008：AI 显式业务上下文与发送前预览

- 状态：已接受并实施（AI5 已交付）
- 日期：2026-09-08
- 对应计划：[AI 显式上下文分阶段计划](../plans/ai-explicit-context-phases.md)
- 授权：用户要求开始按 AI 助手文档继续执行；本 ADR 落实模块文档 AI5 已登记的 Task/Project/Client 显式上下文、发送前范围预览与最小快照
- 当前基线：app v0.1.1 / API v1 / SQLite schema 058

## 问题

当前 AI 助手只能看到对话、会话记忆与用户输入，无法解释用户指向的 Task、Project 或 Client。直接让模型读取业务数据库会破坏本地事实边界，也无法让用户在远程 Provider 场景知道具体外发了什么。

## 决策

### 1. 上下文必须逐项显式选择

- 每次消息最多选择一个 Task、一个 Project、一个 Client；未选择时 AI 行为保持原样。
- 不从 Task 自动扩展 Project/Client，不从 Project 自动扩展 Client/Tasks，不读取附件、Artifact、活动、发票、联系方式或其他关系数据。
- 选择只作用于下一条消息；发送成功后清空，失败时保留供用户修正或重新预览。

### 2. Sidecar 拥有最小快照

- Task：标题、描述（有界）、类型、状态、优先级、完成条件（有界）、计划/截止日、直接所属 Project ID/名称、版本。
- Project：名称、描述（有界）、状态、起止日期、任务总数及 done/blocked/waiting_review 数、版本；不包含金额或 Client。
- Client：名称、状态、备注（有界）、版本；不包含联系人、邮箱、电话、活动、附件、项目或财务。
- 长文本按 Unicode 字符在 2,000 字内截断，并在预览字段中明确标记；全部上下文序列化后不得超过 16 KiB。

### 3. 发送前预览与版本绑定

- `POST /api/v1/ai/context/preview` 接受 Provider ID 和显式来源，返回 Provider 名称/类型/版本、是否离开设备、规范化来源、字段和值、截断标记及序列化字节数。
- 聊天请求携带预览返回的 Provider version 和每个来源 version。Sidecar 在创建会话、消息或 generation 前重新构建同一最小快照；任一版本变化返回 `AI_CONTEXT_CHANGED`，Provider 变化返回 `AI_CONTEXT_PROVIDER_CHANGED`。
- Web 必须展示目标 Provider、本地/远程边界和全部字段，用户点击“确认用于下一条消息”后才允许携带上下文发送。

### 4. 持久化与历史解释

- schema 058 为 `ai_messages` 增加 nullable `context_snapshot` JSON；仅持久会话的 user message 保存实际发送的 Provider 身份/类型/版本及来源快照。
- 历史消息 API 返回目标 Provider 与来源的类型、ID、版本、标题和预览字段，聊天页以只读上下文卡片展示。
- 快照属于 AI 操作态，继续排除业务 JSON/ZIP；一致性 SQLite 备份照常覆盖。

### 5. 提示词与工具边界

- 显式上下文作为独立 system 段注入，纳入最终协议请求 64 KiB 精确预算；它不进入长期记忆或会话压缩输入。
- 记忆工具仍只能访问当前会话 AI 操作态；不新增 Task/Project/Client 读取工具，不允许模型扩大选择范围。
- 模型回复仍只读；任务创建继续通过人工确认卡片和既有 Task API。

## 被拒方案

- 自动读取“当前页面”或最近业务对象：用户无法确定外发范围。
- 只在前端拼接 JSON：绕过 Sidecar 权限、版本与审计边界。
- 发送完整实体/API 响应：包含无关派生字段和潜在敏感数据。
- 用名称绑定预览：同名实体与并发变更不可可靠识别；采用 ID + version。
- 把上下文长期保存为记忆：一次性业务上下文与持久偏好语义不同。

## 验收

- 未显式选择时请求不含业务上下文；非法、重复、超量或不存在来源被拒绝。
- 预览和聊天重建使用同一纯函数字段契约；版本变化在任何 AI 写入前原子拒绝。
- 远程预览明确“将发送给 Provider”，本地预览明确回环；不显示/发送排除字段。
- OpenAI/Anthropic 最终请求都包含独立上下文段并保持 64 KiB 上限。
- 历史 user message 可解释实际发送来源；非持久会话不落快照。
- 业务导出分类、迁移、Go/Web、文档和构建回归通过。
