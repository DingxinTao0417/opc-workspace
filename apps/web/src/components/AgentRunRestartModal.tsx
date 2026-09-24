import { useMutation, useQuery } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import {
  getAgentRun,
  getAiProviders,
  getTaskAgentRunFiles,
  createAgentRun,
  agentRunFileOutputPresets,
} from "../api/client";
import { useTaskQuery, useTaskAssignmentsQuery } from "../api/hooks";
import {
  getAgentRunRestartPreview,
  type AgentRunRestartOptions,
  type AgentRunRestartPreview,
} from "../api/agentRunRestart";
import type {
  AgentRun,
  AgentRunOutputContract,
  AgentRunFileCandidate,
} from "../types/models";
import { ProjectTaskFileSelect } from "./ProjectTaskFileSelect";
import { agentFileReference } from "../lib/agentProjectFiles";
import {
  agentRestartErrorMessage,
  canRestartAgentRun,
  sameAgentRestartFacts,
} from "../lib/agentRunRestart";
import { agentReworkErrorMessage } from "../lib/agentRunRework";
import { TaskAgentRework, type AgentReworkApproval } from "./TaskAgentRework";
import { AiAgentRunStartDetails } from "./AiAgentRunStartDetails";
import { Modal } from "./Modal";
import {
  supportsAgentRunProvider,
  agentRunProviderDisclosure,
} from "../lib/agentRunProvider";

