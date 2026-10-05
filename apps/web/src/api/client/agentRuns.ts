import {
  agentFileReferenceWire,
  validAgentFileReference,
  validFileSourceProof,
} from "../../lib/agentProjectFiles";
import {
  agentRunOutputDeliveryStatuses,
  validAgentRunDeliveryState,
} from "../../lib/agentRunDelivery";
import { validAgentRunRestartRequest } from "../../lib/agentRunRestart";
import {
  validAgentRunReworkContext,
  validAgentRunReworkRequest,
} from "../../lib/agentRunRework";
import type {
  AgentRun,
  AgentRunFileAccessProviderConfirmation,
  AgentRunFileCandidate,
  AgentRunFileCandidatesResult,
  AgentRunFileSourceKind,
  AgentRunInputSlotStatus,
  AgentRunListParams,
  AgentRunListResult,
  AgentRunOutputContract,
  AgentRunOutputDeliveryStatus,
  AgentRunOutputFileContract,
  AgentRunProgress,
  AgentRunReworkContext,
  AgentRunReworkInputFile,
  AgentRunStartGate,
  AgentRunStartGateCloseReason,
  AgentRunStartGateInputSlot,
  AgentRunStatus,
  AgentRunSummary,
  ControlledFile,
  ControlledFileListParams,
  ControlledFileListResult,
  ControlledFileScope,
  CreateAgentRunOptions,
  RetryAgentRunReworkOptions,
} from "../../types/models";
import {
  ApiError,
  apiRequest,
  fieldValue,
  hasExactKeys,
  invalidResponse,
  isRecord,
  nonNegativeInteger,
  nullableNonNegativeInteger,
  nullableString,
  numberField,
  numeric,
  positiveInteger,
  stringField,
  validCanonicalUUID,
  validRFC3339Timestamp,
} from "./core";
import type { JsonRecord } from "./core";

type AgentRunMetadata = Omit<AgentRun, "resultText">;

function agentRunProgressFromRecord(
  value: unknown,
  status: AgentRunStatus,
): AgentRunProgress | undefined {
  if (value === undefined) return undefined;
  if (
    status !== "running" ||
    !isRecord(value) ||
    !hasExactKeys(value, ["phase", "elapsed_ms"]) ||
    !["preparing", "calling_model", "registering_result"].includes(
      String(value.phase),
    ) ||
    !Number.isSafeInteger(value.elapsed_ms) ||
    Number(value.elapsed_ms) < 0
  ) {
    return invalidResponse("Agent 执行进度响应格式无效");
  }
  return {
    phase: value.phase as AgentRunProgress["phase"],
    elapsedMs: Number(value.elapsed_ms),
  };
}

function agentRunOutputContractFromRecord(
  value: unknown,
): AgentRunOutputContract | undefined {
  if (value === undefined) return undefined;
  if (!isRecord(value) || typeof value.type !== "string") {
    return invalidResponse("Agent 产出契约响应格式无效");
  }
  if (value.type === "text") {
    if (!hasExactKeys(value, ["type"])) {
      return invalidResponse("Agent 产出契约响应包含未授权字段");
    }
    return { type: "text" };
  }
  if (value.type === "file") {
    if (
      !hasExactKeys(value, ["type", "name", "mime"]) ||
      typeof value.name !== "string" ||
      typeof value.mime !== "string" ||
      !validAgentRunFileNameAndMIME(value.name, value.mime)
    ) {
      return invalidResponse("Agent 文件产出契约响应格式无效");
    }
    return { type: "file", name: value.name, mime: value.mime };
  }
  if (
    value.type !== "files" ||
    !hasExactKeys(value, ["type", "files"]) ||
    !Array.isArray(value.files) ||
    value.files.length < 2 ||
    value.files.length > 4
  ) {
    return invalidResponse("Agent 多文件产出契约响应格式无效");
  }
  const files = value.files.map((file) => {
    if (
      !isRecord(file) ||
      !hasExactKeys(file, ["name", "mime"]) ||
      typeof file.name !== "string" ||
      typeof file.mime !== "string" ||
      !validAgentRunFileNameAndMIME(file.name, file.mime)
    ) {
      return invalidResponse("Agent 多文件产出契约响应格式无效");
    }
    return { name: file.name, mime: file.mime };
  });
  if (
    new Set(files.map((file) => file.name.toLowerCase())).size !== files.length
  ) {
    return invalidResponse("Agent 多文件产出契约包含重复文件名");
  }
  return { type: "files", files };
}

function validAgentRunLifecycleMetadata(
  status: AgentRunStatus,
  resultBytes: number | null,
  errorCode: string | null,
  startedAt: string | null,
  completedAt: string | null,
) {
  const activeTiming =
    status === "queued"
      ? completedAt === null
      : status === "running"
        ? startedAt !== null && completedAt === null
        : startedAt !== null && completedAt !== null;
  if (!activeTiming) return false;
  if (status === "succeeded") {
    return (
      resultBytes !== null &&
      resultBytes >= 1 &&
      resultBytes <= 65_536 &&
      errorCode === null
    );
  }
  if (resultBytes !== null) return false;
  if (status === "failed") return errorCode !== null;
  return errorCode === null;
}

export const agentRunStartGateCloseReasons = [
  "predecessor_failed",
  "predecessor_cancelled",
  "predecessor_retained",
  "predecessor_not_accepted",
  "predecessor_unavailable",
  "predecessor_output_unavailable",
  "expired",
  "run_not_queued",
] as const;

export const agentRunInputSlotStatuses = [
  "awaiting_predecessor",
  "awaiting_confirmation",
  "bound",
  "closed",
] as const;

export function expectedFilesFromRecord(
  raw: unknown,
): AgentRunOutputFileContract[] {
  if (
    !Array.isArray(raw) ||
    raw.length < 1 ||
    raw.length > 4 ||
    !raw.every(
      (file) =>
        isRecord(file) &&
        hasExactKeys(file, ["name", "mime"]) &&
        typeof file.name === "string" &&
        typeof file.mime === "string" &&
        validAgentRunFileNameAndMIME(file.name, file.mime),
    ) ||
    new Set(raw.map((file) => String(file.name).toLowerCase())).size !==
      raw.length
  )
    return invalidResponse("Agent 执行的待绑定输入声明格式无效");
  return raw.map((file) => ({ name: file.name, mime: file.mime }));
}

/**
 * The slot follows its gate: it waits while the gate waits, is bound exactly
 * when the gate is released, and is closed with it. Only an accepted
 * predecessor can hand files on.
 */
