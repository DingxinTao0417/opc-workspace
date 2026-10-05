import type {
  AgentProjectFileSource,
  AgentRunOutputDeliveryStatus,
  AgentRunRestartSource,
  AgentRunReworkContext,
} from "../../types/models";
import type {
  AiAutomationRetryPreview,
  AiAutomationRetryResult,
} from "../aiAutomationRetry";
import type { AiInvoicePdfResult } from "../aiInvoiceActions";
import type { AiTaskOutputPreview } from "../aiTaskOutputActions";
import type { aiActionLabels } from "./catalog";

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
  execution_contract_version: 1 | 2 | 3 | 4 | 5 | 6 | 7;
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
  /** ADR-033: the Run will take the predecessor's accepted files as inputs. */
  input_slot?: AiAgentRunInputSlotPreview;
}

export interface AiAgentRunInputSlotPreview {
  source: "predecessor_files";
  expected_files: Array<{ name: string; mime: string }>;
  max_files: 4;
  max_file_bytes: 65536;
  max_total_bytes: 131072;
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
    input_slot?: {
      source: "predecessor_files";
      status:
        "awaiting_predecessor" | "awaiting_confirmation" | "bound" | "closed";
      expected_files: Array<{ name: string; mime: string }>;
    };
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
