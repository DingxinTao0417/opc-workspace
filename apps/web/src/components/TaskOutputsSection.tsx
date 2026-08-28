import {
  AlertTriangle,
  Check,
  ChevronDown,
  ChevronUp,
  History,
  LoaderCircle,
  Paperclip,
  Plus,
  RotateCcw,
  Send,
  X,
} from "lucide-react";
import {
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
} from "react";
import { ApiError } from "../api/client";
import {
  useDeleteTaskArtifact,
  useDownloadTaskArtifact,
  useReviewTaskSubmission,
  useSubmitTaskOutput,
  useTaskArtifactsQuery,
  useTaskAssignmentsQuery,
  useTaskSubmissionsQuery,
} from "../api/hooks";
import type {
  NewTaskArtifactInput,
  Task,
  TaskArtifactSummary,
  TaskArtifactStorageKind,
  TaskSubmission,
  TaskSubmissionStatus,
} from "../types/models";
import { TaskArtifactCard } from "./TaskArtifactCard";

interface TaskOutputsSectionProps {
  task: Task;
  disabled?: boolean;
  hasUnsavedFacts?: boolean;
  onBusyChange?: (busy: boolean) => void;
  onHistoryStateChange?: (state: {
    loading: boolean;
    error: boolean;
    total: number | null;
  }) => void;
  onRefreshTask: () => Promise<Task | null>;
  onTaskUpdated?: (task: Task) => void;
}

interface DraftArtifact {
  clientRef: string;
  storageKind: TaskArtifactStorageKind;
  name: string;
  nameFromFile: boolean;
  value: string;
  file: File | null;
  requiresFollowup: boolean;
}

interface VersionConflict {
  latestVersion: number | null;
  refreshing: boolean;
}

interface ReviewEditor {
  decision: "accept" | "request_changes";
  reason: string;
  expectedVersion: number;
}

interface DeleteEditor {
  artifact: TaskArtifactSummary;
  reason: string;
  expectedVersion: number;
}

const storageLabels: Record<TaskArtifactStorageKind, string> = {
  text: "文本",
  link: "链接",
  structured: "结构化",
  file: "文件",
};

const submissionLabels: Record<TaskSubmissionStatus, string> = {
  pending_review: "待验收",
  accepted: "已接受",
  changes_requested: "已要求返工",
  withdrawn: "已撤回",
};

const outputErrorLabels: Record<string, string> = {
  VERSION_REQUIRED: "任务版本缺失，请刷新详情后重试。",
  INVALID_VERSION: "任务版本无效，请刷新详情后重试。",
  TASK_MANUAL_REVIEW_REQUIRED: "只有启用人工验收的任务可以提交产出。",
  TASK_REVIEW_POLICY_REQUIRED: "此任务未启用人工验收。",
  TASK_ASSIGNEE_REQUIRED: "提交产出前需要先设置负责人。",
  TASK_REVIEWER_REQUIRED: "提交产出前需要先设置审核人。",
  TASK_SUBMISSION_ALREADY_PENDING: "已有一批产出等待验收，不能重复提交。",
  TASK_SUBMISSION_NOT_ALLOWED: "只有待办或进行中的任务可以提交产出。",
  TASK_SUBMISSION_INVALID: "当前提交记录不可用，请刷新后重试。",
  TASK_SUBMISSION_REQUIRED: "没有可验收的当前提交。",
  TASK_REVIEW_NOT_ALLOWED: "当前任务或提交状态不允许验收。",
  TASK_TRANSITION_NOT_ALLOWED: "当前任务状态不允许执行此操作。",
  ARTIFACT_PENDING_REVIEW: "待验收批次的产出不能删除。",
  ARTIFACT_ALREADY_DELETED: "这项产出已经删除。",
  ARTIFACT_HAS_ACTIVE_INBOX_SOURCE:
    "这项产出仍是活动收件箱跟进事项的来源。请先解决或忽略对应条目，再删除产出。",
  ARTIFACT_DELETED: "这项产出已经删除。",
  ARTIFACT_CONTENT_UNAVAILABLE: "这项产出没有可下载的文件内容。",
  ARTIFACT_FILE_MISSING: "受控文件已缺失，无法下载。",
  ARTIFACT_INTEGRITY_MISMATCH: "文件校验不一致，已阻止下载。",
  ARTIFACT_FILE_TOO_LARGE: "单个文件不能超过 50 MiB。",
  ARTIFACT_STORAGE_ERROR: "本地产出存储操作失败，请稍后重试。",
  ARTIFACT_STORAGE_UNAVAILABLE: "本地产出存储暂不可用，请稍后重试。",
  IDEMPOTENCY_CONFLICT: "本次操作与已有重试记录不一致，请刷新后重试。",
};

function errorMessage(error: unknown, fallback: string): string | null {
  if (!error) return null;
  if (error instanceof ApiError) {
    if (error.code === "VERSION_CONFLICT") return null;
    const message = outputErrorLabels[error.code] ?? error.message;
    return error.requestId ? `${message} · 请求 ${error.requestId}` : message;
  }
  return fallback;
}

function formatTime(value: string | null): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-CN", {
    month: "numeric",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(date);
}

function formatBytes(value: number | null): string | null {
  if (value === null) return null;
  if (value < 1_024) return `${value} B`;
  if (value < 1_024 * 1_024) return `${(value / 1_024).toFixed(1)} KB`;
  return `${(value / (1_024 * 1_024)).toFixed(1)} MB`;
}

function createDraftArtifact(
  storageKind: TaskArtifactStorageKind,
): DraftArtifact {
  return {
    clientRef: crypto.randomUUID(),
    storageKind,
    name: "",
    nameFromFile: false,
    value: storageKind === "structured" ? "{\n  \n}" : "",
    file: null,
    requiresFollowup: false,
  };
}

