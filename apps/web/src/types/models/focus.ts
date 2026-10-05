import type { PageMeta } from "./common";

export interface FocusSettingValue {
  focusMinutes: number;
  breakMinutes: number;
  cycles: number;
  autoStartBreak: boolean;
  autoStartFocus: boolean;
  soundEnabled: boolean;
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
    seconds: number;
    minutes: number;
  };
}

export type FocusSessionStatus =
  | "planned"
  | "active"
  | "paused"
  | "recovery_pending"
  | "completed"
  | "cancelled"
  | "interrupted";

export type FocusSessionEndReason =
  "user_stop" | "completed" | "cancelled" | "crash_recovery";

export type FocusRecoveryAction =
  "include_gap_resume" | "exclude_gap_resume" | "interrupt";

export interface FocusSession {
  id: string;
  taskId: string | null;
  taskTitle: string | null;
  status: FocusSessionStatus;
  plannedSeconds: number;
  accumulatedSeconds: number;
  startedAt: string;
  endedAt: string | null;
  lastResumedAt: string | null;
  lastHeartbeatAt: string | null;
  endReason: FocusSessionEndReason | null;
  version: number;
  createdAt: string;
  updatedAt: string;
}

export interface FocusSessionSnapshot {
  session: FocusSession | null;
  serverNow: string;
  receivedAtMs: number;
}

export type FocusSessionHistoryStatus =
  "terminal" | "completed" | "cancelled" | "interrupted";

export interface FocusSessionListParams {
  page?: number;
  pageSize?: number;
  status?: FocusSessionHistoryStatus;
  taskId?: string;
  projectId?: string;
}

export interface FocusSessionListResult {
  items: FocusSession[];
  meta: PageMeta;
}

export interface FocusReportDay {
  date: string;
  sessions: number;
  seconds: number;
  minutes: number;
}

export interface FocusReportProject {
  projectId: string | null;
  projectName: string | null;
  sessions: number;
  seconds: number;
  minutes: number;
}

export interface FocusReportHour {
  hour: number;
  sessions: number;
  seconds: number;
  minutes: number;
}

export interface FocusReportHeatmapCell {
  weekday: number;
  hour: number;
  sessions: number;
  seconds: number;
  minutes: number;
}

export interface FocusReportTag {
  tagId: string | null;
  tagName: string | null;
  tagColor: string | null;
  sessions: number;
  seconds: number;
  minutes: number;
}

export interface FocusReport {
  dateFrom: string;
  dateTo: string;
  timezone: string;
  totals: {
    sessions: number;
    seconds: number;
    minutes: number;
  };
  days: FocusReportDay[];
  projects: FocusReportProject[];
  hours: FocusReportHour[];
  heatmap: FocusReportHeatmapCell[];
  tags: FocusReportTag[];
  currentStreakDays: number;
  longestStreakDays: number;
}

export interface FocusReportParams {
  dateFrom: string;
  dateTo: string;
  timezone: string;
  projectId?: string;
}

export interface CreateFocusSessionInput {
  taskId: string | null;
  plannedSeconds: number;
}

export interface FocusSessionCommandInput {
  id: string;
  expectedVersion: number;
}

export interface RecoverFocusSessionInput extends FocusSessionCommandInput {
  action: FocusRecoveryAction;
}
