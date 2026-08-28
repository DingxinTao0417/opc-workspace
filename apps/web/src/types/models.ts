export type TaskStatus = "todo" | "in_progress" | "done";
export type TaskPriority = "P0" | "P1" | "P2" | "P3";
export type ProjectStatus =
  "planning" | "in_progress" | "paused" | "completed" | "archived";
export type ProjectTransitionAction =
  "start" | "pause" | "resume" | "complete" | "reopen" | "archive" | "restore";

export interface Task {
  id: string;
  title: string;
  description: string;
  status: TaskStatus;
  priority: TaskPriority;
  projectId: string | null;
  projectName?: string;
  dueDate: string | null;
  plannedDate: string | null;
  estimatedMinutes: number | null;
  actualMinutes: number;
  createdAt: string;
  updatedAt: string;
  completedAt: string | null;
  tags?: string[];
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
  status: TaskStatus;
  priority: TaskPriority;
  projectId?: string | null;
  dueDate?: string | null;
  plannedDate?: string | null;
  estimatedMinutes?: number | null;
}

export interface UpdateTaskInput {
  title: string;
  description: string;
  priority: TaskPriority;
  projectId?: string | null;
  dueDate: string | null;
  plannedDate: string | null;
  estimatedMinutes: number | null;
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

export interface TaskListParams {
  page?: number;
  pageSize?: number;
  projectId?: string;
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
