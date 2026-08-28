# 模块文档索引

本目录按功能边界拆分模块文档。每份文档同时记录当前实现事实与目标规划，不能仅凭文档中的目标功能判断代码已经完成。

导航：[文档中心](../README.md) · [整体功能架构](../functional-architecture.md) · [PRD v2.0](../opc-workspace-PRD.md)

## v0.1 核心闭环

- [今日工作台](today.md)
- [任务管理](tasks.md)
- [项目管理](projects.md)
- [客户管理](clients.md)
- [收件箱工作编排](inbox.md)
- [Actor 与任务分派](actors.md)
- [专注与工时](focus.md)
- [设置](settings.md)
- [命令面板与搜索](command-search.md)
- [数据、备份与恢复](data-management.md)
- [桌面平台与发布](desktop-platform.md)

## v0.2 本地编排

- [本地 Agent Runtime](local-agents.md)
- [预设自动化](automation.md)

任务看板属于任务模块的 v0.2 阶段，详见 [任务管理](tasks.md)。

## v0.3 规划增强

- [路线图](roadmap.md)
- [内容日历](content-calendar.md)

高级备份计划与导入映射属于数据管理模块的 v0.3 阶段，详见 [数据、备份与恢复](data-management.md)。

## v0.4 业务模块

- [收入、支出与发票](finance-invoices.md)
- [客户回访](client-followups.md)

## 版本待定

- [本地知识库](knowledge-base.md)
- [AI 助手](ai-assistant.md)

## 维护规则

- 当前状态只能根据实际代码、测试和运行证据更新。
- 新增字段必须通过递增迁移，不能回写已经发布的迁移。
- 跨模块写操作必须说明事务、幂等、冲突和失败恢复。
- person 只作本地责任记录；实际 Agent 执行在 v0.2，且必须经过本地 Adapter 和权限边界。
- 页面骨架、静态原型、样式或无行为按钮不能标记为模块完成。
