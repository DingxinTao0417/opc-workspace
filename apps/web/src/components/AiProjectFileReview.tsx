import { useEffect, useState } from "react";
import { useAiProjectFileReview } from "../store/aiProjectFileReview";
import type { AiProvider } from "../types/models";
import { fileReviewDiff, visibleFileLine } from "./aiFileReviewDiff";
import { workspaceApi, workspaceError } from "../api/workspace";
import { useProjectFileWriteCapability } from "../lib/projectFileWriteGate";
import { useWorkspacePanels } from "../store/workspacePanels";
import { useUiStore } from "../store/ui";

export function AiProjectFileReview({
  sessionId,
  provider,
}: {
  sessionId: string;
  provider: AiProvider | null;
}) {
  const { review, error, clear, validate, apply } = useAiProjectFileReview();
  const capability = useProjectFileWriteCapability();
  const [confirmed, setConfirmed] = useState<unknown>(null);
  const [cancelError, setCancelError] = useState("");
  const [cancelling, setCancelling] = useState(false);
  const [workspaceErrorMessage, setWorkspaceErrorMessage] = useState("");
  const canWrite =
    !!review && (review.status === "ready" || review.status === "checked");
  const owned =
    !!review &&
    !!provider &&
    review.approval.sessionId === sessionId &&
    review.approval.providerId === provider.id &&
    review.approval.providerVersion === provider.version;
  useEffect(() => {
    if (review && !owned) clear();
  }, [review, owned, clear]);
  useEffect(() => {
    if (
      !review ||
      !["receiving", "ready", "checking", "checked", "conflict"].includes(
        review.status,
      )
    )
      return;
    const selection = review.selection;
    const timer = window.setTimeout(
      () => {
        if (useAiProjectFileReview.getState().review?.selection === selection)
          clear("文件审查基线已过期，请重新选择文件并生成建议");
      },
      Math.max(0, selection.expiresAt - Date.now()),
    );
    return () => window.clearTimeout(timer);
  }, [review?.selection, review?.status, clear]);
  useEffect(() => () => useAiProjectFileReview.getState().clear(), []);
  if (error)
    return (
      <section className="ai-workbench-handoff" aria-label="文件修改建议">
        <p role="alert">{error}</p>
        <button
          type="button"
          className="button button-quiet"
          onClick={() => clear()}
        >
          关闭文件提示
        </button>
      </section>
    );
  if (!owned || !review?.proposal || !review.targetFile) return null;
  const targetFile = review.targetFile.snapshot;
  return (
    <section className="ai-workbench-handoff" aria-label="文件修改建议">
      <div className="ai-workbench-handoff-heading">
        <strong>
          {review.status === "applied"
            ? "文件修改 · 已写入"
            : review.status === "writing"
              ? "文件修改 · 写入中"
              : review.status === "recovery_required" ||
                  review.status === "uncertain"
                ? "文件修改 · 请核对恢复记录"
                : "文件修改建议 · 尚未写入"}
        </strong>
        <div className="ai-workbench-handoff-actions">
          <button
            type="button"
            className="button button-quiet"
            onClick={() => {
              setWorkspaceErrorMessage("");
              try {
                const id = `ai-file-edit:${review.proposal!.id}`;
                useUiStore.setState({ rightOverviewCollapsed: false });
                useWorkspacePanels.getState().open(
                  {
                    kind: "ai-file-edit",
                    title: "AI 文件差异",
                    proposalId: review.proposal!.id,
                  },
                  id,
                );
              } catch {
                setWorkspaceErrorMessage(
                  "右侧工作区无法再打开新标签，请关闭不需要的标签后重试。",
                );
              }
            }}
          >
            在右侧工作区预览差异
          </button>
          <button
            type="button"
            className="button button-quiet"
            onClick={() => clear()}
          >
            {review.status === "writing" ? "关闭（不取消写入）" : "关闭建议"}
          </button>
        </div>
      </div>
      {workspaceErrorMessage && <p role="alert">{workspaceErrorMessage}</p>}
      <p>
        {review.selection.rootName} / {review.proposal.path} ·{" "}
        {review.providerLabel}
      </p>
      {review.status === "receiving" ? (
        <p role="status">
          本条生成尚未完成，暂不开放审查；失败或中断将丢弃建议。
        </p>
      ) : (
        <>
          <p>
            以下是完整替换差异，未隐藏上下文；中间变动区按整块比较。以转义形式展示换行、制表符、BOM
            和控制字符，空文件显示为空。只用于核对，不会执行文件内容。
          </p>
          <pre
            className="ws-source ai-project-file-source ai-file-review-diff"
            aria-label="完整文件差异"
          >
            {fileReviewDiff(targetFile.content, review.proposal.content).map(
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
            原文件 {targetFile.size} 字节 → 建议{" "}
            {new TextEncoder().encode(review.proposal.content).length} 字节
          </p>
          {capability.available && (
            <p>
              确认后仅处理这一个已审查文件。独立原生窗口会再次展示精确方向；你确认后，Windows
              才会为本次操作请求系统授权。原对象、候选对象和版本 2
              恢复记录会保留在应用本地数据目录（未加密、不交给
              AI、不在业务数据库备份内）。文件变化会拒绝执行；网页复核不等于原生确认或系统授权。
            </p>
          )}
          {review.error && <p role="alert">{review.error}</p>}
          {review.status === "checked" && (
            <p role="status">
              复核通过：此刻原文件与基线一致。没有写入任何内容。
            </p>
          )}
          {review.status === "applied" && (
            <p role="status">
              写入及完整字节核验成功。恢复记录可在右侧文件面板的“文件恢复”中查看。
            </p>
          )}
          {review.status === "writing" && (
            <>
              <p role="status">
                正在等待原生确认、Windows
                授权或执行回执；请勿重复操作。关闭页面不会撤销已开始的操作。
              </p>
              <button
                type="button"
                className="button button-secondary"
                disabled={cancelling}
                onClick={() => {
                  if (!review.proposal) return;
                  setCancelError("");
                  setCancelling(true);
                  void workspaceApi
                    .cancelFileOperation(
                      review.selection.rootId,
                      review.proposal.id,
                    )
                    .catch((reason: unknown) =>
                      setCancelError(workspaceError(reason)),
                    )
                    .finally(() => setCancelling(false));
                }}
              >
                {cancelling ? "正在请求取消…" : "取消本次操作"}
              </button>
              {cancelError && <p role="alert">{cancelError}</p>}
            </>
          )}
          {(review.status === "recovery_required" ||
            review.status === "uncertain") && (
            <p role="alert">
              不要重复写入。请打开右侧文件面板 →
              文件恢复，重新查看当前内容与原文后决定是否恢复。
            </p>
          )}
          {review.backupPath && (
            <p>
              恢复记录：<code>{review.backupPath}</code>
            </p>
          )}
          <button
            type="button"
            className="button button-secondary"
            disabled={!canWrite}
            onClick={() => {
              if (provider) void validate(sessionId, provider);
            }}
          >
            {review.status === "checking"
              ? "正在复核…"
              : "复核当前文件（不写入）"}
          </button>
          {!capability.available && <p role="status">{capability.reason}</p>}
          {canWrite && capability.available && (
            <>
              <label>
                <input
                  type="checkbox"
                  checked={confirmed === review.proposal}
                  onChange={(e) =>
                    setConfirmed(e.target.checked ? review.proposal : null)
                  }
                />
                我已核对完整差异，同意替换此文件并保存本地恢复备份
              </label>
              <button
                type="button"
                className="button button-primary"
                disabled={confirmed !== review.proposal}
                onClick={() => {
                  if (
                    provider &&
                    review.proposal &&
                    confirmed === review.proposal
                  ) {
                    setConfirmed(null);
                    void apply(sessionId, provider, review.proposal.id);
                  }
                }}
              >
                确认写入此文件
              </button>
            </>
          )}
        </>
      )}
    </section>
  );
}

const reviewStatusLabel = {
  receiving: "生成中",
  ready: "待人工复核",
  checking: "正在复核",
  checked: "基线复核通过",
  conflict: "文件冲突或已变化",
  writing: "单文件操作进行中",
  applied: "写入及完整字节核验成功",
  recovery_required: "需要核对恢复记录",
  uncertain: "操作结果未确定",
} as const;

/** Read-only mirror of the current in-memory proposal; it is never an approval surface. */
export function AiProjectFileReviewPreview({
  proposalId,
}: {
  proposalId: string;
}) {
  const review = useAiProjectFileReview((state) => state.review);
  if (
    !proposalId ||
    !review?.proposal ||
    !review.targetFile ||
    review.proposal.id !== proposalId
  )
    return (
      <div className="ws-resource ws-ai-file-review-preview" role="status">
        <h3>AI 文件差异预览已不可用</h3>
        <p>
          该提议已关闭、过期或被替换。预览只存在于当前对话的内存中，不会从历史记录恢复。
        </p>
      </div>
    );

  const snapshot = review.targetFile.snapshot;
  return (
    <div
      className="ws-resource ws-ai-file-review-preview"
      aria-label="AI 文件差异只读预览"
    >
      <header>
        <div>
          <h3>{review.proposal.path}</h3>
          <p>
            {review.selection.rootName} · {review.providerLabel}
          </p>
        </div>
        <span className={`ws-ai-file-review-status is-${review.status}`}>
          {reviewStatusLabel[review.status]}
        </span>
      </header>
      <p>
        只读预览。复核和写入审批仍在左侧原对话的文件建议卡片中；此处不会重新读取磁盘或写入文件。
      </p>
      <dl>
        <div>
          <dt>基线</dt>
          <dd>{snapshot.size} 字节</dd>
        </div>
        <div>
          <dt>建议</dt>
          <dd>
            {new TextEncoder().encode(review.proposal.content).length} 字节
          </dd>
        </div>
      </dl>
      {review.error && <p role="alert">{review.error}</p>}
      <pre
        className="ws-source ai-project-file-source ai-file-review-diff"
        aria-label="右侧完整文件差异"
      >
        {fileReviewDiff(snapshot.content, review.proposal.content).map(
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
    </div>
  );
}