export function AgentRunRestartModal({
  taskId,
  runId,
  onClose,
  onSuccess,
}: {
  taskId: string;
  runId: string;
  onClose: () => void;
  onSuccess: (run: AgentRun) => void;
}) {
  const taskQuery = useTaskQuery(taskId);
  const assignments = useTaskAssignmentsQuery(taskId);
  const source = useQuery({
    queryKey: ["agent-restart-source", taskId, runId],
    queryFn: ({ signal }) => getAgentRun(runId, signal),
    retry: false,
    staleTime: 0,
  });
  const providers = useQuery({
    queryKey: ["ai-providers-for-agent"],
    queryFn: getAiProviders,
    retry: false,
    staleTime: 0,
  });
  const fileQuery = useQuery({
    queryKey: ["task-agent-run-files", taskId],
    queryFn: ({ signal }) => getTaskAgentRunFiles(taskId, signal),
    retry: false,
    staleTime: 0,
  });
  const [providerId, setProviderId] = useState("");
  const [fileIds, setFileIds] = useState<string[]>([]);
  const [projectFiles, setProjectFiles] = useState<AgentRunFileCandidate[]>([]);
  const [projectFileConsent, setProjectFileConsent] = useState(false);
  const [outputType, setOutputType] = useState("text");
  const [outputIds, setOutputIds] = useState<number[]>([]);
  const [reworkActive, setReworkActive] = useState(false);
  const [reworkApproval, setReworkApproval] =
    useState<AgentReworkApproval | null>(null);
  const [preview, setPreview] = useState<AgentRunRestartPreview | null>(null);
  const [executionConsent, setExecutionConsent] = useState(false);
  const [restartConsent, setRestartConsent] = useState(false);
  const [fileConsent, setFileConsent] = useState(false);
  const [reworkConsent, setReworkConsent] = useState(false);
  const task = taskQuery.data;
  const provider = providers.data?.find((item) => item.id === providerId);
  const eligibleProviders =
    providers.data?.filter(
      (p) =>
        supportsAgentRunProvider(p) &&
        p.status === "ready" &&
        p.health_status === "healthy" &&
        Number.isSafeInteger(p.version) &&
        Number.isSafeInteger(p.configVersion) &&
        p.version > 0 &&
        Number(p.configVersion) > 0,
    ) ?? [];
  const ordinaryFiles = (fileQuery.data?.items ?? []).filter((item) =>
    fileIds.includes(item.id),
  );
  const selectedFiles = [...ordinaryFiles, ...projectFiles];
  const output: AgentRunOutputContract =
    outputType === "text"
      ? { type: "text" }
      : outputType === "files"
        ? {
            type: "files",
            files: outputIds
              .map((i) => agentRunFileOutputPresets[i])
              .filter(Boolean)
              .map((p) => ({ name: p.name, mime: p.mime })),
          }
        : {
            type: "file",
            name: agentRunFileOutputPresets[Number(outputType)]?.name ?? "",
            mime: agentRunFileOutputPresets[Number(outputType)]?.mime ?? "",
          };
  const options: AgentRunRestartOptions = {
    restart: { runId, expectedTaskVersion: task?.version ?? 0 },
    ...(selectedFiles.length
      ? {
          inputFiles: selectedFiles.map(agentFileReference),
        }
      : {}),
    outputContract: output,
    ...(reworkActive && reworkApproval
      ? { rework: reworkApproval.request }
      : {}),
  };
  const fetching =
    taskQuery.isFetching ||
    source.isFetching ||
    providers.isFetching ||
    (fileIds.length > 0 && fileQuery.isFetching) ||
    assignments.isFetching;
  const identity = JSON.stringify({
    taskId,
    runId,
    task,
    source: source.data,
    provider,
    files: selectedFiles,
    fileIds,
    output,
    reworkActive,
    reworkApproval,
  });
  const epoch = useRef(0);
  const abort = useRef<AbortController | null>(null);
  function clearApproval() {
    setProjectFileConsent(false);
    setPreview(null);
    setExecutionConsent(false);
    setRestartConsent(false);
    setFileConsent(false);
    setReworkConsent(false);
  }
  useEffect(() => {
    epoch.current += 1;
    clearApproval();
    abort.current?.abort();
    return () => {
      epoch.current += 1;
      abort.current?.abort();
    };
  }, [identity]);
  useEffect(() => {
    if (fetching) {
      epoch.current += 1;
      abort.current?.abort();
      clearApproval();
    }
  }, [fetching]);
  useEffect(() => {
    setProviderId("");
    setFileIds([]);
    setProjectFiles([]);
    setOutputType("text");
    setOutputIds([]);
    setReworkActive(false);
    setReworkApproval(null);
  }, [taskId, runId]);
  const readError =
    taskQuery.error ??
    source.error ??
    providers.error ??
    (fileIds.length > 0 ? fileQuery.error : null) ??
    assignments.error;
  const valid =
    !!task &&
    task.id === taskId &&
    !!source.data &&
    source.data.id === runId &&
    source.data.taskId === taskId &&
    canRestartAgentRun(source.data) &&
    !!provider &&
    eligibleProviders.some((p) => p.id === providerId) &&
    !fetching &&
    !readError &&
    (task.status === "todo" || task.status === "in_progress") &&
    !!assignments.data?.pages[0]?.active.assignee &&
    ordinaryFiles.length === fileIds.length &&
    projectFiles.every(
      (file) => file.sourceTask?.project_id === task.projectId,
    ) &&
    selectedFiles.every((f) => f.eligible) &&
    selectedFiles.length <= 4 &&
    selectedFiles.reduce((n, f) => n + f.sizeBytes, 0) <= 131072 &&
    (output.type !== "files" ||
      (output.files.length >= 2 && output.files.length <= 4)) &&
    (!reworkActive || !!reworkApproval);
  const prepare = useMutation({
    mutationFn: async () => {
      if (!valid)
        throw new Error("请先核对当前任务分派、模型和明确选择的资料。");
      const owner = epoch.current;
      const controller = new AbortController();
      abort.current?.abort();
      abort.current = controller;
      clearApproval();
      const next = await getAgentRunRestartPreview(
        taskId,
        providerId,
        options,
        controller.signal,
      );
      if (owner !== epoch.current || controller.signal.aborted)
        throw new Error("来源或选择已变化，请重新预览。");
      setPreview(next);
    },
  });
  const create = useMutation({
    mutationFn: async () => {
      if (
        !valid ||
        !preview ||
        !executionConsent ||
        !restartConsent ||
        (selectedFiles.length > 0 && !fileConsent) ||
        (projectFiles.length > 0 && !projectFileConsent) ||
        (reworkActive && !reworkConsent)
      )
        throw new Error("请核对完整当前事实并完成各项独立确认。");
      const owner = epoch.current;
      const controller = new AbortController();
      abort.current?.abort();
      abort.current = controller;
      const latest = await getAgentRunRestartPreview(
        taskId,
        providerId,
        options,
        controller.signal,
      );
      if (owner !== epoch.current || controller.signal.aborted)
        throw new Error("预览已关闭或来源已切换，未启动执行。");
      if (!sameAgentRestartFacts(latest, preview)) {
        clearApproval();
        throw new Error("当前事实已变化，请重新完整预览并确认；本次未启动。");
      }
      const p = preview.current_start.provider;
      const confirmation = {
        version: p.version,
        configVersion: p.config_version,
        kind: p.kind,
      };
      const run = await createAgentRun(taskId, providerId, {
        ...options,
        confirmRestart: true,
        restartPreviewHash: preview.fingerprint,
        ...(projectFiles.length ? { confirmProjectTaskFiles: true } : {}),
        ...(selectedFiles.length
          ? {
              confirmFileAccess: true,
              fileAccessProviderConfirmation: confirmation,
            }
          : {}),
        ...(reworkActive
          ? {
              confirmReworkContext: true,
              reworkProviderConfirmation: confirmation,
            }
          : {}),
      });
      return { run, owner };
    },
    onSuccess: ({ run, owner }) => {
      if (owner === epoch.current) onSuccess(run);
    },
    onError: () => clearApproval(),
  });
  const busy = prepare.isPending || create.isPending;
  const error = prepare.error ?? create.error;
  const hasAssignee = !!assignments.data?.pages[0]?.active.assignee;
  return (
    <Modal
      open
      title="按当前事实重新执行"
      width="760px"
      dismissible={!busy}
      onClose={() => {
        if (!busy) onClose();
      }}
      footer={
        <>
          <button
            type="button"
            className="button button-secondary"
            disabled={busy}
            onClick={onClose}
          >
            取消
          </button>
          <button
            type="button"
            className="button button-primary"
            disabled={
              busy ||
              !valid ||
              !preview ||
              !executionConsent ||
              !restartConsent ||
              (selectedFiles.length > 0 && !fileConsent) ||
              (projectFiles.length > 0 && !projectFileConsent) ||
              (reworkActive && !reworkConsent)
            }
            onClick={() => create.mutate()}
          >
            {create.isPending ? "正在核验并启动…" : "确认按当前事实重新执行"}
          </button>
        </>
      }
    >
      <p>
        此入口创建关联原运行的新执行，不是原样重试或产出恢复。只发送当前任务及你本次明确选择的资料；不会覆盖旧结果，也不会自动验收。
      </p>
      {readError ? (
        <p role="alert">
          无法读取当前执行条件，请关闭后重新打开；不会启动模型。
        </p>
      ) : null}
      {!hasAssignee && !assignments.isPending ? (
        <p role="alert">
          请先在任务责任中完成 Agent 分派；此入口不会自动分派。
        </p>
      ) : null}
      {source.data && !canRestartAgentRun(source.data) ? (
        <p role="alert">
          原运行不再符合新执行条件。待登记结果只能恢复登记，已提交结果请进入人工验收。
        </p>
      ) : null}
      <label>
        当前执行模型
        <select
          aria-label="新执行模型"
          value={providerId}
          disabled={busy || fetching}
          onChange={(e) => setProviderId(e.target.value)}
        >
          <option value="">请明确选择模型…</option>
          {eligibleProviders.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}（{p.kind === "remote" ? "在线 · 内容离机" : "本地"} ·{" "}
              {p.model}）
            </option>
          ))}
        </select>
      </label>
      {provider ? (
        <p role="note">{agentRunProviderDisclosure(provider)}</p>
      ) : null}
      <ProjectTaskFileSelect
        taskId={taskId}
        projectId={task?.projectId ?? null}
        selected={projectFiles}
        onChange={setProjectFiles}
        disabled={busy}
        otherCount={ordinaryFiles.length}
        otherBytes={ordinaryFiles.reduce(
          (sum, file) => sum + file.sizeBytes,
          0,
        )}
      />
      <fieldset disabled={busy || fetching}>
        <legend>本次受控输入文件（不继承旧选择）</legend>
        {fileQuery.isError ? (
          <p role="alert">
            受控文件列表读取失败。未选择文件时仍可创建纯文本执行；已选文件不能发送。
            <button
              type="button"
              disabled={busy || fileQuery.isFetching}
              onClick={() => void fileQuery.refetch()}
            >
              重读文件列表
            </button>
          </p>
        ) : null}
        {(fileQuery.data?.items ?? []).map((f) => (
          <label key={`${f.sourceKind}:${f.id}`}>
            <input
              type="checkbox"
              checked={fileIds.includes(f.id)}
              disabled={
                !f.eligible ||
                fileQuery.isError ||
                fileQuery.isFetching ||
                (!fileIds.includes(f.id) && fileIds.length >= 4)
              }
              onChange={(e) =>
                setFileIds((ids) =>
                  e.target.checked
                    ? [...ids, f.id]
                    : ids.filter((id) => id !== f.id),
                )
              }
            />
            {f.name} · {f.sizeBytes} 字节 · {f.sourceKind}{" "}
            {!f.eligible ? "不可用" : ""}
          </label>
        ))}
        {!fileQuery.isPending &&
        !fileQuery.isError &&
        !fileQuery.data?.items.length ? (
          <p>当前没有可选择的文件。</p>
        ) : null}
      </fieldset>
      {fileIds.length > 0 ? (
        <button
          type="button"
          className="button button-secondary"
          disabled={busy}
          onClick={() => setFileIds([])}
        >
          清空本次文件选择
        </button>
      ) : null}
      <label>
        本次输出
        <select
          aria-label="新执行输出契约"
          disabled={busy}
          value={outputType}
          onChange={(e) => setOutputType(e.target.value)}
        >
          <option value="text">行内文本</option>
          {agentRunFileOutputPresets.map((p, i) => (
            <option key={p.name} value={i}>
              单文件 · {p.name}
            </option>
          ))}
          <option value="files">多个受控文件（2–4 个）</option>
        </select>
      </label>
      {outputType === "files" ? (
        <fieldset disabled={busy}>
          <legend>明确选择本次输出文件</legend>
          {agentRunFileOutputPresets.map((p, i) => (
            <label key={p.name}>
              <input
                type="checkbox"
                checked={outputIds.includes(i)}
                disabled={!outputIds.includes(i) && outputIds.length >= 4}
                onChange={(e) =>
                  setOutputIds((ids) =>
                    e.target.checked ? [...ids, i] : ids.filter((n) => n !== i),
                  )
                }
              />
              {p.name}
            </label>
          ))}
        </fieldset>
      ) : null}
      <label>
        <input
          type="checkbox"
          checked={reworkActive}
          disabled={busy}
          onChange={(e) => {
            setReworkActive(e.target.checked);
            setReworkApproval(null);
          }}
        />
        本次明确选择当前退回批次资料（可只发退回意见）
      </label>
      {reworkActive && task ? (
        <TaskAgentRework
          task={task}
          provider={provider}
          disabled={busy}
          hasAssignee={hasAssignee}
          onChange={setReworkApproval}
        />
      ) : null}
      <button
        type="button"
        className="button button-secondary"
        disabled={busy || !valid}
        onClick={() => prepare.mutate()}
      >
        {prepare.isPending ? "正在读取完整当前事实…" : "预览完整当前事实"}
      </button>
      {error ? (
        <p role="alert">
          {agentRestartErrorMessage(error) ??
            agentReworkErrorMessage(error) ??
            (error instanceof Error ? error.message : "操作失败，请重新预览。")}
        </p>
      ) : null}
      {preview ? (
        <>
          <AiAgentRunStartDetails preview={preview.current_start} />
          <label>
            <input
              type="checkbox"
              checked={executionConsent}
              disabled={busy}
              onChange={(e) => setExecutionConsent(e.target.checked)}
            />
            我已核对完整当前任务、责任、模型、资料及运行限制，同意启动一次执行并人工检查结果
          </label>
          <label>
            <input
              type="checkbox"
              checked={restartConsent}
              disabled={busy}
              onChange={(e) => setRestartConsent(e.target.checked)}
            />
            我确认关联此原运行，按当前事实创建新执行，不覆盖旧结果，也不自动验收
          </label>
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
              我另行确认跨任务文件来自同项目当前已验收批次，同意用于此新执行；这不代替文件访问或新任务验收
            </label>
          ) : null}
          {selectedFiles.length ? (
            <label>
              <input
                type="checkbox"
                checked={fileConsent}
                disabled={busy}
                onChange={(e) => setFileConsent(e.target.checked)}
              />
              我单独同意读取所列精确文件正文并发送给当前{" "}
              {preview.current_start.provider.kind === "remote"
                ? "远程 Provider（离机且不可撤回）"
                : "本地 Provider"}
            </label>
          ) : null}
          {reworkActive ? (
            <label>
              <input
                type="checkbox"
                checked={reworkConsent}
                disabled={busy}
                onChange={(e) => setReworkConsent(e.target.checked)}
              />
              我单独同意发送以上完整返工意见与所选旧稿，
              {preview.current_start.provider.kind === "remote"
                ? "内容将离开本机且无法撤回"
                : "内容留在本机"}
              ；这不代替文件许可
            </label>
          ) : null}
        </>
      ) : null}
    </Modal>
  );
}
