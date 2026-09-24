import { validProjectNoteProposal } from "./aiProjectNoteActions";
import { validFileSourceProof } from "../lib/agentProjectFiles";
import type { AgentProjectFileSource } from "../types/models";
import { validAgentRunReworkContext } from "../lib/agentRunRework";
import { validAgentRunRestartSource } from "../lib/agentRunRestart";
import type { AgentRunRestartSource } from "../types/models";
import type { AgentRunReworkContext } from "../types/models";
import { ApiError, agentRunStartGateFromRecord, apiRequest } from "./client";
import {
  isTaskOutputAction,
  validTaskOutputProposal,
  type AiTaskOutputPreview,
} from "./aiTaskOutputActions";
import {
  isAssignmentAction,
  validAssignmentProposal,
} from "./aiAssignmentActions";
import { validFinanceExport } from "./aiFinanceExport";
import { clientRecordHref } from "../lib/clientRecordLocation";
import { automationLocationHref } from "../lib/automationLocation";
import { projectNoteHref } from "../lib/projectNoteLocation";
import { agentRunHref } from "../lib/aiWorkspaceLinks";
import { taskSubmissionHref } from "../lib/taskSubmissionLocation";
import { validAgentRunDeliveryState } from "../lib/agentRunDelivery";
import type {
  AgentRunOutputDeliveryStatus,
  AgentRunStatus,
} from "../types/models";
import { validFinancialProposal } from "./aiFinancialActions";
import {
  validInvoiceProposal,
  validInvoicePdfResult,
  type AiInvoicePdfResult,
} from "./aiInvoiceActions";
import {
  validAutomationRetryProposal,
  type AiAutomationRetryPreview,
  type AiAutomationRetryResult,
} from "./aiAutomationRetry";

export const aiActionLabels = {
  "agent_delegate.spawn": "委派子智能体",
  "agent_delegate.followup": "续交办子智能体",
  "task.batch_create": "批量创建项目任务",
  "task.batch_update": "批量更新任务",
  "task.move": "调整任务顺序",
  "roadmap_milestone.move": "调整路线图顺序",
  "task_view.create": "保存任务视图",
  "task_view.update": "修改任务视图",
  "task_view.delete": "删除任务视图",
  "knowledge_source.create": "存入知识库",
  "knowledge_source.reindex": "重建知识库索引",
  "knowledge_source.delete": "永久删除知识库来源",
  "knowledge_index_job.retry": "重试知识库索引",
  "knowledge_index_job.cancel": "取消知识库索引",
  "project_note.create": "新建项目笔记",
  "project_note.update": "修改项目笔记",
  "project_note.delete": "删除项目笔记",
  "task.submit_output": "提交任务产出",
  "task.review": "验收任务产出",
  "finance.export_csv": "导出财务 CSV",
  "invoice.generate_pdf": "生成发票 PDF",
  "invoice.delete": "永久删除发票草稿",
  "invoice.create": "新建发票草稿",
  "invoice.update": "修改发票草稿",
  "invoice.mark_sent": "记录发票已发送",
  "invoice.mark_viewed": "记录发票已查看",
  "invoice.mark_paid": "记录发票已收款",
  "invoice.mark_overdue": "标记发票逾期",
  "financial_entry.create": "新建收支记录",
  "financial_entry.update": "修改收支记录",
  "financial_entry.void": "作废收支记录",
  "automation.retry": "重试自动化运行",
  "automation.enable": "启用自动化规则",
  "automation.disable": "停用自动化规则",
  "automation.update": "修改自动化配置",
  "client_activity.create": "记录客户活动",
  "client_activity.update": "修改客户活动",
  "client_activity.delete": "删除客户活动",
  "client_followup.create": "安排客户回访",
  "client_followup.update": "修改客户回访计划",
  "client_followup.cancel": "取消客户回访计划",
  "client_followup.complete": "记录已完成的客户回访",
  "client_followup.skip": "跳过客户回访",
  "client_followup.reschedule": "重新安排客户回访",
  "focus.start": "开始专注",
  "focus.pause": "暂停专注",
  "focus.resume": "继续专注",
  "focus.stop": "结束专注并记录工时",
  "focus.cancel": "取消专注",
  "focus.recover": "处理专注中断",
  "reminder.create": "创建本地提醒",
  "reminder.update": "修改本地提醒",
  "reminder.cancel": "取消本地提醒",
  "inbox.split": "拆分并分派任务",
  "inbox.link_task": "关联已有任务",
  "inbox.set_required": "调整必需任务",
  "inbox.unlink_task": "解除任务关联",
  "inbox.create": "创建收件箱事项",
  "inbox.update": "修改收件箱事项",
  "inbox.read": "标为已读",
  "inbox.read_all": "全部标为已读（快照）",
  "inbox.snooze": "稍后处理",
  "inbox.unsnooze": "恢复待处理",
  "inbox.resolve": "解决收件箱事项",
  "inbox.force_resolve": "强制解决收件箱事项",
  "inbox.dismiss": "忽略收件箱事项",
  "inbox.reopen": "重新打开收件箱事项",
  "project.create": "创建项目",
  "project.update": "修改项目",
  "project.start": "开始项目",
  "project.pause": "暂停项目",
  "project.resume": "继续项目",
  "project.complete": "完成项目",
  "project.reopen": "重新打开项目",
  "project.archive": "归档项目",
  "project.restore": "恢复项目",
  "project.delete": "永久删除项目",
  "roadmap_milestone.create": "创建路线图里程碑",
  "roadmap_milestone.update": "修改路线图里程碑",
  "roadmap_milestone.archive": "归档路线图里程碑",
  "roadmap_milestone.restore": "恢复路线图里程碑",
  "roadmap_milestone.delete": "永久删除路线图里程碑",
  "content_item.create": "创建内容日历条目",
  "content_item.update": "修改内容日历条目",
  "content_item.schedule": "安排内容发布时间",
  "content_item.unschedule": "清除内容排期",
  "content_item.review": "送交内容审核",
  "content_item.cancel": "取消内容条目",
  "content_item.reopen": "重新打开内容条目",
  "content_item.archive": "归档内容条目",
  "content_item.restore": "恢复内容条目",
  "content_item.delete": "永久删除内容条目",
  "content_item.publish": "确认内容已发布",
  "content_item.link_task": "关联内容准备任务",
  "content_item.set_task_required": "调整内容准备任务",
  "content_item.unlink_task": "解除内容准备任务",
  "client.create": "创建客户",
  "client.update": "修改客户资料",
  "client.delete": "永久删除客户",
  "client_contact.link": "关联客户联系人",
  "client_contact.unlink": "解除客户联系人",
  "person.create": "创建本地人员",
  "person.update": "修改本地人员",
  "task.assign": "分派任务责任",
  "task.reassign": "改派任务责任",
  "task.unassign": "结束任务责任",
  "task.create": "创建任务",
  "task.update": "修改任务",
  "task.start": "开始任务",
  "task.block": "标记阻塞",
  "task.unblock": "解除阻塞",
  "task.complete": "完成任务",
  "task.cancel": "取消任务",
  "task.reopen": "重新打开任务",
  "task.delete": "永久删除任务",
  "tag.create": "创建任务标签",
  "tag.update": "修改任务标签",
  "tag.delete": "永久删除任务标签",
  "agent_run.start": "启动 Agent 执行",
  "agent_run.cancel": "停止 Agent 执行",
  "agent_run.cancel_many": "批量停止 Agent 执行",
  "agent_run.retry": "重试 Agent 执行",
  "agent_run.recover_output": "恢复 Agent 产出登记",
} as const;
export const aiActionFields: Record<string, string> = {
  active_runs: "活动执行数",
  cancel_requested_runs: "请求停止数",
  chunks: "已生成分段",
  documents: "已提取文档",
  job_status: "索引任务状态",
  job_attempt: "索引尝试次数",
  version: "版本",
  source_type: "来源类型",
  note_state: "笔记状态",
  project_status: "项目状态",
  project_version: "项目版本",
  project_deleted: "永久删除项目",
  project_tasks_detached: "解除项目任务关联",
  project_draft_invoices_detached: "解除草稿发票关联",
  project_financial_entries_detached: "解除收支记录关联",
  project_notes_deleted: "删除项目笔记",
  project_attachments_deleted: "删除附件记录",
  project_attachment_files_deleted: "删除本地附件文件",
  project_inbox_sources_coordinated: "协调项目完成事项",
  client_version: "客户版本",
  client_deleted: "永久删除客户",
  client_projects_detached: "解除客户项目关联",
  client_activities_deleted: "删除客户活动记录",
  client_contact_history_deleted: "删除联系人关系历史",
  client_attachments_deleted: "删除客户附件记录",
  client_attachment_files_deleted: "删除客户本地附件文件",
  content_item_version: "内容条目版本",
  content_item_deleted: "永久删除内容条目",
  content_item_task_links_deleted: "解除准备任务关系",
  content_item_inbox_sources_marked: "标记来源事项已删除",
  roadmap_milestone_version: "路线图里程碑版本",
  roadmap_milestone_deleted: "永久删除路线图里程碑",
  roadmap_milestone_project_links_deleted: "解除关联项目关系",
  roadmap_milestone_inbox_sources_marked: "标记来源事项已删除",
  submission_status: "提交批次状态",
  submission_origin: "批次来源",
  subtask_total: "当前直属子任务数",
  subtask_completed: "当前已完成子任务",
  subtask_cancelled: "当前已取消子任务",
  current_submission_id: "当前验收批次 ID",
  role: "责任角色",
  previous_assignment_id: "原责任记录 ID",
  actor_id: "责任人 ID",
  actor_name: "责任人",
  actor_type: "责任人类型",
  actor_version: "责任人版本",
  actor_status: "人员状态",
  display_name: "人员名称",
  person_type: "人员类型",
  pdf_generated: "生成本地 PDF",
  pdf_replaced: "替换已存 PDF",
  invoice_deleted: "永久删除草稿",
  pdf_removed: "一并移除已存 PDF",
  pdf_asset_id: "PDF 文件标识",
  pdf_file_name: "PDF 文件名",
  pdf_size_bytes: "PDF 大小（字节）",
  pdf_sha256: "PDF SHA-256",
  pdf_generated_from_version: "PDF 来源发票版本",
  pdf_generated_at: "PDF 生成时间",
  date_from: "开始日期（含）",
  date_to: "结束日期（含）",
  entry_type: "收支范围",
  export_status: "记录状态",
  row_count: "全部匹配记录数",
  size_bytes: "文件字节数",
  sha256: "内容 SHA-256",
  csv_columns: "导出列",
  export_sort: "排序",
  invoice_number: "发票编号",
  issue_date: "开票日期",
  paid_date: "实际收款日期",
  type: "收支类型",
  amount_minor: "金额",
  currency: "币种",
  occurred_on: "发生日期",
  category: "收支分类",
  project_name: "关联项目",
  preset_key: "内置预设",
  available: "当前可用",
  unavailable_reason: "不可用原因",
  trigger_type: "触发方式",
  trigger_label: "触发条件",
  action_type: "自动创建的对象",
  action_label: "自动执行的动作",
  permission_summary: "持续本地权限",
  rule_enabled: "启用状态",
  local_time: "当地触发时间",
  body: "正文",
  occurred_at: "实际发生时间（UTC）",
  activity_state: "活动可见性",
  client_name: "客户",
  client_status: "客户状态",
  contact_name: "联系人",
  email: "邮箱",
  phone: "电话",
  assigned_actor_id: "回访负责人 ID",
  assigned_actor_name: "回访负责人",
  assigned_actor_type: "负责人类型",
  assigned_actor_status: "负责人状态",
  scheduled_at: "计划时间（UTC）",
  timezone: "计划时区",
  channel: "沟通渠道（本地记录）",
  purpose: "回访目的",
  notes: "备注",
  result: "实际回访结果",
  completed_at: "实际完成时间（UTC）",
  next_step: "下一步",
  recovery_action: "中断处理方式",
  last_heartbeat_at: "最后在线心跳（UTC）",
  accumulated_seconds: "此前已结算时长（秒）",
  planned_seconds: "专注计划时长（秒）",
  trigger_at: "触发时间（UTC）",
  recurrence_type: "重复规则",
  recurrence_interval: "重复间隔",
  recurrence_timezone: "重复时区",
  recurrence_anchor_day: "日期锚点",
  occurrence_number: "系列次数",
  created_task_count: "新建任务数",
  task_id: "关联任务 ID",
  task_title: "关联任务",
  task_version: "任务版本",
  task_status: "任务状态",
  relation_state: "关联状态",
  is_required: "必需任务",
  resolution_policy: "解决策略",
  resolution_mode: "解决方式",
  active_task_count: "关联任务数",
  required_task_count: "必需任务数",
  required_done_count: "已完成必需任务数",
  required_remaining_count: "未完成必需任务数",
  required_blocked_count: "阻塞的必需任务数",
  required_waiting_review_count: "待验收的必需任务数",
  required_cancelled_count: "已取消的必需任务数",
  summary: "事项说明",
  due_at: "截止时间",
  snoozed_until: "稍后至",
  read_state: "阅读状态",
  snapshot_at: "快照截止时间",
  through_created_at: "执行截止时间",
  snapshot_unread_count: "快照内未读事项",
  marked_count: "实际标为已读",
  name: "名称",
  start_date: "开始日期",
  color: "颜色",
  client_id: "关联客户 ID",
  link_state: "联系人关系",
  incomplete_task_count: "未完成任务数",
  year: "年份",
  quarter: "季度",
  target_date: "目标日期",
  project_ids: "关联项目 ID",
  platform: "发布平台",
  scheduled_timezone: "排期时区",
  external_link: "外部链接文本",
  published_at: "实际发布时间",
  title: "标题",
  description: "说明",
  priority: "优先级",
  kind: "类型",
  planned_date: "计划日期",
  due_date: "截止时间",
  estimated_minutes: "预计分钟",
  project_id: "所属项目 ID",
  parent_task_title: "父任务",
  task_deleted: "永久删除任务",
  child_tasks_moved_to_top_level: "移到顶层的直属子任务",
  submissions_deleted: "删除提交批次",
  artifacts_deleted: "删除任务产出",
  file_artifacts_deleted: "删除本地文件产出",
  assignments_deleted: "删除责任历史",
  agent_runs_deleted: "删除 Agent 执行历史",
  focus_sessions_detached: "解除专注历史关联",
  inbox_relations_detached: "解除收件箱历史关联",
  inbox_sources_coordinated: "协调收件箱来源事项",
  tag_links_deleted: "删除标签关系",
  tag_deleted: "永久删除标签",
  detached_tasks: "解除关联的任务",
  tag_version: "标签版本",
  count: "数量",
  completion_criteria: "完成条件",
  tag_names: "标签",
  review_policy: "验收方式",
  parent_rollup_gates_ready: "父任务自动提请验收的责任条件",
  will_request_parent_review: "本次自动提请父任务验收",
  status: "状态",
  reason: "原因",
};
export interface AiActionProposal {
  id: string;
  generation_id: string;
  fingerprint: string;
  action: {
    action: keyof typeof aiActionLabels;
    financial_entry_id?: string;
    invoice_id?: string;
    task_id?: string;
    tag_id?: string;
    project_id?: string;
    roadmap_milestone_id?: string;
    content_item_id?: string;
    client_id?: string;
    client_actor_link_id?: string;
    actor_id?: string;
    inbox_item_id?: string;
    reminder_id?: string;
    focus_session_id?: string;
    client_followup_id?: string;
    client_activity_id?: string;
    project_note_id?: string;
    automation_rule_id?: string;
    automation_run_id?: string;
    agent_run_id?: string;
    knowledge_source_id?: string;
    knowledge_index_job_id?: string;
    task_saved_view_id?: string;
    expected_version?: number;
    changes: Record<string, unknown>;
  };
  preview: {
    label: string;
    before: Record<string, string | number | boolean | null | string[]>;
    after: Record<string, string | number | boolean | null | string[]>;
    tasks?: AiInboxSplitDraft[];
    next_followup?: Record<string, string | number | boolean | null>;
    automation_retry?: AiAutomationRetryPreview;
    task_output?: AiTaskOutputPreview;
    inbox_read_all?: {
      through_created_at: string;
      candidate_count: number;
      selection_fingerprint: string;
    };
    agent_run_start?: AiAgentRunStartPreview;
    agent_run_cancel?: AiAgentRunCancelPreview;
    agent_run_cancel_many?: AiAgentRunCancelManyPreview;
    agent_run_recovery?: AiAgentRunCancelPreview & {
      status: "running";
      output_kind: "text" | "file" | "files";
      output_bytes: number;
      output_sha256: string;
    };
    agent_delegation?: AiAgentDelegationPreview;
    agent_followup?: AiAgentFollowupPreview;
    agent_run_retry?: {
      run_id: string;
      status: "failed" | "cancelled" | "interrupted";
      attempt: number;
    };
    knowledge?: AiKnowledgeProposalPreview;
    task_view?: AiTaskViewPreview;
    task_batch?: AiTaskBatchPreview;
    task_batch_create?: AiTaskBatchCreatePreview;
    task_order?: AiTaskOrderPreview;
    roadmap_order?: AiRoadmapOrderPreview;
  };
  status: "pending" | "confirmed" | "rejected" | "expired" | "unavailable";
  can_confirm: boolean;
  result_id: string | null;
  result_version: number | null;
  route: string;
  created_at: string;
  decided_at: string | null;
  automation_run_result?: AiAutomationRetryResult;
  invoice_pdf_result?: AiInvoicePdfResult;
  inbox_read_all_result?: {
    through_created_at: string;
    marked_count: number;
  };
  agent_run_result?: AiAgentRunResult;
  agent_run_cancel_many_result?: AiAgentRunCancelManyResult;
  agent_delegation_result?: AiAgentDelegationResult;
  agent_followup_result?: AiAgentFollowupResult;
  task_batch_create_result?: AiTaskBatchCreateResult;
  created_task_ids?: string[];
}
export interface AiAgentDelegationPreview {
  task_name: string;
  message: string;
  scopes: string[];
  knowledge_sources?: Array<{
    source_id: string;
    expected_source_version: number;
  }>;
  parent_session_id: string;
  parent_generation_id: string;
  child_session_id: string;
  provider_id: string;
  provider_version: number;
  provider_config_version: number;
  provider_name: string;
  model: string;
  leaves_device: boolean;
  depth: 1;
  max_depth: 1;
  sibling_index: number;
  max_children: 4;
}
export interface AiAgentDelegationResult {
  session_id: string;
  generation_id: string;
  status: "queued" | "streaming" | "completed" | "failed" | "cancelled";
  error_code: string | null;
  pending_approvals: number;
}
export interface AiAgentFollowupPreview {
  target: {
    parent_session_id: string;
    child_session_id: string;
    spawn_proposal_id: string;
    previous_generation_id: string;
    child_version: number;
    provider_id: string;
    provider_version: number;
    provider_config_version: number;
  };
  task_name: string;
  message: string;
  scopes: string[];
  knowledge_sources?: AiAgentDelegationPreview["knowledge_sources"];
  provider_name: string;
  model: string;
  leaves_device: boolean;
}
export interface AiAgentFollowupResult extends Omit<
  AiAgentDelegationResult,
  "status"
> {
  status: AiAgentDelegationResult["status"] | "unavailable";
}
export interface AiTaskBatchCreateItemPreview {
  key: string;
  parent_key?: string;
  title: string;
  description: string;
  kind: "work" | "review" | "followup" | "reminder";
  priority: "P0" | "P1" | "P2" | "P3";
  planned_date: string | null;
  due_date: string | null;
  estimated_minutes: number | null;
  completion_criteria: string;
  review_policy: "none" | "manual";
  tag_ids: string[];
  tag_names: string[];
  assignments?: Array<{
    role: "assignee" | "reviewer";
    actor_id: string;
    actor_name: string;
    actor_type: "owner" | "person" | "agent";
    actor_status: "active";
    actor_version: number;
  }>;
}
export interface AiTaskBatchCreatePreview {
  project_id: string;
  project_name: string;
  project_version: number;
  count: number;
  items: AiTaskBatchCreateItemPreview[];
}
export interface AiTaskBatchCreateResult {
  project_id: string;
  count: number;
  items: Array<{ key: string; id: string; title: string; route: string }>;
}
export interface AiKnowledgeJobPreview {
  id: string;
  operation: "import" | "reindex";
  status: "queued" | "running" | "succeeded" | "failed" | "cancelled";
  attempt: number;
  error_code?: string;
}
export interface AiKnowledgeProposalPreview {
  source_id?: string;
  knowledge_index_job_id?: string;
  name: string;
  source_type: "text" | "markdown" | "pdf";
  status:
    | "pending"
    | "indexing"
    | "ready"
    | "stale"
    | "missing"
    | "failed"
    | "deleted";
  version: number;
  next_status?: string;
  next_version?: number;
  chunk_count: number;
  document_count: number;
  document_version?: number;
  new_job_operation?: "import" | "reindex";
  job?: AiKnowledgeJobPreview;
  proposed_content_bytes?: number;
  proposed_content_sha256?: string;
  proposed_excerpt?: string;
  proposed_excerpt_truncated?: boolean;
  content_included: false;
  note?: string;
}
export interface AiTaskViewPreview {
  id?: string;
  name: string;
  definition: {
    q?: string;
    status?: string;
    priority?: string;
    kind?: string;
    project_id?: string;
    client_id?: string;
    tag_ids?: string[];
    planned_date?: string;
    planned_from?: string;
    planned_to?: string;
    due_from?: string;
    due_to?: string;
    sort?: string;
  };
  version?: number;
  next_version?: number;
  project_name?: string;
  client_name?: string;
  tag_names?: string[];
  deleted?: boolean;
}
export interface AiTaskBatchItemPreview {
  task_id: string;
  title: string;
  version: number;
  field:
    | "priority"
    | "due_date"
    | "project"
    | "planned_date"
    | "tags"
    | "status"
    | "assignee"
    | "reviewer";
  before: string;
  after: string;
  assignment_id?: string;
  before_actor?: AiTaskBatchActorPreview;
  after_actor?: AiTaskBatchActorPreview;
}
export interface AiTaskBatchActorPreview {
  role: "assignee" | "reviewer";
  actor_id: string;
  actor_name: string;
  actor_type: "owner" | "person" | "agent";
  actor_status: "active" | "inactive";
  actor_version: number;
}
export interface AiTaskBatchPreview {
  action:
    | "set_priority"
    | "set_due_date"
    | "set_project"
    | "set_planned_date"
    | "add_tags"
    | "remove_tags"
    | "start"
    | "block"
    | "unblock"
    | "complete"
    | "cancel"
    | "reopen"
    | "set_assignee"
    | "clear_assignee"
    | "set_reviewer"
    | "clear_reviewer";
  count: number;
  reason?: string;
  items: AiTaskBatchItemPreview[];
}
export interface AiTaskOrderPreview {
  task_id: string;
  title: string;
  anchor_task_id: string;
  anchor_title: string;
  placement: "before" | "after";
  planned_date: string | null;
  group_count: number;
  active_count: number;
  before_position: number;
  after_position: number;
  group_fingerprint: string;
}
export interface AiRoadmapOrderPreview {
  milestone_id: string;
  title: string;
  anchor_id: string;
  anchor_title: string;
  placement: "before" | "after";
  year: number;
  quarter: number;
  group_count: number;
  before_position: number;
  after_position: number;
  group_fingerprint: string;
}
export interface AiAgentRunCancelPreview {
  run_id: string;
  task_id: string;
  task_title?: string;
  task_version: number;
  status: "queued" | "running";
  attempt: number;
  provider_id: string;
  model: string;
}
export interface AiAgentRunCancelManyPreview {
  count: number;
  items: Array<AiAgentRunCancelPreview & { task_title: string }>;
}
export interface AiAgentRunCancelManyResult {
  count: number;
  items: Array<{
    run_id: string;
    task_id: string;
    status: "running" | "cancelled";
  }>;
}
export interface AiAgentRunStartPreview {
  restart?: AgentRunRestartSource;
  task: {
    id: string;
    title: string;
    description: string;
    completion_criteria: string;
    status: "todo" | "in_progress";
    kind: "work" | "review" | "followup" | "reminder";
    review_policy: "none" | "manual";
    priority: "P0" | "P1" | "P2" | "P3";
    project_id: string | null;
    parent_task_id: string | null;
    due_date: string | null;
    planned_date: string | null;
    estimated_minutes: number | null;
    actual_minutes: number;
    manual_order: number | null;
    version: number;
    created_at: string;
    updated_at: string;
  };
  assignment: {
    id: string;
    role: "assignee";
    assigned_at: string;
    actor_id: string;
  };
  agent: {
    id: string;
    display_name: string;
    type: "agent";
    status: "active";
    version: number;
  };
  adapter: {
    id: string;
    display_name: string;
    kind: "builtin";
    protocol_version: "opc-agent-pipe-v1";
    status: "enabled";
    health_status: "healthy";
    isolation_status: "verified";
    execution_ready: true;
    version: number;
  };
  provider: {
    id: string;
    name: string;
    kind: "local" | "remote";
    protocol: "openai_chat" | "anthropic_messages";
    model: string;
    status: "ready";
    health_status: "healthy";
    version: number;
    config_version: number;
    // v1 proposals created before the file boundary do not contain this
    // derived disclosure. v2 always includes it.
    leaves_device?: boolean;
  };
  attempt: number;
  execution_contract_version: 1 | 2 | 3 | 4 | 5 | 6;
  rework_context?: AgentRunReworkContext;
  runtime_limits: {
    timeout_seconds: 600;
    max_result_bytes: 65536;
    max_output_tokens?: 8192;
  };
  input_files?: AiAgentRunInputFilePreview[];
  input_file_total_bytes?: number;
  input_files_leave_device?: boolean;
  output_contract?: AiAgentRunOutputContract;
  file_limits?: {
    max_files: 4;
    max_file_bytes: 65536;
    max_total_bytes: 131072;
    max_result_bytes: 65536;
  };
  /** ADR-030: stable predecessor identity only; live state is re-checked. */
  start_gate?: AiAgentRunStartGatePreview;
  success_does_not_complete_task: true;
  result_requires_manual_review: true;
}
export interface AiAgentRunStartGatePreview {
  predecessor_run_id: string;
  predecessor_task_id: string;
  predecessor_task_title: string;
  predecessor_attempt: number;
  require: "submitted" | "accepted";
  within_hours: number;
}
export interface AiAgentRunInputFilePreview {
  source_kind: "task_artifact" | "project_attachment" | "project_task_artifact";
  source_task?: AgentProjectFileSource;
  id: string;
  name: string;
  mime: string;
  size_bytes: number;
  sha256: string;
}
export type AiAgentRunOutputContract =
  | { type: "text" }
  | { type: "file"; name: string; mime: string }
  | { type: "files"; files: Array<{ name: string; mime: string }> };
