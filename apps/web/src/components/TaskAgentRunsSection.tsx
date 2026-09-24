import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import {
  agentRunFileLimits,
  agentRunFileOutputPresets,
  cancelAgentRun,
  createAgentRun,
  getAiProviders,
  getAllActors,
  getTaskAgentRunFiles,
  retryAgentRun,
  ApiError,
} from "../api/client";
import {
  actorQueryKey,
  invalidateAgentRunDeliveryFacts,
  invalidateTaskAggregates,
  taskAgentRunsQueryKey,
  useAgentAdaptersQuery,
  useTaskAgentRunsQuery,
  useTaskAssignmentsQuery,
} from "../api/hooks";
import { agentRunIssueHandoff } from "../lib/aiIssueHandoff";
import { useUiStore } from "../store/ui";
import type {
  Actor,
  AgentAdapter,
  AgentRunFileCandidate,
  AgentRunFileAccessProviderConfirmation,
  AgentRunOutputContract,
  AgentRun,
  AiProvider,
  Task,
  TaskAssignmentListResult,
} from "../types/models";
import { AiIssueHandoffButton } from "./AiWorkbenchHandoff";
import { TaskAgentRework, type AgentReworkApproval } from "./TaskAgentRework";
import { ProjectTaskFileSelect } from "./ProjectTaskFileSelect";
import { agentFileReference } from "../lib/agentProjectFiles";
import { recheckAgentProjectFiles } from "../api/agentProjectFiles";
import { getAgentRunReworkPreview } from "../api/agentRunRework";
import { AgentReworkRetryModal } from "./AgentReworkRetryModal";
import { AgentRunRestartModal } from "./AgentRunRestartModal";
import { canRestartAgentRun } from "../lib/agentRunRestart";
import { agentReworkErrorMessage } from "../lib/agentRunRework";
import { agentRunFailureReason } from "../lib/agentRunFailureSource";
import { agentRunLifecycle } from "../lib/agentRunLifecycle";
import {
  agentRunElapsedLabel,
  agentRunProgressLabel,
} from "../lib/agentRunProgress";
import {
  supportsAgentRunProvider,
  agentRunProviderDisclosure,
} from "../lib/agentRunProvider";
import {
  agentRunFileApplyCandidates,
  useAgentRunFileApply,
  type AgentRunFileApplyCandidate,
} from "../store/agentRunFileApply";

function executionSetup(adapters: AgentAdapter[], actors: Actor[]) {
  const adapter = adapters.find(
    (item) => item.adapterKey === "builtin-local-text-v1",
  );
  if (!adapter)
    return {
      problem:
        "尚未登记内置 Agent，请先在「设置 → 本地 Agent」登记、检查并启用适配器。",
    };
  if (!adapter.executionReady || adapter.healthStatus !== "healthy") {
    return {
      problem:
        "内置 Agent 尚未通过当前平台的运行检查，请在「设置 → 本地 Agent」检查运行条件。",
    };
  }
  if (adapter.status !== "enabled")
    return {
      problem:
        "内置 Agent 尚未启用，请先在「设置 → 本地 Agent」显式启用适配器。",
    };
  const matches = actors.filter(
    (actor) =>
      actor.type === "agent" &&
      actor.status === "active" &&
      actor.agentAdapterId === adapter.id,
  );
  if (matches.length !== 1)
    return {
      problem:
        "未找到唯一可执行的 Agent 身份，请在「设置 → 本地 Agent」检查启用状态后刷新。",
    };
  return { actor: matches[0] };
}

function assignmentProblem(
  assignments: TaskAssignmentListResult,
  actor: Actor,
) {
  const assignee = assignments.active.assignee;
  return assignee && assignee.actorId !== actor.id
    ? "当前任务已有其他负责人，请先在「责任分派」中处理分派；不会自动替换现有负责人。"
    : null;
}

function executionError(error: unknown) {
  const reworkMessage = agentReworkErrorMessage(error);
  if (reworkMessage) return reworkMessage;
  if (error instanceof ApiError) {
    switch (error.code) {
      case "ACTOR_NOT_FOUND":
      case "ASSIGNMENT_ACTOR_NOT_ACTIVE":
      case "ASSIGNMENT_ACTOR_NOT_EXECUTABLE":
      case "AGENT_RUN_NOT_EXECUTABLE":
        return "Agent 身份或启用状态已变化，请检查「设置 → 本地 Agent」并刷新执行状态。";
      case "VERSION_CONFLICT":
      case "ASSIGNMENT_ALREADY_ACTIVE":
        return "任务或负责人已变化，请刷新并确认最新分派后重试。";
      case "AGENT_PROVIDER_INVALID":
        return "所选模型当前不可用于执行，请检查模型配置后重新选择。";
      case "AGENT_RUN_FILE_INVALID":
      case "AGENT_RUN_FILE_UNAVAILABLE":
      case "AGENT_RUN_FILE_INTEGRITY_INVALID":
      case "AGENT_RUN_FILE_STORAGE_UNAVAILABLE":
        return "所选受控文件已变化或当前不可读取；本次未启动，请刷新文件并重新确认。";
      case "AGENT_FILE_ACCESS_CONFIRMATION_REQUIRED":
        return "发送文件正文前，需要单独确认所选 Provider 的文件访问。";
      case "AGENT_FILE_ACCESS_PROVIDER_CONFIRMATION_REQUIRED":
        return "请重新核对当前 Provider 的版本和本地/在线类型，再确认文件访问。";
      case "AGENT_RUN_IDENTITY_CHANGED":
        return "任务、Agent 或 Provider 身份已变化；本次未启动，请刷新并重新确认。";
      case "AGENT_RUN_QUEUE_FULL":
        return "Agent 执行队列已满，本次没有创建执行，也没有修改任务分派。等已有执行释放容量后，可以直接重试。";
    }
  }
  return error instanceof Error
    ? error.message
    : "执行操作失败，请刷新状态后重试。";
}

const pendingDeliveryGuidance =
  "执行结果正在等待产出登记；请点击「查看执行过程」，再选择「重试登记产出」。";

