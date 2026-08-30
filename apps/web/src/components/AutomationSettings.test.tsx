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

function renderSettings(onOpenTask = vi.fn()) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <AutomationSettings onOpenTask={onOpenTask} />
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
});
