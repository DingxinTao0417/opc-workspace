import { validFileSourceProof } from "../../lib/agentProjectFiles";
import type {
  AgentRunInputSlotStatus,
  AgentRunReworkInputFile,
  AgentRunStartGateCloseReason,
  AgentRunStartGateInputs,
  ConfirmAgentRunStartGateInputs,
} from "../../types/models";
import {
  agentRunInputSlotStatuses,
  agentRunStartGateCloseReasons,
  expectedFilesFromRecord,
} from "./agentRuns";
import {
  apiRequest,
  hasExactKeys,
  invalidResponse,
  isRecord,
  validCanonicalUUID,
} from "./core";

// ADR-033: what a v7 Run's input slot holds, and the second human
// confirmation that releases the Run with exactly those files.

function agentRunStartGateInputsFromRecord(
  value: unknown,
  runId: string,
): AgentRunStartGateInputs {
  const invalid = () => invalidResponse("前置产出输入响应格式无效");
  if (!isRecord(value)) return invalid();
  const status = value.status;
  const closed = status === "closed";
  const hasFingerprint = Object.hasOwn(value, "fingerprint");
  const predecessor = value.predecessor;
  const provider = value.provider;
  const limits = value.limits;
  const files = value.files;
  if (
    !hasExactKeys(value, [
      "run_id",
      "task_id",
      "status",
      ...(Object.hasOwn(value, "close_reason") ? ["close_reason"] : []),
      "expires_at",
      "predecessor",
      "expected_files",
      "provider",
      "files",
      "total_bytes",
      "limits",
      ...(hasFingerprint ? ["fingerprint"] : []),
    ]) ||
    value.run_id !== runId ||
    typeof value.task_id !== "string" ||
    !validCanonicalUUID(value.task_id) ||
    !agentRunInputSlotStatuses.includes(status as never) ||
    closed !== Object.hasOwn(value, "close_reason") ||
    (closed &&
      !agentRunStartGateCloseReasons.includes(value.close_reason as never)) ||
    typeof value.expires_at !== "string" ||
    !Number.isFinite(Date.parse(value.expires_at)) ||
    !isRecord(predecessor) ||
    typeof predecessor.run_id !== "string" ||
    !validCanonicalUUID(predecessor.run_id) ||
    !isRecord(provider) ||
    !hasExactKeys(provider, [
      "id",
      "name",
      "kind",
      "model",
      "version",
      "config_version",
      "leaves_device",
    ]) ||
    typeof provider.id !== "string" ||
    !validCanonicalUUID(provider.id) ||
    typeof provider.name !== "string" ||
    !provider.name ||
    (provider.kind !== "local" && provider.kind !== "remote") ||
    typeof provider.model !== "string" ||
    !provider.model ||
    !Number.isSafeInteger(provider.version) ||
    Number(provider.version) < 1 ||
    !Number.isSafeInteger(provider.config_version) ||
    Number(provider.config_version) < 1 ||
    provider.leaves_device !== (provider.kind === "remote") ||
    !isRecord(limits) ||
    !hasExactKeys(limits, ["max_files", "max_file_bytes", "max_total_bytes"]) ||
    limits.max_files !== 4 ||
    limits.max_file_bytes !== 65_536 ||
    limits.max_total_bytes !== 131_072 ||
    !Array.isArray(files) ||
    !Number.isSafeInteger(value.total_bytes)
  )
    return invalid();
  // The predecessor's Task details are there unless the Run is already gone.
  const predecessorKeys = Object.keys(predecessor);
  const fullPredecessor = predecessorKeys.length === 4;
  if (
    (!fullPredecessor && predecessorKeys.length !== 1) ||
    (fullPredecessor &&
      (!hasExactKeys(predecessor, [
        "run_id",
        "task_id",
        "task_title",
        "attempt",
      ]) ||
        typeof predecessor.task_id !== "string" ||
        !validCanonicalUUID(predecessor.task_id) ||
        predecessor.task_id === value.task_id ||
        typeof predecessor.task_title !== "string" ||
        !predecessor.task_title.trim() ||
        !Number.isSafeInteger(predecessor.attempt) ||
        Number(predecessor.attempt) < 1))
  )
    return invalid();
  const expectedFiles = expectedFilesFromRecord(value.expected_files);
  // Files and a fingerprint are shown exactly when there is something to
  // confirm or something was confirmed, and they are what was announced.
  const showsFiles = status === "awaiting_confirmation" || status === "bound";
  if (
    showsFiles !== hasFingerprint ||
    (hasFingerprint &&
      (typeof value.fingerprint !== "string" ||
        !/^[a-f0-9]{64}$/.test(value.fingerprint))) ||
    files.length !== (showsFiles ? expectedFiles.length : 0) ||
    !files.every(
      (file, index) =>
        isRecord(file) &&
        hasExactKeys(file, [
          "id",
          "source_kind",
          "name",
          "mime",
          "size_bytes",
          "sha256",
          "source_task",
        ]) &&
        file.source_kind === "project_task_artifact" &&
        validFileSourceProof(file, true, value.task_id as string) &&
        validCanonicalUUID(file.id) &&
        file.name === expectedFiles[index].name &&
        file.mime === expectedFiles[index].mime &&
        Number.isSafeInteger(file.size_bytes) &&
        Number(file.size_bytes) > 0 &&
        Number(file.size_bytes) <= 65_536 &&
        typeof file.sha256 === "string" &&
        /^[a-f0-9]{64}$/.test(file.sha256),
    ) ||
    new Set(files.map((file) => file.id)).size !== files.length ||
    value.total_bytes !==
      files.reduce((sum, file) => sum + Number(file.size_bytes), 0) ||
    Number(value.total_bytes) > 131_072
  )
    return invalid();
  return {
    runId,
    taskId: value.task_id,
    status: status as AgentRunInputSlotStatus,
    ...(closed
      ? { closeReason: value.close_reason as AgentRunStartGateCloseReason }
      : {}),
    expiresAt: value.expires_at,
    predecessor: {
      runId: predecessor.run_id,
      ...(fullPredecessor
        ? {
            taskId: predecessor.task_id as string,
            taskTitle: predecessor.task_title as string,
            attempt: Number(predecessor.attempt),
          }
        : {}),
    },
    expectedFiles,
    provider: {
      id: provider.id,
      name: provider.name,
      kind: provider.kind,
      model: provider.model,
      version: Number(provider.version),
      configVersion: Number(provider.config_version),
      leavesDevice: provider.leaves_device === true,
    },
    files: files as AgentRunReworkInputFile[],
    totalBytes: Number(value.total_bytes),
    limits: { maxFiles: 4, maxFileBytes: 65_536, maxTotalBytes: 131_072 },
    ...(hasFingerprint ? { fingerprint: value.fingerprint as string } : {}),
  };
}

