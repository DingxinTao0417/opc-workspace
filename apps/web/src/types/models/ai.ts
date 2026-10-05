import type { KnowledgeSourceType } from "./knowledge";

export type AiProviderProtocol = "openai_chat" | "anthropic_messages";

export type AiProviderKind = "remote" | "local";

export type AiProviderStatus =
  "unconfigured" | "checking" | "ready" | "unavailable" | "disabled";

export interface AiProvider {
  configVersion?: number;
  id: string;
  name: string;
  kind: AiProviderKind;
  protocol: AiProviderProtocol;
  base_url: string;
  model: string;
  status: AiProviderStatus;
  health_status: "unknown" | "healthy" | "unhealthy";
  health_error_code: string | null;
  has_key: boolean;
  last_health_at: string | null;
  version: number;
  created_at: string;
  updated_at: string;
}

export interface CreateAiProviderInput {
  name: string;
  kind: AiProviderKind;
  protocol: AiProviderProtocol;
  base_url: string;
  model: string;
}

export interface UpdateAiProviderInput {
  name?: string;
  kind?: AiProviderKind;
  protocol?: AiProviderProtocol;
  base_url?: string;
  model?: string;
}

export interface AiMemory {
  id: string;
  content: string;
  source_message_id?: string | null;
  created_at: string;
  updated_at: string;
}

export interface AiMemoryProposal {
  id: string;
  session_id: string;
  session_title: string;
  content: string;
  tags: string[];
  created_at: string;
}

export type AiBusinessContextType = "task" | "project" | "client";

export interface AiBusinessContextSource {
  type: AiBusinessContextType;
  id: string;
  version: number;
  label: string;
  fields: Record<string, unknown>;
  truncated_fields: string[];
}

export interface AiBusinessContextProviderSnapshot {
  id: string;
  name: string;
  kind: AiProviderKind;
  version: number;
}

export interface AiKnowledgeContextSource {
  source_id: string;
  source_name: string;
  source_version: number;
  source_type: KnowledgeSourceType;
  document_id: string;
  document_title: string;
  document_version: number;
  chunk_id: string;
  chunk_index: number;
  start_char: number;
  end_char: number;
  start_line: number;
  end_line: number;
  start_page?: number;
  end_page?: number;
  content: string;
}

export type AiCitationStatus =
  "not_requested" | "validated" | "no_evidence" | "missing" | "invalid";

export interface AiCitation {
  chunk_id: string;
  source_id: string;
  source_name: string;
  source_type: KnowledgeSourceType;
  source_version: number;
  document_id: string;
  document_title: string;
  document_version: number;
  chunk_index: number;
  start_char: number;
  end_char: number;
  start_line: number;
  end_line: number;
  start_page?: number;
  end_page?: number;
}

export interface AiBusinessContextPreview {
  provider_id: string;
  provider_name: string;
  provider_kind: AiProviderKind;
  provider_version: number;
  leaves_device: boolean;
  serialized_bytes: number;
  sources: AiBusinessContextSource[];
  knowledge: AiKnowledgeContextSource[];
}

export type AiWorkspaceScope =
  | "work"
  | "clients"
  | "outputs"
  | "output_files"
  | "actions"
  | "agent_execution"
  | "agent_files"
  | "agent_project_files"
  | "workspace_ui"
  | "workspace_browser"
  | "knowledge"
  | "knowledge_actions"
  | "finance"
  | "finance_actions"
  | "invoice_actions"
  | "finance_exports";

export type AiWorkspaceRecordNavigationType =
  | "task"
  | "task_saved_view"
  | "project"
  | "client"
  | "client_activity"
  | "client_followup"
  | "project_note"
  | "financial_entry"
  | "invoice"
  | "inbox_item"
  | "reminder"
  | "roadmap_milestone"
  | "content_item"
  | "agent_run"
  | "task_submission"
  | "task_artifact";

export interface AiWorkspaceGrant {
  provider_version: number;
  scopes: AiWorkspaceScope[];
  knowledge_sources?: Array<{
    source_id: string;
    expected_source_version: number;
  }>;
}

export interface AiBusinessContextSelection {
  provider_version: number;
  sources: Array<{
    type: AiBusinessContextType;
    id: string;
    expected_version: number;
  }>;
  knowledge?: Array<{
    source_id: string;
    document_id: string;
    chunk_id: string;
    expected_source_version: number;
    expected_document_version: number;
  }>;
}

export interface AiSession {
  id: string;
  title: string;
  persist: boolean;
  compacted_message_count: number;
  /** Absent when an older Sidecar does not report it; never guessed as 0. */
  message_count?: number;
  version: number;
  created_at: string;
  updated_at: string;
}

