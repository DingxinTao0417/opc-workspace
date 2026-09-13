import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
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

function adapterPayload({
  health = "healthy",
  enabled = false,
  version = 1,
}: {
  health?: "unknown" | "blocked" | "healthy" | "unhealthy";
  enabled?: boolean;
  version?: number;
} = {}) {
  const ready = health === "healthy";
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
    status: enabled ? "enabled" : "disabled",
    health_status: health,
    health_error_code:
      health === "blocked" ? "PLATFORM_ISOLATION_UNVERIFIED" : null,
    isolation_status: ready ? "verified" : "unverified",
    execution_ready: ready,
    last_health_at: health === "unknown" ? null : "2026-09-12T12:00:00Z",
    readiness: {
      can_enable: ready,
      ...(ready ? {} : { unavailable_code: "PLATFORM_ISOLATION_UNVERIFIED" }),
      required_gates: [
        "process_isolation",
        "network_block",
        "process_tree_cleanup",
      ],
    },
    version,
    created_at: "2026-09-12T11:00:00Z",
    updated_at: "2026-09-12T12:00:00Z",
  };
}

type AdapterPayload = ReturnType<typeof adapterPayload>;

function mockAdapterAPI(
  initial: AdapterPayload | null,
  onAction?: (action: string) => Promise<Response | void>,
) {
  let stored = initial;
  const fetchMock = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (init?.method === "POST") {
        const action = url.split("/").at(-1)!;
        const override = await onAction?.(action);
        if (override) return override;
        if (action === "agent-adapters") {
          stored = adapterPayload();
        } else if (action === "check") {
          stored = adapterPayload({
            enabled: stored?.status === "enabled",
            version: stored?.version,
          });
        } else if (action === "enable" || action === "disable") {
          stored = adapterPayload({
            enabled: action === "enable",
            version: stored!.version + 1,
          });
        } else {
          throw new Error(`Unexpected write: ${url}`);
        }
        return response(
          { data: stored },
          action === "agent-adapters" ? 201 : 200,
        );
      }
      if (!url.endsWith("/agent-adapters")) {
        throw new Error(`Unexpected read: ${url}`);
      }
      return response({ data: stored ? [stored] : [] });
    },
  );
  vi.stubGlobal("fetch", fetchMock);
  return {
    fetchMock,
    setAdapter(value: AdapterPayload) {
      stored = value;
    },
    writes() {
      return fetchMock.mock.calls.filter(([, init]) => init?.method === "POST");
    },
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
  it("registers the code-owned preset without automatically enabling or running it", async () => {
    const api = mockAdapterAPI(null);
    renderSettings();
    fireEvent.click(
      await screen.findByRole("button", { name: "登记内置适配器" }),
    );

    expect(
      await screen.findByText("内置适配器已登记，尚未启用。"),
    ).toBeTruthy();
    expect(screen.getByText("未启用 · 健康")).toBeTruthy();
    expect(screen.getByRole("button", { name: "启用适配器" })).toBeEnabled();
    expect(api.writes()).toHaveLength(1);
    expect(JSON.parse(String(api.writes()[0][1]?.body))).toEqual({
      preset_key: "builtin-local-text-v1",
    });
    expect(
      new Headers(api.writes()[0][1]?.headers).get("Idempotency-Key"),
    ).toBeTruthy();
    expect(screen.queryByText("网络阻断")).not.toBeInTheDocument();
    expect(screen.getByText(/不代表已通过操作系统沙箱或禁网验证/)).toBeTruthy();
  });

  it("checks unknown readiness and shows the healthy lifecycle result", async () => {
    const api = mockAdapterAPI(
      adapterPayload({ health: "unknown", version: 3 }),
    );
    renderSettings();
    expect(await screen.findByText("未启用 · 未检查")).toBeTruthy();
    expect(screen.getByRole("button", { name: "启用适配器" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "检查运行条件" }));

    expect(
      await screen.findByText("内置执行器生命周期检查已通过。"),
    ).toBeTruthy();
    expect(screen.getByText("未启用 · 健康")).toBeTruthy();
    expect(screen.getByRole("button", { name: "启用适配器" })).toBeEnabled();
    expect(new Headers(api.writes()[0][1]?.headers).get("If-Match")).toBe(
      '"3"',
    );
    expect(api.writes()).toHaveLength(1);
  });

  it.each(["blocked", "unhealthy"] as const)(
    "keeps an %s adapter disabled and reports the check result without enabling it",
    async (health) => {
      const blocked = adapterPayload({ health });
      const api = mockAdapterAPI(blocked, async () =>
        response({ data: blocked }),
      );
      renderSettings();
      fireEvent.click(await screen.findByRole("button", { name: "重新检查" }));
      expect(
        await screen.findByText("检查已完成，当前运行条件尚未就绪。"),
      ).toBeTruthy();
      expect(screen.getByText("暂不可启用")).toBeTruthy();
      fireEvent.click(screen.getByRole("button", { name: "启用适配器" }));
      expect(api.writes()).toHaveLength(1);
      expect(String(api.writes()[0][0])).toMatch(/\/check$/);
    },
  );

  it("explicitly enables and disables with the latest adapter version", async () => {
    const api = mockAdapterAPI(adapterPayload({ version: 5 }));
    renderSettings();
    fireEvent.click(await screen.findByRole("button", { name: "启用适配器" }));
    expect(
      await screen.findByText("适配器已启用；请到任务详情启动执行。"),
    ).toBeTruthy();
    expect(screen.getByText("已启用 · 健康")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "停用适配器" }));
    expect(
      await screen.findByText("适配器已停用，不再接受新的 Agent 分派或执行。"),
    ).toBeTruthy();
    expect(screen.getByText("未启用 · 健康")).toBeTruthy();

    expect(api.writes().map(([url]) => String(url).split("/").at(-1))).toEqual([
      "enable",
      "disable",
    ]);
    expect(
      api
        .writes()
        .map(([, init]) => new Headers(init?.headers).get("If-Match")),
    ).toEqual(['"5"', '"6"']);
  });

  it.each([
    ["register", "登记内置适配器", "正在登记…"],
    ["check", "重新检查", "正在检查…"],
    ["enable", "启用适配器", "正在启用…"],
    ["disable", "停用适配器", "正在停用…"],
  ])(
    "serializes all actions while %s is pending",
    async (action, label, pendingLabel) => {
      let release!: () => void;
      const pending = new Promise<void>((resolve) => {
        release = resolve;
      });
      const api = mockAdapterAPI(
        action === "register"
          ? null
          : adapterPayload({ enabled: action === "disable" }),
        async () => {
          await pending;
        },
      );
      renderSettings();
      fireEvent.click(await screen.findByRole("button", { name: label }));
      expect(
        await screen.findByRole("button", { name: pendingLabel }),
      ).toBeDisabled();
      for (const button of screen.getAllByRole("button")) {
        expect(button).toBeDisabled();
        fireEvent.click(button);
      }
      expect(api.writes()).toHaveLength(1);
      await act(async () => {
        release();
      });
      await waitFor(() =>
        expect(
          screen.queryByRole("button", { name: pendingLabel }),
        ).not.toBeInTheDocument(),
      );
    },
  );

  it("refreshes on version conflict and retries with the new version", async () => {
    let fail = true;
    const api = mockAdapterAPI(
      adapterPayload({ version: 1 }),
      async (action) => {
        if (action === "enable" && fail) {
          fail = false;
          api.setAdapter(adapterPayload({ version: 4 }));
          return response(
            { code: "VERSION_CONFLICT", message: "Adapter changed" },
            409,
          );
        }
      },
    );
    renderSettings();
    fireEvent.click(await screen.findByRole("button", { name: "启用适配器" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "适配器状态已变化，请查看刷新后的状态再重试。",
    );
    fireEvent.click(screen.getByRole("button", { name: "启用适配器" }));
    expect(
      await screen.findByText("适配器已启用；请到任务详情启动执行。"),
    ).toBeTruthy();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(
      api
        .writes()
        .map(([, init]) => new Headers(init?.headers).get("If-Match")),
    ).toEqual(['"1"', '"4"']);
  });

  it("explains rejected enable readiness and clears that error after a new check", async () => {
    mockAdapterAPI(adapterPayload(), async (action) => {
      if (action === "enable") {
        return response(
          { code: "AGENT_ADAPTER_NOT_EXECUTION_READY", message: "Not ready" },
          409,
        );
      }
    });
    renderSettings();
    fireEvent.click(await screen.findByRole("button", { name: "启用适配器" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "内置执行器尚未就绪，请重新检查运行条件后再启用。",
    );
    fireEvent.click(screen.getByRole("button", { name: "重新检查" }));
    expect(
      await screen.findByText("内置执行器生命周期检查已通过。"),
    ).toBeTruthy();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("reports registration failure without showing a false success", async () => {
    mockAdapterAPI(null, async () =>
      response(
        { code: "AGENT_ADAPTER_PRESET_INVALID", message: "内置适配器不可用" },
        422,
      ),
    );
    renderSettings();
    fireEvent.click(
      await screen.findByRole("button", { name: "登记内置适配器" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "内置适配器不可用",
    );
    expect(
      screen.queryByText("内置适配器已登记，尚未启用。"),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "登记内置适配器" }),
    ).toBeEnabled();
  });

  it("explains an Agent identity conflict without showing enabled state", async () => {
    mockAdapterAPI(adapterPayload(), async (action) => {
      if (action === "enable") {
        return response(
          { code: "AGENT_ADAPTER_ACTOR_CONFLICT", message: "Actor mismatch" },
          409,
        );
      }
    });
    renderSettings();
    fireEvent.click(await screen.findByRole("button", { name: "启用适配器" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Agent 身份关联冲突，无法启用此适配器；请检查现有 Agent 的关联配置。",
    );
    expect(screen.getByText("未启用 · 健康")).toBeTruthy();
    expect(
      screen.queryByText("适配器已启用；请到任务详情启动执行。"),
    ).not.toBeInTheDocument();
  });

  it.each([
    ["check", "重新检查", false],
    ["disable", "停用适配器", true],
  ] as const)(
    "retains committed state when %s fails",
    async (action, label, enabled) => {
      const api = mockAdapterAPI(
        adapterPayload({ enabled }),
        async (requestedAction) => {
          if (requestedAction === action) {
            return response(
              {
                code: "TEST_FAILURE",
                message: "操作暂时不可用",
              },
              422,
            );
          }
        },
      );
      renderSettings();
      fireEvent.click(await screen.findByRole("button", { name: label }));
      expect(await screen.findByRole("alert")).toHaveTextContent(
        `操作暂时不可用 · 请求 ${new Headers(api.writes()[0][1]?.headers).get("X-Request-ID")}`,
      );
      expect(
        screen.getByText(enabled ? "已启用 · 健康" : "未启用 · 健康"),
      ).toBeTruthy();
      expect(screen.getByRole("button", { name: label })).toBeEnabled();
      expect(api.writes()).toHaveLength(1);
    },
  );
});
