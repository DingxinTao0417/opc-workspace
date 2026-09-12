# opc-workspace 文档中心

本目录集中维护 opc-workspace 的产品范围、整体功能架构和模块级实现契约。

> 当前代码基线为 app v0.1.1 / API v1 / SQLite schema 70。PRD v10.9 与 [ADR-025](adr/025-ai-reliability-confirmations-and-evaluation-identity.md) 收口 AI 运行隔离/预算、流终态、应用级恢复、任务确认事务、记忆决定、分段压缩和 dataset v4 配置身份评测；[ADR-026](adr/026-local-knowledge-pdf-extraction.md) 交付知识库 PDF 提取与页码定位（schema 070）。业务导入兼容为 v49/v63–v69→70，AI/知识库操作态仍排除便携业务导出。业务冲突合并、外部备份目录和覆盖仍禁用。当前 Windows x64 已完成 Tauri 原生链接、Rust 测试及未签名本地打包；实机模型、真实故障与跨平台发布验收不由隔离自动化代替。

## 阅读顺序与事实优先级

1. [产品需求文档（PRD v10.9）](opc-workspace-PRD.md)：产品范围、版本边界、数据/API 目标契约和当前状态。
2. [整体功能架构](functional-architecture.md)：模块如何协作、事件如何流转、谁拥有哪类事实。
3. [模块文档](modules/README.md)：单个模块的用户流程、数据、API、依赖、实施阶段和验收条件。
4. 仓库代码与测试：判断“现在实际实现了什么”的最终证据。
5. 当前 React 页面与 `styles.css`：判断现有视觉和交互事实；历史 HTML 原型已于 2026-08-27 从仓库移除。

发生冲突时，当前实现事实以代码和测试为准；目标范围以 PRD 为准；跨模块关系以整体功能架构为准；模块文档负责展开落地细节。

## 核心模块

| 模块           | 当前状态                                                                                                                         | 目标版本                    | 文档                                 |
| -------------- | -------------------------------------------------------------------------------------------------------------------------------- | --------------------------- | ------------------------------------ |
| 今日工作台     | 部分完成（T-06A–H、截止风险、客户回访待办，以及右侧真实客户动态/临近路线图节点已交付）                                           | v0.1                        | [today.md](modules/today.md)         |
| 任务管理       | 部分完成（事实层、D1/D2、筛选/保存视图、共享服务端 Client/Project 选择、计划组拖拽、受控跨列看板与父任务自动待验收已交付）       | v0.1                        | [tasks.md](modules/tasks.md)         |
| 项目管理       | 部分完成（含 Artifact nullable follow-up/实时 required 进度、四种跟进状态、Inbox 深链、任务/Focus/Client 活动协作）              | v0.1                        | [projects.md](modules/projects.md)   |
| 客户管理       | 部分完成（基础资料、共享分页搜索选择器、Project 关联、人工活动/附件/person、回访 API/到期 Inbox 投影及详情管理已交付）           | v0.1；回访/财务 v0.4        | [clients.md](modules/clients.md)     |
| 收件箱工作编排 | 部分完成（人工闭环含来源 Project 继承/清除、完成条件、person 本地责任、共享 Task 详情、缓存失效与 automatic resolved/100% 金链） | 人工闭环 v0.1；Agent v0.2   | [inbox.md](modules/inbox.md)         |
| 本地提醒       | 一次性及 daily/weekly/weekdays/monthly Reminder、启动补偿与到期 Inbox 投影已完成                                                 | v0.1；复杂规则/原生通知后续 | [reminders.md](modules/reminders.md) |
| Actor 与分派   | 部分完成（Actor、Assignment、生命周期与 D2 产出责任已交付；Agent 未实现）                                                        | v0.1                        | [actors.md](modules/actors.md)       |
| 专注与工时     | Core A+B+C+D1+D2a、日期范围回顾与项目详情 Focus 读取已完成；原生反馈延后                                                         | v0.1                        | [focus.md](modules/focus.md)         |

## 平台与共享能力

