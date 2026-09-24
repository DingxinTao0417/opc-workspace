import { ShieldCheck } from "lucide-react";
import { useEffect, useId, useRef, useState } from "react";
import type {
  AiProvider,
  AiWorkspaceGrant,
  AiWorkspaceScope,
} from "../types/models";
import { Modal } from "./Modal";
import { AiKnowledgeAccess } from "./AiKnowledgeAccess";

const scopes: { id: AiWorkspaceScope; label: string; description: string }[] = [
  {
    id: "finance_exports",
    label: "财务导出（独立确认）",
    description:
      "依赖财务查询，仅已保存会话可提议 CSV。须明确币种、日期、收支类型和状态；人工核验完整匹配范围、数量、列与文件指纹，单独同意后再点击下载。包含本地备注及客户/项目/发票名称，不自动发送给模型或外部，不改账本。数据变化须重新提议；不保存历史 CSV 正文，审批结果不等于文件已下载。最多 10000 条、16 MiB。建议、筛选与校验元数据保存在当前会话审批历史。",
  },
  {
    id: "invoice_actions",
    label: "发票操作（独立确认）",
    description:
      "起草本地发票草稿创建、编辑和实际状态记录，依赖财务查询，不授予账本或工作事项操作。每张卡需另行核验：已发送/已查看须实际发生，已收款须核对全额回款和实际日期，确认后生成唯一账本收入并解决到期事项；标记逾期可能触发已启用的本地自动化任务。不会发送发票、执行银行付款或退款。完整本地备注与关联名称仅在人工审批卡，不作为工具结果或后续回执发给模型。可提议删除无账本关联的草稿，须再次勾选永久删除：发票及其受控 PDF 一并移除，无撤销，审批与审计历史保留，不影响副本或备份。可提议生成本地 PDF，须另行确认完整字段及替换旧文件的效果；不改变发票状态。结果卡提供本次文件的手动下载，旧文件被替换后拒绝下载，不自动发送文件或向模型提供字节、路径和文件元数据。CSV 导出需另选“财务导出”，且需要已保存会话。",
  },
  {
    id: "finance_actions",
    label: "财务操作（独立确认）",
    description:
      "仅起草本地收支的创建、修改和带原因作废，依赖财务查询，不自动授予工作事项或普通操作权限。每张卡需核对金额、币种、收支类型、日期、状态及关联，并单独勾选后执行。作废保留记录但排除统计，当前不能撤销，不代表退款。发票回款记录和已作废记录不可修改。不执行银行付款、发票状态、PDF 或导出。完整前后值（含本地备注和关联名称）保存于人工审批卡，不作为工具结果或后续回执发给模型；用户在聊天中提供的新备注仍会发送给所选模型。需要已保存会话。",
  },
  {
    id: "finance",
    label: "财务查询（只读）",
    description:
      "收支和发票的金额、币种、日期、状态、分类、编号、版本与关联客户/项目/发票 ID，以及明确日期范围内的同币种统计。查询结果会发送给当前模型；不读取客户名称和联系字段、备注、作废原因、PDF 或附件；分类和编号可能含你录入的敏感信息，不自动脱敏。不确认付款、不记账、不作废、不导出；另选操作建议也不会开放财务写入。金额为本地录入事实，不等于银行到账证据，不做跨币种折算。",
  },
  {
    id: "knowledge",
    label: "知识检索与引用",
    description:
      "模型可在下方选择的来源内主动搜索并读取完整片段，无需你逐段挑选。来源名称、搜索摘要、片段正文和位置将发送给当前模型；仅完整读取成功的片段可成为已验证引用。最多选择 20 个来源，每次运行累计最多 8 次成功知识查询、24 KiB 结果。不会导入文件、扫描磁盘、修改或删除知识库。",
  },
  {
    id: "knowledge_actions",
    label: "知识库管理（逐项确认）",
    description:
      "允许读取知识库来源与索引任务的元数据：来源名称、类型、状态、版本、大小、已生成分段数和最新索引尝试/错误码；这些元数据会发送给当前模型。不读取来源正文、提取文本、分段内容、哈希或文件字节，也不开放知识检索（那需要另选“知识检索与引用”）。仅已保存会话可用。可把你本轮明确提供的完整 UTF-8 文本起草存为新的 .txt/.md/.markdown 知识来源；确认卡只展示名称、字节数、SHA-256 和开头片段供你核对，不会扫描本地文件或读取路径。也可起草重建索引、重试失败或取消的索引任务、取消排队中/运行中的索引任务，以及永久删除来源；每一项都要你核对完整前后状态后确认，删除还需单独勾选并会移除来源、索引和受管副本，无撤销。取消索引不会删除已有资料；审批与索引历史保留在本地。",
  },
  {
    id: "actions",
    label: "操作建议（逐项确认）",
    description:
      "同时允许在当前保存会话中创建/修订多步计划；计划标题、步骤与历史版本保存在本地，可在后续重新授权的消息中发送给当前模型以接续工作。操作步骤只读取同会话真实审批回执，计划本身不执行、批准或自动后台继续。可起草任务、项目与收件箱的创建、修改及状态操作；任务可在展示完整影响并再次勾选后永久删除。已归档项目也可在展示任务、草稿发票、账本、笔记、附件、本地文件及来源协调影响并再次勾选后永久删除；删除项目会保留并解除可保留记录的项目关联，受保护引用仍会阻止。已归档内容条目可在展示准备任务关系和来源事项影响并再次勾选后永久删除；任务、项目和收件箱审计快照保留，活动来源事项会阻止。可创建任务标签、修改名称或颜色；永久删除标签会解除任务关联但不删除任务，同样需要独立勾选。可读取本地任务筛选预设（保存视图，最多 20 个，不含任务正文）并起草新建、改名/改筛选或删除建议；视图只是本地筛选预设，不修改任何任务，也不会自动应用，删除需再次勾选。支持已有任务首次分派、带原因改派与结束责任，验收人仅本人，保留历史并按既有子任务规则核对父任务待验收；支持分诊、关联、必需标记、解除关联，以及整批拆分并分派 1–20 个任务。每项提议需点击确认才执行，拆分整批成功或回滚。关系变更可能按既有策略自动解决事项。person 仅本地责任记录，分派给 Agent 不会启动执行。另可创建、改期、调整重复规则或取消本地提醒；取消停止后续系列，不删除历史，不发送外部通知。项目关联客户另需客户权限；不包含合同金额修改或项目审核。自动解决策略的事项可起草带原因的强制解决，但必须额外人工勾选，且不完成关联任务或绕过其验收。还可提议开始、暂停、继续、结束、取消和异常恢复专注（确认时生效）。开始需额外勾选，以新循环替代本地休息/旧轮次；计入中断间隔也需额外勾选。结束按有效时长入账但不完成任务，取消/中断不入账。不直接读取或控制本地休息轮次。建议及结果保存在当前已保存会话中（需要工作事项权限）另可建议内置自动化规则的启停和有限配置修改；启用或修改已启用规则须额外勾选持续的本地自动效果，停用不撤销已捕获事件的投递。可建议重试失败运行，另需人工核对原始配置/动作快照并勾选，按原规则最多三次尝试。快照只在本地确认卡完整展示，不发给模型；模型仅收到运行元数据与实际成功/失败回执。没有自由规则或脚本权限。",
  },
  {
    id: "agent_execution",
    label: "启动 / 重试 / 停止 Agent 执行（独立确认）",
    description:
      "还可提议恢复待登记产出，另行确认后只处理已保存结果，不重新调用模型或发送输入文件。恢复结果仍需人工检查和验收。" +
      "可提议重试失败、取消或中断的执行，沿用原任务、模型和文件快照，重新人工确认后调用 Provider，可能再次产生费用；受控文件需重新授权和确认。还可提议停止指定的排队中/运行中 Run，另行勾选确认后才发送持久取消请求；停止执行不取消任务，待登记产出不能取消丢弃。" +
      "允许智能体为一个已分派且可执行的任务起草一次 Agent Run；依赖工作事项、任务产出和操作建议。每张确认卡会冻结任务完整快照、Agent、Adapter、Provider 与运行限制，并要求你再次勾选后才真正启动。执行可能调用所示本地端点或远程 Provider，最长 10 分钟、结果最多 64 KiB；成功只表示运行结束，不会完成任务，产出仍须人工检查和验收。单独授权此能力不会读取文件或终端；只有另选受控文件能力并再次确认，才会读取卡片所列文件。不授予脚本、浏览器或长期执行权限。需要已保存会话。",
  },
  {
    id: "agent_files",
    label: "Agent 受控文件（再次确认）",
    description:
      "允许当前模型查看可用于某个任务的受控文件元数据，并在 Agent Run 建议中选择最多 4 个文件；依赖工作事项、任务产出、操作建议和 Agent 执行。模型只得到临时候选标识、名称、类型、大小和时间，不得到真实文件 ID、路径或正文。确认卡会冻结真实文件元数据；真正读取正文前还须另行勾选同意。远程执行 Provider 会接收所选文件字节并使其离开本机，本地 Provider 则留在本机。也可建议一个由服务端命名的 UTF-8 文件产出；成功不等于任务完成，产出仍需人工验收。需要已保存会话。",
  },
  {
    id: "agent_project_files",
    label: "Agent 跨任务已验收文件（独立确认）",
    description:
      "仅允许为当前任务选择同项目其他已完成任务、当前已验收批次中的受控文件。依赖工作事项、执行产出、操作建议、Agent 执行及原受控文件权限；不扩大原文件范围。模型只读取有界来源元数据和临时候选标识，不自动读正文。执行前须核对来源任务、版本、批次及 SHA-256，再独立同意跨任务交接与文件正文发送；远程 Provider 将收到所选文件，内容离开本机。混合输入共用四文件、单文件 64 KiB、总计 128 KiB。已验收只描述来源，不代表新任务已验收；不会自动调度或完成任务。须手动选择，仅本消息有效，需要已保存会话。",
  },
  {
    id: "workspace_ui",
    label: "工作区导航（不读取内容）",
    description:
      "允许智能体在本次消息中打开右侧 Agent 工作区的固定标签：环境概览、子智能体、文件、变更审查、终端、浏览器或任务产出。若同时选择工作事项、客户资料或执行产出读取，智能体还可请求你确认后在本地定位一条已获授权的记录、Agent 执行或任务提交批次；后两者的所属任务均由服务端核对，不会接受任意路由、路径、URL 或文件。不会读取任何标签内容，不会输入或执行终端命令，也不会控制浏览器、Git 或本机文件。无需保存会话，不发送工作台数据给模型。",
  },
  {
    id: "workspace_browser",
    label: "浏览器标签操作（逐次确认）",
    description:
      "允许智能体为你已明确提出的网址请求打开 HTTP/HTTPS 新标签，或请求对当前内嵌 Chromium 标签后退、前进、重新加载、停止加载。右侧会显示目标地址或当前标签，由你逐次确认后才操作；网页模式不支持标签操作。不会读取、截图、提取或发送网页内容、Cookie、登录状态、浏览器历史、本机路径或操作结果给模型，也不会点击网页、填写表单、下载文件或执行终端/Git 操作。无需保存会话。",
  },
  {
    id: "work",
    label: "工作事项",
    description:
      "任务、项目、收件箱、本地提醒、路线图、内容日历的名称、状态和说明；另含未删除项目笔记的标题、完整分段正文和版本（手写内容可能敏感，查询结果会发送给当前模型）；同时选择操作建议时可起草新建、修改和软删除笔记，逐项确认，删除另需勾选且无撤销，历史及审批预览保留；不读取项目附件；另含真实专注会话/终态历史、绑定任务、时长与恢复状态，以及按明确日期/时区查询的已完成工时、逐日/项目/标签/时段分布和连续天数；报告按当前项目与标签归属统计，不读取本地休息及轮次；含提醒触发时间、时区、重复规则、系列和到期收件箱 ID；含收件箱日期、已读/稍后、解决策略、关联任务进度及当前/历史关系；仅在已授予范围内核对收件箱来源的身份、版本、状态和本地详情地址，产出来源另需执行产出（outputs）、客户回访另需客户资料（clients）、发票另需财务查询（finance）；未授予对应范围时只提示所需权限，不披露受保护来源的 ID、版本或地址，来源查询不返回正文或原始载荷，也不自动打开或处理来源；可查询可分派负责人和标签的 ID、名称、类型与版本，以及任务当前/历史分派的角色、责任人身份、状态、版本与起止时间，不含人员备注、联系方式、适配器配置、来源原始载荷、关系操作者或解除原因；另含自动化预设、配置、权限、版本及运行元数据；不含运行的业务快照、原始来源事件或结果正文。",
  },
  {
    id: "clients",
    label: "客户资料",
    description:
      "客户名称、状态、备注及当前联系人关系的人员名称/类型/状态/版本；不含客户联系字段、人员备注、metadata、附件或财务信息。另含本地活动正文和回访计划、负责人身份、时间/时区、结果、下一步及跳过/取消原因；手写正文可能含敏感资料，授权后所查询的内容会发送给当前模型。同时选择工作事项和操作建议时，可起草客户主档创建/修改，把现有 active person 关联为联系人或带原因解除并保留历史，以及回访和活动操作，均需逐项确认；不能创建/删除人员或实际联系客户。inactive 客户可提议永久删除：发票、收支或任何回访会阻止；确认卡展示 Project 解绑、本地活动/联系人历史/附件与受控文件删除数量，并要求再次勾选。删除无应用内撤销，不删除 person，也不影响备份或外部副本。完成回访还须勾选核实实际结果与时间，活动软删除须单独勾选并保留历史与附件。系统活动只读。",
  },
  {
    id: "outputs",
    label: "执行产出",
    description:
      "任务提交批次、状态、摘要、验收意见、批次产出清单及 Agent Run 的文本结果；含历史和已删除产出的元数据，不含已删除正文或人员私有资料。摘要和验收意见可能包含手写敏感信息。文件仅元数据，不读取文件正文（需要工作事项权限）。也可按项目汇总任务产出、关联跟进事项及必需任务进度；不读取项目附件正文。同时选择操作建议时，可起草文本/链接/结构化产出提交和接受/返工建议；需你核实完整内容、产出归属及验收证据并单独勾选后确认，不自动提交或通过验收",
  },
  {
    id: "output_files",
    label: "产出文件正文（独立只读授权）",
    description:
      "依赖工作事项和执行产出。允许模型按需读取任意任务当前或历史提交中未删除的受管 UTF-8 文本文件产出，每个文件最多 64 KiB；不限于当前页面或交接的单个文件。每次校验实际大小、SHA-256、归属和任务版本，按最多 4000 字符分页读取。所读取的完整分页正文会发送给当前模型；远程供应商会使这些内容离开本机，发送后无法撤回。文件中可能包含敏感信息，不自动脱敏。不支持 PDF、图片、压缩包、项目附件或任意磁盘路径，不运行文件内指令。只读不代表已检查所有分页、外部证据或已经通过验收；不启动 Agent，也不授予操作或验收权。须你手动勾选，仅本条消息有效，不因交接推荐自动选中。",
  },
];