function agentRunInputSlotFromRecord(
  raw: unknown,
  gateStatus: string,
  require: string,
): AgentRunStartGateInputSlot {
  if (
    !isRecord(raw) ||
    !hasExactKeys(raw, ["source", "status", "expected_files"]) ||
    raw.source !== "predecessor_files" ||
    require !== "accepted" ||
    !agentRunInputSlotStatuses.includes(raw.status as never) ||
    (gateStatus === "waiting" &&
      raw.status !== "awaiting_predecessor" &&
      raw.status !== "awaiting_confirmation") ||
    (gateStatus === "released" && raw.status !== "bound") ||
    (gateStatus === "closed" && raw.status !== "closed")
  )
    return invalidResponse("Agent 执行的待绑定输入与启动条件不一致");
  return {
    source: "predecessor_files",
    status: raw.status as AgentRunInputSlotStatus,
    expectedFiles: expectedFilesFromRecord(raw.expected_files),
  };
}

/**
 * A gate must agree with the Run it guards: a waiting gate only exists on a
 * queued Run, and a gate closed for a predecessor reason cancelled that Run.
 */
export function agentRunStartGateFromRecord(
  raw: unknown,
  status: string,
  deliveryStatus: string,
): AgentRunStartGate | undefined {
  if (raw === undefined) return undefined;
  if (!isRecord(raw)) return invalidResponse("Agent 执行启动条件格式无效");
  const closed = raw.status === "closed";
  const hasSlot = Object.hasOwn(raw, "input_slot");
  // The reconciler closes the gate of a cancelled Run a moment after the
  // cancellation commits; until then the gate is still reported as waiting.
  // By the reconciliation rule (ADR-030) it can only close as
  // `run_not_queued`, so it is read as that instead of being rejected.
  const settling =
    raw.status === "waiting" &&
    status === "cancelled" &&
    deliveryStatus === "not_ready";
  const keys = [
    "predecessor_run_id",
    "require",
    "expires_at",
    "status",
    ...(closed ? ["close_reason"] : []),
    ...(hasSlot ? ["input_slot"] : []),
  ];
  if (
    Object.keys(raw).length !== keys.length ||
    !keys.every((key) => Object.hasOwn(raw, key)) ||
    typeof raw.predecessor_run_id !== "string" ||
    !validCanonicalUUID(raw.predecessor_run_id) ||
    (raw.require !== "submitted" && raw.require !== "accepted") ||
    typeof raw.expires_at !== "string" ||
    !Number.isFinite(Date.parse(raw.expires_at)) ||
    !["waiting", "released", "closed"].includes(String(raw.status)) ||
    (closed &&
      !agentRunStartGateCloseReasons.includes(raw.close_reason as never)) ||
    (raw.status === "waiting" &&
      !settling &&
      (status !== "queued" || deliveryStatus !== "not_ready")) ||
    (closed &&
      raw.close_reason !== "run_not_queued" &&
      (status !== "cancelled" || deliveryStatus !== "not_ready"))
  ) {
    return invalidResponse("Agent 执行启动条件与执行状态不一致");
  }
  // Without a slot nothing can be missing from an accepted predecessor.
  if (!hasSlot && raw.close_reason === "predecessor_output_unavailable")
    return invalidResponse("Agent 执行启动条件与执行状态不一致");
  const slot = hasSlot
    ? agentRunInputSlotFromRecord(
        raw.input_slot,
        String(raw.status),
        raw.require,
      )
    : undefined;
  return {
    predecessorRunId: raw.predecessor_run_id,
    require: raw.require,
    expiresAt: raw.expires_at,
    status: settling ? "closed" : (raw.status as AgentRunStartGate["status"]),
    ...(closed
      ? { closeReason: raw.close_reason as AgentRunStartGateCloseReason }
      : settling
        ? { closeReason: "run_not_queued" as const }
        : {}),
    ...(slot
      ? { inputSlot: settling ? { ...slot, status: "closed" as const } : slot }
      : {}),
  };
}

