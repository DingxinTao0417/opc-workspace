import type { AiProviderKind } from "./ai";
import type { PageMeta } from "./common";

export type AgentRunStatus =
  "queued" | "running" | "succeeded" | "failed" | "cancelled" | "interrupted";

export type AgentRunOutputDeliveryStatus =
  "not_ready" | "pending" | "submitted" | "retained";

export type AgentRunProgressPhase =
  "preparing" | "calling_model" | "registering_result";

export interface AgentRunProgress {
  phase: AgentRunProgressPhase;
  elapsedMs: number;
}

export type AgentRunStartGateCloseReason =
  | "predecessor_failed"
  | "predecessor_cancelled"
  | "predecessor_retained"
  | "predecessor_not_accepted"
  | "predecessor_unavailable"
  | "predecessor_output_unavailable"
  | "expired"
  | "run_not_queued";

export type AgentRunInputSlotStatus =
  "awaiting_predecessor" | "awaiting_confirmation" | "bound" | "closed";

/**
 * ADR-033: the Run takes the predecessor's accepted files as its inputs. The
 * gate is released only when a person confirms the exact files.
 */
export interface AgentRunStartGateInputSlot {
  source: "predecessor_files";
  status: AgentRunInputSlotStatus;
  expectedFiles: AgentRunOutputFileContract[];
}

/** ADR-030 human-confirmed start precondition, projected by the Sidecar. */
export interface AgentRunStartGate {
  predecessorRunId: string;
  require: "submitted" | "accepted";
  expiresAt: string;
  status: "waiting" | "released" | "closed";
  closeReason?: AgentRunStartGateCloseReason;
  inputSlot?: AgentRunStartGateInputSlot;
}

/** What a Run's input slot holds right now, as read for the confirmation. */
export interface AgentRunStartGateInputs {
  runId: string;
  taskId: string;
  status: AgentRunInputSlotStatus;
  closeReason?: AgentRunStartGateCloseReason;
  expiresAt: string;
  predecessor: {
    runId: string;
    taskId?: string;
    taskTitle?: string;
    attempt?: number;
  };
  expectedFiles: AgentRunOutputFileContract[];
  provider: {
    id: string;
    name: string;
    kind: AiProviderKind;
    model: string;
    version: number;
    configVersion: number;
    leavesDevice: boolean;
  };
  files: AgentRunReworkInputFile[];
  totalBytes: number;
  limits: { maxFiles: number; maxFileBytes: number; maxTotalBytes: number };
  /** Present while there is something to confirm, and after it was confirmed. */
  fingerprint?: string;
}

export interface ConfirmAgentRunStartGateInputs {
  runId: string;
  fingerprint: string;
  provider: AgentRunFileAccessProviderConfirmation;
}

export interface AgentRun {
  id: string;
  taskId: string;
  assignmentId: string;
  actorId: string;
  adapterId: string;
  createdByActorId: string;
  parentRunId: string | null;
  restartOfRunId?: string | null;
  startGate?: AgentRunStartGate;
  attempt: number;
  status: AgentRunStatus;
  /** Ephemeral, content-free stage metadata for native UI polling only. */
  progress?: AgentRunProgress;
  providerId: string;
  model: string;
  /** Server-frozen output shape; it never contains model-controlled paths or bodies. */
  outputContract?: AgentRunOutputContract;
  executionContractVersion?: number;
  reworkContext?: AgentRunReworkContext;
  reworkProviderConfirmation?: AgentRunFileAccessProviderConfirmation;
  reworkInputFiles?: AgentRunReworkInputFile[];
  modelProtocol?: "anthropic_messages" | "openai_chat";
  maxOutputTokens?: 8192 | 0;
  executionProviderConfirmation?: AgentRunFileAccessProviderConfirmation;
  executionInputFiles?: AgentRunReworkInputFile[];
  resultText: string | null;
  resultBytes: number | null;
  errorCode: string | null;
  outputDeliveryStatus: AgentRunOutputDeliveryStatus;
  outputDeliveryErrorCode: string | null;
  submissionId: string | null;
  artifactId: string | null;
  startedAt: string | null;
  completedAt: string | null;
  createdAt: string;
}

export type AgentRunSummary = Omit<
  AgentRun,
  | "resultText"
  | "reworkContext"
  | "reworkProviderConfirmation"
  | "reworkInputFiles"
  | "modelProtocol"
  | "maxOutputTokens"
  | "executionProviderConfirmation"
  | "executionInputFiles"
> & {
  taskTitle: string;
};

export interface AgentRunListParams {
  page?: number;
  pageSize?: number;
  status?: AgentRunStatus;
  outputDeliveryStatus?: AgentRunOutputDeliveryStatus;
  attentionOnly?: boolean;
  unplannedOnly?: boolean;
}

