import type { AgentAdapter } from "../../types/models";
import {
  apiRequest,
  expectedVersionHeader,
  fieldValue,
  invalidResponse,
  isRecord,
  nullableString,
  positiveInteger,
  stringArray,
  stringField,
} from "./core";

function normalizeAgentAdapterStatus(value: unknown): AgentAdapter["status"] {
  if (value === "enabled" || value === "disabled") return value;
  return invalidResponse("本地 Agent 适配器状态响应无效");
}

function normalizeAgentAdapterHealthStatus(
  value: unknown,
): AgentAdapter["healthStatus"] {
  if (
    value === "unknown" ||
    value === "blocked" ||
    value === "healthy" ||
    value === "unhealthy"
  ) {
    return value;
  }
  return invalidResponse("本地 Agent 健康状态响应无效");
}

function normalizeAgentAdapterIsolationStatus(
  value: unknown,
): AgentAdapter["isolationStatus"] {
  if (
    value === "unverified" ||
    value === "verified" ||
    value === "unsupported"
  ) {
    return value;
  }
  return invalidResponse("本地 Agent 隔离状态响应无效");
}

export function normalizeAgentAdapter(value: unknown): AgentAdapter {
  if (!isRecord(value)) {
    return invalidResponse("本地 Agent 适配器响应格式无效");
  }
  const manifest = value.manifest;
  const readiness = value.readiness;
  if (
    !isRecord(manifest) ||
    !isRecord(readiness) ||
    "executable_ref" in value ||
    "executableRef" in value ||
    "path" in value ||
    manifest.execution_mode !== "short_lived_process" ||
    typeof readiness.can_enable !== "boolean" ||
    typeof value.execution_ready !== "boolean"
  ) {
    return invalidResponse("本地 Agent 适配器响应格式无效");
  }
  const id = stringField(value, "id");
  const adapterKey = stringField(value, "adapter_key", "adapterKey");
  const displayName = stringField(value, "display_name", "displayName");
  const createdAt = stringField(value, "created_at", "createdAt");
  const updatedAt = stringField(value, "updated_at", "updatedAt");
  const healthErrorCode = nullableString(
    fieldValue(value, "health_error_code", "healthErrorCode"),
  );
  const lastHealthAt = nullableString(
    fieldValue(value, "last_health_at", "lastHealthAt"),
  );
  const unavailableCode = fieldValue(
    readiness,
    "unavailable_code",
    "unavailableCode",
  );
  if (
    !id ||
    !adapterKey ||
    !displayName ||
    !createdAt ||
    !updatedAt ||
    value.kind !== "builtin" ||
    fieldValue(value, "protocol_version", "protocolVersion") !==
      "opc-agent-pipe-v1" ||
    (unavailableCode !== undefined && typeof unavailableCode !== "string") ||
    (lastHealthAt !== null && Number.isNaN(Date.parse(lastHealthAt))) ||
    (healthErrorCode !== null && lastHealthAt === null)
  ) {
    return invalidResponse("本地 Agent 适配器响应不一致");
  }
  const capabilities = stringArray(manifest.capabilities, "本地 Agent 能力");
  const requirements = stringArray(
    manifest.requirements,
    "本地 Agent 安全要求",
  );
  const requiredGates = stringArray(
    fieldValue(readiness, "required_gates", "requiredGates"),
    "本地 Agent 启用闸门",
  );
  const executionReady = value.execution_ready;
  const canEnable = readiness.can_enable;
  if (
    executionReady !== canEnable ||
    requirements.length !== requiredGates.length ||
    requirements.some((gate, index) => gate !== requiredGates[index]) ||
    (executionReady && unavailableCode) ||
    (!executionReady && !unavailableCode)
  ) {
    return invalidResponse("本地 Agent 就绪状态响应不一致");
  }
  return {
    id,
    adapterKey,
    kind: "builtin",
    displayName,
    protocolVersion: "opc-agent-pipe-v1",
    manifest: {
      executionMode: "short_lived_process",
      capabilities,
      requirements,
    },
    status: normalizeAgentAdapterStatus(value.status),
    healthStatus: normalizeAgentAdapterHealthStatus(
      fieldValue(value, "health_status", "healthStatus"),
    ),
    healthErrorCode,
    isolationStatus: normalizeAgentAdapterIsolationStatus(
      fieldValue(value, "isolation_status", "isolationStatus"),
    ),
    executionReady,
    lastHealthAt,
    readiness: {
      canEnable,
      unavailableCode:
        typeof unavailableCode === "string" ? unavailableCode : "",
      requiredGates,
    },
    version: positiveInteger(value.version, "本地 Agent 适配器版本"),
    createdAt,
    updatedAt,
  };
}

export async function getAgentAdapters(): Promise<AgentAdapter[]> {
  const payload = await apiRequest<unknown>("/api/v1/agent-adapters");
  if (!isRecord(payload) || !Array.isArray(payload.data)) {
    return invalidResponse("本地 Agent 适配器列表响应格式无效");
  }
  return payload.data.map(normalizeAgentAdapter);
}

export async function registerAgentAdapter(
  presetKey = "builtin-local-text-v1",
  idempotencyKey: string = crypto.randomUUID(),
): Promise<AgentAdapter> {
  const payload = await apiRequest<unknown>("/api/v1/agent-adapters", {
    method: "POST",
    headers: { "Idempotency-Key": idempotencyKey },
    body: JSON.stringify({ preset_key: presetKey }),
  });
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeAgentAdapter(body);
}

async function changeAgentAdapter(
  id: string,
  action: "check" | "enable" | "disable",
  expectedVersion: number,
): Promise<AgentAdapter> {
  const payload = await apiRequest<unknown>(
    `/api/v1/agent-adapters/${encodeURIComponent(id)}/${action}`,
    { method: "POST", headers: expectedVersionHeader(expectedVersion) },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  const adapter = normalizeAgentAdapter(body);
  if (adapter.id !== id) {
    return invalidResponse("本地 Agent 适配器操作响应与请求不一致");
  }
  return adapter;
}

export function checkAgentAdapter(
  id: string,
  expectedVersion: number,
): Promise<AgentAdapter> {
  return changeAgentAdapter(id, "check", expectedVersion);
}

export function enableAgentAdapter(
  id: string,
  expectedVersion: number,
): Promise<AgentAdapter> {
  return changeAgentAdapter(id, "enable", expectedVersion);
}

export function disableAgentAdapter(
  id: string,
  expectedVersion: number,
): Promise<AgentAdapter> {
  return changeAgentAdapter(id, "disable", expectedVersion);
}
