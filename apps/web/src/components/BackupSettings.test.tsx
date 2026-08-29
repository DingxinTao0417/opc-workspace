import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../api/client";
import type {
  BackupSummary,
  ScheduledBackupRestoreResult,
} from "../types/models";
import { BackupSettings } from "./BackupSettings";

const backup: BackupSummary = {
  id: "018f0000-0000-7000-8000-000000001701",
  createdAt: "2026-08-28T12:00:00Z",
  verifiedAt: "2026-08-28T12:00:02Z",
  verificationStatus: "verified",
  note: "提交前检查点",
  appVersion: "0.1.0",
  apiVersion: "v1",
  schemaVersion: 18,
  artifactCount: 2,
  artifactBytes: 4096,
  databaseBytes: 65536,
  totalBytes: 69632,
  error: null,
};

const mocks = vi.hoisted(() => ({
  create: vi.fn(),
  createError: null as Error | null,
  verify: vi.fn(),
  drill: vi.fn(),
  restore: vi.fn(),
  deleteBackup: vi.fn(),
  exportData: vi.fn(),
  exportPackage: vi.fn(),
  previewImport: vi.fn(),
  applyImport: vi.fn(),
  previewPackageImport: vi.fn(),
  applyPackageImport: vi.fn(),
  restartApplication: vi.fn(),
  reset: vi.fn(),
  refetch: vi.fn(),
  restoreDiagnostics: {
    status: "idle",
    restartRequired: false,
    appliedThisStartup: false,
    cleanupRequired: false,
    attentionRequired: false,
    backupId: null as string | null,
    rollbackBackupId: null as string | null,
    requestedAt: null as string | null,
    residualAppliedCount: 0,
    failedAttemptCount: 0,
    invalidEntryCount: 0,
  },
}));

vi.mock("../api/desktop", () => ({
  requestApplicationRestart: mocks.restartApplication,
}));

vi.mock("../api/hooks", () => ({
  useRestoreDiagnosticsQuery: () => ({
    data: mocks.restoreDiagnostics,
    isPending: false,
    isFetching: false,
    isError: false,
    error: null,
    refetch: mocks.refetch,
  }),
  useBackupsQuery: () => ({
    data: [
      {
        id: "018f0000-0000-7000-8000-000000001701",
        createdAt: "2026-08-28T12:00:00Z",
        verifiedAt: "2026-08-28T12:00:02Z",
        verificationStatus: "verified",
        note: "提交前检查点",
        appVersion: "0.1.0",
        apiVersion: "v1",
        schemaVersion: 18,
        artifactCount: 2,
        artifactBytes: 4096,
        databaseBytes: 65536,
        totalBytes: 69632,
        error: null,
      },
    ],
    isPending: false,
    isFetching: false,
    isError: false,
    error: null,
    refetch: mocks.refetch,
  }),
  useCreateBackup: () => ({
    mutate: mocks.create,
    reset: mocks.reset,
    isPending: false,
    error: mocks.createError,
  }),
  useVerifyBackup: () => ({
    mutate: mocks.verify,
    reset: mocks.reset,
    isPending: false,
    error: null,
  }),
  useDrillBackupRestore: () => ({
    mutate: mocks.drill,
    reset: mocks.reset,
    isPending: false,
    error: null,
  }),
  useScheduleBackupRestore: () => ({
    mutate: mocks.restore,
    reset: mocks.reset,
    isPending: false,
    error: null,
  }),
  useDeleteBackup: () => ({
    mutate: mocks.deleteBackup,
    reset: mocks.reset,
    isPending: false,
    error: null,
  }),
  useExportBusinessData: () => ({
    mutate: mocks.exportData,
    reset: mocks.reset,
    isPending: false,
    error: null,
  }),
  useExportBusinessPackage: () => ({
    mutate: mocks.exportPackage,
    reset: mocks.reset,
    isPending: false,
    error: null,
  }),
  usePreviewBusinessDataImport: () => ({
    mutate: mocks.previewImport,
    reset: mocks.reset,
    data: {
      formatVersion: 1,
      schemaVersion: 28,
      exportedAt: "2026-08-28T12:00:00Z",
      tableCounts: { tasks: 2 },
      totalRows: 2,
      canApply: true,
      blocker: null,
    },
    isPending: false,
    error: null,
  }),
  useApplyBusinessDataImport: () => ({
    mutate: mocks.applyImport,
    reset: mocks.reset,
    isPending: false,
    error: null,
  }),
  usePreviewBusinessPackageImport: () => ({
    mutate: mocks.previewPackageImport,
    reset: mocks.reset,
    data: {
      formatVersion: 1,
      schemaVersion: 28,
      exportedAt: "2026-08-28T12:00:00Z",
      tableCounts: { tasks: 2 },
      totalRows: 2,
      fileCount: 3,
      fileBytes: 4096,
      canApply: true,
      blocker: null,
    },
    isPending: false,
    error: null,
  }),
  useApplyBusinessPackageImport: () => ({
    mutate: mocks.applyPackageImport,
    reset: mocks.reset,
    isPending: false,
    error: null,
  }),
}));