export interface AiMessageTaskRef {
  task_id: string;
  task_title_snapshot: string;
}

export interface AiGenerationOrigin {
  kind: "plan_continuation";
  continuation_id: string;
  turn_index: number;
  max_turns: number;
}

export interface AiMessage {
  origin?: AiGenerationOrigin;
  access_request?: { scopes: AiWorkspaceScope[] };
  id: string;
  session_id: string;
  role: "user" | "assistant";
  status: "completed" | "cancelled" | "failed";
  content: string;
  reasoning: string | null;
  task_id: string | null;
  task_title_snapshot: string | null;
  generation_id: string | null;
  context_provider: AiBusinessContextProviderSnapshot | null;
  context_sources: AiBusinessContextSource[];
  context_knowledge: AiKnowledgeContextSource[];
  citation_status: AiCitationStatus;
  citations: AiCitation[];
  created_at: string;
}

export interface AiMessageListResult {
  data: AiMessage[];
  meta: {
    has_more: boolean;
    oldest_created_at?: string;
    oldest_id?: string;
  };
}

// Undefined evidence means unavailable, not a verified absence of sources.
export interface AiCitationResult {
  status: AiCitationStatus;
  items: AiCitation[];
}

export interface AiChatStreamMeta {
  protocol: string;
  generation_id: string;
  session_id: string;
  model: string;
  provider_id: string;
  sse_protocol: string;
}

export interface AiRunProgress {
  sequence: number;
  kind:
    | "model_turn"
    | "tool_call"
    | "self_check"
    | "citation_validation"
    | "persistence";
  status: "running" | "succeeded" | "failed" | "cancelled";
  turn_index?: number;
  tool_name?: string;
  started_at: string;
  completed_at?: string;
  duration_ms: number;
  retry_count?: number;
  retry_reason?: string;
  trimmed_history_turns?: number;
  compacted_tool_results?: number;
}

export type AiWorkspacePanel =
  | "overview"
  | "agents"
  | "files"
  | "review"
  | "terminal"
  | "browser"
  | "managed";

export interface AiProjectFileProposal {
  id: string;
  path: string;
  baseSHA256: string;
  content: string;
}

export type AiChatStreamEvent =
  | { type: "meta"; meta: AiChatStreamMeta }
  | {
      type: "project_file_proposal";
      generationId: string;
      proposal: AiProjectFileProposal;
    }
  | { type: "progress"; generationId: string; step: AiRunProgress }
  | {
      type: "workspace_panel";
      generationId: string;
      panel: AiWorkspacePanel;
    }
  | {
      type: "workspace_panels";
      generationId: string;
      panels: [AiWorkspacePanel, AiWorkspacePanel];
      splitRatio?: number;
    }
  | {
      type: "workspace_browser_navigation";
      generationId: string;
      url: string;
    }
  | {
      type: "workspace_browser_action";
      generationId: string;
      action: "back" | "forward" | "reload" | "stop";
    }
  | {
      type: "workspace_access_request";
      generationId: string;
      scopes: AiWorkspaceScope[];
    }
  | {
      type: "workspace_plan_updated";
      generationId: string;
      version: number;
      stepCount: number;
    }
  | {
      type: "workspace_record_navigation";
      generationId: string;
      recordType: Exclude<
        AiWorkspaceRecordNavigationType,
        | "agent_run"
        | "task_submission"
        | "task_artifact"
        | "project_note"
        | "client_activity"
        | "client_followup"
      >;
      recordId: string;
    }
  | {
      type: "workspace_record_navigation";
      generationId: string;
      recordType: "agent_run" | "task_submission";
      recordId: string;
      taskId: string;
    }
  | {
      type: "workspace_record_navigation";
      generationId: string;
      recordType: "task_artifact";
      recordId: string;
      taskId: string;
      submissionId: string;
    }
  | {
      type: "workspace_record_navigation";
      generationId: string;
      recordType: "project_note" | "client_activity" | "client_followup";
      recordId: string;
      parentId: string;
    }
  | { type: "delta"; generationId: string; text: string }
  | { type: "reasoning"; generationId: string; text: string }
  | {
      type: "replace";
      generationId: string;
      text: string;
      reasoning: string;
    }
  | { type: "done"; generationId: string; citationEvidence?: AiCitationResult }
  | { type: "cancelled"; generationId: string; partialText: string }
  | {
      type: "error";
      generationId: string;
      error: string;
      detail?: string;
      partialText?: string;
    };
