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
  Routes,
  Route,
  useNavigate,
  useLocation,
  type NavigateFunction,
  type Location,
} from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  normalizeAutomationRunDetail,
  resetRuntimeConnection,
} from "../api/client";
import { automationRunDetailQueryKey } from "../api/hooks";
import { renderAiRichText } from "../components/AiRichText";
import { useAiChatStore } from "../store/aiChat";
import { useAiWorkbenchHandoff } from "../store/aiWorkbenchHandoff";
import { AutomationLocationPage } from "./AutomationLocationPage";

const ruleId = "00000000-0000-5000-8000-000000000102";
const otherRule = "00000000-0000-5000-8000-000000000103";
const runId = "018f0000-0000-7000-8000-000000001610";
const nextId = "018f0000-0000-7000-8000-000000001611";
const session = "018f0000-0000-7000-8000-000000001612";
const date = "2026-09-18T09:00:00Z";
const rule = (id = ruleId) => ({
  id,
  preset_key: id === ruleId ? "daily-today-reminder" : "weekly-review-reminder",
  name: id === ruleId ? "精确每日规则" : "另一个预设",
  description: "本地提醒",
  status: "disabled",
  available: true,
  unavailable_reason: "",
  trigger_type: "schedule",
  trigger_label: "当地时间",
  action_type: "reminder",
  action_label: "创建本地提醒",
  config: { local_time: "11:45", timezone: "Asia/Shanghai" },
  next_run_at: null,
  permissions: ["创建本地 Reminder"],
  version: 4,
  created_at: date,
  updated_at: date,
});
const attempt = (id: string, n: number) => ({
  id,
  status: "failed",
  attempt: n,
  retry_of_run_id: n === 1 ? null : runId,
  retryable: n < 3,
  retry_at: null,
  error_code: "ACTION_WRITE_FAILED",
  result_type: null,
  result_id: null,
  result_summary: "动作失败",
  started_at: date,
  ended_at: date,
});
const run = (id = runId) => ({
  ...attempt(id, id === runId ? 1 : 2),
  rule_id: ruleId,
  rule_version: 2,
  preset_key: "daily-today-reminder",
  rule_name: "捕获的每日规则",
  trigger_type: "schedule",
  source_event_id: null,
  scheduled_for: date,
  caused_by_run_id: null,
  causal_depth: 0,
  config_snapshot: { local_time: "09:00", timezone: "UTC" },
  action_snapshot: { action_type: "reminder", title: "只供人工看的执行快照" },
  source: {
    kind: "schedule",
    available: true,
    event_id: null,
    aggregate_type: null,
    aggregate_id: null,
    action: null,
    occurred_at: null,
    scheduled_for: date,
  },
  retry_chain: [attempt(runId, 1), attempt(nextId, 2)],
});
const response = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
const calls = vi.fn(
  async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const path = new URL(String(input), "http://localhost").pathname;
    if (path === `/api/v1/automations/runs/${runId}`)
      return response({ data: run() });
    if (path === `/api/v1/automations/runs/${nextId}`)
      return response({ data: run(nextId) });
    if (path.endsWith("/automations/rules"))
      return response({ data: [rule(otherRule), rule()] });
    if (path.endsWith("/automations/runs"))
      return response({ data: [], meta: { page: 1, page_size: 20, total: 0 } });
    if (path.endsWith("/preview"))
      return response({
        data: {
          can_enable: true,
          unavailable_reason: "",
          trigger_summary: "当地时间",
          action_summary: "创建本地提醒",
          config: JSON.parse(String(init?.body)).config,
          next_run_at: null,
          permissions: ["创建本地 Reminder"],
        },
      });
    return response(
      { error: { code: "NOT_FOUND", message: "记录不存在" } },
      404,
    );
  },
);
const originalHandler = calls.getMockImplementation()!;

