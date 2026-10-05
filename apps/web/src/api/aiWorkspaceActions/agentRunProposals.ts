import { validFileSourceProof } from "../../lib/agentProjectFiles";
import { validAgentRunDeliveryState } from "../../lib/agentRunDelivery";
import { validAgentRunRestartSource } from "../../lib/agentRunRestart";
import { validAgentRunReworkContext } from "../../lib/agentRunRework";
import { agentRunHref } from "../../lib/aiWorkspaceLinks";
import { taskSubmissionHref } from "../../lib/taskSubmissionLocation";
import type {
  AgentRunOutputDeliveryStatus,
  AgentRunStatus,
} from "../../types/models";
import { agentRunStartGateFromRecord } from "../client";
import {
  canonicalUuid,
  exactKeys,
  invalid,
  nonemptyString,
  object,
  positiveIntegerValue,
  sha256,
} from "./primitives";
import type {
  AiAgentRunInputFilePreview,
  AiAgentRunOutputContract,
  AiAgentRunStartPreview,
} from "./types";

const utcTimestamp = (value: unknown) =>
  typeof value === "string" &&
  /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/.test(value) &&
  Number.isFinite(Date.parse(value));

const agentTextFileExtensions: Record<string, string[]> = {
  "text/plain": [".txt", ".log"],
  "text/markdown": [".md", ".markdown"],
  "text/csv": [".csv"],
  "text/html": [".html", ".htm"],
  "text/css": [".css"],
  "text/javascript": [".js", ".mjs", ".cjs", ".jsx"],
  "application/javascript": [".js", ".mjs", ".cjs", ".jsx"],
  "text/typescript": [".ts", ".tsx"],
  "application/json": [".json"],
  "application/xml": [".xml"],
  "text/xml": [".xml"],
  "application/yaml": [".yaml", ".yml"],
  "text/yaml": [".yaml", ".yml"],
  "text/x-go": [".go"],
  "text/x-python": [".py"],
  "text/x-shellscript": [".sh"],
  "text/x-sql": [".sql"],
};

