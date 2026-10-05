import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  useApplyBusinessDataImport,
  useApplyBusinessPackageImport,
} from "./hooks";

const mocks = vi.hoisted(() => ({
  applyData: vi.fn(),
  applyPackage: vi.fn(),
}));

vi.mock("./client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./client")>()),
  applyBusinessDataImport: mocks.applyData,
  applyBusinessPackageImport: mocks.applyPackage,
}));

function setup() {
  const queryClient = new QueryClient();
  const order: string[] = [];
  vi.spyOn(queryClient, "invalidateQueries").mockImplementation(async () => {
    order.push("refresh");
  });
  const wrapper = ({ children }: PropsWithChildren) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
  return { order, wrapper };
}

const input = {
  file: new File(["{}"], "workspace.json", { type: "application/json" }),
  applyMode: "append" as const,
  upgrade: false,
  planDigest: null,
};

describe("business import hooks", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    mocks.applyData.mockReset();
    mocks.applyPackage.mockReset();
  });

  // The refresh re-reads the settings and remounts the app, so whoever started
  // the import has to hear about the result first.
  it("reports an applied JSON import before anything is refreshed", async () => {
    mocks.applyData.mockResolvedValue({
      importedRows: 3,
      backupId: "018f0000-0000-7000-8000-000000001799",
      applyMode: "append",
    });
    const { order, wrapper } = setup();
    const { result } = renderHook(
      () =>
        useApplyBusinessDataImport({
          onApplied: (applied) => order.push(`applied ${applied.importedRows}`),
        }),
      { wrapper },
    );

    act(() => result.current.mutate(input));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(order).toEqual(["applied 3", "refresh"]);
  });

  it("reports an applied controlled-file import before anything is refreshed", async () => {
    mocks.applyPackage.mockResolvedValue({
      importedRows: 5,
      importedFiles: 2,
      backupId: "018f0000-0000-7000-8000-000000001798",
      applyMode: "append",
    });
    const { order, wrapper } = setup();
    const { result } = renderHook(
      () =>
        useApplyBusinessPackageImport({
          onApplied: (applied) =>
            order.push(`applied ${applied.importedFiles}`),
        }),
      { wrapper },
    );

    act(() => result.current.mutate(input));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(order).toEqual(["applied 2", "refresh"]);
  });

  it("does not refresh or report when the import fails", async () => {
    mocks.applyData.mockRejectedValue(new Error("IMPORT_APPLY_FAILED"));
    const { order, wrapper } = setup();
    const { result } = renderHook(
      () =>
        useApplyBusinessDataImport({
          onApplied: () => order.push("applied"),
        }),
      { wrapper },
    );

    act(() => result.current.mutate(input));
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(order).toEqual([]);
  });
});