function agentRunMetadataFromRecord(value: JsonRecord): AgentRunMetadata {
  const status = stringField(value, "status");
  if (
    ![
      "queued",
      "running",
      "succeeded",
      "failed",
      "cancelled",
      "interrupted",
    ].includes(status ?? "")
  ) {
    return invalidResponse("Agent 执行状态无效");
  }
  const deliveryStatus = stringField(
    value,
    "output_delivery_status",
    "outputDeliveryStatus",
  );
  if (!agentRunOutputDeliveryStatuses.includes(deliveryStatus as never)) {
    return invalidResponse("Agent 产出登记状态无效");
  }
  const progress = agentRunProgressFromRecord(
    value.progress,
    status as AgentRunStatus,
  );
  const deliveryErrorCode = nullableString(
    fieldValue(value, "output_delivery_error_code", "outputDeliveryErrorCode"),
  );
  const submissionId = nullableString(
    fieldValue(value, "submission_id", "submissionId"),
  );
  const artifactId = nullableString(
    fieldValue(value, "artifact_id", "artifactId"),
  );
  const resultBytes = nullableNonNegativeInteger(
    fieldValue(value, "result_bytes", "resultBytes"),
    "Agent 执行产出字节数",
  );
  const errorCode = nullableString(
    fieldValue(value, "error_code", "errorCode"),
  );
  const startedAt = nullableString(
    fieldValue(value, "started_at", "startedAt"),
  );
  const completedAt = nullableString(
    fieldValue(value, "completed_at", "completedAt"),
  );
  const id = stringField(value, "id");
  const taskId = stringField(value, "task_id", "taskId");
  const assignmentId = stringField(value, "assignment_id", "assignmentId");
  const actorId = stringField(value, "actor_id", "actorId");
  const adapterId = stringField(value, "adapter_id", "adapterId");
  const createdByActorId = stringField(
    value,
    "created_by_actor_id",
    "createdByActorId",
  );
  const parentRunId = nullableString(
    fieldValue(value, "parent_run_id", "parentRunId"),
  );
  const rawRestart = fieldValue(value, "restart_of_run_id", "restartOfRunId");
  if (
    (Object.hasOwn(value, "restart_of_run_id") &&
      Object.hasOwn(value, "restartOfRunId")) ||
    ((Object.hasOwn(value, "restart_of_run_id") ||
      Object.hasOwn(value, "restartOfRunId")) &&
      rawRestart !== null &&
      (typeof rawRestart !== "string" || !validCanonicalUUID(rawRestart)))
  )
    return invalidResponse("Agent 新执行来源身份无效");
  const restartOfRunId = rawRestart == null ? null : (rawRestart as string);
  if (
    restartOfRunId !== null &&
    (!validCanonicalUUID(restartOfRunId) ||
      restartOfRunId === id ||
      (Object.hasOwn(value, "parent_run_id") &&
        Object.hasOwn(value, "parentRunId")) ||
      ["parent_run_id", "parentRunId"].some(
        (key) => Object.hasOwn(value, key) && value[key] !== null,
      ) ||
      parentRunId !== null)
  )
    return invalidResponse("Agent 新执行来源身份无效");
  const providerId = stringField(value, "provider_id", "providerId");
  const model = stringField(value, "model");
  const createdAt = stringField(value, "created_at", "createdAt");
  const outputContract = agentRunOutputContractFromRecord(
    fieldValue(value, "output_contract", "outputContract"),
  );
  if (
    !validCanonicalUUID(id) ||
    !validCanonicalUUID(taskId) ||
    !validCanonicalUUID(assignmentId) ||
    !validCanonicalUUID(actorId) ||
    !validCanonicalUUID(adapterId) ||
    !validCanonicalUUID(createdByActorId) ||
    (parentRunId !== null && !validCanonicalUUID(parentRunId)) ||
    !validCanonicalUUID(providerId) ||
    !model ||
    !createdAt ||
    (submissionId !== null && !validCanonicalUUID(submissionId)) ||
    (artifactId !== null && !validCanonicalUUID(artifactId))
  ) {
    return invalidResponse("Agent 执行身份响应格式无效");
  }
  if (
    !validAgentRunDeliveryState(
      status as AgentRunStatus,
      deliveryStatus as AgentRunOutputDeliveryStatus,
      deliveryErrorCode,
      submissionId,
      artifactId,
    )
  ) {
    return invalidResponse("Agent 执行与产出登记状态不一致");
  }
  if (
    !validAgentRunLifecycleMetadata(
      status as AgentRunStatus,
      resultBytes,
      errorCode,
      startedAt,
      completedAt,
    )
  ) {
    return invalidResponse("Agent 执行生命周期状态不一致");
  }
  const startGate = agentRunStartGateFromRecord(
    value.start_gate,
    status as string,
    deliveryStatus as string,
  );
  return {
    id,
    taskId,
    assignmentId,
    actorId,
    adapterId,
    createdByActorId,
    parentRunId,
    ...(restartOfRunId !== null ? { restartOfRunId } : {}),
    ...(startGate ? { startGate } : {}),
    attempt: positiveInteger(
      fieldValue(value, "attempt"),
      "Agent 执行尝试次数",
    ),
    status: status as AgentRunStatus,
    ...(progress ? { progress } : {}),
    providerId,
    model,
    outputContract,
    resultBytes,
    errorCode,
    outputDeliveryStatus: deliveryStatus as AgentRunOutputDeliveryStatus,
    outputDeliveryErrorCode: deliveryErrorCode,
    submissionId,
    artifactId,
    startedAt,
    completedAt,
    createdAt,
  };
}

function agentRunFromRecord(value: unknown, allowDetail = false): AgentRun {
  if (!isRecord(value)) return invalidResponse("Agent 执行记录响应格式无效");
  if (
    !allowDetail &&
    [
      "model_protocol",
      "modelProtocol",
      "max_output_tokens",
      "maxOutputTokens",
      "execution_provider_confirmation",
      "executionProviderConfirmation",
    ].some((key) => Object.hasOwn(value, key))
  )
    return invalidResponse("执行列表或回执不能携带私有执行详情");
  const run = agentRunMetadataFromRecord(value);
  const resultText = nullableString(
    fieldValue(value, "result_text", "resultText"),
  );
  if (
    (run.status === "succeeded" &&
      (resultText === null ||
        run.resultBytes !== new TextEncoder().encode(resultText).byteLength)) ||
    (run.status !== "succeeded" && resultText !== null)
  ) {
    return invalidResponse("Agent 执行生命周期状态不一致");
  }
  const version = fieldValue(
    value,
    "execution_contract_version",
    "executionContractVersion",
  );
  // Schema 074 backfilled 0 for Runs created before execution identity was
  // frozen. They stay readable history; every retry path is version-gated.
  if (
    version !== undefined &&
    (!Number.isSafeInteger(version) ||
      Number(version) < 0 ||
      Number(version) > 7)
  )
    return invalidResponse("Agent 执行契约版本无效");
  // Contract v7 and the input slot exist only together.
  if (
    version !== undefined &&
    (version === 7) !== (run.startGate?.inputSlot !== undefined) &&
    (version === 7 || run.startGate !== undefined)
  )
    return invalidResponse("Agent 执行契约与待绑定输入不一致");
  if (
    !allowDetail &&
    (version === 5 || version === 6 || version === 7) &&
    [
      "input_files",
      "inputFiles",
      "rework_context",
      "reworkContext",
      "rework_provider_confirmation",
      "reworkProviderConfirmation",
    ].some((key) => Object.hasOwn(value, key))
  )
    return invalidResponse("执行列表或回执不能携带私有执行详情");
  return {
    ...run,
    resultText,
    ...(version !== undefined
      ? { executionContractVersion: Number(version) }
      : {}),
  };
}

export async function getTaskAgentRuns(taskId: string): Promise<AgentRun[]> {
  const payload = await apiRequest<unknown>(
    `/api/v1/tasks/${encodeURIComponent(taskId)}/agent-runs`,
  );
  if (!isRecord(payload) || !Array.isArray(payload.data)) {
    return invalidResponse("Agent 执行列表响应格式无效");
  }
  if (
    payload.data.some(
      (value) =>
        isRecord(value) &&
        [
          "rework_context",
          "reworkContext",
          "input_files",
          "rework_provider_confirmation",
        ].some((key) => Object.hasOwn(value, key)),
    )
  )
    return invalidResponse("执行列表不能携带私有返工上下文");
  const runs = payload.data.map((row) => agentRunFromRecord(row));
  if (runs.some((run) => run.taskId !== taskId)) {
    return invalidResponse("Agent 执行列表包含其他任务的记录");
  }
  return runs;
}

export const agentRunFileLimits = Object.freeze({
  maxFiles: 4,
  maxFileBytes: 65_536,
  maxTotalBytes: 131_072,
  maxResultBytes: 65_536,
});

export const agentRunFileOutputPresets = [
  { type: "file", name: "agent-output.md", mime: "text/markdown" },
  { type: "file", name: "agent-output.txt", mime: "text/plain" },
  { type: "file", name: "agent-output.json", mime: "application/json" },
] as const satisfies readonly AgentRunOutputContract[];

const agentRunFileSourceKinds: readonly AgentRunFileSourceKind[] = [
  "task_artifact",
  "project_attachment",
];

