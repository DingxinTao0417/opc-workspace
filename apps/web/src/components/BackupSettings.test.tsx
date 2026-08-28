import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { BackupSummary } from "../types/models";
import { BackupSettings } from "./BackupSettings";

const backup: BackupSummary = {
  id: "018f0000-0000-7000-8000-000000001701",
  createdAt: "2026-08-28T12:00:00Z",
  verifiedAt: "2026-08-28T12:00:02Z",
  verificationStatus: "verified",
  note: "提交前检查点",
  appVersion: "0.1.0",
  apiVersion: "v1",
  schemaVersion: 17,
  artifactCount: 2,
  artifactBytes: 4096,
  databaseBytes: 65536,
  totalBytes: 69632,
  error: null,
};

const mocks = vi.hoisted(() => ({
  create: vi.fn(),
  verify: vi.fn(),
  reset: vi.fn(),
  refetch: vi.fn(),
}));

vi.mock("../api/hooks", () => ({
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
        schemaVersion: 17,
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
    error: null,
  }),
  useVerifyBackup: () => ({
    mutate: mocks.verify,
    reset: mocks.reset,
    isPending: false,
    error: null,
  }),
}));

describe("BackupSettings", () => {
  beforeEach(() => {
    mocks.create.mockClear();
    mocks.verify.mockClear();
    mocks.reset.mockClear();
    mocks.refetch.mockClear();
  });

  it("creates a noted backup and can explicitly reverify an existing package", () => {
    render(<BackupSettings />);

    expect(screen.getByText("提交前检查点")).toBeVisible();
    expect(screen.getByText(/schema v17/)).toHaveTextContent("2 个文件");

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

    fireEvent.click(screen.getByRole("button", { name: "刷新备份列表" }));
    expect(mocks.refetch).toHaveBeenCalledTimes(1);
  });
});
