import { ApiError, apiRequest } from "./client";
import { isWorkspaceIdentity } from "../lib/focusReportLocation";
import {
  continuationReasons,
  continuationStatuses,
} from "./aiPlanContinuation";

export type AiAgentInboxKind =
  | "all"
  | "approval"
  | "access_request"
  | "review"
  | "automation_failure"
  | "knowledge_failure"
  | "generation_failure"
  | "provider_issue"
  | "adapter_issue"
  | "maintenance_failure"
  | "agent_run"
  | "continuation";

interface AiAgentInboxBase {
  id: string;
  created_at: string;
}

export interface AiAgentInboxApproval extends AiAgentInboxBase {
  kind: "approval";
  action: string;
  session_id: string;
  session_title: string;
  generation_id: string;
}

export interface AiAgentInboxAccessRequest extends AiAgentInboxBase {
  kind: "access_request";
  session_id: string;
  session_title: string;
  generation_id: string;
}

export interface AiAgentInboxReview extends AiAgentInboxBase {
  kind: "review";
  task_id: string;
  task_title: string;
  submission_id: string;
  sequence: number;
  submission_origin: "manual" | "child_rollup";
}

export interface AiAgentInboxAutomationFailure extends AiAgentInboxBase {
  kind: "automation_failure";
  rule_id: string;
  rule_name: string;
  attempt: number;
  retryable: boolean;
  retry_at: string | null;
  error_code: string | null;
}

export interface AiAgentInboxKnowledgeFailure extends AiAgentInboxBase {
  kind: "knowledge_failure";
  source_id: string;
  source_name: string;
  operation: "import" | "reindex";
  attempt: number;
  error_code: string;
}

export interface AiAgentInboxGenerationFailure extends AiAgentInboxBase {
  kind: "generation_failure";
  session_id: string;
  session_title: string;
  generation_id: string;
  error_code: string;
}

export interface AiAgentInboxProviderIssue extends AiAgentInboxBase {
  kind: "provider_issue";
  provider_id: string;
  provider_name: string;
  provider_model: string;
  provider_status: "unconfigured" | "unavailable";
  health_status: "unknown" | "unhealthy";
  error_code: string | null;
}

export interface AiAgentInboxAdapterIssue extends AiAgentInboxBase {
  kind: "adapter_issue";
  adapter_id: string;
  adapter_name: string;
  adapter_key: string;
  adapter_status: "enabled" | "disabled";
  health_status: "blocked" | "unhealthy";
  isolation_status: string;
  execution_ready: boolean;
  error_code: string | null;
}

export interface AiAgentInboxMaintenanceFailure extends AiAgentInboxBase {
  kind: "maintenance_failure";
  component: "backup" | "database" | "sidecar" | "storage";
  operation:
    | "create"
    | "verify"
    | "drill"
    | "restore"
    | "startup"
    | "migration"
    | "runtime"
    | "low_space";
  error_code:
    | "backup_create_failed"
    | "backup_verify_failed"
    | "backup_drill_failed"
    | "backup_restore_failed"
    | "database_startup_failed"
    | "database_migration_failed"
    | "database_runtime_failed"
    | "sidecar_startup_failed"
    | "storage_low_space";
}

export interface AiAgentInboxAgentRun extends AiAgentInboxBase {
  kind: "agent_run";
  task_id: string;
  task_title: string;
  attempt: number;
  run_status:
    "queued" | "running" | "succeeded" | "failed" | "cancelled" | "interrupted";
  output_delivery_status: "not_ready" | "pending" | "submitted" | "retained";
}

export interface AiAgentInboxContinuation extends AiAgentInboxBase {
  kind: "continuation";
  session_id: string;
  session_title: string;
  continuation_status: keyof typeof continuationStatuses;
  continuation_reason: keyof typeof continuationReasons;
  continuation_max_turns: number;
  continuation_turns_started: number;
  continuation_expires_at: string;
}

export type AiAgentInboxItem =
  | AiAgentInboxApproval
  | AiAgentInboxAccessRequest
  | AiAgentInboxReview
  | AiAgentInboxAutomationFailure
  | AiAgentInboxKnowledgeFailure
  | AiAgentInboxGenerationFailure
  | AiAgentInboxProviderIssue
  | AiAgentInboxAdapterIssue
  | AiAgentInboxMaintenanceFailure
  | AiAgentInboxAgentRun
  | AiAgentInboxContinuation;

