import type {
  AgentProjectFileSource,
  AgentRunFileCandidate,
  AgentRunFileReference,
} from "../types/models";
import { isWorkspaceIdentity } from "./focusReportLocation";

export const projectTaskFileKind = "project_task_artifact";
export const projectFileObject = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === "object" && !Array.isArray(v);
export const projectFileKeys = (v: Record<string, unknown>, keys: string[]) =>
  Object.keys(v).length === keys.length &&
  keys.every((key) => Object.hasOwn(v, key));
const positive = (v: unknown) => Number.isSafeInteger(v) && Number(v) > 0;
const uuid = (v: unknown) => typeof v === "string" && isWorkspaceIdentity(v);
export function validAgentProjectFileSource(
  v: unknown,
): v is AgentProjectFileSource {
  return (
    projectFileObject(v) &&
    projectFileKeys(v, [
      "project_id",
      "task_id",
      "task_title",
      "task_version",
      "submission_id",
      "submission_sequence",
    ]) &&
    uuid(v.project_id) &&
    uuid(v.task_id) &&
    uuid(v.submission_id) &&
    typeof v.task_title === "string" &&
    v.task_title.trim() === v.task_title &&
    [...v.task_title].length >= 2 &&
    [...v.task_title].length <= 200 &&
    !v.task_title.includes("\0") &&
    new TextDecoder().decode(new TextEncoder().encode(v.task_title)) ===
      v.task_title &&
    positive(v.task_version) &&
    positive(v.submission_sequence)
  );
}
export function sameProjectFileSource(a: unknown, b: unknown) {
  return (
    validAgentProjectFileSource(a) &&
    validAgentProjectFileSource(b) &&
    Object.keys(a).every(
      (key) =>
        a[key as keyof AgentProjectFileSource] ===
        b[key as keyof AgentProjectFileSource],
    )
  );
}
export function validAgentFileReference(
  v: unknown,
): v is AgentRunFileReference {
  return (
    projectFileObject(v) &&
    uuid(v.id) &&
    (v.sourceKind === projectTaskFileKind
      ? projectFileKeys(v, ["sourceKind", "id", "sourceTask", "sha256"]) &&
        validAgentProjectFileSource(v.sourceTask) &&
        typeof v.sha256 === "string" &&
        /^[a-f0-9]{64}$/.test(v.sha256)
      : ["task_artifact", "project_attachment"].includes(
          String(v.sourceKind),
        ) && projectFileKeys(v, ["sourceKind", "id"]))
  );
}
export function agentFileReference(
  file: AgentRunFileCandidate,
): AgentRunFileReference {
  return {
    sourceKind: file.sourceKind,
    id: file.id,
    ...(file.sourceKind === projectTaskFileKind
      ? { sourceTask: file.sourceTask, sha256: file.sha256 }
      : {}),
  };
}
export function agentFileReferenceWire(file: AgentRunFileReference) {
  return {
    source_kind: file.sourceKind,
    id: file.id,
    ...(file.sourceKind === projectTaskFileKind
      ? { source_task: file.sourceTask, sha256: file.sha256 }
      : {}),
  };
}
export function validFileSourceProof(
  file: Record<string, unknown>,
  allowProject: boolean,
  targetTaskId?: string,
  projectId?: string | null,
) {
  if (file.source_kind !== projectTaskFileKind)
    return !Object.hasOwn(file, "source_task");
  return (
    allowProject &&
    validAgentProjectFileSource(file.source_task) &&
    (!targetTaskId || file.source_task.task_id !== targetTaskId) &&
    (projectId === undefined || file.source_task.project_id === projectId)
  );
}
