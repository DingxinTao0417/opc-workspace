# 模块文档索引

导航：[文档中心](../README.md) · [整体功能架构](../functional-architecture.md) · [PRD](../opc-workspace-PRD.md) · [变更记录](../changelog.md)

本目录按功能边界拆分模块文档。每份文档同时记录当前实现事实与目标规划，不能仅凭文档中的目标功能判断代码已经完成。

本索引只维护导航、各模块的状态总览和维护规则。每个切片交付了什么，见[变更记录](../changelog.md)和对应的模块文档。

## v0.1 核心闭环

- [今日工作台](today.md)（T-06A–H 日期分组、导航、按钮排序、同日/跨日期拖拽、空精确日期/未排期落点、行内任意日期安排、安全执行快捷操作、编辑/确认删除入口、截止风险快捷筛选、客户回访待办，以及右侧真实本地客户动态/临近路线图节点已交付）
- [任务管理](tasks.md)（事实层、D1/D2、筛选/保存视图/计划组拖拽、共享服务端搜索 Client 筛选，以及 Task 新建/编辑、Tasks 筛选/批量目标和 Inbox 拆分共用的 Project 选择器已交付；六状态看板、直属子任务汇总自动待验收、Inbox 编排、follow-up Artifact、Task 阻塞和 Task 临期来源已交付；按截止日日程视图、看板截止着色与列内手动顺序拖拽已交付；v0.2 Agent Run 区与条件式文本提交见本地 Agent 模块）
- [项目管理](projects.md)（基础纵切、任务浏览器、共享选择器、笔记/附件、Task Artifact 聚合及 nullable follow-up/实时 required 进度、产出区四种跟进状态与 Inbox 深链、活动时间线、来源投影、Client 系统活动及项目 Focus 已交付；财务与里程碑增强待开发）
- [客户管理](clients.md)（基础资料 CRUD、共享分页搜索选择器、Project 关联、人工活动时间线、Project 生命周期只读系统活动、跨客户最近动态读模型、受控附件、person 显式关联、客户回访详情管理、完成时原子下一次计划及 H2-I/J/K AI 主档、联系人关系与本地人员审批已交付；真实浏览器/窄屏/大数据量专项及外部来源/财务仍待验收或开发）
- [收件箱工作编排](inbox.md)（人工受理、关系/Reminder、来源投影、拆分/自动解决及客户回访来源上下文已交付；split 继承但可清除/改选可信来源 Project，写入独立完成条件并明确 person 本地责任，关系行打开共享 Task；成功 mutation 失效来源 Project，split 另失效 Task/Today/Project。required 仍是显式独立关系事实；H3-C3 Agent 失败诊断已接通，完整自治编排仍待）
- [本地提醒](reminders.md)（T-11A3 一次性与每日/每周/工作日/每月本地 Reminder、启动补偿、跨 DST/月末递推和到期 Inbox 投影已交付；法定节假日/自定义规则与原生/远程通知待开发）
- [Actor 与任务分派](actors.md)（人工 Assignment/D2 已交付；schema 071 agent_adapter_id、受控 Agent 身份与初始化门控，不以固定 ID 代替真实关联）
- [专注与工时](focus.md)（2026-10-29 起日常界面入口已移除，API 与 `/focus` 路由保留；Core A+B+C、D1 历史/报告、Task 与 Project 详情记录及 D2b 分析已交付；通用托盘不改变 Focus，专注状态/动作与原生通知延后）
- [设置](settings.md)（SQLite 设置 schema v2、草稿预览、受控头像、关闭到托盘、Actor、备份导入、无路径容量检查/7 天趋势、每日计划/自动包保留、脱敏诊断与关于入口）
- [命令面板与搜索](command-search.md)（Task/Project/Client/活动 Inbox 统一本地搜索、可刷新详情直达、本地最近使用、运行诊断直达、全局渲染错误恢复及命令面板/新建任务 OS 快捷键已交付）
- [数据、受控文件、备份与恢复](data-management.md)（一致性备份恢复、业务 JSON/ZIP、同 schema 零冲突追加、schema v43 容量历史及 schema v44 每日计划/启动补偿/只清理自动包的保留已交付；启动前备份选择已交付；旧版本升级导入、保留目标与跳过相同行、应用前演练、历史系统提醒与已结束指派的导入已交付；外部目录、覆盖/合并有分歧的行与 UUID 重映射仍待开发）
- [桌面平台与发布](desktop-platform.md)（内置 Sidecar generation-aware 有界恢复、父管道 EOF、前端世代清理、并发 shutdown、托盘最小闭环、持久化关闭偏好、运行诊断能力快照，以及 Windows x64 原生链接/Rust 测试/未签名 NSIS+MSI 打包已交付；新增智能体统一标签工作区、目录授权只读预览/Git 审查、Windows 手动 PTY、原生 WebView2 多标签浏览器与侧边聊天，原生交互验收单独记录；T-02 仍部分完成，安装后托盘交互、真实父崩溃/进程树、签名、干净系统和其他平台待验收）

任务看板已交付读取、筛选、分页、选择、详情入口及跨列受控生命周期交互；人工验收仍必须在任务详情完成，详见 [任务管理](tasks.md)。

v0.1 的 Project Artifact→Inbox→Task Go 金链使用 owner/person + manual owner reviewer；Web 表单另覆盖 person 的本地责任提示与提交载荷。全链不调用 AI/LLM，也不创建 Agent Run。自动化已覆盖事实链；真实浏览器/WebView、窄屏、焦点和大数据量仍待专项验收。