describe("BackupSettings", () => {
  afterEach(cleanup);

  beforeEach(() => {
    Object.assign(mocks.restoreDiagnostics, {
      status: "idle",
      restartRequired: false,
      appliedThisStartup: false,
      cleanupRequired: false,
      attentionRequired: false,
      backupId: null,
      rollbackBackupId: null,
      requestedAt: null,
      residualAppliedCount: 0,
      failedAttemptCount: 0,
      invalidEntryCount: 0,
    });
    mocks.create.mockClear();
    mocks.createError = null;
    mocks.verify.mockClear();
    mocks.drill.mockClear();
    mocks.restore.mockClear();
    mocks.deleteBackup.mockClear();
    mocks.exportData.mockClear();
    mocks.exportPackage.mockClear();
    mocks.previewImport.mockClear();
    mocks.applyImport.mockClear();
    mocks.previewPackageImport.mockClear();
    mocks.applyPackageImport.mockClear();
    mocks.restartApplication.mockReset();
    mocks.restartApplication.mockResolvedValue(true);
    mocks.reset.mockClear();
    mocks.refetch.mockClear();
  });

  it("creates a noted backup and can explicitly reverify an existing package", () => {
    render(<BackupSettings />);

    expect(screen.getByText("提交前检查点")).toBeVisible();
    expect(screen.getByText(/schema v18/)).toHaveTextContent("2 个文件");

    fireEvent.change(screen.getByLabelText("备份说明"), {
      target: { value: "发布前" },
    });
    fireEvent.click(screen.getByRole("button", { name: "立即备份" }));
    expect(mocks.create).toHaveBeenCalledWith(
      { note: "发布前" },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );

    fireEvent.click(
      screen.getByRole("button", { name: /重新校验备份 018f0000/ }),
    );
    expect(mocks.verify).toHaveBeenCalledWith(
      backup.id,
      expect.objectContaining({
        onSuccess: expect.any(Function),
        onSettled: expect.any(Function),
      }),
    );

    fireEvent.click(
      screen.getByRole("button", { name: /恢复演练备份 018f0000/ }),
    );
    expect(mocks.drill).toHaveBeenCalledWith(
      backup.id,
      expect.objectContaining({
        onSuccess: expect.any(Function),
        onSettled: expect.any(Function),
      }),
    );

    fireEvent.click(screen.getByRole("button", { name: /恢复备份 018f0000/ }));
    expect(screen.getByText(/确认恢复到“提交前检查点”/)).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "确认并安排恢复" }));
    expect(mocks.restore).toHaveBeenCalledWith(
      backup.id,
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );

    fireEvent.click(screen.getByRole("button", { name: /删除备份 018f0000/ }));
    expect(screen.getByText(/永久删除“提交前检查点”/)).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "确认永久删除" }));
    expect(mocks.deleteBackup).toHaveBeenCalledWith(
      backup.id,
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );

    fireEvent.click(screen.getByRole("button", { name: "下载 JSON" }));
    expect(mocks.exportData).toHaveBeenCalledWith(
      undefined,
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );

    fireEvent.click(screen.getByRole("button", { name: "下载含文件 ZIP" }));
    expect(mocks.exportPackage).toHaveBeenCalledWith(
      undefined,
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );

    const importFile = new File(["{}"], "workspace.json", {
      type: "application/json",
    });
    fireEvent.change(screen.getByLabelText("选择业务数据 JSON"), {
      target: { files: [importFile] },
    });
    expect(mocks.previewImport).toHaveBeenCalledWith(importFile);
    fireEvent.click(screen.getByRole("button", { name: "确认导入" }));
    expect(mocks.applyImport).toHaveBeenCalledWith(
      importFile,
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );

    const packageFile = new File(["PK"], "workspace.zip", {
      type: "application/zip",
    });
    fireEvent.change(screen.getByLabelText("选择含文件业务 ZIP"), {
      target: { files: [packageFile] },
    });
    expect(mocks.previewPackageImport).toHaveBeenCalledWith(packageFile);
    expect(screen.getByText(/3 个受控文件/)).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "确认含文件导入" }));
    expect(mocks.applyPackageImport).toHaveBeenCalledWith(
      packageFile,
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );

    fireEvent.click(screen.getByRole("button", { name: "刷新备份列表" }));
    expect(mocks.refetch).toHaveBeenCalledTimes(1);
  });

  it.each([
    {
      code: "BACKUP_SPACE_INSUFFICIENT",
      status: 507,
      message: "备份位置可用空间不足，请清理备份位置或旧备份后重试。",
    },
    {
      code: "BACKUP_CAPACITY_UNAVAILABLE",
      status: 503,
      message: "暂时无法确认备份容量，请刷新容量状态并确认本地存储可用后重试。",
    },
  ])(
    "shows actionable $code feedback and keeps the unsubmitted note draft",
    ({ code, status, message }) => {
      const { rerender } = render(<BackupSettings />);
      const noteInput = screen.getByLabelText("备份说明");

      fireEvent.change(noteInput, { target: { value: "空间门禁前草稿" } });
      fireEvent.click(screen.getByRole("button", { name: "立即备份" }));

      expect(mocks.create).toHaveBeenCalledOnce();
      expect(noteInput).toHaveValue("空间门禁前草稿");

      mocks.createError = new ApiError("服务端备份失败", {
        code,
        status,
        requestId: "request-backup-space-gate",
      });
      rerender(<BackupSettings />);

      expect(screen.getByRole("alert")).toHaveTextContent(message);
      expect(screen.getByLabelText("备份说明")).toHaveValue("空间门禁前草稿");
      expect(screen.queryByText(/备份已创建并校验：/)).not.toBeInTheDocument();
      expect(mocks.create).toHaveBeenCalledOnce();
    },
  );

  it("keeps the existing request ID feedback for other backup errors", () => {
    mocks.createError = new ApiError("服务端返回未知备份错误", {
      code: "UNEXPECTED_BACKUP_ERROR",
      status: 500,
      requestId: "request-unknown-backup-error",
    });

    render(<BackupSettings />);

    expect(screen.getByRole("alert")).toHaveTextContent(
      "服务端返回未知备份错误 · 请求 request-unknown-backup-error",
    );
  });

  it("offers a safe desktop restart after a restore is scheduled", async () => {
    mocks.restore.mockImplementationOnce(
      (
        _id: string,
        options: { onSuccess: (result: ScheduledBackupRestoreResult) => void },
      ) => {
        options.onSuccess({
          backupId: backup.id,
          rollbackBackupId: "018f0000-0000-7000-8000-000000001702",
          requestedAt: "2026-08-28T12:05:00Z",
          restartRequired: true,
        });
      },
    );
    render(<BackupSettings />);

    fireEvent.click(screen.getByRole("button", { name: /恢复备份 018f0000/ }));
    fireEvent.click(screen.getByRole("button", { name: "确认并安排恢复" }));
    expect(screen.getByText(/恢复已安全挂起/)).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "立即安全重启" }));

    await waitFor(() =>
      expect(mocks.restartApplication).toHaveBeenCalledOnce(),
    );
    expect(
      screen.getByRole("button", { name: "正在安全重启…" }),
    ).toBeDisabled();
  });

  it("explains the manual fallback when the Sidecar is external", async () => {
    mocks.restartApplication.mockResolvedValueOnce(false);
    mocks.restore.mockImplementationOnce(
      (
        _id: string,
        options: { onSuccess: (result: ScheduledBackupRestoreResult) => void },
      ) => {
        options.onSuccess({
          backupId: backup.id,
          rollbackBackupId: "018f0000-0000-7000-8000-000000001702",
          requestedAt: "2026-08-28T12:05:00Z",
          restartRequired: true,
        });
      },
    );
    render(<BackupSettings />);

    fireEvent.click(screen.getByRole("button", { name: /恢复备份 018f0000/ }));
    fireEvent.click(screen.getByRole("button", { name: "确认并安排恢复" }));
    fireEvent.click(screen.getByRole("button", { name: "立即安全重启" }));

    expect(
      await screen.findByText(/浏览器开发模式不会接管 Sidecar/),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: "立即安全重启" })).toBeEnabled();
  });

  it("keeps the application open and reports a rejected safe restart", async () => {
    mocks.restartApplication.mockRejectedValueOnce(
      new Error("Sidecar 未确认安全退出，已取消应用重启"),
    );
    mocks.restore.mockImplementationOnce(
      (
        _id: string,
        options: { onSuccess: (result: ScheduledBackupRestoreResult) => void },
      ) => {
        options.onSuccess({
          backupId: backup.id,
          rollbackBackupId: "018f0000-0000-7000-8000-000000001702",
          requestedAt: "2026-08-28T12:05:00Z",
          restartRequired: true,
        });
      },
    );
    render(<BackupSettings />);

    fireEvent.click(screen.getByRole("button", { name: /恢复备份 018f0000/ }));
    fireEvent.click(screen.getByRole("button", { name: "确认并安排恢复" }));
    fireEvent.click(screen.getByRole("button", { name: "立即安全重启" }));

    expect(
      await screen.findByText("Sidecar 未确认安全退出，已取消应用重启"),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: "立即安全重启" })).toBeEnabled();
  });

  it("shows sanitized startup restore attention without inventing a success", () => {
    Object.assign(mocks.restoreDiagnostics, {
      status: "attention_required",
      cleanupRequired: true,
      attentionRequired: true,
      residualAppliedCount: 1,
      failedAttemptCount: 2,
      invalidEntryCount: 1,
    });
    render(<BackupSettings />);

    expect(screen.getByText(/发现 2 次失败记录和 1 个无效记录/)).toBeVisible();
    expect(screen.queryByText(/恢复已完成备份/)).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "重新检查" }));
    expect(mocks.refetch).toHaveBeenCalled();
  });

  it("restores the restart gate from server diagnostics after reopening settings", () => {
    Object.assign(mocks.restoreDiagnostics, {
      status: "restart_required",
      restartRequired: true,
      backupId: backup.id,
      rollbackBackupId: "018f0000-0000-7000-8000-000000001702",
      requestedAt: "2026-08-28T12:05:00Z",
    });
    render(<BackupSettings />);

    expect(screen.getByText(/恢复已安全挂起/)).toBeVisible();
    expect(screen.getByRole("button", { name: "立即备份" })).toBeDisabled();
  });
});
