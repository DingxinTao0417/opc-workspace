import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  clientAttachmentQueryKey,
  projectAttachmentQueryKey,
  projectQueryKey,
  useCreateProjectAttachment,
  useDownloadClientAttachment,
  useDownloadProjectAttachment,
} from "./hooks";

const downloadClientAttachmentMock = vi.hoisted(() => vi.fn());
const downloadProjectAttachmentMock = vi.hoisted(() => vi.fn());
const createProjectAttachmentMock = vi.hoisted(() => vi.fn());

vi.mock("./client", async () => {
  const actual = await vi.importActual<typeof import("./client")>("./client");
  return {
    ...actual,
    createProjectAttachment: createProjectAttachmentMock,
    downloadClientAttachment: downloadClientAttachmentMock,
    downloadProjectAttachment: downloadProjectAttachmentMock,
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

describe("attachment download integrity refresh", () => {
  it.each(["success", "failure"] as const)(
    "refreshes client attachment facts after %s",
    async (outcome) => {
      if (outcome === "success") {
        downloadClientAttachmentMock.mockResolvedValue({
          blob: new Blob(["file"]),
          fileName: "合同.pdf",
          mimeType: "application/pdf",
        });
      } else {
        downloadClientAttachmentMock.mockRejectedValue(
          new Error("integrity failure"),
        );
      }
      const queryClient = createQueryClient();
      const invalidate = vi.spyOn(queryClient, "invalidateQueries");
      const { result } = renderHook(() => useDownloadClientAttachment(), {
        wrapper: wrapperFor(queryClient),
      });

      act(() =>
        result.current.mutate({
          id: "attachment-1",
          clientId: "client-1",
          name: "合同.pdf",
        }),
      );
      await waitFor(() =>
        expect(
          outcome === "success"
            ? result.current.isSuccess
            : result.current.isError,
        ).toBe(true),
      );

      expect(invalidate).toHaveBeenCalledWith({
        queryKey: clientAttachmentQueryKey("client-1"),
      });
    },
  );

  it.each(["success", "failure"] as const)(
    "refreshes project attachment facts after %s",
    async (outcome) => {
      if (outcome === "success") {
        downloadProjectAttachmentMock.mockResolvedValue({
          blob: new Blob(["file"]),
          fileName: "需求.pdf",
          mimeType: "application/pdf",
        });
      } else {
        downloadProjectAttachmentMock.mockRejectedValue(
          new Error("integrity failure"),
        );
      }
      const queryClient = createQueryClient();
      const invalidate = vi.spyOn(queryClient, "invalidateQueries");
      const { result } = renderHook(() => useDownloadProjectAttachment(), {
        wrapper: wrapperFor(queryClient),
      });

      act(() =>
        result.current.mutate({
          id: "attachment-2",
          projectId: "project-1",
          name: "需求.pdf",
        }),
      );
      await waitFor(() =>
        expect(
          outcome === "success"
            ? result.current.isSuccess
            : result.current.isError,
        ).toBe(true),
      );

      expect(invalidate).toHaveBeenCalledWith({
        queryKey: projectAttachmentQueryKey("project-1"),
      });
    },
  );

  it("refreshes a successful attachment create through the Project tree once", async () => {
    createProjectAttachmentMock.mockResolvedValue({});
    const queryClient = createQueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useCreateProjectAttachment(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() =>
      result.current.mutate({
        projectId: "project-1",
        input: {
          name: "需求.pdf",
          expectedVersion: 1,
          file: new File(["file"], "需求.pdf", {
            type: "application/pdf",
          }),
        },
      }),
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(invalidate).toHaveBeenCalledTimes(1);
    expect(invalidate).toHaveBeenCalledWith({ queryKey: projectQueryKey });
  });
});