function validAgentTextFileNameAndMime(name: unknown, mime: unknown) {
  if (
    typeof name !== "string" ||
    typeof mime !== "string" ||
    name.length === 0 ||
    name !== name.trim() ||
    name === "." ||
    name === ".." ||
    // eslint-disable-next-line no-control-regex -- rejects control characters on purpose
    /[\\/<>:"|?*\u0000-\u001f\u007f]/.test(name) ||
    new TextEncoder().encode(name).byteLength > 255
  )
    return false;
  const extensions = agentTextFileExtensions[mime];
  const lowerName = name.toLowerCase();
  return !!extensions?.some((extension) => lowerName.endsWith(extension));
}

function validAgentRunInputFile(
  value: unknown,
): value is AiAgentRunInputFilePreview {
  return (
    object(value) &&
    exactKeys(value, [
      "id",
      "source_kind",
      "name",
      "mime",
      "size_bytes",
      "sha256",
      ...(value.source_kind === "project_task_artifact" ? ["source_task"] : []),
    ]) &&
    typeof value.id === "string" &&
    canonicalUuid.test(value.id) &&
    ["task_artifact", "project_attachment", "project_task_artifact"].includes(
      String(value.source_kind),
    ) &&
    validFileSourceProof(value, true) &&
    validAgentTextFileNameAndMime(value.name, value.mime) &&
    positiveIntegerValue(value.size_bytes) &&
    Number(value.size_bytes) <= 65_536 &&
    typeof value.sha256 === "string" &&
    sha256.test(value.sha256)
  );
}

function validAgentRunOutputContract(
  value: unknown,
): value is AiAgentRunOutputContract {
  if (!object(value) || typeof value.type !== "string") return false;
  if (value.type === "text") return exactKeys(value, ["type"]);
  if (value.type === "file") {
    return (
      exactKeys(value, ["type", "name", "mime"]) &&
      validAgentTextFileNameAndMime(value.name, value.mime)
    );
  }
  if (value.type !== "files" || !exactKeys(value, ["type", "files"])) {
    return false;
  }
  return (
    Array.isArray(value.files) &&
    value.files.length >= 2 &&
    value.files.length <= 4 &&
    value.files.every(
      (file) =>
        object(file) &&
        exactKeys(file, ["name", "mime"]) &&
        validAgentTextFileNameAndMime(file.name, file.mime),
    ) &&
    new Set(value.files.map((file) => String(file.name).toLowerCase())).size ===
      value.files.length
  );
}

/** The files a v7 start announces: the predecessor's own output contract. */
function validInputSlotPreview(value: unknown): boolean {
  return (
    object(value) &&
    exactKeys(value, [
      "source",
      "expected_files",
      "max_files",
      "max_file_bytes",
      "max_total_bytes",
    ]) &&
    value.source === "predecessor_files" &&
    value.max_files === 4 &&
    value.max_file_bytes === 65_536 &&
    value.max_total_bytes === 131_072 &&
    Array.isArray(value.expected_files) &&
    value.expected_files.length >= 1 &&
    value.expected_files.length <= 4 &&
    value.expected_files.every(
      (file) =>
        object(file) &&
        exactKeys(file, ["name", "mime"]) &&
        validAgentTextFileNameAndMime(file.name, file.mime),
    ) &&
    new Set(value.expected_files.map((file) => String(file.name).toLowerCase()))
      .size === value.expected_files.length
  );
}

export function validAgentRunStartProposal(
  value: Record<string, unknown>,
  nativePreview = false,
): boolean {
  const action = value.action;
  const preview = value.preview;
  if (!object(action) || !object(preview)) return false;
  const changes = action.changes;
  const snapshot = preview.agent_run_start;
  if (!object(changes) || !object(snapshot)) return false;
  const task = snapshot.task;
  const assignment = snapshot.assignment;
  const agent = snapshot.agent;
  const adapter = snapshot.adapter;
  const provider = snapshot.provider;
  const limits = snapshot.runtime_limits;
  if (
    !object(task) ||
    !object(assignment) ||
    !object(agent) ||
    !object(adapter) ||
    !object(provider) ||
    !object(limits)
  )
    return false;
  const expectedVersion = action.expected_version;
  const retry = action.action === "agent_run.retry";
  const hasRestart = Object.hasOwn(changes, "restart_of_run_id");
  const restart = snapshot.restart;
  const hasStartGate = Object.hasOwn(changes, "start_after_run_id");
  // ADR-033: the start takes the predecessor's accepted files as inputs.
  const hasInputSlot = Object.hasOwn(changes, "start_after_input");
  const gate = snapshot.start_gate;
  if (
    hasStartGate !== Object.hasOwn(snapshot, "start_gate") ||
    (hasInputSlot &&
      (!hasStartGate ||
        changes.start_after_input !== "predecessor_files" ||
        changes.start_after !== "accepted" ||
        !object(gate) ||
        !validInputSlotPreview(gate.input_slot))) ||
    (hasStartGate &&
      (retry ||
        hasRestart ||
        !object(gate) ||
        !exactKeys(gate, [
          "predecessor_run_id",
          "predecessor_task_id",
          "predecessor_task_title",
          "predecessor_attempt",
          "require",
          "within_hours",
          ...(hasInputSlot ? ["input_slot"] : []),
        ]) ||
        gate.predecessor_run_id !== changes.start_after_run_id ||
        typeof gate.predecessor_run_id !== "string" ||
        !canonicalUuid.test(gate.predecessor_run_id) ||
        typeof gate.predecessor_task_id !== "string" ||
        !canonicalUuid.test(gate.predecessor_task_id) ||
        gate.predecessor_task_id === task.id ||
        typeof gate.predecessor_task_title !== "string" ||
        gate.predecessor_task_title.trim() === "" ||
        !positiveIntegerValue(gate.predecessor_attempt) ||
        gate.require !== changes.start_after ||
        (gate.require !== "submitted" && gate.require !== "accepted") ||
        gate.within_hours !== changes.start_within_hours ||
        !Number.isInteger(gate.within_hours) ||
        Number(gate.within_hours) < 1 ||
        Number(gate.within_hours) > 24))
  )
    return false;
  const gateChangeKeys = hasStartGate
    ? [
        "start_after_run_id",
        "start_after",
        "start_within_hours",
        ...(hasInputSlot ? ["start_after_input"] : []),
      ]
    : [];
  if (
    hasRestart !== Object.hasOwn(snapshot, "restart") ||
    (hasRestart &&
      (retry ||
        !validAgentRunRestartSource(restart) ||
        restart.run_id !== changes.restart_of_run_id ||
        restart.task_id !== task.id))
  )
    return false;
  const previous = preview.agent_run_retry;
  if (
    retry &&
    (!object(previous) ||
      !exactKeys(previous, ["run_id", "status", "attempt"]) ||
      typeof previous.run_id !== "string" ||
      !canonicalUuid.test(previous.run_id) ||
      previous.run_id !== action.agent_run_id ||
      !["failed", "cancelled", "interrupted"].includes(
        String(previous.status),
      ) ||
      !positiveIntegerValue(previous.attempt) ||
      !positiveIntegerValue(snapshot.attempt) ||
      Number(previous.attempt) >= Number(snapshot.attempt))
  )
    return false;
  const providerId = retry ? provider.id : changes.provider_id;
  const expectedProviderVersion = retry
    ? provider.version
    : changes.expected_provider_version;
  const expectedProviderConfigVersion = retry
    ? provider.config_version
    : changes.expected_provider_config_version;
  const executionContractVersion = snapshot.execution_contract_version;
  const v4 = executionContractVersion === 4;
  const v5 = executionContractVersion === 5;
  const v6 = executionContractVersion === 6;
  // v7 freezes only the declaration of its inputs. It exists exactly when the
  // proposal asks for the predecessor's files, and carries no file of its own.
  const v7 = executionContractVersion === 7;
  if (
    v7 !== hasInputSlot ||
    (v7 &&
      (Object.hasOwn(changes, "input_file_candidate_ids") ||
        Object.hasOwn(snapshot, "input_files") ||
        task.project_id === null))
  )
    return false;
  const anthropic =
    v5 || ((v6 || v7) && provider.protocol === "anthropic_messages");
  const v2 =
    executionContractVersion === 2 ||
    executionContractVersion === 3 ||
    v4 ||
    v5 ||
    v6 ||
    v7;
  const hasRework = Object.hasOwn(snapshot, "rework_context");
  const hasReworkID = Object.hasOwn(changes, "rework_submission_id");
  const hasReworkArtifacts = Object.hasOwn(changes, "rework_artifact_ids");
  const rework = snapshot.rework_context;
  const reworkIDs = changes.rework_artifact_ids;
  if (
    (v4 && !hasRework) ||
    (!v4 && !v5 && !v6 && hasRework) ||
    (!hasRework && (hasReworkID || hasReworkArtifacts)) ||
    (hasRework &&
      (!validAgentRunReworkContext(rework) ||
        task.review_policy !== "manual" ||
        task.status !== "in_progress" ||
        (!retry &&
          (!hasReworkID ||
            !hasReworkArtifacts ||
            changes.rework_submission_id !== rework.submission_id ||
            !Array.isArray(reworkIDs) ||
            reworkIDs.length !== rework.artifacts.length ||
            rework.artifacts.some(
              (artifact, index) => artifact.id !== reworkIDs[index],
            )))))
  )
    return false;
  const hasCandidateIDs = Object.hasOwn(changes, "input_file_candidate_ids");
  const hasOutputKind = Object.hasOwn(changes, "output_kind");
  const candidateIDs = changes.input_file_candidate_ids;
  const outputKind =
    retry && object(snapshot.output_contract)
      ? snapshot.output_contract.type
      : hasOutputKind
        ? changes.output_kind
        : "text";
  const providerHasLeavesDevice = Object.hasOwn(provider, "leaves_device");
  const hasInputFiles = Object.hasOwn(snapshot, "input_files");
  const hasInputFileTotal = Object.hasOwn(snapshot, "input_file_total_bytes");
  const inputFiles = snapshot.input_files;
  const outputContract = snapshot.output_contract;
  const fileLimits = snapshot.file_limits;
  const commonSnapshotKeys = [
    "task",
    "assignment",
    "agent",
    "adapter",
    "provider",
    "attempt",
    "execution_contract_version",
    "runtime_limits",
    "success_does_not_complete_task",
    "result_requires_manual_review",
    ...(hasRework ? ["rework_context"] : []),
    ...(hasRestart ? ["restart"] : []),
    ...(hasStartGate ? ["start_gate"] : []),
  ];
  const validCandidateIDs =
    !hasCandidateIDs ||
    (Array.isArray(candidateIDs) &&
      (v5 || candidateIDs.length > 0) &&
      candidateIDs.length <= 4 &&
      candidateIDs.every(
        (candidateID) =>
          typeof candidateID === "string" && canonicalUuid.test(candidateID),
      ) &&
      new Set(candidateIDs).size === candidateIDs.length);
  const validChanges = retry
    ? exactKeys(changes, [])
    : v2
      ? exactKeys(changes, [
          "provider_id",
          "expected_provider_version",
          "expected_provider_config_version",
          ...(hasCandidateIDs ? ["input_file_candidate_ids"] : []),
          ...(hasOutputKind ? ["output_kind"] : []),
          ...(hasRework ? ["rework_submission_id", "rework_artifact_ids"] : []),
          ...(hasRestart ? ["restart_of_run_id"] : []),
          ...gateChangeKeys,
        ]) &&
        (v4 || v5 || v6 || v7 || hasCandidateIDs || hasOutputKind) &&
        validCandidateIDs &&
        (!hasOutputKind ||
          outputKind === "text" ||
          outputKind === "file" ||
          outputKind === "files")
      : exactKeys(changes, [
          "provider_id",
          "expected_provider_version",
          "expected_provider_config_version",
          ...(hasRestart ? ["restart_of_run_id"] : []),
          ...(hasCandidateIDs ? ["input_file_candidate_ids"] : []),
          ...(hasOutputKind ? ["output_kind"] : []),
          ...gateChangeKeys,
        ]) &&
        (!hasCandidateIDs ||
          (Array.isArray(candidateIDs) && candidateIDs.length === 0)) &&
        (!hasOutputKind || outputKind === "text");
  const validFrozenFiles =
    (!hasInputFiles &&
      !hasInputFileTotal &&
      (!hasCandidateIDs ||
        (v5 && Array.isArray(candidateIDs) && candidateIDs.length === 0))) ||
    (hasInputFiles &&
      hasInputFileTotal &&
      Array.isArray(inputFiles) &&
      (retry ||
        (hasCandidateIDs &&
          Array.isArray(candidateIDs) &&
          inputFiles.length === candidateIDs.length)) &&
      inputFiles.length > 0 &&
      inputFiles.length <= 4 &&
      inputFiles.every(validAgentRunInputFile) &&
      inputFiles.every((file) =>
        validFileSourceProof(
          file as unknown as Record<string, unknown>,
          v6,
          String(task.id),
          task.project_id as string | null,
        ),
      ) &&
      v6 ===
        inputFiles.some(
          (file) => file.source_kind === "project_task_artifact",
        ) &&
      new Set(inputFiles.map((file) => file.id)).size === inputFiles.length &&
      (retry ||
        nativePreview ||
        (Array.isArray(candidateIDs) &&
          candidateIDs.every(
            (candidateID) =>
              !inputFiles.some((file) => file.id === candidateID),
          ))) &&
      positiveIntegerValue(snapshot.input_file_total_bytes) &&
      snapshot.input_file_total_bytes ===
        inputFiles.reduce((total, file) => total + file.size_bytes, 0) &&
      snapshot.input_file_total_bytes <= 131_072);
  const fileBoundary =
    !v5 ||
    (Array.isArray(inputFiles) && inputFiles.length > 0) ||
    (object(outputContract) &&
      ["file", "files"].includes(String(outputContract.type)));
  const validV2Boundary =
    v2 &&
    exactKeys(snapshot, [
      ...commonSnapshotKeys,
      ...(hasInputFiles ? ["input_files"] : []),
      ...(hasInputFileTotal ? ["input_file_total_bytes"] : []),
      "output_contract",
      ...(fileBoundary ? ["input_files_leave_device", "file_limits"] : []),
    ]) &&
    validFrozenFiles &&
    validAgentRunOutputContract(outputContract) &&
    outputContract.type === outputKind &&
    (!fileBoundary ||
      (typeof snapshot.input_files_leave_device === "boolean" &&
        snapshot.input_files_leave_device ===
          ((hasInputFiles || v7) && provider.leaves_device === true) &&
        object(fileLimits) &&
        exactKeys(fileLimits, [
          "max_files",
          "max_file_bytes",
          "max_total_bytes",
          "max_result_bytes",
        ]) &&
        fileLimits.max_files === 4 &&
        fileLimits.max_file_bytes === 65_536 &&
        fileLimits.max_total_bytes === 131_072 &&
        fileLimits.max_result_bytes === 65_536));
  const confirmed = value.status === "confirmed";
  const confirmedHasLiveResult = confirmed && object(value.agent_run_result);
  if (
    !exactKeys(value, [
      "id",
      "generation_id",
      "fingerprint",
      "action",
      "preview",
      "status",
      "can_confirm",
      "result_id",
      "result_version",
      "route",
      "created_at",
      "decided_at",
      ...(confirmedHasLiveResult ? ["agent_run_result"] : []),
    ]) ||
    !exactKeys(action, [
      "action",
      "task_id",
      "expected_version",
      "changes",
      ...(retry ? ["agent_run_id"] : []),
    ]) ||
    (!retry && action.action !== "agent_run.start") ||
    typeof action.task_id !== "string" ||
    !canonicalUuid.test(action.task_id) ||
    !positiveIntegerValue(expectedVersion) ||
    !validChanges ||
    typeof providerId !== "string" ||
    !canonicalUuid.test(providerId) ||
    !positiveIntegerValue(expectedProviderVersion) ||
    !positiveIntegerValue(expectedProviderConfigVersion) ||
    !exactKeys(preview, [
      "label",
      "before",
      "after",
      "agent_run_start",
      ...(retry ? ["agent_run_retry"] : []),
    ]) ||
    !object(preview.before) ||
    Object.keys(preview.before).length !== 0 ||
    !object(preview.after) ||
    Object.keys(preview.after).length !== 0 ||
    (!v2 && !exactKeys(snapshot, commonSnapshotKeys)) ||
    (v2 && !validV2Boundary) ||
    !exactKeys(task, [
      "id",
      "title",
      "description",
      "completion_criteria",
      "status",
      "kind",
      "review_policy",
      "priority",
      "project_id",
      "parent_task_id",
      "due_date",
      "planned_date",
      "estimated_minutes",
      "actual_minutes",
      "manual_order",
      "version",
      "created_at",
      "updated_at",
    ]) ||
    task.id !== action.task_id ||
    !nonemptyString(task.title) ||
    typeof task.description !== "string" ||
    typeof task.completion_criteria !== "string" ||
    !["todo", "in_progress"].includes(String(task.status)) ||
    !["work", "review", "followup", "reminder"].includes(String(task.kind)) ||
    !["none", "manual"].includes(String(task.review_policy)) ||
    !["P0", "P1", "P2", "P3"].includes(String(task.priority)) ||
    (task.project_id !== null &&
      (typeof task.project_id !== "string" ||
        !canonicalUuid.test(task.project_id))) ||
    (task.parent_task_id !== null &&
      (typeof task.parent_task_id !== "string" ||
        !canonicalUuid.test(task.parent_task_id))) ||
    (task.due_date !== null && typeof task.due_date !== "string") ||
    (task.planned_date !== null && typeof task.planned_date !== "string") ||
    (task.estimated_minutes !== null &&
      (typeof task.estimated_minutes !== "number" ||
        !Number.isSafeInteger(task.estimated_minutes) ||
        task.estimated_minutes < 0)) ||
    typeof task.actual_minutes !== "number" ||
    !Number.isSafeInteger(task.actual_minutes) ||
    task.actual_minutes < 0 ||
    (task.manual_order !== null &&
      (typeof task.manual_order !== "number" ||
        !Number.isSafeInteger(task.manual_order))) ||
    task.version !== expectedVersion ||
    !utcTimestamp(task.created_at) ||
    !utcTimestamp(task.updated_at) ||
    preview.label !== task.title ||
    !exactKeys(assignment, ["id", "role", "assigned_at", "actor_id"]) ||
    typeof assignment.id !== "string" ||
    !canonicalUuid.test(assignment.id) ||
    assignment.role !== "assignee" ||
    !utcTimestamp(assignment.assigned_at) ||
    typeof assignment.actor_id !== "string" ||
    !canonicalUuid.test(assignment.actor_id) ||
    !exactKeys(agent, ["id", "display_name", "type", "status", "version"]) ||
    agent.id !== assignment.actor_id ||
    !nonemptyString(agent.display_name) ||
    agent.type !== "agent" ||
    agent.status !== "active" ||
    !positiveIntegerValue(agent.version) ||
    !exactKeys(adapter, [
      "id",
      "display_name",
      "kind",
      "protocol_version",
      "status",
      "health_status",
      "isolation_status",
      "execution_ready",
      "version",
    ]) ||
    typeof adapter.id !== "string" ||
    !canonicalUuid.test(adapter.id) ||
    !nonemptyString(adapter.display_name) ||
    adapter.kind !== "builtin" ||
    adapter.protocol_version !== "opc-agent-pipe-v1" ||
    adapter.status !== "enabled" ||
    adapter.health_status !== "healthy" ||
    adapter.isolation_status !== "verified" ||
    adapter.execution_ready !== true ||
    !positiveIntegerValue(adapter.version) ||
    !exactKeys(provider, [
      "id",
      "name",
      "kind",
      "protocol",
      "model",
      "status",
      "health_status",
      "version",
      "config_version",
      ...(providerHasLeavesDevice ? ["leaves_device"] : []),
    ]) ||
    provider.id !== providerId ||
    !nonemptyString(provider.name) ||
    !["local", "remote"].includes(String(provider.kind)) ||
    (anthropic
      ? provider.protocol !== "anthropic_messages" || provider.kind !== "remote"
      : provider.protocol !== "openai_chat") ||
    !nonemptyString(provider.model) ||
    provider.status !== "ready" ||
    provider.health_status !== "healthy" ||
    provider.version !== expectedProviderVersion ||
    provider.config_version !== expectedProviderConfigVersion ||
    (v2 && !providerHasLeavesDevice) ||
    (providerHasLeavesDevice &&
      provider.leaves_device !== (provider.kind === "remote")) ||
    !positiveIntegerValue(snapshot.attempt) ||
    ![1, 2, 3, 4, 5, 6, 7].includes(Number(executionContractVersion)) ||
    !exactKeys(limits, [
      "timeout_seconds",
      "max_result_bytes",
      ...(anthropic ? ["max_output_tokens"] : []),
    ]) ||
    (anthropic && limits.max_output_tokens !== 8192) ||
    (v6 &&
      (!Array.isArray(inputFiles) ||
        !inputFiles.some(
          (file) => file.source_kind === "project_task_artifact",
        ))) ||
    limits.timeout_seconds !== 600 ||
    limits.max_result_bytes !== 65_536 ||
    snapshot.success_does_not_complete_task !== true ||
    snapshot.result_requires_manual_review !== true
  )
    return false;

  const result = value.agent_run_result;
  const runId = value.result_id;
  const baseRoute = `/tasks/${task.id}`;
  if (!confirmed) {
    return (
      runId === null &&
      value.result_version === null &&
      result === undefined &&
      value.route ===
        (retry
          ? agentRunHref(task.id as string, action.agent_run_id as string)
          : hasRestart
            ? agentRunHref(
                task.id as string,
                changes.restart_of_run_id as string,
              )
            : baseRoute)
    );
  }
  if (
    typeof runId !== "string" ||
    !canonicalUuid.test(runId) ||
    (retry && runId === action.agent_run_id) ||
    (hasRestart && runId === changes.restart_of_run_id) ||
    value.result_version !== 1
  )
    return false;
  const runRoute = agentRunHref(task.id as string, runId);
  if (result === undefined) return value.route === "";
  if (!object(result)) return false;
  if (
    hasRestart
      ? result.restart_of_run_id !== changes.restart_of_run_id
      : Object.hasOwn(result, "restart_of_run_id")
  )
    return false;
  const resultStatus = result.status;
  const deliveryStatus = result.output_delivery_status;
  const deliveryErrorCode = result.output_delivery_error_code;
  const submissionId = result.submission_id;
  const submissionStatus = result.submission_status;
  const artifactId = result.artifact_id;
  if (
    ![
      "queued",
      "running",
      "succeeded",
      "failed",
      "cancelled",
      "interrupted",
    ].includes(String(resultStatus)) ||
    !["not_ready", "pending", "submitted", "retained"].includes(
      String(deliveryStatus),
    ) ||
    (deliveryErrorCode !== null &&
      (typeof deliveryErrorCode !== "string" || !deliveryErrorCode)) ||
    (submissionId !== null &&
      (typeof submissionId !== "string" ||
        !canonicalUuid.test(submissionId))) ||
    (artifactId !== null &&
      (typeof artifactId !== "string" || !canonicalUuid.test(artifactId))) ||
    (submissionStatus !== undefined &&
      (deliveryStatus !== "submitted" ||
        ![
          "pending_review",
          "accepted",
          "changes_requested",
          "withdrawn",
        ].includes(String(submissionStatus)))) ||
    !validAgentRunDeliveryState(
      resultStatus as AgentRunStatus,
      deliveryStatus as AgentRunOutputDeliveryStatus,
      deliveryErrorCode as string | null,
      submissionId as string | null,
      artifactId as string | null,
    )
  )
    return false;
  if (hasStartGate !== Object.hasOwn(result, "start_gate")) return false;
  if (hasStartGate) {
    try {
      const parsedGate = agentRunStartGateFromRecord(
        result.start_gate,
        String(resultStatus),
        String(deliveryStatus),
      );
      if (
        !parsedGate ||
        parsedGate.predecessorRunId !== changes.start_after_run_id ||
        parsedGate.require !== changes.start_after ||
        hasInputSlot !== (parsedGate.inputSlot !== undefined)
      )
        return false;
    } catch {
      return false;
    }
  }
  const submissionRoute =
    deliveryStatus === "submitted"
      ? taskSubmissionHref(task.id as string, submissionId as string)
      : null;
  return (
    (value.route === runRoute ||
      (submissionRoute !== null && value.route === submissionRoute)) &&
    exactKeys(result, [
      "id",
      "task_id",
      "status",
      "attempt",
      "provider_id",
      "model",
      "output_delivery_status",
      "output_delivery_error_code",
      "submission_id",
      "artifact_id",
      ...(submissionStatus === undefined ? [] : ["submission_status"]),
      ...(hasRestart ? ["restart_of_run_id"] : []),
      ...(hasStartGate ? ["start_gate"] : []),
    ]) &&
    result.id === runId &&
    result.task_id === task.id &&
    [
      "queued",
      "running",
      "succeeded",
      "failed",
      "cancelled",
      "interrupted",
    ].includes(String(result.status)) &&
    result.attempt === snapshot.attempt &&
    result.provider_id === provider.id &&
    result.model === provider.model
  );
}

/** Reuse the full frozen-start schema for native previews, without creating a
 * proposal. Native file references are real IDs, unlike AI grant-local tokens. */
export function parseNativeAgentRunStartPreview(
  value: unknown,
): AiAgentRunStartPreview {
  if (
    !object(value) ||
    !object(value.task) ||
    !object(value.provider) ||
    !validAgentRunRestartSource(value.restart)
  )
    return invalid();
  const task = value.task,
    provider = value.provider;
  const changes = {
    provider_id: provider.id,
    expected_provider_version: provider.version,
    expected_provider_config_version: provider.config_version,
    restart_of_run_id: value.restart.run_id,
    ...(Array.isArray(value.input_files)
      ? {
          input_file_candidate_ids: value.input_files.map((file) =>
            object(file) ? file.id : null,
          ),
        }
      : {}),
    ...(object(value.output_contract)
      ? { output_kind: value.output_contract.type }
      : {}),
    ...(validAgentRunReworkContext(value.rework_context)
      ? {
          rework_submission_id: value.rework_context.submission_id,
          rework_artifact_ids: value.rework_context.artifacts.map(
            (item) => item.id,
          ),
        }
      : {}),
  };
  const envelope = {
    id: "",
    generation_id: "",
    fingerprint: "",
    created_at: "",
    decided_at: null,
    action: {
      action: "agent_run.start",
      task_id: task.id,
      expected_version: task.version,
      changes,
    },
    preview: {
      label: task.title,
      before: {},
      after: {},
      agent_run_start: value,
    },
    status: "pending",
    can_confirm: false,
    result_id: null,
    result_version: null,
    route: agentRunHref(String(task.id), value.restart.run_id),
  };
  if (!validAgentRunStartProposal(envelope, true)) return invalid();
  return value as unknown as AiAgentRunStartPreview;
}

export function validAgentRunCancelProposal(
  value: Record<string, unknown>,
): boolean {
  const a = value.action,
    p = value.preview;
  if (!object(a) || !object(p)) return false;
  const recovery = a.action === "agent_run.recover_output";
  const previewKey = recovery ? "agent_run_recovery" : "agent_run_cancel";
  const s = p[previewKey];
  if (!object(s) || (!recovery && a.action !== "agent_run.cancel"))
    return false;
  if (
    recovery &&
    (s.status !== "running" ||
      !["text", "file", "files"].includes(String(s.output_kind)) ||
      typeof s.output_bytes !== "number" ||
      !Number.isSafeInteger(s.output_bytes) ||
      s.output_bytes < 1 ||
      s.output_bytes > 65_536 ||
      typeof s.output_sha256 !== "string" ||
      !/^[a-f0-9]{64}$/.test(s.output_sha256))
  )
    return false;
  const confirmed = value.status === "confirmed";
  if (
    !exactKeys(value, [
      "id",
      "generation_id",
      "fingerprint",
      "action",
      "preview",
      "status",
      "can_confirm",
      "result_id",
      "result_version",
      "route",
      "created_at",
      "decided_at",
      ...(value.agent_run_result !== undefined ? ["agent_run_result"] : []),
    ]) ||
    !exactKeys(a, [
      "action",
      "task_id",
      "agent_run_id",
      "expected_version",
      "changes",
    ]) ||
    !exactKeys(p, ["label", "before", "after", previewKey]) ||
    !object(a.changes) ||
    Object.keys(a.changes).length !== 0 ||
    !object(p.before) ||
    Object.keys(p.before).length !== 0 ||
    !object(p.after) ||
    Object.keys(p.after).length !== 0 ||
    !exactKeys(s, [
      "run_id",
      "task_id",
      "task_version",
      "status",
      "attempt",
      "provider_id",
      "model",
      ...(recovery ? ["output_kind", "output_bytes", "output_sha256"] : []),
    ]) ||
    ![s.run_id, s.task_id, s.provider_id].every(
      (id) => typeof id === "string" && canonicalUuid.test(id),
    ) ||
    a.task_id !== s.task_id ||
    a.agent_run_id !== s.run_id ||
    a.expected_version !== s.task_version ||
    !positiveIntegerValue(s.task_version) ||
    !positiveIntegerValue(s.attempt) ||
    !["queued", "running"].includes(String(s.status)) ||
    typeof s.model !== "string" ||
    !s.model ||
    (confirmed && value.can_confirm !== false)
  )
    return false;
  const route = agentRunHref(String(s.task_id), String(s.run_id));
  if (!confirmed)
    return (
      value.result_id === null &&
      value.result_version === null &&
      value.agent_run_result === undefined &&
      value.route === route
    );
  if (value.result_id !== s.run_id || value.result_version !== 1) return false;
  const result = value.agent_run_result;
  if (result === undefined) return value.route === "";
  const hasRestartResult =
    object(result) && Object.hasOwn(result, "restart_of_run_id");
  const hasSubmissionStatus =
    object(result) && Object.hasOwn(result, "submission_status");
  return (
    object(result) &&
    (!hasRestartResult ||
      (typeof result.restart_of_run_id === "string" &&
        canonicalUuid.test(result.restart_of_run_id) &&
        result.restart_of_run_id !== result.id)) &&
    exactKeys(result, [
      "id",
      "task_id",
      "status",
      "attempt",
      "provider_id",
      "model",
      "output_delivery_status",
      "output_delivery_error_code",
      "submission_id",
      "artifact_id",
      ...(hasSubmissionStatus ? ["submission_status"] : []),
      ...(hasRestartResult ? ["restart_of_run_id"] : []),
    ]) &&
    result.id === s.run_id &&
    result.task_id === s.task_id &&
    result.attempt === s.attempt &&
    result.provider_id === s.provider_id &&
    result.model === s.model &&
    (recovery
      ? validAgentRecoveryReceipt(result, value.route, String(s.task_id), route)
      : value.route === route &&
        ["running", "cancelled"].includes(String(result.status)) &&
        result.output_delivery_status === "not_ready" &&
        result.output_delivery_error_code === null &&
        result.submission_id === null &&
        result.artifact_id === null)
  );
}

export function validAgentRunCancelManyProposal(
  value: Record<string, unknown>,
): boolean {
  const action = value.action;
  const preview = value.preview;
  if (!object(action) || !object(preview)) return false;
  const batch = preview.agent_run_cancel_many;
  const changes = action.changes;
  if (
    action.action !== "agent_run.cancel_many" ||
    !exactKeys(action, ["action", "changes"]) ||
    !object(changes) ||
    !exactKeys(changes, ["items"]) ||
    !Array.isArray(changes.items) ||
    changes.items.length < 1 ||
    changes.items.length > 8 ||
    !exactKeys(preview, [
      "label",
      "before",
      "after",
      "agent_run_cancel_many",
    ]) ||
    !object(preview.before) ||
    !object(preview.after) ||
    !exactKeys(preview.before, ["active_runs"]) ||
    !exactKeys(preview.after, ["cancel_requested_runs"]) ||
    !object(batch) ||
    !exactKeys(batch, ["count", "items"]) ||
    !positiveIntegerValue(batch.count) ||
    batch.count !== changes.items.length ||
    batch.count !== preview.before.active_runs ||
    batch.count !== preview.after.cancel_requested_runs ||
    !Array.isArray(batch.items) ||
    batch.items.length !== batch.count
  )
    return false;
  const runIds = new Set<string>();
  const taskIds = new Set<string>();
  for (let index = 0; index < batch.items.length; index += 1) {
    const requested = changes.items[index];
    const item = batch.items[index];
    if (
      !object(requested) ||
      !object(item) ||
      !exactKeys(requested, [
        "agent_run_id",
        "task_id",
        "expected_task_version",
      ]) ||
      !exactKeys(item, [
        "run_id",
        "task_id",
        "task_title",
        "task_version",
        "status",
        "attempt",
        "provider_id",
        "model",
      ]) ||
      ![item.run_id, item.task_id, item.provider_id].every(
        (id) => typeof id === "string" && canonicalUuid.test(id),
      ) ||
      requested.agent_run_id !== item.run_id ||
      requested.task_id !== item.task_id ||
      requested.expected_task_version !== item.task_version ||
      !positiveIntegerValue(item.task_version) ||
      !positiveIntegerValue(item.attempt) ||
      typeof item.task_title !== "string" ||
      item.task_title.length < 1 ||
      typeof item.model !== "string" ||
      item.model.length < 1 ||
      !["queued", "running"].includes(String(item.status)) ||
      runIds.has(String(item.run_id)) ||
      taskIds.has(String(item.task_id))
    )
      return false;
    runIds.add(String(item.run_id));
    taskIds.add(String(item.task_id));
  }
  const confirmed = value.status === "confirmed";
  const hasResult = value.agent_run_cancel_many_result !== undefined;
  if (
    !exactKeys(value, [
      "id",
      "generation_id",
      "fingerprint",
      "action",
      "preview",
      "status",
      "can_confirm",
      "result_id",
      "result_version",
      "route",
      "created_at",
      "decided_at",
      ...(hasResult ? ["agent_run_cancel_many_result"] : []),
    ]) ||
    value.route !== "/ai?workspace=agents"
  )
    return false;
  if (!confirmed)
    return (
      value.result_id === null && value.result_version === null && !hasResult
    );
  if (
    value.can_confirm !== false ||
    value.result_id !== value.id ||
    value.result_version !== 1 ||
    !hasResult
  )
    return false;
  const result = value.agent_run_cancel_many_result;
  if (
    !object(result) ||
    !exactKeys(result, ["count", "items"]) ||
    result.count !== batch.count ||
    !Array.isArray(result.items) ||
    result.items.length !== batch.count
  )
    return false;
  const previewItems = batch.items as Array<Record<string, unknown>>;
  return result.items.every((entry, index) => {
    const expected = previewItems[index];
    return (
      object(entry) &&
      exactKeys(entry, ["run_id", "task_id", "status"]) &&
      entry.run_id === expected.run_id &&
      entry.task_id === expected.task_id &&
      ["running", "cancelled"].includes(String(entry.status))
    );
  });
}

function validAgentRecoveryReceipt(
  result: Record<string, unknown>,
  route: unknown,
  taskId: string,
  runRoute: string,
): boolean {
  const delivery = result.output_delivery_status;
  if (
    result.status !== "succeeded" ||
    !["submitted", "retained"].includes(String(delivery))
  )
    return false;
  if (
    result.output_delivery_error_code !== null &&
    typeof result.output_delivery_error_code !== "string"
  )
    return false;
  if (
    result.submission_status !== undefined &&
    (delivery !== "submitted" ||
      ![
        "pending_review",
        "accepted",
        "changes_requested",
        "withdrawn",
      ].includes(String(result.submission_status)))
  )
    return false;
  for (const id of [result.submission_id, result.artifact_id]) {
    if (id !== null && (typeof id !== "string" || !canonicalUuid.test(id)))
      return false;
  }
  return (
    validAgentRunDeliveryState(
      "succeeded",
      delivery as AgentRunOutputDeliveryStatus,
      result.output_delivery_error_code as string | null,
      result.submission_id as string | null,
      result.artifact_id as string | null,
    ) &&
    route ===
      (delivery === "submitted"
        ? taskSubmissionHref(taskId, result.submission_id as string)
        : runRoute)
  );
}
