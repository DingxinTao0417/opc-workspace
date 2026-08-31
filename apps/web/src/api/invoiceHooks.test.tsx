import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Invoice } from "../types/models";
import { ApiError } from "./client";
import {
  automationQueryKey,
  financialEntryQueryKey,
  incomeStatsQueryKey,
  inboxQueryKey,
  invoiceDetailQueryKey,
  invoicePdfQueryKey,
  invoiceQueryKey,
  projectQueryKey,
  roadmapMilestoneQueryKey,
  searchQueryKey,
  taskQueryKey,
  useDownloadInvoicePdf,
  useGenerateInvoicePdf,
  useTransitionInvoice,
} from "./hooks";

const transitionInvoiceMock = vi.hoisted(() => vi.fn());
const generateInvoicePdfMock = vi.hoisted(() => vi.fn());
const downloadInvoicePdfMock = vi.hoisted(() => vi.fn());

vi.mock("./client", async () => {
  const actual = await vi.importActual<typeof import("./client")>("./client");
  return {
    ...actual,
    transitionInvoice: transitionInvoiceMock,
    generateInvoicePdf: generateInvoicePdfMock,
    downloadInvoicePdf: downloadInvoicePdfMock,
  };
});

const paidInvoice: Invoice = {
  id: "invoice-1",
  invoiceNumber: "INV-202608-001",
  clientId: "client-1",
  clientName: "星河工作室",
  projectId: "project-1",
  projectName: "客户门户",
  amountMinor: 128045,
  currency: "CNY",
  status: "paid",
  issueDate: "2026-08-29",
  dueDate: "2026-09-28",
  paidDate: "2026-09-03",
  notes: "",
  financialEntryId: "entry-1",
  version: 4,
  createdAt: "2026-08-29T00:00:00Z",
  updatedAt: "2026-09-03T00:00:00Z",
};

const overdueInvoice: Invoice = {
  ...paidInvoice,
  status: "overdue",
  paidDate: null,
  financialEntryId: null,
};

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

describe("useTransitionInvoice", () => {
  it("reuses one idempotency key for the same failed command fingerprint", async () => {
    transitionInvoiceMock
      .mockRejectedValueOnce(new Error("response lost"))
      .mockResolvedValueOnce(paidInvoice);
    const input = {
      action: "mark_paid" as const,
      paidDate: "2026-09-03",
      expectedVersion: 3,
    };
    const { result } = renderHook(() => useTransitionInvoice(), {
      wrapper: wrapperFor(createQueryClient()),
    });

    act(() => result.current.mutate({ id: "invoice-1", input }));
    await waitFor(() => expect(result.current.isError).toBe(true));
    act(() => result.current.mutate({ id: "invoice-1", input }));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(transitionInvoiceMock.mock.calls[0][2]).toBeTruthy();
    expect(transitionInvoiceMock.mock.calls[1][2]).toBe(
      transitionInvoiceMock.mock.calls[0][2],
    );
  });

  it.each(["IDEMPOTENCY_REPLAY_UNAVAILABLE", "IDEMPOTENCY_CONFLICT"])(
    "rotates the transition key after permanent idempotency error %s",
    async (code) => {
      transitionInvoiceMock
        .mockRejectedValueOnce(
          new ApiError("permanent idempotency error", {
            code,
            status: 409,
          }),
        )
        .mockResolvedValueOnce(paidInvoice);
      const input = {
        action: "mark_paid" as const,
        paidDate: "2026-09-03",
        expectedVersion: 3,
      };
      const { result } = renderHook(() => useTransitionInvoice(), {
        wrapper: wrapperFor(createQueryClient()),
      });

      act(() => result.current.mutate({ id: "invoice-1", input }));
      await waitFor(() => expect(result.current.isError).toBe(true));
      act(() => result.current.mutate({ id: "invoice-1", input }));
      await waitFor(() => expect(result.current.isSuccess).toBe(true));

      expect(transitionInvoiceMock.mock.calls[0][2]).toBeTruthy();
      expect(transitionInvoiceMock.mock.calls[1][2]).toBeTruthy();
      expect(transitionInvoiceMock.mock.calls[1][2]).not.toBe(
        transitionInvoiceMock.mock.calls[0][2],
      );
    },
  );

  it("refreshes invoice, ledger, income, project, and inbox facts after payment", async () => {
    transitionInvoiceMock.mockResolvedValue(paidInvoice);
    const queryClient = createQueryClient();
    const searchKey = [
      ...searchQueryKey,
      { q: paidInvoice.invoiceNumber, types: ["invoice"] },
    ] as const;
    queryClient.setQueryData(searchKey, {
      items: [],
      meta: { page: 1, pageSize: 20, total: 0 },
    });
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useTransitionInvoice(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() =>
      result.current.mutate({
        id: "invoice-1",
        input: {
          action: "mark_paid",
          paidDate: "2026-09-03",
          expectedVersion: 3,
        },
      }),
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(
      queryClient.getQueryData(invoiceDetailQueryKey("invoice-1")),
    ).toEqual(paidInvoice);
    expect(invalidate).toHaveBeenCalledWith({ queryKey: invoiceQueryKey });
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: financialEntryQueryKey,
    });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: incomeStatsQueryKey });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: projectQueryKey });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: inboxQueryKey });
    expect(queryClient.getQueryState(searchKey)?.isInvalidated).toBe(true);
  });

  it("refreshes automation-created task facts after marking an invoice overdue", async () => {
    transitionInvoiceMock.mockResolvedValue(overdueInvoice);
    const queryClient = createQueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useTransitionInvoice(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() =>
      result.current.mutate({
        id: overdueInvoice.id,
        input: { action: "mark_overdue", expectedVersion: 3 },
      }),
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(invalidate).toHaveBeenCalledWith({ queryKey: automationQueryKey });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: taskQueryKey });
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: roadmapMilestoneQueryKey,
    });
  });
});

