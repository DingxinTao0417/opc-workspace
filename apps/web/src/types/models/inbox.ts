import type { ActorSummary } from "./actors";
import type { PageMeta } from "./common";
import type { TaskAssignment } from "./taskAssignments";
import type {
  Task,
  TaskKind,
  TaskPriority,
  TaskReviewPolicy,
  TaskStatus,
} from "./tasks";

export interface InboxStats {
  serverNow: string;
  pending: number;
  unread: number;
  tracking: number;
  blocked: number;
  waitingReview: number;
}

export type InboxItemKind = "manual" | "reminder" | "event";

export type InboxItemPriority = TaskPriority;

export type InboxItemStatus = "open" | "tracking" | "resolved" | "dismissed";

export type InboxItemView = "inbox" | "snoozed" | "archive";

export type InboxItemRisk = "tracking" | "blocked" | "waiting_review";

export type InboxResolutionPolicy = "manual" | "all_required_tasks_done";

export type InboxResolutionMode = "manual" | "forced" | "automatic";

export type InboxItemAction =
  | "read"
  | "edit"
  | "snooze"
  | "unsnooze"
  | "resolve"
  | "force-resolve"
  | "dismiss"
  | "reopen";

export interface InboxItem {
  id: string;
  kind: InboxItemKind;
  title: string;
  summary: string;
  sourceEntityType:
    | "manual"
    | "reminder"
    | "task_artifact"
    | "task"
    | "task_due"
    | "invoice_due"
    | "client_followup"
    | "content_item"
    | "roadmap_milestone"
    | "project_completion"
    | "automation"
    | "agent_run_failed"
    | "system_maintenance";
  sourceEntityId: string | null;
  sourceEventKey: string | null;
  sourceDeletedAt: string | null;
  priority: InboxItemPriority;
  status: InboxItemStatus;
  resolutionPolicy: InboxResolutionPolicy;
  dueAt: string | null;
  readAt: string | null;
  triagedAt: string | null;
  snoozedUntil: string | null;
  resolvedByActorId: string | null;
  resolvedAt: string | null;
  resolutionReason: string | null;
  resolutionMode: InboxResolutionMode | null;
  dismissedByActorId: string | null;
  dismissedAt: string | null;
  dismissReason: string | null;
  payloadJson: Record<string, unknown>;
  version: number;
  createdAt: string;
  updatedAt: string;
  availableActions: InboxItemAction[];
}

export interface InboxItemListParams {
  view?: InboxItemView;
  q?: string;
  priority?: InboxItemPriority;
  risk?: InboxItemRisk;
  sourceEntityType?: InboxItem["sourceEntityType"];
  page?: number;
  pageSize?: number;
}

export interface InboxItemListMeta extends PageMeta {
  unreadTotal: number;
  snapshotAt: string;
  serverNow: string;
}

export interface InboxItemListResult {
  items: InboxItem[];
  meta: InboxItemListMeta;
}

export type InboxTaskRelationType = "created" | "linked";

export interface InboxTaskSummary {
  id: string;
  title: string;
  status: TaskStatus;
  priority: TaskPriority;
  kind: TaskKind;
  projectId: string | null;
  projectName: string | null;
  version: number;
}

export interface InboxItemTaskRelation {
  id: string;
  inboxItemId: string;
  taskRefId: string;
  taskId: string | null;
  taskTitleSnapshot: string;
  task: InboxTaskSummary | null;
  relationType: InboxTaskRelationType;
  isRequired: boolean;
  position: number;
  linkedByActorId: string;
  linkedByActor: ActorSummary;
  linkedAt: string;
  unlinkedByActorId: string | null;
  unlinkedByActor: ActorSummary | null;
  unlinkedAt: string | null;
  unlinkReason: string | null;
  isActive: boolean;
  taskDeleted: boolean;
}

export interface InboxTaskProgress {
  activeTotal: number;
  requiredTotal: number;
  requiredDone: number;
  requiredRemaining: number;
  requiredBlocked: number;
  requiredWaitingReview: number;
  requiredCancelled: number;
  percent: number | null;
  allRequiredDone: boolean;
}

export interface InboxItemTaskListMeta extends PageMeta {
  inboxItemVersion: number;
  progress: InboxTaskProgress;
}

export interface InboxItemTaskListResult {
  active: InboxItemTaskRelation[];
  history: InboxItemTaskRelation[];
  meta: InboxItemTaskListMeta;
}

export interface InboxItemTaskListParams {
  page?: number;
  pageSize?: number;
}

export interface InboxItemTaskMutationResult {
  inboxItem: InboxItem;
  relation: InboxItemTaskRelation;
  progress: InboxTaskProgress;
}

export interface LinkInboxItemTaskInput {
  inboxItemId: string;
  taskId: string;
  isRequired: boolean;
  expectedVersion: number;
}

export type UpdateInboxItemTaskRequirementInput = LinkInboxItemTaskInput;

export interface UnlinkInboxItemTaskInput {
  inboxItemId: string;
  taskId: string;
  reason: string;
  expectedVersion: number;
}

export interface InboxSplitTaskInput {
  key: string;
  parentKey: string | null;
  title: string;
  description: string;
  kind: TaskKind;
  priority: TaskPriority;
  projectId: string | null;
  completionCriteria: string;
  tagIds: string[];
  dueDate: string | null;
  plannedDate: string | null;
  estimatedMinutes: number | null;
  reviewPolicy: TaskReviewPolicy;
  isRequired: boolean;
  assigneeActorId: string;
}

export interface SplitInboxItemInput {
  inboxItemId: string;
  expectedVersion: number;
  resolutionPolicy: InboxResolutionPolicy;
  tasks: InboxSplitTaskInput[];
}

export interface InboxSplitTaskResult {
  key: string;
  task: Task;
  assignments: TaskAssignment[];
  relation: InboxItemTaskRelation;
}

export interface SplitInboxItemResult {
  inboxItem: InboxItem;
  created: InboxSplitTaskResult[];
  progress: InboxTaskProgress;
}

export interface ForceResolveInboxItemInput {
  id: string;
  expectedVersion: number;
  reason: string;
  confirm: true;
}

export interface CreateInboxItemInput {
  title: string;
  summary: string;
  priority: InboxItemPriority;
  dueAt: string | null;
}

export interface UpdateInboxItemInput {
  title?: string;
  summary?: string;
  priority?: InboxItemPriority;
  dueAt?: string | null;
  expectedVersion: number;
}

export type InboxItemCommandInput =
  | {
      action: "read" | "unsnooze" | "reopen";
      id: string;
      expectedVersion: number;
    }
  | {
      action: "snooze";
      id: string;
      snoozedUntil: string;
      expectedVersion: number;
    }
  | {
      action: "resolve" | "dismiss";
      id: string;
      reason: string;
      expectedVersion: number;
    };

export interface MarkAllInboxReadInput {
  throughCreatedAt: string;
}

export interface MarkAllInboxReadResult {
  throughCreatedAt: string;
  markedCount: number;
}

export interface InboxWorkflowEvent {
  id: string;
  action: string;
  actorId: string | null;
  actor: ActorSummary | null;
  requestId: string | null;
  previous: Record<string, unknown> | null;
  current: Record<string, unknown> | null;
  reason: string | null;
  createdAt: string;
}

export interface InboxEventListParams {
  page?: number;
  pageSize?: number;
}

export interface InboxEventListMeta extends PageMeta {
  inboxItemVersion: number;
}

export interface InboxEventListResult {
  items: InboxWorkflowEvent[];
  meta: InboxEventListMeta;
}
