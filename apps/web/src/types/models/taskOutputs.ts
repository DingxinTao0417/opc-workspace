import type { ActorSummary } from "./actors";
import type { PageMeta } from "./common";
import type { Task, TaskWorkflowEvent } from "./tasks";

export type TaskSubmissionStatus =
  "pending_review" | "accepted" | "changes_requested" | "withdrawn";

export type TaskSubmissionOrigin = "manual" | "child_rollup";

export type TaskArtifactStorageKind = "text" | "link" | "structured" | "file";

export type TaskArtifactIntegrityStatus =
  "unverified" | "verified" | "missing" | "mismatch";

export interface TaskArtifactSummary {
  id: string;
  taskId: string;
  submissionId: string;
  submissionStatus: TaskSubmissionStatus;
  position: number;
  storageKind: TaskArtifactStorageKind;
  name: string;
  mimeType: string | null;
  sizeBytes: number | null;
  sha256: string | null;
  requiresFollowup: boolean;
  producedByActorId: string;
  producedByActor: ActorSummary;
  recordedByActorId: string;
  recordedByActor: ActorSummary;
  integrityStatus: TaskArtifactIntegrityStatus;
  integrityCheckedAt: string | null;
  deletedAt: string | null;
  deletedByActorId: string | null;
  deletedByActor: ActorSummary | null;
  deleteReason: string | null;
  createdAt: string;
}

export interface TaskArtifact extends TaskArtifactSummary {
  contentText: string | null;
  referenceUrl: string | null;
  structuredJson: Record<string, unknown> | null;
}

export interface TaskSubmission {
  id: string;
  taskId: string;
  sequence: number;
  status: TaskSubmissionStatus;
  origin: TaskSubmissionOrigin;
  summary: string;
  submittedByActorId: string;
  submittedByActor: ActorSummary;
  submittedAt: string;
  reviewedByActorId: string | null;
  reviewedByActor: ActorSummary | null;
  reviewedAt: string | null;
  reviewReason: string | null;
  withdrawnByActorId: string | null;
  withdrawnByActor: ActorSummary | null;
  withdrawnAt: string | null;
  isInferred: boolean;
  artifacts: TaskArtifactSummary[];
}

export interface TaskSubmissionListParams {
  page?: number;
  pageSize?: number;
}

export interface TaskArtifactListParams {
  page?: number;
  pageSize?: number;
  submissionId?: string;
  includeDeleted?: boolean;
}

export interface TaskAggregateListMeta extends PageMeta {
  taskVersion: number;
}

export interface TaskSubmissionListResult {
  items: TaskSubmission[];
  meta: TaskAggregateListMeta;
}

export interface TaskArtifactListResult {
  items: TaskArtifactSummary[];
  meta: TaskAggregateListMeta;
}

interface NewArtifactBase {
  clientRef: string;
  name: string;
  requiresFollowup: boolean;
}

export type NewTaskArtifactInput =
  | (NewArtifactBase & {
      storageKind: "text";
      contentText: string;
    })
  | (NewArtifactBase & {
      storageKind: "link";
      referenceUrl: string;
    })
  | (NewArtifactBase & {
      storageKind: "structured";
      structuredJson: Record<string, unknown>;
    })
  | (NewArtifactBase & {
      storageKind: "file";
      file: File;
    });

export interface SubmitTaskOutputInput {
  summary: string;
  artifacts: NewTaskArtifactInput[];
  expectedVersion: number;
}

export interface SubmitTaskOutputResult {
  task: Task;
  submission: TaskSubmission;
  artifacts: TaskArtifactSummary[];
  event: TaskWorkflowEvent;
}

export type ReviewTaskSubmissionInput =
  | {
      decision: "accept";
      expectedVersion: number;
    }
  | {
      decision: "request_changes";
      reason: string;
      expectedVersion: number;
    };

export interface ReviewTaskSubmissionResult {
  task: Task;
  submission: TaskSubmission;
  event: TaskWorkflowEvent;
}

export interface DeleteTaskArtifactInput {
  reason: string;
  expectedVersion: number;
}

export interface DeleteTaskArtifactResult {
  task: Task;
  artifact: TaskArtifactSummary;
  event: TaskWorkflowEvent;
}

export interface TaskArtifactDownload {
  blob: Blob;
  fileName: string;
  mimeType: string;
}
