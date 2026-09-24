import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import {
  MemoryRouter,
  Route,
  Routes,
  useNavigate,
  type NavigateFunction,
} from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { normalizeFinancialEntry, resetRuntimeConnection } from "../api/client";
import { financialEntryDetailQueryKey } from "../api/hooks";
import { renderAiRichText } from "../components/AiRichText";
import { useAiChatStore } from "../store/aiChat";
import { useAiWorkbenchHandoff } from "../store/aiWorkbenchHandoff";
import { FinancialEntryDetailPage } from "./FinancialEntryDetailPage";

vi.mock("../components/FinancialEntryFormModal", () => ({
  FinancialEntryFormModal: ({ open }: { open: boolean }) =>
    open ? <div role="dialog">人工编辑表单</div> : null,
}));
const id = "018f0000-0000-7000-8000-000000001710";
const session = "018f0000-0000-7000-8000-000000001711";
const other = "018f0000-0000-7000-8000-000000001712";
const date = "2026-09-18T09:00:00Z";
const entry = () => ({
  id,
  type: "income",
  amount_minor: 12345,
  currency: "CNY",
  occurred_on: "2026-09-01",
  status: "confirmed",
  category: "精确回款",
  client_id: null,
  client_name: null,
  project_id: null,
  project_name: null,
  invoice_id: null,
  invoice_number: null,
  notes: "仅在本机展示的备注",
  created_by_actor_id: other,
  voided_at: null,
  void_reason: null,
  version: 3,
  created_at: date,
  updated_at: date,
});
const response = (data: unknown, status = 200) =>
  new Response(JSON.stringify(data), {
    status,
    headers: { "Content-Type": "application/json" },
  });
const calls =
  vi.fn<(input: RequestInfo | URL, init?: RequestInit) => Promise<Response>>();
function mount(
  path = "/ai",
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, retryDelay: 0 } },
  }),
) {
  let navigate!: NavigateFunction;
  function Probe() {
    navigate = useNavigate();
    return null;
  }
  const view = render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>
        <Probe />
        <Routes>
          <Route
            path="/ai"
            element={
              <div>
                {renderAiRichText(`[打开收支](/income/${id})`, session)}
              </div>
            }
          />
          <Route
            path="/income/:entryId"
            element={<FinancialEntryDetailPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return {
    ...view,
    client,
    navigate: (url: string) => act(() => navigate(url)),
  };
}
beforeEach(() => {
  resetRuntimeConnection();
  calls.mockReset();
  calls.mockImplementation(async () => response({ data: entry() }));
  vi.stubGlobal("fetch", calls);
  useAiChatStore.setState({ activeSessionId: undefined });
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  resetRuntimeConnection();
  useAiWorkbenchHandoff.setState({ pending: null, pendingIssue: null });
});

describe("financial record exact navigation", () => {
  it("reads the actual linked identity, returns to its source chat and never writes on navigation", async () => {
    mount();
    fireEvent.click(screen.getByRole("link", { name: "打开收支" }));
    expect(await screen.findByText("备注：仅在本机展示的备注")).toBeVisible();
    expect(screen.getByRole("heading", { name: /123.45/ })).toBeVisible();
    expect(
      calls.mock.calls.every(
        ([input, init]) =>
          new URL(String(input), "http://localhost").pathname ===
            `/api/v1/financial-entries/${id}` &&
          (!init?.method || init.method === "GET"),
      ),
    ).toBe(true);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "编辑记录" }));
    expect(screen.getByRole("dialog")).toBeVisible();
    fireEvent.click(screen.getByRole("link", { name: "返回原对话" }));
    expect(useAiChatStore.getState().activeSessionId).toBe(session);
    expect(screen.getByRole("link", { name: "打开收支" })).toBeVisible();
  });

  it.each(["not_found", "wrong_identity"])(
    "does not show cached or substituted record after %s",
    async (kind) => {
      const client = new QueryClient({
        defaultOptions: { queries: { retryDelay: 0 } },
      });
      client.setQueryData(
        financialEntryDetailQueryKey(id),
        normalizeFinancialEntry(entry()),
      );
      calls.mockImplementation(async () =>
        kind === "not_found"
          ? response(
              {
                error: {
                  code: "FINANCIAL_ENTRY_NOT_FOUND",
                  message: "已不存在",
                },
              },
              404,
            )
          : response({ data: { ...entry(), id: other } }),
      );
      mount(`/income/${id}?return_session=${session}`, client);
      expect(
        screen.queryByText("备注：仅在本机展示的备注"),
      ).not.toBeInTheDocument();
      expect(await screen.findByText("财务记录不可用")).toBeVisible();
      expect(
        screen.queryByRole("button", { name: "编辑记录" }),
      ).not.toBeInTheDocument();
      expect(screen.getByRole("link", { name: "返回原对话" })).toBeVisible();
      client.clear();
    },
  );

  it("aborts an in-flight detail request when leaving", async () => {
    let signal: AbortSignal | null | undefined;
    calls.mockImplementation((_input, init) => {
      signal = init?.signal;
      return new Promise((_resolve, reject) =>
        signal?.addEventListener("abort", () =>
          reject(new DOMException("Aborted", "AbortError")),
        ),
      );
    });
    const view = mount(`/income/${id}`);
    await waitFor(() => expect(signal).toBeDefined());
    view.navigate("/ai");
    await waitFor(() => expect(signal?.aborted).toBe(true));
  });

  it.each(["voided", "invoice"])("keeps %s records read-only", async (kind) => {
    calls.mockResolvedValue(
      response({
        data: {
          ...entry(),
          ...(kind === "voided"
            ? { status: "voided", voided_at: date, void_reason: "重复" }
            : { invoice_id: other, invoice_number: "INV-001" }),
        },
      }),
    );
    mount(`/income/${id}?return_session=${session}`);
    expect(await screen.findByText("备注：仅在本机展示的备注")).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "编辑记录" }),
    ).not.toBeInTheDocument();
    if (kind === "invoice")
      expect(screen.getByRole("link", { name: "INV-001" })).toHaveAttribute(
        "href",
        `/invoices/${other}?return_session=${session}`,
      );
  });

  it("does not read malformed identities or unexpected query parameters", () => {
    const view = mount("/income/not-an-id");
    expect(screen.getByRole("alert")).toHaveTextContent("链接无效");
    view.navigate(`/income/${id}?confirm=true`);
    expect(screen.getByRole("alert")).toHaveTextContent("链接无效");
    expect(calls).not.toHaveBeenCalled();
  });

  it("hands the exact financial record to the agent with finance action scope", async () => {
    mount(`/income/${id}`);
    expect(await screen.findByText("备注：仅在本机展示的备注")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "交给智能体" }));
    const pending = useAiWorkbenchHandoff.getState().pendingIssue;
    expect(pending?.label).toBe("收支记录");
    expect(pending?.route).toBe(`/income/${id}`);
    expect(pending?.scopes).toEqual(["finance", "finance_actions"]);
    expect(pending?.prompt).toContain("workspace_finance");
    expect(pending?.prompt).toContain(`id=${id}`);
    expect(pending?.prompt).toContain("不要假设备注、外部付款或银行到账事实");
    expect(useAiWorkbenchHandoff.getState().pending).toBeNull();
    expect(screen.getByRole("link", { name: "打开收支" })).toBeVisible();
  });
});
