import { useMutation, useQuery } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { getAgentRun, getAiProviders, retryAgentRun } from "../api/client";
import type { AgentRun } from "../types/models";
import { AgentRunReworkDetails } from "./AgentRunReworkDetails";
import { Modal } from "./Modal";
import { agentReworkErrorMessage } from "../lib/agentRunRework";
import { ProjectFileSourceDetails } from "./ProjectTaskFileSelect";

export function AgentReworkRetryModal({
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
  const [execution, setExecution] = useState(false);
  const [rework, setRework] = useState(false);
  const [files, setFiles] = useState(false);
  const [projectFiles, setProjectFiles] = useState(false);
  const detail = useQuery({
    queryKey: ["agent-rework-retry-detail", taskId, runId],
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
  const run = detail.data;
  const v6 = run?.executionContractVersion === 6;
  const v5 = run?.executionContractVersion === 5 || v6;
  const provider = providers.data?.find((item) => item.id === run?.providerId);
  const confirmation = v5
    ? run?.executionProviderConfirmation
    : run?.reworkProviderConfirmation;
  const hasRework = !!run?.reworkContext;
  const identity = JSON.stringify({ run, provider });
  const epoch = useRef(0);
  useEffect(() => {
    epoch.current += 1;
    return () => {
      epoch.current += 1;
    };
  }, [taskId, runId, identity]);
  useEffect(() => {
    setExecution(false);
    setRework(false);
    setFiles(false);
    setProjectFiles(false);
  }, [identity]);
  useEffect(() => {
    if (detail.isFetching || providers.isFetching) {
      epoch.current += 1;
      setExecution(false);
      setRework(false);
      setFiles(false);
      setProjectFiles(false);
    }
  }, [detail.isFetching, providers.isFetching]);
  useEffect(() => {
    epoch.current += 1;
    setExecution(false);
    setRework(false);
    setFiles(false);
    setProjectFiles(false);
  }, [detail.dataUpdatedAt, providers.dataUpdatedAt]);
  const inputFiles =
    (v5 ? run?.executionInputFiles : run?.reworkInputFiles) ?? [];
  const hasProjectFiles = inputFiles.some(
    (file) => file.source_kind === "project_task_artifact",
  );
  const valid =
    !!run &&
    run.id === runId &&
    run.taskId === taskId &&
    (v5
      ? ((run.modelProtocol === "anthropic_messages" &&
          run.maxOutputTokens === 8192) ||
          (v6 &&
            run.modelProtocol === "openai_chat" &&
            run.maxOutputTokens === 0)) &&
        Array.isArray(run.executionInputFiles) &&
        v6 === hasProjectFiles
      : run.executionContractVersion === 4 && hasRework) &&
    !!confirmation &&
    ["failed", "cancelled", "interrupted"].includes(run.status) &&
    run.outputDeliveryStatus === "not_ready" &&
    !!provider &&
    provider.status === "ready" &&
    provider.health_status === "healthy" &&
    provider.version === confirmation.version &&
    provider.configVersion === confirmation.configVersion &&
    provider.kind === confirmation.kind;
  const validProtocol = v5
    ? provider?.protocol === run?.modelProtocol &&
      (run?.modelProtocol === "openai_chat" ||
        (provider?.kind === "remote" && confirmation?.kind === "remote"))
    : provider?.protocol === "openai_chat";
  const retry = useMutation({
    mutationFn: async () => {
      if (
        !valid ||
        !validProtocol ||
        !run ||
        !confirmation ||
        !execution ||
        (hasProjectFiles && !projectFiles) ||
        (hasRework && !rework) ||
        (inputFiles.length > 0 && !files)
      )
        throw new Error("请重新核对原执行和独立确认。");
      const owner = epoch.current;
      const current = await getAgentRun(runId);
      if (owner !== epoch.current)
        throw new Error("执行预览已关闭或来源已切换，未发起重试。");
      if (JSON.stringify(current) !== JSON.stringify(run)) {
        setExecution(false);
        setRework(false);
        setFiles(false);
        setProjectFiles(false);
        throw new Error("原执行事实已变化，请重新读取完整预览。");
      }
      const options = {
        ...(hasProjectFiles ? { confirmProjectTaskFiles: true } : {}),
        ...(hasRework
          ? {
              confirmReworkContext: true,
              reworkProviderConfirmation: confirmation,
            }
          : {}),
        ...(inputFiles.length
          ? {
              confirmFileAccess: true,
              fileAccessProviderConfirmation: confirmation,
            }
          : {}),
      };
      return Object.keys(options).length > 0
        ? retryAgentRun(runId, options)
        : retryAgentRun(runId);
    },
    onSuccess,
    onError: () => {
      setExecution(false);
      setRework(false);
      setFiles(false);
      setProjectFiles(false);
    },
  });
  const busy = retry.isPending;
  return (
    <Modal
      open
      title={v5 ? "重试原执行" : "重试原返工执行"}
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
              detail.isFetching ||
              providers.isFetching ||
              detail.isError ||
              providers.isError ||
              !valid ||
              !validProtocol ||
              !execution ||
              (hasProjectFiles && !projectFiles) ||
              (hasRework && !rework) ||
              (inputFiles.length > 0 && !files)
            }
            onClick={() => retry.mutate()}
          >
            {v5 ? "确认重试原执行" : "确认重试原返工执行"}
          </button>
        </>
      }
    >
      {detail.isFetching || providers.isFetching ? (
        <p role="status">正在读取精确执行与 Provider…</p>
      ) : null}
      {!detail.isFetching &&
      !providers.isFetching &&
      (!valid || !validProtocol || detail.isError || providers.isError) ? (
        <p role="alert">
          原执行、返工上下文或 Provider
          不再满足冻结条件；不会换用其他执行或重新创建来源。
        </p>
      ) : null}
      {valid && validProtocol && run ? (
        <>
          <p>
            执行 ID <code>{run.id}</code> · 第 {run.attempt} 次 · {run.model}
          </p>
          {run.reworkContext ? (
            <AgentRunReworkDetails context={run.reworkContext} retry />
          ) : (
            <p>本次没有返工上下文，不会自动带入旧稿或退回意见。</p>
          )}
          {v5 ? (
            <p role="note">
              冻结协议{" "}
              {run.modelProtocol === "anthropic_messages"
                ? "Anthropic Messages"
                : "OpenAI Chat"}{" "}
              · v{run.executionContractVersion} ·{" "}
              {run.maxOutputTokens
                ? `最多 ${run.maxOutputTokens} 输出 tokens`
                : "沿用原 OpenAI 输出预算（未指定 token 上限）"}
              。只接受完整文本结束，截断或拒绝不会登记产出。输出契约：
              {run.outputContract?.type === "files"
                ? run.outputContract.files
                    .map((file) => `${file.name}（${file.mime}）`)
                    .join("；")
                : run.outputContract?.type === "file"
                  ? `${run.outputContract.name}（${run.outputContract.mime}）`
                  : "行内文本"}
              。
            </p>
          ) : null}
          <p role="note">
            {provider?.kind === "remote"
              ? `远程 Provider「${provider.name}」将收到下方同意的上下文，内容会离开本机并可能再次产生费用。`
              : `原本地 Provider「${provider?.name}」将再次执行，内容留在本机。`}
          </p>
          {inputFiles.length > 0 ? (
            <section aria-label="返工重试受控输入文件">
              {inputFiles.map((file) => (
                <p key={file.id}>
                  {file.name} · {file.source_kind} · {file.mime} ·{" "}
                  {file.size_bytes} B · ID <code>{file.id}</code> · SHA-256{" "}
                  <code>{file.sha256}</code>
                  <ProjectFileSourceDetails source={file.source_task} />
                </p>
              ))}
              <label>
                <input
                  type="checkbox"
                  checked={files}
                  disabled={busy}
                  onChange={(event) => setFiles(event.target.checked)}
                />
                我另行同意读取并发送上述冻结输入文件正文，不以返工上下文确认代替文件许可
              </label>
            </section>
          ) : null}
          {hasProjectFiles ? (
            <label>
              <input
                type="checkbox"
                checked={projectFiles}
                disabled={busy}
                onChange={(event) => setProjectFiles(event.target.checked)}
              />
              我另行确认原跨任务文件及已验收来源，用相同冻结资料再次执行；来源变化须重新选择，不以原许可替代本次确认
            </label>
          ) : null}
          {hasRework ? (
            <label>
              <input
                type="checkbox"
                checked={rework}
                disabled={busy}
                onChange={(event) => setRework(event.target.checked)}
              />
              我已核对完整返工意见和旧稿，同意再次发送给原 Provider
            </label>
          ) : null}
          <label>
            <input
              type="checkbox"
              checked={execution}
              disabled={busy}
              onChange={(event) => setExecution(event.target.checked)}
            />
            我同意重新调用模型执行；不自动完成任务或验收
          </label>
        </>
      ) : null}
      {retry.error ? (
        <p role="alert">
          {agentReworkErrorMessage(retry.error) ??
            (retry.error instanceof Error
              ? retry.error.message
              : "重试失败，请重新核对")}
        </p>
      ) : null}
    </Modal>
  );
}
