import type { Task, TaskStatus } from "../types/models";

// Matches the sidecar task due Inbox projection lead (1440 minutes).
const DUE_SOON_LEAD_MS = 24 * 60 * 60 * 1000;

const ACTIVE_STATUSES: ReadonlySet<TaskStatus> = new Set([
  "todo",
  "in_progress",
  "blocked",
  "waiting_review",
]);

const WEEKDAYS = ["周日", "周一", "周二", "周三", "周四", "周五", "周六"];

export type TaskDueState = "overdue" | "due_soon";

function pad(value: number): string {
  return String(value).padStart(2, "0");
}

export function localDayKey(time: number): string {
  const date = new Date(time);
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(
    date.getDate(),
  )}`;
}

function shiftLocalDay(time: number, days: number): string {
  const date = new Date(time);
  date.setDate(date.getDate() + days);
  return localDayKey(date.getTime());
}

export function taskDueDayKey(task: Pick<Task, "dueDate">): string | null {
  if (!task.dueDate) return null;
  const due = Date.parse(task.dueDate);
  if (Number.isNaN(due)) return null;
  return localDayKey(due);
}

export function taskDueState(
  task: Pick<Task, "dueDate" | "status">,
  now: number,
): TaskDueState | null {
  if (!task.dueDate || !ACTIVE_STATUSES.has(task.status)) return null;
  const due = Date.parse(task.dueDate);
  if (Number.isNaN(due)) return null;
  if (due <= now) return "overdue";
  return due - now <= DUE_SOON_LEAD_MS ? "due_soon" : null;
}

export function taskDueStateClass(state: TaskDueState | null): string {
  if (state === "overdue") return " task-due-overdue";
  if (state === "due_soon") return " task-due-soon";
  return "";
}

export function formatTaskDue(value: string, now: number): string | null {
  const due = Date.parse(value);
  if (Number.isNaN(due)) return null;
  const date = new Date(due);
  const clock = `${pad(date.getHours())}:${pad(date.getMinutes())}`;
  const dayKey = localDayKey(due);
  if (dayKey === localDayKey(now)) return `今天 ${clock}`;
  if (dayKey === shiftLocalDay(now, 1)) return `明天 ${clock}`;
  const monthDay = `${pad(date.getMonth() + 1)}/${pad(date.getDate())}`;
  return date.getFullYear() === new Date(now).getFullYear()
    ? `${monthDay} ${clock}`
    : `${date.getFullYear()}/${monthDay} ${clock}`;
}

export function formatDueDayHeader(dayKey: string, now: number): string {
  const [year, month, day] = dayKey.split("-").map(Number);
  if (!year || !month || !day) return dayKey;
  const weekday = WEEKDAYS[new Date(year, month - 1, day).getDay()];
  if (dayKey === localDayKey(now)) return `今天 ${weekday}`;
  if (dayKey === shiftLocalDay(now, 1)) return `明天 ${weekday}`;
  return `${month}月${day}日 ${weekday}`;
}
