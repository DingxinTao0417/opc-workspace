import type { ActorSummary, ActorType } from "./actors";
import type {
  ClientActivityActor,
  ClientAttachmentDownload,
  ClientAttachmentIntegrityStatus,
} from "./clients";
import type { PageMeta } from "./common";
import type {
  InboxItemStatus,
  InboxResolutionPolicy,
  InboxTaskProgress,
} from "./inbox";
import type { TaskArtifactSummary } from "./taskOutputs";
import type { TaskStatus } from "./tasks";

export type ProjectStatus =
  "planning" | "in_progress" | "paused" | "completed" | "archived";

export type ProjectTransitionAction =
  "start" | "pause" | "resume" | "complete" | "reopen" | "archive" | "restore";

export interface ProjectArtifactItem {
  artifact: TaskArtifactSummary;
  task: {
    id: string;
    title: string;
    status: TaskStatus;
  };
  submissionSequence: number;
  followup: {
    inboxItemId: string;
    inboxItemVersion: number;
    status: InboxItemStatus;
    resolutionPolicy: InboxResolutionPolicy;
    sourceDeletedAt: string | null;
    progress: InboxTaskProgress;
  } | null;
}

export interface ProjectArtifactListParams {
  page?: number;
  pageSize?: number;
  includeDeleted?: boolean;
}

export interface ProjectArtifactListResult {
  items: ProjectArtifactItem[];
  meta: PageMeta & { projectVersion: number };
}

export interface ProjectTaskSummary {
  total: number;
  completed: number;
  inProgress: number;
  remaining: number;
  progressPercent: number;
  actualMinutes: number;
}

export interface Project {
  id: string;
  name: string;
  description: string;
  clientId: string | null;
  clientName: string | null;
  status: ProjectStatus;
  startDate: string | null;
  dueDate: string | null;
  amountMinor: number | null;
  color: string | null;
  version: number;
  archivedFromStatus: Exclude<ProjectStatus, "archived"> | null;
  createdAt: string;
  updatedAt: string;
  taskSummary: ProjectTaskSummary;
  invoiceCount: number;
  availableActions: ProjectTransitionAction[];
}

export interface ProjectListResult {
  items: Project[];
  meta: PageMeta;
}

export interface ProjectWorkflowEvent {
  id: string;
  action: string;
  actor: ActorSummary | null;
  requestId: string | null;
  previous: Record<string, unknown> | null;
  current: Record<string, unknown> | null;
  createdAt: string;
}

export interface ProjectEventListParams {
  page?: number;
  pageSize?: number;
}

export interface ProjectEventListMeta extends PageMeta {
  projectVersion: number;
}

export interface ProjectEventListResult {
  items: ProjectWorkflowEvent[];
  meta: ProjectEventListMeta;
}

export interface ProjectNoteActor {
  id: string;
  type: ActorType;
  displayName: string;
}

export interface ProjectNote {
  id: string;
  projectId: string;
  title: string;
  body: string | null;
  occurredAt: string;
  createdBy: ProjectNoteActor;
  version: number;
  deletedAt: string | null;
  deletedByActorId: string | null;
  deleteReason: string | null;
  createdAt: string;
  updatedAt: string;
  projectVersion: number;
}

export interface ProjectNoteListParams {
  page?: number;
  pageSize?: number;
  includeDeleted?: boolean;
}

export interface ProjectNoteListResult {
  items: ProjectNote[];
  meta: PageMeta & { projectVersion: number };
}

export interface CreateProjectNoteInput {
  title: string;
  body: string;
  occurredAt: string;
}

export interface UpdateProjectNoteInput {
  title?: string;
  body?: string;
  occurredAt?: string;
  expectedVersion: number;
}

export interface DeleteProjectNoteInput {
  reason: string;
  expectedVersion: number;
}

export interface ProjectListParams {
  page?: number;
  pageSize?: number;
  query?: string;
  status?: ProjectStatus;
  clientId?: string;
  includeArchived?: boolean;
  sort?: string;
}

export interface ProjectInput {
  name: string;
  description: string;
  clientId: string | null;
  startDate: string | null;
  dueDate: string | null;
  amountMinor: number | null;
  color: string | null;
}

export interface UpdateProjectInput extends ProjectInput {
  expectedVersion: number;
}

export interface DeleteProjectResult {
  deletedId: string;
  detachedTasks: number;
  detachedInvoices: number;
  detachedFinancialEntries: number;
}

export interface ProjectAttachment {
  id: string;
  projectId: string;
  name: string;
  mimeType: string;
  sizeBytes: number;
  sha256: string;
  recordedBy: ClientActivityActor;
  integrityStatus: ClientAttachmentIntegrityStatus;
  integrityCheckedAt: string;
  deletedAt: string | null;
  deletedByActorId: string | null;
  deleteReason: string | null;
  createdAt: string;
  projectVersion: number;
}

export interface ProjectAttachmentListParams {
  page?: number;
  pageSize?: number;
  includeDeleted?: boolean;
}

export interface ProjectAttachmentListResult {
  items: ProjectAttachment[];
  meta: PageMeta & { projectVersion: number };
}

export interface CreateProjectAttachmentInput {
  file: File;
  name: string;
  expectedVersion: number;
}

export interface DeleteProjectAttachmentInput {
  reason: string;
  expectedVersion: number;
}

export type ProjectAttachmentDownload = ClientAttachmentDownload;