const scopeSummaries: Record<AiWorkspaceScope, string> = {
  work: "查询任务、项目、收件箱、提醒、路线图、内容、专注与自动化元数据，含项目笔记正文；可能包含敏感信息，不读取附件。",
  outputs:
    "读取任务提交、验收意见及 Agent 文本结果，可能含敏感正文；文件仅元数据。依赖工作事项，提交或验收另需操作建议及确认。",
  clients:
    "读取客户备注、活动正文、联系人关系及回访计划，可能含敏感信息；不读取联系字段或财务，不实际联系客户。写入或删除另需确认。",
  finance:
    "只读收支、发票及同币种统计，含金额与可能敏感的分类、编号；不读取备注或附件。另选操作建议也不会开放财务写入。",
  workspace_ui:
    "打开固定工作区标签；定位具体记录需对应读取权限和确认。不读取标签内容，不执行终端、Git 或文件操作。",
  workspace_browser:
    "仅桌面端：请求打开明确网址或操作当前浏览器标签，逐次确认；不读取网页、登录状态或历史，不点击或填表。",
  actions:
    "在已保存会话中制定计划、提出工作事项的创建、修改、分派或删除建议；依赖工作事项，每项写入须人工确认，删除另行核对。不会自动执行 Agent。",
  agent_execution:
    "提议启动、重试、停止或恢复 Agent Run；依赖工作事项、产出与操作建议，需保存会话并独立确认。可能再次调用模型并产生费用，成功不等于完成任务。",
  knowledge_actions:
    "读取知识来源和索引元数据，提议保存你提供的文本、重建索引或删除来源；需保存会话并逐项确认。不读取正文，删除不可撤销。",
  finance_actions:
    "提议创建、修改或作废本地收支；依赖财务查询、保存会话及独立确认，不执行银行付款或退款。作废当前不可撤销。",
  invoice_actions:
    "提议本地发票草稿、状态、PDF 或永久删除操作；依赖财务查询、保存会话及独立确认，不发送发票或执行银行付款。回款记录须核实真实到账。",
  knowledge:
    "在你选择的最多 20 个来源内搜索并读取完整片段，名称、摘要、正文及位置会发送给当前模型；不导入或修改知识库。",
  output_files:
    "手动选择后模型可按需读取任意任务的受管文本产出正文，不限当前文件，远程模型会使正文离开本机且无法撤回。依赖工作事项及产出，无逐文件确认。",
  agent_files:
    "为 Agent 选择最多 4 个受控文件；本轮模型只见元数据，执行前须再次同意正文发送。远程执行会使文件离开本机，需保存会话和 Agent 执行等依赖。",
  agent_project_files:
    "手动选择同项目其他已完成任务的已验收文件供 Agent 使用；继承受控文件依赖，须独立核对并同意跨任务正文发送，不自动调度。",
  finance_exports:
    "提议含备注及关联名称的本地 CSV；依赖财务查询、保存会话及独立核验，手动下载，不自动发给模型或外部。",
};

