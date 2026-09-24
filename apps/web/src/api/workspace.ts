import { isDesktopRuntime } from "./desktop";

export interface WorkspaceRoot {
  id: string;
  name: string;
  path: string;
}
export interface WorkspaceFile {
  name: string;
  path: string;
  directory: boolean;
  size: number;
}
export interface FilePreview {
  kind: "text" | "image";
  content: string;
  size: number;
  truncated: boolean;
}
// Exact native edit baseline, not an AI grant and not a truncated preview.
// Never persist this content/id or automatically add it to model messages.
export interface WorkspaceFileSnapshot {
  id: string;
  path: string;
  content: string;
  size: number;
  sha256: string;
  expiresInSeconds: number;
}
export interface GitReview {
  status: string;
  diff: string;
  branch: string;
}
export interface WorkspaceFileEditResult {
  id: string;
  status: "applied" | "cancelled" | "recovery_required" | "already_recorded";
  backupPath: string;
}
export interface WorkspaceFileWriteCapability {
  available: boolean;
  reason: string;
}
export interface WorkspaceFileRecoveryList {
  records: Array<{
    id: string;
    path: string;
    createdAt: number | null;
    restores: string | null;
    version?: 1 | 2;
  }>;
  damaged: number;
  directory: string;
  replacementDirectory?: string;
}
export type ReplacementFileState =
  "missing" | "original" | "candidate" | "changed" | "foreign" | "unavailable";
export interface WorkspaceFileReplacementPreview {
  id: string;
  path: string;
  observation:
    "source_present" | "target_missing" | "candidate_present" | "conflict";
  target: ReplacementFileState;
  parked: ReplacementFileState;
  staged: ReplacementFileState;
  currentBase64: string | null;
  originalBase64: string;
  candidateBase64: string;
}
export interface WorkspaceFileRecoveryPreview {
  id: string;
  snapshotId: string;
  path: string;
  currentBase64: string;
  originalBase64: string;
  expiresInSeconds: number;
}
export type FileRecoveryMode = "missing_target" | "undo_installed";
export interface WorkspaceFileReplacementReview extends WorkspaceFileReplacementPreview {
  reviewId: string;
  mode: FileRecoveryMode;
  expiresInSeconds: number;
}
async function command<T>(
  name: string,
  args?: Record<string, unknown>,
): Promise<T> {
  if (!isDesktopRuntime()) throw new Error("请在桌面端使用本地工作区功能");
  const { invoke } = await import("@tauri-apps/api/core");
  return invoke<T>(name, args);
}
export const workspaceApi = {
  chooseRoot: () => command<WorkspaceRoot | null>("workspace_choose_root"),
  list: (rootId: string, path: string) =>
    command<{ entries: WorkspaceFile[]; truncated: boolean }>(
      "workspace_list",
      { rootId, path },
    ),
  preview: (rootId: string, path: string) =>
    command<FilePreview>("workspace_preview", { rootId, path }),
  fileSnapshot: (rootId: string, path: string) =>
    command<WorkspaceFileSnapshot>("workspace_file_snapshot", { rootId, path }),
  // This only checks freshness now. A final write must revalidate atomically
  // with its own file access; this result must never be treated as permission.
  validateFileSnapshot: (rootId: string, snapshotId: string) =>
    command<void>("workspace_file_snapshot_validate", { rootId, snapshotId }),
  releaseFileSnapshot: (rootId: string, snapshotId: string) =>
    command<void>("workspace_file_snapshot_release", { rootId, snapshotId }),
  fileWriteCapability: () =>
    command<WorkspaceFileWriteCapability>("workspace_file_write_capability"),
  applyFileEdit: (
    rootId: string,
    snapshotId: string,
    operationId: string,
    content: string,
  ) =>
    command<WorkspaceFileEditResult>("workspace_file_edit_apply", {
      rootId,
      snapshotId,
      operationId,
      content,
    }),
  fileRecoveryList: (rootId: string) =>
    command<WorkspaceFileRecoveryList>("workspace_file_recovery_list", {
      rootId,
    }),
  fileRecoveryPreview: (rootId: string, recordId: string) =>
    command<WorkspaceFileRecoveryPreview>("workspace_file_recovery_preview", {
      rootId,
      recordId,
    }),
  fileReplacementPreview: (rootId: string, recordId: string) =>
    command<WorkspaceFileReplacementPreview>(
      "workspace_file_replacement_preview",
      {
        rootId,
        recordId,
      },
    ),
  applyFileRecovery: (
    rootId: string,
    recordId: string,
    snapshotId: string,
    operationId: string,
  ) =>
    command<WorkspaceFileEditResult>("workspace_file_recovery_apply", {
      rootId,
      recordId,
      snapshotId,
      operationId,
    }),
  reviewFileReplacement: (
    rootId: string,
    recordId: string,
    mode: FileRecoveryMode,
  ) =>
    command<WorkspaceFileReplacementReview>(
      "workspace_file_replacement_review",
      {
        rootId,
        recordId,
        mode,
      },
    ),
  releaseReplacementReview: (rootId: string, reviewId: string) =>
    command<void>("workspace_file_replacement_review_release", {
      rootId,
      reviewId,
    }),
  applyFileReplacement: (
    rootId: string,
    reviewId: string,
    operationId: string,
  ) =>
    command<WorkspaceFileEditResult>("workspace_file_replacement_apply", {
      rootId,
      reviewId,
      operationId,
    }),
  cancelFileOperation: (rootId: string, operationId: string) =>
    command<void>("workspace_file_operation_cancel", {
      rootId,
      operationId,
    }),
  review: (rootId: string, staged: boolean) =>
    command<GitReview>("workspace_git_review", { rootId, staged }),
  terminalStart: (rootId: string) =>
    command<string>("workspace_terminal_start", { rootId }),
  terminalRead: (id: string, offset: number) =>
    command<{
      data: string;
      nextOffset: number;
      dropped: boolean;
      ended: boolean;
    }>("workspace_terminal_read", { id, offset }),
  terminalWrite: (id: string, data: string) =>
    command<void>("workspace_terminal_write", { id, data }),
  terminalResize: (id: string, cols: number, rows: number) =>
    command<void>("workspace_terminal_resize", { id, cols, rows }),
  terminalClose: (id: string) =>
    command<void>("workspace_terminal_close", { id }),
};
export function workspaceError(reason: unknown): string {
  return reason instanceof Error
    ? reason.message
    : typeof reason === "string"
      ? reason
      : "操作失败，请重试";
}
