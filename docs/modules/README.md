# 模块文档索引

本目录按功能边界拆分模块文档。每份文档同时记录当前实现事实与目标规划，不能仅凭文档中的目标功能判断代码已经完成。

导航：[文档中心](../README.md) · [整体功能架构](../functional-architecture.md) · [PRD v9.9](../opc-workspace-PRD.md)

## v0.1 核心闭环

- [今日工作台](today.md)（T-06A–H 日期分组、导航、按钮排序、同日/跨日期拖拽、空精确日期/未排期落点、行内任意日期安排、安全执行快捷操作及编辑/确认删除入口已交付）
- [任务管理](tasks.md)（事实层、D1/D2、筛选/保存视图/计划组拖拽、六状态看板及受控跨列生命周期、Inbox 编排、follow-up Artifact、Task 阻塞和 Task 临期来源已交付；Agent 待开发）
- [项目管理](projects.md)（基础纵切、任务树/平铺及项目内搜索/状态筛选/服务端分页、Client、笔记、附件、Task Artifact 聚合、活动时间线、显式 follow-up 产出和 Project 完成节点→Inbox 已交付；高级分析待开发）
- [客户管理](clients.md)（基础资料 CRUD、Project 关联、人工活动时间线、受控附件和 person 显式关联已交付；外部来源/回访/财务仍待开发）
- [收件箱工作编排](inbox.md)（T-11A1/A2/A3/B/C/F 和 T-11E follow-up Artifact/Task 阻塞/Task 临期/Project 完成、备份操作、数据库启动/迁移、Sidecar 启动、运行期数据库操作失败及可配置低空间监测已交付；Agent 待开发）
- [本地提醒](reminders.md)（T-11A3 一次性本地 Reminder、启动补偿和到期 Inbox 投影已交付；重复提醒与原生/远程通知待开发）
- [Actor 与任务分派](actors.md)（owner/person/system、Assignment 与 D2 产出责任已交付；agent 执行仍待开发）
- [专注与工时](focus.md)（Core A 事实迁移、B API/事务、C 前端接入已交付；历史/报告/原生桌面反馈 D 延后）
- [设置](settings.md)（SQLite 偏好、草稿预览、受控工作区头像、Actor、数据、脱敏运行诊断/诊断包与关于入口）
- [命令面板与搜索](command-search.md)（Task/Project/Client/活动 Inbox 统一本地搜索、可刷新详情直达、本地最近使用、运行诊断直达与全局渲染错误恢复已交付；OS 全局快捷键待开发）
- [数据、受控文件、备份与恢复](data-management.md)（受控文件 store、一致性备份恢复、启动后恢复结果诊断、全局启动故障恢复页 v1、故障 Inbox、桌面安全重启，以及业务 JSON/含文件 ZIP 的空工作区安全导入导出已交付；数据库打开前备份选择/实时恢复进度、非空目标/跨 schema 冲突合并仍待开发）
- [桌面平台与发布](desktop-platform.md)

## v0.2 本地编排

- [本地 Agent Runtime](local-agents.md)
- [预设自动化](automation.md)

任务看板已交付读取、筛选、分页、选择、详情入口及跨列受控生命周期交互；人工验收仍必须在任务详情完成，详见 [任务管理](tasks.md)。

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
- 受控文件能力必须同时说明数据库身份绑定 marker、进程锁、staging/objects/avatars/trash/quarantine、跨 Task Artifact/Client Attachment/Project Attachment/Workspace Avatar 的 ID 唯一、相对路径、大小/哈希、耐久同步、删除墓碑与恢复边界；不能把目录预留写成备份已完成。
- person 只作本地责任记录；实际 Agent 执行在 v0.2，且必须经过本地 Adapter 和权限边界。
- 页面骨架、静态原型、样式或无行为按钮不能标记为模块完成。