const scopeSections: {
  title: string;
  description: string;
  items: AiWorkspaceScope[];
}[] = [
  {
    title: "常用查询与定位",
    description: "按需读取勾选的工作台事实；定位和浏览器请求仍须你逐次确认。",
    items: [
      "work",
      "outputs",
      "clients",
      "finance",
      "workspace_ui",
      "workspace_browser",
    ],
  },
  {
    title: "操作建议与执行",
    description: "这里只允许提出建议；写入、执行或恢复仍走各自的人工确认。",
    items: [
      "actions",
      "agent_execution",
      "knowledge_actions",
      "finance_actions",
      "invoice_actions",
    ],
  },
  {
    title: "敏感内容与文件",
    description: "可能读取完整片段、文件正文或生成财务导出，请逐项核对范围。",
    items: [
      "knowledge",
      "output_files",
      "agent_files",
      "agent_project_files",
      "finance_exports",
    ],
  },
];

const quickSelections: {
  label: string;
  description: string;
  scopes: AiWorkspaceScope[];
  persistentOnly?: boolean;
}[] = [
  {
    label: "只查工作事项",
    description: "任务、项目与收件箱等只读查询",
    scopes: ["work"],
  },
  {
    label: "查询并定位",
    description: "工作事项查询及受确认的本地导航",
    scopes: ["work", "workspace_ui"],
  },
  {
    label: "查看执行产出",
    description: "任务产出元数据，不含文件正文",
    scopes: ["work", "outputs", "workspace_ui"],
  },
  {
    label: "准备操作建议",
    description: "工作事项提议，执行前逐项人工确认",
    scopes: ["work", "actions", "workspace_ui"],
    persistentOnly: true,
  },
];

