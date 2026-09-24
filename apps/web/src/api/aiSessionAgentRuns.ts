import type {
  AgentRunOutputDeliveryStatus,
  AgentRunSummary,
} from "../types/models";
import {
  ApiError,
  apiRequest,
  parseAgentRunSummaryRecord,
  validCanonicalUUID,
  validRFC3339Timestamp,
} from "./client";

export interface AiSessionDelegatedRunPlan {
  version: number;
  stepId: string;
  stepTitle: string;
  supersededBy?: string;
}

export interface AiSessionDelegatedRunFacts {
  sessionId: string;
  generationId: string;
  proposalId: string;
  action: "agent_run.start" | "agent_run.retry";
  createdAt: string;
  decidedAt: string;
  plan: AiSessionDelegatedRunPlan | null;
}

export interface AiSessionDelegatedRun {
  runId: string;
  run: AgentRunSummary | null;
  delegation: AiSessionDelegatedRunFacts;
}

export interface AiSessionDelegatedRunListParams {
  page?: number;
  pageSize?: number;
  outputDeliveryStatus?: AgentRunOutputDeliveryStatus;
}

export interface AiSessionDelegatedRunListResult {
  items: AiSessionDelegatedRun[];
  meta: {
    page: number;
    pageSize: number;
    total: number;
    activeTotal: number;
    pendingDeliveryTotal: number;
    unavailableTotal: number;
    asOf: string;
  };
}

type JsonRecord = Record<string, unknown>;

function invalid(message: string): never {
  throw new ApiError(message, { code: "INVALID_RESPONSE" });
}

function record(value: unknown, message: string): JsonRecord {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return invalid(message);
  }
  return value as JsonRecord;
}

function nonNegativeInteger(value: unknown, label: string): number {
  if (!Number.isSafeInteger(value) || Number(value) < 0) {
    return invalid(`${label}无效`);
  }
  return Number(value);
}

function positiveInteger(value: unknown, label: string): number {
  const parsed = nonNegativeInteger(value, label);
  if (parsed < 1) return invalid(`${label}无效`);
  return parsed;
}

function parsePlan(value: unknown): AiSessionDelegatedRunPlan | null {
  if (value === null) return null;
  const row = record(value, "会话委派计划关联格式无效");
  const stepId = row.step_id;
  const stepTitle = row.step_title;
  const supersededBy = row.superseded_by;
  if (
    typeof stepId !== "string" ||
    !/^[a-z][a-z0-9_-]{0,39}$/.test(stepId) ||
    typeof stepTitle !== "string" ||
    stepTitle.trim().length === 0 ||
    [...stepTitle].length > 300 ||
    (supersededBy !== undefined &&
      (typeof supersededBy !== "string" ||
        !/^[a-z][a-z0-9_-]{0,39}$/.test(supersededBy)))
  ) {
    return invalid("会话委派计划关联格式无效");
  }
  return {
    version: positiveInteger(row.version, "会话委派计划版本"),
    stepId,
    stepTitle,
    ...(typeof supersededBy === "string" ? { supersededBy } : {}),
  };
}

function parseItem(
  value: unknown,
  expectedSessionId: string,
): AiSessionDelegatedRun {
  const row = record(value, "会话委派响应格式无效");
  const runId = row.run_id;
  const delegation = record(row.delegation, "会话委派身份格式无效");
  const forbidden = [
    "action_json",
    "preview_json",
    "input_snapshot_json",
    "result_text",
    "resultText",
    "provider_endpoint",
    "provider_key",
    "workspace_grant",
  ];
  if (
    forbidden.some(
      (key) => Object.hasOwn(row, key) || Object.hasOwn(delegation, key),
    ) ||
    !validCanonicalUUID(runId) ||
    !validCanonicalUUID(delegation.session_id) ||
    delegation.session_id !== expectedSessionId ||
    !validCanonicalUUID(delegation.generation_id) ||
    !validCanonicalUUID(delegation.proposal_id) ||
    (delegation.action !== "agent_run.start" &&
      delegation.action !== "agent_run.retry") ||
    !validRFC3339Timestamp(delegation.created_at) ||
    !validRFC3339Timestamp(delegation.decided_at)
  ) {
    return invalid("会话委派身份格式无效");
  }
  const run = row.run === null ? null : parseAgentRunSummaryRecord(row.run);
  if (run && run.id !== runId) return invalid("会话委派执行身份不匹配");
  return {
    runId,
    run,
    delegation: {
      sessionId: delegation.session_id,
      generationId: delegation.generation_id,
      proposalId: delegation.proposal_id,
      action: delegation.action,
      createdAt: delegation.created_at,
      decidedAt: delegation.decided_at,
      plan: parsePlan(delegation.plan),
    },
  };
}

export async function getAiSessionDelegatedRuns(
  sessionId: string,
  input: AiSessionDelegatedRunListParams = {},
  signal?: AbortSignal,
): Promise<AiSessionDelegatedRunListResult> {
  if (!validCanonicalUUID(sessionId)) return invalid("AI 会话身份无效");
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 20),
  });
  if (input.outputDeliveryStatus) {
    params.set("output_delivery_status", input.outputDeliveryStatus);
  }
  const payload = record(
    await apiRequest<unknown>(
      `/api/v1/ai/sessions/${encodeURIComponent(sessionId)}/delegated-runs?${params}`,
      { signal },
    ),
    "会话委派响应格式无效",
  );
  if (!Array.isArray(payload.data)) return invalid("会话委派响应格式无效");
  const meta = record(payload.meta, "会话委派分页格式无效");
  if (!validRFC3339Timestamp(meta.as_of))
    return invalid("会话委派快照时间无效");
  return {
    items: payload.data.map((item) => parseItem(item, sessionId)),
    meta: {
      page: positiveInteger(meta.page, "会话委派页码"),
      pageSize: positiveInteger(meta.page_size, "会话委派分页大小"),
      total: nonNegativeInteger(meta.total, "会话委派总数"),
      activeTotal: nonNegativeInteger(meta.active_total, "会话委派执行中总数"),
      pendingDeliveryTotal: nonNegativeInteger(
        meta.pending_delivery_total,
        "会话委派待登记总数",
      ),
      unavailableTotal: nonNegativeInteger(
        meta.unavailable_total,
        "会话委派不可用总数",
      ),
      asOf: meta.as_of,
    },
  };
}