| 模块                       | 当前状态                                                                                                                                                                               | 目标版本            | 文档                                               |
| -------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------- | -------------------------------------------------- |
| 本地 Agent Runtime         | 安全 ADR、受限 Adapter 登记与诊断已交付；Runner、Agent Run 与执行尚未实现                                                                                                              | v0.2                | [local-agents.md](modules/local-agents.md)         |
| 设置                       | 部分完成                                                                                                                                                                               | v0.1 / v0.2         | [settings.md](modules/settings.md)                 |
| 命令面板与搜索             | 核心本地搜索、详情直达、本地最近使用、脱敏运行诊断/诊断包和全局渲染错误恢复完成；OS 快捷键待后续                                                                                       | v0.1                | [command-search.md](modules/command-search.md)     |
| 数据、受控文件、备份与恢复 | 迁移、Artifact store、备份/恢复完整闭环、每日计划/启动补偿/自动包安全保留、业务 JSON/含文件 ZIP 空目标及同 schema 零主键冲突追加已交付；启动前备份选择、外部目录、冲突合并及升级待实现 | v0.1；高级配置 v0.3 | [data-management.md](modules/data-management.md)   |
| 桌面平台与发布             | 部分完成（Sidecar 有界恢复/父管道/运行锁/并发 shutdown、托盘最小源码闭环和运行诊断能力快照已交付；托盘原生链接、真实父崩溃/进程树、三平台与安装包仍待验收）                            | v0.1 发布闸门       | [desktop-platform.md](modules/desktop-platform.md) |

## 后续业务与规划模块

| 模块             | 当前状态                                                                                                                                               | 目标版本         | 文档                                               |
| ---------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ | ---------------- | -------------------------------------------------- |
| 收入、支出与发票 | 页面骨架 / 数据表预留                                                                                                                                  | v0.4             | [finance-invoices.md](modules/finance-invoices.md) |
| 客户回访         | C2–C5 数据/API、原子下一次计划、到期 Inbox 投影、详情管理及 Today/Inbox 入口完成                                                                       | v0.4             | [client-followups.md](modules/client-followups.md) |
| 路线图           | R2/R3/R5 完成，R4 同季度排序、跨季度/跨年度移动与季度内精确日期拖拽已交付                                                                              | v0.3             | [roadmap.md](modules/roadmap.md)                   |
| 内容日历         | CC1–CC5-B、CC6-A 与指定详情 URL 已交付；拖拽/键盘改期即时预移且失败回滚，审核/发布 Inbox 可精确回到跨月份最新详情，不自动外发                          | v0.3             | [content-calendar.md](modules/content-calendar.md) |
| 预设自动化       | 首个纵向切片完成                                                                                                                                       | v0.2             | [automation.md](modules/automation.md)             |
| 本地知识库       | TXT/Markdown/PDF（ADR-026 页码定位）、Actor、FTS5/中文检索、定位、重建、删除、清单与 ADR-010 AI6 显式片段已交付；授权引用、来源变化待后续              | 独立轨道         | [knowledge-base.md](modules/knowledge-base.md)     |
| AI 助手          | Provider/Harness/记忆/显式上下文/citation、dataset v4 配置身份评测/人工审计、steps/用量趋势，以及 ADR-025 可靠性、确认与恢复已交付；真机场景待专项验收 | 待定（独立轨道） | [ai-assistant.md](modules/ai-assistant.md)         |

## 全局产品边界