const agentRunTextFileExtensions = new Map<string, ReadonlySet<string>>([
  ["text/plain", new Set([".txt", ".log"])],
  ["text/markdown", new Set([".md", ".markdown"])],
  ["text/csv", new Set([".csv"])],
  ["text/html", new Set([".html", ".htm"])],
  ["text/css", new Set([".css"])],
  ["text/javascript", new Set([".js", ".mjs", ".cjs", ".jsx"])],
  ["application/javascript", new Set([".js", ".mjs", ".cjs", ".jsx"])],
  ["text/typescript", new Set([".ts", ".tsx"])],
  ["application/json", new Set([".json"])],
  ["application/xml", new Set([".xml"])],
  ["text/xml", new Set([".xml"])],
  ["application/yaml", new Set([".yaml", ".yml"])],
  ["text/yaml", new Set([".yaml", ".yml"])],
  ["text/x-go", new Set([".go"])],
  ["text/x-python", new Set([".py"])],
  ["text/x-shellscript", new Set([".sh"])],
  ["text/x-sql", new Set([".sql"])],
]);

function validAgentRunFileName(name: unknown): name is string {
  if (
    typeof name !== "string" ||
    !name ||
    name.trim() !== name ||
    name === "." ||
    name === ".." ||
    new TextEncoder().encode(name).byteLength > 255 ||
    // eslint-disable-next-line no-control-regex -- rejects control characters on purpose
    /[\\/<>:"|?*\u0000-\u001f\u007f]/.test(name)
  ) {
    return false;
  }
  return true;
}

function validAgentRunFileMIME(mime: unknown): mime is string {
  return (
    typeof mime === "string" &&
    new TextEncoder().encode(mime).byteLength <= 255 &&
    // eslint-disable-next-line no-control-regex -- rejects control characters on purpose
    !/[\u0000-\u001f\u007f]/.test(mime)
  );
}

export function validAgentRunFileNameAndMIME(name: unknown, mime: unknown) {
  if (!validAgentRunFileName(name) || !validAgentRunFileMIME(mime)) {
    return false;
  }
  const dot = name.lastIndexOf(".");
  const extension = dot >= 0 ? name.slice(dot).toLowerCase() : "";
  return agentRunTextFileExtensions.get(mime)?.has(extension) ?? false;
}

export function agentRunFileCandidateFromRecord(
  value: unknown,
  limits: AgentRunFileCandidatesResult["limits"],
  allowProject = false,
): AgentRunFileCandidate {
  if (!isRecord(value)) return invalidResponse("Agent 受控文件响应格式无效");
  const eligible = value.eligible;
  const expectedKeys =
    eligible === true
      ? [
          "source_kind",
          "id",
          "name",
          "mime",
          "size_bytes",
          "sha256",
          "eligible",
          "created_at",
        ]
      : [
          "source_kind",
          "id",
          "name",
          "mime",
          "size_bytes",
          "sha256",
          "eligible",
          "error_code",
          "created_at",
        ];
  if (allowProject && value.source_kind === "project_task_artifact")
    expectedKeys.push("source_task");
  if (!hasExactKeys(value, expectedKeys)) {
    return invalidResponse("Agent 受控文件响应包含未授权字段");
  }
  const sourceKind = stringField(value, "source_kind");
  const id = stringField(value, "id");
  const name = stringField(value, "name");
  const mime = typeof value.mime === "string" ? value.mime : undefined;
  const sizeBytes = numberField(value, "size_bytes");
  const sha256 = typeof value.sha256 === "string" ? value.sha256 : undefined;
  const createdAt = stringField(value, "created_at");
  const errorCode = stringField(value, "error_code") ?? null;
  const validEligibleMetadata =
    validAgentRunFileNameAndMIME(name, mime) &&
    Number.isSafeInteger(sizeBytes) &&
    (sizeBytes ?? 0) > 0 &&
    /^[a-f0-9]{64}$/.test(sha256 ?? "") &&
    (sizeBytes ?? 0) <= limits.maxFileBytes &&
    (sizeBytes ?? 0) <= limits.maxTotalBytes;
  const validIneligibleMetadata =
    validAgentRunFileName(name) &&
    validAgentRunFileMIME(mime) &&
    Number.isSafeInteger(sizeBytes) &&
    (sizeBytes ?? 0) >= 0 &&
    (sha256 === "" || /^[a-f0-9]{64}$/.test(sha256 ?? ""));
  if (
    ![
      ...agentRunFileSourceKinds,
      ...(allowProject ? ["project_task_artifact"] : []),
    ].includes(sourceKind as AgentRunFileSourceKind) ||
    !validFileSourceProof(value, allowProject) ||
    !validCanonicalUUID(id) ||
    typeof eligible !== "boolean" ||
    !validRFC3339Timestamp(createdAt) ||
    (eligible && errorCode !== null) ||
    (!eligible && !/^[A-Z][A-Z0-9_]{0,63}$/.test(errorCode ?? "")) ||
    (eligible ? !validEligibleMetadata : !validIneligibleMetadata)
  ) {
    return invalidResponse("Agent 受控文件响应身份或元数据无效");
  }
  return {
    sourceKind: sourceKind as AgentRunFileSourceKind,
    ...(value.source_kind === "project_task_artifact"
      ? {
          sourceTask:
            value.source_task as import("../../types/models").AgentProjectFileSource,
        }
      : {}),
    id,
    name: name!,
    mime: mime!,
    sizeBytes: sizeBytes!,
    sha256: sha256!,
    eligible,
    errorCode,
    createdAt,
  };
}

export async function getTaskAgentRunFiles(
  taskId: string,
  signal?: AbortSignal,
): Promise<AgentRunFileCandidatesResult> {
  const payload = await apiRequest<unknown>(
    `/api/v1/tasks/${encodeURIComponent(taskId)}/agent-run-files`,
    { signal },
  );
  if (
    !isRecord(payload) ||
    !hasExactKeys(payload, ["data", "meta"]) ||
    !Array.isArray(payload.data) ||
    !isRecord(payload.meta) ||
    !hasExactKeys(payload.meta, ["limits"]) ||
    !isRecord(payload.meta.limits) ||
    !hasExactKeys(payload.meta.limits, [
      "max_files",
      "max_file_bytes",
      "max_total_bytes",
      "max_result_bytes",
    ])
  ) {
    return invalidResponse("Agent 受控文件列表响应格式无效");
  }
  const limits = {
    maxFiles: numberField(payload.meta.limits, "max_files"),
    maxFileBytes: numberField(payload.meta.limits, "max_file_bytes"),
    maxTotalBytes: numberField(payload.meta.limits, "max_total_bytes"),
    maxResultBytes: numberField(payload.meta.limits, "max_result_bytes"),
  };
  if (
    !Object.values(limits).every(
      (value) => Number.isSafeInteger(value) && (value ?? 0) > 0,
    ) ||
    (limits.maxFiles ?? 0) > agentRunFileLimits.maxFiles ||
    (limits.maxFileBytes ?? 0) > agentRunFileLimits.maxFileBytes ||
    (limits.maxTotalBytes ?? 0) > agentRunFileLimits.maxTotalBytes ||
    (limits.maxResultBytes ?? 0) > agentRunFileLimits.maxResultBytes ||
    (limits.maxTotalBytes ?? 0) >
      (limits.maxFiles ?? 0) * (limits.maxFileBytes ?? 0)
  ) {
    return invalidResponse("Agent 受控文件限制与客户端安全边界不一致");
  }
  const parsedLimits = {
    maxFiles: limits.maxFiles!,
    maxFileBytes: limits.maxFileBytes!,
    maxTotalBytes: limits.maxTotalBytes!,
    maxResultBytes: limits.maxResultBytes!,
  };
  const items = payload.data.map((row) =>
    agentRunFileCandidateFromRecord(row, parsedLimits),
  );
  const seen = new Set<string>();
  for (const item of items) {
    if (seen.has(item.id)) {
      return invalidResponse("Agent 受控文件列表包含重复身份");
    }
    seen.add(item.id);
  }
  return {
    items,
    limits: parsedLimits,
  };
}

export async function getAgentRun(
  runId: string,
  signal?: AbortSignal,
): Promise<AgentRun> {
  const payload = await apiRequest<unknown>(
    `/api/v1/agent-runs/${encodeURIComponent(runId)}`,
    { signal },
  );
  if (!isRecord(payload) || !isRecord(payload.data)) {
    return invalidResponse("Agent 执行详情响应格式无效");
  }
  const run = agentRunFromRecord(payload.data, true);
  // v7 freezes the same protocol facts as v6. Its files are the ones a
  // person confirmed later, so there may be none yet, and never any other kind.
  const v7 = run.executionContractVersion === 7;
  const v6 = run.executionContractVersion === 6 || v7;
  const v5 = run.executionContractVersion === 5 || v6;
  const privateV5Keys = [
    "model_protocol",
    "max_output_tokens",
    "execution_provider_confirmation",
  ];
  if (
    ["modelProtocol", "maxOutputTokens", "executionProviderConfirmation"].some(
      (key) => Object.hasOwn(payload.data as JsonRecord, key),
    ) ||
    (!v5 &&
      privateV5Keys.some((key) =>
        Object.hasOwn(payload.data as JsonRecord, key),
      ))
  )
    return invalidResponse("执行协议详情与冻结版本不一致");
  if (run.executionContractVersion === 4 || v5) {
    const context = payload.data.rework_context;
    const confirmation = v5
      ? payload.data.execution_provider_confirmation
      : payload.data.rework_provider_confirmation;
    const inputFiles = Object.hasOwn(payload.data, "input_files")
      ? payload.data.input_files
      : [];
    if (
      (v5
        ? Object.hasOwn(payload.data, "rework_context") &&
          !validAgentRunReworkContext(context)
        : !validAgentRunReworkContext(context)) ||
      (v5 &&
        (!run.outputContract ||
          (v6 && payload.data.model_protocol === "openai_chat"
            ? payload.data.max_output_tokens !== 0
            : payload.data.model_protocol !== "anthropic_messages" ||
              payload.data.max_output_tokens !== 8192) ||
          [
            "rework_provider_confirmation",
            "reworkProviderConfirmation",
            "inputFiles",
            "reworkContext",
          ].some((key) => Object.hasOwn(payload.data as JsonRecord, key)))) ||
      !isRecord(confirmation) ||
      !hasExactKeys(confirmation, ["version", "config_version", "kind"]) ||
      !Number.isSafeInteger(confirmation.version) ||
      Number(confirmation.version) < 1 ||
      !Number.isSafeInteger(confirmation.config_version) ||
      Number(confirmation.config_version) < 1 ||
      !["local", "remote"].includes(String(confirmation.kind)) ||
      (v5 &&
        payload.data.model_protocol === "anthropic_messages" &&
        confirmation.kind !== "remote") ||
      !Array.isArray(inputFiles) ||
      inputFiles.length > 4 ||
      !inputFiles.every(
        (file) =>
          isRecord(file) &&
          hasExactKeys(file, [
            "source_kind",
            "id",
            "name",
            "mime",
            "size_bytes",
            "sha256",
            ...(file.source_kind === "project_task_artifact"
              ? ["source_task"]
              : []),
          ]) &&
          [
            ...agentRunFileSourceKinds,
            ...(v6 ? ["project_task_artifact"] : []),
          ].includes(file.source_kind as AgentRunFileSourceKind) &&
          validFileSourceProof(file, v6, run.taskId) &&
          validCanonicalUUID(file.id) &&
          typeof file.name === "string" &&
          typeof file.mime === "string" &&
          validAgentRunFileNameAndMIME(file.name, file.mime) &&
          Number.isSafeInteger(file.size_bytes) &&
          Number(file.size_bytes) > 0 &&
          Number(file.size_bytes) <= 65536 &&
          typeof file.sha256 === "string" &&
          /^[a-f0-9]{64}$/.test(file.sha256),
      ) ||
      (v6 &&
        !v7 &&
        !inputFiles.some(
          (file) => file.source_kind === "project_task_artifact",
        )) ||
      (v7 &&
        (Object.hasOwn(payload.data, "rework_context") ||
          inputFiles.some(
            (file) => file.source_kind !== "project_task_artifact",
          ) ||
          inputFiles.length > 0 !==
            (run.startGate?.inputSlot?.status === "bound"))) ||
      // A v7 Run nobody has confirmed yet has no file and so no project.
      (v6 &&
        new Set(
          inputFiles
            .filter((file) => file.source_kind === "project_task_artifact")
            .map((file) => file.source_task.project_id),
        ).size !== (v7 && inputFiles.length === 0 ? 0 : 1)) ||
      new Set(inputFiles.map((file) => file.id)).size !== inputFiles.length ||
      inputFiles.reduce((sum, file) => sum + Number(file.size_bytes), 0) >
        131072
    )
      return invalidResponse("返工执行详情不完整，不能确认重试");
    if (context !== undefined)
      run.reworkContext = context as AgentRunReworkContext;
    const parsedConfirmation = {
      version: Number(confirmation.version),
      configVersion: Number(confirmation.config_version),
      kind: confirmation.kind as "local" | "remote",
    };
    if (v5) {
      run.modelProtocol = payload.data.model_protocol as
        "openai_chat" | "anthropic_messages";
      run.maxOutputTokens = payload.data.max_output_tokens as 0 | 8192;
      run.executionProviderConfirmation = parsedConfirmation;
      run.executionInputFiles = inputFiles as AgentRunReworkInputFile[];
    } else {
      run.reworkProviderConfirmation = parsedConfirmation;
      run.reworkInputFiles = inputFiles as AgentRunReworkInputFile[];
    }
  } else if (
    ["rework_context", "input_files", "rework_provider_confirmation"].some(
      (key) => Object.hasOwn(payload.data as JsonRecord, key),
    )
  )
    return invalidResponse("旧执行契约不能携带返工上下文");
  if (run.id !== runId) {
    return invalidResponse("Agent 执行详情身份不匹配");
  }
  return run;
}

export function parseAgentRunSummaryRecord(value: unknown): AgentRunSummary {
  if (!isRecord(value)) return invalidResponse("Agent 执行列表响应格式无效");
  if (
    [
      "model_protocol",
      "modelProtocol",
      "max_output_tokens",
      "maxOutputTokens",
      "execution_provider_confirmation",
      "executionProviderConfirmation",
      "inputFiles",
      "reworkProviderConfirmation",
    ].some((key) => Object.hasOwn(value, key))
  )
    return invalidResponse("执行列表不能携带私有执行详情");
  if (
    [
      "result_text",
      "resultText",
      "rework_context",
      "reworkContext",
      "input_files",
      "rework_provider_confirmation",
    ].some((key) => Object.hasOwn(value, key))
  ) {
    return invalidResponse("Agent 执行摘要不能包含产出正文");
  }
  const run = agentRunMetadataFromRecord(value);
  const taskTitle = stringField(value, "task_title", "taskTitle");
  if (!taskTitle) {
    return invalidResponse("Agent 执行摘要响应格式无效");
  }
  return {
    ...run,
    taskTitle,
  };
}

export async function getAgentRuns(
  input: AgentRunListParams = {},
  signal?: AbortSignal,
): Promise<AgentRunListResult> {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 20),
  });
  if (input.status) params.set("status", input.status);
  if (input.outputDeliveryStatus) {
    params.set("output_delivery_status", input.outputDeliveryStatus);
  }
  if (input.attentionOnly) params.set("attention", "1");
  if (input.unplannedOnly) params.set("plan_link", "unplanned");
  const payload = await apiRequest<unknown>(`/api/v1/agent-runs?${params}`, {
    signal,
  });
  if (
    !isRecord(payload) ||
    !Array.isArray(payload.data) ||
    !isRecord(payload.meta)
  ) {
    return invalidResponse("Agent 执行列表响应格式无效");
  }
  return {
    items: payload.data.map(parseAgentRunSummaryRecord),
    meta: {
      page: numeric(payload.meta.page, input.page ?? 1),
      pageSize: numeric(
        payload.meta.page_size ?? payload.meta.pageSize,
        input.pageSize ?? 20,
      ),
      total: numeric(payload.meta.total),
      activeTotal: nonNegativeInteger(
        fieldValue(payload.meta, "active_total", "activeTotal"),
        "Agent 执行中总数",
      ),
      pendingDeliveryTotal: nonNegativeInteger(
        fieldValue(
          payload.meta,
          "pending_delivery_total",
          "pendingDeliveryTotal",
        ),
        "Agent 待登记总数",
      ),
      succeededTotal: nonNegativeInteger(
        fieldValue(payload.meta, "succeeded_total", "succeededTotal"),
        "Agent 已成功总数",
      ),
    },
  };
}