function validateAndSerializeArtifacts(drafts: DraftArtifact[]): {
  artifacts?: NewTaskArtifactInput[];
  error?: string;
} {
  const artifacts: NewTaskArtifactInput[] = [];
  for (const [index, draft] of drafts.entries()) {
    const name = draft.name.trim();
    if (!name) return { error: `第 ${index + 1} 项产出需要填写名称。` };
    if (Array.from(name).length > 255 || /[\r\n\0]/.test(name)) {
      return {
        error: `第 ${index + 1} 项产出名称必须为 1 到 255 个安全字符。`,
      };
    }
    const common = {
      clientRef: draft.clientRef,
      name,
      requiresFollowup: draft.requiresFollowup,
    };
    switch (draft.storageKind) {
      case "text":
        if (!draft.value.trim()) {
          return { error: `第 ${index + 1} 项文本产出不能为空。` };
        }
        if (Array.from(draft.value).length > 500_000) {
          return {
            error: `第 ${index + 1} 项文本产出不能超过 500000 个 Unicode 字符。`,
          };
        }
        artifacts.push({
          ...common,
          storageKind: "text",
          contentText: draft.value,
        });
        break;
      case "link": {
        let url: URL;
        const reference = draft.value.trim();
        if (new TextEncoder().encode(reference).byteLength > 4_096) {
          return { error: `第 ${index + 1} 项链接不能超过 4096 字节。` };
        }
        try {
          url = new URL(reference);
        } catch {
          return { error: `第 ${index + 1} 项链接格式无效。` };
        }
        if (
          (url.protocol !== "http:" && url.protocol !== "https:") ||
          !url.host ||
          url.username ||
          url.password
        ) {
          return {
            error: `第 ${index + 1} 项链接只支持无账号凭据的 http 或 https 地址。`,
          };
        }
        artifacts.push({
          ...common,
          storageKind: "link",
          referenceUrl: url.toString(),
        });
        break;
      }
      case "structured": {
        let structuredJson: unknown;
        try {
          structuredJson = JSON.parse(draft.value);
        } catch {
          return { error: `第 ${index + 1} 项结构化内容不是有效 JSON。` };
        }
        if (
          typeof structuredJson !== "object" ||
          structuredJson === null ||
          Array.isArray(structuredJson)
        ) {
          return { error: `第 ${index + 1} 项结构化内容必须是 JSON 对象。` };
        }
        if (
          new TextEncoder().encode(JSON.stringify(structuredJson)).byteLength >
          1_024 * 1_024
        ) {
          return {
            error: `第 ${index + 1} 项结构化内容编码后不能超过 1 MiB。`,
          };
        }
        artifacts.push({
          ...common,
          storageKind: "structured",
          structuredJson: structuredJson as Record<string, unknown>,
        });
        break;
      }
      case "file":
        if (!draft.file) return { error: `第 ${index + 1} 项尚未选择文件。` };
        if (draft.file.size < 1) {
          return { error: `第 ${index + 1} 项文件不能为空。` };
        }
        if (draft.file.size > 50 * 1_024 * 1_024) {
          return { error: `第 ${index + 1} 项文件不能超过 50 MiB。` };
        }
        artifacts.push({
          ...common,
          storageKind: "file",
          file: draft.file,
        });
        break;
    }
  }
  return { artifacts };
}

function ConflictNotice({
  conflict,
  message,
  onKeep,
  onRetryRefresh,
}: {
  conflict: VersionConflict;
  message: string;
  onKeep: (version: number) => void;
  onRetryRefresh: () => void;
}) {
  return (
    <div className="task-conflict task-output-conflict" role="alert">
      {conflict.refreshing ? (
        <LoaderCircle className="spin" size={15} />
      ) : (
        <AlertTriangle size={15} />
      )}
      <div>
        <strong>任务已在其他窗口发生变化</strong>
        <span>
          {conflict.refreshing
            ? `正在读取最新版；${message}仍保留。`
            : conflict.latestVersion
              ? `已读取最新版 v${conflict.latestVersion}；${message}仍保留，请确认后再次提交。`
              : `尚未确认最新版；${message}仍保留，当前不会重放操作。`}
        </span>
      </div>
      {!conflict.refreshing && !conflict.latestVersion ? (
        <button
          className="button button-secondary"
          onClick={onRetryRefresh}
          type="button"
        >
          重试读取
        </button>
      ) : null}
      <button
        className="button button-primary"
        disabled={!conflict.latestVersion || conflict.refreshing}
        onClick={() => conflict.latestVersion && onKeep(conflict.latestVersion)}
        type="button"
      >
        保留草稿重试
      </button>
    </div>
  );
}

function SubmissionSummary({ submission }: { submission: TaskSubmission }) {
  return (
    <div className="task-submission-summary">
      <div>
        <strong>第 {submission.sequence} 次提交</strong>
        <span className={`task-submission-status is-${submission.status}`}>
          {submissionLabels[submission.status]}
        </span>
        {submission.isInferred ? <em>迁移推定</em> : null}
      </div>
      <p>
        {submission.submittedByActor.displayName} 录入 ·{" "}
        {formatTime(submission.submittedAt)}
      </p>
      {submission.summary ? (
        <blockquote>{submission.summary}</blockquote>
      ) : null}
      {submission.reviewReason ? (
        <small>验收说明：{submission.reviewReason}</small>
      ) : null}
    </div>
  );
}

