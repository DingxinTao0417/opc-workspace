import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../api/client";
import type {
  BackupSummary,
  BusinessImportPreview,
  BusinessPackageImportPreview,
  ScheduledBackupRestoreResult,
  ScheduledBackupPolicy,
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
  kind: "manual",
};

const scheduledPolicy: ScheduledBackupPolicy = {
  enabled: false,
  localTime: "02:00",
  timezone: "UTC",
  retentionCount: 30,
  lastAttemptedDate: null,
  lastAttemptAt: null,
  lastSuccessAt: null,
  lastBackupId: null,
  lastStatus: "idle",
  lastErrorCode: null,
  version: 1,
  updatedAt: "2026-08-29T12:00:00Z",
  nextRunAt: null,
};

const mocks = vi.hoisted(() => ({
  backupItems: [] as BackupSummary[],
  create: vi.fn(),
  createError: null as Error | null,
  verify: vi.fn(),
  drill: vi.fn(),
  restore: vi.fn(),
  restoreError: null as Error | null,
  deleteBackup: vi.fn(),
  downloadArchive: vi.fn(),
  downloadArchiveError: null as Error | null,
  downloadArchivePending: false,
  downloadArchiveReset: vi.fn(),
  exportData: vi.fn(),
  exportPackage: vi.fn(),
  previewImport: vi.fn(),
  importPreviewData: null as BusinessImportPreview | null,
  applyImport: vi.fn(),
  applyImportError: null as Error | null,
  previewPackageImport: vi.fn(),
  packageImportPreviewData: null as BusinessPackageImportPreview | null,
  applyPackageImport: vi.fn(),
  applyPackageImportError: null as Error | null,
  restartApplication: vi.fn(),
  reset: vi.fn(),
  refetch: vi.fn(),
  updateScheduledPolicy: vi.fn(),
  updateScheduledPolicyError: null as Error | null,
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
    data: mocks.backupItems,
    isPending: false,
    isFetching: false,
    isError: false,
    error: null,
    refetch: mocks.refetch,
  }),
  useScheduledBackupPolicyQuery: () => ({
    data: scheduledPolicy,
    isPending: false,
    isFetching: false,
    isError: false,
    error: null,
    refetch: mocks.refetch,
  }),
  useUpdateScheduledBackupPolicy: () => ({
    mutate: mocks.updateScheduledPolicy,
    reset: mocks.reset,
    isPending: false,
    error: mocks.updateScheduledPolicyError,
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
    error: mocks.restoreError,
  }),
  useDeleteBackup: () => ({
    mutate: mocks.deleteBackup,
    reset: mocks.reset,
    isPending: false,
    error: null,
  }),
  useDownloadBackupArchive: () => ({
    mutate: mocks.downloadArchive,
    reset: mocks.downloadArchiveReset,
    isPending: mocks.downloadArchivePending,
    error: mocks.downloadArchiveError,
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
    data: mocks.importPreviewData,
    isPending: false,
    error: null,
  }),
  useApplyBusinessDataImport: () => ({
    mutate: mocks.applyImport,
    reset: mocks.reset,
    isPending: false,
    error: mocks.applyImportError,
  }),
  usePreviewBusinessPackageImport: () => ({
    mutate: mocks.previewPackageImport,
    reset: mocks.reset,
    data: mocks.packageImportPreviewData,
    isPending: false,
    error: null,
  }),
  useApplyBusinessPackageImport: () => ({
    mutate: mocks.applyPackageImport,
    reset: mocks.reset,
    isPending: false,
    error: mocks.applyPackageImportError,
  }),
}));

