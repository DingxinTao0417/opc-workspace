import { ApiError, apiRequest, agentRunFileOutputPresets } from "./client";
import {
  parseNativeAgentRunStartPreview,
  type AiAgentRunStartPreview,
} from "./aiWorkspaceActions";
import type {
  AgentRunRestartSource,
  CreateAgentRunOptions,
} from "../types/models";
import {
  validAgentRunRestartRequest,
  validAgentRunRestartSource,
  sameAgentRestartFacts,
} from "../lib/agentRunRestart";
import {
  validAgentRunReworkRequest,
  reworkEncodedBytes,
} from "../lib/agentRunRework";
import { isWorkspaceIdentity } from "../lib/focusReportLocation";
import {
  agentFileReferenceWire,
  validAgentFileReference,
  sameProjectFileSource,
} from "../lib/agentProjectFiles";

export type AgentRunRestartOptions = Pick<
  CreateAgentRunOptions,
  "inputFiles" | "outputContract" | "rework"
> & { restart: NonNullable<CreateAgentRunOptions["restart"]> };
export interface AgentRunRestartPreview {
  task_id: string;
  task_version: number;
  source_run: AgentRunRestartSource;
  current_start: AiAgentRunStartPreview;
  fingerprint: string;
}
const object = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === "object" && !Array.isArray(v);
const keys = (v: Record<string, unknown>, names: string[]) =>
  Object.keys(v).length === names.length &&
  names.every((n) => Object.hasOwn(v, n));
function invalid(
  message = "当前事实预览不完整，请重新读取。",
  code = "INVALID_RESPONSE",
): never {
  throw new ApiError(message, { code });
}

export async function getAgentRunRestartPreview(
  taskId: string,
  providerId: string,
  options: AgentRunRestartOptions,
  signal?: AbortSignal,
): Promise<AgentRunRestartPreview> {
  if (
    !isWorkspaceIdentity(taskId) ||
    !isWorkspaceIdentity(providerId) ||
    !object(options) ||
    !keys(options, [
      "restart",
      ...["inputFiles", "outputContract", "rework"].filter((key) =>
        Object.hasOwn(options, key),
      ),
    ]) ||
    !validAgentRunRestartRequest(options.restart)
  )
    return invalid("新执行来源参数无效。", "INVALID_INPUT");
  const files = options.inputFiles ?? [];
  if (
    (Object.hasOwn(options, "inputFiles") &&
      !Array.isArray(options.inputFiles)) ||
    files.length > 4 ||
    files.some((f) => !validAgentFileReference(f)) ||
    new Set(files.map((f) => f.id)).size !== files.length
  )
    return invalid("请重新选择受控输入文件。", "INVALID_INPUT");
  const output = options.outputContract ?? { type: "text" };
  const preset = (f: unknown) =>
    object(f) &&
    keys(f, ["name", "mime"]) &&
    agentRunFileOutputPresets.some(
      (p) => p.name === f.name && p.mime === f.mime,
    );
  if (
    !object(output) ||
    !(output.type === "text"
      ? keys(output, ["type"])
      : output.type === "file"
        ? keys(output, ["type", "name", "mime"]) &&
          preset({ name: output.name, mime: output.mime })
        : output.type === "files" &&
          keys(output, ["type", "files"]) &&
          Array.isArray(output.files) &&
          output.files.length >= 2 &&
          output.files.length <= 4 &&
          output.files.every(preset) &&
          new Set(output.files.map((f) => f.name)).size ===
            output.files.length) ||
    (Object.hasOwn(options, "outputContract") && options.outputContract == null)
  )
    return invalid("输出契约无效。", "INVALID_INPUT");
  const rework = options.rework;
  if (
    Object.hasOwn(options, "rework") &&
    (!validAgentRunReworkRequest(rework) ||
      rework.expectedTaskVersion !== options.restart.expectedTaskVersion)
  )
    return invalid("返工来源或版本无效。", "INVALID_INPUT");
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/tasks/${taskId}/agent-runs/restart-preview`,
    {
      method: "POST",
      signal,
      body: JSON.stringify({
        provider_id: providerId,
        restart: {
          run_id: options.restart.runId,
          expected_task_version: options.restart.expectedTaskVersion,
        },
        ...(files.length
          ? {
              input_files: files.map(agentFileReferenceWire),
            }
          : {}),
        ...(output.type !== "text" || files.length > 0
          ? { output_contract: output }
          : {}),
        ...(rework
          ? {
              rework: {
                submission_id: rework.submissionId,
                artifact_ids: rework.artifactIds,
                expected_task_version: rework.expectedTaskVersion,
              },
            }
          : {}),
      }),
    },
  );
  const value = response.data;
  if (
    !object(value) ||
    !keys(value, [
      "task_id",
      "task_version",
      "source_run",
      "current_start",
      "fingerprint",
    ]) ||
    value.task_id !== taskId ||
    value.task_version !== options.restart.expectedTaskVersion ||
    !validAgentRunRestartSource(value.source_run) ||
    value.source_run.run_id !== options.restart.runId ||
    value.source_run.task_id !== taskId ||
    typeof value.fingerprint !== "string" ||
    !/^[a-f0-9]{64}$/.test(value.fingerprint) ||
    reworkEncodedBytes(value) > 131072
  )
    return invalid();
  const current = parseNativeAgentRunStartPreview(value.current_start);
  if (
    current.task.id !== taskId ||
    current.task.version !== value.task_version ||
    current.provider.id !== providerId ||
    !sameAgentRestartFacts(current.restart, value.source_run) ||
    !sameAgentRestartFacts(
      current.output_contract ?? { type: "text" },
      output,
    ) ||
    (current.input_files?.length ?? 0) !== files.length ||
    files.some(
      (file, i) =>
        current.input_files?.[i].id !== file.id ||
        current.input_files?.[i].source_kind !== file.sourceKind ||
        (file.sourceKind === "project_task_artifact" &&
          (current.input_files?.[i].sha256 !== file.sha256 ||
            !sameProjectFileSource(
              current.input_files?.[i].source_task,
              file.sourceTask,
            ))),
    ) ||
    (rework
      ? current.rework_context?.submission_id !== rework.submissionId ||
        JSON.stringify(current.rework_context?.artifacts.map((a) => a.id)) !==
          JSON.stringify(rework.artifactIds)
      : current.rework_context !== undefined)
  )
    return invalid();
  return {
    ...value,
    current_start: current,
  } as unknown as AgentRunRestartPreview;
}
