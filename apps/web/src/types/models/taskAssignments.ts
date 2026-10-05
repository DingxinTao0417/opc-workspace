import type { ActorSummary } from "./actors";
import type { PageMeta } from "./common";
import type { Task } from "./tasks";

export type AssignmentRole = "assignee" | "reviewer";

export interface TaskAssignment {
  id: string;
  taskId: string;
  role: AssignmentRole;
  actorId: string;
  actor: ActorSummary;
  assignedByActorId: string;
  assignedByActor: ActorSummary;
  assignedAt: string;
  unassignedAt: string | null;
  reason: string | null;
  isActive: boolean;
  inferred: boolean;
}

export interface TaskAssignmentListParams {
  page?: number;
  pageSize?: number;
  role?: AssignmentRole;
  sort?: string;
}

export interface TaskAssignmentListMeta extends PageMeta {
  taskVersion: number;
}

export interface TaskAssignmentListResult {
  active: Record<AssignmentRole, TaskAssignment | null>;
  history: TaskAssignment[];
  meta: TaskAssignmentListMeta;
}

export interface CreateTaskAssignmentInput {
  role: AssignmentRole;
  actorId: string;
  expectedVersion: number;
}

export interface ReassignTaskAssignmentInput extends CreateTaskAssignmentInput {
  reason: string;
}

export interface EndTaskAssignmentInput {
  reason: string;
  expectedVersion: number;
}

export interface TaskAssignmentMutationResult {
  assignment: TaskAssignment;
  task: Task;
}

export interface ReassignTaskAssignmentResult extends TaskAssignmentMutationResult {
  previousAssignment: TaskAssignment;
}
