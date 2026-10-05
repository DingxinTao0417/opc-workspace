import type { PageMeta } from "./common";
import type { InboxItemPriority } from "./inbox";

export type ReminderStatus = "scheduled" | "fired" | "cancelled";

export type ReminderAction = "edit" | "cancel";

export type ReminderRecurrenceType =
  "none" | "daily" | "weekly" | "weekdays" | "monthly";

export type ReminderSort =
  | "title"
  | "status"
  | "priority"
  | "trigger_at"
  | "created_at"
  | "updated_at"
  | "-title"
  | "-status"
  | "-priority"
  | "-trigger_at"
  | "-created_at"
  | "-updated_at";

export interface Reminder {
  id: string;
  sourceEntityType: "manual" | "automation";
  sourceEntityId: string | null;
  title: string;
  summary: string;
  priority: InboxItemPriority;
  triggerAt: string;
  status: ReminderStatus;
  sourceEventKey: string;
  createdByActorId: string;
  seriesId: string;
  recurrenceType: ReminderRecurrenceType;
  recurrenceInterval: number;
  recurrenceTimezone: string;
  occurrenceNumber: number;
  recurrenceAnchorDay: number;
  firedAt: string | null;
  inboxItemId: string | null;
  cancelledByActorId: string | null;
  cancelledAt: string | null;
  cancelReason: string | null;
  version: number;
  createdAt: string;
  updatedAt: string;
  availableActions: ReminderAction[];
}

export interface ReminderListParams {
  status?: ReminderStatus;
  q?: string;
  sort?: ReminderSort;
  page?: number;
  pageSize?: number;
}

export interface ReminderListMeta extends PageMeta {
  serverNow: string;
}

export interface ReminderListResult {
  items: Reminder[];
  meta: ReminderListMeta;
}

export interface CreateReminderInput {
  title: string;
  summary: string;
  priority: InboxItemPriority;
  triggerAt: string;
  recurrenceType: ReminderRecurrenceType;
  recurrenceInterval: number;
  recurrenceTimezone: string;
}

export interface UpdateReminderInput {
  title?: string;
  summary?: string;
  priority?: InboxItemPriority;
  triggerAt?: string;
  recurrenceType?: ReminderRecurrenceType;
  recurrenceInterval?: number;
  recurrenceTimezone?: string;
  expectedVersion: number;
}

export interface CancelReminderInput {
  id: string;
  reason: string;
  expectedVersion: number;
}
