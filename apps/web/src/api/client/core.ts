import type { HealthResponse } from "../../types/models";

const DEV_TOKEN =
  import.meta.env.VITE_OPC_SESSION_TOKEN ?? "opc-workspace-local-dev";

const DEFAULT_REQUEST_TIMEOUT_MS = 8_000;

export const ARTIFACT_TRANSFER_TIMEOUT_MS = 120_000;

export const BACKUP_OPERATION_TIMEOUT_MS = 180_000;

export const MAX_ARTIFACT_FILE_BYTES = 50 * 1_024 * 1_024;

export const MAX_MULTIPART_REQUEST_BYTES = 100 * 1_024 * 1_024;

// Browsers choose the multipart boundary and encode per-part headers after the
// FormData has been built. Reserve enough space for that envelope so a request
// that passes client validation does not immediately exceed the Sidecar limit.
export const MULTIPART_ENVELOPE_RESERVE_BYTES = 64 * 1_024;

export const MULTIPART_FILE_PART_RESERVE_BYTES = 2 * 1_024;

interface RuntimeConnection {
  baseUrl: string;
  token: string;
  source: "browser" | "tauri";
}

export type JsonRecord = Record<string, unknown>;

export class ApiError extends Error {
  readonly code: string;
  readonly status?: number;
  readonly requestId?: string;

  constructor(
    message: string,
    options: { code?: string; status?: number; requestId?: string } = {},
  ) {
    super(message);
    this.name = "ApiError";
    this.code = options.code ?? "REQUEST_ERROR";
    this.status = options.status;
    this.requestId = options.requestId;
  }
}

export function isRecord(value: unknown): value is JsonRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function hasExactKeys(
  record: JsonRecord,
  keys: readonly string[],
): boolean {
  const actual = Object.keys(record).sort();
  const expected = [...keys].sort();
  return (
    actual.length === expected.length &&
    actual.every((key, index) => key === expected[index])
  );
}

export function stringField(
  record: JsonRecord,
  ...keys: string[]
): string | undefined {
  for (const key of keys) {
    if (typeof record[key] === "string" && record[key])
      return record[key] as string;
  }
  return undefined;
}

export function numberField(
  record: JsonRecord,
  ...keys: string[]
): number | undefined {
  for (const key of keys) {
    if (typeof record[key] === "number" && Number.isFinite(record[key])) {
      return record[key];
    }
  }
  return undefined;
}

function browserConnection(): RuntimeConnection {
  return { baseUrl: "", token: DEV_TOKEN, source: "browser" };
}

function isTauriRuntime(): boolean {
  if (typeof window === "undefined") return false;
  const runtimeWindow = window as Window & {
    __TAURI__?: unknown;
    __TAURI_INTERNALS__?: unknown;
  };
  return Boolean(runtimeWindow.__TAURI__ || runtimeWindow.__TAURI_INTERNALS__);
}

let runtimeConnectionPromise: Promise<RuntimeConnection> | undefined;

async function resolveRuntimeConnection(): Promise<RuntimeConnection> {
  if (!isTauriRuntime()) return browserConnection();

  const { invoke } = await import("@tauri-apps/api/core");
  const readyStates = new Set(["ready", "running", "healthy", "ok"]);
  const waitableStates = new Set(["starting", "restarting"]);
  const maxAttempts = 50;

  for (let attempt = 0; attempt < maxAttempts; attempt += 1) {
    const rawStatus = await invoke<unknown>("sidecar_status");
    if (!isRecord(rawStatus)) {
      throw new ApiError("桌面服务尚未返回可用连接信息", {
        code: "SIDECAR_UNAVAILABLE",
      });
    }

    const state = stringField(
      rawStatus,
      "phase",
      "state",
      "status",
    )?.toLowerCase();

    if (state === "error") {
      throw new ApiError(
        stringField(rawStatus, "message") ?? "本地 Sidecar 启动失败",
        { code: "SIDECAR_START_ERROR" },
      );
    }

    if (state && readyStates.has(state)) {
      const baseUrl = stringField(
        rawStatus,
        "base_url",
        "baseUrl",
        "url",
        "api_base_url",
        "apiBaseUrl",
      );
      const token = stringField(
        rawStatus,
        "token",
        "session_token",
        "sessionToken",
      );
      if (!baseUrl || !token) {
        throw new ApiError("本地 Sidecar 连接信息不完整", {
          code: "SIDECAR_INVALID_STATUS",
        });
      }
      return {
        baseUrl: baseUrl.replace(/\/$/, ""),
        token,
        source: "tauri",
      };
    }

    if (state && !waitableStates.has(state)) {
      throw new ApiError(`本地 Sidecar 当前状态：${state}`, {
        code: "SIDECAR_UNAVAILABLE",
      });
    }

    await new Promise((resolve) => window.setTimeout(resolve, 100));
  }

  throw new ApiError("等待本地 Sidecar 就绪超时", {
    code: "SIDECAR_TIMEOUT",
  });
}

