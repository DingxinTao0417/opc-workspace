import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "./client";
import {
  inboxQueryKey,
  useCreateBackup,
  useDownloadBackupArchive,
  useDrillBackupRestore,
  useScheduleBackupRestore,
  useVerifyBackup,
} from "./hooks";

const createBackupMock = vi.hoisted(() => vi.fn());
const downloadBackupArchiveMock = vi.hoisted(() => vi.fn());
const drillBackupRestoreMock = vi.hoisted(() => vi.fn());
const scheduleBackupRestoreMock = vi.hoisted(() => vi.fn());
const verifyBackupMock = vi.hoisted(() => vi.fn());

vi.mock("./client", async () => {
  const actual = await vi.importActual<typeof import("./client")>("./client");
  return {
    ...actual,
    createBackup: createBackupMock,
    downloadBackupArchive: downloadBackupArchiveMock,
    drillBackupRestore: drillBackupRestoreMock,
    scheduleBackupRestore: scheduleBackupRestoreMock,
    verifyBackup: verifyBackupMock,
  };
});

function createQueryClient() {
  return new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  });
}

function wrapperFor(queryClient: QueryClient) {
  return function Wrapper({ children }: PropsWithChildren) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );
  };
}

afterEach(() => vi.clearAllMocks());

describe("backup maintenance failures", () => {
  it("refreshes Inbox facts for every operation that projects an incident", async () => {
    createBackupMock.mockRejectedValue(
      new ApiError("Backup failed", {
        code: "BACKUP_CREATE_FAILED",
        status: 500,
      }),
    );
    verifyBackupMock.mockRejectedValue(
      new ApiError("Verification failed", {
        code: "BACKUP_VERIFY_FAILED",
        status: 500,
      }),
    );
    drillBackupRestoreMock.mockRejectedValue(
      new ApiError("Drill failed", {
        code: "BACKUP_DRILL_FAILED",
        status: 500,
      }),
    );
    scheduleBackupRestoreMock.mockRejectedValue(
      new ApiError("Restore failed", {
        code: "RESTORE_SCHEDULE_FAILED",
        status: 500,
      }),
    );
    const queryClient = createQueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(
      () => ({
        create: useCreateBackup(),
        verify: useVerifyBackup(),
        drill: useDrillBackupRestore(),
        restore: useScheduleBackupRestore(),
      }),
      { wrapper: wrapperFor(queryClient) },
    );

    act(() => {
      result.current.create.mutate({ note: "故障演练" });
      result.current.verify.mutate("backup-1");
      result.current.drill.mutate("backup-1");
      result.current.restore.mutate("backup-1");
    });

    await waitFor(() => {
      expect(result.current.create.isError).toBe(true);
      expect(result.current.verify.isError).toBe(true);
      expect(result.current.drill.isError).toBe(true);
      expect(result.current.restore.isError).toBe(true);
    });
    expect(invalidate).toHaveBeenCalledTimes(4);
    expect(invalidate).toHaveBeenCalledWith({ queryKey: inboxQueryKey });
  });

  it("does not refresh Inbox for a capacity refusal that creates no incident", async () => {
    createBackupMock.mockRejectedValue(
      new ApiError("Insufficient space", {
        code: "BACKUP_SPACE_INSUFFICIENT",
        status: 507,
      }),
    );
    const queryClient = createQueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useCreateBackup(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() => result.current.mutate({ note: "容量边界" }));

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(invalidate).not.toHaveBeenCalled();
  });
});

describe("useDownloadBackupArchive", () => {
  it("passes the backup id through a pure mutation without invalidating caches", async () => {
    const archive = {
      blob: new Blob([new Uint8Array([80, 75, 3, 4])], {
        type: "application/zip",
      }),
      fileName: "opc-workspace-backup-backup-1.zip",
      backupId: "backup-1",
      formatVersion: 1 as const,
    };
    downloadBackupArchiveMock.mockResolvedValue(archive);
    const queryClient = createQueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const remove = vi.spyOn(queryClient, "removeQueries");
    const { result } = renderHook(() => useDownloadBackupArchive(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() => result.current.mutate("backup-1"));

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(downloadBackupArchiveMock).toHaveBeenCalledWith("backup-1");
    expect(result.current.data).toEqual(archive);
    expect(invalidate).not.toHaveBeenCalled();
    expect(remove).not.toHaveBeenCalled();
  });
});