function mount(
  path = "/ai",
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, retryDelay: 0 } },
  }),
) {
  let navigate!: NavigateFunction;
  let location!: Location;
  function NavigationProbe() {
    navigate = useNavigate();
    location = useLocation();
    return null;
  }
  const router = {
    navigate: (delta: number) => navigate(delta),
    get state() {
      return { location };
    },
  };
  const view = render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[path]}>
        <NavigationProbe />
        <Routes>
          <Route
            path="/ai"
            element={
              <div>
                {renderAiRichText(
                  `[查看记录](/settings/automation?run=${runId})`,
                  session,
                )}
              </div>
            }
          />
          <Route
            path="/settings/automation"
            element={<AutomationLocationPage />}
          />
          <Route path="/tasks/:taskId" element={<div>任务目标</div>} />
          <Route path="/inbox/:itemId" element={<div>收件箱目标</div>} />
          <Route path="/inbox" element={<div>提醒目标</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return { ...view, router, queryClient };
}

beforeEach(() => {
  calls.mockReset().mockImplementation(originalHandler);
  vi.stubGlobal("fetch", calls);
  useAiChatStore.setState({ activeSessionId: undefined });
  useAiWorkbenchHandoff.setState({ pending: null, pendingIssue: null });
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  resetRuntimeConnection();
});

