export type TaskStatus = "todo" | "in_progress" | "done";
export type TaskPriority = "P0" | "P1" | "P2" | "P3";
export type TaskKind = "work" | "review" | "followup" | "reminder";
export type ProjectStatus =
  "planning" | "in_progress" | "paused" | "completed" | "archived";
export type ProjectTransitionAction =
  "start" | "pause" | "resume" | "complete" | "reopen" | "archive" | "restore";
export type ActorType = "owner" | "person" | "system" | "agent";
export type ActorStatus = "active" | "inactive";
export type AssignmentRole = "assignee" | "reviewer";

export interface Actor {
  id: string;
  type: ActorType;
  displayName: string;
  status: ActorStatus;
  isBuiltin: boolean;
  notes: string;
  metadata: Record<string, unknown>;
  version: number;
  createdAt: string;
  updatedAt: string;
}

export interface ActorListParams {
  page?: number;
  pageSize?: number;
  type?: ActorType;
  status?: ActorStatus;
  sort?: string;
}

export interface ActorListResult {
  items: Actor[];
  meta: PageMeta;
}

export interface ActorSummary {
  id: string;
  type: ActorType;
  displayName: string;
  status: ActorStatus;
  isBuiltin: boolean;
  version: number;
}

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

export interface CreatePersonActorInput {
  displayName: string;
  notes?: string;
  metadata?: Record<string, unknown>;
  status?: ActorStatus;
}

export interface UpdateActorInput {
  displayName?: string;
  notes?: string;
  metadata?: Record<string, unknown>;
  status?: ActorStatus;
  expectedVersion: number;
}

export interface Tag {
  id: string;
  name: string;
  color: string;
  version: number;
  createdAt: string;
}

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
  dueDate: string | null;
  plannedDate: string | null;
  estimatedMinutes: number | null;
  actualMinutes: number;
  manualOrder: number | null;
  version: number;
  subtaskTotal: number;
  subtaskCompleted: number;
  createdAt: string;
  updatedAt: string;
  completedAt: string | null;
  tags: Tag[];
}

export interface HealthResponse {
  status: string;
  app?: {
    name?: string;
    version?: string;
    commit?: string;
  };
  api?: {
    version?: string;
  };
  schema?: {
    version?: number;
  };
}

export interface TodayStats {
  date: string;
  tasks: {
    total: number;
    completed: number;
    remaining: number;
    overdue: number;
    dueSoon: number;
    estimatedMinutes: number;
    actualMinutes: number;
  };
  focus: {
    sessions: number;
    minutes: number;
  };
}

export interface NewTaskInput {
  title: string;
  description?: string;
  kind?: TaskKind;
  status: TaskStatus;
  priority: TaskPriority;
  projectId?: string | null;
  parentTaskId?: string | null;
  completionCriteria?: string;
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
  tagIds?: string[];
  dueDate: string | null;
  plannedDate: string | null;
  estimatedMinutes: number | null;
  expectedVersion?: number;
}

export interface PageMeta {
  page: number;
  pageSize: number;
  total: number;
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
  status?: TaskStatus;
  priority?: TaskPriority;
  projectId?: string;
  tagIds?: string[];
  plannedDate?: string;
  plannedFrom?: string;
  plannedTo?: string;
  parentTaskId?: string;
  rootOnly?: boolean;
  sort?: string;
}

export interface TagListResult {
  items: Tag[];
  meta: PageMeta;
}

export interface TagListParams {
  page?: number;
  pageSize?: number;
  query?: string;
  sort?: string;
}

export interface TagInput {
  name: string;
  color: string;
}

export interface UpdateTagInput {
  name?: string;
  color?: string;
  expectedVersion: number;
}

export interface DeleteTagResult {
  deletedId: string;
  detachedTasks: number;
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

export interface ProjectListParams {
  page?: number;
  pageSize?: number;
  query?: string;
  status?: ProjectStatus;
  clientId?: string;
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
}
