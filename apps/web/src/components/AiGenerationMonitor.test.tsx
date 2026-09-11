import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { AiGenerationMonitor } from "./AiGenerationMonitor";
import { useAiChatStore } from "../store/aiChat";
import { resetRuntimeConnection } from "../api/client";

afterEach(() => {
  cleanup();
  useAiChatStore.setState({
    streaming: null,
    activeGenerations: [],
    streamError: null,
    lastSessionId: "",
  });
  sessionStorage.clear();
  vi.unstubAllGlobals();
  resetRuntimeConnection();
});

it.each(["/invoices", "/ai"])(
  "keeps discovered AI runs stoppable at %s even without the chat page",
  async (route) => {
    let cancelled = false;
    const row = {
      id: "generation-1",
      session_id: "session-1",
      provider_id: "provider-1",
      content: "部分回复",
      reasoning: "",
      client_request_id: "request-1",
      persist: true,
    };
    const json = (data: unknown) =>
      new Response(JSON.stringify({ data }), {
        headers: { "Content-Type": "application/json" },
      });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: unknown, init?: RequestInit) => {
        if (String(url).endsWith("/cancel")) {
          expect(init?.method).toBe("POST");
          cancelled = true;
          return json({});
        }
        if (String(url).endsWith("/active-generations"))
          return json(cancelled ? [] : [{ ...row, status: "streaming" }]);
        return json({ ...row, status: cancelled ? "cancelled" : "streaming" });
      }),
    );
    const client = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
    render(
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={[route]}>
          <AiGenerationMonitor />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    await screen.findByText("AI 助手正在生成回复");
    if (route !== "/ai")
      expect(screen.getByRole("link", { name: "返回会话" })).toHaveAttribute(
        "href",
        "/ai",
      );
    fireEvent.click(screen.getByRole("button", { name: "停止生成" }));
    await waitFor(() =>
      expect(screen.queryByLabelText("AI 生成状态")).toBeNull(),
    );
    expect(cancelled).toBe(true);
  },
);
