# 模块文档索引

本目录按功能边界拆分模块文档。每份文档同时记录当前实现事实与目标规划，不能仅凭文档中的目标功能判断代码已经完成。

导航：[文档中心](../README.md) · [整体功能架构](../functional-architecture.md) · [PRD v9.52](../opc-workspace-PRD.md)

## v0.1 核心闭环

- [今日工作台](today.md)（T-06A–H 日期分组、导航、按钮排序、同日/跨日期拖拽、空精确日期/未排期落点、行内任意日期安排、安全执行快捷操作、编辑/确认删除入口、截止风险快捷筛选及客户回访待办已交付）
- [任务管理](tasks.md)（事实层、D1/D2、筛选/保存视图/计划组拖拽、共享服务端搜索 Client 筛选，以及 Task 新建/编辑、Tasks 筛选/批量目标和 Inbox 拆分共用的 Project 选择器已交付；六状态看板、直属子任务汇总自动待验收、Inbox 编排、follow-up Artifact、Task 阻塞和 Task 临期来源已交付；Agent 待开发）
- [项目管理](projects.md)（基础纵切、任务浏览器、共享选择器、笔记/附件、Task Artifact 聚合及 nullable follow-up/实时 required 进度、产出区四种跟进状态与 Inbox 深链、活动时间线、来源投影、Client 系统活动及项目 Focus 已交付；财务与里程碑增强待开发）
- [客户管理](clients.md)（基础资料 CRUD、共享分页搜索选择器、Project 关联、人工活动时间线、Project 生命周期只读系统活动、受控附件、person 显式关联、客户回访详情管理及完成时原子下一次计划已交付；真实浏览器/窄屏/大数据量专项及外部来源/财务仍待验收或开发）
- [收件箱工作编排](inbox.md)（人工受理、关系/Reminder、来源投影、拆分/自动解决及客户回访来源上下文已交付；split 继承但可清除/改选可信来源 Project，写入独立完成条件并明确 person 本地责任，关系行打开共享 Task；成功 mutation 失效来源 Project，split 另失效 Task/Today/Project。required 仍是显式独立关系事实；Agent 待开发）
- [本地提醒](reminders.md)（T-11A3 一次性与每日/每周本地 Reminder、启动补偿、跨 DST 递推和到期 Inbox 投影已交付；每月/自定义规则与原生/远程通知待开发）
- [Actor 与任务分派](actors.md)（owner/person/system、Assignment 与 D2 产出责任已交付；agent 执行仍待开发）
- [专注与工时](focus.md)（Core A+B+C、D1 历史/报告、Task 与 Project 详情记录及 D2b 分析已交付；原生桌面反馈延后）
- [设置](settings.md)（SQLite 偏好、草稿预览、受控工作区头像、Actor、数据、脱敏运行诊断/诊断包与关于入口）
- [命令面板与搜索](command-search.md)（Task/Project/Client/活动 Inbox 统一本地搜索、可刷新详情直达、本地最近使用、运行诊断直达、全局渲染错误恢复及命令面板/新建任务 OS 快捷键已交付）
- [数据、受控文件、备份与恢复](data-management.md)（受控文件 store、一致性备份恢复、启动后恢复结果诊断、数据库父目录运行锁、数据库打开前白名单恢复进度、全局启动故障恢复页 v1、故障 Inbox、桌面安全重启，以及业务 JSON/含文件 ZIP 的空工作区安全导入导出已交付；启动前备份选择、非空目标/跨 schema 冲突合并仍待开发）
- [桌面平台与发布](desktop-platform.md)（内置 Sidecar generation-aware 有界自动重启、父管道 EOF 退出、前端连接/查询世代清理和并发安全 shutdown 已交付；T-02 仍部分完成，真实父崩溃/进程树、三平台与安装包待验收）

## v0.2 本地编排

- [本地 Agent Runtime](local-agents.md)（T-19 v0.2-A 已交付 schema v34、代码所有 Adapter 登记/诊断、设置模块和隔离未验证即禁用；Runner/Run/agent Actor 尚未实现）
- [预设自动化](automation.md)（schema v33、三个可用预设、两个依赖不可用预设、设置预览/启停、Run/重试、IANA/DST 与离线折叠已交付；Agent/发票/自由规则待开发）

任务看板已交付读取、筛选、分页、选择、详情入口及跨列受控生命周期交互；人工验收仍必须在任务详情完成，详见 [任务管理](tasks.md)。

v0.1 的 Project Artifact→Inbox→Task Go 金链使用 owner/person + manual owner reviewer；Web 表单另覆盖 person 的本地责任提示与提交载荷。全链不调用 AI/LLM，也不创建 Agent Run。自动化已覆盖事实链；真实浏览器/WebView、窄屏、焦点和大数据量仍待专项验收。

v0.1 的内置 Sidecar 只在真实 `Terminated` 后按 500 ms、2 s 最多重启两次，当前 generation 连续 Ready 30 秒后重置预算；外部模式、显式 shutdown 或事件流关闭但没有 `Terminated` 不自动重拉。数据库父目录固定 `.opc-sidecar-run.lock` 只阻止 hard-hung orphan 再次打开同库，当前没有 Job Object、进程组、孙进程治理或自动回收。

## v0.3 规划增强

- [路线图](roadmap.md)（R2 数据/API 与 R3 首个界面纵切已交付：季度浏览、新建、Project 跳转、归档恢复；编辑、拖拽与本地事件待开发）
- [内容日历](content-calendar.md)（CC1 数据契约、业务导入导出与 CC2 本地 API 已交付；React 界面待开发）

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