describe("exact automation settings navigation", () => {
  describe.each([
    ["task", "打开任务", `/tasks/${nextId}`],
    ["inbox_item", "打开收件箱事项", `/inbox/${nextId}`],
    ["reminder", "打开提醒", `/inbox?reminder=${nextId}`],
  ])("result next hop %s", (kind, label, target) => {
    function showResult(resultId = nextId) {
      const result = {
        ...attempt(runId, 1),
        status: "succeeded",
        retryable: false,
        error_code: null,
        result_type: kind,
        result_id: resultId,
        result_summary: "已创建本地结果",
      };
      calls.mockImplementation(async () =>
        response({
          data: {
            ...run(),
            ...result,
            retry_chain: [result],
          },
        }),
      );
    }

    it.each([undefined, session, "invalid?return_session=foreign"])(
      "keeps only a valid return identity (%s) without activating chat",
      async (source) => {
        showResult();
        useAiChatStore.setState({ activeSessionId: nextId });
        const { router } = mount(
          `/settings/automation?run=${runId}${source ? `&return_session=${encodeURIComponent(source)}` : ""}`,
        );
        fireEvent.click(await screen.findByRole("button", { name: label }));
        const suffix =
          source === session
            ? `${target.includes("?") ? "&" : "?"}return_session=${session}`
            : "";
        expect(
          `${router.state.location.pathname}${router.state.location.search}`,
        ).toBe(`${target}${suffix}`);
        expect(useAiChatStore.getState().activeSessionId).toBe(nextId);
        expect(useAiWorkbenchHandoff.getState().pendingIssue).toBeNull();
        expect(calls).toHaveBeenCalledTimes(1);
        expect(
          calls.mock.calls.every(
            ([, init]) => !init?.method || init.method === "GET",
          ),
        ).toBe(true);
      },
    );

    it("refuses a malformed result identity without changing routes or chat", async () => {
      showResult(`${nextId}/../../ai?return_session=${session}`);
      const initial = `/settings/automation?run=${runId}&return_session=${session}`;
      const { router } = mount(initial);
      fireEvent.click(await screen.findByRole("button", { name: label }));
      expect(
        `${router.state.location.pathname}${router.state.location.search}`,
      ).toBe(initial);
      expect(useAiChatStore.getState().activeSessionId).toBeUndefined();
      expect(calls).toHaveBeenCalledTimes(1);
    });
  });

  it("opens a run not in the current list, reads its snapshot only locally and returns to the originating chat", async () => {
    const { router } = mount();
    fireEvent.click(screen.getByRole("link", { name: "查看记录" }));
    expect(await screen.findByText(/只供人工看的执行快照/)).toBeVisible();
    expect(router.state.location.search).toBe(
      `?run=${runId}&return_session=${session}`,
    );
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(
      calls.mock.calls.every(
        ([url, init]) =>
          String(url).includes(`/runs/${runId}`) &&
          (!init?.method || init.method === "GET"),
      ),
    ).toBe(true);
    fireEvent.click(screen.getByRole("link", { name: "返回原对话" }));
    expect(router.state.location.pathname).toBe("/ai");
    expect(useAiChatStore.getState().activeSessionId).toBe(session);
  });

  it("makes attempt selection deep-linkable and browser back returns to the original attempt", async () => {
    const { router } = mount(
      `/settings/automation?run=${runId}&return_session=${session}`,
    );
    await screen.findByText("第 1 次尝试");
    fireEvent.click(screen.getByRole("button", { name: /第 2 次/ }));
    await screen.findByText("第 2 次尝试");
    expect(router.state.location.search).toBe(
      `?run=${nextId}&return_session=${session}`,
    );
    await act(async () => {
      await router.navigate(-1);
    });
    await screen.findByText("第 1 次尝试");
    expect(
      calls.mock.calls.some(([url]) => String(url).endsWith("/retry")),
    ).toBe(false);
  });

  it("selects the actual rule rather than the first preset and keeps the existing editor", async () => {
    const { router } = mount(
      `/settings/automation?rule=${ruleId}&return_session=${session}`,
    );
    expect(
      await screen.findByRole("heading", { name: "精确每日规则" }),
    ).toBeVisible();
    expect(screen.getByLabelText("当地时间")).toHaveValue("11:45");
    fireEvent.change(screen.getByLabelText("当地时间"), {
      target: { value: "10:30" },
    });
    fireEvent.click(screen.getByRole("button", { name: /另一个预设/ }));
    expect(
      await screen.findByRole("heading", { name: "另一个预设" }),
    ).toBeVisible();
    expect(screen.getByLabelText("当地时间")).toHaveValue("11:45");
    expect(router.state.location.search).toBe(
      `?rule=${otherRule}&return_session=${session}`,
    );
    expect(calls.mock.calls.some(([, init]) => init?.method === "PATCH")).toBe(
      false,
    );
  });

  it("hands the selected rule to the agent without saving or enabling it", async () => {
    const { router } = mount(
      `/settings/automation?rule=${ruleId}&return_session=${session}`,
    );
    expect(
      await screen.findByRole("heading", { name: "精确每日规则" }),
    ).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "交给智能体" }));

    const pending = useAiWorkbenchHandoff.getState().pendingIssue;
    expect(pending).toMatchObject({
      label: "自动化规则",
      route: `/settings/automation?rule=${ruleId}`,
      scopes: ["work", "actions"],
    });
    expect(pending?.prompt).toContain("workspace_automations");
    expect(pending?.prompt).toContain("view=rule");
    expect(pending?.prompt).toContain(ruleId);
    expect(useAiWorkbenchHandoff.getState().pending).toBeNull();
    expect(router.state.location.pathname).toBe("/ai");
    expect(calls.mock.calls.some(([, init]) => init?.method === "PATCH")).toBe(
      false,
    );
    expect(
      calls.mock.calls.some(([url]) => String(url).endsWith("/enable")),
    ).toBe(false);
  });

  it("hands the exact run to the agent without retrying it", async () => {
    const { router } = mount(
      `/settings/automation?run=${runId}&return_session=${session}`,
    );
    await screen.findByText(/只供人工看的执行快照/);

    fireEvent.click(screen.getByRole("button", { name: "交给智能体" }));

    const pending = useAiWorkbenchHandoff.getState().pendingIssue;
    expect(pending).toMatchObject({
      label: "自动化运行",
      route: `/settings/automation?run=${runId}`,
      scopes: ["work", "actions"],
    });
    expect(pending?.prompt).toContain("workspace_automations");
    expect(pending?.prompt).toContain("view=run");
    expect(pending?.prompt).toContain(runId);
    expect(pending?.prompt).toContain("automation.retry");
    expect(pending?.prompt).toContain("不要读取或复述页面上的配置快照");
    expect(router.state.location.pathname).toBe("/ai");
    expect(
      calls.mock.calls.some(([url]) => String(url).endsWith("/retry")),
    ).toBe(false);
  });

  it("does not fall back to the first rule when the requested rule is missing", async () => {
    mount(`/settings/automation?rule=${runId}&return_session=${session}`);
    expect(await screen.findByText("指定自动化规则不可用")).toBeVisible();
    expect(screen.queryByRole("button", { name: "启用自动化" })).toBeNull();
    expect(
      calls.mock.calls.some(([url]) => String(url).includes("/preview")),
    ).toBe(false);
    expect(screen.getByRole("link", { name: "返回原对话" })).toBeVisible();
  });

  it.each([
    `run=${runId}&rule=${ruleId}`,
    "run=invalid",
    `return_session=${session}&return_session=${session}`,
  ])("rejects invalid selection before querying: %s", (query) => {
    mount(`/settings/automation?${query}`);
    expect(screen.getByRole("alert")).toHaveTextContent("链接无效");
    expect(calls).not.toHaveBeenCalled();
  });

  it("rejects a mismatched run response without exposing snapshots or actions", async () => {
    calls.mockImplementation(async () => response({ data: run(nextId) }));
    mount(`/settings/automation?run=${runId}`);
    expect(await screen.findByText("无法读取运行详情")).toBeVisible();
    expect(screen.queryByText(/只供人工看的执行快照/)).toBeNull();
    expect(screen.queryByRole("button", { name: "重试本次运行" })).toBeNull();
  });

  it("hides cached details on fresh entry and on failed revalidation", async () => {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false, retryDelay: 0 } },
    });
    client.setQueryData(
      automationRunDetailQueryKey(runId),
      normalizeAutomationRunDetail(run()),
    );
    let finish!: (value: Response) => void;
    calls.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    calls.mockImplementation(async () =>
      response({ error: { code: "NOT_FOUND", message: "已不可用" } }, 404),
    );
    mount(`/settings/automation?run=${runId}`, client);
    expect(screen.getByText("正在读取不可变运行记录…")).toBeVisible();
    expect(screen.queryByText(/只供人工看的执行快照/)).toBeNull();
    await waitFor(() => expect(finish).toBeTypeOf("function"));
    await act(async () => {
      finish(
        response({ error: { code: "NOT_FOUND", message: "已不可用" } }, 404),
      );
    });
    expect(await screen.findByText("无法读取运行详情")).toBeVisible();
    expect(screen.queryByText(/只供人工看的执行快照/)).toBeNull();
  });

  it("aborts the exact request when returning to chat during a load", async () => {
    let requestSignal: AbortSignal | null | undefined;
    calls.mockImplementation((_input, init) => {
      requestSignal = init?.signal;
      return new Promise(() => {});
    });
    mount(`/settings/automation?run=${runId}&return_session=${session}`);
    await waitFor(() => expect(requestSignal).toBeTruthy());
    fireEvent.click(screen.getByRole("link", { name: "返回原对话" }));
    expect(requestSignal?.aborted).toBe(true);
  });

  it("does not hijack navigation when a manually requested retry finishes after leaving", async () => {
    let finish!: (value: Response) => void;
    calls.mockImplementation((input, init) =>
      String(input).endsWith("/retry")
        ? new Promise((resolve) => {
            finish = resolve;
          })
        : originalHandler(input, init),
    );
    const { router } = mount(
      `/settings/automation?run=${runId}&return_session=${session}`,
    );
    fireEvent.click(
      await screen.findByRole("button", { name: "重试本次运行" }),
    );
    await waitFor(() => expect(finish).toBeTypeOf("function"));
    fireEvent.click(screen.getByRole("link", { name: "返回原对话" }));
    await act(async () => {
      finish(response({ data: run(nextId) }));
    });
    expect(router.state.location.pathname).toBe("/ai");
    expect(useAiChatStore.getState().activeSessionId).toBe(session);
    expect(
      calls.mock.calls.filter(([url]) => String(url).endsWith("/retry")),
    ).toHaveLength(1);
  });
});
