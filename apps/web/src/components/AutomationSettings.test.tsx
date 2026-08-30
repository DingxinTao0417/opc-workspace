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
import { AutomationSettings } from "./AutomationSettings";

function response(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function rulePayload(version = 1, status = "disabled", localTime = "09:00") {
  return {
    id: "00000000-0000-5000-8000-000000000102",
    preset_key: "daily-today-reminder",
    name: "每日查看今日任务",
    description: "每天指定当地时间创建一条应用内提醒。",
    status,
    available: true,
    trigger_type: "schedule",
    trigger_label: "每天指定当地时间",
    action_type: "reminder",
    action_label: "创建“查看今日任务”本地提醒",
    config: { local_time: localTime, timezone: "Asia/Shanghai" },
    next_run_at: status === "enabled" ? "2026-08-30T00:30:00Z" : null,
    permissions: ["读取本地时间", "创建一条本地 Reminder"],
    version,
    created_at: "2026-08-29T10:00:00Z",
    updated_at: "2026-08-29T10:00:00Z",
  };
}

function invoiceRulePayload(version = 1, status = "disabled", priority = "P1") {
  return {
    id: "00000000-0000-5000-8000-000000000104",
    preset_key: "invoice-overdue-task",
    name: "发票逾期跟进",
    description:
      "发票进入逾期状态后创建本地跟进任务；不会自动发送邮件或客户消息。",
    status,
    available: true,
    unavailable_reason: "",
    trigger_type: "event",
    trigger_label: "发票工作流事件：invoice_overdue",
    action_type: "task",
    action_label: "创建“跟进逾期发票”本地任务",
    config: { priority },
    next_run_at: null,
    permissions: [
      "读取本地发票逾期事件",
      "创建一条本地跟进任务",
      "记录本地自动化运行",
    ],
    version,
    created_at: "2026-08-29T10:00:00Z",
    updated_at: "2026-08-30T10:00:00Z",
  };
}

function invoiceRunPayload() {
  return {
    id: "018f0000-0000-7000-8000-000000001610",
    rule_id: "00000000-0000-5000-8000-000000000104",
    preset_key: "invoice-overdue-task",
    rule_name: "发票逾期跟进",
    rule_version: 3,
    trigger_type: "event",
    source_event_id: "018f0000-0000-7000-8000-000000001611",
    scheduled_for: null,
    status: "succeeded",
    attempt: 1,
    retry_of_run_id: null,
    retryable: false,
    retry_at: null,
    caused_by_run_id: null,
    causal_depth: 0,
    config_snapshot: { priority: "P0" },
    action_snapshot: { action_type: "task", title: "跟进逾期发票" },
    error_code: null,
    result_type: "task",
    result_id: "018f0000-0000-7000-8000-000000001612",
    result_summary: "已创建本地发票跟进任务。",
    started_at: "2026-08-30T10:00:01Z",
    ended_at: "2026-08-30T10:00:01Z",
  };
}

function renderSettings(
  onOpenTask = vi.fn(),
  onOpenInboxItem = vi.fn(),
  onOpenReminder = vi.fn(),
) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <AutomationSettings
        onOpenInboxItem={onOpenInboxItem}
        onOpenReminder={onOpenReminder}
        onOpenTask={onOpenTask}
      />
    </QueryClientProvider>,
  );
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  resetRuntimeConnection();
});