export interface AiAgentInboxMeta {
  kind_filter: AiAgentInboxKind;
  total: number;
  approval_total: number;
  access_request_total: number;
  review_total: number;
  automation_failure_total: number;
  knowledge_failure_total: number;
  generation_failure_total: number;
  provider_issue_total: number;
  adapter_issue_total?: number;
  maintenance_failure_total: number;
  agent_run_total: number;
  continuation_total?: number;
  has_more: boolean;
  next_offset: number | null;
  window_limited: boolean;
  as_of: string;
}

export interface AiAgentInboxPage {
  data: AiAgentInboxItem[];
  meta: AiAgentInboxMeta;
}

const object = (value: unknown): value is Record<string, unknown> =>
  value !== null && typeof value === "object" && !Array.isArray(value);
const integerBetween = (value: unknown, min: number, max: number) =>
  Number.isInteger(value) &&
  (value as number) >= min &&
  (value as number) <= max;
const bounded = (value: unknown, maximum: number): value is string =>
  typeof value === "string" &&
  value.trim().length > 0 &&
  [...value].length <= maximum;
const exactKeys = (value: Record<string, unknown>, allowed: string[]) =>
  Object.keys(value).length === allowed.length &&
  Object.keys(value).every((key) => allowed.includes(key));

function invalid(): never {
  throw new ApiError("Agent Inbox 响应无效，不能据此操作工作台。", {
    code: "INVALID_RESPONSE",
  });
}

