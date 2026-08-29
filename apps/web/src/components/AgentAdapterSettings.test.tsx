import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { resetRuntimeConnection } from "../api/client";
import { AgentAdapterSettings } from "./AgentAdapterSettings";

function response(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function adapterPayload(checked = false) {
  return {
    id: "018f0000-0000-5000-8000-000000003401",
    adapter_key: "builtin-local-text-v1",
    kind: "builtin",
    display_name: "本地文本诊断执行器",
    protocol_version: "opc-agent-pipe-v1",
    manifest: {
      execution_mode: "short_lived_process",
      capabilities: [
        "read_task_snapshot",
        "write_text_artifact",
        "write_structured_artifact",
      ],
      requirements: [
        "process_isolation",
        "network_block",
        "process_tree_cleanup",
      ],
    },
    status: "disabled",
    health_status: checked ? "blocked" : "unknown",
    health_error_code: checked ? "PLATFORM_ISOLATION_UNVERIFIED" : null,
    isolation_status: "unverified",
    execution_ready: false,
    last_health_at: checked ? "2026-08-29T12:00:00Z" : null,
    readiness: {
      can_enable: false,
      unavailable_code: "PLATFORM_ISOLATION_UNVERIFIED",
      required_gates: [
        "process_isolation",
        "network_block",
        "process_tree_cleanup",
      ],
    },
    version: 1,
    created_at: "2026-08-29T11:00:00Z",
    updated_at: "2026-08-29T12:00:00Z",
  };
}

function renderSettings() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <AgentAdapterSettings />
    </QueryClientProvider>,
  );
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  resetRuntimeConnection();
});

describe("AgentAdapterSettings", () => {
  it("registers the code-owned preset and reports blocked safety gates without running it", async () => {
    let adapter: ReturnType<typeof adapterPayload> | null = null;
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        if (init?.method === "POST" && url.endsWith("/agent-adapters")) {
          adapter = adapterPayload(false);
          return response({ data: adapter }, 201);
        }
        if (init?.method === "POST" && url.endsWith("/check")) {
          adapter = adapterPayload(true);
          return response({ data: adapter });
        }
        return response({ data: adapter ? [adapter] : [] });
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    renderSettings();
    fireEvent.click(
      await screen.findByRole("button", { name: "登记内置诊断适配器" }),
    );
    expect(await screen.findByText("内置诊断适配器已登记")).toBeTruthy();
    expect(await screen.findByText("本地文本诊断执行器")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "检查安全闸门" }));
    expect(await screen.findByText("安全诊断已完成")).toBeTruthy();
    expect(await screen.findByText("暂不可启用")).toBeTruthy();
    expect(screen.getByRole("button", { name: "启用适配器" })).toBeDisabled();
    expect(
      screen.getByText(
        "Sidecar 只登记受控清单；不接受路径、Shell、SQL、HTTP 或任意命令。",
      ),
    ).toBeTruthy();
    await waitFor(() => {
      expect(
        fetchMock.mock.calls.some(([url]) => String(url).endsWith("/check")),
      ).toBe(true);
    });
  });
});