const controlledFileScopes: readonly string[] = [
  "artifact",
  "client_attachment",
  "project_attachment",
  "knowledge_document",
];

function controlledFileFromRecord(value: unknown): ControlledFile {
  if (!isRecord(value)) return invalidResponse("受控文件列表响应格式无效");
  const scope = stringField(value, "scope") ?? "";
  if (!controlledFileScopes.includes(scope)) {
    return invalidResponse("受控文件范围无效");
  }
  return {
    id: stringField(value, "id") ?? "",
    scope: scope as ControlledFileScope,
    name: stringField(value, "name") ?? "",
    mimeType: nullableString(fieldValue(value, "mime_type", "mimeType")),
    sizeBytes: nullableNonNegativeInteger(
      fieldValue(value, "size_bytes", "sizeBytes"),
      "受控文件字节数",
    ),
    sha256: nullableString(fieldValue(value, "sha256")),
    ownerLabel: stringField(value, "owner_label", "ownerLabel") ?? "",
    contentRoute: stringField(value, "content_route", "contentRoute") ?? "",
    updatedAt: stringField(value, "updated_at", "updatedAt") ?? "",
  };
}

export async function getControlledFiles(
  input: ControlledFileListParams = {},
  signal?: AbortSignal,
): Promise<ControlledFileListResult> {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 50),
  });
  if (input.scope) params.set("scope", input.scope);
  const payload = await apiRequest<unknown>(`/api/v1/files?${params}`, {
    signal,
  });
  if (
    !isRecord(payload) ||
    !Array.isArray(payload.data) ||
    !isRecord(payload.meta)
  ) {
    return invalidResponse("受控文件列表响应格式无效");
  }
  return {
    items: payload.data.map(controlledFileFromRecord),
    meta: {
      page: numeric(payload.meta.page, input.page ?? 1),
      pageSize: numeric(
        payload.meta.page_size ?? payload.meta.pageSize,
        input.pageSize ?? 50,
      ),
      total: numeric(payload.meta.total),
    },
  };
}

