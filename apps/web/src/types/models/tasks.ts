import type { ActorSummary } from "./actors";
import type { PageMeta } from "./common";
import type { Tag } from "./tags";

export type TaskStatus =
  "todo" | "in_progress" | "blocked" | "waiting_review" | "done" | "cancelled";

export type TaskPriority = "P0" | "P1" | "P2" | "P3";

export type TaskKind = "work" | "review" | "followup" | "reminder";

export type TaskReviewPolicy = "none" | "manual";

export type TaskLifecycleAction =
  "start" | "block" | "unblock" | "complete" | "cancel" | "reopen";

export interface Task {
  id: string;
  title: string;
  description: string;
  kind: TaskKind;
  status: TaskStatus;
  priority: TaskPriority;
  projectId: string | null;
  projectName?: string;
  parentTaskId: string | null;
  parentTaskTitle?: string;
  completionCriteria: string;
  reviewPolicy: TaskReviewPolicy;
  blockedReason: string | null;
  blockedAt: string | null;
  blockedFromStatus: Extract<
    TaskStatus,
    "todo" | "in_progress" | "waiting_review"
  > | null;
  dueDate: string | null;
  plannedDate: string | null;
  estimatedMinutes: number | null;
  actualMinutes: number;
  manualOrder: number | null;
  version: number;
  subtaskTotal: number;
  subtaskCompleted: number;
  /** Added in schema v30; optional keeps durable v29 idempotency snapshots readable. */
  subtaskCancelled?: number;
  createdAt: string;
  updatedAt: string;
  completedAt: string | null;
  submittedAt: string | null;
  reviewedAt: string | null;
  currentSubmissionId: string | null;
  tags: Tag[];
}

export interface TaskWorkflowEvent {
  id: string;
  action: string;
  actor: ActorSummary | null;
  assignmentId: string | null;
  submissionId: string | null;
  artifactId: string | null;
  requestId: string | null;
  commandSeq: number | null;
  previous: Record<string, unknown> | null;
  current: Record<string, unknown> | null;
  reason: string | null;
  createdAt: string;
}

export interface TaskEventListParams {
  page?: number;
  pageSize?: number;
}

export interface TaskEventListMeta extends PageMeta {
  taskVersion: number;
}

export interface TaskEventListResult {
  items: TaskWorkflowEvent[];
  meta: TaskEventListMeta;
}

export type TaskLifecycleCommandInput =
  | {
      action: Exclude<TaskLifecycleAction, "block" | "cancel">;
      expectedVersion: number;
    }
  | {
      action: "block" | "cancel";
      reason: string;
      expectedVersion: number;
    };

export interface TaskLifecycleCommandResult {
  task: Task;
  event: TaskWorkflowEvent;
}

export interface NewTaskInput {
  title: string;
  description?: string;
  kind?: TaskKind;
  priority: TaskPriority;
  projectId?: string | null;
  parentTaskId?: string | null;
  completionCriteria?: string;
  reviewPolicy?: TaskReviewPolicy;
  tagIds?: string[];
  dueDate?: string | null;
  plannedDate?: string | null;
  estimatedMinutes?: number | null;
  manualOrder?: number | null;
}

export interface UpdateTaskInput {
  title: string;
  description: string;
  kind?: TaskKind;
  priority: TaskPriority;
  projectId?: string | null;
  parentTaskId?: string | null;
  completionCriteria?: string;
  reviewPolicy?: TaskReviewPolicy;
  tagIds?: string[];
  dueDate: string | null;
  plannedDate: string | null;
  estimatedMinutes: number | null;
  expectedVersion?: number;
}

export interface TaskListResult {
  items: Task[];
  meta: PageMeta;
}

export interface TaskVersionItem {
  id: string;
  expectedVersion: number;
}

export type BatchUpdateTasksInput =
  | {
      action: "set_priority";
      items: TaskVersionItem[];
      priority: TaskPriority;
    }
  | {
      action: "set_due_date";
      items: TaskVersionItem[];
      dueDate: string | null;
    }
  | {
      action: "set_project";
      items: TaskVersionItem[];
      projectId: string | null;
    }
  | {
      action: "set_planned_date";
      items: TaskVersionItem[];
      plannedDate: string | null;
    }
  | {
      action: "add_tags" | "remove_tags";
      items: TaskVersionItem[];
      tagIds: string[];
    }
  | {
      action: Exclude<TaskLifecycleAction, "block" | "cancel">;
      items: TaskVersionItem[];
    }
  | {
      action: "block" | "cancel";
      items: TaskVersionItem[];
      reason: string;
    };

export interface BatchUpdateTasksResult {
  action: BatchUpdateTasksInput["action"];
  changed: number;
  tasks: Task[];
}

export interface ReorderTasksInput {
  plannedDate: string | null;
  mode: "manual" | "default";
  items: TaskVersionItem[];
}

export interface ReorderTasksResult {
  plannedDate: string | null;
  mode: ReorderTasksInput["mode"];
  changed: number;
  tasks: Task[];
}

export interface TaskListParams {
  page?: number;
  pageSize?: number;
  q?: string;
  kind?: TaskKind;
  status?: TaskStatus | "active";
  priority?: TaskPriority;
  projectId?: string;
  clientId?: string;
  tagIds?: string[];
  plannedDate?: string;
  plannedFrom?: string;
  plannedTo?: string;
  plannedState?: "scheduled" | "unscheduled";
  dueFrom?: string;
  dueTo?: string;
  dueState?: "overdue" | "due_soon";
  parentTaskId?: string;
  rootOnly?: boolean;
  sort?: string;
}

export interface TaskSavedViewDefinition {
  q: string;
  status: TaskStatus | "active" | "";
  priority: TaskPriority | "";
  kind: TaskKind | "";
  projectId: string;
  clientId: string;
  tagIds: string[];
  plannedDate: string;
  plannedFrom: string;
  plannedTo: string;
  dueFrom: string;
  dueTo: string;
  sort: string;
}

export interface TaskSavedView {
  id: string;
  name: string;
  definition: TaskSavedViewDefinition;
  schemaVersion: number;
  version: number;
  createdAt: string;
  updatedAt: string;
}

export interface CreateTaskSavedViewInput {
  name: string;
  definition: TaskSavedViewDefinition;
}

export interface UpdateTaskSavedViewInput {
  expectedVersion: number;
  name?: string;
  definition?: TaskSavedViewDefinition;
}

export interface DeleteTaskSavedViewResult {
  deletedId: string;
}