export function parseAiAgentInbox(
  value: unknown,
  expectedKind: AiAgentInboxKind,
): AiAgentInboxPage {
  if (!object(value) || !Array.isArray(value.data) || !object(value.meta)) {
    return invalid();
  }
  const meta = value.meta;
  const legacyTotal =
    Number(meta.approval_total) +
    Number(meta.review_total) +
    Number(meta.automation_failure_total) +
    Number(meta.knowledge_failure_total) +
    Number(meta.generation_failure_total) +
    Number(meta.provider_issue_total) +
    Number(meta.maintenance_failure_total);
  const accessRequestTotal =
    meta.access_request_total === undefined &&
    expectedKind === "all" &&
    meta.total === legacyTotal &&
    !value.data.some((item) => object(item) && item.kind === "access_request")
      ? 0
      : meta.access_request_total;
  const agentRunFieldPresent = Object.hasOwn(meta, "agent_run_total");
  const agentRunTotal =
    !agentRunFieldPresent &&
    !value.data.some((item) => object(item) && item.kind === "agent_run")
      ? 0
      : meta.agent_run_total;
  const continuationFieldPresent = Object.hasOwn(meta, "continuation_total");
  const continuationTotal =
    !continuationFieldPresent &&
    !value.data.some((item) => object(item) && item.kind === "continuation")
      ? 0
      : meta.continuation_total;
  const adapterIssueFieldPresent = Object.hasOwn(meta, "adapter_issue_total");
  const adapterIssueTotal =
    !adapterIssueFieldPresent &&
    !value.data.some((item) => object(item) && item.kind === "adapter_issue")
      ? 0
      : meta.adapter_issue_total;
  if (
    meta.kind_filter !== expectedKind ||
    !integerBetween(meta.total, 0, 1_000_000_000) ||
    !integerBetween(meta.approval_total, 0, 1_000_000_000) ||
    !integerBetween(accessRequestTotal, 0, 1_000_000_000) ||
    !integerBetween(meta.review_total, 0, 1_000_000_000) ||
    !integerBetween(meta.automation_failure_total, 0, 1_000_000_000) ||
    !integerBetween(meta.knowledge_failure_total, 0, 1_000_000_000) ||
    !integerBetween(meta.generation_failure_total, 0, 1_000_000_000) ||
    !integerBetween(meta.provider_issue_total, 0, 1_000_000_000) ||
    !integerBetween(adapterIssueTotal, 0, 1_000_000_000) ||
    !integerBetween(meta.maintenance_failure_total, 0, 1_000_000_000) ||
    !integerBetween(agentRunTotal, 0, 1_000_000_000) ||
    !integerBetween(continuationTotal, 0, 1_000_000_000) ||
    meta.total !==
      (expectedKind === "approval"
        ? meta.approval_total
        : expectedKind === "access_request"
          ? accessRequestTotal
          : expectedKind === "review"
            ? meta.review_total
            : expectedKind === "automation_failure"
              ? meta.automation_failure_total
              : expectedKind === "knowledge_failure"
                ? meta.knowledge_failure_total
                : expectedKind === "generation_failure"
                  ? meta.generation_failure_total
                  : expectedKind === "provider_issue"
                    ? meta.provider_issue_total
                    : expectedKind === "adapter_issue"
                      ? adapterIssueTotal
                      : expectedKind === "agent_run"
                        ? agentRunTotal
                        : expectedKind === "maintenance_failure"
                          ? meta.maintenance_failure_total
                          : expectedKind === "continuation"
                            ? continuationTotal
                            : (meta.approval_total as number) +
                              (accessRequestTotal as number) +
                              (meta.review_total as number) +
                              (meta.automation_failure_total as number) +
                              (meta.knowledge_failure_total as number) +
                              (meta.generation_failure_total as number) +
                              (meta.provider_issue_total as number) +
                              (adapterIssueTotal as number) +
                              (meta.maintenance_failure_total as number) +
                              (agentRunTotal as number) +
                              (continuationTotal as number)) ||
    typeof meta.has_more !== "boolean" ||
    typeof meta.window_limited !== "boolean" ||
    (meta.next_offset !== null && !integerBetween(meta.next_offset, 1, 999)) ||
    meta.has_more !== (meta.next_offset !== null) ||
    typeof meta.as_of !== "string" ||
    !Number.isFinite(Date.parse(meta.as_of)) ||
    value.data.length > 50 ||
    value.data.length > (meta.total as number)
  ) {
    return invalid();
  }

  const identities = new Set<string>();
  const items: AiAgentInboxItem[] = [];
  let previous: AiAgentInboxItem | null = null;
  for (const raw of value.data) {
    if (
      !object(raw) ||
      !isWorkspaceIdentity(String(raw.id)) ||
      typeof raw.created_at !== "string" ||
      !Number.isFinite(Date.parse(raw.created_at))
    ) {
      return invalid();
    }
    if (expectedKind !== "all" && raw.kind !== expectedKind) return invalid();
    let item: AiAgentInboxItem;
    if (raw.kind === "approval") {
      if (
        !exactKeys(raw, [
          "kind",
          "id",
          "created_at",
          "action",
          "session_id",
          "session_title",
          "generation_id",
        ]) ||
        [
          "review",
          "automation_failure",
          "knowledge_failure",
          "generation_failure",
          "provider_issue",
          "maintenance_failure",
        ].includes(expectedKind) ||
        !bounded(raw.action, 80) ||
        !isWorkspaceIdentity(String(raw.session_id)) ||
        !bounded(raw.session_title, 200) ||
        !isWorkspaceIdentity(String(raw.generation_id)) ||
        "task_id" in raw ||
        "submission_id" in raw ||
        "sequence" in raw ||
        "submission_origin" in raw
      ) {
        return invalid();
      }
      item = raw as unknown as AiAgentInboxApproval;
    } else if (raw.kind === "access_request") {
      if (
        !exactKeys(raw, [
          "kind",
          "id",
          "created_at",
          "session_id",
          "session_title",
          "generation_id",
        ]) ||
        !isWorkspaceIdentity(String(raw.session_id)) ||
        !bounded(raw.session_title, 200) ||
        raw.generation_id !== raw.id
      ) {
        return invalid();
      }
      item = raw as unknown as AiAgentInboxAccessRequest;
    } else if (raw.kind === "review") {
      if (
        !exactKeys(raw, [
          "kind",
          "id",
          "created_at",
          "task_id",
          "task_title",
          "submission_id",
          "sequence",
          "submission_origin",
        ]) ||
        [
          "approval",
          "automation_failure",
          "knowledge_failure",
          "generation_failure",
          "provider_issue",
          "maintenance_failure",
        ].includes(expectedKind) ||
        !isWorkspaceIdentity(String(raw.task_id)) ||
        !bounded(raw.task_title, 500) ||
        raw.submission_id !== raw.id ||
        !integerBetween(raw.sequence, 1, 1_000_000_000) ||
        !["manual", "child_rollup"].includes(String(raw.submission_origin)) ||
        "action" in raw ||
        "session_id" in raw ||
        "generation_id" in raw
      ) {
        return invalid();
      }
      item = raw as unknown as AiAgentInboxReview;
    } else if (raw.kind === "automation_failure") {
      if (
        !exactKeys(raw, [
          "kind",
          "id",
          "created_at",
          "rule_id",
          "rule_name",
          "attempt",
          "retryable",
          "retry_at",
          "error_code",
        ]) ||
        [
          "approval",
          "review",
          "knowledge_failure",
          "generation_failure",
          "provider_issue",
          "maintenance_failure",
        ].includes(expectedKind) ||
        !isWorkspaceIdentity(String(raw.rule_id)) ||
        !bounded(raw.rule_name, 200) ||
        !integerBetween(raw.attempt, 1, 3) ||
        typeof raw.retryable !== "boolean" ||
        (raw.retry_at !== null &&
          (typeof raw.retry_at !== "string" ||
            !Number.isFinite(Date.parse(raw.retry_at)))) ||
        (raw.error_code !== null && !bounded(raw.error_code, 120))
      ) {
        return invalid();
      }
      item = raw as unknown as AiAgentInboxAutomationFailure;
    } else if (raw.kind === "knowledge_failure") {
      if (
        !exactKeys(raw, [
          "kind",
          "id",
          "created_at",
          "source_id",
          "source_name",
          "operation",
          "attempt",
          "error_code",
        ]) ||
        [
          "approval",
          "review",
          "automation_failure",
          "generation_failure",
          "provider_issue",
          "maintenance_failure",
        ].includes(expectedKind) ||
        !isWorkspaceIdentity(String(raw.source_id)) ||
        !bounded(raw.source_name, 255) ||
        !["import", "reindex"].includes(String(raw.operation)) ||
        !integerBetween(raw.attempt, 1, 1_000_000_000) ||
        !bounded(raw.error_code, 120)
      ) {
        return invalid();
      }
      item = raw as unknown as AiAgentInboxKnowledgeFailure;
    } else if (raw.kind === "generation_failure") {
      if (
        !exactKeys(raw, [
          "kind",
          "id",
          "created_at",
          "session_id",
          "session_title",
          "generation_id",
          "error_code",
        ]) ||
        [
          "approval",
          "review",
          "automation_failure",
          "knowledge_failure",
          "provider_issue",
          "maintenance_failure",
        ].includes(expectedKind) ||
        !isWorkspaceIdentity(String(raw.session_id)) ||
        !bounded(raw.session_title, 200) ||
        raw.generation_id !== raw.id ||
        !isWorkspaceIdentity(String(raw.generation_id)) ||
        !bounded(raw.error_code, 120)
      ) {
        return invalid();
      }
      item = raw as unknown as AiAgentInboxGenerationFailure;
    } else if (raw.kind === "provider_issue") {
      if (
        !exactKeys(raw, [
          "kind",
          "id",
          "created_at",
          "provider_id",
          "provider_name",
          "provider_model",
          "provider_status",
          "health_status",
          "error_code",
        ]) ||
        [
          "approval",
          "review",
          "automation_failure",
          "knowledge_failure",
          "generation_failure",
          "maintenance_failure",
        ].includes(expectedKind) ||
        raw.provider_id !== raw.id ||
        !isWorkspaceIdentity(String(raw.provider_id)) ||
        !bounded(raw.provider_name, 120) ||
        !bounded(raw.provider_model, 160) ||
        !(
          (raw.provider_status === "unconfigured" &&
            raw.health_status === "unknown") ||
          (raw.provider_status === "unavailable" &&
            raw.health_status === "unhealthy")
        ) ||
        (raw.error_code !== null && !bounded(raw.error_code, 120))
      ) {
        return invalid();
      }
      item = raw as unknown as AiAgentInboxProviderIssue;
    } else if (raw.kind === "adapter_issue") {
      if (
        !exactKeys(raw, [
          "kind",
          "id",
          "created_at",
          "adapter_id",
          "adapter_name",
          "adapter_key",
          "adapter_status",
          "health_status",
          "isolation_status",
          "execution_ready",
          "error_code",
        ]) ||
        [
          "approval",
          "review",
          "automation_failure",
          "knowledge_failure",
          "generation_failure",
          "provider_issue",
          "maintenance_failure",
        ].includes(expectedKind) ||
        raw.adapter_id !== raw.id ||
        !isWorkspaceIdentity(String(raw.adapter_id)) ||
        !bounded(raw.adapter_name, 120) ||
        !bounded(raw.adapter_key, 120) ||
        !["enabled", "disabled"].includes(String(raw.adapter_status)) ||
        !["blocked", "unhealthy"].includes(String(raw.health_status)) ||
        !bounded(raw.isolation_status, 40) ||
        typeof raw.execution_ready !== "boolean" ||
        (raw.error_code !== null && !bounded(raw.error_code, 120))
      ) {
        return invalid();
      }
      item = raw as unknown as AiAgentInboxAdapterIssue;
    } else if (raw.kind === "maintenance_failure") {
      const maintenanceKinds: Record<string, string> = {
        "backup:create": "backup_create_failed",
        "backup:verify": "backup_verify_failed",
        "backup:drill": "backup_drill_failed",
        "backup:restore": "backup_restore_failed",
        "database:startup": "database_startup_failed",
        "database:migration": "database_migration_failed",
        "database:runtime": "database_runtime_failed",
        "sidecar:startup": "sidecar_startup_failed",
        "storage:low_space": "storage_low_space",
      };
      if (
        !exactKeys(raw, [
          "kind",
          "id",
          "created_at",
          "component",
          "operation",
          "error_code",
        ]) ||
        [
          "approval",
          "review",
          "automation_failure",
          "knowledge_failure",
          "generation_failure",
          "provider_issue",
        ].includes(expectedKind) ||
        maintenanceKinds[
          `${String(raw.component)}:${String(raw.operation)}`
        ] !== raw.error_code
      ) {
        return invalid();
      }
      item = raw as unknown as AiAgentInboxMaintenanceFailure;
    } else if (raw.kind === "agent_run") {
      if (
        !exactKeys(raw, [
          "kind",
          "id",
          "created_at",
          "task_id",
          "task_title",
          "attempt",
          "run_status",
          "output_delivery_status",
        ]) ||
        !isWorkspaceIdentity(String(raw.task_id)) ||
        !bounded(raw.task_title, 500) ||
        !integerBetween(raw.attempt, 1, 1_000_000_000) ||
        ![
          "queued",
          "running",
          "succeeded",
          "failed",
          "cancelled",
          "interrupted",
        ].includes(String(raw.run_status)) ||
        !["not_ready", "pending", "submitted", "retained"].includes(
          String(raw.output_delivery_status),
        )
      ) {
        return invalid();
      }
      item = raw as unknown as AiAgentInboxAgentRun;
    } else if (raw.kind === "continuation") {
      if (
        !exactKeys(raw, [
          "kind",
          "id",
          "created_at",
          "session_id",
          "session_title",
          "continuation_status",
          "continuation_reason",
          "continuation_max_turns",
          "continuation_turns_started",
          "continuation_expires_at",
        ]) ||
        !isWorkspaceIdentity(String(raw.session_id)) ||
        !bounded(raw.session_title, 200) ||
        !["waiting", "running"].includes(String(raw.continuation_status)) ||
        !Object.hasOwn(continuationStatuses, String(raw.continuation_status)) ||
        !Object.hasOwn(continuationReasons, String(raw.continuation_reason)) ||
        !integerBetween(raw.continuation_max_turns, 1, 8) ||
        !integerBetween(
          raw.continuation_turns_started,
          0,
          raw.continuation_max_turns as number,
        ) ||
        typeof raw.continuation_expires_at !== "string" ||
        !Number.isFinite(Date.parse(raw.continuation_expires_at))
      ) {
        return invalid();
      }
      item = raw as unknown as AiAgentInboxContinuation;
    } else {
      return invalid();
    }
    const identity = `${item.kind}:${item.id}`;
    if (identities.has(identity)) return invalid();
    if (previous) {
      const priorTime = Date.parse(previous.created_at);
      const currentTime = Date.parse(item.created_at);
      if (
        currentTime > priorTime ||
        (currentTime === priorTime &&
          `${item.kind}:${item.id}` < `${previous.kind}:${previous.id}`)
      ) {
        return invalid();
      }
    }
    identities.add(identity);
    items.push(item);
    previous = item;
  }
  const parsedMeta = {
    ...meta,
    access_request_total: accessRequestTotal,
    ...(agentRunFieldPresent ? { agent_run_total: agentRunTotal } : {}),
    ...(continuationFieldPresent
      ? { continuation_total: continuationTotal }
      : {}),
    ...(adapterIssueFieldPresent
      ? { adapter_issue_total: adapterIssueTotal }
      : {}),
  };
  return {
    data: items,
    meta: parsedMeta as unknown as AiAgentInboxMeta,
  };
}

export async function getAiAgentInbox(
  kind: AiAgentInboxKind = "all",
  limit = 50,
  offset = 0,
  signal?: AbortSignal,
): Promise<AiAgentInboxPage> {
  if (
    ![
      "all",
      "approval",
      "access_request",
      "review",
      "automation_failure",
      "knowledge_failure",
      "generation_failure",
      "provider_issue",
      "maintenance_failure",
      "agent_run",
      "continuation",
    ].includes(kind) ||
    !integerBetween(limit, 1, 50) ||
    !integerBetween(offset, 0, 999)
  ) {
    return invalid();
  }
  const params = new URLSearchParams({
    kind,
    limit: String(limit),
    offset: String(offset),
  });
  const response = await apiRequest<unknown>(`/api/v1/ai/inbox?${params}`, {
    signal,
  });
  return parseAiAgentInbox(response, kind);
}
