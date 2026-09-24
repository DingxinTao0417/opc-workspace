import { useEffect, useState } from "react";
import type { WorkspacePanel } from "../store/workspacePanels";
import { useAgentRunFileApply } from "../store/agentRunFileApply";
import { workspaceApi, workspaceError } from "../api/workspace";
import { useProjectFileWriteCapability } from "../lib/projectFileWriteGate";
import { fileReviewDiff, visibleFileLine } from "./aiFileReviewDiff";

export function AgentRunFileApplyReview({
  panel,
  targetIsText,
}: {
  panel: WorkspacePanel;
  targetIsText: boolean;
}) {
  const {
    candidate,
    review,
    loading,
    error,
    stageTarget,
    validate,
    apply,
    clear,
  } = useAgentRunFileApply();
  const capability = useProjectFileWriteCapability();
  const [confirmed, setConfirmed] = useState<unknown>(null);
  const [cancelling, setCancelling] = useState(false);
  const [cancelError, setCancelError] = useState("");
  const ownsReview =
    !!review && review.root.id === panel.root?.id && review.path === panel.path;
  const canWrite =
    ownsReview &&
    (review.status === "ready" || review.status === "checked") &&
    review.expiresAt > Date.now();

  useEffect(() => {
    setConfirmed(null);
  }, [review?.operationId, review?.status]);
  useEffect(() => {
    if (!ownsReview || !review || review.expiresAt <= Date.now()) return;
    const operationId = review.operationId;
    const timer = window.setTimeout(
      () => {
        if (useAgentRunFileApply.getState().review?.operationId === operationId)
          clear("文件审查基线已过期，请重新选择 Agent 产出和目标文件");
      },
      Math.max(0, review.expiresAt - Date.now()),
    );
    return () => window.clearTimeout(timer);
  }, [ownsReview, review, clear]);

  if (!candidate) return null;
  if (!panel.root || !panel.path)
    return (
      <section className="ai-workbench-handoff" aria-label="Agent 产出写入">
        <p role="alert">当前文件标签缺少受信工作目录或相对路径。</p>
        <button
          className="button button-quiet"
          onClick={() => clear()}
          type="button"
        >
          丢弃候选
        </button>
      </section>
    );
  if (review && !ownsReview)
    return (
      <section className="ai-workbench-handoff" aria-label="Agent 产出写入">
        <strong>Agent 文件产出已绑定另一目标</strong>
        <p>
          {review.root.name} / {review.path}
          。一次审查只能处理一个文件；若要改用当前文件，请先丢弃旧审查。
        </p>
        <button
          className="button button-quiet"
          onClick={() => clear()}
          type="button"
        >
          丢弃旧审查
        </button>
      </section>
    );
  if (!ownsReview || !review)
    return (
      <section className="ai-workbench-handoff" aria-label="Agent 产出写入">
        <div className="ai-workbench-handoff-heading">
          <strong>使用 Agent 文件产出</strong>
          <button
            className="button button-quiet"
            onClick={() => clear()}
            type="button"
          >
            丢弃候选
          </button>
        </div>
        <p>
          {candidate.taskTitle} · 第 {candidate.attempt} 次执行 ·{" "}
          {candidate.name}
        </p>
        <p>
          先为当前文件建立完整本机基线，再显示完整替换差异。此动作不会自动写入，也不会把目标路径交给模型。
        </p>
        {error ? <p role="alert">{error}</p> : null}
        <button
          className="button button-secondary"
          disabled={loading || !targetIsText}
          onClick={() => void stageTarget(panel.root!, panel.path!)}
          type="button"
        >
          {loading ? "正在读取完整基线…" : "准备替换当前文件"}
        </button>
        {!targetIsText ? (
          <p role="status">
            当前目标不是可完整读取的文本文件，不能进入替换审查。
          </p>
        ) : null}
      </section>
    );

  return (
    <section className="ai-workbench-handoff" aria-label="Agent 产出写入">
      <div className="ai-workbench-handoff-heading">
        <strong>
          {review.status === "applied"
            ? "Agent 产出 · 已写入"
            : review.status === "verifying_source"
              ? "Agent 产出 · 正在核对来源"
              : review.status === "writing"
                ? "Agent 产出 · 写入中"
                : review.status === "recovery_required" ||
                    review.status === "uncertain"
                  ? "Agent 产出 · 请核对恢复记录"
                  : "Agent 产出 · 尚未写入"}
        </strong>
        <button
          className="button button-quiet"
          onClick={() => clear()}
          type="button"
        >
          {review.status === "writing" ? "关闭（不取消写入）" : "丢弃审查"}
        </button>
      </div>
      <p>
        来源：{candidate.taskTitle} · 第 {candidate.attempt} 次执行 ·{" "}
        {candidate.name}
      </p>
      <p>
        目标：{review.root.name} / {review.path}
      </p>
      <p>
        下面展示完整替换差异；文件产出已登记不等于任务已验收，也不构成写入许可。
      </p>
      <pre
        className="ws-source ai-project-file-source ai-file-review-diff"
        aria-label="Agent 产出完整文件差异"
      >
        {fileReviewDiff(review.snapshot.content, candidate.content).map(
          (part, index) => (
            <span key={index} className={`ai-file-diff-${part.kind}`}>
              {part.lines
                .map(
                  (line) =>
                    `${part.kind === "removed" ? "-" : part.kind === "added" ? "+" : " "} ${visibleFileLine(line)}\n`,
                )
                .join("")}
            </span>
          ),
        )}
      </pre>
      <p>
        原文件 {review.snapshot.size} 字节 → Agent 产出{" "}
        {new TextEncoder().encode(candidate.content).byteLength} 字节
      </p>
      <p>
        确认后仍只处理这一个文件。独立原生窗口会再次显示操作方向，随后 Windows
        才为本次操作请求系统授权；网页确认不等于原生确认、UAC 或写入成功。
      </p>
      {review.error ? <p role="alert">{review.error}</p> : null}
      {review.status === "checked" ? (
        <p role="status">复核通过：此刻目标文件仍与基线一致，尚未写入。</p>
      ) : null}
      {review.status === "verifying_source" ? (
        <p role="status">
          正在重新读取精确执行记录，核对提交身份、契约和完整产出；尚未发起原生写入。
        </p>
      ) : null}
      {review.status === "applied" ? (
        <p role="status">
          写入及完整字节核验成功；恢复记录可在“文件恢复”中查看。
        </p>
      ) : null}
      {review.status === "writing" ? (
        <>
          <p role="status">
            正在等待原生确认、Windows 授权或执行回执；请勿重复操作。
          </p>
          <button
            className="button button-secondary"
            disabled={cancelling}
            onClick={() => {
              setCancelling(true);
              setCancelError("");
              void workspaceApi
                .cancelFileOperation(review.root.id, review.operationId)
                .catch((reason: unknown) =>
                  setCancelError(workspaceError(reason)),
                )
                .finally(() => setCancelling(false));
            }}
            type="button"
          >
            {cancelling ? "正在请求取消…" : "取消本次操作"}
          </button>
          {cancelError ? <p role="alert">{cancelError}</p> : null}
        </>
      ) : null}
      {review.status === "recovery_required" ||
      review.status === "uncertain" ? (
        <p role="alert">
          不要重复写入。请打开“文件恢复”，重新核对当前内容与恢复记录。
        </p>
      ) : null}
      {review.backupPath ? (
        <p>
          恢复记录：<code>{review.backupPath}</code>
        </p>
      ) : null}
      <button
        className="button button-secondary"
        disabled={!canWrite}
        onClick={() => void validate()}
        type="button"
      >
        {review.status === "checking" ? "正在复核…" : "复核当前文件（不写入）"}
      </button>
      {!capability.available ? <p role="status">{capability.reason}</p> : null}
      {canWrite && capability.available ? (
        <>
          <label>
            <input
              checked={confirmed === review}
              onChange={(event) =>
                setConfirmed(event.target.checked ? review : null)
              }
              type="checkbox"
            />
            我已核对完整差异，同意用此 Agent 产出替换当前文件并保留本地恢复记录
          </label>
          <button
            className="button button-primary"
            disabled={confirmed !== review}
            onClick={() => {
              if (confirmed === review) {
                setConfirmed(null);
                void apply();
              }
            }}
            type="button"
          >
            确认写入此文件
          </button>
        </>
      ) : null}
    </section>
  );
}