export interface AiAgentRunResult {
  restart_of_run_id?: string;
  id: string;
  task_id: string;
  status:
    "queued" | "running" | "succeeded" | "failed" | "cancelled" | "interrupted";
  attempt: number;
  provider_id: string;
  model: string;
  output_delivery_status: AgentRunOutputDeliveryStatus;
  output_delivery_error_code: string | null;
  submission_id: string | null;
  submission_status?:
    "pending_review" | "accepted" | "changes_requested" | "withdrawn";
  start_gate?: {
    predecessor_run_id: string;
    require: "submitted" | "accepted";
    expires_at: string;
    status: "waiting" | "released" | "closed";
    close_reason?: string;
  };
  artifact_id: string | null;
}
export interface AiTaskOption {
  id: string;
  label: string;
  type: "owner" | "person" | "agent" | "project" | "tag";
  version: number;
}
export interface AiInboxSplitDraft {
  key: string;
  parent_key: string;
  fields: Record<string, string | number | boolean | null>;
  assignee: AiTaskOption;
  reviewer: AiTaskOption | null;
  project: AiTaskOption | null;
  tags: AiTaskOption[];
  is_required: boolean;
}
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const canonicalUuid =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const object = (v: unknown): v is Record<string, unknown> =>
  typeof v === "object" && v !== null && !Array.isArray(v);
const exactKeys = (value: Record<string, unknown>, allowed: string[]) =>
  Object.keys(value).length === allowed.length &&
  Object.keys(value).every((key) => allowed.includes(key));
const positiveIntegerValue = (value: unknown): value is number =>
  typeof value === "number" && Number.isSafeInteger(value) && value > 0;
const nonemptyString = (value: unknown) =>
  typeof value === "string" && value.trim().length > 0;
const utcTimestamp = (value: unknown) =>
  typeof value === "string" &&
  /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/.test(value) &&
  Number.isFinite(Date.parse(value));
