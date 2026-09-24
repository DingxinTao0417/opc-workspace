import { useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { getTaskSubmissionLocation } from "../api/taskSubmissionLocation";
import { getAgentRunReworkPreview } from "../api/agentRunRework";
import type {
  AgentRunFileAccessProviderConfirmation,
  AgentRunReworkContext,
  AgentRunReworkRequest,
  AiProvider,
  Task,
} from "../types/models";
import { AgentRunReworkDetails } from "./AgentRunReworkDetails";
import { agentReworkErrorMessage } from "../lib/agentRunRework";

export interface AgentReworkApproval {
  request: AgentRunReworkRequest;
  context: AgentRunReworkContext;
  providerId: string;
  providerConfirmation: AgentRunFileAccessProviderConfirmation;
}

export function TaskAgentRework({
  task,
  provider,
  disabled,
  hasAssignee,
  onChange,
}: {
  task: Task;
  provider?: AiProvider;
  disabled: boolean;
  hasAssignee: boolean;
  onChange: (value: AgentReworkApproval | null) => void;
}) {
  const [selectedSubmission, setSelectedSubmission] = useState("");
  const [artifactIds, setArtifactIds] = useState<string[]>([]);
  const [requested, setRequested] = useState(false);
  const [consentIdentity, setConsentIdentity] = useState<string | null>(null);
  const source = useQuery({
    queryKey: [
      "agent-rework-source",
      task.id,
      task.currentSubmissionId,
      task.version,
    ],
    queryFn: ({ signal }) =>
      getTaskSubmissionLocation(task.id, task.currentSubmissionId!, signal),
    enabled: !!task.currentSubmissionId,
    retry: false,
    staleTime: 0,
  });
  const submission = source.data?.submission;
  const available =
    !!submission &&
    source.data?.task.currentSubmissionId === submission.id &&
    source.data.task.version === task.version &&
    task.reviewPolicy === "manual" &&
    submission.status === "changes_requested" &&
    source.data.task.status === "in_progress" &&
    !!submission.reviewReason?.trim();
  const request: AgentRunReworkRequest = {
    submissionId: selectedSubmission,
    artifactIds,
    expectedTaskVersion: task.version,
  };
  const preview = useQuery({
    queryKey: [
      "agent-rework-preview",
      task.id,
      task.version,
      selectedSubmission,
      artifactIds,
    ],
    queryFn: ({ signal }) => getAgentRunReworkPreview(task.id, request, signal),
    enabled: requested && available && selectedSubmission === submission?.id,
    retry: false,
    staleTime: 0,
  });
  const providerConfirmation =
    provider &&
    Number.isSafeInteger(provider.configVersion) &&
    Number(provider.configVersion) > 0
      ? {
          version: provider.version,
          configVersion: provider.configVersion!,
          kind: provider.kind,
        }
      : null;
  const identity = JSON.stringify({
    task: task.id,
    request,
    context: requested ? preview.data : null,
    providerId: provider?.id,
    providerProtocol: provider?.protocol,
    providerModel: provider?.model,
    providerConfirmation,
  });
  const confirmed = consentIdentity === identity;
  useEffect(() => {
    setConsentIdentity(null);
  }, [
    task.id,
    task.version,
    provider?.id,
    provider?.version,
    provider?.configVersion,
    provider?.kind,
    provider?.protocol,
    provider?.model,
  ]);
  useEffect(() => {
    if (source.isFetching) setConsentIdentity(null);
  }, [source.isFetching]);
  useEffect(() => {
    setConsentIdentity(null);
  }, [source.dataUpdatedAt]);
  useEffect(() => {
    if (
      !available ||
      source.isFetching ||
      source.isError ||
      !requested ||
      preview.isFetching ||
      preview.isError ||
      !preview.data ||
      !provider ||
      !providerConfirmation ||
      !confirmed ||
      !hasAssignee
    ) {
      onChange(null);
      return;
    }
    onChange({
      request,
      context: preview.data,
      providerId: provider.id,
      providerConfirmation,
    });
    // Changes to the encoded request, exact preview or Provider invalidate consent.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    identity,
    available,
    source.isFetching,
    source.isError,
    requested,
    preview.isFetching,
    preview.isError,
    confirmed,
    hasAssignee,
    onChange,
  ]);
  useEffect(() => () => onChange(null), [onChange]);
  const candidates = available
    ? submission.artifacts.filter(
        (item) => item.deletedAt === null && item.storageKind !== "file",
      )
    : [];
  return (
    <section aria-label="选择返工来源" className="task-agent-files-body">
      <p>
        只可选择当前已退回批次，不沿用旧 Run
        输入；本次可以只发返工意见，或明确选择最多 4
        项旧文本、链接、结构化产出。文件输入仍需在独立区域另选另确认。
      </p>
      {!hasAssignee ? (
        <p role="alert">
          请先在任务责任中完成 Agent 分派；返工启动不会自动分派。
        </p>
      ) : null}
      {source.isFetching ? (
        <p role="status">正在核对当前退回批次…</p>
      ) : source.isError ? (
        <p role="alert">无法读取当前提交，请刷新重试。</p>
      ) : !available ? (
        <p role="status">
          当前没有与任务版本匹配的已退回批次，不能准备返工执行。
        </p>
      ) : (
        <>
          <label>
            返工来源批次
            <select
              aria-label="选择返工批次"
              disabled={disabled}
              value={selectedSubmission}
              onChange={(event) => {
                setSelectedSubmission(event.target.value);
                setArtifactIds([]);
                setRequested(false);
                setConsentIdentity(null);
              }}
            >
              <option value="">请选择当前退回批次…</option>
              <option value={submission.id}>
                第 {submission.sequence} 次提交 · 已要求返工
              </option>
            </select>
          </label>
          {selectedSubmission === submission.id ? (
            <fieldset disabled={disabled}>
              <legend>选择携带的旧产出（最多 4 项，可不选）</legend>
              {candidates.length === 0 ? (
                <p>无可选的未删除非文件产出，可仅携带意见。</p>
              ) : (
                candidates.map((item) => (
                  <label key={item.id}>
                    <input
                      type="checkbox"
                      aria-label={`携带旧产出 ${item.name}`}
                      checked={artifactIds.includes(item.id)}
                      disabled={
                        !artifactIds.includes(item.id) &&
                        artifactIds.length >= 4
                      }
                      onChange={(event) => {
                        const selected = new Set(artifactIds);
                        if (event.target.checked) selected.add(item.id);
                        else selected.delete(item.id);
                        setArtifactIds(
                          candidates
                            .filter((candidate) => selected.has(candidate.id))
                            .map((candidate) => candidate.id),
                        );
                        setRequested(false);
                        setConsentIdentity(null);
                      }}
                    />
                    {item.name} · {item.storageKind}
                  </label>
                ))
              )}
            </fieldset>
          ) : null}
          <button
            type="button"
            className="button button-secondary"
            disabled={disabled || !selectedSubmission || preview.isFetching}
            onClick={() => {
              setConsentIdentity(null);
              if (requested) void preview.refetch();
              else setRequested(true);
            }}
          >
            预览完整返工上下文
          </button>
        </>
      )}
      {requested && preview.isFetching ? (
        <p role="status">正在读取完整原文并核验返工来源…</p>
      ) : null}
      {requested && preview.isError ? (
        <p role="alert">
          返工预览失败：
          {agentReworkErrorMessage(preview.error) ??
            "请刷新批次并重新选择旧产出后重试"}
          ；不会发送任何返工上下文。
        </p>
      ) : null}
      {requested && !preview.isFetching && !preview.isError && preview.data ? (
        <>
          <AgentRunReworkDetails context={preview.data} />
          <p role="note">
            {provider
              ? provider.kind === "remote"
                ? `将发送给远程 Provider「${provider.name}」；上述返工意见及所选旧稿会离开本机，发送后无法撤回。`
                : `将发送给本地 Provider「${provider.name}」，上述内容留在本机。`
              : "请先选择执行 Provider，再核对发送范围。"}
          </p>
          <label>
            <input
              type="checkbox"
              aria-label="同意发送返工意见与旧产出"
              checked={confirmed}
              disabled={
                disabled ||
                source.isFetching ||
                source.isError ||
                !providerConfirmation ||
                !available ||
                !hasAssignee
              }
              onChange={(event) =>
                setConsentIdentity(event.target.checked ? identity : null)
              }
            />
            我已完整核对上方意见与所选原文，同意本次发送给当前
            Provider；成功后仍需人工验收
          </label>
        </>
      ) : null}
      <button
        type="button"
        className="button button-secondary"
        disabled={disabled || source.isFetching}
        onClick={() => {
          setConsentIdentity(null);
          setRequested(false);
          void source.refetch();
        }}
      >
        刷新退回批次
      </button>
    </section>
  );
}
