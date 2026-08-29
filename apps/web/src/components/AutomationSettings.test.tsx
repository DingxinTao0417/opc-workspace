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

function renderSettings() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <AutomationSettings />
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
});