export async function createAgentRun(
  taskId: string,
  providerId: string,
  options?: CreateAgentRunOptions,
): Promise<AgentRun> {
  const rework = options?.rework;
  const restart = options?.restart;
  if (
    restart !== undefined &&
    (!validAgentRunRestartRequest(restart) ||
      options?.autoAssign !== undefined ||
      (rework && rework.expectedTaskVersion !== restart.expectedTaskVersion))
  )
    throw new ApiError("新执行来源或任务版本无效；请先完成责任分派。", {
      code: "INVALID_INPUT",
    });
  if (
    restart !== undefined &&
    (options?.confirmRestart !== true ||
      typeof options.restartPreviewHash !== "string" ||
      !/^[a-f0-9]{64}$/.test(options.restartPreviewHash))
  )
    throw new ApiError("请完整预览并独立确认按当前事实重新执行。", {
      code: "CONFIRMATION_REQUIRED",
    });
  if (
    restart === undefined &&
    (options?.confirmRestart !== undefined ||
      options?.restartPreviewHash !== undefined)
  )
    throw new ApiError("没有新执行来源时不能提交关联确认。", {
      code: "INVALID_INPUT",
    });
  if (
    rework !== undefined &&
    (!validAgentRunReworkRequest(rework) || options?.autoAssign !== undefined)
  ) {
    throw new ApiError(
      "返工来源无效；请先完成责任分派，不能在返工启动中自动分派。",
      { code: "INVALID_INPUT" },
    );
  }
  const reworkConfirmation = options?.reworkProviderConfirmation;
  if (rework !== undefined && options?.confirmReworkContext !== true)
    throw new ApiError("必须单独确认发送返工意见与旧产出。", {
      code: "AGENT_REWORK_CONFIRMATION_REQUIRED",
    });
  if (
    rework !== undefined &&
    (!isRecord(reworkConfirmation) ||
      !hasExactKeys(reworkConfirmation, ["version", "configVersion", "kind"]) ||
      !Number.isSafeInteger(reworkConfirmation.version) ||
      reworkConfirmation.version < 1 ||
      !Number.isSafeInteger(reworkConfirmation.configVersion) ||
      reworkConfirmation.configVersion < 1 ||
      !["local", "remote"].includes(reworkConfirmation.kind))
  )
    throw new ApiError("请重新确认返工 Provider 的版本与本地/在线类型。", {
      code: "INVALID_INPUT",
    });
  if (
    rework === undefined &&
    (options?.confirmReworkContext !== undefined ||
      reworkConfirmation !== undefined)
  )
    throw new ApiError("未选择返工来源，不能提交返工确认。", {
      code: "INVALID_INPUT",
    });
  const autoAssign = options?.autoAssign;
  if (
    autoAssign !== undefined &&
    (!isRecord(autoAssign) ||
      !hasExactKeys(autoAssign, ["actorId", "expectedTaskVersion"]) ||
      !validCanonicalUUID(autoAssign.actorId) ||
      !Number.isSafeInteger(autoAssign.expectedTaskVersion) ||
      autoAssign.expectedTaskVersion < 1)
  ) {
    throw new ApiError("Agent 自动分派身份无效。", {
      code: "INVALID_INPUT",
    });
  }
  const inputFiles = options?.inputFiles ?? [];
  if (inputFiles.length > agentRunFileLimits.maxFiles) {
    throw new ApiError("受控输入文件不能超过 4 个。", {
      code: "INVALID_INPUT",
    });
  }
  const seenFileIds = new Set<string>();
  for (const file of inputFiles) {
    if (!validAgentFileReference(file) || seenFileIds.has(file.id)) {
      throw new ApiError("受控输入文件身份无效。", {
        code: "INVALID_INPUT",
      });
    }
    seenFileIds.add(file.id);
  }
  const hasProjectFiles = inputFiles.some(
    (file) => file.sourceKind === "project_task_artifact",
  );
  if (
    hasProjectFiles
      ? options?.confirmProjectTaskFiles !== true
      : options?.confirmProjectTaskFiles !== undefined
  )
    throw new ApiError("跨任务文件须核对已验收来源并单独确认。", {
      code: "AGENT_PROJECT_FILE_CONFIRMATION_REQUIRED",
    });
  const outputContract = options?.outputContract ?? { type: "text" as const };
  const validOutputFile = (value: unknown) =>
    isRecord(value) &&
    hasExactKeys(value, ["name", "mime"]) &&
    agentRunFileOutputPresets.some(
      (preset) => preset.name === value.name && preset.mime === value.mime,
    );
  let validOutputContract = false;
  if (outputContract.type === "text") {
    validOutputContract = hasExactKeys(outputContract, ["type"]);
  } else if (outputContract.type === "file") {
    validOutputContract =
      hasExactKeys(outputContract, ["type", "name", "mime"]) &&
      validOutputFile({
        name: outputContract.name,
        mime: outputContract.mime,
      });
  } else if (outputContract.type === "files") {
    validOutputContract =
      hasExactKeys(outputContract, ["type", "files"]) &&
      Array.isArray(outputContract.files) &&
      outputContract.files.length >= 2 &&
      outputContract.files.length <= agentRunFileLimits.maxFiles &&
      outputContract.files.every(validOutputFile) &&
      new Set(outputContract.files.map((file) => file.name.toLowerCase()))
        .size === outputContract.files.length;
  }
  if (!validOutputContract) {
    throw new ApiError("Agent 输出格式不在允许范围内。", {
      code: "INVALID_INPUT",
    });
  }
  if (inputFiles.length > 0 && options?.confirmFileAccess !== true) {
    throw new ApiError("发送受控文件前必须单独确认 Provider 访问。", {
      code: "AGENT_FILE_ACCESS_CONFIRMATION_REQUIRED",
    });
  }
  const providerConfirmation = options?.fileAccessProviderConfirmation;
  const validProviderConfirmation =
    isRecord(providerConfirmation) &&
    hasExactKeys(providerConfirmation, ["version", "configVersion", "kind"]) &&
    Number.isSafeInteger(providerConfirmation.version) &&
    providerConfirmation.version > 0 &&
    Number.isSafeInteger(providerConfirmation.configVersion) &&
    providerConfirmation.configVersion > 0 &&
    (providerConfirmation.kind === "local" ||
      providerConfirmation.kind === "remote");
  if (inputFiles.length > 0 && providerConfirmation === undefined) {
    throw new ApiError(
      "发送受控文件前必须确认当前 Provider 的版本与本地/在线类型。",
      { code: "AGENT_FILE_ACCESS_PROVIDER_CONFIRMATION_REQUIRED" },
    );
  }
  if (inputFiles.length > 0 && !validProviderConfirmation) {
    throw new ApiError("Provider 文件访问确认身份无效。", {
      code: "INVALID_INPUT",
    });
  }
  if (
    inputFiles.length === 0 &&
    (options?.confirmFileAccess === true || providerConfirmation !== undefined)
  ) {
    throw new ApiError("没有选择受控文件时不能提交文件访问确认。", {
      code: "INVALID_INPUT",
    });
  }
  const requestBody: JsonRecord = { provider_id: providerId };
  if (hasProjectFiles) requestBody.confirm_project_task_files = true;
  if (restart) {
    requestBody.restart = {
      run_id: restart.runId,
      expected_task_version: restart.expectedTaskVersion,
    };
    requestBody.confirm_restart = true;
    requestBody.restart_preview_hash = options!.restartPreviewHash;
  }
  if (rework && reworkConfirmation) {
    requestBody.rework = {
      submission_id: rework.submissionId,
      artifact_ids: rework.artifactIds,
      expected_task_version: rework.expectedTaskVersion,
    };
    requestBody.confirm_rework_context = true;
    requestBody.rework_provider_confirmation = {
      version: reworkConfirmation.version,
      config_version: reworkConfirmation.configVersion,
      kind: reworkConfirmation.kind,
    };
  }
  if (autoAssign !== undefined) {
    requestBody.auto_assign = {
      actor_id: autoAssign.actorId,
      expected_task_version: autoAssign.expectedTaskVersion,
    };
  }
  if (
    inputFiles.length > 0 ||
    outputContract.type === "file" ||
    outputContract.type === "files"
  ) {
    if (inputFiles.length > 0) {
      requestBody.input_files = inputFiles.map(agentFileReferenceWire);
      requestBody.confirm_file_access = true;
      const confirmed =
        providerConfirmation as AgentRunFileAccessProviderConfirmation;
      requestBody.file_access_provider_confirmation = {
        version: confirmed.version,
        config_version: confirmed.configVersion,
        kind: confirmed.kind,
      };
    }
    requestBody.output_contract = outputContract;
  }
  const payload = await apiRequest<unknown>(
    `/api/v1/tasks/${encodeURIComponent(taskId)}/agent-runs`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(requestBody),
    },
  );
  const body =
    isRecord(payload) && isRecord(payload.data) ? payload.data : null;
  if (!body) return invalidResponse("Agent 执行创建响应格式无效");
  const created = agentRunFromRecord(body);
  if (
    hasProjectFiles &&
    (created.executionContractVersion !== 6 ||
      created.taskId !== taskId ||
      created.providerId !== providerId)
  )
    return invalidResponse("跨任务文件执行回执与已确认目标不一致");
  if (
    restart &&
    (created.taskId !== taskId ||
      created.providerId !== providerId ||
      created.restartOfRunId !== restart.runId ||
      created.id === restart.runId ||
      created.parentRunId !== null)
  )
    return invalidResponse("新执行回执与已确认来源不一致");
  return created;
}