interface TaskAgentRunsSectionProps {
  task: Task;
  disabled?: boolean;
  returnSession?: string | null;
}

function downloadTextFile(name: string, mime: string, content: string) {
  const blob = new Blob([content], { type: `${mime};charset=utf-8` });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = name;
  anchor.click();
  URL.revokeObjectURL(url);
}

function downloadRunResult(taskTitle: string, run: AgentRun) {
  if (!run.resultText || run.outputContract?.type === "files") return;
  if (run.outputContract?.type === "file") {
    downloadTextFile(
      run.outputContract.name,
      run.outputContract.mime,
      run.resultText,
    );
    return;
  }
  const isHtml = /<!doctype html|<html/i.test(run.resultText);
  downloadTextFile(
    `${taskTitle}${isHtml ? ".html" : ".md"}`,
    isHtml ? "text/html" : "text/markdown",
    run.resultText,
  );
}

function orderedMultiFileResult(
  run: AgentRun,
): readonly { name: string; mime: string; content: string }[] | null {
  if (run.outputContract?.type !== "files" || !run.resultText) return null;
  try {
    const bodies: unknown = JSON.parse(run.resultText);
    if (
      !Array.isArray(bodies) ||
      bodies.length !== run.outputContract.files.length ||
      !bodies.every((body) => typeof body === "string")
    ) {
      return null;
    }
    return run.outputContract.files.map((file, index) => ({
      ...file,
      content: bodies[index] as string,
    }));
  } catch {
    return null;
  }
}

function formatRunTime(value: string | null) {
  return value ? new Date(value).toLocaleString() : "—";
}

function formatBytes(value: number) {
  return value < 1024
    ? `${value} B`
    : `${Math.round((value / 1024) * 10) / 10} KiB`;
}

function fileCandidateKey(
  candidate: Pick<AgentRunFileCandidate, "sourceKind" | "id">,
) {
  return `${candidate.sourceKind}:${candidate.id}`;
}

function sameFileCandidate(
  selected: AgentRunFileCandidate,
  current: AgentRunFileCandidate,
) {
  return (
    selected.sourceKind === current.sourceKind &&
    selected.id === current.id &&
    selected.name === current.name &&
    selected.mime === current.mime &&
    selected.sizeBytes === current.sizeBytes &&
    selected.sha256 === current.sha256 &&
    current.eligible
  );
}

function selectedFilesProblem(
  selected: readonly AgentRunFileCandidate[],
  current: readonly AgentRunFileCandidate[],
  limits: { maxFiles: number; maxFileBytes: number; maxTotalBytes: number },
) {
  if (selected.length > limits.maxFiles) {
    return `最多只能选择 ${limits.maxFiles} 个文件。`;
  }
  const total = selected.reduce((sum, file) => sum + file.sizeBytes, 0);
  if (
    total > limits.maxTotalBytes ||
    selected.some((file) => file.sizeBytes > limits.maxFileBytes)
  ) {
    return `文件大小超过当前服务端允许的受控输入范围。`;
  }
  for (const file of selected) {
    const latest = current.find(
      (candidate) => fileCandidateKey(candidate) === fileCandidateKey(file),
    );
    if (!latest || !sameFileCandidate(file, latest)) {
      return `“${file.name}”已变化或不可用；不会自动换成新版本，请取消选择后重新勾选。`;
    }
  }
  return null;
}

function providerFileAccessConfirmation(
  provider: AiProvider | undefined,
): AgentRunFileAccessProviderConfirmation | null {
  const configVersion = provider?.configVersion;
  if (
    !provider ||
    !Number.isSafeInteger(provider.version) ||
    provider.version < 1 ||
    !Number.isSafeInteger(configVersion) ||
    (configVersion ?? 0) < 1
  ) {
    return null;
  }
  return {
    version: provider.version,
    configVersion: configVersion!,
    kind: provider.kind,
  };
}

function sameProviderFileAccessConfirmation(
  left: AgentRunFileAccessProviderConfirmation,
  right: AgentRunFileAccessProviderConfirmation,
) {
  return (
    left.version === right.version &&
    left.configVersion === right.configVersion &&
    left.kind === right.kind
  );
}

function prepareAgentRunFileApply(candidate: AgentRunFileApplyCandidate) {
  if (!useAgentRunFileApply.getState().select(candidate)) return;
  useUiStore.setState({ rightOverviewCollapsed: false });
  useUiStore.getState().setRightPanelTab("files", "activate");
}

type OutputSelection = "text" | "files" | `file:${number}`;

function selectedOutputContract(
  value: OutputSelection,
  selectedPresetIndexes: readonly number[],
): AgentRunOutputContract {
  if (value === "text") return { type: "text" };
  if (value === "files") {
    const files = selectedPresetIndexes
      .map((index) => agentRunFileOutputPresets[index])
      .filter((preset): preset is (typeof agentRunFileOutputPresets)[number] =>
        Boolean(preset),
      )
      .map((preset) => ({ name: preset.name, mime: preset.mime }));
    return { type: "files", files };
  }
  const index = Number(value.slice("file:".length));
  const preset = agentRunFileOutputPresets[index];
  return preset
    ? { type: "file", name: preset.name, mime: preset.mime }
    : { type: "text" };
}