v0.1 的内置 Sidecar 只在真实 `Terminated` 后按 500 ms、2 s 最多重启两次，当前 generation 连续 Ready 30 秒后重置预算；外部模式、显式 shutdown 或事件流关闭但没有 `Terminated` 不自动重拉。数据库父目录固定 `.opc-sidecar-run.lock` 阻止第二个进程打开同库；Windows 上桌面以 kill-on-close Job Object 托管每一代 Sidecar 的进程树（ADR-031），桌面进程结束时卡死的 Sidecar 与 Agent 执行器一并终止，macOS/Linux 没有进程树治理。

## v0.2 本地编排

- [本地 Agent Runtime](local-agents.md)（ADR-027/029/030/033：Windows 内置 Runner、Run 界面、可恢复 manual-review text/单 file/有界多 file 提交、H5-C 受控文件输入、H5-D/H5-E1 会话计划与计划续办、H5-E18 计划提交 metadata-only 即时回执、H5-E2 计划外可处理 Run、H5-E3 计划外审批/当前验收入口、H5-E5 自动化失败入口、H5-E6 当前知识索引失败入口、H5-E7 当前生成失败入口、H5-E8 Provider 配置异常入口、H5-E4 人工点击的严格工作区工具入口、H5-E13 UI-only 固定标签导航、H5-E14 用户确认浏览器新标签、H5-E15 业务记录定位、H5-E16/H5-E17 已核对 owner Task 的 Agent Run/Task Submission 定位，以及 H4-AQ 脱敏网页地址双重人工交接；跨 Run 条件式延后启动，以及前置 Run 已验收的文件产出经二次人工确认后作为后续 Run 的输入；设置显式启停及真实 Adapter/Actor/Assignment 初始化门控；本地/在线 OpenAI 兼容模型。macOS/Linux、external、自动工具操作、不经逐项人工确认的自治多步和其余失败/恢复来源仍待，既知执行器缺口与真机验收不因本切片完成）
- [预设自动化](automation.md)（表始于 schema v33，五个可用但默认关闭的预设，H3-C3 已接 Agent 失败诊断、精确来源与历史导入/删除协调；设置/投递恢复/Run/重试/IANA/DST 已交付；H3-C1/C2 增加智能体读取、人工审批启停/有限配置与原快照重试，实际结果回执及 H4-E 规则/Run 精确定位、尝试链与原对话往返已接通；自由规则仍待）

## v0.3 规划增强

- [路线图](roadmap.md)（R2 数据/API、R3 基础界面与 R5 本地 Inbox 事件已完成，R4 已交付同季度安全排序、年度跨季度/跨年度移动与季度内精确日期拖拽/键盘输入；目标日期排序、Today 最近节点和指定详情 URL 联动已接通；H2-G/H2-S 已接 AI 安全读取、受审批新建/修改/归档/恢复及已归档里程碑的独立同意永久删除）
- [内容日历](content-calendar.md)（CC1–CC5-B、CC6-A、指定详情 URL 与 H2-H/H2-R/H2-T/H2-AD 已交付：月格/IANA 安全改期、任务协同、版本化审核/发布到期 Inbox，以及 AI 安全读取、受审批操作、已归档条目的独立同意永久删除和用户独立核实后的本地发布事实登记；准备 Task 关系只改关系、不改 Task 事实，不自动外发）

高级备份计划与导入映射属于数据管理模块的 v0.3 阶段，详见 [数据、备份与恢复](data-management.md)。

## v0.4 业务模块

- [收入、支出与发票](finance-invoices.md)（部分完成：schema 045–048 账本、统计、发票命令/付款原子入账、PDF 与到期来源已有实现；AI H3-E1 独立只读财务授权及报告/记录往返已接通，H3-E2 已接独立财务权限和收支创建/修改/作废审批；H3-E3 发票草稿/状态/实际回款审批已接入，H3-E4A 草稿删除审批已接入，H3-E4B PDF 生成/替换审批与精确人工下载已接入，H3-E5 CSV 范围审批及精确人工下载已接入；真实桌面与剩余增强待实现/验收）
- [客户回访](client-followups.md)

## 版本待定（独立轨道）

- [本地知识库](knowledge-base.md)（ADR-009、schema 059、TXT/Markdown、Actor、FTS5/中文检索、定位、重建、删除、清单与 ADR-010 AI6 已交付；ADR-026/schema 070 的 PDF 提取与页码定位已交付；授权引用与变化检测待后续）
- [AI 助手](ai-assistant.md)（Provider、Harness、记忆、AI5/AI6、回答级 citation、dataset v4 的 smoke/topic/full 配置身份评测与人工决定审计、steps/原始 token/unknown/UTC 趋势，以及 ADR-025 运行隔离、全局停止与恢复、原子任务确认、记忆决定和分段压缩已交付；费用、句子级覆盖率、自动路由与真机质量验证待后续）

## 维护规则

- 当前状态只能根据实际代码、测试和运行证据更新。
- 新增字段必须通过递增迁移，不能回写已经发布的迁移。
- 跨模块写操作必须说明事务、幂等、冲突和失败恢复。
- 受控文件能力必须同时说明数据库身份绑定 marker、进程锁、staging/objects/avatars/trash/quarantine、跨 Task Artifact/Client Attachment/Project Attachment/Workspace Avatar 的 ID 唯一、相对路径、大小/哈希、耐久同步、删除墓碑与恢复边界；不能把目录预留写成备份已完成。
- person 只作本地责任记录；实际 Agent 执行在 v0.2，且必须经过本地 Adapter 和权限边界。
- 页面骨架、静态原型、样式或无行为按钮不能标记为模块完成。
- 切片交付说明写入[变更记录](../changelog.md)，不写在本索引顶部；模块状态变化时只更新上面的状态总览。