const persistentOnlyScopes = new Set<AiWorkspaceScope>([
  "actions",
  "agent_execution",
  "agent_files",
  "agent_project_files",
  "finance_actions",
  "invoice_actions",
  "finance_exports",
  "knowledge_actions",
]);

const manualRecommendationScopes = new Set<AiWorkspaceScope>([
  "output_files",
  "agent_project_files",
]);

function normalizeWorkspaceScopes(values: AiWorkspaceScope[]) {
  const next = new Set(values);
  if (next.has("agent_project_files")) next.add("agent_files");
  if (next.has("output_files")) {
    next.add("work");
    next.add("outputs");
  }
  if (next.has("outputs") || next.has("actions")) next.add("work");
  if (next.has("agent_execution")) {
    next.add("work");
    next.add("outputs");
    next.add("actions");
  }
  if (next.has("agent_files")) {
    next.add("work");
    next.add("outputs");
    next.add("actions");
    next.add("agent_execution");
  }
  if (
    next.has("finance_exports") ||
    next.has("finance_actions") ||
    next.has("invoice_actions")
  )
    next.add("finance");
  return scopes.filter((item) => next.has(item.id)).map((item) => item.id);
}

export function AiWorkspaceGrantSummary({
  value,
}: {
  value?: AiWorkspaceGrant;
}) {
  if (!value) return null;
  const labels = value.scopes.map(
    (scope) => scopes.find((item) => item.id === scope)?.label ?? scope,
  );
  return (
    <div className="ai-workspace-grant-summary" aria-label="本条工作台授权范围">
      <ShieldCheck size={13} aria-hidden="true" />
      <span>
        下一条消息已选 {labels.length} 项：{labels.join("、")}
        。发送后不继承，操作仍需逐项确认。
      </span>
    </div>
  );
}

