import type { PageMeta } from "./common";
import type { InboxItemPriority } from "./inbox";

export type AutomationRuleStatus = "enabled" | "disabled" | "unavailable";

export type AutomationTriggerType = "event" | "schedule";

export type AutomationActionType = "inbox_item" | "task" | "reminder";

export type AutomationRunStatus =
  "succeeded" | "failed" | "skipped" | "cancelled";

export interface AutomationConfig {
  priority?: InboxItemPriority;
  localTime?: string;
  timezone?: string;
}

export interface AutomationRule {
  id: string;
  presetKey: string;
  name: string;
  description: string;
  status: AutomationRuleStatus;
  available: boolean;
  unavailableReason: string;
  triggerType: AutomationTriggerType;
  triggerLabel: string;
  actionType: AutomationActionType;
  actionLabel: string;
  config: AutomationConfig;
  nextRunAt: string | null;
  permissions: string[];
  version: number;
  createdAt: string;
  updatedAt: string;
}

export interface AutomationPreview {
  canEnable: boolean;
  unavailableReason: string;
  triggerSummary: string;
  actionSummary: string;
  config: AutomationConfig;
  nextRunAt: string | null;
  permissions: string[];
}

export interface AutomationRun {
  id: string;
  ruleId: string;
  presetKey: string;
  ruleName: string;
  ruleVersion: number;
  triggerType: AutomationTriggerType;
  sourceEventId: string | null;
  scheduledFor: string | null;
  status: AutomationRunStatus;
  attempt: number;
  retryOfRunId: string | null;
  retryable: boolean;
  retryAt: string | null;
  causedByRunId: string | null;
  causalDepth: number;
  configSnapshot: Record<string, unknown>;
  actionSnapshot: Record<string, unknown>;
  errorCode: string | null;
  resultType: string | null;
  resultId: string | null;
  resultSummary: string;
  startedAt: string;
  endedAt: string;
}

export interface AutomationRunSource {
  kind: AutomationTriggerType;
  available: boolean;
  eventId: string | null;
  aggregateType: string | null;
  aggregateId: string | null;
  action: string | null;
  occurredAt: string | null;
  scheduledFor: string | null;
}

export interface AutomationRunAttemptSummary {
  id: string;
  status: AutomationRunStatus;
  attempt: number;
  retryOfRunId: string | null;
  retryable: boolean;
  retryAt: string | null;
  errorCode: string | null;
  resultType: string | null;
  resultId: string | null;
  resultSummary: string;
  startedAt: string;
  endedAt: string;
}

export interface AutomationRunDetail extends AutomationRun {
  source: AutomationRunSource;
  retryChain: AutomationRunAttemptSummary[];
}

export interface AutomationRunListParams {
  ruleId?: string;
  status?: AutomationRunStatus;
  page?: number;
  pageSize?: number;
}

export interface AutomationRunListResult {
  items: AutomationRun[];
  meta: PageMeta;
}
