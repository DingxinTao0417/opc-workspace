import {
  ApiError,
  apiRequest,
  agentRunFileCandidateFromRecord,
  validAgentRunFileNameAndMIME,
  agentRunFileLimits,
} from "./client";
import type {
  AgentRunFileCandidate,
  AgentRunFileReference,
  AgentRunReworkInputFile,
} from "../types/models";
import {
  agentFileReferenceWire,
  projectFileKeys as keys,
  projectFileObject as object,
  sameProjectFileSource,
  validAgentFileReference,
  validFileSourceProof,
} from "../lib/agentProjectFiles";
import { isWorkspaceIdentity } from "../lib/focusReportLocation";

export interface AgentProjectFilePage {
  items: AgentRunFileCandidate[];
  offset: number;
  total: number;
  nextOffset: number | null;
}
const invalid = (code = "INVALID_RESPONSE"): never => {
  throw new ApiError(
    "跨任务文件来源不完整或已经变化，请重新选择并核对已验收批次。",
    { code },
  );
};
export async function getAgentProjectFiles(
  taskId: string,
  offset = 0,
  query = "",
  signal?: AbortSignal,
): Promise<AgentProjectFilePage> {
  if (
    !isWorkspaceIdentity(taskId) ||
    !Number.isSafeInteger(offset) ||
    offset < 0 ||
    typeof query !== "string" ||
    [...query].length > 200 ||
    query.includes("\0") ||
    new TextDecoder().decode(new TextEncoder().encode(query)) !== query
  )
    return invalid("INVALID_INPUT");
  const response = await apiRequest<unknown>(
    `/api/v1/tasks/${taskId}/agent-runs/project-task-files?${new URLSearchParams({ offset: String(offset), query })}`,
    { signal },
  );
  if (
    !object(response) ||
    !keys(response, ["data", "meta"]) ||
    !Array.isArray(response.data) ||
    response.data.length > 20 ||
    !object(response.meta) ||
    !keys(response.meta, ["offset", "limit", "total", "next_offset"])
  )
    return invalid();
  const meta = response.meta;
  if (
    meta.offset !== offset ||
    meta.limit !== 20 ||
    !Number.isSafeInteger(meta.total) ||
    Number(meta.total) < 0 ||
    (meta.next_offset !== null && meta.next_offset !== offset + 20) ||
    response.data.length !==
      Math.max(0, Math.min(20, Number(meta.total) - offset)) ||
    meta.next_offset !==
      (offset + response.data.length < Number(meta.total) ? offset + 20 : null)
  )
    return invalid();
  const items = response.data.map((file) =>
    agentRunFileCandidateFromRecord(file, agentRunFileLimits, true),
  );
  if (
    items.some(
      (file) =>
        file.sourceKind !== "project_task_artifact" ||
        file.sourceTask?.task_id === taskId,
    ) ||
    new Set(items.map((f) => f.id)).size !== items.length
  )
    return invalid();
  return {
    items,
    offset,
    total: Number(meta.total),
    nextOffset: meta.next_offset as number | null,
  };
}
export async function previewAgentProjectFiles(
  taskId: string,
  files: AgentRunFileReference[],
  signal?: AbortSignal,
): Promise<AgentRunReworkInputFile[]> {
  if (
    !isWorkspaceIdentity(taskId) ||
    !Array.isArray(files) ||
    files.length < 1 ||
    files.length > 4 ||
    files.some(
      (file) =>
        !validAgentFileReference(file) ||
        file.sourceKind !== "project_task_artifact" ||
        file.sourceTask?.task_id === taskId,
    ) ||
    new Set(files.map((f) => f.id)).size !== files.length
  )
    return invalid("INVALID_INPUT");
  const response = await apiRequest<unknown>(
    `/api/v1/tasks/${taskId}/agent-runs/project-task-files/preview`,
    {
      method: "POST",
      signal,
      body: JSON.stringify({ input_files: files.map(agentFileReferenceWire) }),
    },
  );
  if (
    !object(response) ||
    !keys(response, ["data"]) ||
    !Array.isArray(response.data) ||
    response.data.length !== files.length
  )
    return invalid();
  const data = response.data;
  if (
    data.some(
      (f, i) =>
        !object(f) ||
        !keys(f, [
          "id",
          "source_kind",
          "name",
          "mime",
          "size_bytes",
          "sha256",
          "source_task",
        ]) ||
        f.source_kind !== "project_task_artifact" ||
        f.id !== files[i].id ||
        !validFileSourceProof(f, true, taskId) ||
        !sameProjectFileSource(f.source_task, files[i].sourceTask) ||
        f.sha256 !== files[i].sha256 ||
        !validAgentRunFileNameAndMIME(f.name, f.mime) ||
        !Number.isSafeInteger(f.size_bytes) ||
        Number(f.size_bytes) < 1 ||
        Number(f.size_bytes) > 65536,
    ) ||
    data.reduce((sum, f) => sum + Number(f.size_bytes), 0) > 131072
  )
    return invalid();
  return data as AgentRunReworkInputFile[];
}
export async function recheckAgentProjectFiles(
  taskId: string,
  selected: AgentRunFileCandidate[],
  signal?: AbortSignal,
) {
  const files = selected.filter(
    (file) => file.sourceKind === "project_task_artifact",
  );
  if (!files.length) return;
  const fresh = await previewAgentProjectFiles(
    taskId,
    files.map((file) => ({
      sourceKind: file.sourceKind,
      id: file.id,
      sha256: file.sha256,
      sourceTask: file.sourceTask,
    })),
    signal,
  );
  if (
    fresh.some(
      (file, i) =>
        file.name !== files[i].name ||
        file.mime !== files[i].mime ||
        file.size_bytes !== files[i].sizeBytes,
    )
  )
    return invalid();
}