describe("invoice PDF hooks", () => {
  const pdfMetadata = {
    invoiceId: "invoice-1",
    fileName: "INV-202608-001.pdf",
    mimeType: "application/pdf" as const,
    sizeBytes: 4096,
    sha256: "a".repeat(64),
    generatedFromVersion: 4,
    generatedAt: "2026-09-03T00:00:00Z",
    integrityStatus: "verified" as const,
    integrityCheckedAt: "2026-09-03T00:00:01Z",
  };

  it("reuses an idempotency key for one failed generation attempt and caches success", async () => {
    generateInvoicePdfMock
      .mockRejectedValueOnce(new Error("response lost"))
      .mockResolvedValueOnce(pdfMetadata);
    const queryClient = createQueryClient();
    const { result } = renderHook(() => useGenerateInvoicePdf(), {
      wrapper: wrapperFor(queryClient),
    });
    const command = { id: "invoice-1", expectedVersion: 4 };

    act(() => result.current.mutate(command));
    await waitFor(() => expect(result.current.isError).toBe(true));
    act(() => result.current.mutate(command));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(generateInvoicePdfMock.mock.calls[0][2]).toBeTruthy();
    expect(generateInvoicePdfMock.mock.calls[1][2]).toBe(
      generateInvoicePdfMock.mock.calls[0][2],
    );
    expect(queryClient.getQueryData(invoicePdfQueryKey("invoice-1"))).toEqual(
      pdfMetadata,
    );
  });

  it("rotates the key after a stale idempotency replay is rejected", async () => {
    generateInvoicePdfMock
      .mockRejectedValueOnce(
        new ApiError("stale replay", {
          code: "IDEMPOTENCY_REPLAY_UNAVAILABLE",
          status: 409,
        }),
      )
      .mockResolvedValueOnce(pdfMetadata);
    const { result } = renderHook(() => useGenerateInvoicePdf(), {
      wrapper: wrapperFor(createQueryClient()),
    });
    const command = { id: "invoice-1", expectedVersion: 4 };

    act(() => result.current.mutate(command));
    await waitFor(() => expect(result.current.isError).toBe(true));
    act(() => result.current.mutate(command));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(generateInvoicePdfMock.mock.calls[0][2]).toBeTruthy();
    expect(generateInvoicePdfMock.mock.calls[1][2]).toBeTruthy();
    expect(generateInvoicePdfMock.mock.calls[1][2]).not.toBe(
      generateInvoicePdfMock.mock.calls[0][2],
    );
  });

  it("invalidates PDF metadata after a failed download integrity check", async () => {
    downloadInvoicePdfMock.mockRejectedValueOnce(new Error("mismatch"));
    const queryClient = createQueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useDownloadInvoicePdf(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() =>
      result.current.mutate({
        id: "invoice-1",
        name: "INV-202608-001.pdf",
      }),
    );
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(invalidate).toHaveBeenCalledWith({
      queryKey: invoicePdfQueryKey("invoice-1"),
      exact: true,
    });
  });
});
