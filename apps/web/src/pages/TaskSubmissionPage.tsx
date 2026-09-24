import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { ApiError, downloadTaskArtifact } from "../api/client";
import {
  taskArtifactDetailQueryKey,
  taskSubmissionQueryKey,
} from "../api/hooks";
import { getTaskSubmissionLocation } from "../api/taskSubmissionLocation";
import { AiIssueHandoffButton } from "../components/AiWorkbenchHandoff";
import { ReturnToAiChat } from "../components/ClientRecordLocation";
import { ErrorState, LoadingState } from "../components/feedback";
import { PageHeader } from "../components/PageHeader";
import { TaskArtifactCard } from "../components/TaskArtifactCard";
import { TaskSubmissionSummary } from "../components/TaskOutputsSection";
import { taskSubmissionHandoff } from "../lib/aiIssueHandoff";
import { aiWorkspaceHref } from "../lib/aiWorkspaceLinks";
import { validTaskSubmissionLocation } from "../lib/taskSubmissionLocation";
import type { TaskArtifactSummary } from "../types/models";

const taskStatuses = {
  todo: "待办",
  in_progress: "进行中",
  blocked: "已阻塞",
  waiting_review: "待验收",
  done: "已完成",
  cancelled: "已取消",
};

export function TaskSubmissionPage() {
  const { taskId, submissionId } = useParams();
  return (
    <SubmissionPage
      key={`${taskId}/${submissionId}`}
      taskId={taskId}
      submissionId={submissionId}
    />
  );
}

function SubmissionPage({
  taskId,
  submissionId,
}: {
  taskId?: string;
  submissionId?: string;
}) {
  const [params] = useSearchParams();
  const valid = validTaskSubmissionLocation(taskId, submissionId, params);
  const returnSession = valid ? params.get("return_session") : null;
  const focusedArtifactId = valid ? params.get("artifact") : null;
  const client = useQueryClient();
  const [expanded, setExpanded] = useState<string | null>(focusedArtifactId);
  useEffect(() => setExpanded(focusedArtifactId), [focusedArtifactId]);
  const [downloading, setDownloading] = useState<string | null>(null);
  const [downloadMessage, setDownloadMessage] = useState<string | null>(null);
  const download = useRef<AbortController | null>(null);
  useEffect(() => () => download.current?.abort(), []);
  const query = useQuery({
    queryKey: [
      ...taskSubmissionQueryKey(taskId ?? "invalid"),
      "detail",
      submissionId,
    ],
    queryFn: ({ signal }) =>
      getTaskSubmissionLocation(taskId!, submissionId!, signal),
    enabled: valid,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: "always",
  });
  // Never display stale evidence while refreshing, after an error or for a bad URL.
  const data =
    valid && !query.isFetching && !query.isError ? query.data : undefined;
  async function downloadFile(artifact: TaskArtifactSummary) {
    if (download.current || !data || artifact.deletedAt) return;
    const controller = new AbortController();
    download.current = controller;
    setDownloading(artifact.id);
    setDownloadMessage(null);
    try {
      const file = await downloadTaskArtifact(
        artifact.id,
        artifact.name,
        controller.signal,
      );
      if (controller.signal.aborted) return;
      const url = URL.createObjectURL(file.blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = file.fileName;
      anchor.rel = "noopener";
      document.body.appendChild(anchor);
      anchor.click();
      anchor.remove();
      window.setTimeout(() => URL.revokeObjectURL(url), 0);
      setDownloadMessage(
        "文件已交给浏览器下载，请自行检查内容；下载不等于验收通过。",
      );
    } catch (error) {
      if (!controller.signal.aborted)
        setDownloadMessage(
          error instanceof ApiError ? error.message : "文件下载失败，请重试。",
        );
    } finally {
      if (!controller.signal.aborted) {
        download.current = null;
        setDownloading(null);
        await Promise.all([
          client.invalidateQueries({
            queryKey: taskSubmissionQueryKey(taskId!),
          }),
          client.invalidateQueries({
            queryKey: taskArtifactDetailQueryKey(artifact.id),
          }),
        ]);
      }
    }
  }
  return (
    <div className="page">
      <PageHeader
        title="任务提交批次"
        actions={<ReturnToAiChat sessionId={returnSession} />}
      />
      {!valid ? (
        <p role="alert">提交批次链接无效。</p>
      ) : query.isPending || query.isFetching ? (
        <LoadingState label="正在读取指定提交批次…" />
      ) : !data ? (
        <ErrorState
          title="提交批次不可用"
          message="该批次可能已不存在、不属于当前任务，或服务暂不可用；不会用新批次或旧缓存替代。"
          onRetry={() => void query.refetch()}
        />
      ) : (
        <section
          className="task-output-section"
          aria-label="定位的提交批次"
          style={{ overflowWrap: "anywhere" }}
        >
          <h2>{data.task.title}</h2>
          <p>
            任务当前状态：{taskStatuses[data.task.status]} · 版本{" "}
            {data.task.version}
          </p>
          <p>
            {data.task.currentSubmissionId === data.submission.id
              ? "这是任务当前指向的批次；当前指针不等于待验收或已通过。"
              : "这是历史提交批次，不会替换为任务当前批次。"}
          </p>
          <p>批次 ID：{data.submission.id}</p>
          <TaskSubmissionSummary submission={data.submission} />
          {data.submission.reviewedAt && (
            <p>
              验收记录时间：{data.submission.reviewedAt} ·{" "}
              {data.submission.reviewedByActor?.displayName ?? "所有者"}
            </p>
          )}
          {data.submission.withdrawnAt && (
            <p>撤回时间：{data.submission.withdrawnAt}</p>
          )}
          <p>
            此处是本地只读核验。查看或下载不会提交、验收，也不会把产出内容发送给智能体。文件完整性校验不代表内容正确。
          </p>
          <div className="task-artifact-list">
            {focusedArtifactId &&
              !data.submission.artifacts.some(
                (artifact) =>
                  artifact.id === focusedArtifactId && !artifact.deletedAt,
              ) && (
                <p role="alert">
                  指定产出已不存在、不属于此批次或已删除；不会改为打开其他产出。
                </p>
              )}
            {data.submission.artifacts.map((artifact) => (
              <TaskArtifactCard
                key={artifact.id}
                artifact={artifact}
                readOnly
                disabled
                downloadDisabled={!!downloading}
                downloading={downloading === artifact.id}
                expanded={expanded === artifact.id}
                onToggle={(id) => setExpanded(expanded === id ? null : id)}
                onDelete={() => {}}
                onDownload={(art) => void downloadFile(art)}
              />
            ))}
            {!data.submission.artifacts.length && <p>此批次没有附件。</p>}
          </div>
          {downloadMessage && <p role="status">{downloadMessage}</p>}
          <Link
            className="button button-secondary"
            to={aiWorkspaceHref(
              `/tasks/${data.task.id}`,
              returnSession ?? undefined,
            )}
          >
            打开任务详情处理
          </Link>
          <AiIssueHandoffButton
            content={taskSubmissionHandoff(
              data.task.id,
              data.submission.id,
              data.task.title,
              data.submission.sequence,
              data.submission.origin,
            )}
          />
          <button
            className="button button-quiet"
            type="button"
            onClick={() => void query.refetch()}
          >
            刷新当前事实
          </button>
        </section>
      )}
    </div>
  );
}