describe("BackupSettings", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  beforeEach(() => {
    mocks.backupItems = [backup];
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
    mocks.importPreviewData = {
      formatVersion: 1,
      schemaVersion: 28,
      targetSchemaVersion: 28,
      exportedAt: "2026-08-28T12:00:00Z",
      tableCounts: { tasks: 2 },
      totalRows: 2,
      targetRows: 0,
      keyConflicts: 0,
      conflictTables: [],
      canApply: true,
      applyMode: "replace_empty",
      blocker: null,
    };
    mocks.packageImportPreviewData = {
      formatVersion: 1,
      schemaVersion: 28,
      targetSchemaVersion: 28,
      exportedAt: "2026-08-28T12:00:00Z",
      tableCounts: { tasks: 2 },
      totalRows: 2,
      targetRows: 0,
      keyConflicts: 0,
      conflictTables: [],
      fileCount: 3,
      fileBytes: 4096,
      fileConflicts: 0,
      canApply: true,
      applyMode: "replace_empty",
      blocker: null,
    };
    mocks.create.mockClear();
    mocks.createError = null;
    mocks.restoreError = null;
    mocks.applyImportError = null;
    mocks.applyPackageImportError = null;
    mocks.verify.mockClear();
    mocks.drill.mockClear();
    mocks.restore.mockClear();
    mocks.deleteBackup.mockClear();
    mocks.downloadArchive.mockReset();
    mocks.downloadArchiveReset.mockReset();
    mocks.downloadArchiveError = null;
    mocks.downloadArchivePending = false;
    mocks.exportData.mockClear();
    mocks.exportPackage.mockClear();
    mocks.previewImport.mockClear();
    mocks.applyImport.mockClear();
    mocks.previewPackageImport.mockClear();
    mocks.applyPackageImport.mockClear();
    mocks.restartApplication.mockReset();
    mocks.updateScheduledPolicy.mockReset();
    mocks.updateScheduledPolicyError = null;
    mocks.restartApplication.mockResolvedValue(true);
    mocks.reset.mockClear();
    mocks.refetch.mockClear();
  });

  it("downloads a complete backup archive and revokes its object URL", async () => {
    const createObjectURL = vi.fn(() => "blob:complete-backup");
    const revokeObjectURL = vi.fn();
    vi.stubGlobal("URL", { createObjectURL, revokeObjectURL });
    const anchorClick = vi
      .spyOn(HTMLAnchorElement.prototype, "click")
      .mockImplementation(() => undefined);
    const blob = new Blob(["complete backup"], { type: "application/zip" });
    mocks.downloadArchive.mockImplementationOnce(
      (
        id: string,
        options: {
          onSuccess: (result: {
            blob: Blob;
            fileName: string;
            backupId: string;
            formatVersion: number;
          }) => void;
          onSettled: () => void;
        },
      ) => {
        options.onSuccess({
          blob,
          fileName: "opc-workspace-backup.zip",
          backupId: id,
          formatVersion: 1,
        });
        options.onSettled();
      },
    );

    render(<BackupSettings />);
    expect(
      screen.getByText(/完整备份包含 SQLite、工作区身份、历史和受控文件/),
    ).toBeVisible();
    fireEvent.click(
      screen.getByRole("button", { name: /下载完整备份 018f0000/ }),
    );

    expect(mocks.downloadArchive).toHaveBeenCalledWith(
      backup.id,
      expect.objectContaining({
        onSuccess: expect.any(Function),
        onSettled: expect.any(Function),
      }),
    );
    expect(createObjectURL).toHaveBeenCalledWith(blob);
    expect(anchorClick).toHaveBeenCalledOnce();
    expect(anchorClick.mock.instances[0]).toMatchObject({
      download: "opc-workspace-backup.zip",
      href: "blob:complete-backup",
    });
    expect(
      screen.getByText("敏感的完整本机备份已保存：opc-workspace-backup.zip"),
    ).toBeVisible();
    await waitFor(() =>
      expect(revokeObjectURL).toHaveBeenCalledWith("blob:complete-backup"),
    );
    expect(mocks.refetch).not.toHaveBeenCalled();
  });

  it("locks duplicate exports while showing progress only on the target card", () => {
    const secondBackup: BackupSummary = {
      ...backup,
      id: "028f0000-0000-7000-8000-000000001702",
      note: "未校验备份",
      verifiedAt: null,
      verificationStatus: "unverified",
    };
    mocks.backupItems = [backup, secondBackup];
    mocks.downloadArchive.mockImplementationOnce(() => {
      mocks.downloadArchivePending = true;
    });

    render(<BackupSettings />);
    const firstCard = screen.getByText("提交前检查点").closest("article");
    const secondCard = screen.getByText("未校验备份").closest("article");
    expect(firstCard).not.toBeNull();
    expect(secondCard).not.toBeNull();

    fireEvent.click(
      within(firstCard!).getByRole("button", { name: /下载完整备份/ }),
    );

    const activeButton = within(firstCard!).getByRole("button", {
      name: /下载完整备份/,
    });
    const otherButton = within(secondCard!).getByRole("button", {
      name: /下载完整备份/,
    });
    expect(activeButton).toBeDisabled();
    expect(activeButton).toHaveTextContent("正在导出…");
    expect(activeButton.querySelector(".animate-spin")).not.toBeNull();
    expect(otherButton).toBeDisabled();
    expect(otherButton).toHaveTextContent("下载完整备份");
    expect(otherButton.querySelector(".animate-spin")).toBeNull();

    fireEvent.click(otherButton);
    expect(mocks.downloadArchive).toHaveBeenCalledOnce();
  });

  it("disables invalid backup exports while allowing unverified backups", () => {
    mocks.backupItems = [
      {
        ...backup,
        note: "损坏备份",
        verificationStatus: "invalid",
        error: "manifest mismatch",
      },
      {
        ...backup,
        id: "028f0000-0000-7000-8000-000000001702",
        note: "尚未校验",
        verifiedAt: null,
        verificationStatus: "unverified",
      },
    ];

    render(<BackupSettings />);

    expect(
      within(screen.getByText("损坏备份").closest("article")!).getByRole(
        "button",
        { name: /下载完整备份/ },
      ),
    ).toBeDisabled();
    expect(
      within(screen.getByText("尚未校验").closest("article")!).getByRole(
        "button",
        { name: /下载完整备份/ },
      ),
    ).toBeEnabled();
  });

  it("reports when the runtime cannot save a complete backup", () => {
    vi.stubGlobal("URL", {});
    const anchorClick = vi
      .spyOn(HTMLAnchorElement.prototype, "click")
      .mockImplementation(() => undefined);
    mocks.downloadArchive.mockImplementationOnce(
      (
        id: string,
        options: {
          onSuccess: (result: {
            blob: Blob;
            fileName: string;
            backupId: string;
            formatVersion: number;
          }) => void;
          onSettled: () => void;
        },
      ) => {
        options.onSuccess({
          blob: new Blob(["backup"]),
          fileName: "backup.zip",
          backupId: id,
          formatVersion: 1,
        });
        options.onSettled();
      },
    );

    render(<BackupSettings />);
    fireEvent.click(
      screen.getByRole("button", { name: /下载完整备份 018f0000/ }),
    );

    expect(screen.getByRole("alert")).toHaveTextContent(
      "当前运行环境不支持保存完整备份，请在桌面应用中重试。",
    );
    expect(anchorClick).not.toHaveBeenCalled();
  });

  it.each([
    {
      code: "BACKUP_INVALID",
      message: "备份完整性校验失败，请勿下载或用它恢复数据。",
    },
    {
      code: "BACKUP_NOT_FOUND",
      message: "备份不存在或已被删除，请刷新备份列表。",
    },
    {
      code: "BACKUP_EXPORT_SPACE_INSUFFICIENT",
      message: "导出完整备份所需的临时空间不足，请清理本地磁盘后重试。",
    },
    {
      code: "BACKUP_EXPORT_CAPACITY_UNAVAILABLE",
      message: "暂时无法确认完整备份导出容量，请检查本地存储后重试。",
    },
    {
      code: "BACKUP_EXPORT_FAILED",
      message: "无法生成完整备份 ZIP，请重新校验备份后重试。",
    },
    {
      code: "RESTORE_RESTART_REQUIRED",
      message: "恢复已经挂起，请关闭并重新打开应用后继续。",
    },
  ])("shows sanitized $code archive export feedback", ({ code, message }) => {
    mocks.downloadArchiveError = new ApiError("private archive path", {
      code,
      status: 500,
      requestId: "request-backup-archive",
    });

    render(<BackupSettings />);

    expect(screen.getByRole("alert")).toHaveTextContent(message);
    expect(screen.getByRole("alert")).toHaveTextContent(
      "请求 request-backup-archive",
    );
    expect(screen.getByRole("alert")).not.toHaveTextContent(
      "private archive path",
    );
  });

  it("previews scheduled backup changes immediately and restores the saved policy on cancel", () => {
    render(<BackupSettings />);
    const toggle = screen.getByRole("switch", {
      name: "启用每日计划备份",
    });
    expect(toggle).toHaveAttribute("aria-checked", "false");
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute("aria-checked", "true");
    expect(screen.getByText(/预览：每天 02:00/)).toBeInTheDocument();
    fireEvent.input(screen.getByLabelText("每日备份时间"), {
      target: { value: "04:30" },
    });
    fireEvent.change(screen.getByLabelText("自动备份保留份数"), {
      target: { value: "12" },
    });
    expect(screen.getByText(/每天 04:30.*超过 12 份/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "取消" }));
    expect(toggle).toHaveAttribute("aria-checked", "false");
    expect(screen.getByText(/关闭计划执行/)).toBeInTheDocument();

    fireEvent.click(toggle);
    fireEvent.click(screen.getByRole("button", { name: "保存计划" }));
    expect(mocks.updateScheduledPolicy).toHaveBeenCalledWith(
      {
        enabled: true,
        localTime: "02:00",
        timezone: "UTC",
        retentionCount: 30,
        expectedVersion: 1,
      },
      expect.any(Object),
    );
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
      { file: importFile, applyMode: "replace_empty" },
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
      { file: packageFile, applyMode: "replace_empty" },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );

    fireEvent.click(screen.getByRole("button", { name: "刷新备份列表" }));
    expect(mocks.refetch).toHaveBeenCalledTimes(1);
  });

  it("shows a read-only table conflict inventory for a non-empty target", () => {
    mocks.importPreviewData = {
      formatVersion: 1,
      schemaVersion: 42,
      targetSchemaVersion: 42,
      exportedAt: "2026-08-29T12:00:00Z",
      tableCounts: { clients: 2, tasks: 4 },
      totalRows: 6,
      targetRows: 5,
      keyConflicts: 2,
      conflictTables: [
        {
          table: "clients",
          incomingRows: 2,
          targetRows: 2,
          keyConflicts: 1,
        },
        {
          table: "tasks",
          incomingRows: 4,
          targetRows: 3,
          keyConflicts: 1,
        },
      ],
      canApply: false,
      applyMode: null,
      blocker: "target_key_conflicts",
    };
    render(<BackupSettings />);
    const file = new File(["{}"], "conflicts.json", {
      type: "application/json",
    });

    fireEvent.change(screen.getByLabelText("选择业务数据 JSON"), {
      target: { files: [file] },
    });

    expect(screen.getByText(/已有 5 行业务事实/)).toHaveTextContent(
      "检测到 2 条主键重叠",
    );
    expect(
      screen.getByRole("list", { name: "导入冲突清单" }),
    ).toHaveTextContent("客户源 2 · 目标 2 · 重叠 1");
    expect(screen.getByRole("button", { name: "确认导入" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "确认导入" }));
    expect(mocks.applyImport).not.toHaveBeenCalled();
  });

  it("previews and explicitly confirms a zero-conflict append", () => {
    mocks.importPreviewData = {
      formatVersion: 1,
      schemaVersion: 43,
      targetSchemaVersion: 43,
      exportedAt: "2026-08-29T12:00:00Z",
      tableCounts: { clients: 1 },
      totalRows: 1,
      targetRows: 2,
      keyConflicts: 0,
      conflictTables: [
        {
          table: "clients",
          incomingRows: 1,
          targetRows: 2,
          keyConflicts: 0,
        },
      ],
      canApply: true,
      applyMode: "append",
      blocker: null,
    };
    render(<BackupSettings />);
    const file = new File(["{}"], "append.json", {
      type: "application/json",
    });

    fireEvent.change(screen.getByLabelText("选择业务数据 JSON"), {
      target: { files: [file] },
    });

    expect(screen.getByText(/当前工作区已有 2/)).toHaveTextContent(
      "预检未发现主键重叠",
    );
    expect(
      screen.getByRole("list", { name: "目标工作区数据清单" }),
    ).toHaveTextContent("客户源 1 · 目标 2 · 重叠 0");
    fireEvent.click(screen.getByRole("button", { name: "确认导入" }));
    expect(mocks.applyImport).toHaveBeenCalledWith(
      { file, applyMode: "append" },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
  });

  it("classifies an older controlled-file package without enabling apply", () => {
    mocks.packageImportPreviewData = {
      formatVersion: 1,
      schemaVersion: 41,
      targetSchemaVersion: 42,
      exportedAt: "2026-08-29T12:00:00Z",
      tableCounts: { tasks: 2 },
      totalRows: 2,
      targetRows: 0,
      keyConflicts: 0,
      conflictTables: [],
      fileCount: 1,
      fileBytes: 1024,
      fileConflicts: 0,
      canApply: false,
      applyMode: null,
      blocker: "source_schema_older",
    };
    render(<BackupSettings />);
    const file = new File(["PK"], "older.zip", {
      type: "application/zip",
    });

    fireEvent.change(screen.getByLabelText("选择含文件业务 ZIP"), {
      target: { files: [file] },
    });

    expect(screen.getByText(/源数据为 schema v41/)).toHaveTextContent(
      "当前工作区为 v42",
    );
    expect(
      screen.getByRole("button", { name: "确认含文件导入" }),
    ).toBeDisabled();
  });

  it("blocks a controlled-file target collision without exposing paths", () => {
    mocks.packageImportPreviewData = {
      formatVersion: 1,
      schemaVersion: 43,
      targetSchemaVersion: 43,
      exportedAt: "2026-08-29T12:00:00Z",
      tableCounts: { task_artifacts: 1 },
      totalRows: 1,
      targetRows: 0,
      keyConflicts: 0,
      conflictTables: [],
      fileCount: 1,
      fileBytes: 1024,
      fileConflicts: 1,
      canApply: false,
      applyMode: null,
      blocker: "target_file_conflicts",
    };
    render(<BackupSettings />);
    const file = new File(["PK"], "collision.zip", {
      type: "application/zip",
    });

    fireEvent.change(screen.getByLabelText("选择含文件业务 ZIP"), {
      target: { files: [file] },
    });

    expect(screen.getByText(/受控存储中已有 1/)).toHaveTextContent(
      "为避免覆盖",
    );
    expect(
      screen.getByRole("button", { name: "确认含文件导入" }),
    ).toBeDisabled();
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

  it.each([
    {
      source: "restore",
      code: "RESTORE_ROLLBACK_SPACE_INSUFFICIENT",
      status: 507,
      message:
        "备份位置空间不足，无法同时创建当前数据回滚点并暂存恢复包；恢复没有被安排。",
    },
    {
      source: "restore",
      code: "RESTORE_ROLLBACK_CAPACITY_UNAVAILABLE",
      status: 503,
      message:
        "暂时无法确认恢复所需容量，请刷新容量状态并确认本地存储可用后重试。",
    },
    {
      source: "import",
      code: "IMPORT_BACKUP_SPACE_INSUFFICIENT",
      status: 507,
      message: "备份位置空间不足，无法创建导入前回滚备份；现有数据没有改变。",
    },
    {
      source: "package",
      code: "IMPORT_BACKUP_CAPACITY_UNAVAILABLE",
      status: 503,
      message:
        "暂时无法确认导入前回滚备份容量；现有数据没有改变，请检查本地存储后重试。",
    },
  ])("shows actionable $code feedback", ({ source, code, status, message }) => {
    const error = new ApiError("private storage detail", {
      code,
      status,
      requestId: "request-automatic-rollback-capacity",
    });
    if (source === "restore") mocks.restoreError = error;
    if (source === "import") mocks.applyImportError = error;
    if (source === "package") mocks.applyPackageImportError = error;

    render(<BackupSettings />);

    expect(screen.getByRole("alert")).toHaveTextContent(message);
    expect(screen.getByRole("alert")).not.toHaveTextContent(
      "private storage detail",
    );
  });

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
