# 模块文档索引

本目录按功能边界拆分模块文档。每份文档同时记录当前实现事实与目标规划，不能仅凭文档中的目标功能判断代码已经完成。

导航：[文档中心](../README.md) · [整体功能架构](../functional-architecture.md) · [PRD v4.1](../opc-workspace-PRD.md)

## v0.1 核心闭环

- [今日工作台](today.md)（T-06A–H 日期分组、导航、按钮排序、同日/跨日期拖拽、空精确日期/未排期落点、行内任意日期安排、安全执行快捷操作及编辑/确认删除入口已交付）
- [任务管理](tasks.md)（事实层、D1/D2、Inbox 关系删除互锁与批量编排已交付；来源消费与 Agent 仍待开发）
- [项目管理](projects.md)（基础纵切与 Client 选择/筛选已交付；项目级产出/附件/事件仍待开发）
- [客户管理](clients.md)（基础资料 CRUD、基础详情与 Project 关联已交付；活动/附件/Actor 关联/回访/财务仍待开发）
- [收件箱工作编排](inbox.md)（T-11A1/A2/A3/B/C/F 的受理分诊、Reminder、Task 编排和 Today/Sidebar 运营计数已交付；其他来源投影和 Agent 待开发）
- [本地提醒](reminders.md)（T-11A3 一次性本地 Reminder、启动补偿和到期 Inbox 投影已交付；重复提醒与原生/远程通知待开发）
- [Actor 与任务分派](actors.md)（owner/person/system、Assignment 与 D2 产出责任已交付；agent 执行仍待开发）
- [专注与工时](focus.md)（Core A 事实迁移、B API/事务、C 前端接入已交付；历史/报告/原生桌面反馈 D 延后）
- [设置](settings.md)
- [命令面板与搜索](command-search.md)
- [数据、受控文件、备份与恢复](data-management.md)（Artifact store 已交付；产品化备份/恢复仍待开发）
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
- 受控文件能力必须同时说明数据库身份绑定 marker、进程锁、staging/objects/trash/quarantine、相对路径、大小/哈希、耐久同步、软删与聚合硬删；不能把目录预留写成备份已完成。
- person 只作本地责任记录；实际 Agent 执行在 v0.2，且必须经过本地 Adapter 和权限边界。
- 页面骨架、静态原型、样式或无行为按钮不能标记为模块完成。