export function AiWorkspaceAccess({
  provider,
  value,
  onChange,
  disabled,
  persist = true,
  openRequest = 0,
  recommendedScopes = [],
}: {
  provider: AiProvider;
  value?: AiWorkspaceGrant;
  onChange: (value: AiWorkspaceGrant | undefined) => void;
  disabled: boolean;
  persist?: boolean;
  openRequest?: number;
  recommendedScopes?: AiWorkspaceScope[];
}) {
  const scopeIdPrefix = useId();
  const [open, setOpen] = useState(false);
  const [selected, setSelected] = useState<AiWorkspaceScope[]>([]);
  const [manualRecommendations, setManualRecommendations] = useState<
    AiWorkspaceScope[]
  >([]);
  const [knowledgeSources, setKnowledgeSources] = useState<
    NonNullable<AiWorkspaceGrant["knowledge_sources"]>
  >([]);
  const handledOpenRequest = useRef(openRequest);
  useEffect(() => {
    if (handledOpenRequest.current === openRequest) return;
    handledOpenRequest.current = openRequest;
    if (disabled) return;
    setManualRecommendations(
      recommendedScopes.filter((scope) =>
        manualRecommendationScopes.has(scope),
      ),
    );
    setSelected(
      normalizeWorkspaceScopes([
        ...(value?.scopes ?? []),
        ...recommendedScopes.filter(
          (scope) => !manualRecommendationScopes.has(scope),
        ),
      ]).filter((scope) => persist || !persistentOnlyScopes.has(scope)),
    );
    setKnowledgeSources(value?.knowledge_sources ?? []);
    setOpen(true);
  }, [disabled, openRequest, persist, recommendedScopes, value]);
  const unselectedManualRecommendations = manualRecommendations.filter(
    (scope) => !selected.includes(scope),
  );
  function toggle(scope: AiWorkspaceScope, checked: boolean) {
    setSelected((current) => {
      const next = new Set(current);
      if (checked) {
        next.add(scope);
        if (scope === "outputs" || scope === "actions") next.add("work");
        if (scope === "output_files") {
          next.add("work");
          next.add("outputs");
        }
        if (scope === "agent_execution") {
          next.add("work");
          next.add("outputs");
          next.add("actions");
        }
        if (scope === "agent_files" || scope === "agent_project_files") {
          next.add("work");
          next.add("outputs");
          next.add("actions");
          next.add("agent_execution");
          if (scope === "agent_project_files") next.add("agent_files");
        }
        if (
          scope === "finance_exports" ||
          scope === "finance_actions" ||
          scope === "invoice_actions"
        )
          next.add("finance");
      } else {
        next.delete(scope);
        if (
          [
            "work",
            "outputs",
            "actions",
            "agent_execution",
            "agent_files",
          ].includes(scope)
        )
          next.delete("agent_project_files");
        if (scope === "finance") {
          next.delete("finance_actions");
          next.delete("invoice_actions");
          next.delete("finance_exports");
        }
        if (scope === "work") {
          next.delete("outputs");
          next.delete("output_files");
          next.delete("actions");
          next.delete("agent_execution");
          next.delete("agent_files");
        }
        if (scope === "outputs" || scope === "actions") {
          next.delete("agent_execution");
          next.delete("agent_files");
        }
        if (scope === "outputs") next.delete("output_files");
        if (scope === "agent_execution") {
          next.delete("agent_files");
        }
      }
      return scopes.filter((item) => next.has(item.id)).map((item) => item.id);
    });
  }
  return (
    <>
      <button
        aria-haspopup="dialog"
        className="ai-context-toggle"
        data-confirmed={!!value}
        disabled={disabled}
        onClick={() => {
          setManualRecommendations([]);
          setSelected(
            normalizeWorkspaceScopes(value?.scopes ?? []).filter(
              (scope) => persist || !persistentOnlyScopes.has(scope),
            ),
          );
          setKnowledgeSources(value?.knowledge_sources ?? []);
          setOpen(true);
        }}
        type="button"
      >
        <ShieldCheck size={14} />
        {value?.scopes.some(
          (scope) =>
            scope === "actions" ||
            scope === "agent_execution" ||
            scope === "agent_files" ||
            scope === "finance_actions" ||
            scope === "invoice_actions" ||
            scope === "finance_exports" ||
            scope === "knowledge_actions",
        )
          ? "本次可提议操作"
          : value
            ? value.scopes.length === 1 && value.scopes.includes("workspace_ui")
              ? "本次可开工作区"
              : value.scopes.length === 1 &&
                  value.scopes.includes("workspace_browser")
                ? "本次可请求浏览器操作"
                : value.scopes.includes("output_files")
                  ? "本次可读产出文件"
                  : "本次可查工作台"
            : "工作台权限"}
      </button>
      <Modal
        open={open}
        title="授权本次工作台能力"
        onClose={() => setOpen(false)}
        footer={
          <>
            <button
              className="button button-secondary"
              type="button"
              onClick={() => {
                onChange(undefined);
                setOpen(false);
              }}
            >
              不授权
            </button>
            <button
              className="button button-primary"
              type="button"
              disabled={
                disabled ||
                selected.length === 0 ||
                (!persist &&
                  selected.some((scope) => persistentOnlyScopes.has(scope))) ||
                (selected.includes("knowledge") &&
                  knowledgeSources.length === 0)
              }
              onClick={() => {
                onChange({
                  provider_version: provider.version,
                  scopes: selected,
                  ...(selected.includes("knowledge")
                    ? { knowledge_sources: knowledgeSources }
                    : {}),
                });
                setOpen(false);
              }}
            >
              仅授权下一条消息
            </button>
          </>
        }
      >
        <div className="ai-workspace-consent">
          <p>
            {provider.name} · {provider.model}
          </p>
          <p>
            {provider.kind === "local"
              ? "查询结果将发送到所选本机模型端点。"
              : "查询结果会发送给以上远程供应商，离开本机。"}
            {selected.length === 1 && selected[0] === "workspace_ui"
              ? "该项只允许打开固定工作区标签，不会查询或发送任何工作台内容。"
              : selected.length === 1 && selected[0] === "workspace_browser"
                ? "该项只允许提出一个待你确认的新网页或当前标签操作请求；网页内容、浏览器状态、操作结果和本机路径不会发送给模型。"
                : selected.includes("workspace_ui") &&
                    (selected.includes("work") ||
                      selected.includes("clients") ||
                      selected.includes("outputs"))
                  ? "智能体可查询你勾选的数据范围，并可请求你确认后在本地打开一条已获授权的记录或 Agent 执行；执行过程的所属任务由服务端核对，记录内容不会因定位请求再次发送给模型。"
                  : "模型可按需查询勾选范围内的记录，不限于当前页面。"}
          </p>
          {unselectedManualRecommendations.length > 0 ? (
            <p className="ai-workspace-manual-note" role="note">
              智能体建议的
              {unselectedManualRecommendations
                .map((scope) => scopes.find((item) => item.id === scope)?.label)
                .join("、")}
              尚未勾选。文件权限不会随建议自动授予；若确实需要，请在下方亲自勾选并核对相关依赖。
              {!persist &&
              unselectedManualRecommendations.includes("agent_project_files")
                ? "跨任务文件权限仅在已保存会话可用。"
                : ""}
            </p>
          ) : null}
          <div
            className="ai-workspace-presets"
            role="group"
            aria-label="常用权限组合"
          >
            <p>
              快速选择会替换下方待选范围，不会立即授权。文件正文、财务和 Agent
              执行不会自动选中。
            </p>
            <div className="ai-workspace-preset-buttons">
              {quickSelections.map((preset) => (
                <button
                  key={preset.label}
                  className="button button-secondary"
                  type="button"
                  disabled={disabled || (preset.persistentOnly && !persist)}
                  title={preset.description}
                  onClick={() => {
                    setSelected(normalizeWorkspaceScopes(preset.scopes));
                    setKnowledgeSources([]);
                  }}
                >
                  {preset.label}
                </button>
              ))}
            </div>
          </div>
          {scopeSections.map((section) => (
            <fieldset key={section.title} disabled={disabled}>
              <legend>{section.title}</legend>
              <p className="ai-workspace-section-note">{section.description}</p>
              {section.items.map((scopeId) => {
                const scope = scopes.find((item) => item.id === scopeId);
                if (!scope) return null;
                return (
                  <div
                    key={scope.id}
                    className={
                      unselectedManualRecommendations.includes(scope.id)
                        ? "ai-workspace-manual-recommendation"
                        : undefined
                    }
                  >
                    <label>
                      <input
                        type="checkbox"
                        aria-label={scope.label}
                        aria-describedby={`${scopeIdPrefix}-${scope.id}`}
                        disabled={
                          persistentOnlyScopes.has(scope.id) && !persist
                        }
                        checked={selected.includes(scope.id)}
                        onChange={(event) =>
                          toggle(scope.id, event.target.checked)
                        }
                      />
                      <span>
                        <strong>{scope.label}</strong>
                        <small id={`${scopeIdPrefix}-${scope.id}`}>
                          {scopeSummaries[scope.id]}
                        </small>
                      </span>
                    </label>
                    <details
                      className="ai-workspace-scope-details"
                      open={
                        selected.includes(scope.id) ||
                        unselectedManualRecommendations.includes(scope.id)
                      }
                    >
                      <summary>{scope.label}：完整范围与风险</summary>
                      <small>{scope.description}</small>
                    </details>
                  </div>
                );
              })}
            </fieldset>
          ))}
          {open && selected.includes("knowledge") ? (
            <AiKnowledgeAccess
              value={knowledgeSources}
              onChange={setKnowledgeSources}
              disabled={disabled}
            />
          ) : null}
          {!persist ? (
            <p role="note">
              当前会话不保存记录，操作建议与多步计划暂不可用；Agent
              执行暂不可用；Agent
              受控文件暂不可用；工作台查询和产出文件只读仍可单独授权。
            </p>
          ) : null}
          <p>
            仅限下一条消息的本次运行，不保存为长期权限；切换模型或会话需重新授权。可用“停止生成”终止后续查询；已经发送的数据无法撤回。
          </p>
          <p>
            产出文件正文须另行手选，授权后模型可按需读取，无逐文件弹窗；不授予任意文件、终端、浏览器或长期网络权限。Agent
            执行只有在对应确认卡再次人工同意后，才会调用卡片所示的本地端点或远程
            Provider；选择了受控输入文件时，还必须单独同意发送文件正文。授权本身不会启动。操作建议另存明确的变更预览供你确认，最近审批状态与结果标识会作为本会话后续上下文发送给所选模型，不继承操作权限；其余工具原始结果不单独保存，模型可能引用到回答或会话记忆，按当前会话的保存设置处理。
          </p>
        </div>
      </Modal>
    </>
  );
}
