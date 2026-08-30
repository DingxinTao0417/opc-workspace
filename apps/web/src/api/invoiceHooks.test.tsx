import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Invoice } from "../types/models";
import {
  financialEntryQueryKey,
  incomeStatsQueryKey,
  invoiceDetailQueryKey,
  invoiceQueryKey,
  projectQueryKey,
  useTransitionInvoice,
} from "./hooks";

const transitionInvoiceMock = vi.hoisted(() => vi.fn());

vi.mock("./client", async () => {
  const actual = await vi.importActual<typeof import("./client")>("./client");
  return { ...actual, transitionInvoice: transitionInvoiceMock };
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

  it("refreshes invoice, ledger, income, and project facts after payment", async () => {
    transitionInvoiceMock.mockResolvedValue(paidInvoice);
    const queryClient = createQueryClient();
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
  });
});