/** Reads what the Run's input slot holds; no file content is returned. */
export async function getAgentRunStartGateInputs(
  runId: string,
  signal?: AbortSignal,
): Promise<AgentRunStartGateInputs> {
  const payload = await apiRequest<unknown>(
    `/api/v1/agent-runs/${encodeURIComponent(runId)}/start-gate/inputs`,
    { signal },
  );
  if (!isRecord(payload)) return invalidResponse("前置产出输入响应格式无效");
  return agentRunStartGateInputsFromRecord(payload.data, runId);
}

/**
 * The second human confirmation: send exactly the files that were shown to
 * exactly the Provider that was shown. The Run starts once this succeeds.
 */
export async function confirmAgentRunStartGateInputs(
  input: ConfirmAgentRunStartGateInputs,
): Promise<AgentRunStartGateInputs> {
  const payload = await apiRequest<unknown>(
    `/api/v1/agent-runs/${encodeURIComponent(input.runId)}/start-gate/inputs/confirm`,
    {
      method: "POST",
      body: JSON.stringify({
        confirm_file_access: true,
        confirm_project_task_files: true,
        file_access_provider_confirmation: {
          version: input.provider.version,
          config_version: input.provider.configVersion,
          kind: input.provider.kind,
        },
        fingerprint: input.fingerprint,
      }),
    },
  );
  if (!isRecord(payload)) return invalidResponse("前置产出输入响应格式无效");
  const result = agentRunStartGateInputsFromRecord(payload.data, input.runId);
  if (result.status !== "bound" || result.fingerprint !== input.fingerprint)
    return invalidResponse("前置产出输入确认结果与所确认的内容不一致");
  return result;
}