export function getRuntimeConnection(): Promise<RuntimeConnection> {
  runtimeConnectionPromise ??= resolveRuntimeConnection().catch((error) => {
    runtimeConnectionPromise = undefined;
    throw error;
  });
  return runtimeConnectionPromise;
}

export function resetRuntimeConnection(): void {
  runtimeConnectionPromise = undefined;
}

async function readResponseJSON(response: Response): Promise<unknown> {
  try {
    return await response.json();
  } catch (error) {
    if (error instanceof SyntaxError) return undefined;
    throw error;
  }
}

export async function apiFetch<T>(
  path: string,
  consume: (response: Response) => Promise<T>,
  init: RequestInit = {},
  accept = "application/json",
  timeoutMs = DEFAULT_REQUEST_TIMEOUT_MS,
): Promise<T> {
  const connection = await getRuntimeConnection();
  const headers = new Headers(init.headers);
  const requestId = crypto.randomUUID();
  headers.set("Accept", accept);
  headers.set("X-Request-ID", requestId);
  const multipart =
    typeof FormData !== "undefined" && init.body instanceof FormData;
  if (init.body && !multipart && !headers.has("Content-Type"))
    headers.set("Content-Type", "application/json");
  if (connection.token)
    headers.set("Authorization", `Bearer ${connection.token}`);
  let correlatedRequestId: string = requestId;

  const controller = new AbortController();
  const upstreamSignal = init.signal;
  const abortFromUpstream = () => controller.abort();
  if (upstreamSignal?.aborted) controller.abort();
  else
    upstreamSignal?.addEventListener("abort", abortFromUpstream, {
      once: true,
    });
  const timeout = window.setTimeout(() => controller.abort(), timeoutMs);

  try {
    const response = await fetch(`${connection.baseUrl}${path}`, {
      ...init,
      headers,
      signal: controller.signal,
    });
    correlatedRequestId =
      response.headers.get("X-Request-ID")?.trim() || requestId;
    if (!response.ok) {
      const body = await readResponseJSON(response);
      const errorBody = isRecord(body) ? body : {};
      throw new ApiError(
        stringField(errorBody, "message") ?? `请求失败（${response.status}）`,
        {
          code: stringField(errorBody, "code") ?? "HTTP_ERROR",
          status: response.status,
          requestId:
            correlatedRequestId ||
            stringField(errorBody, "request_id", "requestId") ||
            requestId,
        },
      );
    }

    return await consume(response);
  } catch (error) {
    if (error instanceof ApiError) {
      if (error.requestId) throw error;
      throw new ApiError(error.message, {
        code: error.code,
        status: error.status,
        requestId: correlatedRequestId,
      });
    }
    if (
      controller.signal.aborted ||
      (error instanceof DOMException && error.name === "AbortError")
    ) {
      throw new ApiError("连接本地 Sidecar 超时", {
        code: "TIMEOUT",
        requestId,
      });
    }
    throw new ApiError("无法连接本地 Sidecar", {
      code: "NETWORK_ERROR",
      requestId,
    });
  } finally {
    window.clearTimeout(timeout);
    upstreamSignal?.removeEventListener("abort", abortFromUpstream);
  }
}

export async function apiRequest<T>(
  path: string,
  init: RequestInit = {},
  timeoutMs = DEFAULT_REQUEST_TIMEOUT_MS,
): Promise<T> {
  return apiFetch(
    path,
    async (response) => (await readResponseJSON(response)) as T,
    init,
    "application/json",
    timeoutMs,
  );
}

export function invalidResponse(message: string): never {
  throw new ApiError(message, { code: "INVALID_RESPONSE" });
}

export function nullableString(value: unknown): string | null {
  return typeof value === "string" && value ? value : null;
}

export function fieldValue(record: JsonRecord, ...keys: string[]): unknown {
  for (const key of keys) {
    if (key in record) return record[key];
  }
  return undefined;
}

export function numeric(value: unknown, fallback = 0): number {
  return typeof value === "number" && Number.isFinite(value) ? value : fallback;
}

export function nonNegativeInteger(
  value: unknown,
  field: string,
  fallback?: number,
): number {
  if (value === undefined && fallback !== undefined) return fallback;
  if (typeof value === "number" && Number.isInteger(value) && value >= 0) {
    return value;
  }
  return invalidResponse(`${field} 响应无效`);
}

export function positiveInteger(
  value: unknown,
  field: string,
  fallback?: number,
): number {
  if (value === undefined && fallback !== undefined) return fallback;
  if (typeof value === "number" && Number.isInteger(value) && value >= 1) {
    return value;
  }
  return invalidResponse(`${field} 响应无效`);
}

