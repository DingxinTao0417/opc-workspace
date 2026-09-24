import { useEffect } from "react";
import { Link } from "react-router-dom";
import { useTaskArtifactQuery } from "../api/hooks";
import { taskArtifactPreviewHandoff } from "../lib/aiIssueHandoff";
import { isWorkspaceIdentity } from "../lib/focusReportLocation";
import { revealAgentChat } from "../lib/revealAgentChat";
import { taskArtifactHref } from "../lib/taskSubmissionLocation";
import {
  useWorkspacePanels,
  type WorkspacePanel,
} from "../store/workspacePanels";
import { AiIssueHandoffButton } from "./AiWorkbenchHandoff";

// This panel is a local, human-confirmed read. The server-derived ownership
// identities are checked again against the fresh detail response before any
// body is rendered; neither this body nor a download enters the AI chat.
export function TaskArtifactWorkspacePreview({
  panel,
}: {
  panel: WorkspacePanel;
}) {
  const valid =
    panel.kind === "artifact" &&
    isWorkspaceIdentity(panel.artifactId) &&
    isWorkspaceIdentity(panel.artifactTaskId) &&
    isWorkspaceIdentity(panel.artifactSubmissionId);
  const query = useTaskArtifactQuery(
    valid ? panel.artifactId! : null,
    true,
    true,
  );
  const candidate =
    valid && !query.isFetching && !query.isError ? query.data : undefined;
  const mismatch =
    candidate &&
    (candidate.id !== panel.artifactId ||
      candidate.taskId !== panel.artifactTaskId ||
      candidate.submissionId !== panel.artifactSubmissionId);
  const artifact = !mismatch && !candidate?.deletedAt ? candidate : undefined;

  useEffect(() => {
    if (!artifact) return;
    const panels = useWorkspacePanels.getState();
    const current = panels.tabs.find((tab) => tab.id === panel.id);
    const title = artifact.name.slice(0, 60);
    if (
      current?.kind === "artifact" &&
      current.artifactId === artifact.id &&
      current.artifactTaskId === artifact.taskId &&
      current.artifactSubmissionId === artifact.submissionId &&
      current.title !== title
    )
      panels.update(panel.id, { title });
  }, [artifact, panel.id]);

  const detailHref =
    artifact && panel.artifactTaskId && panel.artifactSubmissionId
      ? `${taskArtifactHref(
          panel.artifactTaskId,
          panel.artifactSubmissionId,
          artifact.id,
        )}${
          isWorkspaceIdentity(panel.returnSession)
            ? `&return_session=${panel.returnSession}`
            : ""
        }`
      : "";
  const handoff = artifact
    ? taskArtifactPreviewHandoff(
        artifact.id,
        artifact.taskId,
        artifact.submissionId,
        artifact.storageKind,
      )
    : null;

  return (
    <section
      className="ws-resource ws-artifact-preview"
      aria-label="任务产出预览"
    >
      <div className="ws-artifact-preview-heading">
        <strong>任务产出 · 本地只读</strong>
        {valid && (
          <button
            className="button button-quiet"
            type="button"
            onClick={() => void query.refetch()}
          >
            刷新事实
          </button>
        )}
      </div>
      <p className="ws-artifact-preview-note">
        预览不会提交或验收，也不会自动把产出内容发给智能体。文件下载需在完整详情页手动操作。
      </p>
      {!valid ? (
        <p role="alert">产出预览身份无效，未读取内容。</p>
      ) : query.isPending || query.isFetching ? (
        <p role="status">正在读取指定产出…</p>
      ) : query.isError ? (
        <p role="alert">产出读取失败，请刷新后重试；不会展示旧缓存。</p>
      ) : mismatch ? (
        <p role="alert">产出与指定任务或提交批次不一致，未展示内容。</p>
      ) : candidate?.deletedAt ? (
        <p role="alert">指定产出已删除，未展示内容。</p>
      ) : !artifact ? (
        <p role="alert">指定产出不可用，未展示内容。</p>
      ) : (
        <>
          <h2>{artifact.name}</h2>
          <p>
            {artifact.storageKind === "file"
              ? "受控文件"
              : artifact.storageKind === "structured"
                ? "结构化产出"
                : artifact.storageKind === "link"
                  ? "链接产出"
                  : "文本产出"}
            {artifact.sizeBytes !== null ? ` · ${artifact.sizeBytes} 字节` : ""}
          </p>
          {artifact.storageKind === "text" && (
            <pre className="ws-artifact-preview-body">
              {artifact.contentText}
            </pre>
          )}
          {artifact.storageKind === "structured" && (
            <pre className="ws-artifact-preview-body">
              {JSON.stringify(artifact.structuredJson, null, 2)}
            </pre>
          )}
          {artifact.storageKind === "link" && (
            <pre className="ws-artifact-preview-body">
              {artifact.referenceUrl}
            </pre>
          )}
          {artifact.storageKind === "file" && (
            <dl className="ws-artifact-preview-metadata">
              <div>
                <dt>MIME</dt>
                <dd>{artifact.mimeType ?? "未知"}</dd>
              </div>
              <div>
                <dt>SHA-256</dt>
                <dd>{artifact.sha256 ?? "未提供"}</dd>
              </div>
              <div>
                <dt>完整性</dt>
                <dd>{artifact.integrityStatus}</dd>
              </div>
            </dl>
          )}
          <div className="ws-artifact-preview-actions">
            <AiIssueHandoffButton
              content={handoff}
              label="继续交给智能体"
              stayInCurrentChat
              onNavigate={revealAgentChat}
            />
            <Link className="button button-secondary" to={detailHref}>
              打开完整详情
            </Link>
          </div>
        </>
      )}
    </section>
  );
}