export async function cancelAgentRun(runId: string): Promise<void> {
  await apiRequest<unknown>(
    `/api/v1/agent-runs/${encodeURIComponent(runId)}/cancel`,
    { method: "POST" },
  );
}

export async function retryAgentRun(
  runId: string,
  options?: RetryAgentRunReworkOptions,
): Promise<AgentRun> {
  const validConfirmation = (
    value: unknown,
  ): value is AgentRunFileAccessProviderConfirmation =>
    isRecord(value) &&
    hasExactKeys(value, ["version", "configVersion", "kind"]) &&
    Number.isSafeInteger(value.version) &&
    Number(value.version) > 0 &&
    Number.isSafeInteger(value.configVersion) &&
    Number(value.configVersion) > 0 &&
    ["local", "remote"].includes(String(value.kind));
  if (
    options !== undefined &&
    (!isRecord(options) ||
      !Object.keys(options).every((key) =>
        [
          "confirmReworkContext",
          "reworkProviderConfirmation",
          "confirmFileAccess",
          "fileAccessProviderConfirmation",
          "confirmProjectTaskFiles",
        ].includes(key),
      ) ||
      (options.confirmReworkContext === undefined &&
        options.confirmFileAccess === undefined) ||
      (options.confirmProjectTaskFiles !== undefined &&
        (options.confirmProjectTaskFiles !== true ||
          options.confirmFileAccess !== true)) ||
      (options.confirmReworkContext !== undefined &&
        options.confirmReworkContext !== true) ||
      (options.confirmReworkContext === true) !==
        (options.reworkProviderConfirmation !== undefined) ||
      (options.reworkProviderConfirmation !== undefined &&
        !validConfirmation(options.reworkProviderConfirmation)) ||
      (options.confirmFileAccess !== undefined &&
        options.confirmFileAccess !== true) ||
      (options.confirmFileAccess === true) !==
        (options.fileAccessProviderConfirmation !== undefined) ||
      (options.fileAccessProviderConfirmation !== undefined &&
        !validConfirmation(options.fileAccessProviderConfirmation)))
  )
    throw new ApiError("返工重试需要独立核对上下文与 Provider。", {
      code: "INVALID_INPUT",
    });
  const wire = (value: unknown) => {
    if (!validConfirmation(value))
      throw new ApiError("Provider 确认信息无效", { code: "INVALID_INPUT" });
    return {
      version: value.version,
      config_version: value.configVersion,
      kind: value.kind,
    };
  };
  const payload = await apiRequest<unknown>(
    `/api/v1/agent-runs/${encodeURIComponent(runId)}/retry`,
    {
      method: "POST",
      ...(options
        ? {
            body: JSON.stringify({
              ...(options.confirmProjectTaskFiles
                ? { confirm_project_task_files: true }
                : {}),
              ...(options.confirmReworkContext &&
              options.reworkProviderConfirmation
                ? {
                    confirm_rework_context: true,
                    rework_provider_confirmation: wire(
                      options.reworkProviderConfirmation,
                    ),
                  }
                : {}),
              ...(options.confirmFileAccess &&
              options.fileAccessProviderConfirmation
                ? {
                    confirm_file_access: true,
                    file_access_provider_confirmation: wire(
                      options.fileAccessProviderConfirmation,
                    ),
                  }
                : {}),
            }),
          }
        : {}),
    },
  );
  const body =
    isRecord(payload) && isRecord(payload.data) ? payload.data : null;
  if (!body) return invalidResponse("Agent 执行重试响应格式无效");
  const retried = agentRunFromRecord(body);
  if (
    options?.confirmProjectTaskFiles &&
    (retried.executionContractVersion !== 6 ||
      retried.parentRunId !== runId ||
      retried.id === runId)
  )
    return invalidResponse("跨任务文件重试回执与原执行不一致");
  return retried;
}

export async function retryAgentRunOutputDelivery(
  runId: string,
): Promise<AgentRun> {
  const payload = await apiRequest<unknown>(
    `/api/v1/agent-runs/${encodeURIComponent(runId)}/output-delivery/retry`,
    { method: "POST" },
  );
  const body =
    isRecord(payload) && isRecord(payload.data) ? payload.data : null;
  if (!body) return invalidResponse("Agent 产出登记重试响应格式无效");
  const run = agentRunFromRecord(body);
  if (run.id !== runId) {
    return invalidResponse("Agent 产出登记重试身份不匹配");
  }
  return run;
}