export function TaskAgentRunsSection({
  task,
  disabled,
  returnSession,
}: TaskAgentRunsSectionProps) {
  const queryClient = useQueryClient();
  const openAgentRunDrawer = useUiStore((state) => state.openAgentRunDrawer);
  const [providerId, setProviderId] = useState("");
  const [reworkActive, setReworkActive] = useState(false);
  const [reworkApproval, setReworkApproval] =
    useState<AgentReworkApproval | null>(null);
  const [reworkReset, setReworkReset] = useState(0);
  const [reworkRetryId, setReworkRetryId] = useState<string | null>(null);
  const [restartRunId, setRestartRunId] = useState<string | null>(null);
  const reworkOwner = useRef(0);
  useEffect(() => {
    reworkOwner.current += 1;
    return () => {
      reworkOwner.current += 1;
    };
  }, [
    task.id,
    task.version,
    task.currentSubmissionId,
    providerId,
    reworkActive,
  ]);
  const [expandedRunId, setExpandedRunId] = useState<string | null>(null);
  const [selectedFiles, setSelectedFiles] = useState<AgentRunFileCandidate[]>(
    [],
  );
  const [projectFileConsent, setProjectFileConsent] = useState(false);
  const projectFiles = selectedFiles.filter(
    (file) => file.sourceKind === "project_task_artifact",
  );
  const ordinaryFiles = selectedFiles.filter(
    (file) => file.sourceKind !== "project_task_artifact",
  );
  const [fileAccessProviderConfirmation, setFileAccessProviderConfirmation] =
    useState<AgentRunFileAccessProviderConfirmation | null>(null);
  const [fileSelectionError, setFileSelectionError] = useState<string | null>(
    null,
  );
  const [outputSelection, setOutputSelection] =
    useState<OutputSelection>("text");
  const [selectedOutputPresetIndexes, setSelectedOutputPresetIndexes] =
    useState<number[]>([0, 1]);
  const adaptersQuery = useAgentAdaptersQuery();
  const actorsQuery = useQuery({
    queryKey: [...actorQueryKey, "agent-execution-options"],
    queryFn: ({ signal }) =>
      getAllActors({ type: "agent", status: "active" }, signal),
    enabled:
      adaptersQuery.data?.some(
        (adapter) => adapter.status === "enabled" && adapter.executionReady,
      ) ?? false,
    retry: false,
    staleTime: 10_000,
  });
  const assignmentsQuery = useTaskAssignmentsQuery(task.id);

  const runsQuery = useTaskAgentRunsQuery(task.id);

  const filesQuery = useQuery({
    queryKey: ["task-agent-run-files", task.id],
    queryFn: ({ signal }) => getTaskAgentRunFiles(task.id, signal),
    retry: false,
    staleTime: 0,
  });

  const providersQuery = useQuery({
    queryKey: ["ai-providers-for-agent"],
    queryFn: () => getAiProviders(),
  });
  const usableProviders = (providersQuery.data ?? [])
    .filter(
      (provider) =>
        provider.status === "ready" && provider.health_status === "healthy",
    )
    .filter(supportsAgentRunProvider)
    .filter(
      (provider) =>
        Number.isSafeInteger(provider.version) &&
        provider.version > 0 &&
        Number.isSafeInteger(provider.configVersion) &&
        (provider.configVersion ?? 0) > 0,
    );
  const setup = executionSetup(
    adaptersQuery.data ?? [],
    actorsQuery.data ?? [],
  );
  const assignments = assignmentsQuery.data?.pages[0];
  const setupLoading =
    adaptersQuery.isPending ||
    assignmentsQuery.isPending ||
    (actorsQuery.isEnabled && actorsQuery.isPending);
  const setupError =
    adaptersQuery.error ?? actorsQuery.error ?? assignmentsQuery.error;
  const readinessProblem = setupLoading
    ? "正在检查 Agent 与任务分派状态…"
    : setupError
      ? "无法读取 Agent 或任务分派状态，请刷新重试。"
      : (setup.problem ??
        (setup.actor && assignments
          ? assignmentProblem(assignments, setup.actor)
          : "无法确认当前任务分派，请刷新重试。"));
  const taskProblem =
    task.status !== "todo" && task.status !== "in_progress"
      ? "当前任务状态不可启动执行，请先将任务恢复为待办或执行中。"
      : null;
  const activeRun = runsQuery.data?.some(
    (run) => run.status === "queued" || run.status === "running",
  );
  const pendingDelivery = runsQuery.data?.some(
    (run) => run.outputDeliveryStatus === "pending",
  );
  const selectedProvider = usableProviders.find(
    (provider) => provider.id === providerId,
  );
  const selectedProviderConfirmation =
    providerFileAccessConfirmation(selectedProvider);
  useEffect(() => {
    reworkOwner.current += 1;
    setFileAccessProviderConfirmation(null);
    setReworkApproval(null);
  }, [selectedProvider?.protocol, selectedProvider?.model]);
  const fileAccessConfirmed = Boolean(
    fileAccessProviderConfirmation &&
    selectedProviderConfirmation &&
    sameProviderFileAccessConfirmation(
      fileAccessProviderConfirmation,
      selectedProviderConfirmation,
    ),
  );
  const projectConfirmationIdentity = JSON.stringify({
    files: selectedFiles,
    provider: selectedProvider,
    taskId: task.id,
    version: task.version,
    projectId: task.projectId,
  });
  useEffect(() => {
    reworkOwner.current += 1;
    setProjectFileConsent(false);
    setFileAccessProviderConfirmation(null);
  }, [projectConfirmationIdentity]);
  const currentFileCandidates = filesQuery.data?.items ?? [];
  const fileLimits = filesQuery.data?.limits ?? agentRunFileLimits;
  const selectedFileBytes = selectedFiles.reduce(
    (sum, file) => sum + file.sizeBytes,
    0,
  );
  const fileIdentityProblem =
    ordinaryFiles.length > 0
      ? filesQuery.isError
        ? "无法重新验证所选文件，本次不会发送文件正文。"
        : selectedFilesProblem(ordinaryFiles, currentFileCandidates, fileLimits)
      : null;
  const fileStartProblem =
    fileSelectionError ??
    fileIdentityProblem ??
    (projectFiles.length > 0 && !projectFileConsent
      ? "请另行确认跨任务已验收文件来源。"
      : null) ??
    (selectedFiles.length > 0 && !fileAccessConfirmed
      ? "请单独勾选文件正文发送确认后再启动。"
      : null);
  const outputContract = selectedOutputContract(
    outputSelection,
    selectedOutputPresetIndexes,
  );
  const outputSelectionProblem =
    outputContract.type === "files" && outputContract.files.length < 2
      ? "多文件交付至少需要选择两个不同的受控输出文件。"
      : null;
  const startProblem =
    readinessProblem ??
    taskProblem ??
    (pendingDelivery
      ? pendingDeliveryGuidance
      : activeRun
        ? "任务已有正在进行的执行，请等待结束或先取消。"
        : null);

  useEffect(() => {
    setProviderId("");
    setReworkActive(false);
    setReworkApproval(null);
    setReworkRetryId(null);
    setRestartRunId(null);
    setSelectedFiles([]);
    setFileAccessProviderConfirmation(null);
    setFileSelectionError(null);
    setOutputSelection("text");
    setSelectedOutputPresetIndexes([0, 1]);
  }, [task.id]);

  useEffect(() => {
    setFileAccessProviderConfirmation((current) => {
      if (!current) return null;
      const latest = providerFileAccessConfirmation(selectedProvider);
      return latest && sameProviderFileAccessConfirmation(current, latest)
        ? current
        : null;
    });
  }, [selectedProvider]);

  async function refreshExecutionState() {
    return Promise.all([
      adaptersQuery.refetch(),
      actorsQuery.refetch(),
      assignmentsQuery.refetch(),
      providersQuery.refetch(),
      runsQuery.refetch(),
    ]);
  }

  async function checkBeforeExecution() {
    const [adapters, actors, taskAssignments, providers, runs] =
      await refreshExecutionState();
    if (
      adapters.error ||
      actors.error ||
      taskAssignments.error ||
      providers.error ||
      runs.error
    ) {
      throw new Error("无法确认最新执行状态，本次未启动，请刷新后重试。");
    }
    const current = executionSetup(adapters.data ?? [], actors.data ?? []);
    if (!current.actor) throw new Error(current.problem);
    const currentAssignments = taskAssignments.data?.pages[0];
    if (!currentAssignments)
      throw new Error("无法读取当前任务分派，本次未启动。");
    const conflict = assignmentProblem(currentAssignments, current.actor);
    if (conflict || taskProblem) throw new Error(conflict ?? taskProblem!);
    if (runs.data?.some((run) => run.outputDeliveryStatus === "pending")) {
      throw new Error(pendingDeliveryGuidance);
    }
    if (
      runs.data?.some(
        (run) => run.status === "queued" || run.status === "running",
      )
    ) {
      throw new Error("任务已有正在进行的执行，请等待结束或先取消。");
    }
    const currentProvider = requireProvider(providers.data ?? [], providerId);
    if (
      currentProvider.protocol !== selectedProvider?.protocol ||
      currentProvider.model !== selectedProvider?.model
    ) {
      setFileAccessProviderConfirmation(null);
      setReworkApproval(null);
      throw new Error(
        "所选 Provider 的协议或模型已变化，请重新核对执行边界并确认。",
      );
    }
    let confirmedProvider: AgentRunFileAccessProviderConfirmation | undefined;
    if (selectedFiles.length > 0) {
      const latestProvider = providerFileAccessConfirmation(currentProvider);
      if (
        !fileAccessProviderConfirmation ||
        !latestProvider ||
        !sameProviderFileAccessConfirmation(
          fileAccessProviderConfirmation,
          latestProvider,
        )
      ) {
        setFileAccessProviderConfirmation(null);
        throw new Error(
          "所选 Provider 的版本或本地/在线类型已变化；请重新核对离机提示并确认文件访问。",
        );
      }
      confirmedProvider = fileAccessProviderConfirmation;
      if (ordinaryFiles.length > 0) {
        const currentFiles = await filesQuery.refetch();
        if (currentFiles.error || !currentFiles.data) {
          throw new Error("无法重新验证所选文件，本次未启动。请刷新后重试。");
        }
        const fileProblem = selectedFilesProblem(
          ordinaryFiles,
          currentFiles.data.items,
          currentFiles.data.limits,
        );
        if (fileProblem) throw new Error(fileProblem);
      }
      if (projectFiles.length > 0) {
        if (
          !projectFileConsent ||
          selectedFiles.length > 4 ||
          selectedFileBytes > 131072 ||
          projectFiles.some(
            (file) => file.sourceTask?.project_id !== task.projectId,
          )
        )
          throw new Error("请重新核对跨任务来源和共同文件预算。");
        await recheckAgentProjectFiles(task.id, projectFiles);
      }
    }
    return {
      actor: current.actor,
      assignments: currentAssignments,
      providers: providers.data ?? [],
      fileAccessProviderConfirmation: confirmedProvider,
    };
  }

  function requireProvider(
    providers: NonNullable<typeof providersQuery.data>,
    id: string,
  ) {
    const provider = providers.find(
      (candidate) =>
        candidate.id === id &&
        candidate.status === "ready" &&
        candidate.health_status === "healthy" &&
        supportsAgentRunProvider(candidate) &&
        providerFileAccessConfirmation(candidate) !== null,
    );
    if (!provider) {
      throw new Error(
        "所选模型不可用于执行，请选择已就绪的 OpenAI 兼容模型或远程 Anthropic Messages 模型。",
      );
    }
    return provider;
  }

  const invalidate = () => {
    void queryClient.invalidateQueries({
      queryKey: taskAgentRunsQueryKey(task.id),
    });
  };

  function toggleFileCandidate(
    candidate: AgentRunFileCandidate,
    checked: boolean,
  ) {
    setFileSelectionError(null);
    setFileAccessProviderConfirmation(null);
    const key = fileCandidateKey(candidate);
    if (!checked) {
      setSelectedFiles((current) =>
        current.filter((file) => fileCandidateKey(file) !== key),
      );
      return;
    }
    if (!candidate.eligible) {
      setFileSelectionError("该文件不满足受控文本输入要求，不能发送。 ");
      return;
    }
    if (selectedFiles.some((file) => fileCandidateKey(file) === key)) return;
    const limits = filesQuery.data?.limits ?? agentRunFileLimits;
    if (selectedFiles.length >= limits.maxFiles) {
      setFileSelectionError(`最多只能选择 ${limits.maxFiles} 个文件。`);
      return;
    }
    if (selectedFileBytes + candidate.sizeBytes > limits.maxTotalBytes) {
      setFileSelectionError(
        `文件总量不能超过 ${formatBytes(limits.maxTotalBytes)}。`,
      );
      return;
    }
    setSelectedFiles((current) => [...current, candidate]);
  }

  const startMutation = useMutation({
    mutationFn: async () => {
      const owner = reworkOwner.current;
      const current = await checkBeforeExecution();
      if (projectFiles.length > 0 && owner !== reworkOwner.current)
        throw new Error("跨任务文件预览已关闭或选择已变化，未启动执行。");
      if (reworkActive) {
        if (owner !== reworkOwner.current)
          throw new Error("返工预览已关闭或来源已切换，未启动执行。");
        if (
          !reworkApproval ||
          !current.assignments.active.assignee ||
          reworkApproval.providerId !== providerId
        )
          throw new Error("请先完成分派、预览返工上下文并独立确认发送。");
        const latestProvider = providerFileAccessConfirmation(
          requireProvider(current.providers, providerId),
        );
        if (
          !latestProvider ||
          !sameProviderFileAccessConfirmation(
            reworkApproval.providerConfirmation,
            latestProvider,
          )
        ) {
          setReworkApproval(null);
          setReworkReset((value) => value + 1);
          throw new Error(
            "Provider 身份已变化，请重新核对返工内容及离机说明。",
          );
        }
        const latest = await getAgentRunReworkPreview(
          task.id,
          reworkApproval.request,
        );
        if (owner !== reworkOwner.current)
          throw new Error("返工预览已关闭或来源已切换，未启动执行。");
        if (
          JSON.stringify(latest) !== JSON.stringify(reworkApproval.context) ||
          current.assignments.meta.taskVersion !==
            reworkApproval.request.expectedTaskVersion
        ) {
          setReworkApproval(null);
          setReworkReset((value) => value + 1);
          throw new Error("返工来源或任务版本已变化，请重新预览并确认。");
        }
      }
      const autoAssign = current.assignments.active.assignee
        ? undefined
        : {
            actorId: current.actor.id,
            expectedTaskVersion: current.assignments.meta.taskVersion,
          };
      if (
        !reworkActive &&
        selectedFiles.length === 0 &&
        outputContract.type === "text"
      ) {
        return autoAssign
          ? createAgentRun(task.id, providerId, { autoAssign })
          : createAgentRun(task.id, providerId);
      }
      return createAgentRun(task.id, providerId, {
        ...(reworkActive && reworkApproval
          ? {
              rework: reworkApproval.request,
              confirmReworkContext: true,
              reworkProviderConfirmation: reworkApproval.providerConfirmation,
            }
          : {}),
        ...(autoAssign ? { autoAssign } : {}),
        outputContract,
        ...(selectedFiles.length > 0
          ? {
              inputFiles: selectedFiles.map(agentFileReference),
              ...(projectFiles.length ? { confirmProjectTaskFiles: true } : {}),
              confirmFileAccess: true,
              fileAccessProviderConfirmation:
                current.fileAccessProviderConfirmation,
            }
          : {}),
      });
    },
    onSuccess: (run) => {
      setProviderId("");
      setReworkActive(false);
      setReworkApproval(null);
      setSelectedFiles([]);
      setFileAccessProviderConfirmation(null);
      setFileSelectionError(null);
      setOutputSelection("text");
      setSelectedOutputPresetIndexes([0, 1]);
      invalidate();
      void invalidateTaskAggregates(queryClient);
      // A newly queued Run can change the continuation/attention projection.
      // This only reloads local facts; it does not continue or control the Run.
      void invalidateAgentRunDeliveryFacts(queryClient, run.taskId);
      openAgentRunDrawer(task.id, run.id, returnSession);
    },
    onError: (error) => {
      if (projectFiles.length > 0) {
        setProjectFileConsent(false);
        setFileAccessProviderConfirmation(null);
      }
      if (error instanceof ApiError && error.status === 409) {
        setReworkApproval(null);
        setReworkReset((value) => value + 1);
        setFileAccessProviderConfirmation(null);
        void assignmentsQuery.refetch();
        void invalidateTaskAggregates(queryClient);
        void providersQuery.refetch();
        void filesQuery.refetch();
      }
    },
  });

  const cancelMutation = useMutation({
    mutationFn: (runId: string) => cancelAgentRun(runId),
    onSuccess: () => {
      invalidate();
      void invalidateAgentRunDeliveryFacts(queryClient, task.id);
    },
  });
  const retryMutation = useMutation({
    mutationFn: async (run: AgentRun) => {
      const current = await checkBeforeExecution();
      if (
        current.assignments.active.assignee?.actorId !== run.actorId ||
        current.actor.id !== run.actorId
      ) {
        throw new Error(
          "原执行的负责人已变化，不能直接重试；请确认分派后重新启动。",
        );
      }
      requireProvider(current.providers, run.providerId);
      return retryAgentRun(run.id);
    },
    onSuccess: (run) => {
      invalidate();
      void invalidateAgentRunDeliveryFacts(queryClient, run.taskId);
    },
  });

  const runs = runsQuery.data ?? [];
  const busy =
    startMutation.isPending ||
    cancelMutation.isPending ||
    retryMutation.isPending;
  const fileGroups = [
    {
      sourceKind: "task_artifact" as const,
      title: "当前任务产出",
      items: currentFileCandidates.filter(
        (file) => file.sourceKind === "task_artifact",
      ),
    },
    {
      sourceKind: "project_attachment" as const,
      title: "当前项目附件",
      items: currentFileCandidates.filter(
        (file) => file.sourceKind === "project_attachment",
      ),
    },
  ];

  return (
    <section className="task-section task-agent-runs">
      {restartRunId ? (
        <AgentRunRestartModal
          key={`${task.id}:${restartRunId}`}
          taskId={task.id}
          runId={restartRunId}
          onClose={() => setRestartRunId(null)}
          onSuccess={(run) => {
            setRestartRunId(null);
            invalidate();
            void invalidateAgentRunDeliveryFacts(queryClient, run.taskId);
            openAgentRunDrawer(run.taskId, run.id, returnSession);
          }}
        />
      ) : null}
      {reworkRetryId ? (
        <AgentReworkRetryModal
          taskId={task.id}
          runId={reworkRetryId}
          onClose={() => setReworkRetryId(null)}
          onSuccess={(newRun) => {
            setReworkRetryId(null);
            invalidate();
            void invalidateAgentRunDeliveryFacts(queryClient, newRun.taskId);
            openAgentRunDrawer(task.id, newRun.id, returnSession);
          }}
        />
      ) : null}
      <div className="task-outputs-heading task-agent-runs-heading">
        <div>
          <h3>Agent 执行</h3>
          <p>
            由已启用的内置 Agent
            调用所选模型生成交付文本；满足人工验收条件时提交产出，否则保留在执行记录中。
          </p>
        </div>
        <button
          className="button button-secondary"
          disabled={disabled || busy}
          onClick={() =>
            openAgentRunDrawer(task.id, runs[0]?.id, returnSession)
          }
          type="button"
        >
          查看执行过程
        </button>
      </div>
      <div className="task-agent-runs-controls">
        <select
          aria-label="选择执行模型"
          disabled={
            disabled ||
            busy ||
            Boolean(startProblem) ||
            providersQuery.isPending ||
            providersQuery.isError
          }
          onChange={(event) => {
            setProviderId(event.currentTarget.value);
            setFileAccessProviderConfirmation(null);
          }}
          value={providerId}
        >
          <option value="">选择模型…</option>
          {usableProviders.map((provider) => (
            <option key={provider.id} value={provider.id}>
              {provider.name}（{provider.kind === "local" ? "本地" : "在线"} ·{" "}
              {provider.model}）
            </option>
          ))}
        </select>
        <button
          className="button button-primary"
          disabled={
            disabled ||
            busy ||
            !selectedProvider ||
            Boolean(startProblem) ||
            Boolean(fileStartProblem) ||
            Boolean(outputSelectionProblem) ||
            (reworkActive && !reworkApproval) ||
            providersQuery.isError ||
            runsQuery.isPending ||
            runsQuery.isError
          }
          onClick={() => startMutation.mutate()}
          type="button"
        >
          {reworkActive ? "基于退回批次启动新执行" : "启动执行"}
        </button>
      </div>
      {selectedProvider ? (
        <p role="note">{agentRunProviderDisclosure(selectedProvider)}</p>
      ) : null}
      <button
        className="button button-secondary"
        type="button"
        disabled={disabled || busy}
        onClick={() => {
          setReworkActive((value) => !value);
          setReworkApproval(null);
        }}
      >
        {reworkActive ? "关闭返工执行" : "基于当前退回批次再次执行"}
      </button>
      {reworkActive ? (
        <TaskAgentRework
          key={`${task.id}:${reworkReset}`}
          task={task}
          provider={selectedProvider}
          disabled={disabled || busy}
          hasAssignee={!!assignments?.active.assignee}
          onChange={setReworkApproval}
        />
      ) : null}
      <details className="task-agent-files">
        <summary>
          <span>受控输入文件与输出</span>
          <span className="task-agent-files-summary">
            已选 {selectedFiles.length}/{fileLimits.maxFiles} ·{" "}
            {formatBytes(selectedFileBytes)}/
            {formatBytes(fileLimits.maxTotalBytes)}
          </span>
        </summary>
        <div className="task-agent-files-body">
          <p className="task-agent-files-help">
            原文件范围仅此任务的有效文件产出与当前项目附件；跨任务已验收文件须在独立区域选择。只传输已勾选的
            UTF-8 文本正文，不向执行器暴露本地路径。
          </p>
          <ProjectTaskFileSelect
            taskId={task.id}
            projectId={task.projectId}
            selected={projectFiles}
            disabled={busy}
            otherCount={ordinaryFiles.length}
            otherBytes={ordinaryFiles.reduce(
              (sum, file) => sum + file.sizeBytes,
              0,
            )}
            onChange={(files) => {
              setSelectedFiles([...ordinaryFiles, ...files]);
              setFileSelectionError(null);
            }}
          />
          {projectFiles.length > 0 ? (
            <label>
              <input
                type="checkbox"
                checked={projectFileConsent}
                disabled={busy}
                onChange={(event) =>
                  setProjectFileConsent(event.target.checked)
                }
              />
              我另行确认上述跨任务文件来自同项目已完成任务的当前已验收批次，同意用于本次执行；这不代替文件正文发送确认或新任务验收
            </label>
          ) : null}
          <label className="task-agent-output-field">
            <span>输出格式</span>
            <select
              aria-label="选择 Agent 输出格式"
              disabled={busy}
              onChange={(event) => {
                setOutputSelection(
                  event.currentTarget.value as OutputSelection,
                );
                setFileSelectionError(null);
              }}
              value={outputSelection}
            >
              <option value="text">文本结果（默认）</option>
              <option value="files">多个受控文件（按下方选择）</option>
              {agentRunFileOutputPresets.map((preset, index) => (
                <option key={preset.name} value={`file:${index}`}>
                  文件 · {preset.name}
                </option>
              ))}
            </select>
          </label>
          {outputSelection === "files" ? (
            <fieldset className="task-agent-output-files">
              <legend>多文件交付</legend>
              <p className="task-agent-files-help">
                文件名和 MIME
                会在启动前冻结；模型只能按这里的顺序提供正文，不能添加路径或额外文件。所有文件总计最多{" "}
                {formatBytes(fileLimits.maxResultBytes)}。
              </p>
              {agentRunFileOutputPresets.map((preset, index) => {
                const checked = selectedOutputPresetIndexes.includes(index);
                return (
                  <label key={preset.name}>
                    <input
                      aria-label={`选择输出文件 ${preset.name}`}
                      checked={checked}
                      disabled={busy}
                      onChange={(event) => {
                        setSelectedOutputPresetIndexes((current) => {
                          if (event.currentTarget.checked) {
                            return current.includes(index)
                              ? current
                              : [...current, index].sort((a, b) => a - b);
                          }
                          return current.filter((value) => value !== index);
                        });
                      }}
                      type="checkbox"
                    />
                    {preset.name}（{preset.mime}）
                  </label>
                );
              })}
            </fieldset>
          ) : null}
          {outputSelectionProblem ? (
            <p className="task-agent-runs-alert" role="status">
              {outputSelectionProblem}
            </p>
          ) : null}
          {filesQuery.isPending ? (
            <p role="status">正在读取可用文件…</p>
          ) : filesQuery.isError ? (
            <p className="task-agent-runs-alert" role="alert">
              无法读取受控文件候选；不会发送任何文件正文。
            </p>
          ) : currentFileCandidates.length === 0 ? (
            <p className="task-agent-runs-empty">
              当前没有可用的受控文件候选。
            </p>
          ) : (
            <div className="task-agent-file-groups">
              {fileGroups.map((group) => (
                <section
                  key={group.sourceKind}
                  className="task-agent-file-group"
                >
                  <h4>{group.title}</h4>
                  {group.items.length === 0 ? (
                    <p>暂无文件。</p>
                  ) : (
                    <ul>
                      {group.items.map((candidate) => {
                        const key = fileCandidateKey(candidate);
                        const checked = selectedFiles.some(
                          (file) => fileCandidateKey(file) === key,
                        );
                        const exceedsSelectionLimit =
                          !checked &&
                          (selectedFiles.length >= fileLimits.maxFiles ||
                            selectedFileBytes + candidate.sizeBytes >
                              fileLimits.maxTotalBytes);
                        return (
                          <li key={key}>
                            <label>
                              <input
                                aria-label={`选择文件 ${candidate.name}`}
                                checked={checked}
                                disabled={
                                  busy ||
                                  (!checked &&
                                    (!candidate.eligible ||
                                      exceedsSelectionLimit))
                                }
                                onChange={(event) =>
                                  toggleFileCandidate(
                                    candidate,
                                    event.currentTarget.checked,
                                  )
                                }
                                type="checkbox"
                              />
                              <span className="task-agent-file-name">
                                {candidate.name}
                              </span>
                              <span className="task-agent-file-meta">
                                {candidate.mime} ·{" "}
                                {formatBytes(candidate.sizeBytes)}
                              </span>
                            </label>
                            {!candidate.eligible ? (
                              <span className="task-agent-file-unavailable">
                                不可用 · {candidate.errorCode}
                              </span>
                            ) : exceedsSelectionLimit ? (
                              <span className="task-agent-file-unavailable">
                                已达到文件数量或总量上限
                              </span>
                            ) : null}
                          </li>
                        );
                      })}
                    </ul>
                  )}
                </section>
              ))}
            </div>
          )}
          {selectedFiles.length > 0 ? (
            <div className="task-agent-file-consent">
              {selectedProvider?.kind === "remote" ? (
                <p data-tone="warning" role="status">
                  所选 Provider「{selectedProvider.name}」为在线服务；这{" "}
                  {selectedFiles.length}
                  个文件的正文将离开本机并发送给该 Provider。
                </p>
              ) : selectedProvider?.kind === "local" ? (
                <p role="status">
                  所选 Provider「{selectedProvider.name}
                  」在本机运行，文件正文不会发送到远程 Provider。
                </p>
              ) : (
                <p role="status">请先选择 Provider，再确认文件正文发送范围。</p>
              )}
              <label>
                <input
                  aria-label="同意发送所选文件正文"
                  checked={fileAccessConfirmed}
                  disabled={
                    busy || !selectedProvider || Boolean(fileIdentityProblem)
                  }
                  onChange={(event) => {
                    setFileAccessProviderConfirmation(
                      event.currentTarget.checked
                        ? selectedProviderConfirmation
                        : null,
                    );
                  }}
                  type="checkbox"
                />
                我同意将已选文件正文发送给当前所选 Provider
              </label>
            </div>
          ) : null}
          {fileStartProblem ? (
            <p className="task-agent-runs-alert" role="status">
              {fileStartProblem}
            </p>
          ) : null}
        </div>
      </details>
      {startProblem ? (
        <p className="task-agent-runs-alert" role="status">
          {startProblem}
        </p>
      ) : !assignments?.active.assignee ? (
        <p className="task-agent-runs-empty">
          点击「启动执行」会先将此任务分派给「{setup.actor?.displayName}
          」，再调用所选模型。
        </p>
      ) : null}
      {providersQuery.isError ? (
        <p role="alert">无法读取执行模型，请刷新重试。</p>
      ) : providersQuery.isPending ? null : usableProviders.length === 0 ? (
        <p role="status">
          暂无已就绪的 OpenAI 兼容模型或远程 Anthropic Messages 模型，请在「设置
          → AI 助手」配置。
        </p>
      ) : null}
      <button
        className="button button-secondary"
        disabled={
          busy ||
          adaptersQuery.isFetching ||
          actorsQuery.isFetching ||
          assignmentsQuery.isFetching ||
          providersQuery.isFetching ||
          runsQuery.isFetching ||
          filesQuery.isFetching
        }
        onClick={() => {
          startMutation.reset();
          retryMutation.reset();
          cancelMutation.reset();
          setFileSelectionError(null);
          void refreshExecutionState();
          void filesQuery.refetch();
        }}
        type="button"
      >
        刷新执行状态
      </button>
      {startMutation.error ? (
        <p className="task-agent-runs-alert" role="alert">
          {executionError(startMutation.error)}
        </p>
      ) : null}
      {cancelMutation.error || retryMutation.error ? (
        <p className="task-agent-runs-alert" role="alert">
          {executionError(cancelMutation.error ?? retryMutation.error)}
        </p>
      ) : null}
      {runsQuery.isError ? (
        <p className="task-agent-runs-alert" role="alert">
          无法读取执行记录，请刷新重试。
        </p>
      ) : runsQuery.isPending ? (
        <p role="status">正在读取执行记录…</p>
      ) : runs.length === 0 ? (
        <p className="task-agent-runs-empty">尚无执行记录。</p>
      ) : (
        <ul className="task-agent-runs-list">
          {runs.map((run) => {
            const multiFileResult = orderedMultiFileResult(run);
            const fileApplyCandidates = agentRunFileApplyCandidates(
              run,
              task.title,
            );
            const fileApplyCandidatesByIndex = new Map(
              fileApplyCandidates.map((candidate) => [
                candidate.index,
                candidate,
              ]),
            );
            const multiFileCount =
              run.outputContract?.type === "files"
                ? run.outputContract.files.length
                : null;
            const isMultiFileResult = multiFileCount !== null;
            return (
              <li
                key={run.id}
                className="task-agent-run-card"
                data-status={run.status}
              >
                {run.restartOfRunId ? (
                  <p>
                    按当前事实重新执行 · 来源 <code>{run.restartOfRunId}</code>
                    （旧记录保留）
                  </p>
                ) : null}
                <div className="task-agent-run-heading">
                  <span
                    className="task-agent-run-status"
                    data-phase={agentRunLifecycle(run).phase}
                    data-status={run.status}
                  >
                    {agentRunLifecycle(run).label}
                  </span>
                  <span className="task-agent-run-meta">
                    第 {run.attempt} 次 · {run.model} ·{" "}
                    {formatRunTime(run.completedAt ?? run.startedAt)}
                  </span>
                  <span className="task-agent-run-actions">
                    <AiIssueHandoffButton
                      content={agentRunIssueHandoff({
                        ...run,
                        taskTitle: task.title,
                      })}
                      disabled={disabled || busy}
                    />
                    <button
                      className="button button-secondary"
                      disabled={disabled || busy}
                      onClick={() =>
                        openAgentRunDrawer(task.id, run.id, returnSession)
                      }
                      type="button"
                    >
                      查看过程
                    </button>
                    {(run.status === "queued" || run.status === "running") &&
                    run.outputDeliveryStatus !== "pending" ? (
                      <button
                        className="button button-secondary"
                        disabled={disabled || busy}
                        onClick={() => cancelMutation.mutate(run.id)}
                        type="button"
                      >
                        取消
                      </button>
                    ) : null}
                    {run.status !== "queued" &&
                    run.status !== "running" &&
                    (![4, 5, 6].includes(run.executionContractVersion ?? 0) ||
                      (run.status !== "succeeded" &&
                        run.outputDeliveryStatus === "not_ready")) ? (
                      <button
                        className="button button-secondary"
                        disabled={
                          disabled ||
                          busy ||
                          Boolean(startProblem) ||
                          assignments?.active.assignee?.actorId !== run.actorId
                        }
                        onClick={() =>
                          [4, 5, 6].includes(run.executionContractVersion ?? 0)
                            ? setReworkRetryId(run.id)
                            : retryMutation.mutate(run)
                        }
                        type="button"
                      >
                        重试
                      </button>
                    ) : null}
                    {canRestartAgentRun(run) ? (
                      <button
                        type="button"
                        className="button button-secondary"
                        disabled={disabled || busy}
                        onClick={() => setRestartRunId(run.id)}
                      >
                        按当前事实重新执行
                      </button>
                    ) : null}
                    {run.resultText ? (
                      <>
                        <button
                          className="button button-secondary"
                          onClick={() =>
                            setExpandedRunId((current) =>
                              current === run.id ? null : run.id,
                            )
                          }
                          type="button"
                        >
                          {expandedRunId === run.id
                            ? "收起产出"
                            : isMultiFileResult
                              ? `查看 ${multiFileCount} 份产出`
                              : "查看产出"}
                        </button>
                        {!isMultiFileResult ? (
                          <button
                            className="button button-secondary"
                            onClick={() => downloadRunResult(task.title, run)}
                            type="button"
                          >
                            下载
                          </button>
                        ) : null}
                      </>
                    ) : null}
                  </span>
                </div>
                {run.status === "running" && run.progress ? (
                  <p
                    className="task-agent-run-progress"
                    role="status"
                    aria-live="polite"
                  >
                    <span>{agentRunProgressLabel(run.progress)}</span>
                    <span aria-hidden="true">
                      {` · 已运行 ${agentRunElapsedLabel(run.progress.elapsedMs)}`}
                    </span>
                  </p>
                ) : null}
                {run.outputDeliveryStatus === "pending" &&
                startProblem !== pendingDeliveryGuidance ? (
                  <p className="task-agent-runs-alert" role="status">
                    {pendingDeliveryGuidance}
                  </p>
                ) : null}
                {run.errorCode ? (
                  <p className="task-agent-run-error">
                    失败码：{run.errorCode}
                    {agentRunFailureReason(run.errorCode) ? (
                      <span> {agentRunFailureReason(run.errorCode)}</span>
                    ) : null}
                  </p>
                ) : null}
                {expandedRunId === run.id && run.resultText ? (
                  multiFileResult ? (
                    <div className="task-agent-run-result-files">
                      {multiFileResult.map((file, index) => (
                        <section
                          className="task-agent-run-result-file"
                          key={file.name}
                        >
                          <header>
                            <span>{file.name}</span>
                            <span>{file.mime}</span>
                            <button
                              className="button button-secondary"
                              onClick={() =>
                                downloadTextFile(
                                  file.name,
                                  file.mime,
                                  file.content,
                                )
                              }
                              type="button"
                            >
                              下载
                            </button>
                            {fileApplyCandidatesByIndex.get(index) ? (
                              <button
                                className="button button-secondary"
                                onClick={() =>
                                  prepareAgentRunFileApply(
                                    fileApplyCandidatesByIndex.get(index)!,
                                  )
                                }
                                type="button"
                              >
                                用于项目文件
                              </button>
                            ) : null}
                          </header>
                          <pre className="task-agent-run-result">
                            {file.content}
                          </pre>
                        </section>
                      ))}
                    </div>
                  ) : isMultiFileResult ? (
                    <p className="task-agent-run-error" role="alert">
                      产出载荷不符合冻结的多文件契约，已阻止展示。
                    </p>
                  ) : fileApplyCandidates[0] ? (
                    <section className="task-agent-run-result-file">
                      <header>
                        <span>{fileApplyCandidates[0].name}</span>
                        <span>{fileApplyCandidates[0].mime}</span>
                        <button
                          className="button button-secondary"
                          onClick={() =>
                            prepareAgentRunFileApply(fileApplyCandidates[0])
                          }
                          type="button"
                        >
                          用于项目文件
                        </button>
                      </header>
                      <pre className="task-agent-run-result">
                        {run.resultText}
                      </pre>
                    </section>
                  ) : (
                    <pre className="task-agent-run-result">
                      {run.resultText}
                    </pre>
                  )
                ) : null}
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