const sha256 = /^[a-f0-9]{64}$/;
const agentTextFileExtensions: Record<string, string[]> = {
  "text/plain": [".txt", ".log"],
  "text/markdown": [".md", ".markdown"],
  "text/csv": [".csv"],
  "text/html": [".html", ".htm"],
  "text/css": [".css"],
  "text/javascript": [".js", ".mjs", ".cjs", ".jsx"],
  "application/javascript": [".js", ".mjs", ".cjs", ".jsx"],
  "text/typescript": [".ts", ".tsx"],
  "application/json": [".json"],
  "application/xml": [".xml"],
  "text/xml": [".xml"],
  "application/yaml": [".yaml", ".yml"],
  "text/yaml": [".yaml", ".yml"],
  "text/x-go": [".go"],
  "text/x-python": [".py"],
  "text/x-shellscript": [".sh"],
  "text/x-sql": [".sql"],
};
function validAgentTextFileNameAndMime(name: unknown, mime: unknown) {
  if (
    typeof name !== "string" ||
    typeof mime !== "string" ||
    name.length === 0 ||
    name !== name.trim() ||
    name === "." ||
    name === ".." ||
    /[\\/<>:"|?*\u0000-\u001f\u007f]/.test(name) ||
    new TextEncoder().encode(name).byteLength > 255
  )
    return false;
  const extensions = agentTextFileExtensions[mime];
  const lowerName = name.toLowerCase();
  return !!extensions?.some((extension) => lowerName.endsWith(extension));
}
function validAgentRunInputFile(
  value: unknown,
): value is AiAgentRunInputFilePreview {
  return (
    object(value) &&
    exactKeys(value, [
      "id",
      "source_kind",
      "name",
      "mime",
      "size_bytes",
      "sha256",
      ...(value.source_kind === "project_task_artifact" ? ["source_task"] : []),
    ]) &&
    typeof value.id === "string" &&
    canonicalUuid.test(value.id) &&
    ["task_artifact", "project_attachment", "project_task_artifact"].includes(
      String(value.source_kind),
    ) &&
    validFileSourceProof(value, true) &&
    validAgentTextFileNameAndMime(value.name, value.mime) &&
    positiveIntegerValue(value.size_bytes) &&
    Number(value.size_bytes) <= 65_536 &&
    typeof value.sha256 === "string" &&
    sha256.test(value.sha256)
  );
}
function validAgentRunOutputContract(
  value: unknown,
): value is AiAgentRunOutputContract {
  if (!object(value) || typeof value.type !== "string") return false;
  if (value.type === "text") return exactKeys(value, ["type"]);
  if (value.type === "file") {
    return (
      exactKeys(value, ["type", "name", "mime"]) &&
      validAgentTextFileNameAndMime(value.name, value.mime)
    );
  }
  if (value.type !== "files" || !exactKeys(value, ["type", "files"])) {
    return false;
  }
  return (
    Array.isArray(value.files) &&
    value.files.length >= 2 &&
    value.files.length <= 4 &&
    value.files.every(
      (file) =>
        object(file) &&
        exactKeys(file, ["name", "mime"]) &&
        validAgentTextFileNameAndMime(file.name, file.mime),
    ) &&
    new Set(value.files.map((file) => String(file.name).toLowerCase())).size ===
      value.files.length
  );
}
function invalid(): never {
  throw new ApiError("操作确认信息不完整，请重新读取", {
    code: "INVALID_RESPONSE",
  });
}
function fields(
  v: unknown,
): v is Record<string, string | number | boolean | null | string[]> {
  return (
    object(v) &&
    Object.entries(v).every(
      ([key, value]) =>
        Object.hasOwn(aiActionFields, key) &&
        (value === null ||
          ([
            "is_required",
            "available",
            "rule_enabled",
            "invoice_deleted",
            "pdf_removed",
            "pdf_generated",
            "pdf_replaced",
            "task_deleted",
            "tag_deleted",
            "project_deleted",
            "client_deleted",
            "content_item_deleted",
            "roadmap_milestone_deleted",
            "parent_rollup_gates_ready",
            "will_request_parent_review",
          ].includes(key) &&
            typeof value === "boolean") ||
          typeof value === "string" ||
          (key === "project_ids" &&
            Array.isArray(value) &&
            value.length <= 100 &&
            new Set(value).size === value.length &&
            value.every((id) => typeof id === "string" && uuid.test(id))) ||
          (key === "tag_names" &&
            Array.isArray(value) &&
            value.length <= 20 &&
            new Set(value).size === value.length &&
            value.every(
              (name) =>
                typeof name === "string" &&
                name.trim().length > 0 &&
                Array.from(name).length <= 50,
            )) ||
          (typeof value === "number" && Number.isFinite(value))),
    )
  );
}

function validTaskReviewPolicyProposal(
  value: Record<string, unknown>,
): boolean {
  const action = value.action as Record<string, unknown>;
  if (action.action !== "task.update") return true;
  const changes = action.changes as Record<string, unknown>;
  const preview = value.preview as Record<string, unknown>;
  const before = preview.before as Record<string, unknown>;
  const after = preview.after as Record<string, unknown>;
  const rollupFields = [
    "subtask_total",
    "subtask_completed",
    "subtask_cancelled",
    "parent_rollup_gates_ready",
    "will_request_parent_review",
  ];
  if (!Object.hasOwn(changes, "review_policy"))
    return ["review_policy", ...rollupFields].every(
      (field) => !Object.hasOwn(before, field) && !Object.hasOwn(after, field),
    );
  const policy = (entry: unknown) => entry === "none" || entry === "manual";
  const allowedChanges = [
    "title",
    "description",
    "priority",
    "kind",
    "planned_date",
    "due_date",
    "estimated_minutes",
    "project_id",
    "parent_task_id",
    "completion_criteria",
    "tag_ids",
    "review_policy",
  ];
  const changedFields = Object.keys(changes).map((field) =>
    field === "tag_ids"
      ? "tag_names"
      : field === "parent_task_id"
        ? "parent_task_title"
        : field,
  );
  if (
    !exactKeys(action, ["action", "task_id", "expected_version", "changes"]) ||
    Object.keys(changes).some((field) => !allowedChanges.includes(field)) ||
    ["title", "description", "kind", "priority", "completion_criteria"].some(
      (field) => Object.hasOwn(changes, field) && changes[field] === null,
    ) ||
    !exactKeys(preview, ["label", "before", "after"]) ||
    !exactKeys(before, [...changedFields, "status"]) ||
    !exactKeys(after, [...changedFields, "status", ...rollupFields]) ||
    !policy(changes.review_policy) ||
    !policy(before.review_policy) ||
    after.review_policy !== changes.review_policy ||
    ![
      "todo",
      "in_progress",
      "blocked",
      "waiting_review",
      "done",
      "cancelled",
    ].includes(String(before.status)) ||
    (before.review_policy !== changes.review_policy &&
      before.status !== "todo") ||
    ["subtask_total", "subtask_completed", "subtask_cancelled"].some(
      (field) =>
        typeof after[field] !== "number" ||
        !Number.isSafeInteger(after[field]) ||
        Number(after[field]) < 0,
    ) ||
    typeof after.parent_rollup_gates_ready !== "boolean" ||
    typeof after.will_request_parent_review !== "boolean"
  )
    return false;
  const remaining =
    Number(after.subtask_total) - Number(after.subtask_cancelled);
  if (remaining < 0 || Number(after.subtask_completed) > remaining)
    return false;
  const willRequestReview =
    before.review_policy === "none" &&
    changes.review_policy === "manual" &&
    before.status === "todo" &&
    remaining > 0 &&
    Number(after.subtask_completed) === remaining &&
    after.parent_rollup_gates_ready;
  return (
    after.will_request_parent_review === willRequestReview &&
    after.status === (willRequestReview ? "waiting_review" : before.status)
  );
}

function validTaskTagProposal(value: Record<string, unknown>): boolean {
  const action = value.action as Record<string, unknown>;
  if (!["task.create", "task.update"].includes(String(action.action)))
    return true;
  const changes = action.changes as Record<string, unknown>;
  const before = (value.preview as Record<string, unknown>).before as Record<
    string,
    unknown
  >;
  const after = (value.preview as Record<string, unknown>).after as Record<
    string,
    unknown
  >;
  if (!Object.hasOwn(changes, "tag_ids"))
    return (
      !Object.hasOwn(before, "tag_names") && !Object.hasOwn(after, "tag_names")
    );
  const ids = changes.tag_ids;
  const beforeNames = before.tag_names;
  const afterNames = after.tag_names;
  const validNames = (entry: unknown): entry is string[] =>
    Array.isArray(entry) &&
    entry.length <= 20 &&
    new Set(entry).size === entry.length &&
    entry.every(
      (name) =>
        typeof name === "string" &&
        name.trim().length > 0 &&
        Array.from(name).length <= 50,
    );
  return (
    Array.isArray(ids) &&
    ids.length <= 20 &&
    new Set(ids).size === ids.length &&
    ids.every((id) => typeof id === "string" && canonicalUuid.test(id)) &&
    validNames(afterNames) &&
    afterNames.length === ids.length &&
    (action.action === "task.create"
      ? !Object.hasOwn(before, "tag_names")
      : validNames(beforeNames))
  );
}

function validTaskParentProposal(value: Record<string, unknown>): boolean {
  const action = value.action as Record<string, unknown>;
  if (!["task.create", "task.update"].includes(String(action.action)))
    return true;
  const changes = action.changes as Record<string, unknown>;
  const before = (value.preview as Record<string, unknown>).before as Record<
    string,
    unknown
  >;
  const after = (value.preview as Record<string, unknown>).after as Record<
    string,
    unknown
  >;
  if (!Object.hasOwn(changes, "parent_task_id"))
    return (
      !Object.hasOwn(before, "parent_task_title") &&
      !Object.hasOwn(after, "parent_task_title")
    );
  const parentId = changes.parent_task_id;
  const validTitle = (entry: unknown) =>
    entry === null ||
    (typeof entry === "string" &&
      entry.trim().length > 0 &&
      Array.from(entry).length <= 200);
  return (
    (parentId === null ||
      (typeof parentId === "string" && canonicalUuid.test(parentId))) &&
    Object.hasOwn(after, "parent_task_title") &&
    validTitle(after.parent_task_title) &&
    (action.action === "task.create"
      ? !Object.hasOwn(before, "parent_task_title")
      : Object.hasOwn(before, "parent_task_title") &&
        validTitle(before.parent_task_title))
  );
}

function validTaskDeleteProposal(value: Record<string, unknown>): boolean {
  const action = value.action as Record<string, unknown>;
  if (action.action !== "task.delete") return true;
  const preview = value.preview as Record<string, unknown>;
  const before = preview.before as Record<string, unknown>;
  const after = preview.after as Record<string, unknown>;
  const counts = [
    "child_tasks_moved_to_top_level",
    "submissions_deleted",
    "artifacts_deleted",
    "file_artifacts_deleted",
    "assignments_deleted",
    "agent_runs_deleted",
    "focus_sessions_detached",
    "inbox_relations_detached",
    "inbox_sources_coordinated",
    "tag_links_deleted",
  ];
  return (
    Object.keys(action.changes as Record<string, unknown>).length === 0 &&
    Object.keys(preview).length === 3 &&
    Object.keys(before).length === 0 &&
    Object.keys(after).length === counts.length + 1 &&
    after.task_deleted === true &&
    counts.every(
      (key) =>
        typeof after[key] === "number" &&
        Number.isSafeInteger(after[key]) &&
        Number(after[key]) >= 0,
    )
  );
}

function validProjectDeleteProposal(value: Record<string, unknown>): boolean {
  const action = value.action as Record<string, unknown>;
  if (action.action !== "project.delete") return true;
  const preview = value.preview as Record<string, unknown>;
  const before = preview.before as Record<string, unknown>;
  const after = preview.after as Record<string, unknown>;
  const counts = [
    "project_tasks_detached",
    "project_draft_invoices_detached",
    "project_financial_entries_detached",
    "project_notes_deleted",
    "project_attachments_deleted",
    "project_attachment_files_deleted",
    "project_inbox_sources_coordinated",
  ];
  return (
    exactKeys(action, [
      "action",
      "project_id",
      "expected_version",
      "changes",
    ]) &&
    object(action.changes) &&
    Object.keys(action.changes).length === 0 &&
    exactKeys(preview, ["label", "before", "after"]) &&
    typeof preview.label === "string" &&
    preview.label.trim().length > 0 &&
    exactKeys(before, ["status", "project_version"]) &&
    before.status === "archived" &&
    before.project_version === action.expected_version &&
    exactKeys(after, ["project_deleted", ...counts]) &&
    after.project_deleted === true &&
    counts.every(
      (key) =>
        typeof after[key] === "number" &&
        Number.isSafeInteger(after[key]) &&
        Number(after[key]) >= 0,
    )
  );
}

function validTagProposal(value: Record<string, unknown>): boolean {
  const action = value.action as Record<string, unknown>;
  if (!String(action.action).startsWith("tag.")) return true;
  const preview = value.preview as Record<string, unknown>;
  const before = preview.before as Record<string, unknown>;
  const after = preview.after as Record<string, unknown>;
  const changes = action.changes as Record<string, unknown>;
  const creating = action.action === "tag.create";
  const updating = action.action === "tag.update";
  const deleting = action.action === "tag.delete";
  const validName = (entry: unknown) =>
    typeof entry === "string" &&
    entry === entry.trim() &&
    Array.from(entry).length >= 1 &&
    Array.from(entry).length <= 50;
  const validColor = (entry: unknown) =>
    typeof entry === "string" && /^#[0-9A-F]{6}$/.test(entry);
  if (
    !exactKeys(preview, ["label", "before", "after"]) ||
    typeof preview.label !== "string" ||
    !validName(preview.label)
  )
    return false;
  if (creating) {
    return (
      exactKeys(action, ["action", "changes"]) &&
      exactKeys(changes, ["name", "color"]) &&
      Object.keys(before).length === 0 &&
      exactKeys(after, ["name", "color"]) &&
      validName(changes.name) &&
      validColor(changes.color) &&
      changes.name === after.name &&
      changes.color === after.color &&
      preview.label === after.name
    );
  }
  if (
    !exactKeys(action, ["action", "tag_id", "expected_version", "changes"]) ||
    typeof action.tag_id !== "string" ||
    !canonicalUuid.test(action.tag_id) ||
    !positiveIntegerValue(action.expected_version)
  )
    return false;
  if (deleting) {
    return (
      Object.keys(changes).length === 0 &&
      exactKeys(before, ["name", "color", "tag_version"]) &&
      exactKeys(after, ["tag_deleted", "detached_tasks"]) &&
      validName(before.name) &&
      validColor(before.color) &&
      before.tag_version === action.expected_version &&
      after.tag_deleted === true &&
      typeof after.detached_tasks === "number" &&
      Number.isSafeInteger(after.detached_tasks) &&
      Number(after.detached_tasks) >= 0 &&
      preview.label === before.name
    );
  }
  if (!updating) return false;
  const keys = Object.keys(changes);
  return (
    keys.length >= 1 &&
    keys.length <= 2 &&
    keys.every((key) => key === "name" || key === "color") &&
    exactKeys(before, keys) &&
    exactKeys(after, keys) &&
    keys.every((key) =>
      key === "name"
        ? validName(changes[key]) &&
          validName(before[key]) &&
          changes[key] === after[key]
        : validColor(changes[key]) &&
          validColor(before[key]) &&
          changes[key] === after[key],
    ) &&
    preview.label === before.name
  );
}

const roadmapEditableFields = new Set([
  "title",
  "description",
  "year",
  "quarter",
  "target_date",
  "status",
  "project_ids",
]);

function validRoadmapChange(field: string, value: unknown): boolean {
  if (field === "title")
    return (
      typeof value === "string" &&
      value.trim().length >= 1 &&
      value.trim().length <= 200
    );
  if (field === "description")
    return (
      value === null ||
      (typeof value === "string" &&
        value.trim().length >= 1 &&
        value.trim().length <= 4_000)
    );
  if (field === "year")
    return (
      typeof value === "number" &&
      Number.isSafeInteger(value) &&
      value >= 2000 &&
      value <= 2100
    );
  if (field === "quarter")
    return (
      typeof value === "number" &&
      Number.isSafeInteger(value) &&
      value >= 1 &&
      value <= 4
    );
  if (field === "target_date")
    return (
      typeof value === "string" &&
      /^\d{4}-\d{2}-\d{2}$/.test(value) &&
      Number.isFinite(Date.parse(`${value}T00:00:00Z`))
    );
  if (field === "status")
    return ["planned", "active", "achieved"].includes(String(value));
  return (
    field === "project_ids" &&
    Array.isArray(value) &&
    value.length <= 100 &&
    new Set(value).size === value.length &&
    value.every((id) => typeof id === "string" && uuid.test(id))
  );
}

function validRoadmapPreviewField(field: string, value: unknown): boolean {
  return field === "status"
    ? ["planned", "active", "achieved", "archived"].includes(String(value))
    : validRoadmapChange(field, value);
}

function validRoadmapProposal(value: Record<string, unknown>): boolean {
  const action = value.action as Record<string, unknown>;
  const preview = value.preview as Record<string, unknown>;
  const changes = action.changes as Record<string, unknown>;
  const before = preview.before as Record<string, unknown>;
  const after = preview.after as Record<string, unknown>;
  if (
    !exactKeys(preview, ["label", "before", "after"]) ||
    Object.keys(changes).some(
      (field) =>
        !roadmapEditableFields.has(field) ||
        !validRoadmapChange(field, changes[field]),
    )
  )
    return false;
  if (action.action === "roadmap_milestone.delete") {
    const counts = [
      "roadmap_milestone_project_links_deleted",
      "roadmap_milestone_inbox_sources_marked",
    ];
    return (
      exactKeys(action, [
        "action",
        "roadmap_milestone_id",
        "expected_version",
        "changes",
      ]) &&
      typeof action.roadmap_milestone_id === "string" &&
      uuid.test(action.roadmap_milestone_id) &&
      typeof action.expected_version === "number" &&
      Number.isSafeInteger(action.expected_version) &&
      Number(action.expected_version) >= 1 &&
      Object.keys(changes).length === 0 &&
      typeof preview.label === "string" &&
      preview.label.trim().length > 0 &&
      exactKeys(before, ["status", "roadmap_milestone_version"]) &&
      before.status === "archived" &&
      before.roadmap_milestone_version === action.expected_version &&
      exactKeys(after, ["roadmap_milestone_deleted", ...counts]) &&
      after.roadmap_milestone_deleted === true &&
      counts.every(
        (key) =>
          typeof after[key] === "number" &&
          Number.isSafeInteger(after[key]) &&
          Number(after[key]) >= 0,
      )
    );
  }
  if (action.action === "roadmap_milestone.create") {
    if (
      !["title", "year", "quarter", "target_date"].every((field) =>
        Object.hasOwn(changes, field),
      ) ||
      Object.keys(before).length !== 0 ||
      !exactKeys(after, [
        "title",
        "description",
        "year",
        "quarter",
        "target_date",
        "status",
        "project_ids",
      ])
    )
      return false;
  } else if (action.action === "roadmap_milestone.update") {
    const keys = Object.keys(changes).sort();
    if (
      keys.length === 0 ||
      JSON.stringify(Object.keys(before).sort()) !== JSON.stringify(keys) ||
      JSON.stringify(Object.keys(after).sort()) !== JSON.stringify(keys)
    )
      return false;
  } else {
    if (
      !["roadmap_milestone.archive", "roadmap_milestone.restore"].includes(
        String(action.action),
      ) ||
      Object.keys(changes).length !== 0 ||
      !exactKeys(before, ["status", "project_ids"]) ||
      !exactKeys(after, ["status", "project_ids"]) ||
      !Array.isArray(before.project_ids) ||
      !Array.isArray(after.project_ids) ||
      JSON.stringify(before.project_ids) !== JSON.stringify(after.project_ids)
    )
      return false;
  }
  return (
    Object.entries(before).every(([field, entry]) =>
      validRoadmapPreviewField(field, entry),
    ) &&
    Object.entries(after).every(([field, entry]) =>
      validRoadmapPreviewField(field, entry),
    )
  );
}

const clientEditableFields = [
  "name",
  "contact_name",
  "email",
  "phone",
  "notes",
  "status",
] as const;

function validClientField(field: string, value: unknown): boolean {
  if (field === "name")
    return (
      typeof value === "string" &&
      value === value.trim() &&
      Array.from(value).length >= 1 &&
      Array.from(value).length <= 200
    );
  if (["contact_name", "phone", "notes"].includes(field)) {
    const maximum = field === "notes" ? 10_000 : field === "phone" ? 50 : 200;
    return (
      value === null ||
      (typeof value === "string" &&
        value === value.trim() &&
        value.length > 0 &&
        Array.from(value).length <= maximum)
    );
  }
  if (field === "email")
    return (
      value === null ||
      (typeof value === "string" &&
        value === value.trim() &&
        value.length <= 320 &&
        /^[^\s@]+@[^\s@]+$/.test(value))
    );
  return (
    field === "status" && ["active", "lead", "inactive"].includes(String(value))
  );
}

function validClientProposal(value: Record<string, unknown>): boolean {
  const action = value.action as Record<string, unknown>;
  const preview = value.preview as Record<string, unknown>;
  const changes = action.changes as Record<string, unknown>;
  const before = preview.before as Record<string, unknown>;
  const after = preview.after as Record<string, unknown>;
  if (action.action === "client.delete") {
    const counts = [
      "client_projects_detached",
      "client_activities_deleted",
      "client_contact_history_deleted",
      "client_attachments_deleted",
      "client_attachment_files_deleted",
    ];
    return (
      exactKeys(action, [
        "action",
        "client_id",
        "expected_version",
        "changes",
      ]) &&
      object(changes) &&
      Object.keys(changes).length === 0 &&
      exactKeys(preview, ["label", "before", "after"]) &&
      typeof preview.label === "string" &&
      preview.label.trim().length > 0 &&
      exactKeys(before, ["status", "client_version"]) &&
      before.status === "inactive" &&
      before.client_version === action.expected_version &&
      exactKeys(after, ["client_deleted", ...counts]) &&
      after.client_deleted === true &&
      counts.every(
        (key) =>
          typeof after[key] === "number" &&
          Number.isSafeInteger(after[key]) &&
          Number(after[key]) >= 0,
      )
    );
  }
  if (
    !exactKeys(preview, ["label", "before", "after"]) ||
    Object.keys(changes).some(
      (field) =>
        !clientEditableFields.includes(
          field as (typeof clientEditableFields)[number],
        ) || !validClientField(field, changes[field]),
    )
  )
    return false;
  if (action.action === "client.create") {
    if (
      !exactKeys(action, ["action", "changes"]) ||
      !Object.hasOwn(changes, "name") ||
      Object.keys(before).length !== 0 ||
      !exactKeys(after, [...clientEditableFields]) ||
      preview.label !== after.name ||
      Object.entries(after).some(
        ([field, entry]) => !validClientField(field, entry),
      )
    )
      return false;
    return Object.entries(changes).every(
      ([field, entry]) => after[field] === entry,
    );
  }
  if (action.action !== "client.update") return false;
  const keys = Object.keys(changes).sort();
  return (
    exactKeys(action, ["action", "client_id", "expected_version", "changes"]) &&
    keys.length > 0 &&
    JSON.stringify(Object.keys(before).sort()) === JSON.stringify(keys) &&
    JSON.stringify(Object.keys(after).sort()) === JSON.stringify(keys) &&
    Object.entries(before).every(([field, entry]) =>
      validClientField(field, entry),
    ) &&
    Object.entries(after).every(([field, entry]) =>
      validClientField(field, entry),
    ) &&
    Object.entries(changes).every(([field, entry]) => after[field] === entry) &&
    preview.label !== ""
  );
}

const personEditableFields = ["display_name", "notes", "status"] as const;

function validPersonField(field: string, value: unknown): boolean {
  if (field === "display_name")
    return (
      typeof value === "string" &&
      value === value.trim() &&
      Array.from(value).length >= 1 &&
      Array.from(value).length <= 100 &&
      !/[\u0000-\u001f\u007f]/u.test(value)
    );
  if (field === "notes")
    return (
      typeof value === "string" &&
      Array.from(value).length <= 2_000 &&
      !/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/u.test(value)
    );
  return field === "status" && ["active", "inactive"].includes(String(value));
}

function validPersonProposal(value: Record<string, unknown>): boolean {
  const action = value.action as Record<string, unknown>;
  const preview = value.preview as Record<string, unknown>;
  const changes = action.changes as Record<string, unknown>;
  const before = preview.before as Record<string, unknown>;
  const after = preview.after as Record<string, unknown>;
  if (
    !exactKeys(preview, ["label", "before", "after"]) ||
    Object.keys(changes).some(
      (field) =>
        !personEditableFields.includes(
          field as (typeof personEditableFields)[number],
        ) || !validPersonField(field, changes[field]),
    )
  )
    return false;
  if (action.action === "person.create")
    return (
      exactKeys(action, ["action", "changes"]) &&
      Object.hasOwn(changes, "display_name") &&
      !Object.hasOwn(changes, "status") &&
      Object.keys(before).length === 0 &&
      exactKeys(after, ["display_name", "notes", "status", "person_type"]) &&
      preview.label === after.display_name &&
      after.person_type === "person" &&
      after.status === "active" &&
      validPersonField("display_name", after.display_name) &&
      validPersonField("notes", after.notes) &&
      Object.entries(changes).every(([field, entry]) => after[field] === entry)
    );
  if (action.action !== "person.update") return false;
  const keys = Object.keys(changes).sort();
  return (
    exactKeys(action, ["action", "actor_id", "expected_version", "changes"]) &&
    keys.length > 0 &&
    JSON.stringify(Object.keys(before).sort()) === JSON.stringify(keys) &&
    JSON.stringify(Object.keys(after).sort()) === JSON.stringify(keys) &&
    Object.entries(before).every(([field, entry]) =>
      validPersonField(field, entry),
    ) &&
    Object.entries(after).every(([field, entry]) =>
      validPersonField(field, entry),
    ) &&
    Object.entries(changes).every(([field, entry]) => after[field] === entry) &&
    typeof preview.label === "string" &&
    preview.label.length > 0
  );
}

function validClientContactProposal(value: Record<string, unknown>): boolean {
  const action = value.action as Record<string, unknown>;
  const preview = value.preview as Record<string, unknown>;
  const changes = action.changes as Record<string, unknown>;
  const before = preview.before as Record<string, unknown>;
  const after = preview.after as Record<string, unknown>;
  const linking = action.action === "client_contact.link";
  const unlinking = action.action === "client_contact.unlink";
  const identityKeys = [
    "client_id",
    "client_name",
    "client_status",
    "actor_id",
    "actor_name",
    "actor_type",
    "actor_status",
    "actor_version",
    "role",
    "link_state",
  ];
  const validIdentity = (
    record: Record<string, unknown>,
    state: "active" | "unlinked",
  ) =>
    exactKeys(record, [
      ...identityKeys,
      ...(state === "unlinked" ? ["reason"] : []),
    ]) &&
    typeof record.client_id === "string" &&
    uuid.test(record.client_id) &&
    typeof record.client_name === "string" &&
    record.client_name.length > 0 &&
    ["active", "lead", "inactive"].includes(String(record.client_status)) &&
    typeof record.actor_id === "string" &&
    uuid.test(record.actor_id) &&
    typeof record.actor_name === "string" &&
    record.actor_name.length > 0 &&
    record.actor_type === "person" &&
    record.actor_status === "active" &&
    typeof record.actor_version === "number" &&
    Number.isSafeInteger(record.actor_version) &&
    record.actor_version > 0 &&
    record.role === "contact" &&
    record.link_state === state &&
    (state !== "unlinked" ||
      (typeof record.reason === "string" &&
        record.reason.trim() === record.reason &&
        record.reason.length > 0 &&
        Array.from(record.reason).length <= 1_000));
  if (
    !exactKeys(preview, ["label", "before", "after"]) ||
    (!linking && !unlinking) ||
    preview.label !== after.client_name ||
    action.client_id !== after.client_id ||
    !validIdentity(after, unlinking ? "unlinked" : "active")
  )
    return false;
  if (linking) {
    return (
      exactKeys(action, [
        "action",
        "client_id",
        "expected_version",
        "changes",
      ]) &&
      exactKeys(changes, ["actor_id"]) &&
      exactKeys(before, ["link_state"]) &&
      before.link_state === "none" &&
      changes.actor_id === after.actor_id
    );
  }
  return (
    exactKeys(action, [
      "action",
      "client_id",
      "client_actor_link_id",
      "expected_version",
      "changes",
    ]) &&
    typeof action.client_actor_link_id === "string" &&
    uuid.test(action.client_actor_link_id) &&
    exactKeys(changes, ["reason"]) &&
    validIdentity(before, "active") &&
    identityKeys
      .filter((key) => key !== "link_state")
      .every((key) => before[key] === after[key]) &&
    changes.reason === after.reason
  );
}

const contentItemEditableFields = new Set([
  "title",
  "platform",
  "project_id",
  "notes",
  "external_link",
]);
const contentItemStatuses = new Set([
  "draft",
  "in_review",
  "scheduled",
  "published",
  "cancelled",
  "archived",
]);

function validContentTimezone(value: string): boolean {
  if (value.length > 100) return false;
  try {
    new Intl.DateTimeFormat("en", { timeZone: value });
    return true;
  } catch {
    return false;
  }
}

function validContentItemField(field: string, value: unknown): boolean {
  if (field === "title")
    return (
      typeof value === "string" &&
      value.trim().length >= 1 &&
      value.trim().length <= 200
    );
  if (field === "platform")
    return (
      typeof value === "string" &&
      value.trim().length >= 1 &&
      value.trim().length <= 64
    );
  if (field === "project_id")
    return value === null || (typeof value === "string" && uuid.test(value));
  if (field === "notes" || field === "external_link") {
    const maximum = field === "notes" ? 4_000 : 2_048;
    return (
      value === null ||
      (typeof value === "string" &&
        value.trim().length >= 1 &&
        value.trim().length <= maximum)
    );
  }
  if (field === "scheduled_at")
    return (
      value === null ||
      (typeof value === "string" &&
        /^\d{4}-\d{2}-\d{2}T/.test(value) &&
        Number.isFinite(Date.parse(value)))
    );
  if (field === "scheduled_timezone")
    return (
      value === null ||
      (typeof value === "string" &&
        value === value.trim() &&
        validContentTimezone(value))
    );
  return field === "status" && contentItemStatuses.has(String(value));
}

function validContentItemProposal(value: Record<string, unknown>): boolean {
  const action = value.action as Record<string, unknown>;
  const preview = value.preview as Record<string, unknown>;
  const changes = action.changes as Record<string, unknown>;
  const before = preview.before as Record<string, unknown>;
  const after = preview.after as Record<string, unknown>;
  if (!exactKeys(preview, ["label", "before", "after"])) return false;

  if (action.action === "content_item.publish") {
    if (
      !exactKeys(action, [
        "action",
        "content_item_id",
        "expected_version",
        "changes",
      ]) ||
      typeof action.content_item_id !== "string" ||
      !canonicalUuid.test(action.content_item_id) ||
      !positiveIntegerValue(action.expected_version) ||
      typeof preview.label !== "string" ||
      preview.label.trim().length < 2 ||
      !object(changes) ||
      Object.keys(changes).some(
        (key) => !["published_at", "external_link"].includes(key),
      ) ||
      !exactKeys(before, ["status", "published_at", "external_link"]) ||
      !exactKeys(after, ["status", "published_at", "external_link"]) ||
      !["draft", "in_review", "scheduled"].includes(String(before.status)) ||
      before.published_at !== null ||
      !(
        before.external_link === null ||
        typeof before.external_link === "string"
      ) ||
      after.status !== "published" ||
      !(after.external_link === null || typeof after.external_link === "string")
    )
      return false;
    if (Object.hasOwn(changes, "published_at")) {
      if (
        typeof changes.published_at !== "string" ||
        !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/.test(
          changes.published_at,
        ) ||
        !Number.isFinite(Date.parse(changes.published_at)) ||
        typeof after.published_at !== "string" ||
        !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/.test(
          after.published_at,
        ) ||
        Date.parse(after.published_at) !== Date.parse(changes.published_at)
      )
        return false;
    } else if (after.published_at !== "confirmation_time") return false;
    if (Object.hasOwn(changes, "external_link")) {
      if (
        !(
          changes.external_link === null ||
          (typeof changes.external_link === "string" &&
            changes.external_link.trim().length > 0 &&
            Array.from(changes.external_link.trim()).length <= 2048)
        ) ||
        after.external_link !==
          (typeof changes.external_link === "string"
            ? changes.external_link.trim()
            : null)
      )
        return false;
    } else if (after.external_link !== before.external_link) return false;
    if (value.result_id !== null && value.result_id !== action.content_item_id)
      return false;
    if (
      value.result_version !== null &&
      value.result_version !== action.expected_version &&
      value.result_version !== Number(action.expected_version) + 1
    )
      return false;
    return (
      value.status !== "confirmed" ||
      (value.result_id === action.content_item_id &&
        value.result_version !== null)
    );
  }

  if (
    [
      "content_item.link_task",
      "content_item.set_task_required",
      "content_item.unlink_task",
    ].includes(String(action.action))
  ) {
    const linking = action.action === "content_item.link_task";
    const setting = action.action === "content_item.set_task_required";
    const unlinking = action.action === "content_item.unlink_task";
    const relationKeys = [
      "task_id",
      "task_title",
      "task_version",
      "task_status",
      "relation_state",
      "is_required",
      "content_item_version",
    ];
    const validBefore =
      exactKeys(before, [
        "relation_state",
        "is_required",
        "content_item_version",
      ]) &&
      ["linked", "unlinked"].includes(String(before.relation_state)) &&
      typeof before.is_required === "boolean" &&
      positiveIntegerValue(before.content_item_version);
    const validAfter = (record: Record<string, unknown>) =>
      exactKeys(record, relationKeys) &&
      typeof record.task_id === "string" &&
      canonicalUuid.test(record.task_id) &&
      typeof record.task_title === "string" &&
      record.task_title.trim().length > 0 &&
      positiveIntegerValue(record.task_version) &&
      [
        "todo",
        "in_progress",
        "blocked",
        "waiting_review",
        "done",
        "cancelled",
      ].includes(String(record.task_status)) &&
      ["linked", "unlinked"].includes(String(record.relation_state)) &&
      typeof record.is_required === "boolean" &&
      positiveIntegerValue(record.content_item_version);
    if (
      !exactKeys(action, [
        "action",
        "content_item_id",
        "expected_version",
        "changes",
      ]) ||
      typeof action.content_item_id !== "string" ||
      !canonicalUuid.test(action.content_item_id) ||
      !positiveIntegerValue(action.expected_version) ||
      typeof preview.label !== "string" ||
      preview.label.trim().length === 0 ||
      !validBefore ||
      !validAfter(after) ||
      !exactKeys(
        changes,
        unlinking
          ? ["task_id", "expected_task_version"]
          : ["task_id", "expected_task_version", "is_required"],
      ) ||
      changes.task_id !== after.task_id ||
      changes.expected_task_version !== after.task_version ||
      before.content_item_version !== action.expected_version ||
      after.content_item_version !== Number(action.expected_version) + 1 ||
      !positiveIntegerValue(changes.expected_task_version)
    )
      return false;
    if (linking)
      return (
        before.relation_state === "unlinked" &&
        before.is_required === false &&
        after.relation_state === "linked" &&
        after.is_required === changes.is_required &&
        typeof changes.is_required === "boolean"
      );
    if (setting)
      return (
        before.relation_state === "linked" &&
        after.relation_state === "linked" &&
        typeof changes.is_required === "boolean" &&
        after.is_required === changes.is_required &&
        before.is_required !== after.is_required
      );
    return (
      unlinking &&
      before.relation_state === "linked" &&
      after.relation_state === "unlinked" &&
      before.is_required === after.is_required
    );
  }

  if (action.action === "content_item.delete") {
    const counts = [
      "content_item_task_links_deleted",
      "content_item_inbox_sources_marked",
    ];
    return (
      exactKeys(action, [
        "action",
        "content_item_id",
        "expected_version",
        "changes",
      ]) &&
      typeof action.content_item_id === "string" &&
      uuid.test(action.content_item_id) &&
      typeof action.expected_version === "number" &&
      Number.isSafeInteger(action.expected_version) &&
      Number(action.expected_version) >= 1 &&
      object(changes) &&
      Object.keys(changes).length === 0 &&
      typeof preview.label === "string" &&
      preview.label.trim().length > 0 &&
      exactKeys(before, ["status", "content_item_version"]) &&
      before.status === "archived" &&
      before.content_item_version === action.expected_version &&
      exactKeys(after, ["content_item_deleted", ...counts]) &&
      after.content_item_deleted === true &&
      counts.every(
        (key) =>
          typeof after[key] === "number" &&
          Number.isSafeInteger(after[key]) &&
          Number(after[key]) >= 0,
      )
    );
  }

  if (action.action === "content_item.create") {
    const allowed = new Set([
      ...contentItemEditableFields,
      "status",
      "scheduled_at",
      "scheduled_timezone",
    ]);
    if (
      !Object.hasOwn(changes, "title") ||
      !Object.hasOwn(changes, "platform") ||
      Object.keys(changes).some(
        (field) =>
          !allowed.has(field) || !validContentItemField(field, changes[field]),
      ) ||
      Object.keys(before).length !== 0 ||
      !exactKeys(after, [
        "title",
        "platform",
        "status",
        "scheduled_at",
        "scheduled_timezone",
        "project_id",
        "notes",
        "external_link",
      ]) ||
      !["draft", "in_review", "scheduled"].includes(String(after.status))
    )
      return false;
    const hasAt = Object.hasOwn(changes, "scheduled_at");
    const hasTimezone = Object.hasOwn(changes, "scheduled_timezone");
    if (hasAt !== hasTimezone || (hasAt && after.status !== "scheduled"))
      return false;
  } else if (action.action === "content_item.update") {
    const keys = Object.keys(changes).sort();
    if (
      keys.length === 0 ||
      keys.some(
        (field) =>
          !contentItemEditableFields.has(field) ||
          !validContentItemField(field, changes[field]),
      ) ||
      JSON.stringify(Object.keys(before).sort()) !== JSON.stringify(keys) ||
      JSON.stringify(Object.keys(after).sort()) !== JSON.stringify(keys)
    )
      return false;
  } else if (action.action === "content_item.schedule") {
    if (
      !exactKeys(changes, ["scheduled_at", "scheduled_timezone"]) ||
      !exactKeys(before, ["scheduled_at", "scheduled_timezone", "status"]) ||
      !exactKeys(after, ["scheduled_at", "scheduled_timezone", "status"]) ||
      after.status !== "scheduled" ||
      after.scheduled_at === null ||
      after.scheduled_timezone === null
    )
      return false;
  } else if (action.action === "content_item.unschedule") {
    if (
      Object.keys(changes).length !== 0 ||
      !exactKeys(before, ["scheduled_at", "scheduled_timezone", "status"]) ||
      !exactKeys(after, ["scheduled_at", "scheduled_timezone", "status"]) ||
      after.scheduled_at !== null ||
      after.scheduled_timezone !== null
    )
      return false;
  } else {
    if (
      ![
        "content_item.review",
        "content_item.cancel",
        "content_item.reopen",
        "content_item.archive",
        "content_item.restore",
      ].includes(String(action.action)) ||
      Object.keys(changes).length !== 0 ||
      !exactKeys(before, ["status"]) ||
      !exactKeys(after, ["status"])
    )
      return false;
  }
  return (
    Object.entries(before).every(([field, entry]) =>
      validContentItemField(field, entry),
    ) &&
    Object.entries(after).every(([field, entry]) =>
      validContentItemField(field, entry),
    )
  );
}
function option(value: unknown, types: string[]): value is AiTaskOption {
  return (
    object(value) &&
    typeof value.id === "string" &&
    uuid.test(value.id) &&
    typeof value.label === "string" &&
    value.label.length > 0 &&
    typeof value.type === "string" &&
    types.includes(value.type) &&
    typeof value.version === "number" &&
    Number.isSafeInteger(value.version) &&
    value.version > 0
  );
}
function splitDrafts(
  value: unknown,
  changes: Record<string, unknown>,
): value is AiInboxSplitDraft[] {
  if (
    !Array.isArray(value) ||
    value.length < 1 ||
    value.length > 20 ||
    !Array.isArray(changes.tasks) ||
    changes.tasks.length !== value.length
  )
    return false;
  const keys = new Set<string>();
  const rawTasks = changes.tasks;
  return value.every((draft, index) => {
    const raw = rawTasks[index];
    if (
      !object(draft) ||
      !object(raw) ||
      typeof draft.key !== "string" ||
      !draft.key ||
      keys.has(draft.key) ||
      typeof draft.parent_key !== "string" ||
      (draft.parent_key !== "" && !keys.has(draft.parent_key)) ||
      typeof raw.key !== "string" ||
      draft.key !== raw.key.trim() ||
      draft.parent_key !==
        (typeof raw.parent_key === "string" ? raw.parent_key.trim() : "") ||
      !fields(draft.fields) ||
      !option(draft.assignee, ["owner", "person", "agent"]) ||
      draft.assignee.id !== raw.assignee_actor_id ||
      typeof draft.is_required !== "boolean" ||
      draft.is_required !== raw.is_required ||
      !Array.isArray(draft.tags) ||
      !draft.tags.every((tag) => option(tag, ["tag"])) ||
      new Set(draft.tags.map((tag) => tag.id)).size !== draft.tags.length ||
      (draft.project !== null && !option(draft.project, ["project"])) ||
      (draft.reviewer !== null && !option(draft.reviewer, ["owner"]))
    )
      return false;
    const f = draft.fields;
    if (
      [
        "title",
        "description",
        "priority",
        "kind",
        "completion_criteria",
        "review_policy",
        "status",
      ].some((key) => typeof f[key] !== "string") ||
      typeof raw.title !== "string" ||
      f.title !== raw.title.trim() ||
      !["work", "review", "followup", "reminder"].includes(String(f.kind)) ||
      !["P0", "P1", "P2", "P3"].includes(String(f.priority)) ||
      !["none", "manual"].includes(String(f.review_policy)) ||
      f.status !== "todo" ||
      ["planned_date", "due_date"].some(
        (key) => f[key] !== null && typeof f[key] !== "string",
      ) ||
      (f.estimated_minutes !== null &&
        (typeof f.estimated_minutes !== "number" ||
          !Number.isSafeInteger(f.estimated_minutes) ||
          f.estimated_minutes < 0)) ||
      f.project_id !== (draft.project?.id ?? null) ||
      (raw.project_id ?? null) !== f.project_id ||
      (f.review_policy === "manual") !== (draft.reviewer !== null) ||
      (draft.reviewer !== null &&
        draft.reviewer.id !== "00000000-0000-5000-8000-000000000001")
    )
      return false;
    const tagIds = raw.tag_ids ?? [];
    const tags = draft.tags;
    if (
      !Array.isArray(tagIds) ||
      tagIds.length !== draft.tags.length ||
      tagIds.some((id) => !tags.some((tag) => tag.id === id))
    )
      return false;
    keys.add(draft.key);
    return true;
  });
}
function validAgentRunStartProposal(
  value: Record<string, unknown>,
  nativePreview = false,
): boolean {
  const action = value.action;
  const preview = value.preview;
  if (!object(action) || !object(preview)) return false;
  const changes = action.changes;
  const snapshot = preview.agent_run_start;
  if (!object(changes) || !object(snapshot)) return false;
  const task = snapshot.task;
  const assignment = snapshot.assignment;
  const agent = snapshot.agent;
  const adapter = snapshot.adapter;
  const provider = snapshot.provider;
  const limits = snapshot.runtime_limits;
  if (
    !object(task) ||
    !object(assignment) ||
    !object(agent) ||
    !object(adapter) ||
    !object(provider) ||
    !object(limits)
  )
    return false;
  const expectedVersion = action.expected_version;
  const retry = action.action === "agent_run.retry";
  const hasRestart = Object.hasOwn(changes, "restart_of_run_id");
  const restart = snapshot.restart;
  const hasStartGate = Object.hasOwn(changes, "start_after_run_id");
  const gate = snapshot.start_gate;
  if (
    hasStartGate !== Object.hasOwn(snapshot, "start_gate") ||
    (hasStartGate &&
      (retry ||
        hasRestart ||
        !object(gate) ||
        !exactKeys(gate, [
          "predecessor_run_id",
          "predecessor_task_id",
          "predecessor_task_title",
          "predecessor_attempt",
          "require",
          "within_hours",
        ]) ||
        gate.predecessor_run_id !== changes.start_after_run_id ||
        typeof gate.predecessor_run_id !== "string" ||
        !canonicalUuid.test(gate.predecessor_run_id) ||
        typeof gate.predecessor_task_id !== "string" ||
        !canonicalUuid.test(gate.predecessor_task_id) ||
        gate.predecessor_task_id === task.id ||
        typeof gate.predecessor_task_title !== "string" ||
        gate.predecessor_task_title.trim() === "" ||
        !positiveIntegerValue(gate.predecessor_attempt) ||
        gate.require !== changes.start_after ||
        (gate.require !== "submitted" && gate.require !== "accepted") ||
        gate.within_hours !== changes.start_within_hours ||
        !Number.isInteger(gate.within_hours) ||
        Number(gate.within_hours) < 1 ||
        Number(gate.within_hours) > 24))
  )
    return false;
  const gateChangeKeys = hasStartGate
    ? ["start_after_run_id", "start_after", "start_within_hours"]
    : [];
  if (
    hasRestart !== Object.hasOwn(snapshot, "restart") ||
    (hasRestart &&
      (retry ||
        !validAgentRunRestartSource(restart) ||
        restart.run_id !== changes.restart_of_run_id ||
        restart.task_id !== task.id))
  )
    return false;
  const previous = preview.agent_run_retry;
  if (
    retry &&
    (!object(previous) ||
      !exactKeys(previous, ["run_id", "status", "attempt"]) ||
      typeof previous.run_id !== "string" ||
      !canonicalUuid.test(previous.run_id) ||
      previous.run_id !== action.agent_run_id ||
      !["failed", "cancelled", "interrupted"].includes(
        String(previous.status),
      ) ||
      !positiveIntegerValue(previous.attempt) ||
      !positiveIntegerValue(snapshot.attempt) ||
      Number(previous.attempt) >= Number(snapshot.attempt))
  )
    return false;
  const providerId = retry ? provider.id : changes.provider_id;
  const expectedProviderVersion = retry
    ? provider.version
    : changes.expected_provider_version;
  const expectedProviderConfigVersion = retry
    ? provider.config_version
    : changes.expected_provider_config_version;
  const executionContractVersion = snapshot.execution_contract_version;
  const v4 = executionContractVersion === 4;
  const v5 = executionContractVersion === 5;
  const v6 = executionContractVersion === 6;
  const anthropic = v5 || (v6 && provider.protocol === "anthropic_messages");
  const v2 =
    executionContractVersion === 2 ||
    executionContractVersion === 3 ||
    v4 ||
    v5 ||
    v6;
  const hasRework = Object.hasOwn(snapshot, "rework_context");
  const hasReworkID = Object.hasOwn(changes, "rework_submission_id");
  const hasReworkArtifacts = Object.hasOwn(changes, "rework_artifact_ids");
  const rework = snapshot.rework_context;
  const reworkIDs = changes.rework_artifact_ids;
  if (
    (v4 && !hasRework) ||
    (!v4 && !v5 && !v6 && hasRework) ||
    (!hasRework && (hasReworkID || hasReworkArtifacts)) ||
    (hasRework &&
      (!validAgentRunReworkContext(rework) ||
        task.review_policy !== "manual" ||
        task.status !== "in_progress" ||
        (!retry &&
          (!hasReworkID ||
            !hasReworkArtifacts ||
            changes.rework_submission_id !== rework.submission_id ||
            !Array.isArray(reworkIDs) ||
            reworkIDs.length !== rework.artifacts.length ||
            rework.artifacts.some(
              (artifact, index) => artifact.id !== reworkIDs[index],
            )))))
  )
    return false;
  const hasCandidateIDs = Object.hasOwn(changes, "input_file_candidate_ids");
  const hasOutputKind = Object.hasOwn(changes, "output_kind");
  const candidateIDs = changes.input_file_candidate_ids;
  const outputKind =
    retry && object(snapshot.output_contract)
      ? snapshot.output_contract.type
      : hasOutputKind
        ? changes.output_kind
        : "text";
  const providerHasLeavesDevice = Object.hasOwn(provider, "leaves_device");
  const hasInputFiles = Object.hasOwn(snapshot, "input_files");
  const hasInputFileTotal = Object.hasOwn(snapshot, "input_file_total_bytes");
  const inputFiles = snapshot.input_files;
  const outputContract = snapshot.output_contract;
  const fileLimits = snapshot.file_limits;
  const commonSnapshotKeys = [
    "task",
    "assignment",
    "agent",
    "adapter",
    "provider",
    "attempt",
    "execution_contract_version",
    "runtime_limits",
    "success_does_not_complete_task",
    "result_requires_manual_review",
    ...(hasRework ? ["rework_context"] : []),
    ...(hasRestart ? ["restart"] : []),
    ...(hasStartGate ? ["start_gate"] : []),
  ];
  const validCandidateIDs =
    !hasCandidateIDs ||
    (Array.isArray(candidateIDs) &&
      (v5 || candidateIDs.length > 0) &&
      candidateIDs.length <= 4 &&
      candidateIDs.every(
        (candidateID) =>
          typeof candidateID === "string" && canonicalUuid.test(candidateID),
      ) &&
      new Set(candidateIDs).size === candidateIDs.length);
  const validChanges = retry
    ? exactKeys(changes, [])
    : v2
      ? exactKeys(changes, [
          "provider_id",
          "expected_provider_version",
          "expected_provider_config_version",
          ...(hasCandidateIDs ? ["input_file_candidate_ids"] : []),
          ...(hasOutputKind ? ["output_kind"] : []),
          ...(hasRework ? ["rework_submission_id", "rework_artifact_ids"] : []),
          ...(hasRestart ? ["restart_of_run_id"] : []),
          ...gateChangeKeys,
        ]) &&
        (v4 || v5 || v6 || hasCandidateIDs || hasOutputKind) &&
        validCandidateIDs &&
        (!hasOutputKind ||
          outputKind === "text" ||
          outputKind === "file" ||
          outputKind === "files")
      : exactKeys(changes, [
          "provider_id",
          "expected_provider_version",
          "expected_provider_config_version",
          ...(hasRestart ? ["restart_of_run_id"] : []),
          ...(hasCandidateIDs ? ["input_file_candidate_ids"] : []),
          ...(hasOutputKind ? ["output_kind"] : []),
          ...gateChangeKeys,
        ]) &&
        (!hasCandidateIDs ||
          (Array.isArray(candidateIDs) && candidateIDs.length === 0)) &&
        (!hasOutputKind || outputKind === "text");
  const validFrozenFiles =
    (!hasInputFiles &&
      !hasInputFileTotal &&
      (!hasCandidateIDs ||
        (v5 && Array.isArray(candidateIDs) && candidateIDs.length === 0))) ||
    (hasInputFiles &&
      hasInputFileTotal &&
      Array.isArray(inputFiles) &&
      (retry ||
        (hasCandidateIDs &&
          Array.isArray(candidateIDs) &&
          inputFiles.length === candidateIDs.length)) &&
      inputFiles.length > 0 &&
      inputFiles.length <= 4 &&
      inputFiles.every(validAgentRunInputFile) &&
      inputFiles.every((file) =>
        validFileSourceProof(
          file as unknown as Record<string, unknown>,
          v6,
          String(task.id),
          task.project_id as string | null,
        ),
      ) &&
      v6 ===
        inputFiles.some(
          (file) => file.source_kind === "project_task_artifact",
        ) &&
      new Set(inputFiles.map((file) => file.id)).size === inputFiles.length &&
      (retry ||
        nativePreview ||
        (Array.isArray(candidateIDs) &&
          candidateIDs.every(
            (candidateID) =>
              !inputFiles.some((file) => file.id === candidateID),
          ))) &&
      positiveIntegerValue(snapshot.input_file_total_bytes) &&
      snapshot.input_file_total_bytes ===
        inputFiles.reduce((total, file) => total + file.size_bytes, 0) &&
      snapshot.input_file_total_bytes <= 131_072);
  const fileBoundary =
    !v5 ||
    (Array.isArray(inputFiles) && inputFiles.length > 0) ||
    (object(outputContract) &&
      ["file", "files"].includes(String(outputContract.type)));
  const validV2Boundary =
    v2 &&
    exactKeys(snapshot, [
      ...commonSnapshotKeys,
      ...(hasInputFiles ? ["input_files"] : []),
      ...(hasInputFileTotal ? ["input_file_total_bytes"] : []),
      "output_contract",
      ...(fileBoundary ? ["input_files_leave_device", "file_limits"] : []),
    ]) &&
    validFrozenFiles &&
    validAgentRunOutputContract(outputContract) &&
    outputContract.type === outputKind &&
    (!fileBoundary ||
      (typeof snapshot.input_files_leave_device === "boolean" &&
        snapshot.input_files_leave_device ===
          (hasInputFiles && provider.leaves_device === true) &&
        object(fileLimits) &&
        exactKeys(fileLimits, [
          "max_files",
          "max_file_bytes",
          "max_total_bytes",
          "max_result_bytes",
        ]) &&
        fileLimits.max_files === 4 &&
        fileLimits.max_file_bytes === 65_536 &&
        fileLimits.max_total_bytes === 131_072 &&
        fileLimits.max_result_bytes === 65_536));
  const confirmed = value.status === "confirmed";
  const confirmedHasLiveResult = confirmed && object(value.agent_run_result);
  if (
    !exactKeys(value, [
      "id",
      "generation_id",
      "fingerprint",
      "action",
      "preview",
      "status",
      "can_confirm",
      "result_id",
      "result_version",
      "route",
      "created_at",
      "decided_at",
      ...(confirmedHasLiveResult ? ["agent_run_result"] : []),
    ]) ||
    !exactKeys(action, [
      "action",
      "task_id",
      "expected_version",
      "changes",
      ...(retry ? ["agent_run_id"] : []),
    ]) ||
    (!retry && action.action !== "agent_run.start") ||
    typeof action.task_id !== "string" ||
    !canonicalUuid.test(action.task_id) ||
    !positiveIntegerValue(expectedVersion) ||
    !validChanges ||
    typeof providerId !== "string" ||
    !canonicalUuid.test(providerId) ||
    !positiveIntegerValue(expectedProviderVersion) ||
    !positiveIntegerValue(expectedProviderConfigVersion) ||
    !exactKeys(preview, [
      "label",
      "before",
      "after",
      "agent_run_start",
      ...(retry ? ["agent_run_retry"] : []),
    ]) ||
    !object(preview.before) ||
    Object.keys(preview.before).length !== 0 ||
    !object(preview.after) ||
    Object.keys(preview.after).length !== 0 ||
    (!v2 && !exactKeys(snapshot, commonSnapshotKeys)) ||
    (v2 && !validV2Boundary) ||
    !exactKeys(task, [
      "id",
      "title",
      "description",
      "completion_criteria",
      "status",
      "kind",
      "review_policy",
      "priority",
      "project_id",
      "parent_task_id",
      "due_date",
      "planned_date",
      "estimated_minutes",
      "actual_minutes",
      "manual_order",
      "version",
      "created_at",
      "updated_at",
    ]) ||
    task.id !== action.task_id ||
    !nonemptyString(task.title) ||
    typeof task.description !== "string" ||
    typeof task.completion_criteria !== "string" ||
    !["todo", "in_progress"].includes(String(task.status)) ||
    !["work", "review", "followup", "reminder"].includes(String(task.kind)) ||
    !["none", "manual"].includes(String(task.review_policy)) ||
    !["P0", "P1", "P2", "P3"].includes(String(task.priority)) ||
    (task.project_id !== null &&
      (typeof task.project_id !== "string" ||
        !canonicalUuid.test(task.project_id))) ||
    (task.parent_task_id !== null &&
      (typeof task.parent_task_id !== "string" ||
        !canonicalUuid.test(task.parent_task_id))) ||
    (task.due_date !== null && typeof task.due_date !== "string") ||
    (task.planned_date !== null && typeof task.planned_date !== "string") ||
    (task.estimated_minutes !== null &&
      (typeof task.estimated_minutes !== "number" ||
        !Number.isSafeInteger(task.estimated_minutes) ||
        task.estimated_minutes < 0)) ||
    typeof task.actual_minutes !== "number" ||
    !Number.isSafeInteger(task.actual_minutes) ||
    task.actual_minutes < 0 ||
    (task.manual_order !== null &&
      (typeof task.manual_order !== "number" ||
        !Number.isSafeInteger(task.manual_order))) ||
    task.version !== expectedVersion ||
    !utcTimestamp(task.created_at) ||
    !utcTimestamp(task.updated_at) ||
    preview.label !== task.title ||
    !exactKeys(assignment, ["id", "role", "assigned_at", "actor_id"]) ||
    typeof assignment.id !== "string" ||
    !canonicalUuid.test(assignment.id) ||
    assignment.role !== "assignee" ||
    !utcTimestamp(assignment.assigned_at) ||
    typeof assignment.actor_id !== "string" ||
    !canonicalUuid.test(assignment.actor_id) ||
    !exactKeys(agent, ["id", "display_name", "type", "status", "version"]) ||
    agent.id !== assignment.actor_id ||
    !nonemptyString(agent.display_name) ||
    agent.type !== "agent" ||
    agent.status !== "active" ||
    !positiveIntegerValue(agent.version) ||
    !exactKeys(adapter, [
      "id",
      "display_name",
      "kind",
      "protocol_version",
      "status",
      "health_status",
      "isolation_status",
      "execution_ready",
      "version",
    ]) ||
    typeof adapter.id !== "string" ||
    !canonicalUuid.test(adapter.id) ||
    !nonemptyString(adapter.display_name) ||
    adapter.kind !== "builtin" ||
    adapter.protocol_version !== "opc-agent-pipe-v1" ||
    adapter.status !== "enabled" ||
    adapter.health_status !== "healthy" ||
    adapter.isolation_status !== "verified" ||
    adapter.execution_ready !== true ||
    !positiveIntegerValue(adapter.version) ||
    !exactKeys(provider, [
      "id",
      "name",
      "kind",
      "protocol",
      "model",
      "status",
      "health_status",
      "version",
      "config_version",
      ...(providerHasLeavesDevice ? ["leaves_device"] : []),
    ]) ||
    provider.id !== providerId ||
    !nonemptyString(provider.name) ||
    !["local", "remote"].includes(String(provider.kind)) ||
    (anthropic
      ? provider.protocol !== "anthropic_messages" || provider.kind !== "remote"
      : provider.protocol !== "openai_chat") ||
    !nonemptyString(provider.model) ||
    provider.status !== "ready" ||
    provider.health_status !== "healthy" ||
    provider.version !== expectedProviderVersion ||
    provider.config_version !== expectedProviderConfigVersion ||
    (v2 && !providerHasLeavesDevice) ||
    (providerHasLeavesDevice &&
      provider.leaves_device !== (provider.kind === "remote")) ||
    !positiveIntegerValue(snapshot.attempt) ||
    ![1, 2, 3, 4, 5, 6].includes(Number(executionContractVersion)) ||
    !exactKeys(limits, [
      "timeout_seconds",
      "max_result_bytes",
      ...(anthropic ? ["max_output_tokens"] : []),
    ]) ||
    (anthropic && limits.max_output_tokens !== 8192) ||
    (v6 &&
      (!Array.isArray(inputFiles) ||
        !inputFiles.some(
          (file) => file.source_kind === "project_task_artifact",
        ))) ||
    limits.timeout_seconds !== 600 ||
    limits.max_result_bytes !== 65_536 ||
    snapshot.success_does_not_complete_task !== true ||
    snapshot.result_requires_manual_review !== true
  )
    return false;

  const result = value.agent_run_result;
  const runId = value.result_id;
  const baseRoute = `/tasks/${task.id}`;
  if (!confirmed) {
    return (
      runId === null &&
      value.result_version === null &&
      result === undefined &&
      value.route ===
        (retry
          ? agentRunHref(task.id as string, action.agent_run_id as string)
          : hasRestart
            ? agentRunHref(
                task.id as string,
                changes.restart_of_run_id as string,
              )
            : baseRoute)
    );
  }
  if (
    typeof runId !== "string" ||
    !canonicalUuid.test(runId) ||
    (retry && runId === action.agent_run_id) ||
    (hasRestart && runId === changes.restart_of_run_id) ||
    value.result_version !== 1
  )
    return false;
  const runRoute = agentRunHref(task.id as string, runId);
  if (result === undefined) return value.route === "";
  if (!object(result)) return false;
  if (
    hasRestart
      ? result.restart_of_run_id !== changes.restart_of_run_id
      : Object.hasOwn(result, "restart_of_run_id")
  )
    return false;
  const resultStatus = result.status;
  const deliveryStatus = result.output_delivery_status;
  const deliveryErrorCode = result.output_delivery_error_code;
  const submissionId = result.submission_id;
  const submissionStatus = result.submission_status;
  const artifactId = result.artifact_id;
  if (
    ![
      "queued",
      "running",
      "succeeded",
      "failed",
      "cancelled",
      "interrupted",
    ].includes(String(resultStatus)) ||
    !["not_ready", "pending", "submitted", "retained"].includes(
      String(deliveryStatus),
    ) ||
    (deliveryErrorCode !== null &&
      (typeof deliveryErrorCode !== "string" || !deliveryErrorCode)) ||
    (submissionId !== null &&
      (typeof submissionId !== "string" ||
        !canonicalUuid.test(submissionId))) ||
    (artifactId !== null &&
      (typeof artifactId !== "string" || !canonicalUuid.test(artifactId))) ||
    (submissionStatus !== undefined &&
      (deliveryStatus !== "submitted" ||
        ![
          "pending_review",
          "accepted",
          "changes_requested",
          "withdrawn",
        ].includes(String(submissionStatus)))) ||
    !validAgentRunDeliveryState(
      resultStatus as AgentRunStatus,
      deliveryStatus as AgentRunOutputDeliveryStatus,
      deliveryErrorCode as string | null,
      submissionId as string | null,
      artifactId as string | null,
    )
  )
    return false;
  if (hasStartGate !== Object.hasOwn(result, "start_gate")) return false;
  if (hasStartGate) {
    try {
      const parsedGate = agentRunStartGateFromRecord(
        result.start_gate,
        String(resultStatus),
        String(deliveryStatus),
      );
      if (
        !parsedGate ||
        parsedGate.predecessorRunId !== changes.start_after_run_id ||
        parsedGate.require !== changes.start_after
      )
        return false;
    } catch {
      return false;
    }
  }
  const submissionRoute =
    deliveryStatus === "submitted"
      ? taskSubmissionHref(task.id as string, submissionId as string)
      : null;
  return (
    (value.route === runRoute ||
      (submissionRoute !== null && value.route === submissionRoute)) &&
    exactKeys(result, [
      "id",
      "task_id",
      "status",
      "attempt",
      "provider_id",
      "model",
      "output_delivery_status",
      "output_delivery_error_code",
      "submission_id",
      "artifact_id",
      ...(submissionStatus === undefined ? [] : ["submission_status"]),
      ...(hasRestart ? ["restart_of_run_id"] : []),
      ...(hasStartGate ? ["start_gate"] : []),
    ]) &&
    result.id === runId &&
    result.task_id === task.id &&
    [
      "queued",
      "running",
      "succeeded",
      "failed",
      "cancelled",
      "interrupted",
    ].includes(String(result.status)) &&
    result.attempt === snapshot.attempt &&
    result.provider_id === provider.id &&
    result.model === provider.model
  );
}
/** Reuse the full frozen-start schema for native previews, without creating a
 * proposal. Native file references are real IDs, unlike AI grant-local tokens. */
export function parseNativeAgentRunStartPreview(
  value: unknown,
): AiAgentRunStartPreview {
  if (
    !object(value) ||
    !object(value.task) ||
    !object(value.provider) ||
    !validAgentRunRestartSource(value.restart)
  )
    return invalid();
  const task = value.task,
    provider = value.provider;
  const changes = {
    provider_id: provider.id,
    expected_provider_version: provider.version,
    expected_provider_config_version: provider.config_version,
    restart_of_run_id: value.restart.run_id,
    ...(Array.isArray(value.input_files)
      ? {
          input_file_candidate_ids: value.input_files.map((file) =>
            object(file) ? file.id : null,
          ),
        }
      : {}),
    ...(object(value.output_contract)
      ? { output_kind: value.output_contract.type }
      : {}),
    ...(validAgentRunReworkContext(value.rework_context)
      ? {
          rework_submission_id: value.rework_context.submission_id,
          rework_artifact_ids: value.rework_context.artifacts.map(
            (item) => item.id,
          ),
        }
      : {}),
  };
  const envelope = {
    id: "",
    generation_id: "",
    fingerprint: "",
    created_at: "",
    decided_at: null,
    action: {
      action: "agent_run.start",
      task_id: task.id,
      expected_version: task.version,
      changes,
    },
    preview: {
      label: task.title,
      before: {},
      after: {},
      agent_run_start: value,
    },
    status: "pending",
    can_confirm: false,
    result_id: null,
    result_version: null,
    route: agentRunHref(String(task.id), value.restart.run_id),
  };
  if (!validAgentRunStartProposal(envelope, true)) return invalid();
  return value as unknown as AiAgentRunStartPreview;
}
function validAgentRunCancelProposal(value: Record<string, unknown>): boolean {
  const a = value.action,
    p = value.preview;
  if (!object(a) || !object(p)) return false;
  const recovery = a.action === "agent_run.recover_output";
  const previewKey = recovery ? "agent_run_recovery" : "agent_run_cancel";
  const s = p[previewKey];
  if (!object(s) || (!recovery && a.action !== "agent_run.cancel"))
    return false;
  if (
    recovery &&
    (s.status !== "running" ||
      !["text", "file", "files"].includes(String(s.output_kind)) ||
      typeof s.output_bytes !== "number" ||
      !Number.isSafeInteger(s.output_bytes) ||
      s.output_bytes < 1 ||
      s.output_bytes > 65_536 ||
      typeof s.output_sha256 !== "string" ||
      !/^[a-f0-9]{64}$/.test(s.output_sha256))
  )
    return false;
  const confirmed = value.status === "confirmed";
  if (
    !exactKeys(value, [
      "id",
      "generation_id",
      "fingerprint",
      "action",
      "preview",
      "status",
      "can_confirm",
      "result_id",
      "result_version",
      "route",
      "created_at",
      "decided_at",
      ...(value.agent_run_result !== undefined ? ["agent_run_result"] : []),
    ]) ||
    !exactKeys(a, [
      "action",
      "task_id",
      "agent_run_id",
      "expected_version",
      "changes",
    ]) ||
    !exactKeys(p, ["label", "before", "after", previewKey]) ||
    !object(a.changes) ||
    Object.keys(a.changes).length !== 0 ||
    !object(p.before) ||
    Object.keys(p.before).length !== 0 ||
    !object(p.after) ||
    Object.keys(p.after).length !== 0 ||
    !exactKeys(s, [
      "run_id",
      "task_id",
      "task_version",
      "status",
      "attempt",
      "provider_id",
      "model",
      ...(recovery ? ["output_kind", "output_bytes", "output_sha256"] : []),
    ]) ||
    ![s.run_id, s.task_id, s.provider_id].every(
      (id) => typeof id === "string" && canonicalUuid.test(id),
    ) ||
    a.task_id !== s.task_id ||
    a.agent_run_id !== s.run_id ||
    a.expected_version !== s.task_version ||
    !positiveIntegerValue(s.task_version) ||
    !positiveIntegerValue(s.attempt) ||
    !["queued", "running"].includes(String(s.status)) ||
    typeof s.model !== "string" ||
    !s.model ||
    (confirmed && value.can_confirm !== false)
  )
    return false;
  const route = agentRunHref(String(s.task_id), String(s.run_id));
  if (!confirmed)
    return (
      value.result_id === null &&
      value.result_version === null &&
      value.agent_run_result === undefined &&
      value.route === route
    );
  if (value.result_id !== s.run_id || value.result_version !== 1) return false;
  const result = value.agent_run_result;
  if (result === undefined) return value.route === "";
  const hasRestartResult =
    object(result) && Object.hasOwn(result, "restart_of_run_id");
  const hasSubmissionStatus =
    object(result) && Object.hasOwn(result, "submission_status");
  return (
    object(result) &&
    (!hasRestartResult ||
      (typeof result.restart_of_run_id === "string" &&
        canonicalUuid.test(result.restart_of_run_id) &&
        result.restart_of_run_id !== result.id)) &&
    exactKeys(result, [
      "id",
      "task_id",
      "status",
      "attempt",
      "provider_id",
      "model",
      "output_delivery_status",
      "output_delivery_error_code",
      "submission_id",
      "artifact_id",
      ...(hasSubmissionStatus ? ["submission_status"] : []),
      ...(hasRestartResult ? ["restart_of_run_id"] : []),
    ]) &&
    result.id === s.run_id &&
    result.task_id === s.task_id &&
    result.attempt === s.attempt &&
    result.provider_id === s.provider_id &&
    result.model === s.model &&
    (recovery
      ? validAgentRecoveryReceipt(result, value.route, String(s.task_id), route)
      : value.route === route &&
        ["running", "cancelled"].includes(String(result.status)) &&
        result.output_delivery_status === "not_ready" &&
        result.output_delivery_error_code === null &&
        result.submission_id === null &&
        result.artifact_id === null)
  );
}

function validAgentRunCancelManyProposal(
  value: Record<string, unknown>,
): boolean {
  const action = value.action;
  const preview = value.preview;
  if (!object(action) || !object(preview)) return false;
  const batch = preview.agent_run_cancel_many;
  const changes = action.changes;
  if (
    action.action !== "agent_run.cancel_many" ||
    !exactKeys(action, ["action", "changes"]) ||
    !object(changes) ||
    !exactKeys(changes, ["items"]) ||
    !Array.isArray(changes.items) ||
    changes.items.length < 1 ||
    changes.items.length > 8 ||
    !exactKeys(preview, [
      "label",
      "before",
      "after",
      "agent_run_cancel_many",
    ]) ||
    !object(preview.before) ||
    !object(preview.after) ||
    !exactKeys(preview.before, ["active_runs"]) ||
    !exactKeys(preview.after, ["cancel_requested_runs"]) ||
    !object(batch) ||
    !exactKeys(batch, ["count", "items"]) ||
    !positiveIntegerValue(batch.count) ||
    batch.count !== changes.items.length ||
    batch.count !== preview.before.active_runs ||
    batch.count !== preview.after.cancel_requested_runs ||
    !Array.isArray(batch.items) ||
    batch.items.length !== batch.count
  )
    return false;
  const runIds = new Set<string>();
  const taskIds = new Set<string>();
  for (let index = 0; index < batch.items.length; index += 1) {
    const requested = changes.items[index];
    const item = batch.items[index];
    if (
      !object(requested) ||
      !object(item) ||
      !exactKeys(requested, [
        "agent_run_id",
        "task_id",
        "expected_task_version",
      ]) ||
      !exactKeys(item, [
        "run_id",
        "task_id",
        "task_title",
        "task_version",
        "status",
        "attempt",
        "provider_id",
        "model",
      ]) ||
      ![item.run_id, item.task_id, item.provider_id].every(
        (id) => typeof id === "string" && canonicalUuid.test(id),
      ) ||
      requested.agent_run_id !== item.run_id ||
      requested.task_id !== item.task_id ||
      requested.expected_task_version !== item.task_version ||
      !positiveIntegerValue(item.task_version) ||
      !positiveIntegerValue(item.attempt) ||
      typeof item.task_title !== "string" ||
      item.task_title.length < 1 ||
      typeof item.model !== "string" ||
      item.model.length < 1 ||
      !["queued", "running"].includes(String(item.status)) ||
      runIds.has(String(item.run_id)) ||
      taskIds.has(String(item.task_id))
    )
      return false;
    runIds.add(String(item.run_id));
    taskIds.add(String(item.task_id));
  }
  const confirmed = value.status === "confirmed";
  const hasResult = value.agent_run_cancel_many_result !== undefined;
  if (
    !exactKeys(value, [
      "id",
      "generation_id",
      "fingerprint",
      "action",
      "preview",
      "status",
      "can_confirm",
      "result_id",
      "result_version",
      "route",
      "created_at",
      "decided_at",
      ...(hasResult ? ["agent_run_cancel_many_result"] : []),
    ]) ||
    value.route !== "/ai?workspace=agents"
  )
    return false;
  if (!confirmed)
    return (
      value.result_id === null && value.result_version === null && !hasResult
    );
  if (
    value.can_confirm !== false ||
    value.result_id !== value.id ||
    value.result_version !== 1 ||
    !hasResult
  )
    return false;
  const result = value.agent_run_cancel_many_result;
  if (
    !object(result) ||
    !exactKeys(result, ["count", "items"]) ||
    result.count !== batch.count ||
    !Array.isArray(result.items) ||
    result.items.length !== batch.count
  )
    return false;
  const previewItems = batch.items as Array<Record<string, unknown>>;
  return result.items.every((entry, index) => {
    const expected = previewItems[index];
    return (
      object(entry) &&
      exactKeys(entry, ["run_id", "task_id", "status"]) &&
      entry.run_id === expected.run_id &&
      entry.task_id === expected.task_id &&
      ["running", "cancelled"].includes(String(entry.status))
    );
  });
}

function validAgentRecoveryReceipt(
  result: Record<string, unknown>,
  route: unknown,
  taskId: string,
  runRoute: string,
): boolean {
  const delivery = result.output_delivery_status;
  if (
    result.status !== "succeeded" ||
    !["submitted", "retained"].includes(String(delivery))
  )
    return false;
  if (
    result.output_delivery_error_code !== null &&
    typeof result.output_delivery_error_code !== "string"
  )
    return false;
  if (
    result.submission_status !== undefined &&
    (delivery !== "submitted" ||
      ![
        "pending_review",
        "accepted",
        "changes_requested",
        "withdrawn",
      ].includes(String(result.submission_status)))
  )
    return false;
  for (const id of [result.submission_id, result.artifact_id]) {
    if (id !== null && (typeof id !== "string" || !canonicalUuid.test(id)))
      return false;
  }
  return (
    validAgentRunDeliveryState(
      "succeeded",
      delivery as AgentRunOutputDeliveryStatus,
      result.output_delivery_error_code as string | null,
      result.submission_id as string | null,
      result.artifact_id as string | null,
    ) &&
    route ===
      (delivery === "submitted"
        ? taskSubmissionHref(taskId, result.submission_id as string)
        : runRoute)
  );
}

const taskViewDefinitionKeys = [
  "q",
  "status",
  "priority",
  "kind",
  "project_id",
  "client_id",
  "tag_ids",
  "planned_date",
  "planned_from",
  "planned_to",
  "due_from",
  "due_to",
  "sort",
];

function validTaskViewDefinition(value: unknown): boolean {
  if (!object(value)) return false;
  if (Object.keys(value).some((key) => !taskViewDefinitionKeys.includes(key)))
    return false;
  for (const key of ["q", "status", "priority", "kind", "sort"]) {
    if (value[key] !== undefined && typeof value[key] !== "string")
      return false;
  }
  if (typeof value.q === "string" && Array.from(value.q).length > 200)
    return false;
  for (const key of ["project_id", "client_id"]) {
    if (value[key] === undefined || value[key] === "") continue;
    if (typeof value[key] !== "string" || !canonicalUuid.test(value[key]))
      return false;
  }
  if (value.tag_ids !== undefined) {
    if (
      !Array.isArray(value.tag_ids) ||
      value.tag_ids.length > 20 ||
      new Set(value.tag_ids).size !== value.tag_ids.length ||
      value.tag_ids.some(
        (id) => typeof id !== "string" || !canonicalUuid.test(id),
      )
    )
      return false;
  }
  const dateKeys = [
    "planned_date",
    "planned_from",
    "planned_to",
    "due_from",
    "due_to",
  ];
  for (const key of dateKeys) {
    // The server always emits every definition field, so an empty string means
    // "no filter" rather than a malformed date.
    if (value[key] === "") continue;
    if (
      value[key] !== undefined &&
      (typeof value[key] !== "string" ||
        !/^\d{4}-\d{2}-\d{2}$/.test(value[key] as string))
    )
      return false;
  }
  if (
    typeof value.planned_date === "string" &&
    value.planned_date !== "" &&
    ((typeof value.planned_from === "string" && value.planned_from !== "") ||
      (typeof value.planned_to === "string" && value.planned_to !== ""))
  )
    return false;
  if (
    typeof value.planned_from === "string" &&
    typeof value.planned_to === "string" &&
    value.planned_from !== "" &&
    value.planned_to !== "" &&
    value.planned_from > value.planned_to
  )
    return false;
  if (
    typeof value.due_from === "string" &&
    typeof value.due_to === "string" &&
    value.due_from !== "" &&
    value.due_to !== "" &&
    value.due_from > value.due_to
  )
    return false;
  return true;
}

// Saved views are local filter presets. The card must prove the server froze
// the same identity/version and never smuggled in a task write.
function validTaskViewProposal(value: Record<string, unknown>): boolean {
  const action = value.action;
  const preview = value.preview;
  if (!object(action) || !object(preview)) return false;
  const name = String(action.action);
  if (
    !["task_view.create", "task_view.update", "task_view.delete"].includes(name)
  )
    return false;
  const view = preview.task_view;
  if (!object(view)) return false;
  const changes = action.changes;
  const before = preview.before;
  const after = preview.after;
  if (!object(changes) || !object(before) || !object(after)) return false;
  if (
    typeof view.name !== "string" ||
    view.name.trim().length === 0 ||
    Array.from(view.name).length > 80 ||
    !validTaskViewDefinition(view.definition)
  )
    return false;
  if (view.project_name !== undefined && typeof view.project_name !== "string")
    return false;
  if (view.client_name !== undefined && typeof view.client_name !== "string")
    return false;
  if (
    view.tag_names !== undefined &&
    (!Array.isArray(view.tag_names) ||
      view.tag_names.some((tag) => typeof tag !== "string"))
  )
    return false;
  if (name === "task_view.create") {
    if (
      action.task_saved_view_id !== undefined ||
      action.expected_version !== undefined ||
      Object.keys(changes).some(
        (key) => key !== "name" && key !== "definition",
      ) ||
      typeof changes.name !== "string" ||
      changes.name.trim() !== view.name ||
      !validTaskViewDefinition(changes.definition) ||
      JSON.stringify(changes.definition) !== JSON.stringify(view.definition) ||
      Object.keys(before).length !== 0 ||
      after.name !== view.name ||
      view.id !== undefined ||
      view.deleted === true ||
      view.next_version !== 1
    )
      return false;
    if (value.result_id !== null && typeof value.result_id !== "string")
      return false;
    if (
      value.result_id !== null &&
      !canonicalUuid.test(String(value.result_id))
    )
      return false;
    if (value.result_version !== null && value.result_version !== 1)
      return false;
    if (
      value.status === "confirmed" &&
      (value.result_id === null || value.result_version !== 1)
    )
      return false;
    return true;
  }
  if (
    typeof action.task_saved_view_id !== "string" ||
    !canonicalUuid.test(action.task_saved_view_id) ||
    !isPositiveInteger(action.expected_version) ||
    view.id !== action.task_saved_view_id ||
    view.version !== action.expected_version ||
    typeof before.name !== "string" ||
    before.name.trim().length === 0 ||
    Object.keys(before).length !== 1
  )
    return false;
  if (name === "task_view.delete") {
    if (
      Object.keys(changes).length !== 0 ||
      view.deleted !== true ||
      Object.keys(after).length !== 0
    )
      return false;
  } else {
    if (
      Object.keys(changes).length === 0 ||
      Object.keys(changes).some(
        (key) => key !== "name" && key !== "definition",
      ) ||
      (changes.name !== undefined &&
        (typeof changes.name !== "string" ||
          changes.name.trim() !== view.name)) ||
      (changes.definition !== undefined &&
        (!validTaskViewDefinition(changes.definition) ||
          JSON.stringify(changes.definition) !==
            JSON.stringify(view.definition))) ||
      after.name !== view.name ||
      Object.keys(after).length !== 1 ||
      view.deleted === true ||
      view.next_version !== view.version + 1
    )
      return false;
  }
  if (value.result_id !== null && value.result_id !== action.task_saved_view_id)
    return false;
  if (
    value.result_version !== null &&
    value.result_version !== action.expected_version &&
    value.result_version !== (action.expected_version as number) + 1
  )
    return false;
  if (
    value.status === "confirmed" &&
    (value.result_id === null || value.result_version === null)
  )
    return false;
  return true;
}

function validTaskOrderProposal(value: Record<string, unknown>): boolean {
  const action = value.action;
  const preview = value.preview;
  if (!object(action) || !object(preview)) return false;
  const changes = action.changes;
  const order = preview.task_order;
  if (
    !exactKeys(action, ["action", "task_id", "expected_version", "changes"]) ||
    !object(changes) ||
    !exactKeys(changes, [
      "anchor_task_id",
      "expected_anchor_version",
      "placement",
    ]) ||
    !exactKeys(preview, ["label", "before", "after", "task_order"]) ||
    !object(order) ||
    !exactKeys(order, [
      "task_id",
      "title",
      "anchor_task_id",
      "anchor_title",
      "placement",
      "planned_date",
      "group_count",
      "active_count",
      "before_position",
      "after_position",
      "group_fingerprint",
    ]) ||
    !object(preview.before) ||
    Object.keys(preview.before).length !== 0 ||
    !object(preview.after) ||
    Object.keys(preview.after).length !== 0
  )
    return false;
  if (
    typeof action.task_id !== "string" ||
    !canonicalUuid.test(action.task_id) ||
    !positiveIntegerValue(action.expected_version) ||
    typeof changes.anchor_task_id !== "string" ||
    !canonicalUuid.test(changes.anchor_task_id) ||
    changes.anchor_task_id === action.task_id ||
    !positiveIntegerValue(changes.expected_anchor_version) ||
    (changes.placement !== "before" && changes.placement !== "after") ||
    order.task_id !== action.task_id ||
    order.anchor_task_id !== changes.anchor_task_id ||
    order.placement !== changes.placement ||
    typeof order.title !== "string" ||
    order.title.trim().length < 2 ||
    Array.from(order.title).length > 200 ||
    typeof order.anchor_title !== "string" ||
    order.anchor_title.trim().length < 2 ||
    Array.from(order.anchor_title).length > 200 ||
    preview.label !== order.title ||
    !(
      order.planned_date === null ||
      (typeof order.planned_date === "string" &&
        /^\d{4}-\d{2}-\d{2}$/.test(order.planned_date))
    ) ||
    !positiveIntegerValue(order.group_count) ||
    order.group_count > 1000 ||
    !positiveIntegerValue(order.active_count) ||
    order.active_count < 2 ||
    order.active_count > order.group_count ||
    !positiveIntegerValue(order.before_position) ||
    !positiveIntegerValue(order.after_position) ||
    order.before_position > order.active_count ||
    order.after_position > order.active_count ||
    order.before_position === order.after_position ||
    typeof order.group_fingerprint !== "string" ||
    !/^[a-f0-9]{64}$/.test(order.group_fingerprint)
  )
    return false;
  if (value.result_id !== null && value.result_id !== action.task_id)
    return false;
  if (
    value.result_version !== null &&
    value.result_version !== action.expected_version &&
    value.result_version !== (action.expected_version as number) + 1
  )
    return false;
  if (
    value.status === "confirmed" &&
    (value.result_id !== action.task_id || value.result_version === null)
  )
    return false;
  return true;
}

function validRoadmapOrderProposal(value: Record<string, unknown>): boolean {
  const action = value.action;
  const preview = value.preview;
  if (!object(action) || !object(preview)) return false;
  const changes = action.changes;
  const order = preview.roadmap_order;
  if (
    !exactKeys(action, [
      "action",
      "roadmap_milestone_id",
      "expected_version",
      "changes",
    ]) ||
    !object(changes) ||
    !exactKeys(changes, [
      "anchor_milestone_id",
      "expected_anchor_version",
      "placement",
    ]) ||
    !exactKeys(preview, ["label", "before", "after", "roadmap_order"]) ||
    !object(order) ||
    !exactKeys(order, [
      "milestone_id",
      "title",
      "anchor_id",
      "anchor_title",
      "placement",
      "year",
      "quarter",
      "group_count",
      "before_position",
      "after_position",
      "group_fingerprint",
    ]) ||
    !object(preview.before) ||
    Object.keys(preview.before).length !== 0 ||
    !object(preview.after) ||
    Object.keys(preview.after).length !== 0
  )
    return false;
  if (
    typeof action.roadmap_milestone_id !== "string" ||
    !canonicalUuid.test(action.roadmap_milestone_id) ||
    !positiveIntegerValue(action.expected_version) ||
    typeof changes.anchor_milestone_id !== "string" ||
    !canonicalUuid.test(changes.anchor_milestone_id) ||
    changes.anchor_milestone_id === action.roadmap_milestone_id ||
    !positiveIntegerValue(changes.expected_anchor_version) ||
    (changes.placement !== "before" && changes.placement !== "after") ||
    order.milestone_id !== action.roadmap_milestone_id ||
    order.anchor_id !== changes.anchor_milestone_id ||
    order.placement !== changes.placement ||
    typeof order.title !== "string" ||
    order.title.trim().length < 2 ||
    Array.from(order.title).length > 200 ||
    typeof order.anchor_title !== "string" ||
    order.anchor_title.trim().length < 2 ||
    Array.from(order.anchor_title).length > 200 ||
    preview.label !== order.title ||
    !positiveIntegerValue(order.year) ||
    order.year < 2000 ||
    order.year > 2100 ||
    !positiveIntegerValue(order.quarter) ||
    order.quarter > 4 ||
    !positiveIntegerValue(order.group_count) ||
    order.group_count < 2 ||
    order.group_count > 100 ||
    !positiveIntegerValue(order.before_position) ||
    order.before_position > order.group_count ||
    !positiveIntegerValue(order.after_position) ||
    order.after_position > order.group_count ||
    order.before_position === order.after_position ||
    typeof order.group_fingerprint !== "string" ||
    !/^[a-f0-9]{64}$/.test(order.group_fingerprint)
  )
    return false;
  if (
    value.result_id !== null &&
    value.result_id !== action.roadmap_milestone_id
  )
    return false;
  if (
    value.result_version !== null &&
    value.result_version !== action.expected_version &&
    value.result_version !== (action.expected_version as number) + 1
  )
    return false;
  return (
    value.status !== "confirmed" ||
    (value.result_id === action.roadmap_milestone_id &&
      value.result_version !== null)
  );
}

function validTaskBatchCreateProposal(value: Record<string, unknown>): boolean {
  const action = value.action;
  const preview = value.preview;
  if (!object(action) || !object(preview) || !object(action.changes))
    return false;
  const changes = action.changes;
  const batch = preview.task_batch_create;
  if (
    !exactKeys(action, [
      "action",
      "project_id",
      "expected_version",
      "changes",
    ]) ||
    !canonicalUuid.test(String(action.project_id)) ||
    !positiveIntegerValue(action.expected_version) ||
    !exactKeys(changes, ["drafts"]) ||
    !Array.isArray(changes.drafts) ||
    changes.drafts.length < 1 ||
    changes.drafts.length > 20 ||
    !object(batch) ||
    batch.project_id !== action.project_id ||
    batch.project_version !== action.expected_version ||
    typeof batch.project_name !== "string" ||
    !batch.project_name.trim() ||
    batch.count !== changes.drafts.length ||
    !Array.isArray(batch.items) ||
    batch.items.length !== changes.drafts.length ||
    !object(preview.before) ||
    Object.keys(preview.before).length !== 0 ||
    !object(preview.after) ||
    !exactKeys(preview.after, ["count"]) ||
    preview.after.count !== batch.count ||
    value.route !== `/projects/${action.project_id}`
  )
    return false;
  const allowedDraft = new Set([
    "key",
    "parent_key",
    "title",
    "description",
    "kind",
    "priority",
    "planned_date",
    "due_date",
    "estimated_minutes",
    "completion_criteria",
    "review_policy",
    "tag_ids",
    "assignee_actor_id",
  ]);
  const known = new Set<string>();
  for (const [index, rawDraft] of changes.drafts.entries()) {
    const item = batch.items[index];
    if (!object(rawDraft) || !object(item)) return false;
    const key = rawDraft.key;
    if (
      Object.keys(rawDraft).some((field) => !allowedDraft.has(field)) ||
      typeof key !== "string" ||
      !/^[a-zA-Z][a-zA-Z0-9_-]{0,49}$/.test(key) ||
      known.has(key) ||
      (rawDraft.parent_key !== undefined &&
        (typeof rawDraft.parent_key !== "string" ||
          !known.has(rawDraft.parent_key))) ||
      typeof rawDraft.title !== "string" ||
      !exactKeys(item, [
        "key",
        "title",
        "description",
        "kind",
        "priority",
        "planned_date",
        "due_date",
        "estimated_minutes",
        "completion_criteria",
        "review_policy",
        "tag_ids",
        "tag_names",
        ...(rawDraft.parent_key ? ["parent_key"] : []),
        ...(rawDraft.assignee_actor_id ? ["assignments"] : []),
      ]) ||
      item.key !== key ||
      item.parent_key !== rawDraft.parent_key ||
      item.title !== rawDraft.title.trim() ||
      typeof item.description !== "string" ||
      typeof item.completion_criteria !== "string" ||
      !["work", "review", "followup", "reminder"].includes(String(item.kind)) ||
      !["P0", "P1", "P2", "P3"].includes(String(item.priority)) ||
      !["none", "manual"].includes(String(item.review_policy)) ||
      (item.planned_date !== null &&
        (typeof item.planned_date !== "string" ||
          !/^\d{4}-\d{2}-\d{2}$/.test(item.planned_date))) ||
      (item.due_date !== null && typeof item.due_date !== "string") ||
      (item.estimated_minutes !== null &&
        (typeof item.estimated_minutes !== "number" ||
          !Number.isSafeInteger(item.estimated_minutes) ||
          item.estimated_minutes < 0)) ||
      !Array.isArray(item.tag_ids) ||
      item.tag_ids.some(
        (id) => typeof id !== "string" || !canonicalUuid.test(id),
      ) ||
      !Array.isArray(item.tag_names) ||
      item.tag_names.some((name) => typeof name !== "string")
    )
      return false;
    if (rawDraft.assignee_actor_id !== undefined) {
      if (
        typeof rawDraft.assignee_actor_id !== "string" ||
        !canonicalUuid.test(rawDraft.assignee_actor_id) ||
        !Array.isArray(item.assignments) ||
        item.assignments.length !== (item.review_policy === "manual" ? 2 : 1)
      )
        return false;
      for (const [assignmentIndex, assignment] of item.assignments.entries()) {
        if (
          !object(assignment) ||
          !exactKeys(assignment, [
            "role",
            "actor_id",
            "actor_name",
            "actor_type",
            "actor_status",
            "actor_version",
          ]) ||
          typeof assignment.actor_id !== "string" ||
          !canonicalUuid.test(assignment.actor_id) ||
          typeof assignment.actor_name !== "string" ||
          !assignment.actor_name.trim() ||
          assignment.actor_status !== "active" ||
          !positiveIntegerValue(assignment.actor_version)
        )
          return false;
        if (assignmentIndex === 0) {
          if (
            assignment.role !== "assignee" ||
            assignment.actor_id !== rawDraft.assignee_actor_id ||
            !["owner", "person", "agent"].includes(
              String(assignment.actor_type),
            )
          )
            return false;
        } else if (
          assignment.role !== "reviewer" ||
          assignment.actor_type !== "owner" ||
          assignment.actor_id !== "00000000-0000-5000-8000-000000000001"
        )
          return false;
      }
    }
    const tagIDs = rawDraft.tag_ids === undefined ? [] : rawDraft.tag_ids;
    if (
      item.description !== (rawDraft.description ?? "") ||
      item.completion_criteria !== (rawDraft.completion_criteria ?? "") ||
      item.kind !== (rawDraft.kind ?? "work") ||
      item.priority !== (rawDraft.priority ?? "P2") ||
      item.review_policy !== (rawDraft.review_policy ?? "none") ||
      item.estimated_minutes !== (rawDraft.estimated_minutes ?? null) ||
      item.planned_date !==
        (typeof rawDraft.planned_date === "string"
          ? rawDraft.planned_date.trim()
          : null) ||
      (rawDraft.due_date === undefined
        ? item.due_date !== null
        : typeof rawDraft.due_date !== "string" ||
          typeof item.due_date !== "string" ||
          !Number.isFinite(Date.parse(rawDraft.due_date)) ||
          Date.parse(rawDraft.due_date) !== Date.parse(item.due_date)) ||
      !Array.isArray(tagIDs) ||
      tagIDs.some(
        (id) => typeof id !== "string" || !canonicalUuid.test(id.trim()),
      ) ||
      JSON.stringify(item.tag_ids) !==
        JSON.stringify(
          [...new Set(tagIDs.map((id: string) => id.trim()))].sort(),
        ) ||
      item.tag_names.length !== item.tag_ids.length
    )
      return false;
    known.add(key);
  }
  if (value.status === "confirmed") {
    const result = value.task_batch_create_result;
    if (
      value.result_id !== value.id ||
      value.result_version !== 1 ||
      !object(result) ||
      result.project_id !== action.project_id ||
      result.count !== batch.count ||
      !Array.isArray(result.items) ||
      result.items.length !== batch.count
    )
      return false;
    for (const [index, item] of result.items.entries()) {
      const draft = batch.items[index];
      if (
        !object(item) ||
        !exactKeys(item, ["key", "id", "title", "route"]) ||
        item.key !== draft.key ||
        item.title !== draft.title ||
        typeof item.id !== "string" ||
        !canonicalUuid.test(item.id) ||
        item.route !== `/tasks/${item.id}`
      )
        return false;
    }
  } else if (
    value.task_batch_create_result !== undefined ||
    value.result_id !== null ||
    value.result_version !== null
  ) {
    return false;
  }
  return true;
}

function validTaskBatchActor(
  value: unknown,
  role: "assignee" | "reviewer",
  activeOnly: boolean,
): value is Record<string, unknown> {
  return (
    object(value) &&
    exactKeys(value, [
      "role",
      "actor_id",
      "actor_name",
      "actor_type",
      "actor_status",
      "actor_version",
    ]) &&
    value.role === role &&
    typeof value.actor_id === "string" &&
    canonicalUuid.test(value.actor_id) &&
    typeof value.actor_name === "string" &&
    Boolean(value.actor_name.trim()) &&
    ["owner", "person", "agent"].includes(String(value.actor_type)) &&
    (role !== "reviewer" || value.actor_type === "owner") &&
    ["active", "inactive"].includes(String(value.actor_status)) &&
    (!activeOnly || value.actor_status === "active") &&
    positiveIntegerValue(value.actor_version)
  );
}

function validTaskBatchUtcInstant(value: unknown): value is string {
  if (
    typeof value !== "string" ||
    !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/.test(value)
  )
    return false;
  const parsed = new Date(value);
  return (
    !Number.isNaN(parsed.getTime()) &&
    parsed.toISOString().slice(0, 19) === value.slice(0, 19)
  );
}

function validTaskBatchProposal(value: Record<string, unknown>): boolean {
  const action = value.action;
  const preview = value.preview;
  if (!object(action) || !object(preview)) return false;
  if (action.action !== "task.batch_update") return true;
  const changes = action.changes;
  const batch = preview.task_batch;
  const before = preview.before;
  const after = preview.after;
  if (!object(changes) || !object(batch) || !object(before) || !object(after))
    return false;
  const batchActions = [
    "set_priority",
    "set_due_date",
    "set_project",
    "set_planned_date",
    "add_tags",
    "remove_tags",
    "start",
    "block",
    "unblock",
    "complete",
    "cancel",
    "reopen",
    "set_assignee",
    "clear_assignee",
    "set_reviewer",
    "clear_reviewer",
  ];
  const name = changes.batch_action;
  if (typeof name !== "string" || !batchActions.includes(name)) return false;
  const role = name.endsWith("_assignee")
    ? "assignee"
    : name.endsWith("_reviewer")
      ? "reviewer"
      : null;
  const settingResponsibility = name.startsWith("set_") && role !== null;
  const items = changes.items;
  const versions = changes.expected_versions;
  if (
    !Array.isArray(items) ||
    items.length === 0 ||
    items.length > 20 ||
    new Set(items).size !== items.length ||
    items.some((id) => typeof id !== "string" || !canonicalUuid.test(id)) ||
    !Array.isArray(versions) ||
    versions.length !== items.length ||
    versions.some((version) => !positiveIntegerValue(version))
  )
    return false;
  const allowed = new Set(["batch_action", "items", "expected_versions"]);
  if (name === "set_priority") allowed.add("priority");
  if (name === "set_due_date") allowed.add("due_date");
  if (name === "set_project") allowed.add("project_id");
  if (name === "set_planned_date") allowed.add("planned_date");
  if (name === "add_tags" || name === "remove_tags") allowed.add("tag_ids");
  if (name === "block" || name === "cancel") allowed.add("reason");
  if (role !== null) allowed.add("reason");
  if (settingResponsibility) allowed.add("actor_id");
  if (Object.keys(changes).some((key) => !allowed.has(key))) return false;
  if (
    name === "set_priority" &&
    !["P0", "P1", "P2", "P3"].includes(String(changes.priority))
  )
    return false;
  if (
    name === "set_due_date" &&
    !(
      Object.hasOwn(changes, "due_date") &&
      (changes.due_date === null || validTaskBatchUtcInstant(changes.due_date))
    )
  )
    return false;
  if (
    name === "set_project" &&
    !(
      Object.hasOwn(changes, "project_id") &&
      (changes.project_id === null ||
        (typeof changes.project_id === "string" &&
          canonicalUuid.test(changes.project_id)))
    )
  )
    return false;
  if (
    name === "set_planned_date" &&
    !(
      Object.hasOwn(changes, "planned_date") &&
      (changes.planned_date === null ||
        (typeof changes.planned_date === "string" &&
          /^\d{4}-\d{2}-\d{2}$/.test(changes.planned_date)))
    )
  )
    return false;
  if (name === "add_tags" || name === "remove_tags") {
    const tagIDs = changes.tag_ids;
    if (
      !Array.isArray(tagIDs) ||
      tagIDs.length === 0 ||
      tagIDs.length > 20 ||
      new Set(tagIDs).size !== tagIDs.length ||
      tagIDs.some((id) => typeof id !== "string" || !canonicalUuid.test(id))
    )
      return false;
  }
  if (
    settingResponsibility &&
    (typeof changes.actor_id !== "string" ||
      !canonicalUuid.test(changes.actor_id))
  )
    return false;
  if (name === "block" || name === "cancel" || role !== null) {
    if (
      typeof changes.reason !== "string" ||
      Array.from(changes.reason.trim()).length < (role !== null ? 1 : 2) ||
      Array.from(changes.reason.trim()).length > 1000 ||
      batch.reason !== changes.reason.trim()
    )
      return false;
  } else if (batch.reason !== undefined) {
    return false;
  }
  if (
    Object.keys(before).length !== 0 ||
    Object.keys(after).length !== 1 ||
    after.count !== items.length ||
    batch.action !== name ||
    batch.count !== items.length ||
    !Array.isArray(batch.items) ||
    batch.items.length !== items.length
  )
    return false;
  const expectedField =
    role !== null
      ? role
      : name === "set_priority"
        ? "priority"
        : name === "set_due_date"
          ? "due_date"
          : name === "set_project"
            ? "project"
            : name === "set_planned_date"
              ? "planned_date"
              : name === "add_tags" || name === "remove_tags"
                ? "tags"
                : "status";
  for (const [index, row] of batch.items.entries()) {
    if (
      !object(row) ||
      !exactKeys(row, [
        "task_id",
        "title",
        "version",
        "field",
        "before",
        "after",
        ...(role !== null && row.assignment_id !== undefined
          ? ["assignment_id"]
          : []),
        ...(role !== null && row.before_actor !== undefined
          ? ["before_actor"]
          : []),
        ...(role !== null && row.after_actor !== undefined
          ? ["after_actor"]
          : []),
      ]) ||
      row.task_id !== items[index] ||
      row.version !== versions[index] ||
      row.field !== expectedField ||
      typeof row.title !== "string" ||
      row.title.trim().length < 2 ||
      Array.from(row.title).length > 200 ||
      typeof row.before !== "string" ||
      typeof row.after !== "string"
    )
      return false;
    if (
      name === "set_priority" &&
      (row.after !== changes.priority ||
        !["P0", "P1", "P2", "P3"].includes(row.before))
    )
      return false;
    if (
      name === "set_due_date" &&
      (row.after !== (changes.due_date ?? "未安排") ||
        (row.before !== "未安排" && !validTaskBatchUtcInstant(row.before)))
    )
      return false;
    if (role !== null) {
      const current = row.before_actor;
      const target = row.after_actor;
      if (current === undefined) {
        if (
          row.assignment_id !== undefined ||
          row.before !== "未分派" ||
          !settingResponsibility
        )
          return false;
      } else if (
        !validTaskBatchActor(current, role, false) ||
        typeof row.assignment_id !== "string" ||
        !canonicalUuid.test(row.assignment_id) ||
        row.before !== current.actor_name
      )
        return false;
      if (settingResponsibility) {
        if (
          !validTaskBatchActor(target, role, true) ||
          target.actor_id !== changes.actor_id ||
          row.after !== target.actor_name ||
          (current !== undefined && current.actor_id === target.actor_id)
        )
          return false;
      } else if (target !== undefined || row.after !== "未分派") return false;
    }
  }
  const targetKeys = [
    "task_id",
    "project_id",
    "tag_id",
    "inbox_item_id",
    "reminder_id",
    "focus_session_id",
    "client_followup_id",
    "client_activity_id",
    "automation_rule_id",
    "automation_run_id",
    "roadmap_milestone_id",
    "content_item_id",
    "knowledge_source_id",
    "knowledge_index_job_id",
    "task_saved_view_id",
    "client_id",
    "client_actor_link_id",
    "actor_id",
    "expected_version",
  ];
  if (targetKeys.some((key) => action[key] !== undefined)) return false;
  if (value.result_id !== null && value.result_id !== value.id) return false;
  if (value.result_version !== null && value.result_version !== 1) return false;
  if (
    value.status === "confirmed" &&
    (value.result_id !== value.id || value.result_version !== 1)
  )
    return false;
  return true;
}

// Knowledge library management carries metadata only. The card must prove the
// server frozen the same source/version and never accepted document text.
const isPositiveInteger = (value: unknown): value is number =>
  typeof value === "number" && Number.isSafeInteger(value) && value > 0;

function validKnowledgeProposal(value: Record<string, unknown>): boolean {
  const action = value.action;
  const preview = value.preview;
  if (!object(action) || !object(preview)) return false;
  const name = String(action.action);
  if (
    ![
      "knowledge_source.create",
      "knowledge_source.reindex",
      "knowledge_source.delete",
      "knowledge_index_job.retry",
      "knowledge_index_job.cancel",
    ].includes(name)
  )
    return false;
  const knowledge = preview.knowledge;
  if (!object(knowledge)) return false;
  const before = preview.before;
  const after = preview.after;
  if (!object(before) || !object(after)) return false;
  const changes = action.changes;
  if (!object(changes)) return false;
  if (name === "knowledge_source.create") {
    // Creation carries the text the user supplied in this conversation; the
    // card must prove the excerpt really belongs to that exact content.
    if (
      action.knowledge_source_id !== undefined ||
      action.knowledge_index_job_id !== undefined ||
      action.expected_version !== undefined ||
      Object.keys(changes).some(
        (key) => !["name", "title", "content"].includes(key),
      ) ||
      typeof changes.name !== "string" ||
      !/\.(?:txt|md|markdown)$/i.test(changes.name) ||
      typeof changes.content !== "string" ||
      changes.content.length === 0 ||
      new TextEncoder().encode(changes.content).length > 24000 ||
      (changes.title !== undefined && typeof changes.title !== "string")
    )
      return false;
    const content = changes.content;
    const excerpt = knowledge.proposed_excerpt;
    const truncated = knowledge.proposed_excerpt_truncated;
    if (
      knowledge.name !== changes.name ||
      !["text", "markdown"].includes(String(knowledge.source_type)) ||
      knowledge.status !== "pending" ||
      knowledge.version !== 0 ||
      knowledge.next_status !== "indexing" ||
      knowledge.next_version !== 1 ||
      knowledge.new_job_operation !== "import" ||
      knowledge.chunk_count !== 0 ||
      knowledge.document_count !== 0 ||
      knowledge.content_included !== false ||
      !isPositiveInteger(knowledge.proposed_content_bytes) ||
      knowledge.proposed_content_bytes !==
        new TextEncoder().encode(content).length ||
      typeof knowledge.proposed_content_sha256 !== "string" ||
      !sha256.test(knowledge.proposed_content_sha256) ||
      typeof excerpt !== "string" ||
      typeof truncated !== "boolean" ||
      (truncated ? !content.startsWith(excerpt) : excerpt !== content) ||
      Object.keys(before).length !== 0 ||
      after.name !== knowledge.name ||
      after.source_type !== knowledge.source_type ||
      after.status !== "indexing" ||
      after.version !== 1 ||
      after.size_bytes !== knowledge.proposed_content_bytes
    )
      return false;
    if (
      value.result_id !== null &&
      !canonicalUuid.test(String(value.result_id))
    )
      return false;
    if (value.result_version !== null && value.result_version !== 1)
      return false;
    if (
      value.status === "confirmed" &&
      (value.result_id === null || value.result_version !== 1)
    )
      return false;
    return true;
  }
  const isJob = name.startsWith("knowledge_index_job.");
  const isDelete = name === "knowledge_source.delete";
  const jobTarget = action.knowledge_index_job_id;
  const sourceTarget = action.knowledge_source_id;
  if (isJob) {
    if (
      typeof jobTarget !== "string" ||
      !canonicalUuid.test(jobTarget) ||
      sourceTarget !== undefined
    )
      return false;
  } else if (
    typeof sourceTarget !== "string" ||
    !canonicalUuid.test(sourceTarget) ||
    jobTarget !== undefined
  )
    return false;
  if (!isPositiveInteger(action.expected_version)) return false;
  if (
    typeof knowledge.source_id !== "string" ||
    !canonicalUuid.test(knowledge.source_id) ||
    (isJob
      ? knowledge.knowledge_index_job_id !== jobTarget
      : sourceTarget !== knowledge.source_id)
  )
    return false;
  if (
    !nonemptyString(knowledge.name) ||
    Array.from(String(knowledge.name)).length > 255 ||
    !["text", "markdown", "pdf"].includes(String(knowledge.source_type)) ||
    !["pending", "indexing", "ready", "stale", "missing", "failed"].includes(
      String(knowledge.status),
    ) ||
    knowledge.content_included !== false ||
    !isPositiveInteger(knowledge.version) ||
    knowledge.version !== action.expected_version
  )
    return false;
  for (const key of ["chunk_count", "document_count"]) {
    const count = knowledge[key];
    if (!Number.isSafeInteger(count) || (count as number) < 0) return false;
  }
  if (
    before.status !== knowledge.status ||
    before.version !== knowledge.version ||
    before.chunks !== knowledge.chunk_count ||
    before.documents !== knowledge.document_count
  )
    return false;
  if (
    isDelete
      ? after.chunks !== 0 || after.documents !== 0
      : after.chunks !== before.chunks || after.documents !== before.documents
  )
    return false;
  if (isDelete) {
    if (
      Object.keys(changes).some((key) => key !== "reason") ||
      (changes.reason !== undefined &&
        (typeof changes.reason !== "string" ||
          changes.reason.trim().length === 0 ||
          Array.from(changes.reason).length > 500))
    )
      return false;
    if (
      knowledge.next_status !== "deleted" ||
      after.status !== "deleted" ||
      after.chunks !== 0 ||
      after.documents !== 0 ||
      knowledge.next_version !== knowledge.version + 1 ||
      after.version !== knowledge.next_version
    )
      return false;
  } else if (Object.keys(changes).length !== 0) {
    return false;
  }
  if (name === "knowledge_source.reindex") {
    if (
      knowledge.next_status !== "indexing" ||
      knowledge.new_job_operation !== "reindex" ||
      after.status !== "indexing" ||
      after.version !== knowledge.version + 1
    )
      return false;
    if (
      knowledge.document_version !== undefined &&
      !isPositiveInteger(knowledge.document_version)
    )
      return false;
  }
  if (isJob) {
    const job = knowledge.job;
    if (
      !object(job) ||
      job.id !== jobTarget ||
      !["import", "reindex"].includes(String(job.operation)) ||
      !isPositiveInteger(job.attempt) ||
      before.job_attempt !== job.attempt
    )
      return false;
    if (name === "knowledge_index_job.retry") {
      if (
        !["failed", "cancelled"].includes(String(job.status)) ||
        knowledge.next_status !== "indexing" ||
        knowledge.new_job_operation !== job.operation ||
        after.job_status !== "queued" ||
        after.version !== knowledge.version + 1
      )
        return false;
      if (job.error_code !== undefined && !nonemptyString(job.error_code))
        return false;
    } else {
      if (
        !["queued", "running"].includes(String(job.status)) ||
        !["ready", "failed"].includes(String(knowledge.next_status)) ||
        after.job_status !== "cancelled" ||
        before.job_status !== job.status ||
        after.status !== knowledge.next_status
      )
        return false;
    }
  }
  if (value.result_id !== null) {
    if (
      typeof value.result_id !== "string" ||
      !canonicalUuid.test(value.result_id)
    )
      return false;
    // Deletion and cancellation keep the addressed record identity; reindex
    // and retry receipts identify the new index attempt instead.
    if (isDelete && value.result_id !== knowledge.source_id) return false;
    if (name === "knowledge_index_job.cancel" && value.result_id !== jobTarget)
      return false;
  }
  if (value.result_version !== null && !isPositiveInteger(value.result_version))
    return false;
  if (
    value.status === "confirmed" &&
    (value.result_id === null || value.result_version === null)
  )
    return false;
  return true;
}

const delegatedAgentScopes = [
  "work",
  "clients",
  "outputs",
  "output_files",
  "actions",
  "agent_execution",
  "agent_files",
  "agent_project_files",
  "knowledge",
  "knowledge_actions",
  "finance",
  "finance_actions",
  "invoice_actions",
  "finance_exports",
] as const;

function validAgentDelegationProposal(value: Record<string, unknown>): boolean {
  const action = value.action;
  const preview = value.preview;
  if (!object(action) || !object(preview) || !object(action.changes))
    return false;
  const changes = action.changes;
  const delegation = preview.agent_delegation;
  if (
    !exactKeys(action, ["action", "changes"]) ||
    !exactKeys(changes, ["task_name", "message", "scopes"]) ||
    !object(delegation) ||
    !exactKeys(preview, ["label", "before", "after", "agent_delegation"])
  )
    return false;
  const knowledge = delegation.knowledge_sources;
  const delegationKeys = [
    "task_name",
    "message",
    "scopes",
    ...(knowledge === undefined ? [] : ["knowledge_sources"]),
    "parent_session_id",
    "parent_generation_id",
    "child_session_id",
    "provider_id",
    "provider_version",
    "provider_config_version",
    "provider_name",
    "model",
    "leaves_device",
    "depth",
    "max_depth",
    "sibling_index",
    "max_children",
  ];
  if (!exactKeys(delegation, delegationKeys)) return false;
  const taskName = changes.task_name;
  const message = changes.message;
  const scopes = changes.scopes;
  if (
    typeof taskName !== "string" ||
    !/^[a-z][a-z0-9_]{0,39}$/.test(taskName) ||
    typeof message !== "string" ||
    message !== message.trim() ||
    message.length === 0 ||
    Array.from(message).length > 4000 ||
    new TextEncoder().encode(message).byteLength > 16_384 ||
    !Array.isArray(scopes) ||
    scopes.length < 1 ||
    scopes.length > delegatedAgentScopes.length ||
    scopes.some(
      (scope) =>
        typeof scope !== "string" ||
        !delegatedAgentScopes.includes(
          scope as (typeof delegatedAgentScopes)[number],
        ),
    ) ||
    new Set(scopes).size !== scopes.length ||
    scopes.some(
      (scope, index) =>
        index > 0 &&
        delegatedAgentScopes.indexOf(scope as never) <=
          delegatedAgentScopes.indexOf(scopes[index - 1] as never),
    )
  )
    return false;
  if (
    delegation.task_name !== taskName ||
    delegation.message !== message ||
    JSON.stringify(delegation.scopes) !== JSON.stringify(scopes) ||
    preview.label !== taskName ||
    Object.keys(preview.before as Record<string, unknown>).length !== 0 ||
    Object.keys(preview.after as Record<string, unknown>).length !== 0 ||
    typeof value.generation_id !== "string" ||
    delegation.parent_generation_id !== value.generation_id
  )
    return false;
  for (const id of [
    delegation.parent_session_id,
    delegation.parent_generation_id,
    delegation.child_session_id,
    delegation.provider_id,
  ]) {
    if (typeof id !== "string" || !canonicalUuid.test(id)) return false;
  }
  if (
    !positiveIntegerValue(delegation.provider_version) ||
    !positiveIntegerValue(delegation.provider_config_version) ||
    !nonemptyString(delegation.provider_name) ||
    !nonemptyString(delegation.model) ||
    typeof delegation.leaves_device !== "boolean" ||
    delegation.depth !== 1 ||
    delegation.max_depth !== 1 ||
    !positiveIntegerValue(delegation.sibling_index) ||
    Number(delegation.sibling_index) > 4 ||
    delegation.max_children !== 4
  )
    return false;
  const hasKnowledge = scopes.includes("knowledge");
  if (hasKnowledge) {
    if (
      !Array.isArray(knowledge) ||
      knowledge.length < 1 ||
      knowledge.length > 20 ||
      knowledge.some(
        (source) =>
          !object(source) ||
          !exactKeys(source, ["source_id", "expected_source_version"]) ||
          typeof source.source_id !== "string" ||
          !canonicalUuid.test(source.source_id) ||
          !positiveIntegerValue(source.expected_source_version),
      ) ||
      new Set(knowledge.map((source) => String(source.source_id))).size !==
        knowledge.length
    )
      return false;
  } else if (knowledge !== undefined) return false;
  const confirmed = value.status === "confirmed";
  if (
    confirmed !== (value.result_id !== null) ||
    confirmed !== (value.result_version !== null) ||
    (confirmed &&
      (value.result_id !== delegation.child_session_id ||
        value.result_version !== 1 ||
        value.route !== `/ai?session=${delegation.child_session_id}`)) ||
    (!confirmed && value.route !== "")
  )
    return false;
  const result = value.agent_delegation_result;
  if (!confirmed) return result === undefined;
  return (
    object(result) &&
    exactKeys(result, [
      "session_id",
      "generation_id",
      "status",
      "error_code",
      "pending_approvals",
    ]) &&
    result.session_id === delegation.child_session_id &&
    result.generation_id === value.id &&
    ["queued", "streaming", "completed", "failed", "cancelled"].includes(
      String(result.status),
    ) &&
    Number.isSafeInteger(result.pending_approvals) &&
    Number(result.pending_approvals) >= 0 &&
    (result.error_code === null ||
      (typeof result.error_code === "string" &&
        result.error_code.length >= 1 &&
        result.error_code.length <= 100))
  );
}

function validAgentFollowupProposal(value: Record<string, unknown>): boolean {
  const action = value.action;
  const preview = value.preview;
  if (!object(action) || !object(preview) || !object(action.changes))
    return false;
  const changes = action.changes;
  const followup = preview.agent_followup;
  if (
    !exactKeys(action, ["action", "changes"]) ||
    !exactKeys(changes, ["child_session_id", "message", "scopes"]) ||
    !object(followup) ||
    !exactKeys(preview, ["label", "before", "after", "agent_followup"])
  )
    return false;
  const knowledge = followup.knowledge_sources;
  if (
    !exactKeys(followup, [
      "target",
      "task_name",
      "message",
      "scopes",
      ...(knowledge === undefined ? [] : ["knowledge_sources"]),
      "provider_name",
      "model",
      "leaves_device",
    ]) ||
    !object(followup.target) ||
    !exactKeys(followup.target, [
      "parent_session_id",
      "child_session_id",
      "spawn_proposal_id",
      "previous_generation_id",
      "child_version",
      "provider_id",
      "provider_version",
      "provider_config_version",
    ])
  )
    return false;
  const target = followup.target;
  for (const id of [
    target.parent_session_id,
    target.child_session_id,
    target.spawn_proposal_id,
    target.previous_generation_id,
    target.provider_id,
  ]) {
    if (typeof id !== "string" || !canonicalUuid.test(id)) return false;
  }
  const message = changes.message;
  const scopes = changes.scopes;
  if (
    changes.child_session_id !== target.child_session_id ||
    typeof followup.task_name !== "string" ||
    !/^[a-z][a-z0-9_]{0,39}$/.test(followup.task_name) ||
    typeof message !== "string" ||
    message !== message.trim() ||
    message.length === 0 ||
    Array.from(message).length > 4000 ||
    new TextEncoder().encode(message).byteLength > 16_384 ||
    followup.message !== message ||
    !Array.isArray(scopes) ||
    scopes.length < 1 ||
    scopes.length > delegatedAgentScopes.length ||
    scopes.some(
      (scope) =>
        typeof scope !== "string" ||
        !delegatedAgentScopes.includes(
          scope as (typeof delegatedAgentScopes)[number],
        ),
    ) ||
    new Set(scopes).size !== scopes.length ||
    scopes.some(
      (scope, index) =>
        index > 0 &&
        delegatedAgentScopes.indexOf(scope as never) <=
          delegatedAgentScopes.indexOf(scopes[index - 1] as never),
    ) ||
    JSON.stringify(followup.scopes) !== JSON.stringify(scopes) ||
    preview.label !== followup.task_name ||
    Object.keys(preview.before as Record<string, unknown>).length !== 0 ||
    Object.keys(preview.after as Record<string, unknown>).length !== 0 ||
    !positiveIntegerValue(target.child_version) ||
    !positiveIntegerValue(target.provider_version) ||
    !positiveIntegerValue(target.provider_config_version) ||
    !nonemptyString(followup.provider_name) ||
    !nonemptyString(followup.model) ||
    typeof followup.leaves_device !== "boolean"
  )
    return false;
  if (scopes.includes("knowledge")) {
    if (
      !Array.isArray(knowledge) ||
      knowledge.length < 1 ||
      knowledge.length > 20 ||
      knowledge.some(
        (source) =>
          !object(source) ||
          !exactKeys(source, ["source_id", "expected_source_version"]) ||
          typeof source.source_id !== "string" ||
          !canonicalUuid.test(source.source_id) ||
          !positiveIntegerValue(source.expected_source_version),
      ) ||
      new Set(knowledge.map((source) => String(source.source_id))).size !==
        knowledge.length
    )
      return false;
  } else if (knowledge !== undefined) return false;
  const confirmed = value.status === "confirmed";
  if (
    confirmed !== (value.result_id !== null) ||
    confirmed !== (value.result_version !== null) ||
    (confirmed &&
      (value.result_id !== target.child_session_id ||
        value.result_version !== 1)) ||
    (!confirmed && value.route !== "")
  )
    return false;
  const result = value.agent_followup_result;
  if (!confirmed) return result === undefined;
  return (
    object(result) &&
    exactKeys(result, [
      "session_id",
      "generation_id",
      "status",
      "error_code",
      "pending_approvals",
    ]) &&
    result.session_id === target.child_session_id &&
    result.generation_id === value.id &&
    [
      "queued",
      "streaming",
      "completed",
      "failed",
      "cancelled",
      "unavailable",
    ].includes(String(result.status)) &&
    value.route ===
      (result.status === "unavailable"
        ? ""
        : `/ai?session=${target.child_session_id}`) &&
    Number.isSafeInteger(result.pending_approvals) &&
    Number(result.pending_approvals) >= 0 &&
    (result.error_code === null ||
      (typeof result.error_code === "string" &&
        result.error_code.length >= 1 &&
        result.error_code.length <= 100))
  );
}

export function parseAiActionProposal(value: unknown): AiActionProposal {
  if (
    !object(value) ||
    typeof value.id !== "string" ||
    !uuid.test(value.id) ||
    typeof value.generation_id !== "string" ||
    !uuid.test(value.generation_id) ||
    typeof value.fingerprint !== "string" ||
    !/^[a-f0-9]{64}$/.test(value.fingerprint) ||
    !object(value.action) ||
    typeof value.action.action !== "string" ||
    !Object.hasOwn(aiActionLabels, value.action.action) ||
    !object(value.action.changes) ||
    !object(value.preview) ||
    typeof value.preview.label !== "string" ||
    !fields(value.preview.before) ||
    !fields(value.preview.after) ||
    typeof value.can_confirm !== "boolean" ||
    typeof value.status !== "string" ||
    !["pending", "confirmed", "rejected", "expired", "unavailable"].includes(
      value.status,
    ) ||
    (value.can_confirm && value.status !== "pending") ||
    typeof value.created_at !== "string" ||
    (value.decided_at !== null && typeof value.decided_at !== "string")
  )
    return invalid();
  if (value.created_task_ids !== undefined) {
    const ids = value.created_task_ids;
    if (
      value.action.action !== "inbox.split" ||
      value.status !== "confirmed" ||
      !Array.isArray(ids) ||
      ids.length < 1 ||
      ids.length > 20 ||
      !Array.isArray(value.preview.tasks) ||
      ids.length !== value.preview.tasks.length ||
      ids.some((id) => typeof id !== "string" || !canonicalUuid.test(id)) ||
      new Set(ids).size !== ids.length
    )
      return invalid();
  }
  if (value.action.action === "agent_delegate.spawn") {
    if (!validAgentDelegationProposal(value)) return invalid();
    return value as unknown as AiActionProposal;
  }
  if (value.action.action === "agent_delegate.followup") {
    if (!validAgentFollowupProposal(value)) return invalid();
    return value as unknown as AiActionProposal;
  }
  if (value.action.action === "agent_run.cancel_many") {
    if (!validAgentRunCancelManyProposal(value)) return invalid();
    return value as unknown as AiActionProposal;
  }
  if (
    value.action.action === "agent_run.cancel" ||
    value.action.action === "agent_run.recover_output"
  ) {
    if (!validAgentRunCancelProposal(value)) return invalid();
    return value as unknown as AiActionProposal;
  }
  if (
    value.action.action.startsWith("knowledge_source.") ||
    value.action.action.startsWith("knowledge_index_job.")
  ) {
    if (!validKnowledgeProposal(value)) return invalid();
    // Management receipts describe queued/running/cancelled/deleted states the
    // native page cannot highlight, so they open the library root instead of
    // inventing a precise location.
    return { ...(value as unknown as AiActionProposal), route: "/knowledge" };
  }
  if (value.action.action.startsWith("task_view.")) {
    if (!validTaskViewProposal(value)) return invalid();
    // Saved views are applied inside the Tasks page filter bar; there is no
    // per-view route, so the card only links to the task list.
    return { ...(value as unknown as AiActionProposal), route: "/tasks" };
  }
  if (value.action.action === "task.batch_update") {
    if (!validTaskBatchProposal(value)) return invalid();
    return { ...(value as unknown as AiActionProposal), route: "/tasks" };
  }
  if (value.action.action === "task.batch_create") {
    if (!validTaskBatchCreateProposal(value)) return invalid();
    return value as unknown as AiActionProposal;
  }
  if (value.action.action === "task.move") {
    if (!validTaskOrderProposal(value)) return invalid();
    return {
      ...(value as unknown as AiActionProposal),
      route: `/tasks/${value.action.task_id}`,
    };
  }
  if (value.action.action === "roadmap_milestone.move") {
    if (!validRoadmapOrderProposal(value)) return invalid();
    return {
      ...(value as unknown as AiActionProposal),
      route: `/roadmap?milestone=${value.action.roadmap_milestone_id}`,
    };
  }
  if (
    (value.action.agent_run_id !== undefined &&
      value.action.action !== "agent_run.retry") ||
    (value.preview.agent_run_retry !== undefined &&
      value.action.action !== "agent_run.retry") ||
    value.preview.agent_run_cancel !== undefined ||
    value.preview.agent_run_recovery !== undefined
  )
    return invalid();
  if (value.action.action.startsWith("project_note.")) {
    if (!validProjectNoteProposal(value as unknown as AiActionProposal))
      return invalid();
    const projectId = String(value.preview.after.project_id);
    const noteId =
      typeof value.result_id === "string"
        ? value.result_id
        : typeof value.action.project_note_id === "string"
          ? value.action.project_note_id
          : null;
    return {
      ...(value as unknown as AiActionProposal),
      route: noteId
        ? projectNoteHref(projectId, noteId)
        : `/projects/${projectId}`,
    };
  }
  if (
    value.action.action === "agent_run.start" ||
    value.action.action === "agent_run.retry"
  ) {
    if (!validAgentRunStartProposal(value)) return invalid();
    const result = value.agent_run_result as
      Record<string, unknown> | undefined;
    if (result === undefined) {
      return {
        ...(value as unknown as AiActionProposal),
        route: value.status === "confirmed" ? "" : String(value.route),
      };
    }
    const route =
      result?.output_delivery_status === "submitted" &&
      typeof result.submission_id === "string"
        ? taskSubmissionHref(String(value.action.task_id), result.submission_id)
        : String(value.route);
    return { ...(value as unknown as AiActionProposal), route };
  }
  if (
    value.preview.agent_run_start !== undefined ||
    value.agent_run_result !== undefined
  )
    return invalid();
  if (Object.hasOwn(value.action, "project_note_id")) return invalid();
  if (
    Object.hasOwn(value.action, "knowledge_source_id") ||
    Object.hasOwn(value.action, "knowledge_index_job_id") ||
    value.preview.knowledge !== undefined
  )
    return invalid();
  if (
    Object.hasOwn(value.action, "task_saved_view_id") ||
    value.preview.task_view !== undefined
  )
    return invalid();
  if (value.preview.task_batch !== undefined) return invalid();
  if (value.preview.task_order !== undefined) return invalid();
  if (value.preview.roadmap_order !== undefined) return invalid();
  if (isTaskOutputAction(value.action.action)) {
    if (!validTaskOutputProposal(value as unknown as AiActionProposal))
      return invalid();
    return {
      ...(value as unknown as AiActionProposal),
      route: value.result_id
        ? `/tasks/${value.action.task_id}/submissions/${value.result_id}`
        : value.action.action === "task.review"
          ? `/tasks/${value.action.task_id}/submissions/${value.action.changes.submission_id}`
          : `/tasks/${value.action.task_id}`,
    };
  }
  if (value.preview.task_output !== undefined) return invalid();
  if (value.action.action === "inbox.read_all") {
    const readAllAction = value.action as Record<string, unknown>;
    const changes = value.action.changes;
    const before = value.preview.before;
    const after = value.preview.after;
    const snapshot = value.preview.inbox_read_all;
    const cutoff = changes.through_created_at;
    const count = after.marked_count;
    const targetKeys = [
      "invoice_id",
      "financial_entry_id",
      "task_id",
      "project_id",
      "inbox_item_id",
      "reminder_id",
      "focus_session_id",
      "client_followup_id",
      "client_activity_id",
      "client_id",
      "automation_rule_id",
      "automation_run_id",
      "project_note_id",
      "expected_version",
    ];
    const validTimestamp =
      typeof cutoff === "string" &&
      /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/.test(cutoff) &&
      Number.isFinite(Date.parse(cutoff));
    const validCount =
      typeof count === "number" &&
      Number.isSafeInteger(count) &&
      count > 0 &&
      count <= 1_000;
    if (
      Object.keys(readAllAction).length !== 2 ||
      Object.keys(readAllAction).some(
        (key) => key !== "action" && key !== "changes",
      ) ||
      Object.keys(value.preview).length !== 4 ||
      Object.keys(value.preview).some(
        (key) => !["label", "before", "after", "inbox_read_all"].includes(key),
      ) ||
      Object.keys(changes).length !== 1 ||
      !validTimestamp ||
      targetKeys.some((key) => readAllAction[key] !== undefined) ||
      !object(snapshot) ||
      Object.keys(snapshot).length !== 3 ||
      snapshot.through_created_at !== cutoff ||
      snapshot.candidate_count !== count ||
      typeof snapshot.selection_fingerprint !== "string" ||
      !/^[a-f0-9]{64}$/.test(snapshot.selection_fingerprint) ||
      Object.keys(before).length !== 2 ||
      before.snapshot_at !== cutoff ||
      before.snapshot_unread_count !== count ||
      Object.keys(after).length !== 3 ||
      after.through_created_at !== cutoff ||
      after.snapshot_unread_count !== 0 ||
      !validCount ||
      value.preview.tasks !== undefined ||
      value.preview.next_followup !== undefined ||
      value.preview.automation_retry !== undefined ||
      value.automation_run_result !== undefined ||
      value.invoice_pdf_result !== undefined ||
      (value.result_id !== null &&
        (typeof value.result_id !== "string" || !uuid.test(value.result_id))) ||
      (value.result_version !== null && value.result_version !== 1)
    )
      return invalid();
    const confirmed = value.status === "confirmed";
    const readAllResult = object(value.inbox_read_all_result)
      ? value.inbox_read_all_result
      : null;
    if (
      confirmed !== Boolean(readAllResult) ||
      (confirmed &&
        (!readAllResult ||
          Object.keys(readAllResult).length !== 2 ||
          value.result_id !== value.id ||
          value.result_version !== 1 ||
          readAllResult.through_created_at !== cutoff ||
          readAllResult.marked_count !== count)) ||
      (!confirmed &&
        (value.result_id !== null || value.result_version !== null))
    )
      return invalid();
    return { ...(value as unknown as AiActionProposal), route: "/inbox" };
  }
  if (
    value.preview.inbox_read_all !== undefined ||
    value.inbox_read_all_result !== undefined
  )
    return invalid();
  if (value.action.action === "finance.export_csv") {
    if (!validFinanceExport(value as unknown as AiActionProposal))
      return invalid();
    return { ...(value as unknown as AiActionProposal), route: "" };
  }
  if (
    isAssignmentAction(value.action.action) &&
    !validAssignmentProposal(value as unknown as AiActionProposal)
  )
    return invalid();
  const isProject = value.action.action.startsWith("project.");
  const isTag = value.action.action.startsWith("tag.");
  const isRoadmap = value.action.action.startsWith("roadmap_milestone.");
  const isContentItem = value.action.action.startsWith("content_item.");
  const isFinancial = value.action.action.startsWith("financial_entry.");
  const isInvoice = value.action.action.startsWith("invoice.");
  const isInbox = value.action.action.startsWith("inbox.");
  const isReminder = value.action.action.startsWith("reminder.");
  const isFocus = value.action.action.startsWith("focus.");
  const isClient = value.action.action.startsWith("client.");
  const isClientContact = value.action.action.startsWith("client_contact.");
  const isPerson = value.action.action.startsWith("person.");
  const isFollowup = value.action.action.startsWith("client_followup.");
  const isActivity = value.action.action.startsWith("client_activity.");
  const isAutomation = value.action.action.startsWith("automation.");
  const isAutomationRetry = value.action.action === "automation.retry";
  const action = value.action;
  if (
    !validTaskReviewPolicyProposal(value) ||
    !validTaskTagProposal(value) ||
    !validTaskParentProposal(value) ||
    !validTaskDeleteProposal(value) ||
    !validProjectDeleteProposal(value) ||
    !validTagProposal(value)
  )
    return invalid();
  const targetKey = isInvoice
    ? "invoice_id"
    : isFinancial
      ? "financial_entry_id"
      : isAutomationRetry
        ? "automation_run_id"
        : isAutomation
          ? "automation_rule_id"
          : isClient || isClientContact
            ? "client_id"
            : isPerson
              ? "actor_id"
              : isTag
                ? "tag_id"
                : isActivity
                  ? "client_activity_id"
                  : isFollowup
                    ? "client_followup_id"
                    : isFocus
                      ? "focus_session_id"
                      : isReminder
                        ? "reminder_id"
                        : isInbox
                          ? "inbox_item_id"
                          : isRoadmap
                            ? "roadmap_milestone_id"
                            : isContentItem
                              ? "content_item_id"
                              : isProject
                                ? "project_id"
                                : "task_id";
  const targetId = value.action[targetKey];
  const unexpectedTarget = [
    "invoice_id",
    "financial_entry_id",
    "task_id",
    "tag_id",
    "project_id",
    "roadmap_milestone_id",
    "content_item_id",
    "inbox_item_id",
    "reminder_id",
    "focus_session_id",
    "client_followup_id",
    "client_activity_id",
    "client_id",
    "client_actor_link_id",
    "actor_id",
    "automation_rule_id",
    "automation_run_id",
    "knowledge_source_id",
    "knowledge_index_job_id",
  ].some(
    (key) =>
      key !== targetKey &&
      !(isClientContact && key === "client_actor_link_id") &&
      action[key] !== undefined,
  );
  const creating =
    value.action.action.endsWith(".create") || action.action === "focus.start";
  if (isInvoice && !validInvoiceProposal(value as unknown as AiActionProposal))
    return invalid();
  if (
    isFinancial &&
    !validFinancialProposal(value as unknown as AiActionProposal)
  )
    return invalid();
  if (isRoadmap && !validRoadmapProposal(value)) return invalid();
  if (isContentItem && !validContentItemProposal(value)) return invalid();
  if (isClient && !validClientProposal(value)) return invalid();
  if (isClientContact && !validClientContactProposal(value)) return invalid();
  if (isPerson && !validPersonProposal(value)) return invalid();
  if (!isFollowup && value.preview.next_followup !== undefined)
    return invalid();
  if (isAutomationRetry) {
    if (!validAutomationRetryProposal(value)) return invalid();
  } else if (
    value.preview.automation_retry !== undefined ||
    value.automation_run_result !== undefined
  )
    return invalid();
  if (isAutomation && !isAutomationRetry) {
    const before = value.preview.before;
    const after = value.preview.after;
    const changes = value.action.changes;
    const updating = action.action === "automation.update";
    const presetIds: Record<string, [string, string, string]> = {
      "00000000-0000-5000-8000-000000000101": [
        "project-completed-inbox",
        "event",
        "inbox_item",
      ],
      "00000000-0000-5000-8000-000000000102": [
        "daily-today-reminder",
        "schedule",
        "reminder",
      ],
      "00000000-0000-5000-8000-000000000103": [
        "weekly-review-reminder",
        "schedule",
        "reminder",
      ],
      "00000000-0000-5000-8000-000000000104": [
        "invoice-overdue-task",
        "event",
        "task",
      ],
      "00000000-0000-5000-8000-000000000105": [
        "agent-run-failed-inbox",
        "event",
        "inbox_item",
      ],
    };
    const preset = presetIds[String(targetId)];
    if (!preset) return invalid();
    const configKeys =
      preset[1] === "schedule" ? ["local_time", "timezone"] : ["priority"];
    const metadata = [
      "preset_key",
      "name",
      "available",
      "unavailable_reason",
      "trigger_type",
      "trigger_label",
      "action_type",
      "action_label",
      "permission_summary",
    ];
    const keys = [...metadata, "rule_enabled", ...configKeys];
    const validRecord = (f: Record<string, unknown>) => {
      if (
        Object.keys(f).length !== keys.length ||
        Object.keys(f).some((key) => !keys.includes(key)) ||
        metadata.some((key) =>
          key === "available"
            ? typeof f[key] !== "boolean"
            : typeof f[key] !== "string",
        ) ||
        typeof f.rule_enabled !== "boolean" ||
        !f.name ||
        !f.permission_summary ||
        !f.trigger_label ||
        !f.action_label ||
        f.preset_key !== preset[0] ||
        f.trigger_type !== preset[1] ||
        f.action_type !== preset[2]
      )
        return false;
      if (preset[1] === "event")
        return ["P0", "P1", "P2", "P3"].includes(String(f.priority));
      if (
        typeof f.local_time !== "string" ||
        !/^([01]\d|2[0-3]):[0-5]\d$/.test(f.local_time) ||
        typeof f.timezone !== "string" ||
        f.timezone === "Local" ||
        !f.timezone ||
        f.timezone.length > 100
      )
        return false;
      try {
        new Intl.DateTimeFormat("en", { timeZone: f.timezone });
        return true;
      } catch {
        return false;
      }
    };
    if (
      !validRecord(before) ||
      !validRecord(after) ||
      value.preview.label !== after.name ||
      metadata.some((key) => before[key] !== after[key]) ||
      Object.keys(changes).length !== (updating ? configKeys.length : 0) ||
      Object.keys(changes).some(
        (key) => !configKeys.includes(key) || changes[key] !== after[key],
      ) ||
      (updating
        ? before.rule_enabled !== after.rule_enabled
        : configKeys.some((key) => before[key] !== after[key])) ||
      (!updating &&
        after.rule_enabled !== (action.action === "automation.enable")) ||
      (action.action === "automation.enable" && !after.available) ||
      (value.result_id !== null && value.result_id !== targetId)
    )
      return invalid();
  }
  if (isActivity) {
    const after = value.preview.after;
    const before = value.preview.before;
    const changes = value.action.changes;
    const deleting = action.action === "client_activity.delete";
    const text = (v: unknown, max: number) =>
      typeof v === "string" &&
      v.trim().length > 0 &&
      Array.from(v).length <= max;
    const metadata = [
      "client_id",
      "client_name",
      "client_status",
      "kind",
      "title",
      "occurred_at",
      "activity_state",
    ];
    const bodyChanged = Object.hasOwn(changes, "body");
    const previewKeys = [...metadata, ...(bodyChanged ? ["body"] : [])];
    const record = (f: Record<string, unknown>, state: string) =>
      typeof f.client_id === "string" &&
      uuid.test(f.client_id) &&
      text(f.client_name, 200) &&
      ["active", "inactive", "lead"].includes(String(f.client_status)) &&
      ["note", "meeting"].includes(String(f.kind)) &&
      text(f.title, 200) &&
      typeof f.occurred_at === "string" &&
      /Z$/.test(f.occurred_at) &&
      Number.isFinite(Date.parse(f.occurred_at)) &&
      f.activity_state === state &&
      (!bodyChanged || text(f.body, 10000));
    const allowedChanges = deleting
      ? ["reason"]
      : [
          "kind",
          "title",
          "body",
          "occurred_at",
          ...(creating ? ["client_id"] : []),
        ];
    if (
      !record(after, deleting ? "deleted" : "visible") ||
      value.preview.label !== after.title ||
      Object.keys(after).some(
        (key) =>
          ![...previewKeys, ...(deleting ? ["reason"] : [])].includes(key),
      ) ||
      Object.keys(changes).length === 0 ||
      Object.keys(changes).some(
        (key) => !allowedChanges.includes(key) || changes[key] !== after[key],
      ) ||
      (deleting && !text(changes.reason, 1000)) ||
      (creating &&
        (Object.keys(before).length !== 0 ||
          allowedChanges.some((key) => !Object.hasOwn(changes, key)))) ||
      (!creating &&
        (!record(before, "visible") ||
          Object.keys(before).some((key) => !previewKeys.includes(key)) ||
          metadata.some(
            (key) =>
              key !== "activity_state" &&
              !Object.hasOwn(changes, key) &&
              before[key] !== after[key],
          )))
    )
      return invalid();
  }
  if (isFollowup) {
    const after = value.preview.after;
    const before = value.preview.before;
    const changes = value.action.changes;
    const next = value.preview.next_followup;
    const completing = action.action === "client_followup.complete";
    const rescheduling = action.action === "client_followup.reschedule";
    const reasonOnly = [
      "client_followup.cancel",
      "client_followup.skip",
    ].includes(String(action.action));
    const planFields = [
      "assigned_actor_id",
      "scheduled_at",
      "timezone",
      "channel",
      "purpose",
      "notes",
      "priority",
    ];
    const identities = [
      "client_id",
      "client_name",
      "client_status",
      "assigned_actor_name",
      "assigned_actor_type",
      "assigned_actor_status",
    ];
    const previewFields = [...planFields, ...identities, "status"];
    const instant = (v: unknown) =>
      typeof v === "string" && /Z$/.test(v) && Number.isFinite(Date.parse(v));
    const nonempty = (v: unknown, max: number) =>
      typeof v === "string" &&
      v.trim().length > 0 &&
      Array.from(v).length <= max;
    const plan = (f: Record<string, unknown>) =>
      ["client_id", "assigned_actor_id"].every(
        (key) => typeof f[key] === "string" && uuid.test(f[key] as string),
      ) &&
      [
        "client_name",
        "assigned_actor_name",
        "purpose",
        "channel",
        "timezone",
      ].every(
        (key) =>
          typeof f[key] === "string" && (f[key] as string).trim().length > 0,
      ) &&
      ["active", "lead", "inactive"].includes(String(f.client_status)) &&
      ["owner", "person"].includes(String(f.assigned_actor_type)) &&
      ["active", "inactive"].includes(String(f.assigned_actor_status)) &&
      ["low", "normal", "high"].includes(String(f.priority)) &&
      instant(f.scheduled_at) &&
      (f.notes === undefined ||
        f.notes === null ||
        typeof f.notes === "string");
    const expectedStatus = completing
      ? "completed"
      : rescheduling || action.action === "client_followup.cancel"
        ? "cancelled"
        : action.action === "client_followup.skip"
          ? "skipped"
          : "planned";
    const lifecycleFields = completing
      ? ["result", "completed_at", "next_step"]
      : reasonOnly || rescheduling
        ? ["reason"]
        : [];
    if (
      !plan(after) ||
      after.status !== expectedStatus ||
      ((creating || action.action === "client_followup.update") &&
        after.client_status === "inactive") ||
      ((creating ||
        (action.action === "client_followup.update" &&
          Object.hasOwn(changes, "assigned_actor_id"))) &&
        after.assigned_actor_status !== "active") ||
      (creating
        ? Object.keys(before).length !== 0
        : !plan(before) ||
          before.status !== "planned" ||
          before.client_id !== after.client_id) ||
      Object.keys(before).some((key) => !previewFields.includes(key)) ||
      Object.keys(after).some(
        (key) => !previewFields.includes(key) && !lifecycleFields.includes(key),
      ) ||
      Object.keys(changes).length === 0
    )
      return invalid();
    // Existing records retain every unedited plan field. Nested next plans must
    // be shown independently, never silently substituted for the original.
    if (
      !creating &&
      Object.entries(before).some(
        ([key, v]) =>
          key !== "status" &&
          !(
            action.action === "client_followup.update" &&
            (Object.hasOwn(changes, key) ||
              (Object.hasOwn(changes, "assigned_actor_id") &&
                key.startsWith("assigned_actor_")))
          ) &&
          after[key] !== v,
      )
    )
      return invalid();
    if (completing) {
      if (
        Object.keys(changes).some(
          (key) => ![...lifecycleFields, "next_followup"].includes(key),
        ) ||
        !nonempty(after.result, 4000) ||
        !instant(after.completed_at) ||
        !(after.next_step === null || typeof after.next_step === "string") ||
        lifecycleFields.some((key) => after[key] !== changes[key]) ||
        Object.hasOwn(changes, "next_followup") !== (next !== undefined)
      )
        return invalid();
    } else if (reasonOnly) {
      if (
        Object.keys(changes).length !== 1 ||
        !nonempty(changes.reason, 1000) ||
        changes.reason !== after.reason
      )
        return invalid();
    } else if (rescheduling) {
      if (
        !nonempty(changes.reason, 1000) ||
        changes.reason !== after.reason ||
        next === undefined
      )
        return invalid();
    } else {
      if (
        Object.entries(changes).some(
          ([key, v]) =>
            !(planFields.includes(key) || (creating && key === "client_id")) ||
            after[key] !== v,
        ) ||
        (creating &&
          ["client_id", ...planFields].some(
            (key) => !Object.hasOwn(changes, key),
          )) ||
        (!creating &&
          Object.hasOwn(changes, "notes") &&
          !Object.hasOwn(before, "notes"))
      )
        return invalid();
    }
    if (next !== undefined) {
      const rawNext = rescheduling ? changes : changes.next_followup;
      if (
        (!completing && !rescheduling) ||
        !fields(next) ||
        !plan(next) ||
        next.status !== "planned" ||
        next.client_id !== after.client_id ||
        next.client_name !== after.client_name ||
        next.client_status !== after.client_status ||
        next.client_status === "inactive" ||
        next.assigned_actor_status !== "active" ||
        Object.keys(next).some((key) => !previewFields.includes(key)) ||
        !object(rawNext) ||
        planFields.some(
          (key) => !Object.hasOwn(rawNext, key) || rawNext[key] !== next[key],
        ) ||
        Object.keys(rawNext).some(
          (key) =>
            !planFields.includes(key) && !(rescheduling && key === "reason"),
        )
      )
        return invalid();
    }
  }
  if (isFocus) {
    const after = value.preview.after;
    const before = value.preview.before;
    const changes = value.action.changes;
    const starting = action.action === "focus.start";
    const recovering = action.action === "focus.recover";
    const expectedStatus: Record<string, string> = {
      "focus.start": "active",
      "focus.pause": "paused",
      "focus.resume": "active",
      "focus.stop": "completed",
      "focus.cancel": "cancelled",
      "focus.recover":
        changes.recovery_action === "interrupt" ? "interrupted" : "active",
    };
    const beforeStatuses: Record<string, string[]> = {
      "focus.start": ["idle"],
      "focus.pause": ["active"],
      "focus.resume": ["paused"],
      "focus.stop": ["active", "paused"],
      "focus.cancel": ["active", "paused"],
      "focus.recover": ["recovery_pending"],
    };
    if (
      Object.keys(before).length !== 1 ||
      Object.keys(after).length !== (starting ? 5 : recovering ? 7 : 4) ||
      !beforeStatuses[String(action.action)]?.includes(String(before.status)) ||
      after.status !== expectedStatus[String(action.action)] ||
      !(
        after.task_id === null ||
        (typeof after.task_id === "string" && uuid.test(after.task_id))
      ) ||
      !(after.task_title === null || typeof after.task_title === "string") ||
      (after.task_id === null) !== (after.task_title === null) ||
      typeof after.planned_seconds !== "number" ||
      !Number.isSafeInteger(after.planned_seconds) ||
      after.planned_seconds < 300 ||
      after.planned_seconds > 7200
    )
      return invalid();
    if (starting) {
      if (
        Object.keys(changes).some(
          (key) =>
            !["task_id", "expected_task_version", "planned_seconds"].includes(
              key,
            ),
        ) ||
        changes.task_id !== after.task_id ||
        changes.planned_seconds !== after.planned_seconds ||
        (after.task_id === null
          ? after.task_version !== null ||
            Object.hasOwn(changes, "expected_task_version")
          : typeof changes.expected_task_version !== "number" ||
            !Number.isSafeInteger(changes.expected_task_version) ||
            changes.expected_task_version < 1 ||
            after.task_version !== changes.expected_task_version)
      )
        return invalid();
    } else if (recovering) {
      if (
        Object.keys(changes).length !== 1 ||
        !["include_gap_resume", "exclude_gap_resume", "interrupt"].includes(
          String(changes.recovery_action),
        ) ||
        after.recovery_action !== changes.recovery_action ||
        !(
          after.last_heartbeat_at === null ||
          (typeof after.last_heartbeat_at === "string" &&
            /Z$/.test(after.last_heartbeat_at) &&
            Number.isFinite(Date.parse(after.last_heartbeat_at)))
        ) ||
        typeof after.accumulated_seconds !== "number" ||
        !Number.isSafeInteger(after.accumulated_seconds) ||
        after.accumulated_seconds < 0 ||
        after.accumulated_seconds > after.planned_seconds
      )
        return invalid();
    } else if (Object.keys(changes).length !== 0) return invalid();
  }
  if (value.action.action === "inbox.force_resolve") {
    const before = value.preview.before;
    const after = value.preview.after;
    const changes = value.action.changes;
    const counts = [
      "active_task_count",
      "required_task_count",
      "required_done_count",
      "required_remaining_count",
      "required_blocked_count",
      "required_waiting_review_count",
      "required_cancelled_count",
    ];
    if (
      Object.keys(changes).length !== 1 ||
      typeof changes.reason !== "string" ||
      !changes.reason.trim() ||
      after.reason !== changes.reason.trim() ||
      !["open", "tracking"].includes(String(before.status)) ||
      after.status !== "resolved" ||
      before.resolution_policy !== "all_required_tasks_done" ||
      after.resolution_policy !== before.resolution_policy ||
      after.resolution_mode !== "forced" ||
      counts.some(
        (key) =>
          typeof after[key] !== "number" ||
          !Number.isSafeInteger(after[key]) ||
          Number(after[key]) < 0 ||
          before[key] !== after[key],
      ) ||
      Number(after.required_task_count) > Number(after.active_task_count) ||
      Number(after.required_done_count) +
        Number(after.required_remaining_count) !==
        Number(after.required_task_count) ||
      Number(after.required_blocked_count) +
        Number(after.required_waiting_review_count) +
        Number(after.required_cancelled_count) >
        Number(after.required_remaining_count)
    )
      return invalid();
  }
  if (isReminder) {
    const schedule = (f: Record<string, unknown>) =>
      typeof f.title === "string" &&
      (f.summary === undefined || typeof f.summary === "string") &&
      ["P0", "P1", "P2", "P3"].includes(String(f.priority)) &&
      typeof f.trigger_at === "string" &&
      /Z$/.test(f.trigger_at) &&
      Number.isFinite(Date.parse(f.trigger_at)) &&
      ["none", "daily", "weekly", "weekdays", "monthly"].includes(
        String(f.recurrence_type),
      ) &&
      typeof f.recurrence_timezone === "string" &&
      f.recurrence_timezone.length > 0 &&
      [
        "recurrence_interval",
        "recurrence_anchor_day",
        "occurrence_number",
      ].every(
        (key) =>
          typeof f[key] === "number" &&
          Number.isSafeInteger(f[key]) &&
          Number(f[key]) > 0,
      ) &&
      Number(f.recurrence_interval) <= 365 &&
      Number(f.recurrence_anchor_day) <= 31 &&
      (f.recurrence_type === "monthly" || f.recurrence_anchor_day === 1) &&
      (f.recurrence_type !== "none" ||
        (f.recurrence_interval === 1 && f.recurrence_timezone === "UTC"));
    const after = value.preview.after;
    if (
      !schedule(after) ||
      ((creating || Object.hasOwn(value.action.changes, "summary")) &&
        typeof after.summary !== "string") ||
      (!creating &&
        Object.hasOwn(value.action.changes, "summary") &&
        typeof value.preview.before.summary !== "string") ||
      (!creating &&
        (!schedule(value.preview.before) ||
          value.preview.before.status !== "scheduled")) ||
      after.status !==
        (action.action === "reminder.cancel" ? "cancelled" : "scheduled") ||
      (action.action === "reminder.cancel" &&
        (typeof after.reason !== "string" || !after.reason.trim()))
    )
      return invalid();
  }
  if (value.action.action === "inbox.split") {
    const after = value.preview.after;
    if (
      !splitDrafts(value.preview.tasks, value.action.changes) ||
      after.created_task_count !== value.preview.tasks.length ||
      after.status !== "tracking" ||
      !["manual", "all_required_tasks_done"].includes(
        String(after.resolution_policy),
      ) ||
      ["active_task_count", "required_task_count", "required_done_count"].some(
        (key) =>
          typeof after[key] !== "number" ||
          !Number.isSafeInteger(after[key]) ||
          Number(after[key]) < 0,
      )
    )
      return invalid();
  } else if (value.preview.tasks !== undefined) return invalid();
  if (
    ["inbox.link_task", "inbox.set_required", "inbox.unlink_task"].includes(
      value.action.action,
    )
  ) {
    const changes = value.action.changes;
    const after = value.preview.after;
    if (
      typeof changes.task_id !== "string" ||
      !uuid.test(changes.task_id) ||
      typeof changes.expected_task_version !== "number" ||
      !Number.isSafeInteger(changes.expected_task_version) ||
      changes.expected_task_version < 1 ||
      after.task_id !== changes.task_id ||
      after.task_version !== changes.expected_task_version ||
      typeof after.task_title !== "string" ||
      typeof after.task_status !== "string" ||
      typeof after.is_required !== "boolean" ||
      !["linked", "unlinked"].includes(String(after.relation_state)) ||
      !["manual", "all_required_tasks_done"].includes(
        String(after.resolution_policy),
      ) ||
      !["open", "tracking", "resolved"].includes(String(after.status)) ||
      [
        "active_task_count",
        "required_task_count",
        "required_done_count",
        "required_remaining_count",
      ].some(
        (key) =>
          typeof after[key] !== "number" ||
          !Number.isSafeInteger(after[key]) ||
          Number(after[key]) < 0,
      )
    )
      return invalid();
  }
  if (
    unexpectedTarget ||
    (creating &&
      (targetId !== undefined || value.action.expected_version !== undefined))
  )
    return invalid();
  if (
    !creating &&
    (typeof targetId !== "string" ||
      !uuid.test(targetId) ||
      typeof value.action.expected_version !== "number" ||
      !Number.isSafeInteger(value.action.expected_version) ||
      value.action.expected_version < 1)
  )
    return invalid();
  if (
    value.action.action === "project.complete" &&
    (typeof value.preview.after.incomplete_task_count !== "number" ||
      !Number.isSafeInteger(value.preview.after.incomplete_task_count) ||
      value.preview.after.incomplete_task_count < 0)
  )
    return invalid();
  if (
    value.result_id !== null &&
    (typeof value.result_id !== "string" || !uuid.test(value.result_id))
  )
    return invalid();
  if (
    value.result_version !== null &&
    (typeof value.result_version !== "number" ||
      !Number.isInteger(value.result_version) ||
      value.result_version < 1)
  )
    return invalid();
  if (
    value.status === "confirmed" &&
    (value.result_id === null || value.result_version === null)
  )
    return invalid();
  const result = value as unknown as AiActionProposal;
  if (!validInvoicePdfResult(result)) return invalid();
  const routeId =
    action.action === "invoice.generate_pdf"
      ? targetId
      : isClientContact
        ? action.client_id
        : (result.result_id ?? targetId);
  return {
    ...result,
    route:
      action.action === "project.delete" && result.status === "confirmed"
        ? "/projects"
        : action.action === "client.delete" && result.status === "confirmed"
          ? "/clients"
          : action.action === "content_item.delete" &&
              result.status === "confirmed"
            ? "/content-calendar"
            : action.action === "roadmap_milestone.delete" &&
                result.status === "confirmed"
              ? "/roadmap"
              : action.action === "task.delete" && result.status === "confirmed"
                ? "/tasks"
                : isTag
                  ? "/tasks"
                  : action.action === "invoice.delete" &&
                      result.status === "confirmed"
                    ? "/invoices"
                    : isAutomation
                      ? automationLocationHref(
                          isAutomationRetry ? "run" : "rule",
                          String(routeId),
                        )
                      : isPerson
                        ? ""
                        : isFollowup || isActivity
                          ? routeId
                            ? clientRecordHref(
                                String(result.preview.after.client_id),
                                isFollowup ? "followup" : "activity",
                                String(routeId),
                              )
                            : `/clients/${result.preview.after.client_id}`
                          : routeId
                            ? isFocus
                              ? "/focus"
                              : isReminder
                                ? `/inbox?reminder=${routeId}`
                                : isRoadmap
                                  ? `/roadmap?milestone=${routeId}`
                                  : isContentItem
                                    ? `/content-calendar?item=${routeId}`
                                    : isClient || isClientContact
                                      ? `/clients/${routeId}`
                                      : `/${isInvoice ? "invoices" : isFinancial ? "income" : isInbox ? "inbox" : isProject ? "projects" : "tasks"}/${routeId}`
                            : "",
  };
}
export async function getAiWorkspaceActions(
  generationId: string,
): Promise<AiActionProposal[]> {
  const result = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/generations/${encodeURIComponent(generationId)}/actions`,
  );
  if (!Array.isArray(result.data) || result.data.length > 8) return invalid();
  const items = result.data.map(parseAiActionProposal);
  if (
    items.some((item) => item.generation_id !== generationId) ||
    new Set(items.map((item) => item.id)).size !== items.length
  )
    return invalid();
  return items;
}
export async function decideAiTaskOutput(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  consent: boolean,
) {
  if (!validTaskOutputProposal(proposal)) return invalid();
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm" ? { confirm_task_output: consent } : {}),
      }),
    },
  );
  const result = parseAiActionProposal(response.data);
  if (
    result.id !== proposal.id ||
    result.generation_id !== proposal.generation_id ||
    result.fingerprint !== proposal.fingerprint ||
    result.action.action !== proposal.action.action ||
    result.action.task_id !== proposal.action.task_id
  )
    return invalid();
  return result;
}

export async function decideAiAgentDelegation(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  consent: boolean,
) {
  if (
    proposal.action.action !== "agent_delegate.spawn" ||
    !validAgentDelegationProposal(
      proposal as unknown as Record<string, unknown>,
    )
  )
    return invalid();
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm"
          ? { confirm_agent_delegation: consent }
          : {}),
      }),
    },
  );
  const result = parseAiActionProposal(response.data);
  if (
    result.id !== proposal.id ||
    result.generation_id !== proposal.generation_id ||
    result.fingerprint !== proposal.fingerprint ||
    result.action.action !== proposal.action.action ||
    JSON.stringify(result.action.changes) !==
      JSON.stringify(proposal.action.changes)
  )
    return invalid();
  return result;
}

export async function confirmAiAgentDelegationBatch(
  generationId: string,
  proposals: AiActionProposal[],
  consent: boolean,
): Promise<AiActionProposal[]> {
  if (
    !uuid.test(generationId) ||
    !Array.isArray(proposals) ||
    proposals.length < 2 ||
    proposals.length > 4 ||
    consent !== true
  )
    return invalid();
  const ids = new Set<string>();
  const fingerprints = new Set<string>();
  let parentSessionId = "";
  for (const proposal of proposals) {
    if (
      !proposal ||
      proposal.generation_id !== generationId ||
      proposal.action.action !== "agent_delegate.spawn" ||
      proposal.status !== "pending" ||
      !proposal.can_confirm ||
      !validAgentDelegationProposal(
        proposal as unknown as Record<string, unknown>,
      ) ||
      ids.has(proposal.id) ||
      fingerprints.has(proposal.fingerprint)
    )
      return invalid();
    const parent = proposal.preview.agent_delegation?.parent_session_id;
    if (!parent || (parentSessionId && parentSessionId !== parent))
      return invalid();
    parentSessionId = parent;
    ids.add(proposal.id);
    fingerprints.add(proposal.fingerprint);
  }
  const payload = await apiRequest<unknown>(
    `/api/v1/ai/generations/${encodeURIComponent(generationId)}/agent-delegations/confirm`,
    {
      method: "POST",
      body: JSON.stringify({
        confirm_agent_delegations: true,
        items: proposals.map(({ id, fingerprint }) => ({
          proposal_id: id,
          fingerprint,
        })),
      }),
    },
  );
  if (
    !object(payload) ||
    !exactKeys(payload, ["data"]) ||
    !Array.isArray(payload.data)
  )
    return invalid();
  if (payload.data.length !== proposals.length) return invalid();
  const results = payload.data.map(parseAiActionProposal);
  for (const [index, result] of results.entries()) {
    const selected = proposals[index];
    if (
      result.id !== selected.id ||
      result.generation_id !== generationId ||
      result.fingerprint !== selected.fingerprint ||
      result.action.action !== "agent_delegate.spawn" ||
      JSON.stringify(result.action.changes) !==
        JSON.stringify(selected.action.changes) ||
      result.status !== "confirmed" ||
      result.result_id !==
        selected.preview.agent_delegation?.child_session_id ||
      result.agent_delegation_result?.session_id !==
        selected.preview.agent_delegation?.child_session_id
    )
      return invalid();
  }
  return results;
}

export async function decideAiAgentFollowup(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  consent: boolean,
) {
  if (
    proposal.action.action !== "agent_delegate.followup" ||
    !validAgentFollowupProposal(proposal as unknown as Record<string, unknown>)
  )
    return invalid();
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm"
          ? { confirm_agent_delegation: consent }
          : {}),
      }),
    },
  );
  const result = parseAiActionProposal(response.data);
  if (
    result.id !== proposal.id ||
    result.generation_id !== proposal.generation_id ||
    result.fingerprint !== proposal.fingerprint ||
    result.action.action !== proposal.action.action ||
    JSON.stringify(result.action.changes) !==
      JSON.stringify(proposal.action.changes) ||
    JSON.stringify(result.preview.agent_followup) !==
      JSON.stringify(proposal.preview.agent_followup)
  )
    return invalid();
  return result;
}

export async function decideAiAgentRun(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  confirmAgentExecution: boolean,
  confirmAgentFiles?: boolean,
  confirmAgentRework?: boolean,
  confirmAgentRestart?: boolean,
  confirmAgentProjectFiles?: boolean,
) {
  if (
    !["agent_run.start", "agent_run.retry"].includes(proposal.action.action) ||
    !validAgentRunStartProposal(proposal as unknown as Record<string, unknown>)
  )
    return invalid();
  const hasInputFiles =
    (proposal.preview.agent_run_start?.input_files?.length ?? 0) > 0;
  const hasProjectFiles = proposal.preview.agent_run_start?.input_files?.some(
    (file) => file.source_kind === "project_task_artifact",
  );
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        ...(decision === "confirm" && hasProjectFiles
          ? { confirm_agent_project_files: confirmAgentProjectFiles }
          : {}),
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm"
          ? { confirm_agent_execution: confirmAgentExecution }
          : {}),
        ...(decision === "confirm" && hasInputFiles
          ? { confirm_agent_files: confirmAgentFiles }
          : {}),
        ...(decision === "confirm" &&
        proposal.preview.agent_run_start?.rework_context
          ? { confirm_agent_rework: confirmAgentRework }
          : {}),
        ...(decision === "confirm" && proposal.preview.agent_run_start?.restart
          ? { confirm_agent_restart: confirmAgentRestart }
          : {}),
      }),
    },
  );
  const result = parseAiActionProposal(response.data);
  if (
    result.id !== proposal.id ||
    result.generation_id !== proposal.generation_id ||
    result.fingerprint !== proposal.fingerprint ||
    result.action.action !== proposal.action.action ||
    result.action.agent_run_id !== proposal.action.agent_run_id ||
    result.action.task_id !== proposal.action.task_id ||
    result.action.changes.restart_of_run_id !==
      proposal.action.changes.restart_of_run_id
  )
    return invalid();
  return result;
}

export async function decideAiAgentRunCancel(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  confirmed: boolean,
) {
  if (
    !["agent_run.cancel", "agent_run.cancel_many"].includes(
      proposal.action.action,
    ) ||
    !(proposal.action.action === "agent_run.cancel"
      ? validAgentRunCancelProposal(
          proposal as unknown as Record<string, unknown>,
        )
      : validAgentRunCancelManyProposal(
          proposal as unknown as Record<string, unknown>,
        ))
  )
    return invalid();
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm" ? { confirm_agent_cancel: confirmed } : {}),
      }),
    },
  );
  const result = parseAiActionProposal(response.data);
  if (
    result.id !== proposal.id ||
    result.generation_id !== proposal.generation_id ||
    result.fingerprint !== proposal.fingerprint ||
    result.action.action !== proposal.action.action ||
    JSON.stringify(result.action.changes) !==
      JSON.stringify(proposal.action.changes) ||
    (proposal.action.action === "agent_run.cancel" &&
      (result.action.agent_run_id !== proposal.action.agent_run_id ||
        result.action.task_id !== proposal.action.task_id))
  )
    return invalid();
  return result;
}

export async function decideAiAgentRunRecovery(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  consent: boolean,
) {
  if (
    proposal.action.action !== "agent_run.recover_output" ||
    !validAgentRunCancelProposal(proposal as unknown as Record<string, unknown>)
  )
    return invalid();
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm"
          ? { confirm_agent_output_recovery: consent }
          : {}),
      }),
    },
  );
  const result = parseAiActionProposal(response.data);
  if (
    result.id !== proposal.id ||
    result.generation_id !== proposal.generation_id ||
    result.fingerprint !== proposal.fingerprint ||
    result.action.action !== proposal.action.action ||
    result.action.task_id !== proposal.action.task_id ||
    result.action.agent_run_id !== proposal.action.agent_run_id
  )
    return invalid();
  return result;
}

export async function decideAiTaskDelete(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  consent: boolean,
) {
  if (
    proposal.action.action !== "task.delete" ||
    !validTaskDeleteProposal(proposal as unknown as Record<string, unknown>)
  )
    return invalid();
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm" ? { confirm_task_delete: consent } : {}),
      }),
    },
  );
  const result = parseAiActionProposal(response.data);
  if (
    result.id !== proposal.id ||
    result.generation_id !== proposal.generation_id ||
    result.fingerprint !== proposal.fingerprint ||
    result.action.action !== "task.delete" ||
    result.action.task_id !== proposal.action.task_id ||
    result.action.expected_version !== proposal.action.expected_version
  )
    return invalid();
  return result;
}

export async function decideAiProjectDelete(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  consent: boolean,
) {
  if (
    proposal.action.action !== "project.delete" ||
    !validProjectDeleteProposal(proposal as unknown as Record<string, unknown>)
  )
    return invalid();
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm" ? { confirm_project_delete: consent } : {}),
      }),
    },
  );
  const result = parseAiActionProposal(response.data);
  if (
    result.id !== proposal.id ||
    result.generation_id !== proposal.generation_id ||
    result.fingerprint !== proposal.fingerprint ||
    result.action.action !== "project.delete" ||
    result.action.project_id !== proposal.action.project_id ||
    result.action.expected_version !== proposal.action.expected_version
  )
    return invalid();
  return result;
}

export async function decideAiClientDelete(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  consent: boolean,
) {
  if (
    proposal.action.action !== "client.delete" ||
    !validClientProposal(proposal as unknown as Record<string, unknown>)
  )
    return invalid();
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm" ? { confirm_client_delete: consent } : {}),
      }),
    },
  );
  const result = parseAiActionProposal(response.data);
  if (
    result.id !== proposal.id ||
    result.generation_id !== proposal.generation_id ||
    result.fingerprint !== proposal.fingerprint ||
    result.action.action !== "client.delete" ||
    result.action.client_id !== proposal.action.client_id ||
    result.action.expected_version !== proposal.action.expected_version
  )
    return invalid();
  return result;
}

export async function decideAiContentItemDelete(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  consent: boolean,
) {
  if (
    proposal.action.action !== "content_item.delete" ||
    !validContentItemProposal(proposal as unknown as Record<string, unknown>)
  )
    return invalid();
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm"
          ? { confirm_content_item_delete: consent }
          : {}),
      }),
    },
  );
  const result = parseAiActionProposal(response.data);
  if (
    result.id !== proposal.id ||
    result.generation_id !== proposal.generation_id ||
    result.fingerprint !== proposal.fingerprint ||
    result.action.action !== "content_item.delete" ||
    result.action.content_item_id !== proposal.action.content_item_id ||
    result.action.expected_version !== proposal.action.expected_version
  )
    return invalid();
  return result;
}

export async function decideAiContentPublished(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  consent: boolean,
) {
  if (
    proposal.action.action !== "content_item.publish" ||
    !validContentItemProposal(proposal as unknown as Record<string, unknown>)
  )
    return invalid();
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm"
          ? { confirm_content_published: consent }
          : {}),
      }),
    },
  );
  const result = parseAiActionProposal(response.data);
  if (
    result.id !== proposal.id ||
    result.generation_id !== proposal.generation_id ||
    result.fingerprint !== proposal.fingerprint ||
    result.action.action !== "content_item.publish" ||
    result.action.content_item_id !== proposal.action.content_item_id ||
    result.action.expected_version !== proposal.action.expected_version
  )
    return invalid();
  return result;
}

export async function decideAiRoadmapMilestoneDelete(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  consent: boolean,
) {
  if (
    proposal.action.action !== "roadmap_milestone.delete" ||
    !validRoadmapProposal(proposal as unknown as Record<string, unknown>)
  )
    return invalid();
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm"
          ? { confirm_roadmap_milestone_delete: consent }
          : {}),
      }),
    },
  );
  const result = parseAiActionProposal(response.data);
  if (
    result.id !== proposal.id ||
    result.generation_id !== proposal.generation_id ||
    result.fingerprint !== proposal.fingerprint ||
    result.action.action !== "roadmap_milestone.delete" ||
    result.action.roadmap_milestone_id !==
      proposal.action.roadmap_milestone_id ||
    result.action.expected_version !== proposal.action.expected_version
  )
    return invalid();
  return result;
}

export async function decideAiTaskViewAction(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  consent: boolean,
) {
  const action = proposal.action.action;
  if (
    !["task_view.create", "task_view.update", "task_view.delete"].includes(
      action,
    ) ||
    !validTaskViewProposal(proposal as unknown as Record<string, unknown>)
  )
    return invalid();
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm" && action === "task_view.delete"
          ? { confirm_task_view_delete: consent }
          : {}),
      }),
    },
  );
  const result = parseAiActionProposal(response.data);
  if (
    result.id !== proposal.id ||
    result.generation_id !== proposal.generation_id ||
    result.fingerprint !== proposal.fingerprint ||
    result.action.action !== action ||
    result.action.task_saved_view_id !== proposal.action.task_saved_view_id ||
    result.action.expected_version !== proposal.action.expected_version
  )
    return invalid();
  return result;
}

export async function decideAiKnowledgeAction(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  consent: boolean,
) {
  const action = proposal.action.action;
  if (
    ![
      "knowledge_source.create",
      "knowledge_source.reindex",
      "knowledge_source.delete",
      "knowledge_index_job.retry",
      "knowledge_index_job.cancel",
    ].includes(action) ||
    !validKnowledgeProposal(proposal as unknown as Record<string, unknown>)
  )
    return invalid();
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm" && action === "knowledge_source.delete"
          ? { confirm_knowledge_source_delete: consent }
          : {}),
      }),
    },
  );
  const result = parseAiActionProposal(response.data);
  if (
    result.id !== proposal.id ||
    result.generation_id !== proposal.generation_id ||
    result.fingerprint !== proposal.fingerprint ||
    result.action.action !== action ||
    result.action.knowledge_source_id !== proposal.action.knowledge_source_id ||
    result.action.knowledge_index_job_id !==
      proposal.action.knowledge_index_job_id
  )
    return invalid();
  return result;
}

export async function decideAiTagDelete(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  consent: boolean,
) {
  if (
    proposal.action.action !== "tag.delete" ||
    !validTagProposal(proposal as unknown as Record<string, unknown>)
  )
    return invalid();
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm" ? { confirm_tag_delete: consent } : {}),
      }),
    },
  );
  const result = parseAiActionProposal(response.data);
  if (
    result.id !== proposal.id ||
    result.generation_id !== proposal.generation_id ||
    result.fingerprint !== proposal.fingerprint ||
    result.action.action !== "tag.delete" ||
    result.action.tag_id !== proposal.action.tag_id ||
    result.action.expected_version !== proposal.action.expected_version
  )
    return invalid();
  return result;
}

export async function decideAiWorkspaceAction(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  confirmIncompleteTasks?: boolean,
  confirmForceResolve?: boolean,
  focusConsent?: { start?: boolean; includeGap?: boolean },
  confirmFollowupCompleted?: boolean,
  confirmActivityDelete?: boolean,
  confirmAutomationEffects?: boolean,
  confirmAutomationRetry?: boolean,
  confirmFinancialEffects?: boolean,
  confirmInvoiceEffects?: boolean,
  confirmInvoiceDelete?: boolean,
  confirmInvoicePdf?: boolean,
) {
  const result = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm" &&
        proposal.action.action === "invoice.delete" &&
        confirmInvoiceDelete !== undefined
          ? { confirm_invoice_delete: confirmInvoiceDelete }
          : {}),
        ...(decision === "confirm" &&
        proposal.action.action === "invoice.generate_pdf" &&
        confirmInvoicePdf !== undefined
          ? { confirm_invoice_pdf: confirmInvoicePdf }
          : {}),
        ...(decision === "confirm" &&
        proposal.action.action.startsWith("invoice.") &&
        confirmInvoiceEffects !== undefined
          ? { confirm_invoice_effects: confirmInvoiceEffects }
          : {}),
        ...(decision === "confirm" &&
        proposal.action.action.startsWith("financial_entry.") &&
        confirmFinancialEffects !== undefined
          ? { confirm_financial_effects: confirmFinancialEffects }
          : {}),
        ...(decision === "confirm" &&
        proposal.action.action === "automation.retry" &&
        confirmAutomationRetry !== undefined
          ? { confirm_automation_retry: confirmAutomationRetry }
          : {}),
        ...(decision === "confirm" &&
        needsAutomationEffectsConsent(proposal) &&
        confirmAutomationEffects !== undefined
          ? { confirm_automation_effects: confirmAutomationEffects }
          : {}),
        ...(decision === "confirm" &&
        proposal.action.action === "client_activity.delete" &&
        confirmActivityDelete !== undefined
          ? { confirm_activity_delete: confirmActivityDelete }
          : {}),
        ...(decision === "confirm" &&
        proposal.action.action === "client_followup.complete" &&
        confirmFollowupCompleted !== undefined
          ? { confirm_followup_completed: confirmFollowupCompleted }
          : {}),
        ...(decision === "confirm" &&
        proposal.action.action === "project.complete" &&
        confirmIncompleteTasks !== undefined
          ? { confirm_incomplete_tasks: confirmIncompleteTasks }
          : {}),
        ...(decision === "confirm" &&
        proposal.action.action === "inbox.force_resolve" &&
        confirmForceResolve !== undefined
          ? { confirm_force_resolve: confirmForceResolve }
          : {}),
        ...(decision === "confirm" &&
        proposal.action.action === "focus.start" &&
        focusConsent?.start !== undefined
          ? { confirm_focus_start: focusConsent.start }
          : {}),
        ...(decision === "confirm" &&
        proposal.action.action === "focus.recover" &&
        proposal.action.changes.recovery_action === "include_gap_resume" &&
        focusConsent?.includeGap !== undefined
          ? { confirm_focus_gap: focusConsent.includeGap }
          : {}),
      }),
    },
  );
  const parsed = parseAiActionProposal(result.data);
  if (
    parsed.id !== proposal.id ||
    parsed.fingerprint !== proposal.fingerprint ||
    parsed.generation_id !== proposal.generation_id
  )
    return invalid();
  return parsed;
}

export function needsAutomationEffectsConsent(proposal: AiActionProposal) {
  return (
    proposal.action.action === "automation.enable" ||
    (proposal.action.action === "automation.update" &&
      proposal.preview.after.rule_enabled === true)
  );
}

export async function decideAiFinanceExport(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  consent: boolean,
) {
  if (
    proposal.action.action !== "finance.export_csv" ||
    !validFinanceExport(proposal)
  )
    return invalid();
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm" ? { confirm_finance_export: consent } : {}),
      }),
    },
  );
  const result = parseAiActionProposal(response.data);
  if (
    result.id !== proposal.id ||
    result.generation_id !== proposal.generation_id ||
    result.fingerprint !== proposal.fingerprint ||
    result.action.action !== "finance.export_csv" ||
    result.status !== (decision === "confirm" ? "confirmed" : "rejected") ||
    !Object.entries(proposal.preview.after).every(
      ([key, value]) => result.preview.after[key] === value,
    )
  )
    return invalid();
  return result;
}

export async function decideAiProjectNote(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  confirmDelete: boolean,
) {
  const result = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm" &&
        proposal.action.action === "project_note.delete"
          ? { confirm_project_note_delete: confirmDelete }
          : {}),
      }),
    },
  );
  return parseAiActionProposal(result.data);
}