describe("AutomationSettings", () => {
  it("previews schedule edits, saves them, and enables the rule", async () => {
    let rule = rulePayload();
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        if (url.includes("/automations/runs")) {
          return response({
            data: [],
            meta: { page: 1, page_size: 20, total: 0 },
          });
        }
        if (url.endsWith("/preview")) {
          const body = JSON.parse(String(init?.body));
          return response({
            data: {
              can_enable: true,
              trigger_summary: "每天指定当地时间",
              action_summary: "创建本地提醒",
              config: body.config,
              next_run_at: "2026-08-30T00:30:00Z",
              permissions: ["读取本地时间"],
            },
          });
        }
        if (init?.method === "PATCH") {
          const body = JSON.parse(String(init.body));
          rule = rulePayload(2, "disabled", body.config.local_time);
          return response({ data: rule });
        }
        if (url.endsWith("/enable")) {
          rule = rulePayload(3, "enabled", "08:30");
          return response({ data: rule });
        }
        return response({ data: [rule] });
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    renderSettings();

    const timeInput = await screen.findByLabelText("当地时间");
    fireEvent.change(timeInput, { target: { value: "08:30" } });
    await waitFor(() => {
      expect(
        fetchMock.mock.calls.some(
          ([url, init]) =>
            String(url).endsWith("/preview") &&
            JSON.parse(String(init?.body)).config.local_time === "08:30",
        ),
      ).toBe(true);
    });

    fireEvent.click(screen.getByRole("button", { name: "保存配置" }));
    expect(await screen.findByText("配置已保存")).toBeTruthy();
    const patchCall = fetchMock.mock.calls.find(
      ([, init]) => init?.method === "PATCH",
    );
    expect(new Headers(patchCall?.[1]?.headers).get("If-Match")).toBe('"1"');

    fireEvent.click(screen.getByRole("button", { name: "启用自动化" }));
    expect(await screen.findByText("自动化已启用")).toBeTruthy();
    const enableCall = fetchMock.mock.calls.find(([url]) =>
      String(url).endsWith("/enable"),
    );
    expect(new Headers(enableCall?.[1]?.headers).get("If-Match")).toBe('"2"');
    expect(
      screen.getByText(
        "仅执行白名单本地动作；不运行脚本、SQL、HTTP，不对外发送内容。",
      ),
    ).toBeTruthy();
  });

  it("enables the available invoice task preset with priority and opens its result task", async () => {
    let rule = invoiceRulePayload();
    const onOpenTask = vi.fn();
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        if (url.includes("/automations/runs")) {
          return response({
            data: [invoiceRunPayload()],
            meta: { page: 1, page_size: 20, total: 1 },
          });
        }
        if (url.endsWith("/preview")) {
          const body = JSON.parse(String(init?.body));
          return response({
            data: {
              can_enable: true,
              unavailable_reason: "",
              trigger_summary: "发票工作流事件：invoice_overdue",
              action_summary: "创建“跟进逾期发票”本地任务",
              config: body.config,
              next_run_at: null,
              permissions: [
                "读取本地发票逾期事件",
                "创建一条本地跟进任务",
                "记录本地自动化运行",
              ],
            },
          });
        }
        if (init?.method === "PATCH") {
          const body = JSON.parse(String(init.body));
          rule = invoiceRulePayload(2, "disabled", body.config.priority);
          return response({ data: rule });
        }
        if (url.endsWith("/enable")) {
          rule = invoiceRulePayload(3, "enabled", "P0");
          return response({ data: rule });
        }
        return response({ data: [rule] });
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    renderSettings(onOpenTask);

    expect(
      await screen.findByRole("heading", {
        name: "发票逾期跟进",
      }),
    ).toBeTruthy();
    expect(screen.getByText("动作 · 任务")).toBeTruthy();
    expect(screen.getByText("创建“跟进逾期发票”本地任务")).toBeTruthy();
    expect(screen.queryByText("待依赖")).toBeNull();
    expect(screen.queryByText(/依赖.*不可用/)).toBeNull();

    fireEvent.change(screen.getByLabelText("任务优先级"), {
      target: { value: "P0" },
    });
    await waitFor(() => {
      expect(
        fetchMock.mock.calls.some(
          ([url, init]) =>
            String(url).endsWith("/preview") &&
            JSON.parse(String(init?.body)).config.priority === "P0",
        ),
      ).toBe(true);
    });

    fireEvent.click(screen.getByRole("button", { name: "启用自动化" }));
    expect(await screen.findByText("自动化已启用")).toBeTruthy();
    const patchCall = fetchMock.mock.calls.find(
      ([, init]) => init?.method === "PATCH",
    );
    expect(JSON.parse(String(patchCall?.[1]?.body))).toEqual({
      config: { priority: "P0" },
    });
    expect(new Headers(patchCall?.[1]?.headers).get("If-Match")).toBe('"1"');
    const enableCall = fetchMock.mock.calls.find(([url]) =>
      String(url).endsWith("/enable"),
    );
    expect(new Headers(enableCall?.[1]?.headers).get("If-Match")).toBe('"2"');

    fireEvent.click(screen.getByRole("button", { name: "打开任务" }));
    expect(onOpenTask).toHaveBeenCalledWith(
      "018f0000-0000-7000-8000-000000001612",
    );
    expect(
      screen.getByText(
        "启用受限的本地预设，让明确事件或时间触发 Inbox、Task 与 Reminder 动作。",
      ),
    ).toBeTruthy();
  });

  it("uses server pagination and resets the page when filters change", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = new URL(String(input), "http://localhost");
      if (url.pathname.endsWith("/automations/runs")) {
        const page = Number(url.searchParams.get("page"));
        const status = url.searchParams.get("status");
        if (status === "cancelled") {
          return response({
            data: [],
            meta: { page: 1, page_size: 20, total: 0 },
          });
        }
        return response({
          data: [
            {
              ...invoiceRunPayload(),
              id: `018f0000-0000-7000-8000-00000000161${page}`,
              result_summary: `第 ${page} 页记录`,
            },
          ],
          meta: { page, page_size: 20, total: 45 },
        });
      }
      if (url.pathname.endsWith("/preview")) {
        return response({
          data: {
            can_enable: true,
            unavailable_reason: "",
            trigger_summary: "发票工作流事件：invoice_overdue",
            action_summary: "创建任务",
            config: { priority: "P1" },
            next_run_at: null,
            permissions: [],
          },
        });
      }
      return response({ data: [invoiceRulePayload()] });
    });
    vi.stubGlobal("fetch", fetchMock);

    renderSettings();

    expect(await screen.findByText("第 1 页记录")).toBeVisible();
    expect(screen.getByText("第 1 / 3 页")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    expect(await screen.findByText("第 2 页记录")).toBeVisible();
    expect(screen.queryByText("第 1 页记录")).toBeNull();
    expect(screen.getByText("第 2 / 3 页")).toBeVisible();

    fireEvent.change(screen.getByLabelText("状态"), {
      target: { value: "cancelled" },
    });

    expect(await screen.findByText("没有符合筛选条件的运行记录")).toBeVisible();
    const filteredCall = fetchMock.mock.calls.find(([input]) => {
      const url = new URL(String(input), "http://localhost");
      return (
        url.pathname.endsWith("/automations/runs") &&
        url.searchParams.get("status") === "cancelled"
      );
    });
    expect(
      new URL(String(filteredCall?.[0]), "http://localhost").searchParams.get(
        "page",
      ),
    ).toBe("1");
    expect(
      new URL(String(filteredCall?.[0]), "http://localhost").searchParams.get(
        "page_size",
      ),
    ).toBe("20");
  });

  it("opens successful inbox and reminder results", async () => {
    const onOpenInboxItem = vi.fn();
    const onOpenReminder = vi.fn();
    const inboxRun = {
      ...invoiceRunPayload(),
      id: "018f0000-0000-7000-8000-000000001620",
      result_type: "inbox_item",
      result_id: "018f0000-0000-7000-8000-000000001621",
      result_summary: "已创建收件箱事项",
    };
    const reminderRun = {
      ...invoiceRunPayload(),
      id: "018f0000-0000-7000-8000-000000001622",
      result_type: "reminder",
      result_id: "018f0000-0000-7000-8000-000000001623",
      result_summary: "已创建提醒",
    };
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input);
        if (url.includes("/automations/runs")) {
          return response({
            data: [inboxRun, reminderRun],
            meta: { page: 1, page_size: 20, total: 2 },
          });
        }
        if (url.endsWith("/preview")) {
          return response({
            data: {
              can_enable: true,
              unavailable_reason: "",
              trigger_summary: "事件",
              action_summary: "动作",
              config: { priority: "P1" },
              next_run_at: null,
              permissions: [],
            },
          });
        }
        return response({ data: [invoiceRulePayload()] });
      }),
    );

    renderSettings(vi.fn(), onOpenInboxItem, onOpenReminder);

    fireEvent.click(
      await screen.findByRole("button", { name: "打开收件箱事项" }),
    );
    expect(onOpenInboxItem).toHaveBeenCalledWith(inboxRun.result_id);
    expect(screen.getByText("已创建提醒")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "打开提醒" }));
    expect(onOpenReminder).toHaveBeenCalledWith(reminderRun.result_id);
    expect(screen.getAllByRole("button", { name: "查看详情" })).toHaveLength(2);
  });

  it("distinguishes initial run load errors from refresh errors with stale data", async () => {
    let runRequests = 0;
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes("/automations/runs")) {
        runRequests += 1;
        if (runRequests > 1) {
          return response(
            { error: { code: "RUNS_UNAVAILABLE", message: "暂时不可用" } },
            503,
          );
        }
        return response({
          data: [invoiceRunPayload()],
          meta: { page: 1, page_size: 20, total: 1 },
        });
      }
      if (url.endsWith("/preview")) {
        return response({
          data: {
            can_enable: true,
            unavailable_reason: "",
            trigger_summary: "事件",
            action_summary: "动作",
            config: { priority: "P1" },
            next_run_at: null,
            permissions: [],
          },
        });
      }
      return response({ data: [invoiceRulePayload()] });
    });
    vi.stubGlobal("fetch", fetchMock);

    renderSettings();
    expect(await screen.findByText("已创建本地发票跟进任务。")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "刷新" }));

    expect(
      await screen.findByText(
        /运行记录刷新失败，当前显示上次成功读取的数据/,
        {},
        { timeout: 3_000 },
      ),
    ).toBeVisible();
    expect(screen.getByText("已创建本地发票跟进任务。")).toBeVisible();
  });

  it("shows an actionable initial run load error", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input);
        if (url.includes("/automations/runs")) {
          return response(
            { error: { code: "RUNS_UNAVAILABLE", message: "暂时不可用" } },
            503,
          );
        }
        if (url.endsWith("/preview")) {
          return response({
            data: {
              can_enable: true,
              unavailable_reason: "",
              trigger_summary: "事件",
              action_summary: "动作",
              config: { priority: "P1" },
              next_run_at: null,
              permissions: [],
            },
          });
        }
        return response({ data: [invoiceRulePayload()] });
      }),
    );

    renderSettings();

    const alert = await screen.findByRole("alert", {}, { timeout: 3_000 });
    expect(alert).toHaveTextContent("请求失败（503）");
    expect(alert).toContainElement(
      screen.getByRole("button", { name: "重试" }),
    );
    expect(screen.queryByText("暂无运行记录")).toBeNull();
  });
});
