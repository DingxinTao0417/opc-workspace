import {
  Download,
  FileText,
  History,
  Paperclip,
  ShieldAlert,
  ShieldCheck,
  Trash2,
  Upload,
  X,
} from "lucide-react";
import { useRef, useState } from "react";
import { ApiError } from "../api/client";
import {
  useCreateProjectAttachment,
  useDeleteProjectAttachment,
  useDownloadProjectAttachment,
  useProjectAttachmentsQuery,
} from "../api/hooks";
import { useSettledPage } from "../lib/useSettledPage";
import { EmptyState, ErrorState, SkeletonRows } from "./feedback";

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function formatTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

function attachmentError(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof ApiError) {
    if (error.code === "VERSION_CONFLICT") {
      return "项目已在其他窗口变化，已刷新最新数据，请重新确认。";
    }
    if (error.code === "PROJECT_ARCHIVED") {
      return "归档项目为只读，请恢复项目后再修改附件。";
    }
    if (error.code === "PROJECT_ATTACHMENT_FILE_MISSING") {
      return "附件文件已不在受控存储中，系统已记录缺失状态。";
    }
    if (error.code === "PROJECT_ATTACHMENT_INTEGRITY_MISMATCH") {
      return "附件完整性校验失败，已拒绝下载。";
    }
    return error.requestId
      ? `${error.message} · 请求 ${error.requestId}`
      : error.message;
  }
  return "项目附件操作失败，请重试。";
}

function saveDownloadedFile(blob: Blob, name: string) {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = name;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}

interface ProjectAttachmentsSectionProps {
  archived: boolean;
  projectId: string;
  projectVersion: number;
}

