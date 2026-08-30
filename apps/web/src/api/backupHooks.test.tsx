import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useDownloadBackupArchive } from "./hooks";

const downloadBackupArchiveMock = vi.hoisted(() => vi.fn());

vi.mock("./client", async () => {
  const actual = await vi.importActual<typeof import("./client")>("./client");
  return {
    ...actual,
    downloadBackupArchive: downloadBackupArchiveMock,
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