- 所有核心业务、Actor、任务、收件箱、提醒、产出和运行记录默认只保存在本机。
- v0.1 不引入账号、多人登录、远程任务领取、云同步或线上工作流。
- v0.1 不调用 AI/LLM，不创建或运行 Agent；Project Artifact→Inbox→Task 只使用 owner/person 与 owner manual review。AI 助手按 ADR-004 在独立轨道交付（远程 Provider + 只读会话 + 语义建任务确认卡片），不并入 v0.1–v0.4。
- `person` Actor 只记录线下责任，不会向对方发送任务或授予应用权限。
- manual Artifact 的 producer 由当前 active assignee 派生；内置 owner 负责代录、提交、审核、撤回和删除，不能由客户端伪造 Actor ID。
- Task file Artifact、Client Attachment、Project Attachment 与 Workspace Avatar 只保存在 Sidecar 声明的同一受控目录并经鉴权 API 下载；受控根通过身份 marker、Artifact root 锁、耐久同步与 quarantine 防止错库、双写和误删。数据库父目录另用固定 `.opc-sidecar-run.lock` 在任何恢复、迁移或打开前阻止第二个 Sidecar 接触同库。应用已能管理 SQLite+active files 内部备份，以及业务 JSON/含文件业务 ZIP 的空目标和同 schema 零主键冲突追加；预检可只读列出非空目标表/主键重叠、目标文件碰撞并分类跨 schema 方向，真实冲突策略、UUID 重映射与版本升级仍未实现。
- 实际 Agent 执行归入 v0.2，必须使用受控本地 Adapter、专用鉴权和可验证的隔离边界。
- Agent Run 成功只表示产生了结果；高风险或要求审核的任务必须由 owner 验收后才完成。
- 发票、客户沟通、付款确认、数据删除等高风险动作不得由 Agent 无审核完成。
- 当前阶段使用签名离线更新包，不依赖在线 Updater。

## 模块文档维护模板

每份模块文档至少维护以下内容：

1. 定位与非目标。
2. 当前实现状态与代码证据。
3. 目标功能和用户流程。
4. 数据事实、API、状态机和事件。
5. 与其他模块的输入、输出和依赖。
6. 分阶段实施顺序。
7. 可验证的验收标准。

代码交付一个纵向切片时，应同时更新对应模块文档的当前状态、PRD 的实现追踪和实际验证证据。只有页面、按钮外观、静态样式或数据库预留表时，不得将模块标记为“已完成”。

## 规划草稿（待评审）

- [AI 助手 MVP 实施计划（已评审并实施；实现状态以 [模块文档](modules/ai-assistant.md) 为准）](plans/ai-assistant-mvp.md)
- [Agent Harness 分阶段计划（ADR-005/006 已实施；实现状态以 [模块文档](modules/ai-assistant.md) 为准）](plans/agent-harness-phases.md)
- [会话上下文压缩与记忆工具分阶段计划（ADR-007；G1–G5 已交付）](plans/context-memory-phases.md)
- [AI 显式业务上下文分阶段计划（ADR-008；AI5 已交付）](plans/ai-explicit-context-phases.md)
- [本地知识库分阶段计划（ADR-009/026；K1–K6 与 K3b PDF 已交付）](plans/knowledge-base-phases.md)
- [AI6 显式知识片段实施计划（ADR-010；已交付）](plans/ai-knowledge-context-phases.md)
- [AI7 质量闸门与可解释性计划（ADR-011–025；引用、本地质量/用量、可靠性与确认恢复；验证记录集中维护）](plans/ai-quality-gates.md)

## 架构决策

