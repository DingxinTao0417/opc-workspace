import { beforeEach, describe, expect, it, vi } from "vitest";
import { workspaceApi } from "./workspace";

const mocks = vi.hoisted(() => ({ invoke: vi.fn(), desktop: vi.fn() }));
vi.mock("./desktop", () => ({ isDesktopRuntime: mocks.desktop }));
vi.mock("@tauri-apps/api/core", () => ({ invoke: mocks.invoke }));

beforeEach(() => {
  vi.clearAllMocks();
  mocks.desktop.mockReturnValue(true);
  mocks.invoke.mockResolvedValue(undefined);
});

describe("exact local file baselines", () => {
  it("uses distinct review and release IPC without invoking a write or elevation", async () => {
    await workspaceApi.reviewFileReplacement(
      "root",
      "record",
      "undo_installed",
    );
    await workspaceApi.releaseReplacementReview("root", "ticket");
    expect(mocks.invoke.mock.calls).toEqual([
      [
        "workspace_file_replacement_review",
        { rootId: "root", recordId: "record", mode: "undo_installed" },
      ],
      [
        "workspace_file_replacement_review_release",
        { rootId: "root", reviewId: "ticket" },
      ],
    ]);
  });
  it("uses dedicated capability, v2 apply and exact cancellation commands", async () => {
    await workspaceApi.fileWriteCapability();
    await workspaceApi.applyFileReplacement(
      "root",
      "review",
      "018f0000-0000-7000-8000-000000000401",
    );
    await workspaceApi.cancelFileOperation(
      "root",
      "018f0000-0000-7000-8000-000000000401",
    );
    expect(mocks.invoke.mock.calls).toEqual([
      ["workspace_file_write_capability", undefined],
      [
        "workspace_file_replacement_apply",
        {
          rootId: "root",
          reviewId: "review",
          operationId: "018f0000-0000-7000-8000-000000000401",
        },
      ],
      [
        "workspace_file_operation_cancel",
        {
          rootId: "root",
          operationId: "018f0000-0000-7000-8000-000000000401",
        },
      ],
    ]);
  });
  it("inspects replacement records through a separate read-only command", async () => {
    await workspaceApi.fileReplacementPreview("root", "record");
    expect(mocks.invoke).toHaveBeenCalledWith(
      "workspace_file_replacement_preview",
      {
        rootId: "root",
        recordId: "record",
      },
    );
    mocks.desktop.mockReturnValue(false);
    await expect(
      workspaceApi.fileReplacementPreview("root", "record"),
    ).rejects.toThrow("桌面端");
    expect(mocks.invoke).toHaveBeenCalledTimes(1);
  });
  it("uses separate local apply and recovery commands, never shell or model APIs", async () => {
    await workspaceApi.applyFileEdit(
      "root",
      "snapshot",
      "operation",
      "\ufeffnew\r\n",
    );
    await workspaceApi.fileRecoveryList("root");
    await workspaceApi.fileRecoveryPreview("root", "record");
    await workspaceApi.applyFileRecovery(
      "root",
      "record",
      "snapshot",
      "restore-operation",
    );
    expect(mocks.invoke.mock.calls).toEqual([
      [
        "workspace_file_edit_apply",
        {
          rootId: "root",
          snapshotId: "snapshot",
          operationId: "operation",
          content: "\ufeffnew\r\n",
        },
      ],
      ["workspace_file_recovery_list", { rootId: "root" }],
      [
        "workspace_file_recovery_preview",
        { rootId: "root", recordId: "record" },
      ],
      [
        "workspace_file_recovery_apply",
        {
          rootId: "root",
          recordId: "record",
          snapshotId: "snapshot",
          operationId: "restore-operation",
        },
      ],
    ]);
  });
  it("uses dedicated read/validate/release commands without falling back to lossy previews", async () => {
    const exact = {
      id: "snapshot-id",
      path: "src/main.rs",
      content: "\ufeff你好\r\n",
      size: 11,
      sha256: "digest",
      expiresInSeconds: 600,
    };
    mocks.invoke.mockResolvedValueOnce(exact);
    expect(await workspaceApi.fileSnapshot("root-id", "src/main.rs")).toBe(
      exact,
    );
    await workspaceApi.validateFileSnapshot("root-id", "snapshot-id");
    await workspaceApi.releaseFileSnapshot("root-id", "snapshot-id");
    expect(mocks.invoke.mock.calls).toEqual([
      ["workspace_file_snapshot", { rootId: "root-id", path: "src/main.rs" }],
      [
        "workspace_file_snapshot_validate",
        { rootId: "root-id", snapshotId: "snapshot-id" },
      ],
      [
        "workspace_file_snapshot_release",
        { rootId: "root-id", snapshotId: "snapshot-id" },
      ],
    ]);
  });

  it("does not retry or substitute preview text after a native rejection", async () => {
    mocks.invoke.mockRejectedValueOnce(new Error("文件不是有效 UTF-8"));
    await expect(
      workspaceApi.fileSnapshot("root-id", "file.txt"),
    ).rejects.toThrow("UTF-8");
    expect(mocks.invoke).toHaveBeenCalledTimes(1);
  });

  it("rejects all baseline operations outside the desktop runtime", async () => {
    mocks.desktop.mockReturnValue(false);
    await expect(workspaceApi.fileSnapshot("root", "file.txt")).rejects.toThrow(
      "桌面端",
    );
    await expect(
      workspaceApi.validateFileSnapshot("root", "snapshot"),
    ).rejects.toThrow("桌面端");
    await expect(
      workspaceApi.releaseFileSnapshot("root", "snapshot"),
    ).rejects.toThrow("桌面端");
    expect(mocks.invoke).not.toHaveBeenCalled();
  });
});