export function ProjectAttachmentsSection({
  archived,
  projectId,
  projectVersion,
}: ProjectAttachmentsSectionProps) {
  const [page, setPage] = useState(1);
  const [showHistory, setShowHistory] = useState(false);
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  const [attachmentName, setAttachmentName] = useState("");
  const [deleting, setDeleting] = useState<{
    id: string;
    name: string;
    reason: string;
  } | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const query = useProjectAttachmentsQuery(projectId, {
    page,
    pageSize: 10,
    includeDeleted: showHistory,
  });
  const createMutation = useCreateProjectAttachment();
  const deleteMutation = useDeleteProjectAttachment();
  const downloadMutation = useDownloadProjectAttachment();
  const items = query.data?.items ?? [];
  const effectiveVersion = Math.max(
    projectVersion,
    query.data?.meta.projectVersion ?? 0,
  );
  const pages = Math.max(
    1,
    Math.ceil(
      (query.data?.meta.total ?? 0) / (query.data?.meta.pageSize ?? 10),
    ),
  );
  const busy = createMutation.isPending || deleteMutation.isPending;
  const error =
    attachmentError(createMutation.error) ??
    attachmentError(deleteMutation.error) ??
    attachmentError(downloadMutation.error);

  useSettledPage({
    page,
    meta: query.data?.meta,
    isFetching: query.isFetching,
    isPlaceholderData: query.isPlaceholderData,
    isSuccess: query.isSuccess,
    setPage,
  });

  const resetUpload = () => {
    setSelectedFile(null);
    setAttachmentName("");
    if (inputRef.current) inputRef.current.value = "";
  };

  const submitUpload = () => {
    if (!selectedFile || !attachmentName.trim() || archived) return;
    createMutation.mutate(
      {
        projectId,
        input: {
          file: selectedFile,
          name: attachmentName.trim(),
          expectedVersion: effectiveVersion,
        },
      },
      { onSuccess: resetUpload },
    );
  };

  const confirmDelete = () => {
    if (!deleting?.reason.trim() || archived) return;
    deleteMutation.mutate(
      {
        id: deleting.id,
        projectId,
        input: {
          reason: deleting.reason.trim(),
          expectedVersion: effectiveVersion,
        },
      },
      { onSuccess: () => setDeleting(null) },
    );
  };

  const download = (id: string, name: string) => {
    downloadMutation.mutate(
      { id, name },
      {
        onSuccess: (result) => saveDownloadedFile(result.blob, result.fileName),
      },
    );
  };

  return (
    <section className="project-detail-section client-attachments-section">
      <div className="project-detail-heading client-attachments-heading">
        <div>
          <h2>项目附件</h2>
          <p>合同、需求与交付资料保存在本地受控目录，下载前校验完整性。</p>
        </div>
        <div className="client-attachment-heading-actions">
          <button
            aria-pressed={showHistory}
            className={`button button-secondary ${showHistory ? "is-active" : ""}`}
            onClick={() => {
              setPage(1);
              setShowHistory((value) => !value);
            }}
            type="button"
          >
            <History size={14} />
            {showHistory ? "隐藏删除记录" : "删除历史"}
          </button>
          <button
            className="button button-primary"
            disabled={busy || archived}
            onClick={() => inputRef.current?.click()}
            title={archived ? "恢复项目后再添加附件" : undefined}
            type="button"
          >
            <Paperclip size={14} /> 添加附件
          </button>
          <input
            className="sr-only"
            onChange={(event) => {
              const file = event.currentTarget.files?.[0] ?? null;
              setSelectedFile(file);
              setAttachmentName(file?.name ?? "");
              createMutation.reset();
            }}
            ref={inputRef}
            type="file"
          />
        </div>
      </div>

      {selectedFile ? (
        <div className="client-attachment-upload" aria-label="待上传项目附件">
          <span className="client-attachment-icon" aria-hidden="true">
            <Upload size={17} />
          </span>
          <div className="client-attachment-upload-fields">
            <label htmlFor="project-attachment-name">附件名称</label>
            <input
              autoFocus
              id="project-attachment-name"
              maxLength={255}
              onChange={(event) => setAttachmentName(event.target.value)}
              value={attachmentName}
            />
            <small>
              {selectedFile.name} · {formatSize(selectedFile.size)}
            </small>
          </div>
          <button
            aria-label="取消项目附件上传"
            className="icon-button"
            disabled={busy}
            onClick={resetUpload}
            type="button"
          >
            <X size={15} />
          </button>
          <button
            className="button button-primary"
            disabled={
              busy ||
              archived ||
              !attachmentName.trim() ||
              selectedFile.size === 0
            }
            onClick={submitUpload}
            type="button"
          >
            {createMutation.isPending ? "正在上传…" : "确认上传"}
          </button>
        </div>
      ) : null}

      {error ? (
        <div className="form-error" role="alert">
          {error}
        </div>
      ) : null}
      {query.isPending ? <SkeletonRows count={3} /> : null}
      {query.isError ? (
        <ErrorState
          compact
          message="无法读取项目附件。"
          onRetry={() => void query.refetch()}
        />
      ) : null}
      {query.isSuccess && items.length === 0 ? (
        <EmptyState
          message={
            showHistory
              ? "该项目还没有附件记录。"
              : archived
                ? "该归档项目没有附件。"
                : "添加合同、需求文档或项目资料，文件只保存在本机。"
          }
          title={showHistory ? "暂无附件历史" : "暂无项目附件"}
        />
      ) : null}

      {items.length > 0 ? (
        <div className="client-attachment-list">
          {items.map((attachment) => {
            const deleted = attachment.deletedAt !== null;
            const integrityProblem = attachment.integrityStatus !== "verified";
            return (
              <article
                className={`client-attachment-row ${deleted ? "is-deleted" : ""}`}
                key={attachment.id}
              >
                <span className="client-attachment-icon" aria-hidden="true">
                  <FileText size={17} />
                </span>
                <div className="client-attachment-main">
                  <strong>{attachment.name}</strong>
                  <small>
                    {formatSize(attachment.sizeBytes)} · 上传于{" "}
                    {formatTime(attachment.createdAt)} ·{" "}
                    {attachment.recordedBy.displayName}
                  </small>
                  {deleted ? (
                    <span className="client-attachment-deleted-note">
                      已删除 · {attachment.deleteReason}
                    </span>
                  ) : null}
                </div>
                <span
                  className={`client-attachment-integrity ${integrityProblem ? "is-warning" : ""}`}
                  title={`完整性状态：${attachment.integrityStatus}`}
                >
                  {integrityProblem ? (
                    <ShieldAlert size={14} />
                  ) : (
                    <ShieldCheck size={14} />
                  )}
                  {integrityProblem ? "需检查" : "已校验"}
                </span>
                {!deleted ? (
                  <div className="project-action-row client-attachment-actions">
                    <button
                      aria-label={`下载 ${attachment.name}`}
                      className="icon-button"
                      disabled={downloadMutation.isPending}
                      onClick={() => download(attachment.id, attachment.name)}
                      type="button"
                    >
                      <Download size={15} />
                    </button>
                    <button
                      aria-label={`删除 ${attachment.name}`}
                      className="icon-button danger"
                      disabled={busy || archived}
                      onClick={() =>
                        setDeleting({
                          id: attachment.id,
                          name: attachment.name,
                          reason: "",
                        })
                      }
                      type="button"
                    >
                      <Trash2 size={15} />
                    </button>
                  </div>
                ) : null}
                {deleting?.id === attachment.id ? (
                  <div
                    className="client-attachment-delete-confirm"
                    role="alert"
                  >
                    <strong>删除“{deleting.name}”</strong>
                    <input
                      autoFocus
                      maxLength={1000}
                      onChange={(event) =>
                        setDeleting((value) =>
                          value
                            ? { ...value, reason: event.target.value }
                            : value,
                        )
                      }
                      placeholder="填写删除原因"
                      value={deleting.reason}
                    />
                    <button
                      className="button button-secondary"
                      disabled={busy}
                      onClick={() => setDeleting(null)}
                      type="button"
                    >
                      取消
                    </button>
                    <button
                      className="button button-danger"
                      disabled={busy || archived || !deleting.reason.trim()}
                      onClick={confirmDelete}
                      type="button"
                    >
                      {deleteMutation.isPending ? "正在删除…" : "确认删除"}
                    </button>
                  </div>
                ) : null}
              </article>
            );
          })}
        </div>
      ) : null}

      {pages > 1 ? (
        <nav aria-label="项目附件分页" className="pagination">
          <button
            className="button button-secondary"
            disabled={page <= 1 || query.isFetching}
            onClick={() => setPage((value) => Math.max(1, value - 1))}
            type="button"
          >
            上一页
          </button>
          <span>
            第 {page} / {pages} 页
          </span>
          <button
            className="button button-secondary"
            disabled={page >= pages || query.isFetching}
            onClick={() => setPage((value) => value + 1)}
            type="button"
          >
            下一页
          </button>
        </nav>
      ) : null}
    </section>
  );
}