- [ADR-003：本地 Agent Runtime 安全与传输边界](adr/003-local-agent-runtime-security.md)
- [ADR-004：AI 助手远程 Provider 接入与安全边界](adr/004-ai-assistant-provider-access.md)
- [ADR-005：Agent Harness 架构与本地大模型接入](adr/005-agent-harness-and-local-models.md)
- [ADR-006：Harness 完整组件矩阵与自进化边界](adr/006-harness-matrix-memory-evolution.md)
- [ADR-007：会话上下文压缩与记忆工具（G1–G5 已交付）](adr/007-session-context-compaction-and-memory-tools.md)
- [ADR-008：AI 显式业务上下文与发送前预览（AI5 已交付）](adr/008-ai-explicit-business-context.md)
- [ADR-009：本地知识库导入、索引与检索边界（文本基线已交付；PDF 由 ADR-026 接续）](adr/009-local-knowledge-base-ingestion-and-search.md)
- [ADR-010：AI 显式知识片段上下文与来源引用（AI6 已交付）](adr/010-ai-explicit-knowledge-context.md)
- [ADR-011：AI 回答的可验证知识引用（AI7-Q1 已交付）](adr/011-ai-validated-knowledge-citations.md)
- [ADR-012：AI 运行步骤与本地无正文指标（AI7-Q3 steps、Provider usage 与本地聚合已交付）](adr/012-ai-run-steps-and-local-metrics.md)
- [ADR-013：AI 本地模型质量评测 Actor（AI7-Q2 显式本地运行闭环已交付）](adr/013-ai-local-quality-evaluation-runner.md)
- [ADR-014：AI 本地质量分组与时间趋势（AI7-Q2 同口径趋势已交付）](adr/014-ai-local-quality-trends.md)
- [ADR-015：AI 本地质量类别分解（AI7-Q2 category 统计已交付）](adr/015-ai-local-quality-categories.md)
- [ADR-016：AI 本地质量失败原因聚合（AI7-Q2 failure-code 统计已交付）](adr/016-ai-local-quality-failure-codes.md)
- [ADR-017：AI 本地质量评测数据集 v2（12-case 多样本纵切已交付）](adr/017-ai-local-quality-dataset-v2.md)
- [ADR-018：AI 本地质量 Wilson 区间与重复证据（描述性不确定性已交付）](adr/018-ai-local-quality-confidence-intervals.md)
- [ADR-019：AI 本地质量人工评审候选（advisory readiness 已交付）](adr/019-ai-local-quality-advisory-readiness.md)
- [ADR-020：AI 本地质量评测数据集 v3（24-case 事实覆盖已交付）](adr/020-ai-local-quality-dataset-v3.md)
- [ADR-021：AI 本地质量分层评测套件（8-case smoke / 24-case full 已交付）](adr/021-ai-local-quality-tiered-suites.md)
- [ADR-022：AI 本地质量人工决定审计（精确证据快照与不可变理由已交付）](adr/022-ai-local-quality-human-review-audit.md)
- [ADR-023：AI 本地质量代码所有专题套件（四类 6-case 已交付）](adr/023-ai-local-quality-topic-suites.md)
- [ADR-024：AI 本地用量 UTC 时间趋势（1–30 天已交付）](adr/024-ai-local-usage-time-trends.md)
- [ADR-025：AI 运行可靠性、确认事务与评测配置身份（schema 068–069、dataset v4）](adr/025-ai-reliability-confirmations-and-evaluation-identity.md)
- [ADR-026：本地知识库 PDF 文本提取与页码定位（K3b 已交付）](adr/026-local-knowledge-pdf-extraction.md)

## 核心术语

| 术语              | 含义                                                                                              |
| ----------------- | ------------------------------------------------------------------------------------------------- |
| Inbox Item        | 说明为什么需要处理、来源和跟进策略，不承担任务执行状态                                            |
| Inbox–Task 关系   | 保存活动/历史关联、required、顺序和软解除；进度实时从 Task 派生                                   |
| Task              | 唯一可执行工单实体，保存工作内容、状态、完成条件和验收策略                                        |
| Actor             | 本地责任主体：owner、person、agent、system                                                        |
| Assignment        | Task 当前负责人和历史改派记录                                                                     |
| Task Submission   | 一次提交批次；`origin=manual/child_rollup` 区分人工产出和系统子任务汇总，并保存审核状态与操作责任 |
| Agent Run         | 本地 Agent 的一次执行尝试；成功不等于 Task 完成                                                   |
| Task Artifact     | text/file/link/structured 产出，区分实际产出者与 owner 录入者，带完整性和软删除审计               |
| Client Attachment | 客户本地受控文件，可选关联 Activity，带完整性、软删除和聚合删除补偿                               |
| Client Actor Link | Client 与 active person 的显式本地 contact 关系，带不可变解除历史                                 |
| Reminder          | 本地调度事实；到期后幂等生成 Inbox Item                                                           |
| Workflow Event    | 创建、拆分、分派、执行、验收和返工的追加式审计时间线                                              |
| Focus Session     | 服务端持久化的一次工作段；interval 保存实际计入区间，前端 ticker 只派生显示                       |
