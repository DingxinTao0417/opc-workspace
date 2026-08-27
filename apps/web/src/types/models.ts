export type TaskStatus = "todo" | "in_progress" | "done";
export type TaskPriority = "P0" | "P1" | "P2" | "P3";

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
