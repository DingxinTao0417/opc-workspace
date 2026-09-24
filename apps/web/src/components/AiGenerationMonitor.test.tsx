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

const firstSessionId = "018f0000-0000-7000-8000-00000000abcd";
const secondSessionId = "018f0000-0000-7000-8000-00000000abce";

afterEach(() => {
  cleanup();
  useAiChatStore.setState({
    streaming: null,
    activeGenerations: [],
    streamError: null,
    lastSessionId: "",
    activeSessionId: "",
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
      session_id: firstSessionId,
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
    if (route === "/ai")
      useAiChatStore.setState({ activeSessionId: secondSessionId });
    render(
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={[route]}>
          <AiGenerationMonitor />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    await screen.findByText("AI 助手正在生成回复");
    expect(screen.getByRole("link", { name: "返回会话" })).toHaveAttribute(
      "href",
      `/ai?session=${firstSessionId}`,
    );
    fireEvent.click(screen.getByRole("button", { name: "停止生成" }));
    await waitFor(() =>
      expect(screen.queryByLabelText("AI 生成状态")).toBeNull(),
    );
    expect(cancelled).toBe(true);
  },
);

it("links each other persisted generation to its own exact session", async () => {
  const json = (data: unknown) =>
    new Response(JSON.stringify({ data }), {
      headers: { "Content-Type": "application/json" },
    });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: unknown) => {
      if (String(url).endsWith("/active-generations"))
        return json([
          {
            id: "generation-1",
            session_id: firstSessionId,
            provider_id: "provider-1",
            status: "streaming",
            content: "",
            reasoning: "",
            client_request_id: "request-1",
            persist: true,
          },
          {
            id: "generation-2",
            session_id: secondSessionId,
            provider_id: "provider-1",
            status: "streaming",
            content: "",
            reasoning: "",
            client_request_id: "request-2",
            persist: true,
          },
        ]);
      return json({});
    }),
  );
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/ai"]}>
        <AiGenerationMonitor />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  expect(await screen.findByRole("link", { name: "查看会话" })).toHaveAttribute(
    "href",
    `/ai?session=${secondSessionId}`,
  );
});

it.each([
  ["temporary", firstSessionId, false],
  ["malformed", "session-1", true],
])(
  "does not expose a deep link for a %s generation",
  async (_, sessionId, persist) => {
    const json = (data: unknown) =>
      new Response(JSON.stringify({ data }), {
        headers: { "Content-Type": "application/json" },
      });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: unknown) =>
        String(url).endsWith("/active-generations")
          ? json([
              {
                id: "generation-1",
                session_id: sessionId,
                provider_id: "provider-1",
                status: "streaming",
                content: "",
                reasoning: "",
                client_request_id: "request-1",
                persist,
              },
            ])
          : json({}),
      ),
    );
    const client = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
    render(
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={["/invoices"]}>
          <AiGenerationMonitor />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    expect(
      await screen.findByRole("link", { name: "返回会话" }),
    ).toHaveAttribute("href", "/ai");
  },
);