export function TaskOutputsSection({
  task,
  disabled = false,
  hasUnsavedFacts = false,
  onBusyChange,
  onHistoryStateChange,
  onRefreshTask,
  onTaskUpdated,
}: TaskOutputsSectionProps) {
  const draftPanelId = useId();
  const historyPanelId = useId();
  const artifactsPanelId = useId();
  const draftContinueRef = useRef<HTMLButtonElement>(null);
  const reviewContinueRef = useRef<HTMLButtonElement>(null);
  const deleteContinueRef = useRef<HTMLButtonElement>(null);
  const [focusAfterCollapse, setFocusAfterCollapse] = useState<
    "draft" | "review" | "delete" | null
  >(null);
  const [draftOpen, setDraftOpen] = useState(true);
  const [summary, setSummary] = useState("");
  const [draftArtifacts, setDraftArtifacts] = useState<DraftArtifact[]>([]);
  const [addKind, setAddKind] = useState<TaskArtifactStorageKind>("text");
  const [submitExpectedVersion, setSubmitExpectedVersion] = useState(
    task.version,
  );
  const [submitConflict, setSubmitConflict] = useState<VersionConflict | null>(
    null,
  );
  const [reviewEditor, setReviewEditor] = useState<ReviewEditor | null>(null);
  const [reviewEditorOpen, setReviewEditorOpen] = useState(false);
  const [reviewConflict, setReviewConflict] = useState<VersionConflict | null>(
    null,
  );
  const [deleteEditor, setDeleteEditor] = useState<DeleteEditor | null>(null);
  const [deleteEditorOpen, setDeleteEditorOpen] = useState(false);
  const [deleteConflict, setDeleteConflict] = useState<VersionConflict | null>(
    null,
  );
  const [historyOpen, setHistoryOpen] = useState(false);
  const [artifactsOpen, setArtifactsOpen] = useState(false);
  const [expandedArtifactId, setExpandedArtifactId] = useState<string | null>(
    null,
  );
  const [validationError, setValidationError] = useState<string | null>(null);
  const [downloadError, setDownloadError] = useState<string | null>(null);

  const submissionsQuery = useTaskSubmissionsQuery(task.id, { pageSize: 10 });
  const artifactsQuery = useTaskArtifactsQuery(
    task.id,
    { pageSize: 20, includeDeleted: true },
    artifactsOpen,
  );
  const assignmentsQuery = useTaskAssignmentsQuery(task.id, {
    pageSize: 20,
    sort: "-assigned_at",
  });
  const submitMutation = useSubmitTaskOutput();
  const reviewMutation = useReviewTaskSubmission();
  const deleteMutation = useDeleteTaskArtifact();
  const downloadMutation = useDownloadTaskArtifact();
  const writeBusy =
    submitMutation.isPending ||
    reviewMutation.isPending ||
    deleteMutation.isPending;
  const downloadBusy = downloadMutation.isPending;
  const operationBusy = writeBusy || downloadBusy;
  const writeDisabled = disabled || hasUnsavedFacts || operationBusy;
  const downloadDisabled = disabled || operationBusy;

  const submissions = useMemo(() => {
    const seen = new Set<string>();
    return (submissionsQuery.data?.pages ?? [])
      .flatMap((page) => page.items)
      .filter((submission) => {
        if (seen.has(submission.id)) return false;
        seen.add(submission.id);
        return true;
      });
  }, [submissionsQuery.data?.pages]);
  const artifacts = useMemo(() => {
    const seen = new Set<string>();
    return (artifactsQuery.data?.pages ?? [])
      .flatMap((page) => page.items)
      .filter((artifact) => {
        if (seen.has(artifact.id)) return false;
        seen.add(artifact.id);
        return true;
      });
  }, [artifactsQuery.data?.pages]);
  const currentSubmission = task.currentSubmissionId
    ? (submissions.find(
        (submission) => submission.id === task.currentSubmissionId,
      ) ?? null)
    : null;
  const activeAssignments = assignmentsQuery.data?.pages[0]?.active;
  const assignee = activeAssignments?.assignee ?? null;
  const reviewer = activeAssignments?.reviewer ?? null;
  const historyTotal = submissionsQuery.data?.pages[0]?.meta.total ?? null;
  const reviewReady =
    task.status === "waiting_review" &&
    !assignmentsQuery.isPending &&
    !assignmentsQuery.isError &&
    !submissionsQuery.isPending &&
    !submissionsQuery.isError &&
    reviewer !== null &&
    currentSubmission?.status === "pending_review";

  useEffect(() => {
    onBusyChange?.(operationBusy);
    return () => onBusyChange?.(false);
  }, [onBusyChange, operationBusy]);

  useEffect(() => {
    onHistoryStateChange?.({
      loading: submissionsQuery.isPending,
      error: submissionsQuery.isError,
      total: historyTotal,
    });
  }, [
    historyTotal,
    onHistoryStateChange,
    submissionsQuery.isError,
    submissionsQuery.isPending,
  ]);

  useEffect(() => {
    setDraftOpen(true);
    setSummary("");
    setDraftArtifacts([]);
    setSubmitExpectedVersion(task.version);
    setSubmitConflict(null);
    setReviewEditor(null);
    setReviewEditorOpen(false);
    setReviewConflict(null);
    setDeleteEditor(null);
    setDeleteEditorOpen(false);
    setDeleteConflict(null);
    setHistoryOpen(false);
    setArtifactsOpen(false);
    setExpandedArtifactId(null);
    setValidationError(null);
    setDownloadError(null);
    setFocusAfterCollapse(null);
    submitMutation.reset();
    reviewMutation.reset();
    deleteMutation.reset();
    downloadMutation.reset();
  }, [task.id]);

  useEffect(() => {
    if (!focusAfterCollapse) return;
    const target =
      focusAfterCollapse === "draft"
        ? draftContinueRef.current
        : focusAfterCollapse === "review"
          ? reviewContinueRef.current
          : deleteContinueRef.current;
    target?.focus();
    setFocusAfterCollapse(null);
  }, [focusAfterCollapse]);

  const preventParentFormSubmit = (event: ReactKeyboardEvent<HTMLElement>) => {
    if (
      event.key === "Enter" &&
      !event.nativeEvent.isComposing &&
      event.target instanceof HTMLInputElement &&
      (event.target.type === "text" || event.target.type === "url")
    ) {
      event.preventDefault();
    }
  };

  useEffect(() => {
    if (submitConflict || reviewConflict || deleteConflict || writeBusy) return;
    setSubmitExpectedVersion((current) => Math.max(current, task.version));
    setReviewEditor((current) =>
      current
        ? {
            ...current,
            expectedVersion: Math.max(current.expectedVersion, task.version),
          }
        : null,
    );
    setDeleteEditor((current) =>
      current
        ? {
            ...current,
            expectedVersion: Math.max(current.expectedVersion, task.version),
          }
        : null,
    );
  }, [deleteConflict, reviewConflict, submitConflict, task.version, writeBusy]);

  const refreshConflict = async (
    expectedVersion: number,
    setConflict: (value: VersionConflict) => void,
  ) => {
    setConflict({ latestVersion: null, refreshing: true });
    const latest = await onRefreshTask();
    setConflict({
      latestVersion:
        latest && latest.version > expectedVersion ? latest.version : null,
      refreshing: false,
    });
  };

  const addArtifact = () => {
    if (draftArtifacts.length >= 20) {
      setValidationError("每次最多添加 20 项产出。");
      return;
    }
    setDraftArtifacts((items) => [...items, createDraftArtifact(addKind)]);
    setValidationError(null);
    setDraftOpen(true);
  };

  const updateDraftArtifact = (
    clientRef: string,
    update: Partial<DraftArtifact>,
  ) => {
    setDraftArtifacts((items) =>
      items.map((item) =>
        item.clientRef === clientRef ? { ...item, ...update } : item,
      ),
    );
  };

  const submitOutput = () => {
    if (writeDisabled || submitConflict) return;
    const cleanSummary = summary.trim();
    const serialized = validateAndSerializeArtifacts(draftArtifacts);
    if (serialized.error) {
      setValidationError(serialized.error);
      return;
    }
    if (!cleanSummary && serialized.artifacts?.length === 0) {
      setValidationError("请填写提交摘要，或至少添加一项有效产出。");
      return;
    }
    if (!assignee || !reviewer) {
      setValidationError("提交前需要同时设置负责人和审核人。");
      return;
    }
    setValidationError(null);
    submitMutation.mutate(
      {
        taskId: task.id,
        input: {
          summary: cleanSummary,
          artifacts: serialized.artifacts ?? [],
          expectedVersion: submitExpectedVersion,
        },
      },
      {
        onError: (error) => {
          if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
            void refreshConflict(submitExpectedVersion, setSubmitConflict);
          }
        },
        onSuccess: (result) => {
          setSummary("");
          setDraftArtifacts([]);
          setSubmitConflict(null);
          setValidationError(null);
          onTaskUpdated?.(result.task);
        },
      },
    );
  };

  const submitReview = () => {
    if (!reviewEditor || writeDisabled || reviewConflict || !reviewReady)
      return;
    const reason = reviewEditor.reason.trim();
    if (reviewEditor.decision === "request_changes" && !reason) {
      setValidationError("要求返工时必须填写原因。");
      return;
    }
    setValidationError(null);
    reviewMutation.mutate(
      {
        taskId: task.id,
        input:
          reviewEditor.decision === "accept"
            ? {
                decision: "accept",
                expectedVersion: reviewEditor.expectedVersion,
              }
            : {
                decision: "request_changes",
                reason,
                expectedVersion: reviewEditor.expectedVersion,
              },
      },
      {
        onError: (error) => {
          if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
            void refreshConflict(
              reviewEditor.expectedVersion,
              setReviewConflict,
            );
          }
        },
        onSuccess: (result) => {
          setReviewEditor(null);
          setReviewEditorOpen(false);
          setReviewConflict(null);
          setValidationError(null);
          onTaskUpdated?.(result.task);
        },
      },
    );
  };

  const confirmDelete = () => {
    if (!deleteEditor || writeDisabled || deleteConflict) return;
    const reason = deleteEditor.reason.trim();
    if (!reason) {
      setValidationError("删除产出时必须填写原因。");
      return;
    }
    setValidationError(null);
    deleteMutation.mutate(
      {
        taskId: task.id,
        artifactId: deleteEditor.artifact.id,
        input: { reason, expectedVersion: deleteEditor.expectedVersion },
      },
      {
        onError: (error) => {
          if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
            void refreshConflict(
              deleteEditor.expectedVersion,
              setDeleteConflict,
            );
          }
        },
        onSuccess: (result) => {
          setDeleteEditor(null);
          setDeleteEditorOpen(false);
          setDeleteConflict(null);
          setExpandedArtifactId(null);
          setValidationError(null);
          onTaskUpdated?.(result.task);
        },
      },
    );
  };

  const downloadArtifact = async (artifact: TaskArtifactSummary) => {
    if (downloadDisabled) return;
    setDownloadError(null);
    try {
      const result = await downloadMutation.mutateAsync({
        taskId: task.id,
        id: artifact.id,
        name: artifact.name,
      });
      if (typeof URL.createObjectURL !== "function") {
        setDownloadError("当前运行环境不支持浏览器下载。请使用桌面应用重试。");
        return;
      }
      const url = URL.createObjectURL(result.blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = result.fileName;
      anchor.rel = "noopener";
      document.body.appendChild(anchor);
      anchor.click();
      anchor.remove();
      window.setTimeout(() => URL.revokeObjectURL(url), 0);
    } catch (error) {
      setDownloadError(errorMessage(error, "文件下载失败，请重试。"));
    }
  };

  const renderArtifact = (artifact: TaskArtifactSummary) => (
    <TaskArtifactCard
      artifact={artifact}
      disabled={writeDisabled}
      downloadDisabled={downloadDisabled}
      downloading={
        downloadBusy && downloadMutation.variables?.id === artifact.id
      }
      expanded={expandedArtifactId === artifact.id}
      key={artifact.id}
      onDelete={(target) => {
        setDeleteEditor((current) =>
          current?.artifact.id === target.id
            ? {
                ...current,
                expectedVersion: Math.max(
                  current.expectedVersion,
                  task.version,
                ),
              }
            : {
                artifact: target,
                reason: "",
                expectedVersion: task.version,
              },
        );
        setDeleteEditorOpen(true);
        setDeleteConflict(null);
        setValidationError(null);
      }}
      onDownload={downloadArtifact}
      onToggle={(artifactId) =>
        setExpandedArtifactId((current) =>
          current === artifactId ? null : artifactId,
        )
      }
    />
  );

  const mutationError =
    errorMessage(submitMutation.error, "提交产出失败，请重试。") ??
    errorMessage(reviewMutation.error, "验收操作失败，请重试。") ??
    errorMessage(deleteMutation.error, "删除产出失败，请重试。");

  return (
    <section
      aria-labelledby="task-outputs-heading"
      className="task-outputs"
      onKeyDown={preventParentFormSubmit}
    >
      <div className="task-outputs-heading">
        <div>
          <h3 id="task-outputs-heading">产出与验收</h3>
          <p>产出保留独立历史；任务是否完成只由受控验收决定。</p>
        </div>
        <span className="task-review-policy">
          {task.reviewPolicy === "manual" ? "人工验收" : "无需验收"}
        </span>
      </div>

      {hasUnsavedFacts ? (
        <div className="task-output-notice">
          当前有未保存的任务信息；请先保存，再提交、验收或删除产出。
        </div>
      ) : null}

      {task.reviewPolicy === "none" ? (
        <div className="task-output-policy-note">
          <Check size={14} />
          <div>
            <strong>此任务无需验收</strong>
            <span>待办或进行中时，可在“任务状态”中直接完成。</span>
          </div>
        </div>
      ) : null}

      {task.reviewPolicy === "manual" ? (
        <>
          {assignmentsQuery.isPending ? (
            <div aria-live="polite" className="task-output-state" role="status">
              <LoaderCircle className="spin" size={13} /> 正在确认责任分派…
            </div>
          ) : null}
          {assignmentsQuery.isError ? (
            <div className="task-output-error" role="alert">
              <AlertTriangle size={13} />
              <span>无法确认负责人和审核人，提交与验收已暂停。</span>
              <button
                className="form-inline-action"
                onClick={() => void assignmentsQuery.refetch()}
                type="button"
              >
                重试
              </button>
            </div>
          ) : null}
          {!assignmentsQuery.isPending &&
          !assignmentsQuery.isError &&
          (!assignee || !reviewer) ? (
            <div className="task-output-prerequisites">
              <AlertTriangle size={14} />
              <div>
                <strong>提交前还缺少责任角色</strong>
                <span>
                  {!assignee ? "请先在上方设置负责人。" : ""}
                  {!assignee && !reviewer ? " " : ""}
                  {!reviewer ? "请设置所有者为审核人。" : ""}
                </span>
              </div>
            </div>
          ) : null}
          {assignee && reviewer ? (
            <p className="task-output-actors">
              {assignee.actor.displayName} 负责产出；由{" "}
              {reviewer.actor.displayName}
              验收。person 的线下结果由所有者代录，不会通知对方。
            </p>
          ) : null}

          {task.status === "todo" || task.status === "in_progress" ? (
            <div className="task-output-editor">
              <div className="task-output-editor-heading">
                <div>
                  <strong>提交一批产出</strong>
                  <span>摘要或至少一项产出必填；单次最多 20 项。</span>
                </div>
                <button
                  aria-controls={draftPanelId}
                  aria-expanded={draftOpen}
                  className="button button-quiet"
                  onClick={() => setDraftOpen((value) => !value)}
                  ref={draftContinueRef}
                  type="button"
                >
                  {draftOpen ? (
                    <ChevronUp size={13} />
                  ) : (
                    <ChevronDown size={13} />
                  )}
                  {draftOpen ? "收起草稿" : "继续编辑"}
                </button>
              </div>
              {draftOpen ? (
                <div id={draftPanelId}>
                  <label className="form-field">
                    <span>提交摘要</span>
                    <textarea
                      aria-label="提交摘要"
                      maxLength={10_000}
                      onChange={(event) => setSummary(event.target.value)}
                      placeholder="说明本次完成了什么、需要审核什么…"
                      rows={3}
                      value={summary}
                    />
                    <small>{summary.length}/10000</small>
                  </label>

                  <div className="task-output-draft-list">
                    {draftArtifacts.map((artifact, index) => (
                      <article
                        className="task-output-draft"
                        key={artifact.clientRef}
                      >
                        <div className="task-output-draft-heading">
                          <strong>
                            {index + 1}. {storageLabels[artifact.storageKind]}
                            产出
                          </strong>
                          <button
                            aria-label={`移除第 ${index + 1} 项产出`}
                            className="button button-quiet"
                            disabled={writeBusy}
                            onClick={() =>
                              setDraftArtifacts((items) =>
                                items.filter(
                                  (item) =>
                                    item.clientRef !== artifact.clientRef,
                                ),
                              )
                            }
                            type="button"
                          >
                            <X size={13} />
                          </button>
                        </div>
                        <label className="form-field">
                          <span>名称</span>
                          <input
                            aria-label={`第 ${index + 1} 项产出名称`}
                            maxLength={255}
                            onChange={(event) =>
                              updateDraftArtifact(artifact.clientRef, {
                                name: event.target.value,
                                nameFromFile: false,
                              })
                            }
                            placeholder={
                              artifact.storageKind === "file"
                                ? (artifact.file?.name ?? "例如：最终设计稿")
                                : "便于以后识别的名称"
                            }
                            value={artifact.name}
                          />
                        </label>
                        {artifact.storageKind === "text" ? (
                          <label className="form-field">
                            <span>文本内容</span>
                            <textarea
                              aria-label={`第 ${index + 1} 项文本内容`}
                              onChange={(event) =>
                                updateDraftArtifact(artifact.clientRef, {
                                  value: event.target.value,
                                })
                              }
                              rows={4}
                              value={artifact.value}
                            />
                          </label>
                        ) : null}
                        {artifact.storageKind === "link" ? (
                          <label className="form-field">
                            <span>网页链接</span>
                            <input
                              aria-label={`第 ${index + 1} 项网页链接`}
                              maxLength={4_096}
                              onChange={(event) =>
                                updateDraftArtifact(artifact.clientRef, {
                                  value: event.target.value,
                                })
                              }
                              placeholder="https://example.com/result"
                              type="url"
                              value={artifact.value}
                            />
                          </label>
                        ) : null}
                        {artifact.storageKind === "structured" ? (
                          <label className="form-field">
                            <span>JSON 对象</span>
                            <textarea
                              aria-label={`第 ${index + 1} 项结构化内容`}
                              className="task-output-json"
                              onChange={(event) =>
                                updateDraftArtifact(artifact.clientRef, {
                                  value: event.target.value,
                                })
                              }
                              rows={5}
                              spellCheck={false}
                              value={artifact.value}
                            />
                          </label>
                        ) : null}
                        {artifact.storageKind === "file" ? (
                          <label className="form-field task-output-file">
                            <span>本地文件</span>
                            <input
                              aria-label={`第 ${index + 1} 项本地文件`}
                              onChange={(event) => {
                                const file =
                                  event.currentTarget.files?.[0] ?? null;
                                const syncName =
                                  artifact.nameFromFile || !artifact.name;
                                updateDraftArtifact(artifact.clientRef, {
                                  file,
                                  name: syncName
                                    ? (file?.name ?? "")
                                    : artifact.name,
                                  nameFromFile: Boolean(file && syncName),
                                });
                              }}
                              type="file"
                            />
                            {artifact.file ? (
                              <small>
                                {artifact.file.name} ·{" "}
                                {formatBytes(artifact.file.size)}
                              </small>
                            ) : null}
                          </label>
                        ) : null}
                        <label className="task-output-followup">
                          <input
                            checked={artifact.requiresFollowup}
                            onChange={(event) =>
                              updateDraftArtifact(artifact.clientRef, {
                                requiresFollowup: event.target.checked,
                              })
                            }
                            type="checkbox"
                          />
                          标记为需要后续跟进
                        </label>
                      </article>
                    ))}
                  </div>

                  <div className="task-output-add">
                    <select
                      aria-label="新增产出类型"
                      disabled={writeBusy || draftArtifacts.length >= 20}
                      onChange={(event) =>
                        setAddKind(
                          event.target.value as TaskArtifactStorageKind,
                        )
                      }
                      value={addKind}
                    >
                      {Object.entries(storageLabels).map(([value, label]) => (
                        <option key={value} value={value}>
                          {label}
                        </option>
                      ))}
                    </select>
                    <button
                      className="button button-secondary"
                      disabled={writeBusy || draftArtifacts.length >= 20}
                      onClick={addArtifact}
                      type="button"
                    >
                      <Plus size={13} />
                      添加产出
                    </button>
                    <span>{draftArtifacts.length}/20</span>
                  </div>

                  {submitConflict ? (
                    <ConflictNotice
                      conflict={submitConflict}
                      message="摘要、文本、链接、结构化内容和已选择文件"
                      onKeep={(version) => {
                        setSubmitExpectedVersion(version);
                        setSubmitConflict(null);
                        submitMutation.reset();
                      }}
                      onRetryRefresh={() =>
                        void refreshConflict(
                          submitExpectedVersion,
                          setSubmitConflict,
                        )
                      }
                    />
                  ) : null}

                  <div className="task-output-editor-actions">
                    <button
                      className="button button-secondary"
                      disabled={writeBusy}
                      onClick={() => {
                        setDraftOpen(false);
                        setFocusAfterCollapse("draft");
                      }}
                      type="button"
                    >
                      收起并保留
                    </button>
                    <button
                      className="button button-primary"
                      disabled={
                        writeDisabled ||
                        Boolean(submitConflict) ||
                        !assignee ||
                        !reviewer ||
                        assignmentsQuery.isPending ||
                        assignmentsQuery.isError
                      }
                      onClick={submitOutput}
                      type="button"
                    >
                      {submitMutation.isPending ? (
                        <LoaderCircle className="spin" size={13} />
                      ) : (
                        <Send size={13} />
                      )}
                      {submitMutation.isPending ? "正在提交…" : "提交验收"}
                    </button>
                  </div>
                </div>
              ) : null}
            </div>
          ) : null}

          {task.status === "waiting_review" ? (
            <div className="task-review-panel">
              {submissionsQuery.isPending ? (
                <div
                  aria-live="polite"
                  className="task-output-state"
                  role="status"
                >
                  <LoaderCircle className="spin" size={13} /> 正在读取当前提交…
                </div>
              ) : null}
              {submissionsQuery.isError ? (
                <div className="task-output-error" role="alert">
                  <AlertTriangle size={13} />
                  <span>当前提交读取失败，验收操作已暂停。</span>
                  <button
                    className="form-inline-action"
                    onClick={() => void submissionsQuery.refetch()}
                    type="button"
                  >
                    重试
                  </button>
                </div>
              ) : null}
              {!submissionsQuery.isPending &&
              !submissionsQuery.isError &&
              !currentSubmission ? (
                <div className="task-output-error" role="alert">
                  <AlertTriangle size={13} />
                  <span>
                    任务处于待验收，但当前提交记录不可用。请刷新后重试。
                  </span>
                </div>
              ) : null}
              {currentSubmission ? (
                <>
                  <SubmissionSummary submission={currentSubmission} />
                  <div className="task-artifact-list">
                    {currentSubmission.artifacts.length > 0 ? (
                      currentSubmission.artifacts.map(renderArtifact)
                    ) : (
                      <div className="task-output-state">
                        本批次仅提交了摘要。
                      </div>
                    )}
                  </div>
                  {!reviewer ? (
                    <div className="task-output-prerequisites">
                      <AlertTriangle size={14} />
                      <div>
                        <strong>缺少审核人</strong>
                        <span>请先设置所有者为审核人，再进行验收。</span>
                      </div>
                    </div>
                  ) : null}
                  <div className="task-review-actions">
                    <button
                      className="button button-primary"
                      disabled={writeDisabled || !reviewReady}
                      onClick={() => {
                        setReviewEditor((current) =>
                          current?.decision === "accept"
                            ? {
                                ...current,
                                expectedVersion: Math.max(
                                  current.expectedVersion,
                                  task.version,
                                ),
                              }
                            : {
                                decision: "accept",
                                reason: "",
                                expectedVersion: task.version,
                              },
                        );
                        setReviewEditorOpen(true);
                        setReviewConflict(null);
                        setValidationError(null);
                      }}
                      type="button"
                    >
                      <Check size={13} /> 接受并完成
                    </button>
                    <button
                      className="button button-secondary"
                      disabled={writeDisabled || !reviewReady}
                      onClick={() => {
                        setReviewEditor((current) =>
                          current?.decision === "request_changes"
                            ? {
                                ...current,
                                expectedVersion: Math.max(
                                  current.expectedVersion,
                                  task.version,
                                ),
                              }
                            : {
                                decision: "request_changes",
                                reason: "",
                                expectedVersion: task.version,
                              },
                        );
                        setReviewEditorOpen(true);
                        setReviewConflict(null);
                        setValidationError(null);
                      }}
                      type="button"
                    >
                      <RotateCcw size={13} /> 要求返工
                    </button>
                  </div>
                </>
              ) : null}
            </div>
          ) : null}

          {task.status === "blocked" ? (
            <div className="task-output-policy-note">
              <AlertTriangle size={14} />
              <div>
                <strong>任务当前处于阻塞</strong>
                <span>解除阻塞后，才能继续提交或验收产出。</span>
              </div>
            </div>
          ) : null}
        </>
      ) : null}

      {reviewEditor && !reviewEditorOpen ? (
        <div className="task-output-retained-draft">
          <span>
            已保留
            {reviewEditor.decision === "accept" ? "接受决定" : "返工原因"}
            草稿。
          </span>
          <div>
            <button
              className="button button-quiet"
              onClick={() => setReviewEditorOpen(true)}
              ref={reviewContinueRef}
              type="button"
            >
              继续验收草稿
            </button>
            <button
              className="button button-quiet"
              onClick={() => {
                setReviewEditor(null);
                setReviewConflict(null);
                reviewMutation.reset();
              }}
              type="button"
            >
              放弃草稿
            </button>
          </div>
        </div>
      ) : null}

      {reviewEditor && reviewEditorOpen ? (
        <div className="task-output-command-editor">
          <div className="task-output-editor-heading">
            <div>
              <strong>
                {reviewEditor.decision === "accept"
                  ? "确认接受产出"
                  : "要求返工"}
              </strong>
              <span>
                {reviewEditor.decision === "accept"
                  ? "接受后任务完成，并结束当前责任分派。"
                  : "原提交与产出会保留，任务回到进行中。"}
              </span>
            </div>
            <button
              aria-label="关闭验收操作"
              className="button button-quiet"
              disabled={reviewMutation.isPending}
              onClick={() => {
                setReviewEditor(null);
                setReviewEditorOpen(false);
                setReviewConflict(null);
                reviewMutation.reset();
              }}
              type="button"
            >
              <X size={13} />
            </button>
          </div>
          {reviewEditor.decision === "request_changes" ? (
            <label className="form-field">
              <span>返工原因</span>
              <textarea
                aria-label="返工原因"
                maxLength={1_000}
                onChange={(event) =>
                  setReviewEditor((current) =>
                    current ? { ...current, reason: event.target.value } : null,
                  )
                }
                rows={3}
                value={reviewEditor.reason}
              />
              <small>{reviewEditor.reason.length}/1000</small>
            </label>
          ) : null}
          {reviewConflict ? (
            <ConflictNotice
              conflict={reviewConflict}
              message={
                reviewEditor.decision === "accept" ? "验收决定" : "返工原因"
              }
              onKeep={(version) => {
                setReviewEditor((current) =>
                  current ? { ...current, expectedVersion: version } : null,
                );
                setReviewConflict(null);
                reviewMutation.reset();
              }}
              onRetryRefresh={() =>
                void refreshConflict(
                  reviewEditor.expectedVersion,
                  setReviewConflict,
                )
              }
            />
          ) : null}
          <div className="task-output-editor-actions">
            <button
              className="button button-secondary"
              disabled={reviewMutation.isPending}
              onClick={() => {
                setReviewEditorOpen(false);
                setFocusAfterCollapse("review");
              }}
              type="button"
            >
              取消并保留
            </button>
            <button
              className={
                reviewEditor.decision === "accept"
                  ? "button button-primary"
                  : "button button-secondary"
              }
              disabled={
                writeDisabled || Boolean(reviewConflict) || !reviewReady
              }
              onClick={submitReview}
              type="button"
            >
              {reviewMutation.isPending ? "正在提交…" : "确认提交"}
            </button>
          </div>
        </div>
      ) : null}

      {deleteEditor && !deleteEditorOpen ? (
        <div className="task-output-retained-draft">
          <span>已保留“{deleteEditor.artifact.name}”的软删除确认草稿。</span>
          <div>
            <button
              className="button button-quiet"
              onClick={() => setDeleteEditorOpen(true)}
              ref={deleteContinueRef}
              type="button"
            >
              继续删除草稿
            </button>
            <button
              className="button button-quiet"
              onClick={() => {
                setDeleteEditor(null);
                setDeleteConflict(null);
                deleteMutation.reset();
              }}
              type="button"
            >
              放弃草稿
            </button>
          </div>
        </div>
      ) : null}

      {deleteEditor && deleteEditorOpen ? (
        <div className="task-output-command-editor task-output-delete-editor">
          <div className="task-output-editor-heading">
            <div>
              <strong>确认删除“{deleteEditor.artifact.name}”</strong>
              <span>仅软删除并保留审计；不会重写提交历史。</span>
            </div>
            <button
              aria-label="关闭删除产出操作"
              className="button button-quiet"
              disabled={deleteMutation.isPending}
              onClick={() => {
                setDeleteEditor(null);
                setDeleteEditorOpen(false);
                setDeleteConflict(null);
                deleteMutation.reset();
              }}
              type="button"
            >
              <X size={13} />
            </button>
          </div>
          <label className="form-field">
            <span>删除原因</span>
            <textarea
              aria-label="删除原因"
              maxLength={1_000}
              onChange={(event) =>
                setDeleteEditor((current) =>
                  current ? { ...current, reason: event.target.value } : null,
                )
              }
              rows={3}
              value={deleteEditor.reason}
            />
            <small>{deleteEditor.reason.length}/1000</small>
          </label>
          {deleteConflict ? (
            <ConflictNotice
              conflict={deleteConflict}
              message="删除目标和原因"
              onKeep={(version) => {
                setDeleteEditor((current) =>
                  current ? { ...current, expectedVersion: version } : null,
                );
                setDeleteConflict(null);
                deleteMutation.reset();
              }}
              onRetryRefresh={() =>
                void refreshConflict(
                  deleteEditor.expectedVersion,
                  setDeleteConflict,
                )
              }
            />
          ) : null}
          <div className="task-output-editor-actions">
            <button
              className="button button-secondary"
              disabled={deleteMutation.isPending}
              onClick={() => {
                setDeleteEditorOpen(false);
                setFocusAfterCollapse("delete");
              }}
              type="button"
            >
              取消并保留
            </button>
            <button
              className="button button-danger"
              disabled={writeDisabled || Boolean(deleteConflict)}
              onClick={confirmDelete}
              type="button"
            >
              {deleteMutation.isPending ? "正在删除…" : "确认软删除"}
            </button>
          </div>
        </div>
      ) : null}

      {validationError ? (
        <div className="task-output-error" role="alert">
          <AlertTriangle size={13} /> {validationError}
        </div>
      ) : null}
      {mutationError ? (
        <div className="task-output-error" role="alert">
          <AlertTriangle size={13} /> {mutationError}
        </div>
      ) : null}
      {downloadError ? (
        <div className="task-output-error" role="alert">
          <AlertTriangle size={13} /> {downloadError}
        </div>
      ) : null}

      <div className="task-output-history">
        <button
          aria-controls={historyPanelId}
          aria-expanded={historyOpen}
          className="button button-quiet"
          onClick={() => setHistoryOpen((value) => !value)}
          type="button"
        >
          <History size={13} />
          {historyOpen
            ? "收起提交历史"
            : historyTotal && historyTotal > 0
              ? `提交历史 ${historyTotal}`
              : "提交历史"}
        </button>
        {historyOpen ? (
          <div className="task-output-history-panel" id={historyPanelId}>
            {submissionsQuery.isPending ? (
              <div
                aria-live="polite"
                className="task-output-state"
                role="status"
              >
                <LoaderCircle className="spin" size={13} /> 正在读取提交历史…
              </div>
            ) : null}
            {submissionsQuery.isError ? (
              <div className="task-output-error" role="alert">
                <AlertTriangle size={13} />
                <span>提交历史读取失败。</span>
                <button
                  className="form-inline-action"
                  onClick={() => void submissionsQuery.refetch()}
                  type="button"
                >
                  重试
                </button>
              </div>
            ) : null}
            {!submissionsQuery.isPending &&
            !submissionsQuery.isError &&
            submissions.length === 0 ? (
              <div className="task-output-state">暂无提交记录</div>
            ) : null}
            {submissions.map((submission) => (
              <article className="task-submission-card" key={submission.id}>
                <SubmissionSummary submission={submission} />
                <div className="task-artifact-list">
                  {submission.artifacts.map(renderArtifact)}
                </div>
              </article>
            ))}
            {submissionsQuery.hasNextPage ? (
              <button
                className="button button-secondary task-output-load-more"
                disabled={submissionsQuery.isFetchingNextPage}
                onClick={() => void submissionsQuery.fetchNextPage()}
                type="button"
              >
                {submissionsQuery.isFetchingNextPage
                  ? "正在读取…"
                  : "加载更早提交"}
              </button>
            ) : null}
          </div>
        ) : null}
      </div>

      <div className="task-output-history">
        <button
          aria-controls={artifactsPanelId}
          aria-expanded={artifactsOpen}
          className="button button-quiet"
          onClick={() => setArtifactsOpen((value) => !value)}
          type="button"
        >
          <Paperclip size={13} />
          {artifactsOpen ? "收起全部产出" : "查看全部产出（含已删除）"}
        </button>
        {artifactsOpen ? (
          <div className="task-output-history-panel" id={artifactsPanelId}>
            {artifactsQuery.isPending ? (
              <div
                aria-live="polite"
                className="task-output-state"
                role="status"
              >
                <LoaderCircle className="spin" size={13} /> 正在读取全部产出…
              </div>
            ) : null}
            {artifactsQuery.isError ? (
              <div className="task-output-error" role="alert">
                <AlertTriangle size={13} />
                <span>产出列表读取失败。</span>
                <button
                  className="form-inline-action"
                  onClick={() => void artifactsQuery.refetch()}
                  type="button"
                >
                  重试
                </button>
              </div>
            ) : null}
            {!artifactsQuery.isPending &&
            !artifactsQuery.isError &&
            artifacts.length === 0 ? (
              <div className="task-output-state">暂无产出</div>
            ) : null}
            <div className="task-artifact-list">
              {artifacts.map(renderArtifact)}
            </div>
            {artifactsQuery.hasNextPage ? (
              <button
                className="button button-secondary task-output-load-more"
                disabled={artifactsQuery.isFetchingNextPage}
                onClick={() => void artifactsQuery.fetchNextPage()}
                type="button"
              >
                {artifactsQuery.isFetchingNextPage
                  ? "正在读取…"
                  : "加载更多产出"}
              </button>
            ) : null}
          </div>
        ) : null}
      </div>
    </section>
  );
}