export interface AgentRunListMeta extends PageMeta {
  activeTotal: number;
  pendingDeliveryTotal: number;
  succeededTotal: number;
}

export interface AgentRunListResult {
  items: AgentRunSummary[];
  meta: AgentRunListMeta;
}

export type AgentRunFileSourceKind =
  "task_artifact" | "project_attachment" | "project_task_artifact";

export interface AgentProjectFileSource {
  project_id: string;
  task_id: string;
  task_title: string;
  task_version: number;
  submission_id: string;
  submission_sequence: number;
}

export interface AgentRunFileCandidate {
  sourceTask?: AgentProjectFileSource;
  sourceKind: AgentRunFileSourceKind;
  id: string;
  name: string;
  mime: string;
  sizeBytes: number;
  sha256: string;
  eligible: boolean;
  errorCode: string | null;
  createdAt: string;
}

export interface AgentRunFileLimits {
  maxFiles: number;
  maxFileBytes: number;
  maxTotalBytes: number;
  maxResultBytes: number;
}

export interface AgentRunFileCandidatesResult {
  items: AgentRunFileCandidate[];
  limits: AgentRunFileLimits;
}

export interface AgentRunFileReference {
  sourceTask?: AgentProjectFileSource;
  sha256?: string;
  sourceKind: AgentRunFileSourceKind;
  id: string;
}

export interface AgentRunFileAccessProviderConfirmation {
  version: number;
  configVersion: number;
  kind: AiProviderKind;
}

export interface AgentRunOutputFileContract {
  name: string;
  mime: string;
}

export type AgentRunOutputContract =
  | { type: "text" }
  | { type: "file"; name: string; mime: string }
  | { type: "files"; files: AgentRunOutputFileContract[] };

export interface AgentRunAutoAssign {
  actorId: string;
  expectedTaskVersion: number;
}

export interface CreateAgentRunOptions {
  confirmProjectTaskFiles?: boolean;
  restart?: AgentRunRestartRequest;
  confirmRestart?: boolean;
  restartPreviewHash?: string;
  autoAssign?: AgentRunAutoAssign;
  inputFiles?: AgentRunFileReference[];
  outputContract?: AgentRunOutputContract;
  confirmFileAccess?: boolean;
  fileAccessProviderConfirmation?: AgentRunFileAccessProviderConfirmation;
  rework?: AgentRunReworkRequest;
  confirmReworkContext?: boolean;
  reworkProviderConfirmation?: AgentRunFileAccessProviderConfirmation;
}

export interface AgentRunRestartRequest {
  runId: string;
  expectedTaskVersion: number;
}

export interface AgentRunRestartSource {
  run_id: string;
  task_id: string;
  status: "failed" | "cancelled" | "interrupted" | "succeeded";
  output_delivery_status: "not_ready" | "retained";
  attempt: number;
  task_version: number;
  actor_id: string;
  provider_id: string;
  model: string;
  completed_at: string;
}

export interface AgentRunReworkRequest {
  submissionId: string;
  artifactIds: string[];
  expectedTaskVersion: number;
}

export interface AgentRunReworkContext {
  submission_id: string;
  sequence: number;
  review_reason: string;
  reviewed_at: string;
  artifacts: {
    id: string;
    storage_kind: "text" | "link" | "structured";
    name: string;
    content: string;
    sha256: string;
  }[];
}

export interface AgentRunReworkInputFile {
  source_task?: AgentProjectFileSource;
  source_kind: AgentRunFileSourceKind;
  id: string;
  name: string;
  mime: string;
  size_bytes: number;
  sha256: string;
}

export interface RetryAgentRunReworkOptions {
  confirmProjectTaskFiles?: boolean;
  confirmReworkContext?: boolean;
  reworkProviderConfirmation?: AgentRunFileAccessProviderConfirmation;
  confirmFileAccess?: boolean;
  fileAccessProviderConfirmation?: AgentRunFileAccessProviderConfirmation;
}

export type ControlledFileScope =
  | "artifact"
  | "client_attachment"
  | "project_attachment"
  | "knowledge_document";

export interface ControlledFile {
  id: string;
  scope: ControlledFileScope;
  name: string;
  mimeType: string | null;
  sizeBytes: number | null;
  sha256: string | null;
  ownerLabel: string;
  contentRoute: string;
  updatedAt: string;
}

export interface ControlledFileListParams {
  page?: number;
  pageSize?: number;
  scope?: ControlledFileScope;
}

export interface ControlledFileListResult {
  items: ControlledFile[];
  meta: PageMeta;
}