export function nullableNonNegativeInteger(
  value: unknown,
  field: string,
): number | null {
  if (value === undefined || value === null) return null;
  return nonNegativeInteger(value, field);
}

export function expectedVersionHeader(value: unknown): Record<string, string> {
  if (typeof value !== "number" || !Number.isInteger(value) || value < 1) {
    throw new ApiError("资源版本缺失，请刷新后重试", {
      code: "EXPECTED_VERSION_REQUIRED",
    });
  }
  return { "If-Match": `"${value}"` };
}

export function validInvoiceDate(value: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
  const parsed = new Date(`${value}T00:00:00.000Z`);
  return (
    !Number.isNaN(parsed.getTime()) &&
    parsed.toISOString().slice(0, 10) === value
  );
}

export function requiredNullableString(
  value: unknown,
  field: string,
): string | null {
  if (value === null) return null;
  if (typeof value === "string" && value.length > 0) return value;
  return invalidResponse(`${field}响应无效`);
}

export function nullableEventString(
  value: unknown,
  field: string,
): string | null {
  if (value === null) return null;
  if (typeof value === "string" && value.length > 0) return value;
  return invalidResponse(`${field} 响应无效`);
}

export function nullableEventSnapshot(
  value: unknown,
  field: string,
): Record<string, unknown> | null {
  if (value === null) return null;
  if (isRecord(value)) return value;
  return invalidResponse(`${field} 响应无效`);
}

export function validCanonicalUUID(value: unknown): value is string {
  return (
    typeof value === "string" &&
    /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
      value,
    )
  );
}

export function validRFC3339Timestamp(value: unknown): value is string {
  if (typeof value !== "string") return false;
  const match = value.match(
    /^(\d{4}-\d{2}-\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d+)?(Z|[+-]\d{2}:\d{2})$/,
  );
  if (!match || !validInvoiceDate(match[1])) return false;
  const hour = Number(match[2]);
  const minute = Number(match[3]);
  const second = Number(match[4]);
  if (hour > 23 || minute > 59 || second > 59) return false;
  if (match[5] !== "Z") {
    const [offsetHour, offsetMinute] = match[5].slice(1).split(":").map(Number);
    if (offsetHour > 23 || offsetMinute > 59) return false;
  }
  return !Number.isNaN(new Date(value).getTime());
}

export function stringArray(value: unknown, label: string): string[] {
  if (
    !Array.isArray(value) ||
    value.some((item) => typeof item !== "string" || !item)
  ) {
    return invalidResponse(`${label}响应无效`);
  }
  return [...value];
}

export function normalizeHealthResponse(value: unknown): HealthResponse {
  if (
    !isRecord(value) ||
    !isRecord(value.app) ||
    !isRecord(value.api) ||
    !isRecord(value.schema)
  ) {
    return invalidResponse("健康检查响应格式无效");
  }
  const status = stringField(value, "status");
  const appName = stringField(value.app, "name");
  const appVersion = stringField(value.app, "version");
  const commit = stringField(value.app, "commit");
  const apiVersion = stringField(value.api, "version");
  if (
    !status ||
    !appName ||
    !appVersion ||
    !commit ||
    !apiVersion ||
    status.length > 32 ||
    appName.length > 128 ||
    appVersion.length > 128 ||
    commit.length > 128 ||
    apiVersion.length > 64
  ) {
    return invalidResponse("健康检查响应字段无效");
  }
  return {
    status,
    app: { name: appName, version: appVersion, commit },
    api: { version: apiVersion },
    schema: {
      version: positiveInteger(value.schema.version, "数据库 schema 版本"),
    },
  };
}

export async function getHealth(): Promise<HealthResponse> {
  return normalizeHealthResponse(await apiRequest<unknown>("/health"));
}

export function downloadFileName(
  disposition: string | null,
  fallback: string,
): string {
  let candidate = "";
  const encoded = disposition?.match(/filename\*=UTF-8''([^;]+)/i)?.[1];
  if (encoded) {
    try {
      candidate = decodeURIComponent(encoded.trim());
    } catch {
      candidate = "";
    }
  }
  if (!candidate) {
    candidate = disposition?.match(/filename="?([^";]+)"?/i)?.[1]?.trim() ?? "";
  }
  const safe = (candidate || fallback)
    // eslint-disable-next-line no-control-regex -- replaces control characters on purpose
    .replace(/[\\/\u0000-\u001f\u007f]/g, "_")
    .trim();
  return safe || "artifact";
}

export function versionedCommandHeaders(
  expectedVersion: number,
  idempotencyKey: string,
): Record<string, string> {
  return {
    ...expectedVersionHeader(expectedVersion),
    "Idempotency-Key": idempotencyKey,
  };
}
